//go:build !windows

package main

import wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

func (a *App) initTray() {}

func (a *App) quitTray() {}

func (a *App) HideToTray() {
	wailsRuntime.WindowMinimise(a.ctx)
}

// bringToFront on non-Windows simply ensures the window is visible.
func (a *App) bringToFront() {
	wailsRuntime.WindowShow(a.ctx)
}
