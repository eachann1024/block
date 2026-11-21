package editor

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
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
	// Disabled greys the item out and skips it in keyboard navigation (e.g. the block's own type).
	Disabled bool
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
	Doc              *model.Doc
	FocusIdx         int
	Anchor           Position
	Head             Position
	Annotations      []*model.Annotation
	FoldTails        map[model.BlockID]model.BlockID
	ActiveAnnotID    string
	EditingAnnotID   string
	EditingAnnotText string
	CommentDraft     string
	ToolbarPop       string
}

// Session holds all document, selection, overlay, and collaboration state for an editor instance.
type Session struct {
	notePath    string
	doc         *model.Doc
	codec       model.MarkdownCodec
	host        model.WorkspaceHost
	dirty       bool
	dirtyAtomic atomic.Bool

	undoStack    []undoSnapshot
	redoStack    []undoSnapshot
	lastUndoAt   time.Time
	lastUndoKey  string
	suppressUndo bool

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
	lastAutoConvert      *autoConvertRecord
	nextInsertGuard      *nextInsertStyleGuard
	rightStyleBias       *rightStyleBias
	postEditCodeEdge     appUi.RichCodeEdge
	// navCarryX is the sticky window column kept by consecutive Up/Down across blocks.
	navCarryX float32
	navCarry  bool

	blockHandles       map[model.BlockID]ui.Handle
	richInputs         map[model.BlockID]*appUi.RichInputState
	tableInputs        map[tableCellKey]*appUi.RichInputState
	canvasLayoutOrigin ui.Point
	hasCanvasOrigin    bool
	blockDocRects      map[model.BlockID]ui.Rect
	blockTextDocRects  map[model.BlockID]ui.Rect
	tableCellDocRects  map[tableCellKey]tableCellGeom
	textLayoutCache    map[model.BlockID]cachedBlockLayout
	tableCellLayouts   map[tableCellKey]cachedCellLayout
	codeSyntaxCache    map[model.BlockID]cachedCodeSyntax
	codePreviewCache   map[model.BlockID]cachedCodePreview
	tableColCache      map[model.BlockID]cachedTableCols
	tableGridGeoms     map[model.BlockID]tableGridGeom
	tableHScrolls      map[model.BlockID]*ui.ScrollState
	blockIdxCache      map[model.BlockID]int
	blockIdxRev        uint64
	frameBlocks        []frameBlockMeta
	docRev             uint64
	docRevAtomic       atomic.Uint64
	outlineRev         uint64
	outlineLen         int
	outlineCache       []model.OutlineItem
	lastVirtSig        float32
	virtReady          bool
	folded             map[model.BlockID]bool
	foldTails          map[model.BlockID]model.BlockID
	trailingPlus       bool
	typingPointer      *ui.Point
	// soloAddID/soloAddAt track when the persistent add button of a
	// single-blank-line document first appeared, to play its hover intro.
	soloAddID model.BlockID
	soloAddAt time.Time

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
	lastPressScrollY float32
	pressJustBegan   bool

	blockMenu *blockMenuState

	// Selection floating toolbar & format popovers
	toolbarPop          string // "", "more", "palette", "link", "reactions", "comment"
	toolbarMoreIndex    int
	linkDraft           string
	linkInputHandle     ui.Handle
	linkFocusPending    bool
	commentDraft        string
	commentHandle       ui.Handle
	commentFocusPending bool
	commentBtnPressed   bool

	// Inline AI
	dataDir                 string
	promptStore             *inlinePromptStore
	aiDisabled              bool
	inlineAIResponder       func(ctx, orig, draft, instruction string) string
	inlineAIStreamResponder func(ctx context.Context, ctxBefore, orig, draft, instruction string, onDelta func(string)) (string, error)
	inlineAI                *inlineAISession
	savedMarkdown           string
	diskFingerprint         atomic.Int64
	caretHidden             bool
	reactionPickerAnnotID   string
	detachedCompose         string
	codeUndoCmd             string

	// Find bar
	findOpen         bool
	findQuery        string
	findMatches      []findMatch
	findMatchesRev   uint64
	findMatchesQuery string
	findCurrent      int
	findInputHandle  ui.Handle
	findFocusPending bool
	findToggleAt     time.Time
	// Replace row of the find bar
	findReplace             string
	findReplaceOpen         bool
	findReplaceHandle       ui.Handle
	findReplaceFocusPending bool

	// Outline rail
	outlineHovered      bool
	outlinePinnedClosed bool
	outlineHoverEnterAt time.Time
	outlineHoverLeaveAt time.Time
	jump                editorJump
	jumpAlpha           float32
	outlineForceNarrow  bool

	// Edge-float safe insets & scroll suppression
	edgeInsetLeft      float32
	edgeInsetRight     float32
	edgeLastScrollY    float32
	edgeScrollInit     bool
	edgeScrollQuietAt  time.Time
	hoverScrollQuietAt time.Time
	bgStyle            BackgroundStyle

	// Annotations
	annotations           []*model.Annotation
	annotLoadedPath       string
	editMirror            *editLayout                       // mirrored layout of the focused input (edit_decor.go)
	caretBlink            caretBlinkState                   // own caret blink cycle (edit_decor.go)
	annotLoadFailed       bool                              // store unreadable: never write over it
	annotAppliedRaw       map[string]files.StoredAnnotation // applied annotations' stored body/rewrite, kept on re-save
	annotUnresolved       []files.StoredAnnotation          // stored entries whose anchor is not in this doc, written back verbatim
	annotationAIResponder func(docMarkdown string, pending []*model.Annotation) map[string]model.AnnotationReply
	annotGenCh            chan map[string]model.AnnotationReply
	annotGenInFlight      int
	followupDrafts        map[string]*string
	annotExtraBottom      float32
	activeAnnotID         string
	hoveredAnnotID        string
	editingAnnotID        string
	editingAnnotText      string
	editingFocusPending   bool
	editingHandle         ui.Handle
	nextAnnotSeq          int
	annotFlashID          string
	annotFlashAt          time.Time
	annotFlashAlpha       float32

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
	imagePopBlock          model.BlockID
	imagePopKind           string // "", "more", "alt", "size", "crop"
	imageAltDraft          string
	imageAltHandle         ui.Handle
	imageMu                sync.Mutex
	imageCache             map[string]ui.ImageSource
	imageOrder             []string
	imageLoading           map[string]bool
	imageEpoch             uint64
	imageFailed            map[string]bool
	videoPlayers           map[string]*ui.VideoPlayer
	videoCtrlStates        map[string]*appUi.VideoControlState
	activeVideoSrc         string
	videoBytes             map[string]int64
	previewMedia           func(src string, video, load bool) PreviewAsset
	onPreviewMediaOpen     func(src string, isVideo bool)
	isPreviewMediaExpanded func(src string) (thisExpanded bool, anyLightboxOpen bool)
	videoFailed            map[string]bool
	imageDocRects          map[model.BlockID]ui.Rect
	imgResizeBlock         model.BlockID
	imgResizeStartPct      float32
	imgResizeCurPct        float32

	// Table block UI
	tableActiveBlock  model.BlockID
	tableActiveRow    int
	tableActiveCol    int
	tableMenuBlock    model.BlockID
	tableMenuKind     string // "", "row", "col", "table", "cell"
	tableMenuIndex    int
	tableExtend       *tableExtendState
	tableResizeBlock  model.BlockID
	tableResizeCol    int
	tableResizeStartW float32
	tableResizeCurW   float32
	tableDragBlock    model.BlockID
	tableDragKind     string // "", "col", "row"
	tableDragIndex    int
	tableDragStartPt  ui.Point
	tableDragActive   bool
	tableDragMoved    bool

	// Drag reorder
	dragBlockID     model.BlockID
	dragOverIdx     int
	dragTargetIdx   int
	dragSlot        int
	dragAfter       bool
	dragIndent      int
	dragLevelKey    [2]int
	dragLevelOffset int
	dragCollapsed   bool
	dragSavedHeight float32
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
	detachedFocus     ui.Handle
	frameRoot         ui.Element
	toolbarRect       ui.Rect
	overlayBtnRects   map[string]ui.Rect
	overlayPanelRects map[string]ui.Rect
	annotRowHeights   map[string]float32
	frameAnnotations  annotationFrame
	readOnly          bool
	minimal           bool
	embedded          bool
	minCanvasHeight   float32
	OnPickImages      func(blockID model.BlockID, replace bool)
	ExtraContextItems func() []ContextMenuExtra
}

// ContextMenuExtra describes an extra item appended to the editor canvas context menu.
type ContextMenuExtra struct {
	Label    string
	Icon     ui.MenuIcon
	OnChoose func()
}

// NewSession constructs a Session for a note path and document.
func NewSession(notePath string, doc *model.Doc, codec model.MarkdownCodec, host model.WorkspaceHost) *Session {
	d := normalizeDoc(doc)
	s := &Session{
		notePath:          notePath,
		doc:               d,
		codec:             codec,
		host:              host,
		focusedBlock:      defaultFocusedBlock(d),
		hoveredBlock:      -1,
		dragOverIdx:       -1,
		dragTargetIdx:     -1,
		dragSlot:          -1,
		dragLevelKey:      [2]int{-2, -2},
		dragDropFrom:      -1,
		dragDropTo:        -1,
		mouseOriginBlock:  -1,
		blockHandles:      make(map[model.BlockID]ui.Handle),
		richInputs:        make(map[model.BlockID]*appUi.RichInputState),
		tableInputs:       make(map[tableCellKey]*appUi.RichInputState),
		blockDocRects:     make(map[model.BlockID]ui.Rect),
		blockTextDocRects: make(map[model.BlockID]ui.Rect),
		tableCellDocRects: make(map[tableCellKey]tableCellGeom),
		textLayoutCache:   make(map[model.BlockID]cachedBlockLayout),
		codeSyntaxCache:   make(map[model.BlockID]cachedCodeSyntax),
		codePreviewCache:  make(map[model.BlockID]cachedCodePreview),
		tableColCache:     make(map[model.BlockID]cachedTableCols),
		tableGridGeoms:    make(map[model.BlockID]tableGridGeom),
		tableHScrolls:     make(map[model.BlockID]*ui.ScrollState),
		docRev:            1,
		folded:            make(map[model.BlockID]bool),
		foldTails:         make(map[model.BlockID]model.BlockID),
		imageCache:        make(map[string]ui.ImageSource),
		imageLoading:      make(map[string]bool),
		imageFailed:       make(map[string]bool),
		videoPlayers:      make(map[string]*ui.VideoPlayer),
		videoFailed:       make(map[string]bool),
		imageDocRects:     make(map[model.BlockID]ui.Rect),
		overlayBtnRects:   make(map[string]ui.Rect),
		overlayPanelRects: make(map[string]ui.Rect),
		annotRowHeights:   make(map[string]float32),
	}
	s.docRevAtomic.Store(s.docRev)
	s.savedMarkdown = s.Markdown()
	return s
}

func (s *Session) bumpDocRev() {
	s.docRev++
	s.docRevAtomic.Store(s.docRev)
}

func (s *Session) setDirty(v bool) {
	s.dirty = v
	s.dirtyAtomic.Store(v)
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
	s.resetMedia()
	s.onNotePathChanged(old, p)
}

func (s *Session) Doc() *model.Doc {
	return s.doc
}

func (s *Session) SetDoc(doc *model.Doc) {
	s.flushAnnotationEdit()
	s.doc = normalizeDoc(doc)
	s.setDirty(false)
	s.undoStack = nil
	s.redoStack = nil
	s.lastUndoAt = time.Time{}
	s.lastUndoKey = ""
	s.focusedBlock = defaultFocusedBlock(s.doc)
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
	s.clearAutoConvertState()
	s.postEditCodeEdge = appUi.RichCodeEdgeNone
	s.blockMenu = nil
	s.toolbarPop = ""
	s.cancelInlineAI()
	s.inlineAI = nil
	s.detachedCompose = ""
	s.codeUndoCmd = ""
	s.codeLangMenuBlock = 0
	s.calloutMenuBlock = 0
	s.calloutMenuIndex = 0
	s.imagePopBlock = 0
	s.imagePopKind = ""
	s.imgResizeBlock = 0
	s.resetTableExtend()
	s.tableMenuBlock = 0
	s.tableMenuKind = ""
	s.jump = editorJump{}
	s.outlineHovered = false
	s.outlinePinnedClosed = false
	s.outlineHoverEnterAt = time.Time{}
	s.outlineHoverLeaveAt = time.Time{}
	s.annotations = nil
	s.annotLoadedPath = ""
	s.activeAnnotID = ""
	s.hoveredAnnotID = ""
	s.editingAnnotID = ""
	s.editingFocusPending = false
	s.commentDraft = ""
	s.annotFlashID = ""
	s.annotFlashAt = time.Time{}
	s.annotFlashAlpha = 0
	s.reactionPickerAnnotID = ""
	s.caretHidden = false
	s.toolbarMoreIndex = 0
	s.resetMedia()
	s.trailingPlus = false
	clear(s.folded)
	clear(s.foldTails)
	clear(s.richInputs)
	clear(s.tableInputs)
	clear(s.blockDocRects)
	clear(s.blockTextDocRects)
	clear(s.tableCellDocRects)
	clear(s.imageDocRects)
	s.hasCanvasOrigin = false
	clear(s.textLayoutCache)
	clear(s.tableCellLayouts)
	clear(s.codeSyntaxCache)
	clear(s.codePreviewCache)
	clear(s.tableColCache)
	clear(s.tableGridGeoms)
	clear(s.tableHScrolls)
	clear(s.blockIdxCache)
	s.blockIdxRev = 0
	s.bumpDocRev()
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

// Close releases native media handles and heavy caches held by a closed tab Session.
func (s *Session) Close() {
	if s == nil {
		return
	}
	s.cancelInlineAI()
	s.resetMedia()
	clear(s.textLayoutCache)
	clear(s.tableCellLayouts)
	clear(s.codeSyntaxCache)
	clear(s.codePreviewCache)
	clear(s.tableColCache)
	clear(s.blockIdxCache)
	s.undoStack = nil
	s.redoStack = nil
}

// TrimInactiveCaches drops decoded bitmaps and transient layout caches on background tabs
// while preserving document content, selection, and undo history.
func (s *Session) TrimInactiveCaches() {
	if s == nil {
		return
	}
	for _, p := range s.videoPlayers {
		if p != nil && p.Playing() {
			p.Pause()
		}
	}
	s.imageMu.Lock()
	clear(s.imageCache)
	s.imageOrder = nil
	s.imageMu.Unlock()
	clear(s.textLayoutCache)
	clear(s.tableCellLayouts)
	clear(s.codeSyntaxCache)
}

func (s *Session) Dirty() bool {
	return s.dirtyAtomic.Load()
}

func (s *Session) DiskFingerprint() int64 {
	return s.diskFingerprint.Load()
}

func (s *Session) SetDiskFingerprint(version int64) {
	s.diskFingerprint.Store(version)
}

func (s *Session) DocRev() uint64 {
	return s.docRevAtomic.Load()
}

func (s *Session) MarkSaved() {
	s.setDirty(false)
	s.lastUndoKey = ""
	s.lastUndoAt = time.Time{}
	s.savedMarkdown = s.Markdown()
}

// MarkSavedIfUnchanged marks the session clean only if docRev still matches the snapshot that was written to disk.
func (s *Session) MarkSavedIfUnchanged(rev uint64, savedMD string) bool {
	if s.docRev != rev {
		return false
	}
	s.setDirty(false)
	s.lastUndoKey = ""
	s.lastUndoAt = time.Time{}
	s.savedMarkdown = savedMD
	return true
}

// SyncDiskSavedMarkdown records the Markdown text that was actually written to disk
// when the session moved to a newer docRev while a save was in flight, ensuring
// Dirty() reflects divergence from disk (never falsely reporting clean after Undo).
func (s *Session) SyncDiskSavedMarkdown(diskMD string) {
	s.savedMarkdown = diskMD
	s.setDirty(s.Markdown() != diskMD)
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
	s.detachedCompose = ""
	s.codeUndoCmd = ""
	s.navCarry = false
}

func isEmptyDoc(doc *model.Doc) bool {
	if doc == nil || len(doc.Blocks) == 0 {
		return true
	}
	for i := range doc.Blocks {
		if !isPlainEmptyParagraph(&doc.Blocks[i]) {
			return false
		}
	}
	return true
}

func defaultFocusedBlock(doc *model.Doc) int {
	if isEmptyDoc(doc) {
		return 0
	}
	return -1
}

func (s *Session) isEmptyDocument() bool {
	return isEmptyDoc(s.doc)
}

// ClearFocus releases active block focus so non-empty notes render cleanly until clicked,
// while empty notes keep the first empty-line hint visible by default.
func (s *Session) ClearFocus() {
	s.focusedBlock = defaultFocusedBlock(s.doc)
	s.focusPending = false
	s.focusPreventScroll = false
	s.multiSelFocusPending = false
	s.selSyncPending = false
	s.firstLineDeleteArmed = false
	s.tableActiveBlock = 0
	s.detachedCompose = ""
	s.codeUndoCmd = ""
}

func (s *Session) FirstLineDeleteArmed() bool {
	return s.firstLineDeleteArmed
}

func (s *Session) HighlightedBlockID() model.BlockID {
	return s.jump.block
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

func (s *Session) captureUndoSnapshot() undoSnapshot {
	return undoSnapshot{
		Doc:              s.doc.Clone(),
		FocusIdx:         s.focusedBlock,
		Anchor:           s.selAnchor,
		Head:             s.selHead,
		Annotations:      s.cloneAnnotations(s.annotations),
		FoldTails:        cloneFoldTails(s.foldTails),
		ActiveAnnotID:    s.activeAnnotID,
		EditingAnnotID:   s.editingAnnotID,
		EditingAnnotText: s.editingAnnotText,
		CommentDraft:     s.commentDraft,
		ToolbarPop:       s.toolbarPop,
	}
}

func (s *Session) pushUndoSnapshot(snap undoSnapshot) {
	s.undoStack = append(s.undoStack, snap)
	if len(s.undoStack) > 200 {
		s.undoStack = s.undoStack[len(s.undoStack)-200:]
	}
	s.redoStack = nil
	s.lastUndoKey = ""
	s.lastUndoAt = time.Time{}
}

func (s *Session) pushUndo() {
	s.pushUndoSnapshot(s.captureUndoSnapshot())
}

const textUndoCoalesceWindow = 600 * time.Millisecond

func (s *Session) pushCoalescedTextUndo(key string) {
	now := time.Now()
	if !testing.Testing() &&
		key != "" &&
		key == s.lastUndoKey &&
		len(s.undoStack) > 0 &&
		len(s.redoStack) == 0 &&
		!s.lastUndoAt.IsZero() &&
		now.Sub(s.lastUndoAt) <= textUndoCoalesceWindow {
		s.lastUndoAt = now
		return
	}
	s.pushUndo()
	s.lastUndoKey = key
	s.lastUndoAt = now
}

func (s *Session) Undo() bool {
	if len(s.undoStack) == 0 {
		return false
	}
	s.lastUndoKey = ""
	s.lastUndoAt = time.Time{}
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

// HostEditRollback captures pre-edit document, undo/redo history, and
// annotation/popup state so a failed host-driven edit can roll back completely
// without losing prior Undo/Redo history (even at the 200-step undo cap) or
// in-progress annotation drafts and popups.
type HostEditRollback struct {
	valid                 bool
	preEditSnap           undoSnapshot
	prevUndo              []undoSnapshot
	prevRedo              []undoSnapshot
	prevLastUndoKey       string
	prevLastUndoAt        time.Time
	prevWasClean          bool
	prevSavedMarkdown     string
	activeAnnotID         string
	editingAnnotID        string
	editingAnnotText      string
	commentDraft          string
	toolbarPop            string
	reactionPickerAnnotID string
	annotFlashID          string
	annotFlashAt          time.Time
	annotFlashAlpha       float32
	tableActiveBlock      model.BlockID
	tableActiveRow        int
	tableActiveCol        int
}

// CaptureHostEditRollback snapshots reversible session state before a host-driven edit.
func (s *Session) CaptureHostEditRollback() HostEditRollback {
	var undoCopy []undoSnapshot
	if s.undoStack != nil {
		undoCopy = make([]undoSnapshot, len(s.undoStack))
		copy(undoCopy, s.undoStack)
	}
	var redoCopy []undoSnapshot
	if s.redoStack != nil {
		redoCopy = make([]undoSnapshot, len(s.redoStack))
		copy(redoCopy, s.redoStack)
	}
	return HostEditRollback{
		valid:                 true,
		preEditSnap:           s.captureUndoSnapshot(),
		prevUndo:              undoCopy,
		prevRedo:              redoCopy,
		prevLastUndoKey:       s.lastUndoKey,
		prevLastUndoAt:        s.lastUndoAt,
		prevWasClean:          !s.dirty,
		prevSavedMarkdown:     s.savedMarkdown,
		activeAnnotID:         s.activeAnnotID,
		editingAnnotID:        s.editingAnnotID,
		editingAnnotText:      s.editingAnnotText,
		commentDraft:          s.commentDraft,
		toolbarPop:            s.toolbarPop,
		reactionPickerAnnotID: s.reactionPickerAnnotID,
		annotFlashID:          s.annotFlashID,
		annotFlashAt:          s.annotFlashAt,
		annotFlashAlpha:       s.annotFlashAlpha,
		tableActiveBlock:      s.tableActiveBlock,
		tableActiveRow:        s.tableActiveRow,
		tableActiveCol:        s.tableActiveCol,
	}
}

// RollbackHostEdit reverts the edit pushed since rb was captured and restores
// prior Undo/Redo history and in-progress annotation/popup state verbatim.
func (s *Session) RollbackHostEdit(rb HostEditRollback) bool {
	if !rb.valid {
		return false
	}
	s.restoreSnapshot(rb.preEditSnap)
	if rb.prevUndo != nil {
		s.undoStack = make([]undoSnapshot, len(rb.prevUndo))
		copy(s.undoStack, rb.prevUndo)
	} else {
		s.undoStack = nil
	}
	if rb.prevRedo != nil {
		s.redoStack = make([]undoSnapshot, len(rb.prevRedo))
		copy(s.redoStack, rb.prevRedo)
	} else {
		s.redoStack = nil
	}
	s.lastUndoKey = rb.prevLastUndoKey
	s.lastUndoAt = rb.prevLastUndoAt
	s.activeAnnotID = rb.activeAnnotID
	s.editingAnnotID = rb.editingAnnotID
	s.editingAnnotText = rb.editingAnnotText
	s.commentDraft = rb.commentDraft
	s.toolbarPop = rb.toolbarPop
	s.reactionPickerAnnotID = rb.reactionPickerAnnotID
	s.annotFlashID = rb.annotFlashID
	s.annotFlashAt = rb.annotFlashAt
	s.annotFlashAlpha = rb.annotFlashAlpha
	s.tableActiveBlock = rb.tableActiveBlock
	s.tableActiveRow = rb.tableActiveRow
	s.tableActiveCol = rb.tableActiveCol
	if s.editingAnnotID != "" {
		s.editingFocusPending = true
	}
	s.multiSelFocusPending = s.needsDetachedSelectionFocus()
	s.focusPending = !s.multiSelFocusPending
	s.selSyncPending = !s.multiSelFocusPending
	s.savedMarkdown = rb.prevSavedMarkdown
	if rb.prevWasClean {
		s.setDirty(false)
		s.savedMarkdown = s.Markdown()
	} else {
		s.setDirty(true)
	}
	return true
}

// RevertLastUndo restores the most recent undo snapshot without recording the
// reverted state on the redo stack (used when a host-driven edit fails to save
// and must roll back cleanly without resurfacing on Redo or losing in-progress
// annotation drafts).
func (s *Session) RevertLastUndo() bool {
	if len(s.undoStack) == 0 {
		return false
	}
	s.lastUndoKey = ""
	s.lastUndoAt = time.Time{}
	prev := s.undoStack[len(s.undoStack)-1]
	s.undoStack = s.undoStack[:len(s.undoStack)-1]
	s.redoStack = nil
	s.restoreSnapshot(prev)
	s.activeAnnotID = prev.ActiveAnnotID
	s.editingAnnotID = prev.EditingAnnotID
	s.editingAnnotText = prev.EditingAnnotText
	s.commentDraft = prev.CommentDraft
	if prev.ToolbarPop == "comment" {
		s.toolbarPop = prev.ToolbarPop
	}
	if s.editingAnnotID != "" {
		s.editingFocusPending = true
	}
	return true
}

func (s *Session) Redo() bool {
	if len(s.redoStack) == 0 {
		return false
	}
	s.lastUndoKey = ""
	s.lastUndoAt = time.Time{}
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
	s.bumpDocRev()
	s.virtReady = false
	s.setDirty(s.Markdown() != s.savedMarkdown)
	s.focusPending = true
	s.selSyncPending = true
	s.caretHidden = false
	s.resetBlockDrag()
	s.firstLineDeleteArmed = false
	s.clearAutoConvertState()
	s.postEditCodeEdge = appUi.RichCodeEdgeNone
	s.blockMenu = nil
	s.toolbarPop = ""
	s.toolbarMoreIndex = 0
	s.commentDraft = ""
	s.editingAnnotID = ""
	s.editingAnnotText = ""
	s.editingFocusPending = false
	s.annotFlashID = ""
	s.annotFlashAt = time.Time{}
	s.annotFlashAlpha = 0
	s.reactionPickerAnnotID = ""
	s.cancelInlineAI()
	s.inlineAI = nil
	s.detachedCompose = ""
	s.codeUndoCmd = ""
	s.calloutMenuBlock = 0
	s.imgResizeBlock = 0
	s.resetTableExtend()
	s.persistAnnotations()
	if s.findOpen {
		s.recomputeFind(false)
	}
}

func (s *Session) markDirty() {
	s.setDirty(true)
	s.bumpDocRev()
	if s.doc != nil {
		if s.doc.CleanRaw != "" && strings.Contains(s.doc.CleanRaw, "]:") && s.codec != nil {
			if exp, ok := s.codec.(interface{ ExpandBlockRefCleanSources([]model.Block, string) }); ok {
				exp.ExpandBlockRefCleanSources(s.doc.Blocks, s.doc.CleanRaw)
			}
		}
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
	if s.doc == nil || a.Block < 0 || b.Block >= len(s.doc.Blocks) {
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

// CollapseSelectionToEnd collapses any active non-empty selection to its end
// position without deleting selected text, so subsequent inserts append after
// the selection instead of replacing it.
func (s *Session) CollapseSelectionToEnd() {
	if !s.hasNonEmptySelection() {
		return
	}
	_, b := s.orderedSelection()
	s.SelectRange(b.Block, b.Offset, b.Block, b.Offset)
	if b.Block >= 0 && b.Block < len(s.doc.Blocks) {
		blk := &s.doc.Blocks[b.Block]
		if blk.Kind == model.BlockTable && blk.Table != nil {
			if row, col, _, _, ok := tableCellAtRuneRange(blk.Table, b.Offset, b.Offset); ok {
				s.tableActiveBlock = blk.ID
				s.tableActiveRow = row
				s.tableActiveCol = col
			}
		}
	}
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
	s.detachedCompose = ""
	s.codeUndoCmd = ""
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
		s.reactionPickerAnnotID != "" ||
		s.activePlan != nil ||
		s.outlineHovered
}

func (s *Session) closeTransientOverlays() {
	s.flushAnnotationEdit()
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
	s.editingFocusPending = false
	s.commentDraft = ""
	s.annotFlashID = ""
	s.annotFlashAt = time.Time{}
	s.annotFlashAlpha = 0
	s.reactionPickerAnnotID = ""
	s.trailingPlus = false
	s.outlineHovered = false
	s.outlineHoverEnterAt = time.Time{}
	s.outlineHoverLeaveAt = time.Time{}
}

// closeNonAnnotationOverlays dismisses geometric popups without committing or
// discarding any in-progress annotation edit or comment draft.
func (s *Session) closeNonAnnotationOverlays() {
	s.blockMenu = nil
	if s.toolbarPop != "comment" {
		s.toolbarPop = ""
	}
	s.toolbarMoreIndex = 0
	s.codeLangMenuBlock = 0
	s.calloutMenuBlock = 0
	s.calloutMenuIndex = 0
	s.imagePopBlock = 0
	s.imagePopKind = ""
	s.tableMenuBlock = 0
	s.tableMenuKind = ""
	s.reactionPickerAnnotID = ""
	s.trailingPlus = false
	s.outlineHovered = false
	s.outlineHoverEnterAt = time.Time{}
	s.outlineHoverLeaveAt = time.Time{}
}

func (s *Session) blockIndexByID(id model.BlockID) int {
	if s.doc == nil || id == 0 {
		return -1
	}
	blocks := s.doc.Blocks
	if s.blockIdxCache != nil && s.blockIdxRev == s.docRev && len(s.blockIdxCache) == len(blocks) {
		if idx, ok := s.blockIdxCache[id]; ok && idx >= 0 && idx < len(blocks) && blocks[idx].ID == id {
			return idx
		}
		if _, ok := s.blockIdxCache[id]; !ok {
			return -1
		}
	}
	if s.blockIdxCache == nil {
		s.blockIdxCache = make(map[model.BlockID]int, len(blocks))
	} else {
		clear(s.blockIdxCache)
	}
	found := -1
	for i := range blocks {
		bid := blocks[i].ID
		s.blockIdxCache[bid] = i
		if bid == id {
			found = i
		}
	}
	s.blockIdxRev = s.docRev
	return found
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
	s.detachedCompose = ""
	s.codeUndoCmd = ""
}

func trimSpaces(s string) string {
	return strings.TrimSpace(s)
}
