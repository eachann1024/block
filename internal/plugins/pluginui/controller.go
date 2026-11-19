package pluginui

import (
	"strconv"
	"strings"
	"sync"

	"block/internal/icons"
	"block/internal/modals"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var _ modals.PluginSettingsHost = (*Controller)(nil)

// Controller bridges Block's Settings UI with the App Store catalog and installed plugin Supervisor.
type Controller struct {
	sup               *plugins.Supervisor
	storeReadme       *storeReadmePreview
	storeReadmeWindow *mygo.Window

	mu             sync.Mutex
	storeFilter    int
	manualSpec     string
	drafts         map[string]map[string]string
	lastSynced     map[string]map[string]string
	secretRevealed map[string]map[string]bool
	selectStates   map[string]map[string]*bui.DropdownSelectState
	treemapStates  map[string]map[string]*bui.TreemapState
}

// NewController constructs a PluginSettingsHost controller backed by sup.
func NewController(sup *plugins.Supervisor) *Controller {
	return &Controller{
		sup:            sup,
		drafts:         make(map[string]map[string]string),
		lastSynced:     make(map[string]map[string]string),
		secretRevealed: make(map[string]map[string]bool),
		selectStates:   make(map[string]map[string]*bui.DropdownSelectState),
		treemapStates:  make(map[string]map[string]*bui.TreemapState),
	}
}

// Supervisor returns the underlying plugin Supervisor.
func (c *Controller) Supervisor() *plugins.Supervisor {
	return c.sup
}

// NavEntries returns the lower-partition Settings sidebar entries: App Store followed by installed plugins.
func (c *Controller) NavEntries() []modals.PluginNavEntry {
	storeSpec := plugins.AppStoreIconSpec()
	var installed []plugins.InstalledApp
	if c.sup != nil {
		installed = c.sup.Installed()
	}
	entries := []modals.PluginNavEntry{
		{
			TabID:     modals.TabAppStore,
			Title:     theme.Tr("应用商店", "App Store"),
			CountText: strconv.Itoa(len(installed)),
			DrawIcon: func(ctx *ui.Context, size float32, colors theme.IconColors) {
				DrawDuoSpec(ctx, storeSpec, size, colors)
			},
		},
	}
	if c.sup == nil {
		return entries
	}
	locale := c.sup.Locale()
	for _, inst := range installed {
		app := inst
		entries = append(entries, modals.PluginNavEntry{
			TabID:     modals.TabPluginPrefix + app.Manifest.ID,
			Title:     app.Manifest.Name.Resolve(locale),
			StatusDot: app.Enabled,
			HasError:  c.sup.HasPluginError(app.Manifest.ID),
			DrawIcon: func(ctx *ui.Context, size float32, colors theme.IconColors) {
				DrawDuoSpec(ctx, app.Manifest.Icon, size, colors)
			},
		})
	}
	return entries
}

// TabTitle returns the localized header title for the App Store or an installed plugin tab.
func (c *Controller) TabTitle(tab string) string {
	if tab == modals.TabAppStore {
		return theme.Tr("应用商店", "App Store")
	}
	if pluginID, ok := strings.CutPrefix(tab, modals.TabPluginPrefix); ok {
		if c.sup != nil {
			if inst, found := c.sup.GetInstalled(pluginID); found {
				return inst.Manifest.Name.Resolve(c.sup.Locale())
			}
		}
		return pluginID
	}
	return ""
}

// RenderHeaderExtras renders the trailing controls in the Settings top header for App Store or plugin tabs.
func (c *Controller) RenderHeaderExtras(ctx *ui.Context, tab string, onSelectTab func(nextTab string)) {
	if c.sup == nil {
		return
	}
	if tab == modals.TabAppStore {
		renderStoreHeaderExtras(ctx, c)
		return
	}
	pluginID, ok := strings.CutPrefix(tab, modals.TabPluginPrefix)
	if !ok || pluginID == "" {
		return
	}
	inst, ok := c.sup.GetInstalled(pluginID)
	if !ok {
		return
	}

	var vs plugins.PluginViewState
	if inst.Enabled {
		vs = c.sup.EnsurePluginView(pluginID)
	}
	ui.Row(ctx.Key("plugin-header-extras-" + pluginID)).Gap(theme.Space10).AlignItems(ui.Center).MinWidth(0).Shrink(1).Children(func() {
		if inst.Enabled {
			statusText := strings.TrimSpace(vs.Notice)
			if statusText == "" && vs.Page != nil {
				statusText = strings.TrimSpace(vs.Page.Status)
			}
			if vs.PendingSaves > 0 {
				statusText = strconv.Itoa(vs.PendingSaves) + theme.Tr(" 项待处理", " pending")
			}
			if statusText != "" {
				renderStatusText(ctx, statusText)
			}
		} else {
			renderStatusText(ctx, theme.Tr("已停用", "Disabled"))
		}

		enabled := inst.Enabled
		toggleTip := theme.Tr("已停用", "Disabled")
		if enabled {
			toggleTip = theme.Tr("已启用", "Enabled")
		}
		toggleWrap := ui.Row(ctx.Key("plugin-hdr-enable-wrap-" + pluginID)).AlignItems(ui.Center).Shrink(0)
		bui.Tooltip(ctx, toggleWrap, toggleTip)
		toggleWrap.Children(func() {
			if bui.TogglePill(ctx, "plugin-hdr-enable-"+pluginID, theme.Tr("启用扩展", "Enable Extension"), &enabled, vs.BusyAction != "") {
				c.sup.SetPluginEnabled(pluginID, enabled)
			}
		})

		if inst.Enabled && vs.Page != nil {
			for _, btn := range vs.Page.HeaderActions {
				b := btn
				el := bui.ActionButton(ctx, "plugin-hdr-act-"+pluginID+"-"+b.ID, b.Label, nil, mapButtonStyle(b.Variant), vs.BusyAction != "")
				if el.Clicked() {
					c.sup.TriggerActionAsync(pluginID, b.ID)
				}
			}
		}

		uninstBtn := bui.ActionButton(ctx, "plugin-hdr-uninstall-"+pluginID, theme.Tr("卸载", "Uninstall"), nil, bui.ButtonDanger, vs.Loading || vs.BusyAction != "")
		if uninstBtn.Clicked() {
			c.uninstallPlugin(pluginID, func() {
				if onSelectTab != nil {
					onSelectTab(modals.TabAppStore)
				}
			})
		}
	})
}

// RenderTabBody renders the main body of the App Store or an installed plugin Settings page.
func (c *Controller) RenderTabBody(ctx *ui.Context, tab string, onSelectTab func(nextTab string)) {
	if c.sup == nil {
		return
	}
	if tab == modals.TabAppStore {
		renderStoreBody(ctx, c, onSelectTab)
		return
	}
	pluginID, ok := strings.CutPrefix(tab, modals.TabPluginPrefix)
	if !ok || pluginID == "" {
		return
	}
	inst, isInstalled := c.sup.GetInstalled(pluginID)
	if !isInstalled {
		pal := theme.Ocean()
		ctx.Key("plugin-not-installed-" + pluginID)
		bui.Card(ctx, func() {
			ui.Row(ctx).FillWidth().MinWidth(0).Gap(theme.Space16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(ctx, theme.Tr("该插件尚未安装", "This plugin is not installed")).
					FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
				btn := bui.ActionButton(ctx, "plugin-go-store-"+pluginID, theme.Tr("前往应用商店", "Go to App Store"), nil, bui.ButtonSecondary, false)
				if btn.Clicked() && onSelectTab != nil {
					onSelectTab(modals.TabAppStore)
				}
			})
		})
		return
	}
	if !inst.Enabled {
		pal := theme.Ocean()
		ctx.Key("plugin-disabled-" + pluginID)
		bui.Card(ctx, func() {
			ui.Row(ctx).FillWidth().MinWidth(0).Gap(theme.Space16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(ctx, theme.Tr("已停用", "Disabled")).
					FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
				btn := bui.ActionButton(ctx, "plugin-enable-"+pluginID, theme.Tr("启用", "Enable"), nil, bui.ButtonSecondary, false)
				if btn.Clicked() {
					c.sup.SetPluginEnabled(pluginID, true)
				}
			})
		})
		return
	}

	vs := c.sup.EnsurePluginView(pluginID)
	pal := theme.Ocean()

	ui.Column(ctx.Key("plugin-tab-body-" + pluginID)).FillWidth().MinWidth(0).Gap(theme.Space16).Children(func() {
		if vs.Error != "" {
			ui.Row(ctx.Key("plugin-error-"+pluginID)).FillWidth().MinWidth(0).
				Padding(theme.Space10, theme.Space12).Gap(theme.Space12).Radius(theme.RadiusMD).
				Background(pal.DangerSurface).Border(1, pal.Danger).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(ctx, vs.Error).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					TextColor(pal.Danger).Grow(1).MinWidth(0)
				ui.Row(ctx.Key("plugin-error-actions-" + pluginID)).Gap(theme.Space8).AlignItems(ui.Center).Shrink(0).Children(func() {
					busy := vs.Loading || vs.BusyAction != ""
					label := theme.Tr("重试", "Retry")
					if vs.RetryKind == plugins.RetryKindReload {
						label = theme.Tr("重新加载", "Reload")
					} else if pluginID == "block-sync" && (vs.RetryKind == plugins.RetryKindNoteSaved || vs.FailedActionID == "sync_now") {
						label = theme.Tr("重试同步", "Retry sync")
					}
					retryBtn := bui.ActionButton(ctx, "plugin-retry-"+pluginID, label, nil, bui.ButtonSecondary, busy)
					if retryBtn.Clicked() {
						if vs.RetryKind == plugins.RetryKindUninstall {
							c.uninstallPlugin(pluginID, func() {
								if onSelectTab != nil {
									onSelectTab(modals.TabAppStore)
								}
							})
						} else {
							c.sup.RetryPluginAsync(pluginID)
						}
					}
					if vs.RetryKind != plugins.RetryKindNone && vs.RetryKind != plugins.RetryKindReload {
						reloadBtn := bui.ActionButton(ctx, "plugin-reload-"+pluginID, theme.Tr("重新加载", "Reload"), nil, bui.ButtonSecondary, busy)
						if reloadBtn.Clicked() {
							c.sup.ReloadPluginViewAsync(pluginID)
						}
					}
				})
			})
		}

		if vs.Loading && vs.Page == nil {
			ctx.Key("plugin-loading-" + pluginID)
			bui.Card(ctx, func() {
				ui.Row(ctx).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
					icons.Draw(ctx, icons.Spinner, theme.IconAction, pal.DuoIconColors(pal.Accent))
					ui.Text(ctx, theme.Tr("正在加载插件页面…", "Loading plugin page…")).
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
				})
			})
			return
		}

		if vs.Page != nil {
			if vs.Page.RefreshIntervalMs > 0 {
				c.sup.ScheduleLiveRefresh(pluginID, vs.Page.RefreshIntervalMs)
			}
			renderPageCards(
				ctx,
				pluginID,
				vs.Page,
				vs.Values,
				vs.BusyAction,
				c.inputBindingsFor(pluginID),
				func(controlID string, nextValue any, immediate bool) {
					c.sup.UpdateControlValue(pluginID, controlID, nextValue, immediate)
				},
				func(actionID string, _ map[string]any) {
					c.sup.TriggerActionAsync(pluginID, actionID)
				},
			)
		}
	})
}

func (c *Controller) storeFilterValue() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.storeFilter
}

func (c *Controller) setStoreFilter(v int) {
	c.mu.Lock()
	c.storeFilter = v
	c.mu.Unlock()
}

func (c *Controller) manualSpecValue() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.manualSpec
}

func (c *Controller) setManualSpec(v string) {
	c.mu.Lock()
	c.manualSpec = v
	c.mu.Unlock()
}

func (c *Controller) clearManualSpec() {
	c.mu.Lock()
	c.manualSpec = ""
	c.mu.Unlock()
}

func (c *Controller) uninstallPlugin(pluginID string, onDone func()) {
	c.sup.UninstallAsync(pluginID, func() {
		c.clearPluginState(pluginID)
		if onDone != nil {
			onDone()
		}
	})
}

func (c *Controller) pluginStateKey(pluginID string) string {
	nbRoot := ""
	if c.sup != nil {
		nbRoot = c.sup.NotebookRoot()
	}
	return nbRoot + "\x00" + pluginID
}

func (c *Controller) clearPluginState(pluginID string) {
	suffix := "\x00" + pluginID
	c.mu.Lock()
	for k := range c.drafts {
		if strings.HasSuffix(k, suffix) {
			delete(c.drafts, k)
		}
	}
	for k := range c.lastSynced {
		if strings.HasSuffix(k, suffix) {
			delete(c.lastSynced, k)
		}
	}
	for k := range c.secretRevealed {
		if strings.HasSuffix(k, suffix) {
			delete(c.secretRevealed, k)
		}
	}
	for k := range c.selectStates {
		if strings.HasSuffix(k, suffix) {
			delete(c.selectStates, k)
		}
	}
	for k := range c.treemapStates {
		if strings.HasSuffix(k, suffix) {
			delete(c.treemapStates, k)
		}
	}
	c.mu.Unlock()
}

func (c *Controller) inputBindingsFor(pluginID string) *InputBindings {
	var pickDirectory func(controlID, title string)
	if c.sup.CanPickDirectory() {
		pickDirectory = func(controlID, title string) {
			c.sup.PickDirectoryForControl(pluginID, controlID, title)
		}
	}
	return &InputBindings{
		PickDirectory: pickDirectory,
		TreemapState: func(controlID string) *bui.TreemapState {
			stateKey := c.pluginStateKey(pluginID)
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.treemapStates[stateKey] == nil {
				c.treemapStates[stateKey] = make(map[string]*bui.TreemapState)
			}
			st := c.treemapStates[stateKey][controlID]
			if st == nil {
				st = &bui.TreemapState{}
				c.treemapStates[stateKey][controlID] = st
			}
			return st
		},
		OnTreemapSelect: func() {
			c.sup.NotifyRedraw()
		},
		SelectState: func(controlID string) *bui.DropdownSelectState {
			stateKey := c.pluginStateKey(pluginID)
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.selectStates[stateKey] == nil {
				c.selectStates[stateKey] = make(map[string]*bui.DropdownSelectState)
			}
			st := c.selectStates[stateKey][controlID]
			if st == nil {
				st = &bui.DropdownSelectState{Cursor: -1}
				c.selectStates[stateKey][controlID] = st
			}
			return st
		},
		ResolveDraft: func(controlID, fallback string) string {
			stateKey := c.pluginStateKey(pluginID)
			c.mu.Lock()
			defer c.mu.Unlock()
			pDrafts := c.drafts[stateKey]
			if pDrafts == nil {
				pDrafts = make(map[string]string)
				c.drafts[stateKey] = pDrafts
			}
			pSynced := c.lastSynced[stateKey]
			if pSynced == nil {
				pSynced = make(map[string]string)
				c.lastSynced[stateKey] = pSynced
			}
			prevSynced, hasSynced := pSynced[controlID]
			if !hasSynced || fallback != prevSynced {
				pSynced[controlID] = fallback
				pDrafts[controlID] = fallback
				return fallback
			}
			return pDrafts[controlID]
		},
		OnDraftChanged: func(controlID, next string) {
			stateKey := c.pluginStateKey(pluginID)
			c.mu.Lock()
			if c.drafts[stateKey] == nil {
				c.drafts[stateKey] = make(map[string]string)
			}
			if c.lastSynced[stateKey] == nil {
				c.lastSynced[stateKey] = make(map[string]string)
			}
			c.drafts[stateKey][controlID] = next
			c.lastSynced[stateKey][controlID] = next
			c.mu.Unlock()
		},
		IsSecretShown: func(controlID string) bool {
			stateKey := c.pluginStateKey(pluginID)
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.secretRevealed[stateKey][controlID]
		},
		ToggleSecret: func(controlID string) {
			stateKey := c.pluginStateKey(pluginID)
			c.mu.Lock()
			if c.secretRevealed[stateKey] == nil {
				c.secretRevealed[stateKey] = make(map[string]bool)
			}
			c.secretRevealed[stateKey][controlID] = !c.secretRevealed[stateKey][controlID]
			c.mu.Unlock()
		},
	}
}
