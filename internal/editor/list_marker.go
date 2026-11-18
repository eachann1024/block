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
	Number    int
	Ancestors []listAncestor
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
	idx int
	anc listAncestor
}

type listGuideUIKey struct {
	id  model.BlockID
	idx int
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
	var runLen [13]int
	var numBase [13]int
	var ancStack [16]ancStackEntry
	ancLen := 0

	for i := 0; i < n; i++ {
		b := &blocks[i]
		isPlainEmptyZero := b.Kind == model.BlockParagraph && b.Indent == 0 && len(b.Spans) == 0
		num := 0
		if !isPlainEmptyZero {
			ind := min(max(b.Indent, 0), 12)
			for d := ind + 1; d <= 12; d++ {
				runLen[d] = 0
				numBase[d] = 0
			}
			if b.Kind == model.BlockNumbered {
				runLen[ind]++
				if numBase[ind] > 1 {
					num = numBase[ind] + runLen[ind] - 1
				} else if b.ListIndex > 1 {
					num = b.ListIndex
				} else {
					num = runLen[ind]
				}
				if b.ListIndex > 1 {
					numBase[ind] = b.ListIndex
					runLen[ind] = 1
				}
			} else {
				runLen[ind] = 0
				numBase[ind] = 0
			}
		}

		var ancestors []listAncestor
		if !meta[i].Hidden && (b.Kind.IsList() || b.Indent > 0) && b.Indent > 0 && ancLen > 0 {
			for k := ancLen - 1; k >= 0; k-- {
				if i-ancStack[k].idx > listMaxScan {
					break
				}
				if ancStack[k].anc.Indent < b.Indent {
					ancestors = append(ancestors, ancStack[k].anc)
				}
			}
		}

		if !b.Kind.IsList() {
			if b.Indent <= 0 {
				ancLen = 0
			}
		} else {
			for ancLen > 0 && ancStack[ancLen-1].anc.Indent >= b.Indent {
				ancLen--
			}
			center := float32(listMarkCenter)
			if b.Kind == model.BlockTodo {
				center = listTodoCenter
			}
			if ancLen < len(ancStack) {
				ancStack[ancLen] = ancStackEntry{
					idx: i,
					anc: listAncestor{Indent: b.Indent, Center: center},
				}
				ancLen++
			}
		}

		if !meta[i].Hidden {
			meta[i].PrevVisible = prevVis
			meta[i].List = listCtx{Number: num, Ancestors: ancestors}
			prevVis = b
		} else {
			meta[i].PrevVisible = nil
			meta[i].List = listCtx{}
		}
	}

	return meta
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
	if !b.Kind.IsList() && b.Indent <= 0 {
		return listCtx{Number: num}
	}
	var ancestors []listAncestor
	threshold := b.Indent
	start := max(idx-listMaxScan, 0)
	for prev := idx - 1; prev >= start && threshold > 0; prev-- {
		pb := &s.doc.Blocks[prev]
		if !pb.Kind.IsList() {
			if pb.Indent > 0 {
				continue
			}
			break
		}
		if pb.Indent < threshold {
			center := listMarkCenter
			if pb.Kind == model.BlockTodo {
				center = listTodoCenter
			}
			ancestors = append(ancestors, listAncestor{Indent: pb.Indent, Center: center})
			threshold = pb.Indent
		}
	}
	return listCtx{Number: num, Ancestors: ancestors}
}

func drawListGuides(c *ui.Context, id model.BlockID, ctx listCtx, leftGutter, zoom float32) {
	if len(ctx.Ancestors) == 0 {
		return
	}
	if zoom <= 0 {
		zoom = 1.0
	}
	pal := theme.Ocean()
	for i, anc := range ctx.Ancestors {
		x := float32(math.Round(float64(leftGutter + float32(min(anc.Indent, 12))*editorIndentStep*zoom + anc.Center*zoom)))
		g := ui.Box(c.Key(listGuideUIKey{id: id, idx: i})).Absolute().Top(0).Bottom(0).Left(x).Width(1).
			Background(pal.ListGuide).IgnoreInput(true)
		if appUi.IsProbeEnabled() {
			appUi.Probe(g, fmt.Sprintf("list-guide-%d-%d", id, i))
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
		Center().Radius(theme.RadiusSM * z).Background(pal.TodoBG).Cursor(ui.CursorPointer).Disabled(readOnly).
		Label(theme.Tr("待办复选框", "Todo checkbox"))
	if cb.Hovered() && !readOnly {
		cb.Border(1, pal.TodoRingHover)
	} else {
		cb.Border(1, pal.TodoRing)
	}
	cb.Children(func() {
		if appUi.IsProbeEnabled() {
			idLayer := ui.Box(c).Absolute().Left(0).Top(0).Size(boxSz, boxSz).PassThrough()
			appUi.Probe(idLayer, fmt.Sprintf("todo-checkbox-%d", id))
		}
		if checked {
			icons.Draw(c, icons.Check, theme.IconMicro*z, pal.MonoIconColors(pal.Accent))
		}
	})
	appUi.Probe(cb, "todo-checkbox")
	if cb.Clicked() && !readOnly {
		s.ToggleTodo(idx)
	}
}
