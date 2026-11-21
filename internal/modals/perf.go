package modals

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

var platformProcessStatsFn func() (cpu float64, rss uint64, ok bool)

// PerfBreakdownEntry describes a single largest file or grouped item within a PerfCategory.
type PerfBreakdownEntry struct {
	Name  string
	Path  string
	Bytes uint64
	Count int
}

// PerfCategory describes one of the 7 disk-usage categories in the Performance modal.
type PerfCategory struct {
	ID        string
	Label     string
	Detail    string
	Bytes     uint64
	FileCount int
	Color     ui.Color
	Entries   []PerfBreakdownEntry
}

// PerfSnapshotData captures live host process usage and the 7-category storage breakdown.
type PerfSnapshotData struct {
	CPUPercent float64
	RSSBytes   uint64
	TotalBytes uint64
	LastScan   string
	ScanError  string
	Categories []PerfCategory
}

type fileIdentity struct {
	dev uint64
	ino uint64
}

type perfExtraState struct {
	refreshing bool
	refreshGen uint64
}

func (m *Manager) perfExtra() *perfExtraState {
	return &m.perfState
}

var perfCategoryDefs = []struct {
	id       string
	zh, en   string
	dZh, dEn string
	hex      string
}{
	{"notes", "笔记", "Notes", "Markdown 文档", "Markdown documents", "#547dd0"},
	{"assets", "资源图片", "Assets & Images", "图片与资源文件", "Images & assets", "#3ba57b"},
	{"history", "历史快照", "History", "本地历史快照", "Version snapshots", "#a870bc"},
	{"recovery", "恢复缓存", "Recovery & Cache", "恢复缓存与临时文件", "Recovery & cache", "#8c8d96"},
	{"ai_sessions", "AI 会话", "AI Sessions", "对话记录与草稿", "Chats & drafts", "#d19348"},
	{"logs", "日志", "Logs", "应用运行日志", "Application logs", "#d16e6e"},
	{"config", "配置状态", "Config & State", "设置与状态数据", "Settings & state", "#6a9bab"},
}

func (m *Manager) initDefaultPerfCategories() {
	pal := theme.Ocean()
	cats := make([]PerfCategory, len(perfCategoryDefs))
	for i, d := range perfCategoryDefs {
		cats[i] = PerfCategory{
			ID:     d.id,
			Label:  theme.Tr(d.zh, d.en),
			Detail: theme.Tr(d.dZh, d.dEn),
			Color:  pal.PerfCategoryColor(d.id),
		}
	}
	m.perfMu.Lock()
	m.perfCategories = cats
	m.perfMu.Unlock()
}

// IsPerformanceOpen reports whether the Performance & Usage modal is open.
func (m *Manager) IsPerformanceOpen() bool {
	m.perfMu.Lock()
	defer m.perfMu.Unlock()
	return m.perfOpen
}

// OpenPerformance opens the Performance modal and triggers asynchronous CPU/RSS and disk sampling.
func (m *Manager) OpenPerformance() {
	ex := m.perfExtra()
	m.perfMu.Lock()
	if m.perfOpen {
		m.perfMu.Unlock()
		return
	}
	if m.perfCancel != nil {
		close(m.perfCancel)
	}
	cancel := make(chan struct{})
	m.perfCancel = cancel
	m.perfOpen = true
	m.perfOffset = ui.Point{}
	m.perfSamples = 0
	m.perfScanErr = ""
	ex.refreshGen++
	ex.refreshing = false
	m.perfMu.Unlock()

	m.triggerPerformanceRefresh()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-cancel:
				return
			case <-ticker.C:
				m.sampleProcessOnce()
				m.requestRedraw()
			}
		}
	}()
	m.requestRedraw()
}

// ClosePerformance closes the Performance modal and stops background sampling.
func (m *Manager) ClosePerformance() {
	ex := m.perfExtra()
	m.perfMu.Lock()
	if !m.perfOpen {
		m.perfMu.Unlock()
		return
	}
	m.perfOpen = false
	ex.refreshGen++
	ex.refreshing = false
	if m.perfCancel != nil {
		close(m.perfCancel)
		m.perfCancel = nil
	}
	m.perfMu.Unlock()
	m.requestRedraw()
}

func (m *Manager) triggerPerformanceRefresh() {
	ex := m.perfExtra()
	m.perfMu.Lock()
	if !m.perfOpen || ex.refreshing {
		m.perfMu.Unlock()
		return
	}
	ex.refreshing = true
	gen := ex.refreshGen
	dataDir := m.dataDir
	m.perfMu.Unlock()

	nbRoots := m.notebookRoots()

	go func() {
		defer func() {
			m.perfMu.Lock()
			if ex.refreshGen == gen {
				ex.refreshing = false
			}
			m.perfMu.Unlock()
		}()

		cpu, rss := readProcessStats()
		m.perfMu.Lock()
		if !m.perfOpen || ex.refreshGen != gen {
			m.perfMu.Unlock()
			return
		}
		cancel := m.perfCancel
		m.perfCPU = cpu
		m.perfRSS = rss
		m.perfSamples++
		m.perfMu.Unlock()
		m.requestRedraw()

		cats, lastScan, scanErr := scanPerformanceDisk("", nbRoots, dataDir, cancel)
		if isPerfScanCancelled(cancel) {
			return
		}
		m.perfMu.Lock()
		if !m.perfOpen || ex.refreshGen != gen {
			m.perfMu.Unlock()
			return
		}
		m.perfCategories = cats
		m.perfLastScan = lastScan
		m.perfScanErr = scanErr
		m.perfMu.Unlock()
		m.requestRedraw()
	}()
}

// PerformanceCategories returns a snapshot of the 7 disk categories with current labels,
// byte counts, file counts, and top breakdown entries.
func (m *Manager) PerformanceCategories() []PerfCategory {
	m.perfMu.Lock()
	defer m.perfMu.Unlock()
	out := make([]PerfCategory, len(perfCategoryDefs))
	byID := make(map[string]PerfCategory, len(m.perfCategories))
	for _, c := range m.perfCategories {
		byID[c.ID] = c
	}
	pal := theme.Ocean()
	for i, d := range perfCategoryDefs {
		prev := byID[d.id]
		entriesCopy := append([]PerfBreakdownEntry(nil), prev.Entries...)
		out[i] = PerfCategory{
			ID:        d.id,
			Label:     theme.Tr(d.zh, d.en),
			Detail:    theme.Tr(d.dZh, d.dEn),
			Bytes:     prev.Bytes,
			FileCount: prev.FileCount,
			Color:     pal.PerfCategoryColor(d.id),
			Entries:   entriesCopy,
		}
	}
	return out
}

// RefreshPerformanceDisk scans the 7 disk categories synchronously.
// When `overrideRoot` is non-empty, all files under `overrideRoot` are scanned and classified.
func (m *Manager) RefreshPerformanceDisk(overrideRoot string) {
	cats, lastScan, scanErr := scanPerformanceDisk(overrideRoot, m.notebookRoots(), m.dataDir, nil)

	m.perfMu.Lock()
	m.perfCategories = cats
	m.perfLastScan = lastScan
	m.perfScanErr = scanErr
	m.perfMu.Unlock()
	m.requestRedraw()
}

// PerfSnapshot samples the host process CPU/RSS and returns the 7-category disk breakdown,
// rescanning storage when refreshDisk is true or when no scan has completed yet.
func (m *Manager) PerfSnapshot(refreshDisk bool) PerfSnapshotData {
	cpu, rss := readProcessStats()

	m.perfMu.Lock()
	m.perfCPU = cpu
	m.perfRSS = rss
	m.perfSamples++
	needDisk := refreshDisk || m.perfLastScan == ""
	dataDir := m.dataDir
	m.perfMu.Unlock()

	if needDisk {
		cats, lastScan, scanErr := scanPerformanceDisk("", m.notebookRoots(), dataDir, nil)
		m.perfMu.Lock()
		m.perfCategories = cats
		m.perfLastScan = lastScan
		m.perfScanErr = scanErr
		m.perfMu.Unlock()
	}

	cats := m.PerformanceCategories()
	var totalBytes uint64
	for _, c := range cats {
		totalBytes += c.Bytes
	}

	m.perfMu.Lock()
	lastScan := m.perfLastScan
	scanErr := m.perfScanErr
	m.perfMu.Unlock()

	return PerfSnapshotData{
		CPUPercent: cpu,
		RSSBytes:   rss,
		TotalBytes: totalBytes,
		LastScan:   lastScan,
		ScanError:  scanErr,
		Categories: cats,
	}
}

const perfMaxBreakdownEntries = 6

type perfCategoryAccum struct {
	bytes     uint64
	fileCount int
	top       []PerfBreakdownEntry
	groups    map[string]*PerfBreakdownEntry
}

func newPerfAccum() []perfCategoryAccum {
	acc := make([]perfCategoryAccum, len(perfCategoryDefs))
	for i := range acc {
		acc[i].top = make([]PerfBreakdownEntry, 0, perfMaxBreakdownEntries)
	}
	return acc
}

func perfEntryBetter(a, b PerfBreakdownEntry) bool {
	if a.Bytes != b.Bytes {
		return a.Bytes > b.Bytes
	}
	if a.Count != b.Count {
		return a.Count > b.Count
	}
	return a.Name < b.Name
}

func insertTopPerfEntry(top *[]PerfBreakdownEntry, ent PerfBreakdownEntry, limit int) {
	if limit <= 0 {
		return
	}
	s := *top
	n := len(s)
	if n >= limit && !perfEntryBetter(ent, s[n-1]) {
		return
	}
	pos := n
	for i := 0; i < n; i++ {
		if perfEntryBetter(ent, s[i]) {
			pos = i
			break
		}
	}
	if n < limit {
		s = append(s, PerfBreakdownEntry{})
		copy(s[pos+1:], s[pos:n])
		s[pos] = ent
		*top = s
		return
	}
	copy(s[pos+1:], s[pos:n-1])
	s[pos] = ent
	*top = s
}

func (a *perfCategoryAccum) add(key, name, fullPath string, fileBytes uint64, grouped bool) {
	a.bytes += fileBytes
	a.fileCount++
	if key == "" {
		key = fullPath
	}
	if name == "" {
		name = filepath.Base(fullPath)
	}
	if grouped {
		if a.groups == nil {
			a.groups = make(map[string]*PerfBreakdownEntry)
		}
		if cur, ok := a.groups[key]; ok {
			cur.Bytes += fileBytes
			cur.Count++
			return
		}
		a.groups[key] = &PerfBreakdownEntry{
			Name:  name,
			Path:  fullPath,
			Bytes: fileBytes,
			Count: 1,
		}
		return
	}
	insertTopPerfEntry(&a.top, PerfBreakdownEntry{
		Name:  name,
		Path:  fullPath,
		Bytes: fileBytes,
		Count: 1,
	}, perfMaxBreakdownEntries)
}

func (a *perfCategoryAccum) topEntries(limit int) []PerfBreakdownEntry {
	if limit <= 0 || (len(a.top) == 0 && len(a.groups) == 0) {
		return nil
	}
	merged := make([]PerfBreakdownEntry, 0, limit)
	for _, ent := range a.top {
		insertTopPerfEntry(&merged, ent, limit)
	}
	for _, ent := range a.groups {
		insertTopPerfEntry(&merged, *ent, limit)
	}
	return merged
}

func loadHistoryPageLabels(dataDir string) map[string]string {
	out := make(map[string]string)
	if strings.TrimSpace(dataDir) == "" {
		return out
	}
	for _, dirName := range []string{"History", "history"} {
		raw, err := os.ReadFile(filepath.Join(dataDir, dirName, "index.json"))
		if err != nil {
			continue
		}
		var doc struct {
			Pages map[string]string `json:"pages"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		for origPath, pageID := range doc.Pages {
			id := strings.TrimSpace(pageID)
			p := strings.TrimSpace(origPath)
			if id != "" && p != "" {
				out[id] = p
			}
		}
		if len(out) > 0 {
			break
		}
	}
	return out
}

func shortDisplayPath(fullPath string) string {
	slash := filepath.ToSlash(filepath.Clean(fullPath))
	parts := strings.Split(slash, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return filepath.Base(fullPath)
}

func describeBreakdownEntry(idx int, root, fullPath, prefix string, historyLabels map[string]string) (key, name, tipPath string, grouped bool) {
	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		rel = filepath.Base(fullPath)
	}
	relSlash := filepath.ToSlash(rel)
	parts := strings.Split(relSlash, "/")

	switch idx {
	case 2: // history: aggregate per page folder and resolve note path via History/index.json
		if len(parts) >= 2 && strings.EqualFold(parts[0], "history") {
			pageID := parts[1]
			if orig, ok := historyLabels[pageID]; ok {
				return "history:" + pageID, shortDisplayPath(orig), orig, true
			}
			if len(parts) >= 3 {
				return "history:" + pageID, parts[0] + "/" + pageID, filepath.Join(root, parts[0], pageID), true
			}
		}
	case 6: // config: group plugin installs/data per plugin while keeping fonts/json files granular
		if len(parts) >= 3 && (strings.EqualFold(parts[0], "apps") || strings.EqualFold(parts[0], "apps-data")) {
			group := parts[0] + "/" + parts[1]
			return "config:" + group, group, filepath.Join(root, parts[0], parts[1]), true
		}
	}

	display := relSlash
	if prefix != "" {
		display = prefix + "/" + relSlash
	}
	return fullPath, display, fullPath, false
}

func isPerfScanCancelled(cancel <-chan struct{}) bool {
	if cancel == nil {
		return false
	}
	select {
	case <-cancel:
		return true
	default:
		return false
	}
}

var perfIgnoredNotebookDirs = map[string]bool{
	".git":         true,
	".hg":          true,
	".svn":         true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"__pycache__":  true,
	".next":        true,
	".nuxt":        true,
	".venv":        true,
	"venv":         true,
}

func shouldSkipPerfNotebookDir(name string) bool {
	return perfIgnoredNotebookDirs[name]
}

func scanPerformanceDisk(overrideRoot string, nbRoots []string, dataDir string, cancel <-chan struct{}) (cats []PerfCategory, lastScan, scanErr string) {
	acc := newPerfAccum()
	seen := make(map[fileIdentity]struct{})
	var errs []string
	historyLabels := loadHistoryPageLabels(dataDir)

	if strings.TrimSpace(overrideRoot) != "" {
		absRoot, err := filepath.Abs(overrideRoot)
		if err != nil {
			errs = append(errs, err.Error())
		} else {
			scanTree(absRoot, cancel, nil, seen, &errs, func(path string) int {
				return classifyOverridePath(absRoot, path)
			}, func(idx int, path string) (string, string, string, bool) {
				return describeBreakdownEntry(idx, absRoot, path, "", historyLabels)
			}, acc)
		}
	} else {
		multiNB := len(nbRoots) > 1
		// 1. Notebooks (notes vs assets)
		for _, nb := range nbRoots {
			if isPerfScanCancelled(cancel) {
				break
			}
			nbPrefix := ""
			if multiNB {
				nbPrefix = filepath.Base(filepath.Clean(nb))
			}
			scanTree(nb, cancel, shouldSkipPerfNotebookDir, seen, &errs, func(path string) int {
				if isMarkdownPath(path) {
					return 0 // notes
				}
				return 1 // assets
			}, func(idx int, path string) (string, string, string, bool) {
				return describeBreakdownEntry(idx, nb, path, nbPrefix, historyLabels)
			}, acc)
		}
		// 2. Application data directory (history, recovery, ai_sessions, logs, config)
		if !isPerfScanCancelled(cancel) && strings.TrimSpace(dataDir) != "" {
			scanTree(dataDir, cancel, nil, seen, &errs, func(path string) int {
				return classifyDataDirPath(dataDir, path)
			}, func(idx int, path string) (string, string, string, bool) {
				return describeBreakdownEntry(idx, dataDir, path, "", historyLabels)
			}, acc)
		}
		// 3. OS cache directory
		if !isPerfScanCancelled(cancel) {
			if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
				blockCache := filepath.Join(cacheDir, "Block")
				if _, stErr := os.Lstat(blockCache); stErr == nil {
					scanTree(blockCache, cancel, nil, seen, &errs, func(_ string) int {
						return 3 // recovery
					}, func(idx int, path string) (string, string, string, bool) {
						return describeBreakdownEntry(idx, blockCache, path, "Cache", historyLabels)
					}, acc)
				}
			}
		}
	}

	pal := theme.Ocean()
	cats = make([]PerfCategory, len(perfCategoryDefs))
	for i, d := range perfCategoryDefs {
		cats[i] = PerfCategory{
			ID:        d.id,
			Label:     theme.Tr(d.zh, d.en),
			Detail:    theme.Tr(d.dZh, d.dEn),
			Bytes:     acc[i].bytes,
			FileCount: acc[i].fileCount,
			Color:     pal.PerfCategoryColor(d.id),
			Entries:   acc[i].topEntries(perfMaxBreakdownEntries),
		}
	}
	lastScan = time.Now().Format("15:04:05")
	if len(errs) > 0 {
		scanErr = strings.Join(errs, "; ")
	}
	return cats, lastScan, scanErr
}

func (m *Manager) notebookRoots() []string {
	st := m.currentState()
	return m.knownNotebooks(st)
}

func scanTree(
	root string,
	cancel <-chan struct{},
	skipDir func(name string) bool,
	seen map[fileIdentity]struct{},
	errs *[]string,
	classify func(path string) int,
	describe func(idx int, path string) (key, name, tipPath string, grouped bool),
	acc []perfCategoryAccum,
) {
	if isPerfScanCancelled(cancel) {
		return
	}
	info, err := os.Lstat(root)
	if err != nil {
		if !os.IsNotExist(err) {
			*errs = append(*errs, err.Error())
		}
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return
	}
	if !info.IsDir() {
		addFileBytes(root, info, seen, classify, describe, acc)
		return
	}

	stack := []string{root}
	for len(stack) > 0 {
		if isPerfScanCancelled(cancel) {
			return
		}
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			*errs = append(*errs, readErr.Error())
			continue
		}
		for _, ent := range entries {
			if isPerfScanCancelled(cancel) {
				return
			}
			typ := ent.Type()
			if typ&os.ModeSymlink != 0 {
				continue
			}
			name := ent.Name()
			full := filepath.Join(dir, name)
			if typ.IsDir() {
				if skipDir != nil && skipDir(name) {
					continue
				}
				stack = append(stack, full)
				continue
			}
			if typ.IsRegular() {
				st, stErr := ent.Info()
				if stErr != nil {
					*errs = append(*errs, stErr.Error())
					continue
				}
				addFileBytes(full, st, seen, classify, describe, acc)
				continue
			}
			if typ == 0 {
				st, stErr := os.Lstat(full)
				if stErr != nil {
					*errs = append(*errs, stErr.Error())
					continue
				}
				if st.Mode()&os.ModeSymlink != 0 {
					continue
				}
				if st.IsDir() {
					if skipDir != nil && skipDir(name) {
						continue
					}
					stack = append(stack, full)
				} else if st.Mode().IsRegular() {
					addFileBytes(full, st, seen, classify, describe, acc)
				}
			}
		}
	}
}

func addFileBytes(
	path string,
	info os.FileInfo,
	seen map[fileIdentity]struct{},
	classify func(string) int,
	describe func(idx int, path string) (key, name, tipPath string, grouped bool),
	acc []perfCategoryAccum,
) {
	id, hasID, fileBytes := statPhysicalFile(info)
	if hasID {
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
	}

	idx := classify(path)
	if idx >= 0 && idx < len(acc) {
		key, name, tipPath, grouped := describe(idx, path)
		acc[idx].add(key, name, tipPath, fileBytes, grouped)
	}
}

func classifyOverridePath(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	slash := strings.ToLower(filepath.ToSlash(rel))
	parts := strings.Split(slash, "/")
	base := parts[len(parts)-1]

	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "history":
			return 2
		case "recovery", "cache", "quick":
			return 3
		case "ai", "ai_sessions", "ai-sessions":
			return 4
		case "logs", "log":
			return 5
		case "config":
			return 6
		case "assets", "images", "attachments":
			return 1
		}
	}

	switch base {
	case "ai-chats.json", "ai-active-draft.json", "ai-failed-drafts.json", "ai.json", "ai-backends.json":
		return 4
	case "state.json", "settings.json", "window-state.json":
		return 6
	}
	if strings.HasSuffix(base, ".log") {
		return 5
	}
	if isMarkdownPath(base) {
		return 0
	}
	return 1
}

func classifyDataDirPath(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	slash := strings.ToLower(filepath.ToSlash(rel))
	parts := strings.Split(slash, "/")
	base := parts[len(parts)-1]

	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "history":
			return 2
		case "recovery", "cache", "quick":
			return 3
		case "logs", "log":
			return 5
		}
	}

	switch base {
	case "ai-chats.json", "ai-active-draft.json", "ai-failed-drafts.json", "ai.json", "ai-backends.json":
		return 4
	}
	if strings.HasSuffix(base, ".log") {
		return 5
	}
	return 6
}

func isMarkdownPath(path string) bool {
	return files.IsMarkdown(path)
}

func (m *Manager) sampleProcessOnce() {
	cpu, rss := readProcessStats()
	m.perfMu.Lock()
	m.perfCPU = cpu
	m.perfRSS = rss
	m.perfSamples++
	m.perfMu.Unlock()
}

func readProcessStats() (cpu float64, rss uint64) {
	if platformProcessStatsFn != nil {
		if pCPU, pRSS, ok := platformProcessStatsFn(); ok {
			cpu = pCPU
			rss = pRSS
		}
	}
	if rss == 0 {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		rss = ms.Sys
	}
	return cpu, rss
}

func (m *Manager) renderPerformanceModal(c *ui.Context) {
	pal := theme.Ocean()
	open := true
	title := theme.Tr("性能与占用", "Performance & Usage")

	cats := m.PerformanceCategories()
	m.perfMu.Lock()
	cpu := m.perfCPU
	rss := m.perfRSS
	lastScan := m.perfLastScan
	scanErr := m.perfScanErr
	m.perfMu.Unlock()

	var totalBytes uint64
	treemapItems := make([]internalui.TreemapItem, len(cats))
	for i, cat := range cats {
		totalBytes += cat.Bytes
		entries := make([]internalui.TreemapEntry, len(cat.Entries))
		for ei, ent := range cat.Entries {
			entries[ei] = internalui.TreemapEntry{
				Name:  ent.Name,
				Path:  ent.Path,
				Bytes: ent.Bytes,
				Count: ent.Count,
			}
		}
		treemapItems[i] = internalui.TreemapItem{
			ID:        cat.ID,
			ColorKey:  cat.ID,
			Label:     cat.Label,
			Detail:    cat.Detail,
			Bytes:     cat.Bytes,
			FileCount: cat.FileCount,
			Color:     cat.Color,
			Entries:   entries,
		}
	}
	if lastScan == "" {
		lastScan = "—"
	}

	winW, winH := c.Size()
	limitX := max((winW-720)/2, 0) + 120
	limitY := max((winH-520)/2, 20)
	clampOffset := func() {
		m.perfOffset.X = max(-limitX, min(limitX, m.perfOffset.X))
		m.perfOffset.Y = max(-limitY, min(limitY, m.perfOffset.Y))
	}
	clampOffset()
	internalui.Dialog(c, "perf-modal", title, &open, internalui.ModalLayout{Width: 720, MaxHeight: 620, Centered: true, Offset: m.perfOffset}, func() {
		// Header bar.
		header := ui.Row(c.Key("perf-header")).Cursor(ui.CursorGrab).FillWidth().Height(46).PaddingX(20).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).
			BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
		if dx, dy, held := header.Dragged(); held {
			header.Cursor(ui.CursorGrabbing)
			m.perfOffset.X += dx
			m.perfOffset.Y += dy
			clampOffset()
			if dx != 0 || dy != 0 {
				m.requestRedraw()
			}
		}
		header.Children(func() {
			ui.Text(c, title).FontSize(theme.FontTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ref := internalui.IconButton(c, icons.Refresh, internalui.IconButtonOptions{
					ID:      "perf-refresh",
					Label:   theme.Tr("刷新磁盘统计", "Refresh Disk Usage"),
					Tooltip: theme.Tr("刷新", "Refresh"),
				})
				if ref.Clicked() {
					m.triggerPerformanceRefresh()
				}
				cls := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
					ID:      "perf-close",
					Label:   theme.Tr("关闭", "Close"),
					Tooltip: theme.Tr("关闭", "Close"),
				})
				if cls.Clicked() {
					m.ClosePerformance()
				}
			})
		})

		// Metrics bar (Storage, Memory RSS, CPU, Scan time).
		ui.Row(c.Key("perf-metrics")).FillWidth().MinHeight(38).Padding(6, 20).Wrap().Gap(12).
			Background(pal.Sidebar).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Row(c).Gap(20).Wrap().AlignItems(ui.Center).Children(func() {
				perfMetric(c, theme.Tr("磁盘占用", "Storage"), internalui.FormatBytesIEC(totalBytes))
				perfMetric(c, theme.Tr("物理内存", "Memory RSS"), internalui.FormatBytesIEC(rss))
				perfMetric(c, "CPU", fmt.Sprintf("%.1f%%", cpu))
			})
			perfMetric(c, theme.Tr("统计时间", "Scanned"), lastScan)
		})

		// Body: Interactive Squarified Treemap drawn via ui.Painter + clickable category rows.
		ui.Column(c).FillWidth().Padding(16, 20, 18, 20).Gap(12).Children(func() {
			if scanErr != "" {
				ui.Text(c, theme.Tr("部分文件未能读取：", "Some files could not be read: ")+scanErr).
					FontSize(theme.FontBody).TextColor(pal.Warning)
			}

			internalui.RenderStorageTreemap(c, &m.perfTreemap, treemapItems, totalBytes, internalui.TreemapOptions{
				CanvasKey:       "perf-treemap",
				CanvasProbeID:   "perf-treemap",
				DetailKey:       "perf-detail-bar",
				TileProbePrefix: "perf-tile-",
				Height:          256,
				OnSelect: func(_ string) {
					m.requestRedraw()
				},
			})
		})
	})

	if !open {
		m.ClosePerformance()
	}
}

func perfMetric(c *ui.Context, label, value string) {
	pal := theme.Ocean()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
		ui.Text(c, value).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text)
	})
}
