package film

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prime-radiant-inc/proving-it-works/internal/jsonl"
)

func openSet(t *testing.T) Set {
	t.Helper()
	s := Set{Dir: t.TempDir()}
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	if err := s.Roll(); err != nil {
		t.Fatal(err)
	}
	return s
}

func marks(t *testing.T, s Set) []Mark {
	t.Helper()
	m, err := jsonl.Read[Mark](filepath.Join(s.Dir, "marks.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func ok() (string, error) { return "did it", nil }

func TestActMarksTheAppBusyUntilItSettles(t *testing.T) {
	s := openSet(t)
	settled := false
	did, code, err := s.Act("", ok, func() { settled = true })
	if err != nil || code != 0 || did != "did it" || !settled {
		t.Fatalf("got %q %d %v settled=%v", did, code, err, settled)
	}
	m := marks(t, s)
	if n := len(m); n != 4 || !m[2].Busy || m[3].Busy || !m[3].Film {
		t.Fatalf("marks %+v", m)
	}
}

func TestAFailedActionCountsAsWaiting(t *testing.T) {
	s := openSet(t)
	_, code, err := s.Act("", func() (string, error) { return "", Failed{"not there"} }, func() { t.Fatal("settled a failed action") })
	if code != 1 || err == nil || err.Error() != "not there" {
		t.Fatalf("got %d %v", code, err)
	}
	m := marks(t, s)
	busy, idle := m[len(m)-2], m[len(m)-1]
	if !busy.Busy || idle.Busy || busy.T != idle.T {
		t.Fatalf("the failure should unmark its own busy time: %+v", m)
	}
	shots := Shots([]Frame{{T: m[0].T, File: "a.png"}}, m)
	if last := shots[len(shots)-1]; !last.Waiting {
		t.Fatalf("the failed action's time is not waiting: %+v", shots)
	}
}

func TestAnErrorThatIsNotAVerdictExits2(t *testing.T) {
	s := openSet(t)
	_, code, err := s.Act("", func() (string, error) { return "", errors.New("broken") }, func() {})
	if code != 2 || err == nil {
		t.Fatalf("got %d %v", code, err)
	}
}

func TestSayEndsABeatAndTheNextActionCuts(t *testing.T) {
	s := openSet(t)
	if _, _, err := s.Act("One.", ok, func() {}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Act("", ok, func() {}); err != nil {
		t.Fatal(err)
	}
	beats, _ := ReadBeats(s.Dir)
	if !slices.Equal(beats, []Beat{{Take: 1, Say: "One."}}) {
		t.Fatalf("beats %v", beats)
	}
	if st, _ := s.state(); st.Take != 2 || st.CutPending || !st.Film {
		t.Fatalf("state %+v, want take 2 filming", st)
	}
}

func TestSayIsRefusedWhileFilmingIsOff(t *testing.T) {
	s := openSet(t)
	s.SetFilm(false)
	_, code, err := s.Act("Nothing shows this.", func() (string, error) { t.Fatal("acted"); return "", nil }, func() {})
	if code != 2 || err == nil || !strings.Contains(err.Error(), "filming is off") {
		t.Fatalf("got %d %v", code, err)
	}
}

func TestFilmOffThenOnSpendsAPendingCut(t *testing.T) {
	s := openSet(t)
	s.Act("One.", ok, func() {})
	s.SetFilm(false)
	s.Act("", ok, func() {}) // off camera: the pending cut must not turn filming on
	if st, _ := s.state(); st.Film {
		t.Fatal("a pending cut turned filming back on")
	}
	s.SetFilm(true)
	s.Act("", ok, func() {})
	if st, _ := s.state(); st.Take != 2 {
		t.Fatalf("take %d, want 2: film on starts the take, the spent cut starts none", st.Take)
	}
}

func TestReelKeepsOnlyChangedPictures(t *testing.T) {
	dir := t.TempDir()
	r, err := NewReel(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range []string{"a", "a", "b", "a"} {
		if err := r.Add(float64(i), []byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	r.End(9, "gone")
	frames, _ := jsonl.Read[Frame](filepath.Join(dir, "frames.jsonl"))
	if len(frames) != 4 || frames[1].T != 2 || frames[2].File != "000003.png" || !frames[3].End || frames[3].Reason != "gone" {
		t.Fatalf("frames %+v", frames)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "frames", "000002.png")); string(data) != "b" {
		t.Fatalf("frame 2 holds %q", data)
	}
}
