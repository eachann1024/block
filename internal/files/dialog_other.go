//go:build !darwin

package files

import (
	"context"

	"github.com/egoist/mygo"
)

func darwinBundleIntact() bool {
	return true
}

// MaybeRunDarwinPickerHelper is a no-op on non-macOS platforms.
func MaybeRunDarwinPickerHelper() bool {
	return false
}

// PrewarmFileDialogs is a no-op on non-macOS platforms.
func PrewarmFileDialogs() {}

// ShutdownFileDialogs is a no-op on non-macOS platforms.
func ShutdownFileDialogs() {}

// ResumeFileDialogs is a no-op on non-macOS platforms.
func ResumeFileDialogs() {}

func openDialogDarwin(context.Context, *mygo.Window, mygo.OpenDialogOptions, func() bool) ([]string, error) {
	return nil, ErrFileDialogUnavailable
}

func saveDialogDarwin(context.Context, *mygo.Window, mygo.SaveDialogOptions, func() bool) (string, error) {
	return "", ErrFileDialogUnavailable
}
