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
	"github.com/egoist/mygo/ui"
)

const (
	editorHandleWidth float32 = 56
	editorRowGap      float32 = 4
	editorRightInset  float32 = 12
	editorMarkerWidth float32 = 29.68
	editorIndentStep  float32 = editorMarkerWidth / 2
	editorTopPad      float32 = 48
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
	appUi.Probe(root, "editor")

	root.Children(func() {
		topPad := editorTopPad
		if s.findOpen {
			topPad = 64
		}

		// Main scrollable document body
		scroll := ui.Scroll(c.Key("editor-scroll")).TrackScroll(&s.bodyScroll).Fill()
		scroll.Children(func() {
			// Center column: max 672px body + 56px handle gutter + 4px gap + 12px inset = 744px.
			outerMaxW := theme.EditorStandardWidth + editorHandleWidth + editorRowGap + editorRightInset
			col := ui.Column(c.Key("editor-canvas")).FillWidth().AlignItems(ui.Center).
				Padding(topPad, 0, editorBottomPad, 0)
			col.Children(func() {
				// Top margin click area to insert/focus top paragraph.
				if !readOnly {
					topClick := ui.Box(c.Key("editor-top-margin")).FillWidth().MaxWidth(outerMaxW).Height(12)
					if topClick.Clicked() {
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
						MinHeight(120).Grow(1)
					if bottomBlank.Clicked() {
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
		s.renderTableMenuOverlay(c)
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
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		s.OpenFind()
	}
	if c.Shortcut(0, ui.KeyEscape) {
		if s.findOpen {
			s.CloseFind()
		} else if s.hasAnyOverlayOpen() {
			s.closeTransientOverlays()
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

func blockGutterSpacing(prev, cur *model.Block) float32 {
	if prev == nil {
		return 0
	}
	if prev.Kind.IsList() && cur.Kind.IsList() {
		return 4
	}
	if cur.Kind == model.BlockHeading2 {
		return 24
	}
	if cur.Kind.IsHeading() {
		return 18
	}
	if cur.Kind == model.BlockDivider || prev.Kind == model.BlockDivider {
		return 14
	}
	if cur.Kind == model.BlockTable || prev.Kind == model.BlockTable {
		return 14
	}
	return 8
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
	isHovered := row.Hovered()
	isFocused := s.focusedBlock == idx
	isHighlighted := s.highlightedBlockID == id
	deleteArmed := idx == 0 && s.firstLineDeleteArmed && s.canArmFirstLineDelete()

	row.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		s.blockRects[id] = r
		appUi.RecordBounds(fmt.Sprintf("block-%d", id), r)
		if isHighlighted {
			appUi.RecordBounds("outline-target-highlight", r)
		}
	})
	if isHighlighted {
		row.Background(pal.Selected).Radius(6)
		appUi.Probe(row, "outline-target-highlight")
	}

	// Drag target indicator
	if s.dragActive && s.dragOverIdx == idx {
		row.BorderWidth(2, 0, 0, 0).BorderColor(pal.Accent)
	}

	showHandles := !readOnly && !s.hasAnyOverlayOpen() && (isHovered || isFocused || s.dragBlockID == id)
	isFolded := !readOnly && b.Kind.IsHeading() && s.folded[id]
	blankPara := b.Kind == model.BlockParagraph && b.PlainText() == ""

	row.Children(func() {
		line := ui.Row(c.Key("row-inner")).FillWidth().AlignItems(ui.Start)
		line.Children(func() {
			// Left handle gutter (56px wide + 4px margin), strictly only shown on hover or focus!
			gutter := ui.Row(c.Key("gutter")).Width(editorHandleWidth).Shrink(0).
				Margin(0, editorRowGap, 0, 0).Justify(ui.End).AlignItems(ui.Center).Gap(2)
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
				} else if showHandles {
					if blankPara {
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
					} else {
						grip := ui.ButtonBase(c.Key(fmt.Sprintf("block-grip-%d", id))).Size(26, 26).Center().
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

			// List / indent marker gutter
			indentPx := float32(min(max(b.Indent, 0), 12)) * editorIndentStep
			if b.Kind.IsList() {
				marker := ui.Row(c.Key("marker")).Width(editorMarkerWidth+indentPx).Shrink(0).
					Padding(0, 8, 0, indentPx).Justify(ui.End).AlignItems(ui.Center)
				marker.Children(func() {
					switch b.Kind {
					case model.BlockBullet:
						ui.Text(c, "•").FontSize(theme.FontBodySize).TextColor(pal.Text)
					case model.BlockNumbered:
						ui.Text(c, fmt.Sprintf("%d.", s.numberedListIndex(idx))).
							FontSize(theme.FontBodySize).TextColor(pal.Text)
					case model.BlockTodo:
						s.renderTodoCheckbox(c, idx, b.Checked, readOnly)
					}
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

	return isHovered
}

func (s *Session) renderTodoCheckbox(c *ui.Context, idx int, checked, readOnly bool) {
	pal := theme.Ocean()
	id := s.doc.Blocks[idx].ID
	cb := ui.ButtonBase(c.Key(fmt.Sprintf("todo-checkbox-%d", id))).Size(18, 18).Margin(4, 0, 0, 0).
		Center().Radius(4).Cursor(ui.CursorPointer).Disabled(readOnly).
		Label(theme.Tr("待办复选框", "Todo checkbox"))
	if checked {
		cb.Background(pal.Accent)
	} else {
		border := pal.BtnBorder
		if cb.Hovered() {
			border = pal.Accent
		}
		cb.Border(1.5, border)
	}
	cb.Children(func() {
		idLayer := ui.Box(c).Absolute().Left(0).Top(0).Size(18, 18).PassThrough()
		appUi.Probe(idLayer, fmt.Sprintf("todo-checkbox-%d", id))
		if checked {
			icons.Draw(c, icons.Check, 12, pal.MonoIconColors(pal.AccentText))
		}
	})
	appUi.Probe(cb, "todo-checkbox")
	if cb.Clicked() && !readOnly {
		s.ToggleTodo(idx)
	}
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

func blockTextMetrics(kind model.BlockKind) (fontSize float32, weight int, lineHeight float32) {
	switch kind {
	case model.BlockHeading1:
		return 30, 700, 1.3
	case model.BlockHeading2:
		return 24, 650, 1.35
	case model.BlockHeading3:
		return 20, 600, 1.4
	case model.BlockHeading4:
		return 18, 600, 1.45
	case model.BlockHeading5, model.BlockHeading6:
		return 16, 600, 1.5
	default:
		return theme.FontBodySize, 400, 1.5 // 16px body
	}
}

func calloutStyle(pal theme.Palette, kind model.CalloutKind) (bg, border, accent ui.Color, label string) {
	switch kind {
	case model.CalloutTip:
		return pal.SuccessSurface, pal.Success.Alpha(0.35), pal.Success, theme.Tr("提示", "Tip")
	case model.CalloutImportant:
		return pal.Selected, pal.Accent.Alpha(0.4), pal.Accent, theme.Tr("重要", "Important")
	case model.CalloutWarning:
		return pal.WarningSurface, pal.Warning.Alpha(0.4), pal.Warning, theme.Tr("警告", "Warning")
	case model.CalloutCaution:
		return pal.DangerSurface, pal.Danger.Alpha(0.4), pal.Danger, theme.Tr("注意", "Caution")
	default:
		return pal.Hover, pal.Accent.Alpha(0.3), pal.Accent, theme.Tr("标注", "Note")
	}
}

func (s *Session) renderRichTextBlock(c *ui.Context, idx int, b *model.Block, isFocused, readOnly, deleteArmed bool) {
	pal := theme.Ocean()
	fontSize, weight, lh := blockTextMetrics(b.Kind)

	wrap := ui.Column(c.Key("text-wrap")).FillWidth()
	if b.Kind == model.BlockQuote {
		wrap.Padding(2, 0, 2, 16).BorderWidth(0, 0, 0, 3).BorderColor(pal.QuoteBar)
	} else if b.Kind == model.BlockCallout {
		bg, brd, acc, lbl := calloutStyle(pal, b.Callout)
		wrap.Padding(10, 14).Gap(4).Radius(theme.RadiusControl).Background(bg).Border(1, brd)
		wrap.Children(func() {
			hdr := ui.Row(c.Key("callout-hdr")).Gap(6).AlignItems(ui.Center)
			hdr.Children(func() {
				icons.Draw(c, icons.Callout, 16, pal.MonoIconColors(acc))
				ui.Text(c, lbl).FontSize(14).FontWeight(600).TextColor(acc)
			})
		})
	}

	wrap.Children(func() {
		if isFocused && !readOnly {
			s.renderEditableTextBlock(c, idx, b, fontSize, weight, lh, deleteArmed)
			return
		}
		spans := s.buildDisplaySpans(idx, b, fontSize, weight)
		var rt ui.Element
		if len(spans) == 0 {
			placeholder := " "
			if idx == 0 && b.Kind == model.BlockParagraph {
				placeholder = theme.Tr("输入 '/' 插入块或开始写作…", "Type '/' for commands or start writing…")
			}
			rt = ui.Text(c.Key("rt-empty"), placeholder).FontSize(fontSize).LineHeight(lh).
				TextColor(pal.Placeholder).FillWidth()
		} else {
			rt = ui.RichText(c.Key("rt"), spans...).FontSize(fontSize).FontWeight(weight).
				LineHeight(lh).TextColor(pal.Text).FillWidth()
		}
		if rt.Clicked() && !readOnly {
			s.FocusBlock(idx)
			rlen := len([]rune(b.PlainText()))
			s.selAnchor = Position{Block: idx, Offset: rlen}
			s.selHead = s.selAnchor
			s.selSyncPending = true
		}
	})
}

func (s *Session) renderEditableTextBlock(c *ui.Context, idx int, b *model.Block, fontSize float32, weight int, lh float32, deleteArmed bool) {
	pal := theme.Ocean()
	draft := b.PlainText()
	oldText := draft
	handle := *s.blockHandle(b.ID)

	in := ui.TextAreaBase(c.Key("input"), &draft).Lines(1, 500).FillWidth().
		FontSize(fontSize).FontWeight(weight).LineHeight(lh).
		TextColor(pal.Text).CaretColor(pal.Accent).Bind(&handle)

	if deleteArmed {
		in.Placeholder(theme.Tr("再次按删除键删除此行", "Press Backspace again to delete this line")).
			PlaceholderColor(pal.Danger).CaretColor(pal.Danger)
	} else if b.Kind == model.BlockParagraph && idx == 0 && len(s.doc.Blocks) == 1 && draft == "" {
		in.Placeholder(theme.Tr("输入 '/' 插入块…", "Type '/' for commands…")).
			PlaceholderColor(pal.Placeholder)
	}

	// Apply inline span weights/colors + find/annotation highlights via TextRanges.
	ranges := s.buildInputTextRanges(idx, b)
	if len(ranges) > 0 {
		in.TextRanges(ranges...)
	}

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

func (s *Session) buildDisplaySpans(idx int, b *model.Block, baseSize float32, baseWeight int) []ui.Span {
	pal := theme.Ocean()
	dark := theme.IsDark()
	if len(b.Spans) == 0 {
		return nil
	}

	var out []ui.Span
	pos := 0
	for _, sp := range b.Spans {
		rlen := len([]rune(sp.Text))
		s0, s1 := pos, pos+rlen
		pos = s1
		u := ui.Span{Text: sp.Text}
		if sp.Marks&model.MarkBold != 0 {
			u.Weight = max(baseWeight, 700)
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
		if sp.Marks&model.MarkCode != 0 {
			u.Font = "monospace"
			u.Size = max(baseSize-2, theme.FontSmallMinSize)
			u.Background = pal.CodeBG
			u.Radius = 4
			u.PaddingX = 4
		}
		if sp.Link != "" {
			u.Color = pal.Accent
			u.Underline = true
		}
		if col, ok := theme.ResolveNamedFG(sp.FGColor, dark); ok {
			u.Color = col
		}
		if bg, ok := theme.ResolveNamedBG(sp.BGColor, dark); ok {
			u.Background = bg
			u.Radius = 3
		}
		// Highlight annotation range or find match if overlapping.
		if s.rangeHasAnnotation(b.ID, s0, s1) && u.Background.A == 0 {
			u.Background = pal.WarningSurface
		}
		if s.rangeHasFindMatch(idx, s0, s1) {
			u.Background = pal.SelectionBG
		}
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
			tr.Start, tr.End, tr.Weight = s0, s1, 700
			hasStyle = true
		}
		if col, ok := theme.ResolveNamedFG(sp.FGColor, dark); ok {
			tr.Start, tr.End, tr.Color = s0, s1, col
			hasStyle = true
		} else if sp.Link != "" {
			tr.Start, tr.End, tr.Color = s0, s1, pal.Accent
			hasStyle = true
		}
		if hasStyle {
			out = append(out, tr)
		}
	}
	if s.findOpen {
		for _, m := range s.findMatches {
			if m.BlockIdx == idx {
				out = append(out, ui.TextRange{Start: m.StartRune, End: m.EndRune, Color: pal.Accent, Weight: 600})
			}
		}
	}
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
	id := b.ID
	canon := canonicalLangTag(b.Language)
	previewable := isPreviewLang(canon)

	card := ui.Column(c.Key("code-card")).FillWidth().Radius(theme.RadiusControl).
		Background(pal.CodeBG).Border(1, pal.Hairline).Padding(10, 14).Gap(8)
	if isFocused {
		card.Border(1.5, pal.Accent)
	}

	card.Children(func() {
		// Top controls bar: left Source toggle (for Mermaid/LaTeX/CSV), right Copy + Lang + Wrap
		hdr := ui.Row(c.Key("code-hdr")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8)
		hdr.Children(func() {
			left := ui.Row(c.Key("code-hdr-left")).AlignItems(ui.Center).Gap(6)
			left.Children(func() {
				if previewable {
					srcOpen := b.ShowSource || strings.TrimSpace(b.Code) == ""
					lbl := theme.Tr("源码", "Source")
					if !srcOpen {
						lbl = theme.Tr("预览", "Preview") + " · " + theme.Tr("源码", "Source")
					}
					srcBtn := ui.ButtonBase(c.Key(fmt.Sprintf("cb-source-toggle-%d", id))).Height(24).PaddingX(8).
						Radius(6).AlignItems(ui.Center).Gap(4).Cursor(ui.CursorPointer).Label(lbl)
					if srcOpen || srcBtn.Hovered() {
						srcBtn.Background(pal.Hover)
					}
					srcBtn.Children(func() {
						g := icons.ChevronRight
						if srcOpen {
							g = icons.ChevronDown
						}
						icons.Draw(c, g, 14, pal.MonoIconColors(pal.TextSecondary))
						ui.Text(c, theme.Tr("源码", "Source")).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
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
						Size:     24,
						IconSize: 14,
					})
					if copyBtn.Clicked() {
						c.WriteClipboard(b.Code)
						s.codeCopiedBlock = id
					}

					langBtn := ui.ButtonBase(c.Key(fmt.Sprintf("code-lang-%d", id))).Height(24).PaddingX(8).
						Radius(6).Center().Background(pal.Card).Border(1, pal.BtnBorder).
						Cursor(ui.CursorPointer).Label(languageDisplayLabel(b.Language))
					langBtn.Children(func() {
						ui.Text(c, languageDisplayLabel(b.Language)).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
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
			if isFocused && !readOnly {
				draft := b.Code
				oldCode := draft
				handle := *s.blockHandle(id)
				ta := ui.TextAreaBase(c.Key("code-input"), &draft).Lines(1, 500).FillWidth().
					Font("monospace").FontSize(14).LineHeight(1.5).TextColor(pal.Text).
					CaretColor(pal.Accent).Bind(&handle)
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
				if len(spans) == 0 {
					rt := ui.Text(c.Key("code-empty"), theme.Tr("// 输入代码…", "// Write code…")).
						Font("monospace").FontSize(14).TextColor(pal.Placeholder).FillWidth()
					if rt.Clicked() && !readOnly {
						s.FocusBlock(idx)
					}
				} else {
					rt := ui.RichText(c.Key("code-rt"), spans...).Font("monospace").
						FontSize(14).LineHeight(1.5).TextColor(pal.Text).FillWidth()
					if rt.Clicked() && !readOnly {
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

func (s *Session) renderTableBlock(c *ui.Context, idx int, b *model.Block, isFocused, isHovered, readOnly bool) {
	pal := theme.Ocean()
	id := b.ID
	if b.Table == nil || len(b.Table.Rows) == 0 {
		return
	}
	tbl := b.Table

	wrap := ui.Column(c.Key("table-wrap")).FillWidth().Gap(4)
	showChrome := !readOnly && (isHovered || isFocused || s.tableMenuBlock == id)
	wrap.Children(func() {
		// Top bar: reserve consistent height so hover changes never shift blocks below.
		bar := ui.Row(c.Key("table-top-bar")).FillWidth().Height(24).Justify(ui.End).Gap(4)
		if showChrome {
			bar.Children(func() {
				more := appUi.IconButton(c, icons.More, appUi.IconButtonOptions{
					ID:       fmt.Sprintf("table-more-%d", id),
					Label:    theme.Tr("表格操作", "Table options"),
					Size:     24,
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
			grid := ui.Column(c.Key("table-grid")).Grow(1).Shrink(1).
				Radius(6).Border(1, pal.Hairline).Clip()
			grid.Children(func() {
				for rIdx := range tbl.Rows {
					row := tbl.Rows[rIdx]
					rowEl := ui.Row(c.Key(fmt.Sprintf("tr-%d", rIdx))).FillWidth().
						BorderWidth(0, 0, boolToBorder(rIdx+1 < len(tbl.Rows)), 0).BorderColor(pal.Hairline)
					if rIdx == 0 {
						rowEl.Background(pal.Shell)
					} else {
						rowEl.Background(pal.Paper)
					}
					rowEl.Children(func() {
						for cIdx := range row {
							s.renderTableCell(c, idx, id, tbl, rIdx, cIdx, readOnly)
						}
					})
				}
			})

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

		// Bottom add row bar (reserve 22px height so blocks below never jump)
		rowSlot := ui.Box(c.Key("table-add-row-slot")).FillWidth().Height(22)
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

func (s *Session) renderTableCell(c *ui.Context, blockIdx int, blockID model.BlockID, tbl *model.TableData, rIdx, cIdx int, readOnly bool) {
	pal := theme.Ocean()
	cell := &tbl.Rows[rIdx][cIdx]
	isCellFocused := !readOnly && s.focusedBlock == blockIdx &&
		s.tableActiveBlock == blockID && s.tableActiveRow == rIdx && s.tableActiveCol == cIdx

	box := ui.Box(c.Key(fmt.Sprintf("tc-%d-%d", rIdx, cIdx))).Grow(1).Shrink(1).Padding(6, 10).
		BorderWidth(0, boolToBorder(cIdx+1 < len(tbl.Rows[rIdx])), 0, 0).BorderColor(pal.Hairline)
	if isCellFocused {
		box.Background(pal.Selected)
	}
	weight := 400
	if rIdx == 0 {
		weight = 600
	}

	box.Children(func() {
		if isCellFocused {
			draft := cell.PlainText()
			in := ui.TextInputBase(c.Key("cell-in"), &draft).FillWidth().FontSize(14).
				FontWeight(weight).TextColor(pal.Text).CaretColor(pal.Accent)
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
			if txt == "" {
				txt = " "
			}
			t := ui.Text(c.Key("cell-txt"), txt).FontSize(14).FontWeight(weight).TextColor(pal.Text).FillWidth()
			if cIdx < len(tbl.Aligns) {
				switch tbl.Aligns[cIdx] {
				case model.ColAlignCenter:
					t.TextAlign(ui.Center)
				case model.ColAlignRight:
					t.TextAlign(ui.End)
				}
			}
			if (box.Clicked() || t.Clicked()) && !readOnly {
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

	outer := ui.Column(c.Key("img-outer")).FillWidth().AlignItems(align).Gap(8)
	outer.Children(func() {
		// Floating Image Toolbar shown when image block is focused
		if isFocused && !readOnly {
			s.renderImageToolbar(c, idx, b)
		}

		picBox := ui.Box(c.Key(fmt.Sprintf("img-frame-%d", id))).WidthPercent(widthPct).Radius(6).Clip().
			Cursor(ui.CursorPointer)
		if isFocused {
			picBox.Border(2, pal.Accent)
		} else {
			picBox.Border(1, pal.Hairline)
		}
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
		})
		appUi.Probe(picBox, fmt.Sprintf("image-%d", id))
		if picBox.Clicked() && !readOnly {
			s.FocusBlock(idx)
		}
	})
}

func (s *Session) renderImageToolbar(c *ui.Context, idx int, b *model.Block) {
	pal := theme.Ocean()
	id := b.ID
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
			ID:     "img-crop",
			Label:  theme.Tr("裁剪图片", "Crop image"),
			Size:   28,
			IconSize: 16,
			Active: s.imagePopBlock == id && s.imagePopKind == "crop",
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
			ID:     "img-size",
			Label:  theme.Tr("设置图片尺寸", "Set image size"),
			Size:   28,
			IconSize: 16,
			Active: s.imagePopBlock == id && s.imagePopKind == "size",
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
}

func timeAfter(t interface{ IsZero() bool }) bool {
	return false // Expiry handled explicitly via timers/actions so tests remain deterministic.
}
