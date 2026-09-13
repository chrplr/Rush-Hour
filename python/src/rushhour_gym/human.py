# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the Apache License, Version 2.0.
# SPDX-License-Identifier: Apache-2.0
"""Rush Hour as a person plays it: ``RushHourHuman-v0``.

:class:`RushHourEnv` is an agent's environment -- ``Discrete(32)`` names a
vehicle and a direction outright, and ``render`` is a letter grid. A person
with four buttons cannot press "slot 7, right". This module is the experiment
program's interface (``main.go``, ``rushinput``, ``rushui``) as a Gymnasium
environment, so that a harness which presents games to participants -- an fMRI
harness, say -- needs nothing but a keymap:

* **Actions** are the eight meta-actions of ``rushinput.Action``: choose the
  car above / below / left / right (spatial, what a gamepad d-pad does), the
  previous / next car (the four-button box and the arrow keys), and slide the
  chosen car back or forward. Choosing is local -- no engine step -- and a
  slide becomes the engine's ``Discrete`` action, reported as ``env_action``.
  Selection follows ``rush.Board``: ``Neighbour`` for the spatial presses,
  ``Cycle`` for the sequential ones, and -- ``movable_only``, on by default as
  in the program -- both skip cars that cannot move at all.
* **Rendering** (``render_mode="rgb_array"``) is ``rushui``'s picture: the
  1024x768 board, the palette, a white outline on the chosen car and a white
  arrow at each end it can still slide towards (from the engine's mask), and
  the status line. Text needs pygame or Pillow; without either the frames
  carry no text.
* **Trial flow**, when ``paced``: before every puzzle but the first a
  self-paced "press any key" screen, then a blank interval
  (``iti``), the board, and after a solve a "PUZZLE SOLVED!" hold
  (``solved_feedback``). Time-driven transitions happen in ``render``, so a
  harness must keep calling it between key presses (an idle redraw); keys in
  a screen that takes none are recorded as ``ignored``, and a press within
  ``READY_GRACE`` of the ready screen appearing is ignored too, as the program
  discards a press meant for the previous screen.
* **Puzzles**: ``puzzle_order="library"`` plays the library in its order
  (easiest first, what the program's ``-n`` presents), ``puzzle_indices`` an
  explicit list; with neither, the seeded draw from the pool, as the agent
  env does. ``paced`` defaults to on exactly when a sequence is given.
* **info** carries the columns of the program's results file
  (``internal/rushlog``): ``event`` (``trial_start``, ``start``, ``select``,
  ``move``, ``blocked``, ``trial_end``, ``ignored``, ``noop``), ``puzzle``,
  ``puzzle_index``, ``min_moves``, ``car``, ``orientation``,
  ``from_row``/``from_col``/``to_row``/``to_col``, ``n_slides``, ``solved``,
  ``t_ms`` (since the board appeared), ``trial_ms`` (at ``trial_end``),
  plus ``env_action``, ``selected``, ``moved``, ``illegal``, ``phase``. The
  results file's ``trial`` counter is not among them: which puzzle is which
  trial is the harness's to know (it built the sequence), so neither the
  board nor the ready screen numbers the puzzle.

Episodes end only by solving; there is no step budget here, so
``RushHourHuman-v0`` registers with none -- a participant on a hard puzzle
must not be cut off.
"""

from __future__ import annotations

import os
import time
from typing import Any, Sequence

import gymnasium
import numpy as np
from gymnasium import spaces

from .env import RushHourEnv

__all__ = [
    "RushHourHumanEnv",
    "META_ACTIONS",
    "DEFAULT_KEYS",
    "SELECT_UP", "SELECT_DOWN", "SELECT_LEFT", "SELECT_RIGHT",
    "SELECT_PREV", "SELECT_NEXT", "MOVE_BACK", "MOVE_FORWARD", "NOOP",
]

# ── Meta-actions (rushinput.Action) ──────────────────────────────────────────

SELECT_UP, SELECT_DOWN, SELECT_LEFT, SELECT_RIGHT = 0, 1, 2, 3
SELECT_PREV, SELECT_NEXT = 4, 5
MOVE_BACK, MOVE_FORWARD = 6, 7
#: A step with no press: nothing happens, nothing is selected.
NOOP = -1
META_ACTIONS = ("up", "down", "left", "right", "prev", "next", "back", "forward")

#: rushinput.DefaultMap's keyboard, by key name: the four-button box on the
#: arrows (left/right choose, up/down slide), 1 2 3 4 as the box sends them,
#: and , . for the slides.
DEFAULT_KEYS: dict[str, int] = {
    "LEFT": SELECT_PREV, "RIGHT": SELECT_NEXT,
    "UP": MOVE_BACK, "DOWN": MOVE_FORWARD,
    "3": SELECT_PREV, "4": SELECT_NEXT,
    "1": MOVE_BACK, "2": MOVE_FORWARD,
    "COMMA": MOVE_BACK, "PERIOD": MOVE_FORWARD,
}

# rush.Board.Neighbour's scoring constant (select.go: 2 * GridSize).
_GRID = 6
_SIDEWAYS_PENALTY = 2 * _GRID

# ── rushui's look (internal/rushui/render.go), pixel coordinates, y down ─────

WIDTH, HEIGHT = 1024, 768
_TILE = 90
_BOARD_X0 = WIDTH // 2 - 3 * _TILE          # 242: centred horizontally
_BOARD_Y0 = HEIGHT // 2 - (3 * _TILE + 40)  # 74: shifted up for the status line
_CAR_INSET = 4
_EXIT_W = 10
_STATUS_Y = _BOARD_Y0 + 6 * _TILE + 40
_TARGET_ROW = 2
_BG = (240, 240, 240)
_GRID_COLOR = (180, 180, 180)
_EXIT = (220, 50, 50)
_TEXT = (30, 30, 30)
_OUTLINE = (0, 0, 0)
_SELECT = (255, 255, 255)
_ARROW = (255, 255, 255)
# rushui.carColors: the red target, then by the car's alphabetical index.
_CAR_COLORS = [
    (220, 50, 50), (50, 120, 220), (50, 180, 80), (220, 160, 40),
    (140, 60, 200), (200, 200, 50), (50, 180, 200),
]
_ARROW_TIP_INSET, _ARROW_LEN, _ARROW_HALF = 0.20, 0.34, 0.16
_FONT_SIZE = 28

# ── Trial flow (main.go) ─────────────────────────────────────────────────────

PHASE_READY, PHASE_ITI, PHASE_PLAY, PHASE_SOLVED = "ready", "iti", "play", "solved"
#: Seconds after the ready screen appears during which a press is ignored:
#: the program drains presses meant for the previous screen.
READY_GRACE = 0.25


class RushHourHumanEnv(gymnasium.Env):
    """Rush Hour with the experiment program's controls, picture and trial flow.

    Wraps :class:`RushHourEnv` (``obs_mode="cars"``): the rules stay in the Go
    engine, this class adds what a person needs. See the module docstring.
    """

    metadata = {"render_modes": ["rgb_array"], "render_fps": 60}

    def __init__(
        self,
        puzzle: str | None = None,
        puzzle_indices: Sequence[int] | None = None,
        min_moves_range: tuple[int, int] | None = None,
        *,
        puzzle_order: str | None = None,
        movable_only: bool = True,
        paced: bool | None = None,
        iti: float = 0.8,
        solved_feedback: float = 1.2,
        render_mode: str | None = "rgb_array",
        binary: str | os.PathLike[str] | None = None,
        puzzle_file: str | os.PathLike[str] | None = None,
        csv_path: str | os.PathLike[str] | None = None,
        subject_id: int = 0,
    ):
        if puzzle_order not in (None, "library"):
            raise ValueError(f"puzzle_order must be None or 'library', got {puzzle_order!r}")
        if render_mode is not None and render_mode not in self.metadata["render_modes"]:
            raise ValueError(f"unknown render_mode {render_mode!r}")
        self.render_mode = render_mode

        self.env = RushHourEnv(
            puzzle=puzzle,
            puzzle_indices=puzzle_indices,
            min_moves_range=min_moves_range,
            obs_mode="cars",
            binary=binary,
            puzzle_file=puzzle_file,
            csv_path=csv_path,
            subject_id=subject_id,
        )
        self.action_space = spaces.Discrete(len(META_ACTIONS))
        self.observation_space = self.env.observation_space

        self._sequence: list[int] | None = (
            list(puzzle_indices) if puzzle_indices
            else list(range(len(self.env.catalog))) if puzzle_order == "library"
            else None
        )
        self.movable_only = bool(movable_only)
        self.paced = bool(self._sequence is not None if paced is None else paced)
        self.iti = float(iti)
        self.solved_feedback = float(solved_feedback)

        self._episode = -1
        self._cars: list[dict[str, Any]] = []
        self._by_slot: dict[int, dict[str, Any]] = {}
        self._mask: np.ndarray | None = None
        self._labels = ""
        self._selected = 0
        self._last_obs: Any = None
        self._last_info: dict[str, Any] = {}
        self._event = ""
        self._trial_ms = -1.0
        self._phase = PHASE_PLAY
        self._phase_t0 = time.perf_counter()
        self._trial_onset: float | None = None
        self._solved_frame: np.ndarray | None = None
        self._holding = False   # showing the previous puzzle's solved picture
        self._text: Any = None

    # ── Gymnasium API ────────────────────────────────────────────────────────

    def reset(self, *, seed: int | None = None, options: dict[str, Any] | None = None):
        super().reset(seed=seed)
        solved_before = self._phase == PHASE_SOLVED
        self._episode += 1

        inner_options = dict(options or {})
        if self._sequence is not None and not (
            "puzzle" in inner_options or "puzzle_index" in inner_options
        ):
            inner_options["puzzle_index"] = self._sequence[self._episode % len(self._sequence)]
        obs, info = self.env.reset(seed=seed, options=inner_options or None)
        self._ingest(obs, info)
        self._selected = 0  # the red car, as the program starts every trial
        self._last_obs, self._last_info = obs, info
        self._event = "trial_start"
        self._trial_ms = -1.0
        self._trial_onset = None
        self._holding = False

        if not self.paced:
            self._begin_trial()
        elif solved_before and self._solved_frame is not None:
            # Keep the solved picture up for the rest of its hold -- timed
            # from the solving move, so the phase timer is not restarted --
            # then render() moves on to the ready screen.
            self._holding = True
        elif self._episode > 0:
            self._set_phase(PHASE_READY)
        else:
            # The first puzzle has no ready screen -- the instructions asked
            # for that press -- but does get the blank interval.
            self._set_phase(PHASE_ITI)
        return obs, self._info(info)

    def step(self, action):
        meta = int(action)
        self._advance()
        if self._phase == PHASE_READY:
            if meta >= 0 and time.perf_counter() - self._phase_t0 >= READY_GRACE:
                self._set_phase(PHASE_ITI)
                self._event = "start"
            else:
                self._event = "ignored"
            return self._ui_only()
        if self._phase != PHASE_PLAY:
            self._event = "ignored"
            return self._ui_only()

        if meta < 0:
            self._event = "noop"
            return self._ui_only()
        if meta <= SELECT_NEXT:
            self._do_select(meta)
            self._event = "select"
            return self._ui_only()

        discrete = int(self._selected) * 2 + (0 if meta == MOVE_BACK else 1)
        obs, reward, terminated, truncated, info = self.env.step(discrete)
        self._ingest(obs, info)
        # Keep the highlight on the car just acted on when the engine names a
        # slot (a blocked attempt still names one).
        slot = info.get("slot")
        if isinstance(slot, int) and slot in self._by_slot:
            self._selected = slot
        info = dict(info)
        info["env_action"] = discrete
        self._last_obs, self._last_info = obs, info
        self._event = "move" if info.get("moved") else "blocked"
        if terminated:
            self._event = "trial_end"
            self._trial_ms = self._t_ms()
            self._set_phase(PHASE_SOLVED)
            self._solved_frame = None  # drawn by the next render()
        return obs, float(reward), bool(terminated), bool(truncated), self._info(info)

    def render(self):
        if self.render_mode != "rgb_array":
            return None
        self._advance()
        if self._phase == PHASE_SOLVED:
            if self._solved_frame is None:
                self._solved_frame = self._draw_board(None, "PUZZLE SOLVED!")
            return self._solved_frame
        if self._phase == PHASE_READY:
            return self._draw_screen(["Press any key or button to start."])
        if self._phase == PHASE_ITI:
            return self._draw_screen([])
        return self._draw_board(self._selected_car(), "free the RED car")

    def close(self):
        self.env.close()

    # ── Trial flow ───────────────────────────────────────────────────────────

    @property
    def phase(self) -> str:
        """``ready``, ``iti``, ``play`` or ``solved`` (time-driven ones advance in render)."""
        self._advance()
        return self._phase

    def _set_phase(self, phase: str) -> None:
        self._phase = phase
        self._phase_t0 = time.perf_counter()

    def _begin_trial(self) -> None:
        self._set_phase(PHASE_PLAY)
        self._trial_onset = self._phase_t0

    def _advance(self) -> None:
        elapsed = time.perf_counter() - self._phase_t0
        if self._phase == PHASE_ITI and elapsed >= self.iti:
            self._begin_trial()
        elif self._phase == PHASE_SOLVED and self._holding and elapsed >= self.solved_feedback:
            # The hold is over and the next puzzle is loaded: its ready screen.
            self._holding = False
            self._set_phase(PHASE_READY)

    def _t_ms(self) -> float:
        if self._trial_onset is None or self._phase not in (PHASE_PLAY, PHASE_SOLVED):
            return -1.0
        ref = self._phase_t0 if self._phase == PHASE_SOLVED else time.perf_counter()
        return (ref - self._trial_onset) * 1000.0

    # ── Selection (rush.Board.Neighbour / CycleAmong) ────────────────────────

    def _ui_only(self):
        info = dict(self._last_info)
        info["env_action"] = NOOP
        info["moved"] = False
        info["illegal"] = False
        info["slot"] = -1
        return self._last_obs, 0.0, False, False, self._info(info)

    def _ingest(self, obs: Any, info: dict[str, Any]) -> None:
        self._labels = str(info.get("labels") or self._labels)
        mask = info.get("action_mask")
        self._mask = None if mask is None else np.asarray(mask, dtype=bool)
        cars = []
        for slot, (row, col, length, horizontal) in enumerate(np.asarray(obs).tolist()):
            if int(length) == 0:
                continue
            cars.append({
                "slot": slot,
                "label": self._labels[slot] if slot < len(self._labels) else "?",
                "row": int(row), "col": int(col),
                "length": int(length), "horizontal": bool(horizontal),
            })
        if cars:
            self._cars = cars
            self._by_slot = {c["slot"]: c for c in cars}

    def _selected_car(self) -> dict[str, Any] | None:
        car = self._by_slot.get(int(self._selected))
        return car or (self._cars[0] if self._cars else None)

    def _can_move(self, car: dict[str, Any]) -> tuple[bool, bool]:
        """(back, forward) legality, straight from the engine's mask."""
        if self._mask is None:
            return True, True
        i = 2 * int(car["slot"])
        if i + 1 >= len(self._mask):
            return True, True
        return bool(self._mask[i]), bool(self._mask[i + 1])

    def _candidates(self) -> list[dict[str, Any]]:
        if not self.movable_only:
            return self._cars
        return [c for c in self._cars if any(self._can_move(c))]

    def _do_select(self, meta: int) -> None:
        cars = self._candidates()
        if not cars:
            return  # nothing acceptable: the selection stays, as *Among do
        cur = self._selected_car() or cars[0]
        if meta == SELECT_PREV:
            nxt = _cycle(self._cars, cars, cur, -1)
        elif meta == SELECT_NEXT:
            nxt = _cycle(self._cars, cars, cur, 1)
        else:
            d_row, d_col = {
                SELECT_UP: (-1, 0), SELECT_DOWN: (1, 0),
                SELECT_LEFT: (0, -1), SELECT_RIGHT: (0, 1),
            }[meta]
            nxt = _neighbour(cars, cur, d_row, d_col)
        self._selected = int(nxt["slot"])

    # ── info: the results-file columns ───────────────────────────────────────

    def _info(self, info: dict[str, Any]) -> dict[str, Any]:
        out = dict(info)
        out["event"] = self._event
        out["phase"] = self._phase
        out["selected"] = int(self._selected)
        out.setdefault("env_action", NOOP)
        out.setdefault("moved", False)
        out.setdefault("illegal", False)
        car: dict[str, Any] | None = None
        frm = to = (-1, -1)
        if self._event in ("move", "blocked", "trial_end"):
            car = self._by_slot.get(int(info.get("slot", -1)))
            frm = tuple(int(x) for x in info.get("from", frm))
            to = tuple(int(x) for x in info.get("to", to))
        elif self._event == "select":
            car = self._selected_car()
            if car is not None:
                frm = to = (car["row"], car["col"])
        out["car"] = car["label"] if car else ""
        out["orientation"] = ("H" if car["horizontal"] else "V") if car else ""
        out["from_row"], out["from_col"] = frm
        out["to_row"], out["to_col"] = to
        out["solved"] = self._event == "trial_end"
        out["t_ms"] = self._t_ms()
        out["trial_ms"] = self._trial_ms
        return out

    # ── Drawing (rushui) ─────────────────────────────────────────────────────

    def _draw_board(self, selected: dict[str, Any] | None, status: str) -> np.ndarray:
        img = np.empty((HEIGHT, WIDTH, 3), dtype=np.uint8)
        img[:] = _BG
        x_end, y_end = _BOARD_X0 + 6 * _TILE, _BOARD_Y0 + 6 * _TILE
        for i in range(7):
            img[_BOARD_Y0 + i * _TILE, _BOARD_X0:x_end + 1] = _GRID_COLOR
            img[_BOARD_Y0:y_end + 1, _BOARD_X0 + i * _TILE] = _GRID_COLOR
        ey = _BOARD_Y0 + _TARGET_ROW * _TILE
        _fill_rect(img, x_end - _EXIT_W, ey, x_end, ey + _TILE, _EXIT)
        for car in self._cars:
            x0, y0, x1, y1 = _car_rect(car)
            plate = _SELECT if (selected is not None and car["slot"] == selected["slot"]) else _OUTLINE
            k = _CAR_INSET / 2
            _fill_rect(img, x0 + k, y0 + k, x1 - k, y1 - k, plate)
            k = 3 * _CAR_INSET / 2
            _fill_rect(img, x0 + k, y0 + k, x1 - k, y1 - k, _car_color(car))
        if selected is not None:
            back, forward = self._can_move(selected)
            for direction, legal in ((-1, back), (1, forward)):
                if legal:
                    _fill_triangle(img, _arrow_points(selected, direction), _ARROW)
        self._blit_text(img, status, WIDTH / 2, _STATUS_Y)
        return img

    def _draw_screen(self, lines: list[str]) -> np.ndarray:
        img = np.empty((HEIGHT, WIDTH, 3), dtype=np.uint8)
        img[:] = _BG
        pitch = int(_FONT_SIZE * 1.4)
        top = HEIGHT / 2 - pitch * (len(lines) - 1) / 2
        for i, line in enumerate(lines):
            self._blit_text(img, line, WIDTH / 2, top + i * pitch)
        return img

    def _blit_text(self, img: np.ndarray, text: str, cx: float, cy: float) -> None:
        if not text:
            return
        if self._text is None:
            self._text = _text_renderer()
        arr = self._text(text)
        if arr is None:
            return
        h, w = arr.shape[:2]
        x0, y0 = int(round(cx - w / 2)), int(round(cy - h / 2))
        xa, ya = max(x0, 0), max(y0, 0)
        xb, yb = min(x0 + w, img.shape[1]), min(y0 + h, img.shape[0])
        if xb > xa and yb > ya:
            img[ya:yb, xa:xb] = arr[ya - y0:yb - y0, xa - x0:xb - x0]


# ── Selection helpers: ports of rush/select.go ───────────────────────────────


def _span(car: dict[str, Any]) -> tuple[int, int, int, int]:
    row0, col0 = car["row"], car["col"]
    if car["horizontal"]:
        return row0, row0, col0, col0 + car["length"] - 1
    return row0, row0 + car["length"] - 1, col0, col0


def _center2(car: dict[str, Any]) -> tuple[int, int]:
    r0, r1, c0, c1 = _span(car)
    return r0 + r1, c0 + c1


def _gap(a0: int, a1: int, b0: int, b1: int) -> int:
    d = max(a0, b0) - min(a1, b1)
    return d if d > 0 else 0


def _neighbour(cars, from_car, d_row: int, d_col: int):
    """rush.Board.NeighbourAmong: the nearest car that way, same band first, wrapping."""
    if (d_row != 0) == (d_col != 0):
        return from_car
    from_r2, from_c2 = _center2(from_car)
    f_r0, f_r1, f_c0, f_c1 = _span(from_car)
    best = wrapped = None
    best_score = wrap_score = 0
    for cand in cars:
        if cand["slot"] == from_car["slot"]:
            continue
        cand_r2, cand_c2 = _center2(cand)
        ahead = (cand_r2 - from_r2) * d_row + (cand_c2 - from_c2) * d_col
        c_r0, c_r1, c_c0, c_c1 = _span(cand)
        sideways = _gap(f_c0, f_c1, c_c0, c_c1) if d_row != 0 else _gap(f_r0, f_r1, c_r0, c_r1)
        score = ahead + _SIDEWAYS_PENALTY * sideways
        if ahead > 0:
            if best is None or score < best_score:
                best, best_score = cand, score
        elif wrapped is None or score < wrap_score:
            wrapped, wrap_score = cand, score
    return best or wrapped or from_car


def _cycle(all_cars, ok, from_car, delta: int):
    """rush.Board.CycleAmong: from ``from_car``'s place in board order, step
    past cars not in ``ok`` until one is, wrapping; stays put when none is."""
    n = len(all_cars)
    if n == 0 or not ok:
        return from_car
    accepted = {c["slot"] for c in ok}
    idx = next((i for i, c in enumerate(all_cars) if c["slot"] == from_car["slot"]), 0)
    step = 1 if delta > 0 else -1
    for k in range(1, n + 1):
        cand = all_cars[(idx + k * step) % n]
        if cand["slot"] in accepted:
            return cand
    return from_car


# ── Drawing helpers: ports of rushui/render.go ───────────────────────────────


def _fill_rect(img, x0, y0, x1, y1, color) -> None:
    xa, ya = max(int(round(x0)), 0), max(int(round(y0)), 0)
    xb, yb = min(int(round(x1)), img.shape[1]), min(int(round(y1)), img.shape[0])
    if xb > xa and yb > ya:
        img[ya:yb, xa:xb] = color


def _fill_triangle(img, pts, color) -> None:
    (x0, y0), (x1, y1), (x2, y2) = pts
    xa, xb = int(np.floor(min(x0, x1, x2))), int(np.ceil(max(x0, x1, x2))) + 1
    ya, yb = int(np.floor(min(y0, y1, y2))), int(np.ceil(max(y0, y1, y2))) + 1
    xa, ya = max(xa, 0), max(ya, 0)
    xb, yb = min(xb, img.shape[1]), min(yb, img.shape[0])
    if xb <= xa or yb <= ya:
        return
    ys, xs = np.mgrid[ya:yb, xa:xb]
    px, py = xs + 0.5, ys + 0.5
    e0 = (x1 - x0) * (py - y0) - (y1 - y0) * (px - x0)
    e1 = (x2 - x1) * (py - y1) - (y2 - y1) * (px - x1)
    e2 = (x0 - x2) * (py - y2) - (y0 - y2) * (px - x2)
    inside = ((e0 >= 0) & (e1 >= 0) & (e2 >= 0)) | ((e0 <= 0) & (e1 <= 0) & (e2 <= 0))
    img[ya:yb, xa:xb][inside] = color


def _car_rect(car) -> tuple[float, float, float, float]:
    x0 = _BOARD_X0 + car["col"] * _TILE
    y0 = _BOARD_Y0 + car["row"] * _TILE
    w = _TILE * (car["length"] if car["horizontal"] else 1)
    h = _TILE * (1 if car["horizontal"] else car["length"])
    return x0, y0, x0 + w, y0 + h


def _car_color(car) -> tuple[int, int, int]:
    """rushui.carColor: red for the target, else by alphabetical index (rush.Car.ID)."""
    label = str(car.get("label", "?"))
    if label == "A" or int(car.get("slot", -1)) == 0:
        return _CAR_COLORS[0]
    idx = ord(label) - ord("A") if label.isalpha() else int(car.get("slot", 1))
    return _CAR_COLORS[idx % (len(_CAR_COLORS) - 1) + 1]


def _arrow_points(car, direction: int):
    """rushui.arrowPoints, tip first: inside the leading end, pointing the way
    a slide in ``direction`` (-1 back, +1 forward) goes."""
    x0, y0, x1, y1 = _car_rect(car)
    cx, cy = (x0 + x1) / 2, (y0 + y1) / 2
    tip_inset, length, half = _TILE * _ARROW_TIP_INSET, _TILE * _ARROW_LEN, _TILE * _ARROW_HALF
    if car["horizontal"]:
        tip_x = cx + direction * ((x1 - x0) / 2 - tip_inset)
        back_x = tip_x - direction * length
        return [(tip_x, cy), (back_x, cy + half), (back_x, cy - half)]
    tip_y = cy + direction * ((y1 - y0) / 2 - tip_inset)
    back_y = tip_y - direction * length
    return [(cx, tip_y), (cx + half, back_y), (cx - half, back_y)]


def _text_renderer():
    """A ``text -> (h, w, 3) uint8`` function on the board background, from
    pygame if importable, else Pillow, else one that draws nothing."""
    try:
        import pygame

        if not pygame.font.get_init():
            pygame.font.init()
        font = pygame.font.Font(pygame.font.get_default_font(), _FONT_SIZE)

        def with_pygame(text: str):
            surf = font.render(text, True, _TEXT, _BG)
            return pygame.surfarray.array3d(surf).transpose(1, 0, 2)

        return with_pygame
    except Exception:
        pass
    try:
        from PIL import Image, ImageDraw, ImageFont

        try:
            font = ImageFont.truetype("DejaVuSans-Bold.ttf", _FONT_SIZE)
        except Exception:
            font = ImageFont.load_default()

        def with_pil(text: str):
            l, t, r, b = ImageDraw.Draw(Image.new("RGB", (1, 1))).textbbox((0, 0), text, font=font)
            im = Image.new("RGB", (r - l + 2, b - t + 2), _BG)
            ImageDraw.Draw(im).text((-l + 1, -t + 1), text, fill=_TEXT, font=font)
            return np.asarray(im)

        return with_pil
    except Exception:
        pass
    return lambda text: None
