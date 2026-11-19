package pluginui

import (
	"strings"

	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

func surfaceRenderScope(pluginID string, surface plugins.Surface) string {
	if surface == "" || surface == plugins.SurfaceSettingsPage {
		return pluginID
	}
	clean := strings.NewReplacer(":", "-", "/", "-").Replace(string(surface))
	return pluginID + "-" + clean
}

// RenderSurface draws one plugin surface (settings section, panel or dialog
// body) with the same declarative renderer as plugin settings pages.
func (c *Controller) RenderSurface(ctx *ui.Context, pluginID string, surface plugins.Surface) {
	if c == nil || c.sup == nil {
		return
	}
	inst, ok := c.sup.GetInstalled(pluginID)
	if !ok || !inst.Enabled {
		return
	}

	vs := c.sup.EnsureSurfaceView(pluginID, surface)
	pal := theme.Ocean()
	scope := surfaceRenderScope(pluginID, surface)
	bodyGap := theme.Space12
	cardGap := theme.Space16
	if surface == plugins.SurfaceSettingsPage {
		bodyGap = theme.SettingsSectionGap
		cardGap = theme.SettingsSectionGap
	}

	ui.Column(ctx.Key("plugin-surface-body-" + scope)).FillWidth().MinWidth(0).Gap(bodyGap).Children(func() {
		if vs.Error != "" {
			ui.Row(ctx.Key("plugin-error-"+scope)).FillWidth().MinWidth(0).
				Padding(theme.Space10, theme.Space12).Gap(theme.Space12).Radius(theme.RadiusMD).
				Background(pal.DangerSurface).Border(1, pal.Danger.Alpha(0.35)).AlignItems(ui.Center).Justify(ui.SpaceBetween).
				Transition(bui.CascadeRowTransition(0)).Children(func() {
				ui.Text(ctx, vs.Error).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					TextColor(pal.Danger).Grow(1).MinWidth(0)
				busy := vs.Loading || vs.BusyAction != ""
				label := theme.Tr("重新加载", "Reload")
				if busy {
					label = theme.Tr("加载中…", "Loading…")
				}
				refIc := icons.Refresh
				reloadBtn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
					ID:       "plugin-reload-" + scope,
					Label:    label,
					Leading:  &refIc,
					Style:    bui.ButtonSecondary,
					Disabled: busy,
					Loading:  busy,
				})
				if reloadBtn.Clicked() && !busy {
					c.sup.ReloadSurfaceViewAsync(pluginID, surface)
				}
			})
		}

		if vs.Loading && vs.Page == nil {
			ctx.Key("plugin-loading-" + scope)
			msg := theme.Tr("正在加载插件页面…", "Loading plugin page…")
			bui.SettingsRowFrame(ui.Row(ctx)).Gap(theme.Space8).Children(func() {
				ui.Spinner(ctx).FontSize(theme.IconAction).TextColor(pal.Accent).Label(msg)
				ui.Text(ctx, msg).
					FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			})
			return
		}

		if vs.Page != nil {
			if surface == plugins.SurfaceSettingsPage && vs.Page.RefreshIntervalMs > 0 {
				c.sup.ScheduleLiveRefresh(pluginID, vs.Page.RefreshIntervalMs)
			}
			renderPageCardsWithScope(
				ctx,
				scope,
				cardGap,
				vs.Page,
				vs.Values,
				actionFeedbackFromView(vs),
				c.inputBindingsForSurface(pluginID, surface),
				func(controlID string, nextValue any, immediate bool) {
					c.sup.UpdateSurfaceControlValue(pluginID, surface, controlID, nextValue, immediate)
				},
				func(actionID string, _ map[string]any) {
					c.sup.TriggerSurfaceActionAsync(pluginID, surface, actionID)
				},
			)
		}
	})
}

// RenderSettingsSections appends plugin sections declared for a built-in settings tab.
func (c *Controller) RenderSettingsSections(ctx *ui.Context, tab string) {
	if c == nil || c.sup == nil {
		return
	}
	sections := c.sup.SettingsSections(tab)
	if len(sections) == 0 {
		return
	}
	for _, sec := range sections {
		s := sec
		ctx.Key("plugin-settings-sec-" + s.PluginID + "-" + s.ID)
		bui.SettingsSection(ctx, func() {
			if strings.TrimSpace(s.Title) != "" {
				bui.SettingsSectionTitle(ctx, s.Title, nil)
			}
			c.RenderSurface(ctx, s.PluginID, s.Surface)
		})
	}
}
