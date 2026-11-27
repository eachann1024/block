//go:build unix && !darwin

package files

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func supportsNotebookXattr() bool              { return false }
func readNotebookXattr(string) (string, error) { return "", nil }
func writeNotebookXattr(string, string) error  { return nil }
func clearNotebookXattr(string)                {}

func liveRootDirPath(dir *os.Root) string {
	if dir == nil {
		return ""
	}
	if runtime.GOOS == "linux" {
		if f, err := dir.Open("."); err == nil {
			target, rErr := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", f.Fd()))
			runtime.KeepAlive(f)
			_ = f.Close()
			if rErr == nil && target != "" && target[0] == '/' && filepath.Base(target) != "" {
				if !IsTrashPath(target) {
					return filepath.Clean(target)
				}
			}
		}
	}
	return dir.Name()
}

func isParentVolumeAccessible(parent string, pDev uint64, hasPDev bool, recDev uint64, _ string) bool {
	if recDev != 0 && hasPDev && pDev != recDev {
		return false
	}
	cleanParent := filepath.Clean(parent)
	if recDev == 0 && (cleanParent == "/mnt" || cleanParent == "/media" || cleanParent == "/Volumes") {
		return false
	}
	return true
}

func findSiblingByNotebookXattr(string, string) (string, bool, bool) { return "", false, false }
