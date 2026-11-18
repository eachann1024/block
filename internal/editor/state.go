package editor

import (
	"context"
	"slices"
	"strings"
	"time"

	"block/internal/files"
	"block/internal/model"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// Position identifies a rune offset within a block by block index.
type Position struct {
	Block  int
	Offset int
}

type blockMenuMode uint8

const (
	menuSlash blockMenuMode = iota
	menuHandle
)

type blockCmdKind uint8

const (
	cmdTurnParagraph blockCmdKind = iota
	cmdTurnH1
	cmdTurnH2
	cmdTurnH3
	cmdTurnBullet
	cmdTurnNumbered
	cmdTurnTodo
	cmdTurnQuote
	cmdCallout
	cmdCode
	cmdCodeMermaid
	cmdCodeMath
	cmdTable
	cmdDivider
	cmdImage
	cmdAI
	cmdDuplicate
	cmdMoveUp
	cmdMoveDown
	cmdDelete
)

type blockMenuItem struct {
	Kind      blockCmdKind
	Label     string
	Keywords  string
	Hint      string
	Danger    bool
	Separator bool
}

type blockMenuState struct {
	BlockIdx      int
	Mode          blockMenuMode
	Filter        string
	ActiveIndex   int
	PointerActive bool
	Scroll        ui.ScrollState
	TriggerRect   ui.Rect
}

type findMatch struct {
	BlockIdx  int
	StartRune int
	EndRune   int
}

type undoSnapshot struct {
	Doc         *model.Doc
	FocusIdx    int
	Anchor      Position
	Head        Position
	Annotations []*model.Annotation
	FoldTails   map[model.BlockID]model.BlockID
}

// Session holds all document, selection, overlay, and collaboration state for an editor instance.
type Session struct {
	notePath string
	doc      *model.Doc
	codec    model.MarkdownCodec
	host     model.WorkspaceHost
	dirty    bool

	undoStack []undoSnapshot
	redoStack []undoSnapshot

	focusedBlock         int
	hoveredBlock         int
	selAnchor            Position
	selHead              Position
	focusPending         bool
	focusPreventScroll   bool
	selSyncPending       bool
	multiSelFocusPending bool
	firstLineDeleteArmed bool
	caretLineHint        int

	blockHandles     map[model.BlockID]ui.Handle
	richInputs       map[model.BlockID]*appUi.RichInputState
	tableInputs      map[tableCellKey]*appUi.RichInputState
	blockRects       map[model.BlockID]ui.Rect
	blockTextRects   map[model.BlockID]ui.Rect
	tableCellRects   map[tableCellKey]ui.Rect
	textLayoutCache  map[model.BlockID]cachedBlockLayout
	codeSyntaxCache  map[model.BlockID]cachedCodeSyntax
	codePreviewCache map[model.BlockID]cachedCodePreview
	frameBlocks      []frameBlockMeta
	docRev           uint64
	outlineRev       uint64
	outlineLen       int
	outlineCache     []model.OutlineItem
	lastVirtSig      float32
	virtReady        bool
	folded           map[model.BlockID]bool
	foldTails        map[model.BlockID]model.BlockID
	trailingPlus     bool

	// Mouse multi-block / marquee selection
	mouseMode        mouseSelMode
	mouseOriginBlock int
	mouseStartPt     ui.Point
	mouseCurPt       ui.Point
	mouseStartDocPt  ui.Point
	mouseTextAnchor  Position
	mouseUnitRange   [2]Position
	mouseDragged     bool
	lastPressMods    ui.Modifiers
	lastPressClicks  int
	lastPressLocal   ui.Point
	pressJustBegan   bool

	blockMenu *blockMenuState

	// Selection floating toolbar & format popovers
	toolbarPop          string // "", "more", "kinds", "lists", "palette", "link", "reactions", "comment"
	toolbarMoreIndex    int
	linkDraft           string
	linkInputHandle     ui.Handle
	linkFocusPending    bool
	commentDraft        string
	commentHandle       ui.Handle
	commentFocusPending bool

	// Inline AI
	dataDir                 string
	promptStore             *inlinePromptStore
	inlineAIResponder       func(ctx, orig, draft, instruction string) string
	inlineAIStreamResponder func(ctx context.Context, ctxBefore, orig, draft, instruction string, onDelta func(string)) (string, error)
	inlineAI                *inlineAISession
	copyPending             bool
	savedMarkdown           string
	caretHidden             bool
	footPickerAnnotID       string

	// Find bar
	findOpen         bool
	findQuery        string
	findMatches      []findMatch
	findCurrent      int
	findInputHandle  ui.Handle
	findFocusPending bool

	// Outline rail
	outlineHovered      bool
	outlinePinnedClosed bool
	highlightedBlockID  model.BlockID
	highlightUntil      time.Time
	outlineForceNarrow  bool

	// Annotations
	annotations           []*model.Annotation
	annotLoadedPath       string
	editMirror            *editLayout                       // mirrored layout of the focused input (edit_decor.go)
	caretBlink            caretBlinkState                   // own caret blink cycle (edit_decor.go)
	annotLoadFailed       bool                              // store unreadable: never write over it
	annotAppliedRaw       map[string]files.StoredAnnotation // applied annotations' stored body/rewrite, kept on re-save
	annotUnresolved       []files.StoredAnnotation          // stored entries whose anchor is not in this doc, written back verbatim
	annotationAIResponder func(docMarkdown string, pending []*model.Annotation) map[string]string
	activeAnnotID         string
	hoveredAnnotID        string
	editingAnnotID        string
	editingAnnotText      string
	editingHandle         ui.Handle
	nextAnnotSeq          int

	// BatchPlan diff review
	activePlan     *model.BatchPlan
	planExpandedOp int
	planDiffScroll ui.ScrollState

	// Code block UI
	codeLangMenuBlock model.BlockID
	codeLangScroll    ui.ScrollState
	codeCopiedBlock   model.BlockID
	codeCopiedUntil   time.Time

	// Callout block UI
	calloutMenuBlock model.BlockID
	calloutMenuIndex int

	// Image block UI
	imagePopBlock     model.BlockID
	imagePopKind      string // "", "more", "alt", "size", "crop"
	imageAltDraft     string
	imageAltHandle    ui.Handle
	imageCache        map[string]ui.ImageSource
	imageFailed       map[string]bool
	videoPlayers      map[string]*ui.VideoPlayer
	videoFailed       map[string]bool
	imageRects        map[model.BlockID]ui.Rect
	imgResizeBlock    model.BlockID
	imgResizeStartPct float32
	imgResizeCurPct   float32

	// Table block UI
	tableActiveBlock model.BlockID
	tableActiveRow   int
	tableActiveCol   int
	tableMenuBlock   model.BlockID
	tableMenuKind    string // "", "row", "col", "cell"
	tableMenuIndex   int

	// Drag reorder
	dragBlockID     model.BlockID
	dragOverIdx     int
	dragAfter       bool
	dragNested      bool
	dragIndent      int
	dragDropIndent  int
	dragActive      bool
	dragCanceled    bool
	dragStartPt     ui.Point
	dragDropPending bool
	dragDropFrom    int
	dragDropTo      int

	// Layout & Scroll
	bodyScroll        ui.ScrollState
	viewportWidth     float32
	viewportHeight    float32
	editorRect        ui.Rect
	scrollRect        ui.Rect
	rootHandle        ui.Handle
	frameRoot         ui.Element
	toolbarRect       ui.Rect
	overlayBtnRects   map[string]ui.Rect
	overlayPanelRects map[string]ui.Rect
	annotRowHeights   map[string]float32
	readOnly          bool
	minimal           bool
	minCanvasHeight   float32
	OnPickImages      func(blockID model.BlockID, replace bool)
}

// NewSession constructs a Session for a note path and document.
func NewSession(notePath string, doc *model.Doc, codec model.MarkdownCodec, host model.WorkspaceHost) *Session {
	d := normalizeDoc(doc)
	s := &Session{
		notePath:          notePath,
		doc:               d,
		codec:             codec,
		host:              host,
		focusedBlock:      0,
		hoveredBlock:      -1,
		dragOverIdx:       -1,
		dragDropFrom:      -1,
		dragDropTo:        -1,
		mouseOriginBlock:  -1,
		blockHandles:      make(map[model.BlockID]ui.Handle),
		richInputs:        make(map[model.BlockID]*appUi.RichInputState),
		tableInputs:       make(map[tableCellKey]*appUi.RichInputState),
		blockRects:        make(map[model.BlockID]ui.Rect),
		blockTextRects:    make(map[model.BlockID]ui.Rect),
		tableCellRects:    make(map[tableCellKey]ui.Rect),
		textLayoutCache:   make(map[model.BlockID]cachedBlockLayout),
		codeSyntaxCache:   make(map[model.BlockID]cachedCodeSyntax),
		codePreviewCache:  make(map[model.BlockID]cachedCodePreview),
		docRev:            1,
		folded:            make(map[model.BlockID]bool),
		foldTails:         make(map[model.BlockID]model.BlockID),
		imageCache:        make(map[string]ui.ImageSource),
		imageFailed:       make(map[string]bool),
		videoPlayers:      make(map[string]*ui.VideoPlayer),
		videoFailed:       make(map[string]bool),
		imageRects:        make(map[model.BlockID]ui.Rect),
		overlayBtnRects:   make(map[string]ui.Rect),
		overlayPanelRects: make(map[string]ui.Rect),
		annotRowHeights:   make(map[string]float32),
	}
	s.savedMarkdown = s.Markdown()
	return s
}

func normalizeDoc(doc *model.Doc) *model.Doc {
	if doc == nil {
		return model.NewEmptyDoc()
	}
	if len(doc.Blocks) == 0 {
		cp := doc.Clone()
		cp.Blocks = []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph}}
		return cp
	}
	for i := range doc.Blocks {
		if doc.Blocks[i].ID == 0 {
			doc.Blocks[i].ID = model.NewBlockID()
		}
	}
	return doc
}

func (s *Session) NotePath() string {
	return s.notePath
}

func (s *Session) SetNotePath(p string) {
	if p == s.notePath {
		return
	}
	old := s.notePath
	s.notePath = p
	s.onNotePathChanged(old, p)
}

func (s *Session) Doc() *model.Doc {
	return s.doc
}

func (s *Session) SetDoc(doc *model.Doc) {
	s.doc = normalizeDoc(doc)
	s.dirty = false
	s.undoStack = nil
	s.redoStack = nil
	s.focusedBlock = 0
	s.hoveredBlock = -1
	s.selAnchor = Position{}
	s.selHead = Position{}
	s.focusPreventScroll = false
	s.multiSelFocusPending = false
	s.mouseMode = mouseSelNone
	s.mouseOriginBlock = -1
	s.mouseDragged = false
	s.pressJustBegan = false
	s.resetBlockDrag()
	s.firstLineDeleteArmed = false
	s.blockMenu = nil
	s.toolbarPop = ""
	s.cancelInlineAI()
	s.inlineAI = nil
	s.copyPending = false
	s.codeLangMenuBlock = 0
	s.calloutMenuBlock = 0
	s.calloutMenuIndex = 0
	s.imagePopBlock = 0
	s.imagePopKind = ""
	s.imgResizeBlock = 0
	s.tableMenuBlock = 0
	s.tableMenuKind = ""
	s.highlightedBlockID = 0
	s.outlineHovered = false
	s.outlinePinnedClosed = false
	s.annotations = nil
	s.annotLoadedPath = ""
	s.activeAnnotID = ""
	s.hoveredAnnotID = ""
	s.editingAnnotID = ""
	s.footPickerAnnotID = ""
	s.caretHidden = false
	s.toolbarMoreIndex = 0
	s.closeVideoPlayers()
	s.trailingPlus = false
	clear(s.folded)
	clear(s.foldTails)
	clear(s.richInputs)
	clear(s.tableInputs)
	clear(s.blockRects)
	clear(s.blockTextRects)
	clear(s.tableCellRects)
	clear(s.textLayoutCache)
	clear(s.codeSyntaxCache)
	clear(s.codePreviewCache)
	s.docRev++
	s.virtReady = false
	clear(s.overlayBtnRects)
	clear(s.overlayPanelRects)
	clear(s.annotRowHeights)
	s.toolbarRect = ui.Rect{}
	s.savedMarkdown = s.Markdown()
	s.ensureAnnotationsLoaded()
	if s.findOpen {
		s.recomputeFind(false)
	}
}

func (s *Session) Dirty() bool {
	return s.dirty
}

func (s *Session) MarkSaved() {
	s.dirty = false
	s.savedMarkdown = s.Markdown()
}

func (s *Session) ClearTransientOverlays() {
	s.closeTransientOverlays()
}

func (s *Session) IsBusy() bool {
	if s.inlineAI != nil && s.inlineAI.Status == aiStatusThinking {
		return true
	}
	for _, a := range s.annotations {
		if a != nil && a.RewriteBusy {
			return true
		}
	}
	return false
}

func (s *Session) CanUndo() bool {
	return len(s.undoStack) > 0
}

func (s *Session) CanRedo() bool {
	return len(s.redoStack) > 0
}

func (s *Session) FocusedBlockIndex() int {
	return s.focusedBlock
}

func (s *Session) FocusBlock(idx int) {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		s.focusedBlock = -1
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s.doc.Blocks) {
		idx = len(s.doc.Blocks) - 1
	}
	s.focusedBlock = idx
	s.selAnchor = Position{Block: idx, Offset: 0}
	s.selHead = Position{Block: idx, Offset: 0}
	if s.needsDetachedSelectionFocus() {
		s.multiSelFocusPending = true
		s.focusPending = false
		s.selSyncPending = false
	} else {
		s.multiSelFocusPending = false
		s.focusPending = true
		s.focusPreventScroll = false
		s.selSyncPending = true
	}
	s.firstLineDeleteArmed = false
	s.caretHidden = false
}

func (s *Session) FirstLineDeleteArmed() bool {
	return s.firstLineDeleteArmed
}

func (s *Session) HighlightedBlockID() model.BlockID {
	return s.highlightedBlockID
}

func (s *Session) IsFindOpen() bool {
	return s.findOpen
}

func (s *Session) FindMatchCount() int {
	return len(s.findMatches)
}

func (s *Session) FindCurrentIndex() int {
	return s.findCurrent
}

func (s *Session) IsSlashMenuOpen() bool {
	return s.blockMenu != nil && s.blockMenu.Mode == menuSlash
}

func (s *Session) IsSelectionToolbarOpen() bool {
	return !s.readOnly && s.hasNonEmptySelection() && !s.findOpen && s.inlineAI == nil && s.selectionSupportsToolbar()
}

func (s *Session) Annotations() []*model.Annotation {
	s.ensureAnnotationsLoaded()
	out := make([]*model.Annotation, len(s.annotations))
	copy(out, s.annotations)
	return out
}

func (s *Session) ActivePlan() *model.BatchPlan {
	return s.activePlan
}

func (s *Session) SetActivePlan(plan *model.BatchPlan) {
	s.activePlan = plan
	s.planExpandedOp = 0
	if plan != nil {
		for i := range plan.Ops {
			s.ensurePlanOpLineSelection(&plan.Ops[i])
		}
	}
}

func (s *Session) SetOutlineForceNarrow(narrow bool) {
	s.outlineForceNarrow = narrow
}

func (s *Session) blockHandle(id model.BlockID) *ui.Handle {
	h, ok := s.blockHandles[id]
	if !ok {
		h = ui.Handle{}
		s.blockHandles[id] = h
	}
	return &h
}

func (s *Session) setBlockHandle(id model.BlockID, h ui.Handle) {
	s.blockHandles[id] = h
}

func (s *Session) richInputState(id model.BlockID) *appUi.RichInputState {
	if s.richInputs == nil {
		s.richInputs = make(map[model.BlockID]*appUi.RichInputState)
	}
	st, ok := s.richInputs[id]
	if !ok || st == nil {
		st = &appUi.RichInputState{}
		s.richInputs[id] = st
	}
	return st
}

func (s *Session) tableInputState(key tableCellKey) *appUi.RichInputState {
	if s.tableInputs == nil {
		s.tableInputs = make(map[tableCellKey]*appUi.RichInputState)
	}
	st, ok := s.tableInputs[key]
	if !ok || st == nil {
		st = &appUi.RichInputState{}
		s.tableInputs[key] = st
	}
	return st
}

func (s *Session) cloneAnnotations(src []*model.Annotation) []*model.Annotation {
	if len(src) == 0 {
		return nil
	}
	out := make([]*model.Annotation, len(src))
	for i, a := range src {
		if a == nil {
			continue
		}
		cp := *a
		cp.Reactions = slices.Clone(a.Reactions)
		out[i] = &cp
	}
	return out
}

func (s *Session) pushUndo() {
	snap := undoSnapshot{
		Doc:         s.doc.Clone(),
		FocusIdx:    s.focusedBlock,
		Anchor:      s.selAnchor,
		Head:        s.selHead,
		Annotations: s.cloneAnnotations(s.annotations),
		FoldTails:   cloneFoldTails(s.foldTails),
	}
	s.undoStack = append(s.undoStack, snap)
	if len(s.undoStack) > 200 {
		s.undoStack = s.undoStack[len(s.undoStack)-200:]
	}
	s.redoStack = nil
}

func (s *Session) Undo() bool {
	if len(s.undoStack) == 0 {
		return false
	}
	cur := undoSnapshot{
		Doc:         s.doc.Clone(),
		FocusIdx:    s.focusedBlock,
		Anchor:      s.selAnchor,
		Head:        s.selHead,
		Annotations: s.cloneAnnotations(s.annotations),
		FoldTails:   cloneFoldTails(s.foldTails),
	}
	s.redoStack = append(s.redoStack, cur)
	prev := s.undoStack[len(s.undoStack)-1]
	s.undoStack = s.undoStack[:len(s.undoStack)-1]
	s.restoreSnapshot(prev)
	return true
}

func (s *Session) Redo() bool {
	if len(s.redoStack) == 0 {
		return false
	}
	cur := undoSnapshot{
		Doc:         s.doc.Clone(),
		FocusIdx:    s.focusedBlock,
		Anchor:      s.selAnchor,
		Head:        s.selHead,
		Annotations: s.cloneAnnotations(s.annotations),
		FoldTails:   cloneFoldTails(s.foldTails),
	}
	s.undoStack = append(s.undoStack, cur)
	next := s.redoStack[len(s.redoStack)-1]
	s.redoStack = s.redoStack[:len(s.redoStack)-1]
	s.restoreSnapshot(next)
	return true
}

func (s *Session) restoreSnapshot(snap undoSnapshot) {
	s.doc = normalizeDoc(snap.Doc)
	s.focusedBlock = snap.FocusIdx
	if s.focusedBlock >= len(s.doc.Blocks) {
		s.focusedBlock = len(s.doc.Blocks) - 1
	}
	s.selAnchor = snap.Anchor
	s.selHead = snap.Head
	s.annotations = s.cloneAnnotations(snap.Annotations)
	clear(s.foldTails)
	for k, v := range snap.FoldTails {
		s.foldTails[k] = v
	}
	s.trailingPlus = false
	s.docRev++
	s.virtReady = false
	s.dirty = s.Markdown() != s.savedMarkdown
	s.focusPending = true
	s.selSyncPending = true
	s.caretHidden = false
	s.resetBlockDrag()
	s.firstLineDeleteArmed = false
	s.blockMenu = nil
	s.toolbarPop = ""
	s.toolbarMoreIndex = 0
	s.footPickerAnnotID = ""
	s.cancelInlineAI()
	s.inlineAI = nil
	s.copyPending = false
	s.calloutMenuBlock = 0
	s.imgResizeBlock = 0
	s.persistAnnotations()
	if s.findOpen {
		s.recomputeFind(false)
	}
}

func (s *Session) markDirty() {
	s.dirty = true
	s.docRev++
	if s.doc != nil {
		s.doc.CleanRaw = ""
	}
}

func (s *Session) orderedSelection() (Position, Position) {
	a, b := s.selAnchor, s.selHead
	if a.Block > b.Block || (a.Block == b.Block && a.Offset > b.Offset) {
		return b, a
	}
	return a, b
}

func (s *Session) hasNonEmptySelection() bool {
	a, b := s.orderedSelection()
	if a.Block < 0 || b.Block < 0 || s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	if a.Block != b.Block {
		return true
	}
	return a.Offset < b.Offset
}

func (s *Session) clampPos(p Position) Position {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return Position{Block: 0, Offset: 0}
	}
	if p.Block < 0 {
		p.Block = 0
	}
	if p.Block >= len(s.doc.Blocks) {
		p.Block = len(s.doc.Blocks) - 1
	}
	rlen := s.blockSelectionLen(p.Block)
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Offset > rlen {
		p.Offset = rlen
	}
	return p
}

func (s *Session) SelectRange(startBlock, startOff, endBlock, endOff int) {
	s.selAnchor = s.clampPos(Position{Block: startBlock, Offset: startOff})
	s.selHead = s.clampPos(Position{Block: endBlock, Offset: endOff})
	s.focusedBlock = s.selHead.Block
	if s.needsDetachedSelectionFocus() {
		s.multiSelFocusPending = true
		s.focusPending = false
		s.selSyncPending = false
	} else {
		s.multiSelFocusPending = false
		s.selSyncPending = true
	}
	s.firstLineDeleteArmed = false
	s.caretHidden = false
}

func (s *Session) hasAnyOverlayOpen() bool {
	return s.hasBlockingOverlay() || s.hasNonEmptySelection()
}

func (s *Session) hasMenuPopupOpen() bool {
	return s.blockMenu != nil ||
		(s.toolbarPop != "" && s.toolbarPop != "link") ||
		s.codeLangMenuBlock != 0 ||
		s.calloutMenuBlock != 0 ||
		(s.tableMenuBlock != 0 && s.tableMenuKind != "")
}

func (s *Session) hasBlockingOverlay() bool {
	return s.blockMenu != nil ||
		s.toolbarPop != "" ||
		s.inlineAI != nil ||
		s.findOpen ||
		s.codeLangMenuBlock != 0 ||
		s.calloutMenuBlock != 0 ||
		s.imagePopKind != "" ||
		(s.tableMenuBlock != 0 && s.tableMenuKind != "") ||
		s.editingAnnotID != "" ||
		s.footPickerAnnotID != "" ||
		s.activePlan != nil ||
		s.outlineHovered
}

func (s *Session) closeTransientOverlays() {
	s.blockMenu = nil
	s.toolbarPop = ""
	s.toolbarMoreIndex = 0
	s.codeLangMenuBlock = 0
	s.calloutMenuBlock = 0
	s.calloutMenuIndex = 0
	s.imagePopBlock = 0
	s.imagePopKind = ""
	s.tableMenuBlock = 0
	s.tableMenuKind = ""
	s.activeAnnotID = ""
	s.editingAnnotID = ""
	s.footPickerAnnotID = ""
	s.trailingPlus = false
}

func (s *Session) blockIndexByID(id model.BlockID) int {
	if s.doc == nil || id == 0 {
		return -1
	}
	for i := range s.doc.Blocks {
		if s.doc.Blocks[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Session) numberedListIndex(idx int) int {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return 1
	}
	target := &s.doc.Blocks[idx]
	if target.Kind != model.BlockNumbered {
		return 1
	}
	n := 1
	for i := idx - 1; i >= 0; i-- {
		b := &s.doc.Blocks[i]
		if b.Kind == model.BlockParagraph && b.Indent == 0 && len(b.Spans) == 0 {
			continue
		}
		if b.Indent > target.Indent {
			continue
		}
		if b.Indent < target.Indent {
			break
		}
		if b.Kind != model.BlockNumbered {
			break
		}
		n++
		if b.ListIndex > 1 {
			return n + b.ListIndex - 1
		}
	}
	if target.ListIndex > 1 {
		return target.ListIndex
	}
	return n
}

func (s *Session) isBlockHiddenByFold(idx int) bool {
	if s.doc == nil || len(s.folded) == 0 || idx <= 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	for hIdx := 0; hIdx < idx; hIdx++ {
		head := &s.doc.Blocks[hIdx]
		if !head.Kind.IsHeading() || !s.folded[head.ID] {
			continue
		}
		if idx < s.foldSectionEnd(hIdx) && !s.tailReveals(hIdx, idx) {
			return true
		}
	}
	return false
}

func (s *Session) headingSectionChildCount(idx int) int {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return 0
	}
	if !s.doc.Blocks[idx].Kind.IsHeading() {
		return 0
	}
	return max(s.foldSectionEnd(idx)-idx-1, 0)
}

func (s *Session) toggleFold(id model.BlockID) {
	delete(s.foldTails, id)
	if s.folded[id] {
		delete(s.folded, id)
	} else {
		s.folded[id] = true
	}
}

func (s *Session) requestRedraw() {
	if s.host != nil {
		s.host.RequestRedraw()
	}
}

func (s *Session) notifyFocusBlock(idx int) {
	s.focusedBlock = idx
	s.focusPending = true
	s.firstLineDeleteArmed = false
	s.caretHidden = false
}

func trimSpaces(s string) string {
	return strings.TrimSpace(s)
}
