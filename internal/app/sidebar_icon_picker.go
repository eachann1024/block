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
	iconPickerWidth     float32 = 236 // 6 × 34 cells + 5 × 2 gaps + 2 × 10 padding + border
	iconPickerMaxHeight float32 = 318
	iconPickerMinHeight float32 = 200
	iconPickerPadX      float32 = 10
	iconPickerSearchH   float32 = 30
	iconPickerRailBtn   float32 = 26
	iconPickerFooterH   float32 = 42
	iconPickerCell      float32 = 34
	iconPickerCellGap   float32 = 2
	iconPickerSwatch    float32 = 16
	iconPickerCols              = 6
)

// placeIconPicker resolves (x, y, maxH) for the icon picker popover so it never
// obscures the clicked sidebar icon.
//
// Tier 1: float directly right of the icon tile, top edge level with the row.
// Tier 2 (window too narrow): place strictly below or strictly above the icon
// while capping height to available vertical room so clamping never pulls the
// popover back on top of the icon.
func placeIconPicker(iconRect, rowRect ui.Rect, popW, popH, vpW, vpH float32) (ox, oy, finalH float32) {
	const (
		margin float32 = 8
		gapX   float32 = 6
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

	if iconRect.W > 0 && iconRect.H > 0 {
		rightOfIcon := iconRect.X + iconRect.W + gapX
		if rightOfIcon+popW <= vpW-margin {
			// Start level with the clicked row so the popover never rises above it.
			anchorY := iconRect.Y
			if rowRect.H > 0 && rowRect.Y <= iconRect.Y && rowRect.Y+rowRect.H >= iconRect.Y+iconRect.H {
				anchorY = rowRect.Y
			}
			return round(rightOfIcon), round(clampY(anchorY, finalH)), finalH
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

	showRail := !searching && len(pack.Pack.Categories) > 1
	headerH := theme.Space10 + iconPickerSearchH + theme.Space6
	if showRail {
		headerH += theme.Space8 + iconPickerRailBtn
	}
	bodyH := max(finalH-headerH-iconPickerFooterH-2, 80)

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
			Size(iconPickerCell, iconPickerCell).Shrink(0).Center().Radius(theme.RadiusMD).
			Cursor(ui.CursorPointer).FocusRing(false).
			Selected(selected).Label(ic.Name.Resolve(loc))
		hovered := cell.Hovered()
		pressed := cell.Pressed()

		bg := ui.Transparent
		bd := ui.Transparent
		switch {
		case selected:
			bg = pal.Selected
			bd = pal.SelectedBorder
		case hovered || pressed:
			bg = pal.MenuHover
		}
		blockui.AnimateSurfaceColors(cell, bg, bd, false)
		blockui.TactileButton(cell, hovered, pressed, false)
		blockui.RichTooltip(c, cell, blockui.TooltipSpec{
			Placement: blockui.TooltipAbove,
			Rows:      []blockui.TooltipRow{{Trigger: ic.Name.Resolve(loc)}},
		})

		lineCol := pal.MenuIcon
		if selected || hovered || pressed {
			lineCol = pal.Text
		}
		cellTint := theme.IconTintNone
		if selected {
			cellTint = activeTint
		}
		colors := pal.TintedDuoIconColors(cellTint, lineCol, hovered || selected)
		var spec plugins.DuoIconSpec
		if ic.Spec != nil {
			spec = *ic.Spec
		}
		cell.Children(func() {
			pluginui.DrawFileIcon(c, ic.Ref, spec, 20, colors)
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
			ui.Row(c.Key(fmt.Sprintf("%s-r%d", keyPrefix, start))).FillWidth().Gap(iconPickerCellGap).Children(func() {
				for _, ic := range row {
					renderIconCell(keyPrefix, ic, allowReveal)
				}
			})
		}
	}

	card.Children(func() {
		// 1. Header: search box + category rail, no divider (matches the design).
		ui.Column(c.Key("icon-picker-header")).FillWidth().Height(headerH).Shrink(0).
			Padding(theme.Space10, iconPickerPadX, theme.Space6, iconPickerPadX).Gap(theme.Space8).
			Children(func() {
				var in ui.Element
				searchBox := ui.Row(c.Key("icon-picker-search-box")).FillWidth().MinWidth(0).Height(iconPickerSearchH).Shrink(0).
					Padding(0, theme.Space8, 0, theme.Space8).Gap(theme.Space6).
					AlignItems(ui.Center).Radius(theme.RadiusMD).Background(pal.Shell)
				searchBox.Children(func() {
					icons.Draw(c, icons.FindSearch, theme.IconInline, pal.MonoIconColors(pal.MenuMuted))
					ui.Box(c.Key("icon-picker-input-wrap")).Grow(1).MinWidth(0).Height(28).Clip().Children(func() {
						in = ui.TextInputBase(c.Key("icon-picker-input"), &p.State.IconPickerQuery).
							FillWidth().Height(28).Padding(0).
							Label(theme.Tr("搜索图标", "Search icons")).
							FontSize(theme.FontSmall).FixedLineHeight(28).
							TextColor(pal.Text).CaretColor(pal.Accent).
							Bind(&p.State.IconPickerInput).AutoFocus()
						blockui.Probe(in, "icon-picker-input")
						if p.State.IconPickerQuery == "" && !in.Composing() {
							ph := theme.Tr("搜索图标", "Search icons")
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
				bd := pal.BtnBorder
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

				// 2. Category quick-jump rail (hidden during search)
				if !showRail {
					return
				}
				ui.Row(c.Key("icon-picker-rail")).FillWidth().Height(iconPickerRailBtn).Shrink(0).
					AlignItems(ui.Center).Justify(ui.SpaceBetween).
					Children(func() {
						for _, cat := range pack.Pack.Categories {
							cat := cat
							active := p.State.IconPickerActiveCat == cat.ID
							cb := ui.ButtonBase(c.Key("icon-rail-"+cat.ID)).
								Size(iconPickerRailBtn, iconPickerRailBtn).Shrink(0).Center().Radius(theme.RadiusSM).
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
								lineCol := pal.MenuMuted
								switch {
								case active:
									lineCol = pal.Text
								case hov || prs:
									lineCol = pal.TextSecondary
								}
								colors := pal.TintedDuoIconColors(theme.IconTintNone, lineCol, false)
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
			})

		// 3. Scrollable icon grid
		scrollBox := blockui.Scroll(c.Key("icon-picker-scroll"), &p.State.IconPickerScroll).
			FillWidth().Height(bodyH).Shrink(0)
		scrollRect := scrollBox.Bounds()
		var observedCat string
		scrollBox.Children(func() {
			secLabel := func(text string) {
				ui.Row(c).FillWidth().Padding(theme.Space8, theme.Space4, theme.Space4, theme.Space4).Children(func() {
					ui.Text(c, text).FontSize(theme.FontMeta).TextColor(pal.MenuMuted).SingleLine()
				})
			}
			ui.Column(c).FillWidth().Padding(0, iconPickerPadX, theme.Space8, iconPickerPadX).Gap(iconPickerCellGap).Children(func() {
				if searching {
					if len(filtered) == 0 {
						ui.Column(c).FillWidth().Padding(theme.Space24, theme.Space8, theme.Space24, theme.Space8).
							AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
							ui.Text(c, theme.Tr("没有匹配的图标", "No matching icons")).
								FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
						})
						return
					}
					ui.Box(c).Height(theme.Space6)
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
						ui.Column(c.Key("icon-sec-recent")).FillWidth().Gap(iconPickerCellGap).Children(func() {
							secLabel(theme.Tr("最近使用", "Recent"))
							renderGrid("recent", recDefs, false)
						})
					}
				}

				for _, cat := range pack.Pack.Categories {
					list := catIcons[cat.ID]
					if len(list) == 0 {
						continue
					}
					ui.Column(c.Key("icon-sec-" + cat.ID)).FillWidth().Gap(iconPickerCellGap).Children(func() {
						hdr := ui.Row(c.Key("icon-hdr-"+cat.ID)).FillWidth().Padding(theme.Space8, theme.Space4, theme.Space4, theme.Space4).Children(func() {
							ui.Text(c, cat.Name.Resolve(loc)).FontSize(theme.FontMeta).TextColor(pal.MenuMuted).SingleLine()
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

		// 4. Footer: 7 solid tint swatches + icon-only Reset (matches the design).
		ui.Row(c.Key("icon-picker-footer")).FillWidth().Height(iconPickerFooterH).Shrink(0).
			Padding(0, iconPickerPadX, 0, iconPickerPadX).Gap(theme.Space4).AlignItems(ui.Center).
			BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).
			Children(func() {
				for _, t := range theme.IconTints {
					t := t
					selected := activeTint == t
					hue := pal.IconTintSwatch(t)
					sw := ui.ButtonBase(c.Key("icon-tint-"+string(t))).
						Size(iconPickerSwatch+4, iconPickerSwatch+4).Shrink(0).Center().Radius(theme.RadiusPill).
						Cursor(ui.CursorPointer).FocusRing(false).
						Selected(selected).Label(theme.IconTintLabel(t))
					hov := sw.Hovered()
					// Keep the button surface clear; the selected state is drawn by the ring only.
					blockui.AnimateSurfaceColors(sw, ui.Transparent, ui.Transparent, false)
					dotSize := iconPickerSwatch
					if selected {
						dotSize = iconPickerSwatch - 6
					} else if hov {
						dotSize = iconPickerSwatch + 2
					}
					blockui.RichTooltip(c, sw, blockui.TooltipSpec{
						Placement: blockui.TooltipAbove,
						Rows:      []blockui.TooltipRow{{Trigger: theme.IconTintLabel(t)}},
					})
					sw.Children(func() {
						if selected {
							// Thin hue ring, clear gap, smaller solid dot.
							ui.Box(c).Size(iconPickerSwatch+2, iconPickerSwatch+2).Radius(theme.RadiusPill).
								Border(1.5, hue).Center().Children(func() {
								ui.Box(c).Size(dotSize, dotSize).Radius(theme.RadiusPill).Background(hue)
							})
							return
						}
						ui.Box(c).Size(dotSize, dotSize).Radius(theme.RadiusPill).Background(hue)
					})
					blockui.Probe(sw, "icon-tint:"+string(t))
					if sw.Clicked() {
						applyTint(t)
					}
				}
				ui.Box(c).Grow(1)

				resetBtn := ui.ButtonBase(c.Key("icon-picker-reset")).
					Size(iconPickerRailBtn, iconPickerRailBtn).Shrink(0).Center().
					Radius(theme.RadiusSM).FocusRing(false).
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
				blockui.RichTooltip(c, resetBtn, blockui.TooltipSpec{
					Placement: blockui.TooltipAbove,
					Rows:      []blockui.TooltipRow{{Trigger: theme.Tr("恢复默认", "Reset")}},
				})
				resetBtn.Children(func() {
					icons.Draw(c, icons.Refresh, theme.IconAction, pal.MonoIconColors(fg))
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
