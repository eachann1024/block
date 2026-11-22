package editor

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"block/internal/media"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

var docPNGMagicHeader = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

type docMediaEntry struct {
	blockID  model.BlockID
	blockIdx int
	src      string
	alt      string
	isVideo  bool
}

func (s *Session) collectDocMedia() []docMediaEntry {
	if s == nil || s.doc == nil {
		return nil
	}
	var out []docMediaEntry
	for i := range s.doc.Blocks {
		b := &s.doc.Blocks[i]
		if b.Kind != model.BlockImage {
			continue
		}
		src := strings.TrimSpace(b.ImageSrc)
		if src == "" {
			continue
		}
		out = append(out, docMediaEntry{
			blockID:  b.ID,
			blockIdx: i,
			src:      src,
			alt:      strings.TrimSpace(b.ImageAlt),
			isVideo:  IsSupportedVideoPath(src),
		})
	}
	return out
}

// IsLightboxOpen reports whether the document editor's full-window media lightbox is open.
func (s *Session) IsLightboxOpen() bool {
	return s != nil && s.lightbox.Open
}

func (s *Session) hasFocusedBlockID(id model.BlockID) bool {
	if s == nil || s.doc == nil || s.focusedBlock < 0 || s.focusedBlock >= len(s.doc.Blocks) {
		return false
	}
	return s.doc.Blocks[s.focusedBlock].ID == id
}

// CloseLightbox closes the document media lightbox if open and pauses active playback.
func (s *Session) CloseLightbox() {
	if s == nil || !s.lightbox.Open {
		return
	}
	s.lightbox.Open = false
	s.PauseAllVideos()
}

// OpenMediaLightbox opens the full-window media lightbox focused on the given image/video block.
func (s *Session) OpenMediaLightbox(blockID model.BlockID) {
	if s == nil || s.previewMedia != nil || s.doc == nil {
		return
	}
	entries := s.collectDocMedia()
	if len(entries) == 0 {
		return
	}
	idx := -1
	for i, e := range entries {
		if e.blockID == blockID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	s.closeTransientOverlays()
	entry := entries[idx]
	if entry.isVideo {
		s.activeVideoSrc = entry.src
	}
	s.lightbox.Open = true
	s.lightbox.Index = idx
	s.lightbox.ResetView()
	s.lightbox.AutoFullscreen = entry.isVideo
}

// mediaExpandedState reports whether src is currently displayed in an open lightbox
// (either preview-owned or document-owned) and whether any lightbox is open.
func (s *Session) mediaExpandedState(src string) (thisExpanded bool, anyLightboxOpen bool) {
	if s == nil {
		return false, false
	}
	if s.previewMedia != nil {
		return s.previewExpandedState(src)
	}
	if !s.lightbox.Open {
		return false, false
	}
	entries := s.collectDocMedia()
	if s.lightbox.Index >= 0 && s.lightbox.Index < len(entries) {
		return entries[s.lightbox.Index].src == src, true
	}
	return false, true
}

func (s *Session) copyLocalMedia(svc ui.Services, src string, isVideo bool) bool {
	if s == nil {
		return false
	}
	if !isVideo {
		if path, ok := media.LocalTarget(s.mediaDir(), src); ok {
			if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
				if bytes.HasPrefix(raw, docPNGMagicHeader) {
					svc.WriteClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, raw))))
					return true
				}
				if img, _, err := image.Decode(bytes.NewReader(raw)); err == nil {
					var buf bytes.Buffer
					if err := png.Encode(&buf, img); err == nil {
						svc.WriteClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, buf.Bytes()))))
						return true
					}
				}
			}
		}
	}
	if href := media.WebLink(src); href != "" {
		svc.WriteClipboard(href)
		return true
	}
	if path, ok := media.LocalTarget(s.mediaDir(), src); ok {
		svc.WriteClipboard(path)
		return true
	}
	if src != "" {
		svc.WriteClipboard(src)
		return true
	}
	return false
}

func (s *Session) renderMediaLightbox(c *ui.Context) {
	if s == nil || s.previewMedia != nil || !s.lightbox.Open {
		return
	}
	entries := s.collectDocMedia()
	if len(entries) == 0 {
		s.lightbox.Open = false
		return
	}
	if s.lightbox.Index < 0 {
		s.lightbox.Index = 0
	} else if s.lightbox.Index >= len(entries) {
		s.lightbox.Index = len(entries) - 1
	}

	items := make([]appUi.LightboxItem, len(entries))
	for i, entry := range entries {
		name := entry.alt
		if name == "" {
			name = filepath.Base(entry.src)
		}
		if name == "" {
			if entry.isVideo {
				name = theme.Tr("视频", "Video")
			} else {
				name = theme.Tr("图片", "Image")
			}
		}
		item := appUi.LightboxItem{
			ID:      fmt.Sprintf("doc-media-%d", entry.blockID),
			Name:    name,
			OpenURL: entry.src,
		}
		if entry.isVideo {
			item.IsVideo = true
			if player, ok := s.resolveVideoPlayer(entry.src); ok && player != nil {
				item.Video = player
				vw, vh := player.Size()
				item.ImageW, item.ImageH = vw, vh
				dur := player.Duration()
				switch {
				case vw > 0 && vh > 0 && dur > 0:
					item.Meta = fmt.Sprintf("%d × %d · %s", vw, vh, appUi.FormatMediaDuration(dur))
				case vw > 0 && vh > 0:
					item.Meta = fmt.Sprintf("%d × %d", vw, vh)
				case dur > 0:
					item.Meta = appUi.FormatMediaDuration(dur)
				default:
					item.Meta = theme.Tr("视频", "Video")
				}
			} else {
				item.Unavailable = true
				item.Meta = theme.Tr("视频", "Video")
			}
		} else {
			item.IsImage = true
			imgSrc, hasImage := s.resolveImageSource(entry.src)
			if hasImage && imgSrc != nil {
				item.Image = imgSrc
				sw, sh := ui.ImageSize(imgSrc)
				item.ImageW = int(math.Round(float64(sw)))
				item.ImageH = int(math.Round(float64(sh)))
				if item.ImageW > 0 && item.ImageH > 0 {
					item.Meta = fmt.Sprintf("%d × %d", item.ImageW, item.ImageH)
				}
			} else {
				s.imageMu.Lock()
				item.Loading = s.imageLoading[entry.src]
				item.Unavailable = s.imageFailed[entry.src]
				s.imageMu.Unlock()
			}
		}
		items[i] = item
	}

	prevIdx := s.lightbox.Index
	cb := appUi.LightboxCallbacks{
		OnClose: func() {
			s.PauseAllVideos()
			c.Invalidate()
		},
		OnSelect: func(idx int) {
			if prevIdx != idx {
				s.PauseAllVideos()
			}
			if idx >= 0 && idx < len(entries) && entries[idx].isVideo {
				s.activeVideoSrc = entries[idx].src
			}
			c.Invalidate()
		},
		OnCopy: func(idx int) bool {
			if idx < 0 || idx >= len(entries) {
				return false
			}
			return s.copyLocalMedia(c.Services(), entries[idx].src, entries[idx].isVideo)
		},
		OnOpenURL: func(idx int) {
			if idx < 0 || idx >= len(entries) {
				return
			}
			s.PauseAllVideos()
			s.openBlockLink(c.Services(), entries[idx].src)
		},
	}

	appUi.RenderLightbox(c, "editor-media-lightbox", &s.lightbox, items, cb)
}
