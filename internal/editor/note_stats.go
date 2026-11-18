package editor

import (
	"strings"
	"unicode"

	"block/internal/model"
)

// NoteStats counts non-whitespace characters and content blocks in the live note.
func (s *Session) NoteStats() (characters, blocks int) {
	if s.doc == nil {
		return 0, 0
	}
	for i := range s.doc.Blocks {
		block := &s.doc.Blocks[i]
		if isFrontmatterBlock(i, block) {
			continue
		}
		if block.Kind == model.BlockDivider {
			blocks++
			continue
		}
		plain := block.PlainText()
		if block.Kind == model.BlockParagraph && strings.TrimSpace(plain) == "" {
			continue
		}
		blocks++
		for _, char := range plain {
			if !unicode.IsSpace(char) {
				characters++
			}
		}
	}
	return characters, blocks
}
