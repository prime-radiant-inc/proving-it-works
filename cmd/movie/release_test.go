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
