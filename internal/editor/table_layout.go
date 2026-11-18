package editor

import (
	"math"

	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

type cachedTableCols struct {
	rev    uint64
	rows   int
	cols   int
	font   string
	fontSz float32
	padX   float32
	wantW  []float32
}

type tableGridGeom struct {
	docY float32
	h    float32
}

// resolveTableColWidths computes shared column widths across all rows of tbl so every row
// aligns to identical column boundaries, returning per-column interior widths and whether
// horizontal scrolling is required.
func (s *Session) resolveTableColWidths(id model.BlockID, tbl *model.TableData, availGrid float32) (colWidths []float32, needHScroll bool) {
	if tbl == nil || len(tbl.Rows) == 0 || len(tbl.Rows[0]) == 0 {
		return nil, false
	}
	cols := len(tbl.Rows[0])
	tFontSz, _, _, tMinColW, tMaxColW, _, tPadX := s.tableMetrics()
	edFont := resolvedEditorFont()
	lhFactor := tableCellLineHeight / tableCellFontSize

	if s.tableColCache == nil {
		s.tableColCache = make(map[model.BlockID]cachedTableCols)
	}
	cached, ok := s.tableColCache[id]
	if !ok || cached.rev != s.docRev || cached.rows != len(tbl.Rows) || cached.cols != cols ||
		cached.font != edFont || cached.fontSz != tFontSz || cached.padX != tPadX || len(cached.wantW) != cols {
		wantW := make([]float32, cols)
		for c := 0; c < cols; c++ {
			wantW[c] = tMinColW
		}
		for rIdx, row := range tbl.Rows {
			weight := theme.WeightRegular
			if rIdx == 0 {
				weight = theme.WeightTitle
			}
			fnt := ui.Font{
				Family:     edFont,
				Size:       tFontSz,
				Weight:     weight,
				LineHeight: lhFactor,
			}
			for cIdx := 0; cIdx < cols && cIdx < len(row); cIdx++ {
				cell := &row[cIdx]
				if cell.PlainText() == "" {
					continue
				}
				cellBlk := model.Block{ID: id, Kind: model.BlockParagraph, Spans: cell.Spans}
				spans := s.buildPureDisplaySpans(&cellBlk, tFontSz, weight)
				if len(spans) == 0 {
					continue
				}
				lay := ui.ShapeRichTextWith(spans, fnt, ui.TextShape{Width: 1e5})
				if lay == nil {
					continue
				}
				tw, _ := lay.Size()
				need := float32(math.Ceil(float64(tw))) + 2*tPadX + 4
				if need > wantW[cIdx] {
					wantW[cIdx] = min(need, tMaxColW)
				}
			}
		}
		cached = cachedTableCols{
			rev:    s.docRev,
			rows:   len(tbl.Rows),
			cols:   cols,
			font:   edFont,
			fontSz: tFontSz,
			padX:   tPadX,
			wantW:  wantW,
		}
		s.tableColCache[id] = cached
	}

	borderOverhead := float32(cols+1) * appUi.TableGridRuleWidth
	innerAvail := max(availGrid-borderOverhead, float32(cols))
	z := s.zoom()

	want := make([]float32, cols)
	pinned := make([]bool, cols)
	hasPinned := false
	for c := 0; c < cols; c++ {
		want[c] = cached.wantW[c]
		if c < len(tbl.Widths) && tbl.Widths[c] > 0 {
			want[c] = max(tbl.Widths[c]*z, tMinColW)
			pinned[c] = true
			hasPinned = true
		}
		if s.tableResizeBlock == id && s.tableResizeCol == c && s.tableResizeCurW > 0 {
			want[c] = max(s.tableResizeCurW*z, tMinColW)
			pinned[c] = true
			hasPinned = true
		}
	}

	var sumWant, sumMin float32
	for c := 0; c < cols; c++ {
		sumWant += want[c]
		if pinned[c] {
			sumMin += want[c]
		} else {
			sumMin += tMinColW
		}
	}

	colWidths = make([]float32, cols)
	if sumMin > innerAvail {
		for c, w := range want {
			colWidths[c] = float32(math.Round(float64(max(w, tMinColW))))
		}
		return colWidths, true
	}

	raw := make([]float32, cols)
	if !hasPinned {
		if sumWant <= innerAvail {
			extra := innerAvail - sumWant
			for c, w := range want {
				if sumWant > 0 {
					raw[c] = w + extra*(w/sumWant)
				} else {
					raw[c] = innerAvail / float32(cols)
				}
			}
		} else {
			room := innerAvail - sumMin
			flexSpan := max(sumWant-sumMin, 1)
			for c, w := range want {
				raw[c] = tMinColW + room*(max(w-tMinColW, 0)/flexSpan)
			}
		}
	} else {
		var pinnedSum, unpinnedWant, unpinnedMin float32
		unpinnedCount := 0
		for c, w := range want {
			if pinned[c] {
				pinnedSum += w
			} else {
				unpinnedWant += w
				unpinnedMin += tMinColW
				unpinnedCount++
			}
		}
		rem := max(innerAvail-pinnedSum, unpinnedMin)
		for c, w := range want {
			if pinned[c] {
				raw[c] = w
				continue
			}
			if rem >= unpinnedWant {
				extra := rem - unpinnedWant
				if unpinnedWant > 0 {
					raw[c] = w + extra*(w/unpinnedWant)
				} else {
					raw[c] = rem / float32(max(unpinnedCount, 1))
				}
			} else {
				room := max(rem-unpinnedMin, 0)
				flexSpan := max(unpinnedWant-unpinnedMin, 1)
				raw[c] = tMinColW + room*(max(w-tMinColW, 0)/flexSpan)
			}
		}
		if unpinnedCount == 0 && pinnedSum < innerAvail {
			raw[cols-1] += innerAvail - pinnedSum
		}
	}

	var cum, prevEdge float32
	for c := 0; c < cols; c++ {
		cum += raw[c]
		edge := float32(math.Round(float64(cum)))
		if c == cols-1 {
			edge = float32(math.Round(float64(innerAvail)))
		}
		colWidths[c] = max(edge-prevEdge, 1)
		prevEdge = edge
	}
	return colWidths, false
}

func tableGridCell(c *ui.Context, row, col int, width float32, fixedWidth bool) ui.Element {
	cell := ui.Column(c.Key(tableCellPosKey{row: row, col: col})).
		Justify(ui.Center).AlignItems(ui.Stretch).MinWidth(0)
	if fixedWidth {
		return cell.Width(width).Shrink(0)
	}
	return cell.Basis(width).Grow(width).Shrink(1)
}

func renderPlainTableRows[T any](c *ui.Context, grid ui.Element, rows [][]T, fontSize, zoom float32, cellText func(T) string) {
	cols := 0
	for _, row := range rows {
		cols = max(cols, len(row))
	}
	pal := theme.Ocean()
	grid.Children(func() {
		for r, row := range rows {
			bg, weight := pal.Paper, theme.WeightRegular
			if r == 0 {
				bg, weight = pal.Table().HeaderBG, theme.WeightTitle
			}
			appUi.TableGridRow(c.Key(tableRowUIKey{row: r})).Children(func() {
				for col := 0; col < cols; col++ {
					text := ""
					if col < len(row) {
						text = cellText(row[col])
					}
					tableGridCell(c, r, col, 1, false).Padding(theme.Space6*zoom, theme.Space8*zoom).
						Background(bg).Children(func() {
						ui.Text(c, text).FontSize(fontSize).FontWeight(weight).TextColor(pal.Text).FillWidth()
					})
				}
			})
		}
	})
}

// tableAddColIconTop returns the top offset inside table-add-col so the '+' icon stays
// centered within the visible intersection of the table and the editor scroll viewport.
func (s *Session) tableAddColIconTop(id model.BlockID, slotInset, iconSz float32) (top float32, ok bool) {
	geom, hasGeom := s.tableGridGeoms[id]
	if !hasGeom || geom.h <= 0 || s.scrollRect.H <= 0 {
		return 0, false
	}
	slotTop := s.scrollRect.Y + geom.docY - s.bodyScroll.Y + slotInset
	slotH := max(geom.h-2*slotInset, iconSz)
	slotBottom := slotTop + slotH
	vpTop := s.scrollRect.Y
	vpBottom := s.scrollRect.Y + s.scrollRect.H
	visTop := max(slotTop, vpTop)
	visBottom := min(slotBottom, vpBottom)
	if visBottom <= visTop {
		return max((slotH-iconSz)*0.5, 0), true
	}
	pad := min(s.scale(6), max((slotH-iconSz)*0.5, 0))
	rawTop := (visTop+visBottom)*0.5 - slotTop - iconSz*0.5
	return clampFloat32(rawTop, pad, max(slotH-iconSz-pad, 0)), true
}
