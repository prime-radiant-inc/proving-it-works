package check

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
)

// WorkDir is where check writes its evidence: <movie stem>-check beside it.
func WorkDir(movie string) string {
	return strings.TrimSuffix(movie, filepath.Ext(movie)) + "-check"
}

// Run checks movie, prints the verdict, and writes check.json and the contact
// sheet. It returns exitcode.OK or exitcode.Verdict; an error means the movie
// could not be examined at all.
func Run(movie string, o Options, stdout io.Writer) (int, error) {
	movie, err := filepath.Abs(movie)
	if err != nil {
		return 0, err
	}
	if _, err := os.Stat(movie); err != nil {
		return 0, fmt.Errorf("no such movie: %s", movie)
	}
	if err := ffmpeg.Require(); err != nil {
		return 0, err
	}
	info, err := ffmpeg.Probe(movie)
	if err != nil {
		return 0, err
	}
	video, ok := info.First("video")
	if !ok {
		return 0, errors.New("no video stream")
	}
	workdir := WorkDir(movie)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return 0, err
	}
	paths, changes, err := samplePicture(movie, workdir)
	if err != nil {
		return 0, err
	}
	m := Measurements{Duration: info.Duration, HasAudio: info.Has("audio"), Changes: changes}
	if m.HasAudio {
		if m.Levels, err = sampleSound(movie); err != nil {
			return 0, err
		}
	}
	var subs Subtitles
	if NeedsSubtitles(m, o) {
		if subs, err = findSubtitles(movie, info); err != nil {
			return 0, err
		}
	}
	r := Evaluate(m, subs, o)

	fmt.Fprintf(stdout, "container  %s %dx%d, %.1fs, audio=%s\n",
		video.CodecName, video.Width, video.Height, info.Duration, yesNo(m.HasAudio))
	fmt.Fprintf(stdout, "picture    reaches a new state in %d of %d seconds%s\n",
		len(r.ChangeSeconds), max(len(changes), 1), lastAt(r.ChangeSeconds))
	if len(m.Levels) > 0 {
		fmt.Fprintf(stdout, "sound      audible in %d of %d seconds%s\n",
			len(r.TalkSeconds), len(m.Levels), lastAt(r.TalkSeconds))
	}
	if r.SubtitleNote != "" {
		fmt.Fprintf(stdout, "subtitles  %s\n", r.SubtitleNote)
	}
	sheet := filepath.Join(workdir, "contact-sheet.png")
	picks, err := contactSheet(paths, sheet)
	if err != nil {
		return 0, err
	}
	shown := make([]string, len(picks))
	for i, p := range picks {
		shown[i] = fmt.Sprintf("%ds", p)
	}
	fmt.Fprintf(stdout, "sheet      %s\n           sampled at %s\n", sheet, strings.Join(shown, ", "))
	return finish(r, workdir, stdout)
}

func lastAt(seconds []int) string {
	if len(seconds) == 0 {
		return ""
	}
	return fmt.Sprintf("; last at %ds", seconds[len(seconds)-1])
}

// finish prints warnings and failures, writes check.json, and picks the exit code.
func finish(r Report, workdir string, stdout io.Writer) (int, error) {
	for _, w := range r.Warnings {
		fmt.Fprintf(stdout, "WARN       %s\n", w)
	}
	for _, f := range r.Failures {
		fmt.Fprintf(stdout, "FAIL       %s\n", f)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(workdir, "check.json"), data, 0o644); err != nil {
		return 0, err
	}
	if len(r.Failures) > 0 {
		fmt.Fprintln(stdout, "\nNOT SHIPPABLE. Fix, regenerate, re-run.")
		return exitcode.Verdict, nil
	}
	fmt.Fprintln(stdout, "\nMechanical checks pass. NOW OPEN THE CONTACT SHEET AND LOOK AT IT: "+
		"this cannot see wrong content, unreadable text, a missing cursor, or "+
		"narration that says something the picture contradicts.")
	return exitcode.OK, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
