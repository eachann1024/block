package mindmap

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"block/internal/icons"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// NoteCandidate represents a notebook file selectable in the Link-to-Note picker.
type NoteCandidate struct {
	Title   string
	RelPath string
}

// Callbacks bridges mindmap block actions back to the host editor and workspace.
type Callbacks struct {
	OnChange         func(newMarkdown string)
	OnUndo           func()
	OnRedo           func()
	OnOpenLink       func(target string)
	OnListNotes      func() []NoteCandidate
	OnPickImportFile func(onImported func(root *Node, report ImportReport))
	OnSaveExportFile func(defaultName, ext string, data []byte, onDone func(err error))
	OnNotify         func(msg, tone string)
	OnRequestFocus   func()
	ReadOnly         bool
}

// BlockState keeps all interactive runtime state for one mindmap block.
type BlockState struct {
	Root        *Node
	NextID      int
	LastSource  string
	SelectedID  int
	HoveredID   int
	View        ViewMode
	Layout      LayoutMode
	LayoutSet   bool
	Fullscreen  bool

	Camera         Camera
	CameraInit     bool
	AutoFitPending bool
	LastCanvasW    float32
	LastCanvasH    float32
	Measure        MeasureCache

	Panning  bool
	PanLastX float32
	PanLastY float32

	EditingID        int
	EditDraft        string
	EditFresh        bool
	EditFocusPending bool
	EditHandle       ui.Handle

	LinkNodeID       int
	LinkDraft        string
	LinkFocusPending bool
	LinkHandle       ui.Handle

	ExportMenuOpen bool
	ImportResult   *ImportReport

	Undo [][]byte
	Redo [][]byte

	CanvasHandle  ui.Handle
	HintScroll    ui.ScrollState
	OutlineScroll ui.ScrollState
	SourceScroll  ui.ScrollState
}

// NewBlockState initializes a new BlockState with defaults from cfg.
func NewBlockState(cfg Config) *BlockState {
	lay := cfg.DefaultLayout
	if lay != LayoutBoth {
		lay = LayoutRight
	}
	return &BlockState{
		View:           ViewMap,
		Layout:         lay,
		AutoFitPending: true,
	}
}

// SyncSource reconciles the in-memory tree with the underlying block Markdown source.
func (st *BlockState) SyncSource(src string, cfg Config) {
	if !st.LayoutSet && cfg.DefaultLayout != "" {
		st.Layout = cfg.DefaultLayout
	}
	if st.Root != nil && st.LastSource == src {
		return
	}
	foldPaths := captureFoldPaths(st.Root)
	root := ParseMarkdown(src)
	restoreFoldPaths(root, foldPaths)
	st.Root = root
	st.NextID = maxNodeID(root)
	st.LastSource = src
	// External source updates (Source tab, Editor Undo/Redo, external reload) supersede local snapshots.
	st.Undo = st.Undo[:0]
	st.Redo = st.Redo[:0]
	if st.Root != nil && st.Root.FindByID(st.SelectedID) == nil {
		st.SelectedID = st.Root.ID
	}
	st.AutoFitPending = true
}

// HasFocus reports whether any interactive MindMap control currently owns keyboard focus.
func (st *BlockState) HasFocus(c *ui.Context) bool {
	if st == nil || c == nil {
		return false
	}
	return st.CanvasHandle.Focused(c) || st.EditHandle.Focused(c) || st.LinkHandle.Focused(c)
}

func maxNodeID(n *Node) int {
	if n == nil {
		return 0
	}
	m := n.ID
	for _, ch := range n.Children {
		if cm := maxNodeID(ch); cm > m {
			m = cm
		}
	}
	return m
}

func (st *BlockState) selectedNode() *Node {
	if st.Root == nil {
		return nil
	}
	if n := st.Root.FindByID(st.SelectedID); n != nil {
		return n
	}
	st.SelectedID = st.Root.ID
	return st.Root
}

func (st *BlockState) pushUndo() {
	if st.Root == nil {
		return
	}
	st.Undo = append(st.Undo, marshalSnapshot(st.Root))
	if len(st.Undo) > 64 {
		st.Undo = st.Undo[len(st.Undo)-64:]
	}
	st.Redo = st.Redo[:0]
}

func (st *BlockState) commitChange(cb Callbacks) {
	if st.Root == nil {
		return
	}
	md := SerializeMarkdown(st.Root)
	st.LastSource = md
	if cb.OnChange != nil {
		cb.OnChange(md)
	}
}

func (st *BlockState) allocID() int {
	st.NextID++
	return st.NextID
}

// --- Core CRUD Operations ---

func (st *BlockState) opAddChild(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	n := st.selectedNode()
	if n == nil {
		return
	}
	st.pushUndo()
	n.Folded = false
	child := &Node{
		ID:     st.allocID(),
		Text:   theme.Tr("新节点", "New Topic"),
		Parent: n,
	}
	n.Children = append(n.Children, child)
	st.SelectedID = child.ID
	st.commitChange(cb)
	st.beginEdit(child, true)
}

func (st *BlockState) opAddSibling(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	n := st.selectedNode()
	if n == nil {
		return
	}
	if n.Parent == nil {
		st.opAddChild(cb)
		return
	}
	st.pushUndo()
	idx := n.IndexInParent()
	sib := &Node{
		ID:     st.allocID(),
		Text:   theme.Tr("新节点", "New Topic"),
		Parent: n.Parent,
	}
	kids := n.Parent.Children
	kids = append(kids, nil)
	copy(kids[idx+2:], kids[idx+1:])
	kids[idx+1] = sib
	n.Parent.Children = kids
	st.SelectedID = sib.ID
	st.commitChange(cb)
	st.beginEdit(sib, true)
}

func (st *BlockState) opDelete(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	n := st.selectedNode()
	if n == nil || n.Parent == nil {
		return
	}
	st.pushUndo()
	parent := n.Parent
	idx := n.IndexInParent()
	parent.Children = append(parent.Children[:idx], parent.Children[idx+1:]...)
	if idx < len(parent.Children) {
		st.SelectedID = parent.Children[idx].ID
	} else if idx-1 >= 0 {
		st.SelectedID = parent.Children[idx-1].ID
	} else {
		st.SelectedID = parent.ID
	}
	st.commitChange(cb)
}

func (st *BlockState) opToggleFold() {
	n := st.selectedNode()
	if n == nil || len(n.Children) == 0 {
		return
	}
	n.Folded = !n.Folded
}

func (st *BlockState) opMoveSibling(delta int, cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	n := st.selectedNode()
	if n == nil || n.Parent == nil {
		return
	}
	kids := n.Parent.Children
	i := n.IndexInParent()
	j := i + delta
	if i < 0 || j < 0 || j >= len(kids) {
		return
	}
	st.pushUndo()
	kids[i], kids[j] = kids[j], kids[i]
	st.commitChange(cb)
}

func (st *BlockState) opToggleTask(n *Node, cb Callbacks) {
	if cb.ReadOnly || n == nil || n.Parent == nil {
		return
	}
	st.pushUndo()
	if n.Task == nil {
		n.Task = BoolPtr(false)
	} else {
		n.Task = nil
	}
	st.commitChange(cb)
}

func (st *BlockState) opFlipTaskDone(n *Node, cb Callbacks) {
	if cb.ReadOnly || n == nil || n.Task == nil {
		return
	}
	st.pushUndo()
	next := !*n.Task
	n.Task = &next
	st.SelectedID = n.ID
	st.commitChange(cb)
}

func (st *BlockState) opUndo(cb Callbacks) {
	if cb.ReadOnly {
		return
	}
	if cb.OnUndo != nil {
		cb.OnUndo()
		return
	}
	if len(st.Undo) == 0 || st.Root == nil {
		return
	}
	st.Redo = append(st.Redo, marshalSnapshot(st.Root))
	snap := st.Undo[len(st.Undo)-1]
	st.Undo = st.Undo[:len(st.Undo)-1]
	if restored := unmarshalSnapshot(snap); restored != nil {
		st.Root = restored
		st.NextID = max(st.NextID, maxNodeID(restored))
		if st.Root.FindByID(st.SelectedID) == nil {
			st.SelectedID = st.Root.ID
		}
		st.commitChange(cb)
	}
}

func (st *BlockState) opRedo(cb Callbacks) {
	if cb.ReadOnly {
		return
	}
	if cb.OnRedo != nil {
		cb.OnRedo()
		return
	}
	if len(st.Redo) == 0 || st.Root == nil {
		return
	}
	st.Undo = append(st.Undo, marshalSnapshot(st.Root))
	snap := st.Redo[len(st.Redo)-1]
	st.Redo = st.Redo[:len(st.Redo)-1]
	if restored := unmarshalSnapshot(snap); restored != nil {
		st.Root = restored
		st.NextID = max(st.NextID, maxNodeID(restored))
		if st.Root.FindByID(st.SelectedID) == nil {
			st.SelectedID = st.Root.ID
		}
		st.commitChange(cb)
	}
}

func (st *BlockState) opNavigate(dir string) {
	n := st.selectedNode()
	if n == nil {
		return
	}
	switch dir {
	case "up":
		if n.Parent != nil {
			if i := n.IndexInParent(); i > 0 {
				st.SelectedID = n.Parent.Children[i-1].ID
			}
		}
	case "down":
		if n.Parent != nil {
			if i := n.IndexInParent(); i >= 0 && i+1 < len(n.Parent.Children) {
				st.SelectedID = n.Parent.Children[i+1].ID
			}
		}
	case "out":
		if n.IsOpen() {
			st.SelectedID = n.Children[0].ID
		}
	case "in":
		if n.Parent != nil {
			st.SelectedID = n.Parent.ID
		}
	}
}

func (st *BlockState) beginEdit(n *Node, fresh bool) {
	if n == nil {
		return
	}
	st.EditingID = n.ID
	st.EditDraft = n.Text
	st.EditFresh = fresh
	st.EditFocusPending = true
	st.LinkNodeID = 0
}

func (st *BlockState) finishEdit(commit bool, cb Callbacks) {
	if st.EditingID == 0 {
		return
	}
	n := st.Root.FindByID(st.EditingID)
	st.EditingID = 0
	st.EditFocusPending = false
	if commit && n != nil {
		trimmed := strings.TrimSpace(st.EditDraft)
		if trimmed != "" && trimmed != n.Text {
			if !st.EditFresh {
				st.pushUndo()
			}
			n.Text = trimmed
			st.commitChange(cb)
		}
	}
	st.EditFresh = false
}

func (st *BlockState) beginLinkEdit(n *Node) {
	if n == nil {
		return
	}
	st.EditingID = 0
	st.LinkNodeID = n.ID
	st.LinkDraft = n.Link
	st.LinkFocusPending = true
}

func (st *BlockState) applyLink(target string, cb Callbacks) {
	if cb.ReadOnly || st.LinkNodeID == 0 || st.Root == nil {
		st.LinkNodeID = 0
		return
	}
	n := st.Root.FindByID(st.LinkNodeID)
	st.LinkNodeID = 0
	st.LinkFocusPending = false
	if n == nil {
		return
	}
	target = strings.TrimSpace(target)
	if target != n.Link {
		st.pushUndo()
		n.Link = target
		st.commitChange(cb)
	}
}

func (st *BlockState) applyImportedTree(root *Node, report ImportReport, cb Callbacks) {
	if cb.ReadOnly || root == nil {
		return
	}
	st.pushUndo()
	st.Root = root
	st.NextID = maxNodeID(root)
	st.SelectedID = root.ID
	st.AutoFitPending = true
	st.View = ViewMap
	if report.HasDropped() || report.Nodes > 0 {
		rep := report
		st.ImportResult = &rep
	}
	st.commitChange(cb)
}

func (st *BlockState) triggerImport(cb Callbacks) {
	if cb.ReadOnly || cb.OnPickImportFile == nil {
		return
	}
	st.ExportMenuOpen = false
	cb.OnPickImportFile(func(root *Node, report ImportReport) {
		st.applyImportedTree(root, report, cb)
	})
}

func (st *BlockState) triggerExport(format Format, cb Callbacks) {
	if st.Root == nil || cb.OnSaveExportFile == nil {
		return
	}
	st.ExportMenuOpen = false
	pal := theme.Ocean()
	data, err := ExportBytes(st.Root, st.Layout, format, pal)
	if err != nil {
		if cb.OnNotify != nil {
			cb.OnNotify(theme.Tr("导出失败：", "Export failed: ")+err.Error(), "danger")
		}
		return
	}
	base := sanitizeFileBase(st.Root.Text)
	ext := string(format)
	cb.OnSaveExportFile(base+"."+ext, ext, data, func(saveErr error) {
		if cb.OnNotify == nil {
			return
		}
		if saveErr != nil {
			cb.OnNotify(theme.Tr("导出失败：", "Export failed: ")+saveErr.Error(), "danger")
			return
		}
		cb.OnNotify(theme.Tr("已导出 "+base+"."+ext, "Exported "+base+"."+ext), "success")
	})
}

func sanitizeFileBase(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return "mindmap"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "", "<", "", ">", "", "|", "")
	s = strings.TrimSpace(replacer.Replace(s))
	if s == "" {
		return "mindmap"
	}
	return s
}

// --- Inline & Fullscreen Render Entry Points ---

// RenderInline renders the interactive Ocean Paper MindMap block inside the note editor.
func (st *BlockState) RenderInline(c *ui.Context, id string, source string, cfg Config, focused bool, cb Callbacks) {
	st.SyncSource(source, cfg)
	pal := theme.Ocean()
	borderCol := pal.Hairline
	if focused || st.CanvasHandle.Focused(c) || st.EditingID != 0 {
		borderCol = pal.Accent
	}

	card := ui.Column(c.Key("mm-card-"+id)).FillWidth().Radius(theme.RadiusXL).
		Background(pal.Paper).Border(1, borderCol).Clip()
	bui.Probe(card, "mm-card-"+id)
	card.Children(func() {
		if st.Fullscreen {
			// Keep only one active interactive canvas mounted while fullscreen is open,
			// preventing inline/fullscreen resize ping-pong and duplicate Handle bindings.
			row := ui.Row(c.Key("mm-inline-fs-placeholder-"+id)).FillWidth().Height(theme.HeightLarge).
				PaddingX(theme.Space12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Background(pal.CodeBG)
			row.Children(func() {
				ui.Text(c, theme.Tr("正在全屏查看思维导图", "Mind map is open in fullscreen")).
					FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
				exitBtn := ui.ButtonBase(c.Key("mm-inline-fs-exit-"+id)).
					Height(theme.HeightChrome).PaddingX(theme.Space10).Radius(theme.RadiusSM).
					Border(1, pal.BtnBorder).Background(pal.Paper).Center().Cursor(ui.CursorPointer).
					Label(theme.Tr("退出全屏", "Exit Fullscreen"))
				if exitBtn.Hovered() || exitBtn.Pressed() {
					exitBtn.Background(pal.Hover)
				}
				exitBtn.Children(func() {
					ui.Text(c, theme.Tr("退出全屏", "Exit Fullscreen")).
						FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
				})
				if exitBtn.Clicked() {
					st.Fullscreen = false
					st.AutoFitPending = true
					c.Invalidate()
				}
			})
			return
		}
		st.renderHeader(c, id, false, cb)
		if st.Fullscreen {
			// Fullscreen was just toggled on during header click; defer body/hint rendering
			// to RenderFullscreen in this frame so ui.Handle/ScrollState are never bound twice.
			c.Invalidate()
			return
		}
		st.renderBody(c, id, cfg.InlineHeightDIP(), false, cb)
		if cfg.ShowHints {
			st.renderHintBar(c, id)
		}
	})
}

// RenderFullscreen renders the full-window interactive MindMap overlay when Fullscreen is active.
func (st *BlockState) RenderFullscreen(c *ui.Context, id string, source string, cfg Config, cb Callbacks) {
	if !st.Fullscreen {
		return
	}
	st.SyncSource(source, cfg)
	pal := theme.Ocean()

	if c.Shortcut(0, ui.KeyEscape) && st.EditingID == 0 && st.LinkNodeID == 0 {
		if st.ImportResult != nil {
			st.ImportResult = nil
		} else if st.ExportMenuOpen {
			st.ExportMenuOpen = false
		} else {
			st.Fullscreen = false
			st.AutoFitPending = true
			c.Invalidate()
			return
		}
	}
	if !cb.ReadOnly && st.View == ViewOutline && st.EditingID == 0 && st.LinkNodeID == 0 {
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ) || c.Shortcut(ui.Cmd, ui.KeyY) {
			st.opRedo(cb)
		} else if c.Shortcut(ui.Cmd, ui.KeyZ) {
			st.opUndo(cb)
		}
	}

	overlay := ui.Column(c.Key("mm-fullscreen-"+id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).
		Background(pal.Paper)
	bui.Probe(overlay, "mm-fullscreen-"+id)
	overlay.Children(func() {
		st.renderHeader(c, id+"-fs", true, cb)
		if !st.Fullscreen {
			c.Invalidate()
			return
		}
		st.renderBody(c, id+"-fs", 0, true, cb)
		if cfg.ShowHints {
			st.renderHintBar(c, id+"-fs")
		}
	})
}

// --- Header Bar ---

func (st *BlockState) renderHeader(c *ui.Context, id string, fullscreen bool, cb Callbacks) {
	pal := theme.Ocean()
	hdr := ui.Row(c.Key("mm-hdr-"+id)).FillWidth().Height(theme.HeightLarge).Shrink(0).
		PaddingX(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space6).
		Background(pal.Paper)
	hdr.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Line(r.X, r.Y+r.H-0.5, r.X+r.W, r.Y+r.H-0.5, 1, pal.Hairline)
	})
	hdr.Children(func() {
		// Left: Segmented view switcher (导图 / 大纲 / 源码)
		seg := ui.Row(c.Key("mm-seg-"+id)).AlignItems(ui.Center).Gap(theme.Space2).Shrink(0)
		seg.Children(func() {
			tabs := []struct {
				mode  ViewMode
				icon  icons.Icon
				label string
			}{
				{ViewMap, icons.Diagram, theme.Tr("导图", "Map")},
				{ViewOutline, icons.ListBullet, theme.Tr("大纲", "Outline")},
				{ViewSource, icons.Source, theme.Tr("源码", "Source")},
			}
			for _, tab := range tabs {
				active := st.View == tab.mode
				btn := ui.ButtonBase(c.Key("mm-tab-"+id+"-"+string(tab.mode))).
					Height(theme.HeightChrome).PaddingX(theme.Space8).Radius(theme.RadiusSM).
					AlignItems(ui.Center).Gap(theme.Space4).Cursor(ui.CursorPointer).Shrink(0).Label(tab.label)
				hov := btn.Hovered()
				prs := btn.Pressed()
				bg := ui.Transparent
				border := ui.Transparent
				fg := pal.TextSecondary
				if active {
					bg = pal.Selected
					border = pal.Accent
					fg = pal.Text
				} else if hov || prs {
					bg = pal.Hover
					fg = pal.Text
				}
				bui.AnimateSurfaceColors(btn, bg, border, active)
				btn.Children(func() {
					icons.DrawAnimated(c, tab.icon, theme.IconInline, pal.DuoIconColors(fg), active || hov || prs)
					ui.Text(c, tab.label).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
				})
				if btn.Clicked() {
					st.finishEdit(true, cb)
					st.LinkNodeID = 0
					st.ExportMenuOpen = false
					st.View = tab.mode
					if tab.mode == ViewMap {
						st.AutoFitPending = true
					}
				}
			}
		})

		// Right: Layout toggle, Fit view, Import, Export dropdown, Fullscreen/Close
		actions := ui.Row(c.Key("mm-act-"+id)).AlignItems(ui.Center).Gap(theme.Space2).Shrink(0)
		actions.Children(func() {
			layIcon := icons.AlignLeft
			layTip := theme.Tr("切换为左右平衡布局", "Switch to balanced layout")
			if st.Layout == LayoutBoth {
				layIcon = icons.Columns
				layTip = theme.Tr("切换为向右布局", "Switch to right-side layout")
			}
			layBtn := bui.IconButton(c, layIcon, bui.IconButtonOptions{
				ID:       "mm-layout-" + id,
				Label:    layTip,
				Size:     theme.HeightChrome,
				IconSize: theme.IconAction,
			})
			if layBtn.Clicked() {
				if st.Layout == LayoutRight {
					st.Layout = LayoutBoth
				} else {
					st.Layout = LayoutRight
				}
				st.LayoutSet = true
				st.AutoFitPending = true
			}

			fitBtn := bui.IconButton(c, icons.TbReset, bui.IconButtonOptions{
				ID:       "mm-fit-" + id,
				Label:    theme.Tr("适配画布", "Fit to view"),
				Size:     theme.HeightChrome,
				IconSize: theme.IconAction,
			})
			if fitBtn.Clicked() {
				st.AutoFitPending = true
			}

			if !cb.ReadOnly {
				impBtn := bui.IconButton(c, icons.Upload, bui.IconButtonOptions{
					ID:       "mm-import-" + id,
					Label:    theme.Tr("导入 (.md / .opml / .mm / .xmind)", "Import (.md / .opml / .mm / .xmind)"),
					Size:     theme.HeightChrome,
					IconSize: theme.IconAction,
				})
				if impBtn.Clicked() {
					st.triggerImport(cb)
				}
			}

			expBtn := bui.IconButton(c, icons.Download, bui.IconButtonOptions{
				ID:       "mm-export-" + id,
				Label:    theme.Tr("导出", "Export"),
				Size:     theme.HeightChrome,
				IconSize: theme.IconAction,
				Active:   st.ExportMenuOpen,
			})
			if expBtn.Clicked() {
				st.ExportMenuOpen = !st.ExportMenuOpen
			}

			fsIcon := icons.Maximize
			fsTip := theme.Tr("全屏", "Fullscreen")
			if fullscreen {
				fsIcon = icons.Close
				fsTip = theme.Tr("退出全屏", "Exit fullscreen")
			}
			fsBtn := bui.IconButton(c, fsIcon, bui.IconButtonOptions{
				ID:       "mm-fs-" + id,
				Label:    fsTip,
				Size:     theme.HeightChrome,
				IconSize: theme.IconAction,
			})
			if fsBtn.Clicked() {
				st.finishEdit(true, cb)
				st.LinkNodeID = 0
				st.ExportMenuOpen = false
				st.Fullscreen = !fullscreen
				st.AutoFitPending = true
				c.Invalidate()
			}
		})
	})
}

// --- Body Area (Map / Outline / Source + Floating Overlays) ---

func (st *BlockState) renderBody(c *ui.Context, id string, fixedHeight float32, fullscreen bool, cb Callbacks) {
	pal := theme.Ocean()
	body := ui.Box(c.Key("mm-body-"+id)).FillWidth().Background(pal.CodeBG).Clip()
	if fullscreen {
		body.Grow(1).MinHeight(0)
	} else {
		body.Height(fixedHeight).Shrink(0)
	}
	body.Children(func() {
		switch st.View {
		case ViewOutline:
			st.renderOutlineView(c, id, cb)
		case ViewSource:
			st.renderSourceView(c, id, cb)
		default:
			st.renderCanvasView(c, id, cb)
		}

		if st.ExportMenuOpen {
			st.renderExportMenu(c, id, cb)
		}
		if st.LinkNodeID != 0 {
			st.renderLinkPopover(c, id, cb)
		}
		if st.ImportResult != nil {
			st.renderImportResultCard(c, id)
		}
	})
}

// --- Native GPU MindMap Canvas ---

func (st *BlockState) renderCanvasView(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	canvas := ui.Box(c.Key("mm-canvas-"+id)).Fill().Focusable().FocusRing(false).Bind(&st.CanvasHandle)
	bui.Probe(canvas, "mm-canvas-"+id)

	// Measure viewport size and ensure camera fits initially or after layout/resize.
	bounds := canvas.LayoutBounds()
	cw, ch := bounds.W, bounds.H
	if cw <= 0 {
		cw = 600
	}
	if ch <= 0 {
		ch = 360
	}
	if st.LastCanvasW > 0 && (math.Abs(float64(cw-st.LastCanvasW)) > 24 || math.Abs(float64(ch-st.LastCanvasH)) > 24) {
		st.AutoFitPending = true
	}
	st.LastCanvasW, st.LastCanvasH = cw, ch

	// Context menu (right-click on node or canvas).
	canvas.ContextMenu(func(m *ui.Menu) {
		n := st.selectedNode()
		if n == nil {
			return
		}
		if !cb.ReadOnly {
			if m.Item(theme.Tr("添加子节点", "Add Child")).Shortcut(0, ui.KeyTab).Chosen() {
				st.opAddChild(cb)
			}
			if n.Parent != nil {
				if m.Item(theme.Tr("添加同级节点", "Add Sibling")).Shortcut(0, ui.KeyEnter).Chosen() {
					st.opAddSibling(cb)
				}
				if m.Item(theme.Tr("上移节点", "Move Up")).Shortcut(ui.Alt, ui.KeyUp).Chosen() {
					st.opMoveSibling(-1, cb)
				}
				if m.Item(theme.Tr("下移节点", "Move Down")).Shortcut(ui.Alt, ui.KeyDown).Chosen() {
					st.opMoveSibling(1, cb)
				}
			}
			if m.Item(theme.Tr("改名", "Rename")).Shortcut(0, ui.KeyF2).Chosen() {
				st.beginEdit(n, false)
			}
			m.Separator()
			linkLbl := theme.Tr("链接到笔记", "Link to Note")
			if strings.TrimSpace(n.Link) != "" {
				linkLbl = theme.Tr("编辑链接", "Edit Link")
			}
			if m.Item(linkLbl).Shortcut(ui.Cmd, ui.KeyL).Chosen() {
				st.beginLinkEdit(n)
			}
			if strings.TrimSpace(n.Link) != "" {
				target := n.Link
				if m.Item(theme.Tr("打开链接", "Open Link")).Chosen() {
					if cb.OnOpenLink != nil {
						cb.OnOpenLink(target)
					}
				}
				if m.Item(theme.Tr("移除链接", "Remove Link")).Chosen() {
					st.pushUndo()
					n.Link = ""
					st.commitChange(cb)
				}
			}
			if n.Parent != nil {
				taskLbl := theme.Tr("设为任务", "Convert to Task")
				if n.Task != nil {
					taskLbl = theme.Tr("取消任务", "Remove Task")
				}
				if m.Item(taskLbl).Chosen() {
					st.opToggleTask(n, cb)
				}
				if n.Task != nil {
					doneLbl := theme.Tr("标记已完成", "Mark Done")
					if *n.Task {
						doneLbl = theme.Tr("标记未完成", "Mark Undone")
					}
					if m.Item(doneLbl).Chosen() {
						st.opFlipTaskDone(n, cb)
					}
				}
			}
		}
		if len(n.Children) > 0 {
			m.Separator()
			foldLbl := theme.Tr("折叠", "Fold")
			if n.Folded {
				foldLbl = theme.Tr("展开", "Unfold")
			}
			if m.Item(foldLbl).Shortcut(0, ui.KeySpace).Chosen() {
				st.opToggleFold()
			}
		}
		if !cb.ReadOnly && n.Parent != nil {
			m.Separator()
			if m.Item(theme.Tr("删除", "Delete")).Shortcut(0, ui.KeyBackspace).Chosen() {
				st.opDelete(cb)
			}
		}
	})

	// Pointer, Scroll, Zoom, and Keyboard input handler.
	canvas.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerMove:
			if st.Panning && st.Camera.Scale > 0 {
				dx := ev.X - st.PanLastX
				dy := ev.Y - st.PanLastY
				st.Camera.X -= dx / st.Camera.Scale
				st.Camera.Y -= dy / st.Camera.Scale
				st.PanLastX, st.PanLastY = ev.X, ev.Y
				return true
			}
			wx, wy := st.Camera.ScreenToWorld(ev.X, ev.Y)
			hit := HitTest(st.Root, wx, wy)
			if hit.Node != nil {
				st.HoveredID = hit.Node.ID
			} else {
				st.HoveredID = 0
			}
			return false

		case ui.InputPointerDown:
			st.ExportMenuOpen = false
			st.ImportResult = nil
			st.LinkNodeID = 0
			if st.EditingID != 0 {
				st.finishEdit(true, cb)
			}
			wx, wy := st.Camera.ScreenToWorld(ev.X, ev.Y)
			hit := HitTest(st.Root, wx, wy)

			if ev.Button == 1 {
				// Right-click selects hit node before ContextMenu opens.
				if hit.Node != nil {
					st.SelectedID = hit.Node.ID
				}
				return false
			}
			if ev.Button != 0 {
				return false
			}

			st.CanvasHandle.Focus()
			if cb.OnRequestFocus != nil {
				cb.OnRequestFocus()
			}

			switch hit.Kind {
			case HitCheckbox:
				st.opFlipTaskDone(hit.Node, cb)
				return true
			case HitFoldBadge:
				hit.Node.Folded = false
				st.SelectedID = hit.Node.ID
				return true
			case HitNode:
				st.SelectedID = hit.Node.ID
				if (ev.Mods&(ui.Cmd|ui.Ctrl)) != 0 && strings.TrimSpace(hit.Node.Link) != "" && cb.OnOpenLink != nil {
					cb.OnOpenLink(hit.Node.Link)
					return true
				}
				if ev.Clicks >= 2 && !cb.ReadOnly {
					st.beginEdit(hit.Node, false)
					return true
				}
				return true
			default:
				if ev.Clicks >= 2 {
					st.AutoFitPending = true
					return true
				}
				st.Panning = true
				st.PanLastX, st.PanLastY = ev.X, ev.Y
				return true
			}

		case ui.InputPointerUp:
			if st.Panning {
				st.Panning = false
				return true
			}
			return false

		case ui.InputZoom:
			if ev.Scale > 0 && st.Camera.Scale > 0 {
				st.zoomAt(ev.X, ev.Y, st.Camera.Scale*ev.Scale)
				return true
			}
			return false

		case ui.InputScroll:
			if st.Camera.Scale <= 0 {
				return false
			}
			// Cmd/Ctrl + Wheel zooms smoothly around pointer.
			if ev.Mods&(ui.Cmd|ui.Ctrl) != 0 {
				factor := float32(math.Exp(float64(-ev.DY) * 0.008))
				st.zoomAt(ev.X, ev.Y, st.Camera.Scale*factor)
				return true
			}
			// In fullscreen or when actively holding Shift/horizontal scroll, pan the canvas.
			if st.Fullscreen || math.Abs(float64(ev.DX)) > 0.5 || st.CanvasHandle.Focused(c) {
				st.Camera.X += ev.DX / st.Camera.Scale
				st.Camera.Y += ev.DY / st.Camera.Scale
				return true
			}
			return false

		case ui.InputKeyDown:
			if st.EditingID != 0 || st.LinkNodeID != 0 {
				return false
			}
			return st.handleCanvasKey(ev.Key, ev.Mods, cb)
		}
		return false
	})

	// Paint native vector mindmap.
	canvas.Draw(func(p *ui.Painter, r ui.Rect) {
		if st.Root == nil || r.W <= 0 || r.H <= 0 {
			return
		}
		worldBounds := ComputeLayout(st.Root, st.Layout, p, &st.Measure)
		if !st.CameraInit || st.AutoFitPending {
			st.Camera = FitCamera(worldBounds, r.W, r.H, 28, 22)
			st.CameraInit = true
			st.AutoFitPending = false
		}
		PaintScene(p, st.Root, r, st.Camera, PaintOptions{
			SelectedID: st.SelectedID,
			HoveredID:  st.HoveredID,
			EditingID:  st.EditingID,
			Palette:    pal,
			Background: pal.CodeBG,
		})
	})

	// Inline node text editor overlay positioned exactly over the editing node.
	if st.EditingID != 0 && !cb.ReadOnly {
		if n := st.Root.FindByID(st.EditingID); n != nil {
			sx, sy := st.Camera.WorldToScreen(n.X, n.Y-n.H/2)
			scale := st.Camera.Scale
			if scale <= 0 {
				scale = 1
			}
			edW := max(n.W*scale+36, 96)
			edH := max(n.H*scale+4, theme.HeightCompact)
			edX := clampFloat32(sx-2, theme.Space6, max(cw-edW-theme.Space6, theme.Space6))
			edY := clampFloat32(sy-2, theme.Space6, max(ch-edH-theme.Space6, theme.Space6))

			input := ui.TextInputBase(c.Key("mm-inline-edit-"+id), &st.EditDraft).
				Absolute().Left(edX).Top(edY).Width(edW).Height(edH).
				PaddingX(theme.Space8).Radius(theme.RadiusMD).
				Background(pal.Paper).Border(1.5, pal.Accent).
				FontSize(theme.FontSmall).TextColor(pal.Text).
				CaretColor(pal.Accent).SelectionColor(pal.EditorSelectionBG()).
				Bind(&st.EditHandle)
			if st.EditFocusPending {
				st.EditFocusPending = false
				st.EditHandle.Focus()
			}
			input.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind != ui.InputKeyDown {
					return false
				}
				switch ev.Key {
				case ui.KeyEnter:
					st.finishEdit(true, cb)
					st.CanvasHandle.Focus()
					return true
				case ui.KeyEscape:
					st.finishEdit(false, cb)
					st.CanvasHandle.Focus()
					return true
				case ui.KeyTab:
					st.finishEdit(true, cb)
					st.opAddChild(cb)
					return true
				}
				return false
			})
			if input.FocusLeft() {
				st.finishEdit(true, cb)
			}
		}
	}
}

func (st *BlockState) zoomAt(sx, sy, targetScale float32) {
	targetScale = clampFloat32(targetScale, 0.2, 3.0)
	if st.Camera.Scale <= 0 {
		st.Camera.Scale = 1
	}
	wx, wy := st.Camera.ScreenToWorld(sx, sy)
	st.Camera.Scale = targetScale
	st.Camera.X = wx - sx/targetScale
	st.Camera.Y = wy - sy/targetScale
}

func (st *BlockState) handleCanvasKey(key ui.Key, mods ui.Modifiers, cb Callbacks) bool {
	// Cmd/Ctrl shortcuts
	if mods == ui.Cmd || mods == ui.Ctrl {
		switch key {
		case ui.KeyZ:
			st.opUndo(cb)
			return true
		case ui.KeyL:
			if !cb.ReadOnly {
				st.beginLinkEdit(st.selectedNode())
				return true
			}
		case ui.Key0:
			st.AutoFitPending = true
			return true
		case ui.KeyEqual:
			st.zoomAt(st.LastCanvasW*0.5, st.LastCanvasH*0.5, st.Camera.Scale*1.15)
			return true
		case ui.KeyMinus:
			st.zoomAt(st.LastCanvasW*0.5, st.LastCanvasH*0.5, st.Camera.Scale/1.15)
			return true
		}
		return false
	}
	if (mods == ui.Cmd|ui.Shift || mods == ui.Ctrl|ui.Shift) && key == ui.KeyZ {
		st.opRedo(cb)
		return true
	}

	// Alt+Up / Alt+Down: reorder node
	if mods == ui.Alt {
		switch key {
		case ui.KeyUp:
			st.opMoveSibling(-1, cb)
			return true
		case ui.KeyDown:
			st.opMoveSibling(1, cb)
			return true
		}
		return false
	}

	if mods != 0 {
		return false
	}

	n := st.selectedNode()
	if n == nil {
		return false
	}
	leftSide := n.Dir < 0

	switch key {
	case ui.KeyTab:
		st.opAddChild(cb)
		return true
	case ui.KeyEnter:
		st.opAddSibling(cb)
		return true
	case ui.KeyBackspace, ui.KeyDelete:
		st.opDelete(cb)
		return true
	case ui.KeySpace:
		st.opToggleFold()
		return true
	case ui.KeyF2:
		if !cb.ReadOnly {
			st.beginEdit(n, false)
			return true
		}
	case ui.KeyUp:
		st.opNavigate("up")
		return true
	case ui.KeyDown:
		st.opNavigate("down")
		return true
	case ui.KeyRight:
		if leftSide {
			st.opNavigate("in")
		} else {
			st.opNavigate("out")
		}
		return true
	case ui.KeyLeft:
		if n.Depth == 0 && st.Layout == LayoutBoth && n.IsOpen() {
			k := (len(n.Children) + 1) / 2
			if k < len(n.Children) {
				st.SelectedID = n.Children[k].ID
				return true
			}
		}
		if leftSide {
			st.opNavigate("out")
		} else {
			st.opNavigate("in")
		}
		return true
	}
	return false
}

// --- Outline View ---

func (st *BlockState) renderOutlineView(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	scr := bui.Scroll(c.Key("mm-out-scroll-"+id), &st.OutlineScroll).Fill().Padding(theme.Space12, theme.Space16)
	scr.Children(func() {
		col := ui.Column(c.Key("mm-out-col-"+id)).FillWidth().Gap(theme.Space2)
		col.Children(func() {
			if st.Root == nil {
				return
			}
			// Root heading row
			ui.Text(c, st.Root.Text).FontSize(theme.FontTitle).FontWeight(theme.WeightTitle).
				TextColor(pal.Text).Margin(0, 0, theme.Space6, 0).SingleLine()

			var renderChildren func(parent *Node, depth int)
			renderChildren = func(parent *Node, depth int) {
				for _, ch := range parent.Children {
					node := ch
					indent := float32(depth) * 18
					selected := node.ID == st.SelectedID
					row := ui.Row(c.Key(fmt.Sprintf("mm-out-row-%s-%d", id, node.ID))).
						FillWidth().Height(theme.HeightChrome).Padding(0, theme.Space6, 0, indent+theme.Space4).
						AlignItems(ui.Center).Gap(theme.Space8).Radius(theme.RadiusSM).Cursor(ui.CursorPointer)
					if selected {
						row.Background(pal.Selected)
					} else if row.Hovered() {
						row.Background(pal.Hover)
					}
					row.Children(func() {
						if node.Task != nil {
							done := *node.Task
							cbBox := ui.ButtonBase(c.Key(fmt.Sprintf("mm-out-cb-%s-%d", id, node.ID))).
								Size(14, 14).Shrink(0).Radius(theme.RadiusXS).Cursor(ui.CursorPointer)
							if done {
								cbBox.Background(pal.Accent)
							} else {
								cbBox.Background(pal.Paper).Border(1, pal.TextSecondary)
							}
							cbBox.DrawOver(func(p *ui.Painter, r ui.Rect) {
								if !done {
									return
								}
								var ck ui.Path
								ck.Cap(ui.RoundCap).Join(ui.RoundJoin)
								ck.MoveTo(r.X+3.2, r.Y+7.2)
								ck.LineTo(r.X+5.8, r.Y+9.8)
								ck.LineTo(r.X+10.8, r.Y+4.2)
								p.StrokePath(&ck, 1.5, pal.Paper)
							})
							if cbBox.Clicked() {
								st.opFlipTaskDone(node, cb)
							}
						} else {
							dot := ui.Box(c).Size(14, 14).Shrink(0).Center()
							dot.Draw(func(p *ui.Painter, r ui.Rect) {
								cR := ui.Rect{X: r.X + 4.5, Y: r.Y + 4.5, W: 5, H: 5}
								p.Fill(cR, pal.TextSecondary, 2.5)
							})
						}

						txtCol := pal.Text
						if strings.TrimSpace(node.Link) != "" {
							txtCol = pal.Accent
						}
						if node.Task != nil && *node.Task {
							txtCol = txtCol.Alpha(0.55)
						}
						ui.Text(c, node.Text).FontSize(theme.FontSmall).TextColor(txtCol).
							Grow(1).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
					})
					if row.Clicked() {
						st.SelectedID = node.ID
						if strings.TrimSpace(node.Link) != "" && (row.ClickModifiers()&(ui.Cmd|ui.Ctrl)) != 0 && cb.OnOpenLink != nil {
							cb.OnOpenLink(node.Link)
						}
					}
					renderChildren(node, depth+1)
				}
			}
			renderChildren(st.Root, 0)
		})
	})
}

// --- Source View ---

func (st *BlockState) renderSourceView(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	draft := st.LastSource
	if draft == "" && st.Root != nil {
		draft = SerializeMarkdown(st.Root)
	}
	prev := draft
	ta := ui.TextAreaBase(c.Key("mm-src-"+id), &draft).Fill().
		Padding(theme.Space12, theme.Space16).BorderWidth(0).Background(pal.CodeBG).
		FontSize(theme.FontSmall).LineHeight(1.6).TextColor(pal.Text).
		CaretColor(pal.Accent).SelectionColor(pal.EditorSelectionBG())
	if cb.ReadOnly {
		ta.ReadOnly(true)
	}
	bui.AutoHideScrollbars(c, ta, &st.SourceScroll)
	if !cb.ReadOnly && draft != prev {
		st.SyncSource(draft, DefaultConfig())
		if cb.OnChange != nil {
			cb.OnChange(draft)
		}
	}
}

// --- Export Dropdown Menu ---

func (st *BlockState) renderExportMenu(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	// Backdrop to close on outside click.
	scrim := ui.Box(c.Key("mm-exp-scrim-"+id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(ui.Transparent)
	if scrim.Clicked() {
		st.ExportMenuOpen = false
		return
	}

	panel := ui.Column(c.Key("mm-exp-menu-"+id)).Absolute().Top(theme.Space6).Right(theme.Space8).
		Width(204).Padding(theme.MenuPadding).Gap(theme.MenuGap).
		Radius(theme.RadiusLG).Background(pal.Menu).Border(1, pal.MenuBorder)
	bui.Elevate(panel, theme.ElevationPopover)
	panel.Children(func() {
		ui.Text(c, theme.Tr("导出为", "Export as")).
			FontSize(theme.FontMeta).FontWeight(theme.WeightTitle).TextColor(pal.TextSecondary).
			Padding(theme.Space4, theme.Space8, theme.Space2, theme.Space8).SingleLine()

		renderFmtRow := func(label, ext string, format Format) {
			row := ui.ButtonBase(c.Key("mm-exp-item-"+id+"-"+ext)).
				FillWidth().Height(theme.MenuItemHeight).PaddingX(theme.Space8).Radius(theme.RadiusSM).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Cursor(ui.CursorPointer).Label(label)
			if row.Hovered() || row.Pressed() {
				row.Background(pal.MenuHover)
			}
			row.Children(func() {
				ui.Text(c, label).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
					TextColor(pal.Text).SingleLine().Shrink(1).MinWidth(0)
				ui.Text(c, "."+ext).FontSize(theme.FontMeta).FontWeight(theme.WeightMono).
					TextColor(pal.TextSecondary).SingleLine().Shrink(0)
			})
			if row.Clicked() {
				st.triggerExport(format, cb)
			}
		}

		renderFmtRow("Markdown", "md", FormatMarkdown)
		renderFmtRow("OPML", "opml", FormatOPML)
		renderFmtRow("FreeMind", "mm", FormatFreeMind)
		renderFmtRow("XMind", "xmind", FormatXMind)

		ui.Box(c).FillWidth().Height(1).Margin(theme.Space4, theme.Space2).Background(pal.MenuSeparator)

		renderFmtRow(theme.Tr("PNG 图片", "PNG Image"), "png", FormatPNG)
		renderFmtRow("PDF", "pdf", FormatPDF)

		if !cb.ReadOnly {
			ui.Box(c).FillWidth().Height(1).Margin(theme.Space4, theme.Space2).Background(pal.MenuSeparator)
			impRow := ui.ButtonBase(c.Key("mm-exp-imp-"+id)).
				FillWidth().Height(theme.MenuItemHeight).PaddingX(theme.Space8).Radius(theme.RadiusSM).
				AlignItems(ui.Center).Gap(theme.Space8).Cursor(ui.CursorPointer).Label(theme.Tr("导入文件", "Import File"))
			impHot := impRow.Hovered() || impRow.Pressed()
			if impHot {
				impRow.Background(pal.MenuHover)
			}
			impRow.Children(func() {
				ic := pal.DuoIconColors(pal.TextSecondary)
				if impHot {
					ic = pal.DuoIconColors(pal.Text)
				}
				icons.DrawAnimated(c, icons.Upload, theme.IconInline, ic, impHot)
				ui.Text(c, theme.Tr("导入文件", "Import File")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
					TextColor(pal.Text).SingleLine()
			})
			if impRow.Clicked() {
				st.triggerImport(cb)
			}
		}
	})
}

// --- Link-to-Note Popover (Cmd+K) ---

func (st *BlockState) renderLinkPopover(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	scrim := ui.Box(c.Key("mm-link-scrim-"+id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(ui.Transparent)
	if scrim.Clicked() {
		st.LinkNodeID = 0
		return
	}

	panel := ui.Column(c.Key("mm-link-pop-"+id)).Absolute().Top(theme.Space12).Right(theme.Space12).
		Width(280).Padding(theme.Space12).Gap(theme.Space8).
		Radius(theme.RadiusLG).Background(pal.Menu).Border(1, pal.MenuBorder)
	bui.Elevate(panel, theme.ElevationPopover)
	panel.Children(func() {
		ui.Text(c, theme.Tr("链接到笔记或网址", "Link to Note or URL")).
			FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()

		inp := ui.TextInputBase(c.Key("mm-link-in-"+id), &st.LinkDraft).
			FillWidth().Height(theme.HeightControl).PaddingX(theme.Space8).
			Radius(theme.RadiusMD).Background(pal.Paper).Border(1, pal.BtnBorder).
			FontSize(theme.FontSmall).TextColor(pal.Text).
			Placeholder(theme.Tr("输入笔记路径或 https://…", "Note path or https://…")).
			PlaceholderColor(pal.Placeholder).CaretColor(pal.Accent).
			Bind(&st.LinkHandle)
		if st.LinkFocusPending {
			st.LinkFocusPending = false
			st.LinkHandle.Focus()
		}
		inp.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			switch ev.Key {
			case ui.KeyEnter:
				st.applyLink(st.LinkDraft, cb)
				st.CanvasHandle.Focus()
				return true
			case ui.KeyEscape:
				st.LinkNodeID = 0
				st.CanvasHandle.Focus()
				return true
			}
			return false
		})

		// Matching notes from current notebook.
		if cb.OnListNotes != nil {
			notes := cb.OnListNotes()
			q := strings.ToLower(strings.TrimSpace(st.LinkDraft))
			shown := 0
			for _, nc := range notes {
				if q != "" && !strings.Contains(strings.ToLower(nc.Title), q) && !strings.Contains(strings.ToLower(nc.RelPath), q) {
					continue
				}
				if shown >= 5 {
					break
				}
				shown++
				cand := nc
				row := ui.ButtonBase(c.Key("mm-link-note-"+id+"-"+cand.RelPath)).
					FillWidth().Height(theme.MenuItemHeight).PaddingX(theme.Space8).Radius(theme.RadiusSM).
					AlignItems(ui.Center).Gap(theme.Space6).Cursor(ui.CursorPointer).Label(cand.Title)
				rowHot := row.Hovered() || row.Pressed()
				if rowHot {
					row.Background(pal.MenuHover)
				}
				row.Children(func() {
					ic := pal.DuoIconColors(pal.TextSecondary)
					if rowHot {
						ic = pal.DuoIconColors(pal.Text)
					}
					icons.DrawAnimated(c, icons.Note, theme.IconInline, ic, rowHot)
					ui.Text(c, cand.Title).FontSize(theme.FontSmall).TextColor(pal.Text).
						Grow(1).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
				})
				if row.Clicked() {
					st.applyLink(cand.RelPath, cb)
					st.CanvasHandle.Focus()
				}
			}
		}

		btnRow := ui.Row(c.Key("mm-link-btns-"+id)).FillWidth().Justify(ui.End).Gap(theme.Space6)
		btnRow.Children(func() {
			cancel := ui.ButtonBase(c.Key("mm-link-cancel-"+id)).Height(theme.HeightCompact).PaddingX(theme.Space10).
				Radius(theme.RadiusMD).Border(1, pal.BtnBorder).Background(pal.Paper).Center().Cursor(ui.CursorPointer).
				Label(theme.Tr("取消", "Cancel"))
			cancel.Children(func() {
				ui.Text(c, theme.Tr("取消", "Cancel")).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
			})
			if cancel.Clicked() {
				st.LinkNodeID = 0
				st.CanvasHandle.Focus()
			}

			okBtn := ui.ButtonBase(c.Key("mm-link-ok-"+id)).Height(theme.HeightCompact).PaddingX(theme.Space12).
				Radius(theme.RadiusMD).Background(pal.Accent).Center().Cursor(ui.CursorPointer).
				Label(theme.Tr("确定", "Apply"))
			okBtn.Children(func() {
				ui.Text(c, theme.Tr("确定", "Apply")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(pal.Paper).SingleLine()
			})
			if okBtn.Clicked() {
				st.applyLink(st.LinkDraft, cb)
				st.CanvasHandle.Focus()
			}
		})
	})
}

// --- Import Result Card (matches mindmap-b-lite.html .res card) ---

func (st *BlockState) renderImportResultCard(c *ui.Context, id string) {
	rep := st.ImportResult
	if rep == nil {
		return
	}
	pal := theme.Ocean()
	card := ui.Column(c.Key("mm-imp-res-"+id)).Absolute().Top(theme.Space12).Right(theme.Space12).
		Width(232).Padding(theme.Space12, theme.Space16).Gap(theme.Space4).
		Radius(theme.RadiusLG).Background(pal.Menu).Border(1, pal.MenuBorder)
	bui.Elevate(card, theme.ElevationPopover)
	card.Children(func() {
		name := filepath.Base(rep.FileName)
		if name == "" || name == "." {
			name = "mindmap"
		}
		ui.Text(c, theme.Tr("已导入 ", "Imported ")+name).
			FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).TextColor(pal.Text).
			SingleLine().Ellipsis("…").Margin(0, 0, theme.Space4, 0)

		renderStatRow := func(label, val string, dropped bool) {
			row := ui.Row(c).FillWidth().Height(theme.HeightCompact).AlignItems(ui.Center).Justify(ui.SpaceBetween)
			row.Children(func() {
				lblCol := pal.Text
				if dropped {
					lblCol = pal.TextSecondary
				}
				t := ui.Text(c, label).FontSize(theme.FontSmall).TextColor(lblCol).SingleLine()
				if dropped {
					t.Strikethrough()
				}
				ui.Text(c, val).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
			})
		}

		renderStatRow(theme.Tr("节点", "Nodes"), fmt.Sprintf("%d", rep.Nodes), false)
		if rep.Tasks > 0 {
			renderStatRow(theme.Tr("任务", "Tasks"), fmt.Sprintf("%d", rep.Tasks), false)
		}
		if rep.Links > 0 {
			renderStatRow(theme.Tr("链接", "Links"), fmt.Sprintf("%d", rep.Links), false)
		}

		droppedFmt := func(n int) string {
			return fmt.Sprintf("%d %s", n, theme.Tr("未导入", "skipped"))
		}
		if rep.Relationships > 0 {
			renderStatRow(theme.Tr("关联线", "Relationships"), droppedFmt(rep.Relationships), true)
		}
		if rep.Summaries > 0 {
			renderStatRow(theme.Tr("概要", "Summaries"), droppedFmt(rep.Summaries), true)
		}
		if rep.Boundaries > 0 {
			renderStatRow(theme.Tr("外框", "Boundaries"), droppedFmt(rep.Boundaries), true)
		}
		if rep.Notes > 0 {
			renderStatRow(theme.Tr("备注", "Notes"), droppedFmt(rep.Notes), true)
		}
		if rep.Callouts > 0 {
			renderStatRow(theme.Tr("标注", "Callouts"), droppedFmt(rep.Callouts), true)
		}
		if rep.Images > 0 {
			renderStatRow(theme.Tr("图片", "Images"), droppedFmt(rep.Images), true)
		}
		if rep.ExtraSheets > 0 {
			renderStatRow(theme.Tr("其余画布", "Extra Sheets"), droppedFmt(rep.ExtraSheets), true)
		}

		btn := ui.ButtonBase(c.Key("mm-imp-ok-"+id)).FillWidth().Height(theme.HeightCompact).
			Margin(theme.Space6, 0, 0, 0).Radius(theme.RadiusMD).
			Border(1, pal.BtnBorder).Background(pal.Paper).Center().Cursor(ui.CursorPointer).
			Label(theme.Tr("知道了", "Got it"))
		if btn.Hovered() || btn.Pressed() {
			btn.Background(pal.Hover)
		}
		btn.Children(func() {
			ui.Text(c, theme.Tr("知道了", "Got it")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
				TextColor(pal.Text).SingleLine()
		})
		if btn.Clicked() {
			st.ImportResult = nil
		}
	})
}

// --- Bottom Shortcut Hint Bar (Single-Row Horizontal Scroll + Direct Mouse Wheel Scroll) ---

func (st *BlockState) renderHintBar(c *ui.Context, id string) {
	pal := theme.Ocean()
	bar := ui.Row(c.Key("mm-hint-bar-"+id)).FillWidth().Height(theme.HeightControl).Shrink(0).
		Background(pal.Paper).AlignItems(ui.Center).Clip()

	// Convert vertical mouse wheel directly into horizontal scroll over the bottom hint bar
	// whenever the strip overflows horizontally (no line wrapping!).
	handleHintWheel := func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll || st.HintScroll.MaxX <= 0 {
			return false
		}
		if math.Abs(float64(ev.DY)) >= math.Abs(float64(ev.DX)) && math.Abs(float64(ev.DY)) >= 0.1 {
			st.HintScroll.X = clampFloat32(st.HintScroll.X+ev.DY, 0, st.HintScroll.MaxX)
			return true
		}
		return false
	}
	bar.HandleInput(handleHintWheel)

	bar.Children(func() {
		strip := ui.ScrollHorizontal(c.Key("mm-hint-scroll-"+id)).
			Grow(1).Shrink(1).MinWidth(0).FillHeight().
			AlignItems(ui.Center).PaddingX(theme.Space10).Gap(theme.Space12).
			Scrollbars(false).TrackScroll(&st.HintScroll)
		strip.HandleInput(handleHintWheel)
		strip.Children(func() {
			type hintItem struct {
				keys  []string
				label string
			}
			items := []hintItem{
				{[]string{"Tab"}, theme.Tr("子节点", "Child")},
				{[]string{"↵"}, theme.Tr("同级", "Sibling")},
				{[]string{"⌥↑", "⌥↓"}, theme.Tr("移动", "Move")},
				{[]string{"⌫"}, theme.Tr("删除", "Delete")},
				{[]string{"Space"}, theme.Tr("折叠", "Fold")},
				{[]string{"F2"}, theme.Tr("改名", "Rename")},
				{[]string{"⌘L"}, theme.Tr("链接", "Link")},
				{[]string{"⌘Z"}, theme.Tr("撤销", "Undo")},
			}
			for _, it := range items {
				itemRow := ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0)
				itemRow.Children(func() {
					for _, k := range it.keys {
						bui.Keycap(c, k)
					}
					ui.Text(c, it.label).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
				})
			}
		})
	})

	// Subtle horizontal overflow edge indicators so users can spot hidden hints.
	bar.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Line(r.X, r.Y+0.5, r.X+r.W, r.Y+0.5, 1, pal.Hairline)
		if st.HintScroll.MaxX <= 1 {
			return
		}
		if st.HintScroll.X > 1 {
			p.Fill(ui.Rect{X: r.X, Y: r.Y + 1, W: 14, H: r.H - 1}, pal.Paper.Alpha(0.75), 0)
		}
		if st.HintScroll.X < st.HintScroll.MaxX-1 {
			p.Fill(ui.Rect{X: r.X + r.W - 14, Y: r.Y + 1, W: 14, H: r.H - 1}, pal.Paper.Alpha(0.75), 0)
		}
	})
}

func clampFloat32(v, lo, hi float32) float32 {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
