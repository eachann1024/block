package pluginui

import (
	"fmt"
	"strings"

	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

type pendingPermissionPrompt struct {
	id             uint64
	pluginID       string
	name           plugins.LocalizedText
	icon           plugins.DuoIconSpec
	author         string
	currentVersion string
	nextVersion    string
	isUpdate       bool
	sourceChanged  bool
	requested      []plugins.Permission
	existing       []plugins.Permission
	allNext        []plugins.Permission
	reasons        map[plugins.Permission]plugins.LocalizedText
	entry          *plugins.StoreAppEntry
	staged         *plugins.StagedPlugin
	onInstalled    func(pluginID string)
}

// SetOnOpenStore registers a host callback that navigates to the App Store tab.
func (c *Controller) SetOnOpenStore(fn func()) {
	c.mu.Lock()
	c.onOpenStore = fn
	c.mu.Unlock()
}

// SetIsStoreVisible registers a host predicate reporting whether the App Store
// view is currently open and visible to the user.
func (c *Controller) SetIsStoreVisible(fn func() bool) {
	c.mu.Lock()
	c.isStoreVisibleFn = fn
	c.mu.Unlock()
}

func (c *Controller) openStore() {
	c.mu.Lock()
	fn := c.onOpenStore
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (c *Controller) isStoreVisible() bool {
	c.mu.Lock()
	fn := c.isStoreVisibleFn
	c.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return false
}

// HasPermissionModal reports whether a plugin install/update permission
// confirmation dialog is currently active.
func (c *Controller) HasPermissionModal() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	n := len(c.permQueue)
	c.mu.Unlock()
	return n > 0
}

// ClearPermissionPrompts discards all queued permission prompts and cleans up
// any staged manual packages.
func (c *Controller) ClearPermissionPrompts() {
	if c == nil {
		return
	}
	c.mu.Lock()
	queue := c.permQueue
	c.permQueue = nil
	c.mu.Unlock()
	for _, p := range queue {
		if p.staged != nil {
			p.staged.Cleanup()
		}
	}
}

// RequestInstallEntry installs or updates a catalog entry, prompting the user
// first when a fresh install requests sensitive permissions or an update adds
// new sensitive permissions.
func (c *Controller) RequestInstallEntry(entry plugins.StoreAppEntry, onInstalled func(pluginID string)) {
	if c == nil || c.sup == nil {
		return
	}
	installed, isInstalled := c.sup.GetInstalled(entry.ID)
	if !isInstalled {
		sensitive := plugins.SensitivePermissions(entry.Permissions)
		if len(sensitive) == 0 {
			c.sup.InstallEntryConfirmedAsync(entry, nil, false, onInstalled)
			return
		}
		entryCopy := entry
		c.enqueuePermissionPrompt(pendingPermissionPrompt{
			pluginID:    entry.ID,
			name:        entry.Name,
			icon:        entry.Icon,
			author:      entry.Author,
			nextVersion: strings.TrimPrefix(entry.Version, "v"),
			isUpdate:    false,
			requested:   sensitive,
			allNext:     sensitive,
			reasons:     plugins.ClonePermissionReasons(entry.PermissionReasons),
			entry:       &entryCopy,
			onInstalled: onInstalled,
		})
		return
	}

	sourceChanged := !c.sup.MatchesEntrySource(installed, entry)
	allNext := plugins.SensitivePermissions(entry.Permissions)
	if !sourceChanged {
		added := plugins.AddedSensitivePermissions(installed.Manifest.Permissions, entry.Permissions)
		if len(added) == 0 {
			c.sup.InstallEntryConfirmedAsync(entry, installed.Manifest.Permissions, false, onInstalled)
			return
		}
		entryCopy := entry
		c.enqueuePermissionPrompt(pendingPermissionPrompt{
			pluginID:       entry.ID,
			name:           entry.Name,
			icon:           entry.Icon,
			author:         entry.Author,
			currentVersion: installed.Manifest.Version,
			nextVersion:    strings.TrimPrefix(entry.Version, "v"),
			isUpdate:       true,
			requested:      added,
			existing:       plugins.SensitivePermissions(installed.Manifest.Permissions),
			allNext:        allNext,
			reasons:        plugins.ClonePermissionReasons(entry.PermissionReasons),
			entry:          &entryCopy,
			onInstalled:    onInstalled,
		})
		return
	}

	entryCopy := entry
	c.enqueuePermissionPrompt(pendingPermissionPrompt{
		pluginID:       entry.ID,
		name:           entry.Name,
		icon:           entry.Icon,
		author:         entry.Author,
		currentVersion: installed.Manifest.Version,
		nextVersion:    strings.TrimPrefix(entry.Version, "v"),
		isUpdate:       true,
		sourceChanged:  true,
		requested:      allNext,
		allNext:        allNext,
		reasons:        plugins.ClonePermissionReasons(entry.PermissionReasons),
		entry:          &entryCopy,
		onInstalled:    onInstalled,
	})
}

// RequestUpdateAll updates all entries that require no new sensitive
// permissions immediately, and holds back any permission-escalating or
// source-changing updates for explicit user review.
func (c *Controller) RequestUpdateAll(entries []plugins.StoreAppEntry, onInstalled func(pluginID string)) {
	if c == nil || c.sup == nil || len(entries) == 0 {
		return
	}
	safe := make([]plugins.StoreAppEntry, 0, len(entries))
	escalated := make([]plugins.StoreAppEntry, 0)
	for _, entry := range entries {
		inst, ok := c.sup.GetInstalled(entry.ID)
		if !ok {
			if len(plugins.SensitivePermissions(entry.Permissions)) > 0 {
				escalated = append(escalated, entry)
			} else {
				safe = append(safe, entry)
			}
			continue
		}
		if !c.sup.MatchesEntrySource(inst, entry) || len(plugins.AddedSensitivePermissions(inst.Manifest.Permissions, entry.Permissions)) > 0 {
			escalated = append(escalated, entry)
		} else {
			safe = append(safe, entry)
		}
	}

	if len(safe) == 0 {
		c.enqueueEscalatedEntries(escalated, onInstalled)
		return
	}

	c.sup.UpdateEntriesAsync(safe, onInstalled, func(extraEscalated []plugins.StoreAppEntry) {
		merged := mergeUniqueEntries(escalated, extraEscalated)
		if len(merged) == 0 {
			return
		}
		if c.isStoreVisible() {
			c.enqueueEscalatedEntries(merged, onInstalled)
			return
		}
		c.notifyEscalatedUpdates(merged, onInstalled)
	})
}

func mergeUniqueEntries(a, b []plugins.StoreAppEntry) []plugins.StoreAppEntry {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]plugins.StoreAppEntry, 0, len(a)+len(b))
	for _, list := range [][]plugins.StoreAppEntry{a, b} {
		for _, entry := range list {
			if entry.ID == "" || seen[entry.ID] {
				continue
			}
			seen[entry.ID] = true
			out = append(out, entry)
		}
	}
	return out
}

func (c *Controller) enqueueEscalatedEntries(entries []plugins.StoreAppEntry, onInstalled func(pluginID string)) {
	for _, entry := range entries {
		inst, isInstalled := c.sup.GetInstalled(entry.ID)
		if isInstalled {
			sourceChanged := !c.sup.MatchesEntrySource(inst, entry)
			allNext := plugins.SensitivePermissions(entry.Permissions)
			if sourceChanged {
				entryCopy := entry
				c.enqueuePermissionPrompt(pendingPermissionPrompt{
					pluginID:       entry.ID,
					name:           entry.Name,
					icon:           entry.Icon,
					author:         entry.Author,
					currentVersion: inst.Manifest.Version,
					nextVersion:    strings.TrimPrefix(entry.Version, "v"),
					isUpdate:       true,
					sourceChanged:  true,
					requested:      allNext,
					allNext:        allNext,
					reasons:        plugins.ClonePermissionReasons(entry.PermissionReasons),
					entry:          &entryCopy,
					onInstalled:    onInstalled,
				})
				continue
			}
			added := plugins.AddedSensitivePermissions(inst.Manifest.Permissions, entry.Permissions)
			if len(added) == 0 {
				continue
			}
			entryCopy := entry
			c.enqueuePermissionPrompt(pendingPermissionPrompt{
				pluginID:       entry.ID,
				name:           entry.Name,
				icon:           entry.Icon,
				author:         entry.Author,
				currentVersion: inst.Manifest.Version,
				nextVersion:    strings.TrimPrefix(entry.Version, "v"),
				isUpdate:       true,
				requested:      added,
				existing:       plugins.SensitivePermissions(inst.Manifest.Permissions),
				allNext:        allNext,
				reasons:        plugins.ClonePermissionReasons(entry.PermissionReasons),
				entry:          &entryCopy,
				onInstalled:    onInstalled,
			})
			continue
		}
		sensitive := plugins.SensitivePermissions(entry.Permissions)
		if len(sensitive) == 0 {
			continue
		}
		entryCopy := entry
		c.enqueuePermissionPrompt(pendingPermissionPrompt{
			pluginID:    entry.ID,
			name:        entry.Name,
			icon:        entry.Icon,
			author:      entry.Author,
			nextVersion: strings.TrimPrefix(entry.Version, "v"),
			isUpdate:    false,
			requested:   sensitive,
			allNext:     sensitive,
			reasons:     plugins.ClonePermissionReasons(entry.PermissionReasons),
			entry:       &entryCopy,
			onInstalled: onInstalled,
		})
	}
}

func (c *Controller) notifyEscalatedUpdates(entries []plugins.StoreAppEntry, onInstalled func(pluginID string)) {
	if c == nil || c.sup == nil || len(entries) == 0 {
		return
	}
	locale := c.sup.Locale()
	isEn := strings.HasPrefix(strings.ToLower(locale), "en")
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		n := entry.Name.Resolve(locale)
		if n == "" {
			n = entry.ID
		}
		names = append(names, n)
	}
	title := fmt.Sprintf("%d 个插件更新需要确认新权限", len(entries))
	body := strings.Join(names, "、")
	actionLabel := "查看"
	if isEn {
		if len(entries) == 1 {
			title = "1 plugin update needs new permissions"
		} else {
			title = fmt.Sprintf("%d plugin updates need new permissions", len(entries))
		}
		body = strings.Join(names, ", ")
		actionLabel = "Review"
	}
	entriesCopy := append([]plugins.StoreAppEntry(nil), entries...)
	c.sup.NotifyHost(plugins.ToastRequest{
		Message: title,
		Body:    body,
		Tone:    "warning",
		Action:  &plugins.ToastAction{ID: "review_permissions", Label: actionLabel},
		OnAction: func() {
			c.openStore()
			c.enqueueEscalatedEntries(entriesCopy, onInstalled)
		},
	})
}

// RequestManualInstall stages and validates a local/URL/GitHub manual plugin
// source before installing, showing a permission confirmation dialog when needed.
func (c *Controller) RequestManualInstall(spec string, onInstalled func(pluginID string)) {
	if c == nil || c.sup == nil {
		return
	}
	c.sup.StageManualInstallAsync(spec, func(staged *plugins.StagedPlugin) {
		if staged == nil {
			return
		}
		prompt := pendingPermissionPrompt{
			pluginID:      staged.Manifest.ID,
			name:          staged.Manifest.Name,
			icon:          staged.Manifest.Icon,
			author:        staged.Manifest.Author,
			nextVersion:   staged.Manifest.Version,
			isUpdate:      staged.IsUpdate,
			sourceChanged: staged.SourceChanged,
			requested:     staged.RequestedPermissions(),
			allNext:       plugins.SensitivePermissions(staged.Manifest.Permissions),
			reasons:       plugins.ClonePermissionReasons(staged.Manifest.PermissionReasons),
			staged:        staged,
			onInstalled:   onInstalled,
		}
		if staged.Previous != nil {
			prompt.currentVersion = staged.Previous.Version
			if !staged.SourceChanged {
				prompt.existing = plugins.SensitivePermissions(staged.Previous.Permissions)
			}
		}
		c.enqueuePermissionPrompt(prompt)
	}, onInstalled)
}

func (c *Controller) enqueuePermissionPrompt(prompt pendingPermissionPrompt) {
	if len(prompt.requested) == 0 && !prompt.sourceChanged {
		if prompt.staged != nil {
			prompt.staged.Cleanup()
		}
		return
	}
	var replacedStaged *plugins.StagedPlugin
	c.mu.Lock()
	c.nextPromptID++
	prompt.id = c.nextPromptID
	replaced := false
	for i := range c.permQueue {
		if c.permQueue[i].pluginID == prompt.pluginID {
			replacedStaged = c.permQueue[i].staged
			c.permQueue[i] = prompt
			replaced = true
			break
		}
	}
	if !replaced {
		c.permQueue = append(c.permQueue, prompt)
	}
	c.mu.Unlock()
	if replacedStaged != nil && replacedStaged != prompt.staged {
		replacedStaged.Cleanup()
	}
}

func (c *Controller) frontPermissionPrompt() (pendingPermissionPrompt, int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.permQueue) == 0 {
		return pendingPermissionPrompt{}, 0, false
	}
	return c.permQueue[0], len(c.permQueue), true
}

func (c *Controller) dismissPermissionPrompt(promptID uint64) {
	var staged *plugins.StagedPlugin
	c.mu.Lock()
	if len(c.permQueue) > 0 && c.permQueue[0].id == promptID {
		staged = c.permQueue[0].staged
		c.permQueue = c.permQueue[1:]
	}
	c.mu.Unlock()
	if staged != nil {
		staged.Cleanup()
	}
}

func (c *Controller) confirmPermissionPrompt(promptID uint64) {
	if c == nil || c.sup == nil {
		return
	}
	c.mu.Lock()
	if len(c.permQueue) == 0 || c.permQueue[0].id != promptID {
		c.mu.Unlock()
		return
	}
	prompt := c.permQueue[0]
	c.permQueue = c.permQueue[1:]
	c.mu.Unlock()

	var started bool
	if prompt.staged != nil {
		started = c.sup.CommitStagedInstallAsync(prompt.staged, prompt.allNext, prompt.onInstalled)
	} else if prompt.entry != nil {
		started = c.sup.InstallEntryConfirmedAsync(*prompt.entry, prompt.allNext, prompt.sourceChanged, prompt.onInstalled)
	}
	if !started {
		var dropStaged *plugins.StagedPlugin
		c.mu.Lock()
		hasReplacement := false
		for i := range c.permQueue {
			if c.permQueue[i].pluginID == prompt.pluginID {
				hasReplacement = true
				break
			}
		}
		if hasReplacement {
			dropStaged = prompt.staged
		} else {
			c.permQueue = append([]pendingPermissionPrompt{prompt}, c.permQueue...)
		}
		c.mu.Unlock()
		if dropStaged != nil {
			dropStaged.Cleanup()
		}
	}
}

// RenderPermissionModal draws the Ocean Paper permission confirmation modal
// whenever a fresh install requests sensitive permissions or an update adds
// new sensitive permissions.
func (c *Controller) RenderPermissionModal(ctx *ui.Context) {
	if c == nil || c.sup == nil {
		return
	}
	prompt, queueLen, ok := c.frontPermissionPrompt()
	if !ok {
		return
	}
	pal := theme.Ocean()
	locale := c.sup.Locale()
	_, installingID, _, _ := c.sup.StoreStatus()
	busy := installingID != ""

	modalTitle := theme.Tr("确认插件权限", "Confirm Plugin Permissions")
	if prompt.sourceChanged {
		modalTitle = theme.Tr("确认替换插件来源", "Confirm Source Replacement")
	} else if prompt.isUpdate {
		modalTitle = theme.Tr("更新请求了新权限", "Update Requests New Permissions")
	}
	pluginName := prompt.name.Resolve(locale)
	if pluginName == "" {
		pluginName = prompt.pluginID
	}
	versionMeta := ""
	if prompt.isUpdate && prompt.currentVersion != "" && prompt.nextVersion != "" && prompt.currentVersion != prompt.nextVersion {
		versionMeta = fmt.Sprintf("v%s → v%s", prompt.currentVersion, prompt.nextVersion)
	} else if prompt.nextVersion != "" {
		versionMeta = "v" + prompt.nextVersion
	}
	metaParts := []string{pluginName}
	if versionMeta != "" {
		metaParts = append(metaParts, versionMeta)
	}
	if prompt.author != "" {
		metaParts = append(metaParts, prompt.author)
	}
	if queueLen > 1 {
		metaParts = append(metaParts, fmt.Sprintf("1/%d", queueLen))
	}
	metaLine := strings.Join(metaParts, " · ")

	open := true
	bui.Dialog(ctx, "plugin-permission-modal", modalTitle, &open, bui.ModalLayout{
		Width:     theme.PermissionModalWidth,
		MaxHeight: theme.PermissionModalMaxHeight,
		Centered:  true,
	}, func() {
		ui.Column(ctx).FillWidth().MinWidth(0).Children(func() {
			// Header
			ui.Row(ctx).FillWidth().MinWidth(0).
				Padding(theme.Space20, theme.Space20, theme.Space16, theme.Space20).
				Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				ui.Row(ctx).Size(theme.PermissionModalIconBox, theme.PermissionModalIconBox).
					Radius(theme.RadiusMD).Background(pal.Shell).Border(1, pal.Hairline).
					AlignItems(ui.Center).Justify(ui.Center).Shrink(0).Children(func() {
					iconSpec := prompt.icon
					if len(iconSpec.StrokePaths) == 0 {
						iconSpec = plugins.AppStoreIconSpec()
					}
					DrawDuoSpec(ctx, iconSpec, theme.IconAction, pal.DuoIconColors(pal.Accent))
				})
				ui.Column(ctx).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
					ui.Text(ctx, modalTitle).
						FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
						FontWeight(theme.WeightTitle).TextColor(pal.Text).
						MinWidth(0).SingleLine().Ellipsis("…")
					ui.Text(ctx, metaLine).
						FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
						TextColor(pal.TextSecondary).
						MinWidth(0).SingleLine().Ellipsis("…")
				})
			})

			ui.Box(ctx).Height(1).FillWidth().Background(pal.Hairline)

			// Grouped sensitive permissions
			byCat := make(map[plugins.PermissionCategory][]plugins.Permission, len(plugins.PermissionCategoryOrder))
			for _, p := range prompt.requested {
				if cat, ok := plugins.PermissionCategoryOf(p); ok {
					byCat[cat] = append(byCat[cat], p)
				}
			}

			ui.Column(ctx).FillWidth().MinWidth(0).
				Padding(theme.Space16, theme.Space20).Gap(theme.Space12).Children(func() {
				if prompt.sourceChanged {
					ui.Column(ctx.Key("perm-source-change")).FillWidth().MinWidth(0).
						Radius(theme.RadiusMD).Background(pal.WarningSurface).
						Border(1, pal.Warning.Alpha(0.35)).
						Padding(theme.Space10, theme.Space12).Gap(theme.Space4).Children(func() {
						ui.Text(ctx, theme.Tr("安装来源与当前已安装插件不同", "Installation source differs from the installed plugin")).
							FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
							FontWeight(theme.WeightControl).TextColor(pal.Warning).MinWidth(0)
						ui.Text(ctx, fmt.Sprintf(
							theme.Tr("继续将替换现有插件，并沿用其启用状态与私有数据（apps-data/%s）。", "Continuing replaces the installed plugin and hands over its enabled state and private data (apps-data/%s)."),
							prompt.pluginID,
						)).
							FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(pal.Text).MinWidth(0)
					})
				}
				for _, cat := range plugins.PermissionCategoryOrder {
					perms := byCat[cat]
					if len(perms) == 0 {
						continue
					}
					catTitle := plugins.PermissionCategoryLabel(cat, locale)
					ui.Column(ctx.Key("perm-cat-" + string(cat))).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
						ui.Text(ctx, catTitle).
							FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).
							MinWidth(0).SingleLine()

						ui.Column(ctx).FillWidth().MinWidth(0).
							Radius(theme.RadiusMD).Background(pal.Shell).
							Padding(theme.Space10, theme.Space12).Gap(theme.Space10).Children(func() {
							for i, p := range perms {
								if i > 0 {
									ui.Box(ctx).Height(1).FillWidth().Background(pal.Hairline)
								}
								label := plugins.PermissionLabel(p, locale)
								customReason := ""
								if prompt.reasons != nil {
									customReason = prompt.reasons[p].Resolve(locale)
								}
								desc := customReason
								if desc == "" {
									desc = plugins.PermissionDescription(p, locale)
								}
								ui.Column(ctx.Key("perm-item-" + string(p))).FillWidth().MinWidth(0).Gap(theme.Space4).Children(func() {
									ui.Row(ctx).FillWidth().MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
										ui.Text(ctx, label).
											FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
											FontWeight(theme.WeightControl).TextColor(pal.Text).
											MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
										if prompt.isUpdate && !prompt.sourceChanged {
											ui.Row(ctx).Height(theme.KeycapHeight).PaddingX(theme.Space6).
												Radius(theme.RadiusXS).Background(pal.WarningSurface).
												AlignItems(ui.Center).Shrink(0).Children(func() {
												ui.Text(ctx, theme.Tr("新增", "New")).
													FontSize(theme.FontMicro).FixedLineHeight(theme.LineMeta).
													FontWeight(theme.WeightControl).TextColor(pal.Warning).SingleLine()
											})
										}
									})
									ui.Text(ctx, desc).
										FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
										TextColor(pal.TextSecondary).MinWidth(0)
									if p == plugins.PermNetwork && customReason != "" {
										ui.Text(ctx, theme.Tr("Block 不在进程层拦截联网，此项为插件自述。", "Self-declared by the plugin; Block does not sandbox outbound network access.")).
											FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
											TextColor(pal.TextSecondary).MinWidth(0)
									}
								})
							}
						})
					})
				}

				if prompt.isUpdate && len(prompt.existing) > 0 {
					labels := make([]string, 0, len(prompt.existing))
					for _, p := range prompt.existing {
						labels = append(labels, plugins.PermissionLabel(p, locale))
					}
					sep := "、"
					if strings.HasPrefix(strings.ToLower(locale), "en") {
						sep = ", "
					}
					ui.Text(ctx, theme.Tr("已有权限：", "Already granted: ")+strings.Join(labels, sep)).
						FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(pal.TextSecondary).MinWidth(0)
				}
			})

			ui.Box(ctx).Height(1).FillWidth().Background(pal.Hairline)

			// Footer actions
			ui.Row(ctx).FillWidth().MinWidth(0).
				Padding(theme.Space12, theme.Space20, theme.Space16, theme.Space20).
				Gap(theme.Space8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				cancelLabel := theme.Tr("取消", "Cancel")
				if queueLen > 1 {
					cancelLabel = theme.Tr("跳过", "Skip")
				}
				cancelBtn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
					ID:     fmt.Sprintf("plugin-permission-cancel-%d", prompt.id),
					Label:  cancelLabel,
					Style:  bui.ButtonSecondary,
					Height: theme.HeightControl,
				})
				if cancelBtn.Clicked() {
					c.dismissPermissionPrompt(prompt.id)
					ctx.Invalidate()
				}

				confirmLabel := theme.Tr("允许并安装", "Allow & Install")
				if prompt.sourceChanged {
					confirmLabel = theme.Tr("允许并替换", "Allow & Replace")
				} else if prompt.isUpdate {
					confirmLabel = theme.Tr("允许并更新", "Allow & Update")
				}
				confirmBtn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
					ID:       fmt.Sprintf("plugin-permission-confirm-%d", prompt.id),
					Label:    confirmLabel,
					Style:    bui.ButtonPrimary,
					Height:   theme.HeightControl,
					Disabled: busy,
					Loading:  busy,
				})
				if confirmBtn.Clicked() && !busy {
					c.confirmPermissionPrompt(prompt.id)
					ctx.Invalidate()
				}
			})
		})
	})

	if !open {
		c.dismissPermissionPrompt(prompt.id)
		ctx.Invalidate()
	}
}
