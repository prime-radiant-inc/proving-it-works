// Package narrate renders narration clips and accepts only clips that pass
// their engine's gate.
package narrate

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// segmentationScripts are written without spaces between words, so a
// word-by-word comparison says nothing about them.
var segmentationScripts = []struct{ lo, hi rune }{
	{0x3040, 0x30FF},   // Hiragana and Katakana
	{0x3400, 0x4DBF},   // CJK Extension A
	{0x4E00, 0x9FFF},   // CJK Unified Ideographs
	{0xF900, 0xFAFF},   // CJK Compatibility Ideographs
	{0x20000, 0x323AF}, // CJK Unified Ideograph extensions B through I
	{0x2F800, 0x2FA1F}, // CJK Compatibility Ideographs Supplement
	{0x0E00, 0x0E7F},   // Thai
}

func needsSegmentation(text string) bool {
	for _, r := range norm.NFKC.String(text) {
		for _, s := range segmentationScripts {
			if r >= s.lo && r <= s.hi {
				return true
			}
		}
	}
	return false
}

var folder = cases.Fold()

// words normalizes text for comparison: NFKC, case-folded, split on
// whitespace, keeping only letters, numbers, and marks.
func words(text string) []string {
	var out []string
	for _, token := range strings.Fields(folder.String(norm.NFKC.String(text))) {
		kept := strings.Map(func(r rune) rune {
			if unicode.In(r, unicode.Letter, unicode.Number, unicode.Mark) {
				return r
			}
			return -1
		}, token)
		if kept != "" {
			out = append(out, kept)
		}
	}
	return out
}

// ChatAccepts reports whether a chat model's own transcript says it read the
// script verbatim, and why not when it did not. The transcript is the
// engine's text rather than a noisy recognizer's, so the comparison is
// strict: the length difference plus word-by-word mismatches at the same
// position may not exceed max(2, words/25). One spoken word of preamble
// shifts every word after it, so it cannot pass.
func ChatAccepts(script, transcript string) (bool, string) {
	if needsSegmentation(script) || needsSegmentation(transcript) {
		return false, "this script cannot be compared word by word; use engine openai or piper"
	}
	want, got := words(script), words(transcript)
	if len(want) == 0 {
		return false, "the script has no words to compare"
	}
	if len(got) == 0 {
		return false, "the transcript contains no speech"
	}
	drift := max(len(want)-len(got), len(got)-len(want))
	for i := range min(len(want), len(got)) {
		if want[i] != got[i] {
			drift++
		}
	}
	if limit := max(2, len(want)/25); drift > limit {
		return false, fmt.Sprintf("the model ad-libbed (drift %d, limit %d)", drift, limit)
	}
	return true, ""
}
