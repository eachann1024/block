//go:build windows

package files

import (
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/windows"
)

// FileIdentityToken returns a stable cross-restart identity token for an open Windows file/directory handle.
func FileIdentityToken(_ os.FileInfo, f *os.File) string {
	if f == nil {
		return ""
	}
	var bhfi windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &bhfi)
	runtime.KeepAlive(f)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("win:%d:%d:%d", bhfi.VolumeSerialNumber, bhfi.FileIndexHigh, bhfi.FileIndexLow)
}
