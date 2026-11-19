package plugins

// Surface-scoped views let one plugin render several surfaces at once
// (settings page, settings sections, panels, dialogs). The settings page keeps
// its existing view path; other surfaces are wired by the contrib rollout.

// EnsureSurfaceView returns the non-blocking snapshot for one plugin surface,
// starting the initial render in the background when needed.
func (s *Supervisor) EnsureSurfaceView(pluginID string, surface Surface) PluginViewState {
	if surface == SurfaceSettingsPage {
		return s.EnsurePluginView(pluginID)
	}
	return PluginViewState{Loading: true}
}

// ReloadSurfaceViewAsync re-renders one plugin surface in the background.
func (s *Supervisor) ReloadSurfaceViewAsync(pluginID string, surface Surface) {
	if surface == SurfaceSettingsPage {
		s.ReloadPluginViewAsync(pluginID)
	}
}

// TriggerSurfaceActionAsync runs plugin.action for a button on one surface.
func (s *Supervisor) TriggerSurfaceActionAsync(pluginID string, surface Surface, actionID string) {
	if surface == SurfaceSettingsPage {
		s.TriggerActionAsync(pluginID, actionID)
	}
}

// UpdateSurfaceControlValue records a control edit on one surface.
func (s *Supervisor) UpdateSurfaceControlValue(pluginID string, surface Surface, controlID string, val any, immediate bool) {
	if surface == SurfaceSettingsPage {
		s.UpdateControlValue(pluginID, controlID, val, immediate)
	}
}
