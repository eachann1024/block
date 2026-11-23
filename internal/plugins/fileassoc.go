package plugins

import (
	"crypto/sha256"
	"encoding/hex"
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
	codeed "github.com/egoist/mygo/ui/editor"
)

const fileAssocStateVersion = 7

var (
	assocExtRe     = regexp.MustCompile(`^\.[a-z0-9][a-z0-9._+-]{0,31}$`)
	bareAssocExtRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_+-]{0,31}$`)
)

// unsafeOSAssocExtensions blocks native executables, libraries, installers,
// packages, and OS shortcut extensions from ever being registered in OS
// "Open With" stores (even if declared by a third-party language plugin).
var unsafeOSAssocExtensions = map[string]struct{}{
	".a":        {},
	".app":      {},
	".appimage": {},
	".bin":      {},
	".com":      {},
	".deb":      {},
	".dll":      {},
	".dmg":      {},
	".dylib":    {},
	".elf":      {},
	".exe":      {},
	".lnk":      {},
	".msi":      {},
	".o":        {},
	".pif":      {},
	".pkg":      {},
	".rpm":      {},
	".scr":      {},
	".so":       {},
}

func builtinMarkdownExtensions() []string {
	return files.MarkdownExtensions()
}

func isSafeAssocExtension(ext string) bool {
	if _, blocked := unsafeOSAssocExtensions[ext]; blocked {
		return false
	}
	return true
}

// BuiltinAssocExtensions automatically aggregates every safe file extension
// supported by Block out of the box (Markdown, MyGo built-in languages,
// Block's embedded declarative grammars, and known text formats). Adding a new
// built-in grammar or language automatically registers it across Windows,
// macOS, and Linux with no manual extension list to maintain.
func BuiltinAssocExtensions() []string {
	seen := make(map[string]struct{}, 192)
	add := func(raw string) {
		ext, ok := normalizeAssocExtension(raw)
		if !ok || !isSafeAssocExtension(ext) {
			return
		}
		seen[ext] = struct{}{}
	}
	for _, ext := range files.MarkdownExtensions() {
		add(ext)
	}
	for _, ext := range files.KnownTextExtensions() {
		add(ext)
	}
	for _, lang := range codeed.Languages() {
		if lang == nil {
			continue
		}
		for _, ext := range lang.Extensions {
			add(ext)
		}
	}
	if snap := builtinLanguages(); snap != nil {
		for ext := range snap.byExt {
			add(ext)
		}
	}
	out := make([]string, 0, len(seen))
	for ext := range seen {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

// BuiltinMarkdownBareExtensions returns sorted bare Markdown extensions
// (without leading ".") for OS bundle manifests such as macOS Info.plist.
func BuiltinMarkdownBareExtensions() []string {
	md := files.MarkdownExtensions()
	out := make([]string, 0, len(md))
	for _, ext := range md {
		if !isSingleSegmentExtension(ext) {
			continue
		}
		if bare := strings.TrimPrefix(ext, "."); bareAssocExtRe.MatchString(bare) {
			out = append(out, bare)
		}
	}
	sort.Strings(out)
	return out
}

// BuiltinDocumentBareExtensions returns sorted bare single-segment extensions
// (without leading ".") for all built-in non-Markdown formats supported by Block.
func BuiltinDocumentBareExtensions() []string {
	mdSet := make(map[string]struct{}, 16)
	for _, ext := range files.MarkdownExtensions() {
		mdSet[ext] = struct{}{}
	}
	all := BuiltinAssocExtensions()
	out := make([]string, 0, len(all))
	for _, ext := range all {
		if _, isMD := mdSet[ext]; isMD {
			continue
		}
		if !isSingleSegmentExtension(ext) {
			continue
		}
		if bare := strings.TrimPrefix(ext, "."); bareAssocExtRe.MatchString(bare) {
			out = append(out, bare)
		}
	}
	sort.Strings(out)
	return out
}

type winAssocScopeState struct {
	ExecutablePath   string   `json:"executable_path,omitempty"`
	Extensions       []string `json:"extensions,omitempty"`
	LegacyProgID     string   `json:"legacy_prog_id,omitempty"`
	LegacyExtensions []string `json:"legacy_extensions,omitempty"`
}

type linuxAssocScopeState struct {
	DesktopID                  string            `json:"desktop_id,omitempty"`
	DataHome                   string            `json:"data_home,omitempty"`
	ExecutablePath             string            `json:"executable_path,omitempty"`
	Extensions                 []string          `json:"extensions,omitempty"`
	PinnedLinuxDefaults        map[string]string `json:"pinned_linux_defaults,omitempty"`
	PendingPinnedLinuxDefaults map[string]string `json:"pending_pinned_linux_defaults,omitempty"`
	PendingPinTx               bool              `json:"pending_pin_tx,omitempty"`
}

type macAssocScopeState struct {
	BundleID        string   `json:"bundle_id,omitempty"`
	HelperPath      string   `json:"helper_path,omitempty"`
	AppVersion      string   `json:"app_version,omitempty"`
	AppPath         string   `json:"app_path,omitempty"`
	ExecutablePath  string   `json:"executable_path,omitempty"`
	ExecutableSize  int64    `json:"executable_size,omitempty"`
	ExecutableMtime int64    `json:"executable_mtime,omitempty"`
	Extensions      []string `json:"extensions,omitempty"`
}

type fileAssocState struct {
	Version                 int                             `json:"version"`
	AppVersion              string                          `json:"app_version,omitempty"`
	AppPath                 string                          `json:"app_path,omitempty"`
	ProgID                  string                          `json:"prog_id,omitempty"`
	LegacyProgID            string                          `json:"legacy_prog_id,omitempty"`
	LegacyExtensions        []string                        `json:"legacy_extensions,omitempty"`
	WinScopes               map[string]winAssocScopeState   `json:"win_scopes,omitempty"`
	LinuxScopes             map[string]linuxAssocScopeState `json:"linux_scopes,omitempty"`
	MacScopes               map[string]macAssocScopeState   `json:"mac_scopes,omitempty"`
	LinuxMainDesktopID             string                          `json:"linux_main_desktop_id,omitempty"`
	LinuxMainDataHome              string                          `json:"linux_main_data_home,omitempty"`
	ExecutablePath          string                          `json:"executable_path,omitempty"`
	ExecutableSize          int64                           `json:"executable_size,omitempty"`
	ExecutableMtime         int64                           `json:"executable_mtime,omitempty"`
	Extensions              []string                        `json:"extensions"`
	CustomMIMEs             []string                        `json:"custom_mimes,omitempty"`
	PinnedLinuxDefaults            map[string]string               `json:"pinned_linux_defaults,omitempty"`
	PinnedLinuxMainDefaults        map[string]string               `json:"pinned_linux_main_defaults,omitempty"`
	PendingPinnedLinuxMainDefaults map[string]string               `json:"pending_pinned_linux_main_defaults,omitempty"`
	PendingLinuxMainPinTx          bool                            `json:"pending_linux_main_pin_tx,omitempty"`
}

func stableInstallScopeHash(canonicalPath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(strings.TrimSpace(canonicalPath))))
	return hex.EncodeToString(sum[:6])
}

type fileAssocRequest struct {
	gen          uint64
	appDataDir   string
	extensions   []string
	useDiskState bool
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

	// Each platform cleanup removes only the current installation's scope from
	// file-associations.json and deletes the state file only when no other
	// installation scopes remain.
	return cleanupPlatformFileAssociations(dir, isolated)
}

// SyncCurrentExecFileAssociations synchronously registers OS "Open With" file
// associations for the currently running executable using built-in languages
// plus persisted enabled plugin languages. Used when a secondary instance from
// another install directory hands off to an already-running primary instance
// and exits early, so the secondary installation still registers its own
// install-scoped Open-With entry.
func SyncCurrentExecFileAssociations(appDataDir string) error {
	dir := strings.TrimSpace(appDataDir)
	if dir == "" {
		return nil
	}
	unlock, err := acquireFileAssocLock(dir, 10*time.Second)
	if err != nil {
		files.LogWarnf("file association sync lock failed: %v", err)
		return err
	}
	defer unlock()

	pluginExts, loadErr := loadPersistedPluginAssocExtensions(dir)
	if loadErr != nil {
		files.LogWarnf("file association sync skipped: failed to read persisted plugin state under %s: %v", dir, loadErr)
		return loadErr
	}
	managedExts := unionSortedStrings(BuiltinAssocExtensions(), pluginExts)
	if syncErr := syncPlatformFileAssociations(dir, managedExts); syncErr != nil {
		files.LogWarnf("file association sync failed: %v", syncErr)
		return syncErr
	}
	return nil
}

// SyncLinuxUserFileAssociations runs protected per-user Linux Open-With
// registration for the current non-root user. Invoked by Debian package
// postinst (after dropping privileges to the installing user with their real
// desktop session environment) and by /etc/xdg/autostart/block-fileassoc.desktop
// so Block appears in file manager "Open With" menus before first launch
// without displacing existing defaults.
//
// When requireDesktopSession is true (package postinst), syncing is deferred
// whenever XDG_CURRENT_DESKTOP is absent so desktop-specific *-mimeapps.list
// defaults (such as gnome-mimeapps.list) are never misread from a headless
// elevated shell.
func SyncLinuxUserFileAssociations(appDataDir string, requireDesktopSession bool) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("refusing to sync per-user Linux file associations as root")
	}
	if requireDesktopSession && strings.TrimSpace(os.Getenv("XDG_CURRENT_DESKTOP")) == "" {
		return nil
	}
	dir := strings.TrimSpace(appDataDir)
	if dir == "" {
		return fmt.Errorf("app data directory is required")
	}
	unlock, err := acquireFileAssocLock(dir, 10*time.Second)
	if err != nil {
		return err
	}
	defer unlock()

	pluginExts, loadErr := loadPersistedPluginAssocExtensions(dir)
	if loadErr != nil {
		return fmt.Errorf("read persisted plugin state: %w", loadErr)
	}
	managedExts := unionSortedStrings(BuiltinAssocExtensions(), pluginExts)
	return syncPlatformFileAssociations(dir, managedExts)
}

// CurrentLinuxHelperDesktopID returns the deterministic per-executable Linux
// helper desktop entry ID (block-assoc-<hash>.desktop) for the running binary.
func CurrentLinuxHelperDesktopID() (string, error) {
	if runtime.GOOS != "linux" {
		return "", nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	return linuxHelperDesktopIDForExec(filepath.Clean(exe)), nil
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
		if !ok || !isSafeAssocExtension(ext) {
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

func collectManagedAssocExtensions(snap *languageSnapshot) []string {
	return unionSortedStrings(BuiltinAssocExtensions(), collectPluginAssocExtensions(snap))
}

func isPathWithinDir(parent, child string) bool {
	p := filepath.Clean(strings.TrimSpace(parent))
	c := filepath.Clean(strings.TrimSpace(child))
	if p == "" || c == "" {
		return false
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// loadPersistedPluginAssocExtensions re-reads installed plugin manifests and
// disabled-plugins.json from appDataDir while holding the cross-process
// file-association lock. It uses the exact same disabled-plugins.json reader
// and validator as StoreManager so invalid/null state is never treated as
// "all plugins enabled".
func loadPersistedPluginAssocExtensions(appDataDir string) ([]string, error) {
	appsRoot := filepath.Join(appDataDir, "apps")
	cacheRoot := filepath.Join(appDataDir, "apps-cache")

	disabled, stateErr := loadDisabledPluginState(cacheRoot)
	if stateErr != nil {
		return nil, stateErr
	}

	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	seen := make(map[string]struct{})
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !pluginIDRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(appsRoot, e.Name())
		m, loadErr := LoadManifest(dir)
		if loadErr != nil || m.ID != e.Name() {
			continue
		}
		if m.ID == DiagnosticsPluginID || disabled[m.ID] {
			continue
		}
		if m.Contributes == nil || len(m.Contributes.Languages) == 0 {
			continue
		}
		root, openErr := os.OpenRoot(dir)
		if openErr != nil {
			continue
		}
		for _, lc := range m.Contributes.Languages {
			if _, langErr := loadPluginLanguage(root, m.ID, lc); langErr != nil {
				continue
			}
			for _, rawExt := range lc.Extensions {
				ext, ok := normalizeAssocExtension(rawExt)
				if !ok || !isSafeAssocExtension(ext) {
					continue
				}
				if _, dup := seen[ext]; dup {
					continue
				}
				seen[ext] = struct{}{}
				out = append(out, ext)
			}
		}
		root.Close()
	}
	sort.Strings(out)
	return out, nil
}

func scheduleFileAssociationSync(appDataDir string, snap *languageSnapshot, gen uint64, useDiskState bool) {
	if !fileAssocEnabled.Load() {
		return
	}
	dir := strings.TrimSpace(appDataDir)
	if dir == "" {
		return
	}
	exts := collectManagedAssocExtensions(snap)

	fileAssocMu.Lock()
	if gen < fileAssocLastGen {
		fileAssocMu.Unlock()
		return
	}
	fileAssocLastGen = gen
	fileAssocPend = fileAssocRequest{
		gen:          gen,
		appDataDir:   dir,
		extensions:   exts,
		useDiskState: useDiskState,
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
		exts := req.extensions
		skip := false
		if req.useDiskState {
			if diskExts, loadErr := loadPersistedPluginAssocExtensions(req.appDataDir); loadErr == nil {
				exts = unionSortedStrings(BuiltinAssocExtensions(), diskExts)
			} else {
				files.LogWarnf("file association sync skipped: failed to read persisted plugin state under %s: %v", req.appDataDir, loadErr)
				skip = true
			}
		}
		var err error
		if !skip {
			err = syncPlatformFileAssociations(req.appDataDir, exts)
		}
		unlock()
		if err != nil {
			files.LogWarnf("file association sync failed: %v", err)
		}
	}
}

func fileAssocStatePath(appDataDir string) string {
	return filepath.Join(appDataDir, "file-associations.json")
}

// acquireLegacyFileAssocPIDLock bridges upgrade windows where an older Block
// process still coordinates using the legacy O_EXCL PID file
// (".file-associations.lock") without OS flock/LockFileEx. Callers must hold
// the persistent v2 OS lock first so new processes never race with each other
// on the legacy file; acquiring the legacy file also blocks older processes
// from mutating associations concurrently while the new process runs.
//
// A live PID is never reclaimed by lock age (we wait until deadline and fail
// safely instead), and both stale reclamation and lock release verify file
// identity via os.SameFile so a concurrent process's lock file is never deleted.
func acquireLegacyFileAssocPIDLock(appDataDir string, deadline time.Time, isAlive func(int) bool) (func(), error) {
	legacyPath := filepath.Join(appDataDir, ".file-associations.lock")
	const staleAge = 45 * time.Second

	removeIfSameFile := func(expected os.FileInfo) bool {
		if expected == nil {
			return false
		}
		cur, err := os.Lstat(legacyPath)
		if err != nil || !os.SameFile(expected, cur) {
			return false
		}
		return os.Remove(legacyPath) == nil
	}

	sleepOrTimeout := func() error {
		rem := time.Until(deadline)
		if rem <= 0 {
			return fmt.Errorf("timed out waiting for legacy lock %s", legacyPath)
		}
		if rem > 35*time.Millisecond {
			rem = 35 * time.Millisecond
		}
		time.Sleep(rem)
		return nil
	}

	for attempt := 0; ; attempt++ {
		if attempt > 0 && time.Now().After(deadline) {
			return func() {}, fmt.Errorf("timed out waiting for legacy lock %s", legacyPath)
		}
		f, err := os.OpenFile(legacyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			ownedStat, statErr := f.Stat()
			_ = f.Close()
			var once sync.Once
			return func() {
				once.Do(func() {
					if statErr == nil {
						removeIfSameFile(ownedStat)
					}
				})
			}, nil
		}
		if !os.IsExist(err) {
			return func() {}, err
		}

		// Use Lstat so a dangling symlink is distinguished from an actually
		// removed file and symlinks are never followed when inspecting PIDs.
		stBefore, statErr := os.Lstat(legacyPath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				if time.Now().After(deadline) {
					return func() {}, fmt.Errorf("timed out waiting for legacy lock %s", legacyPath)
				}
				continue
			}
		} else if stBefore.Mode()&os.ModeSymlink == 0 && stBefore.Mode().IsRegular() {
			raw, readErr := os.ReadFile(legacyPath)
			reclaim := false
			if readErr == nil {
				if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw))); parseErr == nil && pid > 0 {
					if isAlive != nil {
						// Only reclaim if the recorded PID is confirmed dead.
						// Never reclaim a lock held by a still-live process,
						// regardless of lock file age.
						if !isAlive(pid) {
							reclaim = true
						}
					} else if time.Since(stBefore.ModTime()) > staleAge {
						reclaim = true
					}
				} else if time.Since(stBefore.ModTime()) > staleAge {
					// Empty or corrupted PID file left behind by a crash before write.
					reclaim = true
				}
			}
			if reclaim && removeIfSameFile(stBefore) {
				continue
			}
		}

		if err := sleepOrTimeout(); err != nil {
			return func() {}, err
		}
	}
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
