package term

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Entry is one recorded snapshot of the filmed pane.
type Entry struct {
	T       float64 `json:"t"`
	End     bool    `json:"end,omitempty"`
	Film    bool    `json:"film"`
	Cols    int     `json:"cols"`
	Rows    int     `json:"rows"`
	CursorX int     `json:"cx"`
	CursorY int     `json:"cy"`
	Cursor  bool    `json:"cursor"`
	Screen  string  `json:"screen"`
}

func now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// Record snapshots the pane up to ten times a second, appending each change
// to recording.jsonl, until the tmux server is gone.
func Record(dir string) error {
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
	for {
		began := time.Now()
		st, err := s.status(true)
		if err != nil {
			return enc.Encode(Entry{T: now(), End: true})
		}
		e := Entry{T: now(), Film: st.Film, Cols: st.Cols, Rows: st.Rows,
			CursorX: st.CursorX, CursorY: st.CursorY, Cursor: st.CursorVisible, Screen: st.Screen}
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
