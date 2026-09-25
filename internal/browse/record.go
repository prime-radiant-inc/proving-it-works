package browse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/cdp"
	"github.com/prime-radiant-inc/proving-it-works/internal/film"
)

// Record films the session's page until stop asks it to finish or the
// browser goes away. It adds the overlay to every document the page loads
// and puts each screencast frame on a film.Reel. Problems go to log
// (recorder.log).
func Record(dir string, log io.Writer) error {
	s, err := Load(dir)
	if err != nil {
		return err
	}
	reel, err := film.NewReel(s.Dir)
	if err != nil {
		return err
	}
	defer os.WriteFile(filepath.Join(s.Dir, "recorder.done"), nil, 0o644)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	conn, err := cdp.Dial(ctx, s.Browser.WebSocket)
	if err != nil {
		return err
	}
	defer conn.Close()
	session, err := conn.Attach(ctx, s.Page)
	if err != nil {
		return err
	}
	frames := make(chan json.RawMessage, 16)
	conn.OnEvent(func(e cdp.Event) {
		if e.Session != session || e.Method != "Page.screencastFrame" {
			return
		}
		// Chrome sends the next frame only once this one is acknowledged.
		// Acknowledge off the connection's reader, which must never wait.
		var id struct {
			Session int `json:"sessionId"`
		}
		json.Unmarshal(e.Params, &id)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
			defer cancel()
			if err := conn.Call(ctx, session, "Page.screencastFrameAck", map[string]int{"sessionId": id.Session}, nil); err != nil {
				fmt.Fprintf(log, "acknowledging a frame: %v\n", err)
			}
		}()
		frames <- e.Params
	})
	for _, c := range []struct {
		method string
		params any
	}{
		{"Page.enable", nil},
		{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": overlay}},
		{"Runtime.evaluate", map[string]any{"expression": overlay}},
		{"Page.startScreencast", map[string]any{"format": "png", "everyNthFrame": 1}},
	} {
		if err := conn.Call(ctx, session, c.method, c.params, nil); err != nil {
			return err
		}
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case raw := <-frames:
			var f struct {
				Data     string `json:"data"`
				Metadata struct {
					Timestamp float64 `json:"timestamp"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(raw, &f); err != nil {
				fmt.Fprintf(log, "unreadable frame: %v\n", err)
				continue
			}
			png, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil {
				fmt.Fprintf(log, "undecodable frame: %v\n", err)
				continue
			}
			t := f.Metadata.Timestamp
			if t == 0 {
				t = film.Now()
			}
			if err := reel.Add(t, png); err != nil {
				return err
			}
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(s.Dir, "stop")); err == nil {
				return reel.End(film.Now(), "")
			}
		case <-conn.Done():
			return reel.End(film.Now(), "the browser closed or crashed")
		}
	}
}
