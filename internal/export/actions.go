package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"block/internal/files"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

type exportRequest struct {
	doc        *model.Doc
	notePath   string
	format     model.ExportFormat
	prefs      model.ExportPreferences
	previewRev uint64
}

// requestLocked captures the current options and the immutable document snapshot.
func (c *Controller) requestLocked() exportRequest {
	return exportRequest{
		doc:        c.activeDocLocked(),
		notePath:   c.notePath,
		format:     c.format,
		prefs:      c.renderPrefsLocked(),
		previewRev: c.previewRev,
	}
}

func (req exportRequest) copy(services ui.Services) error {
	if req.doc == nil {
		return errors.New(theme.Tr("当前没有可导出的内容", "Nothing to export"))
	}
	format := req.format
	switch format {
	case model.ExportMarkdown, model.ExportHTML:
	default:
		format = model.ExportPNG
	}
	raw, err := markdown.ExportDocument(req.doc, req.notePath, format, req.prefs)
	if err != nil {
		return err
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
		return mygo.Clipboard.Write(data)
	}
	services.WriteClipboardData(data)
	return nil
}

func (c *Controller) CopyToClipboard() error {
	c.mu.Lock()
	req, services := c.requestLocked(), c.services
	c.mu.Unlock()
	return c.copyRequest(req, services)
}

func (c *Controller) copyRequest(req exportRequest, services ui.Services) error {
	err := req.copy(services)
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
	outPath = strings.TrimSpace(outPath)
	if outPath == "" {
		return errors.New("output path cannot be empty")
	}
	if req.doc == nil {
		return errors.New(theme.Tr("当前没有可导出的内容", "Nothing to export"))
	}
	err := markdown.ExportToFile(req.doc, req.notePath, outPath, req.format, req.prefs)
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
	return c.open && c.previewRev == req.previewRev && c.format == req.format
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
		ext := exportExtension(req.format)
		chosen, err := files.SaveDialog(nil, mygo.SaveDialogOptions{
			Title:             saveButtonLabel(req.format),
			DefaultPath:       filepath.Join(dir, name),
			CreateDirectories: true,
			Filters:           []mygo.FileFilter{{Name: strings.ToUpper(ext), Extensions: []string{ext}}},
		})
		if err != nil || strings.TrimSpace(chosen) == "" {
			return
		}
		c.mu.Lock()
		if c.isCurrentRequestLocked(req) {
			c.notice = theme.Tr("正在保存…", "Saving…")
			c.savedPath = ""
		}
		c.mu.Unlock()
		c.notifyRedraw()
		_ = c.saveRequest(req, chosen)
	}()
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
