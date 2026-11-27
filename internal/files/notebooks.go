package files

import (
	"context"
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
	sweepLastSeenStep     = int64(3600)
	maxPreviousPaths      = 64
	notebookProbeTimeout  = 2 * time.Second
	notebookProbeWorkers  = 4
)

// NotebookRecord tracks the durable identity and filesystem metadata of a notebook.
type NotebookRecord struct {
	ID            string   `json:"id"`
	Path          string   `json:"path"`
	PreviousPaths []string `json:"previous_paths,omitempty"`
	Device        uint64   `json:"device,omitempty"`
	Inode         uint64   `json:"inode,omitempty"`
	Locator       string   `json:"locator,omitempty"`
	LastSeenAt    int64    `json:"last_seen_at"`
	MissingSince  int64    `json:"missing_since,omitempty"`
	RemovedAt     int64    `json:"removed_at,omitempty"`
	Superseded    bool     `json:"superseded,omitempty"`
}

// IsActive reports whether the notebook record is active and pending neither
// user removal cleanup nor replacement by another directory at the same path.
func (r NotebookRecord) IsActive() bool {
	return r.RemovedAt == 0 && !r.Superseded
}

// NotebookRemoved describes a notebook expired and purged from the registry.
type NotebookRemoved struct {
	ID   string
	Path string
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

type notebookSnapshotEntry struct {
	loaded  bool
	loadErr bool
	records []NotebookRecord
}

var (
	notebooksMu        sync.Mutex
	notebookCache      notebookCacheEntry
	notebooksTempSeq   atomic.Uint64
	snapshotMu         sync.RWMutex
	publishedSnapshots = make(map[string]notebookSnapshotEntry)
	cleaningMu         sync.RWMutex
	cleaningNotebooks  = make(map[string]bool)
)

func resolveAppDataDir(appDataDir string) string {
	base := strings.TrimSpace(appDataDir)
	if base == "" {
		base = AppDataDir()
	}
	if base != "" {
		base = filepath.Clean(base)
	}
	return base
}

func cloneNotebookRecord(r NotebookRecord) NotebookRecord {
	if len(r.PreviousPaths) > 0 {
		r.PreviousPaths = append([]string(nil), r.PreviousPaths...)
	} else {
		r.PreviousPaths = nil
	}
	return r
}

func cloneNotebookRecords(records []NotebookRecord) []NotebookRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]NotebookRecord, len(records))
	for i, r := range records {
		out[i] = cloneNotebookRecord(r)
	}
	return out
}

func cleaningKey(appDir, id string) string {
	return appDir + "\x00" + id
}

func setNotebookCleaning(appDir, id string, active bool) {
	if appDir == "" || id == "" {
		return
	}
	k := cleaningKey(appDir, id)
	cleaningMu.Lock()
	if active {
		cleaningNotebooks[k] = true
	} else {
		delete(cleaningNotebooks, k)
	}
	cleaningMu.Unlock()
}

// IsNotebookCleaningIn reports whether id is currently undergoing cleanup under appDataDir.
func IsNotebookCleaningIn(appDataDir, id string) bool {
	appDir := resolveAppDataDir(appDataDir)
	if appDir == "" || id == "" {
		return false
	}
	cleaningMu.RLock()
	active := cleaningNotebooks[cleaningKey(appDir, id)]
	cleaningMu.RUnlock()
	return active
}

func publishNotebookSnapshot(appDir string, records []NotebookRecord, loadErr bool) {
	if appDir == "" {
		return
	}
	cloned := cloneNotebookRecords(records)
	snapshotMu.Lock()
	publishedSnapshots[appDir] = notebookSnapshotEntry{
		loaded:  true,
		loadErr: loadErr,
		records: cloned,
	}
	snapshotMu.Unlock()
}

func markSnapshotLoadError(appDir string) {
	if appDir == "" {
		return
	}
	snapshotMu.Lock()
	ent := publishedSnapshots[appDir]
	ent.loaded = true
	ent.loadErr = true
	publishedSnapshots[appDir] = ent
	snapshotMu.Unlock()
}

// RegistryHasLoadErrorIn reports whether the last registry read for appDataDir failed.
func RegistryHasLoadErrorIn(appDataDir string) bool {
	appDir := resolveAppDataDir(appDataDir)
	if appDir == "" {
		return false
	}
	snapshotMu.RLock()
	loadErr := publishedSnapshots[appDir].loadErr
	snapshotMu.RUnlock()
	return loadErr
}

// WarmNotebookSnapshotIn ensures the published in-memory snapshot for appDataDir is
// populated from notebooks.json without probing notebook directories on disk.
func WarmNotebookSnapshotIn(appDataDir string) error {
	appDir := resolveAppDataDir(appDataDir)
	if appDir == "" {
		return nil
	}
	snapshotMu.RLock()
	ent := publishedSnapshots[appDir]
	snapshotMu.RUnlock()
	if ent.loaded && !ent.loadErr {
		return nil
	}
	notebooksMu.Lock()
	_, err := loadNotebooksLocked(appDir)
	notebooksMu.Unlock()
	return err
}

// SnapshotNotebooksIn returns a deep-cloned slice of active, non-cleaning notebook
// records from the published in-memory snapshot without acquiring notebooksMu or
// running filesystem probes.
func SnapshotNotebooksIn(appDataDir string) []NotebookRecord {
	appDir := resolveAppDataDir(appDataDir)
	if appDir == "" {
		return nil
	}
	snapshotMu.RLock()
	ent := publishedSnapshots[appDir]
	snapshotMu.RUnlock()
	if !ent.loaded {
		_ = WarmNotebookSnapshotIn(appDir)
		snapshotMu.RLock()
		ent = publishedSnapshots[appDir]
		snapshotMu.RUnlock()
	}
	if len(ent.records) == 0 {
		return nil
	}
	out := make([]NotebookRecord, 0, len(ent.records))
	cleaningMu.RLock()
	for _, r := range ent.records {
		if !r.IsActive() || cleaningNotebooks[cleaningKey(appDir, r.ID)] {
			continue
		}
		out = append(out, cloneNotebookRecord(r))
	}
	cleaningMu.RUnlock()
	return out
}

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

func normalizePreviousPaths(paths []string, currentPath string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		p := normalizeNotebookPath(raw)
		if p == "" {
			continue
		}
		if currentPath != "" && sameNotebookPath(p, currentPath) {
			continue
		}
		dup := false
		for _, existing := range out {
			if sameNotebookPath(existing, p) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p)
		}
	}
	if len(out) > maxPreviousPaths {
		out = append([]string(nil), out[len(out)-maxPreviousPaths:]...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func appendPreviousPath(r *NotebookRecord, oldPath string) {
	if r == nil {
		return
	}
	normOld := normalizeNotebookPath(oldPath)
	if normOld != "" {
		r.PreviousPaths = append(r.PreviousPaths, normOld)
	}
	r.PreviousPaths = normalizePreviousPaths(r.PreviousPaths, r.Path)
}

func registryFilePath(appDataDir string) string {
	return filepath.Join(resolveAppDataDir(appDataDir), notebooksRegistryFile)
}

func loadNotebooksLocked(appDataDir string) ([]NotebookRecord, error) {
	base := resolveAppDataDir(appDataDir)
	path := registryFilePath(base)
	info, statErr := os.Stat(path)
	if statErr == nil && notebookCache.hasStat && notebookCache.dataDir == base &&
		notebookCache.modTime == info.ModTime().UnixNano() && notebookCache.size == info.Size() {
		publishNotebookSnapshot(base, notebookCache.records, false)
		return cloneNotebookRecords(notebookCache.records), nil
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			notebookCache = notebookCacheEntry{dataDir: base}
			publishNotebookSnapshot(base, nil, false)
			return nil, nil
		}
		markSnapshotLoadError(base)
		return nil, readErr
	}
	var payload notebooksRegistryPayload
	if jErr := json.Unmarshal(data, &payload); jErr != nil {
		markSnapshotLoadError(base)
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
		rec.PreviousPaths = normalizePreviousPaths(rec.PreviousPaths, rec.Path)
		seenIDs[rec.ID] = true
		records = append(records, rec)
	}
	if info != nil {
		notebookCache = notebookCacheEntry{
			dataDir: base,
			hasStat: true,
			modTime: info.ModTime().UnixNano(),
			size:    info.Size(),
			records: cloneNotebookRecords(records),
		}
	}
	publishNotebookSnapshot(base, records, false)
	return records, nil
}

func syncRegistryParentDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func saveNotebooksLocked(appDataDir string, records []NotebookRecord) error {
	base := resolveAppDataDir(appDataDir)
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	normalized := cloneNotebookRecords(records)
	for i := range normalized {
		normalized[i].PreviousPaths = normalizePreviousPaths(normalized[i].PreviousPaths, normalized[i].Path)
	}
	payload := notebooksRegistryPayload{
		Version:   1,
		Notebooks: normalized,
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
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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
	syncRegistryParentDir(filepath.Dir(path))
	if info, sErr := os.Stat(path); sErr == nil {
		notebookCache = notebookCacheEntry{
			dataDir: base,
			hasStat: true,
			modTime: info.ModTime().UnixNano(),
			size:    info.Size(),
			records: cloneNotebookRecords(normalized),
		}
	} else {
		notebookCache = notebookCacheEntry{}
	}
	publishNotebookSnapshot(base, normalized, false)
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

// inspectOldNotebookPath checks whether oldPath currently points to a distinct live
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

type dirProbeResult struct {
	info      os.FileInfo
	statErr   error
	dev       uint64
	ino       uint64
	hasDevIno bool
	locator   string
	xattrID   string
}

var errNotebookProbeTimeout = errors.New("notebook directory probe timed out")

func runProbeWithTimeout[T any](ctx context.Context, timeout time.Duration, fn func() T) (T, bool) {
	if ctx != nil && ctx.Err() != nil {
		var zero T
		return zero, false
	}
	if timeout <= 0 {
		timeout = notebookProbeTimeout
	}
	wait := timeout
	if ctx != nil {
		if dl, ok := ctx.Deadline(); ok {
			rem := time.Until(dl)
			if rem <= 0 {
				var zero T
				return zero, false
			}
			if rem < wait {
				wait = rem
			}
		}
	}
	ch := make(chan T, 1)
	go func() {
		ch <- fn()
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	if ctx != nil {
		select {
		case res := <-ch:
			return res, true
		case <-timer.C:
			var zero T
			return zero, false
		case <-ctx.Done():
			var zero T
			return zero, false
		}
	}
	select {
	case res := <-ch:
		return res, true
	case <-timer.C:
		var zero T
		return zero, false
	}
}

func probeDirIdentity(ctx context.Context, clean string) dirProbeResult {
	res, ok := runProbeWithTimeout(ctx, notebookProbeTimeout, func() dirProbeResult {
		info, statErr := os.Stat(clean)
		if statErr != nil || !info.IsDir() || IsTrashPath(clean) {
			return dirProbeResult{info: info, statErr: statErr}
		}
		dev, ino, hasDevIno := statDirDevIno(clean, info)
		return dirProbeResult{
			info:      info,
			statErr:   nil,
			dev:       dev,
			ino:       ino,
			hasDevIno: hasDevIno,
			locator:   CaptureDirLocator(clean),
			xattrID:   readNotebookXattr(clean),
		}
	})
	if !ok {
		return dirProbeResult{statErr: errNotebookProbeTimeout}
	}
	return res
}

type oldPathMissingCheck struct {
	confirmedMissing bool
	foundLivePath    string
}

func probeOldPathMissing(ctx context.Context, oldPath, expectedID string, recDev uint64, recLocator string, excludeInfo os.FileInfo) oldPathMissingCheck {
	res, ok := runProbeWithTimeout(ctx, notebookProbeTimeout, func() oldPathMissingCheck {
		if oldPath == "" {
			return oldPathMissingCheck{}
		}
		// 1. Check whether the directory itself or a dangling symlink exists.
		linfo, lErr := os.Lstat(oldPath)
		if lErr == nil {
			if linfo.Mode()&os.ModeSymlink != 0 {
				target, rlErr := os.Readlink(oldPath)
				if rlErr != nil {
					return oldPathMissingCheck{}
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(oldPath), target)
				}
				targetClean := filepath.Clean(target)
				tInfo, tErr := os.Stat(targetClean)
				if tErr == nil {
					if tInfo.IsDir() && !IsTrashPath(targetClean) && (excludeInfo == nil || !os.SameFile(tInfo, excludeInfo)) {
						return oldPathMissingCheck{foundLivePath: targetClean}
					}
					return oldPathMissingCheck{}
				}
				if !isStrictNotExistErr(tErr) {
					return oldPathMissingCheck{}
				}
				tParent := filepath.Dir(targetClean)
				if tParent == "" || tParent == targetClean {
					return oldPathMissingCheck{}
				}
				tpInfo, tpErr := os.Stat(tParent)
				if tpErr != nil || !tpInfo.IsDir() {
					return oldPathMissingCheck{}
				}
				tpDev, _, hasTPDev := statDirDevIno(tParent, tpInfo)
				if !isParentVolumeAccessible(tParent, tpDev, hasTPDev, recDev, recLocator) {
					return oldPathMissingCheck{}
				}
			} else {
				// Path still resolves cleanly (os.Stat might return EACCES/not-dir/etc., or replaced dir).
				return oldPathMissingCheck{}
			}
		} else if !isStrictNotExistErr(lErr) {
			return oldPathMissingCheck{}
		}

		// 2. Verify parent directory is accessible and on expected volume (not an unmounted mountpoint).
		parent := filepath.Dir(oldPath)
		if parent == "" || parent == oldPath {
			return oldPathMissingCheck{}
		}
		pInfo, pErr := os.Stat(parent)
		if pErr != nil || !pInfo.IsDir() {
			return oldPathMissingCheck{}
		}
		pDev, _, hasPDev := statDirDevIno(parent, pInfo)
		if !isParentVolumeAccessible(parent, pDev, hasPDev, recDev, recLocator) {
			return oldPathMissingCheck{}
		}

		// 3. Verify the directory cannot be recovered via locator or sibling xattr.
		if recLocator != "" {
			if locPath, ok := ResolveDirLocator(oldPath, recLocator); ok && locPath != "" && !IsTrashPath(locPath) {
				if locInfo, sErr := os.Stat(locPath); sErr == nil && locInfo.IsDir() {
					if excludeInfo == nil || !os.SameFile(locInfo, excludeInfo) {
						if !supportsNotebookXattr() || expectedID == "" || readNotebookXattr(locPath) == expectedID {
							return oldPathMissingCheck{foundLivePath: filepath.Clean(locPath)}
						}
					}
				}
			}
		}
		if supportsNotebookXattr() && expectedID != "" {
			if sibPath, ok := findSiblingByNotebookXattr(parent, expectedID); ok {
				if sibInfo, sErr := os.Stat(sibPath); sErr == nil && sibInfo.IsDir() {
					if excludeInfo == nil || !os.SameFile(sibInfo, excludeInfo) {
						return oldPathMissingCheck{foundLivePath: filepath.Clean(sibPath)}
					}
				}
			}
		}
		return oldPathMissingCheck{confirmedMissing: true}
	})
	if !ok {
		return oldPathMissingCheck{}
	}
	return res
}

func recordMatchesExactIdentity(r NotebookRecord, dev, ino uint64, hasDevIno bool, xattrID string) bool {
	if !hasDevIno || ino == 0 || r.Inode == 0 || r.Device != dev || r.Inode != ino {
		return false
	}
	if supportsNotebookXattr() {
		return xattrID != "" && xattrID == r.ID
	}
	return true
}

// EnsureNotebook registers rawPath in appDataDir/notebooks.json if needed, reclaims
// its durable ID only on strong verified identity, and handles copied/replaced folders.
func EnsureNotebook(rawPath string) (NotebookRecord, string, error) {
	return EnsureNotebookIn(AppDataDir(), rawPath)
}

// EnsureNotebookIn is EnsureNotebook scoped to appDataDir.
func EnsureNotebookIn(appDataDir, rawPath string) (NotebookRecord, string, error) {
	return EnsureNotebookContext(context.Background(), appDataDir, rawPath)
}

// EnsureNotebookContext is EnsureNotebookIn bounded by ctx.
func EnsureNotebookContext(ctx context.Context, appDataDir, rawPath string) (NotebookRecord, string, error) {
	clean := normalizeNotebookPath(rawPath)
	if clean == "" {
		return NotebookRecord{}, "", errors.New("empty notebook path")
	}
	appDir := resolveAppDataDir(appDataDir)
	now := time.Now().Unix()

	// Probe target directory outside notebooksMu.
	probe := probeDirIdentity(ctx, clean)
	if probe.statErr == nil && !probe.info.IsDir() {
		return NotebookRecord{}, "", errors.New("notebook path is not a directory")
	}
	if probe.statErr != nil || !probe.info.IsDir() || IsTrashPath(clean) {
		notebooksMu.Lock()
		records, loadErr := loadNotebooksLocked(appDir)
		notebooksMu.Unlock()
		if loadErr != nil {
			return NotebookRecord{}, "", loadErr
		}
		for _, r := range records {
			if r.IsActive() && sameNotebookPath(r.Path, clean) && !IsNotebookCleaningIn(appDir, r.ID) {
				return cloneNotebookRecord(r), "", nil
			}
		}
		if probe.statErr != nil {
			return NotebookRecord{}, "", probe.statErr
		}
		return NotebookRecord{}, "", errors.New("notebook path is unavailable")
	}

	notebooksMu.Lock()
	records, loadErr := loadNotebooksLocked(appDir)
	if loadErr != nil {
		notebooksMu.Unlock()
		return NotebookRecord{}, "", loadErr
	}

	// 1. Exact/canonical path match: require strong identity on non-superseded entries.
	supersededPathIDs := make([]string, 0, 1)
	for i := range records {
		r := &records[i]
		if r.Superseded || !sameNotebookPath(r.Path, clean) {
			continue
		}
		if !IsNotebookCleaningIn(appDir, r.ID) && recordMatchesExactIdentity(*r, probe.dev, probe.ino, probe.hasDevIno, probe.xattrID) {
			dirty := false
			if r.MissingSince != 0 {
				r.MissingSince = 0
				dirty = true
			}
			if r.RemovedAt != 0 {
				r.RemovedAt = 0
				dirty = true
			}
			if !r.IsActive() {
				r.Superseded = false
				dirty = true
			}
			if probe.locator != "" && r.Locator != probe.locator {
				r.Locator = probe.locator
				dirty = true
			}
			if r.LastSeenAt == 0 || (now-r.LastSeenAt) >= notebookLastSeenStep {
				r.LastSeenAt = now
				dirty = true
			}
			if dirty {
				if err := saveNotebooksLocked(appDir, records); err != nil {
					notebooksMu.Unlock()
					return NotebookRecord{}, "", err
				}
			}
			recCopy := cloneNotebookRecord(*r)
			notebooksMu.Unlock()
			writeNotebookXattr(clean, recCopy.ID)
			return recCopy, "", nil
		}
		// Same path, mismatched xattr or inode (or cleaning): preserve old record intact
		// as superseded so a new directory can never inherit its ID or baseline.
		supersededPathIDs = append(supersededPathIDs, r.ID)
	}

	// 2. macOS primary cross-path identity via xattr.
	// Only reclaim when BOTH xattr + (device, inode) match, OR when xattr matches and
	// the old path is confirmed ENOENT on an accessible volume.
	var xattrCandidate *NotebookRecord
	if supportsNotebookXattr() && probe.xattrID != "" {
		for i := range records {
			if records[i].ID == probe.xattrID && !IsNotebookCleaningIn(appDir, records[i].ID) {
				cp := cloneNotebookRecord(records[i])
				xattrCandidate = &cp
				break
			}
		}
	}

	forceMint := false
	var reclaimTargetID string
	if xattrCandidate != nil {
		sameDevIno := probe.hasDevIno && probe.ino != 0 && xattrCandidate.Inode != 0 &&
			xattrCandidate.Device == probe.dev && xattrCandidate.Inode == probe.ino
		if sameDevIno {
			reclaimTargetID = xattrCandidate.ID
		} else if xattrCandidate.Superseded || sameNotebookPath(xattrCandidate.Path, clean) {
			// Same path mismatched inode, or old path was already superseded by another dir -> treat as copy/new.
			forceMint = true
		} else {
			// Verify old path outside notebooksMu before reclaiming cross-volume / renamed directory.
			oldPath := xattrCandidate.Path
			oldDev := xattrCandidate.Device
			oldLoc := xattrCandidate.Locator
			oldID := xattrCandidate.ID
			notebooksMu.Unlock()
			chk := probeOldPathMissing(ctx, oldPath, oldID, oldDev, oldLoc, probe.info)
			notebooksMu.Lock()
			records, loadErr = loadNotebooksLocked(appDir)
			if loadErr != nil {
				notebooksMu.Unlock()
				return NotebookRecord{}, "", loadErr
			}
			oldStillMatches := false
			for i := range records {
				if records[i].ID == oldID && !records[i].Superseded && sameNotebookPath(records[i].Path, oldPath) {
					oldStillMatches = true
					break
				}
			}
			if oldStillMatches && chk.confirmedMissing && chk.foundLivePath == "" && !IsNotebookCleaningIn(appDir, oldID) {
				reclaimTargetID = oldID
			} else {
				forceMint = true
			}
		}
	}

	// 3. Fallback identity match for platforms without xattr (Windows/Linux).
	if !supportsNotebookXattr() && !forceMint && reclaimTargetID == "" {
		var fallbackCand *NotebookRecord
		var fallbackSameDevIno bool
		for i := range records {
			if IsNotebookCleaningIn(appDir, records[i].ID) {
				continue
			}
			devInoMatch := probe.hasDevIno && probe.ino != 0 && records[i].Inode != 0 &&
				records[i].Device == probe.dev && records[i].Inode == probe.ino
			locMatch := probe.locator != "" && records[i].Locator != "" && records[i].Locator == probe.locator
			if !devInoMatch && !locMatch {
				continue
			}
			cp := cloneNotebookRecord(records[i])
			fallbackCand = &cp
			fallbackSameDevIno = devInoMatch
			break
		}
		if fallbackCand != nil {
			if fallbackSameDevIno {
				reclaimTargetID = fallbackCand.ID
			} else if !fallbackCand.Superseded && !sameNotebookPath(fallbackCand.Path, clean) {
				oldPath := fallbackCand.Path
				oldDev := fallbackCand.Device
				oldLoc := fallbackCand.Locator
				oldID := fallbackCand.ID
				notebooksMu.Unlock()
				chk := probeOldPathMissing(ctx, oldPath, oldID, oldDev, oldLoc, probe.info)
				notebooksMu.Lock()
				records, loadErr = loadNotebooksLocked(appDir)
				if loadErr != nil {
					notebooksMu.Unlock()
					return NotebookRecord{}, "", loadErr
				}
				oldStillMatches := false
				for i := range records {
					if records[i].ID == oldID && !records[i].Superseded && sameNotebookPath(records[i].Path, oldPath) {
						oldStillMatches = true
						break
					}
				}
				if oldStillMatches && chk.confirmedMissing && chk.foundLivePath == "" && !IsNotebookCleaningIn(appDir, oldID) {
					reclaimTargetID = oldID
				}
			}
		}
	}

	// Re-apply superseded state cleanly on current records for any replaced path entries
	// (except if that ID somehow wasn't matched as reclaimTargetID — same path can't match both).
	if len(supersededPathIDs) > 0 {
		for i := range records {
			if !records[i].Superseded && sameNotebookPath(records[i].Path, clean) && records[i].ID != reclaimTargetID {
				records[i].Superseded = true
				if records[i].RemovedAt == 0 && records[i].MissingSince == 0 {
					records[i].MissingSince = now
				}
			}
		}
	}

	if reclaimTargetID != "" {
		for i := range records {
			if records[i].ID != reclaimTargetID {
				continue
			}
			oldPath := records[i].Path
			renamedFrom := ""
			if oldPath != "" && !sameNotebookPath(oldPath, clean) {
				renamedFrom = oldPath
				records[i].Path = clean
				appendPreviousPath(&records[i], oldPath)
			} else {
				records[i].Path = clean
				records[i].PreviousPaths = normalizePreviousPaths(records[i].PreviousPaths, clean)
			}
			records[i].Device = probe.dev
			records[i].Inode = probe.ino
			records[i].Locator = probe.locator
			records[i].LastSeenAt = now
			records[i].MissingSince = 0
			records[i].RemovedAt = 0
			records[i].Superseded = false
			if err := saveNotebooksLocked(appDir, records); err != nil {
				notebooksMu.Unlock()
				return NotebookRecord{}, "", err
			}
			recCopy := cloneNotebookRecord(records[i])
			notebooksMu.Unlock()
			writeNotebookXattr(clean, recCopy.ID)
			return recCopy, renamedFrom, nil
		}
	}

	// 4. Register new notebook with a freshly generated ID (never adopt unrecognized xattr IDs).
	id := newNotebookID()
	for hasNotebookID(records, id) || IsNotebookCleaningIn(appDir, id) {
		id = newNotebookID()
	}
	rec := NotebookRecord{
		ID:         id,
		Path:       clean,
		Device:     probe.dev,
		Inode:      probe.ino,
		Locator:    probe.locator,
		LastSeenAt: now,
	}
	records = append(records, rec)
	if err := saveNotebooksLocked(appDir, records); err != nil {
		notebooksMu.Unlock()
		return NotebookRecord{}, "", err
	}
	recCopy := cloneNotebookRecord(rec)
	notebooksMu.Unlock()
	writeNotebookXattr(clean, recCopy.ID)
	return recCopy, "", nil
}

// RenameNotebookIn updates the registered notebook record matching oldRawPath (or
// verified newRawPath identity) to newRawPath, preserving history in previous_paths.
func RenameNotebookIn(appDataDir, oldRawPath, newRawPath string) (NotebookRecord, bool, error) {
	oldClean := normalizeNotebookPath(oldRawPath)
	newClean := normalizeNotebookPath(newRawPath)
	if oldClean == "" || newClean == "" {
		return NotebookRecord{}, false, errors.New("invalid notebook path")
	}
	appDir := resolveAppDataDir(appDataDir)
	now := time.Now().Unix()

	probe := probeDirIdentity(context.Background(), newClean)

	notebooksMu.Lock()
	defer notebooksMu.Unlock()

	records, loadErr := loadNotebooksLocked(appDir)
	if loadErr != nil {
		return NotebookRecord{}, false, loadErr
	}

	idx := -1
	for i := range records {
		if !records[i].Superseded && sameNotebookPath(records[i].Path, oldClean) && !IsNotebookCleaningIn(appDir, records[i].ID) {
			if probe.statErr == nil && probe.info != nil && probe.info.IsDir() {
				// If target directory exists, verify it wasn't swapped with an unrelated directory.
				if supportsNotebookXattr() && probe.xattrID != "" && probe.xattrID != records[i].ID {
					continue
				}
				if records[i].Inode != 0 && probe.hasDevIno && probe.ino != 0 &&
					(records[i].Device != probe.dev || records[i].Inode != probe.ino) {
					continue
				}
			}
			idx = i
			break
		}
	}
	if idx < 0 && supportsNotebookXattr() && probe.xattrID != "" {
		for i := range records {
			if records[i].ID != probe.xattrID || IsNotebookCleaningIn(appDir, records[i].ID) {
				continue
			}
			sameDevIno := probe.hasDevIno && probe.ino != 0 && records[i].Inode != 0 &&
				records[i].Device == probe.dev && records[i].Inode == probe.ino
			if sameDevIno {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		id := newNotebookID()
		for hasNotebookID(records, id) || IsNotebookCleaningIn(appDir, id) {
			id = newNotebookID()
		}
		rec := NotebookRecord{
			ID:         id,
			Path:       newClean,
			Device:     probe.dev,
			Inode:      probe.ino,
			Locator:    probe.locator,
			LastSeenAt: now,
		}
		records = append(records, rec)
		if err := saveNotebooksLocked(appDir, records); err != nil {
			return NotebookRecord{}, false, err
		}
		if probe.statErr == nil && probe.info != nil && probe.info.IsDir() {
			writeNotebookXattr(newClean, rec.ID)
		}
		return cloneNotebookRecord(rec), true, nil
	}

	// Preserve any distinct non-superseded record previously claiming newClean as
	// superseded rather than dropping its identity or data.
	for i := range records {
		if i != idx && !records[i].Superseded && sameNotebookPath(records[i].Path, newClean) {
			records[i].Superseded = true
			if records[i].RemovedAt == 0 && records[i].MissingSince == 0 {
				records[i].MissingSince = now
			}
		}
	}

	prevPath := records[idx].Path
	records[idx].Path = newClean
	appendPreviousPath(&records[idx], prevPath)
	if !sameNotebookPath(prevPath, oldClean) {
		appendPreviousPath(&records[idx], oldClean)
	}
	if probe.dev != 0 || probe.ino != 0 {
		records[idx].Device = probe.dev
		records[idx].Inode = probe.ino
	}
	if probe.locator != "" {
		records[idx].Locator = probe.locator
	}
	records[idx].LastSeenAt = now
	records[idx].MissingSince = 0
	records[idx].RemovedAt = 0
	records[idx].Superseded = false

	if err := saveNotebooksLocked(appDir, records); err != nil {
		return NotebookRecord{}, false, err
	}
	updated := cloneNotebookRecord(records[idx])
	if probe.statErr == nil && probe.info != nil && probe.info.IsDir() {
		writeNotebookXattr(newClean, updated.ID)
	}
	return updated, true, nil
}

// RemoveNotebookIn marks a registered notebook (by id or path) as removed in the
// registry with removed_at = now instead of immediately purging its data. Data cleanup
// occurs after 30 days unless the same notebook identity is re-added sooner.
func RemoveNotebookIn(appDataDir, pathOrID string) (NotebookRecord, bool, error) {
	key := strings.TrimSpace(pathOrID)
	if key == "" {
		return NotebookRecord{}, false, nil
	}
	appDir := resolveAppDataDir(appDataDir)
	cleanKey := normalizeNotebookPath(key)
	now := time.Now().Unix()

	notebooksMu.Lock()
	defer notebooksMu.Unlock()

	records, loadErr := loadNotebooksLocked(appDir)
	if loadErr != nil {
		return NotebookRecord{}, false, loadErr
	}
	idx := -1
	for i := range records {
		if records[i].ID == key {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i := range records {
			if records[i].IsActive() && sameNotebookPath(records[i].Path, cleanKey) {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return NotebookRecord{}, false, nil
	}
	if records[idx].RemovedAt == 0 {
		records[idx].RemovedAt = now
		if saveErr := saveNotebooksLocked(appDir, records); saveErr != nil {
			return NotebookRecord{}, false, saveErr
		}
	}
	return cloneNotebookRecord(records[idx]), true, nil
}

func readDirEntryNames(dir *os.Root) ([]string, error) {
	f, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(0)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, ent := range entries {
		name := ent.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

func removeDirRecursiveSafe(parent *os.Root, name string, depth int) {
	if parent == nil || name == "" || name == "." || name == ".." || depth > 64 {
		return
	}
	info, err := parent.Lstat(name)
	if err != nil {
		return
	}
	if isSymlinkOrReparseInfo(info) {
		return
	}
	if !info.IsDir() {
		if info.Mode().IsRegular() {
			_ = parent.Remove(name)
		}
		return
	}
	sub, err := parent.OpenRoot(name)
	if err != nil {
		return
	}
	names, err := readDirEntryNames(sub)
	if err == nil {
		for _, child := range names {
			removeDirRecursiveSafe(sub, child, depth+1)
		}
	}
	_ = sub.Close()
	_ = parent.Remove(name)
}

func removeRelativeSubtreeInRoot(root *os.Root, segments ...string) {
	if root == nil || len(segments) == 0 {
		return
	}
	opened := make([]*os.Root, 0, len(segments)-1)
	defer func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = opened[i].Close()
		}
	}()
	cur := root
	for _, seg := range segments[:len(segments)-1] {
		if seg == "" || seg == "." || seg == ".." {
			return
		}
		info, err := cur.Lstat(seg)
		if err != nil || isSymlinkOrReparseInfo(info) || !info.IsDir() {
			return
		}
		next, err := cur.OpenRoot(seg)
		if err != nil {
			return
		}
		opened = append(opened, next)
		cur = next
	}
	target := segments[len(segments)-1]
	removeDirRecursiveSafe(cur, target, 0)
}

func cleanupNotebookDataUnlocked(appDir, id string) {
	appRoot, err := os.OpenRoot(appDir)
	if err != nil {
		return
	}
	defer appRoot.Close()

	replacedStashMu.Lock()
	removeRelativeSubtreeInRoot(appRoot, "replaced", id)
	replacedStashMu.Unlock()

	appsInfo, err := appRoot.Lstat("apps-data")
	if err != nil || isSymlinkOrReparseInfo(appsInfo) || !appsInfo.IsDir() {
		return
	}
	appsRoot, err := appRoot.OpenRoot("apps-data")
	if err != nil {
		return
	}
	defer appsRoot.Close()
	names, err := readDirEntryNames(appsRoot)
	if err != nil {
		return
	}
	for _, pluginDir := range names {
		pInfo, pErr := appsRoot.Lstat(pluginDir)
		if pErr != nil || isSymlinkOrReparseInfo(pInfo) || !pInfo.IsDir() {
			continue
		}
		removeRelativeSubtreeInRoot(appsRoot, pluginDir, "notebooks", id)
	}
}

// CleanupNotebookDataIn safely deletes <appDataDir>/replaced/<id> and
// <appDataDir>/apps-data/*/notebooks/<id> inside opened private os.Roots without
// following symlinks or Windows junctions, aborting if id was re-activated.
func CleanupNotebookDataIn(appDataDir, id string) {
	if !IsSafeNotebookID(id) {
		return
	}
	appDir := resolveAppDataDir(appDataDir)
	if appDir == "" {
		return
	}
	notebooksMu.Lock()
	records, loadErr := loadNotebooksLocked(appDir)
	if loadErr != nil {
		notebooksMu.Unlock()
		return
	}
	for _, r := range records {
		if r.ID == id && r.IsActive() && r.MissingSince == 0 {
			notebooksMu.Unlock()
			return
		}
	}
	if IsNotebookCleaningIn(appDir, id) {
		notebooksMu.Unlock()
		return
	}
	setNotebookCleaning(appDir, id, true)
	notebooksMu.Unlock()
	defer setNotebookCleaning(appDir, id, false)

	cleanupNotebookDataUnlocked(appDir, id)
}

// ResolveNotebookIn finds an active registered notebook by ID or path.
// It reads from the published snapshot first and only falls back to ensuring the
// single matching entry from knownPaths when absent from the snapshot.
func ResolveNotebookIn(appDataDir, selector string, knownPaths []string) (NotebookRecord, bool) {
	sel := strings.TrimSpace(selector)
	if sel == "" {
		return NotebookRecord{}, false
	}
	appDir := resolveAppDataDir(appDataDir)
	snap := SnapshotNotebooksIn(appDir)

	allowed := func(p string) bool {
		if len(knownPaths) == 0 {
			return true
		}
		for _, kp := range knownPaths {
			if sameNotebookPath(kp, p) {
				return true
			}
		}
		return false
	}

	for _, r := range snap {
		if (r.ID == sel || sameNotebookPath(r.Path, sel)) && allowed(r.Path) {
			return r, true
		}
	}

	if len(knownPaths) > 0 {
		for _, kp := range knownPaths {
			if !sameNotebookPath(kp, sel) {
				continue
			}
			// Only register the single matching unknown path, never the entire list.
			rec, _, err := EnsureNotebookIn(appDir, kp)
			if err == nil && rec.IsActive() && !IsNotebookCleaningIn(appDir, rec.ID) {
				return rec, true
			}
			break
		}
	}
	return NotebookRecord{}, false
}

// NotebookIDForPathIn returns the registered active ID for rawPath from the
// published snapshot without minting one or blocking on disk.
func NotebookIDForPathIn(appDataDir, rawPath string) string {
	if rec, ok := LookupSnapshotNotebookIn(appDataDir, rawPath); ok {
		return rec.ID
	}
	return ""
}

// LookupSnapshotNotebookIn returns the active, non-cleaning notebook record matching
// rawPath from the published snapshot without acquiring notebooksMu or probing disks.
func LookupSnapshotNotebookIn(appDataDir, rawPath string) (NotebookRecord, bool) {
	clean := normalizeNotebookPath(rawPath)
	if clean == "" {
		return NotebookRecord{}, false
	}
	for _, r := range SnapshotNotebooksIn(appDataDir) {
		if sameNotebookPath(r.Path, clean) {
			return r, true
		}
	}
	return NotebookRecord{}, false
}

// NotebookIDForDescendantIn finds the registered active ID of the deepest notebook
// whose root equals or contains dirPath, reading the published snapshot without locking.
func NotebookIDForDescendantIn(appDataDir, dirPath string) string {
	clean := normalizeNotebookPath(dirPath)
	if clean == "" {
		return ""
	}
	canon := canonicalNotebookPath(clean)
	records := SnapshotNotebooksIn(appDataDir)

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

type sweepProbeOutcome struct {
	id                 string
	snapshotPath       string
	snapshotRemovedAt  int64
	snapshotSuperseded bool
	aliveAtPath        bool
	dev                uint64
	ino                uint64
	hasDevIno          bool
	locator            string
	needsXattr         bool
	recoveredElsewhere bool
	missingConfirmed   bool
}

func probeRecordForSweep(ctx context.Context, r NotebookRecord) sweepProbeOutcome {
	out := sweepProbeOutcome{
		id:                 r.ID,
		snapshotPath:       r.Path,
		snapshotRemovedAt:  r.RemovedAt,
		snapshotSuperseded: r.Superseded,
	}
	if r.RemovedAt > 0 {
		return out
	}
	if r.Superseded {
		chk := probeOldPathMissing(ctx, r.Path, r.ID, r.Device, r.Locator, nil)
		if chk.foundLivePath != "" {
			out.recoveredElsewhere = true
		} else if chk.confirmedMissing {
			out.missingConfirmed = true
		} else {
			// Original path is occupied by another directory (not ENOENT); verify parent volume accessibility.
			pRes, pOK := runProbeWithTimeout(ctx, notebookProbeTimeout, func() bool {
				parent := filepath.Dir(r.Path)
				if parent == "" || parent == r.Path {
					return false
				}
				pInfo, pErr := os.Stat(parent)
				if pErr != nil || !pInfo.IsDir() {
					return false
				}
				pDev, _, hasPDev := statDirDevIno(parent, pInfo)
				return isParentVolumeAccessible(parent, pDev, hasPDev, r.Device, r.Locator)
			})
			if pOK && pRes {
				out.missingConfirmed = true
			}
		}
		return out
	}

	p := probeDirIdentity(ctx, r.Path)
	if p.statErr == nil && p.info != nil && p.info.IsDir() && !IsTrashPath(r.Path) {
		sameDevIno := r.Inode == 0 || (p.hasDevIno && p.ino != 0 && r.Device == p.dev && r.Inode == p.ino)
		sameXattr := !supportsNotebookXattr() || (p.xattrID != "" && p.xattrID == r.ID) || (p.xattrID == "" && r.Inode == 0)
		if sameDevIno && sameXattr {
			out.aliveAtPath = true
			out.dev = p.dev
			out.ino = p.ino
			out.hasDevIno = p.hasDevIno
			out.locator = p.locator
			out.needsXattr = supportsNotebookXattr() && p.xattrID != r.ID
			return out
		}
		// Directory at r.Path has a different identity than r (replaced externally).
		// Check whether r's actual directory moved elsewhere before counting missing.
		locAlive, _ := runProbeWithTimeout(ctx, notebookProbeTimeout, func() bool {
			if r.Locator != "" {
				if lp, ok := ResolveDirLocator(r.Path, r.Locator); ok && lp != "" && !IsTrashPath(lp) {
					if li, err := os.Stat(lp); err == nil && li.IsDir() && !os.SameFile(li, p.info) {
						if !supportsNotebookXattr() || readNotebookXattr(lp) == r.ID {
							return true
						}
					}
				}
			}
			if supportsNotebookXattr() {
				if sib, ok := findSiblingByNotebookXattr(filepath.Dir(r.Path), r.ID); ok && !sameNotebookPath(sib, r.Path) {
					return true
				}
			}
			return false
		})
		if locAlive {
			out.recoveredElsewhere = true
			return out
		}
		pAcc, ok := runProbeWithTimeout(ctx, notebookProbeTimeout, func() bool {
			parent := filepath.Dir(r.Path)
			pInfo, pErr := os.Stat(parent)
			if pErr != nil || !pInfo.IsDir() {
				return false
			}
			pDev, _, hasPDev := statDirDevIno(parent, pInfo)
			return isParentVolumeAccessible(parent, pDev, hasPDev, r.Device, r.Locator)
		})
		if ok && pAcc {
			out.missingConfirmed = true
		}
		return out
	}

	if errors.Is(p.statErr, errNotebookProbeTimeout) {
		return out
	}
	chk := probeOldPathMissing(ctx, r.Path, r.ID, r.Device, r.Locator, nil)
	if chk.foundLivePath != "" {
		out.recoveredElsewhere = true
		return out
	}
	if chk.confirmedMissing {
		out.missingConfirmed = true
	}
	return out
}

// SweepNotebookRegistryIn refreshes metadata for present notebooks, maintains
// persistent missing_since/removed_at timers without holding notebooksMu during
// disk probes, and safely purges entries missing or removed for >= 30 days.
func SweepNotebookRegistryIn(appDataDir string) ([]NotebookRemoved, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return SweepNotebookRegistryContext(ctx, appDataDir)
}

// SweepNotebookRegistryContext is SweepNotebookRegistryIn bounded by ctx.
func SweepNotebookRegistryContext(ctx context.Context, appDataDir string) ([]NotebookRemoved, error) {
	appDir := resolveAppDataDir(appDataDir)
	if appDir == "" {
		return nil, nil
	}
	now := time.Now().Unix()
	expirySec := int64(notebookMissingExpiry.Seconds())

	notebooksMu.Lock()
	records, err := loadNotebooksLocked(appDir)
	if err != nil {
		notebooksMu.Unlock()
		return nil, err
	}
	snapshot := cloneNotebookRecords(records)
	notebooksMu.Unlock()

	if len(snapshot) == 0 {
		return nil, nil
	}

	// Probe outside notebooksMu using bounded workers and timeouts.
	probes := make([]sweepProbeOutcome, len(snapshot))
	workers := min(notebookProbeWorkers, len(snapshot))
	jobs := make(chan int, len(snapshot))
	for i := range snapshot {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if ctx != nil && ctx.Err() != nil {
					continue
				}
				probes[idx] = probeRecordForSweep(ctx, snapshot[idx])
			}
		}()
	}
	wg.Wait()

	probeByID := make(map[string]sweepProbeOutcome, len(snapshot))
	for _, pr := range probes {
		if pr.id != "" {
			probeByID[pr.id] = pr
		}
	}

	// Re-acquire lock and reload latest state so concurrent registrations/removals win.
	notebooksMu.Lock()
	latest, loadErr := loadNotebooksLocked(appDir)
	if loadErr != nil {
		notebooksMu.Unlock()
		return nil, loadErr
	}

	dirty := false
	seenDelta := false
	kept := make([]NotebookRecord, 0, len(latest))
	var expired []NotebookRemoved
	type xattrFix struct {
		path string
		id   string
	}
	var xattrFixes []xattrFix

	for _, r := range latest {
		if IsNotebookCleaningIn(appDir, r.ID) {
			kept = append(kept, r)
			continue
		}
		if r.RemovedAt > 0 {
			if (now - r.RemovedAt) >= expirySec {
				expired = append(expired, NotebookRemoved{ID: r.ID, Path: r.Path})
				dirty = true
				continue
			}
			kept = append(kept, r)
			continue
		}

		pr, hasProbe := probeByID[r.ID]
		if !hasProbe || pr.snapshotPath != r.Path || pr.snapshotRemovedAt != r.RemovedAt || pr.snapshotSuperseded != r.Superseded {
			kept = append(kept, r)
			continue
		}
		if pr.aliveAtPath {
			if r.MissingSince != 0 {
				r.MissingSince = 0
				dirty = true
			}
			if pr.hasDevIno && pr.ino != 0 && (r.Device != pr.dev || r.Inode != pr.ino) {
				r.Device = pr.dev
				r.Inode = pr.ino
				dirty = true
			}
			if pr.locator != "" && r.Locator != pr.locator {
				r.Locator = pr.locator
				dirty = true
			}
			if r.LastSeenAt == 0 || (now-r.LastSeenAt) >= sweepLastSeenStep {
				r.LastSeenAt = now
				seenDelta = true
			}
			if pr.needsXattr {
				xattrFixes = append(xattrFixes, xattrFix{path: r.Path, id: r.ID})
			}
			kept = append(kept, r)
			continue
		}
		if pr.recoveredElsewhere {
			if r.MissingSince != 0 {
				r.MissingSince = 0
				dirty = true
			}
			kept = append(kept, r)
			continue
		}
		if pr.missingConfirmed {
			if r.MissingSince <= 0 {
				r.MissingSince = now
				dirty = true
				kept = append(kept, r)
				continue
			}
			if (now - r.MissingSince) >= expirySec {
				expired = append(expired, NotebookRemoved{ID: r.ID, Path: r.Path})
				dirty = true
				continue
			}
		}
		kept = append(kept, r)
	}

	if dirty || seenDelta {
		for _, rem := range expired {
			setNotebookCleaning(appDir, rem.ID, true)
		}
		if sErr := saveNotebooksLocked(appDir, kept); sErr != nil {
			for _, rem := range expired {
				setNotebookCleaning(appDir, rem.ID, false)
			}
			notebooksMu.Unlock()
			return nil, sErr
		}
	}
	notebooksMu.Unlock()

	for _, fix := range xattrFixes {
		writeNotebookXattr(fix.path, fix.id)
	}
	for _, rem := range expired {
		cleanupNotebookDataUnlocked(appDir, rem.ID)
		setNotebookCleaning(appDir, rem.ID, false)
	}
	return expired, nil
}
