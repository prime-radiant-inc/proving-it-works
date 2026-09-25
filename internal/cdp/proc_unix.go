//go:build !windows

package cdp

import (
	"os/exec"
	"syscall"
)

// detach starts the browser in a session of its own, so it outlives the
// command that launched it and Kill can end all its processes at once.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// Kill ends the browser and every process it started.
func (b *Browser) Kill() { syscall.Kill(-b.PID, syscall.SIGKILL) }

// Alive reports whether the browser's process still exists.
func (b *Browser) Alive() bool { return syscall.Kill(b.PID, 0) == nil }
