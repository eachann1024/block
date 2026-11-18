package ui

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Scheme B ("灵动弹簧 · Tactile Spring") unified physics & transition presets.
// Built on mygo/ui's analytical damped harmonic oscillator so mid-flight
// interruptions preserve velocity and OS ReduceMotion is respected automatically.
var (
	// SpringTactile drives interactive controls, icon sub-layers, switches, and checkboxes.
	SpringTactile = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 320 * time.Millisecond,
			Bounce:   0.18,
		},
	}

	// SpringPanel drives docked sidebars, narrow floating drawers, AI panels, and split rails.
	SpringPanel = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 360 * time.Millisecond,
			Bounce:   0.14,
		},
	}

	// SpringSurface drives depth-scale staging across Note, Settings, Page History, and Export views.
	SpringSurface = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 340 * time.Millisecond,
			Bounce:   0.12,
		},
	}

	// SpringModal drives dialogs, Search (Cmd+K), popovers, and floating menus.
	SpringModal = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 320 * time.Millisecond,
			Bounce:   0.20,
		},
	}

	// FadeColor interpolates surface, border, and icon tints in Oklab without spatial motion.
	FadeColor = ui.Animation{
		Duration:  170 * time.Millisecond,
		Ease:      ui.CubicBezier(0.22, 1, 0.36, 1),
		Oklab:     true,
		NonMotion: true,
	}
)

func almostZero(v, eps float32) bool {
	return float32(math.Abs(float64(v))) <= eps
}

// TactileButton applies Scheme B's signature hover lift + scale and press squish
// to compact square/chrome icon buttons. At rest it attaches no Transform.
func TactileButton(el ui.Element, hovered, pressed, disabled bool) ui.Element {
	targetScale := float32(1.0)
	targetLiftY := float32(0.0)
	if !disabled {
		switch {
		case pressed:
			targetScale = 0.92
			targetLiftY = 0.5
		case hovered:
			targetScale = 1.06
			targetLiftY = -1.2
		}
	}
	sc := el.AnimateTo("bScale", targetScale, SpringTactile)
	dy := el.AnimateTo("bLift", targetLiftY, SpringTactile)
	if !almostZero(sc-1, 0.001) || !almostZero(dy, 0.02) {
		el.Translate(0, dy).Scale(sc, sc)
	}
	return el
}

// TactileCard applies subtle tactile spring scale/lift suited to wider buttons,
// segmented pills, provider cards, and list rows without overflowing narrow clips.
func TactileCard(el ui.Element, hovered, pressed, active, disabled bool) ui.Element {
	targetScale := float32(1.0)
	targetLiftY := float32(0.0)
	if !disabled {
		switch {
		case pressed:
			targetScale = 0.97
			targetLiftY = 0.5
		case active && hovered:
			targetScale = 1.022
			targetLiftY = -1.0
		case active:
			targetScale = 1.014
		case hovered:
			targetScale = 1.015
			targetLiftY = -0.8
		}
	}
	sc := el.AnimateTo("cScale", targetScale, SpringTactile)
	dy := el.AnimateTo("cLift", targetLiftY, SpringTactile)
	if !almostZero(sc-1, 0.001) || !almostZero(dy, 0.02) {
		el.Translate(0, dy).Scale(sc, sc)
	}
	return el
}

// AnimateSurfaceColors smoothly transitions background and 1px border colors in Oklab.
func AnimateSurfaceColors(el ui.Element, bg, border ui.Color, hasBorder bool) ui.Element {
	abg := el.AnimateColor("sBg", bg, FadeColor)
	if abg.A > 0 {
		el.Background(abg)
	}
	if hasBorder {
		abd := el.AnimateColor("sBd", border, FadeColor)
		el.Border(1, abd)
	}
	return el
}

// DockedPanelTransition animates docked left/right sidebars (File Tree, Settings Nav,
// AI Assistant, Export Options, Outline Rail) springing open and collapsing shut
// while leaving steady-state drag resizing lag-free.
func DockedPanelTransition(fromRight bool) ui.ElementTransition {
	dx := float32(-14)
	if fromRight {
		dx = 14
	}
	return ui.ElementTransition{
		Animation:    &SpringPanel,
		ExitDuration: 220 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Enter:        &ui.Motion{Collapse: true, X: dx, Opacity: 0},
		Exit:         &ui.Motion{Collapse: true, X: dx, Opacity: 0},
	}
}

// FloatingDrawerTransition animates the narrow-viewport floating sidebar drawer
// with Scheme B elastic slide + subtle scale settle.
func FloatingDrawerTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Animation:    &SpringPanel,
		ExitDuration: 210 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{X: -228, Scale: 0.96, Opacity: 0},
		Exit:         &ui.Motion{X: -228, Scale: 0.96, Opacity: 0},
	}
}

// ScrimFadeTransition fades modal and drawer backdrop scrims in and out cleanly.
func ScrimFadeTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration:     180 * time.Millisecond,
		ExitDuration: 160 * time.Millisecond,
		Ease:         ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Enter:        &ui.Motion{Opacity: 0},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// CascadeRowTransition returns Scheme B's staggered spring cascade (26ms step, capped)
// for folder-child reveals, drawer items, settings navigation, and search results.
func CascadeRowTransition(index int) ui.ElementTransition {
	clamped := min(max(index, 0), 8)
	anim := ui.Animation{
		Spring: SpringPanel.Spring,
		Delay:  time.Duration(clamped*26) * time.Millisecond,
	}
	return ui.ElementTransition{
		Animation:    &anim,
		ExitDuration: 150 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		ExitDelay:    -1,
		Position:     true,
		Colors:       true,
		Enter:        &ui.Motion{X: -12, Scale: 0.98, Opacity: 0, Collapse: true},
		Exit:         &ui.Motion{X: -8, Opacity: 0, Collapse: true},
	}
}

// CascadeItemTransition is like CascadeRowTransition without layout-axis Collapse,
// ideal for cards, AI messages, and timeline entries inside scrollable columns.
func CascadeItemTransition(index int, fromRight bool) ui.ElementTransition {
	clamped := min(max(index, 0), 6)
	dx := float32(-12)
	if fromRight {
		dx = 14
	}
	anim := ui.Animation{
		Spring: SpringPanel.Spring,
		Delay:  time.Duration(clamped*28) * time.Millisecond,
	}
	return ui.ElementTransition{
		Animation:    &anim,
		ExitDuration: 150 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		ExitDelay:    -1,
		Position:     true,
		Colors:       true,
		Enter:        &ui.Motion{X: dx, Y: 6, Scale: 0.97, Opacity: 0},
		Exit:         &ui.Motion{Opacity: 0, Scale: 0.97},
	}
}

// SurfaceStageTransition applies Scheme B depth-scale staging (0.965 -> 1.0 + 10px rise)
// when switching primary views (Note, Settings, Page History, Export, Settings sub-tabs).
func SurfaceStageTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Animation:    &SpringSurface,
		ExitDuration: 170 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Enter:        &ui.Motion{Scale: 0.968, Y: 10, Opacity: 0},
		Exit:         &ui.Motion{Scale: 0.985, Opacity: 0},
	}
}

// ModalSpringTransition animates centered/top-anchored modal dialogs (Search Cmd+K,
// Performance, Feedback, About, Updater) with Scheme B's tactile spring pop.
func ModalSpringTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Animation:    &SpringModal,
		ExitDuration: 160 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{Scale: 0.91, Y: -12, Opacity: 0},
		Exit:         &ui.Motion{Scale: 0.95, Y: -8, Opacity: 0},
	}
}

// PopoverSpringTransition animates anchored menus, Model Deck, Font Picker, and toolbars
// with Flip-aware spring entrance and crisp dismissal.
func PopoverSpringTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Animation:    &SpringModal,
		ExitDuration: 140 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{Scale: 0.93, Y: -6, Opacity: 0, Flip: true},
		Exit:         &ui.Motion{Scale: 0.96, Y: -4, Opacity: 0, Flip: true},
	}
}
