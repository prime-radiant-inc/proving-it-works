// Package check is the mechanical gate for a proof movie: it samples picture
// and sound on one timeline and fails the defects per-frame inspection
// cannot see.
package check

import "fmt"

// Thresholds are heuristics tuned against real good and bad movies. They
// catch the egregious cases; they cannot say a movie is right.
const (
	pixelDelta  = 8     // grey levels a pixel must move to count as moved
	changeFrac  = 0.002 // more than 0.2% of pixels moved: a new state
	speechDB    = -45.0 // a second at or above this RMS is someone talking
	earlyAction = 0.40  // last change before this fraction of runtime: front-loaded
	tailTalkS   = 5.0   // ...with this much narration after it: broken
	warnTailS   = 15.0  // frozen tail worth mentioning even when it passes
	warnGapS    = 30.0  // a hold this long mid-movie looks like a hang
)

// Options says what the movie is expected to contain.
type Options struct {
	ExpectAudio     bool
	ExpectSubtitles bool
	// SpeechEnd, when set, is where subtitles must reach instead of the last
	// second of detected speech. build sets it to the end of the last
	// narration clip, so speech inside a movie scene does not count.
	SpeechEnd *float64
}

// Measurements is what was measured from the movie, once per second.
type Measurements struct {
	Duration float64
	HasAudio bool
	Changes  []float64 // fraction of pixels moved since the previous second
	Levels   []float64 // RMS in dBFS; empty without audio
}

// Subtitles is what the caller found for the movie.
type Subtitles struct {
	Found  bool     // a sidecar or embedded track exists
	Source string   // the sidecar's name, or "embedded"
	End    *float64 // end of the last cue; nil when there are no cues
}

// Report is the verdict, as written to check.json.
type Report struct {
	Duration      float64  `json:"duration"`
	ChangeSeconds []int    `json:"change_seconds"`
	TalkSeconds   []int    `json:"talk_seconds"`
	Failures      []string `json:"failures"`
	Warnings      []string `json:"warnings"`
	SubtitleNote  string   `json:"-"`
}

func changeSeconds(changes []float64) []int {
	s := []int{}
	for i, frac := range changes {
		if frac > changeFrac {
			s = append(s, i)
		}
	}
	return s
}

func talkSeconds(levels []float64) []int {
	s := []int{}
	for i, lv := range levels {
		if lv >= speechDB {
			s = append(s, i)
		}
	}
	return s
}

// NeedsSubtitles reports whether Evaluate will look at subtitles, so the
// caller reads them only when it must.
func NeedsSubtitles(m Measurements, o Options) bool {
	return o.ExpectSubtitles && len(talkSeconds(m.Levels)) > 0
}

// Evaluate turns measurements into failures and warnings.
func Evaluate(m Measurements, subs Subtitles, o Options) Report {
	changes, talking := changeSeconds(m.Changes), talkSeconds(m.Levels)
	r := Report{Duration: m.Duration, ChangeSeconds: changes, TalkSeconds: talking,
		Failures: []string{}, Warnings: []string{}}
	fail := func(format string, a ...any) { r.Failures = append(r.Failures, fmt.Sprintf(format, a...)) }
	warn := func(format string, a ...any) { r.Warnings = append(r.Warnings, fmt.Sprintf(format, a...)) }
	span := max(len(m.Changes), 1)

	if m.Duration < 1 {
		fail("duration is %.2fs - that is not a movie", m.Duration)
	}
	if o.ExpectAudio && !m.HasAudio {
		fail("expected narration but there is no audio stream; pass --no-expect-audio if the movie is meant to be silent")
	}
	if o.ExpectAudio && len(m.Levels) > 0 && len(talking) == 0 {
		fail("the audio track is silent end to end; pass --no-expect-audio if the movie is meant to be silent")
	}

	// a narrated movie with no subtitles fails for everyone watching it muted
	if len(talking) > 0 && o.ExpectSubtitles {
		speechEnd := float64(talking[len(talking)-1] + 1)
		if o.SpeechEnd != nil {
			speechEnd = *o.SpeechEnd
		}
		switch {
		case !subs.Found:
			fail("narrated, but no subtitles: expected %s beside the movie (or an embedded track). "+
				"movie build writes and burns them; pass --no-expect-subtitles only for a movie "+
				"nobody will ever watch muted.", subs.Source)
		case subs.End == nil:
			fail("%s: subtitles contain no cues", subs.Source)
		default:
			r.SubtitleNote = fmt.Sprintf("%s, last cue ends at %.1fs (narration ends %.0fs)", subs.Source, *subs.End, speechEnd)
			if *subs.End < speechEnd-3 {
				fail("subtitles stop at %.0fs but the narration runs to %.0fs - %.0fs of speech has no subtitles",
					*subs.End, speechEnd, speechEnd-*subs.End)
			}
		}
	}

	if len(changes) == 0 {
		fail("the picture never reaches a new state - this is a still, not a movie")
		return r
	}
	lastChange := changes[len(changes)-1]
	tailTalk := 0
	if len(talking) > 0 {
		tailTalk = talking[len(talking)-1] - lastChange
	}
	if float64(lastChange) < earlyAction*float64(span) && float64(tailTalk) > tailTalkS {
		fail("every visible change happens in the first %ds (%.0f%% of runtime), then the picture is "+
			"frozen for %ds while narration keeps talking for %ds of it. The demo is over before the "+
			"explanation starts: pace the action to the narration.",
			lastChange, 100*float64(lastChange)/float64(span), span-lastChange, tailTalk)
	} else if float64(tailTalk) > warnTailS {
		warn("%ds of narration after the last visible change (%.0f%% of runtime frozen)",
			tailTalk, 100*float64(span-lastChange)/float64(span))
	}
	gap := 0
	for i := 1; i < len(changes); i++ {
		gap = max(gap, changes[i]-changes[i-1])
	}
	if float64(gap) > warnGapS {
		warn("%ds with no visible change mid-movie - intentional hold, or did something hang?", gap)
	}
	return r
}
