package ui

import (
	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// EmptyStage fills its parent and centres the content block both ways: the
// block hugs its content (capped at width) and sits in the middle of the pane,
// its items left-aligned inside it; the free space splits 35:65 above/below,
// lifting the block above true centre.
func EmptyStage(c *ui.Context, bg ui.Color, width float32, content func()) ui.Element {
	return emptyStage(c, bg, width, false, content)
}

// EmptyPageStage is EmptyStage with a content column that always spans width
// (shrinking on narrow panes), for empty states laid out like a page.
func EmptyPageStage(c *ui.Context, bg ui.Color, width float32, content func()) ui.Element {
	return emptyStage(c, bg, width, true, content)
}

// emptyStageGutterMax matches the editor's side gutter (54px handle + 14px
// gap), so the ghost page's text lines up with the note it turns into.
const emptyStageGutterMax float32 = 68

// emptyStageGutter sizes the side padding to the pane: roomy panes get the
// editor gutter, narrow ones shrink it proportionally down to Space24.
func emptyStageGutter(paneW float32) float32 {
	hi := emptyStageGutterMax
	if z := theme.Zoom(); z > 0 {
		hi *= z
	}
	if paneW <= 0 {
		return hi
	}
	return min(max(paneW*0.1, theme.Space24), hi)
}

func emptyStage(c *ui.Context, bg ui.Color, width float32, fill bool, content func()) ui.Element {
	stage := ui.Column(c).Fill().AlignItems(ui.Center).Background(bg)
	stage.Padding(0, emptyStageGutter(stage.Bounds().W))
	stage.Children(func() {
		ui.Box(c).FillWidth().Grow(theme.EmptyLiftTop).Shrink(1)
		col := ui.Column(c).MaxWidth(width).MinWidth(0).Shrink(0).AlignItems(ui.Start)
		if fill {
			col.FillWidth()
		}
		col.Children(content)
		ui.Box(c).FillWidth().Grow(theme.EmptyLiftBottom).Shrink(1)
	})
	return stage
}

// EmptyHeading is the 18px empty-state title placed under the illustration.
func EmptyHeading(c *ui.Context, text string) ui.Element {
	pal := theme.Ocean()
	return ui.Text(c, text).FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).
		FontWeight(theme.WeightTitle).TextColor(pal.Text).Margin(theme.Space20, 0, 0, 0)
}

// EmptyLead is the optional secondary line under an empty-state heading.
func EmptyLead(c *ui.Context, text string) ui.Element {
	pal := theme.Ocean()
	return ui.Text(c, text).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
		TextColor(pal.TextSecondary).Margin(theme.Space6, 0, 0, 0)
}

// EmptyActions lays out the action rows of an empty state; the rows' icons
// line up with the heading above.
func EmptyActions(c *ui.Context, rows func()) ui.Element {
	return ui.Column(c).MaxWidth(theme.EmptyActionWidth).Gap(theme.Space2).
		Margin(theme.Space16, 0, 0, -theme.Space10).Children(rows)
}

// EmptyActionSeparator is a hairline between groups of empty-state actions.
func EmptyActionSeparator(c *ui.Context) ui.Element {
	pal := theme.Ocean()
	return ui.Box(c).FillWidth().Height(1).Shrink(0).Margin(theme.Space8, theme.Space10).Background(pal.Hairline)
}

// EmptyActionRow renders one 36px action row: lead (an icon or badge), the
// label, and a right-aligned shortcut keycap when shortcut is non-empty.
func EmptyActionRow(c *ui.Context, id, label string, lead func(), shortcut string) ui.Element {
	return emptyActionRow(c, id, label, func(_ bool) {
		if lead != nil {
			lead()
		}
	}, shortcut)
}

// EmptyActionGlyphRow renders an EmptyActionRow whose leading Duo icon animates on row hover or press.
func EmptyActionGlyphRow(c *ui.Context, id, label string, glyph icons.Icon, shortcut string) ui.Element {
	return emptyActionRow(c, id, label, func(active bool) {
		pal := theme.Ocean()
		icons.DrawAnimated(c, glyph, theme.IconAction, pal.DuoIconColors(pal.Accent), active)
	}, shortcut)
}

func emptyActionRow(c *ui.Context, id, label string, lead func(active bool), shortcut string) ui.Element {
	pal := theme.Ocean()
	if id != "" {
		c.Key(id)
	}
	row := ui.ButtonBase(c).FillWidth().Height(theme.HeightLarge).PaddingX(theme.Space10).Gap(theme.Space10).
		AlignItems(ui.Center).Justify(ui.Start).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(label)
	hovered := row.Hovered()
	pressed := row.Pressed()
	bg := ui.Transparent
	switch {
	case pressed:
		bg = pal.Selected
	case hovered:
		bg = pal.Hover
	}
	AnimateSurfaceColors(row, bg, ui.Transparent, false)
	row.Children(func() {
		if lead != nil {
			lead(hovered || pressed)
		}
		ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).FontWeight(theme.WeightControl).
			TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if shortcut != "" {
			ShortcutKeycap(c, shortcut)
		}
	})
	if id != "" {
		Probe(row, id)
	}
	return row
}

// EmptyActionIcon draws the accent-coloured leading glyph of an action row.
func EmptyActionIcon(c *ui.Context, glyph icons.Icon) ui.Element {
	pal := theme.Ocean()
	return icons.Draw(c, glyph, theme.IconAction, pal.DuoIconColors(pal.Accent))
}

// EmptyHint renders a keycap followed by what it does ("/ 插入块").
func EmptyHint(c *ui.Context, keycap func(), label string) ui.Element {
	pal := theme.Ocean()
	return ui.Row(c).Gap(theme.Space6).AlignItems(ui.Center).Shrink(0).Children(func() {
		keycap()
		ui.Text(c, label).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
	})
}

// GhostRow is a bordered placeholder row for empty lists (the sidebar tree):
// it stands where the first item would be and runs its action on click.
func GhostRow(c *ui.Context, id, label string, glyph icons.Icon) ui.Element {
	pal := theme.Ocean()
	if id != "" {
		c.Key(id)
	}
	row := ui.ButtonBase(c).FillWidth().Height(theme.HeightControl).PaddingX(theme.Space10).Gap(theme.Space8).
		AlignItems(ui.Center).Justify(ui.Start).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(label)
	hovered := row.Hovered() || row.Pressed()
	border, fg, bg := pal.BtnBorder, pal.TextSecondary, ui.Transparent
	if hovered {
		border, fg, bg = pal.IconFill, pal.Text, pal.Hover
	}
	row.Border(1, border)
	AnimateSurfaceColors(row, bg, ui.Transparent, false)
	row.Children(func() {
		icons.DrawAnimated(c, glyph, theme.IconAction, pal.MonoIconColors(fg), hovered)
		ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(fg).
			Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
	})
	if id != "" {
		Probe(row, id)
	}
	return row
}
