package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"block/internal/files"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

type cachedExportPayload struct {
	rev       uint64
	fontEpoch uint64
	data      []byte
}

type exportRequest struct {
	doc           *model.Doc
	notePath      string
	format        model.ExportFormat
	prefs         model.ExportPreferences
	previewRev    uint64
	exportDataRev uint64
}

// requestLocked captures the current options and the immutable document snapshot.
func (c *Controller) requestLocked() exportRequest {
	return exportRequest{
		doc:           c.activeDocLocked(),
		notePath:      c.notePath,
		format:        c.format,
		prefs:         c.renderPrefsLocked(),
		previewRev:    c.previewRev,
		exportDataRev: c.exportDataRev,
	}
}

func (c *Controller) exportBytes(req exportRequest, format model.ExportFormat) ([]byte, error) {
	// Media permissions and file identity can change while the export panel is open.
	cacheable := format == model.ExportMarkdown || !hasExportMedia(req.doc)
	startEpoch := theme.FontEpoch()
	if cacheable {
		c.mu.Lock()
		entry, ok := c.cachedExports[format]
		current := c.exportDataRev == req.exportDataRev &&
			entry.rev == req.exportDataRev &&
			entry.fontEpoch == startEpoch
		c.mu.Unlock()
		if ok && current {
			return entry.data, nil
		}
	}

	prefs := req.prefs
	prefs.Format = format
	if prefs.Watermark != nil {
		wm := *prefs.Watermark && formatSupportsWatermark(format)
		prefs.Watermark = &wm
	}
	raw, err := markdown.ExportDocument(req.doc, req.notePath, format, prefs)
	if err != nil {
		return nil, err
	}

	endEpoch := theme.FontEpoch()
	c.mu.Lock()
	if cacheable && c.open && c.exportDataRev == req.exportDataRev && endEpoch == startEpoch {
		if c.cachedExports == nil {
			c.cachedExports = make(map[model.ExportFormat]cachedExportPayload)
		}
		c.cachedExports[format] = cachedExportPayload{
			rev:       req.exportDataRev,
			fontEpoch: startEpoch,
			data:      raw,
		}
	}
	c.mu.Unlock()
	return raw, nil
}

func hasExportMedia(doc *model.Doc) bool {
	if doc != nil {
		for i := range doc.Blocks {
			if doc.Blocks[i].Kind == model.BlockImage {
				return true
			}
		}
	}
	return false
}

func writeExportFileAtomic(outPath string, data []byte) error {
	dir := filepath.Dir(outPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".block-export-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (c *Controller) copyWithCache(req exportRequest, services ui.Services) (int, error) {
	if req.doc == nil {
		return 0, errors.New(theme.Tr("当前没有可导出的内容", "Nothing to export"))
	}
	format := req.format
	switch format {
	case model.ExportMarkdown, model.ExportHTML:
	default:
		format = model.ExportPNG
	}
	raw, err := c.exportBytes(req, format)
	if err != nil {
		return 0, err
	}
	var item transfer.Item
	switch format {
	case model.ExportMarkdown:
		item = transfer.NewItem(transfer.Bytes(transfer.Text, raw), transfer.Bytes(transfer.Markdown, raw))
	case model.ExportHTML:
		item = transfer.NewItem(transfer.Bytes(transfer.Text, raw), transfer.Bytes(transfer.HTML, raw))
	default:
		item = transfer.NewItem(transfer.Bytes(transfer.PNG, raw))
	}
	data := transfer.New(item)
	if mygo.App.IsReady() {
		return len(raw), mygo.Clipboard.Write(data)
	}
	services.WriteClipboardData(data)
	return len(raw), nil
}

func (c *Controller) CopyToClipboard() error {
	c.mu.Lock()
	req, services := c.requestLocked(), c.services
	c.mu.Unlock()
	return c.copyRequest(req, services)
}

func (c *Controller) copyRequest(req exportRequest, services ui.Services) error {
	start := time.Now()
	files.LogInfof("export started: target=clipboard format=%s", req.format)
	size, err := c.copyWithCache(req, services)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		files.LogErrorf("export failed: target=clipboard format=%s elapsed=%s err=%v", req.format, elapsed, err)
	} else {
		files.LogInfof("export succeeded: target=clipboard format=%s bytes=%d elapsed=%s", req.format, size, elapsed)
	}
	c.mu.Lock()
	if c.isCurrentRequestLocked(req) {
		if err != nil {
			c.notice = err.Error()
		} else {
			c.copied = true
			c.notice = theme.Tr("已复制到剪贴板", "Copied to clipboard")
		}
	}
	c.mu.Unlock()
	c.notifyRedraw()
	return err
}

func (c *Controller) SaveToPath(outPath string) error {
	c.mu.Lock()
	req := c.requestLocked()
	c.mu.Unlock()
	return c.saveRequest(req, outPath)
}

func (c *Controller) saveRequest(req exportRequest, outPath string) error {
	start := time.Now()
	files.LogInfof("export started: target=file format=%s", req.format)
	outPath = strings.TrimSpace(outPath)
	if outPath == "" {
		err := errors.New("output path cannot be empty")
		files.LogErrorf("export failed: target=file format=%s elapsed=%s err=%v", req.format, time.Since(start).Round(time.Millisecond), err)
		return err
	}
	if req.doc == nil {
		err := errors.New(theme.Tr("当前没有可导出的内容", "Nothing to export"))
		files.LogErrorf("export failed: target=file format=%s elapsed=%s err=%v", req.format, time.Since(start).Round(time.Millisecond), err)
		return err
	}
	data, err := c.exportBytes(req, req.format)
	if err == nil {
		err = writeExportFileAtomic(outPath, data)
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		files.LogErrorf("export failed: target=file format=%s elapsed=%s err=%v", req.format, elapsed, err)
	} else {
		files.LogInfof("export succeeded: target=file format=%s bytes=%d elapsed=%s", req.format, len(data), elapsed)
	}
	if absOut, absErr := filepath.Abs(outPath); absErr == nil {
		outPath = absOut
	}
	c.mu.Lock()
	if c.isCurrentRequestLocked(req) {
		if err != nil {
			c.notice = err.Error()
			c.savedPath = ""
		} else {
			c.savedPath = outPath
			c.exportDir = filepath.Dir(outPath)
			c.savePreferencesLocked()
			c.notice = fmt.Sprintf("%s %s", theme.Tr("已保存", "Saved"), filepath.Base(outPath))
		}
	}
	c.mu.Unlock()
	c.notifyRedraw()
	return err
}

func (c *Controller) isCurrentRequestLocked(req exportRequest) bool {
	return c.open && c.exportDataRev == req.exportDataRev && c.format == req.format
}

func (c *Controller) triggerCopy() {
	c.mu.Lock()
	if !c.open || c.actionRunning {
		c.mu.Unlock()
		return
	}
	req, services := c.requestLocked(), c.services
	if !mygo.App.IsReady() {
		c.mu.Unlock()
		_ = c.copyRequest(req, services)
		return
	}
	c.actionRunning = true
	c.notice = theme.Tr("正在复制…", "Copying…")
	c.savedPath = ""
	c.copied = false
	c.mu.Unlock()
	c.notifyRedraw()

	go func() {
		defer c.finishAction()
		_ = c.copyRequest(req, services)
	}()
}

func (c *Controller) triggerSave() {
	c.mu.Lock()
	if !c.open || c.actionRunning {
		c.mu.Unlock()
		return
	}
	req := c.requestLocked()
	dir := c.defaultExportDirLocked()
	name := suggestedExportFilename(c.title, req.format)
	hook := c.OnSaveDialog
	if hook != nil {
		c.mu.Unlock()
		hook(dir, name, func(chosen string) {
			if strings.TrimSpace(chosen) != "" {
				_ = c.saveRequest(req, chosen)
			}
		})
		return
	}
	if !mygo.App.IsReady() {
		c.mu.Unlock()
		return
	}
	c.actionRunning = true
	c.mu.Unlock()
	c.notifyRedraw()

	go func() {
		defer c.finishAction()
		chosen := uniqueExportPath(filepath.Join(downloadsDir(dir), name))
		c.mu.Lock()
		if c.isCurrentRequestLocked(req) {
			c.notice = theme.Tr("正在保存…", "Saving…")
			c.savedPath = ""
		}
		c.mu.Unlock()
		c.notifyRedraw()
		if err := c.saveRequest(req, chosen); err == nil {
			mygo.Shell.ShowItemInFolder(chosen)
		}
	}()
}

// downloadsDir returns the user's Downloads folder, falling back to fallback.
func downloadsDir(fallback string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		d := filepath.Join(home, "Downloads")
		if os.MkdirAll(d, 0o755) == nil {
			return d
		}
	}
	return fallback
}

// uniqueExportPath appends " 2", " 3"… before the extension when p already exists.
func uniqueExportPath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 2; i < 10000; i++ {
		cand := fmt.Sprintf("%s %d%s", base, i, ext)
		if _, err := os.Stat(cand); err != nil {
			return cand
		}
	}
	return p
}

func (c *Controller) finishAction() {
	c.mu.Lock()
	c.actionRunning = false
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) defaultExportDirLocked() string {
	if c.exportDir != "" {
		cur := filepath.Clean(c.exportDir)
		for cur != "" && cur != string(filepath.Separator) && cur != "." {
			if info, err := os.Stat(cur); err == nil && info.IsDir() {
				return cur
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			cur = parent
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, sub := range []string{"Downloads", "Documents", ""} {
			cand := home
			if sub != "" {
				cand = filepath.Join(home, sub)
			}
			if info, err := os.Stat(cand); err == nil && info.IsDir() {
				return cand
			}
		}
	}
	return os.TempDir()
}

func (c *Controller) revealSaved() {
	c.mu.Lock()
	p := c.savedPath
	c.mu.Unlock()
	if p != "" && mygo.App.IsReady() {
		mygo.Shell.ShowItemInFolder(p)
	}
}
