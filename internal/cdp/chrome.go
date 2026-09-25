package cdp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// macBrowsers and pathBrowsers are where FindBrowser looks, in order.
var (
	macBrowsers = []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	}
	pathBrowsers = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"}
)

// FindBrowser returns explicit when it names an executable, or else the
// first Chrome, Chromium, or Edge installed.
func FindBrowser(explicit string) (string, error) {
	if explicit != "" {
		path, err := exec.LookPath(explicit)
		if err != nil {
			return "", fmt.Errorf("--browser %s: not an executable", explicit)
		}
		return path, nil
	}
	for _, path := range macBrowsers {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	for _, name := range pathBrowsers {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("no Chrome, Chromium, or Edge found: install one, or pass --browser PATH")
}

// Options shape a launched browser. The viewport is Width x Height CSS
// pixels, drawn at Scale device pixels per CSS pixel (1 when 0). Profile is
// a new directory for the browser's profile; Log receives its output.
type Options struct {
	Profile       string
	Width, Height int
	Scale         float64
	Log           string
}

// Browser is a running browser: its process, and the DevTools websocket URL
// to Dial.
type Browser struct {
	PID       int    `json:"pid"`
	WebSocket string `json:"websocket"`
}

// Launch starts a headless browser in its own session, so it outlives the
// command that started it, and waits for its DevTools endpoint.
func Launch(path string, o Options) (*Browser, error) {
	if err := os.MkdirAll(o.Profile, 0o755); err != nil {
		return nil, err
	}
	args := []string{
		"--headless=new",
		"--user-data-dir=" + o.Profile,
		"--remote-debugging-port=0", // Chrome picks a free port and writes it to DevToolsActivePort
		fmt.Sprintf("--window-size=%d,%d", o.Width, o.Height),
		"--hide-scrollbars", "--mute-audio",
		"--no-first-run", "--no-default-browser-check", "--disable-extensions",
		"--disable-background-networking", "--disable-sync", "--password-store=basic", "--use-mock-keychain",
	}
	if o.Scale != 0 {
		args = append(args, "--force-device-scale-factor="+strconv.FormatFloat(o.Scale, 'f', -1, 64))
	}
	if os.Geteuid() == 0 {
		args = append(args, "--no-sandbox") // Chrome refuses to run as root with its sandbox
	}
	args = append(args, "about:blank")
	log, err := os.Create(o.Log)
	if err != nil {
		return nil, err
	}
	defer log.Close()
	cmd := exec.Command(path, args...)
	cmd.Stdout, cmd.Stderr = log, log
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", path, err)
	}
	b := &Browser{PID: cmd.Process.Pid}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	port := filepath.Join(o.Profile, "DevToolsActivePort")
	deadline := time.After(30 * time.Second)
	for {
		if data, err := os.ReadFile(port); err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) == 2 {
				b.WebSocket = "ws://127.0.0.1:" + lines[0] + lines[1]
				return b, nil
			}
		}
		select {
		case err := <-exited:
			return nil, fmt.Errorf("%s exited before it was ready (%v); see %s", path, err, o.Log)
		case <-deadline:
			b.Kill()
			return nil, fmt.Errorf("%s did not open its DevTools port within 30 s; see %s", path, o.Log)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
