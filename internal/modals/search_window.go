package modals

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"block/internal/files"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// SearchWindowController manages the standalone 680x460 floating search panel
// summoned by the global hotkey when the main window is visible but inactive.
type SearchWindowController struct {
	host model.WorkspaceHost

	GetState  func() model.AppState
	SearchFn  func(query string, allNotebooks bool) ([]model.SearchHit, error)
	OnOpenHit func(hit model.SearchHit)

	mu          sync.Mutex
	win         *mygo.Window
	visible     bool
	shownAt     time.Time
	query       string
	lastQuery   string
	hits        []model.SearchHit
	selected    int
	errMsg      string
	inputHandle ui.Handle
	st          searchPaletteState
}

// NewSearchWindowController creates a standalone floating SearchWindowController.
func NewSearchWindowController(host model.WorkspaceHost) *SearchWindowController {
	return &SearchWindowController{host: host}
}

func (s *SearchWindowController) currentState() model.AppState {
	if s.GetState != nil {
		return s.GetState()
	}
	return model.DefaultAppState(theme.Language())
}

func (s *SearchWindowController) notebookFolders() []string {
	return collectNotebookFolders(s.currentState(), s.host)
}

func (s *SearchWindowController) activeNotebookPath() string {
	return activeNotebookPath(s.currentState(), s.host)
}

func (s *SearchWindowController) scanOptions() files.ScanOptions {
	opts := scanOptionsFromAppState(s.currentState())
	opts.IgnoredDirs = searchIgnoredDirs(s.host)
	return opts
}

// AttachDesktop registers the hidden standalone 680x460 floating search panel window.
func (s *SearchWindowController) AttachDesktop() {
	s.mu.Lock()
	already := s.win != nil
	s.mu.Unlock()
	if already {
		return
	}

	create := func() {
		s.mu.Lock()
		if s.win != nil {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		x, y := centeredSearchWindowPos(int(searchWindowWidth), int(searchWindowHeight))
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:             theme.Tr("搜索", "Search"),
			X:                 x,
			Y:                 y,
			Width:             int(searchWindowWidth),
			Height:            int(searchWindowHeight),
			Hidden:            true,
			Panel:             true,
			Frameless:         true,
			CornerRadius:      int(searchModalRadius),
			DisableResize:     true,
			DisableMinimize:   true,
			DisableMaximize:   true,
			DisableFullScreen: true,
			BackgroundColor:   "light-dark(#ffffff, #222222)",
			Content:           ui.View(s.RenderView),
			Page:              mygo.PageOptions{DevTools: mygo.DevToolsDisabled},
		})
		win.OnClose(func(e *mygo.CloseEvent) {
			if IsAppQuitting() {
				return
			}
			e.PreventDefault()
			s.mu.Lock()
			scopeOpen := s.st.scopeMenuOpen
			if scopeOpen {
				s.st.scopeMenuOpen = false
			}
			s.mu.Unlock()
			if scopeOpen {
				win.Invalidate()
				return
			}
			s.Hide()
		})
		win.OnBlur(func() {
			s.mu.Lock()
			vis := s.visible
			recent := !s.shownAt.IsZero() && time.Since(s.shownAt) < 150*time.Millisecond
			s.mu.Unlock()
			if vis && !recent {
				s.Hide()
			}
		})

		s.mu.Lock()
		s.win = win
		vis := s.visible
		s.mu.Unlock()
		if vis {
			win.Show()
			win.Focus()
			s.inputHandle.Focus()
		}
	}

	if mygo.App.IsReady() {
		create()
	} else {
		mygo.App.WhenReady(create)
	}
}

func centeredSearchWindowPos(w, h int) (int, int) {
	if !mygo.App.IsReady() {
		return 0, 0
	}
	pt := mygo.Screen.CursorScreenPoint()
	d := mygo.Screen.DisplayNearestPoint(pt)
	area := d.WorkArea
	if area.Width <= 0 || area.Height <= 0 {
		d = mygo.Screen.PrimaryDisplay()
		area = d.WorkArea
	}
	if area.Width <= 0 || area.Height <= 0 {
		return 0, 0
	}
	return area.X + max((area.Width-w)/2, 0), area.Y + max((area.Height-h)/2, 0)
}

// IsVisible reports whether the standalone search window is currently shown.
func (s *SearchWindowController) IsVisible() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.visible
}

// IsFocused reports whether the standalone search window currently has keyboard focus.
func (s *SearchWindowController) IsFocused() bool {
	s.mu.Lock()
	win := s.win
	vis := s.visible
	s.mu.Unlock()
	return vis && win != nil && win.IsFocused()
}

// Open resets and shows the standalone 680x460 floating search window centered on the active display.
func (s *SearchWindowController) Open() {
	RecordSearchTriggered()
	s.AttachDesktop()

	folders := s.notebookFolders()
	s.mu.Lock()
	s.query = ""
	s.lastQuery = ""
	s.hits = nil
	s.selected = 0
	s.errMsg = ""
	s.st.folders = folders
	s.st.scopeFolder = ""
	s.st.scopeMenuOpen = false
	s.st.scopeCursor = 0
	s.st.revealScopeCursor = false
	s.st.resultsScroll.Y = 0
	s.st.hitMeta = nil
	s.st.searchGen.Add(1)
	s.st.cancelSearch()
	s.st.clearAsync()
	s.visible = true
	s.shownAt = time.Now()
	win := s.win
	s.mu.Unlock()

	if win != nil {
		x, y := centeredSearchWindowPos(int(searchWindowWidth), int(searchWindowHeight))
		if x != 0 || y != 0 {
			win.SetBounds(mygo.Rectangle{
				X:      x,
				Y:      y,
				Width:  int(searchWindowWidth),
				Height: int(searchWindowHeight),
			})
		}
		win.Show()
		win.Focus()
		s.inputHandle.Focus()
		win.Invalidate()
	}
}

// Hide hides the standalone floating search window.
func (s *SearchWindowController) Hide() {
	s.mu.Lock()
	if !s.visible && !s.st.scopeMenuOpen {
		s.mu.Unlock()
		return
	}
	s.visible = false
	s.st.scopeMenuOpen = false
	s.st.searchGen.Add(1)
	s.st.cancelSearch()
	s.st.clearAsync()
	win := s.win
	s.mu.Unlock()

	if win != nil {
		win.Hide()
	}
}

// Toggle toggles the standalone floating search window.
func (s *SearchWindowController) Toggle() {
	s.mu.Lock()
	vis := s.visible
	win := s.win
	recent := !s.shownAt.IsZero() && time.Since(s.shownAt) < 150*time.Millisecond
	s.mu.Unlock()

	if vis && (recent || (win != nil && win.IsFocused())) {
		s.Hide()
		return
	}
	s.Open()
}

// Invalidate requests a redraw if the standalone search window exists and is visible.
func (s *SearchWindowController) Invalidate() {
	s.mu.Lock()
	win := s.win
	vis := s.visible
	s.mu.Unlock()
	if vis && win != nil {
		win.Invalidate()
	}
}

func (s *SearchWindowController) runSearch() {
	s.runSearchWithDelay(0)
}

func (s *SearchWindowController) runSearchDebounced() {
	s.runSearchWithDelay(searchInputDebounce)
}

func (s *SearchWindowController) runSearchWithDelay(delay time.Duration) {
	s.lastQuery = s.query
	folders := s.notebookFolders()
	s.st.syncFolders(folders)

	gen := s.st.searchGen.Add(1)
	s.st.cancelSearch()
	s.st.clearAsync()

	if strings.TrimSpace(s.query) == "" {
		s.st.appliedGen = gen
		s.st.appliedQuery = ""
		s.errMsg = ""
		s.hits = nil
		s.st.hitMeta = nil
		s.selected = 0
		s.st.resultsScroll.Y = 0
		return
	}

	if testing.Testing() || internalui.IsProbeEnabled() || s.SearchFn != nil {
		s.runSearchSync(gen, folders)
		return
	}

	scopeFolder := s.st.scopeFolder
	roots := append([]string(nil), s.st.effectiveRoots()...)
	foldersSnap := append([]string(nil), s.st.folders...)
	query := s.query
	activeNB := s.activeNotebookPath()
	scanOpts := s.scanOptions()
	host := s.host

	s.st.scheduleAsyncSearch(gen, query, delay, func(ctx context.Context) ([]model.SearchHit, []searchHitMeta, string) {
		return executeSearchWithSnapshot(
			ctx,
			scopeFolder,
			foldersSnap,
			roots,
			query,
			activeNB,
			scanOpts,
			nil,
			host,
		)
	}, s.Invalidate)
}

func (s *SearchWindowController) runSearchSync(gen uint64, folders []string) {
	hits, meta, errMsg := executeSearchQuery(
		&s.st,
		s.query,
		folders,
		s.activeNotebookPath(),
		s.scanOptions(),
		s.SearchFn,
		s.host,
	)
	s.st.appliedGen = gen
	s.st.appliedQuery = strings.TrimSpace(s.query)
	s.errMsg = errMsg
	s.hits = hits
	s.st.hitMeta = meta
	s.selected = 0
	s.st.resultsScroll.Y = 0
}

func (s *SearchWindowController) ensureFreshHits() {
	s.st.consumeAsyncResult(&s.hits, &s.selected, &s.errMsg)
	if s.st.isResultFresh(s.query) {
		return
	}
	s.lastQuery = s.query
	folders := s.notebookFolders()
	s.st.syncFolders(folders)
	gen := s.st.searchGen.Add(1)
	s.st.cancelSearch()
	s.st.clearAsync()
	if strings.TrimSpace(s.query) == "" {
		s.st.appliedGen = gen
		s.st.appliedQuery = ""
		s.errMsg = ""
		s.hits = nil
		s.st.hitMeta = nil
		s.selected = 0
		s.st.resultsScroll.Y = 0
		return
	}
	s.runSearchSync(gen, folders)
}

func (s *SearchWindowController) openHit(idx int) {
	if idx < 0 || idx >= len(s.hits) {
		return
	}
	hit := s.hits[idx]
	onOpen := s.OnOpenHit
	host := s.host

	s.Hide()
	if onOpen != nil {
		onOpen(hit)
		return
	}
	if host != nil {
		_ = host.OpenNote(hit.Path, hit.Line)
	}
}

// RenderView renders the standalone 680x460 floating search window.
func (s *SearchWindowController) RenderView(c *ui.Context) {
	pal := theme.Ocean()
	uiTheme := pal.ToUITheme("")
	uiTheme.ScrollbarStyle = internalui.StandardScrollbarStyle(pal)
	c.SetTheme(&uiTheme)

	s.st.consumeAsyncResult(&s.hits, &s.selected, &s.errMsg)
	ignoredDirsChanged := s.st.syncIgnoredDirs(searchIgnoredDirs(s.host))
	if ignoredDirsChanged {
		s.hits = nil
		s.st.hitMeta = nil
	}
	if (s.st.syncFolders(s.notebookFolders()) || ignoredDirsChanged) && strings.TrimSpace(s.query) != "" {
		s.runSearch()
	}
	wasScopeOpen := s.st.scopeMenuOpen

	root := ui.Column(c.Key("search-window")).Fill().
		Radius(searchModalRadius).Background(pal.Menu).
		Border(1, pal.MenuBorder).Clip().
		TextColor(pal.Text).
		Role(ui.RoleDialog).Label(theme.Tr("搜索笔记", "Search Notes"))
	internalui.Probe(root, "search-window")

	var scopeTrigger ui.Element
	activeNotebook := s.activeNotebookPath()
	root.Children(func() {
		scopeTrigger = renderSearchHeaderView(
			c, pal, &s.st, activeNotebook, &s.query, &s.lastQuery, &s.inputHandle,
			searchHeaderCallbacks{
				onRunSearch:    s.runSearch,
				onInputChanged: s.runSearchDebounced,
				onMoveSelection: func(delta int) {
					stepHitSelection(&s.selected, len(s.hits), &s.st, delta)
				},
				onAcceptScope: func() {
					if s.st.acceptScopeCursor() {
						s.runSearch()
					}
					s.inputHandle.Focus()
				},
				onOpenSelected: func() {
					s.ensureFreshHits()
					s.openHit(s.selected)
				},
				onToggleScope: func() {
					if s.st.toggleScope(activeNotebook) {
						s.runSearch()
					}
				},
				onResetScopeAll: func() {
					if s.st.resetScopeAll() {
						s.runSearch()
					}
				},
				onClose: s.Hide,
			},
		)
		renderSearchBodyView(
			c, pal, &s.st, s.query, s.hits, s.selected, s.errMsg,
			true, 0, s.SearchFn != nil,
			func(idx int) {
				if !s.st.isResultFresh(s.query) {
					s.ensureFreshHits()
					return
				}
				s.selected = idx
				s.openHit(idx)
			},
		)
		renderSearchFooter(c, pal, true)
	})

	if scopeTrigger.Valid() {
		renderScopeMenuPopoverView(c, pal, &s.st, scopeTrigger, &s.inputHandle, func(idx int) {
			if s.st.selectScopeIndex(idx) {
				s.runSearch()
			}
		})
	}

	// Fallback shortcuts when the search input does not hold keyboard focus.
	if root.Shortcut(0, ui.KeyDown) {
		if !s.st.stepScopeMenu(true) {
			stepHitSelection(&s.selected, len(s.hits), &s.st, 1)
		}
	}
	if root.Shortcut(0, ui.KeyUp) {
		if !s.st.stepScopeMenu(false) {
			stepHitSelection(&s.selected, len(s.hits), &s.st, -1)
		}
	}
	if root.Shortcut(0, ui.KeyEnter) {
		if s.st.scopeMenuOpen {
			if s.st.acceptScopeCursor() {
				s.runSearch()
			}
			s.inputHandle.Focus()
		} else {
			s.openHit(s.selected)
		}
	}
	if root.Shortcut(0, ui.KeyTab) {
		if s.st.toggleScope(activeNotebook) {
			s.runSearch()
		}
	}
	if root.Shortcut(ui.Shift, ui.KeyTab) {
		if s.st.toggleScope(activeNotebook) {
			s.runSearch()
		}
	}
	if root.Shortcut(ui.Cmd, ui.Key0) {
		if s.st.resetScopeAll() {
			s.runSearch()
		}
	}
	if !theme.IsMacOS() && c.Shortcut(ui.Ctrl, ui.KeyQ) {
		if mygo.App.IsReady() {
			mygo.App.Quit()
		}
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyW) || (!theme.IsMacOS() && c.Shortcut(ui.Ctrl, ui.KeyF4)) {
		s.Hide()
	}
	if mods, key, ok := internalui.ParseShortcut(EffectiveShortcut(s.currentState(), "global_summon")); ok {
		if root.Shortcut(mods, key) && MarkSearchTriggered() {
			s.Hide()
		}
	}
	if !wasScopeOpen && root.Shortcut(0, ui.KeyEscape) {
		s.Hide()
	}
}
