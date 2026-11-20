//go:build windows

package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	winBlockProgID       = "Block.Document"
	winSHCNEAssocChanged = 0x08000000
	winSHCNFIdList       = 0x0000
)

var (
	winShell32DLL    = syscall.NewLazyDLL("shell32.dll")
	winSHChangeNotif = winShell32DLL.NewProc("SHChangeNotify")
)

func syncPlatformFileAssociations(appDataDir string, pluginExts []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
		exePath = resolved
	}

	desired := unionSortedStrings(builtinMarkdownExtensions, pluginExts)
	prev, hasPrev := loadFileAssocState(appDataDir)
	if hasPrev &&
		prev.Version == fileAssocStateVersion &&
		prev.ExecutablePath == exePath &&
		equalStringSlices(prev.Extensions, desired) {
		return nil
	}

	if err := registerWinProgID(exePath); err != nil {
		return err
	}

	desiredSet := make(map[string]struct{}, len(desired))
	for _, ext := range desired {
		desiredSet[ext] = struct{}{}
		if err := setWinOpenWithProgid(ext); err != nil {
			return err
		}
	}

	for _, oldExt := range prev.Extensions {
		if _, keep := desiredSet[oldExt]; keep {
			continue
		}
		_ = removeWinOpenWithProgid(oldExt)
	}

	_, _, _ = winSHChangeNotif.Call(
		uintptr(winSHCNEAssocChanged),
		uintptr(winSHCNFIdList),
		0,
		0,
	)

	return saveFileAssocState(appDataDir, fileAssocState{
		ExecutablePath: exePath,
		Extensions:     desired,
	})
}

func registerWinProgID(exePath string) error {
	openCmd := fmt.Sprintf(`"%s" "%%1"`, exePath)
	iconVal := fmt.Sprintf(`"%s",0`, exePath)

	if err := setRegDefault(registry.CURRENT_USER, `Software\Classes\`+winBlockProgID, "Block Document"); err != nil {
		return err
	}
	if err := setRegDefault(registry.CURRENT_USER, `Software\Classes\`+winBlockProgID+`\DefaultIcon`, iconVal); err != nil {
		return err
	}
	if err := setRegDefault(registry.CURRENT_USER, `Software\Classes\`+winBlockProgID+`\shell\open\command`, openCmd); err != nil {
		return err
	}

	exeName := filepath.Base(exePath)
	if exeName != "" {
		appKey := `Software\Classes\Applications\` + exeName
		if err := setRegString(registry.CURRENT_USER, appKey, "FriendlyAppName", "Block"); err != nil {
			return err
		}
		_ = setRegDefault(registry.CURRENT_USER, appKey+`\DefaultIcon`, iconVal)
		_ = setRegDefault(registry.CURRENT_USER, appKey+`\shell\open\command`, openCmd)
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
