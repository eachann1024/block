package systemintegration

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/egoist/mygo"
)

var errTrashVolume = errors.New("trash staging is on a different volume")

type trashRecoveryRecord struct {
	Original string `json:"original"`
	Adjacent bool   `json:"adjacent,omitempty"`
}

func TrashFile(dataDir, path string, expected os.FileInfo, allowed, validPath func() bool) error {
	if !allowed() || !validPath() {
		return errors.New("trash operation is no longer allowed")
	}
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	dataDir, err = filepath.EvalSymlinks(dataDir)
	if err != nil {
		return err
	}
	const windowsNotSameDevice = syscall.Errno(17)
	err = trashFileAtStage(dataDir, path, expected, allowed, validPath, false)
	if !errors.Is(err, errTrashVolume) && !errors.Is(err, syscall.EXDEV) &&
		!(runtime.GOOS == "windows" && errors.Is(err, windowsNotSameDevice)) {
		return err
	}
	if !allowed() || !validPath() {
		return errors.New("trash operation is no longer allowed")
	}
	return trashFileAtStage(dataDir, path, expected, allowed, validPath, true)
}

func trashFileAtStage(dataDir, path string, expected os.FileInfo, allowed, validPath func() bool, adjacent bool) error {
	id := rand.Text()
	stage := trashStagingPath(dataDir, path, id, adjacent)
	anchor := filepath.Dir(path)
	var dir string
	for {
		rel, err := filepath.Rel(anchor, stage)
		if err == nil && filepath.IsLocal(rel) {
			dir = rel
			break
		}
		parent := filepath.Dir(anchor)
		if parent == anchor {
			return errTrashVolume
		}
		anchor = parent
	}
	name, err := filepath.Rel(anchor, path)
	if err != nil || !filepath.IsLocal(name) {
		return errors.New("invalid trash source")
	}
	root, err := os.OpenRoot(anchor)
	if err != nil {
		return err
	}
	defer root.Close()
	stagingBase := filepath.Dir(dir)
	if !adjacent {
		if err := root.MkdirAll(stagingBase, 0o700); err != nil {
			return err
		}
		if info, err := root.Lstat(stagingBase); err != nil || !info.IsDir() {
			return errors.New("trash staging must be a real app-data directory")
		}
	}
	if err := root.Mkdir(dir, 0o700); err != nil {
		return err
	}
	items := filepath.Join(dir, "item")
	staged := filepath.Join(items, filepath.Base(path))
	backup := filepath.Join(dir, "backup")
	journalPath := filepath.Join(dataDir, "trash-recovery", id)
	keepRecovery := false
	journalWritten := false
	recycled := false
	defer func() {
		if keepRecovery {
			return
		}
		if err := root.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
			return
		}
		if !recycled {
			_ = root.Remove(items)
			if err := root.Remove(dir); journalWritten && (err == nil || errors.Is(err, os.ErrNotExist)) {
				_ = os.Remove(journalPath)
			}
		}
	}()
	if err := root.Mkdir(items, 0o700); err != nil {
		return err
	}
	current, err := root.Lstat(name)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(expected, current) || !allowed() || !validPath() {
		return errors.New("file changed before recycling")
	}
	if err := writeTrashRecovery(journalPath, trashRecoveryRecord{Original: path, Adjacent: adjacent}); err != nil {
		return err
	}
	journalWritten = true
	if runtime.GOOS == "windows" {
		if err := root.Link(name, backup); err != nil {
			return err
		}
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
		if restoreErr := restoreTrashFile(root, recoverFrom, name); restoreErr == nil {
			return cause
		}
		keepRecovery = true
		return fmt.Errorf("could not recycle or restore the original name; recover the item from %s: %v", filepath.Join(anchor, recoverFrom), cause)
	}
	stagedPath := filepath.Join(anchor, staged)
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
		return restore(fmt.Errorf("system trash failed: %v", err))
	}
	recycled = true
	return nil
}

func trashStagingPath(dataDir, original, id string, adjacent bool) string {
	if adjacent {
		return filepath.Join(filepath.Dir(original), ".block-trash-"+id)
	}
	return filepath.Join(dataDir, "trash-staging", id)
}

func writeTrashRecovery(path string, record trashRecoveryRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	writeErr := json.NewEncoder(file).Encode(record)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

func PendingTrashRecovery(dataDir string, visit func(original, recovery string)) {
	dir := filepath.Join(dataDir, "trash-recovery")
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		id := entry.Name()
		if !entry.Type().IsRegular() || len(id) < 26 || len(id) > 64 || strings.ContainsFunc(id, func(r rune) bool {
			return (r < 'A' || r > 'Z') && (r < '2' || r > '7')
		}) {
			continue
		}
		entryFile, err := os.Open(filepath.Join(dir, id))
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(entryFile, 64*1024+1))
		_ = entryFile.Close()
		if err != nil || len(raw) > 64*1024 {
			continue
		}
		record := trashRecoveryRecord{Original: string(raw)}
		if strings.HasPrefix(record.Original, "{") && json.Unmarshal(raw, &record) != nil {
			continue
		}
		original := record.Original
		if !filepath.IsAbs(original) || filepath.Clean(original) != original {
			continue
		}
		staging := trashStagingPath(dataDir, original, id, record.Adjacent)
		staged := filepath.Join(staging, "item", filepath.Base(original))
		stagedInfo, stagedErr := os.Lstat(staged)
		if stagedErr == nil && stagedInfo.Mode().IsRegular() {
			visit(original, staged)
		}
		backup := filepath.Join(staging, "backup")
		backupInfo, err := os.Lstat(backup)
		if err != nil || !backupInfo.Mode().IsRegular() || (stagedErr == nil && os.SameFile(backupInfo, stagedInfo)) {
			continue
		}
		if info, err := os.Lstat(original); err != nil || !os.SameFile(backupInfo, info) {
			visit(original, backup)
		}
	}
}
