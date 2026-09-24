package check

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
)

const thumbWidth = 320 // the metric is a pixel fraction, so any width works

// samplePicture writes one frame per second into workdir/samples and returns
// their paths and, per second, the fraction of pixels that moved. ffmpeg runs
// from the samples directory with a relative pattern, so no user path is
// parsed as a pattern.
func samplePicture(movie, workdir string) ([]string, []float64, error) {
	dir := filepath.Join(workdir, "samples")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	old, _ := os.ReadDir(dir)
	for _, e := range old {
		if filepath.Ext(e.Name()) == ".png" {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	if err := ffmpeg.Run(dir, nil, "-i", movie, "-vf", fmt.Sprintf("fps=1,scale=%d:-1", thumbWidth),
		"-f", "image2", "s%05d.png"); err != nil {
		return nil, nil, fmt.Errorf("frame sampling failed: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var paths []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".png" {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, nil, errors.New("no video frames could be sampled")
	}
	var fracs []float64
	var prev []uint8
	for _, p := range paths {
		px, err := grey(p)
		if err != nil {
			return nil, nil, err
		}
		if prev != nil {
			fracs = append(fracs, movedFraction(prev, px))
		}
		prev = px
	}
	return paths, fracs, nil
}

func grey(path string) ([]uint8, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	g := image.NewGray(img.Bounds())
	draw.Draw(g, g.Bounds(), img, img.Bounds().Min, draw.Src)
	return g.Pix, nil
}

// movedFraction is the fraction of pixels whose grey level moved by more
// than pixelDelta.
func movedFraction(a, b []uint8) float64 {
	n := min(len(a), len(b))
	if n == 0 {
		return 0
	}
	moved := 0
	for i := range n {
		if d := int(a[i]) - int(b[i]); d > pixelDelta || d < -pixelDelta {
			moved++
		}
	}
	return float64(moved) / float64(n)
}

// sampleSound decodes the first audio stream to 8 kHz mono and measures it.
func sampleSound(movie string) ([]float64, error) {
	pcm, err := ffmpeg.Output("-i", movie, "-map", "0:a:0", "-ac", "1", "-ar", "8000", "-f", "s16le", "-")
	if err != nil {
		return nil, fmt.Errorf("audio decode failed: %w", err)
	}
	if len(pcm) == 0 {
		return nil, errors.New("audio decode failed: no samples")
	}
	return levels(pcm), nil
}

// levels is the per-second RMS, in dBFS, of 8 kHz mono s16le PCM.
func levels(pcm []byte) []float64 {
	n := len(pcm) / 2
	var out []float64
	for start := 0; start < n; start += 8000 {
		end := min(start+8000, n)
		var sum float64
		for i := start; i < end; i++ {
			s := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
			sum += s * s
		}
		rms := math.Sqrt(sum / float64(end-start))
		if rms > 0 {
			out = append(out, 20*math.Log10(rms/32768))
		} else {
			out = append(out, -120)
		}
	}
	return out
}
