package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// UI contribution permissions. Each contribution kind in manifest.contributes
// requires its matching permission so the install sheet can show it.
const (
	PermUITitlebar Permission = "ui:titlebar" // titlebar buttons + titlebar layout control
	PermUISettings Permission = "ui:settings" // sections inside built-in settings tabs
	PermUICommands Permission = "ui:commands" // commands, shortcuts, page / editor menu items
	PermUIPanels   Permission = "ui:panels"   // side panels
	PermUIStatus   Permission = "ui:status"   // status bar items
	PermUINotify   Permission = "ui:notify"   // toasts and plugin dialogs
	PermEditorRead Permission = "editor:read"
	PermEditorEdit Permission = "editor:write"
	PermEventsUI   Permission = "events:workspace" // note.opened / theme.changed / locale.changed
)

func init() {
	for _, p := range []Permission{PermUITitlebar, PermUISettings, PermUICommands, PermUIPanels, PermUIStatus, PermUINotify, PermEditorRead, PermEditorEdit, PermEventsUI} {
		validPermissions[p] = true
	}
}

func contribPermissionLabel(p Permission, en bool) (string, bool) {
	labels := map[Permission][2]string{
		PermUITitlebar: {"顶栏按钮", "Titlebar"},
		PermUISettings: {"设置项", "Settings Items"},
		PermUICommands: {"命令与菜单", "Commands & Menus"},
		PermUIPanels:   {"侧边面板", "Side Panels"},
		PermUIStatus:   {"状态栏", "Status Bar"},
		PermUINotify:   {"提示与弹窗", "Notices & Dialogs"},
		PermEditorRead: {"读取选区", "Read Selection"},
		PermEditorEdit: {"编辑正文", "Edit Note Text"},
		PermEventsUI:   {"界面事件", "Workspace Events"},
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

// Prefixed surfaces rendered with plugin.render / plugin.action.
const (
	SurfacePrefixSettingsSection = "settings_section:"
	SurfacePrefixPanel           = "panel:"
	SurfacePrefixDialog          = "dialog:"
)

func SettingsSectionSurface(id string) Surface { return Surface(SurfacePrefixSettingsSection + id) }
func PanelSurface(id string) Surface           { return Surface(SurfacePrefixPanel + id) }
func DialogSurface(id string) Surface          { return Surface(SurfacePrefixDialog + id) }

var contribIDRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Built-in settings tabs that accept plugin sections.
var SettingsSectionTabs = []string{"ai", "appearance", "folders", "shortcuts"}

// Contributes declares where a plugin places UI in the host.
type Contributes struct {
	TitlebarButtons  []TitlebarButtonContrib  `json:"titlebar_buttons,omitempty"`
	SettingsSections []SettingsSectionContrib `json:"settings_sections,omitempty"`
	Commands         []CommandContrib         `json:"commands,omitempty"`
	PageMenu         []MenuContrib            `json:"page_menu,omitempty"`
	EditorMenu       []MenuContrib            `json:"editor_menu,omitempty"`
	Panels           []PanelContrib           `json:"panels,omitempty"`
	StatusItems      []StatusItemContrib      `json:"status_items,omitempty"`
	Dialogs          []DialogContrib          `json:"dialogs,omitempty"`
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
}

// CommandContrib appears in the command palette; Shortcut is an optional
// default binding in shortcuts.go spec form, e.g. "cmd-shift-t".
type CommandContrib struct {
	ID       string        `json:"id"`
	Title    LocalizedText `json:"title"`
	Shortcut string        `json:"shortcut,omitempty"`
}

// MenuContrib adds an item to the page "…" menu or the editor context menu.
type MenuContrib struct {
	ID      string        `json:"id"`
	Title   LocalizedText `json:"title"`
	Command string        `json:"command"`
}

// PanelContrib is a right side panel rendered from surface "panel:<id>".
type PanelContrib struct {
	ID    string        `json:"id"`
	Title LocalizedText `json:"title"`
	Icon  DuoIconSpec   `json:"icon"`
}

// StatusItemContrib is a short label in the status strip. Text is the initial
// value until the plugin calls host.status.set.
type StatusItemContrib struct {
	ID      string        `json:"id"`
	Text    LocalizedText `json:"text,omitempty"`
	Command string        `json:"command,omitempty"`
}

// DialogContrib is a modal rendered from surface "dialog:<id>", opened by
// host.ui.open_dialog.
type DialogContrib struct {
	ID    string        `json:"id"`
	Title LocalizedText `json:"title"`
	Width int           `json:"width,omitempty"`
}

func (c *Contributes) empty() bool {
	return c == nil || (len(c.TitlebarButtons) == 0 && len(c.SettingsSections) == 0 && len(c.Commands) == 0 &&
		len(c.PageMenu) == 0 && len(c.EditorMenu) == 0 && len(c.Panels) == 0 && len(c.StatusItems) == 0 && len(c.Dialogs) == 0)
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

func validateContribIcon(field string, ic DuoIconSpec) error {
	if ic.ViewBox != "0 0 24 24" || len(ic.StrokePaths) == 0 {
		return fmt.Errorf("%s needs view_box \"0 0 24 24\" and at least one stroke path", field)
	}
	paths, size := 0, 0
	for _, layer := range [][]string{ic.FillPaths, ic.StrokePaths, ic.AccentPaths} {
		paths += len(layer)
		for _, d := range layer {
			size += len(d)
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
	maxContribIconPaths = 16
	maxContribIconBytes = 4096
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
	{"panels", 8, func(c Contributes) int { return len(c.Panels) }},
	{"status_items", 4, func(c Contributes) int { return len(c.StatusItems) }},
	{"dialogs", 16, func(c Contributes) int { return len(c.Dialogs) }},
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
		need("panels", len(c.Panels), PermUIPanels),
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
	}
	for _, cmd := range c.Commands {
		if err := id("commands", cmd.ID); err != nil {
			return err
		}
		if err := validateContribText("contributes.commands.title", cmd.Title); err != nil {
			return err
		}
	}
	for kind, items := range map[string][]MenuContrib{"page_menu": c.PageMenu, "editor_menu": c.EditorMenu} {
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
	}
	return nil
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
	ID     string `json:"id"`
	Label  string `json:"label"`
	Locked bool   `json:"locked"`
	Hidden bool   `json:"hidden"`
	Plugin string `json:"plugin,omitempty"`
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
		{ID: TitlebarSlotFeedback, Label: tr("反馈", "Feedback")},
		{ID: TitlebarSlotAI, Label: tr("AI 助手", "AI Assistant")},
		{ID: TitlebarSlotSendAll, Label: tr("统一发送批注", "Send All Annotations")},
		{ID: TitlebarSlotWrap, Label: tr("自动换行", "Word Wrap")},
		{ID: TitlebarSlotZoom, Label: tr("缩放", "Zoom")},
		{ID: TitlebarSlotReveal, Label: tr("在文件夹中显示", "Reveal in Folder")},
		{ID: TitlebarSlotMore, Label: tr("页面操作", "Page Actions"), Locked: true},
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

// UIHost is implemented by the app so plugins can drive host UI. All methods
// may be called from plugin goroutines and must be safe off the UI thread.
type UIHost interface {
	Toast(pluginID, message, tone string)
	OpenDialog(pluginID, dialogID string)
	CloseDialog(pluginID, dialogID string)
	OpenPanel(pluginID, panelID string)
	ClosePanel(pluginID, panelID string)
	EditorSelection() (EditorSelection, error)
	// EditorInsert replaces the current selection (or inserts at the caret) with
	// text. When expectedNote is non-empty the insert fails unless that note
	// (notebook-relative path) is still the active one. pluginID identifies the
	// writer so its own note.saved echo can be suppressed.
	EditorInsert(pluginID, expectedNote, text string) error
	TarotState() (TarotState, error)
	SetTarotMode(enabled *bool, toggle bool, room string) (TarotState, error)
}

// ---------------------------------------------------------------------------
// Contribution registry (host-side state pushed by plugins)

type contribRegistry struct {
	mu        sync.RWMutex
	path      string
	hidden    map[string]bool          // titlebar slot id -> hidden
	badges    map[string]string        // plugin slot id -> badge text
	active    map[string]bool          // plugin slot id -> active
	status    map[string]statusOverlay // "<plugin>/<item>" -> text
	refreshed map[string]time.Time     // surface refresh requests "<plugin>|<surface>"
	// commandNote maps a plugin to the note its in-flight command targets.
	commandNote map[string]string
}

type statusOverlay struct {
	Text    string
	Tooltip string
}

type contribFile struct {
	TitlebarHidden []string `json:"titlebar_hidden"`
}

func newContribRegistry(appDataDir string) *contribRegistry {
	r := &contribRegistry{
		hidden:    map[string]bool{},
		badges:    map[string]string{},
		active:    map[string]bool{},
		status:    map[string]statusOverlay{},
		refreshed: map[string]time.Time{},

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
			}
		}
	}
	return r
}

func (r *contribRegistry) saveLocked() {
	if r.path == "" {
		return
	}
	ids := make([]string, 0, len(r.hidden))
	for id, h := range r.hidden {
		if h {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	b, _ := json.MarshalIndent(contribFile{TitlebarHidden: ids}, "", "  ")
	_ = os.MkdirAll(filepath.Dir(r.path), 0o755)
	tmp := r.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, r.path)
	}
}

// forgetPlugin drops every overlay and hidden flag a plugin left behind, so a
// later reinstall with the same ID starts clean.
func (r *contribRegistry) forgetPlugin(pluginID string) {
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
	Surface  Surface
}

type ResolvedCommand struct {
	PluginID   string
	PluginName string
	ID         string
	Title      string
	Shortcut   string
}

type ResolvedMenuItem struct {
	PluginID string
	ID       string
	Title    string
	Command  string
}

type ResolvedPanel struct {
	PluginID string
	ID       string
	Title    string
	Icon     DuoIconSpec
	Surface  Surface
}

type ResolvedStatusItem struct {
	PluginID string
	ID       string
	Text     string
	Tooltip  string
	Command  string
}

type ResolvedDialog struct {
	PluginID string
	ID       string
	Title    string
	Width    int
	Surface  Surface
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

// TitlebarHidden reports whether a titlebar slot is hidden. "more" is never hidden.
func (s *Supervisor) TitlebarHidden(slotID string) bool {
	if slotID == TitlebarSlotMore || s.contrib == nil {
		return false
	}
	s.contrib.mu.RLock()
	defer s.contrib.mu.RUnlock()
	return s.contrib.hidden[slotID]
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

// SettingsSections returns plugin sections for a built-in settings tab.
func (s *Supervisor) SettingsSections(tab string) []ResolvedSection {
	loc := s.Locale()
	var out []ResolvedSection
	for _, a := range s.enabledContribs() {
		for _, sec := range a.Manifest.Contributes.SettingsSections {
			if sec.Tab == tab {
				out = append(out, ResolvedSection{PluginID: a.Manifest.ID, ID: sec.ID, Title: sec.Title.Resolve(loc), Surface: SettingsSectionSurface(sec.ID)})
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
			out = append(out, ResolvedCommand{PluginID: a.Manifest.ID, PluginName: a.Manifest.Name.Resolve(loc), ID: cmd.ID, Title: cmd.Title.Resolve(loc), Shortcut: cmd.Shortcut})
		}
	}
	return out
}

func (s *Supervisor) menuItems(pick func(*Contributes) []MenuContrib) []ResolvedMenuItem {
	loc := s.Locale()
	var out []ResolvedMenuItem
	for _, a := range s.enabledContribs() {
		for _, it := range pick(a.Manifest.Contributes) {
			out = append(out, ResolvedMenuItem{PluginID: a.Manifest.ID, ID: it.ID, Title: it.Title.Resolve(loc), Command: it.Command})
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
			return ResolvedDialog{PluginID: pluginID, ID: d.ID, Title: d.Title.Resolve(loc), Width: w, Surface: DialogSurface(d.ID)}, true
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

// StatusItems returns status strip items with plugin-pushed text applied.
func (s *Supervisor) StatusItems() []ResolvedStatusItem {
	loc := s.Locale()
	var out []ResolvedStatusItem
	for _, a := range s.enabledContribs() {
		for _, st := range a.Manifest.Contributes.StatusItems {
			it := ResolvedStatusItem{PluginID: a.Manifest.ID, ID: st.ID, Text: st.Text.Resolve(loc), Command: st.Command}
			s.contrib.mu.RLock()
			if ov, ok := s.contrib.status[a.Manifest.ID+"/"+st.ID]; ok {
				it.Text, it.Tooltip = ov.Text, ov.Tooltip
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
	Selection string `json:"selection,omitempty"`
	Source    string `json:"source"` // palette | shortcut | titlebar | page_menu | editor_menu | status
}

// Command invokes plugin.command on the runtime.
func (rt *Runtime) Command(ctx context.Context, commandID string, cc CommandContext) (*ActionResult, error) {
	if !rt.manifest.Contributes.hasCommand(commandID) {
		return nil, fmt.Errorf("command %q not declared in manifest", commandID)
	}
	var res struct {
		OK     *bool  `json:"ok,omitempty"`
		Tone   string `json:"tone,omitempty"`
		Status string `json:"status,omitempty"`
	}
	if err := rt.call(ctx, "plugin.command", map[string]any{"command_id": commandID, "context": cc}, &res); err != nil {
		return nil, err
	}
	return &ActionResult{OK: res.OK, Tone: strings.TrimSpace(res.Tone), Status: strings.TrimSpace(res.Status)}, nil
}

// RunCommandAsync starts the plugin (if needed) and runs one command. Errors
// and a non-empty status are shown as a toast.
func (s *Supervisor) RunCommandAsync(pluginID, commandID string, cc CommandContext) {
	// The note and selection belong to the notebook open right now; a command
	// still queued after a notebook switch must not run against another one.
	nbRoot := s.NotebookRoot()
	if cc.Note == "" {
		cc.Note = s.ActiveNoteRel()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		unlock, err := s.lockPluginRPCContext(ctx, pluginID)
		if err != nil {
			s.toast(pluginID, err.Error(), "error")
			return
		}
		defer unlock()
		if s.NotebookRoot() != nbRoot {
			s.toast(pluginID, LocalizedText{Zh: "笔记本已切换，命令未执行", En: "Notebook changed; command not run"}.Resolve(s.Locale()), "warning")
			return
		}
		rt, err := s.ensureRuntime(ctx, pluginID, nbRoot)
		if err != nil {
			s.toast(pluginID, err.Error(), "error")
			return
		}
		if !rt.manifest.HasPermission(PermEditorRead) {
			cc.Selection = ""
		}
		s.contrib.setCommandNote(pluginID, cc.Note)
		res, err := rt.Command(ctx, commandID, cc)
		s.contrib.setCommandNote(pluginID, "")
		if err != nil {
			s.toast(pluginID, err.Error(), "error")
			return
		}
		if res != nil && res.Status != "" {
			tone := res.Tone
			if tone == "" {
				tone = "info"
			}
			s.toast(pluginID, res.Status, tone)
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

func (s *Supervisor) toast(pluginID, msg, tone string) {
	if s.cfg.UI != nil && strings.TrimSpace(msg) != "" {
		s.cfg.UI.Toast(pluginID, msg, tone)
	}
}

// ---------------------------------------------------------------------------
// host.* methods for UI contributions

func rpcErr(code int, msg string) *rpcError { return &rpcError{Code: code, Message: msg} }

func missingPerm(p Permission) *rpcError {
	return rpcErr(-32001, "missing permission: "+string(p))
}

// dispatchContribHostMethod handles host.titlebar.*, host.status.*, host.ui.*
// and host.editor.*. ok=false means the method is not a contrib method.
func (s *Supervisor) dispatchContribHostMethod(rt *Runtime, method string, params json.RawMessage) (res any, rerr *rpcError, ok bool) {
	if !strings.HasPrefix(method, "host.titlebar.") && !strings.HasPrefix(method, "host.status.") &&
		!strings.HasPrefix(method, "host.ui.") && !strings.HasPrefix(method, "host.editor.") {
		return nil, nil, false
	}
	m := rt.manifest
	// A runtime that is stopping or was replaced (disable/enable, notebook
	// switch) must not touch UI that now belongs to its successor.
	s.mu.RLock()
	current := s.runtimes[m.ID] == rt && !rt.closed.Load()
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
		next := map[string]bool{}
		for _, id := range p.Hidden {
			if id == TitlebarSlotMore || !known[id] {
				continue
			}
			next[id] = true
		}
		s.contrib.mu.Lock()
		if !maps.Equal(next, s.contrib.hidden) {
			s.contrib.hidden = next
			s.contrib.saveLocked()
		}
		s.contrib.mu.Unlock()
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
		s.contrib.mu.Lock()
		if p.Badge != nil {
			s.contrib.badges[slot] = truncateRunes(strings.TrimSpace(*p.Badge), 3)
		}
		if p.Active != nil {
			s.contrib.active[slot] = *p.Active
		}
		s.contrib.mu.Unlock()
		return map[string]any{"ok": true}, nil, true

	case "host.status.set":
		if !m.HasPermission(PermUIStatus) {
			return nil, missingPerm(PermUIStatus), true
		}
		var p struct {
			ID      string `json:"id"`
			Text    string `json:"text"`
			Tooltip string `json:"tooltip"`
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
		s.contrib.mu.Lock()
		s.contrib.status[m.ID+"/"+p.ID] = statusOverlay{Text: truncateRunes(strings.TrimSpace(p.Text), 40), Tooltip: truncateRunes(p.Tooltip, 200)}
		s.contrib.mu.Unlock()
		return map[string]any{"ok": true}, nil, true

	case "host.ui.toast":
		if !m.HasPermission(PermUINotify) {
			return nil, missingPerm(PermUINotify), true
		}
		var p struct {
			Message string `json:"message"`
			Tone    string `json:"tone"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		if strings.TrimSpace(p.Message) == "" {
			return nil, rpcErr(-32602, "message is required"), true
		}
		s.toast(m.ID, truncateRunes(p.Message, 160), normalizeTone(p.Tone))
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
		if ui != nil {
			if method == "host.ui.open_dialog" {
				ui.OpenDialog(m.ID, p.ID)
			} else {
				ui.CloseDialog(m.ID, p.ID)
			}
		}
		return map[string]any{"ok": true}, nil, true

	case "host.ui.open_panel", "host.ui.close_panel":
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
		if ui != nil {
			if method == "host.ui.open_panel" {
				ui.OpenPanel(m.ID, p.ID)
			} else {
				ui.ClosePanel(m.ID, p.ID)
			}
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
		// Only surfaces the user has opened are re-rendered; unopened ones
		// load fresh when they are first shown.
		if s.surfaceViewOpened(m.ID, surf) {
			go s.ReloadSurfaceViewAsync(m.ID, surf)
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
		expected := strings.TrimSpace(p.Note)
		if expected == "" {
			expected = s.contrib.runningCommandNote(m.ID)
		}
		if err := ui.EditorInsert(m.ID, expected, p.Text); err != nil {
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
