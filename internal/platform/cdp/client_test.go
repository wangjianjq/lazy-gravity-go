package cdp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Test helpers: fake WebSocket server
// ---------------------------------------------------------------------------

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// echoServer echoes back a successful CDP response for every message it receives.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req map[string]interface{}
			if err := json.Unmarshal(msg, &req); err != nil {
				return
			}
			id, _ := req["id"].(float64)
			resp := map[string]interface{}{
				"id":     id,
				"result": map[string]interface{}{"value": "hello"},
			}
			if err := conn.WriteJSON(resp); err != nil {
				return
			}
		}
	}))
}

// errorServer returns a CDP error payload for every request.
func errorServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req map[string]interface{}
			if err := json.Unmarshal(msg, &req); err != nil {
				return
			}
			id, _ := req["id"].(float64)
			resp := map[string]interface{}{
				"id": id,
				"error": map[string]interface{}{
					"code":    -32700,
					"message": "Parse error",
				},
			}
			if err := conn.WriteJSON(resp); err != nil {
				return
			}
		}
	}))
}

// wsURL converts an httptest server URL (http://...) to a ws:// URL.
func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// ---------------------------------------------------------------------------
// Tests: NewClient / IsConnected
// ---------------------------------------------------------------------------

func TestNewClient_InitialState(t *testing.T) {
	c := NewClient()
	if c == nil {
		t.Fatal("NewClient() returned nil")
	}
	if c.IsConnected() {
		t.Error("new client should not be connected")
	}
	if c.pendingCalls == nil {
		t.Error("pendingCalls map should be initialised")
	}
}

// ---------------------------------------------------------------------------
// Tests: Connect / Disconnect
// ---------------------------------------------------------------------------

func TestConnect_Success(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewClient()
	if err := c.Connect(wsURL(srv)); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	if !c.IsConnected() {
		t.Error("expected IsConnected() == true after successful Connect")
	}

	c.Disconnect()
	// give readLoop goroutine time to exit
	time.Sleep(50 * time.Millisecond)
	if c.IsConnected() {
		t.Error("expected IsConnected() == false after Disconnect")
	}
}

func TestConnect_BadURL(t *testing.T) {
	c := NewClient()
	err := c.Connect("ws://127.0.0.1:1") // nothing listening
	if err == nil {
		t.Fatal("expected error connecting to closed port, got nil")
	}
}

func TestDisconnect_Idempotent(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewClient()
	if err := c.Connect(wsURL(srv)); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	// Double-disconnect must not panic
	c.Disconnect()
	c.Disconnect()
}

// ---------------------------------------------------------------------------
// Tests: Call — happy path
// ---------------------------------------------------------------------------

func TestCall_Success(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewClient()
	if err := c.Connect(wsURL(srv)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Disconnect()

	ctx := context.Background()
	result, err := c.Call(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression": "1+1",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result map")
	}
}

func TestCall_NilParams(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewClient()
	if err := c.Connect(wsURL(srv)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Disconnect()

	ctx := context.Background()
	// nil params should be treated as an empty map, not panic
	_, err := c.Call(ctx, "Runtime.enable", nil)
	if err != nil {
		t.Fatalf("Call with nil params failed: %v", err)
	}
}

func TestCall_SequentialIDIncrement(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewClient()
	if err := c.Connect(wsURL(srv)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Disconnect()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := c.Call(ctx, "Ping", nil); err != nil {
			t.Fatalf("Call #%d failed: %v", i, err)
		}
	}
	c.mu.Lock()
	finalID := c.msgID
	c.mu.Unlock()
	if finalID != 5 {
		t.Errorf("expected msgID == 5 after 5 calls, got %d", finalID)
	}
}

// ---------------------------------------------------------------------------
// Tests: Call — error path
// ---------------------------------------------------------------------------

func TestCall_CDPErrorResponse(t *testing.T) {
	srv := errorServer(t)
	defer srv.Close()

	c := NewClient()
	if err := c.Connect(wsURL(srv)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Disconnect()

	ctx := context.Background()
	_, err := c.Call(ctx, "Runtime.evaluate", nil)
	if err == nil {
		t.Fatal("expected CDP error, got nil")
	}
	if !strings.Contains(err.Error(), "CDP error") {
		t.Errorf("expected 'CDP error' in message, got: %v", err)
	}
}

func TestCall_WhileDisconnected(t *testing.T) {
	c := NewClient() // never connected
	ctx := context.Background()
	_, err := c.Call(ctx, "Runtime.enable", nil)
	if err == nil {
		t.Fatal("expected error calling on disconnected client")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Call — context cancellation
// ---------------------------------------------------------------------------

func TestCall_ContextCancelled(t *testing.T) {
	// Use a server that accepts the connection but never responds.
	silentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Just read messages and drop them — never send a response.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer silentSrv.Close()

	c := NewClient()
	if err := c.Connect(wsURL(silentSrv)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Disconnect()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := c.Call(ctx, "Runtime.evaluate", nil)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("expected context error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: concurrent Call safety
// ---------------------------------------------------------------------------

func TestCall_Concurrent(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewClient()
	if err := c.Connect(wsURL(srv)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Disconnect()

	ctx := context.Background()
	const n = 20
	errCh := make(chan error, n)

	for i := 0; i < n; i++ {
		go func() {
			_, err := c.Call(ctx, "Runtime.evaluate", map[string]interface{}{"expression": "42"})
			errCh <- err
		}()
	}

	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent Call #%d failed: %v", i, err)
		}
	}
}
