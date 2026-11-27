package ui

import (
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// ModalLayout describes a dialog's preferred dimensions in logical pixels.
// By default the card is horizontally centered below the title bar.
type ModalLayout struct {
	Width, MaxHeight float32
	Centered         bool
	Offset           ui.Point
	NoScroll         bool
}

func ModalCard(c *ui.Context, id, title string, open *bool, width, maxHeight float32, body func()) {
	Dialog(c, id, title, open, ModalLayout{Width: width, MaxHeight: maxHeight}, body)
}

func Dialog(c *ui.Context, id, title string, open *bool, layout ModalLayout, body func()) {
	if !*open {
		return
	}
	pal := theme.Ocean()
	w, h := c.Size()
	margin := float32(24)
	top := min(float32(96), max(h/4, 0))
	if layout.Centered {
		margin, top = 20, 20
	}
	width := min(layout.Width, max(w-2*margin, 1))
	height := max(h-top-margin, 1)
	if layout.MaxHeight > 0 {
		height = min(height, layout.MaxHeight)
	}
	ui.Overlay(c, func() {
		layer := ModalLayer(c, id).Padding(top, margin, margin, margin)
		if layout.Centered {
			layer.Justify(ui.Center)
		}
		layer.Children(func() {
			var card ui.Element
			if layout.NoScroll {
				card = ui.Column(c.Key(id)).Clip()
			} else {
				card = Scroll(c.Key(id)).Column()
			}
			card.Width(width).MaxHeight(height).
				Left(layout.Offset.X).Top(layout.Offset.Y).Radius(theme.RadiusXL).
				Background(pal.Menu).Border(1, pal.MenuBorder).
				Role(ui.RoleDialog).Label(title).Transition(ModalSpringTransition())
			Elevate(card, theme.ElevationDialog)
			Probe(card, id)
			card.Children(body)
			if card.PressedOutside() || layer.OverlayShortcut(0, ui.KeyEscape) {
				*open = false
			}
		})
	})
}

// ModalLayer builds the dimming scrim and focus scope as siblings inside an
// overlay. The renderer can retire each visual independently on dismissal.
func ModalLayer(c *ui.Context, id string) ui.Element {
	scrimMotion := ScrimFadeTransition()
	ui.Box(c.Key(id + "-scrim")).Absolute().Left(0).Right(0).Top(0).Bottom(0).
		Background(theme.Ocean().Scrim()).Transition(scrimMotion)
	return ui.Column(c.Key(id + "-layer")).Absolute().Left(0).Right(0).Top(0).Bottom(0).
		AlignItems(ui.Center).Modal()
}
