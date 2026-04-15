package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	goRuntime "runtime"
	"net/http"
	"time"
)

var injectMessageScript = `(() => {
    const editors = Array.from(document.querySelectorAll('div[role="textbox"]:not(.xterm-helper-textarea)'));
    const visible = editors.filter(el => el.offsetParent !== null);
    const editor = visible[visible.length - 1];
    if (!editor) return { ok: false, error: 'No editor found' };
    editor.focus();
    return { ok: true };
})()`

var checkGeneratingScript = `(() => {
	const panel = document.querySelector('.antigravity-agent-side-panel');
	const scopes = [panel, document].filter(Boolean);
	for (const scope of scopes) {
		const el = scope.querySelector('[data-tooltip-id="input-send-button-cancel-tooltip"]');
		if (el) return { isGenerating: true };
	}
	const normalize = (value) => (value || '').toLowerCase().replace(/\s+/g, ' ').trim();
	const STOP_PATTERNS = [/^stop$/, /^stop generating$/, /^stop response$/, /^停止$/, /^生成を停止$/, /^応答を停止$/];
	const isStopLabel = (value) => {
		const normalized = normalize(value);
		if (!normalized) return false;
		return STOP_PATTERNS.some((re) => re.test(normalized));
	};
	for (const scope of scopes) {
		const buttons = scope.querySelectorAll('button, [role="button"]');
		for (let i = 0; i < buttons.length; i++) {
			const btn = buttons[i];
			const labels = [
				btn.textContent || '',
				btn.getAttribute('aria-label') || '',
				btn.getAttribute('title') || '',
			];
			if (labels.some(isStopLabel)) {
				return { isGenerating: true };
			}
		}
	}
	return { isGenerating: false };
})()`

var getResponseTextScript = `(() => {
	const panel = document.querySelector('.antigravity-agent-side-panel');
	const scopes = [panel, document].filter(Boolean);
	const selectors = [
		'.rendered-markdown',
		'.leading-relaxed.select-text',
		'.flex.flex-col.gap-y-3',
		'[data-message-author-role="assistant"]',
		'[data-message-role="assistant"]',
		'[class*="assistant-message"]',
		'[class*="message-content"]',
		'[class*="markdown-body"]',
		'[class*="markdown-content"]',
		'.prose',
		'.markdown',
	];
	const looksLikeActivityLog = (text) => {
		const norm = (text||'').trim().toLowerCase();
		if(!norm) return false;
		if(/^(?:analy[sz]ing|reading|writing|running|searching|planning|thinking|processing|executing|working|calling)/.test(norm) && norm.length <= 250) return true;
		if(/^thought for/.test(norm) || /^thinking/.test(norm)) return true;
		return false;
	};
	const looksLikeToolOutput = (text) => {
		const first = (text||'').trim().split('\n')[0]||'';
		if(/^[a-z0-9._-]+\s*\/\s*[a-z0-9._-]+$/i.test(first)) return true;
		if(/^full output written to /i.test(first)) return true;
		return false;
	};
	const combinedSelector = selectors.join(', ');
	const seen = new Set();
	for (const scope of scopes) {
		const nodes = scope.querySelectorAll(combinedSelector);
		for (let i=nodes.length-1; i>=0; i--) {
			const node = nodes[i];
			if(!node || seen.has(node)) continue;
			seen.add(node);
			if(node.closest('details') || node.closest('footer') || node.closest('.notify-user-container')) continue;
			const text = (node.innerText || node.textContent || '').replace(/\r/g,'').trim();
			if(!text || text.length < 2) continue;
			if(looksLikeActivityLog(text)) continue;
			if(looksLikeToolOutput(text)) continue;
			return text;
		}
	}
	return null;
})()`

// ─── AutoAccept IPC layer ────────────────────────────────────────────────────
// When AutoAccept Dashboard plugin is running it exposes a local HTTP server on
// port 27182. LazyGravity uses this instead of opening a competing WebSocket to
// the same CDP target (workbench.html), avoiding mutual kick-off.

const ipcBase = "http://127.0.0.1:27182"

// ipcHTTP is used for fast health checks (/ping) and short-lived operations (/inject).
// A 3-second timeout is sufficient because these calls should complete instantly.
var ipcHTTP = &http.Client{Timeout: 3 * time.Second}

// evalHTTP is used exclusively for /eval calls.
// AI generation can take tens of seconds; a longer timeout prevents spurious failures.
// See Skill SKILL.md 坑#1: never use ipcHTTP for /eval.
var evalHTTP = &http.Client{Timeout: 35 * time.Second}

// IsAutoAcceptAvailable returns true when the AutoAccept IPC server is reachable
// AND has an active workbench CDP session ready.
func IsAutoAcceptAvailable() bool {
	resp, err := ipcHTTP.Get(ipcBase + "/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
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

// ipcInject sends a text injection request to AutoAccept.
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

// InjectViaAutoAccept is the exported entry point for app.go to delegate text injection.
func InjectViaAutoAccept(text string) error { return ipcInject(text) }

// ipcEval runs a JS expression in the workbench context via AutoAccept.
// Uses evalHTTP (35s timeout) because AI generation can keep the call pending
// for many seconds. Do NOT use ipcHTTP here — see Skill 坑#1.
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

// MonitorResponseViaIPC polls generation status through AutoAccept's CDP connection.
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

			valMap, ok := val.(map[string]interface{})
			isGenerating := false
			if ok {
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
				textVal, err := ipcEval(getResponseTextScript)
				if err != nil {
					return "", fmt.Errorf("IPC get response text failed: %w", err)
				}
				if textStr, isStr := textVal.(string); isStr {
					return textStr, nil
				}
				return "", fmt.Errorf("IPC response text not found or empty")
			}
		}
	}
}

// InitCDP initializes the CDP system within a given context.
func InitCDP(ctx context.Context) (*Client, error) {
	client := NewClient()

	wsURL, err := client.Discover()
	if err != nil {
		return nil, fmt.Errorf("failed to discover Antigravity: %w", err)
	}

	if err := client.Connect(wsURL); err != nil {
		return nil, fmt.Errorf("failed to connect to CDP: %w", err)
	}

	// Initialize Runtime
	if _, err := client.Call(ctx, "Runtime.enable", nil); err != nil {
		client.Disconnect()
		return nil, fmt.Errorf("failed to enable CDP runtime: %w", err)
	}

	return client, nil
}

// Evaluate runs a javascript expression in the connected context.
func Evaluate(ctx context.Context, client *Client, expression string) (interface{}, error) {
	res, err := client.Call(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return nil, err
	}

	resultMap, ok := res["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid result format")
	}

	if value, hasValue := resultMap["value"]; hasValue {
		return value, nil
	}

	return nil, nil
}

// clearInputField selects all text and deletes it
func clearInputField(ctx context.Context, client *Client) error {
	// CDP modifier bitmask: 1=Alt, 2=Ctrl, 4=Meta, 8=Shift
	// Windows: Ctrl+A (modifier=2), macOS: Cmd+A (modifier=4)
	selectAllModifier := 2 // Ctrl (Windows/Linux)
	if goRuntime.GOOS == "darwin" {
		selectAllModifier = 4 // Meta (macOS Cmd)
	}

	client.Call(ctx, "Input.dispatchKeyEvent", map[string]interface{}{
		"type":                  "keyDown",
		"key":                   "a",
		"code":                  "KeyA",
		"modifiers":             selectAllModifier,
		"windowsVirtualKeyCode": 65,
		"nativeVirtualKeyCode":  65,
	})
	client.Call(ctx, "Input.dispatchKeyEvent", map[string]interface{}{"type": "keyUp", "key": "a", "code": "KeyA", "modifiers": selectAllModifier, "windowsVirtualKeyCode": 65, "nativeVirtualKeyCode": 65})
	client.Call(ctx, "Input.dispatchKeyEvent", map[string]interface{}{"type": "keyDown", "key": "Backspace", "code": "Backspace", "windowsVirtualKeyCode": 8, "nativeVirtualKeyCode": 8})
	client.Call(ctx, "Input.dispatchKeyEvent", map[string]interface{}{"type": "keyUp", "key": "Backspace", "code": "Backspace", "windowsVirtualKeyCode": 8, "nativeVirtualKeyCode": 8})
	time.Sleep(50 * time.Millisecond)
	return nil
}

var locateInputScript = `(async () => {
	const wait = (ms) => new Promise(resolve => setTimeout(resolve, ms));
	const visible = (el) => {
		if (!el) return false;
		if (el.offsetParent !== null) return true;
		const style = window.getComputedStyle(el);
		if (!style) return false;
		if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') return false;
		const rect = typeof el.getBoundingClientRect === 'function' ? el.getBoundingClientRect() : null;
		return !!rect && rect.width > 0 && rect.height > 0;
	};
	const normalize = (v) => (v || '').toLowerCase();
	const hasImageAccept = (input) => {
		const accept = normalize(input.getAttribute('accept'));
		return !accept || accept.includes('image') || accept.includes('*/*');
	};
	const findInput = () => {
		const inputs = Array.from(document.querySelectorAll('input[type="file"]'));
		const visibleInput = inputs.find(i => visible(i) && hasImageAccept(i));
		if (visibleInput) return visibleInput;
		return inputs.find(hasImageAccept) || null;
	};

	let input = findInput();
	if (!input) {
		const triggerKeywords = ['attach', 'upload', 'image', 'file', 'paperclip', 'plus'];
		const triggers = Array.from(document.querySelectorAll('button, [role="button"]'))
			.filter(visible)
			.filter((el) => {
				const text = normalize(el.textContent);
				const aria = normalize(el.getAttribute('aria-label'));
				const title = normalize(el.getAttribute('title'));
				const cls = normalize(el.getAttribute('class'));
				const all = [text, aria, title, cls].join(' ');
				return triggerKeywords.some(k => all.includes(k));
			})
			.slice(-8);

		for (const trigger of triggers) {
			if (typeof trigger.click === 'function') {
				trigger.click();
				await wait(150);
				input = findInput();
				if (input) break;
			}
		}
	}

	if (!input) {
		return { ok: false, error: 'Image upload input not found' };
	}

	const token = 'agclaw-upload-' + Math.random().toString(36).slice(2, 10);
	input.setAttribute('data-agclaw-upload-token', token);
	return { ok: true, token: token };
})()`

func attachImageFiles(ctx context.Context, client *Client, filePaths []string) error {
	if len(filePaths) == 0 {
		return nil
	}

	_, err := client.Call(ctx, "DOM.enable", nil)
	if err != nil {
		return fmt.Errorf("failed to enable DOM: %w", err)
	}

	val, err := Evaluate(ctx, client, locateInputScript)
	if err != nil {
		return err
	}
	valMap, ok := val.(map[string]interface{})
	if !ok || valMap["ok"] != true {
		return fmt.Errorf("failed to locate frontend file input")
	}

	token, _ := valMap["token"].(string)

	docRes, err := client.Call(ctx, "DOM.getDocument", map[string]interface{}{"depth": 1, "pierce": true})
	if err != nil {
		return fmt.Errorf("failed to get document: %w", err)
	}
	rootMap, _ := docRes["root"].(map[string]interface{})
	rootNodeIdFloat, _ := rootMap["nodeId"].(float64)

	queryRes, err := client.Call(ctx, "DOM.querySelector", map[string]interface{}{
		"nodeId":   int(rootNodeIdFloat),
		"selector": fmt.Sprintf("input[data-agclaw-upload-token=\"%s\"]", token),
	})
	if err != nil {
		return fmt.Errorf("failed to select input node: %w", err)
	}
	nodeIdFloat, _ := queryRes["nodeId"].(float64)
	if nodeIdFloat == 0 {
		return fmt.Errorf("failed to extract input node ID")
	}

	_, err = client.Call(ctx, "DOM.setFileInputFiles", map[string]interface{}{
		"nodeId": int(nodeIdFloat),
		"files":  filePaths,
	})
	if err != nil {
		return fmt.Errorf("failed to set files: %w", err)
	}

	notifyScript := fmt.Sprintf(`(() => {
		const input = document.querySelector('input[data-agclaw-upload-token="%s"]');
		if (input) input.removeAttribute('data-agclaw-upload-token');
		return { ok: true };
	})()`, token)
	Evaluate(ctx, client, notifyScript)
	time.Sleep(250 * time.Millisecond)
	return nil
}

func InjectMessageWithImages(ctx context.Context, client *Client, text string, imagePaths []string) error {
	// ── IPC path: delegate to AutoAccept to avoid CDP WebSocket competition ──
	// Images are not yet supported over IPC; fall through to direct CDP for those.
	if len(imagePaths) == 0 && IsAutoAcceptAvailable() {
		if err := ipcInject(text); err == nil {
			return nil
		} else {
			log.Printf("[IPC] inject failed, falling back to direct CDP: %v", err)
		}
	}
	// ── CDP path (fallback) ──────────────────────────────────────────────────
	return injectViaCDP(ctx, client, text, imagePaths)
}

// injectViaCDP is the original direct-CDP implementation, kept for fallback.
func injectViaCDP(ctx context.Context, client *Client, text string, imagePaths []string) error {
	val, err := Evaluate(ctx, client, injectMessageScript)
	if err != nil {
		return err
	}

	valMap, ok := val.(map[string]interface{})
	if !ok || valMap["ok"] != true {
		return fmt.Errorf("failed to focus chat input")
	}

	clearInputField(ctx, client)

	err = attachImageFiles(ctx, client, imagePaths)
	if err != nil {
		return fmt.Errorf("image attachment failed: %w", err)
	}

	// Send text
	if text != "" {
		_, err = client.Call(ctx, "Input.insertText", map[string]interface{}{"text": text})
		if err != nil {
			return err
		}
	}

	time.Sleep(200 * time.Millisecond)

	// Press Enter
	client.Call(ctx, "Input.dispatchKeyEvent", map[string]interface{}{"type": "keyDown", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13})
	client.Call(ctx, "Input.dispatchKeyEvent", map[string]interface{}{"type": "keyUp", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13})

	return nil
}

// MonitorResponse waits for Antigravity to finish generating and returns the final text.
func MonitorResponse(ctx context.Context, client *Client, timeout time.Duration) (string, error) {
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
			val, err := Evaluate(ctx, client, checkGeneratingScript)
			if err != nil {
				log.Printf("[CDP] Evaluate checkGeneratingScript error: %v", err)
				errCount++
				if errCount >= 3 {
					return "", fmt.Errorf("CDP disconnected or max evaluate errors reached: %w", err)
				}
				continue
			}
			errCount = 0

			valMap, ok := val.(map[string]interface{})
			isGenerating := false
			if ok {
				if g, ok2 := valMap["isGenerating"].(bool); ok2 {
					isGenerating = g
				}
			}

			if isGenerating {
				stopGoneCount = 0
				// still generating
				continue
			}

			// Stop button gone
			stopGoneCount++
			if stopGoneCount >= 3 {
				// Generation complete, get text
				textVal, err := Evaluate(ctx, client, getResponseTextScript)
				if err != nil {
					return "", fmt.Errorf("failed to extract response: %w", err)
				}
				if textStr, isStr := textVal.(string); isStr {
					return textStr, nil
				}
				return "", fmt.Errorf("response text not found or empty")
			}
		}
	}
}
