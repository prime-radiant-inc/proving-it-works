package term

import (
	"slices"
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
