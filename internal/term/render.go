package term

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
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

	"github.com/prime-radiant-inc/proving-it-works/internal/fonts"
)

// FPS is the frame rate of rendered takes.
const FPS = 10

// Take is one stretch of recording filmed without a break.
type Take struct {
	Start, End float64
	Entries    []Entry
}

// Takes splits a recording into takes. Filming starts at an entry with Film
// set and stops at the next entry without it, or at the end marker. A
// recording that stops without an end marker holds its last snapshot for one
// second.
func Takes(entries []Entry) []Take {
	var takes []Take
	var cur *Take
	for _, e := range entries {
		filming := e.Film && !e.End
		switch {
		case filming && cur == nil:
			cur = &Take{Start: e.T, Entries: []Entry{e}}
		case filming:
			cur.Entries = append(cur.Entries, e)
		case cur != nil:
			cur.End = e.T
			takes = append(takes, *cur)
			cur = nil
		}
	}
	if cur != nil {
		cur.End = cur.Entries[len(cur.Entries)-1].T + 1
		takes = append(takes, *cur)
	}
	return takes
}

// Tighten caps each stretch the shell spends waiting at a prompt for the
// next command at maxWait seconds, counting consecutive waiting snapshots of
// the same screen as one stretch. That time belongs to the agent deciding
// what to type, not to the program; time a command spends running is never
// shortened.
func Tighten(t Take, maxWait float64) Take {
	out := Take{Start: t.Start, Entries: make([]Entry, len(t.Entries))}
	shift, waited := 0.0, 0.0
	for i, e := range t.Entries {
		next := t.End
		if i+1 < len(t.Entries) {
			next = t.Entries[i+1].T
		}
		span := next - e.T
		if !e.Waiting || i == 0 || !t.Entries[i-1].Waiting || t.Entries[i-1].Screen != e.Screen {
			waited = 0
		}
		keep := span
		if e.Waiting {
			keep = math.Max(0, math.Min(span, maxWait-waited))
			waited += keep
		}
		e.T -= shift
		out.Entries[i] = e
		shift += span - keep
	}
	out.End = t.End - shift
	return out
}

// Settled is how many seconds into a take its screen last changed: where
// the take's result has appeared, or 0 if the screen never changes.
func Settled(t Take) float64 {
	settled := 0.0
	for i := 1; i < len(t.Entries); i++ {
		if t.Entries[i].Screen != t.Entries[i-1].Screen {
			settled = t.Entries[i].T - t.Start
		}
	}
	return settled
}

// Slots returns, for each frame of a take at fps, the index of the entry on
// screen at that moment.
func Slots(t Take, fps float64) []int {
	n := int(math.Ceil((t.End-t.Start)*fps - 1e-9))
	slots := make([]int, n)
	j := 0
	for k := range n {
		at := t.Start + float64(k)/fps
		for j+1 < len(t.Entries) && t.Entries[j+1].T <= at {
			j++
		}
		slots[k] = j
	}
	return slots
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

// Render draws every take of the recording in dir into outdir/take-N/.
func Render(dir, outdir string, px image.Point, stdout io.Writer) error {
	if err := requireEmptyDir(outdir); err != nil {
		return err
	}
	entries, err := readRecording(dir)
	if err != nil {
		return err
	}
	takes := Takes(entries)
	if len(takes) == 0 {
		return errors.New("nothing was filmed")
	}
	first := takes[0].Entries[0]
	r := newRenderer(px, first.Cols, first.Rows)
	var scenes strings.Builder
	for i, t := range takes {
		t = Tighten(t, hold)
		name := fmt.Sprintf("take-%d", i+1)
		takeDir, err := filepath.Abs(filepath.Join(outdir, name))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(takeDir, 0o755); err != nil {
			return err
		}
		prev, frame := -1, []byte(nil)
		slots := Slots(t, FPS)
		for k, idx := range slots {
			if idx != prev {
				if frame, err = r.draw(t.Entries[idx]); err != nil {
					return err
				}
				prev = idx
			}
			if err := os.WriteFile(filepath.Join(takeDir, fmt.Sprintf("f%05d.png", k)), frame, 0o644); err != nil {
				return err
			}
		}
		meta, _ := json.MarshalIndent(map[string]any{"frames": takeDir, "rate": FPS,
			"settled": math.Round(Settled(t)*1000) / 1000}, "", "  ")
		if err := os.WriteFile(filepath.Join(takeDir, "take.json"), meta, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: %d frames, %.1fs -> %s\n", name, len(slots), t.End-t.Start, takeDir)
		fmt.Fprintf(&scenes, "  - id: %s\n    frames: %s\n    narration_at: end\n", name, takeDir)
	}
	fmt.Fprintf(stdout, "\nScenes for your scene file: add a narration to each. Each take ends on its result, "+
		"so narration_at: end makes the narration finish there; each takes its rate from take.json.\n%s", scenes.String())
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
