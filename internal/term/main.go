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

  start SESSION [--cwd DIR] [--size 120x34] [-- WRAPPER...]
  run SESSION 'cmd' [--timeout 60]
  stop SESSION OUTDIR [--px 1600x900]
  render SESSION OUTDIR [--px 1600x900]
`

// Main is `movie term`.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitcode.Usage
	}
	verb, rest := args[0], args[1:]
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
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 1 {
			return exitcode.Usage, fmt.Errorf("needs exactly one SESSION directory")
		}
		cols, rows, err := cli.ParseSize(*size)
		if err != nil {
			return exitcode.Usage, err
		}
		return exitcode.OK, Start(pos[0], StartOptions{Cwd: *cwd, Cols: cols, Rows: rows, Wrapper: wrapper}, stdout)
	case "run":
		timeout := fs.Float64("timeout", 60, "seconds to wait for the next prompt")
		pos, err := cli.Parse(fs, args)
		if err != nil || len(pos) != 2 {
			return exitcode.Usage, fmt.Errorf("needs SESSION and a command")
		}
		s, err := Load(pos[0])
		if err != nil {
			return exitcode.Usage, err
		}
		return RunCommand(s, pos[1], seconds(*timeout), stdout)
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
		return exitcode.OK, Record(args[0])
	}
	fmt.Fprint(stderr, usage)
	return exitcode.Usage, fmt.Errorf("unknown verb %q", verb)
}

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
