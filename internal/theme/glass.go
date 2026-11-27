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
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
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
		return ground.Alpha(0.72)
	}
	if p.Dark {
		return ground.Alpha(0.55)
	}
	return ground.Alpha(0.45)
}
