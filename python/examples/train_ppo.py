#!/usr/bin/env python3
# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the MIT License.

"""Train a masked PPO agent on the easy end of the puzzle library.

Needs the optional dependencies:

    pip install -e "python[rl]"

Masking is the point here. Most of the 32 actions do nothing on any given
position — they address a vehicle that cannot move, or a padding slot with no
vehicle at all — so an agent that has to discover legality by trial and error
spends nearly all of its experience learning what it could have been told.
MaskablePPO is given the mask and only ever samples a legal move.

    python examples/train_ppo.py --steps 200000 --max-moves 6

The run prints a solve rate and the average number of moves used against the
known optimum, so the result can be read against `run_optimal` (the ceiling) and
`run_random` (the floor) from examples/random_agent.py.
"""

from __future__ import annotations

import argparse

import numpy as np


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--steps", type=int, default=200_000, help="training steps")
    parser.add_argument("--max-moves", type=int, default=6, help="hardest puzzle to train on")
    parser.add_argument("--envs", type=int, default=4, help="environments to run in parallel")
    parser.add_argument("--eval-episodes", type=int, default=100)
    parser.add_argument("--seed", type=int, default=0)
    parser.add_argument("--save", type=str, default="", help="where to write the trained model")
    args = parser.parse_args()

    try:
        from sb3_contrib import MaskablePPO
        from sb3_contrib.common.maskable.utils import get_action_masks
        from sb3_contrib.common.wrappers import ActionMasker
    except ImportError:  # pragma: no cover - depends on optional extras
        raise SystemExit('sb3-contrib is missing. Install it with: pip install -e "python[rl]"')

    import gymnasium
    from stable_baselines3.common.env_util import make_vec_env
    from stable_baselines3.common.monitor import Monitor

    import rushhour_gym  # noqa: F401 — registers the ids

    def make_env():
        env = gymnasium.make(
            "RushHour-v0", min_moves_range=(0, args.max_moves), obs_mode="planes"
        )
        # MaskablePPO looks for the mask on the environment it is handed, and a
        # wrapper hides the method, so it is re-exposed here.
        env = ActionMasker(env, lambda e: e.unwrapped.action_masks())
        return Monitor(env)

    print(f"training on puzzles of at most {args.max_moves} moves, "
          f"{args.envs} environments, {args.steps} steps")

    train_envs = make_vec_env(make_env, n_envs=args.envs, seed=args.seed)
    model = MaskablePPO(
        "MlpPolicy",
        train_envs,
        seed=args.seed,
        n_steps=256,
        batch_size=256,
        learning_rate=3e-4,
        verbose=1,
    )
    model.learn(total_timesteps=args.steps)
    train_envs.close()

    if args.save:
        model.save(args.save)
        print(f"model saved to {args.save}")

    # ── Evaluation ───────────────────────────────────────────────────────────
    # Reported against the optimum, because "solved" alone says nothing about
    # how wastefully: the point of comparison is the shortest solution.
    env = make_env()
    solved, excess = 0, []
    for episode in range(args.eval_episodes):
        obs, info = env.reset(seed=args.seed + 10_000 + episode)
        terminated = truncated = False
        while not (terminated or truncated):
            action, _ = model.predict(
                obs, action_masks=get_action_masks(env), deterministic=True
            )
            obs, _reward, terminated, truncated, info = env.step(action)
        if terminated:
            solved += 1
            excess.append(info["n_slides"] - info["min_moves"])
    env.close()

    print(f"\nsolved {solved}/{args.eval_episodes} evaluation episodes")
    if excess:
        print(f"moves over the optimum, on the solved ones: "
              f"mean {np.mean(excess):.1f}, median {np.median(excess):.0f}, "
              f"perfect {excess.count(0)}/{len(excess)}")


if __name__ == "__main__":
    main()
