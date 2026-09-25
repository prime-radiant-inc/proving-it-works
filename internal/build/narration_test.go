package build

import "testing"

func TestNarrationPlacement(t *testing.T) {
	for _, c := range []struct {
		name                    string
		visual, speech, settled float64
		atEnd                   bool
		wantDelay, wantLength   float64
	}{
		{"starts with the scene", 10, 4, 0, false, 0, 10},
		{"narration longer than the visuals sets the length", 3, 5, 0, false, 0, 5},
		{"ends as the scene ends", 10, 4, 0, true, 6, 10},
		{"never before the result settles", 6, 4, 4.5, true, 4.5, 8.5},
		{"a result that settles early does not move it", 10, 4, 2, true, 6, 10},
		{"narration longer than the visuals", 4, 5, 0, true, 0, 5},
	} {
		delay, length := narrationPlacement(c.visual, c.speech, c.settled, c.atEnd)
		if delay != c.wantDelay || length != c.wantLength {
			t.Errorf("%s: got delay %v length %v, want %v %v", c.name, delay, length, c.wantDelay, c.wantLength)
		}
	}
}
