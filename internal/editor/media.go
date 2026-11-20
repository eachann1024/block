package editor

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/media"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"
)

const maxImportedFiles = 64

// IsSupportedVideoPath reports whether path has a video file extension Block can embed and play.
func IsSupportedVideoPath(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".mp4", ".mov", ".m4v", ".webm", ".mkv", ".avi", ".ogv":
		return true
	default:
		return false
	}
}

func (s *Session) mediaDir() string {
	if s.notePath != "" {
		return filepath.Dir(s.notePath)
	}
	if s.dataDir != "" {
		return filepath.Join(s.dataDir, "quick")
	}
	return ""
}

func (s *Session) openAssetRoot() (*os.Root, error) {
	dir := s.mediaDir()
	if dir == "" {
		return nil, media.ErrInvalid
	}
	if s.notePath == "" {
		data, err := os.OpenRoot(s.dataDir)
		if err != nil {
			return nil, err
		}
		defer data.Close()
		quick, err := openAssetSubdir(data, "quick")
		if err != nil {
			return nil, err
		}
		defer quick.Close()
		return openAssetSubdir(quick, "assets")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return openAssetSubdir(root, "assets")
}

func openAssetSubdir(root *os.Root, name string) (*os.Root, error) {
	if err := root.Mkdir(name, 0o755); err != nil && !os.IsExist(err) {
		return nil, err
	}
	info, err := root.Lstat(name)
	if err != nil || !info.IsDir() {
		return nil, media.ErrInvalid
	}
	sub, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := sub.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		sub.Close()
		return nil, media.ErrInvalid
	}
	return sub, nil
}

func writeImportedAsset(root *os.Root, source io.Reader, prefix, ext string, limit int64) (string, int64, error) {
	if ext != "" {
		for _, ch := range ext {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9') || len(ext) > 16 {
				ext = ""
				break
			}
		}
		if ext != "" {
			ext = "." + ext
		}
	}
	name := fmt.Sprintf("%s_%s%s", prefix, rand.Text(), ext)
	fd, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", 0, err
	}
	n, copyErr := io.Copy(fd, io.LimitReader(source, limit+1))
	closeErr := fd.Close()
	if copyErr != nil || closeErr != nil || n > limit {
		_ = root.Remove(name)
		return "", 0, media.ErrInvalid
	}
	return "./assets/" + name, n, nil
}

// ImportFiles imports files explicitly selected by the host, such as a drop
// from the file manager. Clipboard paths use importFiles with a workspace scope.
func (s *Session) ImportFiles(paths []string, targetID model.BlockID, replace bool) int {
	return s.importFiles(paths, targetID, replace, false, false)
}

// ImportImageFiles imports images chosen in the file picker.
func (s *Session) ImportImageFiles(paths []string, targetID model.BlockID, replace bool) int {
	return s.importFiles(paths, targetID, replace, true, false)
}

func (s *Session) importFiles(paths []string, targetID model.BlockID, replace, imagesOnly, clipboard bool) int {
	if s.doc == nil || s.readOnly || len(paths) == 0 {
		return 0
	}
	var root *os.Root
	defer func() {
		if root != nil {
			root.Close()
		}
	}()
	budget := int64(media.MaxImportBytes)
	var inserted []model.Block
	for _, path := range paths[:min(len(paths), maxImportedFiles)] {
		if !filepath.IsAbs(path) || (imagesOnly && !IsSupportedImagePath(path)) {
			continue
		}
		dir, ref := filepath.Dir(path), url.PathEscape(filepath.Base(path))
		if clipboard {
			// A clipboard list can be supplied as URI text by another app.
			// Treat its paths like document references, with no external file grant.
			dir, ref = s.mediaDir(), (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath()
		}

		base := strings.NewReplacer("[", "", "]", "").Replace(filepath.Base(path))
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		prefix, limit := "file", budget
		switch {
		case IsSupportedImagePath(path):
			prefix, limit = "img", min(budget, media.MaxImageBytes)
		case IsSupportedVideoPath(path):
			prefix, limit = "vid", min(budget, media.MaxVideoBytes)
		}
		f, _, err := media.OpenLocal(dir, ref, limit)
		if err != nil {
			continue
		}
		var source io.Reader = f
		if prefix != "file" {
			raw, err := media.ReadFile(f, limit)
			f.Close()
			if err != nil {
				continue
			}
			ext, err = validatedAssetExtension(raw, prefix, ext)
			if err != nil {
				continue
			}
			source = bytes.NewReader(raw)
		}
		if root == nil {
			root, err = s.openAssetRoot()
			if err != nil {
				if prefix == "file" {
					f.Close()
				}
				break
			}
		}
		src, written, err := writeImportedAsset(root, source, prefix, ext, limit)
		if prefix == "file" {
			f.Close()
		}
		if err != nil {
			continue
		}
		budget -= written
		b := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
		if prefix == "file" {
			b.Spans = []model.Span{{Text: base, Link: src}}
		} else {
			if prefix == "img" {
				base = strings.TrimSuffix(base, filepath.Ext(base))
			}
			b.Kind, b.ImageSrc, b.ImageAlt = model.BlockImage, src, base
			b.ImageAlign, b.ImageWidth = model.ImageAlignLeft, 100
		}
		inserted = append(inserted, b)
		if budget == 0 {
			break
		}
	}
	if len(inserted) == 0 {
		return 0
	}
	return s.insertImportedBlocks(inserted, targetID, replace)
}

func validatedAssetExtension(raw []byte, prefix, ext string) (string, error) {
	if prefix == "vid" {
		if !media.IsVideo(raw) {
			return "", media.ErrInvalid
		}
		return ext, nil
	}
	if ext == "svg" {
		_, err := parseLocalSVG(raw)
		return ext, err
	}
	_, _, format, err := media.DecodeRaster(raw)
	if format == "jpeg" {
		format = "jpg"
	}
	return format, err
}

// ImportClipboardImageBytes validates the bitmap and names the file using its
// detected format. The transfer representation's extension is only a hint.
func (s *Session) ImportClipboardImageBytes(raw []byte, _ string, targetID model.BlockID, replace bool) bool {
	if s.doc == nil || s.readOnly {
		return false
	}
	ext, err := validatedAssetExtension(raw, "img", "")
	if err != nil {
		return false
	}
	root, err := s.openAssetRoot()
	if err != nil {
		return false
	}
	defer root.Close()
	src, _, err := writeImportedAsset(root, bytes.NewReader(raw), "img", ext, media.MaxImageBytes)
	if err != nil {
		return false
	}
	blk := model.Block{
		ID:         model.NewBlockID(),
		Kind:       model.BlockImage,
		ImageSrc:   src,
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
	s.imageMu.Lock()
	delete(s.imageCache, src)
	s.imageOrder = slices.DeleteFunc(s.imageOrder, func(key string) bool { return key == src })
	delete(s.imageFailed, src)
	s.imageMu.Unlock()
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

	if len(fallbackText) > media.MaxPasteBytes {
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
	paths := clipboardFilePaths(svc.ReadClipboardData(transfer.FileList))
	if s.importFiles(paths, 0, false, false, true) > 0 {
		return true
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
		mdRaw, mdErr := richData.Read(transfer.Markdown)
		htmlRaw, htmlErr := richData.Read(transfer.HTML)
		if len(mdRaw) > media.MaxPasteBytes {
			mdRaw = nil
		}
		if len(htmlRaw) > media.MaxPasteBytes {
			htmlRaw = nil
		}
		mdStr := string(mdRaw)
		inTable := s.isTablePasteTarget()
		if inTable && htmlErr == nil && len(htmlRaw) > 0 {
			if grid, ok := decodeBlockTableCellsHTML(string(htmlRaw)); ok {
				s.pasteTableCellsGrid(grid)
				return true
			}
		}
		if mdErr == nil && (strings.TrimSpace(mdStr) != "" || (len(mdRaw) > 0 && (inTable || !strings.Contains(mdStr, "\n")))) {
			if inTable && htmlErr == nil && len(htmlRaw) > 0 && htmlTableRowCountMatches(string(htmlRaw), mdStr) {
				s.pasteTableWithTrailingEmptyRow(mdStr)
				return true
			}
			s.PasteText(mdStr)
			return true
		}
		if htmlErr == nil && len(htmlRaw) > 0 && s.codec != nil {
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

func clipboardFilePaths(data transfer.Data) []string {
	const maxListBytes = 64 << 10
	var list bytes.Buffer
	items := data.Items()
	for _, item := range items[:min(len(items), maxImportedFiles)] {
		raw, err := item.Read(transfer.FileList)
		if err != nil {
			continue
		}
		if len(raw) > maxListBytes-list.Len()-2 {
			return nil
		}
		list.Write(raw)
		list.WriteString("\r\n")
	}
	if list.Len() == 0 {
		return nil
	}
	bounded := transfer.New(transfer.NewItem(transfer.Bytes(transfer.FileList, list.Bytes())))
	paths, _ := bounded.Files()
	return paths
}

func localFileURL(absPath string) string {
	p := filepath.ToSlash(absPath)
	isUNC := strings.HasPrefix(absPath, `\\`) || strings.HasPrefix(p, "//")
	if isUNC || (len(p) >= 3 && p[1] == ':' && p[2] == '\\') {
		p = strings.ReplaceAll(p, "\\", "/")
	}
	prefix := "file://"
	if isUNC {
		p = strings.TrimLeft(p, "/")
	} else if len(p) > 0 && p[0] != '/' {
		p = "/" + p
	}
	const hex = "0123456789ABCDEF"
	b := make([]byte, 0, len(p)+16)
	b = append(b, prefix...)
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

func (s *Session) openBlockLink(svc ui.Services, rawLink string) {
	if s.previewMedia != nil {
		return
	}
	if href := media.WebLink(rawLink); href != "" {
		svc.OpenURL(href)
		return
	}
	path, ok := media.LocalTarget(s.mediaDir(), rawLink)
	if !ok {
		return
	}
	if files.IsMarkdown(path) && s.host != nil {
		_ = s.host.OpenNote(path, 0)
		return
	}
	// Open known document and media types. The shell can run some file types.
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf", ".txt", ".csv", ".tsv", ".rtf", ".docx", ".xlsx", ".pptx",
		".odt", ".ods", ".odp", ".zip", ".png", ".jpg", ".jpeg", ".gif",
		".webp", ".bmp", ".mp3", ".wav", ".m4a", ".flac", ".mp4", ".mov",
		".m4v", ".webm", ".mkv", ".avi", ".ogv":
		svc.OpenURL(localFileURL(path))
	}
}

func (s *Session) resetMedia() {
	s.closeVideoPlayers()
	s.imageMu.Lock()
	s.imageEpoch++
	clear(s.imageCache)
	clear(s.imageLoading)
	clear(s.imageFailed)
	s.imageOrder = nil
	s.imageMu.Unlock()
}

func (s *Session) closeVideoPlayers() {
	for k, p := range s.videoPlayers {
		if p != nil {
			p.Close()
		}
		delete(s.videoPlayers, k)
	}
	clear(s.videoFailed)
	clear(s.videoBytes)
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
			delete(s.videoBytes, src)
			s.videoFailed[src] = true
			return nil, false
		}
		return p, true
	}
	if s.videoFailed[src] {
		return nil, false
	}
	var player *ui.VideoPlayer
	var err error
	if s.previewMedia != nil {
		asset := s.previewMedia(src, true, false)
		if len(asset.Video) == 0 {
			return nil, false
		}
		player, err = video.OpenData(asset.Video)
	} else {
		remaining := int64(media.MaxImportBytes)
		for _, size := range s.videoBytes {
			remaining -= size
		}
		limit := min(remaining, media.MaxVideoBytes)
		f, _, openErr := media.OpenLocal(s.mediaDir(), src, limit)
		if openErr != nil {
			s.videoFailed[src] = true
			return nil, false
		}
		raw, readErr := media.ReadFile(f, limit)
		f.Close()
		if readErr != nil || !media.IsVideo(raw) {
			s.videoFailed[src] = true
			return nil, false
		}
		player, err = video.OpenData(raw)
		if err == nil {
			if s.videoBytes == nil {
				s.videoBytes = make(map[string]int64)
			}
			s.videoBytes[src] = int64(len(raw))
		}
	}
	if err != nil {
		s.videoFailed[src] = true
		return nil, false
	}
	if s.previewMedia == nil {
		player.Pause()
	}
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
			} else if s.previewMedia != nil {
				ui.Video(c, player).FillWidth().Height(160).Fit(ui.Contain).Label(lbl)
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

				if s.previewMedia != nil {
					return
				}
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

	if s.previewMedia != nil {
		asset := s.previewMedia(b.ImageSrc, true, false)
		label := theme.Tr("播放视频", "Play video")
		if asset.Loading {
			label = theme.Tr("正在加载视频…", "Loading video…")
		} else if asset.Unavailable || asset.Video != nil || s.videoFailed[b.ImageSrc] {
			label = theme.Tr("视频无法播放", "Video unavailable")
		}
		ui.Row(c).FillWidth().Height(72).Center().Background(pal.CodeBlockBG).Children(func() {
			btn := appUi.ActionButton(c, fmt.Sprintf("preview-video-%d", id), label, nil, appUi.ButtonSecondary, asset.Loading || asset.Unavailable || asset.Video != nil || s.videoFailed[b.ImageSrc])
			if btn.Clicked() {
				s.previewMedia(b.ImageSrc, true, true)
			}
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
