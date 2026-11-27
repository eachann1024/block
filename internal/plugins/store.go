package plugins

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"block/internal/files"
	"block/internal/plugins/wasmrt"
)

// StoreAppEntry describes an entry in the Block App Store catalog index (topic: blockmd-plugin).
type StoreAppEntry struct {
	App            string        `json:"app"`
	ID             string        `json:"id"`
	Category       Category      `json:"category,omitempty"`
	Name           LocalizedText `json:"name"`
	Version        string        `json:"version"`
	Author         string        `json:"author"`
	Repo           string        `json:"repo,omitempty"`
	Summary        LocalizedText `json:"summary"`
	DownloadURL    string        `json:"download_url"`
	SHA256         string        `json:"sha256,omitempty"`
	Downloads      int           `json:"downloads"`
	Stars          int           `json:"stars,omitempty"`
	Verified       bool          `json:"verified,omitempty"`
	Beta           bool          `json:"beta,omitempty"`
	UpdatedAt         string                       `json:"updated_at,omitempty"`
	Permissions       []Permission                 `json:"permissions"`
	PermissionReasons map[Permission]LocalizedText `json:"permission_reasons,omitempty"`
	Surfaces          []Surface                    `json:"surfaces"`
	FileExtensions []string      `json:"file_extensions,omitempty"`
	Icon           DuoIconSpec          `json:"icon"`
	MinHostVersion string               `json:"min_host_version,omitempty"`
	ChangelogURL   string               `json:"changelog_url,omitempty"`
	Changelog      []PluginReleaseEntry `json:"changelog,omitempty"`
}

// EffectiveCategory returns the canonical App Store category for e.
func (e StoreAppEntry) EffectiveCategory() Category {
	return ResolveCategory(e.Category, len(e.FileExtensions) > 0)
}

type StoreIndex struct {
	App           string          `json:"app"`
	SchemaVersion int             `json:"schema_version"`
	Topic         string          `json:"topic"`
	UpdatedAt     string          `json:"updated_at"`
	Apps          []StoreAppEntry `json:"apps"`
}

type InstalledApp struct {
	Manifest    Manifest  `json:"manifest"`
	InstallDir  string    `json:"install_dir"`
	DataDir     string    `json:"data_dir"`
	Enabled     bool      `json:"enabled"`
	InstalledAt time.Time `json:"installed_at"`
}

// DiagnosticsPluginID uses Block's built-in Performance & Usage window.
const DiagnosticsPluginID = "block-diagnostics"

const (
	installSourceFile = ".block-install-source"
	maxArchiveBytes   = 64 * 1024 * 1024
	maxPackageFile    = 32 * 1024 * 1024
	maxPackageBytes   = 256 * 1024 * 1024
	maxExpandedTar    = 320 * 1024 * 1024
	maxPackagePaths   = 8192
)

type builtinSeed struct {
	Entry StoreAppEntry
	FS    fs.FS
	Sub   string
}

var (
	builtinMu             sync.RWMutex
	builtinSeeds          = map[string]builtinSeed{}
	embeddedFallbackIndex *StoreIndex
)

func setEmbeddedFallbackIndex(idx *StoreIndex) {
	builtinMu.Lock()
	embeddedFallbackIndex = idx
	builtinMu.Unlock()
}

// RegisterBuiltinCatalogApp registers an embedded official catalog package so the App Store
// works seamlessly offline (extracting into apps/<id> ONLY when the user clicks Install).
func RegisterBuiltinCatalogApp(entry StoreAppEntry, files fs.FS, subDir string) {
	builtinMu.Lock()
	defer builtinMu.Unlock()
	builtinSeeds[entry.ID] = builtinSeed{Entry: entry, FS: files, Sub: subDir}
}

func DefaultSeedIndex() *StoreIndex {
	builtinMu.RLock()
	defer builtinMu.RUnlock()
	seedApps := make([]StoreAppEntry, 0, len(builtinSeeds))
	for _, s := range builtinSeeds {
		if hiddenCatalogID(s.Entry.ID) {
			continue
		}
		seedApps = append(seedApps, s.Entry)
	}
	sort.Slice(seedApps, func(i, j int) bool {
		if seedApps[i].Downloads != seedApps[j].Downloads {
			return seedApps[i].Downloads > seedApps[j].Downloads
		}
		return seedApps[i].ID < seedApps[j].ID
	})
	var fallback *StoreIndex
	if embeddedFallbackIndex != nil {
		cp := *embeddedFallbackIndex
		filtered := make([]StoreAppEntry, 0, len(embeddedFallbackIndex.Apps))
		for _, a := range embeddedFallbackIndex.Apps {
			if hiddenCatalogID(a.ID) {
				continue
			}
			filtered = append(filtered, a)
		}
		cp.Apps = filtered
		fallback = &cp
	}
	if len(seedApps) == 0 && fallback != nil {
		return fallback
	}
	builtinIdx := &StoreIndex{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		Topic:         PluginTopic,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		Apps:          seedApps,
	}
	if fallback != nil {
		return mergeStoreIndexes(fallback, builtinIdx)
	}
	return builtinIdx
}

func readBuiltinSeedReadme(id string, en bool) (PluginReadme, bool) {
	builtinMu.RLock()
	seed, ok := builtinSeeds[id]
	builtinMu.RUnlock()
	if !ok || seed.FS == nil {
		return PluginReadme{}, false
	}
	candidates := []string{"README.zh.md", "README.md"}
	if en {
		candidates = []string{"README.en.md", "README.md"}
	}
	for _, name := range candidates {
		p := path.Join(seed.Sub, name)
		raw, err := fs.ReadFile(seed.FS, p)
		if err != nil {
			continue
		}
		doc, err := readReadmeText(bytes.NewReader(raw))
		if err == nil {
			return PluginReadme{Text: doc, LocalFS: seed.FS, LocalFSSub: seed.Sub}, true
		}
	}
	return PluginReadme{}, false
}

// DefaultStoreIndexURLs fetches catalog digests from the official repository,
// falling back to jsDelivr CDN when raw.githubusercontent.com is unreachable.
var DefaultStoreIndexURLs = []string{
	"https://raw.githubusercontent.com/blockmd-plugins/store-index/main/store-index.json",
	"https://cdn.jsdelivr.net/gh/blockmd-plugins/store-index@main/store-index.json",
}

// archiveMirrorPrefixes are SHA-256-verified fallback relays for GitHub Release archives,
// matching internal/updater/sources.go. Never used without an expected SHA-256 digest.
var archiveMirrorPrefixes = []string{
	"https://gh-proxy.com/",
	"https://ghproxy.net/",
}

const archiveAttemptTimeout = 15 * time.Second

type storeNetworkError struct {
	host string
	err  error
}

func (e *storeNetworkError) Error() string {
	if e == nil {
		return ""
	}
	if e.host != "" {
		return fmt.Sprintf("download from %s: %v", e.host, e.err)
	}
	if e.err != nil {
		return e.err.Error()
	}
	return "network download failed"
}

func (e *storeNetworkError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

var (
	errArchiveTooManyRedirects = errors.New("too many plugin archive redirects")
	errArchiveRedirectSource   = errors.New("plugin archive redirect changed its source")
	errCatalogRedirectSource   = errors.New("plugin catalog redirect changed its source")
	errArchiveInvalidURL       = errors.New("plugin archive requires an HTTPS URL without credentials or a fragment")
)

// StoreManager manages isolated plugin installations under <BlockAppData>/apps/<id>/,
// isolated per-plugin storage under <BlockAppData>/apps-data/<id>/, and store index cache
// under <BlockAppData>/apps-cache/store-index.json without touching Block's core state.json.
type StoreManager struct {
	AppsRoot      string
	DataRoot      string
	CacheRoot     string
	IndexURLs     []string
	HTTP          *http.Client
	dlMu          sync.Mutex
	dlLoaded      bool
	dlCachePath   string
	dlCounts      map[string]int
	dlGen         int
	stateMu       sync.Mutex
	installMu     sync.Mutex
	stageMu       sync.Mutex
	activeStages  map[string]bool
	beforeReplace func(pluginID string) error
}

const (
	stageOwnerFile       = ".block-stage-owner"
	stageOrphanTTL       = 2 * time.Hour
	stageMissingOwnerTTL = 10 * time.Minute
)

var processStageInstanceID = newStageInstanceID()

func newStageInstanceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("pid-%d-%d", os.Getpid(), time.Now().UnixNano())
}

type stageOwnerMeta struct {
	PID         int    `json:"pid"`
	Instance    string `json:"instance"`
	CreatedUnix int64  `json:"created_unix"`
}

func (s *StoreManager) registerStageDir(dir string) {
	if s == nil || dir == "" {
		return
	}
	s.stageMu.Lock()
	if s.activeStages == nil {
		s.activeStages = map[string]bool{}
	}
	s.activeStages[dir] = true
	s.stageMu.Unlock()
	raw, err := json.Marshal(stageOwnerMeta{
		PID:         os.Getpid(),
		Instance:    processStageInstanceID,
		CreatedUnix: time.Now().Unix(),
	})
	if err == nil {
		_ = os.WriteFile(filepath.Join(dir, stageOwnerFile), raw, 0o600)
	}
}

func (s *StoreManager) unregisterStageDir(dir string) {
	if s == nil || dir == "" {
		return
	}
	s.stageMu.Lock()
	delete(s.activeStages, dir)
	s.stageMu.Unlock()
}

func (s *StoreManager) isStageDirActive(dir string) bool {
	if s == nil || dir == "" {
		return false
	}
	s.stageMu.Lock()
	active := s.activeStages[dir]
	s.stageMu.Unlock()
	return active
}

func (s *StoreManager) sweepStaleStagesLocked() {
	entries, err := os.ReadDir(s.CacheRoot)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || (!strings.HasPrefix(name, "stage-dir-") && !strings.HasPrefix(name, "stage-arc-") && !strings.HasPrefix(name, "stage-seed-")) {
			continue
		}
		dir := filepath.Join(s.CacheRoot, name)
		if s.isStageDirActive(dir) {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, stageOwnerFile))
		if readErr != nil {
			if info, statErr := e.Info(); statErr == nil && now.Sub(info.ModTime()) > stageMissingOwnerTTL {
				_ = os.RemoveAll(dir)
			}
			continue
		}
		var meta stageOwnerMeta
		if json.Unmarshal(raw, &meta) != nil || meta.PID <= 0 {
			if info, statErr := e.Info(); statErr == nil && now.Sub(info.ModTime()) > stageMissingOwnerTTL {
				_ = os.RemoveAll(dir)
			}
			continue
		}
		if meta.Instance == processStageInstanceID {
			// Same process instance, but no longer in activeStages -> abandoned.
			_ = os.RemoveAll(dir)
			continue
		}
		if alive, known := stageOwnerPIDAlive(meta.PID); known {
			if !alive {
				_ = os.RemoveAll(dir)
			}
			continue
		}
		created := time.Unix(meta.CreatedUnix, 0)
		if meta.CreatedUnix <= 0 {
			if info, statErr := e.Info(); statErr == nil {
				created = info.ModTime()
			}
		}
		if now.Sub(created) > stageOrphanTTL {
			_ = os.RemoveAll(dir)
		}
	}
}

func NewStoreManager(blockAppDataDir string) *StoreManager {
	urls := append([]string(nil), DefaultStoreIndexURLs...)
	if custom := strings.TrimSpace(os.Getenv("BLOCKMD_STORE_URL")); custom != "" {
		urls = append([]string{custom}, urls...)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyFromEnvironment
	tr.ResponseHeaderTimeout = 10 * time.Second
	return &StoreManager{
		AppsRoot:     filepath.Join(blockAppDataDir, "apps"),
		DataRoot:     filepath.Join(blockAppDataDir, "apps-data"),
		CacheRoot:    filepath.Join(blockAppDataDir, "apps-cache"),
		IndexURLs:    urls,
		activeStages: map[string]bool{},
		HTTP: &http.Client{
			Transport: tr,
			Timeout:   20 * time.Second,
		},
	}
}

func (s *StoreManager) EnsureDirs() error {
	for _, dir := range []string{s.AppsRoot, s.DataRoot, s.CacheRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *StoreManager) cacheIndexPath() string {
	source := sha256.Sum256([]byte(strings.Join(s.IndexURLs, "\n")))
	return filepath.Join(s.CacheRoot, "store-index-"+hex.EncodeToString(source[:8])+".json")
}

func (s *StoreManager) downloadCountsPath() string {
	return filepath.Join(s.CacheRoot, "download-counts.json")
}

func (s *StoreManager) disabledPluginsPath() string {
	return filepath.Join(s.CacheRoot, "disabled-plugins.json")
}

func loadDisabledPluginState(cacheRoot string) (map[string]bool, error) {
	out := map[string]bool{}
	root, err := os.OpenRoot(cacheRoot)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := readPluginFile(root, "disabled-plugins.json", 1024*1024)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("disabled plugin state must be a JSON object")
	}
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return nil, fmt.Errorf("read disabled plugin state: %w", err)
	}
	if out == nil {
		return nil, errors.New("disabled plugin state must be an object")
	}
	return out, nil
}

func (s *StoreManager) loadDisabledLocked() (map[string]bool, error) {
	return loadDisabledPluginState(s.CacheRoot)
}

func (s *StoreManager) saveDisabledLocked(disabled map[string]bool) error {
	if len(disabled) == 0 {
		if err := os.Remove(s.disabledPluginsPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	raw, err := json.MarshalIndent(disabled, "", "  ")
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.CacheRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	return files.WriteNoteAt(root, filepath.Base(s.disabledPluginsPath()), string(raw))
}

// IsEnabled reports whether pluginID is currently enabled (defaults to true).
func (s *StoreManager) IsEnabled(pluginID string) bool {
	if pluginID == DiagnosticsPluginID {
		return false
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	disabled, err := s.loadDisabledLocked()
	return err == nil && !disabled[pluginID]
}

// SetEnabled persists the enabled/disabled state for an installed plugin.
func (s *StoreManager) SetEnabled(pluginID string, enabled bool) error {
	if pluginID == DiagnosticsPluginID && enabled {
		return errors.New("Performance & Usage is built into Block")
	}
	if !pluginIDRe.MatchString(pluginID) {
		return fmt.Errorf("invalid plugin id %q", pluginID)
	}
	if err := s.EnsureDirs(); err != nil {
		return err
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	disabled, err := s.loadDisabledLocked()
	if err != nil {
		return err
	}
	if enabled {
		delete(disabled, pluginID)
	} else {
		disabled[pluginID] = true
	}
	return s.saveDisabledLocked(disabled)
}

func (s *StoreManager) loadDownloadDeltasLocked() map[string]int {
	p := s.downloadCountsPath()
	if s.dlLoaded && s.dlCounts != nil && s.dlCachePath == p {
		return s.dlCounts
	}
	out := map[string]int{}
	if raw, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	s.dlCounts = out
	s.dlCachePath = p
	s.dlLoaded = true
	return s.dlCounts
}

// IncrementDownloads records a completed download/install for pluginID in apps-cache/download-counts.json.
func (s *StoreManager) IncrementDownloads(pluginID string) int {
	if strings.TrimSpace(pluginID) == "" {
		return 0
	}
	_ = s.EnsureDirs()
	s.dlMu.Lock()
	defer s.dlMu.Unlock()
	deltas := s.loadDownloadDeltasLocked()
	deltas[pluginID]++
	s.dlGen++
	next := deltas[pluginID]
	if raw, err := json.MarshalIndent(deltas, "", "  "); err == nil {
		_ = os.WriteFile(s.downloadCountsPath(), raw, 0o644)
	}
	return next
}

// DownloadDeltaGeneration changes whenever a local download is recorded, so
// cached store listings know to re-rank.
func (s *StoreManager) DownloadDeltaGeneration() int {
	s.dlMu.Lock()
	defer s.dlMu.Unlock()
	return s.dlGen
}

// EffectiveDownloads returns the catalog download baseline plus local recorded downloads for entry.ID.
func (s *StoreManager) EffectiveDownloads(entry StoreAppEntry) int {
	s.dlMu.Lock()
	delta := s.loadDownloadDeltasLocked()[entry.ID]
	s.dlMu.Unlock()
	base := entry.Downloads
	if base < 0 {
		base = 0
	}
	return base + delta
}

// LoadCachedOrSeedCatalog returns the freshest available catalog between the
// on-disk cache and the embedded seed index without resurrecting delisted apps.
func (s *StoreManager) LoadCachedOrSeedCatalog() *StoreIndex {
	seed := DefaultSeedIndex()
	raw, err := os.ReadFile(s.cacheIndexPath())
	if err != nil {
		return seed
	}
	parsed, err := ParseStoreIndex(raw)
	if err != nil {
		return seed
	}
	if seed != nil && indexNewerThan(seed.UpdatedAt, parsed.UpdatedAt) {
		return filterStoreIndex(seed)
	}
	if hasRegisteredBuiltinSeeds() {
		return mergeStoreIndexes(parsed, seed)
	}
	return filterStoreIndex(parsed)
}

func hasRegisteredBuiltinSeeds() bool {
	builtinMu.RLock()
	defer builtinMu.RUnlock()
	return len(builtinSeeds) > 0
}

func indexNewerThan(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, strings.TrimSpace(a))
	if errA != nil {
		return false
	}
	tb, errB := time.Parse(time.RFC3339, strings.TrimSpace(b))
	if errB != nil {
		return true
	}
	return ta.After(tb)
}

// RefreshCatalog fetches the latest remote store-index.json from configured sources, caches it locally,
// and returns the filtered remote catalog (giving each source an independent timeout budget).
func (s *StoreManager) RefreshCatalog(ctx context.Context) (*StoreIndex, error) {
	if err := s.EnsureDirs(); err != nil {
		return s.LoadCachedOrSeedCatalog(), err
	}
	baseline := s.LoadCachedOrSeedCatalog()
	const perSourceTimeout = 6 * time.Second
	var lastErr error
	var bestRemote *StoreIndex
	var bestBody []byte
	for _, u := range s.IndexURLs {
		indexURL, err := parseArchiveURL(u)
		if err != nil {
			lastErr = err
			continue
		}
		srcCtx, srcCancel := withStageTimeout(ctx, perSourceTimeout)
		req, err := http.NewRequestWithContext(srcCtx, http.MethodGet, indexURL.String(), nil)
		if err != nil {
			srcCancel()
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "Block-AppStore/1.0 (blockmd)")
		client := *s.HTTP
		client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			target, err := parseArchiveURL(next.URL.String())
			if err != nil || len(via) >= 10 || target.Host != indexURL.Host {
				return errCatalogRedirectSource
			}
			return nil
		}
		resp, err := client.Do(req)
		if err != nil {
			srcCancel()
			lastErr = sanitizeHTTPError(err, indexURL)
			continue
		}
		const maxIndexBytes = 8 * 1024 * 1024
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxIndexBytes+1))
		resp.Body.Close()
		srcCancel()
		if resp.StatusCode != http.StatusOK || readErr != nil || len(body) > maxIndexBytes {
			lastErr = fmt.Errorf("fetch %s: status %d", u, resp.StatusCode)
			continue
		}
		idx, parseErr := ParseStoreIndex(body)
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		if bestRemote == nil || indexNewerThan(idx.UpdatedAt, bestRemote.UpdatedAt) {
			bestRemote = idx
			bestBody = body
		}
		// Keep trying fallback mirrors if this source is older than our local baseline.
		if baseline != nil && indexNewerThan(baseline.UpdatedAt, idx.UpdatedAt) {
			continue
		}
		_ = os.WriteFile(s.cacheIndexPath(), body, 0o644)
		if hasRegisteredBuiltinSeeds() {
			return mergeStoreIndexes(idx, DefaultSeedIndex()), nil
		}
		return filterStoreIndex(idx), nil
	}
	if bestRemote != nil {
		if baseline != nil && indexNewerThan(baseline.UpdatedAt, bestRemote.UpdatedAt) {
			return baseline, nil
		}
		_ = os.WriteFile(s.cacheIndexPath(), bestBody, 0o644)
		if hasRegisteredBuiltinSeeds() {
			return mergeStoreIndexes(bestRemote, DefaultSeedIndex()), nil
		}
		return filterStoreIndex(bestRemote), nil
	}
	return baseline, lastErr
}

// hiddenCatalogID reports catalog entries the store never lists: empty IDs,
// the built-in diagnostics window and language packs now shipped built in.
func hiddenCatalogID(id string) bool {
	return id == "" || id == DiagnosticsPluginID || SupersededLanguagePluginIDs[id]
}

func filterStoreIndex(idx *StoreIndex) *StoreIndex {
	return mergeStoreIndexes(idx, nil)
}

func mergeStoreIndexes(primary, fallback *StoreIndex) *StoreIndex {
	if primary == nil {
		if fallback == nil {
			return nil
		}
		return mergeStoreIndexes(fallback, nil)
	}
	capLen := len(primary.Apps)
	if fallback != nil {
		capLen += len(fallback.Apps)
	}
	seen := make(map[string]bool, len(primary.Apps))
	merged := make([]StoreAppEntry, 0, capLen)
	for _, a := range primary.Apps {
		if hiddenCatalogID(a.ID) || seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		merged = append(merged, a)
	}
	if fallback != nil {
		for _, a := range fallback.Apps {
			if hiddenCatalogID(a.ID) || seen[a.ID] {
				continue
			}
			seen[a.ID] = true
			merged = append(merged, a)
		}
	}
	return &StoreIndex{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		Topic:         PluginTopic,
		UpdatedAt:     primary.UpdatedAt,
		Apps:          merged,
	}
}

func (s *StoreManager) ListInstalled() ([]InstalledApp, error) {
	s.installMu.Lock()
	defer s.installMu.Unlock()
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	// Restore any update interrupted between the two directory renames.
	cached, err := os.ReadDir(s.CacheRoot)
	if err != nil {
		return nil, err
	}
	for _, e := range cached {
		id, ok := strings.CutSuffix(e.Name(), ".previous")
		if !ok || !pluginIDRe.MatchString(id) || !e.IsDir() {
			continue
		}
		dst := filepath.Join(s.AppsRoot, id)
		if _, err := os.Lstat(dst); errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(filepath.Join(s.CacheRoot, e.Name()), dst); err != nil {
				return nil, err
			}
		}
	}
	s.sweepStaleStagesLocked()
	entries, err := os.ReadDir(s.AppsRoot)
	if err != nil {
		return nil, err
	}
	s.stateMu.Lock()
	disabled, stateErr := s.loadDisabledLocked()
	s.stateMu.Unlock()
	var out []InstalledApp
	for _, e := range entries {
		if !e.IsDir() || !pluginIDRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(s.AppsRoot, e.Name())
		m, err := LoadManifest(dir)
		if err != nil || m.ID != e.Name() {
			continue
		}
		dataDir := filepath.Join(s.DataRoot, m.ID)
		out = append(out, InstalledApp{
			Manifest:    *m,
			InstallDir:  dir,
			DataDir:     dataDir,
			Enabled:     stateErr == nil && m.ID != DiagnosticsPluginID && !disabled[m.ID],
			InstalledAt: pluginInstalledAt(dir),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out, nil
}

// PermissionEscalationError is returned when installing or updating a plugin
// would grant sensitive permissions that were not explicitly approved or replace
// an existing plugin from a different installation source.
type PermissionEscalationError struct {
	PluginID      string
	Added         []Permission
	SourceChanged bool
}

func (e *PermissionEscalationError) Error() string {
	names := make([]string, 0, len(e.Added))
	for _, p := range e.Added {
		names = append(names, string(p))
	}
	if e.SourceChanged && len(names) == 0 {
		return fmt.Sprintf("plugin %s comes from a different installation source and requires confirmation", e.PluginID)
	}
	if e.SourceChanged {
		return fmt.Sprintf("plugin %s comes from a different installation source and requests permissions: %s", e.PluginID, strings.Join(names, ", "))
	}
	return fmt.Sprintf("plugin %s requests unapproved permissions: %s", e.PluginID, strings.Join(names, ", "))
}

type installPolicy struct {
	allowReplace       bool
	enforceSourceMatch bool
	enforceApproved    bool
	approvedSensitive  []Permission
	forbidEscalation   bool
	expectedID         string
	expectedVersion    string
	expectedDigest     string
}

// StagedPlugin holds a validated candidate plugin package staged on disk
// awaiting user permission confirmation. Call Cleanup when discarding it;
// CommitStagedPlugin cleans up automatically.
type StagedPlugin struct {
	Manifest      Manifest
	Previous      *Manifest
	Source        string
	StagingDir    string
	PluginRoot    string
	ContentDigest string
	CatalogEntry  *StoreAppEntry
	IsUpdate      bool
	SourceChanged bool
	Sensitive     []Permission
	Added         []Permission
	cleanupFn     func()
}

// NeedsConfirmation reports whether committing p requires explicit user consent.
func (p *StagedPlugin) NeedsConfirmation() bool {
	if p == nil {
		return false
	}
	if p.SourceChanged {
		return true
	}
	if p.IsUpdate {
		return len(p.Added) > 0
	}
	return len(p.Sensitive) > 0
}

// RequestedPermissions returns the sensitive permissions that need user consent.
func (p *StagedPlugin) RequestedPermissions() []Permission {
	if p == nil {
		return nil
	}
	if p.SourceChanged || !p.IsUpdate {
		return append([]Permission(nil), p.Sensitive...)
	}
	return append([]Permission(nil), p.Added...)
}

// Cleanup removes the temporary staging directory if still present.
func (p *StagedPlugin) Cleanup() {
	if p == nil || p.StagingDir == "" {
		return
	}
	if p.cleanupFn != nil {
		p.cleanupFn()
		p.cleanupFn = nil
	}
	_ = os.RemoveAll(p.StagingDir)
	p.StagingDir = ""
	p.PluginRoot = ""
}

// InstallEntry installs a catalog StoreAppEntry via a builtin seed or an HTTPS archive with SHA256 check,
// and increments the persistent download counter on success. Non-interactive replacements with source changes,
// unrecorded previous sources, or added permissions are rejected.
func (s *StoreManager) InstallEntry(ctx context.Context, entry StoreAppEntry) (*InstalledApp, error) {
	inst, err := s.installEntryCore(ctx, entry, installPolicy{})
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

// InstallEntryExplicit installs a catalog StoreAppEntry triggered by an explicit in-app user click.
// It validates the candidate package without running it and replaces any existing plugin with the same ID
// even when the install source changed, the previous source is unrecorded, or permissions were added.
func (s *StoreManager) InstallEntryExplicit(ctx context.Context, entry StoreAppEntry) (*InstalledApp, error) {
	inst, err := s.installEntryCore(ctx, entry, installPolicy{allowReplace: true})
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

// InstallEntryConfirmed installs or updates a catalog entry after verifying
// under lock that its sensitive permissions are a subset of approvedPermissions
// and that an unapproved cross-source replacement is rejected.
func (s *StoreManager) InstallEntryConfirmed(ctx context.Context, entry StoreAppEntry, approvedPermissions []Permission, allowSourceChange bool) (*InstalledApp, error) {
	inst, err := s.installEntryCore(ctx, entry, installPolicy{
		allowReplace:       true,
		enforceSourceMatch: !allowSourceChange,
		enforceApproved:    true,
		approvedSensitive:  approvedPermissions,
	})
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

// UpdateEntryNoEscalation updates an installed catalog entry only if it comes
// from the same installation source and introduces no new sensitive permissions.
func (s *StoreManager) UpdateEntryNoEscalation(ctx context.Context, entry StoreAppEntry) (*InstalledApp, error) {
	inst, err := s.installEntryCore(ctx, entry, installPolicy{
		allowReplace:       true,
		enforceSourceMatch: true,
		forbidEscalation:   true,
	})
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

func catalogEntrySource(entry StoreAppEntry) (string, error) {
	u := strings.TrimSpace(entry.DownloadURL)
	if !pluginIDRe.MatchString(entry.ID) {
		return "", fmt.Errorf("invalid catalog plugin id %q", entry.ID)
	}
	if strings.HasPrefix(u, "builtin://") {
		id := strings.TrimPrefix(u, "builtin://")
		if id != entry.ID {
			return "", errors.New("builtin source does not match catalog plugin id")
		}
		return "builtin:" + id, nil
	}
	archiveURL, err := parseArchiveURL(u)
	if err != nil {
		return "", err
	}
	source := "catalog-url:" + archiveURL.String()
	if repo := githubArchiveRepo(archiveURL); repo != "" {
		if !strings.EqualFold(repo, entry.Repo) {
			return "", errors.New("catalog archive URL does not match its GitHub repository")
		}
		source = "catalog-github:" + repo
	}
	return source, nil
}

// MatchesEntrySource reports whether an already-installed plugin matches the
// recorded installation provenance of catalog entry.
func (s *StoreManager) MatchesEntrySource(installed InstalledApp, entry StoreAppEntry) bool {
	if installed.InstallDir == "" {
		return false
	}
	source, err := catalogEntrySource(entry)
	if err != nil {
		return false
	}
	return sourceMatchesInstalled(installed.InstallDir, source, &entry)
}

func (s *StoreManager) installEntryCore(ctx context.Context, entry StoreAppEntry, policy installPolicy) (*InstalledApp, error) {
	u := strings.TrimSpace(entry.DownloadURL)
	if !pluginIDRe.MatchString(entry.ID) {
		return nil, fmt.Errorf("invalid catalog plugin id %q", entry.ID)
	}
	if strings.HasPrefix(u, "builtin://") {
		id := strings.TrimPrefix(u, "builtin://")
		if id != entry.ID {
			return nil, errors.New("builtin source does not match catalog plugin id")
		}
		return s.installBuiltin(id, &entry, policy)
	}
	archiveURL, err := parseArchiveURL(u)
	if err != nil {
		return nil, err
	}
	sum, err := hex.DecodeString(strings.TrimSpace(entry.SHA256))
	if err != nil || len(sum) != sha256.Size {
		return nil, errors.New("catalog archive requires a valid SHA256")
	}
	source := "catalog-url:" + archiveURL.String()
	if repo := githubArchiveRepo(archiveURL); repo != "" {
		if !strings.EqualFold(repo, entry.Repo) {
			return nil, errors.New("catalog archive URL does not match its GitHub repository")
		}
		source = "catalog-github:" + repo
	}
	return s.installArchive(ctx, archiveURL, entry.SHA256, source, &entry, policy)
}

// InstallFromSource supports local directory paths, archive URLs, or Magpie-style GitHub shorthand (owner/repo or github:owner/repo).
// Non-interactive replacements with source changes, unrecorded previous sources, or added permissions are rejected.
func (s *StoreManager) InstallFromSource(ctx context.Context, spec string) (*InstalledApp, error) {
	inst, err := s.installFromSourceCore(ctx, spec, installPolicy{})
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

// InstallFromSourceExplicit installs a manual plugin source triggered by an explicit in-app user click.
// It validates the candidate package without running it and replaces any existing plugin with the same ID
// even when the install source changed, the previous source is unrecorded, or permissions were added.
func (s *StoreManager) InstallFromSourceExplicit(ctx context.Context, spec string) (*InstalledApp, error) {
	inst, err := s.installFromSourceCore(ctx, spec, installPolicy{allowReplace: true})
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

// StageManualSource fetches and validates a manual plugin source into a
// temporary staging directory without installing or running it, so the caller
// can review requested or newly added sensitive permissions before committing.
func (s *StoreManager) StageManualSource(ctx context.Context, spec string) (*StagedPlugin, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("empty plugin source")
	}
	if strings.HasPrefix(spec, "builtin://") {
		return s.stageBuiltin(strings.TrimPrefix(spec, "builtin://"), nil)
	}
	if strings.HasPrefix(spec, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			spec = filepath.Join(home, spec[2:])
		}
	}
	if strings.HasPrefix(spec, "file://") {
		spec = strings.TrimPrefix(spec, "file://")
	}
	if info, err := os.Stat(spec); err == nil && info.IsDir() {
		srcDir, err := filepath.Abs(spec)
		if err != nil {
			return nil, err
		}
		srcDir, err = filepath.EvalSymlinks(srcDir)
		if err != nil {
			return nil, err
		}
		return s.stageDirectoryCopy(srcDir, "directory:"+srcDir, nil)
	}
	if strings.HasPrefix(spec, "https://github.com/") {
		trimmed := strings.TrimSuffix(strings.TrimPrefix(spec, "https://github.com/"), "/")
		if repoSlugRe.MatchString(trimmed) {
			spec = "github:" + trimmed
		}
	}
	if strings.HasPrefix(spec, "github:") {
		spec = strings.TrimPrefix(spec, "github:")
	}
	if validRepoSlug(spec) {
		slug := strings.ToLower(spec)
		u, err := parseArchiveURL("https://codeload.github.com/" + slug + "/tar.gz/HEAD")
		if err != nil {
			return nil, err
		}
		return s.stageArchive(ctx, u, "", "github:"+slug, nil)
	}
	if strings.HasPrefix(spec, "https://") || strings.HasPrefix(spec, "http://") {
		u, err := parseArchiveURL(spec)
		if err != nil {
			return nil, err
		}
		return s.stageArchive(ctx, u, "", "url:"+u.String(), nil)
	}
	cat := s.LoadCachedOrSeedCatalog()
	for _, app := range cat.Apps {
		if app.ID == spec {
			return s.stageEntry(ctx, app)
		}
	}
	return nil, fmt.Errorf("unsupported plugin source %q", spec)
}

// CommitStagedPlugin commits a previously staged plugin package after user
// confirmation and always cleans up the staging directory.
func (s *StoreManager) CommitStagedPlugin(staged *StagedPlugin, approvedPermissions []Permission) (*InstalledApp, error) {
	if staged == nil || strings.TrimSpace(staged.PluginRoot) == "" || strings.TrimSpace(staged.ContentDigest) == "" {
		return nil, errors.New("staged plugin package is no longer available")
	}
	defer staged.Cleanup()
	inst, err := s.installDirectory(staged.PluginRoot, staged.Source, staged.CatalogEntry, installPolicy{
		allowReplace:       true,
		enforceSourceMatch: !staged.SourceChanged,
		enforceApproved:    true,
		approvedSensitive:  approvedPermissions,
		expectedID:         staged.Manifest.ID,
		expectedVersion:    staged.Manifest.Version,
		expectedDigest:     staged.ContentDigest,
	})
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

func (s *StoreManager) stageEntry(ctx context.Context, entry StoreAppEntry) (*StagedPlugin, error) {
	u := strings.TrimSpace(entry.DownloadURL)
	if !pluginIDRe.MatchString(entry.ID) {
		return nil, fmt.Errorf("invalid catalog plugin id %q", entry.ID)
	}
	entryCopy := entry
	if strings.HasPrefix(u, "builtin://") {
		id := strings.TrimPrefix(u, "builtin://")
		if id != entry.ID {
			return nil, errors.New("builtin source does not match catalog plugin id")
		}
		return s.stageBuiltin(id, &entryCopy)
	}
	archiveURL, err := parseArchiveURL(u)
	if err != nil {
		return nil, err
	}
	sum, err := hex.DecodeString(strings.TrimSpace(entry.SHA256))
	if err != nil || len(sum) != sha256.Size {
		return nil, errors.New("catalog archive requires a valid SHA256")
	}
	source := "catalog-url:" + archiveURL.String()
	if repo := githubArchiveRepo(archiveURL); repo != "" {
		if !strings.EqualFold(repo, entry.Repo) {
			return nil, errors.New("catalog archive URL does not match its GitHub repository")
		}
		source = "catalog-github:" + repo
	}
	return s.stageArchive(ctx, archiveURL, entry.SHA256, source, &entryCopy)
}

func (s *StoreManager) finalizeStagedPlugin(stagingDir, pluginRoot, source string, entry *StoreAppEntry) (*StagedPlugin, error) {
	m, err := validateCandidateDir(pluginRoot, entry)
	if err != nil {
		s.unregisterStageDir(stagingDir)
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	digest, err := hashPluginDir(pluginRoot)
	if err != nil {
		s.unregisterStageDir(stagingDir)
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	staged := &StagedPlugin{
		Manifest:      *m,
		Source:        source,
		StagingDir:    stagingDir,
		PluginRoot:    pluginRoot,
		ContentDigest: digest,
		CatalogEntry:  entry,
		Sensitive:     SensitivePermissions(m.Permissions),
		cleanupFn: func() {
			s.unregisterStageDir(stagingDir)
		},
	}
	dstDir := filepath.Join(s.AppsRoot, m.ID)
	if prev, err := LoadManifest(dstDir); err == nil && prev != nil && prev.ID == m.ID {
		staged.Previous = prev
		staged.IsUpdate = true
		staged.SourceChanged = !sourceMatchesInstalled(dstDir, source, entry)
		if staged.SourceChanged {
			staged.Added = append([]Permission(nil), staged.Sensitive...)
		} else {
			staged.Added = AddedSensitivePermissions(prev.Permissions, m.Permissions)
		}
	}
	return staged, nil
}

func (s *StoreManager) stageDirectoryCopy(srcDir, source string, entry *StoreAppEntry) (*StagedPlugin, error) {
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	stagingDir, err := os.MkdirTemp(s.CacheRoot, "stage-dir-*")
	if err != nil {
		return nil, err
	}
	s.registerStageDir(stagingDir)
	realStaging, err := filepath.EvalSymlinks(stagingDir)
	if err != nil {
		s.unregisterStageDir(stagingDir)
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	if rel, err := filepath.Rel(srcDir, realStaging); err == nil && filepath.IsLocal(rel) {
		s.unregisterStageDir(stagingDir)
		_ = os.RemoveAll(stagingDir)
		return nil, errors.New("plugin source contains the installation staging directory")
	}
	pluginRoot := filepath.Join(stagingDir, "candidate")
	if err := copyDirRecursive(srcDir, pluginRoot); err != nil {
		s.unregisterStageDir(stagingDir)
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	return s.finalizeStagedPlugin(stagingDir, pluginRoot, source, entry)
}

func (s *StoreManager) stageArchive(ctx context.Context, archiveURL *url.URL, expectedSHA256, source string, entry *StoreAppEntry) (*StagedPlugin, error) {
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	data, err := s.fetchVerifiedArchiveBytes(ctx, archiveURL, expectedSHA256)
	if err != nil {
		return nil, err
	}
	stagingDir, err := os.MkdirTemp(s.CacheRoot, "stage-arc-*")
	if err != nil {
		return nil, err
	}
	s.registerStageDir(stagingDir)
	extractDir := filepath.Join(stagingDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		s.unregisterStageDir(stagingDir)
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	if err := ExtractPluginArchive(data, archiveURL.Path, extractDir); err != nil {
		s.unregisterStageDir(stagingDir)
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	pluginRoot, err := FindManifestRoot(extractDir)
	if err != nil {
		s.unregisterStageDir(stagingDir)
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	return s.finalizeStagedPlugin(stagingDir, pluginRoot, source, entry)
}

func (s *StoreManager) stageBuiltin(id string, entry *StoreAppEntry) (*StagedPlugin, error) {
	builtinMu.RLock()
	seed, ok := builtinSeeds[id]
	builtinMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("builtin plugin %q not registered", id)
	}
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	stagingDir, err := os.MkdirTemp(s.CacheRoot, "stage-seed-*")
	if err != nil {
		return nil, err
	}
	s.registerStageDir(stagingDir)
	pluginRoot := filepath.Join(stagingDir, "candidate")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		s.unregisterStageDir(stagingDir)
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	err = fs.WalkDir(seed.FS, seed.Sub, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(seed.Sub, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dst := filepath.Join(pluginRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := fs.ReadFile(seed.FS, path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(dst, ".py") || strings.HasSuffix(dst, ".sh") {
			mode = 0o755
		}
		return os.WriteFile(dst, b, mode)
	})
	if err != nil {
		s.unregisterStageDir(stagingDir)
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	return s.finalizeStagedPlugin(stagingDir, pluginRoot, "builtin:"+id, entry)
}

func (s *StoreManager) installFromSourceCore(ctx context.Context, spec string, policy installPolicy) (*InstalledApp, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("empty plugin source")
	}
	if strings.HasPrefix(spec, "builtin://") {
		return s.installBuiltin(strings.TrimPrefix(spec, "builtin://"), nil, policy)
	}
	if strings.HasPrefix(spec, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			spec = filepath.Join(home, spec[2:])
		}
	}
	if strings.HasPrefix(spec, "file://") {
		spec = strings.TrimPrefix(spec, "file://")
	}
	if info, err := os.Stat(spec); err == nil && info.IsDir() {
		return s.installFromDirectoryCore(spec, policy)
	}
	if strings.HasPrefix(spec, "https://github.com/") {
		trimmed := strings.TrimSuffix(strings.TrimPrefix(spec, "https://github.com/"), "/")
		if repoSlugRe.MatchString(trimmed) {
			spec = "github:" + trimmed
		}
	}
	if strings.HasPrefix(spec, "github:") {
		spec = strings.TrimPrefix(spec, "github:")
	}
	if validRepoSlug(spec) {
		return s.installFromGitHubRepo(ctx, spec, policy)
	}
	if strings.HasPrefix(spec, "https://") || strings.HasPrefix(spec, "http://") {
		return s.installFromArchiveURLCore(ctx, spec, "", policy)
	}
	// An explicitly entered catalog ID uses the same source checks as the store.
	cat := s.LoadCachedOrSeedCatalog()
	for _, app := range cat.Apps {
		if app.ID == spec {
			return s.installEntryCore(ctx, app, policy)
		}
	}
	return nil, fmt.Errorf("unsupported plugin source %q", spec)
}

func (s *StoreManager) installFromGitHubRepo(ctx context.Context, slug string, policy installPolicy) (*InstalledApp, error) {
	slug = strings.ToLower(slug)
	u, err := parseArchiveURL("https://codeload.github.com/" + slug + "/tar.gz/HEAD")
	if err != nil {
		return nil, err
	}
	return s.installArchive(ctx, u, "", "github:"+slug, nil, policy)
}

func (s *StoreManager) InstallFromArchiveURL(ctx context.Context, rawURL string, expectedSHA256 string) (*InstalledApp, error) {
	return s.installFromArchiveURLCore(ctx, rawURL, expectedSHA256, installPolicy{})
}

func (s *StoreManager) installFromArchiveURLCore(ctx context.Context, rawURL string, expectedSHA256 string, policy installPolicy) (*InstalledApp, error) {
	u, err := parseArchiveURL(rawURL)
	if err != nil {
		return nil, err
	}
	return s.installArchive(ctx, u, expectedSHA256, "url:"+u.String(), nil, policy)
}

func validRepoSlug(slug string) bool {
	owner, repo, ok := strings.Cut(slug, "/")
	return ok && owner != "." && owner != ".." && repo != "." && repo != ".." && repoSlugRe.MatchString(slug)
}

func parseArchiveURL(raw string) (*url.URL, error) {
	if len(raw) > 4096 {
		return nil, errors.New("plugin archive URL is too long")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, errArchiveInvalidURL
	}
	u.Host = strings.ToLower(u.Host)
	return u, nil
}

func githubArchiveRepo(u *url.URL) string {
	if path.Clean(u.Path) != u.Path || u.EscapedPath() != (&url.URL{Path: u.Path}).EscapedPath() {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	switch u.Host {
	case "github.com", "codeload.github.com":
	case "api.github.com":
		if len(parts) == 0 || parts[0] != "repos" {
			return ""
		}
		parts = parts[1:]
	default:
		return ""
	}
	if len(parts) < 3 {
		return ""
	}
	slug := parts[0] + "/" + parts[1]
	if !validRepoSlug(slug) {
		return ""
	}
	return strings.ToLower(slug)
}

func isGitHubReleaseAssetURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.Host != "github.com" || githubArchiveRepo(u) == "" {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	return len(parts) >= 6 && parts[2] == "releases" && parts[3] == "download" && parts[4] != "" && parts[5] != ""
}

func verifyArchiveSHA256(data []byte, expectedSHA256 string) error {
	expected := strings.TrimSpace(expectedSHA256)
	if expected == "" {
		return nil
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, got)
	}
	return nil
}

func isArchivePolicyError(err error) bool {
	return errors.Is(err, errArchiveTooManyRedirects) ||
		errors.Is(err, errArchiveRedirectSource) ||
		errors.Is(err, errCatalogRedirectSource) ||
		errors.Is(err, errArchiveInvalidURL)
}

func sanitizeHTTPError(err error, fallbackURL *url.URL) error {
	if err == nil {
		return nil
	}
	var netWrap *storeNetworkError
	if errors.As(err, &netWrap) {
		return err
	}
	host := ""
	if fallbackURL != nil {
		host = strings.ToLower(fallbackURL.Host)
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if isArchivePolicyError(ue.Err) {
			return ue.Err
		}
		if parsed, pErr := url.Parse(ue.URL); pErr == nil && parsed.Host != "" {
			host = strings.ToLower(parsed.Host)
		}
		return &storeNetworkError{host: host, err: ue.Err}
	}
	if isArchivePolicyError(err) {
		return err
	}
	return &storeNetworkError{host: host, err: err}
}

func isFastRetryableArchiveErr(err error) bool {
	if err == nil || isArchivePolicyError(err) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "forcibly closed") ||
		strings.Contains(msg, "http 5") ||
		strings.Contains(msg, "sha256 mismatch")
}

func (s *StoreManager) installArchive(ctx context.Context, archiveURL *url.URL, expectedSHA256, source string, entry *StoreAppEntry, policy installPolicy) (*InstalledApp, error) {
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	data, err := s.fetchVerifiedArchiveBytes(ctx, archiveURL, expectedSHA256)
	if err != nil {
		return nil, err
	}
	tmpExtract, err := os.MkdirTemp(s.CacheRoot, "extract-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpExtract)

	if err := ExtractPluginArchive(data, archiveURL.Path, tmpExtract); err != nil {
		return nil, err
	}
	pluginRoot, err := FindManifestRoot(tmpExtract)
	if err != nil {
		return nil, err
	}
	return s.installDirectory(pluginRoot, source, entry, policy)
}

func (s *StoreManager) fetchVerifiedArchiveBytes(ctx context.Context, archiveURL *url.URL, expectedSHA256 string) ([]byte, error) {
	tryFetch := func(targetURL *url.URL, freshConn bool) ([]byte, error) {
		data, err := s.fetchArchiveBytes(ctx, targetURL, archiveURL, freshConn)
		if err != nil {
			return nil, err
		}
		if err := verifyArchiveSHA256(data, expectedSHA256); err != nil {
			return nil, err
		}
		return data, nil
	}

	data, primaryErr := tryFetch(archiveURL, false)
	if primaryErr == nil {
		return data, nil
	}
	if ctx.Err() != nil {
		return nil, primaryErr
	}

	// Retry once on a fresh HTTP/1.1 connection for fast transient EOF/reset errors
	// (avoids stale keep-alive / HTTP/2 EOF from release-assets.githubusercontent.com or proxies).
	if isFastRetryableArchiveErr(primaryErr) {
		if data, retryErr := tryFetch(archiveURL, true); retryErr == nil {
			return data, nil
		} else {
			primaryErr = retryErr
		}
		if ctx.Err() != nil {
			return nil, primaryErr
		}
	}

	// Only fail over to mirrors when a pinned SHA-256 digest guarantees archive integrity.
	if strings.TrimSpace(expectedSHA256) != "" && isGitHubReleaseAssetURL(archiveURL) {
		for _, prefix := range archiveMirrorPrefixes {
			if ctx.Err() != nil {
				break
			}
			mirrorURL, err := parseArchiveURL(prefix + archiveURL.String())
			if err != nil {
				continue
			}
			if data, err := tryFetch(mirrorURL, true); err == nil {
				return data, nil
			}
		}
	}
	return nil, primaryErr
}

func (s *StoreManager) fetchArchiveBytes(ctx context.Context, targetURL, canonicalURL *url.URL, freshConn bool) ([]byte, error) {
	reqCtx, cancel := withStageTimeout(ctx, archiveAttemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Block-AppStore/1.0 (blockmd)")
	if freshConn {
		req.Close = true
	}

	var client http.Client
	if s.HTTP != nil {
		client = *s.HTTP
	} else {
		client = http.Client{Timeout: 20 * time.Second}
	}
	if freshConn {
		var baseTr *http.Transport
		if client.Transport == nil {
			baseTr, _ = http.DefaultTransport.(*http.Transport)
		} else {
			baseTr, _ = client.Transport.(*http.Transport)
		}
		if baseTr != nil {
			retryTr := baseTr.Clone()
			retryTr.DisableKeepAlives = true
			retryTr.ForceAttemptHTTP2 = false
			retryTr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
			defer retryTr.CloseIdleConnections()
			client.Transport = retryTr
		}
	}

	repo := githubArchiveRepo(canonicalURL)
	isMirror := targetURL.Host != canonicalURL.Host
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errArchiveTooManyRedirects
		}
		u, err := parseArchiveURL(next.URL.String())
		if err != nil {
			return err
		}
		if isMirror {
			if u.Host == targetURL.Host || (repo != "" && (githubArchiveRepo(u) == repo ||
				u.Host == "release-assets.githubusercontent.com" ||
				u.Host == "objects.githubusercontent.com")) {
				return nil
			}
		} else if repo != "" {
			// GitHub serves source archives and release assets on separate hosts.
			if githubArchiveRepo(u) == repo || u.Host == "release-assets.githubusercontent.com" ||
				u.Host == "objects.githubusercontent.com" {
				return nil
			}
		} else if u.Host == canonicalURL.Host {
			return nil
		}
		return errArchiveRedirectSource
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, sanitizeHTTPError(err, targetURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download plugin archive: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxArchiveBytes {
		return nil, errors.New("plugin archive exceeds download limit")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		errURL := targetURL
		if resp.Request != nil && resp.Request.URL != nil {
			errURL = resp.Request.URL
		}
		return nil, sanitizeHTTPError(err, errURL)
	}
	if int64(len(data)) > maxArchiveBytes {
		return nil, errors.New("plugin archive exceeds download limit")
	}
	return data, nil
}

func (s *StoreManager) installBuiltin(id string, entry *StoreAppEntry, policy installPolicy) (*InstalledApp, error) {
	builtinMu.RLock()
	seed, ok := builtinSeeds[id]
	builtinMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("builtin plugin %q not registered", id)
	}
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	tmpExtract, err := os.MkdirTemp(s.CacheRoot, "seed-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpExtract)

	err = fs.WalkDir(seed.FS, seed.Sub, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(seed.Sub, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dst := filepath.Join(tmpExtract, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := fs.ReadFile(seed.FS, path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(dst, ".py") || strings.HasSuffix(dst, ".sh") {
			mode = 0o755
		}
		return os.WriteFile(dst, b, mode)
	})
	if err != nil {
		return nil, err
	}
	return s.installDirectory(tmpExtract, "builtin:"+id, entry, policy)
}

func (s *StoreManager) InstallFromDirectory(srcDir string) (*InstalledApp, error) {
	return s.installFromDirectoryCore(srcDir, installPolicy{})
}

func (s *StoreManager) installFromDirectoryCore(srcDir string, policy installPolicy) (*InstalledApp, error) {
	srcDir, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, err
	}
	srcDir, err = filepath.EvalSymlinks(srcDir)
	if err != nil {
		return nil, err
	}
	return s.installDirectory(srcDir, "directory:"+srcDir, nil, policy)
}

func validateCandidateDir(tmpDir string, entry *StoreAppEntry) (*Manifest, error) {
	m, err := LoadManifest(tmpDir)
	if err != nil {
		return nil, err
	}
	if m.ID == DiagnosticsPluginID {
		return nil, errors.New("Performance & Usage is built into Block")
	}
	if entry != nil {
		if m.ID != entry.ID || m.Version != strings.TrimPrefix(entry.Version, "v") {
			return nil, fmt.Errorf("plugin manifest differs from catalog entry %s %s", entry.ID, entry.Version)
		}
		if m.Repo != "" && entry.Repo != "" && !strings.EqualFold(m.Repo, entry.Repo) {
			return nil, fmt.Errorf("plugin repository differs from catalog entry %s", entry.ID)
		}
		if !slices.Equal(slices.Sorted(slices.Values(m.Permissions)), slices.Sorted(slices.Values(entry.Permissions))) {
			return nil, fmt.Errorf("plugin permissions differ from catalog entry %s", entry.ID)
		}
	}
	switch {
	case m.Entry.JS != "":
		if err := wasmrt.ValidateEntryFile(tmpDir, m.Entry.JS, 0, ".js", ".mjs"); err != nil {
			return nil, fmt.Errorf("manifest.entry.js: %w", err)
		}
	case m.Entry.Wasm != "":
		if err := wasmrt.ValidateEntryFile(tmpDir, m.Entry.Wasm, wasmrt.MaxWasmEntryBytes, ".wasm"); err != nil {
			return nil, fmt.Errorf("manifest.entry.wasm: %w", err)
		}
	case len(m.Entry.Command) > 0:
		// Resolve bare PATH interpreters (python3) only when the runtime actually
		// starts: probing them during install blocks the store on the host
		// environment and caches an interpreter the plugin has not run under yet.
		if !isPython3Command(m.Entry.Command[0]) {
			if _, err := pluginCommand(tmpDir, m.Entry.Command[0]); err != nil {
				return nil, err
			}
		}
	}
	if m.Contributes != nil && len(m.Contributes.Languages) > 0 {
		if err := ValidatePluginLanguages(tmpDir, m); err != nil {
			return nil, err
		}
	}
	if m.Contributes != nil && len(m.Contributes.FileIcons) > 0 {
		if err := ValidatePluginFileIcons(tmpDir, m); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (s *StoreManager) installDirectory(srcDir, source string, entry *StoreAppEntry, policy installPolicy) (*InstalledApp, error) {
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	s.installMu.Lock()
	defer s.installMu.Unlock()
	staging, err := os.MkdirTemp(s.AppsRoot, ".install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	realStaging, err := filepath.EvalSymlinks(staging)
	if err != nil {
		return nil, err
	}
	if rel, err := filepath.Rel(srcDir, realStaging); err == nil && filepath.IsLocal(rel) {
		return nil, errors.New("plugin source contains the installation staging directory")
	}
	tmpDir := filepath.Join(staging, "candidate")
	if err := copyDirRecursive(srcDir, tmpDir); err != nil {
		return nil, err
	}
	m, err := validateCandidateDir(tmpDir, entry)
	if err != nil {
		return nil, err
	}
	if policy.expectedID != "" && m.ID != policy.expectedID {
		return nil, fmt.Errorf("staged plugin id changed from %q to %q before commit", policy.expectedID, m.ID)
	}
	if policy.expectedVersion != "" && m.Version != policy.expectedVersion {
		return nil, fmt.Errorf("staged plugin version changed from %q to %q before commit", policy.expectedVersion, m.Version)
	}
	if policy.expectedDigest != "" {
		actualDigest, err := hashPluginDir(tmpDir)
		if err != nil {
			return nil, err
		}
		if actualDigest != policy.expectedDigest {
			return nil, errors.New("staged plugin contents changed after review; please retry installation")
		}
	}

	dstDir := filepath.Join(s.AppsRoot, m.ID)
	backup := filepath.Join(s.CacheRoot, m.ID+".previous")
	sourceHash := sha256.Sum256([]byte(source))
	sourceRecord := "v1:" + hex.EncodeToString(sourceHash[:])
	provenance, err := os.OpenFile(filepath.Join(tmpDir, installSourceFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	_, writeErr := provenance.WriteString(sourceRecord)
	if err := errors.Join(writeErr, provenance.Close()); err != nil {
		return nil, err
	}

	info, err := os.Lstat(dstDir)
	if errors.Is(err, os.ErrNotExist) {
		if previous, previousErr := os.Lstat(backup); previousErr == nil && previous.IsDir() {
			if err := os.Rename(backup, dstDir); err != nil {
				return nil, err
			}
			info, err = os.Lstat(dstDir)
		}
	}
	wasInstalled := err == nil
	if !wasInstalled && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	var previous *Manifest
	if wasInstalled {
		if !info.IsDir() {
			return nil, errors.New("existing plugin path must be a directory")
		}
		prev, err := LoadManifest(dstDir)
		if err != nil {
			return nil, err
		}
		if prev.ID != m.ID {
			return nil, errors.New("installed manifest does not match its plugin directory")
		}
		previous = prev
		if !policy.allowReplace {
			root, err := os.OpenRoot(dstDir)
			if err != nil {
				return nil, err
			}
			installedSource, sourceErr := readPluginFile(root, installSourceFile, int64(len(sourceRecord)))
			root.Close()
			if sourceErr != nil {
				return nil, errors.New("existing plugin has no verifiable install source; uninstall it before reinstalling")
			}
			if string(installedSource) != sourceRecord {
				return nil, errors.New("plugin replacement source differs from its original installation")
			}
			for _, permission := range m.Permissions {
				if !previous.HasPermission(permission) {
					return nil, fmt.Errorf("replacement adds permission %q; uninstall before reinstalling with the new permissions", permission)
				}
			}
		} else if policy.enforceSourceMatch {
			if !sourceMatchesInstalled(dstDir, source, entry) {
				return nil, &PermissionEscalationError{
					PluginID:      m.ID,
					Added:         SensitivePermissions(m.Permissions),
					SourceChanged: true,
				}
			}
		}
	}

	if policy.enforceApproved {
		if unapproved := AddedSensitivePermissions(policy.approvedSensitive, m.Permissions); len(unapproved) > 0 {
			return nil, &PermissionEscalationError{PluginID: m.ID, Added: unapproved}
		}
	}
	if policy.forbidEscalation {
		var base []Permission
		if previous != nil {
			base = previous.Permissions
		}
		if added := AddedSensitivePermissions(base, m.Permissions); len(added) > 0 {
			return nil, &PermissionEscalationError{PluginID: m.ID, Added: added}
		}
	}

	return s.commitInstallDirLocked(m, tmpDir, dstDir, backup, wasInstalled)
}

func (s *StoreManager) commitInstallDirLocked(m *Manifest, tmpDir, dstDir, backup string, wasInstalled bool) (*InstalledApp, error) {
	if wasInstalled && s.beforeReplace != nil {
		if err := s.beforeReplace(m.ID); err != nil {
			return nil, err
		}
	}
	if !wasInstalled {
		if err := s.SetEnabled(m.ID, m.IsDeclarativeOnly()); err != nil {
			return nil, err
		}
	}
	if wasInstalled {
		if err := os.RemoveAll(backup); err != nil {
			return nil, err
		}
		if err := os.Rename(dstDir, backup); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(tmpDir, dstDir); err != nil {
		if wasInstalled {
			return nil, errors.Join(err, os.Rename(backup, dstDir))
		}
		return nil, err
	}
	_ = os.RemoveAll(backup)
	return &InstalledApp{
		Manifest:    *m,
		InstallDir:  dstDir,
		DataDir:     filepath.Join(s.DataRoot, m.ID),
		Enabled:     s.IsEnabled(m.ID),
		InstalledAt: pluginInstalledAt(dstDir),
	}, nil
}

func pluginInstalledAt(dir string) time.Time {
	if info, err := os.Lstat(filepath.Join(dir, installSourceFile)); err == nil && info.Mode().IsRegular() {
		return info.ModTime()
	}
	if info, err := os.Stat(dir); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

func installSourceRecord(source string) string {
	sum := sha256.Sum256([]byte(source))
	return "v1:" + hex.EncodeToString(sum[:])
}

func sourceMatchesInstalled(dstDir, source string, entry *StoreAppEntry) bool {
	candidateRecord := installSourceRecord(source)
	root, err := os.OpenRoot(dstDir)
	if err != nil {
		return false
	}
	defer root.Close()
	installedSource, sourceErr := readPluginFile(root, installSourceFile, int64(len(candidateRecord)))
	if sourceErr != nil {
		return false
	}
	recorded := string(installedSource)
	if recorded == candidateRecord {
		return true
	}
	if entry != nil && entry.Verified && IsOfficialRepo(entry.Repo) && recorded == installSourceRecord("builtin:"+entry.ID) {
		return true
	}
	return false
}

// hashPluginDir computes a deterministic SHA-256 digest over every path, entry
// type, executable bit, and file content inside dir.
func hashPluginDir(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && (d.Name() == "__pycache__" || d.Name() == ".git") {
			return fs.SkipDir
		}
		slashRel := filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin package must not contain symlinks (%s)", slashRel)
		}
		if d.IsDir() {
			fmt.Fprintf(h, "D:%d:%s\n", len(slashRel), slashRel)
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("plugin package contains unsupported entry %s", slashRel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		execBit := 0
		if info.Mode().Perm()&0o111 != 0 {
			execBit = 1
		}
		fmt.Fprintf(h, "F:%d:%s:%d:%d\n", len(slashRel), slashRel, execBit, info.Size())
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (inst InstalledApp) credentialScope() string {
	root, err := os.OpenRoot(inst.InstallDir)
	if err != nil {
		return ""
	}
	defer root.Close()
	const recordSize = len("v1:") + sha256.Size*2
	raw, err := readPluginFile(root, installSourceFile, int64(recordSize))
	record := string(raw)
	if err != nil || len(raw) != recordSize || !strings.HasPrefix(record, "v1:") {
		return ""
	}
	if _, err := hex.DecodeString(record[len("v1:"):]); err != nil {
		return ""
	}
	repo := strings.ToLower(strings.TrimSpace(inst.Manifest.Repo))
	if validRepoSlug(repo) && record == installSourceRecord("catalog-github:"+repo) {
		return installSourceRecord("github:" + repo)
	}
	return record
}

// MatchesOfficialSource reports whether an installed plugin was genuinely installed
// from the catalog's verified official repository (not a spoofed local directory copy).
func (s *StoreManager) MatchesOfficialSource(installed InstalledApp, entry StoreAppEntry) bool {
	if SupersededLanguagePluginIDs[entry.ID] && installed.Manifest.ID == entry.ID {
		entry = supersededLanguageCatalogEntry(entry.ID)
	}
	if !entry.Verified || !IsOfficialRepo(entry.Repo) {
		return false
	}
	return s.matchesRepoSource(installed, entry, false)
}

// The final store entries still identify the origin of previously installed packages.
func supersededLanguageCatalogEntry(id string) StoreAppEntry {
	if !SupersededLanguagePluginIDs[id] {
		return StoreAppEntry{}
	}
	const version = "v1.0.3"
	repo := "blockmd-plugins/" + id
	return StoreAppEntry{
		ID:          id,
		Repo:        repo,
		DownloadURL: "https://github.com/" + repo + "/releases/download/" + version + "/" + id + "-" + version + ".tar.gz",
		Verified:    true,
	}
}

// MatchesReviewedSource reports whether an installed plugin was genuinely installed
// from a trusted reviewed author's repository (not a spoofed local directory copy).
func (s *StoreManager) MatchesReviewedSource(installed InstalledApp, entry StoreAppEntry) bool {
	if !IsReviewedAuthorRepo(entry.Repo) {
		return false
	}
	return s.matchesRepoSource(installed, entry, true)
}

func (s *StoreManager) matchesRepoSource(installed InstalledApp, entry StoreAppEntry, allowMigratedOfficial bool) bool {
	if installed.InstallDir == "" {
		return false
	}
	wantRepo := strings.ToLower(strings.TrimSpace(entry.Repo))
	gotRepo := strings.ToLower(strings.TrimSpace(installed.Manifest.Repo))
	if wantRepo == "" {
		return false
	}
	repoMatches := gotRepo == wantRepo
	if !repoMatches && allowMigratedOfficial && strings.TrimSpace(entry.DownloadURL) != "" && IsOfficialRepo(gotRepo) {
		_, wantName, okW := strings.Cut(wantRepo, "/")
		_, gotName, okG := strings.Cut(gotRepo, "/")
		repoMatches = okW && okG && wantName == gotName
	}
	if !repoMatches {
		return false
	}
	root, err := os.OpenRoot(installed.InstallDir)
	if err != nil {
		return false
	}
	defer root.Close()
	raw, err := readPluginFile(root, installSourceFile, 128)
	if err != nil {
		// Pre-provenance catalog installations lack .block-install-source; trust
		// them only when the entry is backed by the catalog and its repo matches.
		return errors.Is(err, os.ErrNotExist) && strings.TrimSpace(entry.DownloadURL) != ""
	}
	recorded := strings.TrimSpace(string(raw))
	allowed := []string{
		installSourceRecord("catalog-github:" + wantRepo),
		installSourceRecord("github:" + wantRepo),
		installSourceRecord("builtin:" + entry.ID),
		wantRepo,
	}
	if u, err := parseArchiveURL(strings.TrimSpace(entry.DownloadURL)); err == nil {
		allowed = append(allowed, installSourceRecord("catalog-url:"+u.String()))
	}
	return slices.Contains(allowed, recorded)
}

func (s *StoreManager) Uninstall(pluginID string, purgeData bool) error {
	s.installMu.Lock()
	defer s.installMu.Unlock()
	if !pluginIDRe.MatchString(pluginID) {
		return fmt.Errorf("invalid plugin id %q", pluginID)
	}
	if err := os.RemoveAll(filepath.Join(s.CacheRoot, pluginID+".previous")); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.AppsRoot, pluginID)); err != nil {
		return err
	}
	s.stateMu.Lock()
	disabled, _ := s.loadDisabledLocked()
	if disabled[pluginID] {
		delete(disabled, pluginID)
		_ = s.saveDisabledLocked(disabled)
	}
	s.stateMu.Unlock()
	if purgeData {
		return os.RemoveAll(filepath.Join(s.DataRoot, pluginID))
	}
	return nil
}

func ParseStoreIndex(raw []byte) (*StoreIndex, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("store index must be a JSON object")
	}
	var catalog struct {
		StoreIndex
		SystemApps []StoreAppEntry `json:"system_apps"`
	}
	if err := json.Unmarshal(trimmed, &catalog); err != nil {
		return nil, err
	}
	idx := &catalog.StoreIndex
	if idx.App != TargetApp {
		return nil, fmt.Errorf("store index app mismatch: got %q, want %q", idx.App, TargetApp)
	}
	if idx.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf("store index schema_version mismatch: got %d, want %d", idx.SchemaVersion, CurrentSchemaVersion)
	}
	if idx.Apps == nil {
		return nil, errors.New("store index missing apps array")
	}
	if len(catalog.SystemApps) != 0 {
		current := *idx
		current.Apps = catalog.SystemApps
		return mergeStoreIndexes(&current, idx), nil
	}
	return idx, nil
}

// ExtractPluginArchive extracts a ZIP or gzip-compressed tar within the package limits.
func ExtractPluginArchive(data []byte, hintURL, destDir string) error {
	if len(data) > maxArchiveBytes {
		return errors.New("plugin archive exceeds download limit")
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer root.Close()
	budget := packageBudget{paths: make(map[string]struct{})}
	if strings.HasSuffix(strings.ToLower(hintURL), ".zip") || bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return fmt.Errorf("open zip: %w", err)
		}
		if len(zr.File) > maxPackagePaths {
			return errors.New("plugin archive contains too many entries")
		}
		for _, f := range zr.File {
			name, err := budget.addPath(f.Name)
			if err != nil {
				return err
			}
			switch f.Mode().Type() {
			case os.ModeDir:
				if f.UncompressedSize64 != 0 {
					return fmt.Errorf("unexpected data in plugin directory %q", f.Name)
				}
				err = root.MkdirAll(name, 0o755)
			case 0:
				if f.UncompressedSize64 > maxPackageFile {
					return fmt.Errorf("plugin file %q exceeds size limit", f.Name)
				}
				if err := budget.addFile(name, int64(f.UncompressedSize64)); err != nil {
					return err
				}
				in, openErr := f.Open()
				if openErr != nil {
					return openErr
				}
				err = errors.Join(writePackageFile(root, name, in, int64(f.UncompressedSize64), f.Mode()), in.Close())
			default:
				return fmt.Errorf("unsupported plugin archive entry %q", f.Name)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}

	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open tar.gz: %w", err)
	}
	defer gzr.Close()
	stream := &io.LimitedReader{R: gzr, N: maxExpandedTar + 1}
	tr := tar.NewReader(stream)
	for count := 0; ; count++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if count >= maxPackagePaths {
			return errors.New("plugin archive contains too many entries")
		}
		name, err := budget.addPath(hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if hdr.Size != 0 {
				return fmt.Errorf("unexpected data in plugin directory %q", hdr.Name)
			}
			err = root.MkdirAll(name, 0o755)
		case tar.TypeReg:
			if err := budget.addFile(name, hdr.Size); err != nil {
				return err
			}
			err = writePackageFile(root, name, tr, hdr.Size, os.FileMode(hdr.Mode))
		default:
			return fmt.Errorf("unsupported plugin archive entry %q", hdr.Name)
		}
		if err != nil {
			return err
		}
	}
	// Consume trailing data to check both the total expanded size and gzip integrity.
	if _, err := io.Copy(io.Discard, stream); err != nil {
		return err
	}
	if stream.N == 0 {
		return errors.New("plugin archive exceeds expanded stream limit")
	}
	return nil
}

type packageBudget struct {
	paths map[string]struct{}
	bytes int64
}

func (b *packageBudget) addPath(raw string) (string, error) {
	name := strings.TrimSuffix(strings.TrimPrefix(raw, "./"), "/")
	if raw == "./" {
		name = "."
	}
	if len(name) > 1024 || strings.Count(name, "/") >= 32 || strings.ContainsAny(name, "\\:") {
		return "", fmt.Errorf("unsupported plugin package path %q", raw)
	}
	local, err := filepath.Localize(name)
	if err != nil {
		return "", fmt.Errorf("invalid plugin package path %q: %w", raw, err)
	}
	for p := local; p != "."; p = filepath.Dir(p) {
		b.paths[p] = struct{}{}
		if len(b.paths) > maxPackagePaths {
			return "", errors.New("plugin package contains too many paths")
		}
	}
	return local, nil
}

func (b *packageBudget) addFile(name string, size int64) error {
	if name == "." || size < 0 || size > maxPackageFile {
		return fmt.Errorf("invalid or oversized plugin file %q", name)
	}
	if b.bytes+size > maxPackageBytes {
		return errors.New("plugin package exceeds total file size limit")
	}
	b.bytes += size
	return nil
}

func writePackageFile(root *os.Root, name string, src io.Reader, size int64, mode os.FileMode) error {
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	// Keep execute bits for bundled tools and discard special or writable mode bits.
	out, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644|mode&0o111)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(src, size+1))
	closeErr := out.Close()
	if copyErr == nil && n != size {
		copyErr = fmt.Errorf("plugin file %q differs from its declared size", name)
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = root.Remove(name)
		return err
	}
	return nil
}

// FindManifestRoot locates the manifest at the archive root or in one top-level folder.
func FindManifestRoot(extractDir string) (string, error) {
	var manifestRoot string
	if info, err := os.Lstat(filepath.Join(extractDir, "manifest.json")); err == nil && info.Mode().IsRegular() {
		manifestRoot = extractDir
	}
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		cand := filepath.Join(extractDir, e.Name())
		if info, err := os.Lstat(filepath.Join(cand, "manifest.json")); err == nil && info.Mode().IsRegular() {
			if manifestRoot != "" {
				return "", errors.New("archive contains multiple plugin manifests")
			}
			manifestRoot = cand
		}
	}
	if manifestRoot == "" {
		return "", errors.New("archive does not contain manifest.json")
	}
	return manifestRoot, nil
}

func copyDirRecursive(src, dst string) error {
	source, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := os.Mkdir(dst, 0o755); err != nil {
		return err
	}
	target, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer target.Close()
	budget := packageBudget{paths: make(map[string]struct{})}
	return fs.WalkDir(source.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "__pycache__" || entry.Name() == ".git") {
			return fs.SkipDir
		}
		name, err := budget.addPath(path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return target.MkdirAll(name, 0o755)
		}
		in, info, err := openPluginFile(source, name)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := budget.addFile(name, info.Size()); err != nil {
			return err
		}
		return writePackageFile(target, name, in, info.Size(), info.Mode())
	})
}
