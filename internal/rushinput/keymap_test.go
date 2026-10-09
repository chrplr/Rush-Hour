// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rushinput

import (
	"strings"
	"testing"

	"github.com/Zyko0/go-sdl3/sdl"
)

// The default map has to leave a participant able to finish the puzzle on each
// of the three kinds of hardware, without a flag. That means, on each of them,
// some way to reach any vehicle and both ways to slide it.
func TestDefaultMapIsPlayableOnEachDevice(t *testing.T) {
	m := DefaultMap()

	canSelect := func(bound map[Action]bool) bool {
		spatial := bound[SelectUp] && bound[SelectDown] && bound[SelectLeft] && bound[SelectRight]
		sequential := bound[SelectPrev] || bound[SelectNext]
		return spatial || sequential
	}

	for _, c := range []struct {
		name  string
		bound map[Action]bool
	}{
		{"keyboard", boundActions(m.Keys)},
		{"gamepad", boundActions(m.Pad)},
		{"raw joystick", boundActions(m.Joy)},
	} {
		if !canSelect(c.bound) {
			t.Errorf("%s: no way to choose a vehicle", c.name)
		}
		// Sliding needs either both ways along the vehicle's own axis or all
		// four absolute directions; anything less leaves some vehicle stuck
		// one way.
		alongAxis := c.bound[MoveBack] && c.bound[MoveForward]
		absolute := c.bound[SlideLeft] && c.bound[SlideRight] && c.bound[SlideUp] && c.bound[SlideDown]
		if !alongAxis && !absolute {
			t.Errorf("%s: missing a slide direction (back=%v forward=%v, left=%v right=%v up=%v down=%v)",
				c.name, c.bound[MoveBack], c.bound[MoveForward],
				c.bound[SlideLeft], c.bound[SlideRight], c.bound[SlideUp], c.bound[SlideDown])
		}
	}
}

// The four-button response box is the reason the sequential order exists: keys
// 1..4 alone must be a complete interface.
func TestFourButtonBoxIsComplete(t *testing.T) {
	m := DefaultMap()
	want := map[sdl.Keycode]Action{
		sdl.K_1: MoveBack, sdl.K_2: MoveForward,
		sdl.K_3: SelectPrev, sdl.K_4: SelectNext,
	}
	for k, a := range want {
		if got := m.Keys[k]; got != a {
			t.Errorf("key %q: bound to %v, want %v", KeyName(k), got, a)
		}
	}
	// And on the keypad too, since a box may send either.
	for k, a := range map[sdl.Keycode]Action{
		sdl.K_KP_1: MoveBack, sdl.K_KP_2: MoveForward,
		sdl.K_KP_3: SelectPrev, sdl.K_KP_4: SelectNext,
	} {
		if got := m.Keys[k]; got != a {
			t.Errorf("key %q: bound to %v, want %v", KeyName(k), got, a)
		}
	}
}

// A response box that sends "b y g r" — the fORP default — has to be one flag.
func TestApplyKeysRemapsAResponseBox(t *testing.T) {
	m := DefaultMap()
	if err := m.ApplyKeys("b=back,y=forward,g=prev,r=next"); err != nil {
		t.Fatalf("ApplyKeys: %v", err)
	}
	for name, want := range map[string]Action{"b": MoveBack, "y": MoveForward, "g": SelectPrev, "r": SelectNext} {
		k, ok := ParseKeyName(name)
		if !ok {
			t.Fatalf("ParseKeyName(%q) failed", name)
		}
		if got := m.Keys[k]; got != want {
			t.Errorf("key %q: bound to %v, want %v", name, got, want)
		}
	}
	// Overriding adds to the defaults rather than replacing them, so the arrow
	// keys the experimenter uses to check the setup still work.
	if got := m.Keys[sdl.K_LEFT]; got != SlideLeft {
		t.Errorf("left arrow: bound to %v, want %v", got, SlideLeft)
	}
}

// The keyboard and the gamepad carry the same direct scheme: two controls walk
// the vehicles, and four slide the selected one in the direction they point —
// 's'/'d' and the arrows on the keyboard; the shoulders and the X/Y/A/B
// diamond, duplicated on the d-pad, on a gamepad.
func TestDefaultScheme(t *testing.T) {
	m := DefaultMap()
	for k, a := range map[sdl.Keycode]Action{
		sdl.K_S: SelectPrev, sdl.K_D: SelectNext,
		sdl.K_LEFT: SlideLeft, sdl.K_RIGHT: SlideRight,
		sdl.K_UP: SlideUp, sdl.K_DOWN: SlideDown,
	} {
		if got := m.Keys[k]; got != a {
			t.Errorf("key %q: bound to %v, want %v", KeyName(k), got, a)
		}
	}
	for b, a := range map[sdl.GamepadButton]Action{
		sdl.GAMEPAD_BUTTON_LEFT_SHOULDER:  SelectPrev,
		sdl.GAMEPAD_BUTTON_RIGHT_SHOULDER: SelectNext,
		sdl.GAMEPAD_BUTTON_WEST:           SlideLeft,
		sdl.GAMEPAD_BUTTON_EAST:           SlideRight,
		sdl.GAMEPAD_BUTTON_NORTH:          SlideUp,
		sdl.GAMEPAD_BUTTON_SOUTH:          SlideDown,
		sdl.GAMEPAD_BUTTON_DPAD_LEFT:      SlideLeft,
		sdl.GAMEPAD_BUTTON_DPAD_RIGHT:     SlideRight,
		sdl.GAMEPAD_BUTTON_DPAD_UP:        SlideUp,
		sdl.GAMEPAD_BUTTON_DPAD_DOWN:      SlideDown,
	} {
		if got := m.Pad[b]; got != a {
			t.Errorf("gamepad button %q: bound to %v, want %v", PadButtonName(b), got, a)
		}
	}
}

// A slide in an absolute direction acts only along the vehicle's own axis; the
// back/forward pair of the four-button box acts along whichever axis it has.
func TestSlideDir(t *testing.T) {
	for _, c := range []struct {
		a          Action
		horizontal bool
		dir        int
		ok         bool
	}{
		{MoveBack, true, -1, true}, {MoveBack, false, -1, true},
		{MoveForward, true, 1, true}, {MoveForward, false, 1, true},
		{SlideLeft, true, -1, true}, {SlideLeft, false, 0, false},
		{SlideRight, true, 1, true}, {SlideRight, false, 0, false},
		{SlideUp, false, -1, true}, {SlideUp, true, 0, false},
		{SlideDown, false, 1, true}, {SlideDown, true, 0, false},
		{SelectNext, true, 0, false}, {Confirm, false, 0, false},
	} {
		dir, ok := c.a.SlideDir(c.horizontal)
		if dir != c.dir || ok != c.ok {
			t.Errorf("%v.SlideDir(horizontal=%v) = (%d, %v), want (%d, %v)",
				c.a, c.horizontal, dir, ok, c.dir, c.ok)
		}
		if c.a.IsMove() != (c.a != SelectNext && c.a != Confirm) {
			t.Errorf("%v.IsMove() = %v", c.a, c.a.IsMove())
		}
	}
}

// Every action name a spec may use parses back to its action, the new
// hyphenated ones included.
func TestActionNamesRoundTrip(t *testing.T) {
	for _, name := range ActionNames() {
		a, ok := ParseAction(name)
		if !ok || a.String() != name {
			t.Errorf("ParseAction(%q) = (%v, %v)", name, a, ok)
		}
	}
	m := DefaultMap()
	if err := m.ApplyPad("x=slide-up,y=slide-left"); err != nil {
		t.Fatalf("ApplyPad: %v", err)
	}
	if m.Pad[sdl.GAMEPAD_BUTTON_WEST] != SlideUp || m.Pad[sdl.GAMEPAD_BUTTON_NORTH] != SlideLeft {
		t.Errorf("x/y after remap: %v/%v", m.Pad[sdl.GAMEPAD_BUTTON_WEST], m.Pad[sdl.GAMEPAD_BUTTON_NORTH])
	}
}

// The legend names the arrows first and does not list a keypad digit beside
// the plain digit it duplicates, so a participant is not told about a third
// button that is not on the box.
func TestLegendKeyNames(t *testing.T) {
	lines := DefaultMap().Legend(false)
	want := []string{
		"3 / s  —  choose the previous car",
		"4 / d  —  choose the next car",
		", / 1  —  slide it left, or up if it is vertical",
		". / 2  —  slide it right, or down if it is vertical",
		"left  —  slide a horizontal car left",
		"right  —  slide a horizontal car right",
		"up  —  slide a vertical car up",
		"down  —  slide a vertical car down",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("legend:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	// A keypad digit whose plain digit is unbound is still listed.
	m := DefaultMap()
	if err := m.ApplyKeys("1=none"); err != nil {
		t.Fatalf("ApplyKeys: %v", err)
	}
	if got := m.Legend(false)[2]; got != ", / kp1  —  slide it left, or up if it is vertical" {
		t.Errorf("legend with 1 unbound: %q", got)
	}
}

func TestApplyKeysClearAndNone(t *testing.T) {
	m := DefaultMap()
	if err := m.ApplyKeys("1=none"); err != nil {
		t.Fatalf("ApplyKeys: %v", err)
	}
	if _, still := m.Keys[sdl.K_1]; still {
		t.Error(`"1=none" left key 1 bound`)
	}
	if got := m.Keys[sdl.K_2]; got != MoveForward {
		t.Error(`"1=none" should not touch key 2`)
	}

	m = DefaultMap()
	if err := m.ApplyKeys("clear,a=back"); err != nil {
		t.Fatalf("ApplyKeys: %v", err)
	}
	if len(m.Keys) != 1 {
		t.Errorf(`"clear" left %d keyboard bindings, want 1`, len(m.Keys))
	}
	// Clearing the keyboard must not clear the gamepad: they are separate flags
	// because they are separate devices.
	if len(m.Pad) == 0 {
		t.Error(`"clear" on -keys emptied the gamepad map too`)
	}
}

func TestApplySpecErrors(t *testing.T) {
	for _, c := range []struct {
		spec, want string
		apply      func(*Map, string) error
	}{
		{"1", "expected <control>=<action>", (*Map).ApplyKeys},
		{"1=sideways", "unknown action", (*Map).ApplyKeys},
		{"nosuchkey=back", "unknown key", (*Map).ApplyKeys},
		{"nosuchbutton=back", "unknown gamepad button", (*Map).ApplyPad},
		{"leftshoulder=back", "joystick controls are button numbers", (*Map).ApplyJoy},
	} {
		m := DefaultMap()
		err := c.apply(&m, c.spec)
		if err == nil {
			t.Errorf("%q: no error, want one mentioning %q", c.spec, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: error %q does not mention %q", c.spec, err, c.want)
		}
	}
}

// A bad spec must not leave the map half-applied in a way the caller then uses:
// main exits on the error, so what matters is that the error is loud, which the
// test above covers. What this one checks is that a *good* spec is applied in
// order, so a later pair overrides an earlier one.
func TestApplyKeysLastPairWins(t *testing.T) {
	m := DefaultMap()
	if err := m.ApplyKeys("z=back,z=forward"); err != nil {
		t.Fatalf("ApplyKeys: %v", err)
	}
	k, _ := ParseKeyName("z")
	if got := m.Keys[k]; got != MoveForward {
		t.Errorf("key z: bound to %v, want %v", got, MoveForward)
	}
}

func TestKeyNameRoundTrip(t *testing.T) {
	for _, name := range []string{"up", "down", "left", "right", "space", "tab", "kp1", "kp9", "b", "1", ",", "."} {
		k, ok := ParseKeyName(name)
		if !ok {
			t.Errorf("ParseKeyName(%q) failed", name)
			continue
		}
		if got := KeyName(k); got != name {
			t.Errorf("KeyName(ParseKeyName(%q)) = %q", name, got)
		}
	}
	if _, ok := ParseKeyName("nosuchkey"); ok {
		t.Error(`ParseKeyName("nosuchkey") should fail`)
	}
}

func TestPadButtonNameRoundTrip(t *testing.T) {
	for _, name := range PadButtonNames() {
		b, ok := ParsePadButtonName(name)
		if !ok {
			t.Errorf("ParsePadButtonName(%q) failed", name)
			continue
		}
		// Aliases ("a" for "south") name a button whose canonical spelling is
		// the first entry, so only that one round-trips.
		if canon := PadButtonName(b); canon != name && !isAlias(name) {
			t.Errorf("PadButtonName(ParsePadButtonName(%q)) = %q", name, canon)
		}
	}
}

func isAlias(name string) bool {
	switch name {
	case "a", "b", "x", "y", "l1", "r1":
		return true
	}
	return false
}

// The instruction screen is written from the map, so it must name the controls
// that are actually bound — and must not promise gamepad buttons when no
// gamepad is plugged in.
func TestLegendFollowsTheBindings(t *testing.T) {
	m := DefaultMap()

	withPad := strings.Join(m.Legend(true), "\n")
	if !strings.Contains(withPad, "leftshoulder") {
		t.Errorf("legend with a gamepad does not mention the shoulder buttons:\n%s", withPad)
	}

	withoutPad := strings.Join(m.Legend(false), "\n")
	if strings.Contains(withoutPad, "leftshoulder") {
		t.Errorf("legend without a gamepad mentions gamepad buttons:\n%s", withoutPad)
	}
	if !strings.Contains(withoutPad, "left") {
		t.Errorf("legend without a gamepad does not mention the arrow keys:\n%s", withoutPad)
	}

	// A remapped box shows its own buttons, not the defaults.
	m = DefaultMap()
	if err := m.ApplyKeys("clear,b=back,y=forward,g=prev,r=next"); err != nil {
		t.Fatalf("ApplyKeys: %v", err)
	}
	legend := strings.Join(m.Legend(false), "\n")
	for _, want := range []string{"b", "y", "g", "r"} {
		if !strings.Contains(legend, want) {
			t.Errorf("legend does not mention remapped key %q:\n%s", want, legend)
		}
	}
	// "up" still appears inside "move it left / up"; what must be gone is the
	// arrow keys in the control column, i.e. a line that *starts* with one.
	for _, line := range m.Legend(false) {
		if strings.HasPrefix(line, "up ") || strings.HasPrefix(line, "left ") {
			t.Errorf("legend still offers a cleared arrow key: %q", line)
		}
	}
}

func boundActions[K comparable](table map[K]Action) map[Action]bool {
	out := map[Action]bool{}
	for _, a := range table {
		out[a] = true
	}
	return out
}
