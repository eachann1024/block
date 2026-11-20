package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"block/internal/files"
)

const fileAssocStateVersion = 1

var builtinMarkdownExtensions = []string{
	".md", ".markdown", ".mdown", ".mkd", ".mkdn", ".mdwn", ".mdtxt", ".mdtext", ".mdx",
}

var assocExtRe = regexp.MustCompile(`^\.[a-z0-9][a-z0-9._+-]{0,31}$`)

type fileAssocState struct {
	Version         int      `json:"version"`
	AppPath         string   `json:"app_path,omitempty"`
	ExecutablePath  string   `json:"executable_path,omitempty"`
	ExecutableSize  int64    `json:"executable_size,omitempty"`
	ExecutableMtime int64    `json:"executable_mtime,omitempty"`
	Extensions      []string `json:"extensions"`
	CustomMIMEs     []string `json:"custom_mimes,omitempty"`
}

type fileAssocRequest struct {
	appDataDir string
	extensions []string
}

var (
	fileAssocEnabled atomic.Bool

	fileAssocMu      sync.Mutex
	fileAssocCond    = sync.NewCond(&fileAssocMu)
	fileAssocRunning bool
	fileAssocHasPend bool
	fileAssocPend    fileAssocRequest
)

// SetFileAssociationSyncEnabled controls whether active language extension
// changes synchronize OS "Open With" file associations. It is disabled by
// default so unit tests and isolated runs never mutate host OS registrations.
func SetFileAssociationSyncEnabled(enabled bool) {
	fileAssocEnabled.Store(enabled)
}

// WaitFileAssociationSync blocks until any queued OS file association sync
// finishes. Useful for diagnostics or controlled shutdown.
func WaitFileAssociationSync() {
	fileAssocMu.Lock()
	for fileAssocRunning || fileAssocHasPend {
		fileAssocCond.Wait()
	}
	fileAssocMu.Unlock()
}

func normalizeAssocExtension(raw string) (string, bool) {
	ext := strings.ToLower(strings.TrimSpace(raw))
	if !assocExtRe.MatchString(ext) {
		return "", false
	}
	return ext, true
}

func collectPluginAssocExtensions(snap *languageSnapshot) []string {
	if snap == nil || len(snap.byExt) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(snap.byExt))
	out := make([]string, 0, len(snap.byExt))
	for raw := range snap.byExt {
		ext, ok := normalizeAssocExtension(raw)
		if !ok {
			continue
		}
		if _, dup := seen[ext]; dup {
			continue
		}
		seen[ext] = struct{}{}
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

func scheduleFileAssociationSync(appDataDir string, snap *languageSnapshot) {
	if !fileAssocEnabled.Load() {
		return
	}
	dir := strings.TrimSpace(appDataDir)
	if dir == "" {
		return
	}
	exts := collectPluginAssocExtensions(snap)

	fileAssocMu.Lock()
	fileAssocPend = fileAssocRequest{
		appDataDir: dir,
		extensions: exts,
	}
	fileAssocHasPend = true
	if !fileAssocRunning {
		fileAssocRunning = true
		go runFileAssociationWorker()
	}
	fileAssocMu.Unlock()
}

func runFileAssociationWorker() {
	for {
		fileAssocMu.Lock()
		if !fileAssocHasPend {
			fileAssocRunning = false
			fileAssocCond.Broadcast()
			fileAssocMu.Unlock()
			return
		}
		req := fileAssocPend
		fileAssocHasPend = false
		fileAssocMu.Unlock()

		if err := syncPlatformFileAssociations(req.appDataDir, req.extensions); err != nil {
			files.LogWarnf("file association sync failed: %v", err)
		}
	}
}

func fileAssocStatePath(appDataDir string) string {
	return filepath.Join(appDataDir, "file-associations.json")
}

func loadFileAssocState(appDataDir string) (fileAssocState, bool) {
	raw, err := os.ReadFile(fileAssocStatePath(appDataDir))
	if err != nil {
		return fileAssocState{}, false
	}
	var st fileAssocState
	if err := json.Unmarshal(raw, &st); err != nil {
		return fileAssocState{}, false
	}
	return st, true
}

func saveFileAssocState(appDataDir string, st fileAssocState) error {
	st.Version = fileAssocStateVersion
	if st.Extensions == nil {
		st.Extensions = []string{}
	}
	if err := os.MkdirAll(appDataDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	target := fileAssocStatePath(appDataDir)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func unionSortedStrings(groups ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, g := range groups {
		for _, item := range g {
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
