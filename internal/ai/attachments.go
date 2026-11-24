package ai

import (
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
	maxAttachmentTextRunes = 12000
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
			name := c.uniqueDraftAttachmentName("image", "."+f.ext)
			if !c.addAttachmentBytes(raw, name) {
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

func (c *Controller) uniqueDraftAttachmentName(base, ext string) string {
	c.mu.RLock()
	existing := make(map[string]bool, len(c.draftAttachments))
	for _, a := range c.draftAttachments {
		existing[a.Name] = true
	}
	c.mu.RUnlock()
	cand := base + ext
	if !existing[cand] {
		return cand
	}
	for n := 2; n <= maxComposerAttachments+2; n++ {
		cand = fmt.Sprintf("%s-%d%s", base, n, ext)
		if !existing[cand] {
			return cand
		}
	}
	return base + ext
}

// AddAttachmentFiles imports local files into the active composer draft and
// reports how many files were accepted.
func (c *Controller) AddAttachmentFiles(paths []string) int {
	if len(paths) == 0 {
		return 0
	}
	added := c.addAttachmentFiles(paths)
	if added < len(paths) {
		c.showAttachmentPasteError()
	}
	if added > 0 {
		c.mu.Lock()
		c.composerHandle.Focus()
		c.mu.Unlock()
		c.requestRedraw()
	}
	return added
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
	if slices.ContainsFunc(c.draftAttachments, func(existing model.Attachment) bool {
		return existing.Path == att.Path
	}) {
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

func (c *Controller) prepareAttachments(atts []model.Attachment, builtin, nativeTools bool) (string, []map[string]any, error) {
	if len(atts) == 0 {
		return "", nil, nil
	}
	dir := c.attachmentsDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", nil, attachmentUnreadable()
	}
	defer root.Close()
	var b strings.Builder
	var images []map[string]any
	imageBudget := int64(maxInlineImageBytes)
	imageIdx := 0
	for _, a := range atts {
		name, err := filepath.Rel(dir, a.Path)
		if err != nil || !filepath.IsLocal(name) || filepath.Base(name) != name {
			return "", nil, fmt.Errorf("%q: %w", a.Name, attachmentUnreadable())
		}
		info, err := root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Size() != a.Size || a.Size <= 0 || a.Size > maxAttachmentBytes {
			return "", nil, fmt.Errorf("%q: %w", a.Name, attachmentUnreadable())
		}
		if builtin && a.IsImage() {
			if a.Size > imageBudget {
				return "", nil, fmt.Errorf("%q: %s", a.Name, theme.Tr("每条消息最多发送 8 MB 图片，请缩小图片或减少张数。", "Images can total up to 8 MB per message. Resize or remove an image."))
			}
			raw, err := readCachedAttachment(root, name, a.Size)
			if err != nil || int64(len(raw)) != a.Size {
				return "", nil, fmt.Errorf("%q: %w", a.Name, attachmentUnreadable())
			}
			imageBudget -= a.Size
			imageIdx++
			url := "data:" + a.Mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
			images = append(images,
				map[string]any{"type": "text", "text": fmt.Sprintf("[Attached image %d: %s]", imageIdx, a.Name)},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}},
			)
			continue
		}
		if attachmentIsText(a) {
			limit := min(a.Size, maxAttachmentTextRunes*utf8.UTFMax)
			raw, err := readCachedAttachment(root, name, limit)
			if err != nil || int64(len(raw)) != limit {
				return "", nil, fmt.Errorf("%q: %w", a.Name, attachmentUnreadable())
			}
			if prefix, truncated, ok := attachmentText(raw, a.Size); ok {
				fmt.Fprintf(&b, theme.Tr("\n\n# 用户附件：%q\n\n    %s", "\n\n# User attachment: %q\n\n    %s"), a.Name, strings.ReplaceAll(prefix, "\n", "\n    "))
				if truncated {
					b.WriteString(theme.Tr("\n\n（附件内容已截断。）", "\n\n(Attachment content was cut off.)"))
					if nativeTools {
						fmt.Fprintf(&b, theme.Tr("\n完整文件路径：%q", "\nFull file path: %q"), a.Path)
					}
				}
				continue
			}
		}
		if !nativeTools {
			return "", nil, fmt.Errorf("%q: %s", a.Name, theme.Tr("当前 AI 无法读取此附件，请移除附件或切换 Agent。", "This AI cannot read this attachment. Remove it or switch agents."))
		}
		fmt.Fprintf(&b, theme.Tr("\n\n# 用户附件：%q\n\n本地路径：%q（%s，%d 字节）", "\n\n# User attachment: %q\n\nLocal path: %q (%s, %d bytes)"), a.Name, a.Path, a.Mime, a.Size)
	}
	return b.String(), images, nil
}

func attachmentUnreadable() error {
	return errors.New(theme.Tr("附件无法读取，请移除后重新粘贴。", "The attachment could not be read. Remove and paste it again."))
}

func readCachedAttachment(root *os.Root, name string, limit int64) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, attachmentUnreadable()
	}
	return io.ReadAll(io.LimitReader(f, limit))
}

func attachmentText(raw []byte, size int64) (string, bool, bool) {
	end := 0
	for n := 0; n < maxAttachmentTextRunes && end < len(raw); n++ {
		r, width := utf8.DecodeRune(raw[end:])
		if r == utf8.RuneError && width == 1 {
			return "", false, false
		}
		end += width
	}
	return string(raw[:end]), int64(end) < size, true
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
