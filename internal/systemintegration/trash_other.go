//go:build !darwin && !linux

package systemintegration

import "os"

func restoreTrashFile(root *os.Root, staged, name string) error {
	if err := root.Link(staged, name); err != nil {
		return err
	}
	return root.Remove(staged)
}
