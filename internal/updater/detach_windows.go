//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

func configureDetachedProcess(_ *exec.Cmd) {}

const (
	winCreateNoWindow  = 0x08000000
	winDetachedProcess = 0x00000008
	winSWHide          = 0
)

var (
	shell32DLL    = syscall.NewLazyDLL("shell32.dll")
	shellExecuteW = shell32DLL.NewProc("ShellExecuteW")
	isUserAnAdmin = shell32DLL.NewProc("IsUserAnAdmin")
)

// launchWindowsUpdater starts the NSIS setup executable completely silently
// (/S /UPDATE). Per-user and writable installations update directly with zero
// UAC prompts and zero installer UI; machine-wide installs (e.g. legacy
// Program Files installs) request a single UAC prompt before Block exits and
// still install with zero installer UI.
func launchWindowsUpdater(exePath string) error {
	params, needUAC := windowsUpdaterPlan()
	if !needUAC {
		cmd := exec.Command(exePath)
		cmd.Dir = filepath.Dir(exePath)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CmdLine:       fmt.Sprintf(`"%s" %s`, exePath, params),
			HideWindow:    true,
			CreationFlags: winCreateNoWindow | winDetachedProcess,
		}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to launch silent updater: %w", err)
		}
		_ = cmd.Process.Release()
		return nil
	}

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
	pArgs, err := syscall.UTF16PtrFromString(params)
	if err != nil {
		return err
	}
	// SW_HIDE = 0 keeps the silent installer completely invisible; ShellExecuteW returns > 32 on success.
	ret, _, callErr := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(pArgs)),
		uintptr(unsafe.Pointer(dir)),
		winSWHide,
	)
	if ret <= 32 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return fmt.Errorf("failed to launch elevated updater (code %d): %w", ret, callErr)
		}
		return fmt.Errorf("failed to launch elevated updater (code %d)", ret)
	}
	return nil
}

// windowsUpdaterPlan builds silent NSIS parameters (/S /UPDATE [/ALLUSERS]
// [/D=<dir>]) and reports whether UAC elevation is required. NSIS reads /D=
// verbatim to the end of the command line, so /D= stays last and unquoted.
func windowsUpdaterPlan() (params string, needUAC bool) {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return "/S /UPDATE", false
	}
	dir := cleanWinDir(filepath.Dir(exe))
	if _, err := os.Stat(filepath.Join(dir, "uninstall.exe")); err != nil {
		// Standalone/portable binary without an installer record: install
		// silently into the default per-user directory without UAC.
		return "/S /UPDATE", false
	}
	machine := isWindowsMachineInstalledDir(dir) || isWindowsUnderProgramFiles(dir) || !canWriteWindowsDir(dir)
	if machine {
		return "/S /UPDATE /ALLUSERS /D=" + dir, !isWindowsAdmin()
	}
	return "/S /UPDATE /D=" + dir, false
}

func cleanWinDir(dir string) string {
	dir = strings.TrimSpace(filepath.Clean(dir))
	for len(dir) > 3 && (strings.HasSuffix(dir, `\`) || strings.HasSuffix(dir, "/")) {
		dir = dir[:len(dir)-1]
	}
	return dir
}

func isWindowsAdmin() bool {
	ret, _, _ := isUserAnAdmin.Call()
	return ret != 0
}

func canWriteWindowsDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".block-update-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func isWindowsUnderDir(dir, base string) bool {
	base = strings.TrimSpace(base)
	if base == "" {
		return false
	}
	nd := strings.ToLower(cleanWinDir(dir))
	nb := strings.ToLower(cleanWinDir(base))
	return nd == nb || strings.HasPrefix(nd, nb+`\`)
}

func isWindowsUnderProgramFiles(dir string) bool {
	for _, env := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
		if isWindowsUnderDir(dir, os.Getenv(env)) {
			return true
		}
	}
	return false
}

// isWindowsMachineInstalledDir checks both 32-bit (NSIS) and 64-bit HKLM
// uninstall views to detect existing machine-wide installations.
func isWindowsMachineInstalledDir(dir string) bool {
	normDir := strings.ToLower(cleanWinDir(dir))
	if normDir == "" {
		return false
	}
	for _, view := range []uint32{registry.WOW64_32KEY, 0} {
		uninstKey, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall`, registry.ENUMERATE_SUB_KEYS|view)
		if err != nil {
			continue
		}
		names, _ := uninstKey.ReadSubKeyNames(-1)
		uninstKey.Close()
		for _, name := range names {
			if name != "Block" && !strings.HasPrefix(name, "Block-") {
				continue
			}
			k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall\`+name, registry.QUERY_VALUE|view)
			if err != nil {
				continue
			}
			loc, _, locErr := k.GetStringValue("InstallLocation")
			k.Close()
			if locErr == nil && strings.ToLower(cleanWinDir(loc)) == normDir {
				return true
			}
		}
	}
	return false
}
