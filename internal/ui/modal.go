package ui

import (
	"time"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// ModalLayout describes a dialog's preferred dimensions in logical pixels.
// By default the card is horizontally centered below the title bar.
type ModalLayout struct {
	Width, MaxHeight float32
	Centered         bool
	Offset           ui.Point
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
			card := ui.Scroll(c.Key(id)).Column().Width(width).MaxHeight(height).
				Left(layout.Offset.X).Top(layout.Offset.Y).Radius(theme.RadiusSurface).
				Background(pal.Menu).Border(1, pal.MenuBorder).
				Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, .08)).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, .14)).
				Role(ui.RoleDialog).Label(title)
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
	motion := ui.ElementTransition{
		Duration: 180 * time.Millisecond, ExitDuration: 160 * time.Millisecond,
		Ease: ui.CubicBezier(.16, 1, .3, 1), Colors: true,
	}
	ui.Box(c.Key(id + "-scrim")).Absolute().Left(0).Right(0).Top(0).Bottom(0).
		Background(ui.RGBA(0x20, 0x24, 0x2c, 46.0/255)).Transition(motion)
	return ui.Column(c.Key(id + "-layer")).Absolute().Left(0).Right(0).Top(0).Bottom(0).
		AlignItems(ui.Center).Modal().Transition(motion)
}
