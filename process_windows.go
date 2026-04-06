//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideWindowProcess configures the command to run silently without popping up a command prompt window
func hideWindowProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags = 0x08000000
}
