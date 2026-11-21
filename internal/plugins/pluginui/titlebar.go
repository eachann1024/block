package pluginui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"block/internal/icons"
	"block/internal/modals"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	"golang.org/x/mod/semver"
)

const storeDetailMenuTargetPrefix = "store:"

// CloseTitlebarOverlays dismisses any open plugin titlebar menu or error detail popover.
func (c *Controller) CloseTitlebarOverlays() bool {
	had := c.titlebarMenuOpen || c.titlebarErrOpen
	c.titlebarMenuOpen = false
	c.titlebarMenuTarget = ""
	c.titlebarMenuCursor = -1
	c.titlebarErrOpen = false
	c.titlebarErrPluginID = ""
	c.titlebarErrNotebook = ""
	return had
}

// RenderTitlebarLeading renders the unified Settings titlebar breadcrumb for
// plugin pages ("设置 / <Plugin> [Beta]") and App Store detail pages
// ("← 应用商店 / <Plugin> [Beta]"). Returns true when handled.
func (c *Controller) RenderTitlebarLeading(ctx *ui.Context, tab string, _ func(nextTab string)) bool {
	if c.sup == nil {
		return false
	}
	pal := theme.Ocean()
	if modals.IsStoreTab(tab) {
		entry, installedApp, isInstalled, _, ok := c.resolveStoreDetailState()
		if !ok {
			return false
		}
		locale := c.sup.Locale()
		isBeta := storeAppIsBeta(entry, installedApp, isInstalled)
		row := ui.Row(ctx.Key("store-tb-crumb")).Fill().MinWidth(0).AlignItems(ui.Center).Gap(theme.Space6).PaddingX(theme.Space6).DragWindow()
		row.Children(func() {
			backBtn := ui.ButtonBase(ctx.Key("store-tb-back")).Height(theme.HeightChrome).
				Padding(0, theme.Space6, 0, theme.Space4).Gap(theme.Space2).
				AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Shrink(0).
				Label(theme.Tr("返回应用商店", "Back to App Store"))
			hov := backBtn.Hovered()
			prs := backBtn.Pressed()
			bg := ui.Transparent
			fg := pal.TextSecondary
			if prs {
				bg = pal.ShellChrome().Hover
				fg = pal.Text
			} else if hov {
				bg = pal.ShellChrome().Hover
				fg = pal.Text
			}
			bui.AnimateSurfaceColors(backBtn, bg, ui.Transparent, false)
			backBtn.Children(func() {
				icons.Draw(ctx, icons.ChevronLeft, theme.IconInline, pal.MonoIconColors(fg))
				ui.Text(ctx, theme.Tr("应用商店", "App Store")).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					TextColor(fg).SingleLine()
			})
			bui.Probe(backBtn, "store-tb-back")
			if backBtn.Clicked() {
				c.CloseTitlebarOverlays()
				c.closeStoreReadme()
				ctx.Invalidate()
			}

			ui.Text(ctx, "/").FontSize(theme.FontTitle).TextColor(pal.Placeholder).SingleLine().Shrink(0)
			ui.Text(ctx, entry.Name.Resolve(locale)).FontSize(theme.FontBody).FontWeight(theme.WeightTitle).
				TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
			if isBeta {
				bui.BetaBadge(ctx, true)
			}
		})
		return true
	}

	pluginID, ok := strings.CutPrefix(tab, modals.TabPluginPrefix)
	if !ok || pluginID == "" {
		return false
	}
	inst, found := c.sup.GetInstalled(pluginID)
	title := pluginID
	isBeta := false
	if found {
		title = inst.Manifest.Name.Resolve(c.sup.Locale())
		isBeta = inst.Manifest.Beta
	}
	row := ui.Row(ctx.Key("plugin-tb-crumb-"+pluginID)).Fill().MinWidth(0).AlignItems(ui.Center).Gap(theme.Space6).PaddingX(theme.Space6).DragWindow()
	row.Children(func() {
		ui.Text(ctx, theme.Tr("设置", "Settings")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
		ui.Text(ctx, "/").FontSize(theme.FontTitle).TextColor(pal.Placeholder).SingleLine().Shrink(0)
		ui.Text(ctx, title).FontSize(theme.FontBody).FontWeight(theme.WeightTitle).TextColor(pal.Text).
			Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
		if isBeta {
			bui.BetaBadge(ctx, true)
		}
	})
	return true
}

// RenderTitlebarTrailing renders the unified right-hand titlebar controls for
// plugin pages and App Store detail pages.
func (c *Controller) RenderTitlebarTrailing(ctx *ui.Context, tab string, onSelectTab func(nextTab string)) {
	if c.sup == nil {
		return
	}
	if modals.IsStoreTab(tab) {
		c.renderStoreDetailTitlebarTrailing(ctx, onSelectTab)
		return
	}
	pluginID, ok := strings.CutPrefix(tab, modals.TabPluginPrefix)
	if !ok || pluginID == "" {
		return
	}
	inst, found := c.sup.GetInstalled(pluginID)
	if !found {
		return
	}
	c.renderPluginTitlebarTrailing(ctx, pluginID, inst, onSelectTab)
}

func (c *Controller) renderStoreDetailTitlebarTrailing(ctx *ui.Context, onSelectTab func(nextTab string)) {
	entry, installedApp, isInstalled, installingID, ok := c.resolveStoreDetailState()
	if !ok {
		return
	}
	busy := installingID == entry.ID
	ui.Row(ctx.Key("store-detail-tb-actions-"+entry.ID)).Gap(theme.Space8).AlignItems(ui.Center).Shrink(0).Children(func() {
		if !isInstalled {
			lbl := theme.Tr("安装", "Install")
			if busy {
				lbl = theme.Tr("安装中…", "Installing…")
			}
			dlIc := icons.Download
			btn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
				ID:       "app-store-detail-install",
				Label:    lbl,
				Leading:  &dlIc,
				Style:    bui.ButtonPrimary,
				Height:   theme.HeightCompact,
				Disabled: installingID != "" && !busy,
				Loading:  busy,
			})
			if btn.Clicked() && !busy {
				c.sup.InstallEntryAsync(entry, nil)
			}
			return
		}

		if entry.ID == plugins.DiagnosticsPluginID {
			ui.Text(ctx, theme.Tr("已内置于 Block", "Built into Block")).
				FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				TextColor(theme.Ocean().TextSecondary).SingleLine()
			return
		}

		hasUpdate := entry.DownloadURL != "" && strings.TrimSpace(entry.Version) != "" &&
			semver.Compare("v"+strings.TrimPrefix(entry.Version, "v"), "v"+installedApp.Manifest.Version) > 0
		pluginBusy := c.sup.IsPluginBusyAnyNotebook(entry.ID)
		if hasUpdate {
			lbl := theme.Tr("更新", "Update")
			if busy {
				lbl = theme.Tr("更新中…", "Updating…")
			}
			upIc := icons.Refresh
			upBtn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
				ID:       "app-store-detail-update",
				Label:    lbl,
				Leading:  &upIc,
				Style:    bui.ButtonAccentOutline,
				Height:   theme.HeightCompact,
				Disabled: (installingID != "" && !busy) || (pluginBusy && !busy),
				Loading:  busy,
			})
			if upBtn.Clicked() && !busy && !pluginBusy {
				c.sup.InstallEntryAsync(entry, c.clearPluginState)
			}
		}

		if installedApp.Manifest.HasSettingsPage() {
			openBtn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
				ID:       "app-store-detail-open",
				Label:    theme.Tr("打开设置", "Open Settings"),
				Style:    bui.ButtonSecondary,
				Height:   theme.HeightCompact,
				PaddingX: theme.Space10,
			})
			if openBtn.Clicked() && onSelectTab != nil {
				c.CloseTitlebarOverlays()
				c.closeStoreReadme()
				onSelectTab(modals.TabPluginPrefix + entry.ID)
			}

			target := storeDetailMenuTargetPrefix + entry.ID
			c.renderMoreMenuTrigger(ctx, "store-detail-more-"+entry.ID, target, installingID != "")
			return
		}

		uninstBusy := c.sup.IsPluginUninstalling(entry.ID)
		uninstLbl := theme.Tr("卸载", "Uninstall")
		if uninstBusy {
			uninstLbl = theme.Tr("卸载中…", "Uninstalling…")
		}
		trashIc := icons.Trash
		uninstDisabled := installingID != "" || uninstBusy || pluginBusy
		uninstBtn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
			ID:       "app-store-detail-uninstall",
			Label:    uninstLbl,
			Leading:  &trashIc,
			Style:    bui.ButtonDanger,
			Height:   theme.HeightCompact,
			Disabled: uninstDisabled,
			Loading:  uninstBusy,
		})
		if uninstBtn.Clicked() && !uninstDisabled {
			c.CloseTitlebarOverlays()
			c.uninstallPlugin(entry.ID, nil)
		}
	})
}

func (c *Controller) renderPluginTitlebarTrailing(
	ctx *ui.Context,
	pluginID string,
	inst plugins.InstalledApp,
	onSelectTab func(nextTab string),
) {
	c.activeSettingsTab = modals.TabPluginPrefix + pluginID
	vs, starting, failed, live := c.pluginVisualState(pluginID, inst)
	declarative := inst.Manifest.IsDeclarativeOnly()
	uninstBusy := c.sup.IsPluginUninstalling(pluginID) || vs.BusyAction == "__uninstall__"
	pluginBusyAny := c.sup.IsPluginBusyAnyNotebook(pluginID)
	actionRunning := !uninstBusy && vs.BusyAction != "" && (live || (inst.Enabled && vs.BusyAction == "__retry_note_saved__"))
	canCancel := actionRunning && c.sup.CanCancelAction(pluginID)
	uninstErr := strings.TrimSpace(c.sup.UninstallError(pluginID))

	errDetail := resolvePluginErrorDetail(vs, uninstErr)
	hasError := !uninstBusy && (uninstErr != "" || failed || (live && !actionRunning && errDetail != ""))
	if !hasError && c.titlebarErrOpen && c.titlebarErrPluginID == pluginID {
		c.titlebarErrOpen = false
		c.titlebarErrPluginID = ""
		c.titlebarErrNotebook = ""
	}

	primaryAct, _ := splitHeaderActions(vs.Page)
	fb := actionFeedbackFromView(vs)
	pal := theme.Ocean()
	vpW, _ := ctx.Size()
	narrowTitlebar := vpW > 0 && vpW < 860

	ui.Row(ctx.Key("plugin-tb-controls-"+pluginID)).Gap(theme.Space10).AlignItems(ui.Center).Shrink(0).Children(func() {
		// ② Status slot: strictly 1 item following priority:
		// Error > Starting > Running(empty) > Pending > Language extensions > Plugin status > None.
		switch {
		case hasError:
			c.renderTitlebarErrorPill(ctx, pluginID, shortPluginErrorLabel(pluginID, failed, uninstErr != "", vs), errDetail)
		case actionRunning:
			// Progress is integrated directly into the running primary action pill.
		case starting:
			ui.Row(ctx.Key("plugin-tb-starting-"+pluginID)).Gap(theme.Space6).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Spinner(ctx).FontSize(theme.IconInline).TextColor(pal.Accent).Label(theme.Tr("启动中", "Starting"))
				ui.Text(ctx, theme.Tr("启动中…", "Starting…")).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					TextColor(pal.TextSecondary).SingleLine()
			})
		case narrowTitlebar:
			// Keep breadcrumb, primary action, switch and overflow menu legible on narrow windows.
		case live && vs.PendingSaves > 0:
			c.renderTitlebarPendingStatus(ctx, pluginID, vs.PendingSaves)
		case declarative && inst.Enabled:
			if exts := formatLanguageExtensions(inst); exts != "" {
				ui.Text(ctx, exts).Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					FontWeight(theme.WeightMono).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
			}
		case live:
			statusText, statusTone := resolvePluginTitlebarStatus(vs)
			if statusText != "" {
				renderTitlebarQuietStatus(ctx, statusText, statusTone)
			}
		}

		// ③ Primary action slot: at most 1 button.
		// When a retried note.saved paused for confirmation, surface the plugin's primary action
		// (e.g. Sync Now / Confirm Sync) instead of trapping the user behind a repeating Retry Sync button.
		showRetryPrimary := hasError && !(vs.RetryKind == plugins.RetryKindNoteSaved && vs.SavePausedConfirm && live && primaryAct != nil)
		switch {
		case uninstBusy:
			ui.Row(ctx.Key("plugin-tb-uninstalling-"+pluginID)).Height(theme.HeightCompact).PaddingX(theme.Space10).
				Gap(theme.Space6).Radius(theme.RadiusMD).Background(pal.DangerSurface).
				Border(1, pal.Danger.Alpha(0.35)).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Spinner(ctx).FontSize(theme.IconInline).TextColor(pal.Danger).Label(theme.Tr("卸载中", "Uninstalling"))
				ui.Text(ctx, theme.Tr("卸载中…", "Uninstalling…")).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					FontWeight(theme.WeightControl).TextColor(pal.Danger).SingleLine()
			})
		case actionRunning:
			c.renderRunningActionPill(ctx, pluginID, vs, primaryAct, canCancel)
		case showRetryPrimary:
			retryLabel := resolvePluginRetryLabel(pluginID, failed, uninstErr != "", vs)
			refIc := icons.Refresh
			retryDisabled := vs.Loading || (uninstErr != "" && pluginBusyAny)
			retryBtn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
				ID:       "plugin-tb-retry-" + pluginID,
				Label:    retryLabel,
				Leading:  &refIc,
				Style:    bui.ButtonPrimary,
				Height:   theme.HeightCompact,
				PaddingX: theme.Space12,
				Disabled: retryDisabled,
			})
			if retryBtn.Clicked() && !retryDisabled {
				c.CloseTitlebarOverlays()
				switch {
				case uninstErr != "" || vs.RetryKind == plugins.RetryKindUninstall:
					c.uninstallPlugin(pluginID, func() {
						if onSelectTab != nil && c.activeSettingsTab == modals.TabPluginPrefix+pluginID {
							onSelectTab(modals.TabAppStore)
						}
					})
				case vs.RetryKind != plugins.RetryKindNone || failed:
					c.sup.RetryPluginAsync(pluginID)
				case vs.LastActionID != "" && (vs.LastActionWasHeader || isHeaderActionID(vs.Page, vs.LastActionID)):
					c.sup.TriggerActionAsync(pluginID, vs.LastActionID)
				case primaryAct != nil:
					c.sup.TriggerActionAsync(pluginID, primaryAct.ID)
				default:
					c.sup.ReloadPluginViewAsync(pluginID)
				}
			}
		case live && primaryAct != nil:
			b := *primaryAct
			isSuccess := fb.BusyAction == "" && fb.LastActionButtonActive && fb.LastActionID == b.ID && fb.LastActionTone == "success"
			style := mapButtonStyle(b.Variant)
			var leading *icons.Icon
			if isSuccess {
				style = bui.ButtonSuccess
				chk := icons.Check
				leading = &chk
			} else {
				leading = resolveActionButtonIcon(b.ID)
			}
			label := resolveActionButtonLabel(b.ID, b.Label, false, isSuccess)
			btn := bui.ActionButtonWithOptions(ctx, bui.ActionButtonOptions{
				ID:                 "plugin-hdr-act-" + pluginID + "-" + b.ID,
				Label:              label,
				AccessibilityLabel: label,
				Leading:            leading,
				Style:              style,
				Height:             theme.HeightCompact,
				PaddingX:           theme.Space12,
				Disabled:           vs.Loading,
			})
			if btn.Clicked() && !vs.Loading {
				c.CloseTitlebarOverlays()
				c.sup.TriggerActionAsync(pluginID, b.ID)
			}
		}

		// ④ Light enable switch (28x16, same as settings rows) with a plain state
		// label. Reflects actual persisted enabled state; when startup failed, stays
		// ON with error ring so toggling OFF disables cleanly.
		switchVal := inst.Enabled
		if !starting && !failed && !uninstBusy {
			stateText, stateCol := theme.Tr("已停用", "Disabled"), pal.Placeholder
			if switchVal {
				stateText, stateCol = theme.Tr("运行中", "Running"), pal.TextSecondary
			}
			ui.Text(ctx, stateText).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				FontWeight(theme.WeightRegular).TextColor(stateCol).SingleLine().Shrink(0)
		}
		phase := bui.SwitchIdle
		if starting {
			phase = bui.SwitchBusy
		} else if failed {
			phase = bui.SwitchError
		}
		if bui.SwitchWithOptions(ctx, bui.SwitchOptions{
			ID:          "plugin-hdr-enable-" + pluginID,
			Label:       theme.Tr("启用插件", "Enable Plugin"),
			Value:       &switchVal,
			Size:        bui.SwitchHero,
			Phase:       phase,
			ErrorKey:    vs.Error,
			AsyncEnable: !declarative,
			Disabled:    pluginBusyAny,
			Tooltip:     theme.Tr("启用或停用插件", "Enable or disable plugin"),
		}) {
			c.CloseTitlebarOverlays()
			c.sup.SetPluginEnabled(pluginID, switchVal)
		}

		// ⑤ Unified "⋯" More menu trigger.
		c.renderMoreMenuTrigger(ctx, "plugin-tb-more-"+pluginID, pluginID, uninstBusy)
	})
}

func (c *Controller) renderTitlebarErrorPill(ctx *ui.Context, pluginID, label, detail string) {
	pal := theme.Ocean()
	open := c.titlebarErrOpen && c.titlebarErrPluginID == pluginID
	btn := ui.ButtonBase(ctx.Key("plugin-tb-error-"+pluginID)).Height(theme.HeightChrome).
		PaddingX(theme.Space8).Gap(theme.Space6).AlignItems(ui.Center).
		Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Shrink(0).Label(label)
	hov := btn.Hovered()
	prs := btn.Pressed()
	bg := pal.DangerSurface
	bd := ui.Transparent
	hasBD := false
	switch {
	case open:
		bg = pal.Danger.Alpha(0.16)
		bd = pal.Danger.Alpha(0.45)
		hasBD = true
	case prs:
		bg = pal.Danger.Alpha(0.20)
	case hov:
		bg = pal.Danger.Alpha(0.14)
	}
	bui.AnimateSurfaceColors(btn, bg, bd, hasBD)
	bui.TactileButton(btn, hov, prs, false)
	if !open && detail != "" {
		bui.Tooltip(ctx, btn, theme.Tr("点击查看失败原因", "Click to view error details"))
	}
	btn.Children(func() {
		drawDangerAlertIcon(ctx, theme.IconInline, pal.Danger)
		ui.Text(ctx, label).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
			FontWeight(theme.WeightControl).TextColor(pal.Danger).SingleLine()
	})
	bui.Probe(btn, "plugin-tb-error-"+pluginID)
	if open {
		if b := btn.Bounds(); b.W > 0 {
			c.titlebarErrAnchor = b
		}
	}
	if btn.Clicked() {
		if open {
			c.titlebarErrOpen = false
			c.titlebarErrPluginID = ""
			c.titlebarErrNotebook = ""
		} else {
			c.titlebarMenuOpen = false
			c.titlebarMenuTarget = ""
			c.titlebarErrOpen = true
			c.titlebarErrPluginID = pluginID
			c.titlebarErrNotebook = c.sup.NotebookRoot()
			c.titlebarErrAnchor = btn.Bounds()
			c.titlebarErrScroll.Y = 0
			c.titlebarErrCopiedAt = time.Time{}
		}
	}
}

func drawDangerAlertIcon(ctx *ui.Context, size float32, col ui.Color) {
	ui.Box(ctx).Size(size, size).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		cx := r.X + r.W*0.5
		cy := r.Y + r.H*0.5
		rad := min(r.W, r.H)*0.5 - 1
		if rad <= 0 {
			return
		}
		p.Stroke(ui.Rect{X: cx - rad, Y: cy - rad, W: rad * 2, H: rad * 2}, col, rad, 1.5)
		stemW := float32(1.5)
		stemH := rad * 0.72
		p.Fill(ui.Rect{X: cx - stemW*0.5, Y: cy - stemH*0.68, W: stemW, H: stemH}, col, stemW*0.5)
		dot := float32(1.8)
		p.Fill(ui.Rect{X: cx - dot*0.5, Y: cy + rad*0.36, W: dot, H: dot}, col, dot*0.5)
	})
}

func (c *Controller) renderTitlebarPendingStatus(ctx *ui.Context, pluginID string, count int) {
	pal := theme.Ocean()
	suffix := theme.Tr(" 项待处理", " pending")
	if pluginID == "block-sync" {
		suffix = theme.Tr(" 项待同步", " pending sync")
	}
	ui.Row(ctx.Key("plugin-tb-pending-"+pluginID)).Gap(theme.Space6).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(ctx).Size(theme.StoreStatusDotSize, theme.StoreStatusDotSize).
			Radius(theme.RadiusPill).Background(pal.Accent).Shrink(0)
		ui.Row(ctx).Gap(0).AlignItems(ui.Center).Children(func() {
			ui.Text(ctx, strconv.Itoa(count)).Font("monospace").FontSize(theme.FontSmall).
				FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightMono).TextColor(pal.Accent).SingleLine()
			ui.Text(ctx, suffix).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				TextColor(pal.Accent).SingleLine()
		})
	})
}

func renderTitlebarQuietStatus(ctx *ui.Context, text, tone string) {
	pal := theme.Ocean()
	textCol := pal.TextSecondary
	dotCol, showDot := logToneDotColor(pal, tone)
	if !showDot && isHealthySyncStatus(text) {
		dotCol = pal.Success
		showDot = true
	}
	switch strings.ToLower(strings.TrimSpace(tone)) {
	case "danger", "error":
		textCol = pal.Danger
	case "warning", "warn":
		textCol = pal.Warning
	}
	wrap := ui.Row(ctx).Gap(theme.Space6).AlignItems(ui.Center).MaxWidth(theme.PluginTitlebarStatusMaxW).MinWidth(0).Shrink(1)
	bui.WrappedTooltip(ctx, wrap, text)
	wrap.Children(func() {
		if showDot {
			ui.Box(ctx).Size(theme.StoreStatusDotSize, theme.StoreStatusDotSize).
				Radius(theme.RadiusPill).Background(dotCol).Shrink(0)
		}
		ui.Text(ctx, text).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
			TextColor(textCol).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
	})
}

func isHealthySyncStatus(text string) bool {
	s := strings.TrimSpace(text)
	return strings.HasPrefix(s, "上次同步") || strings.HasPrefix(strings.ToLower(s), "last sync")
}

func (c *Controller) renderRunningActionPill(
	ctx *ui.Context,
	pluginID string,
	vs plugins.PluginViewState,
	primaryAct *plugins.UIButton,
	canCancel bool,
) {
	pal := theme.Ocean()
	actionID := vs.BusyAction
	baseLabel := ""
	if actionID == "" && primaryAct != nil {
		actionID = primaryAct.ID
		baseLabel = primaryAct.Label
	} else if primaryAct != nil && primaryAct.ID == actionID {
		baseLabel = primaryAct.Label
	} else if vs.Page != nil {
		for _, b := range vs.Page.HeaderActions {
			if b.ID == actionID {
				baseLabel = b.Label
				break
			}
		}
	}
	verb, progressText := formatRunningActionLabel(actionID, baseLabel, vs)

	cursor := ui.CursorDefault
	if canCancel {
		cursor = ui.CursorPointer
	}
	padR := theme.Space10
	if canCancel {
		padR = theme.Space4
	}
	a11y := verb
	if progressText != "" {
		a11y += " " + progressText
	}
	if canCancel {
		a11y += theme.Tr("，点击取消", ", click to cancel")
	}

	pill := ui.ButtonBase(ctx.Key("plugin-tb-running-"+pluginID)).Height(theme.HeightCompact).
		Padding(0, padR, 0, theme.Space10).Gap(theme.Space6).
		AlignItems(ui.Center).Radius(theme.RadiusMD).
		Cursor(cursor).Disabled(!canCancel).DimDisabled(false).Shrink(0).Label(a11y)
	hov := pill.Hovered() && canCancel
	prs := pill.Pressed() && canCancel
	bg := pal.Selected
	if prs {
		bg = pal.Selected.Mix(pal.Accent, 0.12)
	} else if hov {
		bg = pal.Selected.Mix(pal.Accent, 0.06)
	}
	bui.AnimateSurfaceColors(pill, bg, pal.SelectedBorder, true)
	if canCancel {
		bui.TactileButton(pill, hov, prs, false)
		bui.Tooltip(ctx, pill, theme.Tr("点击取消当前任务", "Click to cancel task"))
	}
	pill.Children(func() {
		ui.Spinner(ctx).FontSize(theme.IconInline).TextColor(pal.Accent).Label(verb)
		ui.Text(ctx, verb).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
			FontWeight(theme.WeightControl).TextColor(pal.Accent).MaxWidth(150).SingleLine().Ellipsis("…")
		if progressText != "" {
			ui.Text(ctx, progressText).Font("monospace").FontSize(theme.FontSmall).
				FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightMono).TextColor(pal.Accent).SingleLine().Shrink(0)
		}
		if canCancel {
			chipBG := pal.Card
			sqCol := pal.Accent
			if prs {
				chipBG = pal.AccentPressed
				sqCol = pal.AccentText
			} else if hov {
				chipBG = pal.Accent
				sqCol = pal.AccentText
			}
			chip := ui.Box(ctx).Size(theme.PluginCancelChipSize, theme.PluginCancelChipSize).
				Radius(theme.RadiusSM).Center().Shrink(0)
			bui.AnimateSurfaceColors(chip, chipBG, ui.Transparent, false)
			chip.Children(func() {
				ui.Box(ctx).Size(theme.PluginCancelStopSize, theme.PluginCancelStopSize).
					Radius(theme.RadiusXS).Background(sqCol)
			})
		}
	})
	bui.Probe(pill, "plugin-tb-running-"+pluginID)
	if canCancel && pill.Clicked() {
		c.sup.CancelAction(pluginID)
	}
}

func (c *Controller) renderMoreMenuTrigger(ctx *ui.Context, id, target string, disabled bool) {
	open := c.titlebarMenuOpen && c.titlebarMenuTarget == target
	btn := bui.IconButton(ctx, icons.More, bui.IconButtonOptions{
		ID:          id,
		Label:       theme.Tr("更多", "More"),
		Tooltip:     theme.Tr("更多操作", "More actions"),
		Variant:     bui.IconBordered,
		CardBorder:  true,
		Size:        theme.HeightCompact,
		IconSize:    theme.IconAction,
		Radius:      theme.RadiusMD,
		Active:      open,
		ActiveStyle: bui.ActiveAccent,
		Disabled:    disabled,
	})
	if open {
		if b := btn.Bounds(); b.W > 0 {
			c.titlebarMenuAnchor = b
		}
	}
	if btn.Clicked() && !disabled {
		if open {
			c.titlebarMenuOpen = false
			c.titlebarMenuTarget = ""
			c.titlebarMenuCursor = -1
		} else {
			c.titlebarErrOpen = false
			c.titlebarErrPluginID = ""
			c.titlebarMenuOpen = true
			c.titlebarMenuTarget = target
			c.titlebarMenuAnchor = btn.Bounds()
			c.titlebarMenuCursor = -1
		}
	}
}

// RenderTitlebarOverlays draws the floating "⋯" More menu and Error detail
// popover inside the window's top Overlay layer.
func (c *Controller) RenderTitlebarOverlays(ctx *ui.Context, tab string, onSelectTab func(nextTab string)) {
	if c.sup == nil {
		return
	}
	if !c.titlebarMenuOpen && !c.titlebarErrOpen {
		return
	}

	scrim := ui.Box(ctx.Key("plugin-tb-overlay-scrim")).Absolute().Left(0).Top(0).Right(0).Bottom(0)
	if scrim.OverlayShortcut(0, ui.KeyEscape) {
		c.CloseTitlebarOverlays()
		ctx.Invalidate()
		return
	}
	var surface ui.Element
	var hasSurface bool
	if c.titlebarErrOpen {
		surface, hasSurface = c.renderTitlebarErrorPopover(ctx, tab, onSelectTab)
	} else if c.titlebarMenuOpen {
		surface, hasSurface = c.renderTitlebarMoreMenu(ctx, tab, onSelectTab)
	}
	if scrim.Clicked() && (!hasSurface || !surface.Hovered()) {
		c.CloseTitlebarOverlays()
	}
}

func (c *Controller) renderTitlebarErrorPopover(ctx *ui.Context, tab string, _ func(nextTab string)) (ui.Element, bool) {
	pluginID, ok := strings.CutPrefix(tab, modals.TabPluginPrefix)
	if !ok || pluginID == "" || pluginID != c.titlebarErrPluginID || c.sup.NotebookRoot() != c.titlebarErrNotebook {
		c.titlebarErrOpen = false
		c.titlebarErrPluginID = ""
		c.titlebarErrNotebook = ""
		return ui.Element{}, false
	}
	inst, found := c.sup.GetInstalled(pluginID)
	if !found {
		c.titlebarErrOpen = false
		c.titlebarErrPluginID = ""
		c.titlebarErrNotebook = ""
		return ui.Element{}, false
	}
	vs, _, failed, _ := c.pluginVisualState(pluginID, inst)
	uninstErr := strings.TrimSpace(c.sup.UninstallError(pluginID))
	detail := resolvePluginErrorDetail(vs, uninstErr)
	if detail == "" && !failed {
		c.titlebarErrOpen = false
		c.titlebarErrPluginID = ""
		c.titlebarErrNotebook = ""
		return ui.Element{}, false
	}
	headline := shortPluginErrorLabel(pluginID, failed, uninstErr != "", vs)
	if detail == "" {
		detail = theme.Tr("未能启动插件进程，请查看日志了解详情。", "Failed to start plugin process. Open logs for details.")
	}

	pal := theme.Ocean()
	vpW, vpH := ctx.Size()
	popW := min(theme.PluginTitlebarPopoverW, max(vpW-theme.Space16, 1))
	anchor := c.titlebarErrAnchor
	if anchor.W <= 0 {
		anchor = ui.Rect{X: max(vpW-theme.PluginTitlebarPopoverW-theme.Space16, theme.Space8), Y: theme.Space8, W: theme.HeightChrome, H: theme.HeightChrome}
	}
	spec := bui.PlaceBelow(anchor).WithAlignX(bui.AlignX{Kind: bui.AlignRight})
	maxH := bui.MaxHeightForSpec(spec, vpW, vpH, 320)
	ox, oy := bui.ResolveOrigin(spec, popW, min(float32(200), maxH), vpW, vpH)

	card := bui.PopoverSurface(ui.Column(ctx.Key("plugin-tb-err-popover-"+pluginID)).
		Absolute().Left(ox).Top(oy).Width(popW).MaxHeight(maxH).Padding(theme.Space16).Gap(theme.Space10).
		Role(ui.RoleDialog))
	bui.Probe(card, "plugin-tb-err-popover-"+pluginID)
	card.Children(func() {
		ui.Text(ctx, headline).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			FontWeight(theme.WeightTitle).TextColor(pal.Danger)

		bui.Scroll(ctx.Key("plugin-tb-err-scroll-"+pluginID), &c.titlebarErrScroll).
			FillWidth().MinWidth(0).MaxHeight(max(maxH-96, 80)).Children(func() {
			ui.Text(ctx, detail).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				TextColor(pal.TextSecondary)
		})

		copied := !c.titlebarErrCopiedAt.IsZero() && time.Since(c.titlebarErrCopiedAt) < 1500*time.Millisecond
		copyLabel := theme.Tr("复制详情", "Copy details")
		if copied {
			copyLabel = theme.Tr("已复制", "Copied")
		}
		ui.Row(ctx).FillWidth().Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
			copyBtn := renderPopoverTextLink(ctx, "plugin-err-copy-"+pluginID, copyLabel)
			if copyBtn.Clicked() {
				ctx.Services().WriteClipboard(detail)
				c.titlebarErrCopiedAt = time.Now()
				sup := c.sup
				time.AfterFunc(1550*time.Millisecond, func() {
					if sup != nil {
						sup.NotifyRedraw()
					}
				})
			}
			logBtn := renderPopoverTextLink(ctx, "plugin-err-logs-"+pluginID, theme.Tr("打开日志", "Open logs"))
			if logBtn.Clicked() {
				c.CloseTitlebarOverlays()
				plugins.OpenLogFile()
			}
			if !inst.Manifest.IsDeclarativeOnly() && uninstErr == "" && (vs.Page == nil || vs.RetryKind != plugins.RetryKindNone) {
				reloadBtn := renderPopoverTextLink(ctx, "plugin-err-reload-"+pluginID, theme.Tr("重新加载", "Reload"))
				if reloadBtn.Clicked() {
					c.CloseTitlebarOverlays()
					c.sup.RestartPluginAsync(pluginID)
				}
			}
		})
	})
	return card, true
}

func renderPopoverTextLink(ctx *ui.Context, id, label string) ui.Element {
	pal := theme.Ocean()
	btn := ui.ButtonBase(ctx.Key(id)).Height(theme.HeightMicro).PaddingX(theme.Space4).
		Radius(theme.RadiusXS).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(label)
	fg := pal.Accent
	bg := ui.Transparent
	if btn.Pressed() {
		bg = pal.Selected
	} else if btn.Hovered() {
		bg = pal.Hover
	}
	bui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
	btn.Children(func() {
		ui.Text(ctx, label).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
			FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
	})
	bui.Probe(btn, id)
	return btn
}

func (c *Controller) renderTitlebarMoreMenu(ctx *ui.Context, tab string, onSelectTab func(nextTab string)) (ui.Element, bool) {
	var items []bui.MenuItemSpec
	if modals.IsStoreTab(tab) {
		entry, _, isInstalled, installingID, ok := c.resolveStoreDetailState()
		if !ok || !isInstalled || c.titlebarMenuTarget != storeDetailMenuTargetPrefix+entry.ID {
			c.CloseTitlebarOverlays()
			return ui.Element{}, false
		}
		items = c.buildStoreDetailMoreMenuItems(ctx, entry, installingID)
	} else {
		pluginID, ok := strings.CutPrefix(tab, modals.TabPluginPrefix)
		if !ok || pluginID == "" || c.titlebarMenuTarget != pluginID {
			c.CloseTitlebarOverlays()
			return ui.Element{}, false
		}
		inst, found := c.sup.GetInstalled(pluginID)
		if !found {
			c.CloseTitlebarOverlays()
			return ui.Element{}, false
		}
		items = c.buildPluginMoreMenuItems(pluginID, inst, onSelectTab)
	}
	if len(items) == 0 {
		c.CloseTitlebarOverlays()
		return ui.Element{}, false
	}

	vpW, vpH := ctx.Size()
	menuW := min(theme.PluginTitlebarMenuW, max(vpW-theme.Space16, 1))
	anchor := c.titlebarMenuAnchor
	if anchor.W <= 0 {
		anchor = ui.Rect{X: max(vpW-theme.HeightCompact-theme.Space12, theme.Space8), Y: theme.Space8, W: theme.HeightCompact, H: theme.HeightCompact}
	}
	spec := bui.PlaceBelow(anchor).WithAlignX(bui.AlignX{Kind: bui.AlignRight})
	maxH := bui.MaxHeightForSpec(spec, vpW, vpH, 360)
	estH := estimatePluginMenuHeight(items)
	ox, oy := bui.ResolveOrigin(spec, menuW, min(estH, maxH), vpW, vpH)

	panel := bui.MenuSurface(ui.Column(ctx.Key("plugin-tb-more-menu")).
		Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
	bui.NavigateMenu(ctx, panel, items, &c.titlebarMenuCursor)
	bui.Probe(panel, "plugin-tb-more-menu")
	panel.Children(func() {
		bui.Scroll(ctx).FillWidth().MaxHeight(bui.MenuInnerMaxHeight(maxH)).Children(func() {
			ui.Column(ctx).FillWidth().Gap(theme.MenuGap).Children(func() {
				for i, item := range items {
					row := bui.MenuRow(ctx, item, i == c.titlebarMenuCursor)
					if !item.Separator && !item.Disabled && row.Clicked() && item.OnClick != nil {
						item.OnClick()
					}
				}
			})
		})
	})
	return panel, true
}

func (c *Controller) buildUninstallMenuItem(
	id, pluginID string,
	disabled bool,
	onAfter func(),
) bui.MenuItemSpec {
	uninstBusy := c.sup.IsPluginUninstalling(pluginID)
	uninstLbl := theme.Tr("卸载", "Uninstall")
	if uninstBusy {
		uninstLbl = theme.Tr("卸载中…", "Uninstalling…")
	}
	trashIc := icons.Trash
	return bui.MenuItemSpec{
		ID:       id,
		Label:    uninstLbl,
		Icon:     &trashIc,
		Danger:   true,
		Disabled: disabled || uninstBusy || c.sup.IsPluginBusyAnyNotebook(pluginID),
		OnClick: func() {
			c.CloseTitlebarOverlays()
			c.uninstallPlugin(pluginID, onAfter)
		},
	}
}

func (c *Controller) buildStoreDetailMoreMenuItems(
	ctx *ui.Context,
	entry plugins.StoreAppEntry,
	installingID string,
) []bui.MenuItemSpec {
	items := make([]bui.MenuItemSpec, 0, 3)
	if repo := strings.TrimSpace(entry.Repo); repo != "" && !strings.Contains(repo, "://") {
		linkIc := icons.LinkOpen
		items = append(items, bui.MenuItemSpec{
			ID:    "store-detail-menu-repo",
			Label: theme.Tr("打开项目主页", "Open Repository"),
			Icon:  &linkIc,
			OnClick: func() {
				c.CloseTitlebarOverlays()
				ctx.OpenURL("https://github.com/" + strings.TrimPrefix(repo, "/"))
			},
		})
		items = append(items, bui.MenuItemSpec{Separator: true})
	}
	items = append(items, c.buildUninstallMenuItem("app-store-detail-uninstall", entry.ID, installingID != "", nil))
	return items
}

func (c *Controller) buildPluginMoreMenuItems(
	pluginID string,
	inst plugins.InstalledApp,
	onSelectTab func(nextTab string),
) []bui.MenuItemSpec {
	vs, _, failed, live := c.pluginVisualState(pluginID, inst)
	declarative := inst.Manifest.IsDeclarativeOnly()
	uninstBusy := c.sup.IsPluginUninstalling(pluginID) || vs.BusyAction == "__uninstall__"
	pluginBusyAny := c.sup.IsPluginBusyAnyNotebook(pluginID)
	actionRunning := !uninstBusy && vs.BusyAction != "" && (live || (inst.Enabled && vs.BusyAction == "__retry_note_saved__"))

	items := make([]bui.MenuItemSpec, 0, 8)

	// 1. Header actions declared by the plugin (secondary actions, plus primary action if displaced by a retry button).
	if live && vs.Page != nil {
		primaryAct, secondaryActs := splitHeaderActions(vs.Page)
		uninstErr := strings.TrimSpace(c.sup.UninstallError(pluginID))
		errDetail := resolvePluginErrorDetail(vs, uninstErr)
		hasError := !uninstBusy && (uninstErr != "" || failed || (live && !actionRunning && errDetail != ""))
		primaryDisplaced := hasError && !(vs.RetryKind == plugins.RetryKindNoteSaved && vs.SavePausedConfirm)
		if primaryDisplaced && primaryAct != nil {
			b := *primaryAct
			ic := resolveActionButtonIcon(b.ID)
			items = append(items, bui.MenuItemSpec{
				ID:       "plugin-menu-act-" + pluginID + "-" + b.ID,
				Label:    b.Label,
				Icon:     ic,
				Danger:   b.Variant == plugins.VariantDanger,
				Disabled: vs.Loading || actionRunning || uninstBusy,
				OnClick: func() {
					c.CloseTitlebarOverlays()
					c.sup.TriggerActionAsync(pluginID, b.ID)
				},
			})
		}
		for _, act := range secondaryActs {
			b := act
			ic := resolveActionButtonIcon(b.ID)
			items = append(items, bui.MenuItemSpec{
				ID:       "plugin-menu-act-" + pluginID + "-" + b.ID,
				Label:    b.Label,
				Icon:     ic,
				Danger:   b.Variant == plugins.VariantDanger,
				Disabled: vs.Loading || actionRunning || uninstBusy,
				OnClick: func() {
					c.CloseTitlebarOverlays()
					c.sup.TriggerActionAsync(pluginID, b.ID)
				},
			})
		}
	}

	// 2. Scoped cancel for queued background note.saved sync tasks (if any).
	if live && vs.PendingSaves > 0 {
		stopIc := icons.Stop
		cancelLabel := theme.Tr("取消待处理任务", "Cancel pending tasks")
		if pluginID == "block-sync" {
			cancelLabel = theme.Tr("取消待同步任务", "Cancel pending sync")
		}
		items = append(items, bui.MenuItemSpec{
			ID:    "plugin-menu-cancel-pending-" + pluginID,
			Label: cancelLabel,
			Icon:  &stopIc,
			OnClick: func() {
				c.CloseTitlebarOverlays()
				c.sup.CancelPendingSaves(pluginID)
			},
		})
	}

	// 3. View in App Store detail (with mono version hint).
	verHint := ""
	if v := strings.TrimSpace(inst.Manifest.Version); v != "" {
		verHint = "v" + strings.TrimPrefix(v, "v")
	}
	pkgIc := icons.Package
	items = append(items, bui.MenuItemSpec{
		ID:    "plugin-menu-store-" + pluginID,
		Label: theme.Tr("在应用商店查看", "View in App Store"),
		Hint:  verHint,
		Icon:  &pkgIc,
		OnClick: func() {
			c.CloseTitlebarOverlays()
			c.openInstalledPluginReadme(inst)
			if onSelectTab != nil {
				onSelectTab(modals.TabAppStore)
			}
		},
	})

	// 4. Reload runtime (true process restart for enabled non-declarative plugins).
	if inst.Enabled && !declarative {
		refIc := icons.Refresh
		items = append(items, bui.MenuItemSpec{
			ID:       "plugin-menu-reload-" + pluginID,
			Label:    theme.Tr("重新加载", "Reload Plugin"),
			Icon:     &refIc,
			Disabled: vs.Loading || pluginBusyAny,
			OnClick: func() {
				c.CloseTitlebarOverlays()
				c.sup.RestartPluginAsync(pluginID)
			},
		})
	}

	// 5. Separator + Uninstall (danger, unified guard with App Store detail).
	if pluginID != plugins.DiagnosticsPluginID {
		items = append(items, bui.MenuItemSpec{Separator: true})
		items = append(items, c.buildUninstallMenuItem("plugin-hdr-uninstall-"+pluginID, pluginID, vs.Loading, func() {
			if onSelectTab != nil && c.activeSettingsTab == modals.TabPluginPrefix+pluginID {
				onSelectTab(modals.TabAppStore)
			}
		}))
	}

	return items
}

func estimatePluginMenuHeight(items []bui.MenuItemSpec) float32 {
	h := theme.MenuPadding*2 + 2
	for _, it := range items {
		if it.Separator {
			h += 7
		} else {
			h += theme.MenuItemHeight + theme.MenuGap
		}
	}
	return h
}

func splitHeaderActions(page *plugins.UIPage) (primary *plugins.UIButton, secondary []plugins.UIButton) {
	if page == nil || len(page.HeaderActions) == 0 {
		return nil, nil
	}
	primaryIdx := -1
	for i := range page.HeaderActions {
		if page.HeaderActions[i].Variant == plugins.VariantPrimary {
			primaryIdx = i
			break
		}
	}
	if primaryIdx < 0 {
		for i := range page.HeaderActions {
			if page.HeaderActions[i].Variant == plugins.VariantDanger {
				primaryIdx = i
				break
			}
		}
	}
	for i := range page.HeaderActions {
		b := page.HeaderActions[i]
		if i == primaryIdx {
			cp := b
			primary = &cp
			continue
		}
		secondary = append(secondary, b)
	}
	return primary, secondary
}

func resolvePluginErrorDetail(vs plugins.PluginViewState, uninstErr string) string {
	if uninstErr != "" {
		return uninstErr
	}
	if msg := strings.TrimSpace(vs.Error); msg != "" {
		return msg
	}
	if (vs.LastActionWasHeader || isHeaderActionID(vs.Page, vs.LastActionID)) &&
		strings.EqualFold(strings.TrimSpace(vs.LastActionTone), "danger") {
		if msg := strings.TrimSpace(vs.LastActionMessage); msg != "" {
			return msg
		}
		return theme.Tr("操作执行失败", "Action failed")
	}
	return ""
}

func shortPluginErrorLabel(pluginID string, failed, hasUninstErr bool, vs plugins.PluginViewState) string {
	if hasUninstErr || vs.RetryKind == plugins.RetryKindUninstall {
		return theme.Tr("卸载失败", "Uninstall failed")
	}
	if failed {
		return theme.Tr("启动失败", "Failed to start")
	}
	switch pluginID {
	case "block-sync":
		if vs.RetryKind == plugins.RetryKindNoteSaved || vs.FailedActionID == "sync_now" || vs.LastActionID == "sync_now" || vs.LastActionID == "confirm_sync" || vs.LastActionID == "confirm_sync_card" {
			return theme.Tr("同步失败", "Sync failed")
		}
	case "block-migrator":
		if vs.FailedActionID == "run_import" || vs.FailedActionID == "run_import_card" || vs.LastActionID == "run_import" {
			return theme.Tr("迁入失败", "Import failed")
		}
	case "block-image-host":
		if vs.FailedActionID == "upload_now" || vs.LastActionID == "upload_now" {
			return theme.Tr("上传失败", "Upload failed")
		}
	}
	return theme.Tr("操作失败", "Error")
}

func resolvePluginRetryLabel(pluginID string, failed, hasUninstErr bool, vs plugins.PluginViewState) string {
	if hasUninstErr || vs.RetryKind == plugins.RetryKindUninstall {
		return theme.Tr("重试卸载", "Retry Uninstall")
	}
	if failed || vs.RetryKind == plugins.RetryKindReload {
		return theme.Tr("重新加载", "Reload")
	}
	if pluginID == "block-sync" && (vs.RetryKind == plugins.RetryKindNoteSaved || vs.FailedActionID == "sync_now" || vs.LastActionID == "sync_now" || vs.LastActionID == "confirm_sync" || vs.LastActionID == "confirm_sync_card") {
		return theme.Tr("重试同步", "Retry Sync")
	}
	return theme.Tr("重试", "Retry")
}

func resolvePluginTitlebarStatus(vs plugins.PluginViewState) (text, tone string) {
	headerAct := vs.LastActionWasHeader || isHeaderActionID(vs.Page, vs.LastActionID)
	cardAct := vs.LastActionID != "" && !headerAct && isCardActionID(vs.Page, vs.LastActionID)
	if headerAct && strings.TrimSpace(vs.LastActionMessage) != "" {
		text = strings.TrimSpace(vs.LastActionMessage)
		tone = vs.LastActionTone
	} else if !cardAct {
		text = strings.TrimSpace(vs.Notice)
		if text == "" && vs.Page != nil {
			text = strings.TrimSpace(vs.Page.Status)
		}
	} else if vs.Page != nil {
		pageSt := strings.TrimSpace(vs.Page.Status)
		if pageSt != "" && pageSt != strings.TrimSpace(vs.LastActionMessage) {
			text = pageSt
		}
	}
	// The enable switch already communicates enabled/disabled state; never repeat it.
	switch strings.ToLower(text) {
	case "已启用", "enabled", "已停用", "disabled":
		return "", ""
	}
	if tone == "" && text != "" && vs.Page != nil && vs.Page.HasHeaderAction("confirm_sync") {
		tone = "warning"
	}
	return text, tone
}

func formatRunningActionLabel(actionID, baseLabel string, vs plugins.PluginViewState) (verb, progress string) {
	ap := vs.ActionProgress
	if ap.Label != "" {
		verb = ap.Label
	} else {
		verb = resolveActionButtonLabel(actionID, baseLabel, true, false)
	}
	switch {
	case ap.Total > 0:
		verb = strings.TrimSuffix(strings.TrimSuffix(verb, "…"), "...")
		progress = fmt.Sprintf("%d/%d", ap.Done, ap.Total)
	case ap.HasProgress:
		verb = strings.TrimSuffix(strings.TrimSuffix(verb, "…"), "...")
		pct := int(math.Round(ap.Progress * 100))
		progress = fmt.Sprintf("%d%%", max(min(pct, 100), 0))
	case vs.Page != nil && vs.Page.Progress != nil:
		verb = strings.TrimSuffix(strings.TrimSuffix(verb, "…"), "...")
		pct := int(math.Round(*vs.Page.Progress * 100))
		progress = fmt.Sprintf("%d%%", max(min(pct, 100), 0))
	}
	return verb, progress
}

func formatLanguageExtensions(inst plugins.InstalledApp) string {
	langs := plugins.ActiveLanguagesForPlugin(inst.Manifest.ID)
	if len(langs) == 0 {
		langs = plugins.LoadPluginLanguagesFromDir(inst.InstallDir, &inst.Manifest)
	}
	if len(langs) == 0 {
		return ""
	}
	seen := make(map[string]bool)
	tags := make([]string, 0, 4)
	add := func(s string) {
		t := strings.TrimSpace(s)
		if t == "" || seen[t] {
			return
		}
		seen[t] = true
		tags = append(tags, t)
	}
	for _, info := range langs {
		for _, ext := range info.Contrib.Extensions {
			add(ext)
		}
	}
	if len(tags) == 0 {
		for _, info := range langs {
			for _, fn := range info.Contrib.Filenames {
				add(fn)
			}
		}
	}
	if len(tags) == 0 {
		return ""
	}
	if len(tags) > 3 {
		return strings.Join(tags[:3], " ") + " +" + strconv.Itoa(len(tags)-3)
	}
	return strings.Join(tags, " ")
}

func (c *Controller) findCatalogEntry(pluginID string) *plugins.StoreAppEntry {
	if c.sup == nil {
		return nil
	}
	catalog := c.sup.Catalog()
	if catalog == nil {
		return nil
	}
	for i := range catalog.Apps {
		if catalog.Apps[i].ID == pluginID {
			e := catalog.Apps[i]
			return &e
		}
	}
	return nil
}

func (c *Controller) openInstalledPluginReadme(inst plugins.InstalledApp) {
	if entry := c.findCatalogEntry(inst.Manifest.ID); entry != nil {
		c.openStoreReadme(*entry, inst)
		return
	}
	synthetic := plugins.StoreAppEntry{
		ID:          inst.Manifest.ID,
		Category:    inst.Manifest.EffectiveCategory(),
		Name:        inst.Manifest.Name,
		Summary:     inst.Manifest.Summary,
		Version:     inst.Manifest.Version,
		Author:      inst.Manifest.Author,
		Repo:        inst.Manifest.Repo,
		Beta:              inst.Manifest.Beta,
		Permissions:       inst.Manifest.Permissions,
		PermissionReasons: plugins.ClonePermissionReasons(inst.Manifest.PermissionReasons),
		Icon:              inst.Manifest.Icon,
	}
	c.openStoreReadme(synthetic, inst)
}
