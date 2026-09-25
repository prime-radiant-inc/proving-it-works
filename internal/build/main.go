package build

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

// Main is `movie build`.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("movie build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, "usage: movie build SCENES.yaml OUT.mp4") }
	pos, err := cli.Parse(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return exitcode.OK // asking for help is not a mistake
	}
	if err != nil {
		return exitcode.Usage
	}
	if len(pos) != 2 {
		fs.Usage()
		return exitcode.Usage
	}
	code, err := Run(pos[0], pos[1], stdout)
	if err != nil {
		fmt.Fprintf(stderr, "movie build: %v\n", err)
		if code == exitcode.OK {
			code = exitcode.Usage
		}
	}
	return code
}
