package browse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/cdp"
	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/film"
	"github.com/prime-radiant-inc/proving-it-works/internal/jsonl"
)

// scale is the device pixels per CSS pixel the page is filmed at: a
// 1280x720 viewport films at 1600x900.
const scale = 1.25

// StartOptions shape a new session.
type StartOptions struct {
	Width, Height   int
	Title, Subtitle string
	Browser         string // a browser to run instead of the first one found
}

// Start launches the browser, starts the recorder, and loads url; filming
// starts once the page has settled. Any failure after the browser is
// running kills it, so Start never leaves one behind.
func Start(dir, url string, o StartOptions, stdout io.Writer) (err error) {
	if runtime.GOOS == "windows" {
		return errors.New("movie browse needs macOS, Linux, or WSL")
	}
	if err := cli.RequireEmptyDir(dir); err != nil {
		return err
	}
	path, err := cdp.FindBrowser(o.Browser)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	b, err := cdp.Launch(path, cdp.Options{Profile: filepath.Join(abs, "profile"), Width: o.Width, Height: o.Height,
		Scale: scale, Log: filepath.Join(abs, "browser.log")})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			b.Kill()
		}
	}()
	s := &Session{Dir: abs, Browser: *b, Title: o.Title, Subtitle: o.Subtitle}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	conn, err := cdp.Dial(ctx, b.WebSocket)
	if err != nil {
		return err
	}
	s.Page, err = conn.Page(ctx)
	conn.Close()
	if err != nil {
		return err
	}
	if err := s.save(); err != nil {
		return err
	}
	if err := s.fitViewport(o.Width, o.Height); err != nil {
		return err
	}
	if err := s.setState(state{Take: 1}); err != nil {
		return err
	}
	if err := s.mark(false, true); err != nil {
		return err
	}
	if err := cli.SpawnDetached(filepath.Join(abs, "recorder.log"), "browse", "_record", abs); err != nil {
		return err
	}
	if !waitFor(10*time.Second, func() bool { return hasContent(filepath.Join(abs, "frames.jsonl")) }) {
		return fmt.Errorf("the recorder did not start; see %s", filepath.Join(abs, "recorder.log"))
	}
	p, err := s.open()
	if err != nil {
		return err
	}
	defer p.close()
	if err := p.navigate(url); err != nil {
		return err
	}
	if err := s.setState(state{Take: 1, Film: true}); err != nil {
		return err
	}
	if err := s.mark(true, false); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "{\"ready\":true,\"session\":%q}\n", abs)
	return p.report(stdout)
}

// fitViewport sizes the browser window so the page's viewport is exactly
// width x height CSS pixels: --window-size includes the window's own frame,
// whose size differs between browsers and versions.
func (s *Session) fitViewport(width, height int) error {
	p, err := s.open()
	if err != nil {
		return err
	}
	defer p.close()
	// A resize lands a moment later and can round by a pixel at a
	// fractional scale, so measure and correct until it fits.
	var inner struct{ W, H int }
	for range 10 {
		if err := p.eval(`({W: innerWidth, H: innerHeight})`, &inner); err != nil {
			return err
		}
		if inner.W == width && inner.H == height {
			return nil
		}
		var window struct {
			WindowID int `json:"windowId"`
			Bounds   struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"bounds"`
		}
		if err := p.conn.Call(context.Background(), "", "Browser.getWindowForTarget", map[string]string{"targetId": s.Page}, &window); err != nil {
			return err
		}
		bounds := map[string]int{"width": window.Bounds.Width + width - inner.W, "height": window.Bounds.Height + height - inner.H}
		if err := p.conn.Call(context.Background(), "", "Browser.setWindowBounds",
			map[string]any{"windowId": window.WindowID, "bounds": bounds}, nil); err != nil {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("the browser would not size its viewport to %dx%d (it is %dx%d)", width, height, inner.W, inner.H)
}

// waitFor polls cond until it holds, reporting false if it never does
// within timeout.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasContent(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// loadFailed is the failure for a URL the browser could not load, such as
// one whose server refused the connection: the app's failure, not the tool's.
func loadFailed(url, reason string) error {
	return Failed{fmt.Sprintf("could not load %s: %s", url, reason)}
}

// navigate loads url and waits for it to load and settle.
func (p *page) navigate(url string) error {
	loaded := make(chan struct{}, 1)
	p.conn.OnEvent(func(e cdp.Event) {
		if e.Session == p.session && e.Method == "Page.loadEventFired" {
			select {
			case loaded <- struct{}{}:
			default:
			}
		}
	})
	defer p.conn.OnEvent(nil)
	if err := p.call("Page.enable", nil, nil); err != nil {
		return err
	}
	var got struct {
		ErrorText string `json:"errorText"`
	}
	if err := p.call("Page.navigate", map[string]string{"url": url}, &got); err != nil {
		return err
	}
	if got.ErrorText != "" {
		return loadFailed(url, got.ErrorText)
	}
	select {
	case <-loaded:
	case <-time.After(callTimeout):
		return loadFailed(url, "it did not finish loading within 30 s")
	}
	return p.settle()
}

// Settling: an action is over once the page has loaded and has not changed
// for settleQuiet, or after settleMax whatever it is doing.
const (
	settleQuiet = 400 * time.Millisecond
	settleMax   = 5 * time.Second
)

// settle waits for the page to settle. A navigation the action started can
// replace the document mid-check, so a failed check is simply tried again.
func (p *page) settle() error {
	deadline := time.Now().Add(settleMax)
	for time.Now().Before(deadline) {
		var got struct {
			Ready string  `json:"ready"`
			Quiet float64 `json:"quiet"`
		}
		err := p.eval(`({ready: document.readyState, quiet: window.__movie ? __movie.quiet() : -1})`, &got)
		if err == nil && got.Quiet < 0 {
			err = p.injectOverlay() // a new document the recorder's script missed
		}
		if err == nil && got.Ready == "complete" && got.Quiet >= float64(settleQuiet.Milliseconds()) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// report prints where the page is: its URL and title.
func (p *page) report(stdout io.Writer) error {
	var got struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := p.eval(`({url: location.href, title: document.title})`, &got); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "at %s %q\n", got.URL, got.Title)
	return nil
}

// Stop ends the session: it marks the end, has the recorder finish, closes
// the browser, removes its profile, and renders the takes.
func Stop(s *Session, outdir string, stdout io.Writer) error {
	if err := cli.RequireEmptyDir(outdir); err != nil {
		return err
	}
	if err := jsonl.Append(filepath.Join(s.Dir, "marks.jsonl"), Mark{T: now(), End: true}); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "stop"), nil, 0o644); err != nil {
		return err
	}
	if !waitFor(5*time.Second, func() bool { return exists(filepath.Join(s.Dir, "recorder.done")) }) {
		fmt.Fprintln(stdout, "WARN       the recorder did not confirm it stopped; rendering what it wrote")
	}
	closeBrowser(s.Browser)
	if err := os.RemoveAll(filepath.Join(s.Dir, "profile")); err != nil {
		return err
	}
	return Render(s.Dir, outdir, stdout)
}

// closeBrowser asks the browser to exit, and kills it if it has not within
// a few seconds.
func closeBrowser(b cdp.Browser) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if conn, err := cdp.Dial(ctx, b.WebSocket); err == nil {
		conn.Call(ctx, "", "Browser.close", nil, nil)
		select {
		case <-conn.Done():
		case <-ctx.Done():
		}
		conn.Close()
	}
	waitFor(3*time.Second, func() bool { return !b.Alive() })
	b.Kill()
}

// Render draws every take of the session in dir into outdir/take-N/ and
// writes outdir/scenes.yaml.
func Render(dir, outdir string, stdout io.Writer) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	frames, err := jsonl.Read[Frame](filepath.Join(abs, "frames.jsonl"))
	if err != nil {
		return err
	}
	marks, err := jsonl.Read[Mark](filepath.Join(abs, "marks.jsonl"))
	if err != nil {
		return err
	}
	beats, err := film.ReadBeats(abs)
	if err != nil {
		return err
	}
	m := film.Movie{Tool: "movie browse stop"}
	if s, err := Load(abs); err == nil {
		m.Title, m.Subtitle = s.Title, s.Subtitle
	}
	shots := Shots(frames, marks)
	if len(shots) > 0 {
		first, err := os.ReadFile(filepath.Join(abs, "frames", shots[0].Look))
		if err != nil {
			return err
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(first))
		if err != nil {
			return fmt.Errorf("frames/%s: %w", shots[0].Look, err)
		}
		m.Size = image.Pt(cfg.Width, cfg.Height)
	}
	draw := func(shot film.Shot) ([]byte, error) { return os.ReadFile(filepath.Join(abs, "frames", shot.Look)) }
	if _, err := film.Write(outdir, m, film.Split(shots), beats, draw, stdout); err != nil {
		return err
	}
	for _, f := range frames {
		if f.End && f.Reason != "" {
			fmt.Fprintf(stdout, "WARN       the recording ended without a stop: %s\n", f.Reason)
		}
	}
	return nil
}
