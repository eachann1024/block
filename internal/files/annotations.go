package files

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type annotStoreCacheEntry struct {
	path    string
	modTime time.Time
	size    int64
	store   AnnotationStore
	valid   bool
}

var (
	annotMu             sync.Mutex
	annotTempSeq        atomic.Uint64
	annotCache          annotStoreCacheEntry
	cleanAnnotTempsOnce sync.Once
)

// FlexibleUint64 unmarshals either a JSON integer or a numeric/arbitrary string
// (so Rust u64 IDs and legacy Go string IDs both load cleanly) and marshals as
// a JSON u64 integer compatible with thinkly-fast's annotations.json.
type FlexibleUint64 uint64

func (f *FlexibleUint64) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		if v, err := strconv.ParseUint(strings.TrimSpace(str), 10, 64); err == nil {
			*f = FlexibleUint64(v)
			return nil
		}
		*f = 0
		return nil
	}
	var v uint64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*f = FlexibleUint64(v)
	return nil
}

func (f FlexibleUint64) MarshalJSON() ([]byte, error) {
	return json.Marshal(uint64(f))
}

// StoredAnnotationAnchor matches thinkly-fast's AnnotationAnchor in src/editor/annotate_spans.rs.
type StoredAnnotationAnchor struct {
	BlockIndex int    `json:"block_index"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	Quote      string `json:"quote"`
}

// StoredAnnotation matches thinkly-fast's Annotation schema in src/editor/annotate.rs.
// Start and End are UTF-8 byte offsets in the block text.
type StoredAnnotation struct {
	ID          FlexibleUint64           `json:"id"`
	BlockIndex  int                      `json:"block_index"`
	Start       int                      `json:"start"`
	End         int                      `json:"end"`
	Quote       string                   `json:"quote"`
	Body        string                   `json:"body"`
	Anchors     []StoredAnnotationAnchor `json:"anchors,omitempty"`
	Reactions   []string                 `json:"reactions"`
	AIRewritten *string                  `json:"ai_rewritten,omitempty"`
	AIError     *string                  `json:"ai_error,omitempty"`
	AIAnswer    *string                  `json:"ai_answer,omitempty"`
	AIThread    []StoredAnnotationTurn   `json:"ai_thread,omitempty"`
	Applied     *bool                    `json:"applied,omitempty"`
}

// StoredAnnotationTurn is one earlier note/reply round of a follow-up thread.
type StoredAnnotationTurn struct {
	Q    string `json:"q"`
	A    string `json:"a"`
	Kind string `json:"kind,omitempty"`
}

// AnnotationStore matches thinkly-fast's {"notes": {"<abs_path>": [...]}} format.
type AnnotationStore struct {
	Notes map[string][]StoredAnnotation `json:"notes"`
}

func annotationsFilePath(dataDir string) string {
	dir := strings.TrimSpace(dataDir)
	if dir == "" {
		dir = AppDataDir()
	}
	return filepath.Join(dir, "annotations.json")
}

func cloneAnnotationStore(src AnnotationStore) AnnotationStore {
	dst := AnnotationStore{
		Notes: make(map[string][]StoredAnnotation, len(src.Notes)),
	}
	maps.Copy(dst.Notes, src.Notes)
	return dst
}

func loadAnnotationStoreLocked(path string) (AnnotationStore, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if annotCache.valid && annotCache.path == path {
				annotCache = annotStoreCacheEntry{}
			}
			return AnnotationStore{Notes: make(map[string][]StoredAnnotation)}, nil
		}
		return AnnotationStore{Notes: make(map[string][]StoredAnnotation)}, err
	}
	if annotCache.valid && annotCache.path == path && annotCache.size == info.Size() && annotCache.modTime.Equal(info.ModTime()) {
		return cloneAnnotationStore(annotCache.store), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if annotCache.valid && annotCache.path == path {
				annotCache = annotStoreCacheEntry{}
			}
			return AnnotationStore{Notes: make(map[string][]StoredAnnotation)}, nil
		}
		return AnnotationStore{Notes: make(map[string][]StoredAnnotation)}, err
	}
	var store AnnotationStore
	if err := json.Unmarshal(data, &store); err != nil {
		if annotCache.valid && annotCache.path == path {
			annotCache = annotStoreCacheEntry{}
		}
		return AnnotationStore{Notes: make(map[string][]StoredAnnotation)}, err
	}
	if store.Notes == nil {
		store.Notes = make(map[string][]StoredAnnotation)
	}
	annotCache = annotStoreCacheEntry{
		path:    path,
		modTime: info.ModTime(),
		size:    info.Size(),
		store:   cloneAnnotationStore(store),
		valid:   true,
	}
	return store, nil
}

func findAnnotationKey(notes map[string][]StoredAnnotation, notePath string) (string, []StoredAnnotation, bool) {
	if notePath == "" || len(notes) == 0 {
		return "", nil, false
	}
	if list, ok := notes[notePath]; ok {
		return notePath, list, true
	}
	clean := filepath.Clean(notePath)
	if list, ok := notes[clean]; ok {
		return clean, list, true
	}
	for k, list := range notes {
		if filepath.Clean(k) == clean {
			return k, list, true
		}
	}
	return "", nil, false
}

// LoadNoteAnnotations reads annotations for notePath from annotations.json in dataDir (or AppDataDir).
func LoadNoteAnnotations(dataDir, notePath string) ([]StoredAnnotation, error) {
	if strings.TrimSpace(notePath) == "" {
		return nil, nil
	}
	annotMu.Lock()
	defer annotMu.Unlock()

	store, err := loadAnnotationStoreLocked(annotationsFilePath(dataDir))
	if err != nil {
		return nil, err
	}
	_, list, ok := findAnnotationKey(store.Notes, notePath)
	if !ok {
		return nil, nil
	}
	out := make([]StoredAnnotation, len(list))
	copy(out, list)
	return out, nil
}

// SaveNoteAnnotations atomically updates notePath's annotations in annotations.json.
func SaveNoteAnnotations(dataDir, notePath string, notes []StoredAnnotation) error {
	if strings.TrimSpace(notePath) == "" {
		return nil
	}
	annotMu.Lock()
	defer annotMu.Unlock()

	filePath := annotationsFilePath(dataDir)
	store, err := loadAnnotationStoreLocked(filePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		// Never rewrite an unreadable store: that would drop every other note's annotations.
		LogWarnf("load annotations store failed before save, skipping write: %v", err)
		return err
	}
	if store.Notes == nil {
		store.Notes = make(map[string][]StoredAnnotation)
	}

	key := filepath.Clean(notePath)
	if existingKey, _, ok := findAnnotationKey(store.Notes, notePath); ok && existingKey != key {
		delete(store.Notes, existingKey)
	}
	if len(notes) == 0 {
		delete(store.Notes, key)
	} else {
		normalized := make([]StoredAnnotation, len(notes))
		for i, n := range notes {
			if n.Reactions == nil {
				n.Reactions = []string{}
			}
			normalized[i] = n
		}
		store.Notes[key] = normalized
	}
	return writeAnnotationStoreAtomicLocked(filePath, &store)
}

// MoveNoteAnnotations re-keys persisted annotations when a note or folder is
// renamed or moved: the entry for oldPath and every entry under oldPath/ follow.
func MoveNoteAnnotations(dataDir, oldPath, newPath string) error {
	if strings.TrimSpace(oldPath) == "" || strings.TrimSpace(newPath) == "" {
		return nil
	}
	cleanOld := filepath.Clean(oldPath)
	cleanNew := filepath.Clean(newPath)
	if cleanOld == cleanNew {
		return nil
	}
	annotMu.Lock()
	defer annotMu.Unlock()

	filePath := annotationsFilePath(dataDir)
	store, err := loadAnnotationStoreLocked(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	prefix := cleanOld + string(filepath.Separator)
	moved := false
	for key, list := range store.Notes {
		ck := filepath.Clean(key)
		var next string
		switch {
		case ck == cleanOld:
			next = cleanNew
		case strings.HasPrefix(ck, prefix):
			next = filepath.Join(cleanNew, strings.TrimPrefix(ck, prefix))
		default:
			continue
		}
		delete(store.Notes, key)
		store.Notes[next] = list
		moved = true
	}
	if !moved {
		return nil
	}
	return writeAnnotationStoreAtomicLocked(filePath, &store)
}

func writeAnnotationStoreAtomicLocked(path string, store *AnnotationStore) error {
	parent := filepath.Dir(path)
	if parent == "" || parent == "." {
		return fmt.Errorf("invalid annotations path %q", path)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	var (
		tmpPath string
		tmpFile *os.File
	)
	pid := os.Getpid()
	for range 64 {
		seq := annotTempSeq.Add(1)
		candidate := filepath.Join(parent, fmt.Sprintf(".annotations-%d-%d.tmp", pid, seq))
		f, openErr := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
		return errors.New("failed to create temporary annotations file")
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
		if annotCache.valid && annotCache.path == path {
			annotCache = annotStoreCacheEntry{}
		}
	} else if info, statErr := os.Stat(path); statErr == nil {
		annotCache = annotStoreCacheEntry{
			path:    path,
			modTime: info.ModTime(),
			size:    info.Size(),
			store:   cloneAnnotationStore(*store),
			valid:   true,
		}
	} else if annotCache.valid && annotCache.path == path {
		annotCache = annotStoreCacheEntry{}
	}

	cleanAnnotTempsOnce.Do(func() {
		cleanStaleAnnotTemps(parent)
	})
	return commitErr
}

func cleanStaleAnnotTemps(parent string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".annotations-") && strings.HasSuffix(name, ".tmp") {
			if info, err := entry.Info(); err == nil {
				if now.Sub(info.ModTime()) > 60*time.Second {
					_ = os.Remove(filepath.Join(parent, name))
				}
			}
		}
	}
}
