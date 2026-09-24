package narrate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClipNameChangesWithEveryInput(t *testing.T) {
	base := clipName("piper", "en_US-lessac-medium", "en_US-lessac-medium", "hello")
	for _, other := range []string{
		clipName("openai", "en_US-lessac-medium", "en_US-lessac-medium", "hello"),
		clipName("piper", "other", "en_US-lessac-medium", "hello"),
		clipName("piper", "en_US-lessac-medium", "other", "hello"),
		clipName("piper", "en_US-lessac-medium", "en_US-lessac-medium", "hello!"),
	} {
		if other == base {
			t.Fatal("a changed input kept the same clip name")
		}
	}
	if base != clipName("piper", "en_US-lessac-medium", "en_US-lessac-medium", "hello") {
		t.Fatal("clip name is not stable")
	}
}

// scriptedEngine is an Engine whose attempts follow a script: each attempt
// either fails to synthesize (err) or writes a WAV and returns transcript.
type scriptedEngine struct {
	name     string
	attempts []scriptedAttempt
	n        int
}

type scriptedAttempt struct {
	transcript string
	err        error
}

func (e *scriptedEngine) Name() string         { return e.name }
func (e *scriptedEngine) Model(string) string  { return "test-model" }
func (e *scriptedEngine) DefaultVoice() string { return "test-voice" }
func (e *scriptedEngine) Ready(string) error   { return nil }
func (e *scriptedEngine) Synthesize(_, _, wav string) (string, error) {
	a := e.attempts[e.n]
	e.n++
	if a.err != nil {
		return "", a.err
	}
	return a.transcript, os.WriteFile(wav, []byte("RIFF"), 0o644)
}

func TestSynthesisFailuresAreNotRejections(t *testing.T) {
	e := &scriptedEngine{name: "openai", attempts: []scriptedAttempt{
		{err: errors.New("HTTP 500")}, {err: errors.New("HTTP 401: bad key")}}}
	_, err := Clip(t.TempDir(), e, "v", "hello there", io.Discard)
	var rejected *RejectedError
	if err == nil || errors.As(err, &rejected) {
		t.Fatalf("got %T %v, want a plain error", err, err)
	}
	if !strings.Contains(err.Error(), "HTTP 401: bad key") {
		t.Fatalf("error %q does not name the last cause", err)
	}
}

func TestAGateRejectionIsARejection(t *testing.T) {
	e := &scriptedEngine{name: "openai-chat", attempts: []scriptedAttempt{
		{transcript: "Sure, here it is: hello there"}, {err: errors.New("HTTP 500")}}}
	_, err := Clip(t.TempDir(), e, "v", "hello there", io.Discard)
	var rejected *RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("got %T %v, want *RejectedError", err, err)
	}
	if len(rejected.Reasons) != 2 || !strings.Contains(rejected.Reasons[0], "Sure, here it is") {
		t.Fatalf("reasons %q", rejected.Reasons)
	}
}

func TestAutoWithoutAKeyPicksPiper(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", t.TempDir()) // no llm on PATH
	e, err := Resolve("auto")
	if err != nil || e.Name() != "piper" || e.DefaultVoice() != "en_US-lessac-medium" {
		t.Fatalf("%v %v", e, err)
	}
}

func TestExplicitOpenAIWithoutAKeyIsAnError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", t.TempDir())
	if _, err := Resolve("openai-chat"); err == nil {
		t.Fatal("accepted openai-chat without a key")
	}
}

func TestAutoWithAKeyPicksOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	e, err := Resolve("auto")
	if err != nil || e.Name() != "openai" || e.DefaultVoice() != "nova" {
		t.Fatalf("%v %v", e, err)
	}
}

func TestResolvePiperNeverLooksUpAKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	llm := filepath.Join(dir, "llm")
	// PATH is about to be set to dir alone, so the script cannot rely on PATH
	// to find sleep either: it needs sleep's absolute path.
	if err := os.WriteFile(llm, []byte("#!/bin/sh\n/bin/sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	start := time.Now()
	e, err := Resolve("piper")
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("Resolve(\"piper\") took %v: it must never shell out for an OpenAI key", elapsed)
	}
	if err != nil || e.Name() != "piper" {
		t.Fatalf("%v %v", e, err)
	}
}
