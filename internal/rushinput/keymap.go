// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package rushinput

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Zyko0/go-sdl3/sdl"
)

// A Map says which control does what. The three tables are separate namespaces
// because the three device classes are: a key, a gamepad button and a raw
// joystick button number can all be "1" and mean different things.
type Map struct {
	Keys map[sdl.Keycode]Action
	Pad  map[sdl.GamepadButton]Action
	Joy  map[int]Action
}

// DefaultMap is the mapping a session uses when nothing is overridden.
//
// It is chosen so that the three plausible pieces of hardware all work without
// configuration:
//
//   - A gamepad gets the fast scheme: the d-pad (or the left stick) walks the
//     selection over the board, the two shoulder buttons slide the selected
//     vehicle back and forward. X and B duplicate the shoulders for players who
//     reach for the face buttons, and the triggers duplicate them again.
//
//   - A keyboard gets the same thing on the arrow keys, with ',' and '.' for
//     the two slides.
//
//   - A four-button response box sending "1 2 3 4" — the common MRI case — gets
//     the reduced scheme: 1 and 2 slide, 3 and 4 walk the vehicles in order.
//     Four buttons cannot carry four directions *and* two slides, so selection
//     falls back from spatial to sequential rather than losing a direction.
//
// A box that sends other characters (fORP's "b y g r", say) needs one -keys
// flag; a box that enumerates as a joystick needs one -joy flag.
func DefaultMap() Map {
	return Map{
		Keys: map[sdl.Keycode]Action{
			sdl.K_UP:       SelectUp,
			sdl.K_DOWN:     SelectDown,
			sdl.K_LEFT:     SelectLeft,
			sdl.K_RIGHT:    SelectRight,
			sdl.K_COMMA:    MoveBack,
			sdl.K_PERIOD:   MoveForward,
			sdl.K_1:        MoveBack,
			sdl.K_2:        MoveForward,
			sdl.K_3:        SelectPrev,
			sdl.K_4:        SelectNext,
			sdl.K_KP_1:     MoveBack,
			sdl.K_KP_2:     MoveForward,
			sdl.K_KP_3:     SelectPrev,
			sdl.K_KP_4:     SelectNext,
			sdl.K_SPACE:    Confirm,
			sdl.K_RETURN:   Confirm,
			sdl.K_KP_ENTER: Confirm,
		},
		Pad: map[sdl.GamepadButton]Action{
			sdl.GAMEPAD_BUTTON_DPAD_UP:        SelectUp,
			sdl.GAMEPAD_BUTTON_DPAD_DOWN:      SelectDown,
			sdl.GAMEPAD_BUTTON_DPAD_LEFT:      SelectLeft,
			sdl.GAMEPAD_BUTTON_DPAD_RIGHT:     SelectRight,
			sdl.GAMEPAD_BUTTON_LEFT_SHOULDER:  MoveBack,
			sdl.GAMEPAD_BUTTON_RIGHT_SHOULDER: MoveForward,
			sdl.GAMEPAD_BUTTON_WEST:           MoveBack,
			sdl.GAMEPAD_BUTTON_EAST:           MoveForward,
			sdl.GAMEPAD_BUTTON_SOUTH:          Confirm,
			sdl.GAMEPAD_BUTTON_START:          Confirm,
		},
		// A raw joystick has no standard layout, so the only honest default is
		// the four-button scheme on the first four buttons, with 5 and 6 (index
		// 4 and 5, the shoulders on most pads) duplicating the slides. Run with
		// -input-debug to read the real numbers off the box.
		Joy: map[int]Action{
			0: MoveBack,
			1: MoveForward,
			2: SelectPrev,
			3: SelectNext,
			4: MoveBack,
			5: MoveForward,
		},
	}
}

// Clone returns a deep copy, so overriding one session's map cannot reach into
// the defaults.
func (m Map) Clone() Map {
	out := Map{
		Keys: make(map[sdl.Keycode]Action, len(m.Keys)),
		Pad:  make(map[sdl.GamepadButton]Action, len(m.Pad)),
		Joy:  make(map[int]Action, len(m.Joy)),
	}
	for k, v := range m.Keys {
		out.Keys[k] = v
	}
	for k, v := range m.Pad {
		out.Pad[k] = v
	}
	for k, v := range m.Joy {
		out.Joy[k] = v
	}
	return out
}

// ── Spec parsing ─────────────────────────────────────────────────────────────
//
// A spec is a comma-separated list of "control=action" pairs, applied on top of
// the defaults:
//
//	-keys "b=back,y=forward,g=prev,r=next"
//	-pad  "dpup=none,north=confirm"
//	-joy  "0=prev,1=next,2=back,3=forward"
//
// Binding a control to "none" removes it, and the pseudo-pair "clear" as the
// first item starts from an empty table instead of the defaults. Overriding
// rather than replacing is what makes the common case — a box that sends four
// characters that are not 1 2 3 4 — a single short flag.

// ApplyKeys applies a keyboard spec to m.
func (m *Map) ApplyKeys(spec string) error {
	return applySpec(spec, "key", func() { m.Keys = map[sdl.Keycode]Action{} },
		func(name string, a Action) error {
			k, ok := ParseKeyName(name)
			if !ok {
				return fmt.Errorf("unknown key %q", name)
			}
			bind(m.Keys, k, a)
			return nil
		})
}

// ApplyPad applies a gamepad-button spec to m.
func (m *Map) ApplyPad(spec string) error {
	return applySpec(spec, "gamepad button", func() { m.Pad = map[sdl.GamepadButton]Action{} },
		func(name string, a Action) error {
			b, ok := ParsePadButtonName(name)
			if !ok {
				return fmt.Errorf("unknown gamepad button %q (try one of: %s)",
					name, strings.Join(PadButtonNames(), " "))
			}
			bind(m.Pad, b, a)
			return nil
		})
}

// ApplyJoy applies a raw-joystick-button spec to m. Controls are button
// numbers, counted from 0 the way SDL reports them.
func (m *Map) ApplyJoy(spec string) error {
	return applySpec(spec, "joystick button", func() { m.Joy = map[int]Action{} },
		func(name string, a Action) error {
			n, err := strconv.Atoi(name)
			if err != nil || n < 0 {
				return fmt.Errorf("joystick controls are button numbers from 0, got %q", name)
			}
			bind(m.Joy, n, a)
			return nil
		})
}

// bind sets or, for None, removes a binding.
func bind[K comparable](table map[K]Action, key K, a Action) {
	if a == None {
		delete(table, key)
		return
	}
	table[key] = a
}

func applySpec(spec, what string, clear func(), set func(string, Action) error) error {
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.EqualFold(item, "clear") {
			clear()
			continue
		}
		name, actName, ok := strings.Cut(item, "=")
		if !ok {
			return fmt.Errorf("%s binding %q: expected <control>=<action>", what, item)
		}
		a, ok := ParseAction(actName)
		if !ok {
			return fmt.Errorf("%s binding %q: unknown action %q (one of: %s, none)",
				what, item, strings.TrimSpace(actName), strings.Join(ActionNames(), " "))
		}
		if err := set(strings.TrimSpace(name), a); err != nil {
			return fmt.Errorf("%s binding %q: %w", what, item, err)
		}
	}
	return nil
}

// ── Control names ────────────────────────────────────────────────────────────

// namedKeys covers the keys whose name is not simply the character they type.
var namedKeys = map[string]sdl.Keycode{
	"up": sdl.K_UP, "down": sdl.K_DOWN, "left": sdl.K_LEFT, "right": sdl.K_RIGHT,
	"space": sdl.K_SPACE, "return": sdl.K_RETURN, "enter": sdl.K_RETURN,
	"tab": sdl.K_TAB, "backspace": sdl.K_BACKSPACE,
	"kp0": sdl.K_KP_0, "kp1": sdl.K_KP_1, "kp2": sdl.K_KP_2, "kp3": sdl.K_KP_3,
	"kp4": sdl.K_KP_4, "kp5": sdl.K_KP_5, "kp6": sdl.K_KP_6, "kp7": sdl.K_KP_7,
	"kp8": sdl.K_KP_8, "kp9": sdl.K_KP_9, "kpenter": sdl.K_KP_ENTER,
}

// ParseKeyName maps a name from a -keys spec to a keycode. A one-character name
// is the key that types that character — SDL keycodes for printable ASCII are
// the character itself, so "b", "1" and "," need no table.
func ParseKeyName(name string) (sdl.Keycode, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if k, ok := namedKeys[name]; ok {
		return k, true
	}
	if len(name) == 1 && name[0] >= 0x20 && name[0] < 0x7f {
		return sdl.Keycode(name[0]), true
	}
	return 0, false
}

// KeyName is the inverse of ParseKeyName, for the on-screen legend.
func KeyName(k sdl.Keycode) string {
	for name, kc := range namedKeys {
		if kc == k && !strings.HasPrefix(name, "kp") && name != "enter" {
			return name
		}
	}
	for name, kc := range namedKeys {
		if kc == k {
			return name
		}
	}
	if k >= 0x20 && k < 0x7f {
		return string(rune(k))
	}
	return fmt.Sprintf("key-%d", int(k))
}

// padButtonNames uses SDL3's own spellings, with the older A/B/X/Y labels kept
// as aliases because that is what is printed on most controllers.
var padButtonNames = []struct {
	name   string
	button sdl.GamepadButton
}{
	{"south", sdl.GAMEPAD_BUTTON_SOUTH}, {"a", sdl.GAMEPAD_BUTTON_SOUTH},
	{"east", sdl.GAMEPAD_BUTTON_EAST}, {"b", sdl.GAMEPAD_BUTTON_EAST},
	{"west", sdl.GAMEPAD_BUTTON_WEST}, {"x", sdl.GAMEPAD_BUTTON_WEST},
	{"north", sdl.GAMEPAD_BUTTON_NORTH}, {"y", sdl.GAMEPAD_BUTTON_NORTH},
	{"back", sdl.GAMEPAD_BUTTON_BACK},
	{"guide", sdl.GAMEPAD_BUTTON_GUIDE},
	{"start", sdl.GAMEPAD_BUTTON_START},
	{"leftstick", sdl.GAMEPAD_BUTTON_LEFT_STICK},
	{"rightstick", sdl.GAMEPAD_BUTTON_RIGHT_STICK},
	{"leftshoulder", sdl.GAMEPAD_BUTTON_LEFT_SHOULDER}, {"l1", sdl.GAMEPAD_BUTTON_LEFT_SHOULDER},
	{"rightshoulder", sdl.GAMEPAD_BUTTON_RIGHT_SHOULDER}, {"r1", sdl.GAMEPAD_BUTTON_RIGHT_SHOULDER},
	{"dpup", sdl.GAMEPAD_BUTTON_DPAD_UP},
	{"dpdown", sdl.GAMEPAD_BUTTON_DPAD_DOWN},
	{"dpleft", sdl.GAMEPAD_BUTTON_DPAD_LEFT},
	{"dpright", sdl.GAMEPAD_BUTTON_DPAD_RIGHT},
}

// ParsePadButtonName maps a name from a -pad spec to a gamepad button.
func ParsePadButtonName(name string) (sdl.GamepadButton, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, e := range padButtonNames {
		if e.name == name {
			return e.button, true
		}
	}
	return 0, false
}

// PadButtonName is the inverse, returning the first (canonical) spelling.
func PadButtonName(b sdl.GamepadButton) string {
	for _, e := range padButtonNames {
		if e.button == b {
			return e.name
		}
	}
	return fmt.Sprintf("pad-%d", int(b))
}

// PadButtonNames lists the accepted spellings, for error messages.
func PadButtonNames() []string {
	names := make([]string, len(padButtonNames))
	for i, e := range padButtonNames {
		names[i] = e.name
	}
	return names
}

// ── Legend ───────────────────────────────────────────────────────────────────

// legendOrder is the order actions appear on the instruction screen: choosing a
// vehicle first, then moving it, which is the order they are used in.
var legendOrder = []Action{
	SelectUp, SelectDown, SelectLeft, SelectRight,
	SelectPrev, SelectNext, MoveBack, MoveForward,
}

// legendText is what each action is called for a participant, who has no reason
// to know the word "vehicle axis".
var legendText = map[Action]string{
	SelectUp:    "choose the car above",
	SelectDown:  "choose the car below",
	SelectLeft:  "choose the car to the left",
	SelectRight: "choose the car to the right",
	SelectPrev:  "choose the previous car",
	SelectNext:  "choose the next car",
	MoveBack:    "move it left / up",
	MoveForward: "move it right / down",
}

// Legend describes the live bindings, one line per action that has any, for the
// instruction screen. Writing the legend from the map rather than from a
// constant string is the point: a site that remaps its response box sees its
// own buttons in the instructions, and cannot be shown a lie.
//
// pads reports whether a gamepad or joystick is actually connected; when none
// is, the gamepad column is left out rather than telling the participant about
// buttons they do not have.
func (m Map) Legend(pads bool) []string {
	keys := invert(m.Keys)
	pad := invert(m.Pad)
	joy := invert(m.Joy)

	var lines []string
	for _, a := range legendOrder {
		var controls []string
		if names := sortedNames(keys[a], KeyName); len(names) > 0 {
			controls = append(controls, strings.Join(names, " / "))
		}
		if pads {
			if names := sortedNames(pad[a], PadButtonName); len(names) > 0 {
				controls = append(controls, strings.Join(names, " / "))
			}
			if names := sortedNames(joy[a], func(n int) string { return "button " + strconv.Itoa(n+1) }); len(names) > 0 {
				controls = append(controls, strings.Join(names, " / "))
			}
		}
		if len(controls) == 0 {
			continue
		}
		// The instruction screen centres every line, so the two columns cannot
		// be aligned with padding; a dash between them is what reads.
		lines = append(lines, fmt.Sprintf("%s  —  %s", strings.Join(controls, " or "), legendText[a]))
	}
	return lines
}

func invert[K comparable](table map[K]Action) map[Action][]K {
	out := map[Action][]K{}
	for k, a := range table {
		out[a] = append(out[a], k)
	}
	return out
}

// sortedNames renders a set of controls in a stable order, so two runs of the
// same session show the same legend.
func sortedNames[K comparable](controls []K, name func(K) string) []string {
	names := make([]string, 0, len(controls))
	for _, c := range controls {
		names = append(names, name(c))
	}
	sort.Strings(names)
	return names
}
