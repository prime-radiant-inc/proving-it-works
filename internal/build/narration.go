package build

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/narrate"
	"github.com/prime-radiant-inc/proving-it-works/internal/scene"
)

// clip is an accepted narration clip and its measured length.
type clip struct {
	wav     string
	seconds float64
}

// narrateAll renders or reuses a clip for every narrated scene.
func narrateAll(f *scene.File, scratch string, stdout io.Writer) (map[string]clip, error) {
	clips := map[string]clip{}
	if !f.Narrated() {
		return clips, nil
	}
	e, err := narrate.Resolve(f.Engine)
	if err != nil {
		return nil, err
	}
	voice := f.Voice
	if voice == "" {
		voice = e.DefaultVoice()
	}
	if err := e.Ready(voice); err != nil {
		return nil, err
	}
	fmt.Fprintf(stdout, "narration: %s, voice %s\n", e.Name(), voice)
	dir := filepath.Join(scratch, "narration")
	for _, sc := range f.Scenes {
		if sc.Narration == "" {
			continue
		}
		fmt.Fprintf(stdout, "%s:\n", sc.ID)
		wav, err := narrate.Clip(dir, e, voice, sc.Narration, stdout)
		if err != nil {
			return nil, fmt.Errorf("scene %s: %w", sc.ID, err)
		}
		info, err := ffmpeg.Probe(wav)
		if err != nil {
			return nil, err
		}
		clips[sc.ID] = clip{wav: wav, seconds: info.Duration}
	}
	if e.Name() == "piper" {
		fmt.Fprintln(stdout, "local voice: it mispronounces unusual names rather than dropping them - "+
			"listen to one clip before you commit to a voice.")
	}
	return clips, nil
}
