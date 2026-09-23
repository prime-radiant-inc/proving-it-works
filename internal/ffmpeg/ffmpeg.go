// Package ffmpeg runs ffmpeg and ffprobe with the options every call needs.
package ffmpeg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// base is prepended to every ffmpeg call: never read stdin as a terminal
// (it eats loop input), always overwrite, and print only errors.
var base = []string{"-nostdin", "-y", "-v", "error"}

// Require reports ffmpeg or ffprobe missing from PATH.
func Require() error {
	var missing []string
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s not on PATH: install ffmpeg", strings.Join(missing, " and "))
	}
	return nil
}

// Run runs ffmpeg in dir, feeding stdin when it is not nil.
func Run(dir string, stdin io.Reader, args ...string) error {
	full := append(append([]string{}, base...), args...)
	cmd := exec.Command("ffmpeg", full...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg %s: %v\n%s", strings.Join(full, " "), err, tail(stderr.String()))
	}
	return nil
}

// Output runs ffmpeg and returns what it writes to stdout.
func Output(args ...string) ([]byte, error) {
	full := append(append([]string{}, base...), args...)
	cmd := exec.Command("ffmpeg", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg %s: %v\n%s", strings.Join(full, " "), err, tail(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Stream is one stream ffprobe reports.
type Stream struct {
	Index     int    `json:"index"`
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// Info is a media file's duration and streams.
type Info struct {
	Duration float64
	Streams  []Stream
}

// Has reports whether the file has a stream of kind (video, audio, subtitle).
func (i Info) Has(kind string) bool {
	_, ok := i.First(kind)
	return ok
}

// First returns the first stream of kind.
func (i Info) First(kind string) (Stream, bool) {
	for _, s := range i.Streams {
		if s.CodecType == kind {
			return s, true
		}
	}
	return Stream{}, false
}

// Probe reads a media file's duration and streams.
func Probe(path string) (Info, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: %v\n%s", path, err, tail(stderr.String()))
	}
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []Stream `json:"streams"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	d, err := strconv.ParseFloat(raw.Format.Duration, 64)
	if err != nil && raw.Format.Duration != "" {
		return Info{}, errors.New("ffprobe reported an unreadable duration for " + path)
	}
	return Info{Duration: d, Streams: raw.Streams}, nil
}

// HasFilter reports whether this ffmpeg build has the named filter.
func HasFilter(name string) bool {
	out, err := exec.Command("ffmpeg", "-hide_banner", "-filters").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[1] == name {
			return true
		}
	}
	return false
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 800 {
		return "..." + s[len(s)-800:]
	}
	return s
}
