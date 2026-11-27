package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
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

// Logical update channels.
const (
	ChannelMirror = "mirror"
	ChannelGitHub = "github"
)

// ProbeStatus represents the probing lifecycle state of a channel or endpoint node.
type ProbeStatus string

const (
	ProbeIdle    ProbeStatus = "idle"
	ProbeProbing ProbeStatus = "probing"
	ProbeOK      ProbeStatus = "ok"
	ProbeFailed  ProbeStatus = "failed"
)

// NodeProbe records the latest probe measurement for a single endpoint node.
type NodeProbe struct {
	Channel    string
	Host       string
	Kind       string
	Status     ProbeStatus
	LatencyMs  int64
	HTTPStatus int
	ErrorKind  string
	Error      string
	ProbedAt   time.Time
}

// ChannelProbe summarizes the health and fastest latency of a logical update channel.
type ChannelProbe struct {
	Channel    string
	Status     ProbeStatus
	LatencyMs  int64
	HTTPStatus int
	ErrorKind  string
	Error      string
	BestHost   string
	ProbedAt   time.Time
	Nodes      []NodeProbe
}

// Clone returns a deep copy of ChannelProbe safe to read outside locks.
func (c ChannelProbe) Clone() ChannelProbe {
	out := c
	if len(c.Nodes) > 0 {
		out.Nodes = append([]NodeProbe(nil), c.Nodes...)
	}
	return out
}

// FeedFetchResult carries fetched release entries along with routing and probe telemetry.
type FeedFetchResult struct {
	Entries     []ReleaseEntry
	Channel     string
	Host        string
	LatencyMs   int64
	RawFeed     []byte
	MirrorProbe *ChannelProbe
	GitHubProbe *ChannelProbe
}

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
	probeTimeout         = 6 * time.Second
	probeFreshTTL        = 60 * time.Second
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
	Host    string
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

var mirrorHosts = []string{"gh-proxy.com", "ghproxy.net"}

func rawFeedURL() string {
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/main/updates/feed.json?t=%d", PublicRepo, time.Now().Unix())
}

func orderedMirrorHosts(hint ChannelProbe) []string {
	type scored struct {
		host string
		tier int // 0=ok, 1=idle/probing, 2=failed
		ms   int64
		idx  int
	}
	byHost := make(map[string]NodeProbe, len(hint.Nodes))
	for _, n := range hint.Nodes {
		byHost[n.Host] = n
	}
	list := make([]scored, len(mirrorHosts))
	for i, h := range mirrorHosts {
		tier := 1
		var ms int64
		if n, ok := byHost[h]; ok {
			switch n.Status {
			case ProbeOK:
				tier = 0
				ms = n.LatencyMs
			case ProbeFailed:
				tier = 2
			}
		}
		list[i] = scored{host: h, tier: tier, ms: ms, idx: i}
	}
	slices.SortFunc(list, func(a, b scored) int {
		if a.tier != b.tier {
			return a.tier - b.tier
		}
		if a.tier == 0 && a.ms != b.ms {
			if a.ms < b.ms {
				return -1
			}
			return 1
		}
		return a.idx - b.idx
	})
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.host
	}
	return out
}

// DefaultChannelProbe returns an idle ChannelProbe pre-populated with all configured nodes for channel.
func DefaultChannelProbe(channel string) ChannelProbe {
	switch channel {
	case ChannelMirror:
		nodes := make([]NodeProbe, len(mirrorHosts))
		for i, h := range mirrorHosts {
			nodes[i] = NodeProbe{Channel: ChannelMirror, Host: h, Kind: "feed", Status: ProbeIdle}
		}
		return ChannelProbe{Channel: ChannelMirror, Status: ProbeIdle, Nodes: nodes}
	case ChannelGitHub:
		return ChannelProbe{
			Channel: ChannelGitHub,
			Status:  ProbeIdle,
			Nodes: []NodeProbe{
				{Channel: ChannelGitHub, Host: "raw.githubusercontent.com", Kind: "feed", Status: ProbeIdle},
				{Channel: ChannelGitHub, Host: "api.github.com", Kind: "github-api", Status: ProbeIdle},
			},
		}
	default:
		return ChannelProbe{Channel: channel, Status: ProbeIdle}
	}
}

func mirrorFeedCandidates(hint ChannelProbe) []LabeledURL {
	raw := rawFeedURL()
	hosts := orderedMirrorHosts(hint)
	out := make([]LabeledURL, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, LabeledURL{
			URL:     "https://" + h + "/" + raw,
			Host:    h,
			Channel: ChannelMirror,
			Kind:    "feed",
		})
	}
	return out
}

func githubFeedCandidates() []LabeledURL {
	return []LabeledURL{
		{URL: rawFeedURL(), Host: "raw.githubusercontent.com", Channel: ChannelGitHub, Kind: "feed"},
		{URL: fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=30", PublicRepo), Host: "api.github.com", Channel: ChannelGitHub, Kind: "github-api"},
	}
}

// CandidateAssetURLs expands an official GitHub Release asset URL into ordered mirror and/or native download URLs.
func CandidateAssetURLs(canonicalURL string, mode SourceMode) []LabeledURL {
	return CandidateAssetURLsRouted(canonicalURL, mode, ChannelProbe{}, ChannelProbe{})
}

// CandidateAssetURLsRouted expands an official GitHub Release asset URL using live channel probes
// so SourceAuto prioritizes the fastest reachable route and SourceMirror/SourceGitHub strictly honor mode.
func CandidateAssetURLsRouted(canonicalURL string, mode SourceMode, mirrorHint, githubHint ChannelProbe) []LabeledURL {
	if !strings.HasPrefix(canonicalURL, "https://github.com/"+PublicRepo+"/releases/download/") {
		return nil
	}
	hosts := orderedMirrorHosts(mirrorHint)
	mirrors := make([]LabeledURL, 0, len(hosts))
	for _, h := range hosts {
		mirrors = append(mirrors, LabeledURL{
			URL:     "https://" + h + "/" + canonicalURL,
			Host:    h,
			Channel: ChannelMirror,
			Kind:    "asset",
		})
	}
	gh := LabeledURL{URL: canonicalURL, Host: "github.com", Channel: ChannelGitHub, Kind: "asset"}

	switch mode {
	case SourceMirror:
		return mirrors
	case SourceGitHub:
		return []LabeledURL{gh}
	default:
		preferGitHub := githubHint.Status == ProbeOK &&
			(mirrorHint.Status != ProbeOK || (mirrorHint.LatencyMs > 0 && githubHint.LatencyMs > 0 && githubHint.LatencyMs < mirrorHint.LatencyMs))
		if preferGitHub {
			out := make([]LabeledURL, 0, 1+len(mirrors))
			out = append(out, gh)
			out = append(out, mirrors...)
			return out
		}
		out := make([]LabeledURL, 0, len(mirrors)+1)
		out = append(out, mirrors...)
		out = append(out, gh)
		return out
	}
}

type cachedAPIEntry struct {
	etag    string
	entries []ReleaseEntry
	rawFeed []byte
}

var (
	apiETagMu    sync.Mutex
	apiETagCache = make(map[string]cachedAPIEntry)
)

func classifyNetworkError(err error, httpStatus int) (kind, sanitized string) {
	if err == nil {
		return "", ""
	}
	sanitized = SanitizeNetworkError(err)
	lower := strings.ToLower(err.Error())

	var ne net.Error
	var de *net.DNSError
	var oe *net.OpError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return "timeout", sanitized
	case errors.Is(err, context.Canceled):
		return "canceled", sanitized
	case errors.As(err, &de):
		return "dns", sanitized
	case strings.Contains(lower, "x509:") || strings.Contains(lower, "tls:") || strings.Contains(lower, "certificate"):
		return "tls", sanitized
	case strings.Contains(lower, "untrusted release metadata redirect"):
		return "redirect", sanitized
	case httpStatus == http.StatusForbidden || httpStatus == http.StatusTooManyRequests:
		return "rate_limit", sanitized
	case httpStatus >= 400:
		return "http", sanitized
	case errors.As(err, &oe) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		strings.Contains(lower, "connection refused") || strings.Contains(lower, "connection reset"):
		return "connect", sanitized
	case strings.Contains(lower, "json") || strings.Contains(lower, "feed") || strings.Contains(lower, "releases"):
		return "parse", sanitized
	default:
		return "network", sanitized
	}
}

func candidateHost(cand LabeledURL) string {
	if cand.Host != "" {
		return cand.Host
	}
	if u, err := url.Parse(cand.URL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return cand.Channel
}

func isHTTPSDefaultPort(u *url.URL) bool {
	if u == nil || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	port := u.Port()
	return port == "" || port == "443"
}

func isTrustedMetadataRedirect(cand LabeledURL, reqURL, nextURL *url.URL) bool {
	if !isHTTPSDefaultPort(nextURL) {
		return false
	}
	nextHost := strings.ToLower(nextURL.Hostname())
	reqHost := strings.ToLower(reqURL.Hostname())
	officialFeedPath := "/" + PublicRepo + "/main/updates/feed.json"
	officialAPIPathPrefix := "/repos/" + PublicRepo + "/releases"

	if nextHost == "raw.githubusercontent.com" {
		return nextURL.Path == officialFeedPath
	}
	if nextHost == "api.github.com" {
		return cand.Channel == ChannelGitHub && strings.HasPrefix(nextURL.Path, officialAPIPathPrefix)
	}
	if cand.Channel == ChannelMirror {
		sameOrSub := nextHost == reqHost || strings.HasSuffix(nextHost, "."+reqHost)
		if !sameOrSub {
			return false
		}
		return strings.Contains(nextURL.Path, PublicRepo+"/main/updates/feed.json")
	}
	return false
}

func isTrustedDownloadRedirect(reqURL, nextURL *url.URL) bool {
	if !isHTTPSDefaultPort(nextURL) {
		return false
	}
	nextHost := strings.ToLower(nextURL.Hostname())
	reqHost := strings.ToLower(reqURL.Hostname())
	if nextHost == reqHost || strings.HasSuffix(nextHost, "."+reqHost) {
		return true
	}
	switch nextHost {
	case "github.com",
		"objects.githubusercontent.com",
		"release-assets.githubusercontent.com",
		"codeload.github.com":
		return true
	}
	return strings.HasSuffix(nextHost, ".githubusercontent.com")
}

func fetchOneCandidateDetailed(ctx context.Context, cand LabeledURL, timeout time.Duration, useETag bool) ([]ReleaseEntry, []byte, NodeProbe, error) {
	started := time.Now()
	host := candidateHost(cand)
	node := NodeProbe{
		Channel: cand.Channel,
		Host:    host,
		Kind:    cand.Kind,
		Status:  ProbeFailed,
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, cand.URL, nil)
	if err != nil {
		node.ProbedAt = time.Now()
		node.ErrorKind, node.Error = classifyNetworkError(err, 0)
		return nil, nil, node, err
	}
	req.Header.Set("User-Agent", "Block-Updater/"+CurrentVersion())
	req.Header.Set("Cache-Control", "no-cache")

	var cached cachedAPIEntry
	hasCached := false
	if useETag && cand.Kind == "github-api" {
		apiETagMu.Lock()
		cached, hasCached = apiETagCache[cand.URL]
		apiETagMu.Unlock()
		if hasCached && cached.etag != "" && len(cached.entries) > 0 && len(cached.rawFeed) > 0 {
			req.Header.Set("If-None-Match", cached.etag)
		} else {
			hasCached = false
		}
	}

	client := *sharedHTTPClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !isTrustedMetadataRedirect(cand, req.URL, next.URL) {
			return errors.New("untrusted release metadata redirect")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		node.LatencyMs = time.Since(started).Milliseconds()
		node.ProbedAt = time.Now()
		node.ErrorKind, node.Error = classifyNetworkError(err, 0)
		return nil, nil, node, err
	}
	defer resp.Body.Close()

	node.HTTPStatus = resp.StatusCode
	if hasCached && resp.StatusCode == http.StatusNotModified {
		elapsedMs := max(time.Since(started).Milliseconds(), int64(1))
		node.Status = ProbeOK
		node.LatencyMs = elapsedMs
		node.ProbedAt = time.Now()
		return cached.entries, cached.rawFeed, node, nil
	}
	if resp.StatusCode != http.StatusOK {
		node.LatencyMs = time.Since(started).Milliseconds()
		node.ProbedAt = time.Now()
		httpErr := fmt.Errorf("HTTP %d", resp.StatusCode)
		node.ErrorKind, node.Error = classifyNetworkError(httpErr, resp.StatusCode)
		return nil, nil, node, httpErr
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	elapsedMs := time.Since(started).Milliseconds()
	if err != nil {
		node.LatencyMs = elapsedMs
		node.ProbedAt = time.Now()
		node.ErrorKind, node.Error = classifyNetworkError(err, resp.StatusCode)
		return nil, nil, node, err
	}
	if len(raw) > maxFeedBytes {
		sizeErr := errors.New("update metadata exceeds the size limit")
		node.LatencyMs = elapsedMs
		node.ProbedAt = time.Now()
		node.ErrorKind, node.Error = classifyNetworkError(sizeErr, resp.StatusCode)
		return nil, nil, node, sizeErr
	}

	var entries []ReleaseEntry
	var feedBytes []byte
	if cand.Kind == "feed" {
		var parseErr error
		entries, parseErr = ParseFeedBytes(raw)
		if parseErr != nil {
			node.LatencyMs = elapsedMs
			node.ProbedAt = time.Now()
			node.ErrorKind, node.Error = classifyNetworkError(parseErr, resp.StatusCode)
			return nil, nil, node, parseErr
		}
		feedBytes = raw
	} else {
		var parseErr error
		entries, parseErr = parseGitHubAPIReleases(raw)
		if parseErr != nil {
			node.LatencyMs = elapsedMs
			node.ProbedAt = time.Now()
			node.ErrorKind, node.Error = classifyNetworkError(parseErr, resp.StatusCode)
			return nil, nil, node, parseErr
		}
		feedBytes, _ = json.Marshal(FeedDocument{
			SchemaVersion: 1,
			Repo:          PublicRepo,
			UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
			LatestVersion: entries[0].Version,
			LatestTag:     entries[0].Tag,
			Releases:      entries,
		})
		if etag := strings.TrimSpace(resp.Header.Get("ETag")); etag != "" && len(entries) > 0 {
			apiETagMu.Lock()
			apiETagCache[cand.URL] = cachedAPIEntry{etag: etag, entries: entries, rawFeed: feedBytes}
			apiETagMu.Unlock()
		}
	}

	if elapsedMs < 1 {
		elapsedMs = 1
	}
	node.Status = ProbeOK
	node.LatencyMs = elapsedMs
	node.ProbedAt = time.Now()
	return entries, feedBytes, node, nil
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
			if !ok {
				continue
			}
			if !strings.HasPrefix(strings.ToLower(tag), "v") {
				tag = "v" + ver
			}
		} else if strings.TrimSpace(r.Version) != "" {
			if v2, _, _, ok2 := ParseReleaseTag(r.Version); ok2 && v2 != ver {
				continue
			}
		}
		r.Version = ver
		r.Tag = tag
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
	slices.SortFunc(out, func(a, b ReleaseEntry) int {
		return CompareVersion(b.Version, a.Version)
	})
	hasAnyTrustedAsset := false
	for _, entry := range out {
		if len(entry.Assets) > 0 {
			hasAnyTrustedAsset = true
			break
		}
	}
	if !hasAnyTrustedAsset {
		return nil, fmt.Errorf("release feed starting at %s has no trusted installer assets", out[0].Tag)
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
	slices.SortFunc(out, func(a, b ReleaseEntry) int {
		return CompareVersion(b.Version, a.Version)
	})
	return out, nil
}

var errReleaseNotFound = errors.New("release tag not found")

// FetchReleaseByTag retrieves one authoritative published release directly from GitHub by exact tag.
func FetchReleaseByTag(ctx context.Context, tag string) (*ReleaseEntry, error) {
	cleanTag := strings.TrimSpace(tag)
	if cleanTag == "" {
		return nil, errReleaseNotFound
	}
	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", PublicRepo, url.PathEscape(cleanTag))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: HTTP 404", errReleaseNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxFeedBytes {
		return nil, errors.New("release metadata exceeds the size limit")
	}
	var item ghAPIRawRelease
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}
	if item.Draft || item.Prerelease {
		return nil, errors.New("release is not published")
	}
	ver, parsedTag, _, ok := ParseReleaseTag(item.TagName)
	if !ok {
		return nil, errors.New("release tag is invalid")
	}
	zh, en := ParseReleaseBody(item.Body)
	filtered := make([]ReleaseAsset, 0, len(item.Assets))
	for _, a := range item.Assets {
		if IsTrustedReleaseAsset(a, parsedTag) {
			a.SHA256 = a.EffectiveSHA256()
			filtered = append(filtered, a)
		}
	}
	htmlURL := item.HTMLURL
	if htmlURL == "" {
		htmlURL = fmt.Sprintf("https://github.com/%s/releases/tag/%s", PublicRepo, url.PathEscape(parsedTag))
	}
	return &ReleaseEntry{
		Version:     ver,
		Tag:         parsedTag,
		PublishedAt: item.PublishedAt,
		HTMLURL:     htmlURL,
		Zh:          zh,
		En:          en,
		Assets:      filtered,
	}, nil
}

func summarizeChannelProbe(channel string, nodes []NodeProbe) ChannelProbe {
	cp := ChannelProbe{
		Channel: channel,
		Status:  ProbeIdle,
		Nodes:   append([]NodeProbe(nil), nodes...),
	}
	var bestOK *NodeProbe
	anyProbing := false
	anyFailed := false
	allFailedTimeout := true
	var repFail *NodeProbe
	var latestProbed time.Time

	for i := range nodes {
		n := &nodes[i]
		if n.ProbedAt.After(latestProbed) {
			latestProbed = n.ProbedAt
		}
		switch n.Status {
		case ProbeOK:
			if bestOK == nil || n.LatencyMs < bestOK.LatencyMs {
				bestOK = n
			}
		case ProbeProbing:
			anyProbing = true
		case ProbeFailed:
			anyFailed = true
			if n.ErrorKind != "timeout" {
				allFailedTimeout = false
			}
			if repFail == nil || (repFail.ErrorKind == "canceled" && n.ErrorKind != "canceled") {
				repFail = n
			}
		}
	}

	switch {
	case bestOK != nil:
		cp.Status = ProbeOK
		cp.LatencyMs = bestOK.LatencyMs
		cp.BestHost = bestOK.Host
		cp.HTTPStatus = bestOK.HTTPStatus
		cp.ProbedAt = bestOK.ProbedAt
	case anyProbing:
		cp.Status = ProbeProbing
	case anyFailed:
		cp.Status = ProbeFailed
		cp.ProbedAt = latestProbed
		if repFail != nil {
			cp.HTTPStatus = repFail.HTTPStatus
			cp.ErrorKind = repFail.ErrorKind
			cp.Error = repFail.Error
		}
		if allFailedTimeout {
			cp.ErrorKind = "timeout"
		}
	}
	return cp
}

// ProbeChannel concurrently probes every endpoint node configured for channel ("mirror" or "github").
func ProbeChannel(
	ctx context.Context,
	channel string,
	hint ChannelProbe,
	useETag bool,
	onUpdate func(ChannelProbe),
) (ChannelProbe, []ReleaseEntry, []byte) {
	var cands []LabeledURL
	if channel == ChannelMirror {
		cands = mirrorFeedCandidates(hint)
	} else {
		cands = githubFeedCandidates()
	}

	nodes := make([]NodeProbe, len(cands))
	for i, cand := range cands {
		nodes[i] = NodeProbe{
			Channel: channel,
			Host:    candidateHost(cand),
			Kind:    cand.Kind,
			Status:  ProbeProbing,
		}
	}
	var (
		mu             sync.Mutex
		emitMu         sync.Mutex
		emitSeq        uint64
		lastEmittedSeq uint64
		winEntries     []ReleaseEntry
		winRaw         []byte
		winLatency     int64
		wg             sync.WaitGroup
	)
	emit := func(seq uint64, cp ChannelProbe) {
		if onUpdate == nil {
			return
		}
		emitMu.Lock()
		defer emitMu.Unlock()
		if seq <= lastEmittedSeq {
			return
		}
		lastEmittedSeq = seq
		onUpdate(cp)
	}
	if onUpdate != nil {
		emitSeq++
		emit(emitSeq, summarizeChannelProbe(channel, nodes))
	}
	for i, cand := range cands {
		wg.Add(1)
		go func(idx int, c LabeledURL) {
			defer wg.Done()
			entries, raw, node, err := fetchOneCandidateDetailed(ctx, c, probeTimeout, useETag)
			if err == nil && len(entries) > 0 {
				logInfo("probe.node_ok", "channel", c.Channel, "host", node.Host, "kind", c.Kind, "latency_ms", node.LatencyMs, "http_status", node.HTTPStatus)
			} else {
				logWarn("probe.node_fail", "channel", c.Channel, "host", node.Host, "kind", c.Kind, "latency_ms", node.LatencyMs, "http_status", node.HTTPStatus, "err_kind", node.ErrorKind, "err", node.Error)
			}
			mu.Lock()
			nodes[idx] = node
			if err == nil && len(entries) > 0 && (winEntries == nil || node.LatencyMs < winLatency) {
				winEntries = entries
				winRaw = raw
				winLatency = node.LatencyMs
			}
			emitSeq++
			seq := emitSeq
			cp := summarizeChannelProbe(channel, nodes)
			mu.Unlock()
			emit(seq, cp)
		}(i, cand)
	}
	wg.Wait()

	mu.Lock()
	finalCP := summarizeChannelProbe(channel, nodes)
	entries := winEntries
	raw := winRaw
	mu.Unlock()
	return finalCP, entries, raw
}

func fetchChannelSequential(
	ctx context.Context,
	channel string,
	hint ChannelProbe,
	useETag bool,
) (ChannelProbe, []ReleaseEntry, []byte, error) {
	var cands []LabeledURL
	if channel == ChannelMirror {
		cands = mirrorFeedCandidates(hint)
	} else {
		cands = githubFeedCandidates()
	}

	cp := DefaultChannelProbe(channel)
	nodeIndex := make(map[string]int, len(cp.Nodes))
	for i, n := range cp.Nodes {
		nodeIndex[n.Host] = i
	}

	var lastErr error
	for _, cand := range cands {
		if ctx.Err() != nil {
			return summarizeChannelProbe(channel, cp.Nodes), nil, nil, ctx.Err()
		}
		entries, raw, node, fErr := fetchOneCandidateDetailed(ctx, cand, headerTimeout, useETag)
		if idx, ok := nodeIndex[node.Host]; ok {
			cp.Nodes[idx] = node
		} else {
			cp.Nodes = append(cp.Nodes, node)
		}
		if fErr == nil && len(entries) > 0 {
			logInfo("check.candidate_ok", "channel", cand.Channel, "host", node.Host, "kind", cand.Kind, "latency_ms", node.LatencyMs, "releases", len(entries), "latest", entries[0].Version)
			return summarizeChannelProbe(channel, cp.Nodes), entries, raw, nil
		}
		logWarn("check.candidate_failover", "channel", cand.Channel, "host", node.Host, "kind", cand.Kind, "latency_ms", node.LatencyMs, "http_status", node.HTTPStatus, "err_kind", node.ErrorKind, "err", node.Error)
		lastErr = fErr
	}
	if lastErr == nil {
		lastErr = errors.New("all channel candidates failed")
	}
	return summarizeChannelProbe(channel, cp.Nodes), nil, nil, lastErr
}

// FetchReleases retrieves authoritative release metadata for mode without prior probe hints.
func FetchReleases(ctx context.Context, mode SourceMode) (entries []ReleaseEntry, channel string, latencyMs int64, rawFeed []byte, err error) {
	res, err := FetchReleasesRouted(ctx, mode, ChannelProbe{}, ChannelProbe{}, nil)
	if err != nil {
		return nil, "", 0, nil, err
	}
	return res.Entries, res.Channel, res.LatencyMs, res.RawFeed, nil
}

// FetchReleasesRouted fetches release metadata honoring mode and using live probe hints
// to route SourceAuto to the fastest available channel with automatic failover.
func FetchReleasesRouted(
	ctx context.Context,
	mode SourceMode,
	mirrorHint, githubHint ChannelProbe,
	onProbeUpdate func(channel string, cp ChannelProbe),
) (FeedFetchResult, error) {
	norm := NormalizeSourceMode(string(mode))
	logInfo("check.fetch_start", "mode", norm)

	switch norm {
	case SourceGitHub:
		cp, entries, raw, err := fetchChannelSequential(ctx, ChannelGitHub, githubHint, false)
		if onProbeUpdate != nil {
			onProbeUpdate(ChannelGitHub, cp)
		}
		if err != nil {
			logError("check.all_failed", "mode", norm, "err", SanitizeNetworkError(err))
			return FeedFetchResult{GitHubProbe: &cp}, err
		}
		return FeedFetchResult{
			Entries:     entries,
			Channel:     ChannelGitHub,
			Host:        cp.BestHost,
			LatencyMs:   cp.LatencyMs,
			RawFeed:     raw,
			GitHubProbe: &cp,
		}, nil

	case SourceMirror:
		cp, entries, raw := ProbeChannel(ctx, ChannelMirror, mirrorHint, false, func(upd ChannelProbe) {
			if onProbeUpdate != nil {
				onProbeUpdate(ChannelMirror, upd)
			}
		})
		if len(entries) == 0 || cp.Status != ProbeOK {
			err := errors.New("all mirror sources failed")
			if cp.Error != "" {
				err = errors.New(cp.Error)
			} else if ctx.Err() != nil {
				err = ctx.Err()
			}
			logError("check.all_failed", "mode", norm, "err", SanitizeNetworkError(err))
			return FeedFetchResult{MirrorProbe: &cp}, err
		}
		return FeedFetchResult{
			Entries:     entries,
			Channel:     ChannelMirror,
			Host:        cp.BestHost,
			LatencyMs:   cp.LatencyMs,
			RawFeed:     raw,
			MirrorProbe: &cp,
		}, nil

	default: // SourceAuto
		now := time.Now()
		freshMirror := mirrorHint.Status == ProbeOK && !mirrorHint.ProbedAt.IsZero() && now.Sub(mirrorHint.ProbedAt) <= probeFreshTTL
		freshGitHub := githubHint.Status == ProbeOK && !githubHint.ProbedAt.IsZero() && now.Sub(githubHint.ProbedAt) <= probeFreshTTL

		if freshMirror || freshGitHub {
			order := []string{ChannelMirror, ChannelGitHub}
			if freshGitHub && (!freshMirror || githubHint.LatencyMs < mirrorHint.LatencyMs) {
				order = []string{ChannelGitHub, ChannelMirror}
			}
			var res FeedFetchResult
			var lastErr error
			for i, ch := range order {
				hint := mirrorHint
				if ch == ChannelGitHub {
					hint = githubHint
				}
				// Give the primary channel a capped budget so a slow/hanging primary cannot
				// starve failover to the backup channel within the overall check context.
				chCtx := ctx
				var chCancel context.CancelFunc
				if i == 0 && len(order) > 1 {
					budget := 9 * time.Second
					if dl, ok := ctx.Deadline(); ok {
						if rem := time.Until(dl); rem > 2*time.Second {
							half := rem/2 + time.Second
							if half < budget {
								budget = half
							}
						}
					}
					chCtx, chCancel = context.WithTimeout(ctx, budget)
				}
				cp, entries, raw, err := fetchChannelSequential(chCtx, ch, hint, false)
				if chCancel != nil {
					chCancel()
				}
				if ch == ChannelMirror {
					cpCopy := cp
					res.MirrorProbe = &cpCopy
				} else {
					cpCopy := cp
					res.GitHubProbe = &cpCopy
				}
				if onProbeUpdate != nil {
					onProbeUpdate(ch, cp)
				}
				if err == nil && len(entries) > 0 {
					res.Entries = entries
					res.Channel = ch
					res.Host = cp.BestHost
					res.LatencyMs = cp.LatencyMs
					res.RawFeed = raw
					return res, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = errors.New("all update sources failed")
			}
			logError("check.all_failed", "mode", norm, "err", SanitizeNetworkError(lastErr))
			return res, lastErr
		}

		// Cold check or retry after outage: race Mirror and GitHub at node granularity so
		// one fast reachable node wins immediately even if a sibling node in the same channel hangs.
		type raceOutcome struct {
			channel string
			cp      ChannelProbe
			entries []ReleaseEntry
			raw     []byte
			early   bool
		}
		raceCtx := ctx
		var raceCancel context.CancelFunc
		if onProbeUpdate != nil {
			raceCtx, raceCancel = context.WithTimeout(context.Background(), probeTimeout+4*time.Second)
		} else {
			raceCtx, raceCancel = context.WithCancel(ctx)
		}

		resCh := make(chan raceOutcome, 4)
		var remaining atomic.Int32
		remaining.Store(2)

		runChannel := func(ch string, hint ChannelProbe) {
			var cands []LabeledURL
			if ch == ChannelMirror {
				cands = mirrorFeedCandidates(hint)
			} else {
				cands = githubFeedCandidates()
			}
			nodes := make([]NodeProbe, len(cands))
			for i, cand := range cands {
				nodes[i] = NodeProbe{
					Channel: ch,
					Host:    candidateHost(cand),
					Kind:    cand.Kind,
					Status:  ProbeProbing,
				}
			}
			var (
				mu             sync.Mutex
				emitMu         sync.Mutex
				emitSeq        uint64
				lastEmittedSeq uint64
				earlySent      bool
				winEntries     []ReleaseEntry
				winRaw         []byte
				winLatency     int64
				wg             sync.WaitGroup
			)
			emit := func(seq uint64, cp ChannelProbe) {
				if onProbeUpdate == nil {
					return
				}
				emitMu.Lock()
				defer emitMu.Unlock()
				if seq <= lastEmittedSeq {
					return
				}
				lastEmittedSeq = seq
				onProbeUpdate(ch, cp)
			}
			if onProbeUpdate != nil {
				emitSeq++
				emit(emitSeq, summarizeChannelProbe(ch, nodes))
			}
			for i, cand := range cands {
				wg.Add(1)
				go func(idx int, c LabeledURL) {
					defer wg.Done()
					entries, raw, node, err := fetchOneCandidateDetailed(raceCtx, c, probeTimeout, false)
					if err == nil && len(entries) > 0 {
						logInfo("probe.node_ok", "channel", c.Channel, "host", node.Host, "kind", c.Kind, "latency_ms", node.LatencyMs, "http_status", node.HTTPStatus)
					} else {
						logWarn("probe.node_fail", "channel", c.Channel, "host", node.Host, "kind", c.Kind, "latency_ms", node.LatencyMs, "http_status", node.HTTPStatus, "err_kind", node.ErrorKind, "err", node.Error)
					}
					mu.Lock()
					nodes[idx] = node
					shouldEarly := false
					if err == nil && len(entries) > 0 && (winEntries == nil || node.LatencyMs < winLatency) {
						winEntries = entries
						winRaw = raw
						winLatency = node.LatencyMs
						if !earlySent {
							earlySent = true
							shouldEarly = true
						}
					}
					emitSeq++
					seq := emitSeq
					cp := summarizeChannelProbe(ch, nodes)
					mu.Unlock()
					emit(seq, cp)
					if shouldEarly {
						resCh <- raceOutcome{channel: ch, cp: cp, entries: entries, raw: raw, early: true}
					}
				}(i, cand)
			}
			wg.Wait()
			mu.Lock()
			finalCP := summarizeChannelProbe(ch, nodes)
			entries := winEntries
			raw := winRaw
			mu.Unlock()
			resCh <- raceOutcome{channel: ch, cp: finalCP, entries: entries, raw: raw, early: false}
			if remaining.Add(-1) == 0 {
				raceCancel()
			}
		}
		go runChannel(ChannelMirror, mirrorHint)
		go runChannel(ChannelGitHub, githubHint)

		var result FeedFetchResult
		var win *raceOutcome
		finals := 0
		for finals < 2 {
			select {
			case <-ctx.Done():
				raceCancel()
				return result, ctx.Err()
			case out := <-resCh:
				cpCopy := out.cp
				if out.channel == ChannelMirror {
					result.MirrorProbe = &cpCopy
				} else {
					result.GitHubProbe = &cpCopy
				}
				if !out.early {
					finals++
				}
				if win == nil && out.cp.Status == ProbeOK && len(out.entries) > 0 {
					cpWin := out
					win = &cpWin
					result.Entries = out.entries
					result.Channel = out.channel
					result.Host = out.cp.BestHost
					result.LatencyMs = out.cp.LatencyMs
					result.RawFeed = out.raw
					if onProbeUpdate != nil {
						// Return first reachable node immediately while remaining probes finish reporting.
						return result, nil
					}
					// No live listener: cancel remaining slow nodes and return the winner right away.
					raceCancel()
					return result, nil
				} else if win != nil && out.cp.Status == ProbeOK && len(out.entries) > 0 && out.cp.LatencyMs < win.cp.LatencyMs {
					cpWin := out
					win = &cpWin
					result.Entries = out.entries
					result.Channel = out.channel
					result.Host = out.cp.BestHost
					result.LatencyMs = out.cp.LatencyMs
					result.RawFeed = out.raw
				}
			}
		}
		if win != nil {
			return result, nil
		}
		errMsg := "all update sources failed"
		if result.MirrorProbe != nil && result.MirrorProbe.Error != "" {
			errMsg = result.MirrorProbe.Error
		} else if result.GitHubProbe != nil && result.GitHubProbe.Error != "" {
			errMsg = result.GitHubProbe.Error
		}
		logError("check.all_failed", "mode", norm, "err", errMsg)
		return result, errors.New(errMsg)
	}
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
	if !isHTTPSDefaultPort(u) {
		return nil, 0, fmt.Errorf("update download requires HTTPS: %s", rawURL)
	}
	reqCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Block-Updater/"+CurrentVersion())
	client := *sharedHTTPClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !isTrustedDownloadRedirect(req.URL, next.URL) {
			return errors.New("untrusted release download redirect")
		}
		return nil
	}
	resp, err := client.Do(req)
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
