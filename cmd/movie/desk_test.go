package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// deskContainer starts a container from the skill's linux-desktop example,
// with the X11 demo apps added, an 800x600 display on :99, and xcalc on it, removed when t ends, and returns the wrapper that
// runs commands in it. It skips without Docker.
func deskContainer(t *testing.T) []string {
	t.Helper()
	// Windows runners' Docker runs Windows containers, which have no X11
	if out, err := exec.Command("docker", "info", "--format", "{{.OSType}}").Output(); err != nil || strings.TrimSpace(string(out)) != "linux" {
		t.Skip("needs Docker running Linux containers")
	}
	if out, err := exec.Command("docker", "build", "-q", "-t", "movie-desk-test", "--build-arg", "APPS=x11-apps",
		"../../skills/proving-it-works-with-a-movie/examples/linux-desktop").CombinedOutput(); err != nil {
		t.Fatalf("building the test image: %v\n%s", err, out)
	}
	name := fmt.Sprintf("movie-desk-test-%d", rand.IntN(1e9))
	if out, err := exec.Command("docker", "run", "-d", "--init", "--name", name, "movie-desk-test",
		"Xvfb", ":99", "-screen", "0", "800x600x24").CombinedOutput(); err != nil {
		t.Fatalf("starting the container: %v\n%s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	for deadline := time.Now().Add(10 * time.Second); exec.Command("docker", "exec", "-e", "DISPLAY=:99", name, "xdotool", "getdisplaygeometry").Run() != nil; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("Xvfb never came up")
		}
	}
	exec.Command("docker", "exec", "-d", "-e", "DISPLAY=:99", name, "xcalc").Run()
	return []string{"docker", "exec", "-e", "DISPLAY=:99", name}
}

// inBox runs a command in the test container.
func inBox(wrapper []string, args ...string) error {
	argv := append(append([]string{}, wrapper[1:]...), args...)
	return exec.Command(wrapper[0], argv...).Run()
}

func startDesk(t *testing.T, dir string, wrapper []string, args ...string) string {
	t.Helper()
	session := filepath.Join(dir, "session")
	t.Cleanup(func() {
		// the recorder notices the container going away; wait for its last write
		done := filepath.Join(session, "recorder.done")
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if _, err := os.Stat(done); err == nil {
				return
			}
		}
	})
	argv := append(append([]string{"desk", "start", session, "--display", ":99"}, args...), "--")
	r := runMovie(t, dir, append(argv, wrapper...)...)
	if r.code != 0 {
		t.Fatalf("start: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	return session
}

func mustDesk(t *testing.T, dir string, args ...string) result {
	t.Helper()
	r := runMovie(t, dir, append([]string{"desk"}, args...)...)
	if r.code != 0 {
		t.Fatalf("desk %q: code %d\n%s%s", args, r.code, r.stdout, r.stderr)
	}
	return r
}

func TestDeskDrivesAndFilmsADesktopApp(t *testing.T) {
	wrapper := deskContainer(t)
	dir := t.TempDir()
	// away from 0,0, so filmed coordinates and screen coordinates differ
	if err := inBox(wrapper, "xdotool", "search", "--sync", "--onlyvisible", "--name", "Calculator", "windowmove", "100", "50"); err != nil {
		t.Fatal(err)
	}
	session := startDesk(t, dir, wrapper, "--window", "Calculator", "--title", "xcalc")

	r := mustDesk(t, dir, "shot", session)
	m := regexp.MustCompile(`(\S+\.png) \((\d+)x(\d+)\), pointer at `).FindStringSubmatch(r.stdout)
	if m == nil {
		t.Fatalf("shot printed %q", r.stdout)
	}
	shot := readPNG(t, m[1])
	if fmt.Sprint(shot.Bounds().Dx(), "x", shot.Bounds().Dy()) != m[2]+"x"+m[3] {
		t.Fatalf("shot is %v, printed %sx%s", shot.Bounds(), m[2], m[3])
	}

	r = mustDesk(t, dir, "move", session, "20", "20")
	mustDesk(t, dir, "type", session, "12*34=")
	// narrating a result nobody has looked at is refused
	r = runMovie(t, dir, "desk", "say", session, "It shows 408.")
	if r.code != 2 || !strings.Contains(r.stderr, "take a shot") {
		t.Errorf("say before looking: code %d\n%s", r.code, r.stderr)
	}
	mustDesk(t, dir, "shot", session)
	mustDesk(t, dir, "say", session, "We multiply twelve by thirty-four.")
	r = mustDesk(t, dir, "click", session, "30", "10")
	if !strings.Contains(r.stdout, "clicked at 30,10") {
		t.Errorf("click printed %q", r.stdout)
	}
	mustDesk(t, dir, "drag", session, "30", "10", "60", "12")
	mustDesk(t, dir, "key", session, "Escape")
	mustDesk(t, dir, "wait", session, "--quiet", "0.5", "--timeout", "5")
	mustDesk(t, dir, "shot", session)
	mustDesk(t, dir, "say", session, "Then clear it.")

	takes := filepath.Join(dir, "takes")
	r = mustDesk(t, dir, "stop", session, takes)
	if inBox(wrapper, "pgrep", "-f", "x11grab") == nil {
		t.Error("stop left the capture running in the container")
	}
	if !strings.Contains(r.stdout, "2 takes, 2 narrated") {
		t.Fatalf("stop printed:\n%s", r.stdout)
	}
	scenes, _ := os.ReadFile(filepath.Join(takes, "scenes.yaml"))
	for _, want := range []string{"# Written by movie desk stop.", "card: xcalc", "narration: We multiply twelve by thirty-four."} {
		if !strings.Contains(string(scenes), want) {
			t.Errorf("scenes.yaml lacks %q:\n%s", want, scenes)
		}
	}
	frames, _ := filepath.Glob(filepath.Join(takes, "take-1", "f*.png"))
	if len(frames) < 15 {
		t.Fatalf("take-1 has %d frames", len(frames))
	}
	first, last := readPNG(t, frames[0]), readPNG(t, frames[len(frames)-1])
	if first.Bounds() != shot.Bounds() {
		t.Errorf("frames are %v, the shot %v", first.Bounds(), shot.Bounds())
	}
	if samePicture(first, last) {
		t.Error("take 1 should end on the product, not the calculator it opened on")
	}
}

func TestDeskRefusesWhatItCannotDo(t *testing.T) {
	wrapper := deskContainer(t)
	dir := t.TempDir()
	r := runMovie(t, dir, append([]string{"desk", "start", filepath.Join(dir, "a"), "--"}, wrapper...)...)
	if r.code != 2 || !strings.Contains(r.stderr, "--display") {
		t.Errorf("wrapped without --display: code %d\n%s", r.code, r.stderr)
	}
	r = runMovie(t, dir, append([]string{"desk", "start", filepath.Join(dir, "b"), "--display", ":99", "--window", "No Such Window", "--"}, wrapper...)...)
	if r.code != 2 || !strings.Contains(r.stderr, `no visible window named "No Such Window"`) {
		t.Errorf("a missing window: code %d\n%s", r.code, r.stderr)
	}
	session := startDesk(t, dir, wrapper)
	r = runMovie(t, dir, "desk", "click", session, "5000", "5")
	if r.code != 2 || !strings.Contains(r.stderr, "outside the filmed 800x600") {
		t.Errorf("a click off the display: code %d\n%s", r.code, r.stderr)
	}
	r = runMovie(t, dir, "desk", "click", session, "5", "5", "--say", "nothing")
	if r.code != 2 {
		t.Errorf("--say, which desk does not have: code %d", r.code)
	}
	r = mustDesk(t, dir, "stop", session, filepath.Join(dir, "takes"))
	if strings.Contains(r.stdout, "WARN") {
		t.Errorf("stop warned:\n%s", r.stdout)
	}
	r = runMovie(t, dir, "desk", "click", session, "5", "5")
	if r.code != 2 || !strings.Contains(r.stderr, "this session is stopped") {
		t.Errorf("an action after stop: code %d\n%s", r.code, r.stderr)
	}
	r = runMovie(t, dir, "desk", "stop", session, filepath.Join(dir, "again"))
	if r.code != 2 || !strings.Contains(r.stderr, "already stopped") {
		t.Errorf("a second stop: code %d\n%s", r.code, r.stderr)
	}
}

// A capture that cannot run, or stops running, is reported, never filmed past.
func TestDeskNoticesADeadCamera(t *testing.T) {
	wrapper := deskContainer(t)
	dir := t.TempDir()
	// a window hanging off the screen cannot be captured
	inBox(wrapper, "xdotool", "search", "--sync", "--onlyvisible", "--name", "Calculator", "windowmove", "700", "500")
	gone := filepath.Join(dir, "off")
	r := runMovie(t, dir, append([]string{"desk", "start", gone, "--display", ":99", "--window", "Calculator", "--"}, wrapper...)...)
	if r.code != 2 || !strings.Contains(r.stderr, "the capture failed") || !strings.Contains(r.stderr, "outside the screen") {
		t.Errorf("an uncapturable window: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	if entries, _ := os.ReadDir(gone); len(entries) > 0 {
		t.Errorf("a failed start left %d files behind", len(entries))
	}
	session := startDesk(t, dir, wrapper)
	inBox(wrapper, "pkill", "-f", "x11grab")
	time.Sleep(time.Second)
	r = runMovie(t, dir, "desk", "click", session, "10", "10")
	if r.code != 2 || !strings.Contains(r.stderr, "the recorder has stopped") {
		t.Errorf("an action after the capture died: code %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

// Filming stops at even sizes, which the encoder needs, and holds still
// only when nothing in view moves.
func TestDeskFilmsEvenSizesAndSeesMotion(t *testing.T) {
	wrapper := deskContainer(t)
	dir := t.TempDir()
	inBox(wrapper, "sh", "-c", "xeyes -geometry 151x101+300+300 & xclock -update 1 -geometry 120x120+500+50 &")
	session := startDesk(t, dir, wrapper, "--window", "xeyes")
	r := mustDesk(t, dir, "shot", session)
	if !strings.Contains(r.stdout, "(150x100)") {
		t.Errorf("a 151x101 window should film at 150x100: %s", r.stdout)
	}
	// the whole display holds a ticking clock, so it never holds still
	whole := filepath.Join(dir, "whole")
	r = runMovie(t, dir, append([]string{"desk", "start", whole, "--display", ":99", "--"}, wrapper...)...)
	if r.code != 0 {
		t.Fatalf("start: %d %s", r.code, r.stderr)
	}
	r = runMovie(t, dir, "desk", "wait", whole, "--quiet", "1.5", "--timeout", "4")
	if r.code != 1 || !strings.Contains(r.stderr, "did not hold still") {
		t.Errorf("a wait over a ticking clock: code %d\n%s", r.code, r.stderr)
	}
	mustDesk(t, dir, "wait", session, "--quiet", "1.5", "--timeout", "6") // xeyes alone holds still
	mustDesk(t, dir, "stop", whole, filepath.Join(dir, "whole-takes"))
	mustDesk(t, dir, "stop", session, filepath.Join(dir, "takes"))
}
