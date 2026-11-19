package files

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"block/internal/model"
)

const (
	maxHistorySnapshots = 100
	maxMyersDiffDepth   = 2048
)

var (
	historyMu  sync.Mutex
	historySeq atomic.Uint64
)

type historyIndex struct {
	Pages map[string]string `json:"pages"`
}

func historyRootDir() string {
	return filepath.Join(AppDataDir(), "History")
}

func historyIndexPath() string {
	return filepath.Join(historyRootDir(), "index.json")
}

func safeHistoryComponent(val string) bool {
	if len(val) == 0 || len(val) > 96 {
		return false
	}
	for i := range len(val) {
		c := val[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func historyPageDir(pageID string) (string, error) {
	if !safeHistoryComponent(pageID) {
		return "", errors.New("invalid history page id")
	}
	root := filepath.Clean(historyRootDir())
	dir := filepath.Clean(filepath.Join(root, pageID))
	if dir != root && !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		return "", errors.New("history page id escapes history root")
	}
	return dir, nil
}

func uniqueHistoryToken(prefix string) string {
	seq := historySeq.Add(1)
	return fmt.Sprintf("%s_%x_%x_%x", prefix, time.Now().UnixNano(), seq, os.Getpid())
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func loadHistoryIndexLocked() (*historyIndex, error) {
	data, err := os.ReadFile(historyIndexPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &historyIndex{Pages: make(map[string]string)}, nil
		}
		return nil, err
	}
	var idx historyIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	if idx.Pages == nil {
		idx.Pages = make(map[string]string)
	}
	return &idx, nil
}

func saveHistoryIndexLocked(idx *historyIndex) error {
	bytes, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	return writeBytesAtomic(historyIndexPath(), bytes, false)
}

func allocHistoryPageIDLocked() (string, error) {
	root := historyRootDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	for range 64 {
		id := uniqueHistoryToken("p")
		if !safeHistoryComponent(id) {
			continue
		}
		dir, err := historyPageDir(id)
		if err != nil {
			return "", err
		}
		if err := os.Mkdir(dir, 0o755); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", err
		}
		marker := filepath.Join(dir, "page.id")
		f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			_ = os.RemoveAll(dir)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", err
		}
		_, writeErr := f.WriteString(id)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			_ = os.RemoveAll(dir)
			if writeErr != nil {
				return "", writeErr
			}
			if syncErr != nil {
				return "", syncErr
			}
			return "", closeErr
		}
		return id, nil
	}
	return "", errors.New("could not allocate a unique history page id")
}

func lookupHistoryKey(idx *historyIndex, notePath string) (string, string, error) {
	key, err := normalizeAbsPath(notePath)
	if err != nil {
		return "", "", err
	}
	if id, ok := idx.Pages[key]; ok {
		return key, id, nil
	}
	if notePath != key {
		if id, ok := idx.Pages[notePath]; ok {
			return notePath, id, nil
		}
	}
	return key, "", nil
}

func getOrCreateHistoryPageLocked(idx *historyIndex, notePath string) (string, string, error) {
	key, id, err := lookupHistoryKey(idx, notePath)
	if err != nil {
		return "", "", err
	}
	absKey, err := normalizeAbsPath(notePath)
	if err != nil {
		return "", "", err
	}
	if id != "" {
		if _, dirErr := historyPageDir(id); dirErr != nil {
			return "", "", dirErr
		}
		if key != absKey {
			delete(idx.Pages, key)
			idx.Pages[absKey] = id
			if saveErr := saveHistoryIndexLocked(idx); saveErr != nil {
				delete(idx.Pages, absKey)
				idx.Pages[key] = id
				return "", "", saveErr
			}
		}
		return absKey, id, nil
	}

	id, err = allocHistoryPageIDLocked()
	if err != nil {
		return "", "", err
	}
	idx.Pages[absKey] = id
	if saveErr := saveHistoryIndexLocked(idx); saveErr != nil {
		delete(idx.Pages, absKey)
		if dir, dirErr := historyPageDir(id); dirErr == nil {
			_ = os.RemoveAll(dir)
		}
		return "", "", saveErr
	}
	return absKey, id, nil
}

func loadHistoryMetasLocked(pageID string) ([]model.HistorySnapshot, error) {
	dir, err := historyPageDir(pageID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "versions.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var metas []model.HistorySnapshot
	if err := json.Unmarshal(data, &metas); err != nil {
		return nil, err
	}
	for _, m := range metas {
		if !safeHistoryComponent(m.ID) {
			return nil, errors.New("invalid snapshot id in versions.json")
		}
	}
	return metas, nil
}

func saveHistoryMetasLocked(pageID string, metas []model.HistorySnapshot) error {
	dir, err := historyPageDir(pageID)
	if err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(metas, "", "  ")
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	return writeBytesAtomic(filepath.Join(dir, "versions.json"), bytes, false)
}

func writeSnapshotFileLocked(pageID, content string) (string, error) {
	dir, err := historyPageDir(pageID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data := []byte(content)
	for range 64 {
		id := uniqueHistoryToken("v")
		if !safeHistoryComponent(id) {
			continue
		}
		path := filepath.Join(dir, id+".md")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", err
		}
		_, writeErr := f.Write(data)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			_ = os.Remove(path)
			if writeErr != nil {
				return "", writeErr
			}
			if syncErr != nil {
				return "", syncErr
			}
			return "", closeErr
		}
		return id, nil
	}
	return "", errors.New("could not allocate an immutable snapshot file")
}

func removeEmptyHistoryPageLocked(idx *historyIndex, key, pageID string) {
	if pageID != "" {
		if dir, dirErr := historyPageDir(pageID); dirErr == nil {
			_ = os.RemoveAll(dir)
		}
	}
	if idx != nil && key != "" {
		if _, exists := idx.Pages[key]; exists {
			delete(idx.Pages, key)
			_ = saveHistoryIndexLocked(idx)
		}
	}
}

// RecordHistorySnapshot saves a deduplicated snapshot for notePath.
// If the latest snapshot already has the identical SHA-256 content hash, no new
// snapshot is written and the existing latest snapshot is returned.
func RecordHistorySnapshot(notePath, content, summary string) (*model.HistorySnapshot, error) {
	historyMu.Lock()
	defer historyMu.Unlock()

	idx, err := loadHistoryIndexLocked()
	if err != nil {
		return nil, err
	}
	absKey, pageID, err := getOrCreateHistoryPageLocked(idx, notePath)
	if err != nil {
		return nil, err
	}
	metas, err := loadHistoryMetasLocked(pageID)
	if err != nil {
		return nil, err
	}

	hash := sha256Hex([]byte(content))
	if n := len(metas); n > 0 && metas[n-1].ContentHash == hash {
		last := metas[n-1]
		return &last, nil
	}

	snapID, err := writeSnapshotFileLocked(pageID, content)
	if err != nil {
		if len(metas) == 0 {
			removeEmptyHistoryPageLocked(idx, absKey, pageID)
		}
		return nil, err
	}

	snap := model.HistorySnapshot{
		ID:          snapID,
		NotePath:    absKey,
		Timestamp:   time.Now().Unix(),
		ContentHash: hash,
		Size:        len(content),
		Summary:     summary,
	}
	metas = append(metas, snap)

	var pruned []model.HistorySnapshot
	if len(metas) > maxHistorySnapshots {
		excess := len(metas) - maxHistorySnapshots
		pruned = append(pruned, metas[1:1+excess]...)
		metas = append(metas[:1], metas[1+excess:]...)
	}

	if err := saveHistoryMetasLocked(pageID, metas); err != nil {
		if dir, dirErr := historyPageDir(pageID); dirErr == nil {
			_ = os.Remove(filepath.Join(dir, snapID+".md"))
		}
		if len(metas) <= 1 {
			removeEmptyHistoryPageLocked(idx, absKey, pageID)
		}
		return nil, err
	}

	if len(pruned) > 0 {
		if dir, dirErr := historyPageDir(pageID); dirErr == nil {
			for _, old := range pruned {
				_ = os.Remove(filepath.Join(dir, old.ID+".md"))
			}
		}
	}

	return &snap, nil
}

// ListHistorySnapshots returns stored snapshots for notePath ordered newest first.
func ListHistorySnapshots(notePath string) ([]model.HistorySnapshot, error) {
	historyMu.Lock()
	defer historyMu.Unlock()

	idx, err := loadHistoryIndexLocked()
	if err != nil {
		return nil, err
	}
	key, pageID, err := lookupHistoryKey(idx, notePath)
	if err != nil {
		return nil, err
	}
	if pageID == "" {
		return nil, nil
	}
	metas, err := loadHistoryMetasLocked(pageID)
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		removeEmptyHistoryPageLocked(idx, key, pageID)
		return nil, nil
	}
	out := make([]model.HistorySnapshot, len(metas))
	for i := range metas {
		out[i] = metas[len(metas)-1-i]
	}
	return out, nil
}

// ReadHistorySnapshot loads and verifies a stored snapshot by ID.
func ReadHistorySnapshot(notePath, snapshotID string) (string, error) {
	if !safeHistoryComponent(snapshotID) {
		return "", errors.New("invalid snapshot id")
	}

	historyMu.Lock()
	defer historyMu.Unlock()

	idx, err := loadHistoryIndexLocked()
	if err != nil {
		return "", err
	}
	key, pageID, err := lookupHistoryKey(idx, notePath)
	if err != nil {
		return "", err
	}
	if pageID == "" {
		return "", errors.New("version not found")
	}
	metas, err := loadHistoryMetasLocked(pageID)
	if err != nil {
		return "", err
	}
	if len(metas) == 0 {
		removeEmptyHistoryPageLocked(idx, key, pageID)
		return "", errors.New("version not found")
	}

	var targetMeta *model.HistorySnapshot
	for i := range metas {
		if metas[i].ID == snapshotID {
			targetMeta = &metas[i]
			break
		}
	}
	if targetMeta == nil {
		return "", errors.New("version not found")
	}

	dir, err := historyPageDir(pageID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, snapshotID+".md"))
	if err != nil {
		return "", err
	}
	if sha256Hex(data) != targetMeta.ContentHash {
		return "", errors.New("snapshot checksum mismatch")
	}
	return string(data), nil
}

func relocateHistory(fromPath, toPath string, isDir bool) error {
	fromAbs, err := normalizeAbsPath(fromPath)
	if err != nil {
		return err
	}
	toAbs, err := normalizeAbsPath(toPath)
	if err != nil {
		return err
	}

	historyMu.Lock()
	defer historyMu.Unlock()

	idx, err := loadHistoryIndexLocked()
	if err != nil {
		return err
	}
	if len(idx.Pages) == 0 {
		return nil
	}

	type edit struct {
		oldKey string
		newKey string
	}
	var edits []edit
	fromPrefix := fromAbs + string(filepath.Separator)
	for key := range idx.Pages {
		cleanKey := filepath.Clean(key)
		var newKey string
		if isDir {
			if cleanKey == fromAbs {
				newKey = toAbs
			} else if strings.HasPrefix(cleanKey, fromPrefix) {
				newKey = filepath.Join(toAbs, strings.TrimPrefix(cleanKey, fromPrefix))
			} else {
				continue
			}
		} else if cleanKey == fromAbs {
			newKey = toAbs
		} else {
			continue
		}
		if newKey != key {
			edits = append(edits, edit{oldKey: key, newKey: newKey})
		}
	}
	if len(edits) == 0 {
		return nil
	}

	for _, e := range edits {
		id := idx.Pages[e.oldKey]
		delete(idx.Pages, e.oldKey)
		idx.Pages[e.newKey] = id
	}
	return saveHistoryIndexLocked(idx)
}

type diffOp uint8

const (
	diffKeep diffOp = iota
	diffDel
	diffIns
)

// DiffLines computes a Myers line-level diff between oldText and newText.
func DiffLines(oldText, newText string) []model.DiffLine {
	a := splitDiffLines(oldText)
	b := splitDiffLines(newText)
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}

	ops := myersDiffLines(a, b)
	out := make([]model.DiffLine, 0, len(ops))
	oi, ni := 0, 0
	for _, op := range ops {
		switch op {
		case diffKeep:
			if oi >= n || ni >= m {
				continue
			}
			out = append(out, model.DiffLine{
				Kind:    model.DiffEqual,
				OldLine: oi + 1,
				NewLine: ni + 1,
				Text:    a[oi],
			})
			oi++
			ni++
		case diffDel:
			if oi >= n {
				continue
			}
			out = append(out, model.DiffLine{
				Kind:    model.DiffDelete,
				OldLine: oi + 1,
				NewLine: 0,
				Text:    a[oi],
			})
			oi++
		case diffIns:
			if ni >= m {
				continue
			}
			out = append(out, model.DiffLine{
				Kind:    model.DiffInsert,
				OldLine: 0,
				NewLine: ni + 1,
				Text:    b[ni],
			})
			ni++
		}
	}
	for oi < n {
		out = append(out, model.DiffLine{
			Kind:    model.DiffDelete,
			OldLine: oi + 1,
			Text:    a[oi],
		})
		oi++
	}
	for ni < m {
		out = append(out, model.DiffLine{
			Kind:    model.DiffInsert,
			NewLine: ni + 1,
			Text:    b[ni],
		})
		ni++
	}
	return out
}

func splitDiffLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

func myersDiffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	if n == 0 {
		ops := make([]diffOp, m)
		for i := range ops {
			ops[i] = diffIns
		}
		return ops
	}
	if m == 0 {
		ops := make([]diffOp, n)
		for i := range ops {
			ops[i] = diffDel
		}
		return ops
	}

	maxD := n + m
	if maxD > maxMyersDiffDepth {
		maxD = maxMyersDiffDepth
	}
	offset := maxD + 1
	slots := 2*maxD + 3
	v := make([]int, slots)
	trace := make([][]int, 0, maxD+1)
	reachedD := -1

	for d := 0; d <= maxD; d++ {
		snap := make([]int, 2*d+1)
		copy(snap, v[offset-d:offset+d+1])
		trace = append(trace, snap)

		found := false
		for k := -d; k <= d; k += 2 {
			kIdx := k + offset
			if kIdx < 0 || kIdx >= slots {
				return replaceAllOps(n, m)
			}
			var fromBelow bool
			if k == -d {
				fromBelow = true
			} else if k == d {
				fromBelow = false
			} else {
				leftIdx, downIdx := k-1+offset, k+1+offset
				if leftIdx < 0 || downIdx >= slots {
					return replaceAllOps(n, m)
				}
				fromBelow = v[leftIdx] < v[downIdx]
			}
			var x int
			if fromBelow {
				downIdx := k + 1 + offset
				if downIdx < 0 || downIdx >= slots {
					return replaceAllOps(n, m)
				}
				x = v[downIdx]
			} else {
				leftIdx := k - 1 + offset
				if leftIdx < 0 || leftIdx >= slots {
					return replaceAllOps(n, m)
				}
				x = v[leftIdx] + 1
			}
			if x > n {
				return replaceAllOps(n, m)
			}
			y := x - k
			if y < 0 {
				return replaceAllOps(n, m)
			}
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[kIdx] = x
			if x >= n && y >= m {
				reachedD = d
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if reachedD < 0 {
		return replaceAllOps(n, m)
	}
	ops, ok := backtrackMyers(trace, reachedD, n, m)
	if !ok {
		return replaceAllOps(n, m)
	}
	return ops
}

func replaceAllOps(n, m int) []diffOp {
	ops := make([]diffOp, 0, n+m)
	for range n {
		ops = append(ops, diffDel)
	}
	for range m {
		ops = append(ops, diffIns)
	}
	return ops
}

func backtrackMyers(trace [][]int, d, n, m int) ([]diffOp, bool) {
	var ops []diffOp
	x, y := n, m

	for step := d; step >= 1; step-- {
		vPrev := trace[step]
		snapOffset := step
		k := x - y
		var fromBelow bool
		if k == -step {
			fromBelow = true
		} else if k == step {
			fromBelow = false
		} else {
			leftIdx, downIdx := k-1+snapOffset, k+1+snapOffset
			if leftIdx < 0 || downIdx >= len(vPrev) {
				return nil, false
			}
			fromBelow = vPrev[leftIdx] < vPrev[downIdx]
		}
		prevK := k - 1
		if fromBelow {
			prevK = k + 1
		}
		prevIdx := prevK + snapOffset
		if prevIdx < 0 || prevIdx >= len(vPrev) {
			return nil, false
		}
		prevX := vPrev[prevIdx]
		prevY := prevX - prevK
		if prevY < 0 || prevX > x || prevY > y {
			return nil, false
		}
		for x > prevX && y > prevY {
			ops = append(ops, diffKeep)
			x--
			y--
		}
		if x == prevX && y == prevY+1 {
			ops = append(ops, diffIns)
			y--
		} else if y == prevY && x == prevX+1 {
			ops = append(ops, diffDel)
			x--
		} else {
			return nil, false
		}
	}

	if x != y && x != 0 && y != 0 {
		return nil, false
	}
	for x > 0 && y > 0 {
		ops = append(ops, diffKeep)
		x--
		y--
	}
	for x > 0 {
		ops = append(ops, diffDel)
		x--
	}
	for y > 0 {
		ops = append(ops, diffIns)
		y--
	}

	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}
	return ops, true
}
