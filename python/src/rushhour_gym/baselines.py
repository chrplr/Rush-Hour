# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the MIT License.

"""Reference policies to compare a learned agent against.

Neither of these learns anything. The optimal one is the ceiling any curve
should be read against, and the source of behaviour-cloning data; the masked
random one is the floor.
"""

from __future__ import annotations

from typing import Any

import numpy as np

__all__ = ["run_optimal", "run_random", "EpisodeResult"]


class EpisodeResult(dict):
    """One episode's outcome: solved, steps, slides, reward, puzzle."""

    __getattr__ = dict.__getitem__  # type: ignore[assignment]


def run_optimal(env, *, seed: int | None = None, options: dict[str, Any] | None = None):
    """Play one episode along an optimal path.

    The plan is taken once, at reset, and followed to the end: the search is
    deterministic and the board only changes the way the plan expects.
    """
    _obs, info = env.reset(seed=seed, options=options)
    total = 0.0
    terminated = truncated = False

    for action in env.unwrapped.optimal_actions():
        _obs, reward, terminated, truncated, info = env.step(action)
        total += reward
        if terminated or truncated:
            break

    return EpisodeResult(
        solved=bool(terminated),
        truncated=bool(truncated),
        steps=info["n_steps"],
        slides=info["n_slides"],
        min_moves=info["min_moves"],
        reward=total,
        puzzle=info["puzzle"],
    )


def run_random(
    env,
    *,
    seed: int | None = None,
    options: dict[str, Any] | None = None,
    max_steps: int = 10_000,
    masked: bool = True,
):
    """Play one episode at random, optionally restricted to legal actions.

    Unmasked, most actions do nothing and the walk barely moves; masked, it is a
    random walk on the state graph. Either way there is no bound worth
    asserting: Rush Hour has no dead ends, so a random walker on a hard board
    can wander for a very long time without ever being stuck.
    """
    _obs, info = env.reset(seed=seed, options=options)
    rng = np.random.default_rng(seed)
    total = 0.0
    terminated = truncated = False

    for _ in range(max_steps):
        if masked:
            legal = np.flatnonzero(info["action_mask"])
            action = int(rng.choice(legal))
        else:
            action = int(rng.integers(env.action_space.n))
        _obs, reward, terminated, truncated, info = env.step(action)
        total += reward
        if terminated or truncated:
            break

    return EpisodeResult(
        solved=bool(terminated),
        truncated=bool(truncated),
        steps=info["n_steps"],
        slides=info["n_slides"],
        min_moves=info["min_moves"],
        reward=total,
        puzzle=info["puzzle"],
    )
