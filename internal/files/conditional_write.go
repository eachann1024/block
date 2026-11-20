package files

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"block/internal/theme"
)

func conditionalSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func readAllInDir(dir *os.Root, name string) ([]byte, error) {
	f, err := dir.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func preserveExternalConflictInDir(dir *os.Root, tmpPath, baseName, hashHex string) {
	ext := filepath.Ext(baseName)
	stem := strings.TrimSuffix(baseName, ext)
	if ext == "" {
		ext = ".md"
	}
	short := hashHex
	if len(short) > 8 {
		short = short[:8]
	}
	if short == "" {
		short = "unknown"
	}
	for i := range 100 {
		suffix := ""
		if i > 0 {
			suffix = fmt.Sprintf("-%d", i)
		}
		cand := fmt.Sprintf("%s.conflict-external-%s%s%s", stem, short, suffix, ext)
		if err := dir.Link(tmpPath, cand); err == nil {
			_ = dir.Remove(tmpPath)
			return
		} else if errors.Is(err, os.ErrExist) {
			continue
		} else {
			if _, stErr := dir.Lstat(cand); stErr == nil {
				continue
			}
			if renErr := dir.Rename(tmpPath, cand); renErr == nil {
				return
			}
			return
		}
	}
}

// WriteNoteConditionalAt writes content to name inside root, enforcing create-only
// (*expectedSHA256 == "") or conditional overwrite (*expectedSHA256 == prior SHA-256)
// atomically so concurrent external editor saves are never silently clobbered.
func WriteNoteConditionalAt(root *os.Root, name, content string, expectedSHA256 *string, errConflict error) (bool, error) {
	if root == nil {
		return false, errors.New(theme.Tr("无效的笔记根目录", "Invalid notebook root"))
	}
	parent := filepath.Dir(name)
	if err := root.MkdirAll(parent, 0o755); err != nil {
		return false, err
	}
	dir, err := root.OpenRoot(parent)
	if err != nil {
		return false, err
	}
	defer dir.Close()

	base := filepath.Base(name)
	data := []byte(content)
	if expectedSHA256 == nil {
		if err := writeBytesAtomicIn(dir, base, data, true); err != nil {
			return false, err
		}
		return true, nil
	}

	perm := os.FileMode(0o644)
	if info, stErr := dir.Stat(base); stErr == nil {
		perm = info.Mode().Perm()
	}

	var (
		tmpPath string
		tmpFile *os.File
	)
	pid := os.Getpid()
	for range 100 {
		seq := opsSeq.Add(1)
		candidate := fmt.Sprintf(".block-%d-%d.tmp", pid, seq)
		f, openErr := dir.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if openErr == nil {
			tmpPath = candidate
			tmpFile = f
			break
		}
		if !errors.Is(openErr, os.ErrExist) {
			return false, openErr
		}
	}
	if tmpFile == nil {
		return false, errors.New(theme.Tr("无法创建临时文件", "Cannot allocate temporary file"))
	}

	keepTmp := false
	defer func() {
		if !keepTmp && tmpPath != "" {
			_ = dir.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return false, err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return false, err
	}
	if err := tmpFile.Close(); err != nil {
		return false, err
	}

	contentHash := conditionalSHA256Hex(data)

	// Create-only: atomically link temp file to target; fails with ErrExist if created concurrently.
	if *expectedSHA256 == "" {
		linkErr := dir.Link(tmpPath, base)
		if linkErr == nil {
			if file, oErr := dir.Open("."); oErr == nil {
				_ = file.Sync()
				_ = file.Close()
			}
			return true, nil
		}
		if errors.Is(linkErr, os.ErrExist) {
			cur, rErr := readAllInDir(dir, base)
			if rErr == nil && bytes.Equal(cur, data) {
				return false, nil
			}
			return false, errConflict
		}
		if errors.Is(linkErr, syscall.ENOTSUP) || errors.Is(linkErr, syscall.EOPNOTSUPP) ||
			errors.Is(linkErr, syscall.EXDEV) || errors.Is(linkErr, syscall.EMLINK) ||
			errors.Is(linkErr, syscall.ENOSYS) {
			excl, oErr := dir.OpenFile(base, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
			if oErr != nil {
				if errors.Is(oErr, os.ErrExist) {
					cur, rErr := readAllInDir(dir, base)
					if rErr == nil && bytes.Equal(cur, data) {
						return false, nil
					}
					return false, errConflict
				}
				return false, oErr
			}
			_, wErr := excl.Write(data)
			sErr := excl.Sync()
			cErr := excl.Close()
			if wErr != nil || sErr != nil || cErr != nil {
				_ = dir.Remove(base)
				if wErr != nil {
					return false, wErr
				}
				if sErr != nil {
					return false, sErr
				}
				return false, cErr
			}
			return true, nil
		}
		return false, linkErr
	}

	// Conditional overwrite: atomically swap temp file with target when supported (macOS / Linux)
	// and verify the exact replaced inode's SHA-256.
	expected := *expectedSHA256
	swapped, swapErr := swapFilesInDir(dir, tmpPath, base)
	if swapped {
		if swapErr != nil {
			if errors.Is(swapErr, os.ErrNotExist) {
				return false, errConflict
			}
			return false, swapErr
		}
		oldBytes, rErr := readAllInDir(dir, tmpPath)
		if rErr != nil {
			// Roll back on unreadable swapped file to avoid losing external content.
			_, _ = swapFilesInDir(dir, tmpPath, base)
			return false, rErr
		}
		oldHash := conditionalSHA256Hex(oldBytes)
		if oldHash != expected && oldHash != contentHash {
			// Concurrent external save B was at target; roll back and verify we didn't clobber a second save C.
			_, rbErr := swapFilesInDir(dir, tmpPath, base)
			if rbErr == nil {
				rbBytes, rbReadErr := readAllInDir(dir, tmpPath)
				if rbReadErr != nil || conditionalSHA256Hex(rbBytes) != contentHash {
					// Second concurrent save C landed before rollback; restore C to base and preserve the other copy.
					_, rb2Err := swapFilesInDir(dir, tmpPath, base)
					if rb2Err == nil {
						finBytes, finReadErr := readAllInDir(dir, tmpPath)
						finHash := ""
						if finReadErr == nil {
							finHash = conditionalSHA256Hex(finBytes)
						}
						if finHash != contentHash {
							preserveExternalConflictInDir(dir, tmpPath, base, finHash)
							keepTmp = true
						}
					} else {
						rbHash := ""
						if rbReadErr == nil {
							rbHash = conditionalSHA256Hex(rbBytes)
						}
						preserveExternalConflictInDir(dir, tmpPath, base, rbHash)
						keepTmp = true
					}
				}
			} else {
				preserveExternalConflictInDir(dir, tmpPath, base, oldHash)
				keepTmp = true
			}
			return false, errConflict
		}
		if file, oErr := dir.Open("."); oErr == nil {
			_ = file.Sync()
			_ = file.Close()
		}
		return true, nil
	}

	// Non-swap platform fallback: verify target right before atomic rename.
	curInfoBefore, stErr := dir.Lstat(base)
	if stErr != nil {
		if errors.Is(stErr, os.ErrNotExist) {
			return false, errConflict
		}
		return false, stErr
	}
	curBytes, rErr := readAllInDir(dir, base)
	if rErr != nil {
		if errors.Is(rErr, os.ErrNotExist) {
			return false, errConflict
		}
		return false, rErr
	}
	curHash := conditionalSHA256Hex(curBytes)
	if curHash != expected && curHash != contentHash {
		return false, errConflict
	}
	curInfoAfter, stErr2 := dir.Lstat(base)
	if stErr2 != nil || !os.SameFile(curInfoBefore, curInfoAfter) || curInfoBefore.Size() != curInfoAfter.Size() || !curInfoBefore.ModTime().Equal(curInfoAfter.ModTime()) {
		return false, errConflict
	}
	if err := dir.Rename(tmpPath, base); err != nil {
		return false, err
	}
	keepTmp = true
	if file, oErr := dir.Open("."); oErr == nil {
		_ = file.Sync()
		_ = file.Close()
	}
	return true, nil
}
