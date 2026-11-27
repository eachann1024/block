package editor

import (
	"math"
	"slices"

	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

type cachedTableCols struct {
	hash   uint64
	rows   int
	cols   int
	font   string
	fontSz float32
	padX   float32
	wantW  []float32
	// Last fitted (non-scrolling) layout; reused while every column's content still fits so
	// inline style or text edits in one cell don't redistribute every column (width jitter).
	fitAvail  float32
	fitPinned []float32
	fitWidths []float32
}

func tableContentHash(tbl *model.TableData) uint64 {
	if tbl == nil {
		return 0
	}
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	mixByte := func(b byte) {
		h ^= uint64(b)
		h *= prime64
	}
	mixStr := func(s string) {
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= prime64
		}
		mixByte(0xff)
	}
	for _, a := range tbl.Aligns {
		mixStr(string(a))
	}
	mixByte(0xfe)
	for _, row := range tbl.Rows {
		for _, cell := range row {
			for _, sp := range cell.Spans {
				mixByte(byte(sp.Marks))
				mixStr(sp.Text)
			}
			mixByte(0xfd)
		}
		mixByte(0xfc)
	}
	return h
}

type tableGridGeom struct {
	docY float32
	h    float32
}

func tableCellTextWeight(tbl *model.TableData, row, col int) int {
	if tbl.IsHeaderCell(row, col) {
		return theme.WeightControl
	}
	return theme.WeightRegular
}

func tableCellTextAlign(tbl *model.TableData, row, col int) ui.Align {
	switch tbl.CellAlign(row, col) {
	case model.ColAlignCenter:
		return ui.Center
	case model.ColAlignRight:
		return ui.End
	}
	return ui.Start
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
	tblHash := tableContentHash(tbl)
	cached, ok := s.tableColCache[id]
	if !ok || cached.hash != tblHash || cached.rows != len(tbl.Rows) || cached.cols != cols ||
		cached.font != edFont || cached.fontSz != tFontSz || cached.padX != tPadX || len(cached.wantW) != cols {
		wantW := make([]float32, cols)
		for c := 0; c < cols; c++ {
			wantW[c] = tMinColW
		}
		for rIdx, row := range tbl.Rows {
			for cIdx := 0; cIdx < cols && cIdx < len(row); cIdx++ {
				cell := &row[cIdx]
				if cell.PlainText() == "" {
					continue
				}
				weight := tableCellTextWeight(tbl, rIdx, cIdx)
				fnt := ui.Font{
					Family:     edFont,
					Size:       tFontSz,
					Weight:     weight,
					LineHeight: lhFactor,
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
		prevFit := cached
		cached = cachedTableCols{
			fitAvail:  prevFit.fitAvail,
			fitPinned: prevFit.fitPinned,
			fitWidths: prevFit.fitWidths,
			hash:      tblHash,
			rows:      len(tbl.Rows),
			cols:      cols,
			font:      edFont,
			fontSz:    tFontSz,
			padX:      tPadX,
			wantW:     wantW,
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

	pinnedSig := make([]float32, cols)
	for c := 0; c < cols; c++ {
		if pinned[c] {
			pinnedSig[c] = want[c]
		}
	}
	if sumMin <= innerAvail && len(cached.fitWidths) == cols && cached.fitAvail == innerAvail &&
		slices.Equal(cached.fitPinned, pinnedSig) {
		fits := true
		for c := 0; c < cols; c++ {
			if !pinned[c] && want[c] > cached.fitWidths[c] {
				fits = false
				break
			}
		}
		if fits {
			return slices.Clone(cached.fitWidths), false
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
	cached.fitAvail = innerAvail
	cached.fitPinned = pinnedSig
	cached.fitWidths = slices.Clone(colWidths)
	s.tableColCache[id] = cached
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
				bg, weight = pal.Table().HeaderBG, theme.WeightControl
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
