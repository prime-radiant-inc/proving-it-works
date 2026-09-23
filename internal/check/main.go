package check

import (
	"flag"
	"fmt"
	"io"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
)

// Main is `movie check`.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("movie check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noAudio := fs.Bool("no-expect-audio", false, "the movie is meant to be silent")
	noSubs := fs.Bool("no-expect-subtitles", false, "speech without subtitles is acceptable")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: movie check MOVIE [--no-expect-audio] [--no-expect-subtitles]")
	}
	pos, err := cli.Parse(fs, args)
	if err != nil {
		return exitcode.Usage
	}
	if len(pos) != 1 {
		fs.Usage()
		return exitcode.Usage
	}
	code, err := Run(pos[0], Options{ExpectAudio: !*noAudio, ExpectSubtitles: !*noSubs}, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "movie check: %v\n", err)
		return exitcode.Usage
	}
	return code
}
