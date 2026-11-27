package modals

import (
	"block/assets"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"block/internal/updater"
	"github.com/egoist/mygo/ui"
)

// HasActiveModal reports whether any modal dialog is currently open.
func (m *Manager) HasActiveModal() bool {
	return m.searchOpen || m.aboutOpen || m.feedbackOpen || m.IsPerformanceOpen() ||
		m.historyOpen || m.exportOpen || m.updaterOpen
}

// RenderModals draws whichever modal dialog(s) are currently open.
func (m *Manager) RenderModals(c *ui.Context) {
	m.pumpFeedback()
	if m.searchOpen {
		m.renderSearchModal(c)
	}
	if m.aboutOpen {
		m.renderAboutModal(c)
	}
	if m.feedbackOpen {
		m.renderFeedbackModal(c)
	}
	if m.IsPerformanceOpen() {
		m.renderPerformanceModal(c)
	}
	if m.historyOpen {
		m.renderHistoryModal(c)
	}
	if m.exportOpen {
		m.renderExportModal(c)
	}
	if m.updaterOpen {
		m.renderUpdaterModal(c)
	}
}

// --- About Modal ---

// OpenAbout opens the About Block dialog.
func (m *Manager) OpenAbout() {
	m.aboutOpen = true
	m.requestRedraw()
}

// CloseAbout closes the About Block dialog.
func (m *Manager) CloseAbout() {
	if !m.aboutOpen {
		return
	}
	m.aboutOpen = false
	m.aboutCopiedAt = time.Time{}
	m.requestRedraw()
}

// IsAboutOpen reports whether the About Block dialog is open.
func (m *Manager) IsAboutOpen() bool {
	return m.aboutOpen
}

var aboutLogo = sync.OnceValue(func() ui.ImageSource {
	data, _ := assets.Read("app-icon/apple-touch-icon.png")
	img, _ := ui.DecodeImage(data)
	return img
})

func (m *Manager) renderAboutModal(c *ui.Context) {
	pal := theme.Ocean()
	open := true
	title := theme.Tr("关于 Block", "About Block")
	internalui.ModalCard(c, "about-modal", title, &open, 344, 0, func() {
		ui.Column(c).FillWidth().Padding(theme.Space24, theme.Space20, theme.Space20, theme.Space20).Gap(theme.Space16).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Absolute().Top(theme.Space10).Right(theme.Space10).Children(func() {
				if internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{ID: "about-close", Label: theme.Tr("关闭", "Close")}).Clicked() {
					m.CloseAbout()
				}
			})
			ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
				emblem := ui.Image(c.Key("about-emblem"), aboutLogo()).Size(76, 76).Margin(0, 0, theme.Space4, 0)
				internalui.Probe(emblem, "about-emblem")
				ui.Text(c, "Block").FontSize(theme.FontPage).FixedLineHeight(theme.LinePage).FontWeight(theme.WeightTitle).TextColor(pal.Text)

				ver := updater.CurrentVersion()
				copied := !m.aboutCopiedAt.IsZero() && time.Since(m.aboutCopiedAt) < 1400*time.Millisecond
				verLabel := fmt.Sprintf("%s v%s", theme.Tr("版本", "Version"), ver)
				verFG := pal.TextSecondary
				verBG := ui.Transparent
				if copied {
					verLabel = fmt.Sprintf("%s v%s", theme.Tr("已复制", "Copied"), ver)
					verFG = pal.Success
					verBG = pal.SuccessSurface
				}

				verBtn := ui.ButtonBase(c.Key("about-version")).Padding(theme.Space4, theme.Space12).MaxWidthPercent(100).MinWidth(0).
					AlignItems(ui.Center).Justify(ui.Center).Radius(theme.RadiusPill).Cursor(ui.CursorPointer).Label(verLabel)
				verHover := verBtn.Hovered()
				verPress := verBtn.Pressed()
				if !copied {
					if verPress {
						verBG = pal.Selected
					} else if verHover {
						verBG = pal.Hover
					}
				}
				internalui.AnimateSurfaceColors(verBtn, verBG, ui.Transparent, false)
				internalui.TactileButton(verBtn, verHover, verPress, false)
				internalui.Probe(verBtn, "about-version")
				verBtn.Children(func() {
					ui.Text(c, verLabel).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(verFG).
						MaxWidthPercent(100).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
				})
				if verBtn.Clicked() {
					c.Services().WriteClipboard("v" + ver)
					m.aboutCopiedAt = time.Now()
					time.AfterFunc(1450*time.Millisecond, m.requestRedraw)
				}
			})
			ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space16, 0, 0, 0).Gap(theme.Space12).BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
				st := m.currentState()
				telemetryOn := !st.TelemetryOff
				telemetryLabel := theme.Tr("匿名使用统计", "Anonymous Usage Stats")
				telemetryRow := ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightControl).
					AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
					ui.Text(c, telemetryLabel).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).FontWeight(theme.WeightRegular).
						TextColor(pal.Text).SingleLine().MinWidth(0).Shrink(1).Ellipsis("…")
					if internalui.TogglePill(c, "about-telemetry-pill", telemetryLabel, &telemetryOn, false) {
						off := !telemetryOn
						m.mutateState(func(s *model.AppState) {
							s.TelemetryOff = off
						})
					}
				})
				internalui.WrappedTooltip(c, telemetryRow, theme.Tr(
					"每天最多发送一次：随机安装编号、版本、系统及大版本、CPU 架构、界面语言；服务器据网络来源记录国家/地区，不保存 IP。不含笔记内容或文件名。",
					"Sent at most once a day: a random install ID, app version, OS and major version, CPU architecture and UI language. The server records the country from the network origin without storing the IP. No note content or file names.",
				))

				ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
					ghIcon := icons.Github
					if internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "about-repo",
						Label:    "GitHub",
						Leading:  &ghIcon,
						Style:    internalui.ButtonSecondary,
						Height:   theme.HeightLarge,
						PaddingX: theme.Space10,
						Grow:     1,
						Truncate: true,
					}).Basis(0).Clicked() {
						c.OpenURL("https://github.com/eachann1024/block")
					}
					privacyIcon := icons.ShieldCheck
					if internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "about-privacy",
						Label:    theme.Tr("隐私政策", "Privacy Policy"),
						Leading:  &privacyIcon,
						Style:    internalui.ButtonSecondary,
						Height:   theme.HeightLarge,
						PaddingX: theme.Space10,
						Grow:     1,
						Truncate: true,
					}).Basis(0).Clicked() {
						c.OpenURL("https://block.cooool.top/privacy/")
					}
				})
			})
		})
	})

	if !open {
		m.CloseAbout()
	}
}

// --- History Modal ---

// OpenHistory opens the version history & Myers Diff comparison modal for `notePath`.
func (m *Manager) OpenHistory(notePath, currentContent string) {
	m.historyOpen = true
	m.historyNotePath = notePath
	m.historyCurrentContent = currentContent
	m.historyMode = "diff"
	m.historyConfirmRestore = false
	m.historyErr = ""
	m.historySnapshots = nil
	m.historySelectedID = ""
	m.historySnapContent = ""
	m.historyDiffLines = nil
	m.historyRowHeights = nil
	m.historyScroll = ui.ScrollState{}

	if m.ListHistoryFn != nil {
		snaps, err := m.ListHistoryFn(notePath)
		if err != nil {
			m.historyErr = err.Error()
		} else {
			m.historySnapshots = snaps
			if len(snaps) > 0 {
				m.selectHistorySnapshot(snaps[0].ID)
			}
		}
	}
	m.requestRedraw()
}

// CloseHistory closes the version history modal.
func (m *Manager) CloseHistory() {
	if !m.historyOpen {
		return
	}
	m.historyOpen = false
	m.historyConfirmRestore = false
	m.requestRedraw()
}

func (m *Manager) selectHistorySnapshot(snapID string) {
	m.historySelectedID = snapID
	m.historyConfirmRestore = false
	m.historyRowHeights = nil
	m.historyScroll = ui.ScrollState{}
	if m.ReadHistoryFn != nil {
		content, err := m.ReadHistoryFn(m.historyNotePath, snapID)
		if err != nil {
			m.historyErr = err.Error()
			m.historySnapContent = ""
			m.historyDiffLines = nil
			return
		}
		m.historyErr = ""
		m.historySnapContent = content
		if m.DiffLinesFn != nil {
			m.historyDiffLines = m.DiffLinesFn(content, m.historyCurrentContent)
		} else {
			m.historyDiffLines = computeMyersDiffLines(content, m.historyCurrentContent)
		}
	}
}

func (m *Manager) restoreSelectedHistory() {
	if m.historySelectedID == "" {
		return
	}
	if m.host != nil {
		if err := m.host.WriteNote(m.historyNotePath, m.historySnapContent); err != nil {
			m.historyErr = err.Error()
			return
		}
	}
	m.CloseHistory()
}

func (m *Manager) renderHistoryModal(c *ui.Context) {
	pal := theme.Ocean()
	open := true
	title := theme.Tr("历史版本对比", "Version History")

	internalui.ModalCard(c, "history-modal", title, &open, 780, 560, func() {
		// Header.
		ui.Row(c.Key("history-header")).FillWidth().Height(48).PaddingX(20).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
			Children(func() {
				noteTitle := filepath.Base(m.historyNotePath)
				if noteTitle == "" || noteTitle == "." {
					noteTitle = title
				}
				ui.Text(c, noteTitle+" · "+title).FontSize(theme.FontTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()

				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					modeIdx := 0
					if m.historyMode == "clean" {
						modeIdx = 1
					}
					if internalui.SegmentedControl(c, "history-mode-seg", &modeIdx, []internalui.SegmentOption{
						{ID: "history-mode-diff", Label: theme.Tr("差异对比", "Diff")},
						{ID: "history-mode-clean", Label: theme.Tr("快照全文", "Snapshot")},
					}) {
						if modeIdx == 0 {
							m.historyMode = "diff"
						} else {
							m.historyMode = "clean"
						}
					}

					histIc := icons.History
					restoreBtn := internalui.ActionButton(c, "history-restore", theme.Tr("恢复此版本", "Restore"), &histIc, internalui.ButtonPrimary, m.historySelectedID == "")
					if restoreBtn.Clicked() {
						m.historyConfirmRestore = true
					}
					cls := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
						ID:    "history-close",
						Label: theme.Tr("关闭", "Close"),
					})
					if cls.Clicked() {
						m.CloseHistory()
					}
				})
			})

		// Confirmation banner if restoring.
		if m.historyConfirmRestore {
			ui.Row(c.Key("history-confirm-bar")).FillWidth().Padding(10, 20, 10, 20).
				Background(pal.WarningSurface).BorderWidth(0, 0, 1, 0).BorderColor(pal.Warning).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(10).
				Transition(internalui.CascadeItemTransition(0, false)).Children(func() {
				ui.Text(c, theme.Tr("确认用选中的历史快照覆盖当前笔记内容？", "Replace current note with this historical snapshot?")).
					FontSize(theme.FontBody).TextColor(pal.Text)
				ui.Row(c).Gap(8).Children(func() {
					cancelBtn := internalui.BtnCancel(c, "history-cancel-restore", internalui.PresetOptions{})
					if cancelBtn.Clicked() {
						m.historyConfirmRestore = false
					}
					confirmIc := icons.History
					confirmBtn := internalui.ActionButton(c, "history-confirm-restore", theme.Tr("确认恢复", "Confirm"), &confirmIc, internalui.ButtonDanger, false)
					if confirmBtn.Clicked() {
						m.restoreSelectedHistory()
					}
				})
			})
		}

		// Two-column body: snapshot list + diff/preview pane.
		ui.Row(c).FillWidth().Height(440).Children(func() {
			// Left snapshot list.
			internalui.Scroll(c.Key("history-list")).Width(220).FillHeight().Padding(10).Gap(4).
				Background(pal.Sidebar).BorderWidth(0, 1, 0, 0).BorderColor(pal.SidebarBorder).
				Children(func() {
					if len(m.historySnapshots) == 0 {
						ui.Text(c, theme.Tr("暂无历史快照", "No history snapshots")).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
						return
					}
					for i, snap := range m.historySnapshots {
						s := snap
						selected := s.ID == m.historySelectedID
						rowID := fmt.Sprintf("history-snap-%d", i)
						ts := time.Unix(s.Timestamp, 0).Format("01-02 15:04:05")
						if s.Timestamp <= 0 {
							ts = s.ID
						}
						row := ui.ButtonBase(c.Key(rowID)).FillWidth().Padding(8, 10, 8, 10).Gap(2).
							Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(ts).
							Transition(internalui.CascadeItemTransition(i, false))
						hovered := row.Hovered()
						pressed := row.Pressed()
						if selected {
							row.Background(pal.Selected).Border(1, pal.SelectedBorder)
						} else if pressed {
							row.Background(pal.Selected).Border(1, ui.Transparent)
						} else if hovered {
							row.Background(pal.Hover).Border(1, ui.Transparent)
						} else {
							row.Border(1, ui.Transparent)
						}
						internalui.TactileCard(row, hovered, pressed, selected, false)
						row.Children(func() {
							ui.Column(c).FillWidth().Gap(2).Children(func() {
								ui.Text(c, ts).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
								sub := internalui.FormatBytesIEC(uint64(max(s.Size, 0)))
								if s.Summary != "" {
									sub = s.Summary
								}
								ui.Text(c, sub).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
							})
						})
						internalui.Probe(row, rowID)
						if row.Clicked() {
							m.selectHistorySnapshot(s.ID)
						}
					}
				})

			// Right preview/diff pane.
			internalui.Scroll(c.Key("history-preview"), &m.historyScroll).Grow(1).FillHeight().
				Padding(14, 18, 14, 18).Gap(4).Children(func() {
				ui.Column(c.Key("history-stage-" + m.historySelectedID + "-" + m.historyMode)).
					FillWidth().Gap(4).Transition(internalui.SurfaceStageTransition()).Children(func() {
					if m.historyErr != "" {
						ui.Text(c, m.historyErr).FontSize(theme.FontBody).TextColor(pal.Danger)
						return
					}
					if m.historySelectedID == "" {
						ui.Text(c, theme.Tr("请选择左侧历史版本查看对比", "Select a snapshot on the left")).
							FontSize(theme.FontBody).TextColor(pal.TextSecondary)
						return
					}
					if m.historyMode == "clean" {
						ui.Text(c, m.historySnapContent).FontSize(theme.FontBody).TextColor(pal.Text)
						return
					}
					m.renderHistoryDiffLines(c, pal)
				})
			})
		})
	})

	if !open {
		m.CloseHistory()
	}
}

const (
	historyDiffVirtThreshold         = 150
	historyDiffViewHeight    float32 = 440
	historyDiffOverscanPx    float32 = 300
)

func estimateHistoryDiffRowHeight(text string) float32 {
	runes := utf8.RuneCountInString(text) + 2
	lines := max(1, (runes+55)/56)
	return float32(lines)*theme.LineBody + theme.Space4
}

func (m *Manager) ensureHistoryDiffRowHeights() []float32 {
	n := len(m.historyDiffLines)
	if len(m.historyRowHeights) == n {
		return m.historyRowHeights
	}
	heights := make([]float32, n)
	for i, dl := range m.historyDiffLines {
		heights[i] = estimateHistoryDiffRowHeight(dl.Text)
	}
	m.historyRowHeights = heights
	return heights
}

func (m *Manager) renderHistoryDiffRow(c *ui.Context, pal theme.Palette, idx int, dl model.DiffLine, trackHeight bool) {
	bg := ui.Transparent
	fg := pal.Text
	prefix := "  "
	switch dl.Kind {
	case model.DiffDelete:
		bg = pal.DiffDelBG
		fg = pal.DiffDelFG
		prefix = "- "
	case model.DiffInsert:
		bg = pal.DiffAddBG
		fg = pal.DiffAddFG
		prefix = "+ "
	}
	row := ui.Row(c.Key(fmt.Sprintf("history-diff-row-%d", idx))).FillWidth().Padding(2, 6, 2, 6).Radius(theme.RadiusXS).Background(bg).Gap(8)
	row.Children(func() {
		ui.Text(c, prefix+dl.Text).FontSize(theme.FontBody).TextColor(fg)
	})
	if trackHeight {
		row.DrawOver(func(_ *ui.Painter, r ui.Rect) {
			if r.H > 0 && idx >= 0 && idx < len(m.historyRowHeights) {
				m.historyRowHeights[idx] = r.H
			}
		})
	}
}

func (m *Manager) renderHistoryDiffLines(c *ui.Context, pal theme.Palette) {
	total := len(m.historyDiffLines)
	if total <= historyDiffVirtThreshold {
		for i, dl := range m.historyDiffLines {
			m.renderHistoryDiffRow(c, pal, i, dl, false)
		}
		return
	}

	heights := m.ensureHistoryDiffRowHeights()
	gap := theme.Space4
	scrollY := max(m.historyScroll.Y, 0)
	winTop := max(scrollY-historyDiffOverscanPx, 0)
	winBot := scrollY + historyDiffViewHeight + historyDiffOverscanPx

	start := 0
	var y float32
	for start < total {
		rowEnd := y + heights[start]
		if rowEnd >= winTop {
			break
		}
		y = rowEnd + gap
		start++
	}
	if start >= total {
		start = total - 1
	}

	end := start
	curY := y
	for end < total {
		curY += heights[end]
		end++
		if curY > winBot {
			break
		}
		curY += gap
	}

	if start > 0 {
		var topH float32
		for i := 0; i < start; i++ {
			topH += heights[i]
		}
		topH += float32(start-1) * gap
		ui.Box(c.Key("history-diff-spacer-top")).FillWidth().Height(topH).Shrink(0)
	}

	for i := start; i < end; i++ {
		m.renderHistoryDiffRow(c, pal, i, m.historyDiffLines[i], true)
	}

	if end < total {
		var botH float32
		for i := end; i < total; i++ {
			botH += heights[i]
		}
		botH += float32(total-end-1) * gap
		ui.Box(c.Key("history-diff-spacer-bot")).FillWidth().Height(botH).Shrink(0)
	}
}

// computeMyersDiffLines computes line-level Myers shortest edit script diff.
func computeMyersDiffLines(oldText, newText string) []model.DiffLine {
	a := splitLinesPreserve(oldText)
	b := splitLinesPreserve(newText)
	n, m := len(a), len(b)
	maxD := n + m
	if maxD == 0 {
		return nil
	}
	// Guard memory/time for very large files.
	if maxD > 4000 {
		out := make([]model.DiffLine, 0, n+m)
		for i, line := range a {
			out = append(out, model.DiffLine{Kind: model.DiffDelete, OldLine: i + 1, Text: line})
		}
		for j, line := range b {
			out = append(out, model.DiffLine{Kind: model.DiffInsert, NewLine: j + 1, Text: line})
		}
		return out
	}

	offset := maxD
	v := make([]int, 2*maxD+1)
	trace := make([][]int, 0, maxD+1)

	for d := 0; d <= maxD; d++ {
		snap := make([]int, 2*d+1)
		copy(snap, v[offset-d:offset+d+1])
		trace = append(trace, snap)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				return backtrackMyers(a, b, trace)
			}
		}
	}
	return nil
}

func backtrackMyers(a, b []string, trace [][]int) []model.DiffLine {
	x, y := len(a), len(b)
	var rev []model.DiffLine
	for d := len(trace) - 1; d >= 0; d-- {
		if d == 0 {
			for x > 0 && y > 0 {
				rev = append(rev, model.DiffLine{
					Kind:    model.DiffEqual,
					OldLine: x,
					NewLine: y,
					Text:    a[x-1],
				})
				x--
				y--
			}
			break
		}
		v := trace[d]
		snapOffset := d
		k := x - y
		var prevK int
		if k == -d || (k != d && v[snapOffset+k-1] < v[snapOffset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[snapOffset+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			rev = append(rev, model.DiffLine{
				Kind:    model.DiffEqual,
				OldLine: x,
				NewLine: y,
				Text:    a[x-1],
			})
			x--
			y--
		}
		if x == prevX {
			rev = append(rev, model.DiffLine{
				Kind:    model.DiffInsert,
				NewLine: y,
				Text:    b[y-1],
			})
		} else {
			rev = append(rev, model.DiffLine{
				Kind:    model.DiffDelete,
				OldLine: x,
				Text:    a[x-1],
			})
		}
		x, y = prevX, prevY
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

func splitLinesPreserve(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

// --- Export Modal ---

// OpenExport opens the Export dialog for `notePath`.
func (m *Manager) OpenExport(notePath string) {
	st := m.currentState()
	fmtChoice := model.ExportMarkdown
	themeChoice := "current"
	includeMeta := false
	if st.ExportPreferences != nil {
		if st.ExportPreferences.Format != "" {
			fmtChoice = st.ExportPreferences.Format
		}
		if st.ExportPreferences.Theme != "" {
			themeChoice = st.ExportPreferences.Theme
		}
		includeMeta = st.ExportPreferences.IncludeMeta
	}
	m.exportOpen = true
	m.exportNotePath = notePath
	m.exportFormat = fmtChoice
	m.exportTheme = themeChoice
	m.exportIncludeMeta = includeMeta
	m.exportOutPath = defaultExportPath(notePath, fmtChoice, st.ExportDirectory)
	m.exportBusy = false
	m.exportStatus = ""
	m.exportErr = ""
	m.requestRedraw()
}

// CloseExport closes the Export dialog.
func (m *Manager) CloseExport() {
	if !m.exportOpen {
		return
	}
	m.exportOpen = false
	m.requestRedraw()
}

func defaultExportPath(notePath string, format model.ExportFormat, exportDir string) string {
	base := filepath.Base(notePath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" || stem == "." {
		stem = "note"
	}
	dir := exportDir
	if strings.TrimSpace(dir) == "" && notePath != "" {
		dir = filepath.Dir(notePath)
	}
	if dir == "" || dir == "." {
		return stem + "." + exportExt(format)
	}
	return filepath.Join(dir, stem+"."+exportExt(format))
}

func exportExt(format model.ExportFormat) string {
	switch format {
	case model.ExportPNG:
		return "png"
	case model.ExportHTML:
		return "html"
	case model.ExportPDF:
		return "pdf"
	case model.ExportDOCX:
		return "docx"
	case model.ExportZIP:
		return "zip"
	default:
		return "md"
	}
}

func replaceExportExt(path string, format model.ExportFormat) string {
	if strings.TrimSpace(path) == "" {
		return "note." + exportExt(format)
	}
	oldExt := filepath.Ext(path)
	return strings.TrimSuffix(path, oldExt) + "." + exportExt(format)
}

func (m *Manager) runExport() {
	outPath := strings.TrimSpace(m.exportOutPath)
	if outPath == "" {
		m.exportErr = theme.Tr("请输入导出路径", "Please enter an output path")
		return
	}
	if m.ExportFn == nil {
		m.exportErr = theme.Tr("当前未接入导出处理器", "Export handler is not configured")
		return
	}
	m.exportBusy = true
	m.exportErr = ""
	m.exportStatus = ""
	err := m.ExportFn(m.exportFormat, outPath)
	m.exportBusy = false
	if err != nil {
		m.exportErr = err.Error()
		return
	}
	m.exportStatus = theme.Tr("已导出到 ", "Exported to ") + outPath
	m.mutateState(func(s *model.AppState) {
		s.ExportPreferences = &model.ExportPreferences{
			Format:      m.exportFormat,
			Theme:       m.exportTheme,
			IncludeMeta: m.exportIncludeMeta,
		}
		s.ExportDirectory = filepath.Dir(outPath)
	})
}

func (m *Manager) renderExportModal(c *ui.Context) {
	pal := theme.Ocean()
	open := true
	title := theme.Tr("导出笔记", "Export Note")

	internalui.ModalCard(c, "export-modal", title, &open, 520, 480, func() {
		ui.Column(c).FillWidth().Padding(22).Gap(16).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, title).FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text)
				cls := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
					ID:    "export-close",
					Label: theme.Tr("关闭", "Close"),
				})
				if cls.Clicked() {
					m.CloseExport()
				}
			})

			// Format selector.
			formats := []struct {
				fmt   model.ExportFormat
				label string
				icon  icons.Icon
			}{
				{model.ExportMarkdown, "Markdown", icons.Note},
				{model.ExportHTML, "HTML", icons.LangHtml},
				{model.ExportPDF, "PDF", icons.FilePdf},
				{model.ExportPNG, "PNG", icons.Image},
				{model.ExportDOCX, "Word (DOCX)", icons.FileWord},
				{model.ExportZIP, "ZIP", icons.FileArchive},
			}
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				ui.Text(c, theme.Tr("导出格式", "Format")).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary)
				ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
					for _, item := range formats {
						style := internalui.ButtonSecondary
						selected := m.exportFormat == item.fmt
						if selected {
							style = internalui.ButtonPrimary
						}
						ic := item.icon
						btn := internalui.ActionButton(c, "export-fmt-"+string(item.fmt), item.label, &ic, style, false)
						if selected {
							btn.Border(1, ui.Transparent)
						}
						if btn.Clicked() && m.exportFormat != item.fmt {
							m.exportFormat = item.fmt
							m.exportOutPath = replaceExportExt(m.exportOutPath, item.fmt)
							m.exportErr = ""
							m.exportStatus = ""
						}
					}
				})
			})

			// Theme & Metadata options.
			themeIdx := 0
			switch m.exportTheme {
			case "light":
				themeIdx = 1
			case "dark":
				themeIdx = 2
			}
			dialogRow(c, theme.Tr("导出主题", "Theme"), func() {
				if internalui.SegmentedControl(c, "export-theme-seg", &themeIdx, []internalui.SegmentOption{
					{Label: theme.Tr("跟随当前", "Current")},
					{Label: theme.Tr("浅色", "Light")},
					{Label: theme.Tr("深色", "Dark")},
				}) {
					m.exportTheme = []string{"current", "light", "dark"}[themeIdx]
				}
			})
			dialogRow(c, theme.Tr("包含前置元数据", "Include Frontmatter"), func() {
				internalui.TogglePill(c, "export-include-meta", theme.Tr("包含元数据", "Include Frontmatter"), &m.exportIncludeMeta, false)
			})

			// Output path.
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Text(c, theme.Tr("输出路径", "Output Path")).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary)
				in := ui.TextInput(c.Key("export-out-path"), &m.exportOutPath).
					Placeholder("note.md").PlaceholderColor(pal.Placeholder).Label(theme.Tr("输出路径", "Output Path")).FillWidth()
				internalui.Probe(in, "export-out-path")
				if in.Submitted() {
					m.runExport()
				}
			})

			// Footer status + action.
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(10).Children(func() {
				if m.exportErr != "" {
					ui.Text(c, m.exportErr).FontSize(theme.FontBody).TextColor(pal.Danger).Grow(1).MinWidth(0)
				} else if m.exportStatus != "" {
					ui.Text(c, m.exportStatus).FontSize(theme.FontBody).TextColor(pal.Success).Grow(1).MinWidth(0)
				} else {
					ui.Box(c).Grow(1)
				}
				submit := internalui.ActionButton(c, "export-submit", theme.Tr("导出", "Export"), &[]icons.Icon{icons.Download}[0], internalui.ButtonPrimary, m.exportBusy)
				if submit.Clicked() {
					m.runExport()
				}
			})
		})
	})

	if !open {
		m.CloseExport()
	}
}

// dialogRow renders a compact "label …… control" row for dialog forms.
func dialogRow(c *ui.Context, label string, control func()) {
	pal := theme.Ocean()
	ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space16).Children(func() {
		ui.Text(c, label).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine().Shrink(0)
		ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(control)
	})
}
