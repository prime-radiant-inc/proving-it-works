package term

import (
	"image/color"
	"strings"
	"testing"
)

func TestParseLineAppliesSGR(t *testing.T) {
	cells := ParseLine("\x1b[31mred\x1b[39m \x1b[1;38;5;214mB\x1b[0m \x1b[48;2;1;2;3mx\x1b[m", 12)
	if len(cells) != 12 {
		t.Fatalf("got %d cells", len(cells))
	}
	if cells[0].R != 'r' || cells[0].FG != palette(1) {
		t.Errorf("red: %+v", cells[0])
	}
	if cells[3].FG != defaultFG {
		t.Errorf("reset fg: %+v", cells[3])
	}
	if cells[4].R != 'B' || !cells[4].Bold || cells[4].FG != palette(214) {
		t.Errorf("256-colour bold: %+v", cells[4])
	}
	if cells[6].BG != (color.RGBA{1, 2, 3, 255}) {
		t.Errorf("truecolour bg: %+v", cells[6])
	}
	if cells[11].R != ' ' || cells[11].BG != defaultBG {
		t.Errorf("padding: %+v", cells[11])
	}
}

func TestWideCharactersTakeTwoCells(t *testing.T) {
	cells := ParseLine("a漢b", 5)
	if !cells[1].Wide || !cells[2].Skip || cells[3].R != 'b' {
		t.Fatalf("%+v", cells[:4])
	}
}

func TestReverseSwapsColours(t *testing.T) {
	c := ParseLine("\x1b[7mx", 1)[0]
	fg, bg := c.Colours()
	if fg != defaultBG || bg != defaultFG {
		t.Fatalf("%v %v", fg, bg)
	}
}

func TestBoldBrightensTheFirstEightColours(t *testing.T) {
	c := ParseLine("\x1b[1;32mx", 1)[0]
	if fg, _ := c.Colours(); fg != palette(10) {
		t.Fatalf("%v", fg)
	}
}

// Ruling 2: OSC sequences (ESC ] ... terminated by BEL or ESC \), notably
// OSC 8 hyperlinks, must be skipped whole; the URL they carry must never
// appear as visible characters.
func TestOSCHyperlinksAreSkipped(t *testing.T) {
	cells := ParseLine("\x1b]8;;http://example.com\x1b\\link\x1b]8;;\x1b\\ ok", 20)
	var got []rune
	for _, c := range cells {
		if !c.Skip {
			got = append(got, c.R)
		}
	}
	if s := strings.TrimRight(string(got), " "); s != "link ok" {
		t.Fatalf("got %q, want the hyperlink text but not its URL", s)
	}
}

func TestOSCTerminatedByBELIsSkipped(t *testing.T) {
	cells := ParseLine("\x1b]0;window title\x07ok", 5)
	if cells[0].R != 'o' || cells[1].R != 'k' {
		t.Fatalf("%+v", cells[:2])
	}
}

// Ruling 3: combining marks (and the zero-width joiner / variation
// selector-16) must not take a cell of their own.
func TestCombiningMarksDoNotTakeACell(t *testing.T) {
	cells := ParseLine("éb", 3)
	if cells[0].R != 'e' || cells[1].R != 'b' {
		t.Fatalf("%+v", cells[:2])
	}
}

func TestZeroWidthJoinerAndVariationSelectorDoNotTakeACell(t *testing.T) {
	cells := ParseLine("a‍️b", 3)
	if cells[0].R != 'a' || cells[1].R != 'b' {
		t.Fatalf("%+v", cells[:2])
	}
}

// Ruling 4: SGR 2 (dim) blends the foreground halfway toward the
// background; SGR 22 resets both bold and dim.
func TestDimBlendsForegroundHalfwayToBackground(t *testing.T) {
	c := ParseLine("\x1b[2mx", 1)[0]
	fg, _ := c.Colours()
	want := blend(defaultFG, defaultBG)
	if fg != want {
		t.Fatalf("got %v, want %v", fg, want)
	}
}

func TestSGR22ResetsBoldAndDim(t *testing.T) {
	c := ParseLine("\x1b[1;2;22mx", 1)[0]
	if c.Bold || c.Dim {
		t.Fatalf("bold or dim still set: %+v", c)
	}
	if fg, _ := c.Colours(); fg != defaultFG {
		t.Fatalf("got %v, want the plain foreground", fg)
	}
}
