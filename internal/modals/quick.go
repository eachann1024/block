package modals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"block/internal/editor"
	"block/internal/files"
	"block/internal/icons"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var appQuitting atomic.Bool

// SetAppQuitting marks whether the application is in the middle of quitting,
// so resident hidden windows (Quick Note, Search Window) allow close instead
// of calling PreventDefault and aborting Cmd+Q.
func SetAppQuitting(quitting bool) {
	appQuitting.Store(quitting)
}

// IsAppQuitting reports whether an application quit is currently in progress.
func IsAppQuitting() bool {
	return appQuitting.Load()
}

const (
	quickSlotCount      = 3
	quickHotkeyDebounce = 180 * time.Millisecond
	quickNoticeTTL      = 3 * time.Second
	quickBoundsDebounce = 250 * time.Millisecond
	quickDefaultWidth   = 440
	quickDefaultHeight  = 520
	quickMinWidth       = 380
	quickMinHeight      = 320

	quickMinZoomPct     = 70
	quickMaxZoomPct     = 200
	quickStepZoomPct    = 10
	quickDefaultZoomPct = 100
)

type quickMeta struct {
	Names  []string `json:"names"`
	Active int      `json:"active"`
}

type quickSessionHost struct {
	q *QuickNoteController
}

func (h *quickSessionHost) ActiveNotebook() string {
	if h.q.host != nil {
		return h.q.host.ActiveNotebook()
	}
	return ""
}

func (h *quickSessionHost) Notebooks() []string {
	if h.q.host != nil {
		return h.q.host.Notebooks()
	}
	return nil
}

func (h *quickSessionHost) ActiveNotePath() string {
	return h.q.slotFilePath(h.q.ActiveSlot())
}

func (h *quickSessionHost) ListNotebookFiles(root string) ([]model.FileEntry, error) {
	if h.q.host != nil {
		return h.q.host.ListNotebookFiles(root)
	}
	return nil, nil
}

func (h *quickSessionHost) IgnoredDirs() map[string]struct{} {
	return searchIgnoredDirs(h.q.host)
}

func (h *quickSessionHost) SearchWorkspace(query string, allNotebooks bool) ([]model.SearchHit, error) {
	if h.q.host != nil {
		return h.q.host.SearchWorkspace(query, allNotebooks)
	}
	return nil, nil
}

func (h *quickSessionHost) ReadNote(absPath string) (string, error) {
	if h.q.host != nil {
		return h.q.host.ReadNote(absPath)
	}
	b, err := os.ReadFile(absPath)
	return string(b), err
}

func (h *quickSessionHost) WriteNote(absPath string, content string) error {
	if h.q.host != nil {
		return h.q.host.WriteNote(absPath, content)
	}
	return os.WriteFile(absPath, []byte(content), 0o644)
}

func (h *quickSessionHost) OpenNote(absPath string, lineHint int) error {
	if h.q.host != nil {
		return h.q.host.OpenNote(absPath, lineHint)
	}
	return nil
}

func (h *quickSessionHost) OpenNoteAt(absPath string, lineHint, colHint int) error {
	if h.q.host != nil {
		if ph, ok := h.q.host.(interface {
			OpenNoteAt(absPath string, lineHint, colHint int) error
		}); ok {
			return ph.OpenNoteAt(absPath, lineHint, colHint)
		}
		return h.q.host.OpenNote(absPath, lineHint)
	}
	return nil
}

func (h *quickSessionHost) OpenClassifiedAt(target model.VerifiedFileTarget, cf model.ClassifiedFile, lineHint, colHint int) error {
	if h.q.host != nil {
		if ch, ok := h.q.host.(interface {
			OpenClassifiedAt(target model.VerifiedFileTarget, cf model.ClassifiedFile, lineHint, colHint int) error
		}); ok {
			return ch.OpenClassifiedAt(target, cf, lineHint, colHint)
		}
		return h.OpenNoteAt(target.AbsPath, lineHint, colHint)
	}
	return nil
}

func (h *quickSessionHost) PrepareClassifiedTarget(target model.VerifiedFileTarget) (model.PreparedTabTarget, error) {
	if h.q.host != nil {
		if ph, ok := h.q.host.(interface {
			PrepareClassifiedTarget(target model.VerifiedFileTarget) (model.PreparedTabTarget, error)
		}); ok {
			return ph.PrepareClassifiedTarget(target)
		}
	}
	return model.PreparedTabTarget{TabPath: target.AbsPath, Verified: target}, nil
}

func (h *quickSessionHost) ApplyPreparedClassifiedAt(prep model.PreparedTabTarget, cf model.ClassifiedFile, lineHint, colHint int) error {
	if h.q.host != nil {
		if ph, ok := h.q.host.(interface {
			ApplyPreparedClassifiedAt(prep model.PreparedTabTarget, cf model.ClassifiedFile, lineHint, colHint int) error
		}); ok {
			return ph.ApplyPreparedClassifiedAt(prep, cf, lineHint, colHint)
		}
		return h.OpenClassifiedAt(prep.Verified, cf, lineHint, colHint)
	}
	return nil
}

func (h *quickSessionHost) PrepareRevealDirectory(absPath string) model.PreparedDirectoryTarget {
	if h.q.host != nil {
		if ph, ok := h.q.host.(interface {
			PrepareRevealDirectory(absPath string) model.PreparedDirectoryTarget
		}); ok {
			return ph.PrepareRevealDirectory(absPath)
		}
	}
	return model.PreparedDirectoryTarget{AbsPath: absPath}
}

func (h *quickSessionHost) ApplyPreparedRevealDirectory(prep model.PreparedDirectoryTarget) {
	if h.q.host != nil {
		if ph, ok := h.q.host.(interface {
			ApplyPreparedRevealDirectory(prep model.PreparedDirectoryTarget)
		}); ok {
			ph.ApplyPreparedRevealDirectory(prep)
			return
		}
	}
	h.RevealDirectory(prep.AbsPath)
}

func (h *quickSessionHost) RunOnMain(fn func()) bool {
	if h.q.host != nil {
		if ah, ok := h.q.host.(interface{ RunOnMain(func()) bool }); ok {
			return ah.RunOnMain(fn)
		}
	}
	return false
}

func (h *quickSessionHost) RevealPath(absPath string) {
	if h.q.host != nil {
		if rh, ok := h.q.host.(interface{ RevealPath(string) }); ok {
			rh.RevealPath(absPath)
		}
	}
}

func (h *quickSessionHost) RevealDirectory(absPath string) {
	if h.q.host != nil {
		if dh, ok := h.q.host.(interface{ RevealDirectory(string) }); ok {
			dh.RevealDirectory(absPath)
			return
		}
		if rh, ok := h.q.host.(interface{ RevealPath(string) }); ok {
			rh.RevealPath(absPath)
		}
	}
}

func (h *quickSessionHost) NotifyPathError(msg string) {
	h.q.setNotice(msg, false)
	if h.q.host != nil {
		if nh, ok := h.q.host.(interface{ NotifyPathError(string) }); ok {
			nh.NotifyPathError(msg)
		}
	}
}

func (h *quickSessionHost) PreviewPlanInEditor(*model.BatchPlan) error { return nil }
func (h *quickSessionHost) ApplyBatchPlan(*model.BatchPlan) error      { return nil }
func (h *quickSessionHost) RejectBatchPlan(string) error               { return nil }
func (h *quickSessionHost) UndoLastPlan(*model.BatchPlan) error        { return nil }
func (h *quickSessionHost) OpenSettings(tab string) {
	if h.q.host != nil {
		h.q.host.OpenSettings(tab)
	}
}
func (h *quickSessionHost) RequestRedraw() {
	h.q.Invalidate()
}

// QuickNoteController manages the standalone 3-slot Quick Note floating panel.
type QuickNoteController struct {
	dataDir string
	host    model.WorkspaceHost
	codec   model.MarkdownCodec

	// Optional workspace persistence & notification hooks.
	OnSavedNote          func(path string)
	OnSummonSearch       func()
	GetQuickShortcut     func() string
	GetQuickZoom         func() float32
	OnQuickZoomChanged   func(zoom float32)
	GetQuickBounds       func() *model.SavedWindowBounds
	OnQuickBoundsChanged func(b model.SavedWindowBounds)

	mu              sync.Mutex
	diskMu          sync.Mutex
	writeSeq        [quickSlotCount]atomic.Uint64
	win             *mygo.Window
	visible         bool
	activeSlot      int
	slots           [quickSlotCount]string
	names           [quickSlotCount]string
	sessions        [quickSlotCount]*editor.Session
	sessHost        *quickSessionHost
	lastTrigger     time.Time
	wheelAccum      float32
	lastWheelEvent  time.Time
	lastWheelSwitch time.Time
	aiDisabled      bool
	inlineAIRespond func(ctx context.Context, ctxBefore, orig, draft, instruction string, onDelta func(string)) (string, error)
	zoom            float32
	notice          string
	savedFlash      bool
	noticeUntil     time.Time
	noticeSeq       uint64
	noticeTimer     *time.Timer
	savedBounds     *model.SavedWindowBounds
	boundsTimer     *time.Timer
	syncTimer       *time.Timer
	saveFlashAt     time.Time
}

// NewQuickNoteController creates a QuickNoteController backed by `dataDir/quick`.
func NewQuickNoteController(dataDir string, host model.WorkspaceHost, codec model.MarkdownCodec) *QuickNoteController {
	if codec == nil {
		codec = markdown.NewCodec()
	}
	q := &QuickNoteController{
		dataDir: dataDir,
		host:    host,
		codec:   codec,
		zoom:    1.0,
		names: [quickSlotCount]string{
			defaultQuickSlotName(0),
			defaultQuickSlotName(1),
			defaultQuickSlotName(2),
		},
	}
	q.sessHost = &quickSessionHost{q: q}
	q.loadFromDisk()
	q.initSessionsLocked()
	q.sessions[q.activeSlot].EnsureEditorFocus()
	return q
}

func (q *QuickNoteController) initSessionsLocked() {
	for i := 0; i < quickSlotCount; i++ {
		slotIdx := i
		doc := q.codec.Parse(q.slots[i])
		sess := editor.NewSession(q.slotFilePath(i), doc, q.codec, q.sessHost)
		sess.SetMinimal(true)
		sess.SetDataDir(q.dataDir)
		sess.SetAIEnabled(!q.aiDisabled)
		sess.SetInlineAIStreamResponder(q.inlineAIRespond)
		sess.OnPickImages = func(blockID model.BlockID, replace bool) {
			q.pickImagesForSlot(slotIdx, blockID, replace)
		}
		q.sessions[i] = sess
	}
}

// SetInlineAIStreamResponder configures the Inline AI responder for all Quick Note editor slots.
func (q *QuickNoteController) SetInlineAIStreamResponder(fn func(ctx context.Context, ctxBefore, orig, draft, instruction string, onDelta func(string)) (string, error)) {
	q.mu.Lock()
	q.inlineAIRespond = fn
	sessions := q.sessions
	q.mu.Unlock()
	for _, sess := range sessions {
		if sess != nil {
			sess.SetInlineAIStreamResponder(fn)
		}
	}
}

// SetAIEnabled updates the AI controls in every Quick Note slot.
func (q *QuickNoteController) SetAIEnabled(enabled bool) {
	q.mu.Lock()
	q.aiDisabled = !enabled
	sessions := q.sessions
	q.mu.Unlock()
	for _, sess := range sessions {
		if sess != nil {
			sess.SetAIEnabled(enabled)
		}
	}
	q.Invalidate()
}

func (q *QuickNoteController) serializeSessionLocked(slot int) string {
	sess := q.sessions[slot]
	if sess == nil {
		return q.slots[slot]
	}
	doc := sess.Doc()
	if doc == nil || len(doc.Blocks) == 0 {
		return ""
	}
	if len(doc.Blocks) == 1 &&
		doc.Blocks[0].Kind == model.BlockParagraph &&
		doc.Blocks[0].Indent == 0 &&
		doc.Blocks[0].PlainText() == "" {
		return ""
	}
	return q.codec.Serialize(doc)
}

func (q *QuickNoteController) syncSessionToDiskLocked(slot int) error {
	if slot < 0 || slot >= quickSlotCount {
		return nil
	}
	sess := q.sessions[slot]
	if sess != nil && sess.Dirty() {
		q.slots[slot] = q.serializeSessionLocked(slot)
	}
	if err := q.saveSlotLocked(slot); err != nil {
		q.notice, q.savedFlash = err.Error(), false
		return err
	}
	if sess != nil && sess.Dirty() {
		sess.MarkSaved()
	}
	return nil
}

func defaultQuickSlotName(slot int) string {
	if slot == 0 {
		return theme.Tr("灵感", "Ideas")
	}
	if theme.IsEn() {
		return fmt.Sprintf("Quick %d", slot+1)
	}
	return fmt.Sprintf("速记 %d", slot+1)
}

func isDefaultQuickSlotName(slot int, name string) bool {
	trimmed := strings.TrimSpace(name)
	return trimmed == fmt.Sprintf("速记 %d", slot+1) ||
		trimmed == fmt.Sprintf("Quick %d", slot+1) ||
		(slot == 0 && (trimmed == "灵感" || trimmed == "Ideas"))
}

func (q *QuickNoteController) quickDir() string {
	if strings.TrimSpace(q.dataDir) == "" {
		return ""
	}
	return filepath.Join(q.dataDir, "quick")
}

func (q *QuickNoteController) slotFilePath(slot int) string {
	dir := q.quickDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, fmt.Sprintf("slot-%d.md", slot+1))
}

func (q *QuickNoteController) ensureSlotFileLocked(slot int) {
	p := q.slotFilePath(slot)
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644); err == nil {
		_ = f.Close()
	}
}

func (q *QuickNoteController) loadFromDisk() {
	dir := q.quickDir()
	if dir == "" {
		return
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "names.json")); err == nil {
		var meta quickMeta
		if json.Unmarshal(raw, &meta) == nil {
			for i := 0; i < quickSlotCount; i++ {
				if i < len(meta.Names) && strings.TrimSpace(meta.Names[i]) != "" && !(i == 0 && meta.Names[i] == "速记 1") {
					q.names[i] = meta.Names[i]
				}
			}
			if meta.Active >= 0 && meta.Active < quickSlotCount {
				q.activeSlot = meta.Active
			}
		}
	}
	for i := 0; i < quickSlotCount; i++ {
		if b, err := os.ReadFile(q.slotFilePath(i)); err == nil {
			q.slots[i] = string(b)
		}
	}
	q.ensureSlotFileLocked(q.activeSlot)
}

func (q *QuickNoteController) saveMetaLocked() {
	dir := q.quickDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	meta := quickMeta{
		Names:  q.names[:],
		Active: q.activeSlot,
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}
	target := filepath.Join(dir, "names.json")
	tmp := target + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, target)
	}
}

func (q *QuickNoteController) saveSlotLocked(slot int) error {
	if slot < 0 || slot >= quickSlotCount {
		return nil
	}
	q.writeSeq[slot].Add(1)
	p := q.slotFilePath(slot)
	if p == "" {
		return errors.New("missing Quick Note data directory")
	}
	q.diskMu.Lock()
	defer q.diskMu.Unlock()
	_, err := files.WriteNoteAtomic(p, q.slots[slot])
	return err
}

func isFiniteFloat32(vals ...float32) bool {
	for _, v := range vals {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return false
		}
	}
	return true
}

func displayWorkArea(d mygo.Display) mygo.Rectangle {
	if d.WorkArea.Width > 0 && d.WorkArea.Height > 0 {
		return d.WorkArea
	}
	return d.Bounds
}

func displayOverlapArea(a, b mygo.Rectangle) int64 {
	x1 := max(a.X, b.X)
	y1 := max(a.Y, b.Y)
	x2 := min(a.X+a.Width, b.X+b.Width)
	y2 := min(a.Y+a.Height, b.Y+b.Height)
	if x2 <= x1 || y2 <= y1 {
		return 0
	}
	return int64(x2-x1) * int64(y2-y1)
}

func findDisplayForSavedBounds(saved model.SavedWindowBounds, displays []mygo.Display) (mygo.Display, bool) {
	if len(displays) == 0 {
		return mygo.Display{}, false
	}
	if saved.DisplayID != nil {
		wantID := *saved.DisplayID
		for _, d := range displays {
			if uint32(d.ID) == wantID {
				return d, true
			}
		}
	} else {
		r := mygo.Rectangle{
			X:      int(math.Round(float64(saved.X))),
			Y:      int(math.Round(float64(saved.Y))),
			Width:  int(math.Round(float64(saved.Width))),
			Height: int(math.Round(float64(saved.Height))),
		}
		var bestArea int64
		var best mygo.Display
		for _, d := range displays {
			area := displayOverlapArea(displayWorkArea(d), r)
			if area > bestArea {
				bestArea = area
				best = d
			}
		}
		if bestArea > 0 {
			return best, true
		}
	}
	for _, d := range displays {
		if d.Primary {
			return d, true
		}
	}
	return displays[0], true
}

func resolveQuickWindowBoundsFromDisplays(saved *model.SavedWindowBounds, displays []mygo.Display, cursor *mygo.Point) (mygo.Rectangle, bool) {
	if saved != nil &&
		isFiniteFloat32(saved.X, saved.Y, saved.Width, saved.Height) &&
		saved.Width > 0 && saved.Height > 0 {
		width := max(saved.Width, float32(quickMinWidth))
		height := max(saved.Height, float32(quickMinHeight))
		x := saved.X
		y := saved.Y

		if d, ok := findDisplayForSavedBounds(*saved, displays); ok {
			area := displayWorkArea(d)
			dx, dy := float32(area.X), float32(area.Y)
			dw, dh := float32(area.Width), float32(area.Height)
			if dw >= float32(quickMinWidth) {
				width = min(width, dw)
			}
			if dh >= float32(quickMinHeight) {
				height = min(height, dh)
			}
			minX := dx - width + 80
			maxX := max(dx+dw-80, minX)
			minY := dy
			maxY := max(dy+dh-60, minY)
			x = min(max(x, minX), maxX)
			y = min(max(y, minY), maxY)
		}

		return mygo.Rectangle{
			X:      int(math.Round(float64(x))),
			Y:      int(math.Round(float64(y))),
			Width:  int(math.Round(float64(width))),
			Height: int(math.Round(float64(height))),
		}, true
	}

	if len(displays) > 0 {
		target := displays[0]
		found := false
		if cursor != nil {
			for _, d := range displays {
				b := d.Bounds
				if cursor.X >= b.X && cursor.X < b.X+b.Width && cursor.Y >= b.Y && cursor.Y < b.Y+b.Height {
					target = d
					found = true
					break
				}
			}
		}
		if !found {
			for _, d := range displays {
				if d.Primary {
					target = d
					break
				}
			}
		}
		area := displayWorkArea(target)
		if area.Width > 0 && area.Height > 0 {
			w := min(quickDefaultWidth, max(area.Width, quickMinWidth))
			h := min(quickDefaultHeight, max(area.Height, quickMinHeight))
			return mygo.Rectangle{
				X:      area.X + max((area.Width-w)/2, 0),
				Y:      area.Y + max((area.Height-h)/2, 0),
				Width:  w,
				Height: h,
			}, false
		}
	}

	return mygo.Rectangle{Width: quickDefaultWidth, Height: quickDefaultHeight}, false
}

func isRectVisibleOnDisplays(r mygo.Rectangle, displays []mygo.Display) bool {
	if len(displays) == 0 {
		return true
	}
	for _, d := range displays {
		a := displayWorkArea(d)
		iw := min(a.X+a.Width, r.X+r.Width) - max(a.X, r.X)
		ih := min(a.Y+a.Height, r.Y+r.Height) - max(a.Y, r.Y)
		if iw >= min(80, r.Width) && ih >= min(60, r.Height) {
			return true
		}
	}
	return false
}

func captureQuickWindowBounds(win *mygo.Window) (model.SavedWindowBounds, bool) {
	if win == nil {
		return model.SavedWindowBounds{}, false
	}
	r := win.Bounds()
	if r.Width < 320 || r.Height < 260 {
		return model.SavedWindowBounds{}, false
	}
	b := model.SavedWindowBounds{
		X:      float32(r.X),
		Y:      float32(r.Y),
		Width:  float32(r.Width),
		Height: float32(r.Height),
	}
	if mygo.App.IsReady() {
		d := mygo.Screen.DisplayMatching(r)
		if d.ID != 0 || d.Bounds.Width > 0 {
			did := uint32(d.ID)
			b.DisplayID = &did
		}
	}
	return b, true
}

func (q *QuickNoteController) currentSavedBounds() *model.SavedWindowBounds {
	q.mu.Lock()
	var cached *model.SavedWindowBounds
	if q.savedBounds != nil {
		cp := *q.savedBounds
		cached = &cp
	}
	getFn := q.GetQuickBounds
	q.mu.Unlock()
	if cached != nil {
		return cached
	}
	if getFn != nil {
		return getFn()
	}
	return nil
}

func (q *QuickNoteController) resolveBounds() (mygo.Rectangle, bool) {
	saved := q.currentSavedBounds()
	if !mygo.App.IsReady() {
		return resolveQuickWindowBoundsFromDisplays(saved, nil, nil)
	}
	displays := mygo.Screen.Displays()
	pt := mygo.Screen.CursorScreenPoint()
	return resolveQuickWindowBoundsFromDisplays(saved, displays, &pt)
}

func (q *QuickNoteController) persistBoundsNow(win *mygo.Window) {
	b, ok := captureQuickWindowBounds(win)
	if !ok {
		return
	}
	q.mu.Lock()
	cp := b
	q.savedBounds = &cp
	onBounds := q.OnQuickBoundsChanged
	q.mu.Unlock()
	if onBounds != nil {
		onBounds(b)
	}
}

func (q *QuickNoteController) scheduleSaveBounds() {
	q.mu.Lock()
	if !q.visible || q.win == nil {
		q.mu.Unlock()
		return
	}
	if q.boundsTimer != nil {
		q.boundsTimer.Reset(quickBoundsDebounce)
		q.mu.Unlock()
		return
	}
	q.boundsTimer = time.AfterFunc(quickBoundsDebounce, func() {
		mygo.RunOnMain(func() {
			q.mu.Lock()
			q.boundsTimer = nil
			vis := q.visible
			w := q.win
			q.mu.Unlock()
			if vis && w != nil {
				q.persistBoundsNow(w)
			}
		})
	})
	q.mu.Unlock()
}

// AttachDesktop registers the standalone floating Quick Note panel window.
// Red-light close is intercepted as Hide() so the main window is unaffected.
func (q *QuickNoteController) AttachDesktop() {
	q.mu.Lock()
	already := q.win != nil
	q.mu.Unlock()
	if already {
		return
	}

	create := func() {
		q.mu.Lock()
		if q.win != nil {
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()

		rect, hasExplicit := q.resolveBounds()

		win := mygo.NewWindow(mygo.WindowOptions{
			Title:             theme.Tr("速记", "Quick Note"),
			X:                 rect.X,
			Y:                 rect.Y,
			Width:             rect.Width,
			Height:            rect.Height,
			MinWidth:          quickMinWidth,
			MinHeight:         quickMinHeight,
			Hidden:            true,
			Panel:             true,
			AlwaysOnTop:       true,
			TitleBarStyle:     mygo.TitleBarHidden,
			HideWindowButtons: true,
			BackgroundColor:   "light-dark(#ffffff, #161920)",
			Content:           ui.View(q.RenderView),
			Page:              mygo.PageOptions{DevTools: mygo.DevToolsDisabled},
		})
		if hasExplicit || rect.X != 0 || rect.Y != 0 {
			win.SetBounds(rect)
		}
		win.OnFocus(func() {
			q.mu.Lock()
			sess := q.sessions[q.activeSlot]
			q.mu.Unlock()
			if sess != nil {
				sess.EnsureEditorFocus()
			}
			win.Invalidate()
		})
		win.OnMove(func() { q.scheduleSaveBounds() })
		win.OnResize(func() { q.scheduleSaveBounds() })
		win.OnFileDrop(func(e *mygo.FileDropEvent) {
			if e != nil && len(e.Paths) > 0 {
				q.ImportDroppedFiles(e.Paths)
			}
		})
		win.OnClose(func(e *mygo.CloseEvent) {
			if IsAppQuitting() {
				q.Flush()
				return
			}
			e.PreventDefault()
			q.Hide()
		})

		q.mu.Lock()
		q.win = win
		vis := q.visible
		q.mu.Unlock()
		if vis {
			win.Show()
			win.Focus()
		}
	}

	if mygo.App.IsReady() {
		create()
	} else {
		mygo.App.WhenReady(create)
	}
}

// Invalidate requests a redraw of the Quick Note window and refreshes its localized title.
func (q *QuickNoteController) Invalidate() {
	q.mu.Lock()
	win := q.win
	q.mu.Unlock()
	if win != nil {
		win.Invalidate()
	}
}

// RefreshStyle updates the Quick Note window title and invalidates its surface after theme/font changes.
func (q *QuickNoteController) RefreshStyle() {
	q.mu.Lock()
	win := q.win
	q.mu.Unlock()
	if win != nil {
		win.SetTitle(theme.Tr("速记", "Quick Note"))
		win.Invalidate()
	}
}

// IsVisible reports whether the Quick Note panel is currently visible.
func (q *QuickNoteController) IsVisible() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.visible
}

// IsFocused reports whether the Quick Note panel currently has keyboard focus.
func (q *QuickNoteController) IsFocused() bool {
	q.mu.Lock()
	win := q.win
	vis := q.visible
	q.mu.Unlock()
	return vis && win != nil && win.IsFocused()
}

// Show displays and focuses the Quick Note panel, restoring/clamping its cross-screen bounds.
func (q *QuickNoteController) Show() {
	q.mu.Lock()
	q.visible = true
	q.ensureSlotFileLocked(q.activeSlot)
	sess := q.sessions[q.activeSlot]
	win := q.win
	q.mu.Unlock()

	if sess != nil {
		sess.EnsureEditorFocus()
	}
	if win != nil {
		rect, hasExplicit := q.resolveBounds()
		cur := win.Bounds()
		offScreen := mygo.App.IsReady() && !isRectVisibleOnDisplays(cur, mygo.Screen.Displays())
		if offScreen || hasExplicit && (absInt(cur.X-rect.X) > 2 || absInt(cur.Y-rect.Y) > 2 || absInt(cur.Width-rect.Width) > 2 || absInt(cur.Height-rect.Height) > 2) {
			win.SetBounds(rect)
		}
		win.Show()
		win.Focus()
		win.Invalidate()
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Hide flushes the active slot and hides the Quick Note panel without closing the app.
func (q *QuickNoteController) Hide() {
	q.mu.Lock()
	q.visible = false
	if q.boundsTimer != nil {
		q.boundsTimer.Stop()
		q.boundsTimer = nil
	}
	if q.syncTimer != nil {
		q.syncTimer.Stop()
		q.syncTimer = nil
	}
	q.syncSessionToDiskLocked(q.activeSlot)
	q.saveMetaLocked()
	win := q.win
	q.mu.Unlock()

	if win != nil {
		q.persistBoundsNow(win)
		win.Hide()
	}
}

// Flush saves active slot content, metadata, and window bounds before application exit.
func (q *QuickNoteController) Flush() error {
	q.mu.Lock()
	if q.boundsTimer != nil {
		q.boundsTimer.Stop()
		q.boundsTimer = nil
	}
	if q.syncTimer != nil {
		q.syncTimer.Stop()
		q.syncTimer = nil
	}
	var saveErr error
	for i := 0; i < quickSlotCount; i++ {
		if i == q.activeSlot || (q.sessions[i] != nil && q.sessions[i].Dirty()) {
			saveErr = errors.Join(saveErr, q.syncSessionToDiskLocked(i))
		}
	}
	q.saveMetaLocked()
	win := q.win
	vis := q.visible
	q.mu.Unlock()

	if win != nil && vis {
		q.persistBoundsNow(win)
	}
	return saveErr
}

// Toggle toggles the Quick Note panel: if already visible AND focused (or in headless mode),
// hides it; if visible in the background without focus, brings it to focus (matching thinkly-fast).
func (q *QuickNoteController) Toggle() {
	q.mu.Lock()
	vis := q.visible
	win := q.win
	q.mu.Unlock()

	if !vis {
		q.Show()
		return
	}
	if win != nil && mygo.App.IsReady() && (!win.IsVisible() || !win.IsFocused()) {
		q.Show()
		return
	}
	q.Hide()
}

// TriggerHotkey toggles the panel with a built-in 180ms debounce window.
// Returns false when called again within 180ms of the previous accepted trigger.
func (q *QuickNoteController) TriggerHotkey() bool {
	now := time.Now()
	q.mu.Lock()
	if !q.lastTrigger.IsZero() && now.Sub(q.lastTrigger) < quickHotkeyDebounce {
		q.mu.Unlock()
		return false
	}
	q.lastTrigger = now
	q.mu.Unlock()

	q.Toggle()
	return true
}

// Session returns the minimal block editor Session for the active Quick Note slot.
func (q *QuickNoteController) Session() *editor.Session {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.sessions[q.activeSlot]
}

// OpenFind triggers find or find-and-replace in the active Quick Note slot.
func (q *QuickNoteController) OpenFind(replace bool) {
	q.mu.Lock()
	sess := q.sessions[q.activeSlot]
	win := q.win
	q.mu.Unlock()
	if sess == nil {
		return
	}
	if replace {
		sess.OpenFindReplace()
	} else {
		sess.ToggleFind()
	}
	if win != nil {
		win.Invalidate()
	}
}

// Text returns the Markdown text of the currently active Quick Note slot.
func (q *QuickNoteController) Text() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if sess := q.sessions[q.activeSlot]; sess != nil && sess.Dirty() {
		q.slots[q.activeSlot] = q.serializeSessionLocked(q.activeSlot)
		sess.MarkSaved()
		q.saveSlotLocked(q.activeSlot)
	}
	return q.slots[q.activeSlot]
}

// SetText updates the active Quick Note slot text, refreshes its block editor, and persists it to disk.
func (q *QuickNoteController) SetText(s string) {
	q.mu.Lock()
	slot := q.activeSlot
	q.slots[slot] = s
	if sess := q.sessions[slot]; sess != nil {
		sess.SetDoc(q.codec.Parse(s))
		sess.EnsureEditorFocus()
	}
	q.saveSlotLocked(slot)
	win := q.win
	q.mu.Unlock()

	if win != nil {
		win.Invalidate()
	}
}

// ActiveSlot returns the currently selected Quick Note slot index (0..2).
func (q *QuickNoteController) ActiveSlot() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.activeSlot
}

// ImportDroppedFiles imports local images, videos, and arbitrary attachment files into the active Quick Note slot.
func (q *QuickNoteController) ImportDroppedFiles(paths []string) int {
	q.mu.Lock()
	slot := q.activeSlot
	q.ensureSlotFileLocked(slot)
	sess := q.sessions[slot]
	win := q.win
	q.mu.Unlock()

	if sess == nil {
		return 0
	}
	n := sess.ImportFiles(paths, 0, false)
	if n > 0 {
		q.mu.Lock()
		q.syncSessionToDiskLocked(slot)
		q.mu.Unlock()
		if win != nil {
			win.Invalidate()
		}
	}
	return n
}

// ImportDroppedImages imports local image files into the active Quick Note slot.
func (q *QuickNoteController) ImportDroppedImages(paths []string) int {
	q.mu.Lock()
	slot := q.activeSlot
	q.ensureSlotFileLocked(slot)
	sess := q.sessions[slot]
	win := q.win
	q.mu.Unlock()

	if sess == nil {
		return 0
	}
	n := sess.ImportImageFiles(paths, 0, false)
	if n > 0 {
		q.mu.Lock()
		q.syncSessionToDiskLocked(slot)
		q.mu.Unlock()
		if win != nil {
			win.Invalidate()
		}
	}
	return n
}

func (q *QuickNoteController) pickImagesForSlot(slot int, blockID model.BlockID, replace bool) {
	if !mygo.App.IsReady() {
		return
	}
	q.mu.Lock()
	win := q.win
	q.mu.Unlock()
	go func() {
		paths, err := files.OpenDialog(win, mygo.OpenDialogOptions{
			Title:    theme.Tr("导入图片", "Import Image"),
			Multiple: true,
			Filters: []mygo.FileFilter{
				{
					Name:       theme.Tr("图片", "Images"),
					Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif"},
				},
			},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		mygo.RunOnMain(func() {
			q.mu.Lock()
			q.ensureSlotFileLocked(slot)
			sess := q.sessions[slot]
			win := q.win
			q.mu.Unlock()
			if sess == nil {
				return
			}
			if sess.ImportImageFiles(paths, blockID, replace) > 0 {
				q.mu.Lock()
				q.syncSessionToDiskLocked(slot)
				q.mu.Unlock()
				if win != nil {
					win.Focus()
					win.Invalidate()
				}
			}
		})
	}()
}

// OnWheelScroll switches Quick Note slots from standard vertical/horizontal scroll deltas
// (mygo ui.InputScroll convention: positive DY scrolls down -> next slot; negative DY -> previous slot).
func (q *QuickNoteController) OnWheelScroll(delta float32) int {
	return q.OnShiftScroll(-delta)
}

// OnShiftScroll accumulates scroll deltas (16px threshold, 160ms cooldown)
// and switches slots: negative delta advances to (active+1)%3, positive moves to (active+2)%3.
func (q *QuickNoteController) OnShiftScroll(delta float32) int {
	if math.Abs(float64(delta)) < 0.5 {
		return q.ActiveSlot()
	}
	now := time.Now()
	q.mu.Lock()
	expired := !q.lastWheelEvent.IsZero() && now.Sub(q.lastWheelEvent) > 180*time.Millisecond
	if expired || (q.wheelAccum != 0 && delta*q.wheelAccum < 0) {
		q.wheelAccum = 0
	}
	q.lastWheelEvent = now
	if !q.lastWheelSwitch.IsZero() && now.Sub(q.lastWheelSwitch) < 160*time.Millisecond {
		active := q.activeSlot
		q.mu.Unlock()
		return active
	}
	q.wheelAccum += delta
	if math.Abs(float64(q.wheelAccum)) < 16 {
		active := q.activeSlot
		q.mu.Unlock()
		return active
	}
	step := 1
	if q.wheelAccum > 0 {
		step = quickSlotCount - 1
	}
	q.wheelAccum = 0
	q.lastWheelSwitch = now
	next := (q.activeSlot + step) % quickSlotCount
	q.mu.Unlock()

	q.switchSlot(next)
	return next
}

func (q *QuickNoteController) switchSlot(slot int) {
	if slot < 0 || slot >= quickSlotCount {
		return
	}
	q.mu.Lock()
	if q.activeSlot != slot {
		q.syncSessionToDiskLocked(q.activeSlot)
		q.activeSlot = slot
		q.ensureSlotFileLocked(slot)
		q.noticeSeq++
		if q.noticeTimer != nil {
			q.noticeTimer.Stop()
			q.noticeTimer = nil
		}
		q.notice = ""
		q.savedFlash = false
		q.noticeUntil = time.Time{}
		q.saveMetaLocked()
	}
	sess := q.sessions[slot]
	win := q.win
	q.mu.Unlock()

	if sess != nil {
		sess.EnsureEditorFocus()
	}
	if win != nil {
		win.Invalidate()
	}
}

// SaveToNotebook exports the active slot's Markdown as a new note inside the active notebook.
func (q *QuickNoteController) SaveToNotebook() (string, error) {
	q.mu.Lock()
	slot := q.activeSlot
	if err := q.syncSessionToDiskLocked(slot); err != nil {
		q.mu.Unlock()
		q.setNotice(err.Error(), false)
		return "", err
	}
	name := q.names[slot]
	raw := q.slots[slot]
	doc := (*model.Doc)(nil)
	if q.sessions[slot] != nil {
		doc = q.sessions[slot].Doc()
	}
	q.mu.Unlock()

	if strings.TrimSpace(raw) == "" {
		msg := theme.Tr("速记为空，无需保存", "Quick note is empty")
		q.setNotice(msg, false)
		return "", errors.New(msg)
	}

	folder := q.targetNotebookFolder()
	if folder == "" {
		msg := theme.Tr("请先在主窗口选择文件夹", "Choose a folder in the main window first")
		q.setNotice(msg, false)
		return "", errors.New(msg)
	}

	q.copyReferencedAssetsToNotebook(doc, folder)

	content := raw
	if q.codec != nil {
		parsed := q.codec.Parse(raw)
		if serialized := q.codec.Serialize(parsed); serialized != "" {
			content = serialized
		}
	}

	title := quickNoteTitle(slot, name, raw)
	outPath, err := createUniqueQuickNoteFile(folder, title, content)
	if err != nil {
		q.setNotice(err.Error(), false)
		return "", err
	}

	if q.host != nil {
		if err := q.host.WriteNote(outPath, content); err != nil {
			_ = os.Remove(outPath)
			q.setNotice(err.Error(), false)
			return "", err
		}
		q.host.RequestRedraw()
	}

	q.SetText("")
	q.mu.Lock()
	q.saveFlashAt = time.Now()
	q.mu.Unlock()
	q.setNotice(theme.Tr("已存入 ", "Saved to ")+filepath.Base(outPath), true)
	if q.OnSavedNote != nil {
		q.OnSavedNote(outPath)
	}
	return outPath, nil
}

func (q *QuickNoteController) copyReferencedAssetsToNotebook(doc *model.Doc, notebookFolder string) {
	if doc == nil || notebookFolder == "" {
		return
	}
	qDir := q.quickDir()
	if qDir == "" {
		return
	}
	source, err := os.OpenRoot(qDir)
	if err != nil {
		return
	}
	defer source.Close()
	dest, err := os.OpenRoot(notebookFolder)
	if err != nil {
		return
	}
	defer dest.Close()
	copyRel := func(rawRef string) {
		rel := filepath.Clean(filepath.FromSlash(strings.TrimSpace(rawRef)))
		if !filepath.IsLocal(rel) || !strings.HasPrefix(rel, "assets"+string(filepath.Separator)) {
			return
		}
		if err := copyQuickFile(source, dest, rel); err != nil && !errors.Is(err, os.ErrExist) {
			files.LogWarnf("copy Quick Note attachment %q: %v", rel, err)
		}
	}
	for _, b := range doc.Blocks {
		if b.Kind == model.BlockImage {
			copyRel(b.ImageSrc)
		}
		for _, sp := range b.Spans {
			if sp.Link != "" {
				copyRel(sp.Link)
			}
		}
		if b.Table != nil {
			for _, row := range b.Table.Rows {
				for _, cell := range row {
					for _, sp := range cell.Spans {
						if sp.Link != "" {
							copyRel(sp.Link)
						}
					}
				}
			}
		}
	}
}

func copyQuickFile(source, dest *os.Root, name string) error {
	const maxAttachmentBytes = 64 << 20
	info, err := source.Stat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxAttachmentBytes {
		return errors.New("invalid Quick Note attachment")
	}
	in, err := source.Open(name)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := dest.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	out, err := dest.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(in, maxAttachmentBytes+1))
	closeErr := out.Close()
	if n > maxAttachmentBytes {
		copyErr = errors.New("Quick Note attachment is too large")
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = dest.Remove(name)
		return err
	}
	return nil
}

func (q *QuickNoteController) targetNotebookFolder() string {
	if q.host == nil {
		return ""
	}
	if active := strings.TrimSpace(q.host.ActiveNotebook()); active != "" {
		if st, err := os.Stat(active); err == nil && st.IsDir() {
			return active
		}
	}
	for _, nb := range q.host.Notebooks() {
		nb = strings.TrimSpace(nb)
		if nb == "" {
			continue
		}
		if st, err := os.Stat(nb); err == nil && st.IsDir() {
			return nb
		}
	}
	return ""
}

func (q *QuickNoteController) setNotice(text string, flash bool) {
	q.mu.Lock()
	q.notice = text
	q.savedFlash = flash
	q.noticeSeq++
	seq := q.noticeSeq
	if q.noticeTimer != nil {
		q.noticeTimer.Stop()
		q.noticeTimer = nil
	}
	if text != "" || flash {
		q.noticeUntil = time.Now().Add(quickNoticeTTL)
		q.noticeTimer = time.AfterFunc(quickNoticeTTL, func() {
			q.clearNoticeIfSeq(seq)
		})
	} else {
		q.noticeUntil = time.Time{}
	}
	win := q.win
	q.mu.Unlock()

	if win != nil {
		win.Invalidate()
	}
}

func (q *QuickNoteController) clearNoticeIfSeq(seq uint64) {
	q.mu.Lock()
	if q.noticeSeq != seq {
		q.mu.Unlock()
		return
	}
	q.notice = ""
	q.savedFlash = false
	q.noticeUntil = time.Time{}
	q.noticeTimer = nil
	win := q.win
	q.mu.Unlock()

	if win != nil {
		win.Invalidate()
	}
}

func snapQuickZoomPct(val float32) int {
	if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) || val <= 0 {
		return quickDefaultZoomPct
	}
	pct := int(math.Round(float64(val * 100)))
	pct = max(quickMinZoomPct, min(pct, quickMaxZoomPct))
	snapped := ((pct + 5) / 10) * 10
	return max(quickMinZoomPct, min(snapped, quickMaxZoomPct))
}

func (q *QuickNoteController) currentZoom() float32 {
	if q.GetQuickZoom != nil {
		if z := q.GetQuickZoom(); z > 0 {
			return float32(snapQuickZoomPct(z)) / 100.0
		}
	}
	q.mu.Lock()
	z := q.zoom
	q.mu.Unlock()
	if z <= 0 {
		return 1.0
	}
	return float32(snapQuickZoomPct(z)) / 100.0
}

// StepZoom adjusts Quick Note editor zoom in 10% increments within [70%, 200%].
func (q *QuickNoteController) StepZoom(deltaPct int) float32 {
	cur := snapQuickZoomPct(q.currentZoom())
	next := max(quickMinZoomPct, min(cur+deltaPct, quickMaxZoomPct))
	return q.setQuickZoom(float32(next) / 100.0)
}

// ResetZoom resets Quick Note editor zoom back to 100%.
func (q *QuickNoteController) ResetZoom() float32 {
	return q.setQuickZoom(1.0)
}

func (q *QuickNoteController) setQuickZoom(z float32) float32 {
	z = float32(snapQuickZoomPct(z)) / 100.0
	q.mu.Lock()
	q.zoom = z
	win := q.win
	q.mu.Unlock()
	if q.OnQuickZoomChanged != nil {
		q.OnQuickZoomChanged(z)
	}
	if win != nil {
		win.Invalidate()
	}
	return z
}

func quickNoteTitle(slot int, slotName, content string) string {
	if !isDefaultQuickSlotName(slot, slotName) {
		if clean := sanitizeQuickTitle(slotName); clean != "" {
			return clean
		}
	}
	body := stripQuickFrontmatter(content)
	if strings.TrimSpace(body) == "" {
		body = content
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(strings.TrimSpace(line), "#>-* ")
		if clean := sanitizeQuickTitle(trimmed); clean != "" {
			return clean
		}
	}
	return theme.Tr("速记", "Quick Note")
}

func stripQuickFrontmatter(src string) string {
	if !strings.HasPrefix(src, "---\n") && !strings.HasPrefix(src, "---\r\n") {
		return src
	}
	rest := src[4:]
	if idx := strings.Index(rest, "\n---"); idx >= 0 {
		after := rest[idx+4:]
		if nl := strings.IndexByte(after, '\n'); nl >= 0 {
			return after[nl+1:]
		}
		return ""
	}
	return src
}

func sanitizeQuickTitle(raw string) string {
	var b strings.Builder
	count := 0
	for _, r := range raw {
		switch r {
		case '/', '\\', ':', '\x00', '\n', '\r', '*', '?', '"', '<', '>', '|':
			continue
		}
		b.WriteRune(r)
		count++
		if count >= 40 {
			break
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimLeft(out, ".")
	out = strings.TrimSpace(out)
	if !utf8.ValidString(out) {
		return ""
	}
	return out
}

func createUniqueQuickNoteFile(folder, title, content string) (string, error) {
	for n := 0; n < 200; n++ {
		name := title + ".md"
		if n > 0 {
			name = fmt.Sprintf("%s %d.md", title, n+1)
		}
		cand := filepath.Join(folder, name)
		f, err := os.OpenFile(cand, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		_, wErr := f.WriteString(content)
		cErr := f.Close()
		if wErr != nil {
			_ = os.Remove(cand)
			return "", wErr
		}
		if cErr != nil {
			_ = os.Remove(cand)
			return "", cErr
		}
		return cand, nil
	}
	return "", errors.New(theme.Tr("无法分配唯一笔记名称", "Could not allocate a unique note name"))
}

// RenderView renders the standalone Quick Note panel UI.
func (q *QuickNoteController) RenderView(c *ui.Context) {
	theme.WithSurface("paper", func() {
		q.renderViewPaper(c)
	})
}

func (q *QuickNoteController) renderViewPaper(c *ui.Context) {
	pal := theme.Ocean()
	uiTheme := pal.ToUITheme("")
	uiTheme.ScrollbarStyle = internalui.StandardScrollbarStyle(pal)
	c.SetTheme(&uiTheme)

	q.mu.Lock()
	activeSlot := q.activeSlot
	sess := q.sessions[activeSlot]
	q.mu.Unlock()

	if !theme.IsMacOS() && c.Shortcut(ui.Ctrl, ui.KeyQ) {
		if mygo.App.IsReady() {
			mygo.App.Quit()
		}
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyW) || (!theme.IsMacOS() && c.Shortcut(ui.Ctrl, ui.KeyF4)) {
		q.Hide()
		return
	}
	if q.GetQuickShortcut != nil {
		if mods, key, ok := internalui.ParseShortcut(q.GetQuickShortcut()); ok && c.Shortcut(mods, key) {
			q.TriggerHotkey()
			return
		}
	}
	if c.Shortcut(0, ui.KeyEscape) {
		if sess != nil && sess.DismissTransientOverlay() {
			q.syncActiveSlotIfDirty()
			return
		}
		q.Hide()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyP) ||
		c.Shortcut(ui.Cmd|ui.Alt, ui.KeyK) ||
		c.Shortcut(ui.Cmd|ui.Shift, ui.KeyK) ||
		c.Shortcut(ui.Cmd, ui.KeyK) {
		if q.OnSummonSearch != nil {
			q.OnSummonSearch()
		}
		return
	}
	if c.Shortcut(ui.Cmd, ui.Key1) {
		q.switchSlot(0)
		activeSlot = 0
	} else if c.Shortcut(ui.Cmd, ui.Key2) {
		q.switchSlot(1)
		activeSlot = 1
	} else if c.Shortcut(ui.Cmd, ui.Key3) {
		q.switchSlot(2)
		activeSlot = 2
	} else if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS) {
		_, _ = q.SaveToNotebook()
	}

	zoom := q.currentZoom()
	if c.Shortcut(ui.Cmd, ui.KeyEqual) || c.Shortcut(ui.Cmd|ui.Shift, ui.KeyEqual) {
		zoom = q.StepZoom(quickStepZoomPct)
	} else if c.Shortcut(ui.Cmd, ui.KeyMinus) || c.Shortcut(ui.Cmd|ui.Shift, ui.KeyMinus) {
		zoom = q.StepZoom(-quickStepZoomPct)
	} else if c.Shortcut(ui.Cmd, ui.Key0) {
		zoom = q.ResetZoom()
	}
	if c.Zoom() != zoom {
		c.SetZoom(zoom)
	}

	now := time.Now()
	q.mu.Lock()
	if (q.notice != "" || q.savedFlash) && !q.noticeUntil.IsZero() && !now.Before(q.noticeUntil) {
		q.notice = ""
		q.savedFlash = false
		q.noticeUntil = time.Time{}
		q.noticeSeq++
		if q.noticeTimer != nil {
			q.noticeTimer.Stop()
			q.noticeTimer = nil
		}
	}
	activeSlot = q.activeSlot
	sess = q.sessions[activeSlot]
	notice := q.notice
	savedFlash := q.savedFlash
	noticeUntil := q.noticeUntil
	saveFlashAt := q.saveFlashAt
	q.mu.Unlock()

	if (notice != "" || savedFlash) && !noticeUntil.IsZero() {
		if rem := noticeUntil.Sub(now); rem > 0 {
			c.After(rem)
		}
	}

	winW, winH := c.Size()
	if winW <= 0 {
		winW = quickDefaultWidth
	}
	if winH <= 0 {
		winH = quickDefaultHeight
	}
	if sess != nil {
		sess.SetMinCanvasHeight(max(winH-44, 0))
	}

	var qnSkin *theme.ThemeSkinQuickNote
	if skin := theme.ActiveSkin(); skin != nil {
		qnSkin = skin.QuickNote
	}

	root := ui.Column(c.Key("quick-note-root")).Fill().Background(pal.Paper).TextColor(pal.Text).Clip()
	if qnSkin != nil && strings.TrimSpace(qnSkin.BorderAccent) != "" {
		if borderAccent, err := theme.ParseHexColor(qnSkin.BorderAccent); err == nil {
			root.Border(1, borderAccent)
		}
	}
	if qnSkin != nil && strings.TrimSpace(qnSkin.FlashAccent) != "" && !saveFlashAt.IsZero() {
		if el := now.Sub(saveFlashAt).Seconds(); el >= 0 && el < 0.9 {
			if flashColor, err := theme.ParseHexColor(qnSkin.FlashAccent); err == nil {
				alpha := float32(1.0 - el/0.9)
				root.DrawOver(func(p *ui.Painter, r ui.Rect) {
					if r.W <= 2 || r.H <= 2 {
						return
					}
					p.Stroke(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, flashColor.Alpha(0.75*alpha), 0, 2)
					p.AnimationFrameRate(60)
				})
				c.After(16 * time.Millisecond)
			}
		}
	}
	internalui.Probe(root, "quick-note-root")
	root.Children(func() {
		headerBG := pal.Paper
		headerDark := pal.Dark
		if qnSkin != nil && strings.TrimSpace(qnSkin.HeaderTint) != "" {
			if tint, err := theme.ParseHexColor(qnSkin.HeaderTint); err == nil {
				headerBG = tint
				lum := (0.299*float64(tint.R) + 0.587*float64(tint.G) + 0.114*float64(tint.B)) / 255.0
				headerDark = lum <= 0.5
			}
		}
		// Minimalist 44px V1 drag header: quiet top-left dismiss dot, centered recessed-track bead pager, single Duo save action on right.
		header := ui.Row(c.Key("quick-header")).FillWidth().Height(44).Shrink(0).
			Padding(0, 12, 0, 16).AlignItems(ui.Center).Justify(ui.SpaceBetween).
			Background(headerBG).KeepFocus()
		if qnSkin != nil && strings.TrimSpace(qnSkin.BorderAccent) != "" {
			if borderAccent, err := theme.ParseHexColor(qnSkin.BorderAccent); err == nil {
				header.BorderWidth(0, 0, 1, 0).BorderColor(borderAccent.Alpha(0.35))
			}
		}
		header.DragWindow()
		internalui.Probe(header, "quick-header")
		header.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputScroll || ev.Momentum {
				return false
			}
			if ev.Mods&(ui.Ctrl|ui.Alt|ui.Super) != 0 {
				return false
			}
			if sess != nil && sess.HasScrollablePopupOpen() {
				return false
			}
			axis := ev.DY
			if math.Abs(float64(ev.DX)) > math.Abs(float64(ev.DY)) {
				axis = ev.DX
			}
			if math.Abs(float64(axis)) < 0.5 {
				return false
			}
			q.OnWheelScroll(axis)
			return true
		})
		header.Children(func() {
			badgeText := ""
			if qnSkin != nil {
				badgeText = strings.TrimSpace(qnSkin.BadgeText.Resolve(string(theme.Language())))
			}
			leftRow := ui.Row(c.Key("quick-header-left")).AlignItems(ui.Center).Gap(theme.Space8)
			leftRow.Children(func() {
				closeBtn := internalui.WindowDismissDot(
					c,
					"quick-close",
					theme.Tr("关闭", "Close"),
					theme.Tr("隐藏速记", "Hide Quick Note"),
					"Esc",
				).Margin(0, 0, 0, -4).KeepFocus()
				if closeBtn.Clicked() {
					q.Hide()
				}
				if badgeText != "" {
					badgeFG := pal.TextSecondary
					badgeBG := pal.Hover
					if headerDark && !pal.Dark {
						badgeFG = ui.Hex("#e8dec6")            // design:allow dark skin header badge
						badgeBG = ui.RGBA(255, 255, 255, 0.12) // design:allow dark skin header badge
					}
					if qnSkin != nil && strings.TrimSpace(qnSkin.BorderAccent) != "" {
						if accent, err := theme.ParseHexColor(qnSkin.BorderAccent); err == nil {
							badgeFG = accent
							badgeBG = accent.Alpha(0.14)
						}
					}
					ui.Row(c.Key("quick-skin-badge")).Height(20).PaddingX(theme.Space8).AlignItems(ui.Center).
						Radius(theme.RadiusPill).Background(badgeBG).Children(func() {
						ui.Text(c, badgeText).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(badgeFG).SingleLine()
					})
				}
			})

			// Centered 3-slot recessed-track bead pager inside a pass-through layer so drag & side actions work.
			ui.Row(c.Key("quick-pager-layer")).Absolute().Left(0).Top(0).Right(0).Bottom(0).
				Center().PassThrough().Children(func() {
				internalui.DotPager(c, internalui.DotPagerOptions{
					PagerID:  "quick-pager",
					IDPrefix: "quick-slot",
					Count:    quickSlotCount,
					Active:   activeSlot,
					SlotTooltip: func(slot int) *internalui.TooltipSpec {
						return &internalui.TooltipSpec{
							Rows: []internalui.TooltipRow{{
								Trigger:     fmt.Sprintf(theme.Tr("速记页 %d", "Note %d"), slot+1),
								Detail:      theme.Tr("点击或上下滚动切换", "Click or scroll"),
								SplitDetail: true,
							}},
						}
					},
					OnSelect: func(slot int) {
						q.switchSlot(slot)
					},
				})
			})

			// Right single primary action: Scheme A two-tone NoteCheck ("存入正式笔记").
			saveIcon := icons.NoteCheck
			saveTip := theme.Tr("存入正式笔记", "Save to Note")
			saveSc := internalui.FormatShortcutDisplay("cmd-shift-s")
			if savedFlash {
				saveIcon = icons.Check
				saveTip = theme.Tr("已存入正式笔记", "Saved to Note")
				saveSc = ""
			}
			saveBtn := internalui.IconButton(c, saveIcon, internalui.IconButtonOptions{
				ID:        "quick-save",
				Size:      theme.HeightCompact,
				IconSize:  theme.IconChrome,
				Label:     theme.Tr("存入正式笔记", "Save to Note"),
				Tooltip:   saveTip,
				Shortcut:  saveSc,
				Active:    savedFlash,
				AlwaysDuo: !savedFlash,
			}).KeepFocus()
			saveBtn.DrawOver(func(_ *ui.Painter, r ui.Rect) {
				internalui.RecordBounds("quick-save", r)
				internalui.RecordBounds("save-to-note", r)
			})
			if saveBtn.Clicked() {
				_, _ = q.SaveToNotebook()
			}
		})

		// Minimal full-featured Block Markdown editor per slot.
		editorBox := ui.Box(c.Key(fmt.Sprintf("quick-editor-slot-%d", activeSlot))).
			Grow(1).FillWidth().Clip().Transition(internalui.SurfaceStageTransition())
		internalui.Probe(editorBox, "quick-note-editor")
		editorBox.Children(func() {
			if sess != nil {
				sess.Render(c, winW, false)
			}
		})
	})

	q.scheduleSyncActiveSlot()

	if notice != "" {
		noticeRow := ui.Row(c.Key("quick-notice")).Absolute().Right(theme.Space16).Bottom(theme.Space16).MaxWidth(360).Height(30).PaddingX(theme.Space12).
			AlignItems(ui.Center).Radius(theme.RadiusPill).Background(pal.Menu).
			Border(1, pal.MenuBorder).Transition(internalui.PopoverSpringTransition()).
			Children(func() {
				ui.Text(c, notice).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine()
			})
		internalui.Elevate(noticeRow, theme.ElevationPopover)
	}
}

const quickSyncDebounce = 300 * time.Millisecond

func (q *QuickNoteController) scheduleSyncActiveSlot() {
	q.mu.Lock()
	slot := q.activeSlot
	sess := q.sessions[slot]
	if sess == nil || !sess.Dirty() {
		q.mu.Unlock()
		return
	}
	if testing.Testing() || q.win == nil || internalui.IsProbeEnabled() {
		q.syncSessionToDiskLocked(slot)
		q.mu.Unlock()
		return
	}
	if q.syncTimer != nil {
		q.syncTimer.Reset(quickSyncDebounce)
		q.mu.Unlock()
		return
	}
	q.syncTimer = time.AfterFunc(quickSyncDebounce, func() {
		mygo.RunOnMain(func() {
			q.syncActiveSlotAsync()
		})
	})
	q.mu.Unlock()
}

func (q *QuickNoteController) syncActiveSlotAsync() {
	q.mu.Lock()
	if q.syncTimer != nil {
		q.syncTimer.Stop()
		q.syncTimer = nil
	}
	slot := q.activeSlot
	sess := q.sessions[slot]
	if sess == nil || !sess.Dirty() {
		q.mu.Unlock()
		return
	}
	content := q.serializeSessionLocked(slot)
	q.slots[slot] = content
	revision := sess.DocRev()
	path := q.slotFilePath(slot)
	seq := q.writeSeq[slot].Add(1)
	q.mu.Unlock()

	if path == "" {
		return
	}
	go func() {
		err := q.writeSlotFileAsync(slot, path, content, seq)
		mygo.RunOnMain(func() {
			q.mu.Lock()
			defer q.mu.Unlock()
			if q.writeSeq[slot].Load() != seq || q.sessions[slot] != sess {
				return
			}
			if err != nil {
				q.notice, q.savedFlash = err.Error(), false
			} else {
				sess.MarkSavedIfUnchanged(revision, content)
			}
		})
	}()
}

func (q *QuickNoteController) writeSlotFileAsync(slot int, path, content string, seq uint64) error {
	q.diskMu.Lock()
	defer q.diskMu.Unlock()
	if q.writeSeq[slot].Load() != seq {
		return nil
	}
	_, err := files.WriteNoteAtomic(path, content)
	return err
}

func (q *QuickNoteController) syncActiveSlotIfDirty() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.syncTimer != nil {
		q.syncTimer.Stop()
		q.syncTimer = nil
	}
	slot := q.activeSlot
	if sess := q.sessions[slot]; sess != nil && sess.Dirty() {
		q.syncSessionToDiskLocked(slot)
	}
}
