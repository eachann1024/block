//go:build windows

package updater

import (
	"fmt"
	"os"
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
	params, err := syscall.UTF16PtrFromString(windowsInstallerParams())
	if err != nil {
		return err
	}
	// SW_SHOWNORMAL = 1; ShellExecuteW returns > 32 on success.
	ret, _, callErr := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
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

// windowsInstallerParams tells setup it was started by the updater (/UPDATE:
// skip the install prompt, reopen Block when done) and, when this process runs
// from a setup-made installation, where that is (/D=), so an install outside
// the default directory is updated in place. NSIS reads /D= verbatim to the end
// of the command line, so it stays last and unquoted.
func windowsInstallerParams() string {
	params := "/UPDATE"
	exe, err := os.Executable()
	if err != nil {
		return params
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, "uninstall.exe")); err != nil {
		return params
	}
	return params + " /D=" + dir
}
