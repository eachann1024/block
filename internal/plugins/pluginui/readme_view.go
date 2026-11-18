package pluginui

import (
	"context"
	"errors"
	"strings"
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

type storeReadmePreview struct {
	entry     plugins.StoreAppEntry
	installed plugins.InstalledApp
	cancel    context.CancelFunc
	ctx       context.Context
	pending   <-chan readmeResult
	result    readmeResult
	editor    *editor.Session
	media     *readmeMedia
}

func (ctrl *Controller) openStoreReadme(c *ui.Context, entry plugins.StoreAppEntry, installed plugins.InstalledApp) {
	ctrl.closeStoreReadme()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan readmeResult, 1)
	ctrl.storeReadme = &storeReadmePreview{entry: entry, installed: installed, ctx: ctx, cancel: cancel, pending: result}

	store, services := ctrl.sup.Store(), c.Services()
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
			services.Invalidate()
		}
	}()
}

func (ctrl *Controller) closeStoreReadme() {
	if preview := ctrl.storeReadme; preview != nil {
		preview.cancel()
		if preview.editor != nil {
			preview.editor.ClosePreview()
		}
		if preview.media != nil {
			preview.media.close()
		}
		ctrl.storeReadme = nil
	}
}

func (ctrl *Controller) renderStoreReadme(c *ui.Context) {
	preview := ctrl.storeReadme
	if preview == nil {
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
			preview.editor = editor.NewReadOnlyPreview(preview.result.blocks, markdown.NewCodec(), preview.media.asset)
		}
	default:
	}

	pal := theme.Ocean()
	locale := ctrl.sup.Locale()
	title := preview.entry.Name.Resolve(locale)
	open := true
	bui.Dialog(c, "app-store-readme-modal", title, &open, bui.ModalLayout{
		Width: 680, MaxHeight: 600, Centered: true,
	}, func() {
		ui.Row(c).FillWidth().MinWidth(0).Padding(theme.Space12, theme.Space20).
			Gap(theme.Space12).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
			ui.Text(c, title).Grow(1).MinWidth(0).
				FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).FontWeight(theme.WeightTitle).
				TextColor(pal.Text).SingleLine().Ellipsis("…")
			closeButton := bui.IconButton(c, icons.Close, bui.IconButtonOptions{
				ID: "app-store-readme-close", Label: theme.Tr("关闭", "Close"),
			})
			if closeButton.Clicked() {
				open = false
			}
		})

		if preview.editor != nil {
			w, h := c.Size()
			width := max(min(float32(680), w-40), 1)
			height := max(min(float32(600), h-40)-52, 1)
			ui.Box(c.Key("plugin-readme-" + preview.entry.ID)).FillWidth().Height(height).Clip().Children(func() {
				preview.editor.Render(c, width, true)
			})
			return
		}
		ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space16, theme.Space20).Gap(theme.Space12).Children(func() {
			summary := strings.TrimSpace(preview.entry.Summary.Resolve(locale))
			if summary != "" {
				ui.Text(c, summary).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text)
			}
			message := theme.Tr("正在加载 README…", "Loading README…")
			if preview.pending == nil {
				if errors.Is(preview.result.err, plugins.ErrReadmeUnavailable) {
					message = theme.Tr("暂无 README", "No README available")
				} else {
					message = theme.Tr("README 加载失败", "Could not load README")
				}
			}
			ui.Text(c, message).Role(ui.RoleStatus).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
			if preview.pending == nil && !errors.Is(preview.result.err, plugins.ErrReadmeUnavailable) {
				retry := bui.ActionButton(c, "app-store-readme-retry", theme.Tr("重试", "Retry"), nil, bui.ButtonSecondary, false)
				if retry.Clicked() {
					ctrl.openStoreReadme(c, preview.entry, preview.installed)
				}
			}
		})
	})
	if !open {
		ctrl.closeStoreReadme()
	}
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
