package theme

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"block/internal/model"
	"github.com/egoist/mygo/ui"
)

// Ocean Paper layout & radius constants (matching DESIGN.md & src/theme.rs).
const (
	TitleBarHeight      float32 = 40
	FullscreenBarHeight float32 = 52
	TrafficX            float32 = 16
	TrafficY            float32 = 13
	TrafficYTall        float32 = 19
	TrafficLeadingMacOS float32 = 82
	TrafficLeadingOther float32 = 16
	SidebarDefaultWidth float32 = 240
	SidebarMinWidth     float32 = 240
	SidebarMaxWidth     float32 = 420
	AIPanelDefaultWidth float32 = 380
	AIPanelMinWidth     float32 = 320
	AIPanelMaxWidth     float32 = 720
	EditorStandardWidth float32 = 672
	EditorMaxOuterWidth float32 = 720
	EditorSelectionPadX float32 = 3
)

func IsMacOS() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "js"
}

func TrafficLeading() float32 {
	if IsMacOS() {
		return TrafficLeadingMacOS
	}
	return TrafficLeadingOther
}

func TitleBarActionTrailing() float32 {
	if IsMacOS() {
		return TrafficX - 4
	}
	return 0
}

// Palette holds the complete Ocean Paper Light or Dark token set.
type Palette struct {
	Dark           bool
	Shell          ui.Color
	Sidebar        ui.Color
	SettingsBG     ui.Color
	Paper          ui.Color
	Card           ui.Color
	Menu           ui.Color
	Text           ui.Color
	TextSecondary  ui.Color
	Placeholder    ui.Color
	Accent         ui.Color
	AccentHover    ui.Color
	AccentPressed  ui.Color
	AccentText     ui.Color
	IconFill       ui.Color
	IconShade      ui.Color
	IconDetail     ui.Color
	IconLitFill    ui.Color
	IconLitShade   ui.Color
	IconLitDetail  ui.Color
	Hover          ui.Color
	Selected       ui.Color
	SelectedBorder ui.Color // neutral ring on selected rows/chips
	Hairline       ui.Color
	SidebarBorder  ui.Color
	BtnBorder      ui.Color
	CodeBG         ui.Color
	QuoteBar       ui.Color
	SelectionBG    ui.Color
	SelectionFG    ui.Color
	Danger         ui.Color
	DangerSurface  ui.Color
	Warning        ui.Color
	WarningSurface ui.Color
	Success        ui.Color
	SuccessSurface ui.Color
	DiffAddBG      ui.Color
	DiffAddFG      ui.Color
	DiffDelBG      ui.Color
	DiffDelFG      ui.Color

	// Extended component tokens mirrored from thinkly-fast (theme.rs, dropdown.rs,
	// tooltip.rs, scrollbar.rs and the editor paint modules).
	MenuBorder          ui.Color
	MenuIcon            ui.Color
	MenuMuted           ui.Color
	MenuHover           ui.Color
	MenuSeparator       ui.Color
	MenuDangerFG        ui.Color
	MenuDangerHoverBG   ui.Color
	MenuDangerHoverFG   ui.Color
	SwitchTrackOff      ui.Color
	SwitchTrackHover    ui.Color
	SwitchTrackPressed  ui.Color
	SwitchKnob          ui.Color
	SwitchKnobOff       ui.Color
	SwitchKnobBorder    ui.Color
	SwitchKnobShadow    ui.Color
	TooltipBG           ui.Color
	TooltipBorder       ui.Color
	TooltipDivider      ui.Color
	TooltipMuted        ui.Color
	TooltipAccent       ui.Color
	TooltipDanger       ui.Color
	TooltipDangerBorder ui.Color
	TooltipPillBG       ui.Color
	TooltipPillBorder   ui.Color
	TooltipPillFG       ui.Color
	ScrollThumb         ui.Color
	ScrollThumbHover    ui.Color
	Link                ui.Color
	InlineCodeFG        ui.Color
	InlineCodeBG        ui.Color
	InlineCodeHoverBG   ui.Color
	InlineCodeEditBG    ui.Color
	CodeBlockBG         ui.Color
	HintBand            ui.Color
	ListGuide           ui.Color
	ListBullet          ui.Color
	TodoBG              ui.Color
	TodoRing            ui.Color
	TodoRingHover       ui.Color
	AnnotationBG        ui.Color
	AnnotationActive    ui.Color
	AnnotationDone      ui.Color
	AnnotSendFG         ui.Color
	AnnotSendDot        ui.Color
	FindMatch           ui.Color
	FindActive          ui.Color
	FindRing            ui.Color
	SearchHighlight     ui.Color
	OutlineBarLit       ui.Color
	OutlineBarDim       ui.Color

	// Source editor (non-Markdown files): current-line band and syntax colors.
	CodeLine       ui.Color
	GutterText     ui.Color
	SyntaxKeyword  ui.Color
	SyntaxString   ui.Color
	SyntaxComment  ui.Color
	SyntaxNumber   ui.Color
	SyntaxFunction ui.Color
	SyntaxType     ui.Color
	SyntaxKey      ui.Color
	SyntaxTag      ui.Color
	SyntaxLiteral  ui.Color
}

var OceanLight = Palette{
	Dark:           false,
	Shell:          ui.Hex("#f1f1f1"),
	Sidebar:        ui.Hex("#fafafa"),
	SettingsBG:     ui.Hex("#f8f8f8"),
	Paper:          ui.Hex("#ffffff"),
	Card:           ui.Hex("#ffffff"),
	Menu:           ui.Hex("#ffffff"),
	Text:           ui.Hex("#1f1e1d"),
	TextSecondary:  ui.Hex("#52514e"),
	Placeholder:    ui.Hex("#93918c"),
	Accent:         ui.Hex("#547dd0"),
	AccentHover:    ui.Hex("#456ec2"),
	AccentPressed:  ui.Hex("#395fb0"),
	AccentText:     ui.Hex("#ffffff"),
	IconFill:       ui.Hex("#dcdcd9"),
	IconShade:      ui.Hex("#c1c1bd"),
	IconDetail:     ui.Hex("#8a909a"),
	IconLitFill:    ui.Hex("#c9d8f3"),
	IconLitShade:   ui.Hex("#a3bcea"),
	IconLitDetail:  ui.Hex("#5372ac"),
	Hover:          ui.Hex("#f0f0ef"),
	Selected:       ui.Hex("#e5e5e3"),
	SelectedBorder: ui.RGBA(0, 0, 0, 0.10),
	Hairline:       ui.Hex("#e4e6ea"),
	SidebarBorder:  ui.Hex("#e6e6e6"),
	BtnBorder:      ui.Hex("#dcdcdc"),
	CodeBG:         ui.Hex("#f8f8f6"),
	QuoteBar:       ui.Hex("#dbd9d4"),
	SelectionBG:    ui.Hex("#cde2fb"),
	SelectionFG:    ui.Hex("#184f95"),
	Danger:         ui.Hex("#d64545"),
	DangerSurface:  ui.Hex("#fdeeee"),
	Warning:        ui.Hex("#c9822b"),
	WarningSurface: ui.Hex("#fef5e7"),
	Success:        ui.Hex("#1f7344"),
	SuccessSurface: ui.Hex("#eaf6ef"),
	DiffAddBG:      ui.Hex("#e9f7ef"),
	DiffAddFG:      ui.Hex("#1e6b3f"),
	DiffDelBG:      ui.Hex("#fdecea"),
	DiffDelFG:      ui.Hex("#9e2a2b"),

	MenuBorder:          ui.Hex("#e4e4e4"),
	MenuIcon:            ui.Hex("#3d3c39"),
	MenuMuted:           ui.Hex("#898781"),
	MenuHover:           ui.RGBA(0x0b, 0x0b, 0x0b, 0x0d/255.0),
	MenuSeparator:       ui.RGBA(0x0b, 0x0b, 0x0b, 0x1a/255.0),
	MenuDangerFG:        ui.Hex("#8e2626"),
	MenuDangerHoverBG:   ui.Hex("#d03b3b"),
	MenuDangerHoverFG:   ui.Hex("#ffffff"),
	SwitchTrackOff:      ui.Hex("#eceef1"),
	SwitchTrackHover:    ui.Hex("#e1e3e7"),
	SwitchTrackPressed:  ui.Hex("#d6d9de"),
	SwitchKnob:          ui.Hex("#ffffff"),
	SwitchKnobOff:       ui.Hex("#8a94a3"),
	SwitchKnobBorder:    ui.RGBA(0, 0, 0, 0.06),
	SwitchKnobShadow:    ui.RGBA(0, 0, 0, 0.20),
	TooltipBG:           ui.Hex("#ffffff"),
	TooltipBorder:       ui.Hex("#dedede"),
	TooltipDivider:      ui.Hex("#edf0f5"),
	TooltipMuted:        ui.Hex("#586272"),
	TooltipAccent:       ui.Hex("#2f57a6"),
	TooltipDanger:       ui.Hex("#b82e2e"),
	TooltipDangerBorder: ui.Hex("#f1c6c6"),
	TooltipPillBG:       ui.Hex("#f2f4f8"),
	TooltipPillBorder:   ui.Hex("#dfe4ed"),
	TooltipPillFG:       ui.Hex("#3e4c63"),
	ScrollThumb:         ui.RGBA(0x0b, 0x0b, 0x0b, 0.26),
	ScrollThumbHover:    ui.RGBA(0x0b, 0x0b, 0x0b, 0.45),
	Link:                ui.Hex("#256abf"),
	InlineCodeFG:        ui.Hex("#eb5757"),
	InlineCodeBG:        ui.RGBA(135, 131, 120, 0.10),
	InlineCodeHoverBG:   ui.RGBA(135, 131, 120, 0.17),
	InlineCodeEditBG:    ui.RGBA(135, 131, 120, 0.24),
	CodeBlockBG:         ui.Hex("#f8f8f6"),
	HintBand:            ui.Hex("#f8f8f6"),
	ListGuide:           ui.Hex("#e6e4df"),
	ListBullet:          ui.Hex("#6f6e69"),
	TodoBG:              ui.Hex("#f8f8f6"),
	TodoRing:            ui.Hex("#dedcd6"),
	TodoRingHover:       ui.Hex("#c4c2bb"),
	AnnotationBG:        ui.Hex("#fef0d1"),
	AnnotationActive:    ui.Hex("#fdda8c"),
	AnnotationDone:      ui.Hex("#e5edfc"),
	AnnotSendFG:         ui.Hex("#184f95"),
	AnnotSendDot:        ui.Hex("#f5c84c"),
	FindMatch:           ui.Hex("#fbe7a6"),
	FindActive:          ui.Hex("#f4c84f"),
	FindRing:            ui.Hex("#c9971c"),
	SearchHighlight:     ui.Hex("#fbe7a6"),
	OutlineBarLit:       ui.Hex("#1f1e1d"),
	OutlineBarDim:       ui.Hex("#cfd5de"),

	CodeLine:       ui.Hex("#f8f8f6"),
	GutterText:     ui.Hex("#93918c"),
	SyntaxKeyword:  ui.Hex("#395fb0"),
	SyntaxString:   ui.Hex("#1e6b3f"),
	SyntaxComment:  ui.Hex("#8a8884"),
	SyntaxNumber:   ui.Hex("#a5641a"),
	SyntaxFunction: ui.Hex("#7a4a12"),
	SyntaxType:     ui.Hex("#2b6f86"),
	SyntaxKey:      ui.Hex("#395fb0"),
	SyntaxTag:      ui.Hex("#9e2a2b"),
	SyntaxLiteral:  ui.Hex("#8a3fb0"),
}

var OceanDark = Palette{
	Dark:           true,
	Shell:          ui.Hex("#262626"),
	Sidebar:        ui.Hex("#222222"),
	SettingsBG:     ui.Hex("#181818"),
	Paper:          ui.Hex("#181818"),
	Card:           ui.Hex("#222222"),
	Menu:           ui.Hex("#222222"),
	Text:           ui.Hex("#ececec"),
	TextSecondary:  ui.Hex("#b4b4b4"),
	Placeholder:    ui.Hex("#7e7e7a"),
	Accent:         ui.Hex("#779de8"),
	AccentHover:    ui.Hex("#88aae9"),
	AccentPressed:  ui.Hex("#638ad8"),
	AccentText:     ui.Hex("#111622"),
	IconFill:       ui.Hex("#474747"),
	IconShade:      ui.Hex("#5e5e5e"),
	IconDetail:     ui.Hex("#8a8a8a"),
	IconLitFill:    ui.Hex("#31456a"),
	IconLitShade:   ui.Hex("#47629c"),
	IconLitDetail:  ui.Hex("#8ea6d4"),
	Hover:          ui.Hex("#2c2c2c"),
	Selected:       ui.Hex("#3a3a3a"),
	SelectedBorder: ui.RGBA(0xff, 0xff, 0xff, 0.14),
	Hairline:       ui.Hex("#333333"),
	SidebarBorder:  ui.Hex("#303030"),
	BtnBorder:      ui.Hex("#464646"),
	CodeBG:         ui.Hex("#242424"),
	QuoteBar:       ui.Hex("#525252"),
	SelectionBG:    ui.Hex("#2d426b"),
	SelectionFG:    ui.Hex("#9dbcf5"),
	Danger:         ui.Hex("#ef6b6b"),
	DangerSurface:  ui.Hex("#3a2020"),
	Warning:        ui.Hex("#e5a54b"),
	WarningSurface: ui.Hex("#382c1a"),
	Success:        ui.Hex("#4fb87d"),
	SuccessSurface: ui.Hex("#1c3527"),
	DiffAddBG:      ui.Hex("#1b3526"),
	DiffAddFG:      ui.Hex("#7be0a6"),
	DiffDelBG:      ui.Hex("#3b1f21"),
	DiffDelFG:      ui.Hex("#f59393"),

	MenuBorder:          ui.Hex("#404040"),
	MenuIcon:            ui.Hex("#b4b4b4"),
	MenuMuted:           ui.Hex("#989792"),
	MenuHover:           ui.RGBA(0xff, 0xff, 0xff, 0x16/255.0),
	MenuSeparator:       ui.RGBA(0xff, 0xff, 0xff, 0x1a/255.0),
	MenuDangerFG:        ui.Hex("#f28b82"),
	MenuDangerHoverBG:   ui.Hex("#d03b3b"),
	MenuDangerHoverFG:   ui.Hex("#ffffff"),
	SwitchTrackOff:      ui.Hex("#333333"),
	SwitchTrackHover:    ui.Hex("#3d3d3d"),
	SwitchTrackPressed:  ui.Hex("#474747"),
	SwitchKnob:          ui.Hex("#f4f4f4"),
	SwitchKnobOff:       ui.Hex("#8e8d88"),
	SwitchKnobBorder:    ui.RGBA(0, 0, 0, 0.22),
	SwitchKnobShadow:    ui.RGBA(0, 0, 0, 0.36),
	TooltipBG:           ui.Hex("#292a2e"),
	TooltipBorder:       ui.Hex("#44464d"),
	TooltipDivider:      ui.Hex("#3a3c42"),
	TooltipMuted:        ui.Hex("#b4b4b4"),
	TooltipAccent:       ui.Hex("#8bb0f9"),
	TooltipDanger:       ui.Hex("#ff8f8f"),
	TooltipDangerBorder: ui.Hex("#5a3238"),
	TooltipPillBG:       ui.Hex("#33353b"),
	TooltipPillBorder:   ui.Hex("#494c55"),
	TooltipPillFG:       ui.Hex("#e0e0e0"),
	ScrollThumb:         ui.RGBA(0xff, 0xff, 0xff, 0.26),
	ScrollThumbHover:    ui.RGBA(0xff, 0xff, 0xff, 0.45),
	Link:                ui.Hex("#8bb0f9"),
	InlineCodeFG:        ui.Hex("#f08a80"),
	InlineCodeBG:        ui.RGBA(255, 255, 255, 0.085),
	InlineCodeHoverBG:   ui.RGBA(255, 255, 255, 0.11),
	InlineCodeEditBG:    ui.RGBA(255, 255, 255, 0.13),
	CodeBlockBG:         ui.Hex("#242424"),
	HintBand:            ui.Hex("#242424"),
	ListGuide:           ui.Hex("#3c3c3c"),
	ListBullet:          ui.Hex("#aeada9"),
	TodoBG:              ui.Hex("#1f1f1f"),
	TodoRing:            ui.Hex("#707070"),
	TodoRingHover:       ui.Hex("#8c8c8c"),
	AnnotationBG:        ui.Hex("#4a3c21"),
	AnnotationActive:    ui.Hex("#81611e"),
	AnnotationDone:      ui.Hex("#2d426b"),
	AnnotSendFG:         ui.Hex("#e6eefc"),
	AnnotSendDot:        ui.Hex("#f5c84c"),
	FindMatch:           ui.Hex("#43371a"),
	FindActive:          ui.Hex("#684c17"),
	FindRing:            ui.Hex("#dfa928"),
	SearchHighlight:     ui.Hex("#634b16"),
	OutlineBarLit:       ui.Hex("#c6c6c6"),
	OutlineBarDim:       ui.Hex("#505050"),

	CodeLine:       ui.Hex("#222222"),
	GutterText:     ui.Hex("#767676"),
	SyntaxKeyword:  ui.Hex("#9dbcf5"),
	SyntaxString:   ui.Hex("#7be0a6"),
	SyntaxComment:  ui.Hex("#8a8a8a"),
	SyntaxNumber:   ui.Hex("#e5a54b"),
	SyntaxFunction: ui.Hex("#f0d48a"),
	SyntaxType:     ui.Hex("#7fd3e6"),
	SyntaxKey:      ui.Hex("#9dbcf5"),
	SyntaxTag:      ui.Hex("#f59393"),
	SyntaxLiteral:  ui.Hex("#d1a6f5"),
}

type IconColors struct {
	Line   ui.Color
	Fill   ui.Color
	Shade  ui.Color
	Detail ui.Color
	Accent ui.Color

	// Lit enables staggered Ocean-blue color lighting in icons.DrawAnimated when active.
	Lit       bool
	LitFill   ui.Color
	LitShade  ui.Color
	LitDetail ui.Color
	LitAccent ui.Color
}

// TintedIconColors returns a harmonious single-hue 5-layer icon palette
// (e.g. Danger, Warning, Success, Callout tones) without Ocean-blue hover override.
func (p Palette) TintedIconColors(tone ui.Color) IconColors {
	fillAlpha := float32(0.18)
	shadeAlpha := float32(0.32)
	if p.Dark {
		fillAlpha = 0.28
		shadeAlpha = 0.42
	}
	return IconColors{
		Line:   tone,
		Fill:   tone.Alpha(fillAlpha),
		Shade:  tone.Alpha(shadeAlpha),
		Detail: tone.Alpha(0.68),
		Accent: tone,
	}
}

// DuoIconColors returns the 5-layer neutral resting palette with Ocean-blue hover
// targets for neutral line colors, or a harmonious tinted 5-layer set when line
// is a semantic theme color (Accent, Success, Danger, Warning).
func (p Palette) DuoIconColors(line ui.Color) IconColors {
	if line == p.Accent {
		return p.LitDuoIconColors(p.Accent)
	}
	if line == p.Success || line == p.Danger || line == p.Warning {
		return p.TintedIconColors(line)
	}
	return IconColors{
		Line:      line,
		Fill:      p.IconFill,
		Shade:     p.IconShade,
		Detail:    p.IconDetail,
		Accent:    line,
		Lit:       true,
		LitFill:   p.IconLitFill,
		LitShade:  p.IconLitShade,
		LitDetail: p.IconLitDetail,
		LitAccent: p.Accent,
	}
}

// LitDuoIconColors returns the Ocean-lit 5-layer icon colors for active/selected states.
func (p Palette) LitDuoIconColors(line ui.Color) IconColors {
	return IconColors{
		Line:      line,
		Fill:      p.IconLitFill,
		Shade:     p.IconLitShade,
		Detail:    p.IconLitDetail,
		Accent:    p.Accent,
		Lit:       true,
		LitFill:   p.IconLitFill,
		LitShade:  p.IconLitShade,
		LitDetail: p.IconLitDetail,
		LitAccent: p.Accent,
	}
}

// HoverIconColors returns the idle or hovered/pressed 5-layer icon colors.
func (p Palette) HoverIconColors(hot bool) IconColors {
	if hot {
		return p.DuoIconColors(p.Text)
	}
	return p.DuoIconColors(p.TextSecondary)
}

func (p Palette) MonoIconColors(c ui.Color) IconColors {
	return IconColors{
		Line:   c,
		Fill:   ui.Transparent,
		Shade:  ui.Transparent,
		Detail: c.Alpha(0.68),
		Accent: c,
	}
}

// MinimalIconColors renders only the primary stroke and accent layers in a
// single color (omitting fill, shade, and fine detail) for compact ticks,
// checkmarks, and shortcut keycap glyphs.
func (p Palette) MinimalIconColors(c ui.Color) IconColors {
	return IconColors{
		Line:   c,
		Fill:   ui.Transparent,
		Shade:  ui.Transparent,
		Detail: ui.Transparent,
		Accent: c,
	}
}

// IconTint names one of the Ocean Paper custom icon accent tints.
type IconTint string

const (
	IconTintNone   IconTint = "none"
	IconTintBlue   IconTint = "blue"
	IconTintTeal   IconTint = "teal"
	IconTintGreen  IconTint = "green"
	IconTintAmber  IconTint = "amber"
	IconTintRed    IconTint = "red"
	IconTintPurple IconTint = "purple"
)

// IconTints lists the icon picker tint swatches in display order.
var IconTints = []IconTint{
	IconTintNone,
	IconTintBlue,
	IconTintTeal,
	IconTintGreen,
	IconTintAmber,
	IconTintRed,
	IconTintPurple,
}

// NormalizeIconTint canonicalizes any tint string to a known IconTint.
func NormalizeIconTint(raw string) IconTint {
	switch IconTint(raw) {
	case IconTintBlue, IconTintTeal, IconTintGreen, IconTintAmber, IconTintRed, IconTintPurple:
		return IconTint(raw)
	default:
		return IconTintNone
	}
}

// IconTintLabel returns the localized display name for tint.
func IconTintLabel(t IconTint) string {
	switch t {
	case IconTintBlue:
		return Tr("蓝", "Blue")
	case IconTintTeal:
		return Tr("青", "Teal")
	case IconTintGreen:
		return Tr("绿", "Green")
	case IconTintAmber:
		return Tr("琥珀", "Amber")
	case IconTintRed:
		return Tr("红", "Red")
	case IconTintPurple:
		return Tr("紫", "Purple")
	default:
		return Tr("默认", "Default")
	}
}

// IconTintHue returns the semantic hue for a chromatic IconTint.
func (p Palette) IconTintHue(t IconTint) (ui.Color, bool) {
	if p.Dark {
		switch t {
		case IconTintBlue:
			return ui.Hex("#779de8"), true
		case IconTintTeal:
			return ui.Hex("#4fb8cf"), true
		case IconTintGreen:
			return ui.Hex("#5cb883"), true
		case IconTintAmber:
			return ui.Hex("#dfae52"), true
		case IconTintRed:
			return ui.Hex("#e8776c"), true
		case IconTintPurple:
			return ui.Hex("#a987e0"), true
		}
		return ui.Color{}, false
	}
	switch t {
	case IconTintBlue:
		return ui.Hex("#547dd0"), true
	case IconTintTeal:
		return ui.Hex("#2f9fb8"), true
	case IconTintGreen:
		return ui.Hex("#3f9a62"), true
	case IconTintAmber:
		return ui.Hex("#c08a2f"), true
	case IconTintRed:
		return ui.Hex("#d0574b"), true
	case IconTintPurple:
		return ui.Hex("#8a5fc0"), true
	}
	return ui.Color{}, false
}

// IconTintSwatch returns the swatch dot color for t in the icon picker footer.
func (p Palette) IconTintSwatch(t IconTint) ui.Color {
	if hue, ok := p.IconTintHue(t); ok {
		return hue
	}
	return p.MenuMuted
}

// MindMapBranchColors returns the 6 Ocean Paper mind map branch colors for Light or Dark mode.
func (p Palette) MindMapBranchColors() [6]ui.Color {
	if p.Dark {
		return [6]ui.Color{
			ui.Hex("#779de8"),
			ui.Hex("#4fb8cf"),
			ui.Hex("#5cb883"),
			ui.Hex("#dfae52"),
			ui.Hex("#e8776c"),
			ui.Hex("#a987e0"),
		}
	}
	return [6]ui.Color{
		ui.Hex("#547dd0"),
		ui.Hex("#2f9fb8"),
		ui.Hex("#3f9a62"),
		ui.Hex("#c08a2f"),
		ui.Hex("#d0574b"),
		ui.Hex("#8a5fc0"),
	}
}

// TintedDuoIconColors returns 5-layer icon colors for a custom file icon
// with optional tint and interactive Ocean-blue hover lighting.
func (p Palette) TintedDuoIconColors(t IconTint, line ui.Color, accentHover bool) IconColors {
	if hue, ok := p.IconTintHue(t); ok {
		fillAlpha := float32(0.24)
		shadeAlpha := float32(0.38)
		litFillAlpha := float32(0.34)
		litShadeAlpha := float32(0.48)
		if p.Dark {
			fillAlpha = 0.36
			shadeAlpha = 0.50
			litFillAlpha = 0.46
			litShadeAlpha = 0.60
		}
		if accentHover {
			return IconColors{
				Line:      line,
				Fill:      hue.Alpha(litFillAlpha),
				Shade:     hue.Alpha(litShadeAlpha),
				Detail:    hue.Alpha(0.88),
				Accent:    hue,
				Lit:       true,
				LitFill:   hue.Alpha(litFillAlpha),
				LitShade:  hue.Alpha(litShadeAlpha),
				LitDetail: hue.Alpha(0.88),
				LitAccent: hue,
			}
		}
		return IconColors{
			Line:      line,
			Fill:      hue.Alpha(fillAlpha),
			Shade:     hue.Alpha(shadeAlpha),
			Detail:    hue.Alpha(0.72),
			Accent:    hue,
			Lit:       true,
			LitFill:   hue.Alpha(litFillAlpha),
			LitShade:  hue.Alpha(litShadeAlpha),
			LitDetail: hue.Alpha(0.88),
			LitAccent: hue,
		}
	}
	if accentHover {
		return p.LitDuoIconColors(line)
	}
	return p.DuoIconColors(line)
}

// SidebarIconTileHover returns the subtle translucent tile highlight shown behind
// an interactive sidebar file icon on hover or while its picker is open.
func (p Palette) SidebarIconTileHover() ui.Color {
	if p.Dark {
		return ui.RGBA(0xff, 0xff, 0xff, 0.10)
	}
	return ui.RGBA(0, 0, 0, 0.07)
}

// EditorSelectionBG returns the unified soft translucent selection fill shared by
// empty-line bands and inline text selection pills (Option B).
func (p Palette) EditorSelectionBG() ui.Color {
	if p.Dark {
		return p.SelectionBG.Alpha(0.65)
	}
	return p.SelectionBG.Alpha(0.55)
}

func (p Palette) ToUITheme(fontFamily string) ui.Theme {
	base := *ui.LightTheme()
	if p.Dark {
		base = *ui.DarkTheme()
	}
	base.Dark = p.Dark
	base.Background = p.Paper
	base.Surface = p.Card
	base.SurfaceHover = p.Hover
	base.SurfacePressed = p.Selected
	base.Border = p.Hairline
	base.Text = p.Text
	base.TextMuted = p.TextSecondary
	base.Accent = p.Accent
	base.AccentHover = p.AccentHover
	base.AccentPressed = p.AccentPressed
	base.AccentText = p.AccentText
	base.InactiveAccent = p.Accent
	base.Danger = p.Danger
	base.Warning = p.Warning
	base.Success = p.Success
	base.Selection = p.EditorSelectionBG()
	base.Focus = p.Accent
	base.FocusRingWidth = 2
	base.FocusRingOffset = 1
	base.Radius = RadiusMD
	base.FontSize = FontBody
	if strings.TrimSpace(fontFamily) == "" {
		base.Font = DefaultUIFontStack()
	} else {
		base.Font = fontFamily
	}
	base.Scrollbar = p.ScrollThumb
	base.ScrollbarWidth = ScrollThumbWidth
	return base
}

// DefaultUIFontStack uses SF Pro through system-ui on macOS, with PingFang SC
// for Simplified Chinese. Other platforms use their own sans-serif defaults.
func DefaultUIFontStack() string {
	switch runtime.GOOS {
	case "darwin", "js":
		return "system-ui, PingFang SC, Hiragino Sans GB, Microsoft YaHei, Noto Sans SC"
	case "windows":
		return "Segoe UI, Microsoft YaHei, Noto Sans SC, sans-serif"
	default:
		return "Inter, Cantarell, Ubuntu, DejaVu Sans, Noto Sans CJK SC, Noto Sans SC, sans-serif"
	}
}

// ResolvedEditorFont resolves the configured editor body font into a complete CSS/CoreText family list.
func ResolvedEditorFont() string {
	if sf := ActiveSerifFont(); sf != "" {
		return sf
	}
	if df := ActiveDisplayFont(); df != "" {
		return df
	}
	edFont, _, _ := Typography()
	f := strings.TrimSpace(edFont)
	switch f {
	case "", "system-sans":
		return DefaultUIFontStack()
	case "system-serif":
		return "LXGW WenKai, LXGW WenKai Screen, Kaiti SC, Songti SC, Source Han Serif SC, Noto Serif CJK SC, SimSun, serif"
	default:
		return f + ", PingFang SC, Songti SC, Noto Sans SC, Microsoft YaHei"
	}
}

// ResolvedDisplayFont returns the active skin's display font stack when set,
// falling back to ResolvedEditorFont.
func ResolvedDisplayFont() string {
	if df := ActiveDisplayFont(); df != "" {
		return df
	}
	return ResolvedEditorFont()
}

// Inline highlight/text colors supported in selection palette & Markdown spans.
type NamedColor struct {
	Name string
	Zh   string
	En   string
	FG   ui.Color
	BG   ui.Color
}

func FormatPalette(dark bool) []NamedColor {
	if dark {
		return []NamedColor{
			{Name: "default", Zh: "默认", En: "Default", FG: OceanDark.Text, BG: ui.Transparent},
			{Name: "gray", Zh: "灰", En: "Gray", FG: ui.Hex("#a6a6a6"), BG: ui.Hex("#2f2f2f")},
			{Name: "brown", Zh: "棕", En: "Brown", FG: ui.Hex("#c99a7c"), BG: ui.Hex("#3a2b22")},
			{Name: "orange", Zh: "橙", En: "Orange", FG: ui.Hex("#eba059"), BG: ui.Hex("#3e2b18")},
			{Name: "yellow", Zh: "黄", En: "Yellow", FG: ui.Hex("#e5c454"), BG: ui.Hex("#3c3417")},
			{Name: "green", Zh: "绿", En: "Green", FG: ui.Hex("#6bc48d"), BG: ui.Hex("#1e3628")},
			{Name: "blue", Zh: "蓝", En: "Blue", FG: ui.Hex("#779de8"), BG: ui.Hex("#20304d")},
			{Name: "purple", Zh: "紫", En: "Purple", FG: ui.Hex("#b58ce6"), BG: ui.Hex("#312344")},
			{Name: "pink", Zh: "粉", En: "Pink", FG: ui.Hex("#e386b1"), BG: ui.Hex("#3e2230")},
			{Name: "red", Zh: "红", En: "Red", FG: ui.Hex("#ef7676"), BG: ui.Hex("#402121")},
		}
	}
	return []NamedColor{
		{Name: "default", Zh: "默认", En: "Default", FG: OceanLight.Text, BG: ui.Transparent},
		{Name: "gray", Zh: "灰", En: "Gray", FG: ui.Hex("#6f6e69"), BG: ui.Hex("#f1f0ec")},
		{Name: "brown", Zh: "棕", En: "Brown", FG: ui.Hex("#8c5a3c"), BG: ui.Hex("#f6ede7")},
		{Name: "orange", Zh: "橙", En: "Orange", FG: ui.Hex("#c4681c"), BG: ui.Hex("#fbefe3")},
		{Name: "yellow", Zh: "黄", En: "Yellow", FG: ui.Hex("#9e7b12"), BG: ui.Hex("#fbf5dd")},
		{Name: "green", Zh: "绿", En: "Green", FG: ui.Hex("#2b7a4b"), BG: ui.Hex("#e8f5ee")},
		{Name: "blue", Zh: "蓝", En: "Blue", FG: ui.Hex("#3563b8"), BG: ui.Hex("#e8f0fc")},
		{Name: "purple", Zh: "紫", En: "Purple", FG: ui.Hex("#7145a6"), BG: ui.Hex("#f1ebf9")},
		{Name: "pink", Zh: "粉", En: "Pink", FG: ui.Hex("#b03b72"), BG: ui.Hex("#fae9f1")},
		{Name: "red", Zh: "红", En: "Red", FG: ui.Hex("#c43838"), BG: ui.Hex("#fceaea")},
	}
}

func ResolveNamedFG(name string, dark bool) (ui.Color, bool) {
	if name == "" || name == "default" {
		return ui.Transparent, false
	}
	for _, item := range FormatPalette(dark) {
		if item.Name == name {
			return item.FG, true
		}
	}
	return ui.Transparent, false
}

func ResolveNamedBG(name string, dark bool) (ui.Color, bool) {
	if name == "" || name == "default" {
		return ui.Transparent, false
	}
	for _, item := range FormatPalette(dark) {
		if item.Name == name {
			return item.BG, true
		}
	}
	return ui.Transparent, false
}

// Global reactive runtime theme/language/folder-rule state.
var (
	langAtomic      atomic.Int32 // 0 = unset, 1 = Zh, 2 = En
	darkAtomic      atomic.Bool
	fontEpochAtomic atomic.Uint64
	modeMu          sync.RWMutex
	themeMode       = model.ThemeSystem
	editorFont      = ""
	codeFont        = "Menlo"
	lineHeight      = model.LineHeightNormal
	zoomLevel       = float32(1.0)
)

// BumpFontEpoch increments the global font-registration generation so cached layouts
// and font pickers refresh as soon as runtime CDN fonts finish registering.
func BumpFontEpoch() uint64 {
	return fontEpochAtomic.Add(1)
}

// FontEpoch returns the current runtime font-registration generation.
func FontEpoch() uint64 {
	return fontEpochAtomic.Load()
}

func Language() model.LanguageMode {
	switch langAtomic.Load() {
	case 1:
		return model.LangZh
	case 2:
		return model.LangEn
	default:
		l := SystemLanguage()
		SetLanguage(l)
		return l
	}
}

func SetLanguage(l model.LanguageMode) {
	if l == model.LangEn {
		langAtomic.Store(2)
	} else {
		langAtomic.Store(1)
	}
}

func IsEn() bool {
	return Language() == model.LangEn
}

func Tr(zh, en string) string {
	if IsEn() {
		return en
	}
	return zh
}

func SetDark(dark bool) {
	darkAtomic.Store(dark)
}

func IsDark() bool {
	return darkAtomic.Load()
}

var (
	surfaceMu      sync.RWMutex
	surfaceOverlay string
)

// WithSurface temporarily scopes theme.Ocean() to surface ("paper" or "chrome")
// while fn executes, restoring the previous surface on return.
func WithSurface(surface string, fn func()) {
	if fn == nil {
		return
	}
	surfaceMu.Lock()
	prev := surfaceOverlay
	surfaceOverlay = surface
	surfaceMu.Unlock()
	defer func() {
		surfaceMu.Lock()
		surfaceOverlay = prev
		surfaceMu.Unlock()
	}()
	fn()
}

func Ocean() Palette {
	base := OceanLight
	if IsDark() {
		base = OceanDark
	}
	if ActiveSkin() == nil {
		return base
	}
	surfaceMu.RLock()
	surface := surfaceOverlay
	surfaceMu.RUnlock()
	return ApplySkinPalette(base, surface)
}

// relLuminance returns the WCAG relative luminance of c (alpha ignored).
func relLuminance(c ui.Color) float64 {
	ch := func(v uint8) float64 {
		x := float64(v) / 255.0
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

// contrastRatio returns the WCAG contrast ratio between two opaque colors.
func contrastRatio(a, b ui.Color) float64 {
	la, lb := relLuminance(a), relLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// tooltipGround is the tooltip fill composited over the shell, i.e. what is seen.
func (p Palette) tooltipGround() ui.Color {
	bg := p.TooltipBG
	if bg.A < 255 {
		bg = bg.Over(p.Shell)
	}
	return bg
}

// readableOn keeps preferred when it reaches 4.5:1 over bg, else picks the
// higher-contrast of the two fallbacks.
func readableOn(bg, preferred, light, dark ui.Color) ui.Color {
	if contrastRatio(preferred, bg) >= 4.5 {
		return preferred
	}
	if contrastRatio(light, bg) >= contrastRatio(dark, bg) {
		return light
	}
	return dark
}

// TooltipPrimaryFG returns a readable primary text color over p.TooltipBG,
// preventing dark text on a dark-surface tooltip in mixed light/dark skins.
func (p Palette) TooltipPrimaryFG() ui.Color {
	return readableOn(p.tooltipGround(), p.Text, ui.Hex("#ffffff"), ui.Hex("#1e1e1e"))
}

// TooltipSecondaryFG returns a readable muted text color over p.TooltipBG.
func (p Palette) TooltipSecondaryFG() ui.Color {
	return readableOn(p.tooltipGround(), p.TooltipMuted, ui.Hex("#c3cad6"), ui.Hex("#4a5260"))
}

func SetThemeMode(m model.ThemeMode, systemDark bool) {
	modeMu.Lock()
	themeMode = m
	modeMu.Unlock()
	switch m {
	case model.ThemeDark:
		SetDark(true)
	case model.ThemeLight:
		SetDark(false)
	default:
		SetDark(systemDark)
	}
}

func CurrentThemeMode() model.ThemeMode {
	modeMu.RLock()
	defer modeMu.RUnlock()
	return themeMode
}

var (
	harmonyEnsurerMu sync.RWMutex
	harmonyEnsurer   func()

	exportEnsurerMu sync.RWMutex
	exportEnsurer   func(families []string)
)

// SetHarmonyFontEnsurer registers a callback invoked when typography or an
// active theme skin explicitly requests HarmonyOS Sans SC.
func SetHarmonyFontEnsurer(fn func()) {
	harmonyEnsurerMu.Lock()
	harmonyEnsurer = fn
	harmonyEnsurerMu.Unlock()
}

// SetExportFontEnsurer registers a callback invoked by export preview/rendering
// to ensure the requested export theme font families are cached and registered.
func SetExportFontEnsurer(fn func(families []string)) {
	exportEnsurerMu.Lock()
	exportEnsurer = fn
	exportEnsurerMu.Unlock()
}

// EnsureExportFontsForFamilies ensures any needed webfont families for export
// themes are loaded or scheduled for silent background download.
func EnsureExportFontsForFamilies(families []string) {
	if len(families) == 0 {
		return
	}
	exportEnsurerMu.RLock()
	fn := exportEnsurer
	exportEnsurerMu.RUnlock()
	if fn != nil {
		fn(families)
	}
}

func containsHarmonyFont(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "harmonyos sans sc") || strings.Contains(lower, "harmonyos_sans_sc")
}

// NeedsHarmonyFont reports whether the active typography or skin explicitly
// requests HarmonyOS Sans SC.
func NeedsHarmonyFont() bool {
	modeMu.RLock()
	ed := editorFont
	cd := codeFont
	modeMu.RUnlock()
	return containsHarmonyFont(ed) || containsHarmonyFont(cd) ||
		containsHarmonyFont(ActiveSerifFont()) || containsHarmonyFont(ActiveDisplayFont())
}

func maybeEnsureHarmonyFont(fonts ...string) {
	for _, f := range fonts {
		if containsHarmonyFont(f) {
			harmonyEnsurerMu.RLock()
			fn := harmonyEnsurer
			harmonyEnsurerMu.RUnlock()
			if fn != nil {
				fn()
			}
			return
		}
	}
}

func SetTypography(edFont, cdFont string, lh model.LineHeightMode) {
	ed := strings.TrimSpace(edFont)
	cd := strings.TrimSpace(cdFont)
	modeMu.Lock()
	editorFont = ed
	if cd != "" {
		codeFont = cd
	}
	if lh != "" {
		lineHeight = lh
	}
	modeMu.Unlock()
	maybeEnsureHarmonyFont(ed, cd)
}

func Typography() (edFont, cdFont string, lh model.LineHeightMode) {
	modeMu.RLock()
	defer modeMu.RUnlock()
	return editorFont, codeFont, lineHeight
}

func SetZoom(z float32) float32 {
	if z < 0.7 {
		z = 0.7
	}
	if z > 2.0 {
		z = 2.0
	}
	modeMu.Lock()
	zoomLevel = z
	modeMu.Unlock()
	return z
}

func Zoom() float32 {
	modeMu.RLock()
	defer modeMu.RUnlock()
	if zoomLevel <= 0 {
		return 1.0
	}
	return zoomLevel
}

// Folder rules & search ignore helpers (matching src/theme_folders.rs).
type compiledFolderRule struct {
	rule model.FolderRule
	re   *regexp.Regexp
}

var (
	folderMu             sync.RWMutex
	compiledRules        []compiledFolderRule
	ignoredSearchFolders []string
)

func SetFolderRules(rules []model.FolderRule) {
	compiled := make([]compiledFolderRule, 0, len(rules))
	for _, r := range rules {
		var re *regexp.Regexp
		if r.Regex && strings.TrimSpace(r.Pattern) != "" {
			re, _ = regexp.Compile(strings.TrimSpace(r.Pattern))
		}
		compiled = append(compiled, compiledFolderRule{rule: r, re: re})
	}
	folderMu.Lock()
	compiledRules = compiled
	folderMu.Unlock()
}

func FolderRules() []model.FolderRule {
	folderMu.RLock()
	defer folderMu.RUnlock()
	out := make([]model.FolderRule, len(compiledRules))
	for i, c := range compiledRules {
		out[i] = c.rule
	}
	return out
}

func NormalizeFolderPattern(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 && strings.HasPrefix(s, "/") && strings.HasSuffix(s, "/") {
		return s[1 : len(s)-1]
	}
	return s
}

func ValidateFolderRule(raw string, isRegex bool) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errString(Tr("规则不能为空", "Pattern cannot be empty"))
	}
	if isRegex {
		if _, err := regexp.Compile(trimmed); err != nil {
			return "", errString(Tr("正则表达式格式无效", "Invalid regular expression"))
		}
		return trimmed, nil
	}
	if trimmed == "." || trimmed == ".." || strings.ContainsAny(trimmed, `/\`) {
		return "", errString(Tr("文件夹名称不能包含路径分隔符", "Folder name cannot contain path separators"))
	}
	return trimmed, nil
}

type errString string

func (e errString) Error() string { return string(e) }

// IsFolderHidden evaluates default hidden dot-folders, assets folder, and custom FolderRules.
// Later rules override earlier rules, allowing explicit unhide (`Hidden: false`) of dot-folders.
func IsFolderHidden(folderName string, assetsFolder string, hideAssets bool, showHidden bool) bool {
	hidden := !showHidden && strings.HasPrefix(folderName, ".")
	if hideAssets && assetsFolder != "" && strings.EqualFold(folderName, assetsFolder) {
		hidden = true
	}
	folderMu.RLock()
	defer folderMu.RUnlock()
	for _, c := range compiledRules {
		pat := strings.TrimSpace(c.rule.Pattern)
		if pat == "" {
			continue
		}
		matched := false
		if c.rule.Regex {
			if c.re != nil && c.re.MatchString(folderName) {
				matched = true
			}
		} else if folderName == pat {
			matched = true
		}
		if matched {
			hidden = c.rule.Hidden
		}
	}
	return hidden
}

func SetIgnoredSearchFolders(paths []string) {
	dedup := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		clean := filepath.Clean(strings.TrimSpace(p))
		if clean == "" || clean == "." || seen[clean] {
			continue
		}
		seen[clean] = true
		dedup = append(dedup, clean)
	}
	folderMu.Lock()
	ignoredSearchFolders = dedup
	folderMu.Unlock()
}

func IgnoredSearchFolders() []string {
	folderMu.RLock()
	defer folderMu.RUnlock()
	return append([]string(nil), ignoredSearchFolders...)
}

func IsSearchFolderIgnored(absPath string) bool {
	clean := filepath.Clean(absPath)
	folderMu.RLock()
	defer folderMu.RUnlock()
	for _, ignored := range ignoredSearchFolders {
		if clean == ignored || strings.HasPrefix(clean, ignored+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func DisplayFolderPath(absPath string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if rel, err := filepath.Rel(home, absPath); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return "~"
			}
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return absPath
}
