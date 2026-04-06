package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var cdpPorts = []int{9222, 9223, 9224, 9225, 9226, 9227, 9228, 9229, 9230, 9333}

type Client struct {
	mu           sync.Mutex
	ws           *websocket.Conn
	msgID        int
	pendingCalls map[int]chan callResult
}

type callResult struct {
	Result map[string]interface{}
	Error  error
}

func NewClient() *Client {
	return &Client{
		pendingCalls: make(map[int]chan callResult),
	}
}

// Discover finds the Antigravity websocket URL by scanning known CDP ports.
func (c *Client) Discover() (string, error) {
	httpClient := &http.Client{Timeout: time.Second * 1}

	type portResult struct {
		pages []map[string]interface{}
	}

	results := make(chan portResult, len(cdpPorts))
	var wg sync.WaitGroup

	for _, port := range cdpPorts {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			resp, err := httpClient.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", p))
			if err != nil {
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var pages []map[string]interface{}
			if err := json.Unmarshal(body, &pages); err == nil && len(pages) > 0 {
				results <- portResult{pages: pages}
			}
		}(port)
	}

	// Close results channel once all goroutines are done
	go func() {
		wg.Wait()
		close(results)
	}()

	var allPages []map[string]interface{}
	for r := range results {
		allPages = append(allPages, r.pages...)
	}

	for _, page := range allPages {
		url, _ := page["url"].(string)
		title, _ := page["title"].(string)
		wsURL, _ := page["webSocketDebuggerUrl"].(string)

		if wsURL != "" {
			if strings.Contains(url, "workbench") || strings.Contains(title, "Antigravity") || strings.Contains(title, "Cascade") {
				if !strings.Contains(title, "Launchpad") && !strings.Contains(url, "workbench-jetski-agent") {
					return wsURL, nil
				}
			}
		}
	}

	// Fallback 1: Less strict check
	for _, page := range allPages {
		url, _ := page["url"].(string)
		title, _ := page["title"].(string)
		wsURL, _ := page["webSocketDebuggerUrl"].(string)

		if wsURL != "" && (strings.Contains(url, "workbench") || strings.Contains(title, "Cascade") || strings.Contains(title, "Antigravity")) {
			if !strings.Contains(title, "Launchpad") {
				return wsURL, nil
			}
		}
	}

	return "", fmt.Errorf("CDP target not found on any port")
}

func (c *Client) Connect(wsURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	c.ws = conn

	go c.readLoop()
	return nil
}

func (c *Client) readLoop() {
	for {
		c.mu.Lock()
		ws := c.ws
		c.mu.Unlock()
		if ws == nil {
			break
		}

		_, msg, err := ws.ReadMessage()
		if err != nil {
			c.mu.Lock()
			for id, ch := range c.pendingCalls {
				ch <- callResult{Error: err}
				close(ch)
				delete(c.pendingCalls, id)
			}
			c.ws = nil
			c.mu.Unlock()
			break
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(msg, &resp); err == nil {
			if idFloat, ok := resp["id"].(float64); ok {
				id := int(idFloat)
				c.mu.Lock()
				if ch, found := c.pendingCalls[id]; found {
					delete(c.pendingCalls, id)
					c.mu.Unlock()

					// Check for CDP protocol error
					if errObj, hasErr := resp["error"].(map[string]interface{}); hasErr {
						errMsg, _ := errObj["message"].(string)
						ch <- callResult{Error: fmt.Errorf("CDP error: %s", errMsg)}
					} else {
						result, _ := resp["result"].(map[string]interface{})
						ch <- callResult{Result: result}
					}
					close(ch)
				} else {
					c.mu.Unlock()
				}
			}
		}
	}
}

func (c *Client) Call(ctx context.Context, method string, params map[string]interface{}) (map[string]interface{}, error) {
	c.mu.Lock()
	if c.ws == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("websocket is not connected")
	}

	c.msgID++
	id := c.msgID
	ch := make(chan callResult, 1)
	c.pendingCalls[id] = ch

	if params == nil {
		params = make(map[string]interface{})
	}
	req := map[string]interface{}{
		"id":     id,
		"method": method,
		"params": params,
	}
	err := c.ws.WriteJSON(req)
	c.mu.Unlock()

	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pendingCalls, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case res := <-ch:
		return res.Result, res.Error
	}
}

func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws != nil {
		c.ws.Close()
		c.ws = nil
	}
}

func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws != nil
}
