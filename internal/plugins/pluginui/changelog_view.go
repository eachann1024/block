package pluginui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"block/internal/editor"
	"block/internal/icons"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"block/internal/updater"
	"github.com/egoist/mygo/ui"
)

const (
	storeDetailTabReadme    = "readme"
	storeDetailTabChangelog = "changelog"
)

type changelogResult struct {
	doc    plugins.PluginChangelog
	blocks *model.Doc
	err    error
}

func (ctrl *Controller) loadStoreChangelog(preview *storeReadmePreview, entry plugins.StoreAppEntry, installed plugins.InstalledApp) {
	if preview == nil || ctrl.sup == nil || ctrl.sup.Store() == nil {
		return
	}
	preview.changelogMu.Lock()
	if preview.changelogCancel != nil {
		preview.changelogCancel()
		preview.changelogCancel = nil
	}
	ctx, cancel := context.WithCancel(preview.ctx)
	ch := make(chan changelogResult, 1)
	preview.changelogCancel = cancel
	preview.changelogPending = ch
	preview.changelogLoaded = true
	preview.changelogErr = nil
	preview.changelogDark = theme.IsDark()
	// Seed immediately from inline index changelog so switching tabs feels instant.
	if len(preview.changelogDoc.Releases) == 0 && len(entry.Changelog) > 0 {
		releases := make([]plugins.PluginReleaseEntry, len(entry.Changelog))
		copy(releases, entry.Changelog)
		preview.changelogDoc = plugins.PluginChangelog{
			ID:            entry.ID,
			Repo:          entry.Repo,
			UpdatedAt:     entry.UpdatedAt,
			LatestVersion: releases[0].Version,
			ChangelogURL:  entry.ChangelogURL,
			Releases:      releases,
			Source:        "index",
		}
	}
	preview.changelogMu.Unlock()

	store := ctrl.sup.Store()
	locale := ctrl.sup.Locale()
	dark := theme.IsDark()
	go func() {
		reqCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		doc, err := store.ReadPluginChangelog(reqCtx, entry, installed, locale)
		reply := changelogResult{doc: doc, err: err}
		if err == nil && len(doc.Releases) == 0 && strings.TrimSpace(doc.Markdown) != "" && ctx.Err() == nil {
			reply.blocks = parseReadmeBlocks(doc.Markdown, dark)
		}
		ch <- reply
		if ctx.Err() == nil {
			ctrl.sup.NotifyRedraw()
		}
	}()
}

func (ctrl *Controller) pollStoreChangelog(preview *storeReadmePreview) {
	if preview == nil {
		return
	}
	preview.changelogMu.Lock()
	ch := preview.changelogPending
	preview.changelogMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case res := <-ch:
		preview.changelogMu.Lock()
		if preview.changelogPending == ch {
			preview.changelogPending = nil
			if res.err == nil {
				preview.changelogDoc = res.doc
				preview.changelogErr = nil
				if preview.changelogEditor != nil {
					preview.changelogEditor.ClosePreview()
					preview.changelogEditor = nil
				}
				if res.blocks != nil {
					preview.changelogEditor = editor.NewReadOnlyPreview(res.blocks, markdown.NewCodec(), nil)
				}
			} else if len(preview.changelogDoc.Releases) == 0 {
				preview.changelogErr = res.err
			}
		}
		preview.changelogMu.Unlock()
	default:
	}
}

func renderStoreDetailTabs(
	c *ui.Context,
	ctrl *Controller,
	preview *storeReadmePreview,
	entry plugins.StoreAppEntry,
	installed plugins.InstalledApp,
) {
	pal := theme.Ocean()
	activeTab := preview.activeTab
	if activeTab == "" {
		activeTab = storeDetailTabReadme
	}

	type tabSpec struct {
		id    string
		label string
	}
	tabs := []tabSpec{
		{id: storeDetailTabReadme, label: theme.Tr("说明", "Overview")},
		{id: storeDetailTabChangelog, label: theme.Tr("更新日志", "Changelog")},
	}

	ui.Column(c.Key("app-store-detail-tabs-wrap")).FillWidth().MinWidth(0).Gap(theme.Space12).Children(func() {
		ui.Row(c.Key("app-store-detail-tabs")).FillWidth().MinWidth(0).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
			for _, t := range tabs {
				tab := t
				active := activeTab == tab.id
				btnID := "app-store-detail-tab-" + tab.id
				btn := ui.ButtonBase(c.Key(btnID)).
					Height(theme.HeightControl).PaddingX(theme.Space12).Gap(theme.Space6).
					AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
					Label(tab.label).Shrink(0)
				hovered := btn.Hovered()
				pressed := btn.Pressed()
				if active {
					btn.Background(pal.Selected).Border(1, pal.SelectedBorder)
				} else if pressed {
					btn.Background(pal.Selected).Border(1, ui.Transparent)
				} else if hovered {
					btn.Background(pal.Hover).Border(1, ui.Transparent)
				} else {
					btn.Border(1, ui.Transparent)
				}
				bui.TactileCard(btn, hovered, pressed, active, false)
				btn.Children(func() {
					lblColor := pal.TextSecondary
					if active || hovered || pressed {
						lblColor = pal.Text
					}
					ui.Text(c, tab.label).
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(theme.WeightControl).TextColor(lblColor).SingleLine()
				})
				bui.Probe(btn, btnID)
				if btn.Clicked() {
					preview.activeTab = tab.id
					if tab.id == storeDetailTabChangelog {
						preview.changelogMu.Lock()
						loaded := preview.changelogLoaded
						preview.changelogMu.Unlock()
						if !loaded {
							ctrl.loadStoreChangelog(preview, entry, installed)
						}
					}
					c.Invalidate()
				}
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
	})
}

func renderStoreDetailChangelog(
	c *ui.Context,
	ctrl *Controller,
	preview *storeReadmePreview,
	entry plugins.StoreAppEntry,
	installed plugins.InstalledApp,
	isInstalled bool,
	locale string,
	width float32,
) {
	pal := theme.Ocean()
	uPal := pal.UpdaterColors()
	showEn := readmeLocaleKey(locale) == "en"

	preview.changelogMu.Lock()
	pending := preview.changelogPending != nil
	doc := preview.changelogDoc
	loadErr := preview.changelogErr
	mdEditor := preview.changelogEditor
	preview.changelogMu.Unlock()

	ui.Column(c.Key("app-store-detail-changelog")).FillWidth().MinWidth(0).Gap(theme.Space12).Children(func() {
		if len(doc.Releases) > 0 {
			installedVer := ""
			if isInstalled {
				installedVer = strings.TrimPrefix(strings.TrimSpace(installed.Manifest.Version), "v")
			}
			for i, rel := range doc.Releases {
				renderStoreChangelogCard(c, rel, i == 0, installedVer, showEn, pal, uPal)
			}
			return
		}
		if mdEditor != nil {
			ui.Column(c.Key("app-store-detail-changelog-md")).FillWidth().MinWidth(0).Children(func() {
				mdEditor.Render(c, width, true)
			})
			return
		}
		if pending {
			renderStoreChangelogSkeleton(c)
			return
		}

		msg := theme.Tr("更新日志加载失败", "Could not load changelog")
		failed := true
		if loadErr == nil || errors.Is(loadErr, plugins.ErrChangelogUnavailable) {
			msg = theme.Tr("这个应用还没有更新日志", "This app has no changelog yet")
			failed = false
		}
		ui.Row(c).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, msg).Role(ui.RoleStatus).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			if failed {
				refIc := icons.Refresh
				retry := bui.ActionButton(c, "app-store-changelog-retry", theme.Tr("重试", "Retry"), &refIc, bui.ButtonSecondary, false)
				if retry.Clicked() {
					ctrl.loadStoreChangelog(preview, entry, installed)
				}
			}
		})
	})
}

func renderStoreChangelogCard(
	c *ui.Context,
	rel plugins.PluginReleaseEntry,
	isLatest bool,
	installedVer string,
	showEn bool,
	pal theme.Palette,
	uPal theme.UpdaterPalette,
) {
	ver := strings.TrimPrefix(strings.TrimSpace(rel.Version), "v")
	isCurrent := installedVer != "" && ver == installedVer
	notes := rel.NotesForLanguage(showEn)

	ui.Column(c.Key("store-changelog-ver-"+ver)).FillWidth().MinWidth(0).
		Padding(theme.Space12, theme.Space16).Gap(theme.Space10).
		Radius(theme.RadiusLG).Background(pal.Card).Border(1, pal.Hairline).Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).MinWidth(0).Children(func() {
				ui.Text(c, "v"+ver).Font("monospace").FontSize(theme.FontSmall).
					FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightMono).TextColor(pal.Text).SingleLine()
				if isLatest {
					ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).AlignItems(ui.Center).
						Radius(theme.RadiusXS).Background(uPal.VersionPillBG).Shrink(0).Children(func() {
						ui.Text(c, theme.Tr("最新", "Latest")).FontSize(theme.FontMeta).
							FixedLineHeight(theme.LineMeta).FontWeight(theme.WeightControl).TextColor(uPal.VersionPillFG).SingleLine()
					})
				}
				if isCurrent {
					ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).AlignItems(ui.Center).
						Radius(theme.RadiusXS).Background(pal.Shell).Border(1, pal.Hairline).Shrink(0).Children(func() {
						ui.Text(c, theme.Tr("当前版本", "Installed")).FontSize(theme.FontMeta).
							FixedLineHeight(theme.LineMeta).TextColor(pal.TextSecondary).SingleLine()
					})
				}
			})

			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Shrink(0).Children(func() {
				if d := updater.ShortDate(rel.PublishedAt); d != "" {
					ui.Text(c, d).Font("monospace").FontSize(theme.FontMeta).
						FixedLineHeight(theme.LineMeta).TextColor(pal.Placeholder).SingleLine()
				}
				if linkURL := strings.TrimSpace(rel.HTMLURL); linkURL != "" {
					openBtn := bui.IconButton(c, icons.TbLink, bui.IconButtonOptions{
						ID:       "store-changelog-open-" + ver,
						Size:     theme.HeightMicro,
						IconSize: theme.IconMicro,
						Radius:   theme.RadiusXS,
						Tooltip:  theme.Tr("查看发布页", "Open Release"),
						Variant:  bui.IconPlain,
					})
					if openBtn.Clicked() {
						c.OpenURL(linkURL)
					}
				}
			})
		})

		if hl := strings.TrimSpace(notes.Highlights); hl != "" {
			ui.Text(c, hl).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				FontWeight(theme.WeightControl).TextColor(pal.Text)
		}

		type sectionGroup struct {
			id    string
			title string
			fg    ui.Color
			bg    ui.Color
			items []string
		}
		groups := []sectionGroup{
			{id: "new", title: theme.Tr("新增功能", "Features"), fg: uPal.CategoryNewFG, bg: uPal.CategoryNewBG, items: notes.New},
			{id: "imp", title: theme.Tr("体验优化", "Improvements"), fg: uPal.CategoryImpFG, bg: uPal.CategoryImpBG, items: notes.Improvements},
			{id: "fix", title: theme.Tr("问题修复", "Fixes"), fg: uPal.CategoryFixFG, bg: uPal.CategoryFixBG, items: notes.Fixes},
			{id: "other", title: theme.Tr("其他改动", "Other"), fg: pal.TextSecondary, bg: pal.Shell, items: notes.Other},
		}
		nonEmptyCount := 0
		onlyOther := false
		for _, g := range groups {
			if len(g.items) > 0 {
				nonEmptyCount++
				onlyOther = g.id == "other"
			}
		}

		if nonEmptyCount == 0 {
			if body := strings.TrimSpace(rel.Body); body != "" {
				ui.Text(c, body).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			} else if strings.TrimSpace(notes.Highlights) == "" {
				ui.Text(c, theme.Tr("常规稳定性与维护更新。", "Routine stability and maintenance update.")).
					FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			}
			return
		}

		if nonEmptyCount == 1 && onlyOther {
			renderStoreChangelogBullets(c, ver+"-other", notes.Other, pal)
			return
		}

		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space10).Children(func() {
			for _, g := range groups {
				if len(g.items) == 0 {
					continue
				}
				grp := g
				ui.Column(c.Key("store-cl-sec-"+ver+"-"+grp.id)).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
					ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).AlignItems(ui.Center).
						Radius(theme.RadiusSM).Background(grp.bg).Shrink(0).Children(func() {
						ui.Text(c, grp.title).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							FontWeight(theme.WeightControl).TextColor(grp.fg).SingleLine()
					})
					renderStoreChangelogBullets(c, ver+"-"+grp.id, grp.items, pal)
				})
			}
		})
	})
}

func renderStoreChangelogBullets(c *ui.Context, keyPrefix string, items []string, pal theme.Palette) {
	ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
		for i, line := range items {
			text := line
			ui.Row(c.Key(fmt.Sprintf("store-cl-%s-%d", keyPrefix, i))).FillWidth().MinWidth(0).
				Gap(theme.Space8).AlignItems(ui.Start).Children(func() {
				ui.Box(c).Width(theme.UpdaterBulletBoxWidth).Height(theme.LineBody).Center().Shrink(0).Children(func() {
					ui.Box(c).Width(theme.UpdaterBulletDotSize).Height(theme.UpdaterBulletDotSize).
						Radius(theme.RadiusPill).Background(pal.Accent)
				})
				ui.Text(c, text).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).Grow(1).MinWidth(0)
			})
		}
	})
}

func renderStoreChangelogSkeleton(c *ui.Context) {
	pal := theme.Ocean()
	phase := float64(time.Now().UnixNano()%int64(storeReadmeSkeletonPeriod)) / float64(storeReadmeSkeletonPeriod)
	alpha := float32(0.55 + 0.45*(0.5+0.5*math.Cos(2*math.Pi*phase)))
	c.Invalidate()

	bar := func(pct, h float32) {
		ui.Box(c).WidthPercent(pct).Height(h).Radius(theme.RadiusXS).Background(pal.Selected)
	}
	ui.Column(c.Key("app-store-changelog-skeleton")).FillWidth().MinWidth(0).Gap(theme.Space12).
		Opacity(alpha).Role(ui.RoleStatus).Label(theme.Tr("正在加载更新日志…", "Loading changelog…")).Children(func() {
		for i := range 3 {
			ui.Column(c.Key(fmt.Sprintf("app-store-changelog-skel-%d", i))).FillWidth().
				Padding(theme.Space12, theme.Space16).Gap(theme.Space10).
				Radius(theme.RadiusLG).Background(pal.Card).Border(1, pal.Hairline).Children(func() {
				bar(24, theme.StoreSkeletonLineH)
				bar(82, theme.StoreSkeletonLineH)
				bar(64, theme.StoreSkeletonLineH)
			})
		}
	})
}
