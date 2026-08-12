# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the Apache License, Version 2.0.
# SPDX-License-Identifier: Apache-2.0

"""The transport: handshake, framing, and above all not leaving processes behind."""

from __future__ import annotations

import gc
import subprocess

import pytest

from rushhour_gym.binary import find_binary
from rushhour_gym.engine import PROTOCOL, CommandFailed, Engine, EngineDied


@pytest.fixture()
def engine():
    eng = Engine(find_binary(), ["-board"])
    yield eng
    eng.close()


def test_handshake_describes_the_spaces(engine):
    meta = engine.meta
    assert meta["protocol"] == PROTOCOL
    assert meta["grid_size"] == 6
    assert meta["n_actions"] == 2 * meta["max_vehicles"]
    assert meta["puzzles"], "the catalogue should not be empty"
    # The action space has to be able to name every vehicle of every board.
    assert max(p["n_cars"] for p in meta["puzzles"]) <= meta["max_vehicles"]


def test_requests_and_answers_stay_paired(engine):
    engine.request({"cmd": "reset", "puzzle": "p02"})
    for _ in range(20):
        state = engine.request({"cmd": "step", "action": 12})
        assert state["ok"]
        assert "cars" in state and "mask" in state


def test_a_refused_command_raises_but_leaves_the_engine_usable(engine):
    with pytest.raises(CommandFailed) as excinfo:
        engine.request({"cmd": "reset", "puzzle": "there is no such puzzle"})
    assert excinfo.value.kind == "no_such_puzzle"

    # The stream is still in step: a failed command is an answer like any other.
    assert engine.request({"cmd": "reset", "puzzle": "p02"})["puzzle"] == "p02"


def test_close_reaps_the_child(engine):
    proc = engine._proc
    engine.close()
    assert proc.poll() is not None, "the child outlived close()"
    engine.close()  # idempotent


def test_dropping_the_engine_reaps_the_child():
    """No explicit close: the finalizer has to do it, or a training script that
    raises would leak one process per environment."""
    engine = Engine(find_binary())
    proc = engine._proc
    del engine
    gc.collect()
    assert proc.wait(timeout=5) is not None


def test_closing_stdin_is_enough_to_stop_the_server():
    """The backstop for a Python interpreter that dies without cleaning up: the
    pipe closes and the child exits by itself."""
    proc = subprocess.Popen(
        [find_binary()], stdin=subprocess.PIPE, stdout=subprocess.PIPE
    )
    proc.stdin.close()
    assert proc.wait(timeout=10) == 0
    proc.stdout.close()


def test_a_dead_child_raises_instead_of_hanging():
    engine = Engine(find_binary())
    engine._proc.kill()
    engine._proc.wait()
    with pytest.raises(EngineDied):
        engine.request({"cmd": "reset", "puzzle": "p02"})
    # Having died, it stays dead rather than pretending to work.
    with pytest.raises(EngineDied):
        engine.request({"cmd": "hello"})
