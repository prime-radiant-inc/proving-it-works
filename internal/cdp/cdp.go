// Package cdp is a small Chrome DevTools Protocol client: one connection to
// the browser, flat sessions for pages, calls matched to replies by id, and
// events handed to a callback. It also finds and launches Chrome.
package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
)

// Event is a protocol event, from the page session named by Session or from
// the browser when Session is "".
type Event struct {
	Session string
	Method  string
	Params  json.RawMessage
}

// Conn is a connection to the browser's DevTools endpoint.
type Conn struct {
	ws      *websocket.Conn
	next    atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan message
	onEvent func(Event)
	done    chan struct{}
	err     error // why the connection ended; read once done is closed
}

type message struct {
	ID      int64           `json:"id,omitempty"`
	Session string          `json:"sessionId,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Dial connects to a browser's DevTools websocket URL.
func Dial(ctx context.Context, url string) (*Conn, error) {
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to the browser: %w", err)
	}
	// Screencast frames and page text arrive as single messages of several
	// megabytes; the library's default limit is 32 KiB.
	ws.SetReadLimit(256 << 20)
	c := &Conn{ws: ws, pending: map[int64]chan message{}, done: make(chan struct{})}
	go c.read()
	return c, nil
}

func (c *Conn) read() {
	for {
		_, data, err := c.ws.Read(context.Background())
		if err != nil {
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			close(c.done)
			return
		}
		var m message
		if err := json.Unmarshal(data, &m); err != nil {
			continue // not protocol traffic; nothing is waiting on it
		}
		if m.ID != 0 {
			c.mu.Lock()
			reply, ok := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ok {
				reply <- m
			}
			continue
		}
		c.mu.Lock()
		handler := c.onEvent
		c.mu.Unlock()
		if handler != nil {
			handler(Event{Session: m.Session, Method: m.Method, Params: m.Params})
		}
	}
}

// OnEvent sets the function every event is handed to. It runs on the
// connection's reader, so it must not wait on a Call.
func (c *Conn) OnEvent(f func(Event)) {
	c.mu.Lock()
	c.onEvent = f
	c.mu.Unlock()
}

// Done is closed when the connection ends, as when the browser exits.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Call sends method with params to session ("" for the browser) and decodes
// the reply's result into result, when result is not nil.
func (c *Conn) Call(ctx context.Context, session, method string, params, result any) error {
	id := c.next.Add(1)
	m := message{ID: id, Session: session, Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		m.Params = raw
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	reply := make(chan message, 1)
	c.mu.Lock()
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	select {
	case r := <-reply:
		if r.Error != nil {
			return fmt.Errorf("%s: %s (%d)", method, r.Error.Message, r.Error.Code)
		}
		if result != nil {
			return json.Unmarshal(r.Result, result)
		}
		return nil
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()
		return fmt.Errorf("%s: the browser connection closed: %w", method, c.err)
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

// Close ends the connection. Pages this connection attached to stay open.
func (c *Conn) Close() error { return c.ws.CloseNow() }

// Page returns the id of the browser's first page.
func (c *Conn) Page(ctx context.Context) (string, error) {
	var got struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	if err := c.Call(ctx, "", "Target.getTargets", nil, &got); err != nil {
		return "", err
	}
	for _, t := range got.TargetInfos {
		if t.Type == "page" {
			return t.TargetID, nil
		}
	}
	return "", errors.New("the browser has no page open")
}

// Attach opens a session on the page with id target and returns the
// session's id, for Call.
func (c *Conn) Attach(ctx context.Context, target string) (string, error) {
	var got struct {
		SessionID string `json:"sessionId"`
	}
	err := c.Call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true}, &got)
	return got.SessionID, err
}
