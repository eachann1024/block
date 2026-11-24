//go:build darwin

package files

import "context"

func showPluginPicker(ctx context.Context, opts pluginPickerOptions, allowed func() bool) ([]string, error) {
	req := openOptsToDarwinRequest(opts.OpenDialogOptions)
	if opts.save {
		req.Op = "save"
	}
	return showDarwinNativePanel(ctx, opts.Parent, req, allowed)
}
