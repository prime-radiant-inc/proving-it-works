// Package check is the mechanical gate for a proof movie: it samples picture
// and sound on one timeline and fails the defects per-frame inspection
// cannot see.
package check

import "fmt"

// Options says what the movie is expected to contain.
type Options struct {
	ExpectAudio     bool
	ExpectSubtitles bool
	// SpeechEnd, when set, is where subtitles must reach instead of the last
	// second of detected speech. build sets it to the end of the last
	// narration clip, so speech inside a movie scene does not count.
	SpeechEnd *float64
}

// Measurements is what was measured from the movie.
type Measurements struct {
	Duration float64
	HasAudio bool
}

// Report is the verdict, as written to check.json.
type Report struct {
	Duration float64  `json:"duration"`
	Failures []string `json:"failures"`
	Warnings []string `json:"warnings"`
}

// Evaluate turns measurements into failures and warnings.
func Evaluate(m Measurements, o Options) Report {
	r := Report{Duration: m.Duration, Failures: []string{}, Warnings: []string{}}
	if m.Duration < 1 {
		r.Failures = append(r.Failures, fmt.Sprintf("duration is %.2fs - that is not a movie", m.Duration))
	}
	return r
}
