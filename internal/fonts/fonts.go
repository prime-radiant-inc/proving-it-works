// Package fonts embeds the fonts movie draws with, so frames and subtitles
// never depend on what a machine has installed.
package fonts

import (
	_ "embed"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

//go:embed DejaVuSans.ttf
var sansTTF []byte

//go:embed DejaVuSansMono.ttf
var monoTTF []byte

var (
	sans = mustParse(sansTTF)
	mono = mustParse(monoTTF)
)

func mustParse(data []byte) *opentype.Font {
	f, err := opentype.Parse(data)
	if err != nil {
		panic(err)
	}
	return f
}

// Sans is DejaVu Sans, for cards and burned subtitles.
func Sans() *opentype.Font { return sans }

// Mono is DejaVu Sans Mono, for terminals.
func Mono() *opentype.Font { return mono }

// SansTTF is the Sans font file, for handing to libass.
func SansTTF() []byte { return sansTTF }

// Face returns f at size pixels.
func Face(f *opentype.Font, size float64) font.Face {
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	return face
}

// Find returns the index of the first font in chain that can draw r, or -1.
func Find(chain []*opentype.Font, r rune, buf *sfnt.Buffer) int {
	for i, f := range chain {
		if g, err := f.GlyphIndex(buf, r); err == nil && g != 0 {
			return i
		}
	}
	return -1
}

// Missing lists, once each and in order, the characters of text that no
// font in chain can draw. Whitespace is never missing.
func Missing(chain []*opentype.Font, text string) []rune {
	var buf sfnt.Buffer
	seen := map[rune]bool{}
	var missing []rune
	for _, r := range text {
		if unicode.IsSpace(r) || seen[r] {
			continue
		}
		seen[r] = true
		if Find(chain, r, &buf) < 0 {
			missing = append(missing, r)
		}
	}
	return missing
}

// Describe names characters with their code points: "漢 (U+6F22)".
func Describe(runes []rune) string {
	parts := make([]string, len(runes))
	for i, r := range runes {
		parts[i] = fmt.Sprintf("%c (U+%04X)", r, r)
	}
	return strings.Join(parts, ", ")
}
