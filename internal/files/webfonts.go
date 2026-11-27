package files

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"sync"
	"time"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const harmonyFontFamily = "HarmonyOS Sans SC"

type harmonyFontFace struct {
	fileName string
	size     int64
	sha256   string
}

// Official HarmonyOS Sans SC static weights (@fontpkg/harmony-os-sans-sc@1.0.3):
// Regular (400), Medium (500 for UI controls), and Bold (700 for titles/headings/bold).
var harmonyFontFaces = []harmonyFontFace{
	{
		fileName: "HarmonyOS_Sans_SC_Regular.ttf",
		size:     8261128,
		sha256:   "297b088424be212207df2ce8b98e335468b782aa6b96832af0b8b773d711e2b1",
	},
	{
		fileName: "HarmonyOS_Sans_SC_Medium.ttf",
		size:     8227312,
		sha256:   "6ed1553edccddc48eb27ff25d134a4a715cf54211238d4840b3038576cba1944",
	},
	{
		fileName: "HarmonyOS_Sans_SC_Bold.ttf",
		size:     8158996,
		sha256:   "43a424b85e47fb53a17b3b32026a71801f86f8e022ca6798d186b47d39fa5f01",
	},
}

var harmonyCDNBaseURLs = []string{
	"https://cdn.jsdelivr.net/npm/@fontpkg/harmony-os-sans-sc@1.0.3/",
	"https://fastly.jsdelivr.net/npm/@fontpkg/harmony-os-sans-sc@1.0.3/",
	"https://unpkg.com/@fontpkg/harmony-os-sans-sc@1.0.3/",
}

const harmonyTarballMirrorURL = "https://registry.npmmirror.com/@fontpkg/harmony-os-sans-sc/-/harmony-os-sans-sc-1.0.3.tgz"

var (
	harmonyMu          sync.Mutex
	harmonyLoadedFace  = make(map[string]bool, len(harmonyFontFaces))
	harmonyDownloading bool
)

func hasValidFontHeader(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	magic := string(data[:4])
	return magic == "\x00\x01\x00\x00" || magic == "OTTO" || magic == "true" || magic == "ttcf"
}

func verifyHarmonyFontBytes(face harmonyFontFace, data []byte) bool {
	if int64(len(data)) != face.size || !hasValidFontHeader(data) {
		return false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == face.sha256
}

func registerHarmonyFaceLocked(face harmonyFontFace, data []byte) bool {
	if harmonyLoadedFace[face.fileName] {
		return false
	}
	if err := ui.RegisterFont(data, harmonyFontFamily); err != nil {
		LogWarnf("register font %s failed: %v", face.fileName, err)
		return false
	}
	harmonyLoadedFace[face.fileName] = true
	return true
}

func writeFontCacheAtomic(cacheDir, fileName string, data []byte) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	tmpPath := filepath.Join(cacheDir, fmt.Sprintf(".%s.%d.tmp", fileName, time.Now().UnixNano()))
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	dstPath := filepath.Join(cacheDir, fileName)
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func seedFromSystemFontDirs(cacheDir string, missing []harmonyFontFace) map[string][]byte {
	found := make(map[string][]byte)
	if len(missing) == 0 {
		return found
	}
	bySize := make(map[int64]harmonyFontFace, len(missing))
	for _, f := range missing {
		bySize[f.size] = f
	}

	var dirs []string
	switch runtime.GOOS {
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
		}
		dirs = append(dirs, "/Library/Fonts")
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
			face, ok := bySize[info.Size()]
			if !ok {
				continue
			}
			if _, already := found[face.fileName]; already {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, ent.Name()))
			if err != nil || !verifyHarmonyFontBytes(face, data) {
				continue
			}
			found[face.fileName] = data
			_ = writeFontCacheAtomic(cacheDir, face.fileName, data)
		}
	}
	return found
}

// LoadCachedHarmonyFonts synchronously registers any verified HarmonyOS Sans SC
// weights already cached under <dataDir>/fonts (or matching local system font files).
func LoadCachedHarmonyFonts(dataDir string) (allPresent bool) {
	allPresent, _ = loadCachedHarmonyFonts(dataDir)
	return allPresent
}

func loadCachedHarmonyFonts(dataDir string) (allPresent bool, registeredAny bool) {
	if dataDir == "" {
		dataDir = AppDataDir()
	}
	cacheDir := filepath.Join(dataDir, "fonts")

	harmonyMu.Lock()
	var missing []harmonyFontFace
	for _, face := range harmonyFontFaces {
		if harmonyLoadedFace[face.fileName] {
			continue
		}
		cachedPath := filepath.Join(cacheDir, face.fileName)
		data, err := os.ReadFile(cachedPath)
		if err == nil && verifyHarmonyFontBytes(face, data) {
			if registerHarmonyFaceLocked(face, data) {
				registeredAny = true
			}
			continue
		}
		missing = append(missing, face)
	}
	harmonyMu.Unlock()

	if len(missing) > 0 {
		seeded := seedFromSystemFontDirs(cacheDir, missing)
		if len(seeded) > 0 {
			harmonyMu.Lock()
			var stillMissing []harmonyFontFace
			for _, face := range missing {
				if data, ok := seeded[face.fileName]; ok {
					if registerHarmonyFaceLocked(face, data) {
						registeredAny = true
					}
				} else if !harmonyLoadedFace[face.fileName] {
					stillMissing = append(stillMissing, face)
				}
			}
			missing = stillMissing
			harmonyMu.Unlock()
		}
	}

	if registeredAny {
		theme.BumpFontEpoch()
	}
	return len(missing) == 0, registeredAny
}

// EnsureHarmonyFontsAsync loads any cached HarmonyOS Sans SC faces immediately and,
// when weights are missing on disk, fetches them in the background from CDN mirrors
// into <dataDir>/fonts without bloating the application binary.
func EnsureHarmonyFontsAsync(dataDir string, onUpdated func()) {
	if dataDir == "" {
		dataDir = AppDataDir()
	}
	allPresent, registeredAny := loadCachedHarmonyFonts(dataDir)
	if registeredAny && onUpdated != nil {
		onUpdated()
	}
	if allPresent {
		return
	}
	harmonyMu.Lock()
	if harmonyDownloading {
		harmonyMu.Unlock()
		return
	}
	harmonyDownloading = true
	harmonyMu.Unlock()

	go func() {
		defer func() {
			harmonyMu.Lock()
			harmonyDownloading = false
			harmonyMu.Unlock()
		}()
		downloadMissingHarmonyFonts(dataDir, onUpdated)
	}()
}

func downloadMissingHarmonyFonts(dataDir string, onUpdated func()) {
	cacheDir := filepath.Join(dataDir, "fonts")
	client := &http.Client{Timeout: 25 * time.Second}

	missingFaces := func() []harmonyFontFace {
		harmonyMu.Lock()
		defer harmonyMu.Unlock()
		var out []harmonyFontFace
		for _, face := range harmonyFontFaces {
			if !harmonyLoadedFace[face.fileName] {
				out = append(out, face)
			}
		}
		return out
	}

	applyFace := func(face harmonyFontFace, data []byte) bool {
		if !verifyHarmonyFontBytes(face, data) {
			return false
		}
		if err := writeFontCacheAtomic(cacheDir, face.fileName, data); err != nil {
			LogWarnf("cache font %s failed: %v", face.fileName, err)
		}
		harmonyMu.Lock()
		ok := registerHarmonyFaceLocked(face, data)
		harmonyMu.Unlock()
		if ok {
			theme.BumpFontEpoch()
			if onUpdated != nil {
				onUpdated()
			}
		}
		return ok
	}

	for _, face := range missingFaces() {
		for _, base := range harmonyCDNBaseURLs {
			data, err := fetchFontBytes(client, base+face.fileName, face.size+1024)
			if err != nil {
				continue
			}
			if applyFace(face, data) {
				break
			}
		}
	}

	rem := missingFaces()
	if len(rem) == 0 {
		return
	}
	if err := fetchFromTarballMirror(client, rem, applyFace); err != nil {
		LogWarnf("harmonyos font CDN fallback incomplete: %v", err)
	}
}

func fetchFontBytes(client *http.Client, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
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
	client *http.Client,
	wanted []harmonyFontFace,
	applyFace func(harmonyFontFace, []byte) bool,
) error {
	wantByName := make(map[string]harmonyFontFace, len(wanted))
	for _, f := range wanted {
		wantByName[f.fileName] = f
	}
	tarClient := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, harmonyTarballMirrorURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Block-Desktop/1.0")
	resp, err := tarClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mirror HTTP %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(io.LimitReader(resp.Body, 48<<20))
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
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
