package editor

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const blockDragThreshold float64 = 4

// onGutterInput hides idle block handles until the pointer moves or clicks.
func (s *Session) onGutterInput(ev ui.InputEvent) {
	switch ev.Kind {
	case ui.InputKeyDown, ui.InputKeyUp, ui.InputPointerMove, ui.InputScroll, ui.InputZoom:
		if ev.Mods != ui.Cmd {
			s.lastPressCmdIntact = false
			s.cancelAllPendingRichLinks()
		}
	case ui.InputText, ui.InputCompose, ui.InputCommand:
		s.lastPressCmdIntact = false
		s.cancelAllPendingRichLinks()
	}
	switch ev.Kind {
	case ui.InputPointerDown:
		s.typingPointer = nil
		return
	case ui.InputText, ui.InputCompose:
		if ev.Text == "" {
			return
		}
	case ui.InputKeyDown:
		switch ev.Key {
		case ui.KeyEnter, ui.KeyBackspace, ui.KeyDelete:
		default:
			// Native text clients can receive text directly after the key event.
			printable := ev.Key == ui.KeySpace ||
				ev.Key >= ui.KeyMinus && ev.Key <= ui.KeyBackquote ||
				ev.Key >= ui.Key0 && ev.Key <= ui.KeyZ
			if !printable || ev.Mods&(ui.Ctrl|ui.Super) != 0 {
				return
			}
		}
	default:
		return
	}
	s.typingPointer = &ui.Point{X: ev.X, Y: ev.Y}
}

func blockHandleTop(b *model.Block, topGap float32) float32 {
	if b == nil {
		return 0
	}
	const stripH float32 = 24
	switch {
	case b.Kind == model.BlockTable:
		return (tableMinRowHeight - stripH) * 0.5
	case b.Kind == model.BlockImage:
		return 4
	case b.Kind == model.BlockCallout:
		return max(8, -topGap)
	case b.Kind == model.BlockDivider:
		return max((18-stripH)*0.5, -topGap)
	case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
		return max(5, -topGap)
	default:
		fontSize, _, lh := blockTextMetrics(b.Kind)
		top := (fontSize*lh - stripH) * 0.5
		if b.Kind == model.BlockQuote || b.QuoteDepth > 0 {
			top += 2
		}
		return max(top, -topGap)
	}
}

func (s *Session) resetBlockDrag() {
	s.dragBlockID = 0
	s.dragOverIdx = -1
	s.dragTargetIdx = -1
	s.dragSlot = -1
	s.dragAfter = false
	s.dragIndent = 0
	s.dragLevelKey = [2]int{-2, -2}
	s.dragLevelOffset = 0
	s.dragCollapsed = false
	s.dragSavedHeight = 0
	s.dragActive = false
	s.dragCanceled = false
	s.dragStartPt = ui.Point{}
	s.dragDropPending = false
	s.dragDropFrom = -1
	s.dragDropTo = -1
}

func (s *Session) flushPendingBlockDrop(c *ui.Context) {
	if !s.dragDropPending {
		return
	}
	from, to := s.dragDropFrom, s.dragDropTo
	s.dragDropPending = false
	s.dragDropFrom = -1
	s.dragDropTo = -1
	if s.doc == nil || from < 0 || from >= len(s.doc.Blocks) {
		return
	}
	end := s.dragSubtreeEnd(from)
	count := end - from + 1
	if to < 0 || to > len(s.doc.Blocks)-count {
		return
	}
	delta := s.dragDropIndent - s.doc.Blocks[from].Indent
	if to == from && delta == 0 {
		return
	}
	s.pushUndo()
	blocks := s.doc.Blocks
	moved := append([]model.Block(nil), blocks[from:end+1]...)
	for i := range moved {
		b := &moved[i]
		b.Indent = max(b.Indent+delta, 0)
		if delta != 0 {
			b.CleanSource = ""
			b.Dirty = true
		}
		if b.Language == "yaml-frontmatter" && (to+i != 0 || b.Indent > 0) {
			b.Language = "yaml"
			b.Dirty = true
			b.CleanSource = ""
		}
	}
	if to < from {
		copy(blocks[to+count:end+1], blocks[to:from])
	} else {
		copy(blocks[from:to], blocks[end+1:to+count])
	}
	copy(blocks[to:], moved)
	s.FocusBlock(to)
	s.markDirty()
	c.Invalidate()
}

func (s *Session) cancelBlockDrag() {
	if s.dragBlockID == 0 {
		return
	}
	s.dragCanceled = true
	s.dragActive = false
	s.dragCollapsed = false
	s.dragSavedHeight = 0
	s.dragOverIdx = -1
	s.dragTargetIdx = -1
	s.dragSlot = -1
	s.dragAfter = false
}

// preUpdateBlockDrag guards an active block drag against document mutations
// or window deactivation before row widgets rebuild.
func (s *Session) preUpdateBlockDrag(c *ui.Context) {
	if s.dragBlockID == 0 {
		return
	}
	if s.doc == nil || s.blockIndexByID(s.dragBlockID) < 0 {
		s.resetBlockDrag()
		return
	}
	if !c.WindowActive() {
		s.cancelBlockDrag()
	}
}

func (s *Session) isCollapsedDragRow(id model.BlockID) bool {
	return s.dragActive && s.dragCollapsed && s.dragBlockID == id
}

func (s *Session) dragCompactRowHeight(idx int) float32 {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return s.scale(theme.HeightCompact)
	}
	var prev *model.Block
	for p := idx - 1; p >= 0; p-- {
		if len(s.folded) == 0 || !s.isBlockHiddenByFold(p) {
			prev = &s.doc.Blocks[p]
			break
		}
	}
	return s.blockGap(prev, &s.doc.Blocks[idx]) + s.scale(theme.HeightCompact)
}

func (s *Session) activateBlockDrag(fromIdx int) {
	s.dragActive = true
	s.dragSlot = -1
	s.dragLevelKey = [2]int{-2, -2}
	s.dragLevelOffset = 0
	if s.doc == nil || fromIdx < 0 || fromIdx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[fromIdx]
	end := s.dragSubtreeEnd(fromIdx)
	var totalH float32
	for i := fromIdx; i <= end; i++ {
		if len(s.folded) > 0 && i > fromIdx && s.isBlockHiddenByFold(i) {
			continue
		}
		if r, ok := s.blockRect(s.doc.Blocks[i].ID); ok && r.H > 0 {
			totalH += r.H
		}
	}
	compactH := s.dragCompactRowHeight(fromIdx)
	isTall := totalH-compactH > s.scale(14)
	isBlockSurface := b.Kind == model.BlockImage || b.Kind == model.BlockTable ||
		b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML || b.Kind == model.BlockCallout
	if isBlockSurface || end > fromIdx || isTall {
		s.dragCollapsed = true
		if totalH > compactH {
			s.dragSavedHeight = totalH - compactH
		}
		for i := fromIdx + 1; i <= end; i++ {
			s.deleteBlockRect(s.doc.Blocks[i].ID)
		}
	}
}

func collapsedDragBlockIcon(b *model.Block) icons.Icon {
	switch b.Kind {
	case model.BlockImage:
		return icons.Image
	case model.BlockTable:
		return icons.Table
	case model.BlockCode, model.BlockRawHTML:
		if b.Language == "math" {
			return icons.Math
		}
		return icons.Code
	case model.BlockCallout:
		return icons.Callout
	case model.BlockQuote:
		return icons.Quote
	case model.BlockDivider:
		return icons.Divider
	case model.BlockHeading1:
		return icons.Heading1
	case model.BlockHeading2:
		return icons.Heading2
	case model.BlockHeading3, model.BlockHeading4, model.BlockHeading5, model.BlockHeading6:
		return icons.Heading3
	default:
		return icons.Text
	}
}

func collapsedDragBlockSummary(b *model.Block) string {
	switch b.Kind {
	case model.BlockImage:
		if alt := strings.TrimSpace(b.ImageAlt); alt != "" {
			return alt
		}
		src := strings.TrimSpace(b.ImageSrc)
		if src != "" && !strings.HasPrefix(src, "data:") {
			if base := filepath.Base(src); base != "" && base != "." && base != "/" {
				return base
			}
		}
		return theme.Tr("图片", "Image")
	case model.BlockTable:
		if b.Table != nil {
			for _, row := range b.Table.Rows {
				for _, cell := range row {
					if t := strings.TrimSpace(cell.PlainText()); t != "" {
						return firstNonEmptyLine(t)
					}
				}
			}
		}
		return theme.Tr("表格", "Table")
	case model.BlockCode, model.BlockRawHTML:
		if ln := firstNonEmptyLine(b.Code); ln != "" {
			return ln
		}
		if lang := strings.TrimSpace(b.Language); lang != "" {
			return lang
		}
		return theme.Tr("代码块", "Code")
	case model.BlockDivider:
		return theme.Tr("分割线", "Divider")
	default:
		if ln := firstNonEmptyLine(b.PlainText()); ln != "" {
			return ln
		}
		if b.Kind == model.BlockCallout && b.Callout != "" {
			return string(b.Callout)
		}
		return theme.Tr("空白块", "Empty block")
	}
}

func firstNonEmptyLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return ""
}

func (s *Session) renderCollapsedDragBlock(c *ui.Context, idx int, b *model.Block) {
	pal := theme.Ocean()
	subtreeExtra := max(s.dragSubtreeEnd(idx)-idx, 0)
	bar := ui.Row(c.Key("drag-compact")).FillWidth().Height(s.scale(theme.HeightCompact)).
		Padding(0, s.scale(theme.Space10), 0, s.scale(theme.Space10)).
		Gap(s.scale(theme.Space8)).AlignItems(ui.Center).
		Radius(theme.RadiusSM).Background(pal.CodeBlockBG).Border(1, pal.Hairline)
	bar.Children(func() {
		if !b.Kind.IsList() {
			icons.Draw(c, collapsedDragBlockIcon(b), s.scale(theme.IconInline), pal.DuoIconColors(pal.TextSecondary))
		}
		ui.Text(c, collapsedDragBlockSummary(b)).
			FontSize(s.scale(theme.FontBody)).FixedLineHeight(s.scale(theme.LineBody)).
			TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if subtreeExtra > 0 {
			ui.Text(c, fmt.Sprintf("+%d", subtreeExtra)).
				FontSize(s.scale(theme.FontMeta)).FixedLineHeight(s.scale(theme.LineMeta)).
				FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).Shrink(0)
		}
	})
}

func (s *Session) dragPointerPoint(grip ui.Element) (float32, float32) {
	if s.frameRoot != (ui.Element{}) && s.editorRect.W > 0 {
		return s.windowPointer(s.frameRoot)
	}
	px, py, _ := grip.PointerPosition()
	b := grip.Bounds()
	return b.X + px, b.Y + py
}

func (s *Session) isPointerInDropViewport(wx, wy float32) bool {
	vp := s.scrollRect
	if vp.W <= 0 || vp.H <= 0 {
		vp = s.editorRect
	}
	if vp.W <= 0 || vp.H <= 0 {
		return true
	}
	return wx >= vp.X && wx <= vp.X+vp.W && wy >= vp.Y && wy <= vp.Y+vp.H
}

type dragVisBlock struct {
	idx  int
	rect ui.Rect
}

func (s *Session) listMarkerIndentWidth(blockIdx int) int {
	if s.doc == nil || blockIdx < 0 || blockIdx >= len(s.doc.Blocks) {
		return 2
	}
	if s.doc.Blocks[blockIdx].Kind == model.BlockNumbered {
		return len(strconv.Itoa(s.numberedListIndex(blockIdx))) + 2
	}
	return 2
}

// allowedDropIndents returns strictly ascending valid Indent values at the gap
// between prevIdx and nextIdx (ignoring the dragged subtree [fromIdx..last]).
func (s *Session) allowedDropIndents(fromIdx, last, prevIdx, nextIdx int) []int {
	dragged := &s.doc.Blocks[fromIdx]
	if dragged.Kind == model.BlockTable || dragged.Kind == model.BlockRawHTML || prevIdx < 0 {
		return []int{0}
	}
	maxRel := 0
	for i := fromIdx + 1; i <= last; i++ {
		if rel := s.doc.Blocks[i].Indent - dragged.Indent; rel > maxRel {
			maxRel = rel
		}
	}
	maxRootIndent := max(12-maxRel, 0)
	if maxRootIndent == 0 {
		return []int{0}
	}

	prev := &s.doc.Blocks[prevIdx]
	if !prev.Kind.IsList() && prev.Indent <= 0 {
		return []int{0}
	}

	var chain []int
	if prev.Kind.IsList() {
		if child := prev.Indent + s.listMarkerIndentWidth(prevIdx); child <= maxRootIndent {
			chain = append(chain, child)
		}
	}
	if prev.Indent > 0 && prev.Indent <= maxRootIndent {
		chain = append(chain, prev.Indent)
	}
	threshold := prev.Indent
	for k := prevIdx - 1; k >= 0 && threshold > 0; k-- {
		if k >= fromIdx && k <= last {
			continue
		}
		if len(s.folded) > 0 && s.isBlockHiddenByFold(k) {
			continue
		}
		pb := &s.doc.Blocks[k]
		if !pb.Kind.IsList() && pb.Indent <= 0 {
			break
		}
		if pb.Kind.IsList() && pb.Indent < threshold {
			if pb.Indent > 0 && pb.Indent <= maxRootIndent {
				chain = append(chain, pb.Indent)
			}
			threshold = pb.Indent
		}
	}
	chain = append(chain, 0)

	// Reverse chain into strictly ascending levels.
	levels := make([]int, 0, len(chain))
	for i := len(chain) - 1; i >= 0; i-- {
		v := chain[i]
		if len(levels) == 0 || levels[len(levels)-1] < v {
			levels = append(levels, v)
		}
	}

	minIndent := 0
	if nextIdx >= 0 {
		next := &s.doc.Blocks[nextIdx]
		if (next.Kind.IsList() || next.Indent > 0) && next.Indent > 0 {
			minIndent = next.Indent
		}
	}
	if minIndent > 0 {
		filtered := levels[:0]
		for _, v := range levels {
			if v >= minIndent {
				filtered = append(filtered, v)
			}
		}
		if len(filtered) > 0 {
			levels = filtered
		} else {
			levels = []int{levels[len(levels)-1]}
		}
	}
	return levels
}

func (s *Session) resolveHorizontalDropIndent(levels []int, origIndent int, wx float32, gapKey [2]int) int {
	if len(levels) == 0 {
		return 0
	}
	baseIdx := 0
	bestDiff := int(1e9)
	for i, v := range levels {
		d := v - origIndent
		if d < 0 {
			d = -d
		}
		if d < bestDiff {
			bestDiff = d
			baseIdx = i
		}
	}
	if len(levels) == 1 {
		s.dragLevelKey = gapKey
		s.dragLevelOffset = 0
		return levels[0]
	}

	dx := wx - s.dragStartPt.X
	firstStep := s.scale(24)
	nextStep := s.scale(28)
	hyst := s.scale(6)
	threshold := func(step int) float32 {
		return firstStep + float32(step-1)*nextStep
	}

	raw := 0
	if dx >= firstStep {
		raw = 1 + int((dx-firstStep)/nextStep)
	} else if dx <= -firstStep {
		raw = -(1 + int((-dx-firstStep)/nextStep))
	}

	cur := raw
	if s.dragLevelKey == gapKey {
		cur = s.dragLevelOffset
		for cur < raw {
			var bound float32
			if cur >= 0 {
				bound = threshold(cur + 1)
			} else {
				bound = -(threshold(-cur) - hyst)
			}
			if dx >= bound {
				cur++
			} else {
				break
			}
		}
		for cur > raw {
			var bound float32
			if cur <= 0 {
				bound = -threshold(-cur + 1)
			} else {
				bound = threshold(cur) - hyst
			}
			if dx <= bound {
				cur--
			} else {
				break
			}
		}
	}

	chosen := min(max(baseIdx+cur, 0), len(levels)-1)
	s.dragLevelKey = gapKey
	s.dragLevelOffset = chosen - baseIdx
	return levels[chosen]
}

func (s *Session) updateBlockDropTarget(fromIdx int, wx, wy float32) {
	if s.doc == nil || fromIdx < 0 || fromIdx >= len(s.doc.Blocks) || !s.isPointerInDropViewport(wx, wy) {
		s.dragOverIdx = -1
		s.dragTargetIdx = -1
		s.dragSlot = -1
		s.dragAfter = false
		return
	}
	last := s.dragSubtreeEnd(fromIdx)
	vis := make([]dragVisBlock, 0, len(s.doc.Blocks))
	selfPos := -1
	for i := range s.doc.Blocks {
		if i > fromIdx && i <= last {
			continue
		}
		if len(s.folded) > 0 && s.isBlockHiddenByFold(i) {
			continue
		}
		r, ok := s.blockRect(s.doc.Blocks[i].ID)
		if !ok || r.H <= 0 {
			continue
		}
		if s.dragCollapsed && i == fromIdx {
			if compactH := s.dragCompactRowHeight(fromIdx); r.H > compactH {
				r.H = compactH
			}
		}
		if i == fromIdx {
			selfPos = len(vis)
		}
		vis = append(vis, dragVisBlock{idx: i, rect: r})
	}
	if len(vis) == 0 {
		s.dragOverIdx = -1
		s.dragTargetIdx = -1
		s.dragSlot = -1
		s.dragAfter = false
		return
	}

	// Pick vertical gap boundary purely by block midpoints + light hysteresis;
	// vertical Y never alters indentation.
	vHyst := s.scale(3)
	slotIdx := len(vis)
	for k, vb := range vis {
		mid := vb.rect.Y + vb.rect.H*0.5
		if s.dragSlot == k {
			mid += vHyst
		} else if s.dragSlot == k+1 {
			mid -= vHyst
		}
		if wy < mid {
			slotIdx = k
			break
		}
	}
	s.dragSlot = slotIdx

	count := last - fromIdx + 1
	var to int
	if selfPos >= 0 && (slotIdx == selfPos || slotIdx == selfPos+1) {
		to = fromIdx
	} else {
		var docSlot int
		if slotIdx < len(vis) {
			docSlot = vis[slotIdx].idx
		} else {
			docSlot = len(s.doc.Blocks)
		}
		if docSlot <= fromIdx {
			to = docSlot
		} else {
			to = docSlot - count
		}
	}

	prevPos := slotIdx - 1
	if prevPos >= 0 && vis[prevPos].idx == fromIdx {
		prevPos--
	}
	prevIdx := -1
	if prevPos >= 0 {
		prevIdx = vis[prevPos].idx
	}
	nextPos := slotIdx
	if nextPos < len(vis) && vis[nextPos].idx == fromIdx {
		nextPos++
	}
	nextIdx := -1
	if nextPos < len(vis) {
		nextIdx = vis[nextPos].idx
	}

	levels := s.allowedDropIndents(fromIdx, last, prevIdx, nextIdx)
	s.dragIndent = s.resolveHorizontalDropIndent(levels, s.doc.Blocks[fromIdx].Indent, wx, [2]int{prevIdx, nextIdx})

	if to == fromIdx && s.dragIndent == s.doc.Blocks[fromIdx].Indent {
		s.dragOverIdx = -1
		s.dragTargetIdx = -1
		s.dragAfter = false
		return
	}

	s.dragTargetIdx = to
	if selfPos >= 0 && (slotIdx == selfPos || slotIdx == selfPos+1) {
		s.dragOverIdx = fromIdx
		s.dragAfter = false
	} else if slotIdx < len(vis) {
		s.dragOverIdx = vis[slotIdx].idx
		s.dragAfter = false
	} else {
		s.dragOverIdx = vis[len(vis)-1].idx
		s.dragAfter = true
	}
}

func (s *Session) dragSubtreeEnd(start int) int {
	end := start
	if s.doc == nil || start < 0 || start >= len(s.doc.Blocks) {
		return end
	}
	b := &s.doc.Blocks[start]
	if b.Kind.IsHeading() && s.folded[b.ID] {
		return max(s.foldSectionEnd(start)-1, start)
	}
	if !b.Kind.IsList() {
		return end
	}
	for end+1 < len(s.doc.Blocks) && s.doc.Blocks[end+1].Indent > b.Indent {
		end++
	}
	return end
}

func (s *Session) resolvedMoveTarget(from int) int {
	if s.doc == nil || from < 0 || from >= len(s.doc.Blocks) || s.dragOverIdx < 0 {
		return -1
	}
	return s.dragTargetIdx
}

func (s *Session) handleBlockGripGesture(c *ui.Context, grip ui.Element, idx int, id model.BlockID) {
	dx, dy, held := grip.Dragged()
	if grip.PressCanceled() {
		if s.dragBlockID == id {
			s.resetBlockDrag()
			c.Invalidate()
		}
		return
	}

	if held {
		if s.dragBlockID != id {
			s.resetBlockDrag()
			s.dragBlockID = id
			s.mouseMode = mouseSelNone
			s.mouseOriginBlock = -1
			s.mouseDragged = false
			s.pressJustBegan = false
			s.firstLineDeleteArmed = false
			s.closeTransientOverlays()
			wx, wy := s.dragPointerPoint(grip)
			s.dragStartPt = ui.Point{X: wx - dx, Y: wy - dy}
		}
		if s.dragCanceled {
			return
		}
		wx, wy := s.dragPointerPoint(grip)
		if !s.dragActive {
			if math.Hypot(float64(wx-s.dragStartPt.X), float64(wy-s.dragStartPt.Y)) >= blockDragThreshold {
				s.activateBlockDrag(idx)
				c.Invalidate()
			}
		}
		if s.dragActive {
			prevOver, prevAfter, prevIndent := s.dragOverIdx, s.dragAfter, s.dragIndent
			s.updateBlockDropTarget(idx, wx, wy)
			s.autoScrollOnDrag(c, wy)
			if s.dragOverIdx != prevOver || s.dragAfter != prevAfter || s.dragIndent != prevIndent {
				c.Invalidate()
			}
		}
		return
	}

	if s.dragBlockID == id {
		wasActive := s.dragActive
		wasCanceled := s.dragCanceled
		var wx, wy float32
		if wasActive && !wasCanceled {
			wx, wy = s.dragPointerPoint(grip)
			s.updateBlockDropTarget(idx, wx, wy)
		}
		target := s.resolvedMoveTarget(idx)
		indent := s.dragIndent
		s.resetBlockDrag()
		if wasCanceled {
			c.Invalidate()
			return
		}
		if wasActive {
			if target >= 0 && s.isPointerInDropViewport(wx, wy) {
				s.dragDropPending = true
				s.dragDropFrom = idx
				s.dragDropTo = target
				s.dragDropIndent = indent
			}
			c.Invalidate()
			return
		}
	}

	if grip.Clicked() {
		s.pressJustBegan = false
		s.firstLineDeleteArmed = false
		if idx >= 0 && idx < len(s.doc.Blocks) && s.doc.Blocks[idx].Kind == model.BlockTable {
			s.toggleTableWholeMenu(idx, id)
			return
		}
		s.FocusBlock(idx)
		trigger := grip.Bounds()
		if trigger.W <= 0 {
			trigger = s.blockRectValue(id)
		}
		s.openHandleMenu(idx, trigger)
	}
}

func (s *Session) paintBlockDropMarker(p *ui.Painter, r ui.Rect, b *model.Block, pal theme.Palette) {
	if b == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	bleed := s.bandBleed()
	indentPx := float32(min(max(s.dragIndent, 0), 12)) * s.indentStep()
	leftX := r.X + s.leftContentGutter() - bleed + indentPx
	rightX := r.X + r.W - s.rightContentInset() + bleed
	lineW := max(rightX-leftX, 0)
	if lineW <= 0 {
		return
	}
	lineY := r.Y
	if s.dragAfter {
		lineY = r.Y + r.H - 3
	}
	p.Fill(ui.Rect{X: leftX, Y: lineY, W: lineW, H: 3}, pal.Accent, 0)
	p.Fill(ui.Rect{X: leftX - 3, Y: lineY - 2, W: 7, H: 7}, pal.Accent, 3.5)
}
