# Letting an AI play Rush Hour

A step-by-step guide to the [Gymnasium](https://gymnasium.farama.org/) interface,
assuming you have never used one before.

The experiment in this repository asks people to solve Rush Hour puzzles and
records every click. This interface lets a program play **the same puzzles, by
the same rules, one click at a time** — so that what an agent does and what a
participant did can be put side by side.

That last part is the reason for the whole design. The rules are not
reimplemented in Python: they run in the same Go code the experiment runs, and
Python talks to it over a pipe. There is one implementation of the game, so
there is nothing to drift.

If you already know your way around Gymnasium, skip this and read
[python/README.md](python/README.md), which is the reference.

---

## 1. Install it

You need **Python 3.10 or newer**. You also need **[Go](https://go.dev/dl/)**,
unless you take the binary from a release archive.

From the top of this repository:

```sh
go build -o rushhour-env ./cmd/rushhour-env
pip install -e python
```

The first command builds a small program that serves puzzles. The second
installs the Python package that talks to it. (If you skip the first, the
package will try to build it for you the first time you use it.)

Check it worked:

```sh
python -c "
import gymnasium, rushhour_gym
env = gymnasium.make('RushHourFixed-v0', render_mode='ansi')
obs, info = env.reset(seed=0)
print(env.render())
print('this puzzle needs', info['min_moves'], 'moves')
env.close()"
```

```
BCCCoo
BoooDo
oAAEDo <
oooEoo
FFoEoo
ooGGGo

this puzzle needs 4 moves
```

That is a Rush Hour board. `A` is the red car, the other letters are the
vehicles in the way, `o` is an empty square, and the `<` marks the exit. The red
car has to reach the right-hand wall on its row. Vehicles slide along their own
length only — `A` moves left and right, a vertical car moves up and down — and
none of them can pass through another.

> **If that failed**, jump to [§11, when something goes wrong](#11-when-something-goes-wrong).

---

## 2. The two words you need

**Episode** — one puzzle, from a fresh board until it is solved (or until you
give up on it). `env.reset()` starts one.

**Step** — one action. `env.step(a)` takes one and hands back what happened.

The loop every agent runs is: reset, then step until the episode ends.

---

## 3. What an action is

**One action moves one vehicle by one square.** That is deliberately the same
unit as one mouse click in the experiment, which is what makes an agent's play
and a person's play directly comparable.

Actions are plain integers, `0` to `31`:

```
action = slot * 2 + direction
```

- **slot** identifies a vehicle. **Slot 0 is always the red car**, on every
  puzzle. The others are numbered from the top-left of the starting position
  onwards, and keep their numbers for the whole episode.
- **direction** is `0` for left (or up, for a vertical vehicle) and `1` for
  right (or down).

There are always 32 actions even though boards carry between 7 and 15 vehicles.
The spare slots are padding: they never do anything. Keeping the number fixed is
what lets one agent play every puzzle.

Here is that board again with every slot spelled out:

```
BCCCoo      slot  vehicle  where                        actions that work
BoooDo         0     A     horizontal at row 2, col 1    0 = left
oAAEDo <       1     B     vertical   at row 0, col 0    3 = down
oooEoo         2     C     horizontal at row 0, col 1    5 = right
FFoEoo         3     D     vertical   at row 1, col 4    6 = up, 7 = down
ooGGGo         4     E     vertical   at row 2, col 3    8 = up
               5     F     horizontal at row 4, col 0   11 = right
               6     G     horizontal at row 5, col 2   12 = left, 13 = right
```

Nine of the thirty-two actions do anything at all. `A` cannot go right because
`E` is parked next to it — that is the whole puzzle.

---

## 4. The action mask — the one thing not to skip

Most actions do nothing most of the time. An action that cannot move anything is
**not an error**: the board simply stays as it was, and you have wasted a step.

So every `reset` and `step` hands you a **mask** saying which actions would
actually do something:

```python
import numpy as np
from rushhour_gym import RushHourEnv

env = RushHourEnv(puzzle="p02")
obs, info = env.reset(seed=0)

legal = np.flatnonzero(info["action_mask"])
print("legal right now:", legal)          # [ 0  3  5  6  7  8 11 12 13]

obs, reward, terminated, truncated, info = env.step(int(legal[0]))
print("moved:", info["moved"], "| vehicle:", info["label"])
env.close()
```

```
legal right now: [ 0  3  5  6  7  8 11 12 13]
moved: True | vehicle: A
```

**Use the mask.** An agent that picks from `legal` learns from a real move every
time. An agent that picks blindly from all 32 spends most of its life pushing
vehicles into walls, and learns roughly nothing. This single detail makes more
difference to how well an agent does here than any other choice on this page.

---

## 5. Your first agent

Fifteen lines, picking uniformly among the legal moves:

```python
import gymnasium
import numpy as np
import rushhour_gym  # registering the environment ids is all this import does

env = gymnasium.make("RushHourFixed-v0")
rng = np.random.default_rng(0)

obs, info = env.reset(seed=0)
terminated = truncated = False
while not (terminated or truncated):
    legal = np.flatnonzero(info["action_mask"])
    action = int(rng.choice(legal))
    obs, reward, terminated, truncated, info = env.step(action)

print("solved" if terminated else "gave up",
      "after", info["n_steps"], "clicks;",
      "the shortest solution is", info["min_moves"], "moves")
env.close()
```

```
gave up after 100 clicks; the shortest solution is 4 moves
```

**That is the expected result, not a bug.** `RushHourFixed-v0` allows 100 clicks
per episode, and random play rarely finds a four-move solution in that many:
over 200 seeds it got there **11 times**. Random play is genuinely bad at this,
which is the point of the exercise — it is the floor everything else is measured
against.

Note that `terminated` and `truncated` are different things. **`terminated`
means solved.** **`truncated` means the step budget ran out** — which is what
happened here. Rush Hour has no way to lose: every move can be undone, so a
solvable board stays solvable, and an unfinished episode can only end by running
out of time.

There is a ready-made version of this that also plays perfectly for comparison:

```sh
python python/examples/random_agent.py --episodes 6 --max-moves 10
```

```
puzzle    optimum  optimal   random
-----------------------------------
p01             3        3       28
p02             4        4     1341
p03             5        5      323
p04             6        6      579
p05             7        7       82
p06             8        8      366
```

Those two columns are the ceiling and the floor. A learned agent is only
interesting somewhere between them. (`optimal` comes from a breadth-first search
inside the Go program — an oracle to measure against, not a policy to imitate.)

---

## 6. Two ways of counting moves

You will see both, and they mean different things:

- **`n_steps`** counts *actions* — squares moved, which is clicks.
- **`n_slides`** counts *moves* in the puzzle's own sense: sliding one vehicle
  three squares in one go is **one** move.

`min_moves` is on the `n_slides` scale, and so is the Rush Hour literature.
Here is a perfect solution to that board, printed step by step:

```
 action  vehicle        move  reward  slides
      6        D     left/up      -1       1
     12        G     left/up      -1       2
     12        G     left/up      -1       2      <- same vehicle, same way:
      9        E  right/down      -1       3         still the same move
      1        A  right/down      -1       4
      1        A  right/down      -1       4
      1        A  right/down      -1       4

solved=True  total reward=-7  steps=7  slides=4  optimum=4
```

Seven clicks, four moves. A participant solving this optimally would produce
exactly seven `click_move` rows in their data file, and `n_moves = 4`.

---

## 7. What the agent sees

`env.reset()` and `env.step()` return an **observation**: an array describing
the board, which is what you feed to a neural network. There are four shapes to
choose from, all describing the same thing:

```python
RushHourEnv(puzzle="p02", obs_mode="planes")   # the default
```

| `obs_mode` | shape | when to use it |
|---|---|---|
| `planes` | `(19, 6, 6)` | the default; one yes/no layer per vehicle slot, plus layers for horizontal, vertical, and the corridor the red car still has to cross |
| `grid` | `(6, 6)` | one number per square, `0` for empty. Easiest to print and eyeball |
| `cars` | `(16, 4)` | row, column, length and orientation per slot. Tiny; fine for a small network |
| `dict` | all of the above | for libraries that want a dictionary, e.g. SB3's `MultiInputPolicy` |

If you are unsure, keep `planes`. Whichever you pick, the observation always
tells you **which slot is which vehicle** — without that an agent could not
connect what it sees to the action numbers from §3.

```python
env = RushHourEnv(puzzle="p02", obs_mode="grid")
obs, info = env.reset(seed=0)
print(obs)
```

```
[[2 3 3 3 0 0]
 [2 0 0 0 4 0]
 [0 1 1 5 4 0]
 [0 0 0 5 0 0]
 [6 6 0 5 0 0]
 [0 0 7 7 7 0]]
```

`1` is the red car (slot 0, plus one so that `0` can mean empty), `2` is slot 1,
and so on.

> One trap: 6×6 is far too small for `stable-baselines3`'s built-in image
> network, which wants at least 36×36. Use `MlpPolicy`, which flattens the
> observation. §8 does this.

---

## 8. Rewards, and training something

The **reward** is what the agent is trying to maximise. By default it is `-1`
for every step, and nothing else. So the total for an episode is minus the
number of clicks it took, and the best possible agent is the one that solves the
puzzle in the fewest clicks — which is exactly the quantity measured in people.

Now a real learning agent. Install the extras:

```sh
pip install -e "python[rl]"
```

and run:

```sh
python python/examples/train_ppo.py --steps 200000 --max-moves 6
```

One run of exactly that command — the four puzzles solvable in six moves or
fewer — took **under two minutes** and finished at:

```
solved 100/100 evaluation episodes
moves over the optimum, on the solved ones: mean 0.3, median 0, perfect 69/100
```

So it solved every evaluation puzzle, and did so by the shortest possible route
about two-thirds of the time.

Read that as evidence the wiring works, not as a benchmark: it is **one run,
one seed, on the four easiest boards**, measured at 200,000 steps and about
1,800 steps a second (Intel Core Ultra 7 165H, 4 environments, CUDA available
and used by `stable-baselines3` for this policy). Ask for harder puzzles and the
numbers fall off quickly.

The important line in that script is this one:

```python
env = ActionMasker(env, lambda e: e.unwrapped.action_masks())
```

`MaskablePPO` uses the mask from §4, so the agent only ever samples a move that
does something. Plain `PPO` will also run, and will do noticeably worse for the
reason given there.

Start easy. `--max-moves 6` is the first handful of puzzles; the library runs to
51 moves, and the hard end is a genuinely difficult search problem — Rush Hour is
PSPACE-complete in general.

---

## 9. Watching it play, and comparing it to a person

**Watch it.** Build the version that opens a window, and point the package at it:

```sh
go build -tags rushui -o rushhour-env-view ./cmd/rushhour-env
RUSHHOUR_ENV_BIN=./rushhour-env-view python -c "
from rushhour_gym import RushHourEnv, run_optimal
env = RushHourEnv(puzzle='p03', render_mode='human')
print(run_optimal(env))
env.close()"
```

A window opens and the board moves. Add `-render-hold` to the binary's flags to
slow it to a human pace. This is a separate build because it carries SDL, which
a training run has no use for.

**Compare it to a person.** Pass `csv_path` and the agent's play is written in
the experiment's own results format:

```python
from rushhour_gym import RushHourEnv, run_optimal

with RushHourEnv(puzzle="p02", csv_path="agent.csv", subject_id=42) as env:
    run_optimal(env)
# the file is complete once the environment is closed
```

```
subject_id,trial,puzzle,min_moves,event,t_ms,event_ts_ns,mouse_x,mouse_y,car,orientation,from_row,from_col,to_row,to_col,n_moves,solved,trial_ms
42,1,"p02",4,"trial_start",0,0,0,0,"","",-1,-1,-1,-1,-1,false,-1
42,1,"p02",4,"click_move",3,0,0,0,"D","V",1,4,0,4,-1,false,-1
42,1,"p02",4,"click_move",3,0,0,0,"G","H",5,2,5,1,-1,false,-1
...
42,1,"p02",4,"trial_end",5,0,0,0,"","",-1,-1,-1,-1,4,true,5
```

Those are the same seventeen columns a participant's file has, in the same
order, with the same event names — so one analysis script reads both. An agent's
episode is a trial and each of its actions is a click.

What an agent cannot produce is hesitation. There is no thinking time between
its rows, so the timing columns of an agent run mean "when the request arrived"
and nothing more. The `mouse_x`/`mouse_y` columns are filled in only when the
window is open, with the pixel a participant would have had to click.

---

## 10. Choosing which puzzles to play

The 49 puzzles run from 3 moves to 51, hardest last. Three ways to pick:

```python
from rushhour_gym import RushHourEnv

# at construction — everything solvable in 3 to 12 moves
env = RushHourEnv(min_moves_range=(3, 12))

# between episodes — widen as the agent improves
env.set_puzzle_filter(min_moves_range=(3, 20))

# one specific board
env.reset(options={"puzzle": "p07"})

# or a board of your own, in the same notation
env.reset(options={"spec": "BCCCoo BoooDo oAAEDo oooEoo FFoEoo ooGGGo"})
```

Passing `seed=` to `reset` makes the sequence of puzzles reproducible.

To run many boards at once — which is how RL libraries prefer to work — use:

```python
envs = gymnasium.make_vec("RushHour-Easy-v0", num_envs=16)
```

That keeps all sixteen boards inside **one** helper process, advanced by one
request per vector step instead of sixteen. On the machine above that is about
35,000 steps a second against 28,000 for a single environment — a modest gain in
throughput, and a large one in the number of processes you are running.

---

## 11. When something goes wrong

| What you see | What it means |
|---|---|
| `BinaryNotFound` | The helper program is missing. Run `go build -o rushhour-env ./cmd/rushhour-env` from the top of the repository, or set `RUSHHOUR_ENV_BIN` to a copy you already have. |
| `ModuleNotFoundError: rushhour_gym` | The package is not installed in the interpreter you are using. `pip install -e python`, and check `which python`. |
| `EngineDied` | The helper program stopped. Its messages go to your terminal — look just above the traceback. |
| `CommandFailed: no_such_puzzle` | A puzzle name that is not in the library. `env.puzzle_pool` lists what is available. |
| `CommandFailed: bad_spec` | A board you passed in is malformed — or already solved, which the parser rejects on purpose. |
| The agent never solves anything | Are you using the mask from §4? Then check you started on easy puzzles (`--max-moves 6`), and that the step budget is not cutting episodes short. |
| Everything is very slow | About 28,000 steps a second is normal for one environment (measured: 36 µs a step, Intel Core Ultra 7 165H). Almost all of that is the pipe, not the game. If a training run is slower than that, the bottleneck is your agent, not this. |

Two things that are **not** bugs:

- An action that moves nothing. That is a wasted step, by design (§4).
- `truncated=True` with `terminated=False`. The step budget ran out; the puzzle
  was not lost, because it cannot be (§5).

---

## Where to go next

- [python/README.md](python/README.md) — the reference: every argument, every
  reward scheme, the wire protocol.
- [README.md](README.md) — the experiment itself, the results format, the puzzle
  library.
- [RushHourCognition.md](RushHourCognition.md) — what the psychology literature
  has found about how people solve these, which is the reason for comparing an
  agent's play to a person's in the first place.
