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
	showUntil        time.Time
	frameAt          time.Time
	framePrevVisible bool
	initialized      bool
	visible          bool
	holdingBar       bool
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

	now := c.Now()
	win := ensureWindowScrollInput(c)
	primaryDown := win != nil && win.primaryDown

	if !local.frameAt.Equal(now) {
		local.framePrevVisible = local.visible
		local.frameAt = now
	}

	if !local.initialized || local.tracked != st {
		local.tracked = st
		local.lastX = st.X
		local.lastY = st.Y
		local.initialized = true
		local.showUntil = time.Time{}
		local.holdingBar = false
		local.visible = false
	} else {
		dx := math.Abs(float64(st.X - local.lastX))
		dy := math.Abs(float64(st.Y - local.lastY))
		if dx > 0.1 || dy > 0.1 {
			local.lastX = st.X
			local.lastY = st.Y
			local.showUntil = now.Add(ScrollHideDelay)
		}

		if !primaryDown {
			local.holdingBar = false
		}

		if local.framePrevVisible {
			overBar := pointerOverScrollbarTrack(c, el, axes)
			if overBar && el.PressedInside() && primaryDown {
				local.holdingBar = true
			}
			if overBar || local.holdingBar {
				local.showUntil = now.Add(ScrollHideDelay)
			}
		}

		local.visible = !local.showUntil.IsZero() && now.Before(local.showUntil)
	}

	if local.visible {
		el.Scrollbars(true)
		el.KeepScrollbars(!el.Hovered())
		if rem := local.showUntil.Sub(now); rem > 0 {
			c.After(rem + 5*time.Millisecond)
		} else {
			c.After(5 * time.Millisecond)
		}
	} else {
		el.Scrollbars(false)
		el.KeepScrollbars(false)
	}
	return el
}

func pointerOverScrollbarTrack(c *ui.Context, el ui.Element, axes scrollAxes) bool {
	px, py, over := el.PointerPosition()
	if !over {
		return false
	}
	b := el.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return false
	}
	trackW := theme.ScrollGutter
	if th := c.Theme(); th != nil && th.ScrollbarStyle.TrackWidth > 0 {
		trackW = th.ScrollbarStyle.TrackWidth
	}
	if axes&axisVertical != 0 && px >= b.W-trackW {
		return true
	}
	if axes&axisHorizontal != 0 && py >= b.H-trackW {
		return true
	}
	return false
}
