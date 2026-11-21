package editor

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"block/internal/codepreview"
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
	topY := clampFloat32(vp.Y+theme.FindBarTopOffset, vp.Y+theme.Space8, max(vp.Y+vp.H-theme.FindBarHeight-theme.Space6, vp.Y+theme.Space8))
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

	canReplace := !s.readOnly
	padLeft := theme.Space10
	if canReplace {
		padLeft = theme.Space4
	}

	ui.Overlay(c, func() {
		bar := appUi.Elevate(ui.Column(c.Key("find-bar")).Absolute().Top(topY).Right(rightX).MaxWidth(maxBarW).
			Padding(theme.Space4, theme.Space4, theme.Space4, padLeft).Gap(theme.Space4).
			Radius(theme.RadiusXL).Background(pal.Menu).Border(theme.FindBarBorderWidth, pal.MenuBorder).
			Transition(appUi.PopoverSpringTransition()), theme.ElevationPopover)
		appUi.Probe(bar, "find-bar")
		s.recordOverlayPanel("find-bar", bar)

		bar.Children(func() {
			ui.Row(c.Key("find-row")).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
				if canReplace {
					glyph, label := icons.ChevronRight, theme.Tr("展开替换", "Show replace")
					if s.findReplaceOpen {
						glyph, label = icons.ChevronDown, theme.Tr("收起替换", "Hide replace")
					}
					toggle := appUi.IconButton(c, glyph, appUi.IconButtonOptions{
						ID:       "find-replace-toggle",
						Label:    label,
						Tooltip:  label,
						Size:     theme.HeightControl,
						IconSize: theme.IconInline,
					})
					if toggle.Clicked() {
						s.findReplaceOpen = !s.findReplaceOpen
						if s.findReplaceOpen {
							s.findReplaceFocusPending = true
						} else {
							s.findFocusPending = true
						}
					}
				}
				icons.Draw(c, icons.FindSearch, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))

				in := ui.TextInputBase(c.Key("find-input"), &s.findQuery).Width(inputW).Height(theme.HeightCompact).
					Padding(0).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).CaretColor(pal.Accent).
					Placeholder(theme.Tr("查找…", "Find…")).PlaceholderColor(pal.Placeholder).Label("find-input").
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
					if k.Key == ui.KeyTab && k.Mods == 0 && canReplace {
						s.findReplaceOpen = true
						s.findReplaceFocusPending = true
						c.Invalidate()
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
				ui.Row(c).Width(72).PaddingX(theme.Space6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
					ui.Text(c, countText).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
				})

				ui.Box(c).Width(1).Height(theme.IconChrome).Margin(0, theme.Space2).Background(pal.SidebarBorder)

				prevBtn := appUi.IconButton(c, icons.ChevronUp, appUi.IconButtonOptions{
					ID:       "find-prev",
					Label:    theme.Tr("上一处", "Previous match"),
					Tooltip:  theme.Tr("上一处", "Previous match"),
					Shortcut: formatShortcutDisplay("⇧↩", "Shift+Enter"),
					Size:     theme.HeightControl,
					IconSize: theme.IconInline,
					Disabled: len(s.findMatches) == 0,
				})
				if prevBtn.Clicked() {
					s.FindPrev()
				}

				nextBtn := appUi.IconButton(c, icons.ChevronDown, appUi.IconButtonOptions{
					ID:       "find-next",
					Label:    theme.Tr("下一处", "Next match"),
					Tooltip:  theme.Tr("下一处", "Next match"),
					Shortcut: formatShortcutDisplay("↩", "Enter"),
					Size:     theme.HeightControl,
					IconSize: theme.IconInline,
					Disabled: len(s.findMatches) == 0,
				})
				if nextBtn.Clicked() {
					s.FindNext()
				}

				closeBtn := appUi.IconButton(c, icons.Close, appUi.IconButtonOptions{
					ID:       "find-close",
					Label:    theme.Tr("关闭查找", "Close find"),
					Tooltip:  theme.Tr("关闭查找", "Close find"),
					Shortcut: "Esc",
					Size:     theme.HeightControl,
					IconSize: theme.IconInline,
				})
				if closeBtn.Clicked() {
					s.CloseFind()
				}
			})
			if canReplace && s.findReplaceOpen {
				s.renderFindReplaceRow(c, inputW)
			}
		})
	})
}

func (s *Session) renderFindReplaceRow(c *ui.Context, inputW float32) {
	pal := theme.Ocean()
	ui.Row(c.Key("find-replace-row")).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(theme.HeightControl + theme.Space4 + theme.IconRow).Height(theme.IconRow)

		in := ui.TextInputBase(c.Key("find-replace-input"), &s.findReplace).Width(inputW).Height(theme.HeightCompact).
			Padding(0).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).CaretColor(pal.Accent).
			Placeholder(theme.Tr("替换为…", "Replace…")).PlaceholderColor(pal.Placeholder).Label("find-replace-input").
			Bind(&s.findReplaceHandle)
		appUi.Probe(in, "find-replace-input")
		in.OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Key == ui.KeyEnter && k.Mods == ui.Cmd:
				s.ReplaceAllFind()
				return true
			case k.Key == ui.KeyEnter && k.Mods == 0:
				s.ReplaceCurrentFind()
				return true
			case k.Key == ui.KeyEscape:
				s.CloseFind()
				return true
			case k.Key == ui.KeyTab && k.Mods == ui.Shift:
				s.findFocusPending = true
				c.Invalidate()
				return true
			}
			return false
		})
		if s.findReplaceFocusPending {
			in.Focus()
			s.findReplaceFocusPending = false
		}

		ui.Box(c).Grow(1)
		none := len(s.findMatches) == 0
		one := appUi.ActionButtonWithOptions(c, appUi.ActionButtonOptions{
			ID: "find-replace-one", Label: theme.Tr("替换", "Replace"), Style: appUi.ButtonGhost,
			Height: theme.HeightControl, PaddingX: theme.Space8, RegularWeight: true, Disabled: none,
		})
		appUi.Probe(one, "find-replace-one")
		if one.Clicked() {
			s.ReplaceCurrentFind()
		}
		all := appUi.ActionButtonWithOptions(c, appUi.ActionButtonOptions{
			ID: "find-replace-all", Label: theme.Tr("全部替换", "Replace all"), Style: appUi.ButtonGhost,
			Height: theme.HeightControl, PaddingX: theme.Space8, RegularWeight: true, Disabled: none,
		})
		appUi.Probe(all, "find-replace-all")
		if all.Clicked() {
			s.ReplaceAllFind()
		}
	})
}

// --- 2. Single-Row Adaptive Selection Floating Toolbar & Format Popovers ---

type toolbarItemKind uint8

const (
	tbAIRewrite toolbarItemKind = iota
	tbComment
	tbReaction
	tbBold
	tbItalic
	tbUnderline
	tbStrike
	tbCode
	tbPalette
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
	if !s.aiDisabled && !s.readOnly && s.isMarkdownNote() && s.canReplaceSelection() && s.hasNonEmptySelection() {
		items = append(items, toolbarItemSpec{
			Kind: tbAIRewrite, ID: "tb-ai", Group: 0, Width: theme.HeightCompact,
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
	default:
		return false
	}
}

func (s *Session) runToolbarItem(kind toolbarItemKind, fromMore bool) {
	if fromMore && kind != tbPalette {
		s.toolbarPop = ""
	}
	switch kind {
	case tbAIRewrite:
		s.OpenInlineAIOnSelection()
	case tbComment:
		s.toggleToolbarPop("comment")
	case tbReaction:
		s.toggleToolbarPop("reactions")
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
	return s.selectionSpanRects(vp, false)
}

func (s *Session) selectionComposerAnchorRect(vp ui.Rect) (rect ui.Rect, visible bool) {
	r, _, ok := s.selectionSpanRects(vp, true)
	return r, ok
}

func (s *Session) selectionSpanRects(vp ui.Rect, union bool) (rect ui.Rect, isWholeTable bool, visible bool) {
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
			if r, ok := s.blockRect(blk.ID); ok && r.W > 0 {
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
				if cr, okC := s.tableCellRect(blk.ID, row, col); okC && cr.W > 0 {
					return cr, false, inVP(cr)
				}
			}
		}
	}

	var best ui.Rect
	found := false
	sawMeasured := false
	mergeCand := func(cand ui.Rect) bool {
		if !inVP(cand) {
			return false
		}
		if !found {
			best = cand
			found = true
			return true
		}
		if union {
			x0 := min(best.X, cand.X)
			y0 := min(best.Y, cand.Y)
			x1 := max(best.X+best.W, cand.X+cand.W)
			y1 := max(best.Y+best.H, cand.Y+cand.H)
			best = ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
			return true
		}
		if cand.Y < best.Y {
			best = cand
		}
		return true
	}
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		if a.Block != b.Block && idx == b.Block && b.Offset == 0 && s.blockSelectionLen(idx) > 0 {
			break
		}
		// Shaping offscreen blocks on every frame makes large selections stall.
		if r, ok := s.blockRect(s.doc.Blocks[idx].ID); ok && r.H > 0 && !inVP(r) {
			sawMeasured = true
			continue
		}
		lo, hi, _, selOk := s.blockSelectionSpanRange(idx)
		if !selOk || hi <= lo {
			continue
		}
		blk := &s.doc.Blocks[idx]
		if tr, ok := s.blockTextRect(blk.ID); ok && tr.W > 0 && tr.H > 0 {
			sawMeasured = true
			var layout *ui.TextLayout
			var plain string
			if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML {
				plain = blk.Code
				layout = s.cachedShapeBlock(blk.ID, plain, nil, 0, resolvedCodeFont(), s.codeFontSize(), theme.WeightRegular, 1.5, tr.W, false)
			} else if blk.Kind != model.BlockTable && blk.Kind != model.BlockImage && blk.Kind != model.BlockDivider {
				plain = blk.PlainText()
				fontSize, weight, lh := s.textMetrics(blk.Kind)
				layout = s.cachedShapeBlock(blk.ID, plain, blk.Spans, idx, resolvedEditorFont(), fontSize, weight, lh, tr.W, true)
			}
			if layout != nil {
				u0 := runeOffsetToUTF16(plain, lo)
				u1 := runeOffsetToUTF16(plain, hi)
				foundInBlock := false
				for _, sr := range layout.SelectionRects(ui.TextInputRange{Start: u0, End: u1}) {
					cand := ui.Rect{
						X: tr.X + sr.X,
						Y: tr.Y + sr.Y,
						W: max(sr.W, 2),
						H: max(sr.H, 16),
					}
					if mergeCand(cand) {
						foundInBlock = true
					}
				}
				if foundInBlock {
					continue
				}
			}
		}
		if r, ok := s.blockRect(blk.ID); ok && r.W > 0 {
			sawMeasured = true
			cand := ui.Rect{
				X: r.X + s.leftContentGutter(),
				Y: r.Y,
				W: max(r.W-s.leftContentGutter()-s.rightContentInset(), 24),
				H: max(r.H, 24),
			}
			mergeCand(cand)
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
		if r, rOk := s.blockRect(s.doc.Blocks[s.focusedBlock].ID); rOk && r.W > 0 {
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

			bar := appUi.Elevate(ui.Row(c.Key("selection-toolbar")).Absolute().Left(anchorX).Top(anchorY).
				Height(38).Padding(theme.Space4, theme.Space6).Gap(theme.Space2).AlignItems(ui.Center).
				Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.Hairline).
				Transition(appUi.PopoverSpringTransition()), theme.ElevationPopover)
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
						aiBtn := ui.ButtonBase(c.Key("tb-ai")).Height(theme.HeightCompact).Width(theme.HeightCompact).
							Center().Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
							Label(it.Label)
						aiHovered := aiBtn.Hovered()
						aiPressed := aiBtn.Pressed()
						aiBG := pal.Selected
						if aiHovered && !aiPressed {
							aiBG = pal.Hover
						}
						appUi.AnimateSurfaceColors(aiBtn, aiBG, pal.Accent, true)
						appUi.TactileCard(aiBtn, aiHovered, aiPressed, true, false)
						appUi.RichTooltip(c, aiBtn, appUi.TooltipSpec{
							Pill:      true,
							Placement: appUi.TooltipAbove,
							Rows:      []appUi.TooltipRow{{Trigger: it.Label}},
						})
						aiBtn.Children(func() {
							icons.DrawAnimated(c, icons.Sparkles, theme.IconAction, pal.MonoIconColors(pal.Accent), aiHovered || aiPressed)
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
						btn := ui.ButtonBase(c.Key(it.ID)).Height(theme.HeightCompact).MinWidth(41).PaddingX(theme.Space6).Gap(theme.Space2).
							Center().Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(it.Label)
						btnHovered := btn.Hovered()
						btnPressed := btn.Pressed()
						if !active {
							appUi.RichTooltip(c, btn, appUi.TooltipSpec{
								Pill:      true,
								Placement: appUi.TooltipAbove,
								Rows:      []appUi.TooltipRow{{Trigger: it.Label, Shortcut: it.Shortcut}},
							})
						}
						btnBG := ui.Transparent
						if active || btnPressed {
							btnBG = pal.Selected
						} else if btnHovered {
							btnBG = pal.Hover
						}
						appUi.AnimateSurfaceColors(btn, btnBG, ui.Transparent, false)
						appUi.TactileCard(btn, btnHovered, btnPressed, active, false)
						btn.Children(func() {
							ic := pal.MonoIconColors(pal.TextSecondary)
							if active || btnHovered || btnPressed {
								ic = pal.DuoIconColors(pal.Text)
							}
							icons.DrawAnimated(c, it.Icon, theme.IconAction, ic, active || btnHovered || btnPressed)
							icons.DrawAnimated(c, icons.TbChevron, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary), btnHovered || btnPressed)
						})
						appUi.Probe(btn, it.ID)
						s.recordOverlayBtn(it.ID, btn)
						if it.ID == "tb-comment" && btn.Pressed() {
							s.commentBtnPressed = true
						}
						if btn.Clicked() {
							s.runToolbarItem(it.Kind, false)
						}
					} else {
						btn := appUi.IconButton(c, it.Icon, appUi.IconButtonOptions{
							ID:           it.ID,
							Label:        it.Label,
							Tooltip:      it.Label,
							Shortcut:     it.Shortcut,
							TooltipPill:  true,
							TooltipAbove: true,
							Size:         theme.HeightCompact,
							IconSize:     theme.IconAction,
							Active:       active,
						})
						s.recordOverlayBtn(it.ID, btn)
						if it.ID == "tb-comment" && btn.Pressed() {
							s.commentBtnPressed = true
						}
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
					moreLbl := theme.Tr("更多格式", "More formatting")
					moreTip := moreLbl
					if s.toolbarPop == "more" {
						moreTip = ""
					}
					moreBtn := appUi.IconButton(c, icons.More, appUi.IconButtonOptions{
						ID: "tb-more", Label: moreLbl, Tooltip: moreTip,
						TooltipPill: true, TooltipAbove: true,
						Size: theme.HeightCompact, IconSize: theme.IconAction, Active: moreActive,
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
	menuW := float32(228)
	spec := s.toolbarDropSpec("tb-more", appUi.AlignRight, 4, vp)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 304)
	sepCount := 0
	specs := make([]appUi.MenuItemSpec, 0, len(overflow)+4)
	var prevGroup uint8
	for i, item := range overflow {
		it := item
		if i > 0 && prevGroup != it.Group {
			sepCount++
			specs = append(specs, appUi.MenuItemSpec{Separator: true})
		}
		prevGroup = it.Group
		ic := it.Icon
		specs = append(specs, appUi.MenuItemSpec{
			ID:         fmt.Sprintf("toolbar-more-item-%d", i),
			Label:      it.Label,
			Shortcut:   it.Shortcut,
			Icon:       &ic,
			Checked:    s.toolbarItemActive(it.Kind),
			HasSubmenu: it.HasSubmenu,
			OnClick: func() {
				s.runToolbarItem(it.Kind, true)
			},
		})
	}
	estH := min(estimateEditorMenuHeight(len(overflow), sepCount), maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)

	panel := appUi.MenuSurface(ui.Column(c.Key("toolbar-more-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
	appUi.NavigateMenu(c, panel, specs, &s.toolbarMoreIndex)
	appUi.Probe(panel, "toolbar-more-menu")
	s.recordOverlayPanel("toolbar-more-menu", panel)

	panel.Children(func() {
		appUi.Scroll(c.Key("toolbar-more-scroll")).FillWidth().MaxHeight(appUi.MenuInnerMaxHeight(maxH)).Children(func() {
			ui.Column(c.Key("toolbar-more-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
				for i, sp := range specs {
					appUi.MenuRow(c, sp, i == s.toolbarMoreIndex)
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
	ui.Box(c).Width(1).Height(20).MarginX(theme.Space4).Background(pal.Hairline)
}

func (s *Session) toggleToolbarPop(name string) {
	if s.toolbarPop == name {
		s.toolbarPop = ""
		return
	}
	s.toolbarPop = name
	s.toolbarMoreIndex = -1
	if name == "comment" {
		s.flushAnnotationEdit()
		s.commentDraft = ""
		s.commentFocusPending = true
	}
}

type localMenuCursorKey struct{}

func (s *Session) renderPalettePanel(c *ui.Context, vp ui.Rect, winW, winH float32) {
	pal := theme.Ocean()
	dark := theme.IsDark()
	menuW := float32(196)
	spec := s.toolbarDropSpec("tb-palette", appUi.AlignLeft, 4, vp)
	curFG, curBG := s.selectionColors()

	swatches := theme.FormatPalette(dark)
	limit := min(len(swatches), 6)
	bgChoices := []string{"default", "yellow"}
	totalChoices := limit + len(bgChoices)

	rawH := theme.MenuPadding*2 + 2 +
		2*(theme.LineSmall+theme.Space4*2) +
		(1 + theme.Space4*2) +
		float32(totalChoices)*theme.MenuItemHeight +
		float32(totalChoices+2)*theme.MenuGap
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, max(rawH+32, 380))
	_, measH := s.measuredOrEstSize("palette-panel", menuW, rawH)
	estH := min(measH, maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)

	panel := appUi.MenuSurface(ui.Column(c.Key("palette-panel")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
	cur := ui.Local(panel, localMenuCursorKey{}, func() int { return -1 })
	appUi.ObserveMenuPointer(c, panel, cur)
	if totalChoices > 0 {
		down := panel.OverlayShortcut(0, ui.KeyDown)
		right := panel.OverlayShortcut(0, ui.KeyRight)
		up := panel.OverlayShortcut(0, ui.KeyUp)
		left := panel.OverlayShortcut(0, ui.KeyLeft)
		enter := panel.OverlayShortcut(0, ui.KeyEnter)
		switch {
		case down || right:
			*cur = (*cur + 1 + totalChoices) % totalChoices
		case up || left:
			if *cur < 0 {
				*cur = totalChoices - 1
			} else {
				*cur = (*cur - 1 + totalChoices) % totalChoices
			}
		case enter && *cur >= 0 && *cur < totalChoices:
			if *cur < limit {
				name := swatches[*cur].Name
				s.ApplyColorOnSelection(&name, nil)
			} else {
				bg := bgChoices[*cur-limit]
				s.ApplyColorOnSelection(nil, &bg)
			}
			s.toolbarPop = ""
		}
	}
	appUi.Probe(panel, "palette-panel")
	s.recordOverlayPanel("palette-panel", panel)

	panel.Children(func() {
		appUi.Scroll(c.Key("palette-scroll")).FillWidth().MaxHeight(appUi.MenuInnerMaxHeight(maxH)).Children(func() {
			ui.Column(c.Key("palette-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
				ui.Text(c, theme.Tr("文字颜色", "Text color")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
					TextColor(pal.MenuMuted).Padding(theme.Space4, theme.Space10)
				for i, sw := range swatches[:limit] {
					name := sw.Name
					lbl := sw.Zh
					if theme.IsEn() {
						lbl = sw.En
					}
					highlighted := *cur == i
					row := ui.ButtonBase(c.Key("fg-sw-" + name)).FillWidth().Height(theme.MenuItemHeight).PaddingX(theme.Space10).Gap(theme.Space8).
						AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(lbl)
					appUi.RevealOnHighlight(row, highlighted)
					rowHovered := *cur < 0 && row.Hovered()
					rowPressed := row.Pressed()
					rowBG := ui.Transparent
					if curFG == name || rowPressed {
						rowBG = pal.Selected
					} else if highlighted || rowHovered {
						rowBG = pal.MenuHover
					}
					appUi.AnimateSurfaceColors(row, rowBG, ui.Transparent, false)
					row.Children(func() {
						ui.Box(c).Size(theme.IconAction, theme.IconAction).Radius(theme.RadiusXS).Background(sw.FG).Border(1, pal.Hairline)
						ui.Text(c, lbl).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).SingleLine()
						if curFG == name {
							icons.DrawAnimated(c, icons.Check, theme.IconMenuCheck, pal.MonoIconColors(pal.Text), rowHovered || rowPressed || highlighted)
						}
					})
					appUi.Probe(row, "fg-sw-"+name)
					if row.Clicked() {
						s.ApplyColorOnSelection(&name, nil)
						s.toolbarPop = ""
					}
				}

				ui.Box(c).FillWidth().Height(1).MarginY(theme.Space4).Background(pal.MenuSeparator)
				ui.Text(c, theme.Tr("高亮背景", "Highlight")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
					TextColor(pal.MenuMuted).Padding(theme.Space4, theme.Space10)

				for j, bgName := range bgChoices {
					lbl := theme.Tr("无高亮", "None")
					swatchCol := pal.Paper
					if bgName == "yellow" {
						lbl = theme.Tr("黄色", "Yellow")
						if cVal, ok := theme.ResolveNamedBG("yellow", dark); ok {
							swatchCol = cVal
						}
					}
					highlighted := *cur == limit+j
					row := ui.ButtonBase(c.Key("bg-sw-" + bgName)).FillWidth().Height(theme.MenuItemHeight).PaddingX(theme.Space10).Gap(theme.Space8).
						AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(lbl)
					appUi.RevealOnHighlight(row, highlighted)
					rowHovered := *cur < 0 && row.Hovered()
					rowPressed := row.Pressed()
					rowBG := ui.Transparent
					if curBG == bgName || rowPressed {
						rowBG = pal.Selected
					} else if highlighted || rowHovered {
						rowBG = pal.MenuHover
					}
					appUi.AnimateSurfaceColors(row, rowBG, ui.Transparent, false)
					row.Children(func() {
						ui.Box(c).Size(theme.IconAction, theme.IconAction).Radius(theme.RadiusXS).Background(swatchCol).Border(1, pal.BtnBorder)
						ui.Text(c, lbl).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).SingleLine()
						if curBG == bgName {
							icons.DrawAnimated(c, icons.Check, theme.IconMenuCheck, pal.MonoIconColors(pal.Text), rowHovered || rowPressed || highlighted)
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

	card := appUi.Elevate(ui.Row(c.Key("format-link-card")).Absolute().Left(x).Top(y).Width(cardW).
		Padding(theme.Space8, theme.Space10).Gap(theme.Space6).AlignItems(ui.Center).
		Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.MenuBorder.Alpha(0.6)).
		Transition(appUi.PopoverSpringTransition()), theme.ElevationPopover)
	appUi.Probe(card, "format-link-card")
	s.recordOverlayPanel("format-link-card", card)

	card.Children(func() {
		in := ui.TextInputBase(c.Key("link-input"), &s.linkDraft).Grow(1).Height(theme.HeightCompact).
			Padding(0, theme.Space8).Radius(theme.RadiusSM).Border(1, pal.BtnBorder).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			TextColor(pal.Text).Placeholder("https://...").PlaceholderColor(pal.Placeholder).Label("link-url").
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

		chkIc := icons.Check
		applyBtn := appUi.ActionButton(c, "link-apply", theme.Tr("应用", "Apply"), &chkIc, appUi.ButtonPrimary, false)
		if applyBtn.Clicked() {
			s.ApplyLinkOnSelection(s.linkDraft)
			s.toolbarPop = ""
		}
		if hasLink {
			rmLbl := theme.Tr("移除链接", "Remove link")
			rmBtn := appUi.IconButton(c, icons.LinkOff, appUi.IconButtonOptions{
				ID: "link-remove", Label: rmLbl, Tooltip: rmLbl, Size: theme.HeightCompact, IconSize: theme.IconAction, Danger: true,
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
		Padding(theme.Space6, theme.Space8).Gap(theme.Space4).AlignItems(ui.Center)
	appUi.Probe(bar, "annotate-reactions")
	s.recordOverlayPanel("annotate-reactions", bar)

	bar.Children(func() {
		for idx, emoji := range reactionEmojis {
			id := fmt.Sprintf("pick-emoji-%d", idx)
			btn := ui.ButtonBase(c.Key(id)).Size(theme.HeightCompact, theme.HeightCompact).Center().Radius(theme.RadiusMD).
				Cursor(ui.CursorPointer).Label(emoji)
			btnHovered := btn.Hovered()
			btnPressed := btn.Pressed()
			btnBG := ui.Transparent
			if btnPressed {
				btnBG = pal.Selected
			} else if btnHovered {
				btnBG = pal.Hover
			}
			appUi.AnimateSurfaceColors(btn, btnBG, ui.Transparent, false)
			appUi.TactileButton(btn, btnHovered, btnPressed, false)
			btn.Children(func() {
				ui.Text(c, emoji).FontSize(theme.FontTitle)
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

const annotationComposerWidth float32 = 320

func isAnnotationSubmitKey(k ui.TextKey) bool {
	return k.Key == ui.KeyEnter && k.Mods == 0
}

func (s *Session) commentComposerSpec(vp ui.Rect, h float32) (appUi.PlacementSpec, bool) {
	if s.hasNonEmptySelection() {
		selRect, ok := s.selectionComposerAnchorRect(vp)
		if !ok {
			return appUi.PlacementSpec{}, false
		}
		tb := s.toolbarRect
		// If the selection toolbar flipped below the selection (near the top edge of the viewport),
		// stack the composer below the toolbar so it never covers the toolbar or selected text.
		if tb.H > 0 && tb.Y >= selRect.Y+selRect.H-1 {
			return appUi.PlaceBelow(tb).WithViewport(vp).
				WithAlignX(appUi.AlignX{Kind: appUi.AlignPinLeft}).WithGap(theme.Space6).WithMargin(theme.Space8), true
		}
		// Normal case: place directly below the selection.
		if selRect.Y+selRect.H+theme.Space6+h <= vp.Y+vp.H-theme.Space8 {
			return appUi.PlaceBelow(selRect).WithViewport(vp).
				WithAlignX(appUi.AlignX{Kind: appUi.AlignPinLeft}).WithGap(theme.Space6).WithMargin(theme.Space8), true
		}
		// Tight bottom edge: if the toolbar is above the selection, place the composer above the toolbar
		// instead of flipping into the toolbar's slot.
		if tb.H > 0 && tb.Y+tb.H <= selRect.Y+1 {
			return appUi.PlaceAbove(tb).WithViewport(vp).
				WithAlignX(appUi.AlignX{Kind: appUi.AlignPinLeft}).WithGap(theme.Space6).WithMargin(theme.Space8), true
		}
		return appUi.PlaceBelow(selRect).WithViewport(vp).
			WithAlignX(appUi.AlignX{Kind: appUi.AlignPinLeft}).WithGap(theme.Space6).WithMargin(theme.Space8), true
	}
	return s.toolbarDropSpec("tb-comment", appUi.AlignLeft, theme.Space6, vp), true
}

func (s *Session) renderCommentComposer(c *ui.Context, vp ui.Rect, winW, winH float32) {
	pal := theme.Ocean()
	w := min(annotationComposerWidth, max(vp.W-16, 180))
	_, h := s.measuredOrEstSize("annotate-composer", w, 34)
	spec, ok := s.commentComposerSpec(vp, h)
	if !ok {
		s.commentFocusPending = true
		return
	}
	x, y := appUi.ResolveOrigin(spec, w, h, winW, winH)

	card := appUi.FocusedPopoverSurface(ui.Row(c.Key("annotate-composer")).Absolute().Left(x).Top(y).Width(w).
		Padding(theme.Space4, theme.Space6, theme.Space4, theme.Space10).Gap(theme.Space6).AlignItems(ui.End))
	appUi.Probe(card, "annotate-composer")
	s.recordOverlayPanel("annotate-composer", card)

	card.Children(func() {
		submitted := false
		submit := func() {
			if submitted || s.toolbarPop != "comment" {
				return
			}
			if strings.TrimSpace(s.commentDraft) != "" {
				submitted = true
				s.AddAnnotation(s.commentDraft)
				s.commentDraft = ""
				s.toolbarPop = ""
				s.selAnchor = s.selHead
				s.selSyncPending = true
			}
		}
		in := ui.TextAreaBase(c.Key("comment-input"), &s.commentDraft).Grow(1).MinWidth(0).Lines(1, 4).
			Padding(0).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).CaretColor(pal.Accent).
			Placeholder(theme.Tr("添加批注…", "Add annotation…")).PlaceholderColor(pal.Placeholder).Label("annotation-note").
			Bind(&s.commentHandle)
		appUi.AutoHideScrollbars(c, in)
		composing := in.Composing()
		in.OnTextKey(func(k ui.TextKey) bool {
			if composing {
				return false
			}
			switch {
			case k.Key == ui.KeyEscape:
				s.commentDraft = ""
				s.toolbarPop = ""
				return true
			case isAnnotationSubmitKey(k):
				if submitted || s.toolbarPop != "comment" {
					return true
				}
				s.commentDraft = k.Text()
				submit()
				return true
			}
			return false
		})
		if s.commentFocusPending {
			in.Focus()
			s.commentFocusPending = false
		}
		hasText := strings.TrimSpace(s.commentDraft) != ""
		btn := ui.ButtonBase(c.Key("annotate-submit")).Size(theme.HeightMicro, theme.HeightMicro).Shrink(0).Center().
			Radius(theme.RadiusPill).Disabled(!hasText).
			Label(theme.Tr("保存", "Save"))
		if hasText {
			btn.Cursor(ui.CursorPointer)
		}
		btnHovered := btn.Hovered() && hasText
		btnPressed := btn.Pressed() && hasText
		btnBG := ui.Transparent
		iconColor := pal.Placeholder
		if hasText {
			btnBG = pal.Accent
			if btnHovered {
				btnBG = pal.AccentHover
			}
			iconColor = pal.AccentText
		}
		appUi.AnimateSurfaceColors(btn, btnBG, ui.Transparent, false)
		appUi.TactileButton(btn, btnHovered, btnPressed, !hasText)
		btn.Children(func() {
			icons.DrawAnimated(c, icons.ArrowUp, theme.IconInline, pal.MonoIconColors(iconColor), btnHovered || btnPressed)
		})
		appUi.Probe(btn, "annotate-submit")
		if btn.Clicked() {
			submit()
		}
	})
	if card.PressedOutside() && !s.commentBtnPressed {
		s.toolbarPop = ""
	}
}

// Callout 5-state semantic style & dropdown menu (aligned with src/ui/callout_controls.rs)

type calloutStyle = theme.CalloutColors

var allCalloutKinds = []model.CalloutKind{
	model.CalloutNote,
	model.CalloutTip,
	model.CalloutImportant,
	model.CalloutWarning,
	model.CalloutCaution,
}

func calloutTone(kind model.CalloutKind, dark bool) theme.CalloutColors {
	if dark {
		return theme.OceanDark.Callout(kind)
	}
	return theme.OceanLight.Callout(kind)
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
		trigger = s.blockRectValue(b.ID)
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
		shell := appUi.MenuSurface(ui.Column(c.Key("callout-kind-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
		appUi.ObserveMenuPointer(c, shell, &s.calloutMenuIndex)
		total := len(allCalloutKinds) + 1
		down := shell.OverlayShortcut(0, ui.KeyDown)
		right := shell.OverlayShortcut(0, ui.KeyRight)
		up := shell.OverlayShortcut(0, ui.KeyUp)
		left := shell.OverlayShortcut(0, ui.KeyLeft)
		enter := shell.OverlayShortcut(0, ui.KeyEnter)
		switch {
		case down || right:
			s.calloutMenuIndex = (s.calloutMenuIndex + 1 + total) % total
		case up || left:
			if s.calloutMenuIndex < 0 {
				s.calloutMenuIndex = total - 1
			} else {
				s.calloutMenuIndex = (s.calloutMenuIndex - 1 + total) % total
			}
		case enter && s.calloutMenuIndex >= 0 && s.calloutMenuIndex < total:
			if s.calloutMenuIndex < len(allCalloutKinds) {
				k := allCalloutKinds[s.calloutMenuIndex]
				s.setCalloutKind(b.ID, &k)
			} else {
				s.setCalloutKind(b.ID, nil)
			}
			return
		}
		appUi.Probe(shell, "callout-kind-menu")
		s.recordOverlayPanel("callout-kind-menu", shell)

		shell.Children(func() {
			appUi.Scroll(c.Key("callout-kind-scroll")).FillWidth().MaxHeight(appUi.MenuInnerMaxHeight(maxH)).Children(func() {
				ui.Column(c.Key("callout-kind-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
					for ix, k := range allCalloutKinds {
						kind := k
						st := calloutTone(kind, dark)
						checked := kind == curKind
						highlighted := s.calloutMenuIndex == ix
						row := ui.ButtonBase(c.Key(fmt.Sprintf("callout-opt-%d", ix))).FillWidth().Height(theme.HeightControl).
							PaddingX(theme.Space10).Gap(theme.Space8).AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
							Label(calloutLabel(kind))
						appUi.RevealOnHighlight(row, highlighted)
						rowHovered := s.calloutMenuIndex < 0 && row.Hovered()
						rowPressed := row.Pressed()
						rowBG, rowBD := ui.Transparent, ui.Transparent
						if checked {
							rowBG, rowBD = pal.Selected, pal.SelectedBorder
						} else if rowPressed {
							rowBG = pal.Selected
						} else if highlighted || rowHovered {
							rowBG = pal.Hover
						}
						appUi.AnimateSurfaceColors(row, rowBG, rowBD, checked)
						row.Children(func() {
							ui.Box(c).Size(10, 10).Shrink(0).Radius(theme.RadiusXS).Background(st.BG).Border(1, st.Accent)
							ui.Text(c, calloutLabel(kind)).FontSize(theme.FontBody).FontWeight(boolToWeight(checked)).TextColor(pal.Text).Grow(1).SingleLine()
							ui.Text(c, string(kind)).Font("monospace").FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
						})
						appUi.Probe(row, fmt.Sprintf("callout-opt-%d", ix))
						if row.Clicked() {
							s.setCalloutKind(b.ID, &kind)
						}
					}

					ui.Box(c).FillWidth().Height(1).MarginY(theme.Space4).Background(pal.MenuSeparator)

					plainHighlighted := s.calloutMenuIndex == len(allCalloutKinds)
					ic := icons.Text
					appUi.MenuRow(c, appUi.MenuItemSpec{
						ID:            "callout-opt-plain",
						Label:         theme.Tr("转为普通段落", "Convert to Text"),
						Icon:          &ic,
						SuppressHover: s.calloutMenuIndex >= 0,
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
		return theme.WeightTitle
	}
	return theme.WeightRegular
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
	if menu.Mode == menuSlash {
		menuW = 248
	}
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
			if tr, ok := s.blockTextRect(bid); ok && tr.W > 0 {
				trigger = tr
			} else if r, ok := s.blockRect(bid); ok && r.W > 0 {
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
		shell := appUi.MenuSurface(ui.Column(c.Key("block-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
		if menu.Mode == menuHandle && s.focusedBlock >= 0 && s.focusedBlock < len(s.doc.Blocks) {
			fk := s.doc.Blocks[s.focusedBlock].Kind
			if fk == model.BlockCode || fk == model.BlockRawHTML {
				shell.Focusable().AutoFocus()
			}
		}
		svc := c.Services()
		shell.HandleInput(func(ev ui.InputEvent) bool {
			switch ev.Kind {
			case ui.InputPointerMove:
				if !menu.PointerActive || menu.ActiveIndex >= 0 {
					menu.PointerActive = true
					menu.ActiveIndex = -1
					return true
				}
			case ui.InputScroll:
				// 滚轮同样视为鼠标接管：不吞事件以便内部 Scroll 正常滚动，同时让悬停立即接管高亮。
				if !menu.PointerActive || menu.ActiveIndex >= 0 {
					menu.PointerActive = true
					menu.ActiveIndex = -1
					svc.Invalidate()
				}
			}
			return false
		})
		down := shell.OverlayShortcut(0, ui.KeyDown)
		right := shell.OverlayShortcut(0, ui.KeyRight)
		up := shell.OverlayShortcut(0, ui.KeyUp)
		left := shell.OverlayShortcut(0, ui.KeyLeft)
		enter := shell.OverlayShortcut(0, ui.KeyEnter)
		switch {
		case down || right:
			s.stepSlashMenu(true)
		case up || left:
			s.stepSlashMenu(false)
		case enter:
			if s.activateSlashMenu() {
				c.Invalidate()
				return
			}
		}
		appUi.Probe(shell, "block-menu")
		s.recordOverlayPanel("block-menu", shell)

		shell.Children(func() {
			sc := appUi.Scroll(c.Key("block-menu-scroll"), &menu.Scroll).FillWidth().MaxHeight(appUi.MenuInnerMaxHeight(maxH))
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
						shortcut, hint := item.Hint, ""
						if menu.Mode == menuSlash {
							shortcut, hint = "", item.Hint
						}
						row := appUi.MenuRow(c, appUi.MenuItemSpec{
							ID:            fmt.Sprintf("block-menu-item-%d", i),
							Label:         item.Label,
							Shortcut:      shortcut,
							Hint:          hint,
							Icon:          &ic,
							Danger:        item.Danger,
							Disabled:      item.Disabled,
							SuppressHover: !menu.PointerActive,
							OnClick: func() {
								if !item.Disabled {
									s.executeBlockCmd(menu.BlockIdx, menu.Mode, cmd)
								}
							},
						}, !menu.PointerActive && i == menu.ActiveIndex)
						if menu.PointerActive && row.Hovered() && !item.Disabled {
							menu.ActiveIndex = i
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
	curCanon := codepreview.CanonicalLanguage(b.Language)

	vp, winW, winH := s.overlayViewport(c)
	trigger, ok := s.overlayBtnBounds(fmt.Sprintf("code-lang-%d", b.ID))
	if !ok {
		if r, rOk := s.blockRect(b.ID); rOk && r.W > 0 {
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

	specs := make([]appUi.MenuItemSpec, 0, len(codeLanguages))
	for i, lang := range codeLanguages {
		opt := lang
		specs = append(specs, appUi.MenuItemSpec{
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
		})
	}

	ui.Overlay(c, func() {
		shell := appUi.MenuSurface(ui.Column(c.Key("code-lang-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu)).
			Focusable().AutoFocus()
		cur := ui.Local(shell, localMenuCursorKey{}, func() int {
			s.codeLangScroll = ui.ScrollState{}
			return -1
		})
		appUi.NavigateMenu(c, shell, specs, cur)
		appUi.Probe(shell, "code-lang-menu")
		s.recordOverlayPanel("code-lang-menu", shell)
		shell.Children(func() {
			appUi.Scroll(c.Key("code-lang-scroll"), &s.codeLangScroll).FillWidth().MaxHeight(appUi.MenuInnerMaxHeight(maxH)).
				Children(func() {
					ui.Column(c.Key("code-lang-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
						for i, sp := range specs {
							appUi.MenuRow(c, sp, i == *cur)
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
	s.renderTableBorderMenuOverlay(c)
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
		if r, ok := s.imageRect(b.ID); ok && r.W > 0 {
			return r
		}
		return ui.Rect{X: vp.X + 120, Y: vp.Y + 120, W: 28, H: 28}
	}

	switch s.imagePopKind {
	case "size":
		trig := subTrigger("img-size")
		popW, popH := s.measuredOrEstSize("img-size-pop", 206, 40)
		spec := appUi.PlaceBelow(trig).WithViewport(vp).WithAlignX(appUi.AlignX{Kind: appUi.AlignCenter}).WithGap(theme.Space6).WithMargin(theme.Space8)
		x, y := appUi.ResolveOrigin(spec, popW, popH, winW, winH)
		pop := appUi.MenuSurface(ui.Row(c.Key("img-size-pop")).Absolute().Left(x).Top(y)).
			Padding(theme.Space6, theme.Space8).Gap(theme.Space4).AlignItems(ui.Center)
		appUi.Probe(pop, "img-size-pop")
		s.recordOverlayPanel("img-size-pop", pop)
		pop.Children(func() {
			for _, pct := range []float32{25, 50, 75, 100} {
				p := pct
				lbl := fmt.Sprintf("%.0f%%", p)
				btn := ui.ButtonBase(c.Key("img-pct-" + lbl)).Height(theme.HeightCompact).PaddingX(theme.Space8).Center().
					Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(lbl)
				btnSelected := int(b.ImageWidth) == int(p) || (b.ImageWidth == 0 && p == 100)
				btnHovered := btn.Hovered()
				btnPressed := btn.Pressed()
				btnBG := ui.Transparent
				if btnSelected || btnPressed {
					btnBG = pal.Selected
				} else if btnHovered {
					btnBG = pal.Hover
				}
				appUi.AnimateSurfaceColors(btn, btnBG, ui.Transparent, false)
				appUi.TactileCard(btn, btnHovered, btnPressed, btnSelected, false)
				btn.Children(func() {
					ui.Text(c, lbl).FontSize(theme.FontBody).TextColor(pal.Text)
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
		spec := appUi.PlaceBelow(trig).WithViewport(vp).WithAlignX(appUi.AlignX{Kind: appUi.AlignCenter}).WithGap(theme.Space6).WithMargin(theme.Space8)
		x, y := appUi.ResolveOrigin(spec, popW, popH, winW, winH)
		pop := appUi.MenuSurface(ui.Row(c.Key("image-crop-popover")).Absolute().Left(x).Top(y)).
			Padding(theme.Space6, theme.Space8).Gap(theme.Space4).AlignItems(ui.Center)
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
				btn := ui.ButtonBase(c.Key(fmt.Sprintf("img-crop-%d", i))).Height(theme.HeightCompact).PaddingX(theme.Space8).Center().
					Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(item.label)
				btnHovered := btn.Hovered()
				btnPressed := btn.Pressed()
				btnBG := ui.Transparent
				if btnPressed {
					btnBG = pal.Selected
				} else if btnHovered {
					btnBG = pal.Hover
				}
				appUi.AnimateSurfaceColors(btn, btnBG, ui.Transparent, false)
				appUi.TactileCard(btn, btnHovered, btnPressed, false, false)
				btn.Children(func() {
					ui.Text(c, item.label).FontSize(theme.FontBody).TextColor(pal.Text)
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

const (
	outlineMinLeftMargin   float32 = 36
	outlineHoverOpenDelay          = theme.OutlineHoverDelayMs * time.Millisecond
	outlineHoverCloseDelay         = theme.OutlineHoverLeaveMs * time.Millisecond
)

func (s *Session) outlineLeftMargin() float32 {
	if s.outlineForceNarrow {
		return 0
	}
	editorW := s.viewportWidth
	if editorW <= 0 {
		editorW = s.editorRect.W
	}
	return max((editorW-theme.ScrollGutter-s.outerFlowWidth())*0.5, 0)
}

func (s *Session) renderOutlineRail(c *ui.Context) {
	if s.minimal || s.outlineLeftMargin() < outlineMinLeftMargin {
		s.outlineHovered = false
		s.outlineHoverEnterAt = time.Time{}
		s.outlineHoverLeaveAt = time.Time{}
		return
	}
	items := s.Outline()
	if len(items) == 0 {
		s.outlineHovered = false
		s.outlineHoverEnterAt = time.Time{}
		s.outlineHoverLeaveAt = time.Time{}
		return
	}
	pal := theme.Ocean()

	// Determine active heading and lit section range (matching src/editor/outline.rs).
	anchorIdx := s.focusedBlock
	if s.jump.block != 0 {
		if hi := s.blockIndexByID(s.jump.block); hi >= 0 {
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

	const top float32 = 36
	vp, _, _ := s.overlayViewport(c)
	maxH := min(float32(360), max(vp.H-top-12, 120))
	wrap := ui.Column(c.Key("outline-rail-wrap")).Absolute().Left(18).Top(top).AlignItems(ui.Start).
		Transition(appUi.DockedPanelTransition(false))
	appUi.Probe(wrap, "outline-rail")
	hovered := wrap.Hovered()
	busy := s.isMouseDragging() || s.dragBlockID != 0
	if s.outlineHovered {
		s.outlineHoverEnterAt = time.Time{}
		if hovered && !busy {
			s.outlineHoverLeaveAt = time.Time{}
		} else if testing.Testing() || busy {
			s.outlineHovered = false
			s.outlineHoverLeaveAt = time.Time{}
			c.Invalidate()
		} else {
			now := c.Now()
			if s.outlineHoverLeaveAt.IsZero() {
				s.outlineHoverLeaveAt = now
			}
			if elapsed := now.Sub(s.outlineHoverLeaveAt); elapsed >= outlineHoverCloseDelay {
				s.outlineHovered = false
				s.outlineHoverLeaveAt = time.Time{}
				c.Invalidate()
			} else {
				c.After(outlineHoverCloseDelay - elapsed + 5*time.Millisecond)
			}
		}
	} else {
		s.outlineHoverLeaveAt = time.Time{}
		if !hovered {
			s.outlinePinnedClosed = false
			s.outlineHoverEnterAt = time.Time{}
		} else if s.outlinePinnedClosed || busy {
			s.outlineHoverEnterAt = time.Time{}
		} else if testing.Testing() {
			s.outlineHovered = true
			c.Invalidate()
		} else {
			now := c.Now()
			if s.outlineHoverEnterAt.IsZero() {
				s.outlineHoverEnterAt = now
			}
			if elapsed := now.Sub(s.outlineHoverEnterAt); elapsed >= outlineHoverOpenDelay {
				s.outlineHovered = true
				s.outlineHoverEnterAt = time.Time{}
				c.Invalidate()
			} else {
				c.After(outlineHoverOpenDelay - elapsed + 5*time.Millisecond)
			}
		}
	}

	wrap.Children(func() {
		if s.outlineHovered {
			card := appUi.Elevate(ui.Column(c.Key("outline-popover")).Width(236).MaxHeight(maxH).
				Radius(theme.RadiusXL).Background(pal.Menu).
				Border(1, pal.MenuBorder).Clip().
				TransformOrigin(0, 0).
				Transition(appUi.OutlinePopoverTransition()), theme.ElevationPopover)
			appUi.Probe(card, "outline-popover")

			card.Children(func() {
				appUi.Scroll(c.Key("outline-scroll")).FillWidth().MaxHeight(maxH - 2).Children(func() {
					ui.Column(c.Key("outline-list")).FillWidth().Padding(theme.Space6).Gap(theme.Space2).Children(func() {
						for idx, it := range items {
							item := it
							isActive := idx == activeItemIdx
							barLit := isActive || (item.BlockIndex > activeBlockIdx && item.BlockIndex < sectionEnd)
							indent := outlinePopoverIndent(item.Level)
							row := ui.ButtonBase(c.Key(idx)).
								FillWidth().Height(theme.HeightCompact).Padding(0, theme.Space8, 0, indent).Gap(theme.Space8).
								AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
								KeepFocus().FocusRing(false).Label(item.Title)
							rowHovered := row.Hovered()
							rowPressed := row.Pressed()
							rowBG, rowBD := ui.Transparent, ui.Transparent
							textCol := pal.TextSecondary
							fontW := theme.WeightRegular
							if isActive {
								rowBG, rowBD = pal.Selected, pal.SelectedBorder
								textCol = pal.Text
								fontW = theme.WeightControl
							} else {
								if barLit {
									textCol = pal.Text
								}
								if rowPressed {
									rowBG = pal.Selected
									textCol = pal.Text
								} else if rowHovered {
									rowBG = pal.Hover
									textCol = pal.Text
								}
							}
							appUi.AnimateSurfaceColors(row, rowBG, rowBD, true)
							row.Children(func() {
								barW := outlineBarWidth(item.Level)
								barCol := pal.OutlineBarDim
								if barLit {
									barCol = pal.OutlineBarLit
								}
								barBox := ui.Box(c).Width(barW).Height(2).Radius(theme.RadiusXS).Background(barCol).Shrink(0)
								if appUi.IsProbeEnabled() {
									appUi.Probe(barBox, fmt.Sprintf("outline-bar-%d", idx))
								}
								ui.Text(c, item.Title).FontSize(theme.FontSmall).FontWeight(fontW).TextColor(textCol).Grow(1).SingleLine()
							})
							if appUi.IsProbeEnabled() {
								appUi.Probe(row, fmt.Sprintf("outline-item-%d", idx))
							}
							if row.Clicked() {
								s.JumpToBlock(item.BlockIndex)
								c.Invalidate()
							}
						}
					})
				})
			})
		} else {
			rail := ui.Column(c.Key("outline-rail")).Padding(theme.Space4, theme.Space8, theme.Space4, 0).Gap(theme.Space6).AlignItems(ui.Start)
			rail.Children(func() {
				for idx, it := range items {
					item := it
					isActive := idx == activeItemIdx
					barLit := isActive || (item.BlockIndex > activeBlockIdx && item.BlockIndex < sectionEnd)
					barBtn := ui.ButtonBase(c.Key(idx)).
						Height(theme.Space6).Padding(0, theme.Space6, 0, 0).AlignItems(ui.Center).Justify(ui.Start).
						Cursor(ui.CursorPointer).KeepFocus().FocusRing(false).Label(item.Title)
					barBtn.Children(func() {
						barCol := pal.OutlineBarDim
						if barLit {
							barCol = pal.OutlineBarLit
						}
						ui.Box(c).Width(outlineBarWidth(item.Level)).Height(2).Radius(theme.RadiusXS).Background(barCol)
					})
					if appUi.IsProbeEnabled() {
						appUi.Probe(barBtn, fmt.Sprintf("outline-bar-%d", idx))
					}
					if barBtn.Clicked() {
						s.JumpToBlock(item.BlockIndex)
						c.Invalidate()
					}
				}
			})
		}
	})
}

func outlinePopoverIndent(level int) float32 {
	switch {
	case level <= 1:
		return theme.Space8
	case level == 2:
		return theme.Space16
	default:
		return theme.Space24
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

// --- 6. Right-side annotations & block-foot fallback ---

const (
	annotationRailWidth    float32 = 208
	annotationCardMinWidth float32 = 180
)

type anchoredAnnotation struct {
	ann     *model.Annotation
	blockID model.BlockID
}

type annotationFrame struct {
	railLeft, railWidth float32
	right               []anchoredAnnotation
	below               map[model.BlockID][]*model.Annotation
	hovered             string
	pickerAnn           *model.Annotation
	pickerAnchor        ui.Element
}

func (s *Session) placeAnnotations(c *ui.Context, canvasLeft, canvasWidth float32) annotationFrame {
	var frame annotationFrame
	if s.minimal || len(s.annotations) == 0 {
		return frame
	}
	// Match the centered document flow. Leave room for the widest block's
	// bleed, the gap next to it, and the scrollbar at the right edge.
	flowWidth := min(canvasWidth, s.outerFlowWidth())
	bodyRight := canvasLeft + (canvasWidth+flowWidth)*0.5 - s.rightContentInset()
	frame.railLeft = bodyRight + s.bandBleed() + theme.Space8
	frame.railWidth = max(canvasLeft+canvasWidth-theme.ScrollGutter-frame.railLeft, 0)

	countW, _ := c.MeasureText(0, ui.Span{Text: "1", Size: theme.FontBody, Weight: theme.WeightControl})
	for _, ann := range s.annotations {
		if !isAnnotationVisible(ann) {
			continue
		}
		need := annotationCardMinWidth
		if isReactionOnlyAnnotation(ann) {
			need = 0
		}
		for _, emoji := range ann.Reactions {
			iconW, _ := c.MeasureText(0, ui.Span{Text: emoji, Size: theme.FontBody})
			need = max(need, iconW+countW+2*theme.Space8+theme.Space4+2) // Include the pill's border.
		}
		right := frame.railWidth >= need
		start := s.blockIndexByID(ann.StartBlockID)
		end := s.blockIndexByID(ann.EndBlockID)
		block := max(start, end)
		if right && start >= 0 {
			block = start
		}
		block = s.visibleAnnotationAnchorIndex(block)
		if block < 0 {
			continue
		}
		blockID := s.doc.Blocks[block].ID
		if right {
			frame.right = append(frame.right, anchoredAnnotation{ann: ann, blockID: blockID})
			continue
		}
		if frame.below == nil {
			frame.below = make(map[model.BlockID][]*model.Annotation)
		}
		frame.below[blockID] = append(frame.below[blockID], ann)
	}
	return frame
}

func isReactionOnlyAnnotation(ann *model.Annotation) bool {
	return ann != nil && ann.Note == "" && ann.RewriteResult == "" && ann.Answer == "" && ann.RewritePrompt == "" && ann.AIError == "" && len(ann.Reactions) > 0
}

func (s *Session) renderReactionPill(c *ui.Context, id string, ann *model.Annotation, emoji string) {
	active := s.activeAnnotID == ann.ID || s.hoveredAnnotID == ann.ID || s.reactionPickerAnnotID == ann.ID
	rc := theme.Ocean().ReactionPill(active)
	pill := ui.ButtonBase(c.Key(id)).Height(theme.IconButtonBox).PaddingX(theme.Space8).Gap(theme.Space4).Radius(theme.RadiusPill).
		AlignItems(ui.Center).Justify(ui.Center).
		Cursor(ui.CursorPointer).Label(emoji)
	pillHovered := pill.Hovered()
	pillPressed := pill.Pressed()
	pillBG := rc.BG
	if pillHovered {
		pillBG = rc.HoverBG
		s.frameAnnotations.hovered = ann.ID
	}
	appUi.AnimateSurfaceColors(pill, pillBG, rc.Border, true)
	appUi.TactileCard(pill, pillHovered, pillPressed, active, false)
	pill.Children(func() {
		ui.Text(c, emoji).FontSize(theme.FontBody)
		ui.Text(c, "1").FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(rc.FG)
	})
	appUi.Probe(pill, id)
	if pill.Clicked() {
		s.reactionPickerAnnotID = ""
		s.toggleReactionByID(ann.ID, emoji)
	}
	rightClicked := !s.readOnly && pill.RightClicked()
	if rightClicked {
		if s.reactionPickerAnnotID == ann.ID {
			s.reactionPickerAnnotID = ""
		} else {
			s.reactionPickerAnnotID = ann.ID
		}
	}
	if !s.readOnly && s.reactionPickerAnnotID == ann.ID && (!s.frameAnnotations.pickerAnchor.Valid() || rightClicked) {
		s.frameAnnotations.pickerAnn = ann
		s.frameAnnotations.pickerAnchor = pill
	}
}

func (s *Session) renderAnnotationItem(c *ui.Context, ann *model.Annotation, maxW float32) {
	pal := theme.Ocean()
	editing := s.editingAnnotID == ann.ID && !s.readOnly
	isAnswer := !s.aiDisabled && !editing && ann.Answer != ""
	isResult := !s.aiDisabled && !editing && (ann.RewriteResult != "" || isAnswer)

	card := ui.Column(c.Key("annotation-card-"+ann.ID)).Width(maxW).MinWidth(0).MaxWidthPercent(100).
		Padding(theme.Space8, theme.Space10, theme.Space10, theme.Space12).
		Gap(theme.Space4).Radius(theme.RadiusLG).Clip().
		Transition(appUi.GutterHandleTransition())
	cardHovered := card.Hovered()
	if cardHovered {
		s.frameAnnotations.hovered = ann.ID
	}
	hot := cardHovered || s.hoveredAnnotID == ann.ID
	borderCol := pal.Hairline
	if hot || editing {
		borderCol = pal.SelectedBorder
	}
	appUi.AnimateSurfaceColors(card, pal.Menu, borderCol, true)
	appUi.Probe(card, "annotation-card-"+ann.ID)
	card.Children(func() {
		s.renderAnnotationHeader(c, ann, isResult, hot && !editing && !isResult && !s.readOnly)
		switch {
		case editing:
			s.renderAnnotationEditor(c, ann)
		case isAnswer:
			s.renderAnnotationAnswer(c, ann)
		case isResult:
			s.renderAnnotationResult(c, ann)
		default:
			s.renderAnnotationNote(c, ann)
		}
	})
	if editing && card.PressedOutside() {
		s.commitAnnotationEdit(ann.ID)
	}
}

// renderAnnotationHeader draws the quote row: marker, quoted source text, hover actions.
func (s *Session) renderAnnotationHeader(c *ui.Context, ann *model.Annotation, isResult, showActions bool) {
	pal := theme.Ocean()
	ui.Row(c).FillWidth().MinWidth(0).Height(theme.HeightMicro).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
		ui.Row(c).Width(theme.IconInline).Shrink(0).Center().Children(func() {
			if isResult {
				icons.Draw(c, icons.Sparkles, theme.IconInline, pal.MonoIconColors(pal.Accent))
			} else {
				ui.Box(c).Size(theme.Space6, theme.Space6).Radius(theme.RadiusPill).Background(pal.AnnotSendDot)
			}
		})
		head := ann.QuotedText
		if isResult {
			head = theme.Tr("AI 改写", "AI rewrite")
			if ann.Answer != "" {
				head = theme.Tr("AI 回答", "AI answer")
			}
		}
		ui.Text(c, head).Grow(1).Shrink(1).MinWidth(0).SingleLine().
			FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
		if isResult && ann.Answer != "" && !s.readOnly {
			done := appUi.IconButton(c, icons.Check, appUi.IconButtonOptions{
				ID: "ann-answer-done-" + ann.ID, Label: theme.Tr("知道了", "Got it"), Size: theme.HeightMicro, IconSize: theme.IconInline,
			})
			if done.Clicked() {
				s.deleteAnnotationByID(ann.ID)
			}
			return
		}
		if !showActions {
			return
		}
		delBtn := appUi.IconButton(c, icons.Trash, appUi.IconButtonOptions{
			ID: "annotation-delete-" + ann.ID, Label: theme.Tr("删除批注", "Delete annotation"), Size: theme.HeightMicro, IconSize: theme.IconInline, Danger: true,
		})
		if delBtn.Clicked() {
			s.deleteAnnotationByID(ann.ID)
		}
	})
}

func (s *Session) renderAnnotationNote(c *ui.Context, ann *model.Annotation) {
	pal := theme.Ocean()
	noteTxt := ann.Note
	if noteTxt == "" && !s.aiDisabled {
		noteTxt = ann.RewritePrompt
	}
	if n := len(ann.Thread); n > 0 && !s.aiDisabled {
		if prev := strings.Join(strings.Fields(ann.Thread[n-1].A), " "); prev != "" {
			ui.Text(c, prev).FillWidth().MinWidth(0).SingleLine().
				FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.Placeholder)
		}
	}
	if noteTxt != "" {
		if s.readOnly {
			ui.Text(c, noteTxt).FillWidth().MinWidth(0).
				FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text)
		} else {
			body := ui.ButtonBase(c.Key("annotation-edit-" + ann.ID)).FillWidth().MinWidth(0).
				Cursor(ui.CursorText).Label(theme.Tr("编辑批注", "Edit annotation"))
			body.Children(func() {
				ui.Text(c, noteTxt).FillWidth().MinWidth(0).
					FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text)
			})
			if body.Clicked() {
				s.beginAnnotationEdit(ann)
			}
		}
	}
	if ann.RewriteBusy && !s.aiDisabled {
		ui.Row(c).FillWidth().Height(theme.HeightMicro).AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
			ui.Spinner(c).FontSize(theme.IconInline).TextColor(pal.Accent)
			ui.Text(c, theme.Tr("思考中…", "Thinking…")).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
		})
	}
	if s.aiDisabled || ann.RewritePrompt == "" || s.readOnly {
		return
	}
	sparkles := icons.Sparkles
	ui.Row(c).FillWidth().Padding(theme.Space4, 0, 0, 0).Children(func() {
		apply := appUi.ActionButtonWithOptions(c, appUi.ActionButtonOptions{
			ID: "annotation-accept-" + ann.ID, Label: theme.Tr("按批注改写", "Rewrite"), Leading: &sparkles,
			Style: appUi.ButtonSecondary, Height: theme.HeightCompact,
		})
		if apply.Clicked() {
			s.ApplyAnnotationRewrite(ann.ID, ann.RewritePrompt)
		}
	})
}

func (s *Session) renderAnnotationResult(c *ui.Context, ann *model.Annotation) {
	pal := theme.Ocean()
	if ann.QuotedText != "" {
		ui.Text(c, ann.QuotedText).FillWidth().MinWidth(0).Strikethrough().
			FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
	}
	ui.Text(c, ann.RewriteResult).FillWidth().MinWidth(0).
		FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.SelectionFG)
	if s.readOnly {
		return
	}
	ui.Row(c).FillWidth().Padding(theme.Space4, 0, 0, 0).AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
		chkIc := icons.Check
		acc := appUi.ActionButtonWithOptions(c, appUi.ActionButtonOptions{
			ID: "ann-accept-" + ann.ID, Label: theme.Tr("替换原文", "Replace"), Leading: &chkIc, Style: appUi.ButtonPrimary, Height: theme.HeightCompact,
		})
		if acc.Clicked() {
			s.ApplyAnnotationRewrite(ann.ID, ann.RewriteResult)
		}
		dismiss := appUi.BtnClose(c, "ann-dismiss-"+ann.ID, appUi.PresetOptions{
			Label:  theme.Tr("忽略", "Dismiss"),
			Style:  appUi.StyleRef(appUi.ButtonGhost),
			Height: theme.HeightCompact,
		})
		if dismiss.Clicked() {
			s.dismissAnnotationRewrite(ann.ID)
		}
	})
	s.renderAnnotationFollowup(c, ann)
}

// renderAnnotationAnswer shows the AI's reply to a question note: the question, then the answer; nothing to replace.
func (s *Session) renderAnnotationAnswer(c *ui.Context, ann *model.Annotation) {
	pal := theme.Ocean()
	if ann.Note != "" {
		ui.Text(c, ann.Note).FillWidth().MinWidth(0).
			FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
	}
	s.annotationMarkdown(c, ann.Answer)
	s.renderAnnotationFollowup(c, ann)
}

// renderAnnotationFollowup is the always-there "追问…" line under an AI reply: type, Enter sends it for this card.
func (s *Session) renderAnnotationFollowup(c *ui.Context, ann *model.Annotation) {
	if s.readOnly || s.aiDisabled || s.annotationAIResponder == nil {
		return
	}
	pal := theme.Ocean()
	if s.followupDrafts == nil {
		s.followupDrafts = make(map[string]*string)
	}
	draft := s.followupDrafts[ann.ID]
	if draft == nil {
		draft = new(string)
		s.followupDrafts[ann.ID] = draft
	}
	ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space6, 0, 0, 0).Gap(theme.Space8).Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
		in := ui.TextAreaBase(c.Key("ann-followup-"+ann.ID), draft).FillWidth().MinWidth(0).Lines(1, 4).
			Padding(0).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.Text).CaretColor(pal.Accent).
			Placeholder(theme.Tr("追问…", "Follow up…")).PlaceholderColor(pal.Placeholder).Label(theme.Tr("追问", "Follow up"))
		appUi.AutoHideScrollbars(c, in)
		appUi.Probe(in, "ann-followup-"+ann.ID)
		composing := in.Composing()
		id := ann.ID
		in.OnTextKey(func(k ui.TextKey) bool {
			if composing {
				return false
			}
			switch {
			case k.Key == ui.KeyEscape:
				*draft = ""
				return true
			case isAnnotationSubmitKey(k):
				if s.SendAnnotationFollowup(id, k.Text()) {
					delete(s.followupDrafts, id)
				}
				return true
			}
			return false
		})
	})
}

// forwardRailScroll lets the wheel over a margin card scroll the document beneath it.
func (s *Session) forwardRailScroll(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputScroll || ev.Mods&ui.Ctrl != 0 {
		return false
	}
	next := min(max(s.bodyScroll.Y+ev.DY, 0), s.bodyScroll.MaxY)
	if next != s.bodyScroll.Y {
		s.bodyScroll.Y = next
		if s.bodyScroll.Scrolling() {
			s.bodyScroll.ScrollTo(s.bodyScroll.X, next, ui.Animation{})
		}
	}
	return true
}

// renderAnnotationEditor edits the note in place: Enter or clicking outside saves, Shift+Enter breaks the line, Esc cancels.
func (s *Session) renderAnnotationEditor(c *ui.Context, ann *model.Annotation) {
	pal := theme.Ocean()
	in := ui.TextAreaBase(c.Key("edit-ann-"+ann.ID), &s.editingAnnotText).FillWidth().MinWidth(0).Lines(1, 8).
		Padding(0).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).CaretColor(pal.Accent).
		Placeholder(theme.Tr("写下批注…", "Write a note…")).PlaceholderColor(pal.Placeholder).Label(theme.Tr("批注内容", "Annotation")).
		Bind(&s.editingHandle)
	appUi.AutoHideScrollbars(c, in)
	composing := in.Composing()
	in.OnTextKey(func(k ui.TextKey) bool {
		if composing {
			return false
		}
		switch {
		case k.Key == ui.KeyEscape:
			s.editingAnnotID = ""
			return true
		case isAnnotationSubmitKey(k):
			if s.editingAnnotID != ann.ID {
				return true
			}
			s.editingAnnotText = k.Text()
			s.commitAnnotationEdit(ann.ID)
			return true
		}
		return false
	})
	if s.editingFocusPending {
		in.Focus()
		s.editingFocusPending = false
	}
}

// flushAnnotationEdit saves an open annotation draft before the editor drops its transient state.
func (s *Session) flushAnnotationEdit() {
	if s.editingAnnotID != "" {
		s.commitAnnotationEdit(s.editingAnnotID)
	}
}

// beginAnnotationEdit opens the card for editing, first saving any other card left open.
func (s *Session) beginAnnotationEdit(ann *model.Annotation) {
	if s.editingAnnotID == ann.ID {
		return
	}
	if s.toolbarPop == "comment" {
		s.toolbarPop = ""
	}
	s.flushAnnotationEdit()
	s.editingAnnotID = ann.ID
	s.editingAnnotText = ann.Note
	if s.editingAnnotText == "" {
		s.editingAnnotText = ann.RewritePrompt
	}
	s.editingFocusPending = true
}

// commitAnnotationEdit saves the draft into whichever field the card shows
// (the note, or the rewrite prompt of a prompt-only card); clearing it removes the annotation.
func (s *Session) commitAnnotationEdit(id string) {
	s.ensureAnnotationsLoaded()
	text := strings.TrimSpace(s.editingAnnotText)
	for _, ann := range s.annotations {
		if ann.ID != id {
			continue
		}
		promptOnly := ann.Note == "" && ann.RewritePrompt != ""
		if text == "" && len(ann.Reactions) == 0 && (promptOnly || ann.RewritePrompt == "") {
			s.editingAnnotID = ""
			s.deleteAnnotationByID(id)
			return
		}
		if promptOnly {
			s.editingAnnotID = ""
			if text != "" && text != ann.RewritePrompt {
				s.pushUndo()
				ann.RewritePrompt = text
				s.persistAnnotations()
			}
			return
		}
		s.updateAnnotationNote(id, text)
		return
	}
	s.editingAnnotID = ""
}

func (s *Session) estimateAnnotationRowMetrics(ann *model.Annotation, isEditing bool) (height, stepGap float32) {
	if isReactionOnlyAnnotation(ann) {
		return 24, 4
	}
	stepGap = 8
	switch {
	case isEditing:
		height = 64
	case !s.aiDisabled && ann.Answer != "":
		lines := max((len([]rune(ann.Answer))+15)/16, 1)
		height = 96 + float32(lines-1)*theme.LineBody
	case !s.aiDisabled && ann.RewriteResult != "":
		height = 154
	default:
		lines := max((len([]rune(ann.Note))+11)/12, 1)
		height = 64 + float32(lines-1)*theme.LineBody
		if !s.aiDisabled && ann.RewritePrompt != "" {
			height += 36
		}
	}
	if len(ann.Reactions) > 0 {
		height += 28
	}
	return height, stepGap
}

func (s *Session) renderAnnotationRail(c *ui.Context) {
	frame := &s.frameAnnotations
	if len(frame.right) == 0 {
		s.setAnnotExtraBottom(0)
		return
	}
	railTop := s.editorRect.Y
	railHeight := s.editorRect.H
	if railHeight <= 0 {
		railHeight = s.viewportHeight - railTop
	}

	type railEntry struct {
		ann     *model.Annotation
		targetY float32
		height  float32
		gap     float32
	}
	rows := make([]railEntry, 0, len(frame.right))
	for _, anchored := range frame.right {
		ann := anchored.ann
		idx := s.blockIndexByID(anchored.blockID)
		if idx < 0 {
			continue
		}
		targetY := s.canvasTopPad() - s.bodyScroll.Y
		if r, ok := s.blockRect(anchored.blockID); ok {
			targetY = r.Y - railTop
			if idx < len(s.frameBlocks) {
				targetY += s.blockGap(s.frameBlocks[idx].PrevVisible, &s.doc.Blocks[idx])
			}
		}
		h, g := s.estimateAnnotationRowMetrics(ann, s.editingAnnotID == ann.ID)
		if mh := s.annotRowHeights[ann.ID]; mh > 0 {
			h = mh
		}
		rows = append(rows, railEntry{ann: ann, targetY: targetY, height: h, gap: g})
	}
	slices.SortStableFunc(rows, func(a, b railEntry) int {
		return cmp.Compare(a.targetY, b.targetY)
	})

	// Cards stacked past the document end need room below it, or the last ones can never scroll into view.
	stackBottom := float32(-math.MaxFloat32)
	for _, row := range rows {
		stackBottom = max(row.targetY, stackBottom+row.gap) + row.height
	}
	extra := float32(0)
	if last := s.visibleAnnotationAnchorIndex(-1); last >= 0 {
		if r, ok := s.blockRect(s.doc.Blocks[last].ID); ok {
			tail := s.scale(editorBottomPad)
			if !s.readOnly {
				tail += s.scale(120)
			}
			extra = max(stackBottom+theme.Space24-(r.Y+r.H-railTop)-tail, 0)
		}
	}
	s.setAnnotExtraBottom(extra)

	ui.Overlay(c, func() {
		rail := ui.Box(c.Key("annotation-rail")).Absolute().Left(frame.railLeft).Top(railTop).
			Width(frame.railWidth).Height(railHeight).PassThrough().Clip()
		appUi.Probe(rail, "annotation-rail")
		rail.Children(func() {
			bottom := float32(-math.MaxFloat32)
			for _, row := range rows {
				ann := row.ann
				top := max(row.targetY, bottom+row.gap)
				bottom = top + row.height
				if bottom <= 0 || top >= railHeight {
					if ann.ID == s.editingAnnotID {
						s.editingFocusPending = true
					}
					continue
				}

				slot := ui.Column(c.Key("annotation-wrap-" + ann.ID)).Absolute().Top(top).Left(0).
					Width(frame.railWidth).AlignItems(ui.Start).Gap(theme.Space4).
					HandleInput(s.forwardRailScroll)
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
					if !isReactionOnlyAnnotation(ann) {
						s.renderAnnotationItem(c, ann, frame.railWidth)
					}
					if len(ann.Reactions) > 0 {
						ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
							for rIdx, em := range ann.Reactions {
								s.renderReactionPill(c, fmt.Sprintf("reaction-pill-%s-%d", ann.ID, rIdx), ann, em)
							}
						})
					}
				})
			}
		})
	})
}

func (s *Session) setAnnotExtraBottom(v float32) {
	if math.Abs(float64(v-s.annotExtraBottom)) > 1 {
		s.annotExtraBottom = v
		s.requestRedraw()
	}
}

func (s *Session) visibleAnnotationAnchorIndex(anchor int) int {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return -1
	}
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

func (s *Session) renderBlockAnnotationsFoot(c *ui.Context, b *model.Block) {
	anchored := s.frameAnnotations.below[b.ID]
	if len(anchored) == 0 {
		return
	}
	botPad := theme.Space2
	if b.Kind == model.BlockTable {
		botPad = theme.Space10
	}
	maxChipW := min(annotationRailWidth, max(s.viewportWidth-32, 160))

	strip := ui.Column(c.Key(fmt.Sprintf("annotate-foot-%d", b.ID))).FillWidth().MinWidth(0).
		Padding(theme.Space6, 0, botPad, 0).Gap(theme.Space6).AlignItems(ui.Start)
	strip.Children(func() {
		if slices.ContainsFunc(anchored, func(ann *model.Annotation) bool { return len(ann.Reactions) > 0 }) {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
				for _, ann := range anchored {
					if len(ann.Reactions) > 0 {
						s.renderReactionPill(c, fmt.Sprintf("annotation-marker-%s", ann.ID), ann, ann.Reactions[0])
					}
				}
			})
		}
		for _, ann := range anchored {
			if !isReactionOnlyAnnotation(ann) {
				s.renderAnnotationItem(c, ann, maxChipW)
			}
		}
	})
}

func (s *Session) renderAnnotationReactionPicker(c *ui.Context) {
	if s.reactionPickerAnnotID == "" {
		return
	}
	targetAnn := s.frameAnnotations.pickerAnn
	anchor := s.frameAnnotations.pickerAnchor
	if targetAnn == nil || targetAnn.ID != s.reactionPickerAnnotID || !anchor.Valid() {
		s.reactionPickerAnnotID = ""
		return
	}

	pal := theme.Ocean()
	ui.Overlay(c, func() {
		picker := appUi.MenuSurface(ui.Row(c.Key("foot-reaction-picker-"+targetAnn.ID))).
			Padding(theme.Space4, theme.Space6).Gap(theme.Space4).AlignItems(ui.Center).
			AttachTo(anchor, ui.AnchorBottomLeft, ui.AnchorTopLeft).Margin(theme.Space4, 0)
		appUi.Probe(picker, "foot-reaction-picker")
		picker.Children(func() {
			for eIdx, emoji := range reactionEmojis {
				btnID := fmt.Sprintf("foot-pick-emoji-%s-%d", targetAnn.ID, eIdx)
				selected := slices.Contains(targetAnn.Reactions, emoji)
				btn := ui.ButtonBase(c.Key(btnID)).Size(theme.HeightCompact, theme.HeightCompact).Center().Radius(theme.RadiusMD).
					Cursor(ui.CursorPointer).Label(emoji)
				btnHovered := btn.Hovered()
				btnPressed := btn.Pressed()
				btnBG, btnBD := ui.Transparent, ui.Transparent
				if selected {
					btnBG, btnBD = pal.Selected, pal.SelectedBorder
				} else if btnPressed {
					btnBG = pal.Selected
				} else if btnHovered {
					btnBG = pal.Hover
				}
				appUi.AnimateSurfaceColors(btn, btnBG, btnBD, selected)
				appUi.TactileButton(btn, btnHovered, btnPressed, false)
				btn.Children(func() {
					ui.Text(c, emoji).FontSize(theme.FontTitle)
				})
				appUi.Probe(btn, btnID)
				if btn.Clicked() {
					s.reactionPickerAnnotID = ""
					s.toggleReactionByID(targetAnn.ID, emoji)
				}
			}
		})
		if picker.PressedOutside() || picker.OverlayShortcut(0, ui.KeyEscape) {
			s.reactionPickerAnnotID = ""
			c.Invalidate()
		}
	})
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
		card := appUi.Elevate(ui.Column(c.Key("plan-diff-card")).Absolute().Bottom(bottomY).Left(leftX).
			Width(cardW).MaxHeight(maxH).Padding(0).Gap(0).
			Radius(theme.RadiusXL).Clip().Background(pal.Paper).Border(1, pal.Hairline).
			Transition(appUi.PopoverSpringTransition()), theme.ElevationDialog)
		appUi.Probe(card, "plan-diff-card")
		s.recordOverlayPanel("plan-diff-card", card)

		card.Children(func() {
			// Header
			ui.Row(c.Key("plan-hdr")).FillWidth().Padding(theme.Space10, theme.Space12).Radius(theme.RadiusXLInner, theme.RadiusXLInner, 0, 0).
				Background(pal.Sidebar).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).Children(func() {
				title := plan.Summary
				if title == "" {
					title = theme.Tr("AI 修改审阅", "AI Plan Diff Review")
				}
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Grow(1).MinWidth(0).Children(func() {
					icons.Draw(c, icons.Sparkles, theme.IconAction, pal.MonoIconColors(pal.Accent))
					ui.Text(c, title).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
				})
				ui.Row(c).Gap(theme.Space6).AlignItems(ui.Center).Shrink(0).Children(func() {
					rejIc := icons.Close
					rej := appUi.ActionButton(c, "plan-reject", theme.Tr("拒绝", "Reject"), &rejIc, appUi.ButtonDanger, false)
					if rej.Clicked() {
						s.rejectActivePlan()
					}
					accIc := icons.Check
					acc := appUi.ActionButton(c, "plan-accept", theme.Tr("接受选中修改", "Accept Selected"), &accIc, appUi.ButtonPrimary, false)
					if acc.Clicked() {
						s.acceptActivePlan()
					}
				})
			})

			// Diff body with per-line checkboxes
			appUi.Scroll(c.Key("plan-diff-scroll"), &s.planDiffScroll).FillWidth().Grow(1).
				MaxHeight(maxH-64).Padding(theme.Space10, theme.Space12).Children(func() {
				ui.Column(c.Key("plan-ops-col")).FillWidth().Gap(theme.Space8).Children(func() {
					renderLineContent := func(raw, trimmed string, cols int, isTable, isHeader bool, style appUi.DiffLineStyle) {
						style.CodeBG = pal.CodeBlockBG
						if style.Code || !isTable {
							appUi.DiffMarkdownLine(c, raw, style)
							return
						}
						appUi.DiffTableRow(c, trimmed, cols, style, isHeader, pal.Hairline)
					}
					for opIdx := range plan.Ops {
						op := &plan.Ops[opIdx]
						diff := s.ensurePlanOpLineSelection(op)
						texts, changes := diffChanges(diff)
						fenced := appUi.DiffFenceMask(texts, changes)
						setext := appUi.DiffSetextLevels(texts, changes)
						listNums := appUi.DiffListNumbers(texts, changes)
						refs, refDefs := appUi.DiffReferences(texts, changes)
						aligns := appUi.DiffTableAligns(texts, changes)
						math := appUi.DiffMathMask(texts, changes)
						alerts, alertBodies := appUi.DiffAlerts(texts, changes)
						fenceLines, fenceQuotes := appUi.DiffFenceLines(texts, changes)
						hardBreaks := appUi.DiffHardBreaks(texts, changes)
						tableCols := appUi.DiffTableCols(texts, changes)
						headers, seps := appUi.DiffTableRoles(texts, changes)
						ui.Column(c.Key(fmt.Sprintf("plan-op-%d", opIdx))).FillWidth().
							Radius(theme.RadiusLG).Border(1, pal.Hairline).Clip().Children(func() {
							ui.Row(c.Key("op-title")).FillWidth().Padding(theme.Space6, theme.Space10).Radius(theme.RadiusMD, theme.RadiusMD, 0, 0).
								Background(pal.Shell).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
								ui.Text(c, op.Page).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
							})
							selIdx := 0
							for lineIdx, dl := range diff {
								trimmed := strings.TrimSpace(dl.Text)
								if seps[lineIdx] {
									if dl.Kind != model.DiffEqual {
										selIdx++
									}
									continue
								}
								nextVisIdx := -1
								for j := lineIdx + 1; j < len(diff); j++ {
									if !seps[j] {
										nextVisIdx = j
										break
									}
								}
								isLast := nextVisIdx == -1
								isTable := tableCols[lineIdx] > 0
								isHeader := isTable && headers[lineIdx]
								nextIsTable := !isLast && tableCols[nextVisIdx] > 0
								needSep := !isLast && (isTable || nextIsTable || diff[nextVisIdx].Kind != dl.Kind)
								switch dl.Kind {
								case model.DiffEqual:
									row := ui.Row(c.Key(fmt.Sprintf("dl-%d-%d", opIdx, lineIdx))).FillWidth().
										Padding(theme.Space4, theme.Space10).Gap(theme.Space8).AlignItems(ui.Center).
										Background(pal.Paper)
									if isLast {
										const zero float32 = 0
										row.Radius(zero, zero, theme.RadiusMD, theme.RadiusMD)
									}
									if needSep {
										row.BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
									}
									row.Children(func() {
										// 与变更行的复选框同宽留白，表格列边界才能对齐。
										ui.Box(c).Width(theme.IconRow).Shrink(0)
										ui.Box(c).Width(12).Shrink(0)
										renderLineContent(dl.Text, trimmed, tableCols[lineIdx], isTable, isHeader, appUi.DiffLineStyle{Text: pal.TextSecondary, Code: fenced[lineIdx], Setext: setext[lineIdx], ListNum: listNums[lineIdx], Refs: refs[lineIdx], Aligns: aligns[lineIdx], Math: math[lineIdx], RefDef: refDefs[lineIdx], Alert: alerts[lineIdx], AlertBody: alertBodies[lineIdx], Fence: fenceLines[lineIdx], Quote: fenceQuotes[lineIdx], Break: hardBreaks[lineIdx]})
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
									gutter := "+"
									if dl.Kind == model.DiffDelete {
										bg = pal.DiffDelBG
										fg = pal.DiffDelFG
										gutter = "−"
									}
									row := ui.Row(c.Key(fmt.Sprintf("dl-%d-%d", opIdx, lineIdx))).FillWidth().
										Padding(theme.Space4, theme.Space10).Gap(theme.Space8).AlignItems(ui.Center).Background(bg)
									if isLast {
										const zero float32 = 0
										row.Radius(zero, zero, theme.RadiusMD, theme.RadiusMD)
									}
									if needSep {
										row.BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
									}
									row.Children(func() {
										cbID := fmt.Sprintf("plan-diff-line-%d-%d", opIdx, curSelIdx)
										ui.Box(c).Width(theme.IconRow).Shrink(0).Children(func() {
											cb := ui.Checkbox(c.Key(cbID), &checked, "").Label(cbID)
											appUi.Probe(cb, cbID)
											if cb.Changed() && curSelIdx < len(op.LineSelected) {
												op.LineSelected[curSelIdx] = checked
											}
										})
										ui.Text(c, gutter).Width(12).Shrink(0).FontSize(theme.FontBody).TextColor(fg).SingleLine()
										renderLineContent(dl.Text, trimmed, tableCols[lineIdx], isTable, isHeader, appUi.DiffLineStyle{Text: fg, Code: fenced[lineIdx], Setext: setext[lineIdx], ListNum: listNums[lineIdx], Refs: refs[lineIdx], Aligns: aligns[lineIdx], Math: math[lineIdx], RefDef: refDefs[lineIdx], Alert: alerts[lineIdx], AlertBody: alertBodies[lineIdx], Fence: fenceLines[lineIdx], Quote: fenceQuotes[lineIdx], Break: hardBreaks[lineIdx]})
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

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
