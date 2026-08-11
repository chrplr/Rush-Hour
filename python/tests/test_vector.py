# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the MIT License.

"""The batched environment has to be indistinguishable from n separate ones.

That equivalence is the whole justification for the batch protocol: if it can
diverge, the saving in round trips is not worth the second code path.
"""

from __future__ import annotations

import gymnasium
import numpy as np
import pytest
from gymnasium.vector import SyncVectorEnv

import rushhour_gym  # noqa: F401 — registers the ids
from rushhour_gym import RushHourEnv, RushHourVectorEnv

NUM_ENVS = 4
SEED = 7


def _make_sync():
    return SyncVectorEnv(
        [lambda: RushHourEnv(min_moves_range=(0, 12)) for _ in range(NUM_ENVS)]
    )


@pytest.fixture()
def envs():
    e = RushHourVectorEnv(NUM_ENVS, min_moves_range=(0, 12))
    yield e
    e.close()


def test_it_matches_sync_vector_env_step_for_step(envs):
    sync = _make_sync()
    try:
        batched_obs, batched_info = envs.reset(seed=SEED)
        sync_obs, sync_info = sync.reset(seed=SEED)

        assert np.array_equal(batched_obs, sync_obs)
        assert list(batched_info["puzzle"]) == list(sync_info["puzzle"])

        rng = np.random.default_rng(0)
        for step in range(120):
            actions = rng.integers(0, envs.single_action_space.n, size=NUM_ENVS)

            b_obs, b_r, b_term, b_trunc, b_info = envs.step(actions)
            s_obs, s_r, s_term, s_trunc, s_info = sync.step(actions)

            assert np.array_equal(b_obs, s_obs), f"observations differ at step {step}"
            assert np.array_equal(b_r, s_r), f"rewards differ at step {step}"
            assert np.array_equal(b_term, s_term), f"terminations differ at step {step}"
            assert np.array_equal(b_trunc, s_trunc), f"truncations differ at step {step}"
            assert list(b_info["puzzle"]) == list(s_info["puzzle"])
    finally:
        sync.close()


def test_autoreset_happens_on_the_step_after_termination():
    """AutoresetMode.NEXT_STEP: the solving step returns the final observation,
    and the next one discards its action and returns a fresh board."""
    with RushHourVectorEnv(1, puzzle="p02") as envs:
        _obs, _info = envs.reset(seed=0)
        # Drive the single board along an optimal path.
        plan = envs.engine.request({"cmd": "solve", "env_id": 0})["actions"]

        for action in plan:
            _obs, reward, term, trunc, info = envs.step([action])
        assert term[0] and not trunc[0]
        assert info["is_success"][0]
        solved_steps = info["n_steps"][0]

        # The next step is the reset: its action is ignored, no reward.
        _obs, reward, term, trunc, info = envs.step([31])
        assert reward[0] == 0.0
        assert not term[0] and not trunc[0]
        assert info["n_steps"][0] == 0 < solved_steps


def test_truncation_ends_an_episode_that_is_going_nowhere():
    budget = 20
    with RushHourVectorEnv(2, puzzle="p02", max_episode_steps=budget) as envs:
        envs.reset(seed=0)
        for step in range(1, budget + 1):
            # A padding slot: it never moves anything.
            _obs, _r, term, trunc, _info = envs.step([31, 31])
            assert not term.any()
            if step < budget:
                assert not trunc.any(), f"truncated early, at step {step}"
        assert trunc.all()


def test_boards_do_not_interfere(envs):
    envs.reset(seed=SEED)
    before = envs.action_masks().copy()

    # Move only board 0; action 31 is a padding slot and leaves the others alone.
    _obs, _r, _term, _trunc, info = envs.step(
        [int(np.flatnonzero(before[0])[0]), 31, 31, 31]
    )
    after = envs.action_masks()

    assert info["moved"][0] and not info["moved"][1:].any()
    for i in range(1, NUM_ENVS):
        assert np.array_equal(after[i], before[i]), f"board {i} changed"


def test_make_vec_builds_the_batched_env():
    envs = gymnasium.make_vec("RushHour-Easy-v0", num_envs=3)
    try:
        assert isinstance(envs.unwrapped, RushHourVectorEnv)
        assert envs.num_envs == 3
        # The registered step budget has to reach the vector env, or nothing
        # would ever truncate.
        assert envs.unwrapped.max_episode_steps == 200
        obs, _info = envs.reset(seed=0)
        assert envs.observation_space.contains(obs)
    finally:
        envs.close()
