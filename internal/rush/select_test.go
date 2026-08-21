// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rush

import "testing"

// directions is the four presses a d-pad offers, as (dRow, dCol).
var directions = [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}

// Every vehicle has to be reachable from every other one, on every puzzle in
// the library. This is the property the whole button interface rests on: a
// participant who cannot reach a vehicle cannot solve the puzzle, and would
// have no way to say so from inside a scanner.
//
// The test also reports the worst case it found, because that number is the
// cost of the interface — the presses between wanting a vehicle and having it.
func TestNeighbourReachesEveryVehicle(t *testing.T) {
	puzzles, err := DefaultPuzzles()
	if err != nil {
		t.Fatalf("DefaultPuzzles: %v", err)
	}

	worst, worstAt := 0, ""
	for _, p := range puzzles {
		b := p.Fresh()
		for _, start := range b.Cars {
			// Breadth-first over the selection graph.
			dist := map[*Car]int{start: 0}
			queue := []*Car{start}
			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				for _, d := range directions {
					next := b.Neighbour(cur, d[0], d[1])
					if _, seen := dist[next]; seen {
						continue
					}
					dist[next] = dist[cur] + 1
					queue = append(queue, next)
				}
			}

			if len(dist) != len(b.Cars) {
				for _, c := range b.Cars {
					if _, ok := dist[c]; !ok {
						t.Errorf("%s: %s is unreachable from %s",
							p.Name, string(c.Label), string(start.Label))
					}
				}
			}
			for _, n := range dist {
				if n > worst {
					worst, worstAt = n, p.Name
				}
			}
		}
	}
	t.Logf("worst case over the library: %d presses (%s)", worst, worstAt)
	if worst > 6 {
		t.Errorf("worst case is %d presses (%s); the interface is meant to reach any vehicle in a handful", worst, worstAt)
	}
}

// A press has to move the selection: an edge that swallows presses would leave
// a participant pressing a button that does nothing, with no way to ask why.
func TestNeighbourAlwaysMoves(t *testing.T) {
	puzzles, err := DefaultPuzzles()
	if err != nil {
		t.Fatalf("DefaultPuzzles: %v", err)
	}
	for _, p := range puzzles {
		b := p.Fresh()
		for _, car := range b.Cars {
			for _, d := range directions {
				if got := b.Neighbour(car, d[0], d[1]); got == car {
					t.Errorf("%s: %s stays selected after pressing (%d,%d)",
						p.Name, string(car.Label), d[0], d[1])
				}
			}
		}
	}
}

// Pressing a direction should follow the band the selection is already in
// rather than take the vehicle that happens to be closest as the crow flies.
//
//	. . B B . .
//	. C C . . .
//	A A . . D D
func TestNeighbourPrefersTheSameBand(t *testing.T) {
	b, err := ParseBoard("ooBBoo oCCooo AAooDD oooooo oooooo oooooo")
	if err != nil {
		t.Fatalf("ParseBoard: %v", err)
	}
	a := b.Target()

	// D is on A's own row, further away than C; C is nearer but a row up.
	if got := b.Neighbour(a, 0, 1); string(got.Label) != "D" {
		t.Errorf("right of A: got %s, want D", string(got.Label))
	}
	// Straight up from A is C, which overlaps A's columns; B does not.
	if got := b.Neighbour(a, -1, 0); string(got.Label) != "C" {
		t.Errorf("above A: got %s, want C", string(got.Label))
	}
}

// With nothing in the pressed direction the selection wraps to the far side
// instead of stopping.
func TestNeighbourWrapsAtTheEdge(t *testing.T) {
	b, err := ParseBoard("oooooo oooooo AAoCCo BBoooo oooooo oooooo")
	if err != nil {
		t.Fatalf("ParseBoard: %v", err)
	}
	a := b.Target()
	right := b.Neighbour(a, 0, 1) // C, the only thing to the right on row 2
	if string(right.Label) != "C" {
		t.Fatalf("right of A: got %s, want C", string(right.Label))
	}
	// Nothing lies right of C on its band, so another press comes back to A.
	if got := b.Neighbour(right, 0, 1); got != a {
		t.Errorf("right of C: got %s, want the wrap back to A", string(got.Label))
	}
}

// Cycle walks the board's own order and wraps at both ends, which is what the
// two-button fallback needs to reach everything.
func TestCycleWrapsBothWays(t *testing.T) {
	b, err := ParseBoard("BCCCoo BoooDo oAAEDo oooEoo FFoEoo ooGGGo")
	if err != nil {
		t.Fatalf("ParseBoard: %v", err)
	}
	n := len(b.Cars)

	// Forward all the way round returns to the start, having visited each car.
	seen := map[*Car]bool{}
	cur := b.Cars[0]
	for i := 0; i < n; i++ {
		seen[cur] = true
		cur = b.Cycle(cur, 1)
	}
	if cur != b.Cars[0] {
		t.Errorf("after %d steps forward the selection is %s, want the start", n, string(cur.Label))
	}
	if len(seen) != n {
		t.Errorf("forward walk visited %d of %d vehicles", len(seen), n)
	}

	// Backwards from the first car reaches the last.
	if got := b.Cycle(b.Cars[0], -1); got != b.Cars[n-1] {
		t.Errorf("one step back from the first car is %s, want %s",
			string(got.Label), string(b.Cars[n-1].Label))
	}
}
