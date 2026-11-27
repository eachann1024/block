//go:build !unix && !windows

package files

import (
	"errors"
	"os"
	"syscall"
)

func statDirDevIno(string, os.FileInfo) (uint64, uint64, bool) { return 0, 0, false }
func isCrossDeviceErr(err error) bool                          { return errors.Is(err, syscall.EXDEV) }
func readNotebookXattr(string) string                          { return "" }
func writeNotebookXattr(string, string)                        {}
func clearNotebookXattr(string)                                {}
