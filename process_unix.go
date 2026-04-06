//go:build !windows

package main

import (
	"os/exec"
)

// hideWindowProcess is a no-op on non-Windows platforms
func hideWindowProcess(cmd *exec.Cmd) {
	// Not applicable for non-Windows platforms
}
