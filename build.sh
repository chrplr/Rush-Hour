#!/usr/bin/env bash
# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the Apache License, Version 2.0.
# SPDX-License-Identifier: Apache-2.0
#
# Build the Rush-Hour executable for this machine.
#
# Usage:  bash build.sh
#
# The only prerequisite is the Go toolchain (https://go.dev/dl/). Everything
# else -- including SDL3 and every puzzle -- is downloaded by Go and embedded in
# the binary, so there is nothing else to install and no data files to ship.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: the 'go' command was not found."
  echo
  echo "Install the Go programming language first:"
  echo "  https://go.dev/dl/"
  echo "  https://chrplr.github.io/goxpyriment/Installation/"
  exit 1
fi

echo "Using $(go version)"

# The binary is self-contained: no C compiler and no system SDL3 needed.
export CGO_ENABLED=0

BIN="Rush-Hour"
if [[ "$(go env GOOS)" == "windows" ]]; then
  BIN="${BIN}.exe"
fi

echo "Building ${BIN} ..."
go build -trimpath -ldflags="-s -w" -o "${BIN}" .

echo
echo "Done: ./${BIN}"
echo "Run it with, e.g.:"
echo "    ./${BIN}                 # first 12 puzzles, fullscreen"
echo "    ./${BIN} -w -s 1         # windowed mode, subject 1 (for testing)"
echo "    ./${BIN} -n 0            # the whole 49-puzzle library"
