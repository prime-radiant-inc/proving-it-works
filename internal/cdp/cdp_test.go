package cdp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// launch starts a headless Chrome for one test and closes it when t ends,
// skipping the test when no Chrome is installed.
func launch(t *testing.T) *Browser {
	t.Helper()
	path, err := FindBrowser("")
	if err != nil {
		t.Skip(err)
	}
	b, err := Launch(path, Options{Profile: filepath.Join(t.TempDir(), "profile"), Width: 640, Height: 360,
		Log: filepath.Join(t.TempDir(), "chrome.log")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Kill() })
	return b
}

func dial(t *testing.T, b *Browser) *Conn {
	t.Helper()
	c, err := Dial(context.Background(), b.WebSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestAttachEvaluateAndReceiveEvents(t *testing.T) {
	b := launch(t)
	c := dial(t, b)
	page, err := c.Page(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	session, err := c.Attach(ctx(t), page)
	if err != nil {
		t.Fatal(err)
	}
	loaded := make(chan struct{}, 1)
	c.OnEvent(func(e Event) {
		if e.Session == session && e.Method == "Page.loadEventFired" {
			select {
			case loaded <- struct{}{}:
			default:
			}
		}
	})
	if err := c.Call(ctx(t), session, "Page.enable", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx(t), session, "Page.navigate", map[string]string{"url": "data:text/html,<title>hi</title>"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-loaded:
	case <-time.After(10 * time.Second):
		t.Fatal("no Page.loadEventFired")
	}
	var got struct {
		Result struct{ Value string }
	}
	if err := c.Call(ctx(t), session, "Runtime.evaluate", map[string]any{"expression": "document.title + (1+1)", "returnByValue": true}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Result.Value != "hi2" {
		t.Fatalf("evaluate gave %+v", got)
	}
}

// Verbs are separate processes: each attaches to the page, acts, and closes
// its connection. The page must outlive every one of them.
func TestThePageOutlivesAConnectionThatAttachedToIt(t *testing.T) {
	b := launch(t)
	first := dial(t, b)
	page, err := first.Page(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	session, err := first.Attach(ctx(t), page)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Call(ctx(t), session, "Runtime.evaluate", map[string]any{"expression": "window.mark = 'kept'"}, nil); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second := dial(t, b)
	again, err := second.Page(ctx(t))
	if err != nil || again != page {
		t.Fatalf("page %q after the first connection closed, want %q (%v)", again, page, err)
	}
	session, err = second.Attach(ctx(t), page)
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Result struct{ Value string } }
	if err := second.Call(ctx(t), session, "Runtime.evaluate", map[string]any{"expression": "window.mark", "returnByValue": true}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Result.Value != "kept" {
		t.Fatalf("the page was replaced: window.mark is %q", got.Result.Value)
	}
}

func TestProtocolErrorsNameTheMethod(t *testing.T) {
	b := launch(t)
	c := dial(t, b)
	err := c.Call(ctx(t), "", "No.suchMethod", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "No.suchMethod") || !strings.Contains(err.Error(), "wasn't found") {
		t.Fatalf("got %v", err)
	}
}

func TestCallsFailOnceTheBrowserIsGone(t *testing.T) {
	b := launch(t)
	c := dial(t, b)
	b.Kill()
	select {
	case <-c.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the connection never noticed the browser exit")
	}
	if err := c.Call(ctx(t), "", "Browser.getVersion", nil, nil); err == nil {
		t.Fatal("a call on a closed connection succeeded")
	}
}

func TestLargeMessagesArrive(t *testing.T) {
	b := launch(t)
	c := dial(t, b)
	page, _ := c.Page(ctx(t))
	session, err := c.Attach(ctx(t), page)
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Result struct{ Value json.RawMessage } }
	if err := c.Call(ctx(t), session, "Runtime.evaluate", map[string]any{"expression": "'x'.repeat(4<<20)", "returnByValue": true}, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Result.Value) < 4<<20 {
		t.Fatalf("got %d bytes", len(got.Result.Value))
	}
}

func TestFindBrowserHonoursAnExplicitPath(t *testing.T) {
	_, err := FindBrowser("/no/such/chrome")
	if err == nil || !strings.Contains(err.Error(), "/no/such/chrome") {
		t.Fatalf("got %v", err)
	}
}
