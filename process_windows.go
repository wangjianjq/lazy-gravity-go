//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideWindowProcess configures the command to run silently without a console window,
// and detaches it from the LazyGravity process tree so Windows does not incorrectly
// treat the IDE as occluded (which causes the black-screen / renderer-suspend bug).
//
// CreationFlags used:
//   0x08000000 = CREATE_NO_WINDOW  — no console popup
//   0x00000008 = DETACHED_PROCESS  — fully detach from parent, preventing occlusion misdetection
func hideWindowProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags = 0x08000000 | 0x00000008 // CREATE_NO_WINDOW | DETACHED_PROCESS
}
