package term

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Entry is one recorded snapshot of the filmed pane.
type Entry struct {
	T       float64 `json:"t"`
	End     bool    `json:"end,omitempty"`
	Reason  string  `json:"reason,omitempty"` // on an end entry: why recording stopped without a stop request
	Film    bool    `json:"film"`
	Cols    int     `json:"cols"`
	Rows    int     `json:"rows"`
	CursorX int     `json:"cx"`
	CursorY int     `json:"cy"`
	Cursor  bool    `json:"cursor"`
	Screen  string  `json:"screen"`
}

func now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// hold is how long the final screen of a take stays up after filming stops,
// by film off or by stop.
const hold = 1.5

// maxFailures is how many polls in a row may fail before the recorder gives
// up; one slow or failed tmux or docker exec call must not end a take.
const maxFailures = 5

// Record snapshots the pane up to ten times a second, appending each change
// to recording.jsonl, until Stop asks it to finish or tmux stops answering:
// it is the only writer of recording.jsonl, so Stop and film off hand off to
// it through tmux options rather than writing or killing anything under it.
// Each failed poll is logged to log (recorder.log).
func Record(dir string, log io.Writer) error {
	s, err := Load(dir)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, "recording.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer os.WriteFile(filepath.Join(s.Dir, "recorder.done"), nil, 0o644)
	defer f.Close()
	enc := json.NewEncoder(f)
	var last Entry
	first := true
	failures := 0
	for {
		began := time.Now()
		st, err := s.status(true)
		if err != nil {
			failures++
			fmt.Fprintf(log, "poll %d of %d failed: %v\n", failures, maxFailures, err)
			if failures >= maxFailures {
				return enc.Encode(Entry{T: now(), End: true, Reason: err.Error()})
			}
			time.Sleep(100*time.Millisecond - time.Since(began))
			continue
		}
		failures = 0
		e := Entry{T: now(), Film: st.Film, Cols: st.Cols, Rows: st.Rows,
			CursorX: st.CursorX, CursorY: st.CursorY, Cursor: st.CursorVisible, Screen: st.Screen}
		if st.Stop {
			// Flush the final screen unconditionally, even if it looks like
			// the last snapshot, and hold it before ending the take.
			if err := enc.Encode(e); err != nil {
				return err
			}
			return enc.Encode(Entry{T: now() + hold, End: true})
		}
		if !st.Film && !first && last.Film {
			// Filming just stopped. Film the screen as it is now, which a
			// caller's last command already put there, hold it, and confirm
			// to SetFilm so the caller's next action cannot race this.
			filmed := e
			filmed.Film = true
			if err := enc.Encode(filmed); err != nil {
				return err
			}
			e.T = now() + hold
			if err := enc.Encode(e); err != nil {
				return err
			}
			last = e
			if _, err := s.tmux("set-option", "-t", window, filmOffAck, "1"); err != nil {
				fmt.Fprintf(log, "confirming film off: %v\n", err)
			}
			time.Sleep(100*time.Millisecond - time.Since(began))
			continue
		}
		probe := e
		probe.T = last.T
		if first || probe != last {
			if err := enc.Encode(e); err != nil {
				return err
			}
			last, first = e, false
		}
		time.Sleep(100*time.Millisecond - time.Since(began))
	}
}
