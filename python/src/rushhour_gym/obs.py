# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the MIT License.

"""Turning a server state into an observation.

The server sends the board as a slot-indexed array of vehicle geometries and
nothing else. The tensor an agent sees is built here, in numpy, so that trying a
different encoding costs an edit rather than a rebuild of the Go binary.

Whichever encoding is used has to answer one question: *which vehicle does
action 2k refer to?* An observation that cannot be read that way leaves the
agent unable to connect what it sees to what it can do, so every mode below
keeps slot identity visible.
"""

from __future__ import annotations

from typing import Any

import numpy as np
from gymnasium import spaces

__all__ = ["OBS_MODES", "space_for", "encode", "GRID"]

GRID = 6

OBS_MODES = ("planes", "grid", "cars", "dict")

# Row of the exit, matching rush.TargetRow. Checked against the handshake.
TARGET_ROW = 2


def space_for(mode: str, n_slots: int) -> spaces.Space:
    """The observation space for one of :data:`OBS_MODES`."""
    if mode == "planes":
        # One binary plane per slot, plus orientation and the exit corridor.
        return spaces.Box(0, 1, shape=(n_slots + 3, GRID, GRID), dtype=np.uint8)
    if mode == "grid":
        # 0 is empty, slot k is k+1. Compact and readable, but the cell value is
        # a name rather than a quantity, which a network has to unlearn.
        return spaces.Box(0, n_slots, shape=(GRID, GRID), dtype=np.uint8)
    if mode == "cars":
        # row, col, length, horizontal — no spatial structure at all, which
        # suits a plain MLP baseline and nothing else.
        return spaces.Box(0, GRID, shape=(n_slots, 4), dtype=np.uint8)
    if mode == "dict":
        return spaces.Dict(
            {
                "grid": space_for("grid", n_slots),
                "cars": space_for("cars", n_slots),
                "action_mask": spaces.MultiBinary(2 * n_slots),
            }
        )
    raise ValueError(f"unknown obs_mode {mode!r}, expected one of {OBS_MODES}")


def encode(state: dict[str, Any], mode: str, n_slots: int) -> Any:
    """Build the observation for one server state."""
    cars = state["cars"]
    if mode == "planes":
        return _planes(cars, n_slots)
    if mode == "grid":
        return _grid(cars, n_slots)
    if mode == "cars":
        return _cars(cars, n_slots)
    if mode == "dict":
        return {
            "grid": _grid(cars, n_slots),
            "cars": _cars(cars, n_slots),
            "action_mask": np.asarray(state["mask"], dtype=np.int8),
        }
    raise ValueError(f"unknown obs_mode {mode!r}, expected one of {OBS_MODES}")


def _cells(row: int, col: int, length: int, horizontal: int):
    """The cells one vehicle occupies. A padding slot has length 0 and no cells."""
    for i in range(length):
        yield (row, col + i) if horizontal else (row + i, col)


def _planes(cars: list[list[int]], n_slots: int) -> np.ndarray:
    out = np.zeros((n_slots + 3, GRID, GRID), dtype=np.uint8)
    horizontal_plane = out[n_slots]
    vertical_plane = out[n_slots + 1]

    for slot, (row, col, length, horizontal, _is_target) in enumerate(cars):
        orientation = horizontal_plane if horizontal else vertical_plane
        for r, c in _cells(row, col, length, horizontal):
            out[slot, r, c] = 1
            orientation[r, c] = 1

    # The corridor the red car still has to cross: everything to its right on
    # the exit row. Empty exactly when the puzzle is solved.
    red_row, red_col, red_len, _h, _t = cars[0]
    if red_len:
        out[n_slots + 2, red_row, red_col + red_len :] = 1
    return out


def _grid(cars: list[list[int]], n_slots: int) -> np.ndarray:
    out = np.zeros((GRID, GRID), dtype=np.uint8)
    for slot, (row, col, length, horizontal, _is_target) in enumerate(cars):
        for r, c in _cells(row, col, length, horizontal):
            out[r, c] = slot + 1
    return out


def _cars(cars: list[list[int]], n_slots: int) -> np.ndarray:
    out = np.zeros((n_slots, 4), dtype=np.uint8)
    for slot, (row, col, length, horizontal, _is_target) in enumerate(cars):
        out[slot] = (row, col, length, horizontal)
    return out


def board_to_text(board: str) -> str:
    """Render the six-line notation with the exit marked, for ``render_mode="ansi"``."""
    lines = board.split("\n")
    marked = [
        line + (" <" if i == TARGET_ROW else "  ") for i, line in enumerate(lines)
    ]
    return "\n".join(marked)
