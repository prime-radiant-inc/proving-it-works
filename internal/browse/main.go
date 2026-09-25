package browse

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

const usage = `usage: movie browse VERB SESSION ...

  start SESSION URL [--size 1280x720] [--title T] [--subtitle S] [--browser PATH]
        launch a headless Chrome on URL, filming; returns when the page has settled
  goto SESSION URL [--say "narration"]
        load URL and wait for it to settle
  click SESSION TARGET [--say "narration"]
        glide the cursor to TARGET and click it
  type SESSION TARGET 'text' [--say "narration"]
        click TARGET, then type at human pace
  press SESSION Enter|Tab|Escape|Backspace|Delete|Up|Down|Left|Right|<one character> [--say "narration"]
        press one key
  wait SESSION TARGET [--timeout 10] [--say "narration"]
        wait until TARGET is visible
  page SESSION
        print the URL, title, visible text, and the targets on the page
  cut SESSION
        end this take, holding its result, and start the next
  film SESSION on|off
        stop or resume filming; each on starts a new take
  stop SESSION OUTDIR
        close the browser, render each take into OUTDIR/take-N/, and write OUTDIR/scenes.yaml
  render SESSION OUTDIR
        render the takes again from the recording, even after stop

A TARGET is a CSS selector, or text=Label: the button, link, or field a
person would call Label (its text, label, placeholder, or aria-label), else
any element showing that text. --say ends a beat: the sentence narrates
everything since the last --say, over this action's result. Actions exit 1
when the page will not let them happen (the target is missing or covered,
the wait timed out, the URL would not load).
`

// Main is movie browse.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitcode.Usage
	}
	verb, rest := args[0], args[1:]
	if verb == "help" || verb == "-h" || verb == "--help" {
		fmt.Fprint(stdout, usage)
		return exitcode.OK
	}
	code, err := dispatch(verb, rest, stdout, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "movie browse %s: %v\n", verb, err)
		if code == exitcode.OK {
			code = exitcode.Usage
		}
	}
	return code
}

func dispatch(verb string, args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("movie browse "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	switch verb {
	case "start":
		size := fs.String("size", "1280x720", "viewport width x height, in CSS pixels")
		title := fs.String("title", "", "title card for the scene file stop writes")
		subtitle := fs.String("subtitle", "", "subtitle for that title card")
		browser := fs.String("browser", "", "the Chrome, Chromium, or Edge to run")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 2 {
			return exitcode.Usage, fmt.Errorf("needs SESSION and URL")
		}
		w, h, err := cli.ParseSize(*size)
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Start(pos[0], pos[1], StartOptions{Width: w, Height: h, Title: *title,
			Subtitle: *subtitle, Browser: *browser}, stdout)
	case "goto", "click", "type", "press", "wait":
		say := fs.String("say", "", "narration for the beat this action ends")
		timeout := 10.0
		if verb == "wait" {
			fs.Float64Var(&timeout, "timeout", 10, "seconds to wait")
		}
		pos, err := cli.Parse(fs, args)
		want := map[string]int{"goto": 2, "click": 2, "type": 3, "press": 2, "wait": 2}[verb]
		if err != nil || len(pos) != want {
			return exitcode.Usage, fmt.Errorf("needs %s", map[string]string{"goto": "SESSION and URL",
				"click": "SESSION and TARGET", "type": "SESSION, TARGET, and text", "press": "SESSION and a key",
				"wait": "SESSION and TARGET"}[verb])
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		var action func(p *page) error
		switch verb {
		case "goto":
			action = goTo(pos[1])
		case "click":
			action = click(pos[1])
		case "type":
			action = typeInto(pos[1], pos[2])
		case "press":
			k, err := keyNamed(pos[1])
			if err != nil {
				return exitcode.Usage, err
			}
			action = pressKey(k)
		case "wait":
			action = appear(pos[1], time.Duration(timeout*float64(time.Second)))
		}
		return act(s, *say, stdout, action)
	case "page", "cut":
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 1 {
			return exitcode.Usage, fmt.Errorf("needs SESSION")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		if verb == "cut" {
			return exitcode.OK, Cut(s)
		}
		return exitcode.OK, Page(s, stdout)
	case "film":
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 2 || (pos[1] != "on" && pos[1] != "off") {
			return exitcode.Usage, fmt.Errorf("needs SESSION and on or off")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, SetFilm(s, pos[1] == "on")
	case "stop", "render":
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 2 {
			return exitcode.Usage, fmt.Errorf("needs SESSION and OUTDIR")
		}
		if verb == "render" {
			return exitcode.OK, Render(pos[0], pos[1], stdout)
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Stop(s, pos[1], stdout)
	case "_record":
		if len(args) != 1 {
			return exitcode.Usage, fmt.Errorf("needs SESSION")
		}
		return exitcode.OK, Record(args[0], stderr)
	}
	fmt.Fprint(stderr, usage)
	return exitcode.Usage, fmt.Errorf("unknown verb %q", verb)
}
