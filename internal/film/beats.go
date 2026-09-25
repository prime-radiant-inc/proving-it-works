package film

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Beat is one narrated beat: the sentence given to --say, and the number of
// the take it was said in (takes count from 1, one per stretch of filming).
type Beat struct {
	Take int    `json:"take"`
	Say  string `json:"say"`
}

// Narrations gives each of n takes the sentences said while it was being
// filmed, joined in order; a take nobody narrated gets "".
func Narrations(n int, beats []Beat) []string {
	says := make([]string, n)
	for _, b := range beats {
		if b.Take >= 1 && b.Take <= n {
			says[b.Take-1] = strings.TrimSpace(says[b.Take-1] + " " + b.Say)
		}
	}
	return says
}

// AppendBeat records b in dir/beats.jsonl.
func AppendBeat(dir string, b Beat) error {
	f, err := os.OpenFile(filepath.Join(dir, "beats.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(b)
}

// ReadBeats reads dir/beats.jsonl; a session nobody narrated has none.
func ReadBeats(dir string) ([]Beat, error) {
	data, err := os.ReadFile(filepath.Join(dir, "beats.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var beats []Beat
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var b Beat
		if err := json.Unmarshal([]byte(line), &b); err != nil {
			return nil, fmt.Errorf("beats.jsonl line %d: %w", i+1, err)
		}
		beats = append(beats, b)
	}
	return beats, nil
}
