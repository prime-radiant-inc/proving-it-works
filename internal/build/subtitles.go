package build

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/fonts"
	"github.com/prime-radiant-inc/proving-it-works/internal/scene"
	"github.com/prime-radiant-inc/proving-it-works/internal/srt"
)

// Readability limits: two comfortable lines, and a cue on screen long
// enough to read but not so long it goes stale.
const (
	maxCueChars = 84
	maxCueSecs  = 5.5
)

// writeSubtitles writes cues for every narrated scene, each spanning its
// narration clip from where that narration starts in the cut, and returns
// where the last narration ends.
func writeSubtitles(path string, f *scene.File, clips map[string]clip, starts map[string]float64) (float64, error) {
	var cues []srt.Cue
	speechEnd := 0.0
	for _, sc := range f.Scenes {
		c, ok := clips[sc.ID]
		if !ok {
			continue
		}
		sceneCues, err := srt.SceneCues(sc.Narration, starts[sc.ID], c.seconds, maxCueChars, maxCueSecs)
		if err != nil {
			return 0, fmt.Errorf("scene %s subtitles: %w", sc.ID, err)
		}
		cues = append(cues, sceneCues...)
		speechEnd = starts[sc.ID] + c.seconds
	}
	var b strings.Builder
	if err := srt.Write(&b, cues); err != nil {
		return 0, err
	}
	return speechEnd, os.WriteFile(path, []byte(b.String()), 0o644)
}

// subtitleStyle boxes the text: outline-only subtitles are legible over a
// dark terminal and marginal over a white screenshot, and a demo cuts
// between both. The box is about 80% opaque (ASS alpha 0x30), because a
// mostly transparent box over a busy terminal leaves text on top of text.
const subtitleStyle = "FontName=DejaVu Sans,Fontsize=16,BorderStyle=3,Outline=6,Shadow=0,MarginV=30," +
	"PrimaryColour=&H00FFFFFF&,OutlineColour=&H30101014&,BackColour=&H30101014&"

// burn puts subtitles into the picture when ffmpeg has libass, and otherwise
// embeds a soft track and says so. It runs from scratch with fixed names and
// the embedded font, so no user path enters filter syntax and the result
// does not depend on system fonts. ffmpeg 8 dropped positional filter
// options, hence subtitles=filename=.
func burn(scratch, cut, srtPath, out string, stdout io.Writer) (bool, error) {
	data, err := os.ReadFile(srtPath)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(scratch, "captions.srt"), data, 0o644); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Join(scratch, "fonts"), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(scratch, "fonts", "DejaVuSans.ttf"), fonts.SansTTF(), 0o644); err != nil {
		return false, err
	}
	if ffmpeg.HasFilter("subtitles") {
		err := ffmpeg.Run(scratch, nil, "-i", cut,
			"-vf", "subtitles=filename=captions.srt:fontsdir=fonts:force_style='"+subtitleStyle+"'",
			"-c:a", "copy", "-c:v", "libx264", "-preset", "medium", "-pix_fmt", "yuv420p", out)
		if err != nil {
			return false, fmt.Errorf("burning subtitles: %w", err)
		}
		return true, nil
	}
	err = ffmpeg.Run(scratch, nil, "-i", cut, "-i", "captions.srt",
		"-map", "0:v:0", "-map", "0:a?", "-map", "1:s:0",
		"-c", "copy", "-c:s", "mov_text", "-metadata:s:s:0", "language=eng", out)
	if err != nil {
		return false, fmt.Errorf("embedding subtitles: %w", err)
	}
	fmt.Fprintln(stdout, "WARN       this ffmpeg has no libass (no subtitles filter), so the subtitles are a "+
		"soft track a player must choose to show, not pixels. Slack and PR previews will show none. "+
		"Install an ffmpeg with libass to burn them in.")
	return false, nil
}
