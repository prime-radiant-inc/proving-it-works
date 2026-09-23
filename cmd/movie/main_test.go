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
