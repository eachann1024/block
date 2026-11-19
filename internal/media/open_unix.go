//go:build unix

package media

import (
	"os"
	"syscall"
)

func openLocalFile(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
