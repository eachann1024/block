package icons

import (
	"fmt"
	"sync"
	"time"

	"block/assets"
	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

type Layer uint8

const (
	LayerGrid Layer = iota
	LayerFill
	LayerShade
	LayerLine
	LayerDetail
	LayerAccent
)

// Icon enumerates every Scheme A Duo two-color icon in Block.
type Icon string

const (
	Folder          Icon = "folder"
	FolderOpen      Icon = "folder-open"
	Note            Icon = "note"
	ChevronRight    Icon = "chevron-right"
	ChevronLeft     Icon = "chevron-left"
	ChevronDown     Icon = "chevron-down"
	ChevronUp       Icon = "chevron-up"
	FoldAll         Icon = "fold-all"
	UnfoldAll       Icon = "unfold-all"
	Download        Icon = "download"
	Copy            Icon = "copy"
	Check           Icon = "check"
	Plus            Icon = "plus"
	Drag            Icon = "drag"
	Code            Icon = "code"
	Heading         Icon = "heading"
	Eraser          Icon = "eraser"
	Image           Icon = "image"
	AlignLeft       Icon = "align-left"
	AlignCenter     Icon = "align-center"
	AlignRight      Icon = "align-right"
	Crop            Icon = "crop"
	Trash           Icon = "trash"
	Text            Icon = "text"
	Heading1        Icon = "heading-1"
	Heading2        Icon = "heading-2"
	Heading3        Icon = "heading-3"
	ListBullet      Icon = "list-bullet"
	ListNumbered    Icon = "list-numbered"
	Todo            Icon = "todo"
	Quote           Icon = "quote"
	Callout         Icon = "callout"
	Columns         Icon = "columns"
	Table           Icon = "table"
	Divider         Icon = "divider"
	Diagram         Icon = "diagram"
	Math            Icon = "math"
	ArrowUp         Icon = "arrow-up"
	ArrowDown       Icon = "arrow-down"
	ArrowLeft       Icon = "arrow-left"
	ArrowRight      Icon = "arrow-right"
	Wrap            Icon = "wrap"
	SelectAll       Icon = "select-all"
	Source          Icon = "source"
	Duplicate       Icon = "duplicate"
	Cut             Icon = "cut"
	NotePlus        Icon = "note-plus"
	FolderPlus      Icon = "folder-plus"
	Pencil          Icon = "pencil"
	Sparkles        Icon = "sparkles"
	Star            Icon = "star"
	Close           Icon = "close"
	More            Icon = "more"
	FindSearch      Icon = "search"
	Sliders         Icon = "sliders"
	SortFilter      Icon = "sort-filter"
	Stop            Icon = "stop"
	MessageSquare   Icon = "message-square"
	History         Icon = "history"
	AiPlus          Icon = "ai-plus"
	TbComment       Icon = "tb-comment"
	TbReaction      Icon = "tb-reaction"
	TbType          Icon = "tb-type"
	TbBold          Icon = "tb-bold"
	TbItalic        Icon = "tb-italic"
	TbUnderline     Icon = "tb-underline"
	TbStrike        Icon = "tb-strike"
	TbCode          Icon = "tb-code"
	TbPalette       Icon = "tb-palette"
	TbList          Icon = "tb-list"
	TbLink          Icon = "tb-link"
	TbChevron       Icon = "tb-chevron"
	TbChevronRight  Icon = "tb-chevron-right"
	LinkOpen        Icon = "link-open"
	LinkCopy        Icon = "link-copy"
	LinkEdit        Icon = "link-edit"
	LinkOff         Icon = "link-off"
	TbMinus         Icon = "tb-minus"
	TbPlus          Icon = "tb-plus"
	TbReset         Icon = "tb-reset"
	NoteCheck       Icon = "note-check"
	HelpCircle      Icon = "help-circle"
	PauseCircle     Icon = "pause-circle"
	FolderReveal    Icon = "folder-reveal"
	SidebarCollapse Icon = "sidebar-collapse"
	SidebarExpand   Icon = "sidebar-expand"
	ColInsertLeft   Icon = "col-insert-left"
	ColInsertRight  Icon = "col-insert-right"
	ColDelete       Icon = "col-delete"
	RowInsertAbove  Icon = "row-insert-above"
	RowInsertBelow  Icon = "row-insert-below"
	RowDelete       Icon = "row-delete"
	Minus           Icon = "minus"
	WindowMax       Icon = "window-max"
	WindowRestore   Icon = "window-restore"
	LangPlain       Icon = "lang-plain"
	LangBash        Icon = "lang-bash"
	LangCss         Icon = "lang-css"
	LangHtml        Icon = "lang-html"
	LangJs          Icon = "lang-js"
	LangJson        Icon = "lang-json"
	LangLean        Icon = "lang-lean"
	LangPython      Icon = "lang-python"
	LangRust        Icon = "lang-rust"
	LangSql         Icon = "lang-sql"
	LangTs          Icon = "lang-ts"
	LangXml         Icon = "lang-xml"
	LangYaml        Icon = "lang-yaml"
	TabClose        Icon = "tab-close"
	TabCloseOthers  Icon = "tab-close-others"
	TabCloseRight   Icon = "tab-close-right"
	TabCloseAll     Icon = "tab-close-all"
	AiPolish        Icon = "ai-polish"
	AiSimplify      Icon = "ai-simplify"
	AiTransEn       Icon = "ai-trans-en"
	AiTransZh       Icon = "ai-trans-zh"
	AiJargon        Icon = "ai-jargon"
	AiColloquial    Icon = "ai-colloquial"
	AiList          Icon = "ai-list"
	AiTasks         Icon = "ai-tasks"
	ProvOpenai      Icon = "prov-openai"
	ProvAnthropic   Icon = "prov-anthropic"
	ProvDeepseek    Icon = "prov-deepseek"
	Github          Icon = "github"
	KeyCommand      Icon = "key-command"
	KeyOption       Icon = "key-option"
	KeyReturn       Icon = "key-return"
	KeyShift        Icon = "key-shift"
	KeyControl      Icon = "key-control"
	KeyBackspace    Icon = "key-backspace"
	KeyPlus         Icon = "key-plus"
	Refresh         Icon = "refresh"
	Spinner         Icon = "spinner"
	UpdateTangle    Icon = "update-tangle"
	ToolThink       Icon = "tool-think"
	ToolWrite       Icon = "tool-write"
	ToolRun         Icon = "tool-run"
	ToolRead        Icon = "tool-read"
	Upload          Icon = "upload"
	Eye             Icon = "eye"
	EyeOff          Icon = "eye-off"
	Send            Icon = "send"
	Plug            Icon = "plug"
	Package         Icon = "package"
	Zap             Icon = "zap"
	Video           Icon = "video"
	Paperclip       Icon = "paperclip"
	ShieldCheck     Icon = "shield-check"
	FilePdf         Icon = "file-pdf"
	FileWord        Icon = "file-word"
	FileSheet       Icon = "file-sheet"
	FileSlides      Icon = "file-slides"
	FileLog         Icon = "file-log"
	FileBook        Icon = "file-book"
	LangReact       Icon = "lang-react"
	LangGo          Icon = "lang-go"
	LangJava        Icon = "lang-java"
	LangKotlin      Icon = "lang-kotlin"
	LangSwift       Icon = "lang-swift"
	LangC           Icon = "lang-c"
	LangCpp         Icon = "lang-cpp"
	LangCsharp      Icon = "lang-csharp"
	LangRuby        Icon = "lang-ruby"
	LangPhp         Icon = "lang-php"
	LangLua         Icon = "lang-lua"
	LangVue         Icon = "lang-vue"
	FileConfig      Icon = "file-config"
	FileEnv         Icon = "file-env"
	FileLock        Icon = "file-lock"
	FileSvg         Icon = "file-svg"
	FileAudio       Icon = "file-audio"
	FileFont        Icon = "file-font"
	FileArchive     Icon = "file-archive"
	FileBinary      Icon = "file-binary"
	FileReadme      Icon = "file-readme"
	FileLicense     Icon = "file-license"
	FileDocker      Icon = "file-docker"
	FileGit         Icon = "file-git"
	Wrench          Icon = "wrench"
	Tag             Icon = "tag"
	FolderSrc       Icon = "folder-src"
	FolderDocs      Icon = "folder-docs"
	FolderAssets    Icon = "folder-assets"
	FolderTests     Icon = "folder-tests"
	FolderGit       Icon = "folder-git"
	FolderDeps      Icon = "folder-deps"
	Clock           Icon = "clock"
	SkipForward     Icon = "skip-forward"
	Maximize        Icon = "maximize"

	// Settings sidebar categories: one 24 grid, 1.5 stroke, optical centre on (12, 12).
	SettingsAppearance Icon = "settings-appearance"
	SettingsAI         Icon = "settings-ai"
	SettingsFolder     Icon = "settings-folder"
	SettingsShortcuts  Icon = "settings-shortcuts"
)

type MascotState string

const (
	MascotNoFolder    MascotState = "no-folder"
	MascotEmptyFolder MascotState = "empty-folder"
	MascotNoNote      MascotState = "no-note"
	MascotReadError   MascotState = "read-error"
	MascotAIDisabled  MascotState = "ai-disabled"
	MascotAIGreeting  MascotState = "ai-greeting"
)

type iconLayers struct {
	fill   *ui.SVG
	shade  *ui.SVG
	line   *ui.SVG
	detail *ui.SVG
	accent *ui.SVG
}

var (
	svgMu      sync.RWMutex
	svgCache   = make(map[string]*ui.SVG)
	layerMu    sync.RWMutex
	layerCache = make(map[Icon]iconLayers)
)

func loadSVG(rel string) *ui.SVG {
	svgMu.RLock()
	cached, ok := svgCache[rel]
	svgMu.RUnlock()
	if ok {
		return cached
	}
	raw, err := assets.Read(rel)
	var parsed *ui.SVG
	if err == nil && len(raw) > 0 {
		parsed, _ = ui.ParseSVG(raw)
	}
	svgMu.Lock()
	svgCache[rel] = parsed
	svgMu.Unlock()
	return parsed
}

func loadIconLayers(glyph Icon) iconLayers {
	layerMu.RLock()
	cached, ok := layerCache[glyph]
	layerMu.RUnlock()
	if ok {
		return cached
	}
	prefix := "icons/" + string(glyph)
	layers := iconLayers{
		fill:   loadSVG(prefix + ".fill.svg"),
		shade:  loadSVG(prefix + ".shade.svg"),
		line:   loadSVG(prefix + ".line.svg"),
		detail: loadSVG(prefix + ".detail.svg"),
		accent: loadSVG(prefix + ".accent.svg"),
	}
	layerMu.Lock()
	layerCache[glyph] = layers
	layerMu.Unlock()
	return layers
}

// LineSVG returns the single-layer .line.svg for native menus or single-color icons,
// falling back to .accent.svg for solid brand marks that carry no stroke layer.
func LineSVG(glyph Icon) *ui.SVG {
	layers := loadIconLayers(glyph)
	if layers.line != nil {
		return layers.line
	}
	return layers.accent
}

// AgentBrandSVG returns the SVG logo for an AI agent backend.
func AgentBrandSVG(kind model.BackendKind) *ui.SVG {
	return loadSVG(kind.AssetPath())
}

// Color-only staggered layer transitions: geometry never moves.
// Lighting up staggers fill -> shade (+40ms) -> detail (+80ms) -> accent (+120ms);
// extinguishing returns all layers together with zero delay.
var (
	iconColorEase = ui.CubicBezier(0.25, 0.1, 0.25, 1)

	IconFadeOut = ui.Animation{
		Duration:  200 * time.Millisecond,
		Ease:      iconColorEase,
		Oklab:     true,
		NonMotion: true,
	}
	IconLitFill = ui.Animation{
		Duration:  200 * time.Millisecond,
		Ease:      iconColorEase,
		Oklab:     true,
		NonMotion: true,
	}
	IconLitShade = ui.Animation{
		Duration:  200 * time.Millisecond,
		Delay:     40 * time.Millisecond,
		Ease:      iconColorEase,
		Oklab:     true,
		NonMotion: true,
	}
	IconLitDetail = ui.Animation{
		Duration:  200 * time.Millisecond,
		Delay:     80 * time.Millisecond,
		Ease:      iconColorEase,
		Oklab:     true,
		NonMotion: true,
	}
	IconLitAccent = ui.Animation{
		Duration:  200 * time.Millisecond,
		Delay:     120 * time.Millisecond,
		Ease:      iconColorEase,
		Oklab:     true,
		NonMotion: true,
	}
)

// Draw stacks the 5 SVG layers (.fill, .shade, .line, .detail, .accent) of an icon.
func Draw(c *ui.Context, glyph Icon, size float32, colors theme.IconColors) ui.Element {
	if size <= 0 {
		return ui.Box(c).Size(0, 0).Shrink(0)
	}
	layers := loadIconLayers(glyph)
	root := ui.Box(c).Size(size, size).Shrink(0)
	root.Children(func() {
		if colors.Fill.A > 0 && layers.fill != nil {
			ui.Icon(c, layers.fill).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Fill)
		}
		if colors.Shade.A > 0 && layers.shade != nil {
			ui.Icon(c, layers.shade).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Shade)
		}
		if colors.Line.A > 0 && layers.line != nil {
			ui.Icon(c, layers.line).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Line)
		}
		if colors.Detail.A > 0 && layers.detail != nil {
			ui.Icon(c, layers.detail).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Detail)
		}
		if colors.Accent.A > 0 && layers.accent != nil {
			ui.Icon(c, layers.accent).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Accent)
		}
	})
	return root
}

// DrawAnimated renders a 5-layer icon whose geometry stays fixed while its semantic
// layers light up in staggered Ocean-blue order on active/hover and fade back together.
func DrawAnimated(c *ui.Context, glyph Icon, size float32, colors theme.IconColors, active bool) ui.Element {
	if size <= 0 {
		return ui.Box(c).Size(0, 0).Shrink(0)
	}
	layers := loadIconLayers(glyph)
	root := ui.Box(c).Size(size, size).Shrink(0)

	fillTarget, shadeTarget, lineTarget, detailTarget, accTarget := colors.Fill, colors.Shade, colors.Line, colors.Detail, colors.Accent
	fillAnim, shadeAnim, lineAnim, detailAnim, accAnim := IconFadeOut, IconFadeOut, IconFadeOut, IconFadeOut, IconFadeOut
	if active && colors.Lit {
		fillTarget = colors.LitFill
		shadeTarget = colors.LitShade
		detailTarget = colors.LitDetail
		accTarget = colors.LitAccent
		fillAnim = IconLitFill
		shadeAnim = IconLitShade
		detailAnim = IconLitDetail
		accAnim = IconLitAccent
	}

	var fillCol, shadeCol, lineCol, detailCol, accCol ui.Color
	if layers.fill != nil {
		fillCol = root.AnimateColor("icoFill", fillTarget, fillAnim)
	}
	if layers.shade != nil {
		shadeCol = root.AnimateColor("icoShade", shadeTarget, shadeAnim)
	}
	if layers.line != nil {
		lineCol = root.AnimateColor("icoLine", lineTarget, lineAnim)
	}
	if layers.detail != nil {
		detailCol = root.AnimateColor("icoDetail", detailTarget, detailAnim)
	}
	if layers.accent != nil {
		accCol = root.AnimateColor("icoAcc", accTarget, accAnim)
	}

	root.Children(func() {
		if fillCol.A > 0 && layers.fill != nil {
			ui.Icon(c, layers.fill).Absolute().Top(0).Left(0).Size(size, size).TextColor(fillCol)
		}
		if shadeCol.A > 0 && layers.shade != nil {
			ui.Icon(c, layers.shade).Absolute().Top(0).Left(0).Size(size, size).TextColor(shadeCol)
		}
		if lineCol.A > 0 && layers.line != nil {
			ui.Icon(c, layers.line).Absolute().Top(0).Left(0).Size(size, size).TextColor(lineCol)
		}
		if detailCol.A > 0 && layers.detail != nil {
			ui.Icon(c, layers.detail).Absolute().Top(0).Left(0).Size(size, size).TextColor(detailCol)
		}
		if accCol.A > 0 && layers.accent != nil {
			ui.Icon(c, layers.accent).Absolute().Top(0).Left(0).Size(size, size).TextColor(accCol)
		}
	})
	return root
}

// Duo renders a 5-layer icon using Ocean Paper's neutral resting depth and
// staggered Ocean-blue color lighting when active.
func Duo(c *ui.Context, glyph Icon, size float32, active bool) ui.Element {
	pal := theme.Ocean()
	if active {
		return DrawAnimated(c, glyph, size, pal.DuoIconColors(pal.Text), true)
	}
	return DrawAnimated(c, glyph, size, pal.DuoIconColors(pal.TextSecondary), false)
}

// Brand renders an AI backend brand mark at `size`.
func Brand(c *ui.Context, kind model.BackendKind, size float32, tint ui.Color) ui.Element {
	if size <= 0 {
		return ui.Box(c).Size(0, 0).Shrink(0)
	}
	s := AgentBrandSVG(kind)
	if s == nil {
		return Duo(c, Sparkles, size, true)
	}
	return ui.Icon(c, s).Size(size, size).Shrink(0).TextColor(tint)
}

type mascotRole uint8

const (
	mascotFill  mascotRole = iota // soft tinted surface (pal.Selected)
	mascotSoft                    // secondary strokes (pal.IconFill)
	mascotPaper                   // occluding paper (pal.Paper)
	mascotLine                    // primary accent strokes (pal.Accent)
)

// mascotLayers lists each illustration's layers back to front; layer i is
// assets/mascot/<state>.<i+1>.svg drawn in black and tinted by its role.
var mascotLayers = map[MascotState][]mascotRole{
	MascotNoNote:      {mascotFill, mascotSoft, mascotPaper, mascotSoft, mascotLine},
	MascotEmptyFolder: {mascotPaper, mascotSoft, mascotFill, mascotSoft, mascotPaper, mascotLine, mascotSoft},
	MascotNoFolder:    {mascotFill, mascotSoft, mascotLine, mascotPaper},
	MascotReadError:   {mascotFill, mascotSoft, mascotLine, mascotPaper, mascotLine},
	MascotAIGreeting:  {mascotFill, mascotLine, mascotPaper, mascotLine},
	MascotAIDisabled:  {mascotFill, mascotLine, mascotSoft, mascotPaper, mascotLine},
}

// Mascot renders a small square line illustration for an empty state at size px.
func Mascot(c *ui.Context, state MascotState, size float32) ui.Element {
	size = max(size, 1)
	pal := theme.Ocean()
	root := ui.Box(c).Size(size, size).Shrink(0)
	root.Children(func() {
		for i, role := range mascotLayers[state] {
			col := pal.Accent
			switch role {
			case mascotFill:
				col = pal.Selected
			case mascotSoft:
				col = pal.IconFill
			case mascotPaper:
				col = pal.Paper
			}
			if s := loadSVG(fmt.Sprintf("mascot/%s.%d.svg", state, i+1)); s != nil {
				ui.Icon(c, s).Absolute().Top(0).Left(0).Size(size, size).TextColor(col)
			}
		}
	})
	return root
}
