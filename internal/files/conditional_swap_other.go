//go:build !darwin && !linux

package files

import "os"

func swapFilesInDir(_ *os.Root, _, _ string) (bool, error) {
	return false, nil
}
