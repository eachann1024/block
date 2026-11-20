package ui

import (
	"github.com/egoist/mygo/ui"

	"block/internal/icons"
	"block/internal/theme"
)

// Checkbox renders an Ocean Paper check box with its label: a 16px box that
// fills with the accent and draws its tick when checked. It reports whether
// the value changed this frame.
func Checkbox(c *ui.Context, id, label string, value *bool, disabled bool) bool {
	pal := theme.Ocean()
	prev := *value
	if id != "" {
		c.Key(id)
	}
	cursor := ui.CursorPointer
	if disabled {
		cursor = ui.CursorDefault
	}
	row := ui.CheckboxBase(c, value).Row().AlignItems(ui.Center).Gap(theme.Space8).MinWidth(0).
		Cursor(cursor).Disabled(disabled).DimDisabled(false).Label(label)
	hovered := row.Hovered() && !disabled
	pressed := row.Pressed() && !disabled
	if disabled {
		row.Opacity(theme.DisabledOpacity)
	}
	row.Children(func() {
		bg, bd := pal.Card, pal.BtnBorder
		switch {
		case *value && pressed:
			bg, bd = pal.AccentPressed, pal.AccentPressed
		case *value && hovered:
			bg, bd = pal.AccentHover, pal.AccentHover
		case *value:
			bg, bd = pal.Accent, pal.Accent
		case hovered:
			bd = pal.BtnBorder.Mix(pal.Text, 0.25)
		}
		box := ui.Box(c).Size(16, 16).Shrink(0).Radius(theme.RadiusXS).Center()
		AnimateSurfaceColors(box, bg, bd, true)
		TactileButton(box, hovered, pressed, disabled)
		box.Children(func() {
			target := float32(0)
			if *value {
				target = 1
			}
			p := SpringTo(box, "cbTick", target)
			if p > 0.02 {
				tick := icons.Draw(c, icons.Check, 12, pal.MonoIconColors(pal.AccentText))
				tick.Opacity(min(p, 1)).Scale(0.6+0.4*p, 0.6+0.4*p)
			}
		})
		ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			TextColor(pal.Text).SingleLine().MinWidth(0)
	})
	if id != "" {
		Probe(row, id)
	}
	return *value != prev
}

// ProgressBar renders a determinate progress track of the given height.
// percent is 0–100; the fill eases toward it so chunked updates read as one
// continuous movement. Failed draws the fill in the danger color.
func ProgressBar(c *ui.Context, id string, percent, height float32, failed bool) ui.Element {
	pal := theme.Ocean()
	percent = max(0, min(percent, 100))
	radius := theme.RadiusPill
	if height <= 2 {
		radius = 0
	}
	track := ui.Box(c.Key(id)).FillWidth().Height(height).Shrink(0).Radius(radius).
		Background(pal.Hairline).Clip().Role(ui.RoleProgress)
	shown := SpringTo(track, "pbPct", percent)
	fill := pal.Accent
	if failed {
		fill = pal.Danger
	}
	track.Children(func() {
		ui.Box(c).WidthPercent(max(0, min(shown, 100))).Height(height).Radius(radius).Background(fill)
	})
	return track
}
