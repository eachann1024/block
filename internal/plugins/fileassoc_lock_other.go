//go:build !darwin && !linux && !windows

package plugins

import (
	"fmt"
	"sync"
	"time"
)

var fileAssocProcLockMu sync.Mutex

func acquireFileAssocLock(_ string, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	for !fileAssocProcLockMu.TryLock() {
		if time.Now().After(deadline) {
			return func() {}, fmt.Errorf("timed out waiting for file association lock")
		}
		time.Sleep(15 * time.Millisecond)
	}
	var once sync.Once
	return func() {
		once.Do(func() { fileAssocProcLockMu.Unlock() })
	}, nil
}
