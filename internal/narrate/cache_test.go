package narrate

import (
	"os"
	"path/filepath"
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
