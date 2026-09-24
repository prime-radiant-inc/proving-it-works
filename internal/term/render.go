package term

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/image/font"
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
	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		var e Entry
		if json.Unmarshal(scanner.Bytes(), &e) != nil {
			break // a recorder killed mid-write leaves a partial last line
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
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
	for i, t := range takes {
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
		meta, _ := json.MarshalIndent(map[string]any{"id": name, "frames": takeDir, "rate": FPS,
			"seconds": math.Round((t.End-t.Start)*10) / 10}, "", "  ")
		if err := os.WriteFile(filepath.Join(takeDir, "take.json"), meta, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: %d frames, %.1fs -> %s\n", name, len(slots), t.End-t.Start, takeDir)
	}
	return nil
}

var (
	foreground = color.RGBA{0xe8, 0xe6, 0xe1, 0xff}
	backdrop   = color.RGBA{0x10, 0x10, 0x14, 0xff}
	sgr        = regexp.MustCompile("\x1b\\[[0-9;:]*m")
)

// renderer draws snapshots as cols x rows cells filling 96% of the frame.
type renderer struct {
	px                   image.Point
	face                 font.Face
	cols, rows           int
	cellW, cellH, ascent int
}

func newRenderer(px image.Point, cols, rows int) *renderer {
	probe := fonts.Face(fonts.Mono(), 100)
	adv, _ := probe.GlyphAdvance('M')
	height := probe.Metrics().Height
	size := math.Min(
		float64(px.X)*0.96/(float64(cols)*float64(adv)/64/100),
		float64(px.Y)*0.96/(float64(rows)*float64(height)/64/100))
	face := fonts.Face(fonts.Mono(), size)
	adv, _ = face.GlyphAdvance('M')
	m := face.Metrics()
	return &renderer{px: px, face: face, cols: cols, rows: rows,
		cellW: adv.Ceil(), cellH: m.Height.Ceil(), ascent: m.Ascent.Ceil()}
}

// draw renders one snapshot as plain text. Task 17 replaces this with cell
// rendering that honours colours, wide characters, and the cursor.
func (r *renderer) draw(e Entry) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, r.px.X, r.px.Y))
	draw.Draw(img, img.Bounds(), image.NewUniform(backdrop), image.Point{}, draw.Src)
	ox, oy := (r.px.X-r.cols*r.cellW)/2, (r.px.Y-r.rows*r.cellH)/2
	d := font.Drawer{Dst: img, Src: image.NewUniform(foreground), Face: r.face}
	for y, line := range strings.Split(sgr.ReplaceAllString(e.Screen, ""), "\n") {
		if y >= r.rows {
			break
		}
		d.Dot = fixed.P(ox, oy+y*r.cellH+r.ascent)
		d.DrawString(line)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
