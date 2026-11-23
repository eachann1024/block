package pluginui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"block/internal/editor"
	"block/internal/icons"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

type readmeResult struct {
	doc    plugins.PluginReadme
	blocks *model.Doc
	err    error
}

type readmeMediaEntry struct {
	src     string
	alt     string
	isVideo bool
}

// storeReadmePreview is the App Store detail page shown in place of the list.
type storeReadmePreview struct {
	entry            plugins.StoreAppEntry
	installed        plugins.InstalledApp
	locale           string
	dark             bool
	activeTab        string
	cancel           context.CancelFunc
	ctx              context.Context
	pending          <-chan readmeResult
	result           readmeResult
	editorMu         sync.Mutex
	editor           *editor.Session
	media            *readmeMedia
	lightbox         bui.LightboxState
	lightboxItems    []readmeMediaEntry
	changelogMu      sync.Mutex
	changelogCancel  context.CancelFunc
	changelogPending <-chan changelogResult
	changelogLoaded  bool
	changelogDoc     plugins.PluginChangelog
	changelogErr     error
	changelogDark    bool
	changelogEditor  *editor.Session
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
	backBtn := bui.BtnBack(c, "app-store-detail-back", bui.PresetOptions{
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

func (ctrl *Controller) resolveStoreDetailState() (
	entry plugins.StoreAppEntry,
	installedApp plugins.InstalledApp,
	isInstalled bool,
	installingID string,
	ok bool,
) {
	ctrl.mu.Lock()
	preview := ctrl.storeReadme
	ctrl.mu.Unlock()
	if preview == nil || ctrl.sup == nil {
		return plugins.StoreAppEntry{}, plugins.InstalledApp{}, false, "", false
	}
	entry = preview.entry
	inCatalog := false
	if catalog := ctrl.sup.Catalog(); catalog != nil {
		for _, app := range catalog.Apps {
			if app.ID == entry.ID {
				entry = app
				inCatalog = true
				break
			}
		}
	}
	installedApp, isInstalled = ctrl.sup.GetInstalled(entry.ID)
	if isInstalled {
		if !inCatalog {
			entry.DownloadURL = ""
			entry.SHA256 = ""
			entry.Verified = false
			entry.Version = installedApp.Manifest.Version
			entry.Permissions = installedApp.Manifest.Permissions
		}
		if len(installedApp.Manifest.PermissionReasons) > 0 {
			merged := plugins.ClonePermissionReasons(entry.PermissionReasons)
			if merged == nil {
				merged = make(map[plugins.Permission]plugins.LocalizedText, len(installedApp.Manifest.PermissionReasons))
			}
			for perm, reason := range installedApp.Manifest.PermissionReasons {
				if !inCatalog || merged[perm].IsEmpty() {
					merged[perm] = reason
				}
			}
			entry.PermissionReasons = merged
		}
	}
	_, installingID, _, _ = ctrl.sup.StoreStatus()
	return entry, installedApp, isInstalled, installingID, true
}

func (ctrl *Controller) loadStoreReadme(entry plugins.StoreAppEntry, installed plugins.InstalledApp) {
	ctrl.closeStoreReadme()
	locale := ctrl.sup.Locale()
	dark := theme.IsDark()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan readmeResult, 1)
	preview := &storeReadmePreview{
		entry:     entry,
		installed: installed,
		locale:    readmeLocaleKey(locale),
		dark:      dark,
		ctx:       ctx,
		cancel:    cancel,
		pending:   result,
	}
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
			reply.blocks = parseReadmeBlocks(doc.Text, dark)
		}
		result <- reply
		if ctx.Err() == nil {
			ctrl.sup.NotifyRedraw()
		}
	}()
}

func (ctrl *Controller) detachStoreReadmeLocked(preview *storeReadmePreview) {
	if ctrl.storeReadme == preview {
		ctrl.storeReadme = nil
	}
}

func releaseStoreReadmePreview(preview *storeReadmePreview) {
	if preview == nil {
		return
	}
	preview.lightbox.Open = false
	preview.cancel()
	preview.editorMu.Lock()
	if preview.editor != nil {
		preview.editor.ClosePreview()
		preview.editor = nil
	}
	preview.editorMu.Unlock()
	preview.changelogMu.Lock()
	if preview.changelogCancel != nil {
		preview.changelogCancel()
		preview.changelogCancel = nil
	}
	if preview.changelogEditor != nil {
		preview.changelogEditor.ClosePreview()
		preview.changelogEditor = nil
	}
	preview.changelogMu.Unlock()
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

func collectReadmeMedia(doc *model.Doc) []readmeMediaEntry {
	if doc == nil {
		return nil
	}
	var out []readmeMediaEntry
	for _, b := range doc.Blocks {
		if b.Kind != model.BlockImage {
			continue
		}
		src := strings.TrimSpace(b.ImageSrc)
		if src == "" {
			continue
		}
		isVideo := editor.IsPreviewVideo(src) || strings.Contains(strings.ToLower(b.CleanSource), "<video ")
		out = append(out, readmeMediaEntry{
			src:     src,
			alt:     strings.TrimSpace(b.ImageAlt),
			isVideo: isVideo,
		})
	}
	return out
}

func (p *storeReadmePreview) attachEditorHooks(ed *editor.Session) {
	if p == nil || ed == nil {
		return
	}
	ed.SetPreviewMediaHooks(
		func(src string, isVideo bool) {
			p.openLightboxFor(src, isVideo)
		},
		func(src string) (bool, bool) {
			if !p.lightbox.Open {
				return false, false
			}
			if p.lightbox.Index >= 0 && p.lightbox.Index < len(p.lightboxItems) {
				return p.lightboxItems[p.lightbox.Index].src == src, true
			}
			return false, true
		},
	)
}

func (p *storeReadmePreview) openLightboxFor(src string, isVideo bool) {
	if p == nil || src == "" {
		return
	}
	idx := -1
	for i, item := range p.lightboxItems {
		if item.src == src && item.isVideo == isVideo {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i, item := range p.lightboxItems {
			if item.src == src {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		p.lightboxItems = append(p.lightboxItems, readmeMediaEntry{src: src, isVideo: isVideo})
		idx = len(p.lightboxItems) - 1
	}
	if isVideo && p.media != nil {
		p.media.asset(src, true, true)
	}
	p.lightbox.Open = true
	p.lightbox.Index = idx
	p.lightbox.ResetView()
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
			preview.lightboxItems = collectReadmeMedia(preview.result.blocks)
			preview.editorMu.Lock()
			ed := editor.NewReadOnlyPreview(preview.result.blocks, markdown.NewCodec(), preview.media.asset)
			preview.attachEditorHooks(ed)
			preview.editor = ed
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
	ctrl.pollStoreReadme(c, preview)
	ctrl.pollStoreChangelog(preview)

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
		prevTab := preview.activeTab
		ctrl.loadStoreReadme(entry, installedApp)
		ctrl.mu.Lock()
		preview = ctrl.storeReadme
		ctrl.mu.Unlock()
		if preview == nil {
			return
		}
		preview.activeTab = prevTab
		if prevTab == storeDetailTabChangelog {
			ctrl.loadStoreChangelog(preview, entry, installedApp)
		}
	}
	if curDark := theme.IsDark(); preview.pending == nil && preview.result.err == nil && preview.result.doc.Text != "" && preview.dark != curDark {
		preview.dark = curDark
		blocks := parseReadmeBlocks(preview.result.doc.Text, curDark)
		preview.result.blocks = blocks
		preview.lightboxItems = collectReadmeMedia(blocks)
		if preview.lightbox.Index >= len(preview.lightboxItems) && len(preview.lightboxItems) > 0 {
			preview.lightbox.Index = len(preview.lightboxItems) - 1
		}
		preview.editorMu.Lock()
		if preview.editor != nil {
			preview.editor.ClosePreview()
		}
		if preview.media != nil {
			ed := editor.NewReadOnlyPreview(blocks, markdown.NewCodec(), preview.media.asset)
			preview.attachEditorHooks(ed)
			preview.editor = ed
		}
		preview.editorMu.Unlock()
	}
	if curDark := theme.IsDark(); preview.changelogDark != curDark {
		preview.changelogMu.Lock()
		if preview.changelogPending == nil {
			preview.changelogDark = curDark
			if preview.changelogEditor != nil && strings.TrimSpace(preview.changelogDoc.Markdown) != "" {
				preview.changelogEditor.ClosePreview()
				clBlocks := parseReadmeBlocks(preview.changelogDoc.Markdown, curDark)
				preview.changelogEditor = editor.NewReadOnlyPreview(clBlocks, markdown.NewCodec(), nil)
			}
		}
		preview.changelogMu.Unlock()
	}
	_, _, storeErr, _ := ctrl.sup.StoreStatus()

	page := ui.Column(c.Key("app-store-detail-" + entry.ID)).FillWidth().MinWidth(0).Gap(theme.Space24)
	pageW := page.Bounds().W
	if pageW <= 0 {
		if vpW, _ := c.Size(); vpW > 0 {
			pageW = vpW - theme.SidebarMinWidth - 2*theme.Space24
			c.Invalidate()
		}
	}

	lightboxWasOpen := preview.lightbox.Open

	page.Children(func() {
		if !lightboxWasOpen && !preview.lightbox.Open && c.Shortcut(0, ui.KeyEscape) {
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
			renderStoreDetailDownloads(c, ctrl, entry)
		})

		renderStoreDetailMeta(c, entry, locale)

		renderStoreDetailTabs(c, ctrl, preview, entry, installedApp)
		errMsg := storeErr
		if errMsg == "" {
			errMsg = strings.TrimSpace(ctrl.sup.UninstallError(entry.ID))
		}
		if errMsg != "" {
			renderStatusTextWithTone(c, errMsg, "danger")
		}
		if preview.activeTab == storeDetailTabChangelog {
			renderStoreDetailChangelog(c, ctrl, preview, entry, installedApp, isInstalled, locale, pageW)
		} else {
			renderStoreDetailReadme(c, ctrl, preview, entry, installedApp, pageW)
		}
	})

	renderStoreDetailLightbox(c, preview)
}

func readmeMediaDisplayName(entry readmeMediaEntry) string {
	if entry.alt != "" {
		return entry.alt
	}
	if u, err := url.Parse(entry.src); err == nil && u.Path != "" {
		if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
			return base
		}
	}
	if entry.isVideo {
		return theme.Tr("演示视频", "Demo Video")
	}
	return theme.Tr("应用截图", "App Screenshot")
}

func renderStoreDetailLightbox(c *ui.Context, preview *storeReadmePreview) {
	if preview == nil || !preview.lightbox.Open || len(preview.lightboxItems) == 0 || preview.media == nil {
		return
	}
	items := make([]bui.LightboxItem, len(preview.lightboxItems))
	for i, entry := range preview.lightboxItems {
		item := bui.LightboxItem{
			ID:      fmt.Sprintf("store-media-%d", i),
			Name:    readmeMediaDisplayName(entry),
			OpenURL: preview.media.resolveOpenURL(entry.src),
		}
		if entry.isVideo {
			item.IsVideo = true
			asset := preview.media.asset(entry.src, true, false)
			item.Loading = asset.Loading
			item.Unavailable = asset.Unavailable
			preview.editorMu.Lock()
			ed := preview.editor
			var player *ui.VideoPlayer
			var ok bool
			if ed != nil {
				player, ok = ed.ResolvePreviewVideoPlayer(entry.src)
			}
			preview.editorMu.Unlock()
			if ok && player != nil {
				item.Video = player
				item.Loading = false
				item.Unavailable = false
				vw, vh := player.Size()
				dur := player.Duration()
				switch {
				case vw > 0 && vh > 0 && dur > 0:
					item.Meta = fmt.Sprintf("%d × %d · %s", vw, vh, bui.FormatMediaDuration(dur))
				case vw > 0 && vh > 0:
					item.Meta = fmt.Sprintf("%d × %d", vw, vh)
				case dur > 0:
					item.Meta = bui.FormatMediaDuration(dur)
				default:
					item.Meta = theme.Tr("视频", "Video")
				}
			} else if len(asset.Video) > 0 {
				item.Unavailable = true
			}
		} else {
			asset := preview.media.asset(entry.src, false, true)
			item.Loading = asset.Loading
			item.Unavailable = asset.Unavailable
			if asset.Animation != nil {
				item.IsImage = true
				item.IsAnimation = true
				item.Video = asset.Animation
				vw, vh := asset.Animation.Size()
				item.ImageW, item.ImageH = vw, vh
				if vw > 0 && vh > 0 {
					item.Meta = fmt.Sprintf("GIF · %d × %d", vw, vh)
				} else {
					item.Meta = "GIF"
				}
			} else {
				item.IsImage = true
				item.Image = asset.Image
				if asset.Image != nil {
					sw, sh := ui.ImageSize(asset.Image)
					item.ImageW, item.ImageH = int(math.Round(float64(sw))), int(math.Round(float64(sh)))
					if item.ImageW > 0 && item.ImageH > 0 {
						item.Meta = fmt.Sprintf("%d × %d", item.ImageW, item.ImageH)
					}
				}
			}
		}
		items[i] = item
	}

	cb := bui.LightboxCallbacks{
		OnClose: func() {
			preview.editorMu.Lock()
			ed := preview.editor
			preview.editorMu.Unlock()
			if ed != nil {
				ed.PauseAllVideos()
			}
			c.Invalidate()
		},
		OnSelect: func(idx int) {
			if idx >= 0 && idx < len(preview.lightboxItems) && preview.lightboxItems[idx].isVideo {
				preview.media.asset(preview.lightboxItems[idx].src, true, true)
			}
			c.Invalidate()
		},
		OnRequestLoad: func(idx int) {
			if idx >= 0 && idx < len(preview.lightboxItems) && preview.lightboxItems[idx].isVideo {
				preview.media.asset(preview.lightboxItems[idx].src, true, true)
			}
		},
		OnCopy: func(idx int) bool {
			if idx < 0 || idx >= len(preview.lightboxItems) {
				return false
			}
			entry := preview.lightboxItems[idx]
			return preview.media.copyMedia(c.Services(), entry.src, entry.isVideo)
		},
		OnOpenURL: func(idx int) {
			if idx < 0 || idx >= len(items) || items[idx].OpenURL == "" {
				return
			}
			c.OpenURL(items[idx].OpenURL)
		},
	}

	bui.RenderLightbox(c, "store-media-lightbox", &preview.lightbox, items, cb)
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

	ui.Column(c.Key("app-store-detail-readme")).FillWidth().MinWidth(0).Children(func() {
		preview.editorMu.Lock()
		ed := preview.editor
		if ed != nil {
			ui.Column(c.Key("app-store-detail-readme-view")).FillWidth().MinWidth(0).Children(func() {
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
			FontWeight(theme.WeightControl).FontFeatures("tnum").TextColor(pal.Text).SingleLine()
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
	sensitivePerms := plugins.SensitivePermissions(entry.Permissions)
	if ver == "" && author == "" && repo == "" && catLabel == "" && len(sensitivePerms) == 0 && len(entry.FileExtensions) == 0 {
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
		if len(sensitivePerms) == 0 {
			return
		}
		ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, theme.Tr("需要权限", "Permissions")).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
				TextColor(pal.TextSecondary).SingleLine().Margin(0, theme.Space4, 0, 0)
			for _, p := range sensitivePerms {
				perm := p
				label := plugins.PermissionLabel(perm, locale)
				customReason := ""
				if entry.PermissionReasons != nil {
					customReason = entry.PermissionReasons[perm].Resolve(locale)
				}
				defaultDesc := plugins.PermissionDescription(perm, locale)
				card := bui.TooltipCardSpec{
					Title:     label,
					Code:      string(perm),
					Placement: bui.TooltipBelow,
				}
				if customReason != "" {
					card.Body = customReason
					card.Note = theme.Tr("权限说明：", "Scope: ") + defaultDesc
				} else {
					card.Body = defaultDesc
				}
				tag := ui.Row(c.Key("app-store-detail-perm-" + string(perm))).Height(theme.HeightMicro).PaddingX(theme.Space8).Radius(theme.RadiusSM).
					AlignItems(ui.Center).Shrink(0)
				bg := pal.Shell
				border := ui.Transparent
				textCol := pal.TextSecondary
				if tag.Hovered() {
					bg = pal.Hover
					border = pal.BtnBorder
					textCol = pal.Text
				}
				bui.AnimateSurfaceColors(tag, bg, border, true)
				bui.TooltipCard(c, tag, card)
				tag.Children(func() {
					ui.Text(c, label).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(textCol).SingleLine()
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
		icons.DrawAnimated(c, icons.TbLink, theme.IconInline, pal.MonoIconColors(pal.Link), link.Hovered() || link.Pressed())
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

var (
	readmePictureRe = regexp.MustCompile(`(?is)<picture\b[^>]*>(.*?)</picture>`)
	readmeSourceRe  = regexp.MustCompile(`(?is)<source\b([^>]*)/?>`)
	readmeImgRe     = regexp.MustCompile(`(?is)<img\b([^>]*)/?>`)
	readmeAttrRe    = regexp.MustCompile(`(?is)([a-z0-9_-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
)

func htmlAttr(attrs, name string) string {
	for _, m := range readmeAttrRe.FindAllStringSubmatch(attrs, -1) {
		if strings.EqualFold(m[1], name) {
			if m[2] != "" {
				return strings.TrimSpace(m[2])
			}
			return strings.TrimSpace(m[3])
		}
	}
	return ""
}

func resolvePictureBlocks(text string, dark bool) string {
	want := "light"
	if dark {
		want = "dark"
	}
	return readmePictureRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := readmePictureRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		body := inner[1]
		var chosen string
		for _, sm := range readmeSourceRe.FindAllStringSubmatch(body, -1) {
			media := strings.ToLower(htmlAttr(sm[1], "media"))
			srcset := htmlAttr(sm[1], "srcset")
			if srcset == "" {
				continue
			}
			// Take the first URL token in srcset (strip optional 1x/2x descriptor).
			first := strings.Fields(strings.Split(srcset, ",")[0])
			if len(first) == 0 {
				continue
			}
			if strings.Contains(media, "prefers-color-scheme") && strings.Contains(media, want) {
				chosen = first[0]
				break
			}
		}
		var alt, fallback string
		if im := readmeImgRe.FindStringSubmatch(body); len(im) >= 2 {
			alt = htmlAttr(im[1], "alt")
			fallback = htmlAttr(im[1], "src")
		}
		if chosen == "" {
			chosen = fallback
		}
		if chosen == "" {
			return match
		}
		alt = strings.ReplaceAll(alt, "]", `\]`)
		return "![" + alt + "](" + chosen + ")"
	})
}

func parseReadmeBlocks(text string, dark bool) *model.Doc {
	doc := markdown.NewCodec().Parse(resolvePictureBlocks(text, dark))
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
