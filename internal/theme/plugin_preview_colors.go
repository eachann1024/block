package theme

import "github.com/egoist/mygo/ui"

// PluginPreviewColors holds semantic surface tokens for live plugin previews
// (article theme simulations, multi-card pagination previews, and before/after diffs).
type PluginPreviewColors struct {
	OuterBG     ui.Color
	OuterBorder ui.Color
	CanvasBG    ui.Color
	CardBG      ui.Color
	CardBorder  ui.Color
	TitleFG     ui.Color
	BodyFG      ui.Color
	MutedFG     ui.Color
	AccentFG    ui.Color
	AccentSoft  ui.Color
	CodeBG      ui.Color
	BeforeBG    ui.Color
	BeforeBorder ui.Color
	AfterBG     ui.Color
	AfterBorder ui.Color
}

// PluginPreview returns the semantic colour group for a plugin preview surface.
// Supported presets: "ocean" (default), "paper", "ink", "warm".
func (p Palette) PluginPreview(preset string) PluginPreviewColors {
	base := PluginPreviewColors{
		OuterBG:      p.Shell,
		OuterBorder:  p.Hairline,
		CanvasBG:     p.Paper,
		CardBG:       p.Card,
		CardBorder:   p.BtnBorder,
		TitleFG:      p.Text,
		BodyFG:       p.Text,
		MutedFG:      p.TextSecondary,
		AccentFG:     p.Accent,
		AccentSoft:   p.Selected,
		CodeBG:       p.Hover,
		BeforeBG:     p.Shell,
		BeforeBorder: p.Hairline,
		AfterBG:      p.Selected,
		AfterBorder:  p.Accent,
	}

	switch preset {
	case "paper":
		base.CanvasBG = ui.Hex("#F5F8FC")
		base.CardBG = ui.Hex("#FFFFFF")
		base.CardBorder = ui.Hex("#DCE6F2")
		base.TitleFG = ui.Hex("#0F172A")
		base.BodyFG = ui.Hex("#1E293B")
		base.MutedFG = ui.Hex("#64748B")
		base.AccentFG = ui.Hex("#2563EB")
		base.AccentSoft = ui.Hex("#EFF6FF")
		base.CodeBG = ui.Hex("#F1F5F9")
	case "ink":
		base.CanvasBG = ui.Hex("#111317")
		base.CardBG = ui.Hex("#181B20")
		base.CardBorder = ui.Hex("#2A2F38")
		base.TitleFG = ui.Hex("#F3F4F6")
		base.BodyFG = ui.Hex("#E5E7EB")
		base.MutedFG = ui.Hex("#9CA3AF")
		base.AccentFG = ui.Hex("#60A5FA")
		base.AccentSoft = ui.Hex("#1E293B")
		base.CodeBG = ui.Hex("#0D0F12")
	case "warm":
		base.CanvasBG = ui.Hex("#FAF6EF")
		base.CardBG = ui.Hex("#FFFDF8")
		base.CardBorder = ui.Hex("#E6DDD0")
		base.TitleFG = ui.Hex("#2B241D")
		base.BodyFG = ui.Hex("#3B3128")
		base.MutedFG = ui.Hex("#786B5D")
		base.AccentFG = ui.Hex("#9A3412")
		base.AccentSoft = ui.Hex("#FEF3C7")
		base.CodeBG = ui.Hex("#F3ECE1")
	}
	return base
}
