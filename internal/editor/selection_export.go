package editor

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// IsMarkdownNote reports whether the session edits a Markdown note.
func (s *Session) IsMarkdownNote() bool {
	return s.isMarkdownNote()
}

// HasFocus reports whether keyboard focus currently resides inside this editor session.
func (s *Session) HasFocus(c *ui.Context) bool {
	if c == nil {
		return false
	}
	return s.rootHandle.FocusWithin(c) || s.detachedFocus.Focused(c)
}

// SelectionKey returns cheap change-detection fields for the current selection
// without serializing Markdown.
func (s *Session) SelectionKey() (notePath string, docRev uint64, anchor, head Position, nonEmpty bool) {
	return s.notePath, s.docRev, s.selAnchor, s.selHead, s.hasNonEmptySelection()
}

// ExportSelectionSnapshot serializes the current non-empty selection to Markdown,
// builds a collapsed plain-text tooltip preview (up to 80 runes), and returns
// 1-based inclusive block numbers.
func (s *Session) ExportSelectionSnapshot() (md, plainPreview string, startLine, endLine int, ok bool) {
	if !s.hasNonEmptySelection() || s.doc == nil || len(s.doc.Blocks) == 0 {
		return "", "", 0, 0, false
	}
	md = s.SelectedMarkdown()
	if strings.TrimSpace(md) == "" {
		return "", "", 0, 0, false
	}

	a, b := s.orderedSelection()
	firstIdx := -1
	lastIdx := -1
	var plainParts []string
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		lo, hi, _, selected := s.blockSelectionSpanRange(idx)
		if !selected {
			continue
		}
		if firstIdx < 0 {
			firstIdx = idx
		}
		lastIdx = idx
		plain := s.doc.Blocks[idx].PlainText()
		if plain == "" {
			continue
		}
		runes := []rune(plain)
		lo = clampInt(lo, 0, len(runes))
		hi = clampInt(hi, lo, len(runes))
		if hi > lo {
			plainParts = append(plainParts, string(runes[lo:hi]))
		}
	}
	if firstIdx < 0 {
		firstIdx = clampInt(a.Block, 0, len(s.doc.Blocks)-1)
		lastIdx = clampInt(b.Block, firstIdx, len(s.doc.Blocks)-1)
	}

	plain := strings.Join(strings.Fields(strings.Join(plainParts, " ")), " ")
	if plain == "" {
		plain = strings.Join(strings.Fields(md), " ")
	}
	return md, truncatePlainTooltip(plain, 80), firstIdx + 1, lastIdx + 1, true
}

func truncatePlainTooltip(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}
