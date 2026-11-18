package modals

import (
	"strings"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const (
	TabAppStore     = "app-store"
	TabPluginPrefix = "plugin:"
)

func IsPluginSystemTab(tab string) bool {
	return tab == TabAppStore || strings.HasPrefix(tab, TabPluginPrefix)
}

// PluginNavEntry represents a lower-partition Settings sidebar row ("应用商店" or an installed app).
type PluginNavEntry struct {
	TabID     string
	Title     string
	CountText string
	StatusDot bool
	DrawIcon  func(c *ui.Context, size float32, colors theme.IconColors)
}

// PluginSettingsHost decouples Settings UI chrome from the App Store & out-of-process plugin runtime.
type PluginSettingsHost interface {
	NavEntries() []PluginNavEntry
	TabTitle(tab string) string
	RenderHeaderExtras(c *ui.Context, tab string, onSelectTab func(nextTab string))
	RenderTabBody(c *ui.Context, tab string, onSelectTab func(nextTab string))
}
