package icons

import (
	"sync"

	"block/assets"
	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

type Layer uint8

const (
	LayerGrid Layer = iota
	LayerFill
	LayerLine
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
	KeyCommand      Icon = "key-command"
	KeyOption       Icon = "key-option"
	KeyReturn       Icon = "key-return"
	KeyShift        Icon = "key-shift"
	KeyControl      Icon = "key-control"
	KeyBackspace    Icon = "key-backspace"
	Refresh         Icon = "refresh"
	Spinner         Icon = "spinner"
	UpdateTangle    Icon = "update-tangle"
	ToolThink       Icon = "tool-think"
	ToolWrite       Icon = "tool-write"
	ToolRun         Icon = "tool-run"
	ToolRead        Icon = "tool-read"
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

var (
	svgMu    sync.RWMutex
	svgCache = make(map[string]*ui.SVG)
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

// LineSVG returns the single-layer .line.svg for native menus or single-color icons.
func LineSVG(glyph Icon) *ui.SVG {
	return loadSVG("icons/" + string(glyph) + ".line.svg")
}

// AgentBrandSVG returns the SVG logo for an AI agent backend.
func AgentBrandSVG(kind model.BackendKind) *ui.SVG {
	return loadSVG(kind.AssetPath())
}

// Draw stacks the .fill, .line, and .accent SVG masks of a Scheme A Duo icon.
func Draw(c *ui.Context, glyph Icon, size float32, colors theme.IconColors) ui.Element {
	if size <= 0 {
		return ui.Box(c).Size(0, 0).Shrink(0)
	}
	root := ui.Box(c).Size(size, size).Shrink(0)
	root.Children(func() {
		if colors.Fill.A > 0 {
			if fill := loadSVG("icons/" + string(glyph) + ".fill.svg"); fill != nil {
				ui.Icon(c, fill).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Fill)
			}
		}
		if colors.Line.A > 0 {
			if line := loadSVG("icons/" + string(glyph) + ".line.svg"); line != nil {
				ui.Icon(c, line).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Line)
			}
		}
		if colors.Accent.A > 0 {
			if acc := loadSVG("icons/" + string(glyph) + ".accent.svg"); acc != nil {
				ui.Icon(c, acc).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Accent)
			}
		}
	})
	return root
}

// Duo renders a Scheme A Duo icon using Ocean Paper's standard active/hovered or idle tints.
func Duo(c *ui.Context, glyph Icon, size float32, active bool) ui.Element {
	pal := theme.Ocean()
	if active {
		return Draw(c, glyph, size, pal.DuoIconColors(pal.Text))
	}
	return Draw(c, glyph, size, pal.MonoIconColors(pal.TextSecondary))
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

// Mascot renders the 4-layer Scheme C Paper & Blueprint vector illustration (280x220 aspect).
func Mascot(c *ui.Context, state MascotState, maxWidth float32) ui.Element {
	const fullW float32 = 280
	const fullH float32 = 220
	w := fullW
	if maxWidth > 0 && maxWidth < fullW {
		w = max(maxWidth, 1)
	}
	h := fullH * w / fullW
	pal := theme.Ocean()
	slug := string(state)
	root := ui.Box(c).Size(w, h).Shrink(0)
	root.Children(func() {
		layers := []struct {
			ext   string
			color ui.Color
		}{
			{".grid.svg", pal.Hairline},
			{".fill.svg", pal.IconFill},
			{".line.svg", pal.Text},
			{".accent.svg", pal.Accent},
		}
		for _, l := range layers {
			if s := loadSVG("mascot/" + slug + l.ext); s != nil {
				ui.Icon(c, s).Absolute().Top(0).Left(0).Size(w, h).TextColor(l.color)
			}
		}
	})
	return root
}
