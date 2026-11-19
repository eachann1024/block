package app

import (
	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

func (h *pluginHostUI) OpenPanel(pluginID, panelID string) {
	h.onMain(func() {
		h.openPanelLocal(pluginID, panelID)
	})
}

func (h *pluginHostUI) ClosePanel(pluginID, panelID string) {
	h.onMain(func() {
		h.closePanelLocal(pluginID, panelID)
	})
}

func (h *pluginHostUI) TogglePanel(pluginID, panelID string) {
	h.mu.Lock()
	isSame := h.activePanel != nil && h.activePanel.PluginID == pluginID && h.activePanel.PanelID == panelID
	h.mu.Unlock()
	if isSame {
		h.closePanelLocal(pluginID, panelID)
		return
	}
	h.openPanelLocal(pluginID, panelID)
}

func (h *pluginHostUI) openPanelLocal(pluginID, panelID string) {
	sup := h.w.Plugins()
	if sup == nil {
		return
	}
	if _, ok := sup.Panel(pluginID, panelID); !ok {
		return
	}
	ref := &pluginPanelRef{PluginID: pluginID, PanelID: panelID}
	h.mu.Lock()
	h.activePanel = ref
	h.lastPanel = ref
	h.mu.Unlock()

	if h.w.ai != nil && h.w.ai.IsOpen() {
		h.w.ai.SetOpen(false)
	}
	h.w.edgeFloat.DismissAll(h.w)
	h.w.RequestRedraw()
}

func (h *pluginHostUI) closePanelLocal(pluginID, panelID string) {
	h.mu.Lock()
	if h.activePanel == nil {
		h.mu.Unlock()
		return
	}
	if (pluginID == "" || h.activePanel.PluginID == pluginID) &&
		(panelID == "" || h.activePanel.PanelID == panelID) {
		h.activePanel = nil
	}
	h.mu.Unlock()
	h.w.RequestRedraw()
}

func (h *pluginHostUI) isPanelActive(pluginID, panelID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activePanel != nil && h.activePanel.PluginID == pluginID && h.activePanel.PanelID == panelID
}

func (h *pluginHostUI) hasOpenPanel() bool {
	h.mu.Lock()
	ref := h.activePanel
	h.mu.Unlock()
	if ref == nil {
		return false
	}
	sup := h.w.Plugins()
	if sup == nil {
		return false
	}
	if _, ok := sup.Panel(ref.PluginID, ref.PanelID); !ok {
		h.mu.Lock()
		if h.activePanel == ref {
			h.activePanel = nil
		}
		h.mu.Unlock()
		return false
	}
	return true
}

func (h *pluginHostUI) panelWidth() float32 {
	h.mu.Lock()
	w := h.panelWidthVal
	h.mu.Unlock()
	return ClampChatWidth(w)
}

func (h *pluginHostUI) setPanelWidth(w float32) {
	h.mu.Lock()
	h.panelWidthVal = ClampChatWidth(w)
	h.mu.Unlock()
}

func (h *pluginHostUI) currentOrClosingPanel() *pluginPanelRef {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.activePanel != nil {
		return h.activePanel
	}
	return h.lastPanel
}

func (h *pluginHostUI) renderDockedPanel(c *ui.Context, open bool, width, vpW, cardHorizMargin float32) {
	blockui.DockPanel(c, "docked-plugin-slot", open, width, true, func() {
		ref := h.currentOrClosingPanel()
		if ref == nil {
			return
		}
		sup := h.w.Plugins()
		if sup == nil {
			return
		}
		panel, ok := sup.Panel(ref.PluginID, ref.PanelID)
		if !ok {
			return
		}
		pal := theme.Ocean()
		pw := h.panelWidth()
		h.w.mu.RLock()
		sbW := h.w.sidebarWidth
		sbCollapsed := h.w.state.SidebarCollapsed
		h.w.mu.RUnlock()
		maxW := ChatLimit(vpW-cardHorizMargin, sbW, sbCollapsed, ChatMinW, ChatMaxW)
		if blockui.VerticalResizeHandle(c, "plugin-panel-resize", &pw, ChatMinW, maxW, true) {
			h.setPanelWidth(pw)
			c.Invalidate()
		}

		col := ui.Column(c.Key("plugin-side-panel")).FillHeight().Width(width).Shrink(0).
			Background(pal.Paper).TextColor(pal.Text)
		blockui.Probe(col, "plugin-side-panel")
		col.Children(func() {
			header := ui.Row(c.Key("plugin-panel-header")).FillWidth().Height(theme.TitleBarHeight).Shrink(0).
				Padding(0, theme.Space12, 0, theme.Space16).Gap(theme.Space8).
				AlignItems(ui.Center).Background(pal.Paper)
			header.Children(func() {
				ui.Text(c, panel.Title).
					FontSize(theme.FontSmall).
					FixedLineHeight(theme.LineSmall).
					FontWeight(theme.WeightControl).
					TextColor(pal.TextSecondary).
					MaxWidth(theme.TitleBarTitleMaxWidth).
					Shrink(1).
					MinWidth(0).
					SingleLine().
					Ellipsis("…")
				ui.Spacer(c)
				closeBtn := blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{
					ID:      "plugin-panel-close",
					Label:   theme.Tr("关闭面板", "Close panel"),
					Tooltip: theme.Tr("关闭", "Close"),
				})
				if closeBtn.Clicked() {
					h.closePanelLocal(panel.PluginID, panel.ID)
				}
			})

			body := blockui.Scroll(c.Key("plugin-panel-body-" + panel.PluginID + "-" + panel.ID)).
				Grow(1).MinHeight(0).FillWidth().Padding(theme.Space12, theme.Space16)
			body.Children(func() {
				if h.w.pluginsUI != nil {
					h.w.pluginsUI.RenderSurface(c, panel.PluginID, plugins.PanelSurface(panel.ID))
				}
			})
		})
	})
}
