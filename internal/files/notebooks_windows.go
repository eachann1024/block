//go:build windows

package files

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
)

func supportsNotebookXattr() bool { return false }

func statDirDevIno(clean string, _ os.FileInfo) (uint64, uint64, bool) {
	ptr, err := windows.UTF16PtrFromString(clean)
	if err != nil {
		return 0, 0, false
	}
	h, err := windows.CreateFile(
		ptr,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return 0, 0, false
	}
	defer windows.CloseHandle(h)
	var bhfi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &bhfi); err != nil {
		return 0, 0, false
	}
	ino := (uint64(bhfi.FileIndexHigh) << 32) | uint64(bhfi.FileIndexLow)
	return uint64(bhfi.VolumeSerialNumber), ino, true
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
	var bhfi windows.ByHandleFileInformation
	err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &bhfi)
	runtime.KeepAlive(f)
	if err != nil {
		return 0, 0, false
	}
	ino := (uint64(bhfi.FileIndexHigh) << 32) | uint64(bhfi.FileIndexLow)
	return uint64(bhfi.VolumeSerialNumber), ino, true
}

func isUnreadableDirErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
		errors.Is(err, windows.ERROR_NOT_READY) ||
		errors.Is(err, windows.ERROR_BAD_UNIT) ||
		errors.Is(err, windows.ERROR_CRC) ||
		errors.Is(err, windows.ERROR_BAD_NETPATH) ||
		errors.Is(err, windows.ERROR_DEV_NOT_EXIST) ||
		errors.Is(err, windows.ERROR_INVALID_DRIVE) ||
		errors.Is(err, windows.ERROR_BAD_NET_NAME) ||
		errors.Is(err, windows.ERROR_NETWORK_BUSY) ||
		errors.Is(err, windows.ERROR_NETWORK_UNREACHABLE) ||
		errors.Is(err, windows.ERROR_NETNAME_DELETED) ||
		errors.Is(err, windows.ERROR_UNEXP_NET_ERR) ||
		errors.Is(err, windows.ERROR_SEM_TIMEOUT) ||
		errors.Is(err, windows.ERROR_IO_DEVICE) ||
		errors.Is(err, windows.ERROR_BUSY) ||
		errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
		return true
	}
	var werr windows.Errno
	if errors.As(err, &werr) {
		return werr != windows.ERROR_FILE_NOT_FOUND && werr != windows.ERROR_PATH_NOT_FOUND
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		we := windows.Errno(errno)
		return we != windows.ERROR_FILE_NOT_FOUND && we != windows.ERROR_PATH_NOT_FOUND
	}
	return false
}

func isCrossDeviceErr(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.Errno(17))
}

func isStrictNotExistErr(err error) bool {
	if err == nil || isUnreadableDirErr(err) {
		return false
	}
	return errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND)
}

func isSymlinkOrReparseInfo(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return true
	}
	if st, ok := info.Sys().(*syscall.Win32FileAttributeData); ok && st != nil {
		if st.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return true
		}
	}
	return false
}

func liveRootDirPath(dir *os.Root) string {
	if dir == nil {
		return ""
	}
	f, err := dir.Open(".")
	if err == nil {
		p, ok := currentDirPath(windows.Handle(f.Fd()), "")
		runtime.KeepAlive(f)
		_ = f.Close()
		if ok && p != "" {
			return p
		}
	}
	return dir.Name()
}

func renameFileAcrossRoots(srcRoot *os.Root, srcName string, dstRoot *os.Root, dstName string) error {
	if srcRoot == nil || dstRoot == nil || srcName == "" || dstName == "" {
		return os.ErrInvalid
	}
	srcDir := liveRootDirPath(srcRoot)
	if srcDir == "" {
		return os.ErrInvalid
	}
	dstDir := liveRootDirPath(dstRoot)
	if dstDir == "" {
		return os.ErrInvalid
	}
	return os.Rename(filepath.Join(srcDir, srcName), filepath.Join(dstDir, dstName))
}

func isParentVolumeAccessible(_ string, pDev uint64, hasPDev bool, recDev uint64, _ string) bool {
	if recDev != 0 && hasPDev && pDev != recDev {
		return false
	}
	return true
}

func findSiblingByNotebookXattr(string, string) (string, bool, bool) { return "", false, false }

func readNotebookXattr(string) (string, error) { return "", nil }
func writeNotebookXattr(string, string) error  { return nil }
func clearNotebookXattr(string)                {}
