package files

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"block/internal/model"
	"block/internal/theme"
)

const maxNotebooks = 12

var (
	stateMu             sync.Mutex
	stateTempSeq        atomic.Uint64
	cleanStateTempsOnce sync.Once
)

func stateFilePath() string {
	return filepath.Join(AppDataDir(), "state.json")
}

func defaultState() model.AppState {
	return model.DefaultAppState(theme.SystemLanguage())
}

func normalizeState(st *model.AppState) {
	if st.Theme == "" {
		st.Theme = model.ThemeSystem
	}
	if st.Language == "" {
		st.Language = theme.SystemLanguage()
	}
	if st.FontFamily == "" {
		st.FontFamily = model.FontSans
	}
	if st.LineHeight == "" {
		st.LineHeight = model.LineHeightNormal
	}
	switch st.OutlineView {
	case model.OutlineViewAuto, model.OutlineViewCard, model.OutlineViewList:
	default:
		st.OutlineView = model.OutlineViewAuto
	}
	if st.AssetsFolder == "" {
		st.AssetsFolder = "assets"
	}
	if st.HideAssetsFolder == nil {
		v := true
		st.HideAssetsFolder = &v
	}
	if st.HideNonMarkdownFiles == nil {
		v := true
		st.HideNonMarkdownFiles = &v
	}
	if st.EditorZoom != nil {
		v := float64(*st.EditorZoom)
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			st.EditorZoom = nil
		} else {
			var pct int
			if v > 10 {
				pct = int(math.Round(v))
			} else {
				pct = int(math.Round(v * 100))
			}
			pct = max(70, min(((pct+5)/10)*10, 130))
			z := float32(pct) / 100.0
			st.EditorZoom = &z
		}
	}
	if st.MainWindowBounds != nil {
		b := st.MainWindowBounds
		invalid := false
		for _, f := range []float32{b.X, b.Y, b.Width, b.Height} {
			if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
				invalid = true
				break
			}
		}
		if invalid || b.Width <= 0 || b.Height <= 0 {
			st.MainWindowBounds = nil
		}
	}

	// Deduplicate notebooks preserving order and skipping empty entries.
	dedup := make([]string, 0, len(st.Notebooks)+1)
	seen := make(map[string]bool, len(st.Notebooks)+1)
	if st.Folder != "" {
		cleanFolder := filepath.Clean(st.Folder)
		st.Folder = cleanFolder
	}

	for _, nb := range st.Notebooks {
		nb = strings.TrimSpace(nb)
		if nb == "" {
			continue
		}
		clean := filepath.Clean(nb)
		if seen[clean] {
			continue
		}
		seen[clean] = true
		dedup = append(dedup, clean)
	}

	if st.Folder != "" && !seen[st.Folder] {
		dedup = append([]string{st.Folder}, dedup...)
	}
	if len(dedup) > maxNotebooks {
		dedup = dedup[:maxNotebooks]
	}
	st.Notebooks = dedup

	if len(st.NotebookIdentities) > 0 {
		validNotebooks := make(map[string]bool, len(st.Notebooks)+1)
		for _, nb := range st.Notebooks {
			validNotebooks[nb] = true
		}
		if st.Folder != "" {
			validNotebooks[st.Folder] = true
		}
		cleanIDs := make(map[string]string, len(st.NotebookIdentities))
		for rawPath, rawID := range st.NotebookIdentities {
			p := strings.TrimSpace(rawPath)
			id := strings.TrimSpace(rawID)
			if p == "" || id == "" {
				continue
			}
			cleanPath := filepath.Clean(p)
			if !validNotebooks[cleanPath] {
				continue
			}
			if _, exists := cleanIDs[cleanPath]; !exists || rawPath == cleanPath {
				cleanIDs[cleanPath] = id
			}
		}
		if len(cleanIDs) == 0 {
			st.NotebookIdentities = nil
		} else {
			st.NotebookIdentities = cleanIDs
		}
	} else if st.NotebookIdentities != nil {
		st.NotebookIdentities = nil
	}

	// Deduplicate open tabs preserving order and skipping empty entries.
	if len(st.OpenTabs) > 0 {
		cleanTabs := make([]string, 0, len(st.OpenTabs))
		seenTabs := make(map[string]bool, len(st.OpenTabs))
		for _, t := range st.OpenTabs {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			clean := filepath.Clean(t)
			if seenTabs[clean] {
				continue
			}
			seenTabs[clean] = true
			cleanTabs = append(cleanTabs, clean)
		}
		st.OpenTabs = cleanTabs
	}
	st.ActiveTab = strings.TrimSpace(st.ActiveTab)
	if st.ActiveTab != "" {
		st.ActiveTab = filepath.Clean(st.ActiveTab)
	}
	if len(st.OpenTabs) == 0 {
		st.ActiveTab = ""
	} else if st.ActiveTab != "" {
		foundActive := false
		for _, t := range st.OpenTabs {
			if t == st.ActiveTab {
				foundActive = true
				break
			}
		}
		if !foundActive {
			st.ActiveTab = st.OpenTabs[0]
		}
	}

	if len(st.PinnedPluginButtons) > 0 {
		cleanPinned := make([]string, 0, len(st.PinnedPluginButtons))
		seenPinned := make(map[string]bool, len(st.PinnedPluginButtons))
		for _, id := range st.PinnedPluginButtons {
			id = strings.TrimSpace(id)
			if id == "" || seenPinned[id] {
				continue
			}
			seenPinned[id] = true
			cleanPinned = append(cleanPinned, id)
			if len(cleanPinned) >= 3 {
				break
			}
		}
		st.PinnedPluginButtons = cleanPinned
	}

	if st.BrandDeco.Motion != "" {
		if st.BrandDeco.Density == "" && st.BrandDeco.Motion == model.BrandDecoMotionCalm {
			st.BrandDeco.Density = model.BrandDecoDensityCalm
		}
		st.BrandDeco.Motion = ""
	}
}

func loadStateLocked(path string) (model.AppState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultState(), nil
		}
		return defaultState(), err
	}
	st := defaultState()
	// Existing state files omit BrandDeco.Mode when it is BrandDecoByTime (""),
	// so reset BrandDeco before decoding to preserve existing users' preferences
	// and allow fresh users who later enable it to keep it across restarts.
	st.BrandDeco = model.BrandDeco{}
	if err := json.Unmarshal(data, &st); err != nil {
		corruptPath := path + ".corrupt"
		if renameErr := os.Rename(path, corruptPath); renameErr != nil {
			LogWarnf("failed to backup corrupt state %s: %v", path, renameErr)
		} else {
			LogWarnf("corrupt state file backed up to %s: %v", corruptPath, err)
		}
		return defaultState(), nil
	}
	normalizeState(&st)
	return st, nil
}

// LoadState reads state.json from AppDataDir, backing up corrupt files to
// state.json.corrupt and falling back to defaults when missing or unreadable.
func LoadState() model.AppState {
	stateMu.Lock()
	defer stateMu.Unlock()

	st, err := loadStateLocked(stateFilePath())
	if err != nil {
		LogWarnf("load state failed: %v", err)
		return defaultState()
	}
	return st
}

// SaveState normalizes and atomically writes st to state.json.
func SaveState(st model.AppState) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	normalizeState(&st)
	return writeStateAtomicLocked(stateFilePath(), &st)
}

// UpdateState performs a locked read-modify-write cycle on state.json.
func UpdateState(fn func(*model.AppState)) (model.AppState, error) {
	stateMu.Lock()
	defer stateMu.Unlock()

	path := stateFilePath()
	st, err := loadStateLocked(path)
	if err != nil {
		LogWarnf("load state before update failed: %v", err)
		st = defaultState()
	}
	if fn != nil {
		fn(&st)
	}
	normalizeState(&st)
	if err := writeStateAtomicLocked(path, &st); err != nil {
		return st, err
	}
	return st, nil
}

func writeStateAtomicLocked(path string, st *model.AppState) error {
	parent := filepath.Dir(path)
	if parent == "" || parent == "." {
		return errors.New(theme.Tr("无效的偏好路径", "Invalid preferences path"))
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')

	var (
		tmpPath string
		tmpFile *os.File
	)
	pid := os.Getpid()
	for range 64 {
		seq := stateTempSeq.Add(1)
		candidate := filepath.Join(parent, fmt.Sprintf(".state-%d-%d.tmp", pid, seq))
		f, openErr := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if openErr == nil {
			tmpPath = candidate
			tmpFile = f
			break
		}
		if !errors.Is(openErr, os.ErrExist) {
			return openErr
		}
	}
	if tmpFile == nil {
		return errors.New(theme.Tr("无法创建临时偏好文件", "Failed to create temporary preferences file"))
	}

	commitErr := func() error {
		if _, err := tmpFile.Write(bytes); err != nil {
			_ = tmpFile.Close()
			return err
		}
		if err := tmpFile.Sync(); err != nil {
			_ = tmpFile.Close()
			return err
		}
		if err := tmpFile.Close(); err != nil {
			return err
		}
		if err := os.Rename(tmpPath, path); err != nil {
			return err
		}
		if dir, err := os.Open(parent); err == nil {
			_ = dir.Sync()
			_ = dir.Close()
		}
		return nil
	}()
	if commitErr != nil {
		_ = os.Remove(tmpPath)
	}

	cleanStateTempsOnce.Do(func() {
		cleanStaleStateTemps(parent)
	})
	return commitErr
}

func cleanStaleStateTemps(parent string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".state-") && strings.HasSuffix(name, ".tmp") {
			if info, err := entry.Info(); err == nil {
				if now.Sub(info.ModTime()) > 60*time.Second {
					_ = os.Remove(filepath.Join(parent, name))
				}
			}
		}
	}
}
