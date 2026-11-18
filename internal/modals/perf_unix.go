//go:build !windows

package modals

import (
	"os"
	"syscall"
)

func statPhysicalFile(info os.FileInfo) (id fileIdentity, hasID bool, fileBytes uint64) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		id = fileIdentity{dev: uint64(st.Dev), ino: uint64(st.Ino)}
		hasID = true
		if st.Blocks > 0 {
			fileBytes = uint64(st.Blocks) * 512
		}
		return id, hasID, fileBytes
	}
	if info.Size() > 0 {
		fileBytes = uint64(info.Size())
	}
	return fileIdentity{}, false, fileBytes
}
