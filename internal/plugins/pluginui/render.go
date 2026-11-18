package pluginui

import (
	"fmt"

	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// InputBindings connects declarative KindInput/KindSecret controls to persistent string drafts
// and secret visibility toggles managed by the host controller.
type InputBindings struct {
	ResolveDraft   func(controlID, fallback string) string
	OnDraftChanged func(controlID, next string)
	IsSecretShown  func(controlID string) bool
	ToggleSecret   func(controlID string)
	PickDirectory  func(controlID, title string)
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
					renderStatusPill(c, page.Status)
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

		renderPageCards(c, pluginID, page, values, "", nil, onChange, onAction)
	})
}

func renderStatusPill(c *ui.Context, text string) {
	pal := theme.Ocean()
	ui.Row(c).Height(theme.HeightCompact).PaddingX(theme.Space10).Radius(theme.RadiusPill).
		Background(pal.Selected).Border(1, pal.Accent).AlignItems(ui.Center).Children(func() {
		ui.Text(c, text).
			FontSize(theme.FontSmall).
			FixedLineHeight(theme.LineSmall).
			FontWeight(theme.WeightControl).
			TextColor(pal.Text).
			SingleLine()
	})
}

func renderPageCards(
	c *ui.Context,
	pluginID string,
	page *plugins.UIPage,
	values map[string]any,
	busyAction string,
	bindings *InputBindings,
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
					ui.Text(c, cd.Title).
						FontSize(theme.FontTitle).
						FixedLineHeight(theme.LineTitle).
						FontWeight(theme.WeightTitle).
						TextColor(pal.Text).
						SingleLine().
						Ellipsis("…")
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
	onChange func(controlID string, nextValue any),
	onAction func(actionID string, currentValues map[string]any),
) {
	pal := theme.Ocean()
	switch item.Kind {
	case plugins.KindSegmented, plugins.KindSelect:
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
							onChange(item.ID, nextVal)
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
				TextColor(pal.Text).SingleLine().Shrink(0)
			onLbl := theme.Tr("已关闭", "Off")
			if curBool {
				onLbl = theme.Tr("已开启", "On")
			}
			if bui.TogglePill(c, "plugin-sw-"+pluginID+"-"+item.ID, onLbl, &curBool, false) {
				values[item.ID] = curBool
				if onChange != nil {
					onChange(item.ID, curBool)
				}
			}
		})

	case plugins.KindInput, plugins.KindFolder, plugins.KindSecret:
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
				if item.Kind == plugins.KindFolder {
					in.Font("monospace")
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
						onChange(item.ID, draft)
					}
				}
				if item.Kind == plugins.KindFolder && bindings != nil && bindings.PickDirectory != nil {
					pickBtn := bui.ActionButton(c, "plugin-folder-pick-"+pluginID+"-"+item.ID, theme.Tr("选择…", "Browse…"), nil, bui.ButtonSecondary, false)
					if pickBtn.Clicked() {
						bindings.PickDirectory(item.ID, item.Label)
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
					ui.Text(c, rw.Value).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
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
