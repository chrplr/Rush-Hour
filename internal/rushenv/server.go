// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

// Package rushenv serves Rush Hour boards over a line-oriented JSON protocol,
// so that a program in another language — in practice a Gymnasium environment
// in Python — can play them without reimplementing the rules.
//
// The rules stay in package rush. This package only translates: an action index
// into a one-cell step, a board into a slot-indexed array, an error into a
// tagged response.
package rushenv

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"rush-hour/internal/rush"
)

// Options configures a Server. The zero value is not usable; see NewServer.
type Options struct {
	// Puzzles is the library to serve. Defaults to the embedded one.
	Puzzles []rush.Puzzle

	// MaxVehicles fixes the action space at 2*MaxVehicles. It must be at least
	// as large as the busiest board in the library.
	MaxVehicles int

	// IncludeBoard adds the six-line board notation to every state. Handy when
	// driving the protocol by hand, dead weight in a training loop.
	IncludeBoard bool

	// Canonical orders vehicles by geometry rather than by letter.
	Canonical bool

	// Strict turns an action that moves nothing into an error rather than a
	// wasted step. Useful in tests; hostile to an agent that is still learning.
	Strict bool

	// Render reports whether this build is driving a window. Only the SDL
	// build sets it.
	Render bool

	Version string
}

// DefaultMaxVehicles covers the shipped library, whose busiest board holds 15
// vehicles, with room to spare.
const DefaultMaxVehicles = 16

// Notification kinds passed to Server.Observer.
const (
	kindReset = "reset"
	kindStep  = "step"
)

// Server plays boards on request. It is not safe for concurrent use: the
// protocol is strictly one request, one response.
type Server struct {
	opts     Options
	byName   map[string]int
	sessions map[int]*session

	// Observer, when set, is called after every reset and step, with kind
	// "reset" or "step". The results-file recorder hangs off it, and so does
	// the SDL viewer in the rushui build.
	Observer func(envID int, kind string, st State)
}

// NewServer validates the options and prepares the catalogue.
func NewServer(opts Options) (*Server, error) {
	if opts.Puzzles == nil {
		p, err := rush.DefaultPuzzles()
		if err != nil {
			return nil, err
		}
		opts.Puzzles = p
	}
	if len(opts.Puzzles) == 0 {
		return nil, errors.New("empty puzzle library")
	}
	if opts.MaxVehicles <= 0 {
		opts.MaxVehicles = DefaultMaxVehicles
	}

	byName := make(map[string]int, len(opts.Puzzles))
	for i, p := range opts.Puzzles {
		// A fixed action space cannot represent a board with more vehicles than
		// it has slots. Failing here is the only way the client hears about it;
		// silently truncating would make some vehicles unreachable.
		if n := len(p.Board.Cars); n > opts.MaxVehicles {
			return nil, fmt.Errorf("puzzle %s has %d vehicles, more than max-vehicles=%d",
				p.Name, n, opts.MaxVehicles)
		}
		byName[p.Name] = i
	}

	return &Server{opts: opts, byName: byName, sessions: map[int]*session{}}, nil
}

// Run reads requests until the input ends, answering each one. It returns nil
// on end of input, which is how the process exits when its parent dies: the
// pipe closes, the scan stops, and nothing is left orphaned.
func (s *Server) Run(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	// Batch requests are long; the default 64 KiB line limit is not enough.
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)

	w := bufio.NewWriter(out)
	enc := json.NewEncoder(w)

	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		resp, quit := s.Handle(line)
		if err := enc.Encode(resp); err != nil {
			return err
		}
		// Every response is flushed: the client is blocked reading one line and
		// will not send the next request until it arrives.
		if err := w.Flush(); err != nil {
			return err
		}
		if quit {
			return nil
		}
	}
	return sc.Err()
}

// Handle answers one request line. quit is true after a close command. A
// malformed or impossible request is answered with an error and the loop goes
// on: a client bug must not take the server down.
func (s *Server) Handle(line []byte) (resp any, quit bool) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		// The id may still be recoverable, so the client can match the failure
		// to the request that caused it.
		return s.fail(peekID(line), KindBadJSON, err.Error()), false
	}

	switch req.Cmd {
	case "hello":
		return s.hello(req), false
	case "reset":
		return s.reset(req), false
	case "step":
		return s.step(req), false
	case "state":
		return s.state(req), false
	case "reset_batch":
		return s.resetBatch(req), false
	case "step_batch":
		return s.stepBatch(req), false
	case "solve":
		return s.solve(req), false
	case "close":
		return okHeader(req.ID), true
	default:
		return s.fail(req.ID, KindUnknownCmd, fmt.Sprintf("unknown command %q", req.Cmd)), false
	}
}

// ── Commands ─────────────────────────────────────────────────────────────────

func (s *Server) hello(req request) any {
	catalog := make([]PuzzleInfo, len(s.opts.Puzzles))
	for i, p := range s.opts.Puzzles {
		catalog[i] = PuzzleInfo{
			Index: i, Name: p.Name, MinMoves: p.MinMoves,
			NCars: len(p.Board.Cars), Spec: p.Board.String(),
		}
	}
	return helloResponse{
		header:      okHeader(req.ID),
		Protocol:    Protocol,
		Version:     s.opts.Version,
		GridSize:    rush.GridSize,
		TargetRow:   rush.TargetRow,
		MaxVehicles: s.opts.MaxVehicles,
		NActions:    2 * s.opts.MaxVehicles,
		Canonical:   s.opts.Canonical,
		Strict:      s.opts.Strict,
		Render:      s.opts.Render,
		Puzzles:     catalog,
	}
}

func (s *Server) reset(req request) any {
	envID := 0
	if req.EnvID != nil {
		envID = *req.EnvID
	}
	sess, kind, err := s.newSession(req.Puzzle, req.PuzzleIndex, req.Spec)
	if err != nil {
		return s.fail(req.ID, kind, err.Error())
	}
	s.sessions[envID] = sess

	st := sess.observe(envID, s.opts.MaxVehicles)
	s.decorate(sess, &st, req.WantRemaining)
	s.notify(envID, kindReset, st)
	return stateResponse{header: okHeader(req.ID), State: st}
}

func (s *Server) step(req request) any {
	envID := 0
	if req.EnvID != nil {
		envID = *req.EnvID
	}
	sess, ok := s.sessions[envID]
	if !ok {
		return s.fail(req.ID, KindNotReset, fmt.Sprintf("env %d has not been reset", envID))
	}
	if req.Action == nil {
		return s.fail(req.ID, KindBadAction, "step needs an action")
	}

	st := sess.step(envID, *req.Action, s.opts.MaxVehicles)
	if st.Illegal && s.opts.Strict {
		return s.fail(req.ID, KindBadAction,
			fmt.Sprintf("action %d moves nothing on env %d", *req.Action, envID))
	}
	s.decorate(sess, &st, req.WantRemaining)
	s.notify(envID, kindStep, st)
	return stateResponse{header: okHeader(req.ID), State: st}
}

func (s *Server) state(req request) any {
	envID := 0
	if req.EnvID != nil {
		envID = *req.EnvID
	}
	sess, ok := s.sessions[envID]
	if !ok {
		return s.fail(req.ID, KindNotReset, fmt.Sprintf("env %d has not been reset", envID))
	}
	st := sess.observe(envID, s.opts.MaxVehicles)
	s.decorate(sess, &st, req.WantRemaining)
	return stateResponse{header: okHeader(req.ID), State: st}
}

func (s *Server) resetBatch(req request) any {
	n := len(req.EnvIDs)
	if n == 0 {
		return s.fail(req.ID, KindBadBatch, "reset_batch needs env_ids")
	}
	if err := checkLen("puzzles", len(req.Puzzles), n); err != nil {
		return s.fail(req.ID, KindBadBatch, err.Error())
	}
	if err := checkLen("puzzle_indices", len(req.PuzzleIndices), n); err != nil {
		return s.fail(req.ID, KindBadBatch, err.Error())
	}
	if err := checkLen("specs", len(req.Specs), n); err != nil {
		return s.fail(req.ID, KindBadBatch, err.Error())
	}

	states := make([]State, n)
	for i, envID := range req.EnvIDs {
		name, index, spec := pick(req, i)
		sess, kind, err := s.newSession(name, index, spec)
		if err != nil {
			return s.fail(req.ID, kind, fmt.Sprintf("env %d: %v", envID, err))
		}
		s.sessions[envID] = sess
		states[i] = sess.observe(envID, s.opts.MaxVehicles)
		s.decorate(sess, &states[i], req.WantRemaining)
		s.notify(envID, kindReset, states[i])
	}
	return batchResponse{header: okHeader(req.ID), States: states}
}

func (s *Server) stepBatch(req request) any {
	n := len(req.EnvIDs)
	if n == 0 {
		return s.fail(req.ID, KindBadBatch, "step_batch needs env_ids")
	}
	if len(req.Actions) != n {
		return s.fail(req.ID, KindBadBatch,
			fmt.Sprintf("actions has %d entries, env_ids has %d", len(req.Actions), n))
	}

	states := make([]State, n)
	for i, envID := range req.EnvIDs {
		sess, ok := s.sessions[envID]
		if !ok {
			return s.fail(req.ID, KindNotReset, fmt.Sprintf("env %d has not been reset", envID))
		}
		states[i] = sess.step(envID, req.Actions[i], s.opts.MaxVehicles)
		if states[i].Illegal && s.opts.Strict {
			return s.fail(req.ID, KindBadAction,
				fmt.Sprintf("action %d moves nothing on env %d", req.Actions[i], envID))
		}
		s.decorate(sess, &states[i], req.WantRemaining)
		s.notify(envID, kindStep, states[i])
	}
	return batchResponse{header: okHeader(req.ID), States: states}
}

// solve returns an optimal continuation from the current position, expanded
// into one-cell actions. It is an oracle, for baselines and for checking that a
// client agrees with the rules — not a policy an agent is meant to consult.
func (s *Server) solve(req request) any {
	envID := 0
	if req.EnvID != nil {
		envID = *req.EnvID
	}
	sess, ok := s.sessions[envID]
	if !ok {
		return s.fail(req.ID, KindNotReset, fmt.Sprintf("env %d has not been reset", envID))
	}

	slides, solvable := rush.SolveSlides(sess.board)
	if !solvable {
		return s.fail(req.ID, KindUnsolvable, fmt.Sprintf("env %d cannot be solved from here", envID))
	}

	// Slides are indexed by Car.ID; actions are indexed by slot.
	slotOf := make(map[int]int, len(sess.slots))
	for slot, c := range sess.slots {
		slotOf[c.ID] = slot
	}

	var actions []int
	for _, sl := range slides {
		dir, cells := sl.Dir()
		bit := 0
		if dir == rush.Right {
			bit = 1
		}
		for i := 0; i < cells; i++ {
			actions = append(actions, 2*slotOf[sl.CarID]+bit)
		}
	}
	if actions == nil {
		actions = []int{}
	}
	return solveResponse{
		header:  okHeader(req.ID),
		EnvID:   envID,
		Actions: actions,
		NSlides: len(slides),
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// newSession resolves whichever of the three ways of naming a board the request
// used.
func (s *Server) newSession(name *string, index *int, spec *string) (*session, string, error) {
	switch {
	case spec != nil:
		// ParseBoard, not Puzzle.Fresh: Fresh panics on a bad spec, and this
		// one comes from the client.
		board, err := rush.ParseBoard(*spec)
		if err != nil {
			return nil, KindBadSpec, err
		}
		p := rush.Puzzle{Name: "custom", Spec: *spec, Board: board}
		if n := len(board.Cars); n > s.opts.MaxVehicles {
			return nil, KindBadSpec, fmt.Errorf("board has %d vehicles, more than max-vehicles=%d",
				n, s.opts.MaxVehicles)
		}
		return newSession(p, -1, s.opts.Canonical), "", nil

	case name != nil:
		i, ok := s.byName[*name]
		if !ok {
			return nil, KindNoSuchPuzzle, fmt.Errorf("no puzzle named %q", *name)
		}
		return newSession(s.opts.Puzzles[i], i, s.opts.Canonical), "", nil

	case index != nil:
		if *index < 0 || *index >= len(s.opts.Puzzles) {
			return nil, KindNoSuchPuzzle, fmt.Errorf("puzzle index %d outside 0..%d",
				*index, len(s.opts.Puzzles)-1)
		}
		return newSession(s.opts.Puzzles[*index], *index, s.opts.Canonical), "", nil
	}
	return nil, KindNoSuchPuzzle, errors.New("reset needs one of puzzle, puzzle_index or spec")
}

// decorate adds the optional fields the request asked for.
func (s *Server) decorate(sess *session, st *State, wantRemaining bool) {
	if s.opts.IncludeBoard {
		st.Board = sess.board.String()
	}
	if wantRemaining {
		n := rush.MinMoves(sess.board)
		st.Remaining = &n
	}
}

func (s *Server) notify(envID int, kind string, st State) {
	if s.Observer != nil {
		s.Observer(envID, kind, st)
	}
}

// Board returns the live board of an environment, for a viewer that wants to
// draw it. It is the server's own state, not a copy: read it, do not move it.
func (s *Server) Board(envID int) *rush.Board {
	if sess, ok := s.sessions[envID]; ok {
		return sess.board
	}
	return nil
}

// Puzzle returns the puzzle an environment is playing.
func (s *Server) Puzzle(envID int) (rush.Puzzle, bool) {
	if sess, ok := s.sessions[envID]; ok {
		return sess.puzzle, true
	}
	return rush.Puzzle{}, false
}

func (s *Server) fail(id int64, kind, msg string) errorResponse {
	return errorResponse{header: header{ID: id, OK: false}, Kind: kind, Error: msg}
}

func okHeader(id int64) header {
	return header{ID: id, OK: true}
}

// checkLen allows an optional per-item array to be absent, but not to be the
// wrong length — a short array would silently reset the wrong boards.
func checkLen(name string, got, want int) error {
	if got != 0 && got != want {
		return fmt.Errorf("%s has %d entries, env_ids has %d", name, got, want)
	}
	return nil
}

// pick pulls item i out of whichever per-item array the batch request used.
func pick(req request, i int) (name *string, index *int, spec *string) {
	if len(req.Specs) > 0 {
		return nil, nil, &req.Specs[i]
	}
	if len(req.Puzzles) > 0 {
		return &req.Puzzles[i], nil, nil
	}
	if len(req.PuzzleIndices) > 0 {
		return nil, &req.PuzzleIndices[i], nil
	}
	// Fall back to the scalar fields, so a batch can reset every board to the
	// same puzzle without repeating it.
	return req.Puzzle, req.PuzzleIndex, req.Spec
}

// peekID recovers the id from a line that failed to parse as a request, so an
// error can still be matched to its request.
func peekID(line []byte) int64 {
	var probe struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return 0
	}
	return probe.ID
}
