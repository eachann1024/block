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
	iconPickerMaxHeight float32 = 288
	iconPickerMinHeight float32 = 180
	iconPickerPadX      float32 = 10
	iconPickerRailBtn   float32 = 26
	iconPickerHeaderH   float32 = 42
	iconPickerFooterH   float32 = 42
	iconPickerCell      float32 = 34
	iconPickerCellGap   float32 = 2
	iconPickerSwatch    float32 = 16
	iconPickerCols              = 6
)

// placeIconPicker resolves (x, y, maxH) for the icon picker popover so it never
// obscures the clicked icon.
//
// Tier 1: float directly right of the icon tile (or right of the full switcher
// row when anchor == "switcher"), top edge level with the row.
// Tier 2 (titlebar anchor or window too narrow): place strictly below or
// strictly above the icon while capping height to available vertical room so
// clamping never pulls the popover back on top of the icon.
func placeIconPicker(iconRect, rowRect ui.Rect, anchor string, popW, popH, vpW, vpH float32) (ox, oy, finalH float32) {
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

	if anchor != "titlebar" {
		sideRef := iconRect
		if anchor == "switcher" && rowRect.W > 0 && rowRect.H > 0 {
			sideRef = rowRect
		}
		if sideRef.W > 0 && sideRef.H > 0 {
			rightOfRef := sideRef.X + sideRef.W + gapX
			if rightOfRef+popW <= vpW-margin {
				// Start level with the clicked row so the popover never rises above it.
				anchorY := sideRef.Y
				if rowRect.H > 0 && rowRect.Y <= sideRef.Y && rowRect.Y+rowRect.H >= sideRef.Y+sideRef.H {
					anchorY = rowRect.Y
				}
				return round(rightOfRef), round(clampY(anchorY, finalH)), finalH
			}
		}
	}

	rectAnchor := iconRect
	if rectAnchor.W <= 0 && rectAnchor.H <= 0 {
		rectAnchor = rowRect
	}
	if rectAnchor.W <= 0 && rectAnchor.H <= 0 {
		rectAnchor = ui.Rect{X: 24, Y: 80, W: 22, H: 22}
	}

	minX := margin
	maxX := max(vpW-margin-popW, minX)
	ox = max(min(rectAnchor.X, maxX), minX)

	spaceBelow := (vpH - margin) - (rectAnchor.Y + rectAnchor.H + gapY)
	spaceAbove := (rectAnchor.Y - gapY) - margin
	if spaceBelow >= finalH || spaceBelow >= spaceAbove {
		if spaceBelow > 0 {
			finalH = min(finalH, max(spaceBelow, 120))
		}
		oy = rectAnchor.Y + rectAnchor.H + gapY
		if oy+finalH > vpH-margin {
			oy = max(vpH-margin-finalH, rectAnchor.Y+rectAnchor.H+2)
		}
	} else {
		if spaceAbove > 0 {
			finalH = min(finalH, max(spaceAbove, 120))
		}
		oy = max(rectAnchor.Y-gapY-finalH, margin)
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

func sidebarNotebookIcon(p SidebarProps, nbRoot string) (plugins.ResolvedFileIcon, bool) {
	if nbRoot == "" {
		return plugins.ResolvedFileIcon{}, false
	}
	if ic, ok := p.NotebookIcons[nbRoot]; ok {
		return ic, true
	}
	cleanTarget := filepath.Clean(nbRoot)
	for k, ic := range p.NotebookIcons {
		if filepath.Clean(k) == cleanTarget {
			return ic, true
		}
	}
	if p.ActiveNotebook != "" && filepath.Clean(p.ActiveNotebook) == cleanTarget {
		if ic, ok := p.FileIcons[plugins.NotebookFileIconRelPath]; ok {
			return ic, true
		}
	}
	return plugins.ResolvedFileIcon{}, false
}

func iconPickerTarget(state *SidebarUIState, activeNB string) (nbRoot, rel string) {
	if state == nil || state.IconPickerPath == "" {
		return "", ""
	}
	nbRoot = state.IconPickerNotebook
	if nbRoot == "" {
		nbRoot = activeNB
	}
	if nbRoot == "" {
		return "", ""
	}
	if filepath.Clean(state.IconPickerPath) == filepath.Clean(nbRoot) {
		return nbRoot, plugins.NotebookFileIconRelPath
	}
	return nbRoot, sidebarRelPath(nbRoot, state.IconPickerPath)
}

func renderSidebarIconPicker(c *ui.Context, p SidebarProps, scrim ui.Element, vpW, vpH float32) {
	card := renderSidebarIconPickerCard(c, p, scrim, vpW, vpH)
	if !card.Valid() {
		return
	}
	if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
		p.State.ClosePopups()
	}
}

func renderSidebarIconPickerCard(c *ui.Context, p SidebarProps, scrim ui.Element, vpW, vpH float32) ui.Element {
	pack := p.FileIconPack
	if pack == nil || pack.Pack == nil || len(pack.Pack.Icons) == 0 || p.OnSetFileIcon == nil {
		p.State.CloseIconPicker()
		return ui.Element{}
	}
	nbRoot, rel := iconPickerTarget(p.State, p.ActiveNotebook)
	if nbRoot == "" || rel == "" {
		p.State.CloseIconPicker()
		return ui.Element{}
	}

	pal := theme.Ocean()
	loc := iconPickerLocale()
	var curIcon plugins.ResolvedFileIcon
	var hasCustom bool
	if rel == plugins.NotebookFileIconRelPath {
		curIcon, hasCustom = sidebarNotebookIcon(p, nbRoot)
	} else {
		curIcon, hasCustom = p.FileIcons[rel]
	}
	activeTint := p.State.IconPickerTint
	if hasCustom {
		activeTint = theme.NormalizeIconTint(curIcon.Tint)
		p.State.IconPickerTint = activeTint
	} else {
		activeTint = theme.NormalizeIconTint(string(activeTint))
	}

	popW := min(iconPickerWidth, max(vpW-16, 220))
	popH := min(iconPickerMaxHeight, max(vpH-16, iconPickerMinHeight))
	ox, oy, finalH := placeIconPicker(p.State.IconPickerIconBounds, p.State.IconPickerRowBounds, p.State.IconPickerAnchor, popW, popH, vpW, vpH)

	catIcons := make(map[string][]plugins.FileIconDef, len(pack.Pack.Categories))
	for _, ic := range pack.Pack.Icons {
		catIcons[ic.Category] = append(catIcons[ic.Category], ic)
	}

	if p.State.IconPickerActiveCat == "" && len(pack.Pack.Categories) > 0 {
		p.State.IconPickerActiveCat = pack.Pack.Categories[0].ID
	}

	applyIcon := func(iconID string) {
		p.State.IconPickerTint = activeTint
		p.OnSetFileIcon(pack.PluginID, nbRoot, rel, iconID, string(activeTint))
	}

	applyTint := func(tint theme.IconTint) {
		p.State.IconPickerTint = tint
		if hasCustom {
			p.OnSetFileIcon(pack.PluginID, nbRoot, rel, curIcon.IconID, string(tint))
			return
		}
		fallback := "note"
		if p.State.IconPickerIsDir {
			fallback = "folder"
		}
		if _, ok := pack.ByID[fallback]; ok {
			p.OnSetFileIcon(pack.PluginID, nbRoot, rel, fallback, string(tint))
		}
	}

	resetIcon := func() {
		p.State.IconPickerTint = theme.IconTintNone
		p.OnSetFileIcon(pack.PluginID, nbRoot, rel, "", string(theme.IconTintNone))
	}

	showRail := len(pack.Pack.Categories) > 1
	var headerH float32
	if showRail {
		headerH = iconPickerHeaderH
	}
	bodyH := max(finalH-headerH-iconPickerFooterH-2, 80)

	c.Key("sidebar-icon-picker")
	card := blockui.PopoverSurface(
		ui.Column(c).Absolute().Left(ox).Top(oy).Width(popW).Height(finalH).Clip().Role(ui.RoleDialog).
			Label(theme.Tr("选择图标", "Choose Icon")),
	)
	blockui.Probe(card, "sidebar-icon-picker")

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
			pluginui.DrawFileIconAnimated(c, ic.Ref, spec, 20, colors, hovered || pressed || selected)
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
		// 1. Header: category quick-jump rail.
		if showRail {
			ui.Row(c.Key("icon-picker-header")).FillWidth().Height(headerH).Shrink(0).
				Padding(0, iconPickerPadX, 0, iconPickerPadX).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).
				BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
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
								pluginui.DrawFileIconAnimated(c, def.Ref, spec, theme.IconAction, colors, active || hov || prs)
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

		// 2. Scrollable icon grid
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
		if p.State.IconPickerJumpCat == "" && observedCat != "" {
			p.State.IconPickerActiveCat = observedCat
		}

		// 3. Footer: 7 solid tint swatches + icon-only Reset (matches the design).
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
						Label(theme.IconTintLabel(t))
					hov := sw.Hovered()
					// Keep the button surface clear; the selected state is drawn by the ring only.
					blockui.AnimateSurfaceColors(sw, ui.Transparent, ui.Transparent, false)
					dotSize := iconPickerSwatch
					if hov && !selected {
						dotSize = iconPickerSwatch + 2
					}
					blockui.RichTooltip(c, sw, blockui.TooltipSpec{
						Placement: blockui.TooltipAbove,
						Rows:      []blockui.TooltipRow{{Trigger: theme.IconTintLabel(t)}},
					})
					sw.Children(func() {
						// Selected: same solid dot with a small check, no ring or halo.
						ui.Box(c).Size(dotSize, dotSize).Radius(theme.RadiusPill).Background(hue).Center().
							Children(func() {
								if selected {
									icons.Draw(c, icons.Check, 11, pal.MinimalIconColors(pal.AccentText))
								}
							})
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
					icons.DrawAnimated(c, icons.Refresh, theme.IconAction, pal.DuoIconColors(fg), rHov || rPrs)
				})
				blockui.Probe(resetBtn, "icon-picker-reset")
				if hasCustom && resetBtn.Clicked() {
					resetIcon()
				}
			})
	})

	return card
}
