package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
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
	SavedText     string
	BlockedReason string
	StatusMsg     string

	SVGShowSource bool
	Zoom          float32
	ZoomFitMode   bool

	ImgSrc    ui.ImageSource
	ImgLoaded bool
	ImgErr    string
	SVGSrc    *ui.SVG
	SVGText   string
}

// NewTabSurface constructs a TabSurface from a ClassifiedFile.
func NewTabSurface(path string, cf model.ClassifiedFile) *TabSurface {
	var src model.SourceFile
	hasSource := cf.Source != nil
	if hasSource {
		src = *cf.Source
	}
	isSVG := files.IsSVGPath(path) || src.SVG
	isHTML := src.HTML || strings.EqualFold(src.Language, "html")
	s := &TabSurface{
		Path:          path,
		Kind:          cf.Kind,
		Source:        src,
		HasSource:     hasSource,
		IsSVG:         isSVG,
		IsHTML:        isHTML,
		SavedText:     src.Text,
		BlockedReason: cf.Reason,
		Zoom:          1.0,
		ZoomFitMode:   true,
	}
	if isSVG {
		if !hasSource {
			if raw, err := os.ReadFile(path); err == nil {
				s.Source.Text = string(raw)
			}
		}
		s.reparseSVG()
	}
	return s
}

func (s *TabSurface) reparseSVG() {
	if s.SVGText == s.Source.Text && s.SVGSrc != nil {
		return
	}
	s.SVGText = s.Source.Text
	parsed, err := ui.ParseSVG([]byte(s.Source.Text))
	if err == nil {
		s.SVGSrc = parsed
	} else {
		s.SVGSrc = nil
	}
}

func (s *TabSurface) ensureImageLoaded() {
	if s.ImgLoaded {
		return
	}
	s.ImgLoaded = true
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		s.ImgErr = err.Error()
		return
	}
	src, err := ui.DecodeImage(raw)
	if err != nil {
		s.ImgErr = err.Error()
		return
	}
	s.ImgSrc = src
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
	return fmt.Sprintf("%d%%", int(s.Zoom*100+0.5))
}

// ZoomStep multiplies preview zoom and exits Fit mode.
func (s *TabSurface) ZoomStep(factor float32) {
	if s == nil {
		return
	}
	z := s.Zoom * factor
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

// SourceStatusLabel returns the compact language + encoding label for titlebar display.
func (s *TabSurface) SourceStatusLabel() string {
	if s == nil {
		return ""
	}
	if s.StatusMsg != "" {
		return s.StatusMsg
	}
	if s.Kind == model.OpenKindPreview && s.IsSVG && !s.HasSource {
		return theme.Tr("SVG · 只读预览", "SVG · Read-only")
	}
	lang := strings.ToUpper(strings.TrimSpace(s.Source.Language))
	if s.IsSVG {
		lang = "SVG"
	} else if lang == "" {
		ext := strings.TrimPrefix(filepath.Ext(s.Path), ".")
		if ext != "" {
			lang = strings.ToUpper(ext)
		} else {
			lang = "TEXT"
		}
	}
	enc := "UTF-8"
	if s.Source.BOM {
		enc = "UTF-8 BOM"
	}
	return fmt.Sprintf("%s · %s", lang, enc)
}

// Render draws the non-Markdown file surface inside the main workspace column.
func (s *TabSurface) Render(
	c *ui.Context,
	onSave func(path, text string, bom bool) error,
	onReveal func(path string),
	onOpenExternal func(path string),
) {
	pal := theme.Ocean()
	switch {
	case s.Kind == model.OpenKindBlocked:
		c.Key("blocked-file-view")
		box := ui.Column(c).Fill().Padding(32).Center().Gap(14).Background(pal.Paper)
		blockui.Probe(box, "blocked-file-view")
		box.Children(func() {
			icons.Mascot(c, icons.MascotReadError, 128)
			ui.Text(c, filepath.Base(s.Path)).FontSize(16).FontWeight(600).TextColor(pal.Text)
			reason := s.BlockedReason
			if reason == "" {
				reason = theme.Tr("此文件为二进制或超出编辑器大小限制，无法作为文本打开。", "This file is binary or exceeds the editor size limit.")
			}
			ui.Text(c, reason).FontSize(14).TextColor(pal.TextSecondary).MaxWidth(440)
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				icRev := icons.FolderReveal
				revBtn := blockui.ActionButton(c, "blocked-reveal", theme.Tr("在访达中显示", "Reveal in File Manager"), &icRev, blockui.ButtonSecondary, false)
				if revBtn.Clicked() && onReveal != nil {
					onReveal(s.Path)
				}
				icExt := icons.LinkOpen
				openBtn := blockui.ActionButton(c, "blocked-open-external", theme.Tr("用默认应用打开", "Open in Default App"), &icExt, blockui.ButtonPrimary, false)
				if openBtn.Clicked() && onOpenExternal != nil {
					onOpenExternal(s.Path)
				}
			})
		})

	case s.IsSVG:
		if !s.SVGShowSource || !s.HasSource {
			s.reparseSVG()
			c.Key("svg-preview-stage")
			stage := ui.Scroll(c).Fill().Background(pal.Shell)
			blockui.Probe(stage, "svg-preview-stage")
			stage.Children(func() {
				vpW, vpH := c.Size()
				ui.Column(c).FillWidth().MinHeight(max(vpH-80, 240)).Padding(28).Center().Gap(12).Children(func() {
					card := ui.Column(c).Padding(24).Radius(theme.RadiusSurface).Background(pal.Paper).
						Border(1, pal.Hairline).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.08)).Center().Gap(10)
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
								maxW := max(vpW-180, 220)
								maxH := max(vpH-200, 180)
								scale := min(float32(2.0), min(maxW/iw, maxH/ih))
								if scale <= 0 {
									scale = 1
								}
								drawW, drawH = iw*scale, ih*scale
							}
							ui.Image(c, s.SVGSrc).Size(max(drawW, 32), max(drawH, 32))
						} else {
							icons.Duo(c, icons.Code, 42, true)
							ui.Text(c, theme.Tr("SVG 预览不可用，可切换到源码编辑", "SVG preview unavailable — switch to Source")).
								FontSize(14).TextColor(pal.TextSecondary)
						}
					})
				})
			})
			return
		}
		s.renderSourceEditor(c, onSave)

	case s.Kind == model.OpenKindPreview:
		s.ensureImageLoaded()
		c.Key("image-preview-stage")
		stage := ui.Scroll(c).Fill().Background(pal.Shell)
		blockui.Probe(stage, "image-preview-stage")
		stage.Children(func() {
			vpW, vpH := c.Size()
			ui.Column(c).FillWidth().MinHeight(max(vpH-80, 240)).Padding(28).Center().Gap(12).Children(func() {
				card := ui.Column(c).Padding(16).Radius(theme.RadiusSurface).Background(pal.Paper).
					Border(1, pal.Hairline).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.08)).Center().Gap(10)
				card.Children(func() {
					if s.ImgSrc != nil {
						iw, ih := ui.ImageSize(s.ImgSrc)
						if iw <= 0 {
							iw = 320
						}
						if ih <= 0 {
							ih = 240
						}
						drawW, drawH := iw*s.Zoom, ih*s.Zoom
						if s.ZoomFitMode {
							maxW := max(vpW-160, 240)
							maxH := max(vpH-180, 180)
							scale := min(float32(1.0), min(maxW/iw, maxH/ih))
							drawW, drawH = iw*scale, ih*scale
						}
						ui.Image(c, s.ImgSrc).Size(max(drawW, 24), max(drawH, 24))
						ui.Text(c, fmt.Sprintf("%s · %dx%d", filepath.Base(s.Path), int(iw), int(ih))).
							FontSize(14).TextColor(pal.TextSecondary)
					} else {
						icons.Duo(c, icons.Image, 48, true)
						ui.Text(c, filepath.Base(s.Path)).FontSize(15).FontWeight(500).TextColor(pal.Text)
						if s.ImgErr != "" {
							ui.Text(c, s.ImgErr).FontSize(14).TextColor(pal.TextSecondary)
						}
					}
				})
			})
		})

	default: // OpenKindSource (HTML, JSON, code, plain text)
		s.renderSourceEditor(c, onSave)
	}
}

func (s *TabSurface) renderSourceEditor(c *ui.Context, onSave func(path, text string, bom bool) error) {
	pal := theme.Ocean()
	c.Key("source-surface:" + s.Path)
	outer := ui.Column(c).Fill().Background(pal.Paper)
	blockui.Probe(outer, "source-surface")
	_, codeFont, _ := theme.Typography()
	if strings.TrimSpace(codeFont) == "" {
		codeFont = "monospace"
	}
	outer.Children(func() {
		lineCount := strings.Count(s.Source.Text, "\n") + 1
		c.Key("source-scroll:" + s.Path)
		scroll := ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Padding(20, 28, 36, 20)
		scroll.Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(14).Children(func() {
				gutter := ui.Column(c).Width(42).Shrink(0).AlignItems(ui.End).Padding(8, 6, 8, 0).
					BorderWidth(0, 1, 0, 0).BorderColor(pal.Hairline)
				gutter.Children(func() {
					maxLines := min(lineCount, 400)
					var sb strings.Builder
					for i := 1; i <= maxLines; i++ {
						if i > 1 {
							sb.WriteByte('\n')
						}
						fmt.Fprintf(&sb, "%d", i)
					}
					ui.Text(c, sb.String()).Font(codeFont).FontSize(14).LineHeight(1.55).
						TextColor(pal.TextSecondary).AccessHidden(true)
				})

				prev := s.Source.Text
				c.Key("source-editor:" + s.Path)
				ta := ui.TextAreaBase(c, &s.Source.Text).Grow(1).MinWidth(0).MinHeight(360).Padding(8).
					Font(codeFont).FontSize(14).LineHeight(1.55).TextColor(pal.Text).
					Background(ui.Transparent).BorderWidth(0)
				blockui.Probe(ta, "source-editor")
				if s.Source.Text != prev {
					if s.IsSVG {
						s.reparseSVG()
					}
					if onSave != nil {
						if err := onSave(s.Path, s.Source.Text, s.Source.BOM); err != nil {
							s.StatusMsg = err.Error()
						} else {
							s.SavedText = s.Source.Text
							s.StatusMsg = ""
						}
					}
				}
			})
		})
	})
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
}

// HistoryToast represents the undoable banner shown at the top of the note after restoring a snapshot.
type HistoryToast struct {
	Active      bool
	NotePath    string
	Message     string
	UndoContent string
	CanUndo     bool
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
	if info, err := os.Stat(notePath); err == nil && info.Mode().Perm()&0o200 == 0 {
		h.IsReadOnly = true
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
	if len(h.Entries) > 0 {
		h.SelectVersion(h.Entries[0].ID, loadSnap, diffFn)
	}
}

// CheckExternalConflict checks whether the underlying file on disk changed since Page History was opened.
func (h *PageHistoryView) CheckExternalConflict() bool {
	if !h.Open || h.NotePath == "" {
		return false
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
	leftLead := float32(12)
	if theme.IsMacOS() {
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

			center := ui.Row(c).Grow(1).MinWidth(16).FillHeight().PaddingX(16).AlignItems(ui.Center).
				Justify(ui.SpaceBetween).Gap(12).DragWindow()
			blockui.Probe(center, "history-titlebar-drag")
			center.Children(func() {
				noteName := noteDisplayTitle(h.NotePath, false)
				pill := ui.Row(c).Height(28).PaddingX(10).MaxWidth(360).AlignItems(ui.Center).
					Radius(theme.RadiusControl).Background(pal.Selected).Border(1, pal.Accent)
				pill.Children(func() {
					ui.Text(c, fmt.Sprintf("%s · %s", noteName, theme.Tr("页面历史", "Page History"))).
						FontSize(14).FontWeight(500).TextColor(pal.Text).SingleLine().Ellipsis("…")
				})
				ts := h.selectedTimestamp()
				if ts != "" {
					ui.Text(c, ts).FontSize(14).FontWeight(500).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
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
				resBtn := blockui.ActionButton(
					c,
					"history-restore",
					theme.Tr("恢复此版本", "Restore Version"),
					nil,
					blockui.ButtonPrimary,
					!canRestore || h.ConfirmOpen,
				)
				resBtn.Tooltip(restoreHint)
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
					ui.Row(c).FillWidth().Margin(12, 28, 0, 28).Padding(10, 12, 10, 14).
						Radius(theme.RadiusControl).Background(pal.Shell).Border(1, pal.Hairline).
						AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).Children(func() {
						ui.Text(c, theme.Tr("页面已更新，请重新对比。", "Page has been updated externally; please re-diff.")).
							FontSize(14).TextColor(pal.TextSecondary)
						reBtn := blockui.ActionButton(c, "history-error-action", theme.Tr("重新对比", "Re-diff"), nil, blockui.ButtonSecondary, false)
						if reBtn.Clicked() && onRetry != nil {
							onRetry()
						}
					})
				}

				previewCol := ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Padding(28, 36, 40, 36).Background(pal.Paper)
				blockui.Probe(previewCol, "history-preview")
				edFont, codeFont, _ := theme.Typography()
				if strings.TrimSpace(codeFont) == "" {
					codeFont = "monospace"
				}
				previewCol.Children(func() {
					inner := ui.Column(c).FillWidth().MaxWidth(672).Gap(8).MarginX(0)
					inner.Children(func() {
						if len(h.Entries) == 0 {
							ui.Column(c).FillWidth().PaddingY(48).Center().Gap(10).Children(func() {
								icons.Duo(c, icons.History, 36, true)
								ui.Text(c, theme.Tr("当前笔记还没有历史版本快照（保存修改后自动生成）", "No history snapshots recorded for this note yet.")).
									FontSize(14).TextColor(pal.TextSecondary)
							})
							return
						}
						if h.ErrorMsg != "" {
							ui.Column(c).FillWidth().Padding(16).Radius(theme.RadiusControl).Background(pal.DangerSurface).Gap(8).Children(func() {
								ui.Text(c, h.ErrorMsg).FontSize(14).TextColor(pal.Danger)
								retryBtn := blockui.ActionButton(c, "history-retry", theme.Tr("重试", "Retry"), nil, blockui.ButtonSecondary, false)
								if retryBtn.Clicked() && onRetry != nil {
									onRetry()
								}
							})
							return
						}
						if h.Mode == "clean" {
							renderHistoryCleanMarkdown(c, h.SelectedText, edFont, codeFont)
							return
						}
						for ix, dl := range h.DiffLines {
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
							row := ui.Row(c).FillWidth().Padding(3, 10, 3, 10).Radius(4).Background(bg)
							row.Children(func() {
								ui.Text(c, prefix+dl.Text).Font(codeFont).FontSize(14).LineHeight(1.5).TextColor(fg)
							})
						}
					})
				})
			})

			// Right Timeline Sidebar.
			side := ui.Column(c).Width(268).Shrink(0).FillHeight().Background(pal.Sidebar).
				BorderWidth(0, 0, 0, 1).BorderColor(pal.SidebarBorder)
			blockui.Probe(side, "history-timeline")
			side.Children(func() {
				ui.Row(c).FillWidth().Height(38).PaddingX(14).AlignItems(ui.Center).Justify(ui.SpaceBetween).
					BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
					ui.Text(c, fmt.Sprintf("%s (%d)", theme.Tr("历史版本", "Snapshots"), len(h.Entries))).
						FontSize(14).FontWeight(600).TextColor(pal.Text)
				})
				ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Padding(8).Children(func() {
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
							btn := ui.ButtonBase(c).FillWidth().Padding(8, 10, 8, 10).Radius(theme.RadiusControl).
								Cursor(ui.CursorPointer).Label(lbl)
							switch {
							case sel:
								btn.Background(pal.Selected).Border(1, pal.Accent)
							case btn.Hovered():
								btn.Background(pal.Hover).Border(1, ui.Transparent)
							default:
								btn.Border(1, ui.Transparent)
							}
							btn.Children(func() {
								col := ui.Column(c).FillWidth().Gap(3)
								col.Children(func() {
									ts := formatSnapshotTime(entry.Timestamp, "01-02 15:04:05")
									ui.Text(c, ts).FontSize(14).FontWeight(500).TextColor(pal.Text).SingleLine()
									ui.Text(c, lbl).FontSize(14).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
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
	if h.ConfirmOpen {
		ui.Overlay(c, func() {
			scrim := ui.Column(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().
				Background(ui.RGBA(0, 0, 0, 0.36))
			var card ui.Element
			scrim.Children(func() {
				c.Key("history-confirm-modal")
				card = ui.Column(c).Width(400).Padding(20).Gap(14).Radius(theme.RadiusSurface).
					Background(pal.Paper).Border(1, pal.Hairline).Shadow(0, 14, 36, 0, ui.RGBA(0, 0, 0, 0.22))
				blockui.Probe(card, "history-confirm-modal")
				card.Children(func() {
					ui.Text(c, theme.Tr("确认恢复此历史版本？", "Restore this snapshot?")).
						FontSize(16).FontWeight(600).TextColor(pal.Text)
					ui.Text(c, fmt.Sprintf("%s (%s)", theme.Tr("当前笔记内容将被替换为所选版本，恢复前会自动保存一份当前快照。", "Current note content will be replaced; a snapshot of the current text is saved first."), h.selectedTimestamp())).
						FontSize(14).LineHeight(1.5).TextColor(pal.TextSecondary)
					ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
						cancelBtn := blockui.ActionButton(c, "history-cancel-restore", theme.Tr("取消", "Cancel"), nil, blockui.ButtonSecondary, false)
						if cancelBtn.Clicked() {
							h.ConfirmOpen = false
						}
						okBtn := blockui.ActionButton(c, "history-confirm-restore", theme.Tr("确认恢复", "Restore"), nil, blockui.ButtonPrimary, false)
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
			if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
				h.ConfirmOpen = false
			}
		})
	}
}

func renderHistoryCleanMarkdown(c *ui.Context, rawText, edFont, codeFont string) {
	pal := theme.Ocean()
	doc := markdown.ParseMarkdown(rawText)
	if doc == nil || len(doc.Blocks) == 0 {
		ui.Text(c, theme.Tr("（空笔记）", "(Empty note)")).FontSize(15).TextColor(pal.TextSecondary)
		return
	}
	for ix, b := range doc.Blocks {
		c.Key(fmt.Sprintf("hist-clean-blk-%d", ix))
		switch b.Kind {
		case model.BlockDivider:
			ui.Box(c).FillWidth().Height(1).MarginY(8).Background(pal.Hairline)
		case model.BlockCode, model.BlockRawHTML:
			ui.Box(c).FillWidth().Padding(10, 14).Radius(theme.RadiusControl).Background(pal.CodeBG).Border(1, pal.Hairline).Children(func() {
				ui.Text(c, b.Code).Font(codeFont).FontSize(14).LineHeight(1.5).TextColor(pal.Text)
			})
		case model.BlockTable:
			if b.Table != nil && len(b.Table.Rows) > 0 {
				ui.Column(c).FillWidth().Radius(6).Border(1, pal.Hairline).Clip().Children(func() {
					for rIdx, r := range b.Table.Rows {
						rowEl := ui.Row(c).FillWidth()
						if rIdx+1 < len(b.Table.Rows) {
							rowEl.BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
						}
						if rIdx == 0 {
							rowEl.Background(pal.Shell)
						}
						rowEl.Children(func() {
							for _, cell := range r {
								ui.Box(c).Grow(1).Padding(6, 10).Children(func() {
									t := ui.Text(c, cell.PlainText()).FontSize(14).TextColor(pal.Text)
									if rIdx == 0 {
										t.FontWeight(600)
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
			ui.Box(c).FillWidth().Padding(6, 12, 6, 14).BorderWidth(0, 0, 0, 3).BorderColor(pal.Accent).Background(pal.Shell).Radius(4).Children(func() {
				t := ui.Text(c, txt).FontSize(15).LineHeight(1.6).TextColor(pal.Text)
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
		Radius(theme.RadiusControl).Background(pal.Selected).Border(1, pal.Accent).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12)
	blockui.Probe(row, "history-toast")
	row.Children(func() {
		ui.Text(c, t.Message).FontSize(14).FontWeight(500).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
			if t.CanUndo {
				undoBtn := blockui.ActionButton(c, "history-toast-undo", theme.Tr("撤销恢复", "Undo Restore"), nil, blockui.ButtonSecondary, false)
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
