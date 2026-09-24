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
	CursorVisible, Film, Stop    bool
	FilmOffPending               bool   // SetFilm turned filming off and the recorder has not yet confirmed it
	Sent                         int    // prompt sequence number when input was last sent
	Seq, Code                    int    // the latest prompt's sequence number and reported status
	Command                      string // the pane's foreground command
	Screen                       string
}

// AtPrompt reports whether bash has shown a new prompt since input was last
// sent, so typing now cannot land in a running program's stdin.
func (st Status) AtPrompt() bool { return st.Seq > st.Sent && st.Command == "bash" }

const statusFormat = "#{cursor_x};#{cursor_y};#{pane_width};#{pane_height};#{cursor_flag};" +
	"#{@movie_film};#{@movie_stop};#{@movie_film_ack};#{@movie_sent};#{pane_current_command};#{pane_title}"

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
	f := strings.SplitN(line, ";", 11)
	if len(f) != 11 {
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
		CursorVisible: f[4] == "1", Film: f[5] != "off", Stop: f[6] == "1", FilmOffPending: f[7] == "0",
		Command: f[9], Screen: screen}
	st.Sent, _ = strconv.Atoi(f[8])
	st.Seq, st.Code, _ = parseTitle(f[10])
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
