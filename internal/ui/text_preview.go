package ui

import "github.com/egoist/mygo/ui"

// FadedTextPreview reserves a fixed number of lines and fades the last line
// when the text lays out past it. Background should match the surface behind it.
func FadedTextPreview(c *ui.Context, text string, lines int, fontSize, lineHeight float32, foreground, background ui.Color) {
	height := float32(lines) * lineHeight
	ui.Column(c).FillWidth().MinWidth(0).Height(height).Shrink(0).Clip().Children(func() {
		if text == "" {
			return
		}
		// One extra line is enough to detect overflow at the actual laid-out width.
		ui.Text(c, text).FillWidth().MinWidth(0).Shrink(0).
			FontSize(fontSize).FixedLineHeight(lineHeight).TextColor(foreground).
			MaxLines(lines + 1).
			DrawOver(func(p *ui.Painter, box ui.Rect) {
				if box.H <= height {
					return
				}
				fade := ui.Rect{X: box.X, Y: box.Y + height - lineHeight, W: box.W, H: lineHeight}
				p.FillGradient(fade, ui.LinearGradient{From: background.Alpha(0), To: background, Angle: 180}, 0)
			})
	})
}
