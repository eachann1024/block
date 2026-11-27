//go:build unix

package files

import (
	"errors"
	"os"
	"syscall"
)

func statDirDevIno(_ string, info os.FileInfo) (uint64, uint64, bool) {
	if info == nil {
		return 0, 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), true
}

func isCrossDeviceErr(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}
