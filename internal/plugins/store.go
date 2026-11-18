package plugins

import (
	"archive/tar"
	"archive/zip"
	"block/internal/updater"
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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// StoreAppEntry describes an entry in the Block App Store catalog index (topic: blockmd-plugin).
type StoreAppEntry struct {
	App            string        `json:"app"`
	ID             string        `json:"id"`
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
	UpdatedAt      string        `json:"updated_at,omitempty"`
	Permissions    []Permission  `json:"permissions"`
	Surfaces       []Surface     `json:"surfaces"`
	Icon           DuoIconSpec   `json:"icon"`
	MinHostVersion string        `json:"min_host_version,omitempty"`
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
	if len(builtinSeeds) == 0 && embeddedFallbackIndex != nil {
		cp := *embeddedFallbackIndex
		cp.Apps = append([]StoreAppEntry(nil), embeddedFallbackIndex.Apps...)
		return &cp
	}
	apps := make([]StoreAppEntry, 0, len(builtinSeeds))
	for _, s := range builtinSeeds {
		apps = append(apps, s.Entry)
	}
	sort.Slice(apps, func(i, j int) bool {
		if apps[i].Downloads != apps[j].Downloads {
			return apps[i].Downloads > apps[j].Downloads
		}
		return apps[i].ID < apps[j].ID
	})
	return &StoreIndex{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		Topic:         PluginTopic,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		Apps:          apps,
	}
}

// DefaultStoreIndexURLs lists the CDN / mirror endpoints for the auto-crawled blockmd-plugin index.
var DefaultStoreIndexURLs = []string{
	"https://cdn.jsdelivr.net/gh/blockmd-plugins/store-index@main/store-index.json",
	"https://gh-proxy.com/https://raw.githubusercontent.com/blockmd-plugins/store-index/main/store-index.json",
	"https://raw.githubusercontent.com/blockmd-plugins/store-index/main/store-index.json",
}

// StoreManager manages isolated plugin installations under <BlockAppData>/apps/<id>/,
// isolated per-plugin storage under <BlockAppData>/apps-data/<id>/, and store index cache
// under <BlockAppData>/apps-cache/store-index.json without touching Block's core state.json.
type StoreManager struct {
	AppsRoot  string
	DataRoot  string
	CacheRoot string
	IndexURLs []string
	HTTP      *http.Client
	dlMu      sync.Mutex
	installMu sync.Mutex
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
	return filepath.Join(s.CacheRoot, "store-index.json")
}

func (s *StoreManager) downloadCountsPath() string {
	return filepath.Join(s.CacheRoot, "download-counts.json")
}

func (s *StoreManager) loadDownloadDeltasLocked() map[string]int {
	out := map[string]int{}
	raw, err := os.ReadFile(s.downloadCountsPath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
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
	deltas := s.loadDownloadDeltasLocked()
	s.dlMu.Unlock()
	base := entry.Downloads
	if base < 0 {
		base = 0
	}
	return base + deltas[entry.ID]
}

// LoadCachedOrSeedCatalog returns the cached store-index.json merged with built-in official catalog entries.
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
	return mergeStoreIndexes(parsed, seed)
}

// RefreshCatalog fetches the latest remote store-index.json across mirrors, caches it locally,
// and merges built-in seed entries as fallback.
func (s *StoreManager) RefreshCatalog(ctx context.Context) (*StoreIndex, error) {
	if err := s.EnsureDirs(); err != nil {
		return s.LoadCachedOrSeedCatalog(), err
	}
	var lastErr error
	for _, u := range s.IndexURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "Block-AppStore/1.0 (blockmd)")
		resp, err := s.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || readErr != nil {
			lastErr = fmt.Errorf("fetch %s: status %d", u, resp.StatusCode)
			continue
		}
		idx, parseErr := ParseStoreIndex(body)
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		_ = os.WriteFile(s.cacheIndexPath(), body, 0o644)
		return mergeStoreIndexes(idx, DefaultSeedIndex()), nil
	}
	return s.LoadCachedOrSeedCatalog(), lastErr
}

func mergeStoreIndexes(primary, fallback *StoreIndex) *StoreIndex {
	if primary == nil {
		return fallback
	}
	seen := make(map[string]bool, len(primary.Apps))
	merged := make([]StoreAppEntry, 0, len(primary.Apps)+len(fallback.Apps))
	for _, a := range primary.Apps {
		if a.ID == "" || seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		merged = append(merged, a)
	}
	if fallback != nil {
		for _, a := range fallback.Apps {
			if a.ID == "" || seen[a.ID] {
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
		if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(filepath.Join(s.CacheRoot, e.Name()), dst); err != nil {
				return nil, err
			}
		}
	}
	entries, err := os.ReadDir(s.AppsRoot)
	if err != nil {
		return nil, err
	}
	var out []InstalledApp
	for _, e := range entries {
		if !e.IsDir() || strings.HasSuffix(e.Name(), ".installing") {
			continue
		}
		dir := filepath.Join(s.AppsRoot, e.Name())
		m, err := LoadManifest(dir)
		if err != nil {
			continue
		}
		info, _ := e.Info()
		var installedAt time.Time
		if info != nil {
			installedAt = info.ModTime()
		}
		dataDir := filepath.Join(s.DataRoot, m.ID)
		_ = os.MkdirAll(dataDir, 0o755)
		out = append(out, InstalledApp{
			Manifest:    *m,
			InstallDir:  dir,
			DataDir:     dataDir,
			Enabled:     true,
			InstalledAt: installedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out, nil
}

// InstallEntry installs a catalog StoreAppEntry via builtin:// seed, file:// directory, or HTTPS archive with SHA256 check,
// and increments the persistent download counter on success.
func (s *StoreManager) InstallEntry(ctx context.Context, entry StoreAppEntry) (*InstalledApp, error) {
	inst, err := s.installEntryCore(ctx, entry)
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

func (s *StoreManager) installEntryCore(ctx context.Context, entry StoreAppEntry) (*InstalledApp, error) {
	u := strings.TrimSpace(entry.DownloadURL)
	switch {
	case strings.HasPrefix(u, "builtin://"):
		id := strings.TrimPrefix(u, "builtin://")
		return s.installBuiltin(id)
	case strings.HasPrefix(u, "file://"):
		return s.InstallFromDirectory(strings.TrimPrefix(u, "file://"))
	case strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://"):
		if strings.TrimSpace(entry.SHA256) == "" {
			return nil, errors.New("catalog archive is missing its SHA256")
		}
		return s.installArchive(ctx, u, entry.SHA256, &entry)
	default:
		return nil, fmt.Errorf("unsupported catalog download URL %q", u)
	}
}

// InstallFromSource supports local directory paths, archive URLs, or Magpie-style GitHub shorthand (owner/repo or github:owner/repo).
func (s *StoreManager) InstallFromSource(ctx context.Context, spec string) (*InstalledApp, error) {
	inst, err := s.installFromSourceCore(ctx, spec)
	if err == nil && inst != nil {
		s.IncrementDownloads(inst.Manifest.ID)
	}
	return inst, err
}

func (s *StoreManager) installFromSourceCore(ctx context.Context, spec string) (*InstalledApp, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("empty plugin source")
	}
	if strings.HasPrefix(spec, "builtin://") {
		return s.installBuiltin(strings.TrimPrefix(spec, "builtin://"))
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
		return s.InstallFromDirectory(spec)
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
	if repoSlugRe.MatchString(spec) {
		return s.installFromGitHubRepo(ctx, spec)
	}
	if strings.HasPrefix(spec, "https://") || strings.HasPrefix(spec, "http://") {
		return s.InstallFromArchiveURL(ctx, spec, "")
	}
	// Check if spec matches a catalog ID directly (e.g. blockmd://install?id=block-sync).
	cat := s.LoadCachedOrSeedCatalog()
	for _, app := range cat.Apps {
		if app.ID == spec {
			return s.installEntryCore(ctx, app)
		}
	}
	return nil, fmt.Errorf("unsupported plugin source %q", spec)
}

func (s *StoreManager) installFromGitHubRepo(ctx context.Context, slug string) (*InstalledApp, error) {
	candidates := []string{
		fmt.Sprintf("https://codeload.github.com/%s/tar.gz/HEAD", slug),
		fmt.Sprintf("https://gh-proxy.com/https://github.com/%s/archive/HEAD.tar.gz", slug),
	}
	var lastErr error
	for _, u := range candidates {
		inst, err := s.InstallFromArchiveURL(ctx, u, "")
		if err == nil {
			return inst, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("install github:%s failed: %w", slug, lastErr)
}

func (s *StoreManager) InstallFromArchiveURL(ctx context.Context, rawURL string, expectedSHA256 string) (*InstalledApp, error) {
	return s.installArchive(ctx, rawURL, expectedSHA256, nil)
}

func (s *StoreManager) installArchive(ctx context.Context, rawURL, expectedSHA256 string, entry *StoreAppEntry) (*InstalledApp, error) {
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	candidates := []string{rawURL}
	if strings.HasPrefix(rawURL, "https://github.com/") {
		candidates = append(candidates, "https://gh-proxy.com/"+rawURL)
	}
	var data []byte
	var firstErr, lastErr error
	for _, candURL := range candidates {
		data, lastErr = s.fetchArchiveBytes(ctx, candURL)
		if lastErr == nil {
			break
		}
		if firstErr == nil {
			firstErr = lastErr
		}
	}
	if lastErr != nil {
		return nil, firstErr
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

	if err := ExtractPluginArchive(data, rawURL, tmpExtract); err != nil {
		return nil, err
	}
	pluginRoot, err := FindManifestRoot(tmpExtract)
	if err != nil {
		return nil, err
	}
	if entry != nil {
		m, err := LoadManifest(pluginRoot)
		if err != nil {
			return nil, err
		}
		if m.ID != entry.ID || m.Version != strings.TrimPrefix(entry.Version, "v") {
			return nil, fmt.Errorf("archive manifest differs from catalog entry %s %s", entry.ID, entry.Version)
		}
	}
	return s.InstallFromDirectory(pluginRoot)
}

func (s *StoreManager) fetchArchiveBytes(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Block-AppStore/1.0 (blockmd)")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", rawURL, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
}

func (s *StoreManager) installBuiltin(id string) (*InstalledApp, error) {
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
	return s.InstallFromDirectory(tmpExtract)
}

func (s *StoreManager) InstallFromDirectory(srcDir string) (*InstalledApp, error) {
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	m, err := LoadManifest(srcDir)
	if err != nil {
		return nil, err
	}
	dstDir := filepath.Join(s.AppsRoot, m.ID)
	s.installMu.Lock()
	defer s.installMu.Unlock()
	dataDir := filepath.Join(s.DataRoot, m.ID)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(s.AppsRoot, ".install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	tmpDir := filepath.Join(staging, "candidate")
	if err := copyDirRecursive(srcDir, tmpDir); err != nil {
		return nil, err
	}
	backup := filepath.Join(s.CacheRoot, m.ID+".previous")
	_, err = os.Stat(dstDir)
	if err == nil {
		// Initialize the candidate with isolated storage before replacing the working copy.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rt, err := StartRuntime(ctx, *m, HostConfig{PluginDir: tmpDir, DataDir: filepath.Join(staging, "data"), HostVersion: updater.CurrentVersion()})
		if err != nil {
			return nil, err
		}
		err = rt.Initialize(ctx)
		_ = rt.Shutdown(ctx)
		if err != nil {
			return nil, fmt.Errorf("update initialization: %w", err)
		}
		if err := os.RemoveAll(backup); err != nil {
			return nil, err
		}
		if err := os.Rename(dstDir, backup); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.Rename(tmpDir, dstDir); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			return nil, errors.Join(err, os.Rename(backup, dstDir))
		}
		return nil, err
	}
	_ = os.RemoveAll(backup)
	return &InstalledApp{
		Manifest:    *m,
		InstallDir:  dstDir,
		DataDir:     dataDir,
		Enabled:     true,
		InstalledAt: time.Now(),
	}, nil
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
	if purgeData {
		return os.RemoveAll(filepath.Join(s.DataRoot, pluginID))
	}
	return nil
}

func ParseStoreIndex(raw []byte) (*StoreIndex, error) {
	var idx StoreIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, err
	}
	if idx.App != "" && idx.App != TargetApp {
		return nil, fmt.Errorf("store index app mismatch: got %q, want %q", idx.App, TargetApp)
	}
	return &idx, nil
}

// ExtractPluginArchive safely extracts a .zip or .tar.gz/.tgz byte slice into destDir (preventing path traversal).
func ExtractPluginArchive(data []byte, hintURL string, destDir string) error {
	lower := strings.ToLower(hintURL)
	if strings.HasSuffix(lower, ".zip") || (len(data) >= 4 && bytes.Equal(data[:4], []byte("PK\x03\x04"))) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return fmt.Errorf("open zip: %w", err)
		}
		for _, f := range zr.File {
			target, err := safeExtractPath(destDir, f.Name)
			if err != nil {
				return err
			}
			if f.FileInfo().IsDir() {
				if err := os.MkdirAll(target, 0o755); err != nil {
					return err
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode()|0o600)
			if err != nil {
				rc.Close()
				return err
			}
			_, copyErr := io.Copy(out, io.LimitReader(rc, 32*1024*1024))
			out.Close()
			rc.Close()
			if copyErr != nil {
				return copyErr
			}
		}
		return nil
	}

	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open tar.gz: %w", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		target, err := safeExtractPath(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, io.LimitReader(tr, 32*1024*1024))
			out.Close()
			if copyErr != nil {
				return copyErr
			}
		}
	}
	return nil
}

// FindManifestRoot locates the directory containing manifest.json (root or single top-level archive folder).
func FindManifestRoot(extractDir string) (string, error) {
	if _, err := os.Stat(filepath.Join(extractDir, "manifest.json")); err == nil {
		return extractDir, nil
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
		if _, err := os.Stat(filepath.Join(cand, "manifest.json")); err == nil {
			return cand, nil
		}
	}
	return "", errors.New("archive does not contain manifest.json")
}

func safeExtractPath(destDir, entryName string) (string, error) {
	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return "", err
	}
	cleaned := filepath.Clean(filepath.FromSlash(entryName))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("unsafe archive path %q", entryName)
	}
	target := filepath.Join(destAbs, cleaned)
	if target != destAbs && !strings.HasPrefix(target, destAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive path %q escapes destination", entryName)
	}
	return target, nil
}

func copyDirRecursive(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "__pycache__" {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
