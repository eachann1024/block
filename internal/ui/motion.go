package ui

import (
	"math"
	"testing"
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

	// SpringPanel drives narrow floating drawers, edge-float cards, cascades, and split rails.
	SpringPanel = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 360 * time.Millisecond,
			Bounce:   0.14,
		},
	}

	// SpringSurface drives the calm depth staging across Note, Settings, Page History,
	// and Export views; it settles without overshoot so whole surfaces never wobble.
	SpringSurface = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 320 * time.Millisecond,
			Bounce:   0,
		},
	}

	// SpringDock opens and folds docked side panels without overshoot, so a
	// collapsing width never grows past the panel nor shoves the main column back.
	SpringDock = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 340 * time.Millisecond,
			Bounce:   0,
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

	// SpringJumpScroll glides editor viewport jumps smoothly to the upper reading zone without bounce.
	SpringJumpScroll = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 280 * time.Millisecond,
			Bounce:   0,
		},
	}
)

// JumpScrollAnimation returns the scroll animation for in-document jumps,
// snapping immediately during tests or initial tab mount when smooth is false.
func JumpScrollAnimation(smooth bool) *ui.Animation {
	if !smooth || testing.Testing() {
		return nil
	}
	return &SpringJumpScroll
}

func almostZero(v, eps float32) bool {
	return float32(math.Abs(float64(v))) <= eps
}

// TactileButton applies Scheme B's signature hover lift + scale and press squish
// to compact square/chrome icon buttons. At rest it attaches no Transform.
func TactileButton(el ui.Element, hovered, pressed, disabled bool) ui.Element {
	if testing.Testing() {
		return el
	}
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
	if testing.Testing() {
		return el
	}
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
	if testing.Testing() {
		if bg.A > 0 {
			el.Background(bg)
		}
		if hasBorder {
			el.Border(1, border)
		}
		return el
	}
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
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	dx := float32(-8)
	if fromRight {
		dx = 8
	}
	return ui.ElementTransition{
		Animation:    &SpringDock,
		ExitDuration: 220 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Enter:        &ui.Motion{Collapse: true, X: dx, Opacity: 0.02},
		Exit:         &ui.Motion{Collapse: true, X: dx, Opacity: 0},
	}
}

// DockedSplitTransition keeps both the panel and its splitter opaque as they slide.
func DockedSplitTransition(fromRight bool) ui.ElementTransition {
	transition := DockedPanelTransition(fromRight)
	if transition.Enter != nil {
		transition.Enter.Opacity = 1
		transition.Exit.Opacity = 1
	}
	return transition
}

// PanelContentSwapTransition cross-fades what a persistent panel shows (the
// file tree and the settings navigation inside the docked sidebar slot)
// without moving or resizing the slot itself.
func PanelContentSwapTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Duration:     180 * time.Millisecond,
		Ease:         ui.CubicBezier(0.22, 1, 0.36, 1),
		ExitDuration: 140 * time.Millisecond,
		Colors:       true,
		Enter:        &ui.Motion{Opacity: 0.02},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// FloatingDrawerTransition animates the narrow-viewport floating sidebar drawer
// with Scheme B elastic slide + subtle scale settle.
func FloatingDrawerTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Animation:    &SpringPanel,
		ExitDuration: 210 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{XPercent: -100, Scale: 0.96, Opacity: 0.02},
		Exit:         &ui.Motion{XPercent: -100, Scale: 0.96, Opacity: 0},
	}
}

// EdgeFloatCardTransition animates Scheme A's rounded left/right edge-summoned
// floating Sidebar and AI cards with SpringPanel slide, subtle scale settle, and elevation fade.
func EdgeFloatCardTransition(fromRight bool) ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	dx := float32(-32)
	if fromRight {
		dx = 32
	}
	return ui.ElementTransition{
		Animation:    &SpringPanel,
		ExitDuration: 200 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{X: dx, Scale: 0.985, Opacity: 0.02},
		Exit:         &ui.Motion{X: dx, Scale: 0.99, Opacity: 0},
	}
}

// SpringPanelTo animates a layout scalar (such as handle-safe editor insets)
// smoothly on SpringPanel, snapping immediately under tests or within 0.25 DIP.
func SpringPanelTo(el ui.Element, key string, target float32) float32 {
	if testing.Testing() {
		return target
	}
	v := el.AnimateTo(key, target, SpringPanel)
	if almostZero(v-target, 0.25) {
		return target
	}
	return v
}

// ScrimFadeTransition fades modal and drawer backdrop scrims in and out cleanly.
func ScrimFadeTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Duration:     180 * time.Millisecond,
		ExitDuration: 160 * time.Millisecond,
		Ease:         ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Enter:        &ui.Motion{Opacity: 0.02},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// CascadeRowTransition returns Scheme B's staggered spring cascade (26ms step, capped)
// for folder-child reveals, drawer items, settings navigation, and search results.
func CascadeRowTransition(index int) ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
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
		Enter:        &ui.Motion{X: -12, Scale: 0.98, Opacity: 0.02, Collapse: true},
		Exit:         &ui.Motion{X: -8, Opacity: 0, Collapse: true},
	}
}

// TreeRevealTransition unfolds expanded tree rows top-down as one sequence:
// each row opens its height and fades in a step after the row above, on an
// overshoot-free curve so the group reads as a single sweep, not separate pops.
func TreeRevealTransition(order int) ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	clamped := min(max(order, 0), 16)
	anim := ui.Animation{
		Duration: 200 * time.Millisecond,
		Ease:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Delay:    time.Duration(clamped*22) * time.Millisecond,
	}
	return ui.ElementTransition{
		Animation:    &anim,
		ExitDuration: 140 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		ExitDelay:    -1,
		Position:     true,
		Colors:       true,
		Enter:        &ui.Motion{Y: -4, Opacity: 0.02, Collapse: true},
		Exit:         &ui.Motion{Opacity: 0, Collapse: true},
	}
}

// CascadeItemTransition is like CascadeRowTransition without layout-axis Collapse,
// ideal for cards, AI messages, and timeline entries inside scrollable columns.
func CascadeItemTransition(index int, fromRight bool) ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
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
		Enter:        &ui.Motion{X: dx, Y: 6, Scale: 0.97, Opacity: 0.02},
		Exit:         &ui.Motion{Opacity: 0, Scale: 0.97},
	}
}

// SurfaceStageTransition applies Scheme B depth staging (0.99 -> 1.0 + 6px rise)
// when switching primary views (Note, Settings, Page History, Export, Settings sub-tabs).
func SurfaceStageTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Animation:    &SpringSurface,
		ExitDuration: 170 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Enter:        &ui.Motion{Scale: 0.99, Y: 6, Opacity: 0.02},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// ModalSpringTransition animates centered/top-anchored modal dialogs (Search Cmd+K,
// Performance, Feedback, About, Updater) with Scheme B's tactile spring pop.
func ModalSpringTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Animation:    &SpringModal,
		ExitDuration: 160 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{Scale: 0.91, Y: -12, Opacity: 0.02},
		Exit:         &ui.Motion{Scale: 0.95, Y: -8, Opacity: 0},
	}
}

// PopoverSpringTransition animates anchored menus, Model Deck, Font Picker, and toolbars
// with Flip-aware spring entrance and crisp dismissal.
func PopoverSpringTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Animation:    &SpringModal,
		ExitDuration: 140 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{Scale: 0.93, Y: -6, Opacity: 0.02, Flip: true},
		Exit:         &ui.Motion{Scale: 0.96, Y: -4, Opacity: 0, Flip: true},
	}
}

// GutterHandleTransition shows per-row editor affordances (block handles, the
// trailing add button) without overshoot, entering no slower than it leaves:
// a pointer that leaves and returns mid-animation resumes from the leaving copy
// instead of replaying the whole entrance.
func GutterHandleTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	ease := ui.CubicBezier(0.22, 1, 0.36, 1)
	return ui.ElementTransition{
		Duration:     170 * time.Millisecond,
		Ease:         ease,
		ExitDuration: 170 * time.Millisecond,
		ExitEase:     ease,
		Colors:       true,
		Enter:        &ui.Motion{Scale: 0.92, Opacity: 0.02},
		Exit:         &ui.Motion{Scale: 0.92, Opacity: 0},
	}
}

// SpringReveal opens and folds inline segments without overshoot, so a width
// driven by it never grows past its content nor pushes its neighbours back.
var SpringReveal = ui.Animation{
	Spring: ui.SpringOptions{
		Duration: 300 * time.Millisecond,
		Bounce:   0,
	},
}

// RevealTo animates a 0–1 reveal progress toward target with SpringReveal,
// jumping straight to it under tests.
func RevealTo(el ui.Element, key string, target float32) float32 {
	if testing.Testing() {
		return target
	}
	return el.AnimateTo(key, target, SpringReveal)
}

// SpringTo animates a scalar toward target with the tactile spring, jumping
// straight to it under tests.
func SpringTo(el ui.Element, key string, target float32) float32 {
	if testing.Testing() {
		return target
	}
	return el.AnimateTo(key, target, SpringTactile)
}

// ResizeHandleTransition springs an image resize handle in after the selection
// border has appeared: it grows out of the border from `inward` DIPs (positive
// moves right) with a lively overshoot, and leaves with a quick fade.
func ResizeHandleTransition(inward float32) ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	anim := ui.Animation{
		Spring: ui.SpringOptions{Duration: 420 * time.Millisecond, Bounce: 0.34},
		Delay:  130 * time.Millisecond,
	}
	return ui.ElementTransition{
		// Keep the rail's position and height tied to the image during resize.
		Colors:       true,
		Animation:    &anim,
		ExitDuration: 120 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		ExitDelay:    -1,
		Enter:        &ui.Motion{X: inward, Scale: 0.2, Opacity: 0.02},
		Exit:         &ui.Motion{Scale: 0.6, Opacity: 0},
	}
}
