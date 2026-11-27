package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
)

// TitlebarSurfaceKind mirrors thinkly-fast's TitlebarSurface enum.
type TitlebarSurfaceKind uint8

const (
	SurfaceEmpty TitlebarSurfaceKind = iota
	SurfaceNote
	SurfaceImagePreview
	SurfaceSVG
	SurfaceHTML
	SurfaceCode
	SurfaceBlocked
)

// TabSurface stores per-tab state for non-Markdown open files (raster images, SVG, HTML, code/text, or blocked).
type TabSurface struct {
	Path          string
	Kind          model.OpenKind
	Source        model.SourceFile
	HasSource     bool
	IsSVG         bool
	IsHTML        bool
	SavedText       string
	DiskFingerprint int64
	FileSize        int64
	BlockedReason   string
	StatusMsg       string
	GetBinding      func() (model.VerifiedFileTarget, bool)
	OnSavedIdentity func(newInfo os.FileInfo, newID string)

	SVGShowSource bool
	Zoom          float32
	ZoomFitMode   bool

	imgMu        sync.Mutex
	PreviewBytes []byte
	ImgSrc       ui.ImageSource
	ImgLoaded    bool
	ImgLoading   bool
	ImgErr       string
	OnRedraw     func()
	SVGSrc       *ui.SVG
	SVGText      string
	SVGParsed    bool

	// FitScale is the scale Fit mode drew the preview at in the last frame,
	// shown as the titlebar zoom percentage.
	FitScale float32

	code        *codeed.State
	codeVersion uint64
	codeLangGen uint64
	scrollbars  blockui.EditorScrollbars
	findOpen    bool
}

// NewTabSurface constructs a TabSurface from a ClassifiedFile.
func NewTabSurface(path string, cf model.ClassifiedFile) *TabSurface {
	return newTabSurfaceOpts(path, cf, false)
}

// NewTabSurfacePreloaded constructs a TabSurface from an already-verified and
// classified file without performing any fallback filesystem I/O on the UI thread.
func NewTabSurfacePreloaded(path string, cf model.ClassifiedFile) *TabSurface {
	return newTabSurfaceOpts(path, cf, true)
}

func newTabSurfaceOpts(path string, cf model.ClassifiedFile, preloaded bool) *TabSurface {
	var src model.SourceFile
	hasSource := cf.Source != nil
	if hasSource {
		src = *cf.Source
		if _, tag := plugins.ResolveCodeLanguageForFile(path); tag != "" {
			src.Language = tag
		}
	}
	isSVG := files.IsSVGPath(path) || src.SVG
	isHTML := src.HTML || strings.EqualFold(src.Language, "html")
	fileSize := cf.RawSize
	if fileSize == 0 {
		switch {
		case hasSource:
			fileSize = int64(len(src.Text))
		case len(cf.PreviewBytes) > 0:
			fileSize = int64(len(cf.PreviewBytes))
		case !preloaded:
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				fileSize = info.Size()
			}
		}
	}
	diskFP := src.DiskFingerprint
	if hasSource && diskFP == 0 {
		diskFP = files.TextFingerprint(src.Text, src.BOM)
	}
	var previewBytes []byte
	if !isSVG && len(cf.PreviewBytes) > 0 {
		previewBytes = cf.PreviewBytes
	}
	s := &TabSurface{
		Path:            path,
		Kind:            cf.Kind,
		Source:          src,
		HasSource:       hasSource,
		IsSVG:           isSVG,
		IsHTML:          isHTML,
		SavedText:       src.Text,
		DiskFingerprint: diskFP,
		FileSize:        fileSize,
		BlockedReason:   cf.Reason,
		Zoom:            1.0,
		ZoomFitMode:     true,
		PreviewBytes:    previewBytes,
	}
	if isSVG {
		if !hasSource {
			if len(cf.PreviewBytes) > 0 {
				s.Source.Text = string(cf.PreviewBytes)
			} else if !preloaded && cf.Kind == model.OpenKindPreview && (cf.RawSize == 0 || cf.RawSize <= files.PreviewLimit) {
				if raw, over, err := files.ReadBoundedRegular(path, files.PreviewLimit); err == nil && !over {
					s.Source.Text = string(raw)
				}
			}
		}
		s.reparseSVG()
	}
	return s
}

func (s *TabSurface) reparseSVG() {
	if s.SVGParsed && s.SVGText == s.Source.Text {
		return
	}
	s.SVGParsed = true
	s.SVGText = s.Source.Text
	parsed, err := ui.ParseSVG([]byte(s.Source.Text))
	if err == nil {
		s.SVGSrc = parsed
	} else {
		s.SVGSrc = nil
	}
}

func (s *TabSurface) ensureImageLoaded() {
	s.imgMu.Lock()
	if s.ImgLoaded || s.ImgLoading {
		s.imgMu.Unlock()
		return
	}
	onRedraw := s.OnRedraw
	path := s.Path
	preBytes := s.PreviewBytes
	loadImage := func() (ui.ImageSource, string) {
		raw := preBytes
		if len(raw) == 0 {
			var over bool
			var err error
			raw, over, err = files.ReadBoundedRegular(path, files.PreviewLimit)
			if err != nil {
				return nil, err.Error()
			}
			if over {
				return nil, theme.Tr("文件超过预览大小上限", "File exceeds preview size limit")
			}
		}
		decoded, decErr := ui.DecodeImage(raw)
		if decErr != nil {
			return nil, decErr.Error()
		}
		return decoded, ""
	}
	if onRedraw != nil && !testing.Testing() && !blockui.IsProbeEnabled() {
		s.ImgLoading = true
		s.imgMu.Unlock()
		go func() {
			src, errStr := loadImage()
			s.imgMu.Lock()
			s.ImgLoading = false
			s.ImgLoaded = true
			s.ImgSrc = src
			s.ImgErr = errStr
			s.PreviewBytes = nil
			s.imgMu.Unlock()
			onRedraw()
		}()
		return
	}
	s.ImgLoaded = true
	s.imgMu.Unlock()

	src, errStr := loadImage()
	s.imgMu.Lock()
	s.ImgSrc = src
	s.ImgErr = errStr
	s.PreviewBytes = nil
	s.imgMu.Unlock()
}

// TitlebarSurface maps this tab surface to its contextual titlebar tool set.
func (s *TabSurface) TitlebarSurface() TitlebarSurfaceKind {
	if s == nil {
		return SurfaceEmpty
	}
	switch s.Kind {
	case model.OpenKindBlocked:
		return SurfaceBlocked
	case model.OpenKindPreview:
		if s.IsSVG {
			return SurfaceSVG
		}
		return SurfaceImagePreview
	case model.OpenKindSource:
		if s.IsSVG {
			return SurfaceSVG
		}
		if s.IsHTML {
			return SurfaceHTML
		}
		return SurfaceCode
	default:
		return SurfaceNote
	}
}

// IsEditableSource reports whether this surface carries editable text source.
func (s *TabSurface) IsEditableSource() bool {
	if s == nil {
		return false
	}
	return s.HasSource && (s.Kind == model.OpenKindSource || (s.Kind == model.OpenKindPreview && s.IsSVG))
}

// IsDirty reports whether an editable source surface differs from its saved disk text.
func (s *TabSurface) IsDirty() bool {
	if !s.IsEditableSource() {
		return false
	}
	return s.Source.Text != s.SavedText
}

// ZoomPctString returns the formatted zoom percentage (e.g. "100%").
func (s *TabSurface) ZoomPctString() string {
	if s == nil || s.Zoom <= 0 {
		return "100%"
	}
	z := s.Zoom
	if s.ZoomFitMode && s.FitScale > 0 {
		z = s.FitScale
	}
	return fmt.Sprintf("%d%%", int(z*100+0.5))
}

// previewCaptionH is the height of the "name · W×H" caption under an image
// preview plus the gap above it.
const previewCaptionH float32 = 20 + theme.Space10

// previewStageSize returns the preview stage's own size from the previous frame:
// the pane between the sidebar and any side panels, not the whole window. Before
// the first layout it falls back to the window size.
func previewStageSize(c *ui.Context, stage ui.Element) (float32, float32) {
	if b := stage.LayoutBounds(); b.W > 0 && b.H > 0 {
		return b.W, b.H
	}
	return c.Size()
}

// fitPreviewScale returns the scale that fits an iw×ih preview inside a stage of
// stageW×stageH (padded by Space24, inside a card padded by cardPad with a 1px
// border, above a caption of captionH). Fit only shrinks, never enlarges.
func fitPreviewScale(iw, ih, stageW, stageH, cardPad, captionH float32) float32 {
	if iw <= 0 || ih <= 0 {
		return 1
	}
	chrome := 2*theme.Space24 + 2*(cardPad+1)
	availW := max(stageW-chrome, 1)
	availH := max(stageH-chrome-captionH, 1)
	return min(float32(1), min(availW/iw, availH/ih))
}

// ZoomStep multiplies preview zoom and exits Fit mode.
func (s *TabSurface) ZoomStep(factor float32) {
	if s == nil {
		return
	}
	base := s.Zoom
	if s.ZoomFitMode && s.FitScale > 0 {
		base = s.FitScale
	}
	z := base * factor
	if z < 0.1 {
		z = 0.1
	}
	if z > 8.0 {
		z = 8.0
	}
	s.Zoom = z
	s.ZoomFitMode = false
}

// ZoomActual resets zoom to 100% (1:1).
func (s *TabSurface) ZoomActual() {
	if s == nil {
		return
	}
	s.Zoom = 1.0
	s.ZoomFitMode = false
}

// ZoomFit enables Fit-to-viewport mode.
func (s *TabSurface) ZoomFit() {
	if s == nil {
		return
	}
	s.Zoom = 1.0
	s.ZoomFitMode = true
}

// IsPreviewZoomActive reports whether this surface is currently displaying a zoomable image or SVG preview.
func (s *TabSurface) IsPreviewZoomActive() bool {
	if s == nil {
		return false
	}
	if s.IsSVG {
		return !s.SVGShowSource || !s.HasSource
	}
	return s.Kind == model.OpenKindPreview
}

// HandlePreviewZoom applies a zoom shortcut when this surface is showing an image or SVG preview.
// A zero delta restores the actual size.
func (s *TabSurface) HandlePreviewZoom(delta int) bool {
	if !s.IsPreviewZoomActive() {
		return false
	}
	switch {
	case delta > 0:
		s.ZoomStep(1.2)
	case delta < 0:
		s.ZoomStep(1.0 / 1.2)
	default:
		s.ZoomActual()
	}
	return true
}

// SourceStatusLabel returns an exceptional status notice (e.g. read-only preview
// or transient error/status message) for titlebar display. Routine language and
// encoding metadata stay hidden to keep the titlebar uncluttered.
func (s *TabSurface) SourceStatusLabel() string {
	if s == nil {
		return ""
	}
	if s.StatusMsg != "" {
		return s.StatusMsg
	}
	if s.Kind == model.OpenKindPreview && s.IsSVG && !s.HasSource {
		return theme.Tr("只读预览", "Read-only")
	}
	return ""
}

// Render draws the non-Markdown file surface inside the main workspace column.
func (s *TabSurface) Render(
	c *ui.Context,
	onReveal func(path string),
	onOpenExternal func(path string),
) {
	pal := theme.Ocean()
	switch {
	case s.Kind == model.OpenKindBlocked:
		c.Key("blocked-file-view")
		box := blockui.EmptyStage(c, pal.Paper, theme.EmptyStateWidth, func() {
			icons.Mascot(c, icons.MascotReadError, theme.EmptyMascotSize)
			blockui.EmptyHeading(c, filepath.Base(s.Path))
			reason := s.BlockedReason
			if reason == "" {
				reason = theme.Tr("此文件为二进制或超出编辑器大小限制，无法作为文本打开。", "This file is binary or exceeds the editor size limit.")
			}
			blockui.EmptyLead(c, reason).MaxWidth(theme.EmptyActionWidth)
			ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Wrap().Margin(theme.Space20, 0, 0, 0).Children(func() {
				icRev := icons.FolderReveal
				revBtn := blockui.ActionButton(c, "blocked-reveal", theme.Tr("在访达中显示", "Reveal in File Manager"), &icRev, blockui.ButtonSecondary, false)
				if revBtn.Clicked() && onReveal != nil {
					onReveal(s.Path)
				}
				icExt := icons.LinkOpen
				openBtn := blockui.ActionButton(c, "blocked-open-external", theme.Tr("用默认应用打开", "Open in Default App"), &icExt, blockui.ButtonSecondary, false)
				if openBtn.Clicked() && onOpenExternal != nil {
					onOpenExternal(s.Path)
				}
			})
		})
		blockui.Probe(box, "blocked-file-view")

	case s.IsSVG:
		if !s.SVGShowSource || !s.HasSource {
			s.reparseSVG()
			c.Key("svg-preview-stage")
			stage := blockui.Scroll(c).Fill().Background(pal.Shell)
			blockui.Probe(stage, "svg-preview-stage")
			stage.Children(func() {
				vpW, vpH := previewStageSize(c, stage)
				ui.Column(c).FillWidth().MinHeight(vpH).Padding(theme.Space24).Center().Gap(theme.Space12).Children(func() {
					card := blockui.Elevate(ui.Column(c).Padding(theme.Space24).Radius(theme.RadiusXL).Background(pal.Paper).
						Border(1, pal.Hairline).Center().Gap(theme.Space10), theme.ElevationDialog)
					card.Children(func() {
						if s.SVGSrc != nil {
							iw, ih := ui.ImageSize(s.SVGSrc)
							if iw <= 0 {
								iw = 240
							}
							if ih <= 0 {
								ih = 240
							}
							drawW, drawH := iw*s.Zoom, ih*s.Zoom
							if s.ZoomFitMode {
								scale := fitPreviewScale(iw, ih, vpW, vpH, theme.Space24, 0)
								s.FitScale = scale
								drawW, drawH = iw*scale, ih*scale
							}
							ui.Image(c, s.SVGSrc).Size(max(drawW, 4), max(drawH, 4))
						} else {
							icons.Duo(c, icons.FileSvg, 42, true)
							ui.Text(c, theme.Tr("SVG 预览不可用，可切换到源码编辑", "SVG preview unavailable — switch to Source")).
								FontSize(theme.FontBody).TextColor(pal.TextSecondary)
						}
					})
				})
			})
			return
		}
		s.renderSourceEditor(c)

	case s.Kind == model.OpenKindPreview:
		s.ensureImageLoaded()
		c.Key("image-preview-stage")
		stage := blockui.Scroll(c).Fill().Background(pal.Shell)
		blockui.Probe(stage, "image-preview-stage")
		stage.Children(func() {
			vpW, vpH := previewStageSize(c, stage)
			ui.Column(c).FillWidth().MinHeight(vpH).Padding(theme.Space24).Center().Gap(theme.Space12).Children(func() {
				card := blockui.Elevate(ui.Column(c).Padding(theme.Space16).Radius(theme.RadiusXL).Background(pal.Paper).
					Border(1, pal.Hairline).Center().Gap(theme.Space10), theme.ElevationDialog)
				card.Children(func() {
					s.imgMu.Lock()
					imgSrc := s.ImgSrc
					imgErr := s.ImgErr
					s.imgMu.Unlock()
					if imgSrc != nil {
						iw, ih := ui.ImageSize(imgSrc)
						if iw <= 0 {
							iw = 320
						}
						if ih <= 0 {
							ih = 240
						}
						drawW, drawH := iw*s.Zoom, ih*s.Zoom
						if s.ZoomFitMode {
							scale := fitPreviewScale(iw, ih, vpW, vpH, theme.Space16, previewCaptionH)
							s.FitScale = scale
							drawW, drawH = iw*scale, ih*scale
						}
						ui.Image(c, imgSrc).Size(max(drawW, 4), max(drawH, 4))
						ui.Text(c, fmt.Sprintf("%s · %dx%d", filepath.Base(s.Path), int(iw), int(ih))).
							FontSize(theme.FontBody).TextColor(pal.TextSecondary)
					} else {
						icons.Duo(c, icons.Image, 48, true)
						ui.Text(c, filepath.Base(s.Path)).FontSize(theme.FontTitle).FontWeight(theme.WeightControl).TextColor(pal.Text)
						if imgErr != "" {
							ui.Text(c, imgErr).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
						}
					}
				})
			})
		})

	default: // OpenKindSource (HTML, JSON, code, plain text)
		s.renderSourceEditor(c)
	}
}

func formatSnapshotTime(ts int64, format string) string {
	var tm time.Time
	if ts > 1e11 {
		tm = time.UnixMilli(ts)
	} else {
		tm = time.Unix(ts, 0)
	}
	return tm.Local().Format(format)
}

// PageHistoryView manages the full-window Page History stage (`app_history_view.rs` 1:1).
type PageHistoryView struct {
	Open             bool
	NotePath         string
	CurrentText      string
	IsReadOnly       bool
	ExternalConflict bool
	Entries          []model.HistorySnapshot
	SelectedID       string
	SelectedText     string
	DiffLines        []model.DiffLine
	Mode             string // "diff" or "clean"
	ConfirmOpen      bool
	ErrorMsg         string

	lastStatCheck   time.Time
	lastStatMod     time.Time
	lastStatSize    int64
	hasStat         bool
	cleanCachedText string
	cleanCachedDoc  *model.Doc
	previewScroll   ui.ScrollState
}

// HistoryToast represents the undoable banner shown at the top of the note after restoring a snapshot.
type HistoryToast struct {
	Active              bool
	NotePath            string
	Message             string
	UndoContent         string
	RestoredFingerprint int64
	CanUndo             bool
}

// OpenForNote opens the full-window Page History stage for `notePath`.
func (h *PageHistoryView) OpenForNote(
	notePath, currentText string,
	entries []model.HistorySnapshot,
	loadSnap func(id string) (string, error),
	diffFn func(oldText, newText string) []model.DiffLine,
) {
	h.Open = true
	h.NotePath = notePath
	h.CurrentText = currentText
	h.IsReadOnly = false
	h.hasStat = false
	h.lastStatCheck = time.Time{}
	if info, err := os.Stat(notePath); err == nil {
		if info.Mode().Perm()&0o200 == 0 {
			h.IsReadOnly = true
		}
		h.lastStatMod = info.ModTime()
		h.lastStatSize = info.Size()
		h.hasStat = true
		h.lastStatCheck = time.Now()
	}
	h.ExternalConflict = false
	h.Entries = append([]model.HistorySnapshot(nil), entries...)
	if h.Mode == "" {
		h.Mode = "diff"
	}
	h.ConfirmOpen = false
	h.ErrorMsg = ""
	h.SelectedID = ""
	h.SelectedText = ""
	h.DiffLines = nil
	h.cleanCachedText = ""
	h.cleanCachedDoc = nil
	h.previewScroll = ui.ScrollState{}
	if len(h.Entries) > 0 {
		h.SelectVersion(h.Entries[0].ID, loadSnap, diffFn)
	}
}

// CheckExternalConflict checks whether the underlying file on disk changed since Page History was opened.
func (h *PageHistoryView) CheckExternalConflict() bool {
	if !h.Open || h.NotePath == "" {
		return false
	}
	if h.ExternalConflict {
		return true
	}
	now := time.Now()
	if !testing.Testing() && !blockui.IsProbeEnabled() && !h.lastStatCheck.IsZero() && now.Sub(h.lastStatCheck) < 1500*time.Millisecond {
		return h.ExternalConflict
	}
	h.lastStatCheck = now
	if info, err := os.Stat(h.NotePath); err == nil {
		if h.hasStat && info.ModTime().Equal(h.lastStatMod) && info.Size() == h.lastStatSize {
			return h.ExternalConflict
		}
		h.lastStatMod = info.ModTime()
		h.lastStatSize = info.Size()
		h.hasStat = true
	}
	diskTxt, _, err := files.ReadNote(h.NotePath)
	if err == nil && diskTxt != h.CurrentText {
		h.ExternalConflict = true
	}
	return h.ExternalConflict
}

// Close exits the full-window Page History stage.
func (h *PageHistoryView) Close() {
	h.Open = false
	h.ConfirmOpen = false
}

// SelectVersion loads and diffs a snapshot by ID.
func (h *PageHistoryView) SelectVersion(
	id string,
	loadSnap func(id string) (string, error),
	diffFn func(oldText, newText string) []model.DiffLine,
) {
	h.SelectedID = id
	h.ConfirmOpen = false
	if loadSnap == nil || id == "" {
		h.SelectedText = ""
		h.DiffLines = nil
		return
	}
	txt, err := loadSnap(id)
	if err != nil {
		h.ErrorMsg = err.Error()
		h.SelectedText = ""
		h.DiffLines = nil
		return
	}
	h.ErrorMsg = ""
	h.SelectedText = txt
	if diffFn != nil {
		h.DiffLines = diffFn(h.CurrentText, txt)
	}
}

// Navigate steps up (-1) or down (+1) in the snapshot timeline.
func (h *PageHistoryView) Navigate(
	delta int,
	loadSnap func(id string) (string, error),
	diffFn func(oldText, newText string) []model.DiffLine,
) {
	if len(h.Entries) == 0 || h.ConfirmOpen {
		return
	}
	cur := 0
	for i, e := range h.Entries {
		if e.ID == h.SelectedID {
			cur = i
			break
		}
	}
	next := cur + delta
	if next < 0 {
		next = 0
	}
	if next >= len(h.Entries) {
		next = len(h.Entries) - 1
	}
	if h.Entries[next].ID != h.SelectedID {
		h.SelectVersion(h.Entries[next].ID, loadSnap, diffFn)
	}
}

func (h *PageHistoryView) selectedTimestamp() string {
	for _, e := range h.Entries {
		if e.ID == h.SelectedID {
			return formatSnapshotTime(e.Timestamp, "2006-01-02 15:04:05")
		}
	}
	return ""
}

// Render renders the full-window Page History stage (40px titlebar + split diff/clean preview + timeline + confirm dialog).
func (h *PageHistoryView) Render(
	c *ui.Context,
	onBack func(),
	onSelect func(id string),
	onNavigate func(delta int),
	onRetry func(),
	onConfirmRestore func(snapID, content string),
) {
	pal := theme.Ocean()
	tb := c.TitleBar()
	vpW, _ := c.Size()
	if vpW <= 0 {
		vpW = 1100
	}
	narrowHist := vpW < 780
	leftLead := float32(12)
	if blockui.MacTrafficLightClear() {
		leftLead = max(82, tb.Left+12)
	}
	trailingPad := float32(12)
	if !theme.IsMacOS() && tb.Right > 0 {
		trailingPad = tb.Right + 8
	}

	c.Key("history-view")
	root := ui.Column(c).Fill().Background(pal.Paper)
	blockui.Probe(root, "history-view")
	if root.Shortcut(0, ui.KeyEscape) {
		if h.ConfirmOpen {
			h.ConfirmOpen = false
		} else if onBack != nil {
			onBack()
		}
	}
	if root.Shortcut(0, ui.KeyUp) && onNavigate != nil {
		onNavigate(-1)
	}
	if root.Shortcut(0, ui.KeyDown) && onNavigate != nil {
		onNavigate(1)
	}

	root.Children(func() {
		// 1. Dedicated 40px Page History Titlebar.
		bar := ui.Row(c).FillWidth().Height(TitlebarHeight).Shrink(0).AlignItems(ui.Center).
			Background(pal.Shell).BorderWidth(0, 0, 1, 0).BorderColor(pal.SidebarBorder)
		bar.Children(func() {
			left := ui.Row(c).FillHeight().Shrink(0).Padding(0, 12, 0, leftLead).AlignItems(ui.Center).Gap(8).
				BorderWidth(0, 1, 0, 0).BorderColor(pal.SidebarBorder).DragWindow()
			left.Children(func() {
				backBtn := blockui.IconButton(c, icons.ArrowLeft, blockui.IconButtonOptions{
					ID:      "history-back",
					Tooltip: theme.Tr("返回", "Back"),
				})
				if backBtn.Clicked() && onBack != nil {
					onBack()
				}
			})

			center := ui.Row(c).Grow(1).MinWidth(16).FillHeight().PaddingX(12).AlignItems(ui.Center).
				Justify(ui.SpaceBetween).Gap(10).Clip().DragWindow()
			blockui.Probe(center, "history-titlebar-drag")
			center.Children(func() {
				noteName := noteDisplayTitle(h.NotePath, false)
				pill := ui.Row(c).Height(theme.HeightCompact).PaddingX(theme.Space10).MaxWidth(360).Shrink(1).MinWidth(0).AlignItems(ui.Center).
					Radius(theme.RadiusMD).Background(pal.Selected).Border(1, pal.Hairline)
				pill.Children(func() {
					ui.Text(c, fmt.Sprintf("%s · %s", noteName, theme.Tr("页面历史", "Page History"))).
						FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
				})
				ts := h.selectedTimestamp()
				if ts != "" && !narrowHist {
					ui.Text(c, ts).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).Shrink(2).MinWidth(0).SingleLine().Ellipsis("…")
				}
			})

			right := ui.Row(c).FillHeight().Shrink(0).Padding(0, trailingPad, 0, 0).AlignItems(ui.Center).Gap(8)
			right.Children(func() {
				modeIdx := 0
				if h.Mode == "clean" {
					modeIdx = 1
				}
				if blockui.SegmentedControl(c, "history-mode-seg", &modeIdx, []blockui.SegmentOption{
					{ID: "history-mode-diff", Label: theme.Tr("差异对比", "Diff")},
					{ID: "history-mode-clean", Label: theme.Tr("阅读视图", "Clean")},
				}) {
					if modeIdx == 1 {
						h.Mode = "clean"
					} else {
						h.Mode = "diff"
					}
				}

				canRestore := h.SelectedID != "" && !h.IsReadOnly && !h.ExternalConflict && h.ErrorMsg == "" && h.SelectedText != h.CurrentText
				restoreHint := theme.Tr("恢复此版本", "Restore this version")
				switch {
				case h.IsReadOnly:
					restoreHint = theme.Tr("此页面只读，无法恢复。", "This page is read-only; cannot restore.")
				case h.ExternalConflict:
					restoreHint = theme.Tr("页面已更新，请重新对比。", "Page has been updated externally; please re-diff.")
				case h.SelectedID != "" && h.SelectedText == h.CurrentText:
					restoreHint = theme.Tr("与当前版本内容相同", "Identical to current version")
				}
				histIc := icons.History
				resBtn := blockui.ActionButton(
					c,
					"history-restore",
					theme.Tr("恢复此版本", "Restore Version"),
					&histIc,
					blockui.ButtonPrimary,
					!canRestore || h.ConfirmOpen,
				)
				if restoreHint != "" {
					blockui.Tooltip(c, resBtn, restoreHint)
				}
				if resBtn.Clicked() && canRestore {
					h.ConfirmOpen = true
				}
			})
		})

		// 2. Split body: Left preview + Right 268px timeline sidebar.
		h.CheckExternalConflict()
		ui.Row(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			leftWrap := ui.Column(c).Grow(1).MinWidth(0).FillHeight().Background(pal.Paper)
			leftWrap.Children(func() {
				if h.ExternalConflict {
					bannerMarginX := float32(28)
					if narrowHist {
						bannerMarginX = 16
					}
					ui.Row(c).FillWidth().MinWidth(0).Margin(12, bannerMarginX, 0, bannerMarginX).Padding(10, 12, 10, 14).
						Radius(theme.RadiusMD).Background(pal.Shell).Border(1, pal.Hairline).
						AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).Children(func() {
						ui.Text(c, theme.Tr("页面已更新，请重新对比。", "Page has been updated externally; please re-diff.")).
							FontSize(theme.FontBody).TextColor(pal.TextSecondary).Grow(1).MinWidth(0)
						refIc := icons.Refresh
						reBtn := blockui.ActionButton(c, "history-error-action", theme.Tr("重新对比", "Re-diff"), &refIc, blockui.ButtonSecondary, false)
						if reBtn.Clicked() && onRetry != nil {
							onRetry()
						}
					})
				}

				previewPadX := float32(36)
				if narrowHist {
					previewPadX = 20
				}
				previewCol := blockui.Scroll(c, &h.previewScroll).Grow(1).MinHeight(0).FillWidth().Padding(28, previewPadX, 40, previewPadX).Background(pal.Paper)
				blockui.Probe(previewCol, "history-preview")
				previewBounds := previewCol.Bounds()
				_, codeFont, _ := theme.Typography()
				edFont := theme.ResolvedEditorFont()
				if strings.TrimSpace(codeFont) == "" {
					codeFont = "monospace"
				}
				previewCol.Children(func() {
					c.Key("history-stage:" + h.Mode + ":" + h.SelectedID)
					inner := ui.Column(c).FillWidth().MaxWidth(672).Gap(8).MarginX(0).Transition(blockui.SurfaceStageTransition())
					inner.Children(func() {
						if len(h.Entries) == 0 {
							ui.Column(c).FillWidth().PaddingY(48).Center().Gap(10).Children(func() {
								icons.Duo(c, icons.History, 36, true)
								ui.Text(c, theme.Tr("当前笔记还没有历史版本快照（保存修改后自动生成）", "No history snapshots recorded for this note yet.")).
									FontSize(theme.FontBody).TextColor(pal.TextSecondary)
							})
							return
						}
						if h.ErrorMsg != "" {
							ui.Column(c).FillWidth().Padding(16).Radius(theme.RadiusMD).Background(pal.DangerSurface).Gap(8).Children(func() {
								ui.Text(c, h.ErrorMsg).FontSize(theme.FontBody).TextColor(pal.Danger)
								retryBtn := blockui.BtnRetry(c, "history-retry", blockui.PresetOptions{})
								if retryBtn.Clicked() && onRetry != nil {
									onRetry()
								}
							})
							return
						}
						if h.Mode == "clean" {
							if h.cleanCachedDoc == nil || h.cleanCachedText != h.SelectedText {
								h.cleanCachedText = h.SelectedText
								h.cleanCachedDoc = markdown.ParseMarkdown(h.SelectedText)
							}
							renderHistoryCleanDoc(c, h.cleanCachedDoc, edFont, codeFont)
							return
						}
						const estDiffRowPitch float32 = 34
						firstIdx, lastIdx := 0, len(h.DiffLines)
						if len(h.DiffLines) > 160 && !blockui.IsProbeEnabled() {
							vpH := previewBounds.H
							if vpH <= 0 {
								vpH = 900
							}
							scrollY := max(h.previewScroll.Y, 0)
							firstIdx = max(int((scrollY-400)/estDiffRowPitch), 0)
							lastIdx = min(int((scrollY+vpH+400)/estDiffRowPitch)+1, len(h.DiffLines))
							if firstIdx >= lastIdx {
								firstIdx = max(lastIdx-1, 0)
							}
						}
						if firstIdx > 0 {
							ui.Box(c.Key("hist-diff-top")).FillWidth().Height(max(float32(firstIdx)*estDiffRowPitch-8, 0))
						}
						for ix := firstIdx; ix < lastIdx; ix++ {
							dl := h.DiffLines[ix]
							c.Key(fmt.Sprintf("hist-diff-%d", ix))
							prefix := "  "
							bg := ui.Transparent
							fg := pal.Text
							switch dl.Kind {
							case model.DiffInsert:
								prefix = "+ "
								bg = pal.DiffAddBG
								fg = pal.DiffAddFG
							case model.DiffDelete:
								prefix = "- "
								bg = pal.DiffDelBG
								fg = pal.DiffDelFG
							}
							row := ui.Row(c).FillWidth().MinWidth(0).Padding(3, 10, 3, 10).Radius(theme.RadiusXS).Background(bg)
							row.Children(func() {
								ui.Text(c, prefix+dl.Text).Font(codeFont).FontSize(theme.FontBody).LineHeight(1.5).TextColor(fg).Grow(1).MinWidth(0)
							})
						}
						if rem := len(h.DiffLines) - lastIdx; rem > 0 {
							ui.Box(c.Key("hist-diff-bot")).FillWidth().Height(max(float32(rem)*estDiffRowPitch-8, 0))
						}
					})
				})
			})

			// Right Timeline Sidebar.
			timelineW := float32(268)
			if narrowHist {
				timelineW = 220
			}
			c.Key("history-timeline")
			side := ui.Column(c).Width(timelineW).Shrink(0).FillHeight().Background(pal.Sidebar).
				BorderWidth(0, 0, 0, 1).BorderColor(pal.SidebarBorder).
				Transition(blockui.DockedPanelTransition(true))
			blockui.Probe(side, "history-timeline")
			side.Children(func() {
				ui.Row(c).FillWidth().Height(38).PaddingX(14).AlignItems(ui.Center).Justify(ui.SpaceBetween).
					BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
					ui.Text(c, fmt.Sprintf("%s (%d)", theme.Tr("历史版本", "Snapshots"), len(h.Entries))).
						FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text)
				})
				blockui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Padding(8).Children(func() {
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						for ix, entry := range h.Entries {
							entry := entry
							sel := entry.ID == h.SelectedID
							rowID := fmt.Sprintf("history-snap-%d", ix)
							c.Key(rowID)
							lbl := entry.Summary
							if strings.TrimSpace(lbl) == "" {
								lbl = fmt.Sprintf("%d B", entry.Size)
							}
							btn := ui.ButtonBase(c).FillWidth().Padding(8, 10, 8, 10).Radius(theme.RadiusMD).
								Cursor(ui.CursorPointer).Label(lbl).
								Transition(blockui.CascadeItemTransition(ix, true))
							rowHover := btn.Hovered()
							rowPress := btn.Pressed()
							rowBG := ui.Transparent
							rowBD := ui.Transparent
							switch {
							case sel:
								rowBG = pal.Selected
								rowBD = pal.SelectedBorder
							case rowPress:
								rowBG = pal.Selected
							case rowHover:
								rowBG = pal.Hover
							}
							blockui.AnimateSurfaceColors(btn, rowBG, rowBD, true)
							blockui.TactileCard(btn, rowHover, rowPress, sel, false)
							btn.Children(func() {
								col := ui.Column(c).FillWidth().Gap(3)
								col.Children(func() {
									ts := formatSnapshotTime(entry.Timestamp, "01-02 15:04:05")
									ui.Text(c, ts).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
									ui.Text(c, lbl).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
								})
							})
							blockui.Probe(btn, rowID)
							if btn.Clicked() && onSelect != nil {
								onSelect(entry.ID)
							}
						}
					})
				})
			})
		})
	})

	// 3. Restore confirmation modal overlay.
	title := fmt.Sprintf(theme.Tr("恢复到「%s」？", `Restore to "%s"?`), h.selectedTimestamp())
	blockui.ModalCard(c, "history-confirm-modal", title, &h.ConfirmOpen, 400, 0, func() {
		ui.Column(c).FillWidth().Padding(20).Gap(14).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, title).FontSize(theme.FontTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text)
				btn := blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{ID: "history-restore-close", Label: theme.Tr("关闭", "Close")})
				if btn.Clicked() {
					h.ConfirmOpen = false
				}
			})
			ui.Text(c, theme.Tr("当前内容会保留在历史中。", "Current content will be retained in history.")).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				cancelBtn := blockui.BtnCancel(c, "history-cancel-restore", blockui.PresetOptions{})
				if cancelBtn.Clicked() {
					h.ConfirmOpen = false
				}
				histIc := icons.History
				okBtn := blockui.ActionButton(c, "history-confirm-restore", theme.Tr("确认恢复", "Restore"), &histIc, blockui.ButtonPrimary, false)
				if okBtn.Clicked() {
					snapID := h.SelectedID
					txt := h.SelectedText
					h.ConfirmOpen = false
					if onConfirmRestore != nil {
						onConfirmRestore(snapID, txt)
					}
				}
			})
		})
	})
}

func renderHistoryCleanMarkdown(c *ui.Context, rawText, edFont, codeFont string) {
	renderHistoryCleanDoc(c, markdown.ParseMarkdown(rawText), edFont, codeFont)
}

func renderHistoryCleanDoc(c *ui.Context, doc *model.Doc, edFont, codeFont string) {
	pal := theme.Ocean()
	if doc == nil || len(doc.Blocks) == 0 {
		ui.Text(c, theme.Tr("（空笔记）", "(Empty note)")).FontSize(theme.FontTitle).TextColor(pal.TextSecondary)
		return
	}
	for ix, b := range doc.Blocks {
		c.Key(fmt.Sprintf("hist-clean-blk-%d", ix))
		switch b.Kind {
		case model.BlockDivider:
			ui.Box(c).FillWidth().Height(1).MarginY(8).Background(pal.Hairline)
		case model.BlockCode, model.BlockRawHTML:
			ui.Box(c).FillWidth().Padding(10, 14).Radius(theme.RadiusSM).Background(pal.CodeBlockBG).Children(func() {
				ui.Text(c, b.Code).Font(codeFont).FontSize(theme.FontBody).LineHeight(1.5).TextColor(pal.Text)
			})
		case model.BlockTable:
			if b.Table != nil && len(b.Table.Rows) > 0 {
				ui.Column(c).FillWidth().Radius(theme.RadiusSM).Border(1, pal.Hairline).Clip().Children(func() {
					for rIdx, r := range b.Table.Rows {
						rowEl := ui.Row(c).FillWidth()
						if rIdx+1 < len(b.Table.Rows) {
							rowEl.BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
						}
						if rIdx == 0 {
							rowEl.Background(pal.CodeBlockBG)
						}
						rowEl.Children(func() {
							for _, cell := range r {
								ui.Box(c).Grow(1).MinWidth(0).Padding(6, 10).Children(func() {
									t := ui.Text(c, cell.PlainText()).FontSize(theme.FontBody).TextColor(pal.Text)
									if rIdx == 0 {
										t.FontWeight(theme.WeightTitle)
									}
								})
							}
						})
					}
				})
			}
		case model.BlockQuote, model.BlockCallout:
			txt := b.PlainText()
			if b.Kind == model.BlockCallout && b.Callout != "" {
				txt = fmt.Sprintf("[%s] %s", b.Callout, txt)
			}
			ui.Box(c).FillWidth().Padding(6, 12, 6, 14).BorderWidth(0, 0, 0, 3).BorderColor(pal.Accent).Background(pal.CodeBlockBG).Radius(theme.RadiusXS).Children(func() {
				t := ui.Text(c, txt).FontSize(theme.FontTitle).LineHeight(1.6).TextColor(pal.Text)
				if edFont != "" {
					t.Font(edFont)
				}
			})
		default:
			fs, fw := float32(16), 400
			switch b.Kind {
			case model.BlockHeading1:
				fs, fw = 28, 700
			case model.BlockHeading2:
				fs, fw = 22, 650
			case model.BlockHeading3:
				fs, fw = 19, 600
			case model.BlockHeading4, model.BlockHeading5, model.BlockHeading6:
				fs, fw = 17, 600
			}
			prefix := ""
			switch b.Kind {
			case model.BlockBullet:
				prefix = "• "
			case model.BlockNumbered:
				prefix = "1. "
			case model.BlockTodo:
				if b.Checked {
					prefix = "☑ "
				} else {
					prefix = "☐ "
				}
			}
			txt := prefix + b.PlainText()
			if strings.TrimSpace(txt) == "" {
				ui.Box(c).Height(10)
			} else {
				t := ui.Text(c, txt).FontSize(fs).FontWeight(fw).LineHeight(1.55).TextColor(pal.Text)
				if edFont != "" {
					t.Font(edFont)
				}
			}
		}
	}
}

// RenderBanner renders the post-restore undo toast at the top of the main note column.
func (t *HistoryToast) RenderBanner(c *ui.Context, onUndo func(), onDismiss func()) {
	if t == nil || !t.Active {
		return
	}
	pal := theme.Ocean()
	c.Key("history-toast")
	row := ui.Row(c).FillWidth().Margin(12, 28, 0, 28).Padding(10, 12, 10, 14).
		Radius(theme.RadiusMD).Background(pal.Selected).Border(1, ui.Transparent).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12)
	blockui.Probe(row, "history-toast")
	row.Children(func() {
		ui.Text(c, t.Message).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
			if t.CanUndo {
				undoIc := icons.TbReset
				undoBtn := blockui.ActionButton(c, "history-toast-undo", theme.Tr("撤销恢复", "Undo Restore"), &undoIc, blockui.ButtonSecondary, false)
				if undoBtn.Clicked() && onUndo != nil {
					onUndo()
				}
			}
			closeBtn := blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{
				ID:       "history-toast-dismiss",
				Size:     26,
				IconSize: 14,
				Tooltip:  theme.Tr("关闭提示", "Dismiss"),
			})
			if closeBtn.Clicked() && onDismiss != nil {
				onDismiss()
			}
		})
	})
}
