package srt

import (
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

// Cue is one subtitle: seconds from the start of the movie, and its text.
type Cue struct {
	Start, End float64
	Text       string
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// round matches the original tool, which rounded halves to even.
func round(x float64) int { return int(math.RoundToEven(x)) }

// SceneCues splits one scene's narration into cues that exactly tile
// [start, start+duration] in whole milliseconds, each cue's share of time
// proportional to its characters. Readability limits guide where chunks
// split; they never cut off the end of the scene or drop a word.
func SceneCues(text string, start, duration float64, maxChars int, maxSecs float64) ([]Cue, error) {
	end := start + duration
	for _, v := range []float64{start, duration, end} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("start must be finite and nonnegative; duration must be finite and positive")
		}
	}
	if start < 0 || duration <= 0 {
		return nil, errors.New("start must be finite and nonnegative; duration must be finite and positive")
	}
	startMS, endMS := round(start*1000), round(end*1000)
	available := endMS - startMS
	if available <= 0 {
		return nil, errors.New("scene has no representable millisecond subtitle interval")
	}
	charLimit := max(1, min(maxChars, int(float64(runes(text))*math.Min(1, maxSecs/duration))))
	chunks := chunk(text, charLimit)
	if len(chunks) > available {
		n := len(chunks)
		grouped := make([]string, available)
		for i := range available {
			grouped[i] = strings.Join(chunks[i*n/available:(i+1)*n/available], " ")
		}
		chunks = grouped
	}
	for {
		total := 0
		for _, c := range chunks {
			total += runes(c)
		}
		total = max(total, 1)
		elapsed, previous, split := 0, startMS, -1
		var cues []Cue
		for i, c := range chunks {
			elapsed += runes(c)
			remaining := len(chunks) - i - 1
			boundary := startMS + round(float64(available)*float64(elapsed)/float64(total))
			// reserve one millisecond per remaining cue, even for very uneven text
			boundary = min(endMS-remaining, max(previous+1, boundary))
			if remaining == 0 {
				boundary = endMS
			}
			if split < 0 && float64(boundary-previous) > maxSecs*1000 && len(strings.Fields(c)) > 1 {
				split = i
			}
			cues = append(cues, Cue{float64(previous) / 1000, float64(boundary) / 1000, wrap(c, 42)})
			previous = boundary
		}
		if split < 0 || len(chunks) == available {
			return cues, nil
		}
		// Splitting changes every cue's share, so remeasure all of them until
		// every splittable chunk fits or milliseconds limit the count.
		w := strings.Fields(chunks[split])
		mid := len(w) / 2
		chunks = slices.Replace(chunks, split, split+1, strings.Join(w[:mid], " "), strings.Join(w[mid:], " "))
	}
}

// chunk splits text into cue-sized pieces at word boundaries, ending a piece
// early at a sentence end once it is reasonably long.
func chunk(text string, maxChars int) []string {
	var chunks []string
	cur := ""
	for _, w := range strings.Fields(text) {
		try := strings.TrimSpace(cur + " " + w)
		if runes(try) > maxChars && cur != "" {
			chunks = append(chunks, cur)
			cur = w
			continue
		}
		cur = try
		if strings.HasSuffix(cur, ".") || strings.HasSuffix(cur, "!") || strings.HasSuffix(cur, "?") {
			if float64(runes(cur)) > float64(maxChars)*0.45 {
				chunks = append(chunks, cur)
				cur = ""
			}
		}
	}
	if cur != "" {
		chunks = append(chunks, cur)
	}
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

// wrap breaks a cue into at most two lines near width characters.
func wrap(line string, width int) string {
	var out []string
	cur := ""
	for _, w := range strings.Fields(line) {
		try := strings.TrimSpace(cur + " " + w)
		if runes(try) > width && cur != "" {
			out = append(out, cur)
			cur = w
		} else {
			cur = try
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) <= 2 {
		return strings.Join(out, "\n")
	}
	half := len(out) / 2
	return strings.Join(out[:half], " ") + "\n" + strings.Join(out[half:], " ")
}

// Timestamp formats seconds as an SRT time, HH:MM:SS,mmm.
func Timestamp(seconds float64) string {
	ms := round(seconds * 1000)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// Write writes cues as SRT.
func Write(w io.Writer, cues []Cue) error {
	for i, c := range cues {
		if _, err := fmt.Fprintf(w, "%d\n%s --> %s\n%s\n\n", i+1, Timestamp(c.Start), Timestamp(c.End), c.Text); err != nil {
			return err
		}
	}
	return nil
}
