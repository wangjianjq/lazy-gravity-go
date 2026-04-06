package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	oRuntime "runtime"
	"strconv"
	"sync"
	"time"

	"github.com/tokyoweb3/lazy-gravity-go/internal/config"
	"github.com/tokyoweb3/lazy-gravity-go/internal/database"
	"github.com/tokyoweb3/lazy-gravity-go/internal/platform"
	"github.com/tokyoweb3/lazy-gravity-go/internal/platform/cdp"
	"github.com/tokyoweb3/lazy-gravity-go/internal/platform/telegram"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct manages the GUI backend logic
type App struct {
	ctx       context.Context
	adapter   platform.Adapter
	cdpClient *cdp.Client
	cdpMu     sync.Mutex // protects cdpClient across concurrent workers
	msgQueue  chan platform.PlatformMessage
	adapterMu sync.RWMutex // protects adapter from concurrent access
	// Internal flags for testing
	skipIDELaunch bool
	skipEvents    bool
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		msgQueue: make(chan platform.PlatformMessage, 100),
	}
}

// cleanupTempFiles cleans up any leftover telegram photo downloads
// from earlier crashes
func (a *App) cleanupTempFiles() {
	tmpDir := os.TempDir()
	files, err := filepath.Glob(filepath.Join(tmpDir, "tg-photo-*.jpg"))
	if err == nil {
		for _, f := range files {
			_ = os.Remove(f)
		}
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.cleanupTempFiles()
	// Ensure config is loaded if available
	if config.Exists() {
		_ = config.Load()
	}

	// Open Antigravity IDE with remote debugging port automatically on app startup.
	if !a.skipIDELaunch {
		if oRuntime.GOOS == "windows" {
			cmd := exec.Command("cmd", "/c", "Antigravity", "--remote-debugging-port=9222")
			hideWindowProcess(cmd)
			if err := cmd.Start(); err != nil {
				a.emitEvent("bot-log", fmt.Sprintf("[Startup] Could not launch Antigravity IDE: %v", err))
			}
		} else if oRuntime.GOOS == "darwin" {
			cmd := exec.Command("open", "-a", "Antigravity", "--args", "--remote-debugging-port=9222")
			if err := cmd.Start(); err != nil {
				a.emitEvent("bot-log", fmt.Sprintf("[Startup] Could not launch Antigravity IDE: %v", err))
			}
		}
	}

	// Start worker pool for message processing (3 concurrent workers)
	const numWorkers = 3
	for i := 0; i < numWorkers; i++ {
		go a.processQueue()
	}
}


func (a *App) processQueue() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case msg := <-a.msgQueue:
			a.processSingleMessage(msg)
		}
	}
}

func (a *App) processSingleMessage(msg platform.PlatformMessage) {
	var monitorStatus platform.PlatformSentMessage

	// Execute CDP operations in an isolated closure to guarantee lock safety in case of panics
	respText, cdpErr := func() (string, error) {
		a.cdpMu.Lock()
		defer a.cdpMu.Unlock()

		if a.cdpClient == nil || !a.cdpClient.IsConnected() {
			client, err := cdp.InitCDP(a.ctx)
			if err != nil {
				a.emitEvent("bot-error", fmt.Sprintf("CDP initial connection error: %v", err))
				msg.Reply(platform.MessagePayload{Text: "Failed to connect to Antigravity UI: " + err.Error()})
				return "", err
			}
			a.cdpClient = client
		}
		a.emitEvent("bot-log", fmt.Sprintf("[CDP] Injecting message from %s", msg.Author().DisplayName))
		monitorStatus, _ = msg.Reply(platform.MessagePayload{Text: "Processing..."})

		attachments := msg.Attachments()
		err := cdp.InjectMessageWithImages(a.ctx, a.cdpClient, msg.Content(), attachments)

		// Clean up temporal attachment files
		for _, file := range attachments {
			os.Remove(file)
		}
		if err != nil {
			a.cdpClient = nil // Reset so next message triggers reconnect
			a.emitEvent("bot-error", fmt.Sprintf("CDP Inject error: %v", err))
			if monitorStatus != nil {
				monitorStatus.Edit(platform.MessagePayload{Text: fmt.Sprintf("Failed to inject to Antigravity: %v", err)})
			}
			return "", err
		}

		resp, err := cdp.MonitorResponse(a.ctx, a.cdpClient, 15*time.Minute)
		if err != nil {
			a.emitEvent("bot-error", fmt.Sprintf("CDP Monitor exception: %v", err))
			a.cdpClient = nil // Reset connection on monitor failure
			if monitorStatus != nil {
				monitorStatus.Edit(platform.MessagePayload{Text: fmt.Sprintf("Timeout or error waiting for Antigravity: %v", err)})
			}
			time.Sleep(5 * time.Second) // Throttling logic to prevent loop-hammering on persistent errors
			return "", err
		}

		return resp, nil
	}()

	if cdpErr != nil {
		return
	}

	if monitorStatus != nil {
		monitorStatus.Delete()
	}

	// Chunk large replies as TG max allows 4096.
	var lastSent platform.PlatformSentMessage
	const maxLen = 3800
	runes := []rune(respText)
	if len(runes) > maxLen {
		for i := 0; i < len(runes); i += maxLen {
			end := i + maxLen
			if end > len(runes) {
				end = len(runes)
			}
			sent, _ := msg.Reply(platform.MessagePayload{Text: string(runes[i:end])})
			if sent != nil {
				lastSent = sent
			}
		}
	} else {
		sent, _ := msg.Reply(platform.MessagePayload{Text: respText})
		if sent != nil {
			lastSent = sent
		}
	}

	// Trigger TTS only once for the complete final response
	a.adapterMu.RLock()
	adapter := a.adapter
	a.adapterMu.RUnlock()

	if adapter != nil {
		if ta, ok := adapter.(*telegram.TelegramAdapter); ok && lastSent != nil {
			chatID, err := strconv.ParseInt(msg.Channel().ID(), 10, 64)
			if err == nil {
				replyID, _ := strconv.Atoi(lastSent.ID())
				ta.EnqueueTTS(chatID, respText, replyID)
			}
		}
	}
}

func (a *App) shutdown(ctx context.Context) {
	a.StopBot()
}

// emitEvent is a helper to safely call Wails EventsEmit
func (a *App) emitEvent(eventName string, args ...interface{}) {
	if a.skipEvents {
		return
	}
	if a.ctx != nil {
		defer func() { recover() }()
		runtime.EventsEmit(a.ctx, eventName, args...)
	}
}

// ─── API exposed to frontend (JS) ───────────────────────────────────────────

func (a *App) HasConfig() bool {
	return config.Exists()
}

func (a *App) GetConfig() config.Config {
	return config.GetConfig()
}

func (a *App) SaveConfig(c config.Config) error {
	return config.SetConfig(c)
}

// ClearConfig deletes the config file, resets in-memory state, and returns
// the app to the setup-wizard view.
func (a *App) ClearConfig() error {
	a.StopBot()
	return config.ClearConfig()
}

func (a *App) StartBot() error {
	if a.adapter != nil {
		// Mock adapter might already be set, but we still need to initialize DB
	}

	cfg := config.GetConfig()
	
	// Only validate token if we aren't using a pre-set (mock) adapter
	if a.adapter == nil && cfg.TelegramToken == "" {
		return fmt.Errorf("no telegram token configured")
	}

	ws := cfg.WorkspacePath
	if ws == "" {
		dir, _ := os.UserConfigDir()
		ws = filepath.Join(dir, "LazyGravity", "Workspace")
	}
	err := database.InitDB(ws)
	if err != nil {
		return fmt.Errorf("database init error: %v", err)
	}

	a.adapterMu.Lock()
	if a.adapter == nil {
		a.adapter = telegram.NewTelegramAdapter(cfg.TelegramToken)
	}
	adapter := a.adapter
	a.adapterMu.Unlock()

	adapter.OnMessage(func(msg platform.PlatformMessage) {
		logMsg := fmt.Sprintf("[%s]: %s", msg.Author().DisplayName, msg.Content())

		// Broadcast the message to the frontend UI
		a.emitEvent("bot-log", logMsg)

		// Queue the message to avoid blocking the Telegram update loop
		select {
		case a.msgQueue <- msg:
		default:
			go msg.Reply(platform.MessagePayload{Text: "⚠️ 系统队列已满，请稍后重试。"})
			a.emitEvent("bot-error", "Message queue is full, dropping message")
		}
	})

	if err := adapter.Start(); err != nil {
		a.adapterMu.Lock()
		a.adapter = nil
		a.adapterMu.Unlock()
		return fmt.Errorf("failed to start Telegram adapter: %v", err)
	}

	a.adapterMu.Lock()
	a.adapter = adapter
	a.adapterMu.Unlock()

	a.emitEvent("bot-status-changed", true)
	return nil
}

func (a *App) StopBot() {
	a.adapterMu.Lock()
	adapter := a.adapter
	a.adapter = nil
	a.adapterMu.Unlock()

	if adapter != nil {
		adapter.Stop()
		a.emitEvent("bot-status-changed", false)
	}
	database.CloseDB()
}

func (a *App) IsBotRunning() bool {
	a.adapterMu.RLock()
	defer a.adapterMu.RUnlock()
	return a.adapter != nil
}
