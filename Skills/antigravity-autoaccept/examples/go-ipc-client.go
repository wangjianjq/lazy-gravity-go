//go:build ignore
// +build ignore
//
// This file is a reference example — copy the functions you need into
// internal/platform/cdp/antigravity.go rather than importing this file directly.
// The build tags above exclude it from 'go build ./...' and 'go test ./...'.

// Package cdp — IPC client for AntiGravity AutoAccept extension.
// This file is part of the antigravity-autoaccept skill examples.
// Copy the relevant functions into internal/platform/cdp/antigravity.go.

package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// ─── Constants ───────────────────────────────────────────────────────────────

const ipcBase = "http://127.0.0.1:27182"

// ipcHTTP is a dedicated client with a short timeout for health checks.
// A separate extended client is used for /eval calls during generation monitoring.
var ipcHTTP = &http.Client{Timeout: 3 * time.Second}

// evalHTTP has a longer timeout for /eval calls (generation can take minutes).
var evalHTTP = &http.Client{Timeout: 35 * time.Second}

// ─── IsAutoAcceptAvailable ───────────────────────────────────────────────────

// IsAutoAcceptAvailable returns true only when BOTH conditions are met:
//  1. The IPC HTTP server is reachable on :27182
//  2. The AutoAccept extension has an active workbench CDP session ready
//
// Call this once per message, before deciding whether to use the IPC path.
// Do NOT call this inside a polling loop — it makes an outbound HTTP request.
func IsAutoAcceptAvailable() bool {
	resp, err := ipcHTTP.Get(ipcBase + "/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var result struct {
		OK             bool `json:"ok"`
		WorkbenchReady bool `json:"workbenchReady"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}

	return result.OK && result.WorkbenchReady
}

// ─── ipcInject ───────────────────────────────────────────────────────────────

// ipcInject sends text to AutoAccept's /inject endpoint.
// AutoAccept clears the chat input, types the text, and submits (simulated Enter).
func ipcInject(text string) error {
	body, _ := json.Marshal(map[string]string{"text": text})

	resp, err := ipcHTTP.Post(ipcBase+"/inject", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("IPC inject request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("IPC inject response parse error: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("IPC inject error: %s", result.Error)
	}
	return nil
}

// InjectViaAutoAccept is the exported entry point used by app.go.
func InjectViaAutoAccept(text string) error { return ipcInject(text) }

// ─── ipcEval ─────────────────────────────────────────────────────────────────

// ipcEval executes a JavaScript expression in the workbench context via AutoAccept.
// The expression must be synchronous and return a JSON-serializable value.
//
// This is used to poll generation status and extract response text without
// opening a competing CDP WebSocket to workbench.html.
func ipcEval(expression string) (interface{}, error) {
	body, _ := json.Marshal(map[string]string{"expression": expression})

	resp, err := evalHTTP.Post(ipcBase+"/eval", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("IPC eval request failed: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)

	var result struct {
		OK    bool        `json:"ok"`
		Value interface{} `json:"value"`
		Error string      `json:"error"`
	}
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return nil, fmt.Errorf("IPC eval parse error: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("IPC eval error: %s", result.Error)
	}
	return result.Value, nil
}

// ─── MonitorResponseViaIPC ───────────────────────────────────────────────────

// MonitorResponseViaIPC polls Antigravity's generation state via AutoAccept's CDP session.
//
// Algorithm:
//  1. Poll checkGeneratingScript every 2 seconds via /eval
//  2. When isGenerating=false appears 3 consecutive times, generation is complete
//  3. Fetch response text via /eval
//  4. Return the response text
//
// Retry policy: up to 3 consecutive eval errors before giving up.
func MonitorResponseViaIPC(ctx context.Context, timeout time.Duration) (string, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeoutChan := time.After(timeout)

	stopGoneCount := 0
	errCount := 0

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()

		case <-timeoutChan:
			return "", fmt.Errorf("timeout waiting for generation to complete")

		case <-ticker.C:
			val, err := ipcEval(checkGeneratingScript)
			if err != nil {
				log.Printf("[IPC] Eval checkGenerating error: %v", err)
				errCount++
				if errCount >= 3 {
					return "", fmt.Errorf("IPC eval repeatedly failed: %w", err)
				}
				continue
			}
			errCount = 0

			isGenerating := false
			if valMap, ok := val.(map[string]interface{}); ok {
				if g, ok2 := valMap["isGenerating"].(bool); ok2 {
					isGenerating = g
				}
			}

			if isGenerating {
				stopGoneCount = 0
				continue
			}

			stopGoneCount++
			if stopGoneCount >= 3 {
				// Generation complete — extract response text
				textVal, err := ipcEval(getResponseTextScript)
				if err != nil {
					return "", fmt.Errorf("IPC get response text failed: %w", err)
				}
				if textStr, isStr := textVal.(string); isStr && textStr != "" {
					return textStr, nil
				}
				return "", fmt.Errorf("IPC response text not found or empty")
			}
		}
	}
}
