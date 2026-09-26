package desk

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/cli"
	"github.com/prime-radiant-inc/proving-it-works/internal/film"
)

// capture is the ffmpeg command that films the session's region: 10 PNGs a
// second, pointer drawn, written to stdout as each is made.
func (s *Session) capture(ctx context.Context, frames int) []string {
	args := []string{"ffmpeg", "-nostdin", "-v", "error", "-f", "x11grab", "-draw_mouse", "1", "-framerate", "10",
		"-video_size", fmt.Sprintf("%dx%d", s.Region.W, s.Region.H),
		"-i", fmt.Sprintf("%s+%d,%d", s.Display, s.Region.X, s.Region.Y)}
	if frames > 0 {
		args = append(args, "-frames:v", fmt.Sprint(frames))
	}
	return append(args, "-c:v", "png", "-f", "image2pipe", "-flush_packets", "1", "-")
}

// Record films the session until stop asks it to finish or the capture
// ends on its own, stamping each picture when it arrives and putting it on
// a film.Reel. Problems go to log (recorder.log).
func Record(dir string, log io.Writer) error {
	s, err := Load(dir)
	if err != nil {
		return err
	}
	defer os.WriteFile(filepath.Join(s.Dir, "recorder.done"), nil, 0o644)
	reel, err := film.NewReel(s.Dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A shell prints ffmpeg's process id on the display's side, then becomes
	// ffmpeg: through a wrapper, ending the local command does not end it
	// there, so stop kills it by that id.
	cmd := s.command(ctx, append([]string{"sh", "-c", `echo $$; exec "$@"`, "sh"}, s.capture(ctx, 0)...)...)
	var stderr strings.Builder
	cmd.Stderr = io.MultiWriter(log, &stderr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	type shot struct {
		t       float64
		picture []byte
	}
	shots := make(chan shot, 16)
	ended := make(chan error, 1)
	stream := bufio.NewReader(out)
	line, err := stream.ReadString('\n')
	pid := strings.TrimSpace(line)
	if _, convErr := strconv.Atoi(pid); err != nil || convErr != nil {
		cmd.Wait()
		return reel.End(film.Now(), "the capture failed: "+cli.FirstLine(stderr.String(), "it did not start"))
	}
	go func() {
		p := newPictures(stream)
		for {
			picture, err := p.next()
			if err != nil {
				ended <- err
				return
			}
			shots <- shot{film.Now(), picture}
		}
	}()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case sh := <-shots:
			if err := reel.Add(sh.t, sh.picture); err != nil {
				return err
			}
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(s.Dir, "stop")); err == nil {
				// Stop returns once this recorder is done, so the capture must
				// be gone by then, not merely told to go. The shell's own kill
				// needs no procps. Waiting on the command reaps a local ffmpeg,
				// and a docker exec returns once the process in it has exited.
				s.run(10*time.Second, "sh", "-c", `kill "$1"`, "sh", pid)
				exited := make(chan struct{})
				go func() { cmd.Wait(); close(exited) }()
				select {
				case <-exited:
				case <-time.After(5 * time.Second):
					fmt.Fprintln(log, "the capture did not exit within 5 s of being killed")
				}
				cancel()
				return reel.End(film.Now(), "")
			}
		case err := <-ended:
			cmd.Wait()
			return reel.End(film.Now(), "the capture failed: "+cli.FirstLine(stderr.String(), err.Error()))
		}
	}
}
