package systemintegration

import (
	"errors"
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
	err = unix.Renameat2(fd, staged, fd, name, unix.RENAME_NOREPLACE)
	if !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EOPNOTSUPP) {
		return err
	}
	if err := root.Link(staged, name); err != nil {
		return err
	}
	return root.Remove(staged)
}
