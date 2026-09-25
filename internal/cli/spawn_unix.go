//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

// SpawnDetached runs this movie executable with args in a session of its
// own, output to log, so it outlives the command that started it and no
// harness has to keep a task alive for it. Recorders run this way.
func SpawnDetached(log string, args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	f, err := os.Create(log)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = f, f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
