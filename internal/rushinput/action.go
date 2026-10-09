// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

// Package rushinput turns key presses, gamepad buttons and joystick buttons
// into the handful of abstract actions the game needs, so that the trial loop
// never asks which device the participant is holding.
//
// The reason the indirection exists is the scanner. Inside an MRI or a MEG the
// participant has no mouse and no keyboard: they have a response box, and which
// box it is varies by site. Some enumerate as a USB keyboard and send fixed
// characters; some enumerate as a joystick and send button numbers that mean
// nothing outside that model; a piloting session at the desk uses a real
// keyboard or a gamepad. All four produce the same small set of actions here,
// and all four are remappable from the command line, because the alternative —
// editing the trial loop per site — is how a paradigm stops being the same
// paradigm.
package rushinput

import "strings"

// Action is one thing the participant can ask for. The vocabulary is
// deliberately small: everything the game needs is choosing a vehicle and
// sliding it, and a response box may have as few as four buttons to say it
// with.
type Action int

const (
	// None is the zero value: an event that maps to nothing.
	None Action = iota

	// SelectUp and friends move the selection to the neighbouring vehicle in
	// that direction (see rush.Board.Neighbour). They need four controls, so
	// they suit a d-pad, an analog stick, or the arrow keys.
	SelectUp
	SelectDown
	SelectLeft
	SelectRight

	// SelectPrev and SelectNext walk the vehicles in a fixed order instead
	// (rush.Board.Cycle). They are the fallback for a box with only two
	// buttons to spare on choosing.
	SelectPrev
	SelectNext

	// MoveBack slides the selected vehicle one cell towards the left wall if
	// it is horizontal, towards the top wall if it is vertical; MoveForward is
	// the other way. Both are the vehicle's own axis, so two controls suffice
	// whatever its orientation.
	MoveBack
	MoveForward

	// SlideLeft and friends slide the selected vehicle one cell in an absolute
	// direction, and only along its own axis: left and right act on a
	// horizontal vehicle, up and down on a vertical one, and a press across the
	// axis moves nothing (see SlideDir). They cost four controls where
	// MoveBack/MoveForward cost two, but each one means the same thing on
	// screen whichever vehicle is selected — the arrow keys, a d-pad, or the
	// X/Y/A/B diamond of a gamepad.
	SlideLeft
	SlideRight
	SlideUp
	SlideDown

	// Confirm dismisses an instruction screen — the participant's "go on".
	Confirm
)

// actionNames is the spelling used in the -keys/-pad/-joy specs and in the
// on-screen legend. Lower case and without spaces, so a spec never needs
// quoting.
var actionNames = map[Action]string{
	None:        "none",
	SelectUp:    "up",
	SelectDown:  "down",
	SelectLeft:  "left",
	SelectRight: "right",
	SelectPrev:  "prev",
	SelectNext:  "next",
	MoveBack:    "back",
	MoveForward: "forward",
	SlideLeft:   "slide-left",
	SlideRight:  "slide-right",
	SlideUp:     "slide-up",
	SlideDown:   "slide-down",
	Confirm:     "confirm",
}

func (a Action) String() string {
	if s, ok := actionNames[a]; ok {
		return s
	}
	return "action?"
}

// IsSelect reports whether the action moves the selection rather than a vehicle.
func (a Action) IsSelect() bool {
	switch a {
	case SelectUp, SelectDown, SelectLeft, SelectRight, SelectPrev, SelectNext:
		return true
	}
	return false
}

// IsMove reports whether the action slides the selected vehicle.
func (a Action) IsMove() bool {
	switch a {
	case MoveBack, MoveForward, SlideLeft, SlideRight, SlideUp, SlideDown:
		return true
	}
	return false
}

// SlideDir returns the one-cell step a move action asks of a vehicle with the
// given orientation: -1 towards column or row 0, +1 the other way — the values
// of rush.Left and rush.Right. ok is false when the action does not apply: a
// SlideLeft/SlideRight on a vertical vehicle, a SlideUp/SlideDown on a
// horizontal one, or an action that is not a move at all.
func (a Action) SlideDir(horizontal bool) (dir int, ok bool) {
	switch a {
	case MoveBack:
		return -1, true
	case MoveForward:
		return 1, true
	case SlideLeft, SlideRight:
		if !horizontal {
			return 0, false
		}
		if a == SlideLeft {
			return -1, true
		}
		return 1, true
	case SlideUp, SlideDown:
		if horizontal {
			return 0, false
		}
		if a == SlideUp {
			return -1, true
		}
		return 1, true
	}
	return 0, false
}

// ParseAction is the inverse of Action.String. ok is false for an unknown name.
func ParseAction(s string) (Action, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for a, name := range actionNames {
		if name == s {
			return a, true
		}
	}
	return None, false
}

// ActionNames lists the spellings ParseAction accepts, in action order, for
// error messages and -help text.
func ActionNames() []string {
	order := []Action{SelectUp, SelectDown, SelectLeft, SelectRight,
		SelectPrev, SelectNext, MoveBack, MoveForward,
		SlideLeft, SlideRight, SlideUp, SlideDown, Confirm}
	names := make([]string, len(order))
	for i, a := range order {
		names[i] = a.String()
	}
	return names
}
