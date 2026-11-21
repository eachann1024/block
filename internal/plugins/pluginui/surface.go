package pluginui

import (
	"strings"

	"block/internal/icons"
	"block/internal/modals"
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
	c.renderSurface(ctx, pluginID, surface, "")
}

func (c *Controller) renderSurface(ctx *ui.Context, pluginID string, surface plugins.Surface, outerSectionTitle string) {
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
	} else if strings.HasPrefix(string(surface), plugins.SurfacePrefixSidebarSection) {
		bodyGap = theme.Space6
		cardGap = theme.Space8
	}

	ui.Column(ctx.Key("plugin-surface-body-" + scope)).FillWidth().MinWidth(0).Gap(bodyGap).Children(func() {
		if vs.Error != "" {
			ui.Row(ctx.Key("plugin-error-"+scope)).FillWidth().MinWidth(0).
				Padding(theme.Space10, theme.Space12).Gap(theme.Space12).Radius(theme.RadiusMD).
				Background(pal.DangerSurface).AlignItems(ui.Center).Justify(ui.SpaceBetween).
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
			page := vs.Page
			if outerSectionTitle != "" && len(page.Cards) == 1 &&
				strings.EqualFold(strings.TrimSpace(page.Cards[0].Title), strings.TrimSpace(outerSectionTitle)) {
				cloned := *page
				cards := append([]plugins.UICard(nil), page.Cards...)
				cards[0].Title = ""
				cloned.Cards = cards
				page = &cloned
			}
			fb := actionFeedbackFromView(vs)
			if strings.HasPrefix(string(surface), plugins.SurfacePrefixSidebarSection) ||
				strings.HasPrefix(string(surface), plugins.SurfacePrefixPanel) {
				fb.CompactLayout = true
			}
			if strings.HasPrefix(string(surface), plugins.SurfacePrefixSidebarSection) {
				fb.SidebarLayout = true
			}
			renderPageCardsWithScope(
				ctx,
				scope,
				cardGap,
				page,
				vs.Values,
				fb,
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
func (c *Controller) RenderSettingsSections(ctx *ui.Context, tab string, onSelectTab func(nextTab string)) {
	if c == nil || c.sup == nil {
		return
	}
	sections := c.sup.SettingsSections(tab)
	if len(sections) == 0 {
		return
	}
	pal := theme.Ocean()
	for _, sec := range sections {
		s := sec
		inst, ok := c.sup.GetInstalled(s.PluginID)
		if !ok || !inst.Enabled {
			continue
		}
		ctx.Key("plugin-settings-sec-" + s.PluginID + "-" + s.ID)
		bui.SettingsSection(ctx, func() {
			if strings.TrimSpace(s.Title) != "" {
				var lead func()
				if len(s.Icon.StrokePaths) > 0 {
					icSpec := s.Icon
					lead = func() {
						DrawDuoSpec(ctx, icSpec, theme.IconRow, pal.DuoIconColors(pal.Text))
					}
				}
				var trail func()
				if onSelectTab != nil {
					trail = func() {
						jump := ui.ButtonBase(ctx.Key("plugin-sec-jump-" + s.PluginID + "-" + s.ID)).
							Padding(theme.Space2, theme.Space6).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
							Label(theme.Tr("前往插件设置", "Open plugin settings"))
						if jump.Hovered() {
							jump.Background(pal.Hover)
						}
						jump.Children(func() {
							icons.Draw(ctx, icons.TbChevronRight, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary))
						})
						if jump.Clicked() {
							onSelectTab(modals.TabPluginPrefix + s.PluginID)
						}
					}
				}
				bui.SettingsSectionTitleWithIcon(ctx, lead, s.Title, trail)
			}
			c.renderSurface(ctx, s.PluginID, s.Surface, s.Title)
		})
	}
}

// RenderSidebarSections draws compact declarative cards contributed by enabled
// ui:sidebar plugins above the sidebar footer.
func (c *Controller) RenderSidebarSections(ctx *ui.Context) {
	if c == nil || c.sup == nil {
		return
	}
	sections := c.sup.SidebarSections()
	if len(sections) == 0 {
		return
	}
	pal := theme.Ocean()
	bui.Scroll(ctx.Key("plugin-sidebar-sections")).FillWidth().MaxHeight(240).Shrink(1).Children(func() {
		ui.Column(ctx).FillWidth().Padding(theme.Space6, theme.TreeOuterPadX, theme.Space4, theme.TreeOuterPadX).Gap(theme.Space8).Children(func() {
			for _, sec := range sections {
				s := sec
				key := "plugin-sidebar-sec-" + s.PluginID + "-" + s.ID
				bindings := c.inputBindingsForSurface(s.PluginID, s.Surface)
				expanded := true
				if s.Collapsible && bindings != nil && bindings.IsCardExpanded != nil {
					expanded = bindings.IsCardExpanded("__sidebar_sec__:"+s.ID, s.DefaultCollapsed)
				}
				card := ui.Column(ctx.Key(key)).FillWidth().Shrink(0).
					Padding(theme.Space8).Gap(theme.Space6).
					Radius(theme.RadiusLG).Background(pal.Paper).Border(1, pal.Hairline)
				bui.Probe(card, key)
				card.Children(func() {
					headerRow := func() {
						DrawDuoSpec(ctx, s.Icon, theme.IconRow, pal.DuoIconColors(pal.Text))
						ui.Text(ctx, s.Title).
							FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
							FontWeight(theme.WeightTitle).TextColor(pal.Text).
							Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
						if s.Collapsible {
							chev := icons.TbChevronRight
							if expanded {
								chev = icons.ChevronDown
							}
							icons.Draw(ctx, chev, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary))
						}
					}
					if s.Collapsible {
						hdr := ui.ButtonBase(ctx.Key(key+"-hdr")).FillWidth().Height(theme.HeightControl).
							Padding(0, theme.Space8).Gap(theme.Space8).AlignItems(ui.Center).
							Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(s.Title)
						bg := ui.Transparent
						if hdr.Pressed() {
							bg = pal.Selected
						} else if hdr.Hovered() {
							bg = pal.Hover
						}
						bui.AnimateSurfaceColors(hdr, bg, ui.Transparent, false)
						hdr.Children(headerRow)
						if hdr.Clicked() && bindings != nil && bindings.ToggleCardExpanded != nil {
							bindings.ToggleCardExpanded("__sidebar_sec__:"+s.ID, s.DefaultCollapsed)
						}
					} else {
						ui.Row(ctx.Key(key+"-hdr")).FillWidth().Height(theme.HeightControl).
							Padding(0, theme.Space8).Gap(theme.Space8).AlignItems(ui.Center).
							Children(headerRow)
					}
					if expanded {
						c.renderSurface(ctx, s.PluginID, s.Surface, s.Title)
					}
				})
			}
		})
	})
}
