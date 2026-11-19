package editor

import (
	"math"
	"time"

	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	edgeScrollSuppressWindow  = 350 * time.Millisecond
	hoverScrollSuppressWindow = 140 * time.Millisecond
)

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

// IsScrollingForHover reports whether the document is actively scrolling or just scrolled,
// suppressing transient block/table hover chrome, link span mounting, and backdrop path churn.
func (s *Session) IsScrollingForHover(now time.Time) bool {
	if s.edgeScrollInit && math.Abs(float64(s.bodyScroll.Y-s.edgeLastScrollY)) > 0.5 {
		return true
	}
	return !s.hoverScrollQuietAt.IsZero() && now.Before(s.hoverScrollQuietAt)
}

func (s *Session) animatedEdgeInsets(c *ui.Context, root ui.Element) (float32, float32) {
	now := c.Now()
	if !s.edgeScrollInit {
		s.edgeScrollInit = true
		s.edgeLastScrollY = s.bodyScroll.Y
	} else if math.Abs(float64(s.bodyScroll.Y-s.edgeLastScrollY)) > 0.5 {
		s.edgeLastScrollY = s.bodyScroll.Y
		s.edgeScrollQuietAt = now.Add(edgeScrollSuppressWindow)
		s.hoverScrollQuietAt = now.Add(hoverScrollSuppressWindow)
	}
	if !s.hoverScrollQuietAt.IsZero() {
		if rem := s.hoverScrollQuietAt.Sub(now); rem > 0 {
			c.After(rem + 5*time.Millisecond)
		} else {
			s.hoverScrollQuietAt = time.Time{}
		}
	}
	left := appUi.SpringPanelTo(root, "edgeInsetL", s.edgeInsetLeft)
	right := appUi.SpringPanelTo(root, "edgeInsetR", s.edgeInsetRight)
	return left, right
}
