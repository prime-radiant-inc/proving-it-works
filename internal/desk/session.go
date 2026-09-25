// Package desk films a desktop app on an X11 display: xdotool is the hands,
// ffmpeg's x11grab the camera, and the verbs drive the app one action at a
// time, as browse drives a page. With a wrapper such as docker exec, the
// display, the app, xdotool, and ffmpeg live in the container and the
// recording stays here.
package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/film"
)

// region is the part of the display being filmed, in screen pixels.
type region struct{ X, Y, W, H int }

// screen turns a point in what is filmed, as read off a shot, into a point
// on the screen.
func (r region) screen(x, y int) (image.Point, error) {
	if x < 0 || y < 0 || x >= r.W || y >= r.H {
		return image.Point{}, fmt.Errorf("%d,%d is outside the filmed %dx%d", x, y, r.W, r.H)
	}
	return image.Pt(r.X+x, r.Y+y), nil
}

// Session is one filmed display, described by SESSION/session.json.
type Session struct {
	Dir     string   `json:"-"`
	Display string   `json:"display"`
	Wrapper []string `json:"wrapper,omitempty"`
	Region  region   `json:"region"`
	// Title and Subtitle become the title card of the scene file stop writes.
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
}

// Load reads a session's description.
func Load(dir string) (*Session, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(abs, "session.json"))
	if err != nil {
		return nil, fmt.Errorf("no movie desk session at %s (start one with: movie desk start %s)", dir, dir)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s/session.json: %w", dir, err)
	}
	s.Dir = abs
	return &s, nil
}

func (s *Session) save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, "session.json"), data, 0o644)
}

func (s *Session) set() film.Set { return film.Set{Dir: s.Dir} }

// command builds a command that runs on the session's display, through the
// wrapper when there is one.
func (s *Session) command(ctx context.Context, args ...string) *exec.Cmd {
	argv := append(append(append([]string{}, s.Wrapper...), "env", "DISPLAY="+s.Display), args...)
	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

// run runs a command on the display and returns its output. Nothing a verb
// runs should take long; timeout bounds anything that hangs.
func (s *Session) run(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := s.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s did not finish within %s", args[0], timeout)
		}
		return "", fmt.Errorf("%s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (s *Session) xdotool(args ...string) (string, error) {
	return s.run(30*time.Second, append([]string{"xdotool"}, args...)...)
}

// shellValues parses xdotool's --shell output: KEY=VALUE lines.
func shellValues(out string) map[string]int {
	values := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if n, err := strconv.Atoi(v); ok && err == nil {
			values[k] = n
		}
	}
	return values
}

// pointer is where the pointer is on the screen.
func (s *Session) pointer() (image.Point, error) {
	out, err := s.xdotool("getmouselocation", "--shell")
	if err != nil {
		return image.Point{}, err
	}
	v := shellValues(out)
	return image.Pt(v["X"], v["Y"]), nil
}

// glide is the xdotool commands that move the pointer from one screen point
// to another in 15 steps 25 ms apart, so the movie shows it travel.
func glide(from, to image.Point) []string {
	const steps = 15
	var cmd []string
	for i := 1; i <= steps; i++ {
		p := from.Add(to.Sub(from).Mul(i).Div(steps))
		cmd = append(cmd, "mousemove", strconv.Itoa(p.X), strconv.Itoa(p.Y), "sleep", "0.025")
	}
	return cmd
}

// press is the xdotool commands for a click that arrives after the pointer
// has hovered a moment and holds the button briefly: apps such as Blender
// ignore a click that arrives with the pointer or is released at once.
func press(button int, double bool) []string {
	b := strconv.Itoa(button)
	cmd := []string{"sleep", "0.2", "mousedown", b, "sleep", "0.12", "mouseup", b}
	if double {
		cmd = append(cmd, "sleep", "0.08", "mousedown", b, "sleep", "0.12", "mouseup", b)
	}
	return cmd
}
