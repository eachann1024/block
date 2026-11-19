//go:build darwin || linux

package plugins

import (
	"os/exec"
	"syscall"
)

func configurePluginProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return stopPluginProcess(cmd) }
}

func stopPluginProcess(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
