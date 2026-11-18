package editor

import (
	"block/internal/model"
	"github.com/egoist/mygo/ui"
)

// subtreeEnd includes contiguous, more deeply indented list items.
func (s *Session) subtreeEnd(first int) int {
	blocks := s.doc.Blocks
	end := first
	if blocks[first].Kind.IsList() {
		for end+1 < len(blocks) && blocks[end+1].Kind.IsList() && blocks[end+1].Indent > blocks[first].Indent {
			end++
		}
	}
	return end
}

func (s *Session) activeBlockRange() (int, int) {
	if s.hasNonEmptySelection() {
		a, b := s.orderedSelection()
		return a.Block, b.Block
	}
	i := clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
	return i, i
}

func (s *Session) stepBlockRange(lo, hi int, down bool) {
	blocks := s.doc.Blocks
	if lo < 0 || hi < lo || hi >= len(blocks) {
		return
	}
	end := s.subtreeEnd(hi)
	to := lo - 1
	if down {
		if end+1 >= len(blocks) {
			return
		}
		to = lo + s.subtreeEnd(end+1) - end
	} else {
		if lo == 0 {
			return
		}
		for to > 0 && blocks[lo].Kind.IsList() && blocks[to].Kind.IsList() && blocks[to].Indent > blocks[lo].Indent {
			to--
		}
	}
	s.pushUndo()
	moved := append([]model.Block(nil), blocks[lo:end+1]...)
	n := len(moved)
	if down {
		copy(blocks[lo:to], blocks[end+1:to+n])
	} else {
		copy(blocks[to+n:end+1], blocks[to:lo])
	}
	copy(blocks[to:], moved)
	if s.selAnchor.Block >= lo && s.selAnchor.Block <= hi && s.selHead.Block >= lo && s.selHead.Block <= hi {
		s.selAnchor.Block += to - lo
		s.selHead.Block += to - lo
		s.focusedBlock = s.selHead.Block
		s.focusPending = true
		s.selSyncPending = true
	} else {
		s.FocusBlock(to)
	}
	s.markDirty()
}

func (s *Session) duplicateRange(lo, hi int) {
	if s.doc == nil || lo < 0 || hi < lo || hi >= len(s.doc.Blocks) {
		return
	}
	end := s.subtreeEnd(hi)
	copies := make([]model.Block, 0, end-lo+1)
	for _, b := range s.doc.Blocks[lo : end+1] {
		cp := b.Clone()
		cp.ID = model.NewBlockID()
		if cp.Language == "yaml-frontmatter" {
			cp.Language = "yaml"
			cp.CleanSource = ""
			cp.Dirty = true
		}
		copies = append(copies, cp)
	}
	offset := 0
	if lo == hi && s.selHead.Block == lo {
		offset = clampInt(s.selHead.Offset, 0, s.blockSelectionLen(lo))
	}
	s.pushUndo()
	s.spliceBlocks(end+1, end+1, copies)
	s.FocusBlock(end + 1)
	s.selAnchor.Offset = offset
	s.selHead.Offset = offset
	s.markDirty()
}

// convertTextBlock reads from the original representation before changing kind.
func convertTextBlock(b *model.Block, kind model.BlockKind) {
	text := b.PlainText()
	raw := b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML
	b.Kind = kind
	if kind == model.BlockCode {
		b.Code, b.Spans = text, nil
		if b.Language == "" {
			b.Language = "plaintext"
		}
	} else if raw {
		b.Code, b.Language = "", ""
		b.SetText(text)
	}
	b.Callout = ""
	b.Dirty = true
	b.CleanSource = ""
}

func (s *Session) rangeShortcut(mods ui.Modifiers, key ui.Key) bool {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	lo, hi := s.activeBlockRange()
	switch {
	case mods == ui.Cmd && key == ui.KeyD:
		s.duplicateRange(lo, hi)
	case (mods == ui.Alt || mods == ui.Cmd|ui.Alt) && (key == ui.KeyUp || key == ui.KeyDown):
		s.stepBlockRange(lo, hi, key == ui.KeyDown)
	default:
		return false
	}
	return true
}
