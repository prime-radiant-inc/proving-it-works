package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/srt"
)

// findSubtitles reads <movie stem>.srt beside the movie, else the first
// embedded subtitle track.
func findSubtitles(movie string, info ffmpeg.Info) (Subtitles, error) {
	sidecar := strings.TrimSuffix(movie, filepath.Ext(movie)) + ".srt"
	subs := Subtitles{Source: filepath.Base(sidecar)}
	var text string
	if data, err := os.ReadFile(sidecar); err == nil {
		subs.Found, text = true, string(data)
	} else if info.Has("subtitle") {
		out, err := ffmpeg.Output("-i", movie, "-map", "0:s:0", "-f", "srt", "-")
		if err != nil {
			return subs, fmt.Errorf("embedded subtitle extraction failed: %w", err)
		}
		subs.Found, subs.Source, text = true, "embedded", string(out)
	} else {
		return subs, nil
	}
	end, err := srt.End(text)
	if err != nil {
		return subs, fmt.Errorf("invalid subtitle timing in %s: %w", subs.Source, err)
	}
	subs.End = end
	return subs, nil
}
