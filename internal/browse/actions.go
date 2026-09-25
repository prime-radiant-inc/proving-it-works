package browse

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
	"github.com/prime-radiant-inc/proving-it-works/internal/film"
)

// Failed is an action the page would not let happen: a target that is not
// there or is covered, a wait that timed out, a URL that would not load.
// It is the app's verdict, not a mistake in the command (exit 1).
type Failed struct{ Msg string }

func (e Failed) Error() string { return e.Msg }

// act runs one action on the page. If the previous action ended a narrated
// beat, it first cuts to a new take. The page is marked busy from the start
// of the action until it settles. With say, a successful action ends a
// beat: say narrates everything filmed since the previous beat ended, and
// the next action starts a new take. It prints where the page is after.
func act(s *Session, say string, stdout io.Writer, action func(p *page) error) (int, error) {
	st, err := s.state()
	if err != nil {
		return exitcode.Usage, err
	}
	p, err := s.open()
	if err != nil {
		return exitcode.Usage, err
	}
	defer p.close()
	if st.CutPending {
		if err := s.cut(&st); err != nil {
			return exitcode.Usage, err
		}
	}
	if err := s.mark(st.Film, true); err != nil {
		return exitcode.Usage, err
	}
	actErr := action(p)
	p.settle()
	if err := s.mark(st.Film, false); err != nil {
		return exitcode.Usage, err
	}
	var failed Failed
	if errors.As(actErr, &failed) {
		return exitcode.Verdict, actErr
	}
	if actErr != nil {
		return exitcode.Usage, actErr
	}
	if say != "" {
		if err := film.AppendBeat(s.Dir, film.Beat{Take: st.Take, Say: say}); err != nil {
			return exitcode.Usage, err
		}
		st.CutPending = true
		if err := s.setState(st); err != nil {
			return exitcode.Usage, err
		}
	}
	return exitcode.OK, p.report(stdout)
}

// SetFilm turns filming on or off; each on after an off starts a new take.
func SetFilm(s *Session, on bool) error {
	st, err := s.state()
	if err != nil {
		return err
	}
	return s.setFilm(&st, on)
}

func (s *Session) setFilm(st *state, on bool) error {
	if st.Film == on {
		return nil
	}
	if on {
		st.Take++
	}
	st.Film = on
	if err := s.mark(on, false); err != nil {
		return err
	}
	return s.setState(*st)
}

// Cut ends the current take, holding its last picture, and starts the next.
func Cut(s *Session) error {
	st, err := s.state()
	if err != nil {
		return err
	}
	return s.cut(&st)
}

func (s *Session) cut(st *state) error {
	st.CutPending = false
	if err := s.setFilm(st, false); err != nil {
		return err
	}
	return s.setFilm(st, true)
}

// goTo loads url.
func goTo(url string) func(p *page) error {
	return func(p *page) error { return p.navigate(url) }
}

// point is where to click a target, from the overlay's point.
type point struct {
	X, Y  float64
	What  string
	Error string
	By    string
}

// click glides the cursor to target and clicks it with real mouse events.
func click(target string) func(p *page) error {
	return func(p *page) error {
		var at point
		if err := p.eval("__movie.point("+js(target)+")", &at); err != nil {
			return err
		}
		switch at.Error {
		case "missing":
			return p.missing(target)
		case "covered":
			return Failed{fmt.Sprintf("%s is covered by %s, so a click would land on that instead", at.What, at.By)}
		}
		if err := p.eval(fmt.Sprintf("__movie.glide(%g, %g)", at.X, at.Y), nil); err != nil {
			return err
		}
		mouse := func(kind string, buttons int) error {
			return p.call("Input.dispatchMouseEvent", map[string]any{"type": kind, "x": at.X, "y": at.Y,
				"button": "left", "buttons": buttons, "clickCount": 1}, nil)
		}
		if err := mouse("mouseMoved", 0); err != nil {
			return err
		}
		if err := mouse("mousePressed", 1); err != nil {
			return err
		}
		// A click can navigate away before the pulse is drawn; that is fine.
		p.eval("__movie.pulse()", nil)
		return mouse("mouseReleased", 0)
	}
}

// missing is the failure for a target that matches nothing, listing what
// the page does offer so the next attempt can name it.
func (p *page) missing(target string) error {
	msg := "no visible element matches " + target
	if list, err := p.targets(); err == nil && list != "" {
		msg += "; the page's targets:\n" + list
	}
	return Failed{msg}
}

// typeInto clicks target, then types text at human pace, each character a
// real key press. A newline presses Enter.
func typeInto(target, text string) func(p *page) error {
	return func(p *page) error {
		if err := click(target)(p); err != nil {
			return err
		}
		pace := film.TypingPace(len([]rune(text)))
		for _, r := range text {
			k := key{Key: string(r), Text: string(r)}
			if r == '\n' {
				k = keys["Enter"]
			}
			if err := p.press(k); err != nil {
				return err
			}
			time.Sleep(pace)
		}
		return nil
	}
}

// key is one key, as Input.dispatchKeyEvent describes it: Text is what it
// types, if anything.
type key struct {
	Key, Code string
	Code2     int // windowsVirtualKeyCode, which pages read as event.keyCode
	Text      string
}

var keys = map[string]key{
	"Enter":     {"Enter", "Enter", 13, "\r"},
	"Tab":       {"Tab", "Tab", 9, ""},
	"Escape":    {"Escape", "Escape", 27, ""},
	"Backspace": {"Backspace", "Backspace", 8, ""},
	"Delete":    {"Delete", "Delete", 46, ""},
	"Up":        {"ArrowUp", "ArrowUp", 38, ""},
	"Down":      {"ArrowDown", "ArrowDown", 40, ""},
	"Left":      {"ArrowLeft", "ArrowLeft", 37, ""},
	"Right":     {"ArrowRight", "ArrowRight", 39, ""},
}

// keyNamed returns the key called name: one of keys, or one character.
func keyNamed(name string) (key, error) {
	if k, ok := keys[name]; ok {
		return k, nil
	}
	if len([]rune(name)) == 1 {
		return key{Key: name, Text: name}, nil
	}
	return key{}, fmt.Errorf("unknown key %q: use Enter, Tab, Escape, Backspace, Delete, Up, Down, Left, Right, or one character", name)
}

func (p *page) press(k key) error {
	down := map[string]any{"type": "rawKeyDown", "key": k.Key, "code": k.Code, "windowsVirtualKeyCode": k.Code2}
	if k.Text != "" {
		down["type"], down["text"], down["unmodifiedText"] = "keyDown", k.Text, k.Text
	}
	if err := p.call("Input.dispatchKeyEvent", down, nil); err != nil {
		return err
	}
	return p.call("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": k.Key, "code": k.Code,
		"windowsVirtualKeyCode": k.Code2}, nil)
}

func pressKey(k key) func(p *page) error {
	return func(p *page) error { return p.press(k) }
}

// appear waits until target is visible. A navigation can replace the
// document while it waits, so a check that fails is tried again.
func appear(target string, timeout time.Duration) func(p *page) error {
	return func(p *page) error {
		deadline := time.Now().Add(timeout)
		for {
			var seen *bool
			err := p.eval("window.__movie ? __movie.visible("+js(target)+") : null", &seen)
			if err != nil && strings.Contains(err.Error(), "neither a CSS selector") {
				return err
			}
			if err == nil && seen == nil {
				p.injectOverlay()
			}
			if seen != nil && *seen {
				return nil
			}
			if time.Now().After(deadline) {
				return Failed{fmt.Sprintf("%s did not appear within %s", target, timeout)}
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// Page prints what the agent needs to act on the page: its URL and title,
// its visible text, and its targets.
func Page(s *Session, stdout io.Writer) error {
	p, err := s.open()
	if err != nil {
		return err
	}
	defer p.close()
	if err := p.report(stdout); err != nil {
		return err
	}
	var text string
	if err := p.eval("__movie.text()", &text); err != nil {
		return err
	}
	const maxLines = 80
	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("... %d more lines", len(lines)-maxLines))
	}
	fmt.Fprintf(stdout, "\ntext:\n  %s\n", strings.Join(lines, "\n  "))
	list, err := p.targets()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\ntargets:\n%s", list)
	return nil
}

// targets lists the page's links, buttons, and fields, one per line, each
// with the TARGET that names it.
func (p *page) targets() (string, error) {
	var found []struct {
		Kind, Target, Href string
	}
	if err := p.eval("__movie.targets()", &found); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, t := range found {
		target := t.Target
		if target == "" {
			target = "(no label, id, or name: use a CSS selector)"
		}
		fmt.Fprintf(&b, "  %s  %s", t.Kind, target)
		if t.Href != "" {
			fmt.Fprintf(&b, "  -> %s", t.Href)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}
