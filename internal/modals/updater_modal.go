package modals

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"block/internal/updater"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// OpenUpdater opens the application update dialog and triggers an initial check if idle.
func (m *Manager) OpenUpdater() {
	m.updaterOpen = true
	m.updaterPreview = nil
	m.updaterSourceMenu = internalui.DropdownSelectState{
		PreferAbove: true,
		AlignLeft:   true,
	}
	if m.Updater != nil {
		snap := m.Updater.Snapshot()
		if snap.Status == updater.StatusIdle {
			m.Updater.CheckAsync(true)
		}
	}
	m.requestRedraw()
}

// OpenUpdaterPreview opens the application update dialog with a caller-supplied
// snapshot for offscreen layout and snapshot verification without mutating the updater service.
func (m *Manager) OpenUpdaterPreview(snap updater.Snapshot) {
	cp := snap
	m.updaterOpen = true
	m.updaterPreview = &cp
	m.updaterSourceMenu = internalui.DropdownSelectState{
		PreferAbove: true,
		AlignLeft:   true,
	}
	m.requestRedraw()
}

// CloseUpdater closes the application update dialog without interrupting background downloads.
func (m *Manager) CloseUpdater() {
	if !m.updaterOpen {
		return
	}
	m.updaterOpen = false
	m.updaterPreview = nil
	m.updaterSourceMenu.Open = false
	m.requestRedraw()
}

// IsUpdaterOpen reports whether the updater dialog is open.
func (m *Manager) IsUpdaterOpen() bool {
	return m.updaterOpen
}

func updaterTr(showEn bool, zh, en string) string {
	if showEn {
		return en
	}
	return zh
}

func formatMegabytes(bytes int64) string {
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024.0*1024.0))
}

func updaterModalTitle(snap updater.Snapshot, showEn bool) string {
	switch snap.Status {
	case updater.StatusReady:
		return updaterTr(showEn, "新版本已就绪", "Update Ready")
	case updater.StatusAvailable, updater.StatusDownloading, updater.StatusInstalling:
		return updaterTr(showEn, "发现新版本", "Update Available")
	case updater.StatusUpToDate:
		return updaterTr(showEn, "已是最新版本", "Up to Date")
	default:
		return updaterTr(showEn, "检查更新", "App Updates")
	}
}

func updaterVersionLabel(snap updater.Snapshot) string {
	if snap.Status == updater.StatusUpToDate {
		return fmt.Sprintf("v%s", snap.CurrentVersion)
	}
	if snap.Notes.HasNewer && snap.Notes.FromVersion != "" && snap.Notes.ToVersion != "" &&
		updater.CompareVersion(snap.Notes.FromVersion, snap.Notes.ToVersion) < 0 {
		return fmt.Sprintf("v%s → v%s", snap.Notes.FromVersion, snap.Notes.ToVersion)
	}
	if snap.LatestVersion != "" && updater.CompareVersion(snap.CurrentVersion, snap.LatestVersion) < 0 {
		return fmt.Sprintf("v%s → v%s", snap.CurrentVersion, snap.LatestVersion)
	}
	return fmt.Sprintf("v%s", snap.CurrentVersion)
}

func updaterSourceDisplayName(snap updater.Snapshot, showEn bool) string {
	switch snap.SourceMode {
	case updater.SourceMirror:
		return updaterTr(showEn, "国内镜像", "CN Mirror")
	case updater.SourceGitHub:
		return "GitHub"
	default:
		switch snap.EffectiveChannel() {
		case updater.ChannelMirror:
			return updaterTr(showEn, "自动 · 镜像", "Auto · Mirror")
		case updater.ChannelGitHub:
			return updaterTr(showEn, "自动 · GitHub", "Auto · GitHub")
		default:
			return updaterTr(showEn, "智能自动", "Smart Auto")
		}
	}
}

func updaterChannelHealth(snap updater.Snapshot, ch string) (ms int64, st updater.ProbeStatus, errKind string) {
	cp := snap.ProbeFor(ch)
	if cp.Status == updater.ProbeIdle && snap.ActiveChannel == ch && snap.FetchLatencyMs > 0 {
		return snap.FetchLatencyMs, updater.ProbeOK, ""
	}
	return cp.LatencyMs, cp.Status, cp.ErrorKind
}

func updaterEffectiveHealth(snap updater.Snapshot) (ms int64, st updater.ProbeStatus, errKind string) {
	if snap.RetestingTarget {
		return 0, updater.ProbeProbing, ""
	}
	switch snap.SourceMode {
	case updater.SourceMirror:
		return updaterChannelHealth(snap, updater.ChannelMirror)
	case updater.SourceGitHub:
		return updaterChannelHealth(snap, updater.ChannelGitHub)
	default:
		if eff := snap.EffectiveChannel(); eff != "" {
			effMs, effSt, effKind := updaterChannelHealth(snap, eff)
			if effSt == updater.ProbeOK && effMs > 0 {
				return effMs, effSt, effKind
			}
		}
		if snap.AllRoutesUnavailable() {
			kind := ""
			if snap.MirrorProbe.ErrorKind == "timeout" && snap.GitHubProbe.ErrorKind == "timeout" {
				kind = "timeout"
			}
			return 0, updater.ProbeFailed, kind
		}
		if snap.ProbingChannels || snap.Status == updater.StatusChecking {
			return 0, updater.ProbeProbing, ""
		}
		if snap.FetchLatencyMs > 0 {
			return snap.FetchLatencyMs, updater.ProbeOK, ""
		}
		if snap.Status == updater.StatusError {
			return 0, updater.ProbeFailed, ""
		}
		return 0, updater.ProbeIdle, ""
	}
}

func updaterSignalStyle(
	ms int64,
	st updater.ProbeStatus,
	pal theme.Palette,
	uPal theme.UpdaterPalette,
) (bars int, dot, fg, bg ui.Color) {
	switch st {
	case updater.ProbeProbing:
		return 0, pal.Accent, uPal.BadgeAccentFG, uPal.BadgeAccentBG
	case updater.ProbeFailed:
		return 0, pal.Danger, uPal.BadgeDangerFG, uPal.BadgeDangerBG
	case updater.ProbeOK:
		switch {
		case ms <= 300:
			return 3, pal.Success, uPal.BadgeGoodFG, uPal.BadgeGoodBG
		case ms <= 1000:
			return 2, pal.Warning, uPal.BadgeWarnFG, uPal.BadgeWarnBG
		default:
			return 1, pal.Danger, uPal.BadgeDangerFG, uPal.BadgeDangerBG
		}
	default:
		return 0, pal.Placeholder, pal.TextSecondary, ui.Transparent
	}
}

func (m *Manager) renderUpdaterModal(c *ui.Context) {
	pal := theme.Ocean()
	uPal := pal.UpdaterColors()
	open := true
	showEn := theme.IsEn()

	var snap updater.Snapshot
	switch {
	case m.updaterPreview != nil:
		snap = *m.updaterPreview
	case m.Updater != nil:
		snap = m.Updater.Snapshot()
	default:
		cur := updater.CurrentVersion()
		snap = updater.Snapshot{
			Status:         updater.StatusUpToDate,
			CurrentVersion: cur,
			LatestVersion:  cur,
			SourceMode:     updater.NormalizeSourceMode(m.currentState().UpdateSource),
			ViewMode:       updater.ViewMerged,
			Notes: updater.AggregatedNotes{
				FromVersion: cur,
				ToVersion:   cur,
			},
		}
	}

	if snap.Status == updater.StatusDownloading || snap.Status == updater.StatusInstalling {
		m.updaterSourceMenu.Open = false
	}

	title := updaterModalTitle(snap, showEn)
	wasSourceMenuOpen := m.updaterSourceMenu.Open

	internalui.Dialog(c, "updater-modal", title, &open, internalui.ModalLayout{
		Width:     theme.UpdaterModalWidth,
		MaxHeight: theme.UpdaterModalMaxHeight,
		Centered:  true,
	}, func() {
		ui.Column(c).FillWidth().Clip().Padding(theme.Space20, 0, theme.Space16, 0).Gap(theme.Space20).Children(func() {
			// 1. Light Header: App Icon + Title/Version + optional View Toggle + Close.
			ui.Row(c.Key("updater-header")).FillWidth().PaddingX(theme.Space20).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space12).Grow(1).MinWidth(0).Children(func() {
					ui.Box(c.Key("updater-app-icon")).
						Width(theme.UpdaterHeaderIconBox).Height(theme.UpdaterHeaderIconBox).
						Radius(theme.RadiusLG).Background(uPal.Mark.Tile).Clip().
						Center().Shrink(0).Children(func() {
						internalui.CatMark(c, theme.UpdaterHeaderMarkSize, uPal.Mark)
					})

					ui.Column(c).Gap(theme.Space2).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, title).FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
							FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine().Ellipsis("…")
						ui.Text(c, updaterVersionLabel(snap)).Font("monospace").
							FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
					})
				})

				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space2).Shrink(0).Children(func() {
					if snap.Notes.VersionCount > 1 {
						isByVer := snap.ViewMode == updater.ViewByVersion
						toggleIc := icons.History
						toggleTip := updaterTr(showEn, "按版本查看", "By Version")
						if isByVer {
							toggleIc = icons.ListBullet
							toggleTip = updaterTr(showEn, "按分类查看", "By Category")
						}
						toggleBtn := internalui.IconButton(c, toggleIc, internalui.IconButtonOptions{
							ID:      "updater-view-toggle",
							Label:   toggleTip,
							Tooltip: toggleTip,
						})
						if toggleBtn.Clicked() {
							next := updater.ViewByVersion
							if isByVer {
								next = updater.ViewMerged
							}
							if m.updaterPreview != nil {
								m.updaterPreview.ViewMode = next
							} else if m.Updater != nil {
								m.Updater.SetViewMode(next)
							}
						}
					}

					cls := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
						ID:      "updater-close",
						Label:   updaterTr(showEn, "关闭", "Close"),
						Tooltip: updaterTr(showEn, "关闭", "Close"),
					})
					if cls.Clicked() {
						m.CloseUpdater()
					}
				})
			})

			// 2. Scrollable Release Notes Body (cardless, divider-free two-column list).
			notesScroll := internalui.Scroll(c.Key("updater-notes-scroll"), &m.updaterScroll).
				Column().FillWidth().MaxHeight(theme.UpdaterNotesMaxHeight).
				PaddingX(theme.Space20).Gap(theme.Space16)
			internalui.Probe(notesScroll, "updater-notes-scroll")
			notesScroll.Children(func() {
				if snap.Status == updater.StatusError {
					if snap.AllRoutesUnavailable() {
						errMsg := updaterTr(showEn, "更新源均不可用", "All update sources offline")
						ui.Text(c, errMsg).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(uPal.BadgeDangerFG).FillWidth()
					} else {
						errMsg := strings.TrimSpace(snap.ErrorMessage)
						if errMsg == "" {
							errMsg = updaterTr(showEn, "更新请求失败，请检查网络或切换更新源", "Update failed; check network or switch update source")
						}
						errEl := ui.Text(c, errMsg).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(pal.Danger).FillWidth().SingleLine().Ellipsis("…")
						internalui.WrappedTooltip(c, errEl, errMsg)
					}
				}

				if snap.ViewMode == updater.ViewByVersion && len(snap.Notes.Versions) > 0 {
					renderVersionHangingList(c, snap.Notes.Versions, showEn, pal)
					return
				}

				tagged := snap.Notes.TaggedForLanguage(showEn)
				if tagged.IsEmpty() {
					if snap.Status == updater.StatusError {
						return
					}
					emptyMsg := updaterTr(showEn, "暂无可展示的更新日志", "No release notes available yet")
					if snap.Status == updater.StatusChecking {
						emptyMsg = updaterTr(showEn, "正在获取更新日志…", "Loading release notes…")
					}
					ui.Row(c).FillWidth().PaddingY(theme.Space16).Justify(ui.Center).Children(func() {
						ui.Text(c, emptyMsg).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
					})
					return
				}
				renderTaggedHangingList(c, tagged, snap.Notes.VersionCount > 1, showEn, pal)
			})

			// 3. Divider-free Footer: Source Picker (hidden during download/install) + State Actions.
			ui.Column(c.Key("updater-footer")).FillWidth().PaddingX(theme.Space20).Children(func() {
				m.renderUpdaterFooter(c, snap, showEn, pal, uPal)
			})
		})
	})

	if !open && !wasSourceMenuOpen {
		m.CloseUpdater()
	}
}

func renderTaggedHangingList(
	c *ui.Context,
	notes updater.TaggedLocalizedNotes,
	showVersionTag bool,
	showEn bool,
	pal theme.Palette,
) {
	if hl := strings.TrimSpace(notes.Highlights); hl != "" {
		ui.Text(c.Key("merged-hl"), hl).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			TextColor(pal.Text).FillWidth()
	}

	type taggedGroup struct {
		id    string
		label string
		dot   ui.Color
		items []updater.TaggedBullet
	}
	groups := []taggedGroup{
		{id: "new", label: updaterTr(showEn, "新增", "New"), dot: pal.Accent, items: notes.New},
		{id: "imp", label: updaterTr(showEn, "优化", "Improved"), dot: pal.Success, items: notes.Improvements},
		{id: "fix", label: updaterTr(showEn, "修复", "Fixed"), dot: pal.Warning, items: notes.Fixes},
		{id: "other", label: updaterTr(showEn, "其他", "Other"), dot: pal.TextSecondary, items: notes.Other},
	}

	colW := theme.UpdaterCatColWidthZH
	if showEn {
		colW = theme.UpdaterCatColWidthEN
	}

	for _, g := range groups {
		if len(g.items) == 0 {
			continue
		}
		grp := g
		ui.Row(c.Key("merged-grp-"+grp.id)).FillWidth().MinWidth(0).
			Gap(theme.Space12).AlignItems(ui.Start).Children(func() {
			ui.Row(c).Width(colW).Height(theme.LineBody).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0).Children(func() {
				ui.Box(c).Size(theme.UpdaterBadgeDotSize, theme.UpdaterBadgeDotSize).
					Radius(theme.RadiusPill).Background(grp.dot).Shrink(0)
				ui.Text(c, grp.label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					TextColor(pal.TextSecondary).SingleLine()
			})

			ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space8).Children(func() {
				for i, bullet := range grp.items {
					b := bullet
					verTag := ""
					if showVersionTag && b.Version != "" {
						verTag = b.Version
						if !strings.HasPrefix(verTag, "v") {
							verTag = "v" + verTag
						}
					}
					renderUpdaterBulletText(c.Key(fmt.Sprintf("merged-%s-%d", grp.id, i)), b.Text, verTag, pal)
				}
			})
		})
	}
}

func renderVersionHangingList(
	c *ui.Context,
	versions []updater.ReleaseEntry,
	showEn bool,
	pal theme.Palette,
) {
	for _, verEntry := range versions {
		entry := verEntry
		sec := updater.VersionForLanguage(entry, showEn)
		var flat []string
		if hl := strings.TrimSpace(sec.Highlights); hl != "" {
			flat = append(flat, hl)
		}
		flat = append(flat, sec.New...)
		flat = append(flat, sec.Improvements...)
		flat = append(flat, sec.Fixes...)
		flat = append(flat, sec.Other...)

		ui.Row(c.Key("updater-ver-"+entry.Version)).FillWidth().MinWidth(0).
			Gap(theme.Space12).AlignItems(ui.Start).Children(func() {
			ui.Row(c).Width(theme.UpdaterVerColWidth).Height(theme.LineBody).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0).Children(func() {
				ui.Box(c).Size(theme.UpdaterBadgeDotSize, theme.UpdaterBadgeDotSize).
					Radius(theme.RadiusPill).Background(pal.Accent).Shrink(0)
				ui.Text(c, "v"+entry.Version).Font("monospace").FontSize(theme.FontSmall).
					FixedLineHeight(theme.LineBody).FontWeight(theme.WeightMono).
					TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
			})

			ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space8).Children(func() {
				if len(flat) == 0 {
					ui.Text(c, updaterTr(showEn, "常规稳定性与性能维护。", "Routine stability and maintenance improvements.")).
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary).FillWidth()
					return
				}
				for i, line := range flat {
					renderUpdaterBulletText(c.Key(fmt.Sprintf("ver-%s-%d", entry.Version, i)), line, "", pal)
				}
			})
		})
	}
}

func renderUpdaterBulletText(c *ui.Context, text, verTag string, pal theme.Palette) {
	var spans []ui.Span
	if prefix, rest, ok := splitUpdaterNotePrefix(text); ok {
		spans = append(spans,
			ui.Span{Text: prefix, Size: theme.FontBody, Color: pal.Placeholder},
			ui.Span{Text: rest, Size: theme.FontBody, Color: pal.Text},
		)
	} else {
		spans = append(spans, ui.Span{Text: text, Size: theme.FontBody, Color: pal.Text})
	}
	if verTag != "" {
		spans = append(spans, ui.Span{
			Text:  "  " + verTag,
			Font:  "monospace",
			Size:  theme.FontMeta,
			Color: pal.Placeholder,
		})
	}
	ui.RichText(c, spans...).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).FillWidth().MinWidth(0)
}

func splitUpdaterNotePrefix(text string) (prefix, rest string, ok bool) {
	if text == "" || strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
		return "", text, false
	}
	idx := -1
	sepLen := 0
	for i, r := range text {
		if r == '：' || r == ':' {
			if r == ':' && strings.HasPrefix(text[i:], "://") {
				return "", text, false
			}
			idx = i
			sepLen = utf8.RuneLen(r)
			break
		}
		if r == '\n' || r == '/' {
			return "", text, false
		}
	}
	if idx <= 0 {
		return "", text, false
	}
	head := strings.TrimSpace(text[:idx])
	runeLen := utf8.RuneCountInString(head)
	if runeLen < 1 || runeLen > 12 {
		return "", text, false
	}
	after := strings.TrimLeft(text[idx+sepLen:], " ")
	if after == "" {
		return "", text, false
	}
	sep := text[idx : idx+sepLen]
	if sep == ":" {
		sep = ": "
	}
	return head + sep, after, true
}

func (m *Manager) renderUpdaterFooter(
	c *ui.Context,
	snap updater.Snapshot,
	showEn bool,
	pal theme.Palette,
	uPal theme.UpdaterPalette,
) {
	switch snap.Status {
	case updater.StatusDownloading:
		ui.Row(c.Key("updater-footer-downloading")).FillWidth().AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
			abortBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
				ID:       "updater-abort-download",
				Label:    updaterTr(showEn, "取消", "Cancel"),
				Style:    internalui.ButtonGhost,
				Height:   theme.HeightLarge,
				PaddingX: theme.Space12,
			})
			if abortBtn.Clicked() && m.Updater != nil {
				m.Updater.CancelDownload()
			}

			var label string
			pct := float32(snap.Percent)
			if snap.Preparing {
				label = updaterTr(showEn, "正在准备更新…", "Preparing update…")
				pct = 100
			} else {
				label = fmt.Sprintf("%s %d%%", updaterTr(showEn, "下载中", "Downloading"), snap.Percent)
				if snap.SpeedMBps > 0.05 {
					label = fmt.Sprintf("%s · %.1f MB/s", label, snap.SpeedMBps)
				}
			}
			renderUpdaterProgressButton(c, label, pct, pal, uPal, false, theme.UpdaterModalWidth-theme.Space20*2-76)
		})

	case updater.StatusInstalling:
		ui.Row(c.Key("updater-footer-installing")).FillWidth().AlignItems(ui.Center).Children(func() {
			label := updaterTr(showEn, "正在保存笔记并重启…", "Saving notes and restarting…")
			renderUpdaterProgressButton(c, label, 100, pal, uPal, true, theme.UpdaterModalWidth-theme.Space20*2)
		})

	case updater.StatusReady:
		ui.Row(c.Key("updater-footer-ready")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).Children(func() {
			m.renderUpdaterSourcePicker(c, snap, showEn, pal, uPal)
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0).Children(func() {
				if snap.AllRoutesUnavailable() {
					fbBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-feedback",
						Label:    updaterTr(showEn, "反馈", "Feedback"),
						Style:    internalui.ButtonSecondary,
						Height:   theme.HeightLarge,
						PaddingX: theme.Space10,
					})
					if fbBtn.Clicked() {
						m.openUpdaterDiagnosticsFeedback(snap, showEn)
					}
				} else {
					laterBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-cancel",
						Label:    updaterTr(showEn, "稍后", "Later"),
						Style:    internalui.ButtonGhost,
						Height:   theme.HeightLarge,
						PaddingX: theme.Space10,
					})
					if laterBtn.Clicked() {
						m.CloseUpdater()
					}
				}
				submitBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
					ID:       "updater-submit",
					Label:    updaterTr(showEn, "重启并更新", "Restart & Update"),
					Style:    internalui.ButtonPrimary,
					Height:   theme.HeightLarge,
					PaddingX: theme.Space12,
				})
				if submitBtn.Clicked() && m.Updater != nil {
					m.Updater.InstallNow()
				}
			})
		})

	case updater.StatusAvailable:
		ui.Row(c.Key("updater-footer-available")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).Children(func() {
			m.renderUpdaterSourcePicker(c, snap, showEn, pal, uPal)
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0).Children(func() {
				if snap.AllRoutesUnavailable() {
					fbBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-feedback",
						Label:    updaterTr(showEn, "反馈", "Feedback"),
						Style:    internalui.ButtonSecondary,
						Height:   theme.HeightLarge,
						PaddingX: theme.Space10,
					})
					if fbBtn.Clicked() {
						m.openUpdaterDiagnosticsFeedback(snap, showEn)
					}
				} else {
					laterBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-cancel",
						Label:    updaterTr(showEn, "稍后", "Later"),
						Style:    internalui.ButtonGhost,
						Height:   theme.HeightLarge,
						PaddingX: theme.Space10,
					})
					if laterBtn.Clicked() {
						m.CloseUpdater()
					}
				}
				renderUpdaterAvailablePrimaryButton(c, m, snap, showEn)
			})
		})

	case updater.StatusError:
		ui.Row(c.Key("updater-footer-error")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).Children(func() {
			m.renderUpdaterSourcePicker(c, snap, showEn, pal, uPal)
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0).Children(func() {
				if snap.AllRoutesUnavailable() {
					fbBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-feedback",
						Label:    updaterTr(showEn, "反馈问题", "Feedback"),
						Style:    internalui.ButtonSecondary,
						Height:   theme.HeightLarge,
						PaddingX: theme.Space12,
					})
					if fbBtn.Clicked() {
						m.openUpdaterDiagnosticsFeedback(snap, showEn)
					}
					retryBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-submit",
						Label:    updaterTr(showEn, "重试", "Retry"),
						Style:    internalui.ButtonPrimary,
						Height:   theme.HeightLarge,
						PaddingX: theme.Space12,
					})
					if retryBtn.Clicked() && m.Updater != nil {
						m.Updater.CheckAsync(true)
					}
					return
				}
				if snap.LogPath != "" && (mygo.App.IsReady() || m.updaterPreview != nil) {
					logBtn := internalui.IconButton(c, icons.FileLog, internalui.IconButtonOptions{
						ID:           "updater-open-log",
						Label:        updaterTr(showEn, "查看日志", "View Log"),
						Tooltip:      updaterTr(showEn, "查看日志", "View Log"),
						TooltipAbove: true,
						Size:         theme.HeightLarge,
						IconSize:     theme.IconAction,
					})
					if logBtn.Clicked() && mygo.App.IsReady() {
						mygo.Shell.ShowItemInFolder(snap.LogPath)
					}
				}
				if snap.ReleaseURL != "" {
					webBtn := internalui.IconButton(c, icons.LinkOpen, internalui.IconButtonOptions{
						ID:           "updater-open-web",
						Label:        updaterTr(showEn, "下载页", "Releases"),
						Tooltip:      updaterTr(showEn, "下载页", "Releases"),
						TooltipAbove: true,
						Size:         theme.HeightLarge,
						IconSize:     theme.IconAction,
					})
					if webBtn.Clicked() {
						c.OpenURL(snap.ReleaseURL)
					}
				}
				retryBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
					ID:       "updater-submit",
					Label:    updaterTr(showEn, "重试", "Retry"),
					Style:    internalui.ButtonPrimary,
					Height:   theme.HeightLarge,
					PaddingX: theme.Space12,
				})
				if retryBtn.Clicked() && m.Updater != nil {
					m.Updater.CheckAsync(true)
				}
			})
		})

	default:
		ui.Row(c.Key("updater-footer-idle")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).Children(func() {
			m.renderUpdaterSourcePicker(c, snap, showEn, pal, uPal)
			if snap.AllRoutesUnavailable() {
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0).Children(func() {
					fbBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-feedback",
						Label:    updaterTr(showEn, "反馈问题", "Feedback"),
						Style:    internalui.ButtonSecondary,
						Height:   theme.HeightLarge,
						PaddingX: theme.Space12,
					})
					if fbBtn.Clicked() {
						m.openUpdaterDiagnosticsFeedback(snap, showEn)
					}
					retryBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-submit",
						Label:    updaterTr(showEn, "重试", "Retry"),
						Style:    internalui.ButtonPrimary,
						Height:   theme.HeightLarge,
						PaddingX: theme.Space12,
					})
					if retryBtn.Clicked() && m.Updater != nil {
						m.Updater.CheckAsync(true)
					}
				})
				return
			}
			checking := snap.Status == updater.StatusChecking
			label := updaterTr(showEn, "检查更新", "Check Now")
			if checking {
				label = updaterTr(showEn, "正在检查…", "Checking…")
			}
			refIc := icons.Refresh
			var leading *icons.Icon
			if checking {
				leading = &refIc
			}
			btn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
				ID:       "updater-submit",
				Label:    label,
				Leading:  leading,
				Style:    internalui.ButtonPrimary,
				Height:   theme.HeightLarge,
				PaddingX: theme.Space12,
				Disabled: checking,
				Loading:  checking,
			})
			if btn.Clicked() && m.Updater != nil {
				m.Updater.CheckAsync(true)
			}
		})
	}
}

func renderUpdaterAvailablePrimaryButton(c *ui.Context, m *Manager, snap updater.Snapshot, showEn bool) {
	if snap.PackageManaged {
		btn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
			ID:       "updater-submit",
			Label:    updaterTr(showEn, "前往下载页", "Open Releases"),
			Style:    internalui.ButtonPrimary,
			Height:   theme.HeightLarge,
			PaddingX: theme.Space12,
		})
		if btn.Clicked() && snap.ReleaseURL != "" {
			c.OpenURL(snap.ReleaseURL)
		}
		return
	}
	if snap.DownloadPath != "" && !snap.InPlace {
		btn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
			ID:       "updater-submit",
			Label:    updaterTr(showEn, "查看安装包", "Show Installer"),
			Style:    internalui.ButtonPrimary,
			Height:   theme.HeightLarge,
			PaddingX: theme.Space12,
		})
		if btn.Clicked() && mygo.App.IsReady() {
			mygo.Shell.ShowItemInFolder(snap.DownloadPath)
		}
		return
	}
	btn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
		ID:       "updater-submit",
		Label:    updaterTr(showEn, "立即更新", "Update Now"),
		Style:    internalui.ButtonPrimary,
		Height:   theme.HeightLarge,
		PaddingX: theme.Space12,
	})
	if btn.Clicked() && m.Updater != nil {
		m.Updater.StartDownload()
	}
}

func renderUpdaterProgressButton(
	c *ui.Context,
	label string,
	pct float32,
	pal theme.Palette,
	uPal theme.UpdaterPalette,
	fillWidth bool,
	fallbackW float32,
) {
	pct = max(0, min(pct, 100))
	track := ui.Box(c.Key("updater-progress-track")).
		Height(theme.HeightLarge).Radius(theme.RadiusMD).
		Background(uPal.ProgressTrack).Clip().
		Disabled(true).DimDisabled(false).
		Role(ui.RoleProgress).Label(label)
	if fillWidth {
		track.FillWidth()
	} else {
		track.Grow(1).MinWidth(0)
	}
	if b := track.Bounds(); b.W > 0 {
		fallbackW = b.W
	}
	track.Children(func() {
		ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().PaddingX(theme.Space12).Children(func() {
			ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine().Ellipsis("…")
		})
		if pct > 0 {
			ui.Box(c.Key("updater-progress-fill")).Absolute().Left(0).Top(0).Bottom(0).
				WidthPercent(pct).Background(uPal.ProgressFill).Clip().Children(func() {
				ui.Row(c).Absolute().Left(0).Top(0).Bottom(0).Width(fallbackW).Center().PaddingX(theme.Space12).Children(func() {
					ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(theme.WeightControl).TextColor(pal.AccentText).SingleLine().Ellipsis("…")
				})
			})
		}
	})
	internalui.Probe(track, "updater-progress-track")
}

func (m *Manager) openUpdaterDiagnosticsFeedback(snap updater.Snapshot, showEn bool) {
	diag := updater.BuildDiagnosticsReport(snap, m.dataDir, showEn)
	m.CloseUpdater()
	m.OpenFeedbackForUpdater(diag)
}

func renderSignalBars(c *ui.Context, litBars int, litColor, offColor ui.Color) {
	ui.Box(c).Size(theme.UpdaterSignalBoxW, theme.UpdaterSignalBoxH).Shrink(0).
		Draw(func(p *ui.Painter, r ui.Rect) {
			heights := [3]float32{theme.UpdaterSignalBarH1, theme.UpdaterSignalBarH2, theme.UpdaterSignalBarH3}
			for i := 0; i < 3; i++ {
				col := offColor
				if i < litBars {
					col = litColor
				}
				bh := heights[i]
				bx := r.X + float32(i)*(theme.UpdaterSignalBarW+theme.UpdaterSignalBarGap)
				by := r.Y + r.H - bh
				p.Fill(ui.Rect{X: bx, Y: by, W: theme.UpdaterSignalBarW, H: bh}, col, 1)
			}
		})
}

func (m *Manager) renderUpdaterSourcePicker(
	c *ui.Context,
	snap updater.Snapshot,
	showEn bool,
	pal theme.Palette,
	uPal theme.UpdaterPalette,
) {
	srcLabel := updaterSourceDisplayName(snap, showEn)
	effMs, effSt, effKind := updaterEffectiveHealth(snap)
	_, dotCol, badgeFG, badgeBG := updaterSignalStyle(effMs, effSt, pal, uPal)

	var badgeText string
	switch {
	case snap.RetestingTarget:
		badgeText = updaterTr(showEn, "复测…", "Retest…")
	case effSt == updater.ProbeProbing:
		badgeText = updaterTr(showEn, "测速…", "Probe…")
	case effSt == updater.ProbeFailed:
		if effKind == "timeout" {
			badgeText = updaterTr(showEn, "超时", "Timeout")
		} else {
			badgeText = updaterTr(showEn, "不可用", "Offline")
		}
	case effMs > 0:
		badgeText = fmt.Sprintf("%d ms", effMs)
	}

	open := m.updaterSourceMenu.Open
	btn := ui.ButtonBase(c.Key("updater-source-btn")).
		Height(theme.HeightLarge).Padding(0, theme.Space8, 0, theme.Space10).Gap(theme.Space6).
		AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
		MinWidth(0).Shrink(1).Label(srcLabel)
	hovered := btn.Hovered()
	pressed := btn.Pressed()
	fg := pal.Text
	bg := ui.Transparent
	switch {
	case pressed:
		bg = pal.Selected
	case hovered || open:
		bg = pal.Hover
	}
	internalui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
	internalui.TactileButton(btn, hovered, pressed, false)
	btn.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		m.updaterSourceMenu.TriggerRect = r
		if internalui.IsProbeEnabled() {
			internalui.RecordBounds("updater-source-btn", r)
		}
	})
	btn.Children(func() {
		ui.Box(c).Size(theme.UpdaterBadgeDotSize, theme.UpdaterBadgeDotSize).
			Radius(theme.RadiusPill).Background(dotCol).Shrink(0)
		ui.Text(c, srcLabel).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
			FontWeight(theme.WeightControl).TextColor(fg).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		if badgeText != "" {
			ui.Box(c).Padding(theme.Space2, theme.Space6).Radius(theme.RadiusBadge).
				Background(badgeBG).Shrink(0).Children(func() {
				ui.Text(c, badgeText).Font("monospace").
					FontSize(theme.FontMicro).FixedLineHeight(theme.LineMeta).
					FontWeight(theme.WeightMono).TextColor(badgeFG).SingleLine()
			})
		}
		chev := icons.ChevronUp
		if open {
			chev = icons.ChevronDown
		}
		icons.Draw(c, chev, theme.IconMicro, pal.MinimalIconColors(pal.Accent))
	})
	if btn.Clicked() {
		nextOpen := !m.updaterSourceMenu.Open
		m.updaterSourceMenu.Open = nextOpen
		m.updaterSourceMenu.Cursor = -1
		if nextOpen && m.updaterPreview == nil && m.Updater != nil {
			m.Updater.ProbeAllAsync()
		}
	}

	m.renderUpdaterSourcePopover(c, snap, showEn, pal, uPal)
}

func (m *Manager) renderUpdaterSourcePopover(
	c *ui.Context,
	snap updater.Snapshot,
	showEn bool,
	pal theme.Palette,
	uPal theme.UpdaterPalette,
) {
	st := &m.updaterSourceMenu
	if !st.Open || st.TriggerRect.W <= 0 || st.TriggerRect.H <= 0 {
		return
	}
	winW, winH := c.Size()
	if winW <= 0 {
		winW = 1100
	}
	if winH <= 0 {
		winH = 760
	}
	popW := min(theme.UpdaterSourceMenuWidth, max(winW-theme.Space16, 160))
	allUnavailable := snap.AllRoutesUnavailable()

	// Exact deterministic height keeps bottom edge cleanly anchored above TriggerRect.
	estH := 2 + theme.Space6*2 + theme.UpdaterSourcePopoverHeader + theme.UpdaterSourceAutoCardH +
		1 + theme.UpdaterSourceChannelRowH*2 + theme.Space4*4
	if allUnavailable {
		estH += theme.UpdaterSourceErrorCardH + theme.Space4
	}

	spec := internalui.PlaceAbove(st.TriggerRect).
		WithAlignX(internalui.AlignX{Kind: internalui.AlignLeft}).
		WithGap(theme.Space6).
		WithMargin(theme.Space8).
		WithReserveHeight(estH)
	maxH := internalui.MaxHeightForSpec(spec, winW, winH, estH+theme.Space16)
	x, y := internalui.ResolveOrigin(spec, popW, min(estH, maxH), winW, winH)

	mirrorMs, mirrorSt, mirrorKind := updaterChannelHealth(snap, updater.ChannelMirror)
	githubMs, githubSt, githubKind := updaterChannelHealth(snap, updater.ChannelGitHub)
	anyProbing := snap.ProbingChannels || mirrorSt == updater.ProbeProbing || githubSt == updater.ProbeProbing

	bestCh := ""
	switch {
	case mirrorSt == updater.ProbeOK && githubSt == updater.ProbeOK:
		if githubMs < mirrorMs {
			bestCh = updater.ChannelGitHub
		} else {
			bestCh = updater.ChannelMirror
		}
	case mirrorSt == updater.ProbeOK:
		bestCh = updater.ChannelMirror
	case githubSt == updater.ProbeOK:
		bestCh = updater.ChannelGitHub
	}

	var pickedMode *updater.SourceMode
	clickedReprobe := false
	clickedFeedback := false

	ui.Overlay(c, func() {
		backdrop := ui.Box(c.Key("updater-source-menu-select-backdrop")).Absolute().Left(0).Top(0).Right(0).Bottom(0)

		panel := internalui.MenuSurface(
			ui.Column(c.Key("updater-source-menu-menu")).
				Absolute().Left(x).Top(y).Width(popW).MaxHeight(maxH),
		).Padding(theme.Space6).Gap(theme.Space4)
		internalui.Probe(panel, "updater-source-menu-menu")

		panel.Children(func() {
			// 1. Popover Header: "下载线路" + "重测" button (stays open).
			ui.Row(c.Key("updater-source-popover-header")).FillWidth().
				Height(theme.UpdaterSourcePopoverHeader).Padding(0, theme.Space6, 0, theme.Space8).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, updaterTr(showEn, "下载线路", "Route")).
					FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					FontWeight(theme.WeightTitle).TextColor(pal.TextSecondary).SingleLine()

				reprobeBtn := ui.ButtonBase(c.Key("updater-source-reprobe")).
					Height(theme.UpdaterSourcePopoverHeader).PaddingX(theme.Space6).
					Gap(theme.Space4).AlignItems(ui.Center).Radius(theme.RadiusSM).
					Cursor(ui.CursorPointer).Label(updaterTr(showEn, "重新测速", "Retest"))
				rHover := reprobeBtn.Hovered()
				rPress := reprobeBtn.Pressed()
				rBG := ui.Transparent
				rFG := pal.TextSecondary
				if rPress {
					rBG = pal.Selected
					rFG = pal.Text
				} else if rHover {
					rBG = pal.Hover
					rFG = pal.Text
				}
				internalui.AnimateSurfaceColors(reprobeBtn, rBG, ui.Transparent, false)
				reprobeBtn.Children(func() {
					if anyProbing {
						ui.Spinner(c).FontSize(theme.IconMicro).TextColor(pal.Accent)
					} else {
						icons.DrawAnimated(c, icons.Refresh, theme.IconMicro, pal.MinimalIconColors(rFG), rHover || rPress)
					}
					ui.Text(c, updaterTr(showEn, "重测", "Retest")).
						FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(rFG).SingleLine()
				})
				internalui.Probe(reprobeBtn, "updater-source-reprobe")
				if reprobeBtn.Clicked() {
					clickedReprobe = true
				}
			})

			// 2. Smart Auto 2-line Card.
			isAuto := snap.SourceMode == updater.SourceAuto
			autoSub := updaterTr(showEn, "自动择优", "Fastest route")
			autoSubCol := pal.TextSecondary
			switch {
			case anyProbing:
				autoSub = updaterTr(showEn, "择优中…", "Probing…")
			case allUnavailable:
				if mirrorKind == "timeout" && githubKind == "timeout" {
					autoSub = updaterTr(showEn, "全部超时", "All timed out")
				} else {
					autoSub = updaterTr(showEn, "全部不可用", "All offline")
				}
				autoSubCol = uPal.BadgeDangerFG
			case bestCh == updater.ChannelMirror:
				autoSub = updaterTr(showEn, "优选 国内镜像", "Using CN Mirror")
			case bestCh == updater.ChannelGitHub:
				autoSub = updaterTr(showEn, "优选 GitHub", "Using GitHub")
			}

			autoBtn := ui.ButtonBase(c.Key("updater-src-auto")).
				FillWidth().Height(theme.UpdaterSourceAutoCardH).
				Padding(theme.Space8, theme.Space10).Gap(theme.Space8).
				AlignItems(ui.Start).Radius(theme.RadiusLG).Cursor(ui.CursorPointer).
				Label(updaterTr(showEn, "智能自动", "Smart Auto"))
			aHover := autoBtn.Hovered()
			aPress := autoBtn.Pressed()
			aBG := ui.Transparent
			aBD := ui.Transparent
			hasABD := false
			switch {
			case isAuto:
				aBG = pal.Selected
				if !allUnavailable {
					aBD = pal.Accent
					hasABD = true
				}
			case aPress:
				aBG = pal.Selected
			case aHover:
				aBG = pal.Hover
			}
			internalui.AnimateSurfaceColors(autoBtn, aBG, aBD, hasABD)
			internalui.Probe(autoBtn, "updater-src-auto")
			autoBtn.Children(func() {
				zapColors := pal.LitDuoIconColors(pal.Accent)
				if allUnavailable {
					zapColors = pal.TintedIconColors(uPal.BadgeDangerFG)
				}
				ui.Box(c).Size(theme.IconAction, theme.LineBody).Center().Shrink(0).Children(func() {
					icons.Draw(c, icons.Zap, theme.IconInline, zapColors)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
					ui.Text(c, updaterTr(showEn, "智能自动", "Smart Auto")).
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine().Ellipsis("…")
					ui.Text(c, autoSub).
						FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(autoSubCol).SingleLine().Ellipsis("…")
				})
				ui.Box(c).Size(theme.IconAction, theme.LineBody).Center().Shrink(0).Children(func() {
					if isAuto {
						icons.Draw(c, icons.Check, theme.IconInline, pal.MinimalIconColors(pal.Accent))
					}
				})
			})
			if autoBtn.Clicked() {
				mode := updater.SourceAuto
				pickedMode = &mode
			}

			// 3. Divider.
			ui.Box(c).FillWidth().Height(1).Shrink(0).Background(pal.Hairline)

			// 4. Physical route rows: CN Mirror & GitHub.
			routes := []struct {
				id      string
				mode    updater.SourceMode
				channel string
				label   string
				ms      int64
				st      updater.ProbeStatus
				errKind string
			}{
				{
					id:      "updater-src-mirror",
					mode:    updater.SourceMirror,
					channel: updater.ChannelMirror,
					label:   updaterTr(showEn, "国内镜像", "CN Mirror"),
					ms:      mirrorMs,
					st:      mirrorSt,
					errKind: mirrorKind,
				},
				{
					id:      "updater-src-github",
					mode:    updater.SourceGitHub,
					channel: updater.ChannelGitHub,
					label:   "GitHub",
					ms:      githubMs,
					st:      githubSt,
					errKind: githubKind,
				},
			}

			for _, r := range routes {
				route := r
				checked := snap.SourceMode == route.mode
				bars, barCol, rowFG, _ := updaterSignalStyle(route.ms, route.st, pal, uPal)
				if route.st == updater.ProbeProbing || route.st == updater.ProbeIdle {
					rowFG = pal.Placeholder
				}
				var msText string
				switch route.st {
				case updater.ProbeProbing:
					msText = updaterTr(showEn, "测速中…", "Probing…")
				case updater.ProbeOK:
					if route.ms > 0 {
						msText = fmt.Sprintf("%d ms", route.ms)
					} else {
						msText = "--"
					}
				case updater.ProbeFailed:
					if route.errKind == "timeout" {
						msText = updaterTr(showEn, "超时", "Timeout")
					} else {
						msText = updaterTr(showEn, "不可用", "Offline")
					}
				default:
					msText = "--"
				}

				rowBtn := ui.ButtonBase(c.Key(route.id)).
					FillWidth().Height(theme.UpdaterSourceChannelRowH).
					PaddingX(theme.Space10).Gap(theme.Space8).
					AlignItems(ui.Center).Radius(theme.RadiusMD).
					Cursor(ui.CursorPointer).Label(route.label)
				rHover := rowBtn.Hovered()
				rPress := rowBtn.Pressed()
				rBG := ui.Transparent
				if rPress {
					rBG = pal.Selected
				} else if rHover {
					rBG = pal.Hover
				}
				internalui.AnimateSurfaceColors(rowBtn, rBG, ui.Transparent, false)
				internalui.Probe(rowBtn, route.id)
				rowBtn.Children(func() {
					renderSignalBars(c, bars, barCol, uPal.SignalBarOff)
					ui.Text(c, route.label).
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(theme.WeightControl).TextColor(pal.Text).
						SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
					if !anyProbing && bestCh == route.channel && route.st == updater.ProbeOK {
						ui.Box(c).Padding(0, theme.Space6).Radius(theme.RadiusXS).
							Background(uPal.BadgeGoodBG).Shrink(0).Children(func() {
							ui.Text(c, updaterTr(showEn, "最快", "Fastest")).
								FontSize(theme.FontMicro).FixedLineHeight(theme.LineMeta).
								FontWeight(theme.WeightTitle).TextColor(uPal.BadgeGoodFG).SingleLine()
						})
					}
					ui.Box(c).Grow(1).MinWidth(0)
					ui.Text(c, msText).Font("monospace").
						FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(rowFG).SingleLine().Shrink(0)
					ui.Box(c).Size(theme.IconAction, theme.IconAction).Center().Shrink(0).Children(func() {
						if checked {
							icons.Draw(c, icons.Check, theme.IconInline, pal.MinimalIconColors(pal.Accent))
						}
					})
				})
				if rowBtn.Clicked() {
					mMode := route.mode
					pickedMode = &mMode
				}
			}

			// 5. All 3 routes unavailable inline banner inside popover.
			if allUnavailable {
				ui.Row(c.Key("updater-popover-error-card")).
					FillWidth().Height(theme.UpdaterSourceErrorCardH).
					PaddingX(theme.Space10).Gap(theme.Space8).
					AlignItems(ui.Center).Justify(ui.SpaceBetween).
					Radius(theme.RadiusLG).Background(uPal.BadgeDangerBG).
					Border(1, uPal.BadgeDangerBorder).Children(func() {
					ui.Text(c, updaterTr(showEn, "线路均不可用", "All routes offline")).
						FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						FontWeight(theme.WeightTitle).TextColor(uPal.BadgeDangerFG).
						Grow(1).MinWidth(0).SingleLine().Ellipsis("…")

					fbBtn := ui.ButtonBase(c.Key("updater-popover-feedback")).
						Height(theme.HeightCompact).PaddingX(theme.Space10).
						Center().Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
						Background(pal.Paper).Border(1, uPal.BadgeDangerBorder).
						Shrink(0).Label(updaterTr(showEn, "反馈", "Feedback"))
					if fbBtn.Pressed() {
						fbBtn.Background(pal.Selected)
					} else if fbBtn.Hovered() {
						fbBtn.Background(pal.Hover)
					}
					fbBtn.Children(func() {
						ui.Text(c, updaterTr(showEn, "反馈", "Feedback")).
							FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							FontWeight(theme.WeightTitle).TextColor(uPal.BadgeDangerFG).SingleLine()
					})
					internalui.Probe(fbBtn, "updater-popover-feedback")
					if fbBtn.Clicked() {
						clickedFeedback = true
					}
				})
			}
		})

		switch {
		case clickedReprobe:
			if m.updaterPreview == nil && m.Updater != nil {
				m.Updater.ProbeAllAsync()
			}
		case clickedFeedback:
			st.Open = false
			m.openUpdaterDiagnosticsFeedback(snap, showEn)
		case pickedMode != nil:
			mode := *pickedMode
			st.Open = false
			if m.updaterPreview != nil {
				m.updaterPreview.SourceMode = mode
			} else if m.Updater != nil {
				m.Updater.SelectSourceAndRetest(mode)
			} else {
				m.mutateState(func(s *model.AppState) {
					s.UpdateSource = string(mode)
				})
			}
		case backdrop.Clicked() || backdrop.OverlayShortcut(0, ui.KeyEscape):
			st.Open = false
		}
	})
}
