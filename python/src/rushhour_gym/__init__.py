# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the Apache License, Version 2.0.
# SPDX-License-Identifier: Apache-2.0

"""Rush Hour as a Gymnasium environment.

The rules live in Go, in the same code the human experiment runs, and are served
to Python over a pipe. One action is one cell, exactly as one mouse click is one
cell for a participant, so an agent's trace and a participant's trace count the
same events.

    >>> import gymnasium, rushhour_gym          # doctest: +SKIP
    >>> env = gymnasium.make("RushHour-Easy-v0")
    >>> obs, info = env.reset(seed=0)
    >>> obs, reward, terminated, truncated, info = env.step(0)
"""

from __future__ import annotations

import gymnasium

from .baselines import run_optimal, run_random
from .binary import BinaryNotFound, find_binary
from .engine import CommandFailed, Engine, EngineDied, EngineError, ProtocolError
from .env import REWARD_SCHEMES, RushHourEnv
from .obs import OBS_MODES
from .vector_env import RushHourVectorEnv

__all__ = [
    "RushHourEnv",
    "RushHourVectorEnv",
    "Engine",
    "EngineError",
    "EngineDied",
    "ProtocolError",
    "CommandFailed",
    "BinaryNotFound",
    "find_binary",
    "run_optimal",
    "run_random",
    "OBS_MODES",
    "REWARD_SCHEMES",
    "register",
]

__version__ = "0.1.0"

# Step budgets. Rush Hour has no dead ends, so an episode that is not solved
# ends only by truncation, and the budget is the only thing standing between a
# random policy and an endless episode. These are generous relative to the
# optimum — the hardest shipped board needs 51 slides, well over a hundred
# cells — because cutting an episode short teaches nothing.
_SPECS = (
    dict(id="RushHour-v0", max_episode_steps=500, kwargs={}),
    dict(
        id="RushHour-Easy-v0",
        max_episode_steps=200,
        kwargs={"min_moves_range": (0, 12)},
    ),
    dict(id="RushHourFixed-v0", max_episode_steps=100, kwargs={"puzzle": "p02"}),
)


def register() -> None:
    """Register the environment ids. Called on import; safe to call again."""
    for spec in _SPECS:
        if spec["id"] in gymnasium.registry:
            continue
        gymnasium.register(
            entry_point="rushhour_gym.env:RushHourEnv",
            vector_entry_point="rushhour_gym.vector_env:RushHourVectorEnv",
            **spec,
        )


register()
