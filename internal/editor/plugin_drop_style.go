package editor

import (
	"strings"

	"block/internal/model"
)

// BlockDecorator configures how the editor styles and displays blocks marked by
// a plugin comment signature or legacy bracket tag prefix.
type BlockDecorator struct {
	BlockKind        model.BlockKind
	SignaturePrefix  string
	LegacyTagPrefix  string
	DefaultCallout   model.CalloutKind
	CalloutByKind    map[string]model.CalloutKind
	EmptyPlaceholder string
}

// SetBlockDecorators updates the active plugin block decorators for this frame.
func (s *Session) SetBlockDecorators(decs []BlockDecorator) {
	if s == nil {
		return
	}
	s.blockDecorators = decs
}

func isDecoratorKindChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
}

func isCommentEscaped(s string, idx int) bool {
	bs := 0
	for i := idx - 1; i >= 0 && s[i] == '\\'; i-- {
		bs++
	}
	return bs%2 == 1
}

func matchSignatureComment(src, sigPrefix string) (kind string, ok bool) {
	if src == "" || sigPrefix == "" {
		return "", false
	}
	pos := 0
	for pos < len(src) {
		rel := strings.Index(src[pos:], "<!--")
		if rel < 0 {
			return "", false
		}
		start := pos + rel
		if isCommentEscaped(src, start) {
			pos = start + 4
			continue
		}
		endRel := strings.Index(src[start+4:], "-->")
		if endRel < 0 {
			return "", false
		}
		inner := strings.TrimSpace(src[start+4 : start+4+endRel])
		pos = start + 4 + endRel + 3
		fields := strings.Fields(inner)
		if len(fields) == 0 {
			continue
		}
		tok := fields[0]
		if tok == sigPrefix {
			return "", true
		}
		if strings.HasPrefix(tok, sigPrefix+":") {
			rest := tok[len(sigPrefix)+1:]
			if rest != "" && isTokenWord(rest) {
				return rest, true
			}
		}
	}
	return "", false
}

func isTokenWord(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDecoratorKindChar(s[i]) {
			return false
		}
	}
	return len(s) > 0
}

func matchLegacyTagPrefix(plain, tagPrefix string) (kind string, rest string, ok bool) {
	if tagPrefix == "" {
		return "", plain, false
	}
	lines := strings.Split(plain, "\n")
	matched := false
	outLines := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, tagPrefix) {
			after := trimmed[len(tagPrefix):]
			if end := strings.IndexByte(after, ']'); end > 0 && isTokenWord(after[:end]) {
				if !matched {
					kind = after[:end]
					matched = true
				}
				rem := strings.TrimLeft(after[end+1:], " \t")
				if rem != "" {
					outLines = append(outLines, rem)
				}
				continue
			}
		}
		outLines = append(outLines, line)
	}
	if !matched {
		return "", plain, false
	}
	return kind, strings.Join(outLines, "\n"), true
}

func (s *Session) matchBlockDecorator(b *model.Block) (BlockDecorator, string, bool) {
	if s == nil || b == nil || len(s.blockDecorators) == 0 {
		return BlockDecorator{}, "", false
	}
	for _, dec := range s.blockDecorators {
		targetKind := dec.BlockKind
		if targetKind == 0 && dec.SignaturePrefix == "" && dec.LegacyTagPrefix == "" {
			continue
		}
		if b.Kind != targetKind {
			continue
		}
		if dec.SignaturePrefix != "" && b.DecoratorPrefix == dec.SignaturePrefix {
			return dec, b.DecoratorKind, true
		}
		if dec.SignaturePrefix != "" && b.CleanSource != "" {
			if kind, ok := matchSignatureComment(b.CleanSource, dec.SignaturePrefix); ok {
				return dec, kind, true
			}
		}
		if dec.LegacyTagPrefix != "" {
			if kind, _, ok := matchLegacyTagPrefix(b.PlainText(), dec.LegacyTagPrefix); ok {
				return dec, kind, true
			}
		}
	}
	return BlockDecorator{}, "", false
}

func decoratorCalloutKind(dec BlockDecorator, kind string) model.CalloutKind {
	if kind != "" && len(dec.CalloutByKind) > 0 {
		if c, ok := dec.CalloutByKind[strings.ToLower(strings.TrimSpace(kind))]; ok && c != "" {
			return c
		}
	}
	if dec.DefaultCallout != "" {
		return dec.DefaultCallout
	}
	return model.CalloutWarning
}

func decoratorDisplayText(plain string, dec BlockDecorator) string {
	if dec.LegacyTagPrefix != "" {
		if _, stripped, ok := matchLegacyTagPrefix(plain, dec.LegacyTagPrefix); ok {
			plain = stripped
		}
	}
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return dec.EmptyPlaceholder
	}
	return plain
}
