package scene

import (
	"errors"
	"fmt"
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
		`scene 1: id "Bad_ID" must match`,
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

func TestCardTextTheFontCannotDrawIsRejected(t *testing.T) {
	_, err := Load(fixture(t, "scenes:\n  - id: t\n    card: 漢字 works\n"))
	var p Problems
	if !errors.As(err, &p) || !strings.Contains(strings.Join(p, "\n"), "漢 (U+6F22)") {
		t.Fatalf("got %v", err)
	}
}

// A take directory from `movie term stop` carries its own frame rate in
// take.json; a frames scene uses it unless the scene file says otherwise.
func TestFramesSceneTakesItsRateFromTakeJSON(t *testing.T) {
	path := fixture(t, "fps: 30\nscenes:\n  - id: take\n    frames: frames\n  - id: fast\n    frames: frames\n    rate: 25\n")
	os.WriteFile(filepath.Join(filepath.Dir(path), "frames", "take.json"), []byte(`{"frames": "/elsewhere", "rate": 10}`), 0o644)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Scenes[0].Rate != 10 {
		t.Errorf("take.json rate not used: got %v", f.Scenes[0].Rate)
	}
	if f.Scenes[1].Rate != 25 {
		t.Errorf("an explicit rate must win: got %v", f.Scenes[1].Rate)
	}
}

func TestUnreadableTakeJSONIsAProblem(t *testing.T) {
	path := fixture(t, "scenes:\n  - id: take\n    frames: frames\n")
	os.WriteFile(filepath.Join(filepath.Dir(path), "frames", "take.json"), []byte(`{"rate": "fast"}`), 0o644)
	_, err := Load(path)
	var p Problems
	if !errors.As(err, &p) || !strings.Contains(strings.Join(p, "\n"), "take.json") {
		t.Fatalf("got %v", err)
	}
}

func TestNarrationAtEndIsParsedAndValidated(t *testing.T) {
	f, err := Load(fixture(t, "scenes:\n  - id: take\n    frames: frames\n    narration: the result\n    narration_at: end\n  - id: card\n    card: x\n    narration: hi\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !f.Scenes[0].NarrationAtEnd || f.Scenes[1].NarrationAtEnd {
		t.Fatalf("NarrationAtEnd: %v %v", f.Scenes[0].NarrationAtEnd, f.Scenes[1].NarrationAtEnd)
	}
	_, err = Load(fixture(t, "scenes:\n  - id: take\n    frames: frames\n    narration: x\n    narration_at: middle\n  - id: clip\n    movie: clip.mp4\n    narration_at: end\n"))
	all := fmt.Sprint(err)
	for _, want := range []string{"scene take: narration_at must be start or end", `scene clip: "narration_at" is not a field of a movie scene`} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in %s", want, all)
		}
	}
}

func TestFramesSceneReadsWhenTheTakeSettled(t *testing.T) {
	path := fixture(t, "scenes:\n  - id: take\n    frames: frames\n")
	os.WriteFile(filepath.Join(filepath.Dir(path), "frames", "take.json"), []byte(`{"rate": 10, "settled": 2.5}`), 0o644)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Scenes[0].Settled != 2.5 {
		t.Fatalf("settled %v, want 2.5", f.Scenes[0].Settled)
	}
}

func TestProblemsNameTheBadValueAndSuggestTheField(t *testing.T) {
	_, err := Load(fixture(t, "scnes: []\nscenes:\n  - id: Title\n    card: hi\n    narations: oops\n  - card: x\n"))
	all := fmt.Sprint(err)
	for _, want := range []string{
		`unknown top-level key "scnes" (did you mean "scenes"?)`,
		`scene 1: id "Title" must match [a-z0-9][a-z0-9-]*`,
		`scene 1: "narations" is not a field of a card scene (did you mean "narration"?)`,
		`scene 2: needs an id`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
}
