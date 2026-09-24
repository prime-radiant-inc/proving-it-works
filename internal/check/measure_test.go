package check

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestMovedFractionCountsPixelsBeyondTheDelta(t *testing.T) {
	a := []uint8{0, 0, 0, 0}
	b := []uint8{8, 9, 0, 200}
	if got := movedFraction(a, b); got != 0.5 {
		t.Fatal(got)
	}
}

func TestLevelsAreOneSecondRMSInDBFS(t *testing.T) {
	pcm := make([]byte, 2*8000*2) // two seconds at 8 kHz
	for i := range 8000 {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(16384))) // half scale
	}
	got := levels(pcm)
	if len(got) != 2 || math.Abs(got[0]-(-6.02)) > 0.01 || got[1] != -120 {
		t.Fatal(got)
	}
}

func TestGridColumnsFillTheSheetExactly(t *testing.T) {
	for n, want := range map[int]int{12: 4, 9: 3, 5: 5, 2: 2, 7: 4, 1: 1} {
		if got := gridColumns(n); got != want {
			t.Errorf("gridColumns(%d) = %d, want %d", n, got, want)
		}
	}
}
