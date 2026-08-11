// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the MIT License.

//go:build !rushui

package main

import (
	"errors"
	"flag"
	"os"

	"rush-hour/internal/rushenv"
)

// This is the default build: no SDL, no window, nothing to link but the Go
// runtime. The watchable build lives behind the rushui tag, in serve_sdl.go.

// renderFlag exists here only so that asking for a window in the wrong build
// says so, instead of the flag package's "flag provided but not defined".
var renderFlag *bool

func registerViewerFlags() {
	renderFlag = flag.Bool("render", false, "unavailable: rebuild with -tags rushui to watch the agent play")
}

// viewerEnabled is always false here: this build has no window to open.
func viewerEnabled() bool { return false }

func serve(srv *rushenv.Server, _ *rushenv.Recorder) error {
	if *renderFlag {
		return errors.New("this binary was built without a window; rebuild with: " +
			"go build -tags rushui -o rushhour-env-view ./cmd/rushhour-env")
	}
	return srv.Run(os.Stdin, os.Stdout)
}
