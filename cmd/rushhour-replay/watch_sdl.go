// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

//go:build rushui

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"rush-hour/internal/rush"
	"rush-hour/internal/rushreplay"
	"rush-hour/internal/rushui"

	"github.com/chrplr/goxpyriment/clock"
	"github.com/chrplr/goxpyriment/control"
)

// The watchable build: `go build -tags rushui ./cmd/rushhour-replay`. It is a
// separate build because goxpyriment carries SDL for every platform, which
// would be tens of megabytes of dead weight in a binary whose main job is to
// check a file on a machine with no display.

// defaultHoldMS paces a file that carries no usable timing. Agent runs are the
// case that matters: their rows are stamped with when the request arrived, so
// every move in one lands at nearly the same millisecond, and replaying them
// faithfully would flash the whole session past in one frame.
const defaultHoldMS = 400

// maxGapMS caps a single pause. A participant who walked away mid-trial should
// not make the window look frozen; the pause is still visible as a long one,
// and -speed still applies on top.
const maxGapMS = 5000

var (
	renderFlag *bool
	speedFlag  *float64
)

func registerViewerFlags() {
	renderFlag = flag.Bool("render", false, "open a window and watch the session play back")
	speedFlag = flag.Float64("speed", 1, "playback speed multiplier for -render")
}

func viewerEnabled() bool { return *renderFlag }

// watch plays a session back in a window, at the pace it was recorded.
//
// The timing is the reason to watch rather than read: t_ms is when each click
// happened, so the gaps between moves are the participant's, and what the
// window shows is not just the solution path but where they hesitated.
func watch(s *rushreplay.Session, lib rushreplay.Library, only int) error {
	trials := s.Trials
	if only != 0 {
		trials = nil
		for _, t := range s.Trials {
			if t.Trial == only {
				trials = append(trials, t)
			}
		}
		if len(trials) == 0 {
			return fmt.Errorf("no trial %d in this file", only)
		}
	}

	// Replay every trial up front. It is microseconds of work, and it means the
	// window never has to stop and think — and that a file the rules reject is
	// reported before a window opens rather than halfway through one.
	plays := make([]*rushreplay.Replayed, 0, len(trials))
	for _, t := range trials {
		r, err := rushreplay.Replay(t, lib)
		if err != nil {
			return err
		}
		if !r.OK() {
			fmt.Fprintf(os.Stderr, "trial %d (%s) has problems; showing it anyway:\n", t.Trial, t.Puzzle)
			for _, p := range r.Problems {
				fmt.Fprintf(os.Stderr, "  %s\n", p)
			}
		}
		plays = append(plays, r)
	}

	speed := *speedFlag
	if speed <= 0 {
		speed = 1
	}

	// NewExperiment + Initialize, deliberately not NewExperimentFromFlags: that
	// one calls flag.Parse(), which this program has already done.
	exp := control.NewExperiment("Rush Hour replay", 1024, 768, false,
		rushui.BgColor, rushui.TextColor, 28)

	// Initialize always opens a participant data file. Replaying is read-only,
	// and must not drop an empty stub beside real sessions — so goxpyriment's
	// goes to a temporary directory that is removed on the way out.
	scratch, err := os.MkdirTemp("", "rushhour-replay-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	exp.OutputDirectory = scratch

	if err := exp.Initialize(); err != nil {
		return fmt.Errorf("opening the window: %w", err)
	}
	defer func() {
		quiet := log.Writer()
		log.SetOutput(io.Discard)
		exp.End()
		log.SetOutput(quiet)
	}()
	if err := exp.SetLogicalSize(rushui.LogicalW, rushui.LogicalH); err != nil {
		return err
	}

	p := &player{plays: plays, speed: speed}
	p.reset(0)

	runErr := exp.Run(func() error {
		state := exp.PollEvents(nil)
		if state.QuitRequested {
			return control.EndLoop
		}

		// Escape is not handled here: PollEvents turns it into QuitRequested,
		// which the check above already caught.
		switch state.LastKey {
		case control.K_SPACE:
			p.paused = !p.paused
			p.rebase()
		case control.K_RIGHT:
			p.advance()
		case control.K_LEFT:
			p.back()
		case control.K_N:
			p.reset(p.trial + 1)
		case control.K_P:
			p.reset(p.trial - 1)
		case control.K_R:
			p.reset(p.trial)
		case control.K_MINUS:
			p.speed = maxf(0.125, p.speed/2)
			p.rebase()
		case control.K_EQUALS:
			p.speed = minf(16, p.speed*2)
			p.rebase()
		}
		if !p.paused {
			p.tick()
		}
		if err := rushui.DrawBoard(exp, p.board(), nil, p.status()); err != nil {
			return err
		}
		return exp.Screen.PacedFlip()
	})
	if runErr != nil && !control.IsEndLoop(runErr) {
		return runErr
	}
	return nil
}

// player is the tape head: which trial, how many of its moves have been
// applied, and when the next one is due.
type player struct {
	plays []*rushreplay.Replayed

	trial int
	step  int // moves applied so far

	paused bool
	speed  float64

	// origin is the wall-clock millisecond that step 0 of this trial
	// corresponds to.
	origin int64
}

func (p *player) current() *rushreplay.Replayed { return p.plays[p.trial] }

func (p *player) reset(trial int) {
	if trial < 0 {
		trial = 0
	}
	if trial >= len(p.plays) {
		trial = len(p.plays) - 1
	}
	p.trial, p.step = trial, 0
	p.rebase()
}

// rebase re-anchors the clock so the next move is due one gap from now, which
// is what keeps pausing, seeking and changing speed from making the tape jump.
func (p *player) rebase() { p.origin = clock.GetTime() - p.elapsedFor(p.step) }

// board is the position after the moves applied so far.
func (p *player) board() *rush.Board {
	r := p.current()
	if p.step == 0 {
		return r.Start
	}
	return r.Steps[p.step-1].Board
}

// elapsedFor is the recorded time at which a given step happened, in playback
// milliseconds. Falls back to a fixed hold for files with no usable timing.
func (p *player) elapsedFor(step int) int64 {
	r := p.current()
	var total float64
	for i := 0; i < step && i < len(r.Steps); i++ {
		total += p.gap(i)
	}
	return int64(total / p.speed)
}

// gap is how long to hold before showing move i.
func (p *player) gap(i int) float64 {
	r := p.current()
	prev := int64(0)
	if i > 0 {
		prev = r.Steps[i-1].Event.TMS
	}
	d := float64(r.Steps[i].Event.TMS - prev)
	if d <= 0 {
		return defaultHoldMS
	}
	if d > maxGapMS {
		return maxGapMS
	}
	return d
}

func (p *player) tick() {
	for p.step < len(p.current().Steps) {
		if clock.GetTime()-p.origin < p.elapsedFor(p.step+1) {
			return
		}
		p.step++
	}
}

func (p *player) advance() {
	if p.step < len(p.current().Steps) {
		p.step++
		p.rebase()
	}
}

func (p *player) back() {
	if p.step > 0 {
		p.step--
		p.rebase()
	}
}

func (p *player) status() string {
	r := p.current()
	t := r.Trial

	state := ""
	switch {
	case p.step < len(r.Steps):
		state = ""
	case r.Solved:
		state = " - SOLVED"
	case !t.Ended:
		state = " - UNFINISHED"
	default:
		state = " - NOT SOLVED"
	}
	if p.paused {
		state += " - PAUSED"
	}

	return fmt.Sprintf("trial %d/%d  %s (optimum %d)  move %d/%d  x%g%s",
		t.Trial, len(p.plays), t.Puzzle, t.MinMoves, p.step, len(r.Steps), p.speed, state)
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
