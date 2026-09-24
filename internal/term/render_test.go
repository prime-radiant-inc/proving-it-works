package term

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTakesSplitOnFilmOffAndEnd(t *testing.T) {
	entries := []Entry{
		{T: 0, Film: true}, {T: 1, Film: true}, {T: 2, Film: false},
		{T: 5, Film: true}, {T: 6, End: true},
	}
	takes := Takes(entries)
	if len(takes) != 2 {
		t.Fatalf("got %d takes", len(takes))
	}
	if takes[0].Start != 0 || takes[0].End != 2 || len(takes[0].Entries) != 2 {
		t.Errorf("take 1: %+v", takes[0])
	}
	if takes[1].Start != 5 || takes[1].End != 6 || len(takes[1].Entries) != 1 {
		t.Errorf("take 2: %+v", takes[1])
	}
}

func TestTakeWithoutEndMarkerHoldsItsLastSnapshotOneSecond(t *testing.T) {
	takes := Takes([]Entry{{T: 10, Film: true}, {T: 10.5, Film: true}})
	if len(takes) != 1 || takes[0].End != 11.5 {
		t.Fatalf("got %+v", takes)
	}
}

func TestSlotsShowTheLatestSnapshotAtEachFrame(t *testing.T) {
	take := Take{Start: 0, End: 0.5, Entries: []Entry{{T: 0}, {T: 0.25}}}
	if got := Slots(take, 10); !slices.Equal(got, []int{0, 0, 0, 1, 1}) {
		t.Fatalf("got %v", got)
	}
}

func writeRecording(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "recording.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadRecordingTruncatesOnlyATrailingBadLine(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, `{"t":0,"film":true}`+"\n"+`{"t":0.1,"film":tr`)
	entries, err := readRecording(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].T != 0 {
		t.Fatalf("got %+v", entries)
	}
}

func TestReadRecordingErrorsOnABadLineFollowedByMore(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, `{"t":0,"film":true}`+"\n"+`not json`+"\n"+`{"t":0.2,"film":true}`)
	_, err := readRecording(dir)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("got %v, want an error naming line 2", err)
	}
}

func TestRenderWarnsWhenTheRecordingEndedWithoutAStop(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, `{"t":0,"film":true,"cols":10,"rows":2,"screen":"hi\n"}`+"\n"+
		`{"t":1,"end":true,"reason":"tmux display-message: exit status 1: no server running"}`+"\n")
	var out bytes.Buffer
	if err := Render(dir, filepath.Join(dir, "frames"), image.Pt(200, 100), &out); err != nil {
		t.Fatal(err)
	}
	want := "WARN       the recording ended without a stop: tmux display-message: exit status 1: no server running"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("render printed:\n%s\nwant a line containing %q", out.String(), want)
	}
}

func TestRenderDoesNotWarnAboutAStoppedRecording(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, `{"t":0,"film":true,"cols":10,"rows":2,"screen":"hi\n"}`+"\n"+`{"t":1,"end":true}`+"\n")
	var out bytes.Buffer
	if err := Render(dir, filepath.Join(dir, "frames"), image.Pt(200, 100), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "WARN") {
		t.Fatalf("render warned about a stopped recording:\n%s", out.String())
	}
}

func TestRenderMatchesGoldenFramesWithinTolerance(t *testing.T) {
	entries, err := readRecording("testdata/session")
	if err != nil {
		t.Fatal(err)
	}
	r := newRenderer(image.Pt(800, 450), entries[0].Cols, entries[0].Rows)
	for _, i := range []int{0, len(entries) / 2, len(entries) - 1} {
		got, err := r.draw(entries[i])
		if err != nil {
			t.Fatal(err)
		}
		golden := fmt.Sprintf("testdata/golden-%d.png", i)
		if os.Getenv("MOVIE_UPDATE_GOLDEN") == "1" {
			os.WriteFile(golden, got, 0o644)
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		// font rasterizing differs by a grey level or so between arm64 and amd64
		if n := differingPixels(t, got, want, 2); n > 400 {
			t.Errorf("entry %d: %d pixels differ from %s", i, n, golden)
		}
	}
	if len(r.missing) != 0 {
		t.Errorf("glyphs missing from the font chain: %q", r.missing)
	}
}

func differingPixels(t *testing.T, a, b []byte, tolerance int) int {
	t.Helper()
	ia, err1 := png.Decode(bytes.NewReader(a))
	ib, err2 := png.Decode(bytes.NewReader(b))
	if err1 != nil || err2 != nil || ia.Bounds() != ib.Bounds() {
		t.Fatalf("undecodable or differently sized: %v %v", err1, err2)
	}
	n := 0
	for y := ia.Bounds().Min.Y; y < ia.Bounds().Max.Y; y++ {
		for x := ia.Bounds().Min.X; x < ia.Bounds().Max.X; x++ {
			r1, g1, b1, _ := ia.At(x, y).RGBA()
			r2, g2, b2, _ := ib.At(x, y).RGBA()
			for _, d := range []int{int(r1>>8) - int(r2>>8), int(g1>>8) - int(g2>>8), int(b1>>8) - int(b2>>8)} {
				if d > tolerance || d < -tolerance {
					n++
					break
				}
			}
		}
	}
	return n
}

func TestMissingGlyphsAreReportedNotHidden(t *testing.T) {
	r := newRenderer(image.Pt(400, 100), 10, 2)
	if _, err := r.draw(Entry{Cols: 10, Rows: 2, Screen: "a 🦀 b\n"}); err != nil {
		t.Fatal(err)
	}
	if len(r.missing) != 1 || r.missing[0] != '🦀' {
		t.Fatalf("missing %q", r.missing)
	}
}
