package browse

import (
	"slices"
	"testing"

	"github.com/prime-radiant-inc/proving-it-works/internal/film"
)

func TestShotsMergeFramesAndMarksByTime(t *testing.T) {
	frames := []Frame{
		{T: 1, File: "000001.png"}, // about:blank, before filming starts
		{T: 3, File: "000002.png"}, // the app, loaded
		{T: 6, File: "000003.png"}, // a click's result
		{T: 9, End: true},
	}
	marks := []Mark{
		{T: 0, Busy: true},             // start loads the page
		{T: 4, Film: true},             // loaded: filming starts
		{T: 5, Film: true, Busy: true}, // click
		{T: 7, Film: true},             // settled
		{T: 8, End: true},              // stop
	}
	got := Shots(frames, marks)
	want := []film.Shot{
		{T: 1, Look: "000001.png", Index: 0},
		{T: 3, Look: "000002.png", Index: 1},
		{T: 4, Film: true, Waiting: true, Look: "000002.png", Index: 1},
		{T: 5, Film: true, Look: "000002.png", Index: 1},
		{T: 6, Film: true, Look: "000003.png", Index: 2},
		{T: 7, Film: true, Waiting: true, Look: "000003.png", Index: 2},
		{T: 8, End: true, Waiting: true, Look: "000003.png", Index: 2},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%+v\nwant\n%+v", got, want)
	}
}

func TestShotsWaitForTheFirstFrame(t *testing.T) {
	got := Shots([]Frame{{T: 2, File: "a.png"}}, []Mark{{T: 1, Film: true}})
	if len(got) != 1 || got[0].T != 2 || !got[0].Film {
		t.Fatalf("got %+v", got)
	}
}

func TestARecorderThatEndedOnItsOwnEndsTheTimeline(t *testing.T) {
	got := Shots([]Frame{{T: 1, File: "a.png"}, {T: 3, End: true, Reason: "the browser closed"}}, []Mark{{T: 0, Film: true}})
	if last := got[len(got)-1]; !last.End || last.T != 3 {
		t.Fatalf("got %+v", got)
	}
	if takes := film.Split(got); len(takes) != 1 || takes[0].End != 3 {
		t.Fatalf("takes %+v", takes)
	}
}
