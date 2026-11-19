package pluginui

import (
	"block/internal/plugins"
	"github.com/egoist/mygo/ui"
)

// RenderSurface draws one plugin surface (settings section, panel or dialog
// body) with the same declarative renderer as plugin settings pages.
func (c *Controller) RenderSurface(ctx *ui.Context, pluginID string, surface plugins.Surface) {
	_ = c.sup.EnsureSurfaceView(pluginID, surface)
}

// RenderSettingsSections appends plugin sections declared for a built-in settings tab.
func (c *Controller) RenderSettingsSections(ctx *ui.Context, tab string) {
	for _, sec := range c.sup.SettingsSections(tab) {
		c.RenderSurface(ctx, sec.PluginID, sec.Surface)
	}
}
