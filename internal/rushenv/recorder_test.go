// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rushenv

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rush-hour/internal/rushlog"
)

// readResults parses an agent results file, skipping the comment block, and
// returns the header and rows.
func readResults(t *testing.T, path string) ([]string, [][]string) {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var body []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		body = append(body, line)
	}
	records, err := csv.NewReader(strings.NewReader(strings.Join(body, "\n"))).ReadAll()
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if len(records) == 0 {
		t.Fatalf("%s has no rows", path)
	}
	return records[0], records[1:]
}

// An agent's results file has to be the participant file's schema — same
// columns, same order, same event names — or the two traces cannot be put side
// by side, which is the whole reason the agent writes one.
func TestRecorderWritesTheParticipantSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.csv")

	srv, err := NewServer(Options{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	rec, err := NewRecorder(path, 7, []string{"a comment"})
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	srv.Observer = rec.Observe

	mustOK(t, call(t, srv, `{"id":1,"cmd":"reset","puzzle":"p02"}`), "reset")
	sol := mustOK(t, call(t, srv, `{"id":2,"cmd":"solve"}`), "solve")
	actions := ints(t, sol, "actions")
	for _, a := range actions {
		mustOK(t, call(t, srv, fmt.Sprintf(`{"id":3,"cmd":"step","action":%d}`, a)), "step")
	}
	// One blocked action, so the file carries a refused attempt like a human's
	// does. Action 31 addresses a padding slot: nothing to click.
	mustOK(t, call(t, srv, `{"id":4,"cmd":"step","action":31}`), "step")
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	header, rows := readResults(t, path)
	want := append([]string{"subject_id"}, rushlog.Columns...)
	if strings.Join(header, ",") != strings.Join(want, ",") {
		t.Fatalf("header =\n%v\nwant\n%v", header, want)
	}

	// trial_start, one row per action, trial_end (written on the solving step),
	// then the extra blocked action.
	if len(rows) != 1+len(actions)+1+1 {
		t.Fatalf("got %d rows for %d actions", len(rows), len(actions))
	}

	col := func(row []string, name string) string {
		for i, h := range header {
			if h == name {
				return row[i]
			}
		}
		t.Fatalf("no column %q", name)
		return ""
	}

	if got := col(rows[0], "event"); got != rushlog.EventTrialStart {
		t.Errorf("first row is %q, want %q", got, rushlog.EventTrialStart)
	}
	for i, row := range rows[1 : 1+len(actions)] {
		if got := col(row, "event"); got != rushlog.EventClickMove {
			t.Errorf("row %d is %q, want %q (optimal actions all move)", i+1, got, rushlog.EventClickMove)
		}
		if col(row, "car") == "" {
			t.Errorf("row %d names no vehicle", i+1)
		}
	}

	end := rows[1+len(actions)]
	if got := col(end, "event"); got != rushlog.EventTrialEnd {
		t.Fatalf("row after the last move is %q, want %q", got, rushlog.EventTrialEnd)
	}
	if col(end, "solved") != "true" {
		t.Error("trial_end does not report the puzzle as solved")
	}
	// n_moves counts slides, so it must equal the declared optimum — the same
	// column, meaning the same thing, as in a participant's file.
	if got := col(end, "n_moves"); got != "4" {
		t.Errorf("n_moves = %s, want 4 (p02's optimum)", got)
	}
	if got := col(end, "puzzle"); got != "p02" {
		t.Errorf("puzzle = %s", got)
	}

	// The padding-slot action clicks no vehicle: the human equivalent is a
	// click on an empty cell.
	if got := col(rows[len(rows)-1], "event"); got != rushlog.EventClickEmpty {
		t.Errorf("the padding-slot action logged %q, want %q", got, rushlog.EventClickEmpty)
	}

	for _, row := range rows {
		if got := col(row, "subject_id"); got != "7" {
			t.Fatalf("subject_id = %s, want 7", got)
		}
	}
}

// Several boards in one process are several trials, each numbered on its own.
func TestRecorderNumbersTrialsPerEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.csv")

	srv, _ := NewServer(Options{})
	rec, err := NewRecorder(path, 0, nil)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	srv.Observer = rec.Observe

	for i := 0; i < 3; i++ {
		call(t, srv, `{"id":1,"cmd":"reset","env_id":0,"puzzle":"p02"}`)
	}
	call(t, srv, `{"id":2,"cmd":"reset","env_id":1,"puzzle":"p03"}`)
	rec.Close()

	header, rows := readResults(t, path)
	trialCol, puzzleCol := 0, 0
	for i, h := range header {
		switch h {
		case "trial":
			trialCol = i
		case "puzzle":
			puzzleCol = i
		}
	}

	var p02Trials, p03Trials []string
	for _, row := range rows {
		if row[puzzleCol] == "p02" {
			p02Trials = append(p02Trials, row[trialCol])
		} else {
			p03Trials = append(p03Trials, row[trialCol])
		}
	}
	if strings.Join(p02Trials, ",") != "1,2,3" {
		t.Errorf("env 0 trials = %v, want 1,2,3", p02Trials)
	}
	if strings.Join(p03Trials, ",") != "1" {
		t.Errorf("env 1 trials = %v, want 1", p03Trials)
	}
}

// The nil Recorder is what "no results file wanted" looks like, and every
// method has to tolerate it — a crash here would only ever show up in the
// build that opens a window, which is the least tested one.
func TestNilRecorderIsUsable(t *testing.T) {
	rec, err := NewRecorder("", 0, nil)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	if rec != nil {
		t.Fatal("an empty path should give no recorder")
	}
	rec.SetMousePoint(func(State) (float32, float32) { return 1, 2 })
	rec.Observe(0, kindReset, State{})
	rec.Observe(0, kindStep, State{Slot: -1})
	if err := rec.Close(); err != nil {
		t.Errorf("Close on a nil recorder: %v", err)
	}
}
