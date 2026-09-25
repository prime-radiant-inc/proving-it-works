// Package jsonl reads and appends the JSON-lines files recorders and verbs
// keep in a session directory.
package jsonl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Read decodes each line of a JSON-lines file into a T; a missing file
// has none. A process killed mid-write leaves a partial last line, which is
// dropped; a bad line anywhere else is an error naming it.
func Read[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	var out []T
	for i, line := range lines {
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			if i == len(lines)-1 {
				break
			}
			return nil, fmt.Errorf("%s line %d: %w", filepath.Base(path), i+1, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// Append appends v to a JSON-lines file.
func Append(path string, v any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(v)
}
