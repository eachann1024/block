//go:build linux

package plugins

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func fsSwapFilesInDir(dir *os.Root, a, b string) (bool, error) {
	f, err := dir.Open(".")
	if err != nil {
		return false, err
	}
	defer f.Close()
	fd := int(f.Fd())
	err = unix.Renameat2(fd, a, fd, b, unix.RENAME_EXCHANGE)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) ||
		errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.EINVAL) {
		return false, nil
	}
	if errors.Is(err, syscall.ENOENT) {
		return true, os.ErrNotExist
	}
	return true, err
}

func isPlatformICloudPlaceholder(_ os.FileInfo) bool {
	return false
}
