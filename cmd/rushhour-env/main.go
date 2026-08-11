// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the MIT License.

// Command rushhour-env serves Rush Hour boards over a line-oriented JSON
// protocol on stdin/stdout, so a program in another language can play them.
//
// It exists for the Gymnasium environment in python/, which spawns one of these
// per training run and exchanges one JSON object per line. Keeping the rules in
// Go means the agent plays exactly the game the human participants play, rather
// than a second implementation that can drift.
//
// The protocol is meant to be driveable by hand:
//
//	$ rushhour-env -board
//	{"id":1,"cmd":"hello"}
//	{"id":2,"cmd":"reset","puzzle":"p02"}
//	{"id":3,"cmd":"step","action":1}
//
// Anything this program has to say goes to stderr: stdout carries the protocol
// and nothing else.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"rush-hour/internal/rush"
	"rush-hour/internal/rushenv"
)

// version is stamped into the handshake so a client can tell which build it is
// talking to.
var version = "dev"

func main() {
	log.SetFlags(0)
	log.SetPrefix("rushhour-env: ")
	log.SetOutput(os.Stderr)

	var (
		puzzlePath  = flag.String("puzzles", "", "path to an alternative puzzle library (default: the embedded one)")
		maxVehicles = flag.Int("max-vehicles", rushenv.DefaultMaxVehicles, "size of the action space, as 2×this")
		includeBd   = flag.Bool("board", false, "add the six-line board notation to every state")
		canonical   = flag.Bool("canonical", true, "index vehicles by position rather than by letter")
		strict      = flag.Bool("strict", false, "treat an action that moves nothing as an error")
		showVersion = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stderr, version)
		return
	}

	opts := rushenv.Options{
		MaxVehicles:  *maxVehicles,
		IncludeBoard: *includeBd,
		Canonical:    *canonical,
		Strict:       *strict,
		Version:      version,
	}
	if *puzzlePath != "" {
		puzzles, err := rush.LoadPuzzleFile(*puzzlePath)
		if err != nil {
			log.Fatalf("%v", err)
		}
		opts.Puzzles = puzzles
	}

	srv, err := rushenv.NewServer(opts)
	if err != nil {
		log.Fatalf("%v", err)
	}

	if err := srv.Run(os.Stdin, os.Stdout); err != nil {
		log.Fatalf("%v", err)
	}
}
