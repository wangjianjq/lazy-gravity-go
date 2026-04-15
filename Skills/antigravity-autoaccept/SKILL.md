---
name: antigravity-autoaccept
version: "2.0.0"
author: LazyGravity / WJ
license: MIT
tags:
  - cdp
  - ipc
  - antigravity
  - autoaccept
  - electron
  - vscode-extension
  - lazygravity
  - go
  - wails
description: |
  This skill documents how to integrate a Go/Wails application (LazyGravity) with
  the AntiGravity AutoAccept VS Code extension via an IPC HTTP bridge, eliminating
  CDP WebSocket competition for the workbench.html target.

  Trigger this skill when:
  - Setting up LazyGravity bot CDP integration with Antigravity IDE
  - Diagnosing "CDP WebSocket conflict" or "connection kicked" errors
  - Implementing IPC delegation between a Go process and the AutoAccept plugin
  - Verifying port usage on :9222, :9333, :27182
  - Adding /inject, /eval, /ping endpoints to a VS Code extension
  - Adapting a Go CDP client to use IPC instead of direct WebSocket
  - Debugging ipcEval timeout during long AI generation (see Gotcha #1)
  - IDE launch race conditions on startup
  - Concurrent message processing with mutex vs IPC path
dependencies:
  go_packages:
    - net/http
    - encoding/json
    - sync
    - sync/atomic
  node_packages:
    - http (builtin)
    - worker_threads (builtin)
  ide:
    - Antigravity IDE with --remote-debugging-port=9222 flag
    - AntiGravity AutoAccept extension v3.13.8+
changelog:
  - version: "2.0.0"
    date: "2026-04-15"
    changes:
      - "新增「已知陷阱」章节（坑#1~#8）"
      - "坑#1: ipcEval 应用 evalHTTP(35s) 而非 ipcHTTP(3s)"
      - "坑#2: InjectMessageWithImages 中的双重检查层说明"
      - "坑#3: IDE 启动竞态缓解策略"
      - "坑#7: 并发 worker + cdpMu 设计决策"
      - "新增「成功经验」章节（L1~L5）"
      - "补充 Troubleshooting 表格（eval 超时、短响应过滤等）"
      - "修正 go test 路径说明"
      - "更新目录结构说明"
  - version: "1.0.0"
    date: "2026-04-07"
    changes:
      - "初版发布：IPC 委托桥接完整设计"
---

# antigravity-autoaccept IPC Integration Skill

## 📐 Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                     USER / TELEGRAM                     │
└──────────────────────────┬──────────────────────────────┘
                           │ message
                           ▼
┌─────────────────────────────────────────────────────────┐
│              LazyGravity (Go / Wails App)               │
│                                                         │
│  IsAutoAcceptAvailable()                                │
│    GET http://127.0.0.1:27182/ping                      │
│    {"ok":true, "workbenchReady":true}                   │
│         │ YES                    │ NO (IPC offline)     │
│         ▼                       ▼                       │
│  POST /inject            InitCDP() → Discover()         │
│  POST /eval              Direct WebSocket (fallback)    │
└────────┬───────────────────────┬────────────────────────┘
         │ IPC (HTTP loopback)   │ CDP WebSocket (fallback)
         ▼                       ▼
┌────────────────────────────────────────────────────────┐
│        AntiGravity AutoAccept Extension (Node.js)      │
│                                                        │
│  IPC Server :27182                                     │
│    GET  /ping   → {ok, sessions, workbenchReady}       │
│    POST /inject → injectText(text) via CDP             │
│    POST /eval   → evalInWorkbench(expr) via CDP        │
│                                                        │
│  ConnectionManager → Worker Thread → WebSocket         │
│    Port :9333 (primary) │ :9222 (fallback)             │
└────────────────────────────────┬───────────────────────┘
                                 │ CDP WebSocket (single owner)
                                 ▼
┌────────────────────────────────────────────────────────┐
│      Antigravity IDE (Electron / Chromium)             │
│        workbench.html  @  :9333 or :9222               │
└────────────────────────────────────────────────────────┘
```

**Design Principle:** AutoAccept holds the only persistent CDP WebSocket to
`workbench.html`. LazyGravity never opens a competing connection when IPC is
available. Two clients cannot share a CDP WebSocket to the same target without
kicking each other off.

---

## 🔌 IPC Protocol Specification

Full spec → [`references/ipc-protocol.md`](references/ipc-protocol.md)

### Quick Reference

| Method | Path | Request Body | Response |
|--------|------|-------------|----------|
| `GET`  | `/ping` | — | `{"ok":bool,"sessions":int,"workbenchReady":bool}` |
| `POST` | `/inject` | `{"text":"<message>"}` | `{"ok":bool,"error":"..."}` |
| `POST` | `/eval` | `{"expression":"<js>"}` | `{"ok":bool,"value":any,"error":"..."}` |

- **Base URL:** `http://127.0.0.1:27182`
- **Auth:** None (loopback-only, `Access-Control-Allow-Origin: 127.0.0.1`)
- **Timeout recommended:** 3 s for `/ping`, 30 s for `/eval`

---

## 🛠️ Implementation Guide

### Step 1 — Add IPC Server to AutoAccept Extension

File: `src/extension.js`

Add inside `activate()` at the very end, after all handlers are wired:

```javascript
// Start IPC server for LazyGravity bot delegation
startIPCServer();
```

Add `stopIPCServer()` inside `deactivate()`.

Implement the server — see [`examples/extension-ipc-server.js`](examples/extension-ipc-server.js).

**Constraints:**
- MUST bind to `'127.0.0.1'` only (not `'0.0.0.0'`)
- MUST handle `EADDRINUSE` silently (second IDE window may also try to bind)
- Port MUST be `27182` (LazyGravity hardcodes this)
- `/ping` MUST report `workbenchReady` from `connectionManager._getWorkbenchSession()`
- `/eval` delegates to `connectionManager.evalInWorkbench(expression)`
- `/inject` delegates to `connectionManager.injectText(text)`

### Step 2 — Add `_getWorkbenchSession()` and `evalInWorkbench()` to ConnectionManager

File: `src/cdp/ConnectionManager.js`

```javascript
_getWorkbenchSession() {
    for (const [, info] of this.sessions) {
        if (info.url && (
            info.url.includes('workbench') ||
            info.url.startsWith('vscode-file://')
        )) {
            return info;
        }
    }
    return null;
}

async evalInWorkbench(expression) {
    const session = this._getWorkbenchSession();
    if (!session) throw new Error('No workbench CDP session');
    const result = await this._workerEval(session.wsUrl, expression);
    return (result?.result?.result?.value) !== undefined
        ? result.result.result.value
        : null;
}

async injectText(text) {
    const session = this._getWorkbenchSession();
    if (!session) throw new Error('No workbench CDP session — AutoAccept not connected');
    const escapedText = JSON.stringify(text);
    const script = `(function() {
        const editors = Array.from(document.querySelectorAll(
            'div[role="textbox"]:not(.xterm-helper-textarea)'));
        const visible = editors.filter(el => el.offsetParent !== null);
        const editor = visible[visible.length - 1];
        if (!editor) return { ok: false, error: 'No editor found' };
        editor.focus();
        document.execCommand('selectAll', false, null);
        document.execCommand('delete', false, null);
        document.execCommand('insertText', false, ${escapedText});
        ['keydown','keypress','keyup'].forEach(type => {
            editor.dispatchEvent(new KeyboardEvent(type, {
                key: 'Enter', code: 'Enter', keyCode: 13,
                bubbles: true, cancelable: true
            }));
        });
        return { ok: true };
    })()`;
    const result = await this._workerEval(session.wsUrl, script);
    const val = result?.result?.result?.value;
    if (!val?.ok) throw new Error(val?.error || 'Text injection failed');
    return val;
}
```

### Step 3 — Add IPC Client to Go Project

File: `internal/platform/cdp/antigravity.go`

See full implementation → [`examples/go-ipc-client.go`](examples/go-ipc-client.go)

```go
const ipcBase = "http://127.0.0.1:27182"

var ipcHTTP = &http.Client{Timeout: 3 * time.Second}

func IsAutoAcceptAvailable() bool {
    resp, err := ipcHTTP.Get(ipcBase + "/ping")
    if err != nil { return false }
    defer resp.Body.Close()
    if resp.StatusCode != 200 { return false }
    var result struct {
        OK             bool `json:"ok"`
        WorkbenchReady bool `json:"workbenchReady"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return false
    }
    return result.OK && result.WorkbenchReady
}
```

### Step 4 — Wire IPC Fast-Path in Message Handler

File: `app.go` — inside `processSingleMessage()`:

```go
// IPC fast-path: delegate to AutoAccept when available
if len(attachments) == 0 && cdp.IsAutoAcceptAvailable() {
    if err := cdp.InjectViaAutoAccept(msg.Content()); err != nil {
        // handle error
        return
    }
    resp, err := cdp.MonitorResponseViaIPC(ctx, 15*time.Minute)
    // handle resp...
    return
}

// Fallback: direct CDP (used when AutoAccept is offline or for image messages)
```

**Negative constraints — do NOT:**
- Do NOT call `IsAutoAcceptAvailable()` inside a hot loop (it makes an HTTP call each time)
- Do NOT use IPC path for messages with image attachments (IPC does not support file upload)
- Do NOT open a competing CDP WebSocket to `workbench.html` when IPC is active

---

## ✅ Verification Checklist

Execute these in order before declaring the integration working:

### 1. Check listening ports
```powershell
netstat -ano | findstr "9222 9333 27182" | findstr LISTEN
```
Expected:
```
TCP  127.0.0.1:9222   LISTENING  <antigravity-pid>
TCP  127.0.0.1:27182  LISTENING  <node-extension-pid>
```

### 2. Ping IPC server
```powershell
(Invoke-WebRequest 'http://127.0.0.1:27182/ping').Content
```
Expected:
```json
{"ok":true,"sessions":3,"workbenchReady":true}
```
- `sessions` should be ≥ 1
- `workbenchReady` MUST be `true` for IPC path to activate

### 3. Test /inject manually
```powershell
Invoke-WebRequest 'http://127.0.0.1:27182/inject' `
  -Method POST -ContentType 'application/json' `
  -Body '{"text":"hello from IPC test"}'
```
Expected: `{"ok":true}`

### 4. Test /eval manually
```powershell
Invoke-WebRequest 'http://127.0.0.1:27182/eval' `
  -Method POST -ContentType 'application/json' `
  -Body '{"expression":"document.title"}'
```
Expected: `{"ok":true,"value":"<page title>"}`

### 5. Verify Go build
```bash
cd d:\AI\work\GO
go build ./...
go test ./internal/platform/cdp/...
```

---

## 🔥 Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `/ping` returns `workbenchReady: false` | AutoAccept not yet connected to workbench | Wait 5–10s, toggle AutoAccept OFF/ON |
| `/ping` connection refused | IPC server not started | Reload AutoAccept extension in IDE |
| Go falls back to direct CDP every time | `IsAutoAcceptAvailable()` returns `false` | Check steps 1–2 above |
| Two IDE instances, port already in use | Second IDE tries to bind :27182 | Expected — `EADDRINUSE` silently ignored, first instance wins |
| `/inject` succeeds but chat empty | `injectText` regex misses editor | Check `div[role="textbox"]` selector matches current IDE version |
| CDP WebSocket conflict (Go + AutoAccept both connecting) | IPC check skipped | Ensure `IsAutoAcceptAvailable()` is called BEFORE `InitCDP()` |
| `9333` port not listening, only `9222` | IDE launched with `--remote-debugging-port=9222` flag | AutoAccept falls back to 9222 automatically — this is fine |
| `/eval` 3s timeout during AI generation | `ipcHTTP`(3s) used for eval calls | **坑#1** — 使用独立的 `evalHTTP`(35s) client |
| Monitor returns empty string | Activity-log filter over-eager on short responses | 检查 `looksLikeActivityLog` 的 250 char 上限，参见坑#6 |
| Message queue full warning | Burst of Telegram messages, 100-cap buffer | 增加 `msgQueue` 缓冲区或加背压信号 |

---

## ⚠️ 已知陷阱 (Gotchas)

### 坑#1 — `ipcEval` 必须用 `evalHTTP`(35s) 而非 `ipcHTTP`(3s)

**现象：** 长耗时 AI 生成时（超 3s 的单次 `/eval`），Go 报 `IPC eval repeatedly failed`。  
**根本原因：** 生产代码 `antigravity.go` 中 `ipcEval()` 如果复用了 `ipcHTTP`（3s timeout），在 AI 生成期间 eval 调用会超时。示例文件 `go-ipc-client.go` 已正确定义独立的 `evalHTTP`（35s）。  
**正确做法：**
```go
var ipcHTTP = &http.Client{Timeout: 3 * time.Second}   // 用于 /ping 和 /inject
var evalHTTP = &http.Client{Timeout: 35 * time.Second}  // 用于 /eval

func ipcEval(expression string) (interface{}, error) {
    body, _ := json.Marshal(map[string]string{"expression": expression})
    resp, err := evalHTTP.Post(ipcBase+"/eval", "application/json", bytes.NewReader(body)) // ← evalHTTP
    // ...
}
```
**检验：** `Select-String "evalHTTP" internal\platform\cdp\antigravity.go` 应有结果。

---

### 坑#2 — `InjectMessageWithImages` 内有第二层 IPC 检查（双重检查）

**现象：** 阅读 `antigravity.go` 时发现 `InjectMessageWithImages()` 内部也有 IPC 快速路径，看似和 `app.go` 的检查重复。  
**实际情况：** `app.go` layer 1 命中时直接调用 `cdp.InjectViaAutoAccept()`，不走 `InjectMessageWithImages()`，所以两层不会重复。Layer 2 仅在 CDP fallback 路径内偶发命中（保险用）。  
**结论：** 当前设计安全，但存在冗余。后续重构建议移除 layer 2，保持路径清晰。

---

### 坑#3 — IDE 启动竞态：LazyGravity 先就绪，IDE 未就绪

**现象：** App 启动后立即收到 Telegram 消息，CDP `Discover()` 或 `/ping` 失败。  
**根本原因：** `startup()` 用 `cmd.Start()` 异步启动 IDE，CDP 端口和 AutoAccept 插件需要额外 5~15 秒初始化。  
**已有缓解：** 启动后 2s 后台 goroutine 调用 `bringToFront()`；首条消息到来时再做可用性检查（`IsAutoAcceptAvailable()` 超时自动 fallback）。  
**最佳实践：** 不要假设 `startup()` 返回后 IPC/CDP 立即可用。利用现有 fallback 机制自愈即可。

---

### 坑#4 — `examples/go-ipc-client.go` 有 `//go:build ignore`（设计如此）

**现象：** `go build ./...` 和 `go test ./...` 不处理 `examples/` 下的文件。  
**根本原因：** 示例文件头部有 `//go:build ignore`，故意排除在外。  
**结论：** 这是正确行为——示例是参考代码，不是编译目标。

---

### 坑#5 — AutoAccept 插件升级后 IPC 改造丢失

**现象：** IDE 自动更新/重装 AutoAccept 扩展后，`/ping` connection refused。  
**根本原因：** IPC server 代码是手动注入的，不在官方插件中，升级会覆盖。  
**解决方案：** 改造后应 git commit 已修改的插件文件路径，升级后重新注入。长期解法是向上游提 PR。

---

### 坑#6 — `looksLikeActivityLog` 可能误过滤短响应

**现象：** AI 回复很短（< 250 字符）且以动词开头，被误判为工具调用日志，返回空字符串。  
**根本原因：** 正则 `norm.length <= 250` 的阈值过于宽松。  
**当前状态：** 已知 limitation，实际影响极少。出现时可将 250 调低到 100。

---

### 坑#7 — 3 个 Worker 并发 + `cdpMu`：IPC 路径不受 mutex 约束

**设计决策：** `cdpMu` 只保护直连 CDP 路径（CDP WebSocket 不支持并发）。IPC 路径（HTTP loopback）天然并发安全，不需要也不应该放在 `cdpMu` 保护下。  
**不要这样做：** 不要将 `cdp.IsAutoAcceptAvailable()` 或 `cdp.InjectViaAutoAccept()` 放进 `cdpMu.Lock()` 范围内。

---

### 坑#8 — `stopGoneCount >= 3` 的三次确认阈值不可降低

**现象：** 将阈值改为 1 会导致偶发"虚假完成"，AI 思考中的停顿被误判为生成完毕。  
**根本原因：** Antigravity 的 Stop 按钮在 DOM 刷新时可能短暂消失再出现。  
**结论：** 保持 3 次阈值（轮询间隔 2s → 至少 6s 确认完成）。

---

## ✨ 成功经验 (Lessons Learned)

### L1 — IPC 快速路径的双层设计（app.go + 底层 fallback）

`app.go` 的 `processSingleMessage()` 是主干决策点：
- **IPC 快速路径（layer 1）**：文本消息 + AutoAccept 可用 → HTTP 委托，不启动 CDP stack
- **直连 CDP fallback（layer 2）**：图片消息 OR AutoAccept 不可用 → 原有 WebSocket 路径

这种设计隔离清晰，fallback 路径完整，无需修改底层 CDP 逻辑。

### L2 — `execCommand` 在 Electron 内优于 `Input.insertText`

在 Electron Chromium 内，`Input.insertText` 注入文本后按 Enter 不触发 React `onChange`。`document.execCommand('insertText')` + 合成 `KeyboardEvent` 能正确触发。IPC 路径通过 AutoAccept 在 workbench 上下文内部执行 JS，天然绕过这个问题。

### L3 — 解决 CDP 独占的正确思路："收购代理权"而非"共享"

Chromium 的 CDP WebSocket 是独占机制，无法共享。唯一正确解法是让 AutoAccept 独占，Go 退化为纯 HTTP 客户端。任何试图"轮流使用"或"协调连接"的方案都会失败。

### L4 — `atomic.CompareAndSwapInt32` 防止重复启动优于 mutex

`StartBot()` 用 atomic CAS 防并发启动：失败时立即返回错误（fast fail）。mutex 方案会阻塞等待，可能造成 Wails 前端假死。

### L5 — `sendReplyChunked` 用 3800 rune 分片为 Telegram 4096 字节限制预留安全余量

Telegram 上限 4096 字节（UTF-8），中文 3 字节/字。3800 rune 的混合文本经测试不超限，是安全的保守值。

---

## 📋 Port Reference

| Port | Owner | Purpose |
|------|-------|---------|
| 9222 | Antigravity IDE (Electron) | CDP debugging（启动参数指定，当前唯一监听） |
| 9333 | Antigravity IDE (Electron) | CDP debugging（AutoAccept 首选，自动 fallback 到 9222） |
| 27182 | AutoAccept Extension (Node.js) | IPC HTTP server for LazyGravity delegation |

**Rule:** LazyGravity Go app opens **zero** ports. It is purely a client.

**说明：** `startup()` 以 `--remote-debugging-port=9222` 启动 IDE，因此 9333 不会监听。AutoAccept 内部先尝试 9333 失败后自动 fallback 到 9222 —— 这是预期行为，不影响功能。

---

## 📁 Skill Directory Structure

```
Skills/antigravity-autoaccept/
├── SKILL.md          ← 本文件（AI Agent 主入口，frontmatter 用于自动发现）
├── README.md         ← 人类可读说明（中文，含改造背景和 Step by Step）
├── plugin.json       ← Skill 元数据（name、version、IPC 端口等）
├── references/
│   └── ipc-protocol.md        ← IPC API 完整规范（端点/字段/超时/安全）
└── examples/
    ├── extension-ipc-server.js ← AutoAccept 端完整实现（可直接复制）
    └── go-ipc-client.go        ← Go 端完整实现（含正确的 evalHTTP client）
```

> ⚠️ **重要：** `examples/go-ipc-client.go` 中使用了正确的双 HTTP client 设计（`ipcHTTP` + `evalHTTP`）。
> 将代码复制到生产文件时，务必同时复制 `evalHTTP` 变量，避免坑#1。

---

*版本：v2.0.0 | 最后更新：2026-04-15 | 维护者：WJ*
