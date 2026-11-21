package ui

import (
	"fmt"
	"math"
	"time"

	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const (
	lightboxMinZoom  float32 = 0.25
	lightboxMaxZoom  float32 = 4.0
	lightboxZoomStep float32 = 1.2
	lightboxPanStep  float32 = 80
)

// LightboxItem describes one attachment, image, animated GIF, or video rendered inside RenderLightbox.
type LightboxItem struct {
	ID            string
	Name          string
	Meta          string
	IsImage       bool
	IsVideo       bool
	IsAnimation   bool
	Image         ui.ImageSource
	Video         *ui.VideoPlayer
	ImageW        int
	ImageH        int
	OpenURL       string
	TextPreview   string
	TextTruncated bool
	Unavailable   bool
	Loading       bool
	CanRemove     bool
}

// LightboxState holds the open/index/zoom/scroll/video state for RenderLightbox.
type LightboxState struct {
	Open          bool
	Index         int
	Zoom          float32
	FitMode       bool
	FitScale      float32
	Scroll        ui.ScrollState
	CopiedUntil   time.Time
	Video         VideoControlState
	VideoFillMode bool

	panDragging     bool
	panScrollStartX float32
	panScrollStartY float32
	panMoved        bool
}

// LightboxCallbacks supplies optional actions triggered from the lightbox chrome.
type LightboxCallbacks struct {
	OnClose       func()
	OnSelect      func(index int)
	OnCopy        func(index int) bool
	OnReveal      func(index int)
	OnOpenURL     func(index int)
	OnRequestLoad func(index int)
	OnRemove      func(index int)
}

type lightboxLocalKey struct{}

type lightboxLocalState struct {
	mountedFrames int
	lastIndex     int
}

// ResetView restores fit-to-viewport zoom, clears pan/scroll state, and resets transient video UI state.
func (s *LightboxState) ResetView() {
	if s == nil {
		return
	}
	s.Zoom = 1.0
	s.FitMode = true
	s.FitScale = 0
	s.Scroll = ui.ScrollState{}
	s.VideoFillMode = false
	s.panDragging = false
	s.panMoved = false
	s.Video.ResetTransient()
}

func clampLightboxZoom(z, minZoom float32) float32 {
	if minZoom <= 0 || minZoom > lightboxMinZoom {
		minZoom = lightboxMinZoom
	}
	if z < minZoom {
		return minZoom
	}
	if z > lightboxMaxZoom {
		return lightboxMaxZoom
	}
	return z
}

func effectiveMinZoom(fitScale float32) float32 {
	if fitScale > 0 && fitScale < lightboxMinZoom {
		return fitScale
	}
	return lightboxMinZoom
}

func lightboxRevealLabel() string {
	if theme.IsMacOS() {
		return theme.Tr("在访达中显示", "Reveal in Finder")
	}
	return theme.Tr("在文件管理器中显示", "Reveal in File Manager")
}

// lightboxTitlebarClearance returns the height of the hidden-titlebar window
// controls band (macOS traffic lights, Windows/Linux caption buttons), or 0
// in fullscreen and natively framed windows.
func lightboxTitlebarClearance(tb ui.TitleBar) float32 {
	clearance := max(tb.Bottom, tb.Height)
	if clearance <= 0 && (tb.Left > 0 || tb.Right > 0) {
		clearance = theme.HeightControl
	}
	return clearance
}

func lightboxToolbarTopGap(clearance float32) float32 {
	if clearance > 0 {
		return theme.Space6
	}
	return theme.Space12
}

// RenderLightbox renders a full-window Ocean Paper attachment and image lightbox overlay.
func RenderLightbox(c *ui.Context, id string, st *LightboxState, items []LightboxItem, cb LightboxCallbacks) {
	if st == nil || !st.Open || len(items) == 0 {
		return
	}
	if id == "" {
		id = "lightbox"
	}
	if st.Index < 0 {
		st.Index = 0
	} else if st.Index >= len(items) {
		st.Index = len(items) - 1
	}
	if st.Zoom <= 0 {
		st.Zoom = 1.0
	}

	pal := theme.Ocean()
	item := items[st.Index]
	displayName := item.Name
	if displayName == "" {
		displayName = theme.Tr("未命名附件", "Untitled attachment")
	}
	multi := len(items) > 1
	itemCanZoom := func(it LightboxItem) bool {
		if it.Loading || it.Unavailable || it.IsVideo {
			return false
		}
		return (it.IsImage && it.Image != nil) || (it.IsAnimation && it.Video != nil)
	}
	itemHasActiveVideo := func(it LightboxItem) bool {
		return it.IsVideo && it.Video != nil && !it.Loading && !it.Unavailable
	}
	canZoom := itemCanZoom(item)
	activeVideo := itemHasActiveVideo(item)

	closeLightbox := func() {
		if item.Video != nil {
			st.Video.EndPlayerBoost(item.Video)
		}
		st.Open = false
		if cb.OnClose != nil {
			cb.OnClose()
		}
	}

	selectItem := func(idx int) {
		if idx < 0 || idx >= len(items) || idx == st.Index {
			return
		}
		if item.IsVideo && item.Video != nil {
			st.Video.EndPlayerBoost(item.Video)
			item.Video.Pause()
		}
		st.Index = idx
		st.ResetView()
		st.CopiedUntil = time.Time{}
		if cb.OnSelect != nil {
			cb.OnSelect(idx)
		}
	}

	panStage := func(dx, dy float32) {
		st.Scroll.X = min(max(st.Scroll.X+dx, 0), max(st.Scroll.MaxX, 0))
		st.Scroll.Y = min(max(st.Scroll.Y+dy, 0), max(st.Scroll.MaxY, 0))
	}

	stepZoom := func(factor float32) {
		minZ := effectiveMinZoom(st.FitScale)
		base := st.Zoom
		if st.FitMode && st.FitScale > 0 {
			base = st.FitScale
		}
		next := clampLightboxZoom(base*factor, minZ)
		if factor < 1 && next <= minZ+0.001 && st.FitScale > 0 && st.FitScale <= lightboxMinZoom {
			st.FitMode = true
			st.Zoom = 1.0
			st.Scroll = ui.ScrollState{}
			return
		}
		st.Zoom = next
		st.FitMode = false
	}

	setActualZoom := func() {
		st.FitMode = false
		st.Zoom = 1.0
		st.Scroll = ui.ScrollState{}
	}

	setFitZoom := func() {
		st.FitMode = true
		st.Zoom = 1.0
		st.Scroll = ui.ScrollState{}
	}

	ui.Overlay(c, func() {
		scrim := ui.Box(c.Key(id + "-scrim")).
			Absolute().Left(0).Right(0).Top(0).Bottom(0).
			Background(pal.LightboxScrim()).
			Transition(ScrimFadeTransition())
		Probe(scrim, id+"-scrim")

		layer := ui.Column(c.Key(id + "-layer")).
			Absolute().Left(0).Right(0).Top(0).Bottom(0).
			Modal().Role(ui.RoleDialog).Label(displayName)
		Probe(layer, id)

		local := ui.Local(layer, lightboxLocalKey{}, func() lightboxLocalState {
			return lightboxLocalState{lastIndex: -1}
		})
		indexChanged := local.lastIndex != st.Index
		local.lastIndex = st.Index

		// Keyboard shortcuts on the modal overlay.
		if layer.OverlayShortcut(0, ui.KeyEscape) {
			closeLightbox()
			return
		}
		if multi {
			if layer.OverlayShortcut(ui.Cmd, ui.KeyLeft) ||
				layer.OverlayShortcut(0, ui.KeyPageUp) ||
				layer.OverlayShortcut(0, ui.KeyBracketLeft) {
				selectItem((st.Index - 1 + len(items)) % len(items))
			} else if layer.OverlayShortcut(ui.Cmd, ui.KeyRight) ||
				layer.OverlayShortcut(0, ui.KeyPageDown) ||
				layer.OverlayShortcut(0, ui.KeyBracketRight) {
				selectItem((st.Index + 1) % len(items))
			}
		}

		// Re-resolve active item in case Cmd+Arrow / PageUp / PageDown switched index.
		item = items[st.Index]
		canZoom = itemCanZoom(item)
		activeVideo = itemHasActiveVideo(item)

		if activeVideo {
			HandleVideoShortcuts(c, &layer, item.Video, &st.Video, true, nil)
		} else {
			if canZoom && !st.FitMode && st.Scroll.MaxX > 0.5 {
				if layer.OverlayShortcut(0, ui.KeyLeft) {
					panStage(-lightboxPanStep, 0)
				} else if layer.OverlayShortcut(0, ui.KeyRight) {
					panStage(lightboxPanStep, 0)
				}
			} else if multi {
				if layer.OverlayShortcut(0, ui.KeyLeft) {
					selectItem((st.Index - 1 + len(items)) % len(items))
				} else if layer.OverlayShortcut(0, ui.KeyRight) {
					selectItem((st.Index + 1) % len(items))
				}
			}

			if canZoom && !st.FitMode && st.Scroll.MaxY > 0.5 {
				if layer.OverlayShortcut(0, ui.KeyUp) {
					panStage(0, -lightboxPanStep)
				} else if layer.OverlayShortcut(0, ui.KeyDown) {
					panStage(0, lightboxPanStep)
				}
			} else if multi {
				if layer.OverlayShortcut(0, ui.KeyUp) {
					selectItem((st.Index - 1 + len(items)) % len(items))
				} else if layer.OverlayShortcut(0, ui.KeyDown) {
					selectItem((st.Index + 1) % len(items))
				}
			}

			if item.IsAnimation && item.Video != nil {
				if layer.OverlayShortcut(0, ui.KeySpace) && !c.ShortcutRepeated() {
					st.Video.TogglePlayPause(uiVideoTarget{p: item.Video}, c.Now())
				}
			}
		}

		if canZoom {
			if layer.OverlayShortcut(0, ui.KeyEqual) ||
				layer.OverlayShortcut(ui.Shift, ui.KeyEqual) ||
				layer.OverlayShortcut(ui.Cmd, ui.KeyEqual) ||
				layer.OverlayShortcut(ui.Cmd|ui.Shift, ui.KeyEqual) {
				stepZoom(lightboxZoomStep)
			} else if layer.OverlayShortcut(0, ui.KeyMinus) || layer.OverlayShortcut(ui.Cmd, ui.KeyMinus) {
				stepZoom(1.0 / lightboxZoomStep)
			} else if layer.OverlayShortcut(0, ui.Key0) || layer.OverlayShortcut(ui.Cmd, ui.Key0) {
				setFitZoom()
			} else if layer.OverlayShortcut(0, ui.Key1) {
				setActualZoom()
			}
		}
		if cb.OnCopy != nil && layer.OverlayShortcut(ui.Cmd, ui.KeyC) && (canZoom || item.TextPreview != "" || item.OpenURL != "") {
			if cb.OnCopy(st.Index) {
				st.CopiedUntil = c.Now().Add(1500 * time.Millisecond)
				c.After(1505 * time.Millisecond)
			}
		}

		// Re-resolve active item if keyboard navigation switched index.
		item = items[st.Index]
		displayName = item.Name
		if displayName == "" {
			displayName = theme.Tr("未命名附件", "Untitled attachment")
		}
		canZoom = itemCanZoom(item)
		activeVideo = itemHasActiveVideo(item)

		vpWinW, vpWinH := c.Size()
		tb := c.TitleBar()
		titleClearance := lightboxTitlebarClearance(tb)
		toolbarTopGap := lightboxToolbarTopGap(titleClearance)
		toolbarTopTotal := titleClearance + toolbarTopGap

		var (
			titleStripEl ui.Element
			toolbarEl    ui.Element
			stageEl      ui.Element
			contentEl    ui.Element
			prevBtn      ui.Element
			nextBtn      ui.Element
			filmstripEl  ui.Element
			stageW       float32
			stageH       float32
			stagePadX    = theme.LightboxStagePad
			drawW        float32
			drawH        float32
		)

		layer.Children(func() {
			// 0. Clear OS window controls (macOS traffic lights, Windows/Linux caption buttons)
			//    and keep the empty titlebar band draggable without dismissing the lightbox.
			if titleClearance > 0 {
				titleStripEl = ui.Row(c.Key(id+"-titlebar-strip")).
					FillWidth().Height(titleClearance).Shrink(0).
					Padding(0, tb.Right, 0, tb.Left)
				titleStripEl.Children(func() {
					ui.Box(c).Fill().DragWindow()
				})
			}

			// 1. Top floating Ocean Paper toolbar.
			ui.Row(c).FillWidth().Padding(toolbarTopGap, theme.Space16, 0, theme.Space16).Shrink(0).Children(func() {
				toolbarEl = ui.Row(c.Key(id+"-toolbar")).
					FillWidth().Height(theme.LightboxToolbarHeight).
					PaddingX(theme.Space12).Gap(theme.Space12).
					AlignItems(ui.Center).Justify(ui.SpaceBetween).
					Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.MenuBorder).
					Transition(ModalSpringTransition())
				Elevate(toolbarEl, theme.ElevationDialog)
				Probe(toolbarEl, id+"-toolbar")

				toolbarEl.Children(func() {
					// Left: icon + filename + meta + index counter.
					ui.Row(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
						leadIcon := icons.Note
						if item.IsVideo {
							leadIcon = icons.Video
						} else if item.IsImage || item.IsAnimation {
							leadIcon = icons.Image
						}
						icons.Draw(c, leadIcon, theme.IconAction, pal.DuoIconColors(pal.TextSecondary))

						ui.Text(c, displayName).
							FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
							FontWeight(theme.WeightControl).TextColor(pal.Text).
							SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)

						if item.Meta != "" {
							ui.Text(c, item.Meta).
								FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								TextColor(pal.TextSecondary).SingleLine().Shrink(0)
						}

						if multi {
							countText := fmt.Sprintf("%d / %d", st.Index+1, len(items))
							countEl := ui.Row(c.Key(id + "-count")).
								Height(theme.HeightMicro).PaddingX(theme.Space6).Shrink(0).
								Center().Radius(theme.RadiusSM).Background(pal.Hover)
							countEl.Children(func() {
								ui.Text(c, countText).
									FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
									FontWeight(theme.WeightMono).TextColor(pal.TextSecondary).SingleLine()
							})
							Probe(countEl, id+"-count")
						}
					})

					// Right: zoom cluster (image only) + copy + reveal + remove + close.
					ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
						itemKey := item.ID
						if itemKey == "" {
							itemKey = fmt.Sprintf("idx-%d", st.Index)
						}
						if canZoom {
							minZ := effectiveMinZoom(st.FitScale)
							effectiveZoom := clampLightboxZoom(st.Zoom, minZ)
							if st.FitMode {
								if st.FitScale > 0 {
									effectiveZoom = st.FitScale
								} else if item.ImageW > 0 && item.ImageH > 0 && vpWinW > 0 && vpWinH > 0 {
									estChromeH := toolbarTopTotal + theme.LightboxToolbarHeight + theme.Space12
									estPadX := theme.LightboxStagePad
									if multi {
										estChromeH += theme.LightboxFilmstripThumb + theme.Space12*3 + theme.Space2
										estPadX += theme.HeightLarge
									}
									estAvailW := max(vpWinW-2*estPadX, 1)
									estAvailH := max(vpWinH-estChromeH-2*theme.LightboxStagePad, 1)
									effectiveZoom = min(float32(1.0), min(estAvailW/float32(item.ImageW), estAvailH/float32(item.ImageH)))
									minZ = effectiveMinZoom(effectiveZoom)
								} else {
									effectiveZoom = 1.0
								}
							}
							pctText := fmt.Sprintf("%d%%", int(math.Round(float64(effectiveZoom*100))))

							zoomCluster := ui.Row(c.Key(id+"-zoom")).
								Height(theme.HeightCompact).PaddingX(theme.Space4).Gap(theme.Space2).
								AlignItems(ui.Center).Radius(theme.RadiusMD).
								Background(pal.Card).Border(1, pal.BtnBorder)
							Probe(zoomCluster, id+"-zoom")

							zoomCluster.Children(func() {
								outBtn := IconButton(c, icons.TbMinus, IconButtonOptions{
									ID:       id + "-zoom-out",
									Size:     theme.HeightMicro,
									IconSize: theme.IconInline,
									Tooltip:  theme.Tr("缩小", "Zoom Out"),
									Shortcut: "-",
									Disabled: effectiveZoom <= minZ+0.001,
								})
								if outBtn.Clicked() {
									stepZoom(1.0 / lightboxZoomStep)
								}

								pctBtn := ui.ButtonBase(c.Key(id + "-zoom-pct")).
									Height(theme.HeightMicro).PaddingX(theme.Space6).
									Center().Radius(theme.RadiusXS).Cursor(ui.CursorPointer).Label(pctText)
								if pctBtn.Hovered() {
									pctBtn.Background(pal.Hover)
								}
								RichTooltip(c, pctBtn, TooltipSpec{
									Placement: TooltipBelow,
									Rows: []TooltipRow{{
										Trigger:  theme.Tr("切换 100% / 适应窗口", "Toggle 100% / Fit"),
										Shortcut: "1",
									}},
								})
								pctBtn.Children(func() {
									ui.Text(c, pctText).
										FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
										FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
								})
								Probe(pctBtn, id+"-zoom-pct")
								if pctBtn.Clicked() {
									if st.FitMode {
										setActualZoom()
									} else {
										setFitZoom()
									}
								}

								inBtn := IconButton(c, icons.TbPlus, IconButtonOptions{
									ID:       id + "-zoom-in",
									Size:     theme.HeightMicro,
									IconSize: theme.IconInline,
									Tooltip:  theme.Tr("放大", "Zoom In"),
									Shortcut: "=",
									Disabled: effectiveZoom >= lightboxMaxZoom-0.001,
								})
								if inBtn.Clicked() {
									stepZoom(lightboxZoomStep)
								}

								fitLabel := theme.Tr("适应", "Fit")
								fitBtn := ui.ButtonBase(c.Key(id+"-zoom-fit")).
									Height(theme.HeightMicro).PaddingX(theme.Space6).
									Center().Radius(theme.RadiusXS).Border(1, ui.Transparent).
									Cursor(ui.CursorPointer).Label(fitLabel)
								if st.FitMode {
									fitBtn.Background(pal.Selected).Border(1, pal.SelectedBorder)
								} else if fitBtn.Hovered() {
									fitBtn.Background(pal.Hover)
								}
								RichTooltip(c, fitBtn, TooltipSpec{
									Placement: TooltipBelow,
									Rows: []TooltipRow{{
										Trigger:  theme.Tr("适应窗口", "Fit to Window"),
										Shortcut: "0",
									}},
								})
								fitBtn.Children(func() {
									ui.Text(c, fitLabel).
										FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
										FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
								})
								Probe(fitBtn, id+"-zoom-fit")
								if fitBtn.Clicked() {
									if st.FitMode {
										setActualZoom()
									} else {
										setFitZoom()
									}
								}
							})
						}

						canCopy := canZoom || item.TextPreview != "" || item.OpenURL != ""
						if cb.OnCopy != nil && canCopy {
							now := c.Now()
							copiedActive := !st.CopiedUntil.IsZero() && now.Before(st.CopiedUntil)
							if copiedActive {
								if rem := st.CopiedUntil.Sub(now); rem > 0 {
									c.After(rem + 5*time.Millisecond)
								}
							}
							copyIcon := icons.Copy
							copyTip := theme.Tr("复制", "Copy")
							if item.IsVideo && item.OpenURL != "" {
								copyTip = theme.Tr("复制链接", "Copy Link")
							}
							if copiedActive {
								copyIcon = icons.Check
								copyTip = theme.Tr("已复制", "Copied")
							}
							copyShortcut := ""
							if canZoom || item.OpenURL != "" {
								copyShortcut = "cmd-c"
							}
							copyBtn := IconButton(c, copyIcon, IconButtonOptions{
								ID:       id + "-copy-" + itemKey,
								Variant:  IconChrome,
								Tooltip:  copyTip,
								Shortcut: copyShortcut,
								Disabled: item.Unavailable || item.Loading,
								Active:   copiedActive,
							})
							Probe(copyBtn, id+"-copy")
							if copyBtn.Clicked() {
								if cb.OnCopy(st.Index) {
									st.CopiedUntil = c.Now().Add(1500 * time.Millisecond)
									c.After(1505 * time.Millisecond)
								}
							}
						}

						if cb.OnOpenURL != nil && item.OpenURL != "" {
							openBtn := IconButton(c, icons.LinkOpen, IconButtonOptions{
								ID:      id + "-open-url-" + itemKey,
								Variant: IconChrome,
								Tooltip: theme.Tr("在浏览器中打开", "Open in Browser"),
							})
							Probe(openBtn, id+"-open-url")
							if openBtn.Clicked() {
								cb.OnOpenURL(st.Index)
							}
						}

						if cb.OnReveal != nil && !item.Unavailable {
							revBtn := IconButton(c, icons.FolderReveal, IconButtonOptions{
								ID:      id + "-reveal-" + itemKey,
								Variant: IconChrome,
								Tooltip: lightboxRevealLabel(),
							})
							Probe(revBtn, id+"-reveal")
							if revBtn.Clicked() {
								cb.OnReveal(st.Index)
							}
						}

						// Contextual keyboard/mouse shortcuts help button.
						helpBtn := IconButton(c, icons.HelpCircle, IconButtonOptions{
							ID:      id + "-help",
							Variant: IconChrome,
							Label:   theme.Tr("快捷键说明", "Keyboard shortcuts"),
						})
						if activeVideo {
							switchChord := FormatShortcutDisplay("cmd-left") + " / " + FormatShortcutDisplay("cmd-right")
							TooltipCard(c, helpBtn, TooltipCardSpec{
								Title: theme.Tr("视频快捷键", "Video Shortcuts"),
								Body: theme.Tr(
									"空格：播放/暂停 · ← / →：后退/快进 5 秒\n按住 → 或鼠标右键：3x 倍速，松开恢复原速\n↑ / ↓：调节音量 · M：静音 · R：循环 · < / >：倍速",
									"Space: Play/Pause · ← / →: Seek ±5s\nHold → or Right-click: 3x speed, release to restore\n↑ / ↓: Volume · M: Mute · R: Loop · < / >: Speed",
								),
								Note: fmt.Sprintf(
									theme.Tr("双击视频切换填充模式 · %s 切换媒体", "Double-click to toggle fill · %s switch media"),
									switchChord,
								),
								Placement: TooltipBelow,
							})
						} else {
							TooltipCard(c, helpBtn, TooltipCardSpec{
								Title: theme.Tr("预览快捷键", "Viewer Shortcuts"),
								Body: theme.Tr(
									"点击图片：切换 100% / 适应窗口 · 拖拽平移大图\n+ / - / 0 / 1：缩放与复位 · 方向键：平移或切换",
									"Click image: Toggle 100% / Fit · Drag to pan when zoomed\n+ / - / 0 / 1: Zoom & reset · Arrows: Pan or switch",
								),
								Note:      theme.Tr("Esc 关闭预览", "Esc to close"),
								Placement: TooltipBelow,
							})
						}

						if item.CanRemove && cb.OnRemove != nil {
							remBtn := IconButton(c, icons.Trash, IconButtonOptions{
								ID:      id + "-remove-" + itemKey,
								Variant: IconChrome,
								Danger:  true,
								Tooltip: theme.Tr("移除附件", "Remove Attachment"),
							})
							Probe(remBtn, id+"-remove")
							if remBtn.Clicked() {
								cb.OnRemove(st.Index)
							}
						}

						closeBtn := IconButton(c, icons.Close, IconButtonOptions{
							ID:       id + "-close",
							Variant:  IconChrome,
							Tooltip:  theme.Tr("关闭", "Close"),
							Shortcut: "esc",
						})
						if closeBtn.Clicked() {
							closeLightbox()
						}
					})
				})
			})

			// 2. Central stage + floating prev/next navigation.
			stageWrap := ui.Box(c.Key(id + "-stage-wrap")).
				Grow(1).Shrink(1).FillWidth().MinWidth(0).MinHeight(0)

			if b := stageWrap.LayoutBounds(); b.W > 0 && b.H > 0 {
				stageW, stageH = b.W, b.H
			} else {
				stageW = max(vpWinW, 1)
				chromeH := toolbarTopTotal + theme.LightboxToolbarHeight + theme.Space12
				if multi {
					chromeH += theme.LightboxFilmstripThumb + theme.Space12*3 + theme.Space2
				}
				stageH = max(vpWinH-chromeH, 1)
			}

			if multi {
				stagePadX = theme.LightboxStagePad + theme.HeightLarge
			}

			stageWrap.Children(func() {
				stageEl = ScrollBoth(c.Key(id+"-stage"), &st.Scroll).Fill().MinWidth(0).MinHeight(0)
				Probe(stageEl, id+"-stage")

				stageEl.Children(func() {
					ui.Column(c).
						MinWidth(stageW).MinHeight(stageH).
						Padding(theme.LightboxStagePad, stagePadX, theme.LightboxStagePad, stagePadX).
						Center().
						Children(func() {
							switch {
							case item.Loading:
								msg := theme.Tr("正在加载预览…", "Loading preview…")
								if item.IsVideo {
									msg = theme.Tr("正在加载视频…", "Loading video…")
								}
								contentEl = renderLightboxStatusCard(c, id+"-card", pal, displayName, item.Meta,
									msg, true, item.IsImage || item.IsAnimation, item.IsVideo, nil)

							case item.Unavailable || (item.IsImage && !item.IsAnimation && item.Image == nil) || (item.IsAnimation && item.Video == nil):
								msg := theme.Tr("附件文件不可用或已被移动", "Attachment is unavailable or was moved")
								if item.IsVideo {
									msg = theme.Tr("视频无法播放", "Video unavailable")
								} else if item.IsImage || item.IsAnimation {
									msg = theme.Tr("图片无法加载", "Image unavailable")
								}
								var onOpen func()
								if cb.OnOpenURL != nil && item.OpenURL != "" {
									idxCopy := st.Index
									onOpen = func() { cb.OnOpenURL(idxCopy) }
								}
								contentEl = renderLightboxStatusCard(c, id+"-card", pal, displayName, item.Meta,
									msg, false, item.IsImage || item.IsAnimation, item.IsVideo, onOpen)

							case item.IsVideo && item.Video == nil:
								if cb.OnRequestLoad != nil {
									cb.OnRequestLoad(st.Index)
								}
								contentEl = renderLightboxStatusCard(c, id+"-card", pal, displayName, item.Meta,
									theme.Tr("正在加载视频…", "Loading video…"), true, false, true, nil)

							case activeVideo:
								vidKey := item.ID
								if vidKey == "" {
									vidKey = fmt.Sprintf("idx-%d", st.Index)
								}
								vidWrap := ui.Column(c.Key(id+"-video-wrap-"+vidKey)).
									MaxWidth(min(max(stageW-2*stagePadX, 320), theme.LightboxVideoMaxWidth)).
									Shrink(0)
								vidWrap.Children(func() {
									RenderVideoPlayer(c, item.Video, &st.Video, VideoPlayerOptions{
										ID:          id + "-video-" + vidKey,
										Label:       displayName,
										StageWidth:  min(max(stageW-2*stagePadX, 320), theme.LightboxVideoMaxWidth),
										StageHeight: max(stageH-2*theme.LightboxStagePad, 220),
										InLightbox:  true,
										FillMode:    st.VideoFillMode,
										OnToggleFill: func() {
											st.VideoFillMode = !st.VideoFillMode
										},
									})
								})
								contentEl = vidWrap

							case canZoom:
								iw, ih := float32(item.ImageW), float32(item.ImageH)
								if item.IsAnimation && item.Video != nil {
									vw, vh := item.Video.Size()
									if iw <= 0 && vw > 0 {
										iw = float32(vw)
									}
									if ih <= 0 && vh > 0 {
										ih = float32(vh)
									}
								} else if iw <= 0 || ih <= 0 {
									sw, sh := ui.ImageSize(item.Image)
									if iw <= 0 {
										iw = sw
									}
									if ih <= 0 {
										ih = sh
									}
								}
								if iw <= 0 {
									iw = 320
								}
								if ih <= 0 {
									ih = 240
								}

								frameChrome := (theme.Space2 + 1) * 2
								availW := max(stageW-2*stagePadX-frameChrome, 1)
								availH := max(stageH-2*theme.LightboxStagePad-frameChrome, 1)
								fitScale := min(float32(1.0), min(availW/iw, availH/ih))
								if fitScale <= 0 {
									fitScale = 0.01
								}
								st.FitScale = fitScale

								minZ := effectiveMinZoom(fitScale)
								effectiveZoom := clampLightboxZoom(st.Zoom, minZ)
								if st.FitMode {
									effectiveZoom = fitScale
								} else {
									st.Zoom = effectiveZoom
								}
								drawW = max(iw*effectiveZoom, 4)
								drawH = max(ih*effectiveZoom, 4)

								overflows := drawW > availW+1 || drawH > availH+1
								cursor := ui.CursorZoomIn
								if !st.FitMode {
									if overflows {
										if st.panDragging {
											cursor = ui.CursorGrabbing
										} else {
											cursor = ui.CursorGrab
										}
									} else {
										cursor = ui.CursorZoomOut
									}
								}
								imgKey := item.ID
								if imgKey == "" {
									imgKey = fmt.Sprintf("idx-%d", st.Index)
								}
								frame := ui.ButtonBase(c.Key(id+"-image-"+imgKey)).
									Shrink(0).Padding(theme.Space2).
									Radius(theme.RadiusLG).
									Background(pal.AISurface().MediaFrameBG).
									Border(1, pal.Hairline).
									Cursor(cursor).Label(displayName)
								Elevate(frame, theme.ElevationDialog)
								Probe(frame, id+"-image")
								frame.Children(func() {
									if item.IsAnimation && item.Video != nil {
										ui.Video(c, item.Video).Size(drawW, drawH).Shrink(0).
											Radius(theme.RadiusMD).Clip().Label(displayName)
									} else {
										ui.Image(c, item.Image).Size(drawW, drawH).Shrink(0).
											Radius(theme.RadiusMD).Clip()
									}
								})
								dx, dy, dragging := frame.Dragged()
								if overflows && dragging {
									if !st.panDragging {
										st.panDragging = true
										st.panMoved = false
									}
									if math.Abs(float64(dx)) > 0.5 || math.Abs(float64(dy)) > 0.5 {
										st.panMoved = true
										st.Scroll.X = min(max(st.Scroll.X-dx, 0), max(st.Scroll.MaxX, 0))
										st.Scroll.Y = min(max(st.Scroll.Y-dy, 0), max(st.Scroll.MaxY, 0))
									}
								} else if st.panDragging {
									st.panDragging = false
								}
								if frame.Clicked() {
									if st.panMoved {
										st.panMoved = false
									} else {
										if st.FitMode {
											st.FitMode = false
											if math.Abs(float64(fitScale-1.0)) < 0.05 {
												st.Zoom = 2.0
											} else {
												st.Zoom = 1.0
											}
										} else {
											st.FitMode = true
											st.Zoom = 1.0
										}
										st.Scroll = ui.ScrollState{}
									}
								}
								contentEl = frame

							default:
								contentEl = renderLightboxFileCard(c, id, st, item, cb, pal, displayName, stageW-2*stagePadX, stageH-2*theme.LightboxStagePad)
							}
						})
				})

				if multi {
					navSize := theme.HeightLarge
					navTop := max((stageH-navSize)*0.5, theme.Space8)
					prevShortcut := "←"
					nextShortcut := "→"
					if activeVideo {
						prevShortcut = "cmd-left"
						nextShortcut = "cmd-right"
					}

					prevBtn = IconButton(c, icons.ChevronLeft, IconButtonOptions{
						ID:       id + "-prev",
						Variant:  IconBordered,
						Size:     navSize,
						IconSize: theme.IconChrome,
						Radius:   theme.RadiusPill,
						Tooltip:  theme.Tr("上一个", "Previous"),
						Shortcut: prevShortcut,
					}).Absolute().Left(theme.Space16).Top(navTop)
					Elevate(prevBtn, theme.ElevationDialog)
					if prevBtn.Clicked() {
						selectItem((st.Index - 1 + len(items)) % len(items))
					}

					nextBtn = IconButton(c, icons.ChevronRight, IconButtonOptions{
						ID:       id + "-next",
						Variant:  IconBordered,
						Size:     navSize,
						IconSize: theme.IconChrome,
						Radius:   theme.RadiusPill,
						Tooltip:  theme.Tr("下一个", "Next"),
						Shortcut: nextShortcut,
					}).Absolute().Right(theme.Space16).Top(navTop)
					Elevate(nextBtn, theme.ElevationDialog)
					if nextBtn.Clicked() {
						selectItem((st.Index + 1) % len(items))
					}
				}
			})

			// 3. Bottom compact filmstrip when multiple attachments exist.
			if multi {
				ui.Row(c).FillWidth().Padding(0, theme.Space16, theme.Space12, theme.Space16).
					Justify(ui.Center).Shrink(0).Children(func() {
					filmstripEl = ui.Row(c.Key(id+"-filmstrip")).
						MaxWidth(max(vpWinW-2*theme.Space16, 1)).
						Padding(theme.Space6).
						Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.MenuBorder).
						Transition(ModalSpringTransition())
					Elevate(filmstripEl, theme.ElevationDialog)
					Probe(filmstripEl, id+"-filmstrip")

					filmstripEl.Children(func() {
						stripScroll := ScrollHorizontal(c.Key(id + "-filmstrip-scroll")).Shrink(1).MinWidth(0)
						stripScroll.Children(func() {
							ui.Row(c).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
								for i, it := range items {
									idx := i
									isSelected := idx == st.Index
									thumbID := fmt.Sprintf("%s-thumb-%d", id, idx)
									thumbLabel := it.Name
									if thumbLabel == "" {
										thumbLabel = fmt.Sprintf("#%d", idx+1)
									}

									borderW := float32(1)
									borderCol := pal.Hairline
									if isSelected {
										borderW = 2
										borderCol = pal.Accent
									}

									thumb := ui.ButtonBase(c.Key(thumbID)).
										Size(theme.LightboxFilmstripThumb, theme.LightboxFilmstripThumb).Shrink(0).
										Center().Radius(theme.RadiusMD).
										Background(pal.AISurface().MediaFrameBG).
										Border(borderW, borderCol).Clip().
										Cursor(ui.CursorPointer).
										Role(ui.RoleTab).AccessSelected(isSelected).Label(thumbLabel)
									if !isSelected && thumb.Hovered() {
										thumb.Border(borderW, pal.TextSecondary)
									}
									TactileCard(thumb, thumb.Hovered(), thumb.Pressed(), isSelected, false)

									thumb.Children(func() {
										switch {
										case it.IsImage && !it.IsAnimation && it.Image != nil && !it.Unavailable:
											ui.Image(c, it.Image).Fill().Fit(ui.Cover).Clip()
										case it.Loading:
											ui.Spinner(c).FontSize(theme.IconInline).TextColor(pal.Accent)
										case it.IsVideo:
											icons.Duo(c, icons.Video, theme.IconRow, isSelected)
										default:
											glyph := icons.Note
											if it.IsImage || it.IsAnimation {
												glyph = icons.Image
											}
											icons.Duo(c, glyph, theme.IconRow, isSelected)
										}
									})
									Probe(thumb, thumbID)
									if isSelected && indexChanged {
										thumb.ScrollIntoView()
									}
									if thumb.Clicked() {
										selectItem(idx)
									}
								}
							})
						})
					})
				})
			}
		})

		clickedInteractive := (titleStripEl.Valid() && titleStripEl.PressedInside()) ||
			(toolbarEl.Valid() && toolbarEl.PressedInside()) ||
			(contentEl.Valid() && contentEl.PressedInside()) ||
			(prevBtn.Valid() && prevBtn.PressedInside()) ||
			(nextBtn.Valid() && nextBtn.PressedInside()) ||
			(filmstripEl.Valid() && filmstripEl.PressedInside())

		stageOverflow := canZoom && (drawW > max(stageW-2*stagePadX, 1) || drawH > max(stageH-2*theme.LightboxStagePad, 1))
		overStageScrollbar := stageOverflow && stageEl.Valid() &&
			pointerOverScrollbarTrack(c, stageEl, axisVertical|axisHorizontal)

		if local.mountedFrames > 0 && (layer.PressedInside() || scrim.PressedInside()) && !clickedInteractive && !overStageScrollbar {
			closeLightbox()
		}
		local.mountedFrames++
	})
}

func renderLightboxStatusCard(
	c *ui.Context,
	cardID string,
	pal theme.Palette,
	name string,
	meta string,
	message string,
	loading bool,
	isImage bool,
	isVideo bool,
	onOpenURL func(),
) ui.Element {
	card := ui.Column(c.Key(cardID)).
		Width(theme.EmptyActionWidth).
		Padding(theme.Space24, theme.Space24, theme.Space24, theme.Space24).
		Gap(theme.Space10).AlignItems(ui.Center).
		Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.MenuBorder).
		Transition(ModalSpringTransition())
	Elevate(card, theme.ElevationDialog)
	Probe(card, cardID)

	card.Children(func() {
		if loading {
			SparkleLoader(c, theme.IconChrome, pal.Accent, pal.AccentHover)
		} else {
			glyph := icons.Note
			if isVideo {
				glyph = icons.Video
			} else if isImage {
				glyph = icons.Image
			}
			icons.Duo(c, glyph, theme.EmptyMascotCompactSize, false)
		}
		ui.Text(c, name).
			FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
			FontWeight(theme.WeightTitle).TextColor(pal.Text).
			SingleLine().Ellipsis("…").MaxWidth(theme.EmptyActionWidth - theme.Space24*2)
		if meta != "" {
			ui.Text(c, meta).
				FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				TextColor(pal.TextSecondary).SingleLine()
		}
		ui.Text(c, message).
			FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			TextColor(pal.TextSecondary)
		if !loading && onOpenURL != nil {
			ic := icons.LinkOpen
			btn := ActionButton(c, cardID+"-open-url", theme.Tr("在浏览器中打开", "Open in Browser"), &ic, ButtonSecondary, false)
			if btn.Clicked() {
				onOpenURL()
			}
		}
	})
	return card
}

func renderLightboxFileCard(
	c *ui.Context,
	id string,
	st *LightboxState,
	item LightboxItem,
	cb LightboxCallbacks,
	pal theme.Palette,
	displayName string,
	maxStageW float32,
	maxStageH float32,
) ui.Element {
	cardID := id + "-card"
	if item.TextPreview != "" {
		cardW := min(theme.LightboxMaxTextWidth, max(maxStageW, 1))
		cardH := min(theme.LightboxMaxTextHeight, max(maxStageH, 1))
		card := ui.Column(c.Key(cardID)).
			Width(cardW).MaxHeight(cardH).Shrink(0).
			Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.MenuBorder).
			Clip().Transition(ModalSpringTransition())
		Elevate(card, theme.ElevationDialog)
		Probe(card, cardID)

		card.Children(func() {
			textScrollKey := fmt.Sprintf("%s-text-scroll-%d", id, st.Index)
			textScroll := Scroll(c.Key(textScrollKey)).FillWidth().Grow(1).MinHeight(0)
			Probe(textScroll, id+"-text-scroll")
			textScroll.Children(func() {
				ui.Column(c).FillWidth().Padding(theme.Space16, theme.Space20, theme.Space16, theme.Space20).Children(func() {
					ui.Text(c, item.TextPreview).
						FillWidth().MinWidth(0).
						Font("monospace").
						FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
						FontWeight(theme.WeightMono).TextColor(pal.Text).
						Selectable()
				})
			})

			if item.TextTruncated {
				ui.Box(c).FillWidth().Height(1).Shrink(0).Background(pal.Hairline)
				ui.Row(c).FillWidth().Padding(theme.Space8, theme.Space16, theme.Space8, theme.Space16).
					Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween).
					Background(pal.Shell).Shrink(0).Children(func() {
					ui.Text(c, theme.Tr("预览已截断，仅展示部分内容", "Preview truncated; showing partial content")).
						FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(pal.TextSecondary).SingleLine().Shrink(1).MinWidth(0)

					if cb.OnReveal != nil && !item.Unavailable {
						revIc := icons.FolderReveal
						revBtn := ActionButtonWithOptions(c, ActionButtonOptions{
							ID:       id + "-text-reveal",
							Label:    lightboxRevealLabel(),
							Leading:  &revIc,
							Style:    ButtonGhost,
							Height:   theme.HeightMicro,
							PaddingX: theme.Space8,
						})
						if revBtn.Clicked() {
							cb.OnReveal(st.Index)
						}
					}
				})
			}
		})
		return card
	}

	cardW := min(theme.EmptyActionWidth, max(maxStageW, 1))
	card := ui.Column(c.Key(cardID)).
		Width(cardW).Shrink(0).
		Padding(theme.Space24, theme.Space24, theme.Space24, theme.Space24).
		Gap(theme.Space10).AlignItems(ui.Center).
		Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.MenuBorder).
		Transition(ModalSpringTransition())
	Elevate(card, theme.ElevationDialog)
	Probe(card, cardID)

	card.Children(func() {
		icons.Duo(c, icons.Note, theme.EmptyMascotCompactSize, true)
		ui.Text(c, displayName).
			FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
			FontWeight(theme.WeightTitle).TextColor(pal.Text).
			SingleLine().Ellipsis("…").MaxWidth(max(cardW-theme.Space24*2, 1))
		if item.Meta != "" {
			ui.Text(c, item.Meta).
				FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				TextColor(pal.TextSecondary).SingleLine()
		}
		ui.Text(c, theme.Tr("此文件暂不支持直接预览，可打开所在目录查看。", "Preview unavailable for this file type; open its folder to inspect.")).
			FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
			TextColor(pal.TextSecondary)

		if cb.OnReveal != nil && !item.Unavailable {
			ic := icons.FolderReveal
			revBtn := ActionButton(c, id+"-text-reveal", lightboxRevealLabel(), &ic, ButtonSecondary, false)
			if revBtn.Clicked() {
				cb.OnReveal(st.Index)
			}
		}
	})
	return card
}
