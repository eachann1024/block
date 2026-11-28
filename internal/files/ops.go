package files

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo"
)

const (
	maxNoteReadBytes = 5 * 1024 * 1024
	seqNoteLimit     = 1000
	seqFolderLimit   = 100
)

var opsSeq atomic.Uint64

// CreateNewNote exclusively creates a new Markdown note in dir using
// "Untitled.md"/"Untitled 2.md" (when isEn) or "未命名.md"/"未命名 2.md".
func CreateNewNote(dir string, isEn bool) (string, error) {
	base := "未命名"
	if isEn {
		base = "Untitled"
	}
	stamp := time.Now().UnixNano()
	for attempt := range seqNoteLimit + 16 {
		var name string
		switch {
		case attempt == 0:
			name = base + ".md"
		case attempt < seqNoteLimit:
			name = fmt.Sprintf("%s %d.md", base, attempt+1)
		default:
			name = fmt.Sprintf("%s %x-%x.md", base, stamp, opsSeq.Add(1))
		}
		candidate := filepath.Join(dir, name)
		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_ = f.Close()
			return candidate, nil
		}
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("%w: %s", os.ErrExist, theme.Tr("无法分配唯一笔记名称，请重试", "Could not allocate a unique note name; please retry"))
}

// CreateNewFolder exclusively creates a new folder in dir using
// "New Folder"/"New Folder 2" (when isEn) or "新建文件夹"/"新建文件夹 2".
func CreateNewFolder(dir string, isEn bool) (string, error) {
	base := "新建文件夹"
	if isEn {
		base = "New Folder"
	}
	for attempt := range seqFolderLimit {
		name := base
		if attempt > 0 {
			name = fmt.Sprintf("%s %d", base, attempt+1)
		}
		candidate := filepath.Join(dir, name)
		err := os.Mkdir(candidate, 0o755)
		if err == nil {
			return candidate, nil
		}
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("%w: %s", os.ErrExist, theme.Tr("无法分配唯一文件夹名称", "Could not allocate a unique folder name"))
}

func isReservedName(name string) bool {
	stem := name
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) {
		return stem[3] >= '1' && stem[3] <= '9'
	}
	return false
}

func validateItemName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New(theme.Tr("名称不能为空", "Name cannot be empty"))
	}
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return "", errors.New(theme.Tr("名称不能包含 / \\ : * ? \" < > |，不能以 . 开头，也不能以点或空格结尾", "Name cannot contain / \\ : * ? \" < > |, start with ., or end with a dot or space"))
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) {
			return "", errors.New(theme.Tr("名称不能包含 / \\ : * ? \" < > |，不能以 . 开头，也不能以点或空格结尾", "Name cannot contain / \\ : * ? \" < > |, start with ., or end with a dot or space"))
		}
	}
	if isReservedName(name) {
		return "", errors.New(theme.Tr("该名称是系统保留名称", "This name is reserved by the system"))
	}
	return name, nil
}

// RenameItem validates newName and renames oldPath on disk, preserving the
// hidden ".md" extension for canonical Markdown notes and updating history and
// file_order.
func RenameItem(oldPath, newName string) (string, error) {
	info, err := os.Lstat(oldPath)
	if err != nil {
		return "", err
	}
	isDir := info.IsDir()

	name, err := validateItemName(newName)
	if err != nil {
		return "", err
	}

	base := filepath.Base(oldPath)
	fileName := name
	if !isDir && HasHiddenExtension(base) {
		if HasHiddenExtension(name) {
			fileName = name
		} else {
			fileName = name + filepath.Ext(base)
		}
	}

	target := filepath.Join(filepath.Dir(oldPath), fileName)
	if target == oldPath {
		return target, nil
	}

	if existing, err := os.Lstat(target); err == nil {
		if !strings.EqualFold(oldPath, target) || !os.SameFile(info, existing) {
			return "", fmt.Errorf("%w: %s", os.ErrExist, theme.Tr("已存在同名项目", "An item with this name already exists"))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := os.Rename(oldPath, target); err != nil {
		return "", err
	}
	if err := relocateHistory(oldPath, target, isDir); err != nil {
		LogWarnf("history relocate failed %s -> %s: %v", oldPath, target, err)
	}
	rebaseFileOrder(oldPath, target)
	return target, nil
}

// MoveItem moves srcPath into dstDir, rejecting moves into itself or collisions.
func MoveItem(srcPath, dstDir string) (string, error) {
	src := filepath.Clean(srcPath)
	dst := filepath.Clean(dstDir)

	info, err := os.Lstat(src)
	if err != nil {
		return "", err
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return "", err
	}
	if !dstInfo.IsDir() {
		return "", errors.New("destination is not a directory")
	}

	if dst == src || strings.HasPrefix(dst, src+string(filepath.Separator)) {
		return "", errors.New(theme.Tr("不能移动到自身内部", "Cannot move into itself"))
	}

	target := filepath.Join(dst, filepath.Base(src))
	if target == src {
		return target, nil
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("%w: %s", os.ErrExist, theme.Tr("目标位置已存在同名项目", "Destination already has an item with this name"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	isDir := info.IsDir()
	if err := os.Rename(src, target); err != nil {
		return "", err
	}
	if err := relocateHistory(src, target, isDir); err != nil {
		LogWarnf("history relocate failed %s -> %s: %v", src, target, err)
	}
	rebaseFileOrder(src, target)
	return target, nil
}

func rebasePath(p, from, to string) (string, bool) {
	cleanP := filepath.Clean(p)
	cleanFrom := filepath.Clean(from)
	cleanTo := filepath.Clean(to)
	if cleanP == cleanFrom {
		return cleanTo, true
	}
	prefix := cleanFrom + string(filepath.Separator)
	if strings.HasPrefix(cleanP, prefix) {
		return filepath.Join(cleanTo, strings.TrimPrefix(cleanP, prefix)), true
	}
	return p, false
}

// RebaseFileOrder updates persisted custom file_order entries after a file or
// directory moves from from to to.
func RebaseFileOrder(from, to string) {
	rebaseFileOrder(from, to)
}

func rebaseFileOrder(from, to string) {
	stateMu.Lock()
	defer stateMu.Unlock()

	path := stateFilePath()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return
	}
	st, err := loadStateLocked(path)
	if err != nil || len(st.FileOrder) == 0 {
		return
	}

	cleanFrom := filepath.Clean(from)
	cleanTo := filepath.Clean(to)
	sameParent := filepath.Dir(cleanFrom) == filepath.Dir(cleanTo)
	moved := make(map[string][]string, len(st.FileOrder))
	changed := false

	for parent, children := range st.FileOrder {
		cleanParent := filepath.Clean(parent)
		newParent := parent
		if rebased, ok := rebasePath(cleanParent, cleanFrom, cleanTo); ok {
			newParent = rebased
			changed = true
		}
		nextChildren := make([]string, 0, len(children))
		for _, child := range children {
			if child == "" {
				continue
			}
			relChild := !filepath.IsAbs(child)
			fullChild := child
			if relChild {
				fullChild = filepath.Join(cleanParent, child)
			} else {
				fullChild = filepath.Clean(child)
			}
			if !sameParent && fullChild == cleanFrom {
				changed = true
				continue
			}
			if rebased, ok := rebasePath(fullChild, cleanFrom, cleanTo); ok {
				nextItem := rebased
				if relChild {
					if rel, relErr := filepath.Rel(filepath.Clean(newParent), rebased); relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
						nextItem = rel
					} else {
						nextItem = filepath.Base(rebased)
					}
				}
				if nextItem != child {
					changed = true
				}
				nextChildren = append(nextChildren, nextItem)
			} else {
				nextChildren = append(nextChildren, child)
			}
		}
		moved[newParent] = nextChildren
	}

	if !sameParent {
		dstParent := filepath.Dir(cleanTo)
		for key, children := range moved {
			if filepath.Clean(key) != dstParent {
				continue
			}
			filtered := make([]string, 0, len(children)+1)
			for _, child := range children {
				fullChild := child
				if !filepath.IsAbs(fullChild) {
					fullChild = filepath.Join(dstParent, child)
				}
				if filepath.Clean(fullChild) != cleanTo {
					filtered = append(filtered, child)
				}
			}
			filtered = append(filtered, filepath.Base(cleanTo))
			moved[key] = filtered
			changed = true
		}
	}

	if !changed {
		return
	}
	st.FileOrder = moved
	if err := writeStateAtomicLocked(path, &st); err != nil {
		LogWarnf("could not update file order after moving %s: %v", from, err)
	}
}

// TrashItem moves path to the system Trash via mygo.Shell.TrashItem when the
// desktop app loop is running, or moves it to the per-user fallback Trash.
func TrashItem(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	if mygo.App.IsReady() {
		err := mygo.Shell.TrashItem(path)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errors.ErrUnsupported) {
			return err
		}
	}
	return trashFallback(path)
}

func trashFallback(path string) error {
	trashDir := filepath.Join(AppDataDir(), "Trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return err
	}
	recoveryDir, err := os.MkdirTemp(trashDir, "item-")
	if err != nil {
		return err
	}
	if err := os.Rename(path, filepath.Join(recoveryDir, filepath.Base(path))); err != nil {
		_ = os.Remove(recoveryDir)
		return err
	}
	return nil
}

// TrashVerifiedItemAt moves name inside an already-verified parent dir handle
// to the system Trash (or per-user fallback Trash via handle-based rename),
// checking target identity against expectedInfo without re-traversing paths.
func TrashVerifiedItemAt(dir *os.Root, name string, expectedInfo os.FileInfo) error {
	if dir == nil || name == "" || name == "." || name == ".." {
		return os.ErrInvalid
	}
	linfo, err := dir.Lstat(name)
	if err != nil {
		return err
	}
	if isSymlinkOrReparseInfo(linfo) || !linfo.Mode().IsRegular() {
		return errors.New("refusing to trash non-regular file or symlink")
	}
	if expectedInfo != nil && !os.SameFile(expectedInfo, linfo) {
		return os.ErrInvalid
	}
	if mygo.App.IsReady() {
		liveDir := liveRootDirPath(dir)
		if liveDir == "" || IsTrashPath(liveDir) {
			return errors.New("invalid live notebook directory for trash")
		}
		target := filepath.Join(liveDir, name)
		cur, statErr := os.Lstat(target)
		if statErr != nil {
			return statErr
		}
		if isSymlinkOrReparseInfo(cur) || !cur.Mode().IsRegular() || !os.SameFile(linfo, cur) {
			return errors.New("target changed before moving to trash")
		}
		// OS Trash API requires a filesystem path; sub-ms swap after Lstat is outside threat model (不在威胁模型内).
		err := mygo.Shell.TrashItem(target)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errors.ErrUnsupported) {
			return err
		}
	}
	return trashFallbackAt(dir, name, linfo)
}

func trashFallbackAt(dir *os.Root, name string, expectedInfo os.FileInfo) error {
	trashDir := filepath.Join(AppDataDir(), "Trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return err
	}
	recoveryDir, err := os.MkdirTemp(trashDir, "item-")
	if err != nil {
		return err
	}
	dstRoot, err := os.OpenRoot(recoveryDir)
	if err != nil {
		_ = os.Remove(recoveryDir)
		return err
	}
	cur, statErr := dir.Lstat(name)
	if statErr != nil || isSymlinkOrReparseInfo(cur) || !cur.Mode().IsRegular() ||
		(expectedInfo != nil && !os.SameFile(expectedInfo, cur)) {
		_ = dstRoot.Close()
		_ = os.Remove(recoveryDir)
		if statErr != nil {
			return statErr
		}
		return os.ErrInvalid
	}
	renameErr := renameFileAcrossRoots(dir, name, dstRoot, name)
	_ = dstRoot.Close()
	if renameErr != nil {
		_ = os.Remove(recoveryDir)
		return renameErr
	}
	return nil
}

func fingerprintBytes(data []byte) int64 {
	h := fnv.New64a()
	_, _ = h.Write(data)
	return int64(h.Sum64())
}

// TextFingerprint identifies the exact UTF-8 bytes of a file, including its BOM.
func TextFingerprint(text string, bom bool) int64 {
	return SourceTextFingerprint(text, bom, model.TextEncodingUTF8)
}

// SourceTextFingerprint identifies the exact disk bytes of a source file
// including its BOM and UTF-8 / UTF-16 encoding.
func SourceTextFingerprint(text string, bom bool, enc model.TextEncoding) int64 {
	return fingerprintBytes(EncodeSourceText(text, bom, enc))
}

// CheckTextVersion detects an external modification before replacing a file.
func CheckTextVersion(path string, expected int64) error {
	before, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() || before.Size() > TextLimit {
		return errors.New(theme.Tr("文件类型或大小已变化，无法覆盖", "File type or size has changed"))
	}
	f, err := os.OpenFile(path, os.O_RDONLY|oNonBlockFlag, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > TextLimit || !os.SameFile(before, info) {
		return errors.New(theme.Tr("文件类型或大小已变化，无法覆盖", "File type or size has changed"))
	}
	hash := fnv.New64a()
	n, err := io.Copy(hash, io.LimitReader(f, TextLimit+1))
	if err != nil {
		return err
	}
	if n > TextLimit || int64(hash.Sum64()) != expected {
		return errors.New(theme.Tr("磁盘上的文件已修改，未覆盖", "The file changed on disk and was not overwritten"))
	}
	return nil
}

// ReadNote reads a UTF-8 note file up to 5 MiB and returns its content and
// deterministic 64-bit fingerprint.
func ReadNote(path string) (string, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, errors.New(theme.Tr("此文件类型暂不支持", "This file type is not supported"))
	}
	if info.Size() > maxNoteReadBytes {
		return "", 0, errors.New(theme.Tr("文件超过 5 MB", "File is larger than 5 MB"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	if int64(len(data)) > maxNoteReadBytes {
		return "", 0, errors.New(theme.Tr("文件超过 5 MB", "File is larger than 5 MB"))
	}
	if !utf8.Valid(data) {
		return "", 0, errors.New(theme.Tr("不是有效的 UTF-8", "Not valid UTF-8"))
	}
	return string(data), fingerprintBytes(data), nil
}

// WriteNoteAtomic atomically writes content to path via a temp file in the same
// directory and returns the new content fingerprint.
func WriteNoteAtomic(path, content string) (int64, error) {
	return WriteRawFileAtomic(path, content, false)
}

// WriteRawFileAtomic atomically writes text to path via a temp file in the same
// directory, prepending a UTF-8 BOM (0xEF 0xBB 0xBF) when bom is true, and
// returns the written byte fingerprint.
func WriteRawFileAtomic(path, text string, bom bool) (int64, error) {
	fp, _, err := WriteSourceFileAtomic(path, text, bom, model.TextEncodingUTF8)
	return fp, err
}

// WriteSourceFileAtomic atomically writes text to path preserving its original
// BOM and text encoding (UTF-8, UTF-16 LE, or UTF-16 BE), returning the new
// fingerprint and exact written byte size. It rejects writes where either the
// encoded on-disk size or the decoded UTF-8 size exceeds TextLimit so saving
// multi-byte encodings (such as UTF-16 with ASCII or CJK text) never writes a
// file that cannot be reopened.
func WriteSourceFileAtomic(path, text string, bom bool, enc model.TextEncoding) (int64, int64, error) {
	decodedSize := int64(len(text))
	if ExceedsTextSizeLimit(EncodedSourceByteLen(text, bom, enc), decodedSize) {
		return 0, 0, fmt.Errorf("%s", theme.Tr(
			"文件超过 5 MB 大小上限，未保存",
			"File exceeds 5 MB size limit; changes were not saved",
		))
	}
	data := EncodeSourceText(text, bom, enc)
	if ExceedsTextSizeLimit(int64(len(data)), decodedSize) {
		return 0, 0, fmt.Errorf("%s", theme.Tr(
			"文件超过 5 MB 大小上限，未保存",
			"File exceeds 5 MB size limit; changes were not saved",
		))
	}
	fp, err := WriteBytesAtomic(path, data)
	if err != nil {
		return 0, 0, err
	}
	return fp, int64(len(data)), nil
}

// WriteBytesAtomic atomically writes raw bytes to path via a temp file in the
// same directory, preserving existing file permissions, and returns the written
// byte fingerprint.
func WriteBytesAtomic(path string, data []byte) (int64, error) {
	if err := writeBytesAtomic(path, data, true); err != nil {
		return 0, err
	}
	return fingerprintBytes(data), nil
}

func writeBytesAtomic(path string, data []byte, preservePerms bool) error {
	if lstat, err := os.Lstat(path); err == nil && lstat.Mode()&os.ModeSymlink != 0 {
		if realPath, evalErr := filepath.EvalSymlinks(path); evalErr == nil && realPath != "" {
			path = realPath
		}
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	dir, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return writeBytesAtomicIn(dir, filepath.Base(path), data, preservePerms)
}

// WriteNoteAt atomically writes a note within an open notebook root.
func WriteNoteAt(root *os.Root, name, content string) error {
	parent := filepath.Dir(name)
	if err := root.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	dir, err := root.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return writeBytesAtomicIn(dir, filepath.Base(name), []byte(content), true)
}

func writeBytesAtomicIn(dir *os.Root, name string, data []byte, preservePerms bool) error {
	perm := os.FileMode(0o600)
	if preservePerms {
		perm = 0o644
		if info, err := dir.Stat(name); err == nil {
			perm = info.Mode().Perm()
		}
	}

	var (
		tmpPath  string
		tmpFile  *os.File
		leaseTmp *activeTempLease
	)
	pid := os.Getpid()
	for range 100 {
		seq := opsSeq.Add(1)
		candidate := fmt.Sprintf(".block-%d-%d.tmp", pid, seq)
		candLease := acquireActiveConditionalTemp(dir, candidate)
		f, openErr := dir.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if openErr == nil {
			tmpPath = candidate
			tmpFile = f
			leaseTmp = candLease
			break
		}
		candLease.release()
		if !errors.Is(openErr, os.ErrExist) {
			return openErr
		}
	}
	if tmpFile == nil {
		return errors.New(theme.Tr("无法创建临时文件", "Cannot allocate temporary file"))
	}

	commitErr := func() error {
		if _, err := tmpFile.Write(data); err != nil {
			_ = tmpFile.Close()
			return err
		}
		if err := tmpFile.Sync(); err != nil {
			_ = tmpFile.Close()
			return err
		}
		if err := tmpFile.Close(); err != nil {
			return err
		}
		if err := dir.Rename(tmpPath, name); err != nil {
			return err
		}
		if file, err := dir.Open("."); err == nil {
			_ = file.Sync()
			_ = file.Close()
		}
		return nil
	}()
	if commitErr != nil {
		_ = dir.Remove(tmpPath)
		leaseTmp.release()
		return commitErr
	}
	leaseTmp.release()
	return nil
}

type recoveryDraft struct {
	Path        string `json:"path"`
	Source      string `json:"source"`
	DiskVersion *int64 `json:"disk_version,omitempty"`
}

func recoveryDraftPath(notePath string) (string, string) {
	abs, err := normalizeAbsPath(notePath)
	if err != nil {
		abs = filepath.Clean(notePath)
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(abs))
	name := fmt.Sprintf("%016x.json", h.Sum64())
	return filepath.Join(AppDataDir(), "Recovery", name), abs
}

// SaveRecovery persists an atomic crash-recovery draft for notePath.
func SaveRecovery(notePath, content string) error {
	_, version, err := ReadNote(notePath)
	if err == nil {
		return SaveRecoveryForVersion(notePath, content, version)
	}
	return saveRecovery(notePath, content, nil)
}

func SaveRecoveryForVersion(notePath, content string, version int64) error {
	return saveRecovery(notePath, content, &version)
}

func saveRecovery(notePath, content string, version *int64) error {
	target, abs := recoveryDraftPath(notePath)
	draft := recoveryDraft{Path: abs, Source: content, DiskVersion: version}
	data, err := marshalJSON(draft, false)
	if err != nil {
		return err
	}
	return writeBytesAtomic(target, data, false)
}

// ClearRecovery removes any saved crash-recovery draft for notePath.
func ClearRecovery(notePath string) {
	target, _ := recoveryDraftPath(notePath)
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		LogWarnf("clear recovery %s failed: %v", target, err)
	}
}

// LatestRecoveryDraft scans AppDataDir()/Recovery and returns the newest
// valid crash-recovery draft by modification time.
func LatestRecoveryDraft() (notePath string, content string, ok bool) {
	path, source, _, ok := LatestVersionedRecoveryDraft()
	return path, source, ok
}

func LatestVersionedRecoveryDraft() (notePath string, content string, version *int64, ok bool) {
	recoveryDir := filepath.Join(AppDataDir(), "Recovery")
	entries, err := os.ReadDir(recoveryDir)
	if err != nil {
		return "", "", nil, false
	}

	type candidate struct {
		path    string
		name    string
		modTime time.Time
	}
	var candidates []candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		candidates = append(candidates, candidate{
			path:    filepath.Join(recoveryDir, entry.Name()),
			name:    entry.Name(),
			modTime: info.ModTime(),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].modTime.Equal(candidates[j].modTime) {
			return candidates[i].modTime.After(candidates[j].modTime)
		}
		return candidates[i].name > candidates[j].name
	})

	for _, c := range candidates {
		data, tooLarge, readErr := readBounded(c.path, 8*TextLimit)
		if readErr != nil || tooLarge {
			continue
		}
		var draft recoveryDraft
		if json.Unmarshal(data, &draft) == nil && filepath.IsAbs(draft.Path) {
			expected, _ := recoveryDraftPath(draft.Path)
			if c.path == expected {
				return draft.Path, draft.Source, draft.DiskVersion, true
			}
		}
	}
	return "", "", nil, false
}

func normalizeAbsPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func marshalJSON(v any, pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(v, "", "  ")
	}
	return json.Marshal(v)
}

// DownloadsDir returns the user's Downloads directory, falling back to fallback when unavailable.
func DownloadsDir(fallback string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, "Downloads")
	}
	return fallback
}

// SuffixedFilename inserts " <n>" before the file extension when n >= 2.
func SuffixedFilename(name string, n int) string {
	if n < 2 {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s %d%s", base, n, ext)
}

// ReserveUniqueFile atomically creates an empty placeholder at p, or at p with
// " 2", " 3", ... before the extension when taken, and returns the reserved path.
// Callers replace it (e.g. via atomic rename) and remove it on failure.
func ReserveUniqueFile(p string) (string, error) {
	dir := filepath.Dir(p)
	base := filepath.Base(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for i := 1; i < 10000; i++ {
		cand := filepath.Join(dir, SuffixedFilename(base, i))
		f, err := os.OpenFile(cand, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_ = f.Close()
		return cand, nil
	}
	return "", errors.New("could not allocate unique filename")
}
