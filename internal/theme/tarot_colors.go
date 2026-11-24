package theme

import (
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

var (
	tarotModeAtomic atomic.Bool
	paletteOverride atomic.Pointer[Palette]
)

// IsTarotMode reports whether the global Tarot Mode skin & ritual layer is active.
func IsTarotMode() bool {
	return tarotModeAtomic.Load()
}

// SetTarotMode toggles Tarot Mode globally across the workspace, editor, AI panel, and Quick Note.
func SetTarotMode(on bool) {
	tarotModeAtomic.Store(on)
}

func activePaletteOverride() (Palette, bool) {
	p := paletteOverride.Load()
	if p == nil {
		return Palette{}, false
	}
	return *p, true
}

// WithPalette temporarily overrides theme.Ocean() for the duration of fn on the UI thread,
// allowing the parchment editor and midnight AI panel to render in distinct Tarot surfaces
// without duplicating component code.
func WithPalette(p Palette, fn func()) {
	if fn == nil {
		return
	}
	prev := paletteOverride.Swap(&p)
	defer paletteOverride.Store(prev)
	fn()
}

// TarotFonts returns the serif and Roman-numeral font stacks used by Tarot surfaces.
func TarotSerifFont() string {
	return "Noto Serif SC, Songti SC, STSong, Source Han Serif SC, PingFang SC, serif"
}

func TarotRomanFont() string {
	return "Cormorant Garamond, Georgia, Times New Roman, Songti SC, serif"
}

// TarotColors holds the complete "午夜铜版" (Midnight Copperplate) color and foil gradient tokens.
type TarotColors struct {
	// Midnight navy surfaces
	Ground           ui.Color // #0b1424
	ChromeBar        ui.Color // #09111e
	PanelDark        ui.Color // #0a1322
	TableCanvas      ui.Color // #0e1a2c
	TableGlowCenter  ui.Color // #16263f
	TableGlowDaily   ui.Color // #17284a
	TableGlowMid     ui.Color // #0f1b2e
	QuickShell       ui.Color // #0f1a2e
	CardBackTop      ui.Color // #1a2e4f
	CardBackMid      ui.Color // #13233d
	CardBackEdge     ui.Color // #0e1b31
	CardBackShield   ui.Color // #0c1830 @ 0.72
	TodayCardBG      ui.Color // #0c182b
	TodayCardHoverBG ui.Color // #101e34
	FilterActiveBG   ui.Color // #1c2e4d
	DoubtCardBG      ui.Color // rgba(9,17,30,.86)
	CardShadowDark   ui.Color // rgba(2,6,14,.65)

	// Gold & brass foil accents
	Gold             ui.Color // #c9a25a
	GoldBright       ui.Color // #e2c07e
	GoldLight        ui.Color // #f3dfa8
	GoldGlintCore    ui.Color // #fffae8
	GoldDark         ui.Color // #a7843f
	GoldDeep         ui.Color // #7c5b1d
	GoldFlash        ui.Color // #f3d48c
	GoldRing         ui.Color // #d9b46a
	GoldBtnTop       ui.Color // #e3c27e
	GoldBtnMid       ui.Color // #c9a25a
	GoldBtnBot       ui.Color // #b0883f
	GoldBtnText      ui.Color // #1a1408
	GoldBorderHair   ui.Color // rgba(201,162,90,.24)
	GoldBorderSub    ui.Color // rgba(201,162,90,.14)
	GoldBorderSoft   ui.Color // rgba(201,162,90,.34)
	GoldBorderMid    ui.Color // rgba(201,162,90,.45)
	GoldBorderHover  ui.Color // rgba(201,162,90,.65)
	GoldHoverWash    ui.Color // rgba(201,162,90,.10)
	GoldPressWash    ui.Color // rgba(201,162,90,.16)
	GoldClothHatch   ui.Color // rgba(201,162,90,.045)
	GoldLattice      ui.Color // rgba(201,162,90,.28)
	GoldTickMissing  ui.Color // rgba(201,162,90,.18)

	// Parchment card face & writing sheet
	ParchmentCenter  ui.Color // #f4ecd9
	ParchmentMid     ui.Color // #eee4cd
	ParchmentPaper   ui.Color // #f3ecdc
	ParchmentEdge    ui.Color // #e5d9bc
	ParchmentActiveB ui.Color // #ebe0c6
	ParchmentBorder  ui.Color // rgba(122,94,44,.28)
	ParchmentFrame   ui.Color // rgba(167,132,63,.55)
	ParchmentFrame2  ui.Color // rgba(167,132,63,.25)
	ParchmentLaid    ui.Color // rgba(110,90,48,.15)
	ParchmentLaidV   ui.Color // rgba(110,90,48,.07)
	InkDark          ui.Color // #241f18
	InkBody          ui.Color // #2a241b
	InkTone          ui.Color // rgba(42,36,27,.58)
	InkMuted         ui.Color // #6b5c45
	InkSecondary     ui.Color // #4f4434
	InkAccent        ui.Color // #7a5718

	// Text on midnight surfaces
	TextPrimary   ui.Color // #ede4cf
	TextWarm      ui.Color // #d9c9a6
	TextMuted     ui.Color // #9eabbf
	TextDim       ui.Color // #8e9bb0
	ReversedDark  ui.Color // #e7a48f
	ReversedPaper ui.Color // #8c3a2c

	// Prebuilt foil & surface gradients
	FoilLightGrad ui.LinearGradient
	FoilDarkGrad  ui.LinearGradient
	GoldBtnGrad   ui.LinearGradient
	TickGotGrad   ui.LinearGradient
	ActiveRowGrad ui.LinearGradient
	CardBackGrad  ui.RadialGradient
	ParchmentGrad ui.RadialGradient
	TableGlowGrad ui.RadialGradient
	DailyGlowGrad ui.RadialGradient
	GlintGrad     ui.LinearGradient
	ScanLineGrad  ui.LinearGradient
}

var tarotColors = TarotColors{
	Ground:           ui.Hex("#0b1424"),
	ChromeBar:        ui.Hex("#09111e"),
	PanelDark:        ui.Hex("#0a1322"),
	TableCanvas:      ui.Hex("#0e1a2c"),
	TableGlowCenter:  ui.Hex("#16263f"),
	TableGlowDaily:   ui.Hex("#17284a"),
	TableGlowMid:     ui.Hex("#0f1b2e"),
	QuickShell:       ui.Hex("#0f1a2e"),
	CardBackTop:      ui.Hex("#1a2e4f"),
	CardBackMid:      ui.Hex("#13233d"),
	CardBackEdge:     ui.Hex("#0e1b31"),
	CardBackShield:   ui.RGBA(12, 24, 48, 0.72),
	TodayCardBG:      ui.Hex("#0c182b"),
	TodayCardHoverBG: ui.Hex("#101e34"),
	FilterActiveBG:   ui.Hex("#1c2e4d"),
	DoubtCardBG:      ui.RGBA(9, 17, 30, 0.88),
	CardShadowDark:   ui.RGBA(2, 6, 14, 0.65),

	Gold:            ui.Hex("#c9a25a"),
	GoldBright:      ui.Hex("#e2c07e"),
	GoldLight:       ui.Hex("#f3dfa8"),
	GoldGlintCore:   ui.Hex("#fffae8"),
	GoldDark:        ui.Hex("#a7843f"),
	GoldDeep:        ui.Hex("#7c5b1d"),
	GoldFlash:       ui.Hex("#f3d48c"),
	GoldRing:        ui.Hex("#d9b46a"),
	GoldBtnTop:      ui.Hex("#e3c27e"),
	GoldBtnMid:      ui.Hex("#c9a25a"),
	GoldBtnBot:      ui.Hex("#b0883f"),
	GoldBtnText:     ui.Hex("#1a1408"),
	GoldBorderHair:  ui.RGBA(201, 162, 90, 0.24),
	GoldBorderSub:   ui.RGBA(201, 162, 90, 0.14),
	GoldBorderSoft:  ui.RGBA(201, 162, 90, 0.34),
	GoldBorderMid:   ui.RGBA(201, 162, 90, 0.45),
	GoldBorderHover: ui.RGBA(201, 162, 90, 0.68),
	GoldHoverWash:   ui.RGBA(201, 162, 90, 0.10),
	GoldPressWash:   ui.RGBA(201, 162, 90, 0.16),
	GoldClothHatch:  ui.RGBA(201, 162, 90, 0.045),
	GoldLattice:     ui.RGBA(201, 162, 90, 0.28),
	GoldTickMissing: ui.RGBA(201, 162, 90, 0.18),

	ParchmentCenter:  ui.Hex("#f4ecd9"),
	ParchmentMid:     ui.Hex("#eee4cd"),
	ParchmentPaper:   ui.Hex("#f3ecdc"),
	ParchmentEdge:    ui.Hex("#e5d9bc"),
	ParchmentActiveB: ui.Hex("#ebe0c6"),
	ParchmentBorder:  ui.RGBA(122, 94, 44, 0.28),
	ParchmentFrame:   ui.RGBA(167, 132, 63, 0.55),
	ParchmentFrame2:  ui.RGBA(167, 132, 63, 0.25),
	ParchmentLaid:    ui.RGBA(110, 90, 48, 0.15),
	ParchmentLaidV:   ui.RGBA(110, 90, 48, 0.07),
	InkDark:          ui.Hex("#241f18"),
	InkBody:          ui.Hex("#2a241b"),
	InkTone:          ui.RGBA(42, 36, 27, 0.55),
	InkMuted:         ui.Hex("#6b5c45"),
	InkSecondary:     ui.Hex("#4f4434"),
	InkAccent:        ui.Hex("#7a5718"),

	TextPrimary:   ui.Hex("#ede4cf"),
	TextWarm:      ui.Hex("#d9c9a6"),
	TextMuted:     ui.Hex("#9eabbf"),
	TextDim:       ui.Hex("#8e9bb0"),
	ReversedDark:  ui.Hex("#e7a48f"),
	ReversedPaper: ui.Hex("#8c3a2c"),

	FoilLightGrad: ui.LinearGradient{
		Angle: 148,
		Stops: []ui.GradientStop{
			{Color: ui.Hex("#a47e3a"), At: 0},
			{Color: ui.Hex("#d2ad62"), At: 0.30},
			{Color: ui.Hex("#f3dfa8"), At: 0.50},
			{Color: ui.Hex("#c9a25a"), At: 0.72},
			{Color: ui.Hex("#93702f"), At: 1},
		},
	},
	FoilDarkGrad: ui.LinearGradient{
		Angle: 148,
		Stops: []ui.GradientStop{
			{Color: ui.Hex("#7c5b1d"), At: 0},
			{Color: ui.Hex("#a8822f"), At: 0.35},
			{Color: ui.Hex("#c9a655"), At: 0.50},
			{Color: ui.Hex("#9a752c"), At: 0.68},
			{Color: ui.Hex("#6f5119"), At: 1},
		},
	},
	GoldBtnGrad: ui.LinearGradient{
		Angle: 180,
		Stops: []ui.GradientStop{
			{Color: ui.Hex("#e3c27e"), At: 0},
			{Color: ui.Hex("#c9a25a"), At: 0.46},
			{Color: ui.Hex("#b0883f"), At: 1},
		},
	},
	TickGotGrad: ui.LinearGradient{
		Angle: 180,
		From:  ui.Hex("#ecd08f"),
		To:    ui.Hex("#b88e44"),
	},
	ActiveRowGrad: ui.LinearGradient{
		Angle: 180,
		From:  ui.Hex("#f4ecd9"),
		To:    ui.Hex("#ebe0c6"),
	},
	CardBackGrad: ui.RadialGradient{
		Center: ui.Point{X: 0.5, Y: 0.38},
		Stops: []ui.GradientStop{
			{Color: ui.Hex("#1a2e4f"), At: 0},
			{Color: ui.Hex("#13233d"), At: 0.55},
			{Color: ui.Hex("#0e1b31"), At: 1},
		},
	},
	ParchmentGrad: ui.RadialGradient{
		Center: ui.Point{X: 0.5, Y: 0.40},
		Stops: []ui.GradientStop{
			{Color: ui.Hex("#f4ecd9"), At: 0},
			{Color: ui.Hex("#eee4cd"), At: 0.58},
			{Color: ui.Hex("#e5d9bc"), At: 1},
		},
	},
	TableGlowGrad: ui.RadialGradient{
		Center: ui.Point{X: 0.5, Y: 0.46},
		Stops: []ui.GradientStop{
			{Color: ui.Hex("#16263f"), At: 0},
			{Color: ui.Hex("#0f1b2e"), At: 0.52},
			{Color: ui.Hex("#0b1424"), At: 1},
		},
	},
	DailyGlowGrad: ui.RadialGradient{
		Center: ui.Point{X: 0.5, Y: 0.50},
		Stops: []ui.GradientStop{
			{Color: ui.Hex("#17284a"), At: 0},
			{Color: ui.Hex("#0f1b2f"), At: 0.48},
			{Color: ui.Hex("#0b1424"), At: 1},
		},
	},
	GlintGrad: ui.LinearGradient{
		Angle: 112,
		Stops: []ui.GradientStop{
			{Color: ui.RGBA(255, 236, 190, 0), At: 0.0},
			{Color: ui.RGBA(255, 236, 190, 0.45), At: 0.38},
			{Color: ui.RGBA(255, 250, 232, 0.90), At: 0.50},
			{Color: ui.RGBA(255, 236, 190, 0.45), At: 0.62},
			{Color: ui.RGBA(255, 236, 190, 0), At: 1.0},
		},
	},
	ScanLineGrad: ui.LinearGradient{
		Angle: 90,
		Stops: []ui.GradientStop{
			{Color: ui.RGBA(226, 192, 126, 0), At: 0.0},
			{Color: ui.Hex("#e2c07e"), At: 0.16},
			{Color: ui.Hex("#fbefc8"), At: 0.50},
			{Color: ui.Hex("#e2c07e"), At: 0.84},
			{Color: ui.RGBA(226, 192, 126, 0), At: 1.0},
		},
	},
}

// Tarot returns the Midnight Copperplate color & gradient set.
func Tarot() TarotColors {
	return tarotColors
}

// TarotPaperPalette returns the warm parchment Palette used by the Block Editor
// and Quick Note writing surface when Tarot Mode is active.
func TarotPaperPalette() Palette {
	p := OceanLight
	p.Dark = false
	p.Tarot = true
	p.Shell = ui.Hex("#ebe0c6")
	p.Sidebar = ui.Hex("#eee4cd")
	p.SettingsBG = ui.Hex("#f3ecdc")
	p.Paper = ui.Hex("#f3ecdc")
	p.Card = ui.Hex("#f4ecd9")
	p.Menu = ui.Hex("#f4ecd9")
	p.Text = ui.Hex("#221d16")
	p.TextSecondary = ui.Hex("#5c4e38")
	p.Placeholder = ui.Hex("#7d6c52")
	p.Accent = ui.Hex("#8a6526")
	p.AccentHover = ui.Hex("#7a5718")
	p.AccentPressed = ui.Hex("#5c400f")
	p.AccentText = ui.Hex("#f4ecd9")
	p.IconFill = ui.Hex("#e5d7b8")
	p.IconShade = ui.Hex("#d4c198")
	p.IconDetail = ui.Hex("#8c795b")
	p.IconLitFill = ui.Hex("#efe1bc")
	p.IconLitShade = ui.Hex("#dcc283")
	p.IconLitDetail = ui.Hex("#9c7732")
	p.Hover = ui.RGBA(122, 94, 44, 0.09)
	p.Selected = ui.RGBA(167, 132, 63, 0.18)
	p.SelectedBorder = ui.RGBA(167, 132, 63, 0.45)
	p.Hairline = ui.RGBA(167, 132, 63, 0.30)
	p.SidebarBorder = ui.RGBA(167, 132, 63, 0.30)
	p.BtnBorder = ui.RGBA(167, 132, 63, 0.42)
	p.SwitchTrackOff = ui.RGBA(167, 132, 63, 0.16)
	p.SwitchTrackHover = ui.RGBA(167, 132, 63, 0.24)
	p.SwitchTrackPressed = ui.RGBA(167, 132, 63, 0.32)
	p.SwitchKnobOff = ui.Hex("#a08a62")
	p.SwitchKnob = ui.Hex("#fbf7ec")
	p.SwitchKnobBorder = ui.RGBA(122, 94, 44, 0.16)
	p.SwitchKnobShadow = ui.RGBA(36, 31, 24, 0.24)
	p.CodeBG = ui.Hex("#eae0c8")
	p.CodeBlockBG = ui.Hex("#eae0c8")
	p.QuoteBar = ui.Hex("#cdbb94")
	p.SelectionBG = ui.RGBA(201, 162, 90, 0.34)
	p.SelectionFG = ui.Hex("#241f18")
	p.Link = ui.Hex("#7a5718")
	p.ListGuide = ui.RGBA(167, 132, 63, 0.30)
	p.ListBullet = ui.Hex("#8a6526")
	p.TodoBG = ui.Hex("#ece2ca")
	p.TodoRing = ui.Hex("#b89a5e")
	p.TodoRingHover = ui.Hex("#8a6526")
	p.MenuBorder = ui.RGBA(167, 132, 63, 0.42)
	p.MenuIcon = ui.Hex("#4f4434")
	p.MenuMuted = ui.Hex("#6b5c45")
	p.MenuHover = ui.RGBA(122, 94, 44, 0.10)
	p.MenuSeparator = ui.RGBA(167, 132, 63, 0.25)
	p.TooltipBG = ui.Hex("#09111e")
	p.TooltipBorder = ui.RGBA(201, 162, 90, 0.42)
	p.TooltipMuted = ui.Hex("#9eabbf")
	p.TooltipAccent = ui.Hex("#e2c07e")
	p.ScrollThumb = ui.RGBA(122, 94, 44, 0.26)
	p.ScrollThumbHover = ui.RGBA(122, 94, 44, 0.44)
	p.HintBand = ui.Hex("#ece2ca")
	p.OutlineBarLit = ui.Hex("#7a5718")
	p.OutlineBarDim = ui.RGBA(167, 132, 63, 0.35)
	return p
}

// TarotMidnightPalette returns the midnight-navy & gold Palette used by the AI Panel,
// dialogs, popovers, and chrome overlays when Tarot Mode is active.
func TarotMidnightPalette() Palette {
	p := OceanDark
	p.Dark = true
	p.Tarot = true
	p.Shell = ui.Hex("#09111e")
	p.Sidebar = ui.Hex("#09111e")
	p.SettingsBG = ui.Hex("#0b1424")
	p.Paper = ui.Hex("#0a1322")
	p.Card = ui.Hex("#0f1b2e")
	p.Menu = ui.Hex("#0c182b")
	p.Text = ui.Hex("#ede4cf")
	p.TextSecondary = ui.Hex("#9eabbf")
	p.Placeholder = ui.Hex("#75839a")
	p.Accent = ui.Hex("#c9a25a")
	p.AccentHover = ui.Hex("#e2c07e")
	p.AccentPressed = ui.Hex("#b0883f")
	p.AccentText = ui.Hex("#1a1408")
	p.IconFill = ui.Hex("#1c2e4d")
	p.IconShade = ui.Hex("#283f68")
	p.IconDetail = ui.Hex("#7f91ac")
	p.IconLitFill = ui.Hex("#3a3220")
	p.IconLitShade = ui.Hex("#59492a")
	p.IconLitDetail = ui.Hex("#d6b87b")
	p.Hover = ui.RGBA(201, 162, 90, 0.10)
	p.Selected = ui.Hex("#17263f")
	p.SelectedBorder = ui.RGBA(201, 162, 90, 0.45)
	p.Hairline = ui.RGBA(201, 162, 90, 0.20)
	p.SidebarBorder = ui.RGBA(201, 162, 90, 0.24)
	p.BtnBorder = ui.RGBA(201, 162, 90, 0.40)
	p.SwitchTrackOff = ui.RGBA(201, 162, 90, 0.16)
	p.SwitchTrackHover = ui.RGBA(201, 162, 90, 0.24)
	p.SwitchTrackPressed = ui.RGBA(201, 162, 90, 0.32)
	p.SwitchKnobOff = ui.Hex("#8f7d5c")
	p.SwitchKnob = ui.Hex("#f3ece0")
	p.SwitchKnobBorder = ui.RGBA(9, 17, 30, 0.30)
	p.SwitchKnobShadow = ui.RGBA(2, 6, 14, 0.50)
	p.CodeBG = ui.Hex("#0f1c33")
	p.CodeBlockBG = ui.Hex("#0f1c33")
	p.QuoteBar = ui.Hex("#c9a25a")
	p.SelectionBG = ui.RGBA(201, 162, 90, 0.28)
	p.SelectionFG = ui.Hex("#f3dfa8")
	p.Link = ui.Hex("#e2c07e")
	p.MenuBorder = ui.RGBA(201, 162, 90, 0.34)
	p.MenuIcon = ui.Hex("#d9c9a6")
	p.MenuMuted = ui.Hex("#9eabbf")
	p.MenuHover = ui.RGBA(201, 162, 90, 0.12)
	p.MenuSeparator = ui.RGBA(201, 162, 90, 0.18)
	p.TooltipBG = ui.Hex("#09111e")
	p.TooltipBorder = ui.RGBA(201, 162, 90, 0.42)
	p.TooltipMuted = ui.Hex("#9eabbf")
	p.TooltipAccent = ui.Hex("#e2c07e")
	p.ScrollThumb = ui.RGBA(201, 162, 90, 0.26)
	p.ScrollThumbHover = ui.RGBA(201, 162, 90, 0.46)
	return p
}
