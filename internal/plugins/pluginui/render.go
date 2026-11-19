package pluginui

import (
	"fmt"
	"path/filepath"
	"strings"

	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// InputBindings connects declarative controls to persistent string drafts,
// dropdown popover states, directory pickers, and secret visibility toggles.
type InputBindings struct {
	SelectState     func(controlID string) *bui.DropdownSelectState
	TreemapState    func(controlID string) *bui.TreemapState
	OnTreemapSelect func()
	ResolveDraft    func(controlID, fallback string) string
	OnDraftChanged  func(controlID, next string)
	IsSecretShown   func(controlID string) bool
	ToggleSecret    func(controlID string)
	PickDirectory   func(controlID, title string)
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
			bui.Card(c, func() {
				ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space16).Children(func() {
					if strings.TrimSpace(cd.Title) != "" {
						ui.Text(c, cd.Title).
							FontSize(theme.FontTitle).
							FixedLineHeight(theme.LineTitle).
							FontWeight(theme.WeightTitle).
							TextColor(pal.Text).
							SingleLine().
							Ellipsis("…")
					}
					for _, item := range visibleItems {
						renderItem(c, pluginID, item, values, busyAction, bindings, onChange, onAction)
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
		ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Gap(theme.Space16).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				TextColor(pal.Text).SingleLine().Shrink(0)
			ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				if bui.SegmentedControl(c, "plugin-ctrl-"+pluginID+"-"+item.ID, &activeIdx, segOpts) {
					if activeIdx >= 0 && activeIdx < len(item.Options) {
						nextVal := item.Options[activeIdx].Value
						values[item.ID] = nextVal
						if onChange != nil {
							onChange(item.ID, nextVal, true)
						}
					}
				}
			})
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
		ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Gap(theme.Space16).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				TextColor(pal.Text).SingleLine().Shrink(0)
			ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.End).Children(func() {
				label := theme.Tr("选择文件夹…", "Choose Folder…")
				if path != "" {
					folder := ui.Text(c.Key("plugin-folder-name-"+pluginID+"-"+item.ID), filepath.Base(path)).
						FontSize(theme.FontBody).TextColor(pal.TextSecondary).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
					bui.WrappedTooltip(c, folder, path)
					label = theme.Tr("更改…", "Change…")
				}
				disabled := busyAction != "" || bindings == nil || bindings.PickDirectory == nil
				pick := bui.ActionButton(c, "plugin-folder-pick-"+pluginID+"-"+item.ID, label, nil, bui.ButtonSecondary, disabled)
				if !disabled {
					bui.WrappedTooltip(c, pick, path)
					if pick.Clicked() {
						bindings.PickDirectory(item.ID, item.Label)
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
		ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightLarge).Gap(theme.Space16).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				TextColor(pal.Text).SingleLine().Shrink(0)
			ui.Row(c).Grow(1).MinWidth(0).MaxWidth(theme.AIPanelDefaultWidth).Gap(theme.Space8).
				AlignItems(ui.Center).Justify(ui.End).Children(func() {
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
			})
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
						ui.Column(c).Grow(1).Basis(0).MinWidth(0).
							Padding(theme.Space12, theme.Space16).Gap(theme.Space4).
							Radius(theme.RadiusLG).Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
							ui.Text(c, m.Label).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
							ui.Text(c, m.Value).Font("monospace").FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).
								FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine().Ellipsis("…")
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
		if len(item.Entries) == 0 {
			ui.Text(c, item.EmptyText).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			return
		}
		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space8).Children(func() {
			for _, entry := range item.Entries {
				e := entry
				ui.Row(c).FillWidth().MinWidth(0).PaddingY(theme.Space6).Gap(theme.Space12).
					AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, e.Title).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					meta := e.Time
					if e.Status != "" {
						meta = e.Status + " · " + e.Time
					}
					ui.Text(c, meta).Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
						TextColor(pal.TextSecondary).Shrink(0).SingleLine()
				})
			}
		})
	}
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

func cloneValues(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
