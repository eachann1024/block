package theme

import (
	"sync"

	"block/assets"
	"github.com/egoist/mygo/ui"
)

// DefaultCodeFontName is the family name of the bundled JetBrains Mono font.
const DefaultCodeFontName = "JetBrains Mono"

// Bundled JetBrains Mono v2.304 static weights (SIL Open Font License 1.1).
var builtinCodeFontPaths = []string{
	"fonts/JetBrainsMono-Regular.ttf",
	"fonts/JetBrainsMono-Medium.ttf",
	"fonts/JetBrainsMono-Bold.ttf",
	"fonts/JetBrainsMono-Italic.ttf",
	"fonts/JetBrainsMono-BoldItalic.ttf",
}

var builtinCodeFontsOnce sync.Once

// EnsureBuiltinCodeFonts registers the bundled JetBrains Mono static faces
// into the UI text engine once so code editors render with a real monospace
// font out of the box on macOS, Windows, and Linux.
func EnsureBuiltinCodeFonts() {
	builtinCodeFontsOnce.Do(func() {
		registered := false
		for _, rel := range builtinCodeFontPaths {
			data, err := assets.Read(rel)
			if err != nil || len(data) == 0 {
				continue
			}
			if err := ui.RegisterFont(data, DefaultCodeFontName); err == nil {
				registered = true
			}
		}
		if registered {
			BumpFontEpoch()
		}
	})
}
