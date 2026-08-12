// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rushenv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"rush-hour/internal/rush"
)

// run drives a server through a script of request lines and returns the parsed
// responses. Everything here goes through the wire format, because the wire
// format is the contract: a field renamed in Go is a client broken in Python.
func run(t *testing.T, opts Options, lines ...string) []map[string]any {
	t.Helper()

	srv, err := NewServer(opts)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	var out bytes.Buffer
	if err := srv.Run(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return parse(t, out.String())
}

func parse(t *testing.T, output string) []map[string]any {
	t.Helper()

	var got []map[string]any
	for i, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line == "" {
			t.Fatalf("response %d is an empty line", i)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("response %d is not JSON: %v\n%s", i, err, line)
		}
		got = append(got, m)
	}
	return got
}

func num(t *testing.T, m map[string]any, key string) int {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("response has no %q: %v", key, m)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("%q is %T, want a number", key, v)
	}
	return int(f)
}

func ints(t *testing.T, m map[string]any, key string) []int {
	t.Helper()
	raw, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%q is %T, want an array: %v", key, m[key], m)
	}
	out := make([]int, len(raw))
	for i, v := range raw {
		out[i] = int(v.(float64))
	}
	return out
}

func mustOK(t *testing.T, m map[string]any, what string) map[string]any {
	t.Helper()
	if ok, _ := m["ok"].(bool); !ok {
		t.Fatalf("%s failed: %v", what, m)
	}
	return m
}

// ── Framing ──────────────────────────────────────────────────────────────────

// One request, one response, in order, on its own line — the invariant the
// client's strict alternation depends on. A board string contains newlines,
// which is exactly the case where a hand-rolled framing would break.
func TestOneLinePerResponseWithIDsInOrder(t *testing.T) {
	got := run(t, Options{IncludeBoard: true},
		`{"id":11,"cmd":"hello"}`,
		`{"id":22,"cmd":"reset","puzzle":"p02"}`,
		`{"id":33,"cmd":"step","action":1}`,
		`{"id":44,"cmd":"close"}`,
	)
	if len(got) != 4 {
		t.Fatalf("got %d responses, want 4", len(got))
	}
	for i, want := range []int{11, 22, 33, 44} {
		if id := num(t, got[i], "id"); id != want {
			t.Errorf("response %d has id %d, want %d", i, id, want)
		}
	}
	if board, _ := got[1]["board"].(string); !strings.Contains(board, "\n") {
		t.Errorf("board should be the six-line notation, got %q", board)
	}
}

// Only the protocol may go to stdout: a stray print desynchronises the client
// on the spot.
func TestOutputIsNothingButJSONLines(t *testing.T) {
	srv, err := NewServer(Options{IncludeBoard: true})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	var out bytes.Buffer
	script := []string{
		`{"id":1,"cmd":"hello"}`,
		`{"id":2,"cmd":"reset","puzzle":"p01"}`,
		`not json at all`,
		`{"id":4,"cmd":"nonsense"}`,
		`{"id":5,"cmd":"solve"}`,
	}
	if err := srv.Run(strings.NewReader(strings.Join(script, "\n")+"\n"), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := parse(t, out.String()); len(got) != len(script) {
		t.Fatalf("got %d responses for %d requests", len(got), len(script))
	}
}

// End of input is the ordinary way this process dies: the parent closes the
// pipe and the server exits cleanly rather than erroring.
func TestEndOfInputIsNotAnError(t *testing.T) {
	srv, _ := NewServer(Options{})
	var out bytes.Buffer
	if err := srv.Run(strings.NewReader(`{"id":1,"cmd":"hello"}`), &out); err != nil {
		t.Fatalf("Run at EOF: %v", err)
	}
}

// close answers, then stops reading — later lines are not processed.
func TestCloseStopsTheLoop(t *testing.T) {
	got := run(t, Options{},
		`{"id":1,"cmd":"close"}`,
		`{"id":2,"cmd":"hello"}`,
	)
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1 (close should end the loop)", len(got))
	}
}

// ── Handshake ────────────────────────────────────────────────────────────────

func TestHelloDescribesTheSpaces(t *testing.T) {
	got := run(t, Options{}, `{"id":1,"cmd":"hello"}`)
	h := mustOK(t, got[0], "hello")

	if p := num(t, h, "protocol"); p != Protocol {
		t.Errorf("protocol = %d, want %d", p, Protocol)
	}
	if g := num(t, h, "grid_size"); g != rush.GridSize {
		t.Errorf("grid_size = %d, want %d", g, rush.GridSize)
	}
	maxV := num(t, h, "max_vehicles")
	if n := num(t, h, "n_actions"); n != 2*maxV {
		t.Errorf("n_actions = %d, want %d", n, 2*maxV)
	}

	catalog, ok := h["puzzles"].([]any)
	if !ok || len(catalog) == 0 {
		t.Fatalf("hello carries no puzzle catalogue")
	}
	// The action space has to be able to name every vehicle of every board.
	for _, entry := range catalog {
		e := entry.(map[string]any)
		if n := num(t, e, "n_cars"); n > maxV {
			t.Errorf("%v has %d vehicles, more than max_vehicles=%d", e["name"], n, maxV)
		}
	}
}

// A library that does not fit the action space must be refused at startup, not
// silently truncated: the extra vehicles would simply be unplayable.
func TestServerRefusesTooManyVehicles(t *testing.T) {
	puzzles, err := rush.DefaultPuzzles()
	if err != nil {
		t.Fatalf("DefaultPuzzles: %v", err)
	}
	if _, err := NewServer(Options{Puzzles: puzzles, MaxVehicles: 4}); err == nil {
		t.Error("NewServer accepted a library too large for the action space")
	}
}

// ── Observations ─────────────────────────────────────────────────────────────

// Slot 0 is the red car on every board. It is the one index that means the same
// thing across puzzles, so an agent can transfer at least that much.
func TestSlotZeroIsAlwaysTheRedCar(t *testing.T) {
	puzzles, err := rush.DefaultPuzzles()
	if err != nil {
		t.Fatalf("DefaultPuzzles: %v", err)
	}
	var script []string
	for i := range puzzles {
		script = append(script, fmt.Sprintf(`{"id":%d,"cmd":"reset","puzzle_index":%d}`, i, i))
	}
	got := run(t, Options{}, script...)

	for i, m := range got {
		mustOK(t, m, "reset")
		cars := m["cars"].([]any)
		slot0 := cars[0].([]any)
		if isTarget := int(slot0[4].(float64)); isTarget != 1 {
			t.Errorf("%s: slot 0 is not the target", puzzles[i].Name)
		}
		if labels, _ := m["labels"].(string); !strings.HasPrefix(labels, "A") {
			t.Errorf("%s: labels = %q, want to start with A", puzzles[i].Name, labels)
		}
	}
}

// Padding slots must be inert: their four action bits are always masked, and
// their geometry rows are zero, so the observation shape never leaks the number
// of vehicles.
func TestPaddingSlotsAreInert(t *testing.T) {
	got := run(t, Options{}, `{"id":1,"cmd":"reset","puzzle":"p02"}`) // 7 vehicles
	m := mustOK(t, got[0], "reset")

	nCars := num(t, m, "n_cars")
	mask := ints(t, m, "mask")
	cars := m["cars"].([]any)

	if len(cars) != len(mask)/2 {
		t.Fatalf("cars has %d rows, mask covers %d slots", len(cars), len(mask)/2)
	}
	for slot := nCars; slot < len(cars); slot++ {
		if mask[2*slot] != 0 || mask[2*slot+1] != 0 {
			t.Errorf("padding slot %d is not masked off", slot)
		}
		for _, v := range cars[slot].([]any) {
			if v.(float64) != 0 {
				t.Errorf("padding slot %d has non-zero geometry %v", slot, cars[slot])
				break
			}
		}
	}
}

// The mask is the contract an agent's policy is built on: an action is marked
// legal exactly when it changes the board. Checked over a walk, not just the
// starting position.
func TestMaskMatchesWhatActuallyHappens(t *testing.T) {
	srv, err := NewServer(Options{IncludeBoard: true})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	puzzles, _ := rush.DefaultPuzzles()

	for _, p := range puzzles[:8] {
		resp := call(t, srv, fmt.Sprintf(`{"id":1,"cmd":"reset","puzzle":"%s"}`, p.Name))
		mustOK(t, resp, "reset")

		for iter := 0; iter < 25; iter++ {
			mask := ints(t, resp, "mask")
			board, _ := resp["board"].(string)

			// Every action is tried on a private copy of the position, and the
			// mask has to have predicted whether it would move anything.
			for action := range mask {
				probe := call(t, srv, fmt.Sprintf(`{"id":2,"cmd":"reset","env_id":9,"spec":%q}`, board))
				if ok, _ := probe["ok"].(bool); !ok {
					// The board is solved; ParseBoard refuses those on purpose.
					goto nextPuzzle
				}
				after := call(t, srv, fmt.Sprintf(`{"id":3,"cmd":"step","env_id":9,"action":%d}`, action))
				moved, _ := after["moved"].(bool)
				if (mask[action] == 1) != moved {
					t.Fatalf("%s: mask[%d]=%d but moved=%v\n%s",
						p.Name, action, mask[action], moved, board)
				}
			}

			// A solvable board is never stuck: the last move can be undone, so
			// something is always legal.
			if solved, _ := resp["solved"].(bool); !solved {
				legal := 0
				for _, v := range mask {
					legal += v
				}
				if legal == 0 {
					t.Fatalf("%s: no legal action on an unsolved board\n%s", p.Name, board)
				}
			}

			// Advance along the optimal path, to visit positions a fresh board
			// never shows.
			sol := call(t, srv, `{"id":4,"cmd":"solve"}`)
			actions := ints(t, mustOK(t, sol, "solve"), "actions")
			if len(actions) == 0 {
				break
			}
			resp = call(t, srv, fmt.Sprintf(`{"id":5,"cmd":"step","action":%d}`, actions[0]))
			mustOK(t, resp, "step")
		}
	nextPuzzle:
	}
}

// call sends one request and returns the parsed response.
func call(t *testing.T, srv *Server, line string) map[string]any {
	t.Helper()
	resp, _ := srv.Handle([]byte(line))
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshalling the response to %s: %v", line, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshalling the response to %s: %v", line, err)
	}
	return m
}

// ── Actions ──────────────────────────────────────────────────────────────────

// An action that moves nothing is a wasted step, not a failure. An unmasked
// policy emits these constantly while it is learning, and erroring out would
// make training brittle; -strict flips it for tests that want the noise.
func TestIllegalActionIsAWastedStepByDefault(t *testing.T) {
	got := run(t, Options{IncludeBoard: true},
		`{"id":1,"cmd":"reset","puzzle":"p02"}`,
		`{"id":2,"cmd":"step","action":1}`,  // A right: blocked by E
		`{"id":3,"cmd":"step","action":31}`, // a padding slot
	)
	before, _ := mustOK(t, got[0], "reset")["board"].(string)

	for i, m := range got[1:] {
		mustOK(t, m, "step")
		if moved, _ := m["moved"].(bool); moved {
			t.Errorf("step %d reported a move", i)
		}
		if illegal, _ := m["illegal"].(bool); !illegal {
			t.Errorf("step %d is not flagged illegal", i)
		}
		if after, _ := m["board"].(string); after != before {
			t.Errorf("step %d changed the board:\n%s\nwant\n%s", i, after, before)
		}
		// A wasted step still counts as a step, or an agent could stall for free.
		if n := num(t, m, "n_steps"); n != i+1 {
			t.Errorf("step %d: n_steps = %d, want %d", i, n, i+1)
		}
		if n := num(t, m, "n_slides"); n != 0 {
			t.Errorf("step %d: a refused step counted as a slide", i)
		}
	}
}

func TestStrictModeRejectsIllegalActions(t *testing.T) {
	got := run(t, Options{Strict: true},
		`{"id":1,"cmd":"reset","puzzle":"p02"}`,
		`{"id":2,"cmd":"step","action":1}`,
	)
	m := got[1]
	if ok, _ := m["ok"].(bool); ok {
		t.Fatalf("strict mode accepted a blocked action: %v", m)
	}
	if kind, _ := m["kind"].(string); kind != KindBadAction {
		t.Errorf("kind = %q, want %q", kind, KindBadAction)
	}
}

// n_slides collapses consecutive steps of one vehicle one way, which is the
// unit puzzles.txt counts and the unit the human trace records.
func TestSlidesCollapseConsecutiveSteps(t *testing.T) {
	// p02: G is the bottom row's three-cell vehicle at (5,2)-(5,4), slot 6, with
	// two free cells to its left.
	got := run(t, Options{},
		`{"id":1,"cmd":"reset","puzzle":"p02"}`,
		`{"id":2,"cmd":"step","action":12}`, // G left
		`{"id":3,"cmd":"step","action":12}`, // G left again — same slide
		`{"id":4,"cmd":"step","action":13}`, // G right — a new slide
	)
	want := []int{1, 1, 2}
	for i, m := range got[1:] {
		mustOK(t, m, "step")
		if !m["moved"].(bool) {
			t.Fatalf("step %d did not move: %v", i, m)
		}
		if n := num(t, m, "n_slides"); n != want[i] {
			t.Errorf("after step %d: n_slides = %d, want %d", i+1, n, want[i])
		}
	}
}

// ── Errors ───────────────────────────────────────────────────────────────────

// A client bug must not take the server down: it answers, and keeps going.
func TestBadRequestsAreReportedAndTheLoopSurvives(t *testing.T) {
	got := run(t, Options{},
		`this is not json`,
		`{"id":2,"cmd":"nonsense"}`,
		`{"id":3,"cmd":"step","action":0}`,               // before any reset
		`{"id":4,"cmd":"reset","puzzle":"nosuchpuzzle"}`, //
		`{"id":5,"cmd":"reset","puzzle_index":999}`,
		`{"id":6,"cmd":"reset","spec":"garbage"}`,
		`{"id":7,"cmd":"reset","spec":"oooooo oooooo ooooAA oooooo oooooo oooooo"}`, // already solved
		`{"id":8,"cmd":"reset"}`,                                                    // no board named
		`{"id":9,"cmd":"reset","puzzle":"p02"}`,
	)
	wantKinds := []string{
		KindBadJSON, KindUnknownCmd, KindNotReset, KindNoSuchPuzzle,
		KindNoSuchPuzzle, KindBadSpec, KindBadSpec, KindNoSuchPuzzle,
	}
	for i, want := range wantKinds {
		m := got[i]
		if ok, _ := m["ok"].(bool); ok {
			t.Errorf("request %d was accepted: %v", i+1, m)
			continue
		}
		if kind, _ := m["kind"].(string); kind != want {
			t.Errorf("request %d: kind = %q, want %q (%v)", i+1, kind, want, m["error"])
		}
	}
	// The last, valid request still works — the stream never desynchronised.
	mustOK(t, got[len(got)-1], "reset after a run of bad requests")
}

// The id is echoed even when the request could not be understood, so a client
// can pair the failure with what caused it.
func TestBadJSONKeepsTheID(t *testing.T) {
	got := run(t, Options{}, `{"id":77,"cmd":"step","action":"not a number"}`)
	if id := num(t, got[0], "id"); id != 77 {
		t.Errorf("id = %d, want 77", id)
	}
}

// ── The oracle ───────────────────────────────────────────────────────────────

// Replaying the optimal solution through the protocol must solve every shipped
// board in exactly the declared number of slides. This is the end-to-end check
// that the action encoding, the slot table and the rules all agree.
func TestOptimalSolutionSolvesEveryPuzzle(t *testing.T) {
	srv, err := NewServer(Options{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	puzzles, _ := rush.DefaultPuzzles()

	for i, p := range puzzles {
		mustOK(t, call(t, srv, fmt.Sprintf(`{"id":1,"cmd":"reset","puzzle_index":%d}`, i)), "reset")
		sol := mustOK(t, call(t, srv, `{"id":2,"cmd":"solve"}`), "solve")

		if n := num(t, sol, "n_slides"); p.MinMoves != 0 && n != p.MinMoves {
			t.Errorf("%s: solve reports %d slides, puzzles.txt declares %d", p.Name, n, p.MinMoves)
		}

		var last map[string]any
		for _, action := range ints(t, sol, "actions") {
			last = mustOK(t, call(t, srv, fmt.Sprintf(`{"id":3,"cmd":"step","action":%d}`, action)), "step")
			if !last["moved"].(bool) {
				t.Fatalf("%s: optimal action %d moved nothing", p.Name, action)
			}
		}
		if solved, _ := last["solved"].(bool); !solved {
			t.Errorf("%s: the optimal solution did not solve the board", p.Name)
		}
		if n := num(t, last, "n_slides"); p.MinMoves != 0 && n != p.MinMoves {
			t.Errorf("%s: replay counted %d slides, want %d", p.Name, n, p.MinMoves)
		}
	}
}

// ── Batches ──────────────────────────────────────────────────────────────────

// A batch must be exactly a sequence of single calls: that equivalence is what
// lets the vector environment and the plain one share a client.
func TestBatchMatchesIndividualCalls(t *testing.T) {
	single := run(t, Options{IncludeBoard: true},
		`{"id":1,"cmd":"reset","env_id":0,"puzzle":"p02"}`,
		`{"id":2,"cmd":"reset","env_id":1,"puzzle":"p03"}`,
		`{"id":3,"cmd":"step","env_id":0,"action":0}`,
		`{"id":4,"cmd":"step","env_id":1,"action":2}`,
	)
	batch := run(t, Options{IncludeBoard: true},
		`{"id":1,"cmd":"reset_batch","env_ids":[0,1],"puzzles":["p02","p03"]}`,
		`{"id":2,"cmd":"step_batch","env_ids":[0,1],"actions":[0,2]}`,
	)

	states := func(m map[string]any) []any {
		v, ok := m["states"].([]any)
		if !ok {
			t.Fatalf("no states in %v", m)
		}
		return v
	}
	pairs := [][2]any{
		{single[0], states(mustOK(t, batch[0], "reset_batch"))[0]},
		{single[1], states(batch[0])[1]},
		{single[2], states(mustOK(t, batch[1], "step_batch"))[0]},
		{single[3], states(batch[1])[1]},
	}
	for i, pair := range pairs {
		one := pair[0].(map[string]any)
		many := pair[1].(map[string]any)
		for _, key := range []string{"env_id", "puzzle", "board", "n_steps", "n_slides", "solved", "moved"} {
			if fmt.Sprint(one[key]) != fmt.Sprint(many[key]) {
				t.Errorf("pair %d: %s = %v in the batch, %v alone", i, key, many[key], one[key])
			}
		}
	}
}

func TestBatchRejectsMismatchedLengths(t *testing.T) {
	got := run(t, Options{},
		`{"id":1,"cmd":"reset_batch","env_ids":[0,1],"puzzles":["p02"]}`,
		`{"id":2,"cmd":"step_batch","env_ids":[],"actions":[]}`,
		`{"id":3,"cmd":"reset_batch","env_ids":[0,1],"puzzle":"p02"}`,
		`{"id":4,"cmd":"step_batch","env_ids":[0,1],"actions":[0]}`,
	)
	for i, want := range []string{KindBadBatch, KindBadBatch, "", KindBadBatch} {
		m := got[i]
		if want == "" {
			mustOK(t, m, "a batch resetting every board to one puzzle")
			continue
		}
		if kind, _ := m["kind"].(string); kind != want {
			t.Errorf("request %d: kind = %q, want %q", i+1, kind, want)
		}
	}
}

// Sessions are independent: stepping one board must not disturb another.
func TestSessionsAreIndependent(t *testing.T) {
	got := run(t, Options{IncludeBoard: true},
		`{"id":1,"cmd":"reset","env_id":0,"puzzle":"p02"}`,
		`{"id":2,"cmd":"reset","env_id":1,"puzzle":"p02"}`,
		`{"id":3,"cmd":"step","env_id":0,"action":0}`,
		`{"id":4,"cmd":"state","env_id":1}`,
	)
	fresh, _ := mustOK(t, got[1], "reset")["board"].(string)
	untouched, _ := mustOK(t, got[3], "state")["board"].(string)
	if untouched != fresh {
		t.Errorf("env 1 changed when env 0 was stepped:\n%s\nwant\n%s", untouched, fresh)
	}
	if n := num(t, got[3], "n_steps"); n != 0 {
		t.Errorf("env 1 counted %d steps", n)
	}
}

// ── Options ──────────────────────────────────────────────────────────────────

// want_remaining is the potential used for reward shaping; it must fall to zero
// exactly at the solved position.
func TestWantRemainingCountsDownToZero(t *testing.T) {
	srv, _ := NewServer(Options{})
	m := mustOK(t, call(t, srv, `{"id":1,"cmd":"reset","puzzle":"p02","want_remaining":true}`), "reset")
	if n := num(t, m, "remaining"); n != 4 {
		t.Fatalf("remaining = %d at the start of p02, want 4", n)
	}

	sol := mustOK(t, call(t, srv, `{"id":2,"cmd":"solve"}`), "solve")
	for _, action := range ints(t, sol, "actions") {
		m = call(t, srv, fmt.Sprintf(`{"id":3,"cmd":"step","action":%d,"want_remaining":true}`, action))
		mustOK(t, m, "step")
	}
	if n := num(t, m, "remaining"); n != 0 {
		t.Errorf("remaining = %d on the solved board, want 0", n)
	}
}

// Without -board the state carries no board string: it is debugging weight the
// training loop should not pay for.
func TestBoardStringIsOptional(t *testing.T) {
	got := run(t, Options{}, `{"id":1,"cmd":"reset","puzzle":"p02"}`)
	if _, present := got[0]["board"]; present {
		t.Error("board string present without -board")
	}
}
