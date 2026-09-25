package cdp

import (
	"os"
	"os/exec"
)

func detach(cmd *exec.Cmd) {}

// Kill ends the browser.
func (b *Browser) Kill() {
	if p, err := os.FindProcess(b.PID); err == nil {
		p.Kill()
	}
}

// Alive is always false on Windows, where browse does not run.
func (b *Browser) Alive() bool { return false }
