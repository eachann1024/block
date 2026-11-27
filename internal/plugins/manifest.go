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

	"block/internal/plugins/wasmrt"
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
	PermSystemFS       Permission = "system:fs"
	PermSystemExec     Permission = "system:exec"
	PermAIComplete     Permission = "ai:complete"
)

var validPermissions = map[Permission]bool{
	PermStorage:        true,
	PermNotesRead:      true,
	PermNotesWrite:     true,
	PermNetwork:        true,
	PermEventNoteSaved: true,
	PermSystemFS:       true,
	PermSystemExec:     true,
	PermAIComplete:     true,
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
	case PermSystemFS:
		if en {
			return "Scoped File Access"
		}
		return "受控文件读写"
	case PermSystemExec:
		if en {
			return "Allowlisted Executables"
		}
		return "受控命令行工具"
	case PermAIComplete:
		if en {
			return "AI Completion"
		}
		return "AI 生成"
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
			return "Self-declared by the plugin (such as sync, image hosting, or remote APIs). Block does not sandbox outbound network access."
		}
		return "插件自述会发起外部网络请求（如同步、图床或调用远程 API）；Block 不在进程层拦截联网。"
	case PermEventNoteSaved:
		if en {
			return "Receive notifications whenever a note is saved so the plugin can run automatic workflows."
		}
		return "在用户保存笔记后接收事件通知，以便自动触发同步或后续处理。"
	case PermSystemFS:
		if en {
			return "Read and write files inside plugin data, Downloads, known notebooks, and paths explicitly granted via pickers or drag-and-drop."
		}
		return "在插件数据目录、下载目录、已知笔记本以及通过选择框或拖放明确授权的路径内读写文件。"
	case PermSystemExec:
		if en {
			return "Run specific allowlisted executables declared in the manifest without a shell inside granted directories."
		}
		return "以无 Shell 方式运行清单白名单声明的特定外部程序，并在授权目录内执行。"
	case PermAIComplete:
		if en {
			return "Call the configured AI model on demand to generate text or open the AI panel with a prompt."
		}
		return "按需调用已配置的 AI 模型生成文本，或携带提示词打开 AI 面板。"
	default:
		if d, ok := contribPermissionDescription(p, en); ok {
			return d
		}
		return string(p)
	}
}

// PermissionCategory groups sensitive permissions shown in install and update
// confirmation modals. Only permissions in these 5 categories require explicit
// user confirmation; all UI/storage/workspace-event capabilities are exempt.
type PermissionCategory string

const (
	PermCategoryNotes       PermissionCategory = "notes"
	PermCategoryCredentials PermissionCategory = "credentials"
	PermCategoryClipboard   PermissionCategory = "clipboard"
	PermCategorySystem      PermissionCategory = "system"
	PermCategoryNetwork     PermissionCategory = "network"
)

// PermissionCategoryOrder defines the stable display order of the 5 sensitive categories.
var PermissionCategoryOrder = []PermissionCategory{
	PermCategoryNotes,
	PermCategoryCredentials,
	PermCategoryClipboard,
	PermCategorySystem,
	PermCategoryNetwork,
}

// PermissionCategoryOf returns the sensitive category of p, or ("", false) when
// p does not require user consent (UI contributions, private storage, events,
// OS notifications, and dock badge).
func PermissionCategoryOf(p Permission) (PermissionCategory, bool) {
	switch p {
	case PermNotesRead, PermNotesWrite, PermNotesPath, PermEditorRead, PermEditorEdit:
		return PermCategoryNotes, true
	case PermSystemSecrets:
		return PermCategoryCredentials, true
	case PermClipboardRead, PermClipboardWrite:
		return PermCategoryClipboard, true
	case PermSystemTrash, PermSystemOpen, PermSystemDialogs, PermSystemFS, PermSystemExec, PermAIComplete:
		return PermCategorySystem, true
	case PermNetwork:
		return PermCategoryNetwork, true
	default:
		return "", false
	}
}

// IsSensitivePermission reports whether p belongs to one of the 5 user-consent
// permission categories.
func IsSensitivePermission(p Permission) bool {
	_, ok := PermissionCategoryOf(p)
	return ok
}

// IsExemptPermission reports whether p is a valid permission that is now granted
// automatically without needing to be declared in manifest.permissions.
// Event hooks (events:note_saved, events:system, events:workspace) remain silent
// opt-in subscription flags (never prompting the user) and therefore return false.
func IsExemptPermission(p Permission) bool {
	if !validPermissions[p] || IsSensitivePermission(p) {
		return false
	}
	return p != PermEventNoteSaved && p != PermEventsSystem && p != PermEventsUI
}

// PermissionCategoryLabel returns the localized heading for a sensitive category.
func PermissionCategoryLabel(cat PermissionCategory, locale string) string {
	en := strings.HasPrefix(strings.ToLower(locale), "en")
	switch cat {
	case PermCategoryNotes:
		if en {
			return "Notes & Editor"
		}
		return "笔记与编辑"
	case PermCategoryCredentials:
		if en {
			return "System Credentials"
		}
		return "系统凭据"
	case PermCategoryClipboard:
		if en {
			return "Clipboard"
		}
		return "剪贴板"
	case PermCategorySystem:
		if en {
			return "System Files & Actions"
		}
		return "系统文件与外部操作"
	case PermCategoryNetwork:
		if en {
			return "Network Access (Self-Declared)"
		}
		return "网络访问（插件自述）"
	default:
		return string(cat)
	}
}

// SensitivePermissions filters perms to sensitive permissions in stable order (deduped).
func SensitivePermissions(perms []Permission) []Permission {
	if len(perms) == 0 {
		return nil
	}
	seen := make(map[Permission]bool, len(perms))
	out := make([]Permission, 0, len(perms))
	for _, p := range perms {
		if !IsSensitivePermission(p) || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// AddedSensitivePermissions returns sensitive permissions present in next that
// were not declared in current.
func AddedSensitivePermissions(current, next []Permission) []Permission {
	if len(next) == 0 {
		return nil
	}
	have := make(map[Permission]bool, len(current))
	for _, p := range current {
		if IsSensitivePermission(p) {
			have[p] = true
		}
	}
	seen := make(map[Permission]bool, len(next))
	var added []Permission
	for _, p := range next {
		if !IsSensitivePermission(p) || have[p] || seen[p] {
			continue
		}
		seen[p] = true
		added = append(added, p)
	}
	return added
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
	// SurfaceToast is passed to plugin.action when a user clicks an action
	// button on a notification banner created by host.ui.toast.
	SurfaceToast Surface = "toast"
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

func (t LocalizedText) IsEmpty() bool {
	return strings.TrimSpace(t.Zh) == "" && strings.TrimSpace(t.En) == ""
}

type EntrySpec struct {
	JS   string `json:"js,omitempty"`
	Wasm string `json:"wasm,omitempty"`
}

// IsEmpty reports whether no sandbox entry is configured.
func (e EntrySpec) IsEmpty() bool {
	return e.JS == "" && e.Wasm == ""
}

// DuoIconSpec describes an Ocean Paper Scheme A two-tone icon in 24x24 space.
type DuoIconSpec struct {
	ViewBox         string   `json:"view_box"`
	FillPaths       []string `json:"fill_paths,omitempty"`
	StrokePaths     []string `json:"stroke_paths"`
	AccentPaths     []string `json:"accent_paths,omitempty"`
	AccentFillPaths []string `json:"accent_fill_paths,omitempty"`
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
	NetworkDomains    []string                     `json:"network_domains,omitempty"`
	ExecAllowlist     []string                     `json:"exec_allowlist,omitempty"`
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
	return m.Entry.IsEmpty() &&
		len(m.Permissions) == 0 &&
		len(m.Surfaces) == 0 &&
		m.Category == CategoryLanguage &&
		m.Contributes != nil &&
		len(m.Contributes.Languages) > 0 &&
		!m.Contributes.hasRuntimeContributions()
}

var (
	pluginIDRe             = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,47}$`)
	semverRe               = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	repoSlugRe             = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	execAllowlistNameRe    = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	versionedInterpreterRe = regexp.MustCompile(`^(pythonw?|pyw?)[0-9]+(\.[0-9]+)*$`)
)

var forbiddenExecPrograms = map[string]bool{
	"sh":         true,
	"bash":       true,
	"zsh":        true,
	"dash":       true,
	"fish":       true,
	"cmd":        true,
	"cmd.exe":    true,
	"powershell": true,
	"pwsh":       true,
	"python":     true,
	"pythonw":    true,
	"py":         true,
	"pyw":        true,
	"node":       true,
	"perl":       true,
	"ruby":       true,
	"osascript":  true,
}

func validateExecAllowlistProgram(prog string) error {
	if prog == "" || prog != strings.TrimSpace(prog) {
		return errors.New("manifest.exec_allowlist entries must not be empty or have surrounding whitespace")
	}
	if prog == "." || prog == ".." || !execAllowlistNameRe.MatchString(prog) {
		return fmt.Errorf("manifest.exec_allowlist entry %q must be a bare executable basename matching ^[a-zA-Z0-9._-]+$", prog)
	}
	lower := strings.ToLower(prog)
	base := lower
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com"} {
		if trimmed, ok := strings.CutSuffix(base, ext); ok {
			base = trimmed
			break
		}
	}
	if forbiddenExecPrograms[lower] || forbiddenExecPrograms[base] || versionedInterpreterRe.MatchString(base) {
		return fmt.Errorf("manifest.exec_allowlist forbids shell or script interpreter %q", prog)
	}
	return nil
}

func validateNetworkDomain(domain string) error {
	if domain == "" || domain != strings.TrimSpace(domain) {
		return errors.New("manifest.network_domains entries must not be empty or have surrounding whitespace")
	}
	if strings.ContainsAny(domain, " \t\r\n\x00/?#@\\") || strings.Contains(domain, "://") {
		return fmt.Errorf("manifest.network_domains entry %q must be a host or *.domain pattern without scheme or path", domain)
	}
	if strings.Contains(domain, "*") {
		if !strings.HasPrefix(domain, "*.") || strings.Count(domain, "*") != 1 || len(domain) <= 2 || strings.HasSuffix(domain, ".") {
			return fmt.Errorf("manifest.network_domains wildcard %q must have form *.example.com", domain)
		}
	}
	return nil
}

var errRetiredPluginEntry = errors.New("该插件使用已停用的旧版运行方式，请更新到新版。 This plugin uses a retired runtime. Please update it to a newer version.")

func LoadManifest(pluginDir string) (*Manifest, error) {
	raw, err := readManifest(pluginDir)
	if err != nil {
		return nil, err
	}
	return DecodeManifestStrict(raw)
}

// loadInstalledManifest reads metadata for an existing installation, including
// packages whose retired entry can be replaced by a sandboxed update.
func loadInstalledManifest(pluginDir string) (*Manifest, bool, error) {
	raw, err := readManifest(pluginDir)
	if err != nil {
		return nil, false, err
	}
	m, err := decodeManifest(raw)
	retired := errors.Is(err, errRetiredPluginEntry) && m.Entry.IsEmpty()
	if err != nil && !retired {
		return nil, false, err
	}
	if err := m.validate(retired); err != nil {
		return nil, false, err
	}
	return m, retired, nil
}

func readManifest(pluginDir string) ([]byte, error) {
	root, err := os.OpenRoot(pluginDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := readPluginFile(root, "manifest.json", 256*1024)
	if err != nil {
		return nil, fmt.Errorf("read manifest.json: %w", err)
	}
	return raw, nil
}

func DecodeManifestStrict(raw []byte) (*Manifest, error) {
	m, err := decodeManifest(raw)
	if err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func decodeManifest(raw []byte) (*Manifest, error) {
	if err := checkNoDuplicateJSONKeys(raw); err != nil {
		return nil, fmt.Errorf("decode manifest.json: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var decoded struct {
		Manifest
		Entry struct {
			EntrySpec
			RetiredCommand json.RawMessage `json:"command"`
		} `json:"entry"`
	}
	if err := dec.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode manifest.json: %w", err)
	}
	decoded.Manifest.Entry = decoded.Entry.EntrySpec
	if decoded.Entry.RetiredCommand != nil {
		return &decoded.Manifest, errRetiredPluginEntry
	}
	return &decoded.Manifest, nil
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

// HasDeclaredPermission reports whether p is explicitly listed in m.Permissions.
func (m *Manifest) HasDeclaredPermission(p Permission) bool {
	if m == nil {
		return false
	}
	for _, item := range m.Permissions {
		if item == p {
			return true
		}
	}
	return false
}

// HasPermission reports whether the plugin is allowed to use capability p.
// Exempt UI, private storage, OS notification, and dock-badge capabilities are
// granted automatically without needing manifest declaration. Event hooks
// (events:*) remain opt-in subscriptions checked against manifest.permissions.
func (m *Manifest) HasPermission(p Permission) bool {
	if m == nil {
		return false
	}
	if IsExemptPermission(p) {
		return true
	}
	return m.HasDeclaredPermission(p)
}

// RedundantPermissions returns permissions declared in m.Permissions that are
// now exempt and no longer need to be declared.
func (m *Manifest) RedundantPermissions() []Permission {
	if m == nil || len(m.Permissions) == 0 {
		return nil
	}
	var out []Permission
	for _, p := range m.Permissions {
		// ui:sidebar is required for the directory filter, while older sidebar APIs stay exempt.
		if p != PermUISidebar && IsExemptPermission(p) {
			out = append(out, p)
		}
	}
	return out
}

// ToastToneIconSpec returns an Ocean Paper Scheme A 24x24 icon for a built-in
// notification tone ("success", "warning", "error", or "info").
func ToastToneIconSpec(tone string) DuoIconSpec {
	switch normalizeTone(tone) {
	case "success":
		return DuoIconSpec{
			ViewBox:     "0 0 24 24",
			FillPaths:   []string{"M12 3A9 9 0 1 1 3 12A9 9 0 0 1 12 3Z"},
			StrokePaths: []string{"M12 3A9 9 0 1 1 3 12A9 9 0 0 1 12 3Z"},
			AccentPaths: []string{"M8.5 12.4L10.9 14.8L15.8 9.6"},
		}
	case "warning":
		return DuoIconSpec{
			ViewBox:     "0 0 24 24",
			FillPaths:   []string{"M10.42 4.65L3.12 17.3A1.8 1.8 0 0 0 4.68 20H19.32A1.8 1.8 0 0 0 20.88 17.3L13.58 4.65A1.8 1.8 0 0 0 10.42 4.65Z"},
			StrokePaths: []string{"M10.42 4.65L3.12 17.3A1.8 1.8 0 0 0 4.68 20H19.32A1.8 1.8 0 0 0 20.88 17.3L13.58 4.65A1.8 1.8 0 0 0 10.42 4.65Z"},
			AccentPaths: []string{"M12 9.2V13.6M12 16.6V16.7"},
		}
	case "error":
		return DuoIconSpec{
			ViewBox:     "0 0 24 24",
			FillPaths:   []string{"M12 3A9 9 0 1 1 3 12A9 9 0 0 1 12 3Z"},
			StrokePaths: []string{"M12 3A9 9 0 1 1 3 12A9 9 0 0 1 12 3Z"},
			AccentPaths: []string{"M9.2 9.2L14.8 14.8M14.8 9.2L9.2 14.8"},
		}
	default:
		return DuoIconSpec{
			ViewBox:     "0 0 24 24",
			FillPaths:   []string{"M12 3A9 9 0 1 1 3 12A9 9 0 0 1 12 3Z"},
			StrokePaths: []string{"M12 3A9 9 0 1 1 3 12A9 9 0 0 1 12 3Z"},
			AccentPaths: []string{"M12 11.2V16M12 8.2V8.3"},
		}
	}
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
	return m.validate(false)
}

func (m *Manifest) validate(retiredEntry bool) error {
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
	hasJS := m.Entry.JS != ""
	hasWasm := m.Entry.Wasm != ""
	switch {
	case hasJS && hasWasm:
		return errors.New("manifest.entry must specify at most one of js or wasm")
	case hasJS:
		if _, err := wasmrt.ValidateEntryPath(m.Entry.JS, ".js", ".mjs"); err != nil {
			return fmt.Errorf("manifest.entry.js: %w", err)
		}
	case hasWasm:
		if _, err := wasmrt.ValidateEntryPath(m.Entry.Wasm, ".wasm"); err != nil {
			return fmt.Errorf("manifest.entry.wasm: %w", err)
		}
	case !retiredEntry && !m.IsDeclarativeOnly():
		return errors.New("manifest.entry must specify one of js or wasm")
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
	if len(m.NetworkDomains) > 64 {
		return errors.New("manifest.network_domains allows at most 64 entries")
	}
	seenDomains := map[string]bool{}
	for _, d := range m.NetworkDomains {
		if err := validateNetworkDomain(d); err != nil {
			return err
		}
		key := strings.ToLower(d)
		if seenDomains[key] {
			return fmt.Errorf("manifest.network_domains duplicates %q", d)
		}
		seenDomains[key] = true
	}
	if len(m.ExecAllowlist) > 8 {
		return errors.New("manifest.exec_allowlist allows at most 8 entries")
	}
	seenExec := map[string]bool{}
	for _, prog := range m.ExecAllowlist {
		if !retiredEntry {
			if err := validateExecAllowlistProgram(prog); err != nil {
				return err
			}
		}
		if seenExec[prog] {
			return fmt.Errorf("manifest.exec_allowlist duplicates %q", prog)
		}
		seenExec[prog] = true
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
