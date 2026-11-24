package editor

import (
	"fmt"
	"math"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	listMarkCenter = editorMarkerWidth - 11
	listTodoCenter = 12.8
	listMaxScan    = 256
)

type listAncestor struct {
	Indent int
	Center float32
}

type listCtx struct {
	Number       int
	Ancestors    []listAncestor
	HasSelfGuide bool
	SelfGuide    listAncestor
}

type frameBlockMeta struct {
	Hidden      bool
	PrevVisible *model.Block
	List        listCtx
}

type foldFrameEntry struct {
	level     int
	tailStart int
}

type ancStackEntry struct {
	idx         int
	lastDescIdx int
	anc         listAncestor
}

type listGuideUIKey struct {
	id  model.BlockID
	idx int
}

type listSelfGuideUIKey struct {
	id model.BlockID
}

func isPlainEmptyListBridgeBlock(b *model.Block) bool {
	return b.Kind == model.BlockParagraph && b.Indent == 0 && b.QuoteDepth == 0 && len(b.Spans) == 0
}

func listMarkerCenter(kind model.BlockKind) float32 {
	if kind == model.BlockTodo {
		return listTodoCenter
	}
	return listMarkCenter
}

func ancFitsLeftOfBlock(anc listAncestor, b *model.Block) bool {
	if anc.Indent >= b.Indent {
		return false
	}
	if b.Kind.IsList() {
		return true
	}
	bleed := float32(0)
	if isSurfaceBleedBlock(b.Kind) {
		bleed = editorBandBleed
	}
	guideX := float32(min(anc.Indent, 12))*editorIndentStep + anc.Center
	contentLeftX := float32(min(max(b.Indent, 0), 12))*editorIndentStep - bleed
	return guideX+theme.Space2 <= contentLeftX
}

// prepareFrameBlocks computes fold visibility, previous visible sibling, and list
// numbering/guide ancestors for every block in a single O(N) pass per frame.
func (s *Session) prepareFrameBlocks() []frameBlockMeta {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return nil
	}
	blocks := s.doc.Blocks
	n := len(blocks)
	if cap(s.frameBlocks) < n {
		s.frameBlocks = make([]frameBlockMeta, n)
	} else {
		s.frameBlocks = s.frameBlocks[:n]
	}
	meta := s.frameBlocks

	if len(s.folded) == 0 {
		for i := range meta {
			meta[i].Hidden = false
		}
	} else {
		var idToIdx map[model.BlockID]int
		if len(s.foldTails) > 0 {
			idToIdx = make(map[model.BlockID]int, n)
			for i := range blocks {
				idToIdx[blocks[i].ID] = i
			}
		}
		var foldStack [6]foldFrameEntry
		foldLen := 0
		for i := 0; i < n; i++ {
			b := &blocks[i]
			if b.Kind.IsHeading() {
				lvl := b.Kind.HeadingLevel()
				for foldLen > 0 && foldStack[foldLen-1].level >= lvl {
					foldLen--
				}
			}
			hidden := false
			if i > 0 && foldLen > 0 {
				for k := 0; k < foldLen; k++ {
					ts := foldStack[k].tailStart
					if ts <= 0 || i < ts {
						hidden = true
						break
					}
				}
			}
			meta[i].Hidden = hidden
			if b.Kind.IsHeading() && s.folded[b.ID] {
				tailStart := -1
				if idToIdx != nil {
					if tailID, ok := s.foldTails[b.ID]; ok && tailID != 0 {
						if tIdx, exists := idToIdx[tailID]; exists && tIdx > i {
							tailStart = tIdx
						}
					}
				}
				if foldLen < len(foldStack) {
					foldStack[foldLen] = foldFrameEntry{level: b.Kind.HeadingLevel(), tailStart: tailStart}
					foldLen++
				}
			}
		}
	}
	if s.dragActive && s.dragCollapsed && s.dragBlockID != 0 {
		if from := s.blockIndexByID(s.dragBlockID); from >= 0 {
			end := s.dragSubtreeEnd(from)
			for i := from + 1; i <= end && i < n; i++ {
				meta[i].Hidden = true
			}
		}
	}

	var prevVis *model.Block
	var numbering model.NumberedListMarkers
	var ancStack [16]ancStackEntry
	ancLen := 0
	lastBlankIdx := -1

	finalizeAnc := func(e ancStackEntry) {
		if e.lastDescIdx <= e.idx {
			return
		}
		meta[e.idx].List.HasSelfGuide = true
		meta[e.idx].List.SelfGuide = e.anc
		if lastBlankIdx > e.idx {
			for j := e.idx + 1; j < e.lastDescIdx; j++ {
				if !meta[j].Hidden && isPlainEmptyListBridgeBlock(&blocks[j]) {
					meta[j].List.Ancestors = append(meta[j].List.Ancestors, e.anc)
				}
			}
		}
	}

	for i := 0; i < n; i++ {
		b := &blocks[i]
		isPlainEmptyZero := isPlainEmptyListBridgeBlock(b)
		num := numbering.Next(*b)
		if !isPlainEmptyZero {
			for ancLen > 0 && ancStack[ancLen-1].anc.Indent >= b.Indent {
				ancLen--
				finalizeAnc(ancStack[ancLen])
			}
		}

		var ancestors []listAncestor
		if !meta[i].Hidden {
			if isPlainEmptyZero {
				lastBlankIdx = i
			} else if (b.Kind.IsList() || b.Indent > 0) && b.Indent > 0 && ancLen > 0 {
				for k := ancLen - 1; k >= 0; k-- {
					if i-ancStack[k].idx > listMaxScan {
						break
					}
					if ancFitsLeftOfBlock(ancStack[k].anc, b) {
						ancestors = append(ancestors, ancStack[k].anc)
						ancStack[k].lastDescIdx = i
					}
				}
			}
			if b.Kind.IsList() && ancLen < len(ancStack) {
				ancStack[ancLen] = ancStackEntry{
					idx:         i,
					lastDescIdx: -1,
					anc:         listAncestor{Indent: b.Indent, Center: listMarkerCenter(b.Kind)},
				}
				ancLen++
			}
			meta[i].PrevVisible = prevVis
			meta[i].List = listCtx{Number: num, Ancestors: ancestors}
			prevVis = b
		} else {
			meta[i].PrevVisible = nil
			meta[i].List = listCtx{}
		}
	}

	for ancLen > 0 {
		ancLen--
		finalizeAnc(ancStack[ancLen])
	}

	return meta
}

func (s *Session) hasVisibleListDescendant(ancIdx, fromIdx int, anc listAncestor) bool {
	if s.dragActive && s.dragCollapsed && s.dragBlockID == s.doc.Blocks[ancIdx].ID {
		return false
	}
	limit := min(len(s.doc.Blocks)-1, ancIdx+listMaxScan)
	for next := fromIdx; next <= limit; next++ {
		if s.isBlockHiddenByFold(next) {
			continue
		}
		nb := &s.doc.Blocks[next]
		if isPlainEmptyListBridgeBlock(nb) {
			continue
		}
		if nb.Indent <= anc.Indent {
			return false
		}
		if ancFitsLeftOfBlock(anc, nb) {
			return true
		}
	}
	return false
}

func (s *Session) computeListCtx(idx int) listCtx {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return listCtx{}
	}
	b := &s.doc.Blocks[idx]
	num := 0
	if b.Kind == model.BlockNumbered {
		num = s.numberedListIndex(idx)
	}
	if s.isBlockHiddenByFold(idx) {
		return listCtx{Number: num}
	}
	isBridge := isPlainEmptyListBridgeBlock(b)
	if !isBridge && !b.Kind.IsList() && b.Indent <= 0 {
		return listCtx{Number: num}
	}

	var ancestors []listAncestor
	threshold := b.Indent
	if isBridge {
		threshold = 13
	}
	start := max(idx-listMaxScan, 0)
	for prev := idx - 1; prev >= start && threshold > 0; prev-- {
		if s.isBlockHiddenByFold(prev) {
			continue
		}
		pb := &s.doc.Blocks[prev]
		if isPlainEmptyListBridgeBlock(pb) {
			continue
		}
		if !pb.Kind.IsList() {
			if pb.Indent < threshold {
				threshold = pb.Indent
			}
			continue
		}
		if pb.Indent < threshold {
			anc := listAncestor{Indent: pb.Indent, Center: listMarkerCenter(pb.Kind)}
			if isBridge {
				if s.hasVisibleListDescendant(prev, idx+1, anc) {
					ancestors = append(ancestors, anc)
				}
			} else if ancFitsLeftOfBlock(anc, b) {
				ancestors = append(ancestors, anc)
			}
			threshold = pb.Indent
		}
	}

	ctx := listCtx{Number: num, Ancestors: ancestors}
	if b.Kind.IsList() {
		self := listAncestor{Indent: b.Indent, Center: listMarkerCenter(b.Kind)}
		if s.hasVisibleListDescendant(idx, idx+1, self) {
			ctx.HasSelfGuide = true
			ctx.SelfGuide = self
		}
	}
	return ctx
}

func listStemTopOffset(kind model.BlockKind, depth int, fontSize, lineH, zoom float32) float32 {
	pad := theme.Space4 * zoom
	var markerBottom float32
	switch kind {
	case model.BlockBullet:
		d := float32(5) * zoom
		if depth == 0 {
			d = 6 * zoom
		}
		markerBottom = lineH*0.5 + d*0.5
	case model.BlockNumbered:
		markerBottom = lineH*0.5 + fontSize*0.42
	case model.BlockTodo:
		boxSz := 16 * zoom
		markerBottom = max((lineH-boxSz)*0.5, 0) + boxSz
	default:
		return lineH
	}
	return min(markerBottom+pad, max(lineH-1, markerBottom))
}

func listGuideX(leftGutter, zoom float32, anc listAncestor) float32 {
	return float32(math.Round(float64(leftGutter + float32(min(anc.Indent, 12))*editorIndentStep*zoom + anc.Center*zoom)))
}

func drawListGuides(c *ui.Context, id model.BlockID, ctx listCtx, leftGutter, zoom, selfStemTop float32, showSelfStem bool) {
	if len(ctx.Ancestors) == 0 && !(showSelfStem && ctx.HasSelfGuide) {
		return
	}
	if zoom <= 0 {
		zoom = 1.0
	}
	pal := theme.Ocean()
	for i, anc := range ctx.Ancestors {
		x := listGuideX(leftGutter, zoom, anc)
		g := ui.Box(c.Key(listGuideUIKey{id: id, idx: i})).Absolute().Top(0).Bottom(0).Left(x).Width(1).
			Background(pal.ListGuide).IgnoreInput(true)
		if appUi.IsProbeEnabled() {
			appUi.Probe(g, fmt.Sprintf("list-guide-%d-%d", id, i))
		}
	}
	if showSelfStem && ctx.HasSelfGuide {
		x := listGuideX(leftGutter, zoom, ctx.SelfGuide)
		g := ui.Box(c.Key(listSelfGuideUIKey{id: id})).Absolute().Top(selfStemTop).Bottom(0).Left(x).Width(1).
			Background(pal.ListGuide).IgnoreInput(true)
		if appUi.IsProbeEnabled() {
			appUi.Probe(g, fmt.Sprintf("list-stem-%d", id))
		}
	}
}

func (s *Session) renderListMarker(c *ui.Context, idx int, b *model.Block, ctx listCtx, size, lineH float32, readOnly bool) {
	pal := theme.Ocean()
	z := s.zoom()
	depth := len(ctx.Ancestors)
	switch b.Kind {
	case model.BlockBullet:
		d := float32(5) * z
		if depth == 0 {
			d = 6 * z
		}
		cell := ui.Row(c.Key("bullet-cell")).Height(lineH).Padding(0, 11*z-d/2, 0, 0).AlignItems(ui.Center)
		cell.Children(func() {
			dot := ui.Box(c.Key(b.ID)).Size(d, d).Radius(d / 2)
			switch depth {
			case 0:
				dot.Background(pal.Accent)
			case 1:
				dot.Background(pal.ListBullet)
			default:
				dot.Border(max(1, 1.25*z), pal.ListBullet)
			}
			if appUi.IsProbeEnabled() {
				appUi.Probe(dot, fmt.Sprintf("list-bullet-%d", b.ID))
			}
		})
	case model.BlockNumbered:
		col := pal.TextSecondary
		if depth == 0 {
			col = pal.Accent
		}
		cell := ui.Row(c.Key("num-cell")).Height(lineH).Padding(0, 8*z, 0, 0).AlignItems(ui.Center)
		cell.Children(func() {
			txt := ui.Text(c, formatMenuTitle(max(ctx.Number, 1))).Font(resolvedCodeFont()).
				FontSize(size * 0.84).FontWeight(theme.WeightMono).TextColor(col).SingleLine()
			if appUi.IsProbeEnabled() {
				appUi.Probe(txt, fmt.Sprintf("list-number-%d", b.ID))
			}
		})
	case model.BlockTodo:
		s.renderTodoCheckbox(c, idx, b.Checked, readOnly, lineH)
	}
}

func (s *Session) renderTodoCheckbox(c *ui.Context, idx int, checked, readOnly bool, lineH float32) {
	pal := theme.Ocean()
	z := s.zoom()
	boxSz := 16 * z
	id := s.doc.Blocks[idx].ID
	top := max((lineH-boxSz)*0.5, 0)
	cb := ui.ButtonBase(c.Key(id)).Size(boxSz, boxSz).Margin(top, 0, 0, 4.8*z).
		Center().Radius(theme.RadiusSM * z).Cursor(ui.CursorPointer).Disabled(readOnly).
		Label(theme.Tr("待办复选框", "Todo checkbox"))
	hovered := cb.Hovered() && !readOnly
	pressed := cb.Pressed() && !readOnly
	bg := pal.TodoBG
	bd := pal.TodoRing
	if checked || hovered {
		bd = pal.TodoRingHover
	}
	if checked && hovered {
		bg = pal.Selected
	}
	appUi.AnimateSurfaceColors(cb, bg, bd, true)
	appUi.TactileButton(cb, hovered, pressed, readOnly)
	targetChk := float32(0)
	if checked {
		targetChk = 1
	}
	chkP := cb.AnimateTo("todoChk", targetChk, appUi.SpringTactile)
	cb.Children(func() {
		if appUi.IsProbeEnabled() {
			idLayer := ui.Box(c).Absolute().Left(0).Top(0).Size(boxSz, boxSz).PassThrough()
			appUi.Probe(idLayer, fmt.Sprintf("todo-checkbox-%d", id))
		}
		if chkP > 0.01 {
			chkScale := 0.55 + 0.45*chkP
			chkAlpha := min(max(chkP, 0), 1)
			mark := ui.Box(c).Center().Opacity(chkAlpha)
			if float32(math.Abs(float64(chkScale-1))) > 0.001 {
				mark.Scale(chkScale, chkScale)
			}
			mark.Children(func() {
				icons.DrawAnimated(c, icons.Check, theme.IconMicro*z, pal.MinimalIconColors(pal.Accent), hovered || pressed || checked)
			})
		}
	})
	appUi.Probe(cb, "todo-checkbox")
	if cb.Clicked() && !readOnly {
		s.ToggleTodo(idx)
	}
}
