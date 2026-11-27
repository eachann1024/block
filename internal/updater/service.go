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
	Status          Status
	CurrentVersion  string
	LatestVersion   string
	PublishedAt     string
	ReleaseURL      string
	SourceMode      SourceMode
	ActiveChannel   string // "mirror" | "github" | ""
	FetchLatencyMs  int64
	SpeedMBps       float64
	ViewMode        ViewMode
	DownloadedBytes int64
	TotalBytes      int64
	Percent         int
	Preparing       bool
	DownloadPath    string
	LogPath         string
	InPlace         bool
	PackageManaged  bool
	ErrorMessage    string
	Notes           AggregatedNotes
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
	DataDir             string
	InitialSourceMode   SourceMode
	LastUpdatedFrom     string
	IsEn                func() bool
	OnSourceModeChanged func(SourceMode)
	FlushBeforeUpdate   func(fromVersion string) error
	QuitApp             func(startInstaller func() error) error
	OnChange            func()
}

// Service coordinates silent background checks, multi-source downloads, staging, and installation.
type Service struct {
	cfg Config

	mu              sync.Mutex
	status          Status
	sourceMode      SourceMode
	activeChannel   string
	fetchLatencyMs  int64
	speedMBps       float64
	viewMode        ViewMode
	lastUpdatedFrom string
	releases        []ReleaseEntry
	notes           AggregatedNotes
	latestEntry     *ReleaseEntry
	targetAsset     *ReleaseAsset
	readyRecord     *VerifiedRecord
	inPlace         bool
	pkgManaged      bool
	downloadedBytes int64
	totalBytes      int64
	percent         int
	preparing       bool
	downloadPath    string
	errorMsg        string
	checkGen        uint64
	checkRunning    bool
	downloadGen     uint64
	downloadRunning bool
	downloadCancel  context.CancelFunc
	downloadDone    chan struct{}
	installRunning  bool
	autoStarted     bool
	stopCh          chan struct{}
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
		if entries, pErr := ParseFeedBytes(updates.BundledFeed); pErr == nil {
			s.releases = entries
			s.notes = AggregateReleases(entries, cur, s.lastUpdatedFrom)
		}
	}
	if cfg.DataDir != "" {
		if raw, err := os.ReadFile(feedCachePath(cfg.DataDir)); err == nil {
			if entries, pErr := ParseFeedBytes(raw); pErr == nil {
				s.releases = entries
				s.notes = AggregateReleases(entries, cur, s.lastUpdatedFrom)
			}
		}
	}
	if os.Getenv("BLOCK_MOCK_UPDATE") == "1" {
		s.installMockUpdate()
	}
	if lastErr := consumeLastUpdateError(); lastErr != "" {
		s.status = StatusError
		s.errorMsg = lastErr
		logError("update.previous_install_failed", "reason", lastErr)
	}
	return s
}

func (s *Service) notify() {
	if s.cfg.OnChange != nil {
		s.cfg.OnChange()
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
		Status:          s.status,
		CurrentVersion:  cur,
		LatestVersion:   latest,
		PublishedAt:     pubAt,
		ReleaseURL:      relURL,
		SourceMode:      s.sourceMode,
		ActiveChannel:   s.activeChannel,
		FetchLatencyMs:  s.fetchLatencyMs,
		SpeedMBps:       s.speedMBps,
		ViewMode:        s.viewMode,
		DownloadedBytes: s.downloadedBytes,
		TotalBytes:      s.totalBytes,
		Percent:         s.percent,
		Preparing:       s.preparing,
		DownloadPath:    s.downloadPath,
		LogPath:         LogFilePath(s.cfg.DataDir),
		InPlace:         s.inPlace,
		PackageManaged:  s.pkgManaged,
		ErrorMessage:    s.errorMsg,
		Notes:           s.notes,
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
	s.mu.Unlock()
	if changed {
		logInfo("config.source_mode_changed", "mode", norm)
		if s.cfg.OnSourceModeChanged != nil {
			s.cfg.OnSourceModeChanged(norm)
		}
		s.notify()
	}
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
			s.mu.Unlock()
			acknowledgeStartupAndClean(dataDir, cur, lastUpdatedFrom)
			if rec, ok := RestoreReady(dataDir, cur); ok {
				s.mu.Lock()
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
	if os.Getenv("BLOCK_MOCK_UPDATE") == "1" {
		s.installMockUpdate()
		s.notify()
		if onDone != nil {
			onDone()
		}
		return
	}

	s.mu.Lock()
	if s.downloadRunning || s.installRunning {
		s.mu.Unlock()
		return
	}
	if s.checkRunning && !manual {
		s.mu.Unlock()
		return
	}
	s.checkGen++
	gen := s.checkGen
	s.checkRunning = true
	mode := s.sourceMode
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

		entries, channel, latencyMs, rawFeed, err := FetchReleases(ctx, mode)

		s.mu.Lock()
		if gen != s.checkGen {
			s.mu.Unlock()
			return
		}
		s.checkRunning = false
		if s.downloadRunning || s.installRunning {
			s.mu.Unlock()
			return
		}

		if err != nil {
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
			return
		}

		if s.cfg.DataDir != "" && len(rawFeed) > 0 {
			_ = os.MkdirAll(updatesRoot(s.cfg.DataDir), 0o700)
			_ = os.WriteFile(feedCachePath(s.cfg.DataDir), rawFeed, 0o600)
		}

		cur := CurrentVersion()
		s.releases = entries
		s.activeChannel = channel
		s.fetchLatencyMs = latencyMs
		s.notes = AggregateReleases(entries, cur, s.lastUpdatedFrom)
		s.errorMsg = ""

		logInfo("check.summary", "manual", manual, "current", cur, "latest", s.notes.ToVersion, "has_newer", s.notes.HasNewer, "aggregated_versions", s.notes.VersionCount, "channel", channel, "latency_ms", latencyMs)

		if !s.notes.HasNewer || len(s.notes.Versions) == 0 {
			if s.readyRecord != nil && CompareVersion(s.readyRecord.Version, cur) > 0 {
				s.status = StatusReady
			} else {
				s.latestEntry = nil
				s.targetAsset = nil
				s.readyRecord = nil
				s.status = StatusUpToDate
			}
			s.mu.Unlock()
			s.notify()
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
			(asset == nil || CompareVersion(s.readyRecord.Version, picked.Version) > 0) {
			// Keep an already verified local package when the feed lags behind it or lacks a platform asset.
			s.status = StatusReady
		} else {
			s.readyRecord = nil
			s.status = StatusAvailable
			if asset != nil {
				s.totalBytes = asset.Size
			}
		}
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	s.downloadGen++
	dlGen := s.downloadGen
	done := make(chan struct{})
	s.downloadDone = done
	s.downloadRunning = true
	s.downloadCancel = cancel
	s.status = StatusDownloading
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

		rec, err := DownloadAndStage(ctx, dataDir, entry, asset, inPlace, mode, func(dl, tot int64, speed float64, ch string, prep bool) {
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
			}
			s.mu.Unlock()
			s.notify()
			return
		}
		s.downloadPath = rec.ArchivePath
		s.activeChannel = rec.Channel
		s.percent = 100
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
	rec := *s.readyRecord
	dataDir := s.cfg.DataDir
	s.installRunning = true
	s.status = StatusInstalling
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
			s.mu.Unlock()
			s.notify()
		}
	}()
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
	s.notes = AggregateReleases(entries, cur, s.lastUpdatedFrom)
	s.latestEntry = &r1
	s.targetAsset = &asset
	s.inPlace = true
	s.activeChannel = "mirror"
	s.fetchLatencyMs = 38
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
