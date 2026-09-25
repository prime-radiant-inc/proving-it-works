package browse

import (
	"sort"

	"github.com/prime-radiant-inc/proving-it-works/internal/film"
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
func Shots(frames []Frame, marks []Mark) []film.Shot {
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
	var shots []film.Shot
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
		shots = append(shots, film.Shot{T: e.t, Film: state.Film && !end, End: end, Waiting: !state.Busy,
			Look: frames[shown].File, Index: shown})
		if end {
			break
		}
	}
	return shots
}
