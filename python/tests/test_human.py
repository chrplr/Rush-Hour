# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the Apache License, Version 2.0.
# SPDX-License-Identifier: Apache-2.0
"""RushHourHuman-v0: the experiment program's interface as an environment."""

from __future__ import annotations

import time
from collections import deque

import gymnasium as gym
import pytest

import rushhour_gym  # noqa: F401  (registers the ids)
from rushhour_gym import human as H


def bfs(cars):
    """Shortest slide sequence [(slot, dir)] on the 6x6 board, for the tests."""
    geo = [(c["slot"], c["length"], c["horizontal"]) for c in cars]
    start = tuple((c["row"], c["col"]) for c in cars)

    def cells(i, p):
        return [(p[0], p[1] + k) if geo[i][2] else (p[0] + k, p[1]) for k in range(geo[i][1])]

    prev = {start: None}
    q = deque([start])
    while q:
        st = q.popleft()
        if st[0][0] == 2 and st[0][1] + geo[0][1] - 1 == 5:
            path = []
            while prev[st] is not None:
                st, mv = prev[st]
                path.append(mv)
            return path[::-1]
        occ_all = {c for i, p in enumerate(st) for c in cells(i, p)}
        for i, (slot, _length, hz) in enumerate(geo):
            occ = occ_all - set(cells(i, st[i]))
            for d in (-1, 1):
                r, c = st[i]
                nst_i = (r, c + d) if hz else (r + d, c)
                new = cells(i, nst_i)
                if all(0 <= a < 6 and 0 <= b < 6 for a, b in new) and not any(x in occ for x in new):
                    nst = st[:i] + (nst_i,) + st[i + 1:]
                    if nst not in prev:
                        prev[nst] = (st, (slot, d))
                        q.append(nst)
    raise AssertionError("unsolvable")


def solve(env):
    """Play the optimal solution through the meta-actions; return the last info."""
    u = env.unwrapped
    info = None
    for slot, d in bfs(u._cars):
        for _ in range(20):
            if u._selected == slot:
                break
            env.step(H.SELECT_NEXT)
        assert u._selected == slot
        _obs, _r, terminated, _tr, info = env.step(H.MOVE_FORWARD if d > 0 else H.MOVE_BACK)
        assert info["moved"], info["event"]
    assert terminated
    return info


@pytest.fixture(scope="module")
def binary():
    from rushhour_gym import BinaryNotFound, find_binary

    try:
        return find_binary()
    except BinaryNotFound as exc:
        pytest.skip(f"no rushhour-env binary: {exc}")


@pytest.fixture
def paced_env(binary):
    """Fresh per test: a paced env counts trials, so a second reset on a shared
    one would rightly show the ready screen."""
    env = gym.make("RushHourHuman-v0", binary=binary, puzzle_order="library",
                   iti=0.05, solved_feedback=0.05)
    yield env
    env.close()


def test_registered_without_a_step_budget():
    assert gym.spec("RushHourHuman-v0").max_episode_steps is None


def test_library_order_and_the_first_trial_flow(paced_env):
    env, u = paced_env, paced_env.unwrapped
    _obs, info = env.reset(seed=0)
    assert info["puzzle_index"] == 0 and info["event"] == "trial_start"
    assert u.phase == H.PHASE_ITI, "the first puzzle gets the blank interval, not a ready screen"
    frame = env.render()
    assert frame.shape == (H.HEIGHT, H.WIDTH, 3) and tuple(frame[5, 5]) == H._BG
    time.sleep(0.06)
    env.render()
    assert u.phase == H.PHASE_PLAY


def test_selection_is_reported_with_the_car_and_arrows_drawn(paced_env):
    env, u = paced_env, paced_env.unwrapped
    env.reset(seed=0)
    time.sleep(0.06)
    env.render()
    # Movable-only: RIGHT lands on a car that can move, and says which.
    _o, _r, _t, _tr, info = env.step(H.SELECT_NEXT)
    car = u._selected_car()
    assert info["event"] == "select" and info["car"] == car["label"]
    assert info["env_action"] == H.NOOP and (info["from_row"], info["from_col"]) == (car["row"], car["col"])
    assert any(u._can_move(car))
    # The frame shows a white plate around it and an arrow tip in white.
    frame = env.render()
    x0, y0, x1, y1 = H._car_rect(car)
    assert tuple(frame[int(y0) + 3, int((x0 + x1) / 2)]) == H._SELECT
    back, forward = u._can_move(car)
    pts = H._arrow_points(car, 1 if forward else -1)
    cx, cy = sum(x for x, _ in pts) / 3, sum(y for _, y in pts) / 3   # the centroid is inside
    assert tuple(frame[int(cy), int(cx)]) == H._ARROW


def test_solving_ends_the_trial_with_the_results_columns(paced_env):
    env, u = paced_env, paced_env.unwrapped
    _obs, first = env.reset(seed=0)
    time.sleep(0.06)
    env.render()
    info = solve(env)
    assert info["event"] == "trial_end" and info["solved"] is True
    assert info["trial_ms"] >= 0 and info["n_slides"] >= first["min_moves"]
    assert info["car"] == "A" and info["orientation"] == "H" and info["to_col"] == 4
    assert u.phase == H.PHASE_SOLVED
    solved_frame = env.render()
    assert solved_frame is u._solved_frame


def test_hold_then_ready_then_grace_then_interval(paced_env):
    env, u = paced_env, paced_env.unwrapped
    env.reset(seed=0)
    time.sleep(0.06)
    env.render()
    solve(env)
    solved_frame = env.render()
    _obs, info = env.reset(seed=1)
    assert info["puzzle_index"] == 1
    assert u.phase == H.PHASE_SOLVED and (env.render() == solved_frame).all()
    time.sleep(0.06)
    env.render()
    assert u.phase == H.PHASE_READY
    _o, _r, _t, _tr, info = env.step(H.SELECT_NEXT)
    assert info["event"] == "ignored", "a press right after the ready screen is the previous screen's"
    time.sleep(H.READY_GRACE + 0.05)
    _o, _r, _t, _tr, info = env.step(H.SELECT_NEXT)
    assert info["event"] == "start" and u.phase == H.PHASE_ITI
    time.sleep(0.06)
    env.render()
    assert u.phase == H.PHASE_PLAY and u._t_ms() >= 0


def test_unpaced_draws_from_the_pool_like_the_agent_env(binary):
    env = gym.make("RushHourHuman-v0", binary=binary, min_moves_range=(0, 12))
    try:
        u = env.unwrapped
        assert not u.paced
        indices = [env.reset(seed=1000 + s)[1]["puzzle_index"] for s in range(4)]
        assert len(set(indices)) > 1 and u.phase == H.PHASE_PLAY
        assert all(env.unwrapped.env.catalog[i]["min_moves"] <= 12 for i in indices)
    finally:
        env.close()


def test_cycle_skips_stuck_cars_and_movable_only_off_visits_all(binary):
    env = gym.make("RushHourHuman-v0", binary=binary, puzzle_indices=[0], paced=False)
    try:
        u = env.unwrapped
        env.reset(seed=0)
        movable = [c["slot"] for c in u._cars if any(u._can_move(c))]
        seen = []
        for _ in range(len(movable)):
            env.step(H.SELECT_NEXT)
            seen.append(u._selected)
        assert sorted(seen) == sorted(movable), "every movable car once, no stuck one"
    finally:
        env.close()
    env = gym.make("RushHourHuman-v0", binary=binary, puzzle_indices=[0], paced=False,
                   movable_only=False)
    try:
        u = env.unwrapped
        env.reset(seed=0)
        seen = []
        for _ in range(len(u._cars)):
            env.step(H.SELECT_NEXT)
            seen.append(u._selected)
        assert sorted(seen) == sorted(c["slot"] for c in u._cars)
    finally:
        env.close()


def test_default_keys_carry_the_four_button_scheme():
    keys = H.DEFAULT_KEYS
    assert keys["LEFT"] == H.SELECT_PREV and keys["RIGHT"] == H.SELECT_NEXT
    assert keys["UP"] == H.MOVE_BACK and keys["DOWN"] == H.MOVE_FORWARD
    assert keys["1"] == H.MOVE_BACK and keys["4"] == H.SELECT_NEXT
    assert set(keys.values()) <= set(range(len(H.META_ACTIONS)))
