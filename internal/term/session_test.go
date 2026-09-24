package term

import (
	"slices"
	"strings"
	"testing"
)

func TestClaudeEnvNamesFindsEveryVariableStartingWithClaude(t *testing.T) {
	got := claudeEnvNames([]string{
		"CLAUDECODE=1",
		"CLAUDE_CODE_SESSION_ID=abc",
		"CLAUDE_CODE_MESSAGING_TOKEN=xyz",
		"CLAUDE_PID=123",
		"CLAUDE_EFFORT=high",
		"PATH=/bin",
		"HOME=/root",
	})
	want := []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_PID", "CLAUDE_EFFORT"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestShellCommandSetsAUTF8LocaleAndScrubsGivenVariables(t *testing.T) {
	cmd := shellCommand("/tmp/hist", []string{"CLAUDE_CODE_TEST_TOKEN", "CLAUDE_PID"})
	for _, want := range []string{"LANG=C.UTF-8", "-u LC_ALL", "-u LC_CTYPE", "-u CLAUDE_CODE_TEST_TOKEN", "-u CLAUDE_PID"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("shellCommand missing %q:\n%s", want, cmd)
		}
	}
}
