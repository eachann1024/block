package editor

import (
	"math"
	"time"

	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const edgeScrollSuppressWindow = 350 * time.Millisecond

// SetEdgeFloatInsets sets target left/right safe insets so floating edge panels
// nudge the editor column smoothly while keeping the 54px block-handle gutter clear.
func (s *Session) SetEdgeFloatInsets(left, right float32) {
	s.edgeInsetLeft = max(left, 0)
	s.edgeInsetRight = max(right, 0)
}

// OuterFlowWidth returns the full outer editor column width (body + symmetric gutters) at current zoom.
func (s *Session) OuterFlowWidth() float32 {
	return s.outerFlowWidth()
}

// DocRevision returns the monotonic document mutation counter used to detect active typing.
func (s *Session) DocRevision() uint64 {
	return s.docRev
}

// IsBusyForEdgeFloat reports whether an active editor drag, resize, or floating menu
// should suppress edge-dwell panel summoning (or dismiss an open floating card on block drag).
func (s *Session) IsBusyForEdgeFloat() bool {
	return s.dragBlockID != 0 || s.isMouseDragging() || s.imgResizeBlock != 0 || s.hasAnyOverlayOpen()
}

// IsDraggingBlock reports whether a block handle drag is currently underway.
func (s *Session) IsDraggingBlock() bool {
	return s.dragBlockID != 0
}

// IsScrollingRecently reports whether the document scrolled within the suppression window,
// preventing right-edge scrollbar interaction from triggering the floating AI panel.
func (s *Session) IsScrollingRecently(now time.Time) bool {
	return !s.edgeScrollQuietAt.IsZero() && now.Before(s.edgeScrollQuietAt)
}

func (s *Session) animatedEdgeInsets(c *ui.Context, root ui.Element) (float32, float32) {
	now := c.Now()
	if !s.edgeScrollInit {
		s.edgeScrollInit = true
		s.edgeLastScrollY = s.bodyScroll.Y
	} else if math.Abs(float64(s.bodyScroll.Y-s.edgeLastScrollY)) > 0.5 {
		s.edgeLastScrollY = s.bodyScroll.Y
		s.edgeScrollQuietAt = now.Add(edgeScrollSuppressWindow)
	}
	left := appUi.SpringPanelTo(root, "edgeInsetL", s.edgeInsetLeft)
	right := appUi.SpringPanelTo(root, "edgeInsetR", s.edgeInsetRight)
	return left, right
}
