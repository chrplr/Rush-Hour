// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rushui

import (
	"math"

	"rush-hour/internal/rush"

	"github.com/chrplr/goxpyriment/control"
	"github.com/chrplr/goxpyriment/stimuli"
)

// ── Layout ───────────────────────────────────────────────────────────────────
//
// Coordinates are center-relative with +Y pointing UP (see apparatus/CLAUDE.md);
// row 0 is therefore the TOP row and gets the LARGEST Y.

const (
	LogicalW = int32(1024)
	LogicalH = int32(768)

	tile      = float32(90)                       // cell side, as in the pygame original
	boardHalf = float32(rush.GridSize) * tile / 2 // 270 — half the 6×6 board
	boardTop  = boardHalf + 40                    // board is shifted up to leave room for the status line

	carInset  = float32(4)  // colored body inset inside its black outline
	exitWidth = float32(10) // thickness of the exit marker at the right wall

	// Status line, 40 px below the bottom edge of the board.
	statusY = boardTop - float32(rush.GridSize)*tile - 40
)

var (
	BgColor     = control.RGB(240, 240, 240)
	gridColor   = control.RGB(180, 180, 180)
	exitColor   = control.RGB(220, 50, 50)
	TextColor   = control.RGB(30, 30, 30)
	outlineDark = control.RGB(0, 0, 0)
	selectColor = control.RGB(255, 255, 255)
	hoverColor  = control.RGB(120, 120, 120)
	arrowColor  = control.RGB(255, 255, 255)

	// Vehicle palette, ported from CAR_COLORS in rush.py. Index 0 is the red
	// target car; the others cycle over the remaining entries.
	carColors = []control.Color{
		control.RGB(220, 50, 50),  // red — target
		control.RGB(50, 120, 220), // blue
		control.RGB(50, 180, 80),  // green
		control.RGB(220, 160, 40), // orange
		control.RGB(140, 60, 200), // purple
		control.RGB(200, 200, 50), // yellow
		control.RGB(50, 180, 200), // cyan
	}
)

// carColor returns the drawing color of a vehicle.
func carColor(c *rush.Car) control.Color {
	if c.IsTarget {
		return carColors[0]
	}
	return carColors[c.ID%(len(carColors)-1)+1]
}

// CellCenter returns the center of cell (row, col) in center-based coordinates.
func CellCenter(row, col int) control.FPoint {
	return control.FPoint{
		X: -boardHalf + tile*(float32(col)+0.5),
		Y: boardTop - tile*(float32(row)+0.5),
	}
}

// CellAt is the inverse of CellCenter: it maps a point to the cell containing
// it. ok is false when the point falls outside the board.
func CellAt(x, y float32) (row, col int, ok bool) {
	col = int(math.Floor(float64((x + boardHalf) / tile)))
	row = int(math.Floor(float64((boardTop - y) / tile)))
	if row < 0 || row >= rush.GridSize || col < 0 || col >= rush.GridSize {
		return row, col, false
	}
	return row, col, true
}

// CarRect returns the center and size of a vehicle's full tile span.
func CarRect(c *rush.Car) (center control.FPoint, w, h float32) {
	w, h = tile, tile
	if c.Horizontal {
		w = tile * float32(c.Length)
	} else {
		h = tile * float32(c.Length)
	}
	head := CellCenter(c.Row, c.Col)
	if c.Horizontal {
		center = control.FPoint{X: head.X + tile*float32(c.Length-1)/2, Y: head.Y}
	} else {
		center = control.FPoint{X: head.X, Y: head.Y - tile*float32(c.Length-1)/2}
	}
	return center, w, h
}

// StepForPoint maps a click at (x, y) — center-relative screen coordinates —
// to a one-cell step along the vehicle's axis: the side of the vehicle's
// midline the click landed on decides the direction. -1 is left (horizontal) or
// up (vertical), +1 right or down.
//
// Splitting on the midline rather than on cell indices matters only for 3-cell
// vehicles: a cell-index rule leaves their middle cell — a third of their
// surface — with no direction to give, and so inert. For 2-cell vehicles the
// midline is the boundary between their two cells, so the two rules agree.
//
// The caller has already established that (x, y) is inside this vehicle; a
// click exactly on the midline (measure zero) yields 0 and moves nothing.
func StepForPoint(c *rush.Car, x, y float32) int {
	center, _, _ := CarRect(c)

	d := x - center.X // horizontal: right of the midline slides right
	if !c.Horizontal {
		d = center.Y - y // vertical: +Y is up, so below the midline slides down
	}
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	}
	return 0
}

// ClickPoint is the inverse of StepForPoint: the position a participant would
// have to click to slide this vehicle one cell in dir (-1 left/up, +1
// right/down). A quarter of a tile off the midline lands inside the vehicle for
// both 2- and 3-cell vehicles, and unambiguously on the right side of it.
//
// The agent environment writes this into the mouse_x/mouse_y columns of its
// results file, so an agent's row says where the click would have been rather
// than leaving the columns blank.
func ClickPoint(c *rush.Car, dir int) control.FPoint {
	center, _, _ := CarRect(c)
	offset := float32(dir) * tile / 4
	if c.Horizontal {
		return control.FPoint{X: center.X + offset, Y: center.Y}
	}
	// +Y is up, so stepping down (+1) means clicking below the midline.
	return control.FPoint{X: center.X, Y: center.Y - offset}
}

// DrawBoard renders one frame: grid, exit marker, vehicles, the selection, and
// the status line. It clears the screen but does not flip — the caller decides
// when to present (Flip inside the trial loop).
//
// selected is the vehicle the buttons would act on: it gets a white outline and
// an arrow at each end it can still move towards. hover is the vehicle under
// the mouse, outlined more faintly. Either may be nil, and with one input
// device in use usually one of them is.
//
// Drawing the arrows from the board rather than from the vehicle alone is what
// makes the button interface legible: a participant who cannot see a cursor can
// still see, before pressing anything, which of the two buttons will do
// something. It also states the rule the mouse obeys — a click on the half of a
// vehicle an arrow points to is the same move.
func DrawBoard(exp *control.Experiment, b *rush.Board, selected, hover *rush.Car, status string) error {
	if err := exp.Screen.Clear(); err != nil {
		return err
	}

	// Grid lines.
	left, right := -boardHalf, boardHalf
	top, bottom := boardTop, boardTop-float32(rush.GridSize)*tile
	for i := 0; i <= rush.GridSize; i++ {
		y := boardTop - float32(i)*tile
		line := stimuli.NewLine(control.Point(left, y), control.Point(right, y), gridColor, 1)
		if err := line.Draw(exp.Screen); err != nil {
			return err
		}
		x := left + float32(i)*tile
		line = stimuli.NewLine(control.Point(x, top), control.Point(x, bottom), gridColor, 1)
		if err := line.Draw(exp.Screen); err != nil {
			return err
		}
	}

	// Exit marker on the right wall of the target row.
	exitCenter := CellCenter(rush.TargetRow, rush.GridSize-1)
	exit := stimuli.NewRectangle(right-exitWidth/2, exitCenter.Y, exitWidth, tile, exitColor)
	if err := exit.Draw(exp.Screen); err != nil {
		return err
	}

	// Vehicles: a black plate with the colored body inset on top. SDL's
	// RenderFillRect has no border radius, so the pygame rounded corners
	// become square.
	for _, car := range b.Cars {
		center, w, h := CarRect(car)

		border := outlineDark
		switch car {
		case selected:
			border = selectColor
		case hover:
			border = hoverColor
		}
		plate := stimuli.NewRectangle(center.X, center.Y, w-carInset, h-carInset, border)
		if err := plate.Draw(exp.Screen); err != nil {
			return err
		}
		body := stimuli.NewRectangle(center.X, center.Y, w-3*carInset, h-3*carInset, carColor(car))
		if err := body.Draw(exp.Screen); err != nil {
			return err
		}
	}

	// Move arrows, drawn last so they sit on top of the selected vehicle.
	if selected != nil {
		for _, dir := range []int{rush.Left, rush.Right} {
			if !b.CanStep(selected, dir) {
				continue
			}
			if err := drawArrow(exp, selected, dir); err != nil {
				return err
			}
		}
	}

	if status != "" {
		if err := stimuli.NewTextLine(status, 0, statusY, TextColor).Draw(exp.Screen); err != nil {
			return err
		}
	}
	return nil
}

// arrowHalf is half the arrow's width across its base, as a fraction of a tile.
// Small enough to sit inside the end cell of a vehicle, big enough to read at
// the back of a scanner room.
const arrowHalf = tile * 0.16

// drawArrow draws a filled triangle inside the end of a vehicle, pointing the
// way a step in dir would take it: at the left end pointing left for a
// horizontal vehicle, at the top end pointing up for a vertical one, and the
// mirror image for dir = Right.
//
// It is drawn inside the vehicle rather than beyond it because the cell beyond
// is either off the board or occupied by whatever the vehicle is about to move
// into — there is no free space out there to draw in, and an arrow overlapping
// a neighbour would read as belonging to the neighbour.
func drawArrow(exp *control.Experiment, c *rush.Car, dir int) error {
	points := arrowPoints(c, dir)
	// stimuli.Shape takes its points relative to its own position, so the
	// position stays at the origin and the points carry the whole geometry.
	return stimuli.NewShape(points[:], arrowColor).Draw(exp.Screen)
}

// arrowPoints returns the triangle, tip first, in center-relative coordinates.
//
// It is split out from the drawing so the geometry can be tested: +Y pointing
// up while rows count downwards is the one place in this file where a sign
// error produces a picture that still looks like an arrow, and points the
// participant at the wrong button.
func arrowPoints(c *rush.Car, dir int) [3]control.FPoint {
	center, w, h := CarRect(c)

	// The tip sits a fifth of a tile inside the leading edge, the base a third
	// of a tile behind the tip.
	const tipInset, length = tile * 0.20, tile * 0.34

	if c.Horizontal {
		tipX := center.X + float32(dir)*(w/2-tipInset)
		backX := tipX - float32(dir)*length
		return [3]control.FPoint{
			{X: tipX, Y: center.Y},
			{X: backX, Y: center.Y + arrowHalf},
			{X: backX, Y: center.Y - arrowHalf},
		}
	}

	// +Y is up, so dir = Down (+1) points towards smaller Y.
	tipY := center.Y - float32(dir)*(h/2-tipInset)
	backY := tipY + float32(dir)*length
	return [3]control.FPoint{
		{X: center.X, Y: tipY},
		{X: center.X + arrowHalf, Y: backY},
		{X: center.X - arrowHalf, Y: backY},
	}
}
