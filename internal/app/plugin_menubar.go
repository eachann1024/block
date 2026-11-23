package app

import (
	"bytes"
	"encoding/json"
	"html"
	"image/color"
	"image/png"
	"reflect"
	"strconv"
	"strings"
	"time"

	"block/internal/plugins"
	"block/internal/theme"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// pluginMenubar keeps one system tray icon per contributes.menubar_items
// entry of enabled plugins. A poll compares the resolved items, so enabling,
// disabling, updating, switching language and host.menubar.set all land
// without hooks in the workspace. Tray state is touched on the main thread.
type pluginMenubar struct {
	h       *pluginHostUI
	trays   map[string]*menubarTray
	names   map[string]string // plugin id -> localized name, for notifications
	lastSig string
}

type menubarTray struct {
	tray     *mygo.Tray
	item     plugins.ResolvedMenubarItem
	offClick func()
	detach   func()
}

const menubarPollInterval = 400 * time.Millisecond

func (h *pluginHostUI) startMenubar() {
	h.menubar = &pluginMenubar{h: h, trays: map[string]*menubarTray{}, names: map[string]string{}}
	mygo.App.WhenReady(func() { go h.menubar.loop() })
}

func (m *pluginMenubar) loop() {
	ticker := time.NewTicker(menubarPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.h.closing:
			return
		case <-ticker.C:
		}
		sup := m.h.w.Plugins()
		if sup == nil {
			continue
		}
		items := sup.MenubarItems()
		raw, _ := json.Marshal(items)
		if sig := string(raw); sig != m.lastSig {
			m.lastSig = sig
			mygo.RunOnMain(func() { m.apply(items) })
		}
	}
}

func (m *pluginMenubar) apply(items []plugins.ResolvedMenubarItem) {
	seen := map[string]bool{}
	for _, it := range items {
		key := it.PluginID + "/" + it.ID
		seen[key] = true
		m.names[it.PluginID] = it.PluginName
		mt := m.trays[key]
		if mt != nil && (!reflect.DeepEqual(mt.item.Drop, it.Drop) || !reflect.DeepEqual(mt.item.Icon, it.Icon)) {
			m.destroy(mt)
			mt = nil
		}
		if mt == nil {
			if mt = m.create(it); mt == nil {
				continue
			}
			m.trays[key] = mt
		}
		m.update(mt, it)
	}
	for key, mt := range m.trays {
		if !seen[key] {
			m.destroy(mt)
			delete(m.trays, key)
		}
	}
}

func (m *pluginMenubar) create(it plugins.ResolvedMenubarItem) *menubarTray {
	icon, err := menubarIconPNG(it.Icon)
	if err != nil {
		return nil
	}
	// The one-off tooltip lets the native layer find this tray's button.
	sentinel := "block-menubar-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "block.plugin." + it.PluginID + "." + it.ID,
		Icon:           icon,
		IconIsTemplate: true,
		ToolTip:        sentinel,
	})
	if err != nil {
		return nil
	}
	pluginID, itemID := it.PluginID, it.ID
	mt := &menubarTray{tray: tray}
	mt.detach = attachMenubarDrop(sentinel, it.Drop, func(p plugins.MenubarPayload) {
		if sup := m.h.w.Plugins(); sup != nil {
			sup.DeliverMenubarDrop(pluginID, itemID, p)
		}
	})
	return mt
}

func (m *pluginMenubar) update(mt *menubarTray, it plugins.ResolvedMenubarItem) {
	mt.item = it
	mt.tray.SetToolTip(it.Tooltip)
	mt.tray.SetTitle(it.Title)
	menu := m.buildMenu(it)
	mt.tray.SetMenu(menu)
	switch {
	case menu == nil && it.Command != "" && mt.offClick == nil:
		mt.offClick = mt.tray.OnClick(func() {
			cur := mt.item
			if sup := m.h.w.Plugins(); sup != nil && cur.Command != "" {
				go sup.RunMenubarCommand(cur.PluginID, cur.ID, cur.Command, "", nil)
			}
		})
	case menu != nil && mt.offClick != nil:
		mt.offClick()
		mt.offClick = nil
	}
}

func (m *pluginMenubar) destroy(mt *menubarTray) {
	if mt.detach != nil {
		mt.detach()
	}
	if mt.offClick != nil {
		mt.offClick()
	}
	mt.tray.Destroy()
}

func (m *pluginMenubar) buildMenu(it plugins.ResolvedMenubarItem) *mygo.Menu {
	var items []*mygo.MenuItem
	separator := func() {
		if n := len(items); n > 0 && items[n-1].Type != mygo.MenuItemSeparator {
			items = append(items, mygo.Separator())
		}
	}
	for _, e := range it.Menu {
		if e.Separator {
			separator()
			continue
		}
		entry := e
		items = append(items, &mygo.MenuItem{
			Label:    entry.Title,
			Subtitle: entry.Subtitle,
			Disabled: entry.Disabled || entry.Command == "",
			Click: func(*mygo.MenuItem, *mygo.Window) {
				m.runEntry(it.PluginID, it.ID, entry)
			},
		})
	}
	// A command icon runs on click, so it gets no menu of its own.
	if it.HasSettings && (len(it.Menu) > 0 || it.Command == "") {
		separator()
		pluginID := it.PluginID
		items = append(items, &mygo.MenuItem{
			Label: theme.Tr("插件设置…", "Plugin Settings…"),
			Click: func(*mygo.MenuItem, *mygo.Window) {
				mygo.App.Focus()
				m.h.w.OpenSettings("plugin:" + pluginID)
			},
		})
	}
	for len(items) > 0 && items[len(items)-1].Type == mygo.MenuItemSeparator {
		items = items[:len(items)-1]
	}
	if len(items) == 0 {
		return nil
	}
	return mygo.NewMenu(items)
}

// runEntry runs on the main thread, where the clipboard is read for an
// entry with input "clipboard".
func (m *pluginMenubar) runEntry(pluginID, itemID string, e plugins.ResolvedMenubarEntry) {
	sup := m.h.w.Plugins()
	if sup == nil || e.Command == "" {
		return
	}
	var clip *plugins.MenubarPayload
	if e.Input == "clipboard" {
		p := readMenubarClipboard()
		clip = &p
	}
	go sup.RunMenubarCommand(pluginID, itemID, e.Command, e.Payload, clip)
}

// MenubarFeedback reports a menu bar command's result: copyText goes to the
// clipboard, and the message shows as a toast while Block is in front, or as
// a system notification while it is not.
func (h *pluginHostUI) MenubarFeedback(req plugins.ToastRequest, copyText string) {
	h.onMain(func() {
		if copyText != "" && mygo.App.IsReady() {
			if err := mygo.Clipboard.Write(transfer.TextData(copyText)); err != nil {
				req.Message = theme.Tr("无法写入剪贴板", "Could not write to the clipboard")
				req.Body = ""
				req.Tone = "error"
				req.Action = nil
			}
		}
		if strings.TrimSpace(req.Message) == "" {
			return
		}
		win := h.w.Window()
		if !mygo.App.IsReady() || (mygo.App.IsActive() && win != nil && win.IsFocused()) {
			h.toastReq(req)
			return
		}
		title := "Block"
		if h.menubar != nil && h.menubar.names[req.PluginID] != "" {
			title = h.menubar.names[req.PluginID]
		}
		body := req.Message
		if strings.TrimSpace(req.Body) != "" {
			body += "\n" + strings.TrimSpace(req.Body)
		}
		n := mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body, Silent: req.Tone != "error"})
		go func() { _ = n.Show() }()
	})
}

var _ plugins.MenubarFeedbackHost = (*pluginHostUI)(nil)

// menubarIconPNG draws a plugin icon's line layers as a black template image
// for the menu bar; the soft fill layer would read as a solid blob there.
func menubarIconPNG(spec plugins.DuoIconSpec) ([]byte, error) {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`)
	b.WriteString(`<g fill="none" stroke="black" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">`)
	for _, p := range append(append([]string(nil), spec.StrokePaths...), spec.AccentPaths...) {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString(`<path d="` + html.EscapeString(p) + `"/>`)
		}
	}
	b.WriteString(`</g></svg>`)
	svg, err := ui.ParseSVG([]byte(b.String()))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, svg.Image(36, 36, color.Black)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
