package ui

import (
	"sync"
	"sync/atomic"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// Smoke overlay & target bounds probe registry (matching thinkly-fast/src/ui/smoke_overlay.rs).
var (
	smokeActive atomic.Bool
	smokeMu     sync.RWMutex
	boundsMap   = make(map[string]ui.Rect)
	pulseSeq    uint64
	pulsePoint  *ui.Point
	stepText    string
	stepPassed  *bool
)

func EnableSmokeMode() {
	smokeActive.Store(true)
}

func DisableSmokeMode() {
	smokeActive.Store(false)
}

func IsSmokeActive() bool {
	return smokeActive.Load()
}

func RecordBounds(id string, r ui.Rect) {
	if id == "" {
		return
	}
	smokeMu.Lock()
	boundsMap[id] = r
	smokeMu.Unlock()
}

func TargetBounds(id string) (ui.Rect, bool) {
	smokeMu.RLock()
	r, ok := boundsMap[id]
	smokeMu.RUnlock()
	return r, ok && r.W > 0 && r.H > 0
}

func TargetCenter(id string) (ui.Point, bool) {
	r, ok := TargetBounds(id)
	if !ok {
		return ui.Point{}, false
	}
	return ui.Point{X: r.X + r.W*0.5, Y: r.Y + r.H*0.5}, true
}

func AllTargets() map[string]ui.Rect {
	smokeMu.RLock()
	defer smokeMu.RUnlock()
	cp := make(map[string]ui.Rect, len(boundsMap))
	for k, v := range boundsMap {
		cp[k] = v
	}
	return cp
}

func ClearTargets() {
	smokeMu.Lock()
	clear(boundsMap)
	smokeMu.Unlock()
}

func ResetSmoke() {
	smokeMu.Lock()
	clear(boundsMap)
	pulseSeq = 0
	pulsePoint = nil
	stepText = ""
	stepPassed = nil
	smokeMu.Unlock()
}

func SetSmokeStep(pt *ui.Point, text string) {
	smokeMu.Lock()
	pulseSeq++
	if pt != nil {
		cp := *pt
		pulsePoint = &cp
	} else {
		pulsePoint = nil
	}
	stepText = text
	smokeMu.Unlock()
}

func SetSmokeFinished(passed bool, summary string) {
	smokeMu.Lock()
	p := passed
	stepPassed = &p
	pulsePoint = nil
	stepText = summary
	smokeMu.Unlock()
}

// Probe attaches a layout-bounds recorder to `el` under `id` so both in-process
// smoke tests and external automation runners can query exact live bounds.
func Probe(el ui.Element, id string) ui.Element {
	if !el.Valid() || id == "" {
		return el
	}
	el.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		RecordBounds(id, r)
	})
	return el
}

// RenderSmokeHUD draws the non-interactive click ripple and bottom step HUD pill
// when smoke mode is active.
func RenderSmokeHUD(c *ui.Context) {
	if !IsSmokeActive() {
		return
	}
	smokeMu.RLock()
	pt := pulsePoint
	txt := stepText
	passed := stepPassed
	smokeMu.RUnlock()

	if pt == nil && txt == "" {
		return
	}
	pal := theme.Ocean()
	ui.Overlay(c, func() {
		layer := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).IgnoreInput(true).AccessHidden(true)
		layer.Children(func() {
			if pt != nil {
				r := float32(22)
				ui.Box(c).Absolute().Left(pt.X-r).Top(pt.Y-r).Size(r*2, r*2).
					Radius(theme.RadiusPill).Border(2, pal.Accent).Background(pal.Accent.Alpha(0.16)).IgnoreInput(true)
				ui.Box(c).Absolute().Left(pt.X-4).Top(pt.Y-4).Size(8, 8).
					Radius(theme.RadiusPill).Background(pal.Accent).IgnoreInput(true)
			}
			if txt != "" {
				dotColor := pal.Accent
				if passed != nil {
					if *passed {
						dotColor = pal.Success
					} else {
						dotColor = pal.Danger
					}
				}
				ui.Row(c).Absolute().Bottom(20).Left(0).Right(0).Justify(ui.Center).IgnoreInput(true).Children(func() {
					ui.Row(c).Height(36).PaddingX(14).Gap(8).AlignItems(ui.Center).
						Radius(theme.RadiusControl).Background(pal.Paper).Border(1, dotColor).
						Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.14)).IgnoreInput(true).
						Children(func() {
							ui.Box(c).Size(8, 8).Shrink(0).Radius(theme.RadiusPill).Background(dotColor)
							ui.Text(c, txt).FontSize(14).FontWeight(500).TextColor(pal.Text).SingleLine()
						})
				})
			}
		})
	})
}
