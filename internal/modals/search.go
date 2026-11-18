package modals

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	searchModalWidth   float32 = 680
	searchModalHeight  float32 = 460
	searchModalRadius  float32 = 12
	searchHeaderHeight float32 = 52
	searchFooterHeight float32 = 36
	searchScopeMenuW   float32 = 236
	searchScopeMenuH   float32 = 304
	searchMaxHits              = 50
)

type searchHitMeta struct {
	titleMatch   bool
	titleMarks   [][2]int
	snippetMarks [][2]int
	count        int
	folder       string
}

type searchPaletteState struct {
	folders        []string
	scopeFolder    string // "" = All Folders
	scopeMenuOpen  bool
	scopeCursor    int
	scopeScroll    ui.ScrollState
	resultsScroll  ui.ScrollState
	revealSelected bool
	hitMeta        []searchHitMeta
}

var (
	searchStateMu sync.Mutex
	searchStates  = make(map[*Manager]*searchPaletteState)
)

func (m *Manager) searchState() *searchPaletteState {
	searchStateMu.Lock()
	defer searchStateMu.Unlock()
	st := searchStates[m]
	if st == nil {
		st = &searchPaletteState{}
		searchStates[m] = st
	}
	return st
}

// OpenSearch opens the global search palette.
func (m *Manager) OpenSearch(allNotebooks bool) {
	st := m.searchState()
	folders := m.notebookFolders()
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
	st.resultsScroll.Y = 0

	m.searchOpen = true
	m.searchSelected = 0
	m.searchErr = ""
	if strings.TrimSpace(m.searchQuery) != "" {
		m.runSearch()
	} else {
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
	m.searchOpen = false
	m.requestRedraw()
}

// IsSearchOpen reports whether the global search modal is open.
func (m *Manager) IsSearchOpen() bool {
	return m.searchOpen
}

func (m *Manager) searchActiveNotebookPath() string {
	st := m.currentState()
	if strings.TrimSpace(st.Folder) != "" {
		return filepath.Clean(st.Folder)
	}
	if m.host != nil && strings.TrimSpace(m.host.ActiveNotebook()) != "" {
		return filepath.Clean(m.host.ActiveNotebook())
	}
	return ""
}

func (m *Manager) notebookFolders() []string {
	appState := m.currentState()
	var raw []string
	if strings.TrimSpace(appState.Folder) != "" {
		raw = append(raw, appState.Folder)
	} else if m.host != nil && strings.TrimSpace(m.host.ActiveNotebook()) != "" {
		raw = append(raw, m.host.ActiveNotebook())
	}
	if len(appState.Notebooks) > 0 {
		raw = append(raw, appState.Notebooks...)
	} else if m.host != nil {
		raw = append(raw, m.host.Notebooks()...)
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
	st := m.currentState()
	hideNonMD := st.HideNonMarkdownFiles != nil && *st.HideNonMarkdownFiles
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
		return theme.Tr("全部文件夹", "All Folders")
	}
	return searchFolderName(st.scopeFolder)
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
		st.scopeCursor = 0
		if st.scopeFolder != "" {
			for i, f := range st.folders {
				if cleanPathEqual(f, st.scopeFolder) {
					st.scopeCursor = i + 1
					break
				}
			}
		}
		st.scopeScroll.Y = 0
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
	if cur >= count {
		cur = count - 1
	}
	if down {
		if cur+1 < count {
			st.scopeCursor = cur + 1
		}
	} else if cur > 0 {
		st.scopeCursor = cur - 1
	}
	return true
}

func (m *Manager) selectScopeIndex(idx int) bool {
	st := m.searchState()
	var next string
	if idx > 0 && idx-1 < len(st.folders) {
		next = st.folders[idx-1]
	} else if idx != 0 {
		return false
	}
	changed := !cleanPathEqual(st.scopeFolder, next)
	st.scopeFolder = next
	st.scopeMenuOpen = false
	m.searchAllNB = next == ""
	return changed
}

func (m *Manager) acceptScopeCursor() bool {
	st := m.searchState()
	if !st.scopeMenuOpen {
		return false
	}
	idx := st.scopeCursor
	if idx > len(st.folders) {
		idx = len(st.folders)
	}
	return m.selectScopeIndex(idx)
}

func (m *Manager) cycleScope(delta int) bool {
	st := m.searchState()
	st.scopeMenuOpen = false
	options := make([]string, 0, 1+len(st.folders))
	options = append(options, "")
	options = append(options, st.folders...)
	if len(options) == 0 || delta == 0 {
		return false
	}
	cur := 0
	for i, opt := range options {
		if cleanPathEqual(opt, st.scopeFolder) {
			cur = i
			break
		}
	}
	nextIdx := ((cur+delta)%len(options) + len(options)) % len(options)
	next := options[nextIdx]
	changed := !cleanPathEqual(st.scopeFolder, next)
	st.scopeFolder = next
	m.searchAllNB = next == ""
	return changed
}

func (m *Manager) resetScopeAll() bool {
	st := m.searchState()
	st.scopeMenuOpen = false
	changed := st.scopeFolder != ""
	st.scopeFolder = ""
	m.searchAllNB = true
	return changed
}

func (m *Manager) runSearch() {
	st := m.searchState()
	st.syncFolders(m.notebookFolders())
	m.searchAllNB = st.scopeFolder == ""

	q := strings.TrimSpace(m.searchQuery)
	m.searchLastQuery = m.searchQuery
	if q == "" {
		m.searchHits = nil
		st.hitMeta = nil
		m.searchSelected = 0
		m.searchErr = ""
		return
	}

	roots := st.effectiveRoots()
	if len(roots) == 0 && m.SearchFn == nil && m.host == nil {
		m.searchHits = nil
		st.hitMeta = nil
		m.searchSelected = 0
		m.searchErr = ""
		return
	}

	var hits []model.SearchHit
	var err error
	activeNB := m.searchActiveNotebookPath()
	switch {
	case st.scopeFolder == "":
		if m.SearchFn != nil {
			hits, err = m.SearchFn(q, true)
		} else if m.host != nil {
			hits, err = m.host.SearchWorkspace(q, true)
		} else {
			hits, err = files.SearchNotebooks(roots, q, m.searchScanOptions())
		}
	case activeNB != "" && cleanPathEqual(st.scopeFolder, activeNB):
		if m.SearchFn != nil {
			hits, err = m.SearchFn(q, false)
		} else if m.host != nil {
			hits, err = m.host.SearchWorkspace(q, false)
		} else {
			hits, err = files.SearchNotebooks(roots, q, m.searchScanOptions())
		}
	default:
		hits, err = files.SearchNotebooks([]string{st.scopeFolder}, q, m.searchScanOptions())
	}

	if err != nil {
		m.searchErr = err.Error()
		m.searchHits = nil
		st.hitMeta = nil
		m.searchSelected = 0
		return
	}

	// Partition into Title Matches then Content Matches while preserving order within each group.
	type pairedHit struct {
		hit  model.SearchHit
		meta searchHitMeta
	}
	includeRootName := len(roots) > 1
	paired := make([]pairedHit, 0, len(hits))
	for _, h := range hits {
		titleMarks := files.FindFoldMatches(h.Title, q)
		snippetMarks := files.FindFoldMatches(h.Snippet, q)
		if len(snippetMarks) == 0 && h.MatchEnd > h.MatchStart && h.MatchEnd <= len(h.Snippet) {
			snippetMarks = [][2]int{{h.MatchStart, h.MatchEnd}}
		}
		cnt := len(snippetMarks)
		if cnt < 1 {
			cnt = 1
		}
		paired = append(paired, pairedHit{
			hit: h,
			meta: searchHitMeta{
				titleMatch:   len(titleMarks) > 0,
				titleMarks:   titleMarks,
				snippetMarks: snippetMarks,
				count:        cnt,
				folder:       searchFolderLabel(roots, h.Path, h.Title, h.Notebook, includeRootName),
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

	m.searchErr = ""
	m.searchHits = orderedHits
	st.hitMeta = orderedMeta
	m.searchSelected = 0
	st.resultsScroll.Y = 0
}

func (m *Manager) moveSearchSelection(delta int) {
	if len(m.searchHits) == 0 {
		return
	}
	next := m.searchSelected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.searchHits) {
		next = len(m.searchHits) - 1
	}
	if next != m.searchSelected {
		m.searchSelected = next
		m.searchState().revealSelected = true
	}
}

func (m *Manager) openSearchHit(idx int) {
	if idx < 0 || idx >= len(m.searchHits) {
		return
	}
	hit := m.searchHits[idx]
	m.CloseSearch()
	if m.host != nil {
		_ = m.host.OpenNote(hit.Path, hit.Line)
	}
}

func (m *Manager) renderSearchModal(c *ui.Context) {
	pal := theme.Ocean()
	st := m.searchState()
	if st.syncFolders(m.notebookFolders()) && strings.TrimSpace(m.searchQuery) != "" {
		m.runSearch()
	}
	wasScopeOpen := st.scopeMenuOpen

	_, vpH := c.Size()
	topPad := float32(96)
	if vpH > 0 && vpH < searchModalHeight+topPad+24 {
		topPad = max(float32(16), (vpH-searchModalHeight)*0.35)
	}

	var scopeTrigger ui.Element
	var card ui.Element

	ui.Overlay(c, func() {
		backdrop := ui.Column(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			AlignItems(ui.Center).Padding(topPad, 24, 24, 24).
			Background(ui.RGBA(0x20, 0x24, 0x2c, 0x2e/255.0)).Modal()

		backdrop.Children(func() {
			card = ui.Column(c.Key("search-modal")).
				Width(searchModalWidth).Height(searchModalHeight).
				MaxWidthPercent(92).MaxHeightPercent(92).
				Radius(searchModalRadius).Background(pal.Menu).
				Border(1, pal.MenuBorder).Clip().
				Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.08)).
				Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.14)).
				Role(ui.RoleDialog).Label(theme.Tr("搜索笔记", "Search Notes"))
			internalui.Probe(card, "search-modal")

			card.Children(func() {
				scopeTrigger = m.renderSearchHeader(c, pal, st)
				m.renderSearchBody(c, pal, st)
				renderSearchFooter(c, pal)
			})
		})

		if st.scopeMenuOpen && scopeTrigger.Valid() {
			m.renderScopeMenuPopover(c, pal, st, scopeTrigger)
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
			} else {
				m.openSearchHit(m.searchSelected)
			}
		}
		if backdrop.OverlayShortcut(0, ui.KeyTab) {
			if m.cycleScope(1) {
				m.runSearch()
			}
		}
		if backdrop.OverlayShortcut(ui.Shift, ui.KeyTab) {
			if m.cycleScope(-1) {
				m.runSearch()
			}
		}
		if backdrop.OverlayShortcut(ui.Cmd, ui.Key0) {
			if m.resetScopeAll() {
				m.runSearch()
			}
		}
		if backdrop.OverlayShortcut(ui.Cmd, ui.KeyW) {
			m.CloseSearch()
		}
		if !wasScopeOpen && (card.PressedOutside() || backdrop.OverlayShortcut(0, ui.KeyEscape)) {
			m.CloseSearch()
		}
	})
}

func (m *Manager) renderSearchHeader(c *ui.Context, pal theme.Palette, st *searchPaletteState) ui.Element {
	var scopeTrigger ui.Element
	ui.Row(c.Key("search-header")).FillWidth().Height(searchHeaderHeight).Shrink(0).
		Padding(0, 16, 0, 16).Gap(10).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
		Children(func() {
			icons.Draw(c, icons.FindSearch, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))

			in := ui.TextInputBase(c.Key("search-input"), &m.searchQuery).
				Placeholder(theme.Tr("搜索标题与正文", "Search titles and content")).
				PlaceholderColor(pal.Placeholder).
				Label(theme.Tr("搜索", "Search")).
				FontSize(16).TextColor(pal.Text).CaretColor(pal.Accent).
				Bind(&m.searchInputHandle).AutoFocus().Grow(1).MinWidth(0)
			internalui.Probe(in, "search-input")

			in.OnTextKey(func(k ui.TextKey) bool {
				switch k.Key {
				case ui.KeyDown:
					if !st.stepScopeMenu(true) {
						m.moveSearchSelection(1)
					}
					return true
				case ui.KeyUp:
					if !st.stepScopeMenu(false) {
						m.moveSearchSelection(-1)
					}
					return true
				case ui.KeyEnter:
					if st.scopeMenuOpen {
						if m.acceptScopeCursor() {
							m.runSearch()
						}
					} else {
						m.openSearchHit(m.searchSelected)
					}
					return true
				case ui.KeyTab:
					if k.Mods == 0 || k.Mods == ui.Shift {
						delta := 1
						if k.Mods == ui.Shift {
							delta = -1
						}
						if m.cycleScope(delta) {
							m.runSearch()
						}
						return true
					}
				case ui.Key0:
					if k.Mods == ui.Cmd {
						if m.resetScopeAll() {
							m.runSearch()
						}
						return true
					}
				case ui.KeyEscape:
					if st.scopeMenuOpen {
						st.scopeMenuOpen = false
					} else {
						m.CloseSearch()
					}
					return true
				case ui.KeyW:
					if k.Mods == ui.Cmd {
						m.CloseSearch()
						return true
					}
				}
				return false
			})
			if in.Changed() || m.searchQuery != m.searchLastQuery {
				m.runSearch()
			}
			if in.Submitted() {
				if st.scopeMenuOpen {
					if m.acceptScopeCursor() {
						m.runSearch()
					}
				} else {
					m.openSearchHit(m.searchSelected)
				}
			}

			scopeLabel := st.scopeLabel()
			scopeTrigger = ui.ButtonBase(c.Key("search-scope-trigger")).
				Height(30).Shrink(0).Padding(0, 10, 0, 10).Gap(6).
				AlignItems(ui.Center).Radius(theme.RadiusControl).
				Cursor(ui.CursorPointer).Label(scopeLabel)

			line := pal.TextSecondary
			if st.scopeMenuOpen {
				line = pal.Accent
				scopeTrigger.Background(pal.Selected).Border(1, pal.Accent)
			} else if scopeTrigger.Hovered() {
				scopeTrigger.Background(pal.Hover).Border(1, pal.Accent)
			} else {
				scopeTrigger.Background(pal.Sidebar).Border(1, pal.BtnBorder)
			}
			scopeTrigger.Children(func() {
				icons.Draw(c, icons.Folder, theme.IconRow, theme.IconColors{
					Line:   line,
					Fill:   pal.IconFill,
					Accent: pal.Accent,
				})
				ui.Box(c).MaxWidth(136).Children(func() {
					ui.Text(c, scopeLabel).FontSize(theme.FontBody).LineHeight(20.0 / 14.0).
						TextColor(pal.Text).SingleLine().Ellipsis("…")
				})
				icons.Draw(c, icons.ChevronDown, theme.IconInline, pal.MonoIconColors(line))
			})
			internalui.Probe(scopeTrigger, "search-scope-trigger")
			if scopeTrigger.Clicked() {
				st.toggleScopeMenu()
			}
		})
	return scopeTrigger
}

func (m *Manager) renderScopeMenuPopover(c *ui.Context, pal theme.Palette, st *searchPaletteState, trigger ui.Element) {
	ui.PopoverBase(c.Key("search-scope-menu"), trigger, &st.scopeMenuOpen, func(panel ui.Element) {
		panel.AttachTo(trigger, ui.AnchorBottomRight, ui.AnchorTopRight).Margin(4, 0, 0, 0)
		internalui.MenuSurface(panel).Width(searchScopeMenuW).MaxHeight(searchScopeMenuH).Column()
		internalui.Probe(panel, "search-scope-menu")

		folderIcon := icons.Folder
		internalui.Scroll(c.Key("search-scope-scroll"), &st.scopeScroll).
			FillWidth().MaxHeight(searchScopeMenuH - 2*theme.MenuPadding).
			ScrollbarStyle(ui.ScrollbarStyle{
				TrackWidth: theme.ScrollGutter,
				ThumbWidth: theme.ScrollThumbWidth,
				Thumb:      pal.ScrollThumb,
				Hover:      pal.ScrollThumbHover,
			}).
			Children(func() {
				ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
					internalui.MenuRow(c, internalui.MenuItemSpec{
						ID:      "search-scope-0",
						Label:   theme.Tr("全部文件夹", "All Folders"),
						Icon:    &folderIcon,
						Checked: st.scopeFolder == "",
						OnClick: func() {
							if m.selectScopeIndex(0) {
								m.runSearch()
							}
						},
					}, st.scopeCursor == 0)

					if len(st.folders) > 0 {
						internalui.MenuRow(c, internalui.MenuItemSpec{Separator: true}, false)
					}

					for i, folderPath := range st.folders {
						idx := i + 1
						fp := folderPath
						internalui.MenuRow(c, internalui.MenuItemSpec{
							ID:      fmt.Sprintf("search-scope-%d", idx),
							Label:   searchFolderName(fp),
							Icon:    &folderIcon,
							Checked: cleanPathEqual(st.scopeFolder, fp),
							OnClick: func() {
								if m.selectScopeIndex(idx) {
									m.runSearch()
								}
							},
						}, st.scopeCursor == idx)
					}
				})
			})
	})
}

func (m *Manager) renderSearchBody(c *ui.Context, pal theme.Palette, st *searchPaletteState) {
	if m.searchErr != "" {
		ui.Box(c).FillWidth().Grow(1).MinHeight(0).Center().Padding(16).Children(func() {
			ui.Text(c, m.searchErr).FontSize(theme.FontSmall).TextColor(pal.Danger)
		})
		return
	}

	asked := strings.TrimSpace(m.searchQuery) != ""
	if len(st.folders) == 0 && len(m.searchHits) == 0 {
		// Still allow SearchFn-backed tests when no folders are configured on disk.
		if !asked || m.SearchFn == nil {
			renderSearchEmpty(c, pal, theme.Tr("请先在主窗口打开文件夹", "Open a folder in the main window first"))
			return
		}
	}
	if !asked {
		hint := theme.Tr("输入关键词搜索标题与正文", "Type keywords to search titles and content")
		if st.scopeFolder == "" {
			hint = theme.Tr("输入关键词搜索全部文件夹的标题与正文", "Type keywords to search titles and content across all folders")
		}
		renderSearchEmpty(c, pal, hint)
		return
	}
	if len(m.searchHits) == 0 {
		renderSearchEmpty(c, pal, theme.Tr("没有找到匹配的笔记", "No matching notes found"))
		return
	}

	internalui.Scroll(c.Key("search-results"), &st.resultsScroll).
		FillWidth().Grow(1).MinHeight(0).
		Padding(8, theme.ScrollGutter, 8, 8).Gap(2).
		ScrollbarStyle(ui.ScrollbarStyle{
			TrackWidth: theme.ScrollGutter,
			ThumbWidth: theme.ScrollThumbWidth,
			Thumb:      pal.ScrollThumb,
			Hover:      pal.ScrollThumbHover,
		}).
		Children(func() {
			groups := []struct {
				titleGroup bool
				label      string
			}{
				{true, theme.Tr("标题匹配", "Title Matches")},
				{false, theme.Tr("正文匹配", "Content Matches")},
			}
			hasRows := false
			for _, g := range groups {
				var indices []int
				total := 0
				for ix := range m.searchHits {
					meta := hitMetaAt(st, ix, m.searchHits[ix], m.searchQuery)
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
					meta := hitMetaAt(st, ix, m.searchHits[ix], m.searchQuery)
					m.renderSearchHitRow(c, pal, st, ix, m.searchHits[ix], meta)
				}
			}
		})
}

func renderSearchEmpty(c *ui.Context, pal theme.Palette, hint string) {
	ui.Box(c).FillWidth().Grow(1).MinHeight(0).Center().Padding(0, 24, 0, 24).Children(func() {
		ui.Text(c, hint).FontSize(theme.FontSmall).TextColor(pal.TextSecondary)
	})
}

func (m *Manager) renderSearchHitRow(c *ui.Context, pal theme.Palette, st *searchPaletteState, idx int, hit model.SearchHit, meta searchHitMeta) {
	selected := idx == m.searchSelected
	rowID := fmt.Sprintf("search-hit-%d", idx)
	label := hit.Title
	if label == "" {
		label = filepath.Base(hit.Path)
	}

	row := ui.ButtonBase(c.Key(rowID)).FillWidth().Padding(8, 12, 8, 12).Gap(2).
		Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Label(label)
	hovered := row.Hovered()
	switch {
	case selected:
		row.Background(pal.Selected).Border(1, pal.Accent)
	case hovered:
		row.Background(pal.Hover).Border(1, ui.Transparent)
	default:
		row.Border(1, ui.Transparent)
	}

	if selected && st.revealSelected {
		internalui.RevealActiveItem(row)
		st.revealSelected = false
	}

	right := meta.folder
	if !meta.titleMatch && meta.count > 1 {
		if theme.IsEn() {
			right = fmt.Sprintf("%d matches", meta.count)
		} else {
			right = fmt.Sprintf("%d 处", meta.count)
		}
	}

	row.Children(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ic := pal.MonoIconColors(pal.TextSecondary)
				if selected {
					ic = pal.DuoIconColors(pal.Accent)
				} else if hovered {
					ic = pal.DuoIconColors(pal.Text)
				}
				icons.Draw(c, icons.Note, theme.IconRow, ic)

				ui.Box(c).Grow(1).MinWidth(0).Children(func() {
					renderHighlightedText(c, label, meta.titleMarks, theme.FontBody, theme.WeightControl, pal.Text, pal.SearchHighlight)
				})
				if right != "" {
					ui.Box(c).MaxWidth(220).Shrink(0).Children(func() {
						ui.Text(c, right).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
					})
				}
			})
			if hit.Snippet != "" {
				ui.Box(c).FillWidth().Padding(0, 0, 0, 22).Children(func() {
					renderHighlightedText(c, hit.Snippet, meta.snippetMarks, theme.FontSmall, theme.WeightRegular, pal.TextSecondary, pal.SearchHighlight)
				})
			}
		})
	})

	internalui.Probe(row, rowID)
	if row.Clicked() {
		m.searchSelected = idx
		m.openSearchHit(idx)
	}
}

func renderSearchFooter(c *ui.Context, pal theme.Palette) {
	ui.Row(c.Key("search-footer")).FillWidth().Height(searchFooterHeight).Shrink(0).
		Padding(0, 16, 0, 16).Gap(12).AlignItems(ui.Center).
		Radius(0, 0, searchModalRadius-1, searchModalRadius-1).
		BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Background(pal.Sidebar).
		Children(func() {
			renderFooterHint(c, pal, []string{"↑", "↓"}, theme.Tr("选择", "Select"))
			renderFooterHint(c, pal, []string{"↵"}, theme.Tr("打开", "Open"))
			renderFooterHint(c, pal, []string{"Tab"}, theme.Tr("切换文件夹", "Switch Folder"))
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

func renderHighlightedText(c *ui.Context, text string, marks [][2]int, size float32, weight int, fg, hlBG ui.Color) {
	if len(marks) == 0 {
		el := ui.Text(c, text).FontSize(size).TextColor(fg).SingleLine().Ellipsis("…")
		if weight > 0 {
			el.FontWeight(weight)
		}
		return
	}
	spans := buildHighlightSpans(text, marks, hlBG)
	el := ui.RichText(c, spans...).FontSize(size).TextColor(fg).SingleLine().Ellipsis("…")
	if weight > 0 {
		el.FontWeight(weight)
	}
}

func buildHighlightSpans(text string, marks [][2]int, hlBG ui.Color) []ui.Span {
	spans := make([]ui.Span, 0, len(marks)*2+1)
	pos := 0
	for _, r := range marks {
		start, end := r[0], r[1]
		if start < pos {
			start = pos
		}
		if end > len(text) {
			end = len(text)
		}
		if start >= end {
			continue
		}
		if start > pos {
			spans = append(spans, ui.Span{Text: text[pos:start]})
		}
		spans = append(spans, ui.Span{Text: text[start:end], Background: hlBG})
		pos = end
	}
	if pos < len(text) {
		spans = append(spans, ui.Span{Text: text[pos:]})
	}
	if len(spans) == 0 {
		return []ui.Span{{Text: text}}
	}
	return spans
}

func hitMetaAt(st *searchPaletteState, idx int, hit model.SearchHit, query string) searchHitMeta {
	if idx >= 0 && idx < len(st.hitMeta) {
		return st.hitMeta[idx]
	}
	q := strings.TrimSpace(query)
	titleMarks := files.FindFoldMatches(hit.Title, q)
	snippetMarks := files.FindFoldMatches(hit.Snippet, q)
	if len(snippetMarks) == 0 && hit.MatchEnd > hit.MatchStart && hit.MatchEnd <= len(hit.Snippet) {
		snippetMarks = [][2]int{{hit.MatchStart, hit.MatchEnd}}
	}
	cnt := len(snippetMarks)
	if cnt < 1 {
		cnt = 1
	}
	return searchHitMeta{
		titleMatch:   len(titleMarks) > 0,
		titleMarks:   titleMarks,
		snippetMarks: snippetMarks,
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
