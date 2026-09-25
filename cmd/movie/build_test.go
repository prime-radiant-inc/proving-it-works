package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/prime-radiant-inc/proving-it-works/internal/srt"
	"github.com/prime-radiant-inc/proving-it-works/internal/testmedia"
)

// writeFile writes content to dir/name, creating parent directories.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// still writes a solid-colour PNG at dir/name. ffmpeg writes it under a safe
// name first, because its image muxer would read "%03d" in name as a pattern.
func still(t *testing.T, dir, name, colour string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "color=c="+colour+":size=320x180:d=0.1",
		"-frames:v", "1", "still-tmp.png")
	if err := os.Rename(filepath.Join(dir, "still-tmp.png"), filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

func assertNear(t *testing.T, what string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s: got %.3f, want %.3f ± %.3f", what, got, want, tolerance)
	}
}

func TestBuildAssemblesImageScenesInOrderAndChecksTheResult(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	still(t, dir, "red.png", "red")
	still(t, dir, "blue.png", "blue")
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
scenes:
  - id: first
    image: red.png
    duration: 2
  - id: second
    image: blue.png
    duration: 3
`)
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 0 {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	assertNear(t, "movie duration", testmedia.Duration(t, filepath.Join(dir, "demo.mp4")), 5, 0.2)
	if !strings.Contains(r.stdout, "Mechanical checks pass") {
		t.Fatalf("build did not run the check:\n%s", r.stdout)
	}
}

// An unnarrated build removes subtitles left beside the output by an earlier
// narrated build, so a later standalone `movie check` never reads them.
func TestUnnarratedBuildRemovesStaleSubtitles(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	still(t, dir, "red.png", "red")
	still(t, dir, "blue.png", "blue")
	writeFile(t, dir, "demo.srt", "1\n00:00:00,000 --> 00:00:04,000\nnarration from an older build\n")
	writeFile(t, dir, "demo.yaml", "size: 320x180\nfps: 10\nscenes:\n"+
		"  - id: first\n    image: red.png\n    duration: 2\n"+
		"  - id: second\n    image: blue.png\n    duration: 2\n")
	if r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4"); r.code != 0 {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo.srt")); !os.IsNotExist(err) {
		t.Fatalf("demo.srt from an older build is still beside the movie (stat: %v)", err)
	}
}

// awkward is a directory name that breaks every naive ffmpeg path handling.
const awkward = "awk %d [x] 'q' λ & more"

func TestBuildHandlesEverySceneKindAtAwkwardPaths(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := filepath.Join(t.TempDir(), awkward)
	still(t, dir, "media/shot %03d.png", "red")
	for i, c := range []string{"red", "green", "blue", "white", "black"} {
		still(t, dir, filepath.Join("media", "frames [1]", fmt.Sprintf("f%02d.png", i)), c)
	}
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=2",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "media/with sound.mp4")
	testmedia.FFmpeg(t, dir, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10:d=1.5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "media/silent 100%.mp4")
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
scenes:
  - id: title
    card: proving-it-works
    subtitle: every kind of scene
    duration: 1
  - id: shot
    image: "media/shot %03d.png"
    duration: 1
  - id: run
    frames: "media/frames [1]"
    rate: 2.5
  - id: loud
    movie: "media/with sound.mp4"
  - id: quiet
    movie: "media/silent 100%.mp4"
`)
	r := runMovie(t, dir, "build", "demo.yaml", "out/final cut.mp4")
	if r.code != 0 {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	// 1 + 1 + 5/2.5 + 2 + 1.5
	assertNear(t, "movie duration", testmedia.Duration(t, filepath.Join(dir, "out", "final cut.mp4")), 7.5, 0.3)
	for _, line := range []string{"title  1.0s", "shot  1.0s", "run  2.0s", "loud  2.0s", "quiet  1.5s"} {
		if !strings.Contains(r.stdout, line) {
			t.Errorf("missing %q in\n%s", line, r.stdout)
		}
	}
}

func TestBuildRejectsAnInvalidSceneFileBeforeEncoding(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	writeFile(t, dir, "demo.yaml", "scenes:\n  - id: x\n    image: nope.png\n")
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 2 || !strings.Contains(r.stderr, "no such image") {
		t.Fatalf("code %d\n%s", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo.build")); err == nil {
		t.Fatal("scratch was created for an invalid scene file")
	}
}

// requirePiper skips unless the keyless voice is installed.
func requirePiper(t *testing.T) {
	t.Helper()
	testmedia.Require(t, "ffmpeg", "ffprobe", "piper")
	dir := os.Getenv("PIPER_VOICE_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache", "piper-voices")
	}
	if _, err := os.Stat(filepath.Join(dir, "en_US-lessac-medium.onnx")); err != nil {
		t.Skipf("needs the piper voice en_US-lessac-medium in %s", dir)
	}
}

func TestNarratedCardLastsAsLongAsItsClipAndReusesIt(t *testing.T) {
	requirePiper(t)
	dir := t.TempDir()
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
engine: piper
scenes:
  - id: title
    card: proving it works
    duration: 1
    narration: This card is narrated by a local voice, and it lasts as long as the words do.
  - id: end
    card: the end
    duration: 1
`)
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if !strings.Contains(r.stdout, "rendered") {
		t.Fatalf("no clip rendered:\n%s%s", r.stdout, r.stderr)
	}
	clips, _ := filepath.Glob(filepath.Join(dir, "demo.build", "narration", "*.wav"))
	if len(clips) != 1 {
		t.Fatalf("clips: %v", clips)
	}
	speech := testmedia.Duration(t, clips[0])
	segment := testmedia.Duration(t, filepath.Join(dir, "demo.build", "title.mp4"))
	if speech < 2 || segment < speech-0.05 {
		t.Fatalf("clip %.2fs, segment %.2fs", speech, segment)
	}
	again := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if !strings.Contains(again.stdout, "cached") {
		t.Fatalf("second build did not reuse the clip:\n%s", again.stdout)
	}
}

func TestMissingPiperVoiceIsAnEnvironmentErrorNamingTheFix(t *testing.T) {
	testmedia.Require(t, "ffmpeg", "ffprobe", "piper")
	dir := t.TempDir()
	t.Setenv("PIPER_VOICE_DIR", t.TempDir())
	writeFile(t, dir, "demo.yaml", "engine: piper\nscenes:\n  - id: a\n    card: x\n    narration: hello\n")
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 2 || !strings.Contains(r.stderr, "piper.download_voices") {
		t.Fatalf("code %d\n%s", r.code, r.stderr)
	}
}

func TestNarratedBuildWritesSubtitlesAndPassesTheCheck(t *testing.T) {
	requirePiper(t)
	dir := t.TempDir()
	for i, c := range []string{"red", "green", "blue", "white", "black", "yellow", "cyan", "magenta", "gray", "orange"} {
		still(t, dir, fmt.Sprintf("run/f%02d.png", i), c)
	}
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
engine: piper
scenes:
  - id: title
    card: proving it works
    duration: 2
  - id: run
    frames: run
    rate: 1
    narration: The run takes ten seconds, and this sentence is much shorter than that.
`)
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 0 || !strings.Contains(r.stdout, "Mechanical checks pass") {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	data, err := os.ReadFile(filepath.Join(dir, "demo.srt"))
	// the run scene starts after the 2 s card (plus a few ms of audio padding)
	if err != nil || !strings.Contains(string(data), "\n00:00:02,") {
		t.Fatalf("subtitles should start at the run scene's offset, about 2 s:\n%s", data)
	}
}

func TestOpenAIVoiceNarrates(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("needs OPENAI_API_KEY")
	}
	testmedia.Require(t, "ffmpeg", "ffprobe")
	for _, engine := range []string{"openai", "openai-chat"} {
		dir := t.TempDir()
		writeFile(t, dir, "demo.yaml", "size: 320x180\nfps: 10\nengine: "+engine+
			"\nscenes:\n  - id: a\n    card: x\n    narration: Proving it works, out loud.\n")
		r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
		if !strings.Contains(r.stdout, "rendered") {
			t.Errorf("%s:\n%s%s", engine, r.stdout, r.stderr)
		}
	}
}

// narration_at: end delays a scene's narration so it ends as the scene ends,
// where a terminal take shows its result; the subtitles move with it.
func TestNarrationAtEndLandsOnTheResult(t *testing.T) {
	requirePiper(t)
	dir := t.TempDir()
	for i, c := range []string{"red", "green", "blue", "white", "black", "yellow", "cyan", "magenta", "gray", "orange"} {
		still(t, dir, fmt.Sprintf("run/f%02d.png", i), c)
	}
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
engine: piper
scenes:
  - id: run
    frames: run
    rate: 1
    narration: And the result appears.
    narration_at: end
`)
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 0 {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	clips, _ := filepath.Glob(filepath.Join(dir, "demo.build", "narration", "*.wav"))
	speech := testmedia.Duration(t, clips[0])
	data, _ := os.ReadFile(filepath.Join(dir, "demo.srt"))
	end, err := srtEnd(string(data))
	if err != nil {
		t.Fatal(err)
	}
	assertNear(t, "last cue end", end, 10, 0.1)
	first := strings.SplitN(strings.Split(string(data), "\n")[1], " --> ", 2)[0]
	start := parseTimestamp(t, first)
	assertNear(t, "first cue start", start, 10-speech, 0.1)
}

func srtEnd(text string) (float64, error) {
	end, err := srt.End(text)
	if err != nil || end == nil {
		return 0, fmt.Errorf("no cues: %v", err)
	}
	return *end, nil
}

func parseTimestamp(t *testing.T, ts string) float64 {
	t.Helper()
	var h, m, s, ms int
	if _, err := fmt.Sscanf(ts, "%d:%d:%d,%d", &h, &m, &s, &ms); err != nil {
		t.Fatalf("timestamp %q: %v", ts, err)
	}
	return float64(h*3600+m*60+s) + float64(ms)/1000
}

// A take's narration_at: end narration never starts before the take's result
// appears (take.json's settled time); the result is held while it plays.
func TestNarrationWaitsForTheTakesResult(t *testing.T) {
	requirePiper(t)
	dir := t.TempDir()
	for i := range 8 {
		still(t, dir, fmt.Sprintf("take/f%02d.png", i), []string{"red", "green", "blue", "white", "black", "yellow", "cyan", "gray"}[i])
	}
	writeFile(t, dir, "take/take.json", `{"frames": "unused", "rate": 1, "settled": 7}`)
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
engine: piper
scenes:
  - id: take
    frames: take
    narration: And there is the result.
    narration_at: end
`)
	r := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if r.code != 0 {
		t.Fatalf("code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	clips, _ := filepath.Glob(filepath.Join(dir, "demo.build", "narration", "*.wav"))
	speech := testmedia.Duration(t, clips[0])
	data, _ := os.ReadFile(filepath.Join(dir, "demo.srt"))
	first := strings.SplitN(strings.Split(string(data), "\n")[1], " --> ", 2)[0]
	assertNear(t, "narration start", parseTimestamp(t, first), 7, 0.1)
	assertNear(t, "movie length", testmedia.Duration(t, filepath.Join(dir, "demo.mp4")), 7+speech, 0.2)
}

// build reports each clip on one line with its length, warns about the local
// voice only when it rendered something, and shows where each scene's
// narration sits so a scene longer than its narration is visible.
func TestBuildReportsNarrationCompactly(t *testing.T) {
	requirePiper(t)
	dir := t.TempDir()
	writeFile(t, dir, "demo.yaml", `size: 320x180
fps: 10
engine: piper
scenes:
  - id: title
    card: proving it works
    duration: 6
    narration: A short line.
  - id: end
    card: the end
    duration: 2
`)
	first := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	clipLine := regexp.MustCompile(`(?m)^narration  title  \d+\.\ds  rendered  \S+\.wav$`)
	sceneLine := regexp.MustCompile(`(?m)^title  6\.0s  narration \d+\.\ds from 0\.0s$`)
	for _, re := range []*regexp.Regexp{clipLine, sceneLine} {
		if !re.MatchString(first.stdout) {
			t.Errorf("no line matching %s in:\n%s", re, first.stdout)
		}
	}
	if !strings.Contains(first.stdout, "local voice:") {
		t.Errorf("a freshly rendered local voice should come with its warning:\n%s", first.stdout)
	}
	again := runMovie(t, dir, "build", "demo.yaml", "demo.mp4")
	if !regexp.MustCompile(`(?m)^narration  title  \d+\.\ds  cached  `).MatchString(again.stdout) {
		t.Errorf("second build should reuse the clip:\n%s", again.stdout)
	}
	if strings.Contains(again.stdout, "local voice:") {
		t.Errorf("nothing was rendered, so no voice warning:\n%s", again.stdout)
	}
}
