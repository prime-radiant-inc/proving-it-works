package jsonl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type line struct {
	T float64 `json:"t"`
}

func TestAppendThenRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.jsonl")
	if got, err := Read[line](path); err != nil || got != nil {
		t.Fatalf("a missing file: %v %v", got, err)
	}
	Append(path, line{1})
	Append(path, line{2})
	got, err := Read[line](path)
	if err != nil || len(got) != 2 || got[1].T != 2 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestReadDropsOnlyAPartialLastLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.jsonl")
	os.WriteFile(path, []byte(`{"t":0}`+"\n"+`{"t":0.1,"fi`), 0o644)
	if got, err := Read[line](path); err != nil || len(got) != 1 {
		t.Fatalf("got %v %v", got, err)
	}
	os.WriteFile(path, []byte(`{"t":0}`+"\n"+`not json`+"\n"+`{"t":0.2}`), 0o644)
	if _, err := Read[line](path); err == nil || !strings.Contains(err.Error(), "x.jsonl line 2") {
		t.Fatalf("got %v, want an error naming line 2", err)
	}
}
