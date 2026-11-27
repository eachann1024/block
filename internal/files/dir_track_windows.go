//go:build windows

package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// FILE_NAME_NORMALIZED | VOLUME_NAME_DOS (0x0) for GetFinalPathNameByHandle.
const winFileNameNormalized = 0

type dirHandle = windows.Handle

func openWinHandle(clean string, access uint32) (windows.Handle, windows.ByHandleFileInformation, error) {
	var bhfi windows.ByHandleFileInformation
	ptr, err := windows.UTF16PtrFromString(clean)
	if err != nil {
		return windows.InvalidHandle, bhfi, err
	}
	h, err := windows.CreateFile(
		ptr,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return windows.InvalidHandle, bhfi, err
	}
	if err := windows.GetFileInformationByHandle(h, &bhfi); err != nil {
		_ = windows.CloseHandle(h)
		return windows.InvalidHandle, bhfi, err
	}
	return h, bhfi, nil
}

func openDirHandle(clean string) (dirHandle, string, error) {
	h, bhfi, err := openWinHandle(clean, windows.FILE_LIST_DIRECTORY)
	if err != nil {
		return windows.InvalidHandle, "", err
	}
	if bhfi.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		_ = windows.CloseHandle(h)
		return windows.InvalidHandle, "", errors.New("not a directory")
	}
	return h, formatWinFileIdentity(&bhfi), nil
}

func closeDirHandle(h *dirHandle) error {
	if h == nil || *h == 0 || *h == windows.InvalidHandle {
		return nil
	}
	handle := *h
	*h = windows.InvalidHandle
	return windows.CloseHandle(handle)
}

func currentDirPath(h dirHandle, _ string) (string, bool) {
	if h == 0 || h == windows.InvalidHandle {
		return "", false
	}
	buf := make([]uint16, 512)
	var raw string
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), winFileNameNormalized)
		if err != nil || n == 0 {
			return "", false
		}
		if int(n) < len(buf) {
			raw = windows.UTF16ToString(buf[:n])
			break
		}
		if n > 32768 {
			return "", false
		}
		buf = make([]uint16, n+1)
	}
	if strings.HasPrefix(raw, `\\?\UNC\`) {
		raw = `\\` + strings.TrimPrefix(raw, `\\?\UNC\`)
	} else if strings.HasPrefix(raw, `\\?\`) {
		raw = strings.TrimPrefix(raw, `\\?\`)
	}
	p := filepath.Clean(raw)
	if p == "" || p == "." || IsTrashPath(p) {
		return "", false
	}
	st, err := os.Stat(p)
	if err != nil || !st.IsDir() {
		return "", false
	}
	return p, true
}

// CaptureDirLocator returns a persistent cross-restart locator for dir.
func CaptureDirLocator(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	h, bhfi, err := openWinHandle(filepath.Clean(dir), windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return ""
	}
	_ = windows.CloseHandle(h)
	if bhfi.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return ""
	}
	return formatWinFileIdentity(&bhfi)
}

// ResolveDirLocator resolves a persisted locator back to its current directory
// path when oldPath no longer exists.
func ResolveDirLocator(oldPath, locator string) (string, bool) {
	return resolveDirLocatorShallow(oldPath, locator)
}

// PathIdentity returns a stable identity token for path.
func PathIdentity(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	h, bhfi, err := openWinHandle(filepath.Clean(path), windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return ""
	}
	_ = windows.CloseHandle(h)
	return formatWinFileIdentity(&bhfi)
}
