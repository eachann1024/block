//go:build darwin

package files

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

const notebookIDXattr = "com.blockmd.notebook-id"

func supportsNotebookXattr() bool { return true }

func readNotebookXattr(dirPath string) (string, error) {
	fd, err := unix.Open(dirPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	var buf [128]byte
	n, err := unix.Fgetxattr(fd, notebookIDXattr, buf[:])
	runtime.KeepAlive(fd)
	if err != nil {
		if errors.Is(err, unix.ENOATTR) {
			return "", nil
		}
		return "", err
	}
	if n <= 0 {
		return "", nil
	}
	val := strings.TrimSpace(string(buf[:n]))
	if !IsSafeNotebookID(val) {
		return "", errInvalidNotebookXattr
	}
	return val, nil
}

func writeNotebookXattr(dirPath, id string) error {
	if !IsSafeNotebookID(id) {
		return errors.New("invalid notebook id")
	}
	fd, err := unix.Open(dirPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var curBuf [128]byte
	if n, gErr := unix.Fgetxattr(fd, notebookIDXattr, curBuf[:]); gErr == nil && n > 0 {
		if strings.TrimSpace(string(curBuf[:n])) == id {
			runtime.KeepAlive(fd)
			return nil
		}
	}
	err = unix.Fsetxattr(fd, notebookIDXattr, []byte(id), 0)
	runtime.KeepAlive(fd)
	return err
}

func clearNotebookXattr(dirPath string) {
	fd, err := unix.Open(dirPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	defer unix.Close(fd)
	_ = unix.Fremovexattr(fd, notebookIDXattr)
	runtime.KeepAlive(fd)
}

func liveRootDirPath(dir *os.Root) string {
	if dir == nil {
		return ""
	}
	f, err := dir.Open(".")
	if err == nil {
		p, ok := currentDirPath(f, "")
		_ = f.Close()
		if ok && p != "" {
			return p
		}
	}
	return dir.Name()
}

func isParentVolumeAccessible(parent string, pDev uint64, hasPDev bool, recDev uint64, _ string) bool {
	if recDev != 0 && hasPDev && pDev != recDev {
		return false
	}
	cleanParent := filepath.Clean(parent)
	if recDev == 0 && (cleanParent == "/Volumes" || cleanParent == "/System/Volumes/Data/Volumes") {
		return false
	}
	return true
}

func findSiblingByNotebookXattr(parent, expectedID string) (string, bool, bool) {
	if parent == "" || !IsSafeNotebookID(expectedID) {
		return "", false, false
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", false, isUnreadableDirErr(err) || !isStrictNotExistErr(err)
	}
	const maxSiblingProbes = 256
	probed := 0
	inconclusive := false
	for _, ent := range entries {
		if probed >= maxSiblingProbes {
			break
		}
		if !ent.IsDir() {
			continue
		}
		cand := filepath.Join(parent, ent.Name())
		if IsTrashPath(cand) {
			continue
		}
		probed++
		xid, xErr := readNotebookXattr(cand)
		if xErr == nil && xid == expectedID {
			return cand, true, false
		}
		if isUnreadableDirErr(xErr) || (xErr != nil && !isStrictNotExistErr(xErr) && !errors.Is(xErr, errInvalidNotebookXattr)) {
			inconclusive = true
		}
	}
	return "", false, inconclusive
}
