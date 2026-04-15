//go:build windows

package main

import (
	_ "embed"
	goruntime "runtime"
	"time"

	"github.com/energye/systray"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Use the Windows ICO file for best tray icon rendering (16×16 px).
//
//go:embed build/windows/icon.ico
var trayIcon []byte

// initTray starts the systray in a dedicated goroutine that is pinned to a
// single OS thread via runtime.LockOSThread.  This satisfies Win32's
// thread-affinity rule (hidden notification window + GetMessageW must live on
// the same OS thread) while keeping Register() and nativeStart() together in
// the same execution context.
func (a *App) initTray() {
	go func() {
		goruntime.LockOSThread()
		// Intentionally not calling UnlockOSThread: this OS thread is dedicated
		// to the systray message pump for the lifetime of the app.
		systray.Run(a.onTrayReady, func() {})
	}()
}

func (a *App) onTrayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTitle("LazyGravity")
	systray.SetTooltip("LazyGravity — Telegram Bridge")

	// Windows convention: left-click / double-click restores the window.
	// Right-click automatically shows the context menu built below.
	systray.SetOnClick(func(menu systray.IMenu) {
		a.showFromTray()
	})
	systray.SetOnDClick(func(menu systray.IMenu) {
		a.showFromTray()
	})

	// Right-click context menu
	mShow := systray.AddMenuItem("打开 / Open LazyGravity", "Show the LazyGravity window")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出 / Quit", "Quit LazyGravity")

	mShow.Click(func() { a.showFromTray() })
	mQuit.Click(func() { wailsRuntime.Quit(a.ctx) })
}

// showFromTray restores the hidden window and forces it to the foreground.
func (a *App) showFromTray() {
	wailsRuntime.WindowShow(a.ctx)
	a.bringToFront()
}

// bringToFront briefly sets AlwaysOnTop so the OS paints the window above
// every other window (including Antigravity), then removes the flag after
// 150 ms so the user can freely alt-tab or click away afterwards.
func (a *App) bringToFront() {
	wailsRuntime.WindowSetAlwaysOnTop(a.ctx, true)
	go func() {
		time.Sleep(150 * time.Millisecond)
		wailsRuntime.WindowSetAlwaysOnTop(a.ctx, false)
	}()
}

func (a *App) quitTray() {
	// systray.Quit() is safe to call from any goroutine; it posts WM_QUIT to
	// the message pump, causing Run() to return and the goroutine to exit.
	systray.Quit()
}

// HideToTray hides the main window so it disappears from the Windows taskbar.
// The notification-area icon stays visible and clickable.
func (a *App) HideToTray() {
	wailsRuntime.WindowHide(a.ctx)
}
