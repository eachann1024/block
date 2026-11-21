package modals

import (
	"strings"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const (
	TabAppStore       = "app-store"
	TabStoreInstalled = "app-store-installed"
	TabPluginPrefix   = "plugin:"
)

func IsPluginSystemTab(tab string) bool {
	return IsStoreTab(tab) || strings.HasPrefix(tab, TabPluginPrefix)
}

func IsStoreTab(tab string) bool {
	return tab == TabAppStore || tab == TabStoreInstalled
}

// PluginNavEntry represents a lower-partition Settings sidebar row ("应用商店" or an installed app).
type PluginNavEntry struct {
	TabID     string
	Title     string
	CountText string
	Beta      bool
	Lit       bool
	StatusDot bool
	Starting  bool
	HasError  bool
	DrawIcon  func(c *ui.Context, size float32, colors theme.IconColors)
}

// PluginSettingsHost decouples Settings UI chrome from the App Store & out-of-process plugin runtime.
type PluginSettingsHost interface {
	SettingsTabChanged(previous, current string)
	StoreListing(tab string) bool
	RenderStoreFooter(c *ui.Context)
	NavEntries() []PluginNavEntry
	InstalledCount() int
	TabTitle(tab string) string
	TabIsBeta(tab string) bool
	RenderTitlebarLeading(c *ui.Context, tab string, onSelectTab func(nextTab string)) bool
	RenderTitlebarTrailing(c *ui.Context, tab string, onSelectTab func(nextTab string))
	RenderTitlebarOverlays(c *ui.Context, tab string, onSelectTab func(nextTab string))
	CloseTitlebarOverlays() bool
	IsStoreLightboxOpen() bool
	RenderHeaderExtras(c *ui.Context, tab string, onSelectTab func(nextTab string))
	RenderTabBody(c *ui.Context, tab string, onSelectTab func(nextTab string))
	RenderSettingsSections(c *ui.Context, tab string, onSelectTab func(nextTab string))
}
