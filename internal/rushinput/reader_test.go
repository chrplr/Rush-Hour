// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rushinput

import (
	"testing"
	"unsafe"

	"github.com/Zyko0/go-sdl3/sdl"
)

// SDL's Event is a union: the accessors reinterpret its bytes as the event
// struct the Type field selects. Building one for a test means doing the same
// thing in reverse — writing a typed event over the union — which is what these
// helpers do. It is the only way to exercise Handle without a device attached,
// and a scanner is exactly where finding out that Handle is wrong is most
// expensive.
func event[T any](e T) sdl.Event {
	var ev sdl.Event
	if unsafe.Sizeof(e) > unsafe.Sizeof(ev) {
		panic("event does not fit in the SDL union")
	}
	*(*T)(unsafe.Pointer(&ev)) = e
	return ev
}

func keyDown(k sdl.Keycode, ts uint64) sdl.Event {
	return event(sdl.KeyboardEvent{Type: sdl.EVENT_KEY_DOWN, Timestamp: ts, Key: k, Down: true})
}

func keyRepeat(k sdl.Keycode) sdl.Event {
	return event(sdl.KeyboardEvent{Type: sdl.EVENT_KEY_DOWN, Key: k, Down: true, Repeat: true})
}

func padDown(b sdl.GamepadButton, ts uint64) sdl.Event {
	return event(sdl.GamepadButtonEvent{
		Type: sdl.EVENT_GAMEPAD_BUTTON_DOWN, Timestamp: ts, Button: uint8(b), Down: true,
	})
}

func padAxis(axis sdl.GamepadAxis, value int16) sdl.Event {
	return event(sdl.GamepadAxisEvent{
		Type: sdl.EVENT_GAMEPAD_AXIS_MOTION, Axis: uint8(axis), Value: value,
	})
}

func joyDown(button uint8) sdl.Event {
	return event(sdl.JoyButtonEvent{Type: sdl.EVENT_JOYSTICK_BUTTON_DOWN, Button: button, Down: true})
}

func joyHat(value uint8) sdl.Event {
	return event(sdl.JoyHatEvent{Type: sdl.EVENT_JOYSTICK_HAT_MOTION, Value: value})
}

// feed pushes events through the reader and returns the actions they produced.
func feed(r *Reader, events ...sdl.Event) []Action {
	for _, ev := range events {
		if stop := r.Handle(ev); stop {
			panic("Handle asked the event pump to stop; it must never do that")
		}
	}
	var out []Action
	for _, e := range r.Drain() {
		out = append(out, e.Action)
	}
	return out
}

func want(t *testing.T, got []Action, expected ...Action) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("got %v, want %v", got, expected)
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("got %v, want %v", got, expected)
		}
	}
}

func TestKeysProduceActions(t *testing.T) {
	r := New(DefaultMap())
	want(t, feed(r, keyDown(sdl.K_LEFT, 0), keyDown(sdl.K_1, 0), keyDown(sdl.K_2, 0)),
		SelectPrev, MoveBack, MoveForward)
}

func TestUnboundKeyProducesNothing(t *testing.T) {
	r := New(DefaultMap())
	want(t, feed(r, keyDown(sdl.K_F, 0)))
}

// Auto-repeat is the operating system inventing presses. A participant holding
// a button down must not slide a vehicle across the board.
func TestKeyRepeatIsIgnored(t *testing.T) {
	r := New(DefaultMap())
	want(t, feed(r, keyDown(sdl.K_1, 0), keyRepeat(sdl.K_1), keyRepeat(sdl.K_1)), MoveBack)
}

// The hardware timestamp has to survive: it is what the results file times the
// press with, and it is on the same clock as the mouse timestamps already
// there.
func TestTimestampSurvives(t *testing.T) {
	r := New(DefaultMap())
	r.Handle(keyDown(sdl.K_1, 123456789))
	events := r.Drain()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].TsNS != 123456789 {
		t.Errorf("timestamp = %d, want 123456789", events[0].TsNS)
	}
}

func TestDrainEmptiesTheQueue(t *testing.T) {
	r := New(DefaultMap())
	feed(r, keyDown(sdl.K_1, 0))
	if got := r.Drain(); got != nil {
		t.Errorf("second Drain returned %v, want nothing", got)
	}
}

func TestGamepadButtons(t *testing.T) {
	r := New(DefaultMap())
	want(t, feed(r,
		padDown(sdl.GAMEPAD_BUTTON_DPAD_UP, 0),
		padDown(sdl.GAMEPAD_BUTTON_LEFT_SHOULDER, 0),
		padDown(sdl.GAMEPAD_BUTTON_RIGHT_SHOULDER, 0),
	), SelectUp, MoveBack, MoveForward)
}

// A stick is a continuous control answering a discrete question: one push must
// be one action, however many events the hardware sends on the way there and
// back.
func TestStickFiresOncePerPush(t *testing.T) {
	r := New(DefaultMap())
	want(t, feed(r,
		padAxis(sdl.GAMEPAD_AXIS_LEFTX, 5000),  // still centred
		padAxis(sdl.GAMEPAD_AXIS_LEFTX, 19000), // inside the deadband
		padAxis(sdl.GAMEPAD_AXIS_LEFTX, 24000), // crossed: one action
		padAxis(sdl.GAMEPAD_AXIS_LEFTX, 32000), // held: nothing
		padAxis(sdl.GAMEPAD_AXIS_LEFTX, 15000), // on the way back, still nothing
		padAxis(sdl.GAMEPAD_AXIS_LEFTX, 0),     // recentred
		padAxis(sdl.GAMEPAD_AXIS_LEFTX, 24000), // pushed again: a second action
	), SelectRight, SelectRight)
}

// A stick that rests just inside the threshold and jitters must not spray
// actions: that is the failure that would quietly corrupt a session run with a
// cheap box whose unused axes drift.
func TestStickJitterDoesNotRepeat(t *testing.T) {
	r := New(DefaultMap())
	events := []sdl.Event{padAxis(sdl.GAMEPAD_AXIS_LEFTY, 21000)}
	for range 20 {
		events = append(events,
			padAxis(sdl.GAMEPAD_AXIS_LEFTY, 20500),
			padAxis(sdl.GAMEPAD_AXIS_LEFTY, 21500))
	}
	want(t, feed(r, events...), SelectDown) // SDL's Y grows downwards
}

func TestAxesCanBeTurnedOff(t *testing.T) {
	r := New(DefaultMap())
	r.Axes = false
	want(t, feed(r,
		padAxis(sdl.GAMEPAD_AXIS_LEFTX, 32000),
		joyHat(hatUp),
		padDown(sdl.GAMEPAD_BUTTON_DPAD_UP, 0), // buttons keep working
	), SelectUp)
}

func TestTriggersSlide(t *testing.T) {
	r := New(DefaultMap())
	want(t, feed(r,
		padAxis(sdl.GAMEPAD_AXIS_LEFT_TRIGGER, 20000),
		padAxis(sdl.GAMEPAD_AXIS_LEFT_TRIGGER, 30000), // held
		padAxis(sdl.GAMEPAD_AXIS_LEFT_TRIGGER, 0),     // released
		padAxis(sdl.GAMEPAD_AXIS_RIGHT_TRIGGER, 20000),
	), MoveBack, MoveForward)
}

func TestRawJoystickButtons(t *testing.T) {
	r := New(DefaultMap())
	want(t, feed(r, joyDown(0), joyDown(1), joyDown(2), joyDown(3)),
		MoveBack, MoveForward, SelectPrev, SelectNext)
}

// A hat is a d-pad: each direction newly pressed is one action, and a diagonal
// is two.
func TestRawJoystickHat(t *testing.T) {
	r := New(DefaultMap())
	want(t, feed(r,
		joyHat(hatUp),
		joyHat(hatUp),           // held: nothing new
		joyHat(hatUp|hatRight),  // right added
		joyHat(0),               // centred
		joyHat(hatDown|hatLeft), // both new
	), SelectUp, SelectRight, SelectDown, SelectLeft)
}

// SDL reports a recognised controller through both APIs. Taking the gamepad
// events and dropping the raw ones is what stops one press counting twice — and
// counting twice would slide a vehicle two cells for one button.
func TestGamepadEventsAreNotCountedTwice(t *testing.T) {
	r := New(DefaultMap())
	r.dev = &fakeGamepad{}
	want(t, feed(r,
		padDown(sdl.GAMEPAD_BUTTON_LEFT_SHOULDER, 0),
		joyDown(0), // the same physical press, seen as a raw joystick button
	), MoveBack)
}

// fakeGamepad claims every device is a recognised gamepad, so the raw joystick
// path is the one under test.
type fakeGamepad struct{ deviceSet }

func (fakeGamepad) isGamepad(sdl.JoystickID) bool { return true }
