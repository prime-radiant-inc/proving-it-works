package scene

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixture writes a scene file plus the media it names into a temp dir.
func fixture(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"shot.png", "frames/f1.png", "clip.mp4"} {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte("x"), 0o644)
	}
	os.MkdirAll(filepath.Join(dir, "empty"), 0o755)
	path := filepath.Join(dir, "demo.yaml")
	os.WriteFile(path, []byte(yaml), 0o644)
	return path
}

func TestValidFileGetsDefaults(t *testing.T) {
	f, err := Load(fixture(t, `scenes:
  - id: title
    card: proving-it-works
    subtitle: a subtitle
  - id: shot
    image: shot.png
    narration: "  Spoken
      words.  "
  - id: run
    frames: frames
  - id: clip
    movie: clip.mp4
`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Width != 1920 || f.Height != 1080 || f.FPS != 30 || f.Engine != "auto" || f.Voice != "" {
		t.Fatalf("defaults: %+v", f)
	}
	kinds := []Kind{f.Scenes[0].Kind, f.Scenes[1].Kind, f.Scenes[2].Kind, f.Scenes[3].Kind}
	if !slices.Equal(kinds, []Kind{Card, Image, Frames, Movie}) {
		t.Fatalf("kinds %v", kinds)
	}
	if f.Scenes[0].Duration != 3 || f.Scenes[2].Rate != 30 {
		t.Fatalf("duration %v rate %v", f.Scenes[0].Duration, f.Scenes[2].Rate)
	}
	if f.Scenes[1].Narration != "Spoken words." || !f.Narrated() {
		t.Fatalf("narration %q", f.Scenes[1].Narration)
	}
	if f.Scenes[1].Source != filepath.Join(filepath.Dir(f.Path), "shot.png") {
		t.Fatalf("source %q", f.Scenes[1].Source)
	}
}

func TestEveryProblemIsReportedAtOnce(t *testing.T) {
	_, err := Load(fixture(t, `size: 1921x1080
fps: 0
engine: robot
colour: red
scenes:
  - id: Bad_ID
    card: x
  - id: two
    card: x
    image: shot.png
  - id: none
  - id: dup
    image: missing.png
  - id: dup
    frames: empty
  - id: talky
    movie: clip.mp4
    narration: movies keep their own audio
  - id: slow
    frames: frames
    rate: -1
    duration: 4
`))
	var p Problems
	if !errors.As(err, &p) {
		t.Fatalf("got %v", err)
	}
	all := strings.Join(p, "\n")
	for _, want := range []string{
		"size must have even width and height",
		"fps must be a positive whole number",
		`engine must be one of auto, openai, openai-chat, piper`,
		`unknown top-level key "colour"`,
		"scene 1: id must match",
		"scene two: needs exactly one of card, image, frames, movie (found 2)",
		"scene none: needs exactly one of card, image, frames, movie (found 0)",
		"scene dup: no such image",
		"scene dup: duplicate id",
		"scene dup: no PNG frames in",
		`scene talky: "narration" is not a field of a movie scene`,
		"scene slow: rate must be a positive number",
		`scene slow: "duration" is not a field of a frames scene`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing problem %q in:\n%s", want, all)
		}
	}
}
