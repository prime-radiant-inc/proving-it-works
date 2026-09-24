package build

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/prime-radiant-inc/proving-it-works/internal/fonts"
)

var (
	cardBackground = color.RGBA{0x10, 0x10, 0x14, 0xff}
	cardTitle      = color.RGBA{0xf2, 0xf2, 0xf5, 0xff}
	cardSubtitle   = color.RGBA{0x9a, 0x9a, 0xa6, 0xff}
)

// cardBlock is the title or subtitle, wrapped and sized to fit.
type cardBlock struct {
	lines []string
	size  float64
	face  font.Face
}

// fitBlock wraps text at spaces into lines no wider than width, shrinking
// the size until every line fits: a long URL cannot be wrapped, only shrunk.
func fitBlock(text string, size float64, width int) cardBlock {
	for {
		face := fonts.Face(fonts.Sans(), size)
		lines := wrapToWidth(face, text, width)
		widest := 0
		for _, l := range lines {
			widest = max(widest, font.MeasureString(face, l).Ceil())
		}
		if widest <= width || size <= 8 {
			return cardBlock{lines: lines, size: size, face: face}
		}
		size *= 0.9
	}
}

func wrapToWidth(face font.Face, text string, width int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(text) {
		try := strings.TrimSpace(cur + " " + w)
		if cur != "" && font.MeasureString(face, try).Ceil() > width {
			lines = append(lines, cur)
			cur = w
		} else {
			cur = try
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// Card draws a title card: title and subtitle centered on the dark
// background. Drawing it here means build needs no browser.
func Card(title, subtitle string, w, h int) ([]byte, error) {
	width := int(float64(w) * 0.84)
	blocks := []struct {
		cardBlock
		colour color.Color
	}{
		{fitBlock(title, float64(max(28, h/14)), width), cardTitle},
		{fitBlock(subtitle, float64(max(16, h/32)), width), cardSubtitle},
	}
	gap := max(16, h/44)
	total, used := 0, 0
	for _, b := range blocks {
		if len(b.lines) > 0 {
			total += len(b.lines) * b.face.Metrics().Height.Ceil()
			used++
		}
	}
	total += gap * max(used-1, 0)

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(cardBackground), image.Point{}, draw.Src)
	y := (h - total) / 2
	for _, b := range blocks {
		if len(b.lines) == 0 {
			continue
		}
		m := b.face.Metrics()
		for _, line := range b.lines {
			lw := font.MeasureString(b.face, line).Ceil()
			d := font.Drawer{Dst: img, Src: image.NewUniform(b.colour), Face: b.face,
				Dot: fixed.P((w-lw)/2, y+m.Ascent.Ceil())}
			d.DrawString(line)
			y += m.Height.Ceil()
		}
		y += gap
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
