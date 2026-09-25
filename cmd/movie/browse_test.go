package main

import (
	"encoding/json"
	"image"
	"image/png"
	"net"
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

// todoApp adds an item a moment after its form is submitted, as an app
// that saves to a server would. Everything is blue, so the only near-black
// pixels in a frame are the movie's cursor.
const todoApp = `<!doctype html><title>Todo</title>
<body style="margin:20px;background:#fff;color:#0033cc;font:24px sans-serif">
<h1>Todo</h1>
<form id="f"><label>New item <input id="item" placeholder="What needs doing?" style="color:#0033cc;font:24px sans-serif"></label>
<button style="color:#0033cc;font:24px sans-serif">Add</button></form>
<ul id="list"></ul><p id="saved"></p>
<a href="/about" style="color:#0033cc">About</a>
<button id="under" style="position:fixed;left:30px;bottom:30px;color:#0033cc">Under</button>
<div style="position:fixed;left:0;bottom:0;width:300px;height:100px;background:#ccddff">cover</div>
<script>
f.onsubmit = (e) => {
  e.preventDefault();
  const text = item.value;
  item.value = "";
  setTimeout(() => {
    const li = document.createElement("li");
    li.textContent = text;
    list.appendChild(li);
    saved.textContent = "Saved " + list.children.length;
  }, 300);
};
</script>`

const aboutPage = `<!doctype html><title>About</title><body style="color:#0033cc"><h1>About this app</h1></body>`

// cursorPixels counts the near-black pixels in a frame: the cursor's.
func cursorPixels(img image.Image) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if near(img, x, y, 0x11, 0x11, 0x11) {
				n++
			}
		}
	}
	return n
}

func TestBrowseDrivesAnAppAndNarratesIt(t *testing.T) {
	dir := t.TempDir()
	url := serveApp(t, map[string]string{"/": todoApp, "/about": aboutPage})
	session := startBrowse(t, dir, url)

	r := mustBrowse(t, dir, "type", session, "text=New item", "buy milk")
	if !strings.Contains(r.stdout, `at `+url+`/ "Todo"`) {
		t.Errorf("type printed %q", r.stdout)
	}
	mustBrowse(t, dir, "click", session, "text=Add", "--say", "We add an item.")
	mustBrowse(t, dir, "wait", session, "text=Saved 1")
	mustBrowse(t, dir, "type", session, "#item", "write report")
	mustBrowse(t, dir, "press", session, "Enter")
	mustBrowse(t, dir, "wait", session, "text=Saved 2", "--say", "Enter adds one too.")

	r = mustBrowse(t, dir, "page", session)
	for _, want := range []string{url + "/", "Todo", "buy milk", "write report", "Saved 2",
		"button  text=Add", "link  text=About  -> /about", `input:text  text=New item`} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("page lacks %q:\n%s", want, r.stdout)
		}
	}

	r = mustBrowse(t, dir, "click", session, "text=About")
	if !strings.Contains(r.stdout, `at `+url+`/about "About"`) {
		t.Errorf("clicking a link printed %q", r.stdout)
	}
	r = mustBrowse(t, dir, "goto", session, url+"/", "--say", "And back.")
	if !strings.Contains(r.stdout, `"Todo"`) {
		t.Errorf("goto printed %q", r.stdout)
	}

	takes := filepath.Join(dir, "takes")
	r = mustBrowse(t, dir, "stop", session, takes)
	if !strings.Contains(r.stdout, "3 takes, 3 narrated") {
		t.Fatalf("stop printed:\n%s", r.stdout)
	}
	scenes, _ := os.ReadFile(filepath.Join(takes, "scenes.yaml"))
	for _, want := range []string{"narration: We add an item.", "narration: Enter adds one too.", "narration: And back."} {
		if !strings.Contains(string(scenes), want) {
			t.Errorf("scenes.yaml lacks %q:\n%s", want, scenes)
		}
	}
	frames, _ := filepath.Glob(filepath.Join(takes, "take-1", "f*.png"))
	if n := cursorPixels(readPNG(t, frames[len(frames)-1])); n < 30 {
		t.Errorf("take-1's last frame has %d cursor pixels; the cursor should be on the Add button", n)
	}
}

// closedPortURL is a URL nothing is listening on.
func closedPortURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + l.Addr().String() + "/"
	l.Close()
	return url
}

func TestBrowseActionsThatCannotHappenExit1(t *testing.T) {
	dir := t.TempDir()
	url := serveApp(t, map[string]string{"/": todoApp})
	session := startBrowse(t, dir, url)

	r := runBrowse(t, dir, "click", session, "text=Nope")
	if r.code != 1 || !strings.Contains(r.stderr, "no visible element matches text=Nope") || !strings.Contains(r.stderr, "button  text=Add") {
		t.Errorf("a missing target: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	r = runBrowse(t, dir, "click", session, "#under")
	if r.code != 1 || !strings.Contains(r.stderr, `<button#under> "Under" is covered by <div> "cover"`) {
		t.Errorf("a covered target: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	r = runBrowse(t, dir, "wait", session, "text=Never", "--timeout", "1")
	if r.code != 1 || !strings.Contains(r.stderr, "text=Never did not appear within 1s") {
		t.Errorf("a wait that times out: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	closed := closedPortURL(t)
	r = runBrowse(t, dir, "goto", session, closed)
	if r.code != 1 || !strings.Contains(r.stderr, "could not load "+closed+": net::ERR_CONNECTION_REFUSED") {
		t.Errorf("an unreachable URL: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	r = runBrowse(t, dir, "click", session, "[[bad")
	if r.code != 2 || !strings.Contains(r.stderr, "[[bad is neither a CSS selector nor text=Label") {
		t.Errorf("a malformed target: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	r = runBrowse(t, dir, "press", session, "Hyper")
	if r.code != 2 || !strings.Contains(r.stderr, `unknown key "Hyper"`) {
		t.Errorf("an unknown key: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

func TestBrowseFilmOffLeavesActionsOutOfTheMovie(t *testing.T) {
	dir := t.TempDir()
	url := serveApp(t, map[string]string{"/": todoApp})
	session := startBrowse(t, dir, url)
	mustBrowse(t, dir, "film", session, "off")
	mustBrowse(t, dir, "type", session, "#item", "secret")
	mustBrowse(t, dir, "click", session, "text=Add")
	mustBrowse(t, dir, "wait", session, "text=Saved 1")
	mustBrowse(t, dir, "film", session, "on")
	mustBrowse(t, dir, "cut", session)
	takes := filepath.Join(dir, "takes")
	r := mustBrowse(t, dir, "stop", session, takes)
	if !strings.Contains(r.stdout, "3 takes") {
		t.Fatalf("want the start, the result after film on, and the take after the cut:\n%s", r.stdout)
	}
	// the typing happened off camera: take 1 never shows the item, take 2 opens on it saved
	first := readPNG(t, filepath.Join(takes, "take-1", "f00000.png"))
	second := readPNG(t, filepath.Join(takes, "take-2", "f00000.png"))
	if first.Bounds() != second.Bounds() {
		t.Fatal("takes differ in size")
	}
	same := true
	for y := 0; y < first.Bounds().Dy() && same; y += 4 {
		for x := 0; x < first.Bounds().Dx(); x += 4 {
			if first.At(x, y) != second.At(x, y) {
				same = false
				break
			}
		}
	}
	if same {
		t.Error("take 2 should open on the saved item, not the page take 1 showed")
	}
}

// Time spent waiting on the agent is cut to 1.5 s, even while a focused
// field's caret would blink and make the page look busy.
func TestBrowseCutsTheAgentsThinkingTimeWithAFieldFocused(t *testing.T) {
	dir := t.TempDir()
	url := serveApp(t, map[string]string{"/": todoApp})
	session := startBrowse(t, dir, url)
	mustBrowse(t, dir, "type", session, "#item", "x")
	time.Sleep(4 * time.Second)
	takes := filepath.Join(dir, "takes")
	mustBrowse(t, dir, "stop", session, takes)
	frames, _ := filepath.Glob(filepath.Join(takes, "take-1", "f*.png"))
	// start's idle 1.5 s at most, the glide and click, the settle, and 1.5 s held
	if len(frames) > 45 {
		t.Errorf("take-1 has %d frames: the 4 s the agent spent thinking was not cut", len(frames))
	}
}
