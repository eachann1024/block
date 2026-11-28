package theme

import (
	"runtime"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// Window glass: the main window shows the system material (macOS vibrancy,
// Windows 11 Mica) through the shell ground (sidebar + titlebar), while the
// floating paper card stays opaque for reading.

var glassAtLaunch atomic.Bool

// WindowGlassSupported reports whether the OS draws a window material at all.
func WindowGlassSupported() bool {
	// Window translucency is paused for now; keep the implementation for later.
	return false && (runtime.GOOS == "darwin" || runtime.GOOS == "windows")
}

// WindowGlassActive reports whether window translucency is enabled in user
// preferences, backed by a live OS material, and not suppressed by Reduce Transparency.
func WindowGlassActive(c *ui.Context, prefOn bool) bool {
	return WindowGlassSupported() && prefOn && c != nil && c.Vibrancy() && !c.Preferences().ReduceTransparency
}

// SetWindowGlassAtLaunch records whether the main window was created with a material.
func SetWindowGlassAtLaunch(v bool) { glassAtLaunch.Store(v) }

// WindowGlassNeedsRestart reports whether turning glass on only takes effect after a
// restart: Windows shows a material only in a window created with one.
func WindowGlassNeedsRestart(on bool) bool {
	return runtime.GOOS == "windows" && on && !glassAtLaunch.Load()
}

// GlassGround tints the shell ground over the window material: stock themes stay
// light so the material reads through, skins keep more of their own color.
func (p Palette) GlassGround(ground ui.Color) ui.Color {
	if skinActive() {
		return ground.Alpha(0.45)
	}
	if p.Dark {
		return ground.Alpha(0.28)
	}
	return ground.Alpha(0.16)
}

var glassMaskSolid = ui.RGB(0, 0, 0)

func glassMaskStopsFromPx(pts [][2]float32, span float32) []ui.GradientStop {
	if span <= 0 {
		span = 1
	}
	stops := make([]ui.GradientStop, 0, len(pts)+1)
	prevX, prevA := float32(0), pts[0][1]
	for i, pt := range pts {
		x, a := pt[0], pt[1]
		if i > 0 && x <= prevX {
			continue
		}
		if i > 0 && x >= span {
			frac := float32(1)
			if x > prevX {
				frac = (span - prevX) / (x - prevX)
			}
			endA := prevA + (a-prevA)*frac
			stops = append(stops, ui.GradientStop{Color: glassMaskSolid.Alpha(endA), At: 1})
			return stops
		}
		stops = append(stops, ui.GradientStop{Color: glassMaskSolid.Alpha(a), At: x / span})
		prevX, prevA = x, a
	}
	if len(stops) > 0 && stops[len(stops)-1].At < 1 {
		stops = append(stops, ui.GradientStop{Color: glassMaskSolid.Alpha(prevA), At: 1})
	}
	return stops
}

// BrandGlassLayerOpacity returns the sky-wash, artwork-group, and titlebar-spill
// opacities used when compositing the top brand scene band over window glass.
func BrandGlassLayerOpacity(dark, skinChrome bool) (skyScale, artOpacity, spillScale float32) {
	if skinChrome {
		return 0.16, 0.56, 0.72
	}
	if dark {
		return 0.20, 0.60, 0.80
	}
	return 0.16, 0.60, 0.74
}

// BrandGlassVerticalStops returns the vertical alpha-mask stops that dissolve the
// 88px top brand band seamlessly into the frosted shell below without a bottom step.
func BrandGlassVerticalStops() []ui.GradientStop {
	return []ui.GradientStop{
		{Color: glassMaskSolid, At: 0},
		{Color: glassMaskSolid.Alpha(0.95), At: 0.16},
		{Color: glassMaskSolid.Alpha(0.76), At: 0.34},
		{Color: glassMaskSolid.Alpha(0.46), At: 0.52},
		{Color: glassMaskSolid.Alpha(0.20), At: 0.70},
		{Color: glassMaskSolid.Alpha(0.05), At: 0.86},
		{Color: ui.Transparent, At: 1},
	}
}

// BrandGlassHorizontalSkyStops returns horizontal alpha-mask stops that blend the
// brand sky wash across the sidebar edge into the translucent titlebar.
func BrandGlassHorizontalSkyStops(artW, bandW float32) []ui.GradientStop {
	if artW <= 0 {
		return glassMaskStopsFromPx([][2]float32{
			{0, 1},
			{60, 0.90},
			{180, 0.38},
			{320, 0},
		}, bandW)
	}
	return glassMaskStopsFromPx([][2]float32{
		{0, 1},
		{max(artW-60, 0), 0.95},
		{max(artW-10, 0), 0.78},
		{artW + 70, 0.44},
		{artW + 190, 0.14},
		{artW + 320, 0},
	}, bandW)
}

// BrandGlassHorizontalArtStops returns horizontal alpha-mask stops that feather the
// sidebar landscape smoothly past artW into the titlebar without a hard vertical cut.
func BrandGlassHorizontalArtStops(artW, boxW float32) []ui.GradientStop {
	return glassMaskStopsFromPx([][2]float32{
		{0, 1},
		{max(artW-56, 0), 1},
		{max(artW-30, 0), 0.72},
		{max(artW-8, 0), 0.36},
		{artW + 10, 0.10},
		{boxW, 0},
	}, boxW)
}

// GlassScrollEdgeMask returns a vertical edge-fade mask for scrollable sidebar lists
// over translucent glass when content overflows past the top or bottom edge.
func GlassScrollEdgeMask(topFade, botFade bool, height float32) (ui.LinearGradient, bool) {
	if (!topFade && !botFade) || height <= 24 {
		return ui.LinearGradient{}, false
	}
	edge := min(float32(14), height*0.25) / height
	stops := make([]ui.GradientStop, 0, 4)
	if topFade {
		stops = append(stops,
			ui.GradientStop{Color: ui.Transparent, At: 0},
			ui.GradientStop{Color: glassMaskSolid, At: edge},
		)
	} else {
		stops = append(stops, ui.GradientStop{Color: glassMaskSolid, At: 0})
	}
	if botFade {
		stops = append(stops,
			ui.GradientStop{Color: glassMaskSolid, At: 1 - edge},
			ui.GradientStop{Color: ui.Transparent, At: 1},
		)
	} else {
		stops = append(stops, ui.GradientStop{Color: glassMaskSolid, At: 1})
	}
	return ui.LinearGradient{Angle: 180, Stops: stops}, true
}
