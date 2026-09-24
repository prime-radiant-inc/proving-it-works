package narrate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Engine synthesizes text to a WAV file. A chat engine also returns what it
// says it said.
type Engine interface {
	Name() string
	Model(voice string) string
	DefaultVoice() string
	// Ready reports a missing prerequisite before any work starts.
	Ready(voice string) error
	Synthesize(text, voice, wav string) (transcript string, err error)
}

// Resolve picks the engine: auto means openai when a key exists, else piper.
func Resolve(name string) (Engine, error) {
	key := openAIKey()
	switch name {
	case "auto":
		if key != "" {
			return openAI{key: key}, nil
		}
		return piper{}, nil
	case "openai", "openai-chat":
		if key == "" {
			return nil, errors.New("no OPENAI_API_KEY (and `llm keys get openai` found nothing); " +
				"use engine: piper for a local voice")
		}
		return openAI{key: key, chat: name == "openai-chat"}, nil
	case "piper":
		return piper{}, nil
	}
	return nil, errors.New("unknown engine " + name)
}

// openAIKey reads OPENAI_API_KEY, else asks the llm CLI, whose absence is normal.
func openAIKey() string {
	if k := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); k != "" {
		return k
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "llm", "keys", "get", "openai").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
