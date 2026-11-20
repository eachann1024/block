package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"block/internal/files"
)

const fileAssocStateVersion = 3

var builtinMarkdownExtensions = []string{
	".md", ".markdown", ".mdown", ".mkd", ".mkdn", ".mdwn", ".mdtxt", ".mdtext", ".mdx",
}

var assocExtRe = regexp.MustCompile(`^\.[a-z0-9][a-z0-9._+-]{0,31}$`)

type fileAssocState struct {
	Version         int      `json:"version"`
	AppVersion      string   `json:"app_version,omitempty"`
	AppPath         string   `json:"app_path,omitempty"`
	ExecutablePath  string   `json:"executable_path,omitempty"`
	ExecutableSize  int64    `json:"executable_size,omitempty"`
	ExecutableMtime int64    `json:"executable_mtime,omitempty"`
	Extensions      []string `json:"extensions"`
	CustomMIMEs     []string `json:"custom_mimes,omitempty"`
}

type fileAssocRequest struct {
	gen        uint64
	appDataDir string
	extensions []string
}

var (
	fileAssocEnabled atomic.Bool

	fileAssocMu      sync.Mutex
	fileAssocCond    = sync.NewCond(&fileAssocMu)
	fileAssocRunning bool
	fileAssocHasPend bool
	fileAssocLastGen uint64
	fileAssocPend    fileAssocRequest
)

// SetFileAssociationSyncEnabled controls whether active language extension
// changes synchronize OS "Open With" file associations. It is disabled by
// default so unit tests and isolated runs never mutate host OS registrations.
func SetFileAssociationSyncEnabled(enabled bool) {
	fileAssocEnabled.Store(enabled)
}

// WaitFileAssociationSyncTimeout waits up to timeout for any queued OS file
// association sync to finish. Returns true if idle before the deadline.
func WaitFileAssociationSyncTimeout(timeout time.Duration) bool {
	if timeout <= 0 {
		fileAssocMu.Lock()
		idle := !fileAssocRunning && !fileAssocHasPend
		fileAssocMu.Unlock()
		return idle
	}
	done := make(chan struct{})
	go func() {
		fileAssocMu.Lock()
		for fileAssocRunning || fileAssocHasPend {
			fileAssocCond.Wait()
		}
		fileAssocMu.Unlock()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// CleanupFileAssociations removes OS "Open With" registrations managed by Block
// and clears the persisted sync state (for explicit non-elevated user cleanup).
// When isolated is true (e.g. --data-dir or BLOCK_DATA_DIR override), global
// per-user OS stores (Windows HKCU, Linux XDG, and non-local macOS helper paths)
// are left untouched; only the data-dir-scoped macOS helper bundle is cleaned.
func CleanupFileAssociations(appDataDir string, isolated bool) error {
	dir := strings.TrimSpace(appDataDir)
	if dir == "" {
		return nil
	}
	if isolated && runtime.GOOS != "darwin" {
		return fmt.Errorf("refusing to modify global OS file associations under an isolated data directory")
	}
	SetFileAssociationSyncEnabled(false)
	WaitFileAssociationSyncTimeout(2 * time.Second)

	unlock, err := acquireFileAssocLock(dir, 5*time.Second)
	if err != nil {
		return err
	}
	defer unlock()

	if err := cleanupPlatformFileAssociations(dir, isolated); err != nil {
		return err
	}
	_ = os.Remove(fileAssocStatePath(dir))
	return nil
}

func normalizeAssocExtension(raw string) (string, bool) {
	ext := strings.ToLower(strings.TrimSpace(raw))
	if !assocExtRe.MatchString(ext) {
		return "", false
	}
	return ext, true
}

// isSingleSegmentExtension reports whether ext is a single-dot extension such as
// ".plist" (excluding multi-dot patterns like ".d.ts", which macOS LaunchServices
// and Windows Explorer HKCU\Software\Classes cannot match by extension).
func isSingleSegmentExtension(ext string) bool {
	return len(ext) > 1 && ext[0] == '.' && strings.Count(ext, ".") == 1
}

func collectPluginAssocExtensions(snap *languageSnapshot) []string {
	if snap == nil || len(snap.byExt) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(snap.byExt))
	out := make([]string, 0, len(snap.byExt))
	for raw := range snap.byExt {
		ext, ok := normalizeAssocExtension(raw)
		if !ok {
			continue
		}
		if _, dup := seen[ext]; dup {
			continue
		}
		seen[ext] = struct{}{}
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

func scheduleFileAssociationSync(appDataDir string, snap *languageSnapshot, gen uint64) {
	if !fileAssocEnabled.Load() {
		return
	}
	dir := strings.TrimSpace(appDataDir)
	if dir == "" {
		return
	}
	exts := collectPluginAssocExtensions(snap)

	fileAssocMu.Lock()
	if gen < fileAssocLastGen {
		fileAssocMu.Unlock()
		return
	}
	fileAssocLastGen = gen
	fileAssocPend = fileAssocRequest{
		gen:        gen,
		appDataDir: dir,
		extensions: exts,
	}
	fileAssocHasPend = true
	if !fileAssocRunning {
		fileAssocRunning = true
		go runFileAssociationWorker()
	}
	fileAssocMu.Unlock()
}

func runFileAssociationWorker() {
	for {
		fileAssocMu.Lock()
		if !fileAssocHasPend {
			fileAssocRunning = false
			fileAssocCond.Broadcast()
			fileAssocMu.Unlock()
			return
		}
		req := fileAssocPend
		fileAssocHasPend = false
		fileAssocMu.Unlock()

		unlock, lockErr := acquireFileAssocLock(req.appDataDir, 10*time.Second)
		if lockErr != nil {
			files.LogWarnf("file association sync lock failed: %v", lockErr)
			continue
		}
		err := syncPlatformFileAssociations(req.appDataDir, req.extensions)
		unlock()
		if err != nil {
			files.LogWarnf("file association sync failed: %v", err)
		}
	}
}

// acquireFileAssocLock provides lightweight cross-process serialization for
// sync and cleanup operations using an exclusive lock file with immediate dead-PID
// reclamation (plus age fallback when PID cannot be read).
func acquireFileAssocLock(appDataDir string, timeout time.Duration) (func(), error) {
	if err := os.MkdirAll(appDataDir, 0o755); err != nil {
		return func() {}, err
	}
	lockPath := filepath.Join(appDataDir, ".file-associations.lock")
	deadline := time.Now().Add(timeout)
	const staleAge = 45 * time.Second

	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return func() {}, err
		}
		if raw, readErr := os.ReadFile(lockPath); readErr == nil {
			if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw))); parseErr == nil && pid > 0 {
				if !isFileAssocLockPIDAlive(pid) {
					_ = os.Remove(lockPath)
					continue
				}
			}
		}
		if st, statErr := os.Stat(lockPath); statErr == nil && time.Since(st.ModTime()) > staleAge {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return func() {}, fmt.Errorf("timed out waiting for %s", lockPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func fileAssocStatePath(appDataDir string) string {
	return filepath.Join(appDataDir, "file-associations.json")
}

func loadFileAssocState(appDataDir string) (fileAssocState, bool) {
	raw, err := os.ReadFile(fileAssocStatePath(appDataDir))
	if err != nil {
		return fileAssocState{}, false
	}
	var st fileAssocState
	if err := json.Unmarshal(raw, &st); err != nil {
		return fileAssocState{}, false
	}
	return st, true
}

func saveFileAssocState(appDataDir string, st fileAssocState) error {
	st.Version = fileAssocStateVersion
	if st.Extensions == nil {
		st.Extensions = []string{}
	}
	if err := os.MkdirAll(appDataDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(fileAssocStatePath(appDataDir), raw, 0o644)
}

// writeFileAtomic creates an exclusive random temp file in dst's directory,
// writes data, sets permissions, and atomically renames into place.
func writeFileAtomic(dst string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".block-assoc-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func unionSortedStrings(groups ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, g := range groups {
		for _, item := range g {
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
