//go:build !darwin

package app

import (
	"block/internal/plugins"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

// Windows and Linux trays take no drops; the icon's menu still works.
func attachMenubarDrop(string, *plugins.MenubarDrop, func(plugins.MenubarPayload)) func() {
	return nil
}

// readMenubarClipboard reads local files, a PNG image and text. Main thread only.
func readMenubarClipboard() plugins.MenubarPayload {
	var out plugins.MenubarPayload
	if files, err := mygo.Clipboard.ReadFiles(); err == nil {
		out.Files = files
	}
	if png, err := mygo.Clipboard.ReadFormat(transfer.PNG); err == nil && len(png) > 0 {
		out.Images = append(out.Images, png)
	}
	if text, err := mygo.Clipboard.ReadFormat(transfer.Text); err == nil {
		out.Text = string(text)
	}
	return out
}
