package modals

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// PerfCategory describes one of the 7 disk-usage categories in the Performance modal.
type PerfCategory struct {
	ID     string
	Label  string
	Detail string
	Bytes  uint64
	Color  ui.Color
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
		m.perfCPU = cpu
		m.perfRSS = rss
		m.perfSamples++
		m.perfMu.Unlock()
		m.requestRedraw()

		cats, lastScan, scanErr := scanPerformanceDisk("", nbRoots, dataDir)
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

// PerformanceCategories returns a snapshot of the 7 disk categories with current labels and byte counts.
func (m *Manager) PerformanceCategories() []PerfCategory {
	m.perfMu.Lock()
	defer m.perfMu.Unlock()
	out := make([]PerfCategory, len(perfCategoryDefs))
	byID := make(map[string]uint64, len(m.perfCategories))
	for _, c := range m.perfCategories {
		byID[c.ID] = c.Bytes
	}
	pal := theme.Ocean()
	for i, d := range perfCategoryDefs {
		out[i] = PerfCategory{
			ID:     d.id,
			Label:  theme.Tr(d.zh, d.en),
			Detail: theme.Tr(d.dZh, d.dEn),
			Bytes:  byID[d.id],
			Color:  pal.PerfCategoryColor(d.id),
		}
	}
	return out
}

// RefreshPerformanceDisk scans the 7 disk categories synchronously.
// When `overrideRoot` is non-empty, all files under `overrideRoot` are scanned and classified.
func (m *Manager) RefreshPerformanceDisk(overrideRoot string) {
	cats, lastScan, scanErr := scanPerformanceDisk(overrideRoot, m.notebookRoots(), m.dataDir)

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
		cats, lastScan, scanErr := scanPerformanceDisk("", m.notebookRoots(), dataDir)
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

func scanPerformanceDisk(overrideRoot string, nbRoots []string, dataDir string) (cats []PerfCategory, lastScan, scanErr string) {
	totals := make([]uint64, len(perfCategoryDefs))
	seen := make(map[fileIdentity]struct{})
	var errs []string

	if strings.TrimSpace(overrideRoot) != "" {
		absRoot, err := filepath.Abs(overrideRoot)
		if err != nil {
			errs = append(errs, err.Error())
		} else {
			scanTree(absRoot, seen, &errs, func(path string) int {
				return classifyOverridePath(absRoot, path)
			}, totals)
		}
	} else {
		// 1. Notebooks (notes vs assets)
		for _, nb := range nbRoots {
			scanTree(nb, seen, &errs, func(path string) int {
				if isMarkdownPath(path) {
					return 0 // notes
				}
				return 1 // assets
			}, totals)
		}
		// 2. Application data directory (history, recovery, ai_sessions, logs, config)
		if strings.TrimSpace(dataDir) != "" {
			scanTree(dataDir, seen, &errs, func(path string) int {
				return classifyDataDirPath(dataDir, path)
			}, totals)
		}
		// 3. OS cache directory
		if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
			blockCache := filepath.Join(cacheDir, "Block")
			if _, stErr := os.Lstat(blockCache); stErr == nil {
				scanTree(blockCache, seen, &errs, func(_ string) int {
					return 3 // recovery
				}, totals)
			}
		}
	}

	pal := theme.Ocean()
	cats = make([]PerfCategory, len(perfCategoryDefs))
	for i, d := range perfCategoryDefs {
		cats[i] = PerfCategory{
			ID:     d.id,
			Label:  theme.Tr(d.zh, d.en),
			Detail: theme.Tr(d.dZh, d.dEn),
			Bytes:  totals[i],
			Color:  pal.PerfCategoryColor(d.id),
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

func scanTree(root string, seen map[fileIdentity]struct{}, errs *[]string, classify func(path string) int, totals []uint64) {
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
		addFileBytes(root, info, seen, classify, totals)
		return
	}

	stack := []string{root}
	for len(stack) > 0 {
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			*errs = append(*errs, readErr.Error())
			continue
		}
		for _, ent := range entries {
			typ := ent.Type()
			if typ&os.ModeSymlink != 0 {
				continue
			}
			full := filepath.Join(dir, ent.Name())
			if typ.IsDir() {
				stack = append(stack, full)
				continue
			}
			if typ.IsRegular() {
				st, stErr := ent.Info()
				if stErr != nil {
					*errs = append(*errs, stErr.Error())
					continue
				}
				addFileBytes(full, st, seen, classify, totals)
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
					stack = append(stack, full)
				} else if st.Mode().IsRegular() {
					addFileBytes(full, st, seen, classify, totals)
				}
			}
		}
	}
}

func addFileBytes(path string, info os.FileInfo, seen map[fileIdentity]struct{}, classify func(string) int, totals []uint64) {
	id, hasID, fileBytes := statPhysicalFile(info)
	if hasID {
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
	}

	idx := classify(path)
	if idx >= 0 && idx < len(totals) {
		totals[idx] += fileBytes
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
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		out, err := exec.Command("ps", "-p", strconv.Itoa(os.Getpid()), "-o", "%cpu=,rss=").Output()
		if err == nil {
			fields := strings.Fields(string(out))
			if len(fields) >= 2 {
				if v, pErr := strconv.ParseFloat(fields[0], 64); pErr == nil && !math.IsNaN(v) {
					cpu = v
				}
				if kb, pErr := strconv.ParseUint(fields[1], 10, 64); pErr == nil {
					rss = kb * 1024
				}
			}
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
		treemapItems[i] = internalui.TreemapItem{
			ID:     cat.ID,
			Label:  cat.Label,
			Detail: cat.Detail,
			Bytes:  cat.Bytes,
			Color:  cat.Color,
		}
	}
	if lastScan == "" {
		lastScan = "—"
	}

	winW, winH := c.Size()
	limitX := max((winW-720)/2, 0) + 120
	limitY := max((winH-380)/2, 20)
	clampOffset := func() {
		m.perfOffset.X = max(-limitX, min(limitX, m.perfOffset.X))
		m.perfOffset.Y = max(-limitY, min(limitY, m.perfOffset.Y))
	}
	clampOffset()
	internalui.Dialog(c, "perf-modal", title, &open, internalui.ModalLayout{Width: 720, MaxHeight: 440, Centered: true, Offset: m.perfOffset}, func() {
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
				LegendKey:       "perf-legend",
				TileProbePrefix: "perf-tile-",
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
		ui.Text(c, value).FontSize(theme.FontBody).FontWeight(theme.WeightTitle).TextColor(pal.Text)
	})
}
