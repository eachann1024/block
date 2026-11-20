//go:build !darwin && !linux && !windows

package files

import "context"

func showPluginPicker(context.Context, pluginPickerOptions, func() bool) ([]string, error) {
	return nil, ErrFileDialogUnavailable
}
