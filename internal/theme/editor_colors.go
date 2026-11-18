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
	if p.Dark {
		return SyntaxColors{
			Keyword: ui.Hex("#ff7b72"),
			String:  ui.Hex("#a5d6ff"),
			Comment: ui.Hex("#8b949e"),
			Number:  ui.Hex("#79c0ff"),
			Type:    ui.Hex("#ffa657"),
			Func:    ui.Hex("#d2a8ff"),
			Default: p.Text,
		}
	}
	return SyntaxColors{
		Keyword: ui.Hex("#cf222e"),
		String:  ui.Hex("#0a3069"),
		Comment: ui.Hex("#6e7781"),
		Number:  ui.Hex("#0550ae"),
		Type:    ui.Hex("#953800"),
		Func:    ui.Hex("#8250df"),
		Default: p.Text,
	}
}

// TableColors holds border and header fill colours shared by editor and AI diff tables.
type TableColors struct {
	Border   ui.Color
	HeaderBG ui.Color
}

// Table returns the table border and header background colours.
func (p Palette) Table() TableColors {
	if p.Dark {
		return TableColors{
			Border:   p.MenuBorder,
			HeaderBG: p.CodeBlockBG,
		}
	}
	return TableColors{
		Border:   p.Hairline,
		HeaderBG: p.CodeBlockBG,
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
}

// Editor returns editor-specific chrome and overlay colours.
func (p Palette) Editor() EditorColors {
	if p.Dark {
		return EditorColors{
			GripHoverBG:      p.CodeBlockBG,
			ResizeHandleBG:   ui.RGBA(34, 34, 34, 0.85),
			InlineAIDiffTint: ui.RGBA(43, 53, 72, 0.42),
		}
	}
	return EditorColors{
		GripHoverBG:      p.CodeBlockBG,
		ResizeHandleBG:   ui.RGBA(255, 255, 255, 0.85),
		InlineAIDiffTint: p.Hover.Alpha(0.72),
	}
}
