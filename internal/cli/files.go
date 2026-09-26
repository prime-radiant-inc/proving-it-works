package cli

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WaitFor polls cond until it holds, reporting false if it never does
// within timeout.
func WaitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}

// ClearDir removes everything in dir, such as a session directory a
// failed start made or found empty.
func ClearDir(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

// Exists reports whether path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// HasContent reports whether path exists and holds something.
func HasContent(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// FirstLine is the first non-empty line of text, or fallback when there is
// none: the reason a tool gives on stderr.
func FirstLine(text, fallback string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return fallback
}
