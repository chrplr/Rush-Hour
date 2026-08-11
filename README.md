# Rush Hour — sliding-block problem solving

The puzzle game **Rush Hour** (Nob Yoshigahara / ThinkFun) is a popular paradigm in cognitive
science and cognitive psychology for studying human spatial planning, backward reasoning, and mental
simulation (rollout). It has the following features:

* *Deterministic & Bounded State Space:* Unlike chess, Rush Hour has strict 1D physical constraints
  (cars only move forward/backward), making state transitions fully deterministic.
* *Subgoal Hierarchy:* It naturally forces *hierarchical planning*: to
  solve $A$, you must first solve $B$, which requires moving $C$.
* *Measurable Thinking Time:* Initial pause duration (first-move latency) provides direct behavioral
  measurement of the depth of forward planning before execution starts.

Rush Hour is an interesting task for the study of planning and insight: the state
space is small enough to be solved exhaustively (so each puzzle has an exact
minimum-move count) but large enough that people plan, backtrack, and get stuck.
Because the results file holds the complete action sequence, the analysis is not
limited to "solved / time taken" — the solution path, the detours away from the
optimal line, the pauses before each move, and the vehicles the participant
touches without moving are all recoverable.


See [RushHourCognition](RushHourCognition.md) for more.


# What this repository provides

A [goxpyriment](https://chrplr.github.io/goxpyriment) port of the **Rush Hour** puzzle , turned into
a behavioural experiment: a series of trials, each a different 6×6 traffic jam, with **every mouse
action recorded**.


## Play in your browser

A demo is provided that runs directly in the browser (no install) at
**<https://chrplr.github.io/Rush-Hour/>**. 


## Binaries

Prebuilt executables for Linux, macOS, and Windows are attached to each
[release](https://github.com/chrplr/Rush-Hour/releases). Everything — SDL3 and
the whole puzzle library — is embedded in the single file; there is nothing else
to download.

These executables are not code-signed, so macOS and Windows will probably get in
the way on the first run. This does not mean anything is wrong with the program:
an unsigned, newly published executable is simply something neither system has
seen before, and you must authorize it.

- **macOS.** If Gatekeeper refuses to launch it, see [this note about unsigned macOS
  apps](https://chrplr.github.io/note-about-macos-unsigned-apps/).
- **Windows.** Defender SmartScreen shows a blue "Windows protected your PC" box, and Defender
  Antivirus may more rarely quarantine the file as a trojan — a well-documented false positive on Go
  binaries. See [this note about unsigned Windows
  apps](https://chrplr.github.io/note-about-windows-unsigned-apps/).

Building it yourself, as described in the next session, avoids all of this — a locally compiled
executable does not trigger the gatekeepers.

## Build and run from source

The only prerequisites are the [Go toolchain](https://go.dev/dl/) and, to fetch
the source, [git](https://git-scm.com/downloads). Go downloads every library it
needs on its own, and SDL3 is embedded in the binary, so there is nothing else
to install.

```bash
git clone https://github.com/chrplr/Rush-Hour.git
cd Rush-Hour

bash build.sh          # Linux, macOS, or Git Bash on Windows
                       # (on Windows, double-click build.bat instead)
```

Both scripts just run `go build .` with a few convenience options and produce a
`Rush-Hour` executable (`Rush-Hour.exe` on Windows). Or compile and run in one
step, without producing an executable:

```bash
go run .                  # first 12 puzzles, fullscreen
go run . -w -s 3          # windowed, subject 3
go run . -n 20            # first 20 puzzles
go run . -n 0             # the whole 49-puzzle library
```

---

## Task

Each trial shows a 6×6 grid of vehicles. The **red** car sits on row 2 and must
be driven out through the opening in the right wall. Vehicles slide only along
their own axis (a horizontal car left/right, a vertical car up/down) and cannot
pass through each other or the walls.

A trial ends **only when the puzzle is solved** — there is no time limit and no
skip key, so every trial contributes a complete solution path. `ESC` (or closing
the window) ends the whole session; data collected up to that point is kept,
since the file is flushed after every puzzle.

### Moving a vehicle

**One click, one cell.** Click on the side of a vehicle that points the way you
want it to go: anywhere left of its midline sends a horizontal vehicle one cell
left, anywhere right of it one cell right, and likewise above/below for a
vertical one. The split is on the vehicle's midline, not on cell boundaries, so
3-cell vehicles have no inert middle. A step into a wall or into another vehicle
leaves the board unchanged.

There is no selection state and no dragging. The vehicle under the cursor is
outlined in white as a hover cue, but that highlight carries no state — it only
says which vehicle a click would act on.

This differs from the pygame original, which drags. One click = one cell makes
every move a discrete, unambiguous event, which is what the data file records.

---

## Puzzles

`internal/rush/puzzles.txt` is embedded in the binary (`//go:embed`) and holds a library of
**49 puzzles in increasing difficulty, from 3 to 51 moves**, one per line:

```
<name>: <minimum moves>: <board>

p02:  4: BCCCoo BoooDo oAAEDo oooEoo FFoEoo ooGGGo   #  7 vehicles
p49: 51: GBBoLo GHIoLM GHIAAM CCCKoM ooJKDD EEJFFo   # 13 vehicles
```

One character per cell, read row-major: `o` (or `.`) is an empty cell, `A` is
the red target car, and the other letters are the remaining vehicles (2 or 3
cells each). Whitespace is ignored, so a board can also be written as six
6-character groups, as above.

The minimum-move count counts one slide of a vehicle, over any distance, as a
single move. It is written to every data row (`min_moves`), which makes
"excess moves over the optimum" a per-trial measure requiring no extra
analysis. `n_moves` is on the same scale: because a click only ever displaces a
vehicle by one cell, consecutive clicks on the same vehicle in the same
direction are counted as a single slide. The raw click count is recoverable by
counting `click_move` rows.

A full session of 49 puzzles is long — the last ones take many minutes each —
so `-n N` presents only the first *N* (default 12, `-n 0` for all). Because the
file is ordered easy to hard, a prefix is a graded curriculum.

The boards come from **Michael Fogleman's exhaustive Rush Hour database**
(2,577,412 puzzles, [michaelfogleman.com/rush](https://www.michaelfogleman.com/rush/)):
wall-free positions with at least 8 vehicles, one per difficulty level, re-scored
with the solver in `internal/rush/solver.go` (Fogleman's own move metric differs). `p02` is
the board of the original pygame program.

Every line is parsed and validated at startup — a vehicle that is bent, of the
wrong length, or a target car off row 2 aborts the program before the first
trial rather than producing an unsolvable board. `TestEmbeddedPuzzles` goes
further: it solves each puzzle by breadth-first search and fails if one is
unsolvable, duplicated, mis-labelled, or out of order.

To use a different puzzle set, edit `internal/rush/puzzles.txt` and rebuild
(or pass `-puzzles <path>` to the environment server, which reads one at runtime). The declared
move count may be omitted (`name: board`); the test then only checks
solvability and ordering.

---

## Results

One CSV row per **action**, plus a summary row per puzzle.

| Column | Meaning |
|---|---|
| `trial`, `puzzle` | Trial number (1-based) and puzzle name from the puzzle library |
| `min_moves` | Length of the shortest solution for this puzzle |
| `event` | `trial_start`, `click_move`, `click_blocked`, `click_empty`, `trial_end` |
| `t_ms` | Milliseconds since the onset of the puzzle |
| `event_ts_ns` | SDL3 hardware timestamp (ns) — on the three `click_*` rows only |
| `mouse_x`, `mouse_y` | Cursor position, center-relative, +Y up |
| `car`, `orientation` | Vehicle letter and `H`/`V` |
| `from_row`, `from_col` | Position of the vehicle before the move |
| `to_row`, `to_col` | Position after the move |
| `n_moves`, `solved`, `trial_ms` | Summary — filled on the `trial_end` row only (`-1` / `false` elsewhere) |

Every click produces exactly one row. `click_move` is a click that displaced a
vehicle by one cell; `click_blocked` is a click on a vehicle that could not move
that way (a wall or a neighbouring vehicle); `click_empty` is a click
that hit no vehicle at all. The latter two are the record of hesitations and
failed attempts. Because the board is deterministic, replaying the `click_move`
rows in order reconstructs the exact board state at any point in the trial.

---

## Letting an agent play

The same game is available as a [Gymnasium](https://gymnasium.farama.org/)
environment, so an AI agent can be measured on the boards the participants
solve. **One agent action is one cell, exactly as one mouse click is one cell**,
which is what makes the two traces comparable: the agent writes the same
results-file columns, and its `n_moves` is counted by the same rule.

The rules are not reimplemented in Python. A small Go binary serves boards over
a line-oriented JSON protocol on stdin/stdout, and the Python package is a
client:

```sh
go build -o rushhour-env ./cmd/rushhour-env
pip install -e python
```

```python
import gymnasium, rushhour_gym

env = gymnasium.make("RushHour-Easy-v0")
obs, info = env.reset(seed=0)
obs, reward, terminated, truncated, info = env.step(0)
```

The protocol is meant to be driveable by hand, which is also how to check that
the binary works:

```console
$ ./rushhour-env -board
{"id":1,"cmd":"hello"}
{"id":2,"cmd":"reset","puzzle":"p02"}
{"id":3,"cmd":"step","action":12}
```

`-csv <path>` records the agent's play in the participant results format, so an
agent run and a session can be read by the same analysis script.

To watch an agent play on the real board, build the windowed variant and point
the environment at it:

```sh
go build -tags rushui -o rushhour-env-view ./cmd/rushhour-env
RUSHHOUR_ENV_BIN=./rushhour-env-view python -c "
from rushhour_gym import RushHourEnv, run_optimal
env = RushHourEnv(puzzle='p03', render_mode='human'); print(run_optimal(env)); env.close()"
```

The window build is separate because goxpyriment carries SDL for every platform;
the default `rushhour-env` has no graphics dependency at all, which is what a
training loop wants.

New to this? [README-AI.md](README-AI.md) is a step-by-step guide that assumes
no prior Gymnasium experience. [python/README.md](python/README.md) is the
reference: the action and observation spaces, the reward schemes, curricula, and
the vectorised environment.

---

## Implementation notes

| Path | Role |
|---|---|
| `internal/rush/` | The rules — parsing, move legality, win test, and a breadth-first optimal solver. No SDL, fully unit-tested |
| `internal/rush/puzzles.txt` | The embedded puzzle set |
| `internal/rushui/` | Drawing and the cell ↔ screen-coordinate mapping |
| `internal/rushlog/` | The results-file columns, shared by the experiment and the agent environment |
| `internal/rushenv/` | The JSON-lines protocol that serves boards to another language |
| `cmd/rushhour-env/` | The environment server binary |
| `main.go` | Trial loop, input state machine, data logging |
| `python/` | The Gymnasium environment ([its own README](python/README.md)) |

`internal/rush` has no graphics dependency and is where every rule lives, so the
experiment, the agent environment and the tests all play the same game rather
than three implementations of it.

Differences from the pygame original it is based on: a series of puzzles instead
of a single hardcoded board, action logging, one-click/one-cell moves in place
of dragging, and square vehicle corners (SDL's `RenderFillRect` has no border
radius).

It is built with [goxpyriment](https://github.com/chrplr/goxpyriment), a Go
framework for behavioural experiments.

## Reference

- Nob Yoshigahara, *Rush Hour* (ThinkFun, 1996).
- M. Fogleman, ["Solving Rush Hour, the Puzzle Game"](https://www.michaelfogleman.com/rush/)
  (2018) — exhaustive analysis of the state space, the board notation used here,
  and the puzzle database the library is drawn from.

## License

(c) Copyright Christophe Pallier 2026

MIT — see [LICENSE](LICENSE).
