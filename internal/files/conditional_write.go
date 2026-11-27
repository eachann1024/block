package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"block/internal/theme"
)

type pathLockEntry struct {
	mu   sync.Mutex
	refs int
}

var (
	conditionalPathLocksMu sync.Mutex
	conditionalPathLocks   = make(map[string]*pathLockEntry)
)

// lockConditionalWritePath serializes in-process conditional writes to the same
// canonical file path so concurrent RENAME_SWAP rollbacks inside the process
// cannot ping-pong against each other.
func lockConditionalWritePath(parentDir, base string) func() {
	cleanParent := filepath.Clean(parentDir)
	if resolved, err := filepath.EvalSymlinks(cleanParent); err == nil && resolved != "" {
		cleanParent = resolved
	}
	key := filepath.Join(cleanParent, base)
	conditionalPathLocksMu.Lock()
	entry := conditionalPathLocks[key]
	if entry == nil {
		entry = &pathLockEntry{}
		conditionalPathLocks[key] = entry
	}
	entry.refs++
	conditionalPathLocksMu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		conditionalPathLocksMu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(conditionalPathLocks, key)
		}
		conditionalPathLocksMu.Unlock()
	}
}

func conditionalSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

var errNotRegularFile = errors.New(theme.Tr("目标不是常规文件", "Target is not a regular file"))

func readAllInDir(dir *os.Root, name string) ([]byte, error) {
	info, err := dir.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegularFile
	}
	f, err := dir.OpenFile(name, os.O_RDONLY|oNonBlockFlag, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errNotRegularFile
	}
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

const (
	replacedMaxEntries      = 64
	replacedRetention       = 7 * 24 * time.Hour
	externalReplacedBucket  = "_external"
	legacyWalkMaxDepth      = 8
	legacyWalkMaxDirs       = 512
	legacyWalkMaxFiles      = 512
	legacyWalkTimeout       = 15 * time.Second
	pendingStashMaxEntries  = 256
	pendingStashRetryMax    = 24
	orphanTempRecoverMinAge = 10 * time.Minute
)

var (
	replacedStashMu sync.Mutex

	activeConditionalTempsMu sync.Mutex
	activeConditionalTemps   = make(map[string]int)

	pendingStashesMu sync.Mutex
	pendingStashes   = make(map[string]pendingStashEntry)

	legacyReplacedTSRe  = regexp.MustCompile(`^\d{15}-[0-9a-f]{8}-.+`)
	legacyReplacedBakRe = regexp.MustCompile(`^.+\.[0-9a-f]{6,}\.bak(?:-\d+)?$`)
	blockStashTmpRe     = regexp.MustCompile(`^\.block-(?:(?:old|tmp)-)?\d+-\d+\.tmp$`)

	errSymlinkOrReparseBlocked = errors.New("refusing to traverse or modify symlink or reparse point")
)

type activeTempLease struct {
	key      string
	released bool
	mu       sync.Mutex
}

func conditionalTempDirKey(dir *os.Root) string {
	if dir == nil {
		return ""
	}
	live := liveRootDirPath(dir)
	if live == "" {
		live = dir.Name()
	}
	if st, err := dir.Stat("."); err == nil {
		if dev, ino, ok := statDirDevIno(live, st); ok && ino != 0 {
			return fmt.Sprintf("d:%d:%d", dev, ino)
		}
	}
	clean := filepath.Clean(live)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != "" {
		clean = filepath.Clean(resolved)
	}
	if runtime.GOOS == "windows" {
		clean = strings.ToLower(clean)
	}
	return "p:" + clean
}

func conditionalTempKey(dir *os.Root, name string) string {
	dk := conditionalTempDirKey(dir)
	if dk == "" || name == "" {
		return ""
	}
	return dk + "\x00" + name
}

func acquireActiveConditionalTemp(dir *os.Root, name string) *activeTempLease {
	key := conditionalTempKey(dir, name)
	if key == "" {
		return &activeTempLease{released: true}
	}
	activeConditionalTempsMu.Lock()
	activeConditionalTemps[key]++
	activeConditionalTempsMu.Unlock()
	return &activeTempLease{key: key}
}

func (l *activeTempLease) release() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if l.released || l.key == "" {
		l.released = true
		l.mu.Unlock()
		return
	}
	l.released = true
	key := l.key
	l.mu.Unlock()

	activeConditionalTempsMu.Lock()
	if n := activeConditionalTemps[key]; n <= 1 {
		delete(activeConditionalTemps, key)
	} else {
		activeConditionalTemps[key] = n - 1
	}
	activeConditionalTempsMu.Unlock()
}

func (l *activeTempLease) isHeld() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	held := !l.released && l.key != ""
	l.mu.Unlock()
	return held
}

func isActiveConditionalTemp(dir *os.Root, name string) bool {
	key := conditionalTempKey(dir, name)
	if key == "" {
		return false
	}
	activeConditionalTempsMu.Lock()
	n := activeConditionalTemps[key]
	activeConditionalTempsMu.Unlock()
	return n > 0
}

func removeConditionalTempIfUnused(dir *os.Root, name string, lease *activeTempLease) bool {
	if dir == nil || name == "" {
		return false
	}
	key := conditionalTempKey(dir, name)
	expectedRefs := 0
	var leaseHeld bool
	if lease != nil {
		lease.mu.Lock()
		leaseHeld = !lease.released && lease.key != ""
		if leaseHeld {
			key = lease.key
			expectedRefs = 1
		}
		lease.mu.Unlock()
	}
	activeConditionalTempsMu.Lock()
	cur := 0
	if key != "" {
		cur = activeConditionalTemps[key]
	}
	if cur > expectedRefs {
		activeConditionalTempsMu.Unlock()
		if lease != nil {
			lease.release()
		}
		return false
	}
	err := dir.Remove(name)
	activeConditionalTempsMu.Unlock()
	if lease != nil {
		lease.release()
	}
	return err == nil || errors.Is(err, os.ErrNotExist)
}

func touchFileMtimeNow(dir *os.Root, name string) {
	if dir == nil || name == "" {
		return
	}
	info, err := dir.Lstat(name)
	if err != nil || isSymlinkOrReparseInfo(info) || !info.Mode().IsRegular() {
		return
	}
	liveDir := liveRootDirPath(dir)
	if liveDir == "" {
		return
	}
	now := time.Now()
	_ = os.Chtimes(filepath.Join(liveDir, name), now, now)
}

type pendingStashEntry struct {
	appDataDir    string
	dirPath       string
	tmpName       string
	rootHint      string
	relOrBaseName string
	attempts      int
	queuedAt      time.Time
}

func isBlockStashTempName(name string) bool {
	return blockStashTmpRe.MatchString(name)
}

func isLegacyReplacedFileName(name string) bool {
	return legacyReplacedTSRe.MatchString(name) || legacyReplacedBakRe.MatchString(name)
}

func isLegacyWalkSkippedDir(name string) bool {
	if name == "" || name == "." || name == ".." {
		return true
	}
	if strings.HasPrefix(name, ".") || isCommonIgnoredName(name) {
		return true
	}
	switch strings.ToLower(name) {
	case "node_modules", "__pycache__", "venv", ".venv", "dist", "build", "target":
		return true
	default:
		return false
	}
}

func enqueuePendingStash(appDataDir, dirPath, tmpName, rootHint, relOrBaseName string, prevAttempts int, queuedAt time.Time) {
	if dirPath == "" || tmpName == "" || !isBlockStashTempName(tmpName) {
		return
	}
	cleanDir := filepath.Clean(dirPath)
	key := cleanDir + "\x00" + tmpName
	base := resolveAppDataDir(appDataDir)
	now := queuedAt
	if now.IsZero() {
		now = time.Now()
	}
	pendingStashesMu.Lock()
	defer pendingStashesMu.Unlock()
	if cur, exists := pendingStashes[key]; exists {
		cur.attempts = max(cur.attempts, prevAttempts)
		pendingStashes[key] = cur
		return
	}
	if len(pendingStashes) >= pendingStashMaxEntries {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range pendingStashes {
			if oldestKey == "" || v.queuedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.queuedAt
			}
		}
		if oldestKey != "" {
			delete(pendingStashes, oldestKey)
		}
	}
	pendingStashes[key] = pendingStashEntry{
		appDataDir:    base,
		dirPath:       cleanDir,
		tmpName:       tmpName,
		rootHint:      rootHint,
		relOrBaseName: relOrBaseName,
		attempts:      prevAttempts,
		queuedAt:      now,
	}
}

func retryPendingStashes(appDataDir string) {
	base := resolveAppDataDir(appDataDir)
	pendingStashesMu.Lock()
	if len(pendingStashes) == 0 {
		pendingStashesMu.Unlock()
		return
	}
	batch := make([]pendingStashEntry, 0, len(pendingStashes))
	for k, ent := range pendingStashes {
		if ent.appDataDir == base || base == "" {
			batch = append(batch, ent)
			delete(pendingStashes, k)
		}
	}
	pendingStashesMu.Unlock()

	now := time.Now()
	for _, ent := range batch {
		if ent.attempts >= pendingStashRetryMax || (!ent.queuedAt.IsZero() && now.Sub(ent.queuedAt) > replacedRetention) {
			continue
		}
		dirRoot, err := openDirNoSymlinkAbs(ent.dirPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errSymlinkOrReparseBlocked) {
				enqueuePendingStash(ent.appDataDir, ent.dirPath, ent.tmpName, ent.rootHint, ent.relOrBaseName, ent.attempts+1, ent.queuedAt)
			}
			continue
		}
		if isActiveConditionalTemp(dirRoot, ent.tmpName) {
			_ = dirRoot.Close()
			continue
		}
		info, lErr := dirRoot.Lstat(ent.tmpName)
		if lErr != nil {
			_ = dirRoot.Close()
			if !errors.Is(lErr, os.ErrNotExist) {
				enqueuePendingStash(ent.appDataDir, ent.dirPath, ent.tmpName, ent.rootHint, ent.relOrBaseName, ent.attempts+1, ent.queuedAt)
			}
			continue
		}
		if isSymlinkOrReparseInfo(info) || !info.Mode().IsRegular() || !isBlockStashTempName(ent.tmpName) {
			_ = dirRoot.Close()
			continue
		}
		ok := stashReplacedInodeWithRetry(dirRoot, ent.rootHint, ent.tmpName, ent.relOrBaseName, ent.attempts+1, ent.queuedAt)
		_ = dirRoot.Close()
		_ = ok
	}
}

func replacedBucketName(appDataDir, rootHint string, dir *os.Root) string {
	base := resolveAppDataDir(appDataDir)
	nbID := ""
	if rootHint != "" {
		nbID = NotebookIDForPathIn(base, rootHint)
		if nbID == "" {
			nbID = NotebookIDForDescendantIn(base, rootHint)
		}
	}
	if nbID == "" && dir != nil {
		if liveDir := liveRootDirPath(dir); liveDir != "" {
			nbID = NotebookIDForDescendantIn(base, liveDir)
		}
	}
	if !IsSafeNotebookID(nbID) {
		nbID = externalReplacedBucket
	}
	return nbID
}

func openDirNoSymlinkAbs(absDir string) (*os.Root, error) {
	clean := filepath.Clean(absDir)
	if clean == "" || clean == "." {
		return nil, os.ErrInvalid
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if isSymlinkOrReparseInfo(info) || !info.IsDir() {
		return nil, errSymlinkOrReparseBlocked
	}
	root, err := os.OpenRoot(clean)
	if err != nil {
		return nil, err
	}
	rInfo, sErr := root.Stat(".")
	if sErr != nil || !os.SameFile(info, rInfo) {
		_ = root.Close()
		return nil, os.ErrInvalid
	}
	return root, nil
}

func openChildDirNoSymlink(parent *os.Root, seg string) (*os.Root, error) {
	if parent == nil || seg == "" || seg == "." || seg == ".." {
		return nil, os.ErrInvalid
	}
	info, err := parent.Lstat(seg)
	if err != nil {
		return nil, err
	}
	if isSymlinkOrReparseInfo(info) || !info.IsDir() {
		return nil, errSymlinkOrReparseBlocked
	}
	sub, err := parent.OpenRoot(seg)
	if err != nil {
		return nil, err
	}
	subInfo, sErr := sub.Stat(".")
	if sErr != nil || !os.SameFile(info, subInfo) {
		_ = sub.Close()
		return nil, os.ErrInvalid
	}
	return sub, nil
}

func ensureChildDirNoSymlink(parent *os.Root, seg string, perm os.FileMode) (*os.Root, error) {
	if parent == nil || seg == "" || seg == "." || seg == ".." {
		return nil, os.ErrInvalid
	}
	if _, err := parent.Lstat(seg); errors.Is(err, os.ErrNotExist) {
		if mkErr := parent.Mkdir(seg, perm); mkErr != nil && !errors.Is(mkErr, os.ErrExist) {
			return nil, mkErr
		}
	}
	return openChildDirNoSymlink(parent, seg)
}

func openRelDirNoSymlink(root *os.Root, rel string) (*os.Root, error) {
	if root == nil {
		return nil, os.ErrInvalid
	}
	cleanRel := filepath.ToSlash(filepath.Clean(rel))
	if cleanRel == "." || cleanRel == "" {
		return nil, os.ErrInvalid
	}
	segs := strings.Split(cleanRel, "/")
	cur := root
	var opened []*os.Root
	closeOpened := func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = opened[i].Close()
		}
	}
	for _, seg := range segs {
		next, err := openChildDirNoSymlink(cur, seg)
		if err != nil {
			closeOpened()
			return nil, err
		}
		opened = append(opened, next)
		cur = next
	}
	for i := 0; i < len(opened)-1; i++ {
		_ = opened[i].Close()
	}
	return cur, nil
}

func openReplacedBucketRootsLocked(base, bucketName string, create bool) (appRoot, replacedRoot, bucketRoot *os.Root, err error) {
	baseClean := resolveAppDataDir(base)
	if baseClean == "" || bucketName == "" || bucketName == "." || bucketName == ".." {
		return nil, nil, nil, os.ErrInvalid
	}
	appRoot, err = openDirNoSymlinkAbs(baseClean)
	if err != nil {
		if create && errors.Is(err, os.ErrNotExist) {
			if mkErr := os.MkdirAll(baseClean, 0o700); mkErr != nil {
				return nil, nil, nil, mkErr
			}
			appRoot, err = openDirNoSymlinkAbs(baseClean)
		}
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if create {
		replacedRoot, err = ensureChildDirNoSymlink(appRoot, "replaced", 0o700)
	} else {
		replacedRoot, err = openChildDirNoSymlink(appRoot, "replaced")
	}
	if err != nil {
		_ = appRoot.Close()
		return nil, nil, nil, err
	}
	if create {
		bucketRoot, err = ensureChildDirNoSymlink(replacedRoot, bucketName, 0o700)
	} else {
		bucketRoot, err = openChildDirNoSymlink(replacedRoot, bucketName)
	}
	if err != nil {
		_ = replacedRoot.Close()
		_ = appRoot.Close()
		return nil, nil, nil, err
	}
	return appRoot, replacedRoot, bucketRoot, nil
}

func replacedEntryTimestamp(name string, info os.FileInfo) time.Time {
	if tsStr, _, ok := strings.Cut(name, "-"); ok && tsStr != "" {
		if tsVal, err := strconv.ParseInt(tsStr, 10, 64); err == nil && tsVal >= 1_000_000_000_000 {
			return time.UnixMilli(tsVal)
		}
	}
	if info != nil {
		return info.ModTime()
	}
	return time.Time{}
}

func pruneReplacedDirInRootLocked(replacedRoot, bucketRoot *os.Root, bucketName, keepName string, now time.Time, removeIfEmpty bool) bool {
	if bucketRoot == nil {
		return false
	}
	names, err := readDirEntryNames(bucketRoot)
	if err != nil {
		return false
	}
	type candidate struct {
		name string
		ts   time.Time
	}
	expireBefore := now.Add(-replacedRetention)
	kept := make([]candidate, 0, len(names))
	for _, name := range names {
		info, iErr := bucketRoot.Lstat(name)
		if iErr != nil || isSymlinkOrReparseInfo(info) || info.IsDir() {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		ts := replacedEntryTimestamp(name, info)
		if name != keepName && !ts.IsZero() && ts.Before(expireBefore) {
			_ = bucketRoot.Remove(name)
			continue
		}
		kept = append(kept, candidate{name: name, ts: ts})
	}
	if len(kept) > replacedMaxEntries {
		sort.Slice(kept, func(i, j int) bool {
			if !kept[i].ts.Equal(kept[j].ts) {
				return kept[i].ts.Before(kept[j].ts)
			}
			return kept[i].name < kept[j].name
		})
		excess := len(kept) - replacedMaxEntries
		for i := 0; i < excess; i++ {
			if kept[i].name == keepName {
				continue
			}
			_ = bucketRoot.Remove(kept[i].name)
		}
	}
	if removeIfEmpty && keepName == "" && replacedRoot != nil && bucketName != "" {
		if rem, rErr := readDirEntryNames(bucketRoot); rErr == nil && len(rem) == 0 {
			_ = bucketRoot.Close()
			_ = replacedRoot.Remove(bucketName)
			return true
		}
	}
	return false
}

// CleanupReplacedDirs prunes expired (>7 days) and excess (>64 per notebook)
// stashed inodes inside AppDataDir()/replaced/ and retries any pending archives.
func CleanupReplacedDirs() {
	CleanupReplacedDirsIn(AppDataDir())
}

// CleanupReplacedDirsIn is CleanupReplacedDirs scoped to appDataDir.
func CleanupReplacedDirsIn(appDataDir string) {
	base := resolveAppDataDir(appDataDir)
	retryPendingStashes(base)

	replacedStashMu.Lock()
	defer replacedStashMu.Unlock()

	appRoot, err := openDirNoSymlinkAbs(base)
	if err != nil {
		return
	}
	defer appRoot.Close()

	replacedRoot, err := openChildDirNoSymlink(appRoot, "replaced")
	if err != nil {
		return
	}
	defer replacedRoot.Close()

	names, err := readDirEntryNames(replacedRoot)
	if err != nil {
		return
	}
	now := time.Now()
	for _, bucketName := range names {
		bucketRoot, bErr := openChildDirNoSymlink(replacedRoot, bucketName)
		if bErr != nil {
			continue
		}
		closed := pruneReplacedDirInRootLocked(replacedRoot, bucketRoot, bucketName, "", now, true)
		if !closed {
			_ = bucketRoot.Close()
		}
	}
}

// MigrateLegacyReplacedDir silently moves any legacy regular files from
// <nbRoot>/.block-replaced/ (and bounded subdirs) into <AppDataDir()>/replaced/<notebookID>/
// and deletes empty legacy directories without touching unrecognized files.
func MigrateLegacyReplacedDir(nbRoot string) {
	MigrateLegacyReplacedDirIn(AppDataDir(), nbRoot)
}

// MigrateLegacyReplacedDirIn is MigrateLegacyReplacedDir scoped to appDataDir.
func MigrateLegacyReplacedDirIn(appDataDir, nbRoot string) {
	ctx, cancel := context.WithTimeout(context.Background(), legacyWalkTimeout)
	defer cancel()
	MigrateLegacyReplacedDirContext(ctx, appDataDir, nbRoot)
}

// MigrateLegacyReplacedDirContext runs bounded legacy .block-replaced migration under ctx.
func MigrateLegacyReplacedDirContext(ctx context.Context, appDataDir, nbRoot string) {
	cleanRoot := normalizeNotebookPath(nbRoot)
	if cleanRoot == "" {
		return
	}
	root, err := openDirNoSymlinkAbs(cleanRoot)
	if err != nil {
		return
	}
	defer root.Close()

	base := resolveAppDataDir(appDataDir)
	nbID := replacedBucketName(base, cleanRoot, root)

	type dirQueueItem struct {
		rel   string
		depth int
	}
	queue := []dirQueueItem{{rel: ".", depth: 0}}
	visitedDirs := 0
	migratedFiles := 0
	touchedDst := false

	for len(queue) > 0 && visitedDirs < legacyWalkMaxDirs && migratedFiles < legacyWalkMaxFiles {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		cur := queue[0]
		queue = queue[1:]
		visitedDirs++

		var curRoot *os.Root
		var opened *os.Root
		if cur.rel == "." {
			curRoot = root
		} else {
			sub, sErr := openRelDirNoSymlink(root, cur.rel)
			if sErr != nil {
				continue
			}
			opened = sub
			curRoot = sub
		}

		// 1. Inspect <dir>/.block-replaced if present (without following symlinks/junctions).
		if linfo, lErr := curRoot.Lstat(".block-replaced"); lErr == nil && !isSymlinkOrReparseInfo(linfo) && linfo.IsDir() {
			if moved, ok := migrateOneLegacyReplacedDir(ctx, curRoot, base, nbID, legacyWalkMaxFiles-migratedFiles); moved > 0 {
				migratedFiles += moved
				if ok {
					touchedDst = true
				}
			}
		}

		// 2. Bounded scan of immediate subdirectories for nested legacy .block-replaced
		// or stale orphaned .block-*.tmp temp files left behind after crashes.
		names, rErr := readDirEntryNames(curRoot)
		if rErr == nil {
			now := time.Now()
			for _, name := range names {
				if ctx != nil && ctx.Err() != nil {
					break
				}
				if name == ".block-replaced" {
					continue
				}
				info, iErr := curRoot.Lstat(name)
				if iErr != nil || isSymlinkOrReparseInfo(info) {
					continue
				}
				if info.IsDir() {
					if cur.depth < legacyWalkMaxDepth && !isLegacyWalkSkippedDir(name) && len(queue)+visitedDirs < legacyWalkMaxDirs {
						nextRel := name
						if cur.rel != "." {
							nextRel = filepath.Join(cur.rel, name)
						}
						queue = append(queue, dirQueueItem{rel: nextRel, depth: cur.depth + 1})
					}
					continue
				}
				// Recover orphaned stash temp files only when NOT registered in activeConditionalTemps
				// AND unmodified for at least 10 minutes.
				if info.Mode().IsRegular() && isBlockStashTempName(name) &&
					!isActiveConditionalTemp(curRoot, name) &&
					now.Sub(info.ModTime()) >= orphanTempRecoverMinAge {
					if stashReplacedInode(curRoot, cleanRoot, name, name) {
						migratedFiles++
						touchedDst = true
					}
				}
			}
		}

		if opened != nil {
			_ = opened.Close()
		}
	}

	if touchedDst {
		replacedStashMu.Lock()
		if appR, repR, buckR, rErr := openReplacedBucketRootsLocked(base, nbID, false); rErr == nil {
			closed := pruneReplacedDirInRootLocked(repR, buckR, nbID, "", time.Now(), false)
			if !closed {
				_ = buckR.Close()
			}
			_ = repR.Close()
			_ = appR.Close()
		}
		replacedStashMu.Unlock()
	}
}

func migrateOneLegacyReplacedDir(ctx context.Context, parentRoot *os.Root, base, nbID string, budget int) (int, bool) {
	if parentRoot == nil || budget <= 0 {
		return 0, false
	}
	legacyRoot, err := openChildDirNoSymlink(parentRoot, ".block-replaced")
	if err != nil {
		return 0, false
	}
	defer legacyRoot.Close()

	names, err := readDirEntryNames(legacyRoot)
	if err != nil {
		return 0, false
	}
	movedCount := 0
	ensuredDst := false
	for _, name := range names {
		if movedCount >= budget || (ctx != nil && ctx.Err() != nil) {
			break
		}
		if !isLegacyReplacedFileName(name) {
			continue
		}
		info, lErr := legacyRoot.Lstat(name)
		if lErr != nil || isSymlinkOrReparseInfo(info) || !info.Mode().IsRegular() {
			continue
		}

		replacedStashMu.Lock()
		appRoot, replacedRoot, dstRoot, dErr := openReplacedBucketRootsLocked(base, nbID, true)
		if dErr != nil {
			replacedStashMu.Unlock()
			return movedCount, ensuredDst
		}
		ensuredDst = true
		dstName := name
		picked := false
		for idx := 0; idx < 50; idx++ {
			cand := name
			if idx > 0 {
				cand = fmt.Sprintf("%s-%d", name, idx)
			}
			if _, sErr := dstRoot.Lstat(cand); errors.Is(sErr, os.ErrNotExist) {
				dstName = cand
				picked = true
				break
			}
		}
		if !picked {
			_ = dstRoot.Close()
			_ = replacedRoot.Close()
			_ = appRoot.Close()
			replacedStashMu.Unlock()
			continue
		}
		renErr := renameFileAcrossRoots(legacyRoot, name, dstRoot, dstName)
		if renErr == nil {
			_ = dstRoot.Close()
			_ = replacedRoot.Close()
			_ = appRoot.Close()
			replacedStashMu.Unlock()
			movedCount++
			continue
		}
		if isCrossDeviceErr(renErr) {
			copied := copyCrossDevicePreservingMtime(legacyRoot, name, dstRoot, dstName, info.ModTime())
			_ = dstRoot.Close()
			_ = replacedRoot.Close()
			_ = appRoot.Close()
			replacedStashMu.Unlock()
			if copied {
				_ = legacyRoot.Remove(name)
				movedCount++
			}
			continue
		}
		_ = dstRoot.Close()
		_ = replacedRoot.Close()
		_ = appRoot.Close()
		replacedStashMu.Unlock()
	}

	if rem, rErr := readDirEntryNames(legacyRoot); rErr == nil && len(rem) == 0 {
		_ = legacyRoot.Close()
		_ = parentRoot.Remove(".block-replaced")
	}
	return movedCount, ensuredDst
}

func copyCrossDevicePreservingMtime(srcDir *os.Root, srcName string, dstRoot *os.Root, dstName string, mtime time.Time) bool {
	if srcDir == nil || dstRoot == nil || srcName == "" || dstName == "" {
		return false
	}
	src, err := srcDir.OpenFile(srcName, os.O_RDONLY|oNonBlockFlag, 0)
	if err != nil {
		return false
	}
	defer src.Close()
	var (
		tmpName string
		tmpFile *os.File
	)
	for range 50 {
		cand := fmt.Sprintf(".migrate-%d-%d.tmp", os.Getpid(), opsSeq.Add(1))
		f, oErr := dstRoot.OpenFile(cand, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if oErr == nil {
			tmpName = cand
			tmpFile = f
			break
		}
		if !errors.Is(oErr, os.ErrExist) {
			return false
		}
	}
	if tmpFile == nil {
		return false
	}
	_, cErr := io.Copy(tmpFile, src)
	sErr := tmpFile.Sync()
	clErr := tmpFile.Close()
	if cErr != nil || sErr != nil || clErr != nil {
		_ = dstRoot.Remove(tmpName)
		return false
	}
	if !mtime.IsZero() {
		if dstLive := liveRootDirPath(dstRoot); dstLive != "" {
			if st, stErr := dstRoot.Lstat(tmpName); stErr == nil && !isSymlinkOrReparseInfo(st) && st.Mode().IsRegular() {
				_ = os.Chtimes(filepath.Join(dstLive, tmpName), mtime, mtime)
			}
		}
	}
	if renErr := dstRoot.Rename(tmpName, dstName); renErr != nil {
		_ = dstRoot.Remove(tmpName)
		return false
	}
	return true
}

// stashReplacedInode moves a swapped-out or replaced old inode from dir/tmpPath
// into <AppDataDir()>/replaced/<notebookID>/ using same-volume handle rename so any
// external editor still holding an open fd keeps nlink >= 1 and exact inode identity.
func stashReplacedInode(dir *os.Root, rootHint, tmpPath, relOrBaseName string) bool {
	return stashReplacedInodeWithHandoff(dir, rootHint, tmpPath, relOrBaseName, 0, time.Time{}, nil)
}

func stashReplacedInodeWithRetry(dir *os.Root, rootHint, tmpPath, relOrBaseName string, attempts int, queuedAt time.Time) bool {
	return stashReplacedInodeWithHandoff(dir, rootHint, tmpPath, relOrBaseName, attempts, queuedAt, nil)
}

func stashReplacedInodeWithHandoff(dir *os.Root, rootHint, tmpPath, relOrBaseName string, attempts int, queuedAt time.Time, lease *activeTempLease) bool {
	if dir == nil || tmpPath == "" {
		lease.release()
		return false
	}
	// If called without an owner lease (e.g. background patrol / pending retry),
	// never touch a temp file currently registered by an in-progress conditional write.
	if lease == nil || !lease.isHeld() {
		if isActiveConditionalTemp(dir, tmpPath) {
			return false
		}
	}
	touchFileMtimeNow(dir, tmpPath)

	appDir := AppDataDir()
	liveDir := liveRootDirPath(dir)
	nbID := replacedBucketName(appDir, rootHint, dir)
	now := time.Now()
	nowMs := now.UnixMilli()
	shortHash := conditionalSHA256Hex([]byte(relOrBaseName))
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}
	baseTag := fmt.Sprintf("%015d-%s-%s", nowMs, shortHash, sanitizeStashBase(filepath.Base(relOrBaseName)))

	replacedStashMu.Lock()
	appRoot, replacedRoot, dstRoot, err := openReplacedBucketRootsLocked(appDir, nbID, true)
	if err != nil {
		replacedStashMu.Unlock()
		enqueuePendingStash(appDir, liveDir, tmpPath, rootHint, relOrBaseName, attempts, queuedAt)
		lease.release()
		return false
	}

	var chosenName string
	for i := range 50 {
		candName := baseTag
		if i > 0 {
			candName = fmt.Sprintf("%s-%d", baseTag, i)
		}
		if _, err := dstRoot.Lstat(candName); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			break
		}
		err := renameFileAcrossRoots(dir, tmpPath, dstRoot, candName)
		if err == nil {
			chosenName = candName
			break
		}
		if isCrossDeviceErr(err) {
			// Cross-volume (EXDEV): remove tmpPath ONLY after confirming no active writer/user holds it.
			_ = dstRoot.Close()
			_ = replacedRoot.Close()
			_ = appRoot.Close()
			replacedStashMu.Unlock()
			removeConditionalTempIfUnused(dir, tmpPath, lease)
			return false
		}
		_ = dstRoot.Close()
		_ = replacedRoot.Close()
		_ = appRoot.Close()
		replacedStashMu.Unlock()
		enqueuePendingStash(appDir, liveDir, tmpPath, rootHint, relOrBaseName, attempts, queuedAt)
		lease.release()
		return false
	}
	if chosenName == "" {
		_ = dstRoot.Close()
		_ = replacedRoot.Close()
		_ = appRoot.Close()
		replacedStashMu.Unlock()
		enqueuePendingStash(appDir, liveDir, tmpPath, rootHint, relOrBaseName, attempts, queuedAt)
		lease.release()
		return false
	}
	closed := pruneReplacedDirInRootLocked(replacedRoot, dstRoot, nbID, chosenName, now, false)
	if !closed {
		_ = dstRoot.Close()
	}
	_ = replacedRoot.Close()
	_ = appRoot.Close()
	replacedStashMu.Unlock()
	lease.release()
	return true
}

func probeHardLinkInDir(dir *os.Root, srcName string) bool {
	pid := os.Getpid()
	for range 20 {
		probe := fmt.Sprintf(".block-probe-%d-%d.tmp", pid, opsSeq.Add(1))
		err := dir.Link(srcName, probe)
		if err == nil {
			_ = dir.Remove(probe)
			return true
		}
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return false
	}
	return false
}

func allocPrivateTempName(prefix string) string {
	return fmt.Sprintf(".block-%s-%d-%d.tmp", prefix, os.Getpid(), opsSeq.Add(1))
}

func restoreMovedOldInDir(dir *os.Root, movedOld, base, hashHex string, discardOnExist bool) {
	if linkErr := dir.Link(movedOld, base); linkErr == nil {
		_ = dir.Remove(movedOld)
		return
	} else if errors.Is(linkErr, os.ErrExist) {
		if discardOnExist {
			_ = dir.Remove(movedOld)
		} else {
			preserveExternalConflictInDir(dir, movedOld, base, hashHex)
		}
		return
	}
	if renErr := dir.Rename(movedOld, base); renErr != nil && !discardOnExist {
		preserveExternalConflictInDir(dir, movedOld, base, hashHex)
	}
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
	unlockPath := lockConditionalWritePath(dir.Name(), base)
	defer unlockPath()
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
			return false, openErr
		}
	}
	if tmpFile == nil {
		return false, errors.New(theme.Tr("无法创建临时文件", "Cannot allocate temporary file"))
	}

	var ourTmpInfo os.FileInfo
	keepTmp := false
	defer func() {
		if !keepTmp && tmpPath != "" {
			// Only remove tmpPath if it still refers to our own original temp inode (never delete
			// a swapped-out external file or a second concurrent save!).
			if ourTmpInfo == nil || sameInodeInDir(dir, tmpPath, ourTmpInfo) {
				_ = dir.Remove(tmpPath)
			}
		}
		leaseTmp.release()
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
			if rErr != nil && !errors.Is(rErr, errNotRegularFile) {
				return false, rErr
			}
			return false, errConflict
		}
		// Preserve the swapped-out old inode in <AppDataDir()>/replaced/<notebookID>/
		// (same volume, exact inode) so late in-place writes remain recoverable on disk.
		stashReplacedInodeWithHandoff(dir, root.Name(), tmpPath, name, 0, time.Time{}, leaseTmp)
		keepTmp = true
		if file, oErr := dir.Open("."); oErr == nil {
			_ = file.Sync()
			_ = file.Close()
		}
		return true, nil
	}

	// Non-swap platform/filesystem fallback (e.g. HFS+):
	// 1. Verify hard-link support on tmpPath before touching base.
	// 2. Atomically move base aside to a unique private name so we inspect the exact inode.
	// 3. Publish tmpPath via dir.Link(tmpPath, base) (fails atomically with EEXIST if any
	//    external writer created base in the interim — zero silent clobbering!).
	// 4. Stash the verified old inode into <AppDataDir()>/replaced/ so open FDs keep nlink >= 1.
	if !probeHardLinkInDir(dir, tmpPath) {
		return false, errConflict
	}
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
	movedOld := ""
	var leaseOld *activeTempLease
	for range 50 {
		cand := allocPrivateTempName("old")
		if _, lErr := dir.Lstat(cand); lErr == nil {
			continue
		}
		candLease := acquireActiveConditionalTemp(dir, cand)
		if renErr := dir.Rename(base, cand); renErr != nil {
			candLease.release()
			if errors.Is(renErr, os.ErrNotExist) {
				return false, errConflict
			}
			return false, renErr
		}
		movedOld = cand
		leaseOld = candLease
		break
	}
	if movedOld == "" {
		return false, errConflict
	}
	oldBytes, rErr := readAllInDir(dir, movedOld)
	oldHash := ""
	if rErr == nil {
		oldHash = conditionalSHA256Hex(oldBytes)
	}
	if rErr != nil || oldHash != expected {
		sameContent := rErr == nil && oldHash == contentHash
		restoreMovedOldInDir(dir, movedOld, base, oldHash, sameContent)
		leaseOld.release()
		if sameContent {
			return false, nil
		}
		if rErr != nil && !errors.Is(rErr, errNotRegularFile) {
			return false, rErr
		}
		return false, errConflict
	}
	if pubErr := dir.Link(tmpPath, base); pubErr != nil {
		if errors.Is(pubErr, os.ErrExist) {
			// An external writer created base in the interim; leave their file untouched
			// and stash the prior inode so any open fd keeps nlink >= 1.
			stashReplacedInodeWithHandoff(dir, root.Name(), movedOld, name, 0, time.Time{}, leaseOld)
			return false, errConflict
		}
		restoreMovedOldInDir(dir, movedOld, base, oldHash, false)
		leaseOld.release()
		return false, pubErr
	}
	stashReplacedInodeWithHandoff(dir, root.Name(), movedOld, name, 0, time.Time{}, leaseOld)
	_ = dir.Remove(tmpPath)
	keepTmp = true
	if file, oErr := dir.Open("."); oErr == nil {
		_ = file.Sync()
		_ = file.Close()
	}
	return true, nil
}

func readAllCappedInDir(dir *os.Root, name string, limit int64) ([]byte, bool, error) {
	linfo, err := dir.Lstat(name)
	if err != nil {
		return nil, false, err
	}
	if !linfo.Mode().IsRegular() {
		return nil, false, errNotRegularFile
	}
	if linfo.Size() > limit {
		return nil, true, nil
	}
	f, err := dir.OpenFile(name, os.O_RDONLY|oNonBlockFlag, 0)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errNotRegularFile
	}
	if info.Size() > limit {
		return nil, true, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return nil, true, nil
	}
	return data, false, nil
}

// WriteNoteConditionalFingerprint atomically writes content to path ONLY if the
// exact inode being replaced still has FNV-1a 64-bit fingerprint == expected.
// Uses atomic RENAME_SWAP / RENAME_EXCHANGE with rollback on macOS/Linux so an
// external save racing between version check and write is never overwritten.
func WriteNoteConditionalFingerprint(path, content string, expected int64) (int64, error) {
	if err := CheckTextVersion(path, expected); err != nil {
		return 0, err
	}
	if lstat, err := os.Lstat(path); err == nil && lstat.Mode()&os.ModeSymlink != 0 {
		if realPath, evalErr := filepath.EvalSymlinks(path); evalErr == nil && realPath != "" {
			path = realPath
		}
	}
	parent := filepath.Dir(path)
	dir, err := os.OpenRoot(parent)
	if err != nil {
		return 0, err
	}
	defer dir.Close()

	base := filepath.Base(path)
	unlockPath := lockConditionalWritePath(dir.Name(), base)
	defer unlockPath()
	data := []byte(content)
	perm := os.FileMode(0o644)
	if info, stErr := dir.Stat(base); stErr == nil {
		perm = info.Mode().Perm()
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
			return 0, openErr
		}
	}
	if tmpFile == nil {
		return 0, errors.New(theme.Tr("无法创建临时文件", "Cannot allocate temporary file"))
	}

	var ourTmpInfo os.FileInfo
	keepTmp := false
	defer func() {
		if !keepTmp && tmpPath != "" {
			if ourTmpInfo == nil || sameInodeInDir(dir, tmpPath, ourTmpInfo) {
				_ = dir.Remove(tmpPath)
			}
		}
		leaseTmp.release()
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return 0, err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return 0, err
	}
	if st, stErr := tmpFile.Stat(); stErr == nil {
		ourTmpInfo = st
	}
	if err := tmpFile.Close(); err != nil {
		return 0, err
	}
	if ourTmpInfo == nil {
		ourTmpInfo, _ = dir.Lstat(tmpPath)
	}

	conflictErr := errors.New(theme.Tr("磁盘上的文件已修改，未覆盖", "The file changed on disk and was not overwritten"))

	swapped, swapErr := swapFilesInDir(dir, tmpPath, base)
	if swapped {
		if swapErr != nil {
			if errors.Is(swapErr, os.ErrNotExist) {
				return 0, conflictErr
			}
			return 0, swapErr
		}
		oldBytes, tooLarge, rErr := readAllCappedInDir(dir, tmpPath, TextLimit)
		if rErr != nil || tooLarge || fingerprintBytes(oldBytes) != expected {
			// Concurrent external save B landed at target; swap it back atomically.
			_, rbErr := swapFilesInDir(dir, tmpPath, base)
			if rbErr == nil {
				if !sameInodeInDir(dir, tmpPath, ourTmpInfo) {
					// A second concurrent save C landed before rollback; restore C and preserve what came back.
					_, rb2Err := swapFilesInDir(dir, tmpPath, base)
					if rb2Err == nil {
						if !sameInodeInDir(dir, tmpPath, ourTmpInfo) {
							finHash := ""
							if finBytes, _, finReadErr := readAllCappedInDir(dir, tmpPath, TextLimit); finReadErr == nil {
								finHash = conditionalSHA256Hex(finBytes)
							}
							preserveExternalConflictInDir(dir, tmpPath, base, finHash)
							keepTmp = true
						}
					} else {
						rbHash := ""
						if rbBytes, _, rbReadErr := readAllCappedInDir(dir, tmpPath, TextLimit); rbReadErr == nil {
							rbHash = conditionalSHA256Hex(rbBytes)
						}
						preserveExternalConflictInDir(dir, tmpPath, base, rbHash)
						keepTmp = true
					}
				}
			} else {
				oldHash := ""
				if rErr == nil && !tooLarge {
					oldHash = conditionalSHA256Hex(oldBytes)
				}
				preserveExternalConflictInDir(dir, tmpPath, base, oldHash)
				keepTmp = true
			}
			if rErr != nil && !errors.Is(rErr, errNotRegularFile) {
				return 0, rErr
			}
			return 0, conflictErr
		}
		// Verified: replaced inode matched expected fingerprint; stash it under
		// <AppDataDir()>/replaced/ so any external editor holding an open fd keeps nlink >= 1.
		stashReplacedInodeWithHandoff(dir, "", tmpPath, base, 0, time.Time{}, leaseTmp)
		keepTmp = true
		if file, oErr := dir.Open("."); oErr == nil {
			_ = file.Sync()
			_ = file.Close()
		}
		return fingerprintBytes(data), nil
	}

	// Non-swap fallback (e.g. HFS+): move base aside to inspect the exact inode,
	// publish tmpPath exclusively via dir.Link (so concurrent external writes never
	// get clobbered), and stash the replaced inode under <AppDataDir()>/replaced/.
	if !probeHardLinkInDir(dir, tmpPath) {
		return 0, conflictErr
	}
	curInfoBefore, stErr := dir.Lstat(base)
	if stErr != nil || !curInfoBefore.Mode().IsRegular() || curInfoBefore.Size() > TextLimit {
		return 0, conflictErr
	}
	movedOld := ""
	var leaseOld *activeTempLease
	for range 50 {
		cand := allocPrivateTempName("old")
		if _, lErr := dir.Lstat(cand); lErr == nil {
			continue
		}
		candLease := acquireActiveConditionalTemp(dir, cand)
		if renErr := dir.Rename(base, cand); renErr != nil {
			candLease.release()
			if errors.Is(renErr, os.ErrNotExist) {
				return 0, conflictErr
			}
			return 0, renErr
		}
		movedOld = cand
		leaseOld = candLease
		break
	}
	if movedOld == "" {
		return 0, conflictErr
	}
	curBytes, tooLarge, rErr := readAllCappedInDir(dir, movedOld, TextLimit)
	if rErr != nil || tooLarge || fingerprintBytes(curBytes) != expected {
		oldHash := ""
		if rErr == nil && !tooLarge {
			oldHash = conditionalSHA256Hex(curBytes)
		}
		restoreMovedOldInDir(dir, movedOld, base, oldHash, false)
		leaseOld.release()
		if rErr != nil && !errors.Is(rErr, errNotRegularFile) {
			return 0, rErr
		}
		return 0, conflictErr
	}
	if pubErr := dir.Link(tmpPath, base); pubErr != nil {
		if errors.Is(pubErr, os.ErrExist) {
			stashReplacedInodeWithHandoff(dir, "", movedOld, base, 0, time.Time{}, leaseOld)
			return 0, conflictErr
		}
		restoreMovedOldInDir(dir, movedOld, base, conditionalSHA256Hex(curBytes), false)
		leaseOld.release()
		return 0, pubErr
	}
	stashReplacedInodeWithHandoff(dir, "", movedOld, base, 0, time.Time{}, leaseOld)
	_ = dir.Remove(tmpPath)
	keepTmp = true
	if file, oErr := dir.Open("."); oErr == nil {
		_ = file.Sync()
		_ = file.Close()
	}
	return fingerprintBytes(data), nil
}

// ReadNoteInRoot reads a regular Markdown note strictly confined inside confineRoot
// via os.Root + O_NONBLOCK|O_NOFOLLOW, verifying both the root directory identity
// (expectedRootInfo) and target file identity (expectedInfo).
func ReadNoteInRoot(confineRoot, confineRel string, expectedRootInfo, expectedInfo os.FileInfo) (string, int64, error) {
	if strings.TrimSpace(confineRoot) == "" {
		return "", 0, errors.New(theme.Tr("无效的受限目录", "Invalid confined directory"))
	}
	cleanRel := filepath.Clean(confineRel)
	if cleanRel == "." || !filepath.IsLocal(cleanRel) {
		return "", 0, errors.New(theme.Tr("目标路径超出允许范围", "Target path escapes allowed directory"))
	}
	root, err := os.OpenRoot(confineRoot)
	if err != nil {
		return "", 0, err
	}
	defer root.Close()

	if expectedRootInfo != nil {
		rInfo, _, rErr := RootDirIdentityToken(root)
		if rErr != nil || !os.SameFile(rInfo, expectedRootInfo) {
			return "", 0, errors.New(theme.Tr("受限目录身份已变化", "Confined root directory identity changed"))
		}
	}
	if err := verifyNoSymlinkPathInRoot(root, cleanRel); err != nil {
		return "", 0, err
	}

	f, info, err := OpenRootRegularNonBlockNoFollow(root, cleanRel)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	if expectedInfo != nil && !os.SameFile(info, expectedInfo) {
		return "", 0, errors.New(theme.Tr("目标文件身份已变化", "Target file identity changed"))
	}
	if info.Size() > TextLimit {
		return "", 0, errors.New(theme.Tr("文件超过 5 MB 大小上限", "File exceeds 5 MB size limit"))
	}
	data, err := io.ReadAll(io.LimitReader(f, TextLimit+1))
	if err != nil {
		return "", 0, err
	}
	if int64(len(data)) > TextLimit {
		return "", 0, errors.New(theme.Tr("文件超过 5 MB 大小上限", "File exceeds 5 MB size limit"))
	}
	return string(data), fingerprintBytes(data), nil
}

// ReadNoteChecked reads a regular Markdown note without following a leaf symlink,
// verifying that the opened descriptor still matches expectedInfo.
func ReadNoteChecked(path string, expectedInfo os.FileInfo) (string, int64, error) {
	f, info, err := OpenRegularNonBlockNoFollow(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	if expectedInfo != nil && !os.SameFile(info, expectedInfo) {
		return "", 0, errors.New(theme.Tr("目标文件身份已变化", "Target file identity changed"))
	}
	if info.Size() > TextLimit {
		return "", 0, errors.New(theme.Tr("文件超过 5 MB 大小上限", "File exceeds 5 MB size limit"))
	}
	data, err := io.ReadAll(io.LimitReader(f, TextLimit+1))
	if err != nil {
		return "", 0, err
	}
	if int64(len(data)) > TextLimit {
		return "", 0, errors.New(theme.Tr("文件超过 5 MB 大小上限", "File exceeds 5 MB size limit"))
	}
	return string(data), fingerprintBytes(data), nil
}

// WriteNoteConditionalFingerprintInRoot atomically writes content to confineRel strictly
// inside confineRoot via os.Root, refusing symlinks and aborting if either the confined
// root directory identity, target file's inode identity, or content fingerprint changed.
func WriteNoteConditionalFingerprintInRoot(confineRoot, confineRel, content string, expected int64, expectedRootInfo, expectedInfo os.FileInfo) (int64, os.FileInfo, string, error) {
	return WriteBytesConditionalFingerprintInRoot(confineRoot, confineRel, []byte(content), expected, expectedRootInfo, expectedInfo)
}

// WriteNoteConditionalFingerprintChecked atomically writes content to path without
// following a final-component symlink, aborting if either the target file's inode
// identity (expectedInfo) or content fingerprint (expected) changed.
func WriteNoteConditionalFingerprintChecked(path, content string, expected int64, expectedInfo os.FileInfo) (int64, os.FileInfo, string, error) {
	return WriteBytesConditionalFingerprintChecked(path, []byte(content), expected, expectedInfo)
}

// WriteBytesConditionalFingerprintInRoot atomically writes raw bytes to confineRel
// strictly inside confineRoot via os.Root, enforcing root identity, regular-file,
// inode-identity, and fingerprint checks before and after swap.
func WriteBytesConditionalFingerprintInRoot(confineRoot, confineRel string, data []byte, expected int64, expectedRootInfo, expectedInfo os.FileInfo) (int64, os.FileInfo, string, error) {
	if strings.TrimSpace(confineRoot) == "" {
		return 0, nil, "", errors.New(theme.Tr("无效的受限目录", "Invalid confined directory"))
	}
	cleanRel := filepath.Clean(confineRel)
	if cleanRel == "." || !filepath.IsLocal(cleanRel) {
		return 0, nil, "", errors.New(theme.Tr("目标路径超出允许范围", "Target path escapes allowed directory"))
	}
	root, err := os.OpenRoot(confineRoot)
	if err != nil {
		return 0, nil, "", err
	}
	defer root.Close()

	if expectedRootInfo != nil {
		rInfo, _, rErr := RootDirIdentityToken(root)
		if rErr != nil || !os.SameFile(rInfo, expectedRootInfo) {
			return 0, nil, "", errors.New(theme.Tr("受限目录身份已变化，未覆盖", "Confined root directory identity changed and was not overwritten"))
		}
	}
	if err := verifyNoSymlinkPathInRoot(root, cleanRel); err != nil {
		return 0, nil, "", errors.New(theme.Tr("目标文件身份已变化，未覆盖", "Target file identity changed and was not overwritten"))
	}

	parent := filepath.Dir(cleanRel)
	dir := root
	if parent != "." && parent != "" {
		sub, err := root.OpenRoot(parent)
		if err != nil {
			return 0, nil, "", err
		}
		defer sub.Close()
		dir = sub
	}
	return writeBytesConditionalFingerprintInDir(dir, confineRoot, filepath.Base(cleanRel), data, expected, expectedInfo)
}

// WriteBytesConditionalFingerprintChecked atomically writes raw bytes to path without
// following a final-component symlink, enforcing regular-file, inode-identity, and
// fingerprint checks before and after swap.
func WriteBytesConditionalFingerprintChecked(path string, data []byte, expected int64, expectedInfo os.FileInfo) (int64, os.FileInfo, string, error) {
	clean := filepath.Clean(path)
	dir, err := os.OpenRoot(filepath.Dir(clean))
	if err != nil {
		return 0, nil, "", err
	}
	defer dir.Close()
	return writeBytesConditionalFingerprintInDir(dir, "", filepath.Base(clean), data, expected, expectedInfo)
}

func writeBytesConditionalFingerprintInDir(dir *os.Root, rootHint, base string, data []byte, expected int64, expectedInfo os.FileInfo) (int64, os.FileInfo, string, error) {
	conflictErr := errors.New(theme.Tr("磁盘上的文件已修改，未覆盖", "The file changed on disk and was not overwritten"))
	identityErr := errors.New(theme.Tr("目标文件身份已变化，未覆盖", "Target file identity changed and was not overwritten"))

	unlockPath := lockConditionalWritePath(dir.Name(), base)
	defer unlockPath()

	linfo, err := dir.Lstat(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil, "", conflictErr
		}
		return 0, nil, "", err
	}
	if !linfo.Mode().IsRegular() {
		return 0, nil, "", identityErr
	}
	if expectedInfo != nil && !os.SameFile(linfo, expectedInfo) {
		return 0, nil, "", identityErr
	}
	curBytes, tooLarge, rErr := readAllCappedInDir(dir, base, TextLimit)
	if rErr != nil {
		if errors.Is(rErr, errNotRegularFile) {
			return 0, nil, "", identityErr
		}
		return 0, nil, "", rErr
	}
	if tooLarge || fingerprintBytes(curBytes) != expected {
		return 0, nil, "", conflictErr
	}

	perm := linfo.Mode().Perm()
	var (
		tmpPath  string
		tmpFile  *os.File
		leaseTmp *activeTempLease
	)
	for range 100 {
		candidate := allocPrivateTempName("tmp")
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
			return 0, nil, "", openErr
		}
	}
	if tmpFile == nil {
		return 0, nil, "", errors.New(theme.Tr("无法创建临时文件", "Cannot allocate temporary file"))
	}

	var (
		ourTmpInfo os.FileInfo
		ourTmpID   string
	)
	keepTmp := false
	defer func() {
		if !keepTmp && tmpPath != "" {
			if ourTmpInfo == nil || sameInodeInDir(dir, tmpPath, ourTmpInfo) {
				_ = dir.Remove(tmpPath)
			}
		}
		leaseTmp.release()
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return 0, nil, "", err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return 0, nil, "", err
	}
	st, stErr := tmpFile.Stat()
	if stErr != nil || !st.Mode().IsRegular() {
		_ = tmpFile.Close()
		if stErr != nil {
			return 0, nil, "", stErr
		}
		return 0, nil, "", identityErr
	}
	ourTmpInfo = st
	ourTmpID = FileIdentityToken(ourTmpInfo, tmpFile)
	if err := tmpFile.Close(); err != nil {
		return 0, nil, "", err
	}
	// Verify tmpPath still names our exact staged temp file before publishing.
	if !sameInodeInDir(dir, tmpPath, ourTmpInfo) {
		keepTmp = true
		return 0, nil, "", conflictErr
	}

	swapped, swapErr := swapFilesInDir(dir, tmpPath, base)
	if swapped {
		if swapErr != nil {
			if errors.Is(swapErr, os.ErrNotExist) {
				return 0, nil, "", conflictErr
			}
			return 0, nil, "", swapErr
		}
		publishedInfo, pubStErr := dir.Lstat(base)
		publishedOK := pubStErr == nil && publishedInfo.Mode().IsRegular() && os.SameFile(publishedInfo, ourTmpInfo)

		oldInfo, stErr := dir.Lstat(tmpPath)
		identityOK := stErr == nil && oldInfo.Mode().IsRegular() && (expectedInfo == nil || os.SameFile(oldInfo, expectedInfo))
		var oldBytes []byte
		var oldTooLarge bool
		var oldReadErr error
		if identityOK {
			oldBytes, oldTooLarge, oldReadErr = readAllCappedInDir(dir, tmpPath, TextLimit)
		}
		if !publishedOK || !identityOK || oldReadErr != nil || oldTooLarge || fingerprintBytes(oldBytes) != expected {
			_, rbErr := swapFilesInDir(dir, tmpPath, base)
			if rbErr == nil {
				// After rollback swap, tmpPath should hold whatever had been at base
				// right after the first swap (ourTmpInfo normally, or publishedInfo if
				// tmpPath had been replaced before the first swap).
				expectedRolledOut := ourTmpInfo
				if !publishedOK && pubStErr == nil {
					expectedRolledOut = publishedInfo
				}
				if !sameInodeInDir(dir, tmpPath, expectedRolledOut) {
					_, rb2Err := swapFilesInDir(dir, tmpPath, base)
					if rb2Err == nil {
						if !sameInodeInDir(dir, tmpPath, expectedRolledOut) {
							finHash := ""
							if finBytes, _, finReadErr := readAllCappedInDir(dir, tmpPath, TextLimit); finReadErr == nil {
								finHash = conditionalSHA256Hex(finBytes)
							}
							preserveExternalConflictInDir(dir, tmpPath, base, finHash)
							keepTmp = true
						}
					} else {
						rbHash := ""
						if rbBytes, _, rbReadErr := readAllCappedInDir(dir, tmpPath, TextLimit); rbReadErr == nil {
							rbHash = conditionalSHA256Hex(rbBytes)
						}
						preserveExternalConflictInDir(dir, tmpPath, base, rbHash)
						keepTmp = true
					}
				}
				if !publishedOK && !sameInodeInDir(dir, tmpPath, ourTmpInfo) {
					keepTmp = true
				}
			} else {
				oldHash := ""
				if identityOK && oldReadErr == nil && !oldTooLarge {
					oldHash = conditionalSHA256Hex(oldBytes)
				}
				preserveExternalConflictInDir(dir, tmpPath, base, oldHash)
				keepTmp = true
			}
			if !publishedOK {
				return 0, nil, "", conflictErr
			}
			if !identityOK {
				return 0, nil, "", identityErr
			}
			if oldReadErr != nil && !errors.Is(oldReadErr, errNotRegularFile) {
				return 0, nil, "", oldReadErr
			}
			return 0, nil, "", conflictErr
		}
		stashReplacedInodeWithHandoff(dir, rootHint, tmpPath, base, 0, time.Time{}, leaseTmp)
		keepTmp = true
		if file, oErr := dir.Open("."); oErr == nil {
			_ = file.Sync()
			_ = file.Close()
		}
		return fingerprintBytes(data), ourTmpInfo, ourTmpID, nil
	}

	if !probeHardLinkInDir(dir, tmpPath) {
		return 0, nil, "", conflictErr
	}
	curInfoBefore, stErr := dir.Lstat(base)
	if stErr != nil || !curInfoBefore.Mode().IsRegular() || curInfoBefore.Size() > TextLimit {
		return 0, nil, "", identityErr
	}
	if expectedInfo != nil && !os.SameFile(curInfoBefore, expectedInfo) {
		return 0, nil, "", identityErr
	}
	movedOld := ""
	var leaseOld *activeTempLease
	for range 50 {
		cand := allocPrivateTempName("old")
		if _, lErr := dir.Lstat(cand); lErr == nil {
			continue
		}
		candLease := acquireActiveConditionalTemp(dir, cand)
		if renErr := dir.Rename(base, cand); renErr != nil {
			candLease.release()
			if errors.Is(renErr, os.ErrNotExist) {
				return 0, nil, "", conflictErr
			}
			return 0, nil, "", renErr
		}
		movedOld = cand
		leaseOld = candLease
		break
	}
	if movedOld == "" {
		return 0, nil, "", conflictErr
	}
	movedInfo, mStErr := dir.Lstat(movedOld)
	identityOK := mStErr == nil && movedInfo.Mode().IsRegular() && (expectedInfo == nil || os.SameFile(movedInfo, expectedInfo))
	var oldBytes []byte
	var oldTooLarge bool
	var oldReadErr error
	if identityOK {
		oldBytes, oldTooLarge, oldReadErr = readAllCappedInDir(dir, movedOld, TextLimit)
	}
	if !identityOK || oldReadErr != nil || oldTooLarge || fingerprintBytes(oldBytes) != expected {
		oldHash := ""
		if identityOK && oldReadErr == nil && !oldTooLarge {
			oldHash = conditionalSHA256Hex(oldBytes)
		}
		restoreMovedOldInDir(dir, movedOld, base, oldHash, false)
		leaseOld.release()
		if !identityOK {
			return 0, nil, "", identityErr
		}
		if oldReadErr != nil && !errors.Is(oldReadErr, errNotRegularFile) {
			return 0, nil, "", oldReadErr
		}
		return 0, nil, "", conflictErr
	}
	if !sameInodeInDir(dir, tmpPath, ourTmpInfo) {
		keepTmp = true
		restoreMovedOldInDir(dir, movedOld, base, conditionalSHA256Hex(oldBytes), false)
		leaseOld.release()
		return 0, nil, "", conflictErr
	}
	if pubErr := dir.Link(tmpPath, base); pubErr != nil {
		if errors.Is(pubErr, os.ErrExist) {
			stashReplacedInodeWithHandoff(dir, rootHint, movedOld, base, 0, time.Time{}, leaseOld)
			return 0, nil, "", conflictErr
		}
		restoreMovedOldInDir(dir, movedOld, base, conditionalSHA256Hex(oldBytes), false)
		leaseOld.release()
		return 0, nil, "", pubErr
	}
	// Verify base actually points to our staged temp file inode after Link.
	if !sameInodeInDir(dir, base, ourTmpInfo) {
		keepTmp = true
		if swappedBack, _ := swapFilesInDir(dir, movedOld, base); !swappedBack {
			if curBase, bErr := dir.Lstat(base); bErr == nil && curBase.Mode().IsRegular() && !os.SameFile(curBase, ourTmpInfo) {
				_ = dir.Rename(movedOld, base)
			}
			leaseOld.release()
		} else {
			stashReplacedInodeWithHandoff(dir, rootHint, movedOld, base, 0, time.Time{}, leaseOld)
		}
		return 0, nil, "", conflictErr
	}
	stashReplacedInodeWithHandoff(dir, rootHint, movedOld, base, 0, time.Time{}, leaseOld)
	if sameInodeInDir(dir, tmpPath, ourTmpInfo) {
		_ = dir.Remove(tmpPath)
	}
	keepTmp = true
	if file, oErr := dir.Open("."); oErr == nil {
		_ = file.Sync()
		_ = file.Close()
	}
	return fingerprintBytes(data), ourTmpInfo, ourTmpID, nil
}
