@echo off
REM Copyright (2026) Christophe Pallier <christophe@pallier.org>
REM Distributed under the MIT License.
REM
REM Build the Rush-Hour executable on Windows.
REM Double-click this file, or run it from a command prompt:  build.bat
REM
REM The only prerequisite is the Go toolchain (https://go.dev/dl/).

setlocal
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
    echo ERROR: the 'go' command was not found.
    echo.
    echo Install the Go programming language first:
    echo     https://go.dev/dl/
    echo     https://chrplr.github.io/goxpyriment/Installation/
    echo.
    pause
    exit /b 1
)

go version

REM The binary is self-contained: no C compiler and no system SDL3 needed.
set CGO_ENABLED=0

echo Building Rush-Hour.exe ...
go build -trimpath -ldflags="-s -w" -o Rush-Hour.exe .
if errorlevel 1 (
    echo.
    echo Build FAILED.
    pause
    exit /b 1
)

echo.
echo Done: Rush-Hour.exe
echo Run it with, e.g.:
echo     Rush-Hour.exe                 (first 12 puzzles, fullscreen)
echo     Rush-Hour.exe -w -s 1         (windowed mode, subject 1, for testing)
echo     Rush-Hour.exe -n 0            (the whole 49-puzzle library)
pause
