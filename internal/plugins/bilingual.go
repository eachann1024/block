package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsOfficialRepo reports whether repo belongs to the official blockmd-plugins organization.
func IsOfficialRepo(repo string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(repo)), "blockmd-plugins/")
}

// ValidateBilingualDocs enforces Scheme B (Chinese-first) bilingual documentation:
// README.md must be non-empty Chinese UTF-8 and README.en.md must be non-empty English-only UTF-8.
func ValidateBilingualDocs(pluginDir string) error {
	zhText, err := readStrictDoc(pluginDir, "README.md")
	if err != nil {
		return fmt.Errorf("README.md (Chinese): %w", err)
	}
	if !ContainsHan(zhText) {
		return fmt.Errorf("README.md must contain Chinese copy (Scheme B: README.md is Chinese, README.en.md is English)")
	}

	enText, err := readStrictDoc(pluginDir, "README.en.md")
	if err != nil {
		return fmt.Errorf("README.en.md (English): %w", err)
	}
	if bad, ok := FindChineseRune(enText); ok {
		return fmt.Errorf("README.en.md must be English-only, found Chinese character %q", bad)
	}
	if strings.TrimSpace(zhText) == strings.TrimSpace(enText) {
		return fmt.Errorf("README.en.md must not be identical to README.md")
	}
	return nil
}

func readStrictDoc(dir, exactName string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	matched := false
	for _, entry := range entries {
		if entry.Name() == exactName {
			if !entry.Type().IsRegular() {
				return "", fmt.Errorf("%s must be a regular file", exactName)
			}
			matched = true
			break
		}
	}
	if !matched {
		return "", fmt.Errorf("missing required file %s", exactName)
	}
	raw, err := os.ReadFile(filepath.Join(dir, exactName))
	if err != nil {
		return "", err
	}
	if len(raw) > maxReadmeBytes || !utf8.Valid(raw) {
		return "", fmt.Errorf("%s exceeds %d bytes or is not valid UTF-8", exactName, maxReadmeBytes)
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(raw), "\uFEFF"))
	if text == "" {
		return "", fmt.Errorf("%s must not be empty", exactName)
	}
	return text, nil
}

// ContainsHan reports whether s contains at least one CJK Han ideograph.
func ContainsHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// FindChineseRune finds the first Han ideograph or CJK/full-width punctuation rune in s.
func FindChineseRune(s string) (rune, bool) {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF01 && r <= 0xFF5E) {
			return r, true
		}
	}
	return 0, false
}
