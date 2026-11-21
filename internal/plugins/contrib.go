package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UI contribution permissions. Each contribution kind in manifest.contributes
// requires its matching permission so the install sheet can show it.
const (
	PermUITitlebar   Permission = "ui:titlebar"   // titlebar buttons + titlebar layout control
	PermUISettings   Permission = "ui:settings"   // sections inside built-in settings tabs
	PermUICommands   Permission = "ui:commands"   // commands, shortcuts, page / editor / tab menu items
	PermUIPanels     Permission = "ui:panels"     // side panels
	PermUIStatus     Permission = "ui:status"     // status bar items
	PermUINotify     Permission = "ui:notify"     // toasts and plugin dialogs
	PermUISidebar    Permission = "ui:sidebar"    // left sidebar sections, file tree badges & context menu
	PermUIAppearance Permission = "ui:appearance" // custom vector editor canvas backdrops
	PermEditorRead   Permission = "editor:read"
	PermEditorEdit   Permission = "editor:write"
	PermEventsUI     Permission = "events:workspace" // note.opened / theme.changed / locale.changed
	PermNotesPath    Permission = "notes:path"       // absolute file path in plugin.command context
)

func init() {
	for _, p := range []Permission{
		PermUITitlebar, PermUISettings, PermUICommands, PermUIPanels, PermUIStatus, PermUINotify,
		PermUISidebar, PermUIAppearance, PermEditorRead, PermEditorEdit, PermEventsUI, PermNotesPath,
	} {
		validPermissions[p] = true
	}
}

func contribPermissionLabel(p Permission, en bool) (string, bool) {
	labels := map[Permission][2]string{
		PermUITitlebar:   {"顶栏按钮", "Titlebar"},
		PermUISettings:   {"设置项", "Settings Items"},
		PermUICommands:   {"命令与菜单", "Commands & Menus"},
		PermUIPanels:     {"侧边面板", "Side Panels"},
		PermUIStatus:     {"状态栏", "Status Bar"},
		PermUINotify:     {"提示与弹窗", "Notices & Dialogs"},
		PermUISidebar:    {"左侧栏扩展", "Sidebar Extensions"},
		PermUIAppearance: {"编辑区背景", "Editor Backdrops"},
		PermEditorRead:   {"读取选区", "Read Selection"},
		PermEditorEdit:   {"编辑正文", "Edit Note Text"},
		PermEventsUI:     {"界面事件", "Workspace Events"},
		PermNotesPath:    {"文件路径", "File Paths"},
	}
	l, ok := labels[p]
	if !ok {
		return "", false
	}
	if en {
		return l[1], true
	}
	return l[0], true
}

func contribPermissionDescription(p Permission, en bool) (string, bool) {
	descs := map[Permission][2]string{
		PermUITitlebar: {
			"在窗口顶栏右侧添加图标按钮，或调整顶栏控件的显隐布局。",
			"Add icon buttons to the titlebar right rail or control titlebar item visibility.",
		},
		PermUISettings: {
			"在内置设置页（AI、外观、文件夹、快捷键）末尾嵌入专属设置卡片。",
			"Embed custom settings cards inside built-in settings tabs.",
		},
		PermUICommands: {
			"向命令面板、默认快捷键、页面「…」菜单、编辑器右键菜单或标签页右键菜单注册操作。",
			"Register commands, default shortcuts, page menu items, editor context menu items, or tab context menu items.",
		},
		PermUIPanels: {
			"在工作区右侧打开或切换插件提供的原生侧边面板。",
			"Open or toggle native side panels provided by the plugin.",
		},
		PermUIStatus: {
			"在窗口底部状态栏展示简洁的实时状态与点击操作。",
			"Display compact status indicators and actions in the bottom status bar.",
		},
		PermUINotify: {
			"在应用内弹出轻提示（Toast）或原生声明式对话框。",
			"Show in-app toast notifications or native declarative dialogs.",
		},
		PermUISidebar: {
			"在左侧栏底部添加扩展分区、在文件树笔记旁显示自定义角标，或追加侧栏右键菜单项。",
			"Add sections to the left sidebar, show custom badges on file tree notes, or add sidebar context menu items.",
		},
		PermUIAppearance: {
			"在编辑区背景层绘制自定义矢量水印或平铺图案（自动限制透明度以保证阅读清晰）。",
			"Render custom vector watermarks or tiled patterns behind the editor canvas with safe opacity limits.",
		},
		PermEditorRead: {
			"读取当前编辑中的笔记选中文本与光标上下文。",
			"Read the active selection and cursor context in the current editor.",
		},
		PermEditorEdit: {
			"向当前编辑器光标位置或选区插入、替换正文内容。",
			"Insert or replace text at the active cursor or selection in the editor.",
		},
		PermEventsUI: {
			"监听切换笔记、切换深浅色主题或切换界面语言等工作区事件。",
			"Listen for workspace events such as note open, theme change, or locale change.",
		},
		PermNotesPath: {
			"在你触发插件命令时，获取目标文件在本机上的完整路径（含笔记本外打开的文件），不读取文件内容。",
			"Receive the full on-disk path of the target file (including files opened outside notebooks) when you run a plugin command. File contents are not read.",
		},
	}
	d, ok := descs[p]
	if !ok {
		return "", false
	}
	if en {
		return d[1], true
	}
	return d[0], true
}

// Prefixed surfaces rendered with plugin.render / plugin.action.
const (
	SurfacePrefixSettingsSection = "settings_section:"
	SurfacePrefixPanel           = "panel:"
	SurfacePrefixDialog          = "dialog:"
	SurfacePrefixSidebarSection  = "sidebar_section:"
)

func SettingsSectionSurface(id string) Surface { return Surface(SurfacePrefixSettingsSection + id) }
func PanelSurface(id string) Surface           { return Surface(SurfacePrefixPanel + id) }
func DialogSurface(id string) Surface          { return Surface(SurfacePrefixDialog + id) }
func SidebarSectionSurface(id string) Surface  { return Surface(SurfacePrefixSidebarSection + id) }

var contribIDRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Built-in settings tabs that accept plugin sections.
var SettingsSectionTabs = []string{"ai", "appearance", "folders", "shortcuts"}

// Contributes declares where a plugin places UI or language extensions in the host.
type Contributes struct {
	TitlebarButtons  []TitlebarButtonContrib  `json:"titlebar_buttons,omitempty"`
	SettingsSections []SettingsSectionContrib `json:"settings_sections,omitempty"`
	Commands         []CommandContrib         `json:"commands,omitempty"`
	PageMenu         []MenuContrib            `json:"page_menu,omitempty"`
	EditorMenu       []MenuContrib            `json:"editor_menu,omitempty"`
	SidebarMenu      []MenuContrib            `json:"sidebar_menu,omitempty"`
	TabMenu          []MenuContrib            `json:"tab_menu,omitempty"`
	Panels           []PanelContrib           `json:"panels,omitempty"`
	SidebarSections  []SidebarSectionContrib  `json:"sidebar_sections,omitempty"`
	FileBadges       []FileBadgeContrib       `json:"file_badges,omitempty"`
	Backdrops        []BackdropContrib        `json:"backdrops,omitempty"`
	StatusItems      []StatusItemContrib      `json:"status_items,omitempty"`
	Dialogs          []DialogContrib          `json:"dialogs,omitempty"`
	Languages        []LanguageContrib        `json:"languages,omitempty"`
}

// TitlebarButtonContrib is an icon button in the titlebar right rail. Clicking
// runs Command (a declared command id) or toggles Panel (a declared panel id).
type TitlebarButtonContrib struct {
	ID      string        `json:"id"`
	Icon    DuoIconSpec   `json:"icon"`
	Tooltip LocalizedText `json:"tooltip"`
	Command string        `json:"command,omitempty"`
	Panel   string        `json:"panel,omitempty"`
}

// SettingsSectionContrib appends a card rendered from surface
// "settings_section:<id>" to the end of a built-in settings tab.
type SettingsSectionContrib struct {
	ID    string        `json:"id"`
	Tab   string        `json:"tab"`
	Title LocalizedText `json:"title"`
	Icon  *DuoIconSpec  `json:"icon,omitempty"`
}

// CommandContrib appears in the command palette; Shortcut is an optional
// default binding in shortcuts.go spec form, e.g. "cmd-shift-t".
type CommandContrib struct {
	ID       string        `json:"id"`
	Title    LocalizedText `json:"title"`
	Shortcut string        `json:"shortcut,omitempty"`
	Icon     *DuoIconSpec  `json:"icon,omitempty"`
}

// MenuContrib adds an item to the page "…" menu, editor context menu, or sidebar tree context menu.
type MenuContrib struct {
	ID      string        `json:"id"`
	Title   LocalizedText `json:"title"`
	Command string        `json:"command"`
	Icon    *DuoIconSpec  `json:"icon,omitempty"`
	// Targets limits sidebar_menu items to "note" and/or "folder" rows.
	// Empty means ["note"]. Other menu kinds must leave it empty.
	Targets []string `json:"targets,omitempty"`
}

// showsOn reports whether a sidebar menu item applies to a note or folder row.
func (it MenuContrib) showsOn(folder bool) bool {
	if len(it.Targets) == 0 {
		return !folder
	}
	want := "note"
	if folder {
		want = "folder"
	}
	for _, t := range it.Targets {
		if t == want {
			return true
		}
	}
	return false
}

// PanelContrib is a right side panel rendered from surface "panel:<id>".
type PanelContrib struct {
	ID    string        `json:"id"`
	Title LocalizedText `json:"title"`
	Icon  DuoIconSpec   `json:"icon"`
}

// SidebarSectionContrib appends a compact declarative card rendered from
// surface "sidebar_section:<id>" to the bottom of the left sidebar above the footer.
type SidebarSectionContrib struct {
	ID               string        `json:"id"`
	Title            LocalizedText `json:"title"`
	Icon             DuoIconSpec   `json:"icon"`
	Collapsible      bool          `json:"collapsible,omitempty"`
	DefaultCollapsed bool          `json:"default_collapsed,omitempty"`
}

// FileBadgeContrib declares a reusable file-tree row badge icon pushed at runtime via host.sidebar.set_badges.
type FileBadgeContrib struct {
	ID      string        `json:"id"`
	Icon    DuoIconSpec   `json:"icon"`
	Tooltip LocalizedText `json:"tooltip,omitempty"`
}

// BackdropContrib declares a custom vector backdrop that can be applied behind the editor canvas.
type BackdropContrib struct {
	ID       string        `json:"id"`
	Title    LocalizedText `json:"title"`
	Icon     DuoIconSpec   `json:"icon"`
	Pattern  string        `json:"pattern,omitempty"` // "scatter" | "grid" | "watermark" | "corners"
	Density  string        `json:"density,omitempty"` // "sparse" | "medium" | "dense"
	Opacity  float64       `json:"opacity,omitempty"` // 0 (default 0.09) or 0.03..0.25
	Tint     string        `json:"tint,omitempty"`    // "accent" | "muted" | "warm" | "glow"
	DarkGlow bool          `json:"dark_glow,omitempty"`
}

// StatusItemContrib is a short label in the status strip. Text is the initial
// value until the plugin calls host.status.set.
type StatusItemContrib struct {
	ID      string        `json:"id"`
	Text    LocalizedText `json:"text,omitempty"`
	Command string        `json:"command,omitempty"`
	Icon    *DuoIconSpec  `json:"icon,omitempty"`
}

// DialogContrib is a modal rendered from surface "dialog:<id>", opened by
// host.ui.open_dialog.
type DialogContrib struct {
	ID    string        `json:"id"`
	Title LocalizedText `json:"title"`
	Width int           `json:"width,omitempty"`
	Icon  *DuoIconSpec  `json:"icon,omitempty"`
}

func (c *Contributes) hasRuntimeContributions() bool {
	return c != nil && (len(c.TitlebarButtons) > 0 || len(c.SettingsSections) > 0 || len(c.Commands) > 0 ||
		len(c.PageMenu) > 0 || len(c.EditorMenu) > 0 || len(c.SidebarMenu) > 0 || len(c.TabMenu) > 0 ||
		len(c.Panels) > 0 || len(c.SidebarSections) > 0 || len(c.FileBadges) > 0 ||
		len(c.Backdrops) > 0 || len(c.StatusItems) > 0 || len(c.Dialogs) > 0)
}

func (c *Contributes) empty() bool {
	return c == nil || (!c.hasRuntimeContributions() && len(c.Languages) == 0)
}

func (c *Contributes) hasCommand(id string) bool {
	return c != nil && slices.ContainsFunc(c.Commands, func(x CommandContrib) bool { return x.ID == id })
}

func (c *Contributes) hasPanel(id string) bool {
	return c != nil && slices.ContainsFunc(c.Panels, func(x PanelContrib) bool { return x.ID == id })
}

func (c *Contributes) hasDialog(id string) bool {
	return c != nil && slices.ContainsFunc(c.Dialogs, func(x DialogContrib) bool { return x.ID == id })
}

func (c *Contributes) hasSection(id string) bool {
	return c != nil && slices.ContainsFunc(c.SettingsSections, func(x SettingsSectionContrib) bool { return x.ID == id })
}

func (c *Contributes) hasSidebarSection(id string) bool {
	return c != nil && slices.ContainsFunc(c.SidebarSections, func(x SidebarSectionContrib) bool { return x.ID == id })
}

func (c *Contributes) hasFileBadge(id string) bool {
	return c != nil && slices.ContainsFunc(c.FileBadges, func(x FileBadgeContrib) bool { return x.ID == id })
}

func (c *Contributes) backdropByID(id string) (BackdropContrib, bool) {
	if c == nil {
		return BackdropContrib{}, false
	}
	for _, b := range c.Backdrops {
		if b.ID == id {
			return b, true
		}
	}
	return BackdropContrib{}, false
}

// hasContribSurface reports whether a prefixed surface is declared in contributes.
func (m *Manifest) hasContribSurface(s Surface) bool {
	raw := string(s)
	switch {
	case strings.HasPrefix(raw, SurfacePrefixSettingsSection):
		return m.Contributes.hasSection(strings.TrimPrefix(raw, SurfacePrefixSettingsSection))
	case strings.HasPrefix(raw, SurfacePrefixPanel):
		return m.Contributes.hasPanel(strings.TrimPrefix(raw, SurfacePrefixPanel))
	case strings.HasPrefix(raw, SurfacePrefixDialog):
		return m.Contributes.hasDialog(strings.TrimPrefix(raw, SurfacePrefixDialog))
	case strings.HasPrefix(raw, SurfacePrefixSidebarSection):
		return m.Contributes.hasSidebarSection(strings.TrimPrefix(raw, SurfacePrefixSidebarSection))
	}
	return false
}

func validateContribText(field string, t LocalizedText) error {
	if strings.TrimSpace(t.Zh) == "" || strings.TrimSpace(t.En) == "" {
		return fmt.Errorf("%s.zh and %s.en are both required", field, field)
	}
	if ContainsEmoji(t.Zh) || ContainsEmoji(t.En) {
		return fmt.Errorf("%s must not contain emoji", field)
	}
	return nil
}

func validateSVGPathData(field, d string) error {
	trimmed := strings.TrimSpace(d)
	if trimmed == "" {
		return fmt.Errorf("%s contains empty SVG path", field)
	}
	first := trimmed[0]
	if first != 'M' && first != 'm' {
		return fmt.Errorf("%s SVG path must start with M or m", field)
	}
	isWS := func(b byte) bool {
		return b == ' ' || b == '\t' || b == '\n' || b == '\r'
	}
	skipWS := func(s string, pos int) int {
		for pos < len(s) && isWS(s[pos]) {
			pos++
		}
		return pos
	}
	// SVG 1.1 comma-wsp = (wsp+ comma? wsp*) | (comma wsp*)
	// Consumes optional whitespace and at most one comma between consecutive arguments/tuples.
	// Returns error if a comma is not followed by another argument (trailing or double comma).
	skipCommaWSP := func(s string, pos int) (int, bool, error) {
		p := skipWS(s, pos)
		sawComma := false
		if p < len(s) && s[p] == ',' {
			sawComma = true
			p++
			p = skipWS(s, p)
			if p >= len(s) || s[p] == ',' {
				return p, true, fmt.Errorf("%s SVG path invalid comma separator at byte %d", field, pos)
			}
		}
		return p, sawComma, nil
	}
	isCmd := func(b byte) bool {
		switch b {
		case 'M', 'm', 'L', 'l', 'H', 'h', 'V', 'v', 'C', 'c', 'S', 's', 'Q', 'q', 'T', 't', 'A', 'a', 'Z', 'z':
			return true
		}
		return false
	}
	argCount := func(cmd byte) int {
		switch cmd {
		case 'Z', 'z':
			return 0
		case 'H', 'h', 'V', 'v':
			return 1
		case 'M', 'm', 'L', 'l', 'T', 't':
			return 2
		case 'S', 's', 'Q', 'q':
			return 4
		case 'C', 'c':
			return 6
		case 'A', 'a':
			return 7
		}
		return -1
	}
	// Parse one SVG number token starting at pos; returns next index and parsed float.
	readNumber := func(s string, pos int) (int, float64, error) {
		n := len(s)
		start := pos
		if pos < n && (s[pos] == '+' || s[pos] == '-') {
			pos++
		}
		digits := 0
		for pos < n && s[pos] >= '0' && s[pos] <= '9' {
			pos++
			digits++
		}
		if pos < n && s[pos] == '.' {
			pos++
			for pos < n && s[pos] >= '0' && s[pos] <= '9' {
				pos++
				digits++
			}
		}
		if digits == 0 {
			return pos, 0, fmt.Errorf("%s SVG path expected number at byte %d", field, start)
		}
		if pos < n && (s[pos] == 'e' || s[pos] == 'E') {
			pos++
			if pos < n && (s[pos] == '+' || s[pos] == '-') {
				pos++
			}
			expDigits := 0
			for pos < n && s[pos] >= '0' && s[pos] <= '9' {
				pos++
				expDigits++
			}
			if expDigits == 0 {
				return pos, 0, fmt.Errorf("%s SVG path invalid exponent at byte %d", field, start)
			}
		}
		val, err := strconv.ParseFloat(s[start:pos], 64)
		if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
			return pos, 0, fmt.Errorf("%s SVG path non-finite number at byte %d", field, start)
		}
		return pos, val, nil
	}
	readFlag := func(s string, pos int) (int, error) {
		if pos >= len(s) || (s[pos] != '0' && s[pos] != '1') {
			return pos, fmt.Errorf("%s SVG path arc flag must be 0 or 1 at byte %d", field, pos)
		}
		return pos + 1, nil
	}

	pos, n := 0, len(trimmed)
	segments := 0
	var curCmd byte
	for pos < n {
		pos = skipWS(trimmed, pos)
		if pos >= n {
			break
		}
		if isCmd(trimmed[pos]) {
			curCmd = trimmed[pos]
			pos++
			if curCmd == 'Z' || curCmd == 'z' {
				segments++
				if segments > maxContribSVGPathCommands {
					return fmt.Errorf("%s SVG path exceeds %d segments", field, maxContribSVGPathCommands)
				}
				curCmd = 0
				continue
			}
			// Explicit command: whitespace only before first argument (comma forbidden right after command).
			pos = skipWS(trimmed, pos)
			if pos >= n || isCmd(trimmed[pos]) || trimmed[pos] == ',' {
				return fmt.Errorf("%s SVG path command %q missing or malformed parameters", field, curCmd)
			}
		} else {
			if curCmd == 0 {
				return fmt.Errorf("%s SVG path unexpected token %q at byte %d", field, trimmed[pos], pos)
			}
			// Implicit repeat tuple may be preceded by optional comma-wsp (if not followed by command/EOF).
			var sawComma bool
			var err error
			pos, sawComma, err = skipCommaWSP(trimmed, pos)
			if err != nil {
				return err
			}
			if sawComma && (pos >= n || isCmd(trimmed[pos])) {
				return fmt.Errorf("%s SVG path stray comma at byte %d", field, pos)
			}
			if curCmd == 'M' {
				curCmd = 'L'
			} else if curCmd == 'm' {
				curCmd = 'l'
			}
		}
		need := argCount(curCmd)
		if need < 0 {
			return fmt.Errorf("%s SVG path invalid command %q", field, curCmd)
		}
		for a := 0; a < need; a++ {
			if a > 0 {
				var err error
				pos, _, err = skipCommaWSP(trimmed, pos)
				if err != nil {
					return err
				}
			}
			if pos >= n {
				return fmt.Errorf("%s SVG path command %q truncated parameters", field, curCmd)
			}
			if (curCmd == 'A' || curCmd == 'a') && (a == 3 || a == 4) {
				var err error
				pos, err = readFlag(trimmed, pos)
				if err != nil {
					return err
				}
				continue
			}
			var err error
			pos, _, err = readNumber(trimmed, pos)
			if err != nil {
				return err
			}
		}
		segments++
		if segments > maxContribSVGPathCommands {
			return fmt.Errorf("%s SVG path exceeds %d segments", field, maxContribSVGPathCommands)
		}
		// Reject a trailing comma before EOF or before the next explicit command.
		nextWS := skipWS(trimmed, pos)
		if nextWS < n && trimmed[nextWS] == ',' {
			afterComma := skipWS(trimmed, nextWS+1)
			if afterComma >= n || isCmd(trimmed[afterComma]) || trimmed[afterComma] == ',' {
				return fmt.Errorf("%s SVG path trailing or duplicate comma at byte %d", field, nextWS)
			}
		}
	}
	if segments == 0 {
		return fmt.Errorf("%s SVG path contains no drawable segments", field)
	}
	return nil
}

func validateContribIcon(field string, ic DuoIconSpec) error {
	if ic.ViewBox != "0 0 24 24" || len(ic.StrokePaths) == 0 {
		return fmt.Errorf("%s needs view_box \"0 0 24 24\" and at least one stroke path", field)
	}
	paths, size := 0, 0
	for _, layer := range [][]string{ic.FillPaths, ic.StrokePaths, ic.AccentPaths} {
		paths += len(layer)
		for _, d := range layer {
			size += len(d)
			if err := validateSVGPathData(field, d); err != nil {
				return err
			}
		}
	}
	if paths > maxContribIconPaths || size > maxContribIconBytes {
		return fmt.Errorf("%s allows at most %d paths and %d bytes of path data", field, maxContribIconPaths, maxContribIconBytes)
	}
	return nil
}

// Contribution caps keep a single manifest from flooding host menus,
// the command palette or the icon cache.
const (
	maxContribIconPaths       = 16
	maxContribIconBytes       = 4096
	maxContribSVGPathCommands = 256
)

var contribLimits = []struct {
	kind string
	max  int
	n    func(Contributes) int
}{
	{"settings_sections", 8, func(c Contributes) int { return len(c.SettingsSections) }},
	{"commands", 64, func(c Contributes) int { return len(c.Commands) }},
	{"page_menu", 16, func(c Contributes) int { return len(c.PageMenu) }},
	{"editor_menu", 16, func(c Contributes) int { return len(c.EditorMenu) }},
	{"sidebar_menu", 16, func(c Contributes) int { return len(c.SidebarMenu) }},
	{"tab_menu", 16, func(c Contributes) int { return len(c.TabMenu) }},
	{"panels", 8, func(c Contributes) int { return len(c.Panels) }},
	{"sidebar_sections", 4, func(c Contributes) int { return len(c.SidebarSections) }},
	{"file_badges", 4, func(c Contributes) int { return len(c.FileBadges) }},
	{"backdrops", 8, func(c Contributes) int { return len(c.Backdrops) }},
	{"status_items", 4, func(c Contributes) int { return len(c.StatusItems) }},
	{"dialogs", 16, func(c Contributes) int { return len(c.Dialogs) }},
	{"languages", 16, func(c Contributes) int { return len(c.Languages) }},
}

func (m *Manifest) validateContributes() error {
	c := m.Contributes
	if c.empty() {
		return nil
	}
	need := func(kind string, n int, p Permission) error {
		if n > 0 && !m.HasPermission(p) {
			return fmt.Errorf("contributes.%s requires permission %q", kind, p)
		}
		return nil
	}
	for _, chk := range []error{
		need("titlebar_buttons", len(c.TitlebarButtons), PermUITitlebar),
		need("settings_sections", len(c.SettingsSections), PermUISettings),
		need("commands", len(c.Commands), PermUICommands),
		need("page_menu", len(c.PageMenu), PermUICommands),
		need("editor_menu", len(c.EditorMenu), PermUICommands),
		need("sidebar_menu", len(c.SidebarMenu), PermUISidebar),
		need("tab_menu", len(c.TabMenu), PermUICommands),
		need("panels", len(c.Panels), PermUIPanels),
		need("sidebar_sections", len(c.SidebarSections), PermUISidebar),
		need("file_badges", len(c.FileBadges), PermUISidebar),
		need("backdrops", len(c.Backdrops), PermUIAppearance),
		need("status_items", len(c.StatusItems), PermUIStatus),
		need("dialogs", len(c.Dialogs), PermUINotify),
	} {
		if chk != nil {
			return chk
		}
	}
	seen := map[string]bool{}
	id := func(kind, v string) error {
		if !contribIDRe.MatchString(v) {
			return fmt.Errorf("contributes.%s id %q must match %s", kind, v, contribIDRe.String())
		}
		key := kind + ":" + v
		if seen[key] {
			return fmt.Errorf("contributes.%s duplicates id %q", kind, v)
		}
		seen[key] = true
		return nil
	}
	for _, lim := range contribLimits {
		if lim.n(*c) > lim.max {
			return fmt.Errorf("contributes.%s allows at most %d entries", lim.kind, lim.max)
		}
	}
	if len(c.TitlebarButtons) > 3 {
		return errors.New("contributes.titlebar_buttons allows at most 3 buttons")
	}
	for _, b := range c.TitlebarButtons {
		if err := id("titlebar_buttons", b.ID); err != nil {
			return err
		}
		if err := validateContribIcon("contributes.titlebar_buttons.icon", b.Icon); err != nil {
			return err
		}
		if err := validateContribText("contributes.titlebar_buttons.tooltip", b.Tooltip); err != nil {
			return err
		}
		if (b.Command == "") == (b.Panel == "") {
			return fmt.Errorf("contributes.titlebar_buttons %q needs exactly one of command or panel", b.ID)
		}
		if b.Command != "" && !c.hasCommand(b.Command) {
			return fmt.Errorf("contributes.titlebar_buttons %q references unknown command %q", b.ID, b.Command)
		}
		if b.Panel != "" && !c.hasPanel(b.Panel) {
			return fmt.Errorf("contributes.titlebar_buttons %q references unknown panel %q", b.ID, b.Panel)
		}
	}
	for _, s := range c.SettingsSections {
		if err := id("settings_sections", s.ID); err != nil {
			return err
		}
		if !slices.Contains(SettingsSectionTabs, s.Tab) {
			return fmt.Errorf("contributes.settings_sections %q tab must be one of %v", s.ID, SettingsSectionTabs)
		}
		if err := validateContribText("contributes.settings_sections.title", s.Title); err != nil {
			return err
		}
		if s.Icon != nil {
			if err := validateContribIcon("contributes.settings_sections.icon", *s.Icon); err != nil {
				return err
			}
		}
	}
	for _, cmd := range c.Commands {
		if err := id("commands", cmd.ID); err != nil {
			return err
		}
		if err := validateContribText("contributes.commands.title", cmd.Title); err != nil {
			return err
		}
		if cmd.Icon != nil {
			if err := validateContribIcon("contributes.commands.icon", *cmd.Icon); err != nil {
				return err
			}
		}
	}
	for kind, items := range map[string][]MenuContrib{
		"page_menu":    c.PageMenu,
		"editor_menu":  c.EditorMenu,
		"sidebar_menu": c.SidebarMenu,
		"tab_menu":     c.TabMenu,
	} {
		for _, it := range items {
			if err := id(kind, it.ID); err != nil {
				return err
			}
			if err := validateContribText("contributes."+kind+".title", it.Title); err != nil {
				return err
			}
			if !c.hasCommand(it.Command) {
				return fmt.Errorf("contributes.%s %q references unknown command %q", kind, it.ID, it.Command)
			}
			if it.Icon != nil {
				if err := validateContribIcon("contributes."+kind+".icon", *it.Icon); err != nil {
					return err
				}
			}
			if len(it.Targets) > 0 && kind != "sidebar_menu" {
				return fmt.Errorf("contributes.%s %q: targets is only allowed in sidebar_menu", kind, it.ID)
			}
			seenTarget := map[string]bool{}
			for _, t := range it.Targets {
				if t != "note" && t != "folder" {
					return fmt.Errorf("contributes.%s %q: invalid target %q (want \"note\" or \"folder\")", kind, it.ID, t)
				}
				if seenTarget[t] {
					return fmt.Errorf("contributes.%s %q: duplicate target %q", kind, it.ID, t)
				}
				seenTarget[t] = true
			}
		}
	}
	for _, p := range c.Panels {
		if err := id("panels", p.ID); err != nil {
			return err
		}
		if err := validateContribText("contributes.panels.title", p.Title); err != nil {
			return err
		}
		if err := validateContribIcon("contributes.panels.icon", p.Icon); err != nil {
			return err
		}
	}
	for _, s := range c.SidebarSections {
		if err := id("sidebar_sections", s.ID); err != nil {
			return err
		}
		if err := validateContribText("contributes.sidebar_sections.title", s.Title); err != nil {
			return err
		}
		if err := validateContribIcon("contributes.sidebar_sections.icon", s.Icon); err != nil {
			return err
		}
	}
	for _, fb := range c.FileBadges {
		if err := id("file_badges", fb.ID); err != nil {
			return err
		}
		if err := validateContribIcon("contributes.file_badges.icon", fb.Icon); err != nil {
			return err
		}
		if strings.TrimSpace(fb.Tooltip.Zh) != "" || strings.TrimSpace(fb.Tooltip.En) != "" {
			if err := validateContribText("contributes.file_badges.tooltip", fb.Tooltip); err != nil {
				return err
			}
		}
	}
	for _, bg := range c.Backdrops {
		if err := id("backdrops", bg.ID); err != nil {
			return err
		}
		if err := validateContribText("contributes.backdrops.title", bg.Title); err != nil {
			return err
		}
		if err := validateContribIcon("contributes.backdrops.icon", bg.Icon); err != nil {
			return err
		}
		if bg.Pattern != "" && !validBackdropPattern(bg.Pattern) {
			return fmt.Errorf("contributes.backdrops %q pattern %q must be scatter, grid, watermark, or corners", bg.ID, bg.Pattern)
		}
		if bg.Density != "" && !validBackdropDensity(bg.Density) {
			return fmt.Errorf("contributes.backdrops %q density %q must be sparse, medium, or dense", bg.ID, bg.Density)
		}
		if bg.Tint != "" && !validBackdropTint(bg.Tint) {
			return fmt.Errorf("contributes.backdrops %q tint %q must be accent, muted, warm, or glow", bg.ID, bg.Tint)
		}
		if bg.Opacity != 0 && (math.IsNaN(bg.Opacity) || math.IsInf(bg.Opacity, 0) || bg.Opacity < 0.01 || bg.Opacity > 0.35) {
			return fmt.Errorf("contributes.backdrops %q opacity must be between 0.01 and 0.35", bg.ID)
		}
	}
	for _, st := range c.StatusItems {
		if err := id("status_items", st.ID); err != nil {
			return err
		}
		if strings.TrimSpace(st.Text.Zh) != "" || strings.TrimSpace(st.Text.En) != "" {
			if err := validateContribText("contributes.status_items.text", st.Text); err != nil {
				return err
			}
		}
		if st.Command != "" && !c.hasCommand(st.Command) {
			return fmt.Errorf("contributes.status_items %q references unknown command %q", st.ID, st.Command)
		}
		if st.Icon != nil {
			if err := validateContribIcon("contributes.status_items.icon", *st.Icon); err != nil {
				return err
			}
		}
	}
	for _, d := range c.Dialogs {
		if err := id("dialogs", d.ID); err != nil {
			return err
		}
		if err := validateContribText("contributes.dialogs.title", d.Title); err != nil {
			return err
		}
		if d.Width != 0 && (d.Width < 320 || d.Width > 720) {
			return fmt.Errorf("contributes.dialogs %q width must be 320-720", d.ID)
		}
		if d.Icon != nil {
			if err := validateContribIcon("contributes.dialogs.icon", *d.Icon); err != nil {
				return err
			}
		}
	}
	for _, l := range c.Languages {
		if err := id("languages", l.ID); err != nil {
			return err
		}
		if err := validateLanguageContrib(l); err != nil {
			return err
		}
	}
	return nil
}

func validBackdropPattern(p string) bool {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "scatter", "grid", "watermark", "corners":
		return true
	}
	return false
}

func validBackdropDensity(d string) bool {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "sparse", "medium", "dense":
		return true
	}
	return false
}

func validBackdropTint(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "accent", "muted", "warm", "glow":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Titlebar slots

// Built-in titlebar slot ids. TitlebarSlotMore is the page "…" button and can
// never be hidden.
const (
	TitlebarSlotFeedback = "feedback"
	TitlebarSlotAI       = "ai"
	TitlebarSlotSendAll  = "send_all"
	TitlebarSlotWrap     = "wrap"
	TitlebarSlotZoom     = "zoom"
	TitlebarSlotReveal   = "reveal"
	TitlebarSlotMore     = "more"
)

// TitlebarSlot describes one titlebar item for host.titlebar.get_items.
type TitlebarSlot struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Locked bool    `json:"locked"`
	Hidden bool    `json:"hidden"`
	Plugin string  `json:"plugin,omitempty"`
	Icon   *UIIcon `json:"icon,omitempty"`
}

// PluginTitlebarSlotID is the slot id for a plugin button: "plugin:<pluginID>/<buttonID>".
func PluginTitlebarSlotID(pluginID, buttonID string) string {
	return "plugin:" + pluginID + "/" + buttonID
}

func builtinTitlebarSlots(en bool) []TitlebarSlot {
	tr := func(zh, e string) string {
		if en {
			return e
		}
		return zh
	}
	return []TitlebarSlot{
		{ID: TitlebarSlotFeedback, Label: tr("反馈", "Feedback"), Icon: &UIIcon{Name: "message-square"}},
		{ID: TitlebarSlotAI, Label: tr("AI 助手", "AI Assistant"), Icon: &UIIcon{Name: "sparkles"}},
		{ID: TitlebarSlotSendAll, Label: tr("统一发送批注", "Send All Annotations"), Icon: &UIIcon{Chip: tr("统一发送", "Send all"), Dot: true}},
		{ID: TitlebarSlotWrap, Label: tr("自动换行", "Word Wrap"), Icon: &UIIcon{Name: "wrap"}},
		{ID: TitlebarSlotZoom, Label: tr("缩放", "Zoom"), Icon: &UIIcon{Name: "tb-plus"}},
		{ID: TitlebarSlotReveal, Label: tr("在文件夹中显示", "Reveal in Folder"), Icon: &UIIcon{Name: "folder-reveal"}},
		{ID: TitlebarSlotMore, Label: tr("页面操作", "Page Actions"), Locked: true, Icon: &UIIcon{Name: "more"}},
	}
}

// ---------------------------------------------------------------------------
// Host UI bridge

// EditorSelection is the editor state returned by host.editor.selection.
type EditorSelection struct {
	Note     string `json:"note"`
	Text     string `json:"text"`
	HasFocus bool   `json:"has_focus"`
}

// TarotState describes the host's native Tarot Mode state for host.ui.tarot_state
// and host.ui.set_tarot.
type TarotState struct {
	Supported  bool   `json:"supported"`
	Enabled    bool   `json:"enabled"`
	Room       string `json:"room"`
	Collected  int    `json:"collected"`
	Total      int    `json:"total"`
	DailyDrawn bool   `json:"daily_drawn"`
	DailyNote  string `json:"daily_note"`
	ActiveNote string `json:"active_note"`
}

// EditorBackgroundState describes the host editor's decorative background
// controlled via host.ui.editor_background and host.ui.set_editor_background.
type EditorBackgroundState struct {
	Mode     string `json:"mode"`     // "off" | "dots" | "grid"
	Contrast string `json:"contrast"` // "standard" | "high"
}

func normalizeEditorBackgroundState(mode, contrast string) EditorBackgroundState {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "dots":
		mode = "dots"
	case "grid":
		mode = "grid"
	default:
		mode = "off"
	}
	switch strings.ToLower(strings.TrimSpace(contrast)) {
	case "high", "strong":
		contrast = "high"
	default:
		contrast = "standard"
	}
	return EditorBackgroundState{Mode: mode, Contrast: contrast}
}

// EditorBackdropState stores a plugin's active custom vector backdrop configuration
// controlled via host.ui.backdrop and host.ui.set_backdrop.
type EditorBackdropState struct {
	ID       string  `json:"id,omitempty"`
	Enabled  bool    `json:"enabled"`
	Pattern  string  `json:"pattern,omitempty"`
	Density  string  `json:"density,omitempty"`
	Opacity  float64 `json:"opacity,omitempty"`
	Tint     string  `json:"tint,omitempty"`
	DarkGlow bool    `json:"dark_glow,omitempty"`
}

// FileBadgeEntry stores one file-tree badge payload pushed via host.sidebar.set_badges.
type FileBadgeEntry struct {
	Text    string `json:"text,omitempty"`
	Tooltip string `json:"tooltip,omitempty"`
	Tone    string `json:"tone,omitempty"`
}

func normalizeBackdropState(base BackdropContrib, st EditorBackdropState) EditorBackdropState {
	out := st
	if out.ID == "" {
		out.ID = base.ID
	}
	pattern := strings.ToLower(strings.TrimSpace(out.Pattern))
	if !validBackdropPattern(pattern) {
		pattern = strings.ToLower(strings.TrimSpace(base.Pattern))
	}
	if !validBackdropPattern(pattern) {
		pattern = "scatter"
	}
	out.Pattern = pattern

	density := strings.ToLower(strings.TrimSpace(out.Density))
	if !validBackdropDensity(density) {
		density = strings.ToLower(strings.TrimSpace(base.Density))
	}
	if !validBackdropDensity(density) {
		density = "medium"
	}
	out.Density = density

	tint := strings.ToLower(strings.TrimSpace(out.Tint))
	if !validBackdropTint(tint) {
		tint = strings.ToLower(strings.TrimSpace(base.Tint))
	}
	if !validBackdropTint(tint) {
		tint = "accent"
	}
	out.Tint = tint

	op := out.Opacity
	if math.IsNaN(op) || math.IsInf(op, 0) || op <= 0 {
		op = base.Opacity
	}
	if math.IsNaN(op) || math.IsInf(op, 0) || op <= 0 {
		op = 0.09
	}
	if op < 0.03 {
		op = 0.03
	}
	if op > 0.25 {
		op = 0.25
	}
	out.Opacity = math.Round(op*1000) / 1000
	return out
}

func cleanNotebookKey(root string) string {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return ""
	}
	if abs, err := filepath.Abs(trimmed); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(trimmed)
}

func normalizeBadgeRelPath(raw string) (string, bool) {
	if raw == "" || strings.HasPrefix(raw, "/") || strings.HasSuffix(raw, "/") ||
		strings.ContainsAny(raw, "\\:\x00\r\n") {
		return "", false
	}
	for _, seg := range strings.Split(raw, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return raw, true
}

func normalizeBadgeTone(tone string) string {
	switch strings.ToLower(strings.TrimSpace(tone)) {
	case "accent", "success", "warning", "danger":
		return strings.ToLower(strings.TrimSpace(tone))
	case "warn":
		return "warning"
	case "error":
		return "danger"
	default:
		return ""
	}
}

const (
	maxFileBadgesPerScope = 512
	maxFileBadgesPerRow   = 2
)

// UIHost is implemented by the app so plugins can drive host UI. All methods
// may be called from plugin goroutines and must be safe off the UI thread.
type UIHost interface {
	Toast(pluginID, message, tone string, icon *DuoIconSpec)
	OpenDialog(pluginID, dialogID string, checkValid func() error) (string, error)
	ActiveDialogToken(pluginID, dialogID string) string
	CloseDialog(pluginID, dialogID string)
	OpenPanel(pluginID, panelID string)
	ClosePanel(pluginID, panelID string)
	TogglePanel(pluginID, panelID string) (bool, error)
	OpenNote(notebookRoot, relPath string, rootInfo os.FileInfo, content []byte, checkValid func() error) error
	EditorSelection() (EditorSelection, error)
	// EditorInsert inserts or replaces text in the active editor. mode is
	// "replace" (default, replaces active selection if non-empty) or "insert"
	// (collapses selection to end without overwriting selected text).
	// expectedRoot and expectedNote guard against cross-notebook or cross-note
	// races; checkValid re-verifies runtime & epoch liveness on the main thread.
	EditorInsert(pluginID, expectedRoot, expectedNote, mode, text string, checkValid func() error) error
	TarotState() (TarotState, error)
	SetTarotMode(enabled *bool, toggle bool, room string) (TarotState, error)
}

// ---------------------------------------------------------------------------
// Contribution registry (host-side state pushed by plugins)

type contribRegistry struct {
	mu         sync.RWMutex
	path       string
	hidden     map[string]bool                                            // legacy global titlebar slot id -> hidden
	hiddenBy   map[string]map[string]bool                                 // plugin id -> hidden titlebar slot ids
	editorBG   map[string]EditorBackgroundState                           // plugin id -> editor background
	backdropBy map[string]EditorBackdropState                             // plugin id -> custom vector backdrop
	fileBadges map[string]map[string]map[string]map[string]FileBadgeEntry // pluginID -> notebookRoot -> badgeID -> relPath -> entry
	warmedUp   map[string]string                                          // plugin id -> initialized version
	badges     map[string]string                                          // plugin slot id -> badge text
	active     map[string]bool                                            // plugin slot id -> active
	status     map[string]statusOverlay                                   // "<plugin>/<item>" -> text/icon
	refreshed  map[string]time.Time                                       // surface refresh requests "<plugin>|<surface>"
	// commandNote maps a plugin to the note its in-flight command targets.
	commandNote map[string]string
}

type statusOverlay struct {
	Text      string
	Tooltip   string
	Icon      *DuoIconSpec
	ClearIcon bool
}

type contribFile struct {
	TitlebarHidden    []string                                                   `json:"titlebar_hidden,omitempty"`
	TitlebarHiddenBy  map[string][]string                                        `json:"titlebar_hidden_by,omitempty"`
	EditorBackgrounds map[string]EditorBackgroundState                           `json:"editor_backgrounds,omitempty"`
	EditorBackdrops   map[string]EditorBackdropState                             `json:"editor_backdrops,omitempty"`
	FileBadges        map[string]map[string]map[string]map[string]FileBadgeEntry `json:"file_badges,omitempty"`
	WarmedUp          map[string]string                                          `json:"warmed_up,omitempty"`
}

func newContribRegistry(appDataDir string) *contribRegistry {
	r := &contribRegistry{
		hidden:     map[string]bool{},
		hiddenBy:   map[string]map[string]bool{},
		editorBG:   map[string]EditorBackgroundState{},
		backdropBy: map[string]EditorBackdropState{},
		fileBadges: map[string]map[string]map[string]map[string]FileBadgeEntry{},
		warmedUp:   map[string]string{},
		badges:     map[string]string{},
		active:     map[string]bool{},
		status:     map[string]statusOverlay{},
		refreshed:  map[string]time.Time{},

		commandNote: map[string]string{},
	}
	if appDataDir != "" {
		r.path = filepath.Join(appDataDir, "plugins", "contrib-state.json")
		if b, err := os.ReadFile(r.path); err == nil {
			var f contribFile
			if json.Unmarshal(b, &f) == nil {
				for _, id := range f.TitlebarHidden {
					if id != TitlebarSlotMore {
						r.hidden[id] = true
					}
				}
				for pluginID, slots := range f.TitlebarHiddenBy {
					if strings.TrimSpace(pluginID) == "" {
						continue
					}
					m := make(map[string]bool, len(slots))
					for _, id := range slots {
						if id != "" && id != TitlebarSlotMore {
							m[id] = true
						}
					}
					r.hiddenBy[pluginID] = m
				}
				for pluginID, bg := range f.EditorBackgrounds {
					if strings.TrimSpace(pluginID) != "" {
						r.editorBG[pluginID] = normalizeEditorBackgroundState(bg.Mode, bg.Contrast)
					}
				}
				for pluginID, bd := range f.EditorBackdrops {
					if strings.TrimSpace(pluginID) != "" {
						r.backdropBy[pluginID] = bd
					}
				}
				for pluginID, byRoot := range f.FileBadges {
					if strings.TrimSpace(pluginID) == "" || len(byRoot) == 0 {
						continue
					}
					nbMap := make(map[string]map[string]map[string]FileBadgeEntry, len(byRoot))
					for nbRoot, byBadge := range byRoot {
						ck := cleanNotebookKey(nbRoot)
						if ck == "" || len(byBadge) == 0 {
							continue
						}
						badgeMap := make(map[string]map[string]FileBadgeEntry, len(byBadge))
						for badgeID, entries := range byBadge {
							if strings.TrimSpace(badgeID) == "" || len(entries) == 0 {
								continue
							}
							em := make(map[string]FileBadgeEntry, min(len(entries), maxFileBadgesPerScope))
							for rel, ent := range entries {
								if len(em) >= maxFileBadgesPerScope {
									break
								}
								if norm, ok := normalizeBadgeRelPath(rel); ok {
									em[norm] = FileBadgeEntry{
										Text:    truncateRunes(strings.TrimSpace(ent.Text), 4),
										Tooltip: truncateRunes(strings.TrimSpace(ent.Tooltip), 120),
										Tone:    normalizeBadgeTone(ent.Tone),
									}
								}
							}
							if len(em) > 0 {
								badgeMap[badgeID] = em
							}
						}
						if len(badgeMap) > 0 {
							nbMap[ck] = badgeMap
						}
					}
					if len(nbMap) > 0 {
						r.fileBadges[pluginID] = nbMap
					}
				}
				for pluginID, ver := range f.WarmedUp {
					if strings.TrimSpace(pluginID) != "" && strings.TrimSpace(ver) != "" {
						r.warmedUp[pluginID] = strings.TrimSpace(ver)
					}
				}
			}
		}
	}
	return r
}

func (r *contribRegistry) saveLocked() {
	if r.path == "" {
		return
	}
	var ids []string
	if len(r.hiddenBy) == 0 && len(r.hidden) > 0 {
		ids = make([]string, 0, len(r.hidden))
		for id, h := range r.hidden {
			if h {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
	}
	var hiddenBy map[string][]string
	if len(r.hiddenBy) > 0 {
		hiddenBy = make(map[string][]string, len(r.hiddenBy))
		for pluginID, slots := range r.hiddenBy {
			list := make([]string, 0, len(slots))
			for id, h := range slots {
				if h && id != TitlebarSlotMore {
					list = append(list, id)
				}
			}
			slices.Sort(list)
			hiddenBy[pluginID] = list
		}
	}
	var bgs map[string]EditorBackgroundState
	if len(r.editorBG) > 0 {
		bgs = make(map[string]EditorBackgroundState, len(r.editorBG))
		for pluginID, bg := range r.editorBG {
			bgs[pluginID] = normalizeEditorBackgroundState(bg.Mode, bg.Contrast)
		}
	}
	var backdrops map[string]EditorBackdropState
	if len(r.backdropBy) > 0 {
		backdrops = make(map[string]EditorBackdropState, len(r.backdropBy))
		for pluginID, bd := range r.backdropBy {
			backdrops[pluginID] = bd
		}
	}
	var fileBadges map[string]map[string]map[string]map[string]FileBadgeEntry
	if len(r.fileBadges) > 0 {
		fileBadges = make(map[string]map[string]map[string]map[string]FileBadgeEntry, len(r.fileBadges))
		for pid, byRoot := range r.fileBadges {
			nbCopy := make(map[string]map[string]map[string]FileBadgeEntry, len(byRoot))
			for nb, byBadge := range byRoot {
				badgeCopy := make(map[string]map[string]FileBadgeEntry, len(byBadge))
				for bid, entries := range byBadge {
					if len(entries) == 0 {
						continue
					}
					entCopy := make(map[string]FileBadgeEntry, len(entries))
					for rel, ent := range entries {
						entCopy[rel] = ent
					}
					badgeCopy[bid] = entCopy
				}
				if len(badgeCopy) > 0 {
					nbCopy[nb] = badgeCopy
				}
			}
			if len(nbCopy) > 0 {
				fileBadges[pid] = nbCopy
			}
		}
		if len(fileBadges) == 0 {
			fileBadges = nil
		}
	}
	var warmed map[string]string
	if len(r.warmedUp) > 0 {
		warmed = make(map[string]string, len(r.warmedUp))
		for pluginID, ver := range r.warmedUp {
			warmed[pluginID] = ver
		}
	}
	b, _ := json.MarshalIndent(contribFile{
		TitlebarHidden:    ids,
		TitlebarHiddenBy:  hiddenBy,
		EditorBackgrounds: bgs,
		EditorBackdrops:   backdrops,
		FileBadges:        fileBadges,
		WarmedUp:          warmed,
	}, "", "  ")
	_ = os.MkdirAll(filepath.Dir(r.path), 0o755)
	tmp := r.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, r.path)
	}
}

func (r *contribRegistry) isWarmedUp(pluginID, version string) bool {
	if r == nil || strings.TrimSpace(pluginID) == "" || strings.TrimSpace(version) == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.warmedUp[pluginID] == strings.TrimSpace(version)
}

func (r *contribRegistry) markWarmedUp(pluginID, version string) {
	if r == nil || strings.TrimSpace(pluginID) == "" || strings.TrimSpace(version) == "" {
		return
	}
	version = strings.TrimSpace(version)
	r.mu.Lock()
	if r.warmedUp[pluginID] != version {
		r.warmedUp[pluginID] = version
		r.saveLocked()
	}
	r.mu.Unlock()
}

// forgetPlugin drops every overlay and hidden flag a plugin left behind, so a
// later reinstall with the same ID starts clean. clearLegacyTitlebar is true
// only when the uninstalled plugin itself owned legacy global titlebar hides
// and no other legacy titlebar-manager plugin remains installed.
func (r *contribRegistry) forgetPlugin(pluginID string, clearLegacyTitlebar bool) {
	slotPrefix := "plugin:" + pluginID + "/"
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for id := range r.hidden {
		if strings.HasPrefix(id, slotPrefix) {
			delete(r.hidden, id)
			changed = true
		}
	}
	if _, ok := r.hiddenBy[pluginID]; ok {
		delete(r.hiddenBy, pluginID)
		changed = true
	}
	for _, slots := range r.hiddenBy {
		for id := range slots {
			if strings.HasPrefix(id, slotPrefix) {
				delete(slots, id)
				changed = true
			}
		}
	}
	if clearLegacyTitlebar && len(r.hiddenBy) == 0 && len(r.hidden) > 0 {
		clear(r.hidden)
		changed = true
	}
	if _, ok := r.editorBG[pluginID]; ok {
		delete(r.editorBG, pluginID)
		changed = true
	}
	if _, ok := r.backdropBy[pluginID]; ok {
		delete(r.backdropBy, pluginID)
		changed = true
	}
	if _, ok := r.fileBadges[pluginID]; ok {
		delete(r.fileBadges, pluginID)
		changed = true
	}
	if _, ok := r.warmedUp[pluginID]; ok {
		delete(r.warmedUp, pluginID)
		changed = true
	}
	for id := range r.badges {
		if strings.HasPrefix(id, slotPrefix) {
			delete(r.badges, id)
		}
	}
	for id := range r.active {
		if strings.HasPrefix(id, slotPrefix) {
			delete(r.active, id)
		}
	}
	for id := range r.status {
		if strings.HasPrefix(id, pluginID+"/") {
			delete(r.status, id)
		}
	}
	for id := range r.refreshed {
		if strings.HasPrefix(id, pluginID+"|") {
			delete(r.refreshed, id)
		}
	}
	delete(r.commandNote, pluginID)
	if changed {
		r.saveLocked()
	}
}

// clearTransientOverrides drops in-memory per-notebook overrides (titlebar
// badges/active highlights and status bar overrides) when switching notebooks,
// preventing stale values from the previous notebook from bleeding over.
func (r *contribRegistry) clearTransientOverrides() {
	if r == nil {
		return
	}
	r.mu.Lock()
	clear(r.badges)
	clear(r.active)
	clear(r.status)
	r.mu.Unlock()
}

// setCommandNote records the note a running command was invoked on, so
// host.editor.insert during that command cannot land in another note.
func (r *contribRegistry) setCommandNote(pluginID, note string) {
	r.mu.Lock()
	if note == "" {
		delete(r.commandNote, pluginID)
	} else {
		r.commandNote[pluginID] = note
	}
	r.mu.Unlock()
}

func (r *contribRegistry) runningCommandNote(pluginID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.commandNote[pluginID]
}

// ---------------------------------------------------------------------------
// Resolved contributions for UI code

type ResolvedTitlebarButton struct {
	PluginID string
	ID       string
	SlotID   string
	Icon     DuoIconSpec
	Tooltip  string
	Command  string
	Panel    string
	Badge    string
	Active   bool
}

type ResolvedSection struct {
	PluginID string
	ID       string
	Title    string
	Icon     DuoIconSpec
	Surface  Surface
}

type ResolvedCommand struct {
	PluginID   string
	PluginName string
	ID         string
	Title      string
	Shortcut   string
	Icon       DuoIconSpec
}

type ResolvedMenuItem struct {
	PluginID string
	ID       string
	Title    string
	Command  string
	Icon     DuoIconSpec
	OnNote   bool // sidebar_menu: shown on note rows
	OnFolder bool // sidebar_menu: shown on folder rows
}

type ResolvedPanel struct {
	PluginID string
	ID       string
	Title    string
	Icon     DuoIconSpec
	Surface  Surface
}

type ResolvedSidebarSection struct {
	PluginID         string
	ID               string
	Title            string
	Icon             DuoIconSpec
	Collapsible      bool
	DefaultCollapsed bool
	Surface          Surface
}

type ResolvedFileBadge struct {
	PluginID string
	BadgeID  string
	Icon     DuoIconSpec
	Text     string
	Tooltip  string
	Tone     string
}

type ResolvedBackdrop struct {
	PluginID   string
	BackdropID string
	Title      string
	Icon       DuoIconSpec
	Pattern    string
	Density    string
	Opacity    float64
	Tint       string
	DarkGlow   bool
}

type ResolvedStatusItem struct {
	PluginID string
	ID       string
	Text     string
	Tooltip  string
	Command  string
	Icon     *DuoIconSpec
}

type ResolvedDialog struct {
	PluginID string
	ID       string
	Title    string
	Width    int
	Icon     DuoIconSpec
	Surface  Surface
}

func resolveContribIcon(override *DuoIconSpec, fallback DuoIconSpec) DuoIconSpec {
	if override != nil && override.ViewBox == "0 0 24 24" && len(override.StrokePaths) > 0 {
		return *override
	}
	return fallback
}

func (s *Supervisor) enabledContribs() []InstalledApp {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]InstalledApp, 0, len(s.installed))
	for _, a := range s.installed {
		if a.Enabled && !a.Manifest.Contributes.empty() {
			out = append(out, a)
		}
	}
	return out
}

func isLegacyTitlebarManager(a InstalledApp) bool {
	return a.Manifest.HasPermission(PermUITitlebar) &&
		(a.Manifest.Contributes == nil || len(a.Manifest.Contributes.TitlebarButtons) == 0)
}

func (s *Supervisor) isRuntimeActiveLocked(pluginID string, rt *Runtime) bool {
	if s.stopping || rt == nil || rt.closed.Load() || s.runtimes[pluginID] != rt {
		return false
	}
	for _, a := range s.installed {
		if a.Manifest.ID == pluginID {
			return a.Enabled
		}
	}
	return false
}

// TitlebarHidden reports whether a titlebar slot is hidden by an enabled
// titlebar-controlling plugin. "more" is never hidden, and disabling or
// uninstalling all titlebar plugins immediately restores built-in slots.
func (s *Supervisor) TitlebarHidden(slotID string) bool {
	if slotID == TitlebarSlotMore || s == nil || s.contrib == nil {
		return false
	}
	s.mu.RLock()
	enabledTitlebarIDs := make([]string, 0, len(s.installed))
	hasLegacyManager := false
	for _, a := range s.installed {
		if a.Enabled && a.Manifest.HasPermission(PermUITitlebar) {
			enabledTitlebarIDs = append(enabledTitlebarIDs, a.Manifest.ID)
			if isLegacyTitlebarManager(a) {
				hasLegacyManager = true
			}
		}
	}
	s.mu.RUnlock()
	if len(enabledTitlebarIDs) == 0 {
		return false
	}
	s.contrib.mu.RLock()
	defer s.contrib.mu.RUnlock()
	if len(s.contrib.hiddenBy) > 0 {
		for _, pid := range enabledTitlebarIDs {
			if s.contrib.hiddenBy[pid][slotID] {
				return true
			}
		}
		return false
	}
	if !hasLegacyManager {
		return false
	}
	return s.contrib.hidden[slotID]
}

// EditorBackground returns the active decorative editor background set by the
// first enabled plugin with a non-"off" mode. Disabling or uninstalling that
// plugin immediately restores "off".
func (s *Supervisor) EditorBackground() EditorBackgroundState {
	if s == nil || s.contrib == nil {
		return EditorBackgroundState{Mode: "off", Contrast: "standard"}
	}
	s.mu.RLock()
	enabledIDs := make([]string, 0, len(s.installed))
	for _, a := range s.installed {
		if a.Enabled {
			enabledIDs = append(enabledIDs, a.Manifest.ID)
		}
	}
	s.mu.RUnlock()

	s.contrib.mu.RLock()
	defer s.contrib.mu.RUnlock()
	for _, id := range enabledIDs {
		if st, ok := s.contrib.editorBG[id]; ok && st.Mode != "" && st.Mode != "off" {
			return normalizeEditorBackgroundState(st.Mode, st.Contrast)
		}
	}
	return EditorBackgroundState{Mode: "off", Contrast: "standard"}
}

// TitlebarSlots lists built-in and plugin titlebar slots with hidden state.
func (s *Supervisor) TitlebarSlots() []TitlebarSlot {
	loc := s.Locale()
	en := strings.HasPrefix(strings.ToLower(loc), "en")
	slots := builtinTitlebarSlots(en)
	for _, a := range s.enabledContribs() {
		for _, b := range a.Manifest.Contributes.TitlebarButtons {
			slots = append(slots, TitlebarSlot{
				ID:     PluginTitlebarSlotID(a.Manifest.ID, b.ID),
				Label:  b.Tooltip.Resolve(loc),
				Plugin: a.Manifest.ID,
				Icon:   IconFromSpec(b.Icon),
			})
		}
	}
	for i := range slots {
		slots[i].Hidden = s.TitlebarHidden(slots[i].ID)
	}
	return slots
}

// TitlebarButtons returns visible plugin titlebar buttons in install order.
func (s *Supervisor) TitlebarButtons() []ResolvedTitlebarButton {
	loc := s.Locale()
	var out []ResolvedTitlebarButton
	for _, a := range s.enabledContribs() {
		for _, b := range a.Manifest.Contributes.TitlebarButtons {
			slot := PluginTitlebarSlotID(a.Manifest.ID, b.ID)
			if s.TitlebarHidden(slot) {
				continue
			}
			rb := ResolvedTitlebarButton{PluginID: a.Manifest.ID, ID: b.ID, SlotID: slot, Icon: b.Icon,
				Tooltip: b.Tooltip.Resolve(loc), Command: b.Command, Panel: b.Panel}
			s.contrib.mu.RLock()
			rb.Badge = s.contrib.badges[slot]
			rb.Active = s.contrib.active[slot]
			s.contrib.mu.RUnlock()
			out = append(out, rb)
		}
	}
	return out
}

// ActiveBackdrop returns the active custom vector editor backdrop set by the
// first enabled plugin with ui:appearance permission and an enabled backdrop.
// Disabling or uninstalling that plugin immediately clears the backdrop.
func (s *Supervisor) ActiveBackdrop() (ResolvedBackdrop, bool) {
	if s == nil || s.contrib == nil {
		return ResolvedBackdrop{}, false
	}
	loc := s.Locale()
	contribs := s.enabledContribs()
	s.contrib.mu.RLock()
	defer s.contrib.mu.RUnlock()
	for _, a := range contribs {
		if !a.Manifest.HasPermission(PermUIAppearance) || len(a.Manifest.Contributes.Backdrops) == 0 {
			continue
		}
		st, ok := s.contrib.backdropBy[a.Manifest.ID]
		if !ok || !st.Enabled {
			continue
		}
		base, found := a.Manifest.Contributes.backdropByID(st.ID)
		if !found {
			base = a.Manifest.Contributes.Backdrops[0]
		}
		norm := normalizeBackdropState(base, st)
		return ResolvedBackdrop{
			PluginID:   a.Manifest.ID,
			BackdropID: base.ID,
			Title:      base.Title.Resolve(loc),
			Icon:       base.Icon,
			Pattern:    norm.Pattern,
			Density:    norm.Density,
			Opacity:    norm.Opacity,
			Tint:       norm.Tint,
			DarkGlow:   norm.DarkGlow,
		}, true
	}
	return ResolvedBackdrop{}, false
}

// SettingsSections returns plugin sections for a built-in settings tab.
func (s *Supervisor) SettingsSections(tab string) []ResolvedSection {
	loc := s.Locale()
	var out []ResolvedSection
	for _, a := range s.enabledContribs() {
		for _, sec := range a.Manifest.Contributes.SettingsSections {
			if sec.Tab == tab {
				out = append(out, ResolvedSection{
					PluginID: a.Manifest.ID,
					ID:       sec.ID,
					Title:    sec.Title.Resolve(loc),
					Icon:     resolveContribIcon(sec.Icon, a.Manifest.Icon),
					Surface:  SettingsSectionSurface(sec.ID),
				})
			}
		}
	}
	return out
}

// Commands returns every enabled plugin command for the command palette and shortcuts.
func (s *Supervisor) Commands() []ResolvedCommand {
	loc := s.Locale()
	var out []ResolvedCommand
	for _, a := range s.enabledContribs() {
		for _, cmd := range a.Manifest.Contributes.Commands {
			out = append(out, ResolvedCommand{
				PluginID:   a.Manifest.ID,
				PluginName: a.Manifest.Name.Resolve(loc),
				ID:         cmd.ID,
				Title:      cmd.Title.Resolve(loc),
				Shortcut:   cmd.Shortcut,
				Icon:       resolveContribIcon(cmd.Icon, a.Manifest.Icon),
			})
		}
	}
	return out
}

func (s *Supervisor) menuItems(pick func(*Contributes) []MenuContrib) []ResolvedMenuItem {
	loc := s.Locale()
	var out []ResolvedMenuItem
	for _, a := range s.enabledContribs() {
		for _, it := range pick(a.Manifest.Contributes) {
			out = append(out, ResolvedMenuItem{
				PluginID: a.Manifest.ID,
				ID:       it.ID,
				Title:    it.Title.Resolve(loc),
				Command:  it.Command,
				Icon:     resolveContribIcon(it.Icon, a.Manifest.Icon),
				OnNote:   it.showsOn(false),
				OnFolder: it.showsOn(true),
			})
		}
	}
	return out
}

// PageMenuItems returns plugin items for the page "…" menu.
func (s *Supervisor) PageMenuItems() []ResolvedMenuItem {
	return s.menuItems(func(c *Contributes) []MenuContrib { return c.PageMenu })
}

// EditorMenuItems returns plugin items for the editor context menu.
func (s *Supervisor) EditorMenuItems() []ResolvedMenuItem {
	return s.menuItems(func(c *Contributes) []MenuContrib { return c.EditorMenu })
}

// SidebarMenuItems returns plugin items for the left sidebar file-tree context menu.
func (s *Supervisor) SidebarMenuItems() []ResolvedMenuItem {
	return s.menuItems(func(c *Contributes) []MenuContrib { return c.SidebarMenu })
}

// TabMenuItems returns plugin items for the titlebar tab context menu.
func (s *Supervisor) TabMenuItems() []ResolvedMenuItem {
	return s.menuItems(func(c *Contributes) []MenuContrib { return c.TabMenu })
}

// SidebarSections returns enabled plugin sections anchored in the left sidebar.
func (s *Supervisor) SidebarSections() []ResolvedSidebarSection {
	loc := s.Locale()
	var out []ResolvedSidebarSection
	for _, a := range s.enabledContribs() {
		if !a.Manifest.HasPermission(PermUISidebar) {
			continue
		}
		for _, sec := range a.Manifest.Contributes.SidebarSections {
			out = append(out, ResolvedSidebarSection{
				PluginID:         a.Manifest.ID,
				ID:               sec.ID,
				Title:            sec.Title.Resolve(loc),
				Icon:             sec.Icon,
				Collapsible:      sec.Collapsible,
				DefaultCollapsed: sec.DefaultCollapsed,
				Surface:          SidebarSectionSurface(sec.ID),
			})
		}
	}
	return out
}

// FileBadgesForNotebook returns a snapshot map of notebook-relative slash paths
// to resolved file-tree badges contributed by enabled ui:sidebar plugins.
func (s *Supervisor) FileBadgesForNotebook(notebookRoot string) map[string][]ResolvedFileBadge {
	if s == nil || s.contrib == nil {
		return nil
	}
	nbKey := cleanNotebookKey(notebookRoot)
	if nbKey == "" {
		return nil
	}
	loc := s.Locale()
	contribs := s.enabledContribs()
	s.contrib.mu.RLock()
	defer s.contrib.mu.RUnlock()
	var out map[string][]ResolvedFileBadge
	for _, a := range contribs {
		if !a.Manifest.HasPermission(PermUISidebar) || len(a.Manifest.Contributes.FileBadges) == 0 {
			continue
		}
		byRoot := s.contrib.fileBadges[a.Manifest.ID]
		if len(byRoot) == 0 {
			continue
		}
		byBadge := byRoot[nbKey]
		if len(byBadge) == 0 {
			continue
		}
		for _, spec := range a.Manifest.Contributes.FileBadges {
			entries := byBadge[spec.ID]
			if len(entries) == 0 {
				continue
			}
			defTip := spec.Tooltip.Resolve(loc)
			for rel, ent := range entries {
				if out == nil {
					out = make(map[string][]ResolvedFileBadge)
				}
				if len(out[rel]) >= maxFileBadgesPerRow {
					continue
				}
				tip := ent.Tooltip
				if tip == "" {
					tip = defTip
				}
				out[rel] = append(out[rel], ResolvedFileBadge{
					PluginID: a.Manifest.ID,
					BadgeID:  spec.ID,
					Icon:     spec.Icon,
					Text:     ent.Text,
					Tooltip:  tip,
					Tone:     ent.Tone,
				})
			}
		}
	}
	return out
}

// Panels returns every enabled plugin side panel.
func (s *Supervisor) Panels() []ResolvedPanel {
	loc := s.Locale()
	var out []ResolvedPanel
	for _, a := range s.enabledContribs() {
		for _, p := range a.Manifest.Contributes.Panels {
			out = append(out, ResolvedPanel{PluginID: a.Manifest.ID, ID: p.ID, Title: p.Title.Resolve(loc), Icon: p.Icon, Surface: PanelSurface(p.ID)})
		}
	}
	return out
}

// Panel resolves one plugin panel.
func (s *Supervisor) Panel(pluginID, panelID string) (ResolvedPanel, bool) {
	for _, p := range s.Panels() {
		if p.PluginID == pluginID && p.ID == panelID {
			return p, true
		}
	}
	return ResolvedPanel{}, false
}

// Dialog resolves one plugin dialog.
func (s *Supervisor) Dialog(pluginID, dialogID string) (ResolvedDialog, bool) {
	loc := s.Locale()
	inst, ok := s.GetInstalled(pluginID)
	if !ok || !inst.Enabled {
		return ResolvedDialog{}, false
	}
	for _, d := range inst.Manifest.Contributes.dialogs() {
		if d.ID == dialogID {
			w := d.Width
			if w == 0 {
				w = 460
			}
			return ResolvedDialog{
				PluginID: pluginID,
				ID:       d.ID,
				Title:    d.Title.Resolve(loc),
				Width:    w,
				Icon:     resolveContribIcon(d.Icon, inst.Manifest.Icon),
				Surface:  DialogSurface(d.ID),
			}, true
		}
	}
	return ResolvedDialog{}, false
}

func (c *Contributes) dialogs() []DialogContrib {
	if c == nil {
		return nil
	}
	return c.Dialogs
}

// StatusItems returns status strip items with plugin-pushed text and icon applied.
func (s *Supervisor) StatusItems() []ResolvedStatusItem {
	loc := s.Locale()
	var out []ResolvedStatusItem
	for _, a := range s.enabledContribs() {
		for _, st := range a.Manifest.Contributes.StatusItems {
			it := ResolvedStatusItem{
				PluginID: a.Manifest.ID,
				ID:       st.ID,
				Text:     st.Text.Resolve(loc),
				Command:  st.Command,
			}
			if st.Icon != nil {
				ic := *st.Icon
				it.Icon = &ic
			}
			s.contrib.mu.RLock()
			if ov, ok := s.contrib.status[a.Manifest.ID+"/"+st.ID]; ok {
				it.Text, it.Tooltip = ov.Text, ov.Tooltip
				if ov.ClearIcon {
					it.Icon = nil
				} else if ov.Icon != nil {
					ic := *ov.Icon
					it.Icon = &ic
				}
			}
			s.contrib.mu.RUnlock()
			if strings.TrimSpace(it.Text) != "" {
				out = append(out, it)
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Commands and events

// CommandContext is sent with plugin.command.
type CommandContext struct {
	Note      string `json:"note,omitempty"`
	Folder    string `json:"folder,omitempty"` // sidebar_menu on a folder row: notebook-relative folder path
	File      string `json:"file,omitempty"`   // absolute path of the target file or folder; only sent with notes:path
	Selection string `json:"selection,omitempty"`
	Source    string `json:"source"` // palette | shortcut | titlebar | page_menu | editor_menu | sidebar_menu | tab_menu | status
}

// CommandResult extends ActionResult with command-specific toast presentation fields.
type CommandResult struct {
	OK             *bool
	Tone           string
	Status         string
	ShowPluginIcon *bool
}

// Command invokes plugin.command on the runtime.
func (rt *Runtime) Command(ctx context.Context, commandID string, cc CommandContext) (*CommandResult, error) {
	if !rt.manifest.Contributes.hasCommand(commandID) {
		return nil, fmt.Errorf("command %q not declared in manifest", commandID)
	}
	var res struct {
		OK             *bool  `json:"ok,omitempty"`
		Tone           string `json:"tone,omitempty"`
		Status         string `json:"status,omitempty"`
		ShowPluginIcon *bool  `json:"show_plugin_icon,omitempty"`
	}
	if err := rt.call(ctx, "plugin.command", map[string]any{"command_id": commandID, "context": cc}, &res); err != nil {
		return nil, err
	}
	return &CommandResult{
		OK:             res.OK,
		Tone:           strings.TrimSpace(res.Tone),
		Status:         strings.TrimSpace(res.Status),
		ShowPluginIcon: res.ShowPluginIcon,
	}, nil
}

// RunCommandAsync starts the plugin (if needed) and runs one command. Errors
// and a non-empty status are shown as a toast.
func (s *Supervisor) RunCommandAsync(pluginID, commandID string, cc CommandContext) {
	// The note and selection belong to the notebook open right now; a command
	// still queued after a notebook switch must not run against another one.
	nbRoot := s.NotebookRoot()
	switch cc.Source {
	case "sidebar_menu":
		if cc.Note == "" && cc.Folder == "" {
			return
		}
	case "tab_menu":
		// Tab targets may live outside the notebook; note stays empty then.
		if cc.Note == "" && cc.File == "" {
			return
		}
	default:
		if cc.Note == "" {
			cc.Note = s.ActiveNoteRel()
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		unlock, err := s.lockPluginRPCContext(ctx, pluginID)
		if err != nil {
			s.toast(pluginID, err.Error(), "error", nil)
			return
		}
		defer unlock()
		if s.NotebookRoot() != nbRoot {
			s.toast(pluginID, LocalizedText{Zh: "笔记本已切换，命令未执行", En: "Notebook changed; command not run"}.Resolve(s.Locale()), "warning", nil)
			return
		}
		rt, err := s.ensureRuntime(ctx, pluginID, nbRoot)
		if err != nil {
			s.toast(pluginID, err.Error(), "error", nil)
			return
		}
		if !rt.manifest.HasPermission(PermEditorRead) {
			cc.Selection = ""
		}
		if !rt.manifest.HasPermission(PermNotesPath) {
			cc.File = ""
		}
		s.contrib.setCommandNote(pluginID, cc.Note)
		res, err := rt.Command(ctx, commandID, cc)
		s.contrib.setCommandNote(pluginID, "")
		if err != nil {
			s.toast(pluginID, err.Error(), "error", nil)
			return
		}
		if res != nil && res.Status != "" {
			tone := res.Tone
			if tone == "" {
				tone = "info"
			}
			var ic *DuoIconSpec
			if res.ShowPluginIcon == nil || *res.ShowPluginIcon {
				cp := rt.manifest.Icon
				ic = &cp
			}
			s.toast(pluginID, res.Status, tone, ic)
		}
		s.notifyRedraw()
	}()
}

// EmitWorkspaceEvent delivers note.opened / theme.changed / locale.changed to
// already-running plugins that hold events:workspace. It never starts a plugin.
func (s *Supervisor) EmitWorkspaceEvent(event string, data map[string]any) {
	s.mu.RLock()
	targets := make([]*Runtime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		if rt != nil && !rt.closed.Load() && rt.initialized.Load() && rt.manifest.HasPermission(PermEventsUI) {
			targets = append(targets, rt)
		}
	}
	s.mu.RUnlock()
	if len(targets) == 0 {
		return
	}
	params := map[string]any{"event": event, "timestamp": time.Now().UTC().Format(time.RFC3339)}
	for k, v := range data {
		params[k] = v
	}
	for _, rt := range targets {
		go func(rt *Runtime) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var res struct {
				Handled bool `json:"handled"`
			}
			_ = rt.call(ctx, "plugin.event", params, &res)
		}(rt)
	}
}

func (s *Supervisor) toast(pluginID, msg, tone string, icon *DuoIconSpec) {
	if s.cfg.UI != nil && strings.TrimSpace(msg) != "" {
		s.cfg.UI.Toast(pluginID, msg, tone, icon)
	}
}

// ---------------------------------------------------------------------------
// host.* methods for UI contributions

func rpcErr(code int, msg string) *rpcError { return &rpcError{Code: code, Message: msg} }

func missingPerm(p Permission) *rpcError {
	return rpcErr(-32001, "missing permission: "+string(p))
}

// dispatchContribHostMethod handles host.titlebar.*, host.status.*, host.ui.*,
// host.sidebar.* and host.editor.*. ok=false means the method is not a contrib method.
func (s *Supervisor) dispatchContribHostMethod(rt *Runtime, method string, params json.RawMessage) (res any, rerr *rpcError, ok bool) {
	if !strings.HasPrefix(method, "host.titlebar.") && !strings.HasPrefix(method, "host.status.") &&
		!strings.HasPrefix(method, "host.ui.") && !strings.HasPrefix(method, "host.editor.") &&
		!strings.HasPrefix(method, "host.sidebar.") {
		return nil, nil, false
	}
	m := rt.manifest
	// A runtime that is stopping, disabled, or replaced (disable/enable,
	// notebook switch) must not touch UI that now belongs to its successor.
	s.mu.RLock()
	current := s.isRuntimeActiveLocked(m.ID, rt)
	s.mu.RUnlock()
	if !current {
		return nil, rpcErr(-32603, "plugin runtime is no longer active"), true
	}
	decode := func(v any) *rpcError {
		if len(params) == 0 {
			return nil
		}
		if err := json.Unmarshal(params, v); err != nil {
			return rpcErr(-32602, "invalid params: "+err.Error())
		}
		return nil
	}
	ui := s.cfg.UI
	defer func() {
		if rerr == nil {
			s.notifyRedraw()
		}
	}()
	switch method {
	case "host.titlebar.get_items":
		if !m.HasPermission(PermUITitlebar) {
			return nil, missingPerm(PermUITitlebar), true
		}
		return map[string]any{"items": s.TitlebarSlots()}, nil, true

	case "host.titlebar.set_hidden":
		if !m.HasPermission(PermUITitlebar) {
			return nil, missingPerm(PermUITitlebar), true
		}
		var p struct {
			Hidden []string `json:"hidden"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		known := map[string]bool{}
		for _, sl := range s.TitlebarSlots() {
			known[sl.ID] = true
		}
		// Keep hidden preferences for installed-but-currently-disabled plugin
		// buttons so toggling another slot does not drop them before re-enable.
		s.mu.RLock()
		for _, a := range s.installed {
			if a.Manifest.Contributes == nil {
				continue
			}
			for _, b := range a.Manifest.Contributes.TitlebarButtons {
				known[PluginTitlebarSlotID(a.Manifest.ID, b.ID)] = true
			}
		}
		s.mu.RUnlock()
		next := map[string]bool{}
		for _, id := range p.Hidden {
			if id == TitlebarSlotMore || !known[id] {
				continue
			}
			next[id] = true
		}
		isLegacyMgr := m.HasPermission(PermUITitlebar) &&
			(m.Contributes == nil || len(m.Contributes.TitlebarButtons) == 0)
		s.mu.RLock()
		if !s.isRuntimeActiveLocked(m.ID, rt) {
			s.mu.RUnlock()
			return nil, rpcErr(-32603, "plugin runtime is no longer active"), true
		}
		s.contrib.mu.Lock()
		prev, existed := s.contrib.hiddenBy[m.ID]
		clearLegacy := isLegacyMgr && len(s.contrib.hidden) > 0
		if !existed || !maps.Equal(next, prev) || clearLegacy {
			s.contrib.hiddenBy[m.ID] = next
			if clearLegacy {
				clear(s.contrib.hidden)
			}
			s.contrib.saveLocked()
		}
		s.contrib.mu.Unlock()
		s.mu.RUnlock()
		return map[string]any{"ok": true, "items": s.TitlebarSlots()}, nil, true

	case "host.titlebar.set_button":
		if !m.HasPermission(PermUITitlebar) {
			return nil, missingPerm(PermUITitlebar), true
		}
		var p struct {
			ID     string  `json:"id"`
			Badge  *string `json:"badge"`
			Active *bool   `json:"active"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		if m.Contributes == nil || !slices.ContainsFunc(m.Contributes.TitlebarButtons, func(b TitlebarButtonContrib) bool { return b.ID == p.ID }) {
			return nil, rpcErr(-32602, "unknown titlebar button: "+p.ID), true
		}
		slot := PluginTitlebarSlotID(m.ID, p.ID)
		rtRoot := rt.notebookRoot()
		epBefore := s.notebookEpoch.Load()
		if rtRoot != "" && cleanNotebookKey(rtRoot) != cleanNotebookKey(s.NotebookRoot()) {
			return nil, rpcErr(-32603, "plugin runtime scope is no longer active"), true
		}
		s.mu.RLock()
		if !s.isRuntimeActiveLocked(m.ID, rt) || (rtRoot != "" && s.notebookEpoch.Load() != epBefore) {
			s.mu.RUnlock()
			return nil, rpcErr(-32603, "plugin runtime scope is no longer active"), true
		}
		s.contrib.mu.Lock()
		if p.Badge != nil {
			s.contrib.badges[slot] = truncateRunes(strings.TrimSpace(*p.Badge), 3)
		}
		if p.Active != nil {
			s.contrib.active[slot] = *p.Active
		}
		s.contrib.mu.Unlock()
		s.mu.RUnlock()
		return map[string]any{"ok": true}, nil, true

	case "host.status.set":
		if !m.HasPermission(PermUIStatus) {
			return nil, missingPerm(PermUIStatus), true
		}
		var p struct {
			ID             string       `json:"id"`
			Text           string       `json:"text"`
			Tooltip        string       `json:"tooltip"`
			Icon           *DuoIconSpec `json:"icon"`
			ShowPluginIcon bool         `json:"show_plugin_icon"`
			ClearIcon      bool         `json:"clear_icon"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		if m.Contributes == nil || !slices.ContainsFunc(m.Contributes.StatusItems, func(x StatusItemContrib) bool { return x.ID == p.ID }) {
			return nil, rpcErr(-32602, "unknown status item: "+p.ID), true
		}
		if ContainsEmoji(p.Text) {
			return nil, rpcErr(-32602, "status text must not contain emoji"), true
		}
		if p.Icon != nil {
			if err := validateContribIcon("host.status.set.icon", *p.Icon); err != nil {
				return nil, rpcErr(-32602, err.Error()), true
			}
		}
		key := m.ID + "/" + p.ID
		rtRoot := rt.notebookRoot()
		epBefore := s.notebookEpoch.Load()
		if rtRoot != "" && cleanNotebookKey(rtRoot) != cleanNotebookKey(s.NotebookRoot()) {
			return nil, rpcErr(-32603, "plugin runtime scope is no longer active"), true
		}
		s.mu.RLock()
		if !s.isRuntimeActiveLocked(m.ID, rt) || (rtRoot != "" && s.notebookEpoch.Load() != epBefore) {
			s.mu.RUnlock()
			return nil, rpcErr(-32603, "plugin runtime scope is no longer active"), true
		}
		s.contrib.mu.Lock()
		prev := s.contrib.status[key]
		ov := statusOverlay{
			Text:      truncateRunes(strings.TrimSpace(p.Text), 40),
			Tooltip:   truncateRunes(p.Tooltip, 200),
			Icon:      prev.Icon,
			ClearIcon: prev.ClearIcon,
		}
		switch {
		case p.ClearIcon:
			ov.Icon = nil
			ov.ClearIcon = true
		case p.Icon != nil:
			ic := *p.Icon
			ov.Icon = &ic
			ov.ClearIcon = false
		case p.ShowPluginIcon:
			ic := m.Icon
			ov.Icon = &ic
			ov.ClearIcon = false
		}
		s.contrib.status[key] = ov
		s.contrib.mu.Unlock()
		s.mu.RUnlock()
		return map[string]any{"ok": true}, nil, true

	case "host.ui.toast":
		if !m.HasPermission(PermUINotify) {
			return nil, missingPerm(PermUINotify), true
		}
		var p struct {
			Message        string       `json:"message"`
			Tone           string       `json:"tone"`
			Icon           *DuoIconSpec `json:"icon"`
			ShowPluginIcon bool         `json:"show_plugin_icon"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		if strings.TrimSpace(p.Message) == "" {
			return nil, rpcErr(-32602, "message is required"), true
		}
		var ic *DuoIconSpec
		if p.Icon != nil {
			if err := validateContribIcon("host.ui.toast.icon", *p.Icon); err != nil {
				return nil, rpcErr(-32602, err.Error()), true
			}
			cp := *p.Icon
			ic = &cp
		} else if p.ShowPluginIcon {
			cp := m.Icon
			ic = &cp
		}
		s.toast(m.ID, truncateRunes(p.Message, 160), normalizeTone(p.Tone), ic)
		return map[string]any{"ok": true}, nil, true

	case "host.ui.open_dialog", "host.ui.close_dialog":
		if !m.HasPermission(PermUINotify) {
			return nil, missingPerm(PermUINotify), true
		}
		var p struct {
			ID string `json:"id"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		if !m.Contributes.hasDialog(p.ID) {
			return nil, rpcErr(-32602, "unknown dialog: "+p.ID), true
		}
		if method == "host.ui.open_dialog" {
			if ui == nil {
				return nil, rpcErr(-32005, "dialog host unavailable"), true
			}
			expectedRoot := rt.notebookRoot()
			expectedEpoch := s.notebookEpoch.Load()
			checkValid := func() error {
				s.mu.RLock()
				stillActive := s.isRuntimeActiveLocked(m.ID, rt)
				s.mu.RUnlock()
				if !stillActive {
					return errors.New("plugin runtime is no longer active")
				}
				if s.notebookEpoch.Load() != expectedEpoch {
					return errors.New("notebook changed while opening dialog")
				}
				if expectedRoot != "" && cleanNotebookKey(expectedRoot) != cleanNotebookKey(s.NotebookRoot()) {
					return errors.New("notebook changed while opening dialog")
				}
				return nil
			}
			if err := checkValid(); err != nil {
				return nil, rpcErr(-32603, err.Error()), true
			}
			token, err := ui.OpenDialog(m.ID, p.ID, checkValid)
			if err != nil {
				return nil, rpcErr(-32004, err.Error()), true
			}
			return map[string]any{"ok": true, "dialog_token": token}, nil, true
		}
		if ui != nil {
			ui.CloseDialog(m.ID, p.ID)
		}
		return map[string]any{"ok": true}, nil, true

	case "host.ui.open_panel", "host.ui.close_panel", "host.ui.toggle_panel":
		if !m.HasPermission(PermUIPanels) {
			return nil, missingPerm(PermUIPanels), true
		}
		var p struct {
			ID string `json:"id"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		if !m.Contributes.hasPanel(p.ID) {
			return nil, rpcErr(-32602, "unknown panel: "+p.ID), true
		}
		if method == "host.ui.toggle_panel" {
			if ui == nil {
				return nil, rpcErr(-32005, "panel host unavailable"), true
			}
			open, err := ui.TogglePanel(m.ID, p.ID)
			if err != nil {
				return nil, rpcErr(-32004, err.Error()), true
			}
			return map[string]any{"ok": true, "open": open}, nil, true
		}
		if ui == nil {
			return nil, rpcErr(-32005, "panel host unavailable"), true
		}
		if method == "host.ui.open_panel" {
			ui.OpenPanel(m.ID, p.ID)
		} else {
			ui.ClosePanel(m.ID, p.ID)
		}
		return map[string]any{"ok": true}, nil, true

	case "host.ui.refresh":
		var p struct {
			Surface string `json:"surface"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		surf := Surface(p.Surface)
		if !m.HasSurface(surf) {
			return nil, rpcErr(-32602, "unknown surface: "+p.Surface), true
		}
		nbRoot := rt.notebookRoot()
		curRoot := s.NotebookRoot()
		hasNoteScope := m.HasPermission(PermNotesRead) || m.HasPermission(PermNotesWrite) || m.HasPermission(PermEventNoteSaved)
		if hasNoteScope {
			if cleanNotebookKey(nbRoot) != cleanNotebookKey(curRoot) {
				return map[string]any{"ok": true, "skipped": true}, nil, true
			}
		} else if nbRoot == "" {
			nbRoot = curRoot
		}
		// Only surfaces the user has opened in this notebook scope are re-rendered;
		// unopened ones load fresh when they are first shown.
		if s.surfaceViewOpenedForNotebook(m.ID, nbRoot, surf) {
			go s.reloadSurfaceViewForNotebookAsync(m.ID, nbRoot, surf)
		}
		return map[string]any{"ok": true}, nil, true

	case "host.ui.tarot_state":
		if !m.HasPermission(PermUISettings) && !m.HasPermission(PermUICommands) && !m.HasPermission(PermUITitlebar) {
			return nil, missingPerm(PermUISettings), true
		}
		if ui == nil {
			return TarotState{Supported: false, Room: "writing", Total: 22}, nil, true
		}
		st, err := ui.TarotState()
		if err != nil {
			return nil, rpcErr(-32005, err.Error()), true
		}
		return st, nil, true

	case "host.ui.set_tarot":
		if !m.HasPermission(PermUISettings) && !m.HasPermission(PermUICommands) && !m.HasPermission(PermUITitlebar) {
			return nil, missingPerm(PermUISettings), true
		}
		var p struct {
			Enabled *bool  `json:"enabled"`
			Toggle  bool   `json:"toggle"`
			Room    string `json:"room"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		if ui == nil {
			return nil, rpcErr(-32005, "tarot host unavailable"), true
		}
		st, err := ui.SetTarotMode(p.Enabled, p.Toggle, p.Room)
		if err != nil {
			return nil, rpcErr(-32004, err.Error()), true
		}
		return st, nil, true

	case "host.ui.editor_background":
		if !m.HasPermission(PermUISettings) && !m.HasPermission(PermUICommands) && !m.HasPermission(PermUITitlebar) {
			return nil, missingPerm(PermUISettings), true
		}
		s.contrib.mu.RLock()
		st, ok := s.contrib.editorBG[m.ID]
		s.contrib.mu.RUnlock()
		if !ok {
			st = EditorBackgroundState{Mode: "off", Contrast: "standard"}
		}
		return normalizeEditorBackgroundState(st.Mode, st.Contrast), nil, true

	case "host.ui.set_editor_background":
		if !m.HasPermission(PermUISettings) && !m.HasPermission(PermUICommands) && !m.HasPermission(PermUITitlebar) {
			return nil, missingPerm(PermUISettings), true
		}
		var p struct {
			Mode     string `json:"mode"`
			Contrast string `json:"contrast"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		st := normalizeEditorBackgroundState(p.Mode, p.Contrast)
		s.mu.RLock()
		if !s.isRuntimeActiveLocked(m.ID, rt) {
			s.mu.RUnlock()
			return nil, rpcErr(-32603, "plugin runtime is no longer active"), true
		}
		s.contrib.mu.Lock()
		if prev, ok := s.contrib.editorBG[m.ID]; !ok || prev != st {
			s.contrib.editorBG[m.ID] = st
			s.contrib.saveLocked()
		}
		s.contrib.mu.Unlock()
		s.mu.RUnlock()
		return st, nil, true

	case "host.ui.backdrop":
		if !m.HasPermission(PermUIAppearance) {
			return nil, missingPerm(PermUIAppearance), true
		}
		var base BackdropContrib
		if m.Contributes != nil && len(m.Contributes.Backdrops) > 0 {
			base = m.Contributes.Backdrops[0]
		}
		s.contrib.mu.RLock()
		st, ok := s.contrib.backdropBy[m.ID]
		s.contrib.mu.RUnlock()
		if !ok {
			if base.ID == "" {
				return EditorBackdropState{Enabled: false}, nil, true
			}
			st = EditorBackdropState{ID: base.ID, Enabled: false, DarkGlow: base.DarkGlow}
		} else if m.Contributes != nil {
			if b, found := m.Contributes.backdropByID(st.ID); found {
				base = b
			}
		}
		if base.ID == "" {
			return EditorBackdropState{Enabled: false}, nil, true
		}
		return normalizeBackdropState(base, st), nil, true

	case "host.ui.set_backdrop":
		if !m.HasPermission(PermUIAppearance) {
			return nil, missingPerm(PermUIAppearance), true
		}
		var p struct {
			ID       string   `json:"id"`
			Enabled  *bool    `json:"enabled"`
			Pattern  string   `json:"pattern"`
			Density  string   `json:"density"`
			Opacity  *float64 `json:"opacity"`
			Tint     string   `json:"tint"`
			DarkGlow *bool    `json:"dark_glow"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		if m.Contributes == nil || len(m.Contributes.Backdrops) == 0 {
			return nil, rpcErr(-32602, "manifest declares no backdrops"), true
		}
		enabled := true
		if p.Enabled != nil {
			enabled = *p.Enabled
		}
		var base BackdropContrib
		if strings.TrimSpace(p.ID) != "" {
			var found bool
			base, found = m.Contributes.backdropByID(strings.TrimSpace(p.ID))
			if !found {
				return nil, rpcErr(-32602, "unknown backdrop: "+p.ID), true
			}
		} else {
			base = m.Contributes.Backdrops[0]
		}
		if p.Pattern != "" && !validBackdropPattern(p.Pattern) {
			return nil, rpcErr(-32602, "invalid backdrop pattern: "+p.Pattern), true
		}
		if p.Density != "" && !validBackdropDensity(p.Density) {
			return nil, rpcErr(-32602, "invalid backdrop density: "+p.Density), true
		}
		if p.Tint != "" && !validBackdropTint(p.Tint) {
			return nil, rpcErr(-32602, "invalid backdrop tint: "+p.Tint), true
		}
		if p.Opacity != nil && (math.IsNaN(*p.Opacity) || math.IsInf(*p.Opacity, 0)) {
			return nil, rpcErr(-32602, "invalid backdrop opacity"), true
		}
		s.contrib.mu.RLock()
		prev, hasPrev := s.contrib.backdropBy[m.ID]
		s.contrib.mu.RUnlock()
		next := EditorBackdropState{
			ID:       base.ID,
			Enabled:  enabled,
			Pattern:  base.Pattern,
			Density:  base.Density,
			Opacity:  base.Opacity,
			Tint:     base.Tint,
			DarkGlow: base.DarkGlow,
		}
		if hasPrev && (prev.ID == "" || prev.ID == base.ID) {
			next.Pattern = prev.Pattern
			next.Density = prev.Density
			next.Opacity = prev.Opacity
			next.Tint = prev.Tint
			next.DarkGlow = prev.DarkGlow
		}
		if p.Pattern != "" {
			next.Pattern = p.Pattern
		}
		if p.Density != "" {
			next.Density = p.Density
		}
		if p.Tint != "" {
			next.Tint = p.Tint
		}
		if p.Opacity != nil {
			next.Opacity = *p.Opacity
		}
		if p.DarkGlow != nil {
			next.DarkGlow = *p.DarkGlow
		}
		next = normalizeBackdropState(base, next)
		s.mu.RLock()
		if !s.isRuntimeActiveLocked(m.ID, rt) {
			s.mu.RUnlock()
			return nil, rpcErr(-32603, "plugin runtime is no longer active"), true
		}
		s.contrib.mu.Lock()
		if prev, ok := s.contrib.backdropBy[m.ID]; !ok || prev != next {
			s.contrib.backdropBy[m.ID] = next
			s.contrib.saveLocked()
		}
		s.contrib.mu.Unlock()
		s.mu.RUnlock()
		return next, nil, true

	case "host.sidebar.get_badges":
		if !m.HasPermission(PermUISidebar) {
			return nil, missingPerm(PermUISidebar), true
		}
		var p struct {
			BadgeID      string `json:"badge_id"`
			NotebookRoot string `json:"notebook_root"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		nbRoot, err := rt.resolveNotebookRoot(p.NotebookRoot)
		if err != nil {
			return nil, rpcErr(-32602, err.Error()), true
		}
		nbKey := cleanNotebookKey(nbRoot)
		out := map[string]FileBadgeEntry{}
		if nbKey != "" {
			s.contrib.mu.RLock()
			if byBadge := s.contrib.fileBadges[m.ID][nbKey]; len(byBadge) > 0 {
				badgeID := strings.TrimSpace(p.BadgeID)
				if badgeID == "" && m.Contributes != nil && len(m.Contributes.FileBadges) == 1 {
					badgeID = m.Contributes.FileBadges[0].ID
				}
				for rel, ent := range byBadge[badgeID] {
					out[rel] = ent
				}
			}
			s.contrib.mu.RUnlock()
		}
		return map[string]any{"entries": out}, nil, true

	case "host.sidebar.set_badges":
		if !m.HasPermission(PermUISidebar) {
			return nil, missingPerm(PermUISidebar), true
		}
		var p struct {
			BadgeID      string                     `json:"badge_id"`
			NotebookRoot string                     `json:"notebook_root"`
			Clear        bool                       `json:"clear"`
			Patch        bool                       `json:"patch"`
			Entries      map[string]*FileBadgeEntry `json:"entries"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		badgeID := strings.TrimSpace(p.BadgeID)
		if badgeID == "" && m.Contributes != nil && len(m.Contributes.FileBadges) == 1 {
			badgeID = m.Contributes.FileBadges[0].ID
		}
		if !m.Contributes.hasFileBadge(badgeID) {
			return nil, rpcErr(-32602, "unknown file badge: "+badgeID), true
		}
		nbRoot, err := rt.resolveNotebookRoot(p.NotebookRoot)
		if err != nil {
			return nil, rpcErr(-32602, err.Error()), true
		}
		nbKey := cleanNotebookKey(nbRoot)
		if nbKey == "" {
			return nil, rpcErr(-32602, "no active notebook for file badges"), true
		}
		if len(p.Entries) > maxFileBadgesPerScope {
			return nil, rpcErr(-32602, fmt.Sprintf("entries exceeds limit (%d)", maxFileBadgesPerScope)), true
		}
		parsed := make(map[string]*FileBadgeEntry, len(p.Entries))
		for rawRel, ent := range p.Entries {
			rel, ok := normalizeBadgeRelPath(rawRel)
			if !ok {
				return nil, rpcErr(-32602, "invalid relative note path: "+rawRel), true
			}
			if ent == nil {
				parsed[rel] = nil
				continue
			}
			if ContainsEmoji(ent.Text) || ContainsEmoji(ent.Tooltip) {
				return nil, rpcErr(-32602, "file badge text and tooltip must not contain emoji"), true
			}
			clean := FileBadgeEntry{
				Text:    truncateRunes(strings.TrimSpace(ent.Text), 4),
				Tooltip: truncateRunes(strings.TrimSpace(ent.Tooltip), 120),
				Tone:    normalizeBadgeTone(ent.Tone),
			}
			parsed[rel] = &clean
		}
		s.mu.RLock()
		if !s.isRuntimeActiveLocked(m.ID, rt) {
			s.mu.RUnlock()
			return nil, rpcErr(-32603, "plugin runtime is no longer active"), true
		}
		s.contrib.mu.Lock()
		var next map[string]FileBadgeEntry
		if p.Clear {
			next = nil
		} else if p.Patch {
			next = make(map[string]FileBadgeEntry)
			if prev := s.contrib.fileBadges[m.ID][nbKey][badgeID]; len(prev) > 0 {
				for k, v := range prev {
					next[k] = v
				}
			}
			for rel, ent := range parsed {
				if ent == nil {
					delete(next, rel)
				} else {
					next[rel] = *ent
				}
			}
			if len(next) > maxFileBadgesPerScope {
				s.contrib.mu.Unlock()
				s.mu.RUnlock()
				return nil, rpcErr(-32602, fmt.Sprintf("entries exceeds limit (%d)", maxFileBadgesPerScope)), true
			}
		} else {
			next = make(map[string]FileBadgeEntry, len(parsed))
			for rel, ent := range parsed {
				if ent != nil {
					next[rel] = *ent
				}
			}
		}
		byRoot := s.contrib.fileBadges[m.ID]
		if len(next) == 0 {
			if byBadge := byRoot[nbKey]; byBadge != nil {
				delete(byBadge, badgeID)
				if len(byBadge) == 0 {
					delete(byRoot, nbKey)
				}
			}
			if len(byRoot) == 0 {
				delete(s.contrib.fileBadges, m.ID)
			}
		} else {
			if byRoot == nil {
				byRoot = map[string]map[string]map[string]FileBadgeEntry{}
				s.contrib.fileBadges[m.ID] = byRoot
			}
			byBadge := byRoot[nbKey]
			if byBadge == nil {
				byBadge = map[string]map[string]FileBadgeEntry{}
				byRoot[nbKey] = byBadge
			}
			byBadge[badgeID] = next
		}
		count := len(next)
		s.contrib.saveLocked()
		s.contrib.mu.Unlock()
		s.mu.RUnlock()
		return map[string]any{"ok": true, "count": count}, nil, true

	case "host.editor.selection":
		if !m.HasPermission(PermEditorRead) {
			return nil, missingPerm(PermEditorRead), true
		}
		if ui == nil {
			return nil, rpcErr(-32005, "editor unavailable"), true
		}
		sel, err := ui.EditorSelection()
		if err != nil {
			return nil, rpcErr(-32005, err.Error()), true
		}
		return sel, nil, true

	case "host.editor.insert":
		if !m.HasPermission(PermEditorEdit) {
			return nil, missingPerm(PermEditorEdit), true
		}
		var p struct {
			Text string `json:"text"`
			Note string `json:"note"`
			Mode string `json:"mode"` // "" | "replace" | "insert"
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		if ui == nil {
			return nil, rpcErr(-32005, "editor unavailable"), true
		}
		if len(p.Text) > maxPluginNoteWrite {
			return nil, rpcErr(-32602, "text too large"), true
		}
		mode := strings.ToLower(strings.TrimSpace(p.Mode))
		if mode == "" {
			mode = "replace"
		}
		if mode != "replace" && mode != "insert" {
			return nil, rpcErr(-32602, "invalid editor insert mode: "+p.Mode), true
		}
		expected := p.Note
		if expected == "" {
			expected = s.contrib.runningCommandNote(m.ID)
		}
		expectedRoot := rt.notebookRoot()
		expectedEpoch := s.notebookEpoch.Load()
		checkValid := func() error {
			s.mu.RLock()
			stillActive := s.isRuntimeActiveLocked(m.ID, rt)
			s.mu.RUnlock()
			if !stillActive {
				return errors.New("plugin runtime is no longer active")
			}
			if s.notebookEpoch.Load() != expectedEpoch {
				return errors.New("notebook changed during insert")
			}
			if expectedRoot != "" && cleanNotebookKey(expectedRoot) != cleanNotebookKey(s.NotebookRoot()) {
				return errors.New("notebook changed during insert")
			}
			return nil
		}
		if err := checkValid(); err != nil {
			return nil, rpcErr(-32603, err.Error()), true
		}
		if err := ui.EditorInsert(m.ID, expectedRoot, expected, mode, p.Text, checkValid); err != nil {
			if errors.Is(err, ErrNoteConflict) {
				return nil, rpcErr(-32006, err.Error()), true
			}
			return nil, rpcErr(-32004, err.Error()), true
		}
		return map[string]any{"ok": true}, nil, true
	}
	return nil, rpcErr(-32601, "unknown host method: "+method), true
}

const maxPluginNoteWrite = 4 * 1024 * 1024

func normalizeTone(t string) string {
	switch t {
	case "success", "error", "warning":
		return t
	}
	return "info"
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
