package export

import (
	"context"

	"block/internal/markdown"
	"block/internal/mindmap"
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
)

func init() {
	markdown.RegisterMindMapRenderer(mindmap.RenderMarkdownRaster)
}

// queuePreviewLocked invalidates the visible result. One worker renders the
// latest options, so rapidly switching themes cannot pile up large bitmaps.
func (c *Controller) queuePreviewLocked() {
	c.previewRev++
	c.previewBitmap = nil
	c.previewW, c.previewH = 0, 0
	c.previewErr = ""
	if c.previewCancel != nil {
		c.previewCancel()
	}
	if !c.previewRunning && c.open {
		c.previewRunning = true
		go c.renderPreviews()
	}
}

func (c *Controller) renderPreviews() {
	for {
		c.mu.Lock()
		if !c.open {
			c.previewRunning = false
			c.mu.Unlock()
			return
		}
		req := c.requestLocked()
		ctx, cancel := context.WithCancel(context.Background())
		c.previewCancel = cancel
		c.mu.Unlock()

		var bitmap *ui.Bitmap
		var w, h int
		var message string
		if req.doc == nil {
			message = theme.Tr("当前没有可导出的内容", "Nothing to export")
		} else {
			img, outW, outH, err := markdown.RenderExportPreview(ctx, req.doc, req.notePath, req.prefs)
			if err == nil && ctx.Err() == nil {
				bitmap, w, h = ui.NewBitmap(img), outW, outH
			} else if err != nil {
				message = err.Error()
			}
		}
		cancel()

		c.mu.Lock()
		c.previewCancel = nil
		if c.open && req.previewRev != c.previewRev {
			c.mu.Unlock()
			continue
		}
		publish := c.open
		if publish {
			c.previewBitmap, c.previewW, c.previewH, c.previewErr = bitmap, w, h, message
		}
		c.previewRunning = false
		c.mu.Unlock()
		if publish {
			c.notifyRedraw()
		}
		return
	}
}
