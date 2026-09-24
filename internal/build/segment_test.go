package build

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamFilesMissingFileCausesErrorOnClose(t *testing.T) {
	fs := streamFiles([]string{"/definitely/missing.png"})
	// Read from the stream; the goroutine will try to open the file.
	// The error is recorded in the goroutine and only surfaces on Close().
	io.ReadAll(fs)
	cerr := fs.Close()
	if cerr == nil {
		t.Fatal("Close() should have returned an error for missing file")
	}
	if !strings.Contains(cerr.Error(), "missing.png") {
		t.Fatalf("error should mention 'missing.png', got: %v", cerr)
	}
}

func TestStreamFilesConcatenatesAndClosesSuccessfully(t *testing.T) {
	dir := t.TempDir()
	file1 := filepath.Join(dir, "file1.txt")
	file2 := filepath.Join(dir, "file2.txt")
	if err := os.WriteFile(file1, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := streamFiles([]string{file1, file2})
	data, err := io.ReadAll(fs)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "helloworld" {
		t.Fatalf("expected 'helloworld', got %q", string(data))
	}
	if err := fs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
