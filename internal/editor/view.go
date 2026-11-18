package editor

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	editorHandleWidth float32 = 54
	editorRowGap      float32 = 14
	editorRightInset  float32 = editorHandleWidth + editorRowGap
	editorMarkerWidth float32 = 29.68
	editorIndentStep  float32 = editorMarkerWidth / 2
	editorTopPad      float32 = 64
	editorBottomPad   float32 = 160
)

// Render draws the block editor, its inline blocks, and all floating overlays/rails.
func (s *Session) Render(c *ui.Context, viewportWidth float32, readOnly bool) {
	if s.doc == nil {
		s.doc = model.NewEmptyDoc()
	}
	if len(s.doc.Blocks) == 0 {
		s.doc = model.NewEmptyDoc()
	}
	s.readOnly = readOnly
	s.ensureAnnotationsLoaded()
	s.pollInlineAI()
	if s.copyPending {
		s.copyPending = false
		if md := s.SelectedMarkdown(); md != "" {
			c.WriteClipboard(md)
		}
	}

	winW, winH := c.Size()
	if viewportWidth <= 0 {
		viewportWidth = winW
	}
	if viewportWidth <= 0 {
		viewportWidth = 1024
	}
	if winH <= 0 {
		winH = 720
	}
	s.viewportWidth = viewportWidth
	s.viewportHeight = winH

	// Expire outline jump highlight only if wall clock passed and not in a static frame.
	if s.highlightedBlockID != 0 && !s.highlightUntil.IsZero() && timeAfter(s.highlightUntil) {
		s.highlightedBlockID = 0
		s.highlightUntil = s.highlightUntil.Add(-s.highlightUntil.Sub(s.highlightUntil))
	}

	s.handleGlobalShortcuts(c, readOnly)

	pal := theme.Ocean()
	root := ui.Box(c.Key("editor-root")).Fill().Background(pal.Paper).Bind(&s.rootHandle)
	s.frameRoot = root
	defer func() { s.frameRoot = ui.Element{} }()

	root.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		s.editorRect = r
		appUi.RecordBounds("editor", r)
	})

	s.preUpdateBlockDrag(c)
	s.preUpdateFocusedDrag(c, root)

	root.Children(func() {
		topPad := s.scale(editorTopPad)
		bottomPad := s.scale(editorBottomPad)
		bottomMinH := s.scale(120)
		if s.minimal {
			topPad = minimalTopPad
			bottomPad = minimalBottomPad
			bottomMinH = 36
		}

		// Main scrollable document body
		scroll := appUi.Scroll(c.Key("editor-scroll"), &s.bodyScroll).Fill().
			ObserveInput(s.onScrollObserveInput)
		scroll.DrawOver(func(_ *ui.Painter, r ui.Rect) {
			if s.scrollRect != r {
				prevH := s.scrollRect.H
				s.scrollRect = r
				if prevH != r.H {
					c.Invalidate()
				}
			}
		})
		scroll.Children(func() {
			// Center column: max 672px body + symmetric 68px left/right gutters (54px handle gutter + 14px gap) = 808px (scaled by editor zoom).
			outerMaxW := s.outerFlowWidth()
			col := ui.Column(c.Key("editor-canvas")).FillWidth().AlignItems(ui.Center).
				Padding(topPad, 0, bottomPad, 0)
			if minCanvasH := max(s.minCanvasHeight, s.scrollRect.H); minCanvasH > 0 {
				col.MinHeight(minCanvasH)
			}
			if !readOnly {
				col.Cursor(ui.CursorText)
			}
			s.updateCanvasMouseSelection(c, root, col, readOnly)

			col.Children(func() {
				// Top margin spacer preserved for layout symmetry; clicks & marquee drags handled by editor-canvas.
				if !readOnly {
					ui.Box(c.Key("editor-top-margin")).Absolute().Top(0).Left(0).Right(0).Height(topPad).
						Cursor(ui.CursorText)
				}

				flow := ui.Column(c.Key("editor-flow")).FillWidth().MaxWidth(outerMaxW)
				flow.Children(func() {
					virtSig := viewportWidth*1000 + s.zoom()
					canVirt := !appUi.IsProbeEnabled() && s.virtReady && s.lastVirtSig == virtSig && len(s.doc.Blocks) > 60 && s.scrollRect.H > 0
					s.lastVirtSig = virtSig
					s.virtReady = true

					frameMeta := s.prepareFrameBlocks()
					nextHover := -1
					for i := range s.doc.Blocks {
						if frameMeta[i].Hidden {
							continue
						}
						if handled, hov := s.renderInlineAIPreviewRow(c, i, readOnly); handled {
							if hov {
								nextHover = i
							}
							continue
						}
						if canVirt {
							if cachedR, ok := s.canVirtualizeBlock(i, s.doc.Blocks[i].ID); ok {
								id := s.doc.Blocks[i].ID
								sp := ui.Box(c.Key(id)).FillWidth().Height(cachedR.H)
								sp.DrawOver(func(_ *ui.Painter, r ui.Rect) {
									s.blockRects[id] = r
								})
								continue
							}
						}
						hovered := s.renderBlockRowMeta(c, i, readOnly, frameMeta[i])
						if hovered {
							nextHover = i
						}
					}
					nextHover = s.updateTrailingAddHover(c, root, nextHover, readOnly)
					if nextHover != s.hoveredBlock {
						s.hoveredBlock = nextHover
						c.Invalidate()
					}
				})
				s.flushPendingBlockDrop(c)

				// Bottom blank space: clicking creates/focuses a trailing paragraph; dragging box-selects blocks.
				if !readOnly {
					ui.Box(c.Key("editor-bottom-margin")).FillWidth().MaxWidth(outerMaxW).
						MinHeight(bottomMinH).Grow(1).Cursor(ui.CursorText)
				}
			})
		})

		if s.needsDetachedSelectionFocus() {
			multiFocus := ui.Box(c.Key("multi-sel-focus")).Absolute().Left(0).Top(0).Size(0, 0).
				Focusable().TextCaret(ui.Rect{W: 1, H: 16}).
				HandleInput(func(ev ui.InputEvent) bool {
					return s.handleMultiSelectionInput(c, ev)
				})
			if s.multiSelFocusPending {
				multiFocus.Focus(ui.FocusOptions{PreventScroll: true})
				s.multiSelFocusPending = false
			}
		}

		// Render floating overlays & margin rails
		s.renderTrailingAddOverlay(c, readOnly)
		s.renderBoxSelectMarquee(c)
		s.renderFindBar(c)
		s.renderSelectionToolbar(c)
		s.renderBlockMenuOverlay(c)
		s.renderCodeLangMenuOverlay(c)
		s.renderCalloutMenuOverlay(c)
		s.renderTableMenuOverlay(c)
		s.renderInlineAIPanel(c)
		s.renderOutlineRail(c)
		s.renderAnnotationRail(c)
		s.renderPlanDiffCard(c)
	})
}

func (s *Session) focusOrInsertBottomParagraph() {
	s.insertOrFocusBottomParagraph()
}

func (s *Session) handleGlobalShortcuts(c *ui.Context, readOnly bool) {
	if c.Shortcut(ui.Cmd, ui.KeyC) && s.inlineAI == nil && !s.findOpen && s.hasNonEmptySelection() {
		if md := s.SelectedMarkdown(); md != "" {
			c.WriteClipboard(md)
		}
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		s.OpenFind()
	}
	if c.Shortcut(0, ui.KeyEscape) {
		if s.dragBlockID != 0 {
			s.cancelBlockDrag()
		} else if s.inlineAI != nil {
			s.aiEscape()
		} else if s.findOpen {
			s.CloseFind()
		} else if s.hasAnyOverlayOpen() {
			s.closeTransientOverlays()
		}
	}
	if readOnly {
		return
	}
	if s.rootHandle.FocusWithin(c) && !s.hasBlockingOverlay() {
		for _, mods := range []ui.Modifiers{ui.Alt, ui.Cmd | ui.Alt} {
			if c.Shortcut(mods, ui.KeyUp) {
				s.rangeShortcut(mods, ui.KeyUp)
				return
			}
			if c.Shortcut(mods, ui.KeyDown) {
				s.rangeShortcut(mods, ui.KeyDown)
				return
			}
		}
		if c.Shortcut(ui.Cmd, ui.KeyD) {
			s.rangeShortcut(ui.Cmd, ui.KeyD)
			return
		}
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ) || c.Shortcut(ui.Cmd, ui.KeyY) {
		s.Redo()
	} else if c.Shortcut(ui.Cmd, ui.KeyZ) {
		s.Undo()
	}
	if c.Shortcut(ui.Cmd, ui.KeyB) {
		s.ToggleMarkOnSelection(model.MarkBold)
	}
	if c.Shortcut(ui.Cmd, ui.KeyI) {
		s.ToggleMarkOnSelection(model.MarkItalic)
	}
	if c.Shortcut(ui.Cmd, ui.KeyU) {
		s.ToggleMarkOnSelection(model.MarkUnderline)
	}
	if c.Shortcut(ui.Cmd, ui.KeyE) {
		s.ToggleMarkOnSelection(model.MarkCode)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyX) {
		s.ToggleMarkOnSelection(model.MarkStrike)
	}
	if c.Shortcut(ui.Cmd, ui.KeyX) && s.inlineAI == nil && !s.findOpen && (s.hasMultiBlockSelection() || s.hasWholeAtomicSelection()) {
		s.CutSelection(c)
	}
	if c.Shortcut(ui.Cmd, ui.KeyV) && s.inlineAI == nil && !s.findOpen {
		s.PasteFromClipboard(c.Services(), c.ReadClipboard(), false)
	}
	if c.Shortcut(ui.Cmd, ui.KeyA) && s.inlineAI == nil && !s.findOpen && s.needsDetachedSelectionFocus() {
		s.SelectAllBlocks()
	}
	s.handleGlobalBlockShortcuts(c)
}

func blockMargins(b *model.Block) (top, bottom float32) {
	if b == nil {
		return 9.6, 9.6
	}
	switch b.Kind {
	case model.BlockHeading1:
		return 0, 8
	case model.BlockHeading2:
		return 35.2, 11
	case model.BlockHeading3:
		return 25.2, 7.2
	case model.BlockHeading4:
		return 20.8, 4.8
	case model.BlockHeading5, model.BlockHeading6:
		return 16.8, 3.5
	case model.BlockQuote:
		return 12.8, 12.8
	case model.BlockDivider:
		return 16.5, 16.5
	default:
		return 9.6, 9.6
	}
}

func blockGutterSpacing(prev, cur *model.Block) float32 {
	if prev == nil {
		return 0
	}
	if prev.Kind.IsList() && cur.Kind.IsList() {
		return 4
	}
	_, above := blockMargins(prev)
	below, _ := blockMargins(cur)
	if prev.Kind == model.BlockHeading1 && !cur.Kind.IsHeading() && cur.Kind != model.BlockDivider && cur.Kind != model.BlockTable {
		below = 0
	}
	if prev.Kind == model.BlockTable {
		nextMT := below
		if cur.Kind == model.BlockTable {
			nextMT = 18
		}
		return max(10+nextMT-24, 0)
	}
	if cur.Kind == model.BlockTable {
		return max(above, 2) + 16
	}
	return max(above, below)
}

func minimalBlockMargins(b *model.Block) (top, bottom float32) {
	if b == nil {
		return 8, 8
	}
	switch b.Kind {
	case model.BlockHeading1:
		return 0, 6
	case model.BlockHeading2:
		return 18, 6
	case model.BlockHeading3:
		return 14, 5
	case model.BlockHeading4:
		return 12, 4
	case model.BlockHeading5, model.BlockHeading6:
		return 10, 3
	case model.BlockQuote:
		return 10, 10
	case model.BlockDivider:
		return 12, 12
	default:
		return 8, 8
	}
}

func (s *Session) blockGap(prev, cur *model.Block) float32 {
	if !s.minimal {
		return blockGutterSpacing(prev, cur) * s.zoom()
	}
	if prev == nil {
		return 0
	}
	if prev.Kind.IsList() && cur.Kind.IsList() {
		return 4
	}
	_, above := minimalBlockMargins(prev)
	below, _ := minimalBlockMargins(cur)
	if prev.Kind == model.BlockHeading1 && !cur.Kind.IsHeading() && cur.Kind != model.BlockDivider && cur.Kind != model.BlockTable {
		below = 0
	}
	if prev.Kind == model.BlockTable {
		nextMT := below
		if cur.Kind == model.BlockTable {
			nextMT = 14
		}
		return max(10+nextMT-24, 0)
	}
	if cur.Kind == model.BlockTable {
		return max(above, 2) + 12
	}
	return max(above, below)
}

func (s *Session) canVirtualizeBlock(idx int, id model.BlockID) (ui.Rect, bool) {
	if idx == 0 || idx == len(s.doc.Blocks)-1 || idx == s.focusedBlock || idx == s.hoveredBlock {
		return ui.Rect{}, false
	}
	if s.dragBlockID != 0 || s.isMouseDragging() || s.hasNonEmptySelection() || s.highlightedBlockID == id {
		return ui.Rect{}, false
	}
	if s.inlineAI != nil || s.blockMenu != nil || s.findOpen || len(s.annotations) > 0 {
		return ui.Rect{}, false
	}
	if s.calloutMenuBlock == id || s.codeLangMenuBlock == id || s.tableMenuBlock == id || s.imagePopBlock == id || s.imgResizeBlock == id {
		return ui.Rect{}, false
	}
	r, ok := s.blockRects[id]
	if !ok || r.H <= 0 {
		return ui.Rect{}, false
	}
	overscan := max(s.scrollRect.H*1.5, 900)
	if r.Y+r.H < s.scrollRect.Y-overscan || r.Y > s.scrollRect.Y+s.scrollRect.H+overscan {
		return r, true
	}
	return ui.Rect{}, false
}

func (s *Session) renderBlockRow(c *ui.Context, idx int, readOnly bool) bool {
	var prev *model.Block
	for p := idx - 1; p >= 0; p-- {
		if !s.isBlockHiddenByFold(p) {
			prev = &s.doc.Blocks[p]
			break
		}
	}
	return s.renderBlockRowMeta(c, idx, readOnly, frameBlockMeta{
		PrevVisible: prev,
		List:        s.computeListCtx(idx),
	})
}

func (s *Session) renderBlockRowMeta(c *ui.Context, idx int, readOnly bool, meta frameBlockMeta) bool {
	pal := theme.Ocean()
	b := &s.doc.Blocks[idx]
	id := b.ID

	topGap := s.blockGap(meta.PrevVisible, b)
	bandBleed := s.bandBleed()
	rowBleed := float32(0)
	if isSurfaceBleedBlock(b.Kind) {
		rowBleed = bandBleed
	}
	leftPad := float32(0)
	if s.minimal {
		leftPad = max(minimalLeftInset-rowBleed, 0)
	}
	rightPad := max(s.rightContentInset()-rowBleed, 0)

	row := ui.Column(c.Key(id)).FillWidth().Padding(topGap, rightPad, 0, leftPad)
	if !readOnly && b.Kind != model.BlockImage && b.Kind != model.BlockDivider {
		row.Cursor(ui.CursorText)
	}
	isHovered := row.Hovered()
	isFocused := s.focusedBlock == idx
	isHighlighted := s.highlightedBlockID == id
	deleteArmed := idx == 0 && s.firstLineDeleteArmed && s.canArmFirstLineDelete()

	dragTarget := s.dragActive && s.dragOverIdx == idx
	if isHighlighted {
		leftGutter := s.leftContentGutter()
		rightInset := s.rightContentInset()
		hiFill := pal.Selected
		padY := s.scale(2)
		row.Draw(func(p *ui.Painter, r ui.Rect) {
			band := ui.Rect{
				X: r.X + leftGutter - bandBleed,
				Y: r.Y + topGap - padY,
				W: max(r.W-leftGutter-rightInset+2*bandBleed, 0),
				H: max(r.H-topGap+2*padY, 0),
			}
			if band.W > 0 && band.H > 0 {
				p.Fill(band, hiFill, theme.RadiusSM)
				appUi.RecordBounds("outline-target-highlight", band)
			}
		})
	}
	row.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if prev, ok := s.blockRects[id]; !ok || prev != r {
			s.blockRects[id] = r
			if s.IsSelectionToolbarOpen() || s.inlineAI != nil || s.blockMenu != nil || s.calloutMenuBlock == id {
				c.Invalidate()
			}
		}
		if appUi.IsProbeEnabled() {
			appUi.RecordBounds(fmt.Sprintf("block-%d", id), r)
		}
		if dragTarget {
			s.paintBlockDropMarker(p, r, b, pal)
		}
	})
	if s.dragActive && s.dragBlockID == id {
		row.Opacity(0.55)
	}

	overlayBlocking := s.hasAnyOverlayOpen() || s.hoveredAnnotID != "" || s.imgResizeBlock != 0
	isDraggingBlock := s.dragBlockID != 0
	isDraggingAny := isDraggingBlock || s.isMouseDragging()
	showHandles := !s.minimal && !readOnly && !overlayBlocking && ((!isDraggingAny && (isHovered || isFocused)) || s.dragBlockID == id)
	showBlankAdd := !s.minimal && !readOnly && !overlayBlocking && !isDraggingAny && (isHovered || s.hoveredBlock == idx)
	isFolded := !s.minimal && !readOnly && b.Kind.IsHeading() && s.folded[id]
	blankPara := isPlainEmptyParagraph(b)
	listCtx := meta.List

	if s.inlineAI != nil {
		s.paintInlineAIOutline(c, row, id, topGap)
	}

	gripHoverBG := pal.Editor().GripHoverBG

	row.Children(func() {
		drawListGuides(c, id, listCtx, s.leftContentGutter(), s.zoom())
		line := ui.Row(c.Key("row-inner")).FillWidth().AlignItems(ui.Start)
		line.Children(func() {
			if !s.minimal {
				// Left handle gutter (54px wide + 14px gap), strictly only shown on hover or focus!
				showDeleteArm := !readOnly && !overlayBlocking && !isDraggingAny && deleteArmed
				showFold := !readOnly && b.Kind.IsHeading() && !overlayBlocking && (!isDraggingBlock || s.dragBlockID == id) && (showHandles || isFolded)
				hasGutterControls := showDeleteArm || (blankPara && showBlankAdd) || (!blankPara && showHandles) || showFold

				gutter := ui.Box(c.Key("gutter")).Width(s.handleWidth()).Height(0).Shrink(0).
					Margin(0, max(s.rowGap()-rowBleed, 0), 0, 0)
				if hasGutterControls {
					gutter.Children(func() {
						handleTop := s.blockHandleTopScaled(b, topGap)
						btn24 := s.scale(24)
						ic14 := s.scale(14)
						strip := ui.Row(c.Key("handle-strip")).Absolute().Left(0).Right(0).Top(handleTop).Height(btn24).
							Justify(ui.End).AlignItems(ui.Center).Gap(s.scale(2)).PassThrough()
						strip.Children(func() {
							if showDeleteArm {
								delBtn := ui.ButtonBase(c.Key("first-line-delete-badge")).Size(btn24, btn24).Center().
									Radius(theme.RadiusSM).Background(pal.DangerSurface).Border(1, pal.Danger.Alpha(0.4)).
									KeepFocus().Cursor(ui.CursorPointer).Label(theme.Tr("删除空行", "Delete empty line"))
								appUi.TooltipTrigger(c, delBtn,
									theme.Tr("再按 ⌫ 或点击", "Press ⌫ or click"),
									theme.Tr("删除首行", "to delete first line"),
									true,
								)
								delBtn.Children(func() {
									icons.Draw(c, icons.KeyBackspace, ic14, pal.MonoIconColors(pal.Danger))
								})
								appUi.Probe(delBtn, "first-line-delete-badge")
								if delBtn.Clicked() {
									s.pressJustBegan = false
									s.ConfirmFirstLineDelete()
								}
							} else if blankPara {
								if showBlankAdd {
									addID := fmt.Sprintf("block-add-%d", id)
									optClick := theme.Tr("⌥ + 点击", "⌥ + Click")
									if !theme.IsMacOS() {
										optClick = theme.Tr("Alt + 点击", "Alt + Click")
									}
									addTip := appUi.TooltipSpec{
										Rows: []appUi.TooltipRow{
											{
												Trigger:     theme.Tr("点击", "Click"),
												TriggerTone: appUi.TooltipToneAccent,
												Detail:      theme.Tr("在下方插入新块", "to insert block"),
											},
											{
												Trigger:     optClick,
												TriggerTone: appUi.TooltipToneAccent,
												Detail:      theme.Tr("在上方插入新块", "to insert block above"),
											},
										},
									}
									addBtn := appUi.IconButton(c, icons.Plus, appUi.IconButtonOptions{
										ID:          addID,
										Label:       theme.Tr("插入块", "Insert block"),
										TooltipSpec: &addTip,
										Size:        btn24,
										IconSize:    ic14,
									})
									addBtn.KeepFocus()
									s.recordOverlayBtn(addID, addBtn)
									if addBtn.Clicked() {
										s.pressJustBegan = false
										if (addBtn.ClickModifiers()&ui.Alt) != 0 && !(idx == 0 && isFrontmatterBlock(0, &s.doc.Blocks[0])) {
											s.pushUndo()
											s.spliceBlocks(idx, idx, []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}})
											s.FocusBlock(idx)
											s.markDirty()
										} else {
											s.FocusBlock(idx)
											trigger := s.blockTextRects[id]
											if trigger.W <= 0 {
												if r, ok := s.blockRects[id]; ok && r.W > 0 {
													trigger = ui.Rect{
														X: r.X + s.leftContentGutter(),
														Y: r.Y + topGap,
														W: max(r.W-s.leftContentGutter()-s.rightContentInset(), 120),
														H: max(r.H-topGap, 24),
													}
												}
											}
											s.openSlashMenu(idx, trigger)
										}
									}
								}
							} else if showHandles {
								gripID := fmt.Sprintf("block-grip-%d", id)
								grip := ui.ButtonBase(c.Key(gripID)).Size(btn24, btn24).Center().
									Radius(theme.RadiusSM).KeepFocus().Label(theme.Tr("块操作", "Block menu"))
								gripPressed := grip.Pressed()
								gripActive := s.dragBlockID == id && !s.dragCanceled
								gripHovered := grip.Hovered()
								if !gripActive && !gripPressed && s.blockMenu == nil {
									clickDetail := theme.Tr("打开块菜单", "to open menu")
									if !(idx == 0 && isFrontmatterBlock(0, b)) &&
										(b.PlainText() != "" || b.Code != "" || b.Kind == model.BlockTable || b.Kind == model.BlockImage || b.Kind == model.BlockDivider) {
										clickDetail = theme.Tr("转换块", "to turn into")
									}
									appUi.RichTooltip(c, grip, appUi.TooltipSpec{
										Rows: []appUi.TooltipRow{
											{
												Trigger:     theme.Tr("拖动", "Drag"),
												TriggerTone: appUi.TooltipToneAccent,
												Detail:      theme.Tr("调整块顺序", "to move"),
											},
											{
												Trigger:     theme.Tr("点击", "Click"),
												TriggerTone: appUi.TooltipToneAccent,
												Detail:      clickDetail,
											},
										},
									})
								}
								if gripActive || gripPressed {
									grip.Cursor(ui.CursorGrabbing)
								} else {
									grip.Cursor(ui.CursorGrab)
								}
								if gripActive || gripPressed || gripHovered {
									grip.Background(gripHoverBG)
								}
								gripIcon := pal.MonoIconColors(pal.TextSecondary)
								if gripActive || gripPressed || gripHovered {
									gripIcon = pal.DuoIconColors(pal.Text)
								}
								grip.Children(func() {
									icons.Draw(c, icons.Drag, ic14, gripIcon)
								})
								appUi.Probe(grip, gripID)
								s.recordOverlayBtn(gripID, grip)
								s.handleBlockGripGesture(c, grip, idx, id)
							}
							if showFold {
								glyph := icons.ChevronDown
								tip := theme.Tr("折叠章节", "Collapse section")
								childCount := s.headingSectionChildCount(idx)
								var detail string
								if childCount > 0 {
									detail = fmt.Sprintf(theme.Tr("收起 %d 项", "Hide %d"), childCount)
								}
								if isFolded {
									glyph = icons.ChevronRight
									tip = theme.Tr("展开章节", "Expand section")
									if childCount > 0 {
										detail = fmt.Sprintf(theme.Tr("展开 %d 项", "Show %d"), childCount)
									}
								}
								foldSpec := appUi.TooltipSpec{
									Rows: []appUi.TooltipRow{{
										Trigger:     tip,
										Detail:      detail,
										SplitDetail: detail != "",
									}},
								}
								foldBtn := appUi.IconButton(c, glyph, appUi.IconButtonOptions{
									ID:          fmt.Sprintf("block-fold-%d", id),
									Label:       theme.Tr("折叠章节", "Toggle fold"),
									TooltipSpec: &foldSpec,
									Size:        btn24,
									IconSize:    ic14,
								})
								foldBtn.KeepFocus()
								if foldBtn.Clicked() {
									s.pressJustBegan = false
									s.toggleFold(id)
								}
							}
						})
					})
				}
			}

			// List / indent marker gutter (depth-aware bullets, accent numbers, paper-tag todo box)
			indentPx := float32(min(max(b.Indent, 0), 12)) * s.indentStep()
			markerW := s.markerWidth()
			if b.Kind.IsList() {
				listFontSz, _, lh := s.textMetrics(b.Kind)
				lineH := listFontSz * lh
				marker := ui.Row(c.Key("marker")).Width(markerW+indentPx).Shrink(0).
					Padding(0, 0, 0, indentPx).AlignItems(ui.Start)
				if b.Kind != model.BlockTodo {
					marker.Justify(ui.End)
				}
				marker.Children(func() {
					s.renderListMarker(c, idx, b, listCtx, listFontSz, lineH, readOnly)
				})
			} else if indentPx > 0 {
				ui.Box(c.Key("indent-pad")).Width(indentPx).Shrink(0)
			}

			// Standard 672px body column (scaled by editor zoom; surface blocks expand by 2*bandBleed to match empty-line-band)
			stdW := s.standardWidth()
			bodyMaxW := max(stdW-indentPx, s.scale(240)) + 2*rowBleed
			if b.Kind.IsList() {
				bodyMaxW = max(stdW-markerW-indentPx, s.scale(240))
			}
			body := ui.Column(c.Key("body")).Grow(1).Shrink(1).MaxWidth(bodyMaxW)
			body.Children(func() {
				s.renderBlockBody(c, idx, b, isFocused, isHovered, readOnly, deleteArmed)
			})
		})
	})

	return isHovered
}

func (s *Session) renderBlockBody(c *ui.Context, idx int, b *model.Block, isFocused, isHovered, readOnly, deleteArmed bool) {
	switch b.Kind {
	case model.BlockDivider:
		s.renderDividerBlock(c, idx, isFocused)
	case model.BlockImage:
		s.renderImageBlock(c, idx, b, isFocused, readOnly)
	case model.BlockTable:
		s.renderTableBlock(c, idx, b, isFocused, isHovered, readOnly)
	case model.BlockCode, model.BlockRawHTML:
		s.renderCodeBlock(c, idx, b, isFocused, isHovered, readOnly)
	default:
		s.renderRichTextBlock(c, idx, b, isFocused, readOnly, deleteArmed)
	}
	if !s.minimal {
		s.renderBlockAnnotationsFoot(c, idx, b)
	}
}

func (s *Session) renderDividerBlock(c *ui.Context, idx int, isFocused bool) {
	pal := theme.Ocean()
	_, _, _, isSel := s.blockSelectionSpanRange(idx)
	box := ui.Row(c.Key("divider")).FillWidth().Height(s.scale(18)).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	lineColor := pal.Hairline
	lineH := float32(1)
	if isFocused || isSel {
		lineColor = pal.Accent
		lineH = 2
	}
	if isSel {
		box.Background(pal.SelectionBG.Alpha(0.45)).Radius(theme.RadiusXS)
	}
	box.Children(func() {
		ui.Box(c.Key("hr-line")).FillWidth().Height(lineH).Background(lineColor)
	})
	appUi.Probe(box, fmt.Sprintf("divider-%d", s.doc.Blocks[idx].ID))
}

func resolvedEditorFont() string {
	edFont, _, _ := theme.Typography()
	f := strings.TrimSpace(edFont)
	switch f {
	case "", "system-sans":
		return ""
	case "system-serif":
		return "LXGW WenKai, LXGW WenKai Screen, Kaiti SC, Songti SC, Source Han Serif SC, Noto Serif CJK SC, SimSun, serif"
	default:
		return f + ", PingFang SC, Songti SC, Noto Sans SC, Microsoft YaHei"
	}
}

func resolvedCodeFont() string {
	_, cdFont, _ := theme.Typography()
	f := strings.TrimSpace(cdFont)
	if f == "" || strings.EqualFold(f, "auto") {
		return "JetBrains Mono, Menlo, Monaco, Cascadia Mono, Consolas, DejaVu Sans Mono, Liberation Mono, monospace"
	}
	return f
}

func blockTextMetrics(kind model.BlockKind) (fontSize float32, weight int, lineHeight float32) {
	switch kind {
	case model.BlockHeading1:
		return 32, 600, 1.2
	case model.BlockHeading2:
		return 22, 600, 1.3
	case model.BlockHeading3:
		return 18, 600, 1.35
	case model.BlockHeading4:
		return 16, 600, 1.4
	case model.BlockHeading5:
		return 14, 600, 1.45
	case model.BlockHeading6:
		return 14, 500, 1.45
	default:
		_, _, lhMode := theme.Typography()
		return theme.FontEditor, theme.WeightRegular, lhMode.Factor() // 16px body
	}
}

func (s *Session) textMetrics(kind model.BlockKind) (fontSize float32, weight int, lineHeight float32) {
	if s.minimal {
		switch kind {
		case model.BlockHeading1:
			return 22, 600, 1.28
		case model.BlockHeading2:
			return 19, 600, 1.32
		case model.BlockHeading3:
			return 17, 600, 1.36
		case model.BlockHeading4:
			return 16, 600, 1.4
		case model.BlockHeading5:
			return 15, 600, 1.45
		case model.BlockHeading6:
			return 14, 500, 1.45
		}
	}
	fontSize, weight, lh := blockTextMetrics(kind)
	return fontSize * s.zoom(), weight, lh
}

func (s *Session) renderRichTextBlock(c *ui.Context, idx int, b *model.Block, isFocused, readOnly, deleteArmed bool) {
	pal := theme.Ocean()
	dark := theme.IsDark()
	fontSize, weight, lh := s.textMetrics(b.Kind)

	wrap := ui.Column(c.Key("text-wrap")).FillWidth()
	qDepth := b.QuoteDepth
	if b.Kind == model.BlockQuote && qDepth < 1 {
		qDepth = 1
	}
	if b.Kind == model.BlockCallout {
		st := calloutTone(b.Callout, dark)
		wrap.Padding(s.scale(38), s.scale(16), s.scale(12), s.scale(44)).Radius(theme.RadiusMD).Background(st.BG).Border(1, st.Border)
	} else if qDepth > 0 {
		qStep := s.scale(16)
		qBarW := max(2, s.scale(3))
		wrap.Padding(s.scale(2), 0, s.scale(2), float32(qDepth)*qStep).BorderWidth(0, 0, 0, qBarW).BorderColor(pal.QuoteBar)
		if qDepth > 1 {
			qbCol := pal.QuoteBar
			depth := qDepth
			wrap.Draw(func(p *ui.Painter, r ui.Rect) {
				for d := 1; d < depth; d++ {
					bx := r.X + float32(d)*qStep
					p.Fill(ui.Rect{X: bx, Y: r.Y, W: qBarW, H: r.H}, qbCol, 0)
				}
			})
		}
	}

	wrap.Children(func() {
		if b.Kind == model.BlockCallout {
			st := calloutTone(b.Callout, dark)
			open := s.calloutMenuBlock == b.ID
			curKind := b.Callout
			if curKind == "" {
				curKind = model.CalloutNote
			}
			cBtnSz := s.scale(26)
			hdr := ui.Row(c.Key("callout-hdr")).Absolute().Top(s.scale(7)).Left(s.scale(10)).Height(cBtnSz).
				Gap(s.scale(3)).AlignItems(ui.Center)
			hdr.Children(func() {
				toggleMenu := func() {
					if readOnly {
						return
					}
					if s.calloutMenuBlock == b.ID {
						s.calloutMenuBlock = 0
					} else {
						s.calloutMenuBlock = b.ID
						s.calloutMenuIndex = 0
						for i, k := range allCalloutKinds {
							if k == curKind {
								s.calloutMenuIndex = i
								break
							}
						}
					}
				}
				iconBtn := ui.ButtonBase(c.Key(fmt.Sprintf("callout-icon-%d", b.ID))).Size(cBtnSz, cBtnSz).Center().
					Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(theme.Tr("点击切换标注类型", "Click to switch callout type"))
				if open {
					iconBtn.Background(pal.Paper).Border(1, st.Accent)
				} else if iconBtn.Hovered() {
					iconBtn.Background(pal.Paper).Border(1, st.Border)
				} else {
					iconBtn.Border(1, ui.Transparent)
				}
				iconBtn.Children(func() {
					icons.Draw(c, icons.Callout, s.scale(16), theme.IconColors{Line: st.Accent, Fill: st.IconFill, Accent: st.Accent})
				})
				appUi.Probe(iconBtn, "callout-icon")
				if iconBtn.Clicked() {
					toggleMenu()
				}

				pillID := fmt.Sprintf("callout-pill-%d", b.ID)
				pillBtn := ui.ButtonBase(c.Key(pillID)).Height(cBtnSz).PaddingX(s.scale(4)).Gap(s.scale(4)).
					AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(calloutLabel(curKind))
				if open {
					pillBtn.Background(pal.Paper).Border(1, st.Accent)
				} else if pillBtn.Hovered() {
					pillBtn.Background(pal.Paper).Border(1, st.Border)
				} else {
					pillBtn.Border(1, ui.Transparent)
				}
				pillBtn.Children(func() {
					ui.Text(c, calloutLabel(curKind)).FontSize(s.scale(14)).FontWeight(theme.WeightTitle).TextColor(st.Text).SingleLine()
					icons.Draw(c, icons.TbChevron, s.scale(12), pal.MonoIconColors(st.Text))
				})
				appUi.Probe(pillBtn, pillID)
				appUi.Probe(pillBtn, "callout-pill")
				s.recordOverlayBtn(pillID, pillBtn)
				if pillBtn.Clicked() {
					toggleMenu()
				}
			})
		}
		inOriginDrag := s.isMouseDragging() && s.mouseOriginBlock == idx
		if isFocused && !readOnly && !s.needsDetachedSelectionFocus() && (!s.hasMultiBlockSelection() || inOriginDrag) {
			s.renderEditableTextBlock(c, idx, b, fontSize, weight, lh, deleteArmed)
			return
		}
		textCol := pal.Text
		if b.Kind == model.BlockHeading6 {
			textCol = pal.TextSecondary
		}
		edFont := resolvedEditorFont()
		plain := b.PlainText()
		var rt ui.Element
		if len(b.Spans) == 0 || plain == "" {
			rt = ui.Text(c.Key("rt-empty"), "").PreserveSpaces().FontSize(fontSize).FontWeight(weight).LineHeight(lh).
				MinHeight(fontSize * lh).TextColor(pal.Placeholder).FillWidth()
			if _, _, _, selOk := s.blockSelectionSpanRange(idx); selOk && s.hasMultiBlockSelection() {
				selFill := pal.EditorSelectionBG()
				scale := min(fontSize/16, 1)
				chipTargetH := float32(math.Round(float64(fontSize + 5*scale)))
				rt.Draw(func(p *ui.Painter, r ui.Rect) {
					if r.W <= 0 || r.H <= 0 {
						return
					}
					h := max(r.H-1, chipTargetH+2)
					p.Fill(ui.Rect{
						X: r.X - theme.EditorSelectionPadX,
						Y: r.Y + (r.H-h)*0.5,
						W: r.W + 2*theme.EditorSelectionPadX,
						H: h,
					}, selFill, theme.EditorSelectionRadius)
				})
			}
		} else {
			rt = ui.RichText(c.Key("rt")).PreserveSpaces().FontSize(fontSize).FontWeight(weight).
				LineHeight(lh).MinHeight(fontSize * lh).TextColor(textCol).FillWidth()
			rt.Children(func() {
				s.renderDisplaySpansChildren(c, idx, b, fontSize, weight)
			})
			bands := s.blockHighlightBands(idx, b, pal, true)
			codeRanges := blockCodeRanges(b, fontSize)
			if len(bands) > 0 || len(codeRanges) > 0 {
				blockID := b.ID
				spansCopy := b.Spans
				pureSpans := s.buildPureDisplaySpans(b, fontSize, weight)
				var selRange [2]int
				if selLo, selHi, _, selOk := s.blockSelectionSpanRange(idx); selOk && selHi > selLo {
					selRange = [2]int{selLo, selHi}
				}
				var ptrWin ui.Point
				var hasPtr bool
				if len(codeRanges) > 0 {
					ptrWin, hasPtr = s.windowPointerStatus()
				}
				appUi.DrawRichDecorations(c, rt, plain, func(w float32) *ui.TextLayout {
					return s.cachedShapeBlock(blockID, plain, spansCopy, idx, edFont, fontSize, weight, lh, w, true)
				}, appUi.RichDecorOptions{
					FontSize:     fontSize,
					Spans:        pureSpans,
					Bands:        bands,
					CodeRanges:   codeRanges,
					CodeBG:       pal.InlineCodeBG,
					CodeHoverBG:  pal.InlineCodeHoverBG,
					CodeEditBG:   pal.InlineCodeEditBG,
					SelRange:     selRange,
					OpaqueRanges: blockOpaqueBGRanges(b),
					LinkColor:    pal.Link,
					CaretPos:     -1,
					HasChipBG:    blockHasChipBG(b),
					PointerWin:   ptrWin,
					HasPointer:   hasPtr,
				})
			}
		}
		if edFont != "" {
			rt.Font(edFont)
		}
		if !readOnly {
			rt.Cursor(ui.CursorText)
		}
		rt.DrawOver(func(_ *ui.Painter, r ui.Rect) {
			s.recordBlockTextRect(b.ID, r)
		})
	})
}

func spanLinkAtOffset(b *model.Block, off int) string {
	pos := 0
	for _, sp := range b.Spans {
		next := pos + len([]rune(sp.Text))
		if off >= pos && off <= next && sp.Link != "" {
			return sp.Link
		}
		pos = next
	}
	return ""
}

func (s *Session) renderEditableTextBlock(c *ui.Context, idx int, b *model.Block, fontSize float32, weight int, lh float32, deleteArmed bool) {
	pal := theme.Ocean()
	draft := b.PlainText()
	hint := s.emptyHintFor(b, deleteArmed)
	st := s.richInputState(b.ID)

	wrap := ui.Box(c.Key("edit-wrap")).FillWidth()
	s.paintEmptyHintBand(wrap, hint, fontSize, lh)

	wrap.Children(func() {
		s.renderEmptyHintRow(c, hint, fontSize, weight, lh)

		textCol := pal.Text
		if b.Kind == model.BlockHeading6 {
			textCol = pal.TextSecondary
		}
		caretCol := pal.Accent
		if deleteArmed {
			caretCol = pal.Danger
		}
		edFont := resolvedEditorFont()
		syncSel := s.selSyncPending && s.selAnchor.Block == idx && s.selHead.Block == idx
		if syncSel {
			s.selSyncPending = false
		}
		multiSel := s.hasMultiBlockSelection()
		var multiSelRange [2]int
		if multiSel {
			if selLo, selHi, _, selOk := s.blockSelectionSpanRange(idx); selOk && selHi > selLo {
				multiSelRange = [2]int{selLo, selHi}
			}
		}

		in := appUi.RichInput(c, st, appUi.RichInputOptions{
			Key:               "input",
			Text:              draft,
			Spans:             s.buildPureDisplaySpans(b, fontSize, weight),
			Font:              edFont,
			FontSize:          fontSize,
			FontWeight:        weight,
			LineHeight:        lh,
			MinHeight:         fontSize * lh,
			TextColor:         textCol,
			SelectionBG:       pal.EditorSelectionBG(),
			SelectionFG:       textCol,
			CaretColor:        caretCol,
			LinkColor:         pal.Link,
			HideCaret:         s.caretHidden,
			SuppressSelection: multiSel,
			SuppressNavKeys:   s.hasMenuPopupOpen(),
			SyncSelection:     syncSel,
			FocusPending:      s.focusPending,
			Anchor:            s.selAnchor.Offset,
			Caret:             s.selHead.Offset,
			Bands:             s.blockHighlightBands(idx, b, pal, multiSel),
			Links:             blockLinkRanges(b),
			CodeRanges:        blockCodeRanges(b, fontSize),
			CodeBG:            pal.InlineCodeBG,
			CodeHoverBG:       pal.InlineCodeHoverBG,
			CodeEditBG:        pal.InlineCodeEditBG,
			SelRange:          multiSelRange,
			OpaqueRanges:      blockOpaqueBGRanges(b),
			HasChipBG:         blockHasChipBG(b),
			OnChange: func(newText string, _, caret int) {
				s.caretHidden = false
				s.footPickerAnnotID = ""
				s.applyBlockTextEdit(idx, newText, caret)
				if s.doc != nil && s.focusedBlock == idx && idx < len(s.doc.Blocks) {
					after := s.doc.Blocks[idx].PlainText()
					if after != newText {
						st.Sync(after, s.selAnchor.Offset, s.selHead.Offset)
						s.selSyncPending = false
					}
				}
			},
			OnSelect: func(anchor, caret int) {
				s.caretHidden = false
				if !s.hasMultiBlockSelection() {
					s.selAnchor = Position{Block: idx, Offset: anchor}
					s.selHead = Position{Block: idx, Offset: caret}
				}
			},
			OnCopy: func() bool {
				if s.hasNonEmptySelection() {
					if md := s.SelectedMarkdown(); md != "" {
						c.WriteClipboard(md)
						return true
					}
				}
				return false
			},
			OnCut: func() bool {
				if s.hasNonEmptySelection() {
					return s.CutSelection(c)
				}
				return false
			},
			OnPaste: func(svc ui.Services, pasted string) bool {
				return s.PasteFromClipboard(svc, pasted, true)
			},
			OnOpenLink: func(url string) {
				s.openBlockLink(c.Services(), url)
			},
			OnVerticalEdge: func(dir int) bool {
				if dir < 0 {
					if prev := s.stepVisibleBlock(idx, -1); prev >= 0 && prev != idx {
						s.FocusBlock(prev)
						if s.doc.Blocks[prev].Kind != model.BlockDivider && s.doc.Blocks[prev].Kind != model.BlockImage {
							endOff := len([]rune(s.doc.Blocks[prev].PlainText()))
							s.selAnchor = Position{Block: prev, Offset: endOff}
							s.selHead = s.selAnchor
							s.selSyncPending = true
						}
						return true
					}
					return false
				}
				if next := s.stepVisibleBlock(idx, 1); next >= 0 && next != idx {
					s.FocusBlock(next)
					return true
				}
				return false
			},
			OnKey: func(key ui.Key, mods ui.Modifiers, curText string, anchor, caret int) bool {
				s.caretHidden = false
				s.footPickerAnnotID = ""
				if s.hasMenuPopupOpen() {
					return false
				}
				selLo, selHi := min(anchor, caret), max(anchor, caret)

				if s.rangeShortcut(mods, key) {
					return true
				}
				if s.handleBlockAndFormatShortcut(mods, key) {
					return true
				}

				switch {
				case mods == ui.Cmd && key == ui.KeyA:
					rlen := len([]rune(curText))
					if len(s.doc.Blocks) > 1 && (rlen == 0 || (selLo == 0 && selHi >= rlen)) {
						s.SelectAllBlocks()
						return true
					}
				case mods == ui.Cmd && key == ui.KeyC:
					if s.hasNonEmptySelection() {
						s.copyPending = true
						return true
					}
				case mods == ui.Cmd && key == ui.KeyX:
					if s.hasMultiBlockSelection() {
						return s.CutSelection(c)
					}
				case mods == ui.Cmd && key == ui.KeyZ:
					s.Undo()
					return true
				case (mods == ui.Cmd|ui.Shift && key == ui.KeyZ) || (mods == ui.Cmd && key == ui.KeyY):
					s.Redo()
					return true
				case mods == ui.Cmd && key == ui.KeyB:
					s.ToggleMarkOnSelection(model.MarkBold)
					return true
				case mods == ui.Cmd && key == ui.KeyI:
					s.ToggleMarkOnSelection(model.MarkItalic)
					return true
				case mods == ui.Cmd && key == ui.KeyU:
					s.ToggleMarkOnSelection(model.MarkUnderline)
					return true
				case mods == ui.Cmd && key == ui.KeyE:
					s.ToggleMarkOnSelection(model.MarkCode)
					return true
				case mods == ui.Cmd|ui.Shift && key == ui.KeyX:
					s.ToggleMarkOnSelection(model.MarkStrike)
					return true
				case mods == ui.Cmd && key == ui.KeyF:
					s.OpenFind()
					return true
				case mods == 0 && key == ui.KeyEnter:
					if s.handleEnterOnBlock(idx, curText, caret) {
						return true
					}
				case mods == 0 && (key == ui.KeyBackspace || key == ui.KeyDelete):
					if s.hasMultiBlockSelection() {
						return s.DeleteSelection()
					}
					if key == ui.KeyBackspace && selLo == selHi && s.handleBackspaceOnBlock(idx, curText, caret) {
						return true
					}
				case mods == 0 && key == ui.KeyTab:
					if b.Kind.IsList() && b.Indent < 8 {
						s.pushUndo()
						b.Indent++
						b.Dirty = true
						s.markDirty()
						return true
					}
				case mods == ui.Shift && key == ui.KeyTab:
					if b.Kind.IsList() && b.Indent > 0 {
						s.pushUndo()
						b.Indent--
						b.Dirty = true
						s.markDirty()
						return true
					}
				}
				return false
			},
		})

		blockID := b.ID
		in.DrawOver(func(_ *ui.Painter, r ui.Rect) {
			s.recordBlockTextRect(blockID, r)
		})

		_ = s.updateFocusedRichInputDrag(c, s.frameRoot, st, idx)

		if s.focusPending {
			in.Focus(ui.FocusOptions{PreventScroll: s.focusPreventScroll})
			s.focusPending = false
			s.focusPreventScroll = false
		}
	})
}

func inlineCodeStyle(baseSize float32) (fg, bg ui.Color, padX, padY, radius float32) {
	pal := theme.Ocean()
	fg = pal.InlineCodeFG
	bg = pal.InlineCodeBG
	scale := min(baseSize/16, 1)
	padX = min(baseSize*0.25+1.5*scale, 9)
	targetH := float32(math.Round(float64(baseSize + 5*scale)))
	padY = max((targetH-baseSize*1.32)*0.5, 0)
	radius = max(min(float32(math.Round(float64(4*scale))), 5), 3)
	return
}

// buildPureSpan converts a single model.Span into its layout-invariant ui.Span without
// ever splitting spans on selection, find, or annotations (so PaddingX and ligatures never shift).
func buildPureSpan(sp model.Span, baseSize float32, baseWeight int, codeFG, codeBG ui.Color, codePadX, codePadY, codeRadius float32) (ui.Span, bool) {
	pal := theme.Ocean()
	dark := theme.IsDark()
	u := ui.Span{Text: sp.Text}
	if sp.Marks&model.MarkBold != 0 {
		u.Weight = max(baseWeight, 600)
	}
	if sp.Marks&model.MarkItalic != 0 {
		u.Italic = true
	}
	if sp.Marks&model.MarkUnderline != 0 {
		u.Underline = true
	}
	if sp.Marks&model.MarkStrike != 0 {
		u.Strikethrough = true
	}
	hasNamedBG := false
	if sp.Marks&model.MarkCode != 0 {
		u.Font = resolvedCodeFont()
		u.Size = baseSize
		u.Weight = max(u.Weight, theme.WeightRegular)
		u.Color = codeFG
		u.Background = codeBG
		u.Radius = codeRadius
		u.PaddingX = codePadX
		u.PaddingY = codePadY
	}
	if sp.Link != "" {
		u.Color = pal.Link
	}
	if col, ok := theme.ResolveNamedFG(sp.FGColor, dark); ok {
		u.Color = col
	}
	if bg, ok := theme.ResolveNamedBG(sp.BGColor, dark); ok {
		u.Background = bg
		u.Radius = 3
		hasNamedBG = true
	}
	return u, hasNamedBG
}

func (s *Session) buildPureDisplaySpans(b *model.Block, baseSize float32, baseWeight int) []ui.Span {
	if b == nil || len(b.Spans) == 0 {
		return nil
	}
	codeFG, codeBG, codePadX, codePadY, codeRadius := inlineCodeStyle(baseSize)
	out := make([]ui.Span, 0, len(b.Spans))
	for _, sp := range b.Spans {
		u, _ := buildPureSpan(sp, baseSize, baseWeight, codeFG, codeBG, codePadX, codePadY, codeRadius)
		out = append(out, u)
	}
	return out
}

func (s *Session) buildDisplaySpan(idx int, b *model.Block, sp model.Span, start, end int, baseSize float32, baseWeight int, codeFG, codeBG ui.Color, codePadX, codePadY, codeRadius float32) (ui.Span, bool) {
	pal := theme.Ocean()
	u, hasNamedBG := buildPureSpan(sp, baseSize, baseWeight, codeFG, codeBG, codePadX, codePadY, codeRadius)
	if b != nil {
		if !hasNamedBG {
			if annCol, ok := s.annotationHighlightColor(b.ID, start, end, pal); ok {
				u.Background = annCol
				u.Radius = max(u.Radius, 2)
			}
		}
		if findCol, ok := s.findHighlightColor(idx, start, end, pal); ok {
			u.Background = findCol
			u.Radius = max(u.Radius, 2)
		}
	}
	if selLo, selHi, _, selOk := s.blockSelectionSpanRange(idx); selOk && end > selLo && start < selHi {
		u.Background = pal.EditorSelectionBG()
		u.Radius = max(u.Radius, theme.EditorSelectionRadius)
	}
	return u, hasNamedBG
}

type spanUIKey struct {
	idx int
}

type tableRowUIKey struct {
	row int
}

type tableCellPosKey struct {
	row int
	col int
}

type cachedCodeSyntax struct {
	code   string
	canon  string
	dark   bool
	spans  []ui.Span
	ranges []ui.TextRange
}

type cachedCodePreview struct {
	code      string
	canon     string
	csvRows   [][]string
	mathText  string
	mathErr   string
	mmdNodes  []string
	mmdLabels map[string]string
	mmdEdges  []mermaidEdge
	mmdErr    string
}

func (s *Session) renderDisplaySpansChildren(c *ui.Context, _ int, b *model.Block, baseSize float32, baseWeight int) {
	codeFG, codeBG, codePadX, codePadY, codeRadius := inlineCodeStyle(baseSize)
	for i, sp := range b.Spans {
		u, hasNamedBG := buildPureSpan(sp, baseSize, baseWeight, codeFG, codeBG, codePadX, codePadY, codeRadius)
		if sp.Marks&model.MarkCode != 0 && !hasNamedBG {
			u.Background = ui.Color{}
		}
		el := ui.RichText(c.Key(spanUIKey{idx: i}), u)
		if sp.Link != "" {
			if el.Hovered() {
				el.Underline()
			}
			if s.readOnly || c.Modifiers()&ui.Cmd != 0 {
				el.Cursor(ui.CursorPointer)
			}
			linkTrigger := theme.Tr("⌘ + 点击", "⌘ + Click")
			if s.readOnly {
				linkTrigger = theme.Tr("点击", "Click")
			} else if !theme.IsMacOS() {
				linkTrigger = theme.Tr("Ctrl + 点击", "Ctrl + Click")
			}
			appUi.RichTooltip(c, el, appUi.TooltipSpec{
				Pill:      true,
				Placement: appUi.TooltipAbove,
				Rows: []appUi.TooltipRow{{
					Trigger:     linkTrigger,
					TriggerTone: appUi.TooltipToneAccent,
					Detail:      theme.Tr("访问链接", "open link"),
				}},
			})
		}
	}
}

func (s *Session) buildDisplaySpans(idx int, b *model.Block, baseSize float32, baseWeight int) []ui.Span {
	if b == nil || len(b.Spans) == 0 {
		return nil
	}
	codeFG, codeBG, codePadX, codePadY, codeRadius := inlineCodeStyle(baseSize)
	spans := s.splitSpansForHighlights(idx, b.ID, b.Spans)
	out := make([]ui.Span, 0, len(spans))
	pos := 0
	for _, sp := range spans {
		n := len([]rune(sp.Text))
		u, _ := s.buildDisplaySpan(idx, b, sp, pos, pos+n, baseSize, baseWeight, codeFG, codeBG, codePadX, codePadY, codeRadius)
		out = append(out, u)
		pos += n
	}
	return out
}

func (s *Session) buildInputTextRanges(idx int, b *model.Block) []ui.TextRange {
	pal := theme.Ocean()
	dark := theme.IsDark()
	var out []ui.TextRange
	pos := 0
	for _, sp := range b.Spans {
		rlen := len([]rune(sp.Text))
		s0, s1 := pos, pos+rlen
		pos = s1
		var tr ui.TextRange
		hasStyle := false
		if sp.Marks&model.MarkBold != 0 {
			tr.Start, tr.End, tr.Weight = s0, s1, 600
			hasStyle = true
		}
		if sp.Marks&model.MarkCode != 0 {
			tr.Start, tr.End, tr.Color = s0, s1, pal.InlineCodeFG
			if tr.Weight == 0 {
				tr.Weight = theme.WeightRegular
			}
			hasStyle = true
		}
		if col, ok := theme.ResolveNamedFG(sp.FGColor, dark); ok {
			tr.Start, tr.End, tr.Color = s0, s1, col
			hasStyle = true
		} else if sp.Link != "" {
			tr.Start, tr.End, tr.Color = s0, s1, pal.Link
			hasStyle = true
		}
		if hasStyle {
			out = append(out, tr)
		}
	}
	// Find matches and selections are painted as bands behind glyphs rather than recolored.
	return out
}

func (s *Session) rangeHasAnnotation(id model.BlockID, start, end int) bool {
	idx := s.blockIndexByID(id)
	for _, ann := range s.annotations {
		aIdx := s.blockIndexByID(ann.StartBlockID)
		bIdx := s.blockIndexByID(ann.EndBlockID)
		if aIdx < 0 || bIdx < 0 || idx < aIdx || idx > bIdx {
			continue
		}
		aOff := 0
		if idx == aIdx {
			aOff = ann.StartOffset
		}
		bOff := 1 << 30
		if idx == bIdx {
			bOff = ann.EndOffset
		}
		if end > aOff && start < bOff {
			return true
		}
	}
	return false
}

func (s *Session) rangeHasFindMatch(idx, start, end int) bool {
	if !s.findOpen {
		return false
	}
	for _, m := range s.findMatches {
		if m.BlockIdx == idx && end > m.StartRune && start < m.EndRune {
			return true
		}
	}
	return false
}

// --- Code Block + Syntax Highlight + Preview ---

func (s *Session) renderCodeBlock(c *ui.Context, idx int, b *model.Block, isFocused, isHovered, readOnly bool) {
	pal := theme.Ocean()
	dark := theme.IsDark()
	id := b.ID
	canon := canonicalLangTag(b.Language)
	previewable := isPreviewLang(canon)

	card := ui.Column(c.Key("code-card")).FillWidth().Radius(theme.RadiusSM).
		Background(pal.CodeBlockBG).Padding(s.scale(34), s.scale(12), s.scale(10), s.scale(12)).Gap(s.scale(8))
	if _, _, _, selOk := s.blockSelectionSpanRange(idx); selOk && s.hasMultiBlockSelection() {
		accent := pal.Accent
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if r.W > 1 && r.H > 1 {
				p.Stroke(ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1}, accent, 6, 1)
			}
		})
	}
	appUi.Probe(card, fmt.Sprintf("code-card-%d", id))

	card.Children(func() {
		// Top controls bar: left Source toggle (for Mermaid/LaTeX/CSV), right Copy + Lang pill
		hdr := ui.Row(c.Key("code-hdr")).Absolute().Top(s.scale(theme.Space6)).Left(s.scale(theme.Space12)).Right(s.scale(theme.Space8)).Height(s.scale(theme.HeightMicro)).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(s.scale(theme.Space8))
		hdr.Children(func() {
			left := ui.Row(c.Key("code-hdr-left")).AlignItems(ui.Center).Gap(theme.Space6)
			left.Children(func() {
				if previewable {
					srcOpen := b.ShowSource || strings.TrimSpace(b.Code) == ""
					lbl := theme.Tr("源码", "Source")
					if !srcOpen {
						lbl = theme.Tr("预览", "Preview") + " · " + theme.Tr("源码", "Source")
					}
					srcBtn := ui.ButtonBase(c.Key(fmt.Sprintf("cb-source-toggle-%d", id))).Height(theme.HeightMicro).PaddingX(theme.Space6).
						Radius(theme.RadiusSM).AlignItems(ui.Center).Gap(theme.Space4).Cursor(ui.CursorPointer).Label(lbl)
					if srcOpen || srcBtn.Hovered() {
						srcBtn.Background(pal.Hover)
					}
					srcBtn.Children(func() {
						g := icons.ChevronRight
						if srcOpen {
							g = icons.ChevronDown
						}
						icons.Draw(c, g, theme.IconInline, pal.HoverIconColors(srcBtn.Hovered()))
						ui.Text(c, theme.Tr("源码", "Source")).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
					})
					appUi.Probe(srcBtn, fmt.Sprintf("cb-source-toggle-%d", id))
					if srcBtn.Clicked() {
						b.ShowSource = !b.ShowSource
					}
				}
			})

			right := ui.Row(c.Key("code-hdr-right")).AlignItems(ui.Center).Gap(4)
			right.Children(func() {
				showControls := isFocused || isHovered || s.codeLangMenuBlock == id
				if showControls || previewable {
					copyGlyph := icons.Copy
					if s.codeCopiedBlock == id {
						copyGlyph = icons.Check
					}
					copyBtn := appUi.IconButton(c, copyGlyph, appUi.IconButtonOptions{
						ID:       fmt.Sprintf("code-copy-%d", id),
						Label:    theme.Tr("复制代码", "Copy code"),
						Size:     theme.HeightMicro,
						IconSize: theme.IconInline,
					})
					if copyBtn.Clicked() {
						c.WriteClipboard(b.Code)
						s.codeCopiedBlock = id
					}

					codeCols := pal.CodeBlock()
					pillFG := codeCols.PillFG
					pillHoverFG := codeCols.PillHoverFG
					pillBG := codeCols.PillBG
					pillHoverBG := codeCols.PillHoverBG
					langID := fmt.Sprintf("code-lang-%d", id)
					langBtn := ui.ButtonBase(c.Key(langID)).Height(theme.HeightMicro).PaddingX(theme.Space8).
						Radius(theme.RadiusSM).Center().Background(pillBG).Border(1, pal.MenuSeparator).
						Cursor(ui.CursorPointer).Label(languageDisplayLabel(b.Language))
					langFG := pillFG
					if langBtn.Hovered() || s.codeLangMenuBlock == id {
						langBtn.Background(pillHoverBG)
						langFG = pillHoverFG
					}
					langBtn.Children(func() {
						ui.Text(c, languageDisplayLabel(b.Language)).FontSize(theme.FontMeta).LineHeight(1.5).
							FontWeight(theme.WeightRegular).TextColor(langFG).SingleLine()
					})
					appUi.Probe(langBtn, langID)
					s.recordOverlayBtn(langID, langBtn)
					if langBtn.Clicked() && !readOnly {
						if s.codeLangMenuBlock == id {
							s.codeLangMenuBlock = 0
						} else {
							s.codeLangMenuBlock = id
						}
					}
				}
			})
		})

		// Preview rendering for Mermaid / LaTeX / CSV
		if previewable {
			s.renderCodePreview(c, id, canon, b.Code)
		}

		// Source code editor or syntax-highlighted view
		if !previewable || b.ShowSource || strings.TrimSpace(b.Code) == "" {
			cdFont := resolvedCodeFont()
			codeFG := pal.CodeBlock().TextFG
			codeFontSz := s.codeFontSize()
			codeMinLineH := s.scale(21)
			codeSelMinH := s.scale(19)
			codePlaceholder := theme.Tr("// 输入代码…", "// Write code…")
			inOriginDrag := s.isMouseDragging() && s.mouseOriginBlock == idx
			if isFocused && !readOnly && !s.needsDetachedSelectionFocus() && (!s.hasMultiBlockSelection() || inOriginDrag) {
				draft := b.Code
				oldCode := draft
				handle := *s.blockHandle(id)
				svc := c.Services()
				ta := ui.TextAreaBase(c.Key("code-input"), &draft).Lines(1, 500).FillWidth().
					Padding(0).BorderWidth(0).Background(ui.Transparent).
					Font(cdFont).FontSize(codeFontSz).LineHeight(1.5).MinHeight(codeMinLineH).TextColor(codeFG).
					Placeholder(codePlaceholder).PlaceholderColor(pal.Placeholder).
					SelectionColor(pal.EditorSelectionBG()).CaretColor(pal.Accent).CaretWidth(2).Bind(&handle)
				appUi.AutoHideScrollbars(c, ta)
				ta.OnPaste(func(pasted string) bool {
					if pasted == "" {
						return s.PasteFromClipboard(svc, "", true)
					}
					return false
				})
				ta.DrawOver(func(_ *ui.Painter, r ui.Rect) {
					s.recordBlockTextRect(id, r)
				})
				taDragging := s.updateFocusedInputDrag(c, s.frameRoot, ta, idx)
				var codeSelLo, codeSelHi int
				hasCodeSel := false
				if s.hasMultiBlockSelection() {
					ta.SelectionColor(ui.Transparent).CaretColor(ui.Transparent)
					if lo, hi, _, selOk := s.blockSelectionSpanRange(idx); selOk && hi > lo {
						codeSelLo, codeSelHi, hasCodeSel = lo, hi, true
					}
				} else if selA, selB := ta.TextSelection(); selA != selB {
					ta.CaretColor(ui.Transparent)
					if !ta.Composing() {
						ta.SelectionColor(ui.Transparent)
						codeSelLo, codeSelHi, hasCodeSel = min(selA, selB), max(selA, selB), true
					}
				}
				if hasCodeSel {
					selFill := pal.EditorSelectionBG()
					blockID := id
					codeText := draft
					lo, hi := codeSelLo, codeSelHi
					ta.Draw(func(p *ui.Painter, r ui.Rect) {
						if r.W <= 0 {
							return
						}
						codeLay := s.cachedShapeBlock(blockID, codeText, nil, idx, cdFont, codeFontSz, theme.WeightRegular, 1.5, r.W, false)
						if codeLay == nil {
							return
						}
						u0 := utf8RuneToUTF16(codeText, lo)
						u1 := utf8RuneToUTF16(codeText, hi)
						for _, sr := range codeLay.SelectionRects(ui.TextInputRange{Start: u0, End: u1}) {
							h := max(sr.H-1, codeSelMinH)
							p.Fill(ui.Rect{
								X: r.X + sr.X - 2,
								Y: r.Y + sr.Y + (sr.H-h)*0.5,
								W: sr.W + 4,
								H: h,
							}, selFill, 4)
						}
					})
				}
				_, ranges := s.cachedCodeSyntaxFor(id, draft, canon, dark)
				if len(ranges) > 0 {
					ta.TextRanges(ranges...)
				}
				ta.OnTextKey(func(k ui.TextKey) bool {
					if k.Mods == ui.Cmd && k.Key == ui.KeyA {
						rlen := len([]rune(k.Text()))
						selLo, selHi := ta.TextSelection()
						if len(s.doc.Blocks) > 1 && (rlen == 0 || (selLo == 0 && selHi >= rlen)) {
							s.SelectAllBlocks()
							return true
						}
					}
					if k.Mods == ui.Cmd && k.Key == ui.KeyZ {
						s.Undo()
						return true
					}
					if (k.Mods == ui.Cmd|ui.Shift && k.Key == ui.KeyZ) || (k.Mods == ui.Cmd && k.Key == ui.KeyY) {
						s.Redo()
						return true
					}
					if k.Mods == 0 && (k.Key == ui.KeyBackspace || k.Key == ui.KeyDelete) {
						if s.hasMultiBlockSelection() {
							return s.DeleteSelection()
						}
						if k.Key == ui.KeyBackspace && k.Text() == "" {
							return s.handleBackspaceOnBlock(idx, "", 0)
						}
					}
					return false
				})
				if s.focusPending {
					ta.Focus(ui.FocusOptions{PreventScroll: s.focusPreventScroll})
					s.focusPending = false
					s.focusPreventScroll = false
				}
				if s.selSyncPending && s.selAnchor.Block == idx && s.selHead.Block == idx {
					ta.SetTextSelection(s.selAnchor.Offset, s.selHead.Offset)
					s.selSyncPending = false
				}
				if ta.Changed() {
					_, selEnd := ta.TextSelection()
					s.applyBlockTextEdit(idx, draft, selEnd)
					_ = oldCode
				} else if !s.selSyncPending && !taDragging && ta.Focused() && s.selAnchor.Block == s.selHead.Block {
					selStart, selEnd := ta.TextSelection()
					s.selAnchor = Position{Block: idx, Offset: selStart}
					s.selHead = Position{Block: idx, Offset: selEnd}
				}
				s.setBlockHandle(id, handle)
			} else {
				spans, _ := s.cachedCodeSyntaxFor(id, b.Code, canon, dark)
				var rt ui.Element
				if len(spans) == 0 {
					rt = ui.Text(c.Key("code-empty"), codePlaceholder).PreserveSpaces().
						Font(cdFont).FontSize(codeFontSz).LineHeight(1.5).MinHeight(codeMinLineH).TextColor(pal.Placeholder).FillWidth()
					if _, _, _, selOk := s.blockSelectionSpanRange(idx); selOk && s.hasMultiBlockSelection() {
						selFill := pal.EditorSelectionBG()
						rt.Draw(func(p *ui.Painter, r ui.Rect) {
							if r.W <= 0 || r.H <= 0 {
								return
							}
							h := max(r.H-1, codeSelMinH)
							p.Fill(ui.Rect{X: r.X - 2, Y: r.Y + (r.H-h)*0.5, W: r.W + 4, H: h}, selFill, theme.RadiusXS)
						})
					}
				} else {
					rt = ui.RichText(c.Key("code-rt"), spans...).PreserveSpaces().Font(cdFont).
						FontSize(codeFontSz).LineHeight(1.5).MinHeight(codeMinLineH).TextColor(codeFG).FillWidth()
					if selLo, selHi, _, selOk := s.blockSelectionSpanRange(idx); selOk && selHi > selLo {
						selFill := pal.EditorSelectionBG()
						blockID := id
						codeText := b.Code
						lo, hi := selLo, selHi
						rt.Draw(func(p *ui.Painter, r ui.Rect) {
							if r.W <= 0 {
								return
							}
							codeLay := s.cachedShapeBlock(blockID, codeText, nil, idx, cdFont, codeFontSz, theme.WeightRegular, 1.5, r.W, false)
							if codeLay == nil {
								return
							}
							u0 := utf8RuneToUTF16(codeText, lo)
							u1 := utf8RuneToUTF16(codeText, hi)
							for _, sr := range codeLay.SelectionRects(ui.TextInputRange{Start: u0, End: u1}) {
								h := max(sr.H-1, codeSelMinH)
								p.Fill(ui.Rect{
									X: r.X + sr.X - 2,
									Y: r.Y + sr.Y + (sr.H-h)*0.5,
									W: sr.W + 4,
									H: h,
								}, selFill, theme.RadiusXS)
							}
						})
					}
				}
				if !readOnly {
					rt.Cursor(ui.CursorText)
				}
				rt.DrawOver(func(_ *ui.Painter, r ui.Rect) {
					s.recordBlockTextRect(id, r)
				})
			}
		}
	})
}

func (s *Session) cachedCodeSyntaxFor(id model.BlockID, code, canon string, dark bool) ([]ui.Span, []ui.TextRange) {
	if s.codeSyntaxCache == nil {
		s.codeSyntaxCache = make(map[model.BlockID]cachedCodeSyntax)
	}
	if entry, ok := s.codeSyntaxCache[id]; ok && entry.code == code && entry.canon == canon && entry.dark == dark {
		return entry.spans, entry.ranges
	}
	var spans []ui.Span
	var ranges []ui.TextRange
	if code != "" {
		toks := tokenizeCode(code, canon)
		if len(toks) == 0 {
			spans = []ui.Span{{Text: code}}
		} else {
			ranges = make([]ui.TextRange, len(toks))
			r := []rune(code)
			spans = make([]ui.Span, 0, len(toks)*2+1)
			cursor := 0
			for i, tk := range toks {
				col := syntaxTokenColor(tk.Kind, dark)
				ranges[i] = ui.TextRange{Start: tk.Start, End: tk.End, Color: col}
				if tk.Start > cursor {
					spans = append(spans, ui.Span{Text: string(r[cursor:tk.Start])})
				}
				spans = append(spans, ui.Span{Text: string(r[tk.Start:tk.End]), Color: col})
				cursor = tk.End
			}
			if cursor < len(r) {
				spans = append(spans, ui.Span{Text: string(r[cursor:])})
			}
		}
	}
	s.codeSyntaxCache[id] = cachedCodeSyntax{
		code:   code,
		canon:  canon,
		dark:   dark,
		spans:  spans,
		ranges: ranges,
	}
	return spans, ranges
}

func (s *Session) cachedCodePreviewFor(id model.BlockID, canon, src string) cachedCodePreview {
	if s.codePreviewCache == nil {
		s.codePreviewCache = make(map[model.BlockID]cachedCodePreview)
	}
	if entry, ok := s.codePreviewCache[id]; ok && entry.code == src && entry.canon == canon {
		return entry
	}
	entry := cachedCodePreview{code: src, canon: canon}
	switch canon {
	case "csv":
		entry.csvRows = parseCSVRows(src)
	case "latex":
		entry.mathText, entry.mathErr = formatMathPreview(src)
	case "mermaid":
		entry.mmdNodes, entry.mmdLabels, entry.mmdEdges, entry.mmdErr = parseMermaidGraph(src)
	}
	s.codePreviewCache[id] = entry
	return entry
}

func (s *Session) renderCodePreview(c *ui.Context, id model.BlockID, canon, src string) {
	pal := theme.Ocean()
	f14 := s.scale(14)
	box := ui.Column(c.Key("code-preview")).FillWidth().
		Padding(s.scale(12), s.scale(14)).Radius(theme.RadiusSM).Background(pal.Paper).Border(1, pal.Hairline).Gap(s.scale(8))
	if appUi.IsProbeEnabled() {
		appUi.Probe(box, fmt.Sprintf("code-preview-%d", id))
	}

	prev := s.cachedCodePreviewFor(id, canon, src)
	box.Children(func() {
		switch canon {
		case "csv":
			rows := prev.csvRows
			if len(rows) == 0 {
				ui.Text(c, theme.Tr("CSV 内容为空", "Empty CSV")).FontSize(f14).TextColor(pal.TextSecondary)
				return
			}
			for rIdx, row := range rows {
				tr := ui.Row(c.Key(tableRowUIKey{row: rIdx})).FillWidth().
					BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
				if rIdx == 0 {
					tr.Background(pal.CodeBlockBG)
				}
				tr.Children(func() {
					for cIdx, cell := range row {
						tc := ui.Box(c.Key(tableCellPosKey{row: rIdx, col: cIdx})).Grow(1).Shrink(1).Padding(s.scale(6), s.scale(8))
						tc.Children(func() {
							t := ui.Text(c, cell).FontSize(f14).TextColor(pal.Text)
							if rIdx == 0 {
								t.FontWeight(theme.WeightTitle)
							}
						})
					}
				})
			}
		case "latex":
			formatted, errMsg := prev.mathText, prev.mathErr
			if formatted != "" {
				ui.Row(c.Key("math-paper")).FillWidth().Center().Padding(s.scale(8), s.scale(12)).Children(func() {
					ui.Text(c, formatted).FontSize(s.scale(18)).FontWeight(theme.WeightControl).TextColor(pal.Text)
				})
			}
			if errMsg != "" {
				ui.Text(c, errMsg).FontSize(f14).TextColor(pal.Danger)
			}
		case "mermaid":
			nodes, labels, edges, errMsg := prev.mmdNodes, prev.mmdLabels, prev.mmdEdges, prev.mmdErr
			if errMsg != "" {
				ui.Text(c, errMsg).FontSize(f14).TextColor(pal.Danger)
				return
			}
			if len(edges) > 0 {
				for eIdx, e := range edges {
					row := ui.Row(c.Key(tableRowUIKey{row: eIdx})).AlignItems(ui.Center).Gap(s.scale(8)).Wrap()
					row.Children(func() {
						mermaidNodeBox(c, labels[e.From], f14)
						arrow := "→"
						if e.Label != "" {
							arrow = fmt.Sprintf("— %s →", e.Label)
						}
						ui.Text(c, arrow).FontSize(f14).TextColor(pal.Accent).FontWeight(theme.WeightTitle)
						mermaidNodeBox(c, labels[e.To], f14)
					})
				}
			} else {
				row := ui.Row(c.Key("mmd-nodes")).Gap(s.scale(8)).Wrap()
				row.Children(func() {
					for _, n := range nodes {
						mermaidNodeBox(c, labels[n], f14)
					}
				})
			}
		}
	})
}

func mermaidNodeBox(c *ui.Context, label string, fontSize float32) {
	pal := theme.Ocean()
	ui.Box(c).Padding(6, 12).Radius(theme.RadiusSM).Background(pal.Selected).Border(1, pal.Accent).Children(func() {
		ui.Text(c, label).FontSize(fontSize).FontWeight(theme.WeightControl).TextColor(pal.Text)
	})
}

func syntaxTokenColor(kind syntaxTokenKind, dark bool) ui.Color {
	pal := theme.Ocean()
	if dark != pal.Dark {
		if dark {
			pal = theme.OceanDark
		} else {
			pal = theme.OceanLight
		}
	}
	syn := pal.Syntax()
	switch kind {
	case tokKeyword:
		return syn.Keyword
	case tokString:
		return syn.String
	case tokComment:
		return syn.Comment
	case tokNumber:
		return syn.Number
	case tokType:
		return syn.Type
	case tokFunc:
		return syn.Func
	default:
		return syn.Default
	}
}

func codeSyntaxRanges(src, lang string) []ui.TextRange {
	toks := tokenizeCode(src, lang)
	if len(toks) == 0 {
		return nil
	}
	dark := theme.IsDark()
	out := make([]ui.TextRange, len(toks))
	for i, tk := range toks {
		out[i] = ui.TextRange{Start: tk.Start, End: tk.End, Color: syntaxTokenColor(tk.Kind, dark)}
	}
	return out
}

func codeSyntaxSpans(src, lang string) []ui.Span {
	if src == "" {
		return nil
	}
	toks := tokenizeCode(src, lang)
	if len(toks) == 0 {
		return []ui.Span{{Text: src}}
	}
	dark := theme.IsDark()
	r := []rune(src)
	var spans []ui.Span
	cursor := 0
	for _, tk := range toks {
		if tk.Start > cursor {
			spans = append(spans, ui.Span{Text: string(r[cursor:tk.Start])})
		}
		spans = append(spans, ui.Span{Text: string(r[tk.Start:tk.End]), Color: syntaxTokenColor(tk.Kind, dark)})
		cursor = tk.End
	}
	if cursor < len(r) {
		spans = append(spans, ui.Span{Text: string(r[cursor:])})
	}
	return spans
}

// --- Table Block ---

const (
	tableCellFontSize   float32 = 15.04
	tableCellLineHeight float32 = 21.808
	tableMinRowHeight   float32 = 34.8
	tableMinColWidth    float32 = 80
	tableMaxColWidth    float32 = 381.5
)

func (s *Session) renderTableBlock(c *ui.Context, idx int, b *model.Block, isFocused, isHovered, readOnly bool) {
	pal := theme.Ocean()
	id := b.ID
	if b.Table == nil || len(b.Table.Rows) == 0 {
		return
	}
	tbl := b.Table
	cols := len(tbl.Rows[0])
	tblCols := pal.Table()
	borderCol := tblCols.Border
	headerBG := tblCols.HeaderBG

	_, _, tMinRowH, tMinColW, _, _, _ := s.tableMetrics()
	bleed := s.bandBleed()
	wrap := ui.Column(c.Key("table-wrap")).FillWidth().Gap(s.scale(4))
	showChrome := !readOnly && (isHovered || isFocused || s.tableMenuBlock == id)
	wrap.Children(func() {
		// Top bar sits inside the top gutter so hover changes never shift blocks below.
		if showChrome {
			bar := ui.Row(c.Key("table-top-bar")).Absolute().Top(-s.scale(24)).Right(0).Height(s.scale(22)).Justify(ui.End).Gap(s.scale(4))
			bar.Children(func() {
				moreID := fmt.Sprintf("table-more-%d", id)
				more := appUi.IconButton(c, icons.More, appUi.IconButtonOptions{
					ID:       moreID,
					Label:    theme.Tr("表格操作", "Table options"),
					Size:     s.scale(22),
					IconSize: s.scale(14),
				})
				s.recordOverlayBtn(moreID, more)
				if more.Clicked() {
					if s.tableMenuBlock == id {
						s.tableMenuBlock = 0
						s.tableMenuKind = ""
					} else {
						s.tableMenuBlock = id
						s.tableMenuKind = "cell"
					}
				}
			})
		}

		gridRow := ui.Row(c.Key("table-grid-row")).FillWidth().AlignItems(ui.Stretch)
		gridRow.Children(func() {
			availGrid := max(s.standardWidth()+2*bleed, 200)
			if s.viewportWidth > 0 {
				availGrid = min(availGrid, max(s.viewportWidth-s.leftContentGutter()-s.rightContentInset()+2*bleed, 200))
			}
			buildGrid := func(needHScroll bool) {
				grid := ui.Column(c.Key("table-grid")).Radius(theme.RadiusSM).Border(1, borderCol).Clip()
				if needHScroll {
					grid.MinWidth(float32(cols) * tMinColW)
				} else {
					grid.FillWidth()
				}
				grid.Children(func() {
					for rIdx := range tbl.Rows {
						row := tbl.Rows[rIdx]
						rowEl := ui.Row(c.Key(tableRowUIKey{row: rIdx})).FillWidth().MinHeight(tMinRowH).
							BorderWidth(0, 0, boolToBorder(rIdx+1 < len(tbl.Rows)), 0).BorderColor(borderCol)
						if rIdx == 0 {
							rowEl.Background(headerBG)
						} else {
							rowEl.Background(pal.Paper)
						}
						rowEl.Children(func() {
							for cIdx := range row {
								s.renderTableCell(c, idx, id, tbl, rIdx, cIdx, borderCol, needHScroll, readOnly)
							}
						})
					}
				})
			}
			if float32(cols)*tMinColW > availGrid {
				hs := appUi.ScrollHorizontal(c.Key("table-hscroll")).Grow(1).Shrink(1)
				hs.Children(func() {
					buildGrid(true)
				})
			} else {
				buildGrid(false)
			}

			// Right add column bar sits in the right gutter so table width aligns 1:1 with other blocks.
			if showChrome {
				colSlot := ui.Box(c.Key("table-add-col-slot")).Absolute()
				if s.minimal {
					colSlot.Top(s.scale(2)).Bottom(s.scale(2)).Right(s.scale(2)).Width(s.scale(20))
				} else {
					colSlot.Top(0).Bottom(0).Right(-s.scale(26)).Width(s.scale(22))
				}
				colSlot.Children(func() {
					addCol := ui.ButtonBase(c.Key("table-add-col")).Fill().Center().
						Radius(theme.RadiusXS).Cursor(ui.CursorPointer).
						Label(theme.Tr("添加列", "Add column"))
					if addCol.Hovered() || addCol.Pressed() {
						addCol.Background(pal.Hover)
					} else {
						addCol.Background(pal.CodeBlockBG)
					}
					addCol.Children(func() {
						icons.Draw(c, icons.Plus, s.scale(14), pal.HoverIconColors(addCol.Hovered() || addCol.Pressed()))
					})
					appUi.Probe(addCol, "table-add-col")
					if addCol.Clicked() {
						s.tableAddCol(id, len(tbl.Rows[0]))
					}
				})
			}
		})

		// Bottom add row bar (reserve 20px height + 4px gap = 24px EXTEND_ROOM so blocks below never jump)
		rowSlot := ui.Box(c.Key("table-add-row-slot")).FillWidth().Height(s.scale(20))
		if showChrome {
			rowSlot.Children(func() {
				addRow := ui.ButtonBase(c.Key("table-add-row")).Fill().Center().
					Radius(theme.RadiusXS).Cursor(ui.CursorPointer).
					Label(theme.Tr("添加行", "Add row"))
				if addRow.Hovered() || addRow.Pressed() {
					addRow.Background(pal.Hover)
				} else {
					addRow.Background(pal.CodeBlockBG)
				}
				addRow.Children(func() {
					icons.Draw(c, icons.Plus, s.scale(14), pal.HoverIconColors(addRow.Hovered() || addRow.Pressed()))
				})
				appUi.Probe(addRow, "table-add-row")
				if addRow.Clicked() {
					s.tableAddRow(id, len(tbl.Rows))
				}
			})
		}
	})
}

func (s *Session) renderTableCell(c *ui.Context, blockIdx int, blockID model.BlockID, tbl *model.TableData, rIdx, cIdx int, borderCol ui.Color, needHScroll, readOnly bool) {
	pal := theme.Ocean()
	tFontSz, tLineH, _, tMinColW, tMaxColW, tPadY, tPadX := s.tableMetrics()
	cell := &tbl.Rows[rIdx][cIdx]
	inOriginDrag := s.isMouseDragging() && s.mouseOriginBlock == blockIdx
	isCellFocused := !readOnly && s.focusedBlock == blockIdx &&
		s.tableActiveBlock == blockID && s.tableActiveRow == rIdx && s.tableActiveCol == cIdx &&
		!s.needsDetachedSelectionFocus() && (!s.hasMultiBlockSelection() || inOriginDrag)

	cellBase := tableCellStartOffset(tbl, rIdx, cIdx)
	cellLen := len([]rune(cell.PlainText()))
	selLo, selHi, _, hasBlockSel := s.blockSelectionSpanRange(blockIdx)
	cellSelected := hasBlockSel && (s.hasMultiBlockSelection() || !(selLo >= cellBase && selHi <= cellBase+cellLen)) &&
		((cellLen == 0 && selLo <= cellBase && selHi >= cellBase) || (selHi > cellBase && selLo < cellBase+cellLen))

	box := ui.Box(c.Key(tableCellPosKey{row: rIdx, col: cIdx})).Grow(1).Shrink(1).
		MinWidth(tMinColW).Padding(tPadY, tPadX).
		BorderWidth(0, boolToBorder(cIdx+1 < len(tbl.Rows[rIdx])), 0, 0).BorderColor(borderCol)
	if needHScroll {
		box.MaxWidth(tMaxColW)
	}
	box.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		s.recordTableCellRect(blockID, rIdx, cIdx, r)
	})
	if !readOnly {
		box.Cursor(ui.CursorText)
	}
	if cellSelected {
		box.Background(pal.SelectionBG.Alpha(0.42))
	} else if isCellFocused {
		box.Background(pal.Selected)
	}
	weight := 400
	textCol := pal.Text
	if rIdx == 0 {
		weight = 600
		textCol = pal.TextSecondary
	}
	lhFactor := tableCellLineHeight / tableCellFontSize
	edFont := resolvedEditorFont()
	cellAlign := ui.Start
	if cIdx < len(tbl.Aligns) {
		switch tbl.Aligns[cIdx] {
		case model.ColAlignCenter:
			cellAlign = ui.Center
		case model.ColAlignRight:
			cellAlign = ui.End
		}
	}

	box.Children(func() {
		cellBlock := model.Block{ID: blockID, Kind: model.BlockParagraph, Spans: cell.Spans}
		if isCellFocused {
			draft := cell.PlainText()
			cellKey := tableCellKey{BlockID: blockID, Row: rIdx, Col: cIdx}
			st := s.tableInputState(cellKey)
			syncSel := s.selSyncPending && s.selAnchor.Block == blockIdx && s.selHead.Block == blockIdx
			var relA, relB int
			if syncSel {
				relA = clampInt(s.selAnchor.Offset-cellBase, 0, cellLen)
				relB = clampInt(s.selHead.Offset-cellBase, 0, cellLen)
				s.selSyncPending = false
			}
			multiSel := s.hasMultiBlockSelection()
			var cellBands []appUi.RichBand
			var cellSelRange [2]int
			if multiSel && hasBlockSel && selHi > cellBase && selLo < cellBase+cellLen {
				relLo := max(selLo-cellBase, 0)
				relHi := min(selHi-cellBase, cellLen)
				if relHi > relLo {
					cellSelRange = [2]int{relLo, relHi}
					cellBands = []appUi.RichBand{{
						Start:        relLo,
						End:          relHi,
						Fill:         pal.EditorSelectionBG(),
						Radius:       theme.EditorSelectionRadius,
						PadX:         theme.EditorSelectionPadX,
						FullLineBand: true,
						TintOver:     true,
					}}
				}
			}
			in := appUi.RichInput(c, st, appUi.RichInputOptions{
				Key:               "cell-in",
				Text:              draft,
				Spans:             s.buildPureDisplaySpans(&cellBlock, tFontSz, weight),
				Font:              edFont,
				FontSize:          tFontSz,
				FontWeight:        weight,
				LineHeight:        lhFactor,
				MinHeight:         tLineH,
				TextColor:         textCol,
				TextAlign:         cellAlign,
				SelectionBG:       pal.EditorSelectionBG(),
				SelectionFG:       textCol,
				CaretColor:        pal.Accent,
				LinkColor:         pal.Link,
				HideCaret:         s.caretHidden,
				SuppressSelection: multiSel,
				SuppressNavKeys:   s.hasMenuPopupOpen(),
				SyncSelection:     syncSel,
				FocusPending:      s.focusPending,
				Anchor:            relA,
				Caret:             relB,
				Bands:             cellBands,
				Links:             blockLinkRanges(&cellBlock),
				CodeRanges:        blockCodeRanges(&cellBlock, tFontSz),
				CodeBG:            pal.InlineCodeBG,
				CodeHoverBG:       pal.InlineCodeHoverBG,
				CodeEditBG:        pal.InlineCodeEditBG,
				SelRange:          cellSelRange,
				OpaqueRanges:      blockOpaqueBGRanges(&cellBlock),
				HasChipBG:         blockHasChipBG(&cellBlock),
				OnChange: func(newText string, anchor, caret int) {
					s.caretHidden = false
					oldDraft := cell.PlainText()
					if newText == oldDraft {
						return
					}
					s.pushUndo()
					cell.Spans = spliceSpansByTextEdit(cell.Spans, oldDraft, newText)
					s.doc.Blocks[blockIdx].Dirty = true
					s.doc.Blocks[blockIdx].CleanSource = ""
					newBase := tableCellStartOffset(tbl, rIdx, cIdx)
					s.selAnchor = Position{Block: blockIdx, Offset: newBase + anchor}
					s.selHead = Position{Block: blockIdx, Offset: newBase + caret}
					s.markDirty()
				},
				OnSelect: func(anchor, caret int) {
					s.caretHidden = false
					if !s.hasMultiBlockSelection() {
						curBase := tableCellStartOffset(tbl, rIdx, cIdx)
						s.selAnchor = Position{Block: blockIdx, Offset: curBase + anchor}
						s.selHead = Position{Block: blockIdx, Offset: curBase + caret}
					}
				},
				OnPaste: func(svc ui.Services, pasted string) bool {
					return s.PasteFromClipboard(svc, pasted, true)
				},
				OnOpenLink: func(url string) {
					s.openBlockLink(c.Services(), url)
				},
				OnKey: func(key ui.Key, mods ui.Modifiers, _ string, _, _ int) bool {
					s.caretHidden = false
					if s.hasMenuPopupOpen() {
						return false
					}
					if key == ui.KeyTab {
						if mods == ui.Shift {
							s.stepTableCell(blockID, tbl, rIdx, cIdx, -1)
						} else {
							s.stepTableCell(blockID, tbl, rIdx, cIdx, 1)
						}
						return true
					}
					if key == ui.KeyEnter && mods == 0 {
						s.stepTableCell(blockID, tbl, rIdx, cIdx, len(tbl.Rows[0]))
						return true
					}
					if mods == ui.Cmd && key == ui.KeyZ {
						s.Undo()
						return true
					}
					if (mods == ui.Cmd|ui.Shift && key == ui.KeyZ) || (mods == ui.Cmd && key == ui.KeyY) {
						s.Redo()
						return true
					}
					if mods == ui.Cmd && key == ui.KeyB {
						s.ToggleMarkOnSelection(model.MarkBold)
						return true
					}
					if mods == ui.Cmd && key == ui.KeyI {
						s.ToggleMarkOnSelection(model.MarkItalic)
						return true
					}
					if mods == ui.Cmd && key == ui.KeyE {
						s.ToggleMarkOnSelection(model.MarkCode)
						return true
					}
					return false
				},
			})
			_ = s.updateFocusedRichTableCellDrag(c, s.frameRoot, st, blockIdx, blockID, rIdx, cIdx)
			if s.focusPending {
				in.Focus(ui.FocusOptions{PreventScroll: s.focusPreventScroll})
				s.focusPending = false
				s.focusPreventScroll = false
			}
		} else {
			txt := cell.PlainText()
			var t ui.Element
			if txt == "" {
				t = ui.Text(c.Key("cell-txt"), "").PreserveSpaces().FontSize(tFontSz).LineHeight(lhFactor).
					MinHeight(tLineH).FontWeight(weight).TextColor(textCol).FillWidth()
			} else {
				pureSpans := s.buildPureDisplaySpans(&cellBlock, tFontSz, weight)
				codeRanges := blockCodeRanges(&cellBlock, tFontSz)
				renderSpans := pureSpans
				if len(codeRanges) > 0 {
					renderSpans = make([]ui.Span, len(pureSpans))
					copy(renderSpans, pureSpans)
					dark := theme.IsDark()
					for i, sp := range cellBlock.Spans {
						if _, hasNamedBG := theme.ResolveNamedBG(sp.BGColor, dark); sp.Marks&model.MarkCode != 0 && !hasNamedBG && i < len(renderSpans) {
							renderSpans[i].Background = ui.Color{}
						}
					}
				}
				t = ui.RichText(c.Key("cell-txt"), renderSpans...).PreserveSpaces().FontSize(tFontSz).LineHeight(lhFactor).
					MinHeight(tLineH).FontWeight(weight).TextColor(textCol).FillWidth()
				var cellBands []appUi.RichBand
				var cellSelRange [2]int
				if hasBlockSel && selHi > cellBase && selLo < cellBase+cellLen {
					relLo := max(selLo-cellBase, 0)
					relHi := min(selHi-cellBase, cellLen)
					if relHi > relLo {
						cellSelRange = [2]int{relLo, relHi}
						cellBands = []appUi.RichBand{{
							Start:        relLo,
							End:          relHi,
							Fill:         pal.EditorSelectionBG(),
							Radius:       theme.EditorSelectionRadius,
							PadX:         theme.EditorSelectionPadX,
							FullLineBand: true,
							TintOver:     true,
						}}
					}
				}
				if len(cellBands) > 0 || len(codeRanges) > 0 {
					spansCopy := pureSpans
					var ptrWin ui.Point
					var hasPtr bool
					if len(codeRanges) > 0 {
						ptrWin, hasPtr = s.windowPointerStatus()
					}
					appUi.DrawRichDecorations(c, t, txt, func(w float32) *ui.TextLayout {
						return ui.ShapeRichTextWith(spansCopy, ui.Font{
							Family:     edFont,
							Size:       tFontSz,
							Weight:     weight,
							LineHeight: lhFactor,
						}, ui.TextShape{Width: max(w, 1), Align: cellAlign})
					}, appUi.RichDecorOptions{
						FontSize:     tFontSz,
						Spans:        spansCopy,
						Bands:        cellBands,
						CodeRanges:   codeRanges,
						CodeBG:       pal.InlineCodeBG,
						CodeHoverBG:  pal.InlineCodeHoverBG,
						CodeEditBG:   pal.InlineCodeEditBG,
						SelRange:     cellSelRange,
						OpaqueRanges: blockOpaqueBGRanges(&cellBlock),
						CaretPos:     -1,
						HasChipBG:    blockHasChipBG(&cellBlock),
						PointerWin:   ptrWin,
						HasPointer:   hasPtr,
					})
				}
			}
			if edFont != "" {
				t.Font(edFont)
			}
			if !readOnly {
				t.Cursor(ui.CursorText)
			}
			if cellAlign != ui.Start {
				t.TextAlign(cellAlign)
			}
		}
	})
}

func (s *Session) stepTableCell(id model.BlockID, tbl *model.TableData, r, c, delta int) {
	if len(tbl.Rows) == 0 || len(tbl.Rows[0]) == 0 {
		return
	}
	cols := len(tbl.Rows[0])
	total := len(tbl.Rows) * cols
	pos := r*cols + c + delta
	if pos < 0 {
		pos = 0
	}
	if pos >= total {
		s.tableAddRow(id, len(tbl.Rows))
		pos = (len(tbl.Rows) - 1) * cols
	}
	s.tableActiveBlock = id
	s.tableActiveRow = pos / cols
	s.tableActiveCol = pos % cols
	s.focusPending = true
}

func boolToBorder(v bool) float32 {
	if v {
		return 1
	}
	return 0
}

// --- Image Block & Image Toolbar ---

func (s *Session) resolveImageSource(src string) (ui.ImageSource, bool) {
	if src == "" {
		return nil, false
	}
	if cached, ok := s.imageCache[src]; ok {
		return cached, true
	}
	if s.imageFailed[src] {
		return nil, false
	}
	resolved := src
	if !filepath.IsAbs(src) && s.notePath != "" {
		resolved = filepath.Join(filepath.Dir(s.notePath), src)
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		s.imageFailed[src] = true
		return nil, false
	}
	decoded, err := ui.DecodeImage(raw)
	if err != nil {
		s.imageFailed[src] = true
		return nil, false
	}
	s.imageCache[src] = decoded
	return decoded, true
}

func (s *Session) renderImageBlock(c *ui.Context, idx int, b *model.Block, isFocused, readOnly bool) {
	pal := theme.Ocean()
	id := b.ID

	align := ui.Start
	switch b.ImageAlign {
	case model.ImageAlignCenter:
		align = ui.Center
	case model.ImageAlignRight:
		align = ui.End
	}
	widthPct := b.ImageWidth
	if widthPct <= 0 || widthPct > 100 {
		widthPct = 100
	}
	dragging := s.imgResizeBlock == id
	if dragging {
		widthPct = clampFloat32(s.imgResizeCurPct, 10, 100)
	}

	_, _, _, imgSelected := s.blockSelectionSpanRange(idx)
	outer := ui.Column(c.Key("img-outer")).FillWidth().AlignItems(align)
	outer.Children(func() {
		picBox := ui.Box(c.Key(fmt.Sprintf("img-frame-%d", id))).WidthPercent(widthPct).Radius(theme.RadiusSM).Clip().
			Cursor(ui.CursorPointer)
		picBorder := pal.Hairline
		if isFocused || dragging || imgSelected {
			picBorder = pal.Accent
		}
		picBox.Border(1, picBorder)
		picBox.DrawOver(func(p *ui.Painter, r ui.Rect) {
			appUi.RecordBounds(fmt.Sprintf("image-%d", id), r)
			if s.imageRects == nil {
				s.imageRects = make(map[model.BlockID]ui.Rect)
			}
			if prev, ok := s.imageRects[id]; !ok || prev != r {
				s.imageRects[id] = r
				if isFocused {
					c.Invalidate()
				}
			}
			if imgSelected && s.hasMultiBlockSelection() {
				p.Fill(r, pal.SelectionBG.Alpha(0.28), theme.RadiusSM)
			}
		})
		isVideo := IsSupportedVideoPath(b.ImageSrc)
		if !isVideo && !b.ImageCrop.IsZero() {
			picBox.ClipInsetPercent(b.ImageCrop.Top*100, b.ImageCrop.Right*100, b.ImageCrop.Bottom*100, b.ImageCrop.Left*100, theme.RadiusSM)
		}

		picBox.Children(func() {
			if isVideo {
				s.renderVideoContent(c, id, b)
			} else if imgSrc, ok := s.resolveImageSource(b.ImageSrc); ok {
				ui.Image(c, imgSrc).FillWidth()
			} else {
				ph := ui.Row(c.Key("img-ph")).FillWidth().Height(120).Center().Gap(theme.Space8).Background(pal.CodeBlockBG)
				ph.Children(func() {
					icons.Draw(c, icons.Image, 20, pal.MonoIconColors(pal.TextSecondary))
					lbl := b.ImageAlt
					if lbl == "" {
						lbl = b.ImageSrc
					}
					if lbl == "" {
						lbl = theme.Tr("图片", "Image")
					}
					ui.Text(c, lbl).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
				})
			}

			// Left & Right drag-resize side handles (aligned with src/editor/image_resize.rs)
			if !readOnly && (isFocused || dragging || picBox.Hovered()) {
				scale := float32(1)
				if b.ImageAlign == model.ImageAlignCenter {
					scale = 2
				}
				bleed := s.bandBleed()
				stdW := s.standardWidth() + 2*bleed
				availW := min(max(s.editorRect.W-s.leftContentGutter()-s.rightContentInset()+2*bleed, 200), stdW)
				if availW <= 0 {
					availW = stdW
				}
				for _, side := range []struct {
					probe string
					left  bool
					dir   float32
				}{
					{"img-resize-left", true, -1},
					{"img-resize-right", false, 1},
				} {
					h := ui.Box(c.Key(fmt.Sprintf("%s-%d", side.probe, id))).Absolute().
						TopPercent(25).BottomPercent(25).Width(12).Center().Cursor(ui.CursorResizeColumn)
					if side.left {
						h.Left(2)
					} else {
						h.Right(2)
					}
					h.Children(func() {
						ui.Box(c).Width(4).FillHeight().Radius(theme.RadiusXS).Background(pal.Editor().ResizeHandleBG).Border(1, pal.Hairline)
					})
					appUi.Probe(h, side.probe)
					dx, _, held := h.Dragged()
					if held {
						if s.imgResizeBlock != id {
							s.imgResizeBlock = id
							s.imgResizeStartPct = widthPct
							s.imgResizeCurPct = widthPct
						}
						if dx != 0 {
							deltaPct := (dx * side.dir * scale / availW) * 100
							s.imgResizeCurPct = clampFloat32(s.imgResizeCurPct+deltaPct, 10, 100)
						}
					} else if s.imgResizeBlock == id {
						finalPct := clampFloat32(s.imgResizeCurPct, 10, 100)
						startPct := s.imgResizeStartPct
						s.imgResizeBlock = 0
						if math.Abs(float64(finalPct-startPct)) >= 0.5 {
							s.pushUndo()
							if finalPct >= 99.5 {
								b.ImageWidth = 0
							} else {
								b.ImageWidth = float32(math.Round(float64(finalPct)))
							}
							b.Dirty = true
							b.CleanSource = ""
							s.markDirty()
						}
					}
				}
			}
		})
	})

	// Floating Image Toolbar rendered in Overlay above picBox so focus shifts 0px below.
	if isFocused && !readOnly && !dragging && !s.hasMultiBlockSelection() && !s.isMouseDragging() && !s.findOpen && s.inlineAI == nil {
		s.renderImageToolbar(c, idx, b)
	}
}

func (s *Session) overlayBounds(c *ui.Context) (minX, maxX, winW, winH float32) {
	vp, winW, winH := s.overlayViewport(c)
	return vp.X + 8, max(vp.X+vp.W-8, vp.X+8), winW, winH
}

func (s *Session) renderImageToolbar(c *ui.Context, idx int, b *model.Block) {
	pal := theme.Ocean()
	id := b.ID
	vp, winW, winH := s.overlayViewport(c)
	trigger := s.imageRects[id]
	if trigger.W <= 0 {
		trigger = s.blockRects[id]
	}
	if trigger.W <= 0 {
		trigger = ui.Rect{X: vp.X + 120, Y: vp.Y + 120, W: 400, H: 120}
	}
	spec := appUi.PlaceAbove(trigger).WithViewport(vp).WithAlignX(appUi.AlignX{Kind: appUi.AlignCenter}).WithGap(6).WithMargin(8)
	barW, barH := s.measuredOrEstSize("image-toolbar", 275, 38)
	x, y := appUi.ResolveOrigin(spec, barW, barH, winW, winH)

	ui.Overlay(c, func() {
		bar := ui.Row(c.Key(fmt.Sprintf("image-toolbar-%d", id))).Absolute().Left(x).Top(y).
			Height(38).PaddingX(theme.Space6).Gap(theme.Space4).AlignItems(ui.Center).
			Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.Hairline).
			Transition(appUi.PopupTransition())
		appUi.Elevate(bar, theme.ElevationPopover)
		appUi.Probe(bar, "image-toolbar")
		s.recordOverlayPanel("image-toolbar", bar)

		bar.Children(func() {
			if !s.minimal {
				commentLbl := theme.Tr("添加批注", "Add annotation")
				commentBtn := appUi.IconButton(c, icons.TbComment, appUi.IconButtonOptions{
					ID: "img-tb-comment", Label: commentLbl, Tooltip: commentLbl,
					Shortcut: formatShortcutDisplay("⌥⌘M", "Ctrl+Alt+M"),
					TooltipPill: true, TooltipAbove: true,
					Size: theme.HeightCompact, IconSize: theme.IconAction,
					Active: s.toolbarPop == "comment",
				})
				s.recordOverlayBtn("img-tb-comment", commentBtn)
				if commentBtn.Clicked() {
					s.FocusBlock(idx)
					s.toggleToolbarPop("comment")
				}

				reactLbl := theme.Tr("添加表情回应", "Add reaction")
				reactBtn := appUi.IconButton(c, icons.TbReaction, appUi.IconButtonOptions{
					ID: "img-tb-reaction", Label: reactLbl, Tooltip: reactLbl,
					TooltipPill: true, TooltipAbove: true,
					Size: theme.HeightCompact, IconSize: theme.IconAction,
					Active: s.toolbarPop == "reactions",
				})
				s.recordOverlayBtn("img-tb-reaction", reactBtn)
				if reactBtn.Clicked() {
					s.FocusBlock(idx)
					s.toggleToolbarPop("reactions")
				}

				ui.Box(c).Width(1).Height(20).MarginX(2).Background(pal.Hairline)
			}

			for _, item := range []struct {
				id    string
				glyph icons.Icon
				align model.ImageAlign
				label string
			}{
				{"img-align-left", icons.AlignLeft, model.ImageAlignLeft, theme.Tr("左对齐", "Align left")},
				{"img-align-center", icons.AlignCenter, model.ImageAlignCenter, theme.Tr("居中对齐", "Align center")},
				{"img-align-right", icons.AlignRight, model.ImageAlignRight, theme.Tr("右对齐", "Align right")},
			} {
				btn := appUi.IconButton(c, item.glyph, appUi.IconButtonOptions{
					ID: item.id, Label: item.label, Tooltip: item.label,
					TooltipPill: true, TooltipAbove: true,
					Size: theme.HeightCompact, IconSize: theme.IconAction, Active: b.ImageAlign == item.align,
				})
				if btn.Clicked() {
					s.pushUndo()
					b.ImageAlign = item.align
					b.Dirty = true
					s.markDirty()
				}
			}

			ui.Box(c).Width(1).Height(20).MarginX(2).Background(pal.Hairline)

			if !IsSupportedVideoPath(b.ImageSrc) {
				cropLbl := theme.Tr("裁剪图片", "Crop image")
				cropBtn := appUi.IconButton(c, icons.Crop, appUi.IconButtonOptions{
					ID:           "img-crop",
					Label:        cropLbl,
					Tooltip:      cropLbl,
					TooltipPill:  true,
					TooltipAbove: true,
					Size:         theme.HeightCompact,
					IconSize:     theme.IconAction,
					Active:       s.imagePopBlock == id && s.imagePopKind == "crop",
				})
				s.recordOverlayBtn("img-crop", cropBtn)
				if cropBtn.Clicked() {
					if s.imagePopBlock == id && s.imagePopKind == "crop" {
						s.imagePopKind = ""
					} else {
						s.imagePopBlock = id
						s.imagePopKind = "crop"
					}
				}
			}

			sizeLbl := theme.Tr("设置图片尺寸", "Set image size")
			sizeBtn := appUi.IconButton(c, icons.Sliders, appUi.IconButtonOptions{
				ID:           "img-size",
				Label:        sizeLbl,
				Tooltip:      sizeLbl,
				TooltipPill:  true,
				TooltipAbove: true,
				Size:         theme.HeightCompact,
				IconSize:     theme.IconAction,
				Active:       s.imagePopBlock == id && s.imagePopKind == "size",
			})
			s.recordOverlayBtn("img-size", sizeBtn)
			if sizeBtn.Clicked() {
				if s.imagePopBlock == id && s.imagePopKind == "size" {
					s.imagePopKind = ""
				} else {
					s.imagePopBlock = id
					s.imagePopKind = "size"
				}
			}

			delLbl := theme.Tr("删除图片", "Delete image")
			delBtn := appUi.IconButton(c, icons.Trash, appUi.IconButtonOptions{
				ID: "img-delete", Label: delLbl, Tooltip: delLbl,
				TooltipPill: true, TooltipAbove: true,
				Size: 28, IconSize: 16, Danger: true,
			})
			if delBtn.Clicked() {
				s.imagePopBlock = 0
				s.imagePopKind = ""
				s.deleteBlock(idx)
			}
		})

		// Image sub-popovers (Crop / Size / Alt) anchored independently to their trigger buttons
		if s.imagePopBlock == id && s.imagePopKind != "" {
			s.renderImageSubPopover(c, idx, b)
		}
	})
}

func timeAfter(t interface{ IsZero() bool }) bool {
	return false // Expiry handled explicitly via timers/actions so tests remain deterministic.
}
