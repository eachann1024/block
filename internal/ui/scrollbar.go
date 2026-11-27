package ui

import (
	"math"
	"time"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// ScrollHideDelay is how long an overlay scrollbar stays visible after scroll
// movement stops, matching macOS overlay scrollers and ScrollbarMode::Scrolling.
const ScrollHideDelay = 750 * time.Millisecond

// HoverScrollSuppressDelay is how long transient row hover chrome stays quiet
// after scroll movement stops, matching the editor's hover suppression window.
const HoverScrollSuppressDelay = 140 * time.Millisecond

type windowScrollInputKey struct{}

type windowScrollInput struct {
	primaryDown bool
}

func (w *windowScrollInput) observe(ev ui.InputEvent) {
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button == 0 {
			w.primaryDown = true
		}
	case ui.InputPointerUp:
		if ev.Button == 0 || ev.Canceled {
			w.primaryDown = false
		}
	}
}

func ensureWindowScrollInput(c *ui.Context) *windowScrollInput {
	root := c.Root()
	if !root.Valid() {
		return nil
	}
	win := ui.Local(root, windowScrollInputKey{}, func() windowScrollInput {
		return windowScrollInput{}
	})
	if !c.WindowActive() {
		win.primaryDown = false
	}
	root.ObserveInput(win.observe)
	return win
}

type scrollHideKey struct{}

type scrollAxes uint8

const (
	axisVertical scrollAxes = 1 << iota
	axisHorizontal
)

type scrollHideState struct {
	fallback         ui.ScrollState
	tracked          *ui.ScrollState
	lastX            float32
	lastY            float32
	showUntil       time.Time
	hoverQuietUntil time.Time
	initialized     bool
	visible         bool
	holdingBar      bool
	sawScrollInput  bool
}

// Scroll creates a vertical scroll container whose scrollbar stays hidden when
// idle and appears only while scrolling.
func Scroll(c *ui.Context, track ...*ui.ScrollState) ui.Element {
	return attachScrollHide(c, ui.Scroll(c), axisVertical, track...)
}

// ScrollHorizontal creates a horizontal scroll container whose scrollbar stays
// hidden when idle and appears only while scrolling.
func ScrollHorizontal(c *ui.Context, track ...*ui.ScrollState) ui.Element {
	return attachScrollHide(c, ui.ScrollHorizontal(c), axisHorizontal, track...)
}

// ScrollBoth creates a 2D scroll container whose scrollbars stay hidden when
// idle and appear only while scrolling.
func ScrollBoth(c *ui.Context, track ...*ui.ScrollState) ui.Element {
	return attachScrollHide(c, ui.ScrollBoth(c), axisVertical|axisHorizontal, track...)
}

// AutoHideScrollbars attaches scroll-only scrollbar visibility to an existing
// scrollable widget (such as ui.List or ui.TextArea).
func AutoHideScrollbars(c *ui.Context, el ui.Element, track ...*ui.ScrollState) ui.Element {
	return attachScrollHide(c, el, axisVertical, track...)
}

// IsScrollingForHover reports whether a scroll container attached via Scroll or
// AutoHideScrollbars is actively scrolling or within the short quiet window
// afterward, so rows can suppress transient hover flicker while scrolling.
func IsScrollingForHover(c *ui.Context, el ui.Element) bool {
	if c == nil || !el.Valid() {
		return false
	}
	local := ui.Local(el, scrollHideKey{}, func() scrollHideState {
		return scrollHideState{}
	})
	if local.holdingBar || local.sawScrollInput {
		return true
	}
	return !local.hoverQuietUntil.IsZero() && c.Now().Before(local.hoverQuietUntil)
}

func attachScrollHide(c *ui.Context, el ui.Element, axes scrollAxes, track ...*ui.ScrollState) ui.Element {
	if !el.Valid() {
		return el
	}
	local := ui.Local(el, scrollHideKey{}, func() scrollHideState {
		return scrollHideState{}
	})
	st := &local.fallback
	if len(track) > 0 && track[0] != nil {
		st = track[0]
	}
	el.TrackScroll(st)
	el.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind != ui.InputScroll {
			return
		}
		if axes&axisVertical != 0 && ev.DY != 0 && st.MaxY > 0.5 {
			if (ev.DY > 0 && st.Y < st.MaxY-0.5) || (ev.DY < 0 && st.Y > 0.5) {
				local.sawScrollInput = true
			}
		}
		if axes&axisHorizontal != 0 && ev.DX != 0 && st.MaxX > 0.5 {
			if (ev.DX > 0 && st.X < st.MaxX-0.5) || (ev.DX < 0 && st.X > 0.5) {
				local.sawScrollInput = true
			}
		}
	})

	now := c.Now()
	win := ensureWindowScrollInput(c)
	primaryDown := win != nil && win.primaryDown

	canV := axes&axisVertical != 0 && st.MaxY > 0.5
	canH := axes&axisHorizontal != 0 && st.MaxX > 0.5
	scrollable := canV || canH
	overBar := false

	if !local.initialized || local.tracked != st {
		local.tracked = st
		local.lastX = st.X
		local.lastY = st.Y
		local.initialized = true
		local.showUntil = time.Time{}
		local.hoverQuietUntil = time.Time{}
		local.holdingBar = false
		local.sawScrollInput = false
		local.visible = false
	} else if !scrollable {
		local.lastX = st.X
		local.lastY = st.Y
		local.showUntil = time.Time{}
		local.hoverQuietUntil = time.Time{}
		local.holdingBar = false
		local.sawScrollInput = false
		local.visible = false
	} else {
		dx := math.Abs(float64(st.X - local.lastX))
		dy := math.Abs(float64(st.Y - local.lastY))
		if dx > 0.1 || dy > 0.1 {
			local.lastX = st.X
			local.lastY = st.Y
			local.showUntil = now.Add(ScrollHideDelay)
			if dx > 0.5 || dy > 0.5 {
				local.hoverQuietUntil = now.Add(HoverScrollSuppressDelay)
			}
		}
		if local.sawScrollInput {
			local.sawScrollInput = false
			local.hoverQuietUntil = now.Add(HoverScrollSuppressDelay)
		}

		if !primaryDown {
			local.holdingBar = false
		}

		overV, overH := pointerOverTrackEdges(el, resolveTrackWidth(c), canV, canH)
		overBar = overV || overH
		if overBar && el.PressedInside() && primaryDown {
			local.holdingBar = true
		}
		if overBar || local.holdingBar {
			local.showUntil = now.Add(ScrollHideDelay)
		}

		local.visible = !local.showUntil.IsZero() && now.Before(local.showUntil)
	}

	if !local.hoverQuietUntil.IsZero() {
		if rem := local.hoverQuietUntil.Sub(now); rem > 0 {
			c.After(rem + 5*time.Millisecond)
		} else {
			local.hoverQuietUntil = time.Time{}
		}
	}

	if local.visible {
		el.Scrollbars(true)
		el.KeepScrollbars(!el.Hovered())
		if overBar || local.holdingBar {
			el.ScrollbarStyle(resolveHotScrollbarStyle(c))
		}
		scheduleScrollHideFrame(c, now, local.showUntil)
	} else {
		el.Scrollbars(false)
		el.KeepScrollbars(false)
	}
	return el
}

func resolveTrackWidth(c *ui.Context) float32 {
	// Hover wake and drag hit-testing share the hot TrackWidth so the widened
	// thumb can be grabbed immediately anywhere inside the wake zone.
	return resolveHotScrollbarStyle(c).TrackWidth
}

func resolveHotScrollbarStyle(c *ui.Context) ui.ScrollbarStyle {
	hot := HotScrollbarStyle(theme.Ocean())
	if th := c.Theme(); th != nil {
		if th.ScrollbarStyle.Thumb != (ui.Color{}) {
			hot.Thumb = th.ScrollbarStyle.Thumb
		}
		if th.ScrollbarStyle.Hover != (ui.Color{}) {
			hot.Hover = th.ScrollbarStyle.Hover
		}
		if th.ScrollbarStyle.Active != (ui.Color{}) {
			hot.Active = th.ScrollbarStyle.Active
		}
	}
	return hot
}

func scheduleScrollHideFrame(c *ui.Context, now, until time.Time) {
	if rem := until.Sub(now); rem > 0 {
		c.After(rem + 5*time.Millisecond)
	} else {
		c.After(5 * time.Millisecond)
	}
}

func pointerOverScrollbarTrack(c *ui.Context, el ui.Element, axes scrollAxes) bool {
	overV, overH := pointerOverTrackEdges(el, resolveTrackWidth(c), axes&axisVertical != 0, axes&axisHorizontal != 0)
	return overV || overH
}

func pointerOverTrackEdges(el ui.Element, trackW float32, canV, canH bool) (overV, overH bool) {
	if !el.Valid() || (!canV && !canH) || trackW <= 0 {
		return false, false
	}
	px, py, over := el.PointerPosition()
	if !over {
		return false, false
	}
	b := el.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return false, false
	}
	overV = canV && px >= b.W-trackW
	overH = canH && py >= b.H-trackW
	return overV, overH
}
