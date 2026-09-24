package narrate

import (
	"fmt"
	"strings"
	"testing"
)

const script = "This is a container with nothing of ours in it. Claude Code is here, " +
	"and no plugins. We add the marketplace straight from the public repo."

func words50() []string {
	w := make([]string, 50)
	for i := range w {
		w[i] = fmt.Sprintf("word%d", i)
	}
	return w
}

func TestChatGate(t *testing.T) {
	w := words50()
	oneWrong := append([]string{}, w...)
	oneWrong[10] = "other"
	for _, c := range []struct {
		name, script, transcript string
		ok                       bool
	}{
		{"exact", script, script, true},
		{"case and punctuation differ", "Two words.", "two words", true},
		{"one mismatched word in fifty", strings.Join(w, " "), strings.Join(oneWrong, " "), true},
		{"accented Latin and Cyrillic across lines", "Café déjà vu\nПривет мир", "café déjà vu привет мир", true},
		{"spoken preamble", script, "Sure thing: " + script, false},
		{"one-word preamble", script, "Okay. " + script, false},
		{"dropped clause", script, strings.Replace(script, " and no plugins.", "", 1), false},
		{"empty transcript", script, "", false},
		{"punctuation only", script, "...!?", false},
		{"script without word boundaries", "漢字のテスト", "漢字のテスト", false},
	} {
		ok, why := ChatAccepts(c.script, c.transcript)
		if ok != c.ok {
			t.Errorf("%s: got %v (%s), want %v", c.name, ok, why, c.ok)
		}
		if !ok && why == "" {
			t.Errorf("%s: rejected without a reason", c.name)
		}
	}
}
