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
	OnSaveExportFile  func(defaultName, ext string, data []byte, onDone func(savedPath string, err error))
	OnNotify          func(msg, tone string)
	OnRequestFocus    func()
	OnHostNavKey      func(key ui.Key, mods ui.Modifiers) bool
	OnTutorialSeen    func()
	ReadOnly          bool
}

// BlockState keeps all interactive runtime state for one mindmap block.
type BlockState struct {
	Root        *Node
	NextID      int
	LastSource  string
	SelectedID  int
	SelectedIDs map[int]bool
	HoveredID   int
	HoveredHit  HitKind
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

	Panning         bool
	PanLastX        float32
	PanLastY        float32
	SpaceHeld       bool
	SpaceUsedForPan bool
	panButton       int
	panStartX       float32
	panStartY       float32

	BoxSelecting bool
	BoxAdditive  bool
	BoxStartX    float32
	BoxStartY    float32
	BoxCurrX     float32
	BoxCurrY     float32

	DragNodeID   int
	DragActive   bool
	DragStartX   float32
	DragStartY   float32
	DragCurrX    float32
	DragCurrY    float32
	DragTargetID int
	DragPos      DragDropPos
	// releaselessMoves counts consecutive button-up moves during a gesture; one can
	// precede a normal release, so only a second one means the release was lost.
	releaselessMoves int

	lastKeyMod        ui.Modifiers
	ComposeText       string
	LastCanvasFocused bool
	lastHasFocus      bool

	EditingID          int
	EditSeq            int
	EditDraft          string
	EditCompose        string
	editLastLayoutText string
	EditFresh                bool
	EditSelectAll            bool
	EditFocusPending         bool
	EditInsertNewlinePending bool
	EditHandle               ui.Handle

	linkStatusCache   map[string]LinkStatus
	linkStatusChecked time.Time

	LinkNodeID       int
	LinkDraft        string
	LinkFocusPending bool
	LinkComposing    bool
	LinkActiveIdx    int
	LinkBrokenNotice bool
	LinkHandle       ui.Handle
	LinkPanelHandle  ui.Handle
	LinkListScroll   ui.ScrollState

	FindOpen         bool
	FindQuery        string
	FindFocusPending bool
	FindComposing    bool
	FindMatchIDs     []int
	FindMatchSet     map[int]bool
	FindCurrentIdx   int
	FindHandle       ui.Handle
	FindBarHandle    ui.Handle

	ExportMenuOpen   bool
	ExportMenuScroll ui.ScrollState
	ExportTheme      ExportTheme
	ShortcutsOpen    bool
	ShortcutsScroll  ui.ScrollState
	TutorialOpen     bool
	TutorialFirstRun bool
	tutorial         *tutorialPlayerState
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
	OutlineComposing          bool
	OutlineInsertNewlineID    int
	outlinePending            *outlineAction

	SourceComposing   bool
	SourceCopiedUntil time.Time

	// Block-scoped chords (⌘1/2/3 views, ⌘⇧↵ fullscreen, ⌘⇧/ help) captured inside text
	// fields are queued here and applied by handleBlockShortcuts next frame.
	pendingView              ViewMode
	pendingFSToggle          bool
	pendingShortcuts         bool
	viewFocusPending         bool
	viewFocusPreventScroll   bool
	tabFocusClaimedThisFrame bool

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
	if st.Root != nil && st.SelectedID != 0 && st.Root.FindByID(st.SelectedID) == nil {
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
	st.LinkComposing = false
	st.LinkBrokenNotice = false
	st.FindComposing = false
	st.EditingID = 0
	st.EditFocusPending = false
	st.EditInsertNewlinePending = false
	st.EditSelectAll = false
	st.EditCompose = ""
	st.editLastLayoutText = ""
	st.ComposeText = ""
	st.OutlineInsertNewlineID = 0
	st.SourceComposing = false
	st.Measure.ClearTextOverride()
	st.clearMultiSelect()
	st.Panning = false
	st.SpaceHeld = false
	st.SpaceUsedForPan = false
	st.lastKeyMod = 0
	st.resetBoxSelect()
	st.resetDrag()
	st.HoveredID = 0
	st.HoveredHit = HitNone
}

// HasFocus reports whether any interactive MindMap control currently owns keyboard focus.
func (st *BlockState) HasFocus(c *ui.Context) bool {
	if st == nil || c == nil {
		return false
	}
	if st.Fullscreen || st.TutorialOpen || st.ShortcutsOpen || st.ExportMenuOpen || st.ImportResult != nil {
		return true
	}
	if st.LinkNodeID != 0 || st.LinkFocusPending || st.LinkComposing || st.LinkHandle.Focused(c) || st.LinkPanelHandle.FocusWithin(c) {
		return true
	}
	if st.viewFocusPending {
		return true
	}
	switch st.View {
	case ViewOutline:
		return st.OutlineFocusPending || st.OutlineComposing || st.OutlineHandle.FocusWithin(c)
	case ViewSource:
		return st.SourceComposing || st.SourceHandle.Focused(c)
	default:
		return st.EditingID != 0 ||
			st.EditFocusPending ||
			st.EditCompose != "" ||
			st.FindFocusPending ||
			st.FindComposing ||
			st.ComposeText != "" ||
			st.CanvasHandle.Focused(c) ||
			st.EditHandle.Focused(c) ||
			st.FindHandle.Focused(c) ||
			(st.FindOpen && st.FindBarHandle.FocusWithin(c))
	}
}

func (st *BlockState) canvasFocusedOrPending(c *ui.Context) bool {
	return st.CanvasHandle.Focused(c) ||
		(st.viewFocusPending && !st.EditFocusPending && !st.LinkFocusPending && !st.FindFocusPending)
}

// HasEscapeOwner reports whether this MindMap currently has an active cancelable edit,
// IME preedit, popover, find bar, drag/box-select gesture, multi-selection, outline zoom,
// or fullscreen overlay that must receive Escape before outer window/drawer shortcuts intercept it.
func (st *BlockState) HasEscapeOwner(c *ui.Context) bool {
	if st == nil {
		return false
	}
	if st.Fullscreen ||
		st.TutorialOpen ||
		st.ShortcutsOpen ||
		st.ExportMenuOpen ||
		st.ImportResult != nil ||
		st.LinkNodeID != 0 ||
		st.LinkFocusPending ||
		(c != nil && (st.LinkHandle.Focused(c) || st.LinkPanelHandle.FocusWithin(c))) {
		return true
	}
	switch st.View {
	case ViewMap:
		if st.EditingID != 0 ||
			st.EditFocusPending ||
			st.EditCompose != "" ||
			st.FindFocusPending ||
			st.FindComposing ||
			st.LinkComposing ||
			st.BoxSelecting ||
			st.Panning ||
			st.DragActive ||
			st.DragNodeID != 0 ||
			st.ComposeText != "" ||
			(c != nil && (st.EditHandle.Focused(c) || st.FindHandle.Focused(c))) {
			return true
		}
		if st.FindOpen {
			if c != nil {
				if st.HasFocus(c) || st.FindBarHandle.FocusWithin(c) {
					return true
				}
			} else if st.lastHasFocus || st.LastCanvasFocused {
				return true
			}
		}
		if st.hasMultiSelect() {
			if c != nil {
				return st.canvasFocusedOrPending(c)
			}
			return st.LastCanvasFocused || st.viewFocusPending
		}
		return false
	case ViewOutline:
		if st.OutlineComposing {
			return c == nil || st.OutlineHandle.FocusWithin(c)
		}
		if st.OutlineZoomID != 0 {
			if c != nil {
				return st.OutlineHandle.FocusWithin(c) || st.OutlineFocusPending
			}
			return st.lastHasFocus
		}
		return false
	case ViewSource:
		return st.SourceComposing && (c == nil || st.SourceHandle.Focused(c))
	default:
		return false
	}
}

// HasZoomOwner reports whether host zoom shortcuts (Cmd/Ctrl+0/=/−) should target this Map.
func (st *BlockState) HasZoomOwner(c *ui.Context) bool {
	if st == nil || st.View != ViewMap {
		return false
	}
	if st.Fullscreen {
		return true
	}
	if c != nil {
		return st.HasFocus(c)
	}
	return st.LastCanvasFocused || st.lastHasFocus
}

// HasFindOwner reports whether host Cmd/Ctrl+F should open this MindMap's Find bar.
func (st *BlockState) HasFindOwner(c *ui.Context) bool {
	if st == nil {
		return false
	}
	if st.Fullscreen {
		return true
	}
	if c != nil {
		return st.HasFocus(c)
	}
	return st.LastCanvasFocused || st.lastHasFocus
}

// HasFindActive reports whether this MindMap has its in-canvas Find bar open and
// currently owns keyboard focus (or fullscreen), so host Cmd+G / F3 should yield to it.
func (st *BlockState) HasFindActive(c *ui.Context) bool {
	if st == nil || st.View != ViewMap || !st.FindOpen {
		return false
	}
	if st.Fullscreen {
		return true
	}
	if c != nil {
		return st.HasFocus(c) || st.FindBarHandle.FocusWithin(c)
	}
	return st.LastCanvasFocused || st.lastHasFocus
}

// HasCanvasFocus reports whether the Map canvas itself (not a text field) owns focus.
func (st *BlockState) HasCanvasFocus(c *ui.Context) bool {
	return st != nil && c != nil && st.View == ViewMap && st.canvasFocusedOrPending(c) &&
		st.EditingID == 0 && st.LinkNodeID == 0 && !st.FindHandle.Focused(c) &&
		!st.FindFocusPending && !(st.FindOpen && st.FindBarHandle.FocusWithin(c))
}

// HasTabFocus reports whether any focused MindMap control should handle Tab / Shift+Tab
// (or use framework focus traversal inside Find / Link / Source) instead of the outer
// editor swallowing Tab.
func (st *BlockState) HasTabFocus(c *ui.Context) bool {
	if st == nil || c == nil {
		return false
	}
	return st.tabFocusClaimedThisFrame || st.HasFocus(c)
}

// CapturesShortcut reports whether this MindMap currently owns keyboard focus and
// handles the given shortcut chord in its active view/state, preventing host plugin
// commands from hijacking active MindMap shortcuts.
func (st *BlockState) CapturesShortcut(c *ui.Context, mods ui.Modifiers, key ui.Key) bool {
	if st == nil || c == nil || (!st.Fullscreen && !st.HasFocus(c)) {
		return false
	}
	// Block-scoped chords active across all MindMap views.
	if isMMPrimaryMod(mods) {
		if _, ok := viewForDigitKey(key); ok {
			return true
		}
	}
	if isMMPrimaryShiftMod(mods) && (key == ui.KeyEnter || key == ui.KeySlash) {
		return true
	}
	if theme.IsMacOS() && mods == 0 && key == ui.KeyF1 {
		return true
	}

	switch st.View {
	case ViewOutline:
		if !st.OutlineHandle.FocusWithin(c) && !st.OutlineFocusPending && !st.viewFocusPending {
			return false
		}
		if isMMPrimaryMod(mods) {
			switch key {
			case ui.KeyZ, ui.KeyY, ui.KeyPeriod, ui.KeySlash, ui.KeyD, ui.KeyL, ui.KeyEnter:
				return true
			}
		}
		if isMMPrimaryShiftMod(mods) && (key == ui.KeyZ || key == ui.KeyPeriod) {
			return true
		}
		if isMMPrimaryAltMod(mods) && (key == ui.KeySlash || key == ui.KeyT) {
			return true
		}
		if mods == ui.Alt && (key == ui.KeyUp || key == ui.KeyDown || key == ui.KeyEnter) {
			return true
		}
		if key == ui.KeyTab && (mods == 0 || mods == ui.Shift) {
			return true
		}
		if mods == 0 && (key == ui.KeyEnter || key == ui.KeyEscape || key == ui.KeyBackspace || key == ui.KeyUp || key == ui.KeyDown) {
			return true
		}
		return false
	case ViewSource:
		return false
	default: // ViewMap
		if st.LinkNodeID != 0 {
			return (mods == 0 && (key == ui.KeyEnter || key == ui.KeyEscape || key == ui.KeyUp || key == ui.KeyDown)) ||
				(key == ui.KeyTab && (mods == 0 || mods == ui.Shift))
		}
		if st.EditingID != 0 {
			if key == ui.KeyEnter && (mods == 0 || mods == ui.Shift || mods == ui.Alt || isMMPrimaryMod(mods)) {
				return true
			}
			if key == ui.KeyTab && (mods == 0 || mods == ui.Shift) {
				return true
			}
			return mods == 0 && key == ui.KeyEscape
		}
		if st.FindOpen && (st.FindHandle.Focused(c) || st.FindFocusPending || st.FindBarHandle.FocusWithin(c)) {
			if theme.IsMacOS() && (isMMPrimaryMod(mods) || isMMPrimaryShiftMod(mods)) && key == ui.KeyG {
				return true
			}
			if !theme.IsMacOS() && (mods == 0 || mods == ui.Shift) && key == ui.KeyF3 {
				return true
			}
			return key == ui.KeyTab && (mods == 0 || mods == ui.Shift)
		}
		if !st.canvasFocusedOrPending(c) {
			return false
		}
		if isMMPrimaryMod(mods) {
			switch key {
			case ui.KeyZ, ui.KeyY, ui.KeyA, ui.KeyC, ui.KeyX, ui.KeyV, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyL, ui.KeyEnter, ui.KeySlash, ui.KeyPeriod, ui.Key0, ui.KeyEqual, ui.KeyMinus:
				return true
			case ui.KeyG:
				return theme.IsMacOS() && st.FindOpen
			}
			return false
		}
		if isMMPrimaryShiftMod(mods) {
			switch key {
			case ui.KeyZ, ui.KeyA, ui.Key0, ui.KeyEqual, ui.KeyMinus, ui.KeyL, ui.KeyPeriod:
				return true
			case ui.KeyG:
				return theme.IsMacOS() && st.FindOpen
			}
			return false
		}
		if isMMPrimaryAltMod(mods) {
			switch key {
			case ui.KeyA, ui.Key0, ui.KeyT, ui.KeySlash:
				return true
			}
			return false
		}
		if mods == ui.Alt {
			switch key {
			case ui.KeyUp, ui.KeyDown, ui.KeyLeft, ui.KeyRight, ui.KeyBackspace, ui.KeyDelete, ui.KeyEnter:
				return true
			}
			return false
		}
		if mods == ui.Shift {
			switch key {
			case ui.KeyEnter, ui.KeyTab, ui.KeyBackspace, ui.KeyDelete, ui.KeyUp, ui.KeyDown:
				return true
			case ui.KeyF3:
				return !theme.IsMacOS() && st.FindOpen
			}
			return false
		}
		if mods == 0 {
			switch key {
			case ui.KeyTab, ui.KeyInsert, ui.KeyEnter, ui.KeyBackspace, ui.KeyDelete, ui.KeySpace, ui.KeyF2, ui.KeyHome, ui.KeyEnd, ui.KeyPageUp, ui.KeyPageDown, ui.KeyUp, ui.KeyDown, ui.KeyLeft, ui.KeyRight:
				return true
			case ui.KeyF3:
				return !theme.IsMacOS() && st.FindOpen
			}
		}
		return false
	}
}

// DismissTransient closes the topmost transient edit, popup, find bar, selection,
// or zoom inside this MindMap block and reports whether anything was dismissed.
func (st *BlockState) DismissTransient(cb Callbacks) bool {
	if st == nil {
		return false
	}
	if st.TutorialOpen {
		st.CloseTutorial(cb)
		return true
	}
	if st.ImportResult != nil {
		st.ImportResult = nil
		st.ImportUndoBefore = ""
		st.ImportUndoAfter = ""
		return true
	}
	if st.ShortcutsOpen {
		st.ShortcutsOpen = false
		return true
	}
	if st.LinkNodeID != 0 {
		st.LinkNodeID = 0
		st.LinkBrokenNotice = false
		st.LinkFocusPending = false
		st.LinkHandle.CancelFocus()
		st.restoreCanvasFocusAfterEdit(cb)
		return true
	}
	if st.ExportMenuOpen {
		st.ExportMenuOpen = false
		return true
	}
	if st.EditingID != 0 {
		st.finishEdit(false, cb)
		st.restoreCanvasFocusAfterEdit(cb)
		return true
	}
	if st.ComposeText != "" {
		st.ComposeText = ""
		return true
	}
	if st.BoxSelecting {
		st.resetBoxSelect()
		return true
	}
	if st.Panning {
		st.Panning = false
		return true
	}
	if st.DragActive || st.DragNodeID != 0 {
		st.resetDrag()
		return true
	}
	if st.View == ViewMap && st.FindOpen {
		st.CloseFind()
		st.restoreCanvasFocusAfterEdit(cb)
		return true
	}
	if st.View == ViewMap && st.hasMultiSelect() {
		st.clearMultiSelect()
		return true
	}
	if st.View == ViewOutline && st.OutlineZoomID != 0 {
		if cur := st.outlineActiveRoot(); cur != nil && cur != st.Root && cur.Parent != nil && cur.Parent != st.Root {
			st.OutlineZoomID = cur.Parent.ID
		} else {
			st.OutlineZoomID = 0
		}
		return true
	}
	return false
}

func mmShortcutLabel(mac, other string) string {
	if theme.IsMacOS() {
		return mac
	}
	return other
}

func isMMPrimaryMod(mods ui.Modifiers) bool {
	if theme.IsMacOS() {
		return mods == ui.Cmd
	}
	return mods == ui.Ctrl || mods == ui.Cmd
}

func isMMPrimaryShiftMod(mods ui.Modifiers) bool {
	if theme.IsMacOS() {
		return mods == ui.Cmd|ui.Shift
	}
	return mods == ui.Ctrl|ui.Shift || mods == ui.Cmd|ui.Shift
}

func isMMPrimaryAltMod(mods ui.Modifiers) bool {
	if theme.IsMacOS() {
		return mods == ui.Cmd|ui.Alt
	}
	return mods == ui.Ctrl|ui.Alt || mods == ui.Cmd|ui.Alt
}

// RequestFocus schedules focus restoration into the active MindMap view on next render.
func (st *BlockState) RequestFocus(preventScroll bool) {
	if st == nil {
		return
	}
	st.viewFocusPending = true
	if preventScroll {
		st.viewFocusPreventScroll = true
	}
}

func (st *BlockState) consumeViewFocus() (focus bool, preventScroll bool) {
	if st == nil || !st.viewFocusPending {
		return false, false
	}
	focus = true
	preventScroll = st.viewFocusPreventScroll
	st.viewFocusPending = false
	st.viewFocusPreventScroll = false
	return focus, preventScroll
}

func (st *BlockState) cancelViewFocus() {
	if st == nil {
		return
	}
	st.viewFocusPending = false
	st.viewFocusPreventScroll = false
	st.CanvasHandle.CancelFocus()
}

// focusCanvas hands keyboard focus back to the active view (Map canvas,
// outline row, or source) on the next frame, always without scrolling the note.
func (st *BlockState) focusCanvas(c *ui.Context, cb Callbacks) {
	if st == nil {
		return
	}
	st.RequestFocus(true)
	if c != nil {
		c.Invalidate()
	}
	if cb.OnRequestFocus != nil {
		cb.OnRequestFocus()
	}
}

func (st *BlockState) restoreCanvasFocusAfterEdit(cb Callbacks) {
	if st == nil {
		return
	}
	st.RequestFocus(true)
	if cb.OnRequestFocus != nil {
		cb.OnRequestFocus()
	}
}

func (st *BlockState) keepCanvasButtonFocus(c *ui.Context, btn ui.Element) ui.Element {
	if st != nil && c != nil && st.EditingID == 0 && st.LinkNodeID == 0 && !st.FindHandle.Focused(c) {
		btn.KeepFocus()
	}
	return btn
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

// selectedNode returns the primary selection; nil when the selection is empty
// (SelectedID == 0, e.g. after clicking blank canvas).
func (st *BlockState) selectedNode() *Node {
	if st.Root == nil || st.SelectedID == 0 {
		return nil
	}
	if n := st.Root.FindByID(st.SelectedID); n != nil {
		return n
	}
	st.SelectedID = st.Root.ID
	return st.Root
}

func (st *BlockState) hasMultiSelect() bool {
	return len(st.SelectedIDs) > 1
}

func (st *BlockState) clearMultiSelect() {
	st.SelectedIDs = nil
}

func (st *BlockState) setSingleSelection(id int) {
	st.SelectedID = id
	st.SelectedIDs = nil
}

func (st *BlockState) resetBoxSelect() {
	st.BoxSelecting = false
	st.BoxAdditive = false
}

func (st *BlockState) boxSelectActive() bool {
	if !st.BoxSelecting {
		return false
	}
	return math.Hypot(float64(st.BoxCurrX-st.BoxStartX), float64(st.BoxCurrY-st.BoxStartY)) >= 4
}

// boxSelectRect is the marquee clipped to the visible canvas, shared by paint
// and hit-testing so off-screen nodes are never selected.
func (st *BlockState) boxSelectRect() ui.Rect {
	x0 := max(min(st.BoxStartX, st.BoxCurrX), 0)
	y0 := max(min(st.BoxStartY, st.BoxCurrY), 0)
	x1 := max(st.BoxStartX, st.BoxCurrX)
	y1 := max(st.BoxStartY, st.BoxCurrY)
	if st.LastCanvasW > 0 {
		x1 = min(x1, st.LastCanvasW)
	}
	if st.LastCanvasH > 0 {
		y1 = min(y1, st.LastCanvasH)
	}
	if x1 < x0 || y1 < y0 {
		return ui.Rect{X: x0, Y: y0}
	}
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func (st *BlockState) firstVisibleSelectedID(set map[int]bool) int {
	if st.Root == nil || len(set) == 0 {
		return 0
	}
	var found int
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil || found != 0 {
			return
		}
		if set[n.ID] {
			found = n.ID
			return
		}
		if n.Folded {
			return
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(st.Root)
	if found != 0 {
		return found
	}
	for id := range set {
		if st.Root.FindByID(id) != nil {
			return id
		}
	}
	return 0
}

func (st *BlockState) toggleMultiSelectNode(id int) {
	if st.Root == nil || id == 0 || st.Root.FindByID(id) == nil {
		return
	}
	if len(st.SelectedIDs) == 0 {
		if st.SelectedID == 0 || st.Root.FindByID(st.SelectedID) == nil {
			st.SelectedID = id
			return
		}
		if st.SelectedID == id {
			st.SelectedID = 0
			return
		}
		st.SelectedIDs = map[int]bool{
			st.SelectedID: true,
			id:            true,
		}
		st.SelectedID = id
		return
	}
	if st.SelectedIDs[id] {
		if len(st.SelectedIDs) <= 1 {
			st.setSingleSelection(id)
			return
		}
		delete(st.SelectedIDs, id)
		if len(st.SelectedIDs) == 1 {
			for rem := range st.SelectedIDs {
				st.SelectedID = rem
			}
			st.SelectedIDs = nil
			return
		}
		if st.SelectedID == id {
			if next := st.firstVisibleSelectedID(st.SelectedIDs); next != 0 {
				st.SelectedID = next
			}
		}
		return
	}
	if st.SelectedID != 0 && st.Root.FindByID(st.SelectedID) != nil {
		st.SelectedIDs[st.SelectedID] = true
	}
	st.SelectedIDs[id] = true
	st.SelectedID = id
}

func (st *BlockState) boxSelectResult(additive bool) (int, map[int]bool) {
	if st.Root == nil || st.Camera.Scale <= 0 {
		return st.SelectedID, st.SelectedIDs
	}
	sr := st.boxSelectRect()
	wx0, wy0 := st.Camera.ScreenToWorld(sr.X, sr.Y)
	wx1, wy1 := st.Camera.ScreenToWorld(sr.X+sr.W, sr.Y+sr.H)

	var hits []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.X+n.W >= wx0 && n.X <= wx1 && n.Y+n.H/2 >= wy0 && n.Y-n.H/2 <= wy1 {
			hits = append(hits, n)
		}
		if n.Folded {
			return
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(st.Root)

	if !additive {
		switch len(hits) {
		case 0:
			return 0, nil
		case 1:
			return hits[0].ID, nil
		default:
			set := make(map[int]bool, len(hits))
			for _, n := range hits {
				set[n.ID] = true
			}
			primary := st.SelectedID
			if !set[primary] {
				primary = hits[0].ID
			}
			return primary, set
		}
	}

	if len(hits) == 0 {
		return st.SelectedID, st.SelectedIDs
	}
	set := make(map[int]bool, len(st.SelectedIDs)+len(hits)+1)
	for id := range st.SelectedIDs {
		if st.Root.FindByID(id) != nil {
			set[id] = true
		}
	}
	if len(set) == 0 && st.SelectedID != 0 && st.Root.FindByID(st.SelectedID) != nil {
		set[st.SelectedID] = true
	}
	for _, n := range hits {
		set[n.ID] = true
	}
	if len(set) == 1 {
		for id := range set {
			return id, nil
		}
	}
	primary := st.SelectedID
	if primary == 0 || !set[primary] {
		primary = hits[0].ID
	}
	return primary, set
}

func (st *BlockState) applyBoxSelection(additive bool) {
	if st.Root == nil || st.Camera.Scale <= 0 {
		return
	}
	st.SelectedID, st.SelectedIDs = st.boxSelectResult(additive)
}

func (st *BlockState) pruneHiddenMultiSelect() {
	if st.Root == nil || len(st.SelectedIDs) == 0 {
		return
	}
	for id := range st.SelectedIDs {
		n := st.Root.FindByID(id)
		if n == nil || !isNodeVisibleInTree(n) {
			delete(st.SelectedIDs, id)
		}
	}
	switch len(st.SelectedIDs) {
	case 0:
		st.SelectedIDs = nil
	case 1:
		for id := range st.SelectedIDs {
			st.SelectedID = id
		}
		st.SelectedIDs = nil
	default:
		if !st.SelectedIDs[st.SelectedID] {
			if next := st.firstVisibleSelectedID(st.SelectedIDs); next != 0 {
				st.SelectedID = next
			}
		}
	}
}

func (st *BlockState) multiSelectDeletableCount() int {
	if st.Root == nil || len(st.SelectedIDs) == 0 {
		return 0
	}
	count := 0
	for id := range st.SelectedIDs {
		if n := st.Root.FindByID(id); n != nil && n.Parent != nil {
			count++
		}
	}
	return count
}

func (st *BlockState) topLevelSelectedNodes() []*Node {
	if st.Root == nil {
		return nil
	}
	if !st.hasMultiSelect() {
		if n := st.selectedNode(); n != nil {
			return []*Node{n}
		}
		return nil
	}
	var out []*Node
	var walk func(cur *Node)
	walk = func(cur *Node) {
		if cur == nil {
			return
		}
		if st.SelectedIDs[cur.ID] && isNodeVisibleInTree(cur) {
			out = append(out, cur)
			return
		}
		if cur.Folded {
			return
		}
		for _, ch := range cur.Children {
			walk(ch)
		}
	}
	walk(st.Root)
	if len(out) == 0 {
		if n := st.selectedNode(); n != nil {
			return []*Node{n}
		}
	}
	return out
}

// topLevelSelectedNodesExcludingRoot returns top-level visible selected nodes
// excluding the root, so multi-select cut and duplicate act consistently even
// when both root and branches (e.g. after Select All) are selected.
func (st *BlockState) topLevelSelectedNodesExcludingRoot() []*Node {
	if st.Root == nil {
		return nil
	}
	if !st.hasMultiSelect() {
		if n := st.selectedNode(); n != nil && n.Parent != nil {
			return []*Node{n}
		}
		return nil
	}
	var out []*Node
	var walk func(cur *Node)
	walk = func(cur *Node) {
		if cur == nil {
			return
		}
		if cur.Parent != nil && st.SelectedIDs[cur.ID] && isNodeVisibleInTree(cur) {
			out = append(out, cur)
			return
		}
		if cur.Folded {
			return
		}
		for _, ch := range cur.Children {
			walk(ch)
		}
	}
	for _, ch := range st.Root.Children {
		walk(ch)
	}
	return out
}

func (st *BlockState) opSelectAll() {
	if st.Root == nil {
		return
	}
	set := make(map[int]bool)
	var walk func(cur *Node)
	walk = func(cur *Node) {
		if cur == nil {
			return
		}
		set[cur.ID] = true
		if cur.Folded {
			return
		}
		for _, ch := range cur.Children {
			walk(ch)
		}
	}
	walk(st.Root)
	if len(set) <= 1 {
		st.setSingleSelection(st.Root.ID)
		return
	}
	st.SelectedIDs = set
	if st.SelectedID == 0 || !set[st.SelectedID] {
		st.SelectedID = st.Root.ID
	}
}

func (st *BlockState) opSelectSiblings() {
	n := st.selectedNode()
	if n == nil {
		return
	}
	if n.Parent == nil {
		st.setSingleSelection(n.ID)
		return
	}
	kids := n.Parent.Children
	if len(kids) <= 1 {
		st.setSingleSelection(n.ID)
		return
	}
	set := make(map[int]bool, len(kids))
	for _, ch := range kids {
		set[ch.ID] = true
	}
	st.SelectedIDs = set
	st.SelectedID = n.ID
}

func (st *BlockState) opSelectSubtree() {
	n := st.selectedNode()
	if n == nil {
		return
	}
	set := make(map[int]bool)
	var walk func(cur *Node)
	walk = func(cur *Node) {
		if cur == nil {
			return
		}
		set[cur.ID] = true
		if cur.Folded {
			return
		}
		for _, ch := range cur.Children {
			walk(ch)
		}
	}
	walk(n)
	if len(set) <= 1 {
		st.setSingleSelection(n.ID)
		return
	}
	st.SelectedIDs = set
	st.SelectedID = n.ID
}

func (st *BlockState) opExtendSiblingSelection(delta int) {
	n := st.selectedNode()
	if n == nil || n.Parent == nil {
		return
	}
	idx := n.IndexInParent()
	targetIdx := idx + delta
	if targetIdx < 0 || targetIdx >= len(n.Parent.Children) {
		return
	}
	target := n.Parent.Children[targetIdx]
	if len(st.SelectedIDs) == 0 {
		st.SelectedIDs = map[int]bool{n.ID: true, target.ID: true}
	} else {
		st.SelectedIDs[n.ID] = true
		st.SelectedIDs[target.ID] = true
	}
	st.SelectedID = target.ID
	st.EnsureVisiblePending = true
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
	st.setSingleSelection(child.ID)
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
	st.setSingleSelection(sib.ID)
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
		st.setSingleSelection(child.ID)
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
	st.setSingleSelection(sib.ID)
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
	st.setSingleSelection(wrapper.ID)
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
	st.setSingleSelection(n.ID)
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
	st.setSingleSelection(n.ID)
	st.EnsureVisiblePending = true
	st.commitChange(cb)
}

func (st *BlockState) opDelete(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	if st.hasMultiSelect() {
		st.opDeleteMulti(cb)
		return
	}
	st.opDeleteBranch(st.selectedNode(), cb)
}

func (st *BlockState) opDeleteBranch(n *Node, cb Callbacks) {
	if cb.ReadOnly || st.Root == nil || n == nil || n.Parent == nil {
		return
	}
	st.pushUndo()
	parent := n.Parent
	idx := n.IndexInParent()
	parent.Children = append(parent.Children[:idx], parent.Children[idx+1:]...)
	nextID := parent.ID
	if idx < len(parent.Children) {
		nextID = parent.Children[idx].ID
	} else if idx-1 >= 0 {
		nextID = parent.Children[idx-1].ID
	}
	st.setSingleSelection(nextID)
	st.EnsureVisiblePending = true
	st.commitChange(cb)
}

func (st *BlockState) opDeleteMulti(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil || len(st.SelectedIDs) == 0 {
		return
	}
	targets := st.topLevelSelectedNodesExcludingRoot()
	if len(targets) == 0 {
		st.clearMultiSelect()
		return
	}
	st.deleteTargetNodes(targets, cb)
}

func (st *BlockState) deleteTargetNodes(targets []*Node, cb Callbacks) {
	if cb.ReadOnly || st.Root == nil || len(targets) == 0 {
		return
	}
	delSet := make(map[int]bool, len(targets))
	for _, n := range targets {
		if n != nil && n.Parent != nil {
			delSet[n.ID] = true
		}
	}
	if len(delSet) == 0 {
		st.clearMultiSelect()
		return
	}
	st.pushUndo()

	nextSelID := st.Root.ID
	if cur := st.Root.FindByID(st.SelectedID); cur != nil {
		if !delSet[cur.ID] {
			nextSelID = cur.ID
		} else {
			// Highest deleted ancestor along the full chain: everything above it survives.
			topDel := cur
			for a := cur.Parent; a != nil; a = a.Parent {
				if delSet[a.ID] {
					topDel = a
				}
			}
			if survParent := topDel.Parent; survParent != nil {
				nextSelID = survParent.ID
				idx := topDel.IndexInParent()
				found := false
				for i := idx + 1; i < len(survParent.Children); i++ {
					if !delSet[survParent.Children[i].ID] {
						nextSelID = survParent.Children[i].ID
						found = true
						break
					}
				}
				if !found {
					for i := idx - 1; i >= 0; i-- {
						if !delSet[survParent.Children[i].ID] {
							nextSelID = survParent.Children[i].ID
							break
						}
					}
				}
			}
		}
	}

	var prune func(p *Node)
	prune = func(p *Node) {
		if p == nil || len(p.Children) == 0 {
			return
		}
		kept := p.Children[:0]
		for _, ch := range p.Children {
			if delSet[ch.ID] {
				continue
			}
			kept = append(kept, ch)
			prune(ch)
		}
		clear(p.Children[len(kept):])
		p.Children = kept
	}
	prune(st.Root)

	st.setSingleSelection(nextSelID)
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
		st.opDeleteBranch(n, cb)
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
	st.setSingleSelection(promoted[0].ID)
	st.EnsureVisiblePending = true
	st.commitChange(cb)
}

func (st *BlockState) opDuplicate(cb Callbacks) {
	if cb.ReadOnly || st.Root == nil {
		return
	}
	if st.hasMultiSelect() {
		targets := st.topLevelSelectedNodesExcludingRoot()
		if len(targets) == 0 {
			return
		}
		st.pushUndo()
		set := make(map[int]bool, len(targets))
		var firstID int
		for _, n := range targets {
			cp := n.Clone()
			st.NextID = cp.AssignIDs(st.NextID)
			cp.Parent = n.Parent
			idx := n.IndexInParent()
			kids := n.Parent.Children
			kids = append(kids, nil)
			copy(kids[idx+2:], kids[idx+1:])
			kids[idx+1] = cp
			n.Parent.Children = kids
			set[cp.ID] = true
			if firstID == 0 {
				firstID = cp.ID
			}
		}
		if len(set) == 1 {
			st.setSingleSelection(firstID)
		} else {
			st.SelectedIDs = set
			st.SelectedID = firstID
		}
		st.EnsureVisiblePending = true
		st.commitChange(cb)
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
	st.setSingleSelection(cp.ID)
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
	st.setSingleSelection(dragged.ID)
	st.EnsureVisiblePending = true
	st.commitChange(cb)
}

func serializeNodesForClipboard(nodes []*Node) string {
	if len(nodes) == 0 {
		return ""
	}
	if len(nodes) == 1 {
		return SerializeSubtreeMarkdown(nodes[0])
	}
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if s := SerializeSubtreeListItemMarkdown(n); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

func (st *BlockState) opCopy(c *ui.Context) bool {
	if c == nil || st.Root == nil {
		return false
	}
	if st.hasMultiSelect() {
		nodes := st.topLevelSelectedNodes()
		text := serializeNodesForClipboard(nodes)
		if text == "" {
			return false
		}
		c.Services().WriteClipboard(text)
		return true
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
	if st.hasMultiSelect() {
		targets := st.topLevelSelectedNodesExcludingRoot()
		if len(targets) == 0 {
			return false
		}
		text := serializeNodesForClipboard(targets)
		if text == "" {
			// Single empty-text leaf: encode as a list item so cut still copies what it removes.
			text = SerializeSubtreeListItemMarkdown(targets[0])
		}
		if text == "" {
			return false
		}
		c.Services().WriteClipboard(text)
		st.deleteTargetNodes(targets, cb)
		return true
	}
	n := st.selectedNode()
	if n == nil {
		return false
	}
	if !st.opCopy(c) {
		return false
	}
	if n.Parent != nil {
		st.opDeleteBranch(n, cb)
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
	st.setSingleSelection(nodes[0].ID)
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
	st.pruneHiddenMultiSelect()
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
	if st.hasMultiSelect() {
		var foldable []*Node
		anyExpanded := false
		for _, n := range st.topLevelSelectedNodes() {
			if len(n.Children) > 0 {
				foldable = append(foldable, n)
				if !n.Folded {
					anyExpanded = true
				}
			}
		}
		if len(foldable) == 0 {
			return
		}
		for _, n := range foldable {
			n.Folded = anyExpanded
		}
		if anyExpanded {
			st.pruneHiddenMultiSelect()
		}
		return
	}
	n := st.selectedNode()
	if n == nil || len(n.Children) == 0 {
		return
	}
	n.Folded = !n.Folded
	if n.Folded {
		st.pruneHiddenMultiSelect()
	}
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
	st.setSingleSelection(n.ID)
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
	st.clearMultiSelect()
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
	case "end":
		if st.Root != nil {
			cur := n
			if cur == nil || !cur.IsOpen() {
				cur = st.Root
			}
			for cur.IsOpen() {
				cur = cur.Children[len(cur.Children)-1]
			}
			st.SelectedID = cur.ID
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
	st.cancelViewFocus()
	st.clearMultiSelect()
	st.SpaceHeld = false
	st.SpaceUsedForPan = false
	st.lastKeyMod = 0
	st.resetBoxSelect()
	st.EditingID = n.ID
	st.EditSeq++
	st.EditDraft = n.Text
	st.EditCompose = ""
	st.editLastLayoutText = ""
	st.EditFresh = fresh
	st.EditSelectAll = true
	st.EditFocusPending = true
	st.EditInsertNewlinePending = false
	st.EnsureVisiblePending = true
	st.ComposeText = ""
	st.LinkNodeID = 0
}

func (st *BlockState) beginEditWithText(n *Node, initial string) {
	if n == nil {
		return
	}
	st.cancelViewFocus()
	st.clearMultiSelect()
	st.SpaceHeld = false
	st.SpaceUsedForPan = false
	st.lastKeyMod = 0
	st.resetBoxSelect()
	st.EditingID = n.ID
	st.EditSeq++
	st.EditDraft = initial
	st.EditCompose = ""
	st.editLastLayoutText = ""
	st.EditFresh = false
	st.EditSelectAll = false
	st.EditFocusPending = true
	st.EditInsertNewlinePending = false
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
	st.EditInsertNewlinePending = false
	st.EditHandle.CancelFocus()
	st.EditSelectAll = false
	st.ComposeText = ""
	st.lastKeyMod = 0
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
			st.setSingleSelection(n.ID)
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
	st.cancelViewFocus()
	st.SpaceHeld = false
	st.SpaceUsedForPan = false
	st.lastKeyMod = 0
	st.resetBoxSelect()
	st.EditingID = 0
	st.ExportMenuOpen = false
	st.ShortcutsOpen = false
	st.ImportResult = nil
	st.LinkNodeID = n.ID
	st.LinkDraft = n.Link
	st.LinkActiveIdx = -1
	st.LinkBrokenNotice = false
	st.LinkListScroll.Y = 0
	st.LinkComposing = false
	st.LinkFocusPending = true
}

func (st *BlockState) beginLinkEditBroken(n *Node) {
	st.beginLinkEdit(n)
	st.LinkBrokenNotice = true
}

func (st *BlockState) applyLink(target string, cb Callbacks) {
	if cb.ReadOnly || st.LinkNodeID == 0 || st.Root == nil {
		st.LinkNodeID = 0
		st.LinkFocusPending = false
		st.LinkComposing = false
		st.LinkBrokenNotice = false
		st.LinkHandle.CancelFocus()
		return
	}
	n := st.Root.FindByID(st.LinkNodeID)
	st.LinkNodeID = 0
	st.LinkFocusPending = false
	st.LinkComposing = false
	st.LinkBrokenNotice = false
	st.LinkHandle.CancelFocus()
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
	st.SpaceHeld = false
	st.SpaceUsedForPan = false
	st.lastKeyMod = 0
	st.resetBoxSelect()
	st.FindOpen = true
	st.FindComposing = false
	st.FindFocusPending = true
	st.cancelViewFocus()
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
	st.FindComposing = false
	st.FindHandle.CancelFocus()
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
		st.setSingleSelection(curID)
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
		st.setSingleSelection(node.ID)
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
	if mode != ViewMap && st.FindOpen {
		st.CloseFind()
	}
	st.LinkNodeID = 0
	st.LinkBrokenNotice = false
	st.LinkFocusPending = false
	st.LinkComposing = false
	st.FindComposing = false
	st.LinkHandle.CancelFocus()
	st.ExportMenuOpen = false
	st.ShortcutsOpen = false
	st.ImportResult = nil
	st.ImportUndoBefore = ""
	st.ImportUndoAfter = ""
	st.OutlineUndoNodeID = 0
	st.OutlineComposing = false
	st.OutlineInsertNewlineID = 0
	st.SourceComposing = false
	st.ComposeText = ""
	st.lastKeyMod = 0
	st.SpaceHeld = false
	st.SpaceUsedForPan = false
	st.Panning = false
	st.resetBoxSelect()
	st.resetDrag()
	st.clearMultiSelect()
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
	st.RequestFocus(true)
	if mode == ViewOutline {
		if activeRoot := st.outlineActiveRoot(); activeRoot != nil {
			targetID := outlineVisibleFocusTarget(activeRoot, 0, st.SelectedID)
			if target := activeRoot.FindByID(targetID); target != nil {
				st.focusOutlineNode(target.ID, utf8.RuneCountInString(target.Text))
			}
		}
		st.viewFocusPending = false
		st.viewFocusPreventScroll = false
	}
	if cb.OnRequestFocus != nil {
		cb.OnRequestFocus()
	}
}

// captureBlockTextKey queues block-scoped chords typed inside a text field so
// the field never swallows them.
func (st *BlockState) captureBlockTextKey(k ui.TextKey) bool {
	if isMMPrimaryMod(k.Mods) {
		if mode, ok := viewForDigitKey(k.Key); ok {
			st.pendingView = mode
			return true
		}
	}
	if isMMPrimaryShiftMod(k.Mods) && k.Key == ui.KeyEnter {
		st.pendingFSToggle = true
		return true
	}
	if (isMMPrimaryShiftMod(k.Mods) && k.Key == ui.KeySlash) || (theme.IsMacOS() && k.Mods == 0 && k.Key == ui.KeyF1) {
		st.pendingShortcuts = true
		return true
	}
	return false
}

// handleBlockShortcuts runs chords scoped to this block. They only fire while
// the block owns focus or is fullscreen, so ⌘1-9 still switch editor tabs
// everywhere else.
func (st *BlockState) handleBlockShortcuts(c *ui.Context, fullscreen bool, cb Callbacks) {
	view, fsToggle, toggleShortcuts := st.pendingView, st.pendingFSToggle, st.pendingShortcuts
	st.pendingView, st.pendingFSToggle, st.pendingShortcuts = "", false, false
	if !fullscreen && !st.HasFocus(c) && view == "" && !fsToggle && !toggleShortcuts {
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
	if !toggleShortcuts && !c.ShortcutRepeated() &&
		(c.Shortcut(ui.Cmd|ui.Shift, ui.KeySlash) || (theme.IsMacOS() && c.Shortcut(0, ui.KeyF1))) {
		toggleShortcuts = true
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
	if toggleShortcuts {
		st.ShortcutsOpen = !st.ShortcutsOpen
		if st.ShortcutsOpen {
			st.ExportMenuOpen = false
			st.LinkNodeID = 0
			st.ImportResult = nil
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
	cb.OnSaveExportFile(base+"."+ext, ext, data, func(savedPath string, saveErr error) {
		if cb.OnNotify == nil {
			return
		}
		if saveErr != nil {
			cb.OnNotify(theme.Tr("导出失败：", "Export failed: ")+saveErr.Error(), "danger")
			return
		}
		displayName := base + "." + ext
		if savedBase := strings.TrimSpace(savedPath); savedBase != "" {
			if idx := strings.LastIndexAny(savedBase, "/\\"); idx >= 0 {
				savedBase = savedBase[idx+1:]
			}
			if savedBase != "" {
				displayName = savedBase
			}
		}
		cb.OnNotify(theme.Tr("已导出 "+displayName, "Exported "+displayName), "success")
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
	st.SpaceHeld = false
	st.SpaceUsedForPan = false
	st.lastKeyMod = 0
	st.resetBoxSelect()
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
	st.RequestFocus(true)

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
	st.SpaceHeld = false
	st.SpaceUsedForPan = false
	st.lastKeyMod = 0
	st.resetBoxSelect()
	st.clearMultiSelect()
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
	t = max(0, min(t, 1))
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
	st.tabFocusClaimedThisFrame = false
	st.inlineRenderedThisFrame = true
	st.inlineRenderedLastFrame = true
	st.SyncSource(source, cfg)
	st.lastCfg = cfg
	st.lastCfgSet = true
	st.reduceMotion = c.Preferences().ReduceMotion
	st.lastHasFocus = st.HasFocus(c)
	st.lastInlineFocused = focused || st.lastHasFocus || st.EditingID != 0
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
			st.renderHintBar(c, id, cb)
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
	st.tabFocusClaimedThisFrame = false
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

	st.lastHasFocus = st.HasFocus(c)
	if st.Fullscreen && !st.fsAnimActive {
		st.handleBlockShortcuts(c, true, cb)
	}
	if st.Fullscreen && st.View == ViewOutline && st.EditingID == 0 && st.LinkNodeID == 0 && !st.OutlineHandle.FocusWithin(c) {
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyPeriod) || c.Shortcut(ui.Cmd|ui.Alt, ui.KeySlash) {
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
	hadModalPopup := st.TutorialOpen || st.ShortcutsOpen || st.ExportMenuOpen || st.ImportResult != nil
	overlay.Children(func() {
		st.renderHeader(c, id+"-fs", true, cb)
		if !st.IsFullscreenOverlayActive() && animatingOverlay == false {
			c.Invalidate()
			return
		}
		st.renderBody(c, id+"-fs", 0, true, cb)
		if cfg.ShowHints && st.View != ViewSource {
			st.renderHintBar(c, id+"-fs", cb)
		}
	})
	if st.Fullscreen && !hadModalPopup && st.EditingID == 0 && !st.EditFocusPending && st.EditCompose == "" &&
		st.LinkNodeID == 0 && !st.LinkFocusPending && !st.LinkComposing &&
		st.ComposeText == "" && !st.OutlineComposing && !st.SourceComposing &&
		!st.FindHandle.Focused(c) && !st.FindFocusPending && !st.FindComposing &&
		!(st.FindOpen && st.FindBarHandle.FocusWithin(c)) &&
		!st.OutlineHandle.FocusWithin(c) && overlay.OverlayShortcut(0, ui.KeyEscape) {
		switch {
		case st.BoxSelecting:
			st.resetBoxSelect()
		case st.Panning:
			st.Panning = false
		case st.DragActive || st.DragNodeID != 0:
			st.resetDrag()
		case st.FindOpen:
			st.CloseFind()
		case st.View == ViewMap && st.hasMultiSelect():
			st.clearMultiSelect()
		case st.View == ViewOutline && st.OutlineZoomID != 0:
			if cur := st.outlineActiveRoot(); cur != nil && cur != st.Root && cur.Parent != nil && cur.Parent != st.Root {
				st.OutlineZoomID = cur.Parent.ID
			} else {
				st.OutlineZoomID = 0
			}
			c.Invalidate()
		default:
			st.ExitFullscreen(cb)
			c.Invalidate()
		}
	}
}

// --- Header Bar ---

func (st *BlockState) renderHeader(c *ui.Context, id string, fullscreen bool, cb Callbacks) {
	pal := theme.Ocean()
	hdr := ui.Row(c.Key("mm-hdr-" + id)).FillWidth().Height(theme.HeightLarge).Shrink(0).
		PaddingX(theme.Space6).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space6).
		Background(pal.Paper)
	if st.View == ViewMap {
		st.keepCanvasButtonFocus(c, hdr)
	}
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
			showDigits := scoped && isMMPrimaryMod(c.Modifiers())
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
					st.keepCanvasButtonFocus(c, btn)
					btn.Children(func() {
						icons.DrawAnimated(c, tab.icon, theme.IconInline, pal.DuoIconColors(fg), active || hov || prs)
						ui.Text(c, tab.label).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
						if showDigits {
							bui.Keycap(c, sc)
						}
					})
					if btn.Clicked() {
						st.SwitchView(tab.mode, true, cb)
					}
				}
			})
		})

		// Right: Context-aware actions tailored to the active view.
		actions := ui.Row(c.Key("mm-act-" + id)).AlignItems(ui.Center).Gap(theme.Space2).Shrink(0)
		actions.Children(func() {
			// Undo / Redo chrome buttons in Map and Outline views.
			if st.View != ViewSource && !cb.ReadOnly {
				st.renderHeaderHistoryBtn(c, "mm-undo-"+id, false, st.canUndo(cb), theme.Tr("撤销", "Undo"), mmShortcutLabel("⌘Z", "Ctrl+Z"), func() {
					st.opUndo(cb)
					st.focusCanvas(c, cb)
					c.Invalidate()
				})
				st.renderHeaderHistoryBtn(c, "mm-redo-"+id, true, st.canRedo(cb), theme.Tr("重做", "Redo"), mmShortcutLabel("⌘⇧Z", "Ctrl+Y"), func() {
					st.opRedo(cb)
					st.focusCanvas(c, cb)
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
					Shortcut: mmShortcutLabel("⌘F", "Ctrl+F"),
					Size:     theme.HeightChrome,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusSM,
					Active:   st.FindOpen,
				})
				if findBtn.Clicked() {
					if st.FindOpen {
						st.CloseFind()
						st.focusCanvas(c, cb)
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
				layBtn := st.keepCanvasButtonFocus(c, bui.IconButton(c, layIcon, bui.IconButtonOptions{
					ID:       "mm-layout-" + id,
					Label:    layTip,
					Tooltip:  layTip,
					Shortcut: mmShortcutLabel("⌘⇧L", "Ctrl+Shift+L"),
					Size:     theme.HeightChrome,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusSM,
				}))
				if layBtn.Clicked() {
					st.ToggleLayout()
					st.focusCanvas(c, cb)
				}

				fitTip := theme.Tr("适配画布", "Fit to view")
				fitBtn := st.keepCanvasButtonFocus(c, bui.IconButton(c, icons.TbReset, bui.IconButtonOptions{
					ID:       "mm-fit-" + id,
					Label:    fitTip,
					Tooltip:  fitTip,
					Shortcut: mmShortcutLabel("⌘0", "Ctrl+0"),
					Size:     theme.HeightChrome,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusSM,
				}))
				if fitBtn.Clicked() {
					st.AutoFitPending = true
					st.focusCanvas(c, cb)
				}

			case ViewOutline:
				hasExpanded := st.outlineHasAnyExpanded()
				foldIcon := icons.UnfoldAll
				foldTip := theme.Tr("全部展开", "Expand all")
				if hasExpanded {
					foldIcon = icons.FoldAll
					foldTip = theme.Tr("全部折叠", "Collapse all")
				}
				foldBtn := st.keepCanvasButtonFocus(c, bui.IconButton(c, foldIcon, bui.IconButtonOptions{
					ID:       "mm-fold-all-" + id,
					Label:    foldTip,
					Tooltip:  foldTip,
					Shortcut: mmShortcutLabel("⌘⇧.", "Ctrl+Shift+."),
					Size:     theme.HeightChrome,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusSM,
				}))
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
				}).KeepFocus()
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
					impBtn := st.keepCanvasButtonFocus(c, bui.IconButton(c, icons.Upload, bui.IconButtonOptions{
						ID:          "mm-import-" + id,
						Label:       theme.Tr("导入 (.md / .opml / .mm / .xmind)", "Import (.md / .opml / .mm / .xmind)"),
						TooltipSpec: &impSpec,
						Size:        theme.HeightChrome,
						IconSize:    theme.IconAction,
						Radius:      theme.RadiusSM,
					}))
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
				expBtn := st.keepCanvasButtonFocus(c, bui.IconButton(c, icons.Download, expOpts))
				if expBtn.Clicked() {
					st.ExportMenuOpen = !st.ExportMenuOpen
					if st.ExportMenuOpen {
						st.ShortcutsOpen = false
						st.LinkNodeID = 0
					}
					st.focusCanvas(c, cb)
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
			fsBtn := st.keepCanvasButtonFocus(c, bui.IconButton(c, fsIcon, bui.IconButtonOptions{
				ID:       "mm-fs-" + id,
				Label:    fsTip,
				Tooltip:  fsTip,
				Shortcut: fsShortcut,
				Size:     theme.HeightChrome,
				IconSize: theme.IconAction,
				Radius:   theme.RadiusSM,
			}))
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
	st.keepCanvasButtonFocus(c, btn)
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
			st.renderShortcutsPopover(c, id, cb)
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
	fsRestore := !fullscreen && st.consumeFullscreenFocusRestore()
	reqFocus, reqPreventScroll := st.consumeViewFocus()
	if (fsRestore || reqFocus) && st.EditingID == 0 && st.LinkNodeID == 0 && !(st.FindOpen && st.FindFocusPending) {
		if fsRestore || reqPreventScroll {
			canvas.Focus(ui.FocusOptions{PreventScroll: true})
		} else {
			canvas.Focus()
		}
		if fsRestore && cb.OnRequestFocus != nil {
			cb.OnRequestFocus()
		}
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
	findFocused := st.FindOpen && (st.FindFocusPending || st.FindHandle.Focused(c) || st.FindBarHandle.FocusWithin(c))
	canvasFocused := (canvas.Focused() || st.CanvasHandle.Focused(c)) &&
		st.EditingID == 0 && st.LinkNodeID == 0 && !findFocused
	st.LastCanvasFocused = canvasFocused
	if st.EditingID != 0 || st.EditFocusPending || st.LinkNodeID != 0 || findFocused ||
		(st.EditingID == 0 && st.LinkNodeID == 0 && !findFocused && canvas.Focused()) {
		st.tabFocusClaimedThisFrame = true
	}

	spaceAllowed := st.EditingID == 0 && st.LinkNodeID == 0 && !findFocused &&
		!st.TutorialOpen && !st.ShortcutsOpen && !st.ExportMenuOpen && st.ImportResult == nil && st.ComposeText == "" &&
		(canvasFocused || (fullscreen && !c.FocusTakesText()))
	if st.SpaceHeld && (!spaceAllowed || !c.KeyDown(ui.KeySpace)) {
		st.SpaceHeld = false
		st.SpaceUsedForPan = false
	}
	if (c.Modifiers() & (ui.Cmd | ui.Ctrl | ui.Alt)) == 0 {
		st.lastKeyMod = 0
	}
	spacePanReady := spaceAllowed && (st.SpaceHeld || c.KeyDown(ui.KeySpace))

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
				if m.Item(theme.Tr("打开链接", "Open Link")).Shortcut(ui.Alt, ui.KeyEnter).Chosen() {
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
		if !cb.ReadOnly && (n.Parent != nil || (st.hasMultiSelect() && st.multiSelectDeletableCount() > 0)) {
			m.Separator()
			delLbl := theme.Tr("删除", "Delete")
			if st.hasMultiSelect() {
				if cnt := st.multiSelectDeletableCount(); cnt > 0 {
					delLbl = theme.Tr(fmt.Sprintf("删除所选 (%d)", cnt), fmt.Sprintf("Delete Selected (%d)", cnt))
				}
			}
			delKey := ui.KeyBackspace
			if !theme.IsMacOS() {
				delKey = ui.KeyDelete
			}
			if m.Item(delLbl).Shortcut(0, delKey).Chosen() {
				st.opDelete(cb)
			}
			if n.Parent != nil && len(n.Children) > 0 && !st.hasMultiSelect() {
				if m.Item(theme.Tr("仅删除当前节点", "Delete Single Node")).Shortcut(ui.Alt, delKey).Chosen() {
					st.opDeleteSingle(cb)
				}
			}
		}
	})

	// Pointer, Scroll, Zoom, and Keyboard input handler.
	canvas.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerMove:
			// A lost release (capture taken by another control) leaves no button
			// down: drop the stale gesture instead of panning / boxing / dragging on hover.
			if (st.Panning || st.BoxSelecting || st.DragNodeID != 0) && ev.Pointer == ui.PointerMouse && ev.Pressure == 0 {
				st.releaselessMoves++
				if st.releaselessMoves >= 2 {
					st.releaselessMoves = 0
					st.Panning = false
					st.resetBoxSelect()
					st.resetDrag()
				}
			} else {
				st.releaselessMoves = 0
			}
			if st.Panning && st.Camera.Scale > 0 {
				st.fsCamAnimActive = false
				if st.SpaceHeld && math.Hypot(float64(ev.X-st.panStartX), float64(ev.Y-st.panStartY)) >= 4 {
					st.SpaceUsedForPan = true
				}
				dx := ev.X - st.PanLastX
				dy := ev.Y - st.PanLastY
				st.Camera.X -= dx / st.Camera.Scale
				st.Camera.Y -= dy / st.Camera.Scale
				st.PanLastX, st.PanLastY = ev.X, ev.Y
				return true
			}
			if st.BoxSelecting {
				st.BoxCurrX, st.BoxCurrY = ev.X, ev.Y
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
					outOfCanvas := st.LastCanvasW > 0 && st.LastCanvasH > 0 &&
						(ev.X < 0 || ev.X > st.LastCanvasW || ev.Y < 0 || ev.Y > st.LastCanvasH)
					if outOfCanvas {
						st.DragTargetID = 0
						st.DragPos = DropNone
					} else if dragged := st.Root.FindByID(st.DragNodeID); dragged != nil {
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
			if spacePanReady {
				st.HoveredID = 0
				st.HoveredHit = HitNone
				return false
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
			st.resetBoxSelect()
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
				// Right-click selects hit node and focuses canvas before ContextMenu opens.
				st.RequestFocus(true)
				st.CanvasHandle.Focus(c)
				if cb.OnRequestFocus != nil {
					cb.OnRequestFocus()
				}
				if hit.Node != nil {
					if !st.SelectedIDs[hit.Node.ID] {
						st.setSingleSelection(hit.Node.ID)
					} else {
						st.SelectedID = hit.Node.ID
					}
				}
				return false
			}
			if ev.Button == 2 {
				// Middle-mouse drag pans the canvas anywhere, even over a node.
				st.RequestFocus(true)
				st.CanvasHandle.Focus(c)
				if cb.OnRequestFocus != nil {
					cb.OnRequestFocus()
				}
				if st.fsAnimActive {
					return true
				}
				st.Panning = true
				st.panButton = 2
				st.panStartX, st.panStartY = ev.X, ev.Y
				st.PanLastX, st.PanLastY = ev.X, ev.Y
				return true
			}
			if ev.Button != 0 {
				return false
			}

			// SpaceHeld is set only by an unmodified Space KeyDown the canvas itself
			// received and cleared on KeyUp, so it is live here (no frame-old snapshot).
			wasSpacePan := st.SpaceHeld
			// Space pressed before the canvas had focus (e.g. scrolled into an embedded map):
			// honour the physically held key unless a text field owns it.
			if !wasSpacePan && c.KeyDown(ui.KeySpace) && !c.FocusTakesText() && (ev.Mods&(ui.Cmd|ui.Ctrl|ui.Alt|ui.Shift)) == 0 &&
				st.EditingID == 0 && st.LinkNodeID == 0 && !st.FindHandle.Focused(c) {
				st.SpaceHeld = true
				wasSpacePan = true
			}
			if wasSpacePan {
				// Any click while Space is held means "pan", never "fold" on release.
				st.SpaceUsedForPan = true
			}
			st.RequestFocus(true)
			st.CanvasHandle.Focus(c)
			if cb.OnRequestFocus != nil {
				cb.OnRequestFocus()
			}

			if wasSpacePan {
				if !st.fsAnimActive {
					st.Panning = true
					st.panButton = 0
					st.panStartX, st.panStartY = ev.X, ev.Y
					st.PanLastX, st.PanLastY = ev.X, ev.Y
				}
				return true
			}

			switch hit.Kind {
			case HitLinkBadge:
				st.setSingleSelection(hit.Node.ID)
				st.openNodeLink(c.Now(), hit.Node, cb)
				return true
			case HitHoverFold:
				if !cb.ReadOnly {
					st.setSingleSelection(hit.Node.ID)
					hit.Node.Folded = true
					st.HoveredHit = HitNone
				}
				return true
			case HitHoverAdd:
				if !cb.ReadOnly {
					st.setSingleSelection(hit.Node.ID)
					st.HoveredHit = HitNone
					st.opAddChild(cb)
				}
				return true
			case HitCheckbox:
				st.opFlipTaskDone(hit.Node, cb)
				return true
			case HitFoldBadge:
				hit.Node.Folded = false
				st.setSingleSelection(hit.Node.ID)
				st.HoveredHit = HitNone
				return true
			case HitNode:
				if (ev.Mods & (ui.Shift | ui.Cmd | ui.Ctrl)) != 0 {
					st.toggleMultiSelectNode(hit.Node.ID)
					return true
				}
				st.setSingleSelection(hit.Node.ID)
				if ev.Clicks >= 2 && !cb.ReadOnly {
					st.beginEdit(hit.Node, false)
					return true
				}
				if !cb.ReadOnly && hit.Node.Parent != nil && ev.Clicks <= 1 {
					st.DragNodeID = hit.Node.ID
					st.DragActive = false
					st.DragStartX, st.DragStartY = ev.X, ev.Y
					st.DragCurrX, st.DragCurrY = ev.X, ev.Y
					st.DragTargetID = 0
					st.DragPos = DropNone
				}
				return true
			default:
				additive := (ev.Mods & (ui.Shift | ui.Cmd | ui.Ctrl)) != 0
				if ev.Clicks >= 2 && !additive {
					st.clearMultiSelect()
					st.FitCanvas()
					return true
				}
				if !additive {
					st.setSingleSelection(0)
				}
				if st.fsAnimActive {
					// Canvas is still moving during the fullscreen transition.
					return true
				}
				st.BoxSelecting = true
				st.BoxAdditive = additive
				st.BoxStartX, st.BoxStartY = ev.X, ev.Y
				st.BoxCurrX, st.BoxCurrY = ev.X, ev.Y
				return true
			}

		case ui.InputPointerUp:
			if st.Panning {
				if ev.Button == st.panButton || ev.Canceled {
					st.Panning = false
				}
				return true
			}
			if st.BoxSelecting && (ev.Button == 0 || ev.Canceled) {
				if !ev.Canceled && st.boxSelectActive() && st.Root != nil && st.Camera.Scale > 0 {
					st.applyBoxSelection(st.BoxAdditive)
				}
				st.resetBoxSelect()
				return true
			}
			st.releaselessMoves = 0
			if st.DragNodeID != 0 {
				// Re-resolve the drop at the actual release point, not the last move.
				if !ev.Canceled && st.DragActive && st.Root != nil {
					st.DragTargetID, st.DragPos = 0, DropNone
					inCanvas := st.LastCanvasW <= 0 || st.LastCanvasH <= 0 ||
						(ev.X >= 0 && ev.X <= st.LastCanvasW && ev.Y >= 0 && ev.Y <= st.LastCanvasH)
					if dragged := st.Root.FindByID(st.DragNodeID); dragged != nil && inCanvas {
						wx, wy := st.Camera.ScreenToWorld(ev.X, ev.Y)
						if target, pos := HitTestDropTarget(st.Root, dragged, wx, wy); target != nil {
							st.DragTargetID, st.DragPos = target.ID, pos
						}
					}
				}
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
			case "selectAll":
				st.opSelectAll()
				return true
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
			if st.EditingID != 0 || st.LinkNodeID != 0 || st.TutorialOpen || st.ShortcutsOpen || st.ExportMenuOpen || st.ImportResult != nil || cb.ReadOnly || st.DragActive {
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
			if st.LinkNodeID != 0 || st.TutorialOpen || st.ShortcutsOpen || st.ExportMenuOpen || st.ImportResult != nil || cb.ReadOnly || st.DragActive {
				return false
			}
			if st.EditingID != 0 {
				if st.EditFocusPending && (st.lastKeyMod&(ui.Cmd|ui.Ctrl|ui.Alt)) == 0 && isDirectEditText(ev.Text) {
					if st.EditSelectAll {
						st.EditDraft = ev.Text
						st.EditSelectAll = false
					} else {
						st.EditDraft += ev.Text
					}
					return true
				}
				return false
			}
			if st.SpaceHeld && ev.Text == " " {
				return true
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
			if st.TutorialOpen {
				return false
			}
			if ev.Key == ui.KeyEscape && ev.Mods == 0 && (st.EditingID != 0 || st.LinkNodeID != 0) {
				st.SpaceHeld = false
				st.SpaceUsedForPan = false
				if st.ImportResult != nil {
					st.ImportResult = nil
					st.ImportUndoBefore = ""
					st.ImportUndoAfter = ""
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
				if st.LinkNodeID != 0 {
					if st.LinkComposing {
						return false
					}
					st.LinkNodeID = 0
					st.LinkBrokenNotice = false
					st.LinkFocusPending = false
					st.LinkComposing = false
					st.LinkHandle.CancelFocus()
					return true
				}
				if st.EditingID != 0 {
					if st.EditCompose != "" {
						return false
					}
					st.finishEdit(false, cb)
					return true
				}
			}
			if st.EditingID != 0 || st.LinkNodeID != 0 {
				st.SpaceHeld = false
				st.SpaceUsedForPan = false
				return false
			}
			if ev.Key == ui.KeyEscape && ev.Mods == 0 {
				if st.ComposeText != "" {
					st.ComposeText = ""
					return true
				}
				if st.BoxSelecting {
					st.resetBoxSelect()
					return true
				}
				if st.Panning {
					st.Panning = false
					return true
				}
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
					st.ImportUndoBefore = ""
					st.ImportUndoAfter = ""
					return true
				}
				if st.hasMultiSelect() {
					st.clearMultiSelect()
					return true
				}
				if st.Fullscreen {
					st.ExitFullscreen(cb)
					c.Invalidate()
					return true
				}
			}
			if st.ComposeText != "" || st.ShortcutsOpen || st.ExportMenuOpen || st.ImportResult != nil {
				return false
			}
			if !st.FindOpen && !st.BoxSelecting && !st.Panning && !st.DragActive && st.DragNodeID == 0 && cb.OnHostNavKey != nil && cb.OnHostNavKey(ev.Key, ev.Mods) {
				return true
			}
			if ev.Key == ui.KeySpace && ev.Mods == 0 {
				if !st.DragActive && !st.BoxSelecting && !st.Panning {
					if !ev.Repeat && !st.SpaceHeld {
						st.SpaceHeld = true
						st.SpaceUsedForPan = false
					}
				}
				return true
			}
			return st.handleCanvasKey(c, ev.Key, ev.Mods, cb)

		case ui.InputKeyUp:
			st.lastKeyMod = 0
			if ev.Key == ui.KeySpace && st.SpaceHeld {
				shouldFold := !st.SpaceUsedForPan && st.EditingID == 0 && st.LinkNodeID == 0 &&
					st.ComposeText == "" && !st.ShortcutsOpen && !st.ExportMenuOpen &&
					!st.DragActive && !st.BoxSelecting
				st.SpaceHeld = false
				st.SpaceUsedForPan = false
				if shouldFold {
					st.opToggleFold()
				}
				return true
			}
			return false
		}
		return false
	})

	canvasHovered := canvas.Hovered()
	if !canvasHovered && !st.Panning && !st.BoxSelecting && st.DragNodeID == 0 && st.HoveredID != 0 {
		st.HoveredID = 0
		st.HoveredHit = HitNone
	}

	// Dynamic canvas cursor: pointer over interactive badges/buttons, grab when Space-pan ready or on draggable nodes, grabbing when panning/dragging.
	switch {
	case st.Panning || st.DragActive:
		canvas.Cursor(ui.CursorGrabbing)
	case st.BoxSelecting:
		canvas.Cursor(ui.CursorDefault)
	case canvasHovered && spacePanReady:
		canvas.Cursor(ui.CursorGrab)
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
				if st.fsCamToValid {
					if math.Abs(float64(r.W-st.fsCamToW)) > 2 || math.Abs(float64(r.H-st.fsCamToH)) > 2 {
						// Target was fitted for an estimated viewport; refit to the real one so it stays centered.
						st.Camera = FitCamera(worldBounds, r.W, r.H, 28, 22)
					} else if !st.fsAnimEntering {
						st.Camera = st.fsCamTo
					}
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
		if !canvasHovered || st.LinkNodeID != 0 || st.ExportMenuOpen || st.ShortcutsOpen || st.ImportResult != nil || spacePanReady || st.Panning || st.BoxSelecting {
			hoveredID = 0
			hoveredHit = HitNone
		}
		findCurID := 0
		if st.FindOpen && len(st.FindMatchIDs) > 0 && st.FindCurrentIdx >= 0 && st.FindCurrentIdx < len(st.FindMatchIDs) {
			findCurID = st.FindMatchIDs[st.FindCurrentIdx]
		}
		selID := st.SelectedID
		selIDs := st.SelectedIDs
		boxActive := st.boxSelectActive()
		if boxActive && st.Root != nil && st.Camera.Scale > 0 {
			selID, selIDs = st.boxSelectResult(st.BoxAdditive)
		}
		opt := PaintOptions{
			SelectedID:  selID,
			SelectedIDs: selIDs,
			HoveredID:   hoveredID,
			HoveredHit:  hoveredHit,
			EditingID:   paintEditID,
			Palette:     pal,
			Background:  pal.CodeBG,
			ReadOnly:    cb.ReadOnly,
			LinkStatusFn: func(target string) LinkStatus {
				return st.resolveLinkStatusCached(now, target, cb)
			},
			FindActive:      st.FindOpen && strings.TrimSpace(st.FindQuery) != "",
			FindMatchIDs:    st.FindMatchSet,
			FindCurrentID:   findCurID,
			BoxSelectActive: boxActive,
			BoxSelectRect:   st.boxSelectRect(),
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
	if canvasHovered && st.HoveredID != 0 && st.HoveredHit == HitLinkBadge && st.EditingID == 0 && st.LinkNodeID == 0 && !st.DragActive && !st.Panning && !st.BoxSelecting && !spacePanReady && st.Root != nil {
		if hn := st.Root.FindByID(st.HoveredID); hn != nil && strings.TrimSpace(hn.Link) != "" {
			st.renderLinkHoverTooltip(c, id, hn, cw, ch, cb)
		}
	}

	// Bottom-left selected node quick action bar (Task toggle, Link edit, Add child).
	if !cb.ReadOnly && st.SelectedID != 0 && st.EditingID == 0 && st.LinkNodeID == 0 && !st.DragActive && !st.Panning && !st.boxSelectActive() && !st.ExportMenuOpen && !st.ShortcutsOpen && st.ImportResult == nil && st.Root != nil {
		if sel := st.Root.FindByID(st.SelectedID); sel != nil {
			st.renderSelectionQuickBar(c, id, sel, cb)
		}
	}

	// Bottom-right floating Zoom & Fold-All HUD.
	if st.Root != nil && !st.ShortcutsOpen {
		st.renderZoomHUD(c, id, cb)
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
			if st.EditInsertNewlinePending {
				st.EditInsertNewlinePending = false
				s0, s1 := input.TextSelection()
				input.ReplaceText(s0, s1, "\n")
			}
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
				input.Focus(ui.FocusOptions{PreventScroll: true})
				if st.EditSelectAll {
					st.EditHandle.SelectAll(c)
				} else {
					st.EditHandle.Focus(c)
				}
			}
			if !input.Composing() {
				st.EditCompose = ""
			}
			if input.Focused() {
				st.tabFocusClaimedThisFrame = true
			}
			input.ObserveInput(func(ev ui.InputEvent) {
				switch ev.Kind {
				case ui.InputCompose:
					st.EditCompose = ev.Text
				case ui.InputText, ui.InputCommand:
					st.EditCompose = ""
				}
			})
			input.OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyUnknown || st.EditCompose != "" {
					return false
				}
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
					if k.Mods == ui.Alt {
						st.EditInsertNewlinePending = true
						return true
					}
					if isMMPrimaryMod(k.Mods) {
						st.EditDraft = k.Text()
						st.finishEdit(true, cb)
						if target := st.Root.FindByID(st.SelectedID); target != nil {
							st.opToggleTaskDoneOrCreate(target, cb)
						}
						st.restoreCanvasFocusAfterEdit(cb)
						return true
					}
					if k.Mods == 0 {
						st.EditDraft = k.Text()
						st.finishEdit(true, cb)
						st.restoreCanvasFocusAfterEdit(cb)
						return true
					}
				case ui.KeyEscape:
					if k.Mods == 0 {
						// Dismiss topmost popover first without discarding the active rename draft.
						if st.ImportResult != nil {
							st.ImportResult = nil
							st.ImportUndoBefore = ""
							st.ImportUndoAfter = ""
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
						st.finishEdit(false, cb)
						st.restoreCanvasFocusAfterEdit(cb)
						return true
					}
				case ui.KeyTab:
					if k.Mods == ui.Shift {
						st.EditDraft = k.Text()
						st.finishEdit(true, cb)
						st.opInsertParent(cb)
						if st.EditingID == 0 {
							st.restoreCanvasFocusAfterEdit(cb)
						}
						return true
					}
					if k.Mods == 0 {
						st.EditDraft = k.Text()
						st.finishEdit(true, cb)
						st.opAddChild(cb)
						if st.EditingID == 0 {
							st.restoreCanvasFocusAfterEdit(cb)
						}
						return true
					}
				}
				return false
			})
			if input.FocusLeft() && !st.ShortcutsOpen && !st.ExportMenuOpen && st.ImportResult == nil {
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

// ResetZoom resets the canvas zoom to 100% around the viewport center.
func (st *BlockState) ResetZoom() {
	if st == nil {
		return
	}
	st.zoomAt(st.LastCanvasW*0.5, st.LastCanvasH*0.5, 1.0)
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
	return st.EditingID == 0 && st.DragNodeID == 0 && !st.BoxSelecting && !st.Panning &&
		st.LinkNodeID == 0 && !st.ExportMenuOpen && !st.ShortcutsOpen && st.ImportResult == nil
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
		// Empty selection: arrows pick the root again, node-scoped chords are
		// no-ops, and global chords (undo, find, zoom, layout, select-all) still run.
		if st.Root == nil {
			return false
		}
		switch {
		case mods == 0:
			switch key {
			case ui.KeyUp, ui.KeyDown, ui.KeyLeft, ui.KeyRight, ui.KeyHome, ui.KeyEnd:
				st.setSingleSelection(st.Root.ID)
				st.EnsureVisiblePending = true
				return true
			case ui.KeyF3:
				if !theme.IsMacOS() && st.FindOpen {
					st.stepFind(1)
					return true
				}
			}
			return false
		case mods == ui.Shift:
			if key == ui.KeyF3 && !theme.IsMacOS() && st.FindOpen {
				st.stepFind(-1)
				return true
			}
			return false
		case mods == ui.Alt:
			return false
		case isMMPrimaryMod(mods) &&
			(key == ui.KeyC || key == ui.KeyX || key == ui.KeyD || key == ui.KeyE || key == ui.KeyL || key == ui.KeyEnter || key == ui.KeySlash || key == ui.KeyPeriod):
			return false
		case isMMPrimaryShiftMod(mods) && key == ui.KeyA:
			return false
		case isMMPrimaryAltMod(mods) && (key == ui.KeyT || key == ui.KeyA):
			return false
		}
		n = st.Root
	}
	leftSide := n.Dir < 0

	// Primary modifier chords (⌘ on macOS, Ctrl on Windows/Linux).
	if isMMPrimaryMod(mods) {
		switch key {
		case ui.KeyZ:
			st.opUndo(cb)
			return true
		case ui.KeyY:
			st.opRedo(cb)
			return true
		case ui.KeyA:
			st.opSelectAll()
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
		case ui.KeyE:
			if !cb.ReadOnly {
				st.beginEdit(n, false)
				return true
			}
		case ui.KeyF:
			st.OpenFind()
			return true
		case ui.KeyG:
			if theme.IsMacOS() && st.FindOpen {
				st.stepFind(1)
				return true
			}
		case ui.KeyL:
			if !cb.ReadOnly {
				st.beginLinkEdit(n)
				return true
			}
		case ui.KeyEnter:
			if cb.ReadOnly {
				// Read-only (e.g. pending plan review): let ⌘↵ reach the host.
				return false
			}
			st.opToggleTaskDoneOrCreate(n, cb)
			return true
		case ui.KeySlash, ui.KeyPeriod:
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
	if isMMPrimaryShiftMod(mods) {
		switch key {
		case ui.KeyZ:
			st.opRedo(cb)
			return true
		case ui.KeyA:
			st.opSelectSiblings()
			return true
		case ui.KeyG:
			if theme.IsMacOS() && st.FindOpen {
				st.stepFind(-1)
				return true
			}
		case ui.Key0:
			st.ResetZoom()
			return true
		case ui.KeyEqual:
			st.zoomAt(st.LastCanvasW*0.5, st.LastCanvasH*0.5, st.Camera.Scale*1.15)
			return true
		case ui.KeyMinus:
			st.zoomAt(st.LastCanvasW*0.5, st.LastCanvasH*0.5, st.Camera.Scale/1.15)
			return true
		case ui.KeyL:
			st.ToggleLayout()
			return true
		case ui.KeyPeriod:
			st.opToggleExpandCollapseAll()
			return true
		}
		return false
	}
	if isMMPrimaryAltMod(mods) {
		switch key {
		case ui.KeyA:
			st.opSelectSubtree()
			return true
		case ui.Key0:
			st.ResetZoom()
			return true
		case ui.KeyT:
			st.opToggleTask(n, cb)
			return true
		case ui.KeySlash:
			st.opToggleExpandCollapseAll()
			return true
		}
		return false
	}

	// Shift shortcuts: Shift+Enter (sibling above), Shift+Tab (insert parent), Shift+Backspace (delete single), Shift+Up/Down (multi-select siblings)
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
		case ui.KeyUp:
			st.opExtendSiblingSelection(-1)
			return true
		case ui.KeyDown:
			st.opExtendSiblingSelection(1)
			return true
		case ui.KeyF3:
			if !theme.IsMacOS() && st.FindOpen {
				st.stepFind(-1)
				return true
			}
		}
		return false
	}

	// Alt shortcuts: reorder (Up/Down), promote/demote (Left/Right), delete single node (Backspace), open link (Alt+Enter)
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
		case ui.KeyEnter:
			if strings.TrimSpace(n.Link) != "" {
				st.openNodeLink(c.Now(), n, cb)
				return true
			}
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
	case ui.KeyF2:
		if !cb.ReadOnly {
			st.beginEdit(n, false)
			return true
		}
	case ui.KeyF3:
		if !theme.IsMacOS() && st.FindOpen {
			st.stepFind(1)
			return true
		}
	case ui.KeyHome:
		st.opNavigate("home")
		return true
	case ui.KeyEnd:
		st.opNavigate("end")
		return true
	case ui.KeyPageUp:
		if st.Camera.Scale > 0 {
			st.fsCamAnimActive = false
			st.Camera.Y -= max(st.LastCanvasH*0.65, 120) / st.Camera.Scale
			return true
		}
	case ui.KeyPageDown:
		if st.Camera.Scale > 0 {
			st.fsCamAnimActive = false
			st.Camera.Y += max(st.LastCanvasH*0.65, 120) / st.Camera.Scale
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
	bar := st.keepCanvasButtonFocus(c, ui.Row(c.Key("mm-sel-bar-"+id)).Absolute().Left(theme.Space10).Bottom(theme.Space10).
		Height(theme.HeightChrome).PaddingX(theme.Space2).AlignItems(ui.Center).Gap(theme.Space2).
		Radius(theme.RadiusPill).Background(pal.Menu).Border(1, pal.MenuBorder))
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
			st.keepCanvasButtonFocus(c, btn)
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
				st.focusCanvas(c, cb)
			})
			if sel.Task != nil {
				doneLbl := theme.Tr("完成", "Done")
				if *sel.Task {
					doneLbl = theme.Tr("未完", "Undone")
				}
				renderPillBtn("mm-qb-done-"+id, icons.Check, doneLbl, *sel.Task, false, func() {
					st.opFlipTaskDone(sel, cb)
					st.focusCanvas(c, cb)
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

func (st *BlockState) renderZoomHUD(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	hud := st.keepCanvasButtonFocus(c, ui.Row(c.Key("mm-zoom-hud-"+id)).Absolute().Right(theme.Space10).Bottom(theme.Space10).
		Height(theme.HeightChrome).PaddingX(theme.Space4).AlignItems(ui.Center).Gap(theme.Space2).
		Radius(theme.RadiusPill).Background(pal.Menu).Border(1, pal.MenuBorder))
	bui.Elevate(hud, theme.ElevationPopover)
	hud.Children(func() {
		anyFolded := st.hasAnyFolded()
		foldIcon := icons.FoldAll
		foldTip := theme.Tr("折叠所有子分支", "Collapse all branches")
		if anyFolded {
			foldIcon = icons.UnfoldAll
			foldTip = theme.Tr("展开所有分支", "Expand all branches")
		}
		foldBtn := st.keepCanvasButtonFocus(c, bui.IconButton(c, foldIcon, bui.IconButtonOptions{
			ID:       "mm-hud-fold-" + id,
			Label:    foldTip,
			Tooltip:  foldTip,
			Shortcut: mmShortcutLabel("⌘⌥/", "Ctrl+Alt+/"),
			Size:     theme.HeightMicro + theme.Space4,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusPill,
		}))
		if foldBtn.Clicked() {
			st.opToggleExpandCollapseAll()
			st.focusCanvas(c, cb)
		}

		ui.Box(c).Size(1, theme.Space12).Background(pal.Hairline).Shrink(0)

		outBtn := st.keepCanvasButtonFocus(c, bui.IconButton(c, icons.TbMinus, bui.IconButtonOptions{
			ID:       "mm-hud-zout-" + id,
			Label:    theme.Tr("缩小", "Zoom out"),
			Tooltip:  theme.Tr("缩小", "Zoom out"),
			Shortcut: mmShortcutLabel("⌘-", "Ctrl+-"),
			Size:     theme.HeightMicro + theme.Space4,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusPill,
		}))
		if outBtn.Clicked() {
			st.ZoomBy(1.0 / 1.15)
			st.focusCanvas(c, cb)
		}

		scalePct := int(math.Round(float64(st.Camera.Scale * 100)))
		if scalePct <= 0 {
			scalePct = 100
		}
		pctBtn := st.keepCanvasButtonFocus(c, ui.ButtonBase(c.Key("mm-hud-zpct-"+id)).
			Height(theme.HeightMicro + theme.Space4).MinWidth(42).PaddingX(theme.Space4).
			Radius(theme.RadiusPill).Center().Cursor(ui.CursorPointer).Shrink(0).
			Label(theme.Tr("适配画布", "Fit to view")))
		if pctBtn.Hovered() || pctBtn.Pressed() {
			pctBtn.Background(pal.Hover)
		}
		bui.TooltipShortcut(c, pctBtn,
			theme.Tr("点击适配画布（"+mmShortcutLabel("⌘⇧0", "Ctrl+Shift+0")+" 还原 100%）", "Click to fit ("+mmShortcutLabel("⌘⇧0", "Ctrl+Shift+0")+" 100%)"),
			mmShortcutLabel("⌘0", "Ctrl+0"))
		pctBtn.Children(func() {
			ui.Text(c, fmt.Sprintf("%d%%", scalePct)).
				FontSize(theme.FontMeta).FontWeight(theme.WeightMono).TextColor(pal.Text).SingleLine()
		})
		if pctBtn.Clicked() {
			st.FitCanvas()
			st.focusCanvas(c, cb)
		}

		inBtn := st.keepCanvasButtonFocus(c, bui.IconButton(c, icons.TbPlus, bui.IconButtonOptions{
			ID:       "mm-hud-zin-" + id,
			Label:    theme.Tr("放大", "Zoom in"),
			Tooltip:  theme.Tr("放大", "Zoom in"),
			Shortcut: mmShortcutLabel("⌘+", "Ctrl+="),
			Size:     theme.HeightMicro + theme.Space4,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusPill,
		}))
		if inBtn.Clicked() {
			st.ZoomBy(1.15)
			st.focusCanvas(c, cb)
		}
	})
}

func (st *BlockState) renderFindBar(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	bar := ui.Row(c.Key("mm-find-bar-"+id)).Absolute().Top(theme.Space8).Right(theme.Space10).
		Height(theme.HeightControl + theme.Space4).PaddingX(theme.Space6).AlignItems(ui.Center).Gap(theme.Space4).
		Radius(theme.RadiusLG).Background(pal.Menu).Border(1, pal.MenuBorder).
		Bind(&st.FindBarHandle)
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
			inp.Focus(ui.FocusOptions{PreventScroll: true})
			st.FindHandle.SelectAll(c)
		}
		liveFindComposing := inp.Composing()
		st.FindComposing = liveFindComposing
		inp.ObserveInput(func(ev ui.InputEvent) {
			switch ev.Kind {
			case ui.InputCompose:
				liveFindComposing = ev.Text != ""
				st.FindComposing = liveFindComposing
			case ui.InputText, ui.InputCommand:
				liveFindComposing = false
				st.FindComposing = false
			}
		})
		inp.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown || ev.Key == ui.KeyUnknown || liveFindComposing || st.FindComposing {
				return false
			}
			switch ev.Key {
			case ui.KeyTab:
				if ev.Mods == 0 || ev.Mods == ui.Shift {
					return true
				}
			case ui.KeyEscape:
				if ev.Mods == 0 {
					st.CloseFind()
					st.restoreCanvasFocusAfterEdit(cb)
					return true
				}
			case ui.KeyEnter:
				if (ev.Mods & ui.Shift) != 0 {
					st.stepFind(-1)
				} else if ev.Mods == 0 {
					st.stepFind(1)
				} else {
					return false
				}
				return true
			case ui.KeyDown:
				if ev.Mods == 0 {
					st.stepFind(1)
					return true
				}
			case ui.KeyUp:
				if ev.Mods == 0 {
					st.stepFind(-1)
					return true
				}
			case ui.KeyG:
				if theme.IsMacOS() && isMMPrimaryShiftMod(ev.Mods) {
					st.stepFind(-1)
					return true
				}
				if theme.IsMacOS() && isMMPrimaryMod(ev.Mods) {
					st.stepFind(1)
					return true
				}
			case ui.KeyF3:
				if !theme.IsMacOS() {
					if ev.Mods == ui.Shift {
						st.stepFind(-1)
						return true
					}
					if ev.Mods == 0 {
						st.stepFind(1)
						return true
					}
				}
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
			Tooltip:  theme.Tr("上一个", "Previous match"),
			Shortcut: mmShortcutLabel("⌘⇧G", "Shift+F3"),
			Size:     theme.HeightMicro + theme.Space2,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusSM,
			Disabled: !hasMatches,
		}).KeepFocus()
		if prevBtn.Clicked() && hasMatches {
			st.stepFind(-1)
		}
		nextBtn := bui.IconButton(c, icons.ChevronDown, bui.IconButtonOptions{
			ID:       "mm-find-next-" + id,
			Label:    theme.Tr("下一个", "Next match"),
			Tooltip:  theme.Tr("下一个", "Next match"),
			Shortcut: mmShortcutLabel("⌘G", "F3"),
			Size:     theme.HeightMicro + theme.Space2,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusSM,
			Disabled: !hasMatches,
		}).KeepFocus()
		if nextBtn.Clicked() && hasMatches {
			st.stepFind(1)
		}
		closeBtn := bui.IconButton(c, icons.Close, bui.IconButtonOptions{
			ID:       "mm-find-close-" + id,
			Label:    theme.Tr("关闭查找", "Close find"),
			Shortcut: "Esc",
			Tooltip:  theme.Tr("关闭查找", "Close find"),
			Size:     theme.HeightMicro + theme.Space2,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusSM,
		})
		if closeBtn.Clicked() {
			st.CloseFind()
			st.focusCanvas(c, cb)
		}
	})
	if !st.FindComposing && bar.OverlayShortcut(0, ui.KeyEscape) {
		st.CloseFind()
		st.restoreCanvasFocusAfterEdit(cb)
	}
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
		fsRestore := !fullscreen && st.consumeFullscreenFocusRestore()
		reqFocus, reqPreventScroll := st.consumeViewFocus()
		if fsRestore || reqFocus {
			if fsRestore || reqPreventScroll {
				ta.Focus(ui.FocusOptions{PreventScroll: true})
			} else {
				ta.Focus()
			}
			if fsRestore && cb.OnRequestFocus != nil {
				cb.OnRequestFocus()
			}
		}
		liveSourceComposing := ta.Composing()
		st.SourceComposing = liveSourceComposing
		ta.ObserveInput(func(ev ui.InputEvent) {
			switch ev.Kind {
			case ui.InputCompose:
				liveSourceComposing = ev.Text != ""
				st.SourceComposing = liveSourceComposing
			case ui.InputText, ui.InputCommand:
				liveSourceComposing = false
				st.SourceComposing = false
			}
		})
		ta.OnTextKey(func(k ui.TextKey) bool {
			if k.Key == ui.KeyUnknown || liveSourceComposing {
				return false
			}
			flushSource := func() {
				if cb.ReadOnly {
					return
				}
				live := k.Text()
				if live != st.LastSource {
					st.SyncSource(live, Config{})
					if cb.OnChange != nil {
						cb.OnChange(live)
					}
				}
			}
			if cb.OnHostNavKey != nil && cb.OnHostNavKey(k.Key, k.Mods) {
				flushSource()
				return true
			}
			if st.captureBlockTextKey(k) {
				flushSource()
				return true
			}
			return false
		})
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

func (st *BlockState) restoreFocusAfterPopover(c *ui.Context, cb Callbacks) {
	if st.EditingID != 0 {
		if el := c.Resolve(st.EditHandle); el.Valid() {
			el.Focus(ui.FocusOptions{PreventScroll: true})
		} else {
			st.EditHandle.Focus(c)
		}
		return
	}
	st.focusCanvas(c, cb)
}

func (st *BlockState) renderExportMenu(c *ui.Context, id string, fixedHeight float32, cb Callbacks) {
	pal := theme.Ocean()
	// Backdrop to close on outside click or Escape.
	scrim := ui.Box(c.Key("mm-exp-scrim-"+id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(ui.Transparent)
	if scrim.Clicked() || scrim.OverlayShortcut(0, ui.KeyEscape) {
		st.ExportMenuOpen = false
		st.restoreFocusAfterPopover(c, cb)
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
						btn := st.keepCanvasButtonFocus(c, ui.ButtonBase(c.Key("mm-exp-th-"+id+"-"+string(o.mode))).
							Grow(1).Shrink(1).MinWidth(0).Height(theme.HeightCompact).
							Radius(theme.RadiusSM).Center().Cursor(ui.CursorPointer).Label(o.label))
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
							st.focusCanvas(c, cb)
						}
					}
				})

				ui.Box(c).FillWidth().Height(1).Margin(theme.Space4, theme.Space2).Background(pal.MenuSeparator)

				// Quick clipboard copy actions.
				renderClipRow := func(key string, icon icons.Icon, label string, onClick func()) {
					row := st.keepCanvasButtonFocus(c, ui.ButtonBase(c.Key(key)).
						FillWidth().Height(theme.MenuItemHeight).PaddingX(theme.Space8).Radius(theme.RadiusSM).
						AlignItems(ui.Center).Justify(ui.Start).Gap(theme.Space8).Cursor(ui.CursorPointer).Label(label))
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
						st.focusCanvas(c, cb)
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
					row := st.keepCanvasButtonFocus(c, ui.ButtonBase(c.Key("mm-exp-item-"+id+"-"+ext)).
						FillWidth().Height(theme.MenuItemHeight).PaddingX(theme.Space8).Radius(theme.RadiusSM).
						AlignItems(ui.Center).Justify(ui.SpaceBetween).Cursor(ui.CursorPointer).Label(label))
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
						st.focusCanvas(c, cb)
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
					impRow := st.keepCanvasButtonFocus(c, ui.ButtonBase(c.Key("mm-exp-imp-"+id)).
						FillWidth().Height(theme.MenuItemHeight).PaddingX(theme.Space8).Radius(theme.RadiusSM).
						AlignItems(ui.Center).Justify(ui.Start).Gap(theme.Space8).Cursor(ui.CursorPointer).Label(theme.Tr("导入文件", "Import File")))
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
						st.focusCanvas(c, cb)
					}
				}
			})
		})
	})
}

// --- Link-to-Note Popover (Anchored to Selected Node with Keyboard Navigation) ---

func (st *BlockState) renderLinkPopover(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	closeLink := func() {
		st.LinkNodeID = 0
		st.LinkBrokenNotice = false
		st.LinkFocusPending = false
		st.LinkComposing = false
		st.LinkHandle.CancelFocus()
		st.focusCanvas(c, cb)
	}
	scrim := ui.Box(c.Key("mm-link-scrim-"+id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(ui.Transparent)
	if scrim.Clicked() || (!st.LinkComposing && scrim.OverlayShortcut(0, ui.KeyEscape)) {
		closeLink()
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
		Radius(theme.RadiusLG).Background(pal.Menu).Border(1, pal.MenuBorder).
		Bind(&st.LinkPanelHandle)

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
			inp.Focus(ui.FocusOptions{PreventScroll: true})
			st.LinkHandle.Focus(c)
		}
		liveLinkComposing := inp.Composing()
		st.LinkComposing = liveLinkComposing
		inp.ObserveInput(func(ev ui.InputEvent) {
			switch ev.Kind {
			case ui.InputCompose:
				liveLinkComposing = ev.Text != ""
				st.LinkComposing = liveLinkComposing
			case ui.InputText, ui.InputCommand:
				liveLinkComposing = false
				st.LinkComposing = false
			}
		})
		inp.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown || ev.Key == ui.KeyUnknown || liveLinkComposing || st.LinkComposing {
				return false
			}
			switch ev.Key {
			case ui.KeyTab:
				if ev.Mods == 0 || ev.Mods == ui.Shift {
					return true
				}
			case ui.KeyDown:
				if ev.Mods == 0 && len(filtered) > 0 {
					st.LinkActiveIdx = (st.LinkActiveIdx + 1) % len(filtered)
					ensureCandidateVisible(st.LinkActiveIdx)
					return true
				}
			case ui.KeyUp:
				if ev.Mods == 0 && len(filtered) > 0 {
					if st.LinkActiveIdx <= 0 {
						st.LinkActiveIdx = len(filtered) - 1
					} else {
						st.LinkActiveIdx--
					}
					ensureCandidateVisible(st.LinkActiveIdx)
					return true
				}
			case ui.KeyEnter:
				if ev.Mods == 0 {
					if st.LinkActiveIdx >= 0 && st.LinkActiveIdx < len(filtered) {
						st.applyLink(filtered[st.LinkActiveIdx].RelPath, cb)
					} else {
						st.applyLink(st.LinkDraft, cb)
					}
					st.focusCanvas(c, cb)
					return true
				}
			case ui.KeyEscape:
				if ev.Mods == 0 {
					closeLink()
					return true
				}
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
							AlignItems(ui.Center).Justify(ui.Start).Gap(theme.Space8).Cursor(ui.CursorPointer).Label(cand.Title)
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
							st.focusCanvas(c, cb)
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
						st.focusCanvas(c, cb)
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
					closeLink()
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
					st.focusCanvas(c, cb)
				}
			})
		})
	})
}

// --- Grouped Shortcut Cheat-Sheet Popover ---

func (st *BlockState) renderShortcutsPopover(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	scrim := ui.Box(c.Key("mm-sc-scrim-"+id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(ui.Transparent)
	if scrim.Clicked() || scrim.OverlayShortcut(0, ui.KeyEscape) {
		st.ShortcutsOpen = false
		st.restoreFocusAfterPopover(c, cb)
		return
	}

	k := mmShortcutLabel
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
			title: theme.Tr("视图与全局", "Views & Window"),
			items: []scEntry{
				{[]string{k("⌘1/2/3", "Ctrl+1/2/3")}, theme.Tr("切换导图 / 大纲 / 源码", "Map / Outline / Source")},
				{[]string{k("⌘⇧↵", "Ctrl+Shift+Enter")}, theme.Tr("进入 / 退出全屏", "Toggle fullscreen")},
				{[]string{k("⌘⇧L", "Ctrl+Shift+L")}, theme.Tr("切换左右 / 平衡布局", "Toggle layout")},
				{[]string{k("⌘⇧/", "Ctrl+Shift+/")}, theme.Tr("打开 / 关闭快捷键速查", "Toggle shortcuts")},
				{[]string{"Esc"}, theme.Tr("取消 / 关闭弹窗 / 退全屏", "Cancel / close / exit fullscreen")},
			},
		},
		{
			title: theme.Tr("建树与编辑", "Tree & Editing"),
			items: []scEntry{
				{[]string{k("Tab", "Tab / Ins")}, theme.Tr("添加子节点", "Add child")},
				{[]string{k("↵", "Enter")}, theme.Tr("下方同级节点", "Add sibling below")},
				{[]string{k("⇧↵", "Shift+Enter")}, theme.Tr("上方同级节点", "Add sibling above")},
				{[]string{k("⇧Tab", "Shift+Tab")}, theme.Tr("插入父节点", "Insert parent")},
				{[]string{"F2", k("⌘E", "Ctrl+E")}, theme.Tr("重命名 / 编辑节点", "Rename / edit topic")},
				{[]string{k("⇧↵", "Shift+Enter"), k("⌥↵", "Alt+Enter")}, theme.Tr("编辑态内换行", "Newline while editing")},
				{[]string{k("⌫", "Del")}, theme.Tr("删除所选分支", "Delete branch")},
				{[]string{k("⇧⌫", "Shift+Del"), k("⌥⌫", "Alt+Del")}, theme.Tr("仅删节点（保留子级）", "Delete single node")},
			},
		},
		{
			title: theme.Tr("层级、选中与历史", "Structure, Selection & History"),
			items: []scEntry{
				{[]string{k("⌥↑↓", "Alt+↑↓")}, theme.Tr("同级上下移动", "Move up / down")},
				{[]string{k("⌥←→", "Alt+←→")}, theme.Tr("升级 / 降级节点", "Promote / demote")},
				{[]string{k("⌘A", "Ctrl+A")}, theme.Tr("全选可见节点", "Select all nodes")},
				{[]string{k("⌘⇧A", "Ctrl+Shift+A")}, theme.Tr("选中同级节点", "Select siblings")},
				{[]string{k("⌘⌥A", "Ctrl+Alt+A")}, theme.Tr("选中当前分支", "Select subtree")},
				{[]string{k("⌘C / X / V", "Ctrl+C / X / V")}, theme.Tr("复制 / 剪切 / 粘贴分支", "Copy / cut / paste")},
				{[]string{k("⌘D", "Ctrl+D")}, theme.Tr("创建分支副本", "Duplicate branch")},
				{[]string{k("⌘Z", "Ctrl+Z")}, theme.Tr("撤销", "Undo")},
				{[]string{k("⌘⇧Z", "Ctrl+Y / Ctrl+Shift+Z")}, theme.Tr("重做", "Redo")},
			},
		},
		{
			title: theme.Tr("任务、链接、查找与画布", "Tasks, Links, Find & Canvas"),
			items: []scEntry{
				{[]string{k("⌘⌥T", "Ctrl+Alt+T")}, theme.Tr("设为 / 取消任务", "Toggle task")},
				{[]string{k("⌘↵", "Ctrl+Enter")}, theme.Tr("切换任务完成", "Toggle done")},
				{[]string{k("⌘L", "Ctrl+L")}, theme.Tr("编辑节点链接", "Edit link")},
				{[]string{k("⌥↵", "Alt+Enter")}, theme.Tr("打开节点链接", "Open link")},
				{[]string{theme.Tr("空格", "Space"), k("⌘.", "Ctrl+.")}, theme.Tr("折叠 / 展开分支", "Fold / unfold branch")},
				{[]string{k("⌘⌥/", "Ctrl+Alt+/"), k("⌘⇧.", "Ctrl+Shift+.")}, theme.Tr("全部折叠 / 展开", "Fold / unfold all")},
				{[]string{k("⌘F", "Ctrl+F")}, theme.Tr("画布内查找", "Find in canvas")},
				{[]string{k("⌘G / ⌘⇧G", "F3 / Shift+F3")}, theme.Tr("查找下一个 / 上一个", "Next / previous match")},
				{[]string{k("⌘+ / ⌘-", "Ctrl+= / Ctrl+-")}, theme.Tr("放大 / 缩小画布", "Zoom in / out")},
				{[]string{k("⌘0", "Ctrl+0")}, theme.Tr("适配画布", "Fit to view")},
				{[]string{k("⌘⇧0", "Ctrl+Shift+0")}, theme.Tr("还原 100% 缩放", "Reset zoom 100%")},
				{[]string{"Home / End"}, theme.Tr("跳到中心主题 / 末梢节点", "Central / last topic")},
				{[]string{"PageUp / PageDown"}, theme.Tr("纵向翻页平移画布", "Page canvas up / down")},
				{[]string{theme.Tr("拖动空白", "Drag blank")}, theme.Tr("框选节点", "Box select")},
				{[]string{k("⇧/⌘+单击", "Shift/Ctrl+Click")}, theme.Tr("加选 / 减选节点", "Toggle selection")},
				{[]string{theme.Tr("空格+拖动", "Space+Drag")}, theme.Tr("平移画布", "Pan canvas")},
			},
		},
	}

	availH := st.LastCanvasH
	if availH <= 0 {
		availH = 310
	}
	maxPopH := min(float32(380), max(float32(180), availH-theme.Space16))
	panel := ui.Column(c.Key("mm-sc-pop-"+id)).Absolute().Right(theme.Space10).Bottom(theme.Space8).
		Width(356).MaxHeight(maxPopH).Padding(theme.Space12).Gap(theme.Space8).
		Radius(theme.RadiusLG).Background(pal.Menu).Border(1, pal.MenuBorder)
	bui.Elevate(panel, theme.ElevationPopover)
	panel.Children(func() {
		hdr := ui.Row(c.Key("mm-sc-hdr-" + id)).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Shrink(0)
		hdr.Children(func() {
			ui.Text(c, theme.Tr("快捷键速查", "Keyboard Shortcuts")).
				FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()
			hdrRight := ui.Row(c.Key("mm-sc-hdr-r-" + id)).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0)
			hdrRight.Children(func() {
				tutLink := ui.ButtonBase(c.Key("mm-sc-tut-"+id)).
					Height(theme.HeightMicro+theme.Space2).PaddingX(theme.Space6).Radius(theme.RadiusSM).
					AlignItems(ui.Center).Gap(theme.Space4).Cursor(ui.CursorPointer).Shrink(0).
					Label(theme.Tr("动画教程", "Guide")).KeepFocus()
				tutHot := tutLink.Hovered() || tutLink.Pressed()
				tutFG := pal.Accent
				if tutHot {
					tutLink.Background(pal.SelectionBG)
				}
				tutLink.Children(func() {
					icons.DrawAnimated(c, icons.Video, theme.IconMicro, pal.MonoIconColors(tutFG), tutHot)
					ui.Text(c, theme.Tr("动画教程", "Guide")).
						FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(tutFG).SingleLine()
				})
				if tutLink.Clicked() {
					st.OpenTutorial(false)
					c.Invalidate()
				}

				closeBtn := st.keepCanvasButtonFocus(c, bui.IconButton(c, icons.Close, bui.IconButtonOptions{
					ID:       "mm-sc-close-" + id,
					Label:    theme.Tr("关闭", "Close"),
					Size:     theme.HeightMicro + theme.Space2,
					IconSize: theme.IconMicro,
					Radius:   theme.RadiusSM,
				}))
				if closeBtn.Clicked() {
					st.ShortcutsOpen = false
					st.restoreFocusAfterPopover(c, cb)
				}
			})
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
	if card.OverlayShortcut(0, ui.KeyEscape) {
		st.ImportResult = nil
		st.ImportUndoBefore = ""
		st.ImportUndoAfter = ""
		st.restoreFocusAfterPopover(c, cb)
		return
	}
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
				undoBtn := st.keepCanvasButtonFocus(c, ui.ButtonBase(c.Key("mm-imp-undo-"+id)).Grow(1).Shrink(1).MinWidth(0).Height(theme.HeightCompact).
					Radius(theme.RadiusMD).Border(1, pal.BtnBorder).Background(pal.Paper).Center().Cursor(ui.CursorPointer).
					Label(theme.Tr("撤销导入", "Undo Import")))
				if undoBtn.Hovered() || undoBtn.Pressed() {
					undoBtn.Background(pal.Hover)
				}
				undoBtn.Children(func() {
					ui.Text(c, theme.Tr("撤销导入", "Undo Import")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
						TextColor(pal.TextSecondary).SingleLine()
				})
				if undoBtn.Clicked() {
					st.opUndoImport(cb)
					st.focusCanvas(c, cb)
					c.Invalidate()
				}
			}

			okBtn := st.keepCanvasButtonFocus(c, ui.ButtonBase(c.Key("mm-imp-ok-"+id)).Grow(1).Shrink(1).MinWidth(0).Height(theme.HeightCompact).
				Radius(theme.RadiusMD).Border(1, pal.BtnBorder).Background(pal.Paper).Center().Cursor(ui.CursorPointer).
				Label(theme.Tr("知道了", "Got it")))
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
				st.focusCanvas(c, cb)
			}
		})
	})
}

// --- Bottom Shortcut Hint Bar (Single-Row Horizontal Scroll + Direct Mouse Wheel Scroll) ---

func (st *BlockState) renderHintBar(c *ui.Context, id string, cb Callbacks) {
	pal := theme.Ocean()
	bar := ui.Row(c.Key("mm-hint-bar-" + id)).FillWidth().Height(theme.HeightControl).Shrink(0).
		Background(pal.HintBand).AlignItems(ui.Center).Clip()

	// Convert vertical mouse wheel directly into horizontal scroll over the bottom hint bar
	// whenever the strip overflows horizontally (no line wrapping!), and keep canvas focused on click.
	handleHintInput := func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll || st.HintScroll.MaxX <= 0 {
			return false
		}
		if math.Abs(float64(ev.DY)) >= math.Abs(float64(ev.DX)) && math.Abs(float64(ev.DY)) >= 0.1 {
			st.HintScroll.X = clampFloat32(st.HintScroll.X+ev.DY, 0, st.HintScroll.MaxX)
			return true
		}
		return false
	}
	bar.HandleInput(handleHintInput)

	bar.Children(func() {
		strip := ui.ScrollHorizontal(c.Key("mm-hint-scroll-" + id)).
			Grow(1).Shrink(1).MinWidth(0).FillHeight().
			AlignItems(ui.Center).PaddingX(theme.Space10).Gap(theme.Space10).
			Scrollbars(false).TrackScroll(&st.HintScroll)
		// Only the scrolling hint strip hands focus back to the canvas on click;
		// the pinned Shortcuts button must still receive its own pointer events.
		strip.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputPointerDown && ev.Button == 0 && st.View == ViewMap && st.EditingID == 0 && st.LinkNodeID == 0 {
				st.focusCanvas(c, cb)
				return true
			}
			return handleHintInput(ev)
		})
		// Stepped edge fades on the scrolling strip only, so the pinned Shortcuts button stays crisp.
		strip.DrawOver(func(p *ui.Painter, r ui.Rect) {
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
		strip.Children(func() {
			type hintItem struct {
				keys      []string
				label     string
				highlight bool
				divider   bool
			}
			k := mmShortcutLabel
			var items []hintItem
			switch {
			case st.View == ViewOutline:
				items = []hintItem{
					{keys: []string{k("↵", "Enter")}, label: theme.Tr("新行", "New")},
					{keys: []string{k("⇧↵", "Shift+Enter"), k("⌥↵", "Alt+Enter")}, label: theme.Tr("换行", "Break")},
					{keys: []string{"Tab", k("⇧Tab", "Shift+Tab")}, label: theme.Tr("缩进", "Indent")},
					{keys: []string{k("⌥↑↓", "Alt+↑↓")}, label: theme.Tr("移动", "Move")},
					{keys: []string{k("⌘↵", "Ctrl+Enter")}, label: theme.Tr("待办", "Todo")},
					{keys: []string{k("⌘.", "Ctrl+.")}, label: theme.Tr("折叠", "Fold")},
					{keys: []string{k("⌘D", "Ctrl+D")}, label: theme.Tr("副本", "Dup")},
					{keys: []string{k("⌘L", "Ctrl+L")}, label: theme.Tr("链接", "Link")},
				}
				if st.OutlineZoomID != 0 {
					items = append(items, hintItem{keys: []string{"Esc"}, label: theme.Tr("返回上级", "Zoom out")})
				} else {
					items = append(items, hintItem{label: theme.Tr("· 点圆点聚焦", "· Click dot to zoom")})
				}
			case st.EditingID != 0:
				items = []hintItem{
					{keys: []string{k("⇧↵", "Shift+Enter")}, label: theme.Tr("换行", "Newline"), highlight: true, divider: true},
					{keys: []string{"Tab"}, label: theme.Tr("子节点", "Child")},
					{keys: []string{k("↵", "Enter")}, label: theme.Tr("完成", "Done")},
				}
			default:
				items = []hintItem{
					{keys: []string{theme.Tr("空格+拖动", "Space+Drag")}, label: theme.Tr("平移画布", "Pan canvas"), highlight: true, divider: true},
				}
				if !cb.ReadOnly {
					items = append(items,
						hintItem{keys: []string{"Tab"}, label: theme.Tr("子节点", "Child")},
						hintItem{keys: []string{k("⌥↑↓←→", "Alt+↑↓←→")}, label: theme.Tr("移动", "Move")},
					)
				}
				items = append(items,
					hintItem{keys: []string{theme.Tr("空格", "Space")}, label: theme.Tr("折叠", "Fold")},
				)
			}
			for i, it := range items {
				itemRow := ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0)
				itemRow.Children(func() {
					for _, key := range it.keys {
						if it.highlight {
							bui.KeycapWithOptions(c, key, bui.KeycapOptions{Accent: true})
						} else {
							bui.Keycap(c, key)
						}
					}
					lblCol := pal.TextSecondary
					lblWeight := theme.WeightRegular
					if it.highlight {
						lblCol = pal.Text
						lblWeight = theme.WeightControl
					}
					ui.Text(c, it.label).FontSize(theme.FontMeta).FontWeight(lblWeight).
						TextColor(lblCol).SingleLine().Shrink(0)
				})
				if it.divider && i < len(items)-1 {
					ui.Box(c).Size(1, theme.Space12).Background(pal.Hairline).Shrink(0)
				}
			}
		})

		// Pinned right-side Tutorial & Shortcuts buttons.
		ui.Box(c).Size(1, theme.Space12).Background(pal.Hairline).Shrink(0)
		unseen := st.lastCfgSet && !st.lastCfg.TutorialSeen
		tutBtn := ui.ButtonBase(c.Key("mm-tut-btn-"+id)).
			Height(theme.HeightCompact).PaddingX(theme.Space10).Margin(0, theme.Space4, 0, theme.Space4).
			Radius(theme.RadiusMD).AlignItems(ui.Center).Gap(theme.Space4).
			Cursor(ui.CursorPointer).Shrink(0).Label(theme.Tr("教程", "Guide")).KeepFocus()
		bui.Tooltip(c, tutBtn, theme.Tr("观看快捷键动画教程", "Watch animated shortcut guide"))
		tutHot := tutBtn.Hovered() || tutBtn.Pressed()
		tutFG := pal.TextSecondary
		tutBG := ui.Transparent
		tutBD := ui.Transparent
		if st.TutorialOpen {
			tutBG = pal.Accent
			tutFG = pal.Paper
			tutBD = pal.Accent
		} else if unseen {
			tutBG = pal.SelectionBG
			tutFG = pal.Accent
			tutBD = pal.Accent.Alpha(0.35)
			if tutHot {
				tutFG = pal.AccentHover
				tutBD = pal.Accent
			}
		} else if tutHot {
			tutBG = pal.Hover
			tutFG = pal.Text
		}
		tutBtn.Background(tutBG).Border(1, tutBD)
		tutBtn.Children(func() {
			icons.DrawAnimated(c, icons.Video, theme.IconMicro, pal.MonoIconColors(tutFG), st.TutorialOpen || tutHot || unseen)
			ui.Text(c, theme.Tr("教程", "Guide")).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).
				TextColor(tutFG).SingleLine()
		})
		if tutBtn.Clicked() {
			if st.TutorialOpen {
				st.CloseTutorial(cb)
			} else {
				st.OpenTutorial(unseen)
			}
			c.Invalidate()
		}

		ui.Box(c).Size(1, theme.Space12).Background(pal.Hairline).Shrink(0)
		scBtn := ui.ButtonBase(c.Key("mm-sc-btn-"+id)).
			Height(theme.HeightControl).PaddingX(theme.Space8).AlignItems(ui.Center).Gap(theme.Space4).
			Cursor(ui.CursorPointer).Shrink(0).Label(theme.Tr("快捷键", "Shortcuts")).KeepFocus()
		bui.TooltipShortcut(c, scBtn, theme.Tr("快捷键速查", "Keyboard shortcuts"), mmShortcutLabel("⌘⇧/", "Ctrl+Shift+/"))
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
			if st.EditingID != 0 {
				if el := c.Resolve(st.EditHandle); el.Valid() {
					el.Focus(ui.FocusOptions{PreventScroll: true})
				} else {
					st.EditHandle.Focus(c)
				}
			} else {
				st.focusCanvas(c, cb)
			}
		}
	})

	bar.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Line(r.X, r.Y+0.5, r.X+r.W, r.Y+0.5, 1, pal.Hairline)
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
