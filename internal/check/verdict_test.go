package check

import (
	"slices"
	"strings"
	"testing"
)

func repeat(v float64, n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = v
	}
	return s
}

// paced is a 20 s movie whose picture changes every second.
func paced(levels []float64) Measurements {
	return Measurements{Duration: float64(len(levels)), HasAudio: true,
		Changes: repeat(0.1, len(levels)-1), Levels: levels}
}

func f(v float64) *float64 { return &v }

func hasFailure(r Report, needle string) bool {
	return slices.ContainsFunc(r.Failures, func(s string) bool { return strings.Contains(s, needle) })
}

func TestSilentTrackPassesWhenAudioIsNotExpected(t *testing.T) {
	r := Evaluate(paced(repeat(-120, 20)), Subtitles{}, Options{ExpectSubtitles: true})
	if len(r.Failures) != 0 {
		t.Fatal(r.Failures)
	}
}

func TestAbsentAudioPassesWhenAudioIsNotExpected(t *testing.T) {
	m := Measurements{Duration: 20, Changes: repeat(0.1, 19)}
	if r := Evaluate(m, Subtitles{}, Options{ExpectSubtitles: true}); len(r.Failures) != 0 {
		t.Fatal(r.Failures)
	}
}

func TestSilentTrackFailsWhenAudioIsExpected(t *testing.T) {
	r := Evaluate(paced(repeat(-120, 20)), Subtitles{}, Options{ExpectAudio: true, ExpectSubtitles: true})
	if !hasFailure(r, "silent") {
		t.Fatal(r.Failures)
	}
}

func TestMissingAudioStreamFailsWhenAudioIsExpected(t *testing.T) {
	m := Measurements{Duration: 20, Changes: repeat(0.1, 19)}
	if r := Evaluate(m, Subtitles{}, Options{ExpectAudio: true}); !hasFailure(r, "no audio stream") {
		t.Fatal(r.Failures)
	}
}

func TestAudibleSpeechStillNeedsSubtitlesWithoutAudioExpectation(t *testing.T) {
	r := Evaluate(paced(repeat(-20, 20)), Subtitles{Source: "movie.srt"}, Options{ExpectSubtitles: true})
	if !hasFailure(r, "no subtitles") {
		t.Fatal(r.Failures)
	}
}

func TestSubtitleOptOutAllowsSpeechWithoutSubtitles(t *testing.T) {
	if r := Evaluate(paced(repeat(-20, 20)), Subtitles{}, Options{}); len(r.Failures) != 0 {
		t.Fatal(r.Failures)
	}
}

func TestTrackWithNoCuesFails(t *testing.T) {
	for _, n := range []int{2, 20} {
		r := Evaluate(paced(repeat(-20, n)), Subtitles{Found: true, Source: "embedded"}, Options{ExpectSubtitles: true})
		if !hasFailure(r, "no cues") {
			t.Errorf("%d s: %q", n, r.Failures)
		}
	}
}

func TestCuesMustReachTheEndOfSpeech(t *testing.T) {
	levels := append(repeat(-20, 10), repeat(-120, 10)...)
	short := Evaluate(paced(levels), Subtitles{Found: true, Source: "x.srt", End: f(6)}, Options{ExpectSubtitles: true})
	if !hasFailure(short, "subtitles stop at 6s") {
		t.Fatal(short.Failures)
	}
	enough := Evaluate(paced(levels), Subtitles{Found: true, Source: "x.srt", End: f(10)}, Options{ExpectSubtitles: true})
	if len(enough.Failures) != 0 {
		t.Fatal(enough.Failures)
	}
}

func TestSpeechEndOverridesDetectedSpeech(t *testing.T) {
	// speech runs all 20 s, but the narration ends at 8 s: a movie scene is talking
	r := Evaluate(paced(repeat(-20, 20)), Subtitles{Found: true, Source: "x.srt", End: f(6)},
		Options{ExpectSubtitles: true, SpeechEnd: f(8)})
	if len(r.Failures) != 0 {
		t.Fatal(r.Failures)
	}
}

func TestFrontLoadedActionFails(t *testing.T) {
	m := Measurements{Duration: 22, HasAudio: true,
		Changes: append(repeat(0.1, 2), repeat(0, 19)...), Levels: repeat(-20, 22)}
	r := Evaluate(m, Subtitles{Found: true, Source: "x.srt", End: f(22)}, Options{ExpectSubtitles: true})
	if !hasFailure(r, "every visible change happens in the first 1s") {
		t.Fatal(r.Failures)
	}
}

func TestStillFails(t *testing.T) {
	m := Measurements{Duration: 12, Changes: repeat(0, 11)}
	if r := Evaluate(m, Subtitles{}, Options{}); !hasFailure(r, "never reaches a new state") {
		t.Fatal(r.Failures)
	}
}

func TestLongMidMovieHoldWarns(t *testing.T) {
	changes := repeat(0, 40)
	changes[0], changes[35] = 0.1, 0.1
	r := Evaluate(Measurements{Duration: 41, Changes: changes}, Subtitles{}, Options{})
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "35s with no visible change") {
		t.Fatal(r.Warnings)
	}
}
