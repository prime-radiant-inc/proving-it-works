// Command movie makes a movie that proves software works, and checks it.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/prime-radiant-inc/proving-it-works/internal/build"
	"github.com/prime-radiant-inc/proving-it-works/internal/check"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
	"github.com/prime-radiant-inc/proving-it-works/internal/term"
)

const usage = `movie: make a movie that proves software works, and check it.

  movie build SCENES.yaml OUT.mp4     narrate, assemble, subtitle, burn, check
  movie check MOVIE [--no-expect-audio] [--no-expect-subtitles]
  movie term VERB SESSION ...         film a terminal (run "movie term" for verbs)

Exit codes: 0 ok; 1 not shippable or failed; 2 usage or environment error;
3 (term) still running.
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitcode.Usage
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitcode.OK
	case "build":
		return build.Main(args[1:], stdout, stderr)
	case "check":
		return check.Main(args[1:], stdout, stderr)
	case "term":
		return term.Main(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "movie: unknown command %q\n\n%s", args[0], usage)
	return exitcode.Usage
}
