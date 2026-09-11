// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

// Rush Hour — a sliding-block puzzle used as a problem-solving task.
//
// Each trial presents a different 6×6 Rush Hour configuration. The participant
// frees the red car by sliding the other vehicles out of the way; a trial ends
// only when the red car reaches the exit on the right wall. Every action is
// written to the results file, so the full solution path (including dead ends
// and hesitations) can be reconstructed offline.
//
// The puzzle can be played with a mouse or with buttons, because it is meant to
// be run inside an MRI or a MEG as well as at a desk, and in a scanner the
// participant has neither a mouse nor a keyboard within reach — only a response
// box or a gamepad.
//
//   - With the mouse, moving is one click, one cell: a click on the half of a
//     vehicle lying on one side of its midline slides it one cell that way.
//     There is no selection state and no dragging.
//
//   - With buttons, one vehicle is always selected — outlined in white, with an
//     arrow at each end it can still move towards. Two controls step the
//     selection through the vehicles in order, and two slide the selected
//     vehicle along its own axis: that is the four-button response box, and
//     the arrow keys carry the same scheme. A gamepad's d-pad moves the
//     selection spatially over the board instead. See package
//     internal/rushinput, and the -keys / -pad / -joy flags. Choosing skips
//     vehicles that cannot move at all; -movable-only=false offers every
//     vehicle.
//
// The two produce the same rows: a move is a vehicle and a direction whichever
// device named it.
//
// Output columns: trial, puzzle, min_moves, event, t_ms, event_ts_ns, mouse_x,
// mouse_y, car, orientation, from_row, from_col, to_row, to_col, n_moves,
// solved, trial_ms.
//
// Every row carries event_ts_ns, an absolute instant on SDL's monotonic
// nanosecond clock — the one input events and display flips are stamped with,
// and so the one a trigger to an MEG or MRI will share. The companion
// -info.txt file records what that clock is and ties it to wall-clock time.
//
// Usage:
//
//	go run . [-w] [-d N] [-s <subjectID>] [-n <nPuzzles>] [-keys <spec>] ...
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"rush-hour/internal/rush"
	"rush-hour/internal/rushinput"
	"rush-hour/internal/rushlog"
	"rush-hour/internal/rushui"

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

// stamp puts a row on the session clock: event_ts_ns is the hardware instant
// the event happened, t_ms the milliseconds from the trial's onset to it.
//
// Both come from the one clock — SDL's monotonic nanosecond counter, which is
// what stamps input events and display flips, and so what a trigger to an MEG
// or MRI will be stamped with. Deriving t_ms from the row's own hardware
// timestamp rather than from when the frame got round to noticing also takes a
// frame of jitter out of every latency in the file.
//
// A press whose hardware timestamp did not reach us is stamped at the moment it
// was processed instead — at most one frame late, and never silently zero,
// because a zero in this column would look like the start of the session.
func stamp(r *rushlog.Row, onsetNS, tsNS uint64) {
	if tsNS == 0 {
		tsNS = control.TicksNS()
	}
	r.TsNS = tsNS
	if tsNS < onsetNS {
		r.TMS = 0
		return
	}
	r.TMS = int64((tsNS - onsetNS) / 1e6)
}

// carRow fills in the columns that name a vehicle and its position. A row that
// records no displacement — a blocked attempt, a change of selection — has
// from == to, which is what tells a reader nothing moved.
func carRow(r *rushlog.Row, car *rush.Car) {
	r.Car, r.Orient = string(car.Label), car.Orientation()
	r.FromR, r.FromC = car.Row, car.Col
	r.ToR, r.ToC = car.Row, car.Col
}

// selectionFor returns the vehicle a selection action lands on. With
// movableOnly (the -movable-only flag, on by default) vehicles that cannot
// move are skipped.
func selectionFor(b *rush.Board, sel *rush.Car, a rushinput.Action, movableOnly bool) *rush.Car {
	var ok func(*rush.Car) bool // nil: every vehicle is a candidate
	if movableOnly {
		ok = b.Movable
	}
	switch a {
	case rushinput.SelectUp:
		return b.NeighbourAmong(sel, -1, 0, ok)
	case rushinput.SelectDown:
		return b.NeighbourAmong(sel, 1, 0, ok)
	case rushinput.SelectLeft:
		return b.NeighbourAmong(sel, 0, -1, ok)
	case rushinput.SelectRight:
		return b.NeighbourAmong(sel, 0, 1, ok)
	case rushinput.SelectPrev:
		return b.CycleAmong(sel, -1, ok)
	case rushinput.SelectNext:
		return b.CycleAmong(sel, 1, ok)
	}
	return sel
}

// runTrial presents one puzzle and returns when it is solved (or when the
// participant quits, in which case it returns control.EndLoop).
func runTrial(exp *control.Experiment, in *rushinput.Reader, useMouse, movableOnly bool, trial int, p rush.Puzzle, nTrials int) error {
	b := p.Fresh()
	status := fmt.Sprintf("Puzzle %d/%d - free the RED car", trial, nTrials)

	// The selection starts on the red car: the one vehicle the participant is
	// certain to want, and the one the instructions have just talked about.
	sel := b.Target()

	// The trial starts when the board appears, not when this function does.
	// Drawing the first frame here and taking the onset from the flip that
	// presented it is what makes the onset a hardware instant on the same
	// clock as every press and, later, as any trigger sent to the scanner. The
	// difference is a whole frame — 16 ms at 60 Hz — which is small next to a
	// planning latency but not next to a haemodynamic or evoked response.
	if err := rushui.DrawBoard(exp, b, sel, nil, status); err != nil {
		return err
	}
	onsetNS, err := exp.Screen.FlipTS()
	if err != nil {
		return err
	}

	// Anything pressed before the board was on screen is still sitting in SDL's
	// queue — the ready page's own dismissal, or an impatient press during the
	// blank. Draining it here, *after* the onset rather than before the frame
	// that establishes it, keeps a press the participant made without seeing
	// the board from being recorded as a move against it at t_ms = 0.
	exp.PollEvents(in.Handle)
	in.Drain()

	start := rushlog.NewRow(trial, p.Name, p.MinMoves, rushlog.EventTrialStart, 0)
	start.TsNS = onsetNS
	logRow(exp, start)

	// lastMoveNS is when the participant last displaced a vehicle. The trial
	// ends with the move that frees the red car, so that press — not the frame
	// on which the program noticed — is when the trial ended.
	lastMoveNS := onsetNS

	// slides counts *slides*, not presses: consecutive one-cell steps of the
	// same vehicle in the same direction are one slide, which is the metric
	// puzzles.txt uses for min_moves. The raw presses remain one row each. The
	// agent environment counts with the same type, so the two traces compare.
	var slides rush.SlideCounter

	for {
		state := exp.PollEvents(in.Handle)
		if state.QuitRequested {
			return control.EndLoop
		}
		mx, my := exp.Screen.MousePosition()

		// Vehicle under the cursor, outlined as a hover cue.
		var hover *rush.Car
		if useMouse {
			if row, col, onBoard := rushui.CellAt(mx, my); onBoard {
				hover = b.CarAt(row, col)
			}
		}

		// ── Click: slide the clicked vehicle one cell ─────────────────────
		// Every click is logged, including the ones that move nothing, so
		// hesitations and blocked attempts stay in the record.
		if useMouse && state.LastMouseButton == control.BUTTON_LEFT {
			r := rushlog.NewRow(trial, p.Name, p.MinMoves, rushlog.EventClickEmpty, 0)
			stamp(&r, onsetNS, state.LastMouseTimestamp)
			r.MouseX, r.MouseY = mx, my

			row, col, onBoard := rushui.CellAt(mx, my)
			var car *rush.Car
			if onBoard {
				car = b.CarAt(row, col)
			}
			if car != nil {
				// A click also names the vehicle the buttons would act on, so
				// the two input routes cannot disagree about what is selected.
				sel = car
				r.Kind = rushlog.EventClickBlocked
				carRow(&r, car)

				if step := rushui.StepForPoint(car, mx, my); step != 0 {
					if b.Step(car, step) {
						slides.Add(car.ID, step)
						r.Kind = rushlog.EventClickMove
						r.ToR, r.ToC = car.Row, car.Col
						lastMoveNS = r.TsNS
					}
				}
			}
			logRow(exp, r)
		}

		// ── Buttons: choose a vehicle, or slide the chosen one ────────────
		// One press, one row, exactly as for one click.
		for _, ev := range in.Drain() {
			switch {
			case ev.Action.IsSelect():
				sel = selectionFor(b, sel, ev.Action, movableOnly)
				if sel == nil {
					continue
				}
				r := rushlog.NewRow(trial, p.Name, p.MinMoves, rushlog.EventSelect, 0)
				stamp(&r, onsetNS, ev.TsNS)
				carRow(&r, sel)
				logRow(exp, r)

			case ev.Action.IsMove():
				if sel == nil {
					continue
				}
				dir := rush.Left
				if ev.Action == rushinput.MoveForward {
					dir = rush.Right
				}
				r := rushlog.NewRow(trial, p.Name, p.MinMoves, rushlog.EventClickBlocked, 0)
				stamp(&r, onsetNS, ev.TsNS)
				// Where the equivalent click would have landed, so that a
				// button session's rows are as complete as a mouse session's.
				click := rushui.ClickPoint(sel, dir)
				r.MouseX, r.MouseY = click.X, click.Y
				carRow(&r, sel)

				if b.Step(sel, dir) {
					slides.Add(sel.ID, dir)
					r.Kind = rushlog.EventClickMove
					r.ToR, r.ToC = sel.Row, sel.Col
					lastMoveNS = r.TsNS
				}
				logRow(exp, r)
			}
		}

		// ── Solved? ───────────────────────────────────────────────────────
		if b.Solved() {
			// Timed at the move that solved it, so trial_ms is onset-to-answer
			// on the hardware clock rather than onset-to-noticed.
			r := rushlog.NewRow(trial, p.Name, p.MinMoves, rushlog.EventTrialEnd, 0)
			stamp(&r, onsetNS, lastMoveNS)
			trialMS := r.TMS
			r.NMoves = slides.N()
			r.Solved = true
			r.TrialMS = trialMS
			logRow(exp, r)

			if err := rushui.DrawBoard(exp, b, nil, nil, "PUZZLE SOLVED!"); err != nil {
				return err
			}
			if err := exp.Screen.Flip(); err != nil {
				return err
			}
			exp.Audio.PlayCorrect()
			exp.Wait(solvedFeedback)
			fmt.Printf("Puzzle %2d (%s) solved in %d moves (optimum %d), %.1f s\n",
				trial, p.Name, slides.N(), p.MinMoves, float64(trialMS)/1000)
			return nil
		}

		if err := rushui.DrawBoard(exp, b, sel, hover, status); err != nil {
			return err
		}
		if err := exp.Screen.Flip(); err != nil {
			return err
		}
		time.Sleep(frameSleepMS * time.Millisecond)
	}
}

// showScreen presents a block of text, scaled to fit, and waits for any input
// at all — a key, a button on any connected device, or a click.
//
// The layout is goxpyriment's: exp.FittedTextBox wraps the text to the drawing
// area and picks the largest point size at which the whole block fits, which is
// what this screen needs — the instructions list the live bindings, so their
// length depends on what is plugged in, and adding a line later cannot silently
// push the last one off the bottom.
//
// What is local is the waiting. exp.ShowInstructions waits for the spacebar; a
// participant lying in a scanner has no spacebar, and an instruction screen
// they cannot dismiss stops the session. Accepting anything bound — and
// anything unbound too — is the one place where being liberal costs nothing.
func showScreen(exp *control.Experiment, in *rushinput.Reader, text string) error {
	box := exp.FittedTextBox(text)
	defer box.Unload()

	if err := exp.Show(box); err != nil {
		return err
	}

	// Discard whatever arrived while the previous screen was up, so a press
	// meant for it does not skip this one.
	exp.PollEvents(in.Handle)
	in.Drain()

	for {
		state := exp.PollEvents(in.Handle)
		if state.QuitRequested {
			return control.EndLoop
		}
		if state.LastKey != 0 || state.LastMouseButton != 0 || len(in.Drain()) > 0 {
			return nil
		}
		time.Sleep(frameSleepMS * time.Millisecond)
	}
}

// instructions builds the text of the first screen. The control list is
// generated from the live bindings rather than written out, so a site that
// remaps its response box shows the participant its own buttons.
func instructions(nPuzzles int, in *rushinput.Reader, useMouse, movableOnly bool) string {
	var b strings.Builder
	b.WriteString("RUSH HOUR\n\n" +
		"On each puzzle, get the RED car out through the opening\n" +
		"on the right wall.\n\n" +
		"Vehicles only slide along their own axis, and cannot pass\n" +
		"through each other.\n\n")

	if useMouse {
		b.WriteString("To move a vehicle one cell, click on the side of it that points\n" +
			"the way you want it to go.\n\n")
	}

	if lines := in.Map.Legend(len(in.Devices()) > 0); len(lines) > 0 {
		b.WriteString("One car is outlined in white; the arrows on it show which ways\n" +
			"it can still move.\n\n")
		if movableOnly {
			b.WriteString("Cars that cannot move at all are skipped: choosing only ever\n" +
				"lands on a car with at least one arrow.\n\n")
		}
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n\n")
	}

	fmt.Fprintf(&b, "There are %d puzzles, in increasing order of difficulty.\n"+
		"Take all the time you need.\n\n"+
		"Press any button to begin.", nPuzzles)
	return b.String()
}

// writeClockAnchor records what the event_ts_ns column is on, and ties it to
// wall-clock time.
//
// Every row carries a timestamp from SDL's monotonic nanosecond clock, which
// starts at an arbitrary instant when SDL initialises. That clock is the right
// basis for synchronising with an MEG or MRI recording — it is the one the
// hardware event timestamps and the display flips are on, so it is the one a
// trigger will be stamped with too — but on its own it says nothing about when
// the session happened. This anchor is the missing half.
//
// It is sampled by bracketing a wall-clock reading between two clock reads and
// taking the midpoint, so the pairing carries its own uncertainty rather than
// an unstated one.
//
// Wall-clock is for coarse alignment and for finding the session in an
// acquisition log. It is NOT how to align the two records: the two machines'
// clocks drift, and by far more than the effects being measured. Alignment
// belongs to a shared event — a scanner pulse, or a trigger — recorded on the
// clock below.
func writeClockAnchor(exp *control.Experiment) {
	before := control.TicksNS()
	wall := time.Now()
	after := control.TicksNS()

	exp.Data.WriteComment("--CLOCK")
	exp.Data.WriteComment("c event_ts_ns: SDL monotonic nanosecond clock (SDL_GetTicksNS), on every row")
	exp.Data.WriteComment("c t_ms: milliseconds after that trial's trial_start row, from the same clock")
	exp.Data.WriteComment(fmt.Sprintf("c anchor_sdl_ns: %d", (before+after)/2))
	exp.Data.WriteComment(fmt.Sprintf("c anchor_utc: %s", wall.UTC().Format(time.RFC3339Nano)))
	exp.Data.WriteComment(fmt.Sprintf("c anchor_uncertainty_ns: %d", (after-before)/2))
	exp.Data.WriteComment("c sync: align to an acquisition system with a trigger or scanner pulse " +
		"stamped on the SDL clock, not with the wall-clock anchor, which drifts")
	exp.Data.WriteComment("#")
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

	actions := strings.Join(rushinput.ActionNames(), " ")
	keySpec := flag.String("keys", "", "keyboard bindings to override, e.g. \"b=back,y=forward,g=prev,r=next\" (actions: "+actions+" none)")
	padSpec := flag.String("pad", "", "gamepad-button bindings to override, e.g. \"north=confirm\"")
	joySpec := flag.String("joy", "", "raw joystick-button bindings to override, by button number, e.g. \"0=back,1=forward\"")
	useAxes := flag.Bool("axes", true, "let analog sticks, hats and triggers act as buttons (turn off for a response box whose unused axes drift)")
	noMouse := flag.Bool("no-mouse", false, "hide the cursor and ignore clicks — the scanner case, where a stray click is not the participant")
	movableOnly := flag.Bool("movable-only", true, "when choosing with buttons, skip vehicles that cannot move at all; -movable-only=false offers every vehicle, so the participant has to work out which are stuck")
	inputDebug := flag.Bool("input-debug", false, "echo every key and button press to stderr, with the name -keys/-pad/-joy would use for it")

	exp := control.NewExperimentFromFlags("Rush Hour", rushui.BgColor, rushui.TextColor, 28)
	defer exp.End()

	// The bindings are parsed here rather than beside the flag declarations
	// because NewExperimentFromFlags is what calls flag.Parse. A bad spec ends
	// the run through exp.Fatal, which shuts SDL down first — the experiment
	// already owns the window by this point.
	// A stray word after the flags is almost always "-movable-only false":
	// Go's flag package reads that as the bare flag (true) followed by an
	// argument it ignores, so the session would silently run with the
	// opposite setting. Refuse rather than guess.
	if args := flag.Args(); len(args) > 0 {
		exp.Fatal("Rush-Hour: unexpected argument %q (a boolean flag takes -flag=false, not -flag false)", args[0])
	}
	m := rushinput.DefaultMap()
	if err := m.ApplyKeys(*keySpec); err != nil {
		exp.Fatal("Rush-Hour: -keys: %v", err)
	}
	if err := m.ApplyPad(*padSpec); err != nil {
		exp.Fatal("Rush-Hour: -pad: %v", err)
	}
	if err := m.ApplyJoy(*joySpec); err != nil {
		exp.Fatal("Rush-Hour: -joy: %v", err)
	}

	in := rushinput.New(m)
	in.Axes = *useAxes
	in.Debug = *inputDebug
	defer in.Close()

	if *nPuzzles > 0 && *nPuzzles < len(puzzles) {
		puzzles = puzzles[:*nPuzzles]
	}

	if err := exp.SetLogicalSize(rushui.LogicalW, rushui.LogicalH); err != nil {
		log.Printf("Warning: could not set logical size: %v", err)
	}

	exp.AddDataVariableNames(rushlog.Columns)

	useMouse := !*noMouse

	err = exp.Run(func() error {
		// Opening the controllers needs SDL up, so it happens here rather than
		// beside the flag parsing: nothing arrives from a gamepad until it has
		// been opened.
		for _, name := range in.Open() {
			fmt.Printf("Input device: %s\n", name)
		}
		writeClockAnchor(exp)

		if err := exp.Mouse.ShowCursor(useMouse); err != nil {
			log.Printf("Warning: could not set cursor visibility: %v", err)
		}
		if err := showScreen(exp, in, instructions(len(puzzles), in, useMouse, *movableOnly)); err != nil {
			return err
		}

		for i, p := range puzzles {
			// A self-paced gate between puzzles. The trials have no time
			// limit and the hard ones run for minutes, so the participant
			// needs somewhere to rest that is not the middle of a puzzle —
			// and in a scanner, where they cannot say "wait", the only way
			// to offer that is to stop and require a press.
			//
			// It also fixes the start of each trial: the board appears a
			// constant itiMS after a press the participant chose to make,
			// rather than at whatever moment the previous trial happened to
			// end. Not before the first puzzle, where the instruction screen
			// has just asked for the same press.
			if i > 0 {
				ready := fmt.Sprintf("Puzzle %d of %d\n\nPress any key or button to start.", i+1, len(puzzles))
				if err := showScreen(exp, in, ready); err != nil {
					return err
				}
			}

			exp.Blank(itiMS)
			if err := runTrial(exp, in, useMouse, *movableOnly, i+1, p, len(puzzles)); err != nil {
				return err
			}
			exp.Data.Save() // flush after every puzzle — ESC must not cost data
		}

		showScreen(exp, in, "All puzzles completed!\n\nThank you for your participation.\n\nPress any button to exit.")
		return control.EndLoop
	})

	if err != nil && !control.IsEndLoop(err) {
		exp.Fatal("experiment error: %v", err)
	}
}
