//go:build unix

package files

import (
	"errors"
	"os"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

func statDirDevIno(_ string, info os.FileInfo) (uint64, uint64, bool) {
	if info == nil {
		return 0, 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), true
}

func statRootHandleDevIno(root *os.Root) (uint64, uint64, bool) {
	if root == nil {
		return 0, 0, false
	}
	f, err := root.Open(".")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, 0, false
	}
	return statDirDevIno("", info)
}

func isUnreadableDirErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EIO) ||
		errors.Is(err, syscall.ENXIO) ||
		errors.Is(err, syscall.ENODEV) ||
		errors.Is(err, syscall.ETIMEDOUT) ||
		errors.Is(err, syscall.ESTALE) ||
		errors.Is(err, syscall.EBUSY) ||
		errors.Is(err, syscall.EAGAIN) ||
		errors.Is(err, syscall.EINTR) ||
		errors.Is(err, syscall.ENOTCONN) ||
		errors.Is(err, syscall.EHOSTDOWN) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETDOWN) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.ESHUTDOWN) {
		return true
	}
	// Any OS errno other than explicit ENOENT indicates an unreadable or transient state,
	// not confirmed absence.
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno != syscall.ENOENT
	}
	return false
}

func isCrossDeviceErr(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}

func isStrictNotExistErr(err error) bool {
	if err == nil || isUnreadableDirErr(err) {
		return false
	}
	return errors.Is(err, syscall.ENOENT)
}

func isSymlinkOrReparseInfo(info os.FileInfo) bool {
	return info != nil && info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

func renameFileAcrossRoots(srcRoot *os.Root, srcName string, dstRoot *os.Root, dstName string) error {
	if srcRoot == nil || dstRoot == nil || srcName == "" || dstName == "" {
		return os.ErrInvalid
	}
	srcDirF, err := srcRoot.Open(".")
	if err != nil {
		return err
	}
	defer srcDirF.Close()
	dstDirF, err := dstRoot.Open(".")
	if err != nil {
		return err
	}
	defer dstDirF.Close()
	err = unix.Renameat(int(srcDirF.Fd()), srcName, int(dstDirF.Fd()), dstName)
	runtime.KeepAlive(srcDirF)
	runtime.KeepAlive(dstDirF)
	return err
}
