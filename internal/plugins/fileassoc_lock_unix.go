//go:build darwin || linux

package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

var fileAssocProcLockMu sync.Mutex

func isFileAssocLockPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// acquireFileAssocLock acquires an OS-enforced exclusive flock on a persistent
// v2 lock file in appDataDir, then bridges the legacy PID lock file so older
// Block versions running during an upgrade are strictly serialized as well.
func acquireFileAssocLock(appDataDir string, timeout time.Duration) (func(), error) {
	if err := os.MkdirAll(appDataDir, 0o755); err != nil {
		return func() {}, err
	}
	lockPath := filepath.Join(appDataDir, ".file-associations.v2.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, err
	}

	deadline := time.Now().Add(timeout)
	for !fileAssocProcLockMu.TryLock() {
		if time.Now().After(deadline) {
			_ = f.Close()
			return func() {}, fmt.Errorf("timed out waiting for in-process file association lock")
		}
		time.Sleep(15 * time.Millisecond)
	}

	fd := int(f.Fd())
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			fileAssocProcLockMu.Unlock()
			return func() {}, fmt.Errorf("flock %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			fileAssocProcLockMu.Unlock()
			return func() {}, fmt.Errorf("timed out waiting for %s", lockPath)
		}
		time.Sleep(25 * time.Millisecond)
	}

	releaseLegacy, legacyErr := acquireLegacyFileAssocPIDLock(appDataDir, deadline, isFileAssocLockPIDAlive)
	if legacyErr != nil {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
		fileAssocProcLockMu.Unlock()
		return func() {}, legacyErr
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			releaseLegacy()
			_ = syscall.Flock(fd, syscall.LOCK_UN)
			_ = f.Close()
			fileAssocProcLockMu.Unlock()
		})
	}, nil
}
