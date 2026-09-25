package term

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/prime-radiant-inc/proving-it-works/internal/film"
	"github.com/prime-radiant-inc/proving-it-works/internal/fonts"
)

// Shots gives the film package each recorded snapshot, picture identified
// by its screen text.
func Shots(entries []Entry) []film.Shot {
	shots := make([]film.Shot, len(entries))
	for i, e := range entries {
		shots[i] = film.Shot{T: e.T, Film: e.Film, End: e.End, Waiting: e.Waiting, Look: e.Screen, Index: i}
	}
	return shots
}

func readRecording(dir string) ([]Entry, error) {
	f, err := os.Open(filepath.Join(dir, "recording.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(lines))
	for i, line := range lines {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			if i == len(lines)-1 {
				break // a recorder killed mid-write leaves a partial last line
			}
			return nil, fmt.Errorf("recording.jsonl line %d: %w", i+1, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Render draws every take of the recording in dir into outdir/take-N/ and
// writes outdir/scenes.yaml.
func Render(dir, outdir string, px image.Point, stdout io.Writer) error {
	entries, err := readRecording(dir)
	if err != nil {
		return err
	}
	beats, err := film.ReadBeats(dir)
	if err != nil {
		return err
	}
	m := film.Movie{Tool: "movie term stop", Size: px}
	if s, err := Load(dir); err == nil {
		m.Title, m.Subtitle = s.Title, s.Subtitle
	}
	var r *renderer
	if len(entries) > 0 {
		r = newRenderer(px, entries[0].Cols, entries[0].Rows)
	}
	draw := func(shot film.Shot) ([]byte, error) { return r.draw(entries[shot.Index]) }
	if _, err := film.Write(outdir, m, film.Split(Shots(entries)), beats, draw, stdout); err != nil {
		return err
	}
	r.warnMissing(stdout)
	if end := entries[len(entries)-1]; end.End && end.Reason != "" {
		fmt.Fprintf(stdout, "WARN       the recording ended without a stop: %s\n", end.Reason)
	}
	return nil
}

// renderer draws snapshots cell by cell, cols x rows filling 96% of the frame.
type renderer struct {
	px                   image.Point
	chain                []*opentype.Font
	faces                []font.Face
	cols, rows           int
	cellW, cellH, ascent int
	buf                  sfnt.Buffer
	missing              []rune
	seen                 map[rune]bool
}

func newRenderer(px image.Point, cols, rows int) *renderer {
	chain := fonts.MonoChain()
	probe := fonts.Face(chain[0], 100)
	adv, _ := probe.GlyphAdvance('M')
	height := probe.Metrics().Height
	size := math.Min(
		float64(px.X)*0.96/(float64(cols)*float64(adv)/64/100),
		float64(px.Y)*0.96/(float64(rows)*float64(height)/64/100))
	r := &renderer{px: px, chain: chain, cols: cols, rows: rows, seen: map[rune]bool{}}
	for _, f := range chain {
		r.faces = append(r.faces, fonts.Face(f, size))
	}
	adv, _ = r.faces[0].GlyphAdvance('M')
	m := r.faces[0].Metrics()
	r.cellW, r.cellH, r.ascent = adv.Ceil(), m.Height.Ceil(), m.Ascent.Ceil()
	return r
}

func (r *renderer) draw(e Entry) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, r.px.X, r.px.Y))
	draw.Draw(img, img.Bounds(), image.NewUniform(defaultBG), image.Point{}, draw.Src)
	ox, oy := (r.px.X-r.cols*r.cellW)/2, (r.px.Y-r.rows*r.cellH)/2
	lines := strings.Split(e.Screen, "\n")
	for y := 0; y < r.rows; y++ {
		line := ""
		if y < len(lines) {
			line = lines[y]
		}
		for x, c := range ParseLine(line, r.cols) {
			if c.Skip {
				continue
			}
			w := r.cellW
			if c.Wide {
				w *= 2
			}
			if e.Cursor && x == e.CursorX && y == e.CursorY {
				c.Reverse = !c.Reverse
			}
			fg, bg := c.Colours()
			cell := image.Rect(ox+x*r.cellW, oy+y*r.cellH, ox+x*r.cellW+w, oy+(y+1)*r.cellH)
			draw.Draw(img, cell, image.NewUniform(bg), image.Point{}, draw.Src)
			if c.Underline {
				under := image.Rect(cell.Min.X, cell.Max.Y-2, cell.Max.X, cell.Max.Y-1)
				draw.Draw(img, under, image.NewUniform(fg), image.Point{}, draw.Src)
			}
			if c.R == ' ' {
				continue
			}
			i := fonts.Find(r.chain, c.R, &r.buf)
			if i < 0 {
				// a visible box, never a silent blank
				r.note(c.R)
				box := cell.Inset(max(1, r.cellW/6))
				for _, edge := range []image.Rectangle{
					{box.Min, image.Pt(box.Max.X, box.Min.Y+1)}, {image.Pt(box.Min.X, box.Max.Y-1), box.Max},
					{box.Min, image.Pt(box.Min.X+1, box.Max.Y)}, {image.Pt(box.Max.X-1, box.Min.Y), box.Max},
				} {
					draw.Draw(img, edge, image.NewUniform(fg), image.Point{}, draw.Src)
				}
				continue
			}
			face := r.faces[i]
			dot := fixed.P(cell.Min.X, cell.Min.Y+r.ascent)
			if i > 0 {
				// Fallback glyphs (notably Noto's ⏺) can be wider than a
				// cell; centre them horizontally instead of letting them
				// overflow into the next cell.
				adv, _ := face.GlyphAdvance(c.R)
				dot.X += (fixed.I(w) - adv) / 2
			}
			d := font.Drawer{Dst: img, Src: image.NewUniform(fg), Face: face, Dot: dot}
			d.DrawString(string(c.R))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *renderer) note(ch rune) {
	if !r.seen[ch] {
		r.seen[ch] = true
		r.missing = append(r.missing, ch)
	}
}

// warnMissing says which characters no font here can draw, so a box in a
// frame is never a silent defect.
func (r *renderer) warnMissing(stdout io.Writer) {
	if len(r.missing) > 0 {
		fmt.Fprintf(stdout, "WARN       no font here can draw %s; those cells show a box. "+
			"Look at the frames before you use them.\n", fonts.Describe(r.missing))
	}
}
