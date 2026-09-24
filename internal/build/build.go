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
	var names []string
	for _, sc := range f.Scenes {
		if sc.Kind != scene.Image {
			return 0, fmt.Errorf("scene %s: %s scenes arrive in the next task", sc.ID, sc.Kind)
		}
		name := sc.ID + ".mp4"
		pngs := streamFiles([]string{sc.Source})
		err := encodeStill(scratch, name, f, pngs, float64(f.FPS), 1, sc.Duration, "")
		if cerr := pngs.Close(); cerr != nil {
			err = cerr
		}
		if err != nil {
			return 0, fmt.Errorf("scene %s: %w", sc.ID, err)
		}
		info, err := ffmpeg.Probe(filepath.Join(scratch, name))
		if err != nil {
			return 0, err
		}
		fmt.Fprintf(stdout, "%s: %.1fs\n", sc.ID, info.Duration)
		names = append(names, name)
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
