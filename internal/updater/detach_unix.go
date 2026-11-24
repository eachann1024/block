//go:build !windows

package updater

import (
	"errors"
	"os/exec"
	"syscall"
)

func configureDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func launchWindowsUpdater(_ string) error {
	return errors.New("windows installer launch is not supported on this platform")
}

func consumeLastUpdateError() string {
	return ""
}
