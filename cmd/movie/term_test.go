package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
		// The recorder runs detached from tmux and only notices the server is
		// gone on its next poll, then writes recorder.done and exits. Wait for
		// that so t.TempDir()'s cleanup never races its last write.
		done := filepath.Join(session, "recorder.done")
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, err := os.Stat(done); err == nil || time.Now().After(deadline) {
				break
			}
			time.Sleep(20 * time.Millisecond)
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
