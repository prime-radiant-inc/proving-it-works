// Package scene reads and validates a movie's scene file.
package scene

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/image/font/opentype"
	"gopkg.in/yaml.v3"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/fonts"
)

// Kind is what a scene shows.
type Kind string

const (
	Card   Kind = "card"
	Image  Kind = "image"
	Frames Kind = "frames"
	Movie  Kind = "movie"
)

var kinds = []Kind{Card, Image, Frames, Movie}

// File is a parsed, valid scene file.
type File struct {
	Path               string // the scene file
	Dir                string // paths in the file are relative to this
	Width, Height, FPS int
	Engine             string // auto, openai, openai-chat, or piper
	Voice              string // empty: the engine's default
	Scenes             []Scene
}

// Narrated reports whether any scene has narration.
func (f *File) Narrated() bool {
	return slices.ContainsFunc(f.Scenes, func(s Scene) bool { return s.Narration != "" })
}

// Scene is one segment of the movie.
type Scene struct {
	ID              string
	Kind            Kind
	Title, Subtitle string  // card
	Source          string  // absolute path: the image, frames directory, or movie
	Rate            float64 // frames per second of a frames scene
	Duration        float64 // minimum hold of a card or image scene, seconds
	Narration       string  // whitespace collapsed; empty when silent
	// NarrationAtEnd delays the narration so it ends as the scene ends, where
	// a terminal take shows its result, instead of starting with the scene.
	NarrationAtEnd bool
}

// Problems lists everything wrong with a scene file.
type Problems []string

func (p Problems) Error() string { return "invalid scene file:\n  " + strings.Join(p, "\n  ") }

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func set(keys ...string) map[string]bool {
	m := map[string]bool{}
	for _, k := range keys {
		m[k] = true
	}
	return m
}

var (
	topKeys  = set("size", "fps", "engine", "voice", "scenes")
	kindKeys = map[Kind]map[string]bool{
		Card:   set("id", "card", "subtitle", "duration", "narration", "narration_at"),
		Image:  set("id", "image", "duration", "narration", "narration_at"),
		Frames: set("id", "frames", "rate", "narration", "narration_at"),
		Movie:  set("id", "movie"),
	}
)

// Load reads a scene file and reports every problem in it at once.
func Load(path string) (*File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, Problems{fmt.Sprintf("not valid YAML: %v", err)}
	}
	f := &File{Path: abs, Dir: filepath.Dir(abs), Width: 1920, Height: 1080, FPS: 30, Engine: "auto"}
	var p Problems
	for _, k := range sortedKeys(raw) {
		if !topKeys[k] {
			p = append(p, fmt.Sprintf("unknown top-level key %q", k))
		}
	}
	if v, ok := raw["size"]; ok {
		s, _ := v.(string)
		w, h, err := cli.ParseSize(s)
		switch {
		case err != nil:
			p = append(p, "size must be WxH, such as 1920x1080")
		case w%2 != 0 || h%2 != 0:
			p = append(p, "size must have even width and height (the encoder needs it)")
		default:
			f.Width, f.Height = w, h
		}
	}
	if v, ok := raw["fps"]; ok {
		if n, isInt := v.(int); isInt && n > 0 {
			f.FPS = n
		} else {
			p = append(p, "fps must be a positive whole number")
		}
	}
	if v, ok := raw["engine"]; ok {
		s, _ := v.(string)
		if slices.Contains([]string{"auto", "openai", "openai-chat", "piper"}, s) {
			f.Engine = s
		} else {
			p = append(p, "engine must be one of auto, openai, openai-chat, piper")
		}
	}
	if v, ok := raw["voice"]; ok {
		if s, isStr := v.(string); isStr && s != "" {
			f.Voice = s
		} else {
			p = append(p, "voice must be a voice name")
		}
	}
	list, _ := raw["scenes"].([]any)
	if len(list) == 0 {
		p = append(p, "scenes must be a list of at least one scene")
	}
	seen := map[string]bool{}
	for i, item := range list {
		sc, problems := parseScene(f, i, item)
		p = append(p, problems...)
		if sc.ID != "" {
			if seen[sc.ID] {
				p = append(p, fmt.Sprintf("scene %s: duplicate id", sc.ID))
			}
			seen[sc.ID] = true
		}
		f.Scenes = append(f.Scenes, sc)
	}
	if len(p) > 0 {
		return nil, p
	}
	return f, nil
}

func parseScene(f *File, index int, item any) (Scene, Problems) {
	name := fmt.Sprintf("scene %d", index+1)
	m, ok := item.(map[string]any)
	if !ok {
		return Scene{}, Problems{name + ": must be a mapping of fields"}
	}
	var p Problems
	var sc Scene
	if id, ok := m["id"].(string); ok && idPattern.MatchString(id) {
		sc.ID, name = id, "scene "+id
	} else {
		p = append(p, name+": id must match [a-z0-9][a-z0-9-]*")
	}
	var found []Kind
	for _, k := range kinds {
		if _, ok := m[string(k)]; ok {
			found = append(found, k)
		}
	}
	if len(found) != 1 {
		return sc, append(p, fmt.Sprintf("%s: needs exactly one of card, image, frames, movie (found %d)", name, len(found)))
	}
	sc.Kind = found[0]
	for _, k := range sortedKeys(m) {
		if !kindKeys[sc.Kind][k] {
			p = append(p, fmt.Sprintf("%s: %q is not a field of a %s scene", name, k, sc.Kind))
		}
	}
	text := func(key string) (string, bool) {
		v, ok := m[key]
		if !ok {
			return "", false
		}
		s, isStr := v.(string)
		if !isStr {
			p = append(p, fmt.Sprintf("%s: %s must be text", name, key))
		}
		return s, isStr
	}
	positive := func(key string, def float64) float64 {
		v, ok := m[key]
		if !ok {
			return def
		}
		n, isNum := number(v)
		if !isNum || !(n > 0) || math.IsInf(n, 0) {
			p = append(p, fmt.Sprintf("%s: %s must be a positive number", name, key))
			return def
		}
		return n
	}
	source := func(key string) string {
		s, ok := text(key)
		if !ok {
			return ""
		}
		if filepath.IsAbs(s) {
			return s
		}
		return filepath.Join(f.Dir, s)
	}
	switch sc.Kind {
	case Card:
		sc.Title, _ = text("card")
		sc.Subtitle, _ = text("subtitle")
		if missing := fonts.Missing([]*opentype.Font{fonts.Sans()}, sc.Title+" "+sc.Subtitle); len(missing) > 0 {
			p = append(p, fmt.Sprintf("%s: the card font cannot draw %s", name, fonts.Describe(missing)))
		}
		sc.Duration = positive("duration", 3)
	case Image:
		if sc.Source = source("image"); sc.Source != "" && !isFile(sc.Source) {
			p = append(p, fmt.Sprintf("%s: no such image %s", name, sc.Source))
		}
		sc.Duration = positive("duration", 3)
	case Frames:
		if sc.Source = source("frames"); sc.Source != "" && len(PNGs(sc.Source)) == 0 {
			p = append(p, fmt.Sprintf("%s: no PNG frames in %s", name, sc.Source))
		}
		rate := float64(f.FPS)
		if sc.Source != "" {
			if takeRate, err := takeJSONRate(sc.Source); err != nil {
				p = append(p, fmt.Sprintf("%s: %v", name, err))
			} else if takeRate > 0 {
				rate = takeRate
			}
		}
		sc.Rate = positive("rate", rate)
	case Movie:
		if sc.Source = source("movie"); sc.Source != "" && !isFile(sc.Source) {
			p = append(p, fmt.Sprintf("%s: no such movie %s", name, sc.Source))
		}
	}
	if sc.Kind != Movie {
		if n, ok := text("narration"); ok {
			sc.Narration = strings.Join(strings.Fields(n), " ")
		}
		if at, ok := text("narration_at"); ok {
			switch at {
			case "start":
			case "end":
				sc.NarrationAtEnd = true
			default:
				p = append(p, fmt.Sprintf("%s: narration_at must be start or end", name))
			}
		}
	}
	return sc, p
}

// takeJSONRate reads the frame rate `movie term` records in a take
// directory's take.json. It returns 0 when there is no take.json.
func takeJSONRate(dir string) (float64, error) {
	data, err := os.ReadFile(filepath.Join(dir, "take.json"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var take struct {
		Rate float64 `json:"rate"`
	}
	if err := json.Unmarshal(data, &take); err != nil || !(take.Rate > 0) {
		return 0, fmt.Errorf("unreadable take.json in %s: it needs a positive rate", dir)
	}
	return take.Rate, nil
}

// PNGs lists the PNG files in dir in lexical order. It reads the directory
// rather than globbing, so brackets or stars in the path mean nothing.
func PNGs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".png") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	slices.Sort(paths)
	return paths
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
