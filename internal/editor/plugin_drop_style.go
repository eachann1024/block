package editor

import (
	"regexp"
	"strings"

	"block/internal/model"
	"block/internal/theme"
)

var (
	poopLegacyLineRe = regexp.MustCompile(`(?m)^\[poop:([a-z0-9_-]+)\]`)
	poopSigRe        = regexp.MustCompile(`block-poop-everywhere:drop(?::([a-z0-9_-]+))?`)
)

// poopDropKind reports whether a quote is a plugin drop, and which shape it uses.
// Legacy notes keep the shape in visible text ("[poop:classic]"); current notes
// keep it only in the hidden HTML signature.
func poopDropKind(b *model.Block) (string, bool) {
	if b == nil || b.Kind != model.BlockQuote {
		return "", false
	}
	if b.PoopKind != "" {
		return b.PoopKind, true
	}
	if m := poopSigRe.FindStringSubmatch(b.CleanSource); m != nil {
		if m[1] != "" {
			return m[1], true
		}
		return "classic", true
	}
	if m := poopLegacyLineRe.FindStringSubmatch(strings.TrimSpace(b.PlainText())); m != nil {
		return m[1], true
	}
	return "", false
}

func poopDropCallout(kind string) model.CalloutKind {
	switch kind {
	case "ribbon":
		return model.CalloutImportant
	case "pellet":
		return model.CalloutTip
	case "wrap", "wrap-start", "wrap-end":
		return model.CalloutNote
	default:
		return model.CalloutWarning
	}
}

func poopDropDisplayText(plain, _ string) string {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return theme.Tr("到此一拉", "Dropped here")
	}
	return plain
}
