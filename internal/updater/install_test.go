package updater

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type metadataTransport func(*http.Request) (*http.Response, error)

func (transport metadataTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return transport(req)
}

func TestReadyUpdateUsesOfficialDigest(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Setenv("APPIMAGE", "/fixture/Block.AppImage")
	}
	var assets []ReleaseAsset
	for name := range allowlistedInstallers {
		assets = append(assets, ReleaseAsset{Name: name})
	}
	asset, inPlace, _ := PickPlatformAsset(assets, runtime.GOOS, runtime.GOARCH)
	if !inPlace || asset == nil {
		t.Skip("no in-place installer for this platform")
	}
	const version, tag = "100.0.0", "v100.0.0"
	contents := []byte("update verification fixture")
	asset.SHA256 = fmt.Sprintf("%x", sha256.Sum256(contents))
	asset.DownloadURL = PublicReleasesWebURL + "/download/" + tag + "/" + asset.Name
	dir := t.TempDir()
	archive := filepath.Join(updatesRoot(dir), version, asset.Name)
	if err := os.MkdirAll(filepath.Dir(archive), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	feedAsset := *asset
	calls := 0
	original := sharedHTTPClient
	t.Cleanup(func() { sharedHTTPClient = original })
	sharedHTTPClient = &http.Client{Transport: metadataTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Scheme != "https" || req.URL.Host != "raw.githubusercontent.com" {
			t.Fatalf("metadata requested from %s", req.URL)
		}
		doc := FeedDocument{Releases: []ReleaseEntry{{Version: version, Tag: tag, Assets: []ReleaseAsset{feedAsset}}}}
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}

	record := VerifiedRecord{
		Version: version, Tag: tag, AssetName: asset.Name, DownloadURL: asset.DownloadURL,
		ArchivePath: archive, SHA256: asset.SHA256, InPlace: true,
	}
	if err := validateReadyFromOfficialSource(dir, "1.0.0", &record); err != nil {
		t.Fatal(err)
	}
	feedAsset.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("new official installer")))
	if err := validateReadyFromOfficialSource(dir, "1.0.0", &record); err == nil {
		t.Fatal("a staged installer with a superseded digest was accepted")
	}
	record.ArchivePath = filepath.Join(dir, asset.Name)
	if err := validateReadyFromOfficialSource(dir, "1.0.0", &record); err == nil || calls != 2 {
		t.Fatalf("cache path validation did not reject before checking metadata: calls=%d, err=%v", calls, err)
	}
}

func TestCanceledQuitKeepsStagedUpdate(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(updatesRoot(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readyRecordPath(dir), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, "fixture-installer"))
	canceled := errors.New("quit canceled")
	err := startInstallerOnQuit(dir, cmd, func(start func() error) error { return canceled })
	if !errors.Is(err, canceled) || cmd.Process != nil {
		t.Fatalf("installer was not deferred after cancellation: %v", err)
	}
	if _, err := os.Stat(readyRecordPath(dir)); err != nil {
		t.Fatalf("staged update cannot be retried: %v", err)
	}
}

// Set BLOCK_TEST_UPDATE_BUNDLE to a freshly built Block.app to validate a local
// DMG through the production staging code without installing or launching it.
func TestMacInstallerBundleFixture(t *testing.T) {
	fixture := os.Getenv("BLOCK_TEST_UPDATE_BUNDLE")
	if runtime.GOOS != "darwin" || fixture == "" {
		t.Skip("no local macOS bundle fixture")
	}
	versionBytes, err := exec.Command("/usr/bin/plutil", "-extract", "CFBundleShortVersionString", "raw", "-o", "-",
		filepath.Join(fixture, "Contents", "Info.plist")).Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))

	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/ditto", fixture, filepath.Join(source, "Block.app")).CombinedOutput(); err != nil {
		t.Fatalf("copy fixture: %v: %s", err, out)
	}
	dmg := filepath.Join(dir, "fixture.dmg")
	if out, err := exec.Command("/usr/bin/hdiutil", "create", "-srcfolder", source, "-format", "UDZO", dmg).CombinedOutput(); err != nil {
		t.Fatalf("create fixture DMG: %v: %s", err, out)
	}
	unpacked, err := prepareMacAppFromDMG(dmg, filepath.Join(dir, "unpacked"), version)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateMacBundle(unpacked, "0.0.0"); err == nil {
		t.Fatal("a bundle with the wrong version was accepted")
	}
}
