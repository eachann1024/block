package mindmap

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
	"unicode"

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

	LinkNodeID       int
	LinkDraft        string
	LinkFocusPending bool
	LinkHandle       ui.Handle

	ExportMenuOpen bool
	ImportResult   *ImportReport

	Undo [][]byte
	Redo [][]byte

	OutlineZoomID       int
	OutlineFocusID      int
	OutlineFocusPending bool
	OutlineCaretPending bool
	OutlineCaretTarget  int
	OutlineSelNodeID    int
	OutlineSelStart     int
	OutlineSelEnd       int
	OutlineSelRuneLen   int
	OutlineUndoNodeID   int
	outlinePending      *outlineAction

	SourceCopiedUntil time.Time

	CanvasHandle  ui.Handle
	OutlineHandle ui.Handle
	SourceHandle  ui.Handle
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
	st.OutlineUndoNodeID = 0
	if st.Root != nil && st.Root.FindByID(st.SelectedID) == nil {
		st.SelectedID = st.Root.ID
	}
	if st.OutlineZoomID != 0 && (st.Root == nil || st.Root.FindByID(st.OutlineZoomID) == nil) {
		st.OutlineZoomID = 0
	}
	st.AutoFitPending = true
}

// HasFocus reports whether any interactive MindMap control currently owns keyboard focus.
func (st *BlockState) HasFocus(c *ui.Context) bool {
	if st == nil || c == nil {
		return false
	}
	return st.CanvasHandle.Focused(c) ||
		st.EditHandle.Focused(c) ||
		st.LinkHandle.Focused(c) ||
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
	if focused || st.HasFocus(c) || st.EditingID != 0 {
		borderCol = pal.Accent
	}

	card := ui.Column(c.Key("mm-card-"+id)).FillWidth().Radius(theme.RadiusLG).
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
		if cfg.ShowHints && st.View != ViewSource {
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

	if st.EditingID == 0 && st.LinkNodeID == 0 && !st.OutlineHandle.FocusWithin(c) && c.Shortcut(0, ui.KeyEscape) {
		if st.DragActive || st.DragNodeID != 0 {
			st.resetDrag()
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
			st.Fullscreen = false
			st.AutoFitPending = true
			c.Invalidate()
			return
		}
	}
	if st.View == ViewOutline && st.EditingID == 0 && st.LinkNodeID == 0 && !st.OutlineHandle.FocusWithin(c) {
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
		// Left: Neutral-ring segmented view switcher (导图 / 大纲 / 源码) so it never competes with card focus Accent.
		seg := ui.Row(c.Key("mm-seg-" + id)).AlignItems(ui.Center).Gap(theme.Space2).Shrink(0)
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
				btn.Children(func() {
					icons.DrawAnimated(c, tab.icon, theme.IconInline, pal.DuoIconColors(fg), active || hov || prs)
					ui.Text(c, tab.label).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
				})
				if btn.Clicked() {
					st.finishEdit(true, cb)
					st.LinkNodeID = 0
					st.ExportMenuOpen = false
					st.OutlineUndoNodeID = 0
					if st.View != tab.mode {
						st.HintScroll.X = 0
					}
					st.View = tab.mode
					if tab.mode == ViewMap {
						st.AutoFitPending = true
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

			switch st.View {
			case ViewMap:
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
					if st.Layout == LayoutRight {
						st.Layout = LayoutBoth
					} else {
						st.Layout = LayoutRight
					}
					st.LayoutSet = true
					st.AutoFitPending = true
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
			st.ComposeText = ""
			st.lastKeyMod = 0
			st.resetDrag()
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
			if ev.Key == ui.KeyEscape && (st.DragActive || st.DragNodeID != 0) {
				st.resetDrag()
				return true
			}
			return st.handleCanvasKey(c, ev.Key, ev.Mods, cb)
		}
		return false
	})

	canvasHovered := canvas.Hovered()
	if !canvasHovered && !st.Panning && st.DragNodeID == 0 && st.HoveredID != 0 {
		st.HoveredID = 0
	}

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
		if !st.CameraInit || st.AutoFitPending {
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
		if !canvasHovered {
			hoveredID = 0
		}
		opt := PaintOptions{
			SelectedID: st.SelectedID,
			HoveredID:  hoveredID,
			EditingID:  paintEditID,
			Palette:    pal,
			Background: pal.CodeBG,
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
	edX := sx + insetX
	edW := max(sw-insetX*2, 24)
	align := ui.Center
	if n.Task != nil {
		textLeft := nodeContentLeft(n) + layoutTaskBox + layoutTaskGap
		offX := (textLeft - n.X) * scale
		edX = sx + offX
		edW = max(sw-offX-insetX, 24)
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
	st.AutoFitPending = true
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
	if mods == ui.Cmd|ui.Shift || mods == ui.Ctrl|ui.Shift {
		switch key {
		case ui.KeyZ:
			st.opRedo(cb)
			return true
		case ui.KeyEqual:
			st.zoomAt(st.LastCanvasW*0.5, st.LastCanvasH*0.5, st.Camera.Scale*1.15)
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
	ta := ui.TextAreaBase(c.Key("mm-src-"+id), &draft).Fill().
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
	bui.AutoHideScrollbars(c, ta, &st.SourceScroll)
	if !cb.ReadOnly && (ta.Changed() || draft != prev) {
		st.SyncSource(draft, Config{})
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
