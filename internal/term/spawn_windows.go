//go:build windows

package term

import "errors"

func spawnRecorder(*Session) error { return errors.New("movie term needs macOS, Linux, or WSL") }
