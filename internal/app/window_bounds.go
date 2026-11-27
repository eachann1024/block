package app

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"time"

	"block/internal/model"
	"github.com/egoist/mygo"
)

const (
	MainZoomMinPct     = 70
	MainZoomMaxPct     = 130
	MainZoomStepPct    = 10
	MainZoomDefaultPct = 100

	MainWindowDefaultW = 1100
	MainWindowDefaultH = 760

	mainBoundsDebounce = 250 * time.Millisecond
)

// ResolvedMainWindow carries the startup placement and state for the main window.
type ResolvedMainWindow struct {
	Rect        mygo.Rectangle
	HasPosition bool
	Maximized   bool
	Fullscreen  bool
}

func snapMainZoomPct(pct int) int {
	if pct <= 0 {
		return MainZoomDefaultPct
	}
	clamped := max(MainZoomMinPct, min(pct, MainZoomMaxPct))
	snapped := ((clamped + 5) / 10) * 10
	return max(MainZoomMinPct, min(snapped, MainZoomMaxPct))
}

func zoomPctFromState(val *float32) int {
	if val == nil {
		return MainZoomDefaultPct
	}
	v := float64(*val)
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return MainZoomDefaultPct
	}
	var pct int
	if v > 10 {
		pct = int(math.Round(v))
	} else {
		pct = int(math.Round(v * 100))
	}
	return snapMainZoomPct(pct)
}

func isFiniteBoundsFloat(vals ...float32) bool {
	for _, v := range vals {
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	return true
}

func mainDisplayWorkArea(d mygo.Display) mygo.Rectangle {
	if d.WorkArea.Width > 0 && d.WorkArea.Height > 0 {
		return d.WorkArea
	}
	return d.Bounds
}

func rectOverlapArea(a, b mygo.Rectangle) int64 {
	x1 := max(a.X, b.X)
	y1 := max(a.Y, b.Y)
	x2 := min(a.X+a.Width, b.X+b.Width)
	y2 := min(a.Y+a.Height, b.Y+b.Height)
	if x2 <= x1 || y2 <= y1 {
		return 0
	}
	return int64(x2-x1) * int64(y2-y1)
}

func findDisplayForMainBounds(saved model.SavedWindowBounds, displays []mygo.Display) (target mygo.Display, matched bool, ok bool) {
	if len(displays) == 0 {
		return mygo.Display{}, false, false
	}
	if saved.DisplayID != nil {
		wantID := *saved.DisplayID
		for _, d := range displays {
			if uint32(d.ID) == wantID {
				return d, true, true
			}
		}
	}
	r := mygo.Rectangle{
		X:      int(math.Round(float64(saved.X))),
		Y:      int(math.Round(float64(saved.Y))),
		Width:  int(math.Round(float64(saved.Width))),
		Height: int(math.Round(float64(saved.Height))),
	}
	var bestArea int64
	var best mygo.Display
	for _, d := range displays {
		area := rectOverlapArea(mainDisplayWorkArea(d), r)
		if area > bestArea {
			bestArea = area
			best = d
		}
	}
	if bestArea > 0 {
		return best, true, true
	}
	for _, d := range displays {
		if d.Primary {
			return d, false, true
		}
	}
	return displays[0], false, true
}

// ResolveMainWindowBoundsFromDisplays resolves persisted main window bounds against
// connected displays, preserving multi-monitor positions and centering gracefully if
// an external display was disconnected.
func ResolveMainWindowBoundsFromDisplays(saved *model.SavedWindowBounds, displays []mygo.Display) ResolvedMainWindow {
	if saved == nil || !isFiniteBoundsFloat(saved.X, saved.Y, saved.Width, saved.Height) || saved.Width <= 0 || saved.Height <= 0 {
		return ResolvedMainWindow{
			Rect: mygo.Rectangle{Width: MainWindowDefaultW, Height: MainWindowDefaultH},
		}
	}

	width := max(saved.Width, WindowMinW)
	height := max(saved.Height, WindowMinH)
	x := saved.X
	y := saved.Y
	hasPos := true

	if d, matched, ok := findDisplayForMainBounds(*saved, displays); ok {
		area := mainDisplayWorkArea(d)
		dx, dy := float32(area.X), float32(area.Y)
		dw, dh := float32(area.Width), float32(area.Height)
		if dw >= WindowMinW {
			width = min(width, dw)
		}
		if dh >= WindowMinH {
			height = min(height, dh)
		}
		if matched {
			minX := dx - width + 120
			maxX := max(dx+dw-120, minX)
			minY := dy
			maxY := max(dy+dh-60, minY)
			x = min(max(x, minX), maxX)
			y = min(max(y, minY), maxY)
		} else if dw > 0 && dh > 0 {
			x = dx + max((dw-width)/2, 0)
			y = dy + max((dh-height)/2, 0)
		} else {
			hasPos = false
		}
	}

	return ResolvedMainWindow{
		Rect: mygo.Rectangle{
			X:      int(math.Round(float64(x))),
			Y:      int(math.Round(float64(y))),
			Width:  int(math.Round(float64(width))),
			Height: int(math.Round(float64(height))),
		},
		HasPosition: hasPos,
		Maximized:   saved.Maximized,
		Fullscreen:  saved.Fullscreen,
	}
}

// IsMainRectVisibleOnDisplays reports whether r remains reachable on at least one display.
func IsMainRectVisibleOnDisplays(r mygo.Rectangle, displays []mygo.Display) bool {
	if len(displays) == 0 {
		return true
	}
	for _, d := range displays {
		a := mainDisplayWorkArea(d)
		iw := min(a.X+a.Width, r.X+r.Width) - max(a.X, r.X)
		ih := min(a.Y+a.Height, r.Y+r.Height) - max(a.Y, r.Y)
		if iw >= min(120, r.Width) && ih >= min(60, r.Height) {
			return true
		}
	}
	return false
}

func loadLegacyMyGoMainWindowBounds(dataDir string) *model.SavedWindowBounds {
	if dataDir == "" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "window-state.json"))
	if err != nil {
		return nil
	}
	var byKey map[string]struct {
		X          int  `json:"x"`
		Y          int  `json:"y"`
		Width      int  `json:"width"`
		Height     int  `json:"height"`
		Maximized  bool `json:"maximized"`
		FullScreen bool `json:"fullScreen"`
	}
	if json.Unmarshal(raw, &byKey) != nil {
		return nil
	}
	m, ok := byKey["main"]
	if !ok || m.Width <= 0 || m.Height <= 0 {
		return nil
	}
	return &model.SavedWindowBounds{
		X:          float32(m.X),
		Y:          float32(m.Y),
		Width:      float32(m.Width),
		Height:     float32(m.Height),
		Maximized:  m.Maximized,
		Fullscreen: m.FullScreen,
	}
}

func savedWindowBoundsEqual(a, b *model.SavedWindowBounds) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Maximized != b.Maximized || a.Fullscreen != b.Fullscreen {
		return false
	}
	if math.Abs(float64(a.X-b.X)) > 0.5 ||
		math.Abs(float64(a.Y-b.Y)) > 0.5 ||
		math.Abs(float64(a.Width-b.Width)) > 0.5 ||
		math.Abs(float64(a.Height-b.Height)) > 0.5 {
		return false
	}
	switch {
	case a.DisplayID == nil && b.DisplayID == nil:
		return true
	case a.DisplayID != nil && b.DisplayID != nil:
		return *a.DisplayID == *b.DisplayID
	default:
		return false
	}
}

// SetPersistWindowBounds enables or disables writing main window bounds to state.json.
func (w *Workspace) SetPersistWindowBounds(enabled bool) {
	w.mu.Lock()
	w.persistWindowBounds = enabled
	if !enabled && w.mainBoundsTimer != nil {
		w.mainBoundsTimer.Stop()
		w.mainBoundsTimer = nil
	}
	w.mu.Unlock()
}

// SavedMainWindowBounds returns a copy of the persisted main window bounds, if any.
func (w *Workspace) SavedMainWindowBounds() *model.SavedWindowBounds {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.state.MainWindowBounds == nil {
		return nil
	}
	cp := *w.state.MainWindowBounds
	if w.state.MainWindowBounds.DisplayID != nil {
		did := *w.state.MainWindowBounds.DisplayID
		cp.DisplayID = &did
	}
	return &cp
}

// ResolveMainWindowOptions returns the resolved main window placement for the current screen configuration.
func (w *Workspace) ResolveMainWindowOptions() ResolvedMainWindow {
	w.mu.RLock()
	persist := w.persistWindowBounds
	w.mu.RUnlock()
	if !persist {
		return ResolvedMainWindow{
			Rect: mygo.Rectangle{Width: MainWindowDefaultW, Height: MainWindowDefaultH},
		}
	}
	saved := w.SavedMainWindowBounds()
	var displays []mygo.Display
	if mygo.App.IsReady() {
		displays = mygo.Screen.Displays()
	}
	return ResolveMainWindowBoundsFromDisplays(saved, displays)
}

func (w *Workspace) captureMainWindowBounds(win *mygo.Window) (model.SavedWindowBounds, bool) {
	if win == nil || win.IsDestroyed() {
		return model.SavedWindowBounds{}, false
	}
	prev := w.SavedMainWindowBounds()
	isMin := win.IsMinimized()
	isMax := win.IsMaximized()
	isFull := win.IsFullScreen()

	if isMin {
		if prev == nil {
			return model.SavedWindowBounds{}, false
		}
		return *prev, true
	}

	if isMax || isFull {
		var b model.SavedWindowBounds
		if prev != nil && prev.Width >= WindowMinW && prev.Height >= WindowMinH {
			b = *prev
		} else {
			r := win.Bounds()
			bw := max(float32(r.Width), MainWindowDefaultW)
			bh := max(float32(r.Height), MainWindowDefaultH)
			b = model.SavedWindowBounds{
				X:      float32(r.X),
				Y:      float32(r.Y),
				Width:  bw,
				Height: bh,
			}
		}
		b.Maximized = isMax
		b.Fullscreen = isFull
		return b, true
	}

	r := win.Bounds()
	if float32(r.Width) < WindowMinW*0.5 || float32(r.Height) < WindowMinH*0.5 {
		return model.SavedWindowBounds{}, false
	}
	b := model.SavedWindowBounds{
		X:          float32(r.X),
		Y:          float32(r.Y),
		Width:      max(float32(r.Width), WindowMinW),
		Height:     max(float32(r.Height), WindowMinH),
		Maximized:  false,
		Fullscreen: false,
	}
	if mygo.App.IsReady() {
		d := mygo.Screen.DisplayMatching(r)
		if d.ID != 0 || d.Bounds.Width > 0 {
			did := uint32(d.ID)
			b.DisplayID = &did
		}
	}
	return b, true
}

func (w *Workspace) persistMainBoundsNow(win *mygo.Window) {
	w.mu.RLock()
	persist := w.persistWindowBounds
	w.mu.RUnlock()
	if !persist || win == nil {
		return
	}
	b, ok := w.captureMainWindowBounds(win)
	if !ok {
		return
	}
	prev := w.SavedMainWindowBounds()
	if savedWindowBoundsEqual(prev, &b) {
		return
	}
	cp := b
	if b.DisplayID != nil {
		did := *b.DisplayID
		cp.DisplayID = &did
	}
	w.mu.Lock()
	w.state.MainWindowBounds = &cp
	w.mu.Unlock()
	saved := cp
	w.updateStateAsync(func(st *model.AppState) {
		c := saved
		if saved.DisplayID != nil {
			did := *saved.DisplayID
			c.DisplayID = &did
		}
		st.MainWindowBounds = &c
	})
}

func (w *Workspace) scheduleSaveMainBounds() {
	w.mu.Lock()
	if !w.persistWindowBounds || w.win == nil {
		w.mu.Unlock()
		return
	}
	if w.mainBoundsTimer != nil {
		w.mainBoundsTimer.Reset(mainBoundsDebounce)
		w.mu.Unlock()
		return
	}
	w.mainBoundsTimer = time.AfterFunc(mainBoundsDebounce, func() {
		mygo.RunOnMain(func() {
			w.mu.Lock()
			w.mainBoundsTimer = nil
			win := w.win
			persist := w.persistWindowBounds
			w.mu.Unlock()
			if persist && win != nil && !win.IsDestroyed() && win.IsVisible() {
				w.persistMainBoundsNow(win)
			}
		})
	})
	w.mu.Unlock()
}

// FlushWindowBounds cancels any pending debounce timer and immediately saves main window bounds.
func (w *Workspace) FlushWindowBounds() {
	w.mu.Lock()
	if w.mainBoundsTimer != nil {
		w.mainBoundsTimer.Stop()
		w.mainBoundsTimer = nil
	}
	win := w.win
	persist := w.persistWindowBounds
	w.mu.Unlock()
	if persist && win != nil {
		w.persistMainBoundsNow(win)
	}
}
