# Download on First Run Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop committing movie's binaries: the launchers download this machine's release binary on first run from the GitHub release, verify it against committed checksums, and cache it.

**Architecture:** `BIN/movie` (POSIX sh) and `BIN/movie.ps1` (Windows PowerShell) read `BIN/VERSION` and `BIN/checksums.txt`, download `vVERSION/NAME` into a hash-keyed cache, verify, and exec. `script/release` builds the six binaries with the existing reproducible `script/build-binaries`, writes the checksums, bumps the one version with everyharness, and publishes a GitHub release. A release check in CI rebuilds from source and compares hashes.

**Tech Stack:** POSIX sh, bash 3.2+, PowerShell 5.1, Go 1.26 tests (`net/http/httptest`), git, gh, everyharness.

**Spec:** `docs/superpowers/specs/2026-09-26-binary-distribution-design.md`

## Global Constraints

- `BIN` is `skills/proving-it-works-with-a-movie/bin`; the tests' `binDir` constant already names it.
- Release asset names, exactly: `movie-darwin-arm64`, `movie-darwin-amd64`, `movie-linux-amd64`, `movie-linux-arm64`, `movie-windows-amd64.exe`, `movie-windows-arm64.exe`.
- Download URL: `https://github.com/prime-radiant-inc/proving-it-works/releases/download/vVERSION/NAME`; `MOVIE_RELEASE_URL` replaces everything before `/vVERSION`.
- Cache: `ROOT/proving-it-works/SHA256/NAME`, ROOT = `%LOCALAPPDATA%` on Windows (both launchers), else `$XDG_CACHE_HOME`, else `~/.cache`; the system temp directory when ROOT cannot be written. Source builds go in `ROOT/proving-it-works/source/`.
- `BIN/checksums.txt` is `sha256sum` output: one `HASH  NAME` line per asset, lowercase hex, LF.
- Every launcher failure exits 2 with a message starting `movie: ` on stderr. A launcher never builds by itself except under `MOVIE_FROM_SOURCE=1`.
- `BIN/movie` must run under dash (Ubuntu's `sh`); bash scripts must run under macOS's bash 3.2 (no empty-array expansion under `set -u`, no `mapfile`).
- Environment variables: `MOVIE_RELEASE_URL`, `MOVIE_FROM_SOURCE=1` (sh launcher only), `MOVIE_RELEASE_CHECK=1`, `RELEASE_DRY_RUN=1`, `EVERYHARNESS` (path to everyharness's built `dist/cli.js`).
- No new Go module dependencies. No mocks: launcher tests serve the real test-built `movie` binary from a local HTTP server.
- Never `git add -A`; never commit a binary.
- House rules (CLAUDE.md): TDD for every change, test output pristine, match surrounding style, no emdashes in prose.

## File Structure

- `script/build-binaries` (modify): OUTDIR required, optional `OS/ARCH` targets, adds `windows/arm64`.
- `skills/proving-it-works-with-a-movie/bin/movie` (rewrite): the sh launcher.
- `skills/proving-it-works-with-a-movie/bin/movie.ps1` (create): the PowerShell launcher.
- `script/release` (create): the release procedure.
- `cmd/movie/launcher_test.go` (create): both launchers' tests and their fake release.
- `cmd/movie/release_test.go` (modify): build-binaries, release script, and release check tests.
- `skills/proving-it-works-with-a-movie/SKILL.md`, `README.md`, `examples/tour/film.sh`, `.gitattributes`, `.gitignore` (modify).
- `skills/proving-it-works-with-a-movie/bin/movie-*` (delete, Task 6).

`BIN/VERSION` and `BIN/checksums.txt` are written only by `script/release` (Task 7). Between Task 2 and the release, the launcher in this checkout has no release to download: run it with `MOVIE_FROM_SOURCE=1` (Task 3).

---

### Task 1: build-binaries takes targets and builds Windows ARM64

**Files:**
- Modify: `script/build-binaries`
- Test: `cmd/movie/release_test.go`

**Interfaces:**
- Produces: `script/build-binaries OUTDIR [OS/ARCH...]`, exit 2 on no OUTDIR or an unknown target; with no targets, builds all six into OUTDIR as `movie-OS-ARCH[.exe]`.

- [ ] **Step 1: Write the failing tests** (append to `cmd/movie/release_test.go`)

```go
// buildBinaries runs script/build-binaries with args from the repo root.
func buildBinaries(t *testing.T, args ...string) result {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command("bash", append([]string{filepath.Join(root, "script", "build-binaries")}, args...)...)
	cmd.Dir = root
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// build-binaries needs somewhere to put the binaries: they never go in the
// repo, and an unknown target is refused rather than guessed at.
func TestBuildBinariesRefusesAMissingOutdirOrUnknownTarget(t *testing.T) {
	if got := buildBinaries(t); got.code != 2 || !strings.Contains(got.stderr, "usage: script/build-binaries OUTDIR") {
		t.Errorf("no OUTDIR: exit %d\n%s", got.code, got.stderr)
	}
	got := buildBinaries(t, t.TempDir(), "plan9/amd64")
	if got.code != 2 || !strings.Contains(got.stderr, "unknown target plan9/amd64") || !strings.Contains(got.stderr, "windows/arm64") {
		t.Errorf("unknown target: exit %d\n%s", got.code, got.stderr)
	}
}

// Named targets build only those; Windows on ARM is one of them.
func TestBuildBinariesBuildsOnlyTheNamedTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles movie")
	}
	out := t.TempDir()
	if got := buildBinaries(t, out, "windows/arm64"); got.code != 0 {
		t.Fatalf("exit %d\n%s", got.code, got.stderr)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "movie-windows-arm64.exe" {
		t.Fatalf("built %v, want only movie-windows-arm64.exe", entries)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./cmd/movie -run 'TestBuildBinaries' -count=1`
Expected: FAIL. With no OUTDIR the old script builds into `BIN` and exits 0; with `plan9/amd64` it ignores the extra argument and builds all five. The builds are reproducible, so `BIN`'s committed binaries come back byte-identical; confirm with `git status --short skills/`, and if anything shows, `git checkout -- skills/proving-it-works-with-a-movie/bin`.

- [ ] **Step 3: Rewrite `script/build-binaries`**

```bash
#!/usr/bin/env bash
# Build movie's release binaries, reproducibly.
#
# When: script/release builds all six to publish them; the release check
# (MOVIE_RELEASE_CHECK=1) builds them to compare with bin/checksums.txt;
# MOVIE_FROM_SOURCE=1 and examples/tour build one. The binaries ship as
# GitHub release assets: never commit them.
#
# Usage: script/build-binaries OUTDIR [OS/ARCH...]
#   With no OS/ARCH, builds every release target.
set -euo pipefail
all="darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64"
if [[ $# -lt 1 ]]; then
  echo "usage: script/build-binaries OUTDIR [OS/ARCH...]  (targets: $all)" >&2
  exit 2
fi
root=$(cd "$(dirname "$0")/.." && pwd)
out=$1
shift
if [[ $# -eq 0 ]]; then
  set -- $all
fi
for target in "$@"; do
  case " $all " in
    *" $target "*) ;;
    *) echo "build-binaries: unknown target $target; one of: $all" >&2; exit 2 ;;
  esac
done

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
for target in "$@"; do
  goos=${target%/*}
  goarch=${target#*/}
  name=movie-$goos-$goarch
  [[ $goos == windows ]] && name+=.exe
  (cd "$root" && GOTOOLCHAIN=go$version GOOS=$goos GOARCH=$goarch GOAMD64=v1 GOARM64=v8.0 \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$out/$name" ./cmd/movie)
  echo "built $name"
done
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/movie -run 'TestBuildBinaries' -count=1 -v`
Expected: both PASS. Also `git status --short skills/` shows nothing: no binaries were written into `BIN`.

- [ ] **Step 5: Commit**

```bash
git add script/build-binaries cmd/movie/release_test.go
git commit -m "build-binaries: require OUTDIR, take targets, and build Windows ARM64"
```

---

### Task 2: The sh launcher downloads, verifies, caches, and runs

**Files:**
- Rewrite: `skills/proving-it-works-with-a-movie/bin/movie`
- Create: `cmd/movie/launcher_test.go`
- Modify: `cmd/movie/release_test.go` (delete `TestLauncherPassesArgumentsThroughUntouched` and `hostBinaryName`, which the new tests replace)

**Interfaces:**
- Consumes: `movieBin`, `result{code int; stdout, stderr string}`, `repoRoot`, `copyFile`, `binDir` from the existing tests.
- Produces (in `launcher_test.go`, used by Tasks 3 and 4): `testVersion`, `needSh(t)`, `sha256Hex([]byte) string`, `writeText(t, path, text)`, `filesUnder(t, dir) []string`, `hostReleaseName() string`, `hostBinary(t) []byte`, `type fakeRelease` with fields `bin, cache, url, want string`, `downloads atomic.Int32`, `requested sync.Map`, and methods `sh(t, env, args...) result`, `run(t, *exec.Cmd, env) result`, `installed() string`; `newFakeRelease(t, want string, body []byte, delay time.Duration) *fakeRelease`.

- [ ] **Step 1: Write the failing tests** (`cmd/movie/launcher_test.go`)

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testVersion is the version the launcher tests pretend is released.
const testVersion = "9.9.9"

func needSh(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh on PATH (Git Bash on Windows)")
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeText(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// filesUnder lists every file below dir, so a test can see what a launcher
// left behind.
func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// hostReleaseName is the release asset the launchers pick on this machine.
func hostReleaseName() string {
	if runtime.GOOS == "windows" {
		return "movie-windows-" + runtime.GOARCH + ".exe"
	}
	return "movie-" + runtime.GOOS + "-" + runtime.GOARCH
}

// hostBinary is the movie binary under test, which the fake release serves
// as this machine's asset.
func hostBinary(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(movieBin)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fakeRelease is a plugin bin directory holding the launchers, a VERSION,
// and a checksums.txt, with a local server standing in for GitHub releases
// and a cache root of its own.
type fakeRelease struct {
	bin, cache, url string
	want            string // the SHA-256 checksums.txt gives this machine's asset
	downloads       atomic.Int32
	requested       sync.Map // every path the server was asked for
}

// newFakeRelease serves body as this machine's asset after delay (a 404
// when body is nil), and lists want as its checksum.
func newFakeRelease(t *testing.T, want string, body []byte, delay time.Duration) *fakeRelease {
	t.Helper()
	r := &fakeRelease{bin: t.TempDir(), cache: t.TempDir(), want: want}
	copyFile(t, filepath.Join(repoRoot(t), binDir, "movie"), filepath.Join(r.bin, "movie"), 0o755)
	writeText(t, filepath.Join(r.bin, "VERSION"), testVersion+"\n")
	writeText(t, filepath.Join(r.bin, "checksums.txt"), want+"  "+hostReleaseName()+"\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.requested.Store(req.URL.Path, true)
		if body == nil || req.URL.Path != "/v"+testVersion+"/"+hostReleaseName() {
			http.NotFound(w, req)
			return
		}
		r.downloads.Add(1)
		time.Sleep(delay)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	r.url = srv.URL
	return r
}

// run runs a launcher command against the fake release and cache, with env
// added. It reports rather than stops on a failure to start, so it can run
// in goroutines.
func (r *fakeRelease) run(t *testing.T, cmd *exec.Cmd, env []string) result {
	t.Helper()
	cmd.Env = append(os.Environ(), "MOVIE_RELEASE_URL="+r.url, "XDG_CACHE_HOME="+r.cache, "LOCALAPPDATA="+r.cache)
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Errorf("running %v: %v", cmd.Args, err)
		code = -1
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// sh runs the sh launcher with args.
func (r *fakeRelease) sh(t *testing.T, env []string, args ...string) result {
	t.Helper()
	return r.run(t, exec.Command("sh", append([]string{filepath.Join(r.bin, "movie")}, args...)...), env)
}

// installed is where the launchers keep the verified asset.
func (r *fakeRelease) installed() string {
	return filepath.Join(r.cache, "proving-it-works", r.want, hostReleaseName())
}

// The launcher downloads this machine's binary once, verifies and caches it,
// and passes arguments, output, and exit codes through untouched.
func TestLauncherDownloadsVerifiesAndCachesTheBinary(t *testing.T) {
	needSh(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	got := r.sh(t, nil, "a b%c;d")
	if got.code != 2 || !strings.Contains(got.stderr, `unknown command "a b%c;d"`) {
		t.Fatalf("argument did not arrive intact (exit %d):\n%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "downloading "+hostReleaseName()+" "+testVersion) {
		t.Errorf("no download notice:\n%s", got.stderr)
	}
	if data, err := os.ReadFile(r.installed()); err != nil || sha256Hex(data) != r.want {
		t.Fatalf("not cached at %s: %v", r.installed(), err)
	}
	help := r.sh(t, nil, "help")
	if help.code != 0 || !strings.Contains(help.stdout, "Exit codes:") {
		t.Errorf("help: exit %d\n%s%s", help.code, help.stdout, help.stderr)
	}
	if n := r.downloads.Load(); n != 1 || strings.Contains(help.stderr, "downloading") {
		t.Errorf("%d downloads, want 1: the second run must use the cache\n%s", n, help.stderr)
	}
}

// A download that does not match checksums.txt is never run or kept.
func TestLauncherRefusesABinaryWithTheWrongChecksum(t *testing.T) {
	needSh(t)
	body := hostBinary(t)
	wrong := sha256Hex([]byte("not the release"))
	r := newFakeRelease(t, wrong, body, 0)
	got := r.sh(t, nil, "help")
	if got.code != 2 || !strings.Contains(got.stderr, sha256Hex(body)) || !strings.Contains(got.stderr, wrong) {
		t.Fatalf("want exit 2 naming both hashes, got %d:\n%s", got.code, got.stderr)
	}
	if strings.Contains(got.stdout, "Exit codes:") {
		t.Error("ran a binary that failed its checksum")
	}
	if left := filesUnder(t, r.cache); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

// When the download fails, the message says what to fetch, where to put
// it, what it must hash to, and how to build it instead.
func TestLauncherSaysWhatToFetchWhenTheDownloadFails(t *testing.T) {
	needSh(t)
	want := sha256Hex(hostBinary(t))
	r := newFakeRelease(t, want, nil, 0)
	got := r.sh(t, nil, "help")
	if got.code != 2 {
		t.Errorf("exit %d, want 2", got.code)
	}
	url := r.url + "/v" + testVersion + "/" + hostReleaseName()
	for _, s := range []string{"could not download " + url, "proving-it-works/" + want + "/" + hostReleaseName(), "SHA-256 must be " + want, "MOVIE_FROM_SOURCE=1"} {
		if !strings.Contains(got.stderr, s) {
			t.Errorf("message lacks %q:\n%s", s, got.stderr)
		}
	}
	if left := filesUnder(t, r.cache); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

// A cache root the user cannot write sends the cache to the temp directory.
func TestLauncherCachesInTheTempDirectoryWhenTheCacheIsReadOnly(t *testing.T) {
	needSh(t)
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory this user cannot write")
	}
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	if err := os.Chmod(r.cache, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(r.cache, 0o700) })
	tmp := t.TempDir()
	got := r.sh(t, []string{"TMPDIR=" + tmp}, "help")
	if got.code != 0 || !strings.Contains(got.stderr, "cannot write to "+r.cache) {
		t.Fatalf("exit %d:\n%s", got.code, got.stderr)
	}
	if _, err := os.Stat(filepath.Join(tmp, "proving-it-works", r.want, hostReleaseName())); err != nil {
		t.Errorf("not cached in the temp directory: %v", err)
	}
}

// First runs at the same moment, as parallel agent sessions make, all run,
// and leave one verified binary behind.
func TestLauncherFirstRunsAtOnceAllSucceed(t *testing.T) {
	needSh(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 300*time.Millisecond)
	results := make([]result, 4)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = r.sh(t, nil, "help")
		}()
	}
	wg.Wait()
	for i, got := range results {
		if got.code != 0 || !strings.Contains(got.stdout, "Exit codes:") {
			t.Errorf("run %d: exit %d\n%s", i, got.code, got.stderr)
		}
	}
	if left := filesUnder(t, r.cache); len(left) != 1 || left[0] != r.installed() {
		t.Errorf("cache holds %v, want only %s", left, r.installed())
	}
}

// Installing a release removes older releases' binaries, and nothing else.
func TestLauncherRemovesOtherVersionsAfterAnInstall(t *testing.T) {
	needSh(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	cache := filepath.Join(r.cache, "proving-it-works")
	old := filepath.Join(cache, sha256Hex([]byte("an older release")))
	keep := []string{filepath.Join(cache, "source"), filepath.Join(cache, "not-a-hash")}
	for _, d := range append([]string{old}, keep...) {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		writeText(t, filepath.Join(d, "f"), "x")
	}
	if got := r.sh(t, nil, "help"); got.code != 0 {
		t.Fatalf("exit %d\n%s", got.code, got.stderr)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("the older release is still cached: %v", err)
	}
	for _, d := range keep {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s was removed: %v", d, err)
		}
	}
}

// On Windows the launcher asks for the machine's native architecture, which
// an emulated Git Bash's uname hides.
func TestLauncherAsksWindowsForItsNativeArchitecture(t *testing.T) {
	needSh(t)
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	r := newFakeRelease(t, sha256Hex([]byte("x")), nil, 0)
	writeText(t, filepath.Join(r.bin, "checksums.txt"), r.want+"  movie-windows-arm64.exe\n")
	r.sh(t, []string{"PROCESSOR_ARCHITECTURE=ARM64", "PROCESSOR_ARCHITEW6432="}, "help")
	if _, ok := r.requested.Load("/v" + testVersion + "/movie-windows-arm64.exe"); !ok {
		t.Error("did not ask for movie-windows-arm64.exe")
	}
}
```

Then delete `TestLauncherPassesArgumentsThroughUntouched` and `hostBinaryName` from `cmd/movie/release_test.go`; `TestLauncherDownloadsVerifiesAndCachesTheBinary` covers argument pass-through. That leaves `release_test.go`'s `"runtime"` import unused: remove it. (`"bytes"` is still used there until Task 5.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./cmd/movie -run 'TestLauncher' -count=1`
Expected: FAIL. The old launcher looks for `movie-OS-ARCH` beside itself, finds none, and `exec` fails, so no test sees a download.

- [ ] **Step 3: Rewrite `skills/proving-it-works-with-a-movie/bin/movie`**

```sh
#!/bin/sh
# Run movie, downloading this machine's release binary on first use.
#
# The binary for the version in VERSION comes from that GitHub release, is
# checked against checksums.txt, and is cached at
#   ROOT/proving-it-works/SHA256/movie-OS-ARCH[.exe]
# keyed by its expected hash, so only a verified file is ever run from
# there. ROOT is %LOCALAPPDATA% on Windows, else $XDG_CACHE_HOME, else
# ~/.cache, or the temp directory when that cannot be written. Arguments
# and the exit code pass through untouched.
#
#   MOVIE_RELEASE_URL=URL  download URL/vVERSION/NAME instead of from GitHub
set -eu
here=$(cd "$(dirname "$0")" && pwd)

fail() {
  echo "movie: $*" >&2
  exit 2
}

# sha256 prints the SHA-256 of a file, or of standard input for -.
sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  MINGW*|MSYS*|CYGWIN*)
    os=windows
    # Git Bash rewrites Unix-looking arguments for native programs; stop it.
    # Pass paths in Windows form (cygpath -m) instead.
    MSYS_NO_PATHCONV=1
    MSYS2_ARG_CONV_EXCL='*'
    export MSYS_NO_PATHCONV MSYS2_ARG_CONV_EXCL ;;
  *) fail "unsupported system $(uname -s)" ;;
esac
if [ "$os" = windows ]; then
  # an emulated Git Bash on ARM says x86_64; Windows knows the machine
  case "${PROCESSOR_ARCHITEW6432:-${PROCESSOR_ARCHITECTURE:-AMD64}}" in
    ARM64) arch=arm64 ;;
    *) arch=amd64 ;;
  esac
  name=movie-windows-$arch.exe
else
  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) fail "unsupported machine $(uname -m)" ;;
  esac
  name=movie-$os-$arch
fi

if [ "$os" = windows ] && [ -n "${LOCALAPPDATA:-}" ]; then
  root=$(cygpath -u "$LOCALAPPDATA")
else
  root=${XDG_CACHE_HOME:-$HOME/.cache}
fi
cache=$root/proving-it-works
if ! mkdir -p "$cache" 2>/dev/null || [ ! -w "$cache" ]; then
  cache=${TMPDIR:-/tmp}/proving-it-works
  echo "movie: cannot write to $root; caching in $cache instead" >&2
  mkdir -p "$cache" || fail "cannot create $cache"
fi

[ -f "$here/VERSION" ] || fail "$here has no VERSION, so there is no release to download"
version=$(tr -d '\r\n' < "$here/VERSION")
want=$(tr -d '\r' < "$here/checksums.txt" | awk -v n="$name" '$2 == n { print $1 }')
[ -n "$want" ] || fail "checksums.txt has no line for $name"
dir=$cache/$want
bin=$dir/$name

if [ ! -f "$bin" ]; then
  url=${MOVIE_RELEASE_URL:-https://github.com/prime-radiant-inc/proving-it-works/releases/download}/v$version/$name
  mkdir -p "$dir"
  tmp=$dir/.$name.$$
  echo "movie: downloading $name $version (first run only)" >&2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$tmp" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$tmp" "$url"
  else
    false
  fi || {
    rm -f "$tmp"
    rmdir "$dir" 2>/dev/null || true
    fail "could not download $url
  Fetch it another way, put it at $bin, and make it executable.
  Its SHA-256 must be $want.
  Or, with Go installed and network access for its modules, run with
  MOVIE_FROM_SOURCE=1 to build this plugin's source instead."
  }
  got=$(sha256 "$tmp")
  if [ "$got" != "$want" ]; then
    rm -f "$tmp"
    rmdir "$dir" 2>/dev/null || true
    fail "$url has SHA-256 $got, but checksums.txt says $want; not running it"
  fi
  chmod +x "$tmp"
  # another first run may have installed it meanwhile; either copy is verified
  mv -f "$tmp" "$bin" 2>/dev/null || rm -f "$tmp"
  [ -f "$bin" ] || fail "could not install $bin"
  # older releases' binaries are no longer needed
  for old in "$cache"/*; do
    b=${old##*/}
    if [ "$b" != "$want" ] && [ ${#b} -eq 64 ] && ! printf '%s' "$b" | grep -q '[^0-9a-f]'; then
      rm -rf "$old"
    fi
  done
fi
exec "$bin" "$@"
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/movie -run 'TestLauncher' -count=1 -v`
Expected: all PASS (the Windows one SKIPs on macOS and Linux). If Linux is available (`docker run --rm -v "$PWD":/src -w /src golang:1.26 ...` is not needed; CI covers Linux), note that `sh` there is dash: the launcher uses no bash-only syntax.

Then check it under dash if it is installed (`command -v dash`): `dash -n skills/proving-it-works-with-a-movie/bin/movie` must print nothing.

- [ ] **Step 5: Commit**

```bash
git add skills/proving-it-works-with-a-movie/bin/movie cmd/movie/launcher_test.go cmd/movie/release_test.go
git commit -m "Launcher: download this machine's release binary on first run, verified and cached by hash"
```

---

### Task 3: MOVIE_FROM_SOURCE builds the checkout

**Files:**
- Modify: `skills/proving-it-works-with-a-movie/bin/movie`
- Test: `cmd/movie/launcher_test.go`

**Interfaces:**
- Consumes: `fakeRelease.run`, `needSh`, `hostReleaseName` (Task 2); `script/build-binaries OUTDIR OS/ARCH` (Task 1).

- [ ] **Step 1: Write the failing test** (append to `cmd/movie/launcher_test.go`)

```go
// MOVIE_FROM_SOURCE=1 runs this checkout's source, built once and rebuilt
// only when the source changes, for testing unreleased work.
func TestLauncherBuildsTheCheckoutFromSource(t *testing.T) {
	needSh(t)
	if testing.Short() {
		t.Skip("builds movie from source")
	}
	r := &fakeRelease{cache: t.TempDir()}
	launcher := filepath.Join(repoRoot(t), binDir, "movie")
	first := r.run(t, exec.Command("sh", launcher, "help"), []string{"MOVIE_FROM_SOURCE=1"})
	if first.code != 0 || !strings.Contains(first.stdout, "Exit codes:") || !strings.Contains(first.stderr, "building "+hostReleaseName()) {
		t.Fatalf("exit %d\n%s", first.code, first.stderr)
	}
	if _, err := os.Stat(filepath.Join(r.cache, "proving-it-works", "source", hostReleaseName())); err != nil {
		t.Fatalf("not built into the source cache: %v", err)
	}
	again := r.run(t, exec.Command("sh", launcher, "help"), []string{"MOVIE_FROM_SOURCE=1"})
	if again.code != 0 || strings.Contains(again.stderr, "building") {
		t.Errorf("rebuilt unchanged source (exit %d):\n%s", again.code, again.stderr)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/movie -run TestLauncherBuildsTheCheckoutFromSource -count=1`
Expected: FAIL with `movie: ... has no VERSION, so there is no release to download`.

- [ ] **Step 3: Add the source path to the launcher**

In the header comment, below the `MOVIE_RELEASE_URL` line, add:

```sh
#   MOVIE_FROM_SOURCE=1    build this checkout's source and run that, for
#                          working on movie itself (needs Go)
```

Insert this block after the cache directory is settled (after the `if ! mkdir -p "$cache"` block) and before the `[ -f "$here/VERSION" ]` line:

```sh
if [ "${MOVIE_FROM_SOURCE:-}" = 1 ]; then
  src=$(cd "$here/../../.." && pwd)
  [ -f "$src/go.mod" ] || fail "MOVIE_FROM_SOURCE=1 needs this plugin's source; $src has no go.mod"
  out=$cache/source
  mkdir -p "$out"
  # go relinks on every build, so build only when the source has changed
  stamp=$(cd "$src" && find go.mod go.sum cmd internal -type f | LC_ALL=C sort |
    while read -r f; do cat "$f"; done | sha256 -)
  if [ ! -f "$out/$name" ] || [ "$(cat "$out/stamp" 2>/dev/null)" != "$stamp" ]; then
    echo "movie: building $name from $src" >&2
    bash "$src/script/build-binaries" "$out" "$os/$arch" >&2 || fail "building $name from source failed"
    printf '%s\n' "$stamp" > "$out/stamp"
  fi
  exec "$out/$name" "$@"
fi
```

Also change the no-VERSION message to point at it:

```sh
[ -f "$here/VERSION" ] || fail "$here has no VERSION, so there is no release to download; MOVIE_FROM_SOURCE=1 builds this checkout instead"
```

- [ ] **Step 4: Run the launcher tests to verify they pass**

Run: `go test ./cmd/movie -run 'TestLauncher' -count=1 -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add skills/proving-it-works-with-a-movie/bin/movie cmd/movie/launcher_test.go
git commit -m "Launcher: MOVIE_FROM_SOURCE=1 runs the checkout, rebuilt when its source changes"
```

---

### Task 4: The PowerShell launcher

**Files:**
- Create: `skills/proving-it-works-with-a-movie/bin/movie.ps1`
- Modify: `cmd/movie/launcher_test.go` (`newFakeRelease` also copies `movie.ps1`; new tests)
- Modify: `skills/proving-it-works-with-a-movie/SKILL.md` (the PowerShell paragraph)
- Modify: `.gitattributes`

**Interfaces:**
- Consumes: everything `launcher_test.go` produces (Task 2).

These tests run only on Windows. No machine this plan runs on locally has PowerShell, so they are verified in Windows CI in Task 7.

- [ ] **Step 1: Write the failing tests**

In `newFakeRelease`, replace the single `copyFile` of the launcher with:

```go
	for _, launcher := range []string{"movie", "movie.ps1"} {
		copyFile(t, filepath.Join(repoRoot(t), binDir, launcher), filepath.Join(r.bin, launcher), 0o755)
	}
```

Append:

```go
func needPowerShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the PowerShell launcher is for Windows")
	}
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("needs powershell on PATH")
	}
}

// powershell runs movie.ps1 the way SKILL.md says to.
func (r *fakeRelease) powershell(t *testing.T, env []string, args ...string) result {
	t.Helper()
	argv := append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(r.bin, "movie.ps1")}, args...)
	return r.run(t, exec.Command("powershell", argv...), env)
}

// movie.ps1 downloads, verifies, caches, and runs as the sh launcher does,
// into the same cache, so Git Bash then finds it there.
func TestPowerShellLauncherDownloadsIntoTheSharedCache(t *testing.T) {
	needPowerShell(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	got := r.powershell(t, nil, "a b%c;d")
	if got.code != 2 || !strings.Contains(got.stderr, `unknown command "a b%c;d"`) {
		t.Fatalf("argument did not arrive intact (exit %d):\n%s", got.code, got.stderr)
	}
	if data, err := os.ReadFile(r.installed()); err != nil || sha256Hex(data) != r.want {
		t.Fatalf("not cached at %s: %v", r.installed(), err)
	}
	help := r.powershell(t, nil, "help")
	if help.code != 0 || !strings.Contains(help.stdout, "Exit codes:") {
		t.Errorf("help: exit %d\n%s%s", help.code, help.stdout, help.stderr)
	}
	if _, err := exec.LookPath("sh"); err == nil {
		if got := r.sh(t, nil, "help"); got.code != 0 {
			t.Errorf("Git Bash: exit %d\n%s", got.code, got.stderr)
		}
	}
	if n := r.downloads.Load(); n != 1 {
		t.Errorf("%d downloads, want 1: later runs must use the shared cache", n)
	}
}

func TestPowerShellLauncherRefusesTheWrongChecksum(t *testing.T) {
	needPowerShell(t)
	body := hostBinary(t)
	wrong := sha256Hex([]byte("not the release"))
	r := newFakeRelease(t, wrong, body, 0)
	got := r.powershell(t, nil, "help")
	if got.code != 2 || !strings.Contains(got.stderr, sha256Hex(body)) || !strings.Contains(got.stderr, wrong) {
		t.Fatalf("want exit 2 naming both hashes, got %d:\n%s", got.code, got.stderr)
	}
	if left := filesUnder(t, r.cache); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

func TestPowerShellLauncherSaysWhatToFetchWhenTheDownloadFails(t *testing.T) {
	needPowerShell(t)
	want := sha256Hex(hostBinary(t))
	r := newFakeRelease(t, want, nil, 0)
	got := r.powershell(t, nil, "help")
	url := r.url + "/v" + testVersion + "/" + hostReleaseName()
	if got.code != 2 || !strings.Contains(got.stderr, "could not download "+url) || !strings.Contains(got.stderr, "SHA-256 must be "+want) {
		t.Fatalf("exit %d:\n%s", got.code, got.stderr)
	}
}

func TestPowerShellLauncherFirstRunsAtOnceAllSucceed(t *testing.T) {
	needPowerShell(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 300*time.Millisecond)
	results := make([]result, 4)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = r.powershell(t, nil, "help")
		}()
	}
	wg.Wait()
	for i, got := range results {
		if got.code != 0 {
			t.Errorf("run %d: exit %d\n%s", i, got.code, got.stderr)
		}
	}
	if left := filesUnder(t, r.cache); len(left) != 1 || left[0] != r.installed() {
		t.Errorf("cache holds %v, want only %s", left, r.installed())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail to compile or fail**

Run: `go test ./cmd/movie -run 'TestLauncher|TestPowerShell' -count=1`
Expected on macOS/Linux: every `TestLauncher*` FAILS in `newFakeRelease` because `movie.ps1` does not exist yet (`copyFile` cannot read it); the PowerShell tests skip.

- [ ] **Step 3: Write `skills/proving-it-works-with-a-movie/bin/movie.ps1`**

```powershell
# Run movie, downloading this machine's release binary on first use.
#
# Windows PowerShell's default execution policy blocks scripts, so run it as
#   powershell -ExecutionPolicy Bypass -File "$SKILL_DIR\bin\movie.ps1" ARGS...
#
# The binary for the version in VERSION comes from that GitHub release, is
# checked against checksums.txt, and is cached at
#   %LOCALAPPDATA%\proving-it-works\SHA256\movie-windows-ARCH.exe
# keyed by its expected hash: the same place bin/movie uses from Git Bash.
# Arguments and the exit code pass through.
#
#   MOVIE_RELEASE_URL=URL  download URL/vVERSION/NAME instead of from GitHub
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Fail([string]$message) {
    [Console]::Error.WriteLine("movie: $message")
    exit 2
}

$arch = $env:PROCESSOR_ARCHITEW6432
if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
$name = if ($arch -eq 'ARM64') { 'movie-windows-arm64.exe' } else { 'movie-windows-amd64.exe' }

$version = (Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot 'VERSION')).Trim()
$want = $null
foreach ($line in Get-Content -LiteralPath (Join-Path $PSScriptRoot 'checksums.txt')) {
    $fields = $line.Trim() -split '\s+'
    if ($fields.Count -ge 2 -and $fields[1] -eq $name) { $want = $fields[0].ToLower() }
}
if (-not $want) { Fail "checksums.txt has no line for $name" }

$cache = Join-Path $env:LOCALAPPDATA 'proving-it-works'
try {
    New-Item -ItemType Directory -Force -Path $cache | Out-Null
} catch {
    $cache = Join-Path ([IO.Path]::GetTempPath()) 'proving-it-works'
    [Console]::Error.WriteLine("movie: cannot write to $env:LOCALAPPDATA; caching in $cache instead")
    New-Item -ItemType Directory -Force -Path $cache | Out-Null
}
$dir = Join-Path $cache $want
$bin = Join-Path $dir $name

if (-not (Test-Path -LiteralPath $bin)) {
    $base = if ($env:MOVIE_RELEASE_URL) { $env:MOVIE_RELEASE_URL } else { 'https://github.com/prime-radiant-inc/proving-it-works/releases/download' }
    $url = "$base/v$version/$name"
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $tmp = Join-Path $dir ".$name.$PID"
    [Console]::Error.WriteLine("movie: downloading $name $version (first run only)")
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $tmp
    } catch {
        Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $dir -Force -ErrorAction SilentlyContinue
        Fail ("could not download $url`n" +
            "  Fetch it another way and put it at $bin`n" +
            "  Its SHA-256 must be $want.")
    }
    $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $tmp).Hash.ToLower()
    if ($got -ne $want) {
        Remove-Item -LiteralPath $tmp -Force
        Remove-Item -LiteralPath $dir -Force -ErrorAction SilentlyContinue
        Fail "$url has SHA-256 $got, but checksums.txt says $want; not running it"
    }
    # another first run may have installed it meanwhile; either copy is verified
    try {
        Move-Item -LiteralPath $tmp -Destination $bin
    } catch {
        Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
    }
    if (-not (Test-Path -LiteralPath $bin)) { Fail "could not install $bin" }
    Get-ChildItem -LiteralPath $cache -Directory |
        Where-Object { $_.Name -match '^[0-9a-f]{64}$' -and $_.Name -ne $want } |
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
}

# A native program's stderr must not become a terminating error.
$ErrorActionPreference = 'Continue'
& $bin @args
exit $LASTEXITCODE
```

- [ ] **Step 4: Line endings and the skill's PowerShell line**

In `.gitattributes`, below the `bin/movie text eol=lf` line, add:

```
skills/proving-it-works-with-a-movie/bin/movie.ps1 text eol=lf
skills/proving-it-works-with-a-movie/bin/VERSION text eol=lf
skills/proving-it-works-with-a-movie/bin/checksums.txt text eol=lf
```

In `skills/proving-it-works-with-a-movie/SKILL.md`, replace:

```markdown
On Windows PowerShell, run `& "$SKILL_DIR/bin/movie-windows-amd64.exe"` with
the same arguments. From Git Bash, `bin/movie` works; pass paths in Windows
form (`cygpath -m`).
```

with:

```markdown
On Windows PowerShell, run
`powershell -ExecutionPolicy Bypass -File "$SKILL_DIR\bin\movie.ps1"` with
the same arguments. From Git Bash, `bin/movie` works; pass paths in Windows
form (`cygpath -m`). The first run downloads movie for this machine (about
10 MB) and checks it; if it cannot, it says what to fetch and where to put it.
```

- [ ] **Step 5: Run the launcher tests** (macOS/Linux: PowerShell tests skip; the sh tests must pass again now that `movie.ps1` exists)

Run: `go test ./cmd/movie -run 'TestLauncher|TestPowerShell' -count=1 -v`
Expected: `TestLauncher*` PASS, `TestPowerShell*` SKIP.

- [ ] **Step 6: Commit**

```bash
git add skills/proving-it-works-with-a-movie/bin/movie.ps1 cmd/movie/launcher_test.go .gitattributes skills/proving-it-works-with-a-movie/SKILL.md
git commit -m "Add the PowerShell launcher, sharing Git Bash's cache"
```

---

### Task 5: script/release and the release check

**Files:**
- Create: `script/release`
- Modify: `cmd/movie/release_test.go` (release script tests; the release check compares hashes; `releaseNames` gains Windows ARM64; `TestCommittedBinariesMatchSource` is replaced)

**Interfaces:**
- Consumes: `script/build-binaries OUTDIR` (Task 1); `sha256Hex`, `writeText` (Task 2).
- Produces: `script/release VERSION`; `RELEASE_DRY_RUN=1` stops after the local release commit.

- [ ] **Step 1: Write the failing tests** (`cmd/movie/release_test.go`)

Replace `releaseNames` with the six names:

```go
var releaseNames = []string{
	"movie-darwin-arm64", "movie-darwin-amd64", "movie-linux-amd64",
	"movie-linux-arm64", "movie-windows-amd64.exe", "movie-windows-arm64.exe",
}
```

Replace `TestCommittedBinariesMatchSource` with the following, and remove the `"bytes"` import it was the last user of:

```go
// readChecksums parses a checksums.txt into asset name -> SHA-256.
func readChecksums(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("%s: malformed line %q", path, line)
		}
		sums[fields[1]] = fields[0]
	}
	return sums
}

// The release main names must be what its source builds: checksums.txt
// holds the SHA-256 of each binary script/build-binaries makes from this
// source, and VERSION is everyharness.yaml's version. CI runs it on main.
func TestReleaseChecksumsMatchSource(t *testing.T) {
	if os.Getenv("MOVIE_RELEASE_CHECK") != "1" {
		t.Skip("set MOVIE_RELEASE_CHECK=1 to compare checksums.txt with a fresh build")
	}
	root := repoRoot(t)
	out := t.TempDir()
	if got := buildBinaries(t, out); got.code != 0 {
		t.Fatalf("build-binaries: exit %d\n%s", got.code, got.stderr)
	}
	sums := readChecksums(t, filepath.Join(root, binDir, "checksums.txt"))
	if len(sums) != len(releaseNames) {
		t.Errorf("checksums.txt lists %d assets, want %d", len(sums), len(releaseNames))
	}
	for _, name := range releaseNames {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := sha256Hex(data); got != sums[name] {
			t.Errorf("%s builds to %s, but checksums.txt says %q: release this source with script/release", name, got, sums[name])
		}
	}
	version, err := os.ReadFile(filepath.Join(root, binDir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(root, "everyharness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "\nversion: "+strings.TrimSpace(string(version))+"\n") {
		t.Errorf("VERSION says %q, but everyharness.yaml's version differs", strings.TrimSpace(string(version)))
	}
}
```

Append the release script tests:

```go
// git runs git in dir as a test identity, failing the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), gitIdentity...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

var gitIdentity = []string{
	"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
	"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
}

// releaseClone is a clone of this repo on main, with the working tree's
// release scripts committed and a bare origin it has pushed to, so
// script/release can run there without touching this repo or GitHub.
func releaseClone(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	root := repoRoot(t)
	base := t.TempDir()
	dir, origin := filepath.Join(base, "work"), filepath.Join(base, "origin.git")
	git(t, base, "clone", "-q", root, dir)
	git(t, dir, "checkout", "-q", "-B", "main")
	for _, s := range []string{"script/release", "script/build-binaries"} {
		copyFile(t, filepath.Join(root, s), filepath.Join(dir, s), 0o755)
	}
	git(t, dir, "add", "script")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "the scripts under test")
	git(t, base, "init", "-q", "--bare", origin)
	git(t, dir, "remote", "set-url", "origin", origin)
	git(t, dir, "push", "-q", "origin", "main")
	return dir
}

// runRelease runs script/release in dir with env added.
func runRelease(t *testing.T, dir string, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(dir, "script", "release")}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), gitIdentity...), env...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// script/release refuses to start anywhere a release could go wrong: off
// main, with uncommitted changes, behind origin, or on a version already
// tagged. Each refusal happens before anything is built or committed.
func TestReleaseRefusesToStartFromTheWrongPlace(t *testing.T) {
	cases := []struct {
		name    string
		version string
		setup   func(t *testing.T, dir string)
		want    string
	}{
		{"not a version", "9.9", func(*testing.T, string) {}, "usage: script/release VERSION"},
		{"off main", "9.9.9", func(t *testing.T, dir string) { git(t, dir, "checkout", "-q", "-b", "topic") }, "run it on main"},
		{"uncommitted changes", "9.9.9", func(t *testing.T, dir string) { writeText(t, filepath.Join(dir, "stray.txt"), "x") }, "uncommitted changes"},
		{"behind origin", "9.9.9", func(t *testing.T, dir string) {
			git(t, dir, "commit", "-q", "--allow-empty", "-m", "someone else's")
			git(t, dir, "push", "-q", "origin", "main")
			git(t, dir, "reset", "-q", "--hard", "HEAD~1")
		}, "pull first"},
		{"tagged here", "9.9.9", func(t *testing.T, dir string) { git(t, dir, "tag", "v9.9.9") }, "v9.9.9 already exists here"},
		{"tagged on origin", "9.9.9", func(t *testing.T, dir string) {
			git(t, dir, "tag", "v9.9.9")
			git(t, dir, "push", "-q", "origin", "v9.9.9")
			git(t, dir, "tag", "-d", "v9.9.9")
		}, "v9.9.9 already exists on origin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := releaseClone(t)
			c.setup(t, dir)
			before := git(t, dir, "rev-parse", "HEAD")
			got := runRelease(t, dir, nil, c.version)
			if got.code != 2 || !strings.Contains(got.stderr, c.want) {
				t.Errorf("exit %d, want 2 saying %q:\n%s", got.code, c.want, got.stderr)
			}
			if after := git(t, dir, "rev-parse", "HEAD"); after != before {
				t.Errorf("committed despite refusing")
			}
		})
	}
}

// A dry run makes the release commit: the version everywhere, VERSION, and
// the six binaries' checksums; and tags, pushes, and publishes nothing.
func TestReleaseDryRunCommitsTheVersionAndChecksums(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the six release binaries")
	}
	if _, err := exec.LookPath("everyharness"); err != nil && os.Getenv("EVERYHARNESS") == "" {
		t.Skip("needs everyharness on PATH or EVERYHARNESS=/path/to/everyharness/dist/cli.js")
	}
	dir := releaseClone(t)
	pushed := git(t, dir, "rev-parse", "origin/main")
	got := runRelease(t, dir, []string{"RELEASE_DRY_RUN=1"}, "9.9.9")
	if got.code != 0 {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, binDir, "VERSION")); string(data) != "9.9.9\n" {
		t.Errorf("VERSION holds %q", data)
	}
	sums := readChecksums(t, filepath.Join(dir, binDir, "checksums.txt"))
	for _, name := range releaseNames {
		if len(sums[name]) != 64 {
			t.Errorf("checksums.txt has no hash for %s: %v", name, sums)
		}
	}
	for file, want := range map[string]string{"everyharness.yaml": "\nversion: 9.9.9\n", ".claude-plugin/plugin.json": `"version": "9.9.9"`} {
		if data, _ := os.ReadFile(filepath.Join(dir, file)); !strings.Contains(string(data), want) {
			t.Errorf("%s lacks %q", file, want)
		}
	}
	if subject := git(t, dir, "log", "-1", "--format=%s"); subject != "Release 9.9.9" {
		t.Errorf("last commit is %q", subject)
	}
	if status := git(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("left uncommitted:\n%s", status)
	}
	if tags := git(t, dir, "tag", "-l", "v9.9.9"); tags != "" {
		t.Error("a dry run tagged")
	}
	if now := git(t, dir, "ls-remote", "origin", "refs/heads/main"); !strings.HasPrefix(now, pushed) {
		t.Error("a dry run pushed")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `EVERYHARNESS=$HOME/git/everyharness/dist/cli.js go test ./cmd/movie -run 'TestRelease' -count=1`
Expected: FAIL. `releaseClone` cannot copy `script/release`, which does not exist yet.

- [ ] **Step 3: Write `script/release`** (and `chmod +x script/release`)

Before writing it, confirm the gh flags it uses exist in the installed gh: `gh release create --help | grep -E -- '--draft|--verify-tag|--title|--notes'`, `gh release edit --help | grep -- '--draft'`, `gh release delete --help | grep -E -- '--yes|--cleanup-tag'`. If one is missing, stop and report it rather than guessing a replacement.

```bash
#!/usr/bin/env bash
# Release movie: build the six binaries, record their checksums, set the
# version everywhere, and publish the binaries as the GitHub release
# vVERSION, which the launchers download from.
#
# When: whenever main has changes users should get, skill text included.
# Claude Code only updates an installed plugin whose version moved.
#
# Usage: script/release VERSION        (such as 0.2.0)
#
# Needs: gh, logged in with push access; Go; and everyharness, on PATH or as
# EVERYHARNESS=/path/to/everyharness/dist/cli.js (clone
# github.com/prime-radiant-inc/everyharness, then npm ci && npm run build).
#
# RELEASE_DRY_RUN=1 stops after the local release commit: nothing is
# tagged, pushed, or published.
set -euo pipefail
fail() {
  echo "release: $*" >&2
  exit 2
}
version=${1:-}
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "usage: script/release VERSION  (such as 0.2.0)" >&2
  exit 2
}
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
bin=skills/proving-it-works-with-a-movie/bin
tag=v$version

[[ $(git rev-parse --abbrev-ref HEAD) == main ]] || fail "run it on main"
[[ -z $(git status --porcelain) ]] || fail "the tree has uncommitted changes"
git fetch -q origin main
git merge-base --is-ancestor origin/main HEAD || fail "origin/main has commits this main lacks; pull first"
if git rev-parse -q --verify "refs/tags/$tag" > /dev/null; then
  fail "$tag already exists here"
fi
[[ -z $(git ls-remote --tags origin "refs/tags/$tag") ]] || fail "$tag already exists on origin"

if command -v everyharness > /dev/null 2>&1; then
  everyharness=(everyharness)
elif [[ -n ${EVERYHARNESS:-} ]]; then
  everyharness=(node "$EVERYHARNESS")
else
  fail "needs everyharness on PATH, or EVERYHARNESS=/path/to/everyharness/dist/cli.js"
fi

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
bash script/build-binaries "$out"
(
  cd "$out"
  if command -v sha256sum > /dev/null 2>&1; then
    sha256sum movie-*
  else
    shasum -a 256 movie-*
  fi
) > "$bin/checksums.txt"
"${everyharness[@]}" bump "$version"
printf '%s\n' "$version" > "$bin/VERSION"
git add -u
git add "$bin/VERSION" "$bin/checksums.txt"
git commit -q -m "Release $version"

if [[ ${RELEASE_DRY_RUN:-} == 1 ]]; then
  echo "dry run: committed release $version locally; nothing tagged, pushed, or published"
  exit 0
fi

# Publish so that no one can download from a release main does not name:
# the release stays a draft until main is pushed, and is deleted if that
# push fails.
git tag "$tag"
git push -q origin "$tag"
gh release create "$tag" --draft --verify-tag --title "$tag" --notes "movie $version" "$out"/movie-*
if ! git push -q origin main; then
  gh release delete "$tag" --yes --cleanup-tag
  git tag -d "$tag"
  fail "pushing main failed, so the draft release and its tag are deleted; the release commit is still here: pull, then release again"
fi
gh release edit "$tag" --draft=false

# Check what users will download against what was committed.
check=$(mktemp -d)
while read -r sum name; do
  curl -fsSL -o "$check/$name" "https://github.com/prime-radiant-inc/proving-it-works/releases/download/$tag/$name"
  if command -v sha256sum > /dev/null 2>&1; then
    got=$(sha256sum "$check/$name" | cut -d' ' -f1)
  else
    got=$(shasum -a 256 "$check/$name" | cut -d' ' -f1)
  fi
  [[ $got == "$sum" ]] || fail "the published $name has SHA-256 $got, not $sum"
done < "$bin/checksums.txt"
rm -rf "$check"
echo "released $tag: https://github.com/prime-radiant-inc/proving-it-works/releases/tag/$tag"
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `EVERYHARNESS=$HOME/git/everyharness/dist/cli.js go test ./cmd/movie -run 'TestRelease' -count=1 -v`
Expected: the refusal subtests and the dry run PASS; `TestReleaseChecksumsMatchSource` SKIPs (no `MOVIE_RELEASE_CHECK`).

If the dry run fails inside `everyharness bump` (for example its audit finding `0.1.0` in a file it does not manage), read its message: add only the file it names to `release.audit.exclude` in `everyharness.yaml` if that file legitimately carries an old version (a spec, a changelog), and note the change in the commit message. Do not exclude generated files.

- [ ] **Step 5: Commit**

```bash
git add script/release cmd/movie/release_test.go
git commit -m "Add script/release; the release check compares checksums.txt with a fresh build"
```

(Include `everyharness.yaml` only if Step 4 required an audit exclusion.)

---

### Task 6: Stop committing binaries

**Files:**
- Delete: `skills/proving-it-works-with-a-movie/bin/movie-darwin-amd64`, `movie-darwin-arm64`, `movie-linux-amd64`, `movie-linux-arm64`, `movie-windows-amd64.exe`
- Modify: `.gitattributes`, `.gitignore`, `examples/tour/film.sh`, `README.md`

- [ ] **Step 1: Remove the binaries and keep them out**

```bash
git rm -q skills/proving-it-works-with-a-movie/bin/movie-darwin-amd64 \
  skills/proving-it-works-with-a-movie/bin/movie-darwin-arm64 \
  skills/proving-it-works-with-a-movie/bin/movie-linux-amd64 \
  skills/proving-it-works-with-a-movie/bin/movie-linux-arm64 \
  skills/proving-it-works-with-a-movie/bin/movie-windows-amd64.exe
```

In `.gitattributes`, delete the line `skills/proving-it-works-with-a-movie/bin/movie-* binary`.

In `.gitignore`, append:

```
# movie's binaries ship as GitHub release assets, never in the repo
skills/proving-it-works-with-a-movie/bin/movie-*
```

- [ ] **Step 2: The tour builds its Linux binary**

In `examples/tour/film.sh`, in the `linux)` branch, replace the mount line

```bash
      -v "$bin/movie-linux-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/'):/usr/local/bin/movie:ro" \
```

with a build before `docker run` and a mount of its result. The branch becomes:

```bash
  linux)
    arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
    linuxbin=$(mktemp -d)
    bash "$here/../../script/build-binaries" "$linuxbin" "linux/$arch" > /dev/null
    docker rm -f tour > /dev/null 2>&1 || true
    docker run -d --name tour --shm-size 1g \
      -v "$work:/work" \
      -v "$linuxbin/movie-linux-$arch:/usr/local/bin/movie:ro" \
      -v "$here/app/todo:/usr/local/bin/todo:ro" \
      -v "$here/app/index.html:/srv/index.html:ro" \
      movie-tour > /dev/null
```

(leave the rest of the branch as it is). Check it parses: `bash -n examples/tour/film.sh`.

- [ ] **Step 3: README**

In `README.md`, replace

```markdown
One binary, `bin/movie`, prebuilt for macOS, Linux, and Windows. It needs
```

with

```markdown
One binary, run through `bin/movie`, which on first use downloads the build
for your machine (macOS, Linux, or Windows, on x86-64 or ARM64) from this
repo's GitHub release and checks it against `bin/checksums.txt`. It needs
```

- [ ] **Step 4: Confirm nothing else names the committed binaries**

Run: `git grep -n "movie-darwin\|movie-linux\|movie-windows" -- ':!docs/superpowers' ':!cmd/movie' ':!skills/proving-it-works-with-a-movie/bin/movie' ':!skills/proving-it-works-with-a-movie/bin/movie.ps1' ':!script'`
Expected: no output. If something turns up, update it the same way and include it in the commit.

- [ ] **Step 5: Run the whole suite**

Run: `go vet ./... && go test ./... 2>&1 | grep -v -e '^ok' -e 'no test files'`
Expected: no output (everything passes).

- [ ] **Step 6: Commit**

```bash
git add .gitattributes .gitignore examples/tour/film.sh README.md
git commit -m "Stop committing movie's binaries: the launchers download them from the release"
```

---

### Task 7: Verify on Windows and release (controller only; needs Jesse)

Implementers do not do this task. It pushes and publishes, which need Jesse's explicit go-ahead each time.

- [ ] **Step 1:** Ask Jesse to push `wip/download-on-first-run` so CI runs the launcher tests on Windows (PowerShell and Git Bash) and Linux (dash). On approval, push and watch the run with `gh run watch`. Fix anything it finds with the same TDD loop, then push again (same approval covers fixes to this branch unless Jesse says otherwise).
- [ ] **Step 2:** Merge to `main` locally (superpowers:finishing-a-development-branch).
- [ ] **Step 3:** Ask Jesse to approve releasing `0.2.0`. On approval, run `EVERYHARNESS=$HOME/git/everyharness/dist/cli.js script/release 0.2.0`. Its last lines must read `released v0.2.0: ...`.
- [ ] **Step 4:** Check a real first run from a fresh cache: `XDG_CACHE_HOME=$(mktemp -d) skills/proving-it-works-with-a-movie/bin/movie help` must print one `downloading` line on stderr and the usage on stdout; running it again with the same `XDG_CACHE_HOME` must not download.
- [ ] **Step 5:** Confirm CI on `main` passes `TestReleaseChecksumsMatchSource` (`MOVIE_RELEASE_CHECK=1`).
