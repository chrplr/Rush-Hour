# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the MIT License.

from __future__ import annotations

import warnings

import gymnasium
import numpy as np
import pytest
from gymnasium.utils.env_checker import check_env

import rushhour_gym  # noqa: F401 — registers the ids
from rushhour_gym import OBS_MODES, RushHourEnv

CLASSIC = "BCCCoo BoooDo oAAEDo oooEoo FFoEoo ooGGGo"


@pytest.fixture()
def env():
    e = RushHourEnv(puzzle="p02", include_board=True)
    yield e
    e.close()


# ── Conformance ──────────────────────────────────────────────────────────────


@pytest.mark.parametrize("mode", OBS_MODES)
def test_passes_the_gymnasium_checker(mode):
    with RushHourEnv(puzzle="p02", obs_mode=mode) as e:
        with warnings.catch_warnings():
            warnings.simplefilter("error", UserWarning)
            check_env(e, skip_render_check=True)


@pytest.mark.parametrize("mode", OBS_MODES)
def test_observations_stay_inside_their_space(mode):
    with RushHourEnv(min_moves_range=(0, 12), obs_mode=mode) as e:
        obs, _info = e.reset(seed=3)
        assert e.observation_space.contains(obs)
        for _ in range(30):
            obs, *_ = e.step(e.action_space.sample())
            assert e.observation_space.contains(obs)


@pytest.mark.parametrize("env_id", ["RushHour-v0", "RushHour-Easy-v0", "RushHourFixed-v0"])
def test_registered_ids_make(env_id):
    e = gymnasium.make(env_id)
    try:
        obs, info = e.reset(seed=0)
        assert e.observation_space.contains(obs)
        assert e.spec.max_episode_steps is not None, "an unsolved episode must be able to end"
    finally:
        e.close()


# ── Seeding ──────────────────────────────────────────────────────────────────


def test_the_same_seed_gives_the_same_episodes():
    """All randomness is in reset(), on self.np_random, so a run is reproducible
    without the server knowing anything about it."""

    def sequence(seed):
        with RushHourEnv(min_moves_range=(0, 20)) as e:
            names = []
            for i in range(8):
                _obs, info = e.reset(seed=seed if i == 0 else None)
                names.append(info["puzzle"])
            return names

    assert sequence(1234) == sequence(1234)
    assert sequence(1234) != sequence(4321)


# ── Actions and masking ──────────────────────────────────────────────────────


def test_the_mask_predicts_what_moves(env):
    _obs, info = env.reset(seed=0)
    for action in range(env.action_space.n):
        env.reset(seed=0)
        legal = bool(info["action_mask"][action])
        _obs, _r, _t, _tr, step_info = env.step(action)
        assert step_info["moved"] == legal, f"action {action}"


def test_action_masks_method_matches_the_info(env):
    _obs, info = env.reset(seed=0)
    assert np.array_equal(env.action_masks(), info["action_mask"])
    _obs, _r, _t, _tr, info = env.step(int(np.flatnonzero(info["action_mask"])[0]))
    assert np.array_equal(env.action_masks(), info["action_mask"])


def test_the_mask_is_never_empty_before_the_puzzle_is_solved(env):
    """Every move is reversible, so an unsolved board is never stuck. An agent
    that finds no legal action has hit a bug, not a dead end."""
    _obs, info = env.reset(seed=0)
    for _ in range(100):
        legal = np.flatnonzero(info["action_mask"])
        assert legal.size > 0
        _obs, _r, terminated, _tr, info = env.step(int(legal[0]))
        if terminated:
            break


def test_an_illegal_action_wastes_a_step_without_changing_the_board(env):
    _obs, info = env.reset(seed=0)
    blocked = int(np.flatnonzero(~info["action_mask"])[0])
    before = info["board"]

    _obs, reward, terminated, truncated, info = env.step(blocked)
    assert info["illegal"] and not info["moved"]
    assert info["board"] == before
    assert not terminated and not truncated
    assert info["n_steps"] == 1, "a wasted step is still a step"
    assert info["n_slides"] == 0
    assert reward == -1.0


def test_slides_collapse_consecutive_steps(env):
    """n_slides is the unit puzzles.txt counts and the unit the human trace
    records; n_steps is the unit a click is measured in."""
    env.reset(seed=0)
    _obs, _r, _t, _tr, info = env.step(12)  # G left
    assert (info["n_steps"], info["n_slides"]) == (1, 1)
    _obs, _r, _t, _tr, info = env.step(12)  # G left again — same slide
    assert (info["n_steps"], info["n_slides"]) == (2, 1)
    _obs, _r, _t, _tr, info = env.step(13)  # G right — a new slide
    assert (info["n_steps"], info["n_slides"]) == (3, 2)


# ── Rewards and termination ──────────────────────────────────────────────────


def test_step_penalty_return_is_minus_the_number_of_steps():
    with RushHourEnv(puzzle="p02") as e:
        e.reset(seed=0)
        total, steps = 0.0, 0
        for action in e.optimal_actions():
            _obs, reward, terminated, _tr, info = e.step(action)
            total += reward
            steps += 1
        assert terminated
        assert total == -float(steps) == -float(info["n_steps"])


def test_sparse_pays_only_at_the_exit():
    with RushHourEnv(puzzle="p02", reward_scheme="sparse") as e:
        e.reset(seed=0)
        rewards = [e.step(a)[1] for a in e.optimal_actions()]
        assert rewards[-1] == 1.0
        assert set(rewards[:-1]) == {0.0}


def test_shaping_leaves_the_optimal_path_the_best_one():
    """Potential-based shaping is policy-invariant, so the shaped return of an
    optimal path must still beat that of a detour."""
    with RushHourEnv(puzzle="p02", reward_scheme="shaped", gamma=1.0) as e:
        e.reset(seed=0)
        optimal = sum(e.step(a)[1] for a in e.optimal_actions())

        _obs, info = e.reset(seed=0)
        detour = 0.0
        # One wasted move and its undo, then play optimally from there.
        away = int(np.flatnonzero(info["action_mask"])[0])
        detour += e.step(away)[1]
        detour += e.step(away ^ 1)[1]
        detour += sum(e.step(a)[1] for a in e.optimal_actions())
        assert optimal > detour


def test_terminated_means_solved_and_nothing_else():
    with RushHourEnv(puzzle="p02") as e:
        e.reset(seed=0)
        actions = e.optimal_actions()
        for action in actions[:-1]:
            _obs, _r, terminated, truncated, _info = e.step(action)
            assert not terminated and not truncated
        _obs, _r, terminated, truncated, info = e.step(actions[-1])
        assert terminated and not truncated
        assert info["is_success"]


def test_the_bare_env_never_truncates():
    """A step budget is a TimeLimit decision; keeping it in one place means the
    registered ids own it and this class does not."""
    with RushHourEnv(puzzle="p02") as e:
        _obs, info = e.reset(seed=0)
        for _ in range(300):
            action = int(np.flatnonzero(info["action_mask"])[0])
            _obs, _r, terminated, truncated, info = e.step(action)
            assert not truncated
            if terminated:
                e.reset()
                _obs, info = e.reset()


def test_the_registered_id_truncates():
    e = gymnasium.make("RushHourFixed-v0")  # 100 steps
    try:
        e.reset(seed=0)
        for step in range(1, 200):
            # Action 30 is a padding slot: it never moves anything, so the
            # episode can only end by running out of time.
            _obs, _r, terminated, truncated, _info = e.step(30)
            assert not terminated
            if truncated:
                assert step == e.spec.max_episode_steps
                break
        else:
            pytest.fail("the episode never truncated")
    finally:
        e.close()


# ── Puzzle selection ─────────────────────────────────────────────────────────


def test_min_moves_range_restricts_the_pool():
    with RushHourEnv(min_moves_range=(0, 8)) as e:
        assert e.puzzle_pool
        for name in e.puzzle_pool:
            entry = next(p for p in e.catalog if p["name"] == name)
            assert entry["min_moves"] <= 8


def test_set_puzzle_filter_widens_the_pool():
    with RushHourEnv(min_moves_range=(0, 6)) as e:
        narrow = len(e.puzzle_pool)
        e.set_puzzle_filter(min_moves_range=(0, 20))
        assert len(e.puzzle_pool) > narrow


def test_an_impossible_selection_is_refused():
    with pytest.raises(ValueError):
        RushHourEnv(min_moves_range=(1000, 2000)).close()
    with pytest.raises(ValueError):
        RushHourEnv(puzzle="not a puzzle").close()


def test_reset_options_choose_the_board(env):
    _obs, info = env.reset(seed=0, options={"puzzle": "p05"})
    assert info["puzzle"] == "p05"
    _obs, info = env.reset(seed=0, options={"puzzle_index": 0})
    assert info["puzzle_index"] == 0
    _obs, info = env.reset(seed=0, options={"spec": CLASSIC})
    assert info["puzzle"] == "custom"
    assert info["n_cars"] == 7


def test_a_bad_spec_is_reported_rather_than_crashing(env):
    from rushhour_gym import CommandFailed

    with pytest.raises(CommandFailed) as excinfo:
        # Already solved: the parser refuses those on purpose, and the server
        # must say so instead of panicking.
        env.reset(options={"spec": "oooooo oooooo ooooAA oooooo oooooo oooooo"})
    assert excinfo.value.kind == "bad_spec"


# ── Rendering ────────────────────────────────────────────────────────────────


def test_ansi_render_shows_the_board():
    with RushHourEnv(puzzle="p02", render_mode="ansi") as e:
        e.reset(seed=0)
        text = e.render()
        assert text.count("\n") == 5
        assert "A" in text
