//go:build windows

package updater

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

func configureDetachedProcess(_ *exec.Cmd) {}

var (
	shell32DLL    = syscall.NewLazyDLL("shell32.dll")
	shellExecuteW = shell32DLL.NewProc("ShellExecuteW")
)

// launchWindowsElevatedInstaller invokes the NSIS setup executable via ShellExecuteW("runas")
// so standard non-elevated Windows users receive a proper UAC elevation prompt.
func launchWindowsElevatedInstaller(exePath string) error {
	verb, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(exePath)
	if err != nil {
		return err
	}
	dir, err := syscall.UTF16PtrFromString(filepath.Dir(exePath))
	if err != nil {
		return err
	}
	// SW_SHOWNORMAL = 1; ShellExecuteW returns > 32 on success.
	ret, _, callErr := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0,
		uintptr(unsafe.Pointer(dir)),
		1,
	)
	if ret <= 32 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return fmt.Errorf("failed to launch elevated installer (code %d): %w", ret, callErr)
		}
		return fmt.Errorf("failed to launch elevated installer (code %d)", ret)
	}
	return nil
}
