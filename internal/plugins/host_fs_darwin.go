//go:build darwin

package plugins

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const darwinSFDatalessFlag = 0x40000000

func fsSwapFilesInDir(dir *os.Root, a, b string) (bool, error) {
	f, err := dir.Open(".")
	if err != nil {
		return false, err
	}
	defer f.Close()
	fd := int(f.Fd())
	err = unix.RenameatxNp(fd, a, fd, b, unix.RENAME_SWAP)
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

func isPlatformICloudPlaceholder(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Flags&darwinSFDatalessFlag != 0
	}
	return false
}
