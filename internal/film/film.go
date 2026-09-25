// Package film turns a recording into takes: it splits the recording where
// filming stops, cuts the time spent waiting on the agent, gives each take
// its narration, and writes the frames and scene file movie build reads.
// term and browse record differently and share everything from here on.
package film

import (
	"math"
	"time"
)

// FPS is the frame rate of rendered takes.
const FPS = 10

// Hold is how long a take's final picture stays up, and the most time spent
// waiting for the agent's next input that a take keeps.
const Hold = 1.5

// Shot is one moment of a recording: what was on screen from T until the
// next shot.
type Shot struct {
	T    float64
	Film bool
	End  bool
	// Waiting is set while the program waits for the agent's next input:
	// time the agent spends thinking, not time the program spends working.
	Waiting bool
	// Look identifies the picture: shots with equal Looks show the same thing.
	Look string
	// Index is which of the recorder's own entries this shot draws.
	Index int
}

// Take is one stretch of recording filmed without a break.
type Take struct {
	Start, End float64
	Shots      []Shot
}

// Split splits a recording into takes. Filming starts at a shot with Film
// set and stops at the next shot without it, or at the end marker. A
// recording that stops without an end marker holds its last shot for one
// second.
func Split(shots []Shot) []Take {
	var takes []Take
	var cur *Take
	for _, s := range shots {
		filming := s.Film && !s.End
		switch {
		case filming && cur == nil:
			cur = &Take{Start: s.T, Shots: []Shot{s}}
		case filming:
			cur.Shots = append(cur.Shots, s)
		case cur != nil:
			cur.End = s.T
			takes = append(takes, *cur)
			cur = nil
		}
	}
	if cur != nil {
		cur.End = cur.Shots[len(cur.Shots)-1].T + 1
		takes = append(takes, *cur)
	}
	return takes
}

// Tighten caps each stretch spent waiting for the agent's next input at
// maxWait seconds, counting consecutive waiting shots of the same picture as
// one stretch. Time the program spends working is never shortened.
func Tighten(t Take, maxWait float64) Take {
	out := Take{Start: t.Start, Shots: make([]Shot, len(t.Shots))}
	shift, waited := 0.0, 0.0
	for i, s := range t.Shots {
		next := t.End
		if i+1 < len(t.Shots) {
			next = t.Shots[i+1].T
		}
		span := next - s.T
		if !s.Waiting || i == 0 || !t.Shots[i-1].Waiting || t.Shots[i-1].Look != s.Look {
			waited = 0
		}
		keep := span
		if s.Waiting {
			keep = math.Max(0, math.Min(span, maxWait-waited))
			waited += keep
		}
		s.T -= shift
		out.Shots[i] = s
		shift += span - keep
	}
	out.End = t.End - shift
	return out
}

// Settled is how many seconds into a take its picture last changed: where
// the take's result has appeared, or 0 if the picture never changes.
func Settled(t Take) float64 {
	settled := 0.0
	for i := 1; i < len(t.Shots); i++ {
		if t.Shots[i].Look != t.Shots[i-1].Look {
			settled = t.Shots[i].T - t.Start
		}
	}
	return settled
}

// HoldLast makes a take's final picture stay up at least hold seconds, so a
// take that is cut the moment its result appears still shows the result.
func HoldLast(t Take, hold float64) Take {
	t.End = math.Max(t.End, t.Start+Settled(t)+hold)
	return t
}

// Slots returns, for each frame of a take at fps, the index of the shot on
// screen at that moment.
func Slots(t Take, fps float64) []int {
	n := int(math.Ceil((t.End-t.Start)*fps - 1e-9))
	slots := make([]int, n)
	j := 0
	for k := range n {
		at := t.Start + float64(k)/fps
		for j+1 < len(t.Shots) && t.Shots[j+1].T <= at {
			j++
		}
		slots[k] = j
	}
	return slots
}

// TypingPace is the delay between typed characters: about 55 ms, faster for
// long text so typing never takes more than 4 seconds.
func TypingPace(n int) time.Duration {
	if n == 0 {
		return 0
	}
	return min(55*time.Millisecond, 4*time.Second/time.Duration(n))
}
