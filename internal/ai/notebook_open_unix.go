//go:build unix

package ai

import (
	"os"
	"syscall"
)

func openNoteForRead(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
