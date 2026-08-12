// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

// Rush Hour — a sliding-block puzzle used as a problem-solving task.
//
// Each trial presents a different 6×6 Rush Hour configuration. The participant
// frees the red car by sliding the other vehicles out of the way; a trial ends
// only when the red car reaches the exit on the right wall. Every mouse action
// is written to the results file, so the full solution path (including dead
// ends and hesitations) can be reconstructed offline.
//
// Moving is one click, one cell: a click on the half of a vehicle lying on one
// side of its midline slides it one cell that way. There is no selection state
// and no dragging — a click either moves a vehicle by exactly one cell, or
// (wall, neighbour, or no vehicle under the cursor) moves nothing.
//
// Output columns: trial, puzzle, min_moves, event, t_ms, event_ts_ns, mouse_x,
// mouse_y, car, orientation, from_row, from_col, to_row, to_col, n_moves,
// solved, trial_ms.
//
// Usage:
//
//	go run ./examples/Rush-Hour [-w] [-d N] [-s <subjectID>] [-n <nPuzzles>]
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"rush-hour/internal/rush"
	"rush-hour/internal/rushlog"
	"rush-hour/internal/rushui"

	"github.com/chrplr/goxpyriment/clock"
	"github.com/chrplr/goxpyriment/control"
)

const (
	itiMS          = 800  // blank screen between puzzles
	solvedFeedback = 1200 // how long the solved board stays on screen (ms)
	frameSleepMS   = 2    // polling granularity inside the trial loop
)

// logRow appends one row to the data file. The columns and their order live in
// package rushlog, which the agent environment writes too, so a session played
// by a participant and one played by an agent produce the same file.
func logRow(exp *control.Experiment, r rushlog.Row) {
	exp.Data.Add(r.Values()...)
}

// runTrial presents one puzzle and returns when it is solved (or when the
// participant quits, in which case it returns control.EndLoop).
func runTrial(exp *control.Experiment, trial int, p rush.Puzzle, nTrials int) error {
	b := p.Fresh()
	status := fmt.Sprintf("Puzzle %d/%d - free the RED car", trial, nTrials)

	onset := clock.GetTime()
	logRow(exp, rushlog.NewRow(trial, p.Name, p.MinMoves, rushlog.EventTrialStart, 0))

	// slides counts *slides*, not clicks: consecutive one-cell steps of the
	// same vehicle in the same direction are one slide, which is the metric
	// puzzles.txt uses for min_moves. The raw clicks remain one row each. The
	// agent environment counts with the same type, so the two traces compare.
	var slides rush.SlideCounter

	for {
		state := exp.PollEvents(nil)
		if state.QuitRequested {
			return control.EndLoop
		}
		mx, my := exp.Screen.MousePosition()
		now := clock.GetTime() - onset

		// Vehicle under the cursor, outlined in white as a hover cue.
		var hover *rush.Car
		if row, col, onBoard := rushui.CellAt(mx, my); onBoard {
			hover = b.CarAt(row, col)
		}

		// ── Click: slide the clicked vehicle one cell ─────────────────────
		// Every click is logged, including the ones that move nothing, so
		// hesitations and blocked attempts stay in the record.
		if state.LastMouseButton == control.BUTTON_LEFT {
			r := rushlog.NewRow(trial, p.Name, p.MinMoves, rushlog.EventClickEmpty, now)
			r.TsNS = state.LastMouseTimestamp
			r.MouseX, r.MouseY = mx, my

			row, col, onBoard := rushui.CellAt(mx, my)
			var car *rush.Car
			if onBoard {
				car = b.CarAt(row, col)
			}
			if car != nil {
				r.Kind = rushlog.EventClickBlocked
				r.Car, r.Orient = string(car.Label), car.Orientation()
				r.FromR, r.FromC = car.Row, car.Col
				r.ToR, r.ToC = car.Row, car.Col

				if step := rushui.StepForPoint(car, mx, my); step != 0 {
					if b.Step(car, step) {
						slides.Add(car.ID, step)
						r.Kind = rushlog.EventClickMove
						r.ToR, r.ToC = car.Row, car.Col
					}
				}
			}
			logRow(exp, r)
		}

		// ── Solved? ───────────────────────────────────────────────────────
		if b.Solved() {
			trialMS := clock.GetTime() - onset
			r := rushlog.NewRow(trial, p.Name, p.MinMoves, rushlog.EventTrialEnd, trialMS)
			r.NMoves = slides.N()
			r.Solved = true
			r.TrialMS = trialMS
			logRow(exp, r)

			if err := rushui.DrawBoard(exp, b, nil, "PUZZLE SOLVED!"); err != nil {
				return err
			}
			if err := exp.Screen.PacedFlip(); err != nil {
				return err
			}
			exp.Audio.PlayCorrect()
			exp.Wait(solvedFeedback)
			fmt.Printf("Puzzle %2d (%s) solved in %d moves (optimum %d), %.1f s\n",
				trial, p.Name, slides.N(), p.MinMoves, float64(trialMS)/1000)
			return nil
		}

		if err := rushui.DrawBoard(exp, b, hover, status); err != nil {
			return err
		}
		if err := exp.Screen.PacedFlip(); err != nil {
			return err
		}
		time.Sleep(frameSleepMS * time.Millisecond)
	}
}

func main() {
	puzzles, err := rush.DefaultPuzzles()
	if err != nil {
		log.Fatalf("Rush-Hour: %v", err)
	}

	// Registered before NewExperimentFromFlags, which calls flag.Parse().
	// The library holds 49 puzzles from 3 to 51 moves — far more than one
	// session usually needs, so a run normally takes a prefix of the ramp.
	nPuzzles := flag.Int("n", 12, "number of puzzles to present, from the easiest (0 = all)")

	exp := control.NewExperimentFromFlags("Rush Hour", rushui.BgColor, rushui.TextColor, 28)
	defer exp.End()

	if *nPuzzles > 0 && *nPuzzles < len(puzzles) {
		puzzles = puzzles[:*nPuzzles]
	}

	if err := exp.SetLogicalSize(rushui.LogicalW, rushui.LogicalH); err != nil {
		log.Printf("Warning: could not set logical size: %v", err)
	}

	exp.AddDataVariableNames(rushlog.Columns)

	instructions := fmt.Sprintf(
		"RUSH HOUR\n\n"+
			"On each puzzle, get the RED car out through the opening\n"+
			"on the right wall.\n\n"+
			"Vehicles only slide along their own axis, and cannot pass\n"+
			"through each other.\n\n"+
			"To move a vehicle one cell, click on the side of it that points\n"+
			"the way you want it to go.\n\n"+
			"There are %d puzzles, in increasing order of difficulty.\n"+
			"Take all the time you need.\n\n"+
			"Press SPACE to begin.",
		len(puzzles))

	err = exp.Run(func() error {
		if err := exp.Mouse.ShowCursor(true); err != nil {
			log.Printf("Warning: could not show cursor: %v", err)
		}
		if err := exp.ShowInstructions(instructions); err != nil {
			return err
		}

		for i, p := range puzzles {
			exp.Blank(itiMS)
			if err := runTrial(exp, i+1, p, len(puzzles)); err != nil {
				return err
			}
			exp.Data.Save() // flush after every puzzle — ESC must not cost data
		}

		exp.ShowEndMessage("All puzzles completed!\n\nThank you for your participation.\n\nPress any key to exit.")
		return control.EndLoop
	})

	if err != nil && !control.IsEndLoop(err) {
		exp.Fatal("experiment error: %v", err)
	}
}
