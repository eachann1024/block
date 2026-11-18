package modals

import (
	"block/assets"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const blockAppVersion = "1.0.379"

// HasActiveModal reports whether any modal dialog is currently open.
func (m *Manager) HasActiveModal() bool {
	return m.searchOpen || m.aboutOpen || m.feedbackOpen || m.IsPerformanceOpen() ||
		m.historyOpen || m.exportOpen || m.updaterOpen
}

// RenderModals draws whichever modal dialog(s) are currently open.
func (m *Manager) RenderModals(c *ui.Context) {
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
	internalui.ModalCard(c, "about-modal", title, &open, 368, 0, func() {
		ui.Column(c).FillWidth().Padding(28, 24, 22, 24).Gap(18).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Absolute().Top(10).Right(10).Children(func() {
				if internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{ID: "about-close", Label: theme.Tr("关闭", "Close")}).Clicked() {
					m.CloseAbout()
				}
			})
			ui.Column(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Image(c.Key("about-emblem"), aboutLogo()).Size(84, 84).Margin(0, 0, 6, 0)
				ui.Text(c, "Block").FontSize(theme.FontPage).FontWeight(theme.WeightTitle).TextColor(pal.Text)
				ui.Text(c, fmt.Sprintf("%s %s", theme.Tr("版本", "Version"), blockAppVersion)).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
				author := ui.ButtonBase(c.Key("about-profile")).Padding(3, 12).Radius(theme.RadiusPill).Cursor(ui.CursorPointer)
				author.Children(func() { ui.Text(c, theme.Tr("作者 eachann", "Made by eachann")).FontSize(theme.FontBody).TextColor(pal.Accent) })
				if author.Clicked() {
					c.OpenURL("https://github.com/eachann1024")
				}
			})
			ui.Column(c).FillWidth().Padding(16, 0, 4, 0).BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
				if internalui.ActionButton(c, "about-repo", "eachann1024/block", &[]icons.Icon{icons.Code}[0], internalui.ButtonSecondary, false).Height(42).FillWidth().Clicked() {
					c.OpenURL("https://github.com/eachann1024/block")
				}
			})
			if internalui.ActionButton(c, "about-done", theme.Tr("关闭", "Close"), nil, internalui.ButtonPrimary, false).Clicked() {
				m.CloseAbout()
			}
		})
	})

	if !open {
		m.CloseAbout()
	}
}

// --- Feedback Modal ---

// OpenFeedback opens the Feedback modal, restoring any saved FeedbackDraft.
func (m *Manager) OpenFeedback() {
	st := m.currentState()
	if st.FeedbackDraft != nil {
		m.feedbackCategory = normalizeFeedbackCat(st.FeedbackDraft.Category)
		m.feedbackMessage = st.FeedbackDraft.Message
		m.feedbackContact = st.FeedbackDraft.Contact
	} else {
		m.feedbackCategory = "bug"
		m.feedbackMessage = ""
		m.feedbackContact = ""
	}
	m.feedbackDone = false
	m.feedbackErr = ""
	m.feedbackSentContact = ""
	m.feedbackOpen = true
	m.requestRedraw()
}

// CloseFeedback closes the Feedback modal, persisting any non-done draft.
func (m *Manager) CloseFeedback() {
	if !m.feedbackOpen {
		return
	}
	if !m.feedbackDone {
		m.persistFeedbackDraft()
	}
	m.feedbackOpen = false
	m.requestRedraw()
}

// IsFeedbackOpen reports whether the Feedback modal is open.
func (m *Manager) IsFeedbackOpen() bool {
	return m.feedbackOpen
}

func normalizeFeedbackCat(raw string) string {
	switch raw {
	case "idea", "other":
		return raw
	default:
		return "bug"
	}
}

func (m *Manager) persistFeedbackDraft() {
	msg := m.feedbackMessage
	contact := m.feedbackContact
	cat := normalizeFeedbackCat(m.feedbackCategory)
	nonEmpty := strings.TrimSpace(msg) != "" || strings.TrimSpace(contact) != "" || cat != "bug"
	m.mutateState(func(s *model.AppState) {
		if !nonEmpty {
			s.FeedbackDraft = nil
			return
		}
		s.FeedbackDraft = &model.FeedbackDraft{
			Category: cat,
			Message:  msg,
			Contact:  contact,
		}
	})
}

func (m *Manager) submitFeedback() {
	msg := strings.TrimSpace(m.feedbackMessage)
	if msg == "" {
		m.feedbackErr = theme.Tr("写几个字再发送吧。", "Write a few words first.")
		return
	}
	contact := strings.TrimSpace(m.feedbackContact)
	if contact != "" && !validFeedbackEmail(contact) {
		m.feedbackErr = theme.Tr("邮箱格式不太对，检查一下？", "That email doesn't look right.")
		return
	}
	if m.FeedbackSubmitFn != nil {
		if err := m.FeedbackSubmitFn(m.feedbackCategory, msg, contact); err != nil {
			m.feedbackErr = err.Error()
			return
		}
	}
	m.feedbackErr = ""
	m.feedbackSentContact = contact
	m.feedbackDone = true
	m.feedbackMessage = ""
	m.feedbackCategory = "bug"
	m.mutateState(func(s *model.AppState) {
		s.FeedbackDraft = nil
	})
}

func validFeedbackEmail(s string) bool {
	if strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at >= len(s)-1 {
		return false
	}
	domain := s[at+1:]
	dot := strings.LastIndexByte(domain, '.')
	return dot > 0 && dot < len(domain)-1
}

func (m *Manager) renderFeedbackModal(c *ui.Context) {
	pal := theme.Ocean()
	open := true
	title := theme.Tr("给我们带句话", "Send us a note")

	width := float32(500)
	if m.feedbackDone {
		width = 400
	}
	internalui.Dialog(c, "feedback-modal", title, &open, internalui.ModalLayout{Width: width, Centered: true}, func() {
		if m.feedbackDone {
			doneBox := ui.Column(c.Key("feedback-done")).FillWidth().Padding(28).Gap(14).AlignItems(ui.Center)
			internalui.Probe(doneBox, "feedback-done")
			doneBox.Children(func() {
				ui.Box(c).Size(52, 52).Radius(theme.RadiusPill).Background(pal.SuccessSurface).
					Border(1, pal.Success).Center().Children(func() {
					icons.Draw(c, icons.Check, 24, pal.MonoIconColors(pal.Success))
				})
				ui.Text(c, theme.Tr("收到了，谢谢你", "Got it, thank you")).FontSize(theme.FontHeading).FontWeight(theme.WeightTitle).TextColor(pal.Text)
				note := theme.Tr("我们会认真看每一条反馈。", "We read every piece of feedback carefully.")
				if m.feedbackSentContact != "" {
					note = theme.Tr("我们会认真看，有进展会发到 "+m.feedbackSentContact+"。", "We will reply to "+m.feedbackSentContact+".")
				}
				ui.Text(c, note).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
				doneBtn := internalui.ActionButton(c, "feedback-done-close", theme.Tr("好的", "Done"), nil, internalui.ButtonSecondary, false)
				if doneBtn.Clicked() {
					m.CloseFeedback()
				}
			})
			return
		}

		ui.Column(c).FillWidth().Padding(24).Gap(18).Children(func() {
			// Header.
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, title).FontSize(theme.FontHeading).FontWeight(theme.WeightTitle).TextColor(pal.Text)
				cls := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
					ID:    "feedback-close",
					Label: theme.Tr("关闭", "Close"),
				})
				if cls.Clicked() {
					m.CloseFeedback()
				}
			})

			// Category chips: bug / idea / other.
			cats := []struct {
				code string
				zh   string
				en   string
				phZh string
				phEn string
			}{
				{"bug", "问题反馈", "Bug", "哪里出了问题？怎么操作会出现？", "What went wrong, and how can we reproduce it?"},
				{"idea", "功能建议", "Feature idea", "希望加什么功能，或者哪里可以更好用？", "What would you like added or improved?"},
				{"other", "其他", "Other", "有什么想法或疑问，随便聊聊。", "Anything on your mind — questions or notes."},
			}
			activePH := cats[0].phZh
			if theme.IsEn() {
				activePH = cats[0].phEn
			}
			ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
				for _, cat := range cats {
					selected := m.feedbackCategory == cat.code
					if selected {
						activePH = theme.Tr(cat.phZh, cat.phEn)
					}
					chipID := "feedback-cat-" + cat.code
					label := theme.Tr(cat.zh, cat.en)
					style := internalui.ButtonSecondary
					if selected {
						style = internalui.ButtonPrimary
					}
					btn := internalui.ActionButton(c, chipID, label, nil, style, false)
					if selected {
						btn.Border(1, ui.Transparent)
					}
					if btn.Clicked() && m.feedbackCategory != cat.code {
						m.feedbackCategory = cat.code
						m.feedbackErr = ""
						m.persistFeedbackDraft()
					}
				}
			})

			// Message textarea.
			msgIn := ui.TextArea(c.Key("feedback-message"), &m.feedbackMessage).
				Placeholder(activePH).Label(theme.Tr("反馈内容", "Feedback message")).
				FillWidth().Height(116)
			internalui.AutoHideScrollbars(c, msgIn)
			internalui.Probe(msgIn, "feedback-message")
			if msgIn.Changed() {
				m.feedbackErr = ""
				m.persistFeedbackDraft()
			}

			// Optional contact email input.
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Text(c, theme.Tr("回复邮箱（选填）", "Reply email (optional)")).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text)
				contactIn := ui.TextInput(c.Key("feedback-contact"), &m.feedbackContact).
					Placeholder(theme.Tr("留了邮箱，回复会发到这里", "We reply here if you leave one")).
					Label(theme.Tr("回复邮箱", "Reply email")).FillWidth().Height(38)
				internalui.Probe(contactIn, "feedback-contact")
				if contactIn.Changed() {
					m.feedbackErr = ""
					m.persistFeedbackDraft()
				}
			})

			// Footer status + Send button.
			ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).Children(func() {
				if m.feedbackErr != "" {
					ui.Text(c, m.feedbackErr).FontSize(theme.FontBody).TextColor(pal.Danger).Grow(1).MinWidth(0)
				} else if strings.TrimSpace(m.feedbackMessage) != "" || strings.TrimSpace(m.feedbackContact) != "" {
					ui.Text(c, theme.Tr("草稿已自动保存", "Draft auto-saved")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				} else {
					ui.Box(c).Grow(1)
				}
				sendBtn := internalui.ActionButton(c, "feedback-send", theme.Tr("发送", "Send"), nil, internalui.ButtonPrimary, false)
				if sendBtn.Clicked() {
					m.submitFeedback()
				}
			})
		})
	})

	if !open {
		m.CloseFeedback()
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

					restoreBtn := internalui.ActionButton(c, "history-restore", theme.Tr("恢复此版本", "Restore"), nil, internalui.ButtonPrimary, m.historySelectedID == "")
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
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(10).Children(func() {
				ui.Text(c, theme.Tr("确认用选中的历史快照覆盖当前笔记内容？", "Replace current note with this historical snapshot?")).
					FontSize(theme.FontBody).TextColor(pal.Text)
				ui.Row(c).Gap(8).Children(func() {
					cancelBtn := internalui.ActionButton(c, "history-cancel-restore", theme.Tr("取消", "Cancel"), nil, internalui.ButtonSecondary, false)
					if cancelBtn.Clicked() {
						m.historyConfirmRestore = false
					}
					confirmBtn := internalui.ActionButton(c, "history-confirm-restore", theme.Tr("确认恢复", "Confirm"), nil, internalui.ButtonDanger, false)
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
							Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(ts)
						if selected {
							row.Background(pal.Selected).Border(1, pal.Accent)
						} else if row.Hovered() {
							row.Background(pal.Hover).Border(1, ui.Transparent)
						} else {
							row.Border(1, ui.Transparent)
						}
						row.Children(func() {
							ui.Column(c).FillWidth().Gap(2).Children(func() {
								ui.Text(c, ts).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
								sub := formatBytesIEC(uint64(max(s.Size, 0)))
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
				for _, dl := range m.historyDiffLines {
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
					ui.Row(c).FillWidth().Padding(2, 6, 2, 6).Radius(theme.RadiusXS).Background(bg).Gap(8).Children(func() {
						ui.Text(c, prefix+dl.Text).FontSize(theme.FontBody).TextColor(fg)
					})
				}
			})
		})
	})

	if !open {
		m.CloseHistory()
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
		snap := make([]int, len(v))
		copy(snap, v)
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
				return backtrackMyers(a, b, trace, offset)
			}
		}
	}
	return nil
}

func backtrackMyers(a, b []string, trace [][]int, offset int) []model.DiffLine {
	x, y := len(a), len(b)
	var rev []model.DiffLine
	for d := len(trace) - 1; d >= 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[offset+prevK]
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
		if d > 0 {
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
				ui.Text(c, title).FontSize(theme.FontHeading).FontWeight(theme.WeightTitle).TextColor(pal.Text)
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
			}{
				{model.ExportMarkdown, "Markdown"},
				{model.ExportHTML, "HTML"},
				{model.ExportPDF, "PDF"},
				{model.ExportPNG, "PNG"},
				{model.ExportDOCX, "Word (DOCX)"},
				{model.ExportZIP, "ZIP"},
			}
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				ui.Text(c, theme.Tr("导出格式", "Format")).FontSize(theme.FontBody).FontWeight(theme.WeightTitle).TextColor(pal.Text)
				ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
					for _, item := range formats {
						style := internalui.ButtonSecondary
						selected := m.exportFormat == item.fmt
						if selected {
							style = internalui.ButtonPrimary
						}
						btn := internalui.ActionButton(c, "export-fmt-"+string(item.fmt), item.label, nil, style, false)
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
			settingRow(c, theme.Tr("导出主题", "Theme"), func() {
				if internalui.SegmentedControl(c, "export-theme-seg", &themeIdx, []internalui.SegmentOption{
					{Label: theme.Tr("跟随当前", "Current")},
					{Label: theme.Tr("浅色", "Light")},
					{Label: theme.Tr("深色", "Dark")},
				}) {
					m.exportTheme = []string{"current", "light", "dark"}[themeIdx]
				}
			})
			settingRow(c, theme.Tr("包含前置元数据", "Include Frontmatter"), func() {
				internalui.TogglePill(c, "export-include-meta", theme.Tr("包含元数据", "Include Frontmatter"), &m.exportIncludeMeta, false)
			})

			// Output path.
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Text(c, theme.Tr("输出路径", "Output Path")).FontSize(theme.FontBody).FontWeight(theme.WeightTitle).TextColor(pal.Text)
				in := ui.TextInput(c.Key("export-out-path"), &m.exportOutPath).
					Placeholder("note.md").Label(theme.Tr("输出路径", "Output Path")).FillWidth()
				internalui.Probe(in, "export-out-path")
				if in.Submitted() {
					m.runExport()
				}
			})

			// Footer status + action.
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(10).Children(func() {
				if m.exportErr != "" {
					ui.Text(c, m.exportErr).FontSize(theme.FontBody).TextColor(pal.Danger).Grow(1)
				} else if m.exportStatus != "" {
					ui.Text(c, m.exportStatus).FontSize(theme.FontBody).TextColor(pal.Success).Grow(1)
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

// --- Updater Modal ---

// OpenUpdater opens the Check for Updates modal.
func (m *Manager) OpenUpdater() {
	m.updaterOpen = true
	m.updaterChecking = false
	m.updaterUpdating = false
	m.updaterProgress = 0
	if m.updaterStatus == "" {
		m.updaterStatus = theme.Tr("当前已是最新版本", "You're up to date")
	}
	if len(m.updaterNotes) == 0 {
		m.updaterNotes = []string{
			theme.Tr("全窗口设置页与快捷键录制", "Full-window Settings and shortcut recorder"),
			theme.Tr("性能与磁盘占用 Squarified Treemap", "Performance monitor with Squarified Treemap"),
			theme.Tr("历史版本 Myers Diff 对比与恢复", "Version history Myers Diff comparison and restore"),
		}
	}
	m.requestRedraw()
}

// CloseUpdater closes the Check for Updates modal.
func (m *Manager) CloseUpdater() {
	if !m.updaterOpen {
		return
	}
	m.updaterOpen = false
	m.requestRedraw()
}

func (m *Manager) renderUpdaterModal(c *ui.Context) {
	pal := theme.Ocean()
	open := true
	title := theme.Tr("检查更新", "Check for Updates")

	internalui.ModalCard(c, "updater-modal", title, &open, 580, 0, func() {
		ui.Column(c).FillWidth().Padding(24, 24, 20, 24).Gap(16).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					icons.Draw(c, icons.UpdateTangle, 20, pal.DuoIconColors(pal.Accent))
					ui.Text(c, title).FontSize(theme.FontHeading).FontWeight(theme.WeightTitle).TextColor(pal.Text)
				})
				cls := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
					ID:    "updater-close",
					Label: theme.Tr("关闭", "Close"),
				})
				if cls.Clicked() {
					m.CloseUpdater()
				}
			})

			ui.Row(c).FillWidth().Padding(12).Radius(theme.RadiusMD).
				Background(pal.Shell).Border(1, pal.Hairline).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, fmt.Sprintf("%s v%s", theme.Tr("当前版本", "Current"), blockAppVersion)).
					FontSize(theme.FontBody).FontWeight(theme.WeightTitle).TextColor(pal.Text)
				ui.Text(c, m.updaterStatus).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
			})

			ui.Column(c).FillWidth().Gap(theme.Space8).Children(func() {
				ui.Text(c, fmt.Sprintf("v%s %s", m.updaterLatest, theme.Tr("更新内容", "Release Notes"))).
					FontSize(theme.FontTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text)
				for _, item := range m.updaterNotes {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
						ui.Text(c, "•").FontSize(theme.FontBody).TextColor(pal.TextSecondary)
						ui.Text(c, item).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1)
					})
				}
			})

			if m.updaterUpdating {
				ui.Progress(c, m.updaterProgress).Label(theme.Tr("下载更新", "Downloading update"))
			}

			ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)

			ui.Row(c).FillWidth().Justify(ui.End).Gap(10).Children(func() {
				cancelBtn := internalui.ActionButton(c, "updater-cancel", theme.Tr("先不更新", "Not Now"), nil, internalui.ButtonSecondary, false)
				if cancelBtn.Clicked() {
					m.CloseUpdater()
				}
				checkBtn := internalui.ActionButton(c, "updater-submit", theme.Tr("检查更新", "Check Now"), nil, internalui.ButtonPrimary, m.updaterChecking)
				if checkBtn.Clicked() {
					if mygo.App.IsReady() {
						m.updaterChecking = true
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
							defer cancel()
							_, _ = mygo.Updater.Check(ctx)
							mygo.RunOnMain(func() {
								m.updaterChecking = false
								m.updaterStatus = theme.Tr("当前已是最新版本", "You're up to date")
								m.requestRedraw()
							})
						}()
					} else {
						m.updaterStatus = theme.Tr("当前已是最新版本", "You're up to date")
					}
				}
			})
		})
	})

	if !open {
		m.CloseUpdater()
	}
}
