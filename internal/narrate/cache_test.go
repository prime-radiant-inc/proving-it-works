package narrate

import "testing"

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
