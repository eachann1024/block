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
	FontMicro         float32 = 11 // compact monogram badges, micro keycaps, hour-range tags
	FontMeta          float32 = 12 // shortcut hints, counters, keycaps
	FontSmall         float32 = 13 // tooltips, secondary lines, compact buttons
	FontBody          float32 = 14 // controls, list rows, UI body (baseline)
	FontTitle         float32 = 15 // card titles, dialog titles
	FontEditor        float32 = 15 // editor body copy
	FontHeading       float32 = 18 // panel headings, empty-state titles
	FontSettingsTitle float32 = 20 // settings, plugin & store page titles
	FontPage          float32 = 24 // large display numbers, about modal title
	FontDisplay       float32 = 32 // ghost-page title of the no-note empty state

	LineMeta          float32 = 16
	LineSmall         float32 = 18
	LineBody          float32 = 20
	LineTitle         float32 = 22
	LineEditor        float32 = 24
	LineHeading       float32 = 26
	LineSettingsTitle float32 = 28
	LinePage          float32 = 32
	LineDisplay       float32 = 40
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

	MenuItemHeight                = HeightCompact
	MenuPadding                   = Space4
	MenuGap                       = Space2
	MenuMaxWidth          float32 = 320
	SelectCompactMinWidth float32 = 144

	TitleBarTitleMaxWidth float32 = 240
	ExternalPathBarHeight float32 = 40 // Scheme C top breadcrumb path bar for external files

	// Settings pages: centred content column, flat sections with hairline rows.
	SettingsContentMaxWidth float32 = 600
	// Below-paragraph annotation card content measure (matches SettingsContentMaxWidth).
	AnnotBelowContentMaxWidth float32 = 600
	SettingsRowHeight         float32 = 44
	SettingsListRowHeight     float32 = 48
	SettingsSectionTitleH     float32 = 28
	SettingsSectionGap        float32 = 32
	SettingsFormLabelWidth    float32 = 72

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

	ScrollGutter          float32 = 14
	ScrollThumbWidth      float32 = 2
	ScrollThumbHoverWidth float32 = 7
	// ScrollEdgeInset is the gap between the thumb and the window edge, rest or hovered.
	ScrollEdgeInset float32 = 1

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
	EdgeHotZoneWidth     float32 = 8      // outer window-edge pointer sensor width
	EdgeStripIdleWidth   float32 = 3      // resting colored edge indicator bar width
	EdgeFloatInset               = Space8 // = stage card side/bottom gutter; floating cards sit flush on the card (top 0)
	EdgeFloatGutterGap           = Space6
	EdgeFloatSafeClear   float32 = 10 // minimum clearance between edge strip and 54px handle gutter
	EdgeFloatHoverBuffer         = Space6
	EdgeFloatDwellMs             = 140
	EdgeFloatLeaveMs             = 120

	// Narrow sidebar expand-pill cue timings & geometry (designs/settings-narrow-float Scheme A).
	SidebarExpandCueHoldMs               = 2400
	SidebarExpandCuePillMs               = 260
	SidebarExpandCueRingDelayMs          = 80
	SidebarExpandCueRingMs               = 720
	SidebarExpandCueNudgeDelayMs         = 140
	SidebarExpandCueNudgeMs              = 950
	SidebarExpandCueLabelWidthZH float32 = 30
	SidebarExpandCueLabelWidthEN float32 = 48
	SidebarExpandCueRingWidth    float32 = 1.5

	// Full-screen AI chat history drawer metrics.
	AIHistoryDrawerWidth     float32 = 300
	AIHistoryDrawerRowHeight float32 = 36

	// Right-side Orca outline rail, hover card, and directory panel metrics.
	OutlineIdleOpacity        float32 = 0.40
	OutlineMaxTicks                   = 20
	OutlineTickHeight         float32 = 3
	OutlineTickWidthIdle      float32 = 12
	OutlineTickWidthLit       float32 = 20
	OutlineTickHitHeight      float32 = 12
	OutlineTickHitPadX        float32 = 4
	OutlineRailWidth          float32 = OutlineTickWidthLit + OutlineTickHitPadX*2
	OutlineRailRightOffset            = ScrollGutter + Space4
	OutlineRailRightReserve           = OutlineRailRightOffset + OutlineRailWidth + Space4
	OutlineRailMinRightMargin float32 = 36
	OutlineBridgeWidth        float32 = 12
	OutlineHoverCardWidth     float32 = 272
	OutlinePanelWidth         float32 = 288
	OutlinePanelMaxHeight     float32 = 460
	OutlinePanelMinHeight     float32 = 160
	OutlinePanelViewportRatio float32 = 0.60
	OutlineActiveBarWidth     float32 = 3
	OutlineActiveBarHeight    float32 = 14
	OutlineJumpLockMs                 = 350
	OutlineScrollWakeMs               = 450
	OutlineIdleFadeDelayMs            = 700
	OutlineCardLeaveCloseMs           = 100
	OutlineListDwellOpenMs            = 200
	OutlineListLeaveCloseMs           = 150
	OutlineFadeDurationMs             = 180

	// App Store card & header toolbar metrics.
	StoreIconBoxSize       float32 = 42  // leading app icon box inside a store card header
	StoreIconGlyphSize     float32 = 20  // Duo icon size inside StoreIconBoxSize
	StoreStatusDotSize     float32 = 6   // active indicator dot on installed plugin nav rows
	StoreMinColWidth       float32 = 380 // minimum column width for the multi-column store grid
	StoreContentMaxWidth   float32 = 1160
	StorePagePadding       float32 = 40
	StoreCardGap           float32 = 16
	StoreCardInnerGap      float32 = 14
	StoreCardPadTop                = CardPadY
	StoreCardPadX                  = Space20
	StoreCardPadBottom             = StoreCardInnerGap
	StoreCardActionWidth   float32 = 72 // uniform width for App Store card action buttons
	StoreTitleWidth        float32 = 120
	StoreSearchWidth       float32 = 192
	StoreSearchFieldWidth  float32 = 280 // search field with the inline source/sort button
	StoreFilterButtonSize  float32 = 26
	StoreFilterMenuWidth   float32 = 200
	StoreAuthorFadeWidth   float32 = 20
	StoreDetailIconBox     float32 = 56  // app icon box in the in-place store detail header
	StoreDetailIconGlyph   float32 = 28  // Duo icon size inside StoreDetailIconBox
	StoreSkeletonTitleH    float32 = 20  // README skeleton: heading bar
	StoreSkeletonSubtitleH float32 = 16  // README skeleton: section heading bar
	StoreSkeletonLineH     float32 = 10  // README skeleton: body text bar
	StoreSkeletonMediaH    float32 = 168 // README skeleton: image / video block
	StoreEmptyIconBox      float32 = 64  // rounded icon tile in App Store empty states
	StoreEmptyIconGlyph    float32 = 32  // Duo icon size inside StoreEmptyIconBox
	StoreEmptyMaxWidth     float32 = 420 // copy column width in App Store empty states
	StoreEmptyPadY         float32 = 64  // vertical padding for App Store empty states
	// App Store featured hero showcase metrics.
	StoreHeroHeight      float32 = 224 // featured showcase card height
	StoreHeroPadX        float32 = 28  // horizontal padding inside hero content
	StoreHeroPadY        float32 = 22  // vertical padding inside hero content
	StoreHeroIconSize    float32 = 44  // bespoke plugin icon size inside hero header
	StoreHeroCopyMaxW    float32 = 360 // max measure for hero title, summary, and actions
	StoreHeroArtMaxW     float32 = 460 // max illustration width (460:220 native ratio)
	StoreHeroArtH        float32 = 220 // illustration draw height inside hero
	StoreHeroMinArtWidth float32 = 580 // minimum hero card width to display side artwork
	// Segmented control metrics.
	SegmentedControlHeight = HeightControl
	SegmentedInnerHeight   = HeightChrome

	// Global Pluck Switch & Plugin header metrics (designs/plugin-master-switch).
	// Light switch (designs/settings-light V4 方案 C): 28×16 track, 12px knob when
	// on, 10px when off, no shadow. Every size shares the same visual track; the
	// hero size only keeps a taller hit box.
	SwitchCompactTrackW       float32 = 28
	SwitchCompactTrackH       float32 = 16
	SwitchCompactKnobSize     float32 = 12
	SwitchCompactPad          float32 = 2
	SwitchCompactPressStretch float32 = 2
	SwitchTrackW              float32 = 28
	SwitchTrackH              float32 = 16
	SwitchKnobSize            float32 = 12
	SwitchPad                 float32 = 2
	SwitchPressStretch        float32 = 2
	SwitchHeroTrackW          float32 = 28
	SwitchHeroTrackH          float32 = 16
	SwitchHeroKnobSize        float32 = 12
	SwitchHeroPad             float32 = 2
	SwitchHeroPressStretch    float32 = 2
	SwitchHeroHitH            float32 = 44
	SwitchKnobOffSize         float32 = 10
	SwitchOnTrackAlpha        float32 = 0.24
	SwitchOnTrackHoverAlpha   float32 = 0.32
	SwitchOnTrackPressedAlpha float32 = 0.40
	PluckDividerHeight        float32 = 20
	PluginHeaderIconBox       float32 = 44
	PluginHeaderIconGlyph     float32 = 24
	PluginHeaderIconRadius    float32 = 12
	PluginTitlebarStatusMaxW  float32 = 240
	PluginTitlebarPopoverW    float32 = 320
	PluginTitlebarMenuW       float32 = 240
	PluginCancelChipSize      float32 = 20
	PluginCancelStopSize      float32 = 8
)

// Sidebar file tree & updater metrics.
const (
	TreeRowPitch    float32 = 34 // 32 row + 2 gap
	TreeIndentStep  float32 = 20
	TreeOuterPadX           = Space12
	TreeRowPadLeft          = Space8
	TreeMoreBtnSize         = HeightMicro

	// Updater modal & titlebar brand-mark update dot metrics.
	UpdaterModalWidth     float32 = 420
	UpdaterModalMaxHeight float32 = 650
	UpdaterNotesMaxHeight float32 = 350
	UpdaterHeaderIconBox  float32 = 36
	UpdaterHeaderMarkSize float32 = 30
	UpdaterBadgeDotSize   float32 = 6
	UpdaterBulletDotSize  float32 = 5
	UpdaterBulletBoxWidth float32 = 12
	UpdaterCatColWidthZH  float32 = 54
	UpdaterCatColWidthEN  float32 = 82
	UpdaterVerColWidth    float32 = 82

	// Export theme picker tile metrics (designs/export-themes/project/Main.dc.html).
	ExportThemeTileHeight  float32 = 90
	ExportThemeThumbHeight float32 = 56

	// AI chat attachment thumbnails & full-window attachment lightbox metrics.
	AIAttachComposerThumb      float32 = 56
	AIAttachComposerRemoveSize float32 = 18
	AIAttachSingleMaxW         float32 = 224
	AIAttachSingleMaxH         float32 = 176
	AIAttachSingleMinSide      float32 = 64
	AIAttachGridThumb          float32 = 84
	AIAttachFileChipMaxW       float32 = 220
	LightboxToolbarHeight      float32 = 40
	LightboxFilmstripThumb     float32 = 40
	LightboxStagePad           float32 = 28
	LightboxMaxTextWidth       float32 = 680
	LightboxMaxTextHeight      float32 = 560
	LightboxVideoMaxWidth      float32 = 1120

	// Media & video player control metrics (inline preview & full-window lightbox).
	VideoControlBarHeight    float32 = 40
	VideoScrubberHeight      float32 = 16
	VideoScrubberTrackH      float32 = 4
	VideoScrubberTrackHoverH float32 = 6
	VideoScrubberThumbSize   float32 = 12
	VideoVolumeSliderW       float32 = 64
	VideoMaxInlineHeight     float32 = 480
	VideoPlaceholderHeight   float32 = 180
	VideoPlayBadgeSize       float32 = 52
	VideoOSDVolumeBarW       float32 = 72
	VideoOSDVolumeBarH       float32 = 4

	// Top-right notification banner & plugin permission confirmation modal metrics.
	ToastBannerWidth         float32 = 360
	ToastBannerMinHeight     float32 = 44
	ToastBannerIconBox       float32 = 20
	PermissionModalWidth     float32 = 460
	PermissionModalMaxHeight float32 = 540
	PermissionModalIconBox   float32 = 36
)
