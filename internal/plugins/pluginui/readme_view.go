package pluginui

import (
	"context"
	"errors"
	"strings"
	"time"

	"block/internal/editor"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo"
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

func (ctrl *Controller) openStoreReadme(entry plugins.StoreAppEntry, installed plugins.InstalledApp) {
	if !mygo.App.IsReady() {
		return
	}
	title := entry.Name.Resolve(ctrl.sup.Locale())
	win := ctrl.storeReadmeWindow
	if win == nil || win.IsDestroyed() {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title: title, Width: 760, Height: 680, MinWidth: 400, MinHeight: 280,
			Hidden: true, Content: ui.View(ctrl.renderStoreReadme),
			BackgroundColor: "light-dark(#ffffff, #222222)",
		})
		ctrl.storeReadmeWindow = win
		win.OnClosed(func() {
			if ctrl.storeReadmeWindow == win {
				ctrl.storeReadmeWindow = nil
				ctrl.closeStoreReadme()
			}
		})
	} else {
		win.SetTitle(title)
	}
	if ctrl.storeReadme == nil || ctrl.storeReadme.entry.ID != entry.ID {
		ctrl.loadStoreReadme(win, entry, installed)
	}
	win.Show()
	win.Focus()
}

func (ctrl *Controller) loadStoreReadme(win *mygo.Window, entry plugins.StoreAppEntry, installed plugins.InstalledApp) {
	ctrl.closeStoreReadme()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan readmeResult, 1)
	ctrl.storeReadme = &storeReadmePreview{entry: entry, installed: installed, ctx: ctx, cancel: cancel, pending: result}

	store := ctrl.sup.Store()
	win.Invalidate()
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
			win.Invalidate()
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

// CloseReadmeWindow handles the native menu accelerator for this window.
func (ctrl *Controller) CloseReadmeWindow(win *mygo.Window) bool {
	if win == nil || win != ctrl.storeReadmeWindow {
		return false
	}
	win.Close()
	return true
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

	win := ctrl.storeReadmeWindow
	if win != nil && (c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW)) {
		win.Update(win.Close)
		return
	}

	pal := theme.Ocean()
	uiTheme := pal.ToUITheme("")
	uiTheme.ScrollbarStyle = ui.ScrollbarStyle{
		TrackWidth: theme.ScrollGutter, ThumbWidth: theme.ScrollThumbWidth, MinLength: 32,
		Thumb: pal.ScrollThumb, Hover: pal.ScrollThumbHover, Active: pal.ScrollThumbHover,
	}
	c.SetTheme(&uiTheme)
	locale := ctrl.sup.Locale()
	ui.Column(c.Key("plugin-readme-" + preview.entry.ID)).Fill().Background(pal.Paper).Children(func() {
		if preview.editor != nil {
			w, _ := c.Size()
			preview.editor.Render(c, w, true)
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
					if win != nil {
						ctrl.loadStoreReadme(win, preview.entry, preview.installed)
					}
				}
			}
		})
	})
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
