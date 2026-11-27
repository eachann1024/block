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
	s.triggerSurfaceActionAsync(pluginID, surface, actionID, nil)
}

// UpdateSurfaceControlValue records a control edit on one surface.
func (s *Supervisor) UpdateSurfaceControlValue(pluginID string, surface Surface, controlID string, val any, immediate bool) {
	s.updateSurfaceControlValue(pluginID, s.NotebookRoot(), surface, controlID, val, immediate)
}

// surfaceViewOpened reports whether the surface has been shown in the current
// notebook, so plugin-initiated refreshes never build pages nobody opened.
func (s *Supervisor) surfaceViewOpened(pluginID string, surface Surface) bool {
	return s.surfaceViewOpenedForNotebook(pluginID, s.NotebookRoot(), surface)
}

func (s *Supervisor) surfaceViewOpenedForNotebook(pluginID, nbRoot string, surface Surface) bool {
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}
	s.mu.RLock()
	defer s.mu.RUnlock()
	vs, ok := s.views[key]
	return ok && vs.initialLoadStarted
}
