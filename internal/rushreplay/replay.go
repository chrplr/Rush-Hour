// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rushreplay

import (
	"fmt"

	"rush-hour/internal/rush"
	"rush-hour/internal/rushlog"
)

// Library resolves a puzzle name to the board it started from. A results file
// stores the name only, so replaying one needs the same library it was recorded
// against — pass rush.DefaultPuzzles() unless the session used -puzzles.
type Library map[string]rush.Puzzle

// NewLibrary indexes puzzles by name.
func NewLibrary(puzzles []rush.Puzzle) Library {
	lib := make(Library, len(puzzles))
	for _, p := range puzzles {
		lib[p.Name] = p
	}
	return lib
}

// DefaultLibrary is the embedded puzzle set, the one both the experiment and
// the agent environment play by default.
func DefaultLibrary() (Library, error) {
	puzzles, err := rush.DefaultPuzzles()
	if err != nil {
		return nil, err
	}
	return NewLibrary(puzzles), nil
}

// Problem is one disagreement between the file and the rules. Every problem
// names the line it came from, because the point of finding one is to go and
// look at it.
type Problem struct {
	Line   int
	Detail string
}

func (p Problem) String() string {
	if p.Line <= 0 {
		return p.Detail
	}
	return fmt.Sprintf("line %d: %s", p.Line, p.Detail)
}

// Step is one applied move, in replay order.
type Step struct {
	Event Event
	// Car is the vehicle as it stood *after* this move. It is a copy, not a
	// pointer into the board: the vehicle keeps moving, so a pointer would make
	// every step of a given vehicle report the position it ended the trial in.
	Car rush.Car
	// Board is the position after the move. It is a snapshot: replaying does
	// not mutate boards handed out earlier.
	Board *rush.Board
	// Slides is the running slide count, the metric min_moves is on.
	Slides int
	Solved bool
}

// Replayed is the outcome of re-playing one trial.
type Replayed struct {
	Trial *Trial
	// Start is the untouched puzzle the trial began from.
	Start *rush.Board
	// Steps holds one entry per click that moved a vehicle.
	Steps []Step
	// Final is the position the trial ended in.
	Final  *rush.Board
	Slides int
	Solved bool

	Problems []Problem
}

// OK reports that the trial replayed with nothing to complain about.
func (r *Replayed) OK() bool { return len(r.Problems) == 0 }

// Replay re-plays one trial against a fresh copy of its puzzle, checking every
// row as it goes.
//
// Four things are checked, and each of them can only be checked because the row
// records the move's outcome as well as its subject:
//
//   - the vehicle the row names exists, and stands where the row says it stood;
//   - the move is a legal one-cell step, and the rules allow it here;
//   - it lands exactly where the row says it landed;
//   - and, at the end, the recorded n_moves and solved agree with what
//     replaying actually produced.
//
// A trial that reports no problems is one whose file and whose rules describe
// the same session.
func Replay(t *Trial, lib Library) (*Replayed, error) {
	p, ok := lib[t.Puzzle]
	if !ok {
		return nil, fmt.Errorf("trial %d: puzzle %q is not in the library", t.Trial, t.Puzzle)
	}

	b := p.Fresh()
	out := &Replayed{Trial: t, Start: p.Fresh(), Final: b}
	var slides rush.SlideCounter

	for _, e := range t.Events {
		switch e.Kind {
		case rushlog.EventClickEmpty:
			// A click on no vehicle changes nothing, and there is nothing about
			// it that the board can contradict.
			continue

		case rushlog.EventClickBlocked:
			car := carAt(b, e.Car)
			if car == nil {
				out.problem(e, "vehicle %q is not on the board", e.Car)
				continue
			}
			// A blocked click records from == to, so it names no direction to
			// re-test. What can still be checked is that the vehicle was where
			// the row says it was.
			out.checkFrom(e, car)

		case rushlog.EventClickMove:
			car := carAt(b, e.Car)
			if car == nil {
				out.problem(e, "vehicle %q is not on the board", e.Car)
				continue
			}
			if !out.checkFrom(e, car) {
				continue
			}

			dir, err := direction(car, e)
			if err != nil {
				out.problem(e, "%v", err)
				continue
			}
			if !b.Step(car, dir) {
				out.problem(e, "%s cannot step %s from (%d,%d): the rules forbid a move the file records",
					e.Car, dirName(car, dir), e.FromR, e.FromC)
				continue
			}
			slides.Add(car.ID, dir)

			if car.Row != e.ToR || car.Col != e.ToC {
				out.problem(e, "%s moved to (%d,%d), but the file records (%d,%d)",
					e.Car, car.Row, car.Col, e.ToR, e.ToC)
			}

			out.Steps = append(out.Steps, Step{
				Event:  e,
				Car:    *car,
				Board:  b.Clone(),
				Slides: slides.N(),
				Solved: b.Solved(),
			})

		default:
			out.problem(e, "unknown event %q", e.Kind)
		}
	}

	out.Final = b
	out.Slides = slides.N()
	out.Solved = b.Solved()

	// The summary row is a second, independent record of the same trial. If it
	// disagrees with what the clicks produce, one of the two is wrong.
	if t.Ended {
		if t.Solved != out.Solved {
			out.Problems = append(out.Problems, Problem{
				Line: t.EndLine,
				Detail: fmt.Sprintf("the summary says solved=%v, replaying the clicks gives solved=%v",
					t.Solved, out.Solved),
			})
		}
		if t.NMoves >= 0 && t.NMoves != out.Slides {
			out.Problems = append(out.Problems, Problem{
				Line: t.EndLine,
				Detail: fmt.Sprintf("the summary says n_moves=%d, replaying the clicks gives %d",
					t.NMoves, out.Slides),
			})
		}
	}

	return out, nil
}

// checkFrom verifies the vehicle stood where the row says it stood.
func (r *Replayed) checkFrom(e Event, car *rush.Car) bool {
	if e.FromR < 0 || e.FromC < 0 {
		return true // the row does not say
	}
	if car.Row != e.FromR || car.Col != e.FromC {
		r.problem(e, "%s is at (%d,%d), but the file records it at (%d,%d)",
			e.Car, car.Row, car.Col, e.FromR, e.FromC)
		return false
	}
	return true
}

func (r *Replayed) problem(e Event, format string, args ...any) {
	r.Problems = append(r.Problems, Problem{Line: e.Line, Detail: fmt.Sprintf(format, args...)})
}

// direction recovers the one-cell step a click_move row describes. The row
// stores where the vehicle was and where it ended up; the difference along the
// vehicle's own axis is the direction.
func direction(car *rush.Car, e Event) (int, error) {
	dr, dc := e.ToR-e.FromR, e.ToC-e.FromC

	if car.Horizontal {
		if dr != 0 {
			return 0, fmt.Errorf("%s is horizontal but the file moves it %d row(s)", e.Car, dr)
		}
		return unitStep(dc, e.Car)
	}
	if dc != 0 {
		return 0, fmt.Errorf("%s is vertical but the file moves it %d column(s)", e.Car, dc)
	}
	return unitStep(dr, e.Car)
}

// unitStep insists on exactly one cell. One click is one cell everywhere else
// in this program, so a row claiming two is a file that was not written by it.
func unitStep(delta int, label string) (int, error) {
	switch delta {
	case rush.Left, rush.Right:
		return delta, nil
	case 0:
		return 0, fmt.Errorf("%s is recorded as moving, but from and to are the same cell", label)
	}
	return 0, fmt.Errorf("%s moves %d cells in one click, and a click is one cell", label, delta)
}

// dirName describes a direction the way a player would, which depends on how
// the vehicle is laid out.
func dirName(car *rush.Car, dir int) string {
	if car.Horizontal {
		if dir == rush.Left {
			return "left"
		}
		return "right"
	}
	if dir == rush.Up {
		return "up"
	}
	return "down"
}

func carAt(b *rush.Board, label string) *rush.Car {
	if len(label) != 1 {
		return nil
	}
	for _, c := range b.Cars {
		if c.Label == label[0] {
			return c
		}
	}
	return nil
}
