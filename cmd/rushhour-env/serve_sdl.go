// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

//go:build rushui

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"rush-hour/internal/rush"
	"rush-hour/internal/rushenv"
	"rush-hour/internal/rushui"

	"github.com/chrplr/goxpyriment/control"
)

// The watchable build: `go build -tags rushui ./cmd/rushhour-env`. It is a
// separate build because goxpyriment carries SDL for every platform, which
// would be tens of megabytes of dead weight in a training loop that never opens
// a window.
//
// Threading is not a choice here. goxpyriment pins SDL to the goroutine running
// exp.Run, and every Screen call has to stay on it. So the protocol reader is
// the background goroutine and the drawing loop is the main one, not the other
// way round.

var (
	renderFlag  *bool
	renderEvery *int
	renderHold  *int
)

func registerViewerFlags() {
	renderFlag = flag.Bool("render", false, "open a window and draw the board the agent is playing")
	renderEvery = flag.Int("render-every", 1, "draw one frame every N steps")
	renderHold = flag.Int("render-hold", 0, "pause this many ms after each drawn step, to watch at a human pace")
}

func viewerEnabled() bool { return *renderFlag }

func serve(srv *rushenv.Server, recorder *rushenv.Recorder) error {
	if !*renderFlag {
		return srv.Run(os.Stdin, os.Stdout)
	}
	return serveWindowed(srv, recorder)
}

func serveWindowed(srv *rushenv.Server, recorder *rushenv.Recorder) error {
	// NewExperiment + Initialize, deliberately not NewExperimentFromFlags:
	// that one calls flag.Parse(), which this program has already done.
	exp := control.NewExperiment("Rush Hour agent", 1024, 768, false,
		rushui.BgColor, rushui.TextColor, 28)

	// Initialize always opens a participant data file. This program writes its
	// own results file, on -csv, and must not drop an empty stub into the
	// participant data directory beside real sessions — so goxpyriment's goes
	// to a temporary directory that is removed on the way out.
	scratch, err := os.MkdirTemp("", "rushhour-env-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch) // after End(), which writes the file's footer
	exp.OutputDirectory = scratch

	if err := exp.Initialize(); err != nil {
		return fmt.Errorf("opening the window: %w", err)
	}
	// End announces where it filed that data. The path is the scratch directory
	// above and is about to be deleted, so printing it would only send someone
	// looking for a file that is not there.
	defer func() {
		quiet := log.Writer()
		log.SetOutput(io.Discard)
		exp.End()
		log.SetOutput(quiet)
	}()
	if err := exp.SetLogicalSize(rushui.LogicalW, rushui.LogicalH); err != nil {
		return err
	}

	// With a window open, an agent's action has a screen position: the pixel a
	// participant would have had to click for that same move.
	recorder.SetMousePoint(clickPoint)

	// Which board to show, and how far along it is. Written by the observer,
	// which runs inside srv.Handle — on this goroutine — so no lock is needed.
	watched, steps := 0, 0
	previous := srv.Observer
	srv.Observer = func(envID int, kind string, st rushenv.State) {
		if previous != nil {
			previous(envID, kind, st)
		}
		watched = envID
		steps++
	}

	lines := make(chan []byte)
	scanErr := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
		for sc.Scan() {
			// The scanner reuses its buffer, and this crosses a goroutine.
			lines <- append([]byte(nil), sc.Bytes()...)
		}
		scanErr <- sc.Err()
		close(lines)
	}()

	out := bufio.NewWriter(os.Stdout)
	enc := json.NewEncoder(out)
	drawn := 0

	runErr := exp.Run(func() error {
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					return control.EndLoop // stdin closed: the client is gone
				}
				resp, quit := srv.Handle(line)
				if err := enc.Encode(resp); err != nil {
					return err
				}
				if err := out.Flush(); err != nil {
					return err
				}
				if quit {
					return control.EndLoop
				}

			case <-time.After(16 * time.Millisecond):
				// Idle: fall through to redraw, so the window keeps answering
				// the window manager while the agent is thinking.
			}

			if state := exp.PollEvents(nil); state.QuitRequested {
				return control.EndLoop
			}

			if steps != drawn {
				drawn = steps
				if *renderEvery > 1 && drawn%*renderEvery != 0 {
					continue
				}
				if err := drawWatched(exp, srv, watched); err != nil {
					return err
				}
				if *renderHold > 0 {
					exp.Wait(*renderHold)
				}
			}
		}
	})
	if runErr != nil && !control.IsEndLoop(runErr) {
		return runErr
	}
	select {
	case err := <-scanErr:
		return err
	default:
		return nil
	}
}

func drawWatched(exp *control.Experiment, srv *rushenv.Server, envID int) error {
	board := srv.Board(envID)
	if board == nil {
		return nil
	}
	status := fmt.Sprintf("agent - env %d", envID)
	if p, ok := srv.Puzzle(envID); ok {
		status = fmt.Sprintf("agent - %s (optimum %d moves)", p.Name, p.MinMoves)
	}
	if board.Solved() {
		status += " - SOLVED"
	}
	if err := rushui.DrawBoard(exp, board, nil, status); err != nil {
		return err
	}
	return exp.Screen.PacedFlip()
}

// clickPoint gives an action the screen position a participant's click would
// have had, so the mouse columns of an agent's results file mean the same thing
// as a participant's.
func clickPoint(st rushenv.State) (x, y float32) {
	if st.Slot < 0 || st.Slot >= len(st.Cars) {
		return 0, 0
	}
	geometry := st.Cars[st.Slot]
	// The click happens before the move, so the position is the one it came
	// from; only the length and orientation are read from the current row.
	car := &rush.Car{
		Row:        st.From[0],
		Col:        st.From[1],
		Length:     geometry[2],
		Horizontal: geometry[3] == 1,
	}
	p := rushui.ClickPoint(car, st.Dir)
	return p.X, p.Y
}
