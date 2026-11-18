package theme

// Global design tokens: the ONE source for radius, type, weight, control height,
// spacing, elevation and component metrics. UI code reads these names and never
// writes raw numbers or colours; changing a value here restyles the whole app.
// Colours live in Palette (theme.go); component colour groups live beside it.

// Radius scale (6 steps). A child clipped inside a 1px-bordered surface uses
// the step minus 1 (RadiusXLInner).
const (
	RadiusXS      float32 = 4   // keycaps, swatches, micro buttons, thin bars, inline marks
	RadiusSM      float32 = 6   // list rows, menu rows, segments, chips, small badges
	RadiusMD      float32 = 8   // buttons, inputs, icon buttons, segmented tracks
	RadiusLG      float32 = 10  // cards, popovers, tooltips, image frames
	RadiusXL      float32 = 14  // dialogs, menus, panels, window-level surfaces
	RadiusPill    float32 = 999 // switches, pills, round dots
	RadiusXLInner         = RadiusXL - 1

	// EditorSelectionRadius is the editor's text-selection pill corner (tuned
	// against the 16px editor line; see designs/selection-styles).
	EditorSelectionRadius float32 = 5
)

// Type scale (6 UI steps + editor body). Every UI text uses one of these sizes
// with its paired line height.
const (
	FontMeta    float32 = 12 // shortcut hints, counters, keycaps
	FontSmall   float32 = 13 // tooltips, secondary lines, compact buttons
	FontBody    float32 = 14 // controls, list rows, UI body (baseline)
	FontTitle   float32 = 15 // card titles, dialog titles
	FontEditor  float32 = 16 // editor body copy
	FontHeading float32 = 18 // panel headings, empty-state titles
	FontPage    float32 = 24 // settings page titles

	LineMeta    float32 = 16
	LineSmall   float32 = 18
	LineBody    float32 = 20
	LineTitle   float32 = 22
	LineEditor  float32 = 26
	LineHeading float32 = 26
	LinePage    float32 = 32
)

// Font weights: 3 UI weights; 500 only for monospace (code, keycaps).
const (
	WeightRegular = 400 // body copy, values
	WeightControl = 460 // buttons, nav, emphasised labels
	WeightTitle   = 560 // titles, headings, strong emphasis
	WeightMono    = 500 // monospace only
)

// Control heights (5 steps).
const (
	HeightMicro   float32 = 22 // inline micro buttons, badges, tree-row actions
	HeightChrome  float32 = 26 // titlebar / panel-header icon buttons
	HeightCompact float32 = 28 // menu rows, compact toolbar buttons
	HeightControl float32 = 32 // buttons, inputs, search box, tree rows, nav items
	HeightLarge   float32 = 36 // primary dialog actions, settings rows
)

// Spacing scale. Padding / gap / margin values must be one of these steps.
const (
	Space2  float32 = 2
	Space4  float32 = 4
	Space6  float32 = 6
	Space8  float32 = 8
	Space10 float32 = 10
	Space12 float32 = 12
	Space16 float32 = 16
	Space20 float32 = 20
	Space24 float32 = 24
)

// Icon size tokens: one size per role, never interpolated.
const (
	IconChrome      float32 = 18 // standalone icon buttons (titlebar, panel headers, sidebar footer)
	IconRow         float32 = 18 // leading icon of list rows (tree, menu, settings nav, search box)
	IconAction      float32 = 16 // leading icon inside text buttons, menu check glyph
	IconMenuCheck           = IconAction
	IconInline      float32 = 14 // chevrons, close, status, check marks
	IconMicro       float32 = 12 // icons beside 12px text
	ChromeBoxSize           = HeightChrome
	IconButtonBox   float32 = 24 // plain (non-chrome) icon button box
	IconButtonGlyph         = IconAction
)

// Component metrics, expressed on the scales above.
const (
	DisabledOpacity float32 = 0.5

	MenuItemHeight         = HeightCompact
	MenuPadding            = Space4
	MenuGap                = Space2
	MenuMaxWidth   float32 = 320

	CardPadY float32 = 18
	CardPadX         = Space20
	CardGap          = Space16

	KeycapHeight   float32 = 18
	KeycapPadX     float32 = 5
	KeycapFontSize         = FontMeta

	TooltipHeight   float32 = 30
	TooltipPadX             = Space12
	TooltipFontSize         = FontSmall
	TooltipOffset           = Space6

	ScrollGutter     float32 = 14
	ScrollThumbWidth float32 = 6

	ResizeHitWidth  float32 = 8
	ResizeLineWidth float32 = 2

	ImageSelectBorderWidth  float32 = 1.5
	ImageResizeHitWidth     float32 = 18
	ImageResizeHitHeight    float32 = 56
	ImageResizePillWidth    float32 = 12 // 8px solid accent bar + 2px paper halo per side
	ImageResizePillHotWidth float32 = 14 // 10px solid accent bar + 2px paper halo per side
	ImageResizePillHeight   float32 = 48 // 44px solid accent bar + 2px paper halo top/bottom
	ImageResizePillHotH     float32 = 56 // 52px solid accent bar + 2px paper halo top/bottom
	ImageResizeHaloWidth    float32 = 2

	MotionMs = 160 // shared popup enter/exit duration

	ThreeColumnMinMain float32 = 560 // narrowest editor column before the sidebar yields
)

// Sidebar file tree metrics.
const (
	TreeRowPitch    float32 = 34 // 32 row + 2 gap
	TreeIndentStep  float32 = 20
	TreeOuterPadX           = Space12
	TreeRowPadLeft          = Space8
	TreeMoreBtnSize         = HeightMicro
)
