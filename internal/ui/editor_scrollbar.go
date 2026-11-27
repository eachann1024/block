package ui

import (
	"math"
	"time"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
)

// Scrollbar spec "A+D": a 2px square, flush line inset by ScrollEdgeInset
// (1px) from the edge at rest; ScrollThumbHoverWidth (7px) at the same
// ScrollEdgeInset while the hot hit area is hovered or dragged. Identical on
// macOS / Windows / Linux.

// StandardScrollbarStyle is the resting style every window theme installs
// (uiTheme.ScrollbarStyle). Thumb / Hover / Active come from pal.
func StandardScrollbarStyle(pal theme.Palette) ui.ScrollbarStyle {
	return ui.ScrollbarStyle{
		TrackWidth: theme.ScrollThumbWidth + 2*theme.ScrollEdgeInset,
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
// theme.ScrollThumbHoverWidth while keeping the same ScrollEdgeInset.
func HotScrollbarStyle(pal theme.Palette) ui.ScrollbarStyle {
	s := StandardScrollbarStyle(pal)
	s.TrackWidth = theme.ScrollThumbHoverWidth + 2*theme.ScrollEdgeInset
	s.ThumbWidth = theme.ScrollThumbHoverWidth
	return s
}

// EditorScrollbars gives a codeed.View wrapper the same scroll-only vertical
// scrollbar as AutoHideScrollbars while keeping codeed's NoScrollbars=true so
// line wrap width stays constant (right inset is always theme.ScrollGutter).
// The thumb is painted in wrapper's right gutter via DrawOver at
// theme.ScrollEdgeInset from wrapper's right edge.
type EditorScrollbars struct {
	initialized bool
	lastState   *codeed.State
	lastScrollY float64
	showUntil   time.Time
	visible     bool
	hot         bool
	overBar     bool
	holdingBar  bool
	dragGrab    float32
	findOpen    bool
	lastWrapH   float64
	lastScale   float32
}

// Prepare runs before codeed.View each frame. It disables codeed's built-in
// scrollbars so text wrap width never shifts and returns the constant wrapper
// padding (theme.ScrollGutter, 0) where EditorScrollbars paints the thumb.
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
		h.dragGrab = 0
	}

	_, _, canV := h.verticalMetrics(st, h.lastWrapH)
	curY := st.ScrollY()
	if math.Abs(curY-h.lastScrollY) > 0.1 {
		h.lastScrollY = curY
		if canV {
			h.showUntil = now.Add(ScrollHideDelay)
		}
	}
	if !canV {
		h.showUntil = time.Time{}
		h.overBar = false
		h.holdingBar = false
	}

	timedVisible := !h.showUntil.IsZero() && now.Before(h.showUntil)
	h.visible = canV && (findOpen || timedVisible || h.overBar || h.holdingBar)
	h.hot = canV && (h.overBar || h.holdingBar)

	st.NoScrollbars = true
	st.KeepScrollbars = false
	st.Scrollbar = StandardScrollbarStyle(pal)
	return theme.ScrollGutter, 0
}

// Observe runs right after codeed.View with the padded wrapper whose right
// theme.ScrollGutter strip hosts the custom-drawn vertical scrollbar. It handles
// edge hover, thumb drag, track jump, gutter wheel forwarding, 750ms auto-hide,
// and painting via DrawOver.
func (h *EditorScrollbars) Observe(c *ui.Context, wrapper, _ ui.Element, st *codeed.State) {
	if c == nil || st == nil || !wrapper.Valid() {
		return
	}
	now := c.Now()
	b := wrapper.Bounds()
	if b.H > 0 {
		h.lastWrapH = float64(b.H)
	}
	wrapper.KeepFocus()

	win := ensureWindowScrollInput(c)
	primaryDown := win != nil && win.primaryDown
	if !primaryDown {
		h.holdingBar = false
	}

	pal := theme.Ocean()
	restStyle := StandardScrollbarStyle(pal)
	hotStyle := HotScrollbarStyle(pal)
	hotTrackW := hotStyle.TrackWidth

	_, _, canV := h.verticalMetrics(st, float64(b.H))
	curY := st.ScrollY()
	if math.Abs(curY-h.lastScrollY) > 0.1 {
		h.lastScrollY = curY
		if canV {
			h.showUntil = now.Add(ScrollHideDelay)
		}
	}

	overV, _ := pointerOverTrackEdges(wrapper, hotTrackW, canV, false)

	wrapper.HandleInput(func(ev ui.InputEvent) bool {
		wb := wrapper.Bounds()
		vH, cH, scrollable := h.verticalMetrics(st, float64(wb.H))
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button != 0 || !scrollable || wb.W <= 0 || wb.H <= 0 || ev.X < wb.W-hotTrackW {
				return false
			}
			bar := verticalEditorScrollbar(ui.Rect{W: wb.W, H: wb.H}, vH, cH, st.ScrollY(), hotStyle)
			if !bar.Scrolls() {
				return false
			}
			t0, n := bar.Thumb()
			h.holdingBar = true
			h.showUntil = ev.Time.Add(ScrollHideDelay)
			if ev.Y >= t0 && ev.Y <= t0+n {
				h.dragGrab = ev.Y - t0
			} else {
				h.dragGrab = n / 2
				st.SetScrollY(float64(bar.OffsetAt(ev.Y - h.dragGrab)))
				h.lastScrollY = st.ScrollY()
			}
			return true

		case ui.InputPointerMove:
			if !h.holdingBar {
				return false
			}
			bar := verticalEditorScrollbar(ui.Rect{W: wb.W, H: wb.H}, vH, cH, st.ScrollY(), hotStyle)
			if bar.Scrolls() {
				st.SetScrollY(float64(bar.OffsetAt(ev.Y - h.dragGrab)))
				h.lastScrollY = st.ScrollY()
			}
			h.showUntil = ev.Time.Add(ScrollHideDelay)
			return true

		case ui.InputPointerUp:
			if !h.holdingBar || (ev.Button != 0 && !ev.Canceled) {
				return false
			}
			h.holdingBar = false
			h.showUntil = ev.Time.Add(ScrollHideDelay)
			return true

		case ui.InputScroll:
			if !scrollable || ev.DY == 0 || wb.W <= 0 || ev.X < wb.W-theme.ScrollGutter {
				return false
			}
			before := st.ScrollY()
			st.SetScrollY(before + float64(ev.DY))
			if st.ScrollY() == before {
				return false
			}
			h.lastScrollY = st.ScrollY()
			h.showUntil = ev.Time.Add(ScrollHideDelay)
			return true
		}
		return false
	})

	if overV || h.holdingBar {
		h.showUntil = now.Add(ScrollHideDelay)
	}

	nextTimed := !h.showUntil.IsZero() && now.Before(h.showUntil)
	nextVisible := canV && (h.findOpen || nextTimed || overV || h.holdingBar)
	nextHot := canV && (overV || h.holdingBar)

	changed := nextVisible != h.visible || nextHot != h.hot
	h.overBar = overV
	h.visible = nextVisible
	h.hot = nextHot

	wrapper.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if sc := p.Scale(); sc > 0 {
			h.lastScale = sc
		}
		if !h.visible {
			return
		}
		vH, cH, scrollable := h.verticalMetrics(st, float64(r.H))
		if !scrollable {
			return
		}
		style := restStyle
		if h.hot {
			style = hotStyle
		}
		bar := verticalEditorScrollbar(r, vH, cH, st.ScrollY(), style)
		p.ScrollbarThumb(bar, ui.ScrollbarState{Hovered: h.hot})
	})

	if changed {
		c.Invalidate()
	}
	if nextVisible && !h.findOpen && !overV && !h.holdingBar {
		scheduleScrollHideFrame(c, now, h.showUntil)
	}
}

func (h *EditorScrollbars) verticalMetrics(st *codeed.State, fallbackH float64) (viewH, contentH float64, canV bool) {
	if st == nil || st.Buffer == nil {
		return 0, 0, false
	}
	viewH = st.ViewportHeight()
	if viewH <= 0 {
		viewH = fallbackH
	}
	if viewH <= 0 {
		return 0, 0, false
	}
	snap := st.Buffer.Snapshot()
	total := st.LineTop(snap.LineCount())
	if !st.NoScrollPastEnd && !st.FitContent {
		fontSize := st.Font.Size
		if fontSize <= 0 {
			fontSize = 13
		}
		lhMult := st.LineHeight
		if lhMult <= 0 {
			lhMult = 1.5
		}
		scale := h.lastScale
		if scale <= 0 {
			scale = 1
		}
		lineH := math.Round(float64(fontSize*lhMult*scale)) / float64(scale)
		scrollMax := max(0, total-lineH)
		contentH = scrollMax + viewH
	} else {
		contentH = max(total, viewH)
	}
	return viewH, contentH, contentH > viewH+0.5
}

func verticalEditorScrollbar(box ui.Rect, viewH, contentH, scrollY float64, style ui.ScrollbarStyle) ui.Scrollbar {
	return ui.Scrollbar{
		Box:     box,
		View:    float32(viewH),
		Content: float32(contentH),
		Offset:  float32(scrollY),
		Style:   style,
	}
}
