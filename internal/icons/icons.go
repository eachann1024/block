package icons

import (
	"fmt"
	"math"
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

var iconMotion = ui.Animation{
	Duration: 100 * time.Millisecond,
	Ease:     ui.CubicBezier(0.2, 0.8, 0.25, 1),
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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

func isFolderIcon(glyph Icon) bool {
	switch glyph {
	case Folder, FolderOpen, FolderPlus,
		FolderSrc, FolderDocs, FolderAssets, FolderTests, FolderGit, FolderDeps:
		return true
	}
	return false
}

// DrawAnimated gives semantic icon layers short hover motion with immediate color feedback.
func DrawAnimated(c *ui.Context, glyph Icon, size float32, colors theme.IconColors, active bool) ui.Element {
	if size <= 0 {
		return ui.Box(c).Size(0, 0).Shrink(0)
	}
	root := ui.Box(c).Size(size, size).Shrink(0)
	targetP := float32(0)
	if active {
		targetP = 1
	}
	p := root.AnimateTo("icoAct", targetP, iconMotion)
	fillCol, lineCol, accCol := colors.Fill, colors.Line, colors.Accent

	moving := float32(math.Abs(float64(p))) > 0.002
	k := size / 20.0

	if moving {
		switch glyph {
		case Folder, FolderOpen, FolderPlus,
			FolderSrc, FolderDocs, FolderAssets, FolderTests, FolderGit, FolderDeps:
			root.Translate(0, -1.2*k*p)
		case FolderReveal:
			sc := 1 + 0.05*p
			root.Scale(sc, sc)
		case Plus:
			sc := 1 + 0.15*p
			root.Rotate(90*p).Scale(sc, sc)
		case Close:
			sc := 1 + 0.06*p
			root.Rotate(90*p).Scale(sc, sc)
		case Check:
			sc := 1 + 0.12*p
			root.Translate(0, -0.7*k*p).Scale(sc, sc)
		case Note, NoteCheck:
			root.Translate(0, -1.3*k*p).Rotate(-2.5*p)
		case Package:
			root.Translate(0, -1.4*k*p)
		case Send:
			root.Translate(2.0*k*p, -1.8*k*p).Rotate(-5*p)
		case Zap:
			sc := 1 + 0.14*p
			root.Translate(0.5*k*p, -1.1*k*p).Rotate(-8*p).Scale(sc, sc)
		case ChevronDown, TbChevron:
			sc := 1 + 0.13*p
			root.Translate(0, 2.2*k*p).Scale(sc, sc)
		case ChevronRight, TbChevronRight:
			sc := 1 + 0.13*p
			root.Translate(2.2*k*p, 0).Scale(sc, sc)
		case ChevronUp:
			sc := 1 + 0.13*p
			root.Translate(0, -2.2*k*p).Scale(sc, sc)
		case ChevronLeft:
			sc := 1 + 0.13*p
			root.Translate(-2.2*k*p, 0).Scale(sc, sc)
		case Refresh:
			root.Rotate(-45 * p)
		case TbReset, UpdateTangle:
			root.Rotate(-42 * p)
		case Trash:
			root.Translate(0, -0.9*k*p).Rotate(-9*p)
		case Pencil, Eraser, Cut:
			root.Rotate(-6 * p)
		case Drag:
			sc := 1 + 0.12*p
			root.Translate(0, -1.0*k*p).Scale(sc, sc)
		case Clock:
			sc := 1 + 0.05*p
			root.Scale(sc, sc)
		case SkipForward:
			// Triangle layers shift right toward the vertical bar.
		case Maximize:
			sc := 1 + 0.14*p
			root.Scale(sc, sc)
		case TabClose, TabCloseAll, TabCloseOthers, TabCloseRight:
			root.Translate(0, -0.7*k*p)
		case Copy, Duplicate, LinkCopy:
			sc := 1 + 0.05*p
			root.Translate(0.8*k*p, -0.8*k*p).Scale(sc, sc)
		case LinkEdit:
			root.Translate(0, -0.6*k*p).Rotate(-6*p)
		case LinkOff:
			sc := 1 + 0.05*p
			root.Rotate(8*p).Scale(sc, sc)
		case Wrench, Tag, Paperclip:
			root.Translate(0, -0.8*k*p).Rotate(-9*p)
		case More:
			sc := 1 + 0.08*p
			root.Scale(sc, sc)
		case Sliders, SortFilter:
			sc := 1 + 0.06*p
			root.Translate(0, -0.8*k*p).Scale(sc, sc)
		case FindSearch:
			root.Translate(-0.6*k*p, -0.6*k*p).Rotate(-6*p)
		case TbPlus:
			sc := 1 + 0.10*p
			root.Rotate(90*p).Scale(sc, sc)
		case TbMinus, Minus:
			sc := 1 + 0.10*p
			root.Scale(sc, sc)
		case ColDelete, RowDelete:
			root.Translate(0, -0.8*k*p).Rotate(-5*p)
		case ColInsertLeft, ColInsertRight, RowInsertAbove, RowInsertBelow,
			WindowMax, WindowRestore, Crop, SelectAll, History,
			Eye, EyeOff, Stop, NotePlus, AiPlus:
			sc := 1 + 0.05*p
			root.Scale(sc, sc)
		case Download, Upload, ArrowUp, ArrowDown, ArrowLeft, ArrowRight,
			LinkOpen, Plug, SidebarExpand, SidebarCollapse,
			Sparkles, AiPolish, Star:
			// Bespoke layer-level transforms handle motion below.
		default:
			sc := 1 + 0.05*p
			root.Translate(0, -1.0*k*p).Scale(sc, sc)
		}
	}

	root.Children(func() {
		if isFolderIcon(glyph) {
			if lineCol.A > 0 {
				if line := loadSVG("icons/" + string(glyph) + ".line.svg"); line != nil {
					ui.Icon(c, line).Absolute().Top(0).Left(0).Size(size, size).TextColor(lineCol)
				}
			}
			if moving && p > 0.01 {
				pal := theme.Ocean()
				sheet := ui.Box(c).Absolute().
					Left(size*0.27).Top(size*0.35).
					Size(size*0.46, size*0.26).
					Radius(max(1, 1.2*k)).
					Background(pal.Paper).
					Border(1, accCol).
					Opacity(clamp01(p * 1.15))
				sheet.Translate(0, (1-p)*2.4*k).Rotate(-3 * p)
			}
			if fillCol.A > 0 {
				if fill := loadSVG("icons/" + string(glyph) + ".fill.svg"); fill != nil {
					fl := ui.Icon(c, fill).Absolute().Top(0).Left(0).Size(size, size).TextColor(fillCol)
					if moving {
						fl.TransformOrigin(0.5, 0.85).Skew(-8.5*p, 0).Scale(1, max(0.2, 1-0.08*p))
					}
				}
			}
			if accCol.A > 0 {
				if acc := loadSVG("icons/" + string(glyph) + ".accent.svg"); acc != nil {
					ac := ui.Icon(c, acc).Absolute().Top(0).Left(0).Size(size, size).TextColor(accCol)
					if moving {
						ac.TransformOrigin(0.5, 0.85).Skew(-8.5*p, 0).Scale(1, max(0.2, 1-0.08*p))
					}
				}
			}
			return
		}

		if fillCol.A > 0 {
			if fill := loadSVG("icons/" + string(glyph) + ".fill.svg"); fill != nil {
				fl := ui.Icon(c, fill).Absolute().Top(0).Left(0).Size(size, size).TextColor(fillCol)
				if moving {
					switch glyph {
					case SidebarExpand, SidebarCollapse:
						fl.TransformOrigin(0.2, 0.5).Scale(0.88+0.12*clamp01(p), 1)
					case Sparkles, AiPolish, Star:
						sc := 1 + 0.07*p
						fl.Rotate(15*p).Scale(sc, sc)
					case Eye:
						sc := 1 + 0.05*p
						fl.Scale(sc, sc)
					case Stop:
						sc := 1 - 0.08*p
						fl.Scale(sc, sc)
					case Plug:
						fl.Translate(1.2*k*p, -1.2*k*p)
					case SkipForward:
						fl.Translate(2.0*k*p, 0)
					}
				}
			}
		}
		if lineCol.A > 0 {
			if line := loadSVG("icons/" + string(glyph) + ".line.svg"); line != nil {
				ln := ui.Icon(c, line).Absolute().Top(0).Left(0).Size(size, size).TextColor(lineCol)
				if moving {
					switch glyph {
					case LinkOpen:
						ln.Rotate(-3 * p)
					case Sparkles, AiPolish, Star:
						sc := 1 + 0.07*p
						ln.Rotate(15*p).Scale(sc, sc)
					case Download:
						ln.Translate(0, 2.0*k*p)
					case Upload:
						ln.Translate(0, -2.0*k*p)
					case Plug:
						ln.Translate(1.2*k*p, -1.2*k*p)
					case ArrowRight:
						ln.Translate(1.6*k*p, 0)
					case ArrowLeft:
						ln.Translate(-1.6*k*p, 0)
					case ArrowDown:
						ln.Translate(0, 1.4*k*p)
					case ArrowUp:
						ln.Translate(0, -1.4*k*p)
					case SkipForward:
						ln.Translate(2.0*k*p, 0)
					}
				}
			}
		}
		if accCol.A > 0 {
			if acc := loadSVG("icons/" + string(glyph) + ".accent.svg"); acc != nil {
				ac := ui.Icon(c, acc).Absolute().Top(0).Left(0).Size(size, size).TextColor(accCol)
				if moving {
					switch glyph {
					case Plus, Close, Check, Zap, ChevronDown, ChevronRight, ChevronUp, ChevronLeft, TbChevron, TbChevronRight, Drag, Refresh, TbReset, UpdateTangle, Trash, Pencil, Eraser, Cut,
						Copy, Duplicate, LinkCopy, LinkEdit, LinkOff, Wrench, Tag, Paperclip, TbPlus, TbMinus, Minus, ColDelete, RowDelete:
						// Unified root transform already applied in sub-pixel lockstep with .line.
					case Clock:
						ac.Rotate(45 * p)
					case SkipForward:
						ac.Scale(1+0.08*p, 1+0.08*p)
					case Maximize:
						sc := 1 + 0.06*p
						ac.Scale(sc, sc)
					case TabClose:
						ac.Rotate(90 * p)
					case TabCloseAll:
						ac.TransformOrigin(10.5/24.0, 13.0/24.0).Rotate(90 * p)
					case TabCloseRight:
						ac.Translate(1.8*k*p, 0)
					case TabCloseOthers:
						sc := 1 + 0.12*p
						ac.Scale(sc, sc)
					case More:
						sc := 1 + 0.15*p
						ac.Scale(sc, sc)
					case Download, Upload:
						ac.Scale(1+0.08*p, 1)
					case Eye:
						sc := 1 + 0.20*p
						ac.Scale(sc, sc)
					case EyeOff:
						sc := 1 + 0.07*p
						ac.Translate(1.0*k*p, 1.0*k*p).Scale(sc, sc)
					case Send:
						sc := 1 + 0.08*p
						ac.Translate(0.9*k*p, -0.9*k*p).Scale(sc, sc)
					case Plug:
						sc := 1 + 0.12*p
						ac.Scale(sc, sc)
					case Package:
						sc := 1 + 0.10*p
						ac.Scale(sc, sc)
					case Stop:
						sc := 1 + 0.12*p
						ac.Scale(sc, sc)
					case Note, NoteCheck:
						ac.Translate(1.2*k*p, 0)
					case SidebarExpand:
						sc := 1 + 0.08*p
						ac.Translate(2.5*k*p, 0).Scale(sc, sc)
					case SidebarCollapse:
						sc := 1 + 0.08*p
						ac.Translate(-2.5*k*p, 0).Scale(sc, sc)
					case FolderReveal:
						sc := 1 + 0.08*p
						ac.Translate(2.6*k*p, 0).Scale(sc, sc)
					case LinkOpen:
						sc := 1 + 0.08*p
						ac.Translate(2.6*k*p, -2.6*k*p).Scale(sc, sc)
					case Sparkles, AiPolish, Star:
						sc := 1 + 0.20*p
						ac.Rotate(-14*p).Scale(sc, sc)
					case Sliders:
						sc := 1 + 0.08*p
						ac.Scale(sc, sc)
					case NotePlus, AiPlus:
						sc := 1 + 0.16*p
						ac.Scale(sc, sc)
					case History:
						ac.Rotate(-22 * p)
					case FindSearch:
						sc := 1 + 0.08*p
						ac.Translate(1.2*k*p, 1.2*k*p).Scale(sc, sc)
					case ArrowDown, RowInsertBelow:
						ac.Translate(0, 2.0*k*p)
					case ArrowUp, RowInsertAbove:
						ac.Translate(0, -2.0*k*p)
					case ArrowRight, ColInsertRight:
						ac.Translate(1.8*k*p, 0)
					case ArrowLeft, ColInsertLeft:
						ac.Translate(-1.8*k*p, 0)
					case WindowMax, WindowRestore, Crop, SelectAll:
						sc := 1 + 0.11*p
						ac.Scale(sc, sc)
					default:
						sc := 1 + 0.07*p
						ac.Scale(sc, sc)
					}
				}
			}
		}
	})
	return root
}

// Duo renders a Scheme A Duo icon using Ocean Paper's standard active/hovered or idle tints
// with short movement on its semantic layers.
func Duo(c *ui.Context, glyph Icon, size float32, active bool) ui.Element {
	pal := theme.Ocean()
	if active {
		return DrawAnimated(c, glyph, size, pal.DuoIconColors(pal.Text), true)
	}
	return DrawAnimated(c, glyph, size, pal.MonoIconColors(pal.TextSecondary), false)
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
