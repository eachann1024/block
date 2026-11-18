package theme

import (
	"block/internal/model"
	"github.com/egoist/mygo/ui"
)

// CalloutColors holds the 5-state semantic colour set for callout blocks and menus.
type CalloutColors struct {
	BG       ui.Color
	Border   ui.Color
	Accent   ui.Color
	IconFill ui.Color
	Text     ui.Color
}

// Callout returns the semantic light/dark colours for a callout kind.
func (p Palette) Callout(kind model.CalloutKind) CalloutColors {
	if p.Dark {
		switch kind {
		case model.CalloutTip:
			return CalloutColors{BG: ui.Hex("#182923"), Border: ui.Hex("#284d3e"), Accent: ui.Hex("#4fd69c"), IconFill: ui.Hex("#203d31"), Text: ui.Hex("#7ce8b8")}
		case model.CalloutImportant:
			return CalloutColors{BG: ui.Hex("#231e34"), Border: ui.Hex("#3f3366"), Accent: ui.Hex("#a78bfa"), IconFill: ui.Hex("#312750"), Text: ui.Hex("#c4b5fd")}
		case model.CalloutWarning:
			return CalloutColors{BG: ui.Hex("#2b2316"), Border: ui.Hex("#544021"), Accent: ui.Hex("#f5a623"), IconFill: ui.Hex("#3d2f19"), Text: ui.Hex("#f8c165")}
		case model.CalloutCaution:
			return CalloutColors{BG: ui.Hex("#2d1b1e"), Border: ui.Hex("#592e34"), Accent: ui.Hex("#f26d6d"), IconFill: ui.Hex("#422328"), Text: ui.Hex("#ff9b9b")}
		default:
			return CalloutColors{BG: ui.Hex("#1b2434"), Border: ui.Hex("#2f4368"), Accent: ui.Hex("#7aa2f7"), IconFill: ui.Hex("#253552"), Text: ui.Hex("#9ab8ff")}
		}
	}
	switch kind {
	case model.CalloutTip:
		return CalloutColors{BG: ui.Hex("#edf7f2"), Border: ui.Hex("#bce3cf"), Accent: ui.Hex("#1f8a5b"), IconFill: ui.Hex("#d3efe1"), Text: ui.Hex("#166541")}
	case model.CalloutImportant:
		return CalloutColors{BG: ui.Hex("#f3f0fb"), Border: ui.Hex("#d6ccf2"), Accent: ui.Hex("#6e56cf"), IconFill: ui.Hex("#e4ddf8"), Text: ui.Hex("#4c389e")}
	case model.CalloutWarning:
		return CalloutColors{BG: ui.Hex("#fdf5e8"), Border: ui.Hex("#edd4a8"), Accent: ui.Hex("#b86b11"), IconFill: ui.Hex("#f7e2be"), Text: ui.Hex("#8a4e08")}
	case model.CalloutCaution:
		return CalloutColors{BG: ui.Hex("#fceeee"), Border: ui.Hex("#f1c4c4"), Accent: ui.Hex("#c93b3b"), IconFill: ui.Hex("#f8dada"), Text: ui.Hex("#962727")}
	default:
		return CalloutColors{BG: ui.Hex("#eef3fc"), Border: ui.Hex("#c9d8f3"), Accent: ui.Hex("#547dd0"), IconFill: ui.Hex("#dce7fa"), Text: ui.Hex("#2c4c8c")}
	}
}

// ReactionPillColors holds the idle/hover/active colours for annotation reaction pills.
type ReactionPillColors struct {
	FG      ui.Color
	BG      ui.Color
	HoverBG ui.Color
	Border  ui.Color
}

// ReactionPill returns the light/dark colours for a reaction pill.
func (p Palette) ReactionPill(active bool) ReactionPillColors {
	if p.Dark {
		c := ReactionPillColors{
			FG:      ui.Hex("#9ec3ff"),
			BG:      ui.Hex("#212e47"),
			HoverBG: ui.Hex("#2d4673"),
			Border:  ui.Hex("#33486e"),
		}
		if active {
			c.BG = ui.Hex("#283d63")
			c.Border = ui.Hex("#6f97e6")
		}
		return c
	}
	c := ReactionPillColors{
		FG:      ui.Hex("#1757ac"),
		BG:      ui.Hex("#eaf2fd"),
		HoverBG: ui.Hex("#d4e5fb"),
		Border:  ui.Hex("#cfe0f8"),
	}
	if active {
		c.BG = ui.Hex("#dceafb")
		c.Border = ui.Hex("#8cb3ec")
	}
	return c
}
