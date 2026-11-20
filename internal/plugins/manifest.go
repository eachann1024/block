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

// AppStoreIconSpec returns the standard Ocean Paper 2x2 grid icon for the App Store sidebar entry.
func AppStoreIconSpec() DuoIconSpec {
	return DuoIconSpec{
		ViewBox:   "0 0 24 24",
		FillPaths: []string{"M4 4h6.5v6.5H4z", "M4 13.5h6.5V20H4z", "M13.5 13.5H20V20h-6.5z"},
		StrokePaths: []string{
			"M4.8 4h4.9c.44 0 .8.36.8.8v4.9c0 .44-.36.8-.8.8H4.8a.8.8 0 0 1-.8-.8V4.8c0-.44.36-.8.8-.8Z",
			"M4.8 13.5h4.9c.44 0 .8.36.8.8v4.9c0 .44-.36.8-.8.8H4.8a.8.8 0 0 1-.8-.8v-4.9c0-.44.36-.8.8-.8Z",
			"M14.3 13.5h4.9c.44 0 .8.36.8.8v4.9c0 .44-.36.8-.8.8h-4.9a.8.8 0 0 1-.8-.8v-4.9c0-.44.36-.8.8-.8Z",
		},
		AccentPaths: []string{
			"M14.3 4h4.9c.44 0 .8.36.8.8v4.9c0 .44-.36.8-.8.8h-4.9a.8.8 0 0 1-.8-.8V4.8c0-.44.36-.8.8-.8Z",
		},
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
	Entry         EntrySpec     `json:"entry,omitempty"`
	Permissions   []Permission  `json:"permissions,omitempty"`
	Surfaces      []Surface     `json:"surfaces,omitempty"`
	Icon          DuoIconSpec   `json:"icon"`
	Contributes   *Contributes  `json:"contributes,omitempty"`
}

// EffectiveCategory returns the canonical App Store category for m.
func (m *Manifest) EffectiveCategory() Category {
	if m == nil {
		return CategoryTools
	}
	hasLangs := m.Contributes != nil && len(m.Contributes.Languages) > 0
	return ResolveCategory(m.Category, m.ID, hasLangs)
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
	if m.Category != "" && !ValidCategory(m.Category) {
		return fmt.Errorf("manifest.category %q is not a valid category (allowed: sync, edit, appearance, tools, language)", m.Category)
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
	if !semverRe.MatchString(m.Version) {
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
	if !declarative && len(m.Surfaces) == 0 {
		return errors.New("manifest.surfaces must declare [\"settings_page\"]")
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
	if m.Icon.ViewBox != "0 0 24 24" {
		return fmt.Errorf("manifest.icon.view_box must be \"0 0 24 24\", got %q", m.Icon.ViewBox)
	}
	if len(m.Icon.StrokePaths) == 0 {
		return errors.New("manifest.icon.stroke_paths must contain at least one SVG path")
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
		}
	}
	return false
}
