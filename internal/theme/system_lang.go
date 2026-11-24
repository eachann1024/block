package theme

import (
	"os"
	"strings"

	"block/internal/model"
)

// SystemPreferredLanguages returns the user's OS preferred languages in order
// as normalized BCP-47 tags (e.g. "zh-Hans-CN", "en-US", "ja-JP").
func SystemPreferredLanguages() []string {
	raw := platformPreferredLanguages()
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, entry := range raw {
		tag := normalizeLangTag(entry)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	return out
}

// SystemLanguage resolves Block's UI language from the user's OS language
// preferences, defaulting to English for non-Chinese locales.
func SystemLanguage() model.LanguageMode {
	for _, tag := range SystemPreferredLanguages() {
		primary := primaryLanguageSubtag(tag)
		if primary == "zh" {
			return model.LangZh
		}
		if primary == "en" {
			return model.LangEn
		}
	}
	return model.LangEn
}

func primaryLanguageSubtag(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexByte(tag, '-'); i >= 0 {
		return tag[:i]
	}
	return tag
}

func normalizeLangTag(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Strip POSIX encoding (.UTF-8) and modifier (@euro).
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(strings.ReplaceAll(s, "_", "-"))
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if lower == "c" || lower == "posix" {
		return "en"
	}
	return s
}

// posixPreferredLanguages parses standard POSIX / GNU gettext locale variables
// (LC_ALL, LC_MESSAGES, LANG, and LANGUAGE) following gettext precedence:
// LC_ALL > LC_MESSAGES > LANG select the single effective locale, while
// LANGUAGE provides a colon-separated override/fallback list unless the
// effective locale is C or POSIX.
func posixPreferredLanguages() []string {
	effective := strings.TrimSpace(os.Getenv("LC_ALL"))
	if effective == "" {
		effective = strings.TrimSpace(os.Getenv("LC_MESSAGES"))
	}
	if effective == "" {
		effective = strings.TrimSpace(os.Getenv("LANG"))
	}
	if isPosixCLocale(effective) {
		return []string{"en"}
	}

	var out []string
	if language := strings.TrimSpace(os.Getenv("LANGUAGE")); language != "" {
		for _, part := range strings.Split(language, ":") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if isPosixCLocale(p) {
				// Per GNU gettext, C/POSIX in LANGUAGE terminates fallback to English.
				return append(out, "en")
			}
			out = append(out, p)
		}
	}
	if effective != "" {
		out = append(out, effective)
	}
	return out
}

func isPosixCLocale(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	return strings.EqualFold(s, "c") || strings.EqualFold(s, "posix")
}
