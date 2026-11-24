//go:build windows

package installer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
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
	shell32DLL          = syscall.NewLazyDLL("shell32.dll")
	procIsUserAnAdmin   = shell32DLL.NewProc("IsUserAnAdmin")
	procShellExecuteExW = shell32DLL.NewProc("ShellExecuteExW")
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

const (
	sidLocalSystem      = "S-1-5-18"
	sidAdministrators   = "S-1-5-32-544"
	sidTrustedInstaller = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
	sidCreatorOwner     = "S-1-3-0"
	winFileDeleteChild  = 0x00000040 // FILE_DELETE_CHILD from winnt.h

	// Rights that let a non-privileged account write, delete, or take over a target folder/file.
	winWriteOrModifyMask = uint32(
		windows.FILE_WRITE_DATA |
			windows.FILE_APPEND_DATA |
			windows.FILE_WRITE_EA |
			windows.FILE_WRITE_ATTRIBUTES |
			winFileDeleteChild |
			windows.DELETE |
			windows.WRITE_DAC |
			windows.WRITE_OWNER |
			windows.GENERIC_WRITE |
			windows.GENERIC_ALL,
	)

	// Rights on an already-existing parent directory that let a non-privileged
	// account delete/rename an existing child directory or alter parent ACLs.
	winAncestorTamperMask = uint32(
		winFileDeleteChild |
			windows.WRITE_DAC |
			windows.WRITE_OWNER |
			windows.GENERIC_ALL,
	)
)

func isPrivilegedOwnerSID(sid *windows.SID) bool {
	if sid == nil {
		return false
	}
	switch sid.String() {
	case sidLocalSystem, sidAdministrators, sidTrustedInstaller:
		return true
	default:
		return false
	}
}

func isPrivilegedWriteSID(sid *windows.SID, allowCreatorOwner bool) bool {
	if isPrivilegedOwnerSID(sid) {
		return true
	}
	return allowCreatorOwner && sid != nil && sid.String() == sidCreatorOwner
}

// validateMachineInstallDir enforces that a machine-wide (/ALLUSERS) target
// cannot be modified, replaced, or pre-created via junctions/symlinks by
// non-administrator accounts.
func validateMachineInstallDir(dir string) error {
	dir = cleanInstallDir(dir)
	if !validInstallDir(dir) || len(dir) <= 3 {
		return errors.New("machine install directory must be a valid non-root absolute path")
	}
	cleaned := filepath.Clean(strings.ReplaceAll(dir, "/", `\`))
	if !validInstallDir(cleaned) || len(cleaned) <= 3 {
		return errors.New("machine install directory is invalid")
	}

	// Build path chain from drive root (e.g. C:\) down to target directory.
	root := cleaned[:3]
	rest := strings.Split(strings.TrimPrefix(cleaned, root), `\`)
	chain := make([]string, 0, len(rest)+1)
	chain = append(chain, root)
	cur := root
	for _, part := range rest {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("invalid path segment in machine install directory: %q", cleaned)
		}
		if strings.HasSuffix(cur, `\`) {
			cur += part
		} else {
			cur += `\` + part
		}
		chain = append(chain, cur)
	}

	// Determine which prefixes exist on disk right now.
	attrs := make([]uint32, len(chain))
	exists := make([]bool, len(chain))
	for i, p := range chain {
		pPtr, err := windows.UTF16PtrFromString(p)
		if err != nil {
			return err
		}
		a, err := windows.GetFileAttributes(pPtr)
		if err != nil {
			if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
				break
			}
			return fmt.Errorf("stat %s: %w", p, err)
		}
		if a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("reparse point not allowed in machine install path: %s", p)
		}
		if a&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return fmt.Errorf("non-directory component in machine install path: %s", p)
		}
		attrs[i] = a
		exists[i] = true
	}
	if !exists[0] {
		return fmt.Errorf("volume root missing: %s", chain[0])
	}

	for i, p := range chain {
		if !exists[i] {
			break
		}
		isTarget := i == len(chain)-1
		nextExists := !isTarget && exists[i+1]
		if err := checkProtectedACL(p, isTarget || !nextExists); err != nil {
			return err
		}
	}

	// If target directory already exists, also verify existing installed binaries
	// inside it are not reparse points or writable by non-administrators.
	if exists[len(chain)-1] {
		for _, name := range []string{appExeName, "uninstall.exe", appExeName + ".new", appExeName + ".old"} {
			fp := filepath.Join(cleaned, name)
			pPtr, err := windows.UTF16PtrFromString(fp)
			if err != nil {
				return err
			}
			a, err := windows.GetFileAttributes(pPtr)
			if err != nil {
				if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
					continue
				}
				return fmt.Errorf("stat %s: %w", fp, err)
			}
			if a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				return fmt.Errorf("reparse point not allowed at %s", fp)
			}
			if err := checkProtectedACL(fp, true); err != nil {
				return err
			}
		}
	}

	return nil
}

// checkProtectedACL validates ownership and DACL entries on path.
// When strictWriteCheck is true (target directory, existing binaries, or nearest
// existing ancestor where new folders will be created), any effective or
// inheritable write/modify/delete right for a non-privileged SID is rejected.
// When false (higher ancestor whose child already exists, e.g. C:\ above
// C:\Program Files), effective tamper rights (FILE_DELETE_CHILD, WRITE_DAC,
// WRITE_OWNER, GENERIC_ALL) for non-privileged SIDs are rejected.
func checkProtectedACL(path string, strictWriteCheck bool) error {
	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return fmt.Errorf("read ACL for %s: %w: %v", path, errACLReadDenied, err)
		}
		return fmt.Errorf("read ACL for %s: %w", path, err)
	}
	if sd == nil {
		return fmt.Errorf("missing security descriptor for %s", path)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read owner for %s: %w", path, err)
	}
	if !isPrivilegedOwnerSID(owner) {
		return fmt.Errorf("unprivileged owner on %s", path)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read DACL for %s: %w", path, err)
	}
	// A nil DACL grants Everyone Full Control on Windows.
	if dacl == nil {
		return fmt.Errorf("unrestricted NULL DACL on %s", path)
	}
	for idx := uint32(0); idx < uint32(dacl.AceCount); idx++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, idx, &ace); err != nil {
			return fmt.Errorf("read ACE %d on %s: %w", idx, path, err)
		}
		if ace == nil {
			continue
		}
		// Skip explicit deny ACEs; check allow ACEs (fail-closed on exotic allow types).
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		flags := ace.Header.AceFlags
		inheritOnly := flags&windows.INHERIT_ONLY_ACE != 0
		inheritable := flags&(windows.CONTAINER_INHERIT_ACE|windows.OBJECT_INHERIT_ACE) != 0
		mask := uint32(ace.Mask)

		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			if mask&winWriteOrModifyMask != 0 {
				return fmt.Errorf("unsupported allow ACE type %d with write mask on %s", ace.Header.AceType, path)
			}
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if strictWriteCheck {
			// CREATOR OWNER is only safe on an inherit-only ACE when no
			// non-privileged account can create children here (verified by
			// rejecting all other non-privileged write ACEs in this same loop).
			if isPrivilegedWriteSID(sid, inheritOnly) {
				continue
			}
			if (!inheritOnly || inheritable) && (mask&winWriteOrModifyMask != 0) {
				return fmt.Errorf("non-admin principal %s has write access on %s", sid.String(), path)
			}
		} else {
			if inheritOnly || isPrivilegedWriteSID(sid, false) {
				continue
			}
			if mask&winAncestorTamperMask != 0 {
				return fmt.Errorf("non-admin principal %s can tamper with ancestor %s", sid.String(), path)
			}
		}
	}
	return nil
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
