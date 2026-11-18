package editor

import (
	"time"
	"unicode/utf8"

	"block/internal/files"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"
	appUI "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const jumpHighlightHold = 1100 * time.Millisecond

type editorJump struct {
	block    model.BlockID
	row, col int
	revision uint64
	pending  bool
	shownAt  time.Time
}

func (s *Session) JumpToSearchHit(hit model.SearchHit) {
	if hit.Line <= 0 {
		return
	}
	source := markdown.MapSource(s.doc)
	line, column := files.ResolveSearchMatch(source.Text, hit)
	if target, ok := source.Locate(line, column, hit.Query); ok {
		s.jumpTo(target.Block, target.Row, target.Column)
	}
}

// JumpToBlock is used by the outline. Jumping preserves the editing caret.
func (s *Session) JumpToBlock(idx int) {
	if s.jumpTo(idx, -1, -1) {
		s.outlineHovered = false
		s.outlinePinnedClosed = true
	}
}

func (s *Session) jumpTo(idx, row, col int) bool {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	s.ensureBlockUnfolded(idx)
	block := &s.doc.Blocks[idx]
	table := block.Table
	if table == nil || row < 0 || row >= len(table.Rows) || col < 0 || col >= len(table.Rows[row]) {
		row, col = -1, -1
	}
	s.jump = editorJump{block: block.ID, row: row, col: col, revision: s.docRev, pending: true}
	return true
}

func (s *Session) jumpToFindMatch(m findMatch) {
	if table := s.doc.Blocks[m.BlockIdx].Table; table != nil {
		offset := 0
		for r, cells := range table.Rows {
			for c, cell := range cells {
				end := offset + utf8.RuneCountInString(cell.PlainText())
				if m.StartRune >= offset && m.EndRune <= end {
					s.jumpTo(m.BlockIdx, r, c)
					return
				}
				offset = end + 1 // Tab between cells, newline between rows.
			}
		}
	}
	s.jumpTo(m.BlockIdx, -1, -1)
}

// revealJump uses the freshly laid-out row or cell. The scroll containers
// clamp to their actual extents and reveal horizontally clipped cells too.
func (s *Session) revealJump(c *ui.Context, contentTop float32) {
	if !s.jump.pending {
		return
	}
	s.jump.pending = false
	ui.Box(c.Key("jump-anchor")).Absolute().Top(contentTop - s.scale(theme.Space24)).Left(0).Right(0).
		Height(theme.Space2).IgnoreInput(true).AccessHidden(true).
		ScrollIntoView(ui.ScrollIntoViewOptions{
			Block:     ui.ScrollStart,
			Inline:    ui.ScrollNearest,
			Animation: appUI.JumpScrollAnimation(s.scrollRect.H > 0),
		})
}

func (s *Session) jumpHighlight(c *ui.Context) float32 {
	if s.jump.block == 0 {
		return 0
	}
	if s.jump.revision != s.docRev {
		s.jump = editorJump{}
		return 0
	}
	if s.jump.shownAt.IsZero() {
		s.jump.shownAt = c.Now()
	}
	fade := appUI.FadeColor.Duration
	elapsed := c.Now().Sub(s.jump.shownAt)
	if elapsed < jumpHighlightHold {
		c.After(jumpHighlightHold - elapsed)
		return 1
	}
	if elapsed >= jumpHighlightHold+fade {
		s.jump = editorJump{}
		return 0
	}
	c.AnimationFrame()
	return 1 - float32(elapsed-jumpHighlightHold)/float32(fade)
}
