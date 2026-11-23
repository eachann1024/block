package app

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/plugins/pluginui"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	iconPickerWidth     float32 = 316
	iconPickerMaxHeight float32 = 382
	iconPickerMinHeight float32 = 200
	iconPickerHeaderH   float32 = 46
	iconPickerRailH     float32 = 36
	iconPickerFooterH   float32 = 42
	iconPickerCols              = 6
)

// placeIconPicker resolves (x, y, maxH) for the icon picker popover so it never
// obscures the clicked sidebar icon.
//
// Tier 1 (normal desktop): float right of the sidebar row, vertically aligned
// with the icon, keeping both the icon and note/folder title fully visible.
// Tier 2 (narrow window): float right of the icon button itself so the icon
// remains unobstructed.
// Tier 3 (ultra-narrow window): place strictly below or strictly above the icon
// button while capping height to available vertical room so clamping never pulls
// the popover back on top of the icon.
func placeIconPicker(iconRect, rowRect ui.Rect, popW, popH, vpW, vpH float32) (ox, oy, finalH float32) {
	const (
		margin float32 = 8
		gapX   float32 = 10
		gapY   float32 = 6
	)
	availH := max(vpH-margin*2, 120)
	finalH = min(popH, availH)
	clampY := func(raw, h float32) float32 {
		minY := margin
		maxY := max(vpH-margin-h, minY)
		return max(min(raw, maxY), minY)
	}
	round := func(v float32) float32 {
		return float32(math.Round(float64(v)))
	}

	if rowRect.W > 0 {
		rightOfRow := rowRect.X + rowRect.W + gapX
		if rightOfRow+popW <= vpW-margin {
			anchorY := iconRect.Y - 12
			if iconRect.H <= 0 {
				anchorY = rowRect.Y - 8
			}
			return round(rightOfRow), round(clampY(anchorY, finalH)), finalH
		}
	}

	if iconRect.W > 0 {
		rightOfIcon := iconRect.X + iconRect.W + gapX
		if rightOfIcon+popW <= vpW-margin {
			return round(rightOfIcon), round(clampY(iconRect.Y-12, finalH)), finalH
		}
	}

	anchor := iconRect
	if anchor.W <= 0 && anchor.H <= 0 {
		anchor = rowRect
	}
	if anchor.W <= 0 && anchor.H <= 0 {
		anchor = ui.Rect{X: 24, Y: 80, W: 22, H: 22}
	}

	minX := margin
	maxX := max(vpW-margin-popW, minX)
	ox = max(min(anchor.X, maxX), minX)

	spaceBelow := (vpH - margin) - (anchor.Y + anchor.H + gapY)
	spaceAbove := (anchor.Y - gapY) - margin
	if spaceBelow >= finalH || spaceBelow >= spaceAbove {
		if spaceBelow > 0 {
			finalH = min(finalH, max(spaceBelow, 120))
		}
		oy = anchor.Y + anchor.H + gapY
		if oy+finalH > vpH-margin {
			oy = max(vpH-margin-finalH, anchor.Y+anchor.H+2)
		}
	} else {
		if spaceAbove > 0 {
			finalH = min(finalH, max(spaceAbove, 120))
		}
		oy = max(anchor.Y-gapY-finalH, margin)
	}
	return round(ox), round(oy), finalH
}

func iconPickerLocale() string {
	return theme.Tr("zh", "en")
}

func sidebarRelPath(notebookRoot, targetPath string) string {
	if notebookRoot == "" || targetPath == "" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Clean(notebookRoot), filepath.Clean(targetPath))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

func matchFileIconDef(ic plugins.FileIconDef, catName plugins.LocalizedText, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	var hay strings.Builder
	hay.WriteString(strings.ToLower(ic.ID))
	hay.WriteByte(' ')
	hay.WriteString(strings.ToLower(ic.Name.Zh))
	hay.WriteByte(' ')
	hay.WriteString(strings.ToLower(ic.Name.En))
	hay.WriteByte(' ')
	hay.WriteString(strings.ToLower(catName.Zh))
	hay.WriteByte(' ')
	hay.WriteString(strings.ToLower(catName.En))
	for _, kw := range ic.Keywords {
		hay.WriteByte(' ')
		hay.WriteString(strings.ToLower(kw))
	}
	s := hay.String()
	for _, t := range terms {
		if !strings.Contains(s, t) {
			return false
		}
	}
	return true
}

func renderSidebarIconPicker(c *ui.Context, p SidebarProps, scrim ui.Element, vpW, vpH float32) {
	pack := p.FileIconPack
	if pack == nil || pack.Pack == nil || len(pack.Pack.Icons) == 0 || p.OnSetFileIcon == nil {
		p.State.ClosePopups()
		return
	}
	rel := sidebarRelPath(p.ActiveNotebook, p.State.IconPickerPath)
	if rel == "" {
		p.State.ClosePopups()
		return
	}

	pal := theme.Ocean()
	loc := iconPickerLocale()
	curIcon, hasCustom := p.FileIcons[rel]
	activeTint := p.State.IconPickerTint
	if hasCustom {
		activeTint = theme.NormalizeIconTint(curIcon.Tint)
		p.State.IconPickerTint = activeTint
	} else {
		activeTint = theme.NormalizeIconTint(string(activeTint))
	}

	popW := min(iconPickerWidth, max(vpW-16, 220))
	popH := min(iconPickerMaxHeight, max(vpH-16, iconPickerMinHeight))
	ox, oy, finalH := placeIconPicker(p.State.IconPickerIconBounds, p.State.IconPickerRowBounds, popW, popH, vpW, vpH)

	queryTrim := strings.TrimSpace(p.State.IconPickerQuery)
	if p.State.IconPickerQuery != p.State.IconPickerLastQuery {
		p.State.IconPickerLastQuery = p.State.IconPickerQuery
		p.State.IconPickerScroll.Y = 0
	}
	searching := queryTrim != ""

	catByID := make(map[string]plugins.FileIconCategory, len(pack.Pack.Categories))
	catIcons := make(map[string][]plugins.FileIconDef, len(pack.Pack.Categories))
	for _, cat := range pack.Pack.Categories {
		catByID[cat.ID] = cat
	}
	var filtered []plugins.FileIconDef
	if searching {
		terms := strings.Fields(strings.ToLower(queryTrim))
		for _, ic := range pack.Pack.Icons {
			if matchFileIconDef(ic, catByID[ic.Category].Name, terms) {
				filtered = append(filtered, ic)
			}
		}
	} else {
		for _, ic := range pack.Pack.Icons {
			catIcons[ic.Category] = append(catIcons[ic.Category], ic)
		}
	}

	if !searching && p.State.IconPickerActiveCat == "" && len(pack.Pack.Categories) > 0 {
		p.State.IconPickerActiveCat = pack.Pack.Categories[0].ID
	}

	applyIcon := func(iconID string) {
		p.State.IconPickerTint = activeTint
		p.OnSetFileIcon(pack.PluginID, rel, iconID, string(activeTint))
	}

	applyTint := func(tint theme.IconTint) {
		p.State.IconPickerTint = tint
		if hasCustom {
			p.OnSetFileIcon(pack.PluginID, rel, curIcon.IconID, string(tint))
			return
		}
		fallback := "note"
		if p.State.IconPickerIsDir {
			fallback = "folder"
		}
		if _, ok := pack.ByID[fallback]; ok {
			p.OnSetFileIcon(pack.PluginID, rel, fallback, string(tint))
		}
	}

	resetIcon := func() {
		p.State.IconPickerTint = theme.IconTintNone
		p.OnSetFileIcon(pack.PluginID, rel, "", string(theme.IconTintNone))
	}

	railH := float32(0)
	if !searching && len(pack.Pack.Categories) > 1 {
		railH = iconPickerRailH
	}
	bodyH := max(finalH-iconPickerHeaderH-railH-iconPickerFooterH-2, 80)

	c.Key("sidebar-icon-picker")
	card := blockui.PopoverSurface(
		ui.Column(c).Absolute().Left(ox).Top(oy).Width(popW).Height(finalH).Clip().Role(ui.RoleDialog).
			Label(theme.Tr("选择图标", "Choose Icon")),
	)
	blockui.Probe(card, "sidebar-icon-picker")

	if p.State.IconPickerFocus {
		p.State.IconPickerInput.Focus()
		p.State.IconPickerFocus = false
	}

	renderIconCell := func(keyPrefix string, ic plugins.FileIconDef, allowReveal bool) {
		selected := hasCustom && curIcon.IconID == ic.ID
		cell := ui.ButtonBase(c.Key(keyPrefix+"-"+ic.ID)).
			Grow(1).MinWidth(0).Height(38).Center().Radius(theme.RadiusSM).
			Cursor(ui.CursorPointer).FocusRing(false).
			Selected(selected).Label(ic.Name.Resolve(loc))
		hovered := cell.Hovered()
		pressed := cell.Pressed()

		bg := ui.Transparent
		bd := ui.Transparent
		switch {
		case selected:
			bg = pal.Accent.Alpha(0.14)
			if pal.Dark {
				bg = pal.Accent.Alpha(0.24)
			}
			bd = pal.Accent.Alpha(0.55)
		case hovered || pressed:
			bg = pal.MenuHover
		}
		blockui.AnimateSurfaceColors(cell, bg, bd, false)
		blockui.TactileButton(cell, hovered, pressed, false)
		blockui.RichTooltip(c, cell, blockui.TooltipSpec{
			Placement: blockui.TooltipAbove,
			Rows:      []blockui.TooltipRow{{Trigger: ic.Name.Resolve(loc)}},
		})

		lineCol := pal.TextSecondary
		if selected || hovered || pressed {
			lineCol = pal.Text
		}
		colors := pal.TintedDuoIconColors(activeTint, lineCol, hovered || selected)
		var spec plugins.DuoIconSpec
		if ic.Spec != nil {
			spec = *ic.Spec
		}
		cell.Children(func() {
			pluginui.DrawFileIcon(c, ic.Ref, spec, theme.IconRow, colors)
		})
		blockui.Probe(cell, "icon-picker-cell:"+ic.ID)

		if allowReveal && selected && p.State.IconPickerRevealCurrent {
			cell.ScrollIntoView(ui.ScrollIntoViewOptions{
				Block: ui.ScrollCenter,
			})
			p.State.IconPickerRevealCurrent = false
		}
		if cell.Clicked() {
			applyIcon(ic.ID)
		}
	}

	renderGrid := func(keyPrefix string, list []plugins.FileIconDef, allowReveal bool) {
		for start := 0; start < len(list); start += iconPickerCols {
			end := min(start+iconPickerCols, len(list))
			row := list[start:end]
			ui.Row(c.Key(fmt.Sprintf("%s-r%d", keyPrefix, start))).FillWidth().Gap(theme.Space4).Children(func() {
				for _, ic := range row {
					renderIconCell(keyPrefix, ic, allowReveal)
				}
				for pad := len(row); pad < iconPickerCols; pad++ {
					ui.Box(c).Grow(1).MinWidth(0).Height(38)
				}
			})
		}
	}

	card.Children(func() {
		// 1. Search header
		ui.Row(c.Key("icon-picker-header")).FillWidth().Height(iconPickerHeaderH).Shrink(0).
			Padding(theme.Space8, theme.Space10, theme.Space8, theme.Space10).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
			Children(func() {
				var in ui.Element
				searchBox := ui.Row(c.Key("icon-picker-search-box")).Grow(1).MinWidth(0).Height(30).
					Padding(0, theme.Space8, 0, theme.Space8).Gap(theme.Space6).
					AlignItems(ui.Center).Radius(theme.RadiusMD).Background(pal.Paper)
				searchBox.Children(func() {
					icons.Draw(c, icons.FindSearch, theme.IconInline, pal.MonoIconColors(pal.TextSecondary))
					ui.Box(c.Key("icon-picker-input-wrap")).Grow(1).MinWidth(0).Height(28).Clip().Children(func() {
						in = ui.TextInputBase(c.Key("icon-picker-input"), &p.State.IconPickerQuery).
							FillWidth().Height(28).Padding(0).
							Label(theme.Tr("搜索图标", "Search icons")).
							FontSize(theme.FontSmall).FixedLineHeight(28).
							TextColor(pal.Text).CaretColor(pal.Accent).
							Bind(&p.State.IconPickerInput).AutoFocus()
						blockui.Probe(in, "icon-picker-input")
						if p.State.IconPickerQuery == "" && !in.Composing() {
							ph := fmt.Sprintf(theme.Tr("搜索 %d 个图标…", "Search %d icons…"), len(pack.Pack.Icons))
							ui.Row(c.Key("icon-picker-ph")).
								Absolute().Top(0).Bottom(0).Left(theme.Space2).Right(0).
								AlignItems(ui.Center).IgnoreInput(true).
								Children(func() {
									ui.Text(c, ph).FontSize(theme.FontSmall).TextColor(pal.Placeholder).SingleLine()
								})
						}
					})
					if p.State.IconPickerQuery != "" {
						clr := ui.ButtonBase(c.Key("icon-picker-clear")).Size(18, 18).Shrink(0).Center().
							Radius(theme.RadiusXS).Cursor(ui.CursorPointer).FocusRing(false).
							Label(theme.Tr("清除搜索", "Clear search"))
						clrHot := clr.Hovered() || clr.Pressed()
						if clrHot {
							clr.Background(pal.MenuHover)
						}
						clr.Children(func() {
							icons.Draw(c, icons.Close, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary))
						})
						if clr.Clicked() {
							p.State.IconPickerQuery = ""
							p.State.IconPickerScroll.Y = 0
							p.State.IconPickerInput.Focus()
						}
					}
				})
				bd := pal.Hairline
				if in.Valid() && in.Focused() {
					bd = pal.Accent
				}
				searchBox.Border(1, bd)
				if in.Valid() && !in.Composing() {
					in.OnTextKey(func(k ui.TextKey) bool {
						switch k.Key {
						case ui.KeyEscape:
							if p.State.IconPickerQuery != "" {
								p.State.IconPickerQuery = ""
								p.State.IconPickerScroll.Y = 0
							} else {
								p.State.ClosePopups()
							}
							return true
						case ui.KeyEnter:
							if searching && len(filtered) > 0 {
								applyIcon(filtered[0].ID)
								return true
							}
						}
						return false
					})
				}
			})

		// 2. Category quick-jump rail (hidden during search)
		if !searching && len(pack.Pack.Categories) > 1 {
			ui.Row(c.Key("icon-picker-rail")).FillWidth().Height(iconPickerRailH).Shrink(0).
				Padding(theme.Space4, theme.Space8, theme.Space4, theme.Space8).Gap(theme.Space2).
				AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
				Children(func() {
					for _, cat := range pack.Pack.Categories {
						cat := cat
						active := p.State.IconPickerActiveCat == cat.ID
						cb := ui.ButtonBase(c.Key("icon-rail-" + cat.ID)).
							Grow(1).MinWidth(0).Height(28).Center().Radius(theme.RadiusSM).
							Cursor(ui.CursorPointer).FocusRing(false).
							Selected(active).Label(cat.Name.Resolve(loc))
						hov := cb.Hovered()
						prs := cb.Pressed()
						bg := ui.Transparent
						switch {
						case active:
							bg = pal.Selected
						case hov || prs:
							bg = pal.MenuHover
						}
						blockui.AnimateSurfaceColors(cb, bg, ui.Transparent, false)
						blockui.RichTooltip(c, cb, blockui.TooltipSpec{
							Placement: blockui.TooltipAbove,
							Rows:      []blockui.TooltipRow{{Trigger: cat.Name.Resolve(loc)}},
						})
						if def, ok := pack.ByID[cat.Icon]; ok {
							lineCol := pal.TextSecondary
							if active || hov {
								lineCol = pal.Text
							}
							colors := pal.TintedDuoIconColors(theme.IconTintNone, lineCol, active || hov)
							var spec plugins.DuoIconSpec
							if def.Spec != nil {
								spec = *def.Spec
							}
							cb.Children(func() {
								pluginui.DrawFileIcon(c, def.Ref, spec, theme.IconAction, colors)
							})
						}
						blockui.Probe(cb, "icon-rail:"+cat.ID)
						if cb.Clicked() {
							p.State.IconPickerActiveCat = cat.ID
							p.State.IconPickerJumpCat = cat.ID
						}
					}
				})
		}

		// 3. Scrollable icon grid
		scrollBox := blockui.Scroll(c.Key("icon-picker-scroll"), &p.State.IconPickerScroll).
			FillWidth().Height(bodyH).Shrink(0)
		scrollRect := scrollBox.Bounds()
		var observedCat string
		scrollBox.Children(func() {
			ui.Column(c).FillWidth().Padding(theme.Space8, theme.Space10, theme.Space10, theme.Space10).Gap(theme.Space8).Children(func() {
				if searching {
					if len(filtered) == 0 {
						ui.Column(c).FillWidth().Padding(theme.Space24, theme.Space8, theme.Space24, theme.Space8).
							AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
							ui.Text(c, theme.Tr("没有匹配的图标", "No matching icons")).
								FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
						})
						return
					}
					ui.Text(c, fmt.Sprintf(theme.Tr("找到 %d 个图标", "%d icons"), len(filtered))).
						FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine()
					renderGrid("search", filtered, false)
					return
				}

				if len(pack.Recent) > 0 {
					recDefs := make([]plugins.FileIconDef, 0, len(pack.Recent))
					for _, id := range pack.Recent {
						if def, ok := pack.ByID[id]; ok {
							recDefs = append(recDefs, def)
						}
					}
					if len(recDefs) > 0 {
						ui.Column(c.Key("icon-sec-recent")).FillWidth().Gap(theme.Space4).Children(func() {
							ui.Text(c, theme.Tr("最近使用", "Recent")).
								FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine()
							renderGrid("recent", recDefs, false)
						})
					}
				}

				for _, cat := range pack.Pack.Categories {
					list := catIcons[cat.ID]
					if len(list) == 0 {
						continue
					}
					ui.Column(c.Key("icon-sec-" + cat.ID)).FillWidth().Gap(theme.Space4).Children(func() {
						hdr := ui.Row(c.Key("icon-hdr-" + cat.ID)).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
							ui.Text(c, cat.Name.Resolve(loc)).
								FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine()
							ui.Text(c, fmt.Sprintf("%d", len(list))).
								FontSize(theme.FontMeta).TextColor(pal.Placeholder).SingleLine()
						})
						if p.State.IconPickerJumpCat == cat.ID {
							hdr.ScrollIntoView(ui.ScrollIntoViewOptions{
								Block:     ui.ScrollStart,
								Animation: &blockui.SpringTactile,
							})
							p.State.IconPickerJumpCat = ""
						}
						if scrollRect.H > 0 {
							if hb := hdr.Bounds(); hb.H > 0 && hb.Y <= scrollRect.Y+28 {
								observedCat = cat.ID
							}
						}
						renderGrid("cat-"+cat.ID, list, true)
					})
				}
			})
		})
		if !searching && p.State.IconPickerJumpCat == "" && observedCat != "" {
			p.State.IconPickerActiveCat = observedCat
		}

		// 4. Footer: 7 tint swatches + Reset to default
		ui.Row(c.Key("icon-picker-footer")).FillWidth().Height(iconPickerFooterH).Shrink(0).
			Padding(0, theme.Space10, 0, theme.Space10).AlignItems(ui.Center).Justify(ui.SpaceBetween).
			BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).
			Children(func() {
				ui.Row(c.Key("icon-picker-tints")).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
					for _, t := range theme.IconTints {
						t := t
						selected := activeTint == t
						sw := ui.ButtonBase(c.Key("icon-tint-" + string(t))).
							Size(22, 22).Shrink(0).Center().Radius(theme.RadiusPill).
							Cursor(ui.CursorPointer).FocusRing(false).
							Selected(selected).Label(theme.IconTintLabel(t))
						hov := sw.Hovered()
						if selected {
							sw.Border(1.5, pal.Accent)
						} else if hov {
							sw.Background(pal.MenuHover)
						}
						blockui.RichTooltip(c, sw, blockui.TooltipSpec{
							Placement: blockui.TooltipAbove,
							Rows:      []blockui.TooltipRow{{Trigger: theme.IconTintLabel(t)}},
						})
						dotCol := pal.IconTintSwatch(t)
						sw.Children(func() {
							dot := ui.Box(c).Size(12, 12).Radius(theme.RadiusPill).Background(dotCol)
							if t == theme.IconTintNone {
								dot.Border(1, pal.Hairline)
							}
						})
						blockui.Probe(sw, "icon-tint:"+string(t))
						if sw.Clicked() {
							applyTint(t)
						}
					}
				})

				resetBtn := ui.ButtonBase(c.Key("icon-picker-reset")).
					Height(26).Padding(0, theme.Space8, 0, theme.Space8).Shrink(0).
					AlignItems(ui.Center).Radius(theme.RadiusSM).FocusRing(false).
					Disabled(!hasCustom).Cursor(ui.CursorPointer).
					Label(theme.Tr("恢复默认", "Reset"))
				rHov := hasCustom && resetBtn.Hovered()
				rPrs := hasCustom && resetBtn.Pressed()
				rBG := ui.Transparent
				if rHov || rPrs {
					rBG = pal.MenuHover
				}
				blockui.AnimateSurfaceColors(resetBtn, rBG, ui.Transparent, false)
				fg := pal.TextSecondary
				if !hasCustom {
					fg = pal.Placeholder
				} else if rHov || rPrs {
					fg = pal.Text
				}
				resetBtn.Children(func() {
					ui.Text(c, theme.Tr("恢复默认", "Reset")).
						FontSize(theme.FontSmall).TextColor(fg).SingleLine()
				})
				blockui.Probe(resetBtn, "icon-picker-reset")
				if hasCustom && resetBtn.Clicked() {
					resetIcon()
				}
			})
	})

	if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
		p.State.ClosePopups()
	}
}
