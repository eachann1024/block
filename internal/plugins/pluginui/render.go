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
	SelectState        func(controlID string) *bui.DropdownSelectState
	TreemapState       func(controlID string) *bui.TreemapState
	OnTreemapSelect    func()
	ResolveDraft       func(controlID, fallback string) string
	OnDraftChanged     func(controlID, next string)
	IsSecretShown      func(controlID string) bool
	ToggleSecret       func(controlID string)
	PickDirectory      func(controlID, title string)
	PickFile           func(controlID, title string, extensions []string)
	IsCardExpanded     func(cardID string, defaultCollapsed bool) bool
	ToggleCardExpanded func(cardID string, defaultCollapsed bool)
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

	root := ui.Column(c.Key("plugin-page-" + pluginID)).FillWidth().MinWidth(0).Gap(theme.Space24)
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
					FontSize(theme.FontPage).
					FixedLineHeight(theme.LinePage).
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
						el := bui.ActionButton(c, "plugin-act-"+pluginID+"-"+b.ID, b.Label, nil, mapButtonStyle(b.Variant), false)
						if el.Clicked() && onAction != nil {
							onAction(b.ID, cloneValues(values))
						}
					}
				})
			}
		})

		renderPageCards(c, pluginID, page, values, "", nil, wrappedChange, onAction)
	})
}

func renderStatusText(c *ui.Context, text string) {
	pal := theme.Ocean()
	el := ui.Text(c, text).
		FontSize(theme.FontSmall).
		FixedLineHeight(theme.LineSmall).
		TextColor(pal.TextSecondary).
		MaxWidth(280).
		MinWidth(0).
		Shrink(1).
		SingleLine().
		Ellipsis("…")
	bui.WrappedTooltip(c, el, text)
}

func renderPageCards(
	c *ui.Context,
	pluginID string,
	page *plugins.UIPage,
	values map[string]any,
	busyAction string,
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

	ui.Column(c.Key("plugin-cards-" + pluginID)).FillWidth().MinWidth(0).Gap(theme.Space24).Children(func() {
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
			bui.Card(c, func() {
				ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space16).Children(func() {
					hasTitle := strings.TrimSpace(cd.Title) != ""
					hasBadge := strings.TrimSpace(cd.Badge) != ""
					hasMeta := strings.TrimSpace(cd.Meta) != ""
					if hasTitle || hasBadge || hasMeta || cd.Collapsible {
						ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
							ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Grow(1).MinWidth(0).Children(func() {
								if hasTitle {
									ui.Text(c, cd.Title).
										FontSize(theme.FontTitle).
										FixedLineHeight(theme.LineTitle).
										FontWeight(theme.WeightTitle).
										TextColor(pal.Text).
										MinWidth(0).
										Shrink(1).
										SingleLine().
										Ellipsis("…")
								}
								if hasBadge {
									ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).Radius(theme.RadiusPill).
										Background(pal.Selected).AlignItems(ui.Center).Shrink(0).Children(func() {
										ui.Text(c, cd.Badge).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
											FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
									})
								}
							})
							if hasMeta || cd.Collapsible {
								ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Shrink(0).Children(func() {
									if hasMeta {
										ui.Text(c, cd.Meta).
											FontSize(theme.FontSmall).
											FixedLineHeight(theme.LineSmall).
											TextColor(pal.TextSecondary).
											Shrink(0).
											SingleLine()
									}
									if cd.Collapsible {
										toggleLabel := theme.Tr("展开配置", "Expand")
										if expanded {
											toggleLabel = theme.Tr("收起配置", "Collapse")
										}
										toggleBtn := bui.ActionButton(c, "plugin-card-toggle-"+pluginID+"-"+cd.ID, toggleLabel, nil, bui.ButtonSecondary, false)
										if toggleBtn.Clicked() && bindings != nil && bindings.ToggleCardExpanded != nil {
											bindings.ToggleCardExpanded(cd.ID, cd.DefaultCollapsed)
										}
									}
								})
							}
						})
					}
					if expanded {
						for idx, item := range visibleItems {
							if item.Kind == plugins.KindActions && idx > 0 {
								ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
							}
							renderItem(c, pluginID, item, values, busyAction, bindings, onChange, onAction)
						}
					}
				})
			})
		}
	})
}

func renderItem(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	values map[string]any,
	busyAction string,
	bindings *InputBindings,
	onChange func(controlID string, nextValue any, immediate bool),
	onAction func(actionID string, currentValues map[string]any),
) {
	pal := theme.Ocean()
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
			ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Gap(theme.Space16).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					TextColor(pal.Text).SingleLine().Shrink(0)
				ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
					if bui.DropdownSelect(c, "plugin-ctrl-"+pluginID+"-"+item.ID, &curStr, selOpts, bindings.SelectState(item.ID), item.Placeholder) {
						values[item.ID] = curStr
						if onChange != nil {
							onChange(item.ID, curStr, true)
						}
					}
				})
			})
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
					ShellTrack:    true,
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
					ShellTrack:    true,
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
		ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Gap(theme.Space16).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				TextColor(pal.Text).SingleLine().Shrink(0)
			ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				if bui.SegmentedControl(c, "plugin-ctrl-"+pluginID+"-"+item.ID, &activeIdx, segOpts) {
					commitSeg()
				}
			})
		})

	case plugins.KindChecklist:
		renderChecklistItem(c, pluginID, item, values, busyAction, onChange)

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
			for _, field := range visibleFields {
				f := field
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
					renderItem(c, pluginID, f, values, busyAction, bindings, onChange, onAction)
				})
			}
		})

	case plugins.KindSwitch:
		curBool, ok := values[item.ID].(bool)
		if !ok {
			curBool, _ = item.Value.(bool)
		}
		ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Gap(theme.Space16).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			onLbl := theme.Tr("已关闭", "Off")
			if curBool {
				onLbl = theme.Tr("已开启", "On")
			}
			if bui.TogglePill(c, "plugin-sw-"+pluginID+"-"+item.ID, onLbl, &curBool, busyAction != "") {
				values[item.ID] = curBool
				if onChange != nil {
					onChange(item.ID, curBool, true)
				}
			}
		})

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
					pathBox := ui.Row(c.Key("plugin-folder-box-"+pluginID+"-"+item.ID)).
						Grow(1).MinWidth(0).Height(theme.HeightControl).PaddingX(theme.Space10).Gap(theme.Space8).
						AlignItems(ui.Center).Radius(theme.RadiusMD).Background(pal.Shell).Border(1, pal.BtnBorder)
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
						if path != "" {
							icCol = pal.DuoIconColors(pal.Text)
							displayPath = compactFolderPath(path)
							textCol = pal.Text
						}
						icons.Draw(c, icons.Folder, theme.IconAction, icCol)
						ui.Text(c, displayPath).Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(textCol).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					})
					pick := bui.ActionButton(c, "plugin-folder-pick-"+pluginID+"-"+item.ID, label, nil, bui.ButtonSecondary, disabled)
					if !disabled && pick.Clicked() {
						bindings.PickDirectory(item.ID, item.Label)
					}
					if hasFilePicker {
						pickFile := bui.ActionButton(c, "plugin-file-pick-"+pluginID+"-"+item.ID, fileLabel, nil, bui.ButtonSecondary, fileDisabled)
						if !fileDisabled && pickFile.Clicked() {
							bindings.PickFile(item.ID, item.Label, item.AcceptExtensions)
						}
					}
				})
			})
			return
		}
		ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Gap(theme.Space16).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				TextColor(pal.Text).SingleLine().Shrink(0)
			ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.End).Children(func() {
				if path != "" {
					folder := ui.Text(c.Key("plugin-folder-name-"+pluginID+"-"+item.ID), filepath.Base(path)).
						FontSize(theme.FontBody).TextColor(pal.TextSecondary).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
					bui.WrappedTooltip(c, folder, path)
				}
				pick := bui.ActionButton(c, "plugin-folder-pick-"+pluginID+"-"+item.ID, label, nil, bui.ButtonSecondary, disabled)
				if !disabled {
					bui.WrappedTooltip(c, pick, path)
					if pick.Clicked() {
						bindings.PickDirectory(item.ID, item.Label)
					}
				}
				if hasFilePicker {
					pickFile := bui.ActionButton(c, "plugin-file-pick-"+pluginID+"-"+item.ID, fileLabel, nil, bui.ButtonSecondary, fileDisabled)
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
				Height(theme.HeightControl).
				PaddingX(theme.Space10).
				Radius(theme.RadiusMD).
				Background(pal.Shell).
				Border(1, pal.BtnBorder).
				TextColor(pal.Text).
				PlaceholderColor(pal.Placeholder).
				Grow(1).
				MinWidth(0)
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
				if secretShown {
					toggleLbl = theme.Tr("隐藏", "Hide")
				}
				tBtn := bui.ActionButton(c, "plugin-secret-toggle-"+pluginID+"-"+item.ID, toggleLbl, nil, bui.ButtonSecondary, false)
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
		ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Gap(theme.Space16).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				TextColor(pal.Text).SingleLine().Shrink(0)
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
								FontWeight(theme.WeightTitle).TextColor(valCol).SingleLine().Ellipsis("…")
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
		ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.End).Children(func() {
			for _, btn := range item.Buttons {
				b := btn
				el := bui.ActionButton(c, "plugin-act-"+pluginID+"-"+b.ID, b.Label, nil, mapButtonStyle(b.Variant), busyAction != "")
				if el.Clicked() && onAction != nil {
					onAction(b.ID, cloneValues(values))
				}
			}
		})

	case plugins.KindKV:
		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space10).Children(func() {
			for _, row := range item.Rows {
				rw := row
				ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, rw.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						TextColor(pal.Text).SingleLine().Shrink(0)
					value := ui.Text(c, rw.Value).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						TextColor(pal.TextSecondary).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
					bui.WrappedTooltip(c, value, rw.Tooltip)
				})
			}
		})

	case plugins.KindLogList:
		renderLogListItem(c, pluginID, item)
	}
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

func renderLogListItem(c *ui.Context, pluginID string, item plugins.UIItem) {
	if strings.TrimSpace(item.Label) != "" {
		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space8).Children(func() {
			renderFieldLabel(c, item.Label)
			renderLogEntries(c, pluginID, item)
		})
		return
	}
	renderLogEntries(c, pluginID, item)
}

func renderLogEntries(c *ui.Context, pluginID string, item plugins.UIItem) {
	pal := theme.Ocean()
	if len(item.Entries) == 0 {
		if strings.TrimSpace(item.EmptyText) != "" {
			ui.Text(c, item.EmptyText).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
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
		if timeColW < 56 {
			timeColW = 56
		}
		if timeColW > 148 {
			timeColW = 148
		}
	}

	ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
		for idx, entry := range item.Entries {
			e := entry
			rowKey := fmt.Sprintf("plugin-log-%s-%s-%d", pluginID, item.ID, idx)
			row := ui.Row(c.Key(rowKey)).FillWidth().MinWidth(0).
				Height(theme.HeightControl).PaddingX(theme.Space10).Gap(theme.Space12).
				Radius(theme.RadiusSM).Background(pal.Sidebar).
				AlignItems(ui.Center).Justify(ui.SpaceBetween)

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
						p.Fill(ui.Rect{X: bounds.X, Y: bounds.Y, W: w, H: bounds.H}, barColor, theme.RadiusSM)
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
					ui.Text(c, e.Title).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
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
									ui.Text(c, e.Time).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
										TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
								}
							})
						}
					})
				}
			})
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
					FontWeight(theme.WeightTitle).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
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
							FontWeight(theme.WeightTitle).TextColor(pcol.AccentFG).SingleLine().Ellipsis("…")
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
				Radius(theme.RadiusMD).Background(pcol.AfterBG).Border(1, pcol.AfterBorder).Children(func() {
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
	ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space12, theme.Space16).Gap(theme.Space6).
		Radius(theme.RadiusMD).Background(pcol.CardBG).Border(1, pcol.CardBorder).Children(func() {
		lines := splitPreviewLines(pv.Body, 10)
		if len(lines) == 0 {
			ui.Text(c, pv.Title).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
			return
		}
		for _, ln := range lines {
			trimmed := strings.TrimSpace(ln)
			if trimmed == "" {
				continue
			}
			ui.Text(c, trimmed).Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.Text)
		}
	})
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
		FontWeight(theme.WeightControl).
		TextColor(pal.TextSecondary).
		SingleLine()
}

func renderChecklistItem(
	c *ui.Context,
	pluginID string,
	item plugins.UIItem,
	values map[string]any,
	busyAction string,
	onChange func(controlID string, nextValue any, immediate bool),
) {
	pal := theme.Ocean()
	disabled := busyAction != ""

	selected, ok := plugins.StringSliceValue(values[item.ID])
	if !ok {
		selected, _ = plugins.StringSliceValue(item.Value)
	}
	selectedSet := make(map[string]bool, len(selected))
	for _, v := range selected {
		selectedSet[v] = true
	}

	commit := func(nextSet map[string]bool) {
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

	ui.Column(c.Key("plugin-chk-"+pluginID+"-"+item.ID)).FillWidth().MinWidth(0).Gap(theme.Space8).Children(func() {
		if len(item.Options) == 0 {
			empty := strings.TrimSpace(item.EmptyText)
			if empty == "" {
				empty = theme.Tr("暂无可选项", "No options available")
			}
			ui.Row(c).FillWidth().MinWidth(0).Padding(theme.Space12, theme.Space12).Radius(theme.RadiusMD).
				Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
				ui.Text(c, empty).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			})
			return
		}

		selectedCount := 0
		for _, opt := range item.Options {
			if selectedSet[opt.Value] {
				selectedCount++
			}
		}
		hasLabel := strings.TrimSpace(item.Label) != ""
		if hasLabel || len(item.Options) > 1 {
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				if hasLabel {
					renderFieldLabel(c, item.Label)
				} else {
					ui.Box(c).Grow(1)
				}
				if len(item.Options) > 1 {
					allSelected := selectedCount == len(item.Options)
					toggleLabel := theme.Tr("全选", "Select All")
					if allSelected {
						toggleLabel = theme.Tr("取消全选", "Clear All")
					}
					chip := bui.Chip(c, bui.ChipOptions{
						ID:    "plugin-chk-all-" + pluginID + "-" + item.ID,
						Label: toggleLabel,
						Style: bui.ChipMuted,
					})
					if !disabled && chip.Clicked() {
						nextSet := make(map[string]bool, len(item.Options))
						if !allSelected {
							for _, opt := range item.Options {
								nextSet[opt.Value] = true
							}
						}
						commit(nextSet)
					}
				}
			})
		}

		for idx, opt := range item.Options {
			o := opt
			checked := selectedSet[o.Value]
			rowID := fmt.Sprintf("plugin-chk-%s-%s-%d", pluginID, item.ID, idx)
			cursor := ui.CursorPointer
			if disabled {
				cursor = ui.CursorDefault
			}
			row := ui.ButtonBase(c.Key(rowID)).FillWidth().MinWidth(0).
				MinHeight(theme.HeightLarge+theme.Space8).Padding(theme.Space6, theme.Space12).
				Gap(theme.Space10).AlignItems(ui.Center).Radius(theme.RadiusMD).
				Cursor(cursor).Disabled(disabled).DimDisabled(false).Label(o.Label)
			hovered := row.Hovered() && !disabled
			pressed := row.Pressed() && !disabled

			bg := pal.Card
			bd := pal.Hairline
			if checked {
				bg = pal.Selected
				bd = pal.SelectedBorder
				if hovered {
					bd = pal.BtnBorder
				}
			} else if pressed {
				bg = pal.Selected
				bd = pal.SelectedBorder
			} else if hovered {
				bg = pal.Hover
				bd = pal.BtnBorder
			}
			bui.AnimateSurfaceColors(row, bg, bd, true)
			bui.TactileCard(row, hovered, pressed, checked, disabled)
			if disabled {
				row.Opacity(theme.DisabledOpacity)
			}
			if strings.TrimSpace(o.Tooltip) != "" {
				bui.WrappedTooltip(c, row, o.Tooltip)
			}
			bui.Probe(row, rowID)

			row.Children(func() {
				boxBG := pal.Card
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

				icCol := pal.MonoIconColors(pal.TextSecondary)
				if checked || hovered {
					icCol = pal.DuoIconColors(pal.Text)
				}
				icons.Draw(c, icons.Folder, theme.IconRow, icCol)

				nameCol := pal.TextSecondary
				nameWeight := theme.WeightRegular
				if checked {
					nameCol = pal.Text
					nameWeight = theme.WeightControl
				} else if hovered {
					nameCol = pal.Text
				}
				ui.Text(c, o.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					FontWeight(nameWeight).TextColor(nameCol).SingleLine().Shrink(0)

				if strings.TrimSpace(o.Badge) != "" {
					ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).Radius(theme.RadiusSM).
						Background(pal.Card).Border(1, pal.Hairline).AlignItems(ui.Center).Shrink(0).Children(func() {
						ui.Text(c, o.Badge).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).SingleLine()
					})
				}

				if strings.TrimSpace(o.Detail) != "" {
					ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
						ui.Text(c, o.Detail).Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(pal.TextSecondary).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
					})
				} else {
					ui.Box(c).Grow(1)
				}
			})

			if !disabled && row.Clicked() {
				nextSet := make(map[string]bool, len(selectedSet)+1)
				for k, v := range selectedSet {
					nextSet[k] = v
				}
				if checked {
					delete(nextSet, o.Value)
				} else {
					nextSet[o.Value] = true
				}
				commit(nextSet)
			}
		}
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
