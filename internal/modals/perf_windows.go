//go:build windows

package modals

import "os"

func statPhysicalFile(info os.FileInfo) (id fileIdentity, hasID bool, fileBytes uint64) {
	if info.Size() > 0 {
		fileBytes = uint64(info.Size())
	}
	return fileIdentity{}, false, fileBytes
}
