package ui

import (
	"math"

	"github.com/egoist/mygo/ui"
)

const (
	DefaultMargin   float32 = 8
	DefaultGap      float32 = 4
	MinScrollHeight float32 = 80
)

type AlignXKind uint8

const (
	AlignLeft AlignXKind = iota
	AlignPinLeft
	AlignRight
	AlignCenter
	AlignCascadeRight
)

type AlignX struct {
	Kind       AlignXKind
	CascadeGap float32
}

func CascadeRight(gap float32) AlignX {
	return AlignX{Kind: AlignCascadeRight, CascadeGap: gap}
}

// PlacementSpec mirrors thinkly-fast/src/ui/placement.rs 1:1 for intelligent
// viewport-aware floating menu, toolbar, link card, and cascading submenu placement.
type PlacementSpec struct {
	Trigger       ui.Rect
	Viewport      *ui.Rect
	Gap           float32
	Margin        float32
	AlignX        AlignX
	ReserveHeight float32
	PreferAbove   bool
}

func PlaceBelow(trigger ui.Rect) PlacementSpec {
	return PlacementSpec{
		Trigger: trigger,
		Gap:     DefaultGap,
		Margin:  DefaultMargin,
		AlignX:  AlignX{Kind: AlignLeft},
	}
}

func PlaceAbove(trigger ui.Rect) PlacementSpec {
	s := PlaceBelow(trigger)
	s.PreferAbove = true
	return s
}

func PlaceAtPoint(x, y float32) PlacementSpec {
	s := PlaceBelow(ui.Rect{X: x, Y: y, W: 0, H: 0})
	s.Gap = 2
	return s
}

func (s PlacementSpec) WithViewport(vp ui.Rect) PlacementSpec {
	s.Viewport = &vp
	return s
}

func (s PlacementSpec) WithGap(gap float32) PlacementSpec {
	s.Gap = gap
	return s
}

func (s PlacementSpec) WithMargin(margin float32) PlacementSpec {
	s.Margin = margin
	return s
}

func (s PlacementSpec) WithAlignX(a AlignX) PlacementSpec {
	s.AlignX = a
	return s
}

func (s PlacementSpec) WithReserveHeight(h float32) PlacementSpec {
	s.ReserveHeight = h
	return s
}

func intersectRect(a, b ui.Rect) ui.Rect {
	x1 := max(a.X, b.X)
	y1 := max(a.Y, b.Y)
	x2 := min(a.X+a.W, b.X+b.W)
	y2 := min(a.Y+a.H, b.Y+b.H)
	if x2 < x1 {
		x2 = x1
	}
	if y2 < y1 {
		y2 = y1
	}
	return ui.Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}

func safeLimits(spec PlacementSpec, winW, winH float32) (minX, maxX, minY, maxY float32) {
	area := ui.Rect{X: 0, Y: 0, W: winW, H: winH}
	if spec.Viewport != nil && spec.Viewport.W > 0 && spec.Viewport.H > 0 {
		area = intersectRect(*spec.Viewport, area)
	}
	minX = area.X + spec.Margin
	maxX = max(area.X+area.W-spec.Margin, minX)
	minY = area.Y + spec.Margin
	maxY = max(area.Y+area.H-spec.Margin, minY)
	return
}

func verticalSpace(spec PlacementSpec, minY, maxY float32) (above, below float32) {
	above = spec.Trigger.Y - spec.Gap - minY
	below = maxY - (spec.Trigger.Y + spec.Trigger.H + spec.Gap)
	return
}

func placeAbove(spec PlacementSpec, spaceAbove, spaceBelow, needH float32) bool {
	if spec.PreferAbove {
		return spaceAbove >= needH || spaceAbove >= spaceBelow
	}
	return spaceBelow < needH && spaceAbove > spaceBelow
}

func fitAxis(raw, size, minV, maxV float32, pinEnd bool) float32 {
	room := max(maxV-minV, 0)
	if size >= room {
		if pinEnd {
			return maxV - size
		}
		return minV
	}
	return max(min(raw, maxV-size), minV)
}

func AvailableHeightForSpec(spec PlacementSpec, winW, winH, capH float32) float32 {
	_, _, minY, maxY := safeLimits(spec, winW, winH)
	spaceAbove, spaceBelow := verticalSpace(spec, minY, maxY)
	room := max(maxY-minY, 0)
	needH := capH
	if spec.ReserveHeight > 0 && spec.ReserveHeight < needH {
		needH = spec.ReserveHeight
	}
	needH = min(needH, room)
	avail := spaceBelow
	if placeAbove(spec, spaceAbove, spaceBelow, needH) {
		avail = spaceAbove
	}
	return min(max(avail, 0), capH)
}

func MaxHeightForSpec(spec PlacementSpec, winW, winH, capH float32) float32 {
	return max(AvailableHeightForSpec(spec, winW, winH, capH), MinScrollHeight)
}

// ResolveOrigin computes the exact rounded top-left window coordinate (x, y)
// for a floating surface of size (menuW, menuH) inside (winW, winH).
func ResolveOrigin(spec PlacementSpec, menuW, menuH, winW, winH float32) (float32, float32) {
	minX, maxX, minY, maxY := safeLimits(spec, winW, winH)
	pointAnchor := spec.Trigger.W <= 0 && spec.Trigger.H <= 0
	left := spec.Trigger.X
	right := spec.Trigger.X + spec.Trigger.W - menuW
	fits := func(x float32) bool { return x >= minX && x+menuW <= maxX }

	var rawX float32
	switch spec.AlignX.Kind {
	case AlignLeft:
		if !pointAnchor && !fits(left) && fits(right) {
			rawX = right
		} else {
			rawX = left
		}
	case AlignPinLeft:
		rawX = left
	case AlignRight:
		if !pointAnchor && !fits(right) && fits(left) {
			rawX = left
		} else {
			rawX = right
		}
	case AlignCenter:
		rawX = spec.Trigger.X + spec.Trigger.W*0.5 - menuW*0.5
	case AlignCascadeRight:
		gap := spec.AlignX.CascadeGap
		rightX := spec.Trigger.X + spec.Trigger.W + gap
		if rightX+menuW <= maxX {
			rawX = rightX
		} else {
			leftX := spec.Trigger.X - gap - menuW
			if leftX >= minX {
				rawX = leftX
			} else {
				rawX = rightX
			}
		}
	}

	if spec.AlignX.Kind == AlignCascadeRight || pointAnchor {
		x := fitAxis(rawX, menuW, minX, maxX, false)
		var y float32
		if pointAnchor {
			y = fitAxis(spec.Trigger.Y+spec.Trigger.H+spec.Gap, menuH, minY, maxY, false)
		} else {
			y = fitAxis(spec.Trigger.Y, menuH, minY, maxY, false)
		}
		return float32(math.Round(float64(x))), float32(math.Round(float64(y)))
	}

	endAnchor := spec.AlignX.Kind == AlignRight && rawX == right
	x := fitAxis(rawX, menuW, minX, maxX, endAnchor)

	spaceAbove, spaceBelow := verticalSpace(spec, minY, maxY)
	roomY := max(maxY-minY, 0)
	needH := menuH
	if spec.ReserveHeight > needH {
		needH = spec.ReserveHeight
	}
	needH = min(needH, roomY)
	var idealY float32
	if placeAbove(spec, spaceAbove, spaceBelow, needH) {
		idealY = spec.Trigger.Y - spec.Gap - menuH
	} else {
		idealY = spec.Trigger.Y + spec.Trigger.H + spec.Gap
	}
	y := fitAxis(idealY, menuH, minY, maxY, false)
	return float32(math.Round(float64(x))), float32(math.Round(float64(y)))
}
