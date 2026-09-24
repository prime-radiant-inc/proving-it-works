package narrate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// piper is the local voice: free, offline, and the default without a key.
type piper struct{}

func (piper) Name() string              { return "piper" }
func (piper) Model(voice string) string { return voice }
func (piper) DefaultVoice() string      { return "en_US-lessac-medium" }

// voiceDir is where Piper voices live: PIPER_VOICE_DIR, else ~/.cache/piper-voices.
func voiceDir() string {
	if d := os.Getenv("PIPER_VOICE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "piper-voices")
}

func (piper) Ready(voice string) error {
	if _, err := exec.LookPath("piper"); err != nil {
		return fmt.Errorf("piper not on PATH: install it once with: uv tool install piper-tts")
	}
	dir := voiceDir()
	if _, err := os.Stat(filepath.Join(dir, voice+".onnx")); err != nil {
		return fmt.Errorf("piper voice %s is not in %s: download it once with: "+
			"uvx --from piper-tts python -m piper.download_voices --data-dir %q %s", voice, dir, dir, voice)
	}
	return nil
}

// Synthesize passes the text in a UTF-8 file: as arguments, words that look
// like flags would be misparsed, and stdin is decoded in the ANSI code page
// on Windows.
func (piper) Synthesize(text, voice, wav string) (string, error) {
	input := wav + ".txt"
	if err := os.WriteFile(input, []byte(text), 0o644); err != nil {
		return "", err
	}
	defer os.Remove(input)
	out, err := exec.Command("piper", "-m", voice, "--data-dir", voiceDir(), "-i", input, "-f", wav).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("piper: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return "", nil
}
