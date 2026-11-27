package files

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// DirTracker holds an open read-only OS handle to a directory so external
// renames and moves can be followed to the directory's live path.
type DirTracker struct {
	mu          sync.Mutex
	handle      dirHandle
	initialPath string
	locator     string
	closed      bool
}

// OpenDirTracker opens a read-only handle to dir and captures its persistent
// cross-restart locator.
func OpenDirTracker(dir string) (*DirTracker, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("empty directory path")
	}
	clean := filepath.Clean(dir)
	h, locator, err := openDirHandle(clean)
	if err != nil {
		return nil, err
	}
	return &DirTracker{
		handle:      h,
		initialPath: clean,
		locator:     locator,
	}, nil
}

// InitialPath returns the cleaned path originally passed to OpenDirTracker.
func (t *DirTracker) InitialPath() string {
	if t == nil {
		return ""
	}
	return t.initialPath
}

// Locator returns the persistent cross-restart directory locator captured at open time.
func (t *DirTracker) Locator() string {
	if t == nil {
		return ""
	}
	return t.locator
}

// CurrentPath queries the OS for the directory's live absolute path even after
// it has been moved or renamed externally, returning false if deleted or trashed.
func (t *DirTracker) CurrentPath() (string, bool) {
	if t == nil {
		return "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return "", false
	}
	return currentDirPath(t.handle, t.initialPath)
}

// Close releases the underlying directory handle. Calling Close multiple times
// or on a nil receiver is safe.
func (t *DirTracker) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	return closeDirHandle(&t.handle)
}

// IsTrashPath reports whether p points inside a known OS Trash or Recycle Bin location.
func IsTrashPath(p string) bool {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return false
	}
	norm := strings.ReplaceAll(trimmed, "\\", "/")
	clean := path.Clean(norm)
	low := strings.ToLower(clean)

	if strings.Contains(low, "/.trash/") || strings.HasSuffix(low, "/.trash") ||
		strings.Contains(low, "/.trashes/") || strings.HasSuffix(low, "/.trashes") ||
		strings.Contains(low, "/.local/share/trash/") || strings.HasSuffix(low, "/.local/share/trash") ||
		strings.Contains(low, "/$recycle.bin/") || strings.HasSuffix(low, "/$recycle.bin") {
		return true
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		homeClean := strings.TrimSuffix(path.Clean(strings.ReplaceAll(home, "\\", "/")), "/")
		homeLow := strings.ToLower(homeClean)
		if homeLow != "" {
			if low == homeLow+"/trash" || strings.HasPrefix(low, homeLow+"/trash/") {
				return true
			}
			mobilePrefix := homeLow + "/library/mobile documents/"
			if strings.HasPrefix(low, mobilePrefix) && (strings.Contains(low, "/trash/") || strings.HasSuffix(low, "/trash")) {
				return true
			}
		}
	}
	return false
}
