// Package cli holds the argument parsing every movie subcommand shares.
package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Parse parses flags that may appear before, between, or after positional
// arguments and returns the positional arguments in order. A "--" ends flag
// parsing: everything after it is positional.
func Parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if len(args) > len(rest) && args[len(args)-len(rest)-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// ParseSize reads a size written WxH, such as 1920x1080 or 120x34.
func ParseSize(s string) (int, int, error) {
	w, h, ok := strings.Cut(s, "x")
	if !ok {
		return 0, 0, fmt.Errorf("size %q is not WxH", s)
	}
	width, err1 := strconv.Atoi(w)
	height, err2 := strconv.Atoi(h)
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("size %q is not two positive whole numbers", s)
	}
	return width, height, nil
}

// ShortPath shows path relative to the current directory when that is
// shorter, as the person or agent reading the output would type it.
func ShortPath(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(wd, path); err == nil && len(rel) < len(path) {
		return rel
	}
	return path
}

// RequireEmptyDir refuses a directory that exists and holds anything, so a
// new session or take never mixes with an old one.
func RequireEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty: use a new directory", dir)
	}
	return nil
}
