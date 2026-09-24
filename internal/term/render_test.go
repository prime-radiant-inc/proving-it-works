package term

import (
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
