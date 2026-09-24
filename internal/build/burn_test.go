package build

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/testmedia"
)

func TestBurnPutsSubtitlesIntoThePictureOrEmbedsATrack(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := filepath.Join(t.TempDir(), "awk %d [x] 'q'")
	scratch := filepath.Join(dir, "scratch")
	os.MkdirAll(scratch, 0o755)
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "color=c=navy:size=640x360:rate=10:d=3",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo", "-t", "3",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "cut.mp4")
	srtPath := filepath.Join(dir, "cut.srt")
	os.WriteFile(srtPath, []byte("1\n00:00:00,000 --> 00:00:03,000\nProving it works ✓\n"), 0o644)
	out := filepath.Join(dir, "out.mp4")
	var log bytes.Buffer
	burned, err := burn(scratch, filepath.Join(dir, "cut.mp4"), srtPath, out, &log)
	if err != nil {
		t.Fatal(err)
	}
	if !burned {
		info, err := ffmpeg.Probe(out)
		if err != nil || !info.Has("subtitle") || !bytes.Contains(log.Bytes(), []byte("no libass")) {
			t.Fatalf("soft fallback: %v %+v\n%s", err, info, log.String())
		}
		return
	}
	testmedia.FFmpeg(t, dir, "-ss", "1.5", "-i", "cut.mp4", "-frames:v", "1", "before.png")
	testmedia.FFmpeg(t, dir, "-ss", "1.5", "-i", "out.mp4", "-frames:v", "1", "after.png")
	if countDiffering(t, filepath.Join(dir, "before.png"), filepath.Join(dir, "after.png")) < 500 {
		t.Fatal("burning changed almost no pixels: the subtitles are not in the picture")
	}
}

func countDiffering(t *testing.T, a, b string) int {
	t.Helper()
	load := func(p string) []uint8 {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		var px []uint8
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				r, _, _, _ := img.At(x, y).RGBA()
				px = append(px, uint8(r>>8))
			}
		}
		return px
	}
	pa, pb := load(a), load(b)
	n := 0
	for i := range min(len(pa), len(pb)) {
		if d := int(pa[i]) - int(pb[i]); d > 40 || d < -40 {
			n++
		}
	}
	return n
}
