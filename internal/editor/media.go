package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"
)

// IsSupportedVideoPath reports whether path has a video file extension Block can embed and play.
func IsSupportedVideoPath(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".mp4", ".mov", ".m4v", ".webm", ".mkv", ".avi", ".ogv":
		return true
	default:
		return false
	}
}

func uniqueTypedAssetFilename(assetsDir, prefix, ext string) string {
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	for {
		base := uniqueAssetFilename(assetsDir, ext)
		if prefix != "" && prefix != "img" {
			base = prefix + strings.TrimPrefix(base, "img")
		}
		if ext == "" {
			base = strings.TrimSuffix(base, ".png")
		}
		if _, err := os.Stat(filepath.Join(assetsDir, base)); os.IsNotExist(err) {
			return base
		}
	}
}

func (s *Session) resolveAssetsDir() (string, bool) {
	baseDir := ""
	if s.notePath != "" {
		baseDir = filepath.Dir(s.notePath)
	} else if s.dataDir != "" {
		baseDir = filepath.Join(s.dataDir, "quick")
	}
	if baseDir == "" {
		return "", false
	}
	assetsDir := filepath.Join(baseDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return "", false
	}
	return assetsDir, true
}

// ImportFiles copies local image, video, and arbitrary attachment files into `<noteDir>/assets/`
// and inserts corresponding image, video, or linked attachment blocks into the document.
func (s *Session) ImportFiles(paths []string, targetID model.BlockID, replace bool) int {
	if s.doc == nil || s.readOnly || len(paths) == 0 {
		return 0
	}
	assetsDir, ok := s.resolveAssetsDir()
	if !ok {
		return 0
	}

	var inserted []model.Block
	for _, rawPath := range paths {
		p := strings.TrimSpace(rawPath)
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
		baseName := filepath.Base(p)
		cleanName := strings.NewReplacer("[", "", "]", "").Replace(baseName)
		stem := strings.TrimSuffix(cleanName, filepath.Ext(cleanName))
		if stem == "" {
			stem = cleanName
		}

		switch {
		case IsSupportedImagePath(p):
			name := uniqueTypedAssetFilename(assetsDir, "img", ext)
			if err := copyAssetFile(p, filepath.Join(assetsDir, name)); err != nil {
				continue
			}
			inserted = append(inserted, model.Block{
				ID:         model.NewBlockID(),
				Kind:       model.BlockImage,
				ImageSrc:   "./assets/" + name,
				ImageAlt:   stem,
				ImageAlign: model.ImageAlignLeft,
				ImageWidth: 100,
				Dirty:      true,
			})

		case IsSupportedVideoPath(p):
			name := uniqueTypedAssetFilename(assetsDir, "vid", ext)
			if err := copyAssetFile(p, filepath.Join(assetsDir, name)); err != nil {
				continue
			}
			inserted = append(inserted, model.Block{
				ID:         model.NewBlockID(),
				Kind:       model.BlockImage,
				ImageSrc:   "./assets/" + name,
				ImageAlt:   cleanName,
				ImageAlign: model.ImageAlignLeft,
				ImageWidth: 100,
				Dirty:      true,
			})

		default:
			name := uniqueTypedAssetFilename(assetsDir, "file", ext)
			if err := copyAssetFile(p, filepath.Join(assetsDir, name)); err != nil {
				continue
			}
			inserted = append(inserted, model.Block{
				ID:    model.NewBlockID(),
				Kind:  model.BlockParagraph,
				Dirty: true,
				Spans: []model.Span{
					{Text: cleanName, Link: "./assets/" + name},
				},
			})
		}
	}

	if len(inserted) == 0 {
		return 0
	}
	return s.insertImportedBlocks(inserted, targetID, replace)
}

// ImportClipboardImageBytes writes raw image bytes from the clipboard into `<noteDir>/assets/`
// and inserts an image block into the document.
func (s *Session) ImportClipboardImageBytes(raw []byte, ext string, targetID model.BlockID, replace bool) bool {
	if s.doc == nil || s.readOnly || len(raw) == 0 {
		return false
	}
	assetsDir, ok := s.resolveAssetsDir()
	if !ok {
		return false
	}
	if ext == "" {
		ext = "png"
	}
	name := uniqueTypedAssetFilename(assetsDir, "img", ext)
	dst := filepath.Join(assetsDir, name)
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return false
	}
	blk := model.Block{
		ID:         model.NewBlockID(),
		Kind:       model.BlockImage,
		ImageSrc:   "./assets/" + name,
		ImageAlt:   "image",
		ImageAlign: model.ImageAlignLeft,
		ImageWidth: 100,
		Dirty:      true,
	}
	return s.insertImportedBlocks([]model.Block{blk}, targetID, replace) > 0
}

// RequestPickImages triggers the host image file picker for inserting or replacing blockID.
func (s *Session) RequestPickImages(blockID model.BlockID, replace bool) {
	if s.readOnly || s.OnPickImages == nil {
		return
	}
	s.OnPickImages(blockID, replace)
}

func (s *Session) invalidateImageCache(src string) {
	if src == "" {
		return
	}
	delete(s.imageCache, src)
	delete(s.imageFailed, src)
}

func (s *Session) insertImportedBlocks(inserted []model.Block, targetID model.BlockID, replace bool) int {
	if len(inserted) == 0 || s.doc == nil || len(s.doc.Blocks) == 0 {
		return 0
	}
	count := len(inserted)
	idx := s.blockIndexByID(targetID)
	if idx < 0 {
		idx = clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
		cur := &s.doc.Blocks[idx]
		if (cur.Kind == model.BlockParagraph && cur.PlainText() == "") ||
			(cur.Kind == model.BlockImage && cur.ImageSrc == "") {
			replace = true
		} else {
			replace = false
		}
	}

	if replace && s.doc.Blocks[idx].Kind == model.BlockImage && inserted[0].Kind == model.BlockImage {
		orig := s.doc.Blocks[idx]
		s.pushUndo()
		s.invalidateImageCache(orig.ImageSrc)
		s.invalidateImageCache(inserted[0].ImageSrc)

		updated := orig
		updated.ImageSrc = inserted[0].ImageSrc
		updated.ImageAlt = inserted[0].ImageAlt
		updated.ImageCrop = model.ImageCrop{}
		if updated.ImageWidth <= 0 {
			updated.ImageWidth = inserted[0].ImageWidth
		}
		updated.Dirty = true
		updated.CleanSource = ""
		s.doc.Blocks[idx] = updated

		if len(inserted) > 1 {
			extra := make([]model.Block, len(inserted)-1)
			copy(extra, inserted[1:])
			for i := range extra {
				extra[i].Indent = orig.Indent
				if extra[i].Kind == model.BlockImage {
					extra[i].ImageAlign = orig.ImageAlign
				}
				s.invalidateImageCache(extra[i].ImageSrc)
			}
			s.spliceBlocks(idx+1, idx+1, extra)
		}

		lastImgIdx := idx + len(inserted) - 1
		if lastImgIdx >= len(s.doc.Blocks)-1 {
			s.spliceBlocks(len(s.doc.Blocks), len(s.doc.Blocks), []model.Block{{
				ID:     model.NewBlockID(),
				Kind:   model.BlockParagraph,
				Indent: orig.Indent,
				Dirty:  true,
			}})
		}

		s.imagePopKind = ""
		s.FocusBlock(clampInt(lastImgIdx, 0, len(s.doc.Blocks)-1))
		s.focusPreventScroll = true
		s.markDirty()
		s.requestRedraw()
		return count
	}

	inserted = append(inserted, model.Block{
		ID:    model.NewBlockID(),
		Kind:  model.BlockParagraph,
		Dirty: true,
	})

	indent := s.doc.Blocks[idx].Indent
	for i := range inserted {
		inserted[i].Indent = indent
	}

	s.pushUndo()
	start := idx + 1
	end := idx + 1
	if replace {
		start = idx
		end = idx + 1
	}
	s.spliceBlocks(start, end, inserted)
	trailIdx := clampInt(start+len(inserted)-1, 0, len(s.doc.Blocks)-1)
	s.FocusBlock(trailIdx)
	s.markDirty()
	s.requestRedraw()
	return count
}

// PasteFromClipboard inspects clipboard file lists, image bitmaps, URL-over-selection,
// and semantic HTML / Markdown before falling back to plain text insertion.
// Returns true when handled by Session (preventing default plain insert).
func (s *Session) PasteFromClipboard(svc ui.Services, fallbackText string, inlineInput bool) bool {
	return s.pasteFromClipboard(svc, fallbackText, inlineInput, false)
}

// PastePlainFromClipboard handles pasteAndMatchStyle (Cmd/Ctrl+Shift+V) as unformatted plain text.
func (s *Session) PastePlainFromClipboard(svc ui.Services, fallbackText string, inlineInput bool) bool {
	return s.pasteFromClipboard(svc, fallbackText, inlineInput, true)
}

func (s *Session) pasteFromClipboard(svc ui.Services, fallbackText string, inlineInput bool, plainOnly bool) bool {
	if s.readOnly {
		return true
	}

	if plainOnly {
		if fallbackText == "" {
			return false
		}
		s.PastePlainText(fallbackText)
		return true
	}

	// 1. Local file list (images, videos, PDFs, archives, documents copied from Finder / file manager).
	// Checked BEFORE plain text because macOS Finder also places the bare filename into text/plain
	// and the file's icon into image/png!
	fileData := svc.ReadClipboardData(transfer.FileList, transfer.URIList)
	if len(fileData.Items()) > 0 {
		if paths, err := fileData.Files(); err == nil && len(paths) > 0 {
			if s.ImportFiles(paths, 0, false) > 0 {
				return true
			}
		}
	}

	// 2. Direct clipboard bitmap (screenshots, copied web/chat images) when no plain text is on clipboard.
	if fallbackText == "" {
		imgFormats := []struct {
			fmt transfer.Format
			ext string
		}{
			{transfer.PNG, "png"},
			{"image/jpeg", "jpg"},
			{"image/webp", "webp"},
			{"image/gif", "gif"},
		}
		imgData := svc.ReadClipboardData(transfer.PNG, "image/jpeg", "image/webp", "image/gif")
		if len(imgData.Items()) > 0 {
			for _, candidate := range imgFormats {
				if raw, err := imgData.Read(candidate.fmt); err == nil && len(raw) > 0 {
					if s.ImportClipboardImageBytes(raw, candidate.ext, 0, false) {
						return true
					}
				}
			}
		}
	}

	// 3. Smart URL paste over selected text (Notion / Feishu behavior).
	if s.tryPasteURLOverSelection(fallbackText) {
		return true
	}

	// 4. Smart dual-channel clipboard: explicit Markdown format or semantic HTML from web / Feishu / Notion / Word.
	richData := svc.ReadClipboardData(transfer.Markdown, transfer.HTML)
	if len(richData.Items()) > 0 {
		if mdRaw, err := richData.Read(transfer.Markdown); err == nil && strings.TrimSpace(string(mdRaw)) != "" {
			s.PasteText(string(mdRaw))
			return true
		}
		if htmlRaw, err := richData.Read(transfer.HTML); err == nil && len(htmlRaw) > 0 && s.codec != nil {
			if converted, ok := s.codec.ConvertHTMLPaste(string(htmlRaw), fallbackText); ok {
				s.PasteText(converted)
				return true
			}
		}
	}

	if fallbackText == "" {
		return false
	}

	// 5. Text / Markdown paste.
	if inlineInput {
		normProbe := fallbackText
		if s.codec != nil {
			normProbe = s.codec.NormalizePaste(fallbackText)
		}
		if normProbe != fallbackText || strings.ContainsAny(fallbackText, "\r\n\t") ||
			looksLikeMarkdownBlock(normProbe) || looksLikeInlineMarkdown(normProbe) {
			s.PasteText(fallbackText)
			return true
		}
		return false
	}
	s.PasteText(fallbackText)
	return true
}

func localFileURL(absPath string) string {
	p := filepath.ToSlash(absPath)
	if len(p) > 0 && p[0] != '/' {
		p = "/" + p
	}
	const hex = "0123456789ABCDEF"
	b := make([]byte, 0, len(p)+16)
	b = append(b, "file://"...)
	for i := 0; i < len(p); i++ {
		ch := p[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == '/', ch == '-', ch == '_', ch == '.', ch == '~', ch == ':':
			b = append(b, ch)
		default:
			b = append(b, '%', hex[ch>>4], hex[ch&15])
		}
	}
	return string(b)
}

func (s *Session) resolveMediaPath(src string) string {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return trimmed
	}
	if s.notePath != "" {
		return filepath.Join(filepath.Dir(s.notePath), filepath.FromSlash(trimmed))
	}
	if s.dataDir != "" {
		return filepath.Join(s.dataDir, "quick", filepath.FromSlash(trimmed))
	}
	return trimmed
}

func (s *Session) openBlockLink(svc ui.Services, rawLink string) {
	link := strings.TrimSpace(rawLink)
	if link == "" {
		return
	}
	lower := strings.ToLower(link)
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "mailto:") {
		svc.OpenURL(link)
		return
	}
	resolved := s.resolveMediaPath(link)
	if abs, err := filepath.Abs(resolved); err == nil {
		svc.OpenURL(localFileURL(abs))
	}
}

func (s *Session) closeVideoPlayers() {
	for k, p := range s.videoPlayers {
		if p != nil {
			p.Close()
		}
		delete(s.videoPlayers, k)
	}
	clear(s.videoFailed)
}

func (s *Session) resolveVideoPlayer(src string) (*ui.VideoPlayer, bool) {
	if src == "" {
		return nil, false
	}
	if s.videoPlayers == nil {
		s.videoPlayers = make(map[string]*ui.VideoPlayer)
	}
	if s.videoFailed == nil {
		s.videoFailed = make(map[string]bool)
	}
	if p, ok := s.videoPlayers[src]; ok {
		if p.Err() != nil {
			p.Close()
			delete(s.videoPlayers, src)
			s.videoFailed[src] = true
			return nil, false
		}
		return p, true
	}
	if s.videoFailed[src] {
		return nil, false
	}
	resolved := s.resolveMediaPath(src)
	st, err := os.Stat(resolved)
	if err != nil || st.IsDir() || st.Size() == 0 {
		s.videoFailed[src] = true
		return nil, false
	}
	player, err := video.Open(resolved)
	if err != nil {
		s.videoFailed[src] = true
		return nil, false
	}
	player.Pause()
	s.videoPlayers[src] = player
	return player, true
}

func (s *Session) renderVideoContent(c *ui.Context, id model.BlockID, b *model.Block) {
	pal := theme.Ocean()
	lbl := strings.TrimSpace(b.ImageAlt)
	if lbl == "" {
		lbl = filepath.Base(b.ImageSrc)
	}
	if lbl == "" {
		lbl = theme.Tr("视频", "Video")
	}

	player, ok := s.resolveVideoPlayer(b.ImageSrc)
	if ok {
		vw, vh := player.Size()
		stage := ui.Column(c.Key(fmt.Sprintf("video-stage-%d", id))).FillWidth().Background(pal.CodeBlockBG)
		stage.Children(func() {
			if player.Ready() && vw > 0 && vh > 0 {
				vidEl := ui.Video(c, player).FillWidth().Cursor(ui.CursorPointer)
				if vidEl.Clicked() {
					if player.Playing() {
						player.Pause()
					} else {
						player.Play()
					}
				}
			} else {
				ph := ui.Row(c.Key("vid-loading")).FillWidth().Height(160).Center().Gap(theme.Space8).Background(pal.CodeBlockBG)
				ph.Children(func() {
					ui.Text(c, lbl).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
				})
			}

			bar := ui.Row(c.Key("vid-controls")).FillWidth().Height(theme.HeightControl).PaddingX(theme.Space10).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).
				Background(pal.Paper).BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
			bar.Children(func() {
				left := ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Shrink(1)
				left.Children(func() {
					playText := "▶"
					playLabel := theme.Tr("播放", "Play")
					if player.Playing() {
						playText = "❚❚"
						playLabel = theme.Tr("暂停", "Pause")
					}
					playBtn := ui.ButtonBase(c.Key(fmt.Sprintf("vid-play-%d", id))).Size(theme.IconButtonBox, theme.IconButtonBox).Center().
						Radius(theme.RadiusSM).Background(pal.Hover).Cursor(ui.CursorPointer).Label(playLabel)
					playBtn.Children(func() {
						ui.Text(c, playText).FontSize(theme.FontMeta).FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()
					})
					appUi.Probe(playBtn, fmt.Sprintf("video-play-%d", id))
					if playBtn.Clicked() {
						if player.Playing() {
							player.Pause()
						} else {
							player.Play()
						}
					}
					ui.Text(c, lbl).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
				})

				openBtn := appUi.IconButton(c, icons.LinkOpen, appUi.IconButtonOptions{
					ID:       fmt.Sprintf("vid-open-%d", id),
					Label:    theme.Tr("用默认播放器打开", "Open in default player"),
					Tooltip:  theme.Tr("用默认播放器打开", "Open in default player"),
					Size:     theme.IconButtonBox,
					IconSize: theme.IconInline,
				})
				if openBtn.Clicked() {
					player.Pause()
					s.openBlockLink(c.Services(), b.ImageSrc)
				}
			})
		})
		return
	}

	// Graceful fallback card when the OS codec cannot decode the video inline or file is external.
	card := ui.Row(c.Key(fmt.Sprintf("video-fallback-%d", id))).FillWidth().Height(64).PaddingX(theme.Space16).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space10).Background(pal.CodeBlockBG).Cursor(ui.CursorPointer)
	card.Children(func() {
		left := ui.Row(c).AlignItems(ui.Center).Gap(theme.Space10).Shrink(1)
		left.Children(func() {
			badge := ui.Box(c).Size(theme.HeightControl, theme.HeightControl).Center().Radius(theme.RadiusMD).Background(pal.Selected)
			badge.Children(func() {
				ui.Text(c, "▶").FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).TextColor(pal.Accent).SingleLine()
			})
			ui.Text(c, lbl).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
		})
		openBtn := appUi.IconButton(c, icons.LinkOpen, appUi.IconButtonOptions{
			ID:       fmt.Sprintf("vid-open-%d", id),
			Label:    theme.Tr("打开视频", "Open video"),
			Size:     theme.HeightCompact,
			IconSize: theme.IconAction,
		})
		if openBtn.Clicked() {
			s.openBlockLink(c.Services(), b.ImageSrc)
		}
	})
	if card.Clicked() {
		s.openBlockLink(c.Services(), b.ImageSrc)
	}
}
