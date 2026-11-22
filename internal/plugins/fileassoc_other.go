//go:build !darwin && !windows && !linux

package plugins

func syncPlatformFileAssociations(appDataDir string, pluginExts []string) error {
	return nil
}

func cleanupPlatformFileAssociations(appDataDir string, isolated bool) error {
	return nil
}
