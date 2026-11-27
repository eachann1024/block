package pluginui

import (
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

	mu                  sync.Mutex
	storeQuery          string
	storeSource         string
	storeSort           string
	storeFilterOpen     bool
	storeFilterAnchor   ui.Rect
	storeFilterCursor   int
	storeSearchFocused  bool
	storeCategory       plugins.Category
	storeBaseSnap       *storeBaseSnapshot
	storeViewSnap       *storeViewSnapshot
	storeHeroIndex      int
	storeHeroDir        int
	storeHeroLastSwitch time.Time
	storeHeroElapsed    time.Duration
	storeHeroPaused     bool
	onSuggestPlugin     func(category plugins.Category)
	onOpenStore         func()
	isStoreVisibleFn    func() bool
	nextPromptID        uint64
	permQueue           []pendingPermissionPrompt
	manualSpec          string
	drafts              map[string]map[string]string
	lastSynced          map[string]map[string]string
	secretRevealed      map[string]map[string]bool
	selectStates        map[string]map[string]*bui.DropdownSelectState
	treemapStates       map[string]map[string]*bui.TreemapState
	cardExpanded        map[string]map[string]bool

	titlebarMenuOpen    bool
	titlebarMenuTarget  string
	titlebarMenuAnchor  ui.Rect
	titlebarMenuCursor  int
	titlebarErrOpen     bool
	titlebarErrPluginID string
	titlebarErrNotebook string
	titlebarErrAnchor   ui.Rect
	titlebarErrScroll   ui.ScrollState
	titlebarErrCopiedAt time.Time
	activeSettingsTab   string
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

// SettingsTabChanged follows settings navigation. Opening or closing a detail
// stays within the store tab and therefore does not reload the catalog.
func (c *Controller) SettingsTabChanged(previous, current string) {
	c.activeSettingsTab = current
	if c.sup == nil {
		return
	}
	c.CloseTitlebarOverlays()
	if modals.IsStoreTab(previous) {
		c.closeStoreReadme()
	}
	if modals.IsStoreTab(current) {
		c.setStoreCategory("")
	}
	c.closeStoreMenus()
	if modals.IsStoreTab(current) && !modals.IsStoreTab(previous) {
		c.storeHeroIndex = 0
		c.storeHeroDir = 1
		c.storeHeroPaused = false
		c.storeHeroLastSwitch = time.Time{}
		c.storeHeroElapsed = 0
		c.sup.RefreshCatalogAsync()
	}
}

func (c *Controller) selectStoreHero(index, dir int, manual bool, now time.Time) {
	if dir == 0 {
		dir = 1
	}
	c.storeHeroIndex = index
	c.storeHeroDir = dir
	c.storeHeroLastSwitch = now
	c.storeHeroElapsed = 0
	if manual {
		c.storeHeroPaused = true
	}
}

func (c *Controller) StoreListing(tab string) bool {
	return c.sup != nil && modals.IsStoreTab(tab) && !c.storeDetailOpen()
}

// IsStoreLightboxOpen reports whether the App Store README full-window media lightbox is open.
func (c *Controller) IsStoreLightboxOpen() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	preview := c.storeReadme
	c.mu.Unlock()
	return preview != nil && preview.lightbox.Open
}

func (c *Controller) RenderStoreFooter(ctx *ui.Context) {
	_, installingID, _, _ := c.sup.StoreStatus()
	renderManualInstallRow(ctx, c, installingID)
}

func (c *Controller) closeStoreMenus() {
	c.storeFilterOpen = false
}

// InstalledCount returns the number of installed plugins that provide a Settings page.
func (c *Controller) InstalledCount() int {
	if c.sup == nil {
		return 0
	}
	n := 0
	for _, inst := range c.sup.Installed() {
		if inst.Manifest.ID == plugins.DiagnosticsPluginID || !inst.Manifest.HasSettingsPage() {
			continue
		}
		n++
	}
	return n
}

// CommandShortcuts returns enabled plugin commands for shortcut customization.
// Passing an empty pluginID returns commands from every enabled plugin.
func (c *Controller) CommandShortcuts(pluginID string) []modals.PluginCommandShortcut {
	if c.sup == nil {
		return nil
	}
	cmds := c.sup.Commands()
	if len(cmds) == 0 {
		return nil
	}
	out := make([]modals.PluginCommandShortcut, 0, len(cmds))
	for _, cmd := range cmds {
		if pluginID != "" && cmd.PluginID != pluginID {
			continue
		}
		out = append(out, modals.PluginCommandShortcut{
			PluginID:        cmd.PluginID,
			PluginName:      cmd.PluginName,
			CommandID:       cmd.ID,
			Title:           cmd.Title,
			DefaultShortcut: cmd.Shortcut,
		})
	}
	return out
}

// NavEntries returns the lower-partition Settings sidebar entries: App Store followed by installed plugins with Settings pages.
func (c *Controller) NavEntries() []modals.PluginNavEntry {
	storeSpec := plugins.AppStoreIconSpec()
	var installed []plugins.InstalledApp
	if c.sup != nil {
		installed = c.sup.Installed()
	}
	entries := []modals.PluginNavEntry{
		{
			TabID: modals.TabAppStore,
			Title: theme.Tr("应用商店", "App Store"),
			DrawIcon: func(ctx *ui.Context, size float32, colors theme.IconColors, active bool) {
				DrawDuoSpecAnimated(ctx, storeSpec, size, colors, active)
			},
		},
	}
	if c.sup == nil {
		return entries
	}
	locale := c.sup.Locale()
	for _, inst := range installed {
		if inst.Manifest.ID == plugins.DiagnosticsPluginID || !inst.Manifest.HasSettingsPage() {
			continue
		}
		app := inst
		hasErr := c.sup.HasPluginError(app.Manifest.ID)
		lit := app.Enabled && !hasErr
		_, starting, _, _ := c.pluginVisualState(app.Manifest.ID, app)
		disabledLabel := ""
		if app.NeedsUpdate {
			disabledLabel = pluginUpdateRequiredLabel()
		}
		entries = append(entries, modals.PluginNavEntry{
			TabID:         modals.TabPluginPrefix + app.Manifest.ID,
			Title:         app.Manifest.Name.Resolve(locale),
			Beta:          app.Manifest.Beta,
			Lit:           lit,
			Starting:      starting,
			StatusDot:     app.Enabled,
			HasError:      hasErr,
			DisabledLabel: disabledLabel,
			DrawIcon: func(ctx *ui.Context, size float32, colors theme.IconColors, active bool) {
				DrawDuoSpecAnimated(ctx, app.Manifest.Icon, size, colors, active)
			},
		})
	}
	return entries
}

// TabTitle returns the localized header title for the App Store or an installed plugin tab.
func (c *Controller) TabTitle(tab string) string {
	if modals.IsStoreTab(tab) {
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

// pluginVisualState resolves the master switch and header icon states for one installed plugin.
func (c *Controller) pluginVisualState(pluginID string, inst plugins.InstalledApp) (vs plugins.PluginViewState, starting, failed, live bool) {
	declarative := inst.Manifest.IsDeclarativeOnly()
	if inst.Enabled && !declarative {
		vs = c.sup.EnsurePluginView(pluginID)
	}
	starting = inst.Enabled && !declarative && vs.Loading && vs.Page == nil
	failed = inst.Enabled && !declarative && !vs.Loading && vs.Error != "" && vs.Page == nil
	live = inst.Enabled && (declarative || (!starting && !failed))
	return vs, starting, failed, live
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

// RenderHeaderExtras renders the trailing controls in the Settings body header
// for App Store listing (search/source/sort) or standalone Settings fallback.
func (c *Controller) RenderHeaderExtras(ctx *ui.Context, tab string, onSelectTab func(nextTab string)) {
	if c.sup == nil {
		return
	}
	if modals.IsStoreTab(tab) {
		if c.storeDetailOpen() {
			renderStoreDetailBack(ctx, c)
		} else {
			renderStoreHeaderExtras(ctx, c)
		}
		return
	}
	c.closeStoreReadme()
	c.RenderTitlebarTrailing(ctx, tab, onSelectTab)
}

// RenderTabBody renders the main body of the App Store or an installed plugin Settings page.
func (c *Controller) RenderTabBody(ctx *ui.Context, tab string, onSelectTab func(nextTab string)) {
	if c.sup == nil {
		return
	}
	if modals.IsStoreTab(tab) {
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
	if !inst.Manifest.HasSettingsPage() {
		if onSelectTab != nil {
			c.openInstalledPluginReadme(inst)
			onSelectTab(modals.TabAppStore)
			return
		}
		renderPluginDormantOverview(ctx, c, inst)
		return
	}
	if !inst.Enabled {
		renderPluginDormantOverview(ctx, c, inst)
		return
	}

	vs := c.sup.EnsurePluginView(pluginID)

	ui.Column(ctx.Key("plugin-tab-body-" + pluginID)).FillWidth().MinWidth(0).Gap(theme.SettingsSectionGap).Children(func() {
		if vs.Page == nil {
			renderPluginDormantOverview(ctx, c, inst)
			return
		}

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
	})
}

func pluginUpdateRequiredLabel() string {
	return theme.Tr("需要更新", "Update required")
}

func renderPluginUpdateRequired(ctx *ui.Context) {
	ui.Text(ctx, pluginUpdateRequiredLabel()).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
		TextColor(theme.Ocean().Warning).SingleLine().Shrink(0)
}

func renderPluginDormantOverview(ctx *ui.Context, ctrl *Controller, inst plugins.InstalledApp) {
	pal := theme.Ocean()
	locale := ctrl.sup.Locale()
	summary := strings.TrimSpace(inst.Manifest.Summary.Resolve(locale))
	category := plugins.CategoryLabel(inst.Manifest.EffectiveCategory(), locale)
	source := strings.TrimSpace(inst.Manifest.Author)
	if catalog := ctrl.sup.Catalog(); catalog != nil {
		for _, entry := range catalog.Apps {
			if entry.ID == inst.Manifest.ID {
				if storeAppSource(ctrl, entry, inst, true) == storeSourceOfficial {
					source = theme.Tr("Block 官方", "Official by Block")
				}
				break
			}
		}
	}
	if source == "" {
		source = theme.Tr("本地安装", "Local")
	}
	version := strings.TrimSpace(inst.Manifest.Version)

	ui.Column(ctx.Key("plugin-dormant-wrap-" + inst.Manifest.ID)).FillWidth().MinWidth(0).
		Gap(theme.SettingsSectionGap).Transition(bui.SurfaceStageTransition()).Children(func() {
		if !inst.Enabled {
			appName := strings.TrimSpace(inst.Manifest.Name.Resolve(locale))
			bannerTitle := theme.Tr("插件已停用", "Plugin disabled")
			if appName != "" {
				bannerTitle = theme.Tr(appName+"已停用", appName+" is disabled")
			}
			if inst.NeedsUpdate {
				bannerTitle = pluginUpdateRequiredLabel()
			}
			ui.Row(ctx.Key("plugin-disabled-banner-"+inst.Manifest.ID)).FillWidth().MinWidth(0).
				Padding(theme.CardPadY, theme.CardPadX).Gap(theme.Space12).AlignItems(ui.Center).
				Radius(theme.RadiusXL).Background(pal.WarningSurface).Border(1, pal.Warning).Children(func() {
				ui.Box(ctx).Width(theme.HeightLarge).Height(theme.HeightLarge).Shrink(0).
					Radius(theme.RadiusLG).Background(pal.Card).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					icons.Draw(ctx, icons.PauseCircle, theme.IconRow, pal.TintedIconColors(pal.Warning))
				})
				ui.Column(ctx).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
					ui.Text(ctx, bannerTitle).FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
						FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine().Ellipsis("…")
					if !inst.NeedsUpdate {
						ui.Text(ctx, theme.Tr("启用后即可使用插件功能与设置项", "Enable to use this plugin's features and settings")).
							FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
					}
				})
				if inst.NeedsUpdate {
					entry := ctrl.findCatalogEntry(inst.Manifest.ID)
					if entry != nil && storeEntryHasUpdate(*entry, inst) {
						_, installingID, _, _ := ctrl.sup.StoreStatus()
						renderStoreCardAction(ctx, ctrl, storeCard{entry: *entry, installed: inst, isInstalled: true},
							installingID == entry.ID, false, installingID, nil)
					}
					return
				}
				enableBtn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
					ID:     "plugin-dormant-enable-" + inst.Manifest.ID,
					Label:  theme.Tr("立即启用", "Enable"),
					Style:  bui.ButtonPrimary,
					Height: theme.HeightControl,
				})
				if enableBtn.Clicked() {
					ctrl.sup.SetPluginEnabled(inst.Manifest.ID, true)
				}
			})
		}

		ui.Column(ctx.Key("plugin-dormant-"+inst.Manifest.ID)).FillWidth().MinWidth(0).
			Padding(theme.CardPadY, theme.CardPadX).Gap(theme.Space12).
			Radius(theme.RadiusXL).Background(pal.Card).Border(1, pal.Hairline).Children(func() {
			if summary != "" {
				ui.Text(ctx, summary).FontSize(theme.FontBody).FixedLineHeight(theme.LineTitle).TextColor(pal.Text)
				ui.Box(ctx).FillWidth().Height(1).Background(pal.Hairline)
			}
			metaRow := func(k, v string) {
				if strings.TrimSpace(v) == "" {
					return
				}
				ui.Row(ctx).FillWidth().MinWidth(0).Gap(theme.Space16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(ctx, k).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).Shrink(0)
					ui.Text(ctx, v).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
				})
			}
			metaRow(theme.Tr("分类", "Category"), category)
			metaRow(theme.Tr("来源", "Source"), source)
			if version != "" {
				metaRow(theme.Tr("版本", "Version"), "v"+strings.TrimPrefix(version, "v"))
			}
		})
	})
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

// SetOnSuggestPlugin registers the host callback triggered by the "Suggest a plugin"
// action in App Store empty states (typically opening the Feedback modal).
func (c *Controller) SetOnSuggestPlugin(fn func(category plugins.Category)) {
	c.mu.Lock()
	c.onSuggestPlugin = fn
	c.mu.Unlock()
}

func (c *Controller) suggestPlugin(cat plugins.Category) {
	c.mu.Lock()
	fn := c.onSuggestPlugin
	c.mu.Unlock()
	if fn != nil {
		fn(cat)
	}
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
	var pickChecklistFolder func(controlID, title string)
	if c.sup.CanPickDirectory() {
		pickChecklistFolder = func(controlID, title string) {
			c.sup.PickChecklistFolderForControl(pluginID, surface, controlID, title)
		}
	}
	return &InputBindings{
		PickDirectory:       pickDirectory,
		PickFile:            pickFile,
		PickChecklistFolder: pickChecklistFolder,
		SetInputFocus:       c.sup.SurfaceInputFocus(pluginID, surface),
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
