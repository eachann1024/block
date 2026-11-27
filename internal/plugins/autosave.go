package plugins

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"time"

	"block/internal/files"
)

const autosaveDelay = 800 * time.Millisecond

type autosaveEdit struct {
	baseline any
	actionID string
	timer    *time.Timer
	order    uint64 // nonzero once a blur, timer, or discrete edit queues the save
}

type formAutosave struct {
	edits    map[string]*autosaveEdit
	order    uint64
	running  bool
	focused  string
	deferred *autosavePageUpdate
}

type autosavePageUpdate struct {
	previous *UIPage
	page     *UIPage
	values   map[string]any
	seq      uint64
}

func (p *UIPage) autosaveControl(id string) (UIItem, bool) {
	if p != nil {
		for _, card := range p.Cards {
			for _, item := range card.Items {
				if item.ID == id && item.Autosave != "" {
					return item, true
				}
				for _, field := range item.Fields {
					if field.ID == id && field.Autosave != "" {
						return field, true
					}
				}
			}
		}
	}
	return UIItem{}, false
}

func sameFormValue(kind ItemKind, a, b any) bool {
	if kind == KindChecklist {
		left, leftOK := StringSliceValue(a)
		right, rightOK := StringSliceValue(b)
		return leftOK && rightOK && slices.Equal(left, right)
	}
	return reflect.DeepEqual(a, b)
}

func (s *Supervisor) recordAutosaveEditLocked(key viewKey, view *PluginViewState, item UIItem, previous, next any) {
	if sameFormValue(item.Kind, previous, next) {
		return
	}
	state := &view.autosave
	if state.edits == nil {
		state.edits = make(map[string]*autosaveEdit)
	}
	baseline := previous
	if old := state.edits[item.ID]; old != nil {
		baseline = old.baseline
		if old.timer != nil {
			old.timer.Stop()
		}
	}
	if sameFormValue(item.Kind, baseline, next) {
		delete(state.edits, item.ID)
		return
	}
	edit := &autosaveEdit{baseline: baseline, actionID: item.Autosave}
	state.edits[item.ID] = edit
	if item.Kind != KindInput && item.Kind != KindSecret {
		s.queueAutosaveLocked(key, view, edit)
		return
	}
	edit.timer = time.AfterFunc(autosaveDelay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.views[key] == view && view.autosave.edits[item.ID] == edit && edit.order == 0 {
			s.queueAutosaveLocked(key, view, edit)
		}
	})
}

// SurfaceInputFocus binds a focus report to the current view, including its
// notebook and dialog instance. An old control's unmount cannot blur a new view.
func (s *Supervisor) SurfaceInputFocus(pluginID string, surface Surface) func(string) {
	key := viewKey{pluginID: pluginID, notebookRoot: s.NotebookRoot(), surface: surface}
	s.mu.RLock()
	view := s.views[key]
	s.mu.RUnlock()
	return func(controlID string) {
		s.mu.Lock()
		if view == nil || s.views[key] != view || view.autosave.focused == controlID {
			s.mu.Unlock()
			return
		}
		state := &view.autosave
		previous := state.focused
		state.focused = controlID
		if edit := state.edits[previous]; edit != nil && edit.order == 0 {
			s.queueAutosaveLocked(key, view, edit)
		}
		updated := false
		if deferred := state.deferred; deferred != nil {
			state.deferred = nil
			if view.Page == deferred.previous {
				view.mergeAutosavePage(deferred.page, deferred.values, deferred.seq)
				updated = true
			}
		}
		s.mu.Unlock()
		if updated {
			s.notifyRedraw()
		}
	}
}

func (s *Supervisor) queueAutosaveLocked(key viewKey, view *PluginViewState, edit *autosaveEdit) {
	if edit.timer != nil {
		edit.timer.Stop()
		edit.timer = nil
	}
	state := &view.autosave
	state.order++
	edit.order = state.order
	if !state.running {
		state.running = true
		go s.runAutosaves(key, view)
	}
}

func (s *Supervisor) runAutosaves(key viewKey, view *PluginViewState) {
	for {
		if !s.runNextAutosave(key, view) {
			return
		}
	}
}

func (s *Supervisor) runNextAutosave(key viewKey, view *PluginViewState) bool {
	// Wait for any render or explicit action first. Take the form snapshot at
	// dispatch so the follow-up includes changes made while the plugin was busy.
	defer s.lockPluginRPC(key.pluginID)()
	epoch := s.notebookEpoch.Load()
	s.mu.Lock()
	state := &view.autosave
	if s.views[key] != view || s.stopping || s.replacingID == key.pluginID {
		state.running = false
		s.mu.Unlock()
		return false
	}
	var actionID string
	var latest uint64
	for id, edit := range state.edits {
		if edit.order == 0 {
			continue
		}
		delete(state.edits, id)
		if edit.order > latest {
			actionID = edit.actionID
			latest = edit.order
		}
	}
	if actionID == "" {
		state.running = false
		s.mu.Unlock()
		return false
	}
	values := cloneAnyMap(view.Values)
	seq, token := view.editSeq, view.dialogToken
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	rt, err := s.ensureRuntime(ctx, key.pluginID, key.notebookRoot)
	var res *ActionResult
	if err == nil {
		s.mu.Lock()
		if s.views[key] != view || !s.isRuntimeActiveLocked(key.pluginID, rt) {
			s.mu.Unlock()
			return true
		}
		s.activeActionRT[key.pluginID] = rt
		s.mu.Unlock()
		rt.SetNotebookRoot(key.notebookRoot)
		rt.SetActiveNote(s.activeNoteFor(key.notebookRoot))
		scope := systemAction{notebookRoot: key.notebookRoot, epoch: epoch}
		res, err = rt.actionWithTrigger(context.WithValue(ctx, systemActionKey{}, scope), key.surface, actionID, values, token, "autosave")
	}

	if err != nil {
		files.LogErrorf("plugin autosave failed: id=%s action=%s err=%v", key.pluginID, actionID, err)
	}
	locale := s.Locale()
	tone := resolveActionTone(res, err)
	s.mu.Lock()
	if rt != nil && s.activeActionRT[key.pluginID] == rt {
		delete(s.activeActionRT, key.pluginID)
	}
	current := s.views[key] == view
	if current {
		view.AutosaveTone, view.AutosaveMessage = "", ""
		if res != nil {
			sKey := surfaceKey{pluginID: key.pluginID, surface: key.surface}
			s.formVersions[sKey]++
			view.formVersion = s.formVersions[sKey]
			if tone == "success" && len(res.SyncedNotebooks) > 0 {
				s.clearNotebookSaveErrorsLocked(key.pluginID, res.SyncedNotebooks)
			}
			updates := map[string]any{}
			if res.Page != nil {
				view.lastLocale = rt.locale()
				view.autosave.deferred = nil
				updates = res.Page.ExtractDefaultValues()
			}
			for id, value := range res.Values {
				updates[id] = value
			}
			view.mergeAutosavePage(res.Page, updates, seq)
		}
		if tone == "danger" || tone == "warning" {
			view.AutosaveTone = tone
			switch {
			case err != nil:
				view.AutosaveMessage = err.Error()
			case res != nil:
				view.AutosaveMessage = strings.TrimSpace(res.Status)
			}
			if view.AutosaveMessage == "" {
				message := LocalizedText{Zh: "自动保存失败", En: "Autosave failed"}
				if tone == "warning" {
					message = LocalizedText{Zh: "自动保存有警告", En: "Autosave warning"}
				}
				view.AutosaveMessage = message.Resolve(locale)
			}
		}
	}
	s.mu.Unlock()
	if current {
		s.notifyChanged()
	}
	return true
}

// Only other controls can take values from an autosave response. If the new
// layout would remove or move the focused input, apply that layout on blur.
func (view *PluginViewState) mergeAutosavePage(page *UIPage, updates map[string]any, seq uint64) {
	focus := view.autosave.focused
	for id := range updates {
		if view.protectAutosaveValue(id) || view.editedSeq[id] > seq {
			delete(updates, id)
		}
	}
	if page != nil {
		view.autosave.deferred = nil
	}
	if view.autosave.edits != nil && focus != "" {
		oldSlot := view.Page.inputSlot(focus, view.Values)
		if oldSlot != (inputSlot{}) {
			candidate := cloneAnyMap(view.Values)
			for id, value := range updates {
				candidate[id] = value
			}
			next := page
			if next == nil {
				next = view.Page
			}
			if next.inputSlot(focus, candidate) != oldSlot {
				blocked := make(map[string]any)
				for _, id := range []string{oldSlot.visibleBy, oldSlot.groupVisibleBy} {
					if value, ok := updates[id]; ok {
						blocked[id] = value
						delete(updates, id)
					}
				}
				view.autosave.deferred = &autosavePageUpdate{previous: view.Page, page: page, values: blocked, seq: seq}
				page = nil
			}
		}
	}
	if page != nil {
		view.Page = page
	}
	for id, value := range updates {
		view.Values[id] = value
		delete(view.editedControls, id)
		delete(view.editedSeq, id)
	}
}

func (view *PluginViewState) protectAutosaveValue(id string) bool {
	return view.autosave.edits[id] != nil || (view.autosave.edits != nil && view.autosave.focused == id)
}

type inputSlot struct {
	card, kind, layout, group, firstField, groupVisibleBy, visibleBy string
	collapsible, defaultCollapsed                                    bool
}

func (p *UIPage) inputSlot(id string, values map[string]any) inputSlot {
	if p == nil {
		return inputSlot{}
	}
	for _, card := range p.Cards {
		for _, item := range card.Items {
			if !IsItemVisible(item, values) {
				continue
			}
			if item.ID == id && (item.Kind == KindInput || item.Kind == KindSecret) {
				return inputSlot{card: card.ID, kind: string(item.Kind), layout: item.Layout, visibleBy: visibilityControl(item),
					collapsible: card.Collapsible, defaultCollapsed: card.DefaultCollapsed}
			}
			for _, field := range item.Fields {
				if field.ID == id && (field.Kind == KindInput || field.Kind == KindSecret) && IsItemVisible(field, values) {
					firstField := ""
					if item.ID == "" {
						firstField = item.Fields[0].ID
					}
					layout := field.Layout
					if layout == "" {
						layout = "stacked"
					}
					return inputSlot{card: card.ID, kind: string(field.Kind), layout: layout, group: item.ID,
						firstField: firstField, groupVisibleBy: visibilityControl(item), visibleBy: visibilityControl(field),
						collapsible: card.Collapsible, defaultCollapsed: card.DefaultCollapsed}
				}
			}
		}
	}
	return inputSlot{}
}

func visibilityControl(item UIItem) string {
	if item.VisibleWhen != nil {
		return item.VisibleWhen.ControlID
	}
	return ""
}
