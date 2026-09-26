//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// installBinary replaces dst with src. On Windows an in-use executable cannot
// be renamed/deleted directly, so we schedule a deferred move via a detached
// helper process that runs after this process exits.
func installBinary(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	cmd := exec.Command("cmd", "/C",
		fmt.Sprintf("timeout /t 1 >nul && move /y %q %q", src, dst))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
