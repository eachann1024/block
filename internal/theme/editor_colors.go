package theme

import "github.com/egoist/mygo/ui"

// SyntaxColors holds syntax highlighting colours for code blocks.
type SyntaxColors struct {
	Keyword ui.Color
	String  ui.Color
	Comment ui.Color
	Number  ui.Color
	Type    ui.Color
	Func    ui.Color
	Default ui.Color
}

// Syntax returns the code block syntax highlighting palette.
func (p Palette) Syntax() SyntaxColors {
	var syn SyntaxColors
	if p.Dark {
		syn = SyntaxColors{
			Keyword: ui.Hex("#ff7b72"),
			String:  ui.Hex("#a5d6ff"),
			Comment: ui.Hex("#8b949e"),
			Number:  ui.Hex("#79c0ff"),
			Type:    ui.Hex("#ffa657"),
			Func:    ui.Hex("#d2a8ff"),
			Default: p.Text,
		}
	} else {
		syn = SyntaxColors{
			Keyword: ui.Hex("#cf222e"),
			String:  ui.Hex("#0a3069"),
			Comment: ui.Hex("#6e7781"),
			Number:  ui.Hex("#0550ae"),
			Type:    ui.Hex("#953800"),
			Func:    ui.Hex("#8250df"),
			Default: p.Text,
		}
	}
	if skinActive() {
		if p.SyntaxKeyword.A > 0 {
			syn.Keyword = p.SyntaxKeyword
		}
		if p.SyntaxString.A > 0 {
			syn.String = p.SyntaxString
		}
		if p.SyntaxComment.A > 0 {
			syn.Comment = p.SyntaxComment
		}
		if p.SyntaxNumber.A > 0 {
			syn.Number = p.SyntaxNumber
		}
		if p.SyntaxType.A > 0 {
			syn.Type = p.SyntaxType
		}
		if p.SyntaxFunction.A > 0 {
			syn.Func = p.SyntaxFunction
		}
	}
	return syn
}

// TableColors holds border, header, edge-pill handle, selection ring and tint colours for tables.
type TableColors struct {
	Border          ui.Color
	HeaderBG        ui.Color
	SelectedTint    ui.Color
	OuterSelectRing ui.Color
	HandleBG        ui.Color
	HandleHoverBG   ui.Color
	HandleBorder    ui.Color
	HandleFG        ui.Color
	HandleActiveBG  ui.Color
	HandleActiveFG  ui.Color
	TintSlate       ui.Color
	TintOcean       ui.Color
	TintEmerald     ui.Color
	TintAmber       ui.Color
	TintRose        ui.Color
	SwatchSlate     ui.Color
	SwatchOcean     ui.Color
	SwatchEmerald   ui.Color
	SwatchAmber     ui.Color
	SwatchRose      ui.Color
}

// CellTint resolves a subtle row/column background fill for a semantic tint name.
func (tc TableColors) CellTint(name string) (ui.Color, bool) {
	switch name {
	case "slate":
		return tc.TintSlate, true
	case "ocean":
		return tc.TintOcean, true
	case "emerald":
		return tc.TintEmerald, true
	case "amber":
		return tc.TintAmber, true
	case "rose":
		return tc.TintRose, true
	default:
		return ui.Transparent, false
	}
}

// Table returns the table border, header, handle, selection and tint colours.
func (p Palette) Table() TableColors {
	if p.Dark {
		return TableColors{
			Border:          p.MenuBorder,
			HeaderBG:        p.CodeBlockBG,
			SelectedTint:    ui.Hex("#1f2b40"),
			OuterSelectRing: p.Accent.Alpha(0.22),
			HandleBG:        p.Menu,
			HandleHoverBG:   p.Hover,
			HandleBorder:    p.BtnBorder,
			HandleFG:        p.TextSecondary,
			HandleActiveBG:  p.Accent,
			HandleActiveFG:  ui.Hex("#141820"),
			TintSlate:       ui.Hex("#2a2d32"),
			TintOcean:       ui.Hex("#1f2d46"),
			TintEmerald:     ui.Hex("#1c3329"),
			TintAmber:       ui.Hex("#352b1b"),
			TintRose:        ui.Hex("#392224"),
			SwatchSlate:     ui.Hex("#3a3f47"),
			SwatchOcean:     ui.Hex("#314972"),
			SwatchEmerald:   ui.Hex("#275340"),
			SwatchAmber:     ui.Hex("#5c4824"),
			SwatchRose:      ui.Hex("#663438"),
		}
	}
	if skinActive() && !p.Dark {
		return TableColors{
			Border:          p.Hairline,
			HeaderBG:        p.CodeBlockBG,
			SelectedTint:    p.Hover,
			OuterSelectRing: p.Accent.Alpha(0.22),
			HandleBG:        p.Paper,
			HandleHoverBG:   p.Hover,
			HandleBorder:    p.BtnBorder,
			HandleFG:        p.TextSecondary,
			HandleActiveBG:  p.AccentHover,
			HandleActiveFG:  p.AccentText,
			TintSlate:       ui.Hex("#e7e2d7"),
			TintOcean:       ui.Hex("#dde4eb"),
			TintEmerald:     ui.Hex("#dde7dc"),
			TintAmber:       ui.Hex("#f1e4c6"),
			TintRose:        ui.Hex("#f0dad4"),
			SwatchSlate:     ui.Hex("#d5cfbe"),
			SwatchOcean:     ui.Hex("#bfd0e4"),
			SwatchEmerald:   ui.Hex("#bad2bd"),
			SwatchAmber:     ui.Hex("#e6cb8e"),
			SwatchRose:      ui.Hex("#e3b9b0"),
		}
	}
	return TableColors{
		Border:          p.Hairline,
		HeaderBG:        ui.Hex("#fafaf9"),
		SelectedTint:    p.Hover,
		OuterSelectRing: p.Accent.Alpha(0.20),
		HandleBG:        p.Paper,
		HandleHoverBG:   p.Hover,
		HandleBorder:    p.BtnBorder,
		HandleFG:        p.TextSecondary,
		HandleActiveBG:  p.AccentHover,
		HandleActiveFG:  ui.Hex("#ffffff"),
		TintSlate:       ui.Hex("#f1f3f6"),
		TintOcean:       ui.Hex("#e8f0fe"),
		TintEmerald:     ui.Hex("#e7f5ee"),
		TintAmber:       ui.Hex("#fef6e4"),
		TintRose:        ui.Hex("#fdeeed"),
		SwatchSlate:     ui.Hex("#dfe4ec"),
		SwatchOcean:     ui.Hex("#c9d8f3"),
		SwatchEmerald:   ui.Hex("#bce6d1"),
		SwatchAmber:     ui.Hex("#f6dc9c"),
		SwatchRose:      ui.Hex("#f6c9c5"),
	}
}

// CodeBlockColors holds foreground and header pill colours for code blocks.
type CodeBlockColors struct {
	TextFG      ui.Color
	PillFG      ui.Color
	PillHoverFG ui.Color
	PillBG      ui.Color
	PillHoverBG ui.Color
}

// CodeBlock returns the code block text and language/copy pill colours.
func (p Palette) CodeBlock() CodeBlockColors {
	if p.Dark {
		return CodeBlockColors{
			TextFG:      p.Text,
			PillFG:      p.TextSecondary,
			PillHoverFG: p.Text,
			PillBG:      ui.Hex("#2a2a2a"),
			PillHoverBG: p.Selected,
		}
	}
	if skinActive() {
		return CodeBlockColors{
			TextFG:      p.Text,
			PillFG:      p.TextSecondary,
			PillHoverFG: p.Text,
			PillBG:      p.Card,
			PillHoverBG: p.Hover.Over(p.Card),
		}
	}
	return CodeBlockColors{
		TextFG:      ui.Hex("#393a34"),
		PillFG:      p.MenuMuted,
		PillHoverFG: p.Text,
		PillBG:      ui.Hex("#fcfcfb"),
		PillHoverBG: ui.Hex("#f4f3f0"),
	}
}

// EditorColors holds editor-specific chrome and overlay colours.
type EditorColors struct {
	GripHoverBG      ui.Color
	ResizeHandleBG   ui.Color
	InlineAIDiffTint ui.Color
	JumpRing         ui.Color
	BgDotStandard    ui.Color
	BgDotHigh        ui.Color
	BgGridStandard   ui.Color
	BgGridHigh       ui.Color
}

// Editor returns editor-specific chrome and overlay colours.
func (p Palette) Editor() EditorColors {
	if p.Dark {
		return EditorColors{
			GripHoverBG:      p.CodeBlockBG,
			ResizeHandleBG:   ui.RGBA(34, 34, 34, 0.85),
			InlineAIDiffTint: ui.RGBA(43, 53, 72, 0.42),
			JumpRing:         p.Accent.Alpha(0.68),
			BgDotStandard:    p.Text.Alpha(0.16),
			BgDotHigh:        p.Text.Alpha(0.36),
			BgGridStandard:   p.Text.Alpha(0.08),
			BgGridHigh:       p.Text.Alpha(0.18),
		}
	}
	return EditorColors{
		GripHoverBG:      p.CodeBlockBG,
		ResizeHandleBG:   ui.RGBA(255, 255, 255, 0.85),
		InlineAIDiffTint: p.Hover.Alpha(0.72),
		JumpRing:         p.Accent.Alpha(0.58),
		BgDotStandard:    p.Text.Alpha(0.18),
		BgDotHigh:        p.Text.Alpha(0.38),
		BgGridStandard:   p.Text.Alpha(0.08),
		BgGridHigh:       p.Text.Alpha(0.18),
	}
}
