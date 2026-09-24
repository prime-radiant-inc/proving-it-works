package term

import (
	"fmt"
	"unicode/utf8"
)

// namedKeys are passed to tmux by name, so tmux encodes them for the pane's
// current mode (arrows follow application cursor mode, as a real terminal's do).
var namedKeys = map[string]bool{
	"Enter": true, "Escape": true, "Tab": true,
	"Up": true, "Down": true, "Left": true, "Right": true, "C-c": true,
}

// keyArgs is the send-keys arguments for one key: a named key, or one
// character sent as raw bytes.
func keyArgs(name string) ([]string, error) {
	if namedKeys[name] {
		return []string{name}, nil
	}
	if utf8.RuneCountInString(name) == 1 {
		args := []string{"-H"}
		for _, b := range []byte(name) {
			args = append(args, fmt.Sprintf("%02x", b))
		}
		return args, nil
	}
	return nil, fmt.Errorf("unknown key %q: use Enter, Escape, Tab, Up, Down, Left, Right, C-c, or one character", name)
}
