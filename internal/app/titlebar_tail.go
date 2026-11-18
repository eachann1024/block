package app

import (
	"fmt"
	"strconv"

	"block/internal/icons"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// titlebarActionTrailing matches thinkly-fast TITLEBAR_ACTION_TRAILING (TRAFFIC_X - 4 on macOS).
const titlebarActionTrailing = 12

// renderTitlebarTail renders the right rail (thinkly-fast render_titlebar_tail):
// notes show feedback, AI, then the page more pill; other surfaces put their
// context tools before feedback and AI.
func renderTitlebarTail(c *ui.Context, p TitlebarProps, rightInset float32) {
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
	ui.Row(c).FillHeight().Shrink(0).AlignItems(ui.Center).Gap(4).Padding(0, pad, 0, 0).Children(func() {
		if surface != SurfaceNote {
			renderNonNoteTitlebarTools(c, p, surface)
		}
		renderFeedbackButton(c, p)
		renderAIButton(c, p)
		if surface == SurfaceNote {
			renderPageMorePill(c, p)
		}
	})
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
		Tooltip:     fmt.Sprintf("%s %s", theme.Tr("AI 助手", "AI Assistant"), blockui.FormatShortcutDisplay(sc)),
		Active:      p.AIOpen,
		ActiveStyle: blockui.ActiveAccent,
	})
	if btn.Clicked() && p.OnToggleAI != nil {
		p.OnToggleAI()
	}
}

// renderPageMorePill is thinkly-fast titlebar_split_pill: the page more button,
// joined with the annotation send segment when the note has annotations.
func renderPageMorePill(c *ui.Context, p TitlebarProps) {
	pal := theme.Ocean()
	split := p.AnnotHasAny
	open := p.MenuState != nil && p.MenuState.PageMenuOpen
	more := func() {
		opts := blockui.IconButtonOptions{
			ID:          "titlebar-more",
			Label:       theme.Tr("页面操作", "Page Actions"),
			Active:      open,
			ActiveStyle: blockui.ActiveAccent,
		}
		if split {
			opts.Size, opts.Radius = 24, 6
		}
		btn := blockui.IconButton(c, icons.More, opts)
		if b := btn.Bounds(); p.MenuState != nil && b.W > 0 {
			p.MenuState.PageMenuAnchor = b
		}
		if btn.Clicked() && p.MenuState != nil {
			p.MenuState.TabMenuOpen = false
			p.MenuState.PageMenuOpen = !p.MenuState.PageMenuOpen
		}
	}
	if !split {
		more()
		return
	}
	c.Key("titlebar-action-pill")
	ui.Row(c).Height(28).Padding(2).Gap(2).Shrink(0).AlignItems(ui.Center).
		Radius(theme.RadiusControl).Border(1, pal.BtnBorder).Background(pal.Menu).Children(func() {
		renderSendAllSegment(c, p)
		ui.Box(c).Size(1, 14).Shrink(0).Background(pal.BtnBorder)
		more()
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
	btn := ui.ButtonBase(c).Height(24).PaddingX(9).Gap(6).Shrink(0).Radius(6).Center().Label(label).Disabled(!enabled).DimDisabled(false)
	fg := pal.TextSecondary
	switch {
	case active:
		fg = pal.AnnotSendFG
		btn.Background(pal.Selected)
	default:
		btn.Opacity(0.55)
	}
	if enabled {
		btn.Cursor(ui.CursorPointer)
		if btn.Hovered() {
			btn.Background(pal.Hover)
		}
	}
	btn.Children(func() {
		if p.AnnotGenerating {
			ui.Spinner(c).FontSize(theme.IconInline).TextColor(pal.Accent)
		} else {
			ui.Box(c).Size(7, 7).Shrink(0).Radius(theme.RadiusPill).Background(pal.AnnotSendDot)
		}
		ui.Text(c, label).FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
		if p.AnnotDrafts > 0 && !p.AnnotGenerating {
			ui.Text(c, strconv.Itoa(p.AnnotDrafts)).FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).FontWeight(theme.WeightTitle).TextColor(pal.Accent).SingleLine()
		}
	})
	blockui.Probe(btn, "annotations-send-all")
	if enabled && btn.Clicked() {
		p.OnSendAnnotations()
	}
}
