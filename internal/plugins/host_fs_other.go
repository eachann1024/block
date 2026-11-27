//go:build !darwin && !linux

package plugins

import "os"

func fsSwapFilesInDir(_ *os.Root, _, _ string) (bool, error) {
	return false, nil
}

func isPlatformICloudPlaceholder(_ os.FileInfo) bool {
	return false
}
