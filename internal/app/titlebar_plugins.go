package app

import (
	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// maxInlinePluginButtons caps how many plugin buttons sit directly in the titlebar;
// the rest collapse into the overflow menu so built-in tools never get pushed out.
const maxInlinePluginButtons = 3

// splitPluginTitlebarButtons drops user-hidden buttons, then splits the remainder into
// the ones shown inline and the ones folded into the overflow menu. Narrow windows keep
// fewer inline so the tab strip is the last thing to give way.
func splitPluginTitlebarButtons(p TitlebarProps, vpW float32) (inline, overflow []plugins.ResolvedTitlebarButton) {
	visible := make([]plugins.ResolvedTitlebarButton, 0, len(p.PluginButtons))
	for _, b := range p.PluginButtons {
		if !slotHidden(p, pluginButtonSlotID(b)) {
			visible = append(visible, b)
		}
	}
	limit := maxInlinePluginButtons
	switch {
	case vpW <= 0:
	case vpW < 720:
		limit = 0
	case vpW < 900:
		limit = 1
	}
	if len(visible) <= limit {
		return visible, nil
	}
	return visible[:limit], visible[limit:]
}

func pluginButtonSlotID(b plugins.ResolvedTitlebarButton) string {
	if b.SlotID != "" {
		return b.SlotID
	}
	return plugins.PluginTitlebarSlotID(b.PluginID, b.ID)
}

func pluginButtonLabel(b plugins.ResolvedTitlebarButton) string {
	return nonEmptyOr(b.Tooltip, b.ID)
}

func renderPluginOverflowMenu(c *ui.Context, p TitlebarProps, scrim ui.Element) {
	vpW, _ := c.Size()
	_, overflow := splitPluginTitlebarButtons(p, vpW)
	if len(overflow) == 0 {
		p.MenuState.Close()
		return
	}
	icManage := icons.Plug
	items := make([]blockui.MenuItemSpec, 0, len(overflow)+2)
	for _, raw := range overflow {
		b := raw
		items = append(items, blockui.MenuItemSpec{
			ID:      "titlebar-plugin-menu-" + b.PluginID + "-" + b.ID,
			Label:   pluginButtonLabel(b),
			Hint:    pluginOverflowBadge(b.Badge),
			Checked: b.Active,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnPluginButton != nil {
					p.OnPluginButton(b)
				}
			},
		})
	}
	items = append(items, blockui.MenuItemSpec{Separator: true}, blockui.MenuItemSpec{
		ID:    "titlebar-plugin-menu-manage",
		Label: theme.Tr("管理插件…", "Manage Plugins…"),
		Icon:  &icManage,
		OnClick: func() {
			p.MenuState.Close()
			if p.OnOpenPluginStore != nil {
				p.OnOpenPluginStore()
			}
		},
	})
	renderTitlebarAnchoredMenu(c, p, scrim, titlebarMenuSpec{
		Key:        "titlebar-plugin-menu",
		Anchor:     p.MenuState.PluginMenuAnchor,
		AlignRight: true,
		Width:      220,
		Items:      items,
	})
}

func pluginOverflowBadge(badge string) string {
	if isDotBadge(badge) {
		return "•"
	}
	return badge
}
