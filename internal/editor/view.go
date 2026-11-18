package editor

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	editorHandleWidth float32 = 56
	editorRowGap      float32 = 4
	editorRightInset  float32 = 12
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
	root.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		s.editorRect = r
		appUi.RecordBounds("editor", r)
	})

	root.Children(func() {
		topPad := editorTopPad

		// Main scrollable document body
		scroll := ui.Scroll(c.Key("editor-scroll")).TrackScroll(&s.bodyScroll).Fill()
		scroll.Children(func() {
			// Center column: max 672px body + 56px handle gutter + 4px gap + 12px inset = 744px.
			outerMaxW := theme.EditorStandardWidth + editorHandleWidth + editorRowGap + editorRightInset
			col := ui.Column(c.Key("editor-canvas")).FillWidth().AlignItems(ui.Center).
				Padding(topPad, 0, editorBottomPad, 0)
			col.Children(func() {
				// Top margin click area to insert/focus top paragraph without shifting top padding.
				if !readOnly {
					topClick := ui.Box(c.Key("editor-top-margin")).Absolute().Top(0).Left(0).Right(0).Height(topPad).
						Cursor(ui.CursorText)
					if topClick.Pressed() || topClick.Clicked() {
						s.FocusBlock(0)
					}
				}

				flow := ui.Column(c.Key("editor-flow")).FillWidth().MaxWidth(outerMaxW)
				flow.Children(func() {
					nextHover := -1
					for i := range s.doc.Blocks {
						if s.isBlockHiddenByFold(i) {
							continue
						}
						if handled, hov := s.renderInlineAIPreviewRow(c, i, readOnly); handled {
							if hov {
								nextHover = i
							}
							continue
						}
						hovered := s.renderBlockRow(c, i, readOnly)
						if hovered {
							nextHover = i
						}
					}
					if nextHover != s.hoveredBlock {
						s.hoveredBlock = nextHover
						c.Invalidate()
					}
				})

				// Bottom blank space: clicking creates/focuses a trailing paragraph.
				if !readOnly {
					bottomBlank := ui.Box(c.Key("editor-bottom-margin")).FillWidth().MaxWidth(outerMaxW).
						MinHeight(120).Grow(1).Cursor(ui.CursorText)
					if bottomBlank.Pressed() || bottomBlank.Clicked() {
						s.focusOrInsertBottomParagraph()
					}
				}
			})
		})

		// Render floating overlays & margin rails
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
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	last := &s.doc.Blocks[len(s.doc.Blocks)-1]
	if last.Kind == model.BlockParagraph && last.PlainText() == "" {
		s.FocusBlock(len(s.doc.Blocks) - 1)
		return
	}
	s.pushUndo()
	nb := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
	s.doc.Blocks = append(s.doc.Blocks, nb)
	s.FocusBlock(len(s.doc.Blocks) - 1)
	s.markDirty()
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
		if s.inlineAI != nil {
			s.aiEscape()
		} else if s.findOpen {
			s.CloseFind()
		} else if s.hasAnyOverlayOpen() {
			s.closeTransientOverlays()
		}
	}
	if s.calloutMenuBlock != 0 && !readOnly {
		total := len(allCalloutKinds) + 1
		if c.Shortcut(0, ui.KeyDown) {
			s.calloutMenuIndex = (s.calloutMenuIndex + 1) % total
		} else if c.Shortcut(0, ui.KeyUp) {
			s.calloutMenuIndex = (s.calloutMenuIndex - 1 + total) % total
		} else if c.Shortcut(0, ui.KeyEnter) {
			if s.calloutMenuIndex >= 0 && s.calloutMenuIndex < len(allCalloutKinds) {
				k := allCalloutKinds[s.calloutMenuIndex]
				s.setCalloutKind(s.calloutMenuBlock, &k)
			} else {
				s.setCalloutKind(s.calloutMenuBlock, nil)
			}
		}
	}
	if readOnly {
		return
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

func (s *Session) renderBlockRow(c *ui.Context, idx int, readOnly bool) bool {
	pal := theme.Ocean()
	b := &s.doc.Blocks[idx]
	id := b.ID

	var prev *model.Block
	for p := idx - 1; p >= 0; p-- {
		if !s.isBlockHiddenByFold(p) {
			prev = &s.doc.Blocks[p]
			break
		}
	}
	topGap := blockGutterSpacing(prev, b)

	row := ui.Column(c.Key(id)).FillWidth().Padding(topGap, editorRightInset, 0, 0)
	if !readOnly && b.Kind != model.BlockImage && b.Kind != model.BlockDivider {
		row.Cursor(ui.CursorText)
	}
	isHovered := row.Hovered()
	isFocused := s.focusedBlock == idx
	isHighlighted := s.highlightedBlockID == id
	deleteArmed := idx == 0 && s.firstLineDeleteArmed && s.canArmFirstLineDelete()

	dragTarget := s.dragActive && s.dragOverIdx == idx
	row.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if prev, ok := s.blockRects[id]; !ok || prev != r {
			s.blockRects[id] = r
			if s.IsSelectionToolbarOpen() || s.inlineAI != nil || s.blockMenu != nil || s.calloutMenuBlock == id {
				c.Invalidate()
			}
		}
		appUi.RecordBounds(fmt.Sprintf("block-%d", id), r)
		if isHighlighted {
			appUi.RecordBounds("outline-target-highlight", r)
		}
		if dragTarget {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, pal.Accent, 0)
		}
	})
	if isHighlighted {
		row.Background(pal.Selected).Radius(6)
	}

	showHandles := !readOnly && !s.hasAnyOverlayOpen() && (isHovered || isFocused || s.dragBlockID == id)
	showBlankAdd := !readOnly && !s.hasAnyOverlayOpen() && isHovered
	isFolded := !readOnly && b.Kind.IsHeading() && s.folded[id]
	blankPara := b.Kind == model.BlockParagraph && b.PlainText() == ""
	listCtx := s.computeListCtx(idx)

	if s.inlineAI != nil {
		s.paintInlineAIOutline(c, row, id)
	}

	row.Children(func() {
		drawListGuides(c, id, listCtx)
		line := ui.Row(c.Key("row-inner")).FillWidth().AlignItems(ui.Start)
		line.Children(func() {
			// Left handle gutter (56px wide + 4px margin), strictly only shown on hover or focus!
			gutter := ui.Row(c.Key("gutter")).Width(editorHandleWidth).Height(24).Shrink(0).
				Margin(0, editorRowGap, 0, 0).Justify(ui.End).AlignItems(ui.Center).Gap(2).
				Cursor(ui.CursorDefault)
			gutter.Children(func() {
				if deleteArmed {
					delBtn := ui.ButtonBase(c.Key("first-line-delete-badge")).Size(24, 24).Center().
						Radius(6).Background(pal.DangerSurface).Border(1, pal.Danger.Alpha(0.4)).
						Cursor(ui.CursorPointer).Label(theme.Tr("删除空行", "Delete empty line")).
						Tooltip(theme.Tr("再次按删除键删除此行", "Press Backspace again to delete this line"))
					delBtn.Children(func() {
						icons.Draw(c, icons.KeyBackspace, 14, pal.MonoIconColors(pal.Danger))
					})
					appUi.Probe(delBtn, "first-line-delete-badge")
					if delBtn.Clicked() {
						s.ConfirmFirstLineDelete()
					}
				} else if blankPara {
					if showBlankAdd {
						addBtn := appUi.IconButton(c, icons.Plus, appUi.IconButtonOptions{
							ID:       fmt.Sprintf("block-add-%d", id),
							Label:    theme.Tr("插入块", "Insert block"),
							Size:     24,
							IconSize: 14,
						})
						if addBtn.Clicked() {
							s.FocusBlock(idx)
							s.openSlashMenu(idx, s.blockRects[id])
						}
					}
				} else if showHandles {
					grip := ui.ButtonBase(c.Key(fmt.Sprintf("block-grip-%d", id))).Size(24, 24).Center().
						Radius(6).Cursor(ui.CursorGrab).Label(theme.Tr("块操作", "Block menu"))
					if grip.Hovered() || s.dragBlockID == id {
						grip.Background(pal.Hover)
					}
					grip.Children(func() {
						icons.Draw(c, icons.Drag, 14, pal.MonoIconColors(pal.TextSecondary))
					})
					appUi.Probe(grip, fmt.Sprintf("block-grip-%d", id))
					_, dy, held := grip.Dragged()
					if held {
						s.dragBlockID = id
						if dy > 16 || dy < -16 {
							s.dragActive = true
							step := int(dy / 28)
							s.dragOverIdx = clampInt(idx+step, 0, len(s.doc.Blocks)-1)
						}
					} else if s.dragActive && s.dragBlockID == id {
						target := s.dragOverIdx
						s.dragActive = false
						s.dragBlockID = 0
						s.dragOverIdx = -1
						if target >= 0 && target != idx {
							s.moveBlock(idx, target)
						}
					} else if grip.Clicked() {
						s.dragBlockID = 0
						s.dragActive = false
						s.FocusBlock(idx)
						s.openHandleMenu(idx, s.blockRects[id])
					}
				}
				if !readOnly && b.Kind.IsHeading() && (showHandles || isFolded) {
					glyph := icons.ChevronDown
					if isFolded {
						glyph = icons.ChevronRight
					}
					foldBtn := appUi.IconButton(c, glyph, appUi.IconButtonOptions{
						ID:       fmt.Sprintf("block-fold-%d", id),
						Label:    theme.Tr("折叠章节", "Toggle fold"),
						Size:     24,
						IconSize: 14,
					})
					if foldBtn.Clicked() {
						s.toggleFold(id)
					}
				}
			})

			// List / indent marker gutter (depth-aware bullets, accent numbers, paper-tag todo box)
			indentPx := float32(min(max(b.Indent, 0), 12)) * editorIndentStep
			if b.Kind.IsList() {
				_, _, lh := blockTextMetrics(b.Kind)
				lineH := theme.FontBodySize * lh
				marker := ui.Row(c.Key("marker")).Width(editorMarkerWidth+indentPx).Shrink(0).
					Padding(0, 0, 0, indentPx).AlignItems(ui.Start)
				if b.Kind != model.BlockTodo {
					marker.Justify(ui.End)
				}
				marker.Children(func() {
					s.renderListMarker(c, idx, b, listCtx, theme.FontBodySize, lineH, readOnly)
				})
			} else if indentPx > 0 {
				ui.Box(c.Key("indent-pad")).Width(indentPx).Shrink(0)
			}

			// Standard 672px body column (no full-width switch)
			bodyMaxW := theme.EditorStandardWidth
			if b.Kind.IsList() {
				bodyMaxW = max(theme.EditorStandardWidth-editorMarkerWidth-indentPx, 240)
			}
			body := ui.Column(c.Key("body")).Grow(1).Shrink(1).MaxWidth(bodyMaxW)
			body.Children(func() {
				s.renderBlockBody(c, idx, b, isFocused, isHovered, readOnly, deleteArmed)
			})
		})
	})
	if !readOnly && b.Kind != model.BlockTable && b.Kind != model.BlockImage && b.Kind != model.BlockDivider &&
		(row.Pressed() || row.Clicked()) {
		caretOff := len([]rune(b.PlainText()))
		if px, py, ok := row.PointerPosition(); ok && (px < editorHandleWidth+editorRowGap || py < topGap) {
			caretOff = 0
		}
		s.FocusBlock(idx)
		s.selAnchor = Position{Block: idx, Offset: caretOff}
		s.selHead = s.selAnchor
		s.selSyncPending = true
	}

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
	s.renderBlockAnnotationsFoot(c, idx, b)
}

func (s *Session) renderDividerBlock(c *ui.Context, idx int, isFocused bool) {
	pal := theme.Ocean()
	box := ui.Row(c.Key("divider")).FillWidth().Height(18).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	lineColor := pal.Hairline
	lineH := float32(1)
	if isFocused {
		lineColor = pal.Accent
		lineH = 2
	}
	box.Children(func() {
		ui.Box(c.Key("hr-line")).FillWidth().Height(lineH).Background(lineColor)
	})
	appUi.Probe(box, fmt.Sprintf("divider-%d", s.doc.Blocks[idx].ID))
	if box.Clicked() {
		s.FocusBlock(idx)
	}
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
		return theme.FontBodySize, theme.WeightRegular, lhMode.Factor() // 16px body
	}
}

func (s *Session) renderRichTextBlock(c *ui.Context, idx int, b *model.Block, isFocused, readOnly, deleteArmed bool) {
	pal := theme.Ocean()
	dark := theme.IsDark()
	fontSize, weight, lh := blockTextMetrics(b.Kind)

	wrap := ui.Column(c.Key("text-wrap")).FillWidth()
	if b.Kind == model.BlockQuote {
		wrap.Padding(2, 0, 2, 16).BorderWidth(0, 0, 0, 3).BorderColor(pal.QuoteBar)
	} else if b.Kind == model.BlockCallout {
		st := calloutTone(b.Callout, dark)
		wrap.Padding(38, 16, 12, 44).Radius(theme.RadiusControl).Background(st.BG).Border(1, st.Border)
	}

	wrap.Children(func() {
		if b.Kind == model.BlockCallout {
			st := calloutTone(b.Callout, dark)
			open := s.calloutMenuBlock == b.ID
			curKind := b.Callout
			if curKind == "" {
				curKind = model.CalloutNote
			}
			hdr := ui.Row(c.Key("callout-hdr")).Absolute().Top(7).Left(10).Height(26).
				Gap(3).AlignItems(ui.Center)
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
				iconBtn := ui.ButtonBase(c.Key(fmt.Sprintf("callout-icon-%d", b.ID))).Size(26, 26).Center().
					Radius(6).Cursor(ui.CursorPointer).Label(theme.Tr("点击切换标注类型", "Click to switch callout type"))
				if open {
					iconBtn.Background(pal.Paper).Border(1, st.Accent)
				} else if iconBtn.Hovered() {
					iconBtn.Background(pal.Paper).Border(1, st.Border)
				} else {
					iconBtn.Border(1, ui.Transparent)
				}
				iconBtn.Children(func() {
					icons.Draw(c, icons.Callout, 16, theme.IconColors{Line: st.Accent, Fill: st.IconFill, Accent: st.Accent})
				})
				appUi.Probe(iconBtn, "callout-icon")
				if iconBtn.Clicked() {
					toggleMenu()
				}

				pillBtn := ui.ButtonBase(c.Key(fmt.Sprintf("callout-pill-%d", b.ID))).Height(26).PaddingX(4).Gap(4).
					AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer).Label(calloutLabel(curKind))
				if open {
					pillBtn.Background(pal.Paper).Border(1, st.Accent)
				} else if pillBtn.Hovered() {
					pillBtn.Background(pal.Paper).Border(1, st.Border)
				} else {
					pillBtn.Border(1, ui.Transparent)
				}
				pillBtn.Children(func() {
					ui.Text(c, calloutLabel(curKind)).FontSize(14).FontWeight(600).TextColor(st.Text).SingleLine()
					icons.Draw(c, icons.TbChevron, 12, pal.MonoIconColors(st.Text))
				})
				appUi.Probe(pillBtn, fmt.Sprintf("callout-pill-%d", b.ID))
				appUi.Probe(pillBtn, "callout-pill")
				if pillBtn.Clicked() {
					toggleMenu()
				}
			})
		}
		if isFocused && !readOnly {
			s.renderEditableTextBlock(c, idx, b, fontSize, weight, lh, deleteArmed)
			return
		}
		textCol := pal.Text
		if b.Kind == model.BlockHeading6 {
			textCol = pal.TextSecondary
		}
		edFont := resolvedEditorFont()
		var rt ui.Element
		if len(b.Spans) == 0 || b.PlainText() == "" {
			rt = ui.Text(c.Key("rt-empty"), " ").FontSize(fontSize).FontWeight(weight).LineHeight(lh).
				MinHeight(fontSize * lh).TextColor(pal.Placeholder).FillWidth()
		} else {
			rt = ui.RichText(c.Key("rt")).FontSize(fontSize).FontWeight(weight).
				LineHeight(lh).MinHeight(fontSize * lh).TextColor(textCol).FillWidth()
			rt.Children(func() {
				s.renderDisplaySpansChildren(c, idx, b, fontSize, weight)
			})
		}
		if edFont != "" {
			rt.Font(edFont)
		}
		if !readOnly {
			rt.Cursor(ui.CursorText)
		}
		if readOnly {
			if rt.Clicked() {
				caretOff := s.hitTestRichTextOffset(rt, idx, b, fontSize, weight, lh, edFont)
				if link := spanLinkAtOffset(b, caretOff); link != "" {
					c.OpenURL(link)
				}
			}
			return
		}
		mods := c.Modifiers()
		if rt.Clicked() && mods&ui.Cmd != 0 {
			caretOff := s.hitTestRichTextOffset(rt, idx, b, fontSize, weight, lh, edFont)
			if link := spanLinkAtOffset(b, caretOff); link != "" {
				c.OpenURL(link)
				return
			}
		}
		if (rt.Pressed() && mods&ui.Cmd == 0) || rt.Clicked() {
			caretOff := s.hitTestRichTextOffset(rt, idx, b, fontSize, weight, lh, edFont)
			if mods&ui.Shift != 0 {
				s.focusedBlock = idx
				s.selHead = Position{Block: idx, Offset: caretOff}
				s.focusPending = true
				s.selSyncPending = true
				s.firstLineDeleteArmed = false
			} else {
				s.FocusBlock(idx)
				s.selAnchor = Position{Block: idx, Offset: caretOff}
				s.selHead = s.selAnchor
				s.selSyncPending = true
			}
		}
	})
}

func (s *Session) hitTestRichTextOffset(rt ui.Element, idx int, b *model.Block, fontSize float32, weight int, lh float32, edFont string) int {
	plain := b.PlainText()
	if plain == "" {
		return 0
	}
	rlen := len([]rune(plain))
	px, py, ok := rt.PointerPosition()
	if !ok {
		return rlen
	}
	w := rt.Bounds().W
	if w <= 0 {
		w = theme.EditorStandardWidth
	}
	spans := s.buildDisplaySpans(idx, b, fontSize, weight)
	if len(spans) == 0 {
		return 0
	}
	tl := ui.ShapeRichText(spans, ui.Font{
		Family:     edFont,
		Size:       fontSize,
		Weight:     weight,
		LineHeight: lh,
	}, w)
	u16 := tl.IndexAt(ui.Point{X: px, Y: py})
	return clampInt(utf16OffsetToRunes(plain, u16), 0, rlen)
}

func utf16OffsetToRunes(s string, u16 int) int {
	if u16 <= 0 {
		return 0
	}
	units := 0
	runes := 0
	for _, r := range s {
		if units >= u16 {
			return runes
		}
		units += utf16.RuneLen(r)
		runes++
	}
	return runes
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
	oldText := draft
	handle := *s.blockHandle(b.ID)
	hint := emptyHintFor(b, deleteArmed)

	wrap := ui.Box(c.Key("edit-wrap")).FillWidth()
	paintEmptyHintBand(wrap, hint, fontSize, lh)

	var in ui.Element
	wrap.Children(func() {
		s.renderEmptyHintRow(c, hint, fontSize, lh)

		textCol := pal.Text
		if b.Kind == model.BlockHeading6 {
			textCol = pal.TextSecondary
		}
		in = ui.TextAreaBase(c.Key("input"), &draft).Lines(1, 500).FillWidth().
			Padding(0).BorderWidth(0).Background(ui.Transparent).
			FontSize(fontSize).FontWeight(weight).LineHeight(lh).MinHeight(fontSize * lh).
			TextColor(textCol).CaretColor(pal.Accent).CaretWidth(2).Bind(&handle)
		if edFont := resolvedEditorFont(); edFont != "" {
			in.Font(edFont)
		}

		if deleteArmed {
			in.CaretColor(pal.Danger)
		}

		// Apply inline span weights/colors + find/annotation highlights via TextRanges.
		ranges := s.buildInputTextRanges(idx, b)
		if len(ranges) > 0 {
			in.TextRanges(ranges...)
		}
		s.decorateEditInput(c, in, idx, b, draft, ranges, fontSize, weight, lh, deleteArmed)

		in.OnPaste(func(pasted string) bool {
			if strings.Contains(pasted, "\n") || looksLikeMarkdownBlock(pasted) {
				s.PasteText(pasted)
				return true
			}
			return false
		})

		in.OnTextKey(func(k ui.TextKey) bool {
			curText := k.Text()
			caretEstimate := s.estimateCaret(idx, oldText, curText)

			// Slash menu keyboard navigation
			if s.blockMenu != nil && s.blockMenu.Mode == menuSlash && s.blockMenu.BlockIdx == idx {
				switch {
				case k.Key == ui.KeyUp && k.Mods == 0:
					return s.stepSlashMenu(false)
				case k.Key == ui.KeyDown && k.Mods == 0:
					return s.stepSlashMenu(true)
				case k.Key == ui.KeyEnter && k.Mods == 0:
					if s.activateSlashMenu() {
						return true
					}
				case k.Key == ui.KeyEscape && k.Mods == 0:
					s.blockMenu = nil
					return true
				}
			}

			// Document-level shortcuts inside focused text area
			switch {
			case k.Mods == ui.Cmd && k.Key == ui.KeyC:
				if s.hasNonEmptySelection() {
					s.copyPending = true
					return true
				}
			case k.Mods == ui.Cmd && k.Key == ui.KeyZ:
				s.Undo()
				return true
			case (k.Mods == ui.Cmd|ui.Shift && k.Key == ui.KeyZ) || (k.Mods == ui.Cmd && k.Key == ui.KeyY):
				s.Redo()
				return true
			case k.Mods == ui.Cmd && k.Key == ui.KeyB:
				s.ToggleMarkOnSelection(model.MarkBold)
				return true
			case k.Mods == ui.Cmd && k.Key == ui.KeyI:
				s.ToggleMarkOnSelection(model.MarkItalic)
				return true
			case k.Mods == ui.Cmd && k.Key == ui.KeyU:
				s.ToggleMarkOnSelection(model.MarkUnderline)
				return true
			case k.Mods == ui.Cmd && k.Key == ui.KeyE:
				s.ToggleMarkOnSelection(model.MarkCode)
				return true
			case k.Mods == ui.Cmd|ui.Shift && k.Key == ui.KeyX:
				s.ToggleMarkOnSelection(model.MarkStrike)
				return true
			case k.Mods == ui.Cmd && k.Key == ui.KeyF:
				s.OpenFind()
				return true
			case k.Mods == 0 && k.Key == ui.KeyEnter:
				if s.handleEnterOnBlock(idx, curText, caretEstimate) {
					return true
				}
			case k.Mods == 0 && k.Key == ui.KeyBackspace:
				if s.handleBackspaceOnBlock(idx, curText, caretEstimate) {
					return true
				}
			case k.Mods == 0 && k.Key == ui.KeyTab:
				if b.Kind.IsList() && b.Indent < 8 {
					s.pushUndo()
					b.Indent++
					b.Dirty = true
					s.markDirty()
					return true
				}
			case k.Mods == ui.Shift && k.Key == ui.KeyTab:
				if b.Kind.IsList() && b.Indent > 0 {
					s.pushUndo()
					b.Indent--
					b.Dirty = true
					s.markDirty()
					return true
				}
			case k.Mods == 0 && k.Key == ui.KeyUp && caretEstimate == 0 && idx > 0:
				s.FocusBlock(idx - 1)
				endOff := len([]rune(s.doc.Blocks[idx-1].PlainText()))
				s.selAnchor = Position{Block: idx - 1, Offset: endOff}
				s.selHead = s.selAnchor
				s.selSyncPending = true
				return true
			case k.Mods == 0 && k.Key == ui.KeyDown && caretEstimate == len([]rune(curText)) && idx+1 < len(s.doc.Blocks):
				s.FocusBlock(idx + 1)
				return true
			}
			return false
		})

		if s.focusPending {
			in.Focus()
			s.focusPending = false
		}
		if s.selSyncPending && s.selAnchor.Block == idx && s.selHead.Block == idx {
			in.SetTextSelection(s.selAnchor.Offset, s.selHead.Offset)
			s.selSyncPending = false
		}

		if in.Changed() {
			_, selEnd := in.TextSelection()
			if selEnd == 0 && draft != "" {
				selEnd = estimateCaretAfterEdit(oldText, draft)
			}
			s.applyBlockTextEdit(idx, draft, selEnd)
		} else if !s.selSyncPending && in.Focused() {
			selStart, selEnd := in.TextSelection()
			if s.selAnchor.Block == s.selHead.Block {
				s.selAnchor = Position{Block: idx, Offset: selStart}
				s.selHead = Position{Block: idx, Offset: selEnd}
			}
		}
	})

	s.setBlockHandle(b.ID, handle)
}

func (s *Session) estimateCaret(idx int, oldText, curText string) int {
	if curText != oldText {
		return estimateCaretAfterEdit(oldText, curText)
	}
	if s.selHead.Block == idx {
		return clampInt(s.selHead.Offset, 0, len([]rune(curText)))
	}
	return len([]rune(curText))
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

func (s *Session) buildDisplaySpan(idx int, b *model.Block, sp model.Span, s0, s1 int, baseSize float32, baseWeight int, codeFG, codeBG ui.Color, codePadX, codePadY, codeRadius float32) (ui.Span, bool) {
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
	hasOverrideBG := false
	if sp.Marks&model.MarkCode != 0 {
		u.Font = resolvedCodeFont()
		u.Size = baseSize
		// MyGo snaps the mono face's 500 to its bold cut; 400 matches the
		// original's medium chip text visually.
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
		hasOverrideBG = true
	}
	// Highlight annotation range or find match, aligned to inline code chip band height.
	if annBG, ok := s.annotationHighlightColor(b.ID, s0, s1, pal); ok && (u.Background.A == 0 || sp.Marks&model.MarkCode != 0) {
		u.Background = annBG
		u.PaddingY = codePadY
		if u.Radius == 0 {
			u.Radius = 4
		}
		hasOverrideBG = true
	}
	if findBG, ok := s.findHighlightColor(idx, s0, s1, pal); ok {
		u.Background = findBG
		u.PaddingY = codePadY
		if u.Radius == 0 {
			u.Radius = 2
		}
		hasOverrideBG = true
	}
	return u, hasOverrideBG
}

func (s *Session) renderDisplaySpansChildren(c *ui.Context, idx int, b *model.Block, baseSize float32, baseWeight int) {
	pal := theme.Ocean()
	codeFG, codeBG, codePadX, codePadY, codeRadius := inlineCodeStyle(baseSize)
	srcSpans := s.splitSpansForHighlights(idx, b.ID, b.Spans)
	pos := 0
	for i, sp := range srcSpans {
		rlen := len([]rune(sp.Text))
		s0, s1 := pos, pos+rlen
		pos = s1
		u, hasOverrideBG := s.buildDisplaySpan(idx, b, sp, s0, s1, baseSize, baseWeight, codeFG, codeBG, codePadX, codePadY, codeRadius)
		isCode := sp.Marks&model.MarkCode != 0 && !hasOverrideBG
		if isCode {
			u.Background = ui.Color{}
		}
		el := ui.RichText(c.Key(fmt.Sprintf("sp-%d", i)), u)
		if isCode {
			el.Background(codeBG)
			if el.Hovered() {
				el.Background(pal.InlineCodeHoverBG)
			}
		}
		if sp.Link != "" {
			if el.Hovered() {
				el.Underline()
			}
			if s.readOnly || c.Modifiers()&ui.Cmd != 0 {
				el.Cursor(ui.CursorPointer)
			}
		}
	}
}

func (s *Session) buildDisplaySpans(idx int, b *model.Block, baseSize float32, baseWeight int) []ui.Span {
	if len(b.Spans) == 0 {
		return nil
	}
	codeFG, codeBG, codePadX, codePadY, codeRadius := inlineCodeStyle(baseSize)
	srcSpans := s.splitSpansForHighlights(idx, b.ID, b.Spans)
	out := make([]ui.Span, 0, len(srcSpans))
	pos := 0
	for _, sp := range srcSpans {
		rlen := len([]rune(sp.Text))
		s0, s1 := pos, pos+rlen
		pos = s1
		u, _ := s.buildDisplaySpan(idx, b, sp, s0, s1, baseSize, baseWeight, codeFG, codeBG, codePadX, codePadY, codeRadius)
		out = append(out, u)
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
	// Find matches are painted as bands behind the glyphs (edit_decor.go),
	// as in the original, rather than recoloured or bolded.
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

	card := ui.Column(c.Key("code-card")).FillWidth().Radius(6).
		Background(pal.CodeBlockBG).Padding(34, 11.7, 9.75, 11.7).Gap(8)
	appUi.Probe(card, fmt.Sprintf("code-card-%d", id))

	card.Children(func() {
		// Top controls bar: left Source toggle (for Mermaid/LaTeX/CSV), right Copy + Lang pill
		hdr := ui.Row(c.Key("code-hdr")).Absolute().Top(6).Left(11.7).Right(6).Height(22).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8)
		hdr.Children(func() {
			left := ui.Row(c.Key("code-hdr-left")).AlignItems(ui.Center).Gap(6)
			left.Children(func() {
				if previewable {
					srcOpen := b.ShowSource || strings.TrimSpace(b.Code) == ""
					lbl := theme.Tr("源码", "Source")
					if !srcOpen {
						lbl = theme.Tr("预览", "Preview") + " · " + theme.Tr("源码", "Source")
					}
					srcBtn := ui.ButtonBase(c.Key(fmt.Sprintf("cb-source-toggle-%d", id))).Height(22).PaddingX(6).
						Radius(6).AlignItems(ui.Center).Gap(4).Cursor(ui.CursorPointer).Label(lbl)
					if srcOpen || srcBtn.Hovered() {
						srcBtn.Background(pal.Hover)
					}
					srcBtn.Children(func() {
						g := icons.ChevronRight
						if srcOpen {
							g = icons.ChevronDown
						}
						icons.Draw(c, g, theme.IconInline, pal.MonoIconColors(pal.TextSecondary))
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
						Size:     22,
						IconSize: theme.IconInline,
					})
					if copyBtn.Clicked() {
						c.WriteClipboard(b.Code)
						s.codeCopiedBlock = id
					}

					pillFG := pal.MenuMuted
					pillHoverFG := pal.Text
					pillBG := ui.Hex("#fcfcfb")
					pillHoverBG := ui.Hex("#f4f3f0")
					if dark {
						pillFG = ui.Hex("#8b949e")
						pillHoverFG = ui.Hex("#e6edf3")
						pillBG = ui.Hex("#262a33")
						pillHoverBG = ui.Hex("#323844")
					}
					langBtn := ui.ButtonBase(c.Key(fmt.Sprintf("code-lang-%d", id))).Height(22).PaddingX(8).
						Radius(6).Center().Background(pillBG).Border(1, pal.MenuSeparator).
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
					appUi.Probe(langBtn, fmt.Sprintf("code-lang-%d", id))
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
			codeFG := ui.Hex("#393a34")
			if dark {
				codeFG = pal.Text
			}
			if isFocused && !readOnly {
				draft := b.Code
				oldCode := draft
				handle := *s.blockHandle(id)
				ta := ui.TextAreaBase(c.Key("code-input"), &draft).Lines(1, 500).FillWidth().
					Padding(0).BorderWidth(0).Background(ui.Transparent).
					Font(cdFont).FontSize(14).LineHeight(1.5).TextColor(codeFG).
					CaretColor(pal.Accent).CaretWidth(2).Bind(&handle)
				if ranges := codeSyntaxRanges(draft, canon); len(ranges) > 0 {
					ta.TextRanges(ranges...)
				}
				ta.OnTextKey(func(k ui.TextKey) bool {
					if k.Mods == ui.Cmd && k.Key == ui.KeyZ {
						s.Undo()
						return true
					}
					if (k.Mods == ui.Cmd|ui.Shift && k.Key == ui.KeyZ) || (k.Mods == ui.Cmd && k.Key == ui.KeyY) {
						s.Redo()
						return true
					}
					if k.Mods == 0 && k.Key == ui.KeyBackspace {
						if k.Text() == "" {
							return s.handleBackspaceOnBlock(idx, "", 0)
						}
					}
					return false
				})
				if s.focusPending {
					ta.Focus()
					s.focusPending = false
				}
				if ta.Changed() {
					_, selEnd := ta.TextSelection()
					s.applyBlockTextEdit(idx, draft, selEnd)
					_ = oldCode
				}
				s.setBlockHandle(id, handle)
			} else {
				spans := codeSyntaxSpans(b.Code, canon)
				var rt ui.Element
				if len(spans) == 0 {
					rt = ui.Text(c.Key("code-empty"), theme.Tr("// 输入代码…", "// Write code…")).
						Font(cdFont).FontSize(14).LineHeight(1.5).TextColor(pal.Placeholder).FillWidth()
				} else {
					rt = ui.RichText(c.Key("code-rt"), spans...).Font(cdFont).
						FontSize(14).LineHeight(1.5).TextColor(codeFG).FillWidth()
				}
				if !readOnly {
					rt.Cursor(ui.CursorText)
					if rt.Pressed() || rt.Clicked() {
						s.FocusBlock(idx)
					}
				}
			}
		}
	})
}

func (s *Session) renderCodePreview(c *ui.Context, id model.BlockID, canon, src string) {
	pal := theme.Ocean()
	box := ui.Column(c.Key(fmt.Sprintf("code-preview-%d", id))).FillWidth().
		Padding(12, 14).Radius(6).Background(pal.Paper).Border(1, pal.Hairline).Gap(8)
	appUi.Probe(box, fmt.Sprintf("code-preview-%d", id))

	box.Children(func() {
		switch canon {
		case "csv":
			rows := parseCSVRows(src)
			if len(rows) == 0 {
				ui.Text(c, theme.Tr("CSV 内容为空", "Empty CSV")).FontSize(14).TextColor(pal.TextSecondary)
				return
			}
			for rIdx, row := range rows {
				tr := ui.Row(c.Key(fmt.Sprintf("csv-r-%d", rIdx))).FillWidth().
					BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
				if rIdx == 0 {
					tr.Background(pal.Shell)
				}
				tr.Children(func() {
					for cIdx, cell := range row {
						tc := ui.Box(c.Key(fmt.Sprintf("csv-c-%d-%d", rIdx, cIdx))).Grow(1).Shrink(1).Padding(6, 8)
						tc.Children(func() {
							t := ui.Text(c, cell).FontSize(14).TextColor(pal.Text)
							if rIdx == 0 {
								t.FontWeight(600)
							}
						})
					}
				})
			}
		case "latex":
			formatted, errMsg := formatMathPreview(src)
			if formatted != "" {
				ui.Row(c.Key("math-paper")).FillWidth().Center().Padding(8, 12).Children(func() {
					ui.Text(c, formatted).FontSize(18).FontWeight(500).TextColor(pal.Text)
				})
			}
			if errMsg != "" {
				ui.Text(c, errMsg).FontSize(14).TextColor(pal.Danger)
			}
		case "mermaid":
			nodes, labels, edges, errMsg := parseMermaidGraph(src)
			if errMsg != "" {
				ui.Text(c, errMsg).FontSize(14).TextColor(pal.Danger)
				return
			}
			if len(edges) > 0 {
				for eIdx, e := range edges {
					row := ui.Row(c.Key(fmt.Sprintf("mmd-e-%d", eIdx))).AlignItems(ui.Center).Gap(8).Wrap()
					row.Children(func() {
						mermaidNodeBox(c, labels[e.From])
						arrow := "→"
						if e.Label != "" {
							arrow = fmt.Sprintf("— %s →", e.Label)
						}
						ui.Text(c, arrow).FontSize(14).TextColor(pal.Accent).FontWeight(600)
						mermaidNodeBox(c, labels[e.To])
					})
				}
			} else {
				row := ui.Row(c.Key("mmd-nodes")).Gap(8).Wrap()
				row.Children(func() {
					for _, n := range nodes {
						mermaidNodeBox(c, labels[n])
					}
				})
			}
		}
	})
}

func mermaidNodeBox(c *ui.Context, label string) {
	pal := theme.Ocean()
	ui.Box(c).Padding(6, 12).Radius(6).Background(pal.Selected).Border(1, pal.Accent).Children(func() {
		ui.Text(c, label).FontSize(14).FontWeight(500).TextColor(pal.Text)
	})
}

func syntaxTokenColor(kind syntaxTokenKind, dark bool) ui.Color {
	switch kind {
	case tokKeyword:
		if dark {
			return ui.Hex("#ff7b72")
		}
		return ui.Hex("#cf222e")
	case tokString:
		if dark {
			return ui.Hex("#a5d6ff")
		}
		return ui.Hex("#0a3069")
	case tokComment:
		if dark {
			return ui.Hex("#8b949e")
		}
		return ui.Hex("#6e7781")
	case tokNumber:
		if dark {
			return ui.Hex("#79c0ff")
		}
		return ui.Hex("#0550ae")
	case tokType:
		if dark {
			return ui.Hex("#ffa657")
		}
		return ui.Hex("#953800")
	case tokFunc:
		if dark {
			return ui.Hex("#d2a8ff")
		}
		return ui.Hex("#8250df")
	default:
		return theme.Ocean().Text
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
	dark := theme.IsDark()
	id := b.ID
	if b.Table == nil || len(b.Table.Rows) == 0 {
		return
	}
	tbl := b.Table
	cols := len(tbl.Rows[0])
	borderCol := ui.Hex("#e8e5dc")
	headerBG := ui.Hex("#f5f3ed")
	if dark {
		borderCol = pal.MenuBorder
		headerBG = ui.RGBA(255, 255, 255, 0.031)
	}

	wrap := ui.Column(c.Key("table-wrap")).FillWidth().Gap(4)
	showChrome := !readOnly && (isHovered || isFocused || s.tableMenuBlock == id)
	wrap.Children(func() {
		// Top bar sits inside the top gutter so hover changes never shift blocks below.
		if showChrome {
			bar := ui.Row(c.Key("table-top-bar")).Absolute().Top(-24).Right(26).Height(22).Justify(ui.End).Gap(4)
			bar.Children(func() {
				more := appUi.IconButton(c, icons.More, appUi.IconButtonOptions{
					ID:       fmt.Sprintf("table-more-%d", id),
					Label:    theme.Tr("表格操作", "Table options"),
					Size:     22,
					IconSize: 14,
				})
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

		gridRow := ui.Row(c.Key("table-grid-row")).FillWidth().AlignItems(ui.Stretch).Gap(4)
		gridRow.Children(func() {
			availGrid := max(theme.EditorStandardWidth-26, 200)
			if s.viewportWidth > 0 {
				availGrid = min(availGrid, max(s.viewportWidth-editorHandleWidth-editorRowGap-editorRightInset-26, 200))
			}
			buildGrid := func(needHScroll bool) {
				grid := ui.Column(c.Key("table-grid")).Radius(6).Border(1, borderCol).Clip()
				if needHScroll {
					grid.MinWidth(float32(cols) * tableMinColWidth)
				} else {
					grid.Grow(1).Shrink(1).MaxWidth(float32(max(cols, 1)) * tableMaxColWidth)
				}
				grid.Children(func() {
					for rIdx := range tbl.Rows {
						row := tbl.Rows[rIdx]
						rowEl := ui.Row(c.Key(fmt.Sprintf("tr-%d", rIdx))).FillWidth().MinHeight(tableMinRowHeight).
							BorderWidth(0, 0, boolToBorder(rIdx+1 < len(tbl.Rows)), 0).BorderColor(borderCol)
						if rIdx == 0 {
							rowEl.Background(headerBG)
						} else {
							rowEl.Background(pal.Paper)
						}
						rowEl.Children(func() {
							for cIdx := range row {
								s.renderTableCell(c, idx, id, tbl, rIdx, cIdx, borderCol, readOnly)
							}
						})
					}
				})
			}
			if float32(cols)*tableMinColWidth > availGrid {
				hs := ui.ScrollHorizontal(c.Key("table-hscroll")).Grow(1).Shrink(1)
				hs.Children(func() {
					buildGrid(true)
				})
			} else {
				buildGrid(false)
			}

			// Right add column bar (reserve 22px slot so column width stays stable)
			colSlot := ui.Box(c.Key("table-add-col-slot")).Width(22).Shrink(0)
			if showChrome {
				colSlot.Children(func() {
					addCol := ui.ButtonBase(c.Key("table-add-col")).Fill().Center().
						Radius(4).Background(pal.Hover).Cursor(ui.CursorPointer).
						Label(theme.Tr("添加列", "Add column"))
					addCol.Children(func() {
						icons.Draw(c, icons.Plus, 14, pal.MonoIconColors(pal.TextSecondary))
					})
					appUi.Probe(addCol, "table-add-col")
					if addCol.Clicked() {
						s.tableAddCol(id, len(tbl.Rows[0]))
					}
				})
			}
		})

		// Bottom add row bar (reserve 20px height + 4px gap = 24px EXTEND_ROOM so blocks below never jump)
		rowSlot := ui.Box(c.Key("table-add-row-slot")).FillWidth().Height(20)
		if showChrome {
			rowSlot.Children(func() {
				addRow := ui.ButtonBase(c.Key("table-add-row")).Fill().Center().
					Radius(4).Background(pal.Hover).Cursor(ui.CursorPointer).
					Label(theme.Tr("添加行", "Add row"))
				addRow.Children(func() {
					icons.Draw(c, icons.Plus, 14, pal.MonoIconColors(pal.TextSecondary))
				})
				appUi.Probe(addRow, "table-add-row")
				if addRow.Clicked() {
					s.tableAddRow(id, len(tbl.Rows))
				}
			})
		}
	})
}

func (s *Session) renderTableCell(c *ui.Context, blockIdx int, blockID model.BlockID, tbl *model.TableData, rIdx, cIdx int, borderCol ui.Color, readOnly bool) {
	pal := theme.Ocean()
	cell := &tbl.Rows[rIdx][cIdx]
	isCellFocused := !readOnly && s.focusedBlock == blockIdx &&
		s.tableActiveBlock == blockID && s.tableActiveRow == rIdx && s.tableActiveCol == cIdx

	box := ui.Box(c.Key(fmt.Sprintf("tc-%d-%d", rIdx, cIdx))).Grow(1).Shrink(1).
		MinWidth(tableMinColWidth).MaxWidth(tableMaxColWidth).Padding(6, 10).
		BorderWidth(0, boolToBorder(cIdx+1 < len(tbl.Rows[rIdx])), 0, 0).BorderColor(borderCol)
	if !readOnly {
		box.Cursor(ui.CursorText)
	}
	if isCellFocused {
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

	box.Children(func() {
		if isCellFocused {
			draft := cell.PlainText()
			in := ui.TextInputBase(c.Key("cell-in"), &draft).FillWidth().
				FontSize(tableCellFontSize).LineHeight(lhFactor).MinHeight(tableCellLineHeight).
				FontWeight(weight).TextColor(textCol).CaretColor(pal.Accent).CaretWidth(2)
			if edFont != "" {
				in.Font(edFont)
			}
			in.OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyTab {
					if k.Mods == ui.Shift {
						s.stepTableCell(blockID, tbl, rIdx, cIdx, -1)
					} else {
						s.stepTableCell(blockID, tbl, rIdx, cIdx, 1)
					}
					return true
				}
				if k.Key == ui.KeyEnter && k.Mods == 0 {
					s.stepTableCell(blockID, tbl, rIdx, cIdx, len(tbl.Rows[0]))
					return true
				}
				return false
			})
			if s.focusPending {
				in.Focus()
				s.focusPending = false
			}
			if in.Changed() {
				s.pushUndo()
				cell.Spans = []model.Span{{Text: draft}}
				s.doc.Blocks[blockIdx].Dirty = true
				s.doc.Blocks[blockIdx].CleanSource = ""
				s.markDirty()
			}
		} else {
			txt := cell.PlainText()
			var t ui.Element
			if txt == "" {
				t = ui.Text(c.Key("cell-txt"), " ").FontSize(tableCellFontSize).LineHeight(lhFactor).
					MinHeight(tableCellLineHeight).FontWeight(weight).TextColor(textCol).FillWidth()
			} else {
				cellBlock := model.Block{ID: blockID, Kind: model.BlockParagraph, Spans: cell.Spans}
				spans := s.buildDisplaySpans(blockIdx, &cellBlock, tableCellFontSize, weight)
				t = ui.RichText(c.Key("cell-txt"), spans...).FontSize(tableCellFontSize).LineHeight(lhFactor).
					MinHeight(tableCellLineHeight).FontWeight(weight).TextColor(textCol).FillWidth()
			}
			if edFont != "" {
				t.Font(edFont)
			}
			if !readOnly {
				t.Cursor(ui.CursorText)
			}
			if cIdx < len(tbl.Aligns) {
				switch tbl.Aligns[cIdx] {
				case model.ColAlignCenter:
					t.TextAlign(ui.Center)
				case model.ColAlignRight:
					t.TextAlign(ui.End)
				}
			}
			if !readOnly && (box.Pressed() || box.Clicked() || t.Pressed() || t.Clicked()) {
				s.focusedBlock = blockIdx
				s.tableActiveBlock = blockID
				s.tableActiveRow = rIdx
				s.tableActiveCol = cIdx
				s.focusPending = true
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

	outer := ui.Column(c.Key("img-outer")).FillWidth().AlignItems(align)
	outer.Children(func() {
		picBox := ui.Box(c.Key(fmt.Sprintf("img-frame-%d", id))).WidthPercent(widthPct).Radius(6).Clip().
			Cursor(ui.CursorPointer)
		picBorder := pal.Hairline
		if isFocused || dragging {
			picBorder = pal.Accent
		}
		picBox.Border(1, picBorder)
		picBox.DrawOver(func(_ *ui.Painter, r ui.Rect) {
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
		})
		if !b.ImageCrop.IsZero() {
			picBox.ClipInsetPercent(b.ImageCrop.Top*100, b.ImageCrop.Right*100, b.ImageCrop.Bottom*100, b.ImageCrop.Left*100, 6)
		}

		picBox.Children(func() {
			if imgSrc, ok := s.resolveImageSource(b.ImageSrc); ok {
				ui.Image(c, imgSrc).FillWidth()
			} else {
				ph := ui.Row(c.Key("img-ph")).FillWidth().Height(120).Center().Gap(8).Background(pal.Shell)
				ph.Children(func() {
					icons.Draw(c, icons.Image, 20, pal.MonoIconColors(pal.TextSecondary))
					lbl := b.ImageAlt
					if lbl == "" {
						lbl = b.ImageSrc
					}
					if lbl == "" {
						lbl = theme.Tr("图片", "Image")
					}
					ui.Text(c, lbl).FontSize(14).TextColor(pal.TextSecondary)
				})
			}

			// Left & Right drag-resize side handles (aligned with src/editor/image_resize.rs)
			if !readOnly && (isFocused || dragging || picBox.Hovered()) {
				scale := float32(1)
				if b.ImageAlign == model.ImageAlignCenter {
					scale = 2
				}
				availW := min(max(s.editorRect.W-editorHandleWidth-editorRowGap-editorRightInset, 200), theme.EditorStandardWidth)
				if availW <= 0 {
					availW = theme.EditorStandardWidth
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
						pillBG := ui.RGBA(255, 255, 255, 0.85)
						if theme.IsDark() {
							pillBG = ui.RGBA(34, 34, 34, 0.85)
						}
						ui.Box(c).Width(4).FillHeight().Radius(2).Background(pillBG).Border(1, pal.Hairline)
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
		if picBox.Clicked() && !readOnly && !dragging {
			s.FocusBlock(idx)
		}
	})

	// Floating Image Toolbar rendered in Overlay above picBox so focus shifts 0px below.
	if isFocused && !readOnly && !dragging {
		s.renderImageToolbar(c, idx, b)
	}
}

func (s *Session) overlayBounds(c *ui.Context) (minX, maxX, winW, winH float32) {
	winW, winH = c.Size()
	if winW <= 0 {
		winW = max(s.editorRect.X+s.viewportWidth, s.viewportWidth)
	}
	if winH <= 0 {
		winH = s.viewportHeight
	}
	minX = 8
	maxX = winW - 8
	if s.editorRect.W > 0 {
		minX = s.editorRect.X + 8
		maxX = s.editorRect.X + s.editorRect.W - 8
	}
	return minX, maxX, winW, winH
}

func (s *Session) renderImageToolbar(c *ui.Context, idx int, b *model.Block) {
	pal := theme.Ocean()
	id := b.ID
	minX, maxX, winW, winH := s.overlayBounds(c)
	trigger := s.imageRects[id]
	if trigger.W <= 0 {
		trigger = s.blockRects[id]
	}
	if trigger.W <= 0 {
		trigger = ui.Rect{X: minX + 112, Y: 120, W: 400, H: 120}
	}
	spec := appUi.PlaceAbove(trigger).WithAlignX(appUi.AlignX{Kind: appUi.AlignCenter}).WithGap(6).WithMargin(8)
	barW, barH := float32(284), float32(38)
	x, y := appUi.ResolveOrigin(spec, barW, barH, winW, winH)
	x = clampFloat32(x, minX, max(maxX-barW, minX))

	ui.Overlay(c, func() {
		col := ui.Column(c.Key(fmt.Sprintf("image-toolbar-ov-%d", id))).Absolute().Left(x).Top(y).
			AlignItems(ui.Center).Gap(6)
		col.Children(func() {
			bar := ui.Row(c.Key("image-toolbar")).Height(38).PaddingX(6).Gap(3).AlignItems(ui.Center).
				Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.Hairline).
				Shadow(0, 6, 18, 0, ui.RGBA(0, 0, 0, 0.14))
			appUi.Probe(bar, "image-toolbar")

			bar.Children(func() {
				commentBtn := appUi.IconButton(c, icons.TbComment, appUi.IconButtonOptions{
					ID: "img-tb-comment", Label: theme.Tr("添加批注", "Add annotation"), Size: 28, IconSize: 16,
				})
				if commentBtn.Clicked() {
					s.FocusBlock(idx)
					s.toolbarPop = "comment"
					s.commentFocusPending = true
				}

				reactBtn := appUi.IconButton(c, icons.TbReaction, appUi.IconButtonOptions{
					ID: "img-tb-reaction", Label: theme.Tr("添加表情回应", "Add reaction"), Size: 28, IconSize: 16,
				})
				if reactBtn.Clicked() {
					s.FocusBlock(idx)
					s.toolbarPop = "reactions"
				}

				ui.Box(c).Width(1).Height(20).MarginX(2).Background(pal.Hairline)

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
						ID: item.id, Label: item.label, Size: 28, IconSize: 16, Active: b.ImageAlign == item.align,
					})
					if btn.Clicked() {
						s.pushUndo()
						b.ImageAlign = item.align
						b.Dirty = true
						s.markDirty()
					}
				}

				ui.Box(c).Width(1).Height(20).MarginX(2).Background(pal.Hairline)

				cropBtn := appUi.IconButton(c, icons.Crop, appUi.IconButtonOptions{
					ID:       "img-crop",
					Label:    theme.Tr("裁剪图片", "Crop image"),
					Size:     28,
					IconSize: 16,
					Active:   s.imagePopBlock == id && s.imagePopKind == "crop",
				})
				if cropBtn.Clicked() {
					if s.imagePopBlock == id && s.imagePopKind == "crop" {
						s.imagePopKind = ""
					} else {
						s.imagePopBlock = id
						s.imagePopKind = "crop"
					}
				}

				sizeBtn := appUi.IconButton(c, icons.Sliders, appUi.IconButtonOptions{
					ID:       "img-size",
					Label:    theme.Tr("设置图片尺寸", "Set image size"),
					Size:     28,
					IconSize: 16,
					Active:   s.imagePopBlock == id && s.imagePopKind == "size",
				})
				if sizeBtn.Clicked() {
					if s.imagePopBlock == id && s.imagePopKind == "size" {
						s.imagePopKind = ""
					} else {
						s.imagePopBlock = id
						s.imagePopKind = "size"
					}
				}

				delBtn := appUi.IconButton(c, icons.Trash, appUi.IconButtonOptions{
					ID: "img-delete", Label: theme.Tr("删除图片", "Delete image"), Size: 28, IconSize: 16, Danger: true,
				})
				if delBtn.Clicked() {
					s.imagePopBlock = 0
					s.imagePopKind = ""
					s.deleteBlock(idx)
				}
			})

			// Image sub-popovers (Crop / Size / Alt)
			if s.imagePopBlock == id && s.imagePopKind != "" {
				s.renderImageSubPopover(c, idx, b)
			}
		})
	})
}

func timeAfter(t interface{ IsZero() bool }) bool {
	return false // Expiry handled explicitly via timers/actions so tests remain deterministic.
}
