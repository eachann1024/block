package editor

import (
	"math"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const blockDragThreshold float64 = 4

func blockHandleTop(b *model.Block, topGap float32) float32 {
	if b == nil {
		return 0
	}
	switch {
	case b.Kind == model.BlockTable:
		return 4.9
	case b.Kind == model.BlockImage:
		return 4
	case b.Kind == model.BlockCallout:
		return max(7, -topGap)
	case b.Kind == model.BlockDivider:
		return max((18-26)*0.5, -topGap)
	case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
		return max((30-26)*0.5, -topGap)
	default:
		fontSize, _, lh := blockTextMetrics(b.Kind)
		return max((fontSize*lh-26)*0.5, -topGap)
	}
}

func (s *Session) resetBlockDrag() {
	s.dragBlockID = 0
	s.dragOverIdx = -1
	s.dragAfter = false
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
	if s.doc == nil || from < 0 || from >= len(s.doc.Blocks) || to < 0 || to >= len(s.doc.Blocks) || from == to {
		return
	}
	s.moveBlock(from, to)
	if s.focusedBlock >= 0 && s.focusedBlock < len(s.doc.Blocks) {
		s.selAnchor = Position{Block: s.focusedBlock, Offset: 0}
		s.selHead = s.selAnchor
		s.selSyncPending = true
	}
	c.Invalidate()
}

func (s *Session) cancelBlockDrag() {
	if s.dragBlockID == 0 {
		return
	}
	s.dragCanceled = true
	s.dragActive = false
	s.dragOverIdx = -1
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

func (s *Session) updateBlockDropTarget(fromIdx int, wx, wy float32) {
	if s.doc == nil || fromIdx < 0 || fromIdx >= len(s.doc.Blocks) || !s.isPointerInDropViewport(wx, wy) {
		s.dragOverIdx = -1
		s.dragAfter = false
		return
	}
	bestIdx := -1
	bestDist := float32(1e9)
	for i := range s.doc.Blocks {
		if s.isBlockHiddenByFold(i) {
			continue
		}
		r, ok := s.blockRects[s.doc.Blocks[i].ID]
		if !ok || r.H <= 0 {
			continue
		}
		var dist float32
		if wy < r.Y {
			dist = r.Y - wy
		} else if wy > r.Y+r.H {
			dist = wy - (r.Y + r.H)
		}
		if dist < bestDist {
			bestDist = dist
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		s.dragOverIdx = -1
		s.dragAfter = false
		return
	}
	r := s.blockRects[s.doc.Blocks[bestIdx].ID]
	after := false
	switch {
	case bestIdx < fromIdx:
		after = false
	case bestIdx > fromIdx:
		after = true
	default:
		after = wy >= r.Y+r.H*0.5
	}
	slot := bestIdx
	if after {
		slot = bestIdx + 1
	}
	to := slot
	if slot > fromIdx {
		to = slot - 1
	}
	if to == fromIdx {
		s.dragOverIdx = -1
		s.dragAfter = false
		return
	}
	s.dragOverIdx = bestIdx
	s.dragAfter = after
}

func (s *Session) resolvedMoveTarget(fromIdx int) int {
	if s.doc == nil || s.dragOverIdx < 0 || s.dragOverIdx >= len(s.doc.Blocks) {
		return -1
	}
	slot := s.dragOverIdx
	if s.dragAfter {
		slot = s.dragOverIdx + 1
	}
	to := slot
	if slot > fromIdx {
		to = slot - 1
	}
	if to < 0 || to >= len(s.doc.Blocks) || to == fromIdx {
		return -1
	}
	return to
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
				s.dragActive = true
				c.Invalidate()
			}
		}
		if s.dragActive {
			prevOver, prevAfter := s.dragOverIdx, s.dragAfter
			s.updateBlockDropTarget(idx, wx, wy)
			s.autoScrollOnDrag(c, wy)
			if s.dragOverIdx != prevOver || s.dragAfter != prevAfter {
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
		s.resetBlockDrag()
		if wasCanceled {
			c.Invalidate()
			return
		}
		if wasActive {
			if target >= 0 && target != idx && s.isPointerInDropViewport(wx, wy) {
				s.dragDropPending = true
				s.dragDropFrom = idx
				s.dragDropTo = target
			}
			c.Invalidate()
			return
		}
	}

	if grip.Clicked() {
		s.pressJustBegan = false
		s.firstLineDeleteArmed = false
		s.FocusBlock(idx)
		trigger := grip.Bounds()
		if trigger.W <= 0 {
			trigger = s.blockRects[id]
		}
		s.openHandleMenu(idx, trigger)
	}
}

func (s *Session) paintBlockDropMarker(p *ui.Painter, r ui.Rect, b *model.Block, pal theme.Palette) {
	if b == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	indentPx := float32(min(max(b.Indent, 0), 12)) * editorIndentStep
	leftX := r.X + editorHandleWidth + editorRowGap + indentPx
	rightX := r.X + r.W - editorRightInset
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
