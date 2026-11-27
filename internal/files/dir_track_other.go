//go:build !darwin && !linux && !windows

package files

import (
	"errors"
	"os"
)

type dirHandle = struct{}

func openDirHandle(clean string) (dirHandle, string, error) {
	info, err := os.Stat(clean)
	if err != nil {
		return dirHandle{}, "", err
	}
	if !info.IsDir() {
		return dirHandle{}, "", errors.New("not a directory")
	}
	return dirHandle{}, "", nil
}

func closeDirHandle(_ *dirHandle) error {
	return nil
}

func currentDirPath(_ dirHandle, initialPath string) (string, bool) {
	if initialPath == "" || IsTrashPath(initialPath) {
		return "", false
	}
	st, err := os.Stat(initialPath)
	if err != nil || !st.IsDir() {
		return "", false
	}
	return initialPath, true
}

// CaptureDirLocator returns a persistent cross-restart locator for dir.
func CaptureDirLocator(_ string) string {
	return ""
}

// ResolveDirLocator resolves a persisted locator back to its current directory
// path when oldPath no longer exists.
func ResolveDirLocator(_, _ string) (string, bool) {
	return "", false
}

// PathIdentity returns a stable identity token for path.
func PathIdentity(_ string) string {
	return ""
}
