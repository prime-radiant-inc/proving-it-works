package term

import (
	"image/color"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

var (
	defaultFG = color.RGBA{0xe8, 0xe6, 0xe1, 0xff}
	defaultBG = color.RGBA{0x10, 0x10, 0x14, 0xff}
)

// ansi16 is xterm's default 16-colour palette.
var ansi16 = [16]color.RGBA{
	{0, 0, 0, 255}, {205, 0, 0, 255}, {0, 205, 0, 255}, {205, 205, 0, 255},
	{0, 0, 238, 255}, {205, 0, 205, 255}, {0, 205, 205, 255}, {229, 229, 229, 255},
	{127, 127, 127, 255}, {255, 0, 0, 255}, {0, 255, 0, 255}, {255, 255, 0, 255},
	{92, 92, 255, 255}, {255, 0, 255, 255}, {0, 255, 255, 255}, {255, 255, 255, 255},
}

// palette is xterm's 256-colour palette: 16 named colours, a 6x6x6 cube,
// and 24 greys.
func palette(n int) color.RGBA {
	switch {
	case n < 16:
		return ansi16[n]
	case n < 232:
		n -= 16
		level := func(v int) uint8 {
			if v == 0 {
				return 0
			}
			return uint8(55 + 40*v)
		}
		return color.RGBA{level(n / 36), level(n / 6 % 6), level(n % 6), 255}
	default:
		g := uint8(8 + 10*(n-232))
		return color.RGBA{g, g, g, 255}
	}
}

// blend mixes a and b halfway, for drawing SGR 2 (dim).
func blend(a, b color.RGBA) color.RGBA {
	return color.RGBA{
		R: uint8((int(a.R) + int(b.R)) / 2),
		G: uint8((int(a.G) + int(b.G)) / 2),
		B: uint8((int(a.B) + int(b.B)) / 2),
		A: 255,
	}
}

// Cell is one character cell of a snapshot.
type Cell struct {
	R                             rune
	FG, BG                        color.RGBA
	fgIndex                       int // palette index of FG when set from the first 8, else -1
	Bold, Dim, Underline, Reverse bool
	Wide                          bool // takes this cell and the next
	Skip                          bool // the second half of a wide character
}

// Colours applies bold-brightening, dim, and reverse video.
func (c Cell) Colours() (color.RGBA, color.RGBA) {
	fg, bg := c.FG, c.BG
	if c.Bold && c.fgIndex >= 0 && c.fgIndex < 8 {
		fg = palette(c.fgIndex + 8)
	}
	if c.Dim {
		fg = blend(fg, bg)
	}
	if c.Reverse {
		fg, bg = bg, fg
	}
	return fg, bg
}

func isWide(r rune) bool {
	k := width.LookupRune(r).Kind()
	return k == width.EastAsianWide || k == width.EastAsianFullwidth
}

// isZeroWidthCombining reports whether r is a combining mark (Unicode Mn or
// Me) or one of the zero-width joiner/variation-selector runes that ride on
// the cell before them rather than taking a cell of their own. tmux keeps
// these in the base character's cell; dropping them here is simplest.
func isZeroWidthCombining(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200d || r == 0xfe0f
}

// ParseLine turns one line of `tmux capture-pane -e` output (text with SGR
// sequences) into exactly cols cells.
func ParseLine(line string, cols int) []Cell {
	cells := make([]Cell, 0, cols)
	pen := Cell{FG: defaultFG, BG: defaultBG, fgIndex: -1}
	for i := 0; i < len(line) && len(cells) < cols; {
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
			end := i + 2
			for end < len(line) && (line[end] < 0x40 || line[end] > 0x7e) {
				end++
			}
			if end < len(line) && line[end] == 'm' {
				applySGR(&pen, line[i+2:end])
			}
			i = end + 1
			continue
		}
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == ']' {
			// An OSC sequence (notably an OSC 8 hyperlink's URL), terminated
			// by ESC \ or BEL. Skip it whole so its payload never draws as
			// visible text.
			i = oscEnd(line, i+2)
			continue
		}
		r, size := rune(line[i]), 1
		if r >= 0x80 {
			r, size = decodeRune(line[i:])
		}
		i += size
		if isZeroWidthCombining(r) {
			continue
		}
		c := pen
		c.R = r
		if isWide(r) && len(cells)+1 < cols {
			c.Wide = true
			cells = append(cells, c)
			c.Skip, c.Wide, c.R = true, false, ' '
		}
		cells = append(cells, c)
	}
	blank := Cell{R: ' ', FG: defaultFG, BG: defaultBG, fgIndex: -1}
	for len(cells) < cols {
		cells = append(cells, blank)
	}
	return cells
}

// oscEnd returns the index just past an OSC sequence's terminator (ESC \ or
// BEL), starting the search at from. If the line ends first, it returns
// len(line).
func oscEnd(line string, from int) int {
	for i := from; i < len(line); i++ {
		switch {
		case line[i] == 0x07:
			return i + 1
		case line[i] == 0x1b && i+1 < len(line) && line[i+1] == '\\':
			return i + 2
		}
	}
	return len(line)
}

func decodeRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return ' ', 1
}

// applySGR updates the pen from one SGR parameter list ("1;38;5;214").
func applySGR(pen *Cell, params string) {
	if params == "" {
		params = "0"
	}
	p := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	n := make([]int, len(p))
	for i, s := range p {
		n[i], _ = strconv.Atoi(s)
	}
	for i := 0; i < len(n); i++ {
		switch v := n[i]; {
		case v == 0:
			*pen = Cell{FG: defaultFG, BG: defaultBG, fgIndex: -1}
		case v == 1:
			pen.Bold = true
		case v == 2:
			pen.Dim = true
		case v == 4:
			pen.Underline = true
		case v == 7:
			pen.Reverse = true
		case v == 22:
			pen.Bold, pen.Dim = false, false
		case v == 24:
			pen.Underline = false
		case v == 27:
			pen.Reverse = false
		case v >= 30 && v <= 37:
			pen.FG, pen.fgIndex = palette(v-30), v-30
		case v == 39:
			pen.FG, pen.fgIndex = defaultFG, -1
		case v >= 40 && v <= 47:
			pen.BG = palette(v - 40)
		case v == 49:
			pen.BG = defaultBG
		case v >= 90 && v <= 97:
			pen.FG, pen.fgIndex = palette(v-90+8), -1
		case v >= 100 && v <= 107:
			pen.BG = palette(v - 100 + 8)
		case (v == 38 || v == 48) && i+2 < len(n) && n[i+1] == 5:
			c := palette(n[i+2])
			if v == 38 {
				pen.FG, pen.fgIndex = c, -1
			} else {
				pen.BG = c
			}
			i += 2
		case (v == 38 || v == 48) && i+4 < len(n) && n[i+1] == 2:
			c := color.RGBA{uint8(n[i+2]), uint8(n[i+3]), uint8(n[i+4]), 255}
			if v == 38 {
				pen.FG, pen.fgIndex = c, -1
			} else {
				pen.BG = c
			}
			i += 4
		}
	}
}
