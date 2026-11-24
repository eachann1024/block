//go:build unix

package plugins

import (
	"errors"
	"syscall"
)

// stageOwnerPIDAlive reports whether pid refers to a live OS process on Unix.
func stageOwnerPIDAlive(pid int) (alive bool, known bool) {
	if pid <= 0 {
		return false, true
	}
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, true
	}
	return false, false
}
