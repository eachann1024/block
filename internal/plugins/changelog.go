package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"block/internal/updater"
)

const (
	maxChangelogBytes      = 1 << 20
	maxReleaseBodyBytes    = 8192
	maxChangelogReleases   = 20
	changelogStageTimeout  = 5 * time.Second
	defaultChangelogRawURL = "https://raw.githubusercontent.com/blockmd-plugins/store-index/main/changelogs"
)

// ErrChangelogUnavailable reports that no release changelog or local CHANGELOG file exists for a plugin.
var ErrChangelogUnavailable = errors.New("plugin changelog unavailable")

// PluginReleaseEntry represents one published version entry in a plugin changelog.
type PluginReleaseEntry struct {
	Version     string                 `json:"version"`
	Tag         string                 `json:"tag,omitempty"`
	PublishedAt string                 `json:"published_at,omitempty"`
	HTMLURL     string                 `json:"html_url,omitempty"`
	Body        string                 `json:"body,omitempty"`
	Zh          updater.LocalizedNotes `json:"zh,omitempty"`
	En          updater.LocalizedNotes `json:"en,omitempty"`
}

// NotesForLanguage returns the localized structured notes for e, parsing Body lazily if needed.
func (e PluginReleaseEntry) NotesForLanguage(en bool) updater.LocalizedNotes {
	zh, enNotes := e.Zh, e.En
	if zh.IsEmpty() && enNotes.IsEmpty() && strings.TrimSpace(e.Body) != "" {
		zh, enNotes = updater.ParseReleaseBody(e.Body)
	}
	if en {
		if !enNotes.IsEmpty() {
			return enNotes
		}
		return zh
	}
	if !zh.IsEmpty() {
		return zh
	}
	return enNotes
}

// PluginChangelog is the response document for the App Store plugin changelog API
// (changelogs/<id>.json, StoreManager.ReadPluginChangelog, and host.store.changelog).
type PluginChangelog struct {
	App           string               `json:"app,omitempty"`
	SchemaVersion int                  `json:"schema_version,omitempty"`
	ID            string               `json:"id"`
	Repo          string               `json:"repo,omitempty"`
	UpdatedAt     string               `json:"updated_at,omitempty"`
	LatestVersion string               `json:"latest_version,omitempty"`
	ChangelogURL  string               `json:"changelog_url,omitempty"`
	Releases      []PluginReleaseEntry `json:"releases"`
	Markdown      string               `json:"markdown,omitempty"`
	Source        string               `json:"source,omitempty"`
}

// NormalizePluginReleaseEntry builds a structured bilingual PluginReleaseEntry from a GitHub Release payload.
func NormalizePluginReleaseEntry(tag, publishedAt, htmlURL, body string, includeBody bool) PluginReleaseEntry {
	tag = strings.TrimSpace(tag)
	version := strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "V")
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if len(body) > maxReleaseBodyBytes {
		body = truncateUTF8Bytes(body, maxReleaseBodyBytes)
	}
	zh, en := updater.ParseReleaseBody(body)
	entry := PluginReleaseEntry{
		Version:     version,
		Tag:         tag,
		PublishedAt: strings.TrimSpace(publishedAt),
		HTMLURL:     strings.TrimSpace(htmlURL),
		Zh:          zh,
		En:          en,
	}
	if body != "" && (includeBody || (zh.IsEmpty() && en.IsEmpty())) {
		entry.Body = body
	}
	return entry
}

func truncateUTF8Bytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && (s[end]&0xC0) == 0x80 {
		end--
	}
	return strings.TrimSpace(s[:end])
}

// StoreChangelogURLs returns the ordered list of HTTPS API URLs (primary + jsDelivr mirror)
// for fetching a plugin's full changelog JSON document.
func (s *StoreManager) StoreChangelogURLs(entry StoreAppEntry) []string {
	var candidates []string
	seen := make(map[string]bool)
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			return
		}
		if _, err := parseArchiveURL(raw); err != nil {
			return
		}
		seen[raw] = true
		candidates = append(candidates, raw)
	}

	if u := strings.TrimSpace(entry.ChangelogURL); u != "" {
		add(u)
		if mirror := rawGitHubToJSDelivr(u); mirror != "" {
			add(mirror)
		}
	}

	if pluginIDRe.MatchString(strings.TrimSpace(entry.ID)) {
		id := strings.TrimSpace(entry.ID)
		for _, idxURL := range s.IndexURLs {
			if derived := deriveChangelogURLFromIndex(idxURL, id); derived != "" {
				add(derived)
			}
		}
		if len(candidates) == 0 && strings.TrimSpace(entry.Repo) != "" {
			rawURL := defaultChangelogRawURL + "/" + id + ".json"
			add(rawURL)
			if mirror := rawGitHubToJSDelivr(rawURL); mirror != "" {
				add(mirror)
			}
		}
	}
	return candidates
}

func deriveChangelogURLFromIndex(indexURL, pluginID string) string {
	u, err := parseArchiveURL(indexURL)
	if err != nil {
		return ""
	}
	if !strings.HasSuffix(u.Path, "/store-index.json") {
		return ""
	}
	u.Path = strings.TrimSuffix(u.Path, "/store-index.json") + "/changelogs/" + pluginID + ".json"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func rawGitHubToJSDelivr(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Host, "raw.githubusercontent.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 {
		return ""
	}
	owner, repo, branch := parts[0], parts[1], parts[2]
	rest := strings.Join(parts[3:], "/")
	return fmt.Sprintf("https://cdn.jsdelivr.net/gh/%s/%s@%s/%s", owner, repo, branch, rest)
}

// ReadPluginChangelog fetches the plugin's release changelog using the store changelog JSON API,
// falling back to GitHub Releases API, inline catalog entries, and local CHANGELOG files.
func (s *StoreManager) ReadPluginChangelog(ctx context.Context, entry StoreAppEntry, installed InstalledApp, locale string) (PluginChangelog, error) {
	id := strings.TrimSpace(entry.ID)
	if id == "" {
		id = strings.TrimSpace(installed.Manifest.ID)
	}
	repo := readmeRepo(entry.Repo)
	if repo == "" {
		repo = readmeRepo(installed.Manifest.Repo)
	}

	var lastErr error
	apiURLs := s.StoreChangelogURLs(entry)
	for _, apiURL := range apiURLs {
		doc, err := s.fetchStoreChangelogJSON(ctx, apiURL)
		if err == nil && len(doc.Releases) > 0 {
			if doc.ID == "" {
				doc.ID = id
			}
			if doc.Repo == "" {
				doc.Repo = repo
			}
			if doc.ChangelogURL == "" {
				doc.ChangelogURL = apiURL
			}
			doc.Source = "store-api"
			return doc, nil
		}
		if err != nil && !errors.Is(err, ErrChangelogUnavailable) {
			lastErr = err
		}
	}

	if repo != "" {
		doc, err := s.fetchGitHubReleasesChangelog(ctx, id, repo)
		if err == nil && len(doc.Releases) > 0 {
			if len(apiURLs) > 0 {
				doc.ChangelogURL = apiURLs[0]
			}
			doc.Source = "github"
			return doc, nil
		}
		if err != nil && !errors.Is(err, ErrChangelogUnavailable) {
			lastErr = err
		}
	}

	if len(entry.Changelog) > 0 {
		releases := make([]PluginReleaseEntry, len(entry.Changelog))
		copy(releases, entry.Changelog)
		doc := PluginChangelog{
			App:           TargetApp,
			SchemaVersion: CurrentSchemaVersion,
			ID:            id,
			Repo:          repo,
			UpdatedAt:     entry.UpdatedAt,
			LatestVersion: releases[0].Version,
			ChangelogURL:  entry.ChangelogURL,
			Releases:      releases,
			Source:        "index",
		}
		return doc, nil
	}

	en := strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en")
	if md, ok := readLocalChangelog(installed.InstallDir, en); ok {
		return PluginChangelog{
			App:           TargetApp,
			SchemaVersion: CurrentSchemaVersion,
			ID:            id,
			Repo:          repo,
			LatestVersion: installed.Manifest.Version,
			Markdown:      md,
			Source:        "local",
		}, nil
	}

	if lastErr != nil {
		return PluginChangelog{}, lastErr
	}
	return PluginChangelog{}, ErrChangelogUnavailable
}

func (s *StoreManager) fetchStoreChangelogJSON(ctx context.Context, rawURL string) (PluginChangelog, error) {
	u, err := parseArchiveURL(rawURL)
	if err != nil {
		return PluginChangelog{}, err
	}
	reqCtx, cancel := withStageTimeout(ctx, changelogStageTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return PluginChangelog{}, err
	}
	req.Header.Set("User-Agent", "Block-AppStore/1.0 (blockmd)")
	client := *s.HTTP
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		target, err := parseArchiveURL(next.URL.String())
		if err != nil || len(via) >= 10 || target.Host != u.Host {
			return errors.New("plugin changelog redirect changed its source")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return PluginChangelog{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return PluginChangelog{}, ErrChangelogUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		return PluginChangelog{}, fmt.Errorf("fetch plugin changelog: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChangelogBytes+1))
	if err != nil {
		return PluginChangelog{}, err
	}
	if len(body) > maxChangelogBytes {
		return PluginChangelog{}, errors.New("plugin changelog exceeds size limit")
	}
	var doc PluginChangelog
	if err := json.Unmarshal(body, &doc); err != nil {
		return PluginChangelog{}, err
	}
	if len(doc.Releases) > maxChangelogReleases {
		doc.Releases = doc.Releases[:maxChangelogReleases]
	}
	if len(doc.Releases) == 0 {
		return PluginChangelog{}, ErrChangelogUnavailable
	}
	if doc.LatestVersion == "" {
		doc.LatestVersion = doc.Releases[0].Version
	}
	return doc, nil
}

func (s *StoreManager) fetchGitHubReleasesChangelog(ctx context.Context, id, repo string) (PluginChangelog, error) {
	reqCtx, cancel := withStageTimeout(ctx, changelogStageTimeout)
	defer cancel()
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", repo, maxChangelogReleases)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return PluginChangelog{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Block-AppStore/1.0 (blockmd)")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return PluginChangelog{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return PluginChangelog{}, ErrChangelogUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		return PluginChangelog{}, fmt.Errorf("fetch GitHub releases: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChangelogBytes+1))
	if err != nil {
		return PluginChangelog{}, err
	}
	if len(body) > maxChangelogBytes {
		return PluginChangelog{}, errors.New("GitHub releases response exceeds size limit")
	}
	var rawReleases []struct {
		TagName     string `json:"tag_name"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
		PublishedAt string `json:"published_at"`
		CreatedAt   string `json:"created_at"`
		HTMLURL     string `json:"html_url"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(body, &rawReleases); err != nil {
		return PluginChangelog{}, err
	}
	releases := make([]PluginReleaseEntry, 0, len(rawReleases))
	for _, r := range rawReleases {
		if r.Draft || r.Prerelease || strings.TrimSpace(r.TagName) == "" {
			continue
		}
		pub := r.PublishedAt
		if pub == "" {
			pub = r.CreatedAt
		}
		releases = append(releases, NormalizePluginReleaseEntry(r.TagName, pub, r.HTMLURL, r.Body, true))
		if len(releases) >= maxChangelogReleases {
			break
		}
	}
	if len(releases) == 0 {
		return PluginChangelog{}, ErrChangelogUnavailable
	}
	return PluginChangelog{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		ID:            id,
		Repo:          repo,
		UpdatedAt:     releases[0].PublishedAt,
		LatestVersion: releases[0].Version,
		Releases:      releases,
	}, nil
}

func readLocalChangelog(installDir string, en bool) (string, bool) {
	if strings.TrimSpace(installDir) == "" {
		return "", false
	}
	names := []string{"CHANGELOG.md", "CHANGELOG.zh.md", "changelog.md"}
	if en {
		names = []string{"CHANGELOG.en.md", "CHANGELOG.md", "changelog.md"}
	}
	for _, name := range names {
		doc, err := readLocalReadme(filepath.Join(installDir, name))
		if err == nil && strings.TrimSpace(doc) != "" {
			return doc, true
		}
	}
	return "", false
}

// ReadPluginChangelog resolves the plugin entry by ID and reads its changelog via the store API.
func (s *Supervisor) ReadPluginChangelog(ctx context.Context, pluginID, locale string) (PluginChangelog, error) {
	id := strings.TrimSpace(pluginID)
	if id == "" || !pluginIDRe.MatchString(id) {
		return PluginChangelog{}, fmt.Errorf("invalid plugin id %q", pluginID)
	}
	if locale == "" {
		locale = s.Locale()
	}
	var entry StoreAppEntry
	entry.ID = id
	if catalog := s.Catalog(); catalog != nil {
		for _, app := range catalog.Apps {
			if app.ID == id {
				entry = app
				break
			}
		}
	}
	installed, _ := s.GetInstalled(id)
	return s.store.ReadPluginChangelog(ctx, entry, installed, locale)
}

// StoreChangelogURLs returns the static changelog JSON API URLs for pluginID.
func (s *Supervisor) StoreChangelogURLs(pluginID string) []string {
	id := strings.TrimSpace(pluginID)
	if id == "" || !pluginIDRe.MatchString(id) {
		return nil
	}
	entry := StoreAppEntry{ID: id}
	if catalog := s.Catalog(); catalog != nil {
		for _, app := range catalog.Apps {
			if app.ID == id {
				entry = app
				break
			}
		}
	}
	return s.store.StoreChangelogURLs(entry)
}

func (s *Supervisor) dispatchStoreChangelogMethod(rt *Runtime, params json.RawMessage) (any, *rpcError) {
	s.mu.RLock()
	active := s.isRuntimeActiveLocked(rt.manifest.ID, rt)
	s.mu.RUnlock()
	if !active {
		return nil, rpcErr(-32007, "host.store.changelog requires an active runtime")
	}
	var p struct {
		ID     string `json:"id"`
		Locale string `json:"locale"`
		Limit  int    `json:"limit"`
	}
	if len(params) > 0 && json.Unmarshal(params, &p) != nil {
		return nil, rpcErr(-32602, "invalid params for host.store.changelog")
	}
	targetID := strings.TrimSpace(p.ID)
	if targetID == "" {
		targetID = rt.manifest.ID
	}
	if !pluginIDRe.MatchString(targetID) {
		return nil, rpcErr(-32602, "invalid plugin id")
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 10
	} else if limit > maxChangelogReleases {
		limit = maxChangelogReleases
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc, err := s.ReadPluginChangelog(ctx, targetID, strings.TrimSpace(p.Locale))
	if err != nil {
		if errors.Is(err, ErrChangelogUnavailable) {
			return nil, rpcErr(-32004, "plugin changelog unavailable")
		}
		return nil, rpcErr(-32005, err.Error())
	}
	if len(doc.Releases) > limit {
		doc.Releases = doc.Releases[:limit]
	}
	return doc, nil
}
