//go:build windows

package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

var fileAssocProcLockMu sync.Mutex

func isFileAssocLockPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	const stillActive = 259
	return code == stillActive
}

// acquireFileAssocLock acquires an OS-enforced exclusive LockFileEx on a
// persistent v2 lock file in appDataDir, then bridges the legacy PID lock file
// so older Block versions running during an upgrade are strictly serialized.
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

	h := windows.Handle(f.Fd())
	var ol windows.Overlapped
	for {
		err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) && !errors.Is(err, windows.ERROR_IO_PENDING) {
			_ = f.Close()
			fileAssocProcLockMu.Unlock()
			return func() {}, fmt.Errorf("LockFileEx %s: %w", lockPath, err)
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
		var unlockOL windows.Overlapped
		_ = windows.UnlockFileEx(h, 0, 1, 0, &unlockOL)
		_ = f.Close()
		fileAssocProcLockMu.Unlock()
		return func() {}, legacyErr
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			releaseLegacy()
			var unlockOL windows.Overlapped
			_ = windows.UnlockFileEx(h, 0, 1, 0, &unlockOL)
			_ = f.Close()
			fileAssocProcLockMu.Unlock()
		})
	}, nil
}
