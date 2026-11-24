package modals

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	searchDialogWidth   float32 = 640
	searchWindowWidth   float32 = 680
	searchWindowHeight  float32 = 460
	searchMinBodyHeight float32 = 96
	searchEmptyHeight   float32 = 280
	searchMaxBodyHeight float32 = 460
	searchModalRadius   float32 = 12
	searchHeaderHeight  float32 = 52
	searchFooterHeight  float32 = 36
	searchScopeMenuW    float32 = 236
	searchScopeMenuH    float32 = 304
	searchMaxHits               = 50
	searchHotkeyWindow          = 250 * time.Millisecond
	searchInputDebounce         = 80 * time.Millisecond
)

var (
	searchTriggerStart = time.Now()
	lastSearchTrigger  atomic.Int64
)

func searchNowMS() int64 {
	return time.Since(searchTriggerStart).Milliseconds() + 1000
}

// RecordSearchTriggered updates the search trigger timestamp so an immediate
// duplicate OS hotkey echo can be suppressed.
func RecordSearchTriggered() {
	lastSearchTrigger.Store(searchNowMS())
}

// MarkSearchTriggered enforces the 250ms debounce window between global search triggers.
func MarkSearchTriggered() bool {
	now := searchNowMS()
	prev := lastSearchTrigger.Swap(now)
	return now-prev >= int64(searchHotkeyWindow/time.Millisecond)
}

type searchHitMeta struct {
	titleMatch   bool
	titleMarks   [][2]int
	snippetMarks [][2]int
	folderMarks  [][2]int
	count        int
	folder       string
}

type searchPaletteState struct {
	folders           []string
	scopeFolder       string // "" = All Folders
	scopeMenuOpen     bool
	scopeCursor       int
	revealScopeCursor bool
	scopeScroll       ui.ScrollState
	resultsScroll     ui.ScrollState
	revealSelected    bool
	hitMeta           []searchHitMeta

	searchGen     atomic.Uint64
	appliedGen    uint64
	appliedQuery  string
	asyncMu       sync.Mutex
	asyncPending  bool
	asyncGen      uint64
	asyncQuery    string
	asyncHits     []model.SearchHit
	asyncMeta     []searchHitMeta
	asyncErr      string
	cancelFn      context.CancelFunc
	debounceTimer *time.Timer
}

func (st *searchPaletteState) cancelSearch() {
	st.asyncMu.Lock()
	if st.debounceTimer != nil {
		st.debounceTimer.Stop()
		st.debounceTimer = nil
	}
	cancel := st.cancelFn
	st.cancelFn = nil
	st.asyncMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (st *searchPaletteState) scheduleAsyncSearch(
	gen uint64,
	query string,
	delay time.Duration,
	runFn func(context.Context) ([]model.SearchHit, []searchHitMeta, string),
	onDone func(),
) {
	ctx, cancel := context.WithCancel(context.Background())
	normQ := strings.TrimSpace(query)

	st.asyncMu.Lock()
	if st.debounceTimer != nil {
		st.debounceTimer.Stop()
		st.debounceTimer = nil
	}
	prevCancel := st.cancelFn
	st.cancelFn = cancel
	st.asyncPending = false
	st.asyncQuery = ""
	st.asyncHits = nil
	st.asyncMeta = nil
	st.asyncErr = ""

	execFn := func() {
		defer cancel()
		if ctx.Err() != nil || st.searchGen.Load() != gen {
			return
		}
		hits, meta, errMsg := runFn(ctx)
		if ctx.Err() != nil {
			return
		}
		if st.storeAsyncResult(gen, normQ, hits, meta, errMsg) && onDone != nil {
			onDone()
		}
	}

	if delay <= 0 {
		st.asyncMu.Unlock()
		if prevCancel != nil {
			prevCancel()
		}
		go execFn()
		return
	}
	st.debounceTimer = time.AfterFunc(delay, execFn)
	st.asyncMu.Unlock()
	if prevCancel != nil {
		prevCancel()
	}
}

func (st *searchPaletteState) clearAsync() {
	st.asyncMu.Lock()
	st.asyncPending = false
	st.asyncQuery = ""
	st.asyncHits = nil
	st.asyncMeta = nil
	st.asyncErr = ""
	st.asyncMu.Unlock()
}

func (st *searchPaletteState) storeAsyncResult(gen uint64, query string, hits []model.SearchHit, meta []searchHitMeta, errMsg string) bool {
	if st.searchGen.Load() != gen {
		return false
	}
	st.asyncMu.Lock()
	defer st.asyncMu.Unlock()
	if st.searchGen.Load() != gen {
		return false
	}
	st.asyncPending = true
	st.asyncGen = gen
	st.asyncQuery = query
	st.asyncHits = hits
	st.asyncMeta = meta
	st.asyncErr = errMsg
	return true
}

func (st *searchPaletteState) consumeAsyncResult(hits *[]model.SearchHit, selected *int, errMsg *string) {
	st.asyncMu.Lock()
	if !st.asyncPending || st.asyncGen != st.searchGen.Load() {
		st.asyncMu.Unlock()
		return
	}
	resGen := st.asyncGen
	resQuery := st.asyncQuery
	resHits := st.asyncHits
	resMeta := st.asyncMeta
	resErr := st.asyncErr
	st.asyncPending = false
	st.asyncQuery = ""
	st.asyncHits = nil
	st.asyncMeta = nil
	st.asyncErr = ""
	st.asyncMu.Unlock()

	st.appliedGen = resGen
	st.appliedQuery = resQuery
	*hits = resHits
	st.hitMeta = resMeta
	*errMsg = resErr
	*selected = 0
	st.resultsScroll.Y = 0
}

func (st *searchPaletteState) isResultFresh(query string) bool {
	return st.appliedGen == st.searchGen.Load() && st.appliedQuery == strings.TrimSpace(query)
}

// SetOpenFromSearchFn sets the workspace action for opening a selected hit.
func (m *Manager) SetOpenFromSearchFn(fn func(model.SearchHit)) { m.onOpenSearchHit = fn }

func (m *Manager) searchState() *searchPaletteState { return &m.searchPalette }

// OpenSearch opens the global search palette in the main workspace.
func (m *Manager) OpenSearch(allNotebooks bool) {
	RecordSearchTriggered()
	if m.settingsOpen && !m.IsRecordingShortcut() {
		m.CloseSettings()
	}

	st := m.searchState()
	folders := m.notebookFolders()
	if m.searchOpen {
		before := st.scopeFolder
		st.syncFolders(folders)
		if allNotebooks {
			st.resetScopeAll()
		} else if st.scopeFolder != "" && !containsCleanPath(folders, st.scopeFolder) {
			st.scopeFolder = ""
		}
		m.searchAllNB = st.scopeFolder == ""
		if strings.TrimSpace(m.searchQuery) != "" && !cleanPathEqual(before, st.scopeFolder) {
			m.runSearch()
		}
		m.searchInputHandle.Focus()
		m.requestRedraw()
		return
	}

	st.folders = folders
	active := m.searchActiveNotebookPath()
	if allNotebooks || active == "" || !containsCleanPath(folders, active) {
		st.scopeFolder = ""
	} else {
		st.scopeFolder = findCleanPath(folders, active)
	}
	m.searchAllNB = st.scopeFolder == ""
	st.scopeMenuOpen = false
	st.scopeCursor = 0
	st.revealScopeCursor = false
	st.resultsScroll.Y = 0

	m.searchOpen = true
	m.searchSelected = 0
	m.searchErr = ""
	if strings.TrimSpace(m.searchQuery) != "" {
		m.runSearch()
	} else {
		gen := st.searchGen.Add(1)
		st.appliedGen = gen
		st.appliedQuery = ""
		st.clearAsync()
		m.searchHits = nil
		st.hitMeta = nil
	}
	m.searchInputHandle.Focus()
	m.requestRedraw()
}

// CloseSearch closes the global search palette.
func (m *Manager) CloseSearch() {
	if !m.searchOpen {
		return
	}
	st := m.searchState()
	st.scopeMenuOpen = false
	st.searchGen.Add(1)
	st.cancelSearch()
	st.clearAsync()
	m.searchOpen = false
	m.requestRedraw()
}

// IsSearchOpen reports whether the global search modal is open.
func (m *Manager) IsSearchOpen() bool {
	return m.searchOpen
}

// ResetSearchScopeAll resets the workspace search scope to All Folders (`Cmd+0`).
func (m *Manager) ResetSearchScopeAll() {
	if !m.searchOpen {
		return
	}
	if m.resetScopeAll() {
		m.runSearch()
	}
	m.searchInputHandle.Focus()
	m.requestRedraw()
}

func (m *Manager) searchActiveNotebookPath() string {
	return activeNotebookPath(m.currentState(), m.host)
}

func activeNotebookPath(st model.AppState, host model.WorkspaceHost) string {
	if strings.TrimSpace(st.Folder) != "" {
		return filepath.Clean(st.Folder)
	}
	if host != nil && strings.TrimSpace(host.ActiveNotebook()) != "" {
		return filepath.Clean(host.ActiveNotebook())
	}
	return ""
}

func (m *Manager) notebookFolders() []string {
	return collectNotebookFolders(m.currentState(), m.host)
}

func collectNotebookFolders(appState model.AppState, host model.WorkspaceHost) []string {
	var raw []string
	if strings.TrimSpace(appState.Folder) != "" {
		raw = append(raw, appState.Folder)
	} else if host != nil && strings.TrimSpace(host.ActiveNotebook()) != "" {
		raw = append(raw, host.ActiveNotebook())
	}
	if len(appState.Notebooks) > 0 {
		raw = append(raw, appState.Notebooks...)
	} else if host != nil {
		raw = append(raw, host.Notebooks()...)
	}

	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		if seen[clean] {
			continue
		}
		seen[clean] = true
		if theme.IsSearchFolderIgnored(clean) {
			continue
		}
		out = append(out, clean)
	}
	return out
}

func (m *Manager) searchScanOptions() files.ScanOptions {
	return scanOptionsFromAppState(m.currentState())
}

func scanOptionsFromAppState(st model.AppState) files.ScanOptions {
	hideNonMD := st.HideNonMarkdownFiles == nil || *st.HideNonMarkdownFiles
	hideAssets := st.HideAssetsFolder == nil || *st.HideAssetsFolder
	assets := strings.TrimSpace(st.AssetsFolder)
	if assets == "" {
		assets = "assets"
	}
	return files.ScanOptions{
		HideNonMarkdown: hideNonMD,
		HideAssets:      hideAssets,
		AssetsFolder:    assets,
	}
}

func (st *searchPaletteState) syncFolders(folders []string) bool {
	scopeLost := st.scopeFolder != "" && !containsCleanPath(folders, st.scopeFolder)
	changed := scopeLost || !slicesEqual(st.folders, folders)
	if !changed {
		return false
	}
	st.folders = folders
	if scopeLost {
		st.scopeFolder = ""
	}
	return true
}

func (st *searchPaletteState) scopeLabel() string {
	if st.scopeFolder == "" {
		return theme.Tr("全部", "All")
	}
	return searchFolderName(st.scopeFolder)
}

func (st *searchPaletteState) currentNotebookScope(active string) string {
	if active != "" && containsCleanPath(st.folders, active) {
		return findCleanPath(st.folders, active)
	}
	return ""
}

type searchPlaceholderSpec struct {
	prefix string
	keycap string
	suffix string
}

func (st *searchPaletteState) searchPlaceholderParts(active string) searchPlaceholderSpec {
	if st.scopeFolder != "" {
		return searchPlaceholderSpec{
			prefix: theme.Tr("搜索标题与正文，按", "Search titles & content,"),
			keycap: "Tab",
			suffix: theme.Tr("搜全部", "for all"),
		}
	}
	if st.currentNotebookScope(active) != "" {
		return searchPlaceholderSpec{
			prefix: theme.Tr("搜索全部，按", "Search all,"),
			keycap: "Tab",
			suffix: theme.Tr("切回当前", "for current"),
		}
	}
	return searchPlaceholderSpec{
		prefix: theme.Tr("搜索标题与正文", "Search titles and content"),
	}
}

func (st *searchPaletteState) effectiveRoots() []string {
	if st.scopeFolder == "" {
		return st.folders
	}
	return []string{st.scopeFolder}
}

func (st *searchPaletteState) toggleScopeMenu() {
	st.scopeMenuOpen = !st.scopeMenuOpen
	if st.scopeMenuOpen {
		st.scopeCursor = -1
		st.revealScopeCursor = false
		st.scopeScroll = ui.ScrollState{}
	}
}

func (st *searchPaletteState) stepScopeMenu(down bool) bool {
	if !st.scopeMenuOpen {
		return false
	}
	count := 1 + len(st.folders)
	if count == 0 {
		return true
	}
	cur := st.scopeCursor
	if cur < 0 || cur >= count {
		if down {
			st.scopeCursor = 0
		} else {
			st.scopeCursor = count - 1
		}
		st.revealScopeCursor = true
		return true
	}
	step := -1
	if down {
		step = 1
	}
	st.scopeCursor = (cur + step + count) % count
	st.revealScopeCursor = true
	return true
}

func (st *searchPaletteState) selectScopeIndex(idx int) bool {
	var next string
	if idx > 0 && idx-1 < len(st.folders) {
		next = st.folders[idx-1]
	} else if idx != 0 {
		return false
	}
	changed := !cleanPathEqual(st.scopeFolder, next)
	st.scopeFolder = next
	st.scopeMenuOpen = false
	return changed
}

func (st *searchPaletteState) acceptScopeCursor() bool {
	if !st.scopeMenuOpen || st.scopeCursor < 0 {
		return false
	}
	idx := st.scopeCursor
	if idx > len(st.folders) {
		idx = len(st.folders)
	}
	return st.selectScopeIndex(idx)
}

func (st *searchPaletteState) toggleScope(active string) bool {
	st.scopeMenuOpen = false
	current := st.currentNotebookScope(active)
	var next string
	if st.scopeFolder == "" {
		if current == "" {
			return false
		}
		next = current
	} else {
		next = ""
	}
	changed := !cleanPathEqual(st.scopeFolder, next)
	st.scopeFolder = next
	return changed
}

func (st *searchPaletteState) resetScopeAll() bool {
	st.scopeMenuOpen = false
	changed := st.scopeFolder != ""
	st.scopeFolder = ""
	return changed
}

func (m *Manager) selectScopeIndex(idx int) bool {
	st := m.searchState()
	changed := st.selectScopeIndex(idx)
	m.searchAllNB = st.scopeFolder == ""
	return changed
}

func (m *Manager) acceptScopeCursor() bool {
	st := m.searchState()
	changed := st.acceptScopeCursor()
	m.searchAllNB = st.scopeFolder == ""
	return changed
}

func (m *Manager) toggleScope() bool {
	st := m.searchState()
	changed := st.toggleScope(m.searchActiveNotebookPath())
	m.searchAllNB = st.scopeFolder == ""
	return changed
}

func (m *Manager) resetScopeAll() bool {
	st := m.searchState()
	changed := st.resetScopeAll()
	m.searchAllNB = true
	return changed
}

func executeSearchQuery(
	st *searchPaletteState,
	query string,
	folders []string,
	activeNB string,
	scanOpts files.ScanOptions,
	searchFn func(query string, allNotebooks bool) ([]model.SearchHit, error),
	host model.WorkspaceHost,
) ([]model.SearchHit, []searchHitMeta, string) {
	st.syncFolders(folders)
	return executeSearchWithSnapshot(
		context.Background(),
		st.scopeFolder,
		st.folders,
		st.effectiveRoots(),
		query,
		activeNB,
		scanOpts,
		searchFn,
		host,
	)
}

func executeSearchWithSnapshot(
	ctx context.Context,
	scopeFolder string,
	folders []string,
	roots []string,
	query string,
	activeNB string,
	scanOpts files.ScanOptions,
	searchFn func(query string, allNotebooks bool) ([]model.SearchHit, error),
	host model.WorkspaceHost,
) ([]model.SearchHit, []searchHitMeta, string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, nil, ""
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil, ""
	}

	if len(roots) == 0 && searchFn == nil && host == nil {
		return nil, nil, ""
	}

	var hits []model.SearchHit
	var err error
	switch {
	case scopeFolder == "":
		if searchFn != nil {
			hits, err = searchFn(q, true)
		} else if len(roots) > 0 {
			hits, err = files.SearchNotebooksContext(ctx, roots, q, scanOpts)
		} else if host != nil {
			hits, err = host.SearchWorkspace(q, true)
		}
	case activeNB != "" && cleanPathEqual(scopeFolder, activeNB):
		if searchFn != nil {
			hits, err = searchFn(q, false)
		} else if len(roots) > 0 {
			hits, err = files.SearchNotebooksContext(ctx, roots, q, scanOpts)
		} else if host != nil {
			hits, err = host.SearchWorkspace(q, false)
		}
	default:
		hits, err = files.SearchNotebooksContext(ctx, []string{scopeFolder}, q, scanOpts)
	}

	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return nil, nil, ""
		}
		files.LogWarnf("workspace search failed: err=%v", err)
		return nil, nil, err.Error()
	}
	if ctx.Err() != nil {
		return nil, nil, ""
	}

	// Partition into Title Matches then Content Matches while preserving order within each group.
	type pairedHit struct {
		hit  model.SearchHit
		meta searchHitMeta
	}
	includeRootName := len(folders) > 1
	if len(roots) > 1 {
		includeRootName = true
	}
	paired := make([]pairedHit, 0, len(hits))
	for _, h := range hits {
		isTitleMatch := h.TitleMatch || h.Line == 0
		var titleMarks [][2]int
		if isTitleMatch {
			titleMarks = files.FindTitleHighlights(h.Title, q)
		}
		snippetMarks := files.FindBodyHighlights(h.Snippet, q)
		if len(snippetMarks) == 0 && h.MatchEnd > h.MatchStart && h.MatchEnd <= len(h.Snippet) {
			snippetMarks = [][2]int{{h.MatchStart, h.MatchEnd}}
		}
		cnt := len(snippetMarks)
		if cnt < 1 {
			cnt = 1
		}
		folder := searchFolderLabel(roots, h.Path, h.Title, h.Notebook, includeRootName)
		var folderMarks [][2]int
		if isTitleMatch && folder != "" {
			if len(titleMarks) == 0 {
				folderMarks = files.FindTitleHighlights(folder, q)
			} else if strings.Contains(q, "/") || len(strings.Fields(q)) > 1 {
				// Highlight matching folder segment when query targets "folder/note" or multi-token path.
				dirPart := folder
				if h.Title != "" && strings.HasSuffix(folder, " / "+h.Title) {
					dirPart = strings.TrimSuffix(folder, " / "+h.Title)
				}
				if dirPart != "" {
					folderMarks = files.FindTitleHighlights(dirPart, q)
				}
			}
		}
		paired = append(paired, pairedHit{
			hit: h,
			meta: searchHitMeta{
				titleMatch:   isTitleMatch,
				titleMarks:   titleMarks,
				snippetMarks: snippetMarks,
				folderMarks:  folderMarks,
				count:        cnt,
				folder:       folder,
			},
		})
	}
	sort.SliceStable(paired, func(i, j int) bool {
		return paired[i].meta.titleMatch && !paired[j].meta.titleMatch
	})
	if len(paired) > searchMaxHits {
		paired = paired[:searchMaxHits]
	}

	orderedHits := make([]model.SearchHit, len(paired))
	orderedMeta := make([]searchHitMeta, len(paired))
	for i, p := range paired {
		orderedHits[i] = p.hit
		orderedMeta[i] = p.meta
	}
	return orderedHits, orderedMeta, ""
}

func (m *Manager) runSearch() {
	m.runSearchWithDelay(0)
}

func (m *Manager) runSearchDebounced() {
	m.runSearchWithDelay(searchInputDebounce)
}

func (m *Manager) runSearchWithDelay(delay time.Duration) {
	st := m.searchState()
	m.searchLastQuery = m.searchQuery
	folders := m.notebookFolders()
	st.syncFolders(folders)
	m.searchAllNB = st.scopeFolder == ""

	gen := st.searchGen.Add(1)
	st.cancelSearch()
	st.clearAsync()

	if strings.TrimSpace(m.searchQuery) == "" {
		st.appliedGen = gen
		st.appliedQuery = ""
		m.searchErr = ""
		m.searchHits = nil
		st.hitMeta = nil
		m.searchSelected = 0
		st.resultsScroll.Y = 0
		return
	}

	if testing.Testing() || internalui.IsProbeEnabled() || m.SearchFn != nil {
		m.runSearchSync(gen, folders)
		return
	}

	scopeFolder := st.scopeFolder
	roots := append([]string(nil), st.effectiveRoots()...)
	foldersSnap := append([]string(nil), st.folders...)
	query := m.searchQuery
	activeNB := m.searchActiveNotebookPath()
	scanOpts := m.searchScanOptions()
	host := m.host

	st.scheduleAsyncSearch(gen, query, delay, func(ctx context.Context) ([]model.SearchHit, []searchHitMeta, string) {
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
	}, m.requestRedraw)
}

func (m *Manager) runSearchSync(gen uint64, folders []string) {
	st := m.searchState()
	hits, meta, errMsg := executeSearchQuery(
		st,
		m.searchQuery,
		folders,
		m.searchActiveNotebookPath(),
		m.searchScanOptions(),
		m.SearchFn,
		m.host,
	)
	st.appliedGen = gen
	st.appliedQuery = strings.TrimSpace(m.searchQuery)
	m.searchAllNB = st.scopeFolder == ""
	m.searchErr = errMsg
	m.searchHits = hits
	st.hitMeta = meta
	m.searchSelected = 0
	st.resultsScroll.Y = 0
}

func (m *Manager) ensureFreshSearchHits() {
	st := m.searchState()
	st.consumeAsyncResult(&m.searchHits, &m.searchSelected, &m.searchErr)
	if st.isResultFresh(m.searchQuery) {
		return
	}
	m.searchLastQuery = m.searchQuery
	folders := m.notebookFolders()
	st.syncFolders(folders)
	gen := st.searchGen.Add(1)
	st.cancelSearch()
	st.clearAsync()
	if strings.TrimSpace(m.searchQuery) == "" {
		st.appliedGen = gen
		st.appliedQuery = ""
		m.searchErr = ""
		m.searchHits = nil
		st.hitMeta = nil
		m.searchSelected = 0
		st.resultsScroll.Y = 0
		return
	}
	m.runSearchSync(gen, folders)
}

func stepHitSelection(selected *int, total int, st *searchPaletteState, delta int) {
	if total <= 0 {
		return
	}
	next := *selected + delta
	if next < 0 {
		next = 0
	}
	if next >= total {
		next = total - 1
	}
	if next != *selected {
		*selected = next
		st.revealSelected = true
	}
}

func (m *Manager) moveSearchSelection(delta int) {
	stepHitSelection(&m.searchSelected, len(m.searchHits), m.searchState(), delta)
}

func (m *Manager) openSearchHit(idx int) {
	if idx < 0 || idx >= len(m.searchHits) {
		return
	}
	hit := m.searchHits[idx]
	m.CloseSearch()
	if fn := m.onOpenSearchHit; fn != nil {
		fn(hit)
		return
	}
	if m.host != nil {
		_ = m.host.OpenNote(hit.Path, hit.Line)
	}
}

func (m *Manager) renderSearchModal(c *ui.Context) {
	pal := theme.Ocean()
	st := m.searchState()
	st.consumeAsyncResult(&m.searchHits, &m.searchSelected, &m.searchErr)
	if st.syncFolders(m.notebookFolders()) && strings.TrimSpace(m.searchQuery) != "" {
		m.runSearch()
	}
	wasScopeOpen := st.scopeMenuOpen

	_, vpH := c.Size()
	topPad := float32(96)
	if vpH > 0 && vpH < 360 {
		topPad = max(float32(16), (vpH-220)*0.25)
	}
	maxBodyH := searchMaxBodyHeight
	if vpH > 0 {
		maxCardH := max(vpH-topPad-24, searchHeaderHeight+searchMinBodyHeight+searchFooterHeight)
		maxBodyH = min(searchMaxBodyHeight, max(maxCardH-searchHeaderHeight-searchFooterHeight, searchMinBodyHeight))
	}

	var scopeTrigger ui.Element
	var card ui.Element

	ui.Overlay(c, func() {
		backdrop := internalui.ModalLayer(c, "search-modal").Padding(topPad, 24, 24, 24)

		backdrop.Children(func() {
			card = ui.Column(c.Key("search-modal")).
				Width(searchDialogWidth).
				MaxWidthPercent(100).MaxHeightPercent(100).
				Radius(searchModalRadius).Background(pal.Menu).
				Border(1, pal.MenuBorder).Clip().
				Role(ui.RoleDialog).Label(theme.Tr("搜索笔记", "Search Notes")).
				Transition(internalui.ModalSpringTransition())
			internalui.Elevate(card, theme.ElevationDialog)
			internalui.Probe(card, "search-modal")

			card.Children(func() {
				scopeTrigger = renderSearchHeaderView(
					c, pal, st, m.searchActiveNotebookPath(), &m.searchQuery, &m.searchLastQuery, &m.searchInputHandle,
					searchHeaderCallbacks{
						onRunSearch:     m.runSearch,
						onInputChanged:  m.runSearchDebounced,
						onMoveSelection: m.moveSearchSelection,
						onAcceptScope: func() {
							if m.acceptScopeCursor() {
								m.runSearch()
							}
						},
						onOpenSelected: func() {
							m.ensureFreshSearchHits()
							m.openSearchHit(m.searchSelected)
						},
						onToggleScope: func() {
							if m.toggleScope() {
								m.runSearch()
							}
						},
						onResetScopeAll: func() {
							if m.resetScopeAll() {
								m.runSearch()
							}
						},
						onClose: m.CloseSearch,
					},
				)
				renderSearchBodyView(
					c, pal, st, m.searchQuery, m.searchHits, m.searchSelected, m.searchErr,
					false, maxBodyH, m.SearchFn != nil,
					func(idx int) {
						if !st.isResultFresh(m.searchQuery) {
							m.ensureFreshSearchHits()
							return
						}
						m.searchSelected = idx
						m.openSearchHit(idx)
					},
				)
				renderSearchFooter(c, pal, false)
			})
		})

		if scopeTrigger.Valid() {
			renderScopeMenuPopoverView(c, pal, st, scopeTrigger, &m.searchInputHandle, func(idx int) {
				if m.selectScopeIndex(idx) {
					m.runSearch()
				}
			})
		}

		// Fallback shortcuts when the search input does not hold keyboard focus.
		if backdrop.OverlayShortcut(0, ui.KeyDown) {
			if !st.stepScopeMenu(true) {
				m.moveSearchSelection(1)
			}
		}
		if backdrop.OverlayShortcut(0, ui.KeyUp) {
			if !st.stepScopeMenu(false) {
				m.moveSearchSelection(-1)
			}
		}
		if backdrop.OverlayShortcut(0, ui.KeyEnter) {
			if st.scopeMenuOpen {
				if m.acceptScopeCursor() {
					m.runSearch()
				}
				m.searchInputHandle.Focus()
			} else {
				m.openSearchHit(m.searchSelected)
			}
		}
		if backdrop.OverlayShortcut(0, ui.KeyTab) {
			if m.toggleScope() {
				m.runSearch()
			}
		}
		if backdrop.OverlayShortcut(ui.Shift, ui.KeyTab) {
			if m.toggleScope() {
				m.runSearch()
			}
		}
		if backdrop.OverlayShortcut(ui.Cmd, ui.Key0) {
			if m.resetScopeAll() {
				m.runSearch()
			}
		}
		if backdrop.OverlayShortcut(ui.Cmd, ui.KeyW) || (!theme.IsMacOS() && backdrop.OverlayShortcut(ui.Ctrl, ui.KeyF4)) {
			m.CloseSearch()
		}
		if !wasScopeOpen && (card.PressedOutside() || backdrop.OverlayShortcut(0, ui.KeyEscape)) {
			m.CloseSearch()
		}
	})
}

type searchHeaderCallbacks struct {
	onRunSearch     func()
	onInputChanged  func()
	onMoveSelection func(delta int)
	onAcceptScope   func()
	onOpenSelected  func()
	onToggleScope   func()
	onResetScopeAll func()
	onClose         func()
}

func renderSearchHeaderView(
	c *ui.Context,
	pal theme.Palette,
	st *searchPaletteState,
	activeNotebook string,
	query *string,
	lastQuery *string,
	inputHandle *ui.Handle,
	cb searchHeaderCallbacks,
) ui.Element {
	var scopeTrigger ui.Element
	ui.Row(c.Key("search-header")).FillWidth().Height(searchHeaderHeight).Shrink(0).
		Padding(0, 16, 0, 16).Gap(10).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
		Children(func() {
			searchIcon := icons.Draw(c, icons.FindSearch, theme.IconRow, pal.DuoIconColors(pal.TextSecondary))
			internalui.Probe(searchIcon, "search-header-icon")

			ph := st.searchPlaceholderParts(activeNotebook)
			var in ui.Element
			ui.Box(c.Key("search-input-wrap")).Grow(1).MinWidth(0).Height(30).Clip().Children(func() {
				in = ui.TextInputBase(c.Key("search-input"), query).
					FillWidth().Height(30).Padding(0, 0, 0, theme.Space2).
					Label(theme.Tr("搜索", "Search")).
					FontSize(theme.FontEditor).FixedLineHeight(theme.LineTitle).
					TextColor(pal.Text).CaretColor(pal.Accent).CaretWidth(2).
					Bind(inputHandle).AutoFocus()
				internalui.Probe(in, "search-input")

				if *query == "" && !in.Composing() {
					phRow := ui.Row(c.Key("search-placeholder")).
						Absolute().Top(0).Bottom(0).Left(theme.Space2).Right(0).
						AlignItems(ui.Center).Gap(theme.Space4).IgnoreInput(true)
					phRow.Children(func() {
						ui.Text(c, ph.prefix).
							FontSize(theme.FontEditor).FixedLineHeight(theme.LineTitle).
							TextColor(pal.Placeholder).SingleLine().Shrink(0)
						if ph.keycap != "" {
							internalui.Keycap(c, ph.keycap)
						}
						if ph.suffix != "" {
							ui.Text(c, ph.suffix).
								FontSize(theme.FontEditor).FixedLineHeight(theme.LineTitle).
								TextColor(pal.Placeholder).SingleLine().Shrink(0)
						}
					})
					internalui.Probe(phRow, "search-placeholder")
				}
			})

			in.OnTextKey(func(k ui.TextKey) bool {
				switch k.Key {
				case ui.KeyDown:
					if !st.stepScopeMenu(true) && cb.onMoveSelection != nil {
						cb.onMoveSelection(1)
					}
					return true
				case ui.KeyUp:
					if !st.stepScopeMenu(false) && cb.onMoveSelection != nil {
						cb.onMoveSelection(-1)
					}
					return true
				case ui.KeyEnter:
					if st.scopeMenuOpen {
						if cb.onAcceptScope != nil {
							cb.onAcceptScope()
						}
					} else if cb.onOpenSelected != nil {
						cb.onOpenSelected()
					}
					return true
				case ui.KeyTab:
					if k.Mods == 0 || k.Mods == ui.Shift {
						if cb.onToggleScope != nil {
							cb.onToggleScope()
						}
						return true
					}
				case ui.Key0:
					if k.Mods == ui.Cmd {
						if cb.onResetScopeAll != nil {
							cb.onResetScopeAll()
						}
						return true
					}
				case ui.KeyEscape:
					if st.scopeMenuOpen {
						st.scopeMenuOpen = false
					} else if cb.onClose != nil {
						cb.onClose()
					}
					return true
				case ui.KeyW:
					if k.Mods == ui.Cmd {
						if cb.onClose != nil {
							cb.onClose()
						}
						return true
					}
				case ui.KeyF4:
					if !theme.IsMacOS() && k.Mods == ui.Ctrl {
						if cb.onClose != nil {
							cb.onClose()
						}
						return true
					}
				}
				return false
			})
			if in.Changed() || *query != *lastQuery {
				*lastQuery = *query
				if cb.onInputChanged != nil {
					cb.onInputChanged()
				} else if cb.onRunSearch != nil {
					cb.onRunSearch()
				}
			}
			if in.Submitted() {
				if st.scopeMenuOpen {
					if cb.onAcceptScope != nil {
						cb.onAcceptScope()
					}
				} else if cb.onOpenSelected != nil {
					cb.onOpenSelected()
				}
			}

			scopeLabel := st.scopeLabel()
			scopeTrigger = ui.ButtonBase(c.Key("search-scope-trigger")).
				Height(30).Shrink(0).Padding(0, 10, 0, 10).Gap(6).
				AlignItems(ui.Center).Radius(theme.RadiusMD).
				Cursor(ui.CursorPointer).TabStop(false).Label(scopeLabel)

			hovered := scopeTrigger.Hovered()
			pressed := scopeTrigger.Pressed()
			bg := pal.Sidebar
			bd := pal.BtnBorder
			line := pal.TextSecondary
			if st.scopeMenuOpen {
				line = pal.Text
				bg = pal.Selected
				bd = pal.SelectedBorder
			} else if pressed {
				line = pal.Text
				bg = pal.Selected
				bd = pal.SelectedBorder
			} else if hovered {
				bg = pal.Hover
				bd = pal.SelectedBorder
			}
			internalui.AnimateSurfaceColors(scopeTrigger, bg, bd, true)
			internalui.TactileCard(scopeTrigger, hovered, pressed, st.scopeMenuOpen, false)
			scopeTrigger.Children(func() {
				iconActive := st.scopeMenuOpen || hovered || pressed
				icons.DrawAnimated(c, icons.Folder, theme.IconRow, pal.DuoIconColors(line), iconActive)
				ui.Box(c).MaxWidth(136).Children(func() {
					ui.Text(c, scopeLabel).FontSize(theme.FontBody).LineHeight(20.0 / 14.0).
						TextColor(pal.Text).SingleLine().Ellipsis("…")
				})
				icons.DrawAnimated(c, icons.ChevronDown, theme.IconInline, pal.DuoIconColors(line), iconActive)
			})
			internalui.Probe(scopeTrigger, "search-scope-trigger")
			if scopeTrigger.Clicked() {
				st.toggleScopeMenu()
				if inputHandle != nil {
					inputHandle.Focus()
				}
			}
		})
	return scopeTrigger
}

func renderScopeMenuPopoverView(
	c *ui.Context,
	pal theme.Palette,
	st *searchPaletteState,
	trigger ui.Element,
	inputHandle *ui.Handle,
	onPickScope func(idx int),
) {
	_, vpH := c.Size()
	menuMaxH := searchScopeMenuH
	if vpH > 0 {
		menuMaxH = min(searchScopeMenuH, max(vpH-96, 120))
	}
	ui.PopoverBase(c.Key("search-scope-menu"), trigger, &st.scopeMenuOpen, func(panel ui.Element) {
		panel.AttachTo(trigger, ui.AnchorBottomRight, ui.AnchorTopRight).Margin(4, 0, 0, 0)
		internalui.MenuSurface(panel).Width(searchScopeMenuW).MaxHeight(menuMaxH).Column().
			Transition(internalui.PopoverSpringTransition())
		internalui.ObserveMenuPointer(c, panel, &st.scopeCursor)
		internalui.Probe(panel, "search-scope-menu")

		folderIcon := icons.Folder
		suppressHover := st.scopeCursor >= 0
		internalui.Scroll(c.Key("search-scope-scroll"), &st.scopeScroll).
			FillWidth().MaxHeight(internalui.MenuInnerMaxHeight(menuMaxH)).
			ScrollbarStyle(ui.ScrollbarStyle{
				TrackWidth: theme.ScrollGutter,
				ThumbWidth: theme.ScrollThumbWidth,
				Thumb:      pal.ScrollThumb,
				Hover:      pal.ScrollThumbHover,
			}).
			Children(func() {
				ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
					internalui.MenuRow(c, internalui.MenuItemSpec{
						ID:            "search-scope-0",
						Label:         theme.Tr("全部", "All"),
						Icon:          &folderIcon,
						Checked:       st.scopeFolder == "",
						SuppressHover: suppressHover,
						OnClick: func() {
							if onPickScope != nil {
								onPickScope(0)
							}
							if inputHandle != nil {
								inputHandle.Focus()
							}
						},
					}, st.scopeCursor == 0).TabStop(false)

					if len(st.folders) > 0 {
						internalui.MenuRow(c, internalui.MenuItemSpec{Separator: true}, false)
					}

					for i, folderPath := range st.folders {
						idx := i + 1
						fp := folderPath
						internalui.MenuRow(c, internalui.MenuItemSpec{
							ID:            fmt.Sprintf("search-scope-%d", idx),
							Label:         searchFolderName(fp),
							Icon:          &folderIcon,
							Checked:       cleanPathEqual(st.scopeFolder, fp),
							SuppressHover: suppressHover,
							OnClick: func() {
								if onPickScope != nil {
									onPickScope(idx)
								}
								if inputHandle != nil {
									inputHandle.Focus()
								}
							},
						}, st.scopeCursor == idx).TabStop(false)
					}
				})
			})
	})
}

func renderSearchBodyView(
	c *ui.Context,
	pal theme.Palette,
	st *searchPaletteState,
	query string,
	hits []model.SearchHit,
	selected int,
	errMsg string,
	fillHeight bool,
	maxBodyH float32,
	allowEmptyFoldersSearch bool,
	onOpenHit func(idx int),
) {
	emptyH := searchEmptyHeight
	if maxBodyH > 0 && maxBodyH < emptyH {
		emptyH = maxBodyH
	}
	if errMsg != "" {
		box := ui.Box(c.Key("search-error")).FillWidth().Center().Padding(16).
			Transition(internalui.SurfaceStageTransition())
		if fillHeight {
			box.Grow(1).MinHeight(0)
		} else {
			box.Height(emptyH)
		}
		box.Children(func() {
			ui.Text(c, errMsg).FontSize(theme.FontSmall).TextColor(pal.Danger)
		})
		return
	}

	asked := strings.TrimSpace(query) != ""
	if len(st.folders) == 0 && len(hits) == 0 {
		// Still allow SearchFn-backed tests when no folders are configured on disk.
		if !asked || !allowEmptyFoldersSearch {
			renderSearchEmpty(c, pal, theme.Tr("请先在主窗口打开文件夹", "Open a folder in the main window first"), fillHeight, emptyH)
			return
		}
	}
	if !asked {
		renderSearchEmpty(c, pal, "", fillHeight, emptyH)
		return
	}
	if len(hits) == 0 {
		renderSearchEmpty(c, pal, theme.Tr("没有找到匹配的笔记", "No matching notes found"), fillHeight, emptyH)
		return
	}

	scroll := internalui.Scroll(c.Key("search-results"), &st.resultsScroll).
		FillWidth().
		Padding(8, theme.ScrollGutter, 8, 8).Gap(2).
		Transition(internalui.SurfaceStageTransition()).
		ScrollbarStyle(ui.ScrollbarStyle{
			TrackWidth: theme.ScrollGutter,
			ThumbWidth: theme.ScrollThumbWidth,
			Thumb:      pal.ScrollThumb,
			Hover:      pal.ScrollThumbHover,
		})
	if fillHeight {
		scroll.Grow(1).MinHeight(0)
	} else {
		if maxBodyH <= 0 {
			maxBodyH = searchMaxBodyHeight
		}
		scroll.MinHeight(emptyH).MaxHeight(maxBodyH)
	}
	scroll.Children(func() {
		groups := []struct {
			titleGroup bool
			label      string
		}{
			{true, theme.Tr("标题匹配", "Title Matches")},
			{false, theme.Tr("正文匹配", "Content Matches")},
		}
		hasRows := false
		rowOrder := 0
		for _, g := range groups {
			var indices []int
			total := 0
			for ix := range hits {
				meta := hitMetaAt(st, ix, hits[ix], query)
				if meta.titleMatch == g.titleGroup {
					indices = append(indices, ix)
					cnt := meta.count
					if cnt < 1 {
						cnt = 1
					}
					total += cnt
				}
			}
			if len(indices) == 0 {
				continue
			}
			header := g.label
			if !g.titleGroup {
				if theme.IsEn() {
					header = fmt.Sprintf("%s · %d matches", g.label, total)
				} else {
					header = fmt.Sprintf("%s · %d 处", g.label, total)
				}
			}
			pt := float32(6)
			if hasRows {
				pt = 10
			}
			hasRows = true
			ui.Column(c).FillWidth().Shrink(0).Padding(pt, 12, 2, 12).Children(func() {
				ui.Text(c, header).FontSize(theme.FontMeta).LineHeight(16.0 / 12.0).
					TextColor(pal.TextSecondary).SingleLine()
			})
			for _, ix := range indices {
				meta := hitMetaAt(st, ix, hits[ix], query)
				renderSearchHitRowView(c, pal, st, ix, rowOrder, hits[ix], meta, ix == selected, onOpenHit)
				rowOrder++
			}
		}
	})
}

func renderSearchEmpty(c *ui.Context, pal theme.Palette, hint string, fillHeight bool, emptyH float32) {
	box := ui.Box(c.Key("search-empty")).FillWidth().Center().Padding(0, 24, 0, 24).
		Transition(internalui.SurfaceStageTransition())
	if fillHeight {
		box.Grow(1).MinHeight(0)
	} else {
		if emptyH <= 0 {
			emptyH = searchEmptyHeight
		}
		box.Height(emptyH)
	}
	internalui.Probe(box, "search-empty")
	if hint != "" {
		box.Children(func() {
			ui.Text(c, hint).FontSize(theme.FontSmall).TextColor(pal.TextSecondary)
		})
	}
}

func renderSearchHitRowView(
	c *ui.Context,
	pal theme.Palette,
	st *searchPaletteState,
	idx int,
	orderIdx int,
	hit model.SearchHit,
	meta searchHitMeta,
	selected bool,
	onOpenHit func(idx int),
) {
	rowID := fmt.Sprintf("search-hit-%d", idx)
	rowKey := fmt.Sprintf("search-hit-%d:%d:%s:%d", st.appliedGen, idx, hit.Path, hit.Line)
	label := hit.Title
	if label == "" {
		label = filepath.Base(hit.Path)
	}

	row := ui.ButtonBase(c.Key(rowKey)).FillWidth().Padding(8, 12, 8, 12).Gap(2).
		Radius(theme.RadiusMD).Cursor(ui.CursorPointer).TabStop(false).Label(label).
		Transition(internalui.CascadeItemTransition(orderIdx, false))
	hovered := row.Hovered()
	pressed := row.Pressed()
	switch {
	case selected:
		row.Background(pal.Selected).Border(1, pal.SelectedBorder)
	case pressed:
		row.Background(pal.Selected).Border(1, ui.Transparent)
	case hovered:
		row.Background(pal.Hover).Border(1, ui.Transparent)
	default:
		row.Border(1, ui.Transparent)
	}
	internalui.TactileCard(row, hovered, pressed, selected, false)

	if selected && st.revealSelected {
		internalui.RevealActiveItem(row)
		st.revealSelected = false
	}

	right := meta.folder
	rightMarks := meta.folderMarks
	if !meta.titleMatch && meta.count > 1 {
		rightMarks = nil
		if theme.IsEn() {
			right = fmt.Sprintf("%d matches", meta.count)
		} else {
			right = fmt.Sprintf("%d 处", meta.count)
		}
	}

	row.Children(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ic := pal.DuoIconColors(pal.TextSecondary)
				if selected || hovered || pressed {
					ic = pal.DuoIconColors(pal.Text)
				}
				icons.DrawAnimated(c, icons.FileIconOrDefault(hit.Path), theme.IconRow, ic, selected || hovered || pressed)

				ui.Box(c).Grow(1).MinWidth(0).Children(func() {
					internalui.HighlightedText(c, label, meta.titleMarks, theme.FontBody, theme.WeightControl, pal.Text, pal.SearchHighlight)
				})
				if right != "" {
					ui.Box(c).MaxWidth(220).Shrink(0).Children(func() {
						internalui.HighlightedText(c, right, rightMarks, theme.FontMeta, 0, pal.TextSecondary, pal.SearchHighlight)
					})
				}
			})
			if hit.Snippet != "" {
				ui.Box(c).FillWidth().Padding(0, 0, 0, 22).Children(func() {
					internalui.HighlightedText(c, hit.Snippet, meta.snippetMarks, theme.FontSmall, theme.WeightRegular, pal.TextSecondary, pal.SearchHighlight)
				})
			}
		})
	})

	internalui.Probe(row, rowID)
	if row.Clicked() && onOpenHit != nil {
		onOpenHit(idx)
	}
}

func renderSearchFooter(c *ui.Context, pal theme.Palette, dragWindow bool) {
	row := ui.Row(c.Key("search-footer")).FillWidth().Height(searchFooterHeight).Shrink(0).
		Padding(0, 16, 0, 16).Gap(12).AlignItems(ui.Center).
		Radius(0, 0, searchModalRadius-1, searchModalRadius-1).
		BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Background(pal.Sidebar)
	if dragWindow {
		row.DragWindow()
	}
	row.Children(func() {
		renderFooterHint(c, pal, []string{"↑", "↓"}, theme.Tr("选择", "Select"))
		renderFooterHint(c, pal, []string{"↵"}, theme.Tr("打开", "Open"))
		renderFooterHint(c, pal, []string{"esc"}, theme.Tr("关闭", "Close"))
	})
}

func renderFooterHint(c *ui.Context, pal theme.Palette, keys []string, label string) {
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		for _, k := range keys {
			internalui.Keycap(c, k)
		}
		ui.Text(c, label).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
	})
}

func hitMetaAt(st *searchPaletteState, idx int, hit model.SearchHit, query string) searchHitMeta {
	if idx >= 0 && idx < len(st.hitMeta) {
		return st.hitMeta[idx]
	}
	q := strings.TrimSpace(query)
	isTitleMatch := hit.TitleMatch || hit.Line == 0
	var titleMarks [][2]int
	if isTitleMatch {
		titleMarks = files.FindTitleHighlights(hit.Title, q)
	}
	snippetMarks := files.FindBodyHighlights(hit.Snippet, q)
	if len(snippetMarks) == 0 && hit.MatchEnd > hit.MatchStart && hit.MatchEnd <= len(hit.Snippet) {
		snippetMarks = [][2]int{{hit.MatchStart, hit.MatchEnd}}
	}
	cnt := len(snippetMarks)
	if cnt < 1 {
		cnt = 1
	}
	var folderMarks [][2]int
	if isTitleMatch && len(titleMarks) == 0 && hit.Notebook != "" {
		folderMarks = files.FindTitleHighlights(hit.Notebook, q)
	}
	return searchHitMeta{
		titleMatch:   isTitleMatch,
		titleMarks:   titleMarks,
		snippetMarks: snippetMarks,
		folderMarks:  folderMarks,
		count:        cnt,
		folder:       hit.Notebook,
	}
}

func searchFolderLabel(roots []string, hitPath, stem, fallbackNotebook string, includeRootName bool) string {
	cleanHit := filepath.Clean(hitPath)
	var matchedRoot string
	for _, r := range roots {
		cleanRoot := filepath.Clean(r)
		if cleanHit == cleanRoot || strings.HasPrefix(cleanHit, cleanRoot+string(filepath.Separator)) {
			if len(cleanRoot) > len(matchedRoot) {
				matchedRoot = cleanRoot
			}
		}
	}
	if matchedRoot == "" {
		if includeRootName {
			return fallbackNotebook
		}
		return ""
	}

	relDir := ""
	if rel, err := filepath.Rel(matchedRoot, filepath.Dir(cleanHit)); err == nil && rel != "." && rel != "" && !strings.HasPrefix(rel, "..") {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		relDir = strings.Join(parts, " / ")
	}
	folder := ""
	if relDir != "" {
		if stem != "" {
			folder = relDir + " / " + stem
		} else {
			folder = relDir
		}
	}
	if !includeRootName {
		return folder
	}
	rootName := searchFolderName(matchedRoot)
	if rootName == "" {
		return folder
	}
	if folder == "" {
		return rootName
	}
	return rootName + " / " + folder
}

func searchFolderName(path string) string {
	base := filepath.Base(path)
	if base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return path
}

func cleanPathEqual(a, b string) bool {
	if a == "" && b == "" {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func containsCleanPath(paths []string, target string) bool {
	return findCleanPath(paths, target) != ""
}

func findCleanPath(paths []string, target string) string {
	if target == "" {
		return ""
	}
	cleanTarget := filepath.Clean(target)
	for _, p := range paths {
		if filepath.Clean(p) == cleanTarget {
			return p
		}
	}
	return ""
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
