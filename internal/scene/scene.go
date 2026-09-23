// Package scene reads and validates a movie's scene file.
package scene

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
)

// Kind is what a scene shows.
type Kind string

const Image Kind = "image"

// File is a parsed scene file.
type File struct {
	Path               string // the scene file
	Dir                string // paths in the file are relative to this
	Width, Height, FPS int
	Scenes             []Scene
}

// Scene is one segment of the movie.
type Scene struct {
	ID       string
	Kind     Kind
	Source   string  // absolute path of the image
	Duration float64 // minimum hold, seconds
}

// Load reads a scene file.
func Load(path string) (*File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Size   string `yaml:"size"`
		FPS    int    `yaml:"fps"`
		Scenes []struct {
			ID       string  `yaml:"id"`
			Image    string  `yaml:"image"`
			Duration float64 `yaml:"duration"`
		} `yaml:"scenes"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f := &File{Path: abs, Dir: filepath.Dir(abs), Width: 1920, Height: 1080, FPS: 30}
	if raw.Size != "" {
		if f.Width, f.Height, err = cli.ParseSize(raw.Size); err != nil {
			return nil, err
		}
	}
	if raw.FPS > 0 {
		f.FPS = raw.FPS
	}
	for _, s := range raw.Scenes {
		d := s.Duration
		if d <= 0 {
			d = 3
		}
		f.Scenes = append(f.Scenes, Scene{ID: s.ID, Kind: Image, Source: filepath.Join(f.Dir, s.Image), Duration: d})
	}
	return f, nil
}
