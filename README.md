# LazyGravity

**中文** | [English](#english)

> 将 Telegram 消息桥接到 Antigravity IDE 的 AI 编程机器人，通过 CDP 直接控制 Chromium，同时兼容 AntiGravity AutoAccept 插件的 IPC 协作模式。

---

## 目录 / Contents

- [简介](#简介)
- [功能](#功能)
- [快速开始](#快速开始)
- [配置说明](#配置说明)
- [TTS 配置](#tts-配置)
- [项目结构](#项目结构)
- [English](#english)

---

## 简介

LazyGravity 是一款基于 [Wails](https://wails.io/) 的桌面应用，通过 Telegram Bot 接收消息，利用 Chrome DevTools Protocol (CDP) 将消息注入到 [Antigravity IDE](https://antigravity.dev) 的 AI 对话框，实现远程控制 AI 编程助手。

支持两种注入模式，自动切换：
- **CDP 直连模式** — 直接通过 WebSocket 控制 Chromium（支持图片上传）
- **IPC 协作模式** — 兼容 [AntiGravity AutoAccept](https://github.com/yazanbaker94/AntiGravity-AutoAccept) 插件，通过本地 HTTP 桥接委托操作，消除双方争抢同一 CDP WebSocket 连接的问题

详见 [`Skills/antigravity-autoaccept/`](Skills/antigravity-autoaccept/)。

## 功能

- 📨 **Telegram → IDE** — 接收 Telegram 消息并注入 Antigravity IDE 对话框
- 🖼️ **图片支持** — 转发照片附件到 IDE（走直连 CDP 路径，IPC 路径暂不支持图片）
- 🔗 **双模式注入** — 优先 IPC 协作模式（兼容 AutoAccept 插件）；无插件时自动 fallback 到 CDP 直连 Chrome/Chromium
- 🔊 **TTS 语音** — 可选的 Edge TTS 文字转语音播报（需配置，见下方）
- 🛡️ **系统托盘** — 后台运行，Windows/macOS 托盘图标
- 🌐 **中英双语** — 界面支持中文/英文切换

## 快速开始

### 环境要求

- [Go 1.21+](https://go.dev/dl/)
- [Node.js 18+](https://nodejs.org/)
- [Wails v2](https://wails.io/) — `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- [Antigravity IDE](https://antigravity.dev)，需以 `--remote-debugging-port=9222` 启动
- Telegram Bot Token（向 [@BotFather](https://t.me/BotFather) 申请）

### 直接使用（下载发行版）

从 [Releases](https://github.com/wangjianjq/lazy-gravity-go/releases) 下载最新的 `lazygravity.exe`，双击运行即可。

### 从源码构建

```bash
git clone https://github.com/wangjianjq/lazy-gravity-go.git
cd lazy-gravity-go

# 开发模式（热重载）
wails dev

# 生产构建
wails build
# 输出: build/bin/lazygravity.exe
```

## 配置说明

首次启动会弹出设置向导，需填写：

| 配置项 | 说明 |
|--------|------|
| **Telegram Bot Token** | 从 [@BotFather](https://t.me/BotFather) 获取 |
| **工作目录** | 本地存储对话历史的目录 |
| **语音设置** | TTS 语音、语速、音量 |

配置加密存储于 `%APPDATA%\LazyGravity\_config.json`（Windows）。

## TTS 配置

TTS 功能使用 Microsoft Edge Read Aloud 协议，所需凭据**未包含在仓库中**，需手动填写。

1. 打开 `internal/tts/tts.go`，找到文件顶部的常量块：

```go
const (
    // 通过抓包 Edge 浏览器访问 speech.platform.bing.com 的请求获取
    // 或参考任意开源 edge-tts 项目
    edgeTTSClientToken = "YOUR_TRUSTED_CLIENT_TOKEN"

    // 从 Edge Read Aloud 插件的网络请求 Origin 头获取
    edgeTTSOrigin = "chrome-extension://YOUR_EXTENSION_ID"
)
```

2. 将 `YOUR_TRUSTED_CLIENT_TOKEN` 和 `YOUR_EXTENSION_ID` 替换为真实值
3. **不要提交**这两个值到 Git

> 💡 这些值可从任意开源 [edge-tts](https://github.com/rany2/edge-tts) 项目获取，并非私密密钥，仅为保持代码清洁而使用占位符。

## 项目结构

```
lazy-gravity-go/
├── app.go                        # 主逻辑（消息路由、CDP/IPC 调度）
├── main.go                       # Wails 入口
├── tray_windows.go               # 系统托盘（Windows）
├── tray_unix.go                  # 系统托盘（macOS/Linux）
├── internal/
│   ├── config/                   # 配置读写（AES 加密）
│   ├── crypto/                   # 加密工具
│   ├── database/                 # SQLite 对话历史
│   ├── i18n/                     # 双语字符串（EN/ZH）
│   ├── platform/
│   │   ├── cdp/                  # CDP 客户端 + IPC 层
│   │   ├── telegram/             # Telegram 适配器
│   │   └── discord/              # Discord 适配器（实验性）
│   └── tts/                      # Edge TTS 集成
├── frontend/                     # Wails 前端（TypeScript）
├── Skills/
│   └── antigravity-autoaccept/   # AI Agent Skill（IPC 集成文档）
└── build/bin/                    # 编译产物
```

---

## English

> A Telegram bot bridge for [Antigravity IDE](https://antigravity.dev), enabling AI-assisted coding workflows via Telegram messages.

### Features

- 📨 **Telegram → IDE** — Receive Telegram messages and inject them into Antigravity IDE
- 🖼️ **Image support** — Forward photo attachments via direct CDP path
- 🔗 **Dual-mode injection** — IPC mode (compatible with AutoAccept plugin) preferred; auto-fallback to direct CDP WebSocket control of Chrome/Chromium when plugin is unavailable
- 🔊 **TTS** — Optional text-to-speech via Edge TTS (requires configuration)
- 🛡️ **System tray** — Runs as a background tray application on Windows/macOS

### Quick Start

**Download:** Get the latest `lazygravity.exe` from [Releases](https://github.com/wangjianjq/lazy-gravity-go/releases).

**Build from source:**
```bash
git clone https://github.com/wangjianjq/lazy-gravity-go.git
cd lazy-gravity-go
wails build   # Output: build/bin/lazygravity.exe
```

### TTS Setup

Open `internal/tts/tts.go` and fill in the two constants:

```go
const (
    edgeTTSClientToken = "YOUR_TRUSTED_CLIENT_TOKEN"  // from Edge network requests
    edgeTTSOrigin      = "chrome-extension://YOUR_EXTENSION_ID"
)
```

See any open-source [edge-tts](https://github.com/rany2/edge-tts) project for the actual values. **Do not commit these values.**

### IPC Integration

See [`Skills/antigravity-autoaccept/`](Skills/antigravity-autoaccept/) for the complete guide on integrating with the AutoAccept VS Code extension.

---

## License

MIT — see [Skills/antigravity-autoaccept/LICENSE](Skills/antigravity-autoaccept/LICENSE)
