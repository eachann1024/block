package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"block/internal/icons"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

const (
	exportPrefsFilename = "export-preferences.json"
	optionsSidebarWidth = float32(320)
	previewPadX         = float32(32)
	previewPadTop       = float32(36)
	previewPadBottom    = float32(36)
	minFitScale         = float32(0.75)
	minZoomScale        = float32(0.5)
	maxZoomScale        = float32(1.5)
	exportPageWidth     = float32(816)
	exportRasterScale   = float32(2)
)

type persistedPrefs struct {
	Format          model.ExportFormat `json:"format"`
	Style           model.ExportTheme  `json:"style"`
	LightStyle      model.ExportTheme  `json:"light_style"`
	DarkStyle       model.ExportTheme  `json:"dark_style"`
	ShowTitle       *bool              `json:"show_title"`
	Watermark       *bool              `json:"watermark"`
	ExportDirectory string             `json:"export_directory,omitempty"`
}

// Controller manages the full-window A-Export preview stage, preferences, and multi-format export actions.
type Controller struct {
	mu       sync.Mutex
	dataDir  string
	onRedraw func()

	open         bool
	title        string
	notePath     string
	wholeDoc     *model.Doc
	selectionDoc *model.Doc
	useSelection bool

	format     model.ExportFormat
	style      model.ExportTheme
	lightStyle model.ExportTheme
	darkStyle  model.ExportTheme
	showTitle  bool
	watermark  bool
	exportDir  string

	zoom         float32 // 0 means Fit
	lastFitScale float32

	previewPNG    []byte
	previewBitmap *ui.Bitmap
	previewW      int
	previewH      int
	previewErr    string
	previewRev    uint64
	renderedRev   uint64

	notice    string
	savedPath string
	copied    bool

	services    ui.Services
	hasServices bool

	// Optional test hook for interactive save dialog when mygo.App is not running.
	OnSaveDialog func(dir, suggested string, done func(string))
}

// NewController creates a new full-window export preview controller backed by dataDir.
func NewController(dataDir string, onRedraw func()) *Controller {
	c := &Controller{
		dataDir:      strings.TrimSpace(dataDir),
		onRedraw:     onRedraw,
		format:       model.ExportPNG,
		style:        model.ExportThemeMono,
		lightStyle:   model.ExportThemeMono,
		darkStyle:    model.ExportThemeInk,
		showTitle:    true,
		watermark:    true,
		lastFitScale: 1.0,
	}
	c.loadPreferencesLocked()
	return c
}

func (c *Controller) loadPreferencesLocked() {
	if c.dataDir == "" {
		return
	}
	path := filepath.Join(c.dataDir, exportPrefsFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var p persistedPrefs
	if err := json.Unmarshal(data, &p); err != nil {
		return
	}
	if isSupportedFormat(p.Format) {
		c.format = p.Format
	}
	if p.ShowTitle != nil {
		c.showTitle = *p.ShowTitle
	}
	if p.Watermark != nil {
		c.watermark = *p.Watermark
	}
	c.lightStyle = NormalizeTheme(p.LightStyle)
	if c.lightStyle.IsDark() {
		c.lightStyle = DefaultThemeFor(false)
	}
	c.darkStyle = NormalizeTheme(p.DarkStyle)
	if !c.darkStyle.IsDark() {
		c.darkStyle = DefaultThemeFor(true)
	}
	if p.Style != "" {
		c.style = NormalizeTheme(p.Style)
	}
	if c.style.IsDark() {
		c.darkStyle = c.style
	} else {
		c.lightStyle = c.style
	}
	c.exportDir = strings.TrimSpace(p.ExportDirectory)
}

func (c *Controller) savePreferencesLocked() {
	if c.dataDir == "" {
		return
	}
	showTitle := c.showTitle
	watermark := c.watermark
	p := persistedPrefs{
		Format:          c.format,
		Style:           c.style,
		LightStyle:      c.lightStyle,
		DarkStyle:       c.darkStyle,
		ShowTitle:       &showTitle,
		Watermark:       &watermark,
		ExportDirectory: c.exportDir,
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.dataDir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(c.dataDir, ".export-prefs-*.tmp")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	if err := os.Rename(tmpPath, filepath.Join(c.dataDir, exportPrefsFilename)); err != nil {
		return
	}
	cleanup = false
}

func (c *Controller) notifyRedraw() {
	if c.onRedraw != nil {
		c.onRedraw()
	}
}

func (c *Controller) IsOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.open
}

func (c *Controller) Open(title, notePath string, wholeDoc *model.Doc, selectionDoc *model.Doc) {
	c.mu.Lock()
	c.loadPreferencesLocked()
	c.open = true
	t := strings.TrimSpace(title)
	if t == "" && wholeDoc != nil {
		t = strings.TrimSpace(wholeDoc.Title())
	}
	if t == "" && notePath != "" {
		base := filepath.Base(notePath)
		t = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if t == "" {
		t = theme.Tr("未命名", "Untitled")
	}
	c.title = t
	c.notePath = notePath
	if wholeDoc != nil {
		c.wholeDoc = wholeDoc.Clone()
	} else {
		c.wholeDoc = nil
	}
	if selectionDoc != nil && len(selectionDoc.Blocks) > 0 {
		c.selectionDoc = selectionDoc.Clone()
	} else {
		c.selectionDoc = nil
	}
	c.useSelection = false
	c.zoom = 0
	c.notice = ""
	c.savedPath = ""
	c.copied = false
	c.previewRev++
	c.refreshPreviewLocked()
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) Close() {
	c.mu.Lock()
	if !c.open {
		c.mu.Unlock()
		return
	}
	c.open = false
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) ActiveFormat() model.ExportFormat {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.format
}

func (c *Controller) SetFormat(f model.ExportFormat) {
	if !isSupportedFormat(f) {
		return
	}
	c.mu.Lock()
	if c.format == f {
		c.mu.Unlock()
		return
	}
	wmChanged := c.watermark && formatSupportsWatermark(f) != formatSupportsWatermark(c.format)
	c.format = f
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.savePreferencesLocked()
	if wmChanged || c.previewBitmap == nil {
		c.previewRev++
		c.refreshPreviewLocked()
	}
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) ActiveStyle() model.ExportTheme {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.style
}

func (c *Controller) SetStyle(s model.ExportTheme) {
	s = NormalizeTheme(s)
	c.mu.Lock()
	if c.style == s {
		c.mu.Unlock()
		return
	}
	c.style = s
	if s.IsDark() {
		c.darkStyle = s
	} else {
		c.lightStyle = s
	}
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.savePreferencesLocked()
	c.previewRev++
	c.refreshPreviewLocked()
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) setDarkGroup(dark bool) {
	c.mu.Lock()
	if dark == c.style.IsDark() {
		c.mu.Unlock()
		return
	}
	next := c.lightStyle
	if dark {
		next = c.darkStyle
	}
	next = NormalizeTheme(next)
	if next.IsDark() != dark {
		next = DefaultThemeFor(dark)
	}
	c.style = next
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.savePreferencesLocked()
	c.previewRev++
	c.refreshPreviewLocked()
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) UseSelection() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.useSelection
}

func (c *Controller) SetUseSelection(sel bool) {
	c.mu.Lock()
	if sel && c.selectionDoc == nil {
		c.mu.Unlock()
		return
	}
	if c.useSelection == sel {
		c.mu.Unlock()
		return
	}
	c.useSelection = sel
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.previewRev++
	c.refreshPreviewLocked()
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) ShowTitle() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.showTitle
}

func (c *Controller) ToggleShowTitle() {
	c.mu.Lock()
	c.showTitle = !c.showTitle
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.savePreferencesLocked()
	c.previewRev++
	c.refreshPreviewLocked()
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) Watermark() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.watermark
}

func (c *Controller) ToggleWatermark() {
	c.mu.Lock()
	c.watermark = !c.watermark
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.savePreferencesLocked()
	c.previewRev++
	c.refreshPreviewLocked()
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) ZoomIn() {
	c.mu.Lock()
	base := c.zoom
	if base <= 0 {
		base = c.lastFitScale
	}
	if base <= 0 {
		base = 1.0
	}
	next := float32(math.Round(float64((base+0.15)*20))) / 20
	if next < minZoomScale {
		next = minZoomScale
	}
	if next > maxZoomScale {
		next = maxZoomScale
	}
	c.zoom = next
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) ZoomOut() {
	c.mu.Lock()
	base := c.zoom
	if base <= 0 {
		base = c.lastFitScale
	}
	if base <= 0 {
		base = 1.0
	}
	next := float32(math.Round(float64((base-0.15)*20))) / 20
	if next < minZoomScale {
		next = minZoomScale
	}
	if next > maxZoomScale {
		next = maxZoomScale
	}
	c.zoom = next
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) ZoomReset() {
	c.mu.Lock()
	c.zoom = 0
	c.mu.Unlock()
	c.notifyRedraw()
}

func (c *Controller) SavedPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.savedPath
}

func (c *Controller) activeDocLocked() *model.Doc {
	if c.useSelection && c.selectionDoc != nil {
		return c.selectionDoc
	}
	return c.wholeDoc
}

func (c *Controller) renderPrefsLocked() model.ExportPreferences {
	showTitle := c.showTitle
	watermark := c.watermark && formatSupportsWatermark(c.format)
	return model.ExportPreferences{
		Format:      c.format,
		Style:       c.style,
		LightStyle:  c.lightStyle,
		DarkStyle:   c.darkStyle,
		ShowTitle:   &showTitle,
		Watermark:   &watermark,
		IncludeMeta: showTitle,
		Width:       int(exportPageWidth),
		Title:       c.title,
	}
}

func (c *Controller) refreshPreviewLocked() {
	doc := c.activeDocLocked()
	if doc == nil {
		c.previewPNG = nil
		c.previewBitmap = nil
		c.previewW = 0
		c.previewH = 0
		c.previewErr = theme.Tr("当前没有可导出的内容", "Nothing to export")
		c.renderedRev = c.previewRev
		return
	}
	prefs := c.renderPrefsLocked()
	pngBytes, err := markdown.ExportDocument(doc, c.notePath, model.ExportPNG, prefs)
	if err != nil {
		c.previewPNG = nil
		c.previewBitmap = nil
		c.previewW = 0
		c.previewH = 0
		c.previewErr = err.Error()
		c.renderedRev = c.previewRev
		return
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		c.previewPNG = pngBytes
		c.previewBitmap = nil
		c.previewW = 0
		c.previewH = 0
		c.previewErr = err.Error()
		c.renderedRev = c.previewRev
		return
	}
	bm := ui.NewBitmap(img)
	w, h := bm.Size()
	c.previewPNG = pngBytes
	c.previewBitmap = bm
	c.previewW = w
	c.previewH = h
	c.previewErr = ""
	c.renderedRev = c.previewRev
}

func (c *Controller) CopyToClipboard() error {
	c.mu.Lock()
	doc := c.activeDocLocked()
	if doc == nil {
		c.mu.Unlock()
		return errors.New(theme.Tr("当前没有可导出的内容", "Nothing to export"))
	}
	format := c.format
	prefs := c.renderPrefsLocked()
	notePath := c.notePath
	if c.renderedRev != c.previewRev || len(c.previewPNG) == 0 {
		c.refreshPreviewLocked()
	}
	pngBytes := append([]byte(nil), c.previewPNG...)
	services := c.services
	hasServices := c.hasServices
	c.mu.Unlock()

	var tData transfer.Data
	switch format {
	case model.ExportMarkdown:
		raw, err := markdown.ExportDocument(doc, notePath, model.ExportMarkdown, prefs)
		if err != nil {
			return err
		}
		tData = transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.Text, raw),
			transfer.Bytes(transfer.Markdown, raw),
		))
	case model.ExportHTML:
		raw, err := markdown.ExportDocument(doc, notePath, model.ExportHTML, prefs)
		if err != nil {
			return err
		}
		tData = transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.Text, raw),
			transfer.Bytes(transfer.HTML, raw),
		))
	default:
		if len(pngBytes) == 0 {
			var err error
			pngBytes, err = markdown.ExportDocument(doc, notePath, model.ExportPNG, prefs)
			if err != nil {
				return err
			}
		}
		tData = transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.PNG, pngBytes),
		))
	}

	if hasServices {
		services.WriteClipboardData(tData)
	}
	if mygo.App.IsReady() {
		if err := mygo.Clipboard.Write(tData); err != nil {
			return err
		}
	}

	c.mu.Lock()
	c.copied = true
	c.notice = theme.Tr("已复制到剪贴板", "Copied to clipboard")
	c.mu.Unlock()
	c.notifyRedraw()
	return nil
}

func (c *Controller) SaveToPath(outPath string) error {
	trimmed := strings.TrimSpace(outPath)
	if trimmed == "" {
		return errors.New("output path cannot be empty")
	}
	c.mu.Lock()
	doc := c.activeDocLocked()
	if doc == nil {
		c.mu.Unlock()
		return errors.New(theme.Tr("当前没有可导出的内容", "Nothing to export"))
	}
	format := c.format
	prefs := c.renderPrefsLocked()
	notePath := c.notePath
	c.mu.Unlock()

	if err := markdown.ExportToFile(doc, notePath, trimmed, format, prefs); err != nil {
		c.mu.Lock()
		c.notice = err.Error()
		c.savedPath = ""
		c.mu.Unlock()
		c.notifyRedraw()
		return err
	}

	absOut, absErr := filepath.Abs(trimmed)
	if absErr == nil {
		trimmed = absOut
	}
	c.mu.Lock()
	c.savedPath = trimmed
	c.exportDir = filepath.Dir(trimmed)
	c.savePreferencesLocked()
	c.notice = fmt.Sprintf("%s %s", theme.Tr("已保存", "Saved"), filepath.Base(trimmed))
	c.mu.Unlock()
	c.notifyRedraw()
	return nil
}

func (c *Controller) triggerSave() {
	c.mu.Lock()
	dir := c.defaultExportDirLocked()
	name := suggestedExportFilename(c.title, c.format)
	hook := c.OnSaveDialog
	format := c.format
	c.mu.Unlock()

	if hook != nil {
		hook(dir, name, func(chosen string) {
			if strings.TrimSpace(chosen) != "" {
				_ = c.SaveToPath(chosen)
			}
		})
		return
	}
	if !mygo.App.IsReady() {
		return
	}
	ext := exportExtension(format)
	go func() {
		chosen, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Title:             saveButtonLabel(format),
			DefaultPath:       filepath.Join(dir, name),
			CreateDirectories: true,
			Filters:           []mygo.FileFilter{{Name: strings.ToUpper(ext), Extensions: []string{ext}}},
		})
		if err == nil && strings.TrimSpace(chosen) != "" {
			_ = c.SaveToPath(chosen)
		}
	}()
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

// Render builds the full-window A-Export preview UI.
func (c *Controller) Render(ctx *ui.Context) {
	c.mu.Lock()
	if !c.open {
		c.mu.Unlock()
		return
	}
	c.services = ctx.Services()
	c.hasServices = true
	if c.renderedRev != c.previewRev {
		c.refreshPreviewLocked()
	}
	c.mu.Unlock()

	// Keyboard shortcuts: Esc close, Cmd+C copy, Cmd+S save, - / = / 0 zoom.
	switch {
	case ctx.Shortcut(0, ui.KeyEscape):
		c.Close()
		return
	case ctx.Shortcut(ui.Cmd, ui.KeyC):
		_ = c.CopyToClipboard()
	case ctx.Shortcut(ui.Cmd, ui.KeyS):
		c.triggerSave()
	case ctx.Shortcut(0, ui.KeyEqual) || ctx.Shortcut(ui.Shift, ui.KeyEqual):
		c.ZoomIn()
	case ctx.Shortcut(0, ui.KeyMinus):
		c.ZoomOut()
	case ctx.Shortcut(0, ui.Key0):
		c.ZoomReset()
	}

	c.mu.Lock()
	title := c.title
	format := c.format
	style := c.style
	useSelection := c.useSelection
	hasSelection := c.selectionDoc != nil
	showTitle := c.showTitle
	watermark := c.watermark
	zoom := c.zoom
	bm := c.previewBitmap
	pw := c.previewW
	ph := c.previewH
	previewErr := c.previewErr
	notice := c.notice
	savedPath := c.savedPath
	copied := c.copied
	c.mu.Unlock()

	pal := theme.Ocean()
	vpW, _ := ctx.Size()
	if vpW <= 0 {
		vpW = 1200
	}
	room := vpW - optionsSidebarWidth - 1 - previewPadX*2
	if room < 200 {
		room = 200
	}
	fitScale := computeFitScale(room, pw)
	c.mu.Lock()
	c.lastFitScale = fitScale
	c.mu.Unlock()
	scale := zoom
	if scale <= 0 {
		scale = fitScale
	}

	ctx.Key("export-view")
	root := ui.Column(ctx).Fill().Background(pal.Shell).TextColor(pal.Text).Label("export-view")
	internalui.Probe(root, "export-view")

	root.Children(func() {
		c.renderHeader(ctx, pal, title, format, style, pw, ph, zoom)
		ui.Row(ctx).FillWidth().Grow(1).MinHeight(0).Children(func() {
			c.renderStage(ctx, pal, bm, pw, ph, scale, previewErr)
			c.renderSidebar(ctx, pal, format, style, useSelection, hasSelection, showTitle, watermark, notice, savedPath, copied)
		})
	})
}

func computeFitScale(room float32, pw int) float32 {
	logicalW := exportPageWidth
	if pw > 0 {
		logicalW = float32(pw) / exportRasterScale
	}
	s := room / logicalW
	if s < minFitScale {
		return minFitScale
	}
	if s > 1.0 {
		return 1.0
	}
	return s
}

func (c *Controller) renderHeader(ctx *ui.Context, pal theme.Palette, title string, format model.ExportFormat, style model.ExportTheme, pw, ph int, zoom float32) {
	tb := ctx.TitleBar()
	leftLead := float32(12)
	leftMinW := float32(140)
	if theme.IsMacOS() {
		leftLead = max(78, tb.Left+12)
		leftMinW = 200
	}

	bar := ui.Row(ctx).FillWidth().Height(theme.TitleBarHeight).Shrink(0).AlignItems(ui.Center).
		Background(pal.Shell).BorderWidth(0, 0, 1, 0).BorderColor(pal.SidebarBorder).DragWindow()
	bar.Children(func() {
		// Left: macOS clearance + divider + export-back
		ui.Row(ctx).MinWidth(leftMinW).FillHeight().Shrink(0).AlignItems(ui.Center).
			Padding(0, 8, 0, leftLead).Gap(10).Children(func() {
			if theme.IsMacOS() {
				ui.Box(ctx).Width(1).Height(16).Background(pal.SidebarBorder)
			}
			backBtn := internalui.IconButton(ctx, icons.ArrowLeft, internalui.IconButtonOptions{
				ID:      "export-back",
				Label:   "export-back",
				Tooltip: theme.Tr("返回", "Back"),
			})
			if backBtn.Clicked() {
				c.Close()
			}
		})

		// Center: icon + Title · 导出预览
		ui.Row(ctx).Grow(1).MinWidth(0).FillHeight().Center().Gap(8).DragWindow().Children(func() {
			icons.Draw(ctx, icons.Image, 18, pal.MonoIconColors(pal.TextSecondary))
			ui.Text(ctx, title).FontSize(14).FontWeight(560).TextColor(pal.Text).SingleLine().Ellipsis("…")
			ui.Text(ctx, theme.Tr("· 导出预览", "· Export Preview")).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
		})

		// Right: zoom cluster
		dimLabel := formatDimensionLabel(format, style, pw, ph)
		zoomLabel := theme.Tr("适应", "Fit")
		if zoom > 0 {
			zoomLabel = fmt.Sprintf("%d%%", int(math.Round(float64(zoom*100))))
		}
		rightPr := float32(12)
		if theme.IsMacOS() {
			rightPr = 16
		}
		ui.Row(ctx).MinWidth(leftMinW).FillHeight().Shrink(0).AlignItems(ui.Center).
			Justify(ui.End).Padding(0, rightPr, 0, 8).Children(func() {
			capsule := ui.Row(ctx).Height(28).Shrink(0).AlignItems(ui.Center).Gap(6).
				Padding(0, 4, 0, 10).Radius(theme.RadiusControl).
				Background(pal.Paper).Border(1, pal.BtnBorder)
			capsule.Children(func() {
				ui.Text(ctx, dimLabel).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
				ui.Box(ctx).Width(1).Height(14).Shrink(0).Background(pal.Hairline)
				ui.Row(ctx).Shrink(0).AlignItems(ui.Center).Gap(2).Children(func() {
					outBtn := zoomHitButton(ctx, pal, "export-zoom-out", "−", false)
					if outBtn.Clicked() {
						c.ZoomOut()
					}
					rstBtn := zoomHitButton(ctx, pal, "export-zoom-reset", zoomLabel, true)
					if rstBtn.Clicked() {
						c.ZoomReset()
					}
					inBtn := zoomHitButton(ctx, pal, "export-zoom-in", "+", false)
					if inBtn.Clicked() {
						c.ZoomIn()
					}
				})
			})
		})
	})
}

func zoomHitButton(ctx *ui.Context, pal theme.Palette, id, label string, wide bool) ui.Element {
	ctx.Key(id)
	btn := ui.ButtonBase(ctx).Height(22).Shrink(0).Center().Radius(theme.RadiusControl).
		Cursor(ui.CursorPointer).Label(id)
	if wide {
		btn.MinWidth(40).PaddingX(6)
	} else {
		btn.Width(22)
	}
	if btn.Hovered() {
		btn.Background(pal.Hover)
	}
	fg := pal.TextSecondary
	if wide || btn.Hovered() {
		fg = pal.Text
	}
	btn.Children(func() {
		ui.Text(ctx, label).FontSize(14).TextColor(fg).SingleLine()
	})
	internalui.Probe(btn, id)
	return btn
}

func (c *Controller) renderStage(ctx *ui.Context, pal theme.Palette, bm *ui.Bitmap, pw, ph int, scale float32, previewErr string) {
	stageBG := ui.Hex("#e8eaee")
	if pal.Dark {
		stageBG = ui.Hex("#121316")
	}
	stage := ui.Box(ctx).Grow(1).MinWidth(0).FillHeight().Background(stageBG)
	stage.Children(func() {
		ctx.Key("export-stage")
		scroller := internalui.ScrollBoth(ctx).Fill().Label("export-stage")
		internalui.Probe(scroller, "export-stage")
		scroller.Children(func() {
			align := ui.Start
			if bm == nil {
				align = ui.Center
			}
			inner := ui.Column(ctx).MinWidthPercent(100).MinHeightPercent(100).
				AlignItems(ui.Center).Justify(align).
				Padding(previewPadTop, previewPadX, previewPadBottom, previewPadX)
			inner.Children(func() {
				if bm != nil && pw > 0 && ph > 0 {
					drawW := float32(pw) / exportRasterScale * scale
					drawH := float32(ph) / exportRasterScale * scale
					paperWrap := ui.Box(ctx).Size(drawW, drawH).Shrink(0).
						Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.16))
					paperWrap.Children(func() {
						ui.Image(ctx, bm).Size(drawW, drawH)
					})
				} else if previewErr != "" {
					ui.Box(ctx).MaxWidth(520).Padding(16, 20).Radius(theme.RadiusSurface).
						Background(pal.Paper).Border(1, pal.Danger.Alpha(0.5)).
						Children(func() {
							ui.Text(ctx, previewErr).FontSize(14).TextColor(pal.Danger)
						})
				} else {
					ui.Box(ctx).MaxWidth(520).Padding(16, 20).Radius(theme.RadiusSurface).
						Background(pal.Paper).Border(1, pal.SidebarBorder).
						Children(func() {
							ui.Text(ctx, theme.Tr("正在生成预览…", "Rendering preview…")).FontSize(14).TextColor(pal.TextSecondary)
						})
				}
			})
		})
	})
}

func (c *Controller) renderSidebar(
	ctx *ui.Context,
	pal theme.Palette,
	format model.ExportFormat,
	style model.ExportTheme,
	useSelection bool,
	hasSelection bool,
	showTitle bool,
	watermark bool,
	notice string,
	savedPath string,
	copied bool,
) {
	sidebar := ui.Column(ctx).Width(optionsSidebarWidth).Shrink(0).FillHeight().
		Background(pal.Paper).BorderWidth(0, 0, 0, 1).BorderColor(pal.SidebarBorder)
	sidebar.Children(func() {
		ctx.Key("export-options")
		scrollBody := internalui.Scroll(ctx).FillWidth().Grow(1).MinHeight(0).Label("export-options")
		internalui.Probe(scrollBody, "export-options")
		scrollBody.Children(func() {
			ui.Column(ctx).FillWidth().Padding(16).Gap(18).Children(func() {
				// 1. Format selector
				renderOptionSection(ctx, pal, theme.Tr("导出格式", "Format"), func() {
					c.renderFormatBar(ctx, pal, format)
				})

				// 2. Scope selector
				renderOptionSection(ctx, pal, theme.Tr("导出范围", "Scope"), func() {
					scopeIdx := 0
					if useSelection {
						scopeIdx = 1
					}
					if picked, changed := renderExportSegmented(ctx, pal, "export-scope", scopeIdx, []segChoice{
						{ID: "export-scope-whole", Label: theme.Tr("整篇笔记", "Full Note"), Enabled: true},
						{ID: "export-scope-selection", Label: theme.Tr("选中导出", "Selection"), Enabled: hasSelection},
					}); changed {
						c.SetUseSelection(picked == 1)
					}
				})

				// 3. Paper Theme (Light/Dark + 2x3 swatch grid)
				renderOptionSection(ctx, pal, theme.Tr("纸面主题", "Paper Theme"), func() {
					ui.Column(ctx).FillWidth().Gap(8).Children(func() {
						darkIdx := 0
						if style.IsDark() {
							darkIdx = 1
						}
						if picked, changed := renderExportSegmented(ctx, pal, "export-appearance", darkIdx, []segChoice{
							{ID: "export-appearance-light", Label: theme.Tr("浅色 · 6", "Light · 6"), Enabled: true},
							{ID: "export-appearance-dark", Label: theme.Tr("深色 · 6", "Dark · 6"), Enabled: true},
						}); changed {
							c.setDarkGroup(picked == 1)
						}
						c.renderStyleGrid(ctx, pal, style)
					})
				})

				// 4. Page Elements (Show Title + Watermark)
				renderOptionSection(ctx, pal, theme.Tr("版面元素", "Page Elements"), func() {
					ui.Column(ctx).FillWidth().Padding(2, 12).Radius(theme.RadiusSurface).
						Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
						stVal := showTitle
						if renderSwitchRow(ctx, pal, "export-title-toggle", theme.Tr("显示标题", "Show Title"), &stVal) {
							c.ToggleShowTitle()
						}
						if formatSupportsWatermark(format) {
							ui.Box(ctx).FillWidth().Height(1).Background(pal.Hairline)
							wmVal := watermark
							if renderSwitchRow(ctx, pal, "export-watermark", theme.Tr("底部水印", "Watermark"), &wmVal) {
								c.ToggleWatermark()
							}
						}
					})
				})
			})
		})

		// Pinned Footer
		ui.Column(ctx).FillWidth().Shrink(0).Padding(14, 16).Gap(10).
			Background(pal.Paper).BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
			if notice != "" {
				ui.Row(ctx).FillWidth().Padding(8, 12).Gap(8).AlignItems(ui.Center).
					Radius(theme.RadiusControl).Background(pal.Selected).Border(1, pal.Accent).
					Children(func() {
						ui.Text(ctx, notice).FontSize(14).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
						if savedPath != "" {
							ctx.Key("export-reveal")
							rev := ui.ButtonBase(ctx).Shrink(0).Cursor(ui.CursorPointer).
								Label("export-reveal")
							rev.Children(func() {
								ui.Text(ctx, revealLabel()).FontSize(14).FontWeight(500).TextColor(pal.Accent).SingleLine()
							})
							internalui.Probe(rev, "export-reveal")
							if rev.Clicked() {
								c.revealSaved()
							}
						}
					})
			}

			ui.Row(ctx).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				saveLbl := saveButtonLabel(format)
				saveBtn := renderFooterButton(ctx, pal, "export-save", saveLbl, icons.Download, true)
				if saveBtn.Clicked() {
					c.triggerSave()
				}

				copyIcon := icons.Copy
				copyLbl := theme.Tr("复制", "Copy")
				if copied {
					copyIcon = icons.Check
					copyLbl = theme.Tr("已复制", "Copied")
				}
				copyBtn := renderFooterButton(ctx, pal, "export-copy", copyLbl, copyIcon, false)
				if copyBtn.Clicked() {
					_ = c.CopyToClipboard()
				}
			})
		})
	})
}

func renderOptionSection(ctx *ui.Context, pal theme.Palette, title string, content func()) {
	ui.Column(ctx).FillWidth().Gap(8).Children(func() {
		ui.Text(ctx, title).FontSize(14).FontWeight(600).TextColor(pal.Text).SingleLine()
		content()
	})
}

type segChoice struct {
	ID      string
	Label   string
	Enabled bool
}

func renderExportSegmented(ctx *ui.Context, pal theme.Palette, id string, active int, items []segChoice) (int, bool) {
	ctx.Key(id)
	cur := active
	seg := ui.SegmentedBase(ctx, &cur, len(items))
	track := seg.Track.FillWidth().Height(38).Padding(3).Gap(2).Radius(theme.RadiusControl).
		Background(pal.Shell).Border(1, pal.Hairline)
	internalui.Probe(track, id)
	track.Children(func() {
		for i, it := range items {
			selected := i == active
			btn := seg.Segment(i).Grow(1).MinWidth(0).Height(30).Center().Radius(theme.RadiusControl).
				Border(1, ui.Transparent).Disabled(!it.Enabled).Label(it.Label)
			if selected {
				btn.Background(pal.Selected).Border(1, pal.Accent)
			} else if it.Enabled && btn.Hovered() {
				btn.Background(pal.Hover)
			}
			if it.Enabled {
				btn.Cursor(ui.CursorPointer)
			} else {
				btn.Opacity(0.4)
			}
			fg := pal.TextSecondary
			if selected {
				fg = pal.Text
			}
			btn.Children(func() {
				ui.Text(ctx, it.Label).FontSize(14).FontWeight(500).TextColor(fg).SingleLine()
			})
			if it.ID != "" {
				internalui.Probe(btn, it.ID)
			}
		}
	})
	return cur, cur != active
}

func (c *Controller) renderFormatBar(ctx *ui.Context, pal theme.Palette, current model.ExportFormat) {
	formats := []struct {
		fmt   model.ExportFormat
		label string
	}{
		{model.ExportPNG, "PNG"},
		{model.ExportPDF, "PDF"},
		{model.ExportDOCX, "Word"},
		{model.ExportMarkdown, "MD"},
		{model.ExportHTML, "HTML"},
	}
	activeIdx := 0
	for i, f := range formats {
		if f.fmt == current {
			activeIdx = i
			break
		}
	}
	cur := activeIdx
	ctx.Key("export-format")
	seg := ui.SegmentedBase(ctx, &cur, len(formats))
	track := seg.Track.FillWidth().Height(38).Padding(3).Gap(2).Radius(theme.RadiusControl).
		Background(pal.Shell).Border(1, pal.Hairline)
	internalui.Probe(track, "export-format")
	track.Children(func() {
		for i, f := range formats {
			selected := i == activeIdx
			btn := seg.Segment(i).Grow(1).MinWidth(0).Height(30).Center().Radius(theme.RadiusControl).
				Border(1, ui.Transparent).Cursor(ui.CursorPointer).Label(f.label)
			if selected {
				btn.Background(pal.Selected).Border(1, pal.Accent)
			} else if btn.Hovered() {
				btn.Background(pal.Hover)
			}
			fg := pal.TextSecondary
			if selected {
				fg = pal.Text
			}
			btn.Children(func() {
				ui.Text(ctx, f.label).FontSize(14).FontWeight(500).TextColor(fg).SingleLine()
			})
			internalui.Probe(btn, "export-fmt-"+string(f.fmt))
		}
	})
	if cur != activeIdx && cur >= 0 && cur < len(formats) {
		c.SetFormat(formats[cur].fmt)
	}
}

func (c *Controller) renderStyleGrid(ctx *ui.Context, pal theme.Palette, current model.ExportTheme) {
	items := ThemeGroup(current.IsDark())
	ui.Column(ctx).FillWidth().Gap(8).Children(func() {
		for row := 0; row < len(items); row += 2 {
			ui.Row(ctx).FillWidth().Gap(8).Children(func() {
				for col := 0; col < 2 && row+col < len(items); col++ {
					st := items[row+col]
					if c.renderSwatchCard(ctx, pal, st, st == current) {
						c.SetStyle(st)
					}
				}
			})
		}
	})
}

func (c *Controller) renderSwatchCard(ctx *ui.Context, pal theme.Palette, st model.ExportTheme, selected bool) bool {
	spec := ThemeSpec(st)
	id := "export-style-" + string(st)
	ctx.Key(id)
	btn := ui.ButtonBase(ctx).Grow(1).MinWidth(0).Height(52).PaddingX(10).Gap(10).
		AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
		Role(ui.RoleRadio).Selected(selected).Label(id)

	fg := pal.TextSecondary
	if selected {
		btn.Background(pal.Selected).Border(1, pal.Accent)
		fg = pal.Text
	} else if btn.Hovered() {
		btn.Background(pal.Hover).Border(1, pal.BtnBorder)
		fg = pal.Text
	} else {
		btn.Background(pal.Paper).Border(1, pal.BtnBorder)
	}

	chipBorder := ui.Hex("#d8d2c5")
	if st.IsDark() {
		chipBorder = ui.Hex("#3a3a3a")
	}

	btn.Children(func() {
		// Mini paper + accent + inner card swatch chip (28x22)
		ui.Box(ctx).Size(28, 22).Shrink(0).Radius(4).Background(spec.Swatch[0]).
			Border(1, chipBorder).Clip().AlignItems(ui.Center).Justify(ui.End).
			Children(func() {
				ui.Box(ctx).Size(18, 14).Background(spec.Swatch[2]).
					BorderWidth(2, 0, 0, 0).BorderColor(spec.Swatch[1])
			})
		ui.Text(ctx, spec.Label).FontSize(14).FontWeight(500).TextColor(fg).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if selected {
			icons.Draw(ctx, icons.Check, 15, pal.MonoIconColors(pal.Accent))
		}
	})
	internalui.Probe(btn, id)
	return btn.Clicked()
}

func renderSwitchRow(ctx *ui.Context, pal theme.Palette, id, label string, value *bool) bool {
	var changed bool
	ui.Row(ctx).FillWidth().Height(40).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(ctx, label).FontSize(14).TextColor(pal.Text).SingleLine()
		changed = internalui.TogglePill(ctx, id, id, value, false)
	})
	return changed
}

func renderFooterButton(ctx *ui.Context, pal theme.Palette, id, label string, glyph icons.Icon, primary bool) ui.Element {
	ctx.Key(id)
	btn := ui.ButtonBase(ctx).Height(38).PaddingX(16).Gap(6).AlignItems(ui.Center).Justify(ui.Center).
		Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Label(id)
	if primary {
		btn.Grow(1)
	} else {
		btn.Shrink(0)
	}
	fg := pal.Text
	if primary {
		fg = pal.AccentText
		bg := pal.Accent
		if btn.Pressed() {
			bg = pal.AccentPressed
		} else if btn.Hovered() {
			bg = pal.AccentHover
		}
		btn.Background(bg)
	} else {
		bg := pal.Shell
		if btn.Pressed() {
			bg = pal.Selected
		} else if btn.Hovered() {
			bg = pal.Hover
		}
		btn.Background(bg).Border(1, pal.BtnBorder)
	}
	btn.Children(func() {
		icons.Draw(ctx, glyph, 16, pal.MonoIconColors(fg))
		ui.Text(ctx, label).FontSize(14).FontWeight(500).TextColor(fg).SingleLine()
	})
	internalui.Probe(btn, id)
	return btn
}

func isSupportedFormat(f model.ExportFormat) bool {
	switch f {
	case model.ExportPNG, model.ExportPDF, model.ExportDOCX, model.ExportMarkdown, model.ExportHTML, model.ExportZIP:
		return true
	default:
		return false
	}
}

func formatSupportsWatermark(f model.ExportFormat) bool {
	return f != model.ExportMarkdown && f != model.ExportZIP
}

func exportExtension(f model.ExportFormat) string {
	switch f {
	case model.ExportPNG:
		return "png"
	case model.ExportPDF:
		return "pdf"
	case model.ExportDOCX:
		return "docx"
	case model.ExportHTML:
		return "html"
	case model.ExportZIP:
		return "zip"
	default:
		return "md"
	}
}

func saveButtonLabel(f model.ExportFormat) string {
	switch f {
	case model.ExportPNG:
		return theme.Tr("保存 PNG", "Save PNG")
	case model.ExportPDF:
		return theme.Tr("保存 PDF", "Save PDF")
	case model.ExportDOCX:
		return theme.Tr("保存 Word", "Save Word")
	case model.ExportHTML:
		return theme.Tr("保存 HTML", "Save HTML")
	case model.ExportZIP:
		return theme.Tr("保存 ZIP", "Save ZIP")
	default:
		return theme.Tr("保存 Markdown", "Save Markdown")
	}
}

func revealLabel() string {
	if theme.IsMacOS() {
		return theme.Tr("在访达中显示", "Reveal in Finder")
	}
	return theme.Tr("在文件管理器中显示", "Reveal in File Manager")
}

func formatDimensionLabel(f model.ExportFormat, st model.ExportTheme, pw, ph int) string {
	switch f {
	case model.ExportPNG:
		if pw > 0 && ph > 0 {
			return fmt.Sprintf("%s × %s px · %s", fmtInt(pw), fmtInt(ph), theme.Tr("2×", "2×"))
		}
		return theme.Tr("816 px 纸宽 · 2×", "816 px page · 2×")
	case model.ExportPDF:
		if pw > 0 && ph > 0 {
			ptW := int(float32(pw)/exportRasterScale + 0.5)
			ptH := int(float32(ph)/exportRasterScale + 0.5)
			return fmt.Sprintf("PDF · %s × %s pt", fmtInt(ptW), fmtInt(ptH))
		}
		return theme.Tr("PDF · 高清纸面", "PDF · HD Page")
	case model.ExportDOCX:
		return fmt.Sprintf("Word (.docx) · %s", ThemeLabel(st))
	case model.ExportMarkdown:
		return theme.Tr("Markdown (.md) · 纯文本", "Markdown (.md)")
	case model.ExportHTML:
		return fmt.Sprintf("HTML (.html) · %s", ThemeLabel(st))
	case model.ExportZIP:
		return "ZIP (.zip)"
	default:
		return ""
	}
}

func fmtInt(n int) string {
	if n < 0 {
		n = 0
	}
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

func suggestedExportFilename(title string, format model.ExportFormat) string {
	var clean strings.Builder
	for _, ch := range title {
		if ch < 0x20 || strings.ContainsRune(`/\:*?"<>|`, ch) {
			clean.WriteByte('-')
		} else {
			clean.WriteRune(ch)
		}
	}
	stem := strings.Trim(clean.String(), " \t\r\n.")
	if len(stem) > 180 {
		b := make([]byte, 0, 180)
		for _, ch := range stem {
			s := string(ch)
			if len(b)+len(s) > 180 {
				break
			}
			b = append(b, s...)
		}
		stem = strings.TrimRight(string(b), " \t\r\n.")
	}
	if stem == "" {
		stem = theme.Tr("未命名", "Untitled")
	}
	base := strings.ToUpper(strings.TrimSpace(strings.SplitN(stem, ".", 2)[0]))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$":
		stem = "_" + stem
	default:
		if (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && len(base) == 4 && base[3] >= '1' && base[3] <= '9' {
			stem = "_" + stem
		}
	}
	return stem + "." + exportExtension(format)
}
