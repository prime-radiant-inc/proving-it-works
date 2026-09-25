package cli

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseInterleavesFlagsAndPositionals(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	quiet := fs.Bool("quiet", false, "")
	got, err := Parse(fs, []string{"a.mp4", "--quiet", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"a.mp4", "b"}) || !*quiet {
		t.Fatalf("got %q quiet=%v", got, *quiet)
	}
}

func TestParseKeepsEverythingAfterDoubleDash(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	quiet := fs.Bool("quiet", false, "")
	got, err := Parse(fs, []string{"s", "--", "docker", "exec", "--quiet"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"s", "docker", "exec", "--quiet"}) || *quiet {
		t.Fatalf("got %q quiet=%v", got, *quiet)
	}
}

func TestParseSize(t *testing.T) {
	w, h, err := ParseSize("1920x1080")
	if err != nil || w != 1920 || h != 1080 {
		t.Fatalf("got %d %d %v", w, h, err)
	}
	for _, bad := range []string{"1920", "0x10", "ax10", "10x-1", ""} {
		if _, _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) accepted", bad)
		}
	}
}

func TestRequireEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if err := RequireEmptyDir(filepath.Join(dir, "new")); err != nil {
		t.Errorf("a missing directory is fine: %v", err)
	}
	if err := RequireEmptyDir(dir); err != nil {
		t.Errorf("an empty directory is fine: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "x"), nil, 0o644)
	if err := RequireEmptyDir(dir); err == nil || !strings.Contains(err.Error(), "is not empty: use a new directory") {
		t.Errorf("a directory in use: got %v", err)
	}
}
