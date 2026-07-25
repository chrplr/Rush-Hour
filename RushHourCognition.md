The puzzle game **Rush Hour** is a popular paradigm in cognitive science and cognitive psychology
for studying human spatial planning, backward reasoning, and mental simulation (rollout).

It has the follwing features:

* *Deterministic & Bounded State Space:* Unlike chess, Rush Hour has strict 1D physical constraints
  (cars only move forward/backward), making state transitions fully deterministic.
* *Subgoal Hierarchy:* It naturally forces *hierarchical planning*: to
  solve $A$, you must first solve $B$, which requires moving $C$.
* *Measurable Thinking Time:* Initial pause duration (first-move latency) provides direct behavioral
  measurement of the depth of forward planning before execution starts.


### Some Scientific Papers Using Rush Hour

#### 1. Backward Reasoning and Subgoal Planning

* **Paper:** *Backward reasoning through AND/OR trees to solve problems* (Olieslagers et al., 2024)
* **What it studied:** How humans break down complex, long-horizon planning problems into subgoals.
* **Key Finding:** In Rush Hour, moving the target car (red car) to the exit is the primary
  goal. When blocked, the brain creates recursive subgoals (moving blocking cars out of the way),
  forming an **AND/OR goal tree**. The study shows that human move choices and pauses reflect
  backward search through these subgoal trees rather than simple brute-force forward simulation.

#### 2. Visual Gestalt and Mental Representation in Planning

* **Paper:** *Gestalt Effects in Planning: Rush-Hour as an Example* (Bennati, Brüssow, Ragni, &
  Konieczny, 2014 — *Proceedings of the Cognitive Science Society*)
* **What it studied:** How visual perceptual grouping (Gestalt principles) affects how humans plan
  moves ahead in spatial grid puzzles.
* **Key Finding:** Human planners don't view puzzle boards purely as abstract state graphs;
  perceptual organization and visual layout strongly dictate which potential moves are prioritized
  during cognitive planning.

#### 3. Mental Computational Effort & Metacognition

* **Paper:** *Thinking about Thinking as Rational Computation* (Berke, Tenenbaum, Sterling, &
  Jara-Ettinger, 2023)
* **What it studied:** How humans infer the internal planning effort and mental processing time of
  others.
* **Key Finding:** Using Rush Hour as the experimental domain, the researchers built a computational
  model of bounded rational planning. They demonstrated that human observers evaluate pause lengths
  in Rush Hour to determine whether a solver is actively planning, daydreaming, or retrieving a
  solution from memory.

#### 4. State-Space Traversal & Problem Difficulty

* **Paper:** *What Determines Difficulty of Transport Puzzles?* / *Human Problem Solving in Puzzles*
  (Jarušek & Pelánek, 2011/2013)
* **What it studied:** Tracking human behavior during state-space traversal in sliding-block
  transport puzzles like Rush Hour and Sokoban.
* **Key Finding:** Early in a Rush Hour puzzle, human planning resembles a semi-random exploratory
  search. As players get closer to clearing the main obstruction, planning becomes highly direct and
  goal-driven.

### References

Bennati, S., Brüssow, S., Ragni, M., & Konieczny, L. (2014). Gestalt effects in planning: Rush-Hour
as an example. *Proceedings of the 36th Annual Meeting of the Cognitive Science Society*, 36,
170–175. https://escholarship.org/content/qt8458c8hj/qt8458c8hj.pdf

Berke, M. D., Tenenbaum, A. L., Sterling, B. G., & Jara-Ettinger, J. (2023). Thinking about thinking
as rational computation. *Proceedings of the 45th Annual Meeting of the Cognitive Science
Society*, 45. [https://doi.org/10.31234/osf.io/e65p3](https://doi.org/10.31234/osf.io/e65p3)

Jarušek, P., & Pelánek, R. (2011). What determines difficulty of transport puzzles? *Proceedings of
the 24th International Florida Artificial Intelligence Research Society Conference (FLAIRS)*,
428–433. https://cdn.aaai.org/ocs/2518/2518-11200-1-PB.pdf


Olieslagers, J., Bnaya, Z., Li, Y., & Ma, W. J. (2024). Backward reasoning through AND/OR trees to
solve problems. CogSci ... Annual Conference of the Cognitive Science Society. Cognitive Science
Society (U.S.). Conference, 46, 4402–4409. https://pmc.ncbi.nlm.nih.gov/articles/PMC11872137/

