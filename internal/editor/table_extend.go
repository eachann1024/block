package editor

import (
	"math"

	"block/internal/model"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

type tableExtendAxis uint8

const (
	tableExtendNone tableExtendAxis = iota
	tableExtendRow
	tableExtendCol
)

const tableExtendActivateDist float64 = 6

type tableExtendState struct {
	blockID          model.BlockID
	axis             tableExtendAxis
	active           bool
	everChanged      bool
	canceled         bool
	startPt          ui.Point
	startScrollY     float32
	startHScrollX    float32
	baseTable        *model.TableData
	baseRows         int
	baseCols         int
	curCount         int
	removeSteps      []float32
	addStep          float32
	baseGridH        float32
	peakGridH        float32
	startSnap        undoSnapshot
	origBlockDirty   bool
	origBlockClean   string
	startActiveBlock model.BlockID
	startActiveRow   int
	startActiveCol   int
	startAnchor      Position
	startHead        Position
}

func (ext *tableExtendState) baseCount() int {
	if ext.axis == tableExtendRow {
		return ext.baseRows
	}
	return ext.baseCols
}

func (ext *tableExtendState) maxCount() int {
	if ext.axis == tableExtendRow {
		return max(ext.baseRows+200, 500)
	}
	return max(ext.baseCols+48, 64)
}

func (ext *tableExtendState) edgeOffset(count int) float32 {
	base := ext.baseCount()
	switch {
	case count == base:
		return 0
	case count > base:
		return float32(count-base) * ext.addStep
	default:
		var sum float32
		for i := max(count, 1); i < base && i < len(ext.removeSteps); i++ {
			sum += ext.removeSteps[i]
		}
		return -sum
	}
}

func (ext *tableExtendState) resolveCount(delta, hyst float32) int {
	if ext.addStep > 0 {
		hyst = min(hyst, ext.addStep*0.15)
	}
	cur := ext.curCount
	hi := ext.maxCount()
	for cur < hi {
		stepHyst := hyst
		if cur < ext.baseCount() && cur < len(ext.removeSteps) {
			stepHyst = min(stepHyst, ext.removeSteps[cur]*0.15)
		}
		mid := (ext.edgeOffset(cur) + ext.edgeOffset(cur+1)) * 0.5
		if delta >= mid+stepHyst {
			cur++
		} else {
			break
		}
	}
	for cur > 1 {
		stepHyst := hyst
		if cur-1 < ext.baseCount() && cur-1 < len(ext.removeSteps) {
			stepHyst = min(stepHyst, ext.removeSteps[cur-1]*0.15)
		}
		mid := (ext.edgeOffset(cur-1) + ext.edgeOffset(cur)) * 0.5
		if delta <= mid-stepHyst {
			cur--
		} else {
			break
		}
	}
	return cur
}

func buildExtendedTable(base *model.TableData, targetRows, targetCols int) *model.TableData {
	if base == nil {
		return nil
	}
	targetRows = max(targetRows, 1)
	targetCols = max(targetCols, 1)
	out := &model.TableData{
		WasHTML:     base.WasHTML,
		NoHeaderRow: base.NoHeaderRow,
		HeaderCol:   base.HeaderCol,
		Aligns:      make([]model.ColAlign, targetCols),
		Rows:        make([][]model.TableCell, targetRows),
	}
	for c := 0; c < targetCols; c++ {
		if c < len(base.Aligns) {
			out.Aligns[c] = base.Aligns[c]
		} else {
			out.Aligns[c] = model.ColAlignNone
		}
	}
	if len(base.Widths) > 0 {
		n := min(targetCols, len(base.Widths))
		out.Widths = append([]float32(nil), base.Widths[:n]...)
	}
	for r := 0; r < targetRows; r++ {
		row := make([]model.TableCell, targetCols)
		if r < len(base.Rows) {
			srcRow := base.Rows[r]
			for c := 0; c < targetCols && c < len(srcRow); c++ {
				row[c] = model.TableCell{
					Spans: append([]model.Span(nil), srcRow[c].Spans...),
					Align: srcRow[c].Align,
					Tint:  srcRow[c].Tint,
				}
			}
		}
		out.Rows[r] = row
	}
	return out
}

func (s *Session) tableHScroll(id model.BlockID) *ui.ScrollState {
	if s.tableHScrolls == nil {
		s.tableHScrolls = make(map[model.BlockID]*ui.ScrollState)
	}
	st, ok := s.tableHScrolls[id]
	if !ok || st == nil {
		st = &ui.ScrollState{}
		s.tableHScrolls[id] = st
	}
	return st
}

func (s *Session) scrollTableRightEnd(id model.BlockID) {
	s.tableHScroll(id).X = math.MaxFloat32
}

func (s *Session) isTableExtending() bool {
	return s.tableExtend != nil && s.tableExtend.active && !s.tableExtend.canceled
}

func (s *Session) isTableExtendingBlock(id model.BlockID) bool {
	return s.tableExtend != nil && s.tableExtend.blockID == id
}

func (s *Session) isTableExtendingAxis(id model.BlockID, axis tableExtendAxis) bool {
	return s.tableExtend != nil && s.tableExtend.blockID == id &&
		s.tableExtend.axis == axis && s.tableExtend.active && !s.tableExtend.canceled
}

func (s *Session) resetTableExtend() {
	s.tableExtend = nil
}

func (s *Session) tableExtendSavedHeight() float32 {
	if !s.isTableExtending() || s.tableExtend.axis != tableExtendRow {
		return 0
	}
	ext := s.tableExtend
	curGridH := max(ext.baseGridH+ext.edgeOffset(ext.curCount), 0)
	if curGridH > ext.peakGridH {
		ext.peakGridH = curGridH
	}
	return max(ext.peakGridH-curGridH, 0)
}

func (s *Session) preUpdateTableExtend(_ *ui.Context) {
	if s.tableExtend == nil {
		return
	}
	if s.doc == nil || s.blockIndexByID(s.tableExtend.blockID) < 0 {
		s.resetTableExtend()
	}
}

func (s *Session) cancelTableExtend() {
	ext := s.tableExtend
	if ext == nil {
		return
	}
	if idx := s.blockIndexByID(ext.blockID); idx >= 0 {
		b := &s.doc.Blocks[idx]
		b.Table = ext.baseTable.Clone()
		b.Dirty = ext.origBlockDirty
		b.CleanSource = ext.origBlockClean
		s.pruneTableTransientCells(ext.blockID, ext.baseRows, ext.baseCols)
		s.tableActiveBlock = ext.startActiveBlock
		s.tableActiveRow = ext.startActiveRow
		s.tableActiveCol = ext.startActiveCol
		s.selAnchor = s.clampPos(ext.startAnchor)
		s.selHead = s.clampPos(ext.startHead)
		if ext.axis == tableExtendCol {
			s.tableHScroll(ext.blockID).X = ext.startHScrollX
		}
		s.docRev++
	}
	ext.canceled = true
	ext.active = false
}

func (s *Session) pruneTableTransientCells(id model.BlockID, rows, cols int) {
	for k := range s.tableCellDocRects {
		if k.BlockID == id && (k.Row >= rows || k.Col >= cols) {
			delete(s.tableCellDocRects, k)
		}
	}
	for k := range s.tableInputs {
		if k.BlockID == id && (k.Row >= rows || k.Col >= cols) {
			delete(s.tableInputs, k)
		}
	}
}

func (s *Session) beginTableExtend(idx int, id model.BlockID, axis tableExtendAxis, btn ui.Element, dx, dy float32, colWidths []float32) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 || len(b.Table.Rows[0]) == 0 {
		return
	}

	s.mouseMode = mouseSelNone
	s.mouseOriginBlock = -1
	s.mouseDragged = false
	s.pressJustBegan = false
	s.firstLineDeleteArmed = false
	s.closeTransientOverlays()

	wx, wy := s.dragPointerPoint(btn)
	base := b.Table.Clone()
	baseRows := len(base.Rows)
	baseCols := len(base.Rows[0])

	_, _, tMinRowH, tMinColW, _, _, _ := s.tableMetrics()

	ext := &tableExtendState{
		blockID:          id,
		axis:             axis,
		startPt:          ui.Point{X: wx - dx, Y: wy - dy},
		startScrollY:     s.bodyScroll.Y,
		startHScrollX:    s.tableHScroll(id).X,
		baseTable:        base,
		baseRows:         baseRows,
		baseCols:         baseCols,
		startSnap:        s.captureUndoSnapshot(),
		origBlockDirty:   b.Dirty,
		origBlockClean:   b.CleanSource,
		startActiveBlock: s.tableActiveBlock,
		startActiveRow:   s.tableActiveRow,
		startActiveCol:   s.tableActiveCol,
		startAnchor:      s.selAnchor,
		startHead:        s.selHead,
	}

	if axis == tableExtendRow {
		rowStep := max(tMinRowH+appUi.TableGridRuleWidth, s.scale(24))
		ext.curCount = baseRows
		ext.addStep = rowStep
		ext.removeSteps = make([]float32, baseRows)
		var estGridH float32
		for r := 0; r < baseRows; r++ {
			var maxCellH float32
			for c := 0; c < baseCols; c++ {
				if cr, ok := s.tableCellRect(id, r, c); ok && cr.H > maxCellH {
					maxCellH = cr.H
				}
			}
			h := rowStep
			if maxCellH > 0 {
				h = maxCellH + appUi.TableGridRuleWidth
			}
			ext.removeSteps[r] = h
			estGridH += h
		}
		estGridH += appUi.TableGridRuleWidth
		if geom, ok := s.tableGridGeoms[id]; ok && geom.h > 0 {
			ext.baseGridH = geom.h
		} else {
			ext.baseGridH = estGridH
		}
		ext.peakGridH = ext.baseGridH
	} else {
		colStep := max(tMinColW*0.85, s.scale(56))
		ext.curCount = baseCols
		ext.addStep = colStep
		ext.removeSteps = make([]float32, baseCols)
		minStep := max(tMinColW*0.75, s.scale(48))
		maxStep := s.scale(200)
		for c := 0; c < baseCols; c++ {
			w := colStep
			if c < len(colWidths) && colWidths[c] > 0 {
				w = clampFloat32(colWidths[c]+appUi.TableGridRuleWidth, minStep, maxStep)
			} else if cr, ok := s.tableCellRect(id, 0, c); ok && cr.W > 0 {
				w = clampFloat32(cr.W+appUi.TableGridRuleWidth, minStep, maxStep)
			}
			ext.removeSteps[c] = w
		}
	}

	s.tableExtend = ext
}

func (s *Session) applyTableExtendCount(idx int, nextCount int) {
	ext := s.tableExtend
	if ext == nil || s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	targetRows, targetCols := ext.baseRows, ext.baseCols
	if ext.axis == tableExtendRow {
		targetRows = nextCount
	} else {
		targetCols = nextCount
	}

	b.Table = buildExtendedTable(ext.baseTable, targetRows, targetCols)
	ext.curCount = nextCount
	if nextCount != ext.baseCount() {
		ext.everChanged = true
	}

	if ext.axis == tableExtendRow {
		curGridH := max(ext.baseGridH+ext.edgeOffset(nextCount), 0)
		if curGridH > ext.peakGridH {
			ext.peakGridH = curGridH
		}
	} else {
		s.scrollTableRightEnd(ext.blockID)
	}

	s.pruneTableTransientCells(ext.blockID, targetRows, targetCols)

	if ext.startActiveBlock == ext.blockID {
		s.tableActiveBlock = ext.blockID
		s.tableActiveRow = clampInt(ext.startActiveRow, 0, targetRows-1)
		s.tableActiveCol = clampInt(ext.startActiveCol, 0, targetCols-1)
		s.selSyncPending = true
	}
	if targetRows == ext.baseRows && targetCols == ext.baseCols {
		s.selAnchor = s.clampPos(ext.startAnchor)
		s.selHead = s.clampPos(ext.startHead)
	} else {
		s.selAnchor = s.clampPos(s.selAnchor)
		s.selHead = s.clampPos(s.selHead)
	}

	s.docRev++
}

func (s *Session) commitTableExtend(idx int, ext *tableExtendState) {
	if ext == nil || s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	if ext.curCount == ext.baseCount() {
		b.Table = ext.baseTable.Clone()
		b.Dirty = ext.origBlockDirty
		b.CleanSource = ext.origBlockClean
		s.pruneTableTransientCells(ext.blockID, ext.baseRows, ext.baseCols)
		s.tableActiveBlock = ext.startActiveBlock
		s.tableActiveRow = ext.startActiveRow
		s.tableActiveCol = ext.startActiveCol
		s.selAnchor = s.clampPos(ext.startAnchor)
		s.selHead = s.clampPos(ext.startHead)
		if ext.axis == tableExtendCol {
			s.tableHScroll(ext.blockID).X = ext.startHScrollX
		}
		s.docRev++
		return
	}

	targetRows, targetCols := ext.baseRows, ext.baseCols
	if ext.axis == tableExtendRow {
		targetRows = ext.curCount
	} else {
		targetCols = ext.curCount
	}
	b.Table = buildExtendedTable(ext.baseTable, targetRows, targetCols)
	s.pruneTableTransientCells(ext.blockID, targetRows, targetCols)
	s.pushUndoSnapshot(ext.startSnap)
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) handleTableExtendGesture(c *ui.Context, btn ui.Element, idx int, id model.BlockID, axis tableExtendAxis, colWidths []float32) {
	dx, dy, held := btn.Dragged()
	if btn.PressCanceled() {
		if s.tableExtend != nil && s.tableExtend.blockID == id && s.tableExtend.axis == axis {
			s.cancelTableExtend()
			s.resetTableExtend()
			c.Invalidate()
		}
		return
	}

	if held {
		if s.tableExtend == nil || s.tableExtend.blockID != id || s.tableExtend.axis != axis {
			s.beginTableExtend(idx, id, axis, btn, dx, dy, colWidths)
		}
		ext := s.tableExtend
		if ext == nil || ext.canceled {
			return
		}
		wx, wy := s.dragPointerPoint(btn)
		if !ext.active {
			var axisDist float64
			if axis == tableExtendRow {
				axisDist = math.Abs(float64((wy - ext.startPt.Y) + (s.bodyScroll.Y - ext.startScrollY)))
			} else {
				axisDist = math.Abs(float64(wx - ext.startPt.X))
			}
			if axisDist >= tableExtendActivateDist || math.Hypot(float64(wx-ext.startPt.X), float64(wy-ext.startPt.Y)) >= tableExtendActivateDist*1.5 {
				ext.active = true
				c.Invalidate()
			}
		}
		if ext.active {
			var delta float32
			if axis == tableExtendRow {
				s.autoScrollOnDrag(c, wy)
				delta = (wy - ext.startPt.Y) + (s.bodyScroll.Y - ext.startScrollY)
			} else {
				delta = wx - ext.startPt.X
			}
			nextCount := ext.resolveCount(delta, s.scale(3))
			if nextCount != ext.curCount {
				s.applyTableExtendCount(idx, nextCount)
				c.Invalidate()
			}
		}
		return
	}

	if s.tableExtend != nil && s.tableExtend.blockID == id && s.tableExtend.axis == axis {
		ext := s.tableExtend
		wasActive := ext.active
		wasCanceled := ext.canceled
		everChanged := ext.everChanged
		s.resetTableExtend()
		if wasCanceled {
			c.Invalidate()
			return
		}
		if wasActive {
			wx, wy := s.dragPointerPoint(btn)
			sloppyClick := !everChanged && math.Hypot(float64(wx-ext.startPt.X), float64(wy-ext.startPt.Y)) < tableExtendActivateDist*1.75
			if !sloppyClick {
				s.commitTableExtend(idx, ext)
				c.Invalidate()
				return
			}
		}
	}

	if btn.Clicked() {
		if idx < 0 || idx >= len(s.doc.Blocks) || s.doc.Blocks[idx].Table == nil || len(s.doc.Blocks[idx].Table.Rows) == 0 {
			return
		}
		tbl := s.doc.Blocks[idx].Table
		if axis == tableExtendCol {
			s.tableAddCol(id, len(tbl.Rows[0]))
			s.scrollTableRightEnd(id)
		} else {
			s.tableAddRow(id, len(tbl.Rows))
		}
	}
}
