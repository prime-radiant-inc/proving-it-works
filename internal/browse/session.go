// Package browse films a web app: a headless Chrome holds the page, the
// verbs drive it one action at a time, a detached recorder films it through
// the DevTools screencast, and render turns what it filmed into takes.
package browse

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/cdp"
	"github.com/prime-radiant-inc/proving-it-works/internal/jsonl"
)

//go:embed overlay.js
var overlay string

// Session is one filmed browser, described by SESSION/session.json.
type Session struct {
	Dir     string      `json:"-"`
	Browser cdp.Browser `json:"browser"`
	Page    string      `json:"page"`
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
		return nil, fmt.Errorf("no movie browse session at %s (start one with: movie browse start %s URL)", dir, dir)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s/session.json: %w", dir, err)
	}
	s.Dir = abs
	return &s, nil
}

func (s *Session) save() error {
	return writeJSON(filepath.Join(s.Dir, "session.json"), s)
}

// writeJSON replaces path with v, atomically.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// state is what the verbs remember between them, in SESSION/state.json:
// the number of the take being filmed (from 1), whether filming is on, and
// whether the last action ended a narrated beat, so the next one cuts.
type state struct {
	Take       int  `json:"take"`
	Film       bool `json:"film"`
	CutPending bool `json:"cut_pending"`
}

func (s *Session) state() (state, error) {
	var st state
	data, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(data, &st)
}

func (s *Session) setState(st state) error {
	return writeJSON(filepath.Join(s.Dir, "state.json"), st)
}

// mark records, from now on, whether filming is on and whether an action is
// under way.
func (s *Session) mark(film, busy bool) error {
	return jsonl.Append(filepath.Join(s.Dir, "marks.jsonl"), Mark{T: now(), Film: film, Busy: busy})
}

func now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// page is a connection to the session's page, for one verb.
type page struct {
	conn    *cdp.Conn
	session string
}

// callTimeout bounds each protocol call a verb makes.
const callTimeout = 30 * time.Second

// open attaches to the session's page and makes sure the overlay is in it.
func (s *Session) open() (*page, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	conn, err := cdp.Dial(ctx, s.Browser.WebSocket)
	if err != nil {
		return nil, fmt.Errorf("the session's browser is not answering (was it stopped?): %w", err)
	}
	session, err := conn.Attach(ctx, s.Page)
	if err != nil {
		conn.Close()
		return nil, err
	}
	p := &page{conn: conn, session: session}
	if err := p.injectOverlay(); err != nil {
		conn.Close()
		return nil, err
	}
	return p, nil
}

func (p *page) close() { p.conn.Close() }

func (p *page) call(method string, params, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return p.conn.Call(ctx, p.session, method, params, result)
}

// eval evaluates a JavaScript expression in the page, awaiting a promise,
// and decodes its value into result when result is not nil.
func (p *page) eval(expression string, result any) error {
	var got struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	err := p.call("Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}, &got)
	if err != nil {
		return err
	}
	if got.Exception != nil {
		msg := got.Exception.Exception.Description
		if msg == "" {
			msg = got.Exception.Text
		}
		return errors.New(strings.TrimPrefix(strings.SplitN(msg, "\n", 2)[0], "Error: "))
	}
	if result != nil && len(got.Result.Value) > 0 {
		return json.Unmarshal(got.Result.Value, result)
	}
	return nil
}

// injectOverlay runs the overlay in the current document. The recorder
// also adds it to every new document; running it again does nothing.
func (p *page) injectOverlay() error { return p.eval(overlay, nil) }

// js quotes s as a JavaScript string.
func js(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}
