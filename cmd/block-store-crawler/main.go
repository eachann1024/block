package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"block/internal/plugins"
)

const (
	defaultAPIBase   = "https://api.github.com"
	maxArchiveBytes  = 64 * 1024 * 1024
	maxResponseBytes = 8 * 1024 * 1024
	crawlerUserAgent = "Block-Store-Crawler/1.0 (blockmd)"
)

type ghSearchResponse struct {
	Items []ghRepo `json:"items"`
}

type ghRepo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Stargazers    int    `json:"stargazers_count"`
	UpdatedAt     string `json:"updated_at"`
	PushedAt      string `json:"pushed_at"`
}

type ghRelease struct {
	TagName     string `json:"tag_name"`
	TarballURL  string `json:"tarball_url"`
	ZipballURL  string `json:"zipball_url"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		DownloadCount      int    `json:"download_count"`
	} `json:"assets"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	outPath := flag.String("out", "store/store-index.json", "output store-index.json path")
	blocklistPath := flag.String("blocklist", "store/blocklist.json", "path to blocklist.json")
	verifiedPath := flag.String("verified", "store/verified.json", "path to verified.json")
	localSeed := flag.String("local-seed", "", "comma-separated local plugin directories")
	skipGitHub := flag.Bool("skip-github", false, "skip GitHub crawling and build index from local seeds only")
	apiBase := flag.String("api-base", defaultAPIBase, "GitHub API base URL")
	flag.Parse()

	blocklist, err := loadStringSet(*blocklistPath)
	if err != nil {
		return fmt.Errorf("load blocklist: %w", err)
	}
	verified, err := loadVerifiedRegistry(*verifiedPath)
	if err != nil {
		return fmt.Errorf("load verified list: %w", err)
	}
	seedDirs := splitCSV(*localSeed)

	entries := make([]plugins.StoreAppEntry, 0)

	if !*skipGitHub {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		ghEntries, ghErr := crawlGitHub(ctx, *apiBase, os.Getenv("GITHUB_TOKEN"), blocklist, verified)
		if ghErr != nil {
			if len(seedDirs) == 0 {
				return ghErr
			}
			fmt.Fprintf(os.Stderr, "[WARN] GitHub crawl failed (%v); continuing with local seeds\n", ghErr)
		} else {
			entries = append(entries, ghEntries...)
		}
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	for _, dir := range seedDirs {
		m, err := plugins.LoadManifest(dir)
		if err != nil {
			return fmt.Errorf("load local seed %q: %w", dir, err)
		}
		if matchesSet(blocklist, m.ID, m.Repo) {
			continue
		}
		isVerified, err := verified.check(m.ID, m.Repo, m.Repo)
		if err != nil {
			return fmt.Errorf("local seed %q: %w", dir, err)
		}
		entries = append(entries, entryFromManifest(m, "builtin://"+m.ID, "", defaultSeedDownloads(m.ID), 0, nowStr, m.Repo, isVerified))
	}

	// A shared ID without a unique owner cannot identify an install or update.
	owners := make(map[string]string)
	conflicts := make(map[string]bool)
	for _, entry := range entries {
		owner := normalizeKey(entry.Repo)
		if previous, exists := owners[entry.ID]; exists && previous != owner {
			conflicts[entry.ID] = true
		}
		owners[entry.ID] = owner
	}
	seenIDs := make(map[string]bool)
	// Double-check blocklist filter across all entries and sort deterministically.
	filtered := make([]plugins.StoreAppEntry, 0, len(entries))
	for _, entry := range entries {
		if conflicts[entry.ID] || seenIDs[entry.ID] || matchesSet(blocklist, entry.ID, entry.Repo) {
			continue
		}
		seenIDs[entry.ID] = true
		filtered = append(filtered, entry)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Verified != filtered[j].Verified {
			return filtered[i].Verified
		}
		if filtered[i].Stars != filtered[j].Stars {
			return filtered[i].Stars > filtered[j].Stars
		}
		if filtered[i].Downloads != filtered[j].Downloads {
			return filtered[i].Downloads > filtered[j].Downloads
		}
		return filtered[i].ID < filtered[j].ID
	})

	index := plugins.StoreIndex{
		App:           plugins.TargetApp,
		SchemaVersion: plugins.CurrentSchemaVersion,
		Topic:         plugins.PluginTopic,
		UpdatedAt:     nowStr,
		Apps:          filtered,
	}

	encoded, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal store index: %w", err)
	}
	encoded = append(encoded, '\n')

	if dir := filepath.Dir(*outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	if err := os.WriteFile(*outPath, encoded, 0o644); err != nil {
		return fmt.Errorf("write output %q: %w", *outPath, err)
	}
	return nil
}

func crawlGitHub(
	ctx context.Context,
	apiBase string,
	token string,
	blocklist map[string]bool,
	verified verifiedRegistry,
) ([]plugins.StoreAppEntry, error) {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if base == "" {
		base = defaultAPIBase
	}
	token = strings.TrimSpace(token)
	client := &http.Client{Timeout: 30 * time.Second}

	searchURL := fmt.Sprintf(
		"%s/search/repositories?q=topic:%s+fork:false+archived:false&sort=stars&order=desc&per_page=100",
		base, plugins.PluginTopic,
	)
	var searchResp ghSearchResponse
	status, err := fetchJSON(ctx, client, searchURL, token, &searchResp)
	if err != nil {
		return nil, fmt.Errorf("search repositories: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search repositories: HTTP %d", status)
	}

	out := make([]plugins.StoreAppEntry, 0, len(searchResp.Items))
	for _, repo := range searchResp.Items {
		if repo.FullName == "" || matchesSet(blocklist, repo.FullName) {
			continue
		}
		downloadURL, downloads, updatedAt := resolveRepoArchive(ctx, client, base, token, repo)
		if downloadURL == "" {
			continue
		}
		entry, err := verifyRemotePlugin(ctx, client, base, token, repo, downloadURL, downloads, updatedAt, blocklist, verified)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] skip %s: %v\n", repo.FullName, err)
			continue
		}
		if entry != nil {
			out = append(out, *entry)
		}
	}
	return out, nil
}

func resolveRepoArchive(ctx context.Context, client *http.Client, apiBase, token string, repo ghRepo) (string, int, string) {
	updatedAt := firstNonEmpty(repo.PushedAt, repo.UpdatedAt)
	relURL := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, repo.FullName)
	var rel ghRelease
	status, err := fetchJSON(ctx, client, relURL, token, &rel)
	if err == nil && status == http.StatusOK {
		if rel.PublishedAt != "" {
			updatedAt = rel.PublishedAt
		}
		totalDownloads := 0
		for _, a := range rel.Assets {
			if a.DownloadCount > 0 {
				totalDownloads += a.DownloadCount
			}
		}
		if assetURL := pickReleaseArchive(rel); assetURL != "" {
			return assetURL, totalDownloads, updatedAt
		}
	}
	return fallbackTarballURL(apiBase, repo), 0, updatedAt
}

func pickReleaseArchive(rel ghRelease) string {
	for _, asset := range rel.Assets {
		lower := strings.ToLower(asset.Name)
		if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".zip") {
			if asset.BrowserDownloadURL != "" {
				return asset.BrowserDownloadURL
			}
		}
	}
	if rel.TarballURL != "" {
		return rel.TarballURL
	}
	return rel.ZipballURL
}

func fallbackTarballURL(apiBase string, repo ghRepo) string {
	branch := strings.TrimSpace(repo.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	if apiBase == defaultAPIBase {
		return fmt.Sprintf("https://codeload.github.com/%s/tar.gz/%s", repo.FullName, url.PathEscape(branch))
	}
	return fmt.Sprintf("%s/repos/%s/tarball/%s", apiBase, repo.FullName, url.PathEscape(branch))
}

func verifyRemotePlugin(
	ctx context.Context,
	client *http.Client,
	apiBase string,
	token string,
	repo ghRepo,
	downloadURL string,
	downloads int,
	updatedAt string,
	blocklist map[string]bool,
	verified verifiedRegistry,
) (*plugins.StoreAppEntry, error) {
	data, err := downloadBytes(ctx, client, downloadURL, apiBase, token)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])

	tmpDir, err := os.MkdirTemp("", "block-store-crawler-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	if err := plugins.ExtractPluginArchive(data, downloadURL, tmpDir); err != nil {
		return nil, fmt.Errorf("extract archive: %w", err)
	}
	pluginRoot, err := plugins.FindManifestRoot(tmpDir)
	if err != nil {
		return nil, err
	}
	m, err := plugins.LoadManifest(pluginRoot)
	if err != nil {
		return nil, err
	}
	if matchesSet(blocklist, m.ID, m.Repo, repo.FullName) {
		return nil, nil
	}
	isVerified, err := verified.check(m.ID, repo.FullName, m.Repo)
	if err != nil {
		return nil, err
	}
	entry := entryFromManifest(m, downloadURL, shaHex, downloads, repo.Stargazers, updatedAt, repo.FullName, isVerified)
	return &entry, nil
}

func defaultSeedDownloads(id string) int {
	switch id {
	case "block-sync":
		return 1280
	case "block-image-host":
		return 940
	case "block-publisher":
		return 760
	case "block-migrator":
		return 615
	case "block-diagnostics":
		return 320
	default:
		return 0
	}
}

func entryFromManifest(
	m *plugins.Manifest,
	downloadURL string,
	shaHex string,
	downloads int,
	stars int,
	updatedAt string,
	actualRepo string,
	isVerified bool,
) plugins.StoreAppEntry {
	repoSlug := strings.TrimSpace(actualRepo)
	if repoSlug == "" {
		repoSlug = strings.TrimSpace(m.Repo)
	}
	perms := m.Permissions
	if perms == nil {
		perms = []plugins.Permission{}
	}
	return plugins.StoreAppEntry{
		App:         m.App,
		ID:          m.ID,
		Name:        m.Name,
		Version:     m.Version,
		Author:      m.Author,
		Repo:        repoSlug,
		Summary:     m.Summary,
		DownloadURL: downloadURL,
		SHA256:      shaHex,
		Downloads:   downloads,
		Stars:       stars,
		Verified:    isVerified,
		UpdatedAt:   updatedAt,
		Permissions: perms,
		Surfaces:    m.Surfaces,
		Icon:        m.Icon,
	}
}

func fetchJSON(ctx context.Context, client *http.Client, rawURL, token string, dst any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", crawlerUserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		return resp.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return resp.StatusCode, err
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func downloadBytes(ctx context.Context, client *http.Client, rawURL, apiBase, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", crawlerUserAgent)
	if token != "" && sameHost(rawURL, apiBase) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", rawURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchiveBytes {
		return nil, fmt.Errorf("archive %s exceeds %d bytes", rawURL, maxArchiveBytes)
	}
	return data, nil
}

func sameHost(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(ua.Host, ub.Host)
}

type verifiedRegistry struct {
	idToRepo map[string]string
	repoToID map[string]string
}

func loadVerifiedRegistry(path string) (verifiedRegistry, error) {
	reg := verifiedRegistry{
		idToRepo: make(map[string]string),
		repoToID: make(map[string]string),
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return reg, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return reg, nil
		}
		return reg, err
	}
	var bindings map[string]string
	if err := json.Unmarshal(raw, &bindings); err != nil {
		return reg, err
	}
	for rawID, rawRepo := range bindings {
		id := normalizeKey(rawID)
		repo := normalizeKey(rawRepo)
		if id == "" || !isValidRepoSlug(repo) {
			return reg, fmt.Errorf("invalid verified binding %q -> %q", rawID, rawRepo)
		}
		if prevID, exists := reg.repoToID[repo]; exists && prevID != id {
			return reg, fmt.Errorf("duplicate verified repo binding %q for %q and %q", repo, prevID, id)
		}
		reg.idToRepo[id] = repo
		reg.repoToID[repo] = id
	}
	return reg, nil
}

// check enforces 1:1 binding between plugin ID, crawled repo, and manifest repo.
func (r verifiedRegistry) check(id, actualRepo, manifestRepo string) (bool, error) {
	normID := normalizeKey(id)
	normActual := normalizeKey(actualRepo)
	normManifest := normalizeKey(manifestRepo)

	if normManifest != "" && normActual != "" && normManifest != normActual {
		return false, fmt.Errorf("manifest repo %q does not match crawled repo %q", manifestRepo, actualRepo)
	}
	effectiveRepo := normActual
	if effectiveRepo == "" {
		effectiveRepo = normManifest
	}
	if boundRepo, ok := r.idToRepo[normID]; ok && effectiveRepo != boundRepo {
		return false, fmt.Errorf("plugin ID %q is bound to verified repo %q, got %q", id, boundRepo, firstNonEmpty(actualRepo, manifestRepo))
	}
	if boundID, ok := r.repoToID[effectiveRepo]; ok && normID != boundID {
		return false, fmt.Errorf("verified repo %q is bound to plugin ID %q, got %q", firstNonEmpty(actualRepo, manifestRepo), boundID, id)
	}
	return effectiveRepo != "" && r.idToRepo[normID] == effectiveRepo, nil
}

func normalizeKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func isValidRepoSlug(repo string) bool {
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func loadStringSet(path string) (map[string]bool, error) {
	set := make(map[string]bool)
	path = strings.TrimSpace(path)
	if path == "" {
		return set, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return set, nil
		}
		return nil, err
	}
	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	for _, item := range items {
		norm := strings.ToLower(strings.TrimSpace(item))
		if norm != "" {
			set[norm] = true
		}
	}
	return set, nil
}

func matchesSet(set map[string]bool, keys ...string) bool {
	for _, k := range keys {
		norm := strings.ToLower(strings.TrimSpace(k))
		if norm != "" && set[norm] {
			return true
		}
	}
	return false
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
