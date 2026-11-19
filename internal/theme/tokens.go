package theme

// Global design tokens: the ONE source for radius, type, weight, control height,
// spacing, elevation and component metrics. UI code reads these names and never
// writes raw numbers or colours; changing a value here restyles the whole app.
// Colours live in Palette (theme.go); component colour groups live beside it.

// Radius scale (6 steps). A child clipped inside a 1px-bordered surface uses
// the step minus 1 (RadiusXLInner).
const (
	RadiusXS      float32 = 4   // keycaps, swatches, micro buttons, thin bars, inline marks
	RadiusBadge   float32 = 5   // 18x18 brand mark & notebook monogram badges
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
	FontMicro   float32 = 11 // compact monogram badges, micro keycaps, hour-range tags
	FontMeta    float32 = 12 // shortcut hints, counters, keycaps
	FontSmall   float32 = 13 // tooltips, secondary lines, compact buttons
	FontBody    float32 = 14 // controls, list rows, UI body (baseline)
	FontTitle   float32 = 15 // card titles, dialog titles
	FontEditor  float32 = 15 // editor body copy
	FontHeading float32 = 18 // panel headings, empty-state titles
	FontPage    float32 = 24 // settings page titles
	FontDisplay float32 = 32 // ghost-page title of the no-note empty state

	LineMeta    float32 = 16
	LineSmall   float32 = 18
	LineBody    float32 = 20
	LineTitle   float32 = 22
	LineEditor  float32 = 24
	LineHeading float32 = 26
	LinePage    float32 = 32
	LineDisplay float32 = 40
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

	TitleBarTitleMaxWidth float32 = 240

	CardPadY float32 = 18
	CardPadX         = Space20
	CardGap          = Space16

	// Empty states: content column, action list, illustration sizes, and the
	// free-space split that centres content then lifts it (35:65 = 15% up).
	EmptyStateWidth        float32 = 560
	EmptyActionWidth       float32 = 380
	EmptyMascotSize        float32 = 64
	EmptyMascotCompactSize float32 = 48
	EmptyLiftTop           float32 = 35
	EmptyLiftBottom        float32 = 65

	KeycapHeight   float32 = 18
	KeycapPadX     float32 = 5
	KeycapFontSize         = FontMeta

	TooltipHeight   float32 = 30
	TooltipPadX             = Space12
	TooltipFontSize         = FontSmall
	TooltipOffset           = Space6

	// In-note Find bar geometry & top clearance when scrolling matches into view.
	FindBarTopOffset           = Space12
	FindBarBorderWidth float32 = 1
	FindBarHeight              = HeightControl + Space4*2 + FindBarBorderWidth*2
	FindBarJumpGap             = Space24 + Space8

	ScrollGutter     float32 = 14
	ScrollThumbWidth float32 = 6

	ResizeHitWidth  float32 = 8
	ResizeLineWidth float32 = 2

	ImageSelectBorderWidth float32 = 1.5
	JumpHighlightBorderW   float32 = 1.5
	ImageResizeHitWidth    float32 = 18
	ImageResizeHitHeight   float32 = 56
	ImageResizePillWidth   float32 = 5
	ImageResizePillHeight  float32 = 26

	// Table border handles, seam splitters & context menu metrics (Scheme A).
	TableSelectBorderWidth float32 = 1.5
	TableOuterHaloSpread   float32 = 2
	TableColHandleW        float32 = 28
	TableColHandleH        float32 = 14
	TableRowHandleW        float32 = 14
	TableRowHandleH        float32 = 26
	TableHandleHoverBand   float32 = 16
	TableSplitHitSize      float32 = 12
	TableSplitPinSize      float32 = 20
	TableSplitHaloWidth    float32 = 2
	TableGripDotRadius     float32 = 1
	TableGripDotStepX      float32 = 3.5
	TableGripDotStepY      float32 = 2.6
	TableSwatchSize        float32 = 18
	TableSwatchRingWidth   float32 = 2
	TableMenuWidth         float32 = 244
	TableAlignSegBoxW      float32 = 26
	TableAlignSegBoxH      float32 = 22
	TableResizeBadgeH      float32 = 24

	MotionMs = 160 // shared popup enter/exit duration

	ThreeColumnMinMain float32 = 560 // narrowest editor column before the sidebar yields

	ComposerTailSpace = 2 * LineBody // blank lines kept below the caret in growing text areas

	// Edge hot-zone & floating Sidebar / AI card metrics (Scheme A).
	EdgeHotZoneWidth     float32 = 8 // outer window-edge pointer sensor width
	EdgeStripIdleWidth   float32 = 3 // resting colored edge indicator bar width
	EdgeStripHotWidth    float32 = 4 // hovered / active colored edge indicator bar width
	EdgeFloatInset               = Space10
	EdgeFloatGutterGap           = Space6
	EdgeFloatSafeClear   float32 = 10 // minimum clearance between edge strip and 54px handle gutter
	EdgeFloatHoverBuffer         = Space6
	EdgeFloatDwellMs             = 140
	EdgeFloatLeaveMs             = 120

	// Top-left outline rail hover-intent & leave grace timing.
	OutlineHoverDelayMs = 220
	OutlineHoverLeaveMs = 90

	// App Store card & header toolbar metrics.
	StoreIconBoxSize       float32 = 40  // leading app icon box inside a store card header
	StoreIconGlyphSize     float32 = 20  // Duo icon size inside StoreIconBoxSize
	StoreStatusDotSize     float32 = 6   // active indicator dot on installed plugin nav rows
	StoreMinColWidth       float32 = 340 // minimum column width for the multi-column store grid
	StorePermissionSummary         = 2   // inline permission capsules before "+N" collapse
	SegmentedControlHeight         = HeightControl
	SegmentedInnerHeight           = HeightChrome
)

// Sidebar file tree & updater metrics.
const (
	TreeRowPitch    float32 = 34 // 32 row + 2 gap
	TreeIndentStep  float32 = 20
	TreeOuterPadX           = Space12
	TreeRowPadLeft          = Space8
	TreeMoreBtnSize         = HeightMicro

	// Updater modal & sidebar badge metrics.
	UpdaterModalWidth      float32 = 600
	UpdaterModalMaxHeight  float32 = 650
	UpdaterNotesMaxHeight  float32 = 350
	UpdaterHeaderIconBox   float32 = 36
	UpdaterHeaderMarkSize  float32 = 30
	UpdaterProgressHeight  float32 = 6
	UpdaterBadgeButtonSize float32 = 30
	UpdaterBadgeDotSize    float32 = 6
	UpdaterBulletDotSize   float32 = 5
	UpdaterBulletBoxWidth  float32 = 12

	// Export theme picker tile metrics (designs/export-themes/project/Main.dc.html).
	ExportThemeTileHeight  float32 = 90
	ExportThemeThumbHeight float32 = 56
)
