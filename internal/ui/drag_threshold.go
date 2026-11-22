package ui

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// DefaultDragThreshold is the pointer travel distance in DIPs required before
// an item activates value drag, preventing accidental drags from click jitter.
const DefaultDragThreshold float32 = 12

// DragGate delays calling el.Drag(value) until the pointer pressing el has
// moved at least threshold DIPs. Because mygo hardcodes a 4 DIP dragStart,
// deferring Drag until the larger threshold is crossed lets small hand jitter
// resolve as a normal click without starting a drag or edge-scrolling.
type DragGate struct {
	key   string
	dx    float32
	dy    float32
	armed bool
}

// Reset clears any in-flight drag threshold tracking.
func (g *DragGate) Reset() {
	if g == nil {
		return
	}
	g.key = ""
	g.dx = 0
	g.dy = 0
	g.armed = false
}

// Attach tracks primary-button drag distance on el and calls el.Drag(value)
// once the pointer has moved at least threshold DIPs (or while el is already
// dragging). Returns whether drag is currently armed or active for this item.
func (g *DragGate) Attach(el ui.Element, key string, value any, threshold float32) bool {
	if g == nil {
		el.Drag(value)
		return true
	}
	if threshold <= 0 {
		threshold = DefaultDragThreshold
	}
	dx, dy, pressing := el.Dragged()
	if pressing {
		if g.key != key {
			g.key = key
			g.dx = 0
			g.dy = 0
			g.armed = false
		}
		g.dx += dx
		g.dy += dy
		if !g.armed && float32(math.Hypot(float64(g.dx), float64(g.dy))) >= threshold {
			g.armed = true
		}
		if g.armed || el.Dragging() {
			el.Drag(value)
			return true
		}
		return false
	}
	if g.key == key {
		if el.Dragging() {
			el.Drag(value)
			return true
		}
		g.Reset()
	}
	return false
}
