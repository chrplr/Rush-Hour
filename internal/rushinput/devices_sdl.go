// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

//go:build !js

package rushinput

import (
	"fmt"
	"os"
	"sort"

	"github.com/Zyko0/go-sdl3/sdl"
)

// deviceSet holds the controllers the session has opened.
//
// SDL sends button events only for devices something has opened, so this is not
// bookkeeping: without it a plugged-in gamepad is silent. Devices SDL
// recognises (its mapping database covers every mass-market controller) are
// opened as gamepads, which relabels their hardware buttons into the standard
// south/east/shoulder/d-pad layout; the rest are opened as raw joysticks, where
// button 3 is whatever the manufacturer wired to button 3. A response box is
// usually in the second group, which is why the raw path exists at all.
type deviceSet struct {
	pads   map[sdl.JoystickID]*sdl.Gamepad
	sticks map[sdl.JoystickID]*sdl.Joystick
	label  map[sdl.JoystickID]string
	opened bool // scan has run at least once
}

// scan opens every attached controller not already held, closes the ones that
// have gone away, and returns the names of what is now open.
//
// It is called at startup and again on every device ADDED/REMOVED event, so a
// controller that enumerates a second after the experiment starts — or one the
// participant knocks loose in the scanner and an operator plugs back in — is
// picked up without restarting the session. One plugged-in gamepad produces
// several of those events, so scan has to be idempotent, and reports only what
// changed rather than the whole list each time.
func (d *deviceSet) scan(debug bool) []string {
	if d.pads == nil {
		d.pads = map[sdl.JoystickID]*sdl.Gamepad{}
		d.sticks = map[sdl.JoystickID]*sdl.Joystick{}
		d.label = map[sdl.JoystickID]string{}
	}

	present := map[sdl.JoystickID]bool{}
	var added []string

	// Gamepads first: a device that qualifies as one must not also be taken as
	// a raw joystick, or every press would arrive twice.
	ids, err := sdl.GetGamepads()
	if err != nil && debug {
		fmt.Fprintf(os.Stderr, "rushinput: cannot list gamepads: %v\n", err)
	}
	for _, id := range ids {
		present[id] = true
		if _, held := d.pads[id]; held {
			continue
		}
		pad, err := id.OpenGamepad()
		if err != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "rushinput: cannot open gamepad %d: %v\n", id, err)
			}
			continue
		}
		d.pads[id] = pad
		d.label[id] = fmt.Sprintf("%s (gamepad)", pad.Name())
		added = append(added, d.label[id])
	}

	sticks, err := sdl.GetJoysticks()
	if err != nil && debug {
		fmt.Fprintf(os.Stderr, "rushinput: cannot list joysticks: %v\n", err)
	}
	for _, id := range sticks {
		if _, isPad := d.pads[id]; isPad {
			continue
		}
		present[id] = true
		if _, held := d.sticks[id]; held {
			continue
		}
		js, err := id.OpenJoystick()
		if err != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "rushinput: cannot open joystick %d: %v\n", id, err)
			}
			continue
		}
		d.sticks[id] = js
		name, _ := js.Name()
		if name == "" {
			name = "unnamed device"
		}
		nButtons, _ := js.NumButtons()
		d.label[id] = fmt.Sprintf("%s (joystick, %d buttons)", name, nButtons)
		added = append(added, d.label[id])
	}

	for id, pad := range d.pads {
		if !present[id] {
			pad.Close()
			delete(d.pads, id)
			delete(d.label, id)
		}
	}
	for id, js := range d.sticks {
		if !present[id] {
			js.Close()
			delete(d.sticks, id)
			delete(d.label, id)
		}
	}

	if debug {
		if !d.opened && len(d.label) == 0 {
			fmt.Fprintln(os.Stderr, "rushinput: no gamepad or joystick found")
		}
		for _, n := range added {
			fmt.Fprintf(os.Stderr, "rushinput: using %s\n", n)
		}
	}
	d.opened = true

	// Only the first scan reports everything; later ones report what they
	// added, so a caller that prints the result does not repeat itself on
	// every device event.
	if len(added) == 0 {
		return nil
	}
	return added
}

// isGamepad reports whether SDL is reporting this device through the gamepad
// API, in which case its raw joystick events are duplicates to be ignored.
func (d *deviceSet) isGamepad(id sdl.JoystickID) bool {
	_, ok := d.pads[id]
	return ok
}

// names lists the open devices, in a stable order.
func (d *deviceSet) names() []string {
	names := make([]string, 0, len(d.label))
	for _, n := range d.label {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (d *deviceSet) close() {
	d.opened = false
	for id, pad := range d.pads {
		pad.Close()
		delete(d.pads, id)
	}
	for id, js := range d.sticks {
		js.Close()
		delete(d.sticks, id)
	}
	d.label = map[sdl.JoystickID]string{}
}
