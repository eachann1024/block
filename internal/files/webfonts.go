package files

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const (
	harmonyFontFamily     = "HarmonyOS Sans SC"
	lxgwWenKaiFontFamily  = "LXGW WenKai"
	notoSerifSCFontFamily = "Noto Serif SC"
)

type webFontFace struct {
	family   string
	fileName string
	size     int64
	sha256   string
}

type webFontFamilySource struct {
	family           string
	cdnBaseURLs      []string
	tarballMirrorURL string
	tarballMaxBytes  int64
	faces            []webFontFace
}

// Official HarmonyOS Sans SC static weights (@fontpkg/harmony-os-sans-sc@1.0.3).
// Regular/Medium/Bold are used by UI controls; Black (900) is downloaded on demand
// when a display export theme (Riso / Poster / Neon) is opened.
var harmonyUIFaces = []webFontFace{
	{
		family:   harmonyFontFamily,
		fileName: "HarmonyOS_Sans_SC_Regular.ttf",
		size:     8261128,
		sha256:   "297b088424be212207df2ce8b98e335468b782aa6b96832af0b8b773d711e2b1",
	},
	{
		family:   harmonyFontFamily,
		fileName: "HarmonyOS_Sans_SC_Medium.ttf",
		size:     8227312,
		sha256:   "6ed1553edccddc48eb27ff25d134a4a715cf54211238d4840b3038576cba1944",
	},
	{
		family:   harmonyFontFamily,
		fileName: "HarmonyOS_Sans_SC_Bold.ttf",
		size:     8158996,
		sha256:   "43a424b85e47fb53a17b3b32026a71801f86f8e022ca6798d186b47d39fa5f01",
	},
}

var harmonyBlackFace = webFontFace{
	family:   harmonyFontFamily,
	fileName: "HarmonyOS_Sans_SC_Black.ttf",
	size:     8125608,
	sha256:   "5de9560908e88820df2e0a5ed9465bc44644ae2ce1cd6c194b76f2ed8e8f186e",
}

// Official LXGW WenKai v1.520 static weights (@fontpkg/lxgw-wen-kai@1.520.0).
var lxgwWenKaiFaces = []webFontFace{
	{
		family:   lxgwWenKaiFontFamily,
		fileName: "LXGWWenKai-Regular.ttf",
		size:     24744500,
		sha256:   "8d6ba638ac9553413354cfaab97637c1cd778444e259441ea1e5f8fb2c697fba",
	},
	{
		family:   lxgwWenKaiFontFamily,
		fileName: "LXGWWenKai-Medium.ttf",
		size:     24289708,
		sha256:   "d7a98ff8898087f019e5617d026c3f83158926ec9e657184920dc4bc7d7d97c8",
	},
}

// Official Noto Serif SC v2.003 Subset OTF weights (notofonts/noto-cjk@Serif2.003).
// Regular & Bold appear first so body and standard headings settle immediately.
var notoSerifSCFaces = []webFontFace{
	{
		family:   notoSerifSCFontFamily,
		fileName: "NotoSerifSC-Regular.otf",
		size:     11625800,
		sha256:   "e8f396decc1f0963a016a989c3d8852e863d1350996f573860a80767c83a1cd3",
	},
	{
		family:   notoSerifSCFontFamily,
		fileName: "NotoSerifSC-Bold.otf",
		size:     12094336,
		sha256:   "24693d48bdb9152f0a06b02af625638a1097abd6de4010ebba027f6e82710527",
	},
	{
		family:   notoSerifSCFontFamily,
		fileName: "NotoSerifSC-SemiBold.otf",
		size:     11771476,
		sha256:   "517d9736136b3b4e9aea742a7e1acda1922aea91a4425bd8db79281934055bf5",
	},
	{
		family:   notoSerifSCFontFamily,
		fileName: "NotoSerifSC-Black.otf",
		size:     11862600,
		sha256:   "91dde57622845beeb663f300813999febdb7fcd5e451bde199d7c954a9740cb4",
	},
}

var harmonyCDNBaseURLs = []string{
	"https://cdn.jsdelivr.net/npm/@fontpkg/harmony-os-sans-sc@1.0.3/",
	"https://fastly.jsdelivr.net/npm/@fontpkg/harmony-os-sans-sc@1.0.3/",
	"https://unpkg.com/@fontpkg/harmony-os-sans-sc@1.0.3/",
}

const harmonyTarballMirrorURL = "https://registry.npmmirror.com/@fontpkg/harmony-os-sans-sc/-/harmony-os-sans-sc-1.0.3.tgz"

var lxgwCDNBaseURLs = []string{
	"https://cdn.jsdelivr.net/npm/@fontpkg/lxgw-wen-kai@1.520.0/",
	"https://fastly.jsdelivr.net/npm/@fontpkg/lxgw-wen-kai@1.520.0/",
	"https://unpkg.com/@fontpkg/lxgw-wen-kai@1.520.0/",
}

const lxgwTarballMirrorURL = "https://registry.npmmirror.com/@fontpkg/lxgw-wen-kai/-/lxgw-wen-kai-1.520.0.tgz"

var notoSerifCDNBaseURLs = []string{
	"https://cdn.jsdelivr.net/gh/notofonts/noto-cjk@Serif2.003/Serif/SubsetOTF/SC/",
	"https://fastly.jsdelivr.net/gh/notofonts/noto-cjk@Serif2.003/Serif/SubsetOTF/SC/",
	"https://gcore.jsdelivr.net/gh/notofonts/noto-cjk@Serif2.003/Serif/SubsetOTF/SC/",
	"https://testingcf.jsdelivr.net/gh/notofonts/noto-cjk@Serif2.003/Serif/SubsetOTF/SC/",
	"https://raw.githubusercontent.com/notofonts/noto-cjk/Serif2.003/Serif/SubsetOTF/SC/",
}

var (
	harmonyExportSource = webFontFamilySource{
		family:           harmonyFontFamily,
		cdnBaseURLs:      harmonyCDNBaseURLs,
		tarballMirrorURL: harmonyTarballMirrorURL,
		tarballMaxBytes:  48 << 20,
		faces:            append(append([]webFontFace(nil), harmonyUIFaces...), harmonyBlackFace),
	}
	lxgwExportSource = webFontFamilySource{
		family:           lxgwWenKaiFontFamily,
		cdnBaseURLs:      lxgwCDNBaseURLs,
		tarballMirrorURL: lxgwTarballMirrorURL,
		tarballMaxBytes:  96 << 20,
		faces:            lxgwWenKaiFaces,
	}
	notoSerifExportSource = webFontFamilySource{
		family:      notoSerifSCFontFamily,
		cdnBaseURLs: notoSerifCDNBaseURLs,
		faces:       notoSerifSCFaces,
	}
	exportWebFontSources = map[string]webFontFamilySource{
		strings.ToLower(harmonyFontFamily):     harmonyExportSource,
		strings.ToLower(lxgwWenKaiFontFamily):  lxgwExportSource,
		strings.ToLower(notoSerifSCFontFamily): notoSerifExportSource,
	}
	exportOnlyWebFontFamilies = map[string]bool{
		strings.ToLower(lxgwWenKaiFontFamily):  true,
		strings.ToLower(notoSerifSCFontFamily): true,
	}
	managedWebFontFiles = buildManagedWebFontFileSet()
)

func buildManagedWebFontFileSet() map[string]bool {
	set := make(map[string]bool, len(harmonyExportSource.faces)+len(lxgwWenKaiFaces)+len(notoSerifSCFaces))
	for _, src := range []webFontFamilySource{harmonyExportSource, lxgwExportSource, notoSerifExportSource} {
		for _, face := range src.faces {
			set[strings.ToLower(face.fileName)] = true
		}
	}
	return set
}

// IsManagedWebFontFile reports whether name matches one of the exact filenames
// managed by the application webfont cache.
func IsManagedWebFontFile(name string) bool {
	return managedWebFontFiles[strings.ToLower(strings.TrimSpace(name))]
}

// IsAppRegisteredExportFontFamily reports whether family is an export-only
// webfont family registered in-process by Block (not seeded from system fonts),
// so settings font pickers can hide it from system font candidates.
func IsAppRegisteredExportFontFamily(family string) bool {
	key := strings.ToLower(strings.TrimSpace(family))
	if !exportOnlyWebFontFamilies[key] {
		return false
	}
	webFontMu.Lock()
	defer webFontMu.Unlock()
	return webFontAppRegisteredFamilies[key] && !webFontSystemSeededFamilies[key]
}

const (
	webFontFailureCooldown      = 90 * time.Second
	webFontFamilyTimeout        = 3 * time.Minute
	webFontHeaderTimeout        = 12 * time.Second
	webFontTarballMaxUncompSize = 256 << 20
)

type fontDownloadJob struct {
	key       string
	dataDir   string
	source    webFontFamilySource
	onUpdated func()
}

var (
	webFontMu                    sync.Mutex
	webFontLoaded                = make(map[string]bool)
	webFontInFlight              = make(map[string]bool)
	webFontRegisterFailed        = make(map[string]bool)
	webFontFailedUntil           = make(map[string]time.Time)
	webFontAppRegisteredFamilies = make(map[string]bool)
	webFontSystemSeededFamilies  = make(map[string]bool)
	fontJobQueue                 []fontDownloadJob
	fontActiveJobKey             string
	fontWorkerRunning            bool
)

func webFontsCacheDir(dataDir string) string {
	return filepath.Join(dataDir, "fonts", "webfonts")
}

func legacyWebFontsCacheDir(dataDir string) string {
	return filepath.Join(dataDir, "fonts")
}

func hasValidFontHeader(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	magic := string(data[:4])
	return magic == "\x00\x01\x00\x00" || magic == "OTTO" || magic == "true" || magic == "ttcf"
}

func verifyWebFontBytes(face webFontFace, data []byte) bool {
	if int64(len(data)) != face.size || !hasValidFontHeader(data) {
		return false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == face.sha256
}

func registerWebFontFaceLocked(face webFontFace, data []byte, fromSystem bool) bool {
	if webFontLoaded[face.fileName] {
		return false
	}
	if err := ui.RegisterFont(data, face.family); err != nil {
		webFontRegisterFailed[face.fileName] = true
		LogWarnf("verified webfont %s (%s) failed to register in text engine: %v", face.fileName, face.family, err)
		return false
	}
	webFontLoaded[face.fileName] = true
	delete(webFontRegisterFailed, face.fileName)
	delete(webFontFailedUntil, face.fileName)
	famKey := strings.ToLower(strings.TrimSpace(face.family))
	if fromSystem {
		webFontSystemSeededFamilies[famKey] = true
	} else {
		webFontAppRegisteredFamilies[famKey] = true
	}
	return true
}

func writeFontCacheAtomic(cacheDir, fileName string, data []byte) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(cacheDir, "."+fileName+".*.tmp")
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
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	dstPath := filepath.Join(cacheDir, fileName)
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func readCachedWebFontBytes(dataDir string, face webFontFace) ([]byte, bool) {
	for _, dir := range []string{webFontsCacheDir(dataDir), legacyWebFontsCacheDir(dataDir)} {
		cachedPath := filepath.Join(dir, face.fileName)
		info, err := os.Stat(cachedPath)
		if err != nil || info.IsDir() || info.Size() != face.size {
			continue
		}
		data, err := os.ReadFile(cachedPath)
		if err == nil && verifyWebFontBytes(face, data) {
			return data, true
		}
	}
	return nil, false
}

func seedFromSystemFontDirs(cacheDir string, missing []webFontFace) map[string][]byte {
	found := make(map[string][]byte)
	if len(missing) == 0 {
		return found
	}
	bySize := make(map[int64][]webFontFace, len(missing))
	for _, f := range missing {
		bySize[f.size] = append(bySize[f.size], f)
	}

	var dirs []string
	switch runtime.GOOS {
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
		}
		dirs = append(dirs, "/Library/Fonts")
	case "windows":
		if winDir := os.Getenv("WINDIR"); winDir != "" {
			dirs = append(dirs, filepath.Join(winDir, "Fonts"))
		}
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			dirs = append(dirs, filepath.Join(localAppData, "Microsoft", "Windows", "Fonts"))
		}
	case "linux":
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			dirs = append(dirs, filepath.Join(home, ".local", "share", "fonts"))
		}
		dirs = append(dirs, "/usr/local/share/fonts", "/usr/share/fonts")
	}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			info, err := ent.Info()
			if err != nil {
				continue
			}
			cands, ok := bySize[info.Size()]
			if !ok {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, ent.Name()))
			if err != nil {
				continue
			}
			for _, face := range cands {
				if _, already := found[face.fileName]; already {
					continue
				}
				if verifyWebFontBytes(face, data) {
					found[face.fileName] = data
					_ = writeFontCacheAtomic(cacheDir, face.fileName, data)
					break
				}
			}
		}
	}
	return found
}

func loadCachedFaces(dataDir string, faces []webFontFace, scanSystemDirs bool) (allPresent bool, registeredAny bool) {
	if dataDir == "" {
		dataDir = AppDataDir()
	}
	cacheDir := webFontsCacheDir(dataDir)

	// Guard against registering heavy weights before a family's primary (Regular) face.
	primaryFileByFamily := make(map[string]string, 4)
	for _, face := range faces {
		if _, exists := primaryFileByFamily[face.family]; !exists {
			primaryFileByFamily[face.family] = face.fileName
		}
	}

	var uncached []webFontFace
	for _, face := range faces {
		webFontMu.Lock()
		loaded := webFontLoaded[face.fileName]
		regFailed := webFontRegisterFailed[face.fileName]
		primaryFile := primaryFileByFamily[face.family]
		primaryReady := face.fileName == primaryFile || webFontLoaded[primaryFile]
		webFontMu.Unlock()
		if loaded || regFailed {
			continue
		}
		if !primaryReady {
			uncached = append(uncached, face)
			continue
		}
		if data, ok := readCachedWebFontBytes(dataDir, face); ok {
			webFontMu.Lock()
			if registerWebFontFaceLocked(face, data, false) {
				registeredAny = true
			}
			webFontMu.Unlock()
			continue
		}
		uncached = append(uncached, face)
	}

	if scanSystemDirs && len(uncached) > 0 {
		seeded := seedFromSystemFontDirs(cacheDir, uncached)
		if len(seeded) > 0 {
			var stillMissing []webFontFace
			webFontMu.Lock()
			for _, face := range uncached {
				primaryFile := primaryFileByFamily[face.family]
				primaryReady := face.fileName == primaryFile || webFontLoaded[primaryFile]
				if data, ok := seeded[face.fileName]; ok && primaryReady {
					if registerWebFontFaceLocked(face, data, true) {
						registeredAny = true
					}
				} else if !webFontLoaded[face.fileName] && !webFontRegisterFailed[face.fileName] {
					stillMissing = append(stillMissing, face)
				}
			}
			webFontMu.Unlock()
			uncached = stillMissing
		}
	}

	if registeredAny {
		theme.BumpFontEpoch()
	}
	return len(uncached) == 0, registeredAny
}

// LoadCachedHarmonyFonts synchronously registers any verified HarmonyOS Sans SC UI
// weights already cached on disk (or matching local system font files).
func LoadCachedHarmonyFonts(dataDir string) (allPresent bool) {
	allPresent, _ = loadCachedFaces(dataDir, harmonyUIFaces, true)
	return allPresent
}

// LoadCachedExportFonts synchronously registers any verified export webfont weights
// (HarmonyOS Sans SC, LXGW WenKai, Noto Serif SC) already present in <dataDir>/fonts
// without triggering any network requests or slow system font scans.
func LoadCachedExportFonts(dataDir string) (registeredAny bool) {
	allFaces := make([]webFontFace, 0, len(harmonyExportSource.faces)+len(lxgwWenKaiFaces)+len(notoSerifSCFaces))
	allFaces = append(allFaces, harmonyExportSource.faces...)
	allFaces = append(allFaces, lxgwWenKaiFaces...)
	allFaces = append(allFaces, notoSerifSCFaces...)
	_, registeredAny = loadCachedFaces(dataDir, allFaces, false)
	return registeredAny
}

// EnsureHarmonyFontsAsync loads any cached HarmonyOS Sans SC UI faces immediately
// and fetches missing weights in the background from CDN mirrors.
func EnsureHarmonyFontsAsync(dataDir string, onUpdated func()) {
	if dataDir == "" {
		dataDir = AppDataDir()
	}
	allPresent, registeredAny := loadCachedFaces(dataDir, harmonyUIFaces, true)
	if registeredAny && onUpdated != nil {
		onUpdated()
	}
	if allPresent {
		return
	}
	enqueueFontJobs([]fontDownloadJob{{
		key:     "ui:" + harmonyFontFamily,
		dataDir: dataDir,
		source: webFontFamilySource{
			family:           harmonyFontFamily,
			cdnBaseURLs:      harmonyCDNBaseURLs,
			tarballMirrorURL: harmonyTarballMirrorURL,
			tarballMaxBytes:  48 << 20,
			faces:            harmonyUIFaces,
		},
		onUpdated: onUpdated,
	}}, false)
}

// EnsureExportFontsAsync never blocks the UI thread on disk reads or system font
// scans. When requested families are not yet registered in memory, it reads local
// caches and registers them in a background goroutine, then queues any remaining
// weights for prioritized silent download into <dataDir>/fonts/webfonts.
func EnsureExportFontsAsync(dataDir string, families []string, onUpdated func()) {
	if len(families) == 0 {
		return
	}
	if dataDir == "" {
		dataDir = AppDataDir()
	}

	var sources []webFontFamilySource
	seenFamily := make(map[string]bool, len(families))
	for _, raw := range families {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key == "" || seenFamily[key] {
			continue
		}
		src, ok := exportWebFontSources[key]
		if !ok {
			continue
		}
		seenFamily[key] = true
		sources = append(sources, src)
	}
	if len(sources) == 0 {
		return
	}

	// Fast O(1) memory check: skip goroutine if every requested face is loaded,
	// in-flight, unregistrable, or currently in failure cooldown.
	needsWork := false
	for _, src := range sources {
		if hasPendingFaces(src.faces) {
			needsWork = true
			break
		}
	}
	if !needsWork {
		return
	}

	go func() {
		var jobs []fontDownloadJob
		registeredAny := false
		for _, src := range sources {
			allPresent, reg := loadCachedFaces(dataDir, src.faces, false)
			if reg {
				registeredAny = true
			}
			if !allPresent && hasPendingFaces(src.faces) {
				jobs = append(jobs, fontDownloadJob{
					key:       "export:" + src.family,
					dataDir:   dataDir,
					source:    src,
					onUpdated: onUpdated,
				})
			}
		}
		if registeredAny && onUpdated != nil {
			onUpdated()
		}
		if len(jobs) > 0 {
			enqueueFontJobs(jobs, true)
		}
	}()
}

func hasPendingFaces(faces []webFontFace) bool {
	now := time.Now()
	webFontMu.Lock()
	defer webFontMu.Unlock()
	for _, face := range faces {
		if webFontLoaded[face.fileName] || webFontRegisterFailed[face.fileName] || webFontInFlight[face.fileName] {
			continue
		}
		if until, ok := webFontFailedUntil[face.fileName]; ok && now.Before(until) {
			continue
		}
		return true
	}
	return false
}

func enqueueFontJobs(jobs []fontDownloadJob, prioritizeFront bool) {
	if len(jobs) == 0 {
		return
	}
	webFontMu.Lock()
	if prioritizeFront {
		reordered := make([]fontDownloadJob, 0, len(jobs)+len(fontJobQueue))
		seen := make(map[string]bool, len(jobs)+len(fontJobQueue)+1)
		if fontActiveJobKey != "" {
			seen[fontActiveJobKey] = true
		}
		for _, j := range jobs {
			if !seen[j.key] {
				seen[j.key] = true
				reordered = append(reordered, j)
			}
		}
		for _, j := range fontJobQueue {
			if !seen[j.key] {
				seen[j.key] = true
				reordered = append(reordered, j)
			}
		}
		fontJobQueue = reordered
	} else {
		seen := make(map[string]bool, len(fontJobQueue)+1)
		if fontActiveJobKey != "" {
			seen[fontActiveJobKey] = true
		}
		for _, j := range fontJobQueue {
			seen[j.key] = true
		}
		for _, j := range jobs {
			if !seen[j.key] {
				seen[j.key] = true
				fontJobQueue = append(fontJobQueue, j)
			}
		}
	}
	if fontWorkerRunning || len(fontJobQueue) == 0 {
		webFontMu.Unlock()
		return
	}
	fontWorkerRunning = true
	webFontMu.Unlock()

	go runFontDownloadWorker()
}

func runFontDownloadWorker() {
	for {
		webFontMu.Lock()
		if len(fontJobQueue) == 0 {
			fontActiveJobKey = ""
			fontWorkerRunning = false
			webFontMu.Unlock()
			return
		}
		job := fontJobQueue[0]
		fontJobQueue = fontJobQueue[1:]
		fontActiveJobKey = job.key
		webFontMu.Unlock()

		downloadMissingWebFontSource(job.dataDir, job.source, job.onUpdated)
	}
}

func faceDownloadTimeout(size int64) time.Duration {
	const minBytesPerSec = 180 * 1024
	d := time.Duration(size/minBytesPerSec) * time.Second
	if d < 45*time.Second {
		d = 45 * time.Second
	}
	if d > 120*time.Second {
		d = 120 * time.Second
	}
	return d
}

func newWebFontHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = webFontHeaderTimeout
	tr.TLSHandshakeTimeout = 10 * time.Second
	return &http.Client{Transport: tr}
}

func downloadMissingWebFontSource(dataDir string, src webFontFamilySource, onUpdated func()) {
	if len(src.faces) == 0 {
		return
	}
	cacheDir := webFontsCacheDir(dataDir)
	// First check disk cache and system dirs in the background before any network IO.
	if allPresent, reg := loadCachedFaces(dataDir, src.faces, true); allPresent {
		if reg && onUpdated != nil {
			onUpdated()
		}
		return
	} else if reg && onUpdated != nil {
		onUpdated()
	}

	primaryFile := src.faces[0].fileName
	ctx, cancel := context.WithTimeout(context.Background(), webFontFamilyTimeout)
	defer cancel()
	client := newWebFontHTTPClient()

	claimableFaces := func() []webFontFace {
		now := time.Now()
		webFontMu.Lock()
		defer webFontMu.Unlock()
		var out []webFontFace
		for _, face := range src.faces {
			if webFontLoaded[face.fileName] || webFontRegisterFailed[face.fileName] || webFontInFlight[face.fileName] {
				continue
			}
			if until, ok := webFontFailedUntil[face.fileName]; ok && now.Before(until) {
				continue
			}
			out = append(out, face)
		}
		return out
	}

	applyFace := func(face webFontFace, data []byte) bool {
		if !verifyWebFontBytes(face, data) {
			return false
		}
		if err := writeFontCacheAtomic(cacheDir, face.fileName, data); err != nil {
			LogWarnf("cache font %s failed: %v", face.fileName, err)
		}
		webFontMu.Lock()
		ok := registerWebFontFaceLocked(face, data, false)
		webFontMu.Unlock()
		if ok {
			theme.BumpFontEpoch()
			if onUpdated != nil {
				onUpdated()
			}
		}
		return ok
	}

	isLoadedOrRegFailed := func(fileName string) (loaded, regFailed bool) {
		webFontMu.Lock()
		loaded = webFontLoaded[fileName]
		regFailed = webFontRegisterFailed[fileName]
		webFontMu.Unlock()
		return loaded, regFailed
	}

	var failedOnCDN []webFontFace
	for _, face := range claimableFaces() {
		if ctx.Err() != nil {
			break
		}
		// Never download/register bold/black weights until the family's Regular face
		// is registered, avoiding body text transiently rendering in Black.
		if face.fileName != primaryFile {
			if loaded, _ := isLoadedOrRegFailed(primaryFile); !loaded {
				break
			}
		}

		webFontMu.Lock()
		if webFontLoaded[face.fileName] || webFontRegisterFailed[face.fileName] || webFontInFlight[face.fileName] {
			webFontMu.Unlock()
			continue
		}
		webFontInFlight[face.fileName] = true
		webFontMu.Unlock()

		var settled bool
		if cached, ok := readCachedWebFontBytes(dataDir, face); ok {
			settled = applyFace(face, cached)
		}
		if !settled {
			for _, base := range src.cdnBaseURLs {
				if ctx.Err() != nil {
					break
				}
				reqCtx, reqCancel := context.WithTimeout(ctx, faceDownloadTimeout(face.size))
				data, err := fetchFontBytes(reqCtx, client, base+face.fileName, face.size+1024)
				reqCancel()
				if err != nil {
					continue
				}
				if applyFace(face, data) {
					settled = true
					break
				}
				if _, regFailed := isLoadedOrRegFailed(face.fileName); regFailed {
					settled = true
					break
				}
			}
		}

		webFontMu.Lock()
		delete(webFontInFlight, face.fileName)
		if !settled && !webFontLoaded[face.fileName] && !webFontRegisterFailed[face.fileName] {
			failedOnCDN = append(failedOnCDN, face)
		}
		webFontMu.Unlock()
	}

	if len(failedOnCDN) > 0 && src.tarballMirrorURL != "" && ctx.Err() == nil {
		// Re-mark remaining faces as in-flight while pulling the fallback tarball.
		webFontMu.Lock()
		var tarWanted []webFontFace
		for _, face := range failedOnCDN {
			if !webFontLoaded[face.fileName] && !webFontRegisterFailed[face.fileName] && !webFontInFlight[face.fileName] {
				webFontInFlight[face.fileName] = true
				tarWanted = append(tarWanted, face)
			}
		}
		webFontMu.Unlock()

		if len(tarWanted) > 0 {
			// Hold non-primary faces until Regular is registered so body never flashes Black.
			deferred := make(map[string][]byte)
			applyTarFace := func(face webFontFace, data []byte) bool {
				if !verifyWebFontBytes(face, data) {
					return false
				}
				if face.fileName != primaryFile {
					if loaded, _ := isLoadedOrRegFailed(primaryFile); !loaded {
						deferred[face.fileName] = data
						return true
					}
				}
				ok := applyFace(face, data)
				if ok && face.fileName == primaryFile && len(deferred) > 0 {
					for _, f := range src.faces[1:] {
						if buf, has := deferred[f.fileName]; has {
							_ = applyFace(f, buf)
							delete(deferred, f.fileName)
						}
					}
				}
				return ok
			}
			if err := fetchFromTarballMirror(ctx, client, src.tarballMirrorURL, src.tarballMaxBytes, tarWanted, applyTarFace); err != nil {
				LogWarnf("%s font CDN fallback incomplete: %v", src.family, err)
			}
			if loaded, _ := isLoadedOrRegFailed(primaryFile); loaded {
				for _, f := range src.faces[1:] {
					if buf, has := deferred[f.fileName]; has {
						_ = applyFace(f, buf)
					}
				}
			}
			webFontMu.Lock()
			for _, face := range tarWanted {
				delete(webFontInFlight, face.fileName)
			}
			webFontMu.Unlock()
		}
	}

	// Put any still-missing faces on cooldown so weak/offline networks do not thrash the queue.
	cooldownUntil := time.Now().Add(webFontFailureCooldown)
	webFontMu.Lock()
	for _, face := range src.faces {
		if !webFontLoaded[face.fileName] && !webFontRegisterFailed[face.fileName] {
			webFontFailedUntil[face.fileName] = cooldownUntil
		}
	}
	webFontMu.Unlock()
}

func fetchFontBytes(ctx context.Context, client *http.Client, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Block-Desktop/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(resp.Body, maxBytes)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fetchFromTarballMirror(
	ctx context.Context,
	client *http.Client,
	tarballURL string,
	maxCompressedBytes int64,
	wanted []webFontFace,
	applyFace func(webFontFace, []byte) bool,
) error {
	if maxCompressedBytes <= 0 {
		maxCompressedBytes = 64 << 20
	}
	wantByName := make(map[string]webFontFace, len(wanted))
	for _, f := range wanted {
		wantByName[f.fileName] = f
	}
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, tarballURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Block-Desktop/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mirror HTTP %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(io.LimitReader(resp.Body, maxCompressedBytes))
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(io.LimitReader(gz, webFontTarballMaxUncompSize))
	for len(wantByName) > 0 {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		base := path.Base(hdr.Name)
		face, ok := wantByName[base]
		if !ok {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, face.size+1024))
		if err != nil {
			return err
		}
		if applyFace(face, data) {
			delete(wantByName, base)
		}
	}
	if len(wantByName) > 0 {
		return fmt.Errorf("%d font weight(s) still missing after mirror fetch", len(wantByName))
	}
	return nil
}
