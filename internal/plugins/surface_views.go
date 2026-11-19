package plugins

// Surface-scoped views let one plugin render several surfaces at once
// (settings page, settings sections, panels, dialogs).

// EnsureSurfaceView returns the non-blocking snapshot for one plugin surface,
// starting the initial render in the background when needed.
func (s *Supervisor) EnsureSurfaceView(pluginID string, surface Surface) PluginViewState {
	return s.ensureSurfaceView(pluginID, surface)
}

// ReloadSurfaceViewAsync re-renders one plugin surface in the background.
func (s *Supervisor) ReloadSurfaceViewAsync(pluginID string, surface Surface) {
	s.reloadSurfaceViewAsync(pluginID, surface)
}

// TriggerSurfaceActionAsync runs plugin.action for a button on one surface.
func (s *Supervisor) TriggerSurfaceActionAsync(pluginID string, surface Surface, actionID string) {
	s.triggerSurfaceActionAsync(pluginID, surface, actionID)
}

// UpdateSurfaceControlValue records a control edit on one surface.
func (s *Supervisor) UpdateSurfaceControlValue(pluginID string, surface Surface, controlID string, val any, immediate bool) {
	s.updateSurfaceControlValue(pluginID, s.NotebookRoot(), surface, controlID, val, immediate)
}
