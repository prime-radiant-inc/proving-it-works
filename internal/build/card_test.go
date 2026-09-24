package build

import (
	"bytes"
	"image/png"
	"testing"

	"golang.org/x/image/font"
)

func TestLongUnbreakableTitleShrinksToFit(t *testing.T) {
	b := fitBlock("github.com/prime-radiant-inc/proving-it-works", 77, 1613)
	for _, line := range b.lines {
		if w := font.MeasureString(b.face, line).Ceil(); w > 1613 {
			t.Fatalf("line %q is %d px wide", line, w)
		}
	}
	if b.size >= 77 {
		t.Fatalf("size did not shrink: %v", b.size)
	}
}

func TestShortTitleKeepsItsSize(t *testing.T) {
	if b := fitBlock("proving it works", 77, 1613); b.size != 77 || len(b.lines) != 1 {
		t.Fatalf("%+v", b)
	}
}

func TestCardDrawsTextOnTheBackground(t *testing.T) {
	data, err := Card("proving-it-works", "a subtitle", 640, 360)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 640 || img.Bounds().Dy() != 360 {
		t.Fatalf("size %v", img.Bounds())
	}
	lit := 0
	for y := 120; y < 240; y++ {
		for x := 0; x < 640; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r>>8 > 0x60 {
				lit++
			}
		}
	}
	if lit < 200 {
		t.Fatalf("only %d text pixels in the middle band", lit)
	}
}
