package updater

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	macBundleID      = "dev.eachann.block"
	macLegacyID      = "com.block.notes"
	stageDirPrefix   = ".block-update-"
	lsregisterBinary = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
)

var stageDirPattern = regexp.MustCompile(`^\.block-update-[0-9a-fA-F]+$`)

// VerifiedRecord persists a verified, staged update in <dataDir>/app-updates/ready.json.
type VerifiedRecord struct {
	Version         string `json:"version"`
	Tag             string `json:"tag"`
	AssetName       string `json:"asset_name"`
	DownloadURL     string `json:"download_url"`
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	ArchivePath     string `json:"archive_path"`
	UnpackedAppPath string `json:"unpacked_app_path,omitempty"`
	PublishedAt     string `json:"published_at,omitempty"`
	ReleaseURL      string `json:"release_url,omitempty"`
	Channel         string `json:"channel,omitempty"`
	InPlace         bool   `json:"in_place"`
}

type installStageRecord struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

func updatesRoot(dataDir string) string {
	return filepath.Join(dataDir, "app-updates")
}

func readyRecordPath(dataDir string) string {
	return filepath.Join(updatesRoot(dataDir), "ready.json")
}

func stageRecordPath(dataDir string) string {
	return filepath.Join(updatesRoot(dataDir), "install-stage.json")
}

func feedCachePath(dataDir string) string {
	return filepath.Join(updatesRoot(dataDir), "feed-cache.json")
}

// PickPlatformAsset chooses the allowlisted installer for the running OS/arch and reports
// whether it supports automatic in-place restart replacement vs opening an external installer.
func PickPlatformAsset(assets []ReleaseAsset, goos, goarch string) (picked *ReleaseAsset, inPlace bool, pkgManaged bool) {
	findByName := func(name string) *ReleaseAsset {
		for i := range assets {
			if assets[i].Name == name {
				cp := assets[i]
				return &cp
			}
		}
		return nil
	}

	switch goos {
	case "darwin":
		switch goarch {
		case "arm64":
			return findByName("block-macos-arm64.dmg"), true, false
		case "amd64":
			return findByName("block-macos-x64.dmg"), true, false
		}
	case "windows":
		if goarch == "amd64" {
			return findByName("block-windows-x64-setup.exe"), true, false
		}
	case "linux":
		if goarch == "amd64" {
			if os.Getenv("APPIMAGE") != "" {
				return findByName("block-linux-x64.AppImage"), true, false
			}
			if isLinuxSystemPackageManaged() {
				return nil, false, true
			}
			return findByName("block-linux-x64.deb"), false, false
		}
	}
	return nil, false, true
}

func isLinuxSystemPackageManaged() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return false
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if out, err := exec.Command("pacman", "-Qo", exe).Output(); err == nil && len(out) > 0 {
		return true
	}
	if out, err := exec.Command("rpm", "-qf", exe).Output(); err == nil {
		s := strings.ToLower(strings.TrimSpace(string(out)))
		if s != "" && !strings.Contains(s, "not owned") {
			return true
		}
	}
	return false
}

// VerifyFileSHA256 checks that path exists and matches expectedHex (64 hex chars).
func VerifyFileSHA256(path, expectedHex string) error {
	want := strings.ToLower(strings.TrimSpace(expectedHex))
	if len(want) != 64 {
		return errors.New("missing update SHA-256 checksum")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("SHA-256 mismatch: expected %s, got %s", want[:12], got[:12])
	}
	return nil
}

// DownloadProgressCallback reports live download progress and active channel ("mirror" | "github").
type DownloadProgressCallback func(downloaded, total int64, channel string, preparing bool)

// DownloadAndStage downloads the verified asset across candidate sources, verifies SHA-256,
// unpacks & validates the macOS .app bundle when applicable, and persists ready.json.
func DownloadAndStage(
	ctx context.Context,
	dataDir string,
	entry ReleaseEntry,
	asset ReleaseAsset,
	inPlace bool,
	mode SourceMode,
	onProgress DownloadProgressCallback,
) (*VerifiedRecord, error) {
	expectedSHA := asset.EffectiveSHA256()
	if expectedSHA == "" {
		return nil, errors.New("release asset is missing SHA-256 digest")
	}
	cands := CandidateAssetURLs(asset.DownloadURL, mode)
	if len(cands) == 0 {
		return nil, errors.New("untrusted release download URL")
	}

	verDir := filepath.Join(updatesRoot(dataDir), entry.Version)
	if err := os.MkdirAll(verDir, 0o700); err != nil {
		return nil, err
	}
	finalPath := filepath.Join(verDir, asset.Name)
	partPath := finalPath + ".part"

	var usedChannel string
	var lastErr error
	for _, cand := range cands {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		_ = os.Remove(partPath)
		err := downloadOneSource(ctx, cand, partPath, asset.Size, expectedSHA, onProgress)
		if err == nil {
			usedChannel = cand.Channel
			lastErr = nil
			break
		}
		lastErr = err
	}
	if lastErr != nil {
		_ = os.Remove(partPath)
		return nil, lastErr
	}

	if err := os.Rename(partPath, finalPath); err != nil {
		_ = os.Remove(partPath)
		return nil, err
	}

	unpackedApp := ""
	if runtime.GOOS == "darwin" && inPlace && strings.HasSuffix(strings.ToLower(asset.Name), ".dmg") {
		if onProgress != nil {
			onProgress(asset.Size, asset.Size, usedChannel, true)
		}
		unpackedDir := filepath.Join(verDir, "unpacked")
		appPath, err := prepareMacAppFromDMG(finalPath, unpackedDir, entry.Version)
		if err != nil {
			_ = os.RemoveAll(unpackedDir)
			return nil, err
		}
		unpackedApp = appPath
	}

	rec := &VerifiedRecord{
		Version:         entry.Version,
		Tag:             entry.Tag,
		AssetName:       asset.Name,
		DownloadURL:     asset.DownloadURL,
		SHA256:          expectedSHA,
		Size:            asset.Size,
		ArchivePath:     finalPath,
		UnpackedAppPath: unpackedApp,
		PublishedAt:     entry.PublishedAt,
		ReleaseURL:      entry.HTMLURL,
		Channel:         usedChannel,
		InPlace:         inPlace,
	}
	if inPlace {
		if raw, err := json.MarshalIndent(rec, "", "  "); err == nil {
			_ = os.WriteFile(readyRecordPath(dataDir), raw, 0o600)
		}
	}
	return rec, nil
}

func downloadOneSource(
	ctx context.Context,
	cand LabeledURL,
	destPath string,
	expectedSize int64,
	expectedSHA string,
	onProgress DownloadProgressCallback,
) error {
	body, contentLen, err := openDownloadStream(ctx, cand.URL)
	if err != nil {
		return err
	}
	defer body.Close()

	total := expectedSize
	if total <= 0 && contentLen > 0 {
		total = contentLen
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	hasher := sha256.New()
	buf := make([]byte, 64*1024)
	var written int64
	var lastTick time.Time

	if onProgress != nil {
		onProgress(0, total, cand.Channel, false)
	}

	for {
		n, rErr := body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, wErr := f.Write(chunk); wErr != nil {
				return wErr
			}
			hasher.Write(chunk)
			written += int64(n)
			if total > 0 && written > total+1024*1024 {
				return errors.New("downloaded size exceeded manifest size")
			}
			if onProgress != nil && time.Since(lastTick) >= 80*time.Millisecond {
				lastTick = time.Now()
				onProgress(written, total, cand.Channel, false)
			}
		}
		if rErr == io.EOF {
			break
		}
		if rErr != nil {
			return rErr
		}
	}

	if err := f.Sync(); err != nil {
		return err
	}
	if expectedSize > 0 && written != expectedSize {
		return fmt.Errorf("incomplete download (%d / %d bytes)", written, expectedSize)
	}
	gotHex := hex.EncodeToString(hasher.Sum(nil))
	if gotHex != strings.ToLower(expectedSHA) {
		return fmt.Errorf("checksum mismatch from %s", cand.Channel)
	}
	return nil
}

func prepareMacAppFromDMG(dmgPath, unpackedDir, expectedVersion string) (string, error) {
	_ = os.RemoveAll(unpackedDir)
	if err := os.MkdirAll(unpackedDir, 0o700); err != nil {
		return "", err
	}
	mountDir, err := os.MkdirTemp("", "block-dmg-mnt-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(mountDir)

	attach := exec.Command("/usr/bin/hdiutil", "attach", "-nobrowse", "-readonly", "-noverify", "-mountpoint", mountDir, dmgPath)
	if out, err := attach.CombinedOutput(); err != nil {
		return "", fmt.Errorf("mount DMG failed: %s", strings.TrimSpace(string(out)))
	}
	detached := false
	detach := func() {
		if detached {
			return
		}
		detached = true
		_ = exec.Command("/usr/bin/hdiutil", "detach", mountDir, "-force").Run()
	}
	defer detach()

	srcApp := filepath.Join(mountDir, "Block.app")
	info, err := os.Lstat(srcApp)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("DMG does not contain Block.app")
	}

	dstApp := filepath.Join(unpackedDir, "Block.app")
	if out, err := exec.Command("/usr/bin/ditto", srcApp, dstApp).CombinedOutput(); err != nil {
		return "", fmt.Errorf("unpack Block.app failed: %s", strings.TrimSpace(string(out)))
	}
	detach()

	if err := validateMacBundle(dstApp, expectedVersion); err != nil {
		return "", err
	}
	return dstApp, nil
}

func validateMacBundle(bundlePath, expectedVersion string) error {
	st, err := os.Lstat(bundlePath)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid macOS application bundle")
	}
	plistPath := filepath.Join(bundlePath, "Contents", "Info.plist")
	extractKey := func(k string) (string, error) {
		out, err := exec.Command("/usr/bin/plutil", "-extract", k, "raw", "-o", "-", plistPath).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	id, err := extractKey("CFBundleIdentifier")
	if err != nil || (id != macBundleID && id != macLegacyID) {
		return fmt.Errorf("bundle identifier mismatch (%s)", id)
	}
	if expectedVersion != "" {
		ver, err := extractKey("CFBundleShortVersionString")
		if err == nil && ver != "" && CompareVersion(ver, expectedVersion) != 0 {
			return fmt.Errorf("bundle version mismatch (%s != %s)", ver, expectedVersion)
		}
	}
	exePath := filepath.Join(bundlePath, "Contents", "MacOS", "block")
	lipoOut, err := exec.Command("/usr/bin/lipo", "-archs", exePath).Output()
	if err != nil {
		return errors.New("unable to inspect update binary architecture")
	}
	wantArch := "x86_64"
	if runtime.GOARCH == "arm64" {
		wantArch = "arm64"
	}
	if !slices.Contains(strings.Fields(string(lipoOut)), wantArch) {
		return fmt.Errorf("update architecture mismatch (expected %s)", wantArch)
	}
	if err := exec.Command("/usr/bin/codesign", "--verify", "--verbose=2", bundlePath).Run(); err != nil {
		return errors.New("update bundle signature verification failed")
	}
	return nil
}

func installedMacBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(bundle, ".app") {
		return "", errors.New("current process is not running inside a macOS .app bundle")
	}
	return bundle, nil
}

func livesInUpdateStage(bundle string) bool {
	for _, part := range strings.Split(filepath.Clean(bundle), string(filepath.Separator)) {
		if stageDirPattern.MatchString(part) {
			return true
		}
	}
	return false
}

func removeSiblingMacStages(bundle string) {
	if runtime.GOOS != "darwin" || !strings.HasSuffix(bundle, ".app") || livesInUpdateStage(bundle) {
		return
	}
	parent := filepath.Dir(bundle)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, ent := range entries {
		if !ent.IsDir() || !stageDirPattern.MatchString(ent.Name()) {
			continue
		}
		stage := filepath.Join(parent, ent.Name())
		if sub, err := os.ReadDir(stage); err == nil {
			for _, s := range sub {
				if strings.HasSuffix(s.Name(), ".app") {
					_ = exec.Command(lsregisterBinary, "-u", filepath.Join(stage, s.Name())).Run()
				}
			}
		}
		_ = os.RemoveAll(stage)
	}
}

// AcknowledgeStartupAndClean signals any pending macOS install stage that the new version
// launched cleanly, cleans sibling stage directories, and prunes outdated cached downloads.
func AcknowledgeStartupAndClean(dataDir, runningVersion string) {
	root := updatesRoot(dataDir)
	_ = os.MkdirAll(root, 0o700)

	if runtime.GOOS == "darwin" {
		if bundle, err := installedMacBundle(); err == nil && !livesInUpdateStage(bundle) {
			if raw, rErr := os.ReadFile(stageRecordPath(dataDir)); rErr == nil {
				var st installStageRecord
				if json.Unmarshal(raw, &st) == nil && st.Path != "" && stageDirPattern.MatchString(filepath.Base(st.Path)) {
					if st.Version == "" || CompareVersion(runningVersion, st.Version) >= 0 {
						_ = os.WriteFile(filepath.Join(st.Path, "started"), []byte(fmt.Sprintf("%d", os.Getpid())), 0o600)
						removeSiblingMacStages(bundle)
						_ = os.Remove(stageRecordPath(dataDir))
					}
				}
			}
		}
	}

	var keepVer string
	if raw, err := os.ReadFile(readyRecordPath(dataDir)); err == nil {
		var rec VerifiedRecord
		if json.Unmarshal(raw, &rec) == nil && CompareVersion(rec.Version, runningVersion) > 0 {
			keepVer = rec.Version
		} else {
			_ = os.Remove(readyRecordPath(dataDir))
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		name := ent.Name()
		if name == keepVer {
			continue
		}
		if _, _, _, ok := ParseReleaseTag(name); ok {
			_ = os.RemoveAll(filepath.Join(root, name))
		}
	}
}

// RestoreReady validates a persisted ready.json across restarts without re-downloading.
func RestoreReady(dataDir, runningVersion string) (*VerifiedRecord, bool) {
	raw, err := os.ReadFile(readyRecordPath(dataDir))
	if err != nil {
		return nil, false
	}
	var rec VerifiedRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		_ = os.Remove(readyRecordPath(dataDir))
		return nil, false
	}
	if CompareVersion(rec.Version, runningVersion) <= 0 {
		_ = os.Remove(readyRecordPath(dataDir))
		return nil, false
	}
	if err := VerifyFileSHA256(rec.ArchivePath, rec.SHA256); err != nil {
		_ = os.Remove(readyRecordPath(dataDir))
		return nil, false
	}
	if runtime.GOOS == "darwin" && rec.InPlace {
		if rec.UnpackedAppPath == "" {
			unpackedDir := filepath.Join(filepath.Dir(rec.ArchivePath), "unpacked")
			appPath, err := prepareMacAppFromDMG(rec.ArchivePath, unpackedDir, rec.Version)
			if err != nil {
				_ = os.Remove(readyRecordPath(dataDir))
				return nil, false
			}
			rec.UnpackedAppPath = appPath
		} else if err := validateMacBundle(rec.UnpackedAppPath, rec.Version); err != nil {
			_ = os.Remove(readyRecordPath(dataDir))
			return nil, false
		}
	}
	return &rec, true
}

const macSwapInstallerScript = `#!/bin/sh
set -eu
target="$1"
stage="$2"
parent_pid="$3"
count=0
while kill -0 "$parent_pid" 2>/dev/null; do
  count=$((count + 1))
  if [ "$count" -gt 240 ]; then exit 1; fi
  sleep 0.25
done
remove_stage() {
  rm -rf "$stage"
}
rollback() {
  if [ -d "$stage/previous.app" ]; then
    if [ -e "$target" ]; then mv "$target" "$stage/failed.app"; fi
    mv "$stage/previous.app" "$target"
  fi
  /usr/bin/open "$target" || true
  remove_stage
}
trap rollback HUP INT TERM
if ! mv "$target" "$stage/previous.app"; then /usr/bin/open "$target" || true; remove_stage; exit 1; fi
if ! mv "$stage/next.app" "$target"; then rollback; exit 1; fi
if ! /usr/bin/open "$target"; then rollback; exit 1; fi
count=0
while [ ! -f "$stage/started" ]; do
  count=$((count + 1))
  if [ "$count" -gt 60 ]; then
    if ! /usr/bin/pgrep -f "$target/Contents/MacOS/block" >/dev/null 2>&1; then rollback; exit 1; fi
    remove_stage
    exit 0
  fi
  sleep 1
done
remove_stage
`

const linuxAppImageInstallerScript = `#!/bin/sh
set -eu
target="$1"
next_bin="$2"
parent_pid="$3"
count=0
while kill -0 "$parent_pid" 2>/dev/null; do
  count=$((count + 1))
  if [ "$count" -gt 240 ]; then exit 1; fi
  sleep 0.25
done
mv "$next_bin" "$target"
chmod 755 "$target"
exec "$target"
`

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// InstallReady verifies the staged package, flushes workspace data via flushBeforeQuit,
// spawns the platform atomic replacement helper, and invokes quitApp.
func InstallReady(dataDir string, rec *VerifiedRecord, isEn bool, flushBeforeQuit func(), quitApp func()) error {
	if rec == nil {
		return errors.New("no staged update ready to install")
	}
	if err := VerifyFileSHA256(rec.ArchivePath, rec.SHA256); err != nil {
		_ = os.Remove(readyRecordPath(dataDir))
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		bundle, err := installedMacBundle()
		if err != nil {
			if isEn {
				return errors.New("Updates can only be installed from a packaged Block.app")
			}
			return errors.New("仅打包后的 Block.app 支持原位重启更新")
		}
		if strings.Contains(bundle, "/AppTranslocation/") || strings.HasPrefix(bundle, "/Volumes/") {
			if isEn {
				return errors.New("Move Block.app to /Applications before installing updates")
			}
			return errors.New("请先将 Block 移入「应用程序」文件夹后再执行更新")
		}
		parent := filepath.Dir(bundle)
		probe, err := os.CreateTemp(parent, ".block-writecheck-*")
		if err != nil {
			if isEn {
				return fmt.Errorf("No write permission in %s", parent)
			}
			return fmt.Errorf("应用所在目录无写入权限：%s", parent)
		}
		probeName := probe.Name()
		_ = probe.Close()
		_ = os.Remove(probeName)

		removeSiblingMacStages(bundle)
		if rec.UnpackedAppPath == "" {
			unpackedDir := filepath.Join(filepath.Dir(rec.ArchivePath), "unpacked")
			appPath, prepErr := prepareMacAppFromDMG(rec.ArchivePath, unpackedDir, rec.Version)
			if prepErr != nil {
				return prepErr
			}
			rec.UnpackedAppPath = appPath
		}
		if err := validateMacBundle(rec.UnpackedAppPath, rec.Version); err != nil {
			return err
		}

		stage := filepath.Join(parent, stageDirPrefix+randomHex(4))
		if err := os.MkdirAll(stage, 0o700); err != nil {
			return err
		}
		nextApp := filepath.Join(stage, "next.app")
		if out, err := exec.Command("/usr/bin/ditto", rec.UnpackedAppPath, nextApp).CombinedOutput(); err != nil {
			_ = os.RemoveAll(stage)
			return fmt.Errorf("stage update failed: %s", strings.TrimSpace(string(out)))
		}
		if err := validateMacBundle(nextApp, rec.Version); err != nil {
			_ = os.RemoveAll(stage)
			return err
		}
		stRaw, _ := json.Marshal(installStageRecord{Path: stage, Version: rec.Version})
		_ = os.WriteFile(stageRecordPath(dataDir), stRaw, 0o600)

		scriptPath := filepath.Join(stage, "install.sh")
		if err := os.WriteFile(scriptPath, []byte(macSwapInstallerScript), 0o700); err != nil {
			_ = os.RemoveAll(stage)
			return err
		}

		if flushBeforeQuit != nil {
			flushBeforeQuit()
		}

		logFile, _ := os.OpenFile(filepath.Join(updatesRoot(dataDir), "install.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		cmd := exec.Command("/bin/sh", scriptPath, bundle, stage, fmt.Sprintf("%d", os.Getpid()))
		configureDetachedProcess(cmd)
		if logFile != nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}
		if err := cmd.Start(); err != nil {
			if logFile != nil {
				_ = logFile.Close()
			}
			_ = os.RemoveAll(stage)
			return err
		}
		_ = cmd.Process.Release()
		if logFile != nil {
			_ = logFile.Close()
		}
		_ = os.Remove(readyRecordPath(dataDir))
		if quitApp != nil {
			quitApp()
		}
		return nil

	case "linux":
		target := strings.TrimSpace(os.Getenv("APPIMAGE"))
		if target == "" || !strings.HasSuffix(strings.ToLower(rec.ArchivePath), ".appimage") {
			if isEn {
				return errors.New("Please open the downloaded package to complete installation")
			}
			return errors.New("请打开已下载的安装包完成更新")
		}
		if err := os.Chmod(rec.ArchivePath, 0o755); err != nil {
			return err
		}
		stage, err := os.MkdirTemp("", "block-appimage-update-*")
		if err != nil {
			return err
		}
		scriptPath := filepath.Join(stage, "install.sh")
		if err := os.WriteFile(scriptPath, []byte(linuxAppImageInstallerScript), 0o700); err != nil {
			return err
		}
		if flushBeforeQuit != nil {
			flushBeforeQuit()
		}
		cmd := exec.Command("/bin/sh", scriptPath, target, rec.ArchivePath, fmt.Sprintf("%d", os.Getpid()))
		configureDetachedProcess(cmd)
		if err := cmd.Start(); err != nil {
			return err
		}
		_ = cmd.Process.Release()
		_ = os.Remove(readyRecordPath(dataDir))
		if quitApp != nil {
			quitApp()
		}
		return nil

	case "windows":
		if !strings.HasSuffix(strings.ToLower(rec.ArchivePath), ".exe") {
			return errors.New("invalid Windows installer")
		}
		if flushBeforeQuit != nil {
			flushBeforeQuit()
		}
		cmd := exec.Command(rec.ArchivePath, "--updated", "--force-run")
		configureDetachedProcess(cmd)
		if err := cmd.Start(); err != nil {
			return err
		}
		_ = cmd.Process.Release()
		_ = os.Remove(readyRecordPath(dataDir))
		if quitApp != nil {
			quitApp()
		}
		return nil
	}

	return errors.New("unsupported platform for automatic install")
}
