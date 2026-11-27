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
	id := fileIdentityTokenFromHandle(windows.Handle(f.Fd()))
	runtime.KeepAlive(f)
	return id
}

func fileIdentityTokenFromHandle(h windows.Handle) string {
	if h == 0 || h == windows.InvalidHandle {
		return ""
	}
	var bhfi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &bhfi); err != nil {
		return ""
	}
	return formatWinFileIdentity(&bhfi)
}

func formatWinFileIdentity(bhfi *windows.ByHandleFileInformation) string {
	if bhfi == nil {
		return ""
	}
	return fmt.Sprintf("win:%d:%d:%d", bhfi.VolumeSerialNumber, bhfi.FileIndexHigh, bhfi.FileIndexLow)
}
