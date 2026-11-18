//go:build windows

package updater

import "os/exec"

func configureDetachedProcess(_ *exec.Cmd) {}
