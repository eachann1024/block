//go:build !windows

package installer

import (
	"errors"
	"path/filepath"
)

// Setup only ships on Windows; elsewhere the window exists for design preview
// (BLOCK_SETUP_PREVIEW), which never reaches the real install steps.

func defaultInstallDir() string { return `C:\Users\You\AppData\Local\Programs\Block` }

func validInstallDir(dir string) bool { return filepath.IsAbs(dir) }

func setupNeedsElevation(string, bool) bool { return false }

func shouldInstallMachineScope(string, bool) bool { return false }

func validateMachineInstallDir(string) error { return nil }

func runSilentSetup(string, string, bool) error {
	return errors.New("setup is only available on Windows")
}

func launchInstalledApp(string) error { return nil }
