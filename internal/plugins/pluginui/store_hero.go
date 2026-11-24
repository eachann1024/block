package pluginui

import (
	"strings"
	"sync"
	"time"

	"block/assets"
	"block/internal/icons"
	"block/internal/modals"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const storeHeroAutoAdvance = 8 * time.Second

type storeHeroSpec struct {
	pluginID string
	heroFile string
	tone     theme.StoreHeroTone
}

type storeHeroSlide struct {
	spec storeHeroSpec
	card storeCard
}

// storeHeroCatalog defines the curated App Store featured carousel order.
// block-file-icons stays pinned first as the primary showcase.
var storeHeroCatalog = []storeHeroSpec{
	{pluginID: "block-file-icons", heroFile: "hero-block-file-icons.svg", tone: theme.StoreHeroToneFileIcons},
	{pluginID: "block-sync", heroFile: "hero-block-sync.svg", tone: theme.StoreHeroToneSync},
	{pluginID: "block-titlebar", heroFile: "hero-block-titlebar.svg", tone: theme.StoreHeroToneTitlebar},
	{pluginID: "block-mindmap", heroFile: "hero-block-mindmap.svg", tone: theme.StoreHeroToneMindmap},
}

var (
	storeArtMu    sync.RWMutex
	storeArtCache = make(map[string]*ui.SVG)
)

func loadStoreArtSVG(filename string) *ui.SVG {
	if filename == "" {
		return nil
	}
	storeArtMu.RLock()
	cached, ok := storeArtCache[filename]
	storeArtMu.RUnlock()
	if ok {
		return cached
	}
	raw, err := assets.ReadStore(filename)
	var parsed *ui.SVG
	if err == nil && len(raw) > 0 {
		if s, parseErr := ui.ParseSVG(raw); parseErr == nil {
			parsed = s
		}
	}
	storeArtMu.Lock()
	storeArtCache[filename] = parsed
	storeArtMu.Unlock()
	return parsed
}

// renderStorePluginIcon uses the bundled artwork for verified official plugins.
func renderStorePluginIcon(
	c *ui.Context,
	entry plugins.StoreAppEntry,
	official bool,
	boxSize, glyphSize, radius float32,
	fallbackBG, fallbackBorder ui.Color,
	fallbackColors theme.IconColors,
	active bool,
) {
	if official {
		if svg := loadStoreArtSVG("icon-" + entry.ID + ".svg"); svg != nil {
			ui.Image(c, svg.WithDark(theme.IsDark())).Size(boxSize, boxSize).Shrink(0)
			return
		}
	}
	box := ui.Box(c).Size(boxSize, boxSize).Shrink(0).Center().Radius(radius).Background(fallbackBG)
	if fallbackBorder.A > 0 {
		box.Border(1, fallbackBorder)
	}
	box.Children(func() {
		DrawDuoSpecAnimated(c, entry.Icon, glyphSize, fallbackColors, active)
	})
}

func resolveStoreHeroSlides(ctrl *Controller) []storeHeroSlide {
	if ctrl == nil {
		return nil
	}
	base := ctrl.storeBase()
	if len(base.items) == 0 {
		return nil
	}
	byID := make(map[string]storeBaseItem, len(base.items))
	for _, it := range base.items {
		if it.source == storeSourceOfficial {
			byID[it.entry.ID] = it
		}
	}
	slides := make([]storeHeroSlide, 0, len(storeHeroCatalog))
	for _, spec := range storeHeroCatalog {
		it, ok := byID[spec.pluginID]
		if !ok {
			continue
		}
		slides = append(slides, storeHeroSlide{
			spec: spec,
			card: storeCard{
				entry:       it.entry,
				installed:   it.installed,
				downloads:   it.downloads,
				isInstalled: it.isInstalled,
				source:      it.source,
			},
		})
	}
	return slides
}

func renderStoreHero(
	c *ui.Context,
	ctrl *Controller,
	locale, installingID string,
	onSelectTab func(nextTab string),
) {
	slides := resolveStoreHeroSlides(ctrl)
	if len(slides) == 0 {
		return
	}
	idx := ctrl.storeHeroIndex
	if idx < 0 || idx >= len(slides) {
		idx = 0
		ctrl.storeHeroIndex = 0
	}
	now := c.Now()
	if ctrl.storeHeroLastSwitch.IsZero() {
		ctrl.storeHeroLastSwitch = now
	}

	slide := slides[idx]
	dir := ctrl.storeHeroDir
	if dir == 0 {
		dir = 1
	}
	entry := slide.card.entry
	pal := theme.Ocean()
	hc := pal.StoreHero(slide.spec.tone)
	name := entry.Name.Resolve(locale)
	summary := strings.TrimSpace(entry.Summary.Resolve(locale))
	busy := installingID == entry.ID
	hasSettings := slide.card.isInstalled &&
		slide.card.installed.Manifest.HasSettingsPage() &&
		entry.ID != plugins.DiagnosticsPluginID

	wrap := ui.Column(c.Key("app-store-hero")).FillWidth().MinWidth(0).Gap(theme.Space10)
	bui.Probe(wrap, "app-store-hero")

	cardHovered := false
	pagerHovered := false

	wrap.Children(func() {
		card := ui.ButtonBase(c.Key("app-store-hero-card")).
			FillWidth().MinWidth(0).Height(theme.StoreHeroHeight).
			Radius(theme.RadiusXL).Clip().Cursor(ui.CursorPointer).Label(name).
			ChildExit(bui.CarouselSlideExit(dir))
		hovered, pressed := card.Hovered(), card.Pressed()
		cardHovered = hovered || pressed

		bgStart := card.AnimateColor("heroBgS", hc.BGStart, bui.HeroSurfaceFade)
		bgMid := card.AnimateColor("heroBgM", hc.BGMid, bui.HeroSurfaceFade)
		bgEnd := card.AnimateColor("heroBgE", hc.BGEnd, bui.HeroSurfaceFade)
		glowPrimary := card.AnimateColor("heroGlowP", hc.GlowPrimary, bui.HeroSurfaceFade)
		glowSecondary := card.AnimateColor("heroGlowS", hc.GlowSecondary, bui.HeroSurfaceFade)
		targetBorder := hc.Border
		if hovered || pressed {
			targetBorder = hc.HoverBorder
		}
		borderCol := card.AnimateColor("heroBorder", targetBorder, bui.FadeColor)

		card.Draw(func(p *ui.Painter, bounds ui.Rect) {
			p.FillGradient(bounds, ui.LinearGradient{
				Angle: 128,
				Oklab: true,
				Stops: []ui.GradientStop{
					{Color: bgStart, At: 0},
					{Color: bgMid, At: 0.52},
					{Color: bgEnd, At: 1},
				},
			}, theme.RadiusXL)
			p.FillRadialGradient(bounds, ui.RadialGradient{
				Center: ui.Point{X: 0.78, Y: 0.28},
				Radius: max(bounds.W*0.46, bounds.H*1.15),
				Oklab:  true,
				Stops: []ui.GradientStop{
					{Color: glowPrimary, At: 0},
					{Color: glowPrimary.Alpha(0), At: 1},
				},
			}, theme.RadiusXL)
			p.FillRadialGradient(bounds, ui.RadialGradient{
				Center: ui.Point{X: 0.16, Y: 0.90},
				Radius: max(bounds.W*0.34, bounds.H*0.95),
				Oklab:  true,
				Stops: []ui.GradientStop{
					{Color: glowSecondary, At: 0},
					{Color: glowSecondary.Alpha(0), At: 1},
				},
			}, theme.RadiusXL)
			p.Stroke(bounds, borderCol, theme.RadiusXL, 1)
		})

		cardW := card.Bounds().W
		showArt := cardW <= 0 || cardW >= theme.StoreHeroMinArtWidth
		heroArt := loadStoreArtSVG(slide.spec.heroFile)
		if heroArt == nil {
			showArt = false
		}

		actionHandled := false
		actionHovered := false

		card.Children(func() {
			inner := ui.Row(c.Key("app-store-hero-slide-"+entry.ID)).
				Absolute().Fill().MinWidth(0).
				Padding(theme.StoreHeroPadY, theme.StoreHeroPadX).
				Gap(theme.Space20).AlignItems(ui.Center).Justify(ui.SpaceBetween).
				Transition(bui.CarouselSlideTransition(dir))

			inner.Children(func() {
				// Left editorial copy and action column.
				copyCol := ui.Column(c).Grow(1).MinWidth(0).FillHeight().Justify(ui.SpaceBetween).Gap(theme.Space12)
				if showArt {
					copyCol.MaxWidth(theme.StoreHeroCopyMaxW)
				}
				copyCol.Children(func() {
					ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space12).Children(func() {
						ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
							sc := pal.StoreCard()
							renderStorePluginIcon(
								c, entry, true,
								theme.StoreHeroIconSize, theme.StoreIconGlyphSize, theme.RadiusLG,
								sc.IconBG, sc.IconBorder, pal.DuoIconColors(pal.Text), false,
							)
							ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, name).
									FontSize(theme.FontPage).FixedLineHeight(theme.LinePage).
									FontWeight(theme.WeightTitle).TextColor(pal.Text).
									MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
								if storeAppIsBeta(entry, slide.card.installed, slide.card.isInstalled) {
									bui.BetaBadge(c, true)
								}
							})
						})
						if summary != "" {
							ui.Text(c, summary).
								FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
								TextColor(pal.TextSecondary).FillWidth().MinWidth(0)
						}
					})

					ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
						pHandled, pHovered := renderStoreHeroPrimaryAction(c, ctrl, slide.card, hc, busy, hasSettings, installingID, onSelectTab)
						if pHandled {
							actionHandled = true
						}
						if pHovered {
							actionHovered = true
						}
						dHandled, dHovered := renderStoreHeroDetailButton(c, ctrl, slide.card, hc)
						if dHandled {
							actionHandled = true
						}
						if dHovered {
							actionHovered = true
						}
					})
				})

				// Right bespoke vector illustration column.
				if showArt && heroArt != nil {
					artW := theme.StoreHeroArtMaxW
					if cardW > 0 {
						avail := cardW - theme.StoreHeroPadX*2 - theme.Space20 - theme.StoreHeroCopyMaxW
						if avail < artW {
							artW = max(avail, min(cardW*0.44, theme.StoreHeroArtMaxW))
						}
					}
					artH := artW * (220.0 / 460.0)
					if artH > theme.StoreHeroArtH {
						artH = theme.StoreHeroArtH
						artW = artH * (460.0 / 220.0)
					}
					artBox := ui.Box(c.Key("app-store-hero-art-"+entry.ID)).
						Size(artW, artH).Shrink(0).Center().PassThrough().
						Transition(bui.CarouselArtParallaxTransition(dir))
					hoverTarget := float32(0)
					if hovered && !c.Preferences().ReduceMotion {
						hoverTarget = 1
					}
					artLift := card.AnimateTo("heroArtHover", hoverTarget, bui.TreeDrawerOpen)
					if artLift > 0.001 {
						artBox.Translate(0, -2.5*artLift).Scale(1+0.02*artLift, 1+0.02*artLift)
					}
					artBox.Children(func() {
						ui.Image(c, heroArt.WithDark(theme.IsDark())).Size(artW, artH)
					})
				}
			})
		})

		bui.Probe(card, "app-store-hero-card")
		if !actionHandled && !actionHovered && card.Clicked() {
			ctrl.closeStoreMenus()
			ctrl.openStoreReadme(entry, slide.card.installed)
			c.Invalidate()
		}

		if len(slides) > 1 {
			navRow := ui.Row(c.Key("app-store-hero-nav")).
				FillWidth().MinWidth(0).Gap(theme.Space8).
				AlignItems(ui.Center).Justify(ui.Center)
			navRow.Children(func() {
				prevBtn := bui.IconButton(c, icons.ChevronLeft, bui.IconButtonOptions{
					ID:       "app-store-hero-prev",
					Label:    theme.Tr("上一个推荐", "Previous featured"),
					Tooltip:  theme.Tr("上一个推荐", "Previous featured"),
					Variant:  bui.IconPlain,
					Size:     theme.IconButtonBox,
					IconSize: theme.IconInline,
				})
				if prevBtn.Hovered() {
					pagerHovered = true
				}
				if prevBtn.Clicked() {
					ctrl.selectStoreHero((idx-1+len(slides))%len(slides), -1, true, now)
					c.Invalidate()
				}

				pager := bui.DotPager(c, bui.DotPagerOptions{
					PagerID:  "app-store-hero-pager",
					IDPrefix: "app-store-hero-dot",
					Count:    len(slides),
					Active:   idx,
					SlotTooltip: func(slot int) *bui.TooltipSpec {
						if slot < 0 || slot >= len(slides) {
							return nil
						}
						return &bui.TooltipSpec{
							Rows: []bui.TooltipRow{{Trigger: slides[slot].card.entry.Name.Resolve(locale)}},
						}
					},
					OnSelect: func(slot int) {
						if slot == idx {
							return
						}
						stepDir := 1
						if slot < idx {
							stepDir = -1
						}
						ctrl.selectStoreHero(slot, stepDir, true, now)
						c.Invalidate()
					},
				})
				if pager.Hovered() {
					pagerHovered = true
				}

				nextBtn := bui.IconButton(c, icons.ChevronRight, bui.IconButtonOptions{
					ID:       "app-store-hero-next",
					Label:    theme.Tr("下一个推荐", "Next featured"),
					Tooltip:  theme.Tr("下一个推荐", "Next featured"),
					Variant:  bui.IconPlain,
					Size:     theme.IconButtonBox,
					IconSize: theme.IconInline,
				})
				if nextBtn.Hovered() {
					pagerHovered = true
				}
				if nextBtn.Clicked() {
					ctrl.selectStoreHero((idx+1)%len(slides), 1, true, now)
					c.Invalidate()
				}
			})
		}
	})

	// Gentle auto-advance only while untouched, unhovered, and motion is allowed.
	if len(slides) > 1 && !ctrl.storeHeroPaused && installingID == "" && !c.Preferences().ReduceMotion {
		if cardHovered || pagerHovered {
			ctrl.storeHeroLastSwitch = now
		} else {
			elapsed := now.Sub(ctrl.storeHeroLastSwitch)
			if elapsed >= storeHeroAutoAdvance {
				ctrl.selectStoreHero((idx+1)%len(slides), 1, false, now)
				c.Invalidate()
			} else {
				c.After(storeHeroAutoAdvance - elapsed)
			}
		}
	}
}

func renderStoreHeroPrimaryAction(
	c *ui.Context,
	ctrl *Controller,
	card storeCard,
	hc theme.StoreHeroColors,
	busy, hasSettings bool,
	installingID string,
	onSelectTab func(nextTab string),
) (handled, hovered bool) {
	pal := theme.Ocean()
	entry := card.entry
	hasUpdate := card.isInstalled && entry.ID != plugins.DiagnosticsPluginID && storeEntryHasUpdate(entry, card.installed)

	if hasUpdate {
		label := theme.Tr("更新", "Update")
		if busy {
			label = theme.Tr("更新中", "Updating")
		}
		pluginBusy := ctrl.sup.IsPluginBusyAnyNotebook(entry.ID)
		disabled := (installingID != "" && !busy) || (pluginBusy && !busy)
		btn := renderStoreHeroAccentButton(c, "app-store-hero-primary-"+entry.ID, label, hc, true, disabled, busy)
		hovered = btn.Hovered()
		if btn.Clicked() && !busy && !disabled {
			handled = true
			ctrl.closeStoreMenus()
			ctrl.RequestInstallEntry(entry, ctrl.clearPluginState)
		}
		return handled, hovered
	}

	if card.isInstalled && hasSettings {
		btn := renderStoreHeroSurfaceButton(c, "app-store-hero-primary-"+entry.ID, theme.Tr("打开", "Open"), hc, busy)
		hovered = btn.Hovered()
		if btn.Clicked() && !busy {
			handled = true
			ctrl.closeStoreMenus()
			if onSelectTab != nil {
				onSelectTab(modals.TabPluginPrefix + entry.ID)
			} else {
				ctrl.openStoreReadme(entry, card.installed)
				c.Invalidate()
			}
		}
		return handled, hovered
	}

	if card.isInstalled {
		pill := ui.Row(c.Key("app-store-hero-installed-"+entry.ID)).
			Height(theme.HeightControl).PaddingX(theme.Space12).Gap(theme.Space6).
			Radius(theme.RadiusMD).Background(hc.SurfaceBtnBG).Border(1, hc.SurfaceBtnBorder).
			AlignItems(ui.Center).Shrink(0)
		pill.Children(func() {
			icons.Draw(c, icons.Check, theme.IconInline, pal.MinimalIconColors(pal.TextSecondary))
			ui.Text(c, theme.Tr("已安装", "Installed")).
				FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine()
		})
		return false, pill.Hovered()
	}

	label := theme.Tr("安装", "Install")
	if busy {
		label = theme.Tr("安装中", "Installing")
	}
	disabled := installingID != "" && !busy
	btn := renderStoreHeroAccentButton(c, "app-store-hero-primary-"+entry.ID, label, hc, false, disabled, busy)
	hovered = btn.Hovered()
	if btn.Clicked() && !busy && !disabled {
		handled = true
		ctrl.closeStoreMenus()
		ctrl.RequestInstallEntry(entry, ctrl.clearPluginState)
	}
	return handled, hovered
}

func renderStoreHeroAccentButton(
	c *ui.Context,
	id, label string,
	hc theme.StoreHeroColors,
	leadingDot, disabled, loading bool,
) ui.Element {
	btn := ui.ButtonBase(c.Key(id)).
		Height(theme.HeightControl).PaddingX(theme.Space16).Gap(theme.Space6).
		Radius(theme.RadiusMD).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).
		Label(label)
	if disabled {
		btn.Disabled(true).Opacity(theme.DisabledOpacity)
	} else {
		btn.Cursor(ui.CursorPointer)
	}
	hovered := !disabled && btn.Hovered()
	pressed := !disabled && btn.Pressed()
	bg := hc.AccentBG
	if pressed {
		bg = hc.AccentPressedBG
	} else if hovered {
		bg = hc.AccentHoverBG
	}
	bui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
	bui.TactileButton(btn, hovered, pressed, false)
	btn.Children(func() {
		if leadingDot && !loading {
			ui.Box(c).Size(theme.StoreStatusDotSize, theme.StoreStatusDotSize).
				Shrink(0).Radius(theme.RadiusPill).Background(hc.AccentFG)
		}
		ui.Text(c, label).
			FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			FontWeight(theme.WeightControl).TextColor(hc.AccentFG).SingleLine()
	})
	bui.Probe(btn, id)
	return btn
}

func renderStoreHeroSurfaceButton(
	c *ui.Context,
	id, label string,
	hc theme.StoreHeroColors,
	disabled bool,
) ui.Element {
	pal := theme.Ocean()
	btn := ui.ButtonBase(c.Key(id)).
		Height(theme.HeightControl).PaddingX(theme.Space12).Gap(theme.Space6).
		Radius(theme.RadiusMD).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).
		Label(label)
	if disabled {
		btn.Disabled(true).Opacity(theme.DisabledOpacity)
	} else {
		btn.Cursor(ui.CursorPointer)
	}
	hovered := !disabled && btn.Hovered()
	pressed := !disabled && btn.Pressed()
	bg := hc.SurfaceBtnBG
	if hovered || pressed {
		bg = hc.SurfaceBtnHoverBG
	}
	bui.AnimateSurfaceColors(btn, bg, hc.SurfaceBtnBorder, true)
	bui.TactileButton(btn, hovered, pressed, false)
	btn.Children(func() {
		ui.Text(c, label).
			FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
	})
	bui.Probe(btn, id)
	return btn
}

func renderStoreHeroDetailButton(
	c *ui.Context,
	ctrl *Controller,
	card storeCard,
	hc theme.StoreHeroColors,
) (handled, hovered bool) {
	label := theme.Tr("查看详情", "Details")
	btn := renderStoreHeroSurfaceButton(c, "app-store-hero-detail-"+card.entry.ID, label, hc, false)
	hovered = btn.Hovered()
	if btn.Clicked() {
		handled = true
		ctrl.closeStoreMenus()
		ctrl.openStoreReadme(card.entry, card.installed)
		c.Invalidate()
	}
	return handled, hovered
}
