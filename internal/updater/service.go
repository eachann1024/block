package updater

import (
	"block/updates"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

// Status represents the lifecycle phase of the application updater.
type Status string

const (
	StatusIdle        Status = "idle"
	StatusChecking    Status = "checking"
	StatusUpToDate    Status = "up-to-date"
	StatusAvailable   Status = "available"
	StatusDownloading Status = "downloading"
	StatusReady       Status = "ready"
	StatusInstalling  Status = "installing"
	StatusError       Status = "error"
)

// ViewMode switches between cross-version merged summary and per-version timeline.
type ViewMode string

const (
	ViewMerged    ViewMode = "merged"
	ViewByVersion ViewMode = "versions"
)

// Snapshot is an immutable, UI-ready view of the updater state.
type Snapshot struct {
	Status              Status
	CurrentVersion      string
	LatestVersion       string
	PublishedAt         string
	ReleaseURL          string
	SourceMode          SourceMode
	ReleaseChannel      ReleaseChannel
	InstalledChannel    ReleaseChannel
	LatestStableVersion string
	WaitingForStable    bool
	ActiveChannel       string // "mirror" | "github" | ""
	FetchLatencyMs      int64
	MirrorProbe         ChannelProbe
	GitHubProbe         ChannelProbe
	ProbingChannels     bool
	RetestingTarget     bool
	LastPhase           string
	LastErrorSanitized  string
	SpeedMBps           float64
	ViewMode            ViewMode
	DownloadedBytes     int64
	TotalBytes          int64
	Percent             int
	Preparing           bool
	DownloadPath        string
	LogPath             string
	InPlace             bool
	PackageManaged      bool
	ErrorMessage        string
	Notes               AggregatedNotes
}

// ProbeFor returns the latest probe snapshot for channel ("mirror" or "github").
func (s Snapshot) ProbeFor(channel string) ChannelProbe {
	var cp ChannelProbe
	switch channel {
	case ChannelMirror:
		cp = s.MirrorProbe
	case ChannelGitHub:
		cp = s.GitHubProbe
	}
	if cp.Channel == "" {
		cp.Channel = channel
	}
	if cp.Status == "" {
		cp.Status = ProbeIdle
	}
	return cp
}

// EffectiveChannel returns the currently resolved delivery channel for s.SourceMode.
func (s Snapshot) EffectiveChannel() string {
	switch s.SourceMode {
	case SourceMirror:
		return ChannelMirror
	case SourceGitHub:
		return ChannelGitHub
	default:
		mOK := s.MirrorProbe.Status == ProbeOK && s.MirrorProbe.LatencyMs > 0
		gOK := s.GitHubProbe.Status == ProbeOK && s.GitHubProbe.LatencyMs > 0
		switch {
		case mOK && gOK:
			if s.GitHubProbe.LatencyMs < s.MirrorProbe.LatencyMs {
				return ChannelGitHub
			}
			return ChannelMirror
		case mOK:
			return ChannelMirror
		case gOK:
			return ChannelGitHub
		case s.ActiveChannel == ChannelMirror || s.ActiveChannel == ChannelGitHub:
			if s.ProbeFor(s.ActiveChannel).Status != ProbeFailed {
				return s.ActiveChannel
			}
		}
		return ""
	}
}

// AllRoutesUnavailable reports whether both physical routes (Mirror and GitHub) have
// been probed/checked and failed, leaving all three options (Auto, Mirror, GitHub) unavailable.
func (s Snapshot) AllRoutesUnavailable() bool {
	return s.MirrorProbe.Status == ProbeFailed &&
		s.GitHubProbe.Status == ProbeFailed &&
		!s.ProbingChannels &&
		!s.RetestingTarget
}

// HasActionableUpdate reports whether the sidebar badge should appear.
func (s Snapshot) HasActionableUpdate() bool {
	switch s.Status {
	case StatusAvailable, StatusDownloading, StatusReady, StatusInstalling:
		return CompareVersion(s.LatestVersion, s.CurrentVersion) > 0
	default:
		return false
	}
}

// Config wires workspace persistence, flush-before-quit, and redraw callbacks into Service.
type Config struct {
	DataDir               string
	InitialSourceMode     SourceMode
	InitialReleaseChannel ReleaseChannel
	// ChannelChosen is true once the user has explicitly picked a channel (persisted value present).
	ChannelChosen           bool
	LastUpdatedFrom         string
	IsEn                    func() bool
	OnSourceModeChanged     func(SourceMode)
	OnReleaseChannelChanged func(ReleaseChannel)
	FlushBeforeUpdate       func(fromVersion string) error
	QuitApp                 func(startInstaller func() error) error
	OnChange                func()
}

// Service coordinates silent background checks, multi-source downloads, staging, and installation.
type Service struct {
	cfg Config

	mu                    sync.Mutex
	status                Status
	sourceMode            SourceMode
	releaseChannel        ReleaseChannel
	activeChannel         string
	fetchLatencyMs        int64
	mirrorProbe           ChannelProbe
	githubProbe           ChannelProbe
	lastStableMirrorProbe ChannelProbe
	lastStableGitHubProbe ChannelProbe
	mirrorSeq             uint64
	githubSeq             uint64
	mirrorProbing         bool
	githubProbing         bool
	mirrorProbeCancel     context.CancelFunc
	githubProbeCancel     context.CancelFunc
	retestingTarget       bool
	retestGen             uint64
	highestStableVersion  string
	highestNightlyVersion string
	nightlyKnownStable    string
	channelChosen         bool
	lastPhase             string
	lastErrorSanitized    string
	speedMBps             float64
	viewMode              ViewMode
	lastUpdatedFrom       string
	releases              []ReleaseEntry
	stableReleases        []ReleaseEntry
	nightlyReleases       []ReleaseEntry
	notes                 AggregatedNotes
	latestEntry           *ReleaseEntry
	targetAsset           *ReleaseAsset
	readyRecord           *VerifiedRecord
	inPlace               bool
	pkgManaged            bool
	downloadedBytes       int64
	totalBytes            int64
	percent               int
	preparing             bool
	downloadPath          string
	errorMsg              string
	checkGen              uint64
	checkRunning          bool
	downloadGen           uint64
	downloadRunning       bool
	downloadCancel        context.CancelFunc
	downloadDone          chan struct{}
	installRunning        bool
	autoStarted           bool
	stopCh                chan struct{}
}

// NewService constructs a Service and immediately loads bundled/cached feeds and staged ready.json.
func NewService(cfg Config) *Service {
	if cfg.DataDir != "" {
		SetLogDataDir(cfg.DataDir)
	}
	s := &Service{
		cfg:             cfg,
		status:          StatusIdle,
		sourceMode:      NormalizeSourceMode(string(cfg.InitialSourceMode)),
		releaseChannel:  NormalizeReleaseChannel(string(cfg.InitialReleaseChannel)),
		channelChosen:   cfg.ChannelChosen,
		mirrorProbe:     DefaultChannelProbe(ChannelMirror),
		githubProbe:     DefaultChannelProbe(ChannelGitHub),
		lastPhase:       "idle",
		viewMode:        ViewMerged,
		lastUpdatedFrom: cfg.LastUpdatedFrom,
		stopCh:          make(chan struct{}),
	}
	cur := CurrentVersion()
	s.notes = AggregatedNotes{
		FromVersion: cur,
		ToVersion:   cur,
	}

	if len(updates.BundledFeed) > 0 {
		if entries, pErr := ParseFeedBytesForChannel(updates.BundledFeed, ReleaseStable); pErr == nil && len(entries) > 0 {
			s.stableReleases = entries
			s.highestStableVersion = entries[0].Version
		}
	}
	if cfg.DataDir != "" {
		if raw, err := os.ReadFile(feedCachePathForChannel(cfg.DataDir, ReleaseStable)); err == nil {
			if entries, pErr := ParseFeedBytesForChannel(raw, ReleaseStable); pErr == nil && len(entries) > 0 {
				if s.highestStableVersion == "" || CompareVersion(entries[0].Version, s.highestStableVersion) >= 0 {
					s.stableReleases = entries
					s.highestStableVersion = entries[0].Version
				}
			}
		}
		if raw, err := os.ReadFile(feedCachePathForChannel(cfg.DataDir, ReleaseNightly)); err == nil {
			if entries, pErr := ParseFeedBytesForChannel(raw, ReleaseNightly); pErr == nil && len(entries) > 0 {
				s.nightlyReleases = entries
				s.highestNightlyVersion = entries[0].Version
				s.absorbStableFromEntriesLocked(entries)
			}
		}
	}
	s.releases = s.releasesForChannelLocked(s.releaseChannel)
	if len(s.releases) > 0 {
		s.notes = AggregateReleases(s.releases, cur, s.lastUpdatedFrom)
	}
	if os.Getenv("BLOCK_MOCK_UPDATE") == "1" {
		s.installMockUpdate()
	}
	if lastErr := consumeLastUpdateError(); lastErr != "" {
		s.status = StatusError
		s.errorMsg = lastErr
		s.lastPhase = "previous_install"
		s.lastErrorSanitized = sanitizeDiagString(lastErr, cfg.DataDir)
		logError("update.previous_install_failed", "reason", lastErr)
	}
	return s
}

func (s *Service) notify() {
	if s.cfg.OnChange != nil {
		s.cfg.OnChange()
	}
}

func (s *Service) syncActiveChannelFromProbesLocked() {
	switch s.sourceMode {
	case SourceMirror:
		s.activeChannel = ChannelMirror
		switch s.mirrorProbe.Status {
		case ProbeOK:
			s.fetchLatencyMs = s.mirrorProbe.LatencyMs
		case ProbeFailed:
			s.fetchLatencyMs = 0
		}
	case SourceGitHub:
		s.activeChannel = ChannelGitHub
		switch s.githubProbe.Status {
		case ProbeOK:
			s.fetchLatencyMs = s.githubProbe.LatencyMs
		case ProbeFailed:
			s.fetchLatencyMs = 0
		}
	default: // SourceAuto
		mOK := s.mirrorProbe.Status == ProbeOK && s.mirrorProbe.LatencyMs > 0
		gOK := s.githubProbe.Status == ProbeOK && s.githubProbe.LatencyMs > 0
		switch {
		case mOK && gOK:
			if s.githubProbe.LatencyMs < s.mirrorProbe.LatencyMs {
				s.activeChannel = ChannelGitHub
				s.fetchLatencyMs = s.githubProbe.LatencyMs
			} else {
				s.activeChannel = ChannelMirror
				s.fetchLatencyMs = s.mirrorProbe.LatencyMs
			}
		case mOK:
			s.activeChannel = ChannelMirror
			s.fetchLatencyMs = s.mirrorProbe.LatencyMs
		case gOK:
			s.activeChannel = ChannelGitHub
			s.fetchLatencyMs = s.githubProbe.LatencyMs
		case s.mirrorProbe.Status == ProbeFailed && s.githubProbe.Status == ProbeFailed:
			s.activeChannel = ""
			s.fetchLatencyMs = 0
		}
	}
}

func (s *Service) releasesForChannelLocked(ch ReleaseChannel) []ReleaseEntry {
	if NormalizeReleaseChannel(string(ch)) == ReleaseNightly && len(s.nightlyReleases) > 0 {
		return s.nightlyReleases
	}
	return s.stableReleases
}

// absorbStableFromEntriesLocked only remembers which stable version the nightly feed knows about.
// It never replaces the stable list or the stable anti-rollback floor: nightly feed entries for
// older versions carry no installers, so they must not overwrite stable download metadata.
func (s *Service) absorbStableFromEntriesLocked(entries []ReleaseEntry) {
	for _, e := range entries {
		if e.IsNightly() {
			continue
		}
		if s.nightlyKnownStable == "" || CompareVersion(e.Version, s.nightlyKnownStable) > 0 {
			s.nightlyKnownStable = e.Version
		}
		return
	}
}

func (s *Service) isVersionKnownStableLocked(version string) bool {
	if version == "" {
		return false
	}
	for _, r := range s.stableReleases {
		if !r.IsNightly() && CompareVersion(r.Version, version) == 0 {
			return true
		}
	}
	for _, r := range s.nightlyReleases {
		if !r.IsNightly() && CompareVersion(r.Version, version) == 0 {
			return true
		}
	}
	return false
}

func (s *Service) installedChannelLocked(cur string) ReleaseChannel {
	if BuildReleaseChannel() == ReleaseStable {
		return ReleaseStable
	}
	// 从没主动选过通道的用户只会拿到稳定版（官网下载的也是升稳后的同一安装包），
	// 离线时清单可能还没包含这一版，不能据此标成每夜版。
	if !s.channelChosen && s.releaseChannel == ReleaseStable {
		return ReleaseStable
	}
	if s.isVersionKnownStableLocked(cur) {
		return ReleaseStable
	}
	return ReleaseNightly
}

func (s *Service) latestStableVersionLocked() string {
	latest := ""
	if len(s.stableReleases) > 0 {
		latest = s.stableReleases[0].Version
	}
	if s.nightlyKnownStable != "" && (latest == "" || CompareVersion(s.nightlyKnownStable, latest) > 0) {
		latest = s.nightlyKnownStable
	}
	return latest
}

func (s *Service) waitingForStableLocked(cur string) bool {
	if s.releaseChannel != ReleaseStable {
		return false
	}
	if s.installedChannelLocked(cur) != ReleaseNightly {
		return false
	}
	latestStable := s.latestStableVersionLocked()
	if latestStable == "" {
		return false
	}
	return CompareVersion(cur, latestStable) > 0
}

func (s *Service) highestKnownForChannelLocked(ch ReleaseChannel) string {
	if NormalizeReleaseChannel(string(ch)) == ReleaseNightly {
		return s.highestNightlyVersion
	}
	return s.highestStableVersion
}

func (s *Service) setHighestKnownForChannelLocked(ch ReleaseChannel, ver string) {
	if ver == "" {
		return
	}
	if NormalizeReleaseChannel(string(ch)) == ReleaseNightly {
		if s.highestNightlyVersion == "" || CompareVersion(ver, s.highestNightlyVersion) > 0 {
			s.highestNightlyVersion = ver
		}
		return
	}
	if s.highestStableVersion == "" || CompareVersion(ver, s.highestStableVersion) > 0 {
		s.highestStableVersion = ver
	}
}

// readyAllowedForChannelLocked reports whether a staged update may be installed on the current channel.
func (s *Service) readyAllowedForChannelLocked(rec *VerifiedRecord) bool {
	if rec == nil {
		return false
	}
	if s.releaseChannel == ReleaseNightly {
		return true
	}
	return rec.EffectiveReleaseChannel() == ReleaseStable || s.isVersionKnownStableLocked(rec.Version)
}

func (s *Service) dropReadyLocked() {
	s.readyRecord = nil
	if s.status == StatusReady {
		s.status = StatusIdle
	}
	if s.cfg.DataDir != "" {
		_ = os.Remove(readyRecordPath(s.cfg.DataDir))
		discardPreparedStage(s.cfg.DataDir)
	}
}

func (s *Service) currentReleaseChannel() ReleaseChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.releaseChannel
}

func (s *Service) reapplyActiveChannelReleasesLocked() {
	cur := CurrentVersion()
	entries := s.releasesForChannelLocked(s.releaseChannel)
	s.releases = entries
	s.notes = AggregateReleases(entries, cur, s.lastUpdatedFrom)
	s.errorMsg = ""
	s.lastErrorSanitized = ""

	if !s.notes.HasNewer || len(s.notes.Versions) == 0 {
		if s.readyRecord != nil && CompareVersion(s.readyRecord.Version, cur) > 0 &&
			(s.releaseChannel == ReleaseNightly || s.isVersionKnownStableLocked(s.readyRecord.Version)) {
			s.status = StatusReady
		} else {
			s.latestEntry = nil
			s.targetAsset = nil
			s.readyRecord = nil
			s.status = StatusUpToDate
		}
		return
	}

	var picked *ReleaseEntry
	var asset *ReleaseAsset
	var inPlace bool
	_, _, pkgManaged := PickPlatformAsset(s.notes.Versions[0].Assets, runtime.GOOS, runtime.GOARCH)
	for i := range s.notes.Versions {
		cand := s.notes.Versions[i]
		a, ip, pm := PickPlatformAsset(cand.Assets, runtime.GOOS, runtime.GOARCH)
		if pm {
			pkgManaged = true
		}
		if a != nil {
			c := cand
			picked = &c
			asset = a
			inPlace = ip
			break
		}
	}
	if picked == nil {
		latest := s.notes.Versions[0]
		picked = &latest
	}
	s.latestEntry = picked
	s.targetAsset = asset
	s.inPlace = inPlace
	s.pkgManaged = pkgManaged

	if s.readyRecord != nil && asset != nil && inPlace && s.readyRecord.Version == picked.Version &&
		s.readyRecord.Tag == picked.Tag && s.readyRecord.AssetName == asset.Name &&
		s.readyRecord.DownloadURL == asset.DownloadURL && s.readyRecord.SHA256 == asset.EffectiveSHA256() {
		s.status = StatusReady
	} else if s.readyRecord != nil && CompareVersion(s.readyRecord.Version, cur) > 0 &&
		(s.releaseChannel == ReleaseNightly || s.isVersionKnownStableLocked(s.readyRecord.Version)) &&
		(asset == nil || CompareVersion(s.readyRecord.Version, picked.Version) > 0) {
		s.status = StatusReady
	} else {
		s.readyRecord = nil
		s.status = StatusAvailable
		if asset != nil {
			s.totalBytes = asset.Size
		}
	}
}

// Snapshot returns a thread-safe copy of the current updater state.
func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur := CurrentVersion()
	latest := s.notes.ToVersion
	if latest == "" {
		latest = cur
	}
	relURL := PublicReleasesWebURL
	if s.latestEntry != nil && s.latestEntry.HTMLURL != "" {
		relURL = s.latestEntry.HTMLURL
	} else if s.readyRecord != nil && s.readyRecord.ReleaseURL != "" {
		relURL = s.readyRecord.ReleaseURL
	}
	pubAt := s.notes.PublishedAt
	if pubAt == "" && s.readyRecord != nil {
		pubAt = s.readyRecord.PublishedAt
	}
	if s.status == StatusReady && s.readyRecord != nil {
		latest = s.readyRecord.Version
		if s.readyRecord.PublishedAt != "" {
			pubAt = s.readyRecord.PublishedAt
		}
		if s.readyRecord.ReleaseURL != "" {
			relURL = s.readyRecord.ReleaseURL
		}
	}

	return Snapshot{
		Status:              s.status,
		CurrentVersion:      cur,
		LatestVersion:       latest,
		PublishedAt:         pubAt,
		ReleaseURL:          relURL,
		SourceMode:          s.sourceMode,
		ReleaseChannel:      s.releaseChannel,
		InstalledChannel:    s.installedChannelLocked(cur),
		LatestStableVersion: s.latestStableVersionLocked(),
		WaitingForStable:    s.waitingForStableLocked(cur),
		ActiveChannel:       s.activeChannel,
		FetchLatencyMs:      s.fetchLatencyMs,
		MirrorProbe:         s.mirrorProbe.Clone(),
		GitHubProbe:         s.githubProbe.Clone(),
		ProbingChannels:     s.mirrorProbing || s.githubProbing,
		RetestingTarget:     s.retestingTarget,
		LastPhase:           s.lastPhase,
		LastErrorSanitized:  s.lastErrorSanitized,
		SpeedMBps:           s.speedMBps,
		ViewMode:            s.viewMode,
		DownloadedBytes:     s.downloadedBytes,
		TotalBytes:          s.totalBytes,
		Percent:             s.percent,
		Preparing:           s.preparing,
		DownloadPath:        s.downloadPath,
		LogPath:             LogFilePath(s.cfg.DataDir),
		InPlace:             s.inPlace,
		PackageManaged:      s.pkgManaged,
		ErrorMessage:        s.errorMsg,
		Notes:               s.notes,
	}
}

// HasUpdate reports whether an update is available, downloading, or staged & ready.
func (s *Service) HasUpdate() bool {
	return s.Snapshot().HasActionableUpdate()
}

// SetViewMode switches between merged summary and per-version release notes.
func (s *Service) SetViewMode(mode ViewMode) {
	s.mu.Lock()
	if s.viewMode == mode {
		s.mu.Unlock()
		return
	}
	s.viewMode = mode
	s.mu.Unlock()
	s.notify()
}

// SetSourceMode updates the preferred mirror/GitHub channel and persists it.
func (s *Service) SetSourceMode(mode SourceMode) {
	norm := NormalizeSourceMode(string(mode))
	s.mu.Lock()
	changed := s.sourceMode != norm
	s.sourceMode = norm
	s.syncActiveChannelFromProbesLocked()
	s.mu.Unlock()
	if changed {
		logInfo("config.source_mode_changed", "mode", norm)
		if s.cfg.OnSourceModeChanged != nil {
			s.cfg.OnSourceModeChanged(norm)
		}
		s.notify()
	}
}

// SetReleaseChannel switches between stable and nightly update channels, persists it, and checks for updates.
func (s *Service) SetReleaseChannel(ch ReleaseChannel) {
	norm := NormalizeReleaseChannel(string(ch))
	s.mu.Lock()
	if s.releaseChannel == norm {
		s.mu.Unlock()
		return
	}
	s.releaseChannel = norm
	s.channelChosen = true
	var dlCancel context.CancelFunc
	if norm == ReleaseStable && s.downloadRunning && s.latestEntry != nil && s.latestEntry.IsNightly() {
		dlCancel = s.downloadCancel
		s.downloadCancel = nil
		s.downloadGen++
		s.downloadRunning = false
		s.preparing = false
		s.speedMBps = 0
		s.percent = 0
		s.downloadedBytes = 0
	}
	if s.readyRecord != nil && !s.readyAllowedForChannelLocked(s.readyRecord) {
		s.dropReadyLocked()
	}
	s.checkGen++
	s.checkRunning = false
	s.reapplyActiveChannelReleasesLocked()
	s.mu.Unlock()

	if dlCancel != nil {
		dlCancel()
	}
	logInfo("config.release_channel_changed", "channel", norm)
	if s.cfg.OnReleaseChannelChanged != nil {
		s.cfg.OnReleaseChannelChanged(norm)
	}
	s.notify()
	s.CheckAsync(false)
}

func setChannelProbing(cp *ChannelProbe) {
	cp.Status = ProbeProbing
	cp.LatencyMs = 0
	cp.HTTPStatus = 0
	cp.ErrorKind = ""
	cp.Error = ""
	cp.BestHost = ""
	for i := range cp.Nodes {
		cp.Nodes[i].Status = ProbeProbing
		cp.Nodes[i].LatencyMs = 0
		cp.Nodes[i].HTTPStatus = 0
		cp.Nodes[i].ErrorKind = ""
		cp.Nodes[i].Error = ""
	}
}

func (s *Service) stableProbeHintLocked(channel string) ChannelProbe {
	if channel == ChannelMirror {
		if s.mirrorProbe.Status == ProbeOK || s.mirrorProbe.Status == ProbeFailed {
			return s.mirrorProbe.Clone()
		}
		if s.lastStableMirrorProbe.Status == ProbeOK || s.lastStableMirrorProbe.Status == ProbeFailed {
			return s.lastStableMirrorProbe.Clone()
		}
		return s.mirrorProbe.Clone()
	}
	if s.githubProbe.Status == ProbeOK || s.githubProbe.Status == ProbeFailed {
		return s.githubProbe.Clone()
	}
	if s.lastStableGitHubProbe.Status == ProbeOK || s.lastStableGitHubProbe.Status == ProbeFailed {
		return s.lastStableGitHubProbe.Clone()
	}
	return s.githubProbe.Clone()
}

func (s *Service) setChannelProbeLocked(channel string, cp ChannelProbe) {
	if channel == ChannelMirror {
		s.mirrorProbe = cp.Clone()
		if cp.Status == ProbeOK || cp.Status == ProbeFailed {
			s.lastStableMirrorProbe = cp.Clone()
		}
	} else if channel == ChannelGitHub {
		s.githubProbe = cp.Clone()
		if cp.Status == ProbeOK || cp.Status == ProbeFailed {
			s.lastStableGitHubProbe = cp.Clone()
		}
	}
}

// ProbeAllAsync concurrently probes both Mirror and GitHub channels and streams live updates.
func (s *Service) ProbeAllAsync() {
	s.startProbeChannelsAsync([]string{ChannelMirror, ChannelGitHub}, 0)
}

// SelectSourceAndRetest switches to mode, immediately updates the active channel readout,
// retests the target route(s), and refreshes update availability when idle, up-to-date, or errored.
func (s *Service) SelectSourceAndRetest(mode SourceMode) {
	norm := NormalizeSourceMode(string(mode))
	s.mu.Lock()
	changed := s.sourceMode != norm
	s.sourceMode = norm
	s.syncActiveChannelFromProbesLocked()
	s.retestingTarget = true
	s.retestGen++
	rGen := s.retestGen
	needsCheck := s.status == StatusError || s.status == StatusIdle || s.status == StatusUpToDate || s.latestEntry == nil
	s.mu.Unlock()

	if changed {
		logInfo("config.source_mode_changed", "mode", norm)
		if s.cfg.OnSourceModeChanged != nil {
			s.cfg.OnSourceModeChanged(norm)
		}
	}
	s.notify()

	if needsCheck {
		s.checkAsyncInternal(true, nil, rGen)
		return
	}
	channels := []string{ChannelMirror, ChannelGitHub}
	switch norm {
	case SourceMirror:
		channels = []string{ChannelMirror}
	case SourceGitHub:
		channels = []string{ChannelGitHub}
	}
	s.startProbeChannelsAsync(channels, rGen)
}

func (s *Service) startProbeChannelsAsync(channels []string, retestGen uint64) {
	if os.Getenv("BLOCK_MOCK_UPDATE") == "1" {
		s.mu.Lock()
		s.installMockProbesLocked()
		s.mirrorProbing = false
		s.githubProbing = false
		if retestGen == 0 || retestGen == s.retestGen {
			s.retestingTarget = false
		}
		s.syncActiveChannelFromProbesLocked()
		s.mu.Unlock()
		s.notify()
		return
	}

	type channelRun struct {
		channel string
		seq     uint64
		ctx     context.Context
		cancel  context.CancelFunc
		hint    ChannelProbe
	}

	var prevCancels []context.CancelFunc
	runs := make([]channelRun, 0, len(channels))

	s.mu.Lock()
	// If a full reprobe (retestGen == 0) starts while a target retest is in flight,
	// adopt ownership of retestingTarget so canceling the older probe never leaves it stuck.
	effectiveRetestGen := retestGen
	if effectiveRetestGen == 0 && s.retestingTarget {
		s.retestGen++
		effectiveRetestGen = s.retestGen
	}
	for _, ch := range channels {
		if ch != ChannelMirror && ch != ChannelGitHub {
			continue
		}
		hint := s.stableProbeHintLocked(ch)
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout+2*time.Second)
		if ch == ChannelMirror {
			if s.mirrorProbeCancel != nil {
				prevCancels = append(prevCancels, s.mirrorProbeCancel)
			}
			s.mirrorSeq++
			s.mirrorProbeCancel = cancel
			s.mirrorProbing = true
			if len(s.mirrorProbe.Nodes) == 0 {
				s.mirrorProbe = DefaultChannelProbe(ChannelMirror)
			}
			setChannelProbing(&s.mirrorProbe)
			runs = append(runs, channelRun{channel: ch, seq: s.mirrorSeq, ctx: ctx, cancel: cancel, hint: hint})
		} else {
			if s.githubProbeCancel != nil {
				prevCancels = append(prevCancels, s.githubProbeCancel)
			}
			s.githubSeq++
			s.githubProbeCancel = cancel
			s.githubProbing = true
			if len(s.githubProbe.Nodes) == 0 {
				s.githubProbe = DefaultChannelProbe(ChannelGitHub)
			}
			setChannelProbing(&s.githubProbe)
			runs = append(runs, channelRun{channel: ch, seq: s.githubSeq, ctx: ctx, cancel: cancel, hint: hint})
		}
	}
	s.mu.Unlock()

	for _, pc := range prevCancels {
		pc()
	}
	s.notify()

	go func() {
		var wg sync.WaitGroup
		for _, r := range runs {
			wg.Add(1)
			go func(run channelRun) {
				defer wg.Done()
				defer run.cancel()
				finalCP, _, _ := ProbeChannelForReleaseChannel(run.ctx, run.channel, s.currentReleaseChannel(), run.hint, true, func(upd ChannelProbe) {
					s.mu.Lock()
					if (run.channel == ChannelMirror && run.seq != s.mirrorSeq) ||
						(run.channel == ChannelGitHub && run.seq != s.githubSeq) {
						s.mu.Unlock()
						return
					}
					s.setChannelProbeLocked(run.channel, upd)
					s.syncActiveChannelFromProbesLocked()
					s.mu.Unlock()
					s.notify()
				})

				s.mu.Lock()
				if run.channel == ChannelMirror {
					if run.seq == s.mirrorSeq {
						s.mirrorProbing = false
						s.mirrorProbeCancel = nil
						if finalCP.Status == ProbeProbing {
							if s.lastStableMirrorProbe.Status != "" && s.lastStableMirrorProbe.Status != ProbeProbing {
								s.mirrorProbe = s.lastStableMirrorProbe.Clone()
							} else {
								s.mirrorProbe = DefaultChannelProbe(ChannelMirror)
							}
						} else {
							s.setChannelProbeLocked(ChannelMirror, finalCP)
						}
					}
				} else if run.channel == ChannelGitHub {
					if run.seq == s.githubSeq {
						s.githubProbing = false
						s.githubProbeCancel = nil
						if finalCP.Status == ProbeProbing {
							if s.lastStableGitHubProbe.Status != "" && s.lastStableGitHubProbe.Status != ProbeProbing {
								s.githubProbe = s.lastStableGitHubProbe.Clone()
							} else {
								s.githubProbe = DefaultChannelProbe(ChannelGitHub)
							}
						} else {
							s.setChannelProbeLocked(ChannelGitHub, finalCP)
						}
					}
				}
				s.syncActiveChannelFromProbesLocked()
				s.mu.Unlock()
				s.notify()
			}(r)
		}
		wg.Wait()

		shouldRecoverCheck := false
		s.mu.Lock()
		if effectiveRetestGen > 0 && effectiveRetestGen == s.retestGen {
			s.retestingTarget = false
		}
		s.syncActiveChannelFromProbesLocked()
		if s.status == StatusError && s.latestEntry == nil && !s.checkRunning {
			switch s.sourceMode {
			case SourceMirror:
				shouldRecoverCheck = s.mirrorProbe.Status == ProbeOK
			case SourceGitHub:
				shouldRecoverCheck = s.githubProbe.Status == ProbeOK
			default:
				shouldRecoverCheck = s.mirrorProbe.Status == ProbeOK || s.githubProbe.Status == ProbeOK
			}
		}
		s.mu.Unlock()
		s.notify()

		if shouldRecoverCheck {
			s.CheckAsync(true)
		}
	}()
}

// CancelDownload cancels an in-progress download and returns to StatusAvailable.
func (s *Service) CancelDownload() {
	s.mu.Lock()
	cancel := s.downloadCancel
	done := s.downloadDone
	s.downloadCancel = nil
	if !s.downloadRunning {
		s.mu.Unlock()
		return
	}
	s.downloadGen++
	s.downloadRunning = false
	s.preparing = false
	s.speedMBps = 0
	s.percent = 0
	s.downloadedBytes = 0
	if s.latestEntry != nil {
		s.status = StatusAvailable
	} else {
		s.status = StatusIdle
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	s.notify()
}

// StartAutomatic restores any previously staged update on disk, schedules an initial check
// 12s after launch, and repeats every 4 hours while running.
func (s *Service) StartAutomatic() {
	s.mu.Lock()
	if s.autoStarted {
		s.mu.Unlock()
		return
	}
	s.autoStarted = true
	dataDir := s.cfg.DataDir
	s.mu.Unlock()

	go func() {
		cur := CurrentVersion()
		if dataDir != "" {
			s.mu.Lock()
			lastUpdatedFrom := s.lastUpdatedFrom
			relCh := s.releaseChannel
			s.mu.Unlock()
			acknowledgeStartupAndClean(dataDir, cur, lastUpdatedFrom)
			if rec, ok := RestoreReadyForChannel(dataDir, cur, relCh); ok {
				_ = PrepareInstall(dataDir, rec, true)
				s.mu.Lock()
				if s.releaseChannel != relCh || !s.readyAllowedForChannelLocked(rec) {
					// 恢复期间用户切换了通道：丢弃切换前的结果。
					s.readyRecord = rec
					s.dropReadyLocked()
					s.mu.Unlock()
					goto scheduled
				}
				s.readyRecord = rec
				s.status = StatusReady
				s.inPlace = rec.InPlace
				s.downloadPath = rec.ArchivePath
				s.totalBytes = rec.Size
				s.downloadedBytes = rec.Size
				s.percent = 100
				s.activeChannel = rec.Channel
				if CompareVersion(s.notes.ToVersion, rec.Version) < 0 {
					s.notes.ToVersion = rec.Version
					s.notes.HasNewer = true
				}
				s.mu.Unlock()
				s.notify()
				s.CheckAsync(false)
			}
		}

	scheduled:
		select {
		case <-time.After(12 * time.Second):
			s.runScheduledCycle()
		case <-s.stopCh:
			return
		}

		ticker := time.NewTicker(4 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.runScheduledCycle()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *Service) runScheduledCycle() {
	s.CheckAsyncWithAfter(false, func() {
		s.mu.Lock()
		shouldAutoDownload := s.status == StatusAvailable && s.inPlace && s.targetAsset != nil && !s.downloadRunning
		s.mu.Unlock()
		if shouldAutoDownload {
			s.StartDownload()
		}
	})
}

// CheckAsync checks for updates in a background goroutine with request-generation race protection.
func (s *Service) CheckAsync(manual bool) {
	s.CheckAsyncWithAfter(manual, nil)
}

// CheckAsyncWithAfter checks for updates and invokes onDone when finished.
func (s *Service) CheckAsyncWithAfter(manual bool, onDone func()) {
	s.checkAsyncInternal(manual, onDone, 0)
}

func (s *Service) checkAsyncInternal(manual bool, onDone func(), retestGen uint64) {
	if os.Getenv("BLOCK_MOCK_UPDATE") == "1" {
		s.installMockUpdate()
		if retestGen > 0 {
			s.mu.Lock()
			if retestGen == s.retestGen {
				s.retestingTarget = false
			}
			s.mu.Unlock()
		}
		s.notify()
		if onDone != nil {
			onDone()
		}
		return
	}

	s.mu.Lock()
	if s.downloadRunning || s.installRunning {
		if retestGen > 0 && retestGen == s.retestGen {
			s.retestingTarget = false
		}
		s.mu.Unlock()
		return
	}
	if s.checkRunning && !manual {
		s.mu.Unlock()
		return
	}
	s.checkGen++
	gen := s.checkGen
	mSeq := s.mirrorSeq
	gSeq := s.githubSeq
	s.checkRunning = true
	s.lastPhase = "check"
	mode := s.sourceMode
	relCh := s.releaseChannel
	mirrorHint := s.stableProbeHintLocked(ChannelMirror)
	githubHint := s.stableProbeHintLocked(ChannelGitHub)
	wasReady := s.status == StatusReady && s.readyRecord != nil
	if !wasReady {
		s.status = StatusChecking
		s.errorMsg = ""
	}
	s.mu.Unlock()
	s.notify()

	go func() {
		defer func() {
			if onDone != nil {
				onDone()
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
		defer cancel()

		res, err := FetchReleasesRoutedForChannel(ctx, mode, relCh, mirrorHint, githubHint, func(ch string, cp ChannelProbe) {
			s.mu.Lock()
			if ch == ChannelMirror {
				if s.mirrorSeq != mSeq || s.mirrorProbing {
					s.mu.Unlock()
					return
				}
				s.setChannelProbeLocked(ChannelMirror, cp)
			} else if ch == ChannelGitHub {
				if s.githubSeq != gSeq || s.githubProbing {
					s.mu.Unlock()
					return
				}
				s.setChannelProbeLocked(ChannelGitHub, cp)
			}
			s.syncActiveChannelFromProbesLocked()
			s.mu.Unlock()
			s.notify()
		})

		var probeOtherChannel string
		s.mu.Lock()
		if gen != s.checkGen || relCh != s.releaseChannel {
			if retestGen > 0 && retestGen == s.retestGen {
				s.retestingTarget = false
			}
			s.mu.Unlock()
			return
		}
		s.checkRunning = false
		if retestGen > 0 && retestGen == s.retestGen {
			s.retestingTarget = false
		}
		if res.MirrorProbe != nil && s.mirrorSeq == mSeq && !s.mirrorProbing {
			s.setChannelProbeLocked(ChannelMirror, *res.MirrorProbe)
		}
		if res.GitHubProbe != nil && s.githubSeq == gSeq && !s.githubProbing {
			s.setChannelProbeLocked(ChannelGitHub, *res.GitHubProbe)
		}
		if s.downloadRunning || s.installRunning {
			s.mu.Unlock()
			return
		}

		if err != nil {
			s.lastErrorSanitized = SanitizeNetworkError(err)
			if s.lastErrorSanitized == "" {
				s.lastErrorSanitized = sanitizeDiagString(err.Error(), s.cfg.DataDir)
			}
			s.syncActiveChannelFromProbesLocked()
			// When a pinned source fails while the sibling channel has never been probed,
			// probe the sibling in the background so a total outage accurately enables feedback.
			switch mode {
			case SourceMirror:
				if s.githubProbe.Status == ProbeIdle && !s.githubProbing {
					probeOtherChannel = ChannelGitHub
				}
			case SourceGitHub:
				if s.mirrorProbe.Status == ProbeIdle && !s.mirrorProbing {
					probeOtherChannel = ChannelMirror
				}
			}
			if !wasReady {
				if len(s.releases) > 0 && !manual {
					s.status = StatusIdle
				} else {
					s.status = StatusError
					s.errorMsg = err.Error()
				}
			}
			s.mu.Unlock()
			s.notify()
			if probeOtherChannel != "" {
				s.startProbeChannelsAsync([]string{probeOtherChannel}, 0)
			}
			return
		}

		// Anti-rollback guard: do not let a stale mirror feed downgrade a higher already-known release in the same channel.
		fetchedLatest := ""
		if len(res.Entries) > 0 {
			fetchedLatest = res.Entries[0].Version
		}
		knownFloor := s.highestKnownForChannelLocked(relCh)
		if s.readyRecord != nil && (relCh == ReleaseNightly || s.readyRecord.EffectiveReleaseChannel() == ReleaseStable) &&
			(knownFloor == "" || CompareVersion(s.readyRecord.Version, knownFloor) > 0) {
			knownFloor = s.readyRecord.Version
		}
		isMirrorRollback := res.Channel == ChannelMirror && knownFloor != "" && fetchedLatest != "" &&
			CompareVersion(fetchedLatest, knownFloor) < 0
		if isMirrorRollback {
			logWarn("check.mirror_rollback_ignored", "channel", res.Channel, "latest", fetchedLatest, "known", knownFloor)
			s.activeChannel = res.Channel
			s.fetchLatencyMs = res.LatencyMs
			s.errorMsg = ""
			s.lastErrorSanitized = ""
			if s.status == StatusChecking || s.status == StatusError {
				if s.readyRecord != nil && CompareVersion(s.readyRecord.Version, CurrentVersion()) > 0 {
					s.status = StatusReady
				} else if s.latestEntry != nil && CompareVersion(s.latestEntry.Version, CurrentVersion()) > 0 {
					s.status = StatusAvailable
				} else {
					s.status = StatusUpToDate
				}
			}
			s.mu.Unlock()
			s.notify()
			return
		}

		s.setHighestKnownForChannelLocked(relCh, fetchedLatest)
		if relCh == ReleaseNightly {
			s.nightlyReleases = res.Entries
			s.absorbStableFromEntriesLocked(res.Entries)
		} else {
			s.stableReleases = res.Entries
		}
		if s.cfg.DataDir != "" && len(res.RawFeed) > 0 {
			_ = os.MkdirAll(updatesRoot(s.cfg.DataDir), 0o700)
			_ = os.WriteFile(feedCachePathForChannel(s.cfg.DataDir, relCh), res.RawFeed, 0o600)
		}

		cur := CurrentVersion()
		s.activeChannel = res.Channel
		s.fetchLatencyMs = res.LatencyMs
		s.reapplyActiveChannelReleasesLocked()

		logInfo("check.summary", "manual", manual, "current", cur, "latest", s.notes.ToVersion, "has_newer", s.notes.HasNewer, "aggregated_versions", s.notes.VersionCount, "channel", res.Channel, "latency_ms", res.LatencyMs)
		s.mu.Unlock()
		s.notify()
	}()
}

// StartDownload downloads and verifies the available update in the background.
func (s *Service) StartDownload() {
	if os.Getenv("BLOCK_MOCK_UPDATE") == "1" {
		s.runMockDownload()
		return
	}

	s.mu.Lock()
	if s.downloadRunning || s.installRunning || s.status == StatusReady {
		s.mu.Unlock()
		return
	}
	if s.latestEntry == nil || s.cfg.DataDir == "" {
		s.mu.Unlock()
		return
	}
	if s.targetAsset == nil {
		ver := s.latestEntry.Version
		s.status = StatusError
		s.lastPhase = "download"
		s.lastErrorSanitized = "missing compatible platform installer"
		if s.cfg.IsEn != nil && s.cfg.IsEn() {
			s.errorMsg = "No compatible installer found for this platform; open Releases to update manually"
		} else {
			s.errorMsg = "未找到适配当前系统架构的安装包，请前往下载页手动升级"
		}
		s.mu.Unlock()
		logError("download.missing_platform_asset", "version", ver, "os", runtime.GOOS, "arch", runtime.GOARCH)
		s.notify()
		return
	}
	entry := *s.latestEntry
	asset := *s.targetAsset
	inPlace := s.inPlace
	mode := s.sourceMode
	dataDir := s.cfg.DataDir
	mirrorHint := s.stableProbeHintLocked(ChannelMirror)
	githubHint := s.stableProbeHintLocked(ChannelGitHub)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	s.downloadGen++
	dlGen := s.downloadGen
	done := make(chan struct{})
	s.downloadDone = done
	s.downloadRunning = true
	s.downloadCancel = cancel
	s.status = StatusDownloading
	s.lastPhase = "download"
	s.downloadedBytes = 0
	s.totalBytes = asset.Size
	s.speedMBps = 0
	s.percent = 0
	s.preparing = false
	s.errorMsg = ""
	s.mu.Unlock()
	s.notify()

	go func() {
		defer cancel()
		defer close(done)

		rec, failedAttempts, err := downloadAndStageInternal(ctx, dataDir, entry, asset, inPlace, mode, mirrorHint, githubHint, func(dl, tot int64, speed float64, ch string, prep bool) {
			s.mu.Lock()
			if !s.downloadRunning || dlGen != s.downloadGen {
				s.mu.Unlock()
				return
			}
			s.downloadedBytes = dl
			if tot > 0 {
				s.totalBytes = tot
			}
			s.speedMBps = speed
			if prep {
				s.percent = 100
				s.preparing = true
				s.lastPhase = "prepare"
			} else if s.totalBytes > 0 {
				pct := int(dl * 100 / s.totalBytes)
				if pct > 99 {
					pct = 99
				}
				if pct < 0 {
					pct = 0
				}
				s.percent = pct
			}
			if ch != "" {
				s.activeChannel = ch
			}
			s.mu.Unlock()
			s.notify()
		}, func(commit func() error) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.downloadRunning || dlGen != s.downloadGen {
				return context.Canceled
			}
			return commit()
		})
		if err == nil && inPlace && rec != nil {
			// Build the swap stage now (status still shows preparing) so restart is instant later.
			_ = PrepareInstall(dataDir, rec, false)
		}

		var probeOtherChannel string
		s.mu.Lock()
		if dlGen != s.downloadGen {
			s.mu.Unlock()
			return
		}
		if !s.downloadRunning && errors.Is(err, context.Canceled) {
			s.mu.Unlock()
			return
		}
		s.downloadRunning = false
		s.downloadCancel = nil
		s.preparing = false
		s.speedMBps = 0
		if err != nil {
			if errors.Is(err, context.Canceled) {
				s.status = StatusAvailable
			} else {
				s.status = StatusError
				s.errorMsg = err.Error()
				s.lastErrorSanitized = SanitizeNetworkError(err)
				s.recordDownloadFailuresLocked(failedAttempts)
				s.syncActiveChannelFromProbesLocked()
				switch mode {
				case SourceMirror:
					if s.githubProbe.Status == ProbeIdle && !s.githubProbing {
						probeOtherChannel = ChannelGitHub
					}
				case SourceGitHub:
					if s.mirrorProbe.Status == ProbeIdle && !s.mirrorProbing {
						probeOtherChannel = ChannelMirror
					}
				}
			}
			s.mu.Unlock()
			s.notify()
			if probeOtherChannel != "" {
				s.startProbeChannelsAsync([]string{probeOtherChannel}, 0)
			}
			return
		}
		s.downloadPath = rec.ArchivePath
		s.activeChannel = rec.Channel
		s.percent = 100
		s.lastErrorSanitized = ""
		if rec.Size > 0 {
			s.downloadedBytes = rec.Size
			s.totalBytes = rec.Size
		}
		if inPlace {
			s.readyRecord = rec
			s.status = StatusReady
		} else {
			s.status = StatusAvailable
		}
		s.mu.Unlock()
		s.notify()
	}()
}

// InstallNow flushes workspace state and performs atomic in-place replacement & relaunch.
func (s *Service) InstallNow() {
	s.mu.Lock()
	if s.installRunning || s.status != StatusReady || s.readyRecord == nil {
		s.mu.Unlock()
		return
	}
	if !s.readyAllowedForChannelLocked(s.readyRecord) {
		// 用户已切回稳定版：不允许装切换前下载好的每夜版。
		s.dropReadyLocked()
		s.reapplyActiveChannelReleasesLocked()
		s.mu.Unlock()
		s.notify()
		return
	}
	rec := *s.readyRecord
	dataDir := s.cfg.DataDir
	s.installRunning = true
	s.status = StatusInstalling
	s.lastPhase = "install"
	s.errorMsg = ""
	s.mu.Unlock()
	s.notify()

	go func() {
		isEn := false
		if s.cfg.IsEn != nil {
			isEn = s.cfg.IsEn()
		}
		cur := CurrentVersion()
		err := InstallReady(dataDir, &rec, isEn, func() error {
			if s.cfg.FlushBeforeUpdate != nil {
				return s.cfg.FlushBeforeUpdate(cur)
			}
			return nil
		}, s.cfg.QuitApp)

		if err != nil {
			s.mu.Lock()
			s.installRunning = false
			if errors.Is(err, errStagedIntegrity) {
				_ = os.Remove(readyRecordPath(dataDir))
				s.readyRecord = nil
				s.status = StatusError
			} else if _, ok := os.Stat(rec.ArchivePath); ok == nil {
				s.status = StatusReady
			} else {
				_ = os.Remove(readyRecordPath(dataDir))
				s.readyRecord = nil
				s.status = StatusError
			}
			s.errorMsg = err.Error()
			s.lastErrorSanitized = sanitizeDiagString(err.Error(), dataDir)
			s.mu.Unlock()
			s.notify()
		}
	}()
}

func (s *Service) recordDownloadFailuresLocked(attempts []NodeProbe) {
	if len(attempts) == 0 {
		return
	}
	var mirrorFails, githubFails []NodeProbe
	for _, a := range attempts {
		if a.Channel == ChannelMirror {
			mirrorFails = append(mirrorFails, a)
		} else if a.Channel == ChannelGitHub {
			githubFails = append(githubFails, a)
		}
	}
	if len(mirrorFails) > 0 && !s.mirrorProbing {
		s.mirrorSeq++
		s.setChannelProbeLocked(ChannelMirror, summarizeChannelProbe(ChannelMirror, mirrorFails))
	}
	if len(githubFails) > 0 && !s.githubProbing {
		s.githubSeq++
		s.setChannelProbeLocked(ChannelGitHub, summarizeChannelProbe(ChannelGitHub, githubFails))
	}
}

func (s *Service) installMockProbesLocked() {
	now := time.Now()
	s.mirrorProbe = ChannelProbe{
		Channel:    ChannelMirror,
		Status:     ProbeOK,
		LatencyMs:  38,
		HTTPStatus: 200,
		BestHost:   "gh-proxy.com",
		ProbedAt:   now,
		Nodes: []NodeProbe{
			{Channel: ChannelMirror, Host: "gh-proxy.com", Kind: "feed", Status: ProbeOK, LatencyMs: 38, HTTPStatus: 200, ProbedAt: now},
			{Channel: ChannelMirror, Host: "ghproxy.net", Kind: "feed", Status: ProbeOK, LatencyMs: 64, HTTPStatus: 200, ProbedAt: now},
		},
	}
	s.githubProbe = ChannelProbe{
		Channel:    ChannelGitHub,
		Status:     ProbeOK,
		LatencyMs:  412,
		HTTPStatus: 200,
		BestHost:   "raw.githubusercontent.com",
		ProbedAt:   now,
		Nodes: []NodeProbe{
			{Channel: ChannelGitHub, Host: "raw.githubusercontent.com", Kind: "feed", Status: ProbeOK, LatencyMs: 412, HTTPStatus: 200, ProbedAt: now},
			{Channel: ChannelGitHub, Host: "api.github.com", Kind: "github-api", Status: ProbeOK, LatencyMs: 468, HTTPStatus: 200, ProbedAt: now},
		},
	}
}

func (s *Service) installMockUpdate() {
	cur := CurrentVersion()
	_, _, sv, ok := ParseReleaseTag(cur)
	if !ok {
		sv = Semver{Major: 1, Minor: 0, Patch: 0}
	}
	nextVer := fmt.Sprintf("%d.%d.%d", sv.Major, sv.Minor, sv.Patch+2)
	prevVer := fmt.Sprintf("%d.%d.%d", sv.Major, sv.Minor, sv.Patch+1)
	nextTag := "v" + nextVer
	prevTag := "v" + prevVer
	r1 := ReleaseEntry{
		Version:     nextVer,
		Tag:         nextTag,
		PublishedAt: "2026-11-18T12:00:00Z",
		HTMLURL:     PublicReleasesWebURL,
		Zh: LocalizedNotes{
			New:          []string{"支持跨版本中英双语更新日志自动汇总。", "支持国内加速镜像与 GitHub 原生源自动测速切换。"},
			Improvements: []string{"后台静默预下载并校验 SHA-256，重启前自动保存全部笔记。"},
			Fixes:        []string{"修复只读磁盘映像下更新无提示的问题。"},
		},
		En: LocalizedNotes{
			New:          []string{"Automatic cross-version bilingual changelog aggregation.", "Smart failover between acceleration mirrors and native GitHub."},
			Improvements: []string{"Silent background pre-download with SHA-256 verification and pre-restart note flush."},
			Fixes:        []string{"Clear prompt when running from a read-only disk image."},
		},
	}
	r2 := ReleaseEntry{
		Version:     prevVer,
		Tag:         prevTag,
		PublishedAt: "2026-11-16T09:30:00Z",
		HTMLURL:     PublicReleasesWebURL,
		Zh: LocalizedNotes{
			New:   []string{"应用商店新增瀑布流卡片与一键安装。"},
			Fixes: []string{"修复窄窗口下批注栏宽度计算偏差。"},
		},
		En: LocalizedNotes{
			New:   []string{"Masonry layout and one-click installation in App Store."},
			Fixes: []string{"Fixed annotation rail width calculation in narrow windows."},
		},
	}
	entries := []ReleaseEntry{r1, r2}
	asset := ReleaseAsset{
		Name:        "block-macos-arm64.dmg",
		DownloadURL: fmt.Sprintf("https://github.com/%s/releases/download/%s/block-macos-arm64.dmg", PublicRepo, nextTag),
		Size:        13_421_772,
	}
	s.mu.Lock()
	s.releases = entries
	s.stableReleases = entries
	s.nightlyReleases = entries
	s.notes = AggregateReleases(entries, cur, s.lastUpdatedFrom)
	s.latestEntry = &r1
	s.targetAsset = &asset
	s.inPlace = true
	s.installMockProbesLocked()
	s.syncActiveChannelFromProbesLocked()
	s.totalBytes = asset.Size
	s.status = StatusAvailable
	s.mu.Unlock()
}

func (s *Service) runMockDownload() {
	s.mu.Lock()
	if s.downloadRunning {
		s.mu.Unlock()
		return
	}
	s.downloadRunning = true
	s.status = StatusDownloading
	tot := int64(13_421_772)
	s.totalBytes = tot
	s.downloadedBytes = 0
	s.speedMBps = 4.2
	s.percent = 0
	s.mu.Unlock()
	s.notify()

	go func() {
		for step := 1; step <= 10; step++ {
			time.Sleep(80 * time.Millisecond)
			s.mu.Lock()
			if !s.downloadRunning {
				s.mu.Unlock()
				return
			}
			s.downloadedBytes = tot * int64(step) / 10
			s.percent = step * 10
			s.mu.Unlock()
			s.notify()
		}
		s.mu.Lock()
		s.downloadRunning = false
		s.speedMBps = 0
		s.status = StatusReady
		s.mu.Unlock()
		s.notify()
	}()
}
