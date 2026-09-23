// Package testmedia makes real media for tests with ffmpeg's built-in lavfi
// sources, so no media is committed and nothing is mocked.
package testmedia

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// Require skips t unless every named executable is on PATH.
func Require(t testing.TB, tools ...string) {
	t.Helper()
	var missing []string
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		t.Skipf("needs %s on PATH", strings.Join(missing, ", "))
	}
}

// FFmpeg runs ffmpeg in dir and fails t on error.
func FFmpeg(t testing.TB, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", append([]string{"-nostdin", "-y", "-v", "error"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %q: %v\n%s", args, err, out)
	}
}

// Duration returns a media file's duration in seconds.
func Duration(t testing.TB, path string) float64 {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration",
		"-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	return d
}
