package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
