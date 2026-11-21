package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"block/internal/editor"
	"block/internal/files"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/tarot"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const tarotPluginID = "block-tarot"

func (w *Workspace) initTarot() {
	w.tarot = tarot.NewController(w.dataDir, tarot.Host{
		OpenNote: func(absPath string) {
			if absPath == "" {
				return
			}
			_ = w.OpenNote(absPath, 0)
		},
		ToggleAI: func() {
			w.ToggleAIPanel()
		},
		OpenAIWithMsg: func(prompt string) {
			if w.ai == nil || !w.ai.Enabled() {
				return
			}
			if !w.ai.IsOpen() {
				w.ai.SetOpen(true)
			}
			if strings.TrimSpace(prompt) != "" {
				w.ai.NewConversation()
				w.ai.SendPrompt(prompt)
			}
			w.RequestRedraw()
		},
		ExitTarot: func() {
			w.SetTarotMode(false)
		},
		ToggleSidebar: func() {
			w.mu.RLock()
			vpW := w.lastViewportW
			w.mu.RUnlock()
			if vpW <= 0 {
				vpW = 1200
			}
			w.ToggleSidebar(vpW)
		},
		NewNote: func() {
			_, _ = w.CreateNewNote()
		},
		OpenSearch: func() {
			w.modals.OpenSearch(false)
		},
		OpenSettings: func() {
			w.OpenSettings("appearance")
		},
		Notebooks: w.Notebooks,
		SwitchNotebook: func(root string) {
			_ = w.SetNotebook(root)
		},
		ChooseNotebook: w.ChooseNotebookDialog,
		RequestRedraw: func() {
			w.RequestRedraw()
		},
		ReadNoteText: func(absPath string) string {
			if sess := w.SessionFor(absPath); sess != nil {
				return sess.Markdown()
			}
			raw, err := os.ReadFile(absPath)
			if err != nil {
				return ""
			}
			return string(raw)
		},
		CompleteAI: func(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
			if w.ai == nil {
				return "", fmt.Errorf("ai unavailable")
			}
			return w.ai.CompletePrompt(ctx, systemPrompt, userPrompt)
		},
		IsAIReady: func() bool {
			return w.ai != nil && w.ai.Enabled() && w.ai.IsLiveProviderReady()
		},
		IsAIOpen: func() bool {
			return w.ai != nil && w.ai.IsOpen()
		},
		IsAIEnabled: func() bool {
			if w.ai == nil || !w.ai.Enabled() {
				return false
			}
			if sup := w.Plugins(); sup != nil && sup.TitlebarHidden(plugins.TitlebarSlotAI) {
				return false
			}
			return true
		},
		IsAIStreaming: func() bool {
			return w.ai != nil && w.ai.IsStreaming()
		},
	})
}

// Tarot returns the workspace's Tarot Mode controller.
func (w *Workspace) Tarot() *tarot.Controller {
	return w.tarot
}

func (w *Workspace) isTarotPluginEnabled() bool {
	sup := w.Plugins()
	return sup != nil && sup.IsPluginEnabled(tarotPluginID)
}

func (w *Workspace) syncTarotWithPluginState(initial bool) {
	enabled := w.isTarotPluginEnabled()
	if initial {
		if enabled && w.State().TarotMode {
			w.SetTarotMode(true)
			return
		}
		theme.SetTarotMode(false)
		return
	}
	if theme.IsTarotMode() && !enabled {
		w.SetTarotMode(false)
	}
}

func tarotRoomName(r tarot.Room) string {
	switch r {
	case tarot.RoomTable:
		return "table"
	case tarot.RoomDaily:
		return "daily"
	case tarot.RoomDeck:
		return "deck"
	default:
		return "writing"
	}
}

// TarotStateSnapshot reports the current native Tarot Mode state to host.ui.tarot_state.
func (w *Workspace) TarotStateSnapshot() plugins.TarotState {
	st := plugins.TarotState{
		Supported: true,
		Enabled:   theme.IsTarotMode(),
		Room:      "writing",
		Total:     len(tarot.MajorArcana),
	}
	if w.tarot == nil {
		return st
	}
	st.Room = tarotRoomName(w.tarot.Room())
	snap := w.tarot.Store().Snapshot()
	st.Collected = len(snap.Collected)
	today := time.Now().Format("2006-01-02")
	if snap.Daily.Date == today && snap.Daily.Flipped {
		st.DailyDrawn = true
	}
	w.tarot.SyncNotes(w.EntriesSnapshot(), w.State().ActiveTab, w.activeTabDirty())
	if active, ok := w.tarot.ActiveNote(); ok {
		st.ActiveNote = active.Title
	}
	if snap.Daily.Date == today && snap.Daily.NotePath != "" {
		st.DailyNote = strings.TrimSuffix(filepathBase(snap.Daily.NotePath), ".md")
	}
	return st
}

func filepathBase(p string) string {
	idx := strings.LastIndexAny(p, `/\`)
	if idx >= 0 && idx+1 < len(p) {
		return p[idx+1:]
	}
	return p
}

func (w *Workspace) EntriesSnapshot() []model.FileEntry {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]model.FileEntry(nil), w.entries...)
}

func (w *Workspace) activeTabDirty() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if s := w.sessions[w.state.ActiveTab]; s != nil && s.Dirty() {
		return true
	}
	return false
}

// ApplyTarotPluginControl applies host.ui.set_tarot from the block-tarot plugin.
func (w *Workspace) ApplyTarotPluginControl(enabled *bool, toggle bool, room string) plugins.TarotState {
	roomKey := strings.ToLower(strings.TrimSpace(room))
	on := theme.IsTarotMode()
	switch {
	case toggle:
		on = !on
	case enabled != nil:
		on = *enabled
	case roomKey != "":
		on = true
	}
	w.SetTarotMode(on)
	if on && w.tarot != nil && roomKey != "" {
		switch roomKey {
		case "writing":
			w.tarot.SetRoom(tarot.RoomWriting)
		case "table":
			w.tarot.SetRoom(tarot.RoomTable)
		case "daily":
			w.tarot.SetRoom(tarot.RoomDaily)
		case "deck":
			w.tarot.SetRoom(tarot.RoomDeck)
		}
		if w.modals != nil && w.modals.IsSettingsOpen() {
			w.modals.CloseSettings()
		}
	}
	return w.TarotStateSnapshot()
}

// ToggleTarotMode switches Tarot Mode on or off and persists the preference.
func (w *Workspace) ToggleTarotMode() {
	w.SetTarotMode(!theme.IsTarotMode())
}

// SetTarotMode enables or disables Tarot Mode while preserving the open note and cursor.
func (w *Workspace) SetTarotMode(on bool) {
	if on && !w.isTarotPluginEnabled() {
		on = false
	}
	prev := theme.IsTarotMode()
	theme.SetTarotMode(on)
	next, err := files.UpdateState(func(st *model.AppState) {
		st.TarotMode = on
	})
	w.mu.Lock()
	if err == nil {
		w.state = next
	} else {
		w.state.TarotMode = on
	}
	w.tarotSyncKey = ""
	entries := append([]model.FileEntry(nil), w.entries...)
	activeTab := w.state.ActiveTab
	activeDirty := false
	if s := w.sessions[activeTab]; s != nil && s.Dirty() {
		activeDirty = true
	}
	w.mu.Unlock()

	if on && w.tarot != nil {
		w.tarot.Enter()
		w.tarot.SyncNotes(entries, activeTab, activeDirty)
	}
	if w.quick != nil {
		w.quick.RefreshStyle()
	}
	if prev != on {
		if sup := w.Plugins(); sup != nil {
			sup.EmitWorkspaceEvent("theme.changed", map[string]any{"tarot": on})
		}
	}
	w.RequestRedraw()
}

func (w *Workspace) syncTarotDeckIfNeeded(entries []model.FileEntry, activeNB, activeTab string, activeDirty bool) {
	if w.tarot == nil {
		return
	}
	key := fmt.Sprintf("%s|%s|%v|%d", activeNB, activeTab, activeDirty, countMarkdownEntries(entries))
	w.mu.Lock()
	same := w.tarotSyncKey == key
	if !same {
		w.tarotSyncKey = key
	}
	w.mu.Unlock()
	if !same {
		w.tarot.SyncNotes(entries, activeTab, activeDirty)
	}
}

func countMarkdownEntries(entries []model.FileEntry) int {
	total := 0
	for _, e := range entries {
		if e.IsDir {
			total += countMarkdownEntries(e.Children)
		} else if e.IsMarkdown {
			total++
		}
	}
	return total
}

func (w *Workspace) renderTarotWorkspace(
	c *ui.Context,
	vpW float32,
	layout ShellLayout,
	entries []model.FileEntry,
	activeNB string,
	activeTab string,
	activeDirty bool,
	activeSurf *TabSurface,
	activeSess *editor.Session,
	toast *HistoryToast,
	aiEnabled bool,
	sidebarWide bool,
	effMainW float32,
	cardHorizMargin float32,
	edgeSafeL float32,
	edgeSafeR float32,
) {
	w.syncTarotDeckIfNeeded(entries, activeNB, activeTab, activeDirty)

	tc := theme.Tarot()
	midPal := theme.TarotMidnightPalette()
	midTheme := midPal.ToUITheme("")
	midTheme.ScrollbarStyle = ui.ScrollbarStyle{
		TrackWidth: theme.ScrollGutter,
		ThumbWidth: theme.ScrollThumbWidth,
		MinLength:  32,
		Thumb:      midPal.ScrollThumb,
		Hover:      midPal.ScrollThumbHover,
		Active:     midPal.ScrollThumbHover,
	}
	c.SetTheme(&midTheme)

	room := w.tarot.Room()
	root := ui.Column(c.Key("workspace-root")).Fill().Background(tc.Ground).TextColor(tc.TextPrimary).Clip()
	blockui.Probe(root, "workspace-root")

	root.Children(func() {
		// 1. Unified 44px Tarot Topbar.
		w.tarot.RenderTopbar(c, sidebarWide)

		// 2. Active Room Stage.
		switch room {
		case tarot.RoomTable:
			w.tarot.RenderTableRoom(c)
		case tarot.RoomDaily:
			w.tarot.RenderDailyRoom(c)
		case tarot.RoomDeck:
			w.tarot.RenderDeckRoom(c)
		default:
			// Writing Room ("家 · 光标不动"): Tarot Deck Sidebar + Parchment Writing Sheet + Tarot AI Panel.
			body := ui.Row(c.Key("workspace-body")).Grow(1).MinHeight(0).FillWidth()
			body.Children(func() {
				if !layout.SidebarDrawer {
					blockui.DockPanel(c, "docked-sidebar-slot", layout.SidebarDocked, theme.TarotSidebarWidth, false, func() {
						w.tarot.RenderWritingSidebar(c, activeNB)
					})
				}

				aiEligible := aiEnabled && w.ai.IsOpen() && activeSurf == nil && activeSess != nil && activeSess.IsMarkdownNote()
				if !aiEligible {
					w.syncAIEditorSelectionHidden(activeTab)
				}

				w.tarot.RenderWritingStage(c, effMainW, func(sheetW float32) {
					if activeTab != "" && isExternalFile(activeTab, activeNB) {
						renderExternalPathBar(c, activeTab, &w.extPathBar, w.openDirInShell, w.revealInShell)
					}
					if toast != nil && toast.Active && toast.NotePath == activeTab {
						toast.RenderBanner(
							c,
							func() {
								w.bgSaveMu.Lock()
								defer w.bgSaveMu.Unlock()
								if sess := w.SessionFor(toast.NotePath); sess != nil && sess.Dirty() {
									w.setNotice(theme.Tr("笔记已有新编辑，无法撤回恢复", "The note has newer edits"))
									return
								}
								if err := files.CheckTextVersion(toast.NotePath, toast.RestoredFingerprint); err != nil {
									w.setNotice(err.Error())
									return
								}
								if err := w.writeNoteLocked(toast.NotePath, toast.UndoContent); err != nil {
									w.setNotice(err.Error())
									return
								}
								w.mu.Lock()
								w.historyToast = nil
								w.mu.Unlock()
							},
							func() {
								w.mu.Lock()
								w.historyToast = nil
								w.mu.Unlock()
								w.RequestRedraw()
							},
						)
					}

					noteArea := ui.Column(c.Key("workspace-note-area")).Grow(1).MinHeight(0).FillWidth()
					noteArea.Children(func() {
						switch {
						case activeSurf != nil:
							if w.pluginUI != nil {
								w.pluginUI.setEditorFocus(activeTab, false)
							}
							activeSurf.Render(
								c,
								func(p string) {
									if mygo.App.IsReady() {
										mygo.Shell.ShowItemInFolder(p)
									}
								},
								func(p string) {
									if mygo.App.IsReady() {
										_ = mygo.Shell.OpenPath(p)
									}
								},
							)
						case activeSess != nil:
							activeSess.SetEdgeFloatInsets(edgeSafeL, edgeSafeR)
							activeSess.SetOutlineForceNarrow(true)
							activeSess.SetBackground(w.pluginEditorBackground())
							activeSess.Render(c, sheetW, false)
							if w.pluginUI != nil {
								w.pluginUI.setEditorFocus(activeTab, activeSess.HasFocus(c))
							}
							if aiEligible {
								w.syncAIEditorSelection(c, activeSess)
							}
						default:
							if w.pluginUI != nil {
								w.pluginUI.setEditorFocus("", false)
							}
							allowGhostFocus := !w.modals.HasActiveModal() && !w.hasActivePluginModal() && !layout.SidebarDrawer
							w.renderEmptyWorkspaceMascot(c, activeNB, len(entries) == 0, allowGhostFocus)
						}
						if w.pluginUI != nil {
							w.pluginUI.renderToast(c)
						}
					})
				})

				aiOpen := w.ai.IsOpen()
				if aiOpen && w.pluginUI != nil {
					w.pluginUI.closePanelLocal("", "")
				}
				blockui.DockPanel(c, "docked-ai-slot", layout.ChatDocked && aiOpen, layout.ChatWidth, true, func() {
					theme.WithPalette(midPal, func() {
						chatW := w.ai.PanelWidth()
						maxChat := ChatLimit(vpW-cardHorizMargin, theme.TarotSidebarWidth, w.state.SidebarCollapsed, ChatMinW, ChatMaxW)
						if blockui.VerticalResizeHandle(c, "ai-panel-resize", &chatW, ChatMinW, maxChat, true) {
							w.ai.SyncPanelWidth(chatW)
							c.Invalidate()
						}
						w.ai.SetRenderWidth(layout.ChatWidth)
						w.ai.RenderPanel(c)
					})
				})

				if layout.SidebarDrawer {
					scrim := ui.Box(c.Key("sidebar-drawer-scrim")).Absolute().Left(0).Top(0).Right(0).Bottom(0).
						Background(midPal.Scrim())
					if scrim.Clicked() {
						w.ToggleSidebar(vpW)
					}
					drawer := ui.Row(c.Key("sidebar-drawer-panel")).Absolute().Left(0).Top(0).Bottom(0).
						Width(theme.TarotSidebarWidth).Clip().Transition(blockui.FloatingDrawerTransition())
					blockui.Elevate(drawer, theme.ElevationDrawer)
					drawer.Children(func() {
						w.tarot.RenderWritingSidebar(c, activeNB)
					})
				}
			})
		}

		// 3. Modals, Command Palette & Plugin Overlays in Midnight palette.
		theme.WithPalette(midPal, func() {
			w.modals.RenderModals(c)
			w.renderPluginOverlays(c)
		})
		blockui.RenderSmokeHUD(c)
	})
}
