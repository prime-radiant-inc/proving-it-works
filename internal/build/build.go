// Package build turns a scene file into a finished, checked movie.
package build

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/prime-radiant-inc/proving-it-works/internal/check"
	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/scene"
)

// Run builds out from the scene file and checks it.
func Run(scenePath, out string, stdout io.Writer) (int, error) {
	if err := ffmpeg.Require(); err != nil {
		return 0, err
	}
	f, err := scene.Load(scenePath)
	if err != nil {
		return 0, err
	}
	if out, err = filepath.Abs(out); err != nil {
		return 0, err
	}
	scratch := strings.TrimSuffix(out, filepath.Ext(out)) + ".build"
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return 0, err
	}
	var names []string
	for _, sc := range f.Scenes {
		d, err := segment(scratch, f, sc, "", 0)
		if err != nil {
			return 0, err
		}
		fmt.Fprintf(stdout, "%s: %.1fs\n", sc.ID, d)
		names = append(names, sc.ID+".mp4")
	}
	if err := concat(scratch, names, out); err != nil {
		return 0, err
	}
	fmt.Fprintf(stdout, "\nassembled %s\n\n", out)
	return check.Run(out, check.Options{}, stdout)
}

// concat joins segments by their fixed safe names, running from scratch so
// nothing in the list needs escaping.
func concat(scratch string, names []string, out string) error {
	var list strings.Builder
	for _, n := range names {
		fmt.Fprintf(&list, "file '%s'\n", n)
	}
	if err := os.WriteFile(filepath.Join(scratch, "concat.txt"), []byte(list.String()), 0o644); err != nil {
		return err
	}
	return ffmpeg.Run(scratch, nil, "-f", "concat", "-safe", "0", "-i", "concat.txt", "-c", "copy", out)
}
