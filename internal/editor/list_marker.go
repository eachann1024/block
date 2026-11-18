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

func drawListGuides(c *ui.Context, id model.BlockID, ctx listCtx, leftGutter float32) {
	if len(ctx.Ancestors) == 0 {
		return
	}
	pal := theme.Ocean()
	for i, anc := range ctx.Ancestors {
		x := float32(math.Round(float64(leftGutter + float32(min(anc.Indent, 12))*editorIndentStep + anc.Center)))
		g := ui.Box(c.Key(fmt.Sprintf("list-guide-%d-%d", id, i))).Absolute().Top(0).Bottom(0).Left(x).Width(1).
			Background(pal.ListGuide).IgnoreInput(true)
		appUi.Probe(g, fmt.Sprintf("list-guide-%d-%d", id, i))
	}
}

func (s *Session) renderListMarker(c *ui.Context, idx int, b *model.Block, ctx listCtx, size, lineH float32, readOnly bool) {
	pal := theme.Ocean()
	depth := len(ctx.Ancestors)
	switch b.Kind {
	case model.BlockBullet:
		d := float32(5)
		if depth == 0 {
			d = 6
		}
		cell := ui.Row(c.Key("bullet-cell")).Height(lineH).Padding(0, 11-d/2, 0, 0).AlignItems(ui.Center)
		cell.Children(func() {
			dot := ui.Box(c.Key(fmt.Sprintf("list-bullet-%d", b.ID))).Size(d, d).Radius(d / 2)
			switch depth {
			case 0:
				dot.Background(pal.Accent)
			case 1:
				dot.Background(pal.ListBullet)
			default:
				dot.Border(1.25, pal.ListBullet)
			}
			appUi.Probe(dot, fmt.Sprintf("list-bullet-%d", b.ID))
		})
	case model.BlockNumbered:
		col := pal.TextSecondary
		if depth == 0 {
			col = pal.Accent
		}
		cell := ui.Row(c.Key("num-cell")).Height(lineH).Padding(0, 8, 0, 0).AlignItems(ui.Center)
		cell.Children(func() {
			txt := ui.Text(c, fmt.Sprintf("%d", max(ctx.Number, 1))).Font(resolvedCodeFont()).
				FontSize(size * 0.84).FontWeight(theme.WeightMonoMedium).TextColor(col).SingleLine()
			appUi.Probe(txt, fmt.Sprintf("list-number-%d", b.ID))
		})
	case model.BlockTodo:
		s.renderTodoCheckbox(c, idx, b.Checked, readOnly, lineH)
	}
}

func (s *Session) renderTodoCheckbox(c *ui.Context, idx int, checked, readOnly bool, lineH float32) {
	pal := theme.Ocean()
	id := s.doc.Blocks[idx].ID
	top := max((lineH-16)*0.5, 0)
	cb := ui.ButtonBase(c.Key(fmt.Sprintf("todo-checkbox-%d", id))).Size(16, 16).Margin(top, 0, 0, 4.8).
		Center().Radius(theme.RadiusMicroMD).Background(pal.TodoBG).Cursor(ui.CursorPointer).Disabled(readOnly).
		Label(theme.Tr("待办复选框", "Todo checkbox"))
	if cb.Hovered() && !readOnly {
		cb.Border(1, pal.TodoRingHover)
	} else {
		cb.Border(1, pal.TodoRing)
	}
	cb.Children(func() {
		idLayer := ui.Box(c).Absolute().Left(0).Top(0).Size(16, 16).PassThrough()
		appUi.Probe(idLayer, fmt.Sprintf("todo-checkbox-%d", id))
		if checked {
			icons.Draw(c, icons.Check, theme.IconMicro, pal.MonoIconColors(pal.Accent))
		}
	})
	appUi.Probe(cb, "todo-checkbox")
	if cb.Clicked() && !readOnly {
		s.ToggleTodo(idx)
	}
}
