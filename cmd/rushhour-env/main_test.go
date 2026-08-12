// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The protocol tests in internal/rushenv drive the server through buffers. This
// one drives the real binary through real pipes, which is the only place two
// things can be checked: that the response is flushed before the next request
// arrives (otherwise a client blocked on readline deadlocks), and that closing
// stdin makes the process exit on its own — the guarantee that no orphan
// survives a Python interpreter dying without calling close.
func TestBinaryAnswersOverPipesAndExitsOnEOF(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}

	bin := filepath.Join(t.TempDir(), "rushhour-env")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	cmd := exec.Command(bin, "-board")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	reader := bufio.NewReader(stdout)
	ask := func(req string) map[string]any {
		t.Helper()
		if _, err := io.WriteString(stdin, req+"\n"); err != nil {
			t.Fatalf("writing %s: %v", req, err)
		}
		// No timeout wrapper: a missing flush shows up as the test binary's own
		// panic on timeout, with this goroutine parked in ReadString.
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the answer to %s: %v", req, err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("answer to %s is not JSON: %v\n%s", req, err, line)
		}
		if ok, _ := m["ok"].(bool); !ok {
			t.Fatalf("%s failed: %v", req, m)
		}
		return m
	}

	// Strict alternation, one request at a time — the client's exact pattern.
	ask(`{"id":1,"cmd":"hello"}`)
	ask(`{"id":2,"cmd":"reset","puzzle":"p02"}`)
	sol := ask(`{"id":3,"cmd":"solve"}`)

	var last map[string]any
	for _, raw := range sol["actions"].([]any) {
		last = ask(fmt.Sprintf(`{"id":4,"cmd":"step","action":%d}`, int(raw.(float64))))
	}
	if solved, _ := last["solved"].(bool); !solved {
		t.Errorf("the optimal solution did not solve p02 over the wire: %v", last)
	}

	// Closing stdin, with no close command, must be enough to end the process.
	if err := stdin.Close(); err != nil {
		t.Fatalf("closing stdin: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("exit after EOF: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("the process outlived its stdin — a client crash would orphan it")
	}
}
