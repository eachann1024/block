package pluginui

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"block/internal/editor"
	"block/internal/icons"
	"block/internal/markdown"
	"block/internal/modals"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	"golang.org/x/mod/semver"
)

type readmeResult struct {
	doc    plugins.PluginReadme
	blocks *model.Doc
	err    error
}

const storeReadmeIdleTimeout = 10 * time.Second

// storeReadmePreview is the App Store detail page shown in place of the list.
type storeReadmePreview struct {
	entry          plugins.StoreAppEntry
	installed      plugins.InstalledApp
	locale         string
	cancel         context.CancelFunc
	ctx            context.Context
	pending        <-chan readmeResult
	result         readmeResult
	editorMu       sync.Mutex
	editor         *editor.Session
	media          *readmeMedia
	lastRenderedAt time.Time
	idleTimer      *time.Timer
}

func readmeLocaleKey(locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en") {
		return "en"
	}
	return "zh"
}

// openStoreReadme switches the App Store tab to the in-place detail page for entry.
func (ctrl *Controller) openStoreReadme(entry plugins.StoreAppEntry, installed plugins.InstalledApp) {
	loc := readmeLocaleKey(ctrl.sup.Locale())
	ctrl.mu.Lock()
	same := ctrl.storeReadme != nil && ctrl.storeReadme.entry.ID == entry.ID && ctrl.storeReadme.locale == loc
	ctrl.mu.Unlock()
	if same {
		return
	}
	ctrl.loadStoreReadme(entry, installed)
}

// renderStoreDetailBack sits in the settings header slot the list's filter
// and tools occupy, so the detail page needs no extra row for navigation.
func renderStoreDetailBack(c *ui.Context, ctrl *Controller) {
	backIcon := icons.ArrowLeft
	backBtn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
		ID:       "app-store-detail-back",
		Label:    theme.Tr("返回", "Back"),
		Leading:  &backIcon,
		Style:    bui.ButtonGhost,
		Height:   theme.HeightCompact,
		PaddingX: theme.Space8,
	})
	if backBtn.Clicked() {
		ctrl.closeStoreReadme()
		c.Invalidate()
	}
}

func (ctrl *Controller) storeDetailOpen() bool {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	return ctrl.storeReadme != nil
}

func (ctrl *Controller) touchStoreReadmeRendered(preview *storeReadmePreview) {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	if ctrl.storeReadme != preview {
		return
	}
	preview.lastRenderedAt = time.Now()
	if preview.idleTimer != nil {
		preview.idleTimer.Reset(storeReadmeIdleTimeout)
	}
}

func (ctrl *Controller) closeStoreReadmeIfNotRenderedRecently(maxAge time.Duration) {
	ctrl.mu.Lock()
	preview := ctrl.storeReadme
	if preview == nil || time.Since(preview.lastRenderedAt) < maxAge {
		ctrl.mu.Unlock()
		return
	}
	ctrl.detachStoreReadmeLocked(preview)
	ctrl.mu.Unlock()
	releaseStoreReadmePreview(preview)
}

func (ctrl *Controller) loadStoreReadme(entry plugins.StoreAppEntry, installed plugins.InstalledApp) {
	ctrl.closeStoreReadme()
	locale := ctrl.sup.Locale()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan readmeResult, 1)
	preview := &storeReadmePreview{
		entry:          entry,
		installed:      installed,
		locale:         readmeLocaleKey(locale),
		ctx:            ctx,
		cancel:         cancel,
		pending:        result,
		lastRenderedAt: time.Now(),
	}
	preview.idleTimer = time.AfterFunc(storeReadmeIdleTimeout, func() {
		ctrl.closeStoreReadmeIfIdle(preview)
	})
	ctrl.mu.Lock()
	ctrl.storeReadme = preview
	ctrl.mu.Unlock()

	store := ctrl.sup.Store()
	go func() {
		requestCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		doc, err := store.ReadPluginReadme(requestCtx, entry, installed, locale)
		reply := readmeResult{doc: doc, err: err}
		if err == nil && ctx.Err() == nil {
			reply.blocks = parseReadmeBlocks(doc.Text)
		}
		result <- reply
		if ctx.Err() == nil {
			ctrl.sup.NotifyRedraw()
		}
	}()
}

func (ctrl *Controller) closeStoreReadmeIfIdle(preview *storeReadmePreview) {
	ctrl.mu.Lock()
	if ctrl.storeReadme != preview {
		ctrl.mu.Unlock()
		return
	}
	elapsed := time.Since(preview.lastRenderedAt)
	if elapsed < storeReadmeIdleTimeout {
		if preview.idleTimer != nil {
			preview.idleTimer.Reset(storeReadmeIdleTimeout - elapsed)
		}
		ctrl.mu.Unlock()
		return
	}
	ctrl.detachStoreReadmeLocked(preview)
	ctrl.mu.Unlock()
	releaseStoreReadmePreview(preview)
}

func (ctrl *Controller) detachStoreReadmeLocked(preview *storeReadmePreview) {
	if preview.idleTimer != nil {
		preview.idleTimer.Stop()
		preview.idleTimer = nil
	}
	if ctrl.storeReadme == preview {
		ctrl.storeReadme = nil
	}
}

func releaseStoreReadmePreview(preview *storeReadmePreview) {
	if preview == nil {
		return
	}
	preview.cancel()
	preview.editorMu.Lock()
	if preview.editor != nil {
		preview.editor.ClosePreview()
		preview.editor = nil
	}
	preview.editorMu.Unlock()
	if preview.media != nil {
		preview.media.close()
	}
}

func (ctrl *Controller) closeStoreReadme() {
	ctrl.mu.Lock()
	preview := ctrl.storeReadme
	if preview == nil {
		ctrl.mu.Unlock()
		return
	}
	ctrl.detachStoreReadmeLocked(preview)
	ctrl.mu.Unlock()
	releaseStoreReadmePreview(preview)
}

func (ctrl *Controller) pollStoreReadme(c *ui.Context, preview *storeReadmePreview) {
	if preview == nil || preview.pending == nil {
		return
	}
	select {
	case preview.result = <-preview.pending:
		preview.pending = nil
		if preview.result.blocks != nil {
			preview.media = &readmeMedia{
				ctx: preview.ctx, doc: preview.result.doc, services: c.Services(),
				slots: make(chan struct{}, 2), assets: make(map[readmeMediaKey]editor.PreviewAsset),
			}
			preview.editorMu.Lock()
			preview.editor = editor.NewReadOnlyPreview(preview.result.blocks, markdown.NewCodec(), preview.media.asset)
			preview.editorMu.Unlock()
		}
	default:
	}
}

// renderStoreDetail draws the App Store detail page: a header carrying the
// download count beside the actions, one compact meta line (version, author,
// repository) with permission tags, then the README at full width.
func renderStoreDetail(c *ui.Context, ctrl *Controller, onSelectTab func(nextTab string)) {
	ctrl.mu.Lock()
	preview := ctrl.storeReadme
	ctrl.mu.Unlock()
	if preview == nil {
		return
	}
	ctrl.touchStoreReadmeRendered(preview)
	ctrl.pollStoreReadme(c, preview)

	pal := theme.Ocean()
	locale := ctrl.sup.Locale()
	entry := preview.entry
	inCatalog := false
	catalog := ctrl.sup.Catalog()
	if catalog != nil {
		for _, app := range catalog.Apps {
			if app.ID == entry.ID {
				entry = app
				inCatalog = true
				break
			}
		}
	}
	installedApp, isInstalled := ctrl.sup.GetInstalled(entry.ID)
	if catalog != nil && !inCatalog {
		if !isInstalled {
			ctrl.closeStoreReadme()
			c.Invalidate()
			return
		}
		// Plugin was delisted while installed: strip stale catalog update metadata.
		entry.DownloadURL = ""
		entry.SHA256 = ""
		entry.Verified = false
		entry.Version = installedApp.Manifest.Version
	}
	installedChanged := preview.installed.InstallDir != installedApp.InstallDir ||
		preview.installed.Manifest.Version != installedApp.Manifest.Version
	if preview.locale != readmeLocaleKey(locale) || installedChanged {
		ctrl.loadStoreReadme(entry, installedApp)
		ctrl.mu.Lock()
		preview = ctrl.storeReadme
		ctrl.mu.Unlock()
		if preview == nil {
			return
		}
	}
	_, installingID, storeErr, _ := ctrl.sup.StoreStatus()

	page := ui.Column(c.Key("app-store-detail-" + entry.ID)).FillWidth().MinWidth(0).Gap(theme.Space24)
	pageW := page.Bounds().W
	if pageW <= 0 {
		if vpW, _ := c.Size(); vpW > 0 {
			pageW = vpW - theme.SidebarMinWidth - 2*theme.Space24
			c.Invalidate()
		}
	}

	page.Children(func() {
		if c.Shortcut(0, ui.KeyEscape) {
			ctrl.closeStoreReadme()
			c.Invalidate()
			return
		}

		ui.Row(c.Key("app-store-detail-head")).FillWidth().MinWidth(0).Gap(theme.Space16).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(theme.StoreDetailIconBox, theme.StoreDetailIconBox).Shrink(0).Center().
				Radius(theme.RadiusXL).Background(pal.Shell).Children(func() {
				DrawDuoSpec(c, entry.Icon, theme.StoreDetailIconGlyph, pal.DuoIconColors(pal.Text))
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space4).Children(func() {
				ui.Row(c).MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, entry.Name.Resolve(locale)).
						FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).FontWeight(theme.WeightTitle).
						TextColor(pal.Text).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
					switch storeAppSource(ctrl, entry, installedApp, isInstalled) {
					case storeSourceOfficial:
						bui.OfficialBadge(c, true)
					case storeSourceUnreviewed:
						renderStoreUnreviewed(c)
					}
					if storeAppIsBeta(entry, installedApp, isInstalled) {
						bui.BetaBadge(c, true)
					}
				})
				if summary := strings.TrimSpace(entry.Summary.Resolve(locale)); summary != "" {
					ui.Text(c, summary).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
				}
			})
			ui.Row(c).Gap(theme.Space16).AlignItems(ui.Center).Shrink(0).Children(func() {
				renderStoreDetailDownloads(c, ctrl, entry)
				ui.Row(c).Gap(theme.Space4).AlignItems(ui.Center).Shrink(0).Children(func() {
					renderStoreDetailActions(c, ctrl, entry, installedApp, isInstalled, installingID, onSelectTab)
				})
			})
		})

		renderStoreDetailMeta(c, entry, locale)

		ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
		if storeErr != "" {
			renderStatusTextWithTone(c, storeErr, "danger")
		}
		renderStoreDetailReadme(c, ctrl, preview, entry, installedApp, pageW)
	})
}

func renderStoreDetailActions(
	c *ui.Context,
	ctrl *Controller,
	entry plugins.StoreAppEntry,
	installedApp plugins.InstalledApp,
	isInstalled bool,
	installingID string,
	onSelectTab func(nextTab string),
) {
	busy := installingID == entry.ID
	if !isInstalled {
		lbl := theme.Tr("安装", "Install")
		if busy {
			lbl = theme.Tr("安装中…", "Installing…")
		}
		dlIc := icons.Download
		btn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
			ID:       "app-store-detail-install",
			Label:    lbl,
			Leading:  &dlIc,
			Style:    bui.ButtonPrimary,
			Disabled: installingID != "" && !busy,
			Loading:  busy,
		})
		if btn.Clicked() && !busy {
			ctrl.sup.InstallEntryAsync(entry, nil)
		}
		return
	}

	trashIc := icons.Trash
	uninst := bui.ActionButton(c, "app-store-detail-uninstall", theme.Tr("卸载", "Uninstall"), &trashIc, bui.ButtonGhost, installingID != "")
	if uninst.Clicked() {
		ctrl.uninstallPlugin(entry.ID, nil)
	}
	if entry.ID == plugins.DiagnosticsPluginID {
		ui.Text(c, theme.Tr("已内置于 Block", "Built into Block")).FontSize(theme.FontBody).TextColor(theme.Ocean().TextSecondary)
		return
	}

	hasUpdate := entry.DownloadURL != "" && strings.TrimSpace(entry.Version) != "" &&
		semver.Compare("v"+strings.TrimPrefix(entry.Version, "v"), "v"+installedApp.Manifest.Version) > 0
	if hasUpdate {
		lbl := theme.Tr("更新", "Update")
		if busy {
			lbl = theme.Tr("更新中…", "Updating…")
		}
		upIc := icons.Refresh
		upBtn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
			ID:       "app-store-detail-update",
			Label:    lbl,
			Leading:  &upIc,
			Style:    bui.ButtonAccentOutline,
			Disabled: installingID != "" && !busy,
			Loading:  busy,
		})
		if upBtn.Clicked() && !busy {
			ctrl.sup.InstallEntryAsync(entry, ctrl.clearPluginState)
		}
	}

	openLabel := theme.Tr("打开", "Open")
	if entry.ID == "block-sync" {
		openLabel = theme.Tr("配置同步", "Configure Sync")
	} else if installedApp.Manifest.IsDeclarativeOnly() {
		openLabel = theme.Tr("语法与预览", "Syntax & Preview")
	}
	openStyle := bui.ButtonPrimary
	if hasUpdate {
		openStyle = bui.ButtonSecondary
	}
	openIc := icons.Sliders
	openBtn := bui.ActionButton(c, "app-store-detail-open", openLabel, &openIc, openStyle, false)
	if openBtn.Clicked() && onSelectTab != nil {
		ctrl.closeStoreReadme()
		onSelectTab(modals.TabPluginPrefix + entry.ID)
	}
}

func renderStoreDetailReadme(
	c *ui.Context,
	ctrl *Controller,
	preview *storeReadmePreview,
	entry plugins.StoreAppEntry,
	installed plugins.InstalledApp,
	width float32,
) {
	pal := theme.Ocean()
	_, winH := c.Size()
	readmeH := max(winH*0.62, theme.StoreDetailReadmeMinH)

	ui.Column(c.Key("app-store-detail-readme")).FillWidth().MinWidth(0).Children(func() {
		preview.editorMu.Lock()
		ed := preview.editor
		if ed != nil {
			ui.Box(c.Key("app-store-detail-readme-view")).FillWidth().Height(readmeH).Radius(theme.RadiusLG).Clip().Children(func() {
				ed.Render(c, width, true)
			})
			preview.editorMu.Unlock()
			return
		}
		preview.editorMu.Unlock()
		if preview.pending != nil {
			renderStoreReadmeSkeleton(c)
			return
		}
		message := theme.Tr("说明加载失败", "Could not load README")
		failed := true
		if errors.Is(preview.result.err, plugins.ErrReadmeUnavailable) {
			message = theme.Tr("这个应用还没有说明", "This app has no README yet")
			failed = false
		}
		ui.Row(c).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, message).Role(ui.RoleStatus).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			if failed {
				refIc := icons.Refresh
				retry := bui.ActionButton(c, "app-store-readme-retry", theme.Tr("重试", "Retry"), &refIc, bui.ButtonSecondary, false)
				if retry.Clicked() {
					ctrl.loadStoreReadme(entry, installed)
				}
			}
		})
	})
}

// storeReadmeSkeletonPeriod is one full breathe cycle of the README skeleton.
const storeReadmeSkeletonPeriod = 1600 * time.Millisecond

// renderStoreReadmeSkeleton mirrors a typical README (title, paragraph, media,
// list) with soft bars that breathe in unison while the README downloads.
func renderStoreReadmeSkeleton(c *ui.Context) {
	pal := theme.Ocean()
	phase := float64(time.Now().UnixNano()%int64(storeReadmeSkeletonPeriod)) / float64(storeReadmeSkeletonPeriod)
	alpha := float32(0.55 + 0.45*(0.5+0.5*math.Cos(2*math.Pi*phase)))
	c.Invalidate()

	bar := func(pct, h float32) {
		ui.Box(c).WidthPercent(pct).Height(h).Radius(theme.RadiusXS).Background(pal.Selected)
	}
	ui.Column(c.Key("app-store-readme-skeleton")).FillWidth().MinWidth(0).Gap(theme.Space24).
		Opacity(alpha).Role(ui.RoleStatus).Label(theme.Tr("正在加载说明…", "Loading README…")).Children(func() {
		bar(36, theme.StoreSkeletonTitleH)
		ui.Column(c).FillWidth().Gap(theme.Space12).Children(func() {
			bar(94, theme.StoreSkeletonLineH)
			bar(88, theme.StoreSkeletonLineH)
			bar(62, theme.StoreSkeletonLineH)
		})
		ui.Box(c).FillWidth().Height(theme.StoreSkeletonMediaH).Radius(theme.RadiusLG).Background(pal.Selected)
		ui.Column(c).FillWidth().Gap(theme.Space12).Children(func() {
			bar(24, theme.StoreSkeletonSubtitleH)
			for _, pct := range []float32{80, 72, 54} {
				ui.Row(c).FillWidth().Gap(theme.Space10).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(theme.Space6, theme.Space6).Radius(theme.RadiusPill).Background(pal.Selected).Shrink(0)
					bar(pct, theme.StoreSkeletonLineH)
				})
			}
		})
	})
}

// renderStoreDetailDownloads puts the download count beside the actions, the
// one number a visitor weighs before installing.
func renderStoreDetailDownloads(c *ui.Context, ctrl *Controller, entry plugins.StoreAppEntry) {
	pal := theme.Ocean()
	downloads := max(entry.Downloads, 0)
	if store := ctrl.sup.Store(); store != nil {
		downloads = store.EffectiveDownloads(entry)
	}
	ui.Column(c.Key("app-store-detail-downloads")).AlignItems(ui.End).Shrink(0).Children(func() {
		ui.Text(c, formatDownloadCount(downloads)).FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).
			FontWeight(theme.WeightTitle).FontFeatures("tnum").TextColor(pal.Text).SingleLine()
		ui.Text(c, theme.Tr("次下载", "downloads")).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
			TextColor(pal.TextSecondary).SingleLine()
	})
}

// renderStoreDetailMeta lays version, author and repository on one quiet line
// under the title, then the permissions as small tags.
func renderStoreDetailMeta(c *ui.Context, entry plugins.StoreAppEntry, locale string) {
	pal := theme.Ocean()
	indent := theme.StoreDetailIconBox + theme.Space16
	ver := strings.TrimSpace(entry.Version)
	author := strings.TrimSpace(entry.Author)
	repo := strings.TrimSpace(entry.Repo)
	catLabel := plugins.CategoryLabel(entry.EffectiveCategory(), locale)
	if ver == "" && author == "" && repo == "" && catLabel == "" && len(entry.Permissions) == 0 && len(entry.FileExtensions) == 0 {
		return
	}

	ui.Column(c.Key("app-store-detail-meta")).FillWidth().MinWidth(0).Gap(theme.Space10).
		Padding(0, 0, 0, indent).Children(func() {
		first := true
		sep := func() {
			if !first {
				ui.Box(c).Size(1, theme.Space12).Background(pal.Hairline).Shrink(0)
			}
			first = false
		}
		if ver != "" || author != "" || repo != "" || catLabel != "" {
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center).Wrap().Children(func() {
				if ver != "" {
					sep()
					ui.Text(c, "v"+strings.TrimPrefix(ver, "v")).Font("monospace").FontSize(theme.FontMeta).
						FontWeight(theme.WeightMono).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary).SingleLine()
				}
				if catLabel != "" {
					sep()
					ui.Text(c, catLabel).FontSize(theme.FontSmall).FixedLineHeight(theme.LineBody).
						TextColor(pal.TextSecondary).SingleLine()
				}
				if author != "" {
					sep()
					ui.Text(c, author).FontSize(theme.FontSmall).FixedLineHeight(theme.LineBody).
						TextColor(pal.TextSecondary).SingleLine()
				}
				if repo != "" {
					sep()
					renderStoreDetailRepoLink(c, repo)
				}
			})
		}
		if len(entry.FileExtensions) > 0 {
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, theme.Tr("支持文件", "Supported Files")).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					TextColor(pal.TextSecondary).SingleLine().Margin(0, theme.Space4, 0, 0)
				for _, ext := range entry.FileExtensions {
					ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).Radius(theme.RadiusSM).
						Background(pal.CodeBG).AlignItems(ui.Center).Shrink(0).Children(func() {
						ui.Text(c, ext).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							FontWeight(theme.WeightMono).TextColor(pal.TextSecondary).SingleLine()
					})
				}
			})
		}
		if len(entry.Permissions) == 0 {
			return
		}
		ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, theme.Tr("需要权限", "Permissions")).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
				TextColor(pal.TextSecondary).SingleLine().Margin(0, theme.Space4, 0, 0)
			for _, p := range entry.Permissions {
				ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).Radius(theme.RadiusSM).
					Background(pal.Shell).AlignItems(ui.Center).Shrink(0).Children(func() {
					ui.Text(c, plugins.PermissionLabel(p, locale)).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(pal.TextSecondary).SingleLine()
				})
			}
		})
	})
}

func renderStoreDetailRepoLink(c *ui.Context, repo string) {
	pal := theme.Ocean()
	link := ui.ButtonBase(c.Key("app-store-detail-repo")).Height(theme.HeightMicro).PaddingX(theme.Space4).Gap(theme.Space4).AlignItems(ui.Center).
		Radius(theme.RadiusXS).Cursor(ui.CursorPointer).Label(repo).MinWidth(0)
	bg := ui.Transparent
	if link.Pressed() {
		bg = pal.Selected
	} else if link.Hovered() {
		bg = pal.Hover
	}
	bui.AnimateSurfaceColors(link, bg, ui.Transparent, false)
	link.Children(func() {
		icons.Draw(c, icons.TbLink, theme.IconInline, pal.MonoIconColors(pal.Link))
		ui.Text(c, repo).FontSize(theme.FontSmall).FixedLineHeight(theme.LineBody).TextColor(pal.Link).
			SingleLine().Ellipsis("…").MinWidth(0)
	})
	if link.Clicked() {
		c.OpenURL("https://github.com/" + repo)
	}
}

func formatDownloadCount(n int) string {
	s := strconv.Itoa(max(n, 0))
	if len(s) <= 3 {
		return s
	}
	var out []byte
	rem := len(s) % 3
	if rem > 0 {
		out = append(out, s[:rem]...)
	}
	for i := rem; i < len(s); i += 3 {
		if len(out) > 0 {
			out = append(out, ',')
		}
		out = append(out, s[i:i+3]...)
	}
	return string(out)
}

func parseReadmeBlocks(text string) *model.Doc {
	doc := markdown.NewCodec().Parse(text)
	for i := range doc.Blocks {
		b := &doc.Blocks[i]
		if b.Kind != model.BlockParagraph || len(b.Spans) != 1 {
			continue
		}
		span := b.Spans[0]
		src := strings.TrimSpace(span.Link)
		if src == "" && strings.HasPrefix(span.Text, "https://") {
			src = strings.TrimSpace(span.Text)
		}
		if editor.IsPreviewVideo(src) {
			b.Kind, b.ImageSrc, b.ImageAlt = model.BlockImage, src, span.Text
			b.Spans = nil
		}
	}
	return doc
}
