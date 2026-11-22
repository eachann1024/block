package ui

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Scheme A uses short ease-out transitions with immediate state colors.
var (
	motionEase     = ui.CubicBezier(0.2, 0.8, 0.25, 1)
	treeDrawerEase = ui.CubicBezier(0.22, 1, 0.36, 1)

	// Shared scalar presets for controls and panel layout.
	SpringTactile = ui.Animation{Duration: 100 * time.Millisecond, Ease: motionEase}
	SpringPanel   = ui.Animation{Duration: 150 * time.Millisecond, Ease: motionEase}
	jumpScroll    = ui.Animation{Duration: 160 * time.Millisecond, Ease: motionEase}

	// Sidebar tree damped-drawer presets (Scheme A: smooth open, crisp close).
	TreeDrawerOpen  = ui.Animation{Duration: 220 * time.Millisecond, Ease: treeDrawerEase}
	TreeDrawerClose = ui.Animation{Duration: 150 * time.Millisecond, Ease: treeDrawerEase}
	TreeChevronAnim = ui.Animation{Duration: 180 * time.Millisecond, Ease: treeDrawerEase}

	// FadeColor is used where the moving indicator itself also changes tint.
	FadeColor = ui.Animation{
		Duration:  80 * time.Millisecond,
		Ease:      motionEase,
		Oklab:     true,
		NonMotion: true,
	}
)

// TreeDrawerProgress animates a sidebar folder drawer between 0 (collapsed) and
// 1 (expanded), snapping immediately during tests.
func TreeDrawerProgress(el ui.Element, key string, expanded bool) (float32, bool) {
	target := float32(0)
	anim := TreeDrawerClose
	if expanded {
		target = 1
		anim = TreeDrawerOpen
	}
	if testing.Testing() {
		return target, true
	}
	p := el.AnimateTo(key, target, anim)
	settled := el.Settled(key)
	if p < 0 {
		p = 0
	} else if p > 1 {
		p = 1
	}
	if settled || almostZero(p-target, 0.001) {
		return target, settled
	}
	return p, false
}

// JumpScrollAnimation returns the scroll animation for in-document jumps,
// snapping immediately during tests or initial tab mount when smooth is false.
func JumpScrollAnimation(smooth bool) *ui.Animation {
	if !smooth || testing.Testing() {
		return nil
	}
	return &jumpScroll
}

func almostZero(v, eps float32) bool {
	return float32(math.Abs(float64(v))) <= eps
}

// TactileButton and TactileCard give controls a small press response.
func TactileButton(el ui.Element, _ bool, pressed, disabled bool) ui.Element {
	return pressScale(el, pressed && !disabled, 0.98)
}

func TactileCard(el ui.Element, _ bool, pressed, _, disabled bool) ui.Element {
	return pressScale(el, pressed && !disabled, 0.99)
}

// TactileIcon scales an icon down on press and springs back on release.
func TactileIcon(el ui.Element, pressed, disabled bool) ui.Element {
	return pressScale(el, pressed && !disabled, 0.88)
}

func pressScale(el ui.Element, pressed bool, pressedScale float32) ui.Element {
	if testing.Testing() {
		return el
	}
	target := float32(1)
	if pressed {
		target = pressedScale
	}
	scale := el.AnimateTo("pressScale", target, SpringTactile)
	if !almostZero(scale-1, 0.001) {
		el.Scale(scale, scale)
	}
	return el
}

// AnimateSurfaceColors applies the current interaction state during the same frame.
func AnimateSurfaceColors(el ui.Element, bg, border ui.Color, hasBorder bool) ui.Element {
	if bg.A > 0 {
		el.Background(bg)
	}
	if hasBorder {
		el.Border(1, border)
	}
	return el
}

// DockedPanelTransition animates docked side rails inside a surface (Export Options,
// Outline Rail) opening and collapsing shut while leaving drag resizing lag-free.
// The shell's file tree and AI panel use DockPanel, which moves the layout itself.
func DockedPanelTransition(fromRight bool) ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	dx := float32(-8)
	if fromRight {
		dx = 8
	}
	return ui.ElementTransition{
		Animation:    &SpringPanel,
		ExitDuration: 110 * time.Millisecond,
		ExitEase:     motionEase,
		Colors:       true,
		Enter:        &ui.Motion{Collapse: true, X: dx, Opacity: 0.02},
		Exit:         &ui.Motion{Collapse: true, X: dx, Opacity: 0},
	}
}

// PanelContentSwapTransition cross-fades what a persistent panel shows (the
// file tree and the settings navigation inside the docked sidebar slot)
// without moving or resizing the slot itself.
func PanelContentSwapTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Duration:     120 * time.Millisecond,
		Ease:         motionEase,
		ExitDuration: 90 * time.Millisecond,
		Colors:       true,
		Enter:        &ui.Motion{Opacity: 0.02},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// FloatingDrawerTransition animates the narrow-viewport floating sidebar drawer
// with a brief slide from the leading edge.
func FloatingDrawerTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Animation:    &SpringPanel,
		ExitDuration: 110 * time.Millisecond,
		ExitEase:     motionEase,
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{X: -12, Opacity: 0.02},
		Exit:         &ui.Motion{X: -12, Opacity: 0},
	}
}

// EdgeFloatCardTransition slides the floating Sidebar and AI cards from their edge.
func EdgeFloatCardTransition(fromRight bool) ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	dx := float32(-8)
	if fromRight {
		dx = 8
	}
	return ui.ElementTransition{
		Animation:    &SpringPanel,
		ExitDuration: 110 * time.Millisecond,
		ExitEase:     motionEase,
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{X: dx, Opacity: 0.02},
		Exit:         &ui.Motion{X: dx, Opacity: 0},
	}
}

// SettingsDrawerGenieTransition pulls the narrow Settings floating navigation
// card toward the top-left sidebar toggle on exit (designs/settings-narrow-float
// Scheme A), pairing with TransformOrigin(0.10, 0) so the user's eye follows
// the collapse straight to the expand trigger.
func SettingsDrawerGenieTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Duration:     180 * time.Millisecond,
		Ease:         ui.CubicBezier(0.22, 1, 0.36, 1),
		ExitDuration: 240 * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.32, 0, 0.2, 1),
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{X: -12, Y: -10, Scale: 0.92, Opacity: 0.02},
		Exit:         &ui.Motion{X: -18, Y: -22, Scale: 0.16, Opacity: 0},
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
		Duration:     120 * time.Millisecond,
		ExitDuration: 100 * time.Millisecond,
		Ease:         motionEase,
		Colors:       true,
		Enter:        &ui.Motion{Opacity: 0.02},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// CascadeRowTransition reveals a row in parallel with its siblings.
func CascadeRowTransition(_ int) ui.ElementTransition {
	return rowRevealTransition(true)
}

// TreeRevealTransition reveals expanded tree rows together.
func TreeRevealTransition(_ int) ui.ElementTransition {
	return rowRevealTransition(true)
}

// CascadeItemTransition reveals an item without collapsing its layout slot.
func CascadeItemTransition(_ int, _ bool) ui.ElementTransition {
	return rowRevealTransition(false)
}

func rowRevealTransition(collapse bool) ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Duration:     100 * time.Millisecond,
		Ease:         motionEase,
		ExitDuration: 70 * time.Millisecond,
		Position:     true,
		Enter:        &ui.Motion{Opacity: 0.15, Collapse: collapse},
		Exit:         &ui.Motion{Opacity: 0, Collapse: collapse},
	}
}

// SurfaceStageTransition uses a small rise when switching primary views.
func SurfaceStageTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Animation:    &SpringPanel,
		ExitDuration: 100 * time.Millisecond,
		ExitEase:     motionEase,
		Colors:       true,
		Enter:        &ui.Motion{Y: 3, Opacity: 0.15},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// ModalSpringTransition gives centered and top-anchored dialogs a short entrance.
func ModalSpringTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Animation:    &SpringPanel,
		ExitDuration: 100 * time.Millisecond,
		ExitEase:     motionEase,
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{Y: -4, Opacity: 0.15},
		Exit:         &ui.Motion{Y: -3, Opacity: 0},
	}
}

// PopoverSpringTransition animates anchored menus, Model Deck, Font Picker, and toolbars
// with a brief slide toward their resting anchor.
func PopoverSpringTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Duration:     110 * time.Millisecond,
		Ease:         motionEase,
		ExitDuration: 70 * time.Millisecond,
		ExitEase:     motionEase,
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{Y: -3, Opacity: 0.15, Flip: true},
		Exit:         &ui.Motion{Y: -3, Opacity: 0, Flip: true},
	}
}

// OutlinePopoverTransition blooms the top-left outline card smoothly from its
// leading rail origin without vertical jump or overshoot.
func OutlinePopoverTransition() ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		Animation:    &SpringPanel,
		ExitDuration: 90 * time.Millisecond,
		ExitEase:     motionEase,
		Colors:       true,
		Shadows:      true,
		Enter:        &ui.Motion{X: -4, Opacity: 0.15},
		Exit:         &ui.Motion{X: -2, Opacity: 0},
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
	return ui.ElementTransition{
		Duration:     100 * time.Millisecond,
		Ease:         motionEase,
		ExitDuration: 100 * time.Millisecond,
		ExitEase:     motionEase,
		Colors:       true,
		Enter:        &ui.Motion{Opacity: 0.02},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// RevealTo animates a 0–1 reveal progress toward target with the panel motion,
// jumping straight to it under tests.
func RevealTo(el ui.Element, key string, target float32) float32 {
	if testing.Testing() {
		return target
	}
	return el.AnimateTo(key, target, SpringPanel)
}

// SpringTo animates a scalar toward target with the control motion, jumping
// straight to it under tests.
func SpringTo(el ui.Element, key string, target float32) float32 {
	if testing.Testing() {
		return target
	}
	return el.AnimateTo(key, target, SpringTactile)
}

// ResizeHandleTransition reveals the resize handle with the selection border.
func ResizeHandleTransition(inward float32) ui.ElementTransition {
	if testing.Testing() {
		return ui.ElementTransition{Duration: -1}
	}
	return ui.ElementTransition{
		// Keep position and height tied to the image during resize.
		Colors:       true,
		Duration:     90 * time.Millisecond,
		Ease:         motionEase,
		ExitDuration: 70 * time.Millisecond,
		Enter:        &ui.Motion{X: min(max(inward, -3), 3), Opacity: 0.3},
		Exit:         &ui.Motion{Opacity: 0},
	}
}
