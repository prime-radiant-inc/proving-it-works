package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prime-radiant-inc/proving-it-works/internal/testmedia"
)

func TestCheckRejectsAMovieShorterThanASecond(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=0.5",
		"-pix_fmt", "yuv420p", "short.mp4")
	r := runMovie(t, dir, "check", "short.mp4", "--no-expect-audio")
	if r.code != 1 || !strings.Contains(r.stdout, "not a movie") || !strings.Contains(r.stdout, "NOT SHIPPABLE") {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	var report struct{ Failures []string }
	data, err := os.ReadFile(filepath.Join(dir, "short-check", "check.json"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(data, &report); len(report.Failures) != 1 {
		t.Fatalf("check.json failures: %q", report.Failures)
	}
}

func TestCheckMissingMovieExits2(t *testing.T) {
	r := runMovie(t, t.TempDir(), "check", "nope.mp4")
	if r.code != 2 || !strings.Contains(r.stderr, "no such movie") {
		t.Fatalf("code %d stderr %q", r.code, r.stderr)
	}
}
