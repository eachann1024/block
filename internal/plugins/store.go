package plugins

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	Icon           DuoIconSpec   `json:"icon"`
	MinHostVersion string        `json:"min_host_version,omitempty"`
}

// EffectiveCategory returns the canonical App Store category for e.
func (e StoreAppEntry) EffectiveCategory() Category {
	return ResolveCategory(e.Category, e.ID, len(e.FileExtensions) > 0)
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
	stateMu       sync.Mutex
	installMu     sync.Mutex
	beforeReplace func(pluginID string) error
}

func NewStoreManager(blockAppDataDir string) *StoreManager {
	urls := append([]string(nil), DefaultStoreIndexURLs...)
	if custom := strings.TrimSpace(os.Getenv("BLOCKMD_STORE_URL")); custom != "" {
		urls = append([]string{custom}, urls...)
	}
	return &StoreManager{
		AppsRoot:  filepath.Join(blockAppDataDir, "apps"),
		DataRoot:  filepath.Join(blockAppDataDir, "apps-data"),
		CacheRoot: filepath.Join(blockAppDataDir, "apps-cache"),
		IndexURLs: urls,
		HTTP:      &http.Client{Timeout: 20 * time.Second},
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

func (s *StoreManager) loadDisabledLocked() (map[string]bool, error) {
	out := map[string]bool{}
	root, err := os.OpenRoot(s.CacheRoot)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := readPluginFile(root, filepath.Base(s.disabledPluginsPath()), 1024*1024)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("read disabled plugin state: %w", err)
	}
	if out == nil {
		return nil, errors.New("disabled plugin state must be an object")
	}
	return out, nil
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
	next := deltas[pluginID]
	if raw, err := json.MarshalIndent(deltas, "", "  "); err == nil {
		_ = os.WriteFile(s.downloadCountsPath(), raw, 0o644)
	}
	return next
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
				return errors.New("plugin catalog redirect changed its source")
			}
			return nil
		}
		resp, err := client.Do(req)
		if err != nil {
			srcCancel()
			lastErr = err
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

// InstallEntry installs a catalog StoreAppEntry via a builtin seed or an HTTPS archive with SHA256 check,
// and increments the persistent download counter on success. Non-interactive replacements with source changes,
// unrecorded previous sources, or added permissions are rejected.
func (s *StoreManager) InstallEntry(ctx context.Context, entry StoreAppEntry) (*InstalledApp, error) {
	inst, err := s.installEntryCore(ctx, entry, false)
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

// InstallEntryExplicit installs a catalog StoreAppEntry triggered by an explicit in-app user click.
// It validates the candidate package without running it and replaces any existing plugin with the same ID
// even when the install source changed, the previous source is unrecorded, or permissions were added.
func (s *StoreManager) InstallEntryExplicit(ctx context.Context, entry StoreAppEntry) (*InstalledApp, error) {
	inst, err := s.installEntryCore(ctx, entry, true)
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

func (s *StoreManager) installEntryCore(ctx context.Context, entry StoreAppEntry, allowReplace bool) (*InstalledApp, error) {
	u := strings.TrimSpace(entry.DownloadURL)
	if !pluginIDRe.MatchString(entry.ID) {
		return nil, fmt.Errorf("invalid catalog plugin id %q", entry.ID)
	}
	if strings.HasPrefix(u, "builtin://") {
		id := strings.TrimPrefix(u, "builtin://")
		if id != entry.ID {
			return nil, errors.New("builtin source does not match catalog plugin id")
		}
		return s.installBuiltin(id, &entry, allowReplace)
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
	return s.installArchive(ctx, archiveURL, entry.SHA256, source, &entry, allowReplace)
}

// InstallFromSource supports local directory paths, archive URLs, or Magpie-style GitHub shorthand (owner/repo or github:owner/repo).
// Non-interactive replacements with source changes, unrecorded previous sources, or added permissions are rejected.
func (s *StoreManager) InstallFromSource(ctx context.Context, spec string) (*InstalledApp, error) {
	inst, err := s.installFromSourceCore(ctx, spec, false)
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

// InstallFromSourceExplicit installs a manual plugin source triggered by an explicit in-app user click.
// It validates the candidate package without running it and replaces any existing plugin with the same ID
// even when the install source changed, the previous source is unrecorded, or permissions were added.
func (s *StoreManager) InstallFromSourceExplicit(ctx context.Context, spec string) (*InstalledApp, error) {
	inst, err := s.installFromSourceCore(ctx, spec, true)
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

func (s *StoreManager) installFromSourceCore(ctx context.Context, spec string, allowReplace bool) (*InstalledApp, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("empty plugin source")
	}
	if strings.HasPrefix(spec, "builtin://") {
		return s.installBuiltin(strings.TrimPrefix(spec, "builtin://"), nil, allowReplace)
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
		return s.installFromDirectoryCore(spec, allowReplace)
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
		return s.installFromGitHubRepo(ctx, spec, allowReplace)
	}
	if strings.HasPrefix(spec, "https://") || strings.HasPrefix(spec, "http://") {
		return s.installFromArchiveURLCore(ctx, spec, "", allowReplace)
	}
	// An explicitly entered catalog ID uses the same source checks as the store.
	cat := s.LoadCachedOrSeedCatalog()
	for _, app := range cat.Apps {
		if app.ID == spec {
			return s.installEntryCore(ctx, app, allowReplace)
		}
	}
	return nil, fmt.Errorf("unsupported plugin source %q", spec)
}

func (s *StoreManager) installFromGitHubRepo(ctx context.Context, slug string, allowReplace bool) (*InstalledApp, error) {
	slug = strings.ToLower(slug)
	u, err := parseArchiveURL("https://codeload.github.com/" + slug + "/tar.gz/HEAD")
	if err != nil {
		return nil, err
	}
	return s.installArchive(ctx, u, "", "github:"+slug, nil, allowReplace)
}

func (s *StoreManager) InstallFromArchiveURL(ctx context.Context, rawURL string, expectedSHA256 string) (*InstalledApp, error) {
	return s.installFromArchiveURLCore(ctx, rawURL, expectedSHA256, false)
}

func (s *StoreManager) installFromArchiveURLCore(ctx context.Context, rawURL string, expectedSHA256 string, allowReplace bool) (*InstalledApp, error) {
	u, err := parseArchiveURL(rawURL)
	if err != nil {
		return nil, err
	}
	return s.installArchive(ctx, u, expectedSHA256, "url:"+u.String(), nil, allowReplace)
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
		return nil, errors.New("plugin archive requires an HTTPS URL without credentials or a fragment")
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

func (s *StoreManager) installArchive(ctx context.Context, archiveURL *url.URL, expectedSHA256, source string, entry *StoreAppEntry, allowReplace bool) (*InstalledApp, error) {
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	data, err := s.fetchArchiveBytes(ctx, archiveURL)
	if err != nil {
		return nil, err
	}
	if expectedSHA256 != "" {
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if !strings.EqualFold(got, strings.TrimSpace(expectedSHA256)) {
			return nil, fmt.Errorf("sha256 mismatch: expected %s, got %s", expectedSHA256, got)
		}
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
	return s.installDirectory(pluginRoot, source, entry, allowReplace)
}

func (s *StoreManager) fetchArchiveBytes(ctx context.Context, archiveURL *url.URL) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Block-AppStore/1.0 (blockmd)")
	client := *s.HTTP
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many plugin archive redirects")
		}
		u, err := parseArchiveURL(next.URL.String())
		if err != nil {
			return err
		}
		repo := githubArchiveRepo(archiveURL)
		if repo != "" {
			// GitHub serves source archives and release assets on separate hosts.
			if githubArchiveRepo(u) == repo || u.Host == "release-assets.githubusercontent.com" ||
				u.Host == "objects.githubusercontent.com" {
				return nil
			}
		} else if u.Host == archiveURL.Host {
			return nil
		}
		return errors.New("plugin archive redirect changed its source")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download plugin archive: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxArchiveBytes {
		return nil, errors.New("plugin archive exceeds download limit")
	}
	return readLimited(resp.Body, maxArchiveBytes)
}

func (s *StoreManager) installBuiltin(id string, entry *StoreAppEntry, allowReplace bool) (*InstalledApp, error) {
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
	return s.installDirectory(tmpExtract, "builtin:"+id, entry, allowReplace)
}

func (s *StoreManager) InstallFromDirectory(srcDir string) (*InstalledApp, error) {
	return s.installFromDirectoryCore(srcDir, false)
}

func (s *StoreManager) installFromDirectoryCore(srcDir string, allowReplace bool) (*InstalledApp, error) {
	srcDir, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, err
	}
	srcDir, err = filepath.EvalSymlinks(srcDir)
	if err != nil {
		return nil, err
	}
	return s.installDirectory(srcDir, "directory:"+srcDir, nil, allowReplace)
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
	if len(m.Entry.Command) > 0 {
		if _, err := pluginCommand(tmpDir, m.Entry.Command[0]); err != nil {
			return nil, err
		}
	}
	if m.Contributes != nil && len(m.Contributes.Languages) > 0 {
		if err := ValidatePluginLanguages(tmpDir, m); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (s *StoreManager) installDirectory(srcDir, source string, entry *StoreAppEntry, allowReplace bool) (*InstalledApp, error) {
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

	if wasInstalled {
		if !info.IsDir() {
			return nil, errors.New("existing plugin path must be a directory")
		}
		previous, err := LoadManifest(dstDir)
		if err != nil {
			return nil, err
		}
		if previous.ID != m.ID {
			return nil, errors.New("installed manifest does not match its plugin directory")
		}
		if !allowReplace {
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
