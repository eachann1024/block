package systemintegration

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
)

func TrashFile(path string, expected os.FileInfo, allowed, validPath func() bool) error {
	if !allowed() || !validPath() {
		return errors.New("trash operation is no longer allowed")
	}
	parent, name := filepath.Dir(path), filepath.Base(path)
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer root.Close()
	dir := ".block-trash-" + rand.Text()
	if err := root.Mkdir(dir, 0o700); err != nil {
		return err
	}
	staged := filepath.Join(dir, name)
	backup := filepath.Join(dir, ".recovery")
	keepRecovery := false
	defer func() {
		if !keepRecovery {
			_ = root.Remove(dir)
		}
	}()
	current, err := root.Lstat(name)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(expected, current) || !allowed() || !validPath() {
		return errors.New("file changed before recycling")
	}
	if runtime.GOOS == "windows" {
		if err := root.Link(name, backup); err != nil {
			return err
		}
		defer func() {
			if !keepRecovery {
				_ = root.Remove(backup)
			}
		}()
		info, err := root.Lstat(backup)
		if err != nil || !info.Mode().IsRegular() || !os.SameFile(expected, info) {
			return errors.New("file changed before recycling")
		}
	}
	if err := root.Rename(name, staged); err != nil {
		return err
	}
	recoverFrom := staged
	restore := func(cause error) error {
		if restoreErr := root.Link(recoverFrom, name); restoreErr == nil {
			_ = root.Remove(recoverFrom)
			return cause
		}
		keepRecovery = true
		return fmt.Errorf("could not recycle or restore the original name; recover the item from %s: %w", filepath.Join(parent, recoverFrom), cause)
	}
	stagedPath := filepath.Join(parent, staged)
	info, infoErr := root.Lstat(staged)
	visible, visibleErr := os.Lstat(stagedPath)
	if infoErr != nil || visibleErr != nil || !info.Mode().IsRegular() || !visible.Mode().IsRegular() ||
		!os.SameFile(expected, info) || !os.SameFile(expected, visible) || !allowed() {
		return restore(errors.New("file changed before recycling"))
	}
	if err := mygo.Shell.TrashItem(stagedPath); err != nil {
		if runtime.GOOS == "windows" {
			if stagedInfo, statErr := root.Lstat(staged); statErr != nil || !os.SameFile(expected, stagedInfo) {
				recoverFrom = backup
			}
		}
		return restore(err)
	}
	return nil
}
