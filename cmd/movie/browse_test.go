package main

import (
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/cdp"
)

// serveApp serves pages, keyed by path, for one test.
func serveApp(t *testing.T, pages map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(page))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// startBrowse starts a browse session on url and, when t ends, kills its
// browser and waits for its recorder to finish writing.
func startBrowse(t *testing.T, dir, url string, args ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("movie browse needs macOS, Linux, or WSL")
	}
	if _, err := cdp.FindBrowser(""); err != nil {
		t.Skip(err)
	}
	session := filepath.Join(dir, "session")
	t.Cleanup(func() {
		var s struct{ Browser cdp.Browser }
		if data, err := os.ReadFile(filepath.Join(session, "session.json")); err == nil && json.Unmarshal(data, &s) == nil {
			s.Browser.Kill()
		}
		done := filepath.Join(session, "recorder.done")
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(done); err == nil || time.Now().After(deadline) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	r := runMovie(t, dir, append([]string{"browse", "start", session, url}, args...)...)
	if r.code != 0 {
		t.Fatalf("start: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	return session
}

func runBrowse(t *testing.T, dir string, args ...string) result {
	t.Helper()
	return runMovie(t, dir, append([]string{"browse"}, args...)...)
}

func mustBrowse(t *testing.T, dir string, args ...string) result {
	t.Helper()
	r := runBrowse(t, dir, args...)
	if r.code != 0 {
		t.Fatalf("browse %q: code %d\n%s%s", args, r.code, r.stdout, r.stderr)
	}
	return r
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return img
}

// near reports whether the pixel at x, y is within 8 levels of r, g, b.
func near(img image.Image, x, y int, r, g, b uint8) bool {
	cr, cg, cb, _ := img.At(x, y).RGBA()
	d := func(a uint32, b uint8) bool { v := int(a>>8) - int(b); return v <= 8 && v >= -8 }
	return d(cr, r) && d(cg, g) && d(cb, b)
}

const bluePage = `<!doctype html><title>Blue</title>
<body style="margin:0;background:#336699;color:white;font:40px sans-serif"><h1>Hello from the app</h1></body>`

func TestBrowseFilmsAPageIntoFrames(t *testing.T) {
	dir := t.TempDir()
	url := serveApp(t, map[string]string{"/": bluePage})
	session := startBrowse(t, dir, url, "--title", "Demo")
	takes := filepath.Join(dir, "takes")
	r := mustBrowse(t, dir, "stop", session, takes)
	if !strings.Contains(r.stdout, "1 takes, 0 narrated") {
		t.Errorf("stop printed:\n%s", r.stdout)
	}
	first := readPNG(t, filepath.Join(takes, "take-1", "f00000.png"))
	if first.Bounds().Dx() != 1600 || first.Bounds().Dy() != 900 {
		t.Fatalf("frames are %v, want 1600x900 (1280x720 at 1.25x)", first.Bounds())
	}
	if !near(first, 1500, 800, 0x33, 0x66, 0x99) {
		t.Errorf("the first frame is not the app's blue page: %v at 1500,800", first.At(1500, 800))
	}
	frames, _ := filepath.Glob(filepath.Join(takes, "take-1", "f*.png"))
	if len(frames) < 15 {
		t.Errorf("take-1 has %d frames; it should hold the page for at least 1.5 s", len(frames))
	}
	scenes, _ := os.ReadFile(filepath.Join(takes, "scenes.yaml"))
	for _, want := range []string{"# Written by movie browse stop.", "size: 1600x900", "card: Demo", "frames: take-1"} {
		if !strings.Contains(string(scenes), want) {
			t.Errorf("scenes.yaml lacks %q:\n%s", want, scenes)
		}
	}
	if _, err := os.Stat(filepath.Join(session, "profile")); !os.IsNotExist(err) {
		t.Errorf("stop left the browser profile behind: %v", err)
	}
}

func TestBrowseStartRefusesAMissingBrowser(t *testing.T) {
	dir := t.TempDir()
	r := runBrowse(t, dir, "start", filepath.Join(dir, "s"), "http://example.invalid/", "--browser", "/no/such/chrome")
	if r.code != 2 || !strings.Contains(r.stderr, "--browser /no/such/chrome: not an executable") {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

func TestBrowseVerbsNeedASession(t *testing.T) {
	dir := t.TempDir()
	r := runBrowse(t, dir, "page", filepath.Join(dir, "nothing"))
	if r.code != 2 || !strings.Contains(r.stderr, "no movie browse session at") {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}
