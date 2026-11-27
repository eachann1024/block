//go:build linux

package files

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func openDirHandle(clean string) (dirHandle, string, error) {
	f, info, err := openUnixDirFile(clean)
	if err != nil {
		return nil, "", err
	}
	return f, FileIdentityToken(info, f), nil
}

func currentDirPath(h dirHandle, _ string) (string, bool) {
	if h == nil {
		return "", false
	}
	target, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", h.Fd()))
	runtime.KeepAlive(h)
	if err != nil || target == "" || strings.HasSuffix(target, " (deleted)") {
		return "", false
	}
	return verifyLiveDirPath(h, target)
}

// CaptureDirLocator returns a persistent cross-restart locator for dir.
func CaptureDirLocator(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	st, err := os.Stat(filepath.Clean(dir))
	if err != nil || !st.IsDir() {
		return ""
	}
	return FileIdentityToken(st, nil)
}

// ResolveDirLocator resolves a persisted locator back to its current directory
// path when oldPath no longer exists.
func ResolveDirLocator(oldPath, locator string) (string, bool) {
	return resolveDirLocatorShallow(oldPath, locator)
}

func actualExistingPath(clean string) string {
	return clean
}
