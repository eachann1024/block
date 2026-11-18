//go:build !darwin

package theme

// ConfigureDefaultFontSmoothing is a no-op outside macOS.
func ConfigureDefaultFontSmoothing() {}
