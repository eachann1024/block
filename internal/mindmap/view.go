package mindmap

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"block/internal/icons"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// NoteCandidate represents a notebook file selectable in the Link-to-Note picker.
type NoteCandidate struct {
	Title       string
	RelPath     string
	DisplayPath string
}

// Callbacks bridges mindmap block actions back to the host editor and workspace.
type Callbacks struct {
	OnChange          func(newMarkdown string)
	OnUndo            func()
	OnRedo            func()
	CanUndo           func() bool
	CanRedo           func() bool
	OnOpenLink        func(target string)
	OnCheckLinkStatus func(target string) LinkStatus
	OnListNotes       func() []NoteCandidate
	OnPickImportFile  func(onImported func(root *Node, report ImportReport))
	OnSaveExportFile  func(defaultName, ext string, data []byte, onDone func(err error))
	OnNotify          func(msg, tone string)
	OnRequestFocus    func()
	ReadOnly          bool
}

// BlockState keeps all interactive runtime state for one mindmap block.
type BlockState struct {
	Root       *Node
	NextID     int
	LastSource string
	SelectedID int
	HoveredID  int
	HoveredHit HitKind
	View       ViewMode
	Layout     LayoutMode
	LayoutSet  bool
	Fullscreen bool

	Camera               Camera
	CameraInit           bool
	AutoFitPending       bool
	EnsureVisiblePending bool
	LastCanvasW          float32
	LastCanvasH          float32
	Measure              MeasureCache

	Panning  bool
	PanLastX float32
	PanLastY float32

	DragNodeID   int
	DragActive   bool
	DragStartX   float32
	DragStartY   float32
	DragCurrX    float32
	DragCurrY    float32
	DragTargetID int
	DragPos      DragDropPos

	lastKeyMod        ui.Modifiers
	ComposeText       string
	LastCanvasFocused bool

	EditingID          int
	EditSeq            int
	EditDraft          string
	EditCompose        string
	editLastLayoutText string
	EditFresh          bool
	EditSelectAll      bool
	EditFocusPending   bool
	EditHandle         ui.Handle

	linkStatusCache   map[string]LinkStatus
	linkStatusChecked time.Time

	LinkNodeID       int
	LinkDraft        string
	LinkFocusPending bool
	LinkActiveIdx    int
	LinkBrokenNotice bool
	LinkHandle       ui.Handle
	LinkListScroll   ui.ScrollState

	FindOpen         bool
	FindQuery        string
	FindFocusPending bool
	FindMatchIDs     []int
	FindMatchSet     map[int]bool
	FindCurrentIdx   int
	FindHandle       ui.Handle

	ExportMenuOpen   bool
	ExportMenuScroll ui.ScrollState
	ExportTheme      ExportTheme
	ShortcutsOpen    bool
	ShortcutsScroll  ui.ScrollState
	ImportResult     *ImportReport
	ImportUndoBefore string
	ImportUndoAfter  string

	Undo [][]byte
	Redo [][]byte

	OutlineZoomID             int
	OutlineFocusID            int
	OutlineFocusPending       bool
	OutlineFocusPreventScroll bool
	OutlineCaretPending       bool
	OutlineCaretTarget        int
	OutlineSelNodeID          int
	OutlineSelStart           int
	OutlineSelEnd             int
	OutlineSelRuneLen         int
	OutlineUndoNodeID         int
	outlinePending            *outlineAction

	SourceCopiedUntil time.Time

	// Block-scoped chords (⌘1/2/3 views, ⌘⇧↵ fullscreen) captured inside text
	// fields are queued here and applied by handleBlockShortcuts next frame.
	pendingView      ViewMode
	pendingFSToggle  bool
	viewFocusPending bool

	CanvasHandle  ui.Handle
	OutlineHandle ui.Handle
	SourceHandle  ui.Handle
	HintScroll    ui.ScrollState
	OutlineScroll ui.ScrollState
	SourceScroll  ui.ScrollState

	editorBounds        ui.Rect
	editorBoundsValid   bool
	inlineCardRect      ui.Rect
	inlineCardRectValid bool
	lastInlineFocused   bool

	inlineBodyMountedThisFrame bool
	inlineRenderedThisFrame    bool
	inlineRenderedLastFrame    bool
	inlineCanvasW              float32
	inlineCanvasH              float32
	lastCfg                    Config
	lastCfgSet                 bool
	reduceMotion               bool

	fsSavedInlineCam      Camera
	fsSavedInlineCamValid bool
	fsSavedInlineSource   string
	fsSavedInlineLayout   LayoutMode
	fsSavedInlineCanvasW  float32
	fsSavedInlineCanvasH  float32

	fsAnimActive          bool
	fsAnimEntering        bool
	fsAnimStart           time.Time
	fsAnimFromProgress    float32
	fsVisualProgress      float32
	fsCamAnimActive       bool
	fsCamProgress         float32
	fsCamFrom             Camera
	fsCamTo               Camera
	fsCamFromValid        bool
	fsCamToValid          bool
	fsCamFromW, fsCamFromH float32
	fsCamToW, fsCamToH     float32
	fsInlineJustLanded    bool
	fsFocusRestorePending bool
	fsFocusRestoreUntil   time.Time
	fsBorderFocused       bool
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
	st.OutlineUndoNodeID = 0
	st.clearTransientNodeState()
	if st.ImportUndoAfter != "" && src != st.ImportUndoAfter {
		st.ImportUndoBefore = ""
		st.ImportUndoAfter = ""
	}
	st.invalidateLinkStatusCache()
	if st.Root != nil && st.Root.FindByID(st.SelectedID) == nil {
		st.SelectedID = st.Root.ID
	}
	if st.OutlineZoomID != 0 && (st.Root == nil || st.Root.FindByID(st.OutlineZoomID) == nil) {
		st.OutlineZoomID = 0
	}
	if st.FindOpen {
		st.recomputeFind(false)
	}
	st.AutoFitPending = true
}

func (st *BlockState) clearTransientNodeState() {
	st.LinkNodeID = 0
	st.LinkFocusPending = false
	st.LinkBrokenNotice = false
	st.EditingID = 0
	st.EditFocusPending = false
	st.EditSelectAll = false
	st.EditCompose = ""
	st.editLastLayoutText = ""
	st.ComposeText = ""
	st.Measure.ClearTextOverride()
	st.resetDrag()
	st.HoveredID = 0
	st.HoveredHit = HitNone
}

// HasFocus reports whether any interactive MindMap control currently owns keyboard focus.
func (st *BlockState) HasFocus(c *ui.Context) bool {
	if st == nil || c == nil {
		return false
	}
	return st.CanvasHandle.Focused(c) ||
		st.EditHandle.Focused(c) ||
		st.LinkHandle.Focused(c) ||
		st.FindHandle.Focused(c) ||
		st.OutlineHandle.FocusWithin(c) ||
		st.SourceHandle.Focused(c)
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
	if st.ImportUndoAfter != "" && md != st.ImportUndoAfter {
		st.ImportUndoBefore = ""
		st.ImportUndoAfter = ""
	}
	if st.FindOpen {
		st.recomputeFind(false)
	}
	if cb.OnChange != nil {
		cb.OnChange(md)
	}
}

func (st *BlockState) allocID() int {
	st.NextID++
	return st.NextID
}

// --- Core CRUD Operations ---

func (st *BlockState) resetDrag() {
	st.DragNodeID = 0
	st.DragActive = false
	st.DragTargetID = 0
	st.DragPos = DropNone
}

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
	st.EnsureVisiblePending = true
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
	st.EnsureVisiblePending = true
	st.commitChange(cb)
	st.beginEdit(sib, true)
}

func (st *BlockState) opAddSiblingBefore(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	n := st.selectedNode()
	if n == nil {
		return
	}
	if n.Parent == nil {
		// On root, insert as the first child so Shift+Enter always produces a topic.
		st.pushUndo()
		n.Folded = false
		child := &Node{
			ID:     st.allocID(),
			Text:   theme.Tr("新节点", "New Topic"),
			Parent: n,
		}
		n.Children = append([]*Node{child}, n.Children...)
		st.SelectedID = child.ID
		st.EnsureVisiblePending = true
		st.commitChange(cb)
		st.beginEdit(child, true)
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
	copy(kids[idx+1:], kids[idx:])
	kids[idx] = sib
	n.Parent.Children = kids
	st.SelectedID = sib.ID
	st.EnsureVisiblePending = true
	st.commitChange(cb)
	st.beginEdit(sib, true)
}

func (st *BlockState) opInsertParent(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	n := st.selectedNode()
	if n == nil || n.Parent == nil {
		return
	}
	st.pushUndo()
	oldParent := n.Parent
	idx := n.IndexInParent()
	wrapper := &Node{
		ID:       st.allocID(),
		Text:     theme.Tr("新节点", "New Topic"),
		Parent:   oldParent,
		Children: []*Node{n},
	}
	oldParent.Children[idx] = wrapper
	n.Parent = wrapper
	st.SelectedID = wrapper.ID
	st.EnsureVisiblePending = true
	st.commitChange(cb)
	st.beginEdit(wrapper, true)
}

func (st *BlockState) opPromote(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	n := st.selectedNode()
	if n == nil || n.Parent == nil || n.Parent.Parent == nil {
		return
	}
	st.pushUndo()
	oldParent := n.Parent
	grand := oldParent.Parent
	idx := n.IndexInParent()
	oldParent.Children = append(oldParent.Children[:idx], oldParent.Children[idx+1:]...)
	pIdx := oldParent.IndexInParent()
	kids := grand.Children
	kids = append(kids, nil)
	copy(kids[pIdx+2:], kids[pIdx+1:])
	kids[pIdx+1] = n
	grand.Children = kids
	n.Parent = grand
	st.SelectedID = n.ID
	st.EnsureVisiblePending = true
	st.commitChange(cb)
}

func (st *BlockState) opDemote(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	n := st.selectedNode()
	if n == nil || n.Parent == nil {
		return
	}
	idx := n.IndexInParent()
	if idx <= 0 {
		return
	}
	st.pushUndo()
	oldParent := n.Parent
	newParent := oldParent.Children[idx-1]
	oldParent.Children = append(oldParent.Children[:idx], oldParent.Children[idx+1:]...)
	newParent.Folded = false
	n.Parent = newParent
	newParent.Children = append(newParent.Children, n)
	st.SelectedID = n.ID
	st.EnsureVisiblePending = true
	st.commitChange(cb)
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
	st.EnsureVisiblePending = true
	st.commitChange(cb)
}

// opDeleteSingle deletes only the selected node and splices its children into its parent.
func (st *BlockState) opDeleteSingle(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	n := st.selectedNode()
	if n == nil || n.Parent == nil {
		return
	}
	if len(n.Children) == 0 {
		st.opDelete(cb)
		return
	}
	st.pushUndo()
	parent := n.Parent
	idx := n.IndexInParent()
	promoted := make([]*Node, len(n.Children))
	for i, ch := range n.Children {
		ch.Parent = parent
		promoted[i] = ch
	}
	nextKids := make([]*Node, 0, len(parent.Children)-1+len(promoted))
	nextKids = append(nextKids, parent.Children[:idx]...)
	nextKids = append(nextKids, promoted...)
	nextKids = append(nextKids, parent.Children[idx+1:]...)
	parent.Children = nextKids
	st.SelectedID = promoted[0].ID
	st.EnsureVisiblePending = true
	st.commitChange(cb)
}

func (st *BlockState) opDuplicate(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	n := st.selectedNode()
	if n == nil || n.Parent == nil {
		return
	}
	st.pushUndo()
	cp := n.Clone()
	st.NextID = cp.AssignIDs(st.NextID)
	cp.Parent = n.Parent
	idx := n.IndexInParent()
	kids := n.Parent.Children
	kids = append(kids, nil)
	copy(kids[idx+2:], kids[idx+1:])
	kids[idx+1] = cp
	n.Parent.Children = kids
	st.SelectedID = cp.ID
	st.EnsureVisiblePending = true
	st.commitChange(cb)
}

func (st *BlockState) opReparent(dragged, target *Node, pos DragDropPos, cb Callbacks) {
	if cb.ReadOnly || st.Root == nil || dragged == nil || target == nil || dragged.Parent == nil || dragged == target {
		return
	}
	if dragged.IsAncestorOf(target) {
		return
	}
	if (pos == DropBefore || pos == DropAfter) && target.Parent == nil {
		return
	}
	// Skip no-op drops so undo history stays clean.
	switch pos {
	case DropInside:
		if dragged.Parent == target && dragged.IndexInParent() == len(target.Children)-1 {
			return
		}
	case DropBefore:
		if dragged.Parent == target.Parent && dragged.IndexInParent() == target.IndexInParent()-1 {
			return
		}
	case DropAfter:
		if dragged.Parent == target.Parent && dragged.IndexInParent() == target.IndexInParent()+1 {
			return
		}
	default:
		return
	}

	st.pushUndo()
	oldParent := dragged.Parent
	oldIdx := dragged.IndexInParent()
	oldParent.Children = append(oldParent.Children[:oldIdx], oldParent.Children[oldIdx+1:]...)

	if pos == DropInside {
		target.Folded = false
		dragged.Parent = target
		target.Children = append(target.Children, dragged)
	} else {
		newParent := target.Parent
		targetIdx := target.IndexInParent()
		insIdx := targetIdx
		if pos == DropAfter {
			insIdx = targetIdx + 1
		}
		kids := newParent.Children
		kids = append(kids, nil)
		copy(kids[insIdx+1:], kids[insIdx:])
		kids[insIdx] = dragged
		newParent.Children = kids
		dragged.Parent = newParent
	}
	st.SelectedID = dragged.ID
	st.EnsureVisiblePending = true
	st.commitChange(cb)
}

func (st *BlockState) opCopy(c *ui.Context) bool {
	if c == nil || st.Root == nil {
		return false
	}
	n := st.selectedNode()
	if n == nil {
		return false
	}
	text := SerializeSubtreeMarkdown(n)
	if text == "" {
		return false
	}
	c.Services().WriteClipboard(text)
	return true
}

func (st *BlockState) opCut(c *ui.Context, cb Callbacks) bool {
	if cb.ReadOnly || c == nil || st.Root == nil {
		return false
	}
	n := st.selectedNode()
	if n == nil {
		return false
	}
	if !st.opCopy(c) {
		return false
	}
	if n.Parent != nil {
		st.opDelete(cb)
	}
	return true
}

func (st *BlockState) opPaste(c *ui.Context, cb Callbacks) bool {
	if cb.ReadOnly || c == nil || st.Root == nil {
		return false
	}
	n := st.selectedNode()
	if n == nil {
		return false
	}
	raw := c.Services().ReadClipboard()
	nodes := ParseClipboardNodes(raw)
	if len(nodes) == 0 {
		return false
	}
	st.pushUndo()
	n.Folded = false
	for _, ch := range nodes {
		st.NextID = ch.AssignIDs(st.NextID)
		ch.Parent = n
		n.Children = append(n.Children, ch)
	}
	st.SelectedID = nodes[0].ID
	st.EnsureVisiblePending = true
	st.commitChange(cb)
	return true
}

func (st *BlockState) opExpandAll() {
	if st.Root == nil {
		return
	}
	var walk func(n *Node)
	walk = func(n *Node) {
		n.Folded = false
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(st.Root)
}

func (st *BlockState) opCollapseAll() {
	if st.Root == nil {
		return
	}
	var walk func(n *Node)
	walk = func(n *Node) {
		if n.Parent != nil && len(n.Children) > 0 {
			n.Folded = true
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(st.Root)
	// If the currently selected node became hidden, select its nearest visible ancestor.
	if sel := st.selectedNode(); sel != nil {
		cur := sel
		for p := cur.Parent; p != nil; p = p.Parent {
			if p.Folded {
				cur = p
			}
		}
		st.SelectedID = cur.ID
	}
}

func (st *BlockState) hasAnyFolded() bool {
	if st.Root == nil {
		return false
	}
	found := false
	var walk func(n *Node)
	walk = func(n *Node) {
		if found {
			return
		}
		if n.Folded && len(n.Children) > 0 {
			found = true
			return
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(st.Root)
	return found
}

func (st *BlockState) opToggleExpandCollapseAll() {
	if st.hasAnyFolded() {
		st.opExpandAll()
	} else {
		st.opCollapseAll()
	}
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
	st.EnsureVisiblePending = true
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

// opToggleTaskDoneOrCreate converts a non-task topic into an unchecked task,
// or flips completion state if it is already a task (Cmd+Enter).
func (st *BlockState) opToggleTaskDoneOrCreate(n *Node, cb Callbacks) {
	if cb.ReadOnly || n == nil || n.Parent == nil {
		return
	}
	if n.Task == nil {
		st.opToggleTask(n, cb)
		return
	}
	st.opFlipTaskDone(n, cb)
}

func (st *BlockState) opUndo(cb Callbacks) {
	if cb.ReadOnly {
		return
	}
	st.clearTransientNodeState()
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
	st.clearTransientNodeState()
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
	prevID := st.SelectedID
	switch dir {
	case "up":
		if n.Parent != nil {
			if i := n.IndexInParent(); i > 0 {
				st.SelectedID = n.Parent.Children[i-1].ID
			} else if next := st.findVerticalNeighbor(n, -1); next != nil {
				st.SelectedID = next.ID
			}
		}
	case "down":
		if n.Parent != nil {
			if i := n.IndexInParent(); i >= 0 && i+1 < len(n.Parent.Children) {
				st.SelectedID = n.Parent.Children[i+1].ID
			} else if next := st.findVerticalNeighbor(n, 1); next != nil {
				st.SelectedID = next.ID
			}
		}
	case "out":
		if len(n.Children) > 0 && n.Folded {
			n.Folded = false
			st.EnsureVisiblePending = true
			return
		}
		if n.IsOpen() {
			st.SelectedID = n.Children[0].ID
		}
	case "in":
		if n.Parent != nil {
			st.SelectedID = n.Parent.ID
		}
	case "home":
		if st.Root != nil {
			st.SelectedID = st.Root.ID
		}
	}
	if st.SelectedID != prevID {
		st.EnsureVisiblePending = true
	}
}

// findVerticalNeighbor locates the visually nearest visible node strictly above (delta < 0)
// or below (delta > 0) n on the same branch side, preferring nodes at the same depth so
// arrow navigation smoothly crosses branch boundaries like native mind map apps.
func (st *BlockState) findVerticalNeighbor(n *Node, delta int) *Node {
	if st.Root == nil || n == nil {
		return nil
	}
	var bestSame, bestAny *Node
	bestSameDist := float32(1e9)
	bestAnyDist := float32(1e9)
	bestAnyDepthDiff := 1 << 20

	var walk func(cur *Node)
	walk = func(cur *Node) {
		if cur != n && cur.Parent != nil && cur.Dir == n.Dir {
			dy := cur.Y - n.Y
			if (delta < 0 && dy < -0.5) || (delta > 0 && dy > 0.5) {
				dist := float32(math.Abs(float64(dy)))
				if cur.Depth == n.Depth {
					if dist < bestSameDist {
						bestSameDist = dist
						bestSame = cur
					}
				}
				dDiff := cur.Depth - n.Depth
				if dDiff < 0 {
					dDiff = -dDiff
				}
				if dist < bestAnyDist-0.5 || (math.Abs(float64(dist-bestAnyDist)) <= 0.5 && dDiff < bestAnyDepthDiff) {
					bestAnyDist = dist
					bestAnyDepthDiff = dDiff
					bestAny = cur
				}
			}
		}
		if cur.Folded {
			return
		}
		for _, ch := range cur.Children {
			walk(ch)
		}
	}
	walk(st.Root)
	if bestSame != nil {
		return bestSame
	}
	return bestAny
}

func (st *BlockState) ensureNodeVisible(n *Node, vw, vh float32) {
	if n == nil || st.Camera.Scale <= 0 || vw <= 0 || vh <= 0 {
		return
	}
	visW := vw / st.Camera.Scale
	visH := vh / st.Camera.Scale
	padX := min(float32(36)/st.Camera.Scale, visW*0.3)
	padY := min(float32(30)/st.Camera.Scale, visH*0.3)
	left := n.X
	right := n.X + n.W
	top := n.Y - n.H/2
	bottom := n.Y + n.H/2

	if left < st.Camera.X+padX {
		st.Camera.X = left - padX
	} else if right > st.Camera.X+visW-padX {
		st.Camera.X = right - visW + padX
	}
	if top < st.Camera.Y+padY {
		st.Camera.Y = top - padY
	} else if bottom > st.Camera.Y+visH-padY {
		st.Camera.Y = bottom - visH + padY
	}
}

func (st *BlockState) beginEdit(n *Node, fresh bool) {
	if n == nil {
		return
	}
	st.EditingID = n.ID
	st.EditSeq++
	st.EditDraft = n.Text
	st.EditCompose = ""
	st.editLastLayoutText = ""
	st.EditFresh = fresh
	st.EditSelectAll = true
	st.EditFocusPending = true
	st.EnsureVisiblePending = true
	st.ComposeText = ""
	st.LinkNodeID = 0
}

func (st *BlockState) beginEditWithText(n *Node, initial string) {
	if n == nil {
		return
	}
	st.EditingID = n.ID
	st.EditSeq++
	st.EditDraft = initial
	st.EditCompose = ""
	st.editLastLayoutText = ""
	st.EditFresh = false
	st.EditSelectAll = false
	st.EditFocusPending = true
	st.EnsureVisiblePending = true
	st.ComposeText = ""
	st.LinkNodeID = 0
}

func (st *BlockState) finishEdit(commit bool, cb Callbacks) {
	if st.EditingID == 0 {
		return
	}
	n := st.Root.FindByID(st.EditingID)
	st.EditingID = 0
	st.EditCompose = ""
	st.editLastLayoutText = ""
	st.EditFocusPending = false
	st.EditSelectAll = false
	st.ComposeText = ""
	st.Measure.ClearTextOverride()
	if commit && n != nil {
		trimmed := normalizeNodeText(st.EditDraft)
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

func (st *BlockState) canUndo(cb Callbacks) bool {
	if cb.ReadOnly {
		return false
	}
	if cb.CanUndo != nil {
		return cb.CanUndo()
	}
	return len(st.Undo) > 0
}

func (st *BlockState) canRedo(cb Callbacks) bool {
	if cb.ReadOnly {
		return false
	}
	if cb.CanRedo != nil {
		return cb.CanRedo()
	}
	return len(st.Redo) > 0
}

func (st *BlockState) invalidateLinkStatusCache() {
	if st.linkStatusCache != nil {
		clear(st.linkStatusCache)
	}
	st.linkStatusChecked = time.Time{}
}

func (st *BlockState) resolveLinkStatusCached(now time.Time, target string, cb Callbacks) LinkStatus {
	s := strings.TrimSpace(target)
	if s == "" {
		return LinkStatusNone
	}
	if ClassifyLink(s) == LinkWeb {
		return LinkStatusExternal
	}
	if cb.OnCheckLinkStatus == nil {
		return LinkStatusValid
	}
	if st.linkStatusCache == nil || st.linkStatusChecked.IsZero() || now.Sub(st.linkStatusChecked) >= 2*time.Second {
		st.linkStatusCache = make(map[string]LinkStatus, 8)
		st.linkStatusChecked = now
	}
	if cached, ok := st.linkStatusCache[s]; ok {
		return cached
	}
	status := cb.OnCheckLinkStatus(s)
	st.linkStatusCache[s] = status
	return status
}

func (st *BlockState) checkLinkStatusFresh(now time.Time, target string, cb Callbacks) LinkStatus {
	s := strings.TrimSpace(target)
	if s == "" {
		return LinkStatusNone
	}
	if ClassifyLink(s) == LinkWeb {
		return LinkStatusExternal
	}
	if cb.OnCheckLinkStatus == nil {
		return LinkStatusValid
	}
	status := cb.OnCheckLinkStatus(s)
	if st.linkStatusCache == nil {
		st.linkStatusCache = make(map[string]LinkStatus, 8)
	}
	st.linkStatusCache[s] = status
	st.linkStatusChecked = now
	return status
}

func (st *BlockState) openNodeLink(now time.Time, n *Node, cb Callbacks) {
	if n == nil {
		return
	}
	target := strings.TrimSpace(n.Link)
	if target == "" {
		return
	}
	status := st.checkLinkStatusFresh(now, target, cb)
	if status == LinkStatusBroken {
		if !cb.ReadOnly {
			st.SelectedID = n.ID
			st.beginLinkEditBroken(n)
		} else if cb.OnNotify != nil {
			cb.OnNotify(theme.Tr("目标笔记不存在或已移动", "Linked note is missing or moved"), "warning")
		}
		return
	}
	if st.Fullscreen || st.fsAnimActive {
		st.ExitFullscreen(cb)
	}
	if cb.OnOpenLink != nil {
		cb.OnOpenLink(target)
	}
}

func (st *BlockState) beginLinkEdit(n *Node) {
	if n == nil {
		return
	}
	st.EditingID = 0
	st.ExportMenuOpen = false
	st.ShortcutsOpen = false
	st.ImportResult = nil
	st.LinkNodeID = n.ID
	st.LinkDraft = n.Link
	st.LinkActiveIdx = -1
	st.LinkBrokenNotice = false
	st.LinkListScroll.Y = 0
	st.LinkFocusPending = true
}

func (st *BlockState) beginLinkEditBroken(n *Node) {
	st.beginLinkEdit(n)
	st.LinkBrokenNotice = true
}

func (st *BlockState) applyLink(target string, cb Callbacks) {
	if cb.ReadOnly || st.LinkNodeID == 0 || st.Root == nil {
		st.LinkNodeID = 0
		st.LinkBrokenNotice = false
		return
	}
	n := st.Root.FindByID(st.LinkNodeID)
	st.LinkNodeID = 0
	st.LinkFocusPending = false
	st.LinkBrokenNotice = false
	if n == nil {
		return
	}
	target = strings.TrimSpace(target)
	if target != n.Link {
		st.pushUndo()
		n.Link = target
		st.invalidateLinkStatusCache()
		st.commitChange(cb)
	}
}

// OpenFind opens the in-canvas MindMap find bar (switching to Map view if needed).
func (st *BlockState) OpenFind() {
	if st == nil {
		return
	}
	if st.View != ViewMap {
		st.View = ViewMap
		st.AutoFitPending = true
	}
	st.ExportMenuOpen = false
	st.ShortcutsOpen = false
	st.LinkNodeID = 0
	st.FindOpen = true
	st.FindFocusPending = true
	if strings.TrimSpace(st.FindQuery) != "" {
		st.recomputeFind(true)
	}
}

// CloseFind closes the in-canvas MindMap find bar and clears highlights.
func (st *BlockState) CloseFind() {
	if st == nil {
		return
	}
	st.FindOpen = false
	st.FindFocusPending = false
	st.FindMatchIDs = nil
	st.FindMatchSet = nil
	st.FindCurrentIdx = 0
}

func (st *BlockState) recomputeFind(expandAndCenter bool) {
	matches := FindMatches(st.Root, st.FindQuery)
	if len(matches) == 0 {
		st.FindMatchIDs = nil
		st.FindMatchSet = nil
		st.FindCurrentIdx = 0
		return
	}
	ids := make([]int, len(matches))
	set := make(map[int]bool, len(matches))
	for i, m := range matches {
		ids[i] = m.ID
		set[m.ID] = true
		if expandAndCenter {
			ExpandAncestors(m)
		}
	}
	st.FindMatchIDs = ids
	st.FindMatchSet = set
	if st.FindCurrentIdx < 0 || st.FindCurrentIdx >= len(ids) {
		st.FindCurrentIdx = 0
	}
	if expandAndCenter {
		curID := ids[st.FindCurrentIdx]
		st.SelectedID = curID
		st.EnsureVisiblePending = true
	}
}

func (st *BlockState) stepFind(delta int) {
	n := len(st.FindMatchIDs)
	if n == 0 || st.Root == nil {
		return
	}
	st.FindCurrentIdx = ((st.FindCurrentIdx+delta)%n + n) % n
	curID := st.FindMatchIDs[st.FindCurrentIdx]
	if node := st.Root.FindByID(curID); node != nil {
		ExpandAncestors(node)
		st.SelectedID = node.ID
		st.EnsureVisiblePending = true
	}
}

// ToggleLayout switches between right-side and balanced layouts.
// viewOrder is the ⌘1 / ⌘2 / ⌘3 order of the header view tabs.
var viewOrder = [...]ViewMode{ViewMap, ViewOutline, ViewSource}

func viewForDigitKey(key ui.Key) (ViewMode, bool) {
	if key >= ui.Key1 && int(key-ui.Key1) < len(viewOrder) {
		return viewOrder[key-ui.Key1], true
	}
	return "", false
}

func viewShortcutLabel(mode ViewMode) string {
	for i, m := range viewOrder {
		if m == mode {
			if theme.IsMacOS() {
				return fmt.Sprintf("⌘%d", i+1)
			}
			return fmt.Sprintf("Ctrl+%d", i+1)
		}
	}
	return ""
}

// SwitchView changes the active view tab. focus moves keyboard focus into the
// new view so chained shortcuts keep working without a mouse click.
func (st *BlockState) SwitchView(mode ViewMode, focus bool, cb Callbacks) {
	if st == nil {
		return
	}
	st.finishEdit(true, cb)
	st.LinkNodeID = 0
	st.ExportMenuOpen = false
	st.ShortcutsOpen = false
	st.OutlineUndoNodeID = 0
	if st.View != mode {
		st.HintScroll.X = 0
	}
	st.View = mode
	st.fsCamAnimActive = false
	if mode == ViewMap {
		st.AutoFitPending = true
	}
	if !focus {
		return
	}
	st.viewFocusPending = true
	if mode == ViewOutline {
		target := st.outlineActiveRoot()
		if st.Root != nil {
			if n := st.Root.FindByID(st.SelectedID); n != nil {
				target = n
			}
		}
		if target != nil {
			st.focusOutlineNode(target.ID, utf8.RuneCountInString(target.Text))
		}
		st.viewFocusPending = false
	}
	if cb.OnRequestFocus != nil {
		cb.OnRequestFocus()
	}
}

// captureBlockTextKey queues block-scoped chords typed inside a text field so
// the field never swallows them.
func (st *BlockState) captureBlockTextKey(k ui.TextKey) bool {
	if isOutlinePrimaryMod(k.Mods) {
		if mode, ok := viewForDigitKey(k.Key); ok {
			st.pendingView = mode
			return true
		}
	}
	if isOutlinePrimaryShiftMod(k.Mods) && k.Key == ui.KeyEnter {
		st.pendingFSToggle = true
		return true
	}
	return false
}

// handleBlockShortcuts runs chords scoped to this block. They only fire while
// the block owns focus or is fullscreen, so ⌘1-9 still switch editor tabs
// everywhere else.
func (st *BlockState) handleBlockShortcuts(c *ui.Context, fullscreen bool, cb Callbacks) {
	view, fsToggle := st.pendingView, st.pendingFSToggle
	st.pendingView, st.pendingFSToggle = "", false
	if !fullscreen && !st.HasFocus(c) && view == "" && !fsToggle {
		return
	}
	if view == "" {
		for _, mode := range viewOrder {
			if d := viewDigitKey(mode); c.Shortcut(ui.Cmd, d) {
				view = mode
				break
			}
		}
	}
	if !fsToggle && c.Shortcut(ui.Cmd|ui.Shift, ui.KeyEnter) {
		fsToggle = true
	}
	if view != "" {
		st.SwitchView(view, true, cb)
		c.Invalidate()
	}
	if fsToggle {
		if fullscreen {
			st.ExitFullscreen(cb)
		} else {
			st.EnterFullscreen(cb)
		}
		c.Invalidate()
	}
}

func viewDigitKey(mode ViewMode) ui.Key {
	for i, m := range viewOrder {
		if m == mode {
			return ui.Key1 + ui.Key(i)
		}
	}
	return 0
}

func (st *BlockState) ToggleLayout() {
	if st == nil {
		return
	}
	if st.Layout == LayoutRight {
		st.Layout = LayoutBoth
	} else {
		st.Layout = LayoutRight
	}
	st.LayoutSet = true
	st.fsCamAnimActive = false
	st.AutoFitPending = true
}

// OpenExportMenu opens the MindMap export menu popover.
func (st *BlockState) OpenExportMenu() {
	if st == nil {
		return
	}
	if st.View == ViewSource {
		st.View = ViewMap
		st.AutoFitPending = true
	}
	st.LinkNodeID = 0
	st.ShortcutsOpen = false
	st.ImportResult = nil
	st.ImportUndoBefore = ""
	st.ImportUndoAfter = ""
	st.ExportMenuScroll.Y = 0
	st.ExportMenuOpen = true
}

func (st *BlockState) applyImportedTree(root *Node, report ImportReport, cb Callbacks) {
	if cb.ReadOnly || root == nil {
		return
	}
	prevSource := st.LastSource
	nextSource := SerializeMarkdown(root)
	changed := nextSource != prevSource
	if changed {
		st.pushUndo()
	}
	st.clearTransientNodeState()
	st.Root = root
	st.NextID = maxNodeID(root)
	st.SelectedID = root.ID
	st.AutoFitPending = true
	st.View = ViewMap
	st.invalidateLinkStatusCache()
	if report.HasDropped() || report.Nodes > 0 {
		rep := report
		st.ImportResult = &rep
	}
	if changed {
		st.ImportUndoBefore = prevSource
		st.ImportUndoAfter = nextSource
		st.commitChange(cb)
	} else {
		st.ImportUndoBefore = ""
		st.ImportUndoAfter = ""
		if st.FindOpen {
			st.recomputeFind(false)
		}
	}
}

func (st *BlockState) canUndoImport(cb Callbacks) bool {
	if cb.ReadOnly || st.ImportUndoBefore == "" || st.ImportUndoAfter == "" {
		return false
	}
	return st.LastSource == st.ImportUndoAfter
}

func (st *BlockState) opUndoImport(cb Callbacks) {
	if !st.canUndoImport(cb) {
		st.ImportResult = nil
		st.ImportUndoBefore = ""
		st.ImportUndoAfter = ""
		return
	}
	before := st.ImportUndoBefore
	st.ImportResult = nil
	st.ImportUndoBefore = ""
	st.ImportUndoAfter = ""
	st.clearTransientNodeState()
	st.pushUndo()
	restored := ParseMarkdown(before)
	st.Root = restored
	st.NextID = max(st.NextID, maxNodeID(restored))
	st.SelectedID = restored.ID
	st.AutoFitPending = true
	st.invalidateLinkStatusCache()
	st.commitChange(cb)
}

func (st *BlockState) triggerImport(cb Callbacks) {
	if cb.ReadOnly || cb.OnPickImportFile == nil {
		return
	}
	st.ExportMenuOpen = false
	st.ShortcutsOpen = false
	cb.OnPickImportFile(func(root *Node, report ImportReport) {
		st.applyImportedTree(root, report, cb)
	})
}

func (st *BlockState) triggerExport(format Format, cb Callbacks) {
	if st.Root == nil || cb.OnSaveExportFile == nil {
		return
	}
	st.ExportMenuOpen = false
	pal := ResolveExportPalette(st.ExportTheme)
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

func (st *BlockState) copyPNGToClipboard(c *ui.Context, cb Callbacks) {
	if st.Root == nil || c == nil {
		return
	}
	st.ExportMenuOpen = false
	pal := ResolveExportPalette(st.ExportTheme)
	data, err := ExportPNG(st.Root, st.Layout, pal)
	if err != nil {
		if cb.OnNotify != nil {
			cb.OnNotify(theme.Tr("复制图片失败：", "Copy PNG failed: ")+err.Error(), "danger")
		}
		return
	}
	c.Services().WriteClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, data))))
	if cb.OnNotify != nil {
		cb.OnNotify(theme.Tr("已复制 PNG 图片", "Copied PNG image"), "success")
	}
}

func (st *BlockState) copyMarkdownToClipboard(c *ui.Context, cb Callbacks) {
	if st.Root == nil || c == nil {
		return
	}
	st.ExportMenuOpen = false
	c.Services().WriteClipboard(SerializeMarkdown(st.Root))
	if cb.OnNotify != nil {
		cb.OnNotify(theme.Tr("已复制 Markdown", "Copied Markdown"), "success")
	}
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

const (
	fsEnterDuration = 220 * time.Millisecond
	fsExitDuration  = 190 * time.Millisecond
)

var fsTransitionEase = ui.CubicBezier(0.22, 1, 0.36, 1)

// SetEditorBounds records the host editor root rectangle in window DIPs so
// fullscreen enter/exit transitions align seamlessly with the inline card.
func (st *BlockState) SetEditorBounds(r ui.Rect) {
	if st == nil {
		return
	}
	if r.W > 0 && r.H > 0 {
		st.editorBounds = r
		st.editorBoundsValid = true
	} else {
		st.editorBounds = ui.Rect{}
		st.editorBoundsValid = false
	}
}

// IsFullscreenOverlayActive reports whether the fullscreen overlay should be
// rendered (either logically open or animating out back to the inline card).
func (st *BlockState) IsFullscreenOverlayActive() bool {
	if st == nil {
		return false
	}
	return st.Fullscreen || st.fsAnimActive
}

func (st *BlockState) effectiveCfg() Config {
	if st.lastCfgSet {
		return st.lastCfg
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	return cfg
}

func (st *BlockState) inlineRelRect() ui.Rect {
	return ui.Rect{
		X: st.inlineCardRect.X - st.editorBounds.X,
		Y: st.inlineCardRect.Y - st.editorBounds.Y,
		W: st.inlineCardRect.W,
		H: st.inlineCardRect.H,
	}
}

func clipRect(r, bounds ui.Rect) ui.Rect {
	x0 := max(r.X, bounds.X)
	y0 := max(r.Y, bounds.Y)
	x1 := min(r.X+r.W, bounds.X+bounds.W)
	y1 := min(r.Y+r.H, bounds.Y+bounds.H)
	if x1 <= x0 || y1 <= y0 {
		return ui.Rect{}
	}
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func (st *BlockState) clippedInlineRelRect() ui.Rect {
	if !st.editorBoundsValid || st.editorBounds.W <= 0 || st.editorBounds.H <= 0 {
		return ui.Rect{}
	}
	if !st.inlineCardRectValid || st.inlineCardRect.W <= 0 || st.inlineCardRect.H <= 0 {
		return ui.Rect{}
	}
	return clipRect(st.inlineRelRect(), ui.Rect{W: st.editorBounds.W, H: st.editorBounds.H})
}

func (st *BlockState) isInlineRendered() bool {
	if !st.inlineRenderedThisFrame && !st.inlineRenderedLastFrame {
		return false
	}
	if st.editorBoundsValid && st.inlineCardRectValid {
		clipped := st.clippedInlineRelRect()
		return clipped.W > 0 && clipped.H > 0
	}
	return true
}

func (st *BlockState) hasValidFullscreenTransitionCoords() bool {
	if !st.inlineRenderedThisFrame && !st.inlineRenderedLastFrame {
		return false
	}
	clipped := st.clippedInlineRelRect()
	return clipped.W >= 60 && clipped.H >= 60
}

func (st *BlockState) shouldAnimateFullscreen() bool {
	if testing.Testing() || bui.IsProbeEnabled() || st.reduceMotion {
		return false
	}
	return st.hasValidFullscreenTransitionCoords()
}

func (st *BlockState) expectedCanvasSize(fullscreen bool) (float32, float32) {
	cfg := st.effectiveCfg()
	if fullscreen {
		if st.editorBoundsValid && st.editorBounds.W > 0 && st.editorBounds.H > 0 {
			h := st.editorBounds.H - theme.HeightLarge
			if cfg.ShowHints && st.View != ViewSource {
				h -= theme.HeightControl
			}
			if h < 1 {
				h = 1
			}
			return st.editorBounds.W, h
		}
		return 600, 360
	}
	var w float32
	if st.inlineCardRectValid && st.inlineCardRect.W > 2 {
		w = st.inlineCardRect.W - 2
	} else if st.inlineCanvasW > 0 {
		w = st.inlineCanvasW
	}
	if w <= 0 {
		w = 600
	}
	h := cfg.InlineHeightDIP()
	if h <= 0 {
		h = st.inlineCanvasH
	}
	if h <= 0 {
		h = 360
	}
	return w, h
}

func (st *BlockState) canReuseSavedInlineCam() bool {
	if !st.fsSavedInlineCamValid || st.LastSource != st.fsSavedInlineSource || st.Layout != st.fsSavedInlineLayout {
		return false
	}
	if st.fsSavedInlineCanvasW <= 0 || st.fsSavedInlineCanvasH <= 0 {
		return false
	}
	curW, curH := st.expectedCanvasSize(false)
	if curW <= 0 || curH <= 0 {
		return false
	}
	return math.Abs(float64(curW-st.fsSavedInlineCanvasW)) <= 2 &&
		math.Abs(float64(curH-st.fsSavedInlineCanvasH)) <= 2
}

func (st *BlockState) currentInlineCanvasSize() (float32, float32) {
	if st.inlineCanvasW > 0 && st.inlineCanvasH > 0 {
		return st.inlineCanvasW, st.inlineCanvasH
	}
	return st.expectedCanvasSize(false)
}

func (st *BlockState) prepareFullscreenTransition(cb Callbacks) {
	st.finishEdit(true, cb)
	st.ComposeText = ""
	st.LinkNodeID = 0
	st.LinkFocusPending = false
	st.LinkBrokenNotice = false
	st.ExportMenuOpen = false
	st.ShortcutsOpen = false
	st.ImportResult = nil
	st.ImportUndoBefore = ""
	st.ImportUndoAfter = ""
	st.Panning = false
	st.resetDrag()
}

// EnterFullscreen enters fullscreen mode cleanly, capturing inline layout and
// camera state for a smooth expansion transition.
func (st *BlockState) EnterFullscreen(cb Callbacks) {
	if st == nil {
		return
	}
	st.prepareFullscreenTransition(cb)
	if st.Fullscreen {
		return
	}
	if !st.fsAnimActive {
		st.fsSavedInlineCam = st.Camera
		st.fsSavedInlineCamValid = st.CameraInit
		st.fsSavedInlineSource = st.LastSource
		st.fsSavedInlineLayout = st.Layout
		st.fsSavedInlineCanvasW, st.fsSavedInlineCanvasH = st.currentInlineCanvasSize()
		st.fsBorderFocused = st.lastInlineFocused
	}
	st.Fullscreen = true
	st.fsInlineJustLanded = false
	st.fsFocusRestorePending = false
	st.fsFocusRestoreUntil = time.Time{}

	if st.shouldAnimateFullscreen() {
		fromP := float32(0)
		if st.fsAnimActive {
			fromP = clampFloat32(st.fsVisualProgress, 0, 1)
		}
		st.fsAnimActive = true
		st.fsAnimEntering = true
		st.fsAnimFromProgress = fromP
		st.fsVisualProgress = fromP
		st.fsAnimStart = time.Time{}
		st.fsCamProgress = 0
		st.fsCamToValid = false
		if st.View == ViewMap && st.Root != nil {
			st.fsCamAnimActive = true
			if fromP > 0 && st.CameraInit && st.LastCanvasW > 0 && st.LastCanvasH > 0 {
				st.fsCamFrom = st.Camera
				st.fsCamFromW, st.fsCamFromH = st.LastCanvasW, st.LastCanvasH
				st.fsCamFromValid = true
			} else if st.fsSavedInlineCamValid && st.fsSavedInlineCanvasW > 0 && st.fsSavedInlineCanvasH > 0 {
				st.fsCamFrom = st.fsSavedInlineCam
				st.fsCamFromW, st.fsCamFromH = st.fsSavedInlineCanvasW, st.fsSavedInlineCanvasH
				st.fsCamFromValid = true
			} else {
				st.fsCamFromValid = false
			}
		} else {
			st.fsCamAnimActive = false
		}
		st.AutoFitPending = false
		return
	}

	st.fsAnimActive = false
	st.fsAnimEntering = false
	st.fsVisualProgress = 1
	st.fsCamAnimActive = false
	st.AutoFitPending = true
}

func (st *BlockState) finishExitAnimation() {
	st.fsAnimActive = false
	st.fsAnimEntering = false
	st.fsVisualProgress = 0
	st.fsCamAnimActive = false
	st.fsInlineJustLanded = true
	if st.isInlineRendered() {
		st.fsFocusRestorePending = true
		st.fsFocusRestoreUntil = time.Now().Add(150 * time.Millisecond)
	} else {
		st.fsFocusRestorePending = false
		st.fsFocusRestoreUntil = time.Time{}
	}

	unchanged := st.canReuseSavedInlineCam()
	if unchanged {
		st.Camera = st.fsSavedInlineCam
		st.CameraInit = true
		st.AutoFitPending = false
	} else if st.fsCamToValid {
		st.Camera = st.fsCamTo
		st.CameraInit = true
		st.AutoFitPending = false
	} else {
		st.AutoFitPending = true
	}
	if w, h := st.expectedCanvasSize(false); w > 0 && h > 0 {
		st.LastCanvasW, st.LastCanvasH = w, h
	} else {
		st.LastCanvasW, st.LastCanvasH = 0, 0
	}
}

func (st *BlockState) consumeFullscreenFocusRestore() bool {
	if !st.fsFocusRestorePending {
		return false
	}
	st.fsFocusRestorePending = false
	until := st.fsFocusRestoreUntil
	st.fsFocusRestoreUntil = time.Time{}
	if !until.IsZero() && time.Now().After(until) {
		return false
	}
	return true
}

// CancelFullscreen immediately clears fullscreen, animation, edit, and popup states
// without scheduling focus restoration (used when the block is deleted or converted).
func (st *BlockState) CancelFullscreen() {
	if st == nil {
		return
	}
	st.Fullscreen = false
	st.fsAnimActive = false
	st.fsAnimEntering = false
	st.fsAnimStart = time.Time{}
	st.fsAnimFromProgress = 0
	st.fsVisualProgress = 0
	st.fsCamAnimActive = false
	st.fsCamProgress = 0
	st.fsCamFromValid = false
	st.fsCamToValid = false
	st.fsInlineJustLanded = false
	st.fsFocusRestorePending = false
	st.fsFocusRestoreUntil = time.Time{}
	st.fsBorderFocused = false
	st.inlineBodyMountedThisFrame = false
	st.inlineRenderedThisFrame = false
	st.inlineRenderedLastFrame = false
	st.EditingID = 0
	st.EditDraft = ""
	st.EditCompose = ""
	st.editLastLayoutText = ""
	st.EditFocusPending = false
	st.ComposeText = ""
	st.LinkNodeID = 0
	st.LinkFocusPending = false
	st.LinkBrokenNotice = false
	st.ExportMenuOpen = false
	st.ShortcutsOpen = false
	st.ImportResult = nil
	st.ImportUndoBefore = ""
	st.ImportUndoAfter = ""
	st.Panning = false
	st.resetDrag()
	st.Measure.ClearTextOverride()
	st.SetEditorBounds(ui.Rect{})
}

func (st *BlockState) snapFullscreenTransition() {
	st.fsAnimActive = false
	st.fsAnimEntering = false
	if st.Fullscreen {
		st.fsVisualProgress = 1
		st.fsCamAnimActive = false
		st.AutoFitPending = true
		return
	}
	st.finishExitAnimation()
}

// ExitFullscreen exits fullscreen mode cleanly, collapsing smoothly back to the
// inline card and restoring or refitting the inline camera.
func (st *BlockState) ExitFullscreen(cb Callbacks) {
	if st == nil {
		return
	}
	st.prepareFullscreenTransition(cb)
	if !st.Fullscreen && !st.fsAnimActive {
		return
	}
	if !st.Fullscreen && st.fsAnimActive && !st.fsAnimEntering && st.shouldAnimateFullscreen() {
		return
	}
	wasFullscreen := st.Fullscreen
	st.Fullscreen = false
	st.fsFocusRestorePending = false
	st.fsFocusRestoreUntil = time.Time{}
	unchanged := st.canReuseSavedInlineCam()

	if st.shouldAnimateFullscreen() && (wasFullscreen || st.fsVisualProgress > 0) {
		fromP := float32(1)
		if st.fsAnimActive {
			fromP = clampFloat32(st.fsVisualProgress, 0, 1)
		}
		st.fsAnimActive = true
		st.fsAnimEntering = false
		st.fsAnimFromProgress = fromP
		st.fsVisualProgress = fromP
		st.fsAnimStart = time.Time{}
		st.fsCamProgress = 0
		if st.View == ViewMap && st.Root != nil {
			st.fsCamAnimActive = true
			st.fsCamFrom = st.Camera
			st.fsCamFromW, st.fsCamFromH = st.LastCanvasW, st.LastCanvasH
			st.fsCamFromValid = st.CameraInit && st.LastCanvasW > 0 && st.LastCanvasH > 0
			if unchanged && st.fsSavedInlineCanvasW > 0 && st.fsSavedInlineCanvasH > 0 {
				st.fsCamTo = st.fsSavedInlineCam
				st.fsCamToW, st.fsCamToH = st.fsSavedInlineCanvasW, st.fsSavedInlineCanvasH
				st.fsCamToValid = true
			} else {
				st.fsCamToValid = false
			}
		} else {
			st.fsCamAnimActive = false
		}
		st.AutoFitPending = false
		return
	}

	st.fsCamToValid = false
	st.finishExitAnimation()
}

func (st *BlockState) inlinePlaceholderInnerHeight(cfg Config) float32 {
	h := theme.HeightLarge + cfg.InlineHeightDIP()
	if cfg.ShowHints && st.View != ViewSource {
		h += theme.HeightControl
	}
	return h
}

func lerpFloat32(a, b, t float32) float32 {
	return a + (b-a)*t
}

func lerpRect(a, b ui.Rect, t float32) ui.Rect {
	return ui.Rect{
		X: lerpFloat32(a.X, b.X, t),
		Y: lerpFloat32(a.Y, b.Y, t),
		W: lerpFloat32(a.W, b.W, t),
		H: lerpFloat32(a.H, b.H, t),
	}
}

func lerpCameraViewport(from, to Camera, fromW, fromH, toW, toH, curW, curH, t float32) Camera {
	if t <= 0 {
		return from
	}
	if t >= 1 {
		return to
	}
	fromScale := from.Scale
	if fromScale <= 0 {
		fromScale = 1
	}
	toScale := to.Scale
	if toScale <= 0 {
		toScale = 1
	}
	scale := lerpFloat32(fromScale, toScale, t)
	if scale <= 0 {
		scale = 1
	}
	if fromW > 0 && fromH > 0 && toW > 0 && toH > 0 && curW > 0 && curH > 0 {
		fromCX := from.X + (fromW*0.5)/fromScale
		fromCY := from.Y + (fromH*0.5)/fromScale
		toCX := to.X + (toW*0.5)/toScale
		toCY := to.Y + (toH*0.5)/toScale
		cx := lerpFloat32(fromCX, toCX, t)
		cy := lerpFloat32(fromCY, toCY, t)
		return Camera{
			X:     cx - (curW*0.5)/scale,
			Y:     cy - (curH*0.5)/scale,
			Scale: scale,
		}
	}
	return Camera{
		X:     lerpFloat32(from.X, to.X, t),
		Y:     lerpFloat32(from.Y, to.Y, t),
		Scale: scale,
	}
}

// --- Inline & Fullscreen Render Entry Points ---

// RenderInline renders the interactive Ocean Paper MindMap block inside the note editor.
func (st *BlockState) RenderInline(c *ui.Context, id string, source string, cfg Config, focused bool, cb Callbacks) {
	st.inlineRenderedThisFrame = true
	st.inlineRenderedLastFrame = true
	st.SyncSource(source, cfg)
	st.lastCfg = cfg
	st.lastCfgSet = true
	st.reduceMotion = c.Preferences().ReduceMotion
	st.lastInlineFocused = focused || st.HasFocus(c) || st.EditingID != 0
	if !st.IsFullscreenOverlayActive() {
		st.handleBlockShortcuts(c, false, cb)
	}
	pal := theme.Ocean()
	borderCol := pal.Hairline
	if st.lastInlineFocused {
		borderCol = pal.Accent
	}

	card := ui.Column(c.Key("mm-card-"+id)).FillWidth().Radius(theme.RadiusLG).
		Background(pal.Paper).Border(1, borderCol).Clip()
	if cr := card.LayoutBounds(); cr.W > 0 && cr.H > 0 {
		st.inlineCardRect = cr
		st.inlineCardRectValid = true
	}
	card.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		if r.W > 0 && r.H > 0 {
			st.inlineCardRect = r
			st.inlineCardRectValid = true
		}
		if bui.IsProbeEnabled() {
			bui.RecordBounds("mm-card-"+id, r)
		}
	})
	card.Children(func() {
		if st.IsFullscreenOverlayActive() {
			// Keep only one active interactive canvas mounted while fullscreen (or its exit
			// animation) is active, preserving the exact inline height to prevent scroll jump.
			placeholderH := st.inlinePlaceholderInnerHeight(cfg)
			box := ui.Column(c.Key("mm-inline-fs-placeholder-"+id)).FillWidth().Height(placeholderH).
				Padding(theme.Space16).AlignItems(ui.Center).Justify(ui.Center).Gap(theme.Space10).
				Background(pal.CodeBG)
			box.Children(func() {
				ui.Text(c, theme.Tr("正在全屏查看思维导图", "Mind map is open in fullscreen")).
					FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
				exitBtn := ui.ButtonBase(c.Key("mm-inline-fs-exit-"+id)).
					Height(theme.HeightChrome).PaddingX(theme.Space12).Radius(theme.RadiusSM).
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
					st.ExitFullscreen(cb)
					c.Invalidate()
				}
			})
			return
		}
		st.renderHeader(c, id, false, cb)
		if st.IsFullscreenOverlayActive() {
			// Fullscreen was just toggled on during header click; defer body/hint rendering
			// to RenderFullscreen in this frame so ui.Handle/ScrollState are never bound twice.
			c.Invalidate()
			return
		}
		st.inlineBodyMountedThisFrame = true
		st.renderBody(c, id, cfg.InlineHeightDIP(), false, cb)
		if cfg.ShowHints && st.View != ViewSource {
			st.renderHintBar(c, id)
		}
	})
}

// RenderFullscreen renders the full-window interactive MindMap overlay when Fullscreen is active.
func (st *BlockState) RenderFullscreen(c *ui.Context, id string, source string, cfg Config, cb Callbacks) {
	if st == nil {
		return
	}
	if !st.IsFullscreenOverlayActive() {
		st.inlineBodyMountedThisFrame = false
		st.inlineRenderedLastFrame = false
		st.inlineRenderedThisFrame = false
		return
	}
	st.inlineRenderedLastFrame = st.inlineRenderedThisFrame
	st.inlineRenderedThisFrame = false
	if st.inlineBodyMountedThisFrame {
		// RenderInline already mounted live handles in this frame; defer overlay mount to next frame.
		st.inlineBodyMountedThisFrame = false
		c.Invalidate()
		return
	}
	st.SyncSource(source, cfg)
	st.lastCfg = cfg
	st.lastCfgSet = true
	st.reduceMotion = c.Preferences().ReduceMotion
	pal := theme.Ocean()

	if st.fsAnimActive {
		if testing.Testing() || bui.IsProbeEnabled() || st.reduceMotion || !st.hasValidFullscreenTransitionCoords() {
			st.snapFullscreenTransition()
			if !st.Fullscreen {
				c.Invalidate()
				return
			}
		} else {
			now := c.Now()
			if now.IsZero() {
				now = time.Now()
			}
			if st.fsAnimStart.IsZero() {
				st.fsAnimStart = now
			}
			if st.fsAnimEntering {
				dur := time.Duration(float64(fsEnterDuration) * float64(max(1-st.fsAnimFromProgress, 0)))
				if dur <= 0 {
					st.fsAnimActive = false
					st.fsVisualProgress = 1
					st.fsCamProgress = 1
				} else {
					rawT := clampFloat32(float32(now.Sub(st.fsAnimStart))/float32(dur), 0, 1)
					eased := fsTransitionEase(rawT)
					st.fsCamProgress = eased
					st.fsVisualProgress = st.fsAnimFromProgress + (1-st.fsAnimFromProgress)*eased
					if rawT >= 1 {
						st.fsAnimActive = false
						st.fsVisualProgress = 1
					} else {
						c.AnimationFrame()
					}
				}
			} else {
				dur := time.Duration(float64(fsExitDuration) * float64(max(st.fsAnimFromProgress, 0)))
				if dur <= 0 {
					st.fsCamProgress = 1
					st.finishExitAnimation()
					c.Invalidate()
				} else {
					rawT := clampFloat32(float32(now.Sub(st.fsAnimStart))/float32(dur), 0, 1)
					eased := fsTransitionEase(rawT)
					st.fsCamProgress = eased
					st.fsVisualProgress = st.fsAnimFromProgress * (1 - eased)
					if rawT >= 1 {
						st.finishExitAnimation()
						c.Invalidate()
					} else {
						c.AnimationFrame()
					}
				}
			}
		}
	}

	if st.Fullscreen && st.EditingID == 0 && st.LinkNodeID == 0 && !st.FindHandle.Focused(c) && !st.OutlineHandle.FocusWithin(c) && c.Shortcut(0, ui.KeyEscape) {
		if st.DragActive || st.DragNodeID != 0 {
			st.resetDrag()
		} else if st.FindOpen {
			st.CloseFind()
		} else if st.ShortcutsOpen {
			st.ShortcutsOpen = false
		} else if st.ImportResult != nil {
			st.ImportResult = nil
		} else if st.ExportMenuOpen {
			st.ExportMenuOpen = false
		} else if st.View == ViewOutline && st.OutlineZoomID != 0 {
			if cur := st.outlineActiveRoot(); cur != nil && cur != st.Root && cur.Parent != nil && cur.Parent != st.Root {
				st.OutlineZoomID = cur.Parent.ID
			} else {
				st.OutlineZoomID = 0
			}
			c.Invalidate()
			return
		} else {
			st.ExitFullscreen(cb)
			c.Invalidate()
			return
		}
	}
	if st.Fullscreen && !st.fsAnimActive {
		st.handleBlockShortcuts(c, true, cb)
	}
	if st.Fullscreen && st.View == ViewOutline && st.EditingID == 0 && st.LinkNodeID == 0 && !st.OutlineHandle.FocusWithin(c) {
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyPeriod) {
			st.outlineToggleFoldAll()
			c.Invalidate()
		} else if !cb.ReadOnly {
			if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ) || c.Shortcut(ui.Cmd, ui.KeyY) {
				st.opRedo(cb)
			} else if c.Shortcut(ui.Cmd, ui.KeyZ) {
				st.opUndo(cb)
			}
		}
	}

	var overlay ui.Element
	animatingOverlay := (st.fsAnimActive || !st.Fullscreen) && st.hasValidFullscreenTransitionCoords()
	if animatingOverlay {
		p := clampFloat32(st.fsVisualProgress, 0, 1)
		inlineRel := st.inlineRelRect()
		if !st.Fullscreen {
			targetH := st.inlinePlaceholderInnerHeight(cfg) + 2
			if targetH > 0 {
				inlineRel.H = targetH
			}
		}
		inlineRel = clipRect(inlineRel, ui.Rect{W: st.editorBounds.W, H: st.editorBounds.H})
		fullRel := ui.Rect{X: 0, Y: 0, W: st.editorBounds.W, H: st.editorBounds.H}
		cur := lerpRect(inlineRel, fullRel, p)
		radius := theme.RadiusLG * (1 - p)
		borderCol := pal.Hairline
		if st.fsBorderFocused {
			borderCol = pal.Accent
		}
		borderAlpha := 1 - p
		overlay = ui.Column(c.Key("mm-fullscreen-"+id)).Absolute().
			Left(cur.X).Top(cur.Y).Width(cur.W).Height(cur.H).
			Background(pal.Paper).Border(1, borderCol.Alpha(borderAlpha)).Clip()
		if radius > 0.05 {
			overlay.Radius(radius)
		}
		if !st.Fullscreen {
			overlay.IgnoreInput(true)
		}
	} else {
		overlay = ui.Column(c.Key("mm-fullscreen-"+id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Background(pal.Paper).Clip()
		if !st.Fullscreen {
			overlay.IgnoreInput(true)
		}
	}
	bui.Probe(overlay, "mm-fullscreen-"+id)
	overlay.Children(func() {
		st.renderHeader(c, id+"-fs", true, cb)
		if !st.IsFullscreenOverlayActive() && animatingOverlay == false {
			c.Invalidate()
			return
		}
		st.renderBody(c, id+"-fs", 0, true, cb)
		if cfg.ShowHints && st.View != ViewSource {
			st.renderHintBar(c, id+"-fs")
		}
	})
}

// --- Header Bar ---

func (st *BlockState) renderHeader(c *ui.Context, id string, fullscreen bool, cb Callbacks) {
	pal := theme.Ocean()
	hdr := ui.Row(c.Key("mm-hdr-" + id)).FillWidth().Height(theme.HeightLarge).Shrink(0).
		PaddingX(theme.Space6).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space6).
		Background(pal.Paper)
	hdr.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Line(r.X, r.Y+r.H-0.5, r.X+r.W, r.Y+r.H-0.5, 1, pal.Hairline)
	})
	hdr.Children(func() {
		// Left: Neutral-ring segmented view switcher (导图 / 大纲 / 源码) + Task progress pill.
		leftGroup := ui.Row(c.Key("mm-hdr-left-" + id)).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0)
		leftGroup.Children(func() {
			seg := ui.Row(c.Key("mm-seg-" + id)).AlignItems(ui.Center).Gap(theme.Space2).Shrink(0)
			// Holding ⌘ while the block owns focus reveals each tab's ⌘1/2/3 keycap,
			// the same way browsers surface tab numbers.
			scoped := fullscreen || st.HasFocus(c) || st.EditingID != 0
			showDigits := scoped && c.Modifiers() == ui.Cmd
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
						border = pal.SelectedBorder
						fg = pal.Text
					} else if hov || prs {
						bg = pal.Hover
						fg = pal.Text
					}
					bui.AnimateSurfaceColors(btn, bg, border, active)
					sc := viewShortcutLabel(tab.mode)
					if !showDigits {
						bui.TooltipShortcut(c, btn, tab.label, sc)
					}
					btn.Children(func() {
						icons.DrawAnimated(c, tab.icon, theme.IconInline, pal.DuoIconColors(fg), active || hov || prs)
						ui.Text(c, tab.label).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
						if showDigits {
							bui.Keycap(c, sc)
						}
					})
					if btn.Clicked() {
						st.SwitchView(tab.mode, false, cb)
					}
				}
			})

			// Task progress pill (click to jump to next incomplete task).
			if st.Root != nil {
				if totalTasks, doneTasks := st.Root.CountTaskStats(); totalTasks > 0 {
					allDone := doneTasks == totalTasks
					pillTip := theme.Tr("定位下一个未完成任务", "Jump to next incomplete task")
					if allDone {
						pillTip = theme.Tr("全部任务已完成", "All tasks completed")
					}
					pill := ui.ButtonBase(c.Key("mm-task-prog-"+id)).
						Height(theme.HeightCompact).PaddingX(theme.Space8).Radius(theme.RadiusPill).
						AlignItems(ui.Center).Gap(theme.Space4).Cursor(ui.CursorPointer).Shrink(0).
						Label(pillTip)
					pillHov := pill.Hovered() || pill.Pressed()
					bg := pal.Hover
					fg := pal.TextSecondary
					border := pal.Hairline
					if allDone {
						bg = pal.SuccessSurface
						fg = pal.Success
						border = pal.Success.Alpha(0.28)
					} else if pillHov {
						bg = pal.SelectionBG
						fg = pal.Accent
						border = pal.Accent.Alpha(0.35)
					}
					pill.Background(bg).Border(1, border)
					bui.Tooltip(c, pill, pillTip)
					pill.Children(func() {
						icons.DrawAnimated(c, icons.Check, theme.IconMicro, pal.MonoIconColors(fg), pillHov || allDone)
						ui.Text(c, fmt.Sprintf("%d/%d", doneTasks, totalTasks)).
							FontSize(theme.FontMeta).FontWeight(theme.WeightMono).TextColor(fg).SingleLine()
					})
					if pill.Clicked() {
						if next := NextUndoneTask(st.Root, st.SelectedID); next != nil {
							ExpandAncestors(next)
							st.SelectedID = next.ID
							st.EnsureVisiblePending = true
							if st.View == ViewOutline {
								st.focusOutlineNode(next.ID, 0)
							} else if st.View == ViewSource {
								st.View = ViewMap
								st.EnsureVisiblePending = true
							}
							c.Invalidate()
						}
					}
				}
			}
		})

		// Right: Context-aware actions tailored to the active view.
		actions := ui.Row(c.Key("mm-act-" + id)).AlignItems(ui.Center).Gap(theme.Space2).Shrink(0)
		actions.Children(func() {
			k := func(mac, other string) string {
				if theme.IsMacOS() {
					return mac
				}
				return other
			}

			// Undo / Redo chrome buttons in Map and Outline views.
			if st.View != ViewSource && !cb.ReadOnly {
				st.renderHeaderHistoryBtn(c, "mm-undo-"+id, false, st.canUndo(cb), theme.Tr("撤销", "Undo"), k("⌘Z", "Ctrl+Z"), func() {
					st.opUndo(cb)
					c.Invalidate()
				})
				st.renderHeaderHistoryBtn(c, "mm-redo-"+id, true, st.canRedo(cb), theme.Tr("重做", "Redo"), k("⌘⇧Z", "Ctrl+Y"), func() {
					st.opRedo(cb)
					c.Invalidate()
				})
				ui.Box(c).Size(1, theme.Space12).Margin(0, theme.Space2).Background(pal.Hairline).Shrink(0)
			}

			switch st.View {
			case ViewMap:
				findTip := theme.Tr("查找节点", "Find in map")
				findBtn := bui.IconButton(c, icons.FindSearch, bui.IconButtonOptions{
					ID:       "mm-find-" + id,
					Label:    findTip,
					Tooltip:  findTip,
					Shortcut: k("⌘F", "Ctrl+F"),
					Size:     theme.HeightChrome,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusSM,
					Active:   st.FindOpen,
				})
				if findBtn.Clicked() {
					if st.FindOpen {
						st.CloseFind()
					} else {
						st.OpenFind()
					}
				}

				layIcon := icons.AlignLeft
				layTip := theme.Tr("切换为左右平衡布局", "Switch to balanced layout")
				if st.Layout == LayoutBoth {
					layIcon = icons.Columns
					layTip = theme.Tr("切换为向右布局", "Switch to right-side layout")
				}
				layBtn := bui.IconButton(c, layIcon, bui.IconButtonOptions{
					ID:       "mm-layout-" + id,
					Label:    layTip,
					Tooltip:  layTip,
					Size:     theme.HeightChrome,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusSM,
				})
				if layBtn.Clicked() {
					st.ToggleLayout()
				}

				fitTip := theme.Tr("适配画布", "Fit to view")
				fitBtn := bui.IconButton(c, icons.TbReset, bui.IconButtonOptions{
					ID:       "mm-fit-" + id,
					Label:    fitTip,
					Tooltip:  fitTip,
					Shortcut: k("⌘0", "Ctrl+0"),
					Size:     theme.HeightChrome,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusSM,
				})
				if fitBtn.Clicked() {
					st.AutoFitPending = true
				}

			case ViewOutline:
				hasExpanded := st.outlineHasAnyExpanded()
				foldIcon := icons.UnfoldAll
				foldTip := theme.Tr("全部展开", "Expand all")
				if hasExpanded {
					foldIcon = icons.FoldAll
					foldTip = theme.Tr("全部折叠", "Collapse all")
				}
				foldBtn := bui.IconButton(c, foldIcon, bui.IconButtonOptions{
					ID:       "mm-fold-all-" + id,
					Label:    foldTip,
					Tooltip:  foldTip,
					Shortcut: k("⌘⇧.", "Ctrl+Shift+."),
					Size:     theme.HeightChrome,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusSM,
				})
				if foldBtn.Clicked() {
					st.outlineToggleFoldAll()
					c.Invalidate()
				}

			case ViewSource:
				now := c.Now()
				copied := !st.SourceCopiedUntil.IsZero() && now.Before(st.SourceCopiedUntil)
				if copied {
					if rem := st.SourceCopiedUntil.Sub(now); rem > 0 {
						c.After(rem + 5*time.Millisecond)
					}
				}
				copyIcon := icons.Copy
				copyTip := theme.Tr("复制源码", "Copy source")
				if copied {
					copyIcon = icons.Check
					copyTip = theme.Tr("已复制", "Copied")
				}
				copyBtn := bui.IconButton(c, copyIcon, bui.IconButtonOptions{
					ID:          "mm-src-copy-" + id,
					Label:       copyTip,
					Tooltip:     copyTip,
					Size:        theme.HeightChrome,
					IconSize:    theme.IconAction,
					Radius:      theme.RadiusSM,
					Active:      copied,
					ActiveStyle: bui.ActiveAccentFlat,
				})
				if copyBtn.Clicked() {
					src := st.LastSource
					if src == "" && st.Root != nil {
						src = SerializeMarkdown(st.Root)
					}
					c.Services().WriteClipboard(src)
					st.SourceCopiedUntil = c.Now().Add(1500 * time.Millisecond)
					c.After(1505 * time.Millisecond)
				}
			}

			// Import & Export apply to structured views (Map & Outline); Source stays clean (Copy + Fullscreen).
			if st.View != ViewSource {
				if !cb.ReadOnly {
					impSpec := bui.TooltipSpec{
						Rows: []bui.TooltipRow{{
							Trigger: theme.Tr("导入", "Import"),
							Detail:  ".md / .opml / .mm / .xmind",
						}},
					}
					impBtn := bui.IconButton(c, icons.Upload, bui.IconButtonOptions{
						ID:          "mm-import-" + id,
						Label:       theme.Tr("导入 (.md / .opml / .mm / .xmind)", "Import (.md / .opml / .mm / .xmind)"),
						TooltipSpec: &impSpec,
						Size:        theme.HeightChrome,
						IconSize:    theme.IconAction,
						Radius:      theme.RadiusSM,
					})
					if impBtn.Clicked() {
						st.triggerImport(cb)
					}
				}

				expTip := theme.Tr("导出", "Export")
				expOpts := bui.IconButtonOptions{
					ID:       "mm-export-" + id,
					Label:    expTip,
					Size:     theme.HeightChrome,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusSM,
					Active:   st.ExportMenuOpen,
				}
				if !st.ExportMenuOpen {
					expOpts.Tooltip = expTip
				}
				expBtn := bui.IconButton(c, icons.Download, expOpts)
				if expBtn.Clicked() {
					st.ExportMenuOpen = !st.ExportMenuOpen
					if st.ExportMenuOpen {
						st.ShortcutsOpen = false
						st.LinkNodeID = 0
					}
				}
			}

			fsIcon := icons.Maximize
			fsTip := theme.Tr("全屏", "Fullscreen")
			fsShortcut := ""
			if fullscreen {
				fsIcon = icons.Close
				fsTip = theme.Tr("退出全屏", "Exit fullscreen")
				fsShortcut = "Esc"
			}
			fsBtn := bui.IconButton(c, fsIcon, bui.IconButtonOptions{
				ID:       "mm-fs-" + id,
				Label:    fsTip,
				Tooltip:  fsTip,
				Shortcut: fsShortcut,
				Size:     theme.HeightChrome,
				IconSize: theme.IconAction,
				Radius:   theme.RadiusSM,
			})
			if fsBtn.Clicked() {
				if fullscreen {
					st.ExitFullscreen(cb)
				} else {
					st.EnterFullscreen(cb)
				}
				c.Invalidate()
			}
		})
	})
}

func (st *BlockState) renderHeaderHistoryBtn(c *ui.Context, key string, isRedo, enabled bool, label, shortcut string, onClick func()) {
	pal := theme.Ocean()
	btn := ui.ButtonBase(c.Key(key)).
		Size(theme.HeightChrome, theme.HeightChrome).Center().
		Radius(theme.RadiusSM).Shrink(0).Disabled(!enabled).Label(label)
	if enabled {
		btn.Cursor(ui.CursorPointer)
	}
	hov := enabled && btn.Hovered()
	prs := enabled && btn.Pressed()
	bg := ui.Transparent
	if prs {
		bg = pal.Selected
	} else if hov {
		bg = pal.Hover
	}
	btn.Background(bg)
	if enabled {
		bui.TooltipShortcut(c, btn, label, shortcut)
	}
	fg := pal.TextSecondary.Alpha(0.32)
	if enabled {
		fg = pal.TextSecondary
		if hov || prs {
			fg = pal.Text
		}
	}
	btn.Draw(func(p *ui.Painter, r ui.Rect) {
		cx := r.X + r.W*0.5
		cy := r.Y + r.H*0.5
		dir := float32(-1)
		if isRedo {
			dir = 1
		}
		var path ui.Path
		path.Cap(ui.RoundCap).Join(ui.RoundJoin)
		// Curved undo/redo shaft
		path.MoveTo(cx+dir*4.2, cy-2.0)
		path.LineTo(cx-dir*1.4, cy-2.0)
		path.CubeTo(cx-dir*5.0, cy-2.0, cx-dir*5.0, cy+3.5, cx-dir*1.0, cy+3.5)
		path.LineTo(cx+dir*2.4, cy+3.5)
		// Arrowhead at top tip
		path.MoveTo(cx+dir*2.0, cy-4.4)
		path.LineTo(cx+dir*4.6, cy-2.0)
		path.LineTo(cx+dir*2.0, cy+0.4)
		p.StrokePath(&path, 1.4, fg)
	})
	if btn.Clicked() && enabled && onClick != nil {
		onClick()
	}
}

// --- Body Area (Map / Outline / Source + Floating Overlays) ---

func (st *BlockState) renderBody(c *ui.Context, id string, fixedHeight float32, fullscreen bool, cb Callbacks) {
	pal := theme.Ocean()
	bodyBG := pal.CodeBG
	if st.View == ViewOutline {
		bodyBG = pal.Paper
	}
	body := ui.Box(c.Key("mm-body-" + id)).FillWidth().Background(bodyBG).Clip()
	if fullscreen {
		body.Grow(1).MinHeight(0)
	} else {
		body.Height(fixedHeight).Shrink(0)
	}
	body.Children(func() {
		switch st.View {
		case ViewOutline:
			st.renderOutlineView(c, id, fullscreen, cb)
		case ViewSource:
			st.renderSourceView(c, id, fullscreen, cb)
		default:
			st.renderCanvasView(c, id, fullscreen, cb)
		}

		if st.FindOpen && st.View == ViewMap {
			st.renderFindBar(c, id, cb)
		}
		if st.ExportMenuOpen {
			st.renderExportMenu(c, id, fixedHeight, cb)
		}
		if st.LinkNodeID != 0 {
			st.renderLinkPopover(c, id, cb)
		}
		if st.ShortcutsOpen {
			st.renderShortcutsPopover(c, id)
		}
		if st.ImportResult != nil {
			st.renderImportResultCard(c, id, cb)
		}
	})
}

// --- Native GPU MindMap Canvas ---

func (st *BlockState) renderCanvasView(c *ui.Context, id string, fullscreen bool, cb Callbacks) {
	pal := theme.Ocean()
	canvas := ui.Box(c.Key("mm-canvas-"+id)).Fill().Focusable().FocusRing(false).Bind(&st.CanvasHandle)
	bui.Probe(canvas, "mm-canvas-"+id)
	if !fullscreen && st.consumeFullscreenFocusRestore() {
		canvas.Focus(ui.FocusOptions{PreventScroll: true})
		if cb.OnRequestFocus != nil {
			cb.OnRequestFocus()
		}
	}
	if st.viewFocusPending {
		st.viewFocusPending = false
		canvas.Focus(ui.FocusOptions{PreventScroll: true})
	}

	// Measure viewport size and ensure camera fits initially or after layout/resize.
	bounds := canvas.LayoutBounds()
	cw, ch := bounds.W, bounds.H
	hasMeasuredBounds := cw > 0 && ch > 0
	if !hasMeasuredBounds {
		cw, ch = st.expectedCanvasSize(fullscreen)
	}
	if !st.fsAnimActive && hasMeasuredBounds {
		if st.fsInlineJustLanded {
			if st.LastCanvasW <= 0 || st.LastCanvasH <= 0 ||
				math.Abs(float64(cw-st.LastCanvasW)) > 4 || math.Abs(float64(ch-st.LastCanvasH)) > 4 {
				st.AutoFitPending = true
			}
			st.fsInlineJustLanded = false
		} else if st.LastCanvasW > 0 &&
			(math.Abs(float64(cw-st.LastCanvasW)) > 24 || math.Abs(float64(ch-st.LastCanvasH)) > 24) {
			st.AutoFitPending = true
		}
	}
	if hasMeasuredBounds {
		st.LastCanvasW, st.LastCanvasH = cw, ch
	} else if st.LastCanvasW <= 0 || st.LastCanvasH <= 0 {
		st.LastCanvasW, st.LastCanvasH = cw, ch
	}
	st.LastCanvasFocused = st.CanvasHandle.Focused(c) && st.EditingID == 0 && st.LinkNodeID == 0

	// Live-layout during inline editing or direct CJK IME composition using the exact
	// native text measurer so node geometry, Bezier curves, and the transparent inline
	// editor stay in 1:1 sync with zero frame lag.
	activeEditID := 0
	activeEditText := ""
	if !cb.ReadOnly && st.Root != nil {
		if st.EditingID != 0 {
			activeEditID = st.EditingID
			activeEditText = st.EditDraft + st.EditCompose
		} else if st.ComposeText != "" {
			if sel := st.selectedNode(); sel != nil {
				activeEditID = sel.ID
				activeEditText = st.ComposeText
			}
		}
	}
	if st.Root != nil && activeEditID != 0 {
		st.Measure.SetTextOverride(activeEditID, activeEditText)
		wb := ComputeLayout(st.Root, st.Layout, c, &st.Measure)
		if !st.CameraInit || st.AutoFitPending {
			st.Camera = FitCamera(wb, cw, ch, 28, 22)
			st.CameraInit = true
			st.AutoFitPending = false
			st.EnsureVisiblePending = false
		} else if en := st.Root.FindByID(activeEditID); en != nil {
			scale := st.Camera.Scale
			if scale <= 0 {
				scale = 1
			}
			textChanged := activeEditText != st.editLastLayoutText
			st.editLastLayoutText = activeEditText
			if st.EnsureVisiblePending {
				st.EnsureVisiblePending = false
				st.ensureNodeVisible(en, cw, ch)
			} else if textChanged && en.W <= (cw-72)/scale {
				st.ensureNodeVisible(en, cw, ch)
			}
		}
	} else {
		st.Measure.ClearTextOverride()
	}

	// Enable direct typing & IME composition on the selected node when canvas owns focus.
	if !cb.ReadOnly && st.EditingID == 0 && st.LinkNodeID == 0 && st.Root != nil {
		caretR := ui.Rect{X: 8, Y: 8, W: 2, H: 16}
		if sel := st.selectedNode(); sel != nil && sel.W > 0 && sel.H > 0 {
			sx, sy := st.Camera.WorldToScreen(sel.X, sel.Y-sel.H/2)
			scale := st.Camera.Scale
			if scale <= 0 {
				scale = 1
			}
			caretR = ui.Rect{
				X: clampFloat32(sx, 0, max(cw-2, 0)),
				Y: clampFloat32(sy, 0, max(ch-16, 0)),
				W: 2,
				H: max(sel.H*scale, 16),
			}
		}
		canvas.TextCaret(caretR)
	}

	// Context menu (right-click on node or canvas).
	canvas.ContextMenu(func(m *ui.Menu) {
		n := st.selectedNode()
		if n == nil {
			return
		}
		leftSide := n.Dir < 0
		if !cb.ReadOnly {
			if m.Item(theme.Tr("添加子节点", "Add Child")).Shortcut(0, ui.KeyTab).Chosen() {
				st.opAddChild(cb)
			}
			if n.Parent != nil {
				if m.Item(theme.Tr("在下方添加同级", "Add Sibling Below")).Shortcut(0, ui.KeyEnter).Chosen() {
					st.opAddSibling(cb)
				}
				if m.Item(theme.Tr("在上方添加同级", "Add Sibling Above")).Shortcut(ui.Shift, ui.KeyEnter).Chosen() {
					st.opAddSiblingBefore(cb)
				}
				if m.Item(theme.Tr("插入父节点", "Insert Parent")).Shortcut(ui.Shift, ui.KeyTab).Chosen() {
					st.opInsertParent(cb)
				}
			}
			m.Separator()
			if m.Item(theme.Tr("改名", "Rename")).Shortcut(0, ui.KeyF2).Chosen() {
				st.beginEdit(n, false)
			}
			if n.Parent != nil {
				if m.Item(theme.Tr("上移节点", "Move Up")).Shortcut(ui.Alt, ui.KeyUp).Chosen() {
					st.opMoveSibling(-1, cb)
				}
				if m.Item(theme.Tr("下移节点", "Move Down")).Shortcut(ui.Alt, ui.KeyDown).Chosen() {
					st.opMoveSibling(1, cb)
				}
				if n.Parent.Parent != nil {
					promoteKey := ui.KeyLeft
					if leftSide {
						promoteKey = ui.KeyRight
					}
					if m.Item(theme.Tr("升级节点", "Promote")).Shortcut(ui.Alt, promoteKey).Chosen() {
						st.opPromote(cb)
					}
				}
				if n.IndexInParent() > 0 {
					demoteKey := ui.KeyRight
					if leftSide {
						demoteKey = ui.KeyLeft
					}
					if m.Item(theme.Tr("降级节点", "Demote")).Shortcut(ui.Alt, demoteKey).Chosen() {
						st.opDemote(cb)
					}
				}
			}
			m.Separator()
		}
		if m.Item(theme.Tr("复制分支", "Copy")).Shortcut(ui.Cmd, ui.KeyC).Chosen() {
			st.opCopy(c)
		}
		if !cb.ReadOnly {
			if n.Parent != nil {
				if m.Item(theme.Tr("剪切分支", "Cut")).Shortcut(ui.Cmd, ui.KeyX).Chosen() {
					st.opCut(c, cb)
				}
			}
			if m.Item(theme.Tr("粘贴为子节点", "Paste as Child")).Shortcut(ui.Cmd, ui.KeyV).Chosen() {
				st.opPaste(c, cb)
			}
			if n.Parent != nil {
				if m.Item(theme.Tr("创建副本", "Duplicate")).Shortcut(ui.Cmd, ui.KeyD).Chosen() {
					st.opDuplicate(cb)
				}
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
				if m.Item(theme.Tr("打开链接", "Open Link")).Chosen() {
					st.openNodeLink(c.Now(), n, cb)
				}
				if m.Item(theme.Tr("移除链接", "Remove Link")).Chosen() {
					st.pushUndo()
					n.Link = ""
					st.invalidateLinkStatusCache()
					st.commitChange(cb)
				}
			}
			if n.Parent != nil {
				taskLbl := theme.Tr("设为任务", "Convert to Task")
				if n.Task != nil {
					taskLbl = theme.Tr("取消任务", "Remove Task")
				}
				if m.Item(taskLbl).Shortcut(ui.Cmd|ui.Alt, ui.KeyT).Chosen() {
					st.opToggleTask(n, cb)
				}
				if n.Task != nil {
					doneLbl := theme.Tr("标记已完成", "Mark Done")
					if *n.Task {
						doneLbl = theme.Tr("标记未完成", "Mark Undone")
					}
					if m.Item(doneLbl).Shortcut(ui.Cmd, ui.KeyEnter).Chosen() {
						st.opFlipTaskDone(n, cb)
					}
				}
			}
		}
		m.Separator()
		if len(n.Children) > 0 {
			foldLbl := theme.Tr("折叠", "Fold")
			if n.Folded {
				foldLbl = theme.Tr("展开", "Unfold")
			}
			if m.Item(foldLbl).Shortcut(0, ui.KeySpace).Chosen() {
				st.opToggleFold()
			}
		}
		if st.hasAnyFolded() {
			if m.Item(theme.Tr("全部展开", "Expand All")).Shortcut(ui.Cmd|ui.Alt, ui.KeySlash).Chosen() {
				st.opExpandAll()
			}
		} else {
			if m.Item(theme.Tr("折叠所有子分支", "Collapse All")).Shortcut(ui.Cmd|ui.Alt, ui.KeySlash).Chosen() {
				st.opCollapseAll()
			}
		}
		if !cb.ReadOnly && n.Parent != nil {
			m.Separator()
			if m.Item(theme.Tr("删除", "Delete")).Shortcut(0, ui.KeyBackspace).Chosen() {
				st.opDelete(cb)
			}
			if len(n.Children) > 0 {
				if m.Item(theme.Tr("仅删除当前节点", "Delete Single Node")).Shortcut(ui.Alt, ui.KeyBackspace).Chosen() {
					st.opDeleteSingle(cb)
				}
			}
		}
	})

	// Pointer, Scroll, Zoom, and Keyboard input handler.
	canvas.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerMove:
			if st.Panning && st.Camera.Scale > 0 {
				st.fsCamAnimActive = false
				dx := ev.X - st.PanLastX
				dy := ev.Y - st.PanLastY
				st.Camera.X -= dx / st.Camera.Scale
				st.Camera.Y -= dy / st.Camera.Scale
				st.PanLastX, st.PanLastY = ev.X, ev.Y
				return true
			}
			if st.DragNodeID != 0 {
				st.DragCurrX, st.DragCurrY = ev.X, ev.Y
				if !st.DragActive {
					dx := float64(ev.X - st.DragStartX)
					dy := float64(ev.Y - st.DragStartY)
					if math.Hypot(dx, dy) >= 6 {
						st.DragActive = true
					}
				}
				if st.DragActive {
					if dragged := st.Root.FindByID(st.DragNodeID); dragged != nil {
						wx, wy := st.Camera.ScreenToWorld(ev.X, ev.Y)
						target, pos := HitTestDropTarget(st.Root, dragged, wx, wy)
						if target != nil {
							st.DragTargetID = target.ID
							st.DragPos = pos
						} else {
							st.DragTargetID = 0
							st.DragPos = DropNone
						}
					}
				}
				return true
			}
			st.refreshHoverAt(ev.X, ev.Y, cb.ReadOnly)
			return false

		case ui.InputPointerDown:
			// Decide action-button hits with the visibility state the user saw before this click clears it.
			actionsLive := st.canvasActionsLive()
			st.ExportMenuOpen = false
			st.ShortcutsOpen = false
			st.ImportResult = nil
			st.ImportUndoBefore = ""
			st.ImportUndoAfter = ""
			st.LinkNodeID = 0
			st.ComposeText = ""
			st.lastKeyMod = 0
			st.resetDrag()
			if st.EditingID != 0 {
				st.finishEdit(true, cb)
			}
			wx, wy := st.Camera.ScreenToWorld(ev.X, ev.Y)
			activeID := st.HoveredID
			if !actionsLive {
				activeID = 0
			}
			hit := HitTestWithActions(st.Root, [2]int{activeID, 0}, cb.ReadOnly, wx, wy, st.Camera.Scale)

			if ev.Button == 1 {
				// Right-click selects hit node before ContextMenu opens.
				if hit.Node != nil {
					st.SelectedID = hit.Node.ID
				}
				return false
			}
			if ev.Button == 2 {
				// Middle-mouse drag pans the canvas anywhere, even over a node.
				st.CanvasHandle.Focus()
				if cb.OnRequestFocus != nil {
					cb.OnRequestFocus()
				}
				st.Panning = true
				st.PanLastX, st.PanLastY = ev.X, ev.Y
				return true
			}
			if ev.Button != 0 {
				return false
			}

			st.CanvasHandle.Focus()
			if cb.OnRequestFocus != nil {
				cb.OnRequestFocus()
			}

			switch hit.Kind {
			case HitLinkBadge:
				st.SelectedID = hit.Node.ID
				st.openNodeLink(c.Now(), hit.Node, cb)
				return true
			case HitHoverFold:
				if !cb.ReadOnly {
					st.SelectedID = hit.Node.ID
					hit.Node.Folded = true
					st.HoveredHit = HitNone
				}
				return true
			case HitHoverAdd:
				if !cb.ReadOnly {
					st.SelectedID = hit.Node.ID
					st.HoveredHit = HitNone
					st.opAddChild(cb)
				}
				return true
			case HitCheckbox:
				st.opFlipTaskDone(hit.Node, cb)
				return true
			case HitFoldBadge:
				hit.Node.Folded = false
				st.SelectedID = hit.Node.ID
				st.HoveredHit = HitNone
				return true
			case HitNode:
				st.SelectedID = hit.Node.ID
				if (ev.Mods&(ui.Cmd|ui.Ctrl)) != 0 && strings.TrimSpace(hit.Node.Link) != "" {
					st.openNodeLink(c.Now(), hit.Node, cb)
					return true
				}
				if ev.Clicks >= 2 && !cb.ReadOnly {
					st.beginEdit(hit.Node, false)
					return true
				}
				if !cb.ReadOnly && hit.Node.Parent != nil && ev.Clicks == 1 {
					st.DragNodeID = hit.Node.ID
					st.DragActive = false
					st.DragStartX, st.DragStartY = ev.X, ev.Y
					st.DragCurrX, st.DragCurrY = ev.X, ev.Y
					st.DragTargetID = 0
					st.DragPos = DropNone
				}
				return true
			default:
				if ev.Clicks >= 2 {
					st.FitCanvas()
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
			if st.DragNodeID != 0 {
				if !ev.Canceled && st.DragActive && st.DragTargetID != 0 && st.DragPos != DropNone && st.Root != nil {
					dragged := st.Root.FindByID(st.DragNodeID)
					target := st.Root.FindByID(st.DragTargetID)
					st.opReparent(dragged, target, st.DragPos, cb)
				}
				st.resetDrag()
				return true
			}
			return false

		case ui.InputZoom:
			if ev.Scale > 0 && st.Camera.Scale > 0 {
				st.zoomAt(ev.X, ev.Y, st.Camera.Scale*ev.Scale)
				st.refreshHoverAt(ev.X, ev.Y, cb.ReadOnly)
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
				st.refreshHoverAt(ev.X, ev.Y, cb.ReadOnly)
				return true
			}
			// In fullscreen or when actively holding Shift/horizontal scroll, pan the canvas.
			if st.Fullscreen || math.Abs(float64(ev.DX)) > 0.5 || st.CanvasHandle.Focused(c) {
				st.fsCamAnimActive = false
				st.Camera.X += ev.DX / st.Camera.Scale
				st.Camera.Y += ev.DY / st.Camera.Scale
				st.refreshHoverAt(ev.X, ev.Y, cb.ReadOnly)
				return true
			}
			return false

		case ui.InputCommand:
			if st.EditingID != 0 || st.LinkNodeID != 0 {
				return false
			}
			switch ev.Text {
			case "copy":
				return st.opCopy(c)
			case "cut":
				return st.opCut(c, cb)
			case "paste", "pasteAndMatchStyle":
				return st.opPaste(c, cb)
			case "undo":
				st.opUndo(cb)
				return true
			case "redo":
				st.opRedo(cb)
				return true
			case "delete":
				st.opDelete(cb)
				return true
			}
			return false

		case ui.InputCompose:
			if st.EditingID != 0 || st.LinkNodeID != 0 || cb.ReadOnly || st.DragActive {
				st.ComposeText = ""
				return false
			}
			if ev.Text != "" && (st.lastKeyMod&(ui.Cmd|ui.Ctrl|ui.Alt)) != 0 {
				return false
			}
			st.ComposeText = ev.Text
			return true

		case ui.InputText:
			st.ComposeText = ""
			if st.EditingID != 0 || st.LinkNodeID != 0 || cb.ReadOnly || st.DragActive {
				return false
			}
			if (st.lastKeyMod & (ui.Cmd | ui.Ctrl | ui.Alt)) != 0 {
				return false
			}
			if isDirectEditText(ev.Text) {
				if n := st.selectedNode(); n != nil {
					st.beginEditWithText(n, ev.Text)
					return true
				}
			}
			return false

		case ui.InputKeyDown:
			st.lastKeyMod = ev.Mods
			if st.EditingID != 0 || st.LinkNodeID != 0 {
				return false
			}
			if ev.Key == ui.KeyEscape {
				if st.DragActive || st.DragNodeID != 0 {
					st.resetDrag()
					return true
				}
				if st.FindOpen {
					st.CloseFind()
					return true
				}
				if st.ShortcutsOpen {
					st.ShortcutsOpen = false
					return true
				}
				if st.ExportMenuOpen {
					st.ExportMenuOpen = false
					return true
				}
				if st.ImportResult != nil {
					st.ImportResult = nil
					return true
				}
				if st.Fullscreen {
					st.ExitFullscreen(cb)
					c.Invalidate()
					return true
				}
			}
			return st.handleCanvasKey(c, ev.Key, ev.Mods, cb)
		}
		return false
	})

	canvasHovered := canvas.Hovered()
	if !canvasHovered && !st.Panning && st.DragNodeID == 0 && st.HoveredID != 0 {
		st.HoveredID = 0
		st.HoveredHit = HitNone
	}

	// Dynamic canvas cursor: pointer over interactive badges/buttons, grab on draggable nodes, grabbing when panning/dragging.
	switch {
	case st.Panning || st.DragActive:
		canvas.Cursor(ui.CursorGrabbing)
	case canvasHovered && st.HoveredID != 0:
		switch st.HoveredHit {
		case HitLinkBadge, HitFoldBadge:
			canvas.Cursor(ui.CursorPointer)
		case HitCheckbox, HitHoverFold, HitHoverAdd:
			if !cb.ReadOnly {
				canvas.Cursor(ui.CursorPointer)
			} else {
				canvas.Cursor(ui.CursorDefault)
			}
		case HitNode:
			if !cb.ReadOnly && st.Root != nil {
				if hn := st.Root.FindByID(st.HoveredID); hn != nil && hn.Parent != nil {
					canvas.Cursor(ui.CursorGrab)
				} else {
					canvas.Cursor(ui.CursorDefault)
				}
			} else {
				canvas.Cursor(ui.CursorDefault)
			}
		default:
			canvas.Cursor(ui.CursorDefault)
		}
	default:
		canvas.Cursor(ui.CursorDefault)
	}

	now := c.Now()
	// Paint native vector mindmap.
	canvas.Draw(func(p *ui.Painter, r ui.Rect) {
		if st.Root == nil || r.W <= 0 || r.H <= 0 {
			return
		}
		paintEditID := 0
		if !cb.ReadOnly {
			if st.EditingID != 0 {
				paintEditID = st.EditingID
				st.Measure.SetTextOverride(paintEditID, st.EditDraft+st.EditCompose)
			} else if st.ComposeText != "" {
				if sel := st.selectedNode(); sel != nil {
					paintEditID = sel.ID
					st.Measure.SetTextOverride(paintEditID, st.ComposeText)
				}
			}
		}
		if paintEditID == 0 {
			st.Measure.ClearTextOverride()
		}
		worldBounds := ComputeLayout(st.Root, st.Layout, p, &st.Measure)
		if !fullscreen && !st.fsAnimActive && st.fsInlineJustLanded {
			if st.LastCanvasW <= 0 || st.LastCanvasH <= 0 ||
				math.Abs(float64(r.W-st.LastCanvasW)) > 4 || math.Abs(float64(r.H-st.LastCanvasH)) > 4 {
				st.AutoFitPending = true
			}
			st.fsInlineJustLanded = false
		}
		st.LastCanvasW, st.LastCanvasH = r.W, r.H
		if !fullscreen && !st.fsAnimActive {
			st.inlineCanvasW, st.inlineCanvasH = r.W, r.H
		}
		if st.fsCamAnimActive {
			if st.fsAnimEntering {
				fullW, fullH := st.expectedCanvasSize(true)
				if !st.fsCamToValid && fullW > 0 && fullH > 0 {
					st.fsCamTo = FitCamera(worldBounds, fullW, fullH, 28, 22)
					st.fsCamToW, st.fsCamToH = fullW, fullH
					st.fsCamToValid = true
				}
				if !st.fsCamFromValid {
					inlineW, inlineH := st.expectedCanvasSize(false)
					if inlineW > 0 && inlineH > 0 {
						st.fsCamFrom = FitCamera(worldBounds, inlineW, inlineH, 28, 22)
						st.fsCamFromW, st.fsCamFromH = inlineW, inlineH
						st.fsCamFromValid = true
					} else if st.fsCamToValid {
						st.fsCamFrom = st.fsCamTo
						st.fsCamFromW, st.fsCamFromH = st.fsCamToW, st.fsCamToH
						st.fsCamFromValid = true
					}
				}
			} else {
				inlineW, inlineH := st.expectedCanvasSize(false)
				if !st.fsCamToValid && inlineW > 0 && inlineH > 0 {
					st.fsCamTo = FitCamera(worldBounds, inlineW, inlineH, 28, 22)
					st.fsCamToW, st.fsCamToH = inlineW, inlineH
					st.fsCamToValid = true
				}
			}
			if st.fsCamFromValid && st.fsCamToValid {
				st.Camera = lerpCameraViewport(st.fsCamFrom, st.fsCamTo, st.fsCamFromW, st.fsCamFromH, st.fsCamToW, st.fsCamToH, r.W, r.H, st.fsCamProgress)
				st.CameraInit = true
				st.AutoFitPending = false
				st.EnsureVisiblePending = false
			}
			if !st.fsAnimActive {
				st.fsCamAnimActive = false
				if !st.fsAnimEntering && st.fsCamToValid {
					st.Camera = st.fsCamTo
					st.CameraInit = true
					st.AutoFitPending = false
				}
			}
		} else if !st.CameraInit || st.AutoFitPending {
			st.Camera = FitCamera(worldBounds, r.W, r.H, 28, 22)
			st.CameraInit = true
			st.AutoFitPending = false
			st.EnsureVisiblePending = false
		} else if st.EnsureVisiblePending {
			st.EnsureVisiblePending = false
			if sel := st.Root.FindByID(st.SelectedID); sel != nil {
				st.ensureNodeVisible(sel, r.W, r.H)
			}
		}
		hoveredID := st.HoveredID
		hoveredHit := st.HoveredHit
		if !canvasHovered || st.LinkNodeID != 0 || st.ExportMenuOpen || st.ShortcutsOpen || st.ImportResult != nil {
			hoveredID = 0
			hoveredHit = HitNone
		}
		findCurID := 0
		if st.FindOpen && len(st.FindMatchIDs) > 0 && st.FindCurrentIdx >= 0 && st.FindCurrentIdx < len(st.FindMatchIDs) {
			findCurID = st.FindMatchIDs[st.FindCurrentIdx]
		}
		opt := PaintOptions{
			SelectedID: st.SelectedID,
			HoveredID:  hoveredID,
			HoveredHit: hoveredHit,
			EditingID:  paintEditID,
			Palette:    pal,
			Background: pal.CodeBG,
			ReadOnly:   cb.ReadOnly,
			LinkStatusFn: func(target string) LinkStatus {
				return st.resolveLinkStatusCached(now, target, cb)
			},
			FindActive:    st.FindOpen && strings.TrimSpace(st.FindQuery) != "",
			FindMatchIDs:  st.FindMatchSet,
			FindCurrentID: findCurID,
		}
		if st.DragActive && st.DragNodeID != 0 {
			opt.DragNodeID = st.DragNodeID
			opt.DragTargetID = st.DragTargetID
			opt.DragPos = st.DragPos
			opt.DragGhostX = st.DragCurrX
			opt.DragGhostY = st.DragCurrY
		}
		PaintScene(p, st.Root, r, st.Camera, opt)
	})

	// Floating link hover tooltip when pointer hovers a node's link badge.
	if canvasHovered && st.HoveredID != 0 && st.HoveredHit == HitLinkBadge && st.EditingID == 0 && st.LinkNodeID == 0 && !st.DragActive && !st.Panning && st.Root != nil {
		if hn := st.Root.FindByID(st.HoveredID); hn != nil && strings.TrimSpace(hn.Link) != "" {
			st.renderLinkHoverTooltip(c, id, hn, cw, ch, cb)
		}
	}

	// Bottom-left selected node quick action bar (Task toggle, Link edit, Add child).
	if !cb.ReadOnly && st.SelectedID != 0 && st.EditingID == 0 && st.LinkNodeID == 0 && !st.DragActive && !st.Panning && !st.ExportMenuOpen && !st.ShortcutsOpen && st.ImportResult == nil && st.Root != nil {
		if sel := st.Root.FindByID(st.SelectedID); sel != nil {
			st.renderSelectionQuickBar(c, id, sel, cb)
		}
	}

	// Bottom-right floating Zoom & Fold-All HUD.
	if st.Root != nil && !st.ShortcutsOpen {
		st.renderZoomHUD(c, id)
	}

	// Transparent inline IME composition preview while directly typing CJK on a selected node.
	if st.ComposeText != "" && st.EditingID == 0 && !cb.ReadOnly && st.Root != nil {
		if n := st.selectedNode(); n != nil {
			m := st.inlineEditMetrics(n, pal, st.ComposeText)
			compBox := ui.Row(c.Key("mm-ime-compose-"+id)).
				Absolute().Left(m.x).Top(m.y).Width(m.w).Height(m.h).
				Background(ui.Transparent).IgnoreInput(true).
				AlignItems(ui.Center).Justify(m.align)
			compBox.Children(func() {
				ui.Text(c, st.ComposeText).
					FontSize(m.fontSize).FontWeight(m.fontWeight).
					TextColor(m.textColor).Underline().SingleLine()
			})
		}
	}

	// Transparent multi-line inline node editor aligned 1:1 inside the Scheme C vector node.
	if st.EditingID != 0 && !cb.ReadOnly {
		if n := st.Root.FindByID(st.EditingID); n != nil {
			input := ui.TextAreaBase(c.Key(fmt.Sprintf("mm-inline-edit-%s-%d-%d", id, st.EditingID, st.EditSeq)), &st.EditDraft)
			if updatedText := st.EditDraft + st.EditCompose; updatedText != activeEditText {
				activeEditText = updatedText
				st.editLastLayoutText = updatedText
				st.Measure.SetTextOverride(st.EditingID, updatedText)
				ComputeLayout(st.Root, st.Layout, c, &st.Measure)
				if en := st.Root.FindByID(st.EditingID); en != nil {
					n = en
					scale := st.Camera.Scale
					if scale <= 0 {
						scale = 1
					}
					if en.W <= (cw-72)/scale {
						st.ensureNodeVisible(en, cw, ch)
					}
				}
			}
			m := st.inlineEditMetrics(n, pal, activeEditText)
			input.
				Absolute().Left(m.x).Top(m.y).Width(m.w).Height(m.h).
				Padding(m.padTop, 0, 0, 0).
				Background(ui.Transparent).
				FontSize(m.fontSize).FontWeight(m.fontWeight).
				FixedLineHeight(m.lineHeight).
				NoWrap().Scrollbars(false).
				TextAlign(m.align).TextColor(m.textColor).
				CaretColor(m.caretColor).CaretWidth(m.caretWidth).
				SelectionColor(m.selColor).
				Bind(&st.EditHandle)
			if st.EditFocusPending {
				st.EditFocusPending = false
				if st.EditSelectAll {
					st.EditHandle.SelectAll(c)
				} else {
					st.EditHandle.Focus()
				}
			}
			input.ObserveInput(func(ev ui.InputEvent) {
				switch ev.Kind {
				case ui.InputCompose:
					st.EditCompose = ev.Text
				case ui.InputText, ui.InputKeyDown, ui.InputCommand:
					st.EditCompose = ""
				}
			})
			input.OnTextKey(func(k ui.TextKey) bool {
				if st.captureBlockTextKey(k) {
					st.EditDraft = k.Text()
					return true
				}
				switch k.Key {
				case ui.KeyEnter:
					if k.Mods == ui.Shift {
						// Shift+Enter inserts a line break inside the topic text.
						return false
					}
					if k.Mods == 0 {
						st.EditDraft = k.Text()
						st.finishEdit(true, cb)
						st.CanvasHandle.Focus()
						return true
					}
				case ui.KeyEscape:
					st.finishEdit(false, cb)
					st.CanvasHandle.Focus()
					return true
				case ui.KeyTab:
					if k.Mods == ui.Shift {
						st.EditDraft = k.Text()
						st.finishEdit(true, cb)
						st.opInsertParent(cb)
						return true
					}
					if k.Mods == 0 {
						st.EditDraft = k.Text()
						st.finishEdit(true, cb)
						st.opAddChild(cb)
						return true
					}
				}
				return false
			})
			if input.FocusLeft() {
				st.finishEdit(true, cb)
			}
		}
	}
}

type inlineEditMetrics struct {
	x, y, w, h float32
	padTop     float32
	lineHeight float32
	fontSize   float32
	fontWeight int
	textColor  ui.Color
	caretColor ui.Color
	selColor   ui.Color
	caretWidth float32
	align      ui.Align
}

func (st *BlockState) inlineEditMetrics(n *Node, pal theme.Palette, activeText string) inlineEditMetrics {
	scale := st.Camera.Scale
	if scale <= 0 {
		scale = 1
	}
	sx, sy := st.Camera.WorldToScreen(n.X, n.Y-n.H/2)
	sw := n.W * scale
	sh := n.H * scale
	edY := sy
	edH := sh
	if n.Depth > 1 {
		edY = sy - layoutLeafGap*scale
		edH = (n.H + layoutLeafGap) * scale
	}
	lineCount := strings.Count(activeText, "\n") + 1
	if lineCount < 1 {
		lineCount = 1
	}
	lineH := max(nodeLineHeight(n.Depth)*scale, 8)
	textBlockH := float32(lineCount) * lineH
	_, centerY := st.Camera.WorldToScreen(n.X, nodeTextCenterY(n))
	padTop := max(centerY-textBlockH*0.5-edY, 0)

	insetX := max(float32(2)*scale, 1.5)
	rightReserve := insetX
	if strings.TrimSpace(n.Link) != "" {
		rightReserve = (nodePadRight(n) + layoutLinkBadge + layoutLinkGap) * scale
	}
	edX := sx + insetX
	edW := max(sw-insetX-rightReserve, 24)
	align := ui.Center
	if n.Task != nil {
		textLeft := nodeContentLeft(n) + layoutTaskBox + layoutTaskGap
		offX := (textLeft - n.X) * scale
		edX = sx + offX
		edW = max(sw-offX-rightReserve, 24)
		align = ui.Start
	}

	branches := pal.MindMapBranchColors()
	branchCol := branches[n.Branch%len(branches)]
	textCol := pal.Text
	caretCol := branchCol
	selCol := branchCol.Alpha(0.24)
	if pal.Dark {
		selCol = branchCol.Alpha(0.34)
	}
	if n.Depth == 0 {
		textCol = pal.Paper
		caretCol = pal.Paper
		if pal.Dark {
			selCol = pal.Accent.Alpha(0.36)
		} else {
			selCol = pal.Accent.Alpha(0.48)
		}
	}

	return inlineEditMetrics{
		x:          edX,
		y:          edY,
		w:          edW,
		h:          edH,
		padTop:     padTop,
		lineHeight: lineH,
		fontSize:   max(nodeFontSize(n.Depth)*scale, 6),
		fontWeight: nodeFontWeight(n.Depth),
		textColor:  textCol,
		caretColor: caretCol,
		selColor:   selCol,
		caretWidth: max(float32(1.6)*scale, 1.2),
		align:      align,
	}
}

func isDirectEditText(s string) bool {
	if s == "" {
		return false
	}
	hasGraphic := false
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
		if !unicode.IsSpace(r) {
			hasGraphic = true
		}
	}
	return hasGraphic
}

// ZoomBy zooms the canvas around its viewport center by factor.
func (st *BlockState) ZoomBy(factor float32) {
	if st == nil || factor <= 0 {
		return
	}
	st.zoomAt(st.LastCanvasW*0.5, st.LastCanvasH*0.5, st.Camera.Scale*factor)
}

// FitCanvas schedules an automatic zoom-to-fit on the next paint pass.
func (st *BlockState) FitCanvas() {
	if st == nil {
		return
	}
	st.fsCamAnimActive = false
	st.AutoFitPending = true
}

// canvasActionsLive reports whether floating −/+ action buttons are currently drawn,
// so hit-testing never triggers a button the user cannot see.
func (st *BlockState) canvasActionsLive() bool {
	return st.EditingID == 0 && st.DragNodeID == 0 && st.LinkNodeID == 0 &&
		!st.ExportMenuOpen && !st.ShortcutsOpen && st.ImportResult == nil
}

// refreshHoverAt recomputes hover state for the pointer at screen (sx, sy),
// e.g. after the camera moved under a stationary pointer.
func (st *BlockState) refreshHoverAt(sx, sy float32, readOnly bool) {
	if st.Root == nil || st.Camera.Scale <= 0 {
		return
	}
	activeID := st.HoveredID
	if !st.canvasActionsLive() {
		activeID = 0
	}
	wx, wy := st.Camera.ScreenToWorld(sx, sy)
	hit := HitTestWithActions(st.Root, [2]int{activeID, 0}, readOnly, wx, wy, st.Camera.Scale)
	if hit.Node != nil {
		st.HoveredID = hit.Node.ID
		st.HoveredHit = hit.Kind
	} else {
		st.HoveredID = 0
		st.HoveredHit = HitNone
	}
}

func (st *BlockState) zoomAt(sx, sy, targetScale float32) {
	st.fsCamAnimActive = false
	targetScale = clampFloat32(targetScale, 0.2, 3.0)
	if st.Camera.Scale <= 0 {
		st.Camera.Scale = 1
	}
	wx, wy := st.Camera.ScreenToWorld(sx, sy)
	st.Camera.Scale = targetScale
	st.Camera.X = wx - sx/targetScale
	st.Camera.Y = wy - sy/targetScale
}

func (st *BlockState) handleCanvasKey(c *ui.Context, key ui.Key, mods ui.Modifiers, cb Callbacks) bool {
	n := st.selectedNode()
	if n == nil {
		return false
	}
	leftSide := n.Dir < 0

	// Cmd/Ctrl shortcuts
	if mods == ui.Cmd || mods == ui.Ctrl {
		switch key {
		case ui.KeyZ:
			st.opUndo(cb)
			return true
		case ui.KeyY:
			st.opRedo(cb)
			return true
		case ui.KeyC:
			return st.opCopy(c)
		case ui.KeyX:
			return st.opCut(c, cb)
		case ui.KeyV:
			return st.opPaste(c, cb)
		case ui.KeyD:
			st.opDuplicate(cb)
			return true
		case ui.KeyF:
			st.OpenFind()
			return true
		case ui.KeyL:
			if !cb.ReadOnly {
				st.beginLinkEdit(n)
				return true
			}
		case ui.KeyEnter:
			st.opToggleTaskDoneOrCreate(n, cb)
			return true
		case ui.KeySlash:
			st.opToggleFold()
			return true
		case ui.Key0:
			st.FitCanvas()
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
	if mods == ui.Cmd|ui.Shift || mods == ui.Ctrl|ui.Shift {
		switch key {
		case ui.KeyZ:
			st.opRedo(cb)
			return true
		case ui.KeyEqual:
			st.zoomAt(st.LastCanvasW*0.5, st.LastCanvasH*0.5, st.Camera.Scale*1.15)
			return true
		case ui.KeyL:
			st.ToggleLayout()
			return true
		}
		return false
	}
	if mods == ui.Cmd|ui.Alt || mods == ui.Ctrl|ui.Alt {
		switch key {
		case ui.KeyT:
			st.opToggleTask(n, cb)
			return true
		case ui.KeySlash:
			st.opToggleExpandCollapseAll()
			return true
		}
		return false
	}

	// Shift shortcuts: Shift+Enter (sibling above), Shift+Tab (insert parent), Shift+Backspace (delete single)
	if mods == ui.Shift {
		switch key {
		case ui.KeyEnter:
			st.opAddSiblingBefore(cb)
			return true
		case ui.KeyTab:
			st.opInsertParent(cb)
			return true
		case ui.KeyBackspace, ui.KeyDelete:
			st.opDeleteSingle(cb)
			return true
		}
		return false
	}

	// Alt shortcuts: reorder (Up/Down), promote/demote (Left/Right), delete single node (Backspace)
	if mods == ui.Alt {
		switch key {
		case ui.KeyUp:
			st.opMoveSibling(-1, cb)
			return true
		case ui.KeyDown:
			st.opMoveSibling(1, cb)
			return true
		case ui.KeyLeft:
			if leftSide {
				st.opDemote(cb)
			} else {
				st.opPromote(cb)
			}
			return true
		case ui.KeyRight:
			if leftSide {
				st.opPromote(cb)
			} else {
				st.opDemote(cb)
			}
			return true
		case ui.KeyBackspace, ui.KeyDelete:
			st.opDeleteSingle(cb)
			return true
		}
		return false
	}

	if mods != 0 {
		return false
	}

	switch key {
	case ui.KeyTab, ui.KeyInsert:
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
	case ui.KeyHome:
		st.opNavigate("home")
		return true
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
				st.EnsureVisiblePending = true
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

// --- Canvas Floating HUDs & Tooltips ---

func (st *BlockState) renderLinkHoverTooltip(c *ui.Context, id string, n *Node, cw, ch float32, cb Callbacks) {
	pal := theme.Ocean()
	status := st.resolveLinkStatusCached(c.Now(), n.Link, cb)
	lb := nodeLinkBadgeCircle(n)
	sx, sy := st.Camera.WorldToScreen(lb.cx, lb.cy)
	scale := st.Camera.Scale
	if scale <= 0 {
		scale = 1
	}

	// Split link into a readable title (file name without extension) and a dim directory hint.
	link := strings.TrimSpace(n.Link)
	title := strings.TrimSuffix(filepath.Base(link), filepath.Ext(link))
	if title == "" || title == "." {
		title = link
	}
	dir := filepath.Dir(link)
	for strings.HasPrefix(dir, "../") {
		dir = strings.TrimPrefix(dir, "../")
	}
	if dir == "." || dir == ".." || dir == "/" {
		dir = ""
	}

	glyph := icons.Note
	fg := pal.Text
	sub := pal.TextSecondary
	icCol := pal.Accent
	bg := pal.TooltipBG
	border := pal.TooltipBorder
	hint := dir
	if status == LinkStatusBroken {
		glyph = icons.LinkOff
		fg = pal.Warning
		sub = pal.Warning.Alpha(0.75)
		icCol = pal.Warning
		bg = pal.WarningSurface
		border = pal.Warning.Alpha(0.55)
		hint = theme.Tr("笔记不存在 · 点击重连", "Missing · click to relink")
	}

	const maxTipW float32 = 280
	const tipH float32 = 30
	tx := clampFloat32(sx-40, theme.Space8, max(theme.Space8, cw-maxTipW-theme.Space8))
	ty := sy - lb.r*scale - tipH - theme.Space8
	if ty < theme.Space8 {
		ty = sy + lb.r*scale + theme.Space8
	}
	ty = clampFloat32(ty, theme.Space8, max(theme.Space8, ch-tipH-theme.Space8))

	tip := ui.Row(c.Key("mm-link-tip-"+id)).Absolute().Left(tx).Top(ty).
		MaxWidth(maxTipW).Height(tipH).PaddingX(theme.Space8).AlignItems(ui.Center).Gap(theme.Space6).
		Radius(theme.RadiusMD).Background(bg).Border(1, border).IgnoreInput(true)
	bui.Elevate(tip, theme.ElevationPopover)
	tip.Children(func() {
		icons.Draw(c, glyph, theme.IconMicro, pal.DuoIconColors(icCol))
		ui.Text(c, title).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).
			TextColor(fg).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(maxTipW * 0.55)
		if hint != "" {
			ui.Text(c, hint).FontSize(theme.FontMeta).TextColor(sub).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
	})
}

func (st *BlockState) renderSelectionQuickBar(c *ui.Context, id string, sel *Node, cb Callbacks) {
	pal := theme.Ocean()
	bar := ui.Row(c.Key("mm-sel-bar-"+id)).Absolute().Left(theme.Space10).Bottom(theme.Space10).
		Height(theme.HeightChrome).PaddingX(theme.Space2).AlignItems(ui.Center).Gap(theme.Space2).
		Radius(theme.RadiusPill).Background(pal.Menu).Border(1, pal.MenuBorder)
	bui.Elevate(bar, theme.ElevationPopover)
	bar.Children(func() {
		renderPillBtn := func(key string, icon icons.Icon, label string, active, warn bool, onClick func()) {
			btn := ui.ButtonBase(c.Key(key)).
				Height(theme.HeightMicro).PaddingX(theme.Space6).Radius(theme.RadiusPill).
				AlignItems(ui.Center).Gap(theme.Space4).Cursor(ui.CursorPointer).Shrink(0).Label(label)
			hov := btn.Hovered() || btn.Pressed()
			bg := ui.Transparent
			fg := pal.TextSecondary
			if warn {
				bg = pal.WarningSurface
				fg = pal.Warning
			} else if active {
				bg = pal.SelectionBG
				fg = pal.Accent
			} else if hov {
				bg = pal.Hover
				fg = pal.Text
			}
			btn.Background(bg)
			btn.Children(func() {
				icons.DrawAnimated(c, icon, theme.IconMicro, pal.MonoIconColors(fg), active || hov || warn)
				ui.Text(c, label).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
			})
			if btn.Clicked() && onClick != nil {
				onClick()
			}
		}

		// One-click task toggle for non-root nodes.
		if sel.Parent != nil {
			taskLbl := theme.Tr("设任务", "Task")
			if sel.Task != nil {
				taskLbl = theme.Tr("取消任务", "No Task")
			}
			renderPillBtn("mm-qb-task-"+id, icons.Todo, taskLbl, sel.Task != nil, false, func() {
				st.opToggleTask(sel, cb)
			})
			if sel.Task != nil {
				doneLbl := theme.Tr("完成", "Done")
				if *sel.Task {
					doneLbl = theme.Tr("未完", "Undone")
				}
				renderPillBtn("mm-qb-done-"+id, icons.Check, doneLbl, *sel.Task, false, func() {
					st.opFlipTaskDone(sel, cb)
				})
			}
		}

		// Link button (shows broken state if link is missing).
		hasLink := strings.TrimSpace(sel.Link) != ""
		linkWarn := hasLink && st.resolveLinkStatusCached(c.Now(), sel.Link, cb) == LinkStatusBroken
		linkLbl := theme.Tr("链接", "Link")
		if linkWarn {
			linkLbl = theme.Tr("重连", "Fix Link")
		} else if hasLink {
			linkLbl = theme.Tr("编辑链接", "Edit Link")
		}
		linkIcon := icons.TbLink
		if linkWarn {
			linkIcon = icons.LinkOff
		} else if hasLink {
			linkIcon = icons.LinkEdit
		}
		renderPillBtn("mm-qb-link-"+id, linkIcon, linkLbl, hasLink && !linkWarn, linkWarn, func() {
			if linkWarn {
				st.beginLinkEditBroken(sel)
			} else {
				st.beginLinkEdit(sel)
			}
		})

	})
}

func (st *BlockState) renderZoomHUD(c *ui.Context, id string) {
	pal := theme.Ocean()
	k := func(mac, other string) string {
		if theme.IsMacOS() {
			return mac
		}
		return other
	}
	hud := ui.Row(c.Key("mm-zoom-hud-"+id)).Absolute().Right(theme.Space10).Bottom(theme.Space10).
		Height(theme.HeightChrome).PaddingX(theme.Space4).AlignItems(ui.Center).Gap(theme.Space2).
		Radius(theme.RadiusPill).Background(pal.Menu).Border(1, pal.MenuBorder)
	bui.Elevate(hud, theme.ElevationPopover)
	hud.Children(func() {
		anyFolded := st.hasAnyFolded()
		foldIcon := icons.FoldAll
		foldTip := theme.Tr("折叠所有子分支", "Collapse all branches")
		if anyFolded {
			foldIcon = icons.UnfoldAll
			foldTip = theme.Tr("展开所有分支", "Expand all branches")
		}
		foldBtn := bui.IconButton(c, foldIcon, bui.IconButtonOptions{
			ID:       "mm-hud-fold-" + id,
			Label:    foldTip,
			Tooltip:  foldTip,
			Shortcut: k("⌘⌥/", "Ctrl+Alt+/"),
			Size:     theme.HeightMicro + theme.Space4,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusPill,
		})
		if foldBtn.Clicked() {
			st.opToggleExpandCollapseAll()
		}

		ui.Box(c).Size(1, theme.Space12).Background(pal.Hairline).Shrink(0)

		outBtn := bui.IconButton(c, icons.TbMinus, bui.IconButtonOptions{
			ID:       "mm-hud-zout-" + id,
			Label:    theme.Tr("缩小", "Zoom out"),
			Tooltip:  theme.Tr("缩小", "Zoom out"),
			Shortcut: k("⌘-", "Ctrl+-"),
			Size:     theme.HeightMicro + theme.Space4,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusPill,
		})
		if outBtn.Clicked() {
			st.ZoomBy(1.0 / 1.15)
		}

		scalePct := int(math.Round(float64(st.Camera.Scale * 100)))
		if scalePct <= 0 {
			scalePct = 100
		}
		pctBtn := ui.ButtonBase(c.Key("mm-hud-zpct-"+id)).
			Height(theme.HeightMicro + theme.Space4).MinWidth(42).PaddingX(theme.Space4).
			Radius(theme.RadiusPill).Center().Cursor(ui.CursorPointer).Shrink(0).
			Label(theme.Tr("适配画布", "Fit to view"))
		if pctBtn.Hovered() || pctBtn.Pressed() {
			pctBtn.Background(pal.Hover)
		}
		bui.TooltipShortcut(c, pctBtn, theme.Tr("点击适配画布", "Click to fit view"), k("⌘0", "Ctrl+0"))
		pctBtn.Children(func() {
			ui.Text(c, fmt.Sprintf("%d%%", scalePct)).
				FontSize(theme.FontMeta).FontWeight(theme.WeightMono).TextColor(pal.Text).SingleLine()
		})
		if pctBtn.Clicked() {
			st.FitCanvas()
		}

		inBtn := bui.IconButton(c, icons.TbPlus, bui.IconButtonOptions{
			ID:       "mm-hud-zin-" + id,
			Label:    theme.Tr("放大", "Zoom in"),
			Tooltip:  theme.Tr("放大", "Zoom in"),
			Shortcut: k("⌘+", "Ctrl++"),
			Size:     theme.HeightMicro + theme.Space4,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusPill,
		})
		if inBtn.Clicked() {
			st.ZoomBy(1.15)
		}
	})
}

func (st *BlockState) renderFindBar(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	bar := ui.Row(c.Key("mm-find-bar-"+id)).Absolute().Top(theme.Space8).Right(theme.Space10).
		Height(theme.HeightControl + theme.Space4).PaddingX(theme.Space6).AlignItems(ui.Center).Gap(theme.Space4).
		Radius(theme.RadiusLG).Background(pal.Menu).Border(1, pal.MenuBorder)
	bui.Elevate(bar, theme.ElevationPopover)
	bar.Children(func() {
		icons.DrawAnimated(c, icons.FindSearch, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary), false)
		prevQuery := st.FindQuery
		inp := ui.TextInputBase(c.Key("mm-find-in-"+id), &st.FindQuery).
			Width(156).Height(theme.HeightCompact).PaddingX(theme.Space4).
			Background(ui.Transparent).BorderWidth(0).
			FontSize(theme.FontSmall).TextColor(pal.Text).
			Placeholder(theme.Tr("查找节点或链接…", "Find nodes or links…")).
			PlaceholderColor(pal.Placeholder).CaretColor(pal.Accent).
			Bind(&st.FindHandle)
		if st.FindFocusPending {
			st.FindFocusPending = false
			st.FindHandle.SelectAll(c)
		}
		inp.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			switch ev.Key {
			case ui.KeyEscape:
				st.CloseFind()
				st.CanvasHandle.Focus()
				return true
			case ui.KeyEnter:
				if (ev.Mods & ui.Shift) != 0 {
					st.stepFind(-1)
				} else {
					st.stepFind(1)
				}
				return true
			case ui.KeyDown:
				st.stepFind(1)
				return true
			case ui.KeyUp:
				st.stepFind(-1)
				return true
			}
			return false
		})
		if st.FindQuery != prevQuery {
			st.FindCurrentIdx = 0
			st.recomputeFind(true)
		}

		qTrim := strings.TrimSpace(st.FindQuery)
		countText := ""
		countCol := pal.TextSecondary
		if qTrim != "" {
			if len(st.FindMatchIDs) == 0 {
				countText = "0/0"
				countCol = pal.Danger
			} else {
				countText = fmt.Sprintf("%d/%d", st.FindCurrentIdx+1, len(st.FindMatchIDs))
			}
		}
		if countText != "" {
			ui.Text(c, countText).FontSize(theme.FontMeta).FontWeight(theme.WeightMono).
				TextColor(countCol).PaddingX(theme.Space4).SingleLine().Shrink(0)
		}

		hasMatches := len(st.FindMatchIDs) > 0
		prevBtn := bui.IconButton(c, icons.ChevronUp, bui.IconButtonOptions{
			ID:       "mm-find-prev-" + id,
			Label:    theme.Tr("上一个", "Previous match"),
			Size:     theme.HeightMicro + theme.Space2,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusSM,
			Disabled: !hasMatches,
		})
		if prevBtn.Clicked() && hasMatches {
			st.stepFind(-1)
		}
		nextBtn := bui.IconButton(c, icons.ChevronDown, bui.IconButtonOptions{
			ID:       "mm-find-next-" + id,
			Label:    theme.Tr("下一个", "Next match"),
			Size:     theme.HeightMicro + theme.Space2,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusSM,
			Disabled: !hasMatches,
		})
		if nextBtn.Clicked() && hasMatches {
			st.stepFind(1)
		}
		closeBtn := bui.IconButton(c, icons.Close, bui.IconButtonOptions{
			ID:       "mm-find-close-" + id,
			Label:    theme.Tr("关闭查找", "Close find"),
			Size:     theme.HeightMicro + theme.Space2,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusSM,
		})
		if closeBtn.Clicked() {
			st.CloseFind()
			st.CanvasHandle.Focus()
		}
	})
}

// --- Source View ---

func (st *BlockState) renderSourceView(c *ui.Context, id string, fullscreen bool, cb Callbacks) {
	st.LastCanvasFocused = false
	pal := theme.Ocean()
	draft := st.LastSource
	if draft == "" && st.Root != nil {
		draft = SerializeMarkdown(st.Root)
	}
	prev := draft
	padY, padX := theme.Space12, theme.Space16
	if fullscreen {
		padY, padX = theme.Space20, theme.Space24
	}
	_, codeFont, _ := theme.Typography()
	codeFont = strings.TrimSpace(codeFont)
	if codeFont == "" || strings.EqualFold(codeFont, "auto") {
		codeFont = "JetBrains Mono, Menlo, Monaco, Cascadia Mono, Consolas, DejaVu Sans Mono, Liberation Mono, monospace"
	}

	col := ui.Column(c.Key("mm-src-col-" + id)).Fill()
	col.Children(func() {
		ta := ui.TextAreaBase(c.Key("mm-src-"+id), &draft).FillWidth().Grow(1).MinHeight(0).
			Padding(padY, padX).BorderWidth(0).Background(pal.CodeBG).
			Font(codeFont).FontSize(theme.FontSmall).LineHeight(1.6).
			TextColor(pal.CodeBlock().TextFG).
			Placeholder(theme.Tr("# 中心主题\n- 分支一", "# Central Topic\n- Branch 1")).
			PlaceholderColor(pal.Placeholder).
			CaretColor(pal.Accent).SelectionColor(pal.EditorSelectionBG()).
			Bind(&st.SourceHandle)
		if cb.ReadOnly {
			ta.ReadOnly(true)
		}
		if !fullscreen && st.consumeFullscreenFocusRestore() {
			ta.Focus(ui.FocusOptions{PreventScroll: true})
			if cb.OnRequestFocus != nil {
				cb.OnRequestFocus()
			}
		}
		if st.viewFocusPending {
			st.viewFocusPending = false
			ta.Focus(ui.FocusOptions{PreventScroll: true})
		}
		ta.OnTextKey(st.captureBlockTextKey)
		bui.AutoHideScrollbars(c, ta, &st.SourceScroll)
		if !cb.ReadOnly && (ta.Changed() || draft != prev) {
			st.SyncSource(draft, Config{})
			if cb.OnChange != nil {
				cb.OnChange(draft)
			}
		}

		// Bottom syntax status bar with live counts, syntax warnings, and quick template inserts.
		issues := CheckSourceSyntax(draft)
		nodes, tasks, links := st.Root.CountStats()
		bar := ui.Row(c.Key("mm-src-bar-"+id)).FillWidth().Height(theme.HeightControl).Shrink(0).
			PaddingX(theme.Space10).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).
			Background(pal.HintBand)
		bar.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Line(r.X, r.Y+0.5, r.X+r.W, r.Y+0.5, 1, pal.Hairline)
		})
		bar.Children(func() {
			left := ui.Row(c.Key("mm-src-stat-"+id)).AlignItems(ui.Center).Gap(theme.Space8).Grow(1).Shrink(1).MinWidth(0)
			left.Children(func() {
				statStr := fmt.Sprintf(
					theme.Tr("%d 节点 · %d 任务 · %d 链接", "%d nodes · %d tasks · %d links"),
					nodes, tasks, links,
				)
				ui.Text(c, statStr).FontSize(theme.FontMeta).FontWeight(theme.WeightMono).
					TextColor(pal.TextSecondary).SingleLine().Shrink(0)
				if len(issues) > 0 {
					warnMsg := fmt.Sprintf("L%d: %s", issues[0].Line, issues[0].Message)
					if len(issues) > 1 {
						warnMsg += fmt.Sprintf(" (+%d)", len(issues)-1)
					}
					badge := ui.Row(c.Key("mm-src-warn-"+id)).Height(theme.HeightMicro + theme.Space2).
						PaddingX(theme.Space6).AlignItems(ui.Center).Radius(theme.RadiusSM).
						Background(pal.WarningSurface).Border(1, pal.Warning.Alpha(0.45)).
						Shrink(1).MinWidth(0)
					badge.Children(func() {
						ui.Text(c, warnMsg).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).
							TextColor(pal.Warning).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
					})
				}
			})

			if !cb.ReadOnly {
				right := ui.Row(c.Key("mm-src-ins-"+id)).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0)
				right.Children(func() {
					insertSnippet := func(snippet string) {
						next := draft
						if next != "" && !strings.HasSuffix(next, "\n") {
							next += "\n"
						}
						next += snippet
						st.SyncSource(next, Config{})
						if cb.OnChange != nil {
							cb.OnChange(next)
						}
						c.Invalidate()
					}
					renderInsBtn := func(key, label, snippet string) {
						btn := ui.ButtonBase(c.Key(key)).Height(theme.HeightMicro + theme.Space2).
							PaddingX(theme.Space6).Radius(theme.RadiusSM).Border(1, pal.BtnBorder).
							Background(pal.Paper).Center().Cursor(ui.CursorPointer).Shrink(0).Label(label)
						if btn.Hovered() || btn.Pressed() {
							btn.Background(pal.Hover)
						}
						btn.Children(func() {
							ui.Text(c, label).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).
								TextColor(pal.TextSecondary).SingleLine()
						})
						if btn.Clicked() {
							insertSnippet(snippet)
						}
					}
					renderInsBtn("mm-src-ins-task-"+id, theme.Tr("+ 任务", "+ Task"), theme.Tr("- [ ] 待办事项", "- [ ] Todo item"))
					renderInsBtn("mm-src-ins-link-"+id, theme.Tr("+ 内链", "+ Link"), theme.Tr("- [相关笔记](note.md)", "- [Related Note](note.md)"))
				})
			}
		})
	})
}

// --- Export Dropdown Menu ---

func (st *BlockState) renderExportMenu(c *ui.Context, id string, fixedHeight float32, cb Callbacks) {
	pal := theme.Ocean()
	// Backdrop to close on outside click.
	scrim := ui.Box(c.Key("mm-exp-scrim-"+id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(ui.Transparent)
	if scrim.Clicked() {
		st.ExportMenuOpen = false
		return
	}

	availH := st.LastCanvasH
	if availH <= 0 {
		availH = fixedHeight
	}
	if availH <= 0 {
		availH = 310
	}
	maxMenuH := max(float32(140), availH-theme.Space12)

	panel := ui.Column(c.Key("mm-exp-menu-"+id)).Absolute().Top(theme.Space6).Right(theme.Space8).
		Width(228).MaxHeight(maxMenuH).Padding(theme.MenuPadding).
		Radius(theme.RadiusLG).Background(pal.Menu).Border(1, pal.MenuBorder)
	bui.Elevate(panel, theme.ElevationPopover)
	panel.Children(func() {
		scrollMaxH := max(float32(120), maxMenuH-theme.MenuPadding*2)
		scr := bui.Scroll(c.Key("mm-exp-scroll-"+id), &st.ExportMenuScroll).
			FillWidth().MaxHeight(scrollMaxH)
		scr.Children(func() {
			content := ui.Column(c.Key("mm-exp-content-" + id)).FillWidth().Gap(theme.MenuGap)
			content.Children(func() {
				// Background theme selector for raster/PDF exports.
				ui.Text(c, theme.Tr("导出背景", "Export background")).
					FontSize(theme.FontMeta).FontWeight(theme.WeightTitle).TextColor(pal.TextSecondary).
					Padding(theme.Space4, theme.Space8, theme.Space2, theme.Space8).SingleLine()

				curTheme := st.ExportTheme
				if curTheme == "" {
					curTheme = ExportThemeAuto
				}
				themeRow := ui.Row(c.Key("mm-exp-theme-"+id)).FillWidth().PaddingX(theme.Space4).Gap(theme.Space2)
				themeRow.Children(func() {
					opts := []struct {
						mode  ExportTheme
						label string
					}{
						{ExportThemeAuto, theme.Tr("跟随", "Auto")},
						{ExportThemeLight, theme.Tr("浅色", "Light")},
						{ExportThemeDark, theme.Tr("深色", "Dark")},
					}
					for _, o := range opts {
						active := curTheme == o.mode
						btn := ui.ButtonBase(c.Key("mm-exp-th-"+id+"-"+string(o.mode))).
							Grow(1).Shrink(1).MinWidth(0).Height(theme.HeightCompact).
							Radius(theme.RadiusSM).Center().Cursor(ui.CursorPointer).Label(o.label)
						if active {
							btn.Background(pal.Selected).Border(1, pal.SelectedBorder)
						} else if btn.Hovered() || btn.Pressed() {
							btn.Background(pal.MenuHover)
						}
						fg := pal.TextSecondary
						if active {
							fg = pal.Text
						}
						btn.Children(func() {
							ui.Text(c, o.label).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
						})
						if btn.Clicked() {
							st.ExportTheme = o.mode
						}
					}
				})

				ui.Box(c).FillWidth().Height(1).Margin(theme.Space4, theme.Space2).Background(pal.MenuSeparator)

				// Quick clipboard copy actions.
				renderClipRow := func(key string, icon icons.Icon, label string, onClick func()) {
					row := ui.ButtonBase(c.Key(key)).
						FillWidth().Height(theme.MenuItemHeight).PaddingX(theme.Space8).Radius(theme.RadiusSM).
						AlignItems(ui.Center).Gap(theme.Space8).Cursor(ui.CursorPointer).Label(label)
					hot := row.Hovered() || row.Pressed()
					if hot {
						row.Background(pal.MenuHover)
					}
					row.Children(func() {
						ic := pal.DuoIconColors(pal.TextSecondary)
						if hot {
							ic = pal.DuoIconColors(pal.Text)
						}
						icons.DrawAnimated(c, icon, theme.IconInline, ic, hot)
						ui.Text(c, label).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
							TextColor(pal.Text).SingleLine().Shrink(1).MinWidth(0)
					})
					if row.Clicked() && onClick != nil {
						onClick()
					}
				}

				renderClipRow("mm-exp-copy-png-"+id, icons.Image, theme.Tr("复制 PNG 图片", "Copy PNG Image"), func() {
					st.copyPNGToClipboard(c, cb)
				})
				renderClipRow("mm-exp-copy-md-"+id, icons.Copy, theme.Tr("复制 Markdown", "Copy Markdown"), func() {
					st.copyMarkdownToClipboard(c, cb)
				})

				ui.Box(c).FillWidth().Height(1).Margin(theme.Space4, theme.Space2).Background(pal.MenuSeparator)

				ui.Text(c, theme.Tr("导出文件", "Export file")).
					FontSize(theme.FontMeta).FontWeight(theme.WeightTitle).TextColor(pal.TextSecondary).
					Padding(theme.Space2, theme.Space8, theme.Space2, theme.Space8).SingleLine()

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
		})
	})
}

// --- Link-to-Note Popover (Anchored to Selected Node with Keyboard Navigation) ---

func (st *BlockState) renderLinkPopover(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	scrim := ui.Box(c.Key("mm-link-scrim-"+id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(ui.Transparent)
	if scrim.Clicked() {
		st.LinkNodeID = 0
		st.LinkBrokenNotice = false
		return
	}

	var targetNode *Node
	if st.Root != nil {
		targetNode = st.Root.FindByID(st.LinkNodeID)
	}
	if targetNode == nil {
		st.LinkNodeID = 0
		st.LinkBrokenNotice = false
		return
	}

	// Filter candidate notes upfront so Up/Down/Enter keyboard navigation and list rendering share exact items.
	var filtered []NoteCandidate
	if cb.OnListNotes != nil {
		notes := cb.OnListNotes()
		q := strings.ToLower(strings.TrimSpace(st.LinkDraft))
		for _, nc := range notes {
			disp := nc.DisplayPath
			if disp == "" {
				disp = nc.RelPath
			}
			if q != "" && !strings.Contains(strings.ToLower(nc.Title), q) &&
				!strings.Contains(strings.ToLower(nc.RelPath), q) &&
				!strings.Contains(strings.ToLower(disp), q) {
				continue
			}
			filtered = append(filtered, nc)
			if len(filtered) >= 30 {
				break
			}
		}
	}
	if st.LinkActiveIdx >= len(filtered) {
		st.LinkActiveIdx = len(filtered) - 1
	}
	if st.LinkActiveIdx < -1 {
		st.LinkActiveIdx = -1
	}

	const (
		linkListMaxH      float32 = 160
		linkCandidateRowH float32 = 40
		linkCandidateGap  float32 = theme.Space2
	)
	ensureCandidateVisible := func(idx int) {
		if idx < 0 || idx >= len(filtered) {
			return
		}
		step := linkCandidateRowH + linkCandidateGap
		rowTop := float32(idx) * step
		rowBot := rowTop + linkCandidateRowH
		if rowTop < st.LinkListScroll.Y {
			st.LinkListScroll.Y = rowTop
		} else if rowBot > st.LinkListScroll.Y+linkListMaxH {
			st.LinkListScroll.Y = rowBot - linkListMaxH
		}
		totalH := float32(len(filtered))*step - linkCandidateGap
		maxY := max(float32(0), totalH-linkListMaxH)
		if st.LinkListScroll.MaxY > 0 {
			maxY = st.LinkListScroll.MaxY
		}
		st.LinkListScroll.Y = clampFloat32(st.LinkListScroll.Y, 0, maxY)
	}

	const popW float32 = 312
	estH := float32(124)
	if st.LinkBrokenNotice {
		estH += 34
	}
	if len(filtered) > 0 {
		estH += min(float32(len(filtered))*38, 160) + theme.Space8
	}

	panel := ui.Column(c.Key("mm-link-pop-"+id)).Absolute().
		Width(popW).Padding(theme.Space12).Gap(theme.Space8).
		Radius(theme.RadiusLG).Background(pal.Menu).Border(1, pal.MenuBorder)

	// Anchor popover cleanly near the target node when in Map view, flipping above if near bottom edge.
	if st.View == ViewMap && st.LastCanvasW > 0 && st.LastCanvasH > 0 {
		scale := st.Camera.Scale
		if scale <= 0 {
			scale = 1
		}
		sx, sy := st.Camera.WorldToScreen(targetNode.X, targetNode.Y-targetNode.H/2)
		sh := targetNode.H * scale
		px := clampFloat32(sx, theme.Space8, max(theme.Space8, st.LastCanvasW-popW-theme.Space8))
		py := sy + sh + theme.Space8
		if py+estH > st.LastCanvasH-theme.Space8 {
			above := sy - estH - theme.Space8
			if above >= theme.Space8 {
				py = above
			} else {
				py = clampFloat32(py, theme.Space8, max(theme.Space8, st.LastCanvasH-estH-theme.Space8))
			}
		}
		panel.Left(px).Top(py)
	} else {
		panel.Top(theme.Space12).Right(theme.Space12)
	}

	bui.Elevate(panel, theme.ElevationPopover)
	panel.Children(func() {
		ui.Text(c, theme.Tr("链接到笔记或网址", "Link to Note or URL")).
			FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()

		if st.LinkBrokenNotice {
			warn := ui.Row(c.Key("mm-link-warn-"+id)).FillWidth().Padding(theme.Space6, theme.Space8).
				Radius(theme.RadiusSM).Background(pal.WarningSurface).Border(1, pal.Warning.Alpha(0.45))
			warn.Children(func() {
				ui.Text(c, theme.Tr("目标笔记不存在或已移动，请重新选择或移除链接", "Target note missing or moved; pick a new target or remove link")).
					FontSize(theme.FontMeta).TextColor(pal.Warning)
			})
		}

		prevDraft := st.LinkDraft
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
			case ui.KeyDown:
				if len(filtered) > 0 {
					st.LinkActiveIdx = (st.LinkActiveIdx + 1) % len(filtered)
					ensureCandidateVisible(st.LinkActiveIdx)
					return true
				}
			case ui.KeyUp:
				if len(filtered) > 0 {
					if st.LinkActiveIdx <= 0 {
						st.LinkActiveIdx = len(filtered) - 1
					} else {
						st.LinkActiveIdx--
					}
					ensureCandidateVisible(st.LinkActiveIdx)
					return true
				}
			case ui.KeyEnter:
				if st.LinkActiveIdx >= 0 && st.LinkActiveIdx < len(filtered) {
					st.applyLink(filtered[st.LinkActiveIdx].RelPath, cb)
				} else {
					st.applyLink(st.LinkDraft, cb)
				}
				if st.View == ViewMap {
					st.CanvasHandle.Focus()
				}
				return true
			case ui.KeyEscape:
				st.LinkNodeID = 0
				st.LinkBrokenNotice = false
				if st.View == ViewMap {
					st.CanvasHandle.Focus()
				}
				return true
			}
			return false
		})
		if st.LinkDraft != prevDraft {
			st.LinkActiveIdx = -1
			st.LinkListScroll.Y = 0
		}

		// Scrollable candidate list with relative directory path.
		if len(filtered) > 0 {
			listScr := bui.Scroll(c.Key("mm-link-list-"+id), &st.LinkListScroll).
				FillWidth().MaxHeight(linkListMaxH)
			listScr.Children(func() {
				listCol := ui.Column(c.Key("mm-link-col-" + id)).FillWidth().Gap(linkCandidateGap)
				listCol.Children(func() {
					for i, nc := range filtered {
						idx := i
						cand := nc
						isActive := st.LinkActiveIdx == idx
						row := ui.ButtonBase(c.Key("mm-link-note-"+id+"-"+cand.RelPath)).
							FillWidth().Height(linkCandidateRowH).PaddingX(theme.Space8).Radius(theme.RadiusSM).
							AlignItems(ui.Center).Gap(theme.Space8).Cursor(ui.CursorPointer).Label(cand.Title)
						rowHot := row.Hovered() || row.Pressed()
						if isActive {
							row.Background(pal.SelectionBG)
						} else if rowHot {
							row.Background(pal.MenuHover)
						}
						row.Children(func() {
							icCol := pal.TextSecondary
							if isActive {
								icCol = pal.Accent
							} else if rowHot {
								icCol = pal.Text
							}
							icons.DrawAnimated(c, icons.Note, theme.IconInline, pal.DuoIconColors(icCol), isActive || rowHot)
							infoCol := ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(theme.Space2)
							infoCol.Children(func() {
								titleCol := pal.Text
								if isActive {
									titleCol = pal.SelectionFG
								}
								ui.Text(c, cand.Title).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
									TextColor(titleCol).SingleLine().Ellipsis("…")
								dispPath := cand.DisplayPath
								if dispPath == "" {
									dispPath = cand.RelPath
								}
								dir := filepath.ToSlash(filepath.Dir(dispPath))
								pathLabel := dispPath
								if dir != "." && dir != "" {
									pathLabel = dir + "/"
								}
								ui.Text(c, pathLabel).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).
									SingleLine().Ellipsis("…")
							})
						})
						if row.Clicked() {
							st.applyLink(cand.RelPath, cb)
							if st.View == ViewMap {
								st.CanvasHandle.Focus()
							}
						}
					}
				})
			})
		}

		// Footer action bar: [Remove] [Open] ... [Cancel] [Apply]
		btnRow := ui.Row(c.Key("mm-link-btns-"+id)).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space6)
		btnRow.Children(func() {
			leftBtns := ui.Row(c.Key("mm-link-lbtns-" + id)).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0)
			leftBtns.Children(func() {
				if strings.TrimSpace(targetNode.Link) != "" {
					remBtn := ui.ButtonBase(c.Key("mm-link-rem-"+id)).Height(theme.HeightCompact).PaddingX(theme.Space8).
						Radius(theme.RadiusMD).Center().Cursor(ui.CursorPointer).
						Label(theme.Tr("移除", "Remove"))
					if remBtn.Hovered() || remBtn.Pressed() {
						remBtn.Background(pal.DangerSurface)
					}
					remBtn.Children(func() {
						ui.Text(c, theme.Tr("移除", "Remove")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
							TextColor(pal.MenuDangerFG).SingleLine()
					})
					if remBtn.Clicked() {
						st.applyLink("", cb)
						if st.View == ViewMap {
							st.CanvasHandle.Focus()
						}
					}
				}
				draftTrim := strings.TrimSpace(st.LinkDraft)
				if draftTrim != "" && st.resolveLinkStatusCached(c.Now(), draftTrim, cb) != LinkStatusBroken {
					openBtn := ui.ButtonBase(c.Key("mm-link-open-"+id)).Height(theme.HeightCompact).PaddingX(theme.Space8).
						Radius(theme.RadiusMD).Border(1, pal.BtnBorder).Background(pal.Paper).Center().Cursor(ui.CursorPointer).
						Label(theme.Tr("打开", "Open"))
					if openBtn.Hovered() || openBtn.Pressed() {
						openBtn.Background(pal.Hover)
					}
					openBtn.Children(func() {
						ui.Text(c, theme.Tr("打开", "Open")).FontSize(theme.FontSmall).TextColor(pal.Accent).SingleLine()
					})
					if openBtn.Clicked() {
						st.applyLink(draftTrim, cb)
						st.openNodeLink(c.Now(), targetNode, cb)
					}
				}
			})

			rightBtns := ui.Row(c.Key("mm-link-rbtns-" + id)).AlignItems(ui.Center).Gap(theme.Space6).Shrink(0)
			rightBtns.Children(func() {
				cancel := ui.ButtonBase(c.Key("mm-link-cancel-"+id)).Height(theme.HeightCompact).PaddingX(theme.Space10).
					Radius(theme.RadiusMD).Border(1, pal.BtnBorder).Background(pal.Paper).Center().Cursor(ui.CursorPointer).
					Label(theme.Tr("取消", "Cancel"))
				if cancel.Hovered() || cancel.Pressed() {
					cancel.Background(pal.Hover)
				}
				cancel.Children(func() {
					ui.Text(c, theme.Tr("取消", "Cancel")).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
				})
				if cancel.Clicked() {
					st.LinkNodeID = 0
					st.LinkBrokenNotice = false
					if st.View == ViewMap {
						st.CanvasHandle.Focus()
					}
				}

				okBtn := ui.ButtonBase(c.Key("mm-link-ok-"+id)).Height(theme.HeightCompact).PaddingX(theme.Space12).
					Radius(theme.RadiusMD).Background(pal.Accent).Center().Cursor(ui.CursorPointer).
					Label(theme.Tr("确定", "Apply"))
				if okBtn.Hovered() {
					okBtn.Background(pal.AccentHover)
				}
				okBtn.Children(func() {
					ui.Text(c, theme.Tr("确定", "Apply")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(pal.Paper).SingleLine()
				})
				if okBtn.Clicked() {
					if st.LinkActiveIdx >= 0 && st.LinkActiveIdx < len(filtered) {
						st.applyLink(filtered[st.LinkActiveIdx].RelPath, cb)
					} else {
						st.applyLink(st.LinkDraft, cb)
					}
					if st.View == ViewMap {
						st.CanvasHandle.Focus()
					}
				}
			})
		})
	})
}

// --- Grouped Shortcut Cheat-Sheet Popover ---

func (st *BlockState) renderShortcutsPopover(c *ui.Context, id string) {
	pal := theme.Ocean()
	scrim := ui.Box(c.Key("mm-sc-scrim-"+id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(ui.Transparent)
	if scrim.Clicked() {
		st.ShortcutsOpen = false
		return
	}

	k := func(mac, other string) string {
		if theme.IsMacOS() {
			return mac
		}
		return other
	}
	type scEntry struct {
		keys  []string
		label string
	}
	type scGroup struct {
		title string
		items []scEntry
	}
	groups := []scGroup{
		{
			title: theme.Tr("视图", "Views"),
			items: []scEntry{
				{[]string{k("⌘1", "Ctrl+1")}, theme.Tr("导图", "Map")},
				{[]string{k("⌘2", "Ctrl+2")}, theme.Tr("大纲", "Outline")},
				{[]string{k("⌘3", "Ctrl+3")}, theme.Tr("源码", "Source")},
				{[]string{k("⌘⇧↵", "Ctrl+Shift+↵")}, theme.Tr("进入 / 退出全屏", "Toggle fullscreen")},
				{[]string{k("⌘⇧L", "Ctrl+Shift+L")}, theme.Tr("切换左右布局", "Toggle layout")},
			},
		},
		{
			title: theme.Tr("建树与编辑", "Tree & Editing"),
			items: []scEntry{
				{[]string{"Tab"}, theme.Tr("添加子节点", "Add child")},
				{[]string{"↵"}, theme.Tr("下方同级节点", "Add sibling below")},
				{[]string{k("⇧↵", "Shift+↵")}, theme.Tr("上方同级节点", "Add sibling above")},
				{[]string{k("⇧Tab", "Shift+Tab")}, theme.Tr("插入父节点", "Insert parent")},
				{[]string{"F2"}, theme.Tr("重命名节点", "Rename node")},
				{[]string{"⌫"}, theme.Tr("删除分支", "Delete branch")},
				{[]string{k("⌥⌫", "Alt+Del")}, theme.Tr("仅删当前节点", "Delete single node")},
			},
		},
		{
			title: theme.Tr("层级与移动", "Structure & Move"),
			items: []scEntry{
				{[]string{k("⌥↑↓", "Alt+↑↓")}, theme.Tr("同级上下移动", "Move up / down")},
				{[]string{k("⌥←→", "Alt+←→")}, theme.Tr("升级 / 降级节点", "Promote / demote")},
				{[]string{k("⌘D", "Ctrl+D")}, theme.Tr("创建分支副本", "Duplicate branch")},
				{[]string{k("⌘Z", "Ctrl+Z")}, theme.Tr("撤销", "Undo")},
				{[]string{k("⌘⇧Z", "Ctrl+Y")}, theme.Tr("重做", "Redo")},
			},
		},
		{
			title: theme.Tr("任务、链接与画布", "Tasks, Links & Canvas"),
			items: []scEntry{
				{[]string{k("⌘⌥T", "Ctrl+Alt+T")}, theme.Tr("设为 / 取消任务", "Toggle task")},
				{[]string{k("⌘↵", "Ctrl+↵")}, theme.Tr("切换任务完成", "Toggle done")},
				{[]string{k("⌘L", "Ctrl+L")}, theme.Tr("编辑节点链接", "Edit link")},
				{[]string{"Space"}, theme.Tr("折叠 / 展开分支", "Fold / unfold")},
				{[]string{k("⌘⌥/", "Ctrl+Alt+/")}, theme.Tr("全部折叠 / 展开", "Fold / unfold all")},
				{[]string{k("⌘F", "Ctrl+F")}, theme.Tr("画布内查找", "Find in map")},
				{[]string{k("⌘±", "Ctrl+±")}, theme.Tr("缩放画布", "Zoom in / out")},
				{[]string{k("⌘0", "Ctrl+0")}, theme.Tr("适配画布", "Fit to view")},
			},
		},
	}

	panel := ui.Column(c.Key("mm-sc-pop-"+id)).Absolute().Right(theme.Space10).Bottom(theme.Space8).
		Width(320).MaxHeight(300).Padding(theme.Space12).Gap(theme.Space8).
		Radius(theme.RadiusLG).Background(pal.Menu).Border(1, pal.MenuBorder)
	bui.Elevate(panel, theme.ElevationPopover)
	panel.Children(func() {
		hdr := ui.Row(c.Key("mm-sc-hdr-" + id)).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Shrink(0)
		hdr.Children(func() {
			ui.Text(c, theme.Tr("快捷键速查", "Keyboard Shortcuts")).
				FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()
			closeBtn := bui.IconButton(c, icons.Close, bui.IconButtonOptions{
				ID:       "mm-sc-close-" + id,
				Label:    theme.Tr("关闭", "Close"),
				Size:     theme.HeightMicro + theme.Space2,
				IconSize: theme.IconMicro,
				Radius:   theme.RadiusSM,
			})
			if closeBtn.Clicked() {
				st.ShortcutsOpen = false
			}
		})

		scr := bui.Scroll(c.Key("mm-sc-scroll-"+id), &st.ShortcutsScroll).FillWidth().Grow(1).MinHeight(0)
		scr.Children(func() {
			list := ui.Column(c).FillWidth().Gap(theme.Space10)
			list.Children(func() {
				for _, g := range groups {
					grp := ui.Column(c).FillWidth().Gap(theme.Space4)
					grp.Children(func() {
						ui.Text(c, g.title).FontSize(theme.FontMeta).FontWeight(theme.WeightTitle).
							TextColor(pal.TextSecondary).SingleLine()
						for _, it := range g.items {
							row := ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8)
							row.Children(func() {
								ui.Text(c, it.label).FontSize(theme.FontSmall).TextColor(pal.Text).
									SingleLine().Shrink(1).MinWidth(0)
								keysRow := ui.Row(c).AlignItems(ui.Center).Gap(theme.Space2).Shrink(0)
								keysRow.Children(func() {
									for _, kk := range it.keys {
										bui.Keycap(c, kk)
									}
								})
							})
						}
					})
				}
			})
		})
	})
}

// --- Import Result Card (with one-click Undo Import) ---

func (st *BlockState) renderImportResultCard(c *ui.Context, id string, cb Callbacks) {
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

		btnRow := ui.Row(c.Key("mm-imp-btns-"+id)).FillWidth().Margin(theme.Space6, 0, 0, 0).Gap(theme.Space6)
		btnRow.Children(func() {
			if st.canUndoImport(cb) {
				undoBtn := ui.ButtonBase(c.Key("mm-imp-undo-"+id)).Grow(1).Shrink(1).MinWidth(0).Height(theme.HeightCompact).
					Radius(theme.RadiusMD).Border(1, pal.BtnBorder).Background(pal.Paper).Center().Cursor(ui.CursorPointer).
					Label(theme.Tr("撤销导入", "Undo Import"))
				if undoBtn.Hovered() || undoBtn.Pressed() {
					undoBtn.Background(pal.Hover)
				}
				undoBtn.Children(func() {
					ui.Text(c, theme.Tr("撤销导入", "Undo Import")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
						TextColor(pal.TextSecondary).SingleLine()
				})
				if undoBtn.Clicked() {
					st.opUndoImport(cb)
					c.Invalidate()
				}
			}

			okBtn := ui.ButtonBase(c.Key("mm-imp-ok-"+id)).Grow(1).Shrink(1).MinWidth(0).Height(theme.HeightCompact).
				Radius(theme.RadiusMD).Border(1, pal.BtnBorder).Background(pal.Paper).Center().Cursor(ui.CursorPointer).
				Label(theme.Tr("知道了", "Got it"))
			if okBtn.Hovered() || okBtn.Pressed() {
				okBtn.Background(pal.Hover)
			}
			okBtn.Children(func() {
				ui.Text(c, theme.Tr("知道了", "Got it")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
					TextColor(pal.Text).SingleLine()
			})
			if okBtn.Clicked() {
				st.ImportResult = nil
				st.ImportUndoBefore = ""
				st.ImportUndoAfter = ""
			}
		})
	})
}

// --- Bottom Shortcut Hint Bar (Single-Row Horizontal Scroll + Direct Mouse Wheel Scroll) ---

func (st *BlockState) renderHintBar(c *ui.Context, id string) {
	pal := theme.Ocean()
	bar := ui.Row(c.Key("mm-hint-bar-" + id)).FillWidth().Height(theme.HeightControl).Shrink(0).
		Background(pal.HintBand).AlignItems(ui.Center).Clip()

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
		strip := ui.ScrollHorizontal(c.Key("mm-hint-scroll-" + id)).
			Grow(1).Shrink(1).MinWidth(0).FillHeight().
			AlignItems(ui.Center).PaddingX(theme.Space10).Gap(theme.Space10).
			Scrollbars(false).TrackScroll(&st.HintScroll)
		strip.HandleInput(handleHintWheel)
		strip.Children(func() {
			type hintItem struct {
				keys  []string
				label string
			}
			k := func(mac, other string) string {
				if theme.IsMacOS() {
					return mac
				}
				return other
			}
			var items []hintItem
			switch {
			case st.View == ViewOutline:
				items = []hintItem{
					{[]string{"↵"}, theme.Tr("新行", "New")},
					{[]string{k("⇧↵", "Shift+↵")}, theme.Tr("换行", "Break")},
					{[]string{"Tab", k("⇧Tab", "Shift+Tab")}, theme.Tr("缩进", "Indent")},
					{[]string{k("⌥↑↓", "Alt+↑↓")}, theme.Tr("移动", "Move")},
					{[]string{k("⌘↵", "Ctrl+↵")}, theme.Tr("待办", "Todo")},
					{[]string{k("⌘.", "Ctrl+.")}, theme.Tr("折叠", "Fold")},
				}
				if st.OutlineZoomID != 0 {
					items = append(items, hintItem{[]string{"Esc"}, theme.Tr("返回上级", "Zoom out")})
				} else {
					items = append(items, hintItem{nil, theme.Tr("· 点圆点聚焦", "· Click dot to zoom")})
				}
			case st.EditingID != 0:
				items = []hintItem{
					{[]string{"↵"}, theme.Tr("完成", "Done")},
					{[]string{k("⇧↵", "Shift+↵")}, theme.Tr("换行", "Newline")},
					{[]string{"Tab"}, theme.Tr("子节点", "Child")},
					{[]string{k("⇧Tab", "Shift+Tab")}, theme.Tr("父节点", "Parent")},
					{[]string{"Esc"}, theme.Tr("取消", "Cancel")},
				}
			default:
				items = []hintItem{
					{[]string{"Tab"}, theme.Tr("子节点", "Child")},
					{[]string{k("⇧Tab", "Shift+Tab")}, theme.Tr("父节点", "Parent")},
					{[]string{"↵"}, theme.Tr("同级", "Sibling")},
					{[]string{k("⌥↑↓←→", "Alt+↑↓←→")}, theme.Tr("移动", "Move")},
					{[]string{k("⌘↵", "Ctrl+↵")}, theme.Tr("任务", "Task")},
					{[]string{"Space"}, theme.Tr("折叠", "Fold")},
					{[]string{"F2"}, theme.Tr("改名", "Rename")},
					{[]string{k("⌘L", "Ctrl+L")}, theme.Tr("链接", "Link")},
				}
			}
			for _, it := range items {
				itemRow := ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0)
				itemRow.Children(func() {
					for _, key := range it.keys {
						bui.Keycap(c, key)
					}
					ui.Text(c, it.label).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
				})
			}
		})

		// Pinned right-side ? Shortcuts button opening the grouped shortcut cheat-sheet.
		ui.Box(c).Size(1, theme.Space12).Background(pal.Hairline).Shrink(0)
		scBtn := ui.ButtonBase(c.Key("mm-sc-btn-"+id)).
			Height(theme.HeightControl).PaddingX(theme.Space8).AlignItems(ui.Center).Gap(theme.Space4).
			Cursor(ui.CursorPointer).Shrink(0).Label(theme.Tr("快捷键", "Shortcuts"))
		scHot := scBtn.Hovered() || scBtn.Pressed()
		scFG := pal.TextSecondary
		if st.ShortcutsOpen {
			scBtn.Background(pal.SelectionBG)
			scFG = pal.Accent
		} else if scHot {
			scBtn.Background(pal.Hover)
			scFG = pal.Text
		}
		scBtn.Children(func() {
			icons.DrawAnimated(c, icons.HelpCircle, theme.IconMicro, pal.MonoIconColors(scFG), st.ShortcutsOpen || scHot)
			ui.Text(c, theme.Tr("快捷键", "Keys")).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).
				TextColor(scFG).SingleLine()
		})
		if scBtn.Clicked() {
			st.ShortcutsOpen = !st.ShortcutsOpen
			if st.ShortcutsOpen {
				st.ExportMenuOpen = false
				st.LinkNodeID = 0
				st.ImportResult = nil
			}
		}
	})

	// Smooth stepped edge fades when overflowing horizontally on ultra-narrow windows.
	bar.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Line(r.X, r.Y+0.5, r.X+r.W, r.Y+0.5, 1, pal.Hairline)
		if st.HintScroll.MaxX <= 1 {
			return
		}
		fadeAlphas := [3]float32{0.92, 0.58, 0.25}
		const bandW float32 = 7
		if st.HintScroll.X > 1 {
			for i, a := range fadeAlphas {
				p.Fill(ui.Rect{X: r.X + float32(i)*bandW, Y: r.Y + 1, W: bandW, H: r.H - 1}, pal.HintBand.Alpha(a), 0)
			}
		}
		if st.HintScroll.X < st.HintScroll.MaxX-1 {
			for i, a := range fadeAlphas {
				x := r.X + r.W - float32(i+1)*bandW
				p.Fill(ui.Rect{X: x, Y: r.Y + 1, W: bandW, H: r.H - 1}, pal.HintBand.Alpha(a), 0)
			}
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
