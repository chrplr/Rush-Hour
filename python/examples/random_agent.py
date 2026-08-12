#!/usr/bin/env python3
# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the Apache License, Version 2.0.
# SPDX-License-Identifier: Apache-2.0

"""Play the shipped library twice: once at random, once optimally.

The two numbers bracket what a learned agent should be measured against — the
random walk is the floor, the breadth-first solution the ceiling — and the run
doubles as a check that the environment is wired up.

    python examples/random_agent.py --episodes 10 --max-moves 12
"""

from __future__ import annotations

import argparse

from rushhour_gym import RushHourEnv, run_optimal, run_random


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--episodes", type=int, default=8, help="puzzles to play")
    parser.add_argument("--max-moves", type=int, default=12, help="hardest puzzle to include")
    parser.add_argument("--steps", type=int, default=100_000, help="budget for the random walk")
    parser.add_argument("--seed", type=int, default=0)
    args = parser.parse_args()

    with RushHourEnv(min_moves_range=(0, args.max_moves)) as env:
        names = env.puzzle_pool[: args.episodes]
        print(f"{'puzzle':<8}{'optimum':>9}{'optimal':>9}{'random':>9}")
        print("-" * 35)

        for name in names:
            best = run_optimal(env, options={"puzzle": name})
            walk = run_random(
                env, seed=args.seed, options={"puzzle": name}, max_steps=args.steps
            )
            # Moves, not cells: comparable to the min_moves column of
            # puzzles.txt and to the n_moves column of a participant's file.
            random_moves = walk["slides"] if walk["solved"] else None
            print(
                f"{name:<8}{best['min_moves']:>9}{best['slides']:>9}"
                f"{random_moves if random_moves is not None else 'unsolved':>9}"
            )


if __name__ == "__main__":
    main()
