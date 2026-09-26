package desk

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

const usage = `usage: movie desk VERB SESSION ...

  start SESSION [--display :99] [--window NAME] [--title T] [--subtitle S] [-- WRAPPER...]
        film an X display, or one window's area, with ffmpeg; drive it with xdotool
  shot SESSION [PNG]
        save what is filmed now (SESSION/shot.png) to look at; prints its size and the pointer
  move SESSION X Y
        glide the pointer to X,Y
  click SESSION X Y [--right] [--double]
        glide to X,Y and click
  drag SESSION X1 Y1 X2 Y2
        press at X1,Y1, glide to X2,Y2, release
  key SESSION KEY...
        press keys, by xdotool's names: Return, Escape, ctrl+s, shift+a
  type SESSION 'text'
        type at human pace into whatever has the focus
  wait SESSION [--quiet 1] [--timeout 60]
        wait until the picture holds still for --quiet seconds
  say SESSION "sentence"
        end a beat, narrated by the sentence; refused until a shot shows the result
  cut SESSION
        end this take, holding its result, and start the next
  film SESSION on|off
        stop or resume filming; each on starts a new take
  stop SESSION OUTDIR
        stop filming, render each take into OUTDIR/take-N/, and write OUTDIR/scenes.yaml
  render SESSION OUTDIR
        render the takes again from the recording, even after stop

X and Y are pixels of what is filmed: read them off a shot. Start the app
yourself, on the display. Look, act, look: after an action, take a shot and
read it, then say what it shows. The sentence narrates everything since the
last say; it needs filming on. With -- docker exec CONTAINER, the display, the app, xdotool,
and ffmpeg live in the container, and the recording stays here.
`

// Main is movie desk.
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
		fmt.Fprintf(stderr, "movie desk %s: %v\n", verb, err)
		if code == exitcode.OK {
			code = exitcode.Usage
		}
	}
	return code
}

// needs parses args and loads the session, which is the first of want
// positional arguments; more is true when the rest may be longer.
func needs(fs *flag.FlagSet, args []string, want int, more bool, what string) (*Session, []string, error) {
	pos, err := cli.Parse(fs, args)
	if err != nil || len(pos) < want || (!more && len(pos) != want) {
		return nil, nil, fmt.Errorf("needs %s", what)
	}
	s, err := Load(pos[0])
	return s, pos[1:], err
}

func dispatch(verb string, args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("movie desk "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	switch verb {
	case "start":
		var wrapper []string
		if i := slices.Index(args, "--"); i >= 0 {
			args, wrapper = args[:i], args[i+1:]
		}
		display := fs.String("display", "", "the X display to film (default $DISPLAY)")
		window := fs.String("window", "", "film only the window with this name")
		title := fs.String("title", "", "title card for the scene file stop writes")
		subtitle := fs.String("subtitle", "", "subtitle for that title card")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 1 {
			return exitcode.Usage, fmt.Errorf("needs exactly one SESSION directory")
		}
		return exitcode.OK, Start(pos[0], StartOptions{Display: *display, Window: *window, Title: *title,
			Subtitle: *subtitle, Wrapper: wrapper}, stdout)
	case "shot":
		s, pos, err := needs(fs, args, 1, true, "SESSION, and at most a PNG path")
		if err != nil {
			return exitcode.Usage, err
		}
		if len(pos) > 1 {
			return exitcode.Usage, fmt.Errorf("needs SESSION, and at most a PNG path")
		}
		path := ""
		if len(pos) == 1 {
			path = pos[0]
		}
		return exitcode.OK, Shot(s, path, stdout)
	case "move":
		s, pos, err := needs(fs, args, 3, false, "SESSION X Y")
		if err != nil {
			return exitcode.Usage, err
		}
		return Move(s, pos[0], pos[1], stdout)
	case "click":
		right := fs.Bool("right", false, "click the right button")
		double := fs.Bool("double", false, "double-click")
		s, pos, err := needs(fs, args, 3, false, "SESSION X Y")
		if err != nil {
			return exitcode.Usage, err
		}
		return Click(s, pos[0], pos[1], *right, *double, stdout)
	case "drag":
		s, pos, err := needs(fs, args, 5, false, "SESSION X1 Y1 X2 Y2")
		if err != nil {
			return exitcode.Usage, err
		}
		return Drag(s, pos[0], pos[1], pos[2], pos[3], stdout)
	case "key":
		s, pos, err := needs(fs, args, 2, true, "SESSION and at least one KEY")
		if err != nil {
			return exitcode.Usage, err
		}
		return Key(s, pos, stdout)
	case "type":
		s, pos, err := needs(fs, args, 2, false, "SESSION and text")
		if err != nil {
			return exitcode.Usage, err
		}
		return Type(s, pos[0], stdout)
	case "wait":
		quiet := fs.Float64("quiet", 1, "seconds the picture must hold still")
		timeout := fs.Float64("timeout", 60, "seconds to wait")
		s, _, err := needs(fs, args, 1, false, "SESSION")
		if err != nil {
			return exitcode.Usage, err
		}
		return Wait(s, seconds(*quiet), seconds(*timeout), stdout)
	case "say":
		s, pos, err := needs(fs, args, 2, false, "SESSION and a sentence")
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Say(s, pos[0])
	case "cut":
		s, _, err := needs(fs, args, 1, false, "SESSION")
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, s.set().Cut()
	case "film":
		s, pos, err := needs(fs, args, 2, false, "SESSION and on or off")
		if err != nil {
			return exitcode.Usage, err
		}
		if pos[0] != "on" && pos[0] != "off" {
			return exitcode.Usage, fmt.Errorf("needs SESSION and on or off")
		}
		return exitcode.OK, s.set().SetFilm(pos[0] == "on")
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

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
