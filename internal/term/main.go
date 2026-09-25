package term

import (
	"flag"
	"fmt"
	"image"
	"io"
	"slices"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

const usage = `usage: movie term VERB SESSION ...

  start SESSION [--cwd DIR] [--size 120x34] [--title T] [--subtitle S] [-- WRAPPER...]
        start a clean bash in a private tmux server, filming; returns when ready
  run SESSION 'cmd' [--say "narration"] [--timeout 60]
        type a command at a prompt and wait for it; prints its exit code and output;
        --say starts a new beat, narrated by that sentence
  type SESSION 'text'
        type into whatever is running, such as a TUI's input box
  key SESSION Enter|Escape|Tab|Up|Down|Left|Right|C-c|<one character>
        press one key
  wait SESSION [--quiet S] [--timeout 60]
        wait for the prompt, or for the screen to hold still for S seconds
  screen SESSION
        print the screen as text
  cut SESSION
        end this take, holding its result, and start the next
  film SESSION on|off
        stop or resume filming; each on starts a new take
  stop SESSION OUTDIR [--px 1600x900]
        end the session, render each take into OUTDIR/take-N/, and write OUTDIR/scenes.yaml
  render SESSION OUTDIR [--px 1600x900]
        render the takes again from the recording, even after stop
`

// Main is `movie term`.
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
		fmt.Fprintf(stderr, "movie term %s: %v\n", verb, err)
		if code == exitcode.OK {
			code = exitcode.Usage
		}
	}
	return code
}

func dispatch(verb string, args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("movie term "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	switch verb {
	case "start":
		var wrapper []string
		if i := slices.Index(args, "--"); i >= 0 {
			args, wrapper = args[:i], args[i+1:]
		}
		cwd := fs.String("cwd", "", "starting directory (inside the container when wrapped)")
		size := fs.String("size", "120x34", "terminal columns x rows")
		title := fs.String("title", "", "title card for the scene file stop writes")
		subtitle := fs.String("subtitle", "", "subtitle for that title card")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 1 {
			return exitcode.Usage, fmt.Errorf("needs exactly one SESSION directory")
		}
		cols, rows, err := cli.ParseSize(*size)
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Start(pos[0], StartOptions{Cwd: *cwd, Cols: cols, Rows: rows, Wrapper: wrapper,
			Title: *title, Subtitle: *subtitle}, stdout)
	case "run":
		timeout := fs.Float64("timeout", 60, "seconds to wait for the next prompt")
		say := fs.String("say", "", "narration for a new beat that starts with this command")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 2 {
			return exitcode.Usage, fmt.Errorf("needs SESSION and a command")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return RunCommand(s, pos[1], *say, seconds(*timeout), stdout)
	case "type", "key", "film":
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 2 {
			return exitcode.Usage, fmt.Errorf("needs SESSION and one argument")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		switch verb {
		case "type":
			return exitcode.OK, TypeText(s, pos[1])
		case "key":
			return exitcode.OK, PressKey(s, pos[1])
		}
		if pos[1] != "on" && pos[1] != "off" {
			return exitcode.Usage, fmt.Errorf("film takes on or off")
		}
		return exitcode.OK, SetFilm(s, pos[1] == "on")
	case "cut":
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 1 {
			return exitcode.Usage, fmt.Errorf("needs SESSION")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Cut(s)
	case "wait":
		timeout := fs.Float64("timeout", 60, "seconds to wait")
		quiet := fs.Float64("quiet", 0, "return once the screen is unchanged this many seconds")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 1 {
			return exitcode.Usage, fmt.Errorf("needs SESSION")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return Wait(s, seconds(*timeout), seconds(*quiet), stdout)
	case "screen":
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 1 {
			return exitcode.Usage, fmt.Errorf("needs SESSION")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Screen(s, stdout)
	case "stop", "render":
		px := fs.String("px", "1600x900", "frame size in pixels")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 2 {
			return exitcode.Usage, fmt.Errorf("needs SESSION and OUTDIR")
		}
		w, h, err := cli.ParseSize(*px)
		if err != nil {
			return exitcode.Usage, err
		}
		if verb == "render" {
			return exitcode.OK, Render(pos[0], pos[1], image.Pt(w, h), stdout)
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Stop(s, pos[1], image.Pt(w, h), stdout)
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
