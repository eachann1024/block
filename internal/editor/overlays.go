package editor

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf16"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

func (s *Session) overlayViewport(c *ui.Context) (vp ui.Rect, winW, winH float32) {
	winW, winH = c.Size()
	if winW <= 0 {
		winW = max(s.editorRect.X+s.viewportWidth, s.viewportWidth)
	}
	if winH <= 0 {
		winH = s.viewportHeight
	}
	if winW <= 0 {
		winW = 1024
	}
	if winH <= 0 {
		winH = 720
	}
	vp = ui.Rect{X: 0, Y: 0, W: winW, H: winH}
	if s.scrollRect.W > 0 && s.scrollRect.H > 0 {
		vp = s.scrollRect
	} else if s.editorRect.W > 0 && s.editorRect.H > 0 {
		vp = s.editorRect
	}
	return vp, winW, winH
}

func (s *Session) recordOverlayBtn(key string, el ui.Element) {
	if key == "" || !el.Valid() {
		return
	}
	if r := el.LayoutBounds(); r.W > 0 && r.H > 0 {
		if s.overlayBtnRects == nil {
			s.overlayBtnRects = make(map[string]ui.Rect)
		}
		s.overlayBtnRects[key] = r
	}
}

func (s *Session) recordOverlayPanel(key string, el ui.Element) {
	if key == "" || !el.Valid() {
		return
	}
	if r := el.LayoutBounds(); r.W > 0 && r.H > 0 {
		if s.overlayPanelRects == nil {
			s.overlayPanelRects = make(map[string]ui.Rect)
		}
		s.overlayPanelRects[key] = r
	}
}

func (s *Session) measuredOrEstSize(key string, estW, estH float32) (float32, float32) {
	if r, ok := s.overlayPanelRects[key]; ok {
		if r.W > 0 {
			estW = r.W
		}
		if r.H > 0 {
			estH = r.H
		}
	}
	return estW, estH
}

func (s *Session) clearToolbarBtnRects() {
	for k := range s.overlayBtnRects {
		if strings.HasPrefix(k, "tb-") {
			delete(s.overlayBtnRects, k)
		}
	}
}

func (s *Session) overlayBtnBounds(key string) (ui.Rect, bool) {
	if r, ok := s.overlayBtnRects[key]; ok && r.W > 0 && r.H > 0 {
		return r, true
	}
	return ui.Rect{}, false
}

func estimateEditorMenuHeight(itemCount, sepCount int) float32 {
	totalRows := itemCount + sepCount
	if totalRows == 0 {
		return theme.MenuPadding * 2
	}
	h := theme.MenuPadding*2 + float32(itemCount)*theme.MenuItemHeight + float32(sepCount)*7
	if totalRows > 1 {
		h += float32(totalRows-1) * theme.MenuGap
	}
	return h
}

// --- 1. In-Note Find Bar (Cmd+F) ---

func (s *Session) renderFindBar(c *ui.Context) {
	if !s.findOpen {
		return
	}
	pal := theme.Ocean()
	vp, winW, _ := s.overlayViewport(c)
	topY := clampFloat32(vp.Y+12, vp.Y+8, max(vp.Y+vp.H-48, vp.Y+8))
	insetRight := float32(20)
	if vp.W > 0 && vp.W < 420 {
		insetRight = 8
	}
	rightX := max(winW-(vp.X+vp.W), 0) + insetRight
	maxBarW := max(vp.W-insetRight*2, 220)
	inputW := float32(180)
	if vp.W > 0 && vp.W < 420 {
		inputW = clampFloat32(180-(420-vp.W), 72, 180)
	}

	ui.Overlay(c, func() {
		bar := ui.Row(c.Key("find-bar")).Absolute().Top(topY).Right(rightX).MaxWidth(maxBarW).
			Padding(4, 4, 4, 10).Gap(4).AlignItems(ui.Center).
			Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.MenuBorder).
			Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.06)).
			Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.08)).
			Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.12)).
			Transition(appUi.PopupTransition())
		appUi.Probe(bar, "find-bar")
		s.recordOverlayPanel("find-bar", bar)

		bar.Children(func() {
			icons.Draw(c, icons.FindSearch, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))

			in := ui.TextInputBase(c.Key("find-input"), &s.findQuery).Width(inputW).Height(28).
				Padding(0).FontSize(theme.FontBody).TextColor(pal.Text).CaretColor(pal.Accent).
				Placeholder(theme.Tr("查找…", "Find…")).Label("find-input").
				Bind(&s.findInputHandle)
			appUi.Probe(in, "find-input")
			in.OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter {
					if k.Mods == ui.Shift {
						s.FindPrev()
					} else {
						s.FindNext()
					}
					return true
				}
				if k.Key == ui.KeyEscape {
					s.CloseFind()
					return true
				}
				return false
			})
			if s.findFocusPending {
				in.Focus()
				s.findFocusPending = false
			}
			if in.Changed() {
				s.SetFindQuery(s.findQuery)
			}

			countText := ""
			if s.findQuery != "" {
				if len(s.findMatches) == 0 {
					countText = theme.Tr("无结果", "No results")
				} else {
					countText = fmt.Sprintf("%d / %d", s.findCurrent+1, len(s.findMatches))
				}
			}
			ui.Row(c).Width(72).PaddingX(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				ui.Text(c, countText).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
			})

			ui.Box(c).Width(1).Height(18).Margin(0, 2).Background(pal.SidebarBorder)

			prevBtn := appUi.IconButton(c, icons.ChevronUp, appUi.IconButtonOptions{
				ID:       "find-prev",
				Label:    theme.Tr("上一个", "Previous match"),
				Size:     theme.ControlHeight,
				IconSize: theme.IconInline,
				Disabled: len(s.findMatches) == 0,
			})
			if prevBtn.Clicked() {
				s.FindPrev()
			}

			nextBtn := appUi.IconButton(c, icons.ChevronDown, appUi.IconButtonOptions{
				ID:       "find-next",
				Label:    theme.Tr("下一个", "Next match"),
				Size:     theme.ControlHeight,
				IconSize: theme.IconInline,
				Disabled: len(s.findMatches) == 0,
			})
			if nextBtn.Clicked() {
				s.FindNext()
			}

			closeBtn := appUi.IconButton(c, icons.Close, appUi.IconButtonOptions{
				ID:       "find-close",
				Label:    theme.Tr("关闭查找", "Close find"),
				Size:     theme.ControlHeight,
				IconSize: theme.IconInline,
			})
			if closeBtn.Clicked() {
				s.CloseFind()
			}
		})
	})
}

// --- 2. Single-Row Adaptive Selection Floating Toolbar & Format Popovers ---

type toolbarItemKind uint8

const (
	tbAIRewrite toolbarItemKind = iota
	tbComment
	tbReaction
	tbTextType
	tbBold
	tbItalic
	tbUnderline
	tbStrike
	tbCode
	tbPalette
	tbLists
	tbLink
)

type toolbarItemSpec struct {
	Kind       toolbarItemKind
	ID         string
	Group      uint8
	Width      float32
	Icon       icons.Icon
	Label      string
	Shortcut   string
	HasSubmenu bool
}

func (s *Session) enabledToolbarItems(caps toolbarCaps) []toolbarItemSpec {
	var items []toolbarItemSpec
	if !s.readOnly && s.isMarkdownNote() && s.canReplaceSelection() && s.hasNonEmptySelection() {
		w := float32(96)
		if theme.IsEn() {
			w = 116
		}
		items = append(items, toolbarItemSpec{
			Kind: tbAIRewrite, ID: "tb-ai", Group: 0, Width: w,
			Icon: icons.Sparkles, Label: theme.Tr("AI 改写", "AI Rewrite"),
		})
	}
	if !s.minimal {
		items = append(items,
			toolbarItemSpec{
				Kind: tbComment, ID: "tb-comment", Group: 1, Width: 28,
				Icon: icons.TbComment, Label: theme.Tr("添加批注", "Add annotation"),
				Shortcut: formatShortcutDisplay("⌥⌘M", "Ctrl+Alt+M"),
			},
			toolbarItemSpec{
				Kind: tbReaction, ID: "tb-reaction", Group: 1, Width: 28,
				Icon: icons.TbReaction, Label: theme.Tr("添加表情回应", "Add reaction"),
			},
		)
	}
	if caps.BlockType {
		items = append(items, toolbarItemSpec{
			Kind: tbTextType, ID: "tb-type", Group: 2, Width: 41,
			Icon: icons.TbType, Label: theme.Tr("文字类型", "Text type"), HasSubmenu: true,
		})
	}
	if caps.RichText {
		if caps.Bold {
			items = append(items, toolbarItemSpec{
				Kind: tbBold, ID: "tb-bold", Group: 3, Width: 28,
				Icon: icons.TbBold, Label: theme.Tr("粗体", "Bold"),
				Shortcut: formatShortcutDisplay("⌘B", "Ctrl+B"),
			})
		}
		items = append(items,
			toolbarItemSpec{
				Kind: tbItalic, ID: "tb-italic", Group: 3, Width: 28,
				Icon: icons.TbItalic, Label: theme.Tr("斜体", "Italic"),
				Shortcut: formatShortcutDisplay("⌘I", "Ctrl+I"),
			},
			toolbarItemSpec{
				Kind: tbUnderline, ID: "tb-underline", Group: 3, Width: 28,
				Icon: icons.TbUnderline, Label: theme.Tr("下划线", "Underline"),
				Shortcut: formatShortcutDisplay("⌘U", "Ctrl+U"),
			},
			toolbarItemSpec{
				Kind: tbStrike, ID: "tb-strike", Group: 3, Width: 28,
				Icon: icons.TbStrike, Label: theme.Tr("删除线", "Strikethrough"),
				Shortcut: formatShortcutDisplay("⇧⌘S", "Ctrl+Shift+S"),
			},
			toolbarItemSpec{
				Kind: tbCode, ID: "tb-code", Group: 3, Width: 28,
				Icon: icons.TbCode, Label: theme.Tr("行内代码", "Inline code"),
				Shortcut: formatShortcutDisplay("⌘E", "Ctrl+E"),
			},
			toolbarItemSpec{
				Kind: tbPalette, ID: "tb-palette", Group: 3, Width: 41,
				Icon: icons.TbPalette, Label: theme.Tr("文本与高亮颜色", "Text and highlight color"), HasSubmenu: true,
			},
		)
	}
	if caps.BlockType {
		items = append(items, toolbarItemSpec{
			Kind: tbLists, ID: "tb-list", Group: 4, Width: 41,
			Icon: icons.TbList, Label: theme.Tr("列表", "Lists"), HasSubmenu: true,
		})
	}
	if caps.Link {
		items = append(items, toolbarItemSpec{
			Kind: tbLink, ID: "tb-link", Group: 5, Width: 28,
			Icon: icons.TbLink, Label: theme.Tr("链接", "Link"),
			Shortcut: formatShortcutDisplay("⇧⌘K", "Ctrl+Shift+K"),
		})
	}
	return items
}

func sequenceToolbarWidth(items []toolbarItemSpec, withMore bool) float32 {
	if len(items) == 0 && !withMore {
		return 0
	}
	w := float32(14)
	hasPrev := false
	var prevGroup uint8
	for _, it := range items {
		if hasPrev {
			if prevGroup == it.Group {
				w += 2
			} else {
				w += 13
			}
		}
		w += it.Width
		prevGroup = it.Group
		hasPrev = true
	}
	if withMore {
		if hasPrev {
			w += 13
		}
		w += 28
	}
	return w
}

func splitForRoom(enabled []toolbarItemSpec, room float32) (keep int, width float32) {
	full := sequenceToolbarWidth(enabled, false)
	if full <= room {
		return len(enabled), full
	}
	for k := len(enabled) - 1; k >= 0; k-- {
		if w := sequenceToolbarWidth(enabled[:k], true); w <= room {
			return k, w
		}
	}
	return 0, sequenceToolbarWidth(nil, true)
}

func (s *Session) toolbarItemActive(kind toolbarItemKind) bool {
	switch kind {
	case tbBold:
		return s.selectionHasMark(model.MarkBold)
	case tbItalic:
		return s.selectionHasMark(model.MarkItalic)
	case tbUnderline:
		return s.selectionHasMark(model.MarkUnderline)
	case tbStrike:
		return s.selectionHasMark(model.MarkStrike)
	case tbCode:
		return s.selectionHasMark(model.MarkCode)
	case tbPalette:
		fg, bg := s.selectionColors()
		return fg != "default" || bg != "default" || s.toolbarPop == "palette"
	case tbLink:
		return s.selectionLinkURL() != "" || s.toolbarPop == "link"
	case tbComment:
		return s.toolbarPop == "comment"
	case tbReaction:
		return s.toolbarPop == "reactions"
	case tbTextType:
		return s.toolbarPop == "kinds"
	case tbLists:
		return s.toolbarPop == "lists"
	default:
		return false
	}
}

func (s *Session) runToolbarItem(kind toolbarItemKind, fromMore bool) {
	if fromMore && kind != tbTextType && kind != tbPalette && kind != tbLists {
		s.toolbarPop = ""
	}
	switch kind {
	case tbAIRewrite:
		s.OpenInlineAIOnSelection()
	case tbComment:
		s.toggleToolbarPop("comment")
	case tbReaction:
		s.toggleToolbarPop("reactions")
	case tbTextType:
		s.toggleToolbarPop("kinds")
	case tbBold:
		s.ToggleMarkOnSelection(model.MarkBold)
	case tbItalic:
		s.ToggleMarkOnSelection(model.MarkItalic)
	case tbUnderline:
		s.ToggleMarkOnSelection(model.MarkUnderline)
	case tbStrike:
		s.ToggleMarkOnSelection(model.MarkStrike)
	case tbCode:
		s.ToggleMarkOnSelection(model.MarkCode)
	case tbPalette:
		s.toggleToolbarPop("palette")
	case tbLists:
		s.toggleToolbarPop("lists")
	case tbLink:
		if s.toolbarPop != "link" {
			s.linkDraft = s.selectionLinkURL()
			s.linkFocusPending = true
		}
		s.toggleToolbarPop("link")
	}
}

func runeOffsetToUTF16(text string, runeOff int) int {
	if runeOff <= 0 || text == "" {
		return 0
	}
	units := 0
	i := 0
	for _, r := range text {
		if i >= runeOff {
			break
		}
		units += utf16.RuneLen(r)
		i++
	}
	return units
}

func (s *Session) selectionAnchorRect(vp ui.Rect) (rect ui.Rect, isWholeTable bool, visible bool) {
	if s.doc == nil || !s.hasNonEmptySelection() {
		return ui.Rect{}, false, false
	}
	a, b := s.orderedSelection()
	if a.Block < 0 || a.Block >= len(s.doc.Blocks) {
		return ui.Rect{}, false, false
	}
	hasVP := s.scrollRect.W > 0 && s.scrollRect.H > 0
	inVP := func(r ui.Rect) bool {
		if !hasVP {
			return true
		}
		return r.Y+r.H > vp.Y && r.Y < vp.Y+vp.H
	}

	if a.Block == b.Block && s.doc.Blocks[a.Block].Kind == model.BlockTable {
		blk := &s.doc.Blocks[a.Block]
		rlen := s.blockSelectionLen(a.Block)
		if a.Offset == 0 && b.Offset >= rlen {
			if r, ok := s.blockRects[blk.ID]; ok && r.W > 0 {
				tbl := ui.Rect{
					X: r.X + s.leftContentGutter(),
					Y: r.Y,
					W: max(r.W-s.leftContentGutter()-s.rightContentInset(), 40),
					H: r.H,
				}
				return tbl, true, inVP(tbl)
			}
		} else if blk.Table != nil {
			if row, col, _, _, ok := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset); ok {
				if cr, okC := s.tableCellRects[tableCellKey{BlockID: blk.ID, Row: row, Col: col}]; okC && cr.W > 0 {
					return cr, false, inVP(cr)
				}
			}
		}
	}

	var best ui.Rect
	found := false
	sawMeasured := false
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		if a.Block != b.Block && idx == b.Block && b.Offset == 0 && s.blockSelectionLen(idx) > 0 {
			break
		}
		// Shaping offscreen blocks on every frame makes large selections stall.
		if r, ok := s.blockRects[s.doc.Blocks[idx].ID]; ok && r.H > 0 && !inVP(r) {
			sawMeasured = true
			continue
		}
		lo, hi, _, selOk := s.blockSelectionSpanRange(idx)
		if !selOk || hi <= lo {
			continue
		}
		blk := &s.doc.Blocks[idx]
		if tr, ok := s.blockTextRects[blk.ID]; ok && tr.W > 0 && tr.H > 0 {
			sawMeasured = true
			var layout *ui.TextLayout
			var plain string
			if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML {
				plain = blk.Code
				layout = s.cachedShapeBlock(blk.ID, plain, nil, 0, resolvedCodeFont(), 14, theme.WeightRegular, 1.5, tr.W, false)
			} else if blk.Kind != model.BlockTable && blk.Kind != model.BlockImage && blk.Kind != model.BlockDivider {
				plain = blk.PlainText()
				fontSize, weight, lh := blockTextMetrics(blk.Kind)
				layout = s.cachedShapeBlock(blk.ID, plain, blk.Spans, idx, resolvedEditorFont(), fontSize, weight, lh, tr.W, true)
			}
			if layout != nil {
				u0 := runeOffsetToUTF16(plain, lo)
				u1 := runeOffsetToUTF16(plain, hi)
				for _, sr := range layout.SelectionRects(ui.TextInputRange{Start: u0, End: u1}) {
					cand := ui.Rect{
						X: tr.X + sr.X,
						Y: tr.Y + sr.Y,
						W: max(sr.W, 2),
						H: max(sr.H, 16),
					}
					if inVP(cand) && (!found || cand.Y < best.Y) {
						best = cand
						found = true
					}
				}
				if found {
					continue
				}
			}
		}
		if r, ok := s.blockRects[blk.ID]; ok && r.W > 0 {
			sawMeasured = true
			cand := ui.Rect{
				X: r.X + s.leftContentGutter(),
				Y: r.Y,
				W: max(r.W-s.leftContentGutter()-s.rightContentInset(), 24),
				H: max(r.H, 24),
			}
			if inVP(cand) && (!found || cand.Y < best.Y) {
				best = cand
				found = true
			}
		}
	}
	if found {
		return best, false, true
	}
	if sawMeasured && hasVP {
		return ui.Rect{}, false, false
	}
	return ui.Rect{X: vp.X + 120, Y: vp.Y + 60, W: 80, H: 24}, false, true
}

func (s *Session) toolbarDropSpec(btnID string, align appUi.AlignXKind, gap float32, vp ui.Rect) appUi.PlacementSpec {
	var trigger ui.Rect
	var ok bool
	if s.IsSelectionToolbarOpen() {
		trigger, ok = s.overlayBtnBounds(btnID)
		if !ok {
			trigger, ok = s.overlayBtnBounds("tb-more")
		}
	} else {
		switch btnID {
		case "tb-comment":
			trigger, ok = s.overlayBtnBounds("img-tb-comment")
		case "tb-reaction":
			trigger, ok = s.overlayBtnBounds("img-tb-reaction")
		}
		if !ok {
			trigger, ok = s.overlayBtnBounds(btnID)
		}
	}
	if !ok && s.toolbarRect.W > 0 && s.toolbarRect.H > 0 {
		trigger = s.toolbarRect
		ok = true
	}
	if !ok && s.focusedBlock >= 0 && s.focusedBlock < len(s.doc.Blocks) {
		if r, rOk := s.blockRects[s.doc.Blocks[s.focusedBlock].ID]; rOk && r.W > 0 {
			trigger = r
			ok = true
		}
	}
	if !ok {
		trigger = ui.Rect{X: vp.X + 120, Y: vp.Y + 60, W: 28, H: 28}
	}
	return appUi.PlaceBelow(trigger).WithViewport(vp).WithAlignX(appUi.AlignX{Kind: align}).WithGap(gap).WithMargin(8)
}

func (s *Session) renderSelectionToolbar(c *ui.Context) {
	if (!s.IsSelectionToolbarOpen() && s.toolbarPop == "") || s.isMouseDragging() {
		s.clearToolbarBtnRects()
		return
	}
	pal := theme.Ocean()
	vp, winW, winH := s.overlayViewport(c)
	room := max(vp.W-16, 48)
	caps := s.toolbarCapabilities()
	enabled := s.enabledToolbarItems(caps)
	keep, barW := splitForRoom(enabled, room)
	inline := enabled[:keep]
	overflow := enabled[keep:]
	if s.toolbarPop == "more" && len(overflow) == 0 {
		s.toolbarPop = ""
	}

	barOpen := s.IsSelectionToolbarOpen()
	var selRect ui.Rect
	var isWholeTable, selVisible bool
	if barOpen {
		selRect, isWholeTable, selVisible = s.selectionAnchorRect(vp)
		if !selVisible {
			barOpen = false
		}
	}
	if !barOpen {
		s.clearToolbarBtnRects()
	}

	ui.Overlay(c, func() {
		if barOpen {
			align := appUi.AlignPinLeft
			if isWholeTable {
				align = appUi.AlignCenter
			}
			spec := appUi.PlaceAbove(selRect).WithViewport(vp).
				WithAlignX(appUi.AlignX{Kind: align}).WithGap(8).WithMargin(8)
			estW := barW
			if r, ok := s.overlayPanelRects["selection-toolbar"]; ok && r.W > 0 && math.Abs(float64(r.W-barW)) < 24 {
				estW = r.W
			}
			anchorX, anchorY := appUi.ResolveOrigin(spec, estW, 38, winW, winH)

			bar := ui.Row(c.Key("selection-toolbar")).Absolute().Left(anchorX).Top(anchorY).
				Height(38).Padding(4, 6).Gap(2).AlignItems(ui.Center).
				Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.Hairline).
				Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16)).
				Transition(appUi.PopupTransition())
			bar.DrawOver(func(_ *ui.Painter, r ui.Rect) {
				s.toolbarRect = r
				if s.overlayPanelRects == nil {
					s.overlayPanelRects = make(map[string]ui.Rect)
				}
				s.overlayPanelRects["selection-toolbar"] = r
				appUi.RecordBounds("selection-toolbar", r)
			})

			s.clearToolbarBtnRects()
			bar.Children(func() {
				hasPrev := false
				var prevGroup uint8
				for _, item := range inline {
					it := item
					if hasPrev && prevGroup != it.Group {
						toolbarDivider(c)
					}
					hasPrev = true
					prevGroup = it.Group

					if it.Kind == tbAIRewrite {
						aiBtn := ui.ButtonBase(c.Key("tb-ai")).Height(28).PaddingX(8).Gap(4).
							AlignItems(ui.Center).Radius(6).Background(pal.Selected).
							Border(1, pal.Accent).Cursor(ui.CursorPointer).
							Label(it.Label)
						aiBtn.Children(func() {
							icons.Draw(c, icons.Sparkles, 15, pal.MonoIconColors(pal.Accent))
							ui.Text(c, it.Label).FontSize(14).TextColor(pal.Text).SingleLine()
						})
						appUi.Probe(aiBtn, "tb-ai")
						s.recordOverlayBtn("tb-ai", aiBtn)
						if aiBtn.Clicked() {
							s.runToolbarItem(tbAIRewrite, false)
						}
						continue
					}

					active := s.toolbarItemActive(it.Kind)
					if it.HasSubmenu {
						btn := ui.ButtonBase(c.Key(it.ID)).Height(28).MinWidth(41).PaddingX(5).Gap(2).
							Center().Radius(6).Cursor(ui.CursorPointer).Label(it.Label)
						if active {
							btn.Background(pal.Selected)
						} else if btn.Hovered() {
							btn.Background(pal.Hover)
						}
						btn.Children(func() {
							ic := pal.MonoIconColors(pal.TextSecondary)
							if active || btn.Hovered() {
								ic = pal.DuoIconColors(pal.Text)
							}
							icons.Draw(c, it.Icon, 16, ic)
							icons.Draw(c, icons.TbChevron, 10, pal.MonoIconColors(pal.TextSecondary))
						})
						appUi.Probe(btn, it.ID)
						s.recordOverlayBtn(it.ID, btn)
						if btn.Clicked() {
							s.runToolbarItem(it.Kind, false)
						}
					} else {
						btn := appUi.IconButton(c, it.Icon, appUi.IconButtonOptions{
							ID: it.ID, Label: it.Label, Size: 28, IconSize: 16, Active: active,
						})
						s.recordOverlayBtn(it.ID, btn)
						if btn.Clicked() {
							s.runToolbarItem(it.Kind, false)
						}
					}
				}

				if len(overflow) > 0 {
					if hasPrev {
						toolbarDivider(c)
					}
					moreActive := s.toolbarPop == "more"
					for _, ov := range overflow {
						if s.toolbarItemActive(ov.Kind) {
							moreActive = true
							break
						}
					}
					moreBtn := appUi.IconButton(c, icons.More, appUi.IconButtonOptions{
						ID: "tb-more", Label: theme.Tr("更多格式", "More formatting"), Size: 28, IconSize: 16, Active: moreActive,
					})
					s.recordOverlayBtn("tb-more", moreBtn)
					if moreBtn.Clicked() {
						s.toggleToolbarPop("more")
					}
				}
			})
		}

		// Toolbar sub-popovers anchored directly to measured trigger buttons
		switch s.toolbarPop {
		case "more":
			s.renderToolbarMoreMenu(c, vp, winW, winH, overflow)
		case "kinds":
			s.renderToolbarKindsMenu(c, vp, winW, winH, false)
		case "lists":
			s.renderToolbarKindsMenu(c, vp, winW, winH, true)
		case "palette":
			s.renderPalettePanel(c, vp, winW, winH)
		case "link":
			s.renderLinkCardPopover(c, vp, winW, winH)
		case "reactions":
			s.renderReactionPicker(c, vp, winW, winH)
		case "comment":
			s.renderCommentComposer(c, vp, winW, winH)
		}
	})
}

func (s *Session) renderToolbarMoreMenu(c *ui.Context, vp ui.Rect, winW, winH float32, overflow []toolbarItemSpec) {
	if len(overflow) == 0 {
		return
	}
	s.toolbarMoreIndex = clampInt(s.toolbarMoreIndex, 0, len(overflow)-1)
	if c.Shortcut(0, ui.KeyDown) {
		s.toolbarMoreIndex = (s.toolbarMoreIndex + 1) % len(overflow)
	} else if c.Shortcut(0, ui.KeyUp) {
		s.toolbarMoreIndex = (s.toolbarMoreIndex - 1 + len(overflow)) % len(overflow)
	} else if c.Shortcut(0, ui.KeyEnter) {
		s.runToolbarItem(overflow[s.toolbarMoreIndex].Kind, true)
		return
	}
	menuW := float32(228)
	spec := s.toolbarDropSpec("tb-more", appUi.AlignRight, 4, vp)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 304)
	sepCount := 0
	var prevGroup uint8
	for i, it := range overflow {
		if i > 0 && prevGroup != it.Group {
			sepCount++
		}
		prevGroup = it.Group
	}
	estH := min(estimateEditorMenuHeight(len(overflow), sepCount), maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)

	panel := appUi.MenuSurface(ui.Column(c.Key("toolbar-more-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu)).
		HandleInput(func(ev ui.InputEvent) bool {
			return ev.Kind == ui.InputScroll
		})
	appUi.Probe(panel, "toolbar-more-menu")
	s.recordOverlayPanel("toolbar-more-menu", panel)

	panel.Children(func() {
		ui.Scroll(c.Key("toolbar-more-scroll")).FillWidth().MaxHeight(maxH - theme.MenuPadding*2).Children(func() {
			ui.Column(c.Key("toolbar-more-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
				hasPrev := false
				var lastGroup uint8
				for i, item := range overflow {
					it := item
					if hasPrev && lastGroup != it.Group {
						appUi.MenuRow(c, appUi.MenuItemSpec{Separator: true}, false)
					}
					hasPrev = true
					lastGroup = it.Group
					ic := it.Icon
					appUi.MenuRow(c, appUi.MenuItemSpec{
						ID:         fmt.Sprintf("toolbar-more-item-%d", i),
						Label:      it.Label,
						Shortcut:   it.Shortcut,
						Icon:       &ic,
						Checked:    s.toolbarItemActive(it.Kind),
						HasSubmenu: it.HasSubmenu,
						OnClick: func() {
							s.runToolbarItem(it.Kind, true)
						},
					}, i == s.toolbarMoreIndex)
				}
			})
		})
	})
	if panel.PressedOutside() {
		s.toolbarPop = ""
	}
}

func toolbarDivider(c *ui.Context) {
	pal := theme.Ocean()
	ui.Box(c).Width(1).Height(20).MarginX(4).Background(pal.Hairline)
}

func (s *Session) toggleToolbarPop(name string) {
	if s.toolbarPop == name {
		s.toolbarPop = ""
		return
	}
	s.toolbarPop = name
	s.toolbarMoreIndex = 0
	if name == "comment" {
		s.commentFocusPending = true
	}
}

func (s *Session) renderToolbarKindsMenu(c *ui.Context, vp ui.Rect, winW, winH float32, listsOnly bool) {
	btnID := "tb-type"
	if listsOnly {
		btnID = "tb-list"
	}
	menuW := float32(206)
	items := []struct {
		id    string
		kind  model.BlockKind
		icon  icons.Icon
		label string
	}{
		{"kind-p", model.BlockParagraph, icons.Text, theme.Tr("正文", "Text")},
		{"kind-h1", model.BlockHeading1, icons.Heading1, theme.Tr("一级标题", "Heading 1")},
		{"kind-h2", model.BlockHeading2, icons.Heading2, theme.Tr("二级标题", "Heading 2")},
		{"kind-h3", model.BlockHeading3, icons.Heading3, theme.Tr("三级标题", "Heading 3")},
	}
	if listsOnly {
		items = []struct {
			id    string
			kind  model.BlockKind
			icon  icons.Icon
			label string
		}{
			{"list-bullet", model.BlockBullet, icons.ListBullet, theme.Tr("无序列表", "Bulleted list")},
			{"list-num", model.BlockNumbered, icons.ListNumbered, theme.Tr("有序列表", "Numbered list")},
			{"list-todo", model.BlockTodo, icons.Todo, theme.Tr("待办清单", "Checklist")},
		}
	}

	spec := s.toolbarDropSpec(btnID, appUi.AlignLeft, 4, vp)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 304)
	estH := min(estimateEditorMenuHeight(len(items), 0), maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)

	var curKind model.BlockKind
	if s.focusedBlock >= 0 && s.focusedBlock < len(s.doc.Blocks) {
		curKind = s.doc.Blocks[s.focusedBlock].Kind
	}

	panel := appUi.MenuSurface(ui.Column(c.Key("toolbar-kind-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu)).
		HandleInput(func(ev ui.InputEvent) bool {
			return ev.Kind == ui.InputScroll
		})
	appUi.Probe(panel, "toolbar-kind-menu")
	s.recordOverlayPanel("toolbar-kind-menu", panel)
	panel.Children(func() {
		ui.Scroll(c.Key("toolbar-kind-scroll")).FillWidth().MaxHeight(maxH - theme.MenuPadding*2).Children(func() {
			ui.Column(c.Key("toolbar-kind-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
				for _, it := range items {
					ic := it.icon
					appUi.MenuRow(c, appUi.MenuItemSpec{
						ID:      it.id,
						Label:   it.label,
						Icon:    &ic,
						Checked: curKind == it.kind,
						OnClick: func() {
							s.SetSelectionBlockKind(it.kind, listsOnly)
							s.toolbarPop = ""
						},
					}, false)
				}
			})
		})
	})
	if panel.PressedOutside() {
		s.toolbarPop = ""
	}
}

func (s *Session) renderPalettePanel(c *ui.Context, vp ui.Rect, winW, winH float32) {
	pal := theme.Ocean()
	dark := theme.IsDark()
	menuW := float32(196)
	spec := s.toolbarDropSpec("tb-palette", appUi.AlignLeft, 4, vp)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 304)
	_, measH := s.measuredOrEstSize("palette-panel", menuW, 296)
	estH := min(measH, maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)
	curFG, curBG := s.selectionColors()

	panel := appUi.MenuSurface(ui.Column(c.Key("palette-panel")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu)).
		HandleInput(func(ev ui.InputEvent) bool {
			return ev.Kind == ui.InputScroll
		})
	appUi.Probe(panel, "palette-panel")
	s.recordOverlayPanel("palette-panel", panel)

	panel.Children(func() {
		ui.Scroll(c.Key("palette-scroll")).FillWidth().MaxHeight(maxH - theme.MenuPadding*2).Children(func() {
			ui.Column(c.Key("palette-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
				ui.Text(c, theme.Tr("文字颜色", "Text color")).FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).
					TextColor(pal.MenuMuted).Padding(4, 10)
				swatches := theme.FormatPalette(dark)
				limit := min(len(swatches), 6)
				for _, sw := range swatches[:limit] {
					name := sw.Name
					lbl := sw.Zh
					if theme.IsEn() {
						lbl = sw.En
					}
					row := ui.ButtonBase(c.Key("fg-sw-" + name)).FillWidth().Height(theme.MenuItemHeight).PaddingX(10).Gap(8).
						AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Label(lbl)
					if curFG == name {
						row.Background(pal.MenuChecked)
					} else if row.Hovered() {
						row.Background(pal.MenuHover)
					}
					row.Children(func() {
						ui.Box(c).Size(16, 16).Radius(theme.RadiusMicroSM).Background(sw.FG).Border(1, pal.Hairline)
						ui.Text(c, lbl).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).SingleLine()
						if curFG == name {
							icons.Draw(c, icons.Check, 16, pal.MonoIconColors(pal.Text))
						}
					})
					appUi.Probe(row, "fg-sw-"+name)
					if row.Clicked() {
						s.ApplyColorOnSelection(&name, nil)
						s.toolbarPop = ""
					}
				}

				ui.Box(c).FillWidth().Height(1).MarginY(4).Background(pal.MenuSeparator)
				ui.Text(c, theme.Tr("高亮背景", "Highlight")).FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).
					TextColor(pal.MenuMuted).Padding(4, 10)

				for _, bgName := range []string{"default", "yellow"} {
					lbl := theme.Tr("无高亮", "None")
					swatchCol := pal.Paper
					if bgName == "yellow" {
						lbl = theme.Tr("黄色", "Yellow")
						if cVal, ok := theme.ResolveNamedBG("yellow", dark); ok {
							swatchCol = cVal
						}
					}
					row := ui.ButtonBase(c.Key("bg-sw-" + bgName)).FillWidth().Height(theme.MenuItemHeight).PaddingX(10).Gap(8).
						AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Label(lbl)
					if curBG == bgName {
						row.Background(pal.MenuChecked)
					} else if row.Hovered() {
						row.Background(pal.MenuHover)
					}
					row.Children(func() {
						ui.Box(c).Size(16, 16).Radius(theme.RadiusMicroSM).Background(swatchCol).Border(1, pal.BtnBorder)
						ui.Text(c, lbl).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).SingleLine()
						if curBG == bgName {
							icons.Draw(c, icons.Check, 16, pal.MonoIconColors(pal.Text))
						}
					})
					appUi.Probe(row, "bg-sw-"+bgName)
					if row.Clicked() {
						bg := bgName
						s.ApplyColorOnSelection(nil, &bg)
						s.toolbarPop = ""
					}
				}
			})
		})
	})
	if panel.PressedOutside() {
		s.toolbarPop = ""
	}
}

func (s *Session) renderLinkCardPopover(c *ui.Context, vp ui.Rect, winW, winH float32) {
	pal := theme.Ocean()
	hasLink := s.selectionLinkURL() != ""
	estW := float32(320)
	if hasLink {
		estW = 354
	}
	cardW := min(estW, max(vp.W-16, 220))
	_, cardH := s.measuredOrEstSize("format-link-card", cardW, 48)
	spec := s.toolbarDropSpec("tb-link", appUi.AlignLeft, 4, vp)
	x, y := appUi.ResolveOrigin(spec, cardW, cardH, winW, winH)

	card := ui.Row(c.Key("format-link-card")).Absolute().Left(x).Top(y).Width(cardW).
		Padding(8, 10).Gap(6).AlignItems(ui.Center).
		Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.MenuBorder.Alpha(0.6)).
		Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.06)).
		Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.08)).
		Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.12)).
		Transition(appUi.PopupTransition())
	appUi.Probe(card, "format-link-card")
	s.recordOverlayPanel("format-link-card", card)

	card.Children(func() {
		in := ui.TextInputBase(c.Key("link-input"), &s.linkDraft).Grow(1).Height(28).
			PaddingX(8).Radius(6).Border(1, pal.BtnBorder).FontSize(theme.FontBody).
			TextColor(pal.Text).Placeholder("https://...").Label("link-url").
			Bind(&s.linkInputHandle)
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Key == ui.KeyEscape {
				s.toolbarPop = ""
				return true
			}
			return false
		})
		if s.linkFocusPending {
			in.Focus()
			s.linkFocusPending = false
		}
		if in.Submitted() {
			s.ApplyLinkOnSelection(s.linkDraft)
			s.toolbarPop = ""
		}

		applyBtn := appUi.ActionButton(c, "link-apply", theme.Tr("应用", "Apply"), nil, appUi.ButtonPrimary, false)
		if applyBtn.Clicked() {
			s.ApplyLinkOnSelection(s.linkDraft)
			s.toolbarPop = ""
		}
		if hasLink {
			rmBtn := appUi.IconButton(c, icons.LinkOff, appUi.IconButtonOptions{
				ID: "link-remove", Label: theme.Tr("移除链接", "Remove link"), Size: 28, IconSize: 15, Danger: true,
			})
			if rmBtn.Clicked() {
				s.ApplyLinkOnSelection("")
				s.toolbarPop = ""
			}
		}
	})
	if card.PressedOutside() {
		s.toolbarPop = ""
	}
}

var reactionEmojis = []string{"👍", "❤️", "🎉", "🤔", "👀", "🔥", "✅", "💡"}

func (s *Session) renderReactionPicker(c *ui.Context, vp ui.Rect, winW, winH float32) {
	pal := theme.Ocean()
	w := min(float32(268), max(vp.W-16, 200))
	w, h := s.measuredOrEstSize("annotate-reactions", w, 42)
	spec := s.toolbarDropSpec("tb-reaction", appUi.AlignCenter, 6, vp)
	x, y := appUi.ResolveOrigin(spec, w, h, winW, winH)

	bar := appUi.MenuSurface(ui.Row(c.Key("annotate-reactions")).Absolute().Left(x).Top(y)).
		Padding(6, 8).Gap(4).AlignItems(ui.Center)
	appUi.Probe(bar, "annotate-reactions")
	s.recordOverlayPanel("annotate-reactions", bar)

	bar.Children(func() {
		for idx, emoji := range reactionEmojis {
			id := fmt.Sprintf("pick-emoji-%d", idx)
			btn := ui.ButtonBase(c.Key(id)).Size(28, 28).Center().Radius(theme.RadiusControl).
				Cursor(ui.CursorPointer).Label(emoji)
			if btn.Hovered() {
				btn.Background(pal.Hover)
			}
			btn.Children(func() {
				ui.Text(c, emoji).FontSize(15)
			})
			appUi.Probe(btn, id)
			if btn.Clicked() {
				s.AddReaction(emoji)
				s.toolbarPop = ""
			}
		}
	})
	if bar.PressedOutside() {
		s.toolbarPop = ""
	}
}

func (s *Session) renderCommentComposer(c *ui.Context, vp ui.Rect, winW, winH float32) {
	pal := theme.Ocean()
	w := min(float32(260), max(vp.W-16, 200))
	_, h := s.measuredOrEstSize("annotate-composer", w, 44)
	spec := s.toolbarDropSpec("tb-comment", appUi.AlignLeft, 6, vp)
	x, y := appUi.ResolveOrigin(spec, w, h, winW, winH)

	card := ui.Row(c.Key("annotate-composer")).Absolute().Left(x).Top(y).Width(w).
		Padding(6, 10).Gap(8).AlignItems(ui.Center).
		Radius(10).Background(pal.Menu).Border(1, pal.Accent).
		Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12)).
		Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.08)).
		Transition(appUi.PopupTransition())
	appUi.Probe(card, "annotate-composer")
	s.recordOverlayPanel("annotate-composer", card)

	card.Children(func() {
		ui.Box(c).Size(8, 8).Shrink(0).Radius(4).Background(ui.Hex("#f5c84c"))
		in := ui.TextInputBase(c.Key("comment-input"), &s.commentDraft).Grow(1).Height(28).
			Padding(0).FontSize(theme.FontBody).TextColor(pal.Text).CaretColor(pal.Accent).
			Placeholder(theme.Tr("添加批注…", "Add annotation…")).Label("annotation-note").
			Bind(&s.commentHandle)
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Key == ui.KeyEscape {
				s.toolbarPop = ""
				return true
			}
			return false
		})
		if s.commentFocusPending {
			in.Focus()
			s.commentFocusPending = false
		}
		submit := func() {
			if strings.TrimSpace(s.commentDraft) != "" {
				s.AddAnnotation(s.commentDraft)
				s.commentDraft = ""
				s.toolbarPop = ""
			}
		}
		if in.Submitted() {
			submit()
		}
		hasText := strings.TrimSpace(s.commentDraft) != ""
		btnBG := pal.BtnBorder
		if hasText {
			btnBG = pal.Accent
		}
		btn := ui.ButtonBase(c.Key("annotate-submit")).Size(26, 26).Shrink(0).Center().
			Radius(13).Background(btnBG).Disabled(!hasText).Cursor(ui.CursorPointer).
			Label(theme.Tr("保存", "Save"))
		btn.Children(func() {
			icons.Draw(c, icons.ArrowUp, theme.IconInline, pal.MonoIconColors(ui.Hex("#ffffff")))
		})
		appUi.Probe(btn, "annotate-submit")
		if btn.Clicked() {
			submit()
		}
	})
	if card.PressedOutside() {
		s.toolbarPop = ""
	}
}

// Callout 5-state semantic style & dropdown menu (aligned with src/ui/callout_controls.rs)

type calloutStyle struct {
	BG       ui.Color
	Border   ui.Color
	Accent   ui.Color
	IconFill ui.Color
	Text     ui.Color
}

var allCalloutKinds = []model.CalloutKind{
	model.CalloutNote,
	model.CalloutTip,
	model.CalloutImportant,
	model.CalloutWarning,
	model.CalloutCaution,
}

func calloutTone(kind model.CalloutKind, dark bool) calloutStyle {
	switch {
	case kind == model.CalloutTip && !dark:
		return calloutStyle{BG: ui.Hex("#edf7f2"), Border: ui.Hex("#bce3cf"), Accent: ui.Hex("#1f8a5b"), IconFill: ui.Hex("#d3efe1"), Text: ui.Hex("#166541")}
	case kind == model.CalloutImportant && !dark:
		return calloutStyle{BG: ui.Hex("#f3f0fb"), Border: ui.Hex("#d6ccf2"), Accent: ui.Hex("#6e56cf"), IconFill: ui.Hex("#e4ddf8"), Text: ui.Hex("#4c389e")}
	case kind == model.CalloutWarning && !dark:
		return calloutStyle{BG: ui.Hex("#fdf5e8"), Border: ui.Hex("#edd4a8"), Accent: ui.Hex("#b86b11"), IconFill: ui.Hex("#f7e2be"), Text: ui.Hex("#8a4e08")}
	case kind == model.CalloutCaution && !dark:
		return calloutStyle{BG: ui.Hex("#fceeee"), Border: ui.Hex("#f1c4c4"), Accent: ui.Hex("#c93b3b"), IconFill: ui.Hex("#f8dada"), Text: ui.Hex("#962727")}
	case !dark:
		return calloutStyle{BG: ui.Hex("#eef3fc"), Border: ui.Hex("#c9d8f3"), Accent: ui.Hex("#547dd0"), IconFill: ui.Hex("#dce7fa"), Text: ui.Hex("#2c4c8c")}
	case kind == model.CalloutTip && dark:
		return calloutStyle{BG: ui.Hex("#182923"), Border: ui.Hex("#284d3e"), Accent: ui.Hex("#4fd69c"), IconFill: ui.Hex("#203d31"), Text: ui.Hex("#7ce8b8")}
	case kind == model.CalloutImportant && dark:
		return calloutStyle{BG: ui.Hex("#231e34"), Border: ui.Hex("#3f3366"), Accent: ui.Hex("#a78bfa"), IconFill: ui.Hex("#312750"), Text: ui.Hex("#c4b5fd")}
	case kind == model.CalloutWarning && dark:
		return calloutStyle{BG: ui.Hex("#2b2316"), Border: ui.Hex("#544021"), Accent: ui.Hex("#f5a623"), IconFill: ui.Hex("#3d2f19"), Text: ui.Hex("#f8c165")}
	case kind == model.CalloutCaution && dark:
		return calloutStyle{BG: ui.Hex("#2d1b1e"), Border: ui.Hex("#592e34"), Accent: ui.Hex("#f26d6d"), IconFill: ui.Hex("#422328"), Text: ui.Hex("#ff9b9b")}
	default:
		return calloutStyle{BG: ui.Hex("#1b2434"), Border: ui.Hex("#2f4368"), Accent: ui.Hex("#7aa2f7"), IconFill: ui.Hex("#253552"), Text: ui.Hex("#9ab8ff")}
	}
}

func calloutLabel(kind model.CalloutKind) string {
	switch kind {
	case model.CalloutTip:
		return theme.Tr("技巧", "Tip")
	case model.CalloutImportant:
		return theme.Tr("重要", "Important")
	case model.CalloutWarning:
		return theme.Tr("警告", "Warning")
	case model.CalloutCaution:
		return theme.Tr("注意", "Caution")
	default:
		return theme.Tr("提示", "Note")
	}
}

func (s *Session) renderCalloutMenuOverlay(c *ui.Context) {
	if s.calloutMenuBlock == 0 {
		return
	}
	idx := s.blockIndexByID(s.calloutMenuBlock)
	if idx < 0 || s.doc.Blocks[idx].Kind != model.BlockCallout {
		s.calloutMenuBlock = 0
		return
	}
	pal := theme.Ocean()
	dark := theme.IsDark()
	b := &s.doc.Blocks[idx]
	curKind := b.Callout
	if curKind == "" {
		curKind = model.CalloutNote
	}

	trigger, ok := s.overlayBtnBounds(fmt.Sprintf("callout-pill-%d", b.ID))
	if !ok {
		trigger = s.blockRects[b.ID]
	}
	if trigger.W <= 0 {
		trigger = ui.Rect{X: 120, Y: 100, W: 140, H: 28}
	}
	menuW := float32(204)
	vp, winW, winH := s.overlayViewport(c)
	spec := appUi.PlaceBelow(trigger).WithViewport(vp).WithGap(4).WithMargin(8)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 304)
	rawH := theme.MenuPadding*2 + float32(len(allCalloutKinds))*32 + float32(len(allCalloutKinds))*theme.MenuGap + 9 + theme.MenuItemHeight
	estH := min(rawH, maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)

	ui.Overlay(c, func() {
		shell := appUi.MenuSurface(ui.Column(c.Key("callout-kind-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu)).
			HandleInput(func(ev ui.InputEvent) bool {
				return ev.Kind == ui.InputScroll
			})
		appUi.Probe(shell, "callout-kind-menu")
		s.recordOverlayPanel("callout-kind-menu", shell)

		shell.Children(func() {
			ui.Scroll(c.Key("callout-kind-scroll")).FillWidth().MaxHeight(maxH - theme.MenuPadding*2).Children(func() {
				ui.Column(c.Key("callout-kind-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
					for ix, k := range allCalloutKinds {
						kind := k
						st := calloutTone(kind, dark)
						checked := kind == curKind
						highlighted := s.calloutMenuIndex == ix
						row := ui.ButtonBase(c.Key(fmt.Sprintf("callout-opt-%d", ix))).FillWidth().Height(32).
							PaddingX(10).Gap(8).AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer).
							Label(calloutLabel(kind))
						if checked {
							row.Background(pal.Selected).Border(1, pal.Accent)
						} else if highlighted || row.Hovered() {
							row.Background(pal.Hover)
						}
						row.Children(func() {
							ui.Box(c).Size(10, 10).Shrink(0).Radius(5).Background(st.BG).Border(1, st.Accent)
							ui.Text(c, calloutLabel(kind)).FontSize(14).FontWeight(boolToWeight(checked)).TextColor(pal.Text).Grow(1).SingleLine()
							ui.Text(c, string(kind)).Font("monospace").FontSize(12).TextColor(pal.TextSecondary).SingleLine()
						})
						appUi.Probe(row, fmt.Sprintf("callout-opt-%d", ix))
						if row.Clicked() {
							s.setCalloutKind(b.ID, &kind)
						}
					}

					ui.Box(c).FillWidth().Height(1).MarginY(4).Background(pal.MenuSeparator)

					plainHighlighted := s.calloutMenuIndex == len(allCalloutKinds)
					ic := icons.Text
					appUi.MenuRow(c, appUi.MenuItemSpec{
						ID:    "callout-opt-plain",
						Label: theme.Tr("转为普通段落", "Convert to Text"),
						Icon:  &ic,
						OnClick: func() {
							s.setCalloutKind(b.ID, nil)
						},
					}, plainHighlighted)
				})
			})
		})

		if shell.PressedOutside() {
			s.calloutMenuBlock = 0
		}
	})
}

func boolToWeight(on bool) int {
	if on {
		return 600
	}
	return 400
}

// --- 3. Slash & Handle Block Menu Overlay (with internal scroll containment) ---

func (s *Session) renderBlockMenuOverlay(c *ui.Context) {
	if s.blockMenu == nil {
		return
	}
	entries := s.slashMenuEntries()
	if len(entries) == 0 {
		return
	}
	menu := s.blockMenu
	menuW := float32(224)
	vp, winW, winH := s.overlayViewport(c)

	var trigger ui.Rect
	if menu.BlockIdx >= 0 && menu.BlockIdx < len(s.doc.Blocks) {
		bid := s.doc.Blocks[menu.BlockIdx].ID
		if menu.Mode == menuHandle {
			if gr, ok := s.overlayBtnBounds(fmt.Sprintf("block-grip-%d", bid)); ok {
				trigger = gr
			}
		}
		if trigger.W <= 0 {
			if tr, ok := s.blockTextRects[bid]; ok && tr.W > 0 {
				trigger = tr
			} else if r, ok := s.blockRects[bid]; ok && r.W > 0 {
				trigger = ui.Rect{
					X: r.X + s.leftContentGutter(),
					Y: r.Y,
					W: max(r.W-s.leftContentGutter()-s.rightContentInset(), 120),
					H: r.H,
				}
			}
		}
	}
	if trigger.W <= 0 {
		trigger = menu.TriggerRect
	}
	if trigger.W <= 0 {
		trigger = ui.Rect{X: vp.X + 80, Y: vp.Y + 80, W: 200, H: 28}
	}

	spec := appUi.PlaceBelow(trigger).WithViewport(vp).WithGap(4).WithMargin(8).WithReserveHeight(304)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 304)
	itemCount, sepCount := 0, 0
	for _, item := range entries {
		if item.Separator {
			sepCount++
		} else {
			itemCount++
		}
	}
	estH := min(estimateEditorMenuHeight(itemCount, sepCount), maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)

	ui.Overlay(c, func() {
		// Absorb wheel events inside the menu so boundary scrolling never leaks to the document body!
		shell := appUi.MenuSurface(ui.Column(c.Key("block-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu)).
			HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputScroll {
					next := max(0, min(menu.Scroll.Y+ev.DY, menu.Scroll.MaxY))
					menu.Scroll.Y = next
					return true
				}
				return false
			})
		appUi.Probe(shell, "block-menu")
		s.recordOverlayPanel("block-menu", shell)

		shell.Children(func() {
			sc := ui.Scroll(c.Key("block-menu-scroll")).TrackScroll(&menu.Scroll).FillWidth().MaxHeight(maxH - theme.MenuPadding*2)
			sc.Children(func() {
				list := ui.Column(c.Key("block-menu-list")).FillWidth().Gap(theme.MenuGap)
				list.Children(func() {
					for i, item := range entries {
						if item.Separator {
							appUi.MenuRow(c, appUi.MenuItemSpec{Separator: true}, false)
							continue
						}
						cmd := item.Kind
						ic := cmdIcon(cmd)
						row := appUi.MenuRow(c, appUi.MenuItemSpec{
							ID:       fmt.Sprintf("block-menu-item-%d", i),
							Label:    item.Label,
							Shortcut: item.Hint,
							Icon:     &ic,
							Danger:   item.Danger,
							OnClick: func() {
								s.executeBlockCmd(menu.BlockIdx, menu.Mode, cmd)
							},
						}, i == menu.ActiveIndex)
						if i == menu.ActiveIndex && s.focusPending {
							row.ScrollIntoView()
						}
					}
				})
			})
		})

		if shell.PressedOutside() {
			s.blockMenu = nil
		}
	})
}

func cmdIcon(cmd blockCmdKind) icons.Icon {
	switch cmd {
	case cmdTurnParagraph:
		return icons.Text
	case cmdTurnH1:
		return icons.Heading1
	case cmdTurnH2:
		return icons.Heading2
	case cmdTurnH3:
		return icons.Heading3
	case cmdTurnBullet:
		return icons.ListBullet
	case cmdTurnNumbered:
		return icons.ListNumbered
	case cmdTurnTodo:
		return icons.Todo
	case cmdTurnQuote:
		return icons.Quote
	case cmdCallout:
		return icons.Callout
	case cmdCode:
		return icons.Code
	case cmdCodeMermaid:
		return icons.Diagram
	case cmdCodeMath:
		return icons.Math
	case cmdTable:
		return icons.Table
	case cmdDivider:
		return icons.Divider
	case cmdImage:
		return icons.Image
	case cmdAI:
		return icons.Sparkles
	case cmdDuplicate:
		return icons.Duplicate
	case cmdMoveUp:
		return icons.ArrowUp
	case cmdMoveDown:
		return icons.ArrowDown
	case cmdDelete:
		return icons.Trash
	default:
		return icons.Text
	}
}

// --- 4. Code Block Language Picker & Table Menu & Image Sub-Popovers ---

func (s *Session) renderCodeLangMenuOverlay(c *ui.Context) {
	if s.codeLangMenuBlock == 0 {
		return
	}
	idx := s.blockIndexByID(s.codeLangMenuBlock)
	if idx < 0 {
		s.codeLangMenuBlock = 0
		return
	}
	b := &s.doc.Blocks[idx]
	curCanon := canonicalLangTag(b.Language)

	vp, winW, winH := s.overlayViewport(c)
	trigger, ok := s.overlayBtnBounds(fmt.Sprintf("code-lang-%d", b.ID))
	if !ok {
		if r, rOk := s.blockRects[b.ID]; rOk && r.W > 0 {
			trigger = ui.Rect{X: max(r.X+r.W-88, r.X), Y: r.Y + 6, W: 80, H: 22}
		} else {
			trigger = ui.Rect{X: vp.X + 200, Y: vp.Y + 100, W: 80, H: 22}
		}
	}
	menuW := float32(200)
	spec := appUi.PlaceBelow(trigger).WithViewport(vp).WithAlignX(appUi.AlignX{Kind: appUi.AlignRight}).WithGap(6).WithMargin(8).WithReserveHeight(304)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 304)
	estH := min(estimateEditorMenuHeight(len(codeLanguages), 0), maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)

	ui.Overlay(c, func() {
		shell := appUi.MenuSurface(ui.Column(c.Key("code-lang-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu)).
			HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputScroll {
					s.codeLangScroll.Y = max(0, min(s.codeLangScroll.Y+ev.DY, s.codeLangScroll.MaxY))
					return true
				}
				return false
			})
		appUi.Probe(shell, "code-lang-menu")
		s.recordOverlayPanel("code-lang-menu", shell)
		shell.Children(func() {
			ui.Scroll(c.Key("code-lang-scroll")).TrackScroll(&s.codeLangScroll).FillWidth().MaxHeight(maxH - theme.MenuPadding*2).
				Children(func() {
					ui.Column(c.Key("code-lang-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
						for i, lang := range codeLanguages {
							opt := lang
							appUi.MenuRow(c, appUi.MenuItemSpec{
								ID:      fmt.Sprintf("code-lang-item-%d", i),
								Label:   opt.Label,
								Checked: curCanon == opt.Tag,
								OnClick: func() {
									s.pushUndo()
									b.Language = opt.Tag
									b.Dirty = true
									b.CleanSource = ""
									s.codeLangMenuBlock = 0
									s.markDirty()
								},
							}, false)
						}
					})
				})
		})
		if shell.PressedOutside() {
			s.codeLangMenuBlock = 0
		}
	})
}

func (s *Session) renderTableMenuOverlay(c *ui.Context) {
	if s.tableMenuBlock == 0 || s.tableMenuKind == "" {
		return
	}
	idx := s.blockIndexByID(s.tableMenuBlock)
	if idx < 0 {
		s.tableMenuBlock = 0
		s.tableMenuKind = ""
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil {
		return
	}
	id := b.ID
	r := s.tableActiveRow
	col := s.tableActiveCol
	vp, winW, winH := s.overlayViewport(c)
	trigger, ok := s.overlayBtnBounds(fmt.Sprintf("table-more-%d", id))
	if !ok {
		if br, rOk := s.blockRects[id]; rOk && br.W > 0 {
			trigger = ui.Rect{X: max(br.X+br.W-52, br.X), Y: max(br.Y-24, vp.Y), W: 22, H: 22}
		} else {
			trigger = ui.Rect{X: vp.X + 200, Y: vp.Y + 120, W: 22, H: 22}
		}
	}

	rowInsAbove := icons.RowInsertAbove
	rowInsBelow := icons.RowInsertBelow
	rowDel := icons.RowDelete
	colInsLeft := icons.ColInsertLeft
	colInsRight := icons.ColInsertRight
	colDel := icons.ColDelete
	trash := icons.Trash

	entries := []appUi.MenuItemSpec{
		{ID: "tbl-row-above", Label: theme.Tr("上方插入行", "Insert row above"), Icon: &rowInsAbove, OnClick: func() {
			s.tableAddRow(id, r)
			s.tableMenuBlock = 0
			s.tableMenuKind = ""
		}},
		{ID: "tbl-row-below", Label: theme.Tr("下方插入行", "Insert row below"), Icon: &rowInsBelow, OnClick: func() {
			s.tableAddRow(id, r+1)
			s.tableMenuBlock = 0
			s.tableMenuKind = ""
		}},
		{ID: "tbl-row-del", Label: theme.Tr("删除当前行", "Delete row"), Icon: &rowDel, Disabled: len(b.Table.Rows) <= 1, OnClick: func() {
			s.tableDeleteRow(id, r)
			s.tableMenuBlock = 0
			s.tableMenuKind = ""
		}},
		{Separator: true},
		{ID: "tbl-col-left", Label: theme.Tr("左侧插入列", "Insert column left"), Icon: &colInsLeft, OnClick: func() {
			s.tableAddCol(id, col)
			s.tableMenuBlock = 0
			s.tableMenuKind = ""
		}},
		{ID: "tbl-col-right", Label: theme.Tr("右侧插入列", "Insert column right"), Icon: &colInsRight, OnClick: func() {
			s.tableAddCol(id, col+1)
			s.tableMenuBlock = 0
			s.tableMenuKind = ""
		}},
		{ID: "tbl-col-del", Label: theme.Tr("删除当前列", "Delete column"), Icon: &colDel, Disabled: len(b.Table.Rows[0]) <= 1, OnClick: func() {
			s.tableDeleteCol(id, col)
			s.tableMenuBlock = 0
			s.tableMenuKind = ""
		}},
		{Separator: true},
		{ID: "tbl-delete", Label: theme.Tr("删除表格", "Delete table"), Icon: &trash, Danger: true, OnClick: func() {
			s.tableMenuBlock = 0
			s.tableMenuKind = ""
			s.deleteBlock(idx)
		}},
	}

	menuW := float32(224)
	spec := appUi.PlaceBelow(trigger).WithViewport(vp).WithAlignX(appUi.AlignX{Kind: appUi.AlignRight}).WithGap(6).WithMargin(8)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 304)
	estH := min(estimateEditorMenuHeight(7, 2), maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)

	ui.Overlay(c, func() {
		shell := appUi.MenuSurface(ui.Column(c.Key("table-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu)).
			HandleInput(func(ev ui.InputEvent) bool {
				return ev.Kind == ui.InputScroll
			})
		appUi.Probe(shell, "table-menu")
		s.recordOverlayPanel("table-menu", shell)
		shell.Children(func() {
			ui.Scroll(c.Key("table-menu-scroll")).FillWidth().MaxHeight(maxH - theme.MenuPadding*2).Children(func() {
				ui.Column(c.Key("table-menu-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
					for _, it := range entries {
						appUi.MenuRow(c, it, false)
					}
				})
			})
		})
		if shell.PressedOutside() {
			s.tableMenuBlock = 0
			s.tableMenuKind = ""
		}
	})
}

func (s *Session) renderImageSubPopover(c *ui.Context, idx int, b *model.Block) {
	pal := theme.Ocean()
	vp, winW, winH := s.overlayViewport(c)
	subTrigger := func(btnID string) ui.Rect {
		if r, ok := s.overlayBtnBounds(btnID); ok {
			return r
		}
		if r, ok := s.overlayPanelRects["image-toolbar"]; ok && r.W > 0 {
			return r
		}
		if r, ok := s.imageRects[b.ID]; ok && r.W > 0 {
			return r
		}
		return ui.Rect{X: vp.X + 120, Y: vp.Y + 120, W: 28, H: 28}
	}

	switch s.imagePopKind {
	case "size":
		trig := subTrigger("img-size")
		popW, popH := s.measuredOrEstSize("img-size-pop", 206, 40)
		spec := appUi.PlaceBelow(trig).WithViewport(vp).WithAlignX(appUi.AlignX{Kind: appUi.AlignCenter}).WithGap(6).WithMargin(8)
		x, y := appUi.ResolveOrigin(spec, popW, popH, winW, winH)
		pop := ui.Row(c.Key("img-size-pop")).Absolute().Left(x).Top(y).Padding(6, 8).Gap(4).AlignItems(ui.Center).
			Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.MenuBorder.Alpha(0.6)).
			Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.06)).
			Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.08)).
			Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.12)).
			Transition(appUi.PopupTransition())
		appUi.Probe(pop, "img-size-pop")
		s.recordOverlayPanel("img-size-pop", pop)
		pop.Children(func() {
			for _, pct := range []float32{25, 50, 75, 100} {
				p := pct
				lbl := fmt.Sprintf("%.0f%%", p)
				btn := ui.ButtonBase(c.Key("img-pct-" + lbl)).Height(28).PaddingX(8).Center().
					Radius(6).Cursor(ui.CursorPointer).Label(lbl)
				if int(b.ImageWidth) == int(p) || (b.ImageWidth == 0 && p == 100) {
					btn.Background(pal.Selected)
				} else if btn.Hovered() {
					btn.Background(pal.Hover)
				}
				btn.Children(func() {
					ui.Text(c, lbl).FontSize(14).TextColor(pal.Text)
				})
				if btn.Clicked() {
					s.pushUndo()
					b.ImageWidth = p
					b.Dirty = true
					s.imagePopKind = ""
					s.markDirty()
				}
			}
		})
		if pop.PressedOutside() {
			s.imagePopKind = ""
		}
	case "crop":
		trig := subTrigger("img-crop")
		popW, popH := s.measuredOrEstSize("image-crop-popover", 306, 40)
		spec := appUi.PlaceBelow(trig).WithViewport(vp).WithAlignX(appUi.AlignX{Kind: appUi.AlignCenter}).WithGap(6).WithMargin(8)
		x, y := appUi.ResolveOrigin(spec, popW, popH, winW, winH)
		pop := ui.Row(c.Key("image-crop-popover")).Absolute().Left(x).Top(y).Padding(6, 8).Gap(4).AlignItems(ui.Center).
			Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.MenuBorder.Alpha(0.6)).
			Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.06)).
			Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.08)).
			Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.12)).
			Transition(appUi.PopupTransition())
		appUi.Probe(pop, "image-crop-popover")
		s.recordOverlayPanel("image-crop-popover", pop)
		pop.Children(func() {
			presets := []struct {
				label string
				crop  model.ImageCrop
			}{
				{"16:9", model.ImageCrop{Top: 0.1, Bottom: 0.1}},
				{"4:3", model.ImageCrop{Top: 0.06, Bottom: 0.06}},
				{"1:1", model.ImageCrop{Left: 0.15, Right: 0.15}},
				{theme.Tr("裁边 15%", "Trim 15%"), model.ImageCrop{Top: 0.15, Right: 0.15, Bottom: 0.15, Left: 0.15}},
				{theme.Tr("重置", "Reset"), model.ImageCrop{}},
			}
			for i, pr := range presets {
				item := pr
				btn := ui.ButtonBase(c.Key(fmt.Sprintf("img-crop-%d", i))).Height(28).PaddingX(8).Center().
					Radius(6).Cursor(ui.CursorPointer).Label(item.label)
				if btn.Hovered() {
					btn.Background(pal.Hover)
				}
				btn.Children(func() {
					ui.Text(c, item.label).FontSize(14).TextColor(pal.Text)
				})
				if btn.Clicked() {
					s.pushUndo()
					b.ImageCrop = item.crop
					b.Dirty = true
					s.imagePopKind = ""
					s.markDirty()
				}
			}
		})
		if pop.PressedOutside() {
			s.imagePopKind = ""
		}
	}
	_ = idx
}

// --- 5. Left-Margin 4-Level Outline Rail (visible when left margin >= 36px) ---

const outlineMinLeftMargin float32 = 36

func (s *Session) outlineLeftMargin() float32 {
	if s.outlineForceNarrow {
		return 0
	}
	editorW := s.viewportWidth
	if editorW <= 0 {
		editorW = s.editorRect.W
	}
	stdW := theme.EditorStandardWidth + editorHandleWidth + editorRowGap + editorRightInset
	railSlot := float32(0)
	if s.hasVisibleAnnotationRail() && s.canFitWideAnnotationRail() {
		railSlot = annotationRailSlot
	}
	innerW := max(editorW-theme.ScrollGutter, 0)
	colW := min(innerW, stdW+railSlot)
	return max((innerW-colW)*0.5, 0)
}

func (s *Session) renderOutlineRail(c *ui.Context) {
	if s.minimal || s.outlineLeftMargin() < outlineMinLeftMargin {
		s.outlineHovered = false
		return
	}
	items := s.Outline()
	if len(items) == 0 {
		s.outlineHovered = false
		return
	}
	pal := theme.Ocean()

	// Determine active heading and lit section range (matching src/editor/outline.rs).
	anchorIdx := s.focusedBlock
	if s.highlightedBlockID != 0 {
		if hi := s.blockIndexByID(s.highlightedBlockID); hi >= 0 {
			anchorIdx = hi
		}
	}
	activeItemIdx := 0
	for i, it := range items {
		if it.BlockIndex <= anchorIdx {
			activeItemIdx = i
		}
	}
	activeBlockIdx := items[activeItemIdx].BlockIndex
	activeLevel := s.doc.Blocks[activeBlockIdx].Kind.HeadingLevel()
	sectionEnd := len(s.doc.Blocks)
	for j := activeBlockIdx + 1; j < len(s.doc.Blocks); j++ {
		if s.doc.Blocks[j].Kind.IsHeading() && s.doc.Blocks[j].Kind.HeadingLevel() <= activeLevel {
			sectionEnd = j
			break
		}
	}

	ui.Overlay(c, func() {
		vp, _, _ := s.overlayViewport(c)
		leftX := vp.X + 18
		topY := vp.Y + 36
		maxH := min(float32(360), max(vp.Y+vp.H-topY-12, 120))
		wrap := ui.Column(c.Key("outline-rail-wrap")).Absolute().Left(leftX).Top(topY).AlignItems(ui.Start)
		appUi.Probe(wrap, "outline-rail")
		hovered := wrap.Hovered()
		if s.outlinePinnedClosed {
			if !hovered {
				s.outlinePinnedClosed = false
			}
		} else if hovered != s.outlineHovered {
			s.outlineHovered = hovered
			c.Invalidate()
		}

		wrap.Children(func() {
			if s.outlineHovered {
				card := ui.Column(c.Key("outline-popover")).Width(236).MaxHeight(maxH).
					Padding(6).Gap(2).Radius(theme.RadiusSurface).Background(pal.Menu).
					Border(1, pal.Hairline).
					Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.06)).
					Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.08)).
					Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.12)).
					Transition(appUi.PopupTransition())
				appUi.Probe(card, "outline-popover")

				card.Children(func() {
					ui.Scroll(c.Key("outline-scroll")).FillWidth().MaxHeight(maxH - 12).Children(func() {
						for idx, it := range items {
							item := it
							isActive := idx == activeItemIdx
							barLit := isActive || (item.BlockIndex > activeBlockIdx && item.BlockIndex < sectionEnd)
							indent := outlinePopoverIndent(item.Level)
							row := ui.ButtonBase(c.Key(fmt.Sprintf("outline-item-%d", idx))).
								FillWidth().Height(30).Padding(0, 8, 0, indent).Gap(8).
								AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer).Label(item.Title)
							textCol := pal.TextSecondary
							fontW := theme.WeightRegular
							if isActive {
								row.Background(pal.Selected).Border(1, pal.Accent)
								textCol = pal.Text
								fontW = theme.WeightControl
							} else {
								row.Border(1, ui.Transparent)
								if barLit {
									textCol = pal.Text
								}
								if row.Hovered() {
									row.Background(pal.Hover)
									textCol = pal.Text
								}
							}
							row.Children(func() {
								barW := outlineBarWidth(item.Level)
								barCol := pal.OutlineBarDim
								if barLit {
									barCol = pal.OutlineBarLit
								}
								barBox := ui.Box(c).Width(barW).Height(2).Radius(1).Background(barCol).Shrink(0)
								appUi.Probe(barBox, fmt.Sprintf("outline-bar-%d", idx))
								ui.Text(c, item.Title).FontSize(theme.FontSmall).FontWeight(fontW).TextColor(textCol).Grow(1).SingleLine()
							})
							appUi.Probe(row, fmt.Sprintf("outline-item-%d", idx))
							if row.Clicked() {
								s.JumpToBlock(item.BlockIndex)
							}
						}
					})
				})
			} else {
				rail := ui.Column(c.Key("outline-rail")).Padding(4, 8, 4, 0).Gap(7).AlignItems(ui.Start)
				rail.Children(func() {
					for idx, it := range items {
						item := it
						isActive := idx == activeItemIdx
						barLit := isActive || (item.BlockIndex > activeBlockIdx && item.BlockIndex < sectionEnd)
						barBtn := ui.ButtonBase(c.Key(fmt.Sprintf("outline-bar-%d", idx))).
							Height(6).Padding(0, 6, 0, 0).AlignItems(ui.Center).Justify(ui.Start).
							Cursor(ui.CursorPointer).Label(item.Title)
						barBtn.Children(func() {
							barCol := pal.OutlineBarDim
							if barLit {
								barCol = pal.OutlineBarLit
							}
							ui.Box(c).Width(outlineBarWidth(item.Level)).Height(2).Radius(1).Background(barCol)
						})
						appUi.Probe(barBtn, fmt.Sprintf("outline-bar-%d", idx))
						if barBtn.Clicked() {
							s.JumpToBlock(item.BlockIndex)
						}
					}
				})
			}
		})
	})
}

func outlinePopoverIndent(level int) float32 {
	switch {
	case level <= 1:
		return 8
	case level == 2:
		return 18
	default:
		return 28
	}
}

func outlineBarWidth(level int) float32 {
	switch {
	case level <= 1:
		return 20
	case level == 2:
		return 14
	default:
		return 10
	}
}

// --- 6. Annotation Rail (Wide) & Block-Foot Strip (Narrow) ---

const (
	annotationRailWidth float32 = 224
	annotationRailSlot  float32 = annotationRailWidth + 8
)

func (s *Session) canFitWideAnnotationRail() bool {
	stdW := theme.EditorStandardWidth + editorHandleWidth + editorRowGap + editorRightInset
	minW := stdW + annotationRailSlot + theme.ScrollGutter
	editorW := s.viewportWidth
	if editorW <= 0 {
		editorW = s.editorRect.W
	}
	return editorW > 0 && editorW >= minW
}

func isReactionOnlyAnnotation(ann *model.Annotation) bool {
	return ann != nil && ann.Note == "" && ann.RewriteResult == "" && ann.RewritePrompt == "" && ann.AIError == "" && len(ann.Reactions) > 0
}

func reactionPillColors(active, dark bool) (fg, bg, hoverBG, border ui.Color) {
	if dark {
		fg = ui.Hex("#9ec3ff")
		bg = ui.Hex("#212e47")
		hoverBG = ui.Hex("#2d4673")
		border = ui.Hex("#33486e")
		if active {
			bg = ui.Hex("#283d63")
			border = ui.Hex("#6f97e6")
		}
		return
	}
	fg = ui.Hex("#1757ac")
	bg = ui.Hex("#eaf2fd")
	hoverBG = ui.Hex("#d4e5fb")
	border = ui.Hex("#cfe0f8")
	if active {
		bg = ui.Hex("#dceafb")
		border = ui.Hex("#8cb3ec")
	}
	return
}

func (s *Session) renderReactionPill(c *ui.Context, id string, ann *model.Annotation, emoji string, active bool, hoveredOut *string) {
	fg, bg, hoverBG, border := reactionPillColors(active, theme.IsDark())
	pill := ui.ButtonBase(c.Key(id)).Height(24).PaddingX(9).Gap(4).Radius(theme.RadiusPill).
		Background(bg).Border(1, border).AlignItems(ui.Center).Justify(ui.Center).
		Cursor(ui.CursorPointer).Label(emoji)
	if pill.Hovered() {
		pill.Background(hoverBG)
		*hoveredOut = ann.ID
	}
	pill.Children(func() {
		ui.Text(c, emoji).FontSize(theme.FontBody)
		ui.Text(c, "1").FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(fg)
	})
	appUi.Probe(pill, id)
	if pill.Clicked() {
		s.footPickerAnnotID = ""
		s.toggleReactionByID(ann.ID, emoji)
	}
	if !s.readOnly && !s.canFitWideAnnotationRail() && pill.RightClicked() {
		if s.footPickerAnnotID == ann.ID {
			s.footPickerAnnotID = ""
		} else {
			s.footPickerAnnotID = ann.ID
		}
	}
}

func (s *Session) renderAnnotationCardContent(c *ui.Context, ann *model.Annotation, isNarrow bool) {
	pal := theme.Ocean()
	if s.editingAnnotID == ann.ID {
		ui.Row(c).FillWidth().Height(32).PaddingX(8).Radius(6).Background(pal.Shell).
			Border(1, pal.Accent).AlignItems(ui.Center).Children(func() {
			in := ui.TextInputBase(c.Key("edit-ann-"+ann.ID), &s.editingAnnotText).FillWidth().
				Height(30).Padding(0).FontSize(theme.FontBody).TextColor(pal.Text).
				CaretColor(pal.Accent).Bind(&s.editingHandle)
			if in.Submitted() {
				s.updateAnnotationNote(ann.ID, s.editingAnnotText)
			}
		})
		ui.Row(c).FillWidth().Justify(ui.End).Gap(6).Children(func() {
			cancel := appUi.ActionButton(c, "ann-edit-cancel-"+ann.ID, theme.Tr("取消", "Cancel"), nil, appUi.ButtonGhost, false)
			if cancel.Clicked() {
				s.editingAnnotID = ""
			}
			save := appUi.ActionButton(c, "ann-edit-save-"+ann.ID, theme.Tr("保存", "Save"), nil, appUi.ButtonPrimary, false)
			if save.Clicked() {
				s.updateAnnotationNote(ann.ID, s.editingAnnotText)
			}
		})
		return
	}

	if ann.RewriteResult != "" {
		ui.Box(c).FillWidth().Padding(10).Radius(6).Background(pal.Selected).Children(func() {
			ui.Text(c, ann.RewriteResult).FontSize(theme.FontBody).TextColor(pal.SelectionFG)
		})
		if !s.readOnly {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
				acc := ui.ButtonBase(c.Key("ann-accept-"+ann.ID)).Size(30, 30).Shrink(0).Center().
					Radius(6).Background(pal.Accent).Border(1, pal.Accent).
					Cursor(ui.CursorPointer).Label(theme.Tr("同意，替换原文", "Accept and replace"))
				if acc.Hovered() {
					acc.Background(pal.AccentHover)
				}
				acc.Children(func() {
					icons.Draw(c, icons.Check, theme.IconInline, pal.MonoIconColors(pal.AccentText))
				})
				appUi.Probe(acc, "ann-accept-"+ann.ID)
				if acc.Clicked() {
					s.ApplyAnnotationRewrite(ann.ID, ann.RewriteResult)
				}

				ui.Box(c).Grow(1)

				dismiss := appUi.IconButton(c, icons.Trash, appUi.IconButtonOptions{
					ID: "ann-dismiss-" + ann.ID, Label: theme.Tr("忽略", "Dismiss"), Size: 26, IconSize: theme.IconInline, Danger: true,
				})
				if dismiss.Clicked() {
					s.dismissAnnotationRewrite(ann.ID)
				}
			})
		}
		return
	}

	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(8).Children(func() {
		ui.Box(c).Size(8, 8).Margin(9, 0, 0, 0).Shrink(0).Radius(4).Background(ui.Hex("#f5c84c"))
		noteTxt := ann.Note
		if noteTxt == "" && ann.RewritePrompt != "" {
			noteTxt = ann.RewritePrompt
		}
		ui.Text(c, noteTxt).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1)
		if !s.readOnly {
			if ann.RewritePrompt != "" {
				acc := appUi.IconButton(c, icons.Check, appUi.IconButtonOptions{
					ID: "annotation-accept-" + ann.ID, Label: theme.Tr("应用", "Apply"), Size: 26, IconSize: theme.IconInline,
				})
				if acc.Clicked() {
					s.ApplyAnnotationRewrite(ann.ID, ann.RewritePrompt)
				}
			}
			editBtn := appUi.IconButton(c, icons.Pencil, appUi.IconButtonOptions{
				ID: "annotation-edit-" + ann.ID, Label: theme.Tr("编辑批注", "Edit annotation"), Size: 26, IconSize: theme.IconInline,
			})
			if editBtn.Clicked() {
				s.editingAnnotID = ann.ID
				s.editingAnnotText = ann.Note
			}
			delBtn := appUi.IconButton(c, icons.Trash, appUi.IconButtonOptions{
				ID: "annotation-delete-" + ann.ID, Label: theme.Tr("删除批注", "Delete annotation"), Size: 26, IconSize: theme.IconInline, Danger: true,
			})
			if delBtn.Clicked() {
				s.deleteAnnotationByID(ann.ID)
			}
		}
	})
	_ = isNarrow
}

func estimateAnnotationRowMetrics(ann *model.Annotation, isEditing bool) (height, stepGap float32) {
	if isReactionOnlyAnnotation(ann) {
		return 24, 4
	}
	stepGap = 12
	switch {
	case isEditing:
		height = 88
	case ann.RewriteResult != "":
		height = 96
	default:
		lines := max((len([]rune(ann.Note))+13)/14, 1)
		height = 46 + float32(lines-1)*20
	}
	if len(ann.Reactions) > 0 {
		height += 28
	}
	return height, stepGap
}

func (s *Session) renderAnnotationRail(c *ui.Context) {
	if s.minimal || !s.hasVisibleAnnotationRail() || !s.canFitWideAnnotationRail() {
		return
	}
	pal := theme.Ocean()
	editorW := s.viewportWidth
	if editorW <= 0 {
		editorW = s.editorRect.W
	}
	stdW := theme.EditorStandardWidth + editorHandleWidth + editorRowGap + editorRightInset
	bodyRightX := s.editorRect.X + (editorW+stdW)*0.5
	editorRight := s.editorRect.X + editorW
	railLeft := min(bodyRightX+8, editorRight-12-annotationRailWidth)
	railTop := s.editorRect.Y
	railHeight := max(s.editorRect.H, s.viewportHeight-railTop)

	type railEntry struct {
		ann     *model.Annotation
		targetY float32
		height  float32
		gap     float32
		order   int
	}
	var rows []railEntry
	for i, ann := range s.annotations {
		if !isAnnotationVisible(ann) {
			continue
		}
		targetY := float32(24)
		if r, ok := s.blockRects[ann.StartBlockID]; ok && r.H > 0 {
			targetY = max(r.Y-railTop, 24)
		}
		h, g := estimateAnnotationRowMetrics(ann, s.editingAnnotID == ann.ID)
		if mh, ok := s.annotRowHeights[ann.ID]; ok && mh > 0 {
			h = mh
		}
		rows = append(rows, railEntry{ann: ann, targetY: targetY, height: h, gap: g, order: i})
	}
	if len(rows) == 0 {
		return
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].targetY < rows[j-1].targetY; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}

	nextHovered := ""
	ui.Overlay(c, func() {
		rail := ui.Box(c.Key("annotation-rail")).Absolute().Left(railLeft).Top(railTop).
			Width(annotationRailWidth).Height(railHeight).PassThrough()
		appUi.Probe(rail, "annotation-rail")
		rail.Children(func() {
			bottom := float32(12)
			for _, row := range rows {
				ann := row.ann
				top := max(row.targetY, bottom+row.gap)
				bottom = top + row.height
				isActive := s.activeAnnotID == ann.ID
				isHighlighted := isActive || s.hoveredAnnotID == ann.ID

				slot := ui.Column(c.Key("annotation-wrap-" + ann.ID)).Absolute().Top(top).Left(0).
					Width(annotationRailWidth).Gap(4)
				slot.DrawOver(func(_ *ui.Painter, r ui.Rect) {
					if r.H > 0 {
						if s.annotRowHeights == nil {
							s.annotRowHeights = make(map[string]float32)
						}
						if prev, ok := s.annotRowHeights[ann.ID]; !ok || math.Abs(float64(prev-r.H)) > 1 {
							s.annotRowHeights[ann.ID] = r.H
							c.Invalidate()
						}
					}
				})
				slot.Children(func() {
					if isReactionOnlyAnnotation(ann) {
						ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(6).Children(func() {
							for rIdx, em := range ann.Reactions {
								s.renderReactionPill(c, fmt.Sprintf("reaction-pill-%s-%d", ann.ID, rIdx), ann, em, isHighlighted, &nextHovered)
							}
						})
						return
					}

					borderCol := pal.Hairline
					if isActive || s.editingAnnotID == ann.ID {
						borderCol = pal.Accent
					}
					card := ui.Column(c.Key("annotation-card-"+ann.ID)).FillWidth().
						Padding(10, 12).Gap(10).Radius(10).Background(pal.Menu).
						Border(1, borderCol)
					if card.Hovered() {
						nextHovered = ann.ID
					}
					appUi.Probe(card, "annotation-card-"+ann.ID)
					card.Children(func() {
						s.renderAnnotationCardContent(c, ann, false)
					})
					if card.Clicked() {
						if s.activeAnnotID == ann.ID {
							s.activeAnnotID = ""
						} else {
							s.activeAnnotID = ann.ID
						}
					}

					if len(ann.Reactions) > 0 {
						ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(6).Children(func() {
							for rIdx, em := range ann.Reactions {
								s.renderReactionPill(c, fmt.Sprintf("reaction-pill-%s-%d", ann.ID, rIdx), ann, em, isHighlighted, &nextHovered)
							}
						})
					}
				})
			}
		})
	})
	if nextHovered != s.hoveredAnnotID {
		s.hoveredAnnotID = nextHovered
		c.Invalidate()
	}
}

func (s *Session) visibleFootAnchorIndex(ann *model.Annotation) int {
	if s.doc == nil || len(s.doc.Blocks) == 0 || ann == nil {
		return -1
	}
	startIdx := s.blockIndexByID(ann.StartBlockID)
	endIdx := s.blockIndexByID(ann.EndBlockID)
	anchor := max(startIdx, endIdx)
	if anchor < 0 {
		for k := len(s.doc.Blocks) - 1; k >= 0; k-- {
			if !s.isBlockHiddenByFold(k) {
				return k
			}
		}
		return 0
	}
	if s.isBlockHiddenByFold(anchor) {
		for k := anchor - 1; k >= 0; k-- {
			if !s.isBlockHiddenByFold(k) {
				return k
			}
		}
	}
	return anchor
}

func (s *Session) renderBlockAnnotationsFoot(c *ui.Context, idx int, b *model.Block) {
	if len(s.annotations) == 0 || s.canFitWideAnnotationRail() {
		return
	}
	var anchored []*model.Annotation
	for _, ann := range s.annotations {
		if !isAnnotationVisible(ann) {
			continue
		}
		if s.visibleFootAnchorIndex(ann) == idx {
			anchored = append(anchored, ann)
		}
	}
	if len(anchored) == 0 {
		return
	}
	pal := theme.Ocean()
	ownedHover := false
	for _, ann := range anchored {
		if ann.ID == s.hoveredAnnotID {
			ownedHover = true
			break
		}
	}
	nextHovered := ""
	if !ownedHover {
		nextHovered = s.hoveredAnnotID
	}

	botPad := float32(2)
	if b.Kind == model.BlockTable {
		botPad = 10
	}
	minChipW := min(float32(200), max(s.viewportWidth-48, 120))

	strip := ui.Column(c.Key(fmt.Sprintf("annotate-foot-%d", b.ID))).FillWidth().
		Padding(6, 0, botPad, 0).Gap(6).AlignItems(ui.Start)
	strip.Children(func() {
		// Reaction pills row
		var hasReactions bool
		var pickerTarget *model.Annotation
		for _, ann := range anchored {
			if len(ann.Reactions) > 0 {
				hasReactions = true
			}
			if s.footPickerAnnotID != "" && ann.ID == s.footPickerAnnotID {
				pickerTarget = ann
			}
		}
		if hasReactions {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(6).Children(func() {
				for _, ann := range anchored {
					if len(ann.Reactions) == 0 {
						continue
					}
					active := s.activeAnnotID == ann.ID || s.hoveredAnnotID == ann.ID || s.footPickerAnnotID == ann.ID
					s.renderReactionPill(c, fmt.Sprintf("annotation-marker-%s", ann.ID), ann, ann.Reactions[0], active, &nextHovered)
				}
			})
		}
		if pickerTarget != nil && !s.readOnly {
			targetAnn := pickerTarget
			picker := appUi.MenuSurface(ui.Row(c.Key("foot-reaction-picker-"+targetAnn.ID))).
				Padding(4, 6).Gap(4).AlignItems(ui.Center)
			appUi.Probe(picker, "foot-reaction-picker")
			picker.Children(func() {
				for eIdx, em := range reactionEmojis {
					emoji := em
					btnID := fmt.Sprintf("foot-pick-emoji-%s-%d", targetAnn.ID, eIdx)
					selected := slices.Contains(targetAnn.Reactions, emoji)
					btn := ui.ButtonBase(c.Key(btnID)).Size(28, 28).Center().Radius(theme.RadiusControl).
						Cursor(ui.CursorPointer).Label(emoji)
					if selected {
						btn.Background(pal.Selected).Border(1, pal.Accent)
					} else if btn.Hovered() {
						btn.Background(pal.Hover)
					}
					btn.Children(func() {
						ui.Text(c, emoji).FontSize(15)
					})
					appUi.Probe(btn, btnID)
					if btn.Clicked() {
						s.footPickerAnnotID = ""
						s.toggleReactionByID(targetAnn.ID, emoji)
					}
				}
			})
			if picker.PressedOutside() {
				s.footPickerAnnotID = ""
			}
		}

		for _, a := range anchored {
			ann := a
			if isReactionOnlyAnnotation(ann) {
				continue
			}
			isActive := s.activeAnnotID == ann.ID || s.editingAnnotID == ann.ID
			isHighlighted := isActive || s.hoveredAnnotID == ann.ID
			if isActive {
				card := ui.Column(c.Key("annotation-card-"+ann.ID)).FillWidth().MinWidth(minChipW).MaxWidth(340).
					Padding(10, 12).Gap(10).Radius(10).Background(pal.Menu).
					Border(1, pal.Accent)
				if card.Hovered() {
					nextHovered = ann.ID
				}
				appUi.Probe(card, "annotation-card-"+ann.ID)
				card.Children(func() {
					s.renderAnnotationCardContent(c, ann, true)
				})
				if card.Clicked() && s.editingAnnotID != ann.ID {
					s.activeAnnotID = ""
				}
				continue
			}

			// Compact inline capsule with safe minimum width so trailing buttons never clip
			capsuleBG := pal.Sidebar
			capsuleBorder := pal.Hairline
			if ann.RewriteResult != "" {
				capsuleBG = pal.Selected
			}
			if isHighlighted {
				capsuleBorder = pal.Accent
			}
			chip := ui.Row(c.Key("annotation-foot-chip-"+ann.ID)).MinWidth(minChipW).MaxWidth(340).MinHeight(28).
				Padding(4, 4, 4, 10).Gap(6).AlignItems(ui.Center).
				Radius(theme.RadiusControl).Background(capsuleBG).Border(1, capsuleBorder)
			if chip.Hovered() {
				nextHovered = ann.ID
			}
			chip.Children(func() {
				preview := strings.Join(strings.Fields(ann.Note), " ")
				if ann.RewriteResult != "" {
					preview = theme.Tr("建议改写：", "Suggested: ") + strings.Join(strings.Fields(ann.RewriteResult), " ")
				}
				toggle := ui.ButtonBase(c.Key("foot-toggle-" + ann.ID)).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Center).Gap(7).
					Cursor(ui.CursorPointer).Label(preview)
				toggle.Children(func() {
					if ann.RewriteResult != "" {
						icons.Draw(c, icons.TbComment, theme.IconInline, pal.DuoIconColors(pal.Accent))
					} else {
						ui.Box(c).Size(7, 7).Shrink(0).Radius(3.5).Background(ui.Hex("#f5c84c"))
					}
					ui.Text(c, preview).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).Shrink(1).MinWidth(0)
				})
				if toggle.Clicked() {
					s.activeAnnotID = ann.ID
				}
				if !s.readOnly {
					ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(2).Children(func() {
						if ann.RewriteResult != "" {
							acc := ui.ButtonBase(c.Key("ann-accept-"+ann.ID)).Size(22, 22).Shrink(0).Center().
								Radius(theme.RadiusMicroMD).Background(pal.Accent).Cursor(ui.CursorPointer).
								Label(theme.Tr("同意，替换原文", "Accept and replace"))
							acc.Children(func() {
								icons.Draw(c, icons.Check, 13, pal.MonoIconColors(pal.AccentText))
							})
							appUi.Probe(acc, "ann-accept-"+ann.ID)
							if acc.Clicked() {
								s.ApplyAnnotationRewrite(ann.ID, ann.RewriteResult)
							}
						} else {
							editBtn := appUi.IconButton(c, icons.Pencil, appUi.IconButtonOptions{
								ID: "annotation-edit-" + ann.ID, Label: theme.Tr("编辑批注", "Edit annotation"), Size: 22, IconSize: theme.IconInline,
							})
							if editBtn.Clicked() {
								s.activeAnnotID = ann.ID
								s.editingAnnotID = ann.ID
								s.editingAnnotText = ann.Note
							}
							delBtn := appUi.IconButton(c, icons.Trash, appUi.IconButtonOptions{
								ID: "annotation-delete-" + ann.ID, Label: theme.Tr("删除批注", "Delete annotation"), Size: 22, IconSize: theme.IconInline, Danger: true,
							})
							if delBtn.Clicked() {
								s.deleteAnnotationByID(ann.ID)
							}
						}
					})
				}
			})
		}
	})
	if nextHovered != s.hoveredAnnotID {
		s.hoveredAnnotID = nextHovered
		c.Invalidate()
	}
}

// --- 7. AI BatchPlan Diff Review Card ---

func (s *Session) renderPlanDiffCard(c *ui.Context) {
	if s.activePlan == nil {
		return
	}
	plan := s.activePlan
	pal := theme.Ocean()
	vp, _, winH := s.overlayViewport(c)
	cardW := min(max(vp.W-32, 280), 660)
	maxH := min(max(vp.H-48, 220), 460)
	leftX := vp.X + max((vp.W-cardW)*0.5, 8)
	bottomY := max(winH-(vp.Y+vp.H), 0) + 16

	ui.Overlay(c, func() {
		card := ui.Column(c.Key("plan-diff-card")).Absolute().Bottom(bottomY).Left(leftX).
			Width(cardW).MaxHeight(maxH).Padding(0).Gap(0).
			Radius(12).Clip().Background(pal.Paper).Border(1, pal.Accent).
			Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.22)).
			Transition(appUi.PopupTransition())
		appUi.Probe(card, "plan-diff-card")
		s.recordOverlayPanel("plan-diff-card", card)

		card.Children(func() {
			// Header
			ui.Row(c.Key("plan-hdr")).FillWidth().Padding(10, 12).Radius(11, 11, 0, 0).
				Background(pal.Sidebar).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
				title := plan.Summary
				if title == "" {
					title = theme.Tr("AI 修改审阅", "AI Plan Diff Review")
				}
				ui.Row(c).AlignItems(ui.Center).Gap(6).Grow(1).Children(func() {
					icons.Draw(c, icons.Sparkles, 16, pal.MonoIconColors(pal.Accent))
					ui.Text(c, title).FontSize(theme.FontBody).FontWeight(600).TextColor(pal.Text).SingleLine()
				})
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					rej := appUi.ActionButton(c, "plan-reject", theme.Tr("拒绝", "Reject"), nil, appUi.ButtonDanger, false)
					if rej.Clicked() {
						s.rejectActivePlan()
					}
					acc := appUi.ActionButton(c, "plan-accept", theme.Tr("接受选中修改", "Accept Selected"), nil, appUi.ButtonPrimary, false)
					if acc.Clicked() {
						s.acceptActivePlan()
					}
				})
			})

			// Diff body with per-line checkboxes
			ui.Scroll(c.Key("plan-diff-scroll")).TrackScroll(&s.planDiffScroll).FillWidth().Grow(1).
				MaxHeight(maxH-64).Padding(10, 12).Children(func() {
				ui.Column(c.Key("plan-ops-col")).FillWidth().Gap(8).Children(func() {
					codeFont := resolvedCodeFont()
					for opIdx := range plan.Ops {
						op := &plan.Ops[opIdx]
						diff := s.ensurePlanOpLineSelection(op)
						ui.Column(c.Key(fmt.Sprintf("plan-op-%d", opIdx))).FillWidth().
							Radius(10).Border(1, pal.Hairline).Clip().Children(func() {
							ui.Row(c.Key("op-title")).FillWidth().Padding(6, 10).Radius(9, 9, 0, 0).
								Background(pal.Shell).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
								ui.Text(c, op.Page).FontSize(theme.FontBody).FontWeight(600).TextColor(pal.Text).SingleLine()
							})
							selIdx := 0
							for lineIdx, dl := range diff {
								isLast := lineIdx == len(diff)-1
								trimmed := strings.TrimSpace(dl.Text)
								isTable := strings.HasPrefix(trimmed, "|") && strings.Count(trimmed, "|") >= 2
								nextIsTable := false
								if !isLast {
									nt := strings.TrimSpace(diff[lineIdx+1].Text)
									nextIsTable = strings.HasPrefix(nt, "|") && strings.Count(nt, "|") >= 2
								}
								needSep := !isLast && (isTable || nextIsTable || diff[lineIdx+1].Kind != dl.Kind)
								botR := float32(0)
								if isLast {
									botR = 9
								}
								switch dl.Kind {
								case model.DiffEqual:
									row := ui.Row(c.Key(fmt.Sprintf("dl-%d-%d", opIdx, lineIdx))).FillWidth().Padding(3, 10).
										Radius(0, 0, botR, botR).Background(pal.Paper)
									if needSep {
										row.BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
									}
									row.Children(func() {
										ui.Text(c, "  "+dl.Text).Font(codeFont).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
									})
								case model.DiffDelete, model.DiffInsert:
									curSelIdx := selIdx
									selIdx++
									checked := true
									if curSelIdx < len(op.LineSelected) {
										checked = op.LineSelected[curSelIdx]
									}
									bg := pal.DiffAddBG
									fg := pal.DiffAddFG
									prefix := "+ "
									if dl.Kind == model.DiffDelete {
										bg = pal.DiffDelBG
										fg = pal.DiffDelFG
										prefix = "− "
									}
									row := ui.Row(c.Key(fmt.Sprintf("dl-%d-%d", opIdx, lineIdx))).FillWidth().
										Padding(4, 10).Gap(8).AlignItems(ui.Center).Radius(0, 0, botR, botR).Background(bg)
									if needSep {
										row.BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
									}
									row.Children(func() {
										cbID := fmt.Sprintf("plan-diff-line-%d-%d", opIdx, curSelIdx)
										cb := ui.Checkbox(c.Key(cbID), &checked, "").Label(cbID)
										appUi.Probe(cb, cbID)
										if cb.Changed() && curSelIdx < len(op.LineSelected) {
											op.LineSelected[curSelIdx] = checked
										}
										ui.Text(c, prefix+dl.Text).Font(codeFont).FontSize(theme.FontBody).TextColor(fg).Grow(1)
									})
								}
							}
						})
					}
				})
			})
		})
	})
}

func boolColor(cond bool, a, b ui.Color) ui.Color {
	if cond {
		return a
	}
	return b
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
