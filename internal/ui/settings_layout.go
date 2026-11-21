package ui

import (
	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// Settings layout primitives (designs/settings-light, page "V2"): a centred
// content column, flat sections whose title sits on a hairline-separated list
// of rows. No cards, no nested borders.

var (
	settingsSectionRows        []int
	settingsStandaloneHasTitle bool
	settingsStandaloneRows     int
)

// SettingsSection renders a flat section column; place SettingsSectionTitle
// first, then SettingsRow / SettingsRowFrame children.
func SettingsSection(c *ui.Context, children func()) ui.Element {
	settingsSectionRows = append(settingsSectionRows, 0)
	defer func() {
		settingsSectionRows = settingsSectionRows[:len(settingsSectionRows)-1]
		settingsStandaloneHasTitle = false
	}()
	return ui.Column(c).FillWidth().MinWidth(0).Children(children)
}

// SettingsSectionTitle renders a quiet section heading (13px, control weight,
// secondary text) with an optional trailing action (e.g. SettingsTextAction) on the right.
func SettingsSectionTitle(c *ui.Context, title string, trailing func()) {
	SettingsSectionTitleWithIcon(c, nil, title, trailing)
}

// SettingsSectionTitleWithIcon renders a section heading with an optional
// leading icon slot and trailing action.
func SettingsSectionTitleWithIcon(c *ui.Context, leading func(), title string, trailing func()) {
	pal := theme.Ocean()
	if n := len(settingsSectionRows); n > 0 {
		settingsSectionRows[n-1] = 0
	} else {
		settingsStandaloneHasTitle = true
		settingsStandaloneRows = 0
	}
	ui.Row(c).FillWidth().MinWidth(0).Height(theme.SettingsSectionTitleH).Margin(0, 0, theme.Space6, 0).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
		ui.Row(c).MinWidth(0).Shrink(1).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
			if leading != nil {
				leading()
			}
			ui.Text(c, title).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightControl).
				TextColor(pal.TextSecondary).SingleLine().MinWidth(0).Shrink(1).Ellipsis("…")
		})
		if trailing != nil {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(theme.Space4).Children(trailing)
		}
	})
}

// SettingsRowFrame returns an unstyled-content row with the standard minimum
// height and top hairline; callers add Children. Use for custom row layouts.
func SettingsRowFrame(el ui.Element) ui.Element {
	isFirst := false
	if n := len(settingsSectionRows); n > 0 {
		isFirst = settingsSectionRows[n-1] == 0
		settingsSectionRows[n-1]++
	} else if settingsStandaloneHasTitle {
		isFirst = settingsStandaloneRows == 0
		settingsStandaloneRows++
	}
	el.FillWidth().MinWidth(0).MinHeight(theme.SettingsRowHeight).
		AlignItems(ui.Center).Gap(theme.Space16)
	if !isFirst {
		pal := theme.Ocean()
		el.BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
	}
	return el
}

// SettingsRow renders "label …… control" on a hairline-topped row.
func SettingsRow(c *ui.Context, label string, control func()) ui.Element {
	pal := theme.Ocean()
	return SettingsRowFrame(ui.Row(c)).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).FontWeight(theme.WeightRegular).
			TextColor(pal.Text).SingleLine().Shrink(0)
		ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Gap(theme.Space12).Children(control)
	})
}

// SettingsTextAction renders a borderless accent text button (optional
// leading icon), used for section-level actions such as "+ 添加".
func SettingsTextAction(c *ui.Context, id, label string, leading *icons.Icon) ui.Element {
	pal := theme.Ocean()
	btn := ui.ButtonBase(c.Key(id)).Height(theme.HeightCompact).PaddingX(theme.Space8).Gap(theme.Space4).
		AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(label)
	hov := btn.Hovered()
	prs := btn.Pressed()
	bg := ui.Transparent
	if prs {
		bg = pal.Selected
	} else if hov {
		bg = pal.Hover
	}
	AnimateSurfaceColors(btn, bg, ui.Transparent, false)
	Probe(btn, id)
	btn.Children(func() {
		if leading != nil {
			icons.Draw(c, *leading, theme.IconMicro, pal.MonoIconColors(pal.Accent))
		}
		ui.Text(c, label).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightControl).
			TextColor(pal.Accent).SingleLine()
	})
	return btn
}

// SettingsSegmented renders the quiet settings variant of SegmentedControl:
// transparent track with a hairline ring and 13px labels.
func SettingsSegmented(c *ui.Context, id string, active *int, options []SegmentOption) bool {
	return SegmentedControlWithOptions(c, id, active, options, SegmentedControlOptions{Quiet: true})
}
