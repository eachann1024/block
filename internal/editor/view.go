package editor

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"block/internal/codepreview"
	"block/internal/icons"
	"block/internal/media"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	editorHandleWidth        float32 = 54
	editorRowGap             float32 = 14
	editorRightInset         float32 = editorHandleWidth + editorRowGap
	editorMarkerWidth        float32 = 29.68
	editorIndentStep         float32 = editorMarkerWidth / 2
	editorTopPad             float32 = 64
	editorBottomPad          float32 = 160
	virtualizeBlockThreshold         = 80
)

type virtBlockSpanKey struct {
	first model.BlockID
}

type virtBlockEntry struct {
	id model.BlockID
	h  float32
}

// Render draws the block editor, its inline blocks, and all floating overlays/rails.
func (s *Session) Render(c *ui.Context, viewportWidth float32, readOnly bool) {
	readOnly = readOnly || s.previewMedia != nil
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		s.doc = model.NewEmptyDoc()
		s.bumpDocRev()
	}
	// A pending AI plan must be accepted before the note can be edited.
	s.planBaseReadOnly = readOnly
	s.syncPlanReview()
	readOnly = readOnly || s.planReview != nil
	s.readOnly = readOnly
	s.framePathHover = nil
	s.frameTabChildFocused = false
	if s.lastPressCmdIntact && (!c.WindowActive() || c.Modifiers() != ui.Cmd) {
		s.lastPressCmdIntact = false
	}
	s.ensureAnnotationsLoaded()
	s.pollInlineAI()

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

	s.jumpAlpha = s.jumpHighlight(c)
	s.annotFlashAlpha = s.annotationFlashAlpha(c)
	s.commentBtnPressed = false

	s.handleGlobalShortcuts(c, readOnly)

	pal := theme.Ocean()
	var root ui.Element
	if s.embedded {
		root = ui.Column(c.Key("editor-root")).FillWidth().MinWidth(0).Bind(&s.rootHandle)
	} else {
		root = ui.Box(c.Key("editor-root")).Fill().Background(pal.Paper).Bind(&s.rootHandle)
	}
	if rb := root.Bounds(); rb.W > 0 && rb.H > 0 {
		s.editorRect = rb
	}
	root.ObserveInput(s.onGutterInput)
	if !readOnly || s.typingPointer != nil {
		x, y, over := root.PointerPosition()
		if s.typingPointer != nil && over && *s.typingPointer != (ui.Point{X: x, Y: y}) {
			s.typingPointer = nil
		}
	}
	s.frameRoot = root
	defer func() { s.frameRoot = ui.Element{} }()

	animEdgeL, animEdgeR := s.animatedEdgeInsets(c, root)
	canvasWidth := max(viewportWidth-animEdgeL-animEdgeR, 0)
	s.viewportWidth = max(canvasWidth, 240)
	editorBounds := s.editorRect
	if editorBounds.W > 0 {
		canvasWidth = min(canvasWidth, max(editorBounds.W-animEdgeL-animEdgeR, 0))
	}
	s.frameAnnotations = s.placeAnnotations(c, editorBounds.X+animEdgeL, canvasWidth)

	root.Draw(func(_ *ui.Painter, r ui.Rect) {
		s.editorRect = r
	})
	root.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		s.editorRect = r
		appUi.RecordBounds("editor", r)
		if len(s.annotations) > 0 && !nearDocRect(r, editorBounds) {
			c.Invalidate()
		}
	})

	s.preUpdateBlockDrag(c)
	s.preUpdateTableExtend(c)
	s.preUpdateFocusedDrag(c, root)

	root.Children(func() {
		topPad := s.canvasTopPad()
		bottomPad := s.scale(editorBottomPad)
		bottomMinH := s.scale(120)
		if s.embedded {
			bottomPad = 0
			bottomMinH = 0
		} else if s.minimal {
			bottomPad = minimalBottomPad
			bottomMinH = 36
		}
		if s.dragActive && s.dragSavedHeight > 0 {
			bottomMinH += s.dragSavedHeight
		}
		if savedTableH := s.tableExtendSavedHeight(); savedTableH > 0 {
			bottomMinH += savedTableH
		}

		renderCanvas := func() {
			// Center column: max 672px body + symmetric 68px left/right gutters (54px handle gutter + 14px gap) = 808px (scaled by editor zoom).
			outerMaxW := s.outerFlowWidth()
			col := ui.Column(c.Key("editor-canvas")).FillWidth().AlignItems(ui.Center).
				Padding(topPad, animEdgeR, bottomPad, animEdgeL)
			if s.embedded {
				col.ObserveInput(s.onScrollObserveInput)
				if sb := col.Bounds(); sb.W > 0 && sb.H > 0 {
					s.scrollRect = sb
				}
				col.Draw(func(_ *ui.Painter, r ui.Rect) {
					s.scrollRect = r
				})
				col.DrawOver(func(_ *ui.Painter, r ui.Rect) {
					s.scrollRect = r
				})
			}
			if cb := col.Bounds(); cb.W > 0 && cb.H > 0 {
				s.canvasLayoutOrigin = ui.Point{X: cb.X, Y: cb.Y}
				s.hasCanvasOrigin = true
			} else {
				s.hasCanvasOrigin = false
			}
			if !s.embedded {
				if minCanvasH := max(s.minCanvasHeight, s.scrollRect.H); minCanvasH > 0 {
					col.MinHeight(minCanvasH)
				}
			}
			if !readOnly {
				col.Cursor(ui.CursorText)
			}
			svcCanvas := c.Services()
			col.ContextMenu(func(m *ui.Menu) {
				canCopy := s.hasNonEmptySelection() || s.hasFocusedAtomicBlock()
				canSelectAll := s.doc != nil && len(s.doc.Blocks) > 0
				if !readOnly {
					if m.Item(theme.Tr("剪切", "Cut")).Disabled(!canCopy).Chosen() {
						s.CutSelection(svcCanvas)
					}
					if m.Item(theme.Tr("拷贝", "Copy")).Disabled(!canCopy).Chosen() {
						s.CopySelection(svcCanvas)
					}
					if m.Item(theme.Tr("粘贴", "Paste")).Chosen() {
						s.PasteFromClipboard(svcCanvas, svcCanvas.ReadClipboard(), false)
					}
					if m.Item(theme.Tr("粘贴并匹配样式", "Paste and Match Style")).Chosen() {
						s.PastePlainFromClipboard(svcCanvas, svcCanvas.ReadClipboard(), false)
					}
					if m.Item(theme.Tr("删除", "Delete")).Disabled(!canCopy).Chosen() {
						if s.hasNonEmptySelection() {
							s.DeleteSelection()
						} else if s.hasFocusedAtomicBlock() {
							s.deleteBlock(s.focusedBlock)
						}
					}
					m.Separator()
					if m.Item(theme.Tr("全选", "Select All")).Disabled(!canSelectAll).Chosen() {
						s.SelectAllBlocks()
					}
				} else {
					if m.Item(theme.Tr("拷贝", "Copy")).Disabled(!canCopy).Chosen() {
						s.CopySelection(svcCanvas)
					}
					m.Separator()
					if m.Item(theme.Tr("全选", "Select All")).Disabled(!canSelectAll).Chosen() {
						s.SelectAllBlocks()
					}
				}
				s.appendInlinePathContextMenu(m, svcCanvas, "canvas")
				if s.ExtraContextItems != nil {
					if extra := s.ExtraContextItems(); len(extra) > 0 {
						sepAdded := false
						for _, ex := range extra {
							if ex.Label == "" {
								continue
							}
							if !sepAdded {
								m.Separator()
								sepAdded = true
							}
							it := m.Item(ex.Label)
							if ex.Icon != nil {
								it.Icon(ex.Icon)
							}
							if it.Chosen() && ex.OnChoose != nil {
								ex.OnChoose()
							}
						}
					}
				}
			})
			s.updateCanvasMouseSelection(c, root, col, readOnly)

			col.Children(func() {
				// Top margin spacer preserved for layout symmetry; clicks & marquee drags handled by editor-canvas.
				if !readOnly {
					ui.Box(c.Key("editor-top-margin")).Absolute().Top(0).Left(0).Right(0).Height(topPad).
						Cursor(ui.CursorText)
				}

				flow := ui.Column(c.Key("editor-flow")).FillWidth()
				if !s.embedded {
					flow.MaxWidth(outerMaxW)
				}
				flow.Children(func() {
					virtSig := viewportWidth*1000 + s.zoom()
					canVirt := !s.embedded && !appUi.IsProbeEnabled() && s.virtReady && s.lastVirtSig == virtSig && len(s.doc.Blocks) > virtualizeBlockThreshold && s.scrollRect.H > 0
					s.lastVirtSig = virtSig
					s.virtReady = true

					scrolling := s.IsScrollingForHover(c.Now())
					frameMeta := s.prepareFrameBlocks()
					nextHover := -1
					nBlocks := len(s.doc.Blocks)
					for i := 0; i < nBlocks; i++ {
						if frameMeta[i].Hidden {
							continue
						}
						if handled, hov := s.renderInlineAIPreviewRow(c, i, readOnly); handled {
							if hov {
								nextHover = i
							}
							continue
						}
						if handled, hov := s.renderPlanReviewRow(c, i, frameMeta[i]); handled {
							if hov {
								nextHover = i
							}
							continue
						}
						if canVirt {
							if cachedR, ok := s.canVirtualizeBlock(i, s.doc.Blocks[i].ID); ok {
								firstID := s.doc.Blocks[i].ID
								sumH := cachedR.H
								var spanEntries []virtBlockEntry
								lastVirt := i
								for j := i + 1; j < nBlocks; j++ {
									if frameMeta[j].Hidden {
										lastVirt = j
										continue
									}
									if s.inlineAI != nil && s.isInlineAIPreviewIndex(j) {
										break
									}
									if s.isPlanReviewMember(s.doc.Blocks[j].ID) {
										break
									}
									rj, okJ := s.canVirtualizeBlock(j, s.doc.Blocks[j].ID)
									if !okJ {
										break
									}
									if spanEntries == nil {
										spanEntries = []virtBlockEntry{{id: firstID, h: cachedR.H}}
									}
									spanEntries = append(spanEntries, virtBlockEntry{id: s.doc.Blocks[j].ID, h: rj.H})
									sumH += rj.H
									lastVirt = j
								}
								if len(spanEntries) <= 1 {
									sp := ui.Box(c.Key(firstID)).FillWidth().Height(cachedR.H)
									s.recordBlockLayoutBounds(firstID, sp)
									sp.DrawOver(func(_ *ui.Painter, r ui.Rect) {
										s.recordBlockWinRect(firstID, r)
									})
								} else {
									entries := spanEntries
									sp := ui.Box(c.Key(virtBlockSpanKey{first: firstID})).FillWidth().Height(sumH)
									if s.hasCanvasOrigin {
										if b := sp.Bounds(); b.W > 0 && b.H > 0 {
											curY := b.Y - s.canvasLayoutOrigin.Y
											docX := b.X - s.canvasLayoutOrigin.X
											for _, ent := range entries {
												s.setBlockDocRect(ent.id, ui.Rect{X: docX, Y: curY, W: b.W, H: ent.h})
												curY += ent.h
											}
										}
									}
									sp.DrawOver(func(_ *ui.Painter, r ui.Rect) {
										if r.W <= 0 || r.H <= 0 {
											return
										}
										docBase := s.winToDocRect(r)
										curY := docBase.Y
										for _, ent := range entries {
											s.setBlockDocRect(ent.id, ui.Rect{X: docBase.X, Y: curY, W: docBase.W, H: ent.h})
											curY += ent.h
										}
									})
								}
								i = lastVirt
								continue
							}
						}
						hovered := s.renderBlockRowMeta(c, i, readOnly, frameMeta[i])
						if hovered {
							nextHover = i
						}
					}
					if scrolling {
						s.hoveredBlock = -1
						s.trailingPlus = false
					} else {
						nextHover = s.updateTrailingAddHover(c, root, nextHover, readOnly)
						if nextHover != s.hoveredBlock {
							s.hoveredBlock = nextHover
							c.Invalidate()
						}
					}
				})
				s.flushPendingBlockDrop(c)

				// Bottom blank space: clicking creates/focuses a trailing paragraph; dragging box-selects blocks.
				if !readOnly {
					ui.Box(c.Key("editor-bottom-margin")).FillWidth().MaxWidth(outerMaxW).
						MinHeight(bottomMinH).Grow(1).Cursor(ui.CursorText)
				}
				if s.annotExtraBottom > 0 {
					ui.Box(c.Key("editor-annotation-tail")).FillWidth().Height(s.annotExtraBottom).Shrink(0)
				}
			})
		}

		if s.embedded {
			col := ui.Box(c.Key("embedded-input-obs")).Fill().ObserveInput(s.onScrollObserveInput)
			col.Children(renderCanvas)
		} else {
			// Main scrollable document body
			scroll := appUi.Scroll(c.Key("editor-scroll"), &s.bodyScroll).Fill().
				ObserveInput(s.onScrollObserveInput)
			if sb := scroll.Bounds(); sb.W > 0 && sb.H > 0 {
				s.scrollRect = sb
			}
			renderedScrollRect := s.scrollRect
			scroll.Draw(func(p *ui.Painter, r ui.Rect) {
				s.scrollRect = r
				s.paintBackground(p, r, pal, animEdgeL, animEdgeR)
			})
			scroll.DrawOver(func(_ *ui.Painter, r ui.Rect) {
				s.scrollRect = r
				if !nearDocRect(r, renderedScrollRect) {
					c.Invalidate()
				}
			})
			scroll.Children(renderCanvas)
		}

		if s.needsDetachedSelectionFocus() {
			svc := c.Services()
			caret := s.detachedCaretRect()
			multiFocus := ui.Box(c.Key("multi-sel-focus:"+s.notePath)).Absolute().Left(0).Top(0).Size(0, 0).
				Bind(&s.detachedFocus).Focusable().TextCaret(caret).
				HandleInput(func(ev ui.InputEvent) bool {
					return s.handleMultiSelectionInput(svc, ev)
				})
			if s.multiSelFocusPending {
				multiFocus.Focus(ui.FocusOptions{PreventScroll: true})
				s.multiSelFocusPending = false
			}
			multiFocused := multiFocus.Focused()
			if multiFocused {
				s.frameTabChildFocused = true
			}
			if !multiFocused && !s.multiSelFocusPending {
				s.detachedCompose = ""
			}
			if !readOnly && s.detachedCompose != "" && multiFocused && c.WindowActive() {
				ui.Box(c.Key("multi-sel-compose")).Absolute().Left(caret.X).Top(caret.Y).
					Padding(theme.Space2, theme.Space6).Radius(theme.RadiusXS).
					Background(pal.Paper).Border(1, pal.Accent).IgnoreInput(true).
					Children(func() {
						ui.Text(c, s.detachedCompose).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
					})
			}
		} else {
			s.detachedCompose = ""
		}

		// Render floating overlays
		s.renderTrailingAddOverlay(c, readOnly)
		s.renderBoxSelectMarquee(c)
		s.renderFindBar(c)
		s.renderSelectionToolbar(c)
		s.renderBlockMenuOverlay(c)
		s.renderCodeLangMenuOverlay(c)
		s.renderCalloutMenuOverlay(c)
		s.renderTableMenuOverlay(c)
		s.renderInlineAIPanel(c)
		s.pollAnnotationGeneration()
		s.renderAnnotationRail(c)
		// The outline floats above the document and annotation rail, below window popups.
		s.renderOutlineRail(c)
		s.renderAnnotationReactionPicker(c)
		if s.frameAnnotations.hovered != s.hoveredAnnotID {
			s.hoveredAnnotID = s.frameAnnotations.hovered
			c.Invalidate()
		}
		s.renderPlanReviewBar(c)
		s.renderMediaLightbox(c)
		s.renderMindMapFullscreen(c, readOnly)
		s.renderHoveredPathTooltip(c)
		if s.rootHandle.FocusWithin(c) && !s.frameTabChildFocused {
			root.Shortcut(0, ui.KeyTab)
			root.Shortcut(ui.Shift, ui.KeyTab)
		}
		if s.hasMenuPopupOpen() {
			appUi.ReserveMenuNavKeys(c)
			c.Shortcut(0, ui.KeyTab)
			c.Shortcut(ui.Shift, ui.KeyTab)
		}
	})
}

func (s *Session) focusOrInsertBottomParagraph() {
	s.insertOrFocusBottomParagraph()
}

// HasAnnotationInputFocused reports whether the floating annotation composer or
// an in-place annotation card editor currently holds keyboard focus.
func (s *Session) HasAnnotationInputFocused(c *ui.Context) bool {
	return s.commentHandle.Focused(c) || s.editingHandle.Focused(c)
}

var findShortcut = func() *atomic.Value { v := &atomic.Value{}; v.Store("cmd-f"); return v }()

// SetFindShortcut sets the chord that opens find-in-note for every editor session.
func SetFindShortcut(spec string) {
	if strings.TrimSpace(spec) == "" {
		spec = "cmd-f"
	}
	findShortcut.Store(spec)
}

// FindShortcut returns the active find chord shared by note and source editors.
func FindShortcut() string {
	return findShortcut.Load().(string)
}

func (s *Session) isFindShortcut(mods ui.Modifiers, key ui.Key) bool {
	m, k, ok := appUi.ParseShortcut(FindShortcut())
	return ok && m == mods && k == key
}

func (s *Session) handleGlobalShortcuts(c *ui.Context, readOnly bool) {
	if s.lightbox.Open {
		return
	}
	if s.HasMindMapFullscreen() {
		if s.previewMedia == nil && appUi.MatchShortcut(c, FindShortcut()) && s.openMindMapFind(c) {
			c.Invalidate()
		}
		return
	}
	svc := c.Services()
	annotInputFocused := s.HasAnnotationInputFocused(c)
	mindMapFocused := s.HasMindMapFocus(c)
	// Only register window-level clipboard/selectAll shortcuts when no editor input
	// holds focus; otherwise c.Shortcut claims the key in mygo before RichInput /
	// multi-sel-focus HandleInput can handle single-block Cmd+X or staged table Cmd+A.
	canGlobalClip := s.inlineAI == nil && !s.findOpen && !s.HasFocus(c) && !c.FocusTakesText() && !mindMapFocused
	if canGlobalClip && (s.hasNonEmptySelection() || s.hasFocusedAtomicBlock()) && c.Shortcut(ui.Cmd, ui.KeyC) {
		s.CopySelection(svc)
	}
	if s.previewMedia != nil {
		return
	}
	if appUi.MatchShortcut(c, FindShortcut()) {
		if s.openMindMapFind(c) {
			c.Invalidate()
			return
		}
		s.ToggleFind()
	}
	if appUi.MatchShortcut(c, appUi.FindReplaceShortcut()) {
		s.OpenFindReplace()
	}
	if s.findOpen {
		if theme.IsMacOS() {
			if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyG) {
				s.FindPrev()
			} else if c.Shortcut(ui.Cmd, ui.KeyG) {
				s.FindNext()
			}
		} else {
			if c.Shortcut(ui.Shift, ui.KeyF3) {
				s.FindPrev()
			} else if c.Shortcut(0, ui.KeyF3) {
				s.FindNext()
			}
		}
	}
	outlineInputFocused := s.outlineInputHandle.Focused(c)
	if !annotInputFocused && !outlineInputFocused && c.Shortcut(0, ui.KeyEscape) {
		if s.dragBlockID != 0 {
			s.cancelBlockDrag()
		} else if s.isTableExtending() {
			s.cancelTableExtend()
		} else if s.inlineAI != nil {
			s.aiEscape()
		} else if s.findOpen {
			s.CloseFind()
		} else if s.editingAnnotID != "" {
			s.editingAnnotID = ""
			s.editingFocusPending = false
		} else if s.hasAnyOverlayOpen() {
			s.closeTransientOverlays()
		}
	}
	s.handlePlanReviewShortcuts(c, annotInputFocused)
	if readOnly {
		return
	}
	if s.rootHandle.FocusWithin(c) && !s.hasBlockingOverlay() && !mindMapFocused {
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
	if !mindMapFocused {
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ) || c.Shortcut(ui.Cmd, ui.KeyY) {
			s.Redo()
		} else if c.Shortcut(ui.Cmd, ui.KeyZ) {
			s.Undo()
		}
	}
	if !annotInputFocused && !mindMapFocused {
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
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyX) || c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS) {
			s.ToggleMarkOnSelection(model.MarkStrike)
		}
	}
	if canGlobalClip && (s.hasNonEmptySelection() || s.hasFocusedAtomicBlock()) && c.Shortcut(ui.Cmd, ui.KeyX) {
		s.CutSelection(svc)
	}
	if canGlobalClip && (c.Shortcut(ui.Cmd|ui.Shift, ui.KeyV) || c.Shortcut(ui.Cmd|ui.Shift|ui.Alt, ui.KeyV)) {
		s.PastePlainFromClipboard(svc, svc.ReadClipboard(), false)
	}
	if canGlobalClip && c.Shortcut(ui.Cmd, ui.KeyV) {
		s.PasteFromClipboard(svc, svc.ReadClipboard(), false)
	}
	if !s.hasBlockingOverlay() && canGlobalClip && c.Shortcut(ui.Cmd, ui.KeyA) {
		s.SelectCurrentBlockOrAll()
	}
	if !mindMapFocused {
		s.handleGlobalBlockShortcuts(c)
	}
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

func (s *Session) isInlineAIPreviewIndex(idx int) bool {
	if s.inlineAI == nil {
		return false
	}
	lo := min(s.inlineAI.TargetAnchor.Block, s.inlineAI.TargetHead.Block)
	hi := max(s.inlineAI.TargetAnchor.Block, s.inlineAI.TargetHead.Block)
	return idx >= lo-1 && idx <= hi+1
}

func (s *Session) canVirtualizeBlock(idx int, id model.BlockID) (ui.Rect, bool) {
	if idx == 0 || idx == len(s.doc.Blocks)-1 || idx == s.focusedBlock || idx == s.hoveredBlock {
		return ui.Rect{}, false
	}
	if s.dragBlockID != 0 || s.jump.block == id {
		return ui.Rect{}, false
	}
	if s.isMouseDragging() && idx == s.mouseOriginBlock {
		return ui.Rect{}, false
	}
	if s.hasNonEmptySelection() {
		a, b := s.orderedSelection()
		if (idx >= a.Block-1 && idx <= a.Block+1) || (idx >= b.Block-1 && idx <= b.Block+1) {
			return ui.Rect{}, false
		}
	}
	if s.blockMenu != nil && idx == s.blockMenu.BlockIdx {
		return ui.Rect{}, false
	}
	if s.isInlineAIPreviewIndex(idx) {
		return ui.Rect{}, false
	}
	if s.findOpen && s.findCurrent >= 0 && s.findCurrent < len(s.findMatches) && s.findMatches[s.findCurrent].BlockIdx == idx {
		return ui.Rect{}, false
	}
	if s.calloutMenuBlock == id || s.codeLangMenuBlock == id || s.tableMenuBlock == id || s.imagePopBlock == id || s.imgResizeBlock == id || s.isTableExtendingBlock(id) {
		return ui.Rect{}, false
	}
	r, ok := s.blockRect(id)
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

func (s *Session) rowLeftX(id model.BlockID) float32 {
	if r, ok := s.blockRect(id); ok && r.W > 0 {
		return r.X
	}
	outerW := s.outerFlowWidth()
	flowW := min(s.viewportWidth, outerW)
	return s.editorRect.X + s.edgeInsetLeft + max((s.viewportWidth-flowW)*0.5, 0)
}

func (s *Session) renderBlockRowMeta(c *ui.Context, idx int, readOnly bool, meta frameBlockMeta) bool {
	pal := theme.Ocean()
	b := &s.doc.Blocks[idx]
	id := b.ID

	isCollapsedDrag := s.isCollapsedDragRow(id)
	topGap := s.blockGap(meta.PrevVisible, b)
	bandBleed := s.bandBleed()
	rowBleed := float32(0)
	if isSurfaceBleedBlock(b.Kind) && !isCollapsedDrag {
		rowBleed = bandBleed
	}
	leftPad := float32(0)
	if s.minimal {
		leftPad = max(minimalLeftInset-rowBleed, 0)
	}
	rightPad := max(s.rightContentInset()-rowBleed, 0)

	isAtomic := b.Kind == model.BlockImage || b.Kind == model.BlockDivider
	row := ui.Column(c.Key(id)).FillWidth().Padding(topGap, rightPad, 0, leftPad)
	s.recordBlockLayoutBounds(id, row)
	if !readOnly {
		if isAtomic {
			row.Cursor(ui.CursorDefault)
		} else {
			row.Cursor(ui.CursorText)
		}
	}
	isHovered := !s.IsScrollingForHover(c.Now()) && row.Hovered()
	isFocused := s.focusedBlock == idx
	hasBlockFocus := isFocused && isAtomic && (row.FocusWithin() || s.detachedFocus.Focused(c))
	hasTextFocus := isFocused && !isAtomic && (row.FocusWithin() || s.focusPending)
	hoveredForHandles := isHovered
	if isHovered && hasTextFocus && !s.minimal && !readOnly {
		// The active editor keeps its gutter quiet while the pointer is over text.
		// Never call row.PointerPosition() here: setting flagTrackPointer on row marks
		// it interactive in mygo and intercepts clicks/drags meant for editor-canvas.
		if pt, over := s.windowPointerStatus(); over {
			hoveredForHandles = pt.X-s.rowLeftX(id) < s.leftContentGutter()
		} else {
			hoveredForHandles = false
		}
	}
	isHighlighted := s.jump.block == id && s.jump.row < 0
	deleteArmed := idx == 0 && s.firstLineDeleteArmed && s.canArmFirstLineDelete()

	dragTarget := s.dragActive && s.dragOverIdx == idx
	var highlightBand func(ui.Rect) ui.Rect
	if isHighlighted {
		leftGutter := s.leftContentGutter()
		rightInset := s.rightContentInset()
		padY := s.scale(theme.Space2)
		highlightBand = func(r ui.Rect) ui.Rect {
			return ui.Rect{
				X: r.X + leftGutter - bandBleed,
				Y: r.Y + topGap - padY,
				W: max(r.W-leftGutter-rightInset+2*bandBleed, 0),
				H: max(r.H-topGap+2*padY, 0),
			}
		}
		row.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(highlightBand(r), pal.EditorSelectionBG().Alpha(s.jumpAlpha), theme.RadiusSM)
		})
	}
	row.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if s.recordBlockWinRect(id, r) {
			if s.IsSelectionToolbarOpen() || s.inlineAI != nil || s.blockMenu != nil || s.calloutMenuBlock == id || len(s.frameAnnotations.right) > 0 {
				c.Invalidate()
			}
		}
		if appUi.IsProbeEnabled() {
			appUi.RecordBounds(fmt.Sprintf("block-%d", id), r)
		}
		if highlightBand != nil {
			band := highlightBand(r)
			p.Stroke(band, pal.Editor().JumpRing.Alpha(s.jumpAlpha), theme.RadiusSM, theme.JumpHighlightBorderW)
			appUi.RecordBounds("outline-target-highlight", band)
		}
		if dragTarget {
			s.paintBlockDropMarker(p, r, b, pal)
		}
	})
	if s.dragActive && s.dragBlockID == id {
		row.Opacity(0.55)
	}

	overlayBlocking := s.hasAnyOverlayOpen() || s.hoveredAnnotID != "" || s.imgResizeBlock != 0 || s.isTableExtending()
	isDraggingBlock := s.dragBlockID != 0
	isDraggingAny := isDraggingBlock || s.isMouseDragging() || s.isTableExtending()
	tableWholeActive := b.Kind == model.BlockTable && s.isTableWholeSelected(id)
	showIdleHandles := !overlayBlocking && !isDraggingAny && s.typingPointer == nil
	showHandles := !s.minimal && !readOnly && ((showIdleHandles && (hoveredForHandles || hasBlockFocus)) || (!overlayBlocking && s.dragBlockID == id) || tableWholeActive)
	showBlankAdd := !s.minimal && !readOnly && showIdleHandles && (hoveredForHandles || (!hasTextFocus && s.hoveredBlock == idx))
	// A document that is just one blank line keeps its add button visible;
	// it enters by playing the hover animation once instead of popping in.
	soloBlank := !s.minimal && !readOnly && len(s.doc.Blocks) == 1 && isPlainEmptyParagraph(b) &&
		!overlayBlocking && !isDraggingAny
	soloIntro := false
	if soloBlank {
		showBlankAdd = true
		if s.soloAddID != id {
			s.soloAddID, s.soloAddAt = id, time.Now()
		}
		if time.Since(s.soloAddAt) < soloAddIntroDur {
			soloIntro = true
			c.Invalidate()
		}
	} else if s.soloAddID == id {
		s.soloAddID = 0
	}
	isFolded := !s.minimal && !readOnly && b.Kind.IsHeading() && s.folded[id]
	blankPara := isPlainEmptyParagraph(b)
	listCtx := meta.List

	if s.inlineAI != nil {
		s.paintInlineAIOutline(c, row, id, topGap)
	}

	gripHoverBG := pal.Editor().GripHoverBG

	row.Children(func() {
		if isHighlighted {
			s.revealJump(c, topGap+s.matchLineOffsetInBlock(idx, b, topGap))
		}
		var listFontSz, lineH float32
		if b.Kind.IsList() {
			var lh float32
			listFontSz, _, lh = s.textMetrics(b.Kind)
			lineH = listFontSz * lh
		}
		selfStemTop := float32(0)
		showSelfStem := b.Kind.IsList() && listCtx.HasSelfGuide && !isCollapsedDrag
		if showSelfStem {
			selfStemTop = topGap + listStemTopOffset(b.Kind, len(listCtx.Ancestors), listFontSz, lineH, s.zoom())
		}
		drawListGuides(c, id, listCtx, s.leftContentGutter(), s.zoom(), selfStemTop, showSelfStem)
		line := ui.Row(c.Key("row-inner")).FillWidth().AlignItems(ui.Start)
		line.Children(func() {
			if !s.minimal {
				// Keep the gutter width fixed as controls appear and disappear.
				showDeleteArm := !readOnly && !overlayBlocking && !isDraggingAny && deleteArmed
				showFold := !readOnly && b.Kind.IsHeading() && !overlayBlocking && (!isDraggingBlock || s.dragBlockID == id) && (showHandles || isFolded)
				hasGutterControls := showDeleteArm || (blankPara && showBlankAdd) || (!blankPara && showHandles) || showFold

				gutter := ui.Box(c.Key("gutter")).Width(s.handleWidth()).Height(0).Shrink(0).
					Margin(0, max(s.rowGap()-rowBleed, 0), 0, 0)
				if hasGutterControls {
					gutter.Children(func() {
						btn24 := s.scale(24)
						handleTop := s.blockHandleTopScaled(b, topGap)
						if isCollapsedDrag {
							handleTop = max((s.scale(theme.HeightCompact)-btn24)*0.5, -topGap)
						}
						icGutter := s.scale(theme.IconChrome)
						strip := ui.Row(c.Key("handle-strip")).Absolute().Left(0).Right(0).Top(handleTop).Height(btn24).
							Justify(ui.End).AlignItems(ui.Center).Gap(s.scale(2)).PassThrough().
							Transition(appUi.GutterHandleTransition())
						strip.Children(func() {
							if showDeleteArm {
								delBtn := ui.ButtonBase(c.Key("first-line-delete-badge")).Size(btn24, btn24).Center().
									Radius(theme.RadiusSM).
									KeepFocus().Cursor(ui.CursorPointer).Label(theme.Tr("删除空行", "Delete empty line"))
								delHovered := delBtn.Hovered()
								delPressed := delBtn.Pressed()
								delBG := pal.DangerSurface
								if delPressed {
									delBG = pal.Danger.Alpha(0.22)
								}
								appUi.AnimateSurfaceColors(delBtn, delBG, pal.Danger.Alpha(0.4), true)
								appUi.TactileButton(delBtn, delHovered, delPressed, false)
								delKeyHint := theme.Tr("再按 ⌫ 或点击", "Press ⌫ or click")
								if !theme.IsMacOS() {
									delKeyHint = theme.Tr("再按 Backspace 或点击", "Press Backspace or click")
								}
								appUi.TooltipTrigger(c, delBtn,
									delKeyHint,
									theme.Tr("删除首行", "to delete first line"),
									true,
								)
								delBtn.Children(func() {
									icons.DrawAnimated(c, icons.KeyBackspace, s.scale(theme.IconAction), pal.TintedIconColors(pal.Danger), delHovered || delPressed)
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
										IconSize:    icGutter,
										ForceHover:  soloIntro,
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
											trigger := s.blockTextRectValue(id)
											if trigger.W <= 0 {
												if r, ok := s.blockRect(id); ok && r.W > 0 {
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
								if !gripActive && !gripPressed && s.blockMenu == nil && !tableWholeActive {
									clickDetail := theme.Tr("打开块菜单", "to open menu")
									if b.Kind == model.BlockTable {
										clickDetail = theme.Tr("选中整表边框", "to select table")
									} else if !(idx == 0 && isFrontmatterBlock(0, b)) &&
										(b.PlainText() != "" || b.Code != "" || b.Kind == model.BlockImage || b.Kind == model.BlockDivider) {
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
								gripBG := ui.Transparent
								gripBD := ui.Transparent
								if tableWholeActive {
									gripBG = pal.Selected
									gripBD = pal.SelectedBorder
								} else if gripActive || gripPressed || gripHovered {
									gripBG = gripHoverBG
								}
								appUi.AnimateSurfaceColors(grip, gripBG, gripBD, tableWholeActive)
								appUi.TactileButton(grip, gripHovered || gripActive || tableWholeActive, gripPressed, tableWholeActive)
								gripIcon := pal.DuoIconColors(pal.TextSecondary)
								if tableWholeActive {
									gripIcon = pal.LitDuoIconColors(pal.Text)
								} else if gripActive || gripPressed || gripHovered {
									gripIcon = pal.DuoIconColors(pal.Text)
								}
								grip.Children(func() {
									icons.DrawAnimated(c, icons.Drag, icGutter, gripIcon, tableWholeActive || gripActive || gripPressed || gripHovered)
								})
								appUi.Probe(grip, gripID)
								s.recordOverlayBtn(gripID, grip)
								s.handleBlockGripGesture(c, grip, idx, id)
							}
							if showFold {
								tip := theme.Tr("折叠章节", "Collapse section")
								childCount := s.headingSectionChildCount(idx)
								var detail string
								if childCount > 0 {
									detail = fmt.Sprintf(theme.Tr("收起 %d 项", "Hide %d"), childCount)
								}
								if isFolded {
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
								foldID := fmt.Sprintf("block-fold-%d", id)
								foldBtn := ui.ButtonBase(c.Key(foldID)).Size(btn24, btn24).Center().
									Radius(theme.RadiusMD).Cursor(ui.CursorPointer).KeepFocus().
									Label(theme.Tr("折叠章节", "Toggle fold"))
								appUi.RichTooltip(c, foldBtn, foldSpec)
								foldHover := foldBtn.Hovered()
								foldPress := foldBtn.Pressed()
								foldBG := ui.Transparent
								if foldPress {
									foldBG = pal.Selected
								} else if foldHover {
									foldBG = pal.Hover
								}
								appUi.AnimateSurfaceColors(foldBtn, foldBG, ui.Transparent, false)
								appUi.TactileButton(foldBtn, foldHover, foldPress, false)
								targetDeg := float32(90)
								if isFolded {
									targetDeg = 0
								}
								turnDeg := foldBtn.AnimateTo("foldTurn", targetDeg, appUi.SpringTactile)
								foldBtn.Children(func() {
									rotWrap := ui.Box(c).Size(icGutter, icGutter).Shrink(0).Center()
									if turnDeg != 0 {
										rotWrap.Rotate(turnDeg)
									}
									rotWrap.Children(func() {
										ic := pal.HoverIconColors(foldHover || foldPress)
										icons.DrawAnimated(c, icons.ChevronRight, icGutter, ic, foldHover || foldPress)
									})
								})
								appUi.Probe(foldBtn, foldID)
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
			body := ui.Column(c.Key("body")).Grow(1).Shrink(1).MinWidth(0).MaxWidth(bodyMaxW)
			body.Children(func() {
				if isCollapsedDrag {
					s.renderCollapsedDragBlock(c, idx, b)
				} else {
					s.renderBlockBody(c, idx, b, isFocused, isHovered, readOnly, deleteArmed)
				}
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
		s.renderRichTextBlock(c, idx, b, isFocused, isHovered, readOnly, deleteArmed)
	}
	if !s.minimal {
		s.renderBlockAnnotationsFoot(c, b)
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
	return theme.ResolvedEditorFont()
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
		return 28, theme.WeightTitle, 1.25
	case model.BlockHeading2:
		return 20, theme.WeightTitle, 1.3
	case model.BlockHeading3:
		return 17, theme.WeightTitle, 1.35
	case model.BlockHeading4:
		return 15, theme.WeightTitle, 1.4
	case model.BlockHeading5:
		return 14, theme.WeightTitle, 1.45
	case model.BlockHeading6:
		return 14, 500, 1.45
	default:
		_, _, lhMode := theme.Typography()
		return theme.FontEditor, theme.WeightRegular, lhMode.Factor() // 15px body
	}
}

func (s *Session) textMetrics(kind model.BlockKind) (fontSize float32, weight int, lineHeight float32) {
	if s.minimal {
		switch kind {
		case model.BlockHeading1:
			return 22, theme.WeightTitle, 1.28
		case model.BlockHeading2:
			return 19, theme.WeightTitle, 1.32
		case model.BlockHeading3:
			return 17, theme.WeightTitle, 1.36
		case model.BlockHeading4:
			return 16, theme.WeightTitle, 1.4
		case model.BlockHeading5:
			return 15, theme.WeightTitle, 1.45
		case model.BlockHeading6:
			return 14, 500, 1.45
		}
	}
	fontSize, weight, lh := blockTextMetrics(kind)
	return fontSize * s.zoom(), weight, lh
}

func (s *Session) renderRichTextBlock(c *ui.Context, idx int, b *model.Block, isFocused, isHovered, readOnly, deleteArmed bool) {
	pal := theme.Ocean()
	dark := pal.Dark
	fontSize, weight, lh := s.textMetrics(b.Kind)

	wrap := ui.Column(c.Key("text-wrap")).FillWidth()
	qDepth := b.QuoteDepth
	if b.Kind == model.BlockQuote && qDepth < 1 {
		qDepth = 1
	}
	if dec, kind, ok := s.matchBlockDecorator(b); ok {
		st := pal.Callout(decoratorCalloutKind(dec, kind))
		wrap.Padding(s.scale(6), s.scale(12)).Radius(theme.RadiusMD).Background(st.BG).Border(1, st.Border)
	} else if b.Kind == model.BlockCallout {
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
				iconHovered := iconBtn.Hovered()
				iconPressed := iconBtn.Pressed()
				iconBG, iconBD := ui.Transparent, ui.Transparent
				if open {
					iconBG, iconBD = pal.Paper, st.Accent
				} else if iconHovered || iconPressed {
					iconBG, iconBD = pal.Paper, st.Border
				}
				appUi.AnimateSurfaceColors(iconBtn, iconBG, iconBD, true)
				appUi.TactileButton(iconBtn, iconHovered, iconPressed, false)
				iconBtn.Children(func() {
					calloutIC := pal.TintedIconColors(st.Accent)
					calloutIC.Fill = st.IconFill
					icons.DrawAnimated(c, icons.Callout, s.scale(16), calloutIC, open || iconHovered || iconPressed)
				})
				appUi.Probe(iconBtn, "callout-icon")
				if iconBtn.Clicked() {
					toggleMenu()
				}

				pillID := fmt.Sprintf("callout-pill-%d", b.ID)
				pillBtn := ui.ButtonBase(c.Key(pillID)).Height(cBtnSz).PaddingX(s.scale(4)).Gap(s.scale(4)).
					AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(calloutLabel(curKind))
				pillHovered := pillBtn.Hovered()
				pillPressed := pillBtn.Pressed()
				pillBG, pillBD := ui.Transparent, ui.Transparent
				if open {
					pillBG, pillBD = pal.Paper, st.Accent
				} else if pillHovered || pillPressed {
					pillBG, pillBD = pal.Paper, st.Border
				}
				appUi.AnimateSurfaceColors(pillBtn, pillBG, pillBD, true)
				appUi.TactileCard(pillBtn, pillHovered, pillPressed, open, false)
				pillBtn.Children(func() {
					ui.Text(c, calloutLabel(curKind)).FontSize(s.scale(14)).FontWeight(theme.WeightTitle).TextColor(st.Text).SingleLine()
					icons.DrawAnimated(c, icons.TbChevron, s.scale(theme.IconInline), pal.TintedIconColors(st.Text), open || pillHovered || pillPressed)
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
		showInlineDiff := s.blockHasInlineRewrite(idx) && s.annotInlineEditBlock != b.ID
		if isFocused && !readOnly && !s.needsDetachedSelectionFocus() && (!s.hasMultiBlockSelection() || inOriginDrag) && !showInlineDiff {
			s.renderEditableTextBlock(c, idx, b, fontSize, weight, lh, deleteArmed)
			return
		}
		textCol := pal.Text
		if b.Kind == model.BlockHeading6 {
			textCol = pal.TextSecondary
		}
		edFont := resolvedEditorFont()
		plain := b.PlainText()
		origPlain := plain
		isDecorated := false
		if dec, _, ok := s.matchBlockDecorator(b); ok {
			isDecorated = true
			plain = decoratorDisplayText(plain, dec)
		}
		var rt ui.Element
		var pathCursor bool
		if isDecorated && plain != "" && (len(b.Spans) == 0 || strings.TrimSpace(origPlain) == "") {
			rt = ui.Text(c.Key("rt-empty"), plain).PreserveSpaces().FontSize(fontSize).FontWeight(weight).LineHeight(lh).
				MinHeight(fontSize * lh).TextColor(pal.Placeholder).FillWidth()
		} else if isDecorated && plain != origPlain && plain != "" {
			rt = ui.Text(c.Key("rt"), plain).PreserveSpaces().FontSize(fontSize).FontWeight(weight).LineHeight(lh).
				MinHeight(fontSize * lh).TextColor(textCol).FillWidth()
		} else if len(b.Spans) == 0 || plain == "" {
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
		} else if showInlineDiff {
			rt = ui.RichText(c.Key("rt"), s.buildInlineDiffDisplaySpans(idx, b, fontSize, weight)...).PreserveSpaces().FontSize(fontSize).FontWeight(weight).
				LineHeight(lh).MinHeight(fontSize * lh).TextColor(textCol).FillWidth()
			if !readOnly && rt.Clicked() {
				s.annotInlineEditBlock = b.ID
				s.FocusBlock(idx)
			}
		} else {
			interactiveLinks := isHovered && !s.IsScrollingForHover(c.Now()) && blockHasLink(b)
			if interactiveLinks {
				rt = ui.RichText(c.Key("rt")).PreserveSpaces().FontSize(fontSize).FontWeight(weight).
					LineHeight(lh).MinHeight(fontSize * lh).TextColor(textCol).FillWidth()
				rt.Children(func() {
					s.renderDisplaySpansChildren(c, idx, b, fontSize, weight)
				})
			} else {
				rt = ui.RichText(c.Key("rt"), s.buildRenderDisplaySpans(b, fontSize, weight)...).PreserveSpaces().FontSize(fontSize).FontWeight(weight).
					LineHeight(lh).MinHeight(fontSize * lh).TextColor(textCol).FillWidth()
			}
			bands := s.blockHighlightBands(idx, b, pal, true)
			codeRanges := blockCodeRanges(b, fontSize)
			var pathLinks []appUi.RichLinkRange
			cmdActive := c.WindowActive() && c.Modifiers() == ui.Cmd && s.previewMedia == nil
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
					if cmdActive && hasPtr && isHovered && !s.IsScrollingForHover(c.Now()) {
						if pPlain, lay, origin, ok := s.richBlockTextLayout(idx, b); ok {
							if pr, hit := s.hoveredInlinePathRange(b.Spans, pPlain, lay, origin, ptrWin, true); hit {
								pathLinks = []appUi.RichLinkRange{pr}
								pathCursor = true
							}
						}
					}
				}
				appUi.DrawRichDecorations(c, rt, plain, func(w float32) *ui.TextLayout {
					return s.cachedShapeBlock(blockID, plain, spansCopy, idx, edFont, fontSize, weight, lh, w, true)
				}, appUi.RichDecorOptions{
					FontSize:     fontSize,
					Spans:        pureSpans,
					Bands:        bands,
					Links:        pathLinks,
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
					CmdActive:    cmdActive,
				})
			}
		}
		if edFont != "" {
			rt.Font(edFont)
		}
		if pathCursor {
			rt.Cursor(ui.CursorPointer)
		} else if !readOnly {
			rt.Cursor(ui.CursorText)
		}
		s.recordBlockTextLayoutBounds(b.ID, rt)
		rt.DrawOver(func(_ *ui.Painter, r ui.Rect) {
			s.recordBlockTextWinRect(b.ID, r)
		})
	})
}

func spanLinkAtOffset(b *model.Block, off int) string {
	pos := 0
	for _, sp := range b.Spans {
		next := pos + len([]rune(sp.Text))
		if off >= pos && off <= next && sp.Link != "" && !strings.HasPrefix(sp.Link, inlinePathLinkPrefix) {
			return sp.Link
		}
		pos = next
	}
	return ""
}

func (s *Session) renderEditableTextBlock(c *ui.Context, idx int, b *model.Block, fontSize float32, weight int, lh float32, deleteArmed bool) {
	pal := theme.Ocean()
	draft := b.PlainText()
	if dec, _, ok := s.matchBlockDecorator(b); ok {
		draft = decoratorDisplayText(draft, dec)
	}
	st := s.richInputState(b.ID)

	wrap := ui.Box(c.Key("edit-wrap")).FillWidth()
	showEmptyHint := wrap.FocusWithin() || s.focusPending || (idx == 0 && s.isEmptyDocument())
	var hint *emptyHintSpec
	// IME preedit is visible before it becomes part of the block text.
	if showEmptyHint && !st.Composing() {
		hint = s.emptyHintFor(b, deleteArmed)
	}
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
		carryX, carry := s.takeCarryColumn(syncSel)
		multiSel := s.hasMultiBlockSelection()
		var multiSelRange [2]int
		if multiSel {
			if selLo, selHi, _, selOk := s.blockSelectionSpanRange(idx); selOk && selHi > selLo {
				multiSelRange = [2]int{selLo, selHi}
			}
		}

		svcBlock := c.Services()
		focusBlockID := b.ID
		blockLinks := blockLinkRanges(b)
		if c.WindowActive() && c.Modifiers() == ui.Cmd && s.previewMedia == nil && !s.IsScrollingForHover(c.Now()) && !st.Composing() {
			if ptrWin, hasPtr := s.windowPointerStatus(); hasPtr {
				if pPlain, lay, origin, ok := s.richBlockTextLayout(idx, b); ok {
					if pr, hit := s.hoveredInlinePathRange(b.Spans, pPlain, lay, origin, ptrWin, true); hit {
						blockLinks = append(blockLinks, pr)
					}
				}
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
			SuppressSelection: multiSel || s.toolbarPop == "comment",
			SuppressNavKeys:   s.hasMenuPopupOpen(),
			SyncSelection:     syncSel,
			FocusPending:      s.focusPending,
			Anchor:            s.selAnchor.Offset,
			Caret:             s.selHead.Offset,
			Bands:             s.blockHighlightBands(idx, b, pal, multiSel || s.toolbarPop == "comment"),
			Links:             blockLinks,
			CodeRanges:        blockCodeRanges(b, fontSize),
			CodeBG:            pal.InlineCodeBG,
			CodeHoverBG:       pal.InlineCodeHoverBG,
			CodeEditBG:        pal.InlineCodeEditBG,
			SelRange:          multiSelRange,
			OpaqueRanges:      blockOpaqueBGRanges(b),
			HasChipBG:         blockHasChipBG(b),
			OnBounds: func(r ui.Rect) {
				s.recordBlockTextWinRect(b.ID, r)
			},
			OnChange: func(newText string, _, caret int) {
				s.caretHidden = false
				s.reactionPickerAnnotID = ""
				st.SetOnDetachedReplace(s.insertDetachedTextIntoFocusedBlock)
				s.applyBlockTextEditWithEdge(idx, newText, caret, st.CodeEdge())
				if s.doc != nil && s.focusedBlock == idx && idx < len(s.doc.Blocks) {
					after := s.doc.Blocks[idx].PlainText()
					if after != newText || s.postEditCodeEdge != appUi.RichCodeEdgeNone {
						st.SyncWithCodeEdge(after, s.selAnchor.Offset, s.selHead.Offset, s.postEditCodeEdge)
						s.postEditCodeEdge = appUi.RichCodeEdgeNone
						s.selSyncPending = false
					}
				}
			},
			OnText: func(text string, anchor, caret int) bool {
				s.caretHidden = false
				s.reactionPickerAnnotID = ""
				return s.handleBlockTextInput(idx, text, anchor, caret)
			},
			OnSelect: func(anchor, caret int) {
				s.caretHidden = false
				prevOff := -1
				if s.selAnchor.Block == idx && s.selHead.Block == idx && s.selAnchor.Offset == s.selHead.Offset {
					prevOff = s.selHead.Offset
				}
				s.clearAutoConvertState()
				if !s.hasMultiBlockSelection() {
					s.selAnchor = Position{Block: idx, Offset: anchor}
					s.selHead = Position{Block: idx, Offset: caret}
					if prevOff >= 0 && s.doc != nil && idx >= 0 && idx < len(s.doc.Blocks) {
						s.recordRightStyleBias(b.ID, false, tableCellKey{}, s.doc.Blocks[idx].Spans, prevOff, anchor, caret)
					}
				}
			},
			OnCopy: func(svc ui.Services) bool {
				return s.CopySelection(svc)
			},
			OnCut: func(svc ui.Services) bool {
				if s.hasNonEmptySelection() {
					return s.CutSelection(svc)
				}
				return false
			},
			OnPaste: func(svc ui.Services, pasted string) bool {
				return s.PasteFromClipboard(svc, pasted, true)
			},
			OnPastePlain: func(svc ui.Services, pasted string) bool {
				return s.PastePlainFromClipboard(svc, pasted, true)
			},
			OnOpenLink: func(url string) {
				s.openBlockLink(svcBlock, url)
			},
			OnResolveCmdLink: func(pt ui.Point, lay *ui.TextLayout, text string) string {
				if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) || s.doc.Blocks[idx].ID != focusBlockID {
					return ""
				}
				return s.resolveCmdClickPathAtPoint(s.doc.Blocks[idx].Spans, text, lay, pt)
			},
			OnContextMenu: func(m *ui.Menu) {
				s.appendInlinePathContextMenu(m, svcBlock, fmt.Sprintf("block:%d", focusBlockID))
			},
			CarryColumnX: carryX,
			CarryColumn:  carry,
			OnEdge: func(e appUi.RichEdge) bool {
				return s.navigatePastBlock(idx, e)
			},
			OnKey: func(key ui.Key, mods ui.Modifiers, curText string, anchor, caret int) bool {
				s.caretHidden = false
				s.reactionPickerAnnotID = ""
				if s.hasMenuPopupOpen() {
					return false
				}
				if s.navigateDocEdge(key, mods) {
					return true
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
					if s.hasMultipleVisibleBlocks() && (rlen == 0 || (selLo == 0 && selHi >= rlen)) {
						s.SelectAllBlocks()
						return true
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
				case mods == ui.Cmd|ui.Shift && (key == ui.KeyX || key == ui.KeyS):
					s.ToggleMarkOnSelection(model.MarkStrike)
					return true
				case s.isFindShortcut(mods, key):
					if s.OpenMindMapFind() {
						s.requestRedraw()
						return true
					}
					s.ToggleFind()
					return true
				case mods == 0 && key == ui.KeyEnter:
					if s.handleEnterOnBlock(idx, curText, caret) {
						return true
					}
				case mods == 0 && (key == ui.KeyBackspace || key == ui.KeyDelete):
					if s.hasMultiBlockSelection() {
						return s.DeleteSelection()
					}
					if key == ui.KeyBackspace && selLo == selHi {
						if s.tryUndoAutoConvertBackspace(idx, curText, caret) {
							return true
						}
						if s.handleBackspaceOnBlock(idx, curText, caret) {
							return true
						}
					}
				case (mods == 0 || mods == ui.Shift) && key == ui.KeyTab:
					if s.hasMultiBlockSelection() {
						s.indentMultiBlockSelection(mods == ui.Shift)
						return true
					}
					if b.Kind.IsList() {
						if mods == 0 && b.Indent < 8 {
							s.pushUndo()
							b.Indent++
							b.Dirty = true
							b.CleanSource = ""
							s.markDirty()
						} else if mods == ui.Shift && b.Indent > 0 {
							s.pushUndo()
							b.Indent--
							b.Dirty = true
							b.CleanSource = ""
							s.markDirty()
						}
					}
					return true
				}
				return false
			},
		})
		if in.Focused() {
			s.frameTabChildFocused = true
		}

		s.recordBlockTextLayoutBounds(b.ID, in)
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
	padX = min(baseSize*0.2+0.8*scale, 7)
	targetH := float32(math.Round(float64(baseSize + 5*scale)))
	padY = max((targetH-baseSize*1.32)*0.5, 0)
	radius = max(min(float32(math.Round(float64(4*scale))), 5), 3)
	return
}

func isMacSystemUICJKLeakPunct(r rune) bool {
	switch r {
	case '：', '，', '、', '。', '；', '！', '？',
		'）', '】', '》', '」', '』', '〕', '〉', '｝', '］', '”', '’':
		return true
	default:
		return false
	}
}

// stabilizeSystemUICJKPunctBeforeFontChange works around a macOS CoreText bug where
// trailing fullwidth CJK punctuation shaped with system-ui (.AppleSystemUIFont /
// .PingFangUI) right before a different-font span (such as inline code/math) collapses
// to half-width and leaks its missing advance (+3.75~7.5pt) onto the last glyph of the
// following span. Assigning that trailing punctuation slice to PingFang SC keeps the
// punctuation fullwidth and leaves the following code span's glyph advances exact.
func stabilizeSystemUICJKPunctBeforeFontChange(spans []ui.Span, baseFont string) []ui.Span {
	if runtime.GOOS != "darwin" || len(spans) < 2 || !strings.HasPrefix(baseFont, "system-ui") {
		return spans
	}
	var cuts []int
	needBreak := false
	for i := len(spans) - 1; i >= 0; i-- {
		sp := spans[i]
		if sp.Text == "" {
			continue
		}
		if sp.Font != "" {
			needBreak = true
			continue
		}
		if !needBreak {
			continue
		}
		cut := len(sp.Text)
		for cut > 0 {
			r, sz := utf8.DecodeLastRuneInString(sp.Text[:cut])
			if !isMacSystemUICJKLeakPunct(r) {
				break
			}
			cut -= sz
		}
		if cut < len(sp.Text) {
			if cuts == nil {
				cuts = make([]int, len(spans))
				for k := range cuts {
					cuts[k] = -1
				}
			}
			cuts[i] = cut
			needBreak = cut == 0
		} else {
			needBreak = false
		}
	}
	if cuts == nil {
		return spans
	}
	out := make([]ui.Span, 0, len(spans)+2)
	for i, sp := range spans {
		cut := cuts[i]
		if cut < 0 {
			out = append(out, sp)
			continue
		}
		if cut > 0 {
			head := sp
			head.Text = sp.Text[:cut]
			out = append(out, head)
		}
		punct := sp
		punct.Text = sp.Text[cut:]
		punct.Font = "PingFang SC"
		out = append(out, punct)
	}
	return out
}

// buildPureSpan converts a single model.Span into its layout-invariant ui.Span without
// ever splitting spans on selection, find, or annotations (so PaddingX and ligatures never shift).
func buildPureSpan(sp model.Span, baseSize float32, baseWeight int, codeFG, codeBG ui.Color, codePadX, codePadY, codeRadius float32) (ui.Span, bool) {
	pal := theme.Ocean()
	dark := pal.Dark
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
	if sp.Marks&model.MarkSuperscript != 0 {
		u.Size = max(baseSize*0.72, 9)
		u.BaselineShift = baseSize * 0.34
	} else if sp.Marks&model.MarkSubscript != 0 {
		u.Size = max(baseSize*0.72, 9)
		u.BaselineShift = -baseSize * 0.18
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
	} else if sp.Marks&model.MarkMath != 0 {
		u.Font = resolvedCodeFont()
		u.Italic = true
		u.Size = baseSize
		u.Weight = max(u.Weight, theme.WeightRegular)
		u.Color = pal.Accent
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

func blockHasLink(b *model.Block) bool {
	if b == nil {
		return false
	}
	for _, sp := range b.Spans {
		if sp.Link != "" {
			return true
		}
	}
	return false
}

func (s *Session) buildRenderDisplaySpans(b *model.Block, baseSize float32, baseWeight int) []ui.Span {
	if b == nil || len(b.Spans) == 0 {
		return nil
	}
	codeFG, codeBG, codePadX, codePadY, codeRadius := inlineCodeStyle(baseSize)
	out := make([]ui.Span, 0, len(b.Spans))
	for _, sp := range b.Spans {
		u, hasNamedBG := buildPureSpan(sp, baseSize, baseWeight, codeFG, codeBG, codePadX, codePadY, codeRadius)
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 && !hasNamedBG {
			u.Background = ui.Color{}
		}
		out = append(out, u)
	}
	return stabilizeSystemUICJKPunctBeforeFontChange(out, resolvedEditorFont())
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
	return stabilizeSystemUICJKPunctBeforeFontChange(out, resolvedEditorFont())
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
	code    string
	canon   string
	dark    bool
	colors  syntaxColorKey
	langGen uint64
	spans   []ui.Span
	ranges  []ui.TextRange
}

type cachedCodePreview struct {
	code      string
	canon     string
	csvRows   [][]string
	mathText  string
	mathErr   string
	mmdNodes  []string
	mmdLabels map[string]string
	mmdEdges  []codepreview.MermaidEdge
	mmdErr    string
}

func (s *Session) renderDisplaySpansChildren(c *ui.Context, _ int, b *model.Block, baseSize float32, baseWeight int) {
	codeFG, codeBG, codePadX, codePadY, codeRadius := inlineCodeStyle(baseSize)
	raw := make([]ui.Span, 0, len(b.Spans))
	for _, sp := range b.Spans {
		u, hasNamedBG := buildPureSpan(sp, baseSize, baseWeight, codeFG, codeBG, codePadX, codePadY, codeRadius)
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 && !hasNamedBG {
			u.Background = ui.Color{}
		}
		raw = append(raw, u)
	}
	stabilized := stabilizeSystemUICJKPunctBeforeFontChange(raw, resolvedEditorFont())
	spanLinks := make([]string, len(stabilized))
	if len(stabilized) == len(b.Spans) {
		for i, sp := range b.Spans {
			spanLinks[i] = sp.Link
		}
	} else {
		// Map split spans back to their source span's Link by byte progress.
		srcIdx, srcOff := 0, 0
		for i, u := range stabilized {
			for srcIdx < len(b.Spans) && srcOff >= len(b.Spans[srcIdx].Text) {
				srcIdx++
				srcOff = 0
			}
			if srcIdx < len(b.Spans) {
				spanLinks[i] = b.Spans[srcIdx].Link
				srcOff += len(u.Text)
			}
		}
	}
	for i, u := range stabilized {
		el := ui.RichText(c.Key(spanUIKey{idx: i}), u)
		if spanLinks[i] != "" {
			if el.Hovered() {
				el.Underline()
			}
			if s.readOnly || c.Modifiers() == ui.Cmd {
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

func (s *Session) blockHasInlineRewrite(idx int) bool {
	if s.aiDisabled || len(s.annotations) == 0 {
		return false
	}
	for _, ann := range s.annotations {
		if !isAnnotationVisible(ann) {
			continue
		}
		if _, _, ok := s.activeInlineRewrite(ann); !ok {
			continue
		}
		if _, _, ok := s.annotationBlockRange(ann, idx); ok {
			return true
		}
	}
	return false
}

func (s *Session) buildInlineDiffDisplaySpans(idx int, b *model.Block, baseSize float32, baseWeight int) []ui.Span {
	if b == nil || len(b.Spans) == 0 {
		return nil
	}
	pal := theme.Ocean()
	type inlineRW struct {
		lo, hi     int
		text       string
		busy       bool
		insertHere bool
		inserted   bool
	}
	var rewrites []inlineRW
	for _, ann := range s.annotations {
		if !isAnnotationVisible(ann) {
			continue
		}
		rwText, busy, ok := s.activeInlineRewrite(ann)
		if !ok {
			continue
		}
		lo, hi, ok := s.annotationBlockRange(ann, idx)
		if !ok {
			continue
		}
		aIdx := s.blockIndexByID(ann.StartBlockID)
		bIdx := s.blockIndexByID(ann.EndBlockID)
		endIdx := max(aIdx, bIdx)
		rewrites = append(rewrites, inlineRW{
			lo:         lo,
			hi:         hi,
			text:       rwText,
			busy:       busy,
			insertHere: idx == endIdx,
		})
	}

	codeFG, codeBG, codePadX, codePadY, codeRadius := inlineCodeStyle(baseSize)
	spans := s.splitSpansForHighlights(idx, b.ID, b.Spans)
	out := make([]ui.Span, 0, len(spans)+len(rewrites))
	pos := 0
	for _, sp := range spans {
		n := len([]rune(sp.Text))
		s0, s1 := pos, pos+n
		inRewrite := false
		for i := range rewrites {
			if s1 > rewrites[i].lo && s0 < rewrites[i].hi {
				inRewrite = true
				break
			}
		}
		if inRewrite {
			u, _ := buildPureSpan(sp, baseSize, baseWeight, codeFG, codeBG, codePadX, codePadY, codeRadius)
			u.Strikethrough = true
			u.Color = pal.Placeholder
			u.Background = ui.Color{}
			out = append(out, u)
		} else {
			u, _ := s.buildDisplaySpan(idx, b, sp, s0, s1, baseSize, baseWeight, codeFG, codeBG, codePadX, codePadY, codeRadius)
			out = append(out, u)
		}
		pos = s1
		for i := range rewrites {
			if rewrites[i].insertHere && !rewrites[i].inserted && rewrites[i].hi == pos {
				rewrites[i].inserted = true
				fg := pal.SelectionFG
				bg := pal.AnnotationDone
				if rewrites[i].busy {
					fg = pal.TextSecondary
					bg = pal.Hover
				}
				out = append(out, ui.Span{
					Text:       rewrites[i].text,
					Size:       baseSize,
					Weight:     baseWeight,
					Color:      fg,
					Background: bg,
					Radius:     theme.RadiusXS,
				})
			}
		}
	}
	return stabilizeSystemUICJKPunctBeforeFontChange(out, resolvedEditorFont())
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
	return stabilizeSystemUICJKPunctBeforeFontChange(out, resolvedEditorFont())
}

func (s *Session) buildInputTextRanges(idx int, b *model.Block) []ui.TextRange {
	pal := theme.Ocean()
	dark := pal.Dark
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
		} else if sp.Marks&model.MarkMath != 0 {
			tr.Start, tr.End, tr.Color = s0, s1, pal.Accent
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
	i := s.firstFindMatchEndingAfter(idx, start)
	return i < len(s.findMatches) && s.findMatches[i].BlockIdx == idx && s.findMatches[i].StartRune < end
}

// --- Code Block + Syntax Highlight + Preview ---

func (s *Session) renderCodeBlock(c *ui.Context, idx int, b *model.Block, isFocused, isHovered, readOnly bool) {
	pal := theme.Ocean()
	dark := pal.Dark
	id := b.ID
	canon := codepreview.CanonicalLanguage(b.Language)
	if canon == "mindmap" {
		s.renderMindMapCodeBlock(c, idx, b, isFocused, isHovered, readOnly)
		return
	}
	previewable := codepreview.IsPreviewLanguage(canon)
	previewOnly := s.isPreviewOnlyCodeBlock(idx)

	card := ui.Column(c.Key("code-card")).FillWidth().Radius(theme.RadiusSM).
		Background(pal.CodeBlockBG).Padding(s.scale(34), s.scale(12), s.scale(10), s.scale(12)).Gap(s.scale(8))
	_, _, fullCodeSel, codeBlockSelOk := s.blockSelectionSpanRange(idx)
	if (codeBlockSelOk && (s.hasMultiBlockSelection() || (previewOnly && fullCodeSel))) || (isFocused && previewOnly) {
		accent := pal.Accent
		tintFill := codeBlockSelOk && (s.hasMultiBlockSelection() || (previewOnly && fullCodeSel))
		selTint := pal.SelectionBG.Alpha(0.22)
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if r.W > 1 && r.H > 1 {
				if tintFill {
					p.Fill(r, selTint, theme.RadiusSM)
				}
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
					srcHovered := srcBtn.Hovered()
					srcPressed := srcBtn.Pressed()
					srcBG := ui.Transparent
					if srcOpen || srcHovered || srcPressed {
						srcBG = pal.Hover
					}
					appUi.AnimateSurfaceColors(srcBtn, srcBG, ui.Transparent, false)
					appUi.TactileCard(srcBtn, srcHovered, srcPressed, srcOpen, false)
					srcBtn.Children(func() {
						srcDegTarget := float32(0)
						if srcOpen {
							srcDegTarget = 90
						}
						srcDeg := srcBtn.AnimateTo("srcChev", srcDegTarget, appUi.SpringTactile)
						rotWrap := ui.Box(c).Size(theme.IconAction, theme.IconAction).Shrink(0).Center()
						if srcDeg != 0 {
							rotWrap.Rotate(srcDeg)
						}
						rotWrap.Children(func() {
							icons.DrawAnimated(c, icons.ChevronRight, theme.IconAction, pal.HoverIconColors(srcHovered || srcPressed), srcHovered || srcPressed)
						})
						ui.Text(c, theme.Tr("源码", "Source")).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
					})
					appUi.Probe(srcBtn, fmt.Sprintf("cb-source-toggle-%d", id))
					if srcBtn.Clicked() {
						b.ShowSource = !b.ShowSource
						if b.ShowSource && !readOnly {
							endOff := len([]rune(b.Code))
							s.focusedBlock = idx
							s.selAnchor = Position{Block: idx, Offset: endOff}
							s.selHead = s.selAnchor
							s.multiSelFocusPending = false
							s.focusPending = true
							s.selSyncPending = true
						} else if !b.ShowSource && s.focusedBlock == idx && s.needsDetachedSelectionFocus() {
							s.multiSelFocusPending = true
							s.focusPending = false
						}
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
						c.Services().WriteClipboard(b.Code)
						s.codeCopiedBlock = id
					}

					codeCols := pal.CodeBlock()
					pillFG := codeCols.PillFG
					pillHoverFG := codeCols.PillHoverFG
					pillBG := codeCols.PillBG
					pillHoverBG := codeCols.PillHoverBG
					langID := fmt.Sprintf("code-lang-%d", id)
					langBtn := ui.ButtonBase(c.Key(langID)).Height(theme.HeightMicro).PaddingX(theme.Space8).
						Radius(theme.RadiusSM).Center().
						Cursor(ui.CursorPointer).Label(languageDisplayLabel(b.Language))
					langHovered := langBtn.Hovered()
					langPressed := langBtn.Pressed()
					langActive := s.codeLangMenuBlock == id
					langBG := pillBG
					langFG := pillFG
					if langHovered || langPressed || langActive {
						langBG = pillHoverBG
						langFG = pillHoverFG
					}
					appUi.AnimateSurfaceColors(langBtn, langBG, pal.MenuSeparator, true)
					appUi.TactileCard(langBtn, langHovered, langPressed, langActive, false)
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
					if len(pasted) > media.MaxPasteBytes {
						return true
					}
					if pasted == "" {
						return s.PasteFromClipboard(svc, "", true)
					}
					return false
				})
				s.recordBlockTextLayoutBounds(id, ta)
				ta.DrawOver(func(_ *ui.Painter, r ui.Rect) {
					s.recordBlockTextWinRect(id, r)
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
				isFullCodeBlockSelected := func(textLen int) bool {
					if textLen <= 0 {
						return true
					}
					a, bSel := s.orderedSelection()
					return a.Block == idx && bSel.Block == idx && a.Offset == 0 && bSel.Offset >= textLen
				}
				ta.ObserveInput(func(ev ui.InputEvent) {
					if ev.Kind != ui.InputCommand {
						return
					}
					switch ev.Text {
					case "selectAll":
						if s.hasMultipleVisibleBlocks() && isFullCodeBlockSelected(len([]rune(s.doc.Blocks[idx].Code))) {
							s.SelectAllBlocks()
						}
					case "undo", "redo":
						// Intercept native Edit -> Undo/Redo so macOS menu commands route
						// through Session's document undo stack instead of TextAreaBase's local stack.
						s.codeUndoCmd = ev.Text
					}
				})
				ta.OnTextKey(func(k ui.TextKey) bool {
					if k.Key == ui.KeyTab && (k.Mods == 0 || k.Mods == ui.Shift) {
						if !s.readOnly {
							cur := k.Text()
							selA := len([]rune(cur))
							selB := selA
							if s.selAnchor.Block == idx && s.selHead.Block == idx {
								selA, selB = s.selAnchor.Offset, s.selHead.Offset
							}
							nextText, nextA, nextB := applyCodeBlockTab(cur, selA, selB, k.Mods == ui.Shift)
							if nextText != cur {
								k.SetText(nextText)
								s.applyBlockTextEdit(idx, nextText, nextB)
								s.selAnchor = Position{Block: idx, Offset: nextA}
								s.selHead = Position{Block: idx, Offset: nextB}
								s.selSyncPending = true
							}
						}
						return true
					}
					if s.navigateFromCodeKey(idx, k.Key, k.Mods, k.Text()) {
						return true
					}
					if k.Mods == ui.Cmd && k.Key == ui.KeyA {
						if s.hasMultipleVisibleBlocks() && isFullCodeBlockSelected(len([]rune(k.Text()))) {
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
						if k.Key == ui.KeyBackspace {
							if s.tryUndoCodeAutoConvertBackspace(idx, k.Text()) {
								return true
							}
							if k.Text() == "" {
								return s.handleBackspaceOnBlock(idx, "", 0)
							}
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
				if cmd := s.codeUndoCmd; cmd != "" {
					s.codeUndoCmd = ""
					if cmd == "undo" {
						s.Undo()
					} else {
						s.Redo()
					}
					s.scheduleRebuild(c)
				} else if ta.Changed() {
					_, selEnd := ta.TextSelection()
					s.applyBlockTextEdit(idx, draft, selEnd)
					_ = oldCode
				} else if !s.selSyncPending && !taDragging && ta.Focused() && s.selAnchor.Block == s.selHead.Block {
					selStart, selEnd := ta.TextSelection()
					s.selAnchor = Position{Block: idx, Offset: selStart}
					s.selHead = Position{Block: idx, Offset: selEnd}
				}
				if ta.Focused() {
					s.frameTabChildFocused = true
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
				s.recordBlockTextLayoutBounds(id, rt)
				rt.DrawOver(func(_ *ui.Painter, r ui.Rect) {
					s.recordBlockTextWinRect(id, r)
				})
			}
		}
	})
}

func (s *Session) cachedCodeSyntaxFor(id model.BlockID, code, canon string, dark bool) ([]ui.Span, []ui.TextRange) {
	if s.codeSyntaxCache == nil {
		s.codeSyntaxCache = make(map[model.BlockID]cachedCodeSyntax)
	}
	// Resolve colors once and key the cache on them, so a skin swap or recolor
	// between reads can never pin stale highlight colors.
	synPal := syntaxPalette(dark)
	colors := syntaxColorKey{syn: synPal.Syntax(), add: synPal.DiffAddFG, del: synPal.DiffDelFG}
	langGen := plugins.LanguageGeneration()
	if entry, ok := s.codeSyntaxCache[id]; ok && entry.code == code && entry.canon == canon && entry.dark == dark && entry.colors == colors && entry.langGen == langGen {
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
				col := colors.color(tk.Kind)
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
		code:    code,
		canon:   canon,
		dark:    dark,
		colors:  colors,
		langGen: langGen,
		spans:   spans,
		ranges:  ranges,
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
		entry.csvRows = codepreview.ParseCSV(src)
	case "latex":
		entry.mathText, entry.mathErr = codepreview.FormatMath(src)
	case "mermaid":
		entry.mmdNodes, entry.mmdLabels, entry.mmdEdges, entry.mmdErr = codepreview.ParseMermaid(src)
	}
	s.codePreviewCache[id] = entry
	return entry
}

func (s *Session) renderCodePreview(c *ui.Context, id model.BlockID, canon, src string) {
	pal := theme.Ocean()
	f14 := s.scale(14)
	prev := s.cachedCodePreviewFor(id, canon, src)
	if canon == "csv" && len(prev.csvRows) > 0 {
		grid := appUi.TableGrid(c.Key("code-preview"), pal.Table().Border).FillWidth()
		if appUi.IsProbeEnabled() {
			appUi.Probe(grid, fmt.Sprintf("code-preview-%d", id))
		}
		renderPlainTableRows(c, grid, prev.csvRows, f14, s.zoom(), func(cell string) string { return cell })
		return
	}

	box := ui.Column(c.Key("code-preview")).FillWidth().
		Padding(s.scale(12), s.scale(14)).Radius(theme.RadiusSM).Background(pal.Paper).Border(1, pal.Hairline).Gap(s.scale(8))
	if appUi.IsProbeEnabled() {
		appUi.Probe(box, fmt.Sprintf("code-preview-%d", id))
	}

	box.Children(func() {
		switch canon {
		case "csv":
			ui.Text(c, theme.Tr("CSV 内容为空", "Empty CSV")).FontSize(f14).TextColor(pal.TextSecondary)
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
	ui.Box(c).Padding(6, 12).Radius(theme.RadiusSM).Background(pal.Selected).Border(1, ui.Transparent).Children(func() {
		ui.Text(c, label).FontSize(fontSize).FontWeight(theme.WeightControl).TextColor(pal.Text)
	})
}

// syntaxColorKey is the resolved highlight color set; it doubles as a cache key.
type syntaxColorKey struct {
	syn      theme.SyntaxColors
	add, del ui.Color
}

func (k syntaxColorKey) color(kind syntaxTokenKind) ui.Color {
	syn := k.syn
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
	case tokInserted:
		return k.add
	case tokDeleted:
		return k.del
	default:
		return syn.Default
	}
}

// syntaxPalette returns the current palette, falling back to the stock palette
// when the requested darkness differs from the active surface.
func syntaxPalette(dark bool) theme.Palette {
	pal := theme.Ocean()
	if dark != pal.Dark {
		if dark {
			pal = theme.OceanDark
		} else {
			pal = theme.OceanLight
		}
	}
	return pal
}

func syntaxTokenColor(kind syntaxTokenKind, dark bool) ui.Color {
	pal := syntaxPalette(dark)
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
	case tokInserted:
		return pal.DiffAddFG
	case tokDeleted:
		return pal.DiffDelFG
	default:
		return syn.Default
	}
}

func codeSyntaxRanges(src, lang string) []ui.TextRange {
	toks := tokenizeCode(src, lang)
	if len(toks) == 0 {
		return nil
	}
	dark := theme.Ocean().Dark
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
	dark := theme.Ocean().Dark
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
	tableCellFontSize   float32 = 14
	tableCellLineHeight float32 = 21
	tableMinRowHeight   float32 = 34.8
	tableMinColWidth    float32 = 80
	tableMaxColWidth    float32 = 381.5
)

func (s *Session) renderTableBlock(c *ui.Context, idx int, b *model.Block, isFocused, isHovered, readOnly bool) {
	pal := theme.Ocean()
	id := b.ID
	if b.Table == nil || len(b.Table.Rows) == 0 || len(b.Table.Rows[0]) == 0 {
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
	showChrome := !readOnly && (isHovered || isFocused || s.tableMenuBlock == id || s.isTableExtendingBlock(id) || s.tableResizeBlock == id || s.tableDragBlock == id)
	wrap.Children(func() {
		gridRow := ui.Row(c.Key("table-grid-row")).FillWidth().AlignItems(ui.Stretch)
		gridRow.Children(func() {
			indentPx := float32(min(max(b.Indent, 0), 12)) * s.indentStep()
			availGrid := max(s.standardWidth()-indentPx, s.scale(240)) + 2*bleed
			if s.viewportWidth > 0 {
				availGrid = min(availGrid, max(s.viewportWidth-s.leftContentGutter()-s.rightContentInset()-indentPx+2*bleed, 0))
			}
			colWidths, needHScroll := s.resolveTableColWidths(id, tbl, availGrid)
			var totalHScrollW float32
			for _, cw := range colWidths {
				totalHScrollW += cw
			}
			totalHScrollW += float32(cols+1) * appUi.TableGridRuleWidth

			buildGrid := func() {
				grid := appUi.TableGrid(c.Key("table-grid"), borderCol)
				if needHScroll {
					grid.Width(max(totalHScrollW, float32(cols)*tMinColW)).Shrink(0)
				} else {
					grid.FillWidth()
				}
				grid.DrawOver(func(p *ui.Painter, gr ui.Rect) {
					docY := gr.Y - s.scrollRect.Y + s.bodyScroll.Y
					prev, ok := s.tableGridGeoms[id]
					if !ok || math.Abs(float64(prev.docY-docY)) > 0.5 || math.Abs(float64(prev.h-gr.H)) > 0.5 {
						if s.tableGridGeoms == nil {
							s.tableGridGeoms = make(map[model.BlockID]tableGridGeom)
						}
						wasZero := !ok || prev.h <= 0
						s.tableGridGeoms[id] = tableGridGeom{docY: docY, h: gr.H}
						if wasZero && showChrome {
							c.Invalidate()
						}
					}
					s.paintTableSelectionFrames(p, gr, id, tbl, colWidths, pal, tblCols)
				})
				grid.Children(func() {
					for rIdx := range tbl.Rows {
						row := tbl.Rows[rIdx]
						rowEl := appUi.TableGridRow(c.Key(tableRowUIKey{row: rIdx})).MinHeight(tMinRowH)
						rowEl.Children(func() {
							for cIdx, colW := range colWidths {
								if cIdx < len(row) {
									s.renderTableCell(c, idx, id, tbl, rIdx, cIdx, colW, headerBG, needHScroll, readOnly, gridRow, grid)
								} else {
									tableGridCell(c, rIdx, cIdx, colW, needHScroll).Background(pal.Paper)
								}
							}
						})
					}
				})
			}
			if needHScroll {
				hs := appUi.ScrollHorizontal(c.Key("table-hscroll"), s.tableHScroll(id)).Grow(1).Shrink(1).MinWidth(0)
				hs.Children(func() {
					buildGrid()
				})
			} else {
				buildGrid()
			}

			if showChrome {
				s.renderTableBorderChrome(c, idx, id, tbl, colWidths, availGrid, needHScroll, isFocused)
			}

			// Right add column bar sits in the right gutter so table width aligns 1:1 with other blocks.
			if showChrome {
				colSlot := ui.Box(c.Key("table-add-col-slot")).Absolute()
				slotInset := float32(0)
				if s.minimal {
					slotInset = s.scale(2)
					colSlot.Top(slotInset).Bottom(slotInset).Right(s.scale(2)).Width(s.scale(20))
				} else {
					colSlot.Top(0).Bottom(0).Right(-s.scale(26)).Width(s.scale(22))
				}
				colSlot.Children(func() {
					iconSz := s.scale(theme.IconAction)
					iconTop, stickyOK := s.tableAddColIconTop(id, slotInset, iconSz)
					isColDrag := s.isTableExtendingAxis(id, tableExtendCol)
					colCursor := ui.CursorPointer
					if isColDrag {
						colCursor = ui.CursorResizeColumn
					}
					addCol := ui.ButtonBase(c.Key("table-add-col")).Fill().
						Radius(theme.RadiusXS).Cursor(colCursor).
						Label(theme.Tr("添加列", "Add column"))
					if !stickyOK {
						addCol.Center()
					}
					addColActive := addCol.Hovered() || addCol.Pressed() || isColDrag
					addColBG := pal.CodeBlockBG
					if addColActive {
						addColBG = pal.Hover
					}
					appUi.AnimateSurfaceColors(addCol, addColBG, ui.Transparent, false)
					appUi.TactileCard(addCol, addCol.Hovered(), addCol.Pressed() && !isColDrag, false, isColDrag)
					addCol.Children(func() {
						if stickyOK {
							ui.Box(c.Key("table-add-col-icon")).Absolute().Left(0).Right(0).Top(iconTop).Height(iconSz).
								Center().IgnoreInput(true).Children(func() {
								icons.DrawAnimated(c, icons.Plus, iconSz, pal.HoverIconColors(addColActive), addColActive)
							})
						} else {
							icons.DrawAnimated(c, icons.Plus, iconSz, pal.HoverIconColors(addColActive), addColActive)
						}
					})
					appUi.Probe(addCol, "table-add-col")
					s.handleTableExtendGesture(c, addCol, idx, id, tableExtendCol, colWidths)
				})
			}
		})

		// Bottom add row bar (reserve 20px height + 4px gap = 24px EXTEND_ROOM so blocks below never jump)
		rowSlot := ui.Box(c.Key("table-add-row-slot")).FillWidth().Height(s.scale(20))
		if showChrome {
			rowSlot.Children(func() {
				isRowDrag := s.isTableExtendingAxis(id, tableExtendRow)
				rowCursor := ui.CursorPointer
				if isRowDrag {
					rowCursor = ui.CursorResizeRow
				}
				addRow := ui.ButtonBase(c.Key("table-add-row")).Fill().Center().
					Radius(theme.RadiusXS).Cursor(rowCursor).
					Label(theme.Tr("添加行", "Add row"))
				addRowActive := addRow.Hovered() || addRow.Pressed() || isRowDrag
				addRowBG := pal.CodeBlockBG
				if addRowActive {
					addRowBG = pal.Hover
				}
				appUi.AnimateSurfaceColors(addRow, addRowBG, ui.Transparent, false)
				appUi.TactileCard(addRow, addRow.Hovered(), addRow.Pressed() && !isRowDrag, false, isRowDrag)
				addRow.Children(func() {
					icons.DrawAnimated(c, icons.Plus, s.scale(theme.IconAction), pal.HoverIconColors(addRowActive), addRowActive)
				})
				appUi.Probe(addRow, "table-add-row")
				s.handleTableExtendGesture(c, addRow, idx, id, tableExtendRow, nil)
			})
		}
	})
}

func (s *Session) renderTableCell(c *ui.Context, blockIdx int, blockID model.BlockID, tbl *model.TableData, rIdx, cIdx int, colW float32, headerBG ui.Color, needHScroll, readOnly bool, gridRowElem, gridElem ui.Element) {
	pal := theme.Ocean()
	tFontSz, tLineH, _, _, _, tPadY, tPadX := s.tableMetrics()
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

	jumpTarget := s.jump.block == blockID && s.jump.row == rIdx && s.jump.col == cIdx
	box := tableGridCell(c, rIdx, cIdx, colW, needHScroll).Padding(tPadY, tPadX)
	s.recordTableCellLayoutBounds(blockID, rIdx, cIdx, box, gridRowElem, gridElem, needHScroll)
	box.DrawOver(func(p *ui.Painter, r ui.Rect) {
		s.recordTableCellWinRect(blockID, rIdx, cIdx, r, needHScroll)
		if jumpTarget {
			p.Stroke(r, pal.Editor().JumpRing.Alpha(s.jumpAlpha), theme.RadiusXS, theme.JumpHighlightBorderW)
			appUi.RecordBounds("search-target-cell", r)
		}
	})
	if !readOnly {
		box.Cursor(ui.CursorText)
	}
	_ = headerBG
	bg := s.resolveTableCellFill(blockID, tbl, rIdx, cIdx, pal, pal.Table(), isCellFocused, cellSelected)
	if jumpTarget {
		bg = pal.EditorSelectionBG().Alpha(s.jumpAlpha).Over(bg)
	}
	box.Background(bg)
	weight := tableCellTextWeight(tbl, rIdx, cIdx)
	textCol := pal.Text
	if tbl.IsHeaderCell(rIdx, cIdx) {
		textCol = pal.TextSecondary
	}
	lhFactor := tableCellLineHeight / tableCellFontSize
	edFont := resolvedEditorFont()
	cellAlign := tableCellTextAlign(tbl, rIdx, cIdx)

	box.Children(func() {
		if jumpTarget {
			s.revealJump(c, 0)
		}
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
			carryX, carry := s.takeCarryColumn(syncSel)
			multiSel := s.hasMultiBlockSelection() || s.hasMultiCellTableSelection()
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
			if jumpTarget && s.jump.endRune > s.jump.startRune && s.jump.startRune >= 0 && cellLen > 0 {
				jLo := clampInt(s.jump.startRune, 0, cellLen)
				jHi := clampInt(s.jump.endRune, 0, cellLen)
				if jHi > jLo {
					cellBands = append(cellBands, appUi.RichBand{
						Start:    jLo,
						End:      jHi,
						Fill:     pal.Editor().JumpRing.Alpha(s.jumpAlpha * 0.45),
						Radius:   theme.RadiusXS,
						PadX:     1.5,
						TintOver: true,
					})
				}
			}
			svcCell := c.Services()
			cellLinks := blockLinkRanges(&cellBlock)
			if c.WindowActive() && c.Modifiers() == ui.Cmd && s.previewMedia == nil && !s.IsScrollingForHover(c.Now()) && !st.Composing() {
				if ptrWin, hasPtr := s.windowPointerStatus(); hasPtr {
					if s.doc != nil && blockIdx >= 0 && blockIdx < len(s.doc.Blocks) {
						if _, cPlain, lay, origin, ok := s.tableCellTextLayout(&s.doc.Blocks[blockIdx], rIdx, cIdx); ok {
							if pr, hit := s.hoveredInlinePathRange(cellBlock.Spans, cPlain, lay, origin, ptrWin, true); hit {
								cellLinks = append(cellLinks, pr)
							}
						}
					}
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
				Links:             cellLinks,
				CodeRanges:        blockCodeRanges(&cellBlock, tFontSz),
				CodeBG:            pal.InlineCodeBG,
				CodeHoverBG:       pal.InlineCodeHoverBG,
				CodeEditBG:        pal.InlineCodeEditBG,
				SelRange:          cellSelRange,
				OpaqueRanges:      blockOpaqueBGRanges(&cellBlock),
				HasChipBG:         blockHasChipBG(&cellBlock),
				CarryColumnX:      carryX,
				CarryColumn:       carry,
				OnEdge: func(e appUi.RichEdge) bool {
					return s.navigateFromTableCell(blockIdx, rIdx, cIdx, e)
				},
				OnChange: func(newText string, anchor, caret int) {
					s.caretHidden = false
					s.applyTableCellTextEdit(blockIdx, rIdx, cIdx, newText, anchor, caret, st.CodeEdge())
				},
				OnText: func(text string, anchor, caret int) bool {
					s.caretHidden = false
					return s.handleTableCellTextInput(blockIdx, rIdx, cIdx, text, anchor, caret)
				},
				OnSelect: func(anchor, caret int) {
					s.caretHidden = false
					curBase := tableCellStartOffset(tbl, rIdx, cIdx)
					prevOff := -1
					if s.selAnchor.Block == blockIdx && s.selHead.Block == blockIdx && s.selAnchor.Offset == s.selHead.Offset {
						prevOff = s.selHead.Offset - curBase
					}
					s.clearAutoConvertState()
					// 跨格拖选期间由 updateFocusedRichTableCellDrag 接管选区，这里再回写单格选区会与之逐帧互相覆盖导致闪烁。
					crossCellDrag := s.isMouseDragging() && s.mouseOriginBlock == blockIdx && s.hasMultiCellTableSelection()
					if !s.hasMultiBlockSelection() && !crossCellDrag {
						s.selAnchor = Position{Block: blockIdx, Offset: curBase + anchor}
						s.selHead = Position{Block: blockIdx, Offset: curBase + caret}
						if prevOff >= 0 {
							s.recordRightStyleBias(blockID, true, tableCellKey{BlockID: blockID, Row: rIdx, Col: cIdx}, cell.Spans, prevOff, anchor, caret)
						}
					}
				},
				OnCopy: func(svc ui.Services) bool {
					return s.CopySelection(svc)
				},
				OnCut: func(svc ui.Services) bool {
					if s.hasNonEmptySelection() {
						return s.CutSelection(svc)
					}
					return false
				},
				OnPaste: func(svc ui.Services, pasted string) bool {
					return s.PasteFromClipboard(svc, pasted, true)
				},
				OnPastePlain: func(svc ui.Services, pasted string) bool {
					return s.PastePlainFromClipboard(svc, pasted, true)
				},
				OnOpenLink: func(url string) {
					s.openBlockLink(svcCell, url)
				},
				OnResolveCmdLink: func(pt ui.Point, lay *ui.TextLayout, text string) string {
					if s.doc == nil || blockIdx < 0 || blockIdx >= len(s.doc.Blocks) || s.doc.Blocks[blockIdx].ID != blockID {
						return ""
					}
					tblNow := s.doc.Blocks[blockIdx].Table
					if tblNow == nil || rIdx < 0 || rIdx >= len(tblNow.Rows) || cIdx < 0 || cIdx >= len(tblNow.Rows[rIdx]) {
						return ""
					}
					return s.resolveCmdClickPathAtPoint(tblNow.Rows[rIdx][cIdx].Spans, text, lay, pt)
				},
				OnContextMenu: func(m *ui.Menu) {
					s.appendInlinePathContextMenu(m, svcCell, fmt.Sprintf("cell:%d:%d:%d", blockID, rIdx, cIdx))
				},
				OnKey: func(key ui.Key, mods ui.Modifiers, curCellText string, cAnchor, cCaret int) bool {
					s.caretHidden = false
					if s.hasMenuPopupOpen() {
						return false
					}
					if s.navigateDocEdge(key, mods) {
						return true
					}
					if mods == ui.Cmd && key == ui.KeyA {
						cLo, cHi := min(cAnchor, cCaret), max(cAnchor, cCaret)
						rlen := len([]rune(curCellText))
						if rlen == 0 || (cLo == 0 && cHi >= rlen) {
							tblLen := s.blockSelectionLen(blockIdx)
							if tblLen > 0 {
								s.tableActiveBlock = 0
								s.SelectRange(blockIdx, 0, blockIdx, tblLen)
								return true
							}
							if s.hasMultipleVisibleBlocks() {
								s.SelectAllBlocks()
								return true
							}
						}
					}
					if key == ui.KeyBackspace && mods == 0 && cAnchor == cCaret {
						if s.tryUndoCellAutoConvertBackspace(blockIdx, rIdx, cIdx, curCellText, cCaret) {
							return true
						}
					}
					if (mods == 0 || mods == ui.Shift) && key == ui.KeyTab {
						if mods == ui.Shift {
							s.stepTableCell(blockID, tbl, rIdx, cIdx, -1)
						} else {
							s.stepTableCell(blockID, tbl, rIdx, cIdx, 1)
						}
						return true
					}
					if key == ui.KeyEnter && mods == 0 {
						if cCaret > 0 {
							if res := tryBareURLAutolinkBefore(cell.Spans, []rune(curCellText), cCaret); res.changed {
								s.pushUndo()
								cell.Spans = res.spans
								s.doc.Blocks[blockIdx].Dirty = true
								s.doc.Blocks[blockIdx].CleanSource = ""
								s.markDirty()
							}
						}
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
					if mods == ui.Cmd && key == ui.KeyU {
						s.ToggleMarkOnSelection(model.MarkUnderline)
						return true
					}
					if mods == ui.Cmd && key == ui.KeyE {
						s.ToggleMarkOnSelection(model.MarkCode)
						return true
					}
					if mods == ui.Cmd|ui.Shift && (key == ui.KeyX || key == ui.KeyS) {
						s.ToggleMarkOnSelection(model.MarkStrike)
						return true
					}
					if mods == ui.Cmd|ui.Shift && key == ui.KeyH {
						s.toggleHighlightOnSelection()
						return true
					}
					if mods == ui.Cmd|ui.Shift && key == ui.KeyBackslash {
						s.ClearFormattingOnSelection()
						return true
					}
					return false
				},
			})
			if in.Focused() {
				s.frameTabChildFocused = true
			}
			_ = s.updateFocusedRichTableCellDrag(c, s.frameRoot, st, blockIdx, blockID, rIdx, cIdx)
			if s.focusPending {
				in.Focus(ui.FocusOptions{PreventScroll: s.focusPreventScroll})
				s.focusPending = false
				s.focusPreventScroll = false
			}
		} else {
			txt := cell.PlainText()
			var t ui.Element
			var cellPathCursor bool
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
						if _, hasNamedBG := theme.ResolveNamedBG(sp.BGColor, dark); sp.Marks&(model.MarkCode|model.MarkMath) != 0 && !hasNamedBG && i < len(renderSpans) {
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
				if jumpTarget && s.jump.endRune > s.jump.startRune && s.jump.startRune >= 0 && cellLen > 0 {
					jLo := clampInt(s.jump.startRune, 0, cellLen)
					jHi := clampInt(s.jump.endRune, 0, cellLen)
					if jHi > jLo {
						cellBands = append(cellBands, appUi.RichBand{
							Start:    jLo,
							End:      jHi,
							Fill:     pal.Editor().JumpRing.Alpha(s.jumpAlpha * 0.45),
							Radius:   theme.RadiusXS,
							PadX:     1.5,
							TintOver: true,
						})
					}
				}
				var cellPathLinks []appUi.RichLinkRange
				cmdActive := c.WindowActive() && c.Modifiers() == ui.Cmd && s.previewMedia == nil
				if len(cellBands) > 0 || len(codeRanges) > 0 {
					spansCopy := pureSpans
					cellKey := tableCellKey{BlockID: blockID, Row: rIdx, Col: cIdx}
					cellPlain := txt
					var ptrWin ui.Point
					var hasPtr bool
					if len(codeRanges) > 0 {
						ptrWin, hasPtr = s.windowPointerStatus()
						if cmdActive && hasPtr && !s.IsScrollingForHover(c.Now()) && s.doc != nil && blockIdx >= 0 && blockIdx < len(s.doc.Blocks) {
							if cr, ok := s.tableCellRect(blockID, rIdx, cIdx); ok && cr.Contains(ptrWin.X, ptrWin.Y) {
								if _, cPlain, lay, origin, ok := s.tableCellTextLayout(&s.doc.Blocks[blockIdx], rIdx, cIdx); ok {
									if pr, hit := s.hoveredInlinePathRange(cellBlock.Spans, cPlain, lay, origin, ptrWin, true); hit {
										cellPathLinks = []appUi.RichLinkRange{pr}
										cellPathCursor = true
									}
								}
							}
						}
					}
					appUi.DrawRichDecorations(c, t, txt, func(w float32) *ui.TextLayout {
						return s.cachedShapeTableCell(cellKey, cellPlain, spansCopy, edFont, tFontSz, weight, lhFactor, cellAlign, w)
					}, appUi.RichDecorOptions{
						FontSize:     tFontSz,
						Spans:        spansCopy,
						Bands:        cellBands,
						Links:        cellPathLinks,
						CodeRanges:   codeRanges,
						CodeBG:       pal.InlineCodeBG,
						CodeHoverBG:  pal.InlineCodeHoverBG,
						CodeEditBG:   pal.InlineCodeEditBG,
						SelRange:     cellSelRange,
						OpaqueRanges: blockOpaqueBGRanges(&cellBlock),
						LinkColor:    pal.Link,
						CaretPos:     -1,
						HasChipBG:    blockHasChipBG(&cellBlock),
						PointerWin:   ptrWin,
						HasPointer:   hasPtr,
						CmdActive:    cmdActive,
					})
				}
			}
			if edFont != "" {
				t.Font(edFont)
			}
			if cellPathCursor {
				t.Cursor(ui.CursorPointer)
			} else if !readOnly {
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

const maxSessionCachedImages = 48

func (s *Session) storeCachedImageLocked(src string, decoded ui.ImageSource) {
	const maxPixels = 32_000_000
	if s.imageCache == nil {
		s.imageCache = make(map[string]ui.ImageSource)
	}
	w, h := ui.ImageSize(decoded)
	pixels := int(w * h)
	for _, img := range s.imageCache {
		w, h := ui.ImageSize(img)
		pixels += int(w * h)
	}
	for len(s.imageOrder) > 0 && (len(s.imageOrder) >= maxSessionCachedImages || pixels > maxPixels) {
		evict := s.imageOrder[0]
		s.imageOrder = s.imageOrder[1:]
		if img, ok := s.imageCache[evict]; ok {
			w, h := ui.ImageSize(img)
			pixels -= int(w * h)
			delete(s.imageCache, evict)
		}
	}
	s.imageOrder = append(s.imageOrder, src)
	s.imageCache[src] = decoded
}

func (s *Session) resolveImageSource(src string) (ui.ImageSource, bool) {
	dir := s.mediaDir()
	if _, err := media.RelativePath(dir, src); err != nil {
		return nil, false
	}
	s.imageMu.Lock()
	if cached, ok := s.imageCache[src]; ok {
		s.imageMu.Unlock()
		return cached, true
	}
	if s.imageFailed[src] {
		s.imageMu.Unlock()
		return nil, false
	}
	if s.host != nil && !testing.Testing() && !appUi.IsProbeEnabled() {
		if s.imageLoading[src] || len(s.imageLoading) >= 2 {
			s.imageMu.Unlock()
			return nil, false
		}
		s.imageLoading[src] = true
		epoch := s.imageEpoch
		s.imageMu.Unlock()
		go func() {
			decoded, err := loadLocalImage(dir, src)
			s.imageMu.Lock()
			if s.imageEpoch != epoch {
				s.imageMu.Unlock()
				return
			}
			delete(s.imageLoading, src)
			if err != nil {
				s.imageFailed[src] = true
			} else {
				s.storeCachedImageLocked(src, decoded)
			}
			s.imageMu.Unlock()
			s.requestRedraw()
		}()
		return nil, false
	}
	s.imageMu.Unlock()

	decoded, err := loadLocalImage(dir, src)
	s.imageMu.Lock()
	defer s.imageMu.Unlock()
	if err != nil {
		s.imageFailed[src] = true
		return nil, false
	}
	s.storeCachedImageLocked(src, decoded)
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

	_, _, _, imgSelected := s.blockSelectionSpanRange(idx)
	showResizeHandles := !readOnly && ((isFocused && !s.hasMultiBlockSelection()) || dragging)
	borderHighlighted := isFocused || dragging || imgSelected
	borderW := s.scale(theme.ImageSelectBorderWidth)
	picBorder := pal.Hairline
	if borderHighlighted {
		picBorder = pal.Accent
	}
	isVideo := IsSupportedVideoPath(b.ImageSrc)
	if s.previewMedia != nil {
		isVideo = IsPreviewVideo(b.ImageSrc) || strings.Contains(strings.ToLower(b.CleanSource), "<video ")
	}
	var imgSrc ui.ImageSource
	var animation *ui.VideoPlayer
	hasImage := false
	if !isVideo {
		if s.previewMedia != nil {
			asset := s.previewMedia(b.ImageSrc, false, true)
			imgSrc, animation = asset.Image, asset.Animation
			hasImage = imgSrc != nil || animation != nil
		} else {
			imgSrc, hasImage = s.resolveImageSource(b.ImageSrc)
		}
	}
	previewExpandable := s.previewMedia != nil && !isVideo && hasImage && s.onPreviewMediaOpen != nil
	docExpandable := s.previewMedia == nil && !isVideo && hasImage
	picCursor := ui.CursorDefault
	if previewExpandable || (docExpandable && readOnly) {
		picCursor = ui.CursorZoomIn
	} else if !readOnly && (isVideo || !hasImage) {
		picCursor = ui.CursorPointer
	}

	outer := ui.Column(c.Key("img-outer")).FillWidth().AlignItems(align)
	outer.Children(func() {
		frameWrap := ui.Box(c.Key(fmt.Sprintf("img-wrap-%d", id)))
		frameWrap.Children(func() {
			picRadius := theme.RadiusSM
			if s.embedded && readOnly && !isVideo && hasImage && !borderHighlighted {
				picBorder = ui.Transparent
				picRadius = 0
			}
			picBox := ui.Box(c.Key(fmt.Sprintf("img-frame-%d", id))).FillWidth().Radius(picRadius).Clip().
				Cursor(picCursor).Border(1, picBorder)
			picHovered := false
			previewBadgeClicked := false
			docBadgeClicked := false
			if previewExpandable || (docExpandable && readOnly) {
				picHovered = picBox.Hovered()
			} else if !readOnly {
				picHovered = picBox.Hovered()
				if picBox.Clicked() {
					mods := picBox.ClickModifiers()
					s.pressJustBegan = false
					s.firstLineDeleteArmed = false
					s.closeTransientOverlays()
					if mods&ui.Shift != 0 {
						s.selAnchor = s.clampPos(s.selAnchor)
						s.selHead = Position{Block: idx, Offset: s.blockSelectionLen(idx)}
					} else {
						s.selAnchor = Position{Block: idx, Offset: 0}
						s.selHead = s.selAnchor
					}
					s.focusedBlock = idx
					isFocused = true
					_, _, _, imgSelected = s.blockSelectionSpanRange(idx)
					showResizeHandles = !s.hasMultiBlockSelection() || dragging
					borderHighlighted = true
					picBorder = pal.Accent
					picBox.Border(1, picBorder)
					if s.needsDetachedSelectionFocus() {
						s.multiSelFocusPending = true
						s.focusPending = false
						s.selSyncPending = false
					}
					s.scheduleRebuild(c)
					if !isVideo && !hasImage && mods&(ui.Shift|ui.Alt|ui.Cmd) == 0 {
						s.RequestPickImages(id, true)
					}
				}
			}
			s.recordImageLayoutBounds(id, picBox)
			picBox.DrawOver(func(p *ui.Painter, r ui.Rect) {
				appUi.RecordBounds(fmt.Sprintf("image-%d", id), r)
				if s.recordImageWinRect(id, r) && isFocused {
					c.Invalidate()
				}
				if borderHighlighted && r.W > borderW && r.H > borderW {
					p.Stroke(ui.Rect{
						X: r.X + borderW*0.5,
						Y: r.Y + borderW*0.5,
						W: r.W - borderW,
						H: r.H - borderW,
					}, pal.Accent, theme.RadiusSM, borderW)
				}
				if imgSelected && s.hasMultiBlockSelection() {
					p.Fill(r, pal.SelectionBG.Alpha(0.28), theme.RadiusSM)
				}
			})
			if !isVideo && !b.ImageCrop.IsZero() {
				picBox.ClipInsetPercent(b.ImageCrop.Top*100, b.ImageCrop.Right*100, b.ImageCrop.Bottom*100, b.ImageCrop.Left*100, theme.RadiusSM)
			}

			picBox.Children(func() {
				if isVideo {
					s.renderVideoContent(c, id, b)
				} else if animation != nil {
					if animExpanded, _ := s.previewExpandedState(b.ImageSrc); animExpanded {
						ui.Column(c).FillWidth().Height(theme.VideoPlaceholderHeight).Center().Gap(theme.Space8).Background(pal.CodeBlockBG).Children(func() {
							icons.Duo(c, icons.Image, theme.IconChrome, true)
							ui.Text(c, theme.Tr("正在放大视图中查看", "Viewing in expanded viewer")).
								FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
								TextColor(pal.TextSecondary).SingleLine()
						})
					} else {
						ui.Video(c, animation).FillWidth().Label(b.ImageAlt)
					}
				} else if hasImage {
					ui.Image(c, imgSrc).FillWidth()
				} else {
					phActive := picHovered && !dragging
					phBG := pal.CodeBlockBG
					if phActive {
						phBG = pal.Hover
					}
					ph := ui.Row(c.Key("img-ph")).FillWidth().Height(120).Center().Gap(theme.Space8)
					appUi.AnimateSurfaceColors(ph, phBG, ui.Transparent, false)
					ph.Children(func() {
						icCol := pal.HoverIconColors(phActive)
						txtCol := pal.TextSecondary
						if phActive {
							txtCol = pal.Text
						}
						icons.DrawAnimated(c, icons.Image, 20, icCol, phActive)
						lbl := b.ImageAlt
						if lbl == "" {
							lbl = b.ImageSrc
						}
						if lbl == "" {
							if !readOnly {
								lbl = theme.Tr("上传图片", "Upload image")
							} else {
								lbl = theme.Tr("图片", "Image")
							}
						}
						ui.Text(c, lbl).FontSize(theme.FontBody).TextColor(txtCol)
					})
				}
				if previewExpandable {
					if appUi.DrawMediaExpandBadge(c, fmt.Sprintf("img-expand-%d", id), theme.Tr("点击放大", "Click to zoom"), picHovered) {
						previewBadgeClicked = true
					}
				} else if docExpandable {
					if appUi.DrawMediaExpandBadge(c, fmt.Sprintf("img-expand-%d", id), theme.Tr("全屏查看", "Fullscreen"), picHovered) {
						docBadgeClicked = true
					}
				}
			})
			if previewExpandable && (previewBadgeClicked || picBox.Clicked()) {
				s.onPreviewMediaOpen(b.ImageSrc, false)
			} else if docExpandable && (docBadgeClicked || picBox.DoubleClicked()) {
				s.OpenMediaLightbox(id)
			}

			// Keep the visible grip compact while retaining a larger target for dragging.
			if showResizeHandles {
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
				hitW := s.scale(theme.ImageResizeHitWidth)
				hitH := s.scale(theme.ImageResizeHitHeight)
				edgeOffset := -hitW*0.5 + borderW*0.5

				anyHeld := false
				for _, side := range []struct {
					probe string
					left  bool
					dir   float32
				}{
					{"img-resize-left", true, -1},
					{"img-resize-right", false, 1},
				} {
					rail := ui.Column(c.Key(fmt.Sprintf("%s-rail-%d", side.probe, id))).Absolute().
						Top(0).Bottom(0).Width(hitW).Center().PassThrough().
						Transition(appUi.ResizeHandleTransition(-side.dir * s.scale(theme.Space8)))
					if side.left {
						rail.Left(edgeOffset)
					} else {
						rail.Right(edgeOffset)
					}
					var h ui.Element
					rail.Children(func() {
						h = ui.Box(c.Key(fmt.Sprintf("%s-%d", side.probe, id))).
							Width(hitW).Height(hitH).MaxHeightPercent(88).Center().Cursor(ui.CursorResizeEW)
						hovered := h.Hovered()
						dx, _, held := h.Dragged()
						pillFill := pal.Accent
						if held {
							pillFill = pal.AccentPressed
						} else if hovered {
							pillFill = pal.AccentHover
						}
						h.Children(func() {
							ui.Box(c).Width(s.scale(theme.ImageResizePillWidth)).
								Height(s.scale(theme.ImageResizePillHeight)).MaxHeightPercent(100).
								Radius(theme.RadiusPill).Background(pillFill).IgnoreInput(true)
						})
						appUi.Probe(h, side.probe)
						if held {
							anyHeld = true
							if s.imgResizeBlock != id {
								s.imgResizeBlock = id
								s.imgResizeStartPct = widthPct
								s.imgResizeCurPct = widthPct
							}
							if dx != 0 {
								deltaPct := (dx * side.dir * scale / availW) * 100
								s.imgResizeCurPct = clampFloat32(s.imgResizeCurPct+deltaPct, 10, 100)
							}
						}
					})
				}
				if !anyHeld && s.imgResizeBlock == id {
					finalPct := clampFloat32(s.imgResizeCurPct, 10, 100)
					startPct := s.imgResizeStartPct
					s.imgResizeBlock = 0
					if math.Abs(float64(finalPct-startPct)) >= 0.5 {
						s.pushUndo()
						widthPct = float32(math.Round(float64(finalPct)))
						b.ImageWidth = widthPct
						if widthPct == 100 {
							b.ImageWidth = 0
						}
						b.Dirty = true
						b.CleanSource = ""
						s.markDirty()
					}
				}
			}

			// Apply the drag delta to the current layout, including both rails.
			dragging = s.imgResizeBlock == id
			if dragging {
				widthPct = s.imgResizeCurPct
			}
			frameWrap.WidthPercent(widthPct)

			if dragging {
				pctText := fmt.Sprintf("%d%%", int(math.Round(float64(widthPct))))
				badge := ui.Row(c.Key(fmt.Sprintf("img-pct-%d", id))).Absolute().
					Right(theme.Space12).Bottom(theme.Space10).Height(theme.HeightCompact).
					PaddingX(theme.Space8).Center().Radius(theme.RadiusSM).
					Background(pal.TooltipBG).Border(1, pal.TooltipBorder).IgnoreInput(true)
				appUi.Elevate(badge, theme.ElevationRaised)
				badge.Children(func() {
					t := ui.Text(c, pctText).FontSize(theme.FontBody).FontWeight(theme.WeightMono).
						TextColor(pal.Text).SingleLine()
					if cdFont := resolvedCodeFont(); cdFont != "" {
						t.Font(cdFont)
					}
				})
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
	trigger := s.imageRectValue(id)
	if trigger.W <= 0 {
		trigger = s.blockRectValue(id)
	}
	if trigger.W <= 0 {
		trigger = ui.Rect{X: vp.X + 120, Y: vp.Y + 120, W: 400, H: 120}
	}
	spec := appUi.PlaceAbove(trigger).WithViewport(vp).WithAlignX(appUi.AlignX{Kind: appUi.AlignCenter}).WithGap(6).WithMargin(8)
	barW, barH := s.measuredOrEstSize("image-toolbar", 307, 38)
	x, y := appUi.ResolveOrigin(spec, barW, barH, winW, winH)

	imgToolbarBorder := pal.Hairline
	if pal.Dark || theme.IsSkinActive() {
		imgToolbarBorder = pal.MenuBorder
	}
	ui.Overlay(c, func() {
		bar := ui.Row(c.Key(fmt.Sprintf("image-toolbar-%d", id))).Absolute().Left(x).Top(y).
			Height(38).PaddingX(theme.Space6).Gap(theme.Space4).AlignItems(ui.Center).
			Radius(theme.RadiusXL).Background(pal.Menu).Border(1, imgToolbarBorder).Cursor(ui.CursorDefault).
			Transition(appUi.PopoverSpringTransition())
		appUi.Elevate(bar, theme.ElevationPopover)
		appUi.Probe(bar, "image-toolbar")
		s.recordOverlayPanel("image-toolbar", bar)

		bar.Children(func() {
			if !s.minimal {
				commentLbl := theme.Tr("添加批注", "Add annotation")
				commentBtn := appUi.IconButton(c, icons.TbComment, appUi.IconButtonOptions{
					ID: "img-tb-comment", Label: commentLbl, Tooltip: commentLbl,
					Shortcut:    formatShortcutDisplay("⌥⌘M", "Ctrl+Alt+M"),
					TooltipPill: true, TooltipAbove: true,
					Size: theme.HeightCompact, IconSize: theme.IconAction,
					Active: s.toolbarPop == "comment",
				})
				s.recordOverlayBtn("img-tb-comment", commentBtn)
				if commentBtn.Pressed() {
					s.commentBtnPressed = true
				}
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

			if strings.TrimSpace(b.ImageSrc) != "" {
				fullLbl := theme.Tr("全屏查看", "Fullscreen")
				fullBtn := appUi.IconButton(c, icons.Maximize, appUi.IconButtonOptions{
					ID:           "img-fullscreen",
					Label:        fullLbl,
					Tooltip:      fullLbl,
					Shortcut:     "F",
					TooltipPill:  true,
					TooltipAbove: true,
					Size:         theme.HeightCompact,
					IconSize:     theme.IconAction,
				})
				s.recordOverlayBtn("img-fullscreen", fullBtn)
				if fullBtn.Clicked() {
					s.OpenMediaLightbox(id)
				}
			}

			if !IsSupportedVideoPath(b.ImageSrc) {
				uploadLbl := theme.Tr("更换图片", "Replace image")
				if b.ImageSrc == "" {
					uploadLbl = theme.Tr("上传图片", "Upload image")
				}
				uploadBtn := appUi.IconButton(c, icons.Image, appUi.IconButtonOptions{
					ID:           "img-upload",
					Label:        uploadLbl,
					Tooltip:      uploadLbl,
					TooltipPill:  true,
					TooltipAbove: true,
					Size:         theme.HeightCompact,
					IconSize:     theme.IconAction,
				})
				s.recordOverlayBtn("img-upload", uploadBtn)
				if uploadBtn.Clicked() {
					s.imagePopBlock = 0
					s.imagePopKind = ""
					s.RequestPickImages(id, true)
				}

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

// soloAddIntroDur is how long the single-line add button plays its hover intro.
const soloAddIntroDur = 450 * time.Millisecond

const codeBlockTabWidth = 4

// applyCodeBlockTab indents or outdents code block text around [selStart, selEnd] in runes.
func applyCodeBlockTab(text string, selStart, selEnd int, outdent bool) (string, int, int) {
	rs := []rune(text)
	n := len(rs)
	lo := clampInt(min(selStart, selEnd), 0, n)
	hi := clampInt(max(selStart, selEnd), lo, n)

	hasNewline := false
	for i := lo; i < hi; i++ {
		if rs[i] == '\n' {
			hasNewline = true
			break
		}
	}

	if !outdent && !hasNewline {
		unit := []rune(strings.Repeat(" ", codeBlockTabWidth))
		next := make([]rune, 0, n-(hi-lo)+len(unit))
		next = append(next, rs[:lo]...)
		next = append(next, unit...)
		next = append(next, rs[hi:]...)
		pos := lo + len(unit)
		return string(next), pos, pos
	}

	// Find first line start covering lo, and last line end covering hi.
	lineStart := lo
	for lineStart > 0 && rs[lineStart-1] != '\n' {
		lineStart--
	}
	effHi := hi
	if hasNewline && effHi > lo && rs[effHi-1] == '\n' {
		effHi--
	}

	var out []rune
	out = append(out, rs[:lineStart]...)
	newLo, newHi := lo, hi
	pos := lineStart
	for pos <= n {
		end := pos
		for end < n && rs[end] != '\n' {
			end++
		}
		if outdent {
			rem := 0
			if pos < end && rs[pos] == '\t' {
				rem = 1
			} else {
				for rem < codeBlockTabWidth && pos+rem < end && rs[pos+rem] == ' ' {
					rem++
				}
			}
			if rem > 0 {
				if lo > pos {
					newLo -= min(rem, lo-pos)
				}
				if hi > pos {
					newHi -= min(rem, hi-pos)
				}
			}
			out = append(out, rs[pos+rem:end]...)
		} else {
			for k := 0; k < codeBlockTabWidth; k++ {
				out = append(out, ' ')
			}
			if lo >= pos {
				newLo += codeBlockTabWidth
			}
			if hi >= pos {
				newHi += codeBlockTabWidth
			}
			out = append(out, rs[pos:end]...)
		}
		if end >= n {
			break
		}
		out = append(out, '\n')
		pos = end + 1
		if pos > effHi {
			out = append(out, rs[pos:]...)
			break
		}
	}
	nextText := string(out)
	nextLen := len(out)
	newLo = clampInt(newLo, 0, nextLen)
	newHi = clampInt(newHi, newLo, nextLen)
	if selStart > selEnd {
		return nextText, newHi, newLo
	}
	return nextText, newLo, newHi
}
