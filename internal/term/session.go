// Package term films a terminal: tmux holds a clean bash, the verbs drive it,
// a background recorder snapshots the screen, and render draws the frames.
package term

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// window is the tmux session name inside the private server.
const window = "movie"

// Session is one filmed terminal: a private tmux server holding a clean bash.
type Session struct {
	Dir     string   `json:"-"`
	Socket  string   `json:"socket"`
	History string   `json:"history"`
	Wrapper []string `json:"wrapper,omitempty"`
}

// newSession names a session's socket and history. The socket lives in /tmp
// because Unix socket paths are limited to about 100 bytes; it is named by a
// hash of the session directory, so each session gets its own server.
func newSession(dir string, wrapper []string) (*Session, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(abs))
	id := hex.EncodeToString(sum[:6])
	s := &Session{Dir: abs, Socket: "/tmp/movie-" + id + ".sock", Wrapper: wrapper}
	s.History = filepath.Join(abs, "history")
	if len(wrapper) > 0 {
		s.History = "/tmp/movie-" + id + ".history"
	}
	return s, nil
}

// Load reads a running session's description.
func Load(dir string) (*Session, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(abs, "session.json"))
	if err != nil {
		return nil, fmt.Errorf("no movie term session at %s (start one with: movie term start %s)", dir, dir)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s/session.json: %w", dir, err)
	}
	s.Dir = abs
	return &s, nil
}

func (s *Session) save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.Dir, "session.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, "session.json"))
}

// tmux runs a tmux command against this session's private server, through
// the wrapper when there is one. -u: the terminal is UTF-8; -f /dev/null:
// never read the user's tmux.conf.
func (s *Session) tmux(args ...string) (string, error) {
	argv := append(append([]string{}, s.Wrapper...), "tmux", "-u", "-S", s.Socket, "-f", "/dev/null")
	argv = append(argv, args...)
	cmd := exec.Command(argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// quote makes s one word for sh.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// prompt reports each prompt through the pane title, which tmux exposes as
// #{pane_title} and the renderer never draws. It captures $? first, then
// unexports itself so a nested bash does not report prompts of its own.
const prompt = `s=$?; export -n PROMPT_COMMAND; MOVIE_N=$((MOVIE_N+1)); printf '\033]2;MOVIE;%s;%s\007' "$MOVIE_N" "$s"`

// claudeEnvNames returns, from environ (as os.Environ returns it), the names
// of every variable whose name starts with CLAUDE. These are the invoking
// agent's own variables (CLAUDECODE, CLAUDE_CODE_*, CLAUDE_PID, ...); the
// filmed shell must never inherit them, or a nested Claude Code warns about
// them on camera, or a filmed `env` prints a live token.
func claudeEnvNames(environ []string) []string {
	var names []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "CLAUDE") {
			names = append(names, name)
		}
	}
	return names
}

// shellCommand starts a clean bash: no startup files, its own history file,
// no macOS "default shell is now zsh" banner, a UTF-8 locale (the default C
// locale corrupts typed multibyte text), and none of the variables that leak
// the invoking terminal, the Git Bash launcher, or the invoking agent
// (scrub) into the take.
func shellCommand(history string, scrub []string) string {
	parts := []string{
		"exec env",
		"-u TERM_PROGRAM -u TERM_PROGRAM_VERSION -u TERM_SESSION_ID -u TMUX",
		"-u MSYS_NO_PATHCONV -u MSYS2_ARG_CONV_EXCL",
		"-u LC_ALL -u LC_CTYPE",
	}
	for _, name := range scrub {
		parts = append(parts, "-u "+name)
	}
	parts = append(parts,
		"LANG=C.UTF-8",
		"TERM=xterm-256color",
		"BASH_SILENCE_DEPRECATION_WARNING=1",
		"HISTFILE="+quote(history),
		"PS1="+quote(`\w \$ `),
		"PROMPT_COMMAND="+quote(prompt),
		"bash --noprofile --norc -i",
	)
	return strings.Join(parts, " ")
}
