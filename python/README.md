# rushhour-gym

Rush Hour as a [Gymnasium](https://gymnasium.farama.org/) environment.

The rules are not reimplemented here. They run in the Go program this
repository is built around — the same code the human participants play — and
are served to Python over a pipe. **One action is one cell, which is exactly
what one mouse click is for a participant**, so an agent's trace and a
participant's trace count the same events and can be compared directly.

```python
import gymnasium
import rushhour_gym  # registers the environment ids

env = gymnasium.make("RushHour-Easy-v0")
obs, info = env.reset(seed=0)

terminated = truncated = False
while not (terminated or truncated):
    legal = info["action_mask"].nonzero()[0]
    obs, reward, terminated, truncated, info = env.step(legal[0])

print(info["puzzle"], "solved in", info["n_slides"], "moves;",
      "the optimum is", info["min_moves"])
env.close()
```

## Installing

```sh
pip install -e python[dev]
```

The Python package needs the `rushhour-env` binary. It is looked for in
`$RUSHHOUR_ENV_BIN`, then on `PATH`, then in the repository root, and finally
built from source if a Go toolchain is available:

```sh
go build -o rushhour-env ./cmd/rushhour-env
```

Release archives ship a prebuilt one for each platform; point
`$RUSHHOUR_ENV_BIN` at it to skip needing Go.

## The environment

| id | puzzles | step budget |
|---|---|---|
| `RushHour-v0` | all 49 | 500 |
| `RushHour-Easy-v0` | optimum ≤ 12 moves | 200 |
| `RushHourFixed-v0` | `p02`, the classic board | 100 |

**Action space** — `Discrete(32)`: `action = slot * 2 + direction`, direction 0
being left/up and 1 right/down. The vehicle moves exactly one cell, or nothing
happens.

Slot 0 is the red car on every board; slots 1 upward are the other vehicles in
reading order of their starting position, frozen for the episode. Boards have
between 7 and 15 vehicles, so the remaining slots are padding: their actions are
always masked off and their observation rows are zero.

**Observations** — `obs_mode` picks the encoding, all built from the same wire
data:

| mode | space | for |
|---|---|---|
| `planes` (default) | `Box(0, 1, (19, 6, 6))` | one binary plane per slot, plus horizontal / vertical / exit-corridor planes |
| `grid` | `Box(0, 16, (6, 6))` | compact and readable; cell values are names, not quantities |
| `cars` | `Box(0, 6, (16, 4))` | row, col, length, orientation — for an MLP baseline |
| `dict` | `Dict(grid, cars, action_mask)` | SB3's `MultiInputPolicy` |

Whichever you pick, slot identity is visible in the observation: without it an
agent cannot tell which vehicle action `2k` refers to.

**Masking** — `info["action_mask"]` on every `reset` and `step`, and an
`action_masks()` method, which is the name `sb3-contrib`'s `MaskablePPO` looks
for.

**Rewards** — `reward_scheme`:

- `step_penalty` (default): `-1` per step. The return is minus the number of
  cells moved, so the optimal policy is the shortest solution *in clicks* — the
  quantity the human data measures.
- `sparse`: `+1` on solving, `0` otherwise.
- `shaped`: step penalty plus potential-based shaping on the optimal distance to
  go. Policy-invariant, but it runs a search per step; fine for teaching, far
  too slow for the hard end of the library.

**Termination** — `terminated` means solved, and nothing else: Rush Hour has no
dead ends, since every move is reversible. An unsolved episode therefore ends
only by `TimeLimit` truncation, which is why a bare `RushHourEnv()` never
truncates while the registered ids do.

## Curricula

The optimal move count of every puzzle arrives in the handshake, so selection
needs no extra round trips:

```python
env = RushHourEnv(min_moves_range=(3, 12))     # at construction
env.set_puzzle_filter(min_moves_range=(3, 20)) # between episodes
env.reset(options={"puzzle": "p07"})           # a specific board
env.reset(options={"spec": "BCCCoo BoooDo oAAEDo oooEoo FFoEoo ooGGGo"})
```

All randomness lives in `reset`, driven by `self.np_random`, so a seeded run is
reproducible.

## Baselines

`rushhour_gym.run_optimal` plays a breadth-first optimal solution — the ceiling
any learning curve should be read against, and the generator for
behaviour-cloning data. `run_random` is the floor. Neither learns anything.

```python
from rushhour_gym import RushHourEnv, run_optimal
with RushHourEnv(puzzle="p02") as env:
    print(run_optimal(env))
```

## Talking to the server directly

The protocol is one JSON object per line and is meant to be driveable by hand:

```console
$ ./rushhour-env -board
{"id":1,"cmd":"hello"}
{"id":2,"cmd":"reset","puzzle":"p02"}
{"id":3,"cmd":"step","action":12}
```

Commands: `hello`, `reset`, `step`, `state`, `reset_batch`, `step_batch`,
`solve`, `close`. The server reports facts and never a reward — the reward
scheme, the termination rule and the observation tensor all live in Python, so
changing them never means rebuilding Go.

## Notes for training

- 6×6 is too small for SB3's `NatureCNN`, which wants at least 36×36. Use
  `MlpPolicy` on a flattened observation, or a small custom extractor with 3×3
  convolutions and no downsampling.
- A random walk on a hard board can wander for a very long time without ever
  being stuck. Start on `RushHour-Easy-v0`.
- Expect roughly 20–50k steps/s for a single environment, dominated by Python's
  `json`. If that ever binds, batch first.
