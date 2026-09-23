package main

import (
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
