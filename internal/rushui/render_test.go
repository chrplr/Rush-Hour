// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rushui

import (
	"testing"

	"rush-hour/internal/rush"
)

// The board of the original pygame implementation, as in the twin declaration
// in internal/rush/board_test.go.
const classic = "BCCCoo BoooDo oAAEDo oooEoo FFoEoo ooGGGo"

// CellAt must be the exact inverse of CellCenter for every cell. This is the
// place where the +Y-is-UP convention is easiest to get backwards (a mirrored
// board would still look plausible but respond to clicks on the wrong row).
func TestCellAtInvertsCellCenter(t *testing.T) {
	for row := 0; row < rush.GridSize; row++ {
		for col := 0; col < rush.GridSize; col++ {
			p := CellCenter(row, col)
			gotRow, gotCol, ok := CellAt(p.X, p.Y)
			if !ok || gotRow != row || gotCol != col {
				t.Errorf("CellAt(CellCenter(%d,%d)) = (%d,%d,%v), want (%d,%d,true)",
					row, col, gotRow, gotCol, ok, row, col)
			}
		}
	}
}

// Row 0 must be the top row: larger Y.
func TestRowZeroIsAtTheTop(t *testing.T) {
	if CellCenter(0, 0).Y <= CellCenter(rush.GridSize-1, 0).Y {
		t.Error("row 0 should be higher on screen (larger Y) than the last row")
	}
	if CellCenter(0, 0).X >= CellCenter(0, rush.GridSize-1).X {
		t.Error("column 0 should be left (smaller X) of the last column")
	}
}

func TestCellAtOutsideBoard(t *testing.T) {
	corners := [][2]float32{
		{-boardHalf - 1, boardTop - 1},         // left of the board
		{boardHalf + 1, boardTop - 1},          // right of the board
		{0, boardTop + 1},                      // above the board
		{0, boardTop - rush.GridSize*tile - 1}, // below the board
		{0, statusY},                           // on the status line
	}
	for _, c := range corners {
		if _, _, ok := CellAt(c[0], c[1]); ok {
			t.Errorf("CellAt(%v, %v) reported a cell, want outside", c[0], c[1])
		}
	}
}

// CarRect must cover exactly the cells the car occupies: its bounding box has
// to span from the head cell's outer edge to the tail cell's outer edge.
func TestCarRectSpansItsCells(t *testing.T) {
	b, err := rush.ParseBoard(classic)
	if err != nil {
		t.Fatalf("rush.ParseBoard: %v", err)
	}
	for _, car := range b.Cars {
		center, w, h := CarRect(car)
		head := CellCenter(car.Row, car.Col)
		tailRow, tailCol := car.Row, car.Col
		if car.Horizontal {
			tailCol += car.Length - 1
		} else {
			tailRow += car.Length - 1
		}
		tail := CellCenter(tailRow, tailCol)

		wantX := (head.X + tail.X) / 2
		wantY := (head.Y + tail.Y) / 2
		if center.X != wantX || center.Y != wantY {
			t.Errorf("car %s: center = (%v,%v), want (%v,%v)",
				string(car.Label), center.X, center.Y, wantX, wantY)
		}

		wantW, wantH := tile, tile
		if car.Horizontal {
			wantW = tile * float32(car.Length)
		} else {
			wantH = tile * float32(car.Length)
		}
		if w != wantW || h != wantH {
			t.Errorf("car %s: size = (%v,%v), want (%v,%v)", string(car.Label), w, h, wantW, wantH)
		}
	}
}

// StepForPoint decides the direction of a one-cell click-move from the side of
// the vehicle's midline the click landed on. The middle cell of a 3-cell
// vehicle must NOT be inert: its two halves give opposite directions.
func TestStepForPointDirection(t *testing.T) {
	h2 := &rush.Car{Row: 2, Col: 1, Length: 2, Horizontal: true} // cols 1-2
	v3 := &rush.Car{Row: 1, Col: 3, Length: 3}                   // rows 1-3

	// Each case clicks the center of cell (row, col), offset by (dx, dy).
	cases := []struct {
		name     string
		car      *rush.Car
		row, col int
		dx, dy   float32
		want     int
	}{
		{"h2 head cell", h2, 2, 1, 0, 0, -1},
		{"h2 tail cell", h2, 2, 2, 0, 0, 1},
		{"v3 head cell", v3, 1, 3, 0, 0, -1},
		{"v3 tail cell", v3, 3, 3, 0, 0, 1},
		// The formerly-inert middle cell: above the midline sends the vehicle
		// up (+Y is up), below sends it down.
		{"v3 middle cell, upper half", v3, 2, 3, 0, tile / 4, -1},
		{"v3 middle cell, lower half", v3, 2, 3, 0, -tile / 4, 1},
	}
	for _, c := range cases {
		p := CellCenter(c.row, c.col)
		x, y := p.X+c.dx, p.Y+c.dy
		if got := StepForPoint(c.car, x, y); got != c.want {
			t.Errorf("%s: StepForPoint(%v,%v) = %d, want %d", c.name, x, y, got, c.want)
		}
	}

	// A click exactly on the midline is the only inert point.
	center, _, _ := CarRect(v3)
	if got := StepForPoint(v3, center.X, center.Y); got != 0 {
		t.Errorf("midline: StepForPoint = %d, want 0", got)
	}
}

// ClickPoint must land where StepForPoint reads the direction back. Without
// that, the mouse coordinates in an agent's results file would describe clicks
// that do not produce the move the same row records.
func TestClickPointRoundTripsThroughStepForPoint(t *testing.T) {
	b, err := rush.ParseBoard(classic)
	if err != nil {
		t.Fatalf("ParseBoard: %v", err)
	}
	for _, car := range b.Cars {
		for _, dir := range []int{-1, 1} {
			p := ClickPoint(car, dir)
			if got := StepForPoint(car, p.X, p.Y); got != dir {
				t.Errorf("car %s dir %+d: click at (%v,%v) reads back as %d",
					string(car.Label), dir, p.X, p.Y, got)
			}
			// The point has to be on the vehicle, or a participant could not
			// have produced it.
			row, col, onBoard := CellAt(p.X, p.Y)
			if !onBoard || !car.Covers(row, col) {
				t.Errorf("car %s dir %+d: click at (%v,%v) is not on the vehicle",
					string(car.Label), dir, p.X, p.Y)
			}
		}
	}
}
