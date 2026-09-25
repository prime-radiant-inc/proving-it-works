package desk

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
	"github.com/prime-radiant-inc/proving-it-works/internal/film"
	"github.com/prime-radiant-inc/proving-it-works/internal/jsonl"
)

// StartOptions shape a new session.
type StartOptions struct {
	Display         string
	Window          string // film only this window's area, found by its name
	Title, Subtitle string
	Wrapper         []string
}

// Start works out what to film, starts the recorder, and starts filming.
// A failed start leaves the directory as it found it, empty.
func Start(dir string, o StartOptions, stdout io.Writer) (err error) {
	if runtime.GOOS == "windows" {
		return errors.New("movie desk needs Linux with X11, or a container that has it")
	}
	if err := cli.RequireEmptyDir(dir); err != nil {
		return err
	}
	if o.Display == "" && len(o.Wrapper) > 0 {
		return errors.New("--display: which X display in the wrapped environment to film, such as :99")
	}
	if o.Display == "" {
		if o.Display = os.Getenv("DISPLAY"); o.Display == "" {
			return errors.New("no X display: set DISPLAY, or pass --display")
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	s := &Session{Dir: abs, Display: o.Display, Wrapper: o.Wrapper, Title: o.Title, Subtitle: o.Subtitle}
	for _, check := range [][]string{{"xdotool", "version"}, {"ffmpeg", "-version"}} {
		if _, err := s.run(30*time.Second, check...); err != nil {
			return fmt.Errorf("%s does not run on the display's side (%v): install it there", check[0], err)
		}
	}
	if s.Region, err = s.find(o.Window); err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	spawned := false
	defer func() {
		if err != nil {
			if spawned {
				// have the recorder finish its last write before clearing
				os.WriteFile(filepath.Join(abs, "stop"), nil, 0o644)
				cli.WaitFor(5*time.Second, func() bool { return cli.Exists(filepath.Join(abs, "recorder.done")) })
			}
			cli.ClearDir(abs)
		}
	}()
	if err := s.save(); err != nil {
		return err
	}
	if err := s.set().Open(); err != nil {
		return err
	}
	if err := cli.SpawnDetached(filepath.Join(abs, "recorder.log"), "desk", "_record", abs); err != nil {
		return err
	}
	spawned = true
	if !cli.WaitFor(15*time.Second, func() bool { return cli.HasContent(filepath.Join(abs, "frames.jsonl")) }) {
		return fmt.Errorf("the recorder did not start; see %s", filepath.Join(abs, "recorder.log"))
	}
	if err := s.set().Roll(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "{\"ready\":true,\"session\":%q}\nfilming %dx%d of display %s", abs, s.Region.W, s.Region.H, s.Display)
	if o.Window != "" {
		fmt.Fprintf(stdout, ": the window %q at %d,%d", o.Window, s.Region.X, s.Region.Y)
	}
	fmt.Fprintln(stdout, "; coordinates are pixels of what is filmed, as in a shot")
	return nil
}

// find returns the region to film: the named window, or the whole display.
func (s *Session) find(window string) (region, error) {
	if window == "" {
		out, err := s.xdotool("getdisplaygeometry")
		if err != nil {
			return region{}, err
		}
		var r region
		if _, err := fmt.Sscan(out, &r.W, &r.H); err != nil {
			return region{}, fmt.Errorf("unreadable display size %q", out)
		}
		return r, nil
	}
	// the app may still be opening, so look for a few seconds
	var out string
	found := cli.WaitFor(10*time.Second, func() bool {
		var err error
		out, err = s.xdotool("search", "--onlyvisible", "--name", window, "getwindowgeometry", "--shell")
		return err == nil && strings.Contains(out, "WIDTH=")
	})
	if !found {
		return region{}, fmt.Errorf("no visible window named %q on display %s", window, s.Display)
	}
	v := shellValues(out)
	return region{X: v["X"], Y: v["Y"], W: v["WIDTH"], H: v["HEIGHT"]}, nil
}

// Shot saves what is filmed now as a PNG at path (SESSION/shot.png when
// empty) and prints where it is, its size, and where the pointer is.
func Shot(s *Session, path string, stdout io.Writer) error {
	if path == "" {
		path = filepath.Join(s.Dir, "shot.png")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	picture, err := s.command(ctx, s.capture(ctx, 1)...).Output()
	if err != nil {
		return fmt.Errorf("capturing the screen: %w", err)
	}
	if err := os.WriteFile(path, picture, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s (%dx%d), pointer at %s\n", path, s.Region.W, s.Region.H, s.where())
	return nil
}

// where describes the pointer's position in filmed pixels.
func (s *Session) where() string {
	p, err := s.pointer()
	if err != nil {
		return "unknown"
	}
	x, y := p.X-s.Region.X, p.Y-s.Region.Y
	if _, err := s.Region.screen(x, y); err != nil {
		return fmt.Sprintf("%d,%d (outside what is filmed)", x, y)
	}
	return fmt.Sprintf("%d,%d", x, y)
}

// act runs one action as film.Set.Act describes, settling after it, and
// prints what it did.
func act(s *Session, say string, stdout io.Writer, do func() (string, error)) (int, error) {
	did, code, err := s.set().Act(say, do, func() { s.settle(time.Now(), settleQuiet, settleMax) })
	if err == nil && did != "" {
		fmt.Fprintln(stdout, did)
	}
	return code, err
}

// Settling: an action is over once no new picture has arrived for
// settleQuiet since it ended, or after settleMax whatever the app is doing.
const (
	settleQuiet = 400 * time.Millisecond
	settleMax   = 5 * time.Second
)

// settle waits until the picture has held still for quiet since since, as
// the recorder's log shows, and reports whether it did within max.
func (s *Session) settle(since time.Time, quiet, max time.Duration) bool {
	return cli.WaitFor(max, func() bool {
		still := time.Since(since)
		if frames, err := jsonl.Read[film.Frame](filepath.Join(s.Dir, "frames.jsonl")); err == nil && len(frames) > 0 {
			last := frames[len(frames)-1].T
			still = min(still, time.Duration((film.Now()-last)*float64(time.Second)))
		}
		return still >= quiet
	})
}

// point turns filmed coordinates, as the agent gives them, into a screen point.
func (s *Session) point(x, y string) (image.Point, error) {
	xi, err1 := strconv.Atoi(x)
	yi, err2 := strconv.Atoi(y)
	if err1 != nil || err2 != nil {
		return image.Point{}, fmt.Errorf("%s,%s: coordinates are whole pixels", x, y)
	}
	return s.Region.screen(xi, yi)
}

// move glides the pointer to x, y.
func move(s *Session, to image.Point, then ...string) error {
	from, err := s.pointer()
	if err != nil {
		return err
	}
	_, err = s.xdotool(append(glide(from, to), then...)...)
	return err
}

// Move glides the pointer to a filmed point.
func Move(s *Session, x, y, say string, stdout io.Writer) (int, error) {
	to, err := s.point(x, y)
	if err != nil {
		return exitcode.Usage, err
	}
	return act(s, say, stdout, func() (string, error) {
		return fmt.Sprintf("moved to %s,%s", x, y), move(s, to)
	})
}

// Click glides to a filmed point and clicks there.
func Click(s *Session, x, y string, right, double bool, say string, stdout io.Writer) (int, error) {
	to, err := s.point(x, y)
	if err != nil {
		return exitcode.Usage, err
	}
	button, name := 1, "clicked"
	if right {
		button, name = 3, "right-clicked"
	}
	if double {
		name = "double-" + name
	}
	return act(s, say, stdout, func() (string, error) {
		return fmt.Sprintf("%s at %s,%s", name, x, y), move(s, to, press(button, double)...)
	})
}

// Drag presses at one filmed point, glides to another, and releases.
func Drag(s *Session, x1, y1, x2, y2, say string, stdout io.Writer) (int, error) {
	from, err := s.point(x1, y1)
	if err != nil {
		return exitcode.Usage, err
	}
	to, err := s.point(x2, y2)
	if err != nil {
		return exitcode.Usage, err
	}
	return act(s, say, stdout, func() (string, error) {
		if err := move(s, from, "sleep", "0.2", "mousedown", "1"); err != nil {
			return "", err
		}
		_, err := s.xdotool(append(glide(from, to), "sleep", "0.12", "mouseup", "1")...)
		return fmt.Sprintf("dragged from %s,%s to %s,%s", x1, y1, x2, y2), err
	})
}

// Key presses keys, a quarter second apart.
func Key(s *Session, keys []string, say string, stdout io.Writer) (int, error) {
	return act(s, say, stdout, func() (string, error) {
		_, err := s.xdotool(append([]string{"key", "--delay", "250"}, keys...)...)
		return "pressed " + strings.Join(keys, " "), err
	})
}

// Type types text at human pace into whatever has the focus.
func Type(s *Session, text, say string, stdout io.Writer) (int, error) {
	pace := film.TypingPace(len([]rune(text)))
	return act(s, say, stdout, func() (string, error) {
		_, err := s.xdotool("type", "--delay", strconv.Itoa(int(pace.Milliseconds())), "--", text)
		return fmt.Sprintf("typed %q", text), err
	})
}

// Wait waits until the picture holds still for quiet, counted from now,
// within timeout: the app finishing slow work.
func Wait(s *Session, quiet, timeout time.Duration, say string, stdout io.Writer) (int, error) {
	return act(s, say, stdout, func() (string, error) {
		if !s.settle(time.Now(), quiet, timeout) {
			return "", film.Failed{Msg: fmt.Sprintf("the picture did not hold still for %s within %s", quiet, timeout)}
		}
		return fmt.Sprintf("held still for %s", quiet), nil
	})
}

// Stop ends filming and renders the takes.
func Stop(s *Session, outdir string, stdout io.Writer) error {
	if cli.Exists(filepath.Join(s.Dir, "stop")) {
		return fmt.Errorf("%s is already stopped: use render to render it again", s.Dir)
	}
	if err := cli.RequireEmptyDir(outdir); err != nil {
		return err
	}
	if err := s.set().End(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "stop"), nil, 0o644); err != nil {
		return err
	}
	if !cli.WaitFor(5*time.Second, func() bool { return cli.Exists(filepath.Join(s.Dir, "recorder.done")) }) {
		fmt.Fprintln(stdout, "WARN       the recorder did not confirm it stopped; rendering what it wrote")
	}
	return Render(s.Dir, outdir, stdout)
}

// Render draws every take into outdir/take-N/ and writes outdir/scenes.yaml.
func Render(dir, outdir string, stdout io.Writer) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	m := film.Movie{Tool: "movie desk stop"}
	if s, err := Load(abs); err == nil {
		m.Title, m.Subtitle = s.Title, s.Subtitle
	}
	return film.RenderPictures(abs, outdir, m, stdout)
}
