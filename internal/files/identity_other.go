//go:build !unix && !windows

package files

import "os"

func FileIdentityToken(_ os.FileInfo, _ *os.File) string {
	return ""
}
