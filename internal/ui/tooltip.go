package ui

import (
	"strings"
	"time"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// TooltipPlacement controls whether the Ocean Paper tooltip prefers below or above the anchor.
type TooltipPlacement uint8

const (
	TooltipBelow TooltipPlacement = iota
	TooltipAbove
)

// TooltipTone selects the semantic color for a row's leading trigger phrase.
type TooltipTone uint8

const (
	TooltipToneDefault TooltipTone = iota
	TooltipToneAccent
	TooltipToneDanger
)

// TooltipRow describes one row inside an Ocean Paper tooltip card.
type TooltipRow struct {
	Trigger     string
	TriggerTone TooltipTone
	Detail      string
	SplitDetail bool
	Shortcut    string
}

// TooltipSpec configures a single-row or multi-row Ocean Paper tooltip (Scheme C).
type TooltipSpec struct {
	Rows        []TooltipRow
	Pill        bool
	Danger      bool
	Placement   TooltipPlacement
	Description string
}

func (s TooltipSpec) accessibilityText() string {
	if s.Description != "" {
		return s.Description
	}
	var parts []string
	for _, r := range s.Rows {
		var segs []string
		if r.Trigger != "" {
			segs = append(segs, r.Trigger)
		}
		if r.Detail != "" {
			segs = append(segs, r.Detail)
		}
		if r.Shortcut != "" {
			segs = append(segs, r.Shortcut)
		}
		if len(segs) > 0 {
			parts = append(parts, strings.Join(segs, " "))
		}
	}
	return strings.Join(parts, " · ")
}

func styleTooltipSurface(tip, anchor ui.Element, pal theme.Palette, spec TooltipSpec) {
	radius := theme.RadiusLG
	if spec.Pill {
		radius = theme.RadiusPill
	}
	tip.Transition(ui.ElementTransition{
		Duration: 100 * time.Millisecond,
		Colors:   true,
		Enter:    &ui.Motion{},
		Exit:     &ui.Motion{},
	}).Radius(radius).Background(pal.TooltipBG).Clip()
	Elevate(tip, theme.ElevationPopover)
	if spec.Placement == TooltipAbove {
		tip.AttachTo(anchor, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, theme.TooltipOffset, 0)
	} else {
		tip.AttachTo(anchor, ui.AnchorBottom, ui.AnchorTop).Margin(theme.TooltipOffset, 0, 0, 0)
	}
}

func renderTooltipShortcutPill(c *ui.Context, pal theme.Palette, shortcut string, _ bool) {
	ShortcutLabel(c, shortcut, ShortcutLabelStyle{
		Font:       "monospace",
		FontSize:   theme.FontMeta,
		LineHeight: theme.LineMeta,
		FontWeight: theme.WeightMono,
		Color:      pal.TooltipMuted,
		IsShortcut: true,
	})
}

func renderTooltipRow(c *ui.Context, pal theme.Palette, row TooltipRow, pill bool) {
	triggerCol := pal.Text
	switch row.TriggerTone {
	case TooltipToneAccent:
		triggerCol = pal.TooltipAccent
	case TooltipToneDanger:
		triggerCol = pal.TooltipDanger
	}
	if row.Trigger != "" {
		triggerWeight := theme.WeightRegular
		if row.Detail != "" || row.TriggerTone != TooltipToneDefault {
			triggerWeight = theme.WeightControl
		}
		ShortcutLabel(c, row.Trigger, ShortcutLabelStyle{
			FontSize:   theme.FontSmall,
			LineHeight: theme.LineSmall,
			FontWeight: triggerWeight,
			Color:      triggerCol,
		})
	}
	if row.Detail != "" {
		ShortcutLabel(c, row.Detail, ShortcutLabelStyle{
			FontSize:   theme.FontSmall,
			LineHeight: theme.LineSmall,
			FontWeight: theme.WeightRegular,
			Color:      pal.TooltipMuted,
		})
	}
	if row.Shortcut != "" {
		renderTooltipShortcutPill(c, pal, row.Shortcut, pill)
	}
}

// RichTooltip attaches an Ocean Paper tooltip card (single-row, shortcut-badge, split, or multi-row divided) to anchor.
func RichTooltip(c *ui.Context, anchor ui.Element, spec TooltipSpec) ui.Element {
	if len(spec.Rows) == 0 {
		return anchor
	}
	if desc := spec.accessibilityText(); desc != "" {
		anchor.Description(desc)
	}
	pal := theme.Ocean()
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		styleTooltipSurface(tip, anchor, pal, spec)
		if len(spec.Rows) == 1 {
			row := spec.Rows[0]
			gap := float32(7)
			if !row.SplitDetail && row.Shortcut == "" && row.Detail != "" {
				gap = 5
			}
			padX := float32(11)
			if row.Shortcut != "" && !spec.Pill {
				padX = 10
			}
			tip.Padding(5, padX, 5, padX).AlignItems(ui.Center).Justify(ui.Center)
			tip.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(gap).Shrink(0).Children(func() {
					renderTooltipRow(c, pal, row, spec.Pill)
				})
			})
			return
		}
		tip.Children(func() {
			ui.Column(c).AlignItems(ui.Stretch).Shrink(0).Children(func() {
				for i, r := range spec.Rows {
					row := r
					if i > 0 {
						ui.Box(c).Height(1).FillWidth().Background(pal.TooltipDivider)
					}
					gap := float32(5)
					if row.Shortcut != "" || row.SplitDetail {
						gap = 7
					}
					ui.Row(c).Padding(6, 12, 6, 12).AlignItems(ui.Center).Gap(gap).Shrink(0).Children(func() {
						renderTooltipRow(c, pal, row, false)
					})
				}
			})
		})
	})
	return anchor
}

// Tooltip attaches a single-line Ocean Paper tooltip card to anchor.
func Tooltip(c *ui.Context, anchor ui.Element, text string) ui.Element {
	if text == "" {
		return anchor
	}
	return RichTooltip(c, anchor, TooltipSpec{
		Rows:        []TooltipRow{{Trigger: text}},
		Description: text,
	})
}

// WrappedTooltip uses the shared tooltip surface for text that may span several lines.
func WrappedTooltip(c *ui.Context, anchor ui.Element, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	anchor.Description(text)
	pal := theme.Ocean()
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		styleTooltipSurface(tip, anchor, pal, TooltipSpec{Placement: TooltipAbove})
		width, _ := c.Size()
		maxWidth := float32(460)
		if width > 2*theme.Space12 {
			maxWidth = min(maxWidth, width-2*theme.Space12)
		}
		tip.MaxWidth(maxWidth).Padding(5, 11)
		tip.Children(func() {
			ui.Text(c, text).MinWidth(0).FontSize(theme.FontSmall).
				FixedLineHeight(theme.LineSmall).TextColor(pal.Text)
		})
	})
}

// TooltipShortcut attaches an Ocean Paper tooltip with primary action label and a rounded shortcut badge.
func TooltipShortcut(c *ui.Context, anchor ui.Element, label, shortcut string) ui.Element {
	if label == "" && shortcut == "" {
		return anchor
	}
	return RichTooltip(c, anchor, TooltipSpec{
		Rows: []TooltipRow{{Trigger: label, Shortcut: shortcut}},
	})
}

// TooltipSplit attaches a single-row Ocean Paper tooltip with a primary label and hairline-separated secondary note.
func TooltipSplit(c *ui.Context, anchor ui.Element, primary, detail string) ui.Element {
	if primary == "" {
		return anchor
	}
	if detail == "" {
		return Tooltip(c, anchor, primary)
	}
	return RichTooltip(c, anchor, TooltipSpec{
		Rows: []TooltipRow{{Trigger: primary, Detail: detail, SplitDetail: true}},
	})
}

// TooltipCardSpec configures a multi-line Ocean Paper informational tooltip card
// with a title, optional monospace code identifier, primary body, and optional muted note.
type TooltipCardSpec struct {
	Title     string
	Code      string
	Body      string
	Note      string
	Placement TooltipPlacement
}

// TooltipCard attaches a multi-line structured Ocean Paper tooltip card to anchor.
func TooltipCard(c *ui.Context, anchor ui.Element, spec TooltipCardSpec) ui.Element {
	title := strings.TrimSpace(spec.Title)
	code := strings.TrimSpace(spec.Code)
	body := strings.TrimSpace(spec.Body)
	note := strings.TrimSpace(spec.Note)
	if title == "" && body == "" && note == "" {
		return anchor
	}
	var a11y []string
	if title != "" {
		a11y = append(a11y, title)
	}
	if body != "" {
		a11y = append(a11y, body)
	}
	if note != "" {
		a11y = append(a11y, note)
	}
	anchor.Description(strings.Join(a11y, " · "))
	pal := theme.Ocean()
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		styleTooltipSurface(tip, anchor, pal, TooltipSpec{Placement: spec.Placement})
		width, _ := c.Size()
		maxWidth := float32(340)
		if width > 2*theme.Space12 {
			maxWidth = min(maxWidth, width-2*theme.Space12)
		}
		tip.MaxWidth(maxWidth).Padding(theme.Space8, theme.Space12)
		tip.Children(func() {
			ui.Column(c).MinWidth(0).Gap(theme.Space4).Children(func() {
				if title != "" || code != "" {
					ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(theme.Space6).Wrap().Children(func() {
						if title != "" {
							ui.Text(c, title).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
								FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
						}
						if code != "" {
							ui.Text(c, code).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								FontWeight(theme.WeightMono).TextColor(pal.TooltipMuted).SingleLine()
						}
					})
				}
				if body != "" {
					ui.Text(c, body).MinWidth(0).FontSize(theme.FontSmall).
						FixedLineHeight(theme.LineSmall).TextColor(pal.Text)
				}
				if note != "" {
					ui.Box(c).FillWidth().Height(1).Background(pal.TooltipDivider).Margin(theme.Space2, 0, theme.Space2, 0)
					ui.Text(c, note).MinWidth(0).FontSize(theme.FontMeta).
						FixedLineHeight(theme.LineMeta).TextColor(pal.TooltipMuted)
				}
			})
		})
	})
	return anchor
}

// TooltipTrigger attaches a semantic trigger+effect tooltip (Ocean blue accent or destructive red).
func TooltipTrigger(c *ui.Context, anchor ui.Element, trigger, detail string, danger bool) ui.Element {
	tone := TooltipToneAccent
	if danger {
		tone = TooltipToneDanger
	}
	return RichTooltip(c, anchor, TooltipSpec{
		Danger: danger,
		Rows: []TooltipRow{{
			Trigger:     trigger,
			TriggerTone: tone,
			Detail:      detail,
		}},
	})
}
