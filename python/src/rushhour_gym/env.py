# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the Apache License, Version 2.0.
# SPDX-License-Identifier: Apache-2.0

"""The Gymnasium environment.

One action is one cell, which is also what one mouse click is in the experiment
the human participants run. That correspondence is the point: an agent's trace
and a participant's trace count the same events, so the two can be compared
directly rather than through a conversion nobody trusts.
"""

from __future__ import annotations

import os
from typing import Any, Sequence

import gymnasium
import numpy as np
from gymnasium import spaces

from . import obs as _obs
from .binary import find_binary
from .engine import Engine, server_args

__all__ = ["RushHourEnv", "REWARD_SCHEMES", "select_indices", "compute_reward", "state_info"]

REWARD_SCHEMES = ("step_penalty", "sparse", "shaped")

# Default reward for reaching the exit, per scheme. Under step_penalty the
# return is simply minus the number of steps taken, which is the quantity the
# human data measures, so no bonus is needed for the optimum to be the shortest
# solution.
_DEFAULT_SOLVE_REWARD = {"step_penalty": 0.0, "sparse": 1.0, "shaped": 0.0}


# ── Shared with the vector environment ───────────────────────────────────────
#
# These three are free functions rather than methods because RushHourVectorEnv
# needs exactly the same puzzle selection, reward and info, and two copies of
# that logic would be two things to keep in step.


def select_indices(
    catalog: Sequence[dict[str, Any]],
    puzzle: str | None = None,
    puzzle_indices: Sequence[int] | None = None,
    min_moves_range: tuple[int, int] | None = None,
) -> list[int]:
    """Resolve a puzzle selection into library positions."""
    indices = list(range(len(catalog)))
    if puzzle is not None:
        by_name = {entry["name"]: entry["index"] for entry in catalog}
        if puzzle not in by_name:
            raise ValueError(f"no puzzle named {puzzle!r} in the library")
        indices = [by_name[puzzle]]
    if puzzle_indices is not None:
        unknown = [i for i in puzzle_indices if not 0 <= i < len(catalog)]
        if unknown:
            raise ValueError(f"puzzle indices outside the library: {unknown}")
        wanted = set(puzzle_indices)
        indices = [i for i in indices if i in wanted]
    if min_moves_range is not None:
        low, high = min_moves_range
        indices = [i for i in indices if low <= catalog[i]["min_moves"] <= high]
    if not indices:
        raise ValueError("no puzzle matches the given selection")
    return indices


def compute_reward(
    before: dict[str, Any],
    after: dict[str, Any],
    *,
    scheme: str,
    solve_reward: float,
    illegal_penalty: float,
    gamma: float,
) -> float:
    """Price one step."""
    solved = bool(after["solved"])
    bonus = solve_reward if solved else 0.0
    penalty = illegal_penalty if after.get("illegal") else 0.0

    if scheme == "sparse":
        return bonus - penalty

    reward = -1.0 + bonus - penalty
    if scheme == "shaped":
        # Potential-based shaping with Phi(s) = -(optimal slides to go).
        # F = gamma*Phi(s') - Phi(s) leaves the optimal policy unchanged
        # (Ng, Harada & Russell 1999) while making progress visible early.
        phi_before = -float(before["remaining"])
        phi_after = -float(after["remaining"])
        reward += gamma * phi_after - phi_before
    return reward


def state_info(state: dict[str, Any]) -> dict[str, Any]:
    """The per-step info dictionary."""
    info: dict[str, Any] = {
        "action_mask": np.asarray(state["mask"], dtype=bool),
        "moved": bool(state["moved"]),
        "illegal": bool(state["illegal"]),
        "slot": state["slot"],
        "label": state["label"],
        "dir": state["dir"],
        "from": tuple(state["from"]),
        "to": tuple(state["to"]),
        # n_steps counts cells (clicks); n_slides collapses consecutive steps of
        # one vehicle one way, which is the unit puzzles.txt and the Rush Hour
        # literature report.
        "n_steps": state["n_steps"],
        "n_slides": state["n_slides"],
        "puzzle": state["puzzle"],
        "puzzle_index": state["puzzle_index"],
        "min_moves": state["min_moves"],
        "n_cars": state["n_cars"],
        "labels": state["labels"],
        # Monitor turns is_success into a success rate.
        "is_success": bool(state["solved"]),
    }
    if "board" in state:
        info["board"] = state["board"]
    if "remaining" in state:
        info["remaining"] = state["remaining"]
    return info


class RushHourEnv(gymnasium.Env):
    """Rush Hour, played one cell at a time.

    The rules run in a ``rushhour-env`` child process (see :mod:`.engine`), so
    this class never decides whether a move is legal — it only chooses what the
    agent sees and what it is paid.

    Args:
        puzzle: play this puzzle by name every episode.
        puzzle_indices: sample episodes from these library positions.
        min_moves_range: ``(low, high)`` inclusive filter on optimal length, the
            simplest curriculum lever.
        obs_mode: one of :data:`rushhour_gym.obs.OBS_MODES`.
        reward_scheme: one of :data:`REWARD_SCHEMES`.
        solve_reward: paid on the step that frees the red car.
        illegal_penalty: extra cost for an action that moves nothing. Zero by
            default: an unmasked policy emits these constantly early in
            training, and punishing them hard mostly teaches timidity.
        gamma: discount used by the ``shaped`` scheme's potential. It must match
            the agent's discount for the shaping to leave the optimal policy
            unchanged.
        include_board: ask the server for the board notation on every state.
        puzzle_file: an alternative puzzle library.
        csv_path: record the agent's play as a results file, in the columns a
            participant's session produces — the point of which is that one
            analysis script reads both. The file is complete once close() has
            run.
        subject_id: the subject_id column of that file.

    Episodes end only by solving. Rush Hour has no dead ends — every move is
    reversible — so there is no losing state; a step budget is a
    :class:`gymnasium.wrappers.TimeLimit` decision, applied by the registered
    ids and never by this class.
    """

    metadata = {"render_modes": ["ansi", "human"], "render_fps": 30}

    def __init__(
        self,
        puzzle: str | None = None,
        puzzle_indices: Sequence[int] | None = None,
        min_moves_range: tuple[int, int] | None = None,
        *,
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
        if obs_mode not in _obs.OBS_MODES:
            raise ValueError(f"obs_mode must be one of {_obs.OBS_MODES}, got {obs_mode!r}")
        if reward_scheme not in REWARD_SCHEMES:
            raise ValueError(
                f"reward_scheme must be one of {REWARD_SCHEMES}, got {reward_scheme!r}"
            )
        if render_mode is not None and render_mode not in self.metadata["render_modes"]:
            raise ValueError(f"unknown render_mode {render_mode!r}")

        self.obs_mode = obs_mode
        self.reward_scheme = reward_scheme
        self.solve_reward = (
            _DEFAULT_SOLVE_REWARD[reward_scheme] if solve_reward is None else solve_reward
        )
        self.illegal_penalty = illegal_penalty
        self.gamma = gamma
        self.render_mode = render_mode

        # "ansi" renders the board notation, which only the server can produce.
        self._include_board = include_board or render_mode == "ansi"
        # Shaping needs the optimal distance to go, which costs a search per
        # step; nothing else should pay for it.
        self._want_remaining = reward_scheme == "shaped"

        self.engine = Engine(
            find_binary(binary),
            server_args(
                puzzle_file=puzzle_file,
                csv_path=csv_path,
                subject_id=subject_id,
                include_board=self._include_board,
                canonical=canonical_index,
                render=render_mode == "human",
            ),
        )
        meta = self.engine.meta
        self.n_slots: int = meta["max_vehicles"]
        self.catalog: list[dict[str, Any]] = meta["puzzles"]

        self.action_space = spaces.Discrete(meta["n_actions"])
        self.observation_space = _obs.space_for(obs_mode, self.n_slots)

        self._allowed = self._select(puzzle, puzzle_indices, min_moves_range)
        self._state: dict[str, Any] | None = None

    # ── Puzzle selection ─────────────────────────────────────────────────────

    def _select(
        self,
        puzzle: str | None,
        puzzle_indices: Sequence[int] | None,
        min_moves_range: tuple[int, int] | None,
    ) -> list[int]:
        return select_indices(self.catalog, puzzle, puzzle_indices, min_moves_range)

    def set_puzzle_filter(
        self,
        *,
        puzzle: str | None = None,
        puzzle_indices: Sequence[int] | None = None,
        min_moves_range: tuple[int, int] | None = None,
    ) -> None:
        """Narrow or widen the pool between episodes, for a curriculum."""
        self._allowed = self._select(puzzle, puzzle_indices, min_moves_range)

    @property
    def puzzle_pool(self) -> list[str]:
        """Names of the puzzles episodes are currently drawn from."""
        return [self.catalog[i]["name"] for i in self._allowed]

    # ── Gymnasium API ────────────────────────────────────────────────────────

    def reset(self, *, seed: int | None = None, options: dict[str, Any] | None = None):
        super().reset(seed=seed)  # seeds self.np_random

        options = options or {}
        request: dict[str, Any] = {"cmd": "reset", "env_id": 0}
        if "spec" in options:
            request["spec"] = options["spec"]
        elif "puzzle" in options:
            request["puzzle"] = options["puzzle"]
        elif "puzzle_index" in options:
            request["puzzle_index"] = int(options["puzzle_index"])
        else:
            # All randomness lives here, driven by self.np_random, so a seeded
            # run is reproducible without the server knowing anything about it.
            request["puzzle_index"] = int(self.np_random.choice(self._allowed))
        if self._want_remaining:
            request["want_remaining"] = True

        self._state = self.engine.request(request)
        return self._observation(), self._info()

    def step(self, action):
        if self._state is None:
            raise RuntimeError("step() before reset()")

        request: dict[str, Any] = {
            "cmd": "step",
            "env_id": 0,
            # Discrete.sample() returns np.int64, which json refuses to encode.
            "action": int(action),
        }
        if self._want_remaining:
            request["want_remaining"] = True

        previous = self._state
        self._state = self.engine.request(request)

        reward = self._reward(previous, self._state)
        terminated = bool(self._state["solved"])
        # Truncation is a TimeLimit decision; this class keeps no step budget.
        return self._observation(), reward, terminated, False, self._info()

    def render(self):
        if self.render_mode == "ansi":
            if self._state is None:
                return ""
            return _obs.board_to_text(self._state.get("board", ""))
        # "human" is drawn by the server's own window; nothing to do here.
        return None

    def close(self):
        engine = getattr(self, "engine", None)
        if engine is not None:
            engine.close()

    # ── Masking ──────────────────────────────────────────────────────────────

    def action_masks(self) -> np.ndarray:
        """Legal actions as a boolean array.

        Named for sb3-contrib's ``MaskablePPO``, which looks this method up by
        name on the unwrapped environment.
        """
        if self._state is None:
            raise RuntimeError("action_masks() before reset()")
        return np.asarray(self._state["mask"], dtype=bool)

    # ── Internals ────────────────────────────────────────────────────────────

    def _observation(self):
        return _obs.encode(self._state, self.obs_mode, self.n_slots)

    def _reward(self, before: dict[str, Any], after: dict[str, Any]) -> float:
        return compute_reward(
            before,
            after,
            scheme=self.reward_scheme,
            solve_reward=self.solve_reward,
            illegal_penalty=self.illegal_penalty,
            gamma=self.gamma,
        )

    def _info(self) -> dict[str, Any]:
        return state_info(self._state)

    # ── Oracle ───────────────────────────────────────────────────────────────

    def optimal_actions(self) -> list[int]:
        """An optimal continuation from the current position, as actions.

        This is a reference, not a policy: it is what agent behaviour is scored
        against, and what the tests replay to check that both sides of the
        protocol agree about the rules.
        """
        if self._state is None:
            raise RuntimeError("optimal_actions() before reset()")
        return list(self.engine.request({"cmd": "solve", "env_id": 0})["actions"])

    def __enter__(self) -> "RushHourEnv":
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()

    def __del__(self):
        # Belt and braces; the Engine's own finalizer is the real guarantee.
        try:
            self.close()
        except Exception:  # pragma: no cover - interpreter shutdown
            pass
