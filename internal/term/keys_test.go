package term

import (
	"slices"
	"testing"
)

func TestKeyArgs(t *testing.T) {
	for name, want := range map[string][]string{
		"Enter": {"Enter"}, "Escape": {"Escape"}, "Tab": {"Tab"},
		"Up": {"Up"}, "Down": {"Down"}, "Left": {"Left"}, "Right": {"Right"},
		"C-c": {"C-c"}, "q": {"-H", "71"}, ";": {"-H", "3b"}, "λ": {"-H", "ce", "bb"},
	} {
		got, err := keyArgs(name)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("keyArgs(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, bad := range []string{"", "Ctrl-X", "ab"} {
		if _, err := keyArgs(bad); err == nil {
			t.Errorf("keyArgs(%q) accepted", bad)
		}
	}
}
