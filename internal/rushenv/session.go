// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the MIT License.

package rushenv

import (
	"sort"
	"strings"

	"rush-hour/internal/rush"
)

// A session is one board being played. The server holds several, addressed by
// env_id, so a vectorised agent needs only one child process.
type session struct {
	puzzle rush.Puzzle
	index  int // position in the library, -1 for a board given as a raw spec
	board  *rush.Board

	// slots is the stable action index → vehicle table. Slot 0 is always the
	// red car; the rest are frozen at reset, because vehicles move and an
	// ordering recomputed mid-episode would shift indices under the agent.
	slots []*rush.Car

	steps  int
	slides rush.SlideCounter
}

// newSession starts a puzzle. canonical orders the non-target vehicles by the
// reading order of their head cell rather than by their letter, so that the same
// physical board relabelled yields the same actions.
func newSession(p rush.Puzzle, index int, canonical bool) *session {
	b := p.Fresh()

	slots := make([]*rush.Car, 0, len(b.Cars))
	slots = append(slots, b.Target())
	for _, c := range b.Cars {
		if !c.IsTarget {
			slots = append(slots, c)
		}
	}
	if canonical {
		rest := slots[1:]
		sort.SliceStable(rest, func(i, j int) bool {
			return rest[i].Row*rush.GridSize+rest[i].Col < rest[j].Row*rush.GridSize+rest[j].Col
		})
	}

	return &session{puzzle: p, index: index, board: b, slots: slots}
}

// labels spells out the slot table, so a trace can name the vehicle an action
// referred to.
func (s *session) labels() string {
	var sb strings.Builder
	for _, c := range s.slots {
		sb.WriteByte(c.Label)
	}
	return sb.String()
}

// decode splits an action into a slot and a direction. ok is false for an
// action outside the space or pointing at a padding slot.
func (s *session) decode(action, maxVehicles int) (slot, dir int, ok bool) {
	if action < 0 || action >= 2*maxVehicles {
		return 0, 0, false
	}
	slot = action / 2
	dir = rush.Left
	if action%2 == 1 {
		dir = rush.Right
	}
	return slot, dir, slot < len(s.slots)
}

// mask marks the actions that would change the board. Padding slots are always
// masked off.
//
// It is never all zero on an unsolved board: every move is reversible, so at
// worst the last-moved vehicle can go back.
func (s *session) mask(maxVehicles int) []int {
	m := make([]int, 2*maxVehicles)
	for slot, car := range s.slots {
		if s.board.CanStep(car, rush.Left) {
			m[2*slot] = 1
		}
		if s.board.CanStep(car, rush.Right) {
			m[2*slot+1] = 1
		}
	}
	return m
}

// cars is the slot-indexed geometry, padded with zero rows so the array shape
// does not depend on the puzzle: {row, col, length, horizontal, is_target}.
func (s *session) cars(maxVehicles int) [][5]int {
	out := make([][5]int, maxVehicles)
	for slot, c := range s.slots {
		horizontal, target := 0, 0
		if c.Horizontal {
			horizontal = 1
		}
		if c.IsTarget {
			target = 1
		}
		out[slot] = [5]int{c.Row, c.Col, c.Length, horizontal, target}
	}
	return out
}

// step applies one action and reports what happened. An action that cannot move
// anything is not an error: it leaves the board untouched and is reported as
// illegal, which the caller turns into a wasted step.
func (s *session) step(envID, action, maxVehicles int) State {
	st := s.observe(envID, maxVehicles)
	st.Slot, st.Dir = -1, 0

	slot, dir, ok := s.decode(action, maxVehicles)
	if !ok {
		st.Illegal = true
		s.steps++
		st.NSteps = s.steps
		return st
	}

	car := s.slots[slot]
	st.Slot, st.Dir = slot, dir
	st.Label = string(car.Label)
	st.From = [2]int{car.Row, car.Col}

	if s.board.Step(car, dir) {
		s.slides.Add(car.ID, dir)
		st.Moved = true
	} else {
		st.Illegal = true
	}
	s.steps++

	// Re-read the geometry: the board has just changed.
	st.To = [2]int{car.Row, car.Col}
	st.Cars = s.cars(maxVehicles)
	st.Mask = s.mask(maxVehicles)
	st.Solved = s.board.Solved()
	st.NSteps = s.steps
	st.NSlides = s.slides.N()
	return st
}

// observe describes the board without touching it. The move fields describe "no
// move": reset and state use them as is.
func (s *session) observe(envID, maxVehicles int) State {
	return State{
		EnvID:       envID,
		Puzzle:      s.puzzle.Name,
		PuzzleIndex: s.index,
		MinMoves:    s.puzzle.MinMoves,
		NCars:       len(s.slots),
		Labels:      s.labels(),
		Cars:        s.cars(maxVehicles),
		Mask:        s.mask(maxVehicles),
		Solved:      s.board.Solved(),
		Slot:        -1,
		From:        [2]int{-1, -1},
		To:          [2]int{-1, -1},
		NSteps:      s.steps,
		NSlides:     s.slides.N(),
	}
}
