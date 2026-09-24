// Package srt times, writes, and reads SubRip subtitles.
package srt

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	timingLine = regexp.MustCompile(`^(\d{2,}):([0-5]\d):([0-5]\d),(\d{3})\s+-->\s+(\d{2,}):([0-5]\d):([0-5]\d),(\d{3})$`)
	blankLine  = regexp.MustCompile(`\n\s*\n`)
	digits     = regexp.MustCompile(`^\d+$`)
)

// End returns the end of the last-ending cue, or nil when there are no cues.
// Only a cue's second line is its timing; text shaped like a timing line
// inside a caption never counts.
func End(text string) (*float64, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return nil, nil
	}
	var end *float64
	for _, block := range blankLine.Split(text, -1) {
		lines := strings.Split(block, "\n")
		if len(lines) < 2 || !digits.MatchString(strings.TrimSpace(lines[0])) {
			return nil, errors.New("malformed SRT cue index or missing timing line")
		}
		m := timingLine.FindStringSubmatch(strings.TrimSpace(lines[1]))
		if m == nil {
			return nil, fmt.Errorf("malformed SRT cue timing: %s", lines[1])
		}
		var n [4]int
		for i := range n {
			n[i], _ = strconv.Atoi(m[5+i])
		}
		v := float64(n[0]*3600+n[1]*60+n[2]) + float64(n[3])/1000
		if end == nil || v > *end {
			end = &v
		}
	}
	return end, nil
}
