// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

//go:build js

package rushinput

import "github.com/Zyko0/go-sdl3/sdl"

// The browser build initialises SDL without the joystick and gamepad
// subsystems (goxpyriment's control/platform_js.go leaves them out, because
// they fail to start under Emscripten), so there is nothing to open and the
// gamepad entry points are not linked in at all. Keyboard bindings still work,
// which is the whole of what the online demo needs.
type deviceSet struct{}

func (d *deviceSet) scan(debug bool) []string        { return nil }
func (d *deviceSet) isGamepad(_ sdl.JoystickID) bool { return false }
func (d *deviceSet) names() []string                 { return nil }
func (d *deviceSet) close()                          {}
