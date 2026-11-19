package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// SourceMode controls how Block selects between domestic acceleration mirrors and native GitHub.
type SourceMode string

const (
	SourceAuto   SourceMode = "auto"
	SourceMirror SourceMode = "mirror"
	SourceGitHub SourceMode = "github"
)

// NormalizeSourceMode normalizes persisted state strings into a valid SourceMode.
func NormalizeSourceMode(raw string) SourceMode {
	switch SourceMode(strings.ToLower(strings.TrimSpace(raw))) {
	case SourceMirror:
		return SourceMirror
	case SourceGitHub:
		return SourceGitHub
	default:
		return SourceAuto
	}
}

const (
	PublicRepo           = "eachann1024/block"
	PublicReleasesWebURL = "https://github.com/eachann1024/block/releases"
	maxFeedBytes         = 4 * 1024 * 1024
	headerTimeout        = 12 * time.Second
	stallTimeout         = 20 * time.Second
)

var allowlistedInstallers = map[string]bool{
	"block-macos-arm64.dmg":       true,
	"block-macos-x64.dmg":         true,
	"block-windows-x64-setup.exe": true,
	"block-linux-x64.deb":         true,
	"block-linux-x64.AppImage":    true,
}

// LabeledURL pairs a candidate endpoint URL with its display channel kind ("mirror" | "github").
type LabeledURL struct {
	URL     string
	Channel string // "mirror" | "github"
	Kind    string // "feed" | "github-api" | "asset"
}

// IsTrustedReleaseAsset verifies that an installer belongs to the official repo & allowlist and carries a valid SHA-256.
func IsTrustedReleaseAsset(asset ReleaseAsset, tag string) bool {
	if !allowlistedInstallers[asset.Name] {
		return false
	}
	if asset.EffectiveSHA256() == "" {
		return false
	}
	cleanTag := strings.TrimSpace(tag)
	vTag := cleanTag
	if cleanTag != "" && !strings.HasPrefix(strings.ToLower(cleanTag), "v") {
		vTag = "v" + cleanTag
	}
	expected := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", PublicRepo, url.PathEscape(cleanTag), asset.Name)
	expectedV := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", PublicRepo, url.PathEscape(vTag), asset.Name)
	return asset.DownloadURL == expected || asset.DownloadURL == expectedV
}

func newHTTPTransportClient(respHeaderTimeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyFromEnvironment
	tr.ResponseHeaderTimeout = respHeaderTimeout
	return &http.Client{Transport: tr}
}

var sharedHTTPClient = newHTTPTransportClient(headerTimeout)

type ghAPIRawRelease struct {
	TagName     string         `json:"tag_name"`
	HTMLURL     string         `json:"html_url"`
	PublishedAt string         `json:"published_at"`
	Body        string         `json:"body"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	Assets      []ReleaseAsset `json:"assets"`
}

// Mirrors transport installer bytes. The expected digest comes directly from the official repository.
func feedCandidates() []LabeledURL {
	return []LabeledURL{
		{URL: fmt.Sprintf("https://raw.githubusercontent.com/%s/main/updates/feed.json?t=%d", PublicRepo, time.Now().Unix()), Channel: "github", Kind: "feed"},
		{URL: fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=30", PublicRepo), Channel: "github", Kind: "github-api"},
	}
}

// CandidateAssetURLs expands an official GitHub Release asset URL into ordered mirror and/or native download URLs.
func CandidateAssetURLs(canonicalURL string, mode SourceMode) []LabeledURL {
	if !strings.HasPrefix(canonicalURL, "https://github.com/"+PublicRepo+"/releases/download/") {
		return nil
	}
	m1 := LabeledURL{URL: "https://gh-proxy.com/" + canonicalURL, Channel: "mirror", Kind: "asset"}
	m2 := LabeledURL{URL: "https://ghproxy.net/" + canonicalURL, Channel: "mirror", Kind: "asset"}
	gh := LabeledURL{URL: canonicalURL, Channel: "github", Kind: "asset"}

	switch mode {
	case SourceMirror:
		return []LabeledURL{m1, m2, gh}
	case SourceGitHub:
		return []LabeledURL{gh}
	default:
		return []LabeledURL{m1, gh, m2}
	}
}

func fetchOneCandidate(ctx context.Context, cand LabeledURL) ([]ReleaseEntry, []byte, int64, error) {
	started := time.Now()
	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, cand.URL, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("User-Agent", "Block-Updater/"+CurrentVersion())
	req.Header.Set("Cache-Control", "no-cache")

	client := *sharedHTTPClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 || next.URL.Scheme != "https" || next.URL.Host != req.URL.Host {
			return errors.New("untrusted release metadata redirect")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, time.Since(started).Milliseconds(), err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, time.Since(started).Milliseconds(), fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	elapsedMs := time.Since(started).Milliseconds()
	if err != nil {
		return nil, nil, elapsedMs, err
	}
	if len(raw) > maxFeedBytes {
		return nil, nil, elapsedMs, errors.New("update metadata exceeds the size limit")
	}

	if cand.Kind == "feed" {
		entries, parseErr := ParseFeedBytes(raw)
		if parseErr != nil {
			return nil, nil, elapsedMs, parseErr
		}
		return entries, raw, elapsedMs, nil
	}

	entries, parseErr := parseGitHubAPIReleases(raw)
	if parseErr != nil {
		return nil, nil, elapsedMs, parseErr
	}
	feedBytes, _ := json.Marshal(FeedDocument{
		SchemaVersion: 1,
		Repo:          PublicRepo,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		Releases:      entries,
	})
	return entries, feedBytes, elapsedMs, nil
}

// ParseFeedBytes validates and decodes updates/feed.json bytes.
func ParseFeedBytes(raw []byte) ([]ReleaseEntry, error) {
	var doc FeedDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Releases) == 0 {
		return nil, errors.New("empty update feed")
	}
	out := make([]ReleaseEntry, 0, len(doc.Releases))
	for idx, r := range doc.Releases {
		ver, tag, _, ok := ParseReleaseTag(r.Tag)
		if !ok {
			ver, tag, _, ok = ParseReleaseTag(r.Version)
		}
		if !ok {
			continue
		}
		r.Version = ver
		if tag == "" || !strings.HasPrefix(strings.ToLower(tag), "v") {
			r.Tag = "v" + ver
		} else {
			r.Tag = tag
		}
		filtered := make([]ReleaseAsset, 0, len(r.Assets))
		for _, a := range r.Assets {
			if IsTrustedReleaseAsset(a, r.Tag) {
				a.SHA256 = a.EffectiveSHA256()
				filtered = append(filtered, a)
			}
		}
		if idx == 0 && len(r.Assets) > 0 && len(filtered) == 0 {
			logWarn("check.feed_assets_rejected", "version", r.Version, "tag", r.Tag, "raw_assets", len(r.Assets))
		}
		r.Assets = filtered
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, errors.New("no valid releases in feed")
	}
	if len(out[0].Assets) == 0 {
		return nil, fmt.Errorf("latest release %s in feed has no trusted installer assets", out[0].Tag)
	}
	return out, nil
}

func parseGitHubAPIReleases(raw []byte) ([]ReleaseEntry, error) {
	var list []ghAPIRawRelease
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]ReleaseEntry, 0, len(list))
	for _, item := range list {
		if item.Draft || item.Prerelease {
			continue
		}
		ver, tag, _, ok := ParseReleaseTag(item.TagName)
		if !ok {
			continue
		}
		zh, en := ParseReleaseBody(item.Body)
		filtered := make([]ReleaseAsset, 0, len(item.Assets))
		for _, a := range item.Assets {
			if IsTrustedReleaseAsset(a, tag) {
				a.SHA256 = a.EffectiveSHA256()
				filtered = append(filtered, a)
			}
		}
		htmlURL := item.HTMLURL
		if htmlURL == "" {
			htmlURL = fmt.Sprintf("https://github.com/%s/releases/tag/%s", PublicRepo, url.PathEscape(tag))
		}
		out = append(out, ReleaseEntry{
			Version:     ver,
			Tag:         tag,
			PublishedAt: item.PublishedAt,
			HTMLURL:     htmlURL,
			Zh:          zh,
			En:          en,
			Assets:      filtered,
		})
	}
	if len(out) == 0 {
		return nil, errors.New("no published releases found")
	}
	return out, nil
}

// FetchReleases retrieves authoritative release metadata directly from GitHub.
func FetchReleases(ctx context.Context, mode SourceMode) (entries []ReleaseEntry, channel string, latencyMs int64, rawFeed []byte, err error) {
	cands := feedCandidates()
	logInfo("check.fetch_start", "mode", mode, "candidates", len(cands))
	var lastErr error
	for _, cand := range cands {
		if ctx.Err() != nil {
			return nil, "", 0, nil, ctx.Err()
		}
		e, raw, ms, fErr := fetchOneCandidate(ctx, cand)
		if fErr == nil && len(e) > 0 {
			logInfo("check.candidate_ok", "channel", cand.Channel, "kind", cand.Kind, "latency_ms", ms, "releases", len(e), "latest", e[0].Version)
			return e, cand.Channel, ms, raw, nil
		}
		logWarn("check.candidate_failover", "channel", cand.Channel, "kind", cand.Kind, "url", cand.URL, "latency_ms", ms, "err", fErr)
		lastErr = fErr
	}
	if lastErr == nil {
		lastErr = errors.New("all update sources failed")
	}
	logError("check.all_failed", "mode", mode, "err", lastErr)
	return nil, "", 0, nil, lastErr
}

// stalledBody aborts an HTTP stream if zero bytes arrive within stallTimeout.
type stalledBody struct {
	body    io.ReadCloser
	cancel  context.CancelFunc
	timeout time.Duration
	lastNs  atomic.Int64
	stalled atomic.Bool
	closed  atomic.Bool
	timer   *time.Timer
}

func openDownloadStream(ctx context.Context, rawURL string) (io.ReadCloser, int64, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, 0, err
	}
	if u.Scheme != "https" {
		return nil, 0, fmt.Errorf("update download requires HTTPS: %s", rawURL)
	}
	reqCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Block-Updater/"+CurrentVersion())
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		cancel()
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	sb := &stalledBody{
		body:    resp.Body,
		cancel:  cancel,
		timeout: stallTimeout,
	}
	sb.lastNs.Store(time.Now().UnixNano())
	sb.timer = time.AfterFunc(stallTimeout/4, sb.watch)
	return sb, resp.ContentLength, nil
}

func (s *stalledBody) watch() {
	if s.closed.Load() {
		return
	}
	if time.Since(time.Unix(0, s.lastNs.Load())) >= s.timeout {
		s.stalled.Store(true)
		s.cancel()
		return
	}
	s.timer.Reset(s.timeout / 4)
}

func (s *stalledBody) Read(p []byte) (int, error) {
	n, err := s.body.Read(p)
	if n > 0 {
		s.lastNs.Store(time.Now().UnixNano())
	}
	if err != nil && s.stalled.Load() {
		return n, errors.New("download stream stalled (no data for 20s)")
	}
	return n, err
}

func (s *stalledBody) Close() error {
	s.closed.Store(true)
	if s.timer != nil {
		s.timer.Stop()
	}
	s.cancel()
	return s.body.Close()
}
