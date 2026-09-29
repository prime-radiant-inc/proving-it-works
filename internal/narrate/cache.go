package narrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RejectedError means every attempt at a clip failed, and at least one was
// rejected by the openai-chat gate: the voice said something other than the
// script. Attempts that all failed to synthesize are a plain error instead.
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
// name and renamed into place after passing the gate. Files an engine writes
// beside an attempt, such as the MP3 polly keeps, share its stem and follow
// it: renamed with the clip, removed with a failed attempt.
func Clip(dir string, e Engine, voice, text string) (path string, rendered bool, err error) {
	path = filepath.Join(dir, clipName(e.Name(), voice, e.Model(voice), text))
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	var reasons []string
	var lastErr error
	gated := false
	for attempt := 1; attempt <= 2; attempt++ {
		tmp, err := os.CreateTemp(dir, ".attempt-*.wav")
		if err != nil {
			return "", false, err
		}
		tmp.Close()
		transcript, err := e.Synthesize(text, voice, tmp.Name())
		if err == nil && e.Name() == "openai-chat" {
			if ok, why := ChatAccepts(text, transcript); !ok {
				err = fmt.Errorf("%s; it said: %q", why, transcript)
				gated = true
			}
		}
		stem := strings.TrimSuffix(tmp.Name(), ".wav")
		sidecars, _ := filepath.Glob(stem + ".*")
		if err != nil {
			for _, f := range sidecars {
				os.Remove(f)
			}
			reasons = append(reasons, fmt.Sprintf("attempt %d: %v", attempt, err))
			lastErr = err
			continue
		}
		for _, f := range sidecars {
			if f == tmp.Name() {
				continue
			}
			if err := os.Rename(f, strings.TrimSuffix(path, ".wav")+strings.TrimPrefix(f, stem)); err != nil {
				return "", false, err
			}
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			return "", false, err
		}
		return path, true, nil
	}
	if !gated {
		return "", false, fmt.Errorf("narration failed to synthesize: %w", lastErr)
	}
	return "", false, &RejectedError{Reasons: reasons}
}
