# LazyGravity

A Telegram bot bridge for [Antigravity IDE](https://antigravity.dev), enabling AI-assisted coding workflows via Telegram messages.

## Features

- 📨 **Telegram → Antigravity** — Receive Telegram messages and inject them into Antigravity IDE's chat
- 🖼️ **Image support** — Forward photo attachments directly to the IDE
- 🔗 **IPC delegation** — Integrates with the [AntiGravity AutoAccept](https://github.com/yazanbaker94/AntiGravity-AutoAccept) extension via a local HTTP bridge to eliminate CDP WebSocket conflicts
- 🔊 **TTS** — Optional text-to-speech playback of AI responses via Edge TTS
- 🛡️ **System tray** — Runs in the background as a Windows/macOS tray application

## Prerequisites

- [Go 1.21+](https://go.dev/dl/)
- [Node.js 18+](https://nodejs.org/) (for frontend build)
- [Wails v2](https://wails.io/) — `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- [Antigravity IDE](https://antigravity.dev) with `--remote-debugging-port=9222` flag
- A Telegram Bot token from [@BotFather](https://t.me/BotFather)

## Quick Start

```bash
# Clone the repository
git clone https://github.com/wangjianjq/lazy-gravity-go.git
cd lazy-gravity-go

# Configure TTS (see below)

# Run in development mode
wails dev

# Build production binary
wails build
# Output: build/bin/lazygravity.exe
```

## Configuration

On first launch, the setup wizard will prompt for:
- **Telegram Bot Token** — from [@BotFather](https://t.me/BotFather)
- **Workspace path** — local directory for storing conversation history
- **Voice settings** — TTS voice, rate, volume

Config is stored encrypted at `%APPDATA%\LazyGravity\_config.json` (Windows).

## TTS Setup (Edge TTS)

The TTS feature uses Microsoft Edge Read Aloud. The required credentials are **not included** in this repository for security reasons. To enable TTS:

1. Open `internal/tts/tts.go`
2. Fill in the two constants near the top of the file:

```go
const (
    // Obtain by inspecting Edge browser network requests to speech.platform.bing.com
    // while using the Read Aloud feature, or from any open-source edge-tts project.
    edgeTTSClientToken = "YOUR_TRUSTED_CLIENT_TOKEN"

    // Obtain from the Edge Read Aloud extension's network request Origin header
    edgeTTSOrigin = "chrome-extension://YOUR_EXTENSION_ID"
)
```

3. **Do not commit** these values — keep them local only.

> 💡 These values are publicly documented in projects like [edge-tts](https://github.com/rany2/edge-tts). They are not private secrets, but we avoid hardcoding them to keep the codebase clean.

## IPC Integration (AutoAccept Extension)

See [`Skills/antigravity-autoaccept/`](Skills/antigravity-autoaccept/) for the complete
guide on integrating with the AntiGravity AutoAccept VS Code extension via IPC HTTP bridge.

## Building

```powershell
# Verify compilation
go build ./...

# Run tests
go test ./...

# Build production binary (Windows)
wails build
# Output: build/bin/lazygravity.exe
```

## Project Structure

```
lazy-gravity-go/
├── app.go                    # Main application logic (message routing, CDP/IPC)
├── main.go                   # Wails entry point
├── tray_windows.go           # System tray (Windows)
├── tray_unix.go              # System tray (macOS/Linux)
├── internal/
│   ├── config/               # Config load/save with encryption
│   ├── crypto/               # AES encryption for stored tokens
│   ├── database/             # SQLite conversation history
│   ├── i18n/                 # Bilingual strings (EN/ZH)
│   ├── platform/
│   │   ├── cdp/              # Chrome DevTools Protocol client + IPC layer
│   │   ├── telegram/         # Telegram bot adapter
│   │   └── discord/          # Discord adapter (experimental)
│   └── tts/                  # Edge TTS integration
├── frontend/                 # Wails frontend (TypeScript)
├── Skills/
│   └── antigravity-autoaccept/  # AI Agent Skill for IPC integration
└── build/
    └── bin/                  # Compiled binaries
```

## License

MIT — see [LICENSE](Skills/antigravity-autoaccept/LICENSE)
