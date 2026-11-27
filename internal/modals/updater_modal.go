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

func updaterSourceLabel(mode updater.SourceMode, showEn bool) string {
	switch mode {
	case updater.SourceMirror:
		return updaterTr(showEn, "国内镜像", "CN Mirror")
	case updater.SourceGitHub:
		return "GitHub"
	default:
		return updaterTr(showEn, "自动", "Auto")
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
					errMsg := strings.TrimSpace(snap.ErrorMessage)
					if errMsg == "" {
						errMsg = updaterTr(showEn, "更新请求失败，请检查网络或切换更新源", "Update failed; check network or switch update source")
					}
					errEl := ui.Text(c, errMsg).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
						TextColor(pal.Danger).FillWidth().SingleLine().Ellipsis("…")
					internalui.WrappedTooltip(c, errEl, errMsg)
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
			m.renderUpdaterSourcePicker(c, snap, showEn, pal)
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0).Children(func() {
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
			m.renderUpdaterSourcePicker(c, snap, showEn, pal)
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0).Children(func() {
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
				renderUpdaterAvailablePrimaryButton(c, m, snap, showEn)
			})
		})

	case updater.StatusError:
		ui.Row(c.Key("updater-footer-error")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).Children(func() {
			m.renderUpdaterSourcePicker(c, snap, showEn, pal)
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0).Children(func() {
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
			m.renderUpdaterSourcePicker(c, snap, showEn, pal)
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

func (m *Manager) renderUpdaterSourcePicker(
	c *ui.Context,
	snap updater.Snapshot,
	showEn bool,
	pal theme.Palette,
) {
	srcLabel := updaterSourceLabel(snap.SourceMode, showEn)
	dotCol := pal.Placeholder
	switch {
	case snap.Status == updater.StatusError:
		dotCol = pal.Danger
	case snap.FetchLatencyMs > 1000:
		dotCol = pal.Warning
	case snap.FetchLatencyMs > 0 || snap.ActiveChannel != "":
		dotCol = pal.Success
	}

	open := m.updaterSourceMenu.Open
	btn := ui.ButtonBase(c.Key("updater-source-btn")).
		Height(theme.HeightLarge).PaddingX(theme.Space8).Gap(theme.Space6).
		AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
		MinWidth(0).Shrink(1).Label(srcLabel)
	hovered := btn.Hovered()
	pressed := btn.Pressed()
	fg := pal.TextSecondary
	bg := ui.Transparent
	switch {
	case pressed:
		fg = pal.Text
		bg = pal.Selected
	case hovered || open:
		fg = pal.Text
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
			FontWeight(theme.WeightControl).TextColor(fg).SingleLine().Ellipsis("…").MinWidth(0)
		if snap.FetchLatencyMs > 0 {
			ui.Text(c, fmt.Sprintf("%d ms", snap.FetchLatencyMs)).Font("monospace").
				FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
				TextColor(pal.Placeholder).SingleLine().Shrink(0)
		}
		chev := icons.ChevronUp
		if open {
			chev = icons.ChevronDown
		}
		icons.DrawAnimated(c, chev, theme.IconMicro, pal.DuoIconColors(fg), open || hovered || pressed)
	})
	if btn.Clicked() {
		m.updaterSourceMenu.Open = !m.updaterSourceMenu.Open
		m.updaterSourceMenu.Cursor = -1
	}

	options := []internalui.SelectOption{
		{ID: "updater-src-auto", Value: string(updater.SourceAuto), Label: updaterTr(showEn, "自动", "Auto")},
		{ID: "updater-src-mirror", Value: string(updater.SourceMirror), Label: updaterTr(showEn, "国内镜像", "CN Mirror")},
		{ID: "updater-src-github", Value: string(updater.SourceGitHub), Label: "GitHub"},
	}
	if picked := internalui.SelectMenu(c, "updater-source-menu", options, &m.updaterSourceMenu, string(snap.SourceMode)); picked >= 0 {
		mode := updater.SourceMode(options[picked].Value)
		if m.updaterPreview != nil {
			m.updaterPreview.SourceMode = mode
		} else if m.Updater != nil {
			m.Updater.SetSourceMode(mode)
			if snap.Status == updater.StatusError || snap.Status == updater.StatusIdle || snap.Status == updater.StatusUpToDate {
				m.Updater.CheckAsync(true)
			}
		} else {
			m.mutateState(func(s *model.AppState) {
				s.UpdateSource = string(mode)
			})
		}
	}
}
