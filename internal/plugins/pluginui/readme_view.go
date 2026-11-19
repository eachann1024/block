package pluginui

import (
	"context"
	"errors"
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

// openStoreReadme switches the App Store tab to the in-place detail page for entry.
func (ctrl *Controller) openStoreReadme(entry plugins.StoreAppEntry, installed plugins.InstalledApp) {
	ctrl.mu.Lock()
	same := ctrl.storeReadme != nil && ctrl.storeReadme.entry.ID == entry.ID
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
		Label:    theme.Tr("应用商店", "App Store"),
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
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan readmeResult, 1)
	preview := &storeReadmePreview{
		entry:          entry,
		installed:      installed,
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
		doc, err := store.ReadPluginReadme(requestCtx, entry, installed)
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

// renderStoreDetail draws the App Store detail page: a quiet header with the
// single relevant action, the README, and an info column with the metadata the
// list rows leave out.
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
	if catalog := ctrl.sup.Catalog(); catalog != nil {
		for _, app := range catalog.Apps {
			if app.ID == entry.ID {
				entry = app
				break
			}
		}
	}
	installedApp, isInstalled := ctrl.sup.GetInstalled(entry.ID)
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
					if storeAppIsBeta(entry, installedApp, isInstalled) {
						bui.BetaBadge(c, true)
					}
				})
				if summary := strings.TrimSpace(entry.Summary.Resolve(locale)); summary != "" {
					ui.Text(c, summary).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
				}
			})
			ui.Row(c).Gap(theme.Space4).AlignItems(ui.Center).Shrink(0).Children(func() {
				renderStoreDetailActions(c, ctrl, entry, installedApp, isInstalled, installingID, onSelectTab)
			})
		})

		ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
		if storeErr != "" {
			renderStatusTextWithTone(c, storeErr, "danger")
		}

		stacked := pageW > 0 && pageW < theme.StoreDetailStackWidth
		body := ui.Row(c.Key("app-store-detail-body")).FillWidth().MinWidth(0).Gap(theme.Space24).AlignItems(ui.Start)
		if stacked {
			body = ui.Column(c.Key("app-store-detail-body-stacked")).FillWidth().MinWidth(0).Gap(theme.Space24)
		}
		body.Children(func() {
			readmeW := pageW
			if !stacked {
				readmeW = pageW - theme.StoreDetailSideWidth - theme.Space24
			}
			renderStoreDetailReadme(c, ctrl, preview, readmeW, stacked)
			renderStoreDetailInfo(c, ctrl, entry, locale, stacked)
		})
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
		btn := bui.ActionButton(c, "app-store-detail-install", lbl, nil, bui.ButtonPrimary, installingID != "")
		if btn.Clicked() {
			ctrl.sup.InstallEntryAsync(entry, nil)
		}
		return
	}

	uninst := bui.ActionButton(c, "app-store-detail-uninstall", theme.Tr("卸载", "Uninstall"), nil, bui.ButtonGhost, installingID != "")
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
		upBtn := bui.ActionButton(c, "app-store-detail-update", lbl, nil, bui.ButtonAccentOutline, installingID != "")
		if upBtn.Clicked() {
			ctrl.sup.InstallEntryAsync(entry, ctrl.clearPluginState)
		}
	}

	openLabel := theme.Tr("打开", "Open")
	if entry.ID == "block-sync" {
		openLabel = theme.Tr("配置同步", "Configure Sync")
	}
	openStyle := bui.ButtonPrimary
	if hasUpdate {
		openStyle = bui.ButtonSecondary
	}
	openBtn := bui.ActionButton(c, "app-store-detail-open", openLabel, nil, openStyle, false)
	if openBtn.Clicked() && onSelectTab != nil {
		ctrl.closeStoreReadme()
		onSelectTab(modals.TabPluginPrefix + entry.ID)
	}
}

func renderStoreDetailReadme(c *ui.Context, ctrl *Controller, preview *storeReadmePreview, width float32, stacked bool) {
	pal := theme.Ocean()
	_, winH := c.Size()
	readmeH := max(winH*0.62, theme.StoreDetailReadmeMinH)

	col := ui.Column(c.Key("app-store-detail-readme")).MinWidth(0).Grow(1).Shrink(1)
	if stacked {
		col.FillWidth()
	}
	col.Children(func() {
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
		message := theme.Tr("正在加载说明…", "Loading README…")
		failed := false
		if preview.pending == nil {
			if errors.Is(preview.result.err, plugins.ErrReadmeUnavailable) {
				message = theme.Tr("这个应用还没有说明", "This app has no README yet")
			} else {
				message = theme.Tr("说明加载失败", "Could not load README")
				failed = true
			}
		}
		ui.Row(c).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, message).Role(ui.RoleStatus).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			if failed {
				retry := bui.ActionButton(c, "app-store-readme-retry", theme.Tr("重试", "Retry"), nil, bui.ButtonSecondary, false)
				if retry.Clicked() {
					ctrl.loadStoreReadme(preview.entry, preview.installed)
				}
			}
		})
	})
}

func renderStoreDetailInfo(c *ui.Context, ctrl *Controller, entry plugins.StoreAppEntry, locale string, stacked bool) {
	pal := theme.Ocean()
	downloads := max(entry.Downloads, 0)
	if store := ctrl.sup.Store(); store != nil {
		downloads = store.EffectiveDownloads(entry)
	}

	info := ui.Column(c.Key("app-store-detail-info")).MinWidth(0).Gap(theme.Space16).Shrink(0)
	if stacked {
		info.FillWidth()
	} else {
		info.Width(theme.StoreDetailSideWidth)
	}
	info.Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space10).Children(func() {
			if ver := strings.TrimSpace(entry.Version); ver != "" {
				renderStoreDetailField(c, theme.Tr("版本", "Version"), "v"+strings.TrimPrefix(ver, "v"), true)
			}
			if author := strings.TrimSpace(entry.Author); author != "" {
				renderStoreDetailField(c, theme.Tr("作者", "Author"), author, false)
			}
			renderStoreDetailField(c, theme.Tr("下载", "Downloads"), formatDownloadCount(downloads), false)
			if repo := strings.TrimSpace(entry.Repo); repo != "" {
				renderStoreDetailField(c, theme.Tr("仓库", "Repository"), repo, false)
			}
		})

		if len(entry.Permissions) == 0 {
			return
		}
		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space8).
			Padding(theme.Space16, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
			ui.Text(c, theme.Tr("需要的权限", "Permissions")).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			for _, p := range entry.Permissions {
				ui.Text(c, plugins.PermissionLabel(p, locale)).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).SingleLine().Ellipsis("…")
			}
		})
	})
}

func renderStoreDetailField(c *ui.Context, label, value string, mono bool) {
	pal := theme.Ocean()
	ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary).Shrink(0).SingleLine()
		v := ui.Text(c, value).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).
			Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if mono {
			v.Font("monospace").FontSize(theme.FontSmall).FontWeight(theme.WeightMono)
		}
	})
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
