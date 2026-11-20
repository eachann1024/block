package pluginui

import (
	"strconv"
	"strings"
	"sync"
	"time"

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
	sup         *plugins.Supervisor
	storeReadme *storeReadmePreview

	mu             sync.Mutex
	storeFilter    int
	storeCategory  plugins.Category
	manualSpec     string
	manualOpen     bool
	drafts         map[string]map[string]string
	lastSynced     map[string]map[string]string
	secretRevealed map[string]map[string]bool
	selectStates   map[string]map[string]*bui.DropdownSelectState
	treemapStates  map[string]map[string]*bui.TreemapState
	cardExpanded   map[string]map[string]bool
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
		cardExpanded:   make(map[string]map[string]bool),
	}
}

// Supervisor returns the underlying plugin Supervisor.
func (c *Controller) Supervisor() *plugins.Supervisor {
	return c.sup
}

// OpenStoreSource presents a linked source for review without installing it.
func (c *Controller) OpenStoreSource(source string) {
	if c.sup == nil {
		return
	}
	if source == "" {
		c.closeStoreReadme()
		return
	}
	repo := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(source, "github:"), "https://github.com/"), "/")
	if catalog := c.sup.Catalog(); source != "" && catalog != nil {
		for _, entry := range catalog.Apps {
			if source == entry.ID || (entry.Repo != "" && strings.EqualFold(repo, entry.Repo)) || source == entry.DownloadURL {
				installed, _ := c.sup.GetInstalled(entry.ID)
				c.openStoreReadme(entry, installed)
				return
			}
		}
	}
	c.closeStoreReadme()
}

// NavEntries returns the lower-partition Settings sidebar entries: App Store followed by installed plugins.
func (c *Controller) NavEntries() []modals.PluginNavEntry {
	c.closeStoreReadmeIfNotRenderedRecently(2 * time.Second)
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
		if inst.Manifest.ID == plugins.DiagnosticsPluginID {
			continue
		}
		app := inst
		entries = append(entries, modals.PluginNavEntry{
			TabID:     modals.TabPluginPrefix + app.Manifest.ID,
			Title:     app.Manifest.Name.Resolve(locale),
			Beta:      app.Manifest.Beta,
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

// TabIsBeta reports whether the given Settings tab is a plugin declaring beta=true in its manifest.
func (c *Controller) TabIsBeta(tab string) bool {
	pluginID, ok := strings.CutPrefix(tab, modals.TabPluginPrefix)
	if !ok || pluginID == "" || c.sup == nil {
		return false
	}
	if inst, found := c.sup.GetInstalled(pluginID); found {
		return inst.Manifest.Beta
	}
	return false
}

// RenderHeaderExtras renders the trailing controls in the Settings top header for App Store or plugin tabs.
func (c *Controller) RenderHeaderExtras(ctx *ui.Context, tab string, onSelectTab func(nextTab string)) {
	if c.sup == nil {
		return
	}
	if tab == modals.TabAppStore {
		if c.storeDetailOpen() {
			renderStoreDetailBack(ctx, c)
		} else {
			renderStoreHeaderExtras(ctx, c)
		}
		return
	}
	c.closeStoreReadme()
	pluginID, ok := strings.CutPrefix(tab, modals.TabPluginPrefix)
	if !ok || pluginID == "" {
		return
	}
	inst, ok := c.sup.GetInstalled(pluginID)
	if !ok {
		return
	}

	var vs plugins.PluginViewState
	declarative := inst.Manifest.IsDeclarativeOnly()
	if inst.Enabled && !declarative {
		vs = c.sup.EnsurePluginView(pluginID)
	}
	pal := theme.Ocean()
	fb := actionFeedbackFromView(vs)
	ui.Row(ctx.Key("plugin-header-extras-" + pluginID)).Gap(theme.Space10).AlignItems(ui.Center).MinWidth(0).Shrink(1).Children(func() {
		if declarative {
			if inst.Enabled {
				renderStatusText(ctx, theme.Tr("已启用", "Enabled"))
			} else {
				renderStatusText(ctx, theme.Tr("已停用", "Disabled"))
			}
		} else if inst.Enabled {
			if vs.Refreshing && vs.BusyAction == "" {
				ui.Spinner(ctx).FontSize(theme.IconInline).TextColor(pal.Accent).Label(theme.Tr("更新中", "Refreshing"))
			}
			headerAct := isHeaderActionID(vs.Page, vs.LastActionID)
			cardAct := vs.LastActionID != "" && !headerAct && isCardActionID(vs.Page, vs.LastActionID)
			statusText := ""
			statusTone := ""
			if headerAct && strings.TrimSpace(vs.LastActionMessage) != "" {
				statusText = strings.TrimSpace(vs.LastActionMessage)
				statusTone = vs.LastActionTone
			} else if !cardAct {
				statusText = strings.TrimSpace(vs.Notice)
				if statusText == "" && vs.Page != nil {
					statusText = strings.TrimSpace(vs.Page.Status)
				}
			} else if vs.Page != nil {
				pageSt := strings.TrimSpace(vs.Page.Status)
				if pageSt != "" && pageSt != strings.TrimSpace(vs.LastActionMessage) {
					statusText = pageSt
				}
			}
			if vs.PendingSaves > 0 {
				statusText = strconv.Itoa(vs.PendingSaves) + theme.Tr(" 项待处理", " pending")
				statusTone = "info"
			}
			if statusText != "" {
				renderStatusTextWithTone(ctx, statusText, statusTone)
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
				el := renderPluginActionButton(ctx, "plugin-hdr-act-", pluginID, b, fb)
				if el.Clicked() && vs.BusyAction == "" {
					c.sup.TriggerActionAsync(pluginID, b.ID)
				}
			}
		}
		if inst.Enabled && c.sup.CanCancelAction(pluginID) {
			stopIc := icons.Stop
			cancel := bui.ActionButton(ctx, "plugin-cancel-task-"+pluginID, theme.Tr("取消任务", "Cancel task"), &stopIc, bui.ButtonGhost, false)
			if cancel.Clicked() {
				c.sup.CancelAction(pluginID)
			}
		}

		uninstBusy := vs.BusyAction == "__uninstall__"
		uninstLbl := theme.Tr("卸载", "Uninstall")
		if uninstBusy {
			uninstLbl = theme.Tr("卸载中…", "Uninstalling…")
		}
		trashIc := icons.Trash
		uninstBtn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
			ID:       "plugin-hdr-uninstall-" + pluginID,
			Label:    uninstLbl,
			Leading:  &trashIc,
			Style:    bui.ButtonDanger,
			Disabled: vs.Loading || (vs.BusyAction != "" && !uninstBusy),
			Loading:  uninstBusy,
		})
		if uninstBtn.Clicked() && !vs.Loading && vs.BusyAction == "" {
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
		c.sup.MaybeRefreshCatalogAsync(5 * time.Minute)
		if c.storeDetailOpen() {
			renderStoreDetail(ctx, c, onSelectTab)
			return
		}
		renderStoreBody(ctx, c, onSelectTab)
		return
	}
	c.closeStoreReadme()
	pluginID, ok := strings.CutPrefix(tab, modals.TabPluginPrefix)
	if !ok || pluginID == "" {
		return
	}
	inst, isInstalled := c.sup.GetInstalled(pluginID)
	if !isInstalled {
		pal := theme.Ocean()
		ctx.Key("plugin-not-installed-" + pluginID)
		bui.SettingsSection(ctx, func() {
			bui.SettingsRowFrame(ui.Row(ctx)).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(ctx, theme.Tr("该插件尚未安装", "This plugin is not installed")).
					FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
				pkgIc := icons.Package
				btn := bui.ActionButton(ctx, "plugin-go-store-"+pluginID, theme.Tr("前往应用商店", "Go to App Store"), &pkgIc, bui.ButtonSecondary, false)
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
		bui.SettingsSection(ctx, func() {
			bui.SettingsRowFrame(ui.Row(ctx)).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(ctx, theme.Tr("已停用", "Disabled")).
					FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
				chkIc := icons.Check
				btn := bui.ActionButton(ctx, "plugin-enable-"+pluginID, theme.Tr("启用", "Enable"), &chkIc, bui.ButtonSecondary, false)
				if btn.Clicked() {
					c.sup.SetPluginEnabled(pluginID, true)
				}
			})
		})
		return
	}
	if inst.Manifest.IsDeclarativeOnly() {
		renderDeclarativeLanguageTab(ctx, c, inst)
		return
	}

	vs := c.sup.EnsurePluginView(pluginID)
	pal := theme.Ocean()

	ui.Column(ctx.Key("plugin-tab-body-" + pluginID)).FillWidth().MinWidth(0).Gap(theme.SettingsSectionGap).Children(func() {
		if vs.Error != "" {
			ui.Row(ctx.Key("plugin-error-"+pluginID)).FillWidth().MinWidth(0).
				Padding(theme.Space10, theme.Space12).Gap(theme.Space12).Radius(theme.RadiusMD).
				Background(pal.DangerSurface).Border(1, pal.Danger.Alpha(0.35)).AlignItems(ui.Center).Justify(ui.SpaceBetween).
				Transition(bui.CascadeRowTransition(0)).Children(func() {
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
					if busy {
						label = theme.Tr("重试中…", "Retrying…")
					}
					refIc := icons.Refresh
					retryBtn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
						ID:       "plugin-retry-" + pluginID,
						Label:    label,
						Leading:  &refIc,
						Style:    bui.ButtonSecondary,
						Disabled: busy,
						Loading:  busy,
					})
					if retryBtn.Clicked() && !busy {
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
						reloadIc := icons.Refresh
						reloadBtn := bui.ActionButton(ctx, "plugin-reload-"+pluginID, theme.Tr("重新加载", "Reload"), &reloadIc, bui.ButtonSecondary, busy)
						if reloadBtn.Clicked() && !busy {
							c.sup.ReloadPluginViewAsync(pluginID)
						}
					}
				})
			})
		}

		if vs.Loading && vs.Page == nil {
			ctx.Key("plugin-loading-" + pluginID)
			bui.SettingsSection(ctx, func() {
				msg := theme.Tr("正在加载插件页面…", "Loading plugin page…")
				bui.SettingsRowFrame(ui.Row(ctx)).Gap(theme.Space8).Children(func() {
					ui.Spinner(ctx).FontSize(theme.IconAction).TextColor(pal.Accent).Label(msg)
					ui.Text(ctx, msg).
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
				actionFeedbackFromView(vs),
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

func (c *Controller) storeCategoryValue() plugins.Category {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.storeCategory
}

func (c *Controller) setStoreCategory(v plugins.Category) {
	c.mu.Lock()
	c.storeCategory = v
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

func (c *Controller) manualOpenValue() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.manualOpen || c.manualSpec != ""
}

func (c *Controller) toggleManualOpen() {
	c.mu.Lock()
	c.manualOpen = !(c.manualOpen || c.manualSpec != "")
	if !c.manualOpen {
		c.manualSpec = ""
	}
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
		if onDone == nil {
			return
		}
		// UninstallAsync finishes on a background goroutine; Settings state
		// (selected tab, shortcut recording) is owned by the main thread.
		if mygo.App.IsReady() {
			mygo.RunOnMain(onDone)
		} else {
			onDone()
		}
	})
}

func (c *Controller) pluginStateKey(pluginID string) string {
	return c.surfaceStateKey(pluginID, plugins.SurfaceSettingsPage)
}

func (c *Controller) surfaceStateKey(pluginID string, surface plugins.Surface) string {
	nbRoot := ""
	if c.sup != nil {
		nbRoot = c.sup.NotebookRoot()
	}
	if surface == "" || surface == plugins.SurfaceSettingsPage {
		return nbRoot + "\x00" + pluginID
	}
	return nbRoot + "\x00" + string(surface) + "\x00" + pluginID
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
	for k := range c.cardExpanded {
		if strings.HasSuffix(k, suffix) {
			delete(c.cardExpanded, k)
		}
	}
	c.mu.Unlock()
}

func (c *Controller) inputBindingsFor(pluginID string) *InputBindings {
	return c.inputBindingsForSurface(pluginID, plugins.SurfaceSettingsPage)
}

func (c *Controller) inputBindingsForSurface(pluginID string, surface plugins.Surface) *InputBindings {
	var pickDirectory func(controlID, title string)
	if surface == "" {
		surface = plugins.SurfaceSettingsPage
	}
	if c.sup.CanPickDirectory() {
		pickDirectory = func(controlID, title string) {
			c.sup.PickSurfaceDirectoryForControl(pluginID, surface, controlID, title)
		}
	}
	var pickFile func(controlID, title string, extensions []string)
	if c.sup.CanPickFile() {
		pickFile = func(controlID, title string, extensions []string) {
			c.sup.PickSurfaceFileForControl(pluginID, surface, controlID, title, extensions)
		}
	}
	stateKey := c.surfaceStateKey(pluginID, surface)
	return &InputBindings{
		PickDirectory: pickDirectory,
		PickFile:      pickFile,
		IsCardExpanded: func(cardID string, defaultCollapsed bool) bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.cardExpanded[stateKey] == nil {
				c.cardExpanded[stateKey] = make(map[string]bool)
			}
			if expanded, ok := c.cardExpanded[stateKey][cardID]; ok {
				return expanded
			}
			initial := !defaultCollapsed
			c.cardExpanded[stateKey][cardID] = initial
			return initial
		},
		ToggleCardExpanded: func(cardID string, defaultCollapsed bool) {
			c.mu.Lock()
			if c.cardExpanded[stateKey] == nil {
				c.cardExpanded[stateKey] = make(map[string]bool)
			}
			cur := !defaultCollapsed
			if prev, ok := c.cardExpanded[stateKey][cardID]; ok {
				cur = prev
			}
			c.cardExpanded[stateKey][cardID] = !cur
			c.mu.Unlock()
			c.sup.NotifyRedraw()
		},
		TreemapState: func(controlID string) *bui.TreemapState {
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
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.secretRevealed[stateKey][controlID]
		},
		ToggleSecret: func(controlID string) {
			c.mu.Lock()
			if c.secretRevealed[stateKey] == nil {
				c.secretRevealed[stateKey] = make(map[string]bool)
			}
			c.secretRevealed[stateKey][controlID] = !c.secretRevealed[stateKey][controlID]
			c.mu.Unlock()
		},
	}
}
