package ui

import (
	"math"
	"time"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
)

// Scrollbar spec "A+D": a 2px square, flush line centred in the
// ScrollGutter hit area at rest; ScrollThumbHoverWidth while the pointer is
// over the hit area or a thumb is dragged. Identical on macOS / Windows / Linux.

// StandardScrollbarStyle is the resting style every window theme installs
// (uiTheme.ScrollbarStyle). Thumb / Hover / Active come from pal.
func StandardScrollbarStyle(pal theme.Palette) ui.ScrollbarStyle {
	return ui.ScrollbarStyle{
		TrackWidth: theme.ScrollGutter,
		ThumbWidth: theme.ScrollThumbWidth,
		Square:     true,
		Flush:      true,
		MinLength:  32,
		Thumb:      pal.ScrollThumb,
		Hover:      pal.ScrollThumbHover,
		Active:     pal.ScrollThumbHover,
	}
}

// HotScrollbarStyle is StandardScrollbarStyle widened to
// theme.ScrollThumbHoverWidth, applied while the hit area is hovered or dragged.
func HotScrollbarStyle(pal theme.Palette) ui.ScrollbarStyle {
	s := StandardScrollbarStyle(pal)
	s.ThumbWidth = theme.ScrollThumbHoverWidth
	return s
}

// EditorScrollbars gives a codeed.View the same scroll-only scrollbars as
// AutoHideScrollbars: hidden at rest (no thumb, no caret marks), shown while
// scrolling and for ScrollHideDelay after, while the hit area is hovered or a
// thumb is dragged, and always while the editor's find bar is open. Showing or
// hiding never changes the text's layout width or height. The owner keeps one
// value per editor (zero value ready to use).
type EditorScrollbars struct {
	initialized   bool
	lastState     *codeed.State
	lastScrollY   float64
	showUntil     time.Time
	visible       bool
	hot           bool
	overBar       bool
	holdingBar    bool
	sawHScroll    bool
	findOpen      bool
	cachedWrapW   float32
	cachedBufVer  uint64
	cachedHasLong bool
	lastWrapW     float32
}

// Prepare runs before codeed.View each frame. It sets st.NoScrollbars and
// st.Scrollbar for this frame and returns the right and bottom insets the
// caller must pad the editor's wrapper with, so the text area keeps a constant
// size whether or not the bars are drawn.
func (h *EditorScrollbars) Prepare(c *ui.Context, st *codeed.State, findOpen bool) (right, bottom float32) {
	if c == nil || st == nil {
		return 0, 0
	}
	pal := theme.Ocean()
	now := c.Now()
	h.findOpen = findOpen

	if !h.initialized || h.lastState != st {
		h.initialized = true
		h.lastState = st
		h.lastScrollY = st.ScrollY()
		h.showUntil = time.Time{}
		h.visible = false
		h.hot = false
		h.overBar = false
		h.holdingBar = false
		h.sawHScroll = false
		h.cachedBufVer = 0
		h.cachedHasLong = false
	}

	canV, canH := h.scrollableAxes(st)
	curY := st.ScrollY()
	if math.Abs(curY-h.lastScrollY) > 0.1 {
		h.lastScrollY = curY
		if canV {
			h.showUntil = now.Add(ScrollHideDelay)
		}
	}
	if h.sawHScroll {
		h.sawHScroll = false
		if canH {
			h.showUntil = now.Add(ScrollHideDelay)
		}
	}
	if !canV && !canH {
		h.showUntil = time.Time{}
		h.overBar = false
		h.holdingBar = false
	}

	timedVisible := !h.showUntil.IsZero() && now.Before(h.showUntil)
	h.visible = (canV || canH) && (findOpen || timedVisible || h.overBar || h.holdingBar)
	h.hot = h.overBar || h.holdingBar

	if !h.visible {
		st.NoScrollbars = true
		st.KeepScrollbars = false
		st.Scrollbar = StandardScrollbarStyle(pal)
		return theme.ScrollGutter, 0
	}

	st.NoScrollbars = false
	st.KeepScrollbars = true
	if h.hot {
		st.Scrollbar = HotScrollbarStyle(pal)
		st.Scrollbar.Thumb = pal.ScrollThumbHover
	} else {
		st.Scrollbar = StandardScrollbarStyle(pal)
	}
	return 0, 0
}

// Observe runs right after codeed.View with the element it returned (and the
// padded wrapper, whose right/bottom inset is the hit area while the bars are
// hidden). It records scroll movement, hover over the hit area and drags, and
// schedules the repaint that fades the bars out.
func (h *EditorScrollbars) Observe(c *ui.Context, wrapper, ed ui.Element, st *codeed.State) {
	if c == nil || st == nil || !wrapper.Valid() {
		return
	}
	now := c.Now()
	if b := wrapper.Bounds(); b.W > 0 {
		h.lastWrapW = b.W
	}
	if ed.Valid() {
		ed.ObserveInput(func(ev ui.InputEvent) {
			if ev.Kind == ui.InputScroll && ev.DX != 0 && !st.SoftWrap {
				h.sawHScroll = true
			}
		})
	}
	win := ensureWindowScrollInput(c)
	primaryDown := win != nil && win.primaryDown
	if !primaryDown {
		h.holdingBar = false
	}

	canV, canH := h.scrollableAxes(st)
	curY := st.ScrollY()
	if math.Abs(curY-h.lastScrollY) > 0.1 {
		h.lastScrollY = curY
		if canV {
			h.showUntil = now.Add(ScrollHideDelay)
		}
	}
	overV, overH := pointerOverTrackEdges(wrapper, theme.ScrollGutter, canV, canH)
	overBar := overV || overH
	pressedIn := wrapper.PressedInside() || (ed.Valid() && ed.PressedInside())
	if overBar && pressedIn && primaryDown {
		h.holdingBar = true
	}
	if overBar || h.holdingBar {
		h.showUntil = now.Add(ScrollHideDelay)
	}

	nextTimed := !h.showUntil.IsZero() && now.Before(h.showUntil)
	nextVisible := (canV || canH) && (h.findOpen || nextTimed || overBar || h.holdingBar)
	nextHot := overBar || h.holdingBar

	changed := nextVisible != h.visible || nextHot != h.hot || h.sawHScroll
	h.overBar = overBar
	h.visible = nextVisible
	h.hot = nextHot

	if changed {
		c.Invalidate()
	}
	if nextVisible && !h.findOpen && !overBar && !h.holdingBar {
		scheduleScrollHideFrame(c, now, h.showUntil)
	}
}

func (h *EditorScrollbars) scrollableAxes(st *codeed.State) (canV, canH bool) {
	if st == nil || st.Buffer == nil {
		return false, false
	}
	viewH := st.ViewportHeight()
	if viewH > 0 {
		snap := st.Buffer.Snapshot()
		contentH := st.LineTop(snap.LineCount())
		canV = contentH > viewH+0.5
	}
	if !st.SoftWrap && h.lastWrapW > 0 {
		canH = h.editorHasHorizontalOverflow(st)
	}
	return canV, canH
}

func (h *EditorScrollbars) editorHasHorizontalOverflow(st *codeed.State) bool {
	snap := st.Buffer.Snapshot()
	ver := snap.Version()
	if ver == h.cachedBufVer && math.Abs(float64(h.lastWrapW-h.cachedWrapW)) <= 1 {
		return h.cachedHasLong
	}
	fontSize := st.Font.Size
	if fontSize <= 0 {
		fontSize = 13
	}
	charW := fontSize * 0.6
	availW := max(h.lastWrapW-theme.ScrollGutter-64, charW*8)
	maxCols := int(availW / charW)
	hasLong := false
	lines := snap.LineCount()
	for i := 0; i < lines; i++ {
		line := snap.Slice(snap.LineStart(i), snap.LineEnd(i))
		if len(line) > maxCols {
			cols := 0
			for _, r := range line {
				if r == '\t' {
					cols += 4
				} else if r >= 0x1100 {
					cols += 2
				} else {
					cols++
				}
				if cols > maxCols {
					hasLong = true
					break
				}
			}
			if hasLong {
				break
			}
		}
	}
	h.cachedBufVer = ver
	h.cachedWrapW = h.lastWrapW
	h.cachedHasLong = hasLong
	return hasLong
}
