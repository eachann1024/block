package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/plugins/pluginui"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// titlebarActionTrailing matches thinkly-fast TITLEBAR_ACTION_TRAILING (TRAFFIC_X - 4 on macOS).
const titlebarActionTrailing = 12

// renderTitlebarTail renders the right rail (thinkly-fast render_titlebar_tail):
// notes show feedback, AI, then the page more pill; other surfaces put their
// context tools before feedback and AI.
func renderTitlebarTail(c *ui.Context, p TitlebarProps, rightInset, availW float32) {
	pad := float32(titlebarActionTrailing)
	if !theme.IsMacOS() {
		pad = 0
		if rightInset > 0 {
			pad = rightInset + 8
		}
	}
	surface := p.ActiveSurface
	if len(p.Tabs) == 0 {
		surface = SurfaceEmpty
	} else if surface == SurfaceEmpty {
		surface = SurfaceNote
	}
	showFeedback := !slotHidden(p, plugins.TitlebarSlotFeedback)
	showAI := p.AIEnabled && !slotHidden(p, plugins.TitlebarSlotAI)
	showMore := surface != SurfaceEmpty
	hasBuiltinTail := showFeedback || showAI || showMore
	tail := ui.Row(c).FillHeight().Shrink(0).AlignItems(ui.Center).Gap(4).Padding(0, pad, 0, 0)
	if m := p.MenuState; m != nil {
		prevNeed := m.TitlebarNeedW
		m.TitlebarNeedW = 0
		if tailW := tail.Bounds().W; tailW > 0 && m.tabsNeedW > 0 && len(p.Tabs) > 1 && !p.InSettings {
			foldedZone := float32(0)
			if m.pluginZoneW > 0 {
				foldedZone = pluginSlotW
			}
			m.TitlebarNeedW = m.tabsNeedW + tailW - m.pluginZoneW + foldedZone
		}
		if m.TitlebarNeedW != prevNeed {
			c.After(16 * time.Millisecond)
		}
	}
	tail.Children(func() {
		if surface != SurfaceNote {
			renderNonNoteTitlebarTools(c, p, surface, availW)
		}
		renderPluginTitlebarButtons(c, p, hasBuiltinTail, availW)
		if showFeedback {
			renderFeedbackButton(c, p)
		}
		if showAI {
			renderAIButton(c, p)
		}
		if showMore {
			renderPageMorePill(c, p, availW)
		}
	})
}

// renderPluginTitlebarButtons draws the plugin zone: every button that fits the free
// titlebar space, an overflow button for the rest, then a hairline before the built-in tools.
func renderPluginTitlebarButtons(c *ui.Context, p TitlebarProps, hasBuiltinTail bool, availW float32) {
	vpW, _ := c.Size()
	if m := p.MenuState; m != nil {
		m.pluginLimitReady = false
	}
	inline, overflow := splitPluginTitlebarButtons(p, vpW, availW)
	if m := p.MenuState; m != nil {
		zone := pluginZoneWidth(len(inline), len(inline)+len(overflow))
		if zone != m.pluginZoneW {
			c.After(16 * time.Millisecond)
		}
		m.pluginZoneW = zone
		m.pluginInlineN = len(inline)
		m.pluginLimitReady = true
	}
	if len(inline) == 0 && len(overflow) == 0 {
		return
	}
	for _, b := range inline {
		renderPluginTitlebarButton(c, p, b)
	}
	if len(overflow) > 0 {
		renderPluginOverflowButton(c, p, overflow)
	}
	if hasBuiltinTail {
		ui.Box(c).Size(1, 14).Shrink(0).MarginX(2).Background(theme.Ocean().BtnBorder)
	}
}

func renderPluginOverflowButton(c *ui.Context, p TitlebarProps, overflow []plugins.ResolvedTitlebarButton) {
	pal := theme.Ocean()
	open := p.MenuState != nil && p.MenuState.PluginMenuOpen
	const id = "titlebar-plugin-overflow"
	// A folded button that is active or carries a badge still needs to be noticed.
	attention := false
	attentionLabel := ""
	for _, b := range overflow {
		if b.Active || strings.TrimSpace(b.Badge) != "" {
			attention = true
			if attentionLabel == "" {
				attentionLabel = pluginButtonLabel(b)
			}
		}
	}
	a11yLabel := fmt.Sprintf(theme.Tr("更多插件（%d 个已收起）", "More Plugins (%d folded)"), len(overflow))
	if attentionLabel != "" {
		a11yLabel = fmt.Sprintf(theme.Tr("更多插件（%d 个已收起，含提醒：%s）", "More Plugins (%d folded, attention: %s)"), len(overflow), attentionLabel)
	}
	c.Key(id)
	btn := ui.ButtonBase(c).Size(theme.ChromeBoxSize, theme.ChromeBoxSize).Shrink(0).Center().
		Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(a11yLabel)
	if p.MenuState != nil {
		btn.Bind(&p.MenuState.PluginOverflowTrigger)
	}
	if !open {
		rows := []blockui.TooltipRow{{
			Trigger: theme.Tr("更多插件", "More Plugins"),
			Detail:  strconv.Itoa(len(overflow)),
		}}
		if attentionLabel != "" {
			rows = append(rows, blockui.TooltipRow{
				Trigger:     attentionLabel,
				TriggerTone: blockui.TooltipToneAccent,
			})
		}
		blockui.RichTooltip(c, btn, blockui.TooltipSpec{
			Placement: blockui.TooltipBelow,
			Rows:      rows,
		})
	}
	hovered, pressed := btn.Hovered(), btn.Pressed()
	bgCol, bdCol, hasBorder := ui.Transparent, ui.Transparent, open
	if open {
		bgCol = pal.Selected
		bdCol = pal.SelectedBorder
	} else if pressed || hovered {
		bgCol = pal.ShellChrome().Hover
	}
	blockui.AnimateSurfaceColors(btn, bgCol, bdCol, hasBorder)
	blockui.TactileButton(btn, hovered, pressed, false)
	btn.Children(func() {
		ic := pal.MonoIconColors(pal.TextSecondary)
		if open || hovered || pressed {
			ic = pal.DuoIconColors(pal.Text)
		}
		icons.DrawAnimated(c, icons.Plug, theme.IconChrome, ic, open || hovered || pressed)
		if !open {
			countText := strconv.Itoa(len(overflow))
			if len(overflow) > 9 {
				countText = "9+"
			}
			badgeBG, badgeFG := pal.IconFill, pal.Text
			if attention {
				badgeBG, badgeFG = pal.Accent, pal.AccentText
			}
			ui.Box(c).Absolute().Top(0).Right(0).Height(13).MinWidth(13).PaddingX(theme.Space2).
				Center().Radius(theme.RadiusPill).Background(badgeBG).IgnoreInput(true).
				Children(func() {
					ui.Text(c, countText).FontSize(theme.FontMicro).LineHeight(1).
						FontWeight(theme.WeightControl).TextColor(badgeFG).SingleLine()
				})
		}
	})
	blockui.Probe(btn, id)
	if b := btn.Bounds(); p.MenuState != nil && b.W > 0 {
		p.MenuState.PluginMenuAnchor = b
	}
	if btn.Clicked() && p.MenuState != nil {
		p.MenuState.Close()
		p.MenuState.PluginMenuOpen = !open
		if !open {
			p.MenuState.MenuTrigger = &p.MenuState.PluginOverflowTrigger
		}
	}
}

func isDotBadge(badge string) bool {
	badge = strings.TrimSpace(badge)
	return badge == "." || badge == "•" || badge == "dot"
}

func renderPluginTitlebarButton(c *ui.Context, p TitlebarProps, b plugins.ResolvedTitlebarButton) {
	pal := theme.Ocean()
	slotID := pluginButtonSlotID(b)
	ctxOpen := p.MenuState != nil && p.MenuState.PluginCtxOpen && p.MenuState.PluginCtxSlotID == slotID
	key := "titlebar-plugin:" + slotID
	legacyProbeID := "titlebar-plugin-" + b.PluginID + "-" + b.ID
	c.Key(key)
	btn := ui.ButtonBase(c).Size(theme.ChromeBoxSize, theme.ChromeBoxSize).Shrink(0).Center().
		Radius(theme.RadiusMD).Cursor(ui.CursorPointer)
	if ctxOpen {
		btn.Bind(&p.MenuState.PluginCtxTrigger)
	}
	btn.Label(pluginButtonLabel(b))
	if b.Tooltip != "" && !ctxOpen {
		blockui.RichTooltip(c, btn, blockui.TooltipSpec{
			Placement: blockui.TooltipBelow,
			Rows:      []blockui.TooltipRow{{Trigger: b.Tooltip}},
		})
	}
	hovered := btn.Hovered()
	pressed := btn.Pressed()
	active := b.Active || ctxOpen
	bgCol, bdCol, hasBorder := ui.Transparent, ui.Transparent, active
	if active {
		bgCol = pal.Selected
		bdCol = pal.SelectedBorder
	} else if pressed || hovered {
		bgCol = pal.ShellChrome().Hover
	}
	blockui.AnimateSurfaceColors(btn, bgCol, bdCol, hasBorder)
	blockui.TactileButton(btn, hovered, pressed, false)

	badge := strings.TrimSpace(b.Badge)
	dotBadge := isDotBadge(badge)
	btn.Children(func() {
		ic := pal.MonoIconColors(pal.TextSecondary)
		if active || hovered || pressed {
			ic = pal.DuoIconColors(pal.Text)
		}
		pluginui.DrawDuoSpec(c, b.Icon, theme.IconChrome, ic)
		if badge != "" && !dotBadge {
			ui.Box(c).Absolute().Top(0).Right(0).Height(13).MinWidth(13).PaddingX(theme.Space2).
				Center().Radius(theme.RadiusPill).Background(pal.Accent).IgnoreInput(true).
				Children(func() {
					ui.Text(c, badge).FontSize(theme.FontMicro).LineHeight(1).
						FontWeight(theme.WeightControl).TextColor(pal.AccentText).SingleLine()
				})
		}
	})
	if dotBadge {
		dot := pal.Accent
		btn.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			blockui.RecordBounds(key, r)
			blockui.RecordBounds(legacyProbeID, r)
			pt.Fill(ui.Rect{X: r.X + r.W - 3 - 5.5, Y: r.Y + 3, W: 5.5, H: 5.5}, dot, 2.75)
		})
	} else if blockui.IsProbeEnabled() {
		btn.DrawOver(func(_ *ui.Painter, r ui.Rect) {
			blockui.RecordBounds(key, r)
			blockui.RecordBounds(legacyProbeID, r)
		})
	}
	if ctxOpen {
		if bounds := btn.Bounds(); bounds.W > 0 {
			p.MenuState.PluginCtxAnchor = bounds
		}
	}
	openCtx := btn.RightClicked() ||
		((btn.Shortcut(0, ui.KeyP) || btn.Shortcut(ui.Shift, ui.KeyF10)) && !c.ShortcutRepeated())
	if openCtx && p.MenuState != nil && (p.OnTogglePinPlugin != nil || p.OnOpenPluginStore != nil) {
		p.MenuState.Close()
		p.MenuState.PluginCtxOpen = true
		p.MenuState.PluginCtxSlotID = slotID
		p.MenuState.PluginCtxAnchor = btn.Bounds()
		p.MenuState.MenuTrigger = &p.MenuState.PluginCtxTrigger
	} else if btn.Clicked() && p.OnPluginButton != nil {
		p.OnPluginButton(b)
	}
}

func renderFeedbackButton(c *ui.Context, p TitlebarProps) {
	tip := theme.Tr("反馈", "Feedback")
	if p.FeedbackOpen {
		tip = ""
	}
	btn := blockui.IconButton(c, icons.MessageSquare, blockui.IconButtonOptions{
		ID:          "titlebar-feedback",
		Label:       theme.Tr("反馈", "Feedback"),
		Tooltip:     tip,
		Active:      p.FeedbackOpen,
		ActiveStyle: blockui.ActiveAccentFlat,
		ChromeHover: true,
	})
	if p.HasFeedbackDraft && !p.FeedbackOpen {
		dot := theme.Ocean().Accent
		// DrawOver replaces the smoke probe's, so record its bounds here too.
		btn.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			blockui.RecordBounds("titlebar-feedback", r)
			pt.Fill(ui.Rect{X: r.X + r.W - 3 - 5.5, Y: r.Y + 3, W: 5.5, H: 5.5}, dot, 2.75)
		})
	}
	if btn.Clicked() && p.OnToggleFeedback != nil {
		p.OnToggleFeedback()
	}
}

func renderAIButton(c *ui.Context, p TitlebarProps) {
	sc := nonEmptyOr(p.ShortcutAI, "cmd-j")
	btn := blockui.IconButton(c, icons.Sparkles, blockui.IconButtonOptions{
		ID:          "titlebar-ai",
		Tooltip:     theme.Tr("AI 助手", "AI Assistant"),
		Shortcut:    blockui.FormatShortcutDisplay(sc),
		Active:      p.AIOpen,
		ActiveStyle: blockui.ActiveAccent,
		ChromeHover: true,
	})
	if btn.Clicked() && p.OnToggleAI != nil {
		p.OnToggleAI()
	}
}

// renderPageMorePill is thinkly-fast titlebar_split_pill: the page more button,
// joined with the annotation send segment when the note has annotations. The
// segment unfolds leftward out of the more button and folds back into it while
// staying in the layout, so the titlebar icons beside it glide instead of jump.
func renderPageMorePill(c *ui.Context, p TitlebarProps, availW float32) {
	pal := theme.Ocean()
	vpW, _ := c.Size()
	narrow := isNarrowTitlebar(vpW, availW)
	hasSendAction := p.ActiveSurface == SurfaceNote && p.AnnotHasAny && !slotHidden(p, plugins.TitlebarSlotSendAll)
	split := hasSendAction && !narrow
	foldedSendAttention := hasSendAction && narrow && (p.AnnotDrafts > 0 || p.AnnotGenerating)
	open := p.MenuState != nil && p.MenuState.PageMenuOpen
	c.Key("titlebar-action-pill")
	pill := ui.Row(c).Shrink(0).AlignItems(ui.Center).Radius(theme.RadiusMD)
	target := float32(0)
	bg, border := ui.Transparent, ui.Transparent
	if split {
		target, bg, border = 1, pal.Menu, pal.BtnBorder
	}
	reveal := blockui.RevealTo(pill, "pillReveal", target)
	inset := pillInset * reveal
	pill.Height(theme.HeightChrome + inset).Padding(inset)
	blockui.AnimateSurfaceColors(pill, bg, border, true)
	pill.Children(func() {
		if split || reveal > 0.001 {
			renderSendGroup(c, p, reveal)
		}
		moreLabel := theme.Tr("页面操作", "Page Actions")
		moreTip := ""
		if foldedSendAttention {
			if p.AnnotGenerating {
				moreLabel = theme.Tr("页面操作 · 批注生成中…", "Page Actions · Generating annotations…")
			} else {
				moreLabel = theme.Tr("页面操作 · 待发送批注 ", "Page Actions · Pending annotations ") + strconv.Itoa(p.AnnotDrafts)
			}
			if !open {
				moreTip = moreLabel
			}
		}
		btn := blockui.IconButton(c, icons.More, blockui.IconButtonOptions{
			ID:          "titlebar-more",
			Label:       moreLabel,
			Tooltip:     moreTip,
			Active:      open,
			ActiveStyle: blockui.ActiveAccent,
			Size:        theme.HeightChrome - inset,
			Radius:      theme.RadiusMD - inset,
			ChromeHover: !split,
		})
		if p.MenuState != nil {
			btn.Bind(&p.MenuState.PageMoreTrigger)
		}
		if foldedSendAttention && !open {
			dot := pal.AnnotSendDot
			btn.DrawOver(func(pt *ui.Painter, r ui.Rect) {
				blockui.RecordBounds("titlebar-more", r)
				pt.Fill(ui.Rect{X: r.X + r.W - 3 - 5.5, Y: r.Y + 3, W: 5.5, H: 5.5}, dot, 2.75)
			})
		}
		if b := btn.Bounds(); p.MenuState != nil && b.W > 0 {
			p.MenuState.PageMenuAnchor = b
		}
		if btn.Clicked() && p.MenuState != nil {
			p.MenuState.Close()
			p.MenuState.PageMenuOpen = !open
			if !open {
				p.MenuState.MenuTrigger = &p.MenuState.PageMoreTrigger
			}
		}
	})
}

// pillInset is the split pill's padding and its segments' gap.
const pillInset = 2

// renderSendGroup clips the send segment and divider to reveal × their width,
// pinned to the more button's side, fading with the reveal.
func renderSendGroup(c *ui.Context, p TitlebarProps, reveal float32) {
	pal := theme.Ocean()
	moving := reveal < 0.999
	c.Key("titlebar-send-group")
	clip := ui.Box(c).Shrink(0)
	if moving {
		clip.ClipX().Opacity(reveal)
	}
	clip.Children(func() {
		c.Key("titlebar-send-content")
		content := ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(pillInset).Padding(0, pillInset, 0, 0)
		if moving {
			natural := content.Bounds().W
			if natural <= 0 {
				c.Invalidate()
			}
			shown := natural * reveal
			clip.Width(shown)
			content.Translate(shown-natural, 0)
		}
		content.Children(func() {
			renderSendAllSegment(c, p)
			ui.Box(c).Size(1, 14).Shrink(0).Background(pal.BtnBorder)
		})
	})
}

// renderSendAllSegment is thinkly-fast send_all_button: `统一发送 n` / `生成中…`.
func renderSendAllSegment(c *ui.Context, p TitlebarProps) {
	pal := theme.Ocean()
	enabled := p.AnnotDrafts > 0 && !p.AnnotGenerating && p.OnSendAnnotations != nil
	active := enabled || p.AnnotGenerating
	label := theme.Tr("统一发送", "Send all")
	if p.AnnotGenerating {
		label = theme.Tr("生成中…", "Generating…")
	}
	c.Key("annotations-send-all")
	btn := ui.ButtonBase(c).Height(24).PaddingX(9).Gap(6).Shrink(0).Radius(theme.RadiusSM).Center().Label(label).Disabled(!enabled).DimDisabled(false)
	fg := pal.TextSecondary
	bg := ui.Transparent
	if active {
		fg = pal.AnnotSendFG
		bg = pal.Selected
	} else {
		btn.Opacity(0.55)
	}
	hovered := false
	pressed := false
	if enabled {
		btn.Cursor(ui.CursorPointer)
		hovered = btn.Hovered()
		pressed = btn.Pressed()
		if hovered && !pressed {
			bg = pal.Hover
		}
	}
	blockui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
	blockui.TactileCard(btn, hovered, pressed, false, !enabled)
	btn.Children(func() {
		if p.AnnotGenerating {
			ui.Spinner(c).FontSize(theme.IconInline).TextColor(pal.Accent)
		} else {
			ui.Box(c).Size(7, 7).Shrink(0).Radius(theme.RadiusPill).Background(pal.AnnotSendDot)
		}
		ui.Text(c, label).FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
		if p.AnnotDrafts > 0 && !p.AnnotGenerating {
			ui.Text(c, strconv.Itoa(p.AnnotDrafts)).FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Accent).SingleLine()
		}
	})
	blockui.Probe(btn, "annotations-send-all")
	if enabled && btn.Clicked() {
		p.OnSendAnnotations()
	}
}
