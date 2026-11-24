package updater

import (
	"bufio"
	"context"
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
	maxUpdateBytes     int64 = 2 << 30
	macBundleID              = "dev.eachann.block"
	macLegacyID              = "com.block.notes"
	macLegacyThinklyID       = "dev.eachann.thinkly-fast"
	stageDirPrefix           = ".block-update-"
	lsregisterBinary         = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
)

var (
	stageDirPattern    = regexp.MustCompile(`^\.block-update-[0-9a-fA-F]+$`)
	lsregisterSuffixRe = regexp.MustCompile(`\s+\(0x[0-9a-fA-F]+\)\s*$`)
)

func markMacNeverIndex(dir string) {
	if runtime.GOOS != "darwin" || dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, ".metadata_never_index"), nil, 0o600)
}

// VerifiedRecord persists a verified, staged update in <dataDir>/app-updates/ready.json.
type VerifiedRecord struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	AssetName   string `json:"asset_name"`
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	ArchivePath string `json:"archive_path"`
	PublishedAt string `json:"published_at,omitempty"`
	ReleaseURL  string `json:"release_url,omitempty"`
	Channel     string `json:"channel,omitempty"`
	InPlace     bool   `json:"in_place"`
}

type installStageRecord struct {
	Path        string `json:"path"`
	FromVersion string `json:"from_version,omitempty"`
	Version     string `json:"version"`
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

func lsregisterPruneMarkerPath(dataDir string) string {
	return filepath.Join(updatesRoot(dataDir), "lsregister-pruned.json")
}

type lsregisterPruneMarker struct {
	Executable string `json:"executable"`
	Bundle     string `json:"bundle"`
	Version    string `json:"version"`
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

// DownloadProgressCallback reports live download progress, speed (MB/s), and active channel.
type DownloadProgressCallback func(downloaded, total int64, speedMBps float64, channel string, preparing bool)

// DownloadAndStage downloads the verified asset across candidate sources, verifies SHA-256,
// and persists ready.json for in-place updates.
func DownloadAndStage(
	ctx context.Context,
	dataDir string,
	entry ReleaseEntry,
	asset ReleaseAsset,
	inPlace bool,
	mode SourceMode,
	onProgress DownloadProgressCallback,
	commitGuard ...func(commit func() error) error,
) (*VerifiedRecord, error) {
	expectedSHA := asset.EffectiveSHA256()
	version, _, _, validVersion := ParseReleaseTag(entry.Tag)
	if !validVersion || version != entry.Version || !IsTrustedReleaseAsset(asset, entry.Tag) || asset.Size > maxUpdateBytes {
		logError("download.untrusted_metadata", "version", entry.Version, "asset", asset.Name)
		return nil, errors.New("release asset metadata is invalid")
	}
	cands := CandidateAssetURLs(asset.DownloadURL, mode)
	if len(cands) == 0 {
		logError("download.untrusted_url", "version", entry.Version, "url", asset.DownloadURL)
		return nil, errors.New("untrusted release download URL")
	}

	verDir := filepath.Join(updatesRoot(dataDir), entry.Version)
	if err := os.MkdirAll(verDir, 0o700); err != nil {
		logError("download.mkdir_failed", "dir", verDir, "err", err)
		return nil, err
	}
	markMacNeverIndex(updatesRoot(dataDir))
	markMacNeverIndex(verDir)
	finalPath := filepath.Join(verDir, asset.Name)
	tempDir, err := os.MkdirTemp(verDir, ".download-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	partPath := filepath.Join(tempDir, asset.Name)

	logInfo("download.start", "version", entry.Version, "asset", asset.Name, "expected_bytes", asset.Size, "sha256", expectedSHA[:16], "mode", mode, "candidates", len(cands))

	var usedChannel string
	var lastErr error
	for idx, cand := range cands {
		if ctx.Err() != nil {
			_ = os.Remove(partPath)
			logInfo("download.cancelled", "version", entry.Version, "err", ctx.Err())
			return nil, ctx.Err()
		}
		_ = os.Remove(partPath)
		started := time.Now()
		err := downloadOneSource(ctx, cand, partPath, asset.Size, expectedSHA, onProgress)
		elapsedMs := time.Since(started).Milliseconds()
		if err == nil {
			usedChannel = cand.Channel
			lastErr = nil
			logInfo("download.verified", "version", entry.Version, "channel", cand.Channel, "url", cand.URL, "elapsed_ms", elapsedMs, "dest", finalPath)
			break
		}
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			_ = os.Remove(partPath)
			logInfo("download.cancelled", "version", entry.Version)
			return nil, ctx.Err()
		}
		logWarn("download.source_failover", "attempt", idx+1, "channel", cand.Channel, "url", cand.URL, "elapsed_ms", elapsedMs, "err", err)
		lastErr = err
	}
	if lastErr != nil {
		_ = os.Remove(partPath)
		logError("download.all_sources_failed", "version", entry.Version, "asset", asset.Name, "err", lastErr)
		return nil, lastErr
	}

	rec := &VerifiedRecord{
		Version:     entry.Version,
		Tag:         entry.Tag,
		AssetName:   asset.Name,
		DownloadURL: asset.DownloadURL,
		SHA256:      expectedSHA,
		Size:        asset.Size,
		ArchivePath: finalPath,
		PublishedAt: entry.PublishedAt,
		ReleaseURL:  entry.HTMLURL,
		Channel:     usedChannel,
		InPlace:     inPlace,
	}
	doCommit := func() error {
		if ctx.Err() != nil {
			_ = os.Remove(partPath)
			logInfo("download.cancelled", "version", entry.Version, "err", ctx.Err())
			return ctx.Err()
		}
		if err := os.Rename(partPath, finalPath); err != nil {
			_ = os.Remove(partPath)
			logError("download.rename_failed", "from", partPath, "to", finalPath, "err", err)
			return err
		}
		if inPlace {
			raw, err := json.MarshalIndent(rec, "", "  ")
			if err != nil {
				return err
			}
			if err := writeFileAtomic(readyRecordPath(dataDir), raw, 0o600); err != nil {
				logError("ready.persist_failed", "version", entry.Version, "err", err)
				return err
			}
			logInfo("ready.persisted", "version", entry.Version, "record", readyRecordPath(dataDir))
		}
		return nil
	}
	if len(commitGuard) > 0 && commitGuard[0] != nil {
		if err := commitGuard[0](doCommit); err != nil {
			return nil, err
		}
	} else if err := doCommit(); err != nil {
		return nil, err
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

	if expectedSize > maxUpdateBytes || contentLen > maxUpdateBytes {
		return errors.New("update package is too large")
	}
	total := expectedSize
	if total <= 0 && contentLen > 0 {
		total = contentLen
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	hasher := sha256.New()
	buf := make([]byte, 64*1024)
	var written int64
	started := time.Now()
	var lastTick time.Time

	if onProgress != nil {
		onProgress(0, total, 0, cand.Channel, false)
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
			if written > maxUpdateBytes || (total > 0 && written > total) {
				return errors.New("downloaded size exceeded manifest size")
			}
			now := time.Now()
			if onProgress != nil && now.Sub(lastTick) >= 80*time.Millisecond {
				lastTick = now
				secs := now.Sub(started).Seconds()
				speed := 0.0
				if secs > 0.05 {
					speed = (float64(written) / (1024.0 * 1024.0)) / secs
				}
				onProgress(written, total, speed, cand.Channel, false)
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
		logError("download.checksum_mismatch", "channel", cand.Channel, "url", cand.URL, "expected", expectedSHA[:16], "got", gotHex[:16])
		return fmt.Errorf("checksum mismatch from %s", cand.Channel)
	}
	return nil
}

func prepareMacAppFromDMG(dmgPath, unpackedDir, expectedVersion string) (string, error) {
	if err := os.MkdirAll(unpackedDir, 0o700); err != nil {
		return "", err
	}
	markMacNeverIndex(unpackedDir)
	mountDir, err := os.MkdirTemp("", "block-dmg-mnt-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(mountDir)

	attach := exec.Command("/usr/bin/hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", mountDir, dmgPath)
	if out, err := attach.CombinedOutput(); err != nil {
		return "", fmt.Errorf("mount DMG failed: %s", strings.TrimSpace(string(out)))
	}
	srcApp := filepath.Join(mountDir, "Block.app")
	detached := false
	detach := func() {
		if detached {
			return
		}
		detached = true
		_ = exec.Command(lsregisterBinary, "-u", srcApp).Run()
		_ = exec.Command("/usr/bin/hdiutil", "detach", mountDir, "-force").Run()
	}
	defer detach()

	info, err := os.Lstat(srcApp)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("DMG does not contain Block.app")
	}

	dstApp := filepath.Join(unpackedDir, "Block.bundle")
	if out, err := exec.Command("/usr/bin/ditto", srcApp, dstApp).CombinedOutput(); err != nil {
		return "", fmt.Errorf("unpack Block.app failed: %s", strings.TrimSpace(string(out)))
	}
	detach()
	_ = exec.Command(lsregisterBinary, "-u", dstApp).Run()

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
		if err != nil || ver == "" || CompareVersion(ver, expectedVersion) != 0 {
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
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--verbose=2", bundlePath).CombinedOutput(); err != nil {
		return fmt.Errorf("update bundle signature verification failed: %s", strings.TrimSpace(string(out)))
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

func shouldPruneMacRegistrations(dataDir, runningVersion, lastUpdatedFrom, resolvedExe, canonicalBundle string, acknowledgedUpdate bool) bool {
	if acknowledgedUpdate {
		return true
	}
	if dataDir == "" {
		return false
	}
	raw, err := os.ReadFile(lsregisterPruneMarkerPath(dataDir))
	if err != nil {
		return true
	}
	var marker lsregisterPruneMarker
	if json.Unmarshal(raw, &marker) != nil {
		return true
	}
	if marker.Executable != resolvedExe || marker.Bundle != canonicalBundle {
		return true
	}
	if runningVersion != "" && marker.Version != runningVersion {
		return true
	}
	if lastUpdatedFrom != "" && runningVersion != "" && CompareVersion(runningVersion, lastUpdatedFrom) > 0 && marker.Version == "" {
		return true
	}
	return false
}

func writeLSRegisterPruneMarker(dataDir string, marker lsregisterPruneMarker) {
	if dataDir == "" {
		return
	}
	_ = os.MkdirAll(updatesRoot(dataDir), 0o700)
	if data, err := json.Marshal(marker); err == nil {
		_ = os.WriteFile(lsregisterPruneMarkerPath(dataDir), data, 0o600)
	}
}

func pruneDuplicateMacRegistrations(canonicalBundle string) {
	if runtime.GOOS != "darwin" || canonicalBundle == "" {
		return
	}
	canonical := filepath.Clean(canonicalBundle)
	if real, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = real
	}
	cmd := exec.Command(lsregisterBinary, "-dump")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	seen := make(map[string]struct{})
	var stale []string
	var curPath, curID string
	isTargetID := func(id string) bool {
		return id == macBundleID || id == macLegacyID || id == macLegacyThinklyID
	}
	flush := func() {
		if curPath != "" && isTargetID(curID) {
			clean := filepath.Clean(curPath)
			if real, err := filepath.EvalSymlinks(clean); err == nil {
				clean = real
			}
			if clean != canonical {
				if _, dup := seen[curPath]; !dup {
					seen[curPath] = struct{}{}
					stale = append(stale, curPath)
				}
			}
		}
		curPath = ""
		curID = ""
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(line, "--------------------------------------------------------------------------------") {
			flush()
			continue
		}
		if curPath == "" && strings.HasPrefix(line, "path:") {
			curPath = strings.TrimSpace(lsregisterSuffixRe.ReplaceAllString(line[len("path:"):], ""))
		} else if curID == "" && strings.HasPrefix(line, "identifier:") {
			curID = strings.TrimSpace(lsregisterSuffixRe.ReplaceAllString(line[len("identifier:"):], ""))
		}
	}
	flush()
	_ = cmd.Wait()

	for _, p := range stale {
		_ = exec.Command(lsregisterBinary, "-u", p).Run()
	}
	if len(stale) > 0 {
		_ = exec.Command(lsregisterBinary, "-f", canonical).Run()
		logInfo("startup.lsregister_pruned", "count", len(stale), "canonical", canonical)
	}
}

func refreshMacDocumentIconCacheOnce(updatesDir, bundle string) {
	if runtime.GOOS != "darwin" || updatesDir == "" || bundle == "" {
		return
	}
	docIcon := filepath.Join(bundle, "Contents", "Resources", "DocumentIcon.icns")
	if _, err := os.Stat(docIcon); err != nil {
		return
	}
	marker := filepath.Join(updatesDir, ".docicon-v1")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	_ = exec.Command(lsregisterBinary, "-f", bundle).Run()
	if out, err := exec.Command("/usr/bin/getconf", "DARWIN_USER_CACHE_DIR").Output(); err == nil {
		cacheDir := strings.TrimSpace(string(out))
		if cacheDir != "" {
			if ents, rErr := os.ReadDir(cacheDir); rErr == nil {
				for _, ent := range ents {
					if strings.HasPrefix(ent.Name(), "com.apple.iconservices") {
						_ = os.RemoveAll(filepath.Join(cacheDir, ent.Name()))
					}
				}
			}
		}
	}
	_ = exec.Command("/usr/bin/killall", "iconservicesagent").Run()
	_ = os.WriteFile(marker, []byte("1\n"), 0o600)
}

// AcknowledgeStartupAndClean signals any pending macOS install stage that the new version
// launched cleanly, logs rollback anomalies if an older version booted after an attempted swap,
// cleans sibling stage directories, and prunes outdated cached downloads.
func AcknowledgeStartupAndClean(dataDir, runningVersion string) {
	acknowledgeStartupAndClean(dataDir, runningVersion, "")
}

func acknowledgeStartupAndClean(dataDir, runningVersion, lastUpdatedFrom string) {
	root := updatesRoot(dataDir)
	_ = os.MkdirAll(root, 0o700)
	markMacNeverIndex(root)

	exe, _ := os.Executable()
	resolvedExe := filepath.Clean(exe)
	if realExe, err := filepath.EvalSymlinks(resolvedExe); err == nil {
		resolvedExe = realExe
	}
	logInfo("startup.init", "running_version", runningVersion, "os", runtime.GOOS, "arch", runtime.GOARCH, "exe", exe)

	if runtime.GOOS == "darwin" {
		if bundle, err := installedMacBundle(); err == nil && !livesInUpdateStage(bundle) {
			acknowledgedUpdate := false
			if raw, rErr := os.ReadFile(stageRecordPath(dataDir)); rErr == nil {
				var st installStageRecord
				if json.Unmarshal(raw, &st) == nil && filepath.Dir(st.Path) == filepath.Dir(bundle) &&
					stageDirPattern.MatchString(filepath.Base(st.Path)) {
					stageInfo, stageErr := os.Lstat(st.Path)
					if stageErr == nil && stageInfo.IsDir() && CompareVersion(runningVersion, st.Version) == 0 {
						if err := os.WriteFile(filepath.Join(st.Path, "started"), []byte(fmt.Sprintf("%d", os.Getpid())), 0o600); err == nil {
							_ = os.Remove(stageRecordPath(dataDir))
							acknowledgedUpdate = true
							logInfo("startup.install_acknowledged", "from_version", st.FromVersion, "installed_version", runningVersion, "stage", st.Path)
						}
					}
				}
			}
			canonicalBundle := filepath.Clean(bundle)
			if realBundle, bErr := filepath.EvalSymlinks(canonicalBundle); bErr == nil {
				canonicalBundle = realBundle
			}
			if shouldPruneMacRegistrations(dataDir, runningVersion, lastUpdatedFrom, resolvedExe, canonicalBundle, acknowledgedUpdate) {
				pruneDuplicateMacRegistrations(bundle)
				writeLSRegisterPruneMarker(dataDir, lsregisterPruneMarker{
					Executable: resolvedExe,
					Bundle:     canonicalBundle,
					Version:    runningVersion,
				})
			}
			refreshMacDocumentIconCacheOnce(root, bundle)
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
			if runtime.GOOS == "darwin" {
				_ = exec.Command(lsregisterBinary, "-u", filepath.Join(root, name, "unpacked", "Block.app")).Run()
			}
			_ = os.RemoveAll(filepath.Join(root, name))
			logInfo("startup.pruned_old_cache", "version", name)
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
		logWarn("restore.invalid_json", "err", err)
		_ = os.Remove(readyRecordPath(dataDir))
		return nil, false
	}
	if err := validateReadyFromOfficialSource(dataDir, runningVersion, &rec); err != nil {
		logWarn("restore.untrusted_update", "err", err)
		return nil, false
	}
	logInfo("restore.ready_ok", "version", rec.Version, "archive", rec.ArchivePath)
	return &rec, true
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-ready-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func matchOfficialReleaseAsset(release ReleaseEntry, rec *VerifiedRecord, fileSize int64) (*ReleaseAsset, bool) {
	if release.Version != rec.Version {
		return nil, false
	}
	tagMatches := release.Tag == rec.Tag
	if !tagMatches {
		// Allow legacy ready.json records that stored unprefixed version in Tag to re-bind
		// when the asset URL and SHA-256 still match the official release.
		normRecVer, _, _, ok := ParseReleaseTag(rec.Tag)
		tagMatches = ok && normRecVer == release.Version && strings.EqualFold(release.Tag, "v"+release.Version)
	}
	if !tagMatches {
		return nil, false
	}
	official, inPlace, _ := PickPlatformAsset(release.Assets, runtime.GOOS, runtime.GOARCH)
	if inPlace && official != nil && official.Name == rec.AssetName && official.DownloadURL == rec.DownloadURL &&
		(official.Size <= 0 || official.Size == fileSize) &&
		strings.EqualFold(official.EffectiveSHA256(), rec.SHA256) {
		rec.Tag = release.Tag
		return official, true
	}
	return nil, false
}

var errStagedIntegrity = errors.New("staged update integrity failed")

// A ready record is a hint about a local download. GitHub remains authoritative.
func validateReadyFromOfficialSource(dataDir, runningVersion string, rec *VerifiedRecord) error {
	version, _, _, valid := ParseReleaseTag(rec.Version)
	if !valid || version != rec.Version || CompareVersion(rec.Version, runningVersion) <= 0 {
		return fmt.Errorf("%w: staged update version is invalid", errStagedIntegrity)
	}
	asset := ReleaseAsset{Name: rec.AssetName, DownloadURL: rec.DownloadURL, SHA256: rec.SHA256}
	if !rec.InPlace || !IsTrustedReleaseAsset(asset, rec.Tag) {
		return fmt.Errorf("%w: staged update metadata is invalid", errStagedIntegrity)
	}
	expected := filepath.Join(updatesRoot(dataDir), rec.Version, rec.AssetName)
	if filepath.Clean(rec.ArchivePath) != expected {
		return fmt.Errorf("%w: staged update path does not match the platform asset", errStagedIntegrity)
	}
	root, err := filepath.EvalSymlinks(updatesRoot(dataDir))
	if err != nil {
		return fmt.Errorf("%w: %v", errStagedIntegrity, err)
	}
	resolved, err := filepath.EvalSymlinks(expected)
	if err != nil || resolved != filepath.Join(root, rec.Version, rec.AssetName) {
		return fmt.Errorf("%w: staged update must stay in its cache directory", errStagedIntegrity)
	}
	info, err := os.Stat(expected)
	if err != nil {
		return fmt.Errorf("%w: %v", errStagedIntegrity, err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxUpdateBytes {
		return fmt.Errorf("%w: staged update file type or size is invalid", errStagedIntegrity)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	releases, _, _, _, fetchErr := FetchReleases(ctx, SourceGitHub)
	checkedPlatformAsset := false
	if fetchErr == nil {
		for _, release := range releases {
			if release.Version == rec.Version && release.Tag == rec.Tag {
				if platformAsset, _, _ := PickPlatformAsset(release.Assets, runtime.GOOS, runtime.GOARCH); platformAsset != nil {
					checkedPlatformAsset = true
				}
			}
			if official, ok := matchOfficialReleaseAsset(release, rec, info.Size()); ok {
				if shaErr := VerifyFileSHA256(expected, official.EffectiveSHA256()); shaErr != nil {
					return fmt.Errorf("%w: %v", errStagedIntegrity, shaErr)
				}
				return nil
			}
		}
	}
	if !checkedPlatformAsset {
		// Under frequent releases (or feed outage), verify directly against GitHub's exact tag release API
		// with its own fresh timeout budget so an earlier feed timeout cannot starve tag verification.
		lookupTags := []string{rec.Tag}
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(rec.Tag)), "v") {
			lookupTags = append(lookupTags, "v"+rec.Version)
		}
		var transientErr error
		for _, tag := range lookupTags {
			tagCtx, tagCancel := context.WithTimeout(context.Background(), 15*time.Second)
			rel, tagErr := FetchReleaseByTag(tagCtx, tag)
			tagCancel()
			if tagErr != nil {
				if !errors.Is(tagErr, errReleaseNotFound) {
					transientErr = tagErr
				}
				continue
			}
			if rel == nil {
				continue
			}
			if official, ok := matchOfficialReleaseAsset(*rel, rec, info.Size()); ok {
				if shaErr := VerifyFileSHA256(expected, official.EffectiveSHA256()); shaErr != nil {
					return fmt.Errorf("%w: %v", errStagedIntegrity, shaErr)
				}
				return nil
			}
			return fmt.Errorf("%w: staged update does not match the official platform release", errStagedIntegrity)
		}
		if transientErr != nil {
			return fmt.Errorf("cannot verify the staged update with GitHub: %w", transientErr)
		}
		if fetchErr != nil {
			return fmt.Errorf("cannot verify the staged update with GitHub: %w", fetchErr)
		}
	}
	return fmt.Errorf("%w: staged update does not match the official platform release", errStagedIntegrity)
}

const macSwapInstallerScript = `#!/bin/sh
set -eu
target="$1"
stage="$2"
parent_pid="$3"
target_ver="$4"
ts() { date "+%Y-%m-%dT%H:%M:%S%z"; }
log() { printf "%s [install.sh] %s\n" "$(ts)" "$*"; }

log "begin target=$target stage=$stage parent_pid=$parent_pid target_ver=$target_ver"
count=0
while kill -0 "$parent_pid" 2>/dev/null; do
  count=$((count + 1))
  if [ "$count" -gt 240 ]; then
    log "ERROR timed out waiting for parent_pid=$parent_pid to exit"
    exit 1
  fi
  sleep 0.25
done
log "parent exited after $((count * 250))ms"

remove_stage() {
  rm -rf "$stage"
}
rollback() {
  log "WARN executing rollback for target=$target"
  if [ -d "$stage/previous.bundle" ]; then
    if [ -e "$target" ] && ! mv "$target" "$stage/failed.bundle"; then
      log "ERROR rollback could not move the new bundle; backup kept at $stage/previous.bundle"
      exit 1
    fi
    if ! mv "$stage/previous.bundle" "$target"; then
      log "ERROR rollback could not restore the old bundle; backup kept at $stage/previous.bundle"
      exit 1
    fi
  fi
  /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$target" >/dev/null 2>&1 || true
  /usr/bin/open "$target" || true
  remove_stage
  log "rollback complete"
}
abort() {
  trap - HUP INT TERM
  rollback
  exit 1
}
trap abort HUP INT TERM

if ! mv "$target" "$stage/previous.bundle"; then
  log "ERROR mv current bundle to previous.bundle failed"
  /usr/bin/open "$target" || true
  remove_stage
  exit 1
fi
if ! mv "$stage/next.bundle" "$target"; then
  log "ERROR mv next.bundle to target failed"
  rollback
  exit 1
fi
/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$target" >/dev/null 2>&1 || true
log "bundle swapped into place; launching updated app"
if ! /usr/bin/open "$target"; then
  log "ERROR open updated target failed"
  rollback
  exit 1
fi

count=0
while [ ! -f "$stage/started" ]; do
  count=$((count + 1))
  if [ "$count" -gt 60 ]; then
    if ! /usr/bin/pgrep -f "$target/Contents/MacOS/block" >/dev/null 2>&1; then
      log "ERROR updated process not running after 60s; rolling back"
      rollback
      exit 1
    fi
    log "WARN updated process did not acknowledge startup; backup kept at $stage/previous.bundle"
    exit 1
  fi
  sleep 1
done
log "handshake confirmed ($stage/started); update to v$target_ver succeeded"
remove_stage
`

const linuxAppImageInstallerScript = `#!/bin/sh
set -eu
target="$1"
next_bin="$2"
parent_pid="$3"
ts() { date "+%Y-%m-%dT%H:%M:%S%z"; }
log() { printf "%s [install-linux.sh] %s\n" "$(ts)" "$*"; }

log "waiting for parent_pid=$parent_pid"
count=0
while kill -0 "$parent_pid" 2>/dev/null; do
  count=$((count + 1))
  if [ "$count" -gt 240 ]; then
    log "ERROR timed out waiting for parent exit"
    exit 1
  fi
  sleep 0.25
done

target_dir="$(dirname "$target")"
target_base="$(basename "$target")"
staged="$target_dir/.${target_base}.next.$$"
backup="$target_dir/.${target_base}.bak.$$"
swapped=0

cleanup() {
  rm -f "$staged"
  if [ "$swapped" -eq 0 ]; then
    rm -f "$backup"
  fi
}
trap cleanup EXIT

rollback() {
  log "rolling back AppImage to $backup"
  rm -f "$staged"
  if [ -f "$backup" ]; then
    mv -f "$backup" "$target" || true
  fi
}

cp "$next_bin" "$staged"
chmod 755 "$staged"
if [ -f "$target" ]; then
  cp -p "$target" "$backup"
fi
if ! mv -f "$staged" "$target"; then
  log "ERROR atomic swap failed"
  rollback
  exit 1
fi
swapped=1
log "swapped AppImage at $target; relaunching"
"$target" >/dev/null 2>&1 &
new_pid=$!
sleep 2
if ! kill -0 "$new_pid" 2>/dev/null; then
  log "ERROR updated AppImage exited immediately; rolling back"
  rollback
  "$target" >/dev/null 2>&1 &
  exit 1
fi
rm -f "$backup" "$next_bin"
log "updated AppImage running (pid=$new_pid)"
`

// InstallReady verifies the staged package, flushes workspace data via flushBeforeQuit,
// spawns the platform atomic replacement helper, and invokes quitApp.
func InstallReady(dataDir string, rec *VerifiedRecord, isEn bool, flushBeforeQuit func() error, quitApp func(func() error) error) error {
	if rec == nil || quitApp == nil {
		return errors.New("no staged update ready to install")
	}
	fromVer := CurrentVersion()
	logInfo("install.begin", "from_version", fromVer, "to_version", rec.Version, "archive", rec.ArchivePath)

	if err := validateReadyFromOfficialSource(dataDir, fromVer, rec); err != nil {
		logError("install.verification_failed", "version", rec.Version, "archive", rec.ArchivePath, "err", err)
		return err
	}

	installLogPath := InstallLogFilePath(dataDir)
	_ = os.MkdirAll(filepath.Dir(installLogPath), 0o755)

	switch runtime.GOOS {
	case "darwin":
		bundle, err := installedMacBundle()
		if err != nil {
			logError("install.not_app_bundle", "err", err)
			if isEn {
				return errors.New("Updates can only be installed from a packaged Block.app")
			}
			return errors.New("仅打包后的 Block.app 支持原位重启更新")
		}
		if strings.Contains(bundle, "/AppTranslocation/") || strings.HasPrefix(bundle, "/Volumes/") {
			logError("install.readonly_location", "bundle", bundle)
			if isEn {
				return errors.New("Move Block.app to /Applications before installing updates")
			}
			return errors.New("请先将 Block 移入「应用程序」文件夹后再执行更新")
		}
		parent := filepath.Dir(bundle)
		probe, err := os.CreateTemp(parent, ".block-writecheck-*")
		if err != nil {
			logError("install.permission_denied", "parent", parent, "err", err)
			if isEn {
				return fmt.Errorf("No write permission in %s", parent)
			}
			return fmt.Errorf("应用所在目录无写入权限：%s", parent)
		}
		probeName := probe.Name()
		_ = probe.Close()
		_ = os.Remove(probeName)

		unpackedDir, err := os.MkdirTemp(filepath.Dir(rec.ArchivePath), "unpacked-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(unpackedDir)
		appPath, err := prepareMacAppFromDMG(rec.ArchivePath, unpackedDir, rec.Version)
		if err != nil {
			return err
		}

		stage, err := os.MkdirTemp(parent, stageDirPrefix)
		if err != nil {
			logError("install.stage_mkdir_failed", "stage", stage, "err", err)
			return err
		}
		markMacNeverIndex(stage)
		nextApp := filepath.Join(stage, "next.bundle")
		if out, err := exec.Command("/usr/bin/ditto", appPath, nextApp).CombinedOutput(); err != nil {
			_ = os.RemoveAll(stage)
			logError("install.stage_ditto_failed", "err", strings.TrimSpace(string(out)))
			return fmt.Errorf("stage update failed: %s", strings.TrimSpace(string(out)))
		}
		if err := validateMacBundle(nextApp, rec.Version); err != nil {
			_ = os.RemoveAll(stage)
			logError("install.stage_validate_failed", "err", err)
			return err
		}
		scriptPath := filepath.Join(stage, "install.sh")
		if err := os.WriteFile(scriptPath, []byte(macSwapInstallerScript), 0o700); err != nil {
			_ = os.RemoveAll(stage)
			logError("install.write_script_failed", "err", err)
			return err
		}

		flushStart := time.Now()
		if flushBeforeQuit != nil {
			if err := flushBeforeQuit(); err != nil {
				_ = os.RemoveAll(stage)
				return fmt.Errorf("cannot save changes before updating: %w", err)
			}
		}
		stRaw, _ := json.Marshal(installStageRecord{Path: stage, FromVersion: fromVer, Version: rec.Version})
		if err := os.WriteFile(stageRecordPath(dataDir), stRaw, 0o600); err != nil {
			_ = os.RemoveAll(stage)
			return err
		}
		logInfo("install.flush_ok", "elapsed_ms", time.Since(flushStart).Milliseconds())

		logFile, _ := os.OpenFile(installLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		cmd := exec.Command("/bin/sh", scriptPath, bundle, stage, fmt.Sprintf("%d", os.Getpid()), rec.Version)
		configureDetachedProcess(cmd)
		if logFile != nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}
		if logFile != nil {
			defer logFile.Close()
		}
		err = startInstallerOnQuit(dataDir, cmd, quitApp)
		if err != nil {
			_ = os.RemoveAll(stage)
			_ = os.Remove(stageRecordPath(dataDir))
		}
		return err

	case "linux":
		target := strings.TrimSpace(os.Getenv("APPIMAGE"))
		if target == "" || !strings.HasSuffix(strings.ToLower(rec.ArchivePath), ".appimage") {
			if isEn {
				return errors.New("Please open the downloaded package to complete installation")
			}
			return errors.New("请打开已下载的安装包完成更新")
		}
		if err := os.Chmod(rec.ArchivePath, 0o755); err != nil {
			logError("install.chmod_failed", "err", err)
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
			if err := flushBeforeQuit(); err != nil {
				_ = os.RemoveAll(stage)
				return fmt.Errorf("cannot save changes before updating: %w", err)
			}
		}
		logFile, _ := os.OpenFile(installLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		cmd := exec.Command("/bin/sh", scriptPath, target, rec.ArchivePath, fmt.Sprintf("%d", os.Getpid()))
		configureDetachedProcess(cmd)
		if logFile != nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}
		if logFile != nil {
			defer logFile.Close()
		}
		err = startInstallerOnQuit(dataDir, cmd, quitApp)
		if err != nil {
			_ = os.RemoveAll(stage)
		}
		return err

	case "windows":
		if !strings.HasSuffix(strings.ToLower(rec.ArchivePath), ".exe") {
			return errors.New("invalid Windows installer")
		}
		if flushBeforeQuit != nil {
			if err := flushBeforeQuit(); err != nil {
				return fmt.Errorf("cannot save changes before updating: %w", err)
			}
		}
		// Launch the silent updater BEFORE quitting so any UAC cancellation
		// (only needed for legacy/machine-wide Program Files installs) keeps
		// Block open and ready.
		if err := launchWindowsUpdater(rec.ArchivePath); err != nil {
			logError("install.windows_launch_failed", "err", err)
			return err
		}
		logInfo("install.windows_installer_launched", "path", rec.ArchivePath)
		return quitApp(func() error { return nil })
	}

	return errors.New("unsupported platform for automatic install")
}

func startInstallerOnQuit(dataDir string, cmd *exec.Cmd, quitApp func(func() error) error) error {
	return quitApp(func() error {
		if err := cmd.Start(); err != nil {
			logError("install.spawn_helper_failed", "err", err)
			return err
		}
		logInfo("install.helper_spawned", "helper_pid", cmd.Process.Pid)
		_ = cmd.Process.Release()
		return nil
	})
}
