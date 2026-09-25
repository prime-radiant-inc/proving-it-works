package film

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSplitOnFilmOffAndEnd(t *testing.T) {
	shots := []Shot{
		{T: 0, Film: true}, {T: 1, Film: true}, {T: 2, Film: false},
		{T: 5, Film: true}, {T: 6, End: true},
	}
	takes := Split(shots)
	if len(takes) != 2 {
		t.Fatalf("got %d takes", len(takes))
	}
	if takes[0].Start != 0 || takes[0].End != 2 || len(takes[0].Shots) != 2 {
		t.Errorf("take 1: %+v", takes[0])
	}
	if takes[1].Start != 5 || takes[1].End != 6 || len(takes[1].Shots) != 1 {
		t.Errorf("take 2: %+v", takes[1])
	}
}

func TestTakeWithoutEndMarkerHoldsItsLastShotOneSecond(t *testing.T) {
	takes := Split([]Shot{{T: 10, Film: true}, {T: 10.5, Film: true}})
	if len(takes) != 1 || takes[0].End != 11.5 {
		t.Fatalf("got %+v", takes)
	}
}

func TestSlotsShowTheLatestShotAtEachFrame(t *testing.T) {
	take := Take{Start: 0, End: 0.5, Shots: []Shot{{T: 0}, {T: 0.25}}}
	if got := Slots(take, 10); !slices.Equal(got, []int{0, 0, 0, 1, 1}) {
		t.Fatalf("got %v", got)
	}
}

// While the program sits waiting for the next input, the agent driving it
// is thinking; that time is not the program's and is cut to the hold. Time
// the program spends working is never shortened.
func TestTightenCapsTimeSpentWaitingForInput(t *testing.T) {
	take := Take{Start: 100, End: 121.5, Shots: []Shot{
		{T: 100, Waiting: true, Look: "$ "},             // idle before the first command: 5 s
		{T: 105, Look: "$ make"},                        // typing: 1 s
		{T: 106, Look: "$ make\nbuilding"},              // running: 1 s
		{T: 107, Waiting: true, Look: "$ make\nok\n$ "}, // result, then the agent thinks: 13 s
		{T: 120, Waiting: true, Look: "$ make\nok\n$ "}, // film off's flush and hold: 1.5 s
	}}
	got := Tighten(take, 1.5)
	wantT := []float64{100, 101.5, 102.5, 103.5, 105}
	for i, s := range got.Shots {
		if math.Abs(s.T-wantT[i]) > 1e-9 {
			t.Errorf("shot %d at %v, want %v", i, s.T, wantT[i])
		}
	}
	if math.Abs(got.End-105) > 1e-9 {
		t.Errorf("take ends at %v, want 105 (the result held 1.5 s)", got.End)
	}
}

func TestSettledIsWhenThePictureLastChanged(t *testing.T) {
	take := Take{Start: 10, End: 16, Shots: []Shot{
		{T: 10, Look: "$ "}, {T: 11, Look: "$ ls"}, {T: 12.5, Look: "$ ls\na b\n$ "},
		{T: 14, Look: "$ ls\na b\n$ "},
	}}
	if got := Settled(take); math.Abs(got-2.5) > 1e-9 {
		t.Errorf("settled at %v, want 2.5", got)
	}
	if got := Settled(Take{Start: 3, End: 5, Shots: []Shot{{T: 3, Look: "$ "}}}); got != 0 {
		t.Errorf("a take that never changes settles at 0, got %v", got)
	}
}

func TestHoldLastKeepsTheFinalPictureUpLongEnough(t *testing.T) {
	take := Take{Start: 0, End: 2.2, Shots: []Shot{{T: 0, Look: "a"}, {T: 2, Look: "b"}}}
	if got := HoldLast(take, 1.5); math.Abs(got.End-3.5) > 1e-9 {
		t.Errorf("take ends at %v, want 3.5 (the result held 1.5 s)", got.End)
	}
	long := Take{Start: 0, End: 9, Shots: []Shot{{T: 0, Look: "a"}, {T: 2, Look: "b"}}}
	if got := HoldLast(long, 1.5); got.End != 9 {
		t.Errorf("a take already holding its result was changed to end at %v", got.End)
	}
}

func TestEachSentenceBelongsToTheTakeItWasSpokenIn(t *testing.T) {
	says := Narrations(3, []Beat{{Take: 1, Say: "First."}, {Take: 2, Say: "Second."}, {Take: 2, Say: "More."}, {Take: 9, Say: "lost"}})
	if len(says) != 3 || says[0] != "First." || says[1] != "Second. More." || says[2] != "" {
		t.Fatalf("got %q", says)
	}
}

func TestBeatsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if beats, err := ReadBeats(dir); err != nil || beats != nil {
		t.Fatalf("no beats file: got %v, %v", beats, err)
	}
	for _, b := range []Beat{{Take: 1, Say: "One."}, {Take: 3, Say: "Three."}} {
		if err := AppendBeat(dir, b); err != nil {
			t.Fatal(err)
		}
	}
	beats, err := ReadBeats(dir)
	if err != nil || !slices.Equal(beats, []Beat{{Take: 1, Say: "One."}, {Take: 3, Say: "Three."}}) {
		t.Fatalf("got %v, %v", beats, err)
	}
}

func TestTypingPaceStaysHumanAndBounded(t *testing.T) {
	if got := TypingPace(10).Milliseconds(); got != 55 {
		t.Errorf("short text: %d ms per character, want 55", got)
	}
	if got := TypingPace(400).Milliseconds(); got != 10 {
		t.Errorf("long text: %d ms per character, want 10 (4 s in all)", got)
	}
}

func solidPNG(t *testing.T, c color.Gray) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = c.Y
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestWriteRendersTakesAndTheSceneFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "takes")
	takes := Split([]Shot{
		{T: 0, Film: true, Look: "a", Index: 0}, {T: 0.3, Film: true, Look: "b", Index: 1},
		{T: 2, Film: false, Look: "b", Index: 1},
		{T: 3, Film: true, Look: "c", Index: 2}, {T: 5, End: true},
	})
	pictures := [][]byte{solidPNG(t, color.Gray{0}), solidPNG(t, color.Gray{128}), solidPNG(t, color.Gray{255})}
	var drawn []int
	draw := func(s Shot) ([]byte, error) {
		drawn = append(drawn, s.Index)
		return pictures[s.Index], nil
	}
	var stdout bytes.Buffer
	m := Movie{Tool: "movie test stop", Title: "Demo", Subtitle: "it works", Size: image.Pt(4, 4)}
	names, err := Write(out, m, takes, []Beat{{Take: 2, Say: "The third picture."}}, draw, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"take-1", "take-2"}) {
		t.Fatalf("names %v", names)
	}
	if !slices.Equal(drawn, []int{0, 1, 2}) {
		t.Errorf("each shot is drawn once, in order; drew %v", drawn)
	}
	// take 1: 0.3 s of a, then b held from 0.3 to 2.0 (at least 1.5 s after it appeared)
	frames, _ := filepath.Glob(filepath.Join(out, "take-1", "f*.png"))
	if len(frames) != 20 {
		t.Errorf("take-1 has %d frames, want 20", len(frames))
	}
	var meta struct {
		Rate    int     `json:"rate"`
		Settled float64 `json:"settled"`
	}
	data, err := os.ReadFile(filepath.Join(out, "take-1", "take.json"))
	if err != nil || json.Unmarshal(data, &meta) != nil || meta.Rate != FPS || meta.Settled != 0.3 {
		t.Errorf("take.json %s: %v", data, err)
	}
	scenes, err := os.ReadFile(filepath.Join(out, "scenes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Written by movie test stop.", "size: 4x4", "card: Demo", "subtitle: it works",
		"frames: take-2", "narration: The third picture."} {
		if !strings.Contains(string(scenes), want) {
			t.Errorf("scenes.yaml lacks %q:\n%s", want, scenes)
		}
	}
	if !strings.Contains(stdout.String(), "2 takes, 1 narrated") {
		t.Errorf("summary: %s", stdout.String())
	}
}

func TestWriteRefusesAnOutputDirectoryInUse(t *testing.T) {
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "old.txt"), nil, 0o644)
	takes := Split([]Shot{{T: 0, Film: true}, {T: 1, End: true}})
	_, err := Write(out, Movie{Size: image.Pt(4, 4)}, takes, nil, nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("got %v", err)
	}
}

func TestWriteRefusesNothingFilmed(t *testing.T) {
	_, err := Write(filepath.Join(t.TempDir(), "x"), Movie{Size: image.Pt(4, 4)}, nil, nil, nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "nothing was filmed") {
		t.Fatalf("got %v", err)
	}
}
