//go:build !unix && !windows

package files

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func supportsNotebookXattr() bool                              { return false }
func statDirDevIno(string, os.FileInfo) (uint64, uint64, bool) { return 0, 0, false }
func isCrossDeviceErr(err error) bool                          { return errors.Is(err, syscall.EXDEV) }
func isStrictNotExistErr(err error) bool                       { return errors.Is(err, os.ErrNotExist) }
func isSymlinkOrReparseInfo(info os.FileInfo) bool {
	return info != nil && info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}
func liveRootDirPath(dir *os.Root) string {
	if dir == nil {
		return ""
	}
	return dir.Name()
}
func renameFileAcrossRoots(srcRoot *os.Root, srcName string, dstRoot *os.Root, dstName string) error {
	if srcRoot == nil || dstRoot == nil || srcName == "" || dstName == "" {
		return os.ErrInvalid
	}
	return os.Rename(filepath.Join(srcRoot.Name(), srcName), filepath.Join(dstRoot.Name(), dstName))
}
func isParentVolumeAccessible(_ string, pDev uint64, hasPDev bool, recDev uint64, _ string) bool {
	if recDev != 0 && hasPDev && pDev != recDev {
		return false
	}
	return true
}
func findSiblingByNotebookXattr(string, string) (string, bool) { return "", false }
func readNotebookXattr(string) (string, error)                 { return "", nil }
func writeNotebookXattr(string, string) error                  { return nil }
func clearNotebookXattr(string)                                {}
