package files

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	notebooksRegistryFile = "notebooks.json"
	notebookMissingExpiry = 30 * 24 * time.Hour
	notebookLastSeenStep  = int64(5 * 60)
)

// NotebookRecord tracks the durable identity and filesystem metadata of a notebook.
type NotebookRecord struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Device     uint64 `json:"device,omitempty"`
	Inode      uint64 `json:"inode,omitempty"`
	Locator    string `json:"locator,omitempty"`
	LastSeenAt int64  `json:"last_seen_at"`
}

// NotebookRemoved describes a notebook that was expired or forgotten from the registry.
type NotebookRemoved struct {
	ID   string
	Path string
}

// NotebookMove describes an externally moved notebook whose durable ID was reclaimed.
type NotebookMove struct {
	ID      string
	OldPath string
	NewPath string
}

type notebooksRegistryPayload struct {
	Version   int              `json:"version"`
	Notebooks []NotebookRecord `json:"notebooks"`
}

type notebookCacheEntry struct {
	dataDir string
	hasStat bool
	modTime int64
	size    int64
	records []NotebookRecord
}

var (
	notebooksMu      sync.Mutex
	notebooksTempSeq atomic.Uint64
	notebookCache    notebookCacheEntry
)

// IsSafeNotebookID reports whether id is a valid single-component notebook identifier.
func IsSafeNotebookID(id string) bool {
	if id == "" || len(id) > 64 || id == "." || id == ".." {
		return false
	}
	first := id[0]
	isAlnum := (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || (first >= '0' && first <= '9')
	if !isAlnum {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}

// DeterministicFallbackNotebookID derives a deterministic identifier from path when
// no persistent host app-data registry is available (for isolated CLI checks).
func DeterministicFallbackNotebookID(rawPath string) string {
	clean := normalizeNotebookPath(rawPath)
	if clean == "" {
		clean = strings.TrimSpace(rawPath)
	}
	sum := sha256.Sum256([]byte(clean))
	return "nb_" + hex.EncodeToString(sum[:8])
}

func newNotebookID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "nb_" + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("nb_%016x", time.Now().UnixNano())
}

func normalizeNotebookPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	clean := filepath.Clean(trimmed)
	if !filepath.IsAbs(clean) {
		if abs, err := filepath.Abs(clean); err == nil {
			clean = abs
		}
	}
	return clean
}

func canonicalNotebookPath(clean string) string {
	if clean == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != "" {
		return filepath.Clean(resolved)
	}
	return clean
}

func sameNotebookPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ca := normalizeNotebookPath(a)
	cb := normalizeNotebookPath(b)
	if ca == cb {
		return true
	}
	if runtime.GOOS == "windows" && strings.EqualFold(ca, cb) {
		return true
	}
	ra := canonicalNotebookPath(ca)
	rb := canonicalNotebookPath(cb)
	if ra == rb {
		return true
	}
	if runtime.GOOS == "windows" && strings.EqualFold(ra, rb) {
		return true
	}
	return false
}

func registryFilePath(appDataDir string) string {
	base := strings.TrimSpace(appDataDir)
	if base == "" {
		base = AppDataDir()
	}
	return filepath.Join(base, notebooksRegistryFile)
}

func loadNotebooksLocked(appDataDir string) ([]NotebookRecord, error) {
	path := registryFilePath(appDataDir)
	info, err := os.Stat(path)
	if err == nil && notebookCache.hasStat && notebookCache.dataDir == appDataDir &&
		notebookCache.modTime == info.ModTime().UnixNano() && notebookCache.size == info.Size() {
		return append([]NotebookRecord(nil), notebookCache.records...), nil
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			notebookCache = notebookCacheEntry{dataDir: appDataDir}
			return nil, nil
		}
		return nil, readErr
	}
	var payload notebooksRegistryPayload
	if jErr := json.Unmarshal(data, &payload); jErr != nil {
		return nil, jErr
	}
	records := make([]NotebookRecord, 0, len(payload.Notebooks))
	seenIDs := make(map[string]bool, len(payload.Notebooks))
	for _, rec := range payload.Notebooks {
		rec.ID = strings.TrimSpace(rec.ID)
		rec.Path = normalizeNotebookPath(rec.Path)
		if !IsSafeNotebookID(rec.ID) || rec.Path == "" || seenIDs[rec.ID] {
			continue
		}
		seenIDs[rec.ID] = true
		records = append(records, rec)
	}
	if info != nil {
		notebookCache = notebookCacheEntry{
			dataDir: appDataDir,
			hasStat: true,
			modTime: info.ModTime().UnixNano(),
			size:    info.Size(),
			records: append([]NotebookRecord(nil), records...),
		}
	}
	return records, nil
}

func saveNotebooksLocked(appDataDir string, records []NotebookRecord) error {
	base := strings.TrimSpace(appDataDir)
	if base == "" {
		base = AppDataDir()
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	payload := notebooksRegistryPayload{
		Version:   1,
		Notebooks: records,
	}
	if payload.Notebooks == nil {
		payload.Notebooks = []NotebookRecord{}
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := registryFilePath(base)
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), notebooksTempSeq.Add(1))
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, wErr := f.Write(data)
	sErr := f.Sync()
	cErr := f.Close()
	if wErr != nil || sErr != nil || cErr != nil {
		_ = os.Remove(tmp)
		if wErr != nil {
			return wErr
		}
		if sErr != nil {
			return sErr
		}
		return cErr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if info, sErr := os.Stat(path); sErr == nil {
		notebookCache = notebookCacheEntry{
			dataDir: base,
			hasStat: true,
			modTime: info.ModTime().UnixNano(),
			size:    info.Size(),
			records: append([]NotebookRecord(nil), records...),
		}
	} else {
		notebookCache = notebookCacheEntry{}
	}
	return nil
}

func hasNotebookID(records []NotebookRecord, id string) bool {
	for _, r := range records {
		if r.ID == id {
			return true
		}
	}
	return false
}

// isSameLiveNotebook checks whether oldPath currently points to a distinct live
// directory (true when Finder/cp duplicated the folder while the original stays).
// Returns (oldExists, sameDir).
func inspectOldNotebookPath(oldPath string, curInfo os.FileInfo) (oldExists bool, sameDir bool) {
	if oldPath == "" || IsTrashPath(oldPath) {
		return false, false
	}
	oldInfo, err := os.Stat(oldPath)
	if err != nil || !oldInfo.IsDir() {
		return false, false
	}
	if curInfo != nil && os.SameFile(oldInfo, curInfo) {
		return true, true
	}
	return true, false
}

// EnsureNotebook registers rawPath in appDataDir/notebooks.json if needed, reclaims
// its durable ID after external renames/moves, and handles duplicated folders.
func EnsureNotebook(rawPath string) (NotebookRecord, string, error) {
	return EnsureNotebookIn(AppDataDir(), rawPath)
}

// EnsureNotebookIn is EnsureNotebook scoped to appDataDir.
func EnsureNotebookIn(appDataDir, rawPath string) (NotebookRecord, string, error) {
	clean := normalizeNotebookPath(rawPath)
	if clean == "" {
		return NotebookRecord{}, "", errors.New("empty notebook path")
	}
	appDir := strings.TrimSpace(appDataDir)
	if appDir == "" {
		appDir = AppDataDir()
	}

	info, statErr := os.Stat(clean)
	now := time.Now().Unix()

	notebooksMu.Lock()
	defer notebooksMu.Unlock()

	records, loadErr := loadNotebooksLocked(appDir)
	if loadErr != nil {
		records = nil
	}

	if statErr != nil || !info.IsDir() || IsTrashPath(clean) {
		for _, r := range records {
			if sameNotebookPath(r.Path, clean) {
				return r, "", nil
			}
		}
		if statErr == nil && !info.IsDir() {
			return NotebookRecord{}, "", errors.New("notebook path is not a directory")
		}
		// Previously known offline path entered at startup without an existing record yet:
		// record its initial timestamp so missing > 30 days can still expire cleanly.
		id := newNotebookID()
		for hasNotebookID(records, id) {
			id = newNotebookID()
		}
		rec := NotebookRecord{
			ID:         id,
			Path:       clean,
			LastSeenAt: now,
		}
		records = append(records, rec)
		_ = saveNotebooksLocked(appDir, records)
		return rec, "", nil
	}

	dev, ino, hasDevIno := statDirDevIno(clean, info)
	locator := CaptureDirLocator(clean)
	xattrID := readNotebookXattr(clean)

	// 1. Exact/canonical path match.
	for i := range records {
		if !sameNotebookPath(records[i].Path, clean) {
			continue
		}
		changed := records[i].Device != dev || records[i].Inode != ino || records[i].Locator != locator
		if records[i].Path != clean {
			records[i].Path = clean
			changed = true
		}
		records[i].Device = dev
		records[i].Inode = ino
		records[i].Locator = locator
		needSeenSave := records[i].LastSeenAt == 0 || (now-records[i].LastSeenAt) >= notebookLastSeenStep
		if needSeenSave || changed {
			records[i].LastSeenAt = now
			_ = saveNotebooksLocked(appDir, records)
		}
		writeNotebookXattr(clean, records[i].ID)
		return records[i], "", nil
	}

	// 2. macOS primary identity: custom xattr match across external renames/moves,
	// while minting a fresh ID when the original folder still exists (duplicated folder).
	forceMint := false
	if xattrID != "" {
		for i := range records {
			if records[i].ID != xattrID {
				continue
			}
			oldExists, sameDir := inspectOldNotebookPath(records[i].Path, info)
			if !oldExists || sameDir {
				oldPath := records[i].Path
				records[i].Path = clean
				records[i].Device = dev
				records[i].Inode = ino
				records[i].Locator = locator
				records[i].LastSeenAt = now
				_ = saveNotebooksLocked(appDir, records)
				writeNotebookXattr(clean, records[i].ID)
				return records[i], oldPath, nil
			}
			// Original directory still exists separately -> user copied the folder.
			forceMint = true
			break
		}
	}

	// 3. Fallback identity match via (device, inode) or persistent directory locator.
	if !forceMint {
		for i := range records {
			devInoMatch := hasDevIno && ino != 0 && records[i].Device == dev && records[i].Inode == ino
			locMatch := locator != "" && records[i].Locator == locator
			if !devInoMatch && !locMatch {
				continue
			}
			oldExists, sameDir := inspectOldNotebookPath(records[i].Path, info)
			if !oldExists || sameDir {
				oldPath := records[i].Path
				records[i].Path = clean
				records[i].Device = dev
				records[i].Inode = ino
				records[i].Locator = locator
				records[i].LastSeenAt = now
				_ = saveNotebooksLocked(appDir, records)
				writeNotebookXattr(clean, records[i].ID)
				return records[i], oldPath, nil
			}
		}
	}

	// 4. Register new notebook (reusing unclaimed safe xattr only if not a copy).
	id := ""
	if !forceMint && IsSafeNotebookID(xattrID) && !hasNotebookID(records, xattrID) {
		id = xattrID
	} else {
		id = newNotebookID()
		for hasNotebookID(records, id) {
			id = newNotebookID()
		}
	}
	rec := NotebookRecord{
		ID:         id,
		Path:       clean,
		Device:     dev,
		Inode:      ino,
		Locator:    locator,
		LastSeenAt: now,
	}
	records = append(records, rec)
	_ = saveNotebooksLocked(appDir, records)
	writeNotebookXattr(clean, rec.ID)
	return rec, "", nil
}

// RenameNotebookIn updates the registered notebook record matching oldRawPath (or
// newRawPath identity) to newRawPath and refreshes xattr/device/inode metadata.
func RenameNotebookIn(appDataDir, oldRawPath, newRawPath string) (NotebookRecord, bool, error) {
	oldClean := normalizeNotebookPath(oldRawPath)
	newClean := normalizeNotebookPath(newRawPath)
	if oldClean == "" || newClean == "" {
		return NotebookRecord{}, false, errors.New("invalid notebook path")
	}
	appDir := strings.TrimSpace(appDataDir)
	if appDir == "" {
		appDir = AppDataDir()
	}

	info, statErr := os.Stat(newClean)
	var dev, ino uint64
	var locator, xattrID string
	if statErr == nil && info.IsDir() {
		dev, ino, _ = statDirDevIno(newClean, info)
		locator = CaptureDirLocator(newClean)
		xattrID = readNotebookXattr(newClean)
	}
	now := time.Now().Unix()

	notebooksMu.Lock()
	defer notebooksMu.Unlock()

	records, _ := loadNotebooksLocked(appDir)
	idx := -1
	for i := range records {
		if sameNotebookPath(records[i].Path, oldClean) {
			idx = i
			break
		}
	}
	if idx < 0 && xattrID != "" {
		for i := range records {
			if records[i].ID == xattrID {
				oldExists, sameDir := inspectOldNotebookPath(records[i].Path, info)
				if !oldExists || sameDir {
					idx = i
					break
				}
			}
		}
	}
	if idx < 0 {
		id := newNotebookID()
		for hasNotebookID(records, id) {
			id = newNotebookID()
		}
		rec := NotebookRecord{
			ID:         id,
			Path:       newClean,
			Device:     dev,
			Inode:      ino,
			Locator:    locator,
			LastSeenAt: now,
		}
		records = append(records, rec)
		err := saveNotebooksLocked(appDir, records)
		if statErr == nil && info.IsDir() {
			writeNotebookXattr(newClean, rec.ID)
		}
		return rec, true, err
	}

	// Drop any stale duplicate entry already claiming newClean before rebasing idx.
	targetID := records[idx].ID
	filtered := make([]NotebookRecord, 0, len(records))
	var updated NotebookRecord
	for i := range records {
		if i != idx && sameNotebookPath(records[i].Path, newClean) {
			continue
		}
		if i == idx {
			records[i].Path = newClean
			if dev != 0 || ino != 0 {
				records[i].Device = dev
				records[i].Inode = ino
			}
			if locator != "" {
				records[i].Locator = locator
			}
			records[i].LastSeenAt = now
			updated = records[i]
		}
		filtered = append(filtered, records[i])
	}
	if updated.ID == "" {
		updated.ID = targetID
	}
	err := saveNotebooksLocked(appDir, filtered)
	if statErr == nil && info.IsDir() {
		writeNotebookXattr(newClean, updated.ID)
	}
	return updated, true, err
}

// RemoveNotebookIn unregisters a notebook (by path or id), clears its xattr if
// present, and deletes its host replaced stash and per-notebook plugin data dirs.
func RemoveNotebookIn(appDataDir, pathOrID string) (NotebookRecord, bool, error) {
	key := strings.TrimSpace(pathOrID)
	if key == "" {
		return NotebookRecord{}, false, nil
	}
	appDir := strings.TrimSpace(appDataDir)
	if appDir == "" {
		appDir = AppDataDir()
	}
	cleanKey := normalizeNotebookPath(key)

	notebooksMu.Lock()
	records, loadErr := loadNotebooksLocked(appDir)
	if loadErr != nil {
		notebooksMu.Unlock()
		return NotebookRecord{}, false, loadErr
	}
	idx := -1
	for i := range records {
		if records[i].ID == key || sameNotebookPath(records[i].Path, cleanKey) {
			idx = i
			break
		}
	}
	if idx < 0 {
		notebooksMu.Unlock()
		return NotebookRecord{}, false, nil
	}
	removed := records[idx]
	next := make([]NotebookRecord, 0, len(records)-1)
	next = append(next, records[:idx]...)
	next = append(next, records[idx+1:]...)
	saveErr := saveNotebooksLocked(appDir, next)
	notebooksMu.Unlock()

	if saveErr != nil {
		return NotebookRecord{}, false, saveErr
	}
	if removed.Path != "" {
		clearNotebookXattr(removed.Path)
	}
	CleanupNotebookDataIn(appDir, removed.ID)
	return removed, true, nil
}

// CleanupNotebookDataIn deletes <appDataDir>/replaced/<id> and
// <appDataDir>/apps-data/*/notebooks/<id>.
func CleanupNotebookDataIn(appDataDir, id string) {
	if !IsSafeNotebookID(id) {
		return
	}
	appDir := strings.TrimSpace(appDataDir)
	if appDir == "" {
		appDir = AppDataDir()
	}
	_ = os.RemoveAll(filepath.Join(appDir, "replaced", id))

	appsDataRoot := filepath.Join(appDir, "apps-data")
	entries, err := os.ReadDir(appsDataRoot)
	if err != nil {
		return
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		nbDir := filepath.Join(appsDataRoot, ent.Name(), "notebooks", id)
		_ = os.RemoveAll(nbDir)
	}
}

// ResolveNotebookIn finds a registered notebook by ID or path. When knownPaths is
// non-empty, results are constrained to currently registered paths in knownPaths
// (with EnsureNotebookIn run on match if needed).
func ResolveNotebookIn(appDataDir, selector string, knownPaths []string) (NotebookRecord, bool) {
	sel := strings.TrimSpace(selector)
	if sel == "" {
		return NotebookRecord{}, false
	}
	appDir := strings.TrimSpace(appDataDir)
	if appDir == "" {
		appDir = AppDataDir()
	}

	for _, kp := range knownPaths {
		rec, _, err := EnsureNotebookIn(appDir, kp)
		if err != nil {
			continue
		}
		if rec.ID == sel || sameNotebookPath(rec.Path, sel) {
			return rec, true
		}
	}
	if len(knownPaths) > 0 {
		return NotebookRecord{}, false
	}

	notebooksMu.Lock()
	records, _ := loadNotebooksLocked(appDir)
	notebooksMu.Unlock()
	for _, r := range records {
		if r.ID == sel || sameNotebookPath(r.Path, sel) {
			return r, true
		}
	}
	return NotebookRecord{}, false
}

// NotebookIDForPathIn returns the registered ID for rawPath without minting one.
func NotebookIDForPathIn(appDataDir, rawPath string) string {
	clean := normalizeNotebookPath(rawPath)
	if clean == "" {
		return ""
	}
	appDir := strings.TrimSpace(appDataDir)
	if appDir == "" {
		appDir = AppDataDir()
	}
	notebooksMu.Lock()
	records, _ := loadNotebooksLocked(appDir)
	notebooksMu.Unlock()
	for _, r := range records {
		if sameNotebookPath(r.Path, clean) {
			return r.ID
		}
	}
	return ""
}

// NotebookIDForDescendantIn finds the registered ID of the deepest notebook whose
// root equals or contains dirPath.
func NotebookIDForDescendantIn(appDataDir, dirPath string) string {
	clean := normalizeNotebookPath(dirPath)
	if clean == "" {
		return ""
	}
	canon := canonicalNotebookPath(clean)
	appDir := strings.TrimSpace(appDataDir)
	if appDir == "" {
		appDir = AppDataDir()
	}
	notebooksMu.Lock()
	records, _ := loadNotebooksLocked(appDir)
	notebooksMu.Unlock()

	bestID := ""
	bestLen := -1
	for _, r := range records {
		if r.ID == "" || r.Path == "" {
			continue
		}
		candPaths := []string{r.Path}
		if c := canonicalNotebookPath(r.Path); c != "" && c != r.Path {
			candPaths = append(candPaths, c)
		}
		for _, root := range candPaths {
			rel, err := filepath.Rel(root, clean)
			if (err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) && canon != clean {
				rel, err = filepath.Rel(root, canon)
			}
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				if len(root) > bestLen {
					bestLen = len(root)
					bestID = r.ID
				}
			}
		}
	}
	return bestID
}

// SweepNotebookRegistryIn refreshes last_seen_at for existing notebooks and expires
// any entry whose path has remained missing (or in OS Trash) for over 30 days,
// deleting its replaced data and per-notebook directories under apps-data.
// Present but unused notebooks are never purged.
func SweepNotebookRegistryIn(appDataDir string) ([]NotebookRemoved, error) {
	appDir := strings.TrimSpace(appDataDir)
	if appDir == "" {
		appDir = AppDataDir()
	}
	now := time.Now().Unix()
	expirySec := int64(notebookMissingExpiry.Seconds())

	notebooksMu.Lock()
	records, err := loadNotebooksLocked(appDir)
	if err != nil {
		notebooksMu.Unlock()
		return nil, err
	}
	if len(records) == 0 {
		notebooksMu.Unlock()
		return nil, nil
	}

	dirty := false
	seenDelta := false
	kept := make([]NotebookRecord, 0, len(records))
	var expired []NotebookRemoved
	for _, r := range records {
		info, statErr := os.Stat(r.Path)
		if statErr == nil && info.IsDir() && !IsTrashPath(r.Path) {
			dev, ino, _ := statDirDevIno(r.Path, info)
			locator := CaptureDirLocator(r.Path)
			if r.Device != dev || r.Inode != ino || r.Locator != locator {
				r.Device = dev
				r.Inode = ino
				r.Locator = locator
				dirty = true
			}
			if r.LastSeenAt == 0 || (now-r.LastSeenAt) >= 3600 {
				r.LastSeenAt = now
				seenDelta = true
			}
			writeNotebookXattr(r.Path, r.ID)
			kept = append(kept, r)
			continue
		}
		if r.LastSeenAt <= 0 {
			r.LastSeenAt = now
			dirty = true
			kept = append(kept, r)
			continue
		}
		if (now - r.LastSeenAt) >= expirySec {
			expired = append(expired, NotebookRemoved{ID: r.ID, Path: r.Path})
			dirty = true
			continue
		}
		kept = append(kept, r)
	}
	if dirty || seenDelta {
		if sErr := saveNotebooksLocked(appDir, kept); sErr != nil {
			notebooksMu.Unlock()
			return nil, sErr
		}
	}
	notebooksMu.Unlock()

	for _, rem := range expired {
		CleanupNotebookDataIn(appDir, rem.ID)
	}
	return expired, nil
}
