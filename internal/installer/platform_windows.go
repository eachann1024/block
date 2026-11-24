//go:build windows

package installer

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

const (
	seeMaskNoCloseProcess = 0x00000040
	swHide                = 0
	errWinCanceled        = syscall.Errno(1223)
)

type shellExecuteInfoW struct {
	cbSize         uint32
	fMask          uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       uintptr
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      uintptr
	dwHotKey       uint32
	hIconOrMonitor uintptr
	hProcess       syscall.Handle
}

var (
	procGetUserDefaultUILanguage = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")
	shell32DLL                   = syscall.NewLazyDLL("shell32.dll")
	procIsUserAnAdmin            = shell32DLL.NewProc("IsUserAnAdmin")
	procShellExecuteExW          = shell32DLL.NewProc("ShellExecuteExW")
)

func defaultInstallDir() string {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
	}
	if localAppData != "" {
		return filepath.Join(localAppData, "Programs", "Block")
	}
	root := strings.TrimSpace(os.Getenv("ProgramW6432"))
	if root == "" {
		root = strings.TrimSpace(os.Getenv("ProgramFiles"))
	}
	if root == "" {
		root = `C:\Program Files`
	}
	return filepath.Join(root, "Block")
}

// validInstallDir accepts an absolute drive path without characters Windows
// forbids in folder names; anything else would make NSIS silently fall back to
// its default directory.
func validInstallDir(dir string) bool {
	if len(dir) < 3 || dir[1] != ':' || (dir[2] != '\\' && dir[2] != '/') {
		return false
	}
	drive := dir[0]
	if !(drive >= 'a' && drive <= 'z') && !(drive >= 'A' && drive <= 'Z') {
		return false
	}
	return !strings.ContainsAny(dir[2:], `:*?"<>|`)
}

func isCurrentProcessAdmin() bool {
	ret, _, _ := procIsUserAnAdmin.Call()
	return ret != 0
}

func normalizeInstallPath(dir string) string {
	return strings.ToLower(cleanInstallDir(filepath.Clean(dir)))
}

func isUnderDir(dir, base string) bool {
	base = strings.TrimSpace(base)
	if base == "" {
		return false
	}
	nd := normalizeInstallPath(dir)
	nb := normalizeInstallPath(base)
	if nd == nb {
		return true
	}
	return strings.HasPrefix(nd, nb+`\`)
}

func isUnderProgramFiles(dir string) bool {
	for _, env := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
		if isUnderDir(dir, os.Getenv(env)) {
			return true
		}
	}
	return false
}

// isMachineInstalledDir checks both 32-bit (NSIS default) and 64-bit HKLM
// uninstall views to see whether dir is already a machine-wide Block install.
func isMachineInstalledDir(dir string) bool {
	normDir := normalizeInstallPath(dir)
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
			if locErr == nil && normalizeInstallPath(loc) == normDir {
				return true
			}
		}
	}
	return false
}

// canWriteInstallDir probes the target directory (or its nearest existing
// ancestor) with a temporary file to determine whether the current token can
// install without UAC elevation.
func canWriteInstallDir(dir string) bool {
	probeDir := existingAncestor(dir)
	if probeDir == "" {
		return false
	}
	f, err := os.CreateTemp(probeDir, ".block-write-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func shouldInstallMachineScope(dir string, allUsers bool) bool {
	return allUsers || isMachineInstalledDir(dir) || isUnderProgramFiles(dir) || !canWriteInstallDir(dir)
}

func setupNeedsElevation(dir string, allUsers bool) bool {
	return shouldInstallMachineScope(dir, allUsers) && !isCurrentProcessAdmin()
}

// runSilentSetup runs `setup.exe /S [/ALLUSERS] /D=<dir>`. NSIS takes
// everything after /D= verbatim to the end of the command line, so the
// directory must be last and unquoted even when it holds spaces.
func runSilentSetup(setupExe, dir string, allUsers bool) error {
	machine := shouldInstallMachineScope(dir, allUsers)
	params := fmt.Sprintf(`/S /D=%s`, dir)
	if machine {
		params = fmt.Sprintf(`/S /ALLUSERS /D=%s`, dir)
	}
	if machine && !isCurrentProcessAdmin() {
		return runElevatedSetupAndWait(setupExe, params)
	}
	cmd := exec.Command(setupExe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: fmt.Sprintf(`"%s" %s`, setupExe, params),
	}
	cmd.Dir = filepath.Dir(setupExe)
	return cmd.Run()
}

func runElevatedSetupAndWait(setupExe, params string) error {
	verb, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(setupExe)
	if err != nil {
		return err
	}
	args, err := syscall.UTF16PtrFromString(params)
	if err != nil {
		return err
	}
	workDir, err := syscall.UTF16PtrFromString(filepath.Dir(setupExe))
	if err != nil {
		return err
	}
	sei := shellExecuteInfoW{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfoW{})),
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: args,
		lpDirectory:  workDir,
		nShow:        swHide,
	}
	ok, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if ok == 0 {
		if callErr == errWinCanceled {
			return errElevationCanceled
		}
		if callErr != nil && callErr != syscall.Errno(0) {
			return fmt.Errorf("failed to elevate setup: %w", callErr)
		}
		return fmt.Errorf("failed to elevate setup")
	}
	if sei.hProcess == 0 {
		return nil
	}
	defer syscall.CloseHandle(sei.hProcess)
	if _, err := syscall.WaitForSingleObject(sei.hProcess, syscall.INFINITE); err != nil {
		return err
	}
	var exitCode uint32
	if err := syscall.GetExitCodeProcess(sei.hProcess, &exitCode); err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("elevated setup exited with code %d", exitCode)
	}
	return nil
}

// launchInstalledApp starts Block cleanly as the current user. When setup
// itself runs elevated, delegate through explorer.exe so Block does not inherit
// administrator privileges.
func launchInstalledApp(exe string) error {
	if _, err := os.Stat(exe); err != nil {
		return err
	}
	if !isCurrentProcessAdmin() {
		return exec.Command(exe).Start()
	}
	explorer := filepath.Join(os.Getenv("WINDIR"), "explorer.exe")
	if _, err := os.Stat(explorer); err != nil {
		explorer = "explorer.exe"
	}
	return exec.Command(explorer, exe).Start()
}

// systemPrefersEnglish is false only for a Chinese Windows display language
// (primary language ID 0x04).
func systemPrefersEnglish() bool {
	lang, _, _ := procGetUserDefaultUILanguage.Call()
	return lang&0x3ff != 0x04
}
