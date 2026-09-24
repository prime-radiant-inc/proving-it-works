package narrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// RejectedError means every attempt at a clip failed its gate or synthesis.
type RejectedError struct{ Reasons []string }

func (e *RejectedError) Error() string {
	return "narration rejected:\n    " + strings.Join(e.Reasons, "\n    ")
}

// clipName identifies a clip by everything that shapes its audio.
func clipName(engine, voice, model, text string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{engine, voice, model, text}, "\x00")))
	return hex.EncodeToString(sum[:8]) + ".wav"
}

// Clip returns an accepted clip of text in dir, reusing one that exists. A
// clip file exists only once accepted: attempts are written to a temporary
// name and renamed into place after passing the gate.
func Clip(dir string, e Engine, voice, text string, log io.Writer) (string, error) {
	path := filepath.Join(dir, clipName(e.Name(), voice, e.Model(voice), text))
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(log, "  cached   %s\n", path)
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var reasons []string
	for attempt := 1; attempt <= 2; attempt++ {
		tmp, err := os.CreateTemp(dir, ".attempt-*.wav")
		if err != nil {
			return "", err
		}
		tmp.Close()
		transcript, err := e.Synthesize(text, voice, tmp.Name())
		if err == nil && e.Name() == "openai-chat" {
			if ok, why := ChatAccepts(text, transcript); !ok {
				err = fmt.Errorf("%s; it said: %q", why, transcript)
			}
		}
		if err != nil {
			os.Remove(tmp.Name())
			reasons = append(reasons, fmt.Sprintf("attempt %d: %v", attempt, err))
			continue
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			return "", err
		}
		fmt.Fprintf(log, "  rendered %s\n", path)
		return path, nil
	}
	return "", &RejectedError{Reasons: reasons}
}
