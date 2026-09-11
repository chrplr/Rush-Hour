// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rush

// Choosing a vehicle without a pointing device.
//
// A mouse names a vehicle by pointing at it. A button box cannot, so the
// participant instead moves a *selection* from vehicle to vehicle, and the two
// ways of naming one have to agree: whatever the selection lands on is what a
// click on that vehicle would have named.
//
// Two orders are offered, because the two hardware situations differ. With four
// directions available (a gamepad d-pad, arrow keys) the selection moves
// spatially — "the vehicle up from here" — which is how a player thinks about
// the board and reaches a neighbour in one press. With only a pair of buttons
// left over for selection, Cycle walks the vehicles in a fixed order instead.
//
// Both take an optional filter (NeighbourAmong, CycleAmong), so a session can
// skip vehicles that cannot move at all — see Movable. That is not the default:
// which vehicles are stuck is part of what the participant has to work out.

// sidewaysPenalty is what Neighbour charges a candidate per cell of gap between
// its band and the current vehicle's, in the half-cell units the scores use.
//
// It is set just above the largest distance any candidate can be along the
// board — 2*(GridSize-1) half-cells — so the ranking has a property that is
// easy to state and easier to press blind: a vehicle whose extent overlaps the
// current one's band always wins over one that does not, however far along it
// is. Pressing Right on the red car gives the next car on its row, not the
// nearer one a row up. Only among candidates that all miss the band does the
// gap trade off against the distance.
const sidewaysPenalty = 2 * GridSize

// span returns the vehicle's extent as [row0, row1] × [col0, col1], inclusive.
func (c *Car) span() (row0, row1, col0, col1 int) {
	row0, col0 = c.Row, c.Col
	row1, col1 = c.Row, c.Col
	if c.Horizontal {
		col1 += c.Length - 1
	} else {
		row1 += c.Length - 1
	}
	return row0, row1, col0, col1
}

// center returns the vehicle's midpoint in cell units, doubled so that the
// half-cell offsets of even-length vehicles stay integers.
func (c *Car) center2() (row2, col2 int) {
	row0, row1, col0, col1 := c.span()
	return row0 + row1, col0 + col1
}

// gap returns the distance between two inclusive intervals, 0 when they touch
// or overlap.
func gap(a0, a1, b0, b1 int) int {
	if d := max(a0, b0) - min(a1, b1); d > 0 {
		return d
	}
	return 0
}

// Neighbour returns the vehicle the selection should move to when the
// participant presses a direction: the nearest vehicle whose centre lies that
// way, preferring vehicles in the same row band (for a horizontal press) or
// column band (for a vertical one).
//
// dRow/dCol give the direction; exactly one of them must be -1 or +1. from must
// be a vehicle of this board.
//
// When nothing lies that way the search wraps to the far side, so a direction
// is never a press that does nothing and no vehicle is unreachable — on a
// 6×6 board the wrap is short enough to read as "it went round", and the
// alternative, an edge that silently swallows presses, is worse in a scanner
// where the participant cannot ask what happened.
//
// Returns from itself when the board has no other vehicle.
func (b *Board) Neighbour(from *Car, dRow, dCol int) *Car {
	return b.NeighbourAmong(from, dRow, dCol, nil)
}

// NeighbourAmong is Neighbour restricted to the vehicles ok accepts; a nil ok
// accepts every vehicle. from itself is never a candidate, so it is returned
// only when no acceptable vehicle exists.
func (b *Board) NeighbourAmong(from *Car, dRow, dCol int, ok func(*Car) bool) *Car {
	if from == nil {
		return b.Target()
	}
	if (dRow != 0) == (dCol != 0) { // both zero, or both set: not a direction
		return from
	}

	fromRow2, fromCol2 := from.center2()
	fRow0, fRow1, fCol0, fCol1 := from.span()

	var best, wrapped *Car
	bestScore, wrapScore := 0, 0

	for _, cand := range b.Cars {
		if cand == from || (ok != nil && !ok(cand)) {
			continue
		}
		candRow2, candCol2 := cand.center2()

		// ahead is the displacement along the pressed direction, in half-cells.
		ahead := (candRow2-fromRow2)*dRow + (candCol2-fromCol2)*dCol

		cRow0, cRow1, cCol0, cCol1 := cand.span()
		var sideways int
		if dRow != 0 {
			sideways = gap(fCol0, fCol1, cCol0, cCol1)
		} else {
			sideways = gap(fRow0, fRow1, cRow0, cRow1)
		}

		score := ahead + sidewaysPenalty*sideways
		switch {
		case ahead > 0:
			if best == nil || score < bestScore {
				best, bestScore = cand, score
			}
		default:
			// Behind, or centred on the same line: only reachable by wrapping.
			// Minimising the same score picks the farthest one back, still
			// preferring the current band.
			if wrapped == nil || score < wrapScore {
				wrapped, wrapScore = cand, score
			}
		}
	}

	if best != nil {
		return best
	}
	if wrapped != nil {
		return wrapped
	}
	return from
}

// Cycle returns the vehicle delta places from `from` in the board's own order —
// alphabetical by label, which is the reading order of their top-left cells.
// The walk wraps at both ends.
//
// This is the selection order for a response box with too few buttons to spare
// four of them on directions.
func (b *Board) Cycle(from *Car, delta int) *Car {
	return b.CycleAmong(from, delta, nil)
}

// CycleAmong is Cycle restricted to the vehicles ok accepts; a nil ok accepts
// every vehicle. The walk keeps going in the same direction past vehicles ok
// rejects, so from itself — which may have just been stuck by its own move —
// is the answer only when no other vehicle is acceptable.
func (b *Board) CycleAmong(from *Car, delta int, ok func(*Car) bool) *Car {
	n := len(b.Cars)
	if n == 0 {
		return nil
	}
	i := 0
	for j, c := range b.Cars {
		if c == from {
			i = j
			break
		}
	}
	if ok == nil {
		return b.Cars[((i+delta)%n+n)%n]
	}
	if delta == 0 {
		return b.Cars[i]
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	// |delta| acceptable vehicles onward, at most one lap.
	want := delta * step
	for k, found := 1, 0; k <= n; k++ {
		c := b.Cars[((i+k*step)%n+n)%n]
		if !ok(c) {
			continue
		}
		if found++; found == want {
			return c
		}
	}
	return from
}

// Movable reports whether the vehicle can slide at least one cell in either
// direction — whether it has any degree of freedom at all right now. It is
// the filter for a session that skips stuck vehicles when choosing.
func (b *Board) Movable(c *Car) bool {
	return b.CanStep(c, Left) || b.CanStep(c, Right)
}
