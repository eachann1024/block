package pluginui

import (
	"fmt"
	"path/filepath"
	"strings"

	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// InputBindings connects declarative controls to persistent string drafts,
// dropdown popover states, directory pickers, and secret visibility toggles.
type InputBindings struct {
	SelectState         func(controlID string) *bui.DropdownSelectState
	TreemapState        func(controlID string) *bui.TreemapState
	OnTreemapSelect     func()
	ResolveDraft        func(controlID, fallback string) string
	OnDraftChanged      func(controlID, next string)
	IsSecretShown       func(controlID string) bool
	ToggleSecret        func(controlID string)
	PickDirectory       func(controlID, title string)
	PickFile            func(controlID, title string, extensions []string)
	PickChecklistFolder func(controlID, title string)
	IsCardExpanded      func(cardID string, defaultCollapsed bool) bool
	ToggleCardExpanded  func(cardID string, defaultCollapsed bool)
}

// ActionFeedback describes the live execution and completion state of plugin actions.
type ActionFeedback struct {
	BusyAction             string
	LastActionID           string
	LastActionTone         string
	LastActionMessage      string
	LastActionButtonActive bool
	FailedActionID         string
	Error                  string
	CompactLayout          bool
	SidebarLayout          bool
}

func actionFeedbackFromView(vs plugins.PluginViewState) ActionFeedback {
	return ActionFeedback{
		BusyAction:             vs.BusyAction,
		LastActionID:           vs.LastActionID,
		LastActionTone:         vs.LastActionTone,
		LastActionMessage:      vs.LastActionMessage,
		LastActionButtonActive: vs.LastActionButtonActive,
		FailedActionID:         vs.FailedActionID,
		Error:                  vs.Error,
	}
}

// RenderPage renders a complete validated declarative plugin UIPage (header + cards)
// using Block's native Ocean Paper components.
func RenderPage(
	c *ui.Context,
	pluginID string,
	page *plugins.UIPage,
	values map[string]any,
	onChange func(controlID string, nextValue any),
	onAction func(actionID string, currentValues map[string]any),
) {
	if page == nil {
		return
	}
	pal := theme.Ocean()
	if values == nil {
		values = page.ExtractDefaultValues()
	}

	root := ui.Column(c.Key("plugin-page-" + pluginID)).FillWidth().MinWidth(0).Gap(theme.SettingsSectionGap)
	bui.Probe(root, "plugin-page-"+pluginID)

	var wrappedChange func(string, any, bool)
	if onChange != nil {
		wrappedChange = func(id string, v any, _ bool) {
			onChange(id, v)
		}
	}

	root.Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Row(c).Gap(theme.Space10).AlignItems(ui.Center).MinWidth(0).Grow(1).Children(func() {
				ui.Text(c, page.Title).
					FontSize(theme.FontSettingsTitle).
					FixedLineHeight(theme.LineSettingsTitle).
					FontWeight(theme.WeightTitle).
					TextColor(pal.Text).
					SingleLine().
					Ellipsis("…")
				if page.Status != "" {
					renderStatusText(c, page.Status)
				}
			})
			if len(page.HeaderActions) > 0 {
				ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Shrink(0).Children(func() {
					for _, btn := range page.HeaderActions {
						b := btn
						el := bui.ActionButton(c, "plugin-act-"+pluginID+"-"+b.ID, b.Label, resolveActionButtonIcon(b.ID), mapButtonStyle(b.Variant), false)
						if el.Clicked() && onAction != nil {
							onAction(b.ID, cloneValues(values))
						}
					}
				})
			}
		})

		renderPageCards(c, pluginID, page, values, ActionFeedback{}, nil, wrappedChange, onAction)
	})
}

func isHeaderActionID(page *plugins.UIPage, actionID string) bool {
	if page == nil || actionID == "" {
		return false
	}
	for _, b := range page.HeaderActions {
		if b.ID == actionID {
			return true
		}
	}
	return false
}

func isCardActionID(page *plugins.UIPage, actionID string) bool {
	if page == nil || actionID == "" {
		return false
	}
	for _, card := range page.Cards {
		for _, item := range card.Items {
			if item.Kind == plugins.KindActions {
				for _, b := range item.Buttons {
					if b.ID == actionID {
						return true
					}
				}
			}
		}
	}
	return false
}

func renderStatusText(c *ui.Context, text string) {
	renderStatusTextWithTone(c, text, "")
}

func renderStatusTextWithTone(c *ui.Context, text, tone string) {
	pal := theme.Ocean()
	textCol := pal.TextSecondary
	dotCol, showDot := logToneDotColor(pal, tone)
	switch strings.ToLower(strings.TrimSpace(tone)) {
	case "danger", "error":
		textCol = pal.Danger
	case "warning", "warn":
		textCol = pal.Warning
	}
	wrap := ui.Row(c).Gap(theme.Space6).AlignItems(ui.Center).MaxWidth(280).MinWidth(0).Shrink(1)
	bui.WrappedTooltip(c, wrap, text)
	wrap.Children(func() {
		if showDot {
			ui.Box(c).Size(theme.Space6, theme.Space6).Radius(theme.RadiusPill).Background(dotCol).Shrink(0)
		}
		ui.Text(c, text).
			FontSize(theme.FontSmall).
			FixedLineHeight(theme.LineSmall).
			TextColor(textCol).
			MinWidth(0).
			Shrink(1).
			SingleLine().
			Ellipsis("…")
	})
}

func pluginItemKey(pluginID, cardID string, idx int, item plugins.UIItem) string {
	if strings.TrimSpace(item.ID) != "" {
		return fmt.Sprintf("plugin-item-%s-%s-%s", pluginID, cardID, item.ID)
	}
	if item.Kind == plugins.KindActions && len(item.Buttons) > 0 {
		return fmt.Sprintf("plugin-item-%s-%s-act-%s", pluginID, cardID, item.Buttons[0].ID)
	}
	return fmt.Sprintf("plugin-item-%s-%s-%s-%d", pluginID, cardID, item.Kind, idx)
}

func renderPageCards(
	c *ui.Context,
	pluginID string,
	page *plugins.UIPage,
	values map[string]any,
	fb ActionFeedback,
	bindings *InputBindings,
	onChange func(controlID string, nextValue any, immediate bool),
	onAction func(actionID string, currentValues map[string]any),
) {
	renderPageCardsWithScope(c, pluginID, theme.SettingsSectionGap, page, values, fb, bindings, onChange, onAction)
}

func renderPageCardsWithScope(
	c *ui.Context,
	pluginID string,
	cardGap float32,
	page *plugins.UIPage,
	values map[string]any,
	fb ActionFeedback,
	bindings *InputBindings,
	onChange func(controlID string, nextValue any, immediate bool),
	onAction func(actionID string, currentValues map[string]any),
) {
	if page == nil {
		return
	}
	pal := theme.Ocean()
	if values == nil {
		values = page.ExtractDefaultValues()
	}
	if cardGap <= 0 {
		cardGap = theme.SettingsSectionGap
	}

	ui.Column(c.Key("plugin-cards-" + pluginID)).FillWidth().MinWidth(0).Gap(cardGap).Children(func() {
		for _, card := range page.Cards {
			cd := card
			visibleItems := make([]plugins.UIItem, 0, len(cd.Items))
			for _, it := range cd.Items {
				if plugins.IsItemVisible(it, values) {
					visibleItems = append(visibleItems, it)
				}
			}
			if len(visibleItems) == 0 {
				continue
			}
			c.Key("plugin-card-" + pluginID + "-" + cd.ID)
			expanded := true
			if cd.Collapsible {
				if bindings != nil && bindings.IsCardExpanded != nil {
					expanded = bindings.IsCardExpanded(cd.ID, cd.DefaultCollapsed)
				} else {
					expanded = !cd.DefaultCollapsed
				}
			}
			bui.SettingsSection(c, func() {
				addFolderList := len(visibleItems) == 1 &&
					visibleItems[0].Kind == plugins.KindChecklist &&
					visibleItems[0].AddFolder &&
					strings.TrimSpace(visibleItems[0].Label) == ""
				mergedChecklist := addFolderList || len(visibleItems) == 1 &&
					visibleItems[0].Kind == plugins.KindChecklist &&
					strings.TrimSpace(visibleItems[0].Label) == "" &&
					len(visibleItems[0].Options) > 1
				hasTitle := strings.TrimSpace(cd.Title) != ""
				hasBadge := strings.TrimSpace(cd.Badge) != ""
				hasMeta := strings.TrimSpace(cd.Meta) != ""
				if hasTitle || hasBadge || hasMeta || cd.Collapsible || mergedChecklist {
					var trailing func()
					if hasBadge || hasMeta || cd.Collapsible || mergedChecklist {
						trailing = func() {
							if hasBadge {
								ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).Radius(theme.RadiusPill).
									Background(pal.Selected).AlignItems(ui.Center).Shrink(0).Children(func() {
									ui.Text(c, cd.Badge).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
										FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine()
								})
							}
							if hasMeta {
								ui.Text(c, cd.Meta).
									FontSize(theme.FontSmall).
									FixedLineHeight(theme.LineSmall).
									TextColor(pal.TextSecondary).
									Shrink(0).
									SingleLine()
							}
							if mergedChecklist {
								if addFolderList {
									renderChecklistAddFolderAction(c, pluginID, visibleItems[0], bindings, fb.BusyAction != "")
								} else {
									renderChecklistSelectAllAction(c, pluginID, visibleItems[0], values, fb.BusyAction != "", onChange)
								}
							}
							if cd.Collapsible {
								toggleLabel := theme.Tr("展开配置", "Expand")
								toggleIcon := icons.ChevronDown
								if expanded {
									toggleLabel = theme.Tr("收起配置", "Collapse")
									toggleIcon = icons.ChevronUp
								}
								toggleBtn := bui.IconButton(c, toggleIcon, bui.IconButtonOptions{
									ID:       "plugin-card-toggle-" + pluginID + "-" + cd.ID,
									Label:    toggleLabel,
									Tooltip:  toggleLabel,
									Variant:  bui.IconChrome,
									IconSize: theme.IconInline,
								})
								if toggleBtn.Clicked() && bindings != nil && bindings.ToggleCardExpanded != nil {
									bindings.ToggleCardExpanded(cd.ID, cd.DefaultCollapsed)
								}
							}
						}
					}
					bui.SettingsSectionTitle(c, cd.Title, trailing)
				}
				if expanded {
					ui.Column(c.Key("plugin-card-body-" + pluginID + "-" + cd.ID)).FillWidth().MinWidth(0).
						Transition(bui.CascadeRowTransition(0)).Children(func() {
						for idx, item := range visibleItems {
							it := item
							i := idx
							ui.Column(c.Key(pluginItemKey(pluginID, cd.ID, i, it))).FillWidth().MinWidth(0).
								Transition(bui.CascadeRowTransition(i)).Children(func() {
								if mergedChecklist {
									renderChecklistItem(c, pluginID, it, values, fb.BusyAction, true, bindings, onChange)
									return
								}
								renderSectionItem(c, pluginID, it, i, len(visibleItems), values, fb, bindings, onChange, onAction)
							})
						}
					})
				}
			})
		}
	})
}

func isStackedOrGroupItem(item plugins.UIItem) bool {
	switch item.Kind {
	case plugins.KindSegmented:
		return item.Layout == "full" || item.Layout == "stacked"
	case plugins.KindFolder, plugins.KindInput, plugins.KindSecret:
		return item.Layout == "stacked"
	case plugins.KindGroup:
		return true
	default:
		return false
	}
}

func isDataBlockItem(item plugins.UIItem) bool {
	switch item.Kind {
	case plugins.KindMetrics, plugins.KindTreemap, plugins.KindPreview, plugins.KindLogList, plugins.KindChecklist:
		return true
	default:
		return false
	}
}

func renderSectionItem(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	idx int,
	count int,
	values map[string]any,
	fb ActionFeedback,
	bindings *InputBindings,
	onChange func(controlID string, nextValue any, immediate bool),
	onAction func(actionID string, currentValues map[string]any),
) {
	pal := theme.Ocean()
	switch {
	case item.Kind == plugins.KindActions:
		if fb.SidebarLayout {
			ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space6, 0, 0, 0).Children(func() {
				renderItem(c, pluginID, item, false, values, fb, bindings, onChange, onAction)
			})
		} else {
			bui.SettingsRowFrame(ui.Column(c)).AlignItems(ui.Stretch).Justify(ui.Center).
				Padding(theme.Space8, 0).Children(func() {
				renderItem(c, pluginID, item, false, values, fb, bindings, onChange, onAction)
			})
		}
	case isStackedOrGroupItem(item):
		block := ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space12, 0)
		if !fb.SidebarLayout {
			block.BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
		}
		block.Children(func() {
			renderItem(c, pluginID, item, false, values, fb, bindings, onChange, onAction)
		})
	case isDataBlockItem(item):
		topPad := float32(0)
		bottomPad := float32(0)
		if !fb.SidebarLayout {
			if idx > 0 {
				topPad = theme.Space12
			}
			if idx < count-1 {
				bottomPad = theme.Space12
			}
		}
		block := ui.Column(c).FillWidth().MinWidth(0).Padding(topPad, 0, bottomPad, 0)
		if idx > 0 && !fb.SidebarLayout {
			block.BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
		}
		block.Children(func() {
			renderItem(c, pluginID, item, false, values, fb, bindings, onChange, onAction)
		})
	default:
		renderItem(c, pluginID, item, false, values, fb, bindings, onChange, onAction)
	}
}

func renderItem(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	inGroup bool,
	values map[string]any,
	fb ActionFeedback,
	bindings *InputBindings,
	onChange func(controlID string, nextValue any, immediate bool),
	onAction func(actionID string, currentValues map[string]any),
) {
	pal := theme.Ocean()
	busyAction := fb.BusyAction
	switch item.Kind {
	case plugins.KindSelect:
		curStr, _ := values[item.ID].(string)
		if curStr == "" {
			curStr, _ = item.Value.(string)
		}
		if bindings != nil && bindings.SelectState != nil {
			selOpts := make([]bui.SelectOption, len(item.Options))
			matched := false
			for i, opt := range item.Options {
				selOpts[i] = bui.SelectOption{
					ID:    fmt.Sprintf("plugin-sel-%s-%s-%s", pluginID, item.ID, opt.Value),
					Value: opt.Value,
					Label: opt.Label,
				}
				if opt.Value == curStr {
					matched = true
				}
			}
			if !matched && len(item.Options) > 0 {
				curStr = item.Options[0].Value
			}
			renderSelect := func() {
				if bui.DropdownSelect(c, "plugin-ctrl-"+pluginID+"-"+item.ID, &curStr, selOpts, bindings.SelectState(item.ID), item.Placeholder) {
					values[item.ID] = curStr
					if onChange != nil {
						onChange(item.ID, curStr, true)
					}
				}
			}
			if inGroup {
				ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
					if strings.TrimSpace(item.Label) != "" {
						renderFieldLabel(c, item.Label)
					}
					ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Children(renderSelect)
				})
				return
			}
			bui.SettingsRow(c, item.Label, renderSelect)
			return
		}
		fallthrough

	case plugins.KindSegmented:
		curStr, _ := values[item.ID].(string)
		if curStr == "" {
			curStr, _ = item.Value.(string)
		}
		activeIdx := 0
		segOpts := make([]bui.SegmentOption, len(item.Options))
		for i, opt := range item.Options {
			segOpts[i] = bui.SegmentOption{
				ID:    fmt.Sprintf("plugin-seg-%s-%s-%s", pluginID, item.ID, opt.Value),
				Label: opt.Label,
			}
			if opt.Value == curStr {
				activeIdx = i
			}
		}
		commitSeg := func() {
			if activeIdx >= 0 && activeIdx < len(item.Options) {
				nextVal := item.Options[activeIdx].Value
				values[item.ID] = nextVal
				if onChange != nil {
					onChange(item.ID, nextVal, true)
				}
			}
		}
		if item.Layout == "full" {
			ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				if strings.TrimSpace(item.Label) != "" {
					renderFieldLabel(c, item.Label)
				}
				if bui.SegmentedControlWithOptions(c, "plugin-ctrl-"+pluginID+"-"+item.ID, &activeIdx, segOpts, bui.SegmentedControlOptions{
					FillWidth:     true,
					Quiet:         true,
					TrackHeight:   theme.HeightLarge,
					TrackPadding:  theme.Space4,
					SegmentHeight: theme.HeightCompact,
					SegmentRadius: theme.RadiusSM,
				}) {
					commitSeg()
				}
			})
			return
		}
		if item.Layout == "stacked" {
			ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				if strings.TrimSpace(item.Label) != "" {
					renderFieldLabel(c, item.Label)
				}
				if bui.SegmentedControlWithOptions(c, "plugin-ctrl-"+pluginID+"-"+item.ID, &activeIdx, segOpts, bui.SegmentedControlOptions{
					FillWidth:     true,
					Quiet:         true,
					TrackHeight:   theme.HeightControl,
					TrackPadding:  theme.Space2,
					SegmentHeight: theme.HeightChrome,
					SegmentRadius: theme.RadiusSM,
				}) {
					commitSeg()
				}
			})
			return
		}
		if inGroup {
			ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Gap(theme.Space16).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					FontWeight(theme.WeightRegular).TextColor(pal.Text).SingleLine().Shrink(0)
				ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
					if bui.SettingsSegmented(c, "plugin-ctrl-"+pluginID+"-"+item.ID, &activeIdx, segOpts) {
						commitSeg()
					}
				})
			})
			return
		}
		bui.SettingsRow(c, item.Label, func() {
			if bui.SettingsSegmented(c, "plugin-ctrl-"+pluginID+"-"+item.ID, &activeIdx, segOpts) {
				commitSeg()
			}
		})

	case plugins.KindChecklist:
		renderChecklistItem(c, pluginID, item, values, busyAction, false, bindings, onChange)

	case plugins.KindGroup:
		visibleFields := make([]plugins.UIItem, 0, len(item.Fields))
		for _, field := range item.Fields {
			if plugins.IsItemVisible(field, values) {
				if field.Layout == "" {
					field.Layout = "stacked"
				}
				visibleFields = append(visibleFields, field)
			}
		}
		if len(visibleFields) == 0 {
			return
		}
		ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.End).Children(func() {
			for fi, field := range visibleFields {
				f := field
				fKey := f.ID
				if fKey == "" {
					fKey = fmt.Sprintf("%s-%d", f.Kind, fi)
				}
				ui.Column(c.Key("plugin-grp-field-" + pluginID + "-" + fKey)).Grow(1).Basis(0).MinWidth(0).
					Transition(bui.CascadeItemTransition(fi, false)).Children(func() {
					renderItem(c, pluginID, f, true, values, fb, bindings, onChange, onAction)
				})
			}
		})

	case plugins.KindSwitch:
		curBool, ok := values[item.ID].(bool)
		if !ok {
			curBool, _ = item.Value.(bool)
		}
		renderSwitch := func() {
			onLbl := theme.Tr("已关闭", "Off")
			if curBool {
				onLbl = theme.Tr("已开启", "On")
			}
			if bui.SwitchWithOptions(c, bui.SwitchOptions{
				ID:       "plugin-sw-" + pluginID + "-" + item.ID,
				Label:    onLbl,
				Value:    &curBool,
				Disabled: busyAction != "",
			}) {
				values[item.ID] = curBool
				if onChange != nil {
					onChange(item.ID, curBool, true)
				}
			}
		}
		if inGroup {
			ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Gap(theme.Space16).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
					renderRowIcon(c, item.Icon)
					ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(theme.WeightRegular).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				})
				renderSwitch()
			})
			return
		}
		renderIconSettingsRow(c, item.Icon, item.Label, renderSwitch)

	case plugins.KindFolder:
		path, ok := values[item.ID].(string)
		if !ok {
			path, _ = item.Value.(string)
		}
		disabled := busyAction != "" || bindings == nil || bindings.PickDirectory == nil
		hasFilePicker := len(item.AcceptExtensions) > 0 && bindings != nil && bindings.PickFile != nil
		fileDisabled := busyAction != "" || !hasFilePicker
		label := theme.Tr("选择文件夹…", "Choose Folder…")
		if path != "" && !hasFilePicker {
			label = theme.Tr("更改…", "Change…")
		}
		fileLabel := theme.Tr("选择文件…", "Choose File…")
		for _, ext := range item.AcceptExtensions {
			if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(ext), "."), "zip") {
				fileLabel = theme.Tr("选 ZIP…", "Choose ZIP…")
				break
			}
		}
		if item.Layout == "stacked" {
			ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				if strings.TrimSpace(item.Label) != "" {
					renderFieldLabel(c, item.Label)
				}
				ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
					boxCursor := ui.CursorPointer
					if disabled {
						boxCursor = ui.CursorDefault
					}
					pathBox := ui.ButtonBase(c.Key("plugin-folder-box-" + pluginID + "-" + item.ID)).
						Grow(1).MinWidth(0).Height(theme.HeightControl).PaddingX(theme.Space10).Gap(theme.Space8).
						AlignItems(ui.Center).Justify(ui.Start).Radius(theme.RadiusMD).
						Cursor(boxCursor).Disabled(disabled).DimDisabled(false).Label(item.Label)
					boxHover := pathBox.Hovered() && !disabled
					boxPress := pathBox.Pressed() && !disabled
					boxBG := pal.Shell
					boxBD := pal.Hairline
					if boxPress {
						boxBG = pal.Selected
						boxBD = pal.BtnBorder.Mix(pal.Text, 0.24)
					} else if boxHover {
						boxBG = pal.Hover
						boxBD = pal.BtnBorder
					}
					bui.AnimateSurfaceColors(pathBox, boxBG, boxBD, true)
					bui.TactileCard(pathBox, boxHover, boxPress, false, disabled)
					if disabled {
						pathBox.Opacity(theme.DisabledOpacity)
					}
					if path != "" {
						bui.WrappedTooltip(c, pathBox, path)
					}
					pathBox.Children(func() {
						icCol := pal.MonoIconColors(pal.TextSecondary)
						displayPath := theme.Tr("未选择文件夹", "No folder selected")
						if len(item.AcceptExtensions) > 0 {
							displayPath = theme.Tr("未选择文件夹或归档", "No folder or archive selected")
						}
						textCol := pal.Placeholder
						if path != "" || boxHover || boxPress {
							icCol = pal.DuoIconColors(pal.Text)
						}
						if path != "" {
							displayPath = compactFolderPath(path)
							textCol = pal.Text
						}
						icons.DrawAnimated(c, icons.Folder, theme.IconAction, icCol, boxHover || boxPress)
						ui.Text(c, displayPath).Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(textCol).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					})
					if !disabled && pathBox.Clicked() {
						bindings.PickDirectory(item.ID, item.Label)
					}
					folderIc := icons.Folder
					pick := bui.ActionButton(c, "plugin-folder-pick-"+pluginID+"-"+item.ID, label, &folderIc, bui.ButtonSecondary, disabled)
					if !disabled && pick.Clicked() {
						bindings.PickDirectory(item.ID, item.Label)
					}
					if hasFilePicker {
						fileIc := icons.Note
						pickFile := bui.ActionButton(c, "plugin-file-pick-"+pluginID+"-"+item.ID, fileLabel, &fileIc, bui.ButtonSecondary, fileDisabled)
						if !fileDisabled && pickFile.Clicked() {
							bindings.PickFile(item.ID, item.Label, item.AcceptExtensions)
						}
					}
				})
			})
			return
		}
		bui.SettingsRow(c, item.Label, func() {
			ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.End).Children(func() {
				if path != "" {
					folder := ui.Text(c.Key("plugin-folder-name-"+pluginID+"-"+item.ID), filepath.Base(path)).
						FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
					bui.WrappedTooltip(c, folder, path)
				}
				folderIc := icons.Folder
				pick := bui.ActionButton(c, "plugin-folder-pick-"+pluginID+"-"+item.ID, label, &folderIc, bui.ButtonSecondary, disabled)
				if !disabled {
					bui.WrappedTooltip(c, pick, path)
					if pick.Clicked() {
						bindings.PickDirectory(item.ID, item.Label)
					}
				}
				if hasFilePicker {
					fileIc := icons.Note
					pickFile := bui.ActionButton(c, "plugin-file-pick-"+pluginID+"-"+item.ID, fileLabel, &fileIc, bui.ButtonSecondary, fileDisabled)
					if !fileDisabled && pickFile.Clicked() {
						bindings.PickFile(item.ID, item.Label, item.AcceptExtensions)
					}
				}
			})
		})

	case plugins.KindInput, plugins.KindSecret:
		fallback, ok := values[item.ID].(string)
		if !ok {
			fallback, _ = item.Value.(string)
		}
		draft := fallback
		if bindings != nil && bindings.ResolveDraft != nil {
			draft = bindings.ResolveDraft(item.ID, fallback)
		}
		secretShown := true
		if item.Kind == plugins.KindSecret {
			secretShown = false
			if bindings != nil && bindings.IsSecretShown != nil {
				secretShown = bindings.IsSecretShown(item.ID)
			}
		}
		renderInputControl := func() {
			inputID := "plugin-in-" + pluginID + "-" + item.ID
			in := ui.TextInput(c.Key(inputID), &draft).
				Placeholder(item.Placeholder).
				Label(item.Label).
				FontSize(theme.FontBody).
				FixedLineHeight(theme.LineBody).
				Height(theme.HeightControl).
				Padding(0, theme.Space10).
				Radius(theme.RadiusMD).
				TextColor(pal.Text).
				PlaceholderColor(pal.Placeholder).
				Grow(1).
				MinWidth(0)
			if in.Focused() {
				in.Background(pal.Card).Border(1, pal.Accent)
			} else if in.Hovered() {
				in.Background(pal.Shell).Border(1, pal.BtnBorder)
			} else {
				in.Background(pal.Shell).Border(1, pal.Hairline)
			}
			if item.Kind == plugins.KindSecret && !secretShown {
				in.Password()
			}
			bui.Probe(in, inputID)
			if in.Changed() {
				values[item.ID] = draft
				if bindings != nil && bindings.OnDraftChanged != nil {
					bindings.OnDraftChanged(item.ID, draft)
				}
				if onChange != nil {
					onChange(item.ID, draft, false)
				}
			}
			if item.Kind == plugins.KindSecret {
				toggleLbl := theme.Tr("显示", "Show")
				toggleIc := icons.Eye
				if secretShown {
					toggleLbl = theme.Tr("隐藏", "Hide")
					toggleIc = icons.EyeOff
				}
				tBtn := bui.ActionButton(c, "plugin-secret-toggle-"+pluginID+"-"+item.ID, toggleLbl, &toggleIc, bui.ButtonSecondary, false)
				if tBtn.Clicked() && bindings != nil && bindings.ToggleSecret != nil {
					bindings.ToggleSecret(item.ID)
				}
			}
		}
		if item.Layout == "stacked" {
			ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				if strings.TrimSpace(item.Label) != "" {
					renderFieldLabel(c, item.Label)
				}
				ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Children(renderInputControl)
			})
			return
		}
		bui.SettingsRow(c, item.Label, func() {
			ui.Row(c).Grow(1).MinWidth(0).MaxWidth(theme.AIPanelDefaultWidth).Gap(theme.Space8).
				AlignItems(ui.Center).Justify(ui.End).Children(renderInputControl)
		})

	case plugins.KindMetrics:
		vpW, _ := c.Size()
		cols := 4
		if len(item.Metrics) < cols {
			cols = len(item.Metrics)
		}
		if vpW > 0 && vpW < 760 && cols > 2 {
			cols = 2
		}
		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space12).Children(func() {
			for i := 0; i < len(item.Metrics); i += cols {
				start := i
				end := min(i+cols, len(item.Metrics))
				ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Stretch).Children(func() {
					for j := start; j < end; j++ {
						m := item.Metrics[j]
						bg, border, labelCol, valCol := pal.Shell, pal.Hairline, pal.TextSecondary, pal.Text
						if tone := strings.TrimSpace(m.Tone); tone != "" {
							tc := pal.PerfTileColors(tone)
							bg = tc.BG
							border = tc.Border
							labelCol = tc.SubText
							valCol = tc.TitleText
						}
						ui.Column(c).Grow(1).Basis(0).MinWidth(0).
							Padding(theme.Space12, theme.Space16).Gap(theme.Space4).
							Radius(theme.RadiusLG).Background(bg).Border(1, border).Children(func() {
							ui.Text(c, m.Label).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								TextColor(labelCol).SingleLine().Ellipsis("…")
							ui.Text(c, m.Value).Font("monospace").FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).
								FontWeight(theme.WeightControl).TextColor(valCol).SingleLine().Ellipsis("…")
						})
					}
				})
			}
		})

	case plugins.KindTreemap:
		if len(item.Tiles) == 0 {
			return
		}
		tmID := item.ID
		if tmID == "" {
			tmID = "treemap"
		}
		var tmState *bui.TreemapState
		if bindings != nil && bindings.TreemapState != nil {
			tmState = bindings.TreemapState(tmID)
		}
		if tmState == nil {
			tmState = &bui.TreemapState{}
		}
		totalBytes := item.TotalBytes
		var sumBytes uint64
		tmItems := make([]bui.TreemapItem, len(item.Tiles))
		for i, t := range item.Tiles {
			colorKey := t.Color
			if colorKey == "" {
				colorKey = t.ID
			}
			sumBytes += t.Bytes
			entries := make([]bui.TreemapEntry, len(t.Entries))
			for ei, ent := range t.Entries {
				entries[ei] = bui.TreemapEntry{
					Name:  ent.Name,
					Path:  ent.Path,
					Bytes: ent.Bytes,
					Count: ent.Count,
				}
			}
			tmItems[i] = bui.TreemapItem{
				ID:        t.ID,
				ColorKey:  colorKey,
				Label:     t.Label,
				Detail:    t.Detail,
				Bytes:     t.Bytes,
				FileCount: t.FileCount,
				Color:     pal.PerfCategoryColor(colorKey),
				Entries:   entries,
			}
		}
		if totalBytes == 0 {
			totalBytes = sumBytes
		}
		var summary *bui.TreemapSummary
		if item.Summary != nil {
			sMetrics := make([]bui.TreemapSummaryMetric, len(item.Summary.Metrics))
			for i, m := range item.Summary.Metrics {
				sMetrics[i] = bui.TreemapSummaryMetric{Label: m.Label, Value: m.Value}
			}
			summary = &bui.TreemapSummary{
				TotalLabel: item.Summary.TotalLabel,
				TotalValue: item.Summary.TotalValue,
				SubText:    item.Summary.SubText,
				Metrics:    sMetrics,
			}
		}
		keyPrefix := "plugin-treemap-" + pluginID + "-" + tmID
		bui.RenderStorageTreemap(c, tmState, tmItems, totalBytes, bui.TreemapOptions{
			CanvasKey:       keyPrefix,
			CanvasProbeID:   keyPrefix,
			DetailKey:       keyPrefix + "-detail",
			TileProbePrefix: keyPrefix + "-tile-",
			Summary:         summary,
			OnSelect: func(_ string) {
				if bindings != nil && bindings.OnTreemapSelect != nil {
					bindings.OnTreemapSelect()
				}
			},
		})

	case plugins.KindPreview:
		if item.Preview != nil {
			renderPreviewItem(c, pluginID, item.Preview)
		}

	case plugins.KindActions:
		renderActionsItem(c, pluginID, item, values, fb, onAction)

	case plugins.KindKV:
		ui.Column(c).FillWidth().MinWidth(0).Children(func() {
			for _, row := range item.Rows {
				rw := row
				renderIconSettingsRow(c, rw.Icon, rw.Label, func() {
					value := ui.Text(c, rw.Value).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						TextColor(pal.TextSecondary).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
					bui.WrappedTooltip(c, value, rw.Tooltip)
				})
			}
		})

	case plugins.KindIconStrip:
		renderIconStrip(c, item)

	case plugins.KindLogList:
		renderLogListItem(c, pluginID, item, values, fb.BusyAction, fb.SidebarLayout, onAction)
	}
}

func renderRowIcon(c *ui.Context, ic *plugins.UIIcon) {
	if ic == nil {
		return
	}
	pal := theme.Ocean()
	DrawUIIcon(c, *ic, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))
}

// renderIconSettingsRow is bui.SettingsRow with an optional leading icon.
func renderIconSettingsRow(c *ui.Context, ic *plugins.UIIcon, label string, control func()) {
	if ic == nil {
		bui.SettingsRow(c, label, control)
		return
	}
	pal := theme.Ocean()
	bui.SettingsRowFrame(ui.Row(c)).Justify(ui.SpaceBetween).Children(func() {
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
			renderRowIcon(c, ic)
			ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).FontWeight(theme.WeightRegular).
				TextColor(pal.Text).SingleLine().Shrink(0)
		})
		ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Gap(theme.Space12).Children(control)
	})
}

// renderIconStrip draws a titlebar-like bar of right-aligned icons in their visible order.
func renderIconStrip(c *ui.Context, item plugins.UIItem) {
	pal := theme.Ocean()
	colors := pal.MonoIconColors(pal.TextSecondary)
	ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Padding(theme.Space4, theme.Space8).
		Radius(theme.RadiusMD).Background(pal.Sidebar).Border(1, pal.Hairline).
		AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
		if strings.TrimSpace(item.Label) != "" {
			ui.Text(c, item.Label).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				TextColor(pal.TextSecondary).SingleLine().MinWidth(0).Shrink(1).Ellipsis("…")
		}
		ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
			if len(item.Icons) == 0 {
				ui.Text(c, item.EmptyText).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					TextColor(pal.TextSecondary).SingleLine()
				return
			}
			for _, ic := range item.Icons {
				icon := ic
				if icon.Chip != "" {
					chip := DrawUIChip(c, icon)
					if icon.Tooltip != "" {
						bui.WrappedTooltip(c, chip, icon.Tooltip)
					}
					continue
				}
				box := ui.Box(c).Size(theme.HeightChrome, theme.HeightChrome).Shrink(0).Center().Children(func() {
					DrawUIIcon(c, icon, theme.IconChrome, colors)
				})
				if icon.Tooltip != "" {
					bui.WrappedTooltip(c, box, icon.Tooltip)
				}
			}
		})
	})
}

func logVisualWidth(s string) int {
	w := 0
	for _, r := range s {
		if r > 0x7f {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func logToneDotColor(pal theme.Palette, tone string) (ui.Color, bool) {
	switch strings.ToLower(strings.TrimSpace(tone)) {
	case "":
		return ui.Transparent, false
	case "success":
		return pal.Success, true
	case "danger", "error":
		return pal.Danger, true
	case "warning", "warn":
		return pal.Warning, true
	case "info", "accent":
		return pal.Accent, true
	default:
		return pal.PerfTileColors(tone).Dot, true
	}
}

func logToneBarColor(pal theme.Palette, tone string) ui.Color {
	t := strings.ToLower(strings.TrimSpace(tone))
	if t == "" {
		return pal.Hover
	}
	return pal.PerfTileColors(t).BarBG
}

func renderLogListItem(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	values map[string]any,
	busyAction string,
	sidebar bool,
	onAction func(actionID string, currentValues map[string]any),
) {
	if strings.TrimSpace(item.Label) != "" {
		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space8).Children(func() {
			renderFieldLabel(c, item.Label)
			renderLogEntries(c, pluginID, item, values, busyAction, sidebar, onAction)
		})
		return
	}
	renderLogEntries(c, pluginID, item, values, busyAction, sidebar, onAction)
}

func renderLogEntries(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	values map[string]any,
	busyAction string,
	sidebar bool,
	onAction func(actionID string, currentValues map[string]any),
) {
	pal := theme.Ocean()
	if len(item.Entries) == 0 {
		if strings.TrimSpace(item.EmptyText) != "" {
			ui.Text(c, item.EmptyText).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
		}
		return
	}

	hasStatus := false
	hasTime := false
	allArrow := true
	maxStatusW := 0
	maxTimeW := 0
	for _, e := range item.Entries {
		st := strings.TrimSpace(e.Status)
		tm := strings.TrimSpace(e.Time)
		if st != "" {
			hasStatus = true
			if st != "→" {
				allArrow = false
			}
			if w := logVisualWidth(st); w > maxStatusW {
				maxStatusW = w
			}
		} else {
			allArrow = false
		}
		if tm != "" {
			hasTime = true
			if w := logVisualWidth(tm); w > maxTimeW {
				maxTimeW = w
			}
		}
	}

	var statusColW float32
	if hasStatus && !allArrow {
		statusColW = float32(maxStatusW)*7.5 + 8
		if statusColW < 48 {
			statusColW = 48
		}
		if statusColW > 132 {
			statusColW = 132
		}
	}
	var timeColW float32
	if hasTime && !allArrow {
		timeColW = float32(maxTimeW)*7.5 + 8
		minTime := float32(56)
		if sidebar {
			minTime = 36
		}
		if timeColW < minTime {
			timeColW = minTime
		}
		if timeColW > 148 {
			timeColW = 148
		}
	}

	rowRadius := theme.RadiusSM
	rowPadX := theme.Space10
	rowGap := theme.Space12
	rowBG := pal.Sidebar
	titleSize := theme.FontSmall
	titleLine := theme.LineSmall
	if sidebar {
		rowRadius = theme.RadiusMD
		rowPadX = theme.Space8
		rowGap = theme.Space8
		rowBG = ui.Transparent
		titleSize = theme.FontBody
		titleLine = theme.LineBody
	}

	ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
		for idx, entry := range item.Entries {
			e := entry
			rowKey := fmt.Sprintf("plugin-log-%s-%s-%d", pluginID, item.ID, idx)
			actID := strings.TrimSpace(e.ActionID)
			clickable := actID != "" && onAction != nil
			var row ui.Element
			if clickable {
				row = ui.ButtonBase(c.Key(rowKey)).FillWidth().MinWidth(0).
					Height(theme.HeightControl).PaddingX(rowPadX).Gap(rowGap).
					Radius(rowRadius).Background(rowBG).
					AlignItems(ui.Center).Justify(ui.SpaceBetween).
					Cursor(ui.CursorPointer).Disabled(busyAction != "").Label(e.Title)
				if e.Ratio <= 0 {
					if row.Pressed() {
						row.Background(pal.Selected)
					} else if row.Hovered() {
						row.Background(pal.Hover)
					}
				}
			} else {
				row = ui.Row(c.Key(rowKey)).FillWidth().MinWidth(0).
					Height(theme.HeightControl).PaddingX(rowPadX).Gap(rowGap).
					Radius(rowRadius).Background(rowBG).
					AlignItems(ui.Center).Justify(ui.SpaceBetween)
				if e.Ratio <= 0 && row.Hovered() {
					row.Background(pal.Hover)
				}
			}

			if e.Ratio > 0 {
				ratio := float32(e.Ratio)
				if ratio < 0.04 {
					ratio = 0.04
				}
				if ratio > 1 {
					ratio = 1
				}
				barColor := logToneBarColor(pal, e.Tone)
				row.Draw(func(p *ui.Painter, bounds ui.Rect) {
					w := bounds.W * ratio
					if w >= 2 {
						p.Fill(ui.Rect{X: bounds.X, Y: bounds.Y, W: w, H: bounds.H}, barColor, rowRadius)
					}
				})
			}

			tip := strings.TrimSpace(e.Tooltip)
			if tip != "" {
				bui.WrappedTooltip(c, row, tip)
			}

			if allArrow {
				row.Children(func() {
					ui.Text(c, e.Title).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(pal.TextSecondary).Grow(1).Basis(0).MinWidth(0).SingleLine().Ellipsis("…")
					ui.Text(c, "→").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(pal.Accent).Shrink(0).SingleLine()
					ui.Row(c).Grow(1).Basis(0).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
						ui.Text(c, e.Time).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.Text).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
					})
				})
				if clickable && busyAction == "" && row.Clicked() {
					onAction(actID, cloneValues(values))
				}
				continue
			}

			dotColor, showDot := logToneDotColor(pal, e.Tone)
			if e.Ratio > 0 {
				showDot = false
			}
			row.Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
					if showDot {
						ui.Box(c).Size(theme.Space6, theme.Space6).Radius(theme.RadiusPill).Background(dotColor).Shrink(0)
					}
					ui.Text(c, e.Title).FontSize(titleSize).FixedLineHeight(titleLine).
						TextColor(pal.Text).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
					if strings.TrimSpace(e.SubText) != "" {
						ui.Text(c, e.SubText).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).MinWidth(0).Shrink(2).SingleLine().Ellipsis("…")
					}
				})
				if hasStatus || hasTime {
					ui.Row(c).Gap(theme.Space12).AlignItems(ui.Center).Shrink(0).Children(func() {
						if hasStatus {
							ui.Row(c).Width(statusColW).Justify(ui.End).AlignItems(ui.Center).Shrink(0).Children(func() {
								if strings.TrimSpace(e.Status) != "" {
									ui.Text(c, e.Status).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
										TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
								}
							})
						}
						if hasTime {
							ui.Row(c).Width(timeColW).Justify(ui.End).AlignItems(ui.Center).Shrink(0).Children(func() {
								if strings.TrimSpace(e.Time) != "" {
									if sidebar {
										ui.Text(c, e.Time).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
											TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
									} else {
										ui.Text(c, e.Time).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
											TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
									}
								}
							})
						}
					})
				}
			})
			if clickable && busyAction == "" && row.Clicked() {
				onAction(actID, cloneValues(values))
			}
		}
	})
}

func renderPreviewItem(c *ui.Context, pluginID string, pv *plugins.UIPreview) {
	pal := theme.Ocean()
	pcol := pal.PluginPreview(pv.Palette)

	frame := ui.Column(c.Key("plugin-preview-"+pluginID+"-"+pv.Title)).
		FillWidth().MinWidth(0).
		Padding(theme.Space16).Gap(theme.Space12).
		Radius(theme.RadiusLG).Background(pcol.OuterBG).Border(1, pcol.OuterBorder)
	bui.Probe(frame, "plugin-preview-"+pluginID)

	frame.Children(func() {
		// Meta header bar: Title + Badge + resolved Subtitle (e.g. target path in ~/Downloads)
		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space4).Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, pv.Title).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				if strings.TrimSpace(pv.Badge) != "" {
					ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).Radius(theme.RadiusPill).
						Background(pal.Selected).Border(1, ui.Transparent).AlignItems(ui.Center).Shrink(0).Children(func() {
						ui.Text(c, pv.Badge).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
					})
				}
			})
			if strings.TrimSpace(pv.Subtitle) != "" {
				ui.Text(c, pv.Subtitle).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
			}
		})

		switch pv.Variant {
		case "cards":
			renderPreviewCards(c, pv, pcol)
		case "diff":
			renderPreviewDiff(c, pv, pcol)
		case "article":
			renderPreviewArticle(c, pv, pcol)
		default:
			renderPreviewPlan(c, pv, pcol)
		}
	})
}

func renderPreviewArticle(c *ui.Context, pv *plugins.UIPreview, pcol theme.PluginPreviewColors) {
	ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space16).Radius(theme.RadiusLG).
		Background(pcol.CanvasBG).Border(1, pcol.CardBorder).Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space16, theme.Space20).Gap(theme.Space10).
			Radius(theme.RadiusMD).Background(pcol.CardBG).Border(1, pcol.CardBorder).Children(func() {
			lines := splitPreviewLines(pv.Body, 14)
			for _, rawLine := range lines {
				line := strings.TrimSpace(rawLine)
				if line == "" {
					continue
				}
				switch {
				case strings.HasPrefix(line, "# "):
					ui.Text(c, strings.TrimSpace(strings.TrimPrefix(line, "# "))).
						FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).
						FontWeight(theme.WeightTitle).TextColor(pcol.TitleFG)
				case strings.HasPrefix(line, "## "):
					ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(theme.EdgeStripIdleWidth, theme.FontTitle).Radius(theme.RadiusXS).Background(pcol.AccentFG).Shrink(0)
						ui.Text(c, strings.TrimSpace(strings.TrimPrefix(line, "## "))).
							FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
							FontWeight(theme.WeightTitle).TextColor(pcol.AccentFG).Grow(1).MinWidth(0)
					})
				case strings.HasPrefix(line, "> "):
					ui.Row(c).FillWidth().MinWidth(0).Padding(theme.Space8, theme.Space12).Gap(theme.Space8).
						Radius(theme.RadiusSM).Background(pcol.AccentSoft).AlignItems(ui.Center).Children(func() {
						ui.Text(c, strings.TrimSpace(strings.TrimPrefix(line, "> "))).
							FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pcol.BodyFG).Grow(1).MinWidth(0)
					})
				case strings.HasPrefix(line, "[") && strings.Contains(line, "] "):
					ui.Text(c, line).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(pcol.MutedFG)
				default:
					ui.Text(c, line).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pcol.BodyFG)
				}
			}
		})
	})
}

func renderPreviewCards(c *ui.Context, pv *plugins.UIPreview, pcol theme.PluginPreviewColors) {
	if len(pv.Cards) == 0 {
		renderPreviewArticle(c, pv, pcol)
		return
	}
	vpW, _ := c.Size()
	cols := 3
	if len(pv.Cards) < cols {
		cols = len(pv.Cards)
	}
	if vpW > 0 && vpW < 820 && cols > 2 {
		cols = 2
	}
	ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space12).Gap(theme.Space12).
		Radius(theme.RadiusLG).Background(pcol.CanvasBG).Border(1, pcol.CardBorder).Children(func() {
		for i := 0; i < len(pv.Cards); i += cols {
			start := i
			end := min(i+cols, len(pv.Cards))
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Stretch).Children(func() {
				for j := start; j < end; j++ {
					card := pv.Cards[j]
					cardBG := pcol.CardBG
					if j == 0 && len(pv.Cards) > 1 {
						cardBG = pcol.AccentSoft
					}
					ui.Column(c).Grow(1).Basis(0).MinWidth(0).
						Padding(theme.Space12, theme.Space16).Gap(theme.Space8).
						Radius(theme.RadiusMD).Background(cardBG).Border(1, pcol.CardBorder).Children(func() {
						if strings.TrimSpace(card.Badge) != "" {
							ui.Text(c, card.Badge).Font("monospace").FontSize(theme.FontMeta).
								FixedLineHeight(theme.LineMeta).TextColor(pcol.MutedFG).SingleLine()
						}
						ui.Text(c, card.Title).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
							FontWeight(theme.WeightControl).TextColor(pcol.AccentFG).SingleLine().Ellipsis("…")
						for _, ln := range splitPreviewLines(card.Body, 5) {
							if strings.TrimSpace(ln) == "" {
								continue
							}
							ui.Text(c, ln).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pcol.BodyFG)
						}
					})
				}
			})
		}
	})
}

func renderPreviewDiff(c *ui.Context, pv *plugins.UIPreview, pcol theme.PluginPreviewColors) {
	pal := theme.Ocean()
	ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space10).Children(func() {
		if strings.TrimSpace(pv.Before) != "" {
			ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space10, theme.Space12).Gap(theme.Space4).
				Radius(theme.RadiusMD).Background(pcol.BeforeBG).Border(1, pcol.BeforeBorder).Children(func() {
				ui.Text(c, theme.Tr("处理前", "Before")).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					FontWeight(theme.WeightControl).TextColor(pal.TextSecondary)
				for _, ln := range splitPreviewLines(pv.Before, 6) {
					ui.Text(c, ln).Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
				}
			})
		}
		if strings.TrimSpace(pv.After) != "" {
			ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space10, theme.Space12).Gap(theme.Space4).
				Radius(theme.RadiusMD).Background(pcol.AfterBG).Border(1, pal.Hairline).Children(func() {
				ui.Text(c, theme.Tr("处理后", "After")).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					FontWeight(theme.WeightControl).TextColor(pal.Accent)
				for _, ln := range splitPreviewLines(pv.After, 6) {
					ui.Text(c, ln).Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.Text)
				}
			})
		}
		if strings.TrimSpace(pv.Body) != "" {
			for _, ln := range splitPreviewLines(pv.Body, 6) {
				if strings.TrimSpace(ln) == "" {
					continue
				}
				ui.Text(c, ln).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
			}
		}
	})
}

func renderPreviewPlan(c *ui.Context, pv *plugins.UIPreview, pcol theme.PluginPreviewColors) {
	pal := theme.Ocean()
	bg, fg, muted := planListColors(pal, pv.Palette, pcol)
	ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space12, theme.Space16).Gap(theme.Space6).
		Radius(theme.RadiusMD).Background(bg).Border(1, pal.Hairline).Children(func() {
		lines := splitPreviewLines(pv.Body, 10)
		if len(lines) == 0 {
			ui.Text(c, pv.Title).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(muted)
			return
		}
		for _, ln := range lines {
			trimmed := strings.TrimSpace(ln)
			if trimmed == "" {
				continue
			}
			ui.Text(c, trimmed).Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(fg)
		}
	})
}

// planListColors keeps an operational change list on the app surface. paper and
// warm are fixed light sheets; using them in dark mode paints light text on white.
func planListColors(pal theme.Palette, preset string, pcol theme.PluginPreviewColors) (bg, fg, muted ui.Color) {
	if pal.Dark && (preset == "paper" || preset == "warm") {
		return pal.Card, pal.Text, pal.TextSecondary
	}
	return pcol.CardBG, pcol.BodyFG, pcol.MutedFG
}

func splitPreviewLines(raw string, maxLines int) []string {
	norm := strings.ReplaceAll(raw, "\r\n", "\n")
	rawLines := strings.Split(norm, "\n")
	out := make([]string, 0, min(len(rawLines), maxLines))
	for _, ln := range rawLines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		out = append(out, ln)
		if len(out) >= maxLines {
			break
		}
	}
	return out
}

func mapButtonStyle(v plugins.ButtonVariant) bui.ActionButtonStyle {
	switch v {
	case plugins.VariantPrimary:
		return bui.ButtonPrimary
	case plugins.VariantDanger:
		return bui.ButtonDanger
	default:
		return bui.ButtonSecondary
	}
}

func resolveActionButtonLabel(actionID, baseLabel string, isBusy, isSuccess bool) string {
	id := strings.ToLower(strings.TrimSpace(actionID))
	if isBusy {
		switch id {
		case "test_conn":
			return theme.Tr("测试中…", "Testing…")
		case "save_config":
			return theme.Tr("保存中…", "Saving…")
		case "sync_now", "confirm_sync", "confirm_sync_card", "__retry_note_saved__":
			return theme.Tr("同步中…", "Syncing…")
		case "cancel_confirm":
			return theme.Tr("取消中…", "Cancelling…")
		case "upload_now":
			return theme.Tr("上传中…", "Uploading…")
		case "rescan":
			return theme.Tr("扫描中…", "Scanning…")
		case "run_import", "run_import_card":
			return theme.Tr("迁入中…", "Importing…")
		case "refresh":
			return theme.Tr("刷新中…", "Refreshing…")
		default:
			if theme.IsEn() {
				return "Working…"
			}
			trimmed := strings.TrimSpace(baseLabel)
			for _, pfx := range []string{"立即", "开始", "重新"} {
				trimmed = strings.TrimPrefix(trimmed, pfx)
			}
			if idx := strings.Index(trimmed, " ("); idx > 0 {
				trimmed = trimmed[:idx]
			}
			if trimmed == "" || strings.HasSuffix(trimmed, "…") {
				return baseLabel
			}
			return trimmed + "中…"
		}
	}
	if isSuccess {
		switch id {
		case "test_conn":
			return theme.Tr("连接可用", "Connected")
		case "save_config":
			return theme.Tr("已保存", "Saved")
		case "sync_now", "confirm_sync", "confirm_sync_card":
			return theme.Tr("已同步", "Synced")
		case "upload_now":
			return theme.Tr("已上传", "Uploaded")
		case "rescan":
			return theme.Tr("已扫描", "Scanned")
		case "run_import", "run_import_card":
			return theme.Tr("已迁入", "Imported")
		case "refresh":
			return theme.Tr("已刷新", "Refreshed")
		default:
			return theme.Tr("已完成", "Done")
		}
	}
	return baseLabel
}

func resolveBusyActionHint(actionID string) string {
	switch strings.ToLower(strings.TrimSpace(actionID)) {
	case "test_conn":
		return theme.Tr("正在验证远端连接…", "Verifying remote connection…")
	case "save_config":
		return theme.Tr("正在保存配置…", "Saving configuration…")
	case "sync_now", "confirm_sync", "confirm_sync_card":
		return theme.Tr("正在执行同步…", "Syncing notes…")
	case "upload_now":
		return theme.Tr("正在上传图片…", "Uploading images…")
	case "rescan":
		return theme.Tr("正在扫描笔记图片…", "Scanning note images…")
	case "run_import", "run_import_card":
		return theme.Tr("正在迁入笔记与附件…", "Importing notes and assets…")
	case "refresh":
		return theme.Tr("正在刷新统计…", "Refreshing metrics…")
	default:
		return theme.Tr("正在处理…", "Processing…")
	}
}

func resolveActionButtonIcon(actionID string) *icons.Icon {
	var ic icons.Icon
	switch strings.ToLower(strings.TrimSpace(actionID)) {
	case "test_conn", "test_connection", "ping":
		ic = icons.Plug
	case "save_config", "save", "apply":
		ic = icons.Check
	case "sync_now", "confirm_sync", "confirm_sync_card", "rescan", "refresh", "reload":
		ic = icons.Refresh
	case "upload_now", "upload":
		ic = icons.Upload
	case "run_import", "run_import_card", "import", "download", "export":
		ic = icons.Download
	case "cancel_confirm", "cancel":
		ic = icons.Close
	case "clear", "reset", "delete", "remove":
		ic = icons.Trash
	default:
		return nil
	}
	return &ic
}

func renderPluginActionButton(
	c *ui.Context,
	idPrefix string,
	pluginID string,
	btn plugins.UIButton,
	fb ActionFeedback,
) ui.Element {
	isBusy := fb.BusyAction == btn.ID
	isSuccess := fb.BusyAction == "" && fb.LastActionButtonActive && fb.LastActionID == btn.ID && fb.LastActionTone == "success"
	disabled := fb.BusyAction != "" && !isBusy

	style := mapButtonStyle(btn.Variant)
	var leading *icons.Icon
	if isSuccess {
		style = bui.ButtonSuccess
		chk := icons.Check
		leading = &chk
	} else {
		leading = resolveActionButtonIcon(btn.ID)
	}
	label := resolveActionButtonLabel(btn.ID, btn.Label, isBusy, isSuccess)
	opts := bui.ActionButtonOptions{
		ID:                 idPrefix + pluginID + "-" + btn.ID,
		Label:              label,
		AccessibilityLabel: label,
		Leading:            leading,
		Style:              style,
		Disabled:           disabled,
		Loading:            isBusy,
		Truncate:           fb.CompactLayout,
	}
	if fb.SidebarLayout {
		opts.Height = theme.HeightCompact
		opts.PaddingX = theme.Space10
	}
	return bui.ActionButtonWithOptions(c, opts)
}

func renderActionsItem(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	values map[string]any,
	fb ActionFeedback,
	onAction func(actionID string, currentValues map[string]any),
) {
	pal := theme.Ocean()

	activeBusyID := ""
	matchedResult := false
	resTone := strings.ToLower(strings.TrimSpace(fb.LastActionTone))
	resMsg := strings.TrimSpace(fb.LastActionMessage)

	for _, btn := range item.Buttons {
		if fb.BusyAction != "" && fb.BusyAction == btn.ID {
			activeBusyID = btn.ID
		}
		if fb.BusyAction == "" {
			if fb.LastActionID != "" && fb.LastActionID == btn.ID {
				matchedResult = true
			} else if fb.FailedActionID != "" && fb.FailedActionID == btn.ID && strings.TrimSpace(fb.Error) != "" {
				matchedResult = true
				resTone = "danger"
				resMsg = strings.TrimSpace(fb.Error)
			}
		}
	}

	showCallout := activeBusyID == "" && matchedResult && resMsg != "" && (resTone == "danger" || resTone == "warning")
	showInlineStatus := activeBusyID != "" || (matchedResult && resMsg != "" && !showCallout)

	renderStatusNode := func(stacked bool) {
		if activeBusyID != "" {
			hint := resolveBusyActionHint(activeBusyID)
			row := ui.Row(c.Key("plugin-act-busy-hint-" + pluginID + "-" + activeBusyID)).
				Gap(theme.Space6).AlignItems(ui.Center).
				Transition(bui.CascadeRowTransition(0))
			if stacked {
				row.FillWidth().MinWidth(0)
			} else {
				row.Grow(1).MinWidth(theme.SelectCompactMinWidth)
			}
			row.Children(func() {
				ui.Text(c, hint).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					TextColor(pal.TextSecondary).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
			})
			return
		}
		if showInlineStatus {
			firstID := item.Buttons[0].ID
			dotCol, hasDot := logToneDotColor(pal, resTone)
			statusWrap := ui.Row(c.Key("plugin-act-status-" + pluginID + "-" + firstID)).
				Gap(theme.Space6).AlignItems(ui.Center).
				Transition(bui.CascadeRowTransition(0))
			if stacked {
				statusWrap.FillWidth().MinWidth(0)
			} else {
				statusWrap.Grow(1).MinWidth(theme.SelectCompactMinWidth)
			}
			bui.WrappedTooltip(c, statusWrap, resMsg)
			statusWrap.Children(func() {
				if hasDot && !fb.SidebarLayout {
					ui.Box(c).Size(theme.Space6, theme.Space6).Radius(theme.RadiusPill).Background(dotCol).Shrink(0)
				}
				msgColor := pal.TextSecondary
				if fb.SidebarLayout && resTone == "success" {
					msgColor = pal.Success
				}
				ui.Text(c, resMsg).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					TextColor(msgColor).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
			})
		}
	}

	colGap := theme.Space10
	if fb.CompactLayout {
		colGap = theme.Space6
	}
	ui.Column(c).FillWidth().MinWidth(0).Gap(colGap).Children(func() {
		if showCallout {
			bgCol := pal.DangerSurface
			bdCol := pal.Danger.Alpha(0.35)
			fgCol := pal.Danger
			if resTone == "warning" {
				bgCol = pal.WarningSurface
				bdCol = pal.Warning.Alpha(0.40)
				fgCol = pal.Warning
			}
			firstID := item.Buttons[0].ID
			ui.Row(c.Key("plugin-act-callout-"+pluginID+"-"+firstID)).FillWidth().MinWidth(0).
				Padding(theme.Space8, theme.Space12).Gap(theme.Space8).Radius(theme.RadiusMD).
				Background(bgCol).Border(1, bdCol).AlignItems(ui.Center).
				Transition(bui.CascadeRowTransition(0)).Children(func() {
				ui.Text(c, resMsg).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					TextColor(fgCol).Grow(1).MinWidth(0)
			})
		}

		if fb.CompactLayout {
			if showInlineStatus {
				renderStatusNode(true)
			}
			btnRow := ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center)
			if fb.SidebarLayout {
				btnRow.Justify(ui.Start)
			} else {
				btnRow.Wrap().Justify(ui.End)
			}
			btnRow.Children(func() {
				for _, btn := range item.Buttons {
					b := btn
					el := renderPluginActionButton(c, "plugin-act-", pluginID, b, fb)
					if el.Clicked() && fb.BusyAction == "" && onAction != nil {
						onAction(b.ID, cloneValues(values))
					}
				}
			})
			return
		}

		justify := ui.End
		if showInlineStatus {
			justify = ui.SpaceBetween
		}
		ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(theme.Space12).AlignItems(ui.Center).Justify(justify).Children(func() {
			renderStatusNode(false)

			ui.Row(c).Wrap().Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.End).Shrink(0).Children(func() {
				for _, btn := range item.Buttons {
					b := btn
					el := renderPluginActionButton(c, "plugin-act-", pluginID, b, fb)
					if el.Clicked() && fb.BusyAction == "" && onAction != nil {
						onAction(b.ID, cloneValues(values))
					}
				}
			})
		})
	})
}

func compactFolderPath(path string) string {
	display := theme.DisplayFolderPath(path)
	if len([]rune(display)) <= 44 {
		return display
	}
	parts := strings.Split(filepath.ToSlash(display), "/")
	if len(parts) <= 3 {
		return display
	}
	tail := strings.Join(parts[len(parts)-3:], "/")
	return "…/" + tail
}

func renderFieldLabel(c *ui.Context, text string) {
	pal := theme.Ocean()
	ui.Text(c, text).
		FontSize(theme.FontSmall).
		FixedLineHeight(theme.LineSmall).
		FontWeight(theme.WeightRegular).
		TextColor(pal.TextSecondary).
		SingleLine()
}

func checklistSelectionSet(item plugins.UIItem, values map[string]any) (map[string]bool, int) {
	selected, ok := plugins.StringSliceValue(values[item.ID])
	if !ok {
		selected, _ = plugins.StringSliceValue(item.Value)
	}
	selectedSet := make(map[string]bool, len(selected))
	for _, v := range selected {
		selectedSet[v] = true
	}
	selectedCount := 0
	for _, opt := range item.Options {
		if selectedSet[opt.Value] {
			selectedCount++
		}
	}
	return selectedSet, selectedCount
}

func commitChecklistSelection(
	item plugins.UIItem,
	values map[string]any,
	nextSet map[string]bool,
	onChange func(controlID string, nextValue any, immediate bool),
) {
	next := make([]string, 0, len(item.Options))
	for _, opt := range item.Options {
		if nextSet[opt.Value] {
			next = append(next, opt.Value)
		}
	}
	values[item.ID] = next
	if onChange != nil {
		onChange(item.ID, next, true)
	}
}

func renderChecklistSelectAllAction(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	values map[string]any,
	disabled bool,
	onChange func(controlID string, nextValue any, immediate bool),
) {
	_, selectedCount := checklistSelectionSet(item, values)
	allSelected := selectedCount == len(item.Options)
	toggleLabel := theme.Tr("全选", "Select All")
	if allSelected {
		toggleLabel = theme.Tr("全不选", "Clear All")
	}
	actBtn := bui.SettingsTextAction(c, "plugin-chk-all-"+pluginID+"-"+item.ID, toggleLabel, nil)
	if disabled {
		actBtn.Disabled(true).Opacity(theme.DisabledOpacity)
	} else if actBtn.Clicked() {
		nextSet := make(map[string]bool, len(item.Options))
		if !allSelected {
			for _, opt := range item.Options {
				nextSet[opt.Value] = true
			}
		}
		commitChecklistSelection(item, values, nextSet, onChange)
	}
}

func renderChecklistAddFolderAction(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	bindings *InputBindings,
	disabled bool,
) {
	label := theme.Tr("添加文件夹", "Add Folder")
	canPick := bindings != nil && bindings.PickChecklistFolder != nil
	addBtn := bui.IconButton(c, icons.Plus, bui.IconButtonOptions{
		ID:       "plugin-chk-add-" + pluginID + "-" + item.ID,
		Label:    label,
		Tooltip:  label,
		Variant:  bui.IconChrome,
		IconSize: theme.IconInline,
		Disabled: disabled || !canPick,
	})
	if !disabled && canPick && addBtn.Clicked() {
		bindings.PickChecklistFolder(item.ID, label)
	}
}

func commitChecklistRemoval(
	item plugins.UIItem,
	values map[string]any,
	removed string,
	onChange func(controlID string, nextValue any, immediate bool),
) {
	current, ok := plugins.StringSliceValue(values[item.ID])
	if !ok {
		current, _ = plugins.StringSliceValue(item.Value)
	}
	next := make([]string, 0, len(current))
	for _, entry := range current {
		if entry != removed {
			next = append(next, entry)
		}
	}
	values[item.ID] = next
	if onChange != nil {
		onChange(item.ID, next, true)
	}
}

func renderChecklistFolderRows(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	values map[string]any,
	disabled bool,
	onChange func(controlID string, nextValue any, immediate bool),
) {
	pal := theme.Ocean()
	selectedSet, _ := checklistSelectionSet(item, values)
	rows := make([]plugins.UIOption, 0, len(item.Options))
	for _, opt := range item.Options {
		if selectedSet[opt.Value] {
			rows = append(rows, opt)
		}
	}
	if len(rows) == 0 {
		empty := strings.TrimSpace(item.EmptyText)
		if empty == "" {
			empty = theme.Tr("点击右上角 + 添加文件夹", "Click + to add a folder")
		}
		ui.Row(c).FillWidth().MinWidth(0).Padding(theme.Space12, theme.Space16).Radius(theme.RadiusLG).
			Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
			ui.Text(c, empty).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
		})
		return
	}
	ui.Column(c.Key("plugin-chk-card-"+pluginID+"-"+item.ID)).FillWidth().MinWidth(0).
		Radius(theme.RadiusLG).Background(pal.Card).Border(1, pal.Hairline).Clip().Children(func() {
		for idx, opt := range rows {
			o := opt
			rowID := fmt.Sprintf("plugin-chk-%s-%s-%s", pluginID, item.ID, o.Value)
			row := ui.Row(c.Key(rowID)).FillWidth().MinWidth(0).
				MinHeight(theme.SettingsListRowHeight).Padding(theme.Space6, theme.Space16).
				Gap(theme.Space12).AlignItems(ui.Center)
			if idx > 0 {
				row.BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
			}
			row.Children(func() {
				icons.Draw(c, icons.Folder, theme.IconAction, pal.DuoIconColors(pal.TextSecondary))
				ui.Text(c, o.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine().Shrink(0)
				if strings.TrimSpace(o.Badge) != "" {
					ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).Radius(theme.RadiusXS).
						Background(pal.Shell).AlignItems(ui.Center).Shrink(0).Children(func() {
						ui.Text(c, o.Badge).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).SingleLine()
					})
				}
				ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
					if strings.TrimSpace(o.Detail) == "" {
						return
					}
					detail := ui.Text(c, o.Detail).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(pal.TextSecondary).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
					if strings.TrimSpace(o.Tooltip) != "" {
						bui.WrappedTooltip(c, detail, o.Tooltip)
					}
				})
				removeLabel := theme.Tr("移除", "Remove")
				removeBtn := bui.IconButton(c, icons.Close, bui.IconButtonOptions{
					ID:       "plugin-chk-rm-" + pluginID + "-" + item.ID + "-" + o.Value,
					Label:    removeLabel,
					Tooltip:  removeLabel,
					Variant:  bui.IconChrome,
					IconSize: theme.IconInline,
					Disabled: disabled,
				})
				if !disabled && removeBtn.Clicked() {
					commitChecklistRemoval(item, values, o.Value, onChange)
				}
			})
		}
	})
}

func renderChecklistItem(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	values map[string]any,
	busyAction string,
	hideHeader bool,
	bindings *InputBindings,
	onChange func(controlID string, nextValue any, immediate bool),
) {
	pal := theme.Ocean()
	disabled := busyAction != ""

	selectedSet, _ := checklistSelectionSet(item, values)

	ui.Column(c.Key("plugin-chk-" + pluginID + "-" + item.ID)).FillWidth().MinWidth(0).Gap(theme.Space8).Children(func() {
		if item.AddFolder {
			if !hideHeader {
				ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					if strings.TrimSpace(item.Label) != "" {
						renderFieldLabel(c, item.Label)
					} else {
						ui.Box(c).Grow(1)
					}
					renderChecklistAddFolderAction(c, pluginID, item, bindings, disabled)
				})
			}
			renderChecklistFolderRows(c, pluginID, item, values, disabled, onChange)
			return
		}
		if len(item.Options) == 0 {
			empty := strings.TrimSpace(item.EmptyText)
			if empty == "" {
				empty = theme.Tr("暂无可选项", "No options available")
			}
			ui.Row(c).FillWidth().MinWidth(0).Padding(theme.Space12, theme.Space16).Radius(theme.RadiusLG).
				Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
				ui.Text(c, empty).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
			})
			return
		}

		hasLabel := strings.TrimSpace(item.Label) != ""
		if !hideHeader && (hasLabel || len(item.Options) > 1) {
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				if hasLabel {
					renderFieldLabel(c, item.Label)
				} else {
					ui.Box(c).Grow(1)
				}
				if len(item.Options) > 1 {
					renderChecklistSelectAllAction(c, pluginID, item, values, disabled, onChange)
				}
			})
		}

		// Unified single card with hairline dividers between rows.
		ui.Column(c.Key("plugin-chk-card-"+pluginID+"-"+item.ID)).FillWidth().MinWidth(0).
			Radius(theme.RadiusLG).Background(pal.Card).Border(1, pal.Hairline).Clip().Children(func() {
			for idx, opt := range item.Options {
				o := opt
				checked := selectedSet[o.Value]
				rowChecked := checked
				rowID := fmt.Sprintf("plugin-chk-%s-%s-%s", pluginID, item.ID, o.Value)
				cursor := ui.CursorPointer
				if disabled {
					cursor = ui.CursorDefault
				}
				row := ui.CheckboxBase(c.Key(rowID), &rowChecked).Row().FillWidth().MinWidth(0).
					MinHeight(theme.SettingsListRowHeight).Padding(theme.Space6, theme.Space16).
					Gap(theme.Space12).AlignItems(ui.Center).
					Cursor(cursor).Disabled(disabled).DimDisabled(false).Label(o.Label)
				if idx > 0 {
					row.BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
				}
				hovered := row.Hovered() && !disabled
				pressed := row.Pressed() && !disabled

				bg := ui.Transparent
				if pressed {
					bg = pal.Selected
				} else if hovered {
					bg = pal.Hover
				}
				bui.AnimateSurfaceColors(row, bg, ui.Transparent, false)
				if disabled {
					row.Opacity(theme.DisabledOpacity)
				}
				if strings.TrimSpace(o.Tooltip) != "" {
					bui.WrappedTooltip(c, row, o.Tooltip)
				}
				bui.Probe(row, rowID)

				row.Children(func() {
					boxBG := ui.Transparent
					boxBD := pal.BtnBorder
					if checked {
						boxBG = pal.Accent
						boxBD = pal.Accent
					} else if hovered {
						boxBD = pal.Accent
					}
					box := ui.Box(c).Size(theme.IconAction, theme.IconAction).Radius(theme.RadiusXS).Center().Shrink(0)
					bui.AnimateSurfaceColors(box, boxBG, boxBD, true)
					box.Children(func() {
						if checked {
							icons.Draw(c, icons.Check, theme.IconMicro, pal.MonoIconColors(pal.AccentText))
						}
					})

					nameCol := pal.Text
					nameWeight := theme.WeightControl
					if !checked && !hovered {
						nameCol = pal.TextSecondary
						nameWeight = theme.WeightRegular
					}
					ui.Text(c, o.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(nameWeight).TextColor(nameCol).SingleLine().Shrink(0)

					if strings.TrimSpace(o.Badge) != "" {
						ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).Radius(theme.RadiusXS).
							Background(pal.Shell).AlignItems(ui.Center).Shrink(0).Children(func() {
							ui.Text(c, o.Badge).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								TextColor(pal.TextSecondary).SingleLine()
						})
					}

					if strings.TrimSpace(o.Detail) != "" {
						ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
							ui.Text(c, o.Detail).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								TextColor(pal.TextSecondary).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
						})
					} else {
						ui.Box(c).Grow(1)
					}
				})

				if !disabled && rowChecked != checked {
					nextSet := make(map[string]bool, len(selectedSet)+1)
					for k, v := range selectedSet {
						nextSet[k] = v
					}
					if rowChecked {
						nextSet[o.Value] = true
					} else {
						delete(nextSet, o.Value)
					}
					commitChecklistSelection(item, values, nextSet, onChange)
				}
			}
		})
	})
}

func cloneValues(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if slice, ok := v.([]string); ok {
			cp := make([]string, len(slice))
			copy(cp, slice)
			out[k] = cp
		} else {
			out[k] = v
		}
	}
	return out
}
