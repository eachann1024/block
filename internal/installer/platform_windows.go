//go:build windows

package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func defaultInstallDir() string {
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

// runSilentSetup runs `setup.exe /S /D=<dir>`. NSIS takes everything after /D=
// verbatim to the end of the command line, so the directory must be last and
// unquoted even when it holds spaces; the command line is therefore built by
// hand instead of through Go's argument quoting.
func runSilentSetup(setupExe, dir string) error {
	cmd := exec.Command(setupExe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: fmt.Sprintf(`"%s" /S /D=%s`, setupExe, dir),
	}
	cmd.Dir = filepath.Dir(setupExe)
	return cmd.Run()
}

// launchInstalledApp starts Block through the shell. Setup runs elevated, and a
// direct start would hand that elevation to Block; explorer.exe passes the
// launch to the desktop shell, which runs it as the signed-in user.
func launchInstalledApp(exe string) error {
	if _, err := os.Stat(exe); err != nil {
		return err
	}
	explorer := filepath.Join(os.Getenv("WINDIR"), "explorer.exe")
	if _, err := os.Stat(explorer); err != nil {
		explorer = "explorer.exe"
	}
	return exec.Command(explorer, exe).Start()
}

var procGetUserDefaultUILanguage = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

// systemPrefersEnglish is false only for a Chinese Windows display language
// (primary language ID 0x04).
func systemPrefersEnglish() bool {
	lang, _, _ := procGetUserDefaultUILanguage.Call()
	return lang&0x3ff != 0x04
}
