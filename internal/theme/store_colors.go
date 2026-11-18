package theme

import (
	"hash/fnv"

	"github.com/egoist/mygo/ui"
)

// StoreCardTheme holds the cover surface, emblem box, and Duo icon accents for an App Store feed card.
type StoreCardTheme struct {
	CoverBG      ui.Color
	EmblemBG     ui.Color
	EmblemBorder ui.Color
	IconLine     ui.Color
	IconFill     ui.Color
	IconAccent   ui.Color
	AuthorDotBG  ui.Color
	AuthorDotFG  ui.Color
}

// StoreCard returns a deterministic, subtle Ocean Paper cover palette for pluginID.
func (p Palette) StoreCard(pluginID string) StoreCardTheme {
	idx := storeCardToneIndex(pluginID)
	if p.Dark {
		switch idx {
		case 0: // Ocean Blue
			return StoreCardTheme{
				CoverBG:      ui.Hex("#1e2736"),
				EmblemBG:     ui.Hex("#263246"),
				EmblemBorder: ui.Hex("#354663"),
				IconLine:     p.Text,
				IconFill:     ui.Hex("#31456a"),
				IconAccent:   ui.Hex("#7ca3ef"),
				AuthorDotBG:  ui.Hex("#2b3a52"),
				AuthorDotFG:  ui.Hex("#a6c2f7"),
			}
		case 1: // Emerald Teal
			return StoreCardTheme{
				CoverBG:      ui.Hex("#1c2b28"),
				EmblemBG:     ui.Hex("#243834"),
				EmblemBorder: ui.Hex("#334f49"),
				IconLine:     p.Text,
				IconFill:     ui.Hex("#294740"),
				IconAccent:   ui.Hex("#62c4a7"),
				AuthorDotBG:  ui.Hex("#273e39"),
				AuthorDotFG:  ui.Hex("#8cd9c2"),
			}
		case 2: // Warm Amber
			return StoreCardTheme{
				CoverBG:      ui.Hex("#2d261d"),
				EmblemBG:     ui.Hex("#3a3125"),
				EmblemBorder: ui.Hex("#524432"),
				IconLine:     p.Text,
				IconFill:     ui.Hex("#4d3d28"),
				IconAccent:   ui.Hex("#e8ad5a"),
				AuthorDotBG:  ui.Hex("#423626"),
				AuthorDotFG:  ui.Hex("#f2c685"),
			}
		case 3: // Soft Indigo
			return StoreCardTheme{
				CoverBG:      ui.Hex("#252233"),
				EmblemBG:     ui.Hex("#302c42"),
				EmblemBorder: ui.Hex("#443e5e"),
				IconLine:     p.Text,
				IconFill:     ui.Hex("#3e3759"),
				IconAccent:   ui.Hex("#a794f0"),
				AuthorDotBG:  ui.Hex("#36314c"),
				AuthorDotFG:  ui.Hex("#c5b8f7"),
			}
		default: // Coral Rose
			return StoreCardTheme{
				CoverBG:      ui.Hex("#2e2124"),
				EmblemBG:     ui.Hex("#3b2a2e"),
				EmblemBorder: ui.Hex("#543b41"),
				IconLine:     p.Text,
				IconFill:     ui.Hex("#4f333a"),
				IconAccent:   ui.Hex("#eb8898"),
				AuthorDotBG:  ui.Hex("#452f35"),
				AuthorDotFG:  ui.Hex("#f5aeb9"),
			}
		}
	}

	switch idx {
	case 0: // Ocean Blue
		return StoreCardTheme{
			CoverBG:      ui.Hex("#eef3fb"),
			EmblemBG:     p.Paper,
			EmblemBorder: ui.Hex("#d7e2f4"),
			IconLine:     p.Text,
			IconFill:     ui.Hex("#cfe0f8"),
			IconAccent:   ui.Hex("#4a74c9"),
			AuthorDotBG:  ui.Hex("#dfe9f8"),
			AuthorDotFG:  ui.Hex("#385da8"),
		}
	case 1: // Emerald Teal
		return StoreCardTheme{
			CoverBG:      ui.Hex("#ecf6f3"),
			EmblemBG:     p.Paper,
			EmblemBorder: ui.Hex("#d1e8e1"),
			IconLine:     p.Text,
			IconFill:     ui.Hex("#c9e9df"),
			IconAccent:   ui.Hex("#2f8c72"),
			AuthorDotBG:  ui.Hex("#d8efe8"),
			AuthorDotFG:  ui.Hex("#236e59"),
		}
	case 2: // Warm Amber
		return StoreCardTheme{
			CoverBG:      ui.Hex("#fbf4ea"),
			EmblemBG:     p.Paper,
			EmblemBorder: ui.Hex("#ede0cc"),
			IconLine:     p.Text,
			IconFill:     ui.Hex("#f5e3c6"),
			IconAccent:   ui.Hex("#c47f2a"),
			AuthorDotBG:  ui.Hex("#f3e6d0"),
			AuthorDotFG:  ui.Hex("#965d17"),
		}
	case 3: // Soft Indigo
		return StoreCardTheme{
			CoverBG:      ui.Hex("#f2effb"),
			EmblemBG:     p.Paper,
			EmblemBorder: ui.Hex("#dfd9f2"),
			IconLine:     p.Text,
			IconFill:     ui.Hex("#e2dbf7"),
			IconAccent:   ui.Hex("#6d57c4"),
			AuthorDotBG:  ui.Hex("#e6e0f8"),
			AuthorDotFG:  ui.Hex("#523ea3"),
		}
	default: // Coral Rose
		return StoreCardTheme{
			CoverBG:      ui.Hex("#fbf0f2"),
			EmblemBG:     p.Paper,
			EmblemBorder: ui.Hex("#efd8dd"),
			IconLine:     p.Text,
			IconFill:     ui.Hex("#f6d9df"),
			IconAccent:   ui.Hex("#c9566b"),
			AuthorDotBG:  ui.Hex("#f4dde2"),
			AuthorDotFG:  ui.Hex("#9c3b4d"),
		}
	}
}

func storeCardToneIndex(pluginID string) int {
	switch pluginID {
	case "block-sync":
		return 0
	case "block-image-host":
		return 1
	case "block-publisher":
		return 4
	case "block-migrator":
		return 2
	case "block-diagnostics":
		return 3
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(pluginID))
	return int(h.Sum32() % 5)
}
