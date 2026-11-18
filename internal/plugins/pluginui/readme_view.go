package pluginui

import (
	"context"
	"errors"
	"strings"
	"time"

	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

type readmeResult struct {
	text string
	err  error
}

type storeReadmePreview struct {
	entry     plugins.StoreAppEntry
	installed plugins.InstalledApp
	cancel    context.CancelFunc
	pending   <-chan readmeResult
	result    readmeResult
	markdown  markdown.Options
}

func (ctrl *Controller) openStoreReadme(c *ui.Context, entry plugins.StoreAppEntry, installed plugins.InstalledApp) {
	ctrl.closeStoreReadme()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	result := make(chan readmeResult, 1)
	preview := &storeReadmePreview{entry: entry, installed: installed, cancel: cancel, pending: result}
	preview.markdown.WrapCode = true
	preview.markdown.OnLink = func(target string) {
		if strings.HasPrefix(target, "#") {
			preview.markdown.Anchor = target
			preview.markdown.AnchorVersion++
		}
	}
	ctrl.storeReadme = preview

	store, services := ctrl.sup.Store(), c.Services()
	go func() {
		defer cancel()
		text, err := store.ReadPluginReadme(ctx, entry, installed)
		result <- readmeResult{text: text, err: err}
		services.Invalidate()
	}()
}

func (ctrl *Controller) closeStoreReadme() {
	if preview := ctrl.storeReadme; preview != nil {
		preview.cancel()
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

		ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space16, theme.Space20).Gap(theme.Space12).Children(func() {
			if preview.result.text != "" {
				markdown.View(c.Key("plugin-readme-"+preview.entry.ID), preview.result.text, &preview.markdown)
				return
			}
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
