package app

import (
	"runtime"
	"sync"
	"time"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// mainGlass tracks the material last applied to the main window.
var mainGlass struct {
	sync.Mutex
	win     *mygo.Window
	applied mygo.Vibrancy
}

// MainWindowVibrancy returns the material the main window should show for st.
func MainWindowVibrancy(st model.AppState) mygo.Vibrancy {
	if !theme.WindowGlassSupported() || !st.WindowGlassOn() {
		return mygo.VibrancyNone
	}
	if runtime.GOOS == "darwin" {
		// Liquid Glass on macOS 26+ (the popover material before it): the desktop
		// reads through vividly, as in Tahoe's own sidebars.
		return mygo.VibrancyGlass
	}
	return mygo.VibrancyMica
}

// InitWindowGlass records the material the main window was created with.
func InitWindowGlass(win *mygo.Window, created mygo.Vibrancy) {
	mainGlass.Lock()
	mainGlass.win, mainGlass.applied = win, created
	mainGlass.Unlock()
	theme.SetWindowGlassAtLaunch(created != mygo.VibrancyNone)
}

// syncWindowGlass applies the glass setting to the main window and reports whether
// the shell ground should let the material read through this frame.
func (w *Workspace) syncWindowGlass(c *ui.Context, st model.AppState) bool {
	want := MainWindowVibrancy(st)
	if win := w.Window(); win != nil {
		mainGlass.Lock()
		changed := mainGlass.win == win && mainGlass.applied != want
		if changed {
			mainGlass.applied = want
		}
		mainGlass.Unlock()
		if changed {
			go func() {
				win.SetVibrancy(want)
				time.AfterFunc(60*time.Millisecond, w.RequestRedraw)
			}()
		}
	}
	return want != mygo.VibrancyNone && c.Vibrancy() && !c.Preferences().ReduceTransparency
}
