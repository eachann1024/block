package main

import (
	"bytes"
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
	defaultAPIBase        = "https://api.github.com"
	maxArchiveBytes       = 64 * 1024 * 1024
	maxResponseBytes      = 8 * 1024 * 1024
	crawlerUserAgent      = "Block-Store-Crawler/1.0 (blockmd)"
	legacySyncURL         = "https://github.com/blockmd-plugins/block-sync/releases/download/v1.3.44/block-sync-v1.3.44.tar.gz"
	legacySyncSHA256      = "568b59736dfaf8aefddac049e68aed9d3adae621072be4b2a5ea4ee72f547d29"
	legacySyncPublishedAt = "2026-11-20T14:03:35Z"
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
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	TarballURL  string `json:"tarball_url"`
	PublishedAt string `json:"published_at"`
	CreatedAt   string `json:"created_at"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
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
	changelogsDir := flag.String("changelogs-dir", "", "optional directory to write per-plugin changelogs/<id>.json files")
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
	fullChangelogs := make(map[string][]plugins.PluginReleaseEntry)

	if !*skipGitHub {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		ghEntries, ghChangelogs, ghErr := crawlGitHub(ctx, *apiBase, os.Getenv("GITHUB_TOKEN"), blocklist, verified)
		if ghErr != nil {
			if len(seedDirs) == 0 {
				return ghErr
			}
			fmt.Fprintf(os.Stderr, "[WARN] GitHub crawl failed (%v); continuing with local seeds\n", ghErr)
		} else {
			entries = append(entries, ghEntries...)
			for id, rels := range ghChangelogs {
				fullChangelogs[id] = rels
			}
		}
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	for _, dir := range seedDirs {
		m, err := plugins.LoadManifest(dir)
		if err != nil {
			return fmt.Errorf("load local seed %q: %w", dir, err)
		}
		if m.ID == plugins.DiagnosticsPluginID || matchesSet(blocklist, m.ID, m.Repo) {
			continue
		}
		if err := plugins.ValidatePluginLanguages(dir, m); err != nil {
			return fmt.Errorf("local seed %q languages: %w", dir, err)
		}
		isVerified, err := verified.check(m.ID, m.Repo, m.Repo)
		if err != nil {
			return fmt.Errorf("local seed %q: %w", dir, err)
		}
		if !*skipGitHub && (isVerified || plugins.IsOfficialRepo(m.Repo)) {
			// Do not let a local seed mask a missing or broken remote official release.
			continue
		}
		if isVerified || plugins.IsOfficialRepo(m.Repo) {
			if err := plugins.ValidateManifestEnglish(m); err != nil {
				return fmt.Errorf("local seed %q manifest English: %w", dir, err)
			}
			if err := plugins.ValidateBilingualDocs(dir); err != nil {
				return fmt.Errorf("local seed %q bilingual docs: %w", dir, err)
			}
			if notice := m.CategoryNotice(); notice != "" {
				return fmt.Errorf("local seed %q category: %s", dir, notice)
			}
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
	// Keep the repo that already owned the ID in the previous index, so a new
	// repo cannot hijack or knock out an existing listing by reusing its ID.
	incumbent := loadPreviousOwners(*outPath)
	for id := range conflicts {
		if prev := incumbent[id]; prev != "" {
			kept := false
			for _, entry := range entries {
				if entry.ID == id && normalizeKey(entry.Repo) == prev {
					kept = true
					break
				}
			}
			if kept {
				owners[id] = prev
				delete(conflicts, id)
				fmt.Fprintf(os.Stderr, "[WARN] duplicate id %q: keeping previous owner %s\n", id, prev)
				continue
			}
		}
		fmt.Fprintf(os.Stderr, "[WARN] duplicate id %q without a previous owner: dropping all copies\n", id)
	}
	seenIDs := make(map[string]bool)
	// Double-check blocklist filter across all entries and sort deterministically.
	filtered := make([]plugins.StoreAppEntry, 0, len(entries))
	for _, entry := range entries {
		if conflicts[entry.ID] || seenIDs[entry.ID] || normalizeKey(entry.Repo) != owners[entry.ID] || matchesSet(blocklist, entry.ID, entry.Repo) {
			continue
		}
		seenIDs[entry.ID] = true
		filtered = append(filtered, entry)
	}
	if err := verified.validateApps(filtered); err != nil {
		return err
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

	index := struct {
		plugins.StoreIndex
		SystemApps []plugins.StoreAppEntry `json:"system_apps,omitempty"`
	}{StoreIndex: plugins.StoreIndex{
		App: plugins.TargetApp, SchemaVersion: plugins.CurrentSchemaVersion, Topic: plugins.PluginTopic,
		UpdatedAt: nowStr, Apps: make([]plugins.StoreAppEntry, 0, len(filtered)),
	}}
	for _, entry := range filtered {
		if !requiresSystemHost(entry) {
			index.Apps = append(index.Apps, entry)
			continue
		}
		index.SystemApps = append(index.SystemApps, entry)
		if *skipGitHub || entry.ID != "block-sync" || entry.Repo != "blockmd-plugins/block-sync" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		client := &http.Client{Timeout: 30 * time.Second}
		legacy, err := verifyRemotePlugin(ctx, client, *apiBase, os.Getenv("GITHUB_TOKEN"),
			ghRepo{FullName: entry.Repo, Stargazers: entry.Stars}, legacySyncURL, entry.Downloads, legacySyncPublishedAt, blocklist, verified, false)
		cancel()
		if err != nil {
			return fmt.Errorf("verify compatible Sync release: %w", err)
		}
		if legacy == nil || !legacy.Verified || legacy.ID != entry.ID || legacy.Version != "1.3.44" ||
			legacy.SHA256 != legacySyncSHA256 || requiresSystemHost(*legacy) {
			return errors.New("compatible Sync release has an unexpected identity, digest or permissions")
		}
		legacy.ChangelogURL = entry.ChangelogURL
		index.Apps = append(index.Apps, *legacy)
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
	if dir := strings.TrimSpace(*changelogsDir); dir != "" {
		if err := writePluginChangelogs(dir, nowStr, filtered, fullChangelogs); err != nil {
			return fmt.Errorf("write changelogs: %w", err)
		}
	}
	return nil
}

func writePluginChangelogs(
	dir string,
	nowStr string,
	entries []plugins.StoreAppEntry,
	fullChangelogs map[string][]plugins.PluginReleaseEntry,
) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		releases := fullChangelogs[entry.ID]
		if len(releases) == 0 {
			continue
		}
		doc := plugins.PluginChangelog{
			App:           plugins.TargetApp,
			SchemaVersion: plugins.CurrentSchemaVersion,
			ID:            entry.ID,
			Repo:          entry.Repo,
			UpdatedAt:     nowStr,
			LatestVersion: releases[0].Version,
			Releases:      releases,
		}
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		raw = append(raw, '\n')
		if err := os.WriteFile(filepath.Join(dir, entry.ID+".json"), raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// loadPreviousOwners maps id -> repo from the index being replaced.
func loadPreviousOwners(path string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var prev struct {
		Apps       []plugins.StoreAppEntry `json:"apps"`
		SystemApps []plugins.StoreAppEntry `json:"system_apps"`
	}
	if json.Unmarshal(raw, &prev) != nil {
		return out
	}
	for _, entry := range append(prev.Apps, prev.SystemApps...) {
		if entry.ID != "" && entry.Repo != "" {
			out[entry.ID] = normalizeKey(entry.Repo)
		}
	}
	return out
}

func requiresSystemHost(entry plugins.StoreAppEntry) bool {
	for _, permission := range entry.Permissions {
		if strings.HasPrefix(string(permission), "system:") || permission == plugins.PermEventsSystem {
			return true
		}
	}
	return false
}

func crawlGitHub(
	ctx context.Context,
	apiBase string,
	token string,
	blocklist map[string]bool,
	verified verifiedRegistry,
) ([]plugins.StoreAppEntry, map[string][]plugins.PluginReleaseEntry, error) {
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
		return nil, nil, fmt.Errorf("search repositories: %w", err)
	}
	if status != http.StatusOK {
		return nil, nil, fmt.Errorf("search repositories: HTTP %d", status)
	}

	// The search is capped at 100 results; fetch verified official repos
	// directly so a lower-ranked one can never fail the whole round.
	repos := append([]ghRepo(nil), searchResp.Items...)
	seenRepos := make(map[string]bool, len(repos))
	for _, repo := range repos {
		seenRepos[normalizeKey(repo.FullName)] = true
	}
	for _, repoName := range verified.repos() {
		if seenRepos[repoName] {
			continue
		}
		var repo ghRepo
		status, err := fetchJSON(ctx, client, fmt.Sprintf("%s/repos/%s", base, repoName), token, &repo)
		if err != nil || status != http.StatusOK || repo.FullName == "" {
			fmt.Fprintf(os.Stderr, "[WARN] verified repo %s unavailable (HTTP %d, %v)\n", repoName, status, err)
			continue
		}
		seenRepos[repoName] = true
		repos = append(repos, repo)
	}

	out := make([]plugins.StoreAppEntry, 0, len(repos))
	changelogs := make(map[string][]plugins.PluginReleaseEntry)
	for _, repo := range repos {
		if repo.FullName == "" || matchesSet(blocklist, repo.FullName) {
			continue
		}
		downloadURL, downloads, updatedAt, fullRels, inlineRels := resolveRepoArchive(ctx, client, base, token, repo)
		if downloadURL == "" {
			continue
		}
		entry, err := verifyRemotePlugin(ctx, client, base, token, repo, downloadURL, downloads, updatedAt, blocklist, verified, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] skip %s: %v\n", repo.FullName, err)
			continue
		}
		if entry != nil {
			if len(fullRels) > 0 {
				entry.ChangelogURL = fmt.Sprintf("https://raw.githubusercontent.com/blockmd-plugins/store-index/main/changelogs/%s.json", entry.ID)
				entry.Changelog = inlineRels
				changelogs[entry.ID] = fullRels
			}
			out = append(out, *entry)
		}
	}
	return out, changelogs, nil
}

const (
	maxReleasePages       = 5
	maxFullChangelogItems = 20
	maxInlineChangelog    = 5
)

func resolveRepoArchive(
	ctx context.Context,
	client *http.Client,
	apiBase, token string,
	repo ghRepo,
) (string, int, string, []plugins.PluginReleaseEntry, []plugins.PluginReleaseEntry) {
	updatedAt := firstNonEmpty(repo.PushedAt, repo.UpdatedAt)
	totalDownloads := 0
	var latest *ghRelease
	fetchedAny := false
	hitPageCap := false
	fullRels := make([]plugins.PluginReleaseEntry, 0, maxFullChangelogItems)
	inlineRels := make([]plugins.PluginReleaseEntry, 0, maxInlineChangelog)

	for page := 1; page <= maxReleasePages; page++ {
		relsURL := fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", apiBase, repo.FullName, page)
		var rels []ghRelease
		status, err := fetchJSON(ctx, client, relsURL, token, &rels)
		if err != nil || status != http.StatusOK {
			break
		}
		fetchedAny = true
		for i := range rels {
			for _, a := range rels[i].Assets {
				if a.DownloadCount > 0 {
					totalDownloads += a.DownloadCount
				}
			}
			if !rels[i].Draft && !rels[i].Prerelease {
				if latest == nil {
					relCopy := rels[i]
					latest = &relCopy
				}
				if strings.TrimSpace(rels[i].TagName) != "" && len(fullRels) < maxFullChangelogItems {
					pub := firstNonEmpty(rels[i].PublishedAt, rels[i].CreatedAt)
					fullRels = append(fullRels, plugins.NormalizePluginReleaseEntry(rels[i].TagName, pub, rels[i].HTMLURL, rels[i].Body, true))
					if len(inlineRels) < maxInlineChangelog {
						inlineRels = append(inlineRels, plugins.NormalizePluginReleaseEntry(rels[i].TagName, pub, rels[i].HTMLURL, rels[i].Body, false))
					}
				}
			}
		}
		if len(rels) < 100 {
			break
		}
		if page == maxReleasePages {
			hitPageCap = true
		}
	}

	if latest == nil && (hitPageCap || !fetchedAny) {
		latestURL := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, repo.FullName)
		var rel ghRelease
		if status, err := fetchJSON(ctx, client, latestURL, token, &rel); err == nil && status == http.StatusOK && !rel.Draft && !rel.Prerelease {
			latest = &rel
			if strings.TrimSpace(rel.TagName) != "" && len(fullRels) == 0 {
				pub := firstNonEmpty(rel.PublishedAt, rel.CreatedAt)
				fullRels = append(fullRels, plugins.NormalizePluginReleaseEntry(rel.TagName, pub, rel.HTMLURL, rel.Body, true))
				inlineRels = append(inlineRels, plugins.NormalizePluginReleaseEntry(rel.TagName, pub, rel.HTMLURL, rel.Body, false))
			}
		}
	}

	if latest != nil {
		if latest.PublishedAt != "" {
			updatedAt = latest.PublishedAt
		}
		if assetURL := pickReleaseArchive(*latest); assetURL != "" {
			return assetURL, totalDownloads, updatedAt, fullRels, inlineRels
		}
	}
	return fallbackTarballURL(ctx, client, apiBase, token, repo), totalDownloads, updatedAt, fullRels, inlineRels
}

func isCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
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
	return rel.TarballURL
}

// fallbackTarballURL pins the default-branch source archive to the commit
// seen now, so the indexed sha256 keeps matching after later pushes.
func fallbackTarballURL(ctx context.Context, client *http.Client, apiBase, token string, repo ghRepo) string {
	branch := strings.TrimSpace(repo.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	var head struct {
		SHA string `json:"sha"`
	}
	headURL := fmt.Sprintf("%s/repos/%s/commits/%s", apiBase, repo.FullName, url.PathEscape(branch))
	if status, err := fetchJSON(ctx, client, headURL, token, &head); err != nil || status != http.StatusOK || !isCommitSHA(head.SHA) {
		return ""
	}
	branch = head.SHA
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
	requireOfficialCategory bool,
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
	if m.ID == plugins.DiagnosticsPluginID || matchesSet(blocklist, m.ID, m.Repo, repo.FullName) {
		return nil, nil
	}
	if err := plugins.ValidatePluginLanguages(pluginRoot, m); err != nil {
		return nil, fmt.Errorf("plugin languages check failed: %w", err)
	}
	isVerified, err := verified.check(m.ID, repo.FullName, m.Repo)
	if err != nil {
		return nil, err
	}
	if isVerified || plugins.IsOfficialRepo(repo.FullName) {
		if err := plugins.ValidateManifestEnglish(m); err != nil {
			return nil, fmt.Errorf("official plugin manifest English check failed: %w", err)
		}
		if err := plugins.ValidateBilingualDocs(pluginRoot); err != nil {
			return nil, fmt.Errorf("official plugin bilingual docs check failed: %w", err)
		}
		// The pinned compatible Sync release predates manifest.category.
		if notice := m.CategoryNotice(); requireOfficialCategory && notice != "" {
			return nil, fmt.Errorf("official plugin category check failed: %s", notice)
		}
	}
	entry := entryFromManifest(m, downloadURL, shaHex, downloads, repo.Stargazers, updatedAt, repo.FullName, isVerified)
	return &entry, nil
}

func defaultSeedDownloads(id string) int {
	switch id {
	case "block-sync":
		return 1280
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
	surfs := m.Surfaces
	if surfs == nil {
		surfs = []plugins.Surface{}
	}
	return plugins.StoreAppEntry{
		App:            m.App,
		ID:             m.ID,
		Category:       m.EffectiveCategory(),
		Name:           m.Name,
		Version:        m.Version,
		Author:         m.Author,
		Repo:           repoSlug,
		Summary:        m.Summary,
		DownloadURL:    downloadURL,
		SHA256:         shaHex,
		Downloads:      downloads,
		Stars:          stars,
		Verified:       isVerified,
		Beta:           m.Beta,
		UpdatedAt:         updatedAt,
		Permissions:       perms,
		PermissionReasons: plugins.ClonePermissionReasons(m.PermissionReasons),
		Surfaces:          surfs,
		FileExtensions: m.DisplayFileExtensions(),
		Icon:           m.Icon,
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
	_ = apiBase
	_ = token
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", crawlerUserAgent)
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

func (v verifiedRegistry) repos() []string {
	out := make([]string, 0, len(v.repoToID))
	for repo := range v.repoToID {
		out = append(out, repo)
	}
	sort.Strings(out)
	return out
}

func loadVerifiedRegistry(path string) (verifiedRegistry, error) {
	reg := verifiedRegistry{
		idToRepo: make(map[string]string),
		repoToID: make(map[string]string),
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return reg, errors.New("verified registry path must not be empty")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return reg, err
	}
	bindings, err := decodeStringMapNoDuplicateKeys(raw)
	if err != nil {
		return reg, err
	}
	if len(bindings) == 0 {
		return reg, errors.New("verified registry must be a non-empty JSON object")
	}
	for rawID, rawRepo := range bindings {
		id := normalizeKey(rawID)
		repo := normalizeKey(rawRepo)
		if id == "" || !isValidRepoSlug(repo) || !plugins.IsOfficialRepo(repo) {
			return reg, fmt.Errorf("invalid verified binding %q -> %q (must belong to blockmd-plugins/)", rawID, rawRepo)
		}
		if id == plugins.DiagnosticsPluginID {
			return reg, fmt.Errorf("verified registry must not include reserved id %q", id)
		}
		if prevRepo, exists := reg.idToRepo[id]; exists {
			return reg, fmt.Errorf("duplicate normalized verified id %q (%q and %q)", id, prevRepo, repo)
		}
		if prevID, exists := reg.repoToID[repo]; exists && prevID != id {
			return reg, fmt.Errorf("duplicate verified repo binding %q for %q and %q", repo, prevID, id)
		}
		reg.idToRepo[id] = repo
		reg.repoToID[repo] = id
	}
	return reg, nil
}

func decodeStringMapNoDuplicateKeys(raw []byte) (map[string]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, errors.New("expected JSON object")
	}
	out := make(map[string]string)
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := ktok.(string)
		if !ok {
			return nil, errors.New("expected string key")
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate JSON key %q", key)
		}
		var val string
		if err := dec.Decode(&val); err != nil {
			return nil, fmt.Errorf("value for key %q: %w", key, err)
		}
		out[key] = val
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after JSON object")
	}
	return out, nil
}

func (r verifiedRegistry) validateApps(apps []plugins.StoreAppEntry) error {
	byID := make(map[string]plugins.StoreAppEntry, len(apps))
	for _, app := range apps {
		id := normalizeKey(app.ID)
		repo := normalizeKey(app.Repo)
		if id == "" || repo == "" {
			return fmt.Errorf("store entry missing id or repo: %+v", app)
		}
		if id == plugins.DiagnosticsPluginID {
			return fmt.Errorf("reserved app id %q must not be published", id)
		}
		if _, dup := byID[id]; dup {
			return fmt.Errorf("duplicate app id %q", id)
		}
		byID[id] = app
		if app.Verified {
			if r.idToRepo[id] != repo {
				return fmt.Errorf("verified app %q has repo %q, expected %q", app.ID, app.Repo, r.idToRepo[id])
			}
		}
	}
	for id, expectedRepo := range r.idToRepo {
		app, ok := byID[id]
		if !ok {
			return fmt.Errorf("missing verified app %q (%s) from crawled index", id, expectedRepo)
		}
		if normalizeKey(app.Repo) != expectedRepo || !app.Verified {
			return fmt.Errorf("verified app %q failed binding validation", id)
		}
	}
	return nil
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
