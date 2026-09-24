//go:build !windows

package term

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// spawnRecorder starts `movie term _record SESSION` in its own session, so it
// outlives this command and no harness has to keep a task alive for it.
func spawnRecorder(s *Session) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.Create(filepath.Join(s.Dir, "recorder.log"))
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(exe, "term", "_record", s.Dir)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
