package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// PermUIMenubar lets a plugin place icons in the system menu bar (macOS) or
// notification area, with a menu and a drop target.
const PermUIMenubar Permission = "ui:menubar"

func init() { validPermissions[PermUIMenubar] = true }

// Menubar drop/paste payload kinds a plugin can accept.
const (
	MenubarAcceptFiles = "files"
	MenubarAcceptImage = "image"
	MenubarAcceptText  = "text"
)

// Command sources for menu bar entry points.
const (
	CommandSourceMenubar     = "menubar"      // icon click or a menu entry
	CommandSourceMenubarDrop = "menubar_drop" // files, image data or text dropped on the icon
)

const (
	maxMenubarItems        = 2
	maxMenubarMenuEntries  = 16
	maxMenubarDynamic      = 20
	maxMenubarExtensions   = 32
	maxMenubarFiles        = 50
	maxMenubarImages       = 10
	maxMenubarImageBytes   = 32 << 20
	maxMenubarTextBytes    = 64 << 10
	maxMenubarPayloadBytes = 4096
	maxMenubarTitleRunes   = 16
	menubarInboxTTL        = 24 * time.Hour
)

var menubarExtRe = regexp.MustCompile(`^[a-z0-9][a-z0-9+-]{0,15}$`)

// MenubarItemContrib is an icon in the system menu bar. Clicking opens Menu,
// or runs Command when there is no menu. Drop makes the icon a drop target.
type MenubarItemContrib struct {
	ID      string             `json:"id"`
	Icon    DuoIconSpec        `json:"icon"`
	Tooltip LocalizedText      `json:"tooltip"`
	Command string             `json:"command,omitempty"`
	Menu    []MenubarMenuEntry `json:"menu,omitempty"`
	Drop    *MenubarDrop       `json:"drop,omitempty"`
}

// MenubarMenuEntry is a static entry of a menu bar item's menu: a command,
// or a separator. Input "clipboard" makes the host read the clipboard and
// deliver it like a drop.
type MenubarMenuEntry struct {
	ID        string        `json:"id,omitempty"`
	Title     LocalizedText `json:"title,omitempty"`
	Command   string        `json:"command,omitempty"`
	Input     string        `json:"input,omitempty"`
	Separator bool          `json:"separator,omitempty"`
}

// MenubarDrop declares what the icon accepts and the command that receives it.
type MenubarDrop struct {
	Command    string   `json:"command"`
	Accept     []string `json:"accept,omitempty"`     // files | image | text; empty = files + image
	Extensions []string `json:"extensions,omitempty"` // lower-case, no dot; limits accepted files
}

// MenubarDynamicEntry is a runtime entry pushed with host.menubar.set, shown
// below the static entries, such as recent uploads.
type MenubarDynamicEntry struct {
	ID        string `json:"id,omitempty"`
	Title     string `json:"title,omitempty"`
	Subtitle  string `json:"subtitle,omitempty"`
	Command   string `json:"command,omitempty"`
	Payload   string `json:"payload,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	Separator bool   `json:"separator,omitempty"`
}

type menubarOverlay struct {
	Title   string
	Tooltip string
	Dynamic []MenubarDynamicEntry
}

// ResolvedMenubarItem is a menu bar icon of an enabled plugin, localized and
// with runtime overrides applied.
type ResolvedMenubarItem struct {
	PluginID    string
	PluginName  string
	ID          string
	Icon        DuoIconSpec
	Tooltip     string
	Title       string
	Command     string
	Menu        []ResolvedMenubarEntry
	Drop        *MenubarDrop
	HasSettings bool
}

// ResolvedMenubarEntry is one row of a menu bar item's menu.
type ResolvedMenubarEntry struct {
	ID        string
	Title     string
	Subtitle  string
	Command   string
	Input     string
	Payload   string
	Disabled  bool
	Separator bool
}

// Accepts reports whether the drop target takes payloads of kind.
func (d *MenubarDrop) Accepts(kind string) bool {
	if d == nil {
		return false
	}
	if len(d.Accept) == 0 {
		return kind == MenubarAcceptFiles || kind == MenubarAcceptImage
	}
	return slices.Contains(d.Accept, kind)
}

func (d *MenubarDrop) acceptsFile(path string) bool {
	if len(d.Extensions) == 0 {
		return true
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	return ext != "" && slices.Contains(d.Extensions, ext)
}

func (c *Contributes) validateMenubarItems() error {
	if len(c.MenubarItems) > maxMenubarItems {
		return fmt.Errorf("contributes.menubar_items allows at most %d entries", maxMenubarItems)
	}
	seen := map[string]bool{}
	for _, it := range c.MenubarItems {
		if !contribIDRe.MatchString(it.ID) {
			return fmt.Errorf("contributes.menubar_items id %q must match %s", it.ID, contribIDRe.String())
		}
		if seen[it.ID] {
			return fmt.Errorf("contributes.menubar_items duplicates id %q", it.ID)
		}
		seen[it.ID] = true
		if err := validateContribIcon("contributes.menubar_items.icon", it.Icon); err != nil {
			return err
		}
		if err := validateContribText("contributes.menubar_items.tooltip", it.Tooltip); err != nil {
			return err
		}
		if it.Command != "" && len(it.Menu) > 0 {
			return fmt.Errorf("contributes.menubar_items %q: command and menu are mutually exclusive", it.ID)
		}
		if it.Command == "" && len(it.Menu) == 0 && it.Drop == nil {
			return fmt.Errorf("contributes.menubar_items %q needs a command, a menu or a drop target", it.ID)
		}
		if it.Command != "" && !c.hasCommand(it.Command) {
			return fmt.Errorf("contributes.menubar_items %q references unknown command %q", it.ID, it.Command)
		}
		if len(it.Menu) > maxMenubarMenuEntries {
			return fmt.Errorf("contributes.menubar_items %q menu allows at most %d entries", it.ID, maxMenubarMenuEntries)
		}
		entryIDs := map[string]bool{}
		for _, e := range it.Menu {
			if e.Separator {
				if e.ID != "" || e.Command != "" || e.Input != "" || e.Title.Zh != "" || e.Title.En != "" {
					return fmt.Errorf("contributes.menubar_items %q: a separator takes no other fields", it.ID)
				}
				continue
			}
			if !contribIDRe.MatchString(e.ID) || entryIDs[e.ID] {
				return fmt.Errorf("contributes.menubar_items %q menu id %q is invalid or duplicated", it.ID, e.ID)
			}
			entryIDs[e.ID] = true
			if err := validateContribText("contributes.menubar_items.menu.title", e.Title); err != nil {
				return err
			}
			if !c.hasCommand(e.Command) {
				return fmt.Errorf("contributes.menubar_items %q menu %q references unknown command %q", it.ID, e.ID, e.Command)
			}
			if e.Input != "" && e.Input != "clipboard" {
				return fmt.Errorf("contributes.menubar_items %q menu %q input must be \"clipboard\"", it.ID, e.ID)
			}
		}
		if d := it.Drop; d != nil {
			if !c.hasCommand(d.Command) {
				return fmt.Errorf("contributes.menubar_items %q drop references unknown command %q", it.ID, d.Command)
			}
			kinds := map[string]bool{}
			for _, k := range d.Accept {
				if k != MenubarAcceptFiles && k != MenubarAcceptImage && k != MenubarAcceptText {
					return fmt.Errorf("contributes.menubar_items %q drop accept %q must be files, image or text", it.ID, k)
				}
				if kinds[k] {
					return fmt.Errorf("contributes.menubar_items %q drop accept duplicates %q", it.ID, k)
				}
				kinds[k] = true
			}
			if len(d.Extensions) > maxMenubarExtensions {
				return fmt.Errorf("contributes.menubar_items %q drop allows at most %d extensions", it.ID, maxMenubarExtensions)
			}
			for _, ext := range d.Extensions {
				if !menubarExtRe.MatchString(ext) {
					return fmt.Errorf("contributes.menubar_items %q drop extension %q must be lower-case without a dot", it.ID, ext)
				}
			}
		}
	}
	return nil
}

func (m *Manifest) menubarItem(id string) (MenubarItemContrib, bool) {
	if m.Contributes == nil {
		return MenubarItemContrib{}, false
	}
	for _, it := range m.Contributes.MenubarItems {
		if it.ID == id {
			return it, true
		}
	}
	return MenubarItemContrib{}, false
}

// MenubarItems returns the menu bar icons of enabled plugins.
func (s *Supervisor) MenubarItems() []ResolvedMenubarItem {
	loc := s.Locale()
	var out []ResolvedMenubarItem
	for _, a := range s.enabledContribs() {
		m := a.Manifest
		for _, it := range m.Contributes.MenubarItems {
			r := ResolvedMenubarItem{
				PluginID:    m.ID,
				PluginName:  m.Name.Resolve(loc),
				ID:          it.ID,
				Icon:        it.Icon,
				Tooltip:     it.Tooltip.Resolve(loc),
				Command:     it.Command,
				HasSettings: m.HasSurface(SurfaceSettingsPage),
			}
			if it.Drop != nil {
				d := *it.Drop
				r.Drop = &d
			}
			for _, e := range it.Menu {
				r.Menu = append(r.Menu, ResolvedMenubarEntry{
					ID: e.ID, Title: e.Title.Resolve(loc), Command: e.Command, Input: e.Input, Separator: e.Separator,
				})
			}
			s.contrib.mu.RLock()
			ov, ok := s.contrib.menubar[m.ID+"/"+it.ID]
			s.contrib.mu.RUnlock()
			if ok {
				r.Title = ov.Title
				if ov.Tooltip != "" {
					r.Tooltip = ov.Tooltip
				}
				if len(ov.Dynamic) > 0 && len(r.Menu) > 0 {
					r.Menu = append(r.Menu, ResolvedMenubarEntry{Separator: true})
				}
				for _, e := range ov.Dynamic {
					r.Menu = append(r.Menu, ResolvedMenubarEntry{
						ID: e.ID, Title: e.Title, Subtitle: e.Subtitle, Command: e.Command,
						Payload: e.Payload, Disabled: e.Disabled, Separator: e.Separator,
					})
				}
			}
			out = append(out, r)
		}
	}
	return out
}

// setMenubar handles host.menubar.set.
func (s *Supervisor) setMenubar(rt *Runtime, params json.RawMessage) (any, *rpcError) {
	m := rt.manifest
	if !m.HasPermission(PermUIMenubar) {
		return nil, missingPerm(PermUIMenubar)
	}
	var p struct {
		ID        string                 `json:"id"`
		Title     *string                `json:"title"`
		Tooltip   *string                `json:"tooltip"`
		Menu      *[]MenubarDynamicEntry `json:"menu"`
		ClearMenu bool                   `json:"clear_menu"`
	}
	if len(params) > 512*1024 || json.Unmarshal(params, &p) != nil {
		return nil, rpcErr(-32602, "invalid params")
	}
	if _, ok := m.menubarItem(p.ID); !ok {
		return nil, rpcErr(-32602, "unknown menubar item: "+p.ID)
	}
	if p.Title != nil && ContainsEmoji(*p.Title) {
		return nil, rpcErr(-32602, "menubar title must not contain emoji")
	}
	var dynamic []MenubarDynamicEntry
	if p.Menu != nil {
		if len(*p.Menu) > maxMenubarDynamic {
			return nil, rpcErr(-32602, fmt.Sprintf("menu allows at most %d entries", maxMenubarDynamic))
		}
		for _, e := range *p.Menu {
			if e.Separator {
				dynamic = append(dynamic, MenubarDynamicEntry{Separator: true})
				continue
			}
			e.Title = systemText(e.Title, 60)
			e.Subtitle = systemText(e.Subtitle, 80)
			if e.Title == "" {
				return nil, rpcErr(-32602, "menu entry title is required")
			}
			if e.Command != "" && !m.Contributes.hasCommand(e.Command) {
				return nil, rpcErr(-32602, "menu entry references unknown command: "+e.Command)
			}
			if len(e.Payload) > maxMenubarPayloadBytes || !utf8.ValidString(e.Payload) {
				return nil, rpcErr(-32602, "menu entry payload must be UTF-8 of at most 4096 bytes")
			}
			if e.Command == "" {
				e.Disabled = true
			}
			dynamic = append(dynamic, e)
		}
	}
	s.mu.RLock()
	active := s.isRuntimeActiveLocked(m.ID, rt)
	s.mu.RUnlock()
	if !active {
		return nil, rpcErr(-32603, "plugin runtime is no longer active")
	}
	key := m.ID + "/" + p.ID
	s.contrib.mu.Lock()
	ov := s.contrib.menubar[key]
	if p.Title != nil {
		ov.Title = truncateRunes(systemText(*p.Title, maxMenubarTitleRunes), maxMenubarTitleRunes)
	}
	if p.Tooltip != nil {
		ov.Tooltip = systemText(*p.Tooltip, 200)
	}
	if p.ClearMenu {
		ov.Dynamic = nil
	} else if p.Menu != nil {
		ov.Dynamic = dynamic
	}
	s.contrib.menubar[key] = ov
	s.contrib.mu.Unlock()
	s.notifyRedraw()
	return map[string]any{"ok": true}, nil
}

// MenubarPayload is what the user dropped on, or pasted into, a menu bar
// icon: local files, encoded images (PNG) and text.
type MenubarPayload struct {
	Files  []string
	Images [][]byte
	Text   string
}

// Empty reports whether the payload carries nothing.
func (p MenubarPayload) Empty() bool {
	return len(p.Files) == 0 && len(p.Images) == 0 && strings.TrimSpace(p.Text) == ""
}

// RunMenubarCommand runs a menu bar click or menu entry. payload is the
// clipboard's content for an entry with input "clipboard", nil otherwise.
func (s *Supervisor) RunMenubarCommand(pluginID, itemID, commandID, entryPayload string, clip *MenubarPayload) {
	m, ok := s.installedManifest(pluginID)
	if !ok || !m.HasPermission(PermUIMenubar) {
		s.menubarFeedback(pluginID, "", "", "")
		return
	}
	it, ok := m.menubarItem(itemID)
	if !ok || !m.Contributes.hasCommand(commandID) {
		s.menubarFeedback(pluginID, "", "", "")
		return
	}
	cc := CommandContext{Source: CommandSourceMenubar, MenubarItem: itemID, Payload: entryPayload}
	if clip != nil {
		if !m.HasPermission(PermClipboardRead) {
			s.menubarFeedback(pluginID, LocalizedText{Zh: "插件未获得读取剪贴板权限", En: "Plugin lacks clipboard read permission"}.Resolve(s.Locale()), "error", "")
			return
		}
		drop := it.Drop
		if drop == nil {
			drop = &MenubarDrop{Accept: []string{MenubarAcceptFiles, MenubarAcceptImage, MenubarAcceptText}}
		}
		if !s.fillMenubarPayload(pluginID, drop, *clip, &cc) {
			s.menubarFeedback(pluginID, LocalizedText{Zh: "剪贴板里没有可用的内容", En: "Nothing usable on the clipboard"}.Resolve(s.Locale()), "warning", "")
			return
		}
	}
	s.RunCommandAsync(pluginID, commandID, cc)
}

// DeliverMenubarDrop sends what the user dropped on a menu bar icon to the
// item's drop command. It reports whether anything was accepted.
func (s *Supervisor) DeliverMenubarDrop(pluginID, itemID string, payload MenubarPayload) bool {
	m, ok := s.installedManifest(pluginID)
	if !ok || !m.HasPermission(PermUIMenubar) {
		return false
	}
	it, ok := m.menubarItem(itemID)
	if !ok || it.Drop == nil {
		return false
	}
	cc := CommandContext{Source: CommandSourceMenubarDrop, MenubarItem: itemID}
	if !s.fillMenubarPayload(pluginID, it.Drop, payload, &cc) {
		s.menubarFeedback(pluginID, LocalizedText{Zh: "这个插件不接收这类内容", En: "This plugin does not accept that"}.Resolve(s.Locale()), "warning", "")
		return false
	}
	s.RunCommandAsync(pluginID, it.Drop.Command, cc)
	return true
}

// fillMenubarPayload keeps the first accepted kind, files before images
// before text, as a file dragged from Finder also carries its name as text.
// Images are written to the plugin's inbox and sent as file paths.
func (s *Supervisor) fillMenubarPayload(pluginID string, drop *MenubarDrop, p MenubarPayload, cc *CommandContext) bool {
	if drop.Accepts(MenubarAcceptFiles) && len(p.Files) > 0 {
		for _, f := range p.Files {
			info, err := os.Stat(f)
			if err != nil || !info.Mode().IsRegular() || !drop.acceptsFile(f) {
				continue
			}
			cc.Files = append(cc.Files, f)
			_ = s.GrantFSPath(pluginID, f, "menubar_drop")
			if len(cc.Files) == maxMenubarFiles {
				break
			}
		}
		if len(cc.Files) > 0 {
			cc.Kind = MenubarAcceptFiles
			return true
		}
	}
	if drop.Accepts(MenubarAcceptImage) && len(p.Images) > 0 {
		for i, img := range p.Images {
			if i == maxMenubarImages {
				break
			}
			if len(img) == 0 || len(img) > maxMenubarImageBytes {
				continue
			}
			path, err := s.writeMenubarInbox(pluginID, img)
			if err == nil {
				_ = s.GrantFSPath(pluginID, path, "menubar_image")
				cc.Files = append(cc.Files, path)
			}
		}
		if len(cc.Files) > 0 {
			cc.Kind = MenubarAcceptImage
			return true
		}
	}
	if drop.Accepts(MenubarAcceptText) && strings.TrimSpace(p.Text) != "" && len(p.Text) <= maxMenubarTextBytes && utf8.ValidString(p.Text) {
		cc.Text = p.Text
		cc.Kind = MenubarAcceptText
		return true
	}
	return false
}

// MenubarInboxDir holds images dropped on or pasted into a plugin's menu bar
// icon; files older than a day are removed on the next write.
func (s *Supervisor) MenubarInboxDir(pluginID string) string {
	return filepath.Join(s.cfg.AppDataDir, "apps-cache", "menubar-inbox", pluginID)
}

func (s *Supervisor) writeMenubarInbox(pluginID string, png []byte) (string, error) {
	dir := s.MenubarInboxDir(pluginID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	now := time.Now()
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > menubarInboxTTL {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	f, err := os.CreateTemp(dir, "pasted-"+now.Format("20060102-150405")+"-*.png")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(png); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func (s *Supervisor) installedManifest(pluginID string) (*Manifest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.installed {
		if a.Manifest.ID == pluginID && a.Enabled {
			m := a.Manifest
			return &m, true
		}
	}
	return nil, false
}

// MenubarFeedbackHost is implemented by hosts that can report the result of
// a menu bar command while Block may be in the background.
type MenubarFeedbackHost interface {
	MenubarFeedback(req ToastRequest, copyText string)
}

func (s *Supervisor) menubarFeedback(pluginID, message, tone, copyText string) {
	var icon *DuoIconSpec
	if m, ok := s.installedManifest(pluginID); ok {
		cp := m.Icon
		icon = &cp
	}
	req := ToastRequest{
		PluginID: pluginID,
		Message:  message,
		Tone:     normalizeTone(tone),
		Icon:     icon,
	}
	if h, ok := s.cfg.UI.(MenubarFeedbackHost); ok {
		h.MenubarFeedback(req, copyText)
		return
	}
	s.toastReq(req)
}
