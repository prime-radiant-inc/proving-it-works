package srt

import (
	"math"
	"strings"
	"testing"
)

type ms struct {
	start, end int
	text       string
}

func cuesMS(t *testing.T, text string, start, duration float64, maxChars int, maxSecs float64) []ms {
	t.Helper()
	cues, err := SceneCues(text, start, duration, maxChars, maxSecs)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]ms, len(cues))
	for i, c := range cues {
		out[i] = ms{int(math.Round(c.Start * 1000)), int(math.Round(c.End * 1000)), strings.Join(strings.Fields(c.Text), " ")}
	}
	return out
}

// assertScene: cues tile [start, end] exactly, in order, keeping every word.
func assertScene(t *testing.T, cues []ms, start, end int, text string) {
	t.Helper()
	if len(cues) == 0 || cues[0].start != start || cues[len(cues)-1].end != end {
		t.Fatalf("cues %v do not span %d..%d", cues, start, end)
	}
	previous := start
	var words []string
	for _, c := range cues {
		if c.start != previous || c.start >= c.end || c.end > end {
			t.Fatalf("cues %v are not contiguous and positive", cues)
		}
		previous = c.end
		words = append(words, c.text)
	}
	if strings.Join(words, " ") != text {
		t.Fatalf("words %q, want %q", strings.Join(words, " "), text)
	}
}

func TestFiveChunksFitHalfASecond(t *testing.T) {
	assertScene(t, cuesMS(t, "one two six ten red", 0, 0.5, 3, 5.5), 0, 500, "one two six ten red")
}

func TestOneWordCoversATwelveSecondScene(t *testing.T) {
	if got := cuesMS(t, "Held", 0, 12, 84, 5.5); len(got) != 1 || got[0] != (ms{0, 12000, "Held"}) {
		t.Fatal(got)
	}
}

func TestMixedChunksGetProportionalTime(t *testing.T) {
	got := cuesMS(t, "a bbbbbbbbb", 0, 1, 9, 5.5)
	if len(got) != 2 || got[0] != (ms{0, 100, "a"}) || got[1] != (ms{100, 1000, "bbbbbbbbb"}) {
		t.Fatal(got)
	}
}

func TestMaxSecondsSplitsWithoutLosingWords(t *testing.T) {
	text := "one two six ten red cat dog fox"
	got := cuesMS(t, text, 0, 12, 84, 3)
	assertScene(t, got, 0, 12000, text)
	for _, c := range got {
		if c.end-c.start > 3000 || len(got) < 2 {
			t.Fatal(got)
		}
	}
}

func TestMaxSecondsRefinesUnequalChunks(t *testing.T) {
	got := cuesMS(t, "ab cde f ghi", 0, 6, 84, 3)
	assertScene(t, got, 0, 6000, "ab cde f ghi")
	for _, c := range got {
		if c.end-c.start > 3000 {
			t.Fatal(got)
		}
	}
}

func TestChunksCoalesceToRepresentableMilliseconds(t *testing.T) {
	got := cuesMS(t, "one two six ten red", 0, 0.002, 3, 5.5)
	assertScene(t, got, 0, 2, "one two six ten red")
	if len(got) > 2 {
		t.Fatal(got)
	}
}

func TestUnrepresentableMaxSecondsKeepsPositiveCues(t *testing.T) {
	got := cuesMS(t, "one two six ten red", 0, 0.002, 84, 0.0001)
	assertScene(t, got, 0, 2, "one two six ten red")
}

func TestSubMillisecondSceneUsesItsRoundedInterval(t *testing.T) {
	assertScene(t, cuesMS(t, "one two", 0, 0.0008, 84, 5.5), 0, 1, "one two")
}

func TestOffsetUsesRoundedSceneBoundaries(t *testing.T) {
	assertScene(t, cuesMS(t, "one two six", 2.1254, 0.5004, 3, 5.5), 2125, 2626, "one two six")
}

func TestInvalidDurationsAreRejected(t *testing.T) {
	for _, d := range []float64{0, -1, 0.0001, math.NaN(), math.Inf(1)} {
		if _, err := SceneCues("words", 0, d, 84, 5.5); err == nil {
			t.Errorf("duration %v accepted", d)
		}
	}
}

func TestWriteProducesParseableSRT(t *testing.T) {
	var b strings.Builder
	if err := Write(&b, []Cue{{0, 1.5, "one"}, {1.5, 3.25, "two"}}); err != nil {
		t.Fatal(err)
	}
	if got := end(t, b.String()); got != 3.25 {
		t.Fatal(got)
	}
	if !strings.Contains(b.String(), "00:00:01,500 --> 00:00:03,250") {
		t.Fatal(b.String())
	}
}
