package build

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
	"github.com/prime-radiant-inc/proving-it-works/internal/scene"
)

const background = "#101014"

// encodeArgs make every segment identical in codec parameters, which is what
// lets the final concat copy streams instead of re-encoding.
var encodeArgs = []string{"-c:v", "libx264", "-preset", "medium", "-pix_fmt", "yuv420p",
	"-c:a", "aac", "-ar", "44100", "-ac", "2"}

// fit scales a picture into w x h without distortion and pads the rest.
func fit(w, h int) string {
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,"+
		"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=%s,setsar=1", w, h, w, h, background)
}

// audioInput is the narration clip, or silence.
func audioInput(wav string) []string {
	if wav == "" {
		return []string{"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo"}
	}
	return []string{"-i", wav}
}

// encodeStill writes scratch/name from count PNGs streamed on pngs at rate
// frames per second. The last frame is held and the audio padded so the
// segment lasts target seconds. wav is the narration clip, or "" for silence.
// Piping the frames means no user path is ever parsed as an ffmpeg pattern.
func encodeStill(scratch, name string, f *scene.File, pngs io.Reader, rate float64, count int, target float64, wav string) error {
	hold := math.Max(0, target-float64(count)/rate)
	args := []string{"-f", "image2pipe", "-c:v", "png", "-framerate", strconv.FormatFloat(rate, 'f', -1, 64), "-i", "-"}
	args = append(args, audioInput(wav)...)
	args = append(args,
		"-vf", fit(f.Width, f.Height)+fmt.Sprintf(",tpad=stop_mode=clone:stop_duration=%.3f", hold),
		"-af", "apad", "-r", strconv.Itoa(f.FPS), "-t", fmt.Sprintf("%.3f", target),
		"-map", "0:v:0", "-map", "1:a:0")
	args = append(append(args, encodeArgs...), name)
	return ffmpeg.Run(scratch, pngs, args...)
}

// streamFiles concatenates files onto one reader, opening one at a time.
// Close it after use so the copying goroutine ends even if ffmpeg quit early.
func streamFiles(paths []string) io.ReadCloser {
	r, w := io.Pipe()
	go func() {
		for _, p := range paths {
			file, err := os.Open(p)
			if err != nil {
				w.CloseWithError(err)
				return
			}
			_, err = io.Copy(w, file)
			file.Close()
			if err != nil {
				w.CloseWithError(err)
				return
			}
		}
		w.Close()
	}()
	return r
}
