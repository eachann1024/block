package app

import (
	"block/internal/icons"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	// brandWebsiteURL is where the brand menu's "open website" row leads.
	brandWebsiteURL = "https://block.cooool.top"
	// brandMarkGap separates the brand wordmark from the sidebar toggle.
	brandMarkGap = 2
	// brandMenuHeaderH is the brand menu's single-line identity header (mark + name + version).
	brandMenuHeaderH = theme.HeightControl
)

// renderBrandMark draws the "cooool.top" brand button after the sidebar toggle;
// clicking it opens the brand menu and a pending update shows as a dot on it.
func renderBrandMark(c *ui.Context, p TitlebarProps) {
	open := p.MenuState != nil && p.MenuState.BrandMenuOpen
	versionLabel := brandVersionLabel(p)
	a11yLabel := versionLabel
	rows := []blockui.TooltipRow{{Trigger: versionLabel}}
	switch {
	case p.HasUpdate:
		status := nonEmptyOr(p.UpdateTooltip, theme.Tr("有可用更新", "Update available"))
		a11yLabel = versionLabel + " · " + status
		rows = append(rows, blockui.TooltipRow{
			Trigger:     status,
			TriggerTone: blockui.TooltipToneAccent,
		})
	case p.UpdateChecking:
		checking := theme.Tr("正在检查更新…", "Checking for updates…")
		a11yLabel = versionLabel + " · " + checking
		rows = append(rows, blockui.TooltipRow{
			Detail: checking,
		})
	}
	spec := blockui.TooltipSpec{
		Placement: blockui.TooltipBelow,
		Rows:      rows,
	}
	btn := blockui.AppMarkButton(c, blockui.AppMarkOptions{
		Label:       a11yLabel,
		TooltipSpec: &spec,
		Active:      open,
		UpdateDot:   p.HasUpdate,
	}).Margin(0, 0, 0, brandMarkGap)
	if p.MenuState != nil {
		btn.Bind(&p.MenuState.BrandTrigger)
	}
	if b := btn.Bounds(); p.MenuState != nil && b.W > 0 {
		p.MenuState.BrandMenuAnchor = b
	}
	if btn.Clicked() && p.MenuState != nil {
		p.MenuState.Close()
		p.MenuState.BrandMenuOpen = !open
		if !open {
			p.MenuState.MenuTrigger = &p.MenuState.BrandTrigger
		}
	}
}

func brandVersionLabel(p TitlebarProps) string {
	if p.AppVersion == "" {
		return "Block"
	}
	return "Block · v" + p.AppVersion
}

// buildBrandMenuItems lists where the app comes from and how to keep it current.
func buildBrandMenuItems(c *ui.Context, p TitlebarProps) []blockui.MenuItemSpec {
	icSite := icons.LinkOpen
	icUpdate := icons.UpdateTangle
	if p.HasUpdate {
		icUpdate = icons.Download
	}
	icStore := icons.Plug
	icAbout := icons.HelpCircle
	item := func(id, label, hint string, ic *icons.Icon, restoreFocus bool, fn func()) blockui.MenuItemSpec {
		return blockui.MenuItemSpec{ID: id, Label: label, Hint: hint, Icon: ic, OnClick: func() {
			if restoreFocus {
				p.MenuState.CloseAndRestoreFocus(c)
			} else {
				p.MenuState.Close()
			}
			if fn != nil {
				fn()
			}
		}}
	}
	updateLabel := p.UpdateMenuLabel
	if updateLabel == "" {
		switch {
		case p.HasUpdate:
			updateLabel = nonEmptyOr(p.UpdateTooltip, theme.Tr("有可用更新", "Update available"))
		case p.UpdateChecking:
			updateLabel = theme.Tr("正在检查更新…", "Checking for Updates…")
		default:
			updateLabel = theme.Tr("检查更新…", "Check for Updates…")
		}
	}
	items := []blockui.MenuItemSpec{
		item("brand-update", updateLabel, p.UpdateHint, &icUpdate, false, p.OnOpenUpdater),
		item("brand-website", theme.Tr("打开官网", "Open Website"), "", &icSite, true, func() { c.OpenURL(brandWebsiteURL) }),
		item("brand-plugin-store", theme.Tr("插件商店", "Plugin Store"), "", &icStore, false, p.OnOpenPluginStore),
		{Separator: true},
		item("brand-about", theme.Tr("关于 Block", "About Block"), "", &icAbout, false, p.OnOpenAbout),
	}
	return items
}

func renderBrandMenu(c *ui.Context, p TitlebarProps, scrim ui.Element) {
	pal := theme.Ocean()
	renderTitlebarAnchoredMenu(c, p, scrim, titlebarMenuSpec{
		Key:     "brand-menu",
		Anchor:  p.MenuState.BrandMenuAnchor,
		Width:   232,
		Items:   buildBrandMenuItems(c, p),
		HeaderH: brandMenuHeaderH + 7,
		Header: func() {
			ui.Row(c.Key("brand-menu-header")).FillWidth().Height(brandMenuHeaderH).Shrink(0).
				PaddingX(theme.Space10).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
				blockui.AppMarkImage(c, theme.IconRow)
				ui.Text(c, "Block").FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine()
				if p.AppVersion != "" {
					ui.Text(c, "v"+p.AppVersion).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(pal.MenuMuted).SingleLine().Shrink(0)
				}
			})
			blockui.MenuRow(c, blockui.MenuItemSpec{Separator: true}, false)
		},
	})
}

type titlebarMenuSpec struct {
	Key        string
	Anchor     ui.Rect
	AlignRight bool
	Width      float32
	Items      []blockui.MenuItemSpec
	HeaderH    float32
	Header     func()
}

// renderTitlebarAnchoredMenu drops a keyboard-navigable menu card below a titlebar button.
func renderTitlebarAnchoredMenu(c *ui.Context, p TitlebarProps, scrim ui.Element, m titlebarMenuSpec) {
	vpW, vpH := c.Size()
	menuW := min(m.Width, max(vpW-16, 1))
	anchor := m.Anchor
	if anchor.W <= 0 {
		ax := float32(12)
		if m.AlignRight {
			ax = max(vpW-40, 8)
		}
		anchor = ui.Rect{X: ax, Y: 6, W: theme.ChromeBoxSize, H: theme.ChromeBoxSize}
	}
	spec := blockui.PlaceBelow(anchor)
	if m.AlignRight {
		spec = spec.WithAlignX(blockui.AlignX{Kind: blockui.AlignRight})
	}
	maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 360)
	popH := min(estimateMenuHeight(m.Items)+m.HeaderH, maxH)
	ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

	c.Key(m.Key)
	card := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).
		Focusable().FocusRing(false).AutoFocus().Role(ui.RoleMenu))
	navigateTitlebarMenu(c, card, m.Items, p.MenuState)
	blockui.Probe(card, m.Key)
	card.Children(func() {
		blockui.Scroll(c).FillWidth().MaxHeight(blockui.MenuInnerMaxHeight(maxH)).Children(func() {
			ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
				if m.Header != nil {
					m.Header()
				}
				for i, it := range m.Items {
					row := blockui.MenuRow(c, it, i == p.MenuState.MenuCursor)
					if i == p.MenuState.MenuCursor && p.MenuState.MenuRevealCursor {
						blockui.RevealActiveItem(row)
					}
				}
			})
		})
	})
	if scrim.OverlayShortcut(0, ui.KeyEscape) {
		p.MenuState.CloseAndRestoreFocus(c)
	} else if card.PressedOutside() {
		p.MenuState.Close()
	}
}
