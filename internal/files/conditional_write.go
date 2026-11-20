package files

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

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

func sanitizeStashBase(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
		if b.Len() >= 48 {
			break
		}
	}
	if b.Len() == 0 {
		return "note"
	}
	return b.String()
}

// stashReplacedInodeInRoot moves a swapped-out old inode into <root>/.block-sync-replaced/
// (same filesystem, exact same inode) so any external editor still holding an open fd that
// flushes in-place writes after RENAME_SWAP remains recoverable on disk.
func stashReplacedInodeInRoot(root *os.Root, parent, tmpPath, relName string) bool {
	if root == nil || tmpPath == "" {
		return false
	}
	const stashDirName = ".block-sync-replaced"
	if err := root.MkdirAll(stashDirName, 0o755); err != nil {
		return false
	}
	srcRel := tmpPath
	if parent != "" && parent != "." {
		srcRel = filepath.Join(parent, tmpPath)
	}
	nowMs := time.Now().UnixMilli()
	shortHash := conditionalSHA256Hex([]byte(relName))
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}
	baseTag := fmt.Sprintf("%015d-%s-%s", nowMs, shortHash, sanitizeStashBase(filepath.Base(relName)))
	var chosenName string
	for i := range 50 {
		candName := baseTag
		if i > 0 {
			candName = fmt.Sprintf("%s-%d", baseTag, i)
		}
		dstRel := filepath.Join(stashDirName, candName)
		if _, err := root.Lstat(dstRel); err == nil {
			continue
		}
		if err := root.Rename(srcRel, dstRel); err == nil {
			chosenName = candName
			break
		}
		return false
	}
	if chosenName == "" {
		return false
	}
	// Prune oldest entries beyond 64 by stash-entry timestamp prefix, never pruning the freshly
	// stashed inode or any entry stashed within the last 60 seconds.
	if sdir, err := root.Open(stashDirName); err == nil {
		names, rErr := sdir.Readdirnames(-1)
		_ = sdir.Close()
		if rErr == nil && len(names) > 64 {
			sort.Strings(names)
			cutoffMs := nowMs - 60000
			for _, oldName := range names[:len(names)-64] {
				if oldName == chosenName {
					continue
				}
				if tsStr, _, ok := strings.Cut(oldName, "-"); ok {
					if tsVal, pErr := strconv.ParseInt(tsStr, 10, 64); pErr == nil && tsVal >= cutoffMs {
						continue
					}
				}
				_ = root.Remove(filepath.Join(stashDirName, oldName))
			}
		}
	}
	return true
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

func sameInodeInDir(dir *os.Root, path string, want os.FileInfo) bool {
	if want == nil || path == "" {
		return false
	}
	cur, err := dir.Lstat(path)
	if err != nil {
		return false
	}
	return os.SameFile(want, cur)
}

// WriteNoteConditionalAt writes content to name inside root, enforcing create-only
// (*expectedSHA256 == "") or conditional overwrite (*expectedSHA256 == prior SHA-256)
// atomically so concurrent external editor saves are never silently clobbered.
func WriteNoteConditionalAt(root *os.Root, name, content string, expectedSHA256 *string, errConflict error) (bool, error) {
	if root == nil {
		return false, errors.New(theme.Tr("无效的笔记根目录", "Invalid notebook root"))
	}
	parent := filepath.Dir(name)
	requireExisting := expectedSHA256 != nil && *expectedSHA256 != ""
	if !requireExisting {
		if err := root.MkdirAll(parent, 0o755); err != nil {
			return false, err
		}
	}
	dir, err := root.OpenRoot(parent)
	if err != nil {
		if requireExisting && errors.Is(err, os.ErrNotExist) {
			return false, errConflict
		}
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
	if requireExisting {
		info, stErr := dir.Lstat(base)
		if stErr != nil {
			if errors.Is(stErr, os.ErrNotExist) {
				return false, errConflict
			}
			return false, stErr
		}
		if !info.Mode().IsRegular() {
			return false, errConflict
		}
		perm = info.Mode().Perm()
		if cur, rErr := readAllInDir(dir, base); rErr == nil {
			if bytes.Equal(cur, data) {
				return false, nil
			}
			if conditionalSHA256Hex(cur) != *expectedSHA256 {
				return false, errConflict
			}
		}
	} else if info, stErr := dir.Stat(base); stErr == nil {
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

	var ourTmpInfo os.FileInfo
	keepTmp := false
	defer func() {
		if keepTmp || tmpPath == "" {
			return
		}
		// Only remove tmpPath if it still refers to our own original temp inode (never delete
		// a swapped-out external file or a second concurrent save!).
		if ourTmpInfo == nil || sameInodeInDir(dir, tmpPath, ourTmpInfo) {
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
	if st, stErr := tmpFile.Stat(); stErr == nil {
		ourTmpInfo = st
	}
	if err := tmpFile.Close(); err != nil {
		return false, err
	}
	if ourTmpInfo == nil {
		ourTmpInfo, _ = dir.Lstat(tmpPath)
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
		oldHash := ""
		if rErr == nil {
			oldHash = conditionalSHA256Hex(oldBytes)
		}
		if rErr != nil || oldHash != expected {
			// Concurrent external save B (or identical content written externally, or non-regular object)
			// was at target. Roll back and verify via inode identity (ourTmpInfo) that a second save C
			// is never deleted. When oldHash == contentHash, return (false, nil) with no stash side effects.
			sameContent := rErr == nil && oldHash == contentHash
			_, rbErr := swapFilesInDir(dir, tmpPath, base)
			if rbErr == nil {
				if !sameInodeInDir(dir, tmpPath, ourTmpInfo) {
					// Second concurrent save C landed at base before rollback; restore C to base
					// and preserve whatever came back into tmpPath.
					_, rb2Err := swapFilesInDir(dir, tmpPath, base)
					if rb2Err == nil {
						if !sameInodeInDir(dir, tmpPath, ourTmpInfo) {
							finHash := ""
							if finBytes, finReadErr := readAllInDir(dir, tmpPath); finReadErr == nil {
								finHash = conditionalSHA256Hex(finBytes)
							}
							preserveExternalConflictInDir(dir, tmpPath, base, finHash)
							keepTmp = true
						}
					} else {
						rbHash := ""
						if rbBytes, rbReadErr := readAllInDir(dir, tmpPath); rbReadErr == nil {
							rbHash = conditionalSHA256Hex(rbBytes)
						}
						preserveExternalConflictInDir(dir, tmpPath, base, rbHash)
						keepTmp = true
					}
				}
			} else if !sameContent {
				preserveExternalConflictInDir(dir, tmpPath, base, oldHash)
				keepTmp = true
			}
			if sameContent {
				return false, nil
			}
			if rErr != nil {
				return false, rErr
			}
			return false, errConflict
		}
		// Preserve the swapped-out old inode in <root>/.block-sync-replaced/ (same volume, exact inode)
		// so late in-place writes from an editor holding an open fd remain recoverable on disk.
		// If stashing fails, keep tmpPath on disk rather than unlinking the inode.
		stashReplacedInodeInRoot(root, parent, tmpPath, name)
		keepTmp = true
		if file, oErr := dir.Open("."); oErr == nil {
			_ = file.Sync()
			_ = file.Close()
		}
		return true, nil
	}

	// Non-swap platform/filesystem fallback (e.g. HFS+): preserve the existing old inode via a
	// same-volume hard link into .block-sync-replaced/ BEFORE dir.Rename replaces base, so an
	// external editor holding an open fd never sees nlink drop to 0. If the filesystem supports
	// neither atomic swap nor hard links, abort conditional overwrite rather than risking data loss.
	curInfoBefore, stErr := dir.Lstat(base)
	if stErr != nil {
		if errors.Is(stErr, os.ErrNotExist) {
			return false, errConflict
		}
		return false, stErr
	}
	if !curInfoBefore.Mode().IsRegular() {
		return false, errConflict
	}
	const stashDirName = ".block-sync-replaced"
	if mkErr := root.MkdirAll(stashDirName, 0o755); mkErr != nil {
		return false, errConflict
	}
	srcRel := base
	if parent != "" && parent != "." {
		srcRel = filepath.Join(parent, base)
	}
	nowMs := time.Now().UnixMilli()
	shortHash := conditionalSHA256Hex([]byte(name))
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}
	baseTag := fmt.Sprintf("%015d-%s-%s", nowMs, shortHash, sanitizeStashBase(base))
	var stashRel string
	for i := range 50 {
		candName := baseTag
		if i > 0 {
			candName = fmt.Sprintf("%s-%d", baseTag, i)
		}
		candRel := filepath.Join(stashDirName, candName)
		linkErr := root.Link(srcRel, candRel)
		if linkErr == nil {
			stashRel = candRel
			break
		}
		if errors.Is(linkErr, os.ErrExist) {
			continue
		}
		// Filesystem does not support hard links either; cannot safely preserve old inode.
		return false, errConflict
	}
	if stashRel == "" {
		return false, errConflict
	}
	stashFile, openErr := root.Open(stashRel)
	if openErr != nil {
		_ = root.Remove(stashRel)
		return false, errConflict
	}
	curBytes, rErr := io.ReadAll(stashFile)
	stashInfo, stStashErr := stashFile.Stat()
	_ = stashFile.Close()
	if rErr != nil || stStashErr != nil {
		_ = root.Remove(stashRel)
		return false, errConflict
	}
	curHash := conditionalSHA256Hex(curBytes)
	if curHash == contentHash {
		_ = root.Remove(stashRel)
		return false, nil
	}
	if curHash != expected {
		_ = root.Remove(stashRel)
		return false, errConflict
	}
	curInfoAfter, stErr2 := dir.Lstat(base)
	if stErr2 != nil || !os.SameFile(stashInfo, curInfoAfter) || stashInfo.Size() != curInfoAfter.Size() || !stashInfo.ModTime().Equal(curInfoAfter.ModTime()) {
		_ = root.Remove(stashRel)
		return false, errConflict
	}
	if err := dir.Rename(tmpPath, base); err != nil {
		_ = root.Remove(stashRel)
		return false, err
	}
	keepTmp = true
	// Prune old stash entries beyond 64 (never pruning the newly created stashRel or entries < 60s old).
	if sdir, oErr := root.Open(stashDirName); oErr == nil {
		names, rnErr := sdir.Readdirnames(-1)
		_ = sdir.Close()
		if rnErr == nil && len(names) > 64 {
			sort.Strings(names)
			cutoffMs := nowMs - 60000
			chosenName := filepath.Base(stashRel)
			for _, oldName := range names[:len(names)-64] {
				if oldName == chosenName {
					continue
				}
				if tsStr, _, ok := strings.Cut(oldName, "-"); ok {
					if tsVal, pErr := strconv.ParseInt(tsStr, 10, 64); pErr == nil && tsVal >= cutoffMs {
						continue
					}
				}
				_ = root.Remove(filepath.Join(stashDirName, oldName))
			}
		}
	}
	if file, oErr := dir.Open("."); oErr == nil {
		_ = file.Sync()
		_ = file.Close()
	}
	return true, nil
}
