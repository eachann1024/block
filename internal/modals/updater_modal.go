package modals

import (
	"fmt"
	"strings"

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
	if m.Updater != nil {
		snap := m.Updater.Snapshot()
		if snap.Status == updater.StatusIdle {
			m.Updater.CheckAsync(true)
		}
	}
	m.requestRedraw()
}

// CloseUpdater closes the application update dialog without interrupting background downloads.
func (m *Manager) CloseUpdater() {
	if !m.updaterOpen {
		return
	}
	m.updaterOpen = false
	m.requestRedraw()
}

// IsUpdaterOpen reports whether the updater dialog is open.
func (m *Manager) IsUpdaterOpen() bool {
	return m.updaterOpen
}

func updaterChannelLabel(channel string) string {
	switch channel {
	case "mirror":
		return theme.Tr("国内镜像", "Mirror")
	case "github":
		return "GitHub"
	default:
		return ""
	}
}

func formatMegabytes(bytes int64) string {
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024.0*1024.0))
}

func updaterModalTitle(snap updater.Snapshot) string {
	switch snap.Status {
	case updater.StatusReady:
		return theme.Tr("新版本已就绪", "Update Ready")
	case updater.StatusAvailable, updater.StatusDownloading, updater.StatusInstalling:
		return theme.Tr("发现新版本", "Update Available")
	default:
		return theme.Tr("检查更新", "App Updates")
	}
}

func updaterFooterStatusText(snap updater.Snapshot) (text string, isErr bool) {
	if snap.ErrorMessage != "" {
		return snap.ErrorMessage, true
	}
	switch snap.Status {
	case updater.StatusChecking:
		return theme.Tr("正在连接更新源检查新版本…", "Checking update sources…"), false
	case updater.StatusDownloading:
		return theme.Tr("关掉窗口也会在后台继续下载", "Closing this window keeps the download running"), false
	case updater.StatusReady:
		return theme.Tr("保存当前笔记并重启即可完成更新", "Save open notes and restart to finish updating"), false
	case updater.StatusInstalling:
		return theme.Tr("正在保存笔记并重启 Block…", "Saving notes and restarting Block…"), false
	case updater.StatusAvailable:
		if snap.PackageManaged {
			return theme.Tr("当前通过系统包管理器安装，请通过包管理器或下载页升级", "Installed via package manager; update there or from Releases"), false
		}
		if snap.DownloadPath != "" && !snap.InPlace {
			return theme.Tr("安装包已下载完成，打开即可安装", "Installer downloaded and ready to open"), false
		}
		if snap.TotalBytes > 0 {
			return fmt.Sprintf("%s · %s", theme.Tr("安装包大小", "Package size"), formatMegabytes(snap.TotalBytes)), false
		}
		return fmt.Sprintf("%s v%s", theme.Tr("当前版本", "Current version"), snap.CurrentVersion), false
	default:
		return theme.Tr("当前已是最新版本", "You're up to date"), false
	}
}

func (m *Manager) renderUpdaterModal(c *ui.Context) {
	pal := theme.Ocean()
	uPal := pal.UpdaterColors()
	open := true

	var snap updater.Snapshot
	if m.Updater != nil {
		snap = m.Updater.Snapshot()
	} else {
		cur := updater.CurrentVersion()
		snap = updater.Snapshot{
			Status:         updater.StatusUpToDate,
			CurrentVersion: cur,
			LatestVersion:  cur,
			SourceMode:     updater.SourceAuto,
			ViewMode:       updater.ViewMerged,
			Notes: updater.AggregatedNotes{
				FromVersion: cur,
				ToVersion:   cur,
			},
		}
	}

	title := updaterModalTitle(snap)
	busy := snap.Status == updater.StatusChecking || snap.Status == updater.StatusDownloading || snap.Status == updater.StatusInstalling

	internalui.ModalCard(c, "updater-modal", title, &open, theme.UpdaterModalWidth, theme.UpdaterModalMaxHeight, func() {
		ui.Column(c).FillWidth().Padding(theme.Space20, theme.Space24, theme.Space20, theme.Space24).Gap(theme.Space16).Children(func() {
			// 1. Header: Icon + Title + Version Span Pill + Channel Pill + Close Button.
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space10).Grow(1).MinWidth(0).Children(func() {
					icons.DrawAnimated(c, icons.UpdateTangle, theme.IconRow, pal.DuoIconColors(pal.Accent), busy)
					ui.Text(c, title).FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).
						FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()

					verLabel := fmt.Sprintf("v%s", snap.CurrentVersion)
					if snap.Notes.FromVersion != "" && snap.Notes.ToVersion != "" &&
						updater.CompareVersion(snap.Notes.FromVersion, snap.Notes.ToVersion) < 0 {
						verLabel = fmt.Sprintf("v%s → v%s", snap.Notes.FromVersion, snap.Notes.ToVersion)
					}
					ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).AlignItems(ui.Center).
						Radius(theme.RadiusPill).Background(uPal.VersionPillBG).Border(1, uPal.VersionPillBorder).Children(func() {
						ui.Text(c, verLabel).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							FontWeight(theme.WeightMono).TextColor(uPal.VersionPillFG).SingleLine()
					})

					if snap.Notes.VersionCount > 1 {
						countText := fmt.Sprintf(theme.Tr("汇总 %d 个版本", "%d releases"), snap.Notes.VersionCount)
						ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).AlignItems(ui.Center).
							Radius(theme.RadiusPill).Background(uPal.SourcePillBG).Border(1, uPal.SourcePillBorder).Children(func() {
							ui.Text(c, countText).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								FontWeight(theme.WeightControl).TextColor(uPal.SourcePillFG).SingleLine()
						})
					}
				})

				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Shrink(0).Children(func() {
					if chText := updaterChannelLabel(snap.ActiveChannel); chText != "" {
						ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).AlignItems(ui.Center).
							Radius(theme.RadiusSM).Background(uPal.SourcePillBG).Border(1, uPal.SourcePillBorder).Children(func() {
							ui.Text(c, chText).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								FontWeight(theme.WeightRegular).TextColor(uPal.SourcePillFG).SingleLine()
						})
					}
					cls := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
						ID:    "updater-close",
						Label: theme.Tr("关闭", "Close"),
					})
					if cls.Clicked() {
						m.CloseUpdater()
					}
				})
			})

			// 2. Controls Bar: View Mode (Merged vs By-Version) + Source Selector (Auto / Mirror / GitHub).
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space10).Children(func() {
				if len(snap.Notes.Versions) > 1 && m.Updater != nil {
					viewIdx := 0
					if snap.ViewMode == updater.ViewByVersion {
						viewIdx = 1
					}
					if internalui.SegmentedControlWithOptions(c, "updater-view-seg", &viewIdx, []internalui.SegmentOption{
						{ID: "updater-view-merged", Label: theme.Tr("分类汇总", "Summary")},
						{ID: "updater-view-versions", Label: theme.Tr("按版本", "By Version")},
					}, internalui.SegmentedControlOptions{ShellTrack: true}) {
						if viewIdx == 1 {
							m.Updater.SetViewMode(updater.ViewByVersion)
						} else {
							m.Updater.SetViewMode(updater.ViewMerged)
						}
					}
				} else {
					ui.Text(c, fmt.Sprintf("%s v%s", theme.Tr("当前运行", "Installed"), snap.CurrentVersion)).
						FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
				}

				srcIdx := 0
				switch snap.SourceMode {
				case updater.SourceMirror:
					srcIdx = 1
				case updater.SourceGitHub:
					srcIdx = 2
				}
				if internalui.SegmentedControlWithOptions(c, "updater-source-seg", &srcIdx, []internalui.SegmentOption{
					{ID: "updater-src-auto", Label: theme.Tr("自动优选", "Auto")},
					{ID: "updater-src-mirror", Label: theme.Tr("国内加速", "Mirror")},
					{ID: "updater-src-github", Label: "GitHub"},
				}, internalui.SegmentedControlOptions{ShellTrack: true}) {
					modes := []updater.SourceMode{updater.SourceAuto, updater.SourceMirror, updater.SourceGitHub}
					picked := modes[srcIdx]
					if m.Updater != nil {
						m.Updater.SetSourceMode(picked)
						if snap.Status == updater.StatusError || snap.Status == updater.StatusIdle {
							m.Updater.CheckAsync(true)
						}
					} else {
						m.mutateState(func(s *model.AppState) {
							s.UpdateSource = string(picked)
						})
					}
				}
			})

			// 3. Scrollable Bilingual Release Notes Body.
			notesCard := internalui.Scroll(c.Key("updater-notes-scroll"), &m.updaterScroll).
				Column().FillWidth().MaxHeight(theme.UpdaterNotesMaxHeight).
				Padding(theme.Space16).Gap(theme.Space16).
				Radius(theme.RadiusLG).Background(pal.Shell).Border(1, pal.Hairline)
			internalui.Probe(notesCard, "updater-notes-scroll")
			notesCard.Children(func() {
				isEn := theme.IsEn()
				if snap.ViewMode == updater.ViewByVersion && len(snap.Notes.Versions) > 1 {
					for idx, verEntry := range snap.Notes.Versions {
						if idx > 0 {
							ui.Box(c.Key(fmt.Sprintf("updater-ver-sep-%d", idx))).FillWidth().Height(1).Background(pal.Hairline)
						}
						sec := updater.VersionForLanguage(verEntry, isEn)
						ui.Column(c.Key("updater-ver-" + verEntry.Version)).FillWidth().Gap(theme.Space10).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
								ui.Text(c, "v"+verEntry.Version).Font("monospace").FontSize(theme.FontTitle).
									FixedLineHeight(theme.LineTitle).FontWeight(theme.WeightMono).TextColor(pal.Text)
								if d := updater.ShortDate(verEntry.PublishedAt); d != "" {
									ui.Text(c, d).Font("monospace").FontSize(theme.FontMeta).
										FixedLineHeight(theme.LineMeta).TextColor(pal.TextSecondary)
								}
							})
							renderLocalizedNotesSections(c, verEntry.Version, sec, pal, uPal)
						})
					}
					return
				}

				merged := snap.Notes.ForLanguage(isEn)
				if merged.IsEmpty() {
					emptyMsg := theme.Tr("暂无可展示的更新日志", "No release notes available yet")
					if snap.Status == updater.StatusChecking {
						emptyMsg = theme.Tr("正在获取更新日志…", "Loading release notes…")
					}
					ui.Row(c).FillWidth().PaddingY(theme.Space20).Justify(ui.Center).Children(func() {
						ui.Text(c, emptyMsg).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
					})
					return
				}
				renderLocalizedNotesSections(c, "merged", merged, pal, uPal)
			})

			// 4. Live Download / Ready Progress Bar.
			if snap.Status == updater.StatusDownloading || snap.Status == updater.StatusReady {
				ui.Column(c.Key("updater-progress-box")).FillWidth().Gap(theme.Space6).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
						var leftLabel string
						switch {
						case snap.Status == updater.StatusReady:
							leftLabel = fmt.Sprintf("%s v%s", theme.Tr("已就绪", "Ready"), snap.LatestVersion)
						case snap.Preparing:
							leftLabel = theme.Tr("校验通过，正在解包准备更新…", "Checksum verified, preparing bundle…")
						case snap.TotalBytes > 0:
							leftLabel = fmt.Sprintf("%s（%s / %s）",
								theme.Tr("正在下载", "Downloading"),
								formatMegabytes(snap.DownloadedBytes),
								formatMegabytes(snap.TotalBytes),
							)
						default:
							leftLabel = theme.Tr("正在下载更新包…", "Downloading update…")
						}
						ui.Text(c, leftLabel).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.Text)
						pct := snap.Percent
						if snap.Status == updater.StatusReady {
							pct = 100
						}
						ui.Text(c, fmt.Sprintf("%d%%", pct)).Font("monospace").FontSize(theme.FontMeta).
							FixedLineHeight(theme.LineMeta).FontWeight(theme.WeightMono).TextColor(pal.TextSecondary)
					})

					track := ui.Box(c.Key("updater-progress-track")).FillWidth().Height(theme.UpdaterProgressHeight).
						Radius(theme.RadiusPill).Background(uPal.ProgressTrack).Clip()
					track.Children(func() {
						pct := float32(snap.Percent)
						if snap.Status == updater.StatusReady {
							pct = 100
						}
						if pct < 2 && snap.Status == updater.StatusDownloading {
							pct = 2
						}
						ui.Box(c.Key("updater-progress-fill")).WidthPercent(pct).Height(theme.UpdaterProgressHeight).
							Radius(theme.RadiusPill).Background(uPal.ProgressFill)
					})
				})
			}

			ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)

			// 5. Footer Status & State-Driven Primary Action.
			statusText, isErr := updaterFooterStatusText(snap)
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Grow(1).MinWidth(0).Children(func() {
					statusCol := pal.TextSecondary
					if isErr {
						statusCol = pal.Danger
					}
					ui.Text(c, statusText).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
						TextColor(statusCol).Grow(1).MinWidth(0)
					if isErr && snap.ReleaseURL != "" {
						webBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
							ID:      "updater-open-web",
							Label:   theme.Tr("下载页", "Releases"),
							Style:   internalui.ButtonGhost,
							Height:  theme.HeightCompact,
							PaddingX: theme.Space8,
						})
						if webBtn.Clicked() {
							c.OpenURL(snap.ReleaseURL)
						}
					}
				})

				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space10).Shrink(0).Children(func() {
					dismissLabel := theme.Tr("先不更新", "Not Now")
					if !snap.HasActionableUpdate() {
						dismissLabel = theme.Tr("关闭", "Close")
					}
					cancelBtn := internalui.ActionButton(c, "updater-cancel", dismissLabel, nil, internalui.ButtonSecondary, false)
					if cancelBtn.Clicked() {
						m.CloseUpdater()
					}

					renderUpdaterPrimaryButton(c, m, snap)
				})
			})
		})
	})

	if !open {
		m.CloseUpdater()
	}
}

func renderLocalizedNotesSections(
	c *ui.Context,
	keyPrefix string,
	notes updater.LocalizedNotes,
	pal theme.Palette,
	uPal theme.UpdaterPalette,
) {
	if hl := strings.TrimSpace(notes.Highlights); hl != "" {
		ui.Row(c.Key(keyPrefix+"-hl")).FillWidth().Padding(theme.Space10, theme.Space12).
			Radius(theme.RadiusMD).Background(pal.Card).Border(1, pal.Hairline).Children(func() {
			ui.Text(c, hl).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1)
		})
	}

	type catGroup struct {
		id    string
		title string
		fg    ui.Color
		bg    ui.Color
		items []string
	}
	groups := []catGroup{
		{
			id:    "new",
			title: theme.Tr("新功能", "New"),
			fg:    uPal.CategoryNewFG,
			bg:    uPal.CategoryNewBG,
			items: notes.New,
		},
		{
			id:    "imp",
			title: theme.Tr("改进", "Improvements"),
			fg:    uPal.CategoryImpFG,
			bg:    uPal.CategoryImpBG,
			items: notes.Improvements,
		},
		{
			id:    "fix",
			title: theme.Tr("修复", "Fixes"),
			fg:    uPal.CategoryFixFG,
			bg:    uPal.CategoryFixBG,
			items: notes.Fixes,
		},
		{
			id:    "other",
			title: theme.Tr("其他", "Other"),
			fg:    pal.TextSecondary,
			bg:    pal.Card,
			items: notes.Other,
		},
	}

	for _, g := range groups {
		if len(g.items) == 0 {
			continue
		}
		grp := g
		ui.Column(c.Key(keyPrefix+"-sec-"+grp.id)).FillWidth().Gap(theme.Space8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
				ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).AlignItems(ui.Center).
					Radius(theme.RadiusSM).Background(grp.bg).Children(func() {
					ui.Text(c, grp.title).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						FontWeight(theme.WeightTitle).TextColor(grp.fg).SingleLine()
				})
				ui.Text(c, fmt.Sprintf("%d", len(grp.items))).Font("monospace").FontSize(theme.FontMeta).
					FixedLineHeight(theme.LineMeta).TextColor(pal.Placeholder)
			})
			for i, item := range grp.items {
				bulletText := item
				ui.Row(c.Key(fmt.Sprintf("%s-%s-%d", keyPrefix, grp.id, i))).FillWidth().Gap(theme.Space8).AlignItems(ui.Start).Children(func() {
					ui.Box(c).Width(theme.UpdaterBulletDotWidth).Height(theme.LineBody).Center().Shrink(0).Children(func() {
						ui.Text(c, "•").FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
					})
					ui.Text(c, bulletText).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).Grow(1)
				})
			}
		})
	}
}

func renderUpdaterPrimaryButton(c *ui.Context, m *Manager, snap updater.Snapshot) {
	switch snap.Status {
	case updater.StatusReady:
		ic := icons.Refresh
		btn := internalui.ActionButton(c, "updater-submit", theme.Tr("重启并更新", "Restart & Update"), &ic, internalui.ButtonPrimary, false)
		if btn.Clicked() && m.Updater != nil {
			m.Updater.InstallNow()
		}
	case updater.StatusInstalling:
		internalui.ActionButton(c, "updater-submit", theme.Tr("正在保存并重启…", "Saving & Restarting…"), nil, internalui.ButtonPrimary, true)
	case updater.StatusDownloading:
		internalui.ActionButton(c, "updater-submit", theme.Tr("正在下载…", "Downloading…"), nil, internalui.ButtonPrimary, true)
	case updater.StatusAvailable:
		if snap.PackageManaged {
			btn := internalui.ActionButton(c, "updater-submit", theme.Tr("前往下载页", "Open Releases"), nil, internalui.ButtonPrimary, false)
			if btn.Clicked() && snap.ReleaseURL != "" {
				c.OpenURL(snap.ReleaseURL)
			}
			return
		}
		if snap.DownloadPath != "" && !snap.InPlace {
			btn := internalui.ActionButton(c, "updater-submit", theme.Tr("查看安装包", "Show Installer"), nil, internalui.ButtonPrimary, false)
			if btn.Clicked() && mygo.App.IsReady() {
				mygo.Shell.ShowItemInFolder(snap.DownloadPath)
			}
			return
		}
		ic := icons.Download
		btn := internalui.ActionButton(c, "updater-submit", theme.Tr("立即更新", "Update Now"), &ic, internalui.ButtonPrimary, false)
		if btn.Clicked() && m.Updater != nil {
			m.Updater.StartDownload()
		}
	default:
		checking := snap.Status == updater.StatusChecking
		label := theme.Tr("检查更新", "Check Now")
		if checking {
			label = theme.Tr("正在检查…", "Checking…")
		}
		btn := internalui.ActionButton(c, "updater-submit", label, nil, internalui.ButtonPrimary, checking)
		if btn.Clicked() && m.Updater != nil {
			m.Updater.CheckAsync(true)
		}
	}
}
