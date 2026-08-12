// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rushenv

// Wire format: one JSON object per line, in each direction, request and
// response paired by id. It is meant to be readable and to be driveable by
// hand — start the binary and type {"id":1,"cmd":"hello"}.
//
// The server reports facts and never a reward: the reward scheme, the
// termination rule and the observation tensor all live in the client, so they
// can be changed without rebuilding this binary.

// Protocol is the wire version. Bump it on any incompatible change; the client
// checks it during the handshake.
const Protocol = 1

// Error kinds, so a client can branch on the failure without parsing prose.
const (
	KindBadJSON       = "bad_json"
	KindUnknownCmd    = "unknown_cmd"
	KindNoSuchEnv     = "no_such_env"
	KindNoSuchPuzzle  = "no_such_puzzle"
	KindBadSpec       = "bad_spec"
	KindBadAction     = "bad_action"
	KindNotReset      = "not_reset"
	KindBadBatch      = "bad_batch"
	KindUnsolvable    = "unsolvable"
	KindNotSupported  = "not_supported"
	KindInternalError = "internal"
)

// request is the union of every command's fields. Pointers distinguish "absent"
// from "zero", which matters for env_id 0 and puzzle_index 0.
type request struct {
	ID  int64  `json:"id"`
	Cmd string `json:"cmd"`

	EnvID  *int  `json:"env_id"`
	EnvIDs []int `json:"env_ids"`

	Puzzle        *string  `json:"puzzle"`
	Puzzles       []string `json:"puzzles"`
	PuzzleIndex   *int     `json:"puzzle_index"`
	PuzzleIndices []int    `json:"puzzle_indices"`
	Spec          *string  `json:"spec"`
	Specs         []string `json:"specs"`

	Action  *int  `json:"action"`
	Actions []int `json:"actions"`

	// WantRemaining asks for the optimal number of slides still needed. It runs
	// a breadth-first search, so it is opt-in per request: on the hard end of
	// the library it is far more expensive than the move itself.
	WantRemaining bool `json:"want_remaining"`
}

// header is on every response.
type header struct {
	ID int64 `json:"id"`
	OK bool  `json:"ok"`
}

type errorResponse struct {
	header
	Kind  string `json:"kind"`
	Error string `json:"error"`
}

// PuzzleInfo is one entry of the library catalogue, sent once at the handshake
// so the client can build curricula without further round trips.
type PuzzleInfo struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	MinMoves int    `json:"min_moves"`
	NCars    int    `json:"n_cars"`
	Spec     string `json:"spec"`
}

type helloResponse struct {
	header
	Protocol    int          `json:"protocol"`
	Version     string       `json:"version"`
	GridSize    int          `json:"grid_size"`
	TargetRow   int          `json:"target_row"`
	MaxVehicles int          `json:"max_vehicles"`
	NActions    int          `json:"n_actions"`
	Canonical   bool         `json:"canonical"`
	Strict      bool         `json:"strict"`
	Render      bool         `json:"render"`
	Puzzles     []PuzzleInfo `json:"puzzles"`
}

// State is what the client turns into an observation. Cars and Mask are indexed
// by slot and padded to max_vehicles, so their shape never depends on the
// puzzle.
type State struct {
	EnvID       int    `json:"env_id"`
	Puzzle      string `json:"puzzle"`
	PuzzleIndex int    `json:"puzzle_index"`
	MinMoves    int    `json:"min_moves"`
	NCars       int    `json:"n_cars"`
	Labels      string `json:"labels"`

	Cars   [][5]int `json:"cars"` // {row, col, length, horizontal, is_target}
	Mask   []int    `json:"mask"`
	Solved bool     `json:"solved"`

	// What the last action did. On reset and state: Slot -1, Dir 0, no move.
	Moved   bool   `json:"moved"`
	Illegal bool   `json:"illegal"`
	Slot    int    `json:"slot"`
	Label   string `json:"label"`
	Dir     int    `json:"dir"`
	From    [2]int `json:"from"`
	To      [2]int `json:"to"`

	NSteps  int `json:"n_steps"`  // one-cell steps, i.e. clicks
	NSlides int `json:"n_slides"` // slide-collapsed, comparable to min_moves

	Board     string `json:"board,omitempty"`
	Remaining *int   `json:"remaining,omitempty"`
}

type stateResponse struct {
	header
	State
}

type batchResponse struct {
	header
	States []State `json:"states"`
}

type solveResponse struct {
	header
	EnvID   int   `json:"env_id"`
	Actions []int `json:"actions"`  // one-cell steps, ready to feed back as steps
	NSlides int   `json:"n_slides"` // optimal slide count from the current state
}
