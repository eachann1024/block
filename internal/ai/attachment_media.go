package ai

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"block/internal/media"
	"block/internal/model"
	"block/internal/theme"
	bui "block/internal/ui"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

const (
	maxCachedAttachmentThumbs = 64
	attachmentThumbMaxSide    = 448 // 2x Retina for 224px max single-image thumbnail
	maxConcurrentImageDecodes = 3
)

type cachedAttachmentThumb struct {
	src    *ui.Bitmap
	origW  int
	origH  int
	isFull bool // true when original fit within attachmentThumbMaxSide
	failed bool
}

type attachmentMediaCache struct {
	mu      sync.Mutex
	thumbs  map[string]cachedAttachmentThumb
	order   []string
	loading map[string]bool

	// Single-slot full-resolution cache + single serialized worker for the active Lightbox image.
	fullPath          string
	fullSrc           *ui.Bitmap
	fullW             int
	fullH             int
	fullFailed        bool
	fullWorkerRunning bool
	fullInFlight      string
	fullRequestedPath string
	fullDesired       *model.Attachment
}

type pendingAttachmentClip struct {
	seq      uint64
	pngBytes []byte
	text     string
}

func newAttachmentMediaCache() *attachmentMediaCache {
	return &attachmentMediaCache{
		thumbs:  make(map[string]cachedAttachmentThumb),
		loading: make(map[string]bool),
	}
}

func (c *Controller) ensureMediaCache() *attachmentMediaCache {
	c.mu.Lock()
	if c.mediaCache == nil {
		c.mediaCache = newAttachmentMediaCache()
	}
	mc := c.mediaCache
	c.mu.Unlock()
	return mc
}

// resolveAttachmentImage returns a Retina-sized cached thumbnail bitmap,
// the original EXIF-adjusted dimensions (origW, origH), and loading/failed flags.
// Thumbnails are bounded to <=448px per side so even many 25MP photos never thrash.
func (c *Controller) resolveAttachmentImage(att model.Attachment) (src ui.ImageSource, w, h int, loading, failed bool) {
	if !att.IsImage() || att.Path == "" {
		return nil, 0, 0, false, true
	}
	mc := c.ensureMediaCache()
	key := att.Path

	mc.mu.Lock()
	if e, ok := mc.thumbs[key]; ok {
		mc.touchLocked(key)
		mc.mu.Unlock()
		if e.src == nil {
			return nil, e.origW, e.origH, false, e.failed
		}
		return e.src, e.origW, e.origH, false, e.failed
	}
	if mc.loading[key] {
		mc.mu.Unlock()
		return nil, 0, 0, true, false
	}

	syncDecode := testing.Testing() || bui.IsProbeEnabled()
	if !syncDecode {
		if len(mc.loading) >= maxConcurrentImageDecodes {
			mc.mu.Unlock()
			return nil, 0, 0, true, false
		}
		mc.loading[key] = true
		mc.mu.Unlock()
		dir := c.attachmentsDir()
		go func() {
			entry := decodeCachedAttachmentThumb(dir, att)
			mc.mu.Lock()
			delete(mc.loading, key)
			mc.storeThumbLocked(key, entry)
			mc.mu.Unlock()
			c.requestRedraw()
		}()
		return nil, 0, 0, true, false
	}
	mc.mu.Unlock()

	entry := decodeCachedAttachmentThumb(c.attachmentsDir(), att)
	mc.mu.Lock()
	mc.storeThumbLocked(key, entry)
	mc.mu.Unlock()
	if entry.src == nil {
		return nil, entry.origW, entry.origH, false, entry.failed
	}
	return entry.src, entry.origW, entry.origH, false, entry.failed
}

// resolveAttachmentFullImage returns the full-resolution bitmap for the currently
// inspected Lightbox image. While full-res decodes asynchronously, it immediately
// returns the cached thumbnail (if available) so there is zero blank flash.
func (c *Controller) resolveAttachmentFullImage(att model.Attachment) (src ui.ImageSource, w, h int, loading, failed bool) {
	if !att.IsImage() || att.Path == "" {
		return nil, 0, 0, false, true
	}
	mc := c.ensureMediaCache()
	key := att.Path

	mc.mu.Lock()
	mc.fullRequestedPath = key
	if thumb, ok := mc.thumbs[key]; ok {
		mc.touchLocked(key)
		if thumb.failed {
			mc.fullDesired = nil
			mc.mu.Unlock()
			return nil, 0, 0, false, true
		}
		if thumb.isFull && thumb.src != nil {
			mc.fullDesired = nil
			mc.mu.Unlock()
			return thumb.src, thumb.origW, thumb.origH, false, false
		}
	}
	if mc.fullPath == key {
		mc.fullDesired = nil
		if mc.fullFailed {
			mc.mu.Unlock()
			return nil, 0, 0, false, true
		}
		if mc.fullSrc != nil {
			src, fw, fh := mc.fullSrc, mc.fullW, mc.fullH
			mc.mu.Unlock()
			return src, fw, fh, false, false
		}
	}
	var fallbackThumb *ui.Bitmap
	var fbW, fbH int
	if thumb, ok := mc.thumbs[key]; ok && thumb.src != nil {
		fallbackThumb, fbW, fbH = thumb.src, thumb.origW, thumb.origH
	}
	syncDecode := testing.Testing() || bui.IsProbeEnabled()
	if !syncDecode {
		if mc.fullInFlight == key {
			// Already decoding this exact image; cancel any earlier superseded target.
			mc.fullDesired = nil
		} else {
			cp := att
			mc.fullDesired = &cp
		}
		if !mc.fullWorkerRunning {
			mc.fullWorkerRunning = true
			dir := c.attachmentsDir()
			mc.mu.Unlock()
			go func() {
				for {
					mc.mu.Lock()
					target := mc.fullDesired
					mc.fullDesired = nil
					if target == nil {
						mc.fullInFlight = ""
						mc.fullWorkerRunning = false
						mc.mu.Unlock()
						return
					}
					if mc.fullPath == target.Path && (mc.fullSrc != nil || mc.fullFailed) {
						mc.fullInFlight = ""
						mc.mu.Unlock()
						continue
					}
					mc.fullInFlight = target.Path
					mc.mu.Unlock()

					bm, dw, dh, err := decodeCachedAttachmentFull(dir, *target)

					mc.mu.Lock()
					mc.fullInFlight = ""
					// Only publish if this image is still the active requested target.
					if mc.fullRequestedPath == target.Path {
						if mc.fullDesired != nil && mc.fullDesired.Path == target.Path {
							mc.fullDesired = nil
						}
						mc.setFullLocked(target.Path, bm, dw, dh, err != nil || bm == nil)
					}
					mc.mu.Unlock()
					c.requestRedraw()
				}
			}()
		} else {
			mc.mu.Unlock()
		}
		if fallbackThumb != nil {
			return fallbackThumb, fbW, fbH, false, false
		}
		return nil, 0, 0, true, false
	}
	mc.mu.Unlock()

	bm, dw, dh, err := decodeCachedAttachmentFull(c.attachmentsDir(), att)
	mc.mu.Lock()
	mc.setFullLocked(key, bm, dw, dh, err != nil || bm == nil)
	mc.mu.Unlock()
	if err != nil || bm == nil {
		return nil, 0, 0, false, true
	}
	return bm, dw, dh, false, false
}

func (mc *attachmentMediaCache) setFullLocked(key string, bm *ui.Bitmap, w, h int, failed bool) {
	// Drop reference without calling ReleasePixels off the UI thread; active frames
	// may still hold the previous bitmap until the current paint pass completes.
	mc.fullPath = key
	mc.fullSrc = bm
	mc.fullW = w
	mc.fullH = h
	mc.fullFailed = failed
}

func (mc *attachmentMediaCache) touchLocked(key string) {
	if i := slices.Index(mc.order, key); i >= 0 && i < len(mc.order)-1 {
		mc.order = append(slices.Delete(mc.order, i, i+1), key)
	}
}

func (mc *attachmentMediaCache) storeThumbLocked(key string, entry cachedAttachmentThumb) {
	if _, ok := mc.thumbs[key]; ok {
		if i := slices.Index(mc.order, key); i >= 0 {
			mc.order = slices.Delete(mc.order, i, i+1)
		}
	}
	for len(mc.order) >= maxCachedAttachmentThumbs {
		evict := mc.order[0]
		mc.order = mc.order[1:]
		delete(mc.thumbs, evict)
	}
	mc.thumbs[key] = entry
	mc.order = append(mc.order, key)
}

func openAttachmentInDir(dir string, att model.Attachment) (*os.Root, string, int64, error) {
	name, err := filepath.Rel(dir, att.Path)
	if err != nil || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return nil, "", 0, attachmentUnreadable()
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", 0, err
	}
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		root.Close()
		return nil, "", 0, attachmentUnreadable()
	}
	return root, name, info.Size(), nil
}

func decodeCachedAttachmentFull(dir string, att model.Attachment) (*ui.Bitmap, int, int, error) {
	root, name, _, err := openAttachmentInDir(dir, att)
	if err != nil {
		return nil, 0, 0, err
	}
	defer root.Close()
	raw, err := readCachedAttachment(root, name)
	if err != nil {
		return nil, 0, 0, err
	}
	if _, _, err := media.ImageConfig(raw); err != nil {
		return nil, 0, 0, err
	}
	bm, err := ui.DecodeBitmap(raw)
	if err != nil {
		return nil, 0, 0, err
	}
	iw, ih := bm.Size()
	return bm, iw, ih, nil
}

func decodeCachedAttachmentThumb(dir string, att model.Attachment) cachedAttachmentThumb {
	root, name, _, err := openAttachmentInDir(dir, att)
	if err != nil {
		return cachedAttachmentThumb{failed: true}
	}
	defer root.Close()
	raw, err := readCachedAttachment(root, name)
	if err != nil {
		return cachedAttachmentThumb{failed: true}
	}
	cfg, format, err := media.ImageConfig(raw)
	if err != nil {
		return cachedAttachmentThumb{failed: true}
	}
	if cfg.Width <= attachmentThumbMaxSide && cfg.Height <= attachmentThumbMaxSide {
		bm, decErr := ui.DecodeBitmap(raw)
		if decErr != nil {
			return cachedAttachmentThumb{failed: true}
		}
		iw, ih := bm.Size()
		return cachedAttachmentThumb{src: bm, origW: iw, origH: ih, isFull: true}
	}

	img, _, _, decErr := media.DecodeRaster(raw)
	if decErr != nil {
		return cachedAttachmentThumb{failed: true}
	}
	b := img.Bounds()
	origW, origH := b.Dx(), b.Dy()
	tw, th := scaledThumbDims(origW, origH, attachmentThumbMaxSide)
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)

	if format == "jpeg" {
		if orient := jpegExifOrientation(raw); orient > 1 && orient <= 8 {
			dst = applyExifOrientation(dst, orient)
			if orient >= 5 {
				origW, origH = origH, origW
			}
		}
	}
	return cachedAttachmentThumb{
		src:   ui.NewBitmap(dst),
		origW: origW,
		origH: origH,
	}
}

func scaledThumbDims(w, h, maxSide int) (int, int) {
	if w <= 0 || h <= 0 {
		return maxSide, maxSide
	}
	if w <= maxSide && h <= maxSide {
		return w, h
	}
	if w >= h {
		th := max(h*maxSide/w, 1)
		return maxSide, th
	}
	tw := max(w*maxSide/h, 1)
	return tw, maxSide
}

// jpegExifOrientation reads the EXIF Orientation tag (0x0112) from a JPEG APP1 segment.
func jpegExifOrientation(raw []byte) int {
	if len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(raw) {
		if raw[i] != 0xFF {
			return 1
		}
		for i < len(raw) && raw[i] == 0xFF {
			i++
		}
		if i >= len(raw) {
			return 1
		}
		marker := raw[i]
		i++
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		if i+2 > len(raw) {
			return 1
		}
		segLen := int(binary.BigEndian.Uint16(raw[i : i+2]))
		if segLen < 2 || i+segLen > len(raw) {
			return 1
		}
		if marker == 0xE1 && segLen >= 16 && bytes.Equal(raw[i+2:i+8], []byte("Exif\x00\x00")) {
			tiff := raw[i+8 : i+segLen]
			return parseTiffOrientation(tiff)
		}
		i += segLen
	}
	return 1
}

func parseTiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		bo = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return 1
	}
	ifdOff := int(bo.Uint32(tiff[4:8]))
	if ifdOff < 8 || ifdOff+2 > len(tiff) {
		return 1
	}
	count := int(bo.Uint16(tiff[ifdOff : ifdOff+2]))
	for k := 0; k < count; k++ {
		off := ifdOff + 2 + k*12
		if off+12 > len(tiff) {
			break
		}
		if bo.Uint16(tiff[off:off+2]) == 0x0112 {
			return int(bo.Uint16(tiff[off+8 : off+10]))
		}
	}
	return 1
}

func applyExifOrientation(src *image.RGBA, orient int) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if orient >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orient {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			default:
				dx, dy = x, y
			}
			so := src.PixOffset(x, y)
			do := dst.PixOffset(dx, dy)
			copy(dst.Pix[do:do+4], src.Pix[so:so+4])
		}
	}
	return dst
}

// copyAttachmentToClipboard prepares an image (as upright PNG) or full UTF-8 text
// attachment for the system clipboard. Expensive image transcoding runs off the UI
// thread and is flushed on the next UI frame so large JPEGs never stall the interface.
func (c *Controller) copyAttachmentToClipboard(svc ui.Services, att model.Attachment) bool {
	if !att.IsImage() && !attachmentIsText(att) {
		return false
	}
	dir := c.attachmentsDir()
	// Quick existence/confinement check so the UI gets immediate feedback if unreadable.
	root, name, _, err := openAttachmentInDir(dir, att)
	if err != nil {
		return false
	}
	root.Close()
	_ = name

	if testing.Testing() || bui.IsProbeEnabled() {
		clip, ok := buildAttachmentClipPayload(dir, att)
		if !ok {
			return false
		}
		applyAttachmentClip(svc, clip)
		return true
	}

	c.mu.Lock()
	c.clipSeq++
	seq := c.clipSeq
	c.mu.Unlock()

	go func() {
		clip, ok := buildAttachmentClipPayload(dir, att)
		if !ok {
			return
		}
		clip.seq = seq
		c.mu.Lock()
		if c.clipSeq != seq || !c.open || !c.providerCfg.Enabled {
			c.mu.Unlock()
			return
		}
		c.pendingClip = &clip
		c.mu.Unlock()
		c.requestRedraw()
	}()
	return true
}

func (c *Controller) cancelPendingClipboardLocked() {
	c.clipSeq++
	c.pendingClip = nil
}

func (c *Controller) flushPendingClipboard(svc ui.Services) {
	c.mu.Lock()
	clip := c.pendingClip
	if clip != nil && clip.seq != c.clipSeq {
		clip = nil
	}
	c.pendingClip = nil
	c.mu.Unlock()
	if clip != nil {
		applyAttachmentClip(svc, *clip)
	}
}

func applyAttachmentClip(svc ui.Services, clip pendingAttachmentClip) {
	if len(clip.pngBytes) > 0 {
		svc.WriteClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, clip.pngBytes))))
	} else if clip.text != "" {
		svc.WriteClipboard(clip.text)
	}
}

func buildAttachmentClipPayload(dir string, att model.Attachment) (pendingAttachmentClip, bool) {
	root, name, _, err := openAttachmentInDir(dir, att)
	if err != nil {
		return pendingAttachmentClip{}, false
	}
	defer root.Close()
	raw, err := readCachedAttachment(root, name)
	if err != nil || len(raw) == 0 {
		return pendingAttachmentClip{}, false
	}
	if att.IsImage() {
		pngBytes := raw
		if att.Mime != "image/png" {
			img, _, format, decErr := media.DecodeRaster(raw)
			if decErr != nil {
				return pendingAttachmentClip{}, false
			}
			if format == "jpeg" {
				if orient := jpegExifOrientation(raw); orient > 1 && orient <= 8 {
					b := img.Bounds()
					rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
					xdraw.Copy(rgba, image.Point{}, img, b, xdraw.Src, nil)
					img = applyExifOrientation(rgba, orient)
				}
			}
			var buf bytes.Buffer
			if encErr := png.Encode(&buf, img); encErr != nil {
				return pendingAttachmentClip{}, false
			}
			pngBytes = buf.Bytes()
		}
		return pendingAttachmentClip{pngBytes: pngBytes}, true
	}
	if attachmentIsText(att) && utf8.Valid(raw) {
		return pendingAttachmentClip{text: string(raw)}, true
	}
	return pendingAttachmentClip{}, false
}

// readAttachmentPreviewText loads UTF-8 text preview for a non-image attachment.
func (c *Controller) readAttachmentPreviewText(att model.Attachment) (string, bool, bool) {
	if !attachmentIsText(att) {
		return "", false, false
	}
	root, name, _, err := openAttachmentInDir(c.attachmentsDir(), att)
	if err != nil {
		return "", false, false
	}
	defer root.Close()
	raw, err := readCachedAttachment(root, name)
	if err != nil || !utf8.Valid(raw) {
		return "", false, false
	}
	return string(raw), false, true
}

func formatAttachmentSize(size int64) string {
	switch {
	case size <= 0:
		return ""
	case size < 1024:
		return fmt.Sprintf("%d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
}

func attachmentMetaString(att model.Attachment, w, h int) string {
	var parts []string
	if att.IsImage() && w > 0 && h > 0 {
		parts = append(parts, fmt.Sprintf("%d × %d", w, h))
	} else if att.Mime != "" && att.Mime != "application/octet-stream" {
		parts = append(parts, att.Mime)
	}
	if sz := formatAttachmentSize(att.Size); sz != "" {
		parts = append(parts, sz)
	}
	return strings.Join(parts, " · ")
}

// singleImageThumbSize computes the display size of a single user-message image
// preserving aspect ratio within Ocean Paper bounds.
func singleImageThumbSize(iw, ih int) (float32, float32) {
	maxW := theme.AIAttachSingleMaxW
	maxH := theme.AIAttachSingleMaxH
	minS := theme.AIAttachSingleMinSide
	if iw <= 0 || ih <= 0 {
		return theme.AIAttachGridThumb * 2, theme.AIAttachGridThumb * 1.4
	}
	fw, fh := float32(iw), float32(ih)
	scale := min(maxW/fw, maxH/fh)
	if scale > 1 {
		scale = 1
	}
	w := fw * scale
	h := fh * scale
	if w < minS && h < minS {
		up := min(minS/w, minS/h)
		w *= up
		h *= up
	}
	w = min(max(w, minS), maxW)
	h = min(max(h, minS), maxH)
	return w, h
}

// displayUserMessageText returns the clean text to display in a user bubble.
// New messages (separate == true) never strip user-typed text; legacy persisted
// messages strip the historical "[filename] " prefix that used to be injected.
func displayUserMessageText(text string, atts []model.Attachment, separate bool) string {
	if separate || len(atts) == 0 {
		return text
	}
	return stripAttachmentPrefixes(text, atts)
}

// stripAttachmentPrefixes removes legacy "[filename] " tokens matching atts
// from user message text or a persisted conversation title, even when preceded by
// "@ref " markers.
func stripAttachmentPrefixes(text string, atts []model.Attachment) string {
	if text == "" || len(atts) == 0 {
		return text
	}
	s := text
	for _, a := range atts {
		if a.Name == "" {
			continue
		}
		tok := "[" + a.Name + "]"
		trimmed := strings.TrimLeft(s, " ")
		if rest, ok := strings.CutPrefix(trimmed, tok); ok {
			s = strings.TrimLeft(rest, " ")
			continue
		}
		if idx := strings.Index(s, " "+tok+" "); idx >= 0 {
			s = s[:idx] + " " + s[idx+len(" "+tok+" "):]
			continue
		}
		if strings.HasSuffix(s, " "+tok) {
			s = strings.TrimSuffix(s, " "+tok)
		}
	}
	return strings.TrimSpace(s)
}

// conversationTitleFromMessage builds a clean title for a conversation
// without raw "[image.png]" prefixes.
func conversationTitleFromMessage(msg model.ChatMessage) string {
	clean := displayUserMessageText(msg.Content, msg.Attachments, msg.AttachmentsSeparate)
	if t := firstTitleLine(clean); t != "" && !isLegacyAttachmentFallback(t) {
		return t
	}
	if len(msg.Attachments) > 0 {
		imgCount := 0
		for _, a := range msg.Attachments {
			if a.IsImage() {
				imgCount++
			}
		}
		if imgCount == len(msg.Attachments) {
			if imgCount == 1 {
				name := strings.TrimSpace(msg.Attachments[0].Name)
				if name != "" && !isGenericImageName(name) {
					return truncateRunes(name, 24)
				}
				return theme.Tr("图片对话", "Image chat")
			}
			return fmt.Sprintf(theme.Tr("%d 张图片", "%d images"), imgCount)
		}
		if len(msg.Attachments) == 1 && msg.Attachments[0].Name != "" {
			return truncateRunes(msg.Attachments[0].Name, 24)
		}
		return fmt.Sprintf(theme.Tr("%d 个附件", "%d attachments"), len(msg.Attachments))
	}
	return theme.Tr("新会话", "New Chat")
}

func isGenericImageName(name string) bool {
	base := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	return base == "image" || strings.HasPrefix(base, "image-")
}

func isLegacyAttachmentFallback(s string) bool {
	t := strings.TrimSpace(s)
	return t == "请查看附件。" || t == "Please look at the attachments."
}

// displayConversationTitle cleans up persisted titles that may contain legacy
// "[image.png] " prefixes (including titles truncated mid-filename at 24 runes),
// re-deriving the full 24-rune clean title from the first user message.
func displayConversationTitle(conv model.Conversation) string {
	title := strings.TrimSpace(conv.Title)
	var firstUser *model.ChatMessage
	for i := range conv.Messages {
		if conv.Messages[i].Role == "user" {
			firstUser = &conv.Messages[i]
			break
		}
	}
	if firstUser != nil && len(firstUser.Attachments) > 0 && !firstUser.AttachmentsSeparate {
		legacyAuto := firstTitleLine(firstUser.Content)
		firstTok := "[" + firstUser.Attachments[0].Name + "]"
		truncatedInsideTok := strings.HasPrefix(title, "[") && strings.HasPrefix(firstTok, title)
		cleaned := strings.TrimSpace(stripAttachmentPrefixes(title, firstUser.Attachments))
		if title == legacyAuto || truncatedInsideTok || cleaned != title || cleaned == "" || isLegacyAttachmentFallback(cleaned) {
			return conversationTitleFromMessage(*firstUser)
		}
	}
	if title == "" {
		return theme.Tr("新会话", "New Chat")
	}
	return title
}

// userMessageModelText formats a stored user message for LLM wire requests so
// attachment-only turns and historical turns retain clear context for the model
// while the UI displays clean text + native thumbnails.
func userMessageModelText(m model.ChatMessage, includeCurrentContext bool) string {
	text := m.Content
	if strings.TrimSpace(text) == "" && len(m.Attachments) > 0 {
		text = theme.Tr("请查看附件。", "Please look at the attachments.")
	}
	if !includeCurrentContext && len(m.Attachments) > 0 {
		var missing []string
		for _, a := range m.Attachments {
			if a.Name != "" && !strings.Contains(text, "["+a.Name+"]") {
				missing = append(missing, "["+a.Name+"]")
			}
		}
		if len(missing) > 0 {
			text = strings.Join(missing, " ") + " " + text
		}
	}
	return text
}
