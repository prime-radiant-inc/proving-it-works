package build

import "testing"

func TestNarrationDelay(t *testing.T) {
	for _, c := range []struct {
		scene, speech float64
		atEnd         bool
		want          float64
	}{
		{10, 4, false, 0},
		{10, 4, true, 6},
		{4, 4, true, 0}, // narration longer than the visuals sets the length: no delay
		{3, 5, true, 0},
	} {
		if got := narrationDelay(c.scene, c.speech, c.atEnd); got != c.want {
			t.Errorf("narrationDelay(%v, %v, %v) = %v, want %v", c.scene, c.speech, c.atEnd, got, c.want)
		}
	}
}
