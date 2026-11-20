package systemintegration

import (
	"os"

	"golang.org/x/sys/unix"
)

func restoreTrashFile(root *os.Root, staged, name string) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	fd := int(dir.Fd())
	return unix.RenameatxNp(fd, staged, fd, name, unix.RENAME_EXCL)
}
