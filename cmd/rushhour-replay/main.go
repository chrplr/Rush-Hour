// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

// Command rushhour-replay re-plays a results file.
//
// A results file records one row per click, naming the vehicle and where it
// went, against a puzzle identified by name. Rush Hour is deterministic, so
// those rows plus the puzzle library are enough to reconstruct every position
// the board passed through — no state is stored in the file, and none needs to
// be.
//
//	rushhour-replay session.csv          # check it replays, print a summary
//	rushhour-replay -v session.csv       # print every move
//	rushhour-replay -board session.csv   # print the final position
//	rushhour-replay -render session.csv  # watch it (needs -tags rushui)
//
// Every click_move row carries the vehicle's position before and after the
// move, so replaying checks the file rather than trusting it: a row that the
// rules cannot produce, or that lands somewhere other than where it says, is
// reported with its line number. The exit status is non-zero if anything did
// not line up, which is what makes this usable as a check on collected data.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"rush-hour/internal/rush"
	"rush-hour/internal/rushreplay"
)

// version is set at build time via -ldflags="-X main.version=...".
var version = "dev"

func main() {
	var (
		puzzlePath  = flag.String("puzzles", "", "path to the puzzle library the session was recorded against (default: the embedded one)")
		trialOnly   = flag.Int("trial", 0, "replay only this trial (0 means all)")
		verbose     = flag.Bool("v", false, "print every move")
		showBoard   = flag.Bool("board", false, "print the final position of each trial")
		showVersion = flag.Bool("version", false, "print the version and exit")
	)
	registerViewerFlags()
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Printf("rushhour-replay %s\n", version)
		return
	}

	files := flag.Args()
	if len(files) == 0 {
		usage()
		os.Exit(2)
	}

	lib, err := library(*puzzlePath)
	if err != nil {
		fail(err)
	}

	if viewerEnabled() {
		if len(files) != 1 {
			fail(fmt.Errorf("-render takes one file at a time, got %d", len(files)))
		}
		session, err := rushreplay.ReadFile(files[0])
		if err != nil {
			fail(err)
		}
		if err := watch(session, lib, *trialOnly); err != nil {
			fail(err)
		}
		return
	}

	problems := 0
	for _, path := range files {
		n, err := check(path, lib, *trialOnly, *verbose, *showBoard)
		if err != nil {
			fail(err)
		}
		problems += n
	}
	if problems > 0 {
		fmt.Fprintf(os.Stderr, "\n%d problem(s): the file and the rules do not describe the same session\n", problems)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: rushhour-replay [flags] FILE...\n\n"+
		"Re-play a results file and check it against the rules.\n\n")
	flag.PrintDefaults()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// library loads the puzzle set the session was played against. A results file
// names its puzzles but does not carry them, so replaying one recorded with
// -puzzles needs the same file passed here.
func library(path string) (rushreplay.Library, error) {
	if path == "" {
		return rushreplay.DefaultLibrary()
	}
	puzzles, err := rush.LoadPuzzleFile(path)
	if err != nil {
		return nil, err
	}
	return rushreplay.NewLibrary(puzzles), nil
}

// check replays one file and prints a line per trial. It returns how many
// problems were found.
func check(path string, lib rushreplay.Library, only int, verbose, showBoard bool) (int, error) {
	session, err := rushreplay.ReadFile(path)
	if err != nil {
		return 0, err
	}

	subject := "unknown subject"
	if session.SubjectID >= 0 {
		subject = fmt.Sprintf("subject %d", session.SubjectID)
	}
	fmt.Printf("%s: %s, %d trial(s), %d clicks\n", path, subject, len(session.Trials), session.Clicks())

	problems := 0
	for _, t := range session.Trials {
		if only != 0 && t.Trial != only {
			continue
		}

		r, err := rushreplay.Replay(t, lib)
		if err != nil {
			return 0, err
		}

		fmt.Printf("  trial %-3d %-6s %s\n", t.Trial, t.Puzzle, summary(t, r))

		if verbose {
			for _, s := range r.Steps {
				fmt.Printf("      %6dms  %s %s -> (%d,%d)   slide %d\n",
					s.Event.TMS, s.Event.Car, s.Event.Orient, s.Car.Row, s.Car.Col, s.Slides)
			}
		}
		if showBoard {
			fmt.Println(indent(r.Final.String(), "      "))
		}
		for _, p := range r.Problems {
			fmt.Fprintf(os.Stderr, "      %s\n", p)
			problems++
		}
	}
	return problems, nil
}

// summary is the one-line verdict for a trial.
func summary(t *rushreplay.Trial, r *rushreplay.Replayed) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%2d moves in %2d clicks", r.Slides, len(t.Events))
	if t.MinMoves > 0 {
		fmt.Fprintf(&b, " (optimum %d)", t.MinMoves)
	}

	switch {
	case r.Solved && t.TrialMS > 0:
		fmt.Fprintf(&b, ", solved in %.1fs", float64(t.TrialMS)/1000)
	case r.Solved:
		b.WriteString(", solved")
	case !t.Ended:
		// The file stops mid-trial: the session was quit or the process killed.
		b.WriteString(", unfinished")
	default:
		b.WriteString(", not solved")
	}

	if r.OK() {
		b.WriteString("  OK")
	} else {
		fmt.Fprintf(&b, "  %d PROBLEM(S)", len(r.Problems))
	}
	return b.String()
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
