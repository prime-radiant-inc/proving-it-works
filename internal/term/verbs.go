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
// recorder in the background, and returns. Any failure once the tmux server
// exists kills it, so Start never leaves an orphaned server behind.
func Start(dir string, o StartOptions, stdout io.Writer) (err error) {
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
	// A wrapper may pass the host's environment through (env does, and so
	// may a wrapper script), so scrub wrapped sessions too; env -u of a
	// name the container never had is harmless.
	scrub := claudeEnvNames(os.Environ())
	args = append(args, shellCommand(s.History, scrub), ";",
		"set-option", "-t", window, "status", "off", ";",
		"set-option", "-t", window, "@movie_film", "on", ";",
		"set-option", "-t", window, "@movie_sent", "0")
	if _, err := s.tmux(args...); err != nil {
		return err
	}
	// The tmux server now exists: kill it on any failure from here on, so
	// Start never leaks a server on error.
	defer func() {
		if err != nil {
			s.tmux("kill-server")
		}
	}()
	if _, err := s.waitFor(10*time.Second, func(st Status) bool { return st.Seq >= 1 }); err != nil {
		return fmt.Errorf("the shell never showed a prompt: %w", err)
	}
	if err := s.send("clear", "Enter"); err != nil {
		return err
	}
	if _, err := s.waitFor(10*time.Second, func(st Status) bool { return st.Seq >= 2 }); err != nil {
		return fmt.Errorf("the shell did not clear: %w", err)
	}
	if err := s.save(); err != nil {
		return err
	}
	if err := spawnRecorder(s); err != nil {
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

// filmOffAck is the tmux option the recorder sets to 1 once it has filmed
// and held the screen at a film off.
const filmOffAck = "@movie_film_ack"

// SetFilm turns filming on or off; each "on" after "off" starts a new take.
//
// Turning filming off hands off to the recorder the way Stop does: the
// recorder polls on its own clock, so a command's result that run has just
// seen may not be recorded yet. SetFilm clears the acknowledgement and turns
// filming off in one tmux call, then waits for the recorder to film the
// current screen, hold it, and acknowledge, so the caller's next action
// cannot race that last filmed screen.
func SetFilm(s *Session, on bool) error {
	if on {
		_, err := s.tmux("set-option", "-t", window, "@movie_film", "on")
		return err
	}
	st, err := s.status(false)
	if err != nil {
		return err
	}
	if !st.Film {
		return nil
	}
	if _, err := s.tmux("set-option", "-t", window, filmOffAck, "0", ";",
		"set-option", "-t", window, "@movie_film", "off"); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := s.tmux("display-message", "-p", "-t", window, "#{"+filmOffAck+"}")
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) == "1" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the recorder did not confirm filming stopped; see %s", filepath.Join(s.Dir, "recorder.log"))
		}
		time.Sleep(50 * time.Millisecond)
	}
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
//
// The recorder is the only writer of recording.jsonl, so Stop does not kill
// the tmux server first: that would race the recorder's last snapshot against
// the server going away. Instead it asks the recorder to flush and hold the
// final screen (@movie_stop), waits for it to confirm and exit
// (recorder.done), and only then tears the server down.
func Stop(s *Session, outdir string, px image.Point, stdout io.Writer) error {
	if err := requireEmptyDir(outdir); err != nil {
		return err
	}
	s.tmux("set-option", "-t", window, "@movie_stop", "1") // best effort: a dead server means the recorder already stopped on its own
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
	s.tmux("kill-server") // ignore errors: already gone is fine
	if len(s.Wrapper) == 0 {
		if err := os.Remove(s.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return Render(s.Dir, outdir, px, stdout)
}
