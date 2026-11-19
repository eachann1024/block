package ai

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

const (
	maxComposerAttachments = 10
	maxAttachmentBytes     = 20 << 20
	maxAttachmentTextBytes = 256 << 10
	maxInlineImageBytes    = 8 << 20
)

func (c *Controller) attachmentsDir() string {
	return filepath.Join(c.dataDir, "ai-attachments")
}

// handleComposerPaste turns a pasted file list or bitmap into draft attachments.
// It reports false for ordinary text so the input pastes it as usual.
func (c *Controller) handleComposerPaste(svc ui.Services) bool {
	if paths := clipboardPaths(svc.ReadClipboardData(transfer.FileList)); len(paths) > 0 {
		if c.addAttachmentFiles(paths) < len(paths) {
			c.showAttachmentPasteError()
		}
		return true
	}
	// Browsers can put an image and its URL on the clipboard together.
	data := svc.ReadClipboardData(transfer.PNG, "image/jpeg", "image/webp", "image/gif")
	for _, f := range []struct {
		format transfer.Format
		ext    string
	}{{transfer.PNG, "png"}, {"image/jpeg", "jpg"}, {"image/webp", "webp"}, {"image/gif", "gif"}} {
		if raw, err := data.Read(f.format); err == nil && len(raw) > 0 {
			if detectMime(raw, "") != string(f.format) {
				continue
			}
			if !c.addAttachmentBytes(raw, "image."+f.ext) {
				c.showAttachmentPasteError()
			}
			return true
		}
	}
	return false
}

func (c *Controller) showAttachmentPasteError() {
	c.mu.Lock()
	c.turnError = theme.Tr("部分附件无法添加。最多 10 个文件，每个文件最多 20 MB。", "Some attachments could not be added. Add up to 10 files, up to 20 MB each.")
	c.turnErrorSettings = false
	c.mu.Unlock()
	c.requestRedraw()
}

func clipboardPaths(data transfer.Data) []string {
	if len(data.Items()) == 0 {
		return nil
	}
	paths, _ := data.Files()
	return paths
}

func (c *Controller) addAttachmentFiles(paths []string) int {
	added := 0
	for _, p := range paths {
		c.mu.RLock()
		full := len(c.draftAttachments) >= maxComposerAttachments
		c.mu.RUnlock()
		if full {
			break
		}
		raw, err := readAttachmentSource(p)
		if err == nil && c.addAttachmentBytes(raw, filepath.Base(p)) {
			added++
		}
	}
	return added
}

func readAttachmentSource(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxAttachmentBytes {
		return nil, errors.New("invalid attachment file type or size")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxAttachmentBytes+1))
}

// addAttachmentBytes stores a content-addressed copy and appends it to the draft.
func (c *Controller) addAttachmentBytes(raw []byte, name string) bool {
	if len(raw) == 0 || len(raw) > maxAttachmentBytes {
		return false
	}
	sum := sha256.Sum256(raw)
	id := hex.EncodeToString(sum[:12])
	ext := strings.ToLower(filepath.Ext(name))
	dir := c.attachmentsDir()
	dst := filepath.Join(dir, id+ext)
	att := model.Attachment{Name: name, Path: dst, Mime: detectMime(raw, ext), Size: int64(len(raw))}
	c.mu.Lock()
	if slices.Contains(c.draftAttachments, att) {
		c.mu.Unlock()
		return true
	}
	if len(c.draftAttachments) >= maxComposerAttachments || cacheAttachment(dir, dst, raw) != nil {
		c.mu.Unlock()
		return false
	}
	c.draftAttachments = append(c.draftAttachments, att)
	c.saveTabDraftLocked()
	c.persistActiveDraftLocked()
	c.mu.Unlock()
	c.requestRedraw()
	return true
}

func cacheAttachment(dir, dst string, raw []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(dst); err == nil && info.Mode().IsRegular() && info.Size() == int64(len(raw)) {
		return nil
	}
	tmp, err := os.CreateTemp(dir, ".attachment-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(raw)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func (c *Controller) removeDraftAttachment(att model.Attachment) {
	c.mu.Lock()
	if i := slices.Index(c.draftAttachments, att); i >= 0 {
		c.draftAttachments = slices.Delete(c.draftAttachments, i, i+1)
		c.saveTabDraftLocked()
		c.persistActiveDraftLocked()
	}
	c.mu.Unlock()
}

func detectMime(raw []byte, ext string) string {
	mime := http.DetectContentType(raw)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	if mime == "application/octet-stream" && ext == ".pdf" {
		return "application/pdf"
	}
	return mime
}

func attachmentIsText(a model.Attachment) bool {
	return strings.HasPrefix(a.Mime, "text/") || a.Mime == "application/json"
}

// attachmentContext describes attachments for the model: text inline, images
// and other files by their local path so agents with file access can open them.
func attachmentContext(atts []model.Attachment) string {
	var b strings.Builder
	for _, a := range atts {
		if attachmentIsText(a) && a.Size <= maxAttachmentTextBytes {
			if raw, err := os.ReadFile(a.Path); err == nil && utf8.Valid(raw) {
				fmt.Fprintf(&b, "\n\n# 用户附件：%s\n\n```\n%s\n```", a.Name, truncateRunes(string(raw), 12000))
				continue
			}
		}
		kind := "文件"
		if a.IsImage() {
			kind = "图片"
		}
		fmt.Fprintf(&b, "\n\n# 用户附件%s：%s\n\n本地路径：%s（%s，%d 字节）", kind, a.Name, a.Path, a.Mime, a.Size)
	}
	return b.String()
}

// attachmentImageParts returns OpenAI-style image_url parts for image attachments.
func attachmentImageParts(atts []model.Attachment) []map[string]any {
	var parts []map[string]any
	for _, a := range atts {
		if !a.IsImage() || a.Size > maxInlineImageBytes {
			continue
		}
		f, err := os.Open(a.Path)
		if err != nil {
			continue
		}
		var buf bytes.Buffer
		_, err = io.Copy(&buf, io.LimitReader(f, maxInlineImageBytes+1))
		f.Close()
		if err != nil || buf.Len() > maxInlineImageBytes {
			continue
		}
		url := "data:" + a.Mime + ";base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
	}
	return parts
}

// openAIPartsToAnthropic converts OpenAI content parts into Anthropic blocks.
func openAIPartsToAnthropic(parts []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		switch p["type"] {
		case "text":
			out = append(out, map[string]any{"type": "text", "text": p["text"]})
		case "image_url":
			img, _ := p["image_url"].(map[string]any)
			url, _ := img["url"].(string)
			head, data, ok := strings.Cut(strings.TrimPrefix(url, "data:"), ";base64,")
			if !ok {
				continue
			}
			out = append(out, map[string]any{"type": "image", "source": map[string]any{
				"type": "base64", "media_type": head, "data": data,
			}})
		}
	}
	return out
}
