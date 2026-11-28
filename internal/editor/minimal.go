package editor

import (
	"path/filepath"
	"strings"

	"block/internal/model"
	"github.com/egoist/mygo/ui"
)

const (
	minimalTopPad     float32 = 14
	minimalBottomPad  float32 = 20
	minimalLeftInset  float32 = 20
	minimalRightInset float32 = 20
)

// SetMinimal enables or disables Quick Note compact editor mode:
// compact horizontal/vertical insets and heading scale without the 56px left side-menu gutter
// or side rails, while retaining empty-line hints, selection formatting toolbar, Slash menu,
// paste/drop media, inline AI, and block shortcuts.
func (s *Session) SetMinimal(minimal bool) {
	s.minimal = minimal
}

// IsMinimal reports whether minimal (Quick Note) mode is active.
func (s *Session) IsMinimal() bool {
	return s.minimal
}

// SetMinCanvasHeight sets the minimum height of the scrollable canvas so short notes fill
// the Quick Note viewport for click-to-focus without inflating bodyScroll.MaxY > 0.
func (s *Session) SetMinCanvasHeight(h float32) {
	s.minCanvasHeight = max(h, 0)
}

// CanScrollVertically reports whether the document body overflows the scroll viewport.
func (s *Session) CanScrollVertically() bool {
	return s.bodyScroll.MaxY > 1
}

// VerticalScrollBounds returns the current vertical scroll offset and maximum scroll offset.
func (s *Session) VerticalScrollBounds() (y, maxY float32) {
	return s.bodyScroll.Y, s.bodyScroll.MaxY
}

// HasScrollablePopupOpen reports whether an editor popup menu with internal scrolling is open.
func (s *Session) HasScrollablePopupOpen() bool {
	return s.blockMenu != nil ||
		s.codeLangMenuBlock != 0 ||
		s.calloutMenuBlock != 0 ||
		s.tableMenuKind != ""
}

// DismissTransientOverlay closes the topmost transient popup/bar/selection inside the editor
// and reports whether anything was dismissed. Used by Quick Note so Escape closes popups first
// before hiding the panel.
func (s *Session) DismissTransientOverlay() bool {
	if s.dismissMindMapTransient() {
		return true
	}
	if s.inlineAI != nil {
		s.aiEscape()
		return true
	}
	if s.findOpen {
		s.CloseFind()
		return true
	}
	if s.blockMenu != nil ||
		s.toolbarPop != "" ||
		s.codeLangMenuBlock != 0 ||
		s.calloutMenuBlock != 0 ||
		s.imagePopKind != "" ||
		s.tableMenuKind != "" ||
		s.activeAnnotID != "" ||
		s.editingAnnotID != "" {
		s.closeTransientOverlays()
		return true
	}
	if s.hasNonEmptySelection() {
		s.selAnchor = s.selHead
		s.focusedBlock = s.selHead.Block
		s.syncCollapsedTableCellFocus(s.selHead)
		s.multiSelFocusPending = s.needsDetachedSelectionFocus()
		s.focusPending = !s.multiSelFocusPending
		s.selSyncPending = !s.multiSelFocusPending
		return true
	}
	return false
}

// EnsureEditorFocus schedules native focus onto the active block's input on the next frame.
func (s *Session) EnsureEditorFocus() {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	s.focusedBlock = clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
	if s.needsDetachedSelectionFocus() {
		s.multiSelFocusPending = true
		s.focusPending = false
		return
	}
	s.focusPending = true
}

func (s *Session) leftContentGutter() float32 {
	if s.embedded {
		return 0
	}
	if s.minimal {
		return minimalLeftInset
	}
	return s.handleWidth() + s.rowGap()
}

func (s *Session) rightContentInset() float32 {
	if s.embedded {
		return 0
	}
	if s.minimal {
		return minimalRightInset
	}
	return editorRightInset * s.zoom()
}

// ClearFormattingOnSelection strips inline marks, colors, and links across the selected range.
func (s *Session) ClearFormattingOnSelection() {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return
	}
	changed := false
	s.visitSelectionSpanRanges(func(blk *model.Block, targetSpans *[]model.Span, lo, hi, totalLen int) bool {
		if !changed {
			s.pushUndo()
			changed = true
		}
		left := sliceSpans(*targetSpans, 0, lo)
		mid := sliceSpans(*targetSpans, lo, hi)
		right := sliceSpans(*targetSpans, hi, totalLen)
		for i := range mid {
			mid[i].Marks = 0
			mid[i].FGColor = ""
			mid[i].BGColor = ""
			mid[i].Link = ""
		}
		var merged []model.Span
		merged = append(merged, left...)
		merged = append(merged, mid...)
		merged = append(merged, right...)
		*targetSpans = normalizeSpans(merged)
		blk.Dirty = true
		blk.CleanSource = ""
		return true
	})
	if changed {
		s.markDirty()
	}
}

func (s *Session) toggleHighlightOnSelection() {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return
	}
	saw := false
	allYellow := true
	s.visitSelectionSpanRanges(func(_ *model.Block, spans *[]model.Span, lo, hi, _ int) bool {
		saw = true
		if !spansAllMatch(*spans, lo, hi, func(sp model.Span) bool { return sp.BGColor == "yellow" }) {
			allYellow = false
			return false
		}
		return true
	})
	if !saw {
		return
	}
	target := "yellow"
	if allYellow {
		target = "default"
	}
	s.ApplyColorOnSelection(nil, &target)
}

func (s *Session) handleInlineFormatShortcut(mods ui.Modifiers, key ui.Key) bool {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 || s.hasMenuPopupOpen() || s.isAnyInputComposing() {
		return false
	}
	switch {
	case mods == ui.Cmd && key == ui.KeyB:
		s.ToggleMarkOnSelection(model.MarkBold)
		return true
	case mods == ui.Cmd && key == ui.KeyI:
		s.ToggleMarkOnSelection(model.MarkItalic)
		return true
	case mods == ui.Cmd && key == ui.KeyU:
		s.ToggleMarkOnSelection(model.MarkUnderline)
		return true
	case mods == ui.Cmd && key == ui.KeyE:
		s.ToggleMarkOnSelection(model.MarkCode)
		return true
	case mods == ui.Cmd|ui.Shift && (key == ui.KeyX || key == ui.KeyS):
		s.ToggleMarkOnSelection(model.MarkStrike)
		return true
	case mods == ui.Cmd|ui.Shift && key == ui.KeyH:
		s.toggleHighlightOnSelection()
		return true
	case mods == ui.Cmd|ui.Shift && key == ui.KeyBackslash:
		s.ClearFormattingOnSelection()
		return true
	}
	return false
}

func (s *Session) cmdEnterShortcut() bool {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	if s.hasMultiBlockSelection() {
		a, b := s.orderedSelection()
		lo := clampInt(a.Block, 0, len(s.doc.Blocks)-1)
		hi := clampInt(b.Block, lo, len(s.doc.Blocks)-1)
		anyTodo := false
		for i := lo; i <= hi; i++ {
			if s.doc.Blocks[i].Kind == model.BlockTodo {
				anyTodo = true
				break
			}
		}
		if anyTodo {
			s.pushUndo()
			for i := lo; i <= hi; i++ {
				if s.doc.Blocks[i].Kind == model.BlockTodo {
					s.doc.Blocks[i].Checked = !s.doc.Blocks[i].Checked
					s.doc.Blocks[i].Dirty = true
					s.doc.Blocks[i].CleanSource = ""
				}
			}
			s.markDirty()
			return true
		}
	}

	idx := clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
	blk := &s.doc.Blocks[idx]
	if blk.Kind == model.BlockTodo {
		s.ToggleTodo(idx)
		return true
	}
	if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML || blk.Kind == model.BlockDivider || blk.Kind == model.BlockImage {
		s.pushUndo()
		nb := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
		s.spliceBlocks(idx+1, idx+1, []model.Block{nb})
		s.FocusBlock(idx + 1)
		s.markDirty()
		return true
	}
	return false
}

func (s *Session) handleBlockAndFormatShortcut(mods ui.Modifiers, key ui.Key) bool {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	isCmdAlt := mods == (ui.Cmd | ui.Alt)
	isCtrlShift := mods == (ui.Ctrl | ui.Shift)
	isCmdShift := mods == (ui.Cmd | ui.Shift)

	switch {
	case (isCmdAlt || isCtrlShift) && key == ui.Key0:
		s.SetSelectionBlockKind(model.BlockParagraph, false)
		s.focusPending = true
		return true
	case (isCmdAlt || isCtrlShift) && key == ui.Key1:
		s.SetSelectionBlockKind(model.BlockHeading1, true)
		s.focusPending = true
		return true
	case (isCmdAlt || isCtrlShift) && key == ui.Key2:
		s.SetSelectionBlockKind(model.BlockHeading2, true)
		s.focusPending = true
		return true
	case (isCmdAlt || isCtrlShift) && key == ui.Key3:
		s.SetSelectionBlockKind(model.BlockHeading3, true)
		s.focusPending = true
		return true
	case (isCmdAlt || isCtrlShift) && key == ui.Key4:
		s.SetSelectionBlockKind(model.BlockTodo, true)
		s.focusPending = true
		return true
	case ((isCmdAlt || isCtrlShift) && key == ui.Key5) || (isCmdShift && key == ui.Key8):
		s.SetSelectionBlockKind(model.BlockBullet, true)
		s.focusPending = true
		return true
	case ((isCmdAlt || isCtrlShift) && key == ui.Key6) || (isCmdShift && key == ui.Key7):
		s.SetSelectionBlockKind(model.BlockNumbered, true)
		s.focusPending = true
		return true
	case (isCmdAlt || isCtrlShift) && key == ui.Key8:
		s.SetSelectionBlockKind(model.BlockCode, true)
		s.focusPending = true
		return true
	case ((isCmdAlt || isCtrlShift) && key == ui.Key9) || (isCmdShift && key == ui.Key9):
		s.SetSelectionBlockKind(model.BlockQuote, true)
		s.focusPending = true
		return true
	case mods == ui.Cmd && key == ui.KeyD:
		s.duplicateBlock(clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1))
		return true
	case mods == ui.Cmd && key == ui.KeyEnter:
		return s.cmdEnterShortcut()
	case (mods == ui.Alt || isCmdAlt) && key == ui.KeyUp:
		idx := clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
		if idx > 0 {
			s.moveBlock(idx, idx-1)
		}
		return true
	case (mods == ui.Alt || isCmdAlt) && key == ui.KeyDown:
		idx := clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
		if idx+1 < len(s.doc.Blocks) {
			s.moveBlock(idx, idx+1)
		}
		return true
	case isCmdShift && (key == ui.KeyH || key == ui.KeyBackslash):
		return s.handleInlineFormatShortcut(mods, key)
	}
	return false
}

func (s *Session) handleGlobalBlockShortcuts(c *ui.Context) {
	if s.readOnly || s.inlineAI != nil || s.findOpen || s.toolbarPop != "" || s.editingAnnotID != "" || s.hasMenuPopupOpen() {
		return
	}
	for _, combo := range []struct {
		mods ui.Modifiers
		key  ui.Key
	}{
		{ui.Cmd | ui.Alt, ui.Key0},
		{ui.Cmd | ui.Alt, ui.Key1},
		{ui.Cmd | ui.Alt, ui.Key2},
		{ui.Cmd | ui.Alt, ui.Key3},
		{ui.Cmd | ui.Alt, ui.Key4},
		{ui.Cmd | ui.Alt, ui.Key5},
		{ui.Cmd | ui.Alt, ui.Key6},
		{ui.Cmd | ui.Alt, ui.Key8},
		{ui.Cmd | ui.Alt, ui.Key9},
		{ui.Ctrl | ui.Shift, ui.Key0},
		{ui.Ctrl | ui.Shift, ui.Key1},
		{ui.Ctrl | ui.Shift, ui.Key2},
		{ui.Ctrl | ui.Shift, ui.Key3},
		{ui.Ctrl | ui.Shift, ui.Key4},
		{ui.Ctrl | ui.Shift, ui.Key5},
		{ui.Ctrl | ui.Shift, ui.Key6},
		{ui.Ctrl | ui.Shift, ui.Key8},
		{ui.Ctrl | ui.Shift, ui.Key9},
		{ui.Cmd | ui.Shift, ui.Key7},
		{ui.Cmd | ui.Shift, ui.Key8},
		{ui.Cmd | ui.Shift, ui.Key9},
		{ui.Cmd, ui.KeyD},
		{ui.Cmd, ui.KeyEnter},
		{ui.Alt, ui.KeyUp},
		{ui.Alt, ui.KeyDown},
		{ui.Cmd | ui.Alt, ui.KeyUp},
		{ui.Cmd | ui.Alt, ui.KeyDown},
		{ui.Cmd | ui.Shift, ui.KeyH},
		{ui.Cmd | ui.Shift, ui.KeyBackslash},
	} {
		if (combo.key == ui.KeyUp || combo.key == ui.KeyDown || combo.key == ui.KeyD) && s.hasBlockingOverlay() {
			continue
		}
		if c.Shortcut(combo.mods, combo.key) {
			s.handleBlockAndFormatShortcut(combo.mods, combo.key)
			return
		}
	}
}

// IsSupportedImagePath reports whether path has an image file extension Block can embed.
func IsSupportedImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".ico", ".avif":
		return true
	default:
		return false
	}
}
