// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rushinput

import (
	"fmt"
	"os"

	"github.com/Zyko0/go-sdl3/sdl"
)

// Axis thresholds, on SDL's −32768..32767 scale.
//
// A stick is a continuous device asked to answer a discrete question, so it
// needs a threshold; a single threshold would then fire repeatedly while the
// stick rested near it. The gap between enter and leave is the hysteresis that
// prevents that: once a direction has fired, the stick must come most of the
// way back to centre before it can fire again. Both are far enough from centre
// that a drifting or miscentred device — which a cheap response box may well
// be — sits below them.
const (
	axisEnter = 20000
	axisLeave = 12000

	// Triggers rest at 0 and only go positive, so they get their own, lower
	// pair.
	triggerEnter = 16000
	triggerLeave = 8000
)

// Event is one action, carrying the SDL3 hardware timestamp of the press that
// produced it — the same clock as Screen.FlipTS and as the mouse timestamps
// already in the results file, so a button press and a click are timed alike.
type Event struct {
	Action Action
	TsNS   uint64
}

// Reader translates SDL events into Events. Feed it from the experiment's own
// event pump:
//
//	state := exp.PollEvents(input.Handle)
//	for _, ev := range input.Drain() { ... }
//
// It is not safe for concurrent use, which is fine: SDL events must be polled
// from the thread that owns the window anyway.
type Reader struct {
	// Map is the live binding. Changing it between trials takes effect at once.
	Map Map

	// Axes reports whether analog sticks, hats and triggers produce actions.
	// Buttons are unaffected. Turning it off is the escape hatch for a response
	// box whose unused axes float around the threshold.
	Axes bool

	// Debug echoes every press to stderr, bound or not, with the name the -keys
	// / -pad / -joy specs would use for it. This is how the button numbers of
	// an unfamiliar response box get discovered: run once, press each button,
	// read them off.
	Debug bool

	queue []Event
	axis  map[axisKey]int
	dev   devices
}

// devices is the platform-dependent half: opening controllers and knowing
// which of them SDL is willing to describe as a gamepad. The browser build
// supplies a version that opens nothing, because its SDL is initialised without
// the joystick subsystem. Making it an interface also lets a test stand in for
// it, which is the only way to exercise the "same press, two APIs" case without
// a controller plugged into the machine running the tests.
type devices interface {
	scan(debug bool) []string
	isGamepad(sdl.JoystickID) bool
	names() []string
	close()
}

// axisKey identifies one axis of one device across events.
type axisKey struct {
	id   sdl.JoystickID
	raw  bool // a raw joystick axis rather than a mapped gamepad axis
	axis uint8
}

// New returns a Reader bound to m, with axes enabled.
func New(m Map) *Reader {
	return &Reader{Map: m, Axes: true, axis: map[axisKey]int{}, dev: &deviceSet{}}
}

// Open takes hold of every gamepad and joystick currently attached and returns
// their names, for the experimenter's log. SDL delivers button events only for
// devices that have been opened, so nothing arrives from a controller until
// this has run; devices plugged in later are picked up from their ADDED events.
//
// On a build with no joystick support (the browser one) it returns nil and the
// keyboard path keeps working.
func (r *Reader) Open() []string { return r.dev.scan(r.Debug) }

// Close releases the devices. Safe to call twice.
func (r *Reader) Close() { r.dev.close() }

// Devices returns the names of the controllers currently held, so a caller can
// ask whether to mention gamepad buttons in the instructions.
func (r *Reader) Devices() []string { return r.dev.names() }

// Drain returns the actions accumulated since the last call, oldest first, and
// empties the queue.
func (r *Reader) Drain() []Event {
	if len(r.queue) == 0 {
		return nil
	}
	out := r.queue
	r.queue = nil
	return out
}

// Handle is the callback for control.Experiment.PollEvents. It never asks the
// pump to stop early: the experiment's own quit and mouse handling live in the
// same loop and must see every event.
func (r *Reader) Handle(ev sdl.Event) bool {
	switch ev.Type {
	case sdl.EVENT_KEY_DOWN:
		ke := ev.KeyboardEvent()
		// Auto-repeat is the operating system inventing presses the
		// participant did not make. One press, one action, one row.
		if ke.Repeat {
			break
		}
		r.press(r.Map.Keys[ke.Key], ke.Timestamp, "key", KeyName(ke.Key))

	case sdl.EVENT_GAMEPAD_BUTTON_DOWN:
		ge := ev.GamepadButtonEvent()
		b := sdl.GamepadButton(ge.Button)
		r.press(r.Map.Pad[b], ge.Timestamp, "gamepad button", PadButtonName(b))

	case sdl.EVENT_JOYSTICK_BUTTON_DOWN:
		je := ev.JoyButtonEvent()
		// A gamepad is also a joystick, and SDL reports it as both. Taking the
		// gamepad events and dropping these keeps one press from counting
		// twice.
		if r.dev.isGamepad(je.Which) {
			break
		}
		r.press(r.Map.Joy[int(je.Button)], je.Timestamp, "joystick button", fmt.Sprint(je.Button))

	case sdl.EVENT_GAMEPAD_AXIS_MOTION:
		if !r.Axes {
			break
		}
		ae := ev.GamepadAxisEvent()
		r.gamepadAxis(ae)

	case sdl.EVENT_JOYSTICK_AXIS_MOTION:
		if !r.Axes || r.dev.isGamepad(ev.JoyAxisEvent().Which) {
			break
		}
		ae := ev.JoyAxisEvent()
		// Raw joysticks have no standard layout; axis 0/1 is the stick on
		// essentially everything that has one.
		var act [2]Action
		switch ae.Axis {
		case 0:
			act = [2]Action{SelectLeft, SelectRight}
		case 1:
			act = [2]Action{SelectUp, SelectDown}
		default:
			break
		}
		if act[0] != None {
			r.axisEdge(axisKey{ae.Which, true, ae.Axis}, ae.Value, act[0], act[1], ae.Timestamp)
		}

	case sdl.EVENT_JOYSTICK_HAT_MOTION:
		if !r.Axes || r.dev.isGamepad(ev.JoyHatEvent().Which) {
			break
		}
		r.hat(ev.JoyHatEvent())

	case sdl.EVENT_GAMEPAD_ADDED, sdl.EVENT_GAMEPAD_REMOVED,
		sdl.EVENT_JOYSTICK_ADDED, sdl.EVENT_JOYSTICK_REMOVED:
		// A controller unplugged mid-session and plugged back in — or, more
		// often, one that finished enumerating just after the experiment
		// started — must not need a restart.
		r.dev.scan(r.Debug)
	}
	return false
}

// gamepadAxis turns one axis event from a mapped gamepad into an action.
func (r *Reader) gamepadAxis(ae *sdl.GamepadAxisEvent) {
	key := axisKey{ae.Which, false, ae.Axis}
	switch sdl.GamepadAxis(ae.Axis) {
	case sdl.GAMEPAD_AXIS_LEFTX, sdl.GAMEPAD_AXIS_RIGHTX:
		r.axisEdge(key, ae.Value, SelectLeft, SelectRight, ae.Timestamp)
	case sdl.GAMEPAD_AXIS_LEFTY, sdl.GAMEPAD_AXIS_RIGHTY:
		// SDL's Y grows downwards.
		r.axisEdge(key, ae.Value, SelectUp, SelectDown, ae.Timestamp)
	case sdl.GAMEPAD_AXIS_LEFT_TRIGGER:
		r.triggerEdge(key, ae.Value, MoveBack, ae.Timestamp)
	case sdl.GAMEPAD_AXIS_RIGHT_TRIGGER:
		r.triggerEdge(key, ae.Value, MoveForward, ae.Timestamp)
	}
}

// axisEdge fires neg or pos on the crossing into a deflection, and nothing at
// all while the stick stays there or drifts back.
func (r *Reader) axisEdge(key axisKey, value int16, neg, pos Action, ts uint64) {
	prev := r.axis[key]
	next := prev
	switch {
	case int(value) >= axisEnter:
		next = 1
	case int(value) <= -axisEnter:
		next = -1
	case int(value) > -axisLeave && int(value) < axisLeave:
		next = 0
	}
	if next == prev {
		return
	}
	r.axis[key] = next
	switch next {
	case 1:
		r.press(pos, ts, "axis", fmt.Sprintf("%d+", key.axis))
	case -1:
		r.press(neg, ts, "axis", fmt.Sprintf("%d-", key.axis))
	}
}

// triggerEdge is axisEdge for a control that rests at zero and only pulls one
// way.
func (r *Reader) triggerEdge(key axisKey, value int16, a Action, ts uint64) {
	prev := r.axis[key]
	switch {
	case int(value) >= triggerEnter && prev == 0:
		r.axis[key] = 1
		r.press(a, ts, "trigger", fmt.Sprint(key.axis))
	case int(value) <= triggerLeave && prev != 0:
		r.axis[key] = 0
	}
}

// SDL hat bit values (SDL_HAT_UP and friends), which go-sdl3 does not export.
const (
	hatUp    = 0x01
	hatRight = 0x02
	hatDown  = 0x04
	hatLeft  = 0x08
)

// hat turns a raw joystick's d-pad into select actions, one per newly pressed
// direction.
func (r *Reader) hat(he *sdl.JoyHatEvent) {
	key := axisKey{he.Which, true, 0x80 | he.Hat} // 0x80: hats share the axis map
	prev := uint8(r.axis[key])
	r.axis[key] = int(he.Value)

	for _, d := range []struct {
		bit uint8
		a   Action
	}{{hatUp, SelectUp}, {hatDown, SelectDown}, {hatLeft, SelectLeft}, {hatRight, SelectRight}} {
		if he.Value&d.bit != 0 && prev&d.bit == 0 {
			r.press(d.a, he.Timestamp, "hat", fmt.Sprintf("%d %s", he.Hat, d.a))
		}
	}
}

// press queues an action and, in debug mode, reports the control that produced
// it whether or not it was bound.
func (r *Reader) press(a Action, ts uint64, what, name string) {
	if r.Debug {
		if a == None {
			fmt.Fprintf(os.Stderr, "rushinput: %s %q — not bound\n", what, name)
		} else {
			fmt.Fprintf(os.Stderr, "rushinput: %s %q → %s\n", what, name, a)
		}
	}
	if a != None {
		r.queue = append(r.queue, Event{Action: a, TsNS: ts})
	}
}
