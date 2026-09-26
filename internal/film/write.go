package film

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
)

// Movie describes what Write makes: Tool names the command that wrote the
// scene file, Title and Subtitle make its title card when Title is set, and
// Size is the size of the drawn pictures.
type Movie struct {
	Tool            string
	Title, Subtitle string
	Size            image.Point
}

// Write renders each take into outdir/take-N/ at FPS, drawing each shot's
// picture as PNG with draw, and writes outdir/scenes.yaml for movie build.
// Each take first has its waiting time cut and its final picture held. It
// returns the take directories' names.
func Write(outdir string, m Movie, takes []Take, beats []Beat, draw func(Shot) ([]byte, error), stdout io.Writer) ([]string, error) {
	if err := cli.RequireEmptyDir(outdir); err != nil {
		return nil, err
	}
	if len(takes) == 0 {
		return nil, errors.New("nothing was filmed")
	}
	says := Narrations(len(takes), beats)
	var names []string
	for i, t := range takes {
		t = HoldLast(Tighten(t, Hold), Hold)
		name := fmt.Sprintf("take-%d", i+1)
		takeDir, err := filepath.Abs(filepath.Join(outdir, name))
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(takeDir, 0o755); err != nil {
			return nil, err
		}
		prev, picture := -1, []byte(nil)
		slots := Slots(t, FPS)
		for k, idx := range slots {
			if idx != prev {
				if picture, err = draw(t.Shots[idx]); err != nil {
					return nil, err
				}
				prev = idx
			}
			if err := os.WriteFile(filepath.Join(takeDir, fmt.Sprintf("f%05d.png", k)), picture, 0o644); err != nil {
				return nil, err
			}
		}
		meta, _ := json.MarshalIndent(map[string]any{"frames": takeDir, "rate": FPS,
			"settled": math.Round(Settled(t)*1000) / 1000}, "", "  ")
		if err := os.WriteFile(filepath.Join(takeDir, "take.json"), meta, 0o644); err != nil {
			return nil, err
		}
		fmt.Fprintf(stdout, "%s: %d frames, %.1fs -> %s\n", name, len(slots), t.End-t.Start, cli.ShortPath(takeDir))
		names = append(names, name)
	}
	scenesPath := filepath.Join(outdir, "scenes.yaml")
	if err := writeScenes(scenesPath, m, names, says); err != nil {
		return nil, err
	}
	narrated := 0
	for _, say := range says {
		if say != "" {
			narrated++
		}
	}
	fmt.Fprintf(stdout, "\nwrote %s: %d takes, %d narrated. Add or reword anything, then:\n  movie build %s OUT.mp4\n",
		cli.ShortPath(scenesPath), len(names), narrated, cli.ShortPath(scenesPath))
	return names, nil
}

// writeScenes writes a scene file for movie build beside the takes: the
// title card when there is a title, then one frames scene per take,
// narrated where the agent said something, with paths relative to the file.
func writeScenes(path string, m Movie, takes, says []string) error {
	type sceneOut struct {
		ID        string `yaml:"id"`
		Card      string `yaml:"card,omitempty"`
		Subtitle  string `yaml:"subtitle,omitempty"`
		Frames    string `yaml:"frames,omitempty"`
		Narration string `yaml:"narration,omitempty"`
	}
	file := struct {
		Size   string     `yaml:"size"`
		Scenes []sceneOut `yaml:"scenes"`
	}{Size: fmt.Sprintf("%dx%d", m.Size.X, m.Size.Y)}
	if m.Title != "" {
		file.Scenes = append(file.Scenes, sceneOut{ID: "title", Card: m.Title, Subtitle: m.Subtitle})
	}
	for i, name := range takes {
		file.Scenes = append(file.Scenes, sceneOut{ID: name, Frames: name, Narration: says[i]})
	}
	var data bytes.Buffer
	enc := yaml.NewEncoder(&data)
	enc.SetIndent(2)
	if err := enc.Encode(file); err != nil {
		return err
	}
	header := "# Written by " + m.Tool + ". Edit freely: reword narration, add image,\n" +
		"# card, or movie scenes. Build it with: movie build scenes.yaml OUT.mp4\n"
	return os.WriteFile(path, append([]byte(header), data.Bytes()...), 0o644)
}
