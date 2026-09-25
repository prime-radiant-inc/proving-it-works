// Package build turns a scene file into a finished, checked movie.
package build

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/prime-radiant-inc/proving-it-works/internal/check"
	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/narrate"
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
	clips, err := narrateAll(f, scratch, stdout)
	if err != nil {
		var rejected *narrate.RejectedError
		if errors.As(err, &rejected) {
			return exitcode.Verdict, err
		}
		return 0, err
	}
	// speechStarts is where each scene's narration starts in the cut: the
	// scene's offset, plus any narration_at: end delay.
	speechStarts := map[string]float64{}
	clock := 0.0
	var names []string
	for _, sc := range f.Scenes {
		c := clips[sc.ID]
		d, delay, err := segment(scratch, f, sc, c.wav, c.seconds)
		if err != nil {
			return 0, err
		}
		speechStarts[sc.ID] = clock + delay
		clock += d
		if c.wav != "" {
			fmt.Fprintf(stdout, "%s  %.1fs  narration %.1fs from %.1fs\n", sc.ID, d, c.seconds, delay)
		} else {
			fmt.Fprintf(stdout, "%s  %.1fs\n", sc.ID, d)
		}
		names = append(names, sc.ID+".mp4")
	}
	opts := check.Options{}
	srtPath := strings.TrimSuffix(out, filepath.Ext(out)) + ".srt"
	if !f.Narrated() {
		if err := concat(scratch, names, out); err != nil {
			return 0, err
		}
		// check reads <stem>.srt beside the movie: one left by an earlier
		// narrated build would describe a movie that no longer exists.
		if err := os.Remove(srtPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	} else {
		cut := filepath.Join(scratch, "cut.mp4")
		if err := concat(scratch, names, cut); err != nil {
			return 0, err
		}
		speechEnd, err := writeSubtitles(srtPath, f, clips, speechStarts)
		if err != nil {
			return 0, err
		}
		if _, err := burn(scratch, cut, srtPath, out, stdout); err != nil {
			return 0, err
		}
		opts = check.Options{ExpectAudio: true, ExpectSubtitles: true, SpeechEnd: &speechEnd}
	}
	fmt.Fprintf(stdout, "\nassembled %s (%.1fs)\n\n", out, clock)
	return check.Run(out, opts, stdout)
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
