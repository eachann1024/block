package editor

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

func (s *Session) isTableWholeSelected(id model.BlockID) bool {
	return s.tableMenuBlock == id && s.tableMenuKind == "table"
}

func (s *Session) isTableColSelected(id model.BlockID, col int) bool {
	return s.tableMenuBlock == id && s.tableMenuKind == "col" && s.tableMenuIndex == col
}

func (s *Session) isTableRowSelected(id model.BlockID, row int) bool {
	return s.tableMenuBlock == id && s.tableMenuKind == "row" && s.tableMenuIndex == row
}

func (s *Session) toggleTableWholeMenu(idx int, id model.BlockID) {
	s.FocusBlock(idx)
	if s.tableMenuBlock == id && s.tableMenuKind == "table" {
		s.tableMenuBlock = 0
		s.tableMenuKind = ""
		return
	}
	s.closeTransientOverlays()
	s.tableMenuBlock = id
	s.tableMenuKind = "table"
	s.tableMenuIndex = 0
}

func (s *Session) toggleTableColMenu(idx int, id model.BlockID, col int) {
	s.FocusBlock(idx)
	s.tableActiveBlock = id
	s.tableActiveCol = col
	if s.tableMenuBlock == id && s.tableMenuKind == "col" && s.tableMenuIndex == col {
		s.tableMenuBlock = 0
		s.tableMenuKind = ""
		return
	}
	s.closeTransientOverlays()
	s.tableMenuBlock = id
	s.tableMenuKind = "col"
	s.tableMenuIndex = col
}

func (s *Session) toggleTableRowMenu(idx int, id model.BlockID, row int) {
	s.FocusBlock(idx)
	s.tableActiveBlock = id
	s.tableActiveRow = row
	if s.tableMenuBlock == id && s.tableMenuKind == "row" && s.tableMenuIndex == row {
		s.tableMenuBlock = 0
		s.tableMenuKind = ""
		return
	}
	s.closeTransientOverlays()
	s.tableMenuBlock = id
	s.tableMenuKind = "row"
	s.tableMenuIndex = row
}

// --- Table Column / Row / Whole-Table Operations ---

func uniformColTint(tbl *model.TableData, col int) (model.TableTint, bool) {
	if tbl == nil || len(tbl.Rows) == 0 || col < 0 || col >= len(tbl.Rows[0]) {
		return model.TableTintNone, false
	}
	first := tbl.Rows[0][col].Tint
	for r := 1; r < len(tbl.Rows); r++ {
		if col < len(tbl.Rows[r]) && tbl.Rows[r][col].Tint != first {
			return model.TableTintNone, false
		}
	}
	return first, true
}

func uniformRowTint(tbl *model.TableData, row int) (model.TableTint, bool) {
	if tbl == nil || row < 0 || row >= len(tbl.Rows) || len(tbl.Rows[row]) == 0 {
		return model.TableTintNone, false
	}
	first := tbl.Rows[row][0].Tint
	for c := 1; c < len(tbl.Rows[row]); c++ {
		if tbl.Rows[row][c].Tint != first {
			return model.TableTintNone, false
		}
	}
	return first, true
}

func uniformRowAlignOverride(tbl *model.TableData, row int) (model.ColAlign, bool) {
	if tbl == nil || row < 0 || row >= len(tbl.Rows) || len(tbl.Rows[row]) == 0 {
		return model.ColAlignNone, false
	}
	first := tbl.CellEffectiveAlign(row, 0)
	anyOverride := tbl.Rows[row][0].Align != model.ColAlignNone
	for c := 1; c < len(tbl.Rows[row]); c++ {
		if tbl.CellEffectiveAlign(row, c) != first {
			return model.ColAlignNone, false
		}
		if tbl.Rows[row][c].Align != model.ColAlignNone {
			anyOverride = true
		}
	}
	if !anyOverride {
		return model.ColAlignNone, false
	}
	return first, true
}

func tableUniformAlign(tbl *model.TableData, kind string, targetRow, targetCol int) (model.ColAlign, bool) {
	if tbl == nil || len(tbl.Rows) == 0 || len(tbl.Rows[0]) == 0 {
		return model.ColAlignNone, false
	}
	switch kind {
	case "col":
		first := tbl.CellEffectiveAlign(0, targetCol)
		for r := 1; r < len(tbl.Rows); r++ {
			if tbl.CellEffectiveAlign(r, targetCol) != first {
				return model.ColAlignNone, false
			}
		}
		return first, true
	case "row":
		if targetRow < 0 || targetRow >= len(tbl.Rows) {
			return model.ColAlignNone, false
		}
		first := tbl.CellEffectiveAlign(targetRow, 0)
		for c := 1; c < len(tbl.Rows[targetRow]); c++ {
			if tbl.CellEffectiveAlign(targetRow, c) != first {
				return model.ColAlignNone, false
			}
		}
		return first, true
	case "table":
		first := tbl.CellEffectiveAlign(0, 0)
		for r := range tbl.Rows {
			for c := range tbl.Rows[r] {
				if tbl.CellEffectiveAlign(r, c) != first {
					return model.ColAlignNone, false
				}
			}
		}
		return first, true
	default:
		return model.ColAlignNone, false
	}
}

func tableUniformTint(tbl *model.TableData, kind string, targetRow, targetCol int) (model.TableTint, bool) {
	switch kind {
	case "row":
		return uniformRowTint(tbl, targetRow)
	case "col":
		return uniformColTint(tbl, targetCol)
	default:
		return model.TableTintNone, false
	}
}

func (s *Session) tableSetColAlign(blockID model.BlockID, col int, align model.ColAlign) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 || col < 0 || col >= len(b.Table.Rows[0]) {
		return
	}
	cols := len(b.Table.Rows[0])
	cur := model.ColAlignNone
	if col < len(b.Table.Aligns) {
		cur = b.Table.Aligns[col]
	}
	hasOverride := false
	for r := range b.Table.Rows {
		if col < len(b.Table.Rows[r]) && b.Table.Rows[r][col].Align != model.ColAlignNone {
			hasOverride = true
			break
		}
	}
	if !hasOverride && (cur == align || (cur == model.ColAlignNone && align == model.ColAlignLeft)) {
		return
	}
	s.pushUndo()
	for len(b.Table.Aligns) < cols {
		b.Table.Aligns = append(b.Table.Aligns, model.ColAlignNone)
	}
	b.Table.Aligns[col] = align
	// Last-write-wins: clear any cell-level alignment overrides in this column.
	for r := range b.Table.Rows {
		if col < len(b.Table.Rows[r]) {
			b.Table.Rows[r][col].Align = model.ColAlignNone
		}
	}
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableSetRowAlign(blockID model.BlockID, row int, align model.ColAlign) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || row < 0 || row >= len(b.Table.Rows) || len(b.Table.Rows[row]) == 0 {
		return
	}
	if cur, ok := tableUniformAlign(b.Table, "row", row, 0); ok && cur == align {
		return
	}
	s.pushUndo()
	for c := range b.Table.Rows[row] {
		if align == b.Table.ColBaseAlign(c) {
			b.Table.Rows[row][c].Align = model.ColAlignNone
		} else {
			b.Table.Rows[row][c].Align = align
		}
	}
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableSetAllAlign(blockID model.BlockID, align model.ColAlign) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 || len(b.Table.Rows[0]) == 0 {
		return
	}
	if cur, ok := tableUniformAlign(b.Table, "table", 0, 0); ok && cur == align {
		return
	}
	s.pushUndo()
	cols := len(b.Table.Rows[0])
	b.Table.Aligns = make([]model.ColAlign, cols)
	for c := 0; c < cols; c++ {
		if align == model.ColAlignLeft {
			b.Table.Aligns[c] = model.ColAlignNone
		} else {
			b.Table.Aligns[c] = align
		}
	}
	for r := range b.Table.Rows {
		for c := range b.Table.Rows[r] {
			b.Table.Rows[r][c].Align = model.ColAlignNone
		}
	}
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func compareTableCellText(a, b string) int {
	ta := strings.TrimSpace(a)
	tb := strings.TrimSpace(b)
	if fa, errA := strconv.ParseFloat(ta, 64); errA == nil {
		if fb, errB := strconv.ParseFloat(tb, 64); errB == nil {
			switch {
			case fa < fb:
				return -1
			case fa > fb:
				return 1
			default:
				return 0
			}
		}
	}
	la := strings.ToLower(ta)
	lb := strings.ToLower(tb)
	if la != lb {
		return strings.Compare(la, lb)
	}
	return strings.Compare(ta, tb)
}

func (s *Session) tableSortByCol(blockID model.BlockID, col int, asc bool) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) <= 1 || col < 0 || col >= len(b.Table.Rows[0]) {
		return
	}
	startRow := 1
	if b.Table.NoHeaderRow {
		startRow = 0
	}
	if len(b.Table.Rows)-startRow <= 1 {
		return
	}
	s.pushUndo()
	body := b.Table.Rows[startRow:]
	sort.SliceStable(body, func(i, j int) bool {
		var va, vb string
		if col < len(body[i]) {
			va = body[i][col].PlainText()
		}
		if col < len(body[j]) {
			vb = body[j][col].PlainText()
		}
		cmp := compareTableCellText(va, vb)
		if asc {
			return cmp < 0
		}
		return cmp > 0
	})
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableSetColTint(blockID model.BlockID, col int, tint model.TableTint) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 || col < 0 || col >= len(b.Table.Rows[0]) {
		return
	}
	if cur, ok := uniformColTint(b.Table, col); ok && cur == tint {
		return
	}
	s.pushUndo()
	for r := range b.Table.Rows {
		if col < len(b.Table.Rows[r]) {
			b.Table.Rows[r][col].Tint = tint
		}
	}
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableSetRowTint(blockID model.BlockID, row int, tint model.TableTint) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || row < 0 || row >= len(b.Table.Rows) {
		return
	}
	if cur, ok := uniformRowTint(b.Table, row); ok && cur == tint {
		return
	}
	s.pushUndo()
	for c := range b.Table.Rows[row] {
		b.Table.Rows[row][c].Tint = tint
	}
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableToggleHeaderRow(blockID model.BlockID) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil {
		return
	}
	s.pushUndo()
	b.Table.NoHeaderRow = !b.Table.NoHeaderRow
	if b.Table.NoHeaderRow && len(b.Table.Rows) > 0 {
		for c := range b.Table.Rows[0] {
			b.Table.Rows[0][c].Header = false
		}
	}
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableToggleHeaderCol(blockID model.BlockID) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil {
		return
	}
	s.pushUndo()
	b.Table.HeaderCol = !b.Table.HeaderCol
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableDuplicateRow(blockID model.BlockID, row int) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || row < 0 || row >= len(b.Table.Rows) {
		return
	}
	s.pushUndo()
	src := b.Table.Rows[row]
	dup := make([]model.TableCell, len(src))
	for c, cell := range src {
		dup[c] = model.TableCell{
			Spans: append([]model.Span(nil), cell.Spans...),
			Align: cell.Align,
			Tint:  cell.Tint,
		}
	}
	at := row + 1
	oldLen := len(b.Table.Rows)
	rows := make([][]model.TableCell, 0, oldLen+1)
	rows = append(rows, b.Table.Rows[:at]...)
	rows = append(rows, dup)
	rows = append(rows, b.Table.Rows[at:]...)
	b.Table.Rows = rows
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableClearCol(blockID model.BlockID, col int) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 || col < 0 || col >= len(b.Table.Rows[0]) {
		return
	}
	startRow := 1
	if b.Table.NoHeaderRow || len(b.Table.Rows) == 1 {
		startRow = 0
	}
	anyNonEmpty := false
	for r := startRow; r < len(b.Table.Rows); r++ {
		if col < len(b.Table.Rows[r]) && b.Table.Rows[r][col].PlainText() != "" {
			anyNonEmpty = true
			break
		}
	}
	if !anyNonEmpty {
		return
	}
	s.pushUndo()
	for r := startRow; r < len(b.Table.Rows); r++ {
		if col < len(b.Table.Rows[r]) {
			b.Table.Rows[r][col].Spans = nil
		}
	}
	b.Dirty = true
	b.CleanSource = ""
	s.syncTableActiveBounds(idx)
	s.markDirty()
}

func (s *Session) tableClearRow(blockID model.BlockID, row int) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || row < 0 || row >= len(b.Table.Rows) {
		return
	}
	anyNonEmpty := false
	for c := range b.Table.Rows[row] {
		if b.Table.Rows[row][c].PlainText() != "" {
			anyNonEmpty = true
			break
		}
	}
	if !anyNonEmpty {
		return
	}
	s.pushUndo()
	for c := range b.Table.Rows[row] {
		b.Table.Rows[row][c].Spans = nil
	}
	b.Dirty = true
	b.CleanSource = ""
	s.syncTableActiveBounds(idx)
	s.markDirty()
}

func (s *Session) tableClearAll(blockID model.BlockID) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 {
		return
	}
	startRow := 1
	if b.Table.NoHeaderRow || len(b.Table.Rows) == 1 {
		startRow = 0
	}
	anyNonEmpty := false
	for r := startRow; r < len(b.Table.Rows); r++ {
		for c := range b.Table.Rows[r] {
			if b.Table.Rows[r][c].PlainText() != "" {
				anyNonEmpty = true
				break
			}
		}
	}
	if !anyNonEmpty {
		return
	}
	s.pushUndo()
	for r := startRow; r < len(b.Table.Rows); r++ {
		for c := range b.Table.Rows[r] {
			b.Table.Rows[r][c].Spans = nil
		}
	}
	b.Dirty = true
	b.CleanSource = ""
	s.syncTableActiveBounds(idx)
	s.markDirty()
}

func (s *Session) tableResetColWidths(blockID model.BlockID) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Widths) == 0 {
		return
	}
	s.pushUndo()
	b.Table.Widths = nil
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableMoveCol(blockID model.BlockID, from, to int) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 {
		return
	}
	cols := len(b.Table.Rows[0])
	if from < 0 || from >= cols || to < 0 || to >= cols || from == to {
		return
	}
	s.pushUndo()
	for r := range b.Table.Rows {
		row := b.Table.Rows[r]
		if len(row) < cols {
			continue
		}
		moved := row[from]
		without := append(append([]model.TableCell(nil), row[:from]...), row[from+1:]...)
		next := make([]model.TableCell, 0, cols)
		next = append(next, without[:to]...)
		next = append(next, moved)
		next = append(next, without[to:]...)
		b.Table.Rows[r] = next
	}
	if len(b.Table.Aligns) > 0 {
		for len(b.Table.Aligns) < cols {
			b.Table.Aligns = append(b.Table.Aligns, model.ColAlignNone)
		}
		mv := b.Table.Aligns[from]
		wo := append(append([]model.ColAlign(nil), b.Table.Aligns[:from]...), b.Table.Aligns[from+1:]...)
		nx := make([]model.ColAlign, 0, cols)
		nx = append(nx, wo[:to]...)
		nx = append(nx, mv)
		nx = append(nx, wo[to:]...)
		b.Table.Aligns = nx
	}
	if len(b.Table.Widths) > 0 {
		for len(b.Table.Widths) < cols {
			b.Table.Widths = append(b.Table.Widths, 0)
		}
		mv := b.Table.Widths[from]
		wo := append(append([]float32(nil), b.Table.Widths[:from]...), b.Table.Widths[from+1:]...)
		nx := make([]float32, 0, cols)
		nx = append(nx, wo[:to]...)
		nx = append(nx, mv)
		nx = append(nx, wo[to:]...)
		b.Table.Widths = nx
	}
	s.tableActiveCol = to
	if s.tableMenuBlock == blockID && s.tableMenuKind == "col" {
		s.tableMenuIndex = to
	}
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableMoveRow(blockID model.BlockID, from, to int) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil {
		return
	}
	rows := len(b.Table.Rows)
	if from < 0 || from >= rows || to < 0 || to >= rows || from == to {
		return
	}
	s.pushUndo()
	moved := b.Table.Rows[from]
	without := append(append([][]model.TableCell(nil), b.Table.Rows[:from]...), b.Table.Rows[from+1:]...)
	next := make([][]model.TableCell, 0, rows)
	next = append(next, without[:to]...)
	next = append(next, moved)
	next = append(next, without[to:]...)
	b.Table.Rows = next
	s.tableActiveRow = to
	if s.tableMenuBlock == blockID && s.tableMenuKind == "row" {
		s.tableMenuIndex = to
	}
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableCopyMarkdown(svc ui.Services, idx int) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	if s.doc.Blocks[idx].Kind != model.BlockTable || s.doc.Blocks[idx].Table == nil {
		return
	}
	cp := s.doc.Blocks[idx].Clone()
	cp.ID = model.NewBlockID()
	cp.Dirty = true
	cp.CleanSource = ""
	cp.Table.Widths = nil
	cp.Table.NoHeaderRow = false
	cp.Table.HeaderCol = false
	cp.Table.WasHTML = false
	for r := range cp.Table.Rows {
		for col := range cp.Table.Rows[r] {
			cp.Table.Rows[r][col].Align = model.ColAlignNone
			cp.Table.Rows[r][col].Tint = model.TableTintNone
		}
	}
	single := []model.Block{cp}
	if md := strings.TrimSuffix(s.serializeBlocks(single), "\n"); md != "" {
		s.writeRichClipboard(svc, md, &model.Doc{Blocks: single, TrailingLine: "\n"}, true)
	}
}

func tableMaxCols(tbl *model.TableData) int {
	if tbl == nil {
		return 0
	}
	maxCols := 0
	for _, row := range tbl.Rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	return maxCols
}

func (s *Session) tableCopyMenuSelection(svc ui.Services, idx int, kind string, targetRow, targetCol int) bool {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	tbl := s.doc.Blocks[idx].Table
	if tbl == nil || len(tbl.Rows) == 0 {
		return false
	}
	switch kind {
	case "table":
		single := []model.Block{s.doc.Blocks[idx].Clone()}
		md := strings.TrimSuffix(s.serializeBlocks(single), "\n")
		if md == "" {
			return false
		}
		s.writeRichClipboard(svc, md, &model.Doc{Blocks: single, TrailingLine: "\n"}, true)
		return true
	case "row":
		if targetRow < 0 || targetRow >= len(tbl.Rows) || len(tbl.Rows[targetRow]) == 0 {
			return false
		}
		parts := make([]string, len(tbl.Rows[targetRow]))
		plainParts := make([]string, len(tbl.Rows[targetRow]))
		rowCopy := make([]model.TableCell, len(tbl.Rows[targetRow]))
		for cIdx, cell := range tbl.Rows[targetRow] {
			parts[cIdx] = sanitizeTableCellTSV(serializeInlineBlockMarkdown(cell.Spans, s.codec))
			plainParts[cIdx] = sanitizeTableCellTSV(cell.PlainText())
			rowCopy[cIdx] = model.TableCell{
				Spans: append([]model.Span(nil), cell.Spans...),
				Align: cell.Align,
				Tint:  cell.Tint,
			}
		}
		s.writeTableCellsClipboard(svc, strings.Join(plainParts, "\t"), strings.Join(parts, "\t"), [][]model.TableCell{rowCopy})
		return true
	case "col":
		maxCols := tableMaxCols(tbl)
		if maxCols == 0 || targetCol < 0 || targetCol >= maxCols {
			return false
		}
		parts := make([]string, 0, len(tbl.Rows))
		plainParts := make([]string, 0, len(tbl.Rows))
		rows := make([][]model.TableCell, 0, len(tbl.Rows))
		for _, row := range tbl.Rows {
			if targetCol < len(row) {
				cell := row[targetCol]
				parts = append(parts, sanitizeTableCellTSV(serializeInlineBlockMarkdown(cell.Spans, s.codec)))
				plainParts = append(plainParts, sanitizeTableCellTSV(cell.PlainText()))
				rows = append(rows, []model.TableCell{{
					Spans: append([]model.Span(nil), cell.Spans...),
					Align: cell.Align,
					Tint:  cell.Tint,
				}})
			} else {
				parts = append(parts, "")
				plainParts = append(plainParts, "")
				rows = append(rows, []model.TableCell{{}})
			}
		}
		s.writeTableCellsClipboard(svc, strings.Join(plainParts, "\n"), strings.Join(parts, "\n"), rows)
		return true
	}
	return false
}

func (s *Session) tableCutMenuSelection(svc ui.Services, idx int, kind string, targetRow, targetCol int) bool {
	if !s.tableCopyMenuSelection(svc, idx, kind, targetRow, targetCol) {
		return false
	}
	if s.readOnly {
		return true
	}
	b := &s.doc.Blocks[idx]
	tbl := b.Table
	id := b.ID
	switch kind {
	case "table":
		s.deleteBlock(idx)
		return true
	case "row":
		if targetRow < 0 || targetRow >= len(tbl.Rows) {
			return false
		}
		if len(tbl.Rows) > 1 {
			s.tableDeleteRow(id, targetRow)
		} else {
			s.tableClearRow(id, targetRow)
		}
		return true
	case "col":
		maxCols := tableMaxCols(tbl)
		if maxCols == 0 || targetCol < 0 || targetCol >= maxCols {
			return false
		}
		if maxCols > 1 {
			s.tableDeleteCol(id, targetCol)
			return true
		}
		anyNonEmpty := false
		for r := range tbl.Rows {
			if targetCol < len(tbl.Rows[r]) && tbl.Rows[r][targetCol].PlainText() != "" {
				anyNonEmpty = true
				break
			}
		}
		if anyNonEmpty {
			s.pushUndo()
			for r := range tbl.Rows {
				if targetCol < len(tbl.Rows[r]) {
					tbl.Rows[r][targetCol].Spans = nil
				}
			}
			b.Dirty = true
			b.CleanSource = ""
			s.syncTableActiveBounds(idx)
			s.markDirty()
		}
		return true
	}
	return false
}

func (s *Session) tablePasteMenuSelection(svc ui.Services, idx int, kind string, targetRow, targetCol int) bool {
	if s.readOnly || s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	b := &s.doc.Blocks[idx]
	tbl := b.Table
	if tbl == nil || len(tbl.Rows) == 0 {
		return false
	}
	rows := len(tbl.Rows)
	cols := max(tableMaxCols(tbl), 1)
	s.focusedBlock = idx
	s.tableActiveBlock = b.ID
	switch kind {
	case "row":
		s.tableActiveRow = clampInt(targetRow, 0, rows-1)
		s.tableActiveCol = 0
	case "col":
		s.tableActiveRow = 0
		s.tableActiveCol = clampInt(targetCol, 0, cols-1)
	case "table":
		s.tableActiveRow = 0
		s.tableActiveCol = 0
	default:
		s.tableActiveRow = clampInt(s.tableActiveRow, 0, rows-1)
		s.tableActiveCol = clampInt(s.tableActiveCol, 0, cols-1)
	}
	off := tableCellStartOffset(tbl, s.tableActiveRow, s.tableActiveCol)
	s.selAnchor = Position{Block: idx, Offset: off}
	s.selHead = s.selAnchor
	return s.PasteFromClipboard(svc, svc.ReadClipboard(), false)
}

// --- Cell Fill & Selection Ring Painting ---

func (s *Session) resolveTableCellFill(
	blockID model.BlockID,
	tbl *model.TableData,
	rIdx, cIdx int,
	pal theme.Palette,
	tblCols theme.TableColors,
	isCellFocused, cellSelected bool,
) ui.Color {
	bg := pal.Paper
	if tbl.IsHeaderCell(rIdx, cIdx) {
		bg = tblCols.HeaderBG
	}
	if tint := tbl.CellTint(rIdx, cIdx); tint != model.TableTintNone {
		if tc, ok := tblCols.CellTint(string(tint)); ok {
			bg = tc
		}
	}
	if s.isTableWholeSelected(blockID) || s.isTableColSelected(blockID, cIdx) || s.isTableRowSelected(blockID, rIdx) {
		bg = tblCols.SelectedTint
	}
	_ = isCellFocused
	if cellSelected {
		bg = pal.SelectionBG.Alpha(0.42).Over(bg)
	}
	return bg
}

func (s *Session) paintTableSelectionFrames(
	p *ui.Painter,
	gr ui.Rect,
	id model.BlockID,
	tbl *model.TableData,
	colWidths []float32,
	pal theme.Palette,
	tblCols theme.TableColors,
) {
	if s.tableMenuBlock != id || gr.W <= 0 || gr.H <= 0 {
		return
	}
	ruleW := appUi.TableGridRuleWidth
	strokeW := s.scale(theme.TableSelectBorderWidth)
	switch s.tableMenuKind {
	case "table":
		spread := s.scale(theme.TableOuterHaloSpread)
		halo := ui.Rect{X: gr.X - spread, Y: gr.Y - spread, W: gr.W + 2*spread, H: gr.H + 2*spread}
		p.Stroke(halo, tblCols.OuterSelectRing, theme.RadiusSM+spread, spread)
		p.Stroke(gr, pal.Accent, theme.RadiusSM, strokeW)
	case "col":
		col := s.tableMenuIndex
		if col < 0 || col >= len(colWidths) {
			return
		}
		var xOff float32
		for c := 0; c < col; c++ {
			xOff += ruleW + colWidths[c]
		}
		colRect := ui.Rect{
			X: gr.X + xOff,
			Y: gr.Y,
			W: colWidths[col] + 2*ruleW,
			H: gr.H,
		}
		p.Stroke(colRect, pal.Accent, theme.RadiusXS, strokeW)
	case "row":
		row := s.tableMenuIndex
		if row < 0 || row >= len(tbl.Rows) {
			return
		}
		cellR, ok := s.tableCellRect(id, row, 0)
		if !ok || cellR.H <= 0 {
			return
		}
		rowRect := ui.Rect{
			X: gr.X,
			Y: cellR.Y - ruleW,
			W: gr.W,
			H: cellR.H + 2*ruleW,
		}
		p.Stroke(rowRect, pal.Accent, theme.RadiusXS, strokeW)
	}
}

// --- Notion-Style Edge Pill Handles, Seam Splitters & Intersection '+' Pins ---

func drawPillSixDots(p *ui.Painter, r ui.Rect, horizontal bool, dotR, stepA, stepB float32, col ui.Color) {
	cx := r.X + r.W*0.5
	cy := r.Y + r.H*0.5
	d := dotR * 2
	if horizontal {
		for _, ox := range []float32{-stepA, 0, stepA} {
			for _, oy := range []float32{-stepB * 0.5, stepB * 0.5} {
				p.Fill(ui.Rect{X: cx + ox - dotR, Y: cy + oy - dotR, W: d, H: d}, col, dotR)
			}
		}
		return
	}
	for _, oy := range []float32{-stepA, 0, stepA} {
		for _, ox := range []float32{-stepB * 0.5, stepB * 0.5} {
			p.Fill(ui.Rect{X: cx + ox - dotR, Y: cy + oy - dotR, W: d, H: d}, col, dotR)
		}
	}
}

func (s *Session) renderTableBorderChrome(
	c *ui.Context,
	idx int,
	id model.BlockID,
	tbl *model.TableData,
	colWidths []float32,
	availGrid float32,
	needHScroll bool,
	isFocused bool,
) {
	_ = isFocused
	if s.isTableExtendingBlock(id) {
		return
	}
	pal := theme.Ocean()
	tblCols := pal.Table()
	rows := len(tbl.Rows)
	cols := len(colWidths)
	if rows == 0 || cols == 0 {
		return
	}

	_, _, tMinRowH, tMinColW, tMaxColW, _, _ := s.tableMetrics()
	ruleW := appUi.TableGridRuleWidth
	scrollX := float32(0)
	if needHScroll {
		scrollX = s.tableHScroll(id).X
	}

	var totalGridW float32 = ruleW
	colLefts := make([]float32, cols)
	for ci, cw := range colWidths {
		colLefts[ci] = totalGridW
		totalGridW += cw + ruleW
	}
	visW := min(totalGridW, availGrid)
	if !needHScroll {
		visW = totalGridW
	}

	// Compute row vertical offsets & heights relative to gridRow top using recorded cell rects.
	firstCell, hasFirst := s.tableCellRect(id, 0, 0)
	gridWinTop := firstCell.Y - ruleW
	gridWinLeft := firstCell.X - ruleW + scrollX
	if needHScroll {
		gridWinLeft = firstCell.X - colLefts[0] + scrollX
	}

	rowTops := make([]float32, rows)
	rowHeights := make([]float32, rows)
	var curY float32 = ruleW
	for ri := 0; ri < rows; ri++ {
		if hasFirst {
			if cr, ok := s.tableCellRect(id, ri, 0); ok && cr.H > 0 {
				rowTops[ri] = cr.Y - gridWinTop
				rowHeights[ri] = cr.H
				curY = rowTops[ri] + cr.H + ruleW
				continue
			}
		}
		rowTops[ri] = curY
		rowHeights[ri] = tMinRowH
		curY += tMinRowH + ruleW
	}
	gridH := curY
	if geom, ok := s.tableGridGeoms[id]; ok && geom.h > 0 {
		gridH = geom.h
	}

	// Pointer hit-testing in window space (expanded by TableHandleHoverBand so moving onto edge pills never flickers).
	hoverBand := s.scale(theme.TableHandleHoverBand)
	hoverCol, hoverRow := -1, -1
	ptrWin, hasPtr := s.windowPointerStatus()
	if hasPtr && hasFirst {
		relX := ptrWin.X - gridWinLeft
		relY := ptrWin.Y - gridWinTop
		inHoverBox := relX >= -hoverBand*1.6 && relX <= visW+hoverBand &&
			relY >= -hoverBand*1.4 && relY <= gridH+hoverBand
		if inHoverBox {
			docX := relX + scrollX
			for ci := 0; ci < cols; ci++ {
				if docX >= colLefts[ci]-ruleW && docX <= colLefts[ci]+colWidths[ci]+ruleW {
					hoverCol = ci
					break
				}
			}
			for ri := 0; ri < rows; ri++ {
				if relY >= rowTops[ri]-ruleW && relY <= rowTops[ri]+rowHeights[ri]+ruleW {
					hoverRow = ri
					break
				}
			}
		}
	}

	colW := s.scale(theme.TableColHandleW)
	colH := s.scale(theme.TableColHandleH)
	rowW := s.scale(theme.TableRowHandleW)
	rowH := s.scale(theme.TableRowHandleH)
	dotR := s.scale(theme.TableGripDotRadius)
	stepA := s.scale(theme.TableGripDotStepX)
	stepB := s.scale(theme.TableGripDotStepY)

	// Minimal-mode whole-table handle at top-left corner (when left gutter block-grip is absent).
	if s.minimal {
		wholeID := fmt.Sprintf("table-whole-handle-%d", id)
		wholeSel := s.isTableWholeSelected(id)
		wb := ui.ButtonBase(c.Key(wholeID)).Absolute().
			Left(-rowW*0.5).Top(-colH*0.5).Size(rowW+s.scale(theme.Space2), colH+s.scale(theme.Space2)).
			Radius(theme.RadiusXS).Cursor(ui.CursorPointer).KeepFocus().
			Label(theme.Tr("整表操作", "Table options"))
		wHov := wb.Hovered()
		wBg := tblCols.HandleBG
		wBd := tblCols.HandleBorder
		wFg := tblCols.HandleFG
		if wholeSel {
			wBg, wBd, wFg = tblCols.HandleActiveBG, pal.Accent, tblCols.HandleActiveFG
		} else if wHov {
			wBg, wBd = tblCols.HandleHoverBG, pal.Accent
		}
		appUi.AnimateSurfaceColors(wb, wBg, wBd, true)
		wb.DrawOver(func(p *ui.Painter, r ui.Rect) {
			drawPillSixDots(p, r, true, dotR*0.9, stepA*0.8, stepB*0.8, wFg)
		})
		appUi.Probe(wb, wholeID)
		s.recordOverlayBtn(wholeID, wb)
		if wb.Clicked() {
			s.toggleTableWholeMenu(idx, id)
		}
	}

	// 1. Column top-border centered pill handles (click -> column menu, drag -> reorder columns).
	anyColDragHeld := false
	for ci := 0; ci < cols; ci++ {
		cIdx := ci
		centerX := colLefts[cIdx] + colWidths[cIdx]*0.5 - scrollX
		if needHScroll && (centerX < colW*0.45 || centerX > visW-colW*0.45) {
			continue
		}
		selected := s.isTableColSelected(id, cIdx)
		draggingThis := s.tableDragBlock == id && s.tableDragKind == "col" && s.tableDragIndex == cIdx
		btnID := fmt.Sprintf("table-col-handle-%d-%d", id, cIdx)
		showColPill := selected || draggingThis || cIdx == hoverCol || s.isOverlayBtnHovered(btnID)
		if !showColPill || s.tableResizeBlock == id {
			continue
		}

		colName := strings.TrimSpace(tbl.Rows[0][cIdx].PlainText())
		if colName == "" {
			colName = fmt.Sprintf("#%d", cIdx+1)
		}
		btn := ui.ButtonBase(c.Key(btnID)).Absolute().
			Left(centerX-colW*0.5).Top(-colH*0.5).Size(colW, colH).
			Radius(theme.RadiusXS).KeepFocus().
			Label(fmt.Sprintf(theme.Tr("选中列 %s", "Select column %s"), colName))
		hovered := btn.Hovered()
		pressed := btn.Pressed()
		dx, dy, held := btn.Dragged()
		if held || draggingThis {
			btn.Cursor(ui.CursorGrabbing)
		} else {
			btn.Cursor(ui.CursorPointer)
		}

		bg, bd, fg := tblCols.HandleBG, tblCols.HandleBorder, tblCols.HandleFG
		if selected || draggingThis || pressed {
			bg, bd, fg = tblCols.HandleActiveBG, pal.Accent, tblCols.HandleActiveFG
		} else if hovered {
			bg, bd, fg = tblCols.HandleHoverBG, pal.Accent, pal.Text
		}
		appUi.AnimateSurfaceColors(btn, bg, bd, true)
		appUi.Elevate(btn, theme.ElevationRaised)
		btn.DrawOver(func(p *ui.Painter, r ui.Rect) {
			drawPillSixDots(p, r, true, dotR, stepA, stepB, fg)
		})
		appUi.Probe(btn, btnID)
		appUi.Probe(btn, "table-col-handle")
		s.recordOverlayBtn(btnID, btn)

		if held {
			anyColDragHeld = true
			wx, wy := s.dragPointerPoint(btn)
			if !draggingThis {
				s.tableDragBlock = id
				s.tableDragKind = "col"
				s.tableDragIndex = cIdx
				s.tableDragStartPt = ui.Point{X: wx - dx, Y: wy - dy}
				s.tableDragActive = false
				s.tableDragMoved = false
			}
			if !s.tableDragActive && math.Abs(float64(wx-s.tableDragStartPt.X)) >= blockDragThreshold {
				s.tableDragActive = true
				s.tableMenuBlock = 0
				s.tableMenuKind = ""
			}
			if s.tableDragActive && hasFirst {
				docX := (wx - gridWinLeft) + scrollX
				for targetC := 0; targetC < cols; targetC++ {
					mid := colLefts[targetC] + colWidths[targetC]*0.5
					half := colWidths[targetC] * 0.45
					if targetC != s.tableDragIndex && docX >= mid-half && docX <= mid+half {
						s.tableMoveCol(id, s.tableDragIndex, targetC)
						s.tableDragIndex = targetC
						s.tableDragMoved = true
						c.Invalidate()
						break
					}
				}
			}
		} else if btn.Clicked() && !s.tableDragMoved {
			s.toggleTableColMenu(idx, id, cIdx)
		}
	}
	if !anyColDragHeld && s.tableDragBlock == id && s.tableDragKind == "col" {
		s.tableDragBlock = 0
		s.tableDragKind = ""
		s.tableDragActive = false
		s.tableDragMoved = false
	}

	// 2. Row left-border centered pill handles (click -> row menu, drag -> reorder rows).
	anyRowDragHeld := false
	for ri := 0; ri < rows; ri++ {
		rIdx := ri
		centerY := rowTops[rIdx] + rowHeights[rIdx]*0.5
		selected := s.isTableRowSelected(id, rIdx)
		draggingThis := s.tableDragBlock == id && s.tableDragKind == "row" && s.tableDragIndex == rIdx
		btnID := fmt.Sprintf("table-row-handle-%d-%d", id, rIdx)
		showRowPill := selected || draggingThis || rIdx == hoverRow || s.isOverlayBtnHovered(btnID)
		if !showRowPill || s.tableResizeBlock == id {
			continue
		}

		btn := ui.ButtonBase(c.Key(btnID)).Absolute().
			Left(-rowW*0.5).Top(centerY-rowH*0.5).Size(rowW, rowH).
			Radius(theme.RadiusXS).KeepFocus().
			Label(fmt.Sprintf(theme.Tr("选中第 %d 行", "Select row %d"), rIdx+1))
		hovered := btn.Hovered()
		pressed := btn.Pressed()
		dx, dy, held := btn.Dragged()
		if held || draggingThis {
			btn.Cursor(ui.CursorGrabbing)
		} else {
			btn.Cursor(ui.CursorPointer)
		}

		bg, bd, fg := tblCols.HandleBG, tblCols.HandleBorder, tblCols.HandleFG
		if selected || draggingThis || pressed {
			bg, bd, fg = tblCols.HandleActiveBG, pal.Accent, tblCols.HandleActiveFG
		} else if hovered {
			bg, bd, fg = tblCols.HandleHoverBG, pal.Accent, pal.Text
		}
		appUi.AnimateSurfaceColors(btn, bg, bd, true)
		appUi.Elevate(btn, theme.ElevationRaised)
		btn.DrawOver(func(p *ui.Painter, r ui.Rect) {
			drawPillSixDots(p, r, false, dotR, stepA, stepB, fg)
		})
		appUi.Probe(btn, btnID)
		appUi.Probe(btn, "table-row-handle")
		s.recordOverlayBtn(btnID, btn)

		if held {
			anyRowDragHeld = true
			wx, wy := s.dragPointerPoint(btn)
			if !draggingThis {
				s.tableDragBlock = id
				s.tableDragKind = "row"
				s.tableDragIndex = rIdx
				s.tableDragStartPt = ui.Point{X: wx - dx, Y: wy - dy}
				s.tableDragActive = false
				s.tableDragMoved = false
			}
			if !s.tableDragActive && math.Abs(float64(wy-s.tableDragStartPt.Y)) >= blockDragThreshold {
				s.tableDragActive = true
				s.tableMenuBlock = 0
				s.tableMenuKind = ""
			}
			if s.tableDragActive && hasFirst {
				relY := wy - gridWinTop
				minTarget := 1
				if tbl.NoHeaderRow || s.tableDragIndex == 0 {
					minTarget = 0
				}
				for targetR := minTarget; targetR < rows; targetR++ {
					mid := rowTops[targetR] + rowHeights[targetR]*0.5
					half := rowHeights[targetR] * 0.45
					if targetR != s.tableDragIndex && relY >= mid-half && relY <= mid+half {
						s.tableMoveRow(id, s.tableDragIndex, targetR)
						s.tableDragIndex = targetR
						s.tableDragMoved = true
						c.Invalidate()
						break
					}
				}
			}
		} else if btn.Clicked() && !s.tableDragMoved {
			s.toggleTableRowMenu(idx, id, rIdx)
		}
	}
	if !anyRowDragHeld && s.tableDragBlock == id && s.tableDragKind == "row" {
		s.tableDragBlock = 0
		s.tableDragKind = ""
		s.tableDragActive = false
		s.tableDragMoved = false
	}

	// 3. Vertical column border seams: drag to resize width, double-click to auto-fit.
	splitHitW := s.scale(theme.TableSplitHitSize)
	lineW := s.scale(theme.ResizeLineWidth)
	z := s.zoom()

	anyResizeHeld := false
	for ci := 0; ci < cols-1; ci++ {
		cIdx := ci
		seamX := colLefts[cIdx] + colWidths[cIdx] + ruleW*0.5 - scrollX
		if seamX < 2 || seamX > visW-2 {
			continue
		}
		seamID := fmt.Sprintf("table-col-seam-%d-%d", id, cIdx)
		seam := ui.Box(c.Key(seamID)).Absolute().
			Left(seamX - splitHitW*0.5).Top(0).Height(gridH).Width(splitHitW).
			Cursor(ui.CursorResizeColumn)
		dx, _, held := seam.Dragged()
		seamHov := seam.Hovered()
		resizingThis := s.tableResizeBlock == id && s.tableResizeCol == cIdx

		if (seamHov && s.tableMenuBlock != id) || held || resizingThis {
			seam.DrawOver(func(p *ui.Painter, r ui.Rect) {
				lx := r.X + r.W*0.5 - lineW*0.5
				p.Fill(ui.Rect{X: lx, Y: r.Y, W: lineW, H: r.H}, pal.Accent, lineW*0.5)
			})
		}
		appUi.Probe(seam, seamID)

		if seam.DoubleClicked() {
			if cIdx < len(tbl.Widths) && tbl.Widths[cIdx] > 0 {
				s.pushUndo()
				tbl.Widths[cIdx] = 0
				hasAny := false
				for _, w := range tbl.Widths {
					if w > 0 {
						hasAny = true
						break
					}
				}
				if !hasAny {
					tbl.Widths = nil
				}
				s.doc.Blocks[idx].Dirty = true
				s.doc.Blocks[idx].CleanSource = ""
				s.markDirty()
			}
		} else if held {
			anyResizeHeld = true
			if !resizingThis {
				s.tableResizeBlock = id
				s.tableResizeCol = cIdx
				baseUnscaled := colWidths[cIdx] / max(z, 0.1)
				s.tableResizeStartW = baseUnscaled
				s.tableResizeCurW = baseUnscaled
				s.tableMenuBlock = 0
				s.tableMenuKind = ""
			}
			if dx != 0 {
				minUnscaled := tMinColW / max(z, 0.1)
				maxUnscaled := max(tMaxColW*1.6, 560) / max(z, 0.1)
				s.tableResizeCurW = clampFloat32(s.tableResizeCurW+dx/max(z, 0.1), minUnscaled, maxUnscaled)
				c.Invalidate()
			}
		}

		if held || resizingThis {
			badgeText := fmt.Sprintf("%dpx", int(math.Round(float64(colWidths[cIdx]/max(z, 0.1)))))
			badge := ui.Row(c.Key(fmt.Sprintf("table-col-badge-%d-%d", id, cIdx))).Absolute().
				Left(min(seamX+s.scale(theme.Space6), max(visW-s.scale(72), 0))).
				Top(s.scale(theme.Space8)).Height(s.scale(theme.TableResizeBadgeH)).
				PaddingX(s.scale(theme.Space8)).AlignItems(ui.Center).
				Radius(theme.RadiusSM).Background(pal.Menu).Border(1, pal.Accent).IgnoreInput(true)
			appUi.Elevate(badge, theme.ElevationRaised)
			badge.Children(func() {
				ui.Text(c, badgeText).Font(resolvedCodeFont()).FontSize(s.scale(theme.FontMeta)).
					FontWeight(theme.WeightMono).TextColor(pal.Text).SingleLine()
			})
		}
	}

	if !anyResizeHeld && s.tableResizeBlock == id {
		rc := s.tableResizeCol
		finalW := float32(math.Round(float64(s.tableResizeCurW)))
		startW := s.tableResizeStartW
		s.tableResizeBlock = 0
		if rc >= 0 && rc < cols && math.Abs(float64(finalW-startW)) >= 1 {
			s.pushUndo()
			if len(tbl.Widths) < cols {
				ws := make([]float32, cols)
				for i := 0; i < cols; i++ {
					if i < len(tbl.Widths) && tbl.Widths[i] > 0 {
						ws[i] = tbl.Widths[i]
					} else {
						ws[i] = float32(math.Round(float64(colWidths[i] / max(z, 0.1))))
					}
				}
				tbl.Widths = ws
			}
			tbl.Widths[rc] = finalW
			s.doc.Blocks[idx].Dirty = true
			s.doc.Blocks[idx].CleanSource = ""
			s.markDirty()
		}
	}
}

func (s *Session) isOverlayBtnHovered(btnID string) bool {
	r, ok := s.overlayBtnBounds(btnID)
	if !ok || r.W <= 0 || r.H <= 0 {
		return false
	}
	pt, has := s.windowPointerStatus()
	if !has {
		return false
	}
	pad := s.scale(theme.Space4)
	return pt.X >= r.X-pad && pt.X <= r.X+r.W+pad && pt.Y >= r.Y-pad && pt.Y <= r.Y+r.H+pad
}

// --- Notion-Style Table Border Context Menu Overlay ---

func (s *Session) renderTableBorderMenuOverlay(c *ui.Context) {
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
	if b.Table == nil || len(b.Table.Rows) == 0 || len(b.Table.Rows[0]) == 0 {
		s.tableMenuBlock = 0
		s.tableMenuKind = ""
		return
	}

	pal := theme.Ocean()
	tblCols := pal.Table()
	id := b.ID
	tbl := b.Table
	rows := len(tbl.Rows)
	cols := len(tbl.Rows[0])
	kind := s.tableMenuKind

	targetRow := clampInt(s.tableActiveRow, 0, rows-1)
	targetCol := clampInt(s.tableActiveCol, 0, cols-1)
	if kind == "row" {
		targetRow = clampInt(s.tableMenuIndex, 0, rows-1)
	} else if kind == "col" {
		targetCol = clampInt(s.tableMenuIndex, 0, cols-1)
	}

	vp, winW, winH := s.overlayViewport(c)
	var trigger ui.Rect
	var hasTrig bool
	alignKind := appUi.AlignLeft
	switch kind {
	case "col":
		trigger, hasTrig = s.overlayBtnBounds(fmt.Sprintf("table-col-handle-%d-%d", id, targetCol))
		if !hasTrig {
			if cr, ok := s.tableCellRect(id, 0, targetCol); ok && cr.W > 0 {
				trigger, hasTrig = ui.Rect{X: cr.X, Y: cr.Y, W: cr.W, H: theme.HeightMicro}, true
			}
		}
		alignKind = appUi.AlignCenter
	case "row":
		trigger, hasTrig = s.overlayBtnBounds(fmt.Sprintf("table-row-handle-%d-%d", id, targetRow))
		if !hasTrig {
			if cr, ok := s.tableCellRect(id, targetRow, 0); ok && cr.H > 0 {
				trigger, hasTrig = ui.Rect{X: cr.X, Y: cr.Y, W: theme.HeightMicro, H: cr.H}, true
			}
		}
	case "table":
		trigger, hasTrig = s.overlayBtnBounds(fmt.Sprintf("block-grip-%d", id))
		if !hasTrig {
			trigger, hasTrig = s.overlayBtnBounds(fmt.Sprintf("table-whole-handle-%d", id))
		}
	default:
		trigger, hasTrig = s.overlayBtnBounds(fmt.Sprintf("table-more-%d", id))
		alignKind = appUi.AlignRight
	}
	if !hasTrig {
		if br, ok := s.blockRect(id); ok && br.W > 0 {
			trigger = ui.Rect{X: br.X + s.leftContentGutter(), Y: max(br.Y, vp.Y), W: theme.HeightCompact, H: theme.HeightCompact}
		} else {
			trigger = ui.Rect{X: vp.X + 160, Y: vp.Y + 120, W: theme.HeightCompact, H: theme.HeightCompact}
		}
	}

	icUp := icons.ArrowUp
	icDown := icons.ArrowDown
	icLeft := icons.ArrowLeft
	icRight := icons.ArrowRight
	icRowAbove := icons.RowInsertAbove
	icRowBelow := icons.RowInsertBelow
	icRowDel := icons.RowDelete
	icColLeft := icons.ColInsertLeft
	icColRight := icons.ColInsertRight
	icColDel := icons.ColDelete
	icDup := icons.Duplicate
	icEraser := icons.Eraser
	icHeading := icons.Heading
	icCopy := icons.Copy
	icCols := icons.Columns
	icTrash := icons.Trash

	closeMenu := func() {
		s.tableMenuBlock = 0
		s.tableMenuKind = ""
	}

	var headerTitle string
	var entries []appUi.MenuItemSpec
	var showAlignBar, showTintBar bool
	var deleteAction func()

	switch kind {
	case "col":
		colLabel := strings.TrimSpace(tbl.Rows[0][targetCol].PlainText())
		if colLabel == "" {
			colLabel = fmt.Sprintf(theme.Tr("第 %d 列", "Column %d"), targetCol+1)
		}
		headerTitle = colLabel
		showAlignBar = true
		showTintBar = true
		deleteAction = func() {
			if cols > 1 {
				s.tableDeleteCol(id, targetCol)
				closeMenu()
			}
		}
		if targetCol == 0 {
			entries = append(entries, appUi.MenuItemSpec{
				ID:      "tbl-col-header-toggle",
				Label:   theme.Tr("设为标题列", "Header column"),
				Icon:    &icHeading,
				Checked: tbl.HeaderCol,
				OnClick: func() {
					s.tableToggleHeaderCol(id)
				},
			}, appUi.MenuItemSpec{Separator: true})
		}
		entries = append(entries,
			appUi.MenuItemSpec{ID: "tbl-col-sort-asc", Label: theme.Tr("按 A → Z 升序排列", "Sort A → Z"), Icon: &icUp, Disabled: rows <= 1, OnClick: func() {
				s.tableSortByCol(id, targetCol, true)
				closeMenu()
			}},
			appUi.MenuItemSpec{ID: "tbl-col-sort-desc", Label: theme.Tr("按 Z → A 降序排列", "Sort Z → A"), Icon: &icDown, Disabled: rows <= 1, OnClick: func() {
				s.tableSortByCol(id, targetCol, false)
				closeMenu()
			}},
			appUi.MenuItemSpec{Separator: true},
			appUi.MenuItemSpec{ID: "tbl-col-left", Label: theme.Tr("左侧插入列", "Insert column left"), Icon: &icColLeft, OnClick: func() {
				s.tableAddCol(id, targetCol)
				closeMenu()
			}},
			appUi.MenuItemSpec{ID: "tbl-col-right", Label: theme.Tr("右侧插入列", "Insert column right"), Icon: &icColRight, OnClick: func() {
				s.tableAddCol(id, targetCol+1)
				closeMenu()
			}},
			appUi.MenuItemSpec{ID: "tbl-col-move-left", Label: theme.Tr("向左移动一列", "Move column left"), Icon: &icLeft, Disabled: targetCol <= 0, OnClick: func() {
				s.tableMoveCol(id, targetCol, targetCol-1)
			}},
			appUi.MenuItemSpec{ID: "tbl-col-move-right", Label: theme.Tr("向右移动一列", "Move column right"), Icon: &icRight, Disabled: targetCol >= cols-1, OnClick: func() {
				s.tableMoveCol(id, targetCol, targetCol+1)
			}},
			appUi.MenuItemSpec{ID: "tbl-col-clear", Label: theme.Tr("清空本列内容", "Clear column contents"), Icon: &icEraser, OnClick: func() {
				s.tableClearCol(id, targetCol)
				closeMenu()
			}},
			appUi.MenuItemSpec{Separator: true},
			appUi.MenuItemSpec{ID: "tbl-col-del", Label: theme.Tr("删除当前列", "Delete column"), Shortcut: "⌫", Icon: &icColDel, Danger: true, Disabled: cols <= 1, OnClick: deleteAction},
		)

	case "row":
		if targetRow == 0 && !tbl.NoHeaderRow {
			headerTitle = theme.Tr("表头行", "Header row")
		} else {
			headerTitle = fmt.Sprintf(theme.Tr("第 %d 行", "Row %d"), targetRow+1)
		}
		showAlignBar = true
		showTintBar = true
		deleteAction = func() {
			if rows > 1 {
				s.tableDeleteRow(id, targetRow)
				closeMenu()
			}
		}
		if targetRow == 0 {
			entries = append(entries, appUi.MenuItemSpec{
				ID:      "tbl-row-header-toggle",
				Label:   theme.Tr("设为标题行", "Header row"),
				Icon:    &icHeading,
				Checked: !tbl.NoHeaderRow,
				OnClick: func() {
					s.tableToggleHeaderRow(id)
				},
			}, appUi.MenuItemSpec{Separator: true})
		}
		entries = append(entries,
			appUi.MenuItemSpec{ID: "tbl-row-above", Label: theme.Tr("上方插入行", "Insert row above"), Icon: &icRowAbove, OnClick: func() {
				s.tableAddRow(id, targetRow)
				closeMenu()
			}},
			appUi.MenuItemSpec{ID: "tbl-row-below", Label: theme.Tr("下方插入行", "Insert row below"), Icon: &icRowBelow, OnClick: func() {
				s.tableAddRow(id, targetRow+1)
				closeMenu()
			}},
			appUi.MenuItemSpec{ID: "tbl-row-dup", Label: theme.Tr("复制当前行到下方", "Duplicate row below"), Icon: &icDup, OnClick: func() {
				s.tableDuplicateRow(id, targetRow)
				closeMenu()
			}},
			appUi.MenuItemSpec{ID: "tbl-row-move-up", Label: theme.Tr("向上移动一行", "Move row up"), Icon: &icUp, Disabled: targetRow <= 0 || (!tbl.NoHeaderRow && targetRow == 1), OnClick: func() {
				s.tableMoveRow(id, targetRow, targetRow-1)
			}},
			appUi.MenuItemSpec{ID: "tbl-row-move-down", Label: theme.Tr("向下移动一行", "Move row down"), Icon: &icDown, Disabled: targetRow >= rows-1 || (!tbl.NoHeaderRow && targetRow == 0), OnClick: func() {
				s.tableMoveRow(id, targetRow, targetRow+1)
			}},
			appUi.MenuItemSpec{ID: "tbl-row-clear", Label: theme.Tr("清空本行内容", "Clear row contents"), Icon: &icEraser, OnClick: func() {
				s.tableClearRow(id, targetRow)
				closeMenu()
			}},
			appUi.MenuItemSpec{Separator: true},
			appUi.MenuItemSpec{ID: "tbl-row-del", Label: theme.Tr("删除当前行", "Delete row"), Shortcut: "⌫", Icon: &icRowDel, Danger: true, Disabled: rows <= 1, OnClick: deleteAction},
		)

	case "table":
		showAlignBar = true
		deleteAction = func() {
			closeMenu()
			s.deleteBlock(idx)
		}
		entries = []appUi.MenuItemSpec{
			{ID: "tbl-header-toggle", Label: theme.Tr("标题行", "Header row"), Icon: &icHeading, Checked: !tbl.NoHeaderRow, OnClick: func() {
				s.tableToggleHeaderRow(id)
			}},
			{ID: "tbl-header-col-toggle", Label: theme.Tr("标题列", "Header column"), Icon: &icHeading, Checked: tbl.HeaderCol, OnClick: func() {
				s.tableToggleHeaderCol(id)
			}},
			{Separator: true},
			{ID: "tbl-copy-md", Label: theme.Tr("复制为 Markdown 表格", "Copy as Markdown"), Icon: &icCopy, OnClick: func() {
				s.tableCopyMarkdown(c.Services(), idx)
				closeMenu()
			}},
			{ID: "tbl-fit-cols", Label: theme.Tr("均分自动列宽", "Auto-fit column widths"), Icon: &icCols, Disabled: len(tbl.Widths) == 0, OnClick: func() {
				s.tableResetColWidths(id)
				closeMenu()
			}},
			{ID: "tbl-clear-all", Label: theme.Tr("清空表格数据", "Clear table data"), Icon: &icEraser, OnClick: func() {
				s.tableClearAll(id)
				closeMenu()
			}},
			{Separator: true},
			{ID: "tbl-delete", Label: theme.Tr("删除整张表格", "Delete table"), Shortcut: "⌫", Icon: &icTrash, Danger: true, OnClick: deleteAction},
		}

	default:
		deleteAction = func() {
			closeMenu()
			s.deleteBlock(idx)
		}
		entries = []appUi.MenuItemSpec{
			{ID: "tbl-row-above", Label: theme.Tr("上方插入行", "Insert row above"), Icon: &icRowAbove, OnClick: func() {
				s.tableAddRow(id, targetRow)
				closeMenu()
			}},
			{ID: "tbl-row-below", Label: theme.Tr("下方插入行", "Insert row below"), Icon: &icRowBelow, OnClick: func() {
				s.tableAddRow(id, targetRow+1)
				closeMenu()
			}},
			{ID: "tbl-row-del", Label: theme.Tr("删除当前行", "Delete row"), Icon: &icRowDel, Disabled: rows <= 1, OnClick: func() {
				s.tableDeleteRow(id, targetRow)
				closeMenu()
			}},
			{Separator: true},
			{ID: "tbl-col-left", Label: theme.Tr("左侧插入列", "Insert column left"), Icon: &icColLeft, OnClick: func() {
				s.tableAddCol(id, targetCol)
				closeMenu()
			}},
			{ID: "tbl-col-right", Label: theme.Tr("右侧插入列", "Insert column right"), Icon: &icColRight, OnClick: func() {
				s.tableAddCol(id, targetCol+1)
				closeMenu()
			}},
			{ID: "tbl-col-del", Label: theme.Tr("删除当前列", "Delete column"), Icon: &icColDel, Disabled: cols <= 1, OnClick: func() {
				s.tableDeleteCol(id, targetCol)
				closeMenu()
			}},
			{Separator: true},
			{ID: "tbl-delete", Label: theme.Tr("删除表格", "Delete table"), Icon: &icTrash, Danger: true, OnClick: deleteAction},
		}
	}

	itemCount, sepCount := 0, 0
	for _, it := range entries {
		if it.Separator {
			sepCount++
		} else {
			itemCount++
		}
	}
	extraRows := 0
	if headerTitle != "" {
		extraRows++
	}
	if showAlignBar {
		extraRows++
	}
	if showTintBar {
		extraRows++
		sepCount++
	}

	menuW := theme.TableMenuWidth
	spec := appUi.PlaceBelow(trigger).WithViewport(vp).WithAlignX(appUi.AlignX{Kind: alignKind}).WithGap(theme.Space6).WithMargin(theme.Space8)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 380)
	_, measH := s.measuredOrEstSize("table-menu", menuW, estimateEditorMenuHeight(itemCount+extraRows, sepCount))
	estH := min(measH, maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)

	ui.Overlay(c, func() {
		shell := appUi.MenuSurface(ui.Column(c.Key("table-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
		shell.Focusable().AutoFocus()
		cur := ui.Local(shell, localMenuCursorKey{}, func() int { return -1 })
		appUi.NavigateMenu(c, shell, entries, cur)
		if shell.OverlayShortcut(ui.Cmd, ui.KeyC) {
			if s.tableCopyMenuSelection(c.Services(), idx, kind, targetRow, targetCol) {
				closeMenu()
				c.Invalidate()
				return
			}
		}
		if shell.OverlayShortcut(ui.Cmd, ui.KeyX) {
			if s.tableCutMenuSelection(c.Services(), idx, kind, targetRow, targetCol) {
				closeMenu()
				c.Invalidate()
				return
			}
		}
		if shell.OverlayShortcut(ui.Cmd, ui.KeyV) {
			if s.readOnly {
				closeMenu()
				c.Invalidate()
				return
			}
			closeMenu()
			s.tablePasteMenuSelection(c.Services(), idx, kind, targetRow, targetCol)
			c.Invalidate()
			return
		}
		if !s.readOnly && deleteAction != nil && (shell.OverlayShortcut(0, ui.KeyBackspace) || shell.OverlayShortcut(0, ui.KeyDelete)) {
			deleteAction()
			c.Invalidate()
			return
		}
		if shell.OverlayShortcut(0, ui.KeyEscape) {
			closeMenu()
			c.Invalidate()
			return
		}
		appUi.Probe(shell, "table-menu")
		s.recordOverlayPanel("table-menu", shell)

		shell.Children(func() {
			appUi.Scroll(c.Key("table-menu-scroll")).FillWidth().MaxHeight(appUi.MenuInnerMaxHeight(maxH)).Children(func() {
				ui.Column(c.Key("table-menu-list")).FillWidth().Gap(theme.MenuGap).Children(func() {
					if headerTitle != "" {
						hdr := ui.Row(c.Key("table-menu-hdr")).FillWidth().Height(theme.HeightCompact).
							PaddingX(theme.Space10).AlignItems(ui.Center)
						hdr.Children(func() {
							ui.Text(c, headerTitle).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
								TextColor(pal.MenuMuted).Grow(1).SingleLine()
						})
					}

					if showAlignBar {
						curAlign, uniform := tableUniformAlign(tbl, kind, targetRow, targetCol)
						alignLabel := theme.Tr("对齐方式", "Alignment")
						if kind == "table" {
							alignLabel = theme.Tr("整表对齐", "Alignment")
						}
						alignRow := ui.Row(c.Key("table-menu-align-row")).FillWidth().Height(theme.HeightControl).
							Padding(0, theme.Space8, 0, theme.Space10).AlignItems(ui.Center).Justify(ui.SpaceBetween)
						alignRow.Children(func() {
							ui.Text(c, alignLabel).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
							seg := ui.Row(c.Key("table-menu-align-seg")).Padding(theme.Space2).Gap(theme.Space2).
								Radius(theme.RadiusSM).Background(pal.Shell).Border(1, tblCols.Border)
							seg.Children(func() {
								for _, opt := range []struct {
									id    string
									align model.ColAlign
									icon  icons.Icon
									label string
								}{
									{"tbl-align-left", model.ColAlignLeft, icons.AlignLeft, theme.Tr("左对齐", "Align left")},
									{"tbl-align-center", model.ColAlignCenter, icons.AlignCenter, theme.Tr("居中对齐", "Align center")},
									{"tbl-align-right", model.ColAlignRight, icons.AlignRight, theme.Tr("右对齐", "Align right")},
								} {
									active := uniform && curAlign == opt.align
									ab := ui.ButtonBase(c.Key(opt.id)).Size(theme.TableAlignSegBoxW, theme.TableAlignSegBoxH).
										Center().Radius(theme.RadiusXS).Cursor(ui.CursorPointer).Label(opt.label)
									hov := ab.Hovered()
									bg := ui.Transparent
									bd := ui.Transparent
									fg := pal.TextSecondary
									if active {
										bg, bd, fg = pal.Selected, pal.SelectedBorder, pal.Text
									} else if hov {
										bg, fg = pal.MenuHover, pal.Text
									}
									appUi.AnimateSurfaceColors(ab, bg, bd, active)
									ab.Children(func() {
										ic := pal.DuoIconColors(fg)
										if active {
											ic = pal.LitDuoIconColors(pal.Text)
										}
										icons.DrawAnimated(c, opt.icon, theme.IconInline, ic, active || hov || ab.Pressed())
									})
									appUi.Probe(ab, opt.id)
									if !s.readOnly && ab.Clicked() {
										switch kind {
										case "row":
											s.tableSetRowAlign(id, targetRow, opt.align)
										case "table":
											s.tableSetAllAlign(id, opt.align)
										default:
											s.tableSetColAlign(id, targetCol, opt.align)
										}
									}
								}
							})
						})
					}

					// Render menu entries, injecting the tint swatch bar right before the final separator + delete row.
					lastSepIdx := -1
					if showTintBar {
						for i := len(entries) - 1; i >= 0; i-- {
							if entries[i].Separator {
								lastSepIdx = i
								break
							}
						}
					}

					for i, it := range entries {
						if showTintBar && i == lastSepIdx {
							appUi.MenuRow(c, appUi.MenuItemSpec{Separator: true}, false)
							s.renderTableMenuTintRow(c, id, tbl, kind, targetRow, targetCol, pal, tblCols)
						}
						if s.readOnly && it.ID != "tbl-copy-md" && !it.Separator {
							it.Disabled = true
						}
						appUi.MenuRow(c, it, i == *cur)
					}
				})
			})
		})
		if shell.PressedOutside() {
			pt, hasPt := s.windowPointerStatus()
			inTrigger := hasPt && trigger.W > 0 && trigger.H > 0 &&
				pt.X >= trigger.X && pt.X <= trigger.X+trigger.W &&
				pt.Y >= trigger.Y && pt.Y <= trigger.Y+trigger.H
			if !inTrigger {
				closeMenu()
			}
		}
	})
}

func (s *Session) renderTableMenuTintRow(
	c *ui.Context,
	id model.BlockID,
	tbl *model.TableData,
	kind string,
	targetRow, targetCol int,
	pal theme.Palette,
	tblCols theme.TableColors,
) {
	curTint, uniform := tableUniformTint(tbl, kind, targetRow, targetCol)

	tintRow := ui.Row(c.Key("table-menu-tint-row")).FillWidth().Height(theme.HeightControl).
		PaddingX(theme.Space10).AlignItems(ui.Center).Justify(ui.SpaceBetween)
	tintRow.Children(func() {
		ui.Text(c, theme.Tr("底色标记", "Background")).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
		swRow := ui.Row(c.Key("table-menu-swatches")).AlignItems(ui.Center).Gap(theme.Space4)
		swRow.Children(func() {
			for _, sw := range []struct {
				id    string
				tint  model.TableTint
				fill  ui.Color
				label string
			}{
				{"tbl-tint-none", model.TableTintNone, pal.Paper, theme.Tr("默认无底色", "Default")},
				{"tbl-tint-slate", model.TableTintSlate, tblCols.SwatchSlate, theme.Tr("中性石墨底色", "Slate")},
				{"tbl-tint-ocean", model.TableTintOcean, tblCols.SwatchOcean, theme.Tr("Ocean 海蓝底色", "Ocean")},
				{"tbl-tint-emerald", model.TableTintEmerald, tblCols.SwatchEmerald, theme.Tr("翠绿标记底色", "Emerald")},
				{"tbl-tint-amber", model.TableTintAmber, tblCols.SwatchAmber, theme.Tr("琥珀提醒底色", "Amber")},
				{"tbl-tint-rose", model.TableTintRose, tblCols.SwatchRose, theme.Tr("珊瑚警示底色", "Rose")},
			} {
				on := uniform && curTint == sw.tint
				btn := ui.ButtonBase(c.Key(sw.id)).Size(theme.TableSwatchSize, theme.TableSwatchSize).
					Radius(theme.RadiusPill).Cursor(ui.CursorPointer).Label(sw.label)
				bd := tblCols.HandleBorder
				bw := float32(1)
				if on {
					bd = pal.Accent
					bw = theme.TableSwatchRingWidth
				} else if btn.Hovered() {
					bd = pal.Accent
				}
				btn.Background(sw.fill).Border(bw, bd)
				appUi.TactileButton(btn, btn.Hovered(), btn.Pressed(), on)
				appUi.Probe(btn, sw.id)
				if !s.readOnly && btn.Clicked() {
					if kind == "row" {
						s.tableSetRowTint(id, targetRow, sw.tint)
					} else {
						s.tableSetColTint(id, targetCol, sw.tint)
					}
				}
			}
		})
	})
}
