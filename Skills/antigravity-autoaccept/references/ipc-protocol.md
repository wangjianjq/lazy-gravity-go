# IPC Protocol Reference — antigravity-autoaccept

**Base URL:** `http://127.0.0.1:27182`  
**Transport:** HTTP/1.1 over TCP loopback  
**Auth:** None (loopback isolation is the security boundary)  
**Content-Type:** `application/json` for all POST bodies and responses

---

## Endpoints

### `GET /ping` — Health Check

Used by LazyGravity to determine whether to use IPC path or fall back to direct CDP.

**Request:** No body.

**Response:**
```json
{
  "ok": true,
  "sessions": 3,
  "workbenchReady": true
}
```

| Field | Type | Description |
|-------|------|-------------|
| `ok` | bool | Always true when server is running |
| `sessions` | int | Number of active CDP sessions injected by AutoAccept |
| `workbenchReady` | bool | `true` if the primary workbench target (vscode-file://*workbench*) is connected |

**Decision logic in Go client:**
```
workbenchReady == true  →  use IPC path (POST /inject + POST /eval)
workbenchReady == false →  fall back to direct CDP (InitCDP)
server unreachable      →  fall back to direct CDP (InitCDP)
```

**HTTP status codes:**
- `200 OK` — server running (always, even if sessions=0)
- Connection refused — IPC server not started (extension unloaded or IDE restarted)

---

### `POST /inject` — Inject Text into Antigravity Chat

Clears the chat input, types the provided text, and submits it (simulated Enter key).

**Request body:**
```json
{ "text": "Your message here\nWith possible newlines" }
```

| Field | Type | Constraints |
|-------|------|-------------|
| `text` | string | Required. Non-empty. No length limit enforced server-side. |

**Success response `200 OK`:**
```json
{ "ok": true }
```

**Error responses:**

`400 Bad Request` — missing or empty `text` field:
```json
{ "ok": false, "error": "Missing or empty text field" }
```

`500 Internal Server Error` — inject failed:
```json
{ "ok": false, "error": "No workbench CDP session — AutoAccept may not be connected yet" }
```
```json
{ "ok": false, "error": "No editor found" }
```

**Implementation notes:**
- Uses `document.execCommand('selectAll')` + `document.execCommand('insertText')` — does NOT use `Input.insertText` CDP domain
- Sends synthetic `KeyboardEvent` for Enter (keydown/keypress/keyup) — NOT `Input.dispatchKeyEvent`
- This approach works inside Electron Chromium without `Input` domain permissions
- The editor selector `div[role="textbox"]:not(.xterm-helper-textarea)` targets the last visible textbox

---

### `POST /eval` — Evaluate JavaScript in Workbench Context

Executes arbitrary JavaScript in the workbench page context and returns the result.
LazyGravity uses this to run `checkGeneratingScript` and `getResponseTextScript`.

**Request body:**
```json
{ "expression": "document.title" }
```

| Field | Type | Constraints |
|-------|------|-------------|
| `expression` | string | Required. Any synchronous JS expression returning a serializable value. |

**Success response `200 OK`:**
```json
{ "ok": true, "value": "GO - Antigravity - Antigravity 配额监控" }
```

The `value` field contains the JavaScript return value, JSON-serialized.
- Primitives: string, number, boolean, null
- Objects: serialized as JSON objects (must be JSON-serializable)
- `undefined` return → `value: null`

**Error responses:**

`400 Bad Request`:
```json
{ "ok": false, "error": "Missing expression" }
```

`500 Internal Server Error`:
```json
{ "ok": false, "error": "No workbench CDP session" }
```
```json
{ "ok": false, "error": "ipc backpressure: too many pending calls" }
```
```json
{ "ok": false, "error": "ipc timeout" }
```

**Usage patterns in LazyGravity:**

Check if Antigravity is generating:
```javascript
// expression value:
(() => {
    const panel = document.querySelector('.antigravity-agent-side-panel');
    const scopes = [panel, document].filter(Boolean);
    for (const scope of scopes) {
        const el = scope.querySelector('[data-tooltip-id="input-send-button-cancel-tooltip"]');
        if (el) return { isGenerating: true };
    }
    // ... stop button text checks ...
    return { isGenerating: false };
})()
```

Get latest response text:
```javascript
// expression value:
(() => {
    const selectors = ['.rendered-markdown', '.leading-relaxed.select-text', ...];
    // ... reverse traversal to find last assistant message ...
    return text; // string or null
})()
```

---

## Timing & Retry Recommendations

| Operation | HTTP Client | Recommended timeout | Retry strategy |
|-----------|-------------|---------------------|----------------|
| `/ping` | `ipcHTTP` | 3 s | Every message, no retry needed (fast fail) |
| `/inject` | `ipcHTTP` | 3 s (shared) | Retry once if `No editor found` |
| `/eval` (check generating) | `evalHTTP` | 35 s | Retry up to 3 times on error, then fail |
| `/eval` (get response text) | `evalHTTP` | 35 s | No retry (called only after generating stops) |

> ⚠️ **CRITICAL:** `/eval` MUST use a dedicated HTTP client with 35s timeout.
> Using the 3s `ipcHTTP` client for eval will cause spurious timeouts during AI generation.
> See SKILL.md 坑#1 for the full explanation and fix.

**Monitor polling interval:** Every 2 seconds.  
**Generation completion threshold:** 3 consecutive polls with `isGenerating: false`.

---

## Security Notes

- The IPC server binds to `127.0.0.1` (loopback only). It is **not accessible** from the network.
- No authentication is implemented. Any local process can call these endpoints.
- The `Access-Control-Allow-Origin` header is set to `127.0.0.1` (not `*`).
- `eval` endpoint is intentionally unrestricted — it executes arbitrary JS in the workbench. This is acceptable because both the caller (LazyGravity) and the server (AutoAccept) run under the same local user account.

---

## Compatibility

| AutoAccept Version | IPC Support | Notes |
|--------------------|-------------|-------|
| < 3.13.0 | ❌ No | IPC server not present |
| ≥ 3.13.8 | ✅ Yes | `/ping`, `/inject`, `/eval` all available |
| Future | ✅ Expected | `/upload` endpoint planned for image support |
