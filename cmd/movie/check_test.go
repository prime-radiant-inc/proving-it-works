package main

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"slices"
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
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	// a 0.5s movie also never reaches a new state, so "not a movie" only needs
	// to be among the failures, not the only one.
	if !slices.ContainsFunc(report.Failures, func(s string) bool { return strings.Contains(s, "not a movie") }) {
		t.Fatalf("check.json failures: %q", report.Failures)
	}
}

func TestCheckMissingMovieExits2(t *testing.T) {
	r := runMovie(t, t.TempDir(), "check", "nope.mp4")
	if r.code != 2 || !strings.Contains(r.stderr, "no such movie") {
		t.Fatalf("code %d stderr %q", r.code, r.stderr)
	}
}

// checkerMovies makes the movies the checker must judge, as today's suite did.
func checkerMovies(t *testing.T, dir string) {
	t.Helper()
	enc := []string{"-c:v", "libx264", "-pix_fmt", "yuv420p"}
	testmedia.FFmpeg(t, dir, append(append([]string{
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=2",
		"-f", "lavfi", "-i", "color=c=navy:size=320x240:rate=10:d=20",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=22",
		"-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0[v]", "-map", "[v]", "-map", "2:a"},
		enc...), "-c:a", "aac", "-shortest", "front-loaded.mp4")...)
	testmedia.FFmpeg(t, dir, append(append([]string{
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=22",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=22"}, enc...), "-c:a", "aac", "-shortest", "paced.mp4")...)
	testmedia.FFmpeg(t, dir, append(append([]string{
		"-f", "lavfi", "-i", "color=c=navy:size=320x240:rate=10:d=12",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=12"}, enc...), "-c:a", "aac", "-shortest", "still.mp4")...)
	testmedia.FFmpeg(t, dir, append([]string{
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=12"}, append(enc, "silent.mp4")...)...)
	subs := "1\n00:00:00,000 --> 00:00:07,000\nA narrated movie needs subtitles.\n\n" +
		"2\n00:00:07,000 --> 00:00:14,000\nThe checker treats their absence as a defect.\n\n" +
		"3\n00:00:14,000 --> 00:00:21,500\nAnd it notices when they stop early.\n"
	writeFile(t, dir, "paced.srt", subs)
	for _, name := range []string{"short", "nosubs", "embedded"} {
		copyFile(t, filepath.Join(dir, "paced.mp4"), filepath.Join(dir, name+".mp4"), 0o644)
	}
	writeFile(t, dir, "short.srt", strings.Join(strings.Split(subs, "\n")[:8], "\n")+"\n")
	testmedia.FFmpeg(t, dir, "-i", "paced.mp4", "-i", "paced.srt", "-map", "0:v:0", "-map", "0:a:0",
		"-map", "1:s:0", "-c", "copy", "-c:s", "mov_text", "embedded-track.mp4")
}

func TestCheckVerdictsOnRealMovies(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	checkerMovies(t, dir)
	for _, c := range []struct {
		movie  string
		args   []string
		code   int
		needle string
	}{
		{"front-loaded.mp4", nil, 1, "every visible change happens in the first"},
		{"paced.mp4", nil, 0, "Mechanical checks pass"},
		{"nosubs.mp4", nil, 1, "no subtitles"},
		{"nosubs.mp4", []string{"--no-expect-subtitles"}, 0, "Mechanical checks pass"},
		{"short.mp4", nil, 1, "subtitles stop at"},
		{"embedded-track.mp4", nil, 0, "subtitles  embedded"},
		{"still.mp4", nil, 1, "never reaches a new state"},
		{"silent.mp4", nil, 1, "no audio stream"},
		{"silent.mp4", []string{"--no-expect-audio"}, 0, "Mechanical checks pass"},
	} {
		r := runMovie(t, dir, append([]string{"check", c.movie}, c.args...)...)
		if r.code != c.code || !strings.Contains(r.stdout, c.needle) {
			t.Errorf("check %s %v: code %d, want %d with %q\n%s%s", c.movie, c.args, r.code, c.code, c.needle, r.stdout, r.stderr)
		}
	}
}

func TestCheckWritesAContactSheet(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "m.mp4")
	if r := runMovie(t, dir, "check", "m.mp4", "--no-expect-audio"); r.code != 0 {
		t.Fatalf("code %d\n%s", r.code, r.stdout)
	}
	f, err := os.Open(filepath.Join(dir, "m-check", "contact-sheet.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := png.Decode(f); err != nil {
		t.Fatalf("contact sheet is not a PNG: %v", err)
	}
}
