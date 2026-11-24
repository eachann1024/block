//go:build !windows

package installer

import (
	"errors"
	"path/filepath"

	"block/internal/model"
	"block/internal/theme"
)

// Setup only ships on Windows; elsewhere the window exists for design preview
// (BLOCK_SETUP_PREVIEW), which never reaches the real install steps.

func defaultInstallDir() string { return `C:\Program Files\Block` }

func validInstallDir(dir string) bool { return filepath.IsAbs(dir) }

func runSilentSetup(string, string) error {
	return errors.New("setup is only available on Windows")
}

func launchInstalledApp(string) error { return nil }

func systemPrefersEnglish() bool { return theme.SystemLanguage() == model.LangEn }
