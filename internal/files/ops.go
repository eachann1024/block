package files

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

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
// Markdown extension for Markdown notes and updating history and file_order.
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
	if !isDir && IsMarkdown(base) {
		ext := strings.TrimPrefix(filepath.Ext(base), ".")
		if ext == "" {
			ext = "md"
		}
		fileName = name + "." + ext
	}

	target := filepath.Join(filepath.Dir(oldPath), fileName)
	if target == oldPath {
		return target, nil
	}

	sameCaseInsensitive := strings.EqualFold(oldPath, target)
	if _, err := os.Lstat(target); err == nil && !sameCaseInsensitive {
		return "", fmt.Errorf("%w: %s", os.ErrExist, theme.Tr("已存在同名项目", "An item with this name already exists"))
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

	sameParent := filepath.Dir(filepath.Clean(from)) == filepath.Dir(filepath.Clean(to))
	moved := make(map[string][]string, len(st.FileOrder))
	changed := false

	for parent, children := range st.FileOrder {
		newParent := parent
		if rebased, ok := rebasePath(parent, from, to); ok {
			newParent = rebased
			changed = true
		}
		nextChildren := make([]string, 0, len(children))
		for _, child := range children {
			if !sameParent && filepath.Clean(child) == filepath.Clean(from) {
				changed = true
				continue
			}
			if rebased, ok := rebasePath(child, from, to); ok {
				nextChildren = append(nextChildren, rebased)
				changed = true
			} else {
				nextChildren = append(nextChildren, child)
			}
		}
		moved[newParent] = nextChildren
	}

	if !sameParent {
		dstParent := filepath.Dir(filepath.Clean(to))
		if children, ok := moved[dstParent]; ok {
			filtered := make([]string, 0, len(children)+1)
			for _, child := range children {
				if filepath.Clean(child) != filepath.Clean(to) {
					filtered = append(filtered, child)
				}
			}
			filtered = append(filtered, filepath.Clean(to))
			moved[dstParent] = filtered
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
// desktop app loop is running, falling back to safe removal in headless/test
// environments.
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
	if hasDataDirOverride() {
		trashDir := filepath.Join(AppDataDir(), "Trash")
		if err := os.MkdirAll(trashDir, 0o755); err == nil {
			base := filepath.Base(path)
			target := filepath.Join(trashDir, base)
			if _, statErr := os.Lstat(target); statErr == nil {
				target = filepath.Join(trashDir, fmt.Sprintf("%s-%x-%x", base, time.Now().UnixNano(), opsSeq.Add(1)))
			}
			if renameErr := os.Rename(path, target); renameErr == nil {
				return nil
			}
		}
	}
	return os.RemoveAll(path)
}

func fingerprintBytes(data []byte) int64 {
	h := fnv.New64a()
	_, _ = h.Write(data)
	return int64(h.Sum64())
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
	var data []byte
	if bom {
		data = make([]byte, len(utf8BOM)+len(text))
		copy(data, utf8BOM)
		copy(data[len(utf8BOM):], text)
	} else {
		data = []byte(text)
	}
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
	if parent == "" {
		return errors.New(theme.Tr("缺少父级目录", "Missing parent"))
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	perm := os.FileMode(0o644)
	if preservePerms {
		if info, err := os.Stat(path); err == nil {
			perm = info.Mode().Perm()
		}
	}

	var (
		tmpPath string
		tmpFile *os.File
	)
	pid := os.Getpid()
	for range 100 {
		seq := opsSeq.Add(1)
		candidate := filepath.Join(parent, fmt.Sprintf(".block-%d-%d.tmp", pid, seq))
		f, openErr := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if openErr == nil {
			tmpPath = candidate
			tmpFile = f
			break
		}
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
		if err := os.Rename(tmpPath, path); err != nil {
			return err
		}
		if dir, err := os.Open(parent); err == nil {
			_ = dir.Sync()
			_ = dir.Close()
		}
		return nil
	}()
	if commitErr != nil {
		_ = os.Remove(tmpPath)
		return commitErr
	}
	return nil
}

type recoveryDraft struct {
	Path   string `json:"path"`
	Source string `json:"source"`
	Base   int64  `json:"base"`
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
	target, abs := recoveryDraftPath(notePath)
	draft := recoveryDraft{
		Path:   abs,
		Source: content,
		Base:   fingerprintBytes([]byte(content)),
	}
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
	recoveryDir := filepath.Join(AppDataDir(), "Recovery")
	entries, err := os.ReadDir(recoveryDir)
	if err != nil {
		return "", "", false
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
		if infoErr != nil {
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
		data, readErr := os.ReadFile(c.path)
		if readErr != nil {
			continue
		}
		var draft struct {
			Path   string `json:"path"`
			Source string `json:"source"`
		}
		if json.Unmarshal(data, &draft) == nil && draft.Path != "" {
			return draft.Path, draft.Source, true
		}
	}
	return "", "", false
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
