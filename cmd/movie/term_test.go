package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/term"
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
	if len(frames) < 15 {
		t.Fatalf("take-1 has %d frames, want at least 15 (the final screen held 1.5s)", len(frames))
	}
	var take struct{ Rate float64 }
	data, err := os.ReadFile(filepath.Join(takes, "take-1", "take.json"))
	if err != nil || json.Unmarshal(data, &take) != nil || take.Rate != 10 {
		t.Fatalf("take.json: %s %v", data, err)
	}

	// The last non-end entry of the recording is the flush snapshot Stop
	// asked the recorder for: it must show the command's actual output, not
	// just the typed command.
	recording, err := os.ReadFile(filepath.Join(session, "recording.jsonl"))
	if err != nil {
		t.Fatalf("recording.jsonl: %v", err)
	}
	var last term.Entry
	found := false
	for _, line := range strings.Split(strings.TrimRight(string(recording), "\n"), "\n") {
		var e term.Entry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if !e.End {
			last, found = e, true
		}
	}
	if !found {
		t.Fatal("recording.jsonl has no non-end entries")
	}
	if !slices.Contains(strings.Split(last.Screen, "\n"), "proving-it-works") {
		t.Fatalf("last non-end entry's screen has no line exactly \"proving-it-works\":\n%q", last.Screen)
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
