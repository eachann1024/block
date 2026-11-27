package updater

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitForStatus(t *testing.T, svc *Service, want Status, timeout time.Duration) Snapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snap := svc.Snapshot()
		if snap.Status == want {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap := svc.Snapshot()
	t.Fatalf("timed out waiting for status %q; last status=%q err=%q", want, snap.Status, snap.ErrorMessage)
	return snap
}

func buildTestMacDMG(t *testing.T, version string) []byte {
	t.Helper()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "dmg-src")
	bundle := filepath.Join(src, "Block.app")
	macosDir := filepath.Join(bundle, "Contents", "MacOS")
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		t.Fatal(err)
	}

	exePath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exeBytes, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(macosDir, "block"), exeBytes, 0o755); err != nil {
		t.Fatal(err)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>block</string>
	<key>CFBundleIdentifier</key>
	<string>%s</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>%s</string>
</dict>
</plist>
`, macBundleID, version)
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", bundle).CombinedOutput(); err != nil {
		t.Fatalf("codesign fixture bundle: %v: %s", err, out)
	}
	dmgPath := filepath.Join(tmp, "update.dmg")
	if out, err := exec.Command("/usr/bin/hdiutil", "create", "-srcfolder", src, "-format", "UDZO", dmgPath).CombinedOutput(); err != nil {
		t.Fatalf("hdiutil create: %v: %s", err, out)
	}
	raw, err := os.ReadFile(dmgPath)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestServiceFullLifecycle(t *testing.T) {
	t.Setenv("BLOCK_MOCK_UPDATE", "")

	// On macOS, InstallReady requires running inside a .app bundle. Re-exec this
	// test binary inside a temporary Host.app bundle so InstallNow runs the real path.
	if runtime.GOOS == "darwin" && os.Getenv("BLOCK_TEST_SERVICE_IN_BUNDLE") != "1" {
		tmp := t.TempDir()
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		exeBytes, err := os.ReadFile(exe)
		if err != nil {
			t.Fatal(err)
		}
		bundleExe := filepath.Join(tmp, "Host.app", "Contents", "MacOS", "updater.test")
		if err := os.MkdirAll(filepath.Dir(bundleExe), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bundleExe, exeBytes, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bundleExe, "-test.run", "^TestServiceFullLifecycle$", "-test.v")
		cmd.Env = append(os.Environ(), "BLOCK_TEST_SERVICE_IN_BUNDLE=1", "BLOCK_MOCK_UPDATE=")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bundled TestServiceFullLifecycle failed: %v\n%s", err, out)
		}
		return
	}

	const nextVer, nextTag = "999.0.0", "v999.0.0"
	var payload []byte
	if runtime.GOOS == "darwin" {
		payload = buildTestMacDMG(t, nextVer)
	} else {
		if runtime.GOOS == "linux" {
			appImg := filepath.Join(t.TempDir(), "Block.AppImage")
			if err := os.WriteFile(appImg, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("APPIMAGE", appImg)
		}
		payload = []byte("block-test-installer-payload")
	}

	var candidates []ReleaseAsset
	for name := range allowlistedInstallers {
		candidates = append(candidates, ReleaseAsset{Name: name})
	}
	picked, inPlace, _ := PickPlatformAsset(candidates, runtime.GOOS, runtime.GOARCH)
	if !inPlace || picked == nil {
		t.Skip("no in-place installer for this platform")
	}
	asset := *picked
	asset.Size = int64(len(payload))
	asset.SHA256 = fmt.Sprintf("%x", sha256.Sum256(payload))
	asset.DownloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", PublicRepo, nextTag, asset.Name)

	feedDoc := FeedDocument{
		SchemaVersion: 1,
		Repo:          PublicRepo,
		LatestVersion: nextVer,
		LatestTag:     nextTag,
		Releases: []ReleaseEntry{{
			Version:     nextVer,
			Tag:         nextTag,
			PublishedAt: "2026-11-20T12:00:00Z",
			HTMLURL:     PublicReleasesWebURL,
			Zh:          LocalizedNotes{New: []string{"测试：全链路状态机更新"}},
			En:          LocalizedNotes{New: []string{"Test: Full updater lifecycle"}},
			Assets:      []ReleaseAsset{asset},
		}},
	}
	feedRaw, err := json.Marshal(feedDoc)
	if err != nil {
		t.Fatal(err)
	}

	origClient := sharedHTTPClient
	t.Cleanup(func() { sharedHTTPClient = origClient })
	sharedHTTPClient = &http.Client{Transport: metadataTransport(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "raw.githubusercontent.com":
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: int64(len(feedRaw)),
				Body:          io.NopCloser(bytes.NewReader(feedRaw)),
			}, nil
		case strings.HasSuffix(req.URL.Path, "/"+asset.Name):
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: int64(len(payload)),
				Body:          io.NopCloser(bytes.NewReader(payload)),
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("not found")),
			}, nil
		}
	})}

	var (
		mu          sync.Mutex
		seenStatus  []Status
		flushed     bool
		flushedFrom string
		quitCalled  bool
		quitDone    = make(chan struct{})
	)

	dataDir := t.TempDir()
	var svc *Service
	svc = NewService(Config{
		DataDir:           dataDir,
		InitialSourceMode: SourceAuto,
		FlushBeforeUpdate: func(fromVersion string) error {
			mu.Lock()
			flushed = true
			flushedFrom = fromVersion
			mu.Unlock()
			return nil
		},
		QuitApp: func(startInstaller func() error) error {
			mu.Lock()
			wasFlushed := flushed
			quitCalled = true
			mu.Unlock()
			if !wasFlushed {
				t.Errorf("expected FlushBeforeUpdate before QuitApp")
			}
			// Do not spawn the detached installer helper during tests.
			close(quitDone)
			return nil
		},
		OnChange: func() {
			if svc == nil {
				return
			}
			st := svc.Snapshot().Status
			mu.Lock()
			seenStatus = append(seenStatus, st)
			mu.Unlock()
		},
	})

	if got := svc.Snapshot().Status; got != StatusIdle {
		t.Fatalf("expected initial StatusIdle, got %q", got)
	}

	// Idle -> Checking -> Available
	svc.CheckAsync(true)
	availSnap := waitForStatus(t, svc, StatusAvailable, 5*time.Second)
	if availSnap.LatestVersion != nextVer || availSnap.TotalBytes != int64(len(payload)) {
		t.Fatalf("unexpected Available snapshot: %+v", availSnap)
	}

	// Available -> StartDownload (real disk write + SHA-256 verification) -> Ready
	svc.StartDownload()
	readySnap := waitForStatus(t, svc, StatusReady, 10*time.Second)
	if readySnap.DownloadPath == "" {
		t.Fatal("expected DownloadPath populated in Ready")
	}
	if err := VerifyFileSHA256(readySnap.DownloadPath, asset.SHA256); err != nil {
		t.Fatalf("staged file SHA-256 verification failed: %v", err)
	}
	if _, err := os.Stat(readyRecordPath(dataDir)); err != nil {
		t.Fatalf("expected ready.json persisted: %v", err)
	}

	// Ready -> InstallNow -> FlushBeforeUpdate + QuitApp
	svc.InstallNow()
	select {
	case <-quitDone:
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for QuitApp; status=%q err=%q", svc.Snapshot().Status, svc.Snapshot().ErrorMessage)
	}

	mu.Lock()
	gotFlushed := flushed
	gotFrom := flushedFrom
	gotQuit := quitCalled
	statuses := append([]Status(nil), seenStatus...)
	mu.Unlock()

	if !gotFlushed || gotFrom != CurrentVersion() {
		t.Fatalf("expected FlushBeforeUpdate(%q), got flushed=%v from=%q", CurrentVersion(), gotFlushed, gotFrom)
	}
	if !gotQuit {
		t.Fatal("expected QuitApp invoked")
	}
	for _, want := range []Status{StatusChecking, StatusAvailable, StatusDownloading, StatusReady, StatusInstalling} {
		found := false
		for _, s := range statuses {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing expected transition %q in observed sequence %v", want, statuses)
		}
	}
}

type blockingStream struct {
	firstChunk []byte
	sentFirst  bool
	started    chan struct{}
	once       sync.Once
	ctx        interface{ Done() <-chan struct{} }
	errFn      func() error
}

func (b *blockingStream) Read(p []byte) (int, error) {
	if !b.sentFirst {
		b.sentFirst = true
		n := copy(p, b.firstChunk)
		b.once.Do(func() { close(b.started) })
		return n, nil
	}
	<-b.ctx.Done()
	return 0, b.errFn()
}

func (b *blockingStream) Close() error { return nil }

func TestServiceCancelDownloadAndChecksumMismatch(t *testing.T) {
	t.Setenv("BLOCK_MOCK_UPDATE", "")
	if runtime.GOOS == "linux" {
		t.Setenv("APPIMAGE", "/fixture/Block.AppImage")
	}

	var candidates []ReleaseAsset
	for name := range allowlistedInstallers {
		candidates = append(candidates, ReleaseAsset{Name: name})
	}
	picked, inPlace, _ := PickPlatformAsset(candidates, runtime.GOOS, runtime.GOARCH)
	if !inPlace || picked == nil {
		t.Skip("no in-place installer for this platform")
	}

	const nextVer, nextTag = "999.0.0", "v999.0.0"
	expectedPayload := []byte("valid-installer-bytes")
	corruptedPayload := []byte("wrong-installer-bytes")
	if len(expectedPayload) != len(corruptedPayload) {
		t.Fatal("fixture payloads must match length")
	}

	// 1. CancelDownload falls back to StatusAvailable without persisting ready.json.
	t.Run("CancelDownload", func(t *testing.T) {
		asset := *picked
		asset.Size = 1024 * 1024
		asset.SHA256 = fmt.Sprintf("%x", sha256.Sum256(expectedPayload))
		asset.DownloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", PublicRepo, nextTag, asset.Name)

		feedRaw, err := json.Marshal(FeedDocument{
			SchemaVersion: 1,
			Repo:          PublicRepo,
			LatestVersion: nextVer,
			LatestTag:     nextTag,
			Releases: []ReleaseEntry{{
				Version: nextVer,
				Tag:     nextTag,
				Assets:  []ReleaseAsset{asset},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}

		streamStarted := make(chan struct{})
		origClient := sharedHTTPClient
		t.Cleanup(func() { sharedHTTPClient = origClient })
		sharedHTTPClient = &http.Client{Transport: metadataTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "raw.githubusercontent.com" {
				return &http.Response{
					StatusCode:    http.StatusOK,
					ContentLength: int64(len(feedRaw)),
					Body:          io.NopCloser(bytes.NewReader(feedRaw)),
				}, nil
			}
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: asset.Size,
				Body: &blockingStream{
					firstChunk: []byte("partial-chunk"),
					started:    streamStarted,
					ctx:        req.Context(),
					errFn:      req.Context().Err,
				},
			}, nil
		})}

		dataDir := t.TempDir()
		svc := NewService(Config{DataDir: dataDir})
		svc.CheckAsync(true)
		waitForStatus(t, svc, StatusAvailable, 5*time.Second)

		svc.StartDownload()
		select {
		case <-streamStarted:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for download stream to start")
		}
		if got := svc.Snapshot().Status; got != StatusDownloading {
			t.Fatalf("expected StatusDownloading before cancel, got %q", got)
		}

		svc.CancelDownload()
		snap := svc.Snapshot()
		if snap.Status != StatusAvailable || snap.Percent != 0 || snap.DownloadedBytes != 0 {
			t.Fatalf("expected fallback to StatusAvailable with reset progress, got %+v", snap)
		}
		if _, err := os.Stat(readyRecordPath(dataDir)); !os.IsNotExist(err) {
			t.Fatalf("expected no ready.json after CancelDownload, stat err=%v", err)
		}
	})

	// 2. Checksum mismatch across all sources transitions to StatusError.
	t.Run("ChecksumMismatch", func(t *testing.T) {
		asset := *picked
		asset.Size = int64(len(expectedPayload))
		asset.SHA256 = fmt.Sprintf("%x", sha256.Sum256(expectedPayload))
		asset.DownloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", PublicRepo, nextTag, asset.Name)

		feedRaw, err := json.Marshal(FeedDocument{
			SchemaVersion: 1,
			Repo:          PublicRepo,
			LatestVersion: nextVer,
			LatestTag:     nextTag,
			Releases: []ReleaseEntry{{
				Version: nextVer,
				Tag:     nextTag,
				Assets:  []ReleaseAsset{asset},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}

		origClient := sharedHTTPClient
		t.Cleanup(func() { sharedHTTPClient = origClient })
		sharedHTTPClient = &http.Client{Transport: metadataTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "raw.githubusercontent.com" {
				return &http.Response{
					StatusCode:    http.StatusOK,
					ContentLength: int64(len(feedRaw)),
					Body:          io.NopCloser(bytes.NewReader(feedRaw)),
				}, nil
			}
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: int64(len(corruptedPayload)),
				Body:          io.NopCloser(bytes.NewReader(corruptedPayload)),
			}, nil
		})}

		dataDir := t.TempDir()
		svc := NewService(Config{DataDir: dataDir})
		svc.CheckAsync(true)
		waitForStatus(t, svc, StatusAvailable, 5*time.Second)

		svc.StartDownload()
		errSnap := waitForStatus(t, svc, StatusError, 5*time.Second)
		if !strings.Contains(errSnap.ErrorMessage, "checksum mismatch") {
			t.Fatalf("expected checksum mismatch error message, got %q", errSnap.ErrorMessage)
		}
		if _, err := os.Stat(readyRecordPath(dataDir)); !os.IsNotExist(err) {
			t.Fatalf("expected no ready.json on checksum mismatch, stat err=%v", err)
		}
	})
}
