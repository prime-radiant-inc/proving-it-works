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
