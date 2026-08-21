// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

// Package rushreplay reads a results file back and re-plays it.
//
// A results file records what a session did, not what the board looked like:
// one row per click, naming the vehicle and where it went. Rush Hour is
// deterministic and has no hidden state, so replaying those rows against a
// fresh copy of the named puzzle reconstructs every intermediate position
// exactly. Nothing else has to be stored for that to work, which is why the
// files are as small as they are.
//
// Because each click_move row carries both the vehicle's position before the
// move and its position after, a replay can do more than reconstruct: it can
// check itself. If re-applying the log lands a vehicle somewhere other than
// where the log says it landed, the file and the rules disagree, and Verify
// says so with the line number. That is a stronger guarantee than a checksum
// over the final state, because it localises the disagreement to one click.
//
// The reader accepts both writers' output — the participant file that goes
// through goxpyriment's data file and the agent file written by rushlog.Writer
// — by looking columns up by name. That is also what makes it survive a column
// being added in front, which is exactly what subject_id is.
package rushreplay

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"rush-hour/internal/rushlog"
)

// Event is one recorded row of a trial.
type Event struct {
	Kind string
	TMS  int64
	TsNS uint64

	MouseX, MouseY float32

	Car    string
	Orient string

	FromR, FromC int
	ToR, ToC     int

	// Line is the 1-based line in the source file, so a complaint about a row
	// can be traced back to it.
	Line int
}

// Moved reports whether the event is a click that displaced a vehicle.
func (e Event) Moved() bool { return e.Kind == rushlog.EventClickMove }

// Trial is one puzzle attempt: its rows in order, plus whatever the trial_end
// row claimed about it.
type Trial struct {
	Trial    int
	Puzzle   string
	MinMoves int

	Events []Event

	// Ended is false when the file stops mid-trial — the participant quit, or
	// the process was killed. The rows before that point are still sound.
	Ended   bool
	NMoves  int
	Solved  bool
	TrialMS int64
	// EndLine is the line the summary row came from, so a disagreement with it
	// can be pointed at as precisely as one about a click.
	EndLine int
}

// Session is a parsed results file.
type Session struct {
	// SubjectID is -1 when the file has no subject_id column.
	SubjectID int
	// Comments are the leading # lines, which the agent writer uses to record
	// the command line and version.
	Comments []string
	Trials   []*Trial
}

// Clicks counts every recorded click in the session, including the ones that
// moved nothing. Hesitation is data: a blocked click is a decision the player
// made and then could not carry out.
func (s *Session) Clicks() int {
	return s.count(rushlog.EventClickMove, rushlog.EventClickBlocked, rushlog.EventClickEmpty)
}

// Selects counts the presses that moved the selection from one vehicle to
// another without moving the board, which only a session played on a response
// box or a gamepad produces. It is 0 for a mouse session and for an agent's
// file.
func (s *Session) Selects() int { return s.count(rushlog.EventSelect) }

func (s *Session) count(kinds ...string) int {
	var n int
	for _, t := range s.Trials {
		for _, e := range t.Events {
			for _, k := range kinds {
				if e.Kind == k {
					n++
					break
				}
			}
		}
	}
	return n
}

// ReadFile parses a results file from disk.
func ReadFile(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Read parses a results file.
func Read(r io.Reader) (*Session, error) {
	// Collect the # preamble before handing the rest to the CSV reader, which
	// is set to skip those lines but does not keep them.
	var (
		comments []string
		body     strings.Builder
	)
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "#") {
			comments = append(comments, strings.TrimSpace(strings.TrimPrefix(line, "#")))
			continue
		}
		body.WriteString(line)
		body.WriteString("\n")
	}

	cr := csv.NewReader(strings.NewReader(body.String()))
	cr.Comment = '#'
	// Rows are fixed-width, but a file cut off mid-write should report the
	// truncated row rather than failing the whole parse.
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("empty results file")
	}
	if err != nil {
		return nil, fmt.Errorf("reading the header: %w", err)
	}

	col := make(map[string]int, len(header))
	for i, name := range header {
		col[strings.TrimSpace(name)] = i
	}
	// These are the columns a replay cannot do without. subject_id, the mouse
	// position and the timings are all optional.
	for _, name := range []string{"trial", "puzzle", "event", "from_row", "from_col", "to_row", "to_col", "car"} {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("not a results file: no %q column", name)
		}
	}

	s := &Session{SubjectID: -1, Comments: comments}
	var current *Trial
	line := 1 // the header

	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line+1, err)
		}
		line++
		if len(rec) < len(header) {
			// A half-written final row: the writer was killed. Keep everything
			// before it rather than rejecting the file.
			break
		}

		get := func(name string) string {
			i, ok := col[name]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}

		if s.SubjectID < 0 {
			if v, err := strconv.Atoi(get("subject_id")); err == nil {
				s.SubjectID = v
			}
		}

		trial := atoiOr(get("trial"), -1)
		kind := get("event")

		if current == nil || current.Trial != trial {
			current = &Trial{
				Trial:    trial,
				Puzzle:   get("puzzle"),
				MinMoves: atoiOr(get("min_moves"), 0),
				NMoves:   -1,
			}
			s.Trials = append(s.Trials, current)
		}

		switch kind {
		case rushlog.EventTrialStart:
			// Nothing to apply; it carries the onset only.
		case rushlog.EventTrialEnd:
			current.Ended = true
			current.NMoves = atoiOr(get("n_moves"), -1)
			current.Solved = get("solved") == "true"
			current.TrialMS = int64(atoiOr(get("trial_ms"), -1))
			current.EndLine = line
		default:
			current.Events = append(current.Events, Event{
				Kind:   kind,
				TMS:    int64(atoiOr(get("t_ms"), 0)),
				TsNS:   uint64(atoiOr(get("event_ts_ns"), 0)),
				MouseX: float32(atofOr(get("mouse_x"), 0)),
				MouseY: float32(atofOr(get("mouse_y"), 0)),
				Car:    get("car"),
				Orient: get("orientation"),
				FromR:  atoiOr(get("from_row"), -1),
				FromC:  atoiOr(get("from_col"), -1),
				ToR:    atoiOr(get("to_row"), -1),
				ToC:    atoiOr(get("to_col"), -1),
				Line:   line,
			})
		}
	}

	if len(s.Trials) == 0 {
		return nil, fmt.Errorf("no trials in the file")
	}
	return s, nil
}

func atoiOr(s string, def int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

func atofOr(s string, def float64) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}
