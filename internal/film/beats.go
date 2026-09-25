package film

import (
	"path/filepath"
	"strings"

	"github.com/prime-radiant-inc/proving-it-works/internal/jsonl"
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
	return jsonl.Append(filepath.Join(dir, "beats.jsonl"), b)
}

// ReadBeats reads dir/beats.jsonl; a session nobody narrated has none.
func ReadBeats(dir string) ([]Beat, error) {
	return jsonl.Read[Beat](filepath.Join(dir, "beats.jsonl"))
}
