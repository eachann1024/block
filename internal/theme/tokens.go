package theme

// Shared size, radius, icon and type tokens, mirrored 1:1 from thinkly-fast
// (src/ui/radius.rs, icon_size.rs, type_scale.rs, dropdown.rs, tooltip.rs,
// scrollbar.rs). Components read these instead of hardcoding numbers, so the
// whole app stays on one scale.

// Radius tokens.
const (
	RadiusSurfaceInner float32 = 13 // child clipped inside a 1px-bordered surface
	RadiusMicroMD      float32 = 5  // 22px badges, tree-row hover "more" button
	RadiusMicroSM      float32 = 4  // 18px keycaps, 16px swatches/todo, inline micro buttons
	RadiusMicroXS      float32 = 3  // image resize handles, inline diff marks
)

// Icon size tokens: one size per role, never interpolated.
const (
	IconChrome      float32 = 18 // standalone icon buttons (titlebar, panel headers, sidebar footer)
	IconRow         float32 = 18 // leading icon of list rows (tree, menu, settings nav, search box)
	IconInline      float32 = 14 // chevrons, close, status, check marks
	IconMicro       float32 = 12 // icons beside 12px text
	ChromeBoxSize   float32 = 26 // titlebar standalone button box
	IconButtonBox   float32 = 24 // plain (non-chrome) icon button box
	IconButtonGlyph float32 = 14
)

// UI type scale (integer sizes; weights only 400 / 460 / 560, 500 for mono and keycaps).
const (
	FontMeta      float32 = 12
	FontSmall     float32 = 13
	FontBody      float32 = 14
	FontCardTitle float32 = 15
	FontPageTitle float32 = 24

	WeightRegular    = 400
	WeightControl    = 460
	WeightTitle      = 560
	WeightMonoMedium = 500
)

// Shared control metrics.
const (
	ControlHeight   float32 = 32  // search box, buttons, tree rows, settings nav items
	DisabledOpacity float32 = 0.5 // disabled controls

	MenuItemHeight float32 = 28 // dropdown/context menu row
	MenuPadding    float32 = 4  // dropdown surface inner padding
	MenuGap        float32 = 2  // gap between dropdown rows
	MenuMaxWidth   float32 = 320

	KeycapHeight   float32 = 18
	KeycapPadX     float32 = 5
	KeycapFontSize float32 = 11

	TooltipHeight   float32 = 30
	TooltipPadX     float32 = 12
	TooltipFontSize float32 = 13
	TooltipOffset   float32 = 6

	ScrollGutter     float32 = 14
	ScrollThumbWidth float32 = 6

	ResizeHitWidth  float32 = 8
	ResizeLineWidth float32 = 2

	MotionMs = 160 // shared popup enter/exit duration

	ThreeColumnMinMain float32 = 560 // narrowest editor column before the sidebar yields
)

// Sidebar file tree metrics (src/files/tree.rs).
const (
	TreeRowPitch    float32 = 34 // 32 row + 2 gap
	TreeIndentStep  float32 = 20
	TreeOuterPadX   float32 = 12
	TreeRowPadLeft  float32 = 8
	TreeMoreBtnSize float32 = 22
)
