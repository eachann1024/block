//go:build windows

package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	winBlockProgID       = "Block.Document"
	winSHCNEAssocChanged = 0x08000000
	winSHCNFIdList       = 0x0000
)

var (
	winShell32DLL     = syscall.NewLazyDLL("shell32.dll")
	winSHChangeNotif  = winShell32DLL.NewProc("SHChangeNotify")
	winReconciledOnce atomic.Bool
)

func isFileAssocLockPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	const stillActive = 259
	return code == stillActive
}

func syncPlatformFileAssociations(appDataDir string, pluginExts []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
		exePath = resolved
	}

	filtered := make([]string, 0, len(pluginExts))
	for _, ext := range pluginExts {
		if isSingleSegmentExtension(ext) {
			filtered = append(filtered, ext)
		}
	}
	desired := unionSortedStrings(builtinMarkdownExtensions, filtered)

	prev, hasPrev := loadFileAssocState(appDataDir)
	firstRun := winReconciledOnce.CompareAndSwap(false, true)
	if hasPrev &&
		prev.Version == fileAssocStateVersion &&
		prev.ExecutablePath == exePath &&
		equalStringSlices(prev.Extensions, desired) &&
		winProgIDMatches(exePath) &&
		(!firstRun || winAllExtensionsRegistered(desired)) {
		return nil
	}

	if err := registerWinProgID(exePath); err != nil {
		return err
	}

	prevSet := make(map[string]struct{}, len(prev.Extensions))
	for _, ext := range prev.Extensions {
		prevSet[ext] = struct{}{}
	}
	desiredSet := make(map[string]struct{}, len(desired))
	registered := make([]string, 0, len(desired))
	var firstErr error
	for _, ext := range desired {
		desiredSet[ext] = struct{}{}
		if err := setWinOpenWithProgid(ext); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			// If ext was previously registered (or still exists in registry), keep
			// tracking it so a later disable/cleanup never orphans it.
			if _, wasPrev := prevSet[ext]; wasPrev || winHasOpenWithProgid(ext) {
				registered = append(registered, ext)
			}
			continue
		}
		registered = append(registered, ext)
	}

	var keptStale []string
	for _, oldExt := range prev.Extensions {
		if _, keep := desiredSet[oldExt]; keep {
			continue
		}
		if err := removeWinOpenWithProgid(oldExt); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			keptStale = append(keptStale, oldExt)
		}
	}

	notifyWinAssocChanged()

	actual := unionSortedStrings(registered, keptStale)
	saveErr := saveFileAssocState(appDataDir, fileAssocState{
		ExecutablePath: exePath,
		Extensions:     actual,
	})
	if firstErr != nil {
		return firstErr
	}
	return saveErr
}

func cleanupPlatformFileAssociations(appDataDir string, _ bool) error {
	prev, _ := loadFileAssocState(appDataDir)
	allExts := unionSortedStrings(builtinMarkdownExtensions, prev.Extensions)
	var firstErr error
	var remaining []string
	for _, ext := range allExts {
		if !isSingleSegmentExtension(ext) {
			continue
		}
		if err := removeWinOpenWithProgid(ext); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			remaining = append(remaining, ext)
		}
	}
	deleteRegTree(registry.CURRENT_USER, `Software\Classes\`+winBlockProgID)
	notifyWinAssocChanged()
	if firstErr != nil {
		_ = saveFileAssocState(appDataDir, fileAssocState{
			ExecutablePath: prev.ExecutablePath,
			Extensions:     remaining,
		})
		return firstErr
	}
	return nil
}

func notifyWinAssocChanged() {
	_, _, _ = winSHChangeNotif.Call(
		uintptr(winSHCNEAssocChanged),
		uintptr(winSHCNFIdList),
		0,
		0,
	)
}

func winOpenCommand(exePath string) string {
	return fmt.Sprintf(`"%s" "%%1"`, exePath)
}

func winProgIDMatches(exePath string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+winBlockProgID+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	val, _, err := k.GetStringValue("")
	return err == nil && val == winOpenCommand(exePath)
}

func winHasOpenWithProgid(ext string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+ext+`\OpenWithProgids`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetValue(winBlockProgID, nil)
	return err == nil
}

func winAllExtensionsRegistered(exts []string) bool {
	for _, ext := range exts {
		if !winHasOpenWithProgid(ext) {
			return false
		}
	}
	return true
}

// registerWinProgID registers Block's dedicated ProgID under HKCU\Software\Classes
// without touching shared Applications\<exe>.exe keys owned by other installs.
func registerWinProgID(exePath string) error {
	openCmd := winOpenCommand(exePath)
	iconVal := fmt.Sprintf(`"%s",0`, exePath)

	progRoot := `Software\Classes\` + winBlockProgID
	if err := setRegDefault(registry.CURRENT_USER, progRoot, "Block Document"); err != nil {
		return err
	}
	if err := setRegString(registry.CURRENT_USER, progRoot, "FriendlyTypeName", "Block Document"); err != nil {
		return err
	}
	if err := setRegDefault(registry.CURRENT_USER, progRoot+`\DefaultIcon`, iconVal); err != nil {
		return err
	}
	if err := setRegString(registry.CURRENT_USER, progRoot+`\Application`, "ApplicationName", "Block"); err != nil {
		return err
	}
	_ = setRegString(registry.CURRENT_USER, progRoot+`\Application`, "ApplicationDescription", "Block")
	_ = setRegString(registry.CURRENT_USER, progRoot+`\shell\open`, "FriendlyAppName", "Block")
	if err := setRegDefault(registry.CURRENT_USER, progRoot+`\shell\open\command`, openCmd); err != nil {
		return err
	}
	return nil
}

func setWinOpenWithProgid(ext string) error {
	return setRegString(registry.CURRENT_USER, `Software\Classes\`+ext+`\OpenWithProgids`, winBlockProgID, "")
}

func removeWinOpenWithProgid(ext string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+ext+`\OpenWithProgids`, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(winBlockProgID); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func deleteRegTree(root registry.Key, subkey string) {
	k, err := registry.OpenKey(root, subkey, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err == nil {
		names, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, name := range names {
			deleteRegTree(root, subkey+`\`+name)
		}
	}
	_ = registry.DeleteKey(root, subkey)
}

func setRegDefault(root registry.Key, subkey, value string) error {
	return setRegString(root, subkey, "", value)
}

func setRegString(root registry.Key, subkey, name, value string) error {
	k, _, err := registry.CreateKey(root, subkey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("registry create %s: %w", subkey, err)
	}
	defer k.Close()
	if err := k.SetStringValue(name, value); err != nil {
		return fmt.Errorf("registry write %s\\%s: %w", subkey, name, err)
	}
	return nil
}
