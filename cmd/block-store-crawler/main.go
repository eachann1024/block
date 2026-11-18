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
	verified, err := loadStringSet(*verifiedPath)
	if err != nil {
		return fmt.Errorf("load verified list: %w", err)
	}
	seedDirs := splitCSV(*localSeed)

	entries := make([]plugins.StoreAppEntry, 0)
	seenIDs := make(map[string]bool)

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
			for _, entry := range ghEntries {
				if seenIDs[entry.ID] {
					continue
				}
				seenIDs[entry.ID] = true
				entries = append(entries, entry)
			}
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
		if seenIDs[m.ID] {
			continue
		}
		seenIDs[m.ID] = true
		entries = append(entries, entryFromManifest(m, "builtin://"+m.ID, "", defaultSeedDownloads(m.ID), 0, nowStr, m.Repo, verified))
	}

	// Double-check blocklist filter across all entries and sort deterministically.
	filtered := make([]plugins.StoreAppEntry, 0, len(entries))
	for _, entry := range entries {
		if matchesSet(blocklist, entry.ID, entry.Repo) {
			continue
		}
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
	verified map[string]bool,
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
	verified map[string]bool,
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
	entry := entryFromManifest(m, downloadURL, shaHex, downloads, repo.Stargazers, updatedAt, repo.FullName, verified)
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
	fallbackRepo string,
	verified map[string]bool,
) plugins.StoreAppEntry {
	repoSlug := m.Repo
	if repoSlug == "" {
		repoSlug = fallbackRepo
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
		Verified:    matchesSet(verified, m.ID, m.Repo, fallbackRepo),
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
