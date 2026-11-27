package app

import (
	"fmt"
	"strings"

	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/plugins/pluginui"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	// maxInlinePluginButtons caps how many plugin buttons a user can pin to the front.
	maxInlinePluginButtons = 3
	// Responsive titlebar breakpoints: evaluated against both viewport width and
	// remaining width after subtracting the left cell (sidebar/traffic lights) and
	// right window-control inset (Windows/Linux), so wide docked sidebars never crowd
	// out built-in tools or tabs.
	titlebarNarrowBreakpoint float32 = 720
	titlebarNarrowAvailW     float32 = 520
	titlebarTightAvailW      float32 = 420
)

func titlebarAvailableWidth(vpW, leftW, rightInset float32) float32 {
	if vpW <= 0 {
		return 0
	}
	tailPad := float32(titlebarActionTrailing)
	if !theme.IsMacOS() {
		tailPad = 0
		if rightInset > 0 {
			tailPad = rightInset + 8
		}
	}
	return max(vpW-leftW-tailPad, 1)
}

func isNarrowTitlebar(vpW, availW float32) bool {
	if vpW <= 0 {
		return false
	}
	return vpW < titlebarNarrowBreakpoint || (availW > 0 && availW < titlebarNarrowAvailW)
}

// isTightTitlebar reports when even secondary non-note clusters (like the image/SVG zoom
// pill) must fold into the page menu so tabs and built-in tools keep breathing room.
func isTightTitlebar(availW float32) bool {
	return availW > 0 && availW < titlebarTightAvailW
}

// TogglePinnedPluginSlot adds slotID at the front of the pinned list (capped at
// maxInlinePluginButtons so newly pinned plugins always earn an inline slot), or
// removes it when already pinned. When at capacity and activeSlots is provided,
// the oldest stale (uninstalled/disabled) slot is evicted first without disturbing
// the relative recency order of surviving entries.
func TogglePinnedPluginSlot(pinned []string, slotID string, activeSlots map[string]bool) []string {
	slotID = strings.TrimSpace(slotID)
	if slotID == "" {
		return pinned
	}
	cleaned := make([]string, 0, len(pinned))
	seen := make(map[string]bool, len(pinned))
	removed := false
	for _, id := range pinned {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if id == slotID {
			removed = true
			continue
		}
		cleaned = append(cleaned, id)
	}
	if removed {
		return cleaned
	}
	res := make([]string, 0, len(cleaned)+1)
	res = append(res, slotID)
	res = append(res, cleaned...)
	for len(res) > maxInlinePluginButtons {
		evictIdx := -1
		if activeSlots != nil {
			for i := len(res) - 1; i >= 1; i-- {
				if !activeSlots[res[i]] {
					evictIdx = i
					break
				}
			}
		}
		if evictIdx < 0 {
			evictIdx = len(res) - 1
		}
		res = append(res[:evictIdx], res[evictIdx+1:]...)
	}
	return res
}

// willEvictPinnedButton reports which currently visible pinned button would be
// displaced if candidateSlot were pinned right now.
func willEvictPinnedButton(p TitlebarProps, candidateSlot string) (plugins.ResolvedTitlebarButton, bool) {
	if isPluginSlotPinned(p.PinnedPluginSlots, candidateSlot) {
		return plugins.ResolvedTitlebarButton{}, false
	}
	bySlot := make(map[string]plugins.ResolvedTitlebarButton, len(p.PluginButtons))
	for _, b := range p.PluginButtons {
		slot := pluginButtonSlotID(b)
		if !slotHidden(p, slot) {
			bySlot[slot] = b
		}
	}
	livePinned := make([]plugins.ResolvedTitlebarButton, 0, maxInlinePluginButtons)
	seen := make(map[string]bool, len(p.PinnedPluginSlots))
	for _, slot := range p.PinnedPluginSlots {
		if seen[slot] {
			continue
		}
		seen[slot] = true
		if b, ok := bySlot[slot]; ok {
			livePinned = append(livePinned, b)
		}
	}
	if len(livePinned) < maxInlinePluginButtons {
		return plugins.ResolvedTitlebarButton{}, false
	}
	return livePinned[maxInlinePluginButtons-1], true
}

func isPluginSlotPinned(pinned []string, slotID string) bool {
	if slotID == "" {
		return false
	}
	for _, id := range pinned {
		if id == slotID {
			return true
		}
	}
	return false
}

// splitPluginTitlebarButtons drops user-hidden buttons, places pinned buttons first in
// their pinned order, then splits the remainder into inline and overflow slices. Narrow
// windows or wide docked sidebars keep fewer inline so built-in tools and tabs never crowd out.
func splitPluginTitlebarButtons(p TitlebarProps, vpW, availW float32) (inline, overflow []plugins.ResolvedTitlebarButton) {
	visible := make([]plugins.ResolvedTitlebarButton, 0, len(p.PluginButtons))
	for _, b := range p.PluginButtons {
		if !slotHidden(p, pluginButtonSlotID(b)) {
			visible = append(visible, b)
		}
	}
	ordered := plugins.OrderByPinned(visible, pluginButtonSlotID, p.PinnedPluginSlots)
	limit := pluginInlineLimit(p, vpW, availW, len(ordered))
	if len(ordered) <= limit {
		return ordered, nil
	}
	return ordered[:limit], ordered[limit:]
}

// pluginInlineLimit decides how many plugin buttons sit inline: every button that fits
// in the titlebar's real free space stays inline; only the rest fold into overflow.
func pluginInlineLimit(p TitlebarProps, vpW, availW float32, n int) int {
	if m := p.MenuState; m != nil && m.pluginLimitReady {
		return min(m.pluginInlineN, n)
	}
	return pluginInlineLimitFor(p, vpW, availW, n)
}

func pluginInlineLimitFor(p TitlebarProps, vpW, availW float32, n int) int {
	m := p.MenuState
	if vpW <= 0 || n == 0 || m == nil || m.pluginCenterW <= 0 {
		return n
	}
	budget := m.pluginCenterW - pluginCenterNeed(p) + m.pluginZoneW
	for limit := n; limit > 0; limit-- {
		if pluginZoneWidth(limit, n) <= budget {
			return limit
		}
	}
	return 0
}

// pluginSlotW is one titlebar button plus the tail row gap.
const pluginSlotW = float32(theme.ChromeBoxSize) + 4

func pluginZoneWidth(inline, total int) float32 {
	w := float32(inline) * pluginSlotW
	if total > inline {
		w += pluginSlotW
	}
	return w
}

// pluginCenterNeed is the center cell width the tabs/title keep before plugin buttons
// may claim the rest; multiple tabs keep their full titles, so plugins fold first.
func pluginCenterNeed(p TitlebarProps) float32 {
	if m := p.MenuState; m != nil && len(p.Tabs) > 1 && m.tabsNeedW > 0 {
		return m.tabsNeedW
	}
	switch n := len(p.Tabs); {
	case n == 0:
		return 140
	case n == 1:
		return 260
	default:
		return min(float32(n)*tabMinW+float32(theme.ChromeBoxSize)+12, 420)
	}
}

func pluginButtonSlotID(b plugins.ResolvedTitlebarButton) string {
	if b.SlotID != "" {
		return b.SlotID
	}
	return plugins.PluginTitlebarSlotID(b.PluginID, b.ID)
}

func pluginButtonLabel(b plugins.ResolvedTitlebarButton) string {
	return nonEmptyOr(b.Tooltip, b.ID)
}

func renderPluginOverflowMenu(c *ui.Context, p TitlebarProps, scrim ui.Element, availW float32) {
	pal := theme.Ocean()
	vpW, vpH := c.Size()
	_, overflow := splitPluginTitlebarButtons(p, vpW, availW)
	if len(overflow) == 0 {
		p.MenuState.Close()
		return
	}
	icManage := icons.Plug
	navItems := make([]blockui.MenuItemSpec, 0, len(overflow)+2)
	for _, raw := range overflow {
		b := raw
		slotID := pluginButtonSlotID(b)
		navItems = append(navItems, blockui.MenuItemSpec{
			ID:      "titlebar-plugin-menu:" + slotID,
			Label:   pluginButtonLabel(b),
			Checked: b.Active,
			OnClick: func() {
				if b.Panel == "" {
					p.MenuState.CloseAndRestoreFocus(c)
				} else {
					p.MenuState.Close()
				}
				if p.OnPluginButton != nil {
					p.OnPluginButton(b)
				}
			},
		})
	}
	manageIdx := -1
	if p.OnOpenPluginStore != nil {
		manageIdx = len(navItems) + 1
		navItems = append(navItems, blockui.MenuItemSpec{Separator: true}, blockui.MenuItemSpec{
			ID:    "titlebar-plugin-menu-manage",
			Label: theme.Tr("管理插件…", "Manage Plugins…"),
			Icon:  &icManage,
			OnClick: func() {
				p.MenuState.Close()
				p.OnOpenPluginStore()
			},
		})
	}

	menuW := min(float32(244), max(vpW-16, 1))
	anchor := p.MenuState.PluginMenuAnchor
	if anchor.W <= 0 {
		anchor = ui.Rect{X: max(vpW-40, 8), Y: 6, W: theme.ChromeBoxSize, H: theme.ChromeBoxSize}
	}
	spec := blockui.PlaceBelow(anchor).WithAlignX(blockui.AlignX{Kind: blockui.AlignRight})
	maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 360)
	popH := min(estimateMenuHeight(navItems)+theme.LineMeta+theme.Space8+1, maxH)
	ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

	c.Key("titlebar-plugin-menu")
	card := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).
		Focusable().FocusRing(false).AutoFocus().Role(ui.RoleMenu))
	navigateTitlebarMenu(c, card, navItems, p.MenuState)
	// Always consume bare P at the menu layer while open so it never fires a background
	// plugin shortcut, and ignore key-repeat via ShortcutRepeated so holding P does not
	// oscillate pin state. Works for both keyboard-highlighted and pointer-hovered rows.
	if card.OverlayShortcut(0, ui.KeyP) && !c.ShortcutRepeated() && p.OnTogglePinPlugin != nil {
		targetSlot := ""
		if p.MenuState.MenuCursor >= 0 && p.MenuState.MenuCursor < len(overflow) {
			targetSlot = pluginButtonSlotID(overflow[p.MenuState.MenuCursor])
		} else if p.MenuState.HoverPluginSlotID != "" {
			for _, b := range overflow {
				if pluginButtonSlotID(b) == p.MenuState.HoverPluginSlotID {
					targetSlot = p.MenuState.HoverPluginSlotID
					break
				}
			}
		}
		if targetSlot != "" {
			p.OnTogglePinPlugin(targetSlot)
		}
	}
	var kbEvictWarning string
	if p.OnTogglePinPlugin != nil && p.MenuState.MenuCursor >= 0 && p.MenuState.MenuCursor < len(overflow) {
		cand := overflow[p.MenuState.MenuCursor]
		if evicted, ok := willEvictPinnedButton(p, pluginButtonSlotID(cand)); ok {
			kbEvictWarning = fmt.Sprintf(theme.Tr("按 P 固定将替换「%s」", "Press P to pin (replaces %s)"), pluginButtonLabel(evicted))
		}
	}
	blockui.Probe(card, "titlebar-plugin-menu")
	narrow := isNarrowTitlebar(vpW, availW)
	scrollMaxH := blockui.MenuInnerMaxHeight(maxH)
	if kbEvictWarning != "" {
		// Reserve space for the sticky footer outside the scroll area so the replacement
		// warning remains visible even when the overflow list scrolls.
		scrollMaxH = max(scrollMaxH-(theme.LineMeta+theme.Space4+7+theme.MenuGap), theme.MenuItemHeight)
	}
	nextHoveredSlot := ""
	card.Children(func() {
		blockui.Scroll(c).FillWidth().MaxHeight(scrollMaxH).Children(func() {
			ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
				for i, raw := range overflow {
					b := raw
					slotID := pluginButtonSlotID(b)
					pinned := isPluginSlotPinned(p.PinnedPluginSlots, slotID)
					kbActive := i == p.MenuState.MenuCursor
					rowKey := "titlebar-plugin-menu-row:" + slotID
					row := ui.Row(c.Key(rowKey)).FillWidth().Height(theme.MenuItemHeight).
						Padding(0, theme.Space6, 0, theme.Space10).Gap(theme.Space6).
						AlignItems(ui.Center).Radius(theme.RadiusMD)
					blockui.RevealOnHighlight(row, kbActive)
					if kbActive && p.MenuState.MenuRevealCursor {
						blockui.RevealActiveItem(row)
					}
					rowHover := !navItems[i].SuppressHover && row.Hovered()
					if rowHover {
						nextHoveredSlot = slotID
					}
					rowBG, rowBorder, hasBorder := ui.Transparent, ui.Transparent, false
					switch {
					case b.Active && kbActive:
						rowBG, rowBorder, hasBorder = pal.Selected, pal.Accent, true
					case b.Active:
						rowBG = pal.Selected
					case rowHover || kbActive:
						rowBG = pal.MenuHover
					}
					blockui.AnimateSurfaceColors(row, rowBG, rowBorder, hasBorder)
					row.Children(func() {
						label := pluginButtonLabel(b)
						selID := "titlebar-plugin-menu:" + slotID
						sel := ui.ButtonBase(c.Key(selID)).Grow(1).MinWidth(0).FillHeight().Gap(theme.Space8).
							AlignItems(ui.Center).Justify(ui.Start).Cursor(ui.CursorPointer).Label(label)
						sel.Children(func() {
							ic := pal.DuoIconColors(pal.MenuIcon)
							pluginui.DrawDuoSpecAnimated(c, b.Icon, theme.IconRow, ic, b.Active || rowHover || kbActive)
							ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
								TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
							badge := strings.TrimSpace(b.Badge)
							if badge != "" {
								if isDotBadge(badge) {
									ui.Box(c).Size(6, 6).Shrink(0).Radius(theme.RadiusPill).Background(pal.Accent)
								} else {
									ui.Box(c).Height(16).MinWidth(16).PaddingX(theme.Space4).Shrink(0).
										Center().Radius(theme.RadiusPill).Background(pal.Accent).
										Children(func() {
											ui.Text(c, badge).FontSize(theme.FontMicro).LineHeight(1).
												FontWeight(theme.WeightControl).TextColor(pal.AccentText).SingleLine()
										})
								}
							}
							if b.Active {
								icons.DrawAnimated(c, icons.Check, theme.IconMenuCheck, pal.MinimalIconColors(pal.Text), rowHover || kbActive)
							}
						})
						blockui.Probe(sel, selID)
						if sel.Clicked() && navItems[i].OnClick != nil {
							navItems[i].OnClick()
						}
						if p.OnTogglePinPlugin != nil {
							if pinned || rowHover || kbActive {
								var pinA11y, pinTip string
								if pinned {
									pinA11y = fmt.Sprintf(theme.Tr("取消固定「%s」", "Unpin %s"), label)
									if narrow {
										pinTip = theme.Tr("取消固定（窗口加宽后常驻顶栏）", "Unpin (shown inline when wider)")
									} else {
										pinTip = theme.Tr("取消固定", "Unpin from Titlebar")
									}
								} else {
									pinA11y = fmt.Sprintf(theme.Tr("固定「%s」到顶栏", "Pin %s to Titlebar"), label)
									if evicted, ok := willEvictPinnedButton(p, slotID); ok {
										pinTip = fmt.Sprintf(theme.Tr("固定到顶栏（将替换「%s」）", "Pin to Titlebar (replaces %s)"), pluginButtonLabel(evicted))
									} else {
										pinTip = theme.Tr("固定到顶栏", "Pin to Titlebar")
									}
								}
								pinBtn := blockui.IconButton(c, icons.Star, blockui.IconButtonOptions{
									ID:          "titlebar-plugin-pin:" + slotID,
									Label:       pinA11y,
									Tooltip:     pinTip,
									Shortcut:    "P",
									Size:        theme.TreeMoreBtnSize,
									IconSize:    theme.IconInline,
									Radius:      theme.RadiusSM,
									Active:      pinned,
									ActiveStyle: blockui.ActiveAccentFlat,
									AlwaysDuo:   pinned,
								}).KeepFocus()
								if pinBtn.Clicked() {
									p.OnTogglePinPlugin(slotID)
								}
							} else {
								ui.Box(c).Size(theme.TreeMoreBtnSize, theme.TreeMoreBtnSize).Shrink(0)
							}
						}
					})
				}
				if manageIdx >= 0 {
					blockui.MenuRow(c, navItems[manageIdx-1], false)
					row := blockui.MenuRow(c, navItems[manageIdx], manageIdx == p.MenuState.MenuCursor)
					if manageIdx == p.MenuState.MenuCursor && p.MenuState.MenuRevealCursor {
						blockui.RevealActiveItem(row)
					}
				}
			})
		})
		if kbEvictWarning != "" {
			blockui.MenuRow(c, blockui.MenuItemSpec{Separator: true}, false)
			ui.Row(c.Key("titlebar-plugin-evict-hint")).FillWidth().Shrink(0).Padding(theme.Space2, theme.Space10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, kbEvictWarning).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					TextColor(pal.MenuMuted).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			})
		}
	})
	p.MenuState.HoverPluginSlotID = nextHoveredSlot
	if scrim.OverlayShortcut(0, ui.KeyEscape) {
		p.MenuState.CloseAndRestoreFocus(c)
	} else if card.PressedOutside() {
		p.MenuState.Close()
	}
}

func renderPluginContextMenu(c *ui.Context, p TitlebarProps, scrim ui.Element, availW float32) {
	vpW, _ := c.Size()
	slotID := p.MenuState.PluginCtxSlotID
	inline, _ := splitPluginTitlebarButtons(p, vpW, availW)
	found := false
	for _, b := range inline {
		if pluginButtonSlotID(b) == slotID {
			found = true
			break
		}
	}
	if !found {
		p.MenuState.Close()
		return
	}
	pinned := isPluginSlotPinned(p.PinnedPluginSlots, slotID)
	icStar := icons.Star
	icManage := icons.Plug
	var items []blockui.MenuItemSpec
	if p.OnTogglePinPlugin != nil {
		pinLabel := theme.Tr("固定到顶栏", "Pin to Titlebar")
		if pinned {
			pinLabel = theme.Tr("取消固定", "Unpin from Titlebar")
		} else if evicted, ok := willEvictPinnedButton(p, slotID); ok {
			pinLabel = fmt.Sprintf(theme.Tr("固定到顶栏（替换「%s」）", "Pin (replaces %s)"), pluginButtonLabel(evicted))
		}
		items = append(items, blockui.MenuItemSpec{
			ID:    "titlebar-plugin-ctx-pin",
			Label: pinLabel,
			Icon:  &icStar,
			OnClick: func() {
				saved := p.OnTogglePinPlugin(slotID)
				if saved && pinned {
					// If unpinning folds this inline button into overflow, hand focus to
					// the overflow button instead of the button about to leave the bar.
					activeSlots := make(map[string]bool, len(p.PluginButtons))
					for _, b := range p.PluginButtons {
						if s := pluginButtonSlotID(b); !slotHidden(p, s) {
							activeSlots[s] = true
						}
					}
					nextProps := p
					nextProps.PinnedPluginSlots = TogglePinnedPluginSlot(p.PinnedPluginSlots, slotID, activeSlots)
					nextInline, nextOverflow := splitPluginTitlebarButtons(nextProps, vpW, availW)
					staysInline := false
					for _, b := range nextInline {
						if pluginButtonSlotID(b) == slotID {
							staysInline = true
							break
						}
					}
					if !staysInline && len(nextOverflow) > 0 {
						p.MenuState.MenuTrigger = &p.MenuState.PluginOverflowTrigger
					}
				}
				p.MenuState.CloseAndRestoreFocus(c)
			},
		})
	}
	if p.OnOpenPluginStore != nil {
		if len(items) > 0 {
			items = append(items, blockui.MenuItemSpec{Separator: true})
		}
		items = append(items, blockui.MenuItemSpec{
			ID:    "titlebar-plugin-ctx-manage",
			Label: theme.Tr("管理插件…", "Manage Plugins…"),
			Icon:  &icManage,
			OnClick: func() {
				p.MenuState.Close()
				p.OnOpenPluginStore()
			},
		})
	}
	if len(items) == 0 {
		p.MenuState.Close()
		return
	}
	renderTitlebarAnchoredMenu(c, p, scrim, titlebarMenuSpec{
		Key:        "titlebar-plugin-ctx-menu",
		Anchor:     p.MenuState.PluginCtxAnchor,
		AlignRight: true,
		Width:      204,
		Items:      items,
	})
}
