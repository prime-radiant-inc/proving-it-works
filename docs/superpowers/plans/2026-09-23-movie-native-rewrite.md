# `movie` Native Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the skill's Python scripts with one Go binary, `movie`, whose three commands (`build`, `check`, `term`) let any agent make and verify a movie proving software works.

**Architecture:** A walking skeleton first: every command, the launcher, the reproducible build script, and CI exist end to end in their thinnest working form (Tasks 1-6). Then each part is fleshed out to the spec, and each flesh-out task that replaces a Python tool deletes that tool and rewrites the docs that mention it in the same change (Tasks 7-20). Decisions are pure functions over plain data, tested directly; everything touching ffmpeg or tmux is tested black-box against the built binary with media generated at test time.

**Tech Stack:** Go 1.26.1; `gopkg.in/yaml.v3`; `golang.org/x/image` (font rendering); `golang.org/x/text` (NFKC, case folding, character width); ffmpeg/ffprobe; tmux; Piper (optional, keyless voice).

**Spec:** `docs/superpowers/specs/2026-09-22-movie-native-rewrite-design.md`. Read it before starting any task.

## Global Constraints

- Module path: `github.com/prime-radiant-inc/proving-it-works`; `go.mod` line `go 1.26.1` (full patch version; the build script reads it).
- Exit codes, every command: 0 success; 1 negative verdict; 2 usage or environment error; 3 `term` only, still running. Use the `exitcode` package constants, never literals.
- Every ffmpeg call goes through `internal/ffmpeg` and gets `-nostdin -y -v error`.
- No user path is ever placed inside an ffmpeg filter, pattern, or list: user paths appear only as `-i` arguments or output arguments; everything else uses fixed names inside a scratch directory, with ffmpeg run from that directory.
- No mocks. Unit tests call pure functions; black-box tests run the real binary against real ffmpeg/tmux and skip, naming what is missing, when a tool is absent.
- Binaries under `skills/proving-it-works-with-a-movie/bin/movie-*` are NOT committed on this branch; Task 20 builds and commits them once.
- `movie term` supports macOS, Linux, and WSL only; on Windows it exits 2 with `movie term needs macOS, Linux, or WSL`.
- Commit after every task with a message saying what changed and why. Never skip hooks.
- Test output must be pristine: a test that expects an error asserts on its text.

## File Map

```
go.mod, go.sum
.gitattributes
.github/workflows/test.yml
script/build-binaries
cmd/movie/main.go                 dispatch and usage
cmd/movie/main_test.go            black-box harness: builds the binary once
cmd/movie/release_test.go         launcher and committed-binary freshness
cmd/movie/check_test.go           black-box check tests
cmd/movie/build_test.go           black-box build tests
cmd/movie/term_test.go            black-box term tests
internal/exitcode/exitcode.go
internal/cli/args.go              interleaved flag parsing, WxH sizes
internal/ffmpeg/ffmpeg.go         Run, Output, Probe, HasFilter, Require
internal/testmedia/testmedia.go   Require, FFmpeg, Duration, PNG for tests
internal/fonts/                   embedded DejaVu (+ fallbacks), coverage
internal/srt/                     cue timing, SRT writing, SRT end parsing
internal/check/                   verdict (pure), measure, sheet, run, CLI
internal/scene/scene.go           scene file parsing and validation
internal/narrate/                 gate (pure), engines, clip cache
internal/build/                   segments, cards, concat, subtitles, burn, CLI
internal/term/                    session, status, verbs, recorder, render, CLI
skills/proving-it-works-with-a-movie/bin/movie   sh launcher
```

---

# Part 1: Walking skeleton

### Task 1: Module, CLI dispatch, and the black-box harness

**Files:**
- Create: `go.mod`, `.gitattributes`, `internal/exitcode/exitcode.go`, `internal/cli/args.go`, `internal/cli/args_test.go`, `cmd/movie/main.go`, `cmd/movie/main_test.go`
- Modify: `.gitignore`

**Interfaces:**
- Produces: `exitcode.OK/Verdict/Usage/Running`; `cli.Parse(fs *flag.FlagSet, args []string) ([]string, error)`; `cli.ParseSize(s string) (int, int, error)`; test helpers `runMovie(t, dir, args...) result` and `movieBin` in package `main` of `cmd/movie`.

- [ ] **Step 1: Create the module**

```bash
go mod init github.com/prime-radiant-inc/proving-it-works
go mod edit -go=1.26.1
```

- [ ] **Step 2: Write `.gitattributes` and extend `.gitignore`**

`.gitattributes`:
```
*.go text eol=lf
go.mod text eol=lf
go.sum text eol=lf
*.ttf binary
skills/proving-it-works-with-a-movie/bin/movie text eol=lf
skills/proving-it-works-with-a-movie/bin/movie-* binary
```

Append to `.gitignore`:
```
# movie build scratch
*.build/
```

- [ ] **Step 3: Write the failing tests**

`internal/cli/args_test.go`:
```go
package cli

import (
	"flag"
	"slices"
	"testing"
)

func TestParseInterleavesFlagsAndPositionals(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	quiet := fs.Bool("quiet", false, "")
	got, err := Parse(fs, []string{"a.mp4", "--quiet", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"a.mp4", "b"}) || !*quiet {
		t.Fatalf("got %q quiet=%v", got, *quiet)
	}
}

func TestParseKeepsEverythingAfterDoubleDash(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	quiet := fs.Bool("quiet", false, "")
	got, err := Parse(fs, []string{"s", "--", "docker", "exec", "--quiet"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"s", "docker", "exec", "--quiet"}) || *quiet {
		t.Fatalf("got %q quiet=%v", got, *quiet)
	}
}

func TestParseSize(t *testing.T) {
	w, h, err := ParseSize("1920x1080")
	if err != nil || w != 1920 || h != 1080 {
		t.Fatalf("got %d %d %v", w, h, err)
	}
	for _, bad := range []string{"1920", "0x10", "ax10", "10x-1", ""} {
		if _, _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) accepted", bad)
		}
	}
}
```

`cmd/movie/main_test.go`:
```go
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// movieBin is the binary under test, built once for the whole package.
var movieBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "movie-test-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	movieBin = filepath.Join(dir, "movie")
	if runtime.GOOS == "windows" {
		movieBin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", movieBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building movie: %v\n%s", err, out)
		os.Exit(2)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code           int
	stdout, stderr string
}

// runMovie runs the built binary in dir and returns its exit code and output.
func runMovie(t *testing.T, dir string, args ...string) result {
	t.Helper()
	cmd := exec.Command(movieBin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running movie %q: %v", args, err)
	}
	return result{code, stdout.String(), stderr.String()}
}

func TestNoArgumentsPrintsUsageAndExits2(t *testing.T) {
	r := runMovie(t, t.TempDir())
	if r.code != 2 || !strings.Contains(r.stderr, "movie build") {
		t.Fatalf("code %d stderr %q", r.code, r.stderr)
	}
}

func TestUnknownCommandExits2(t *testing.T) {
	r := runMovie(t, t.TempDir(), "frobnicate")
	if r.code != 2 || !strings.Contains(r.stderr, `unknown command "frobnicate"`) {
		t.Fatalf("code %d stderr %q", r.code, r.stderr)
	}
}

func TestHelpExits0(t *testing.T) {
	r := runMovie(t, t.TempDir(), "help")
	if r.code != 0 || !strings.Contains(r.stdout, "movie check") {
		t.Fatalf("code %d stdout %q", r.code, r.stdout)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL (`undefined: Parse`, and `cmd/movie` has no `main`).

- [ ] **Step 5: Write the implementation**

`internal/exitcode/exitcode.go`:
```go
// Package exitcode names the exit codes every movie command shares.
package exitcode

const (
	OK      = 0 // success
	Verdict = 1 // negative verdict: not shippable, narration rejected, command failed
	Usage   = 2 // usage or environment error
	Running = 3 // term only: the command is still running
)
```

`internal/cli/args.go`:
```go
// Package cli holds the argument parsing every movie subcommand shares.
package cli

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// Parse parses flags that may appear before, between, or after positional
// arguments and returns the positional arguments in order. A "--" ends flag
// parsing: everything after it is positional.
func Parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if len(args) > len(rest) && args[len(args)-len(rest)-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// ParseSize reads a size written WxH, such as 1920x1080 or 120x34.
func ParseSize(s string) (int, int, error) {
	w, h, ok := strings.Cut(s, "x")
	if !ok {
		return 0, 0, fmt.Errorf("size %q is not WxH", s)
	}
	width, err1 := strconv.Atoi(w)
	height, err2 := strconv.Atoi(h)
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("size %q is not two positive whole numbers", s)
	}
	return width, height, nil
}
```

`cmd/movie/main.go`:
```go
// Command movie makes a movie that proves software works, and checks it.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

const usage = `movie: make a movie that proves software works, and check it.

  movie build SCENES.yaml OUT.mp4     narrate, assemble, subtitle, burn, check
  movie check MOVIE [--no-expect-audio] [--no-expect-subtitles]
  movie term VERB SESSION ...         film a terminal (run "movie term" for verbs)

Exit codes: 0 ok; 1 not shippable or failed; 2 usage or environment error;
3 (term) still running.
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitcode.Usage
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitcode.OK
	}
	fmt.Fprintf(stderr, "movie: unknown command %q\n\n%s", args[0], usage)
	return exitcode.Usage
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod .gitattributes .gitignore internal/exitcode internal/cli cmd/movie
git commit -m "Start the movie binary: module, dispatch, and a black-box test harness"
```

---

### Task 2: Launcher, reproducible build script, and release checks

**Files:**
- Create: `skills/proving-it-works-with-a-movie/bin/movie`, `script/build-binaries`, `cmd/movie/release_test.go`

**Interfaces:**
- Consumes: `movieBin`, `runMovie` from Task 1.
- Produces: `script/build-binaries [OUTDIR]`; `repoRoot(t) string` test helper; the launcher contract (`bin/movie` execs `movie-<os>-<arch>`).

- [ ] **Step 1: Write the failing tests**

`cmd/movie/release_test.go`:
```go
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const binDir = "skills/proving-it-works-with-a-movie/bin"

var releaseNames = []string{
	"movie-darwin-arm64", "movie-darwin-amd64", "movie-linux-amd64",
	"movie-linux-arm64", "movie-windows-amd64.exe",
}

// repoRoot is the repository root; tests run in cmd/movie.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

func hostBinaryName() string {
	if runtime.GOOS == "windows" {
		return "movie-windows-amd64.exe"
	}
	return "movie-" + runtime.GOOS + "-" + runtime.GOARCH
}

func copyFile(t *testing.T, from, to string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, mode); err != nil {
		t.Fatal(err)
	}
}

// The launcher must pick this machine's binary and pass arguments through
// untouched, including spaces, percent signs, and semicolons.
func TestLauncherPassesArgumentsThroughUntouched(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh on PATH (Git Bash on Windows)")
	}
	dir := t.TempDir()
	copyFile(t, filepath.Join(repoRoot(t), binDir, "movie"), filepath.Join(dir, "movie"), 0o755)
	copyFile(t, movieBin, filepath.Join(dir, hostBinaryName()), 0o755)
	// an unknown command is echoed back verbatim, so it shows what arrived
	out, _ := exec.Command("sh", filepath.Join(dir, "movie"), "a b%c;d").CombinedOutput()
	if !strings.Contains(string(out), `unknown command "a b%c;d"`) {
		t.Fatalf("argument did not arrive intact:\n%s", out)
	}
}

// Committed binaries must be exactly what the build script produces from
// this source. CI sets MOVIE_RELEASE_CHECK=1 on main; work branches do not
// commit binaries.
func TestCommittedBinariesMatchSource(t *testing.T) {
	if os.Getenv("MOVIE_RELEASE_CHECK") != "1" {
		t.Skip("set MOVIE_RELEASE_CHECK=1 to compare committed binaries with a fresh build")
	}
	root := repoRoot(t)
	out := t.TempDir()
	cmd := exec.Command("bash", filepath.Join(root, "script", "build-binaries"), out)
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-binaries: %v\n%s", err, b)
	}
	for _, name := range releaseNames {
		fresh, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile(filepath.Join(root, binDir, name))
		if err != nil {
			t.Errorf("%s is not committed: run script/build-binaries and commit the result", name)
			continue
		}
		if !bytes.Equal(fresh, committed) {
			t.Errorf("%s is stale: run script/build-binaries and commit the result", name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/movie -run Launcher`
Expected: FAIL (launcher file missing).

- [ ] **Step 3: Write the launcher**

`skills/proving-it-works-with-a-movie/bin/movie`:
```sh
#!/bin/sh
# Run the movie binary built for this machine; arguments pass through untouched.
here=$(cd "$(dirname "$0")" && pwd)
case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  MINGW*|MSYS*|CYGWIN*)
    # Git Bash rewrites Unix-looking arguments for native programs; stop it.
    # Pass paths in Windows form (cygpath -m) instead.
    MSYS_NO_PATHCONV=1
    MSYS2_ARG_CONV_EXCL='*'
    export MSYS_NO_PATHCONV MSYS2_ARG_CONV_EXCL
    exec "$here/movie-windows-amd64.exe" "$@" ;;
  *) echo "movie: unsupported system $(uname -s)" >&2; exit 2 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "movie: unsupported machine $(uname -m)" >&2; exit 2 ;;
esac
exec "$here/movie-$os-$arch" "$@"
```

```bash
chmod +x skills/proving-it-works-with-a-movie/bin/movie
```

- [ ] **Step 4: Write the build script**

`script/build-binaries`:
```bash
#!/usr/bin/env bash
# Build the five release binaries of movie, reproducibly.
#
# When: before merging to main (commit the result), and from the freshness
# test, which builds into a temporary OUTDIR and compares bytes.
#
# Usage: script/build-binaries [OUTDIR]
#   OUTDIR defaults to skills/proving-it-works-with-a-movie/bin.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$root/skills/proving-it-works-with-a-movie/bin}

# The toolchain line wins; go mod tidy drops it when it equals the go line,
# so fall back to the go line, which must carry a full patch version.
version=$(awk '$1 == "toolchain" { sub(/^go/, "", $2); print $2; exit }' "$root/go.mod")
if [[ -z $version ]]; then
  version=$(awk '$1 == "go" { print $2; exit }' "$root/go.mod")
fi
if [[ ! $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "build-binaries: go.mod must name a full Go version such as 'go 1.26.1'; found '$version'" >&2
  exit 2
fi

mkdir -p "$out"
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  goos=${target%/*}
  goarch=${target#*/}
  name=movie-$goos-$goarch
  [[ $goos == windows ]] && name+=.exe
  (cd "$root" && GOTOOLCHAIN=go$version GOOS=$goos GOARCH=$goarch GOAMD64=v1 GOARM64=v8.0 \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$out/$name" ./cmd/movie)
  echo "built $name"
done
```

```bash
chmod +x script/build-binaries
```

- [ ] **Step 5: Run the tests and the script**

Run: `go test ./cmd/movie -run 'Launcher|Committed' -v`
Expected: the launcher test passes, and the freshness test SKIPs with the `MOVIE_RELEASE_CHECK` message.

Run: `script/build-binaries "$(mktemp -d)"`
Expected: five `built movie-...` lines.

- [ ] **Step 6: Commit**

```bash
git add skills/proving-it-works-with-a-movie/bin/movie script/build-binaries cmd/movie/release_test.go
git commit -m "Add the movie launcher and a reproducible release build with a freshness check"
```

---

### Task 3: CI

**Files:**
- Create: `.github/workflows/test.yml`

- [ ] **Step 1: Write the workflow**

```yaml
name: test
on:
  push:
  pull_request:
jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Install ffmpeg and tmux (Linux)
        if: runner.os == 'Linux'
        run: sudo apt-get update && sudo apt-get install -y ffmpeg tmux
      - name: Install ffmpeg and tmux (macOS)
        if: runner.os == 'macOS'
        run: brew install ffmpeg tmux
      - name: Install ffmpeg (Windows)
        if: runner.os == 'Windows'
        run: choco install ffmpeg -y
      - name: Test
        shell: bash
        run: go test ./...
        env:
          MOVIE_RELEASE_CHECK: ${{ github.ref == 'refs/heads/main' && '1' || '' }}
```

- [ ] **Step 2: Verify locally that the suite the workflow runs passes**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/test.yml
git commit -m "Run the Go suite in CI on macOS, Linux, and Windows"
```

---

### Task 4: Skeleton `movie check`

The thinnest real gate: probe the movie, fail a non-movie, write `check.json`, print the verdict. Task 7 ports the rest.

**Files:**
- Create: `internal/ffmpeg/ffmpeg.go`, `internal/testmedia/testmedia.go`, `internal/check/verdict.go`, `internal/check/run.go`, `internal/check/main.go`, `cmd/movie/check_test.go`
- Modify: `cmd/movie/main.go`

**Interfaces:**
- Produces:
  - `ffmpeg.Require() error`, `ffmpeg.Run(dir string, stdin io.Reader, args ...string) error`, `ffmpeg.Output(args ...string) ([]byte, error)`, `ffmpeg.Probe(path string) (ffmpeg.Info, error)`, `ffmpeg.HasFilter(name string) bool`; `Info{Duration float64; Streams []Stream}` with `Has(kind string) bool` and `First(kind string) (Stream, bool)`; `Stream{Index int; CodecType, CodecName string; Width, Height int}`.
  - `check.Options{ExpectAudio, ExpectSubtitles bool; SpeechEnd *float64}`, `check.Run(movie string, o Options, stdout io.Writer) (int, error)`, `check.Main(args []string, stdout, stderr io.Writer) int`, `check.WorkDir(movie string) string`.
  - `testmedia.Require(t, tools...)`, `testmedia.FFmpeg(t, dir, args...)`, `testmedia.Duration(t, path) float64`.

- [ ] **Step 1: Write the failing tests**

`internal/testmedia/testmedia.go` (a helper package, needed by the test):
```go
// Package testmedia makes real media for tests with ffmpeg's built-in lavfi
// sources, so no media is committed and nothing is mocked.
package testmedia

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// Require skips t unless every named executable is on PATH.
func Require(t testing.TB, tools ...string) {
	t.Helper()
	var missing []string
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		t.Skipf("needs %s on PATH", strings.Join(missing, ", "))
	}
}

// FFmpeg runs ffmpeg in dir and fails t on error.
func FFmpeg(t testing.TB, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", append([]string{"-nostdin", "-y", "-v", "error"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %q: %v\n%s", args, err, out)
	}
}

// Duration returns a media file's duration in seconds.
func Duration(t testing.TB, path string) float64 {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration",
		"-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	return d
}
```

`cmd/movie/check_test.go`:
```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prime-radiant-inc/proving-it-works/internal/testmedia"
)

func TestCheckRejectsAMovieShorterThanASecond(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=0.5",
		"-pix_fmt", "yuv420p", "short.mp4")
	r := runMovie(t, dir, "check", "short.mp4", "--no-expect-audio")
	if r.code != 1 || !strings.Contains(r.stdout, "not a movie") || !strings.Contains(r.stdout, "NOT SHIPPABLE") {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	var report struct{ Failures []string }
	data, err := os.ReadFile(filepath.Join(dir, "short-check", "check.json"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(data, &report); len(report.Failures) != 1 {
		t.Fatalf("check.json failures: %q", report.Failures)
	}
}

func TestCheckMissingMovieExits2(t *testing.T) {
	r := runMovie(t, t.TempDir(), "check", "nope.mp4")
	if r.code != 2 || !strings.Contains(r.stderr, "no such movie") {
		t.Fatalf("code %d stderr %q", r.code, r.stderr)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/movie -run Check`
Expected: FAIL (`unknown command "check"`).

- [ ] **Step 3: Write `internal/ffmpeg/ffmpeg.go`**

```go
// Package ffmpeg runs ffmpeg and ffprobe with the options every call needs.
package ffmpeg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// base is prepended to every ffmpeg call: never read stdin as a terminal
// (it eats loop input), always overwrite, and print only errors.
var base = []string{"-nostdin", "-y", "-v", "error"}

// Require reports ffmpeg or ffprobe missing from PATH.
func Require() error {
	var missing []string
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s not on PATH: install ffmpeg", strings.Join(missing, " and "))
	}
	return nil
}

// Run runs ffmpeg in dir, feeding stdin when it is not nil.
func Run(dir string, stdin io.Reader, args ...string) error {
	full := append(append([]string{}, base...), args...)
	cmd := exec.Command("ffmpeg", full...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg %s: %v\n%s", strings.Join(full, " "), err, tail(stderr.String()))
	}
	return nil
}

// Output runs ffmpeg and returns what it writes to stdout.
func Output(args ...string) ([]byte, error) {
	full := append(append([]string{}, base...), args...)
	cmd := exec.Command("ffmpeg", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg %s: %v\n%s", strings.Join(full, " "), err, tail(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Stream is one stream ffprobe reports.
type Stream struct {
	Index     int    `json:"index"`
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// Info is a media file's duration and streams.
type Info struct {
	Duration float64
	Streams  []Stream
}

// Has reports whether the file has a stream of kind (video, audio, subtitle).
func (i Info) Has(kind string) bool {
	_, ok := i.First(kind)
	return ok
}

// First returns the first stream of kind.
func (i Info) First(kind string) (Stream, bool) {
	for _, s := range i.Streams {
		if s.CodecType == kind {
			return s, true
		}
	}
	return Stream{}, false
}

// Probe reads a media file's duration and streams.
func Probe(path string) (Info, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: %v\n%s", path, err, tail(stderr.String()))
	}
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []Stream `json:"streams"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	d, err := strconv.ParseFloat(raw.Format.Duration, 64)
	if err != nil && raw.Format.Duration != "" {
		return Info{}, errors.New("ffprobe reported an unreadable duration for " + path)
	}
	return Info{Duration: d, Streams: raw.Streams}, nil
}

// HasFilter reports whether this ffmpeg build has the named filter.
func HasFilter(name string) bool {
	out, err := exec.Command("ffmpeg", "-hide_banner", "-filters").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[1] == name {
			return true
		}
	}
	return false
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 800 {
		return "..." + s[len(s)-800:]
	}
	return s
}
```

- [ ] **Step 4: Write the skeleton check**

`internal/check/verdict.go`:
```go
// Package check is the mechanical gate for a proof movie: it samples picture
// and sound on one timeline and fails the defects per-frame inspection
// cannot see.
package check

import "fmt"

// Options says what the movie is expected to contain.
type Options struct {
	ExpectAudio     bool
	ExpectSubtitles bool
	// SpeechEnd, when set, is where subtitles must reach instead of the last
	// second of detected speech. build sets it to the end of the last
	// narration clip, so speech inside a movie scene does not count.
	SpeechEnd *float64
}

// Measurements is what was measured from the movie.
type Measurements struct {
	Duration float64
	HasAudio bool
}

// Report is the verdict, as written to check.json.
type Report struct {
	Duration float64  `json:"duration"`
	Failures []string `json:"failures"`
	Warnings []string `json:"warnings"`
}

// Evaluate turns measurements into failures and warnings.
func Evaluate(m Measurements, o Options) Report {
	r := Report{Duration: m.Duration, Failures: []string{}, Warnings: []string{}}
	if m.Duration < 1 {
		r.Failures = append(r.Failures, fmt.Sprintf("duration is %.2fs - that is not a movie", m.Duration))
	}
	return r
}
```

`internal/check/run.go`:
```go
package check

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
)

// WorkDir is where check writes its evidence: <movie stem>-check beside it.
func WorkDir(movie string) string {
	return strings.TrimSuffix(movie, filepath.Ext(movie)) + "-check"
}

// Run checks movie, prints the verdict, and writes check.json. It returns
// exitcode.OK or exitcode.Verdict; an error means the movie could not be
// examined at all.
func Run(movie string, o Options, stdout io.Writer) (int, error) {
	movie, err := filepath.Abs(movie)
	if err != nil {
		return 0, err
	}
	if _, err := os.Stat(movie); err != nil {
		return 0, fmt.Errorf("no such movie: %s", movie)
	}
	if err := ffmpeg.Require(); err != nil {
		return 0, err
	}
	info, err := ffmpeg.Probe(movie)
	if err != nil {
		return 0, err
	}
	video, ok := info.First("video")
	if !ok {
		return 0, errors.New("no video stream")
	}
	workdir := WorkDir(movie)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return 0, err
	}
	m := Measurements{Duration: info.Duration, HasAudio: info.Has("audio")}
	r := Evaluate(m, o)
	fmt.Fprintf(stdout, "container  %s %dx%d, %.1fs, audio=%s\n",
		video.CodecName, video.Width, video.Height, info.Duration, yesNo(m.HasAudio))
	return finish(r, workdir, stdout)
}

// finish prints warnings and failures, writes check.json, and picks the exit code.
func finish(r Report, workdir string, stdout io.Writer) (int, error) {
	for _, w := range r.Warnings {
		fmt.Fprintf(stdout, "WARN       %s\n", w)
	}
	for _, f := range r.Failures {
		fmt.Fprintf(stdout, "FAIL       %s\n", f)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(workdir, "check.json"), data, 0o644); err != nil {
		return 0, err
	}
	if len(r.Failures) > 0 {
		fmt.Fprintln(stdout, "\nNOT SHIPPABLE. Fix, regenerate, re-run.")
		return exitcode.Verdict, nil
	}
	fmt.Fprintln(stdout, "\nMechanical checks pass. NOW OPEN THE CONTACT SHEET AND LOOK AT IT: "+
		"this cannot see wrong content, unreadable text, a missing cursor, or "+
		"narration that says something the picture contradicts.")
	return exitcode.OK, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
```

`internal/check/main.go`:
```go
package check

import (
	"flag"
	"fmt"
	"io"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

// Main is `movie check`.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("movie check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noAudio := fs.Bool("no-expect-audio", false, "the movie is meant to be silent")
	noSubs := fs.Bool("no-expect-subtitles", false, "speech without subtitles is acceptable")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: movie check MOVIE [--no-expect-audio] [--no-expect-subtitles]")
	}
	pos, err := cli.Parse(fs, args)
	if err != nil {
		return exitcode.Usage
	}
	if len(pos) != 1 {
		fs.Usage()
		return exitcode.Usage
	}
	code, err := Run(pos[0], Options{ExpectAudio: !*noAudio, ExpectSubtitles: !*noSubs}, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "movie check: %v\n", err)
		return exitcode.Usage
	}
	return code
}
```

In `cmd/movie/main.go`, import `internal/check` and add to the switch in `run`:
```go
	case "check":
		return check.Main(args[1:], stdout, stderr)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ffmpeg internal/testmedia internal/check cmd/movie
git commit -m "Skeleton movie check: probe the movie, fail a non-movie, write check.json"
```

---

### Task 5: Skeleton `movie build`

The thinnest real build: image scenes only, silent, assembled from piped PNGs and concatenated from fixed names, then checked.

**Files:**
- Create: `internal/scene/scene.go`, `internal/build/segment.go`, `internal/build/build.go`, `internal/build/main.go`, `cmd/movie/build_test.go`
- Modify: `cmd/movie/main.go`

**Interfaces:**
- Consumes: `ffmpeg.Run/Probe/Require`, `check.Run`, `cli.ParseSize`.
- Produces (skeleton shapes that Tasks 8-13 extend without renaming):
  - `scene.Load(path string) (*scene.File, error)`; `File{Path, Dir string; Width, Height, FPS int; Scenes []Scene}`; `Scene{ID string; Kind Kind; Source string; Duration float64}`; `Kind` constant `Image`.
  - `build.Run(scenePath, out string, stdout io.Writer) (int, error)`, `build.Main(args, stdout, stderr) int`.
  - In `internal/build`: `encodeStill(scratch, name string, f *scene.File, pngs io.Reader, rate float64, count int, target float64, wav string) error`, `streamFiles(paths []string) io.ReadCloser`, `concat(scratch string, names []string, out string) error`, `fit(w, h int) string`, `encodeArgs`.

- [ ] **Step 1: Write the failing test**

`cmd/movie/build_test.go`:
```go
package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prime-radiant-inc/proving-it-works/internal/testmedia"
)

// writeFile writes content to dir/name, creating parent directories.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// still writes a solid-colour PNG at dir/name. ffmpeg writes it under a safe
// name first, because its image muxer would read "%03d" in name as a pattern.
func still(t *testing.T, dir, name, colour string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "color=c="+colour+":size=320x180:d=0.1",
		"-frames:v", "1", "still-tmp.png")
	if err := os.Rename(filepath.Join(dir, "still-tmp.png"), filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

func assertNear(t *testing.T, what string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s: got %.3f, want %.3f ± %.3f", what, got, want, tolerance)
	}
}

func TestBuildAssemblesImageScenesInOrderAndChecksTheResult(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	still(t, dir, "red.png", "red")
	still(t, dir, "blue.png", "blue")
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
scenes:
  - id: first
    image: red.png
    duration: 2
  - id: second
    image: blue.png
    duration: 3
`)
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 0 {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	assertNear(t, "movie duration", testmedia.Duration(t, filepath.Join(dir, "demo.mp4")), 5, 0.2)
	if !strings.Contains(r.stdout, "Mechanical checks pass") {
		t.Fatalf("build did not run the check:\n%s", r.stdout)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/movie -run Build`
Expected: FAIL (`unknown command "build"`).

- [ ] **Step 3: Write the skeleton scene loader**

```bash
go get gopkg.in/yaml.v3
```

`internal/scene/scene.go`:
```go
// Package scene reads and validates a movie's scene file.
package scene

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
)

// Kind is what a scene shows.
type Kind string

const Image Kind = "image"

// File is a parsed scene file.
type File struct {
	Path               string // the scene file
	Dir                string // paths in the file are relative to this
	Width, Height, FPS int
	Scenes             []Scene
}

// Scene is one segment of the movie.
type Scene struct {
	ID       string
	Kind     Kind
	Source   string  // absolute path of the image
	Duration float64 // minimum hold, seconds
}

// Load reads a scene file.
func Load(path string) (*File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Size   string `yaml:"size"`
		FPS    int    `yaml:"fps"`
		Scenes []struct {
			ID       string  `yaml:"id"`
			Image    string  `yaml:"image"`
			Duration float64 `yaml:"duration"`
		} `yaml:"scenes"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f := &File{Path: abs, Dir: filepath.Dir(abs), Width: 1920, Height: 1080, FPS: 30}
	if raw.Size != "" {
		if f.Width, f.Height, err = cli.ParseSize(raw.Size); err != nil {
			return nil, err
		}
	}
	if raw.FPS > 0 {
		f.FPS = raw.FPS
	}
	for _, s := range raw.Scenes {
		d := s.Duration
		if d <= 0 {
			d = 3
		}
		f.Scenes = append(f.Scenes, Scene{ID: s.ID, Kind: Image, Source: filepath.Join(f.Dir, s.Image), Duration: d})
	}
	return f, nil
}
```

- [ ] **Step 4: Write the segment encoder, concat, and build**

`internal/build/segment.go`:
```go
package build

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/scene"
)

const background = "#101014"

// encodeArgs make every segment identical in codec parameters, which is what
// lets the final concat copy streams instead of re-encoding.
var encodeArgs = []string{"-c:v", "libx264", "-preset", "medium", "-pix_fmt", "yuv420p",
	"-c:a", "aac", "-ar", "44100", "-ac", "2"}

// fit scales a picture into w x h without distortion and pads the rest.
func fit(w, h int) string {
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,"+
		"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=%s,setsar=1", w, h, w, h, background)
}

// audioInput is the narration clip, or silence.
func audioInput(wav string) []string {
	if wav == "" {
		return []string{"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo"}
	}
	return []string{"-i", wav}
}

// encodeStill writes scratch/name from count PNGs streamed on pngs at rate
// frames per second. The last frame is held and the audio padded so the
// segment lasts target seconds. wav is the narration clip, or "" for silence.
// Piping the frames means no user path is ever parsed as an ffmpeg pattern.
func encodeStill(scratch, name string, f *scene.File, pngs io.Reader, rate float64, count int, target float64, wav string) error {
	hold := math.Max(0, target-float64(count)/rate)
	args := []string{"-f", "image2pipe", "-c:v", "png", "-framerate", strconv.FormatFloat(rate, 'f', -1, 64), "-i", "-"}
	args = append(args, audioInput(wav)...)
	args = append(args,
		"-vf", fit(f.Width, f.Height)+fmt.Sprintf(",tpad=stop_mode=clone:stop_duration=%.3f", hold),
		"-af", "apad", "-r", strconv.Itoa(f.FPS), "-t", fmt.Sprintf("%.3f", target),
		"-map", "0:v:0", "-map", "1:a:0")
	args = append(append(args, encodeArgs...), name)
	return ffmpeg.Run(scratch, pngs, args...)
}

// streamFiles concatenates files onto one reader, opening one at a time.
// Close it after use so the copying goroutine ends even if ffmpeg quit early.
func streamFiles(paths []string) io.ReadCloser {
	r, w := io.Pipe()
	go func() {
		for _, p := range paths {
			file, err := os.Open(p)
			if err != nil {
				w.CloseWithError(err)
				return
			}
			_, err = io.Copy(w, file)
			file.Close()
			if err != nil {
				w.CloseWithError(err)
				return
			}
		}
		w.Close()
	}()
	return r
}
```

`internal/build/build.go`:
```go
// Package build turns a scene file into a finished, checked movie.
package build

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/prime-radiant-inc/proving-it-works/internal/check"
	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/scene"
)

// Run builds out from the scene file and checks it.
func Run(scenePath, out string, stdout io.Writer) (int, error) {
	if err := ffmpeg.Require(); err != nil {
		return 0, err
	}
	f, err := scene.Load(scenePath)
	if err != nil {
		return 0, err
	}
	if out, err = filepath.Abs(out); err != nil {
		return 0, err
	}
	scratch := strings.TrimSuffix(out, filepath.Ext(out)) + ".build"
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return 0, err
	}
	var names []string
	for _, sc := range f.Scenes {
		name := sc.ID + ".mp4"
		pngs := streamFiles([]string{sc.Source})
		err := encodeStill(scratch, name, f, pngs, float64(f.FPS), 1, sc.Duration, "")
		pngs.Close()
		if err != nil {
			return 0, fmt.Errorf("scene %s: %w", sc.ID, err)
		}
		info, err := ffmpeg.Probe(filepath.Join(scratch, name))
		if err != nil {
			return 0, err
		}
		fmt.Fprintf(stdout, "%s: %.1fs\n", sc.ID, info.Duration)
		names = append(names, name)
	}
	if err := concat(scratch, names, out); err != nil {
		return 0, err
	}
	fmt.Fprintf(stdout, "\nassembled %s\n\n", out)
	return check.Run(out, check.Options{}, stdout)
}

// concat joins segments by their fixed safe names, running from scratch so
// nothing in the list needs escaping.
func concat(scratch string, names []string, out string) error {
	var list strings.Builder
	for _, n := range names {
		fmt.Fprintf(&list, "file '%s'\n", n)
	}
	if err := os.WriteFile(filepath.Join(scratch, "concat.txt"), []byte(list.String()), 0o644); err != nil {
		return err
	}
	return ffmpeg.Run(scratch, nil, "-f", "concat", "-safe", "0", "-i", "concat.txt", "-c", "copy", out)
}
```

`internal/build/main.go`:
```go
package build

import (
	"flag"
	"fmt"
	"io"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

// Main is `movie build`.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("movie build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, "usage: movie build SCENES.yaml OUT.mp4") }
	pos, err := cli.Parse(fs, args)
	if err != nil {
		return exitcode.Usage
	}
	if len(pos) != 2 {
		fs.Usage()
		return exitcode.Usage
	}
	code, err := Run(pos[0], pos[1], stdout)
	if err != nil {
		fmt.Fprintf(stderr, "movie build: %v\n", err)
		if code == exitcode.OK {
			code = exitcode.Usage
		}
	}
	return code
}
```

In `cmd/movie/main.go`, import `internal/build` and add:
```go
	case "build":
		return build.Main(args[1:], stdout, stderr)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/scene internal/build cmd/movie
git commit -m "Skeleton movie build: image scenes piped to ffmpeg, concatenated, and checked"
```

---

### Task 6: Skeleton `movie term` and embedded fonts

The thinnest real terminal route: `start` a clean bash in a private tmux server and begin recording, `run` a command, `stop` and render plain frames.

**Files:**
- Create: `internal/fonts/fonts.go`, `internal/fonts/fonts_test.go`, `internal/fonts/DejaVuSans.ttf`, `internal/fonts/DejaVuSansMono.ttf`, `internal/fonts/LICENSE-DejaVu`, `internal/term/session.go`, `internal/term/status.go`, `internal/term/status_test.go`, `internal/term/verbs.go`, `internal/term/record.go`, `internal/term/spawn_unix.go`, `internal/term/spawn_windows.go`, `internal/term/render.go`, `internal/term/render_test.go`, `internal/term/main.go`, `cmd/movie/term_test.go`
- Modify: `cmd/movie/main.go`

**Interfaces:**
- Produces:
  - `fonts.Sans() *opentype.Font`, `fonts.Mono() *opentype.Font`, `fonts.SansTTF() []byte`, `fonts.Face(f *opentype.Font, size float64) font.Face`, `fonts.Missing(chain []*opentype.Font, text string) []rune`, `fonts.Find(chain []*opentype.Font, r rune, buf *sfnt.Buffer) int`, `fonts.Describe(runes []rune) string`.
  - `term.Session{Dir, Socket, History string; Wrapper []string}`, `term.Load(dir string) (*Session, error)`, `(*Session).tmux(args ...string) (string, error)`, `(*Session).status(styled bool) (Status, error)`.
  - `term.Status{CursorX, CursorY, Cols, Rows int; CursorVisible, Film bool; Sent, Seq, Code int; Command, Screen string}` with `AtPrompt() bool`; `parseStatus(string) (Status, error)`; `parseTitle(string) (seq, code int, ok bool)`.
  - `term.Entry{T float64; End, Film bool; Cols, Rows, CursorX, CursorY int; Cursor bool; Screen string}`; `term.Take{Start, End float64; Entries []Entry}`; `Takes([]Entry) []Take`; `Slots(Take, fps float64) []int`; `const FPS = 10`.
  - `term.Start`, `term.RunCommand`, `term.Stop`, `term.Render`, `term.Record`, `term.Main`.

- [ ] **Step 1: Add the fonts**

```bash
tmp=$(mktemp -d)
curl -fsSL -o "$tmp/dejavu.zip" https://github.com/dejavu-fonts/dejavu-fonts/releases/download/version_2_37/dejavu-fonts-ttf-2.37.zip
echo "7576310b219e04159d35ff61dd4a4ec4cdba4f35c00e002a136f00e96a908b0a  $tmp/dejavu.zip" | shasum -a 256 -c
unzip -q "$tmp/dejavu.zip" -d "$tmp"
mkdir -p internal/fonts
cp "$tmp/dejavu-fonts-ttf-2.37/ttf/DejaVuSans.ttf" "$tmp/dejavu-fonts-ttf-2.37/ttf/DejaVuSansMono.ttf" internal/fonts/
cp "$tmp/dejavu-fonts-ttf-2.37/LICENSE" internal/fonts/LICENSE-DejaVu
go get golang.org/x/image
```

Expected: `dejavu.zip: OK`.

- [ ] **Step 2: Write the failing tests**

`internal/fonts/fonts_test.go`:
```go
package fonts

import (
	"slices"
	"testing"

	"golang.org/x/image/font/opentype"
)

func TestSansDrawsLatinButNotCJK(t *testing.T) {
	missing := Missing([]*opentype.Font{Sans()}, "Proving it works — λ 漢")
	if !slices.Equal(missing, []rune{'漢'}) {
		t.Fatalf("missing %q", missing)
	}
	if got := Describe(missing); got != "漢 (U+6F22)" {
		t.Fatalf("Describe: %q", got)
	}
}
```

`internal/term/status_test.go`:
```go
package term

import "testing"

func TestParseTitle(t *testing.T) {
	for _, c := range []struct {
		title     string
		seq, code int
		ok        bool
	}{
		{"MOVIE;3;0", 3, 0, true},
		{"MOVIE;12;127", 12, 127, true},
		{"bash", 0, 0, false},
		{"MOVIE;x;0", 0, 0, false},
	} {
		seq, code, ok := parseTitle(c.title)
		if seq != c.seq || code != c.code || ok != c.ok {
			t.Errorf("parseTitle(%q) = %d %d %v", c.title, seq, code, ok)
		}
	}
}

func TestParseStatusSplitsStatusLineFromScreen(t *testing.T) {
	st, err := parseStatus("2;1;120;34;1;on;4;bash;MOVIE;5;1\n$ false\n$\n")
	if err != nil {
		t.Fatal(err)
	}
	want := Status{CursorX: 2, CursorY: 1, Cols: 120, Rows: 34, CursorVisible: true, Film: true,
		Sent: 4, Seq: 5, Code: 1, Command: "bash", Screen: "$ false\n$\n"}
	if st != want {
		t.Fatalf("got %+v", st)
	}
	if !st.AtPrompt() {
		t.Fatal("a new prompt since the last input should be AtPrompt")
	}
}

func TestNotAtPromptUntilANewPromptArrives(t *testing.T) {
	if (Status{Seq: 4, Sent: 4, Command: "bash"}).AtPrompt() {
		t.Fatal("no new prompt since input was sent")
	}
	if (Status{Seq: 5, Sent: 4, Command: "vim"}).AtPrompt() {
		t.Fatal("a program is in the foreground")
	}
}
```

`internal/term/render_test.go`:
```go
package term

import (
	"slices"
	"testing"
)

func TestTakesSplitOnFilmOffAndEnd(t *testing.T) {
	entries := []Entry{
		{T: 0, Film: true}, {T: 1, Film: true}, {T: 2, Film: false},
		{T: 5, Film: true}, {T: 6, End: true},
	}
	takes := Takes(entries)
	if len(takes) != 2 {
		t.Fatalf("got %d takes", len(takes))
	}
	if takes[0].Start != 0 || takes[0].End != 2 || len(takes[0].Entries) != 2 {
		t.Errorf("take 1: %+v", takes[0])
	}
	if takes[1].Start != 5 || takes[1].End != 6 || len(takes[1].Entries) != 1 {
		t.Errorf("take 2: %+v", takes[1])
	}
}

func TestTakeWithoutEndMarkerHoldsItsLastSnapshotOneSecond(t *testing.T) {
	takes := Takes([]Entry{{T: 10, Film: true}, {T: 10.5, Film: true}})
	if len(takes) != 1 || takes[0].End != 11.5 {
		t.Fatalf("got %+v", takes)
	}
}

func TestSlotsShowTheLatestSnapshotAtEachFrame(t *testing.T) {
	take := Take{Start: 0, End: 0.5, Entries: []Entry{{T: 0}, {T: 0.25}}}
	if got := Slots(take, 10); !slices.Equal(got, []int{0, 0, 0, 1, 1}) {
		t.Fatalf("got %v", got)
	}
}
```

`cmd/movie/term_test.go`:
```go
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/prime-radiant-inc/proving-it-works/internal/testmedia"
)

// startSession starts a term session and kills its tmux server when t ends.
func startSession(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("movie term needs macOS, Linux, or WSL")
	}
	testmedia.Require(t, "tmux")
	session := filepath.Join(dir, "session")
	r := runMovie(t, dir, append([]string{"term", "start", session, "--size", "60x10"}, args...)...)
	if r.code != 0 {
		t.Fatalf("start: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	t.Cleanup(func() {
		var s struct{ Socket string }
		if data, err := os.ReadFile(filepath.Join(session, "session.json")); err == nil && json.Unmarshal(data, &s) == nil {
			exec.Command("tmux", "-S", s.Socket, "kill-server").Run()
		}
	})
	return session
}

func TestTermFilmsACommandIntoFrames(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	r := runMovie(t, dir, "term", "run", session, "echo proving-it-works")
	if r.code != 0 || !strings.Contains(r.stdout, "proving-it-works") || !strings.Contains(r.stdout, `"exit_code":0`) {
		t.Fatalf("run: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	takes := filepath.Join(dir, "takes")
	r = runMovie(t, dir, "term", "stop", session, takes)
	if r.code != 0 {
		t.Fatalf("stop: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	frames, _ := filepath.Glob(filepath.Join(takes, "take-1", "f*.png"))
	if len(frames) < 5 {
		t.Fatalf("take-1 has %d frames", len(frames))
	}
	var take struct{ Rate float64 }
	data, err := os.ReadFile(filepath.Join(takes, "take-1", "take.json"))
	if err != nil || json.Unmarshal(data, &take) != nil || take.Rate != 10 {
		t.Fatalf("take.json: %s %v", data, err)
	}
}

func TestTermRunReportsAFailingCommand(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	r := runMovie(t, dir, "term", "run", session, "false")
	if r.code != 1 || !strings.Contains(r.stdout, `"exit_code":1`) {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/fonts ./internal/term ./cmd/movie -run 'Sans|Parse|NotAt|Takes|Take|Slots|Term'`
Expected: FAIL (undefined identifiers, unknown command `term`).

- [ ] **Step 4: Write `internal/fonts/fonts.go`**

```go
// Package fonts embeds the fonts movie draws with, so frames and subtitles
// never depend on what a machine has installed.
package fonts

import (
	_ "embed"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

//go:embed DejaVuSans.ttf
var sansTTF []byte

//go:embed DejaVuSansMono.ttf
var monoTTF []byte

var (
	sans = mustParse(sansTTF)
	mono = mustParse(monoTTF)
)

func mustParse(data []byte) *opentype.Font {
	f, err := opentype.Parse(data)
	if err != nil {
		panic(err)
	}
	return f
}

// Sans is DejaVu Sans, for cards and burned subtitles.
func Sans() *opentype.Font { return sans }

// Mono is DejaVu Sans Mono, for terminals.
func Mono() *opentype.Font { return mono }

// SansTTF is the Sans font file, for handing to libass.
func SansTTF() []byte { return sansTTF }

// Face returns f at size pixels.
func Face(f *opentype.Font, size float64) font.Face {
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	return face
}

// Find returns the index of the first font in chain that can draw r, or -1.
func Find(chain []*opentype.Font, r rune, buf *sfnt.Buffer) int {
	for i, f := range chain {
		if g, err := f.GlyphIndex(buf, r); err == nil && g != 0 {
			return i
		}
	}
	return -1
}

// Missing lists, once each and in order, the characters of text that no
// font in chain can draw. Whitespace is never missing.
func Missing(chain []*opentype.Font, text string) []rune {
	var buf sfnt.Buffer
	seen := map[rune]bool{}
	var missing []rune
	for _, r := range text {
		if unicode.IsSpace(r) || seen[r] {
			continue
		}
		seen[r] = true
		if Find(chain, r, &buf) < 0 {
			missing = append(missing, r)
		}
	}
	return missing
}

// Describe names characters with their code points: "漢 (U+6F22)".
func Describe(runes []rune) string {
	parts := make([]string, len(runes))
	for i, r := range runes {
		parts[i] = fmt.Sprintf("%c (U+%04X)", r, r)
	}
	return strings.Join(parts, ", ")
}
```

- [ ] **Step 5: Write the session and status code**

`internal/term/session.go`:
```go
// Package term films a terminal: tmux holds a clean bash, the verbs drive it,
// a background recorder snapshots the screen, and render draws the frames.
package term

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// window is the tmux session name inside the private server.
const window = "movie"

// Session is one filmed terminal: a private tmux server holding a clean bash.
type Session struct {
	Dir     string   `json:"-"`
	Socket  string   `json:"socket"`
	History string   `json:"history"`
	Wrapper []string `json:"wrapper,omitempty"`
}

// newSession names a session's socket and history. The socket lives in /tmp
// because Unix socket paths are limited to about 100 bytes; it is named by a
// hash of the session directory, so each session gets its own server.
func newSession(dir string, wrapper []string) (*Session, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(abs))
	id := hex.EncodeToString(sum[:6])
	s := &Session{Dir: abs, Socket: "/tmp/movie-" + id + ".sock", Wrapper: wrapper}
	s.History = filepath.Join(abs, "history")
	if len(wrapper) > 0 {
		s.History = "/tmp/movie-" + id + ".history"
	}
	return s, nil
}

// Load reads a running session's description.
func Load(dir string) (*Session, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(abs, "session.json"))
	if err != nil {
		return nil, fmt.Errorf("%s is not a movie term session: %w", dir, err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s/session.json: %w", dir, err)
	}
	s.Dir = abs
	return &s, nil
}

func (s *Session) save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.Dir, "session.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, "session.json"))
}

// tmux runs a tmux command against this session's private server, through
// the wrapper when there is one. -u: the terminal is UTF-8; -f /dev/null:
// never read the user's tmux.conf.
func (s *Session) tmux(args ...string) (string, error) {
	argv := append(append([]string{}, s.Wrapper...), "tmux", "-u", "-S", s.Socket, "-f", "/dev/null")
	argv = append(argv, args...)
	cmd := exec.Command(argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// quote makes s one word for sh.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// prompt reports each prompt through the pane title, which tmux exposes as
// #{pane_title} and the renderer never draws. It captures $? first, then
// unexports itself so a nested bash does not report prompts of its own.
const prompt = `s=$?; export -n PROMPT_COMMAND; MOVIE_N=$((MOVIE_N+1)); printf '\033]2;MOVIE;%s;%s\007' "$MOVIE_N" "$s"`

// shellCommand starts a clean bash: no startup files, its own history file,
// no macOS "default shell is now zsh" banner, and none of the variables that
// leak the invoking terminal or the Git Bash launcher into the take.
func shellCommand(history string) string {
	return strings.Join([]string{
		"exec env",
		"-u TERM_PROGRAM -u TERM_PROGRAM_VERSION -u TERM_SESSION_ID -u TMUX",
		"-u MSYS_NO_PATHCONV -u MSYS2_ARG_CONV_EXCL",
		"TERM=xterm-256color",
		"BASH_SILENCE_DEPRECATION_WARNING=1",
		"HISTFILE=" + quote(history),
		"PS1=" + quote(`\w \$ `),
		"PROMPT_COMMAND=" + quote(prompt),
		"bash --noprofile --norc -i",
	}, " ")
}
```

`internal/term/status.go`:
```go
package term

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Status is what tmux reports about the filmed pane at one moment.
type Status struct {
	CursorX, CursorY, Cols, Rows int
	CursorVisible, Film          bool
	Sent                         int    // prompt sequence number when input was last sent
	Seq, Code                    int    // the latest prompt's sequence number and reported status
	Command                      string // the pane's foreground command
	Screen                       string
}

// AtPrompt reports whether bash has shown a new prompt since input was last
// sent, so typing now cannot land in a running program's stdin.
func (st Status) AtPrompt() bool { return st.Seq > st.Sent && st.Command == "bash" }

const statusFormat = "#{cursor_x};#{cursor_y};#{pane_width};#{pane_height};#{cursor_flag};" +
	"#{@movie_film};#{@movie_sent};#{pane_current_command};#{pane_title}"

// parseTitle reads the MOVIE;<sequence>;<status> marker the prompt sets.
func parseTitle(title string) (seq, code int, ok bool) {
	parts := strings.Split(title, ";")
	if len(parts) != 3 || parts[0] != "MOVIE" {
		return 0, 0, false
	}
	seq, err1 := strconv.Atoi(parts[1])
	code, err2 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return seq, code, true
}

// parseStatus reads a snapshot: one status line, then the screen.
func parseStatus(out string) (Status, error) {
	line, screen, _ := strings.Cut(out, "\n")
	f := strings.SplitN(line, ";", 9)
	if len(f) != 9 {
		return Status{}, fmt.Errorf("unexpected tmux status line %q", line)
	}
	var n [4]int
	for i := range n {
		v, err := strconv.Atoi(f[i])
		if err != nil {
			return Status{}, fmt.Errorf("unexpected tmux status line %q", line)
		}
		n[i] = v
	}
	st := Status{CursorX: n[0], CursorY: n[1], Cols: n[2], Rows: n[3],
		CursorVisible: f[4] == "1", Film: f[5] != "off", Command: f[7], Screen: screen}
	st.Sent, _ = strconv.Atoi(f[6])
	st.Seq, st.Code, _ = parseTitle(f[8])
	return st, nil
}

// status snapshots the pane in one tmux call: status line, then screen, with
// colour codes when styled.
func (s *Session) status(styled bool) (Status, error) {
	capture := []string{"capture-pane", "-p", "-t", window}
	if styled {
		capture = append(capture, "-e")
	}
	out, err := s.tmux(append([]string{"display-message", "-p", "-t", window, statusFormat, ";"}, capture...)...)
	if err != nil {
		return Status{}, err
	}
	return parseStatus(out)
}

var errTimeout = errors.New("timed out")

// waitFor polls the pane until done holds or timeout passes.
func (s *Session) waitFor(timeout time.Duration, done func(Status) bool) (Status, error) {
	deadline := time.Now().Add(timeout)
	for {
		st, err := s.status(false)
		if err != nil {
			return st, err
		}
		if done(st) {
			return st, nil
		}
		if time.Now().After(deadline) {
			return st, errTimeout
		}
		time.Sleep(50 * time.Millisecond)
	}
}
```

- [ ] **Step 6: Write the verbs, recorder, and spawn**

`internal/term/verbs.go`:
```go
package term

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

// StartOptions shape a new session.
type StartOptions struct {
	Cwd        string
	Cols, Rows int
	Wrapper    []string
}

// requireEmptyDir refuses a directory that exists and holds anything, so a
// new session or take never mixes with an old one.
func requireEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty: use a new directory", dir)
	}
	return nil
}

// Start creates the session, clears the screen off camera, starts the
// recorder in the background, and returns.
func Start(dir string, o StartOptions, stdout io.Writer) error {
	if runtime.GOOS == "windows" {
		return errors.New("movie term needs macOS, Linux, or WSL")
	}
	if err := requireEmptyDir(dir); err != nil {
		return err
	}
	if len(o.Wrapper) == 0 {
		if _, err := exec.LookPath("tmux"); err != nil {
			return errors.New("tmux not on PATH: install tmux")
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	s, err := newSession(dir, o.Wrapper)
	if err != nil {
		return err
	}
	cwd := o.Cwd
	if cwd == "" && len(o.Wrapper) == 0 {
		if cwd, err = os.Getwd(); err != nil {
			return err
		}
	}
	args := []string{"new-session", "-d", "-s", window, "-x", strconv.Itoa(o.Cols), "-y", strconv.Itoa(o.Rows)}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	args = append(args, shellCommand(s.History), ";",
		"set-option", "-t", window, "status", "off", ";",
		"set-option", "-t", window, "@movie_film", "on", ";",
		"set-option", "-t", window, "@movie_sent", "0")
	if _, err := s.tmux(args...); err != nil {
		return err
	}
	if _, err := s.waitFor(10*time.Second, func(st Status) bool { return st.Seq >= 1 }); err != nil {
		s.tmux("kill-server")
		return fmt.Errorf("the shell never showed a prompt: %w", err)
	}
	if err := s.send("clear", "Enter"); err != nil {
		return err
	}
	if _, err := s.waitFor(10*time.Second, func(st Status) bool { return st.Seq >= 2 }); err != nil {
		s.tmux("kill-server")
		return fmt.Errorf("the shell did not clear: %w", err)
	}
	if err := s.save(); err != nil {
		return err
	}
	if err := spawnRecorder(s); err != nil {
		s.tmux("kill-server")
		return err
	}
	recording := filepath.Join(s.Dir, "recording.jsonl")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if info, err := os.Stat(recording); err == nil && info.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the recorder did not start; see %s", filepath.Join(s.Dir, "recorder.log"))
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Fprintf(stdout, "{\"ready\":true,\"session\":%q}\n", s.Dir)
	return nil
}

// humanPace is the delay between typed characters: about 55 ms, faster for
// long commands so typing never takes more than 4 seconds.
func humanPace(n int) time.Duration {
	if n == 0 {
		return 0
	}
	return min(55*time.Millisecond, 4*time.Second/time.Duration(n))
}

// typeText types text one character at a time, as raw bytes (send-keys -H),
// so tmux never interprets a character such as ";" or a leading "-".
func (s *Session) typeText(text string) error {
	pace := humanPace(len([]rune(text)))
	for _, r := range text {
		args := []string{"send-keys", "-t", window, "-H"}
		for _, b := range []byte(string(r)) {
			args = append(args, fmt.Sprintf("%02x", b))
		}
		if _, err := s.tmux(args...); err != nil {
			return err
		}
		time.Sleep(pace)
	}
	return nil
}

// send marks input as sent at the current prompt, types text, and presses keys.
func (s *Session) send(text string, keys ...string) error {
	st, err := s.status(false)
	if err != nil {
		return err
	}
	if _, err := s.tmux("set-option", "-t", window, "@movie_sent", strconv.Itoa(st.Seq)); err != nil {
		return err
	}
	if err := s.typeText(text); err != nil {
		return err
	}
	for _, k := range keys {
		if _, err := s.tmux("send-keys", "-t", window, k); err != nil {
			return err
		}
	}
	return nil
}

// Outcome is what run and wait report, on one JSON line before the screen.
type Outcome struct {
	Outcome  string `json:"outcome"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

// RunCommand types command at a prompt, presses Enter, and waits for the
// next prompt.
func RunCommand(s *Session, command string, timeout time.Duration, stdout io.Writer) (int, error) {
	st, err := s.status(false)
	if err != nil {
		return exitcode.Usage, err
	}
	if !st.AtPrompt() {
		return exitcode.Usage, fmt.Errorf("the shell is not at a prompt (%s is running): use wait, or key C-c", st.Command)
	}
	if err := s.send(command, "Enter"); err != nil {
		return exitcode.Usage, err
	}
	return s.await(timeout, 0, stdout)
}

// await waits for the next prompt, for the screen to stay unchanged for
// quiet (when quiet > 0), or for timeout, and prints the outcome and screen.
func (s *Session) await(timeout, quiet time.Duration, stdout io.Writer) (int, error) {
	start, lastChange := time.Now(), time.Now()
	last := ""
	for {
		st, err := s.status(false)
		if err != nil {
			return exitcode.Usage, err
		}
		switch {
		case st.AtPrompt():
			code := st.Code
			report(stdout, Outcome{"completed", &code}, st.Screen)
			if code == 0 {
				return exitcode.OK, nil
			}
			return exitcode.Verdict, nil
		case st.Screen != last:
			last, lastChange = st.Screen, time.Now()
		case quiet > 0 && time.Since(lastChange) >= quiet:
			report(stdout, Outcome{Outcome: "quiet"}, st.Screen)
			return exitcode.Running, nil
		}
		if time.Since(start) >= timeout {
			report(stdout, Outcome{Outcome: "running"}, st.Screen)
			return exitcode.Running, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func report(stdout io.Writer, o Outcome, screen string) {
	line, _ := json.Marshal(o)
	fmt.Fprintf(stdout, "%s\n%s\n", line, strings.TrimRight(screen, "\n"))
}

// Stop ends the session, waits for the recorder to finish, and renders.
func Stop(s *Session, outdir string, px image.Point, stdout io.Writer) error {
	if err := requireEmptyDir(outdir); err != nil {
		return err
	}
	s.tmux("kill-server") // already gone is fine
	done := filepath.Join(s.Dir, "recorder.done")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(done); err == nil {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(stdout, "WARN       the recorder did not confirm it stopped; rendering what it wrote")
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return Render(s.Dir, outdir, px, stdout)
}
```

`internal/term/record.go`:
```go
package term

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Entry is one recorded snapshot of the filmed pane.
type Entry struct {
	T       float64 `json:"t"`
	End     bool    `json:"end,omitempty"`
	Film    bool    `json:"film"`
	Cols    int     `json:"cols"`
	Rows    int     `json:"rows"`
	CursorX int     `json:"cx"`
	CursorY int     `json:"cy"`
	Cursor  bool    `json:"cursor"`
	Screen  string  `json:"screen"`
}

func now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// Record snapshots the pane up to ten times a second, appending each change
// to recording.jsonl, until the tmux server is gone.
func Record(dir string) error {
	s, err := Load(dir)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, "recording.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer os.WriteFile(filepath.Join(s.Dir, "recorder.done"), nil, 0o644)
	defer f.Close()
	enc := json.NewEncoder(f)
	var last Entry
	first := true
	for {
		began := time.Now()
		st, err := s.status(true)
		if err != nil {
			return enc.Encode(Entry{T: now(), End: true})
		}
		e := Entry{T: now(), Film: st.Film, Cols: st.Cols, Rows: st.Rows,
			CursorX: st.CursorX, CursorY: st.CursorY, Cursor: st.CursorVisible, Screen: st.Screen}
		probe := e
		probe.T = last.T
		if first || probe != last {
			if err := enc.Encode(e); err != nil {
				return err
			}
			last, first = e, false
		}
		time.Sleep(100*time.Millisecond - time.Since(began))
	}
}
```

`internal/term/spawn_unix.go`:
```go
//go:build !windows

package term

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// spawnRecorder starts `movie term _record SESSION` in its own session, so it
// outlives this command and no harness has to keep a task alive for it.
func spawnRecorder(s *Session) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.Create(filepath.Join(s.Dir, "recorder.log"))
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(exe, "term", "_record", s.Dir)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
```

`internal/term/spawn_windows.go`:
```go
//go:build windows

package term

import "errors"

func spawnRecorder(*Session) error { return errors.New("movie term needs macOS, Linux, or WSL") }
```

- [ ] **Step 7: Write the skeleton renderer**

`internal/term/render.go`:
```go
package term

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/prime-radiant-inc/proving-it-works/internal/fonts"
)

// FPS is the frame rate of rendered takes.
const FPS = 10

// Take is one stretch of recording filmed without a break.
type Take struct {
	Start, End float64
	Entries    []Entry
}

// Takes splits a recording into takes. Filming starts at an entry with Film
// set and stops at the next entry without it, or at the end marker. A
// recording that stops without an end marker holds its last snapshot for one
// second.
func Takes(entries []Entry) []Take {
	var takes []Take
	var cur *Take
	for _, e := range entries {
		filming := e.Film && !e.End
		switch {
		case filming && cur == nil:
			cur = &Take{Start: e.T, Entries: []Entry{e}}
		case filming:
			cur.Entries = append(cur.Entries, e)
		case cur != nil:
			cur.End = e.T
			takes = append(takes, *cur)
			cur = nil
		}
	}
	if cur != nil {
		cur.End = cur.Entries[len(cur.Entries)-1].T + 1
		takes = append(takes, *cur)
	}
	return takes
}

// Slots returns, for each frame of a take at fps, the index of the entry on
// screen at that moment.
func Slots(t Take, fps float64) []int {
	n := int(math.Ceil((t.End-t.Start)*fps - 1e-9))
	slots := make([]int, n)
	j := 0
	for k := range n {
		at := t.Start + float64(k)/fps
		for j+1 < len(t.Entries) && t.Entries[j+1].T <= at {
			j++
		}
		slots[k] = j
	}
	return slots
}

func readRecording(dir string) ([]Entry, error) {
	f, err := os.Open(filepath.Join(dir, "recording.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		var e Entry
		if json.Unmarshal(scanner.Bytes(), &e) != nil {
			break // a recorder killed mid-write leaves a partial last line
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// Render draws every take of the recording in dir into outdir/take-N/.
func Render(dir, outdir string, px image.Point, stdout io.Writer) error {
	if err := requireEmptyDir(outdir); err != nil {
		return err
	}
	entries, err := readRecording(dir)
	if err != nil {
		return err
	}
	takes := Takes(entries)
	if len(takes) == 0 {
		return errors.New("nothing was filmed")
	}
	first := takes[0].Entries[0]
	r := newRenderer(px, first.Cols, first.Rows)
	for i, t := range takes {
		name := fmt.Sprintf("take-%d", i+1)
		takeDir, err := filepath.Abs(filepath.Join(outdir, name))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(takeDir, 0o755); err != nil {
			return err
		}
		prev, frame := -1, []byte(nil)
		slots := Slots(t, FPS)
		for k, idx := range slots {
			if idx != prev {
				if frame, err = r.draw(t.Entries[idx]); err != nil {
					return err
				}
				prev = idx
			}
			if err := os.WriteFile(filepath.Join(takeDir, fmt.Sprintf("f%05d.png", k)), frame, 0o644); err != nil {
				return err
			}
		}
		meta, _ := json.MarshalIndent(map[string]any{"id": name, "frames": takeDir, "rate": FPS,
			"seconds": math.Round((t.End-t.Start)*10) / 10}, "", "  ")
		if err := os.WriteFile(filepath.Join(takeDir, "take.json"), meta, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: %d frames, %.1fs -> %s\n", name, len(slots), t.End-t.Start, takeDir)
	}
	return nil
}

var (
	foreground = color.RGBA{0xe8, 0xe6, 0xe1, 0xff}
	backdrop   = color.RGBA{0x10, 0x10, 0x14, 0xff}
	sgr        = regexp.MustCompile("\x1b\\[[0-9;:]*m")
)

// renderer draws snapshots as cols x rows cells filling 96% of the frame.
type renderer struct {
	px                   image.Point
	face                 font.Face
	cols, rows           int
	cellW, cellH, ascent int
}

func newRenderer(px image.Point, cols, rows int) *renderer {
	probe := fonts.Face(fonts.Mono(), 100)
	adv, _ := probe.GlyphAdvance('M')
	height := probe.Metrics().Height
	size := math.Min(
		float64(px.X)*0.96/(float64(cols)*float64(adv)/64/100),
		float64(px.Y)*0.96/(float64(rows)*float64(height)/64/100))
	face := fonts.Face(fonts.Mono(), size)
	adv, _ = face.GlyphAdvance('M')
	m := face.Metrics()
	return &renderer{px: px, face: face, cols: cols, rows: rows,
		cellW: adv.Ceil(), cellH: m.Height.Ceil(), ascent: m.Ascent.Ceil()}
}

// draw renders one snapshot as plain text. Task 17 replaces this with cell
// rendering that honours colours, wide characters, and the cursor.
func (r *renderer) draw(e Entry) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, r.px.X, r.px.Y))
	draw.Draw(img, img.Bounds(), image.NewUniform(backdrop), image.Point{}, draw.Src)
	ox, oy := (r.px.X-r.cols*r.cellW)/2, (r.px.Y-r.rows*r.cellH)/2
	d := font.Drawer{Dst: img, Src: image.NewUniform(foreground), Face: r.face}
	for y, line := range strings.Split(sgr.ReplaceAllString(e.Screen, ""), "\n") {
		if y >= r.rows {
			break
		}
		d.Dot = fixed.P(ox, oy+y*r.cellH+r.ascent)
		d.DrawString(line)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 8: Write the term CLI**

`internal/term/main.go`:
```go
package term

import (
	"flag"
	"fmt"
	"image"
	"io"
	"slices"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

const usage = `usage: movie term VERB SESSION ...

  start SESSION [--cwd DIR] [--size 120x34] [-- WRAPPER...]
  run SESSION 'cmd' [--timeout 60]
  stop SESSION OUTDIR [--px 1600x900]
  render SESSION OUTDIR [--px 1600x900]
`

// Main is `movie term`.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitcode.Usage
	}
	verb, rest := args[0], args[1:]
	code, err := dispatch(verb, rest, stdout, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "movie term %s: %v\n", verb, err)
		if code == exitcode.OK {
			code = exitcode.Usage
		}
	}
	return code
}

func dispatch(verb string, args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("movie term "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	switch verb {
	case "start":
		var wrapper []string
		if i := slices.Index(args, "--"); i >= 0 {
			args, wrapper = args[:i], args[i+1:]
		}
		cwd := fs.String("cwd", "", "starting directory (inside the container when wrapped)")
		size := fs.String("size", "120x34", "terminal columns x rows")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 1 {
			return exitcode.Usage, fmt.Errorf("needs exactly one SESSION directory")
		}
		cols, rows, err := cli.ParseSize(*size)
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Start(pos[0], StartOptions{Cwd: *cwd, Cols: cols, Rows: rows, Wrapper: wrapper}, stdout)
	case "run":
		timeout := fs.Float64("timeout", 60, "seconds to wait for the next prompt")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 2 {
			return exitcode.Usage, fmt.Errorf("needs SESSION and a command")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return RunCommand(s, pos[1], seconds(*timeout), stdout)
	case "stop", "render":
		px := fs.String("px", "1600x900", "frame size in pixels")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 2 {
			return exitcode.Usage, fmt.Errorf("needs SESSION and OUTDIR")
		}
		w, h, err := cli.ParseSize(*px)
		if err != nil {
			return exitcode.Usage, err
		}
		if verb == "render" {
			return exitcode.OK, Render(pos[0], pos[1], image.Pt(w, h), stdout)
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Stop(s, pos[1], image.Pt(w, h), stdout)
	case "_record":
		if len(args) != 1 {
			return exitcode.Usage, fmt.Errorf("needs SESSION")
		}
		return exitcode.OK, Record(args[0])
	}
	fmt.Fprint(stderr, usage)
	return exitcode.Usage, fmt.Errorf("unknown verb %q", verb)
}

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
```

In `cmd/movie/main.go`, import `internal/term` and add:
```go
	case "term":
		return term.Main(args[1:], stdout, stderr)
```

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS (term tests SKIP only if tmux is missing, saying so).

- [ ] **Step 10: Look at a rendered frame**

```bash
go build -o /tmp/movie ./cmd/movie
d=$(mktemp -d); /tmp/movie term start "$d/s" --size 60x10
/tmp/movie term run "$d/s" 'echo hello from the skeleton'
/tmp/movie term stop "$d/s" "$d/takes"
open "$d/takes/take-1/f00005.png" 2>/dev/null || xdg-open "$d/takes/take-1/f00005.png"
```
Expected: a dark frame with a `$` prompt, the typed command, and its output, legible and centered. Do not continue until you have looked.

- [ ] **Step 11: Commit**

```bash
git add go.mod go.sum internal/fonts internal/term cmd/movie
git commit -m "Skeleton movie term: a clean bash in private tmux, recorded and rendered to frames"
```

The skeleton is complete: every command runs end to end.

---

# Part 2: Flesh out

### Task 7: Port `check` completely and retire `check-movie`

**Files:**
- Create: `internal/srt/end.go`, `internal/srt/end_test.go`, `internal/check/measure.go`, `internal/check/sheet.go`, `internal/check/subtitles.go`, `internal/check/verdict_test.go`, `internal/check/measure_test.go`
- Modify: `internal/check/verdict.go` (replace), `internal/check/run.go` (replace `Run`), `cmd/movie/check_test.go`
- Delete: `skills/proving-it-works-with-a-movie/scripts/check-movie`, `tests/proving-it-works-with-a-movie/test_checker.py`; in `tests/proving-it-works-with-a-movie/test_narration_contract.py` the method `test_checker_sampling_escapes_only_output_directory_percents`; in `tests/proving-it-works-with-a-movie/test_subtitle_contract.py` the class `SubtitleParserContract`; the `"checker"` entry in `tests/proving-it-works-with-a-movie/run-tests.py` and its line in that directory's `README.md`
- Docs: `SKILL.md`, `README.md`, `rendering-from-a-log.md`, `rendering-stills.md`, `recording-motion.md`, `assembling.md` (only the check-movie references)

**Interfaces:**
- Produces: `srt.End(text string) (*float64, error)`; `check.Measurements{Duration float64; HasAudio bool; Changes, Levels []float64}`; `check.Subtitles{Found bool; Source string; End *float64}`; `check.Evaluate(m Measurements, subs Subtitles, o Options) Report`; `check.NeedsSubtitles(m, o) bool`; `Report` gains `ChangeSeconds, TalkSeconds []int` and `SubtitleNote string`.

- [ ] **Step 1: Write the failing unit tests**

`internal/srt/end_test.go`:
```go
package srt

import "testing"

func end(t *testing.T, text string) float64 {
	t.Helper()
	got, err := End(text)
	if err != nil || got == nil {
		t.Fatalf("End(%q) = %v, %v", text, got, err)
	}
	return *got
}

func TestLiteralArrowInCaptionIsNotATimingLine(t *testing.T) {
	if got := end(t, "1\n00:00:00,000 --> 00:00:01,250\nFollow source --> destination.\n"); got != 1.25 {
		t.Fatal(got)
	}
}

func TestTimestampShapedCaptionCannotExtendCoverage(t *testing.T) {
	if got := end(t, "1\n00:00:00,000 --> 00:00:01,250\n00:00:00,000 --> 00:59:00,000\n"); got != 1.25 {
		t.Fatal(got)
	}
}

func TestMalformedTimingIsRejected(t *testing.T) {
	for _, timing := range []string{"00:00:00,000 --> invalid", "not a timing line", "00:00:00,000 --> 00:99:00,000"} {
		if _, err := End("1\n" + timing + "\ncaption\n"); err == nil {
			t.Errorf("accepted %q", timing)
		}
	}
}

func TestEmptySubtitlesHaveNoEnd(t *testing.T) {
	got, err := End("\n  \n")
	if err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
}

func TestLatestEndWinsAcrossCues(t *testing.T) {
	text := "1\n00:00:00,000 --> 00:00:10,250\nFirst\n\n2\n00:00:05,000 --> 00:00:06,000\nSecond\n"
	if got := end(t, text); got != 10.25 {
		t.Fatal(got)
	}
}

func TestByteOrderMarkAndCRLFAreAccepted(t *testing.T) {
	if got := end(t, "\ufeff1\r\n00:00:00,000 --> 00:00:02,000\r\nλ caption\r\n"); got != 2 {
		t.Fatal(got)
	}
}
```

`internal/check/verdict_test.go`:
```go
package check

import (
	"slices"
	"strings"
	"testing"
)

func repeat(v float64, n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = v
	}
	return s
}

// paced is a 20 s movie whose picture changes every second.
func paced(levels []float64) Measurements {
	return Measurements{Duration: float64(len(levels)), HasAudio: true,
		Changes: repeat(0.1, len(levels)-1), Levels: levels}
}

func f(v float64) *float64 { return &v }

func hasFailure(r Report, needle string) bool {
	return slices.ContainsFunc(r.Failures, func(s string) bool { return strings.Contains(s, needle) })
}

func TestSilentTrackPassesWhenAudioIsNotExpected(t *testing.T) {
	r := Evaluate(paced(repeat(-120, 20)), Subtitles{}, Options{ExpectSubtitles: true})
	if len(r.Failures) != 0 {
		t.Fatal(r.Failures)
	}
}

func TestAbsentAudioPassesWhenAudioIsNotExpected(t *testing.T) {
	m := Measurements{Duration: 20, Changes: repeat(0.1, 19)}
	if r := Evaluate(m, Subtitles{}, Options{ExpectSubtitles: true}); len(r.Failures) != 0 {
		t.Fatal(r.Failures)
	}
}

func TestSilentTrackFailsWhenAudioIsExpected(t *testing.T) {
	r := Evaluate(paced(repeat(-120, 20)), Subtitles{}, Options{ExpectAudio: true, ExpectSubtitles: true})
	if !hasFailure(r, "silent") {
		t.Fatal(r.Failures)
	}
}

func TestMissingAudioStreamFailsWhenAudioIsExpected(t *testing.T) {
	m := Measurements{Duration: 20, Changes: repeat(0.1, 19)}
	if r := Evaluate(m, Subtitles{}, Options{ExpectAudio: true}); !hasFailure(r, "no audio stream") {
		t.Fatal(r.Failures)
	}
}

func TestAudibleSpeechStillNeedsSubtitlesWithoutAudioExpectation(t *testing.T) {
	r := Evaluate(paced(repeat(-20, 20)), Subtitles{Source: "movie.srt"}, Options{ExpectSubtitles: true})
	if !hasFailure(r, "no subtitles") {
		t.Fatal(r.Failures)
	}
}

func TestSubtitleOptOutAllowsSpeechWithoutSubtitles(t *testing.T) {
	if r := Evaluate(paced(repeat(-20, 20)), Subtitles{}, Options{}); len(r.Failures) != 0 {
		t.Fatal(r.Failures)
	}
}

func TestTrackWithNoCuesFails(t *testing.T) {
	for _, n := range []int{2, 20} {
		r := Evaluate(paced(repeat(-20, n)), Subtitles{Found: true, Source: "embedded"}, Options{ExpectSubtitles: true})
		if !hasFailure(r, "no cues") {
			t.Errorf("%d s: %q", n, r.Failures)
		}
	}
}

func TestCuesMustReachTheEndOfSpeech(t *testing.T) {
	levels := append(repeat(-20, 10), repeat(-120, 10)...)
	short := Evaluate(paced(levels), Subtitles{Found: true, Source: "x.srt", End: f(6)}, Options{ExpectSubtitles: true})
	if !hasFailure(short, "subtitles stop at 6s") {
		t.Fatal(short.Failures)
	}
	enough := Evaluate(paced(levels), Subtitles{Found: true, Source: "x.srt", End: f(10)}, Options{ExpectSubtitles: true})
	if len(enough.Failures) != 0 {
		t.Fatal(enough.Failures)
	}
}

func TestSpeechEndOverridesDetectedSpeech(t *testing.T) {
	// speech runs all 20 s, but the narration ends at 8 s: a movie scene is talking
	r := Evaluate(paced(repeat(-20, 20)), Subtitles{Found: true, Source: "x.srt", End: f(6)},
		Options{ExpectSubtitles: true, SpeechEnd: f(8)})
	if len(r.Failures) != 0 {
		t.Fatal(r.Failures)
	}
}

func TestFrontLoadedActionFails(t *testing.T) {
	m := Measurements{Duration: 22, HasAudio: true,
		Changes: append(repeat(0.1, 2), repeat(0, 19)...), Levels: repeat(-20, 22)}
	r := Evaluate(m, Subtitles{Found: true, Source: "x.srt", End: f(22)}, Options{ExpectSubtitles: true})
	if !hasFailure(r, "every visible change happens in the first 1s") {
		t.Fatal(r.Failures)
	}
}

func TestStillFails(t *testing.T) {
	m := Measurements{Duration: 12, Changes: repeat(0, 11)}
	if r := Evaluate(m, Subtitles{}, Options{}); !hasFailure(r, "never reaches a new state") {
		t.Fatal(r.Failures)
	}
}

func TestLongMidMovieHoldWarns(t *testing.T) {
	changes := repeat(0, 40)
	changes[0], changes[35] = 0.1, 0.1
	r := Evaluate(Measurements{Duration: 41, Changes: changes}, Subtitles{}, Options{})
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "35s with no visible change") {
		t.Fatal(r.Warnings)
	}
}
```

`internal/check/measure_test.go`:
```go
package check

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestMovedFractionCountsPixelsBeyondTheDelta(t *testing.T) {
	a := []uint8{0, 0, 0, 0}
	b := []uint8{8, 9, 0, 200}
	if got := movedFraction(a, b); got != 0.5 {
		t.Fatal(got)
	}
}

func TestLevelsAreOneSecondRMSInDBFS(t *testing.T) {
	pcm := make([]byte, 2*8000*2) // two seconds at 8 kHz
	for i := range 8000 {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(16384))) // half scale
	}
	got := levels(pcm)
	if len(got) != 2 || math.Abs(got[0]-(-6.02)) > 0.01 || got[1] != -120 {
		t.Fatal(got)
	}
}

func TestGridColumnsFillTheSheetExactly(t *testing.T) {
	for n, want := range map[int]int{12: 4, 9: 3, 5: 5, 2: 2, 7: 4, 1: 1} {
		if got := gridColumns(n); got != want {
			t.Errorf("gridColumns(%d) = %d, want %d", n, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the unit tests to verify they fail**

Run: `go test ./internal/srt ./internal/check`
Expected: FAIL (undefined `End`, `movedFraction`, `levels`, `gridColumns`, Evaluate signature).

- [ ] **Step 3: Write `internal/srt/end.go`**

```go
// Package srt times, writes, and reads SubRip subtitles.
package srt

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	timingLine = regexp.MustCompile(`^(\d{2,}):([0-5]\d):([0-5]\d),(\d{3})\s+-->\s+(\d{2,}):([0-5]\d):([0-5]\d),(\d{3})$`)
	blankLine  = regexp.MustCompile(`\n\s*\n`)
	digits     = regexp.MustCompile(`^\d+$`)
)

// End returns the end of the last-ending cue, or nil when there are no cues.
// Only a cue's second line is its timing; text shaped like a timing line
// inside a caption never counts.
func End(text string) (*float64, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return nil, nil
	}
	var end *float64
	for _, block := range blankLine.Split(text, -1) {
		lines := strings.Split(block, "\n")
		if len(lines) < 2 || !digits.MatchString(strings.TrimSpace(lines[0])) {
			return nil, errors.New("malformed SRT cue index or missing timing line")
		}
		m := timingLine.FindStringSubmatch(strings.TrimSpace(lines[1]))
		if m == nil {
			return nil, fmt.Errorf("malformed SRT cue timing: %s", lines[1])
		}
		var n [4]int
		for i := range n {
			n[i], _ = strconv.Atoi(m[5+i])
		}
		v := float64(n[0]*3600+n[1]*60+n[2]) + float64(n[3])/1000
		if end == nil || v > *end {
			end = &v
		}
	}
	return end, nil
}
```

- [ ] **Step 4: Replace `internal/check/verdict.go`**

```go
// Package check is the mechanical gate for a proof movie: it samples picture
// and sound on one timeline and fails the defects per-frame inspection
// cannot see.
package check

import "fmt"

// Thresholds are heuristics tuned against real good and bad movies. They
// catch the egregious cases; they cannot say a movie is right.
const (
	pixelDelta  = 8     // grey levels a pixel must move to count as moved
	changeFrac  = 0.002 // more than 0.2% of pixels moved: a new state
	speechDB    = -45.0 // a second at or above this RMS is someone talking
	earlyAction = 0.40  // last change before this fraction of runtime: front-loaded
	tailTalkS   = 5.0   // ...with this much narration after it: broken
	warnTailS   = 15.0  // frozen tail worth mentioning even when it passes
	warnGapS    = 30.0  // a hold this long mid-movie looks like a hang
)

// Options says what the movie is expected to contain.
type Options struct {
	ExpectAudio     bool
	ExpectSubtitles bool
	// SpeechEnd, when set, is where subtitles must reach instead of the last
	// second of detected speech. build sets it to the end of the last
	// narration clip, so speech inside a movie scene does not count.
	SpeechEnd *float64
}

// Measurements is what was measured from the movie, once per second.
type Measurements struct {
	Duration float64
	HasAudio bool
	Changes  []float64 // fraction of pixels moved since the previous second
	Levels   []float64 // RMS in dBFS; empty without audio
}

// Subtitles is what the caller found for the movie.
type Subtitles struct {
	Found  bool     // a sidecar or embedded track exists
	Source string   // the sidecar's name, or "embedded"
	End    *float64 // end of the last cue; nil when there are no cues
}

// Report is the verdict, as written to check.json.
type Report struct {
	Duration      float64  `json:"duration"`
	ChangeSeconds []int    `json:"change_seconds"`
	TalkSeconds   []int    `json:"talk_seconds"`
	Failures      []string `json:"failures"`
	Warnings      []string `json:"warnings"`
	SubtitleNote  string   `json:"-"`
}

func changeSeconds(changes []float64) []int {
	s := []int{}
	for i, frac := range changes {
		if frac > changeFrac {
			s = append(s, i)
		}
	}
	return s
}

func talkSeconds(levels []float64) []int {
	s := []int{}
	for i, lv := range levels {
		if lv >= speechDB {
			s = append(s, i)
		}
	}
	return s
}

// NeedsSubtitles reports whether Evaluate will look at subtitles, so the
// caller reads them only when it must.
func NeedsSubtitles(m Measurements, o Options) bool {
	return o.ExpectSubtitles && len(talkSeconds(m.Levels)) > 0
}

// Evaluate turns measurements into failures and warnings.
func Evaluate(m Measurements, subs Subtitles, o Options) Report {
	changes, talking := changeSeconds(m.Changes), talkSeconds(m.Levels)
	r := Report{Duration: m.Duration, ChangeSeconds: changes, TalkSeconds: talking,
		Failures: []string{}, Warnings: []string{}}
	fail := func(format string, a ...any) { r.Failures = append(r.Failures, fmt.Sprintf(format, a...)) }
	warn := func(format string, a ...any) { r.Warnings = append(r.Warnings, fmt.Sprintf(format, a...)) }
	span := max(len(m.Changes), 1)

	if m.Duration < 1 {
		fail("duration is %.2fs - that is not a movie", m.Duration)
	}
	if o.ExpectAudio && !m.HasAudio {
		fail("expected narration but there is no audio stream")
	}
	if o.ExpectAudio && len(m.Levels) > 0 && len(talking) == 0 {
		fail("the audio track is silent end to end")
	}

	// a narrated movie with no subtitles fails for everyone watching it muted
	if len(talking) > 0 && o.ExpectSubtitles {
		speechEnd := float64(talking[len(talking)-1] + 1)
		if o.SpeechEnd != nil {
			speechEnd = *o.SpeechEnd
		}
		switch {
		case !subs.Found:
			fail("narrated, but no subtitles: expected %s beside the movie (or an embedded track). "+
				"movie build writes and burns them; pass --no-expect-subtitles only for a movie "+
				"nobody will ever watch muted.", subs.Source)
		case subs.End == nil:
			fail("%s: subtitles contain no cues", subs.Source)
		default:
			r.SubtitleNote = fmt.Sprintf("%s, last cue ends at %.1fs (narration ends %.0fs)", subs.Source, *subs.End, speechEnd)
			if *subs.End < speechEnd-3 {
				fail("subtitles stop at %.0fs but the narration runs to %.0fs - %.0fs of speech has no subtitles",
					*subs.End, speechEnd, speechEnd-*subs.End)
			}
		}
	}

	if len(changes) == 0 {
		fail("the picture never reaches a new state - this is a still, not a movie")
		return r
	}
	lastChange := changes[len(changes)-1]
	tailTalk := 0
	if len(talking) > 0 {
		tailTalk = talking[len(talking)-1] - lastChange
	}
	if float64(lastChange) < earlyAction*float64(span) && float64(tailTalk) > tailTalkS {
		fail("every visible change happens in the first %ds (%.0f%% of runtime), then the picture is "+
			"frozen for %ds while narration keeps talking for %ds of it. The demo is over before the "+
			"explanation starts: pace the action to the narration.",
			lastChange, 100*float64(lastChange)/float64(span), span-lastChange, tailTalk)
	} else if float64(tailTalk) > warnTailS {
		warn("%ds of narration after the last visible change (%.0f%% of runtime frozen)",
			tailTalk, 100*float64(span-lastChange)/float64(span))
	}
	gap := 0
	for i := 1; i < len(changes); i++ {
		gap = max(gap, changes[i]-changes[i-1])
	}
	if float64(gap) > warnGapS {
		warn("%ds with no visible change mid-movie - intentional hold, or did something hang?", gap)
	}
	return r
}
```

- [ ] **Step 5: Write measurement, contact sheet, and subtitle lookup**

`internal/check/measure.go`:
```go
package check

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
)

const thumbWidth = 320 // the metric is a pixel fraction, so any width works

// samplePicture writes one frame per second into workdir/samples and returns
// their paths and, per second, the fraction of pixels that moved. ffmpeg runs
// from the samples directory with a relative pattern, so no user path is
// parsed as a pattern.
func samplePicture(movie, workdir string) ([]string, []float64, error) {
	dir := filepath.Join(workdir, "samples")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	old, _ := os.ReadDir(dir)
	for _, e := range old {
		if filepath.Ext(e.Name()) == ".png" {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	if err := ffmpeg.Run(dir, nil, "-i", movie, "-vf", fmt.Sprintf("fps=1,scale=%d:-1", thumbWidth),
		"-f", "image2", "s%05d.png"); err != nil {
		return nil, nil, fmt.Errorf("frame sampling failed: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var paths []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".png" {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, nil, errors.New("no video frames could be sampled")
	}
	var fracs []float64
	var prev []uint8
	for _, p := range paths {
		px, err := grey(p)
		if err != nil {
			return nil, nil, err
		}
		if prev != nil {
			fracs = append(fracs, movedFraction(prev, px))
		}
		prev = px
	}
	return paths, fracs, nil
}

func grey(path string) ([]uint8, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	g := image.NewGray(img.Bounds())
	draw.Draw(g, g.Bounds(), img, img.Bounds().Min, draw.Src)
	return g.Pix, nil
}

// movedFraction is the fraction of pixels whose grey level moved by more
// than pixelDelta.
func movedFraction(a, b []uint8) float64 {
	n := min(len(a), len(b))
	if n == 0 {
		return 0
	}
	moved := 0
	for i := range n {
		if d := int(a[i]) - int(b[i]); d > pixelDelta || d < -pixelDelta {
			moved++
		}
	}
	return float64(moved) / float64(n)
}

// sampleSound decodes the first audio stream to 8 kHz mono and measures it.
func sampleSound(movie string) ([]float64, error) {
	pcm, err := ffmpeg.Output("-i", movie, "-map", "0:a:0", "-ac", "1", "-ar", "8000", "-f", "s16le", "-")
	if err != nil {
		return nil, fmt.Errorf("audio decode failed: %w", err)
	}
	if len(pcm) == 0 {
		return nil, errors.New("audio decode failed: no samples")
	}
	return levels(pcm), nil
}

// levels is the per-second RMS, in dBFS, of 8 kHz mono s16le PCM.
func levels(pcm []byte) []float64 {
	n := len(pcm) / 2
	var out []float64
	for start := 0; start < n; start += 8000 {
		end := min(start+8000, n)
		var sum float64
		for i := start; i < end; i++ {
			s := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
			sum += s * s
		}
		rms := math.Sqrt(sum / float64(end-start))
		if rms > 0 {
			out = append(out, 20*math.Log10(rms/32768))
		} else {
			out = append(out, -120)
		}
	}
	return out
}
```

`internal/check/sheet.go`:
```go
package check

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
)

// gridColumns picks a column count that fills the grid exactly where
// possible: an empty cell reads as a black frame, which is a defect signal,
// and a sheet that lies about the movie defeats the point of the sheet.
func gridColumns(n int) int {
	for _, c := range []int{4, 3, 5, 2} {
		if n%c == 0 {
			return c
		}
	}
	return min(4, n)
}

// contactSheet tiles up to 12 evenly spaced samples into out and returns the
// seconds they show.
func contactSheet(paths []string, out string) ([]int, error) {
	const count = 12
	var picks []int
	if len(paths) <= count {
		for i := range paths {
			picks = append(picks, i)
		}
	} else {
		for i := range count {
			picks = append(picks, int(math.RoundToEven(float64(i*(len(paths)-1))/float64(count-1))))
		}
	}
	var thumbs []image.Image
	for _, i := range picks {
		f, err := os.Open(paths[i])
		if err != nil {
			return nil, err
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		thumbs = append(thumbs, img)
	}
	w, h := thumbs[0].Bounds().Dx(), thumbs[0].Bounds().Dy()
	cols := gridColumns(len(thumbs))
	rows := (len(thumbs) + cols - 1) / cols
	sheet := image.NewRGBA(image.Rect(0, 0, cols*w, rows*h))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{48, 48, 52, 255}), image.Point{}, draw.Src)
	for i, t := range thumbs {
		at := image.Pt((i%cols)*w, (i/cols)*h)
		draw.Draw(sheet, image.Rectangle{at, at.Add(image.Pt(w, h))}, t, t.Bounds().Min, draw.Src)
	}
	f, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return picks, png.Encode(f, sheet)
}
```

`internal/check/subtitles.go`:
```go
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/srt"
)

// findSubtitles reads <movie stem>.srt beside the movie, else the first
// embedded subtitle track.
func findSubtitles(movie string, info ffmpeg.Info) (Subtitles, error) {
	sidecar := strings.TrimSuffix(movie, filepath.Ext(movie)) + ".srt"
	subs := Subtitles{Source: filepath.Base(sidecar)}
	var text string
	if data, err := os.ReadFile(sidecar); err == nil {
		subs.Found, text = true, string(data)
	} else if info.Has("subtitle") {
		out, err := ffmpeg.Output("-i", movie, "-map", "0:s:0", "-f", "srt", "-")
		if err != nil {
			return subs, fmt.Errorf("embedded subtitle extraction failed: %w", err)
		}
		subs.Found, subs.Source, text = true, "embedded", string(out)
	} else {
		return subs, nil
	}
	end, err := srt.End(text)
	if err != nil {
		return subs, fmt.Errorf("invalid subtitle timing in %s: %w", subs.Source, err)
	}
	subs.End = end
	return subs, nil
}
```

- [ ] **Step 6: Replace `Run` in `internal/check/run.go`**

Replace the `Run` function (keep `WorkDir`, `finish`, `yesNo`) with:
```go
// Run checks movie, prints the verdict, and writes check.json and the contact
// sheet. It returns exitcode.OK or exitcode.Verdict; an error means the movie
// could not be examined at all.
func Run(movie string, o Options, stdout io.Writer) (int, error) {
	movie, err := filepath.Abs(movie)
	if err != nil {
		return 0, err
	}
	if _, err := os.Stat(movie); err != nil {
		return 0, fmt.Errorf("no such movie: %s", movie)
	}
	if err := ffmpeg.Require(); err != nil {
		return 0, err
	}
	info, err := ffmpeg.Probe(movie)
	if err != nil {
		return 0, err
	}
	video, ok := info.First("video")
	if !ok {
		return 0, errors.New("no video stream")
	}
	workdir := WorkDir(movie)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return 0, err
	}
	paths, changes, err := samplePicture(movie, workdir)
	if err != nil {
		return 0, err
	}
	m := Measurements{Duration: info.Duration, HasAudio: info.Has("audio"), Changes: changes}
	if m.HasAudio {
		if m.Levels, err = sampleSound(movie); err != nil {
			return 0, err
		}
	}
	var subs Subtitles
	if NeedsSubtitles(m, o) {
		if subs, err = findSubtitles(movie, info); err != nil {
			return 0, err
		}
	}
	r := Evaluate(m, subs, o)

	fmt.Fprintf(stdout, "container  %s %dx%d, %.1fs, audio=%s\n",
		video.CodecName, video.Width, video.Height, info.Duration, yesNo(m.HasAudio))
	fmt.Fprintf(stdout, "picture    reaches a new state in %d of %d seconds%s\n",
		len(r.ChangeSeconds), max(len(changes), 1), lastAt(r.ChangeSeconds))
	if len(m.Levels) > 0 {
		fmt.Fprintf(stdout, "sound      audible in %d of %d seconds%s\n",
			len(r.TalkSeconds), len(m.Levels), lastAt(r.TalkSeconds))
	}
	if r.SubtitleNote != "" {
		fmt.Fprintf(stdout, "subtitles  %s\n", r.SubtitleNote)
	}
	sheet := filepath.Join(workdir, "contact-sheet.png")
	picks, err := contactSheet(paths, sheet)
	if err != nil {
		return 0, err
	}
	shown := make([]string, len(picks))
	for i, p := range picks {
		shown[i] = fmt.Sprintf("%ds", p)
	}
	fmt.Fprintf(stdout, "sheet      %s\n           sampled at %s\n", sheet, strings.Join(shown, ", "))
	return finish(r, workdir, stdout)
}

func lastAt(seconds []int) string {
	if len(seconds) == 0 {
		return ""
	}
	return fmt.Sprintf("; last at %ds", seconds[len(seconds)-1])
}
```

- [ ] **Step 7: Port the real-media checker tests**

Append to `cmd/movie/check_test.go`:
```go
// checkerMovies makes the movies the checker must judge, as today's suite did.
func checkerMovies(t *testing.T, dir string) {
	t.Helper()
	enc := []string{"-c:v", "libx264", "-pix_fmt", "yuv420p"}
	testmedia.FFmpeg(t, dir, append(append([]string{
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=2",
		"-f", "lavfi", "-i", "color=c=navy:size=320x240:rate=10:d=20",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=22",
		"-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0[v]", "-map", "[v]", "-map", "2:a"},
		enc...), "-c:a", "aac", "-shortest", "front-loaded.mp4")...)
	testmedia.FFmpeg(t, dir, append(append([]string{
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=22",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=22"}, enc...), "-c:a", "aac", "-shortest", "paced.mp4")...)
	testmedia.FFmpeg(t, dir, append(append([]string{
		"-f", "lavfi", "-i", "color=c=navy:size=320x240:rate=10:d=12",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=12"}, enc...), "-c:a", "aac", "-shortest", "still.mp4")...)
	testmedia.FFmpeg(t, dir, append([]string{
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=12"}, append(enc, "silent.mp4")...)...)
	subs := "1\n00:00:00,000 --> 00:00:07,000\nA narrated movie needs subtitles.\n\n" +
		"2\n00:00:07,000 --> 00:00:14,000\nThe checker treats their absence as a defect.\n\n" +
		"3\n00:00:14,000 --> 00:00:21,500\nAnd it notices when they stop early.\n"
	writeFile(t, dir, "paced.srt", subs)
	for _, name := range []string{"short", "nosubs", "embedded"} {
		copyFile(t, filepath.Join(dir, "paced.mp4"), filepath.Join(dir, name+".mp4"), 0o644)
	}
	writeFile(t, dir, "short.srt", strings.Join(strings.Split(subs, "\n")[:8], "\n")+"\n")
	testmedia.FFmpeg(t, dir, "-i", "paced.mp4", "-i", "paced.srt", "-map", "0:v:0", "-map", "0:a:0",
		"-map", "1:s:0", "-c", "copy", "-c:s", "mov_text", "embedded-track.mp4")
}

func TestCheckVerdictsOnRealMovies(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	checkerMovies(t, dir)
	for _, c := range []struct {
		movie  string
		args   []string
		code   int
		needle string
	}{
		{"front-loaded.mp4", nil, 1, "every visible change happens in the first"},
		{"paced.mp4", nil, 0, "Mechanical checks pass"},
		{"nosubs.mp4", nil, 1, "no subtitles"},
		{"nosubs.mp4", []string{"--no-expect-subtitles"}, 0, "Mechanical checks pass"},
		{"short.mp4", nil, 1, "subtitles stop at"},
		{"embedded-track.mp4", nil, 0, "subtitles  embedded"},
		{"still.mp4", nil, 1, "never reaches a new state"},
		{"silent.mp4", nil, 1, "no audio stream"},
		{"silent.mp4", []string{"--no-expect-audio"}, 0, "Mechanical checks pass"},
	} {
		r := runMovie(t, dir, append([]string{"check", c.movie}, c.args...)...)
		if r.code != c.code || !strings.Contains(r.stdout, c.needle) {
			t.Errorf("check %s %v: code %d, want %d with %q\n%s%s", c.movie, c.args, r.code, c.code, c.needle, r.stdout, r.stderr)
		}
	}
}

func TestCheckWritesAContactSheet(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "m.mp4")
	if r := runMovie(t, dir, "check", "m.mp4", "--no-expect-audio"); r.code != 0 {
		t.Fatalf("code %d\n%s", r.code, r.stdout)
	}
	f, err := os.Open(filepath.Join(dir, "m-check", "contact-sheet.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := png.Decode(f); err != nil {
		t.Fatalf("contact sheet is not a PNG: %v", err)
	}
}
```
Add `"image/png"` to that file's imports.

- [ ] **Step 8: Run all tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 9: Retire the Python checker and update the docs that mention it**

```bash
git rm skills/proving-it-works-with-a-movie/scripts/check-movie tests/proving-it-works-with-a-movie/test_checker.py
```

- In `tests/proving-it-works-with-a-movie/test_narration_contract.py`, delete the method `test_checker_sampling_escapes_only_output_directory_percents`.
- In `tests/proving-it-works-with-a-movie/test_subtitle_contract.py`, delete the class `SubtitleParserContract`.
- In `tests/proving-it-works-with-a-movie/run-tests.py`, delete the line `"checker": "test_checker.py",`; in that directory's `README.md`, delete the `--suite checker` line.
- In `SKILL.md`, replace the line `"$SKILL_DIR/scripts/check-movie"    movie.mp4      # nonzero exit: do not ship` with `"$SKILL_DIR/bin/movie" check      movie.mp4      # nonzero exit: do not ship`.
- In `rendering-from-a-log.md`, replace `` `"$SKILL_DIR/scripts/check-movie" reel.mp4 --no-expect-audio` `` with `` `"$SKILL_DIR/bin/movie" check reel.mp4 --no-expect-audio` ``.
- In `rendering-stills.md`, replace `` `"$SKILL_DIR/scripts/check-movie"` `` with `` `"$SKILL_DIR/bin/movie" check` ``.
- In `recording-motion.md` and `assembling.md`, replace the words `` `check-movie` `` with `` `movie check` ``.
- In `README.md`: in the scripts table, replace the `check-movie` row with `` | `movie check` | the gate (a Go binary in `bin/`; the other scripts are being ported) | ``; rename the heading `` ### `check-movie` `` to `` ### `movie check` ``; change the example command to `` $ skills/proving-it-works-with-a-movie/bin/movie check demo.mp4 ``; in Requirements, change the `uv` line to say uv runs the remaining Python scripts; in Tests, add `go test ./...` above the Python line.

Run: `uv run --script tests/proving-it-works-with-a-movie/run-tests.py --suite all`
Expected: PASS (the remaining Python suites still pass without the checker).

- [ ] **Step 10: Commit**

```bash
git add -A internal cmd skills tests README.md
git status   # confirm only intended files
git commit -m "Port check completely to Go and retire check-movie"
```

---

### Task 8: Full scene file parsing and validation

**Files:**
- Modify: `internal/scene/scene.go` (replace)
- Create: `internal/scene/scene_test.go`

**Interfaces:**
- Produces: `scene.Kind` constants `Card, Image, Frames, Movie`; `File{Path, Dir string; Width, Height, FPS int; Engine, Voice string; Scenes []Scene}` with `Narrated() bool`; `Scene{ID string; Kind Kind; Title, Subtitle, Source string; Rate, Duration float64; Narration string}`; `scene.Problems []string` (an `error`).

- [ ] **Step 1: Write the failing tests**

`internal/scene/scene_test.go`:
```go
package scene

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixture writes a scene file plus the media it names into a temp dir.
func fixture(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"shot.png", "frames/f1.png", "clip.mp4"} {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte("x"), 0o644)
	}
	os.MkdirAll(filepath.Join(dir, "empty"), 0o755)
	path := filepath.Join(dir, "demo.yaml")
	os.WriteFile(path, []byte(yaml), 0o644)
	return path
}

func TestValidFileGetsDefaults(t *testing.T) {
	f, err := Load(fixture(t, `scenes:
  - id: title
    card: proving-it-works
    subtitle: a subtitle
  - id: shot
    image: shot.png
    narration: "  Spoken
      words.  "
  - id: run
    frames: frames
  - id: clip
    movie: clip.mp4
`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Width != 1920 || f.Height != 1080 || f.FPS != 30 || f.Engine != "auto" || f.Voice != "" {
		t.Fatalf("defaults: %+v", f)
	}
	kinds := []Kind{f.Scenes[0].Kind, f.Scenes[1].Kind, f.Scenes[2].Kind, f.Scenes[3].Kind}
	if !slices.Equal(kinds, []Kind{Card, Image, Frames, Movie}) {
		t.Fatalf("kinds %v", kinds)
	}
	if f.Scenes[0].Duration != 3 || f.Scenes[2].Rate != 30 {
		t.Fatalf("duration %v rate %v", f.Scenes[0].Duration, f.Scenes[2].Rate)
	}
	if f.Scenes[1].Narration != "Spoken words." || !f.Narrated() {
		t.Fatalf("narration %q", f.Scenes[1].Narration)
	}
	if f.Scenes[1].Source != filepath.Join(filepath.Dir(f.Path), "shot.png") {
		t.Fatalf("source %q", f.Scenes[1].Source)
	}
}

func TestEveryProblemIsReportedAtOnce(t *testing.T) {
	_, err := Load(fixture(t, `size: 1921x1080
fps: 0
engine: robot
colour: red
scenes:
  - id: Bad_ID
    card: x
  - id: two
    card: x
    image: shot.png
  - id: none
  - id: dup
    image: missing.png
  - id: dup
    frames: empty
  - id: talky
    movie: clip.mp4
    narration: movies keep their own audio
  - id: slow
    frames: frames
    rate: -1
    duration: 4
`))
	var p Problems
	if !errors.As(err, &p) {
		t.Fatalf("got %v", err)
	}
	all := strings.Join(p, "\n")
	for _, want := range []string{
		"size must have even width and height",
		"fps must be a positive whole number",
		`engine must be one of auto, openai, openai-chat, piper`,
		`unknown top-level key "colour"`,
		"scene 1: id must match",
		"scene two: needs exactly one of card, image, frames, movie (found 2)",
		"scene none: needs exactly one of card, image, frames, movie (found 0)",
		"scene dup: no such image",
		"scene dup: duplicate id",
		"scene dup: no PNG frames in",
		`scene talky: "narration" is not a field of a movie scene`,
		"scene slow: rate must be a positive number",
		`scene slow: "duration" is not a field of a frames scene`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing problem %q in:\n%s", want, all)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/scene`
Expected: FAIL (undefined `Card`, `Problems`, ...).

- [ ] **Step 3: Replace `internal/scene/scene.go`**

```go
// Package scene reads and validates a movie's scene file.
package scene

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
)

// Kind is what a scene shows.
type Kind string

const (
	Card   Kind = "card"
	Image  Kind = "image"
	Frames Kind = "frames"
	Movie  Kind = "movie"
)

var kinds = []Kind{Card, Image, Frames, Movie}

// File is a parsed, valid scene file.
type File struct {
	Path               string // the scene file
	Dir                string // paths in the file are relative to this
	Width, Height, FPS int
	Engine             string // auto, openai, openai-chat, or piper
	Voice              string // empty: the engine's default
	Scenes             []Scene
}

// Narrated reports whether any scene has narration.
func (f *File) Narrated() bool {
	return slices.ContainsFunc(f.Scenes, func(s Scene) bool { return s.Narration != "" })
}

// Scene is one segment of the movie.
type Scene struct {
	ID              string
	Kind            Kind
	Title, Subtitle string  // card
	Source          string  // absolute path: the image, frames directory, or movie
	Rate            float64 // frames per second of a frames scene
	Duration        float64 // minimum hold of a card or image scene, seconds
	Narration       string  // whitespace collapsed; empty when silent
}

// Problems lists everything wrong with a scene file.
type Problems []string

func (p Problems) Error() string { return "invalid scene file:\n  " + strings.Join(p, "\n  ") }

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func set(keys ...string) map[string]bool {
	m := map[string]bool{}
	for _, k := range keys {
		m[k] = true
	}
	return m
}

var (
	topKeys  = set("size", "fps", "engine", "voice", "scenes")
	kindKeys = map[Kind]map[string]bool{
		Card:   set("id", "card", "subtitle", "duration", "narration"),
		Image:  set("id", "image", "duration", "narration"),
		Frames: set("id", "frames", "rate", "narration"),
		Movie:  set("id", "movie"),
	}
)

// Load reads a scene file and reports every problem in it at once.
func Load(path string) (*File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, Problems{fmt.Sprintf("not valid YAML: %v", err)}
	}
	f := &File{Path: abs, Dir: filepath.Dir(abs), Width: 1920, Height: 1080, FPS: 30, Engine: "auto"}
	var p Problems
	for _, k := range sortedKeys(raw) {
		if !topKeys[k] {
			p = append(p, fmt.Sprintf("unknown top-level key %q", k))
		}
	}
	if v, ok := raw["size"]; ok {
		s, _ := v.(string)
		w, h, err := cli.ParseSize(s)
		switch {
		case err != nil:
			p = append(p, "size must be WxH, such as 1920x1080")
		case w%2 != 0 || h%2 != 0:
			p = append(p, "size must have even width and height (the encoder needs it)")
		default:
			f.Width, f.Height = w, h
		}
	}
	if v, ok := raw["fps"]; ok {
		if n, isInt := v.(int); isInt && n > 0 {
			f.FPS = n
		} else {
			p = append(p, "fps must be a positive whole number")
		}
	}
	if v, ok := raw["engine"]; ok {
		s, _ := v.(string)
		if slices.Contains([]string{"auto", "openai", "openai-chat", "piper"}, s) {
			f.Engine = s
		} else {
			p = append(p, "engine must be one of auto, openai, openai-chat, piper")
		}
	}
	if v, ok := raw["voice"]; ok {
		if s, isStr := v.(string); isStr && s != "" {
			f.Voice = s
		} else {
			p = append(p, "voice must be a voice name")
		}
	}
	list, _ := raw["scenes"].([]any)
	if len(list) == 0 {
		p = append(p, "scenes must be a list of at least one scene")
	}
	seen := map[string]bool{}
	for i, item := range list {
		sc, problems := parseScene(f, i, item)
		p = append(p, problems...)
		if sc.ID != "" {
			if seen[sc.ID] {
				p = append(p, fmt.Sprintf("scene %s: duplicate id", sc.ID))
			}
			seen[sc.ID] = true
		}
		f.Scenes = append(f.Scenes, sc)
	}
	if len(p) > 0 {
		return nil, p
	}
	return f, nil
}

func parseScene(f *File, index int, item any) (Scene, Problems) {
	name := fmt.Sprintf("scene %d", index+1)
	m, ok := item.(map[string]any)
	if !ok {
		return Scene{}, Problems{name + ": must be a mapping of fields"}
	}
	var p Problems
	var sc Scene
	if id, ok := m["id"].(string); ok && idPattern.MatchString(id) {
		sc.ID, name = id, "scene "+id
	} else {
		p = append(p, name+": id must match [a-z0-9][a-z0-9-]*")
	}
	var found []Kind
	for _, k := range kinds {
		if _, ok := m[string(k)]; ok {
			found = append(found, k)
		}
	}
	if len(found) != 1 {
		return sc, append(p, fmt.Sprintf("%s: needs exactly one of card, image, frames, movie (found %d)", name, len(found)))
	}
	sc.Kind = found[0]
	for _, k := range sortedKeys(m) {
		if !kindKeys[sc.Kind][k] {
			p = append(p, fmt.Sprintf("%s: %q is not a field of a %s scene", name, k, sc.Kind))
		}
	}
	text := func(key string) (string, bool) {
		v, ok := m[key]
		if !ok {
			return "", false
		}
		s, isStr := v.(string)
		if !isStr {
			p = append(p, fmt.Sprintf("%s: %s must be text", name, key))
		}
		return s, isStr
	}
	positive := func(key string, def float64) float64 {
		v, ok := m[key]
		if !ok {
			return def
		}
		n, isNum := number(v)
		if !isNum || !(n > 0) || math.IsInf(n, 0) {
			p = append(p, fmt.Sprintf("%s: %s must be a positive number", name, key))
			return def
		}
		return n
	}
	source := func(key string) string {
		s, ok := text(key)
		if !ok {
			return ""
		}
		if filepath.IsAbs(s) {
			return s
		}
		return filepath.Join(f.Dir, s)
	}
	switch sc.Kind {
	case Card:
		sc.Title, _ = text("card")
		sc.Subtitle, _ = text("subtitle")
		sc.Duration = positive("duration", 3)
	case Image:
		if sc.Source = source("image"); sc.Source != "" && !isFile(sc.Source) {
			p = append(p, fmt.Sprintf("%s: no such image %s", name, sc.Source))
		}
		sc.Duration = positive("duration", 3)
	case Frames:
		if sc.Source = source("frames"); sc.Source != "" && len(PNGs(sc.Source)) == 0 {
			p = append(p, fmt.Sprintf("%s: no PNG frames in %s", name, sc.Source))
		}
		sc.Rate = positive("rate", float64(f.FPS))
	case Movie:
		if sc.Source = source("movie"); sc.Source != "" && !isFile(sc.Source) {
			p = append(p, fmt.Sprintf("%s: no such movie %s", name, sc.Source))
		}
	}
	if sc.Kind != Movie {
		if n, ok := text("narration"); ok {
			sc.Narration = strings.Join(strings.Fields(n), " ")
		}
	}
	return sc, p
}

// PNGs lists the PNG files in dir in lexical order. It reads the directory
// rather than globbing, so brackets or stars in the path mean nothing.
func PNGs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".png") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	slices.Sort(paths)
	return paths
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
```

- [ ] **Step 4: Make `build` compile against the new scene types**

In `internal/build/build.go`, the loop body is unchanged except that it must only handle `scene.Image` for now; add at the top of the loop:
```go
		if sc.Kind != scene.Image {
			return 0, fmt.Errorf("scene %s: %s scenes arrive in the next task", sc.ID, sc.Kind)
		}
```
(Task 9 replaces this loop.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/scene internal/build
git commit -m "Validate the whole scene file up front and report every problem at once"
```

---

### Task 9: Frames and movie scenes, and awkward paths everywhere

**Files:**
- Modify: `internal/build/segment.go` (add `segment`, `encodeMovie`), `internal/build/build.go` (replace the scene loop), `cmd/movie/build_test.go`

**Interfaces:**
- Produces: `segment(scratch string, f *scene.File, sc scene.Scene, wav string, speech float64) (float64, error)` (Task 10 adds cards to it; Task 12 passes narration).

- [ ] **Step 1: Write the failing tests**

Append to `cmd/movie/build_test.go`:
```go
// awkward is a directory name that breaks every naive ffmpeg path handling.
const awkward = "awk %d [x] 'q' λ & more"

func TestBuildHandlesEverySceneKindAtAwkwardPaths(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := filepath.Join(t.TempDir(), awkward)
	still(t, dir, "media/shot %03d.png", "red")
	for i, c := range []string{"red", "green", "blue", "white", "black"} {
		still(t, dir, filepath.Join("media", "frames [1]", fmt.Sprintf("f%02d.png", i)), c)
	}
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=2",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "media/with sound.mp4")
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=1.5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "media/silent 100%.mp4")
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
scenes:
  - id: shot
    image: "media/shot %03d.png"
    duration: 1
  - id: run
    frames: "media/frames [1]"
    rate: 2.5
  - id: loud
    movie: "media/with sound.mp4"
  - id: quiet
    movie: "media/silent 100%.mp4"
`)
	r := runMovie(t, dir, "build", "demo.yaml", "out/final cut.mp4")
	if r.code != 0 {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	// 1 + 5/2.5 + 2 + 1.5
	assertNear(t, "movie duration", testmedia.Duration(t, filepath.Join(dir, "out", "final cut.mp4")), 6.5, 0.3)
	for _, line := range []string{"shot: 1.0s", "run: 2.0s", "loud: 2.0s", "quiet: 1.5s"} {
		if !strings.Contains(r.stdout, line) {
			t.Errorf("missing %q in\n%s", line, r.stdout)
		}
	}
}

func TestBuildRejectsAnInvalidSceneFileBeforeEncoding(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	writeFile(t, dir, "demo.yaml", "scenes:\n  - id: x\n    image: nope.png\n")
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 2 || !strings.Contains(r.stderr, "no such image") {
		t.Fatalf("code %d\n%s", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo.build")); err == nil {
		t.Fatal("scratch was created for an invalid scene file")
	}
}
```
Add `"fmt"` to the imports; `still` already creates parent directories.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/movie -run Build`
Expected: FAIL (`frames scenes arrive in the next task`).

- [ ] **Step 3: Add `segment` and `encodeMovie` to `internal/build/segment.go`**

Add these imports: `"path/filepath"`. Then append:
```go
// segment encodes one scene into scratch/<id>.mp4 and returns its measured
// duration. wav and speech are the scene's narration clip and its length, or
// "" and 0. A still or frames scene lasts max(narration, visuals).
func segment(scratch string, f *scene.File, sc scene.Scene, wav string, speech float64) (float64, error) {
	name := sc.ID + ".mp4"
	var err error
	switch sc.Kind {
	case scene.Movie:
		err = encodeMovie(scratch, name, f, sc.Source)
	case scene.Frames:
		frames := scene.PNGs(sc.Source)
		pngs := streamFiles(frames)
		err = encodeStill(scratch, name, f, pngs, sc.Rate, len(frames), max(speech, float64(len(frames))/sc.Rate), wav)
		pngs.Close()
	case scene.Image:
		pngs := streamFiles([]string{sc.Source})
		err = encodeStill(scratch, name, f, pngs, float64(f.FPS), 1, max(speech, sc.Duration), wav)
		pngs.Close()
	default:
		err = fmt.Errorf("%s scenes arrive in the next task", sc.Kind)
	}
	if err != nil {
		return 0, fmt.Errorf("scene %s: %w", sc.ID, err)
	}
	info, err := ffmpeg.Probe(filepath.Join(scratch, name))
	if err != nil {
		return 0, err
	}
	return info.Duration, nil
}

// encodeMovie plays an existing movie as itself, scaled to fit, with its own
// audio, or silence when it has none (concat needs every segment to have an
// audio stream).
func encodeMovie(scratch, name string, f *scene.File, src string) error {
	info, err := ffmpeg.Probe(src)
	if err != nil {
		return err
	}
	args := []string{"-i", src}
	audio := "0:a:0"
	if !info.Has("audio") {
		args = append(args, "-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo")
		audio = "1:a:0"
	}
	args = append(args, "-vf", fit(f.Width, f.Height), "-af", "apad", "-r", strconv.Itoa(f.FPS),
		"-t", fmt.Sprintf("%.3f", info.Duration), "-map", "0:v:0", "-map", audio)
	args = append(append(args, encodeArgs...), name)
	return ffmpeg.Run(scratch, nil, args...)
}
```

- [ ] **Step 4: Replace the scene loop in `internal/build/build.go`**

Replace everything in `Run` from `var names []string` through the end of the `for` loop with:
```go
	var names []string
	for _, sc := range f.Scenes {
		d, err := segment(scratch, f, sc, "", 0)
		if err != nil {
			return 0, err
		}
		fmt.Fprintf(stdout, "%s: %.1fs\n", sc.ID, d)
		names = append(names, sc.ID+".mp4")
	}
```
Also: build `out`'s parent directory before concat: add `if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil { return 0, err }` right after computing `scratch`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/build cmd/movie
git commit -m "Build frames and movie scenes, with every path kept out of ffmpeg's syntax"
```

---

### Task 10: Title cards drawn in Go

**Files:**
- Create: `internal/build/card.go`, `internal/build/card_test.go`
- Modify: `internal/build/segment.go` (card case), `internal/scene/scene.go` (card font coverage), `internal/scene/scene_test.go`

**Interfaces:**
- Produces: `build.Card(title, subtitle string, w, h int) ([]byte, error)`; `fitBlock(text string, size float64, width int) cardBlock`.

- [ ] **Step 1: Write the failing tests**

`internal/build/card_test.go`:
```go
package build

import (
	"bytes"
	"image/png"
	"testing"

	"golang.org/x/image/font"
)

func TestLongUnbreakableTitleShrinksToFit(t *testing.T) {
	b := fitBlock("github.com/prime-radiant-inc/proving-it-works", 77, 1613)
	for _, line := range b.lines {
		if w := font.MeasureString(b.face, line).Ceil(); w > 1613 {
			t.Fatalf("line %q is %d px wide", line, w)
		}
	}
	if b.size >= 77 {
		t.Fatalf("size did not shrink: %v", b.size)
	}
}

func TestShortTitleKeepsItsSize(t *testing.T) {
	if b := fitBlock("proving it works", 77, 1613); b.size != 77 || len(b.lines) != 1 {
		t.Fatalf("%+v", b)
	}
}

func TestCardDrawsTextOnTheBackground(t *testing.T) {
	data, err := Card("proving-it-works", "a subtitle", 640, 360)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 640 || img.Bounds().Dy() != 360 {
		t.Fatalf("size %v", img.Bounds())
	}
	lit := 0
	for y := 120; y < 240; y++ {
		for x := 0; x < 640; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r>>8 > 0x60 {
				lit++
			}
		}
	}
	if lit < 200 {
		t.Fatalf("only %d text pixels in the middle band", lit)
	}
}
```

Append to `internal/scene/scene_test.go`:
```go
func TestCardTextTheFontCannotDrawIsRejected(t *testing.T) {
	_, err := Load(fixture(t, "scenes:\n  - id: t\n    card: 漢字 works\n"))
	var p Problems
	if !errors.As(err, &p) || !strings.Contains(strings.Join(p, "\n"), "漢 (U+6F22)") {
		t.Fatalf("got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/build ./internal/scene`
Expected: FAIL (undefined `fitBlock`, `Card`; the CJK card is accepted).

- [ ] **Step 3: Write `internal/build/card.go`**

```go
package build

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/prime-radiant-inc/proving-it-works/internal/fonts"
)

var (
	cardBackground = color.RGBA{0x10, 0x10, 0x14, 0xff}
	cardTitle      = color.RGBA{0xf2, 0xf2, 0xf5, 0xff}
	cardSubtitle   = color.RGBA{0x9a, 0x9a, 0xa6, 0xff}
)

// cardBlock is the title or subtitle, wrapped and sized to fit.
type cardBlock struct {
	lines []string
	size  float64
	face  font.Face
}

// fitBlock wraps text at spaces into lines no wider than width, shrinking
// the size until every line fits: a long URL cannot be wrapped, only shrunk.
func fitBlock(text string, size float64, width int) cardBlock {
	for {
		face := fonts.Face(fonts.Sans(), size)
		lines := wrapToWidth(face, text, width)
		widest := 0
		for _, l := range lines {
			widest = max(widest, font.MeasureString(face, l).Ceil())
		}
		if widest <= width || size <= 8 {
			return cardBlock{lines: lines, size: size, face: face}
		}
		size *= 0.9
	}
}

func wrapToWidth(face font.Face, text string, width int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(text) {
		try := strings.TrimSpace(cur + " " + w)
		if cur != "" && font.MeasureString(face, try).Ceil() > width {
			lines = append(lines, cur)
			cur = w
		} else {
			cur = try
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// Card draws a title card: title and subtitle centered on the dark
// background. Drawing it here means build needs no browser.
func Card(title, subtitle string, w, h int) ([]byte, error) {
	width := int(float64(w) * 0.84)
	blocks := []struct {
		cardBlock
		colour color.Color
	}{
		{fitBlock(title, float64(max(28, h/14)), width), cardTitle},
		{fitBlock(subtitle, float64(max(16, h/32)), width), cardSubtitle},
	}
	gap := max(16, h/44)
	total, used := 0, 0
	for _, b := range blocks {
		if len(b.lines) > 0 {
			total += len(b.lines) * b.face.Metrics().Height.Ceil()
			used++
		}
	}
	total += gap * max(used-1, 0)

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(cardBackground), image.Point{}, draw.Src)
	y := (h - total) / 2
	for _, b := range blocks {
		if len(b.lines) == 0 {
			continue
		}
		m := b.face.Metrics()
		for _, line := range b.lines {
			lw := font.MeasureString(b.face, line).Ceil()
			d := font.Drawer{Dst: img, Src: image.NewUniform(b.colour), Face: b.face,
				Dot: fixed.P((w-lw)/2, y+m.Ascent.Ceil())}
			d.DrawString(line)
			y += m.Height.Ceil()
		}
		y += gap
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 4: Render cards in `segment`**

In `internal/build/segment.go`, add `"bytes"` to the imports and replace the `default:` case of `segment` with:
```go
	case scene.Card:
		var png []byte
		if png, err = Card(sc.Title, sc.Subtitle, f.Width, f.Height); err == nil {
			err = encodeStill(scratch, name, f, bytes.NewReader(png), float64(f.FPS), 1, max(speech, sc.Duration), wav)
		}
```

- [ ] **Step 5: Reject card text the font cannot draw**

In `internal/scene/scene.go`, import `"golang.org/x/image/font/opentype"` and `"github.com/prime-radiant-inc/proving-it-works/internal/fonts"`, and in `parseScene`'s `case Card:` add after reading the title and subtitle:
```go
		if missing := fonts.Missing([]*opentype.Font{fonts.Sans()}, sc.Title+" "+sc.Subtitle); len(missing) > 0 {
			p = append(p, fmt.Sprintf("%s: the card font cannot draw %s", name, fonts.Describe(missing)))
		}
```

- [ ] **Step 6: Add a card to the black-box build test**

In `cmd/movie/build_test.go`, in `TestBuildHandlesEverySceneKindAtAwkwardPaths`, add a first scene to the YAML:
```yaml
  - id: title
    card: proving-it-works
    subtitle: every kind of scene
    duration: 1
```
and change the expected duration to `7.5` and add `"title: 1.0s"` to the expected lines.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 8: Look at a card**

```bash
go build -o /tmp/movie ./cmd/movie && d=$(mktemp -d)
printf 'size: 1920x1080\nscenes:\n  - id: t\n    card: github.com/prime-radiant-inc/proving-it-works\n    subtitle: a movie is evidence\n' > "$d/s.yaml"
/tmp/movie build "$d/s.yaml" "$d/s.mp4"
ffmpeg -v error -i "$d/s.mp4" -frames:v 1 "$d/card.png" && open "$d/card.png" 2>/dev/null || xdg-open "$d/card.png"
```
Expected: the build fails the check ("never reaches a new state"; a lone card is a still) but writes the movie; the card shows the URL on one line, fully inside the frame, with the subtitle below. Look at it before continuing.

- [ ] **Step 9: Commit**

```bash
git add internal/build internal/scene cmd/movie
git commit -m "Draw title cards in Go, shrinking to fit and refusing text the font cannot draw"
```

---

### Task 11: The `openai-chat` gate

**Files:**
- Create: `internal/narrate/gate.go`, `internal/narrate/gate_test.go`

**Interfaces:**
- Produces: `narrate.ChatAccepts(script, transcript string) (bool, string)`.

- [ ] **Step 1: Write the failing tests**

`internal/narrate/gate_test.go`:
```go
package narrate

import (
	"fmt"
	"strings"
	"testing"
)

const script = "This is a container with nothing of ours in it. Claude Code is here, " +
	"and no plugins. We add the marketplace straight from the public repo."

func words50() []string {
	w := make([]string, 50)
	for i := range w {
		w[i] = fmt.Sprintf("word%d", i)
	}
	return w
}

func TestChatGate(t *testing.T) {
	w := words50()
	oneWrong := append([]string{}, w...)
	oneWrong[10] = "other"
	for _, c := range []struct {
		name, script, transcript string
		ok                       bool
	}{
		{"exact", script, script, true},
		{"case and punctuation differ", "Two words.", "two words", true},
		{"one mismatched word in fifty", strings.Join(w, " "), strings.Join(oneWrong, " "), true},
		{"accented Latin and Cyrillic across lines", "Café déjà vu\nПривет мир", "café déjà vu привет мир", true},
		{"spoken preamble", script, "Sure thing: " + script, false},
		{"one-word preamble", script, "Okay. " + script, false},
		{"dropped clause", script, strings.Replace(script, " and no plugins.", "", 1), false},
		{"empty transcript", script, "", false},
		{"punctuation only", script, "...!?", false},
		{"script without word boundaries", "漢字のテスト", "漢字のテスト", false},
	} {
		ok, why := ChatAccepts(c.script, c.transcript)
		if ok != c.ok {
			t.Errorf("%s: got %v (%s), want %v", c.name, ok, why, c.ok)
		}
		if !ok && why == "" {
			t.Errorf("%s: rejected without a reason", c.name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go get golang.org/x/text
go test ./internal/narrate
```
Expected: FAIL (undefined `ChatAccepts`).

- [ ] **Step 3: Write `internal/narrate/gate.go`**

```go
// Package narrate renders narration clips and accepts only clips that pass
// their engine's gate.
package narrate

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// segmentationScripts are written without spaces between words, so a
// word-by-word comparison says nothing about them.
var segmentationScripts = []struct{ lo, hi rune }{
	{0x3040, 0x30FF},   // Hiragana and Katakana
	{0x3400, 0x4DBF},   // CJK Extension A
	{0x4E00, 0x9FFF},   // CJK Unified Ideographs
	{0xF900, 0xFAFF},   // CJK Compatibility Ideographs
	{0x20000, 0x323AF}, // CJK Unified Ideograph extensions B through I
	{0x2F800, 0x2FA1F}, // CJK Compatibility Ideographs Supplement
	{0x0E00, 0x0E7F},   // Thai
}

func needsSegmentation(text string) bool {
	for _, r := range norm.NFKC.String(text) {
		for _, s := range segmentationScripts {
			if r >= s.lo && r <= s.hi {
				return true
			}
		}
	}
	return false
}

var folder = cases.Fold()

// words normalizes text for comparison: NFKC, case-folded, split on
// whitespace, keeping only letters, numbers, and marks.
func words(text string) []string {
	var out []string
	for _, token := range strings.Fields(folder.String(norm.NFKC.String(text))) {
		kept := strings.Map(func(r rune) rune {
			if unicode.In(r, unicode.Letter, unicode.Number, unicode.Mark) {
				return r
			}
			return -1
		}, token)
		if kept != "" {
			out = append(out, kept)
		}
	}
	return out
}

// ChatAccepts reports whether a chat model's own transcript says it read the
// script verbatim, and why not when it did not. The transcript is the
// engine's text rather than a noisy recognizer's, so the comparison is
// strict: the length difference plus word-by-word mismatches at the same
// position may not exceed max(2, words/25). One spoken word of preamble
// shifts every word after it, so it cannot pass.
func ChatAccepts(script, transcript string) (bool, string) {
	if needsSegmentation(script) || needsSegmentation(transcript) {
		return false, "this script cannot be compared word by word; use engine openai or piper"
	}
	want, got := words(script), words(transcript)
	if len(want) == 0 {
		return false, "the script has no words to compare"
	}
	if len(got) == 0 {
		return false, "the transcript contains no speech"
	}
	drift := max(len(want)-len(got), len(got)-len(want))
	for i := range min(len(want), len(got)) {
		if want[i] != got[i] {
			drift++
		}
	}
	if limit := max(2, len(want)/25); drift > limit {
		return false, fmt.Sprintf("the model ad-libbed (drift %d, limit %d)", drift, limit)
	}
	return true, ""
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/narrate`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/narrate
git commit -m "Port the openai-chat verbatim gate exactly, as a pure function"
```

---

### Task 12: Narration engines, the clip cache, and narrated segments

**Files:**
- Create: `internal/narrate/engine.go`, `internal/narrate/openai.go`, `internal/narrate/piper.go`, `internal/narrate/cache.go`, `internal/narrate/cache_test.go`, `internal/build/narration.go`
- Modify: `internal/build/build.go`, `cmd/movie/build_test.go`, `.github/workflows/test.yml`

**Interfaces:**
- Produces: `narrate.Engine` interface (`Name() string; Model(voice string) string; DefaultVoice() string; Ready(voice string) error; Synthesize(text, voice, wav string) (string, error)`); `narrate.Resolve(name string) (Engine, error)`; `narrate.Clip(dir string, e Engine, voice, text string, log io.Writer) (string, error)`; `narrate.RejectedError`; in `internal/build`: `type clip struct{ wav string; seconds float64 }`, `narrateAll(f *scene.File, scratch string, stdout io.Writer) (map[string]clip, error)`.

- [ ] **Step 1: Write the failing unit tests**

`internal/narrate/cache_test.go`:
```go
package narrate

import "testing"

func TestClipNameChangesWithEveryInput(t *testing.T) {
	base := clipName("piper", "en_US-lessac-medium", "en_US-lessac-medium", "hello")
	for _, other := range []string{
		clipName("openai", "en_US-lessac-medium", "en_US-lessac-medium", "hello"),
		clipName("piper", "other", "en_US-lessac-medium", "hello"),
		clipName("piper", "en_US-lessac-medium", "other", "hello"),
		clipName("piper", "en_US-lessac-medium", "en_US-lessac-medium", "hello!"),
	} {
		if other == base {
			t.Fatal("a changed input kept the same clip name")
		}
	}
	if base != clipName("piper", "en_US-lessac-medium", "en_US-lessac-medium", "hello") {
		t.Fatal("clip name is not stable")
	}
}

func TestAutoWithoutAKeyPicksPiper(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", t.TempDir()) // no llm on PATH
	e, err := Resolve("auto")
	if err != nil || e.Name() != "piper" || e.DefaultVoice() != "en_US-lessac-medium" {
		t.Fatalf("%v %v", e, err)
	}
}

func TestExplicitOpenAIWithoutAKeyIsAnError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", t.TempDir())
	if _, err := Resolve("openai-chat"); err == nil {
		t.Fatal("accepted openai-chat without a key")
	}
}

func TestAutoWithAKeyPicksOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	e, err := Resolve("auto")
	if err != nil || e.Name() != "openai" || e.DefaultVoice() != "nova" {
		t.Fatalf("%v %v", e, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/narrate`
Expected: FAIL (undefined `clipName`, `Resolve`).

- [ ] **Step 3: Write the engines**

`internal/narrate/engine.go`:
```go
package narrate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Engine synthesizes text to a WAV file. A chat engine also returns what it
// says it said.
type Engine interface {
	Name() string
	Model(voice string) string
	DefaultVoice() string
	// Ready reports a missing prerequisite before any work starts.
	Ready(voice string) error
	Synthesize(text, voice, wav string) (transcript string, err error)
}

// Resolve picks the engine: auto means openai when a key exists, else piper.
func Resolve(name string) (Engine, error) {
	key := openAIKey()
	switch name {
	case "auto":
		if key != "" {
			return openAI{key: key}, nil
		}
		return piper{}, nil
	case "openai", "openai-chat":
		if key == "" {
			return nil, errors.New("no OPENAI_API_KEY (and `llm keys get openai` found nothing); " +
				"use engine: piper for a local voice")
		}
		return openAI{key: key, chat: name == "openai-chat"}, nil
	case "piper":
		return piper{}, nil
	}
	return nil, errors.New("unknown engine " + name)
}

// openAIKey reads OPENAI_API_KEY, else asks the llm CLI, whose absence is normal.
func openAIKey() string {
	if k := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); k != "" {
		return k
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "llm", "keys", "get", "openai").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

`internal/narrate/openai.go`:
```go
package narrate

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	ttsModel  = "gpt-4o-mini-tts" // reads exactly what it is sent
	chatModel = "gpt-audio-1.5"   // better prosody, will ad-lib; gated
)

type openAI struct {
	key  string
	chat bool
}

func (o openAI) Name() string {
	if o.chat {
		return "openai-chat"
	}
	return "openai"
}

func (o openAI) Model(string) string {
	if o.chat {
		return chatModel
	}
	return ttsModel
}

func (openAI) DefaultVoice() string { return "nova" }

func (openAI) Ready(string) error { return nil }

func (o openAI) Synthesize(text, voice, wav string) (string, error) {
	if !o.chat {
		audio, err := o.post("https://api.openai.com/v1/audio/speech", map[string]any{
			"model": ttsModel, "voice": voice, "input": text, "response_format": "wav"})
		if err != nil {
			return "", err
		}
		return "", os.WriteFile(wav, audio, 0o644)
	}
	body, err := o.post("https://api.openai.com/v1/chat/completions", map[string]any{
		"model":      chatModel,
		"modalities": []string{"text", "audio"},
		"audio":      map[string]string{"voice": voice, "format": "wav"},
		"messages": []map[string]string{{"role": "user", "content": "Read this narration aloud, " +
			"warm and clear, verbatim, and say nothing else:\n\n" + text}},
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Audio struct {
					Data       string `json:"data"`
					Transcript string `json:"transcript"`
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("openai-chat response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("openai-chat returned no choices")
	}
	a := resp.Choices[0].Message.Audio
	audio, err := base64.StdEncoding.DecodeString(a.Data)
	if err != nil {
		return "", fmt.Errorf("openai-chat audio: %w", err)
	}
	return a.Transcript, os.WriteFile(wav, audio, 0o644)
}

func (o openAI) post(url string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 180 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s: %.300s", url, resp.Status, out)
	}
	return out, nil
}
```

`internal/narrate/piper.go`:
```go
package narrate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// piper is the local voice: free, offline, and the default without a key.
type piper struct{}

func (piper) Name() string              { return "piper" }
func (piper) Model(voice string) string { return voice }
func (piper) DefaultVoice() string      { return "en_US-lessac-medium" }

// voiceDir is where Piper voices live: PIPER_VOICE_DIR, else ~/.cache/piper-voices.
func voiceDir() string {
	if d := os.Getenv("PIPER_VOICE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "piper-voices")
}

func (piper) Ready(voice string) error {
	if _, err := exec.LookPath("piper"); err != nil {
		return fmt.Errorf("piper not on PATH: install it once with: uv tool install piper-tts")
	}
	dir := voiceDir()
	if _, err := os.Stat(filepath.Join(dir, voice+".onnx")); err != nil {
		return fmt.Errorf("piper voice %s is not in %s: download it once with: "+
			"uvx --from piper-tts python -m piper.download_voices --data-dir %q %s", voice, dir, dir, voice)
	}
	return nil
}

// Synthesize passes the text in a UTF-8 file: as arguments, words that look
// like flags would be misparsed, and stdin is decoded in the ANSI code page
// on Windows.
func (piper) Synthesize(text, voice, wav string) (string, error) {
	input := wav + ".txt"
	if err := os.WriteFile(input, []byte(text), 0o644); err != nil {
		return "", err
	}
	defer os.Remove(input)
	out, err := exec.Command("piper", "-m", voice, "--data-dir", voiceDir(), "-i", input, "-f", wav).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("piper: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return "", nil
}
```

`internal/narrate/cache.go`:
```go
package narrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// RejectedError means every attempt at a clip failed its gate or synthesis.
type RejectedError struct{ Reasons []string }

func (e *RejectedError) Error() string {
	return "narration rejected:\n    " + strings.Join(e.Reasons, "\n    ")
}

// clipName identifies a clip by everything that shapes its audio.
func clipName(engine, voice, model, text string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{engine, voice, model, text}, "\x00")))
	return hex.EncodeToString(sum[:8]) + ".wav"
}

// Clip returns an accepted clip of text in dir, reusing one that exists. A
// clip file exists only once accepted: attempts are written to a temporary
// name and renamed into place after passing the gate.
func Clip(dir string, e Engine, voice, text string, log io.Writer) (string, error) {
	path := filepath.Join(dir, clipName(e.Name(), voice, e.Model(voice), text))
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(log, "  cached   %s\n", path)
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var reasons []string
	for attempt := 1; attempt <= 2; attempt++ {
		tmp, err := os.CreateTemp(dir, ".attempt-*.wav")
		if err != nil {
			return "", err
		}
		tmp.Close()
		transcript, err := e.Synthesize(text, voice, tmp.Name())
		if err == nil && e.Name() == "openai-chat" {
			if ok, why := ChatAccepts(text, transcript); !ok {
				err = fmt.Errorf("%s; it said: %q", why, transcript)
			}
		}
		if err != nil {
			os.Remove(tmp.Name())
			reasons = append(reasons, fmt.Sprintf("attempt %d: %v", attempt, err))
			continue
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			return "", err
		}
		fmt.Fprintf(log, "  rendered %s\n", path)
		return path, nil
	}
	return "", &RejectedError{Reasons: reasons}
}
```

- [ ] **Step 4: Run the unit tests to verify they pass**

Run: `go test ./internal/narrate`
Expected: PASS.

- [ ] **Step 5: Write the failing narrated-build tests**

Append to `cmd/movie/build_test.go`:
```go
// requirePiper skips unless the keyless voice is installed.
func requirePiper(t *testing.T) {
	t.Helper()
	testmedia.Require(t, "ffmpeg", "ffprobe", "piper")
	dir := os.Getenv("PIPER_VOICE_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache", "piper-voices")
	}
	if _, err := os.Stat(filepath.Join(dir, "en_US-lessac-medium.onnx")); err != nil {
		t.Skipf("needs the piper voice en_US-lessac-medium in %s", dir)
	}
}

func TestNarratedCardLastsAsLongAsItsClipAndReusesIt(t *testing.T) {
	requirePiper(t)
	dir := t.TempDir()
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
engine: piper
scenes:
  - id: title
    card: proving it works
    duration: 1
    narration: This card is narrated by a local voice, and it lasts as long as the words do.
  - id: end
    card: the end
    duration: 1
`)
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if !strings.Contains(r.stdout, "rendered") {
		t.Fatalf("no clip rendered:\n%s%s", r.stdout, r.stderr)
	}
	clips, _ := filepath.Glob(filepath.Join(dir, "demo.build", "narration", "*.wav"))
	if len(clips) != 1 {
		t.Fatalf("clips: %v", clips)
	}
	speech := testmedia.Duration(t, clips[0])
	segment := testmedia.Duration(t, filepath.Join(dir, "demo.build", "title.mp4"))
	if speech < 2 || segment < speech-0.05 {
		t.Fatalf("clip %.2fs, segment %.2fs", speech, segment)
	}
	again := runMovie(t, dir, "build", "demo.yaml", "demo2.mp4")
	if !strings.Contains(again.stdout, "cached") {
		t.Fatalf("second build did not reuse the clip:\n%s", again.stdout)
	}
}

func TestMissingPiperVoiceIsAnEnvironmentErrorNamingTheFix(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe", "piper")
	dir := t.TempDir()
	t.Setenv("PIPER_VOICE_DIR", t.TempDir())
	writeFile(t, dir, "demo.yaml", "engine: piper\nscenes:\n  - id: a\n    card: x\n    narration: hello\n")
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 2 || !strings.Contains(r.stderr, "piper.download_voices") {
		t.Fatalf("code %d\n%s", r.code, r.stderr)
	}
}

func TestOpenAIVoiceNarrates(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("needs OPENAI_API_KEY")
	}
	testmedia.Require(t, "ffmpeg", "ffprobe")
	for _, engine := range []string{"openai", "openai-chat"} {
		dir := t.TempDir()
		writeFile(t, dir, "demo.yaml", "size: 320x180\nfps: 10\nengine: "+engine+
			"\nscenes:\n  - id: a\n    card: x\n    narration: Proving it works, out loud.\n")
		r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
		if !strings.Contains(r.stdout, "rendered") {
			t.Errorf("%s:\n%s%s", engine, r.stdout, r.stderr)
		}
	}
}
```

(The first build's check result is not asserted here: subtitles arrive in Task 13, and a narrated movie without them fails the check by design.)

- [ ] **Step 6: Wire narration into build**

`internal/build/narration.go`:
```go
package build

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/narrate"
	"github.com/prime-radiant-inc/proving-it-works/internal/scene"
)

// clip is an accepted narration clip and its measured length.
type clip struct {
	wav     string
	seconds float64
}

// narrateAll renders or reuses a clip for every narrated scene.
func narrateAll(f *scene.File, scratch string, stdout io.Writer) (map[string]clip, error) {
	clips := map[string]clip{}
	if !f.Narrated() {
		return clips, nil
	}
	e, err := narrate.Resolve(f.Engine)
	if err != nil {
		return nil, err
	}
	voice := f.Voice
	if voice == "" {
		voice = e.DefaultVoice()
	}
	if err := e.Ready(voice); err != nil {
		return nil, err
	}
	fmt.Fprintf(stdout, "narration: %s, voice %s\n", e.Name(), voice)
	dir := filepath.Join(scratch, "narration")
	for _, sc := range f.Scenes {
		if sc.Narration == "" {
			continue
		}
		fmt.Fprintf(stdout, "%s:\n", sc.ID)
		wav, err := narrate.Clip(dir, e, voice, sc.Narration, stdout)
		if err != nil {
			return nil, fmt.Errorf("scene %s: %w", sc.ID, err)
		}
		info, err := ffmpeg.Probe(wav)
		if err != nil {
			return nil, err
		}
		clips[sc.ID] = clip{wav: wav, seconds: info.Duration}
	}
	if e.Name() == "piper" {
		fmt.Fprintln(stdout, "local voice: it mispronounces unusual names rather than dropping them - "+
			"listen to one clip before you commit to a voice.")
	}
	return clips, nil
}
```

In `internal/build/build.go`: add imports `"errors"`, `"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"`, `"github.com/prime-radiant-inc/proving-it-works/internal/narrate"`. After creating `scratch` and `out`'s directory, add:
```go
	clips, err := narrateAll(f, scratch, stdout)
	if err != nil {
		var rejected *narrate.RejectedError
		if errors.As(err, &rejected) {
			return exitcode.Verdict, err
		}
		return 0, err
	}
```
and change the segment call to:
```go
		c := clips[sc.ID]
		d, err := segment(scratch, f, sc, c.wav, c.seconds)
```

- [ ] **Step 7: Install Piper in CI's Linux job**

In `.github/workflows/test.yml`, add after the Linux ffmpeg step:
```yaml
      - name: Install Piper and its default voice (Linux)
        if: runner.os == 'Linux'
        run: |
          curl -LsSf https://astral.sh/uv/install.sh | sh
          export PATH="$HOME/.local/bin:$PATH"
          uv tool install piper-tts
          uvx --from piper-tts python -m piper.download_voices --data-dir "$HOME/.cache/piper-voices" en_US-lessac-medium
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
          piper --help > /dev/null
```

- [ ] **Step 8: Run all tests to verify they pass**

Run: `go test ./...`
Expected: PASS; Piper tests run if Piper and the voice are installed locally, otherwise SKIP naming what is missing. Install Piper locally (`uv tool install piper-tts` and the voice command above) and run again so the narrated test actually runs before you commit.

- [ ] **Step 9: Commit**

```bash
git add internal/narrate internal/build cmd/movie .github/workflows/test.yml
git commit -m "Narrate with openai, openai-chat, or a local Piper voice, caching accepted clips"
```

---

### Task 13: Subtitles, burning, and build's own check expectations

**Files:**
- Create: `internal/srt/cues.go`, `internal/srt/cues_test.go`, `internal/build/subtitles.go`, `internal/build/burn_test.go`
- Modify: `internal/build/build.go`, `cmd/movie/build_test.go`

**Interfaces:**
- Produces: `srt.Cue{Start, End float64; Text string}`, `srt.SceneCues(text string, start, duration float64, maxChars int, maxSecs float64) ([]srt.Cue, error)`, `srt.Write(w io.Writer, cues []srt.Cue) error`, `srt.Timestamp(seconds float64) string`; in `internal/build`: `writeSubtitles(path string, f *scene.File, clips map[string]clip, offsets map[string]float64) (float64, error)`, `burn(scratch, cut, srtPath, out string, stdout io.Writer) (bool, error)`.

- [ ] **Step 1: Write the failing tests**

`internal/srt/cues_test.go`:
```go
package srt

import (
	"math"
	"strings"
	"testing"
)

type ms struct {
	start, end int
	text       string
}

func cuesMS(t *testing.T, text string, start, duration float64, maxChars int, maxSecs float64) []ms {
	t.Helper()
	cues, err := SceneCues(text, start, duration, maxChars, maxSecs)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]ms, len(cues))
	for i, c := range cues {
		out[i] = ms{int(math.Round(c.Start * 1000)), int(math.Round(c.End * 1000)), strings.Join(strings.Fields(c.Text), " ")}
	}
	return out
}

// assertScene: cues tile [start, end] exactly, in order, keeping every word.
func assertScene(t *testing.T, cues []ms, start, end int, text string) {
	t.Helper()
	if len(cues) == 0 || cues[0].start != start || cues[len(cues)-1].end != end {
		t.Fatalf("cues %v do not span %d..%d", cues, start, end)
	}
	previous := start
	var words []string
	for _, c := range cues {
		if c.start != previous || c.start >= c.end || c.end > end {
			t.Fatalf("cues %v are not contiguous and positive", cues)
		}
		previous = c.end
		words = append(words, c.text)
	}
	if strings.Join(words, " ") != text {
		t.Fatalf("words %q, want %q", strings.Join(words, " "), text)
	}
}

func TestFiveChunksFitHalfASecond(t *testing.T) {
	assertScene(t, cuesMS(t, "one two six ten red", 0, 0.5, 3, 5.5), 0, 500, "one two six ten red")
}

func TestOneWordCoversATwelveSecondScene(t *testing.T) {
	if got := cuesMS(t, "Held", 0, 12, 84, 5.5); len(got) != 1 || got[0] != (ms{0, 12000, "Held"}) {
		t.Fatal(got)
	}
}

func TestMixedChunksGetProportionalTime(t *testing.T) {
	got := cuesMS(t, "a bbbbbbbbb", 0, 1, 9, 5.5)
	if len(got) != 2 || got[0] != (ms{0, 100, "a"}) || got[1] != (ms{100, 1000, "bbbbbbbbb"}) {
		t.Fatal(got)
	}
}

func TestMaxSecondsSplitsWithoutLosingWords(t *testing.T) {
	text := "one two six ten red cat dog fox"
	got := cuesMS(t, text, 0, 12, 84, 3)
	assertScene(t, got, 0, 12000, text)
	for _, c := range got {
		if c.end-c.start > 3000 || len(got) < 2 {
			t.Fatal(got)
		}
	}
}

func TestMaxSecondsRefinesUnequalChunks(t *testing.T) {
	got := cuesMS(t, "ab cde f ghi", 0, 6, 84, 3)
	assertScene(t, got, 0, 6000, "ab cde f ghi")
	for _, c := range got {
		if c.end-c.start > 3000 {
			t.Fatal(got)
		}
	}
}

func TestChunksCoalesceToRepresentableMilliseconds(t *testing.T) {
	got := cuesMS(t, "one two six ten red", 0, 0.002, 3, 5.5)
	assertScene(t, got, 0, 2, "one two six ten red")
	if len(got) > 2 {
		t.Fatal(got)
	}
}

func TestUnrepresentableMaxSecondsKeepsPositiveCues(t *testing.T) {
	got := cuesMS(t, "one two six ten red", 0, 0.002, 84, 0.0001)
	assertScene(t, got, 0, 2, "one two six ten red")
}

func TestSubMillisecondSceneUsesItsRoundedInterval(t *testing.T) {
	assertScene(t, cuesMS(t, "one two", 0, 0.0008, 84, 5.5), 0, 1, "one two")
}

func TestOffsetUsesRoundedSceneBoundaries(t *testing.T) {
	assertScene(t, cuesMS(t, "one two six", 2.1254, 0.5004, 3, 5.5), 2125, 2626, "one two six")
}

func TestInvalidDurationsAreRejected(t *testing.T) {
	for _, d := range []float64{0, -1, 0.0001, math.NaN(), math.Inf(1)} {
		if _, err := SceneCues("words", 0, d, 84, 5.5); err == nil {
			t.Errorf("duration %v accepted", d)
		}
	}
}

func TestWriteProducesParseableSRT(t *testing.T) {
	var b strings.Builder
	if err := Write(&b, []Cue{{0, 1.5, "one"}, {1.5, 3.25, "two"}}); err != nil {
		t.Fatal(err)
	}
	if got := end(t, b.String()); got != 3.25 {
		t.Fatal(got)
	}
	if !strings.Contains(b.String(), "00:00:01,500 --> 00:00:03,250") {
		t.Fatal(b.String())
	}
}
```

`internal/build/burn_test.go`:
```go
package build

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/testmedia"
)

func TestBurnPutsSubtitlesIntoThePictureOrEmbedsATrack(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := filepath.Join(t.TempDir(), "awk %d [x] 'q'")
	scratch := filepath.Join(dir, "scratch")
	os.MkdirAll(scratch, 0o755)
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "color=c=navy:size=640x360:rate=10:d=3",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo", "-t", "3",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "cut.mp4")
	srtPath := filepath.Join(dir, "cut.srt")
	os.WriteFile(srtPath, []byte("1\n00:00:00,000 --> 00:00:03,000\nProving it works ✓\n"), 0o644)
	out := filepath.Join(dir, "out.mp4")
	var log bytes.Buffer
	burned, err := burn(scratch, filepath.Join(dir, "cut.mp4"), srtPath, out, &log)
	if err != nil {
		t.Fatal(err)
	}
	if !burned {
		info, err := ffmpeg.Probe(out)
		if err != nil || !info.Has("subtitle") || !bytes.Contains(log.Bytes(), []byte("no libass")) {
			t.Fatalf("soft fallback: %v %+v\n%s", err, info, log.String())
		}
		return
	}
	testmedia.FFmpeg(t, dir, "-ss", "1.5", "-i", "cut.mp4", "-frames:v", "1", "before.png")
	testmedia.FFmpeg(t, dir, "-ss", "1.5", "-i", "out.mp4", "-frames:v", "1", "after.png")
	if countDiffering(t, filepath.Join(dir, "before.png"), filepath.Join(dir, "after.png")) < 500 {
		t.Fatal("burning changed almost no pixels: the subtitles are not in the picture")
	}
}

func countDiffering(t *testing.T, a, b string) int {
	t.Helper()
	load := func(p string) []uint8 {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		var px []uint8
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				r, _, _, _ := img.At(x, y).RGBA()
				px = append(px, uint8(r>>8))
			}
		}
		return px
	}
	pa, pb := load(a), load(b)
	n := 0
	for i := range min(len(pa), len(pb)) {
		if d := int(pa[i]) - int(pb[i]); d > 40 || d < -40 {
			n++
		}
	}
	return n
}
```

Append to `cmd/movie/build_test.go`:
```go
func TestNarratedBuildWritesSubtitlesAndPassesTheCheck(t *testing.T) {
	requirePiper(t)
	dir := t.TempDir()
	for i, c := range []string{"red", "green", "blue", "white", "black", "yellow", "cyan", "magenta", "gray", "orange"} {
		still(t, dir, fmt.Sprintf("run/f%02d.png", i), c)
	}
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
engine: piper
scenes:
  - id: title
    card: proving it works
    duration: 2
  - id: run
    frames: run
    rate: 1
    narration: The run takes ten seconds, and this sentence is much shorter than that.
`)
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 0 || !strings.Contains(r.stdout, "Mechanical checks pass") {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	data, err := os.ReadFile(filepath.Join(dir, "demo.srt"))
	// the run scene starts after the 2 s card (plus a few ms of audio padding)
	if err != nil || !strings.Contains(string(data), "\n00:00:02,") {
		t.Fatalf("subtitles should start at the run scene's offset, about 2 s:\n%s", data)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/srt ./internal/build ./cmd/movie`
Expected: FAIL (undefined `SceneCues`, `Write`, `burn`).

- [ ] **Step 3: Write `internal/srt/cues.go`**

```go
package srt

import (
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

// Cue is one subtitle: seconds from the start of the movie, and its text.
type Cue struct {
	Start, End float64
	Text       string
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// round matches the original tool, which rounded halves to even.
func round(x float64) int { return int(math.RoundToEven(x)) }

// SceneCues splits one scene's narration into cues that exactly tile
// [start, start+duration] in whole milliseconds, each cue's share of time
// proportional to its characters. Readability limits guide where chunks
// split; they never cut off the end of the scene or drop a word.
func SceneCues(text string, start, duration float64, maxChars int, maxSecs float64) ([]Cue, error) {
	end := start + duration
	for _, v := range []float64{start, duration, end} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("start must be finite and nonnegative; duration must be finite and positive")
		}
	}
	if start < 0 || duration <= 0 {
		return nil, errors.New("start must be finite and nonnegative; duration must be finite and positive")
	}
	startMS, endMS := round(start*1000), round(end*1000)
	available := endMS - startMS
	if available <= 0 {
		return nil, errors.New("scene has no representable millisecond subtitle interval")
	}
	charLimit := max(1, min(maxChars, int(float64(runes(text))*math.Min(1, maxSecs/duration))))
	chunks := chunk(text, charLimit)
	if len(chunks) > available {
		n := len(chunks)
		grouped := make([]string, available)
		for i := range available {
			grouped[i] = strings.Join(chunks[i*n/available:(i+1)*n/available], " ")
		}
		chunks = grouped
	}
	for {
		total := 0
		for _, c := range chunks {
			total += runes(c)
		}
		total = max(total, 1)
		elapsed, previous, split := 0, startMS, -1
		var cues []Cue
		for i, c := range chunks {
			elapsed += runes(c)
			remaining := len(chunks) - i - 1
			boundary := startMS + round(float64(available)*float64(elapsed)/float64(total))
			// reserve one millisecond per remaining cue, even for very uneven text
			boundary = min(endMS-remaining, max(previous+1, boundary))
			if remaining == 0 {
				boundary = endMS
			}
			if split < 0 && float64(boundary-previous) > maxSecs*1000 && len(strings.Fields(c)) > 1 {
				split = i
			}
			cues = append(cues, Cue{float64(previous) / 1000, float64(boundary) / 1000, wrap(c, 42)})
			previous = boundary
		}
		if split < 0 || len(chunks) == available {
			return cues, nil
		}
		// Splitting changes every cue's share, so remeasure all of them until
		// every splittable chunk fits or milliseconds limit the count.
		w := strings.Fields(chunks[split])
		mid := len(w) / 2
		chunks = slices.Replace(chunks, split, split+1, strings.Join(w[:mid], " "), strings.Join(w[mid:], " "))
	}
}

// chunk splits text into cue-sized pieces at word boundaries, ending a piece
// early at a sentence end once it is reasonably long.
func chunk(text string, maxChars int) []string {
	var chunks []string
	cur := ""
	for _, w := range strings.Fields(text) {
		try := strings.TrimSpace(cur + " " + w)
		if runes(try) > maxChars && cur != "" {
			chunks = append(chunks, cur)
			cur = w
			continue
		}
		cur = try
		if strings.HasSuffix(cur, ".") || strings.HasSuffix(cur, "!") || strings.HasSuffix(cur, "?") {
			if float64(runes(cur)) > float64(maxChars)*0.45 {
				chunks = append(chunks, cur)
				cur = ""
			}
		}
	}
	if cur != "" {
		chunks = append(chunks, cur)
	}
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

// wrap breaks a cue into at most two lines near width characters.
func wrap(line string, width int) string {
	var out []string
	cur := ""
	for _, w := range strings.Fields(line) {
		try := strings.TrimSpace(cur + " " + w)
		if runes(try) > width && cur != "" {
			out = append(out, cur)
			cur = w
		} else {
			cur = try
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) <= 2 {
		return strings.Join(out, "\n")
	}
	half := len(out) / 2
	return strings.Join(out[:half], " ") + "\n" + strings.Join(out[half:], " ")
}

// Timestamp formats seconds as an SRT time, HH:MM:SS,mmm.
func Timestamp(seconds float64) string {
	ms := round(seconds * 1000)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// Write writes cues as SRT.
func Write(w io.Writer, cues []Cue) error {
	for i, c := range cues {
		if _, err := fmt.Fprintf(w, "%d\n%s --> %s\n%s\n\n", i+1, Timestamp(c.Start), Timestamp(c.End), c.Text); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Write `internal/build/subtitles.go`**

```go
package build

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/fonts"
	"github.com/prime-radiant-inc/proving-it-works/internal/scene"
	"github.com/prime-radiant-inc/proving-it-works/internal/srt"
)

// Readability limits: two comfortable lines, and a cue on screen long
// enough to read but not so long it goes stale.
const (
	maxCueChars = 84
	maxCueSecs  = 5.5
)

// writeSubtitles writes cues for every narrated scene, each spanning its
// narration clip from the scene's offset in the cut, and returns where the
// last narration ends.
func writeSubtitles(path string, f *scene.File, clips map[string]clip, offsets map[string]float64) (float64, error) {
	var cues []srt.Cue
	speechEnd := 0.0
	for _, sc := range f.Scenes {
		c, ok := clips[sc.ID]
		if !ok {
			continue
		}
		sceneCues, err := srt.SceneCues(sc.Narration, offsets[sc.ID], c.seconds, maxCueChars, maxCueSecs)
		if err != nil {
			return 0, fmt.Errorf("scene %s subtitles: %w", sc.ID, err)
		}
		cues = append(cues, sceneCues...)
		speechEnd = offsets[sc.ID] + c.seconds
	}
	var b strings.Builder
	if err := srt.Write(&b, cues); err != nil {
		return 0, err
	}
	return speechEnd, os.WriteFile(path, []byte(b.String()), 0o644)
}

// subtitleStyle boxes the text: outline-only subtitles are legible over a
// dark terminal and marginal over a white screenshot, and a demo cuts
// between both.
const subtitleStyle = "FontName=DejaVu Sans,Fontsize=16,BorderStyle=3,Outline=1,Shadow=0,MarginV=30," +
	"PrimaryColour=&H00FFFFFF&,OutlineColour=&HB0101014&,BackColour=&HB0101014&"

// burn puts subtitles into the picture when ffmpeg has libass, and otherwise
// embeds a soft track and says so. It runs from scratch with fixed names and
// the embedded font, so no user path enters filter syntax and the result
// does not depend on system fonts. ffmpeg 8 dropped positional filter
// options, hence subtitles=filename=.
func burn(scratch, cut, srtPath, out string, stdout io.Writer) (bool, error) {
	data, err := os.ReadFile(srtPath)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(scratch, "captions.srt"), data, 0o644); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Join(scratch, "fonts"), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(scratch, "fonts", "DejaVuSans.ttf"), fonts.SansTTF(), 0o644); err != nil {
		return false, err
	}
	if ffmpeg.HasFilter("subtitles") {
		err := ffmpeg.Run(scratch, nil, "-i", cut,
			"-vf", "subtitles=filename=captions.srt:fontsdir=fonts:force_style='"+subtitleStyle+"'",
			"-c:a", "copy", "-c:v", "libx264", "-preset", "medium", "-pix_fmt", "yuv420p", out)
		if err != nil {
			return false, fmt.Errorf("burning subtitles: %w", err)
		}
		return true, nil
	}
	err = ffmpeg.Run(scratch, nil, "-i", cut, "-i", "captions.srt",
		"-map", "0:v:0", "-map", "0:a?", "-map", "1:s:0",
		"-c", "copy", "-c:s", "mov_text", "-metadata:s:s:0", "language=eng", out)
	if err != nil {
		return false, fmt.Errorf("embedding subtitles: %w", err)
	}
	fmt.Fprintln(stdout, "WARN       this ffmpeg has no libass (no subtitles filter), so the subtitles are a "+
		"soft track a player must choose to show, not pixels. Slack and PR previews will show none. "+
		"Install an ffmpeg with libass to burn them in.")
	return false, nil
}
```

- [ ] **Step 5: Finish `build.Run`**

In `internal/build/build.go`, replace everything in `Run` from `var names []string` to the end of the function with:
```go
	offsets := map[string]float64{}
	clock := 0.0
	var names []string
	for _, sc := range f.Scenes {
		c := clips[sc.ID]
		d, err := segment(scratch, f, sc, c.wav, c.seconds)
		if err != nil {
			return 0, err
		}
		offsets[sc.ID] = clock
		clock += d
		fmt.Fprintf(stdout, "%s: %.1fs\n", sc.ID, d)
		names = append(names, sc.ID+".mp4")
	}
	opts := check.Options{}
	if !f.Narrated() {
		if err := concat(scratch, names, out); err != nil {
			return 0, err
		}
	} else {
		cut := filepath.Join(scratch, "cut.mp4")
		if err := concat(scratch, names, cut); err != nil {
			return 0, err
		}
		srtPath := strings.TrimSuffix(out, filepath.Ext(out)) + ".srt"
		speechEnd, err := writeSubtitles(srtPath, f, clips, offsets)
		if err != nil {
			return 0, err
		}
		if _, err := burn(scratch, cut, srtPath, out, stdout); err != nil {
			return 0, err
		}
		opts = check.Options{ExpectAudio: true, ExpectSubtitles: true, SpeechEnd: &speechEnd}
	}
	fmt.Fprintf(stdout, "\nassembled %s (%.1fs)\n\n", out, clock)
	return check.Run(out, opts, stdout)
```

- [ ] **Step 6: Run all tests to verify they pass**

Run: `go test ./...`
Expected: PASS (with Piper installed, the narrated build test runs and passes).

- [ ] **Step 7: Look at a narrated movie**

Build the narrated test's scene file by hand with Piper installed, then open `demo-check/contact-sheet.png` and play `demo.mp4`. Expected: subtitles boxed at the bottom from 2 s onward, readable over every colour. Do not continue until you have looked and listened.

- [ ] **Step 8: Commit**

```bash
git add internal/srt internal/build cmd/movie
git commit -m "Time subtitles to measured narration, burn them with an embedded font, and check with build's expectations"
```

---

### Task 14: Retire the Python pipeline and rewrite its docs

**Files:**
- Delete: `skills/proving-it-works-with-a-movie/scripts/{narrate,assemble,make-subtitles,burn-subtitles,media_paths.py,narration_contract.py}`, `tests/proving-it-works-with-a-movie/{test_assembly.py,test_narration.py,test_narration_contract.py,test_subtitle_contract.py,test_subtitles.py,test_paths.py}`
- Keep: `skills/proving-it-works-with-a-movie/scripts/browser_tools.py` (the Windows recorder imports it until Task 18)
- Modify: `tests/proving-it-works-with-a-movie/run-tests.py`, `tests/proving-it-works-with-a-movie/README.md`, `skills/proving-it-works-with-a-movie/SKILL.md`, `assembling.md`, `narrating.md`, `README.md`, `examples/e2e/scenes.yaml`, `examples/e2e/Dockerfile`, `examples/e2e/README.md`

- [ ] **Step 1: Delete the Python pipeline and its tests**

```bash
cd skills/proving-it-works-with-a-movie/scripts
git rm narrate assemble make-subtitles burn-subtitles media_paths.py narration_contract.py
cd ../../../tests/proving-it-works-with-a-movie
git rm test_assembly.py test_narration.py test_narration_contract.py test_subtitle_contract.py test_subtitles.py test_paths.py
cd ../..
```

In `run-tests.py`, set `IMPLEMENTED_SUITES` to:
```python
IMPLEMENTED_SUITES = {
    "browser": "test_browser.py",
    "contracts": "test_*contract*.py",
    "terminal": "test_terminal.py",
}
```
In that directory's `README.md`, replace the command list with the three remaining suites plus `all`, and add one line: `The movie pipeline itself is tested in Go: go test ./...`.

Run: `uv run --script tests/proving-it-works-with-a-movie/run-tests.py --suite all`
Expected: PASS (browser, recorder contract, and terminal tests; terminal tests may skip without ttyd, saying so).

- [ ] **Step 2: Rewrite the pipeline section of `SKILL.md`**

Replace everything from `## The gate — every route, before you hand anything over` up to (not including) `## The silent failures` with:
````markdown
## Build it, and gate it

Every route ends in the same two commands. `$SKILL_DIR` is this skill's own
directory (the "Base directory for this skill" printed when it loads;
installed as a plugin, `$CLAUDE_PLUGIN_ROOT/skills/proving-it-works-with-a-movie`).

```bash
"$SKILL_DIR/bin/movie" build scenes.yaml movie.mp4   # narrate, assemble, subtitle, burn, check
"$SKILL_DIR/bin/movie" check other.mp4               # the gate alone, for a movie made elsewhere
```

On Windows PowerShell, run `& "$SKILL_DIR/bin/movie-windows-amd64.exe"` with
the same arguments. From Git Bash, `bin/movie` works; pass paths in Windows
form (`cygpath -m`).

`build` reads a scene file (assembling.md), narrates each narrated scene with
a cloud voice when `OPENAI_API_KEY` exists and a local voice when it does
not (narrating.md), holds every scene for max(narration, visuals), writes
`movie.srt`, burns it into the picture, and runs the gate. A nonzero exit
means do not ship.

The gate samples picture and sound on one timeline and fails the movie when
the action is crammed into the first seconds while narration keeps talking,
when the picture never changes, when the audio is silent, or when a narrated
movie has no subtitles or subtitles that quit before the narration does. It
samples once a second, so any beat that must register (a flash, a blank
frame, a transition) has to be held longer than a second. Then:

1. **Open the contact sheet it wrote (`movie-check/contact-sheet.png`) and
   actually look at it.** Identical tiles mean a frozen movie. Unreadable
   text means your viewport is wrong.
2. **If narrated: listen to it.** No tool here can hear a mispronounced name
   or a skipped sentence. For `openai-chat`, the model's own transcript is
   gated, which proves what it says it said, not what is in the audio.
3. Fix, regenerate, re-run. Never patch the report instead of the movie.
````

In the table under `## The silent failures`, change the last row's second cell to: `` Narration without subtitles. `movie build` writes and burns them ``. In `## Red flags — stop`, replace `"No API key, so no narration" → `narrate` falls back to a local voice.` with `"No API key, so no narration" → `movie build` uses a local voice.`

- [ ] **Step 3: Rewrite `assembling.md`**

Replace the whole file with:
````markdown
# Assembling

`movie build scenes.yaml movie.mp4` turns clips, stills, cards, and
narration into one checked file. This page is the scene file, and the
ffmpeg traps worth knowing when you make the pieces yourself.

## The scene file

```yaml
size: 1920x1080              # default; width and height must be even
fps: 30                      # default
engine: auto                 # auto | openai | openai-chat | piper (narrating.md)
voice: nova                  # default: nova for openai*, en_US-lessac-medium for piper

scenes:
  - id: title                # [a-z0-9][a-z0-9-]*, unique
    card: proving-it-works   # a title card, drawn for you
    subtitle: installed from a public marketplace
    duration: 4              # card and image: seconds to hold, default 3

  - id: install
    frames: takes/install/take-1/   # a directory of PNGs, in name order
    rate: 10                        # frames per second, default fps
    narration: >-
      This is a container with nothing of ours in it.

  - id: sheet
    image: work/out/contact-sheet.png
    narration: So we do.

  - id: movie
    movie: work/out/counter.mp4     # played as itself, with its own audio
```

A scene is whichever one of `card`, `image`, `frames`, or `movie` it has.
Paths are relative to the scene file. `build` validates the whole file
before doing any work and lists every problem at once.

## The segment rule

Each scene lasts **max(narration, visuals)**. Short video freezes its last
frame; short audio pads with silence. A `movie` scene lasts as long as the
movie and keeps its own sound; it takes no narration.

**A long freeze-frame tail is a smell, not a fix.** If a scene's narration
runs 20 seconds past its visuals, the scene is wrong: give the camera
something to do, or cut the words.

## Cards

`card` scenes are drawn by `build` itself: title and subtitle centered on a
dark background, shrunk until the longest line fits. The card font covers
Latin, Greek, and Cyrillic; `build` refuses card text it cannot draw rather
than drawing boxes. For anything else, make an image and use an `image`
scene.

## What build writes

- `movie.mp4`, the movie.
- `movie.srt`, the subtitles, beside it: burned into the picture where
  ffmpeg has libass, and the transcript the checker reads. Without libass,
  `build` embeds a soft track instead and warns that the subtitles are not
  in the picture: anything that autoplays without subtitle UI (Slack, PR
  previews) will show none.
- `movie-check/contact-sheet.png`, the sheet to look at.
- `movie.build/`, scratch: segments and cached narration. Safe to delete.

## `-nostdin` on every ffmpeg call inside a loop

When you make the pieces yourself, remember ffmpeg reads stdin by default
and will eat a loop's input:

```bash
while IFS= read -r scene; do
  ffmpeg -nostdin ...          # without this, ffmpeg swallows the rest of the list
done < scenes.txt
```

Symptom when you forget: scenes silently skipped, and an error naming a
*truncated* identifier (`val-landing` for `eval-landing`). It reads like a
corrupt input file.

## Keep the pipeline out of scratch

The scene file, narration text, and whatever script records the frames
belong in the repo. Scratch directories get cleaned; a movie you cannot
rebuild is a movie you cannot fix. Ask before committing large media; the
pipeline is small and always worth committing.
````

- [ ] **Step 4: Rewrite `narrating.md`**

Replace the whole file with:
````markdown
# Narrating

Narration is where the most embarrassing silent failures live: the movie
looks perfect and says the wrong words.

## Engines

`movie build` narrates every scene that has `narration`, with the scene
file's `engine`:

| Engine | What it is | Watch for |
|---|---|---|
| `auto` (default) | `openai` when `OPENAI_API_KEY` (or `llm keys get openai`) yields a key, else `piper` | |
| `openai` | OpenAI's speech endpoint | Reads exactly what it is sent. The safe default. |
| `openai-chat` | A chat model with audio output | Best prosody, but it ad-libs ("Sure, here it is:"). Gated: its own transcript must match the script word for word, or the clip is rejected and retried once. That proves what the model says it said, not what is in the audio. |
| `piper` | A local neural voice, free and offline | **Mispronounces** unusual names rather than dropping them. |

Local TTS engines other than these can drop out-of-vocabulary words
silently with a zero exit code ("every eval on the shelf" became "every on
the shelf"). That is why `build` offers only these three.

## The local voice

Install Piper once, and its default voice once:

```bash
uv tool install piper-tts
uvx --from piper-tts python -m piper.download_voices --data-dir ~/.cache/piper-voices en_US-lessac-medium
```

`build` looks for voices in `PIPER_VOICE_DIR`, default
`~/.cache/piper-voices`, and names the exact command if something is
missing. It never downloads anything itself.

## Listen to it

Nothing in `build` hears the audio. Before you commit to a voice, listen to
a clip of your actual sentences, including product names and jargon. A voice
that mangles the one word your movie is about is worse than no narration.
After the build, play the movie with sound on.

## Clips are cached

`build` keeps each accepted clip in `movie.build/narration/`, named by its
text, engine, voice, and model, and reuses it while all four are unchanged.
It prints each clip's path. To redo a clip that sounds wrong, delete that
file and build again.

## Measured, never guessed

`build` measures every clip and holds each scene for max(narration,
visuals), so nothing is paced against a guess. When you record motion to
match narration, build once, read each scene's length from the build
output, and pace the recording to it. Word-count estimates (~2.5 words/sec)
are for planning the script only.

## Pronunciation of product names

If a voice mangles one term, try another voice first. Respelling the term
phonetically in `narration` works, but the subtitles will show the
respelling too.
````

- [ ] **Step 5: Update the README and the e2e example**

In `README.md`:
- Replace the `### The scripts` section (heading, table) with:
```markdown
### The tool

One binary, `bin/movie` (prebuilt for macOS, Linux, and Windows; nothing to
install but ffmpeg):

| Command | Does |
|---|---|
| `movie build scenes.yaml movie.mp4` | narrates (a cloud voice with a key, a local one without), assembles each scene to max(narration, visuals), writes and burns subtitles, and runs the gate |
| `movie check movie.mp4` | the gate, alone |
| `movie term ...` | films a terminal session into frames |
```
- In Requirements, replace the `uv` bullet with `` - For the keyless voice: Piper (`uv tool install piper-tts`), see the skill's `narrating.md` ``, and the narration bullet with `- tmux, for filming terminals`.
- In Tests, make the block:
```
go test ./...
```
and describe it: synthesizes movies with known defects via ffmpeg's lavfi sources and asserts each verdict, builds real movies at awkward paths, and drives real tmux sessions.

`examples/e2e/scenes.yaml`:
```yaml
fps: 30
scenes:
  - id: title
    card: proving-it-works
    subtitle: installed from a public marketplace, then used - in a clean container
    duration: 4

  - id: install
    frames: install
    rate: 2.6
    narration: >-
      This is a container with nothing of ours in it. Claude Code is here,
      and no plugins. We add the marketplace straight from the public repo,
      install the plugin, and list again: proving it works, version zero
      point one, enabled.

  - id: agent
    frames: agent
    rate: 5.0
    narration: >-
      Now the real test. We ask Claude, inside that container, to use the
      skill it just installed. Record the real page in motion with a cursor
      you can see. There is no API key in here, so narrate with a local
      voice. And subtitles are not optional.

  - id: elapsed
    card: about six minutes of real agent work
    subtitle: it drove headless Chromium, spoke with a local voice, and burned in subtitles
    duration: 3.5

  - id: handoff
    card: the movie it made
    subtitle: full size, its own voice, its own subtitles
    duration: 3.2
    narration: >-
      So here is that movie, playing as itself. Local voice, burned-in
      subtitles, and a cursor you can watch.

  - id: movie
    movie: work/out/counter.mp4

  - id: verify
    frames: verify
    rate: 2.4
    narration: >-
      And here is the skill's own checker, run against it. The picture
      reaches new states at the right cadence, the narration is there, and
      the subtitles run to the end of the speech. Mechanical checks pass,
      then it tells you the part no script can do. Open the contact sheet
      and look at it.

  - id: sheet
    image: work/out/hostcheck/contact-sheet.png
    narration: >-
      So we do. Zero, one, two, with the cursor on the button and the
      subtitles under it. The plugin installed from a public marketplace,
      the skill was read and followed, and every frame is the real page.

  - id: end
    card: github.com/prime-radiant-inc/proving-it-works
    subtitle: a movie is evidence - verify it before you hand it over
    duration: 4.5
```

`examples/e2e/Dockerfile`: replace the comment `# uv: check-movie runs via `uv run --script` (PEP 723 inline deps)` with `# uv: installs the piper CLI for the keyless voice`, and replace the final `RUN uv run --quiet --with piper-tts python3 -c "..."` block (lines from `RUN uv run --quiet` to `print('voice cached')"`) with:
```dockerfile
RUN uv tool install piper-tts \
    && uvx --from piper-tts python -m piper.download_voices \
         --data-dir "$PIPER_VOICE_DIR" en_US-lessac-medium
```
and change its comment to `# ... movie build finds it here via PIPER_VOICE_DIR.`

`examples/e2e/README.md`: in the pieces table change `` `scripts/assemble` `` to `` `movie build` ``; replace the "Then narrate, assemble, subtitle, and gate it" block with:
```bash
../../skills/proving-it-works-with-a-movie/bin/movie build scenes.yaml demo.mp4
```
and in "What it cost to get right", change `check-movie` to `movie check` and `` `burn-subtitles` detects `` to `` `movie build` detects ``.

- [ ] **Step 6: Verify nothing still points at a deleted tool**

Run: `git grep -n -E "scripts/(narrate|assemble|make-subtitles|burn-subtitles|check-movie)|narration_contract|media_paths|--verify on|manifest.json|offsets.json" -- ':!docs/superpowers'`
Expected: no output.

Run: `go test ./... && uv run --script tests/proving-it-works-with-a-movie/run-tests.py --suite all`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git status
git add -A skills tests README.md examples
git commit -m "Retire the Python pipeline; the docs now describe movie build and movie check"
```

---

### Task 15: `term` spike

A short investigation, recorded as findings; code written here is throwaway and not committed except the findings document and chosen fonts.

**Files:**
- Create: `docs/superpowers/specs/2026-09-23-term-spike-findings.md`
- Create (if the spike selects them): `internal/fonts/<Fallback>.ttf` and their license files

- [ ] **Step 1: Record real sessions**

With the skeleton binary, record four sessions on macOS and on Linux (a Linux container is fine): a plain shell (`ls --color`, `git log --oneline --graph --color`), `less` on a long file, `htop` for a few seconds then `q`, and a Claude Code session (`claude` then a one-line prompt, then `/exit`). Use `movie term start/run/key/stop`; for the TUIs, use `run` with a short `--timeout` and send keys with `tmux -S <socket> send-keys` directly (the `key` verb arrives in Task 16). Keep each session's `recording.jsonl`.

- [ ] **Step 2: Inventory the escape sequences and characters**

Write a scratch Go program that reads each `recording.jsonl` and lists: every distinct CSI sequence in `screen` (expected: only SGR `...m`), and every distinct non-ASCII character with its code point.

- [ ] **Step 3: Choose fallback fonts**

For each non-ASCII character found, check DejaVu Sans Mono with `fonts.Missing`. For the missing ones (expected: `⏺ U+23FA`, `⎿ U+23BF`, `⏵ U+23F5`, `⏸ U+23F8`, braille `U+2800-28FF`), test candidate fonts with open licenses (start with Noto Sans Symbols 2, Noto Sans Math, and DejaVu Sans) and pick the fewest that cover them. Record each chosen font's source URL, SHA-256, and license.

- [ ] **Step 4: Measure what the design assumes**

- `pane_height` equals the requested rows after `status off`.
- Multibyte text typed with `send-keys -H` arrives intact with the default locale on macOS and Linux; if not, record which `LANG`/`LC_ALL` the session command must set (candidates: pass through a UTF-8 host `LANG`; else `en_US.UTF-8` on macOS and `C.UTF-8` on Linux).
- Snapshots per second achieved locally, and through `docker exec` into the e2e image (`docker exec CONTAINER tmux ...` for the chained status + capture call).
- `pane_current_command` inside the container through `docker exec`.
- Whether `tmux -u` is needed or harmful.

- [ ] **Step 5: Write the findings**

`docs/superpowers/specs/2026-09-23-term-spike-findings.md` lists, as decisions for Tasks 16-18: the escape sequences the renderer must handle; the fallback fonts (files, URLs, SHA-256, licenses); the locale setting for the session command; measured capture rates (local and docker); any design assumption that failed and what replaces it. If an assumption failed badly enough to change the spec, stop and bring it to Jesse before Task 16.

- [ ] **Step 6: Commit**

```bash
git add docs/superpowers/specs/2026-09-23-term-spike-findings.md internal/fonts
git commit -m "Record the term spike findings and add the fallback fonts they chose"
```

---

### Task 16: The rest of the `term` verbs

**Files:**
- Modify: `internal/term/verbs.go`, `internal/term/main.go`, `internal/term/session.go` (locale from Task 15), `cmd/movie/term_test.go`
- Create: `internal/term/keys.go`, `internal/term/keys_test.go`

**Interfaces:**
- Produces: `keyArgs(name string) ([]string, error)`; `TypeText(s *Session, text string) error`; `PressKey(s *Session, name string) error`; `Wait(s *Session, timeout, quiet time.Duration, stdout io.Writer) (int, error)`; `Screen(s *Session, stdout io.Writer) error`; `SetFilm(s *Session, on bool) error`.

- [ ] **Step 1: Write the failing tests**

`internal/term/keys_test.go`:
```go
package term

import (
	"slices"
	"testing"
)

func TestKeyArgs(t *testing.T) {
	for name, want := range map[string][]string{
		"Enter": {"Enter"}, "Escape": {"Escape"}, "Tab": {"Tab"},
		"Up": {"Up"}, "Down": {"Down"}, "Left": {"Left"}, "Right": {"Right"},
		"C-c": {"C-c"}, "q": {"-H", "71"}, ";": {"-H", "3b"}, "λ": {"-H", "ce", "bb"},
	} {
		got, err := keyArgs(name)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("keyArgs(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, bad := range []string{"", "Ctrl-X", "ab"} {
		if _, err := keyArgs(bad); err == nil {
			t.Errorf("keyArgs(%q) accepted", bad)
		}
	}
}

func TestHumanPaceCapsLongCommands(t *testing.T) {
	if humanPace(10).Milliseconds() != 55 || humanPace(400).Milliseconds() != 10 {
		t.Fatal(humanPace(10), humanPace(400))
	}
}
```

Append to `cmd/movie/term_test.go`:
```go
func TestRunRefusesWhileACommandRunsAndWaitReportsItStillRunning(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	r := runMovie(t, dir, "term", "run", session, "sleep 3", "--timeout", "0.5")
	if r.code != 3 || !strings.Contains(r.stdout, `"outcome":"running"`) {
		t.Fatalf("run: code %d\n%s", r.code, r.stdout)
	}
	r = runMovie(t, dir, "term", "run", session, "echo too soon")
	if r.code != 2 || !strings.Contains(r.stderr, "not at a prompt") {
		t.Fatalf("second run: code %d\n%s", r.code, r.stderr)
	}
	r = runMovie(t, dir, "term", "wait", session, "--timeout", "10")
	if r.code != 0 || !strings.Contains(r.stdout, `"outcome":"completed"`) {
		t.Fatalf("wait: code %d\n%s", r.code, r.stdout)
	}
}

func TestWaitQuietReturnsWhenATUIGoesStill(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	if r := runMovie(t, dir, "term", "type", session, "cat"); r.code != 0 {
		t.Fatalf("type: %d %s", r.code, r.stderr)
	}
	if r := runMovie(t, dir, "term", "key", session, "Enter"); r.code != 0 {
		t.Fatalf("key: %d %s", r.code, r.stderr)
	}
	r := runMovie(t, dir, "term", "wait", session, "--quiet", "1", "--timeout", "10")
	if r.code != 3 || !strings.Contains(r.stdout, `"outcome":"quiet"`) {
		t.Fatalf("wait --quiet: code %d\n%s", r.code, r.stdout)
	}
	runMovie(t, dir, "term", "key", session, "C-c")
	if r := runMovie(t, dir, "term", "wait", session); r.code != 1 && r.code != 0 {
		t.Fatalf("after C-c: code %d\n%s", r.code, r.stdout)
	}
}

func TestScreenShowsText(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	runMovie(t, dir, "term", "run", session, "echo 'semi;colon -dash λ'")
	r := runMovie(t, dir, "term", "screen", session)
	if r.code != 0 || !strings.Contains(r.stdout, "semi;colon -dash λ") {
		t.Fatalf("screen: code %d\n%s", r.code, r.stdout)
	}
}

func TestFilmOffSplitsTakes(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	runMovie(t, dir, "term", "run", session, "echo one")
	runMovie(t, dir, "term", "film", session, "off")
	runMovie(t, dir, "term", "run", session, "echo off camera")
	runMovie(t, dir, "term", "film", session, "on")
	runMovie(t, dir, "term", "run", session, "echo two")
	takes := filepath.Join(dir, "takes")
	if r := runMovie(t, dir, "term", "stop", session, takes); r.code != 0 {
		t.Fatalf("stop: %s", r.stderr)
	}
	for _, take := range []string{"take-1", "take-2"} {
		if _, err := os.Stat(filepath.Join(takes, take, "take.json")); err != nil {
			t.Errorf("%s missing: %v", take, err)
		}
	}
	if _, err := os.Stat(filepath.Join(takes, "take-3")); err == nil {
		t.Error("a third take appeared")
	}
}

func TestRenderWorksAfterTheSessionIsGoneAndRefusesAUsedDirectory(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	runMovie(t, dir, "term", "run", session, "echo hi")
	if r := runMovie(t, dir, "term", "stop", session, filepath.Join(dir, "a")); r.code != 0 {
		t.Fatal(r.stderr)
	}
	if r := runMovie(t, dir, "term", "render", session, filepath.Join(dir, "b"), "--px", "800x450"); r.code != 0 {
		t.Fatalf("render: %s", r.stderr)
	}
	if r := runMovie(t, dir, "term", "render", session, filepath.Join(dir, "b")); r.code != 2 || !strings.Contains(r.stderr, "not empty") {
		t.Fatalf("reuse: code %d %s", r.code, r.stderr)
	}
	if r := runMovie(t, dir, "term", "start", session); r.code != 2 || !strings.Contains(r.stderr, "not empty") {
		t.Fatalf("restart: code %d %s", r.code, r.stderr)
	}
}

func TestTheUsersHistoryIsNeverReadOrWritten(t *testing.T) {
	home := t.TempDir()
	secret := filepath.Join(home, ".bash_history")
	os.WriteFile(secret, []byte("export SECRET_TOKEN=hunter2\n"), 0o600)
	t.Setenv("HOME", home)
	dir := t.TempDir()
	session := startSession(t, dir)
	runMovie(t, dir, "term", "run", session, "echo filmed")
	runMovie(t, dir, "term", "key", session, "Up")
	r := runMovie(t, dir, "term", "screen", session)
	if strings.Contains(r.stdout, "hunter2") {
		t.Fatal("the user's history reached the screen")
	}
	data, _ := os.ReadFile(secret)
	if strings.Contains(string(data), "echo filmed") {
		t.Fatal("the session wrote the user's history")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/term ./cmd/movie -run 'Key|Pace|Refuses|Quiet|Screen|Film|Render|History'`
Expected: FAIL (undefined `keyArgs`; unknown verbs).

- [ ] **Step 3: Write `internal/term/keys.go`**

```go
package term

import (
	"fmt"
	"unicode/utf8"
)

// namedKeys are passed to tmux by name, so tmux encodes them for the pane's
// current mode (arrows follow application cursor mode, as a real terminal's do).
var namedKeys = map[string]bool{
	"Enter": true, "Escape": true, "Tab": true,
	"Up": true, "Down": true, "Left": true, "Right": true, "C-c": true,
}

// keyArgs is the send-keys arguments for one key: a named key, or one
// character sent as raw bytes.
func keyArgs(name string) ([]string, error) {
	if namedKeys[name] {
		return []string{name}, nil
	}
	if utf8.RuneCountInString(name) == 1 {
		args := []string{"-H"}
		for _, b := range []byte(name) {
			args = append(args, fmt.Sprintf("%02x", b))
		}
		return args, nil
	}
	return nil, fmt.Errorf("unknown key %q: use Enter, Escape, Tab, Up, Down, Left, Right, C-c, or one character", name)
}
```

- [ ] **Step 4: Add the verbs to `internal/term/verbs.go`**

Replace `send` with a version that takes key names through `keyArgs`, and add the new verbs:
```go
// send marks input as sent at the current prompt, types text, and presses keys.
func (s *Session) send(text string, keys ...string) error {
	st, err := s.status(false)
	if err != nil {
		return err
	}
	if _, err := s.tmux("set-option", "-t", window, "@movie_sent", strconv.Itoa(st.Seq)); err != nil {
		return err
	}
	if err := s.typeText(text); err != nil {
		return err
	}
	for _, k := range keys {
		args, err := keyArgs(k)
		if err != nil {
			return err
		}
		if _, err := s.tmux(append([]string{"send-keys", "-t", window}, args...)...); err != nil {
			return err
		}
	}
	return nil
}

// TypeText types into whatever is running, with no prompt check.
func TypeText(s *Session, text string) error { return s.send(text) }

// PressKey presses one key.
func PressKey(s *Session, name string) error {
	if _, err := keyArgs(name); err != nil {
		return err
	}
	return s.send("", name)
}

// Wait waits for the next prompt, or for quiet, or for timeout.
func Wait(s *Session, timeout, quiet time.Duration, stdout io.Writer) (int, error) {
	return s.await(timeout, quiet, stdout)
}

// Screen prints the screen as text.
func Screen(s *Session, stdout io.Writer) error {
	st, err := s.status(false)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, strings.TrimRight(st.Screen, "\n"))
	return nil
}

// SetFilm turns filming on or off; each "on" after "off" starts a new take.
func SetFilm(s *Session, on bool) error {
	value := "off"
	if on {
		value = "on"
	}
	_, err := s.tmux("set-option", "-t", window, "@movie_film", value)
	return err
}
```

In `internal/term/main.go`, extend `usage` with:
```
  type SESSION 'text'
  key SESSION Enter|Escape|Tab|Up|Down|Left|Right|C-c|<one character>
  wait SESSION [--quiet S] [--timeout 60]
  screen SESSION
  film SESSION on|off
```
and add these cases to `dispatch` (each loads the session with `Load(pos[0])` first):
```go
	case "type", "key", "film":
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 2 {
			return exitcode.Usage, fmt.Errorf("needs SESSION and one argument")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		switch verb {
		case "type":
			return exitcode.OK, TypeText(s, pos[1])
		case "key":
			return exitcode.OK, PressKey(s, pos[1])
		}
		if pos[1] != "on" && pos[1] != "off" {
			return exitcode.Usage, fmt.Errorf("film takes on or off")
		}
		return exitcode.OK, SetFilm(s, pos[1] == "on")
	case "wait":
		timeout := fs.Float64("timeout", 60, "seconds to wait")
		quiet := fs.Float64("quiet", 0, "return once the screen is unchanged this many seconds")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 1 {
			return exitcode.Usage, fmt.Errorf("needs SESSION")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return Wait(s, seconds(*timeout), seconds(*quiet), stdout)
	case "screen":
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 1 {
			return exitcode.Usage, fmt.Errorf("needs SESSION")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Screen(s, stdout)
```

Apply the locale decision from the spike findings to `shellCommand` (for example, adding `LANG=...` to the `env` line) and to `Session.tmux`'s environment if the findings require it.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/term cmd/movie
git commit -m "Add type, key, wait, screen, and film to movie term, with honest exit codes"
```

---

### Task 17: Styled terminal rendering with visible glyph fallback

**Files:**
- Create: `internal/term/cells.go`, `internal/term/cells_test.go`, `internal/term/testdata/` (a committed recording and expected PNGs)
- Modify: `internal/term/render.go` (replace `renderer` and `draw`), `internal/term/render_test.go`, `internal/fonts/fonts.go` (the fallback chain from Task 15)

**Interfaces:**
- Produces: `fonts.MonoChain() []*opentype.Font`; `term.Cell{R rune; FG, BG color.RGBA; Bold, Underline, Reverse, Wide, Skip bool}`; `ParseLine(line string, cols int) []Cell`; `palette(n int) color.RGBA`.

- [ ] **Step 1: Write the failing tests**

`internal/term/cells_test.go`:
```go
package term

import (
	"image/color"
	"testing"
)

func TestParseLineAppliesSGR(t *testing.T) {
	cells := ParseLine("\x1b[31mred\x1b[39m \x1b[1;38;5;214mB\x1b[0m \x1b[48;2;1;2;3mx\x1b[m", 12)
	if len(cells) != 12 {
		t.Fatalf("got %d cells", len(cells))
	}
	if cells[0].R != 'r' || cells[0].FG != palette(1) {
		t.Errorf("red: %+v", cells[0])
	}
	if cells[3].FG != defaultFG {
		t.Errorf("reset fg: %+v", cells[3])
	}
	if cells[4].R != 'B' || !cells[4].Bold || cells[4].FG != palette(214) {
		t.Errorf("256-colour bold: %+v", cells[4])
	}
	if cells[6].BG != (color.RGBA{1, 2, 3, 255}) {
		t.Errorf("truecolour bg: %+v", cells[6])
	}
	if cells[11].R != ' ' || cells[11].BG != defaultBG {
		t.Errorf("padding: %+v", cells[11])
	}
}

func TestWideCharactersTakeTwoCells(t *testing.T) {
	cells := ParseLine("a漢b", 5)
	if !cells[1].Wide || !cells[2].Skip || cells[3].R != 'b' {
		t.Fatalf("%+v", cells[:4])
	}
}

func TestReverseSwapsColours(t *testing.T) {
	c := ParseLine("\x1b[7mx", 1)[0]
	fg, bg := c.Colours()
	if fg != defaultBG || bg != defaultFG {
		t.Fatalf("%v %v", fg, bg)
	}
}

func TestBoldBrightensTheFirstEightColours(t *testing.T) {
	c := ParseLine("\x1b[1;32mx", 1)[0]
	if fg, _ := c.Colours(); fg != palette(10) {
		t.Fatalf("%v", fg)
	}
}
```

Append to `internal/term/render_test.go`:
```go
func TestRenderMatchesGoldenFramesWithinTolerance(t *testing.T) {
	entries, err := readRecording("testdata/session")
	if err != nil {
		t.Fatal(err)
	}
	r := newRenderer(image.Pt(800, 450), entries[0].Cols, entries[0].Rows)
	for _, i := range []int{0, len(entries) / 2, len(entries) - 1} {
		got, err := r.draw(entries[i])
		if err != nil {
			t.Fatal(err)
		}
		golden := fmt.Sprintf("testdata/golden-%d.png", i)
		if os.Getenv("MOVIE_UPDATE_GOLDEN") == "1" {
			os.WriteFile(golden, got, 0o644)
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		// font rasterizing differs by a grey level or so between arm64 and amd64
		if n := differingPixels(t, got, want, 2); n > 400 {
			t.Errorf("entry %d: %d pixels differ from %s", i, n, golden)
		}
	}
	if len(r.missing) != 0 {
		t.Errorf("glyphs missing from the font chain: %q", r.missing)
	}
}

func differingPixels(t *testing.T, a, b []byte, tolerance int) int {
	t.Helper()
	ia, err1 := png.Decode(bytes.NewReader(a))
	ib, err2 := png.Decode(bytes.NewReader(b))
	if err1 != nil || err2 != nil || ia.Bounds() != ib.Bounds() {
		t.Fatalf("undecodable or differently sized: %v %v", err1, err2)
	}
	n := 0
	for y := ia.Bounds().Min.Y; y < ia.Bounds().Max.Y; y++ {
		for x := ia.Bounds().Min.X; x < ia.Bounds().Max.X; x++ {
			r1, g1, b1, _ := ia.At(x, y).RGBA()
			r2, g2, b2, _ := ib.At(x, y).RGBA()
			for _, d := range []int{int(r1>>8) - int(r2>>8), int(g1>>8) - int(g2>>8), int(b1>>8) - int(b2>>8)} {
				if d > tolerance || d < -tolerance {
					n++
					break
				}
			}
		}
	}
	return n
}

func TestMissingGlyphsAreReportedNotHidden(t *testing.T) {
	r := newRenderer(image.Pt(400, 100), 10, 2)
	if _, err := r.draw(Entry{Cols: 10, Rows: 2, Screen: "a 🦀 b\n"}); err != nil {
		t.Fatal(err)
	}
	if len(r.missing) != 1 || r.missing[0] != '🦀' {
		t.Fatalf("missing %q", r.missing)
	}
}
```
Add `"bytes"`, `"fmt"`, `"image"`, `"image/png"`, `"os"` to that file's imports.

- [ ] **Step 2: Create the golden recording**

Record a short session with colours, a wide character, box drawing, and each symbol Task 15 found (for example `run 'printf "\e[31mred \e[1;32mbold green\e[0m 漢 ─┼─ ⏺ ⎿ ⠋\n"'`), stop it, and copy its `recording.jsonl` to `internal/term/testdata/session/recording.jsonl`.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/term`
Expected: FAIL (undefined `ParseLine`, `palette`, `r.missing`).

- [ ] **Step 4: Write `internal/term/cells.go`**

```go
package term

import (
	"image/color"
	"strconv"
	"strings"

	"golang.org/x/text/width"
)

var (
	defaultFG = color.RGBA{0xe8, 0xe6, 0xe1, 0xff}
	defaultBG = color.RGBA{0x10, 0x10, 0x14, 0xff}
)

// ansi16 is xterm's default 16-colour palette.
var ansi16 = [16]color.RGBA{
	{0, 0, 0, 255}, {205, 0, 0, 255}, {0, 205, 0, 255}, {205, 205, 0, 255},
	{0, 0, 238, 255}, {205, 0, 205, 255}, {0, 205, 205, 255}, {229, 229, 229, 255},
	{127, 127, 127, 255}, {255, 0, 0, 255}, {0, 255, 0, 255}, {255, 255, 0, 255},
	{92, 92, 255, 255}, {255, 0, 255, 255}, {0, 255, 255, 255}, {255, 255, 255, 255},
}

// palette is xterm's 256-colour palette: 16 named colours, a 6x6x6 cube,
// and 24 greys.
func palette(n int) color.RGBA {
	switch {
	case n < 16:
		return ansi16[n]
	case n < 232:
		n -= 16
		level := func(v int) uint8 {
			if v == 0 {
				return 0
			}
			return uint8(55 + 40*v)
		}
		return color.RGBA{level(n / 36), level(n / 6 % 6), level(n % 6), 255}
	default:
		g := uint8(8 + 10*(n-232))
		return color.RGBA{g, g, g, 255}
	}
}

// Cell is one character cell of a snapshot.
type Cell struct {
	R                        rune
	FG, BG                   color.RGBA
	fgIndex                  int // palette index of FG when set from the first 8, else -1
	Bold, Underline, Reverse bool
	Wide                     bool // takes this cell and the next
	Skip                     bool // the second half of a wide character
}

// Colours applies bold-brightening and reverse video.
func (c Cell) Colours() (color.RGBA, color.RGBA) {
	fg, bg := c.FG, c.BG
	if c.Bold && c.fgIndex >= 0 && c.fgIndex < 8 {
		fg = palette(c.fgIndex + 8)
	}
	if c.Reverse {
		fg, bg = bg, fg
	}
	return fg, bg
}

func isWide(r rune) bool {
	k := width.LookupRune(r).Kind()
	return k == width.EastAsianWide || k == width.EastAsianFullwidth
}

// ParseLine turns one line of `tmux capture-pane -e` output (text with SGR
// sequences) into exactly cols cells.
func ParseLine(line string, cols int) []Cell {
	cells := make([]Cell, 0, cols)
	pen := Cell{FG: defaultFG, BG: defaultBG, fgIndex: -1}
	for i := 0; i < len(line) && len(cells) < cols; {
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
			end := i + 2
			for end < len(line) && (line[end] < 0x40 || line[end] > 0x7e) {
				end++
			}
			if end < len(line) && line[end] == 'm' {
				applySGR(&pen, line[i+2:end])
			}
			i = end + 1
			continue
		}
		r, size := rune(line[i]), 1
		if r >= 0x80 {
			r, size = decodeRune(line[i:])
		}
		i += size
		c := pen
		c.R = r
		if isWide(r) && len(cells)+1 < cols {
			c.Wide = true
			cells = append(cells, c)
			c.Skip, c.Wide, c.R = true, false, ' '
		}
		cells = append(cells, c)
	}
	blank := Cell{R: ' ', FG: defaultFG, BG: defaultBG, fgIndex: -1}
	for len(cells) < cols {
		cells = append(cells, blank)
	}
	return cells
}

func decodeRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return ' ', 1
}

// applySGR updates the pen from one SGR parameter list ("1;38;5;214").
func applySGR(pen *Cell, params string) {
	if params == "" {
		params = "0"
	}
	p := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	n := make([]int, len(p))
	for i, s := range p {
		n[i], _ = strconv.Atoi(s)
	}
	for i := 0; i < len(n); i++ {
		switch v := n[i]; {
		case v == 0:
			*pen = Cell{FG: defaultFG, BG: defaultBG, fgIndex: -1}
		case v == 1:
			pen.Bold = true
		case v == 4:
			pen.Underline = true
		case v == 7:
			pen.Reverse = true
		case v == 22:
			pen.Bold = false
		case v == 24:
			pen.Underline = false
		case v == 27:
			pen.Reverse = false
		case v >= 30 && v <= 37:
			pen.FG, pen.fgIndex = palette(v-30), v-30
		case v == 39:
			pen.FG, pen.fgIndex = defaultFG, -1
		case v >= 40 && v <= 47:
			pen.BG = palette(v - 40)
		case v == 49:
			pen.BG = defaultBG
		case v >= 90 && v <= 97:
			pen.FG, pen.fgIndex = palette(v-90+8), -1
		case v >= 100 && v <= 107:
			pen.BG = palette(v - 100 + 8)
		case (v == 38 || v == 48) && i+2 < len(n) && n[i+1] == 5:
			c := palette(n[i+2])
			if v == 38 {
				pen.FG, pen.fgIndex = c, -1
			} else {
				pen.BG = c
			}
			i += 2
		case (v == 38 || v == 48) && i+4 < len(n) && n[i+1] == 2:
			c := color.RGBA{uint8(n[i+2]), uint8(n[i+3]), uint8(n[i+4]), 255}
			if v == 38 {
				pen.FG, pen.fgIndex = c, -1
			} else {
				pen.BG = c
			}
			i += 4
		}
	}
}
```

- [ ] **Step 5: Add the fallback chain to `internal/fonts`**

Embed each fallback font chosen in Task 15 (one `//go:embed` variable and one parsed `*opentype.Font` each, exactly as `mono` is), and add:
```go
// MonoChain is DejaVu Sans Mono and then the fallbacks, in the order
// renderers should try them.
func MonoChain() []*opentype.Font { return append([]*opentype.Font{mono}, fallbacks...) }
```
where `fallbacks` is a package variable listing the parsed fallback fonts in the spike's chosen order.

- [ ] **Step 6: Replace the renderer in `internal/term/render.go`**

Replace `foreground`, `backdrop`, `sgr`, the `renderer` type, `newRenderer`, and `draw` with the code below, and delete the now-unused `regexp` and `strings` imports if nothing else uses them. In `Render`, after the take loop, add `r.warnMissing(stdout)`.
```go
// renderer draws snapshots cell by cell, cols x rows filling 96% of the frame.
type renderer struct {
	px                   image.Point
	chain                []*opentype.Font
	faces                []font.Face
	cols, rows           int
	cellW, cellH, ascent int
	buf                  sfnt.Buffer
	missing              []rune
	seen                 map[rune]bool
}

func newRenderer(px image.Point, cols, rows int) *renderer {
	chain := fonts.MonoChain()
	probe := fonts.Face(chain[0], 100)
	adv, _ := probe.GlyphAdvance('M')
	height := probe.Metrics().Height
	size := math.Min(
		float64(px.X)*0.96/(float64(cols)*float64(adv)/64/100),
		float64(px.Y)*0.96/(float64(rows)*float64(height)/64/100))
	r := &renderer{px: px, chain: chain, cols: cols, rows: rows, seen: map[rune]bool{}}
	for _, f := range chain {
		r.faces = append(r.faces, fonts.Face(f, size))
	}
	adv, _ = r.faces[0].GlyphAdvance('M')
	m := r.faces[0].Metrics()
	r.cellW, r.cellH, r.ascent = adv.Ceil(), m.Height.Ceil(), m.Ascent.Ceil()
	return r
}

func (r *renderer) draw(e Entry) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, r.px.X, r.px.Y))
	draw.Draw(img, img.Bounds(), image.NewUniform(defaultBG), image.Point{}, draw.Src)
	ox, oy := (r.px.X-r.cols*r.cellW)/2, (r.px.Y-r.rows*r.cellH)/2
	lines := strings.Split(e.Screen, "\n")
	for y := 0; y < r.rows; y++ {
		line := ""
		if y < len(lines) {
			line = lines[y]
		}
		for x, c := range ParseLine(line, r.cols) {
			if c.Skip {
				continue
			}
			w := r.cellW
			if c.Wide {
				w *= 2
			}
			if e.Cursor && x == e.CursorX && y == e.CursorY {
				c.Reverse = !c.Reverse
			}
			fg, bg := c.Colours()
			cell := image.Rect(ox+x*r.cellW, oy+y*r.cellH, ox+x*r.cellW+w, oy+(y+1)*r.cellH)
			draw.Draw(img, cell, image.NewUniform(bg), image.Point{}, draw.Src)
			if c.Underline {
				under := image.Rect(cell.Min.X, cell.Max.Y-2, cell.Max.X, cell.Max.Y-1)
				draw.Draw(img, under, image.NewUniform(fg), image.Point{}, draw.Src)
			}
			if c.R == ' ' {
				continue
			}
			i := fonts.Find(r.chain, c.R, &r.buf)
			if i < 0 {
				// a visible box, never a silent blank
				r.note(c.R)
				box := cell.Inset(max(1, r.cellW/6))
				for _, edge := range []image.Rectangle{
					{box.Min, image.Pt(box.Max.X, box.Min.Y+1)}, {image.Pt(box.Min.X, box.Max.Y-1), box.Max},
					{box.Min, image.Pt(box.Min.X+1, box.Max.Y)}, {image.Pt(box.Max.X-1, box.Min.Y), box.Max},
				} {
					draw.Draw(img, edge, image.NewUniform(fg), image.Point{}, draw.Src)
				}
				continue
			}
			d := font.Drawer{Dst: img, Src: image.NewUniform(fg), Face: r.faces[i],
				Dot: fixed.P(cell.Min.X, cell.Min.Y+r.ascent)}
			d.DrawString(string(c.R))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *renderer) note(ch rune) {
	if !r.seen[ch] {
		r.seen[ch] = true
		r.missing = append(r.missing, ch)
	}
}

// warnMissing says which characters no font could draw, so a box in a frame
// is never a silent defect.
func (r *renderer) warnMissing(stdout io.Writer) {
	if len(r.missing) > 0 {
		fmt.Fprintf(stdout, "WARN       no font here can draw %s; those cells show a box. "+
			"Look at the frames before you use them.\n", fonts.Describe(r.missing))
	}
}
```
Add imports `"golang.org/x/image/font/opentype"` and `"golang.org/x/image/font/sfnt"`.

- [ ] **Step 7: Generate the golden frames and look at them**

```bash
MOVIE_UPDATE_GOLDEN=1 go test ./internal/term -run Golden
open internal/term/testdata/golden-*.png 2>/dev/null || xdg-open internal/term/testdata/golden-0.png
```
Expected: colours, bold, the wide character across two cells, box drawing joined into lines, and every spike symbol drawn (no boxes). Only commit the goldens once they look right.

- [ ] **Step 8: Run all tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/term internal/fonts
git commit -m "Render terminal snapshots cell by cell with colours, wide characters, a cursor, and visible glyph fallback"
```

---

### Task 18: Filming inside containers; retire the Python recorders

**Files:**
- Modify: `cmd/movie/term_test.go`, `examples/e2e/README.md`, `examples/e2e/Dockerfile`, `skills/proving-it-works-with-a-movie/recording-a-terminal.md` (replace)
- Create: `examples/e2e/film.sh`
- Delete: `examples/film-terminal.py`, `skills/proving-it-works-with-a-movie/examples/film-terminal.py`, `skills/proving-it-works-with-a-movie/scripts/browser_tools.py`, `tests/proving-it-works-with-a-movie/` (the whole directory: the remaining suites test only these files)
- Modify: `README.md` (Tests section already says `go test ./...`; drop any mention of the Python suite)

- [ ] **Step 1: Write the failing test**

Append to `cmd/movie/term_test.go`:
```go
// A pass-through wrapper stands in for docker exec: every tmux call goes
// through it, and the prompt still installs.
func TestWrappedSessionRunsThroughTheWrapper(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir, "--", "env", "MOVIE_WRAPPED=yes")
	r := runMovie(t, dir, "term", "run", session, "echo wrapped:$MOVIE_WRAPPED")
	if r.code != 0 || !strings.Contains(r.stdout, "wrapped:yes") {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./cmd/movie -run Wrapped`
Expected: PASS if the wrapper plumbing from Task 6 is right; if it fails, fix `Session.tmux` or `Start` until it passes. (The session's `socket` and `history` for a wrapped session live in `/tmp` inside the target; with `env` as the wrapper that is the host's `/tmp`.)

- [ ] **Step 3: Replace the e2e recorder with a movie term script**

`examples/e2e/film.sh`:
```bash
#!/usr/bin/env bash
# Film the e2e demo's terminal beats inside the running container, from the
# host. The container needs tmux; movie and the recording stay out here.
#
# Usage: examples/e2e/film.sh CONTAINER
set -euo pipefail
container=$1
here=$(cd "$(dirname "$0")" && pwd)
movie=$here/../../skills/proving-it-works-with-a-movie/bin/movie
work=$here/takes-$(date +%s)
wrap=(-- docker exec "$container")

"$movie" term start "$work/install" --cwd /work --size 125x34 "${wrap[@]}"
"$movie" term run "$work/install" 'claude plugin list'
"$movie" term run "$work/install" 'claude plugin marketplace add prime-radiant-inc/proving-it-works' --timeout 120
"$movie" term run "$work/install" 'claude plugin install proving-it-works' --timeout 120
"$movie" term run "$work/install" 'claude plugin list'
"$movie" term stop "$work/install" "$here/install"

"$movie" term start "$work/agent" --cwd /work --size 125x34 "${wrap[@]}"
"$movie" term run "$work/agent" 'claude --permission-mode bypassPermissions -p "Use the proving-it-works-with-a-movie skill. Make a ~12s NARRATED movie proving /work/app/index.html counts 0 to 1 to 2 when clicked. Motion route: record continuous frames from headless chromium with the cursor overlay drawn. No API key here, so narrate with the local engine. Build it with movie build and check it. Save to /work/out/counter.mp4."' --timeout 20 || true
"$movie" term film "$work/agent" off
"$movie" term wait "$work/agent" --timeout 1800
"$movie" term stop "$work/agent" "$here/agent"

"$movie" term start "$work/verify" --cwd /work --size 125x34 "${wrap[@]}"
"$movie" term run "$work/verify" 'ls -la out/counter.mp4'
"$movie" term run "$work/verify" 'SKILL=$(dirname "$(find ~/.claude/plugins -path "*proving-it-works-with-a-movie*" -name SKILL.md | head -1)")'
"$movie" term run "$work/verify" '"$SKILL/bin/movie" check out/counter.mp4' --timeout 120
"$movie" term stop "$work/verify" "$here/verify"
```
```bash
chmod +x examples/e2e/film.sh
```
The frames land in `examples/e2e/install/take-1`, `agent/take-1`, and `verify/take-1`; update `examples/e2e/scenes.yaml`'s `frames:` paths to those, and set their `rate:` to `10`.

In `examples/e2e/Dockerfile`, remove the ttyd install block and the `chromium` package and the `CHROME_PATH`/`PUPPETEER_EXECUTABLE_PATH` lines only if the in-container agent no longer needs Chromium (it does: the agent films a web app), so keep `chromium` and those lines, and remove only ttyd. Update the file's comment about ttyd accordingly.

In `examples/e2e/README.md`, replace the `docker run ... ttyd ...` block and the three `film-terminal.py` lines with:
```bash
docker build -t proving-e2e .
docker run -d --name provdemo -e CLAUDE_CODE_OAUTH_TOKEN -v "$PWD/work:/work" proving-e2e sleep infinity
./film.sh provdemo
../../skills/proving-it-works-with-a-movie/bin/movie build scenes.yaml demo.mp4
```
and change the pieces-table row for `../film-terminal.py` to `` | `film.sh` | films the container's terminal from the host with `movie term`, through `docker exec` | ``.

- [ ] **Step 4: Replace `recording-a-terminal.md`**

````markdown
# Recording a terminal

CLIs, TUIs, installs, test runs, agents at work: a large share of what is
worth proving happens in a terminal. `movie term` films one. tmux holds a
clean bash; you drive it one command at a time, with your own tool calls, a
sub-agent, or a script; `stop` renders what it showed into frames for
`movie build`. It runs on macOS, Linux, and WSL (on Windows, run it inside
WSL).

```bash
m="$SKILL_DIR/bin/movie"
"$m" term start take/                       # a clean bash, 120x34, filming
"$m" term run take/ 'npm test'              # types it, waits for the prompt
"$m" term screen take/                      # what is on screen, as text
"$m" term stop take/ frames/                # frames/take-1/ + take.json
```

`frames/take-1/take.json` holds a ready-to-paste scene:
`frames: .../take-1`, `rate: 10`.

## The verbs

| Verb | Does | Exit |
|---|---|---|
| `start SESSION [--cwd DIR] [--size 120x34] [-- WRAPPER...]` | start the session and the recorder, then return | 0, or 2 |
| `run SESSION 'cmd' [--timeout 60]` | type the command at human pace, press Enter, wait for the prompt; print the outcome and the screen | 0 succeeded, 1 failed, 2 refused (a command is still running), 3 still running at the timeout |
| `type SESSION 'text'` | type into whatever is running, such as a TUI's input box | 0 |
| `key SESSION NAME` | `Enter`, `Escape`, `Tab`, `Up`, `Down`, `Left`, `Right`, `C-c`, or one character | 0 |
| `wait SESSION [--quiet S] [--timeout 60]` | wait for the prompt (0 or 1), or for the screen to hold still for S seconds (3, still running) | |
| `screen SESSION` | print the screen as text | 0 |
| `film SESSION on\|off` | keep what follows out of the movie; each `on` starts a new take | 0 |
| `stop SESSION OUTDIR` | end the session and render every take | 0 |
| `render SESSION OUTDIR [--px 1600x900]` | render again from the recording, even after the session is gone | 0 |

`run` refuses while a command is still running, so keys never land in a
running program's stdin; `wait` for it, or send `key C-c`. Session and
output directories must be new or empty.

## Long work does not belong inside one take

An agent run or a build takes minutes. Film the command being issued, then
`film off`, `wait` for it, `film on`, and film the result: the movie cuts
from the command to its result, and a card in the scene file can say how
long it took. The work is real; the tedium is not. Time inside a take is
never compressed.

## The session is clean

bash starts with no startup files and its own history file, so the take
shows none of your aliases, prompts, or past commands, and nothing typed on
camera lands in your history. Tools on a PATH set in your `.bashrc` need
their full path, or `export PATH=...` as the first `run`.

## Inside a container

```bash
"$m" term start take/ --cwd /work -- docker exec CONTAINER
```

Every tmux command then runs through `docker exec`: the shell lives in the
container, `movie` and the recording stay on the host. The container needs
tmux. `ssh` is not supported.

## Glyphs

Frames are drawn with DejaVu Sans Mono plus fallback fonts that cover the
symbols CLIs print (Claude Code's `⏺` and `⎿`, spinners, box drawing).
Anything no font covers (CJK, emoji) is drawn as a visible box, and `stop`
prints a `WARN` naming each such character. Look at the frames before you
use them.

## Playing a movie inside the terminal

`mpv --vo=tct movie.mp4` renders video as coloured terminal cells. It proves
a file plays where it was made, and it looks blocky. For a demo where the
viewer should see the movie, use a `movie` scene in the scene file instead.
````

- [ ] **Step 5: Delete the Python recorders and their tests**

```bash
git rm examples/film-terminal.py skills/proving-it-works-with-a-movie/examples/film-terminal.py \
       skills/proving-it-works-with-a-movie/scripts/browser_tools.py
git rm -r tests/proving-it-works-with-a-movie
```
If `skills/proving-it-works-with-a-movie/scripts/` and `examples/` under the skill are now empty, they are gone. In `README.md`, remove any remaining mention of the Python test suite.

- [ ] **Step 6: Verify nothing still points at a deleted file**

Run: `git grep -n -E "film-terminal|browser_tools|ttyd|run-tests.py|tests/proving-it-works" -- ':!docs/superpowers'`
Expected: no output.

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Film the e2e demo once, for real**

Build the e2e image, start the container, run `examples/e2e/film.sh provdemo`, and look at a frame from each take. Expected: the install take shows the plugin commands and their output, legible; the agent take shows the command issued; the verify take shows `movie check` passing. If Docker is unavailable, say so in the task report instead of skipping silently.

- [ ] **Step 8: Commit**

```bash
git status
git add -A examples skills tests README.md cmd
git commit -m "Film containers through docker exec with movie term, and retire the Python recorders"
```

---

### Task 19: The instruction routes and the final SKILL.md

**Files:**
- Modify: `skills/proving-it-works-with-a-movie/SKILL.md`, `recording-motion.md`, `rendering-from-a-log.md`, `rendering-stills.md`, `README.md`

- [ ] **Step 1: Update the route table and gate in `SKILL.md`**

Replace the `## Pick the route` table with:
```markdown
| What you have to show | Route |
|---|---|
| Interaction happening: typing, clicking, a list updating live | Drive a browser and capture frames → recording-motion.md |
| A desktop app's window | Capture the window with ffmpeg → recording-motion.md |
| A CLI, a TUI, an install, a test run, an agent working | `movie term` → recording-a-terminal.md |
| A sequence of real states, motion optional | Screenshots as `image` scenes → rendering-stills.md |
| OS capture blocked (wallpaper-only frames), or the thing to prove is a *run*, not a UI | Frames rendered from the run's own log → rendering-from-a-log.md |

Every route ends in `movie build` (assembling.md) and its gate.
```
Delete the paragraph beginning `On native Windows, use the complete PowerShell or Git Bash sequence` if any of it remains.

- [ ] **Step 2: Add the desktop-window section to `recording-motion.md`**

Replace the `## Native Windows desktop capture` section with:
````markdown
## Capturing a desktop app's window

One ffmpeg command per OS captures a window or the screen into frames that
`movie build` takes as a `frames` scene. First capture two seconds and look
at a still: when screen-recording permission is missing, capture
"succeeds" and records wallpaper.

```bash
# macOS: list devices, then capture screen N (grant Screen Recording to your terminal)
ffmpeg -f avfoundation -list_devices true -i ""
ffmpeg -nostdin -f avfoundation -framerate 10 -capture_cursor 1 -i 'N:none' -t 2 -vf fps=10 check/f%04d.png

# Linux (X11)
ffmpeg -nostdin -f x11grab -framerate 10 -i "$DISPLAY" -t 2 -vf fps=10 check/f%04d.png
```

```powershell
# Windows: one window by its exact title
ffmpeg -nostdin -f gdigrab -framerate 10 -i 'title=Your application window title' -t 2 -vf fps=10 check/f%04d.png
```

Look at `check/f0010.png`. Only visible application pixels are a capture;
wallpaper or a blank window means permission is blocked. Then capture the
real take into a new directory and use it as `frames: that-directory`,
`rate: 10`. If no capture shows the app, use the browser route or a reel
rendered from the log, and say what remains unproven.
````
Also, in the section `## Screenshot-based capture: navigation orphans an in-flight capture`, add one sentence at the end: `Write each frame as a numbered PNG into one directory; that directory is a `frames` scene.`

- [ ] **Step 3: Point the log and stills routes at `movie build`**

In `rendering-from-a-log.md`, under `## Draw frames from the log`, replace the Python snippet's ffmpeg pipe with writing numbered PNGs: replace the code block with:
```python
from pathlib import Path
out = Path("frames/reel"); out.mkdir(parents=True, exist_ok=True)
index = 0
for nframes, render in scenes:                     # render(t) -> PIL RGB image
    for i in range(nframes):
        render(i / max(1, nframes - 1)).save(out / f"f{index:05d}.png")
        index += 1
```
and after it add: `Then use `frames: frames/reel` with the rate you rendered at, in a scene file for `movie build`.` Replace `## Gate it` with:
```markdown
## Gate it

`movie build` runs the gate on the reel. A silent reel with no narration is
checked as silent and passes; add narration to explain it. Then open the
contact sheet and confirm the panels are legible at full size: a reel nobody
can read proves nothing.
```

In `rendering-stills.md`, replace `## 2. Sequence the screenshots as they are` through the end of `## 3.` with sections saying: each screenshot becomes an `image` scene with its own `narration`; `movie build` holds each for max(narration, `duration`); title and end cards are `card` scenes, so no compositing is needed; and replace `## 4. Gate it` with `movie build` runs the gate; open the contact sheet. Remove the `-framerate 1/3` advice and the `card-00`/`shot-99` naming advice, which no longer apply.

- [ ] **Step 4: Verify the docs are consistent**

Run: `git grep -n -E "scripts/|check-movie|ttyd|playwright install ffmpeg|drawtext" -- skills README.md`
Expected: no output except intentional mentions (the `drawtext` warning in assembling.md was removed in Task 14; if any line remains, decide whether it is still true and fix it).

Read SKILL.md top to bottom once, as an agent seeing it for the first time, and fix anything that sends the reader to a file, flag, or command that no longer exists.

- [ ] **Step 5: Commit**

```bash
git add skills README.md
git commit -m "Rewrite the instruction routes around movie build, and finish SKILL.md"
```

---

### Task 20: Release binaries

**Files:**
- Create: `skills/proving-it-works-with-a-movie/bin/movie-{darwin-arm64,darwin-amd64,linux-amd64,linux-arm64,windows-amd64.exe}`

- [ ] **Step 1: Build**

Run: `script/build-binaries`
Expected: five `built ...` lines.

- [ ] **Step 2: Verify the freshness test and the launcher against the real files**

Run: `MOVIE_RELEASE_CHECK=1 go test ./cmd/movie -run Committed -v`
Expected: PASS.

Run: `skills/proving-it-works-with-a-movie/bin/movie help`
Expected: the usage text.

- [ ] **Step 3: Run everything once more**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add skills/proving-it-works-with-a-movie/bin
git commit -m "Build the release binaries of movie"
```

Merging this branch to `main` is Jesse's call; CI on `main` runs the freshness check.
