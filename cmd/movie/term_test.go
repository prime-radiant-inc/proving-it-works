package main

import (
	"encoding/json"
	"fmt"
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
			// Stop already removes the socket for unwrapped sessions, but a
			// test that never calls stop would otherwise leave it behind.
			os.Remove(s.Socket)
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
	// take.json is a paste-ready frames scene body: frames and rate, nothing else.
	var take map[string]any
	data, err := os.ReadFile(filepath.Join(takes, "take-1", "take.json"))
	if err != nil || json.Unmarshal(data, &take) != nil {
		t.Fatalf("take.json: %s %v", data, err)
	}
	wantFrames, _ := filepath.Abs(filepath.Join(takes, "take-1"))
	if len(take) != 2 || take["rate"] != float64(10) || take["frames"] != wantFrames {
		t.Fatalf("take.json = %s, want exactly frames %q and rate 10", data, wantFrames)
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

// readEntries reads a session's recording.jsonl.
func readEntries(t *testing.T, session string) []term.Entry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(session, "recording.jsonl"))
	if err != nil {
		t.Fatalf("recording.jsonl: %v", err)
	}
	var entries []term.Entry
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		var e term.Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("recording.jsonl line %q: %v", line, err)
		}
		entries = append(entries, e)
	}
	return entries
}

// hasLine reports whether screen has a line that is exactly want.
func hasLine(screen, want string) bool { return slices.Contains(strings.Split(screen, "\n"), want) }

// TestFilmOffRightAfterRunKeepsTheResultAndHoldsIt: run returns when its own
// poll sees the prompt, and the recorder polls on its own clock, so film off
// must hand off to the recorder the way stop does: the result is filmed and
// held for 1.5 s before the take ends. Run it with -count=10.
func TestFilmOffRightAfterRunKeepsTheResultAndHoldsIt(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	if r := runMovie(t, dir, "term", "run", session, "echo RESULT-LINE"); r.code != 0 {
		t.Fatalf("run: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	if r := runMovie(t, dir, "term", "film", session, "off"); r.code != 0 {
		t.Fatalf("film off: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	takes := filepath.Join(dir, "takes")
	if r := runMovie(t, dir, "term", "stop", session, takes); r.code != 0 {
		t.Fatalf("stop: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	all := term.Takes(readEntries(t, session))
	if len(all) != 1 {
		t.Fatalf("got %d takes, want 1", len(all))
	}
	take := all[0]
	result := -1
	for i, e := range take.Entries {
		if hasLine(e.Screen, "RESULT-LINE") {
			result = i
			break
		}
	}
	if result < 0 {
		t.Fatal("no filmed entry shows the line RESULT-LINE")
	}
	if held := take.End - take.Entries[result].T; held < 1.5 {
		t.Fatalf("the result is on screen for %.2fs of the take, want at least 1.5s", held)
	}
	if last := take.Entries[len(take.Entries)-1]; !hasLine(last.Screen, "RESULT-LINE") {
		t.Fatalf("the last filmed entry has no line exactly RESULT-LINE:\n%q", last.Screen)
	}
	frames, _ := filepath.Glob(filepath.Join(takes, "take-1", "f*.png"))
	if want := len(term.Slots(take, term.FPS)); len(frames) != want {
		t.Fatalf("take-1 has %d frames, want %d", len(frames), want)
	}
}

// A film on followed by film off before the recorder's next poll must still
// be acknowledged: the recorder never saw filming on, but film off waits for
// it. Separate movie processes are too slow to land inside one poll, so this
// calls SetFilm directly.
func TestFilmOffRightAfterFilmOnIsStillConfirmed(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	s, err := term.Load(session)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := term.SetFilm(s, false); err != nil {
			t.Fatalf("film off %d: %v", i, err)
		}
		if err := term.SetFilm(s, true); err != nil {
			t.Fatalf("film on %d: %v", i, err)
		}
	}
}

// TestARecordingThatEndsWithoutStopSaysWhy: when the tmux server goes away
// under the recorder, it logs each failed poll, gives up after five, records
// why in the end entry, and render warns about it.
func TestARecordingThatEndsWithoutStopSaysWhy(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	runMovie(t, dir, "term", "run", session, "echo hi")
	var s struct{ Socket string }
	data, _ := os.ReadFile(filepath.Join(session, "session.json"))
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	exec.Command("tmux", "-S", s.Socket, "kill-server").Run()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(session, "recorder.done")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the recorder never finished")
		}
		time.Sleep(50 * time.Millisecond)
	}
	entries := readEntries(t, session)
	end := entries[len(entries)-1]
	if !end.End || !strings.Contains(end.Reason, "tmux") {
		t.Fatalf("last entry = %+v, want an end entry whose reason names the tmux failure", end)
	}
	log, _ := os.ReadFile(filepath.Join(session, "recorder.log"))
	if n := strings.Count(string(log), "\n"); n < 5 {
		t.Fatalf("recorder.log has %d lines, want one per failed poll (5):\n%s", n, log)
	}
	r := runMovie(t, dir, "term", "render", session, filepath.Join(dir, "frames"))
	if r.code != 0 || !strings.Contains(r.stdout, "WARN") || !strings.Contains(r.stdout, end.Reason) {
		t.Fatalf("render: code %d, want a WARN naming %q\n%s%s", r.code, end.Reason, r.stdout, r.stderr)
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

// TestFilmedShellNeverInheritsAgentVariables applies the controller ruling
// that no variable whose name starts with CLAUDE reaches the filmed shell:
// a nested Claude Code warns about inherited session markers, and a filmed
// `env` would otherwise print a live token on camera. The command greps for
// the one variable, rather than dumping the whole environment, because the
// test session's pane is only 10 rows and a plain `env` scrolls it off
// screen regardless of whether it leaked.
func TestFilmedShellNeverInheritsAgentVariables(t *testing.T) {
	t.Setenv("CLAUDE_CODE_TEST_TOKEN", "secret-token-xyz")
	dir := t.TempDir()
	session := startSession(t, dir)
	r := runMovie(t, dir, "term", "run", session, "env | grep CLAUDE_CODE_TEST_TOKEN")
	if strings.Contains(r.stdout, "secret-token-xyz") {
		t.Fatalf("the filmed shell's env leaked CLAUDE_CODE_TEST_TOKEN:\n%s", r.stdout)
	}
}

// A pass-through wrapper stands in for docker exec: every tmux call goes
// through it, and the prompt still installs. The env wrapper passes the
// host's variables through, as a wrapper may, so the CLAUDE* scrub must
// apply to wrapped sessions too.
func TestWrappedSessionRunsThroughTheWrapper(t *testing.T) {
	t.Setenv("CLAUDE_CODE_TEST_TOKEN", "secret-token-xyz")
	dir := t.TempDir()
	session := startSession(t, dir, "--", "env", "MOVIE_WRAPPED=yes")
	r := runMovie(t, dir, "term", "run", session, "echo wrapped:$MOVIE_WRAPPED")
	if r.code != 0 || !strings.Contains(r.stdout, "wrapped:yes") {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	r = runMovie(t, dir, "term", "run", session, "env | grep CLAUDE_CODE_TEST_TOKEN")
	if strings.Contains(r.stdout, "secret-token-xyz") {
		t.Fatalf("the wrapped shell's env leaked CLAUDE_CODE_TEST_TOKEN:\n%s", r.stdout)
	}
}

// cut ends one take and starts the next in one command, so each narrated
// beat gets its own scene; each take ends holding its result on screen.
func TestCutStartsANewTakeHoldingTheResult(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	runMovie(t, dir, "term", "run", session, "echo FIRST-BEAT")
	if r := runMovie(t, dir, "term", "cut", session); r.code != 0 {
		t.Fatalf("cut: code %d %s", r.code, r.stderr)
	}
	runMovie(t, dir, "term", "run", session, "echo SECOND-BEAT")
	takes := filepath.Join(dir, "takes")
	r := runMovie(t, dir, "term", "stop", session, takes)
	if r.code != 0 {
		t.Fatalf("stop: %s", r.stderr)
	}
	for i := 1; i <= 2; i++ {
		frames, _ := filepath.Glob(filepath.Join(takes, fmt.Sprintf("take-%d", i), "f*.png"))
		if len(frames) < 15 {
			t.Errorf("take-%d has %d frames; it should hold its result for 1.5 s", i, len(frames))
		}
	}
	// each take's final filmed screen shows its own result
	var last [2]string
	take := 0
	for _, e := range readEntries(t, session) {
		if e.End {
			continue
		}
		if e.Film && take < 2 {
			last[take] = e.Screen
		} else if !e.Film && last[take] != "" {
			take++
		}
	}
	if !strings.Contains(last[0], "\nFIRST-BEAT") || !strings.Contains(last[1], "\nSECOND-BEAT") {
		t.Fatalf("takes do not end on their results:\n%q\n%q", last[0], last[1])
	}
}

// stop prints each take as a scene entry to paste into a scene file.
func TestStopPrintsReadyToPasteScenes(t *testing.T) {
	dir := t.TempDir()
	session := startSession(t, dir)
	runMovie(t, dir, "term", "run", session, "echo one")
	runMovie(t, dir, "term", "cut", session)
	runMovie(t, dir, "term", "run", session, "echo two")
	takes := filepath.Join(dir, "takes")
	r := runMovie(t, dir, "term", "stop", session, takes)
	for i := 1; i <= 2; i++ {
		want := fmt.Sprintf("  - id: take-%d\n    frames: %s\n    narration_at: end\n", i, filepath.Join(takes, fmt.Sprintf("take-%d", i)))
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing scene entry %q in:\n%s", want, r.stdout)
		}
	}
}
