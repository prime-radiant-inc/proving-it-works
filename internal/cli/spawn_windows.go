//go:build windows

package cli

import "errors"

// SpawnDetached is unsupported on Windows; the recorders need macOS, Linux,
// or WSL.
func SpawnDetached(log string, args ...string) error {
	return errors.New("recording needs macOS, Linux, or WSL")
}
