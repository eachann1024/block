//go:build unix

package files

import (
	"fmt"
	"os"
	"syscall"
)

// FileIdentityToken returns a stable cross-restart identity token for info (and optional open file).
func FileIdentityToken(info os.FileInfo, _ *os.File) string {
	if info == nil {
		return ""
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return ""
	}
	return fmt.Sprintf("unix:%d:%d", uint64(st.Dev), uint64(st.Ino))
}
