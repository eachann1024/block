package export

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"block/internal/icons"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	exportPrefsFilename = "export-preferences.json"
	optionsSidebarWidth = float32(320)
	previewPadX         = float32(32)
	previewPadTop       = float32(36)
	previewPadBottom    = float32(36)
	minFitScale         = float32(0.75)
	exportPageWidth     = float32(816)
	exportRasterScale   = float32(markdown.ExportScale)
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

	previewBitmap  *ui.Bitmap
	previewW       int
	previewH       int
	previewErr     string
	previewRev     uint64
	previewCancel  context.CancelFunc
	previewRunning bool

	exportDataRev uint64
	cachedExports map[model.ExportFormat]cachedExportPayload

	notice        string
	savedPath     string
	copied        bool
	actionRunning bool

	services ui.Services

	// Optional test hook for interactive save dialog when mygo.App is not running.
	OnSaveDialog func(dir, suggested string, done func(string))
}

// NewController creates a full-window export preview controller backed by dataDir.
// onRedraw may be called from a background worker.
func NewController(dataDir string, onRedraw func()) *Controller {
	c := &Controller{
		dataDir:    strings.TrimSpace(dataDir),
		onRedraw:   onRedraw,
		format:     model.ExportPNG,
		style:      model.ExportThemeMono,
		lightStyle: model.ExportThemeMono,
		darkStyle:  model.ExportThemeInk,
		showTitle:  true,
		watermark:  true,
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
	c.openPreview(title, notePath, wholeDoc, selectionDoc, false)
}

// OpenWithSelection previews the selection when available, falling back to the whole note.
func (c *Controller) OpenWithSelection(title, notePath string, wholeDoc, selectionDoc *model.Doc) {
	c.openPreview(title, notePath, wholeDoc, selectionDoc, true)
}

func (c *Controller) openPreview(title, notePath string, wholeDoc, selectionDoc *model.Doc, preferSelection bool) {
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
	c.useSelection = preferSelection && c.selectionDoc != nil
	c.exportDataRev++
	clear(c.cachedExports)
	c.notice = ""
	c.savedPath = ""
	c.copied = false
	c.queuePreviewLocked()
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
	c.wholeDoc, c.selectionDoc = nil, nil
	c.previewBitmap = nil
	c.exportDataRev++
	c.cachedExports = nil
	if c.previewCancel != nil {
		c.previewCancel()
	}
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
	densityChanged := false
	if f == model.ExportPNG || c.format == model.ExportPNG {
		if c.previewBitmap == nil || c.previewW <= 0 || c.previewH <= 0 {
			densityChanged = true
		} else if f == model.ExportPNG {
			const maxSide = 32768
			const maxPixels = int64(32 * 1024 * 1024)
			densityChanged = c.previewW > maxSide || c.previewH > maxSide || int64(c.previewW)*int64(c.previewH) > maxPixels
		} else {
			fullW := int(exportPageWidth * exportRasterScale)
			densityChanged = c.previewW < fullW-1
		}
	}
	c.format = f
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.savePreferencesLocked()
	if wmChanged || densityChanged {
		c.queuePreviewLocked()
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
	c.exportDataRev++
	clear(c.cachedExports)
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.savePreferencesLocked()
	c.queuePreviewLocked()
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
	c.exportDataRev++
	clear(c.cachedExports)
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.savePreferencesLocked()
	c.queuePreviewLocked()
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
	c.exportDataRev++
	clear(c.cachedExports)
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.queuePreviewLocked()
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
	c.exportDataRev++
	clear(c.cachedExports)
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.savePreferencesLocked()
	c.queuePreviewLocked()
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
	c.exportDataRev++
	clear(c.cachedExports)
	c.copied = false
	c.notice = ""
	c.savedPath = ""
	c.savePreferencesLocked()
	c.queuePreviewLocked()
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

// Render builds the full-window A-Export preview UI.
func (c *Controller) Render(ctx *ui.Context) {
	c.mu.Lock()
	if !c.open {
		c.mu.Unlock()
		return
	}
	c.services = ctx.Services()
	c.mu.Unlock()

	// Keyboard shortcuts: Esc close, Cmd+C copy, Cmd+S save.
	switch {
	case ctx.Shortcut(0, ui.KeyEscape):
		c.Close()
		return
	case ctx.Shortcut(ui.Cmd, ui.KeyC):
		c.triggerCopy()
	case ctx.Shortcut(ui.Cmd, ui.KeyS):
		c.triggerSave()
	}

	c.mu.Lock()
	title := c.title
	format := c.format
	style := c.style
	useSelection := c.useSelection
	hasSelection := c.selectionDoc != nil
	showTitle := c.showTitle
	watermark := c.watermark
	bm := c.previewBitmap
	pw := c.previewW
	ph := c.previewH
	previewErr := c.previewErr
	notice := c.notice
	savedPath := c.savedPath
	copied := c.copied
	busy := c.actionRunning
	c.mu.Unlock()

	pal := theme.Ocean()
	vpW, vpH := ctx.Size()
	if vpW <= 0 {
		vpW = 1200
	}
	sideW := float32(optionsSidebarWidth)
	if vpW < 740 {
		sideW = 272
	}
	stageW := max(0, vpW-sideW)
	stageH := max(0, vpH-theme.TitleBarHeight)
	room := max(180, stageW-previewPadX*2)
	scale := computeFitScale(room, pw)

	ctx.Key("export-view")
	root := ui.Column(ctx).Fill().Background(pal.Shell).TextColor(pal.Text).Label("export-view")
	internalui.Probe(root, "export-view")

	root.Children(func() {
		c.renderHeader(ctx, pal, title, format, style, pw, ph, vpW)
		ui.Row(ctx).FillWidth().Grow(1).MinHeight(0).Children(func() {
			c.renderStage(ctx, pal, bm, pw, ph, scale, stageW, stageH, previewErr)
			c.renderSidebar(ctx, pal, format, style, useSelection, hasSelection, showTitle, watermark, notice, savedPath, copied, busy, sideW)
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

func (c *Controller) renderHeader(ctx *ui.Context, pal theme.Palette, title string, format model.ExportFormat, style model.ExportTheme, pw, ph int, vpW float32) {
	tb := ctx.TitleBar()
	compact := vpW > 0 && vpW < 780
	leftLead := float32(12)
	leftMinW := float32(140)
	if theme.IsMacOS() {
		leftLead = max(78, tb.Left+12)
		leftMinW = 200
	}
	if compact {
		leftMinW = 0
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
		ui.Row(ctx).Grow(1).MinWidth(0).FillHeight().PaddingX(8).Center().Gap(8).Clip().DragWindow().Children(func() {
			icons.Draw(ctx, icons.Image, 18, pal.MonoIconColors(pal.TextSecondary))
			ui.Text(ctx, title).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
			if !compact {
				ui.Text(ctx, theme.Tr("· 导出预览", "· Export Preview")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
			}
		})

		// Right: read-only output size + platform caption-button clearance
		dimLabel := formatDimensionLabel(format, style, pw, ph)
		rightPr := float32(12)
		if theme.IsMacOS() {
			rightPr = 16
		} else if tb.Right > 0 {
			rightPr = tb.Right + 8
		}
		ui.Row(ctx).MinWidth(leftMinW).FillHeight().Shrink(0).AlignItems(ui.Center).
			Justify(ui.End).Padding(0, rightPr, 0, 8).Children(func() {
			if !compact && dimLabel != "" {
				ui.Text(ctx, dimLabel).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
			}
		})
	})
}

func (c *Controller) renderStage(ctx *ui.Context, pal theme.Palette, bm *ui.Bitmap, pw, ph int, scale, viewportW, viewportH float32, previewErr string) {
	drawW := float32(pw) / exportRasterScale * scale
	drawH := float32(ph) / exportRasterScale * scale
	contentW := max(viewportW, drawW+previewPadX*2)
	contentH := max(viewportH, drawH+previewPadTop+previewPadBottom)

	stageBG := pal.Export().StageBG
	stage := ui.Box(ctx).Grow(1).MinWidth(0).FillHeight().Background(stageBG)
	stage.Children(func() {
		ctx.Key("export-stage")
		scroller := internalui.ScrollBoth(ctx).Fill().Label("export-stage")
		internalui.Probe(scroller, "export-stage")
		scroller.Children(func() {
			inner := ui.Column(ctx).Size(contentW, contentH).Shrink(0).Center().
				Padding(previewPadTop, previewPadX, previewPadBottom, previewPadX)
			inner.Children(func() {
				if bm != nil && pw > 0 && ph > 0 {
					paperWrap := internalui.Elevate(
						ui.Box(ctx.Key("export-preview-card")).Size(drawW, drawH).Shrink(0).
							Transition(internalui.SurfaceStageTransition()),
						theme.ElevationDialog,
					)
					paperWrap.Children(func() {
						ui.Image(ctx, bm).Size(drawW, drawH)
					})
				} else if previewErr != "" {
					ui.Box(ctx.Key("export-preview-err")).MaxWidth(520).Padding(16, 20).Radius(theme.RadiusXL).
						Background(pal.Paper).Border(1, pal.Danger.Alpha(0.5)).
						Transition(internalui.SurfaceStageTransition()).
						Children(func() {
							ui.Text(ctx, previewErr).FontSize(theme.FontBody).TextColor(pal.Danger)
						})
				} else {
					ui.Box(ctx.Key("export-preview-loading")).MaxWidth(520).Padding(16, 20).Radius(theme.RadiusXL).
						Background(pal.Paper).Border(1, pal.SidebarBorder).
						Transition(internalui.SurfaceStageTransition()).
						Children(func() {
							ui.Text(ctx, theme.Tr("正在生成预览…", "Rendering preview…")).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
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
	busy bool,
	sideW float32,
) {
	sidebar := ui.Column(ctx.Key("export-sidebar")).Width(sideW).Shrink(0).FillHeight().
		Background(pal.Paper).BorderWidth(0, 0, 0, 1).BorderColor(pal.SidebarBorder).
		Transition(internalui.DockedPanelTransition(true))
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
					ui.Column(ctx).FillWidth().Padding(2, 12).Radius(theme.RadiusXL).
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
					Radius(theme.RadiusMD).Background(pal.Selected).Border(1, ui.Transparent).
					Children(func() {
						ui.Text(ctx, notice).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
						if savedPath != "" {
							ctx.Key("export-reveal")
							rev := ui.ButtonBase(ctx).Shrink(0).Cursor(ui.CursorPointer).
								Label("export-reveal")
							internalui.TactileCard(rev, rev.Hovered(), rev.Pressed(), false, false)
							rev.Children(func() {
								ui.Text(ctx, revealLabel()).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Accent).SingleLine()
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
				saveBtn := renderFooterButton(ctx, pal, "export-save", saveLbl, icons.Download, true, busy)
				if saveBtn.Clicked() {
					c.triggerSave()
				}

				copyIcon := icons.Copy
				copyLbl := theme.Tr("复制", "Copy")
				if copied {
					copyIcon = icons.Check
					copyLbl = theme.Tr("已复制", "Copied")
				}
				copyBtn := renderFooterButton(ctx, pal, "export-copy", copyLbl, copyIcon, false, busy)
				if copyBtn.Clicked() {
					c.triggerCopy()
				}
			})
		})
	})
}

func renderOptionSection(ctx *ui.Context, pal theme.Palette, title string, content func()) {
	ui.Column(ctx).FillWidth().Gap(8).Children(func() {
		ui.Text(ctx, title).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine()
		content()
	})
}

type segChoice struct {
	ID      string
	Label   string
	Enabled bool
}

func renderExportSegmented(ctx *ui.Context, pal theme.Palette, id string, active int, items []segChoice) (int, bool) {
	cur := active
	opts := make([]internalui.SegmentOption, len(items))
	for i, it := range items {
		opts[i] = internalui.SegmentOption{
			ID:       it.ID,
			Label:    it.Label,
			Disabled: !it.Enabled,
		}
	}
	changed := internalui.SegmentedControlWithOptions(ctx, id, &cur, opts, internalui.SegmentedControlOptions{
		FillWidth:     true,
		TrackHeight:   38,
		TrackPadding:  theme.Space2,
		SegmentHeight: 30,
		SegmentRadius: theme.RadiusMD,
		ShellTrack:    true,
	})
	return cur, changed
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
	items := make([]segChoice, len(formats))
	for i, f := range formats {
		if f.fmt == current {
			activeIdx = i
		}
		items[i] = segChoice{
			ID:      "export-fmt-" + string(f.fmt),
			Label:   f.label,
			Enabled: true,
		}
	}
	if cur, changed := renderExportSegmented(ctx, pal, "export-format", activeIdx, items); changed && cur >= 0 && cur < len(formats) {
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
	btn := ui.ButtonBase(ctx).Column().Grow(1).MinWidth(0).Height(theme.ExportThemeTileHeight).
		Padding(theme.Space6).Gap(theme.Space6).
		Radius(theme.RadiusLG).Cursor(ui.CursorPointer).
		Role(ui.RoleRadio).Selected(selected).Label(id)

	hovered := btn.Hovered()
	pressed := btn.Pressed()
	bg, bd := pal.Paper, pal.BtnBorder
	fg := pal.TextSecondary
	if selected {
		bg, bd = pal.Selected, pal.SelectedBorder
		fg = pal.Text
	} else if pressed {
		bg = pal.Selected
		fg = pal.Text
	} else if hovered {
		bg = pal.Hover
		fg = pal.Text
	}
	internalui.AnimateSurfaceColors(btn, bg, bd, true)
	internalui.TactileCard(btn, hovered, pressed, selected, false)

	chipBorder := pal.Export().ChipBorder

	btn.Children(func() {
		ui.Box(ctx).FillWidth().Height(theme.ExportThemeThumbHeight).Shrink(0).
			Radius(theme.RadiusSM).Border(1, chipBorder).
			Draw(func(p *ui.Painter, r ui.Rect) {
				markdown.PaintThemeThumb(p, r, st, theme.RadiusSM)
			})
		ui.Row(ctx).FillWidth().PaddingX(theme.Space4).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space4).Children(func() {
			ui.Text(ctx, spec.Label).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(fg).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			if selected {
				icons.DrawAnimated(ctx, icons.Check, theme.IconInline, pal.MonoIconColors(pal.Text), hovered || pressed)
			}
		})
	})
	internalui.Probe(btn, id)
	return btn.Clicked()
}

func renderSwitchRow(ctx *ui.Context, pal theme.Palette, id, label string, value *bool) bool {
	var changed bool
	ui.Row(ctx).FillWidth().Height(40).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(ctx, label).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
		changed = internalui.TogglePill(ctx, id, id, value, false)
	})
	return changed
}

func renderFooterButton(ctx *ui.Context, pal theme.Palette, id, label string, glyph icons.Icon, primary, disabled bool) ui.Element {
	style := internalui.ButtonSecondary
	var grow float32
	var shrink *float32
	if primary {
		style = internalui.ButtonPrimary
		grow = 1
	} else {
		z := float32(0)
		shrink = &z
	}
	return internalui.ActionButtonWithOptions(ctx, internalui.ActionButtonOptions{
		ID:                 id,
		Label:              label,
		AccessibilityLabel: id,
		Leading:            &glyph,
		Style:              style,
		Disabled:           disabled,
		Height:             38,
		PaddingX:           theme.Space16,
		Grow:               grow,
		Shrink:             shrink,
		SecondaryShellBG:   true,
	})
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
			return fmt.Sprintf("%s × %s px · %.2g×", fmtInt(pw), fmtInt(ph), float32(pw)/exportPageWidth)
		}
		return fmt.Sprintf(theme.Tr("%g px 纸宽 · %g×", "%g px page · %g×"), exportPageWidth, exportRasterScale)
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
