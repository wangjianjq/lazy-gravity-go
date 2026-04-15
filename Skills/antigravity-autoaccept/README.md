# AntiGravity AutoAccept — IPC 集成 Skill

> **一句话总结：** 将 AntiGravity AutoAccept VS Code 插件改造为 IPC HTTP 服务端，让 LazyGravity Go 项目通过本地桥接委托 CDP 操作，彻底消除双方争抢同一 WebSocket 连接的问题。

---

## 📦 插件来源

| 属性 | 详情 |
|------|------|
| **插件名称** | AntiGravity AutoAccept |
| **作者** | [@yazanbaker94](https://github.com/yazanbaker94) |
| **GitHub 仓库** | https://github.com/yazanbaker94/AntiGravity-AutoAccept |
| **Marketplace ID** | `yazanbaker.antigravity-autoaccept` |
| **测试版本** | `3.13.8-universal` |
| **本地安装路径** | `%USERPROFILE%\.antigravity\extensions\yazanbaker.antigravity-autoaccept-3.13.8-universal\` |
| **许可证** | MIT |

---

## 🧩 插件原始功能

AntiGravity AutoAccept 是一个 VS Code / Antigravity IDE 扩展，原始功能如下：

- **自动点击 Accept 按钮**：通过两个通道自动接受 AI Agent 生成的代码变更
  - **Channel 1**：VS Code 命令 API 轮询（`antigravity.agent.acceptAgentStep` 等）
  - **Channel 2**：CDP WebSocket + MutationObserver 注入，在 DOM 层面检测并点击 Accept 按钮
- **Dashboard 面板**：显示累计自动点击次数、时间节省统计、里程碑成就
- **命令过滤**：允许配置 blocklist/allowlist 决定哪些终端命令需要人工确认
- **自动重试**：检测到 Agent 卡住时自动触发重试
- **内存监控**：每 30 秒记录进程内存到 `%TEMP%\aa-memory.log`

### 关键技术架构（改造前）

```
AutoAccept Extension (Node.js)
    │
    ├─ Channel 1: VS Code Command API (轮询，每 500ms)
    └─ Channel 2: CDP WebSocket → workbench.html
                  └─ Worker Thread → MutationObserver 注入
```

CDP 连接由 `ConnectionManager` 管理，通过 Worker Thread 隔离 WebSocket。

---

## ❓ 为什么要改造

### 问题：CDP WebSocket 单连接限制

Chromium/Electron 的 CDP (Chrome DevTools Protocol) 对同一目标页面（`workbench.html`）的 WebSocket 连接实行**独占机制**：

> **任何新的 WebSocket 连接建立时，旧连接会被强制断开。**

LazyGravity Go 项目在收到 Telegram 消息时，需要：
1. 连接 Antigravity IDE 的 CDP 端口（:9222 / :9333）
2. 找到 `workbench.html` 目标
3. 建立 WebSocket，注入消息文本，监控响应

**问题就在这里：**

```
AutoAccept 已占用 workbench.html 的 WebSocket
          ↓
LazyGravity 建立新连接 → AutoAccept 被踢下线
          ↓
LazyGravity 注入完成后释放 → AutoAccept 重连
          ↓
竞争循环：两者不断互相踢出对方
```

实际表现：
- AutoAccept 的 MutationObserver 频繁中断，自动点击失效
- LazyGravity 连接不稳定，偶发注入失败
- 两者同时运行时系统行为不可预测

### 根本原因

```
端口 :9222 或 :9333
    ├─ AutoAccept Extension     ← 持久连接 workbench.html
    └─ LazyGravity Go           ← 每条消息临时连接 workbench.html
                                   ↑ 竞争同一目标！
```

---

## 🔧 改造方案：IPC 委托桥接

### 核心思路

让 AutoAccept 充当 **CDP 代理**，对外暴露一个本地 HTTP 接口（IPC Server）。LazyGravity 不再直连 CDP，而是通过 HTTP 请求委托 AutoAccept 执行操作。

```
改造后：
    AutoAccept → 单一持久 CDP 连接 → workbench.html
                    ↑
    LazyGravity → HTTP :27182 → POST /inject
                               POST /eval
```

两者完全解耦，不再竞争。

### 改造涉及的文件

#### AutoAccept 插件端（2个文件）

| 文件 | 改动类型 | 内容 |
|------|----------|------|
| `src/extension.js` | **新增** | 添加 IPC HTTP Server（`startIPCServer` / `stopIPCServer`），在 `activate()` 末尾启动，在 `deactivate()` 中停止 |
| `src/cdp/ConnectionManager.js` | **新增** | 添加 3 个方法：`_getWorkbenchSession()` / `evalInWorkbench()` / `injectText()` |

#### LazyGravity Go 端（2个文件）

| 文件 | 改动类型 | 内容 |
|------|----------|------|
| `internal/platform/cdp/antigravity.go` | **新增** | IPC 客户端：`IsAutoAcceptAvailable()` / `InjectViaAutoAccept()` / `ipcEval()` / `MonitorResponseViaIPC()` |
| `app.go` | **新增** | 在 `processSingleMessage()` 中加入 IPC 快速路径，优先尝试 IPC，失败则 fallback 到直连 CDP |

---

## 🛠️ 改造方法（Step by Step）

### 第一步：修改 AutoAccept 插件

> ⚠️ 插件文件在 `%USERPROFILE%\.antigravity\extensions\` 下，修改后需在 IDE 中执行 `Reload Extension` 生效。

**1.1 修改 `src/extension.js`**

在文件顶部 `activate()` 函数的末尾（`return` 语句之前）添加：

```javascript
// Start IPC server for LazyGravity bot delegation
startIPCServer();
```

在 `deactivate()` 函数中添加：

```javascript
stopIPCServer();
```

然后将 `startIPCServer` / `stopIPCServer` 函数完整粘贴到文件中。
参考实现：[`examples/extension-ipc-server.js`](examples/extension-ipc-server.js)

**1.2 修改 `src/cdp/ConnectionManager.js`**

在 `ConnectionManager` 类的末尾（`}` 之前）添加以下三个方法：

- `_getWorkbenchSession()` — 查找主 workbench CDP 会话
- `evalInWorkbench(expression)` — 在 workbench 上下文执行 JS
- `injectText(text)` — 注入文本并提交

参考实现：[`examples/extension-ipc-server.js`](examples/extension-ipc-server.js) 中的 `ConnectionManagerAdditions` 对象。

### 第二步：修改 LazyGravity Go 项目

**2.1 修改 `internal/platform/cdp/antigravity.go`**

在文件顶部的 `import` 块中加入必要包（`bytes`、`io`、`log` 通常已有），然后添加 IPC 常量和函数：

```go
const ipcBase = "http://127.0.0.1:27182"
var ipcHTTP = &http.Client{Timeout: 3 * time.Second}
```

添加函数：`IsAutoAcceptAvailable()` / `ipcInject()` / `InjectViaAutoAccept()` / `ipcEval()` / `MonitorResponseViaIPC()`

参考实现：[`examples/go-ipc-client.go`](examples/go-ipc-client.go)

**2.2 修改 `app.go`**

在 `processSingleMessage()` 函数内，在直连 CDP 路径之前插入 IPC 快速路径：

```go
// ── IPC fast-path ──────────────────────────────────────────────────────
if len(attachments) == 0 && cdp.IsAutoAcceptAvailable() {
    // ... IPC inject + monitor (见 examples/go-ipc-client.go)
    return
}

// ── Direct CDP path (fallback / images) ───────────────────────────────
// ... 原有代码不变
```

### 第三步：验证

```powershell
# 检查端口
netstat -ano | findstr "9222 9333 27182" | findstr LISTEN

# 检查 IPC
(Invoke-WebRequest 'http://127.0.0.1:27182/ping').Content
# 期望: {"ok":true,"sessions":3,"workbenchReady":true}

# 测试注入
Invoke-WebRequest 'http://127.0.0.1:27182/inject' `
  -Method POST -ContentType 'application/json' `
  -Body '{"text":"IPC test"}'
# 期望: {"ok":true}
```

---

## 📚 Skill 介绍

### 这个 Skill 是什么

这是一个 **AI Agent Skill**，按照 2025/2026 年最新 SKILL.md 规范编写。它记录了上述改造的完整知识，让 AI Agent（如 Antigravity、Claude、Gemini 等）能够在未来的对话中自动复现这套改造，无需重新分析代码。

### Skill 文件结构

```
Skills/antigravity-autoaccept/
│
├── README.md          ← 你正在看的这个文件
│                        插件来源 / 功能说明 / 改造原因 / 改造方法 / Skill 介绍
│
├── SKILL.md           ← AI Agent 主指令文件
│                        YAML frontmatter（发现路由）+ 架构图 + 实现步骤 +
│                        验证清单 + 故障排查表
│
├── references/
│   └── ipc-protocol.md   ← IPC API 完整规范
│                           所有端点、字段、错误码、超时建议、安全说明
│
└── examples/
    ├── extension-ipc-server.js  ← AutoAccept 端完整实现（可直接复制）
    └── go-ipc-client.go         ← Go 端完整实现（可直接复制）
```

### 如何让 Agent 使用这个 Skill

在对话开头提及关键词，Agent 会根据 `SKILL.md` 的 `description` 字段自动匹配并加载：

- "CDP 冲突 / CDP conflict"
- "AutoAccept IPC"
- "LazyGravity 注入 / inject"
- "端口 27182"
- "工作台 workbench WebSocket"

或直接告诉 Agent：`"使用 Skills/antigravity-autoaccept 这个 Skill"`

### Skill 设计原则

| 原则 | 实现 |
|------|------|
| Progressive Disclosure | frontmatter 只做路由，细节放 `references/`，代码放 `examples/` |
| Deterministic | 步骤明确，含"不允许做什么"的负向约束 |
| 自包含 | 无外部依赖，所有参考文件在同目录内 |
| 可验证 | 每个步骤后都有可执行的验证命令 |
| 版本化 | frontmatter 含 `version`，便于追踪和更新 |

---

## 📊 改造效果对比

| 指标 | 改造前 | 改造后 |
|------|--------|--------|
| CDP 竞争 | ✅ 存在，互相踢出 | ❌ 消除 |
| AutoAccept 稳定性 | 偶发中断 | 持续稳定 |
| LazyGravity 注入成功率 | 不稳定 | 稳定 |
| 资源占用 | 2 个 WebSocket | 1 个 WebSocket |
| 延迟 | 直连 CDP（低） | HTTP loopback（可忽略，< 1ms） |
| 图片消息支持 | ✅ 直连 CDP | ✅ 自动 fallback 到直连 CDP |

---

## 🚨 踩坑速查（关键陷阱）

以下是生产运行过程中验证的高危陷阱，完整说明见 `SKILL.md` 的「已知陷阱」章节。

| 坑# | 标题 | 一句话说明 |
|-----|------|-----------|
| #1 | `ipcEval` 超时 | `/eval` 必须用 `evalHTTP`(35s)，不能用 `ipcHTTP`(3s) |
| #2 | 双重 IPC 检查 | `InjectMessageWithImages` 内的 layer 2 是冗余的（安全但混淆） |
| #3 | IDE 启动竞态 | `startup()` 后 5~15 秒内 IPC/CDP 可能不可用，依赖 fallback 自愈 |
| #4 | 示例文件不编译 | `//go:build ignore` 是故意的，不是 bug |
| #5 | 插件升级丢失改造 | AutoAccept 更新后需重新注入 IPC server 代码 |
| #6 | 短响应被过滤 | `looksLikeActivityLog` 的 250 char 上限可能误判 AI 短回复 |
| #7 | IPC 路径不需要 cdpMu | HTTP loopback 天然并发安全，不要放在 mutex 保护下 |
| #8 | 三次确认阈值不可降 | `stopGoneCount >= 3` 防止 Stop 按钮短暂消失的虚假完成 |

---

*最后更新：2026-04-15 | 维护者：WJ | Skill 版本：v2.0.0*
