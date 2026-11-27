//go:build windows

package files

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

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

func isCrossDeviceErr(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.Errno(17))
}

func readNotebookXattr(string) string    { return "" }
func writeNotebookXattr(string, string) {}
func clearNotebookXattr(string)         {}
