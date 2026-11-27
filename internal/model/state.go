package model

import "os"

type ThemeMode string

const (
	ThemeSystem ThemeMode = "system"
	ThemeLight  ThemeMode = "light"
	ThemeDark   ThemeMode = "dark"
)

type LineHeightMode string

const (
	LineHeightTight  LineHeightMode = "tight"
	LineHeightNormal LineHeightMode = "normal"
	LineHeightLoose  LineHeightMode = "loose"
)

func (l LineHeightMode) Factor() float32 {
	switch l {
	case LineHeightTight:
		return 1.35
	case LineHeightLoose:
		return 1.75
	default:
		return 1.5
	}
}

type FontChoice string

const (
	FontSans  FontChoice = "sans"
	FontSerif FontChoice = "serif"
)

type LanguageMode string

const (
	LangZh LanguageMode = "zh"
	LangEn LanguageMode = "en"
)

type FolderRule struct {
	Pattern string `json:"pattern"`
	Regex   bool   `json:"regex"`
	Hidden  bool   `json:"hidden"`
}

type LastFormatColors struct {
	TextColor       string `json:"textColor"`
	BackgroundColor string `json:"backgroundColor"`
}

type FeedbackDraft struct {
	Category string `json:"category,omitempty"`
	Platform string `json:"platform,omitempty"`
	Message  string `json:"message,omitempty"`
	Contact  string `json:"contact,omitempty"`
}

type SavedWindowBounds struct {
	X          float32 `json:"x"`
	Y          float32 `json:"y"`
	Width      float32 `json:"width"`
	Height     float32 `json:"height"`
	DisplayID  *uint32 `json:"displayId,omitempty"`
	Fullscreen bool    `json:"fullscreen,omitempty"`
	Maximized  bool    `json:"maximized,omitempty"`
}

type ExportFormat string

const (
	ExportPNG      ExportFormat = "png"
	ExportHTML     ExportFormat = "html"
	ExportPDF      ExportFormat = "pdf"
	ExportDOCX     ExportFormat = "docx"
	ExportMarkdown ExportFormat = "markdown"
	ExportZIP      ExportFormat = "zip"
)

// ExportTheme enumerates the 12 paper styles (6 light + 6 dark) from thinkly-fast/src/export/palette.rs.
type ExportTheme string

const (
	ExportThemeMono     ExportTheme = "mono"
	ExportThemeJournal  ExportTheme = "journal"
	ExportThemeRiso     ExportTheme = "riso"
	ExportThemePress    ExportTheme = "press"
	ExportThemePoster   ExportTheme = "poster"
	ExportThemeMist     ExportTheme = "mist"
	ExportThemeInk      ExportTheme = "ink"
	ExportThemeKraft    ExportTheme = "kraft"
	ExportThemeNeon     ExportTheme = "neon"
	ExportThemeFilm     ExportTheme = "film"
	ExportThemeNight    ExportTheme = "night"
	ExportThemeTerminal ExportTheme = "terminal"
)

func (t ExportTheme) IsDark() bool {
	switch t {
	case ExportThemeInk, ExportThemeKraft, ExportThemeNeon, ExportThemeFilm, ExportThemeNight, ExportThemeTerminal:
		return true
	default:
		return false
	}
}

type ExportPreferences struct {
	Format      ExportFormat `json:"format,omitempty"`
	Theme       string       `json:"theme,omitempty"` // legacy alias ("light", "dark", "current") or ExportTheme name
	Style       ExportTheme  `json:"style,omitempty"`
	LightStyle  ExportTheme  `json:"light_style,omitempty"`
	DarkStyle   ExportTheme  `json:"dark_style,omitempty"`
	ShowTitle   *bool        `json:"show_title,omitempty"`
	Watermark   *bool        `json:"watermark,omitempty"`
	IncludeMeta bool         `json:"includeMeta,omitempty"`
	Width       int          `json:"width,omitempty"`
	Title       string       `json:"-"`
}

// OpenKind classifies any opened file in a tab (matching thinkly-fast/src/files/classify.rs).
type OpenKind uint8

const (
	OpenKindMarkdown OpenKind = iota
	OpenKindSource
	OpenKindPreview
	OpenKindBlocked
)

type TextEncoding uint8

const (
	TextEncodingUTF8 TextEncoding = iota
	TextEncodingUTF16LE
	TextEncodingUTF16BE
)

type SourceFile struct {
	Text            string
	BOM             bool
	Encoding        TextEncoding
	BinaryPlist     bool
	DiskFingerprint int64
	Language        string
	Highlight       bool
	HTML            bool
	SVG             bool
}

type ClassifiedFile struct {
	Kind         OpenKind
	Text         string      // For OpenKindMarkdown
	Source       *SourceFile // For OpenKindSource, or editable SVG when Kind == OpenKindPreview
	PreviewBytes []byte      // Bounded regular-file preview bytes for OpenKindPreview (SVG or raster image)
	Reason       string      // For OpenKindBlocked
	RawSize      int64
}

// VerifiedFileTarget records the verified filesystem identity and optional os.Root
// confinement of a file already opened, checked, and classified from disk.
type VerifiedFileTarget struct {
	AbsPath         string
	ConfineRoot     string
	ConfineRel      string
	ConfineRootInfo os.FileInfo
	FileInfo        os.FileInfo
	RootIdentity    string
	FileIdentity    string
	EnforceIdentity bool
}

// PreparedTabTarget carries the background-verified tab resolution result so the
// main thread can apply tab state without performing any blocking filesystem I/O.
type PreparedTabTarget struct {
	TabPath           string
	ReusedTab         bool
	Verified          VerifiedFileTarget
	NoteDir           string
	NoteDirInfo       os.FileInfo
	ProjRoot          string
	ProjRootInfo      os.FileInfo
	NoteBelongsToProj bool
}

// PreparedDirectoryTarget carries the background-resolved directory reveal target
// so the main thread can update sidebar focus or hand off to the OS shell without I/O.
type PreparedDirectoryTarget struct {
	AbsPath        string
	InNotebook     bool
	NotebookFolder string
	NotebookDir    string
}

// PersistedTabBinding stores confined root & file identity metadata in state.json
// so restarted sessions can re-verify confined tabs before restoring them.
type PersistedTabBinding struct {
	ConfineRoot  string `json:"confine_root,omitempty"`
	ConfineRel   string `json:"confine_rel,omitempty"`
	RootIdentity string `json:"root_identity,omitempty"`
	FileIdentity string `json:"file_identity,omitempty"`
}

// AppState matches ~/Library/Application Support/Block/state.json 1:1.
type AppState struct {
	Folder               string                         `json:"folder,omitempty"`
	Notebooks            []string                       `json:"notebooks,omitempty"`
	NotebookIdentities   map[string]string              `json:"notebook_identities,omitempty"`
	FileOrder            map[string][]string            `json:"file_order,omitempty"`
	IgnoredSearchFolders []string                       `json:"ignored_search_folders,omitempty"`
	SidebarWidth         *float32                       `json:"sidebar_width,omitempty"`
	SidebarCollapsed     bool                           `json:"sidebar_collapsed,omitempty"`
	AIPanelWidth         *float32                       `json:"ai_panel_width,omitempty"`
	AIFullscreen         bool                           `json:"ai_fullscreen,omitempty"`
	Theme                ThemeMode                      `json:"theme,omitempty"`
	Language             LanguageMode                   `json:"language,omitempty"`
	FontFamily           FontChoice                     `json:"font_family,omitempty"`
	FontSmoothing        *bool                          `json:"font_smoothing,omitempty"`
	AssetsFolder         string                         `json:"assets_folder,omitempty"`
	HideAssetsFolder     *bool                          `json:"hide_assets_folder,omitempty"`
	HideNonMarkdownFiles *bool                          `json:"hide_non_markdown_files,omitempty"`
	ShowHiddenFiles      bool                           `json:"show_hidden_files,omitempty"`
	SourceSoftWrap       bool                           `json:"source_soft_wrap,omitempty"`
	HiddenFolderRegex    string                         `json:"hidden_folder_regex,omitempty"`
	FolderRules          []FolderRule                   `json:"folder_rules,omitempty"`
	ShortcutSearch       string                         `json:"shortcut_search,omitempty"`
	ShortcutQuick        string                         `json:"shortcut_quick,omitempty"`
	CustomShortcuts      map[string]string              `json:"custom_shortcuts,omitempty"`
	OpenTabs             []string                       `json:"open_tabs,omitempty"`
	ActiveTab            string                         `json:"active_tab,omitempty"`
	TabBindings          map[string]PersistedTabBinding `json:"tab_bindings,omitempty"`
	LastFormatColors     *LastFormatColors              `json:"last_format_colors,omitempty"`
	ExportPreferences    *ExportPreferences             `json:"export_preferences,omitempty"`
	ExportDirectory      string                         `json:"export_directory,omitempty"`
	MainWindowBounds     *SavedWindowBounds             `json:"main_window_bounds,omitempty"`
	EditorZoom           *float32                       `json:"editor_zoom,omitempty"`
	QuickEditorZoom      *float32                       `json:"quick_editor_zoom,omitempty"`
	EditorFont           string                         `json:"editor_font,omitempty"`
	CodeFont             string                         `json:"code_font,omitempty"`
	LineHeight           LineHeightMode                 `json:"line_height,omitempty"`
	QuickWindowBounds    *SavedWindowBounds             `json:"quick_window_bounds,omitempty"`
	FeedbackDraft        *FeedbackDraft                 `json:"feedback_draft,omitempty"`
	UpdateSource         string                         `json:"update_source,omitempty"`
	UpdateChannel        string                         `json:"update_channel,omitempty"`
	LastUpdatedFrom      string                         `json:"last_updated_from,omitempty"`
	TelemetryOff         bool                           `json:"telemetry_off,omitempty"`
	BrandDeco            BrandDeco                      `json:"brand_deco,omitzero"`
	TreeSort             TreeSortMode                   `json:"tree_sort,omitempty"`
	OutlineView          OutlineViewMode                `json:"outline_view,omitempty"`
	PinnedPluginButtons []string `json:"pinned_plugin_buttons,omitempty"`
	WindowGlass         *bool    `json:"window_glass,omitempty"`
	MindMapTutorialSeen bool     `json:"mindmap_tutorial_seen,omitempty"`
}

// WindowGlassOn reports whether the sidebar and titlebar show the system window
// material; unset means on.
func (s AppState) WindowGlassOn() bool { return s.WindowGlass == nil || *s.WindowGlass }

// OutlineViewMode controls the preferred right-rail outline overlay view.
type OutlineViewMode string

const (
	OutlineViewAuto OutlineViewMode = ""
	OutlineViewCard OutlineViewMode = "card"
	OutlineViewList OutlineViewMode = "list"
)

// TreeSortMode controls how sibling entries are ordered in the sidebar file tree.
type TreeSortMode string

const (
	TreeSortManual   TreeSortMode = ""
	TreeSortName     TreeSortMode = "name"
	TreeSortModified TreeSortMode = "modified"
)

// BrandDecoMode chooses how the titlebar brand scene is picked; the zero value follows the clock.
type BrandDecoMode string

const (
	BrandDecoByTime BrandDecoMode = ""
	BrandDecoFixed  BrandDecoMode = "fixed"
	BrandDecoOff    BrandDecoMode = "off"
)

// BrandDecoMotion is the legacy combined motion tier. It is only read as a fallback when
// Density is unset, so states saved by older builds keep their density preference.
type BrandDecoMotion string

const (
	BrandDecoMotionRich  BrandDecoMotion = ""
	BrandDecoMotionCalm  BrandDecoMotion = "calm"
	BrandDecoMotionStill BrandDecoMotion = "still"
)

// BrandDecoDensity controls how many scene elements are drawn; animation tempo is always leisurely.
type BrandDecoDensity string

const (
	BrandDecoDensityRich    BrandDecoDensity = ""
	BrandDecoDensityCalm    BrandDecoDensity = "calm"
	BrandDecoDensityMinimal BrandDecoDensity = "minimal"
)

// BrandDeco stores the window brand decoration preferences; its zero value follows
// the time of day (used when decoding existing state files where Mode was omitted).
// Fresh installs start with Mode = BrandDecoOff via DefaultAppState.
type BrandDeco struct {
	Mode     BrandDecoMode    `json:"mode,omitempty"`
	Motion   BrandDecoMotion  `json:"motion,omitempty"`
	Density  BrandDecoDensity `json:"density,omitempty"`
	PinLight string           `json:"pin_light,omitempty"`
	PinDark  string           `json:"pin_dark,omitempty"`
	HideOrb  bool             `json:"hide_orb,omitempty"`
	HideCat  bool             `json:"hide_cat,omitempty"`
	HideMark bool             `json:"hide_mark,omitempty"`
}

// EffectiveDensity resolves the element density, honoring the legacy Motion value.
func (d BrandDeco) EffectiveDensity() BrandDecoDensity {
	if d.Density == "" && d.Motion == BrandDecoMotionCalm {
		return BrandDecoDensityCalm
	}
	return d.Density
}

func DefaultAppState(defaultLang LanguageMode) AppState {
	hideAssets := true
	hideNonMD := true
	return AppState{
		Theme:                ThemeSystem,
		Language:             defaultLang,
		FontFamily:           FontSans,
		LineHeight:           LineHeightNormal,
		AssetsFolder:         "assets",
		HideAssetsFolder:     &hideAssets,
		HideNonMarkdownFiles: &hideNonMD,
		BrandDeco:            BrandDeco{Mode: BrandDecoOff},
	}
}

type FileEntry struct {
	Path       string
	Name       string // Filename with extension for non-md or folder name; display stem for .md
	RawName    string // Exact base filename on disk
	IsDir      bool
	IsMarkdown bool
	ModUnix    int64
	Size       int64
	Identity   string `json:"-"`
	Children   []FileEntry
}

type SearchHit struct {
	Path       string
	Notebook   string
	Title      string
	TitleMatch bool
	Query      string
	Line       int // 1-based source line, or 0 for a title-only match
	Column     int // 0-based byte offset within the source line
	Snippet    string
	MatchStart int
	MatchEnd   int
}

type HistorySnapshot struct {
	ID          string `json:"id"`
	NotePath    string `json:"notePath"`
	Timestamp   int64  `json:"timestamp"`
	ContentHash string `json:"contentHash"`
	Size        int    `json:"size"`
	Summary     string `json:"summary,omitempty"`
}

type DiffLineKind uint8

const (
	DiffEqual DiffLineKind = iota
	DiffDelete
	DiffInsert
)

type DiffLine struct {
	Kind    DiffLineKind
	OldLine int
	NewLine int
	Text    string
}
