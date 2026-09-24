package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prime-radiant-inc/proving-it-works/internal/testmedia"
)

// writeFile writes content to dir/name, creating parent directories.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// still writes a solid-colour PNG at dir/name. ffmpeg writes it under a safe
// name first, because its image muxer would read "%03d" in name as a pattern.
func still(t *testing.T, dir, name, colour string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "color=c="+colour+":size=320x180:d=0.1",
		"-frames:v", "1", "still-tmp.png")
	if err := os.Rename(filepath.Join(dir, "still-tmp.png"), filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

func assertNear(t *testing.T, what string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s: got %.3f, want %.3f ± %.3f", what, got, want, tolerance)
	}
}

func TestBuildAssemblesImageScenesInOrderAndChecksTheResult(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	still(t, dir, "red.png", "red")
	still(t, dir, "blue.png", "blue")
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
scenes:
  - id: first
    image: red.png
    duration: 2
  - id: second
    image: blue.png
    duration: 3
`)
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 0 {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	assertNear(t, "movie duration", testmedia.Duration(t, filepath.Join(dir, "demo.mp4")), 5, 0.2)
	if !strings.Contains(r.stdout, "Mechanical checks pass") {
		t.Fatalf("build did not run the check:\n%s", r.stdout)
	}
}

// awkward is a directory name that breaks every naive ffmpeg path handling.
const awkward = "awk %d [x] 'q' λ & more"

func TestBuildHandlesEverySceneKindAtAwkwardPaths(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := filepath.Join(t.TempDir(), awkward)
	still(t, dir, "media/shot %03d.png", "red")
	for i, c := range []string{"red", "green", "blue", "white", "black"} {
		still(t, dir, filepath.Join("media", "frames [1]", fmt.Sprintf("f%02d.png", i)), c)
	}
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=2",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "media/with sound.mp4")
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=1.5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "media/silent 100%.mp4")
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
scenes:
  - id: title
    card: proving-it-works
    subtitle: every kind of scene
    duration: 1
  - id: shot
    image: "media/shot %03d.png"
    duration: 1
  - id: run
    frames: "media/frames [1]"
    rate: 2.5
  - id: loud
    movie: "media/with sound.mp4"
  - id: quiet
    movie: "media/silent 100%.mp4"
`)
	r := runMovie(t, dir, "build", "demo.yaml", "out/final cut.mp4")
	if r.code != 0 {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	// 1 + 1 + 5/2.5 + 2 + 1.5
	assertNear(t, "movie duration", testmedia.Duration(t, filepath.Join(dir, "out", "final cut.mp4")), 7.5, 0.3)
	for _, line := range []string{"title: 1.0s", "shot: 1.0s", "run: 2.0s", "loud: 2.0s", "quiet: 1.5s"} {
		if !strings.Contains(r.stdout, line) {
			t.Errorf("missing %q in\n%s", line, r.stdout)
		}
	}
}

func TestBuildRejectsAnInvalidSceneFileBeforeEncoding(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	writeFile(t, dir, "demo.yaml", "scenes:\n  - id: x\n    image: nope.png\n")
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 2 || !strings.Contains(r.stderr, "no such image") {
		t.Fatalf("code %d\n%s", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo.build")); err == nil {
		t.Fatal("scratch was created for an invalid scene file")
	}
}
