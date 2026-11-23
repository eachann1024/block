package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/mod/semver"
)

const (
	TargetApp            = "blockmd"
	PluginTopic          = "blockmd-plugin"
	URLScheme            = "blockmd"
	CurrentSchemaVersion = 1
)

type Permission string

const (
	PermStorage        Permission = "storage"
	PermNotesRead      Permission = "notes:read"
	PermNotesWrite     Permission = "notes:write"
	PermNetwork        Permission = "network"
	PermEventNoteSaved Permission = "events:note_saved"
)

var validPermissions = map[Permission]bool{
	PermStorage:        true,
	PermNotesRead:      true,
	PermNotesWrite:     true,
	PermNetwork:        true,
	PermEventNoteSaved: true,
}

func PermissionLabel(p Permission, locale string) string {
	en := strings.HasPrefix(strings.ToLower(locale), "en")
	if text, ok := systemPermissions[p]; ok {
		if en {
			return text[1]
		}
		return text[0]
	}
	switch p {
	case PermStorage:
		if en {
			return "Plugin Storage"
		}
		return "独立存储"
	case PermNotesRead:
		if en {
			return "Read Notes"
		}
		return "读取笔记"
	case PermNotesWrite:
		if en {
			return "Write Notes"
		}
		return "写入笔记"
	case PermNetwork:
		if en {
			return "Network"
		}
		return "网络请求"
	case PermEventNoteSaved:
		if en {
			return "Save Hook"
		}
		return "保存触发"
	default:
		if l, ok := contribPermissionLabel(p, en); ok {
			return l
		}
		return string(p)
	}
}

// PermissionDescription returns the built-in localized explanation of what
// granting p allows a plugin to do.
func PermissionDescription(p Permission, locale string) string {
	en := strings.HasPrefix(strings.ToLower(locale), "en")
	if text, ok := systemPermissions[p]; ok {
		if en {
			return text[3]
		}
		return text[2]
	}
	switch p {
	case PermStorage:
		if en {
			return "Read and write this plugin's isolated private settings and state under apps-data."
		}
		return "读写该插件专属的私有配置与状态文件（位于 apps-data 目录）。"
	case PermNotesRead:
		if en {
			return "List and read Markdown notes and folder structure in open notebooks."
		}
		return "列出并读取当前已打开笔记本中的 Markdown 笔记与目录结构。"
	case PermNotesWrite:
		if en {
			return "Create, modify, or update Markdown note files in open notebooks."
		}
		return "在当前已打开笔记本中创建、修改或更新笔记文件。"
	case PermNetwork:
		if en {
			return "Declares that this plugin may make outbound network requests (such as sync, image hosting, or remote APIs)."
		}
		return "声明该插件可能发起外部网络请求（如同步、图床或调用远程 API）。"
	case PermEventNoteSaved:
		if en {
			return "Receive notifications whenever a note is saved so the plugin can run automatic workflows."
		}
		return "在用户保存笔记后接收事件通知，以便自动触发同步或后续处理。"
	default:
		if d, ok := contribPermissionDescription(p, en); ok {
			return d
		}
		return string(p)
	}
}

// ClonePermissionReasons returns a shallow copy of reasons, or nil when empty.
func ClonePermissionReasons(reasons map[Permission]LocalizedText) map[Permission]LocalizedText {
	if len(reasons) == 0 {
		return nil
	}
	out := make(map[Permission]LocalizedText, len(reasons))
	for k, v := range reasons {
		out[k] = v
	}
	return out
}

type Surface string

const (
	SurfaceSettingsPage Surface = "settings_page"
)

var validSurfaces = map[Surface]bool{
	SurfaceSettingsPage: true,
}

type LocalizedText struct {
	Zh string `json:"zh"`
	En string `json:"en"`
}

func (t LocalizedText) Resolve(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "en") && strings.TrimSpace(t.En) != "" {
		return strings.TrimSpace(t.En)
	}
	if strings.TrimSpace(t.Zh) != "" {
		return strings.TrimSpace(t.Zh)
	}
	return strings.TrimSpace(t.En)
}

type EntrySpec struct {
	Command []string `json:"command"`
}

// DuoIconSpec describes an Ocean Paper Scheme A two-tone icon in 24x24 space.
type DuoIconSpec struct {
	ViewBox     string   `json:"view_box"`
	FillPaths   []string `json:"fill_paths,omitempty"`
	StrokePaths []string `json:"stroke_paths"`
	AccentPaths []string `json:"accent_paths,omitempty"`
}

// AppStoreIconSpec returns the three-tiles-plus-add icon for the App Store sidebar entry.
func AppStoreIconSpec() DuoIconSpec {
	return DuoIconSpec{
		ViewBox:   "0 0 24 24",
		FillPaths: []string{
			"M6 4.25H9A1.75 1.75 0 0 1 10.75 6V9A1.75 1.75 0 0 1 9 10.75H6A1.75 1.75 0 0 1 4.25 9V6A1.75 1.75 0 0 1 6 4.25Z",
			"M15 4.25H18A1.75 1.75 0 0 1 19.75 6V9A1.75 1.75 0 0 1 18 10.75H15A1.75 1.75 0 0 1 13.25 9V6A1.75 1.75 0 0 1 15 4.25Z",
			"M6 13.25H9A1.75 1.75 0 0 1 10.75 15V18A1.75 1.75 0 0 1 9 19.75H6A1.75 1.75 0 0 1 4.25 18V15A1.75 1.75 0 0 1 6 13.25Z",
		},
		StrokePaths: []string{
			"M6 4.25H9A1.75 1.75 0 0 1 10.75 6V9A1.75 1.75 0 0 1 9 10.75H6A1.75 1.75 0 0 1 4.25 9V6A1.75 1.75 0 0 1 6 4.25Z",
			"M15 4.25H18A1.75 1.75 0 0 1 19.75 6V9A1.75 1.75 0 0 1 18 10.75H15A1.75 1.75 0 0 1 13.25 9V6A1.75 1.75 0 0 1 15 4.25Z",
			"M6 13.25H9A1.75 1.75 0 0 1 10.75 15V18A1.75 1.75 0 0 1 9 19.75H6A1.75 1.75 0 0 1 4.25 18V15A1.75 1.75 0 0 1 6 13.25Z",
		},
		AccentPaths: []string{"M16.5 13.5V19.5M13.5 16.5H19.5"},
	}
}

type Manifest struct {
	App           string        `json:"app"`
	SchemaVersion int           `json:"schema_version"`
	ID            string        `json:"id"`
	Category      Category      `json:"category,omitempty"`
	Name          LocalizedText `json:"name"`
	Version       string        `json:"version"`
	Beta          bool          `json:"beta,omitempty"`
	Author        string        `json:"author"`
	Repo          string        `json:"repo,omitempty"`
	Summary       LocalizedText `json:"summary"`
	Entry             EntrySpec                    `json:"entry,omitempty"`
	Permissions       []Permission                 `json:"permissions,omitempty"`
	PermissionReasons map[Permission]LocalizedText `json:"permission_reasons,omitempty"`
	Surfaces          []Surface                    `json:"surfaces,omitempty"`
	Icon              DuoIconSpec                  `json:"icon"`
	Contributes       *Contributes                 `json:"contributes,omitempty"`
}

// EffectiveCategory returns the canonical App Store category for m.
func (m *Manifest) EffectiveCategory() Category {
	if m == nil {
		return CategoryTools
	}
	hasLangs := m.Contributes != nil && len(m.Contributes.Languages) > 0
	return ResolveCategory(m.Category, hasLangs)
}

// CategoryNotice explains how m's category could be declared better, or
// returns "" when the manifest declares a current category explicitly.
func (m *Manifest) CategoryNotice() string {
	if m == nil {
		return ""
	}
	if m.Category == "" {
		return fmt.Sprintf("manifest.category is not declared; the plugin defaults to %q", m.EffectiveCategory())
	}
	if RetiredCategory(m.Category) {
		return fmt.Sprintf("manifest.category %q is retired; use %q", m.Category, m.EffectiveCategory())
	}
	return ""
}

// IsDeclarativeOnly reports whether m is a zero-process, zero-permission
// declarative language extension plugin that runs purely in-host.
func (m *Manifest) IsDeclarativeOnly() bool {
	if m == nil {
		return false
	}
	return len(m.Entry.Command) == 0 &&
		len(m.Permissions) == 0 &&
		len(m.Surfaces) == 0 &&
		m.Category == CategoryLanguage &&
		m.Contributes != nil &&
		len(m.Contributes.Languages) > 0 &&
		!m.Contributes.hasRuntimeContributions()
}

var (
	pluginIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,47}$`)
	semverRe   = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	repoSlugRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

func LoadManifest(pluginDir string) (*Manifest, error) {
	root, err := os.OpenRoot(pluginDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := readPluginFile(root, "manifest.json", 256*1024)
	if err != nil {
		return nil, fmt.Errorf("read manifest.json: %w", err)
	}
	return DecodeManifestStrict(raw)
}

func DecodeManifestStrict(raw []byte) (*Manifest, error) {
	if err := checkNoDuplicateJSONKeys(raw); err != nil {
		return nil, fmt.Errorf("decode manifest.json: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest.json: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, errors.New("manifest.json must contain exactly one JSON object")
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func checkNoDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := scanJSONValueNoDupKeys(dec); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("manifest.json must contain exactly one JSON object")
	}
	return nil
}

func scanJSONValueNoDupKeys(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for dec.More() {
			ktok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := ktok.(string)
			if !ok {
				return errors.New("expected string key")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = true
			if err := scanJSONValueNoDupKeys(dec); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	case '[':
		for dec.More() {
			if err := scanJSONValueNoDupKeys(dec); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	}
	return nil
}

func (m *Manifest) HasPermission(p Permission) bool {
	for _, item := range m.Permissions {
		if item == p {
			return true
		}
	}
	return false
}

func (m *Manifest) HasSurface(s Surface) bool {
	for _, item := range m.Surfaces {
		if item == s {
			return true
		}
	}
	return m.hasContribSurface(s)
}

// HasSettingsPage reports whether m declares a dedicated Settings page.
func (m *Manifest) HasSettingsPage() bool {
	if m == nil {
		return false
	}
	return m.HasSurface(SurfaceSettingsPage)
}

func (m *Manifest) Validate() error {
	if m.App != TargetApp {
		return fmt.Errorf("manifest.app must be %q, got %q", TargetApp, m.App)
	}
	if m.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("manifest.schema_version must be %d, got %d", CurrentSchemaVersion, m.SchemaVersion)
	}
	if !pluginIDRe.MatchString(m.ID) {
		return fmt.Errorf("manifest.id %q must match %s", m.ID, pluginIDRe.String())
	}
	if m.Category != "" && !AcceptedCategory(m.Category) {
		return fmt.Errorf("manifest.category %q is not a valid category (allowed: %s)", m.Category, AllowedCategoryNames())
	}
	hasLanguages := m.Contributes != nil && len(m.Contributes.Languages) > 0
	if hasLanguages && m.Category != CategoryLanguage {
		return fmt.Errorf("manifest.category must be %q when contributes.languages is declared", CategoryLanguage)
	}
	if strings.TrimSpace(m.Name.Zh) == "" || strings.TrimSpace(m.Name.En) == "" {
		return errors.New("manifest.name.zh and manifest.name.en are both required")
	}
	if ContainsEmoji(m.Name.Zh) || ContainsEmoji(m.Name.En) {
		return errors.New("manifest.name must not contain emoji")
	}
	// Validation must agree with the store's update comparison
	// (golang.org/x/mod/semver), otherwise a version could pass here but
	// sort as invalid and never surface as an update.
	if !semverRe.MatchString(m.Version) || !semver.IsValid("v"+m.Version) {
		return fmt.Errorf("manifest.version %q must be valid semver (e.g. 1.0.0)", m.Version)
	}
	if strings.TrimSpace(m.Author) == "" {
		return errors.New("manifest.author is required")
	}
	if ContainsEmoji(m.Author) {
		return errors.New("manifest.author must not contain emoji")
	}
	if m.Repo != "" && !repoSlugRe.MatchString(m.Repo) {
		return fmt.Errorf("manifest.repo %q must match owner/repo", m.Repo)
	}
	if strings.TrimSpace(m.Summary.Zh) == "" || strings.TrimSpace(m.Summary.En) == "" {
		return errors.New("manifest.summary.zh and manifest.summary.en are both required")
	}
	if utf8.RuneCountInString(m.Summary.Zh) > 80 || utf8.RuneCountInString(m.Summary.En) > 140 {
		return errors.New("manifest.summary exceeds length limit (zh<=80, en<=140)")
	}
	if ContainsEmoji(m.Summary.Zh) || ContainsEmoji(m.Summary.En) {
		return errors.New("manifest.summary must not contain emoji")
	}
	declarative := m.IsDeclarativeOnly()
	if !declarative {
		if len(m.Entry.Command) == 0 || strings.TrimSpace(m.Entry.Command[0]) == "" {
			return errors.New("manifest.entry.command must be a non-empty string array")
		}
	}
	seenPerm := map[Permission]bool{}
	for _, p := range m.Permissions {
		if !validPermissions[p] {
			return fmt.Errorf("manifest.permissions contains unknown permission %q", p)
		}
		if seenPerm[p] {
			return fmt.Errorf("manifest.permissions duplicates %q", p)
		}
		seenPerm[p] = true
	}
	for perm, reason := range m.PermissionReasons {
		if !validPermissions[perm] {
			return fmt.Errorf("manifest.permission_reasons contains unknown permission %q", perm)
		}
		if !seenPerm[perm] {
			return fmt.Errorf("manifest.permission_reasons[%q] requires %q in manifest.permissions", perm, perm)
		}
		zh := strings.TrimSpace(reason.Zh)
		en := strings.TrimSpace(reason.En)
		if zh == "" && en == "" {
			return fmt.Errorf("manifest.permission_reasons[%q] must provide zh or en text", perm)
		}
		if utf8.RuneCountInString(zh) > 80 || utf8.RuneCountInString(en) > 140 {
			return fmt.Errorf("manifest.permission_reasons[%q] exceeds length limit (zh<=80, en<=140)", perm)
		}
		if ContainsEmoji(zh) || ContainsEmoji(en) {
			return fmt.Errorf("manifest.permission_reasons[%q] must not contain emoji", perm)
		}
	}
	seenSurf := map[Surface]bool{}
	for _, s := range m.Surfaces {
		if !validSurfaces[s] {
			return fmt.Errorf("manifest.surfaces contains unsupported surface %q (only \"settings_page\" is supported)", s)
		}
		if seenSurf[s] {
			return fmt.Errorf("manifest.surfaces duplicates %q", s)
		}
		seenSurf[s] = true
	}
	if err := validateContribIcon("manifest.icon", m.Icon); err != nil {
		return err
	}
	return m.validateContributes()
}

// ContainsEmoji enforces Ocean Paper's no-emoji rule across manifest & declarative UI copy.
func ContainsEmoji(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x1F300 && r <= 0x1FAFF:
			return true
		case r >= 0x2600 && r <= 0x27BF:
			return true
		case r >= 0xFE00 && r <= 0xFE0F:
			return true
		case r >= 0x1F000 && r <= 0x1F2FF: // mahjong, cards, enclosed alphanumerics, regional indicators
			return true
		case r >= 0xE0020 && r <= 0xE007F: // tag sequences (subdivision flags)
			return true
		case r == 0x200D || r == 0x20E3: // ZWJ, keycap
			return true
		case r == 0x203C || r == 0x2049 || r == 0x24C2:
			return true
		case r == 0x231A || r == 0x231B || r == 0x2328 || r == 0x23CF:
			return true
		case r >= 0x23E9 && r <= 0x23F3, r >= 0x23F8 && r <= 0x23FA:
			return true
		case r >= 0x2B05 && r <= 0x2B07, r == 0x2B1B, r == 0x2B1C, r == 0x2B50, r == 0x2B55:
			return true
		case r == 0x3030 || r == 0x303D || r == 0x3297 || r == 0x3299:
			return true
		}
	}
	return false
}
