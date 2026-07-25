### Using the game Rush Hour to study Cognition

The puzzle game **Rush Hour** is a popular paradigm in cognitive science and cognitive psychology
for studying human spatial planning and reasoning.

It has the follwing features:

* *Deterministic & Bounded State Space:* Unlike chess, Rush Hour has strict 1D physical constraints
  (cars only move forward/backward), making state transitions fully deterministic.
* *Subgoal Hierarchy:* It naturally forces *hierarchical planning*: to
  solve $A$, you must first solve $B$, which requires moving $C$.
* *Measurable Thinking Time:* Initial pause duration (first-move latency) provides direct behavioral
  measurement of the depth of forward planning before execution starts.



Flake, G. W., & Baum, E. B. (2002). Rush Hour is PSPACE-complete, or “Why you should generously tip
   parking lot attendants.” Theoretical Computer Science, 270(1), 895–911. <https://doi.org/10.1016/S0304-3975(01)00173-6>

   * What it studied: The computational complexity of generalized Rush Hour puzzles (played on an
     arbitrary $n \times n$ grid rather than the standard $6 \times 6$ board).
   * Key Finding: Proved that solving generalized Rush Hour is PSPACE-complete—placing it in the same
     algorithmic complexity class as Sokoban, Generalized Go, and Chess endgames. This formalizes
     mathematically why Rush Hour gets exponentially harder as board sizes grow, as finding an optimal
     solution can require an exponential number of moves with no simple shortcuts.


Jarušek, P., & Pelánek, R. (2011). What determines difficulty of transport puzzles? *Proceedings of
   the 24th International Florida Artificial Intelligence Research Society Conference (FLAIRS)*,
   428–433. <https://cdn.aaai.org/ocs/2518/2518-11200-1-PB.pdf>

   * What it studied: Tracking human behavior during state-space traversal in sliding-block
     transport puzzles like Rush Hour and Sokoban.
   * Key Finding: Early in a Rush Hour puzzle, human planning resembles a semi-random exploratory
     search. As players get closer to clearing the main obstruction, planning becomes highly direct and
     goal-driven.

Ragni, M., Steffenhagen, F., & Fangmeier, T. (2011). A Structural Complexity Measure for Predicting
   Human Planning Performance. Proceedings of the Annual Meeting of the Cognitive Science
   Society, 33. <https://escholarship.org/content/qt1wr6b29g/qt1wr6b29g.pdf>

   * What it studied: How to mathematically predict human planning difficulty and performance in Rush
     Hour using structural properties of the puzzle's search space rather than just shortest-path move
     counts.
   * Key Finding: Traditional complexity measures (like minimum move distance) fail to accurately
     reflect human difficulty in Rush Hour. Instead, human solving time and error rates are best
     predicted by a Structural Complexity Measure based on the puzzle's state graph—specifically taking
     into account branch points, dead ends, and the density of decision nodes that require backward
     steps or detour planning.


Olieslagers, J., Bnaya, Z., Li, Y., & Ma, W. J. (2024). Backward reasoning through AND/OR trees to
   solve problems. CogSci ... Annual Conference of the Cognitive Science Society. Cognitive Science
   Society (U.S.). Conference, 46, 4402–4409. <https://pmc.ncbi.nlm.nih.gov/articles/PMC11872137/>

   * What it studied: How humans break down complex, long-horizon planning problems into subgoals.
   * Key Finding: In Rush Hour, moving the target car (red car) to the exit is the primary
     goal. When blocked, the brain creates recursive subgoals (moving blocking cars out of the way),
     forming an **AND/OR goal tree**. The study shows that human move choices and pauses reflect
     backward search through these subgoal trees rather than simple brute-force forward simulation.

Bennati, S., Brüssow, S., Ragni, M., & Konieczny, L. (2014). Gestalt effects in planning: Rush-Hour
   as an example. *Proceedings of the 36th Annual Meeting of the Cognitive Science Society*, 36,
   170–175. <https://escholarship.org/content/qt8458c8hj/qt8458c8hj.pdf>


   * What it studied: How visual perceptual grouping (Gestalt principles) affects how humans plan
     moves ahead in spatial grid puzzles.
   * Key Finding: Human planners don't view puzzle boards purely as abstract state graphs;
     perceptual organization and visual layout strongly dictate which potential moves are prioritized
     during cognitive planning.

Berke, M. D., Tenenbaum, A. L., Sterling, B. G., & Jara-Ettinger, J. (2023). Thinking about thinking
   as rational computation. *Proceedings of the 45th Annual Meeting of the Cognitive Science
   Society*, 45. [https://doi.org/10.31234/osf.io/e65p3](https://doi.org/10.31234/osf.io/e65p3)

   * What it studied: How humans infer the internal planning effort and mental processing time of
     others.
   * Key Finding: Using Rush Hour as the experimental domain, the researchers built a computational
     model of bounded rational planning. They demonstrated that human observers evaluate pause lengths
     in Rush Hour to determine whether a solver is actively planning, daydreaming, or retrieving a
     solution from memory.
