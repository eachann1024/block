package editor

import (
	"math"

	"block/internal/model"
	"github.com/egoist/mygo/ui"
)

type tableCellGeom struct {
	docRect ui.Rect
	hScroll bool
}

func nearDocRect(a, b ui.Rect) bool {
	const eps = 0.25
	return math.Abs(float64(a.X-b.X)) <= eps &&
		math.Abs(float64(a.Y-b.Y)) <= eps &&
		math.Abs(float64(a.W-b.W)) <= eps &&
		math.Abs(float64(a.H-b.H)) <= eps
}

func (s *Session) winToDocRect(winR ui.Rect) ui.Rect {
	return ui.Rect{
		X: winR.X - s.scrollRect.X,
		Y: winR.Y - s.scrollRect.Y + s.bodyScroll.Y,
		W: winR.W,
		H: winR.H,
	}
}

func (s *Session) docToWinRect(docR ui.Rect) ui.Rect {
	return ui.Rect{
		X: s.scrollRect.X + docR.X,
		Y: s.scrollRect.Y + docR.Y - s.bodyScroll.Y,
		W: docR.W,
		H: docR.H,
	}
}

func (s *Session) blockRect(id model.BlockID) (ui.Rect, bool) {
	r, ok := s.blockDocRects[id]
	if !ok || r.W <= 0 || r.H <= 0 {
		return ui.Rect{}, false
	}
	return s.docToWinRect(r), true
}

func (s *Session) blockRectValue(id model.BlockID) ui.Rect {
	r, _ := s.blockRect(id)
	return r
}

func (s *Session) blockTextRect(id model.BlockID) (ui.Rect, bool) {
	r, ok := s.blockTextDocRects[id]
	if !ok || r.W <= 0 || r.H <= 0 {
		return ui.Rect{}, false
	}
	return s.docToWinRect(r), true
}

func (s *Session) blockTextRectValue(id model.BlockID) ui.Rect {
	r, _ := s.blockTextRect(id)
	return r
}

func (s *Session) tableCellRect(blockID model.BlockID, row, col int) (ui.Rect, bool) {
	g, ok := s.tableCellDocRects[tableCellKey{BlockID: blockID, Row: row, Col: col}]
	if !ok || g.docRect.W <= 0 || g.docRect.H <= 0 {
		return ui.Rect{}, false
	}
	winR := s.docToWinRect(g.docRect)
	if g.hScroll {
		if hs, hasHS := s.tableHScrolls[blockID]; hasHS && hs != nil {
			winR.X -= hs.X
		}
	}
	return winR, true
}

func (s *Session) imageRect(id model.BlockID) (ui.Rect, bool) {
	r, ok := s.imageDocRects[id]
	if !ok || r.W <= 0 || r.H <= 0 {
		return ui.Rect{}, false
	}
	return s.docToWinRect(r), true
}

func (s *Session) imageRectValue(id model.BlockID) ui.Rect {
	r, _ := s.imageRect(id)
	return r
}

func (s *Session) setBlockDocRect(id model.BlockID, docR ui.Rect) bool {
	if docR.W <= 0 || docR.H <= 0 {
		return false
	}
	if s.blockDocRects == nil {
		s.blockDocRects = make(map[model.BlockID]ui.Rect)
	}
	prev, ok := s.blockDocRects[id]
	s.blockDocRects[id] = docR
	return !ok || !nearDocRect(prev, docR)
}

func (s *Session) recordBlockLayoutBounds(id model.BlockID, elem ui.Element) {
	if !s.hasCanvasOrigin {
		return
	}
	b := elem.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	s.setBlockDocRect(id, ui.Rect{
		X: b.X - s.canvasLayoutOrigin.X,
		Y: b.Y - s.canvasLayoutOrigin.Y,
		W: b.W,
		H: b.H,
	})
}

func (s *Session) recordBlockWinRect(id model.BlockID, winR ui.Rect) bool {
	if winR.W <= 0 || winR.H <= 0 {
		return false
	}
	return s.setBlockDocRect(id, s.winToDocRect(winR))
}

func (s *Session) deleteBlockRect(id model.BlockID) {
	delete(s.blockDocRects, id)
}

func (s *Session) setBlockTextDocRect(id model.BlockID, docR ui.Rect) {
	if docR.W <= 0 || docR.H <= 0 {
		return
	}
	if s.blockTextDocRects == nil {
		s.blockTextDocRects = make(map[model.BlockID]ui.Rect)
	}
	s.blockTextDocRects[id] = docR
}

func (s *Session) recordBlockTextLayoutBounds(id model.BlockID, elem ui.Element) {
	if !s.hasCanvasOrigin {
		return
	}
	b := elem.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	s.setBlockTextDocRect(id, ui.Rect{
		X: b.X - s.canvasLayoutOrigin.X,
		Y: b.Y - s.canvasLayoutOrigin.Y,
		W: b.W,
		H: b.H,
	})
}

func (s *Session) recordBlockTextWinRect(id model.BlockID, winR ui.Rect) {
	if winR.W <= 0 || winR.H <= 0 {
		return
	}
	s.setBlockTextDocRect(id, s.winToDocRect(winR))
}

func (s *Session) setTableCellDocRect(blockID model.BlockID, row, col int, docR ui.Rect, needHScroll bool) {
	if docR.W <= 0 || docR.H <= 0 {
		return
	}
	if s.tableCellDocRects == nil {
		s.tableCellDocRects = make(map[tableCellKey]tableCellGeom)
	}
	s.tableCellDocRects[tableCellKey{BlockID: blockID, Row: row, Col: col}] = tableCellGeom{
		docRect: docR,
		hScroll: needHScroll,
	}
}

func (s *Session) recordTableCellLayoutBounds(blockID model.BlockID, row, col int, cellElem, gridRowElem, gridElem ui.Element, needHScroll bool) {
	if !s.hasCanvasOrigin {
		return
	}
	b := cellElem.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	docX := b.X - s.canvasLayoutOrigin.X
	if needHScroll {
		grb := gridRowElem.Bounds()
		gb := gridElem.Bounds()
		if grb.W <= 0 || gb.W <= 0 {
			return
		}
		docX = (grb.X - s.canvasLayoutOrigin.X) + (b.X - gb.X)
	}
	s.setTableCellDocRect(blockID, row, col, ui.Rect{
		X: docX,
		Y: b.Y - s.canvasLayoutOrigin.Y,
		W: b.W,
		H: b.H,
	}, needHScroll)
}

func (s *Session) recordTableCellWinRect(blockID model.BlockID, row, col int, winR ui.Rect, needHScroll bool) {
	if winR.W <= 0 || winR.H <= 0 {
		return
	}
	docR := s.winToDocRect(winR)
	if needHScroll {
		docR.X += s.tableHScroll(blockID).X
	}
	s.setTableCellDocRect(blockID, row, col, docR, needHScroll)
}

func (s *Session) setImageDocRect(id model.BlockID, docR ui.Rect) bool {
	if docR.W <= 0 || docR.H <= 0 {
		return false
	}
	if s.imageDocRects == nil {
		s.imageDocRects = make(map[model.BlockID]ui.Rect)
	}
	prev, ok := s.imageDocRects[id]
	s.imageDocRects[id] = docR
	return !ok || !nearDocRect(prev, docR)
}

func (s *Session) recordImageLayoutBounds(id model.BlockID, elem ui.Element) {
	if !s.hasCanvasOrigin {
		return
	}
	b := elem.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	s.setImageDocRect(id, ui.Rect{
		X: b.X - s.canvasLayoutOrigin.X,
		Y: b.Y - s.canvasLayoutOrigin.Y,
		W: b.W,
		H: b.H,
	})
}

func (s *Session) recordImageWinRect(id model.BlockID, winR ui.Rect) bool {
	if winR.W <= 0 || winR.H <= 0 {
		return false
	}
	return s.setImageDocRect(id, s.winToDocRect(winR))
}
