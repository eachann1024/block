package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

func stripHTMLComments(s string) string {
	s = htmlCommentRe.ReplaceAllString(s, "")
	if idx := strings.Index(s, "<!--"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// IsOfficialRepo reports whether repo belongs to the official blockmd-plugins
// (Block Community) organization.
func IsOfficialRepo(repo string) bool {
	slug := strings.ToLower(strings.TrimSpace(repo))
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || name == "" || strings.Contains(name, "/") {
		return false
	}
	return owner == "blockmd-plugins"
}

// IsReviewedAuthorRepo reports whether repo belongs to a trusted first-party
// author whose plugins are marked as reviewed in the App Store.
func IsReviewedAuthorRepo(repo string) bool {
	slug := strings.ToLower(strings.TrimSpace(repo))
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || name == "" || strings.Contains(name, "/") {
		return false
	}
	return owner == "eachann1024"
}

// ValidateManifestEnglish verifies that all English-localized strings in manifest
// (including contributes) contain no Chinese characters or CJK/full-width punctuation.
func ValidateManifestEnglish(m *Manifest) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	check := func(field, val string) error {
		if bad, ok := FindChineseRune(val); ok {
			return fmt.Errorf("%s contains untranslated Chinese character %q in %q", field, bad, val)
		}
		return nil
	}
	if err := check("manifest.name.en", m.Name.En); err != nil {
		return err
	}
	if err := check("manifest.summary.en", m.Summary.En); err != nil {
		return err
	}
	for _, perm := range m.Permissions {
		reason, ok := m.PermissionReasons[perm]
		if !ok {
			continue
		}
		field := fmt.Sprintf("manifest.permission_reasons[%q].en", perm)
		if strings.TrimSpace(reason.En) == "" {
			return fmt.Errorf("%s is required for bilingual plugins", field)
		}
		if err := check(field, reason.En); err != nil {
			return err
		}
	}
	if m.Contributes == nil {
		return nil
	}
	for i, b := range m.Contributes.TitlebarButtons {
		if err := check(fmt.Sprintf("manifest.contributes.titlebar_buttons[%d].tooltip.en", i), b.Tooltip.En); err != nil {
			return err
		}
	}
	for i, s := range m.Contributes.SettingsSections {
		if err := check(fmt.Sprintf("manifest.contributes.settings_sections[%d].title.en", i), s.Title.En); err != nil {
			return err
		}
	}
	for i, c := range m.Contributes.Commands {
		if err := check(fmt.Sprintf("manifest.contributes.commands[%d].title.en", i), c.Title.En); err != nil {
			return err
		}
	}
	for i, item := range m.Contributes.PageMenu {
		if err := check(fmt.Sprintf("manifest.contributes.page_menu[%d].title.en", i), item.Title.En); err != nil {
			return err
		}
	}
	for i, item := range m.Contributes.EditorMenu {
		if err := check(fmt.Sprintf("manifest.contributes.editor_menu[%d].title.en", i), item.Title.En); err != nil {
			return err
		}
	}
	for i, p := range m.Contributes.Panels {
		if err := check(fmt.Sprintf("manifest.contributes.panels[%d].title.en", i), p.Title.En); err != nil {
			return err
		}
	}
	for i, st := range m.Contributes.StatusItems {
		if err := check(fmt.Sprintf("manifest.contributes.status_items[%d].text.en", i), st.Text.En); err != nil {
			return err
		}
	}
	for i, d := range m.Contributes.Dialogs {
		if err := check(fmt.Sprintf("manifest.contributes.dialogs[%d].title.en", i), d.Title.En); err != nil {
			return err
		}
	}
	for i, s := range m.Contributes.SidebarSections {
		if err := check(fmt.Sprintf("manifest.contributes.sidebar_sections[%d].title.en", i), s.Title.En); err != nil {
			return err
		}
	}
	for i, fb := range m.Contributes.FileBadges {
		if err := check(fmt.Sprintf("manifest.contributes.file_badges[%d].tooltip.en", i), fb.Tooltip.En); err != nil {
			return err
		}
	}
	for i, item := range m.Contributes.SidebarMenu {
		if err := check(fmt.Sprintf("manifest.contributes.sidebar_menu[%d].title.en", i), item.Title.En); err != nil {
			return err
		}
	}
	for i, item := range m.Contributes.TabMenu {
		if err := check(fmt.Sprintf("manifest.contributes.tab_menu[%d].title.en", i), item.Title.En); err != nil {
			return err
		}
	}
	for i, bg := range m.Contributes.Backdrops {
		if err := check(fmt.Sprintf("manifest.contributes.backdrops[%d].title.en", i), bg.Title.En); err != nil {
			return err
		}
	}
	for i, l := range m.Contributes.Languages {
		if err := check(fmt.Sprintf("manifest.contributes.languages[%d].name.en", i), l.Name.En); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBilingualDocs enforces Scheme B (Chinese-first) bilingual documentation:
// README.md must be non-empty Chinese UTF-8 and README.en.md must be non-empty English-only UTF-8.
func ValidateBilingualDocs(pluginDir string) error {
	zhText, err := readStrictDoc(pluginDir, "README.md")
	if err != nil {
		return fmt.Errorf("README.md (Chinese): %w", err)
	}
	zhVisible := stripHTMLComments(zhText)
	if !ContainsHan(zhVisible) {
		return fmt.Errorf("README.md must contain visible Chinese copy (Scheme B: README.md is Chinese, README.en.md is English)")
	}

	enText, err := readStrictDoc(pluginDir, "README.en.md")
	if err != nil {
		return fmt.Errorf("README.en.md (English): %w", err)
	}
	if bad, ok := FindChineseRune(enText); ok {
		return fmt.Errorf("README.en.md must be English-only, found Chinese character %q", bad)
	}
	enVisible := stripHTMLComments(enText)
	if !containsASCIILetter(enVisible) {
		return fmt.Errorf("README.en.md must contain visible English text")
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

func containsASCIILetter(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
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
		if unicode.Is(unicode.Han, r) ||
			(r >= 0x3000 && r <= 0x303F) ||
			(r >= 0xFE10 && r <= 0xFE1F) ||
			(r >= 0xFE30 && r <= 0xFE4F) ||
			(r >= 0xFF01 && r <= 0xFF5E) {
			return r, true
		}
	}
	return 0, false
}
