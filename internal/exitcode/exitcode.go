// Package exitcode names the exit codes every movie command shares.
package exitcode

const (
	OK      = 0 // success
	Verdict = 1 // negative verdict: not shippable, narration rejected, command failed
	Usage   = 2 // usage or environment error
	Running = 3 // term only: the command is still running
)
