package build

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"

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
// segment lasts target seconds. wav is the narration clip, or "" for
// silence, and starts delay seconds into the segment. Piping the frames
// means no user path is ever parsed as an ffmpeg pattern.
func encodeStill(scratch, name string, f *scene.File, pngs io.Reader, rate float64, count int, target float64, wav string, delay float64) error {
	hold := math.Max(0, target-float64(count)/rate)
	audio := "apad"
	if delay > 0 {
		audio = fmt.Sprintf("adelay=%d:all=1,apad", int(math.Round(delay*1000)))
	}
	args := []string{"-f", "image2pipe", "-c:v", "png", "-framerate", strconv.FormatFloat(rate, 'f', -1, 64), "-i", "-"}
	args = append(args, audioInput(wav)...)
	args = append(args,
		"-vf", fit(f.Width, f.Height)+fmt.Sprintf(",tpad=stop_mode=clone:stop_duration=%.3f", hold),
		"-af", audio, "-r", strconv.Itoa(f.FPS), "-t", fmt.Sprintf("%.3f", target),
		"-map", "0:v:0", "-map", "1:a:0")
	args = append(append(args, encodeArgs...), name)
	return ffmpeg.Run(scratch, pngs, args...)
}

// fileStream records a copying goroutine's error and provides Read and Close.
type fileStream struct {
	*io.PipeReader
	done      chan error
	closeOnce sync.Once
	closeErr  error
}

// streamFiles concatenates files onto one reader, opening one at a time.
// Close it after use so the copying goroutine ends even if ffmpeg quit early.
func streamFiles(paths []string) *fileStream {
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() {
		for _, p := range paths {
			file, err := os.Open(p)
			if err != nil {
				done <- fmt.Errorf("reading %s: %w", p, err)
				w.Close()
				return
			}
			_, err = io.Copy(w, file)
			file.Close()
			if err != nil {
				done <- fmt.Errorf("reading %s: %w", p, err)
				w.Close()
				return
			}
		}
		done <- nil
		w.Close()
	}()
	return &fileStream{PipeReader: r, done: done}
}

// Close closes the pipe reader, waits for the copying goroutine, and returns
// any file error (ignoring io.ErrClosedPipe, which means ffmpeg quit early).
// Close is idempotent and may be called multiple times.
func (fs *fileStream) Close() error {
	fs.closeOnce.Do(func() {
		fs.PipeReader.Close()
		err := <-fs.done
		// If the error is io.ErrClosedPipe, the pipe was closed (expected when ffmpeg
		// quits early). Only report real file errors.
		if errors.Is(err, io.ErrClosedPipe) {
			fs.closeErr = nil
		} else {
			fs.closeErr = err
		}
	})
	return fs.closeErr
}

// narrationPlacement is how far into a scene its narration starts and how
// long the scene lasts. Narration starts with the scene; with
// narration_at: end it ends as the visuals end, but never starts before
// settled, where a terminal take's result appears. When it would run past
// the visuals, the last frame (the result) is held until it finishes.
func narrationPlacement(visual, speech, settled float64, atEnd bool) (delay, length float64) {
	if atEnd {
		delay = math.Max(0, math.Max(visual-speech, settled))
	}
	return delay, math.Max(visual, delay+speech)
}

// segment encodes one scene into scratch/<id>.mp4 and returns its measured
// duration and how far into it the narration starts. wav and speech are the
// scene's narration clip and its length, or "" and 0. A still or frames
// scene lasts max(narration, visuals).
func segment(scratch string, f *scene.File, sc scene.Scene, wav string, speech float64) (float64, float64, error) {
	name := sc.ID + ".mp4"
	var err error
	var delay float64
	switch sc.Kind {
	case scene.Movie:
		err = encodeMovie(scratch, name, f, sc.Source)
	case scene.Frames:
		frames := scene.PNGs(sc.Source)
		var target float64
		delay, target = narrationPlacement(float64(len(frames))/sc.Rate, speech, sc.Settled, sc.NarrationAtEnd)
		pngs := streamFiles(frames)
		err = encodeStill(scratch, name, f, pngs, sc.Rate, len(frames), target, wav, delay)
		if cerr := pngs.Close(); cerr != nil {
			err = cerr
		}
	case scene.Image:
		var target float64
		delay, target = narrationPlacement(sc.Duration, speech, 0, sc.NarrationAtEnd)
		pngs := streamFiles([]string{sc.Source})
		err = encodeStill(scratch, name, f, pngs, float64(f.FPS), 1, target, wav, delay)
		if cerr := pngs.Close(); cerr != nil {
			err = cerr
		}
	case scene.Card:
		var target float64
		delay, target = narrationPlacement(sc.Duration, speech, 0, sc.NarrationAtEnd)
		var png []byte
		if png, err = Card(sc.Title, sc.Subtitle, f.Width, f.Height); err == nil {
			err = encodeStill(scratch, name, f, bytes.NewReader(png), float64(f.FPS), 1, target, wav, delay)
		}
	}
	if err != nil {
		return 0, 0, fmt.Errorf("scene %s: %w", sc.ID, err)
	}
	info, err := ffmpeg.Probe(filepath.Join(scratch, name))
	if err != nil {
		return 0, 0, err
	}
	return info.Duration, delay, nil
}

// encodeMovie plays an existing movie as itself, scaled to fit, with its own
// audio, or silence when it has none (concat needs every segment to have an
// audio stream).
func encodeMovie(scratch, name string, f *scene.File, src string) error {
	info, err := ffmpeg.Probe(src)
	if err != nil {
		return err
	}
	args := []string{"-i", src}
	audio := "0:a:0"
	if !info.Has("audio") {
		args = append(args, audioInput("")...)
		audio = "1:a:0"
	}
	args = append(args, "-vf", fit(f.Width, f.Height), "-af", "apad", "-r", strconv.Itoa(f.FPS),
		"-t", fmt.Sprintf("%.3f", info.Duration), "-map", "0:v:0", "-map", audio)
	args = append(append(args, encodeArgs...), name)
	return ffmpeg.Run(scratch, nil, args...)
}
