package modals

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

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

type treemapTileRect struct {
	cat  PerfCategory
	rect ui.Rect
}

type fileIdentity struct {
	dev uint64
	ino uint64
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
	cats := make([]PerfCategory, len(perfCategoryDefs))
	for i, d := range perfCategoryDefs {
		cats[i] = PerfCategory{
			ID:     d.id,
			Label:  theme.Tr(d.zh, d.en),
			Detail: theme.Tr(d.dZh, d.dEn),
			Color:  ui.Hex(d.hex),
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

// OpenPerformance opens the Performance modal, samples CPU/RSS, and scans disk usage.
func (m *Manager) OpenPerformance() {
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
	m.perfSamples = 0
	m.perfScanErr = ""
	m.perfMu.Unlock()

	m.sampleProcessOnce()
	m.RefreshPerformanceDisk("")

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
	m.perfMu.Lock()
	if !m.perfOpen {
		m.perfMu.Unlock()
		return
	}
	m.perfOpen = false
	if m.perfCancel != nil {
		close(m.perfCancel)
		m.perfCancel = nil
	}
	m.perfMu.Unlock()
	m.requestRedraw()
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
	for i, d := range perfCategoryDefs {
		out[i] = PerfCategory{
			ID:     d.id,
			Label:  theme.Tr(d.zh, d.en),
			Detail: theme.Tr(d.dZh, d.dEn),
			Bytes:  byID[d.id],
			Color:  ui.Hex(d.hex),
		}
	}
	return out
}

// RefreshPerformanceDisk scans the 7 disk categories synchronously.
// When `overrideRoot` is non-empty, all files under `overrideRoot` are scanned and classified.
func (m *Manager) RefreshPerformanceDisk(overrideRoot string) {
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
		for _, nb := range m.notebookRoots() {
			scanTree(nb, seen, &errs, func(path string) int {
				if isMarkdownPath(path) {
					return 0 // notes
				}
				return 1 // assets
			}, totals)
		}
		// 2. Application data directory (history, recovery, ai_sessions, logs, config)
		if strings.TrimSpace(m.dataDir) != "" {
			scanTree(m.dataDir, seen, &errs, func(path string) int {
				return classifyDataDirPath(m.dataDir, path)
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

	cats := make([]PerfCategory, len(perfCategoryDefs))
	for i, d := range perfCategoryDefs {
		cats[i] = PerfCategory{
			ID:     d.id,
			Label:  theme.Tr(d.zh, d.en),
			Detail: theme.Tr(d.dZh, d.dEn),
			Bytes:  totals[i],
			Color:  ui.Hex(d.hex),
		}
	}

	m.perfMu.Lock()
	m.perfCategories = cats
	m.perfLastScan = time.Now().Format("15:04:05")
	if len(errs) > 0 {
		m.perfScanErr = strings.Join(errs, "; ")
	} else {
		m.perfScanErr = ""
	}
	m.perfMu.Unlock()
	m.requestRedraw()
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
			if ent.Type()&os.ModeSymlink != 0 {
				continue
			}
			full := filepath.Join(dir, ent.Name())
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
				continue
			}
			if st.Mode().IsRegular() {
				addFileBytes(full, st, seen, classify, totals)
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
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
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
	selectedID := m.perfSelected
	lastScan := m.perfLastScan
	scanErr := m.perfScanErr
	m.perfMu.Unlock()

	var totalBytes uint64
	for _, cat := range cats {
		totalBytes += cat.Bytes
	}
	if lastScan == "" {
		lastScan = "—"
	}

	internalui.ModalCard(c, "perf-modal", title, &open, 720, 560, func() {
		// Header bar.
		ui.Row(c.Key("perf-header")).FillWidth().Height(48).PaddingX(20).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).
			BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
			Children(func() {
				ui.Text(c, title).FontSize(16).FontWeight(600).TextColor(pal.Text)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ref := internalui.IconButton(c, icons.Refresh, internalui.IconButtonOptions{
						ID:      "perf-refresh",
						Label:   theme.Tr("刷新磁盘统计", "Refresh Disk Usage"),
						Tooltip: theme.Tr("刷新", "Refresh"),
					})
					if ref.Clicked() {
						m.RefreshPerformanceDisk("")
						m.sampleProcessOnce()
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
		ui.Row(c.Key("perf-metrics")).FillWidth().Height(42).PaddingX(20).
			Background(pal.Sidebar).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Row(c).Gap(20).AlignItems(ui.Center).Children(func() {
				perfMetric(c, theme.Tr("磁盘占用", "Storage"), formatBytesIEC(totalBytes))
				perfMetric(c, theme.Tr("物理内存", "Memory RSS"), formatBytesIEC(rss))
				perfMetric(c, "CPU", fmt.Sprintf("%.1f%%", cpu))
			})
			perfMetric(c, theme.Tr("统计时间", "Scanned"), lastScan)
		})

		// Body: Interactive Squarified Treemap drawn via ui.Painter + clickable category rows.
		ui.Column(c).FillWidth().Padding(16, 20, 18, 20).Gap(12).Children(func() {
			if scanErr != "" {
				ui.Text(c, theme.Tr("部分文件未能读取：", "Some files could not be read: ")+scanErr).
					FontSize(14).TextColor(pal.Warning)
			}

			m.renderSquarifiedTreemap(c, cats, totalBytes, selectedID)

			// Selected category detail bar.
			var active *PerfCategory
			for i := range cats {
				if cats[i].ID == selectedID {
					active = &cats[i]
					break
				}
			}
			if active == nil && len(cats) > 0 {
				active = &cats[0]
			}
			if active != nil {
				ui.Row(c.Key("perf-detail-bar")).FillWidth().Padding(10, 12, 10, 12).
					Radius(theme.RadiusControl).Background(pal.Shell).Border(1, pal.Hairline).
					AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(10, 10).Radius(theme.RadiusPill).Background(active.Color)
						ui.Text(c, active.Label).FontSize(14).FontWeight(600).TextColor(pal.Text)
						ui.Text(c, active.Detail).FontSize(14).TextColor(pal.TextSecondary)
					})
					ui.Text(c, fmt.Sprintf("%s (%s)", formatBytesIEC(active.Bytes), formatPercent(active.Bytes, totalBytes))).
						FontSize(14).FontWeight(600).TextColor(pal.Text)
				})
			}

			// Accessible & probeable category buttons strip (7 categories).
			ui.Row(c.Key("perf-legend")).FillWidth().Wrap().Gap(8).Children(func() {
				for _, cat := range cats {
					catCopy := cat
					isSelected := catCopy.ID == selectedID
					tileID := "perf-tile-" + catCopy.ID
					btn := ui.ButtonBase(c.Key(tileID)).Height(32).PaddingX(10).Gap(6).
						AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
						Label(catCopy.Label)
					if isSelected {
						btn.Background(pal.Selected).Border(1, pal.Accent)
					} else if btn.Hovered() {
						btn.Background(pal.Hover).Border(1, pal.Hairline)
					} else {
						btn.Background(pal.Card).Border(1, pal.Hairline)
					}
					btn.Children(func() {
						ui.Box(c).Size(8, 8).Radius(theme.RadiusPill).Background(catCopy.Color)
						ui.Text(c, catCopy.Label).FontSize(14).TextColor(pal.Text).SingleLine()
						ui.Text(c, formatBytesIEC(catCopy.Bytes)).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
					})
					internalui.Probe(btn, tileID)
					if btn.Clicked() {
						m.selectPerfCategory(catCopy.ID)
					}
				}
			})
		})
	})

	if !open {
		m.ClosePerformance()
	}
}

func (m *Manager) selectPerfCategory(id string) {
	m.perfMu.Lock()
	m.perfSelected = id
	m.perfMu.Unlock()
}

func (m *Manager) renderSquarifiedTreemap(c *ui.Context, cats []PerfCategory, totalBytes uint64, selectedID string) {
	pal := theme.Ocean()
	canvas := ui.Box(c.Key("perf-treemap")).FillWidth().Height(210).Radius(theme.RadiusControl).
		Background(pal.Shell).Border(1, pal.Hairline)
	internalui.Probe(canvas, "perf-treemap")

	canvas.Draw(func(p *ui.Painter, r ui.Rect) {
		tiles := layoutSquarifiedTreemap(cats, r)
		m.perfMu.Lock()
		m.perfTreeRects = tiles
		m.perfMu.Unlock()

		for _, t := range tiles {
			tr := t.rect
			if tr.W < 2 || tr.H < 2 {
				continue
			}
			active := t.cat.ID == selectedID
			bg := pal.Paper
			border := pal.Hairline
			bw := float32(1)
			if active {
				bg = pal.Selected
				border = pal.Accent
				bw = 2
			}
			p.Fill(tr, bg, theme.RadiusControl)
			p.Stroke(tr, border, theme.RadiusControl, bw)

			// Left accent stripe.
			stripe := ui.Rect{X: tr.X + 4, Y: tr.Y + 6, W: 4, H: max(tr.H-12, 4)}
			p.Fill(stripe, t.cat.Color, 2)

			if tr.W >= 72 && tr.H >= 38 {
				titleSpan := ui.Span{Text: t.cat.Label, Size: 14, Weight: 600, Color: pal.Text}
				p.RichText(tr.X+14, tr.Y+8, tr.W-20, titleSpan)
				if tr.H >= 56 {
					sub := fmt.Sprintf("%s · %s", formatBytesIEC(t.cat.Bytes), formatPercent(t.cat.Bytes, totalBytes))
					subSpan := ui.Span{Text: sub, Size: 14, Weight: 400, Color: pal.TextSecondary}
					p.RichText(tr.X+14, tr.Y+30, tr.W-20, subSpan)
				}
			}
		}
	})

	canvas.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputPointerDown && ev.Kind != ui.InputPointerMove {
			return false
		}
		m.perfMu.Lock()
		tiles := append([]treemapTileRect(nil), m.perfTreeRects...)
		m.perfMu.Unlock()
		for _, t := range tiles {
			if ev.X >= t.rect.X && ev.X <= t.rect.X+t.rect.W && ev.Y >= t.rect.Y && ev.Y <= t.rect.Y+t.rect.H {
				if ev.Kind == ui.InputPointerDown || ev.Kind == ui.InputPointerMove {
					m.selectPerfCategory(t.cat.ID)
				}
				return ev.Kind == ui.InputPointerDown
			}
		}
		return false
	})
}

func layoutSquarifiedTreemap(cats []PerfCategory, bounds ui.Rect) []treemapTileRect {
	if len(cats) == 0 || bounds.W <= 4 || bounds.H <= 4 {
		return nil
	}
	type weightedItem struct {
		cat    PerfCategory
		weight float64
	}
	items := make([]weightedItem, len(cats))
	for i, c := range cats {
		// Smooth log-hybrid weight ensures extreme 60GB vs 0B skew keeps every category readable.
		w := 1.0 + math.Min(math.Log10(float64(c.Bytes)+1)/6.0, 2.0)
		items[i] = weightedItem{cat: c, weight: w}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].weight > items[j].weight
	})

	var totalWeight float64
	for _, it := range items {
		totalWeight += it.weight
	}
	if totalWeight <= 0 {
		return nil
	}

	inner := ui.Rect{
		X: bounds.X + 4,
		Y: bounds.Y + 4,
		W: max(bounds.W-8, 1),
		H: max(bounds.H-8, 1),
	}
	totalArea := float64(inner.W) * float64(inner.H)
	areas := make([]float64, len(items))
	for i, it := range items {
		areas[i] = it.weight / totalWeight * totalArea
	}

	var out []treemapTileRect
	var squarify func(idx int, rect ui.Rect)
	squarify = func(idx int, rect ui.Rect) {
		if idx >= len(items) || rect.W <= 0 || rect.H <= 0 {
			return
		}
		if idx == len(items)-1 {
			out = append(out, treemapTileRect{cat: items[idx].cat, rect: insetRect(rect, 3)})
			return
		}

		shortSide := float64(min(rect.W, rect.H))
		if shortSide <= 0 {
			return
		}
		end := idx + 1
		best := worstAspect(areas[idx:end], shortSide)
		for end < len(items) {
			next := worstAspect(areas[idx:end+1], shortSide)
			if next > best {
				break
			}
			best = next
			end++
		}

		var rowArea float64
		for k := idx; k < end; k++ {
			rowArea += areas[k]
		}
		horizontalStrip := rect.W >= rect.H
		if horizontalStrip {
			stripW := float32(rowArea / float64(rect.H))
			if stripW > rect.W {
				stripW = rect.W
			}
			curY := rect.Y
			for k := idx; k < end; k++ {
				h := float32(areas[k] / float64(max(stripW, 1)))
				if k == end-1 {
					h = rect.Y + rect.H - curY
				}
				tileR := ui.Rect{X: rect.X, Y: curY, W: stripW, H: max(h, 1)}
				out = append(out, treemapTileRect{cat: items[k].cat, rect: insetRect(tileR, 3)})
				curY += h
			}
			squarify(end, ui.Rect{X: rect.X + stripW, Y: rect.Y, W: max(rect.W-stripW, 0), H: rect.H})
		} else {
			stripH := float32(rowArea / float64(rect.W))
			if stripH > rect.H {
				stripH = rect.H
			}
			curX := rect.X
			for k := idx; k < end; k++ {
				w := float32(areas[k] / float64(max(stripH, 1)))
				if k == end-1 {
					w = rect.X + rect.W - curX
				}
				tileR := ui.Rect{X: curX, Y: rect.Y, W: max(w, 1), H: stripH}
				out = append(out, treemapTileRect{cat: items[k].cat, rect: insetRect(tileR, 3)})
				curX += w
			}
			squarify(end, ui.Rect{X: rect.X, Y: rect.Y + stripH, W: rect.W, H: max(rect.H-stripH, 0)})
		}
	}

	squarify(0, inner)
	return out
}

func worstAspect(row []float64, side float64) float64 {
	if len(row) == 0 || side <= 0 {
		return math.Inf(1)
	}
	var sum, maxA float64
	minA := math.Inf(1)
	for _, a := range row {
		sum += a
		if a > maxA {
			maxA = a
		}
		if a < minA {
			minA = a
		}
	}
	if sum <= 0 || minA <= 0 {
		return math.Inf(1)
	}
	s2 := side * side
	sum2 := sum * sum
	return math.Max((s2*maxA)/sum2, sum2/(s2*minA))
}

func insetRect(r ui.Rect, pad float32) ui.Rect {
	if r.W <= pad*2 || r.H <= pad*2 {
		return r
	}
	return ui.Rect{X: r.X + pad, Y: r.Y + pad, W: r.W - pad*2, H: r.H - pad*2}
}

func perfMetric(c *ui.Context, label, value string) {
	pal := theme.Ocean()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(14).TextColor(pal.TextSecondary)
		ui.Text(c, value).FontSize(14).FontWeight(600).TextColor(pal.Text)
	})
}

func formatBytesIEC(b uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	val := float64(b)
	idx := 0
	for val >= 1024.0 && idx < len(units)-1 {
		val /= 1024.0
		idx++
	}
	if idx == 0 {
		return fmt.Sprintf("%d B", b)
	}
	return fmt.Sprintf("%.1f %s", val, units[idx])
}

func formatPercent(b, total uint64) string {
	if total == 0 {
		return "0.0%"
	}
	pct := float64(b) / float64(total) * 100.0
	if b > 0 && pct < 0.01 {
		return "<0.01%"
	}
	return fmt.Sprintf("%.1f%%", pct)
}
