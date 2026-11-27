//go:build darwin

package files

import (
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

const notebookIDXattr = "com.blockmd.notebook-id"

func readNotebookXattr(dirPath string) string {
	fd, err := unix.Open(dirPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return ""
	}
	defer unix.Close(fd)
	var buf [128]byte
	n, err := unix.Fgetxattr(fd, notebookIDXattr, buf[:])
	runtime.KeepAlive(fd)
	if err != nil || n <= 0 {
		return ""
	}
	val := strings.TrimSpace(string(buf[:n]))
	if !IsSafeNotebookID(val) {
		return ""
	}
	return val
}

func writeNotebookXattr(dirPath, id string) {
	if !IsSafeNotebookID(id) {
		return
	}
	fd, err := unix.Open(dirPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	defer unix.Close(fd)
	var curBuf [128]byte
	if n, gErr := unix.Fgetxattr(fd, notebookIDXattr, curBuf[:]); gErr == nil && n > 0 {
		if strings.TrimSpace(string(curBuf[:n])) == id {
			runtime.KeepAlive(fd)
			return
		}
	}
	_ = unix.Fsetxattr(fd, notebookIDXattr, []byte(id), 0)
	runtime.KeepAlive(fd)
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
