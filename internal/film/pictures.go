package film

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/prime-radiant-inc/proving-it-works/internal/jsonl"
)

// Frame is one line of frames.jsonl, which only the recorder writes: a
// picture of the page from T on, saved as frames/File, or the end of the
// recording, with Reason set when it ended without a stop.
type Frame struct {
	T      float64 `json:"t"`
	File   string  `json:"frame,omitempty"`
	End    bool    `json:"end,omitempty"`
	Reason string  `json:"reason,omitempty"`
}

// Mark is one line of marks.jsonl, which only the verbs write: from T on,
// whether filming is on and whether an action is under way (Busy), or, with
// End, that the session stopped.
type Mark struct {
	T    float64 `json:"t"`
	Film bool    `json:"film,omitempty"`
	Busy bool    `json:"busy,omitempty"`
	End  bool    `json:"end,omitempty"`
}

// Shots merges the recorder's frames and the verbs' marks into one timeline
// by time: a shot at every frame and every mark from the first frame on,
// each showing the latest frame with the latest mark's film and busy state.
// Time no action is busy is time the page waits on the agent. The timeline
// ends at the first end, from a stop or from the recorder ending on its own.
func Shots(frames []Frame, marks []Mark) []Shot {
	type event struct {
		t     float64
		frame int // index into frames, or -1 for a mark
		mark  Mark
	}
	var events []event
	for i, f := range frames {
		events = append(events, event{t: f.T, frame: i})
	}
	for _, m := range marks {
		events = append(events, event{t: m.T, frame: -1, mark: m})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].t < events[j].t })
	var shots []Shot
	state, shown := Mark{}, -1
	for _, e := range events {
		end := false
		if e.frame >= 0 {
			if frames[e.frame].End {
				end = true
			} else {
				shown = e.frame
			}
		} else {
			state = e.mark
			end = e.mark.End
		}
		if shown < 0 {
			continue
		}
		shots = append(shots, Shot{T: e.t, Film: state.Film && !end, End: end, Waiting: !state.Busy,
			Look: frames[shown].File, Index: shown})
		if end {
			break
		}
	}
	return shots
}

// Reel is a picture recorder's writer: it saves each frame that differs from
// the one before to DIR/frames/NNNNNN.png and logs it in DIR/frames.jsonl,
// of which it is the only writer.
type Reel struct {
	dir  string
	last []byte
	n    int
}

// NewReel starts a reel in dir.
func NewReel(dir string) (*Reel, error) {
	if err := os.MkdirAll(filepath.Join(dir, "frames"), 0o755); err != nil {
		return nil, err
	}
	return &Reel{dir: dir}, nil
}

// Add records picture, a PNG, as on screen from time t. A picture identical
// to the last one is dropped: nothing changed.
func (r *Reel) Add(t float64, picture []byte) error {
	if bytes.Equal(picture, r.last) {
		return nil
	}
	r.last = picture
	r.n++
	name := fmt.Sprintf("%06d.png", r.n)
	if err := os.WriteFile(filepath.Join(r.dir, "frames", name), picture, 0o644); err != nil {
		return err
	}
	return jsonl.Append(filepath.Join(r.dir, "frames.jsonl"), Frame{T: t, File: name})
}

// End records that the recording ended at t; reason says why when it ended
// without a stop.
func (r *Reel) End(t float64, reason string) error {
	return jsonl.Append(filepath.Join(r.dir, "frames.jsonl"), Frame{T: t, End: true, Reason: reason})
}

// RenderPictures draws every take of the picture recording in dir into
// outdir/take-N/ and writes outdir/scenes.yaml. The pictures' size is the
// scene size.
func RenderPictures(dir, outdir string, m Movie, stdout io.Writer) error {
	frames, err := jsonl.Read[Frame](filepath.Join(dir, "frames.jsonl"))
	if err != nil {
		return err
	}
	marks, err := jsonl.Read[Mark](filepath.Join(dir, "marks.jsonl"))
	if err != nil {
		return err
	}
	beats, err := ReadBeats(dir)
	if err != nil {
		return err
	}
	shots := Shots(frames, marks)
	if len(shots) > 0 {
		first, err := os.ReadFile(filepath.Join(dir, "frames", shots[0].Look))
		if err != nil {
			return err
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(first))
		if err != nil {
			return fmt.Errorf("frames/%s: %w", shots[0].Look, err)
		}
		m.Size = image.Pt(cfg.Width, cfg.Height)
	}
	draw := func(shot Shot) ([]byte, error) { return os.ReadFile(filepath.Join(dir, "frames", shot.Look)) }
	if _, err := Write(outdir, m, Split(shots), beats, draw, stdout); err != nil {
		return err
	}
	for _, f := range frames {
		if f.End && f.Reason != "" {
			fmt.Fprintf(stdout, "WARN       the recording ended without a stop: %s\n", f.Reason)
		}
	}
	return nil
}
