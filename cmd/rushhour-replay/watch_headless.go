// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

//go:build !rushui

package main

import (
	"errors"
	"flag"

	"rush-hour/internal/rushreplay"
)

// This is the default build: no SDL, no window, nothing to link but the Go
// runtime. Checking a results file is something a data-analysis machine should
// be able to do, and goxpyriment carries SDL for every platform — tens of
// megabytes to open a window nobody asked for. The watchable build lives behind
// the rushui tag, in watch_sdl.go.

// renderFlag exists here only so that asking for a window in the wrong build
// says so, instead of the flag package's "flag provided but not defined".
var renderFlag *bool

func registerViewerFlags() {
	renderFlag = flag.Bool("render", false, "unavailable: rebuild with -tags rushui to watch a session")
	flag.Float64("speed", 1, "unavailable: rebuild with -tags rushui to watch a session")
}

func viewerEnabled() bool { return *renderFlag }

func watch(*rushreplay.Session, rushreplay.Library, int) error {
	return errors.New("this binary was built without a window; rebuild with: " +
		"go build -tags rushui -o rushhour-replay-view ./cmd/rushhour-replay")
}
