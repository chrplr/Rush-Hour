# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the Apache License, Version 2.0.
# SPDX-License-Identifier: Apache-2.0

"""Many boards in one child process.

The per-step cost of this environment is entirely the pipe round trip — the
board logic itself is arithmetic on a dozen small integers. Running ``n``
copies of :class:`~rushhour_gym.env.RushHourEnv` therefore pays ``n`` round
trips per vector step and carries ``n`` Go runtimes, for a simulator that could
not care less. This class asks the server to advance all of them in one
request instead.

``SyncVectorEnv``/``AsyncVectorEnv`` over plain ``RushHourEnv`` still work; they
are just not the cheap path.
"""

from __future__ import annotations

import os
from typing import Any, Sequence

import numpy as np
from gymnasium import spaces
from gymnasium.utils import seeding
from gymnasium.vector import VectorEnv
from gymnasium.vector.utils import batch_space

from . import obs as _obs
from .binary import find_binary
from .engine import Engine, server_args
from .env import REWARD_SCHEMES, compute_reward, select_indices, state_info

__all__ = ["RushHourVectorEnv"]

try:  # gymnasium >= 1.0
    from gymnasium.vector import AutoresetMode

    _AUTORESET = AutoresetMode.NEXT_STEP
except ImportError:  # pragma: no cover - older gymnasium
    _AUTORESET = None

_DEFAULT_SOLVE_REWARD = {"step_penalty": 0.0, "sparse": 1.0, "shaped": 0.0}


class RushHourVectorEnv(VectorEnv):
    """``num_envs`` independent boards served by one ``rushhour-env`` process.

    Autoreset follows :data:`gymnasium.vector.AutoresetMode.NEXT_STEP`: the step
    that solves a board returns its final observation, and the *next* step
    ignores its action and returns the reset observation with zero reward.

    Args:
        num_envs: how many boards to keep in play.
        max_episode_steps: truncate an episode after this many steps. There is
            no other way for an unsolved episode to end — Rush Hour has no dead
            ends — so leaving it ``None`` means an episode can run forever.
        Remaining arguments are as for :class:`~rushhour_gym.env.RushHourEnv`.
    """

    metadata = {"render_modes": [], "autoreset_mode": _AUTORESET}

    def __init__(
        self,
        num_envs: int = 1,
        puzzle: str | None = None,
        puzzle_indices: Sequence[int] | None = None,
        min_moves_range: tuple[int, int] | None = None,
        *,
        max_episode_steps: int | None = None,
        obs_mode: str = "planes",
        reward_scheme: str = "step_penalty",
        solve_reward: float | None = None,
        illegal_penalty: float = 0.0,
        gamma: float = 0.99,
        include_board: bool = False,
        canonical_index: bool = True,
        puzzle_file: str | os.PathLike[str] | None = None,
        csv_path: str | os.PathLike[str] | None = None,
        subject_id: int = 0,
        binary: str | os.PathLike[str] | None = None,
        render_mode: str | None = None,
    ):
        if num_envs < 1:
            raise ValueError(f"num_envs must be positive, got {num_envs}")
        if obs_mode not in _obs.OBS_MODES:
            raise ValueError(f"obs_mode must be one of {_obs.OBS_MODES}, got {obs_mode!r}")
        if reward_scheme not in REWARD_SCHEMES:
            raise ValueError(
                f"reward_scheme must be one of {REWARD_SCHEMES}, got {reward_scheme!r}"
            )

        self.num_envs = num_envs
        self.obs_mode = obs_mode
        self.reward_scheme = reward_scheme
        self.solve_reward = (
            _DEFAULT_SOLVE_REWARD[reward_scheme] if solve_reward is None else solve_reward
        )
        self.illegal_penalty = illegal_penalty
        self.gamma = gamma
        self.max_episode_steps = max_episode_steps
        self.render_mode = render_mode

        self._want_remaining = reward_scheme == "shaped"

        self.engine = Engine(
            find_binary(binary),
            server_args(
                puzzle_file=puzzle_file,
                csv_path=csv_path,
                subject_id=subject_id,
                include_board=include_board,
                canonical=canonical_index,
            ),
        )
        meta = self.engine.meta
        self.n_slots: int = meta["max_vehicles"]
        self.catalog: list[dict[str, Any]] = meta["puzzles"]
        self._allowed = select_indices(
            self.catalog, puzzle, puzzle_indices, min_moves_range
        )

        self.single_action_space = spaces.Discrete(meta["n_actions"])
        self.single_observation_space = _obs.space_for(obs_mode, self.n_slots)
        self.action_space = batch_space(self.single_action_space, num_envs)
        self.observation_space = batch_space(self.single_observation_space, num_envs)

        self._env_ids = list(range(num_envs))
        # One generator per board, seeded the way SyncVectorEnv seeds its
        # sub-environments (seed + i), so the two agree episode for episode.
        self._rngs: list[np.random.Generator] = []
        self._states: list[dict[str, Any] | None] = [None] * num_envs
        self._elapsed = np.zeros(num_envs, dtype=np.int64)
        self._needs_reset = np.zeros(num_envs, dtype=bool)

    # ── Gymnasium vector API ─────────────────────────────────────────────────

    def reset(self, *, seed: int | list[int] | None = None, options: dict | None = None):
        if seed is None:
            seeds: list[int | None] = [None] * self.num_envs
        elif isinstance(seed, int):
            seeds = [seed + i for i in range(self.num_envs)]
        else:
            if len(seed) != self.num_envs:
                raise ValueError(f"expected {self.num_envs} seeds, got {len(seed)}")
            seeds = list(seed)

        for i, s in enumerate(seeds):
            if s is not None or len(self._rngs) <= i:
                rng, _ = seeding.np_random(s)
                if len(self._rngs) <= i:
                    self._rngs.append(rng)
                else:
                    self._rngs[i] = rng

        options = options or {}
        states = self._reset_envs(self._env_ids, options)
        for i, state in zip(self._env_ids, states):
            self._states[i] = state
        self._elapsed[:] = 0
        self._needs_reset[:] = False
        return self._observations(), self._infos()

    def step(self, actions):
        actions = np.asarray(actions).reshape(self.num_envs)

        # NEXT_STEP autoreset: boards that finished last step are reset now,
        # and their action is discarded. Everything else takes its step.
        to_reset = [i for i in self._env_ids if self._needs_reset[i]]
        to_step = [i for i in self._env_ids if not self._needs_reset[i]]

        previous = list(self._states)

        if to_reset:
            for i, state in zip(to_reset, self._reset_envs(to_reset, {})):
                self._states[i] = state
                self._elapsed[i] = 0
            self._needs_reset[to_reset] = False

        if to_step:
            request: dict[str, Any] = {
                "cmd": "step_batch",
                "env_ids": to_step,
                # np.int64 is not JSON-serialisable.
                "actions": [int(actions[i]) for i in to_step],
            }
            if self._want_remaining:
                request["want_remaining"] = True
            for i, state in zip(to_step, self.engine.request(request)["states"]):
                self._states[i] = state
                self._elapsed[i] += 1

        rewards = np.zeros(self.num_envs, dtype=np.float64)
        terminations = np.zeros(self.num_envs, dtype=bool)
        truncations = np.zeros(self.num_envs, dtype=bool)

        for i in to_step:
            rewards[i] = compute_reward(
                previous[i],
                self._states[i],
                scheme=self.reward_scheme,
                solve_reward=self.solve_reward,
                illegal_penalty=self.illegal_penalty,
                gamma=self.gamma,
            )
            terminations[i] = bool(self._states[i]["solved"])
            if self.max_episode_steps is not None and not terminations[i]:
                truncations[i] = self._elapsed[i] >= self.max_episode_steps
            self._needs_reset[i] = terminations[i] or truncations[i]

        return self._observations(), rewards, terminations, truncations, self._infos()

    def close(self, **_kwargs):
        engine = getattr(self, "engine", None)
        if engine is not None:
            engine.close()

    # ── Internals ────────────────────────────────────────────────────────────

    def _reset_envs(self, env_ids: Sequence[int], options: dict) -> list[dict[str, Any]]:
        request: dict[str, Any] = {"cmd": "reset_batch", "env_ids": list(env_ids)}
        if "spec" in options:
            request["spec"] = options["spec"]
        elif "puzzle" in options:
            request["puzzle"] = options["puzzle"]
        elif "puzzle_index" in options:
            request["puzzle_index"] = int(options["puzzle_index"])
        else:
            request["puzzle_indices"] = [
                int(self._rngs[i].choice(self._allowed)) for i in env_ids
            ]
        if self._want_remaining:
            request["want_remaining"] = True
        return self.engine.request(request)["states"]

    def _observations(self):
        per_env = [
            _obs.encode(state, self.obs_mode, self.n_slots) for state in self._states
        ]
        if self.obs_mode == "dict":
            return {key: np.stack([o[key] for o in per_env]) for key in per_env[0]}
        return np.stack(per_env)

    def _infos(self) -> dict[str, Any]:
        """Per-key arrays, as gymnasium's vector environments report them."""
        per_env = [state_info(state) for state in self._states]
        out: dict[str, Any] = {}
        for key in per_env[0]:
            values = [info[key] for info in per_env]
            out[key] = np.array(values)
            out[f"_{key}"] = np.ones(self.num_envs, dtype=bool)
        return out

    def action_masks(self) -> np.ndarray:
        """Legal actions for every board, as an ``(num_envs, n_actions)`` array."""
        return np.array([state["mask"] for state in self._states], dtype=bool)

    def __enter__(self) -> "RushHourVectorEnv":
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()

    def __del__(self):
        try:
            self.close()
        except Exception:  # pragma: no cover - interpreter shutdown
            pass
