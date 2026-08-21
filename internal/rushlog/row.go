// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

// Package rushlog holds the results-file schema, so that a session played by a
// participant and a session played by an agent produce the same columns in the
// same order.
//
// That is the point of the package existing at all: the two writers are
// different (the experiment goes through goxpyriment's data file, the agent
// environment writes plain CSV), and the only way the two traces stay
// comparable is if the description of a row lives in exactly one place.
package rushlog

// Columns is the results-file header, in order.
var Columns = []string{
	"trial", "puzzle", "min_moves", "event", "t_ms", "event_ts_ns",
	"mouse_x", "mouse_y", "car", "orientation",
	"from_row", "from_col", "to_row", "to_col",
	"n_moves", "solved", "trial_ms",
}

// Event kinds. An agent cannot click on empty space, so EventClickEmpty appears
// only in human files, and only a participant using a response box produces
// EventSelect; everything else is common to both.
//
// The click_* names are older than the button interface and are kept for every
// device, because what they record is a vehicle and a direction, not a pointer.
// A move made with a gamepad writes the same row a click on that half of that
// vehicle would have written — including mouse_x/mouse_y, which get the
// position of the click that was not made (rushui.ClickPoint), as the agent
// environment has always done. Sessions collected before and after the button
// interface therefore analyse identically.
const (
	EventTrialStart   = "trial_start"
	EventClickMove    = "click_move"
	EventClickBlocked = "click_blocked"
	EventClickEmpty   = "click_empty"
	EventTrialEnd     = "trial_end"

	// EventSelect is a press that moved the selection from one vehicle to
	// another without moving anything on the board. It is the button
	// interface's counterpart to the mouse hover the old files could not
	// record: the vehicles a participant considered and passed over.
	EventSelect = "select"
)

// Row is one line of the results file. The positional fields are -1 on rows
// where they do not apply, and the summary fields are filled only on the
// trial_end row.
type Row struct {
	Trial    int
	Puzzle   string
	MinMoves int

	Kind string
	TMS  int64
	TsNS uint64

	// MouseX/MouseY are the click position in screen coordinates, and stay 0 on
	// rows that no click produced — as they have since the experiment started
	// writing these files, so old and new data stay comparable.
	MouseX float32
	MouseY float32

	Car    string
	Orient string

	FromR, FromC int
	ToR, ToC     int

	// NMoves counts slides, not steps: it is what rush.SlideCounter reports.
	NMoves  int
	Solved  bool
	TrialMS int64
}

// NewRow starts a row with the "does not apply" defaults in place.
func NewRow(trial int, puzzle string, minMoves int, kind string, tMS int64) Row {
	return Row{
		Trial: trial, Puzzle: puzzle, MinMoves: minMoves,
		Kind: kind, TMS: tMS,
		FromR: -1, FromC: -1, ToR: -1, ToC: -1,
		NMoves: -1, TrialMS: -1,
	}
}

// Values returns the fields in Columns order, ready for a variadic data-file
// call or for a CSV writer.
func (r Row) Values() []any {
	return []any{
		r.Trial, r.Puzzle, r.MinMoves, r.Kind, r.TMS, r.TsNS,
		r.MouseX, r.MouseY, r.Car, r.Orient,
		r.FromR, r.FromC, r.ToR, r.ToC,
		r.NMoves, r.Solved, r.TrialMS,
	}
}
