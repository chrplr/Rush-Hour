# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the MIT License.

"""The end-to-end agreement check.

Three things have to say the same thing about every board: the breadth-first
solver in Go, the action encoding the server publishes, and the environment
Python wraps around it. Replaying an optimal solution through ``env.step`` is
what pins all three together — if the slot table, the direction bit or the move
rules disagreed anywhere, an optimal action would fail to move something.
"""

from __future__ import annotations

import numpy as np
import pytest

from rushhour_gym import RushHourEnv, run_optimal, run_random


@pytest.fixture(scope="module")
def env():
    e = RushHourEnv(include_board=True)
    yield e
    e.close()


def test_the_optimal_solution_solves_every_shipped_puzzle(env):
    for entry in env.catalog:
        result = run_optimal(env, options={"puzzle": entry["name"]})
        assert result["solved"], f"{entry['name']} was not solved"
        assert result["slides"] == entry["min_moves"], (
            f"{entry['name']}: played {result['slides']} moves, "
            f"puzzles.txt declares {entry['min_moves']}"
        )
        # The return under the default scheme is minus the number of cells moved.
        assert result["reward"] == -float(result["steps"])


def test_every_optimal_action_is_legal_when_it_is_played(env):
    for entry in env.catalog[:12]:
        _obs, info = env.reset(options={"puzzle": entry["name"]})
        for action in env.optimal_actions():
            assert info["action_mask"][action], (
                f"{entry['name']}: optimal action {action} is marked illegal"
            )
            _obs, _r, _t, _tr, info = env.step(action)
            assert info["moved"]


def test_the_oracle_replans_from_wherever_the_agent_is(env):
    """optimal_actions() is asked from the current position, not the start, so
    it is usable as a rescue policy after a detour."""
    _obs, info = env.reset(options={"puzzle": "p05"})
    for _ in range(6):
        legal = np.flatnonzero(info["action_mask"])
        _obs, _r, _t, _tr, info = env.step(int(legal[-1]))

    terminated = False
    for action in env.optimal_actions():
        _obs, _r, terminated, _tr, info = env.step(action)
    assert terminated


def test_a_masked_random_walk_solves_the_easy_boards(env):
    """The floor, for contrast. There is no tight bound to assert: with no dead
    ends, a random walker on a hard board can wander for a very long time."""
    for name in ("p01", "p02"):
        result = run_random(env, seed=0, options={"puzzle": name}, max_steps=50_000)
        assert result["solved"], f"{name} unsolved after 50000 random steps"
        assert result["slides"] >= result["min_moves"]
