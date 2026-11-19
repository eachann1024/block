//go:build !darwin && !linux

package plugins

import "os/exec"

func configurePluginProcess(_ *exec.Cmd) {}

func stopPluginProcess(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
