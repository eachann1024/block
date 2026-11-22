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
	default:
		return updaterTr(showEn, "检查更新", "App Updates")
	}
}

func updaterIdleInfoText(snap updater.Snapshot, showEn bool) string {
	switch snap.Status {
	case updater.StatusChecking:
		return updaterTr(showEn, "正在连接更新源检查新版本…", "Checking update sources…")
	case updater.StatusAvailable:
		if snap.PackageManaged {
			return updaterTr(showEn, "通过包管理器安装，可在包管理器或下载页升级", "Managed by system package manager; update there or via Releases")
		}
		if snap.DownloadPath != "" && !snap.InPlace {
			return updaterTr(showEn, "安装包已校验完成，打开即可安装", "Installer verified and ready to open")
		}
		if snap.TotalBytes > 0 {
			return fmt.Sprintf("%s %s · SHA-256", updaterTr(showEn, "安装包", "Package"), formatMegabytes(snap.TotalBytes))
		}
		return fmt.Sprintf("%s v%s", updaterTr(showEn, "当前版本", "Installed"), snap.CurrentVersion)
	default:
		return updaterTr(showEn, "已是最新版", "Up to date")
	}
}

func (m *Manager) renderUpdaterModal(c *ui.Context) {
	pal := theme.Ocean()
	uPal := pal.UpdaterColors()
	open := true
	showEn := theme.IsEn()

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

	title := updaterModalTitle(snap, showEn)

	internalui.Dialog(c, "updater-modal", title, &open, internalui.ModalLayout{
		Width:     theme.UpdaterModalWidth,
		MaxHeight: theme.UpdaterModalMaxHeight,
		Centered:  true,
	}, func() {
		ui.Column(c).FillWidth().Clip().Children(func() {
			ui.Row(c.Key("updater-header")).FillWidth().
				Padding(theme.Space16, theme.Space20, theme.Space12, theme.Space20).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space12).Grow(1).MinWidth(0).Children(func() {
					ui.Box(c.Key("updater-app-icon")).
						Width(theme.UpdaterHeaderIconBox).Height(theme.UpdaterHeaderIconBox).
						Radius(theme.RadiusLG).Background(uPal.Mark.Tile).Clip().
						Center().Shrink(0).Children(func() {
						internalui.CatMark(c, theme.UpdaterHeaderMarkSize, uPal.Mark)
					})

					ui.Column(c).Gap(theme.Space4).Grow(1).MinWidth(0).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
							ui.Text(c, title).FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
								FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()

							if snap.Notes.VersionCount > 1 {
								countText := fmt.Sprintf(updaterTr(showEn, "汇总 %d 个版本", "%d Releases"), snap.Notes.VersionCount)
								ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).AlignItems(ui.Center).
									Radius(theme.RadiusPill).Background(uPal.VersionPillBG).Border(1, uPal.VersionPillBorder).Children(func() {
									ui.Text(c, countText).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
										FontWeight(theme.WeightControl).TextColor(uPal.VersionPillFG).SingleLine()
								})
							}
						})

						verLabel := fmt.Sprintf("v%s", snap.CurrentVersion)
						if snap.Notes.FromVersion != "" && snap.Notes.ToVersion != "" &&
							updater.CompareVersion(snap.Notes.FromVersion, snap.Notes.ToVersion) < 0 {
							verLabel = fmt.Sprintf("v%s → v%s", snap.Notes.FromVersion, snap.Notes.ToVersion)
						} else if snap.LatestVersion != "" && updater.CompareVersion(snap.CurrentVersion, snap.LatestVersion) < 0 {
							verLabel = fmt.Sprintf("v%s → v%s", snap.CurrentVersion, snap.LatestVersion)
						}
						ui.Text(c, verLabel).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).SingleLine()
					})
				})

				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space12).Shrink(0).Children(func() {
					if pubDate := updater.ShortDate(snap.PublishedAt); pubDate != "" {
						ui.Text(c, pubDate).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).SingleLine()
					}
					cls := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
						ID:    "updater-close",
						Label: updaterTr(showEn, "关闭", "Close"),
					})
					if cls.Clicked() {
						m.CloseUpdater()
					}
				})
			})

			ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)

			// 2. Scheme A Shell Toolbar: Left View Mode Segmented + Right Source Selector & Latency Badge.
			ui.Row(c.Key("updater-toolbar")).FillWidth().
				Padding(theme.Space10, theme.Space20).Background(pal.Shell).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space10).Children(func() {
				viewIdx := 0
				if snap.ViewMode == updater.ViewByVersion {
					viewIdx = 1
				}
				if internalui.SegmentedControlWithOptions(c, "updater-view-seg", &viewIdx, []internalui.SegmentOption{
					{ID: "updater-view-merged", Label: updaterTr(showEn, "分类合并", "By Category")},
					{ID: "updater-view-versions", Label: updaterTr(showEn, "按版本明细", "By Version")},
				}, internalui.SegmentedControlOptions{CardTrack: true}) {
					if m.Updater != nil {
						if viewIdx == 1 {
							m.Updater.SetViewMode(updater.ViewByVersion)
						} else {
							m.Updater.SetViewMode(updater.ViewMerged)
						}
					}
				}

				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Shrink(0).Children(func() {
					srcIdx := 0
					switch snap.SourceMode {
					case updater.SourceMirror:
						srcIdx = 1
					case updater.SourceGitHub:
						srcIdx = 2
					}
					if internalui.SegmentedControlWithOptions(c, "updater-source-seg", &srcIdx, []internalui.SegmentOption{
						{ID: "updater-src-auto", Label: updaterTr(showEn, "自动", "Auto")},
						{ID: "updater-src-mirror", Label: updaterTr(showEn, "国内镜像", "CN Mirror")},
						{ID: "updater-src-github", Label: "GitHub"},
					}, internalui.SegmentedControlOptions{CardTrack: true}) {
						modes := []updater.SourceMode{updater.SourceAuto, updater.SourceMirror, updater.SourceGitHub}
						picked := modes[srcIdx]
						if m.Updater != nil {
							m.Updater.SetSourceMode(picked)
							if snap.Status == updater.StatusError || snap.Status == updater.StatusIdle || snap.Status == updater.StatusUpToDate {
								m.Updater.CheckAsync(true)
							}
						} else {
							m.mutateState(func(s *model.AppState) {
								s.UpdateSource = string(picked)
							})
						}
					}

					if snap.FetchLatencyMs > 0 {
						ui.Text(c, fmt.Sprintf("%d ms", snap.FetchLatencyMs)).Font("monospace").FontSize(theme.FontMeta).
							FixedLineHeight(theme.LineMeta).TextColor(pal.TextSecondary).SingleLine()
					}
				})
			})

			ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)

			// 3. Scrollable Bilingual Release Notes Body (Scheme A Section Cards).
			notesScroll := internalui.Scroll(c.Key("updater-notes-scroll"), &m.updaterScroll).
				Column().FillWidth().MaxHeight(theme.UpdaterNotesMaxHeight).
				Padding(theme.Space16, theme.Space20).Gap(theme.Space12)
			internalui.Probe(notesScroll, "updater-notes-scroll")
			notesScroll.Children(func() {
				if snap.ViewMode == updater.ViewByVersion && len(snap.Notes.Versions) > 0 {
					for idx, verEntry := range snap.Notes.Versions {
						entry := verEntry
						isLatest := idx == 0
						renderVersionTimelineCard(c, entry, isLatest, showEn, pal, uPal)
					}
					return
				}

				tagged := snap.Notes.TaggedForLanguage(showEn)
				if tagged.IsEmpty() {
					emptyMsg := updaterTr(showEn, "暂无可展示的更新日志", "No release notes available yet")
					if snap.Status == updater.StatusChecking {
						emptyMsg = updaterTr(showEn, "正在获取更新日志…", "Loading release notes…")
					}
					ui.Row(c).FillWidth().PaddingY(theme.Space24).Justify(ui.Center).Children(func() {
						ui.Text(c, emptyMsg).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
					})
					return
				}
				renderTaggedMergedSections(c, tagged, snap.Notes.VersionCount > 1, showEn, pal, uPal)
			})

			ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)

			// 4. Scheme A State-Driven Footer (Available / Downloading / Ready / Installing / Error).
			ui.Column(c.Key("updater-footer")).FillWidth().
				Padding(theme.Space12, theme.Space20, theme.Space16, theme.Space20).Children(func() {
				m.renderUpdaterFooter(c, snap, showEn, pal, uPal)
			})
		})
	})

	if !open {
		m.CloseUpdater()
	}
}

func renderTaggedMergedSections(
	c *ui.Context,
	notes updater.TaggedLocalizedNotes,
	showVersionTag bool,
	showEn bool,
	pal theme.Palette,
	uPal theme.UpdaterPalette,
) {
	if hl := strings.TrimSpace(notes.Highlights); hl != "" {
		ui.Row(c.Key("merged-hl")).FillWidth().Padding(theme.Space12, theme.Space16).
			Radius(theme.RadiusLG).Background(pal.Card).Border(1, pal.Hairline).Children(func() {
			ui.Text(c, hl).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1).MinWidth(0)
		})
	}

	type taggedGroup struct {
		id    string
		title string
		fg    ui.Color
		bg    ui.Color
		items []updater.TaggedBullet
	}
	groups := []taggedGroup{
		{
			id:    "new",
			title: updaterTr(showEn, "新增功能", "Features"),
			fg:    uPal.CategoryNewFG,
			bg:    uPal.CategoryNewBG,
			items: notes.New,
		},
		{
			id:    "imp",
			title: updaterTr(showEn, "体验优化", "Improvements"),
			fg:    uPal.CategoryImpFG,
			bg:    uPal.CategoryImpBG,
			items: notes.Improvements,
		},
		{
			id:    "fix",
			title: updaterTr(showEn, "问题修复", "Fixes"),
			fg:    uPal.CategoryFixFG,
			bg:    uPal.CategoryFixBG,
			items: notes.Fixes,
		},
		{
			id:    "other",
			title: updaterTr(showEn, "其他改动", "Other"),
			fg:    pal.TextSecondary,
			bg:    pal.Shell,
			items: notes.Other,
		},
	}

	for _, g := range groups {
		if len(g.items) == 0 {
			continue
		}
		grp := g
		ui.Column(c.Key("merged-card-"+grp.id)).FillWidth().
			Padding(theme.Space12, theme.Space16).Gap(theme.Space10).
			Radius(theme.RadiusLG).Background(pal.Card).Border(1, pal.Hairline).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).Children(func() {
				ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).AlignItems(ui.Center).
					Radius(theme.RadiusSM).Background(grp.bg).Children(func() {
					ui.Text(c, grp.title).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						FontWeight(theme.WeightControl).TextColor(grp.fg).SingleLine()
				})
				countLabel := fmt.Sprintf(updaterTr(showEn, "%d 项", "%d items"), len(grp.items))
				ui.Text(c, countLabel).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					TextColor(pal.Placeholder).SingleLine()
			})

			ui.Column(c).FillWidth().Gap(theme.Space8).Children(func() {
				for i, bullet := range grp.items {
					b := bullet
					ui.Row(c.Key(fmt.Sprintf("merged-%s-%d", grp.id, i))).FillWidth().Gap(theme.Space8).AlignItems(ui.Start).Children(func() {
						renderAccentBulletDot(c, pal)
						ui.Text(c, b.Text).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).Grow(1).MinWidth(0)
						if showVersionTag && b.Version != "" {
							verTag := b.Version
							if !strings.HasPrefix(verTag, "v") {
								verTag = "v" + verTag
							}
							ui.Text(c, verTag).Font("monospace").FontSize(theme.FontMeta).
								FixedLineHeight(theme.LineBody).TextColor(pal.Placeholder).Shrink(0).SingleLine()
						}
					})
				}
			})
		})
	}
}

func renderVersionTimelineCard(
	c *ui.Context,
	entry updater.ReleaseEntry,
	isLatest bool,
	showEn bool,
	pal theme.Palette,
	uPal theme.UpdaterPalette,
) {
	sec := updater.VersionForLanguage(entry, showEn)
	var flat []string
	if hl := strings.TrimSpace(sec.Highlights); hl != "" {
		flat = append(flat, hl)
	}
	flat = append(flat, sec.New...)
	flat = append(flat, sec.Improvements...)
	flat = append(flat, sec.Fixes...)
	flat = append(flat, sec.Other...)

	ui.Column(c.Key("updater-ver-"+entry.Version)).FillWidth().
		Padding(theme.Space12, theme.Space16).Gap(theme.Space8).
		Radius(theme.RadiusLG).Background(pal.Card).Border(1, pal.Hairline).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
				ui.Text(c, "v"+entry.Version).Font("monospace").FontSize(theme.FontSmall).
					FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightMono).TextColor(pal.Text).SingleLine()
				if isLatest {
					ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).AlignItems(ui.Center).
						Radius(theme.RadiusXS).Background(uPal.VersionPillBG).Children(func() {
						ui.Text(c, updaterTr(showEn, "最新", "Latest")).FontSize(theme.FontMeta).
							FixedLineHeight(theme.LineMeta).FontWeight(theme.WeightControl).TextColor(uPal.VersionPillFG).SingleLine()
					})
				}
			})
			if d := updater.ShortDate(entry.PublishedAt); d != "" {
				ui.Text(c, d).Font("monospace").FontSize(theme.FontMeta).
					FixedLineHeight(theme.LineMeta).TextColor(pal.Placeholder).SingleLine()
			}
		})

		if len(flat) == 0 {
			ui.Text(c, updaterTr(showEn, "常规稳定性与性能维护。", "Routine stability and maintenance improvements.")).
				FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			return
		}
		ui.Column(c).FillWidth().Gap(theme.Space6).Children(func() {
			for i, line := range flat {
				text := line
				ui.Row(c.Key(fmt.Sprintf("ver-%s-%d", entry.Version, i))).FillWidth().Gap(theme.Space8).AlignItems(ui.Start).Children(func() {
					renderAccentBulletDot(c, pal)
					ui.Text(c, text).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).Grow(1).MinWidth(0)
				})
			}
		})
	})
}

func renderAccentBulletDot(c *ui.Context, pal theme.Palette) {
	ui.Box(c).Width(theme.UpdaterBulletBoxWidth).Height(theme.LineBody).Center().Shrink(0).Children(func() {
		ui.Box(c).Width(theme.UpdaterBulletDotSize).Height(theme.UpdaterBulletDotSize).
			Radius(theme.RadiusPill).Background(pal.Accent)
	})
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
		ui.Column(c.Key("updater-footer-downloading")).FillWidth().Gap(theme.Space10).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Grow(1).MinWidth(0).Children(func() {
					var titleText string
					if snap.Preparing {
						titleText = updaterTr(showEn, "校验通过，正在解包准备更新…", "Verified, preparing update bundle…")
					} else {
						titleText = fmt.Sprintf("%s v%s · %d%%",
							updaterTr(showEn, "正在下载", "Downloading"),
							snap.LatestVersion,
							snap.Percent,
						)
					}
					ui.Text(c, titleText).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
						FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()

					var detailParts []string
					if snap.TotalBytes > 0 && !snap.Preparing {
						detailParts = append(detailParts, fmt.Sprintf("%s / %s", formatMegabytes(snap.DownloadedBytes), formatMegabytes(snap.TotalBytes)))
					}
					if snap.SpeedMBps > 0.05 && !snap.Preparing {
						detailParts = append(detailParts, fmt.Sprintf("%.1f MB/s", snap.SpeedMBps))
					}
					if len(detailParts) > 0 {
						ui.Text(c, strings.Join(detailParts, " · ")).Font("monospace").FontSize(theme.FontMeta).
							FixedLineHeight(theme.LineMeta).TextColor(pal.TextSecondary).SingleLine()
					}
				})

				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Shrink(0).Children(func() {
					abortBtn := internalui.BtnCancel(c, "updater-abort-download", internalui.PresetOptions{
						Label:    updaterTr(showEn, "取消", "Cancel"),
						Height:   theme.HeightCompact,
						PaddingX: theme.Space10,
					})
					if abortBtn.Clicked() && m.Updater != nil {
						m.Updater.CancelDownload()
					}

					bgIc := icons.ChevronDown
					bgBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-bg-download",
						Label:    updaterTr(showEn, "后台下载", "Hide"),
						Leading:  &bgIc,
						Style:    internalui.ButtonGhost,
						Height:   theme.HeightCompact,
						PaddingX: theme.Space10,
					})
					if bgBtn.Clicked() {
						m.CloseUpdater()
					}
				})
			})

			track := ui.Box(c.Key("updater-progress-track")).FillWidth().Height(theme.UpdaterProgressHeight).
				Radius(theme.RadiusPill).Background(uPal.ProgressTrack).Clip()
			track.Children(func() {
				pct := float32(snap.Percent)
				if pct < 2 {
					pct = 2
				}
				ui.Box(c.Key("updater-progress-fill")).WidthPercent(pct).Height(theme.UpdaterProgressHeight).
					Radius(theme.RadiusPill).Background(uPal.ProgressFill)
			})
		})

	case updater.StatusReady:
		ui.Row(c.Key("updater-footer-ready")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Grow(1).MinWidth(0).Children(func() {
				icons.Draw(c, icons.Check, theme.IconAction, pal.DuoIconColors(pal.Success))
				ui.Text(c, updaterTr(showEn, "重启时自动保存笔记", "Notes are saved on restart")).
					FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine()
			})

			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Shrink(0).Children(func() {
				laterBtn := internalui.BtnLater(c, "updater-cancel", internalui.PresetOptions{
					Label: updaterTr(showEn, "稍后提醒", "Later"),
				})
				if laterBtn.Clicked() {
					m.CloseUpdater()
				}
				ic := icons.Refresh
				submitBtn := internalui.ActionButton(c, "updater-submit", updaterTr(showEn, "重启并更新", "Restart & Update"), &ic, internalui.ButtonPrimary, false)
				if submitBtn.Clicked() && m.Updater != nil {
					m.Updater.InstallNow()
				}
			})
		})

	case updater.StatusInstalling:
		ui.Row(c.Key("updater-footer-installing")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Grow(1).MinWidth(0).Children(func() {
				icons.DrawAnimated(c, icons.UpdateTangle, theme.IconAction, pal.DuoIconColors(pal.Accent), true)
				ui.Text(c, updaterTr(showEn, "正在保存笔记并重启 Block…", "Saving notes and restarting Block…")).
					FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1).MinWidth(0)
			})
			refIc := icons.Refresh
			internalui.ActionButton(c, "updater-submit", updaterTr(showEn, "正在重启…", "Restarting…"), &refIc, internalui.ButtonPrimary, true)
		})

	case updater.StatusError:
		ui.Row(c.Key("updater-footer-error")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
			errMsg := snap.ErrorMessage
			if errMsg == "" {
				errMsg = updaterTr(showEn, "更新请求失败，请检查网络或切换更新源", "Update failed; check network or switch update source")
			}
			ui.Text(c, errMsg).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				TextColor(pal.Danger).Grow(1).MinWidth(0)

			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Shrink(0).Children(func() {
				if snap.LogPath != "" && mygo.App.IsReady() {
					revIc := icons.FolderReveal
					logBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-open-log",
						Label:    updaterTr(showEn, "查看日志", "View Log"),
						Leading:  &revIc,
						Style:    internalui.ButtonGhost,
						Height:   theme.HeightCompact,
						PaddingX: theme.Space8,
					})
					if logBtn.Clicked() {
						mygo.Shell.ShowItemInFolder(snap.LogPath)
					}
				}
				if snap.ReleaseURL != "" {
					linkIc := icons.LinkOpen
					webBtn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
						ID:       "updater-open-web",
						Label:    updaterTr(showEn, "下载页", "Releases"),
						Leading:  &linkIc,
						Style:    internalui.ButtonGhost,
						Height:   theme.HeightCompact,
						PaddingX: theme.Space8,
					})
					if webBtn.Clicked() {
						c.OpenURL(snap.ReleaseURL)
					}
				}
				closeBtn := internalui.BtnClose(c, "updater-cancel", internalui.PresetOptions{
					Label: updaterTr(showEn, "关闭", "Close"),
				})
				if closeBtn.Clicked() {
					m.CloseUpdater()
				}
				retryBtn := internalui.BtnRetry(c, "updater-submit", internalui.PresetOptions{
					Label: updaterTr(showEn, "重试", "Retry"),
					Style: internalui.StyleRef(internalui.ButtonPrimary),
				})
				if retryBtn.Clicked() && m.Updater != nil {
					m.Updater.CheckAsync(true)
				}
			})
		})

	default:
		ui.Row(c.Key("updater-footer-idle")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
			ui.Text(c, updaterIdleInfoText(snap, showEn)).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				TextColor(pal.TextSecondary).Grow(1).MinWidth(0)

			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Shrink(0).Children(func() {
				var cancelBtn ui.Element
				if snap.HasActionableUpdate() {
					cancelBtn = internalui.BtnLater(c, "updater-cancel", internalui.PresetOptions{
						Label: updaterTr(showEn, "稍后提醒", "Later"),
					})
				} else {
					cancelBtn = internalui.BtnClose(c, "updater-cancel", internalui.PresetOptions{
						Label: updaterTr(showEn, "关闭", "Close"),
					})
				}
				if cancelBtn.Clicked() {
					m.CloseUpdater()
				}
				renderUpdaterIdlePrimaryButton(c, m, snap, showEn)
			})
		})
	}
}

func renderUpdaterIdlePrimaryButton(c *ui.Context, m *Manager, snap updater.Snapshot, showEn bool) {
	if snap.Status == updater.StatusAvailable {
		if snap.PackageManaged {
			linkIc := icons.LinkOpen
			btn := internalui.ActionButton(c, "updater-submit", updaterTr(showEn, "前往下载页", "Open Releases"), &linkIc, internalui.ButtonPrimary, false)
			if btn.Clicked() && snap.ReleaseURL != "" {
				c.OpenURL(snap.ReleaseURL)
			}
			return
		}
		if snap.DownloadPath != "" && !snap.InPlace {
			revIc := icons.FolderReveal
			btn := internalui.ActionButton(c, "updater-submit", updaterTr(showEn, "查看安装包", "Show Installer"), &revIc, internalui.ButtonPrimary, false)
			if btn.Clicked() && mygo.App.IsReady() {
				mygo.Shell.ShowItemInFolder(snap.DownloadPath)
			}
			return
		}
		ic := icons.Download
		btn := internalui.ActionButton(c, "updater-submit", updaterTr(showEn, "立即更新", "Update Now"), &ic, internalui.ButtonPrimary, false)
		if btn.Clicked() && m.Updater != nil {
			m.Updater.StartDownload()
		}
		return
	}

	checking := snap.Status == updater.StatusChecking
	label := updaterTr(showEn, "检查更新", "Check Now")
	if checking {
		label = updaterTr(showEn, "正在检查…", "Checking…")
	}
	refIc := icons.Refresh
	btn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{
		ID:       "updater-submit",
		Label:    label,
		Leading:  &refIc,
		Style:    internalui.ButtonPrimary,
		Disabled: checking,
		Loading:  checking,
	})
	if btn.Clicked() && m.Updater != nil {
		m.Updater.CheckAsync(true)
	}
}
