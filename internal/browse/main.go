package browse

import (
	"flag"
	"fmt"
	"io"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

const usage = `usage: movie browse VERB SESSION ...

  start SESSION URL [--size 1280x720] [--title T] [--subtitle S] [--browser PATH]
        launch a headless Chrome on URL, filming; returns when the page has settled
  stop SESSION OUTDIR
        close the browser, render each take into OUTDIR/take-N/, and write OUTDIR/scenes.yaml
  render SESSION OUTDIR
        render the takes again from the recording, even after stop
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
