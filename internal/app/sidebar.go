package app

import (
	"path/filepath"
	"strconv"
	"strings"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/plugins/pluginui"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// SidebarUIState holds transient sidebar UI state (expanded directories, inline rename,
// drag-and-drop reorder state, notebook switcher popup, and tree item context menu + Move submenu).
type SidebarUIState struct {
	ExpandedDirs map[string]bool
	FocusDir     string
	// RevealRoot is the folder most recently expanded by the user; its visible
	// descendants unfold top-down as one sequence.
	RevealRoot string

	RenamingPath    string
	RenamingDir     bool
	RenamingNewItem bool
	RenameBuffer    string
	RenameFocus     bool
	renameInput     ui.Handle

	DropTargetPath string
	DropAfter      bool
	DropIntoDir    bool
	TreeDragGate   blockui.DragGate

	PendingMiddleOpenPath string
	NBDragTarget          string
	NBDragAfter           bool
	NBDragGate            blockui.DragGate
	ConfirmForgetNotebook string

	SwitcherOpen     bool
	SwitcherBounds   ui.Rect
	SortFilterOpen   bool
	SortFilterBounds ui.Rect

	MenuCursor      int
	MoveCursor      int
	ContextOpen     bool
	ContextPath     string
	ContextIsDir    bool
	ContextX        float32
	ContextY        float32
	ContextRowBounds  ui.Rect
	ContextIconBounds ui.Rect
	MoveSubmenuOpen bool
	MoveRowBounds   ui.Rect
	// MoveHoverHold blocks hover from reopening the cascade after a keyboard
	// close until the pointer leaves the "Move to…" row.
	MoveHoverHold bool

	IconPickerOpen          bool
	IconPickerNotebook      string
	IconPickerPath          string
	IconPickerIsDir         bool
	IconPickerAnchor        string // "tree", "footer", "switcher", "titlebar"
	IconPickerIconBounds    ui.Rect
	IconPickerRowBounds     ui.Rect
	IconPickerTint          theme.IconTint
	IconPickerActiveCat     string
	IconPickerJumpCat       string
	IconPickerRevealCurrent bool
	IconPickerScroll        ui.ScrollState

	treeScroll ui.ScrollState
	treeRows   []flatTreeRow

	nbAvail *notebookAvailability
}

func (s *SidebarUIState) notebookAvail() *notebookAvailability {
	if s.nbAvail == nil {
		s.nbAvail = &notebookAvailability{}
	}
	return s.nbAvail
}

// NewSidebarUIState initializes transient sidebar UI state.
func NewSidebarUIState() *SidebarUIState {
	return &SidebarUIState{
		ExpandedDirs: make(map[string]bool),
		MenuCursor:   -1,
		MoveCursor:   -1,
	}
}

// ToggleDir expands or collapses a folder, remembering an expansion as the
// reveal root so its rows cascade in tree order.
func (s *SidebarUIState) ToggleDir(path string) {
	expand := !s.ExpandedDirs[path]
	s.ExpandedDirs[path] = expand
	if expand {
		s.RevealRoot = path
	}
}

// OpenBlankContextMenu opens the tree context menu for blank space (no target entry):
// New Note, New Folder and Reveal for the active notebook.
func (s *SidebarUIState) OpenBlankContextMenu(x, y float32) {
	s.ClearDrag()
	s.SwitcherOpen = false
	s.SortFilterOpen = false
	s.MenuCursor, s.MoveCursor = -1, -1
	s.ContextOpen = true
	s.ContextPath = ""
	s.ContextIsDir = false
	s.ContextX, s.ContextY = x, y
	s.MoveSubmenuOpen = false
	s.MoveHoverHold = false
}

type sidebarContextKey struct {
	open bool
	path string
	x, y float32
}

func sidebarContextSnapshot(s *SidebarUIState) sidebarContextKey {
	return sidebarContextKey{s.ContextOpen, s.ContextPath, s.ContextX, s.ContextY}
}

// CloseIconPicker closes only the floating custom icon picker, leaving any
// parent popup such as the notebook switcher intact.
func (s *SidebarUIState) CloseIconPicker() bool {
	if !s.IconPickerOpen {
		return false
	}
	s.IconPickerOpen = false
	s.IconPickerNotebook = ""
	s.IconPickerPath = ""
	s.IconPickerAnchor = ""
	s.IconPickerJumpCat = ""
	s.IconPickerRevealCurrent = false
	return true
}

// ClosePopups closes switcher, sort/filter, and tree context menus.
func (s *SidebarUIState) ClosePopups() bool {
	had := s.SwitcherOpen || s.SortFilterOpen || s.ContextOpen || s.IconPickerOpen
	s.SwitcherOpen = false
	s.SortFilterOpen = false
	s.ConfirmForgetNotebook = ""
	s.ContextOpen = false
	s.MoveSubmenuOpen = false
	s.MoveHoverHold = false
	s.MenuCursor, s.MoveCursor = -1, -1
	s.CloseIconPicker()
	return had
}

// OpenIconPicker opens the floating custom file/folder icon picker anchored to
// the sidebar row and its leading icon button.
func (s *SidebarUIState) OpenIconPicker(path string, isDir bool, iconRect, rowRect ui.Rect, curTint theme.IconTint) {
	s.ClearDrag()
	s.ClosePopups()
	s.IconPickerOpen = true
	s.IconPickerNotebook = ""
	s.IconPickerPath = path
	s.IconPickerIsDir = isDir
	s.IconPickerAnchor = "tree"
	s.IconPickerIconBounds = iconRect
	s.IconPickerRowBounds = rowRect
	s.IconPickerTint = theme.NormalizeIconTint(string(curTint))
	s.IconPickerActiveCat = ""
	s.IconPickerJumpCat = ""
	s.IconPickerRevealCurrent = true
	s.IconPickerScroll.Y = 0
}

// OpenNotebookIconPicker opens the floating custom icon picker for notebookRoot itself.
// When anchor is "switcher", the notebook switcher menu stays open beside the picker.
func (s *SidebarUIState) OpenNotebookIconPicker(notebookRoot string, iconRect, rowRect ui.Rect, anchor string, curTint theme.IconTint) {
	if notebookRoot == "" {
		return
	}
	s.ClearDrag()
	if anchor == "switcher" {
		s.SortFilterOpen = false
		s.ConfirmForgetNotebook = ""
		s.ContextOpen = false
		s.MoveSubmenuOpen = false
		s.MoveHoverHold = false
		s.CloseIconPicker()
	} else {
		s.ClosePopups()
	}
	s.IconPickerOpen = true
	s.IconPickerNotebook = notebookRoot
	s.IconPickerPath = notebookRoot
	s.IconPickerIsDir = true
	s.IconPickerAnchor = anchor
	s.IconPickerIconBounds = iconRect
	s.IconPickerRowBounds = rowRect
	s.IconPickerTint = theme.NormalizeIconTint(string(curTint))
	s.IconPickerActiveCat = ""
	s.IconPickerJumpCat = ""
	s.IconPickerRevealCurrent = true
	s.IconPickerScroll.Y = 0
}

// HasActiveOverlay reports whether a sidebar popup or inline rename is active.
func (s *SidebarUIState) HasActiveOverlay() bool {
	return s != nil && (s.SwitcherOpen || s.SortFilterOpen || s.ContextOpen || s.IconPickerOpen || s.RenamingPath != "")
}

// StartRename enters inline rename mode for path.
func (s *SidebarUIState) StartRename(path string, isDir bool) {
	s.ClosePopups()
	s.RenamingPath = path
	s.RenamingDir = isDir
	s.RenamingNewItem = false
	s.RenameBuffer = noteDisplayTitle(path, isDir)
	s.RenameFocus = true
}

// CancelRename exits inline rename mode without modifying disk.
func (s *SidebarUIState) CancelRename() {
	s.RenamingPath = ""
	s.RenamingNewItem = false
	s.RenameBuffer = ""
	s.RenameFocus = false
	s.renameInput.CancelFocus()
}

// ClearDrag resets drag-and-drop tracking.
func (s *SidebarUIState) ClearDrag() {
	s.DropTargetPath = ""
	s.DropAfter = false
	s.DropIntoDir = false
}

type treeDragItem struct {
	Path  string
	IsDir bool
}

type nbDragItem struct {
	Path  string
	Index int
}

func isSameOrDescendant(path, ancestor string) bool {
	cp := filepath.Clean(path)
	ca := filepath.Clean(ancestor)
	return cp == ca || strings.HasPrefix(cp, ca+string(filepath.Separator))
}

// SidebarProps supplies current workspace state and callbacks to RenderSidebar.
type SidebarProps struct {
	// DeferPopups, when set, queues the menu overlays so they are built after
	// the editor and stack above its floating toolbars.
	DeferPopups    func(fn func())
	State          *SidebarUIState
	Width          *float32
	OnWidthChanged func(w float32)
	FloatingCard   bool

	ActiveNotebook string
	Notebooks      []string
	Entries        []model.FileEntry
	ActiveNote     string

	SettingsOpen bool
	SearchOpen   bool

	SortMode        model.TreeSortMode
	HideNonMarkdown bool
	HideAssets      bool

	ShortcutSearch    string
	ShortcutNewFolder string
	ShortcutNewNote   string

	OnOpenSearch         func()
	OnSetSortMode        func(mode model.TreeSortMode)
	OnToggleShowNonMD    func()
	OnToggleShowAssets   func()
	OnNewFolder          func()
	OnNewNote            func()
	OnNewNoteIn          func(dir string)
	OnOpenNote           func(path string, inNewTab bool)
	OnRenameItem         func(oldPath, newName string) error
	OnMoveItem           func(srcPath, dstDir string)
	OnReorderSibling     func(parentDir, srcPath, targetPath string, after bool)
	OnReorderNotebooks   func(fromIdx, toIdx int)
	OnTrashItem          func(path string)
	OnRevealItem         func(path string)
	OnSwitchNotebook     func(root string)
	OnForgetNotebook     func(root string)
	OnChooseNotebook     func()
	RequestRedraw        func()
	OnOpenSettings       func()
	FileBadges           map[string][]plugins.ResolvedFileBadge
	FileIcons            map[string]plugins.ResolvedFileIcon
	NotebookIcons        map[string]plugins.ResolvedFileIcon
	FileIconPack         *plugins.ResolvedFileIconPack
	OnSetFileIcon        func(pluginID, notebookRoot, relPath, iconID, tint string)
	PluginContextMenu    []plugins.ResolvedMenuItem
	OnPluginContextMenu  func(item plugins.ResolvedMenuItem, targetPath string, isDir bool)
	RenderPluginSections func(c *ui.Context)
}

type flatTreeRow struct {
	Entry       *model.FileEntry
	Depth       int
	ChildIdx    int
	Expanded    bool
	RevealOrder int
}

func countVisibleDescendants(entries []model.FileEntry, expanded map[string]bool) int {
	n := 0
	for i := range entries {
		e := &entries[i]
		n++
		if e.IsDir && expanded[e.Path] && len(e.Children) > 0 {
			n += countVisibleDescendants(e.Children, expanded)
		}
	}
	return n
}

func renderTreeEntries(c *ui.Context, p SidebarProps, entries []model.FileEntry, depth int, rowGap float32, treeFocus ui.Element, suppressHover bool, anyDragOver *bool) {
	for idx := range entries {
		e := &entries[idx]
		exp := e.IsDir && p.State.ExpandedDirs[e.Path]
		row := flatTreeRow{
			Entry:    e,
			Depth:    depth,
			ChildIdx: idx,
			Expanded: exp,
		}
		if !e.IsDir || len(e.Children) == 0 {
			if renderTreeRow(c, p, row, treeFocus, suppressHover) {
				*anyDragOver = true
			}
			continue
		}

		c.Key("tree-node:" + e.Path)
		node := ui.Column(c).FillWidth()
		progress, settled := blockui.TreeDrawerProgress(node, "drawerOpen", exp)
		node.Children(func() {
			if renderTreeRow(c, p, row, treeFocus, suppressHover) {
				*anyDragOver = true
			}
			if !exp && progress <= 0.001 {
				return
			}
			if exp && settled && progress >= 0.999 {
				c.Key("tree-drawer:" + e.Path)
				ui.Column(c).FillWidth().Padding(rowGap, 0, 0, 0).Gap(rowGap).Children(func() {
					renderTreeEntries(c, p, e.Children, depth+1, rowGap, treeFocus, suppressHover, anyDragOver)
				})
				return
			}

			visCount := countVisibleDescendants(e.Children, p.State.ExpandedDirs)
			if visCount <= 0 {
				return
			}
			drawerH := float32(visCount) * theme.TreeRowPitch * progress
			op := progress
			if !exp {
				op = progress * progress
			}
			slideY := (1 - progress) * -6

			c.Key("tree-drawer:" + e.Path)
			drawer := ui.Box(c).FillWidth().Height(drawerH).Clip()
			drawer.Children(func() {
				inner := ui.Column(c).FillWidth().Padding(rowGap, 0, 0, 0).Gap(rowGap).
					Opacity(op).Translate(0, slideY)
				inner.Children(func() {
					renderTreeEntries(c, p, e.Children, depth+1, rowGap, treeFocus, suppressHover || !exp, anyDragOver)
				})
			})
		})
	}
}

func flattenEntries(entries []model.FileEntry, depth int, expanded map[string]bool, out *[]flatTreeRow) {
	for idx := range entries {
		e := &entries[idx]
		exp := e.IsDir && expanded[e.Path]
		*out = append(*out, flatTreeRow{
			Entry:    e,
			Depth:    depth,
			ChildIdx: idx,
			Expanded: exp,
		})
		if exp && len(e.Children) > 0 {
			flattenEntries(e.Children, depth+1, expanded, out)
		}
	}
}

// assignRevealOrder numbers the rows under root by their flat position so an
// expansion unfolds top-down as one sequence, nested open folders included.
// Rows elsewhere fall back to their index among siblings.
func assignRevealOrder(rows []flatTreeRow, root string) {
	rootIdx, rootDepth := -1, 0
	for i := range rows {
		r := &rows[i]
		r.RevealOrder = r.ChildIdx
		if root != "" && rootIdx < 0 && r.Entry.Path == root {
			rootIdx, rootDepth = i, r.Depth
			continue
		}
		if rootIdx >= 0 {
			if r.Depth <= rootDepth {
				rootIdx = -2 // left the subtree; stop matching
				continue
			}
			r.RevealOrder = i - rootIdx - 1
		}
	}
}

func findSameKindSiblings(entries []model.FileEntry, targetPath string) []model.FileEntry {
	parent := filepath.Dir(filepath.Clean(targetPath))
	var search func(items []model.FileEntry, curDir string) []model.FileEntry
	search = func(items []model.FileEntry, curDir string) []model.FileEntry {
		if curDir == parent {
			var targetIsDir bool
			found := false
			for _, it := range items {
				if filepath.Clean(it.Path) == filepath.Clean(targetPath) {
					targetIsDir = it.IsDir
					found = true
					break
				}
			}
			if !found {
				return nil
			}
			var sibs []model.FileEntry
			for _, it := range items {
				if it.IsDir == targetIsDir {
					sibs = append(sibs, it)
				}
			}
			return sibs
		}
		for _, it := range items {
			if it.IsDir {
				if got := search(it.Children, filepath.Clean(it.Path)); got != nil {
					return got
				}
			}
		}
		return nil
	}
	return search(entries, filepath.Clean(parentRootOfEntries(entries, parent)))
}

func parentRootOfEntries(entries []model.FileEntry, fallback string) string {
	if len(entries) == 0 {
		return fallback
	}
	return filepath.Dir(filepath.Clean(entries[0].Path))
}

// splitSidebarFileLabel splits an unrecognized non-.md file name into its clean
// stem and a short uppercase extension badge (up to 6 chars) for the sidebar row.
// Canonical ".md" notes, folders, extensionless files, and very long suffixes
// stay badge-free.
func splitSidebarFileLabel(entry model.FileEntry) (title, extBadge string) {
	if entry.IsDir {
		return entry.Name, ""
	}
	raw := entry.RawName
	if raw == "" {
		raw = filepath.Base(entry.Path)
	}
	if files.HasHiddenExtension(raw) {
		return entry.Name, ""
	}
	ext := filepath.Ext(raw)
	tag := strings.TrimPrefix(ext, ".")
	stem := strings.TrimSuffix(raw, ext)
	if stem == "" || tag == "" || len(tag) > 6 {
		return entry.Name, ""
	}
	return stem, strings.ToUpper(tag)
}

func moveNotebookMenuItems(p SidebarProps, srcPath string) []blockui.MenuItemSpec {
	active := filepath.Clean(p.ActiveNotebook)
	parent := filepath.Dir(filepath.Clean(srcPath))
	icon := icons.Folder
	var items []blockui.MenuItemSpec
	for _, notebook := range p.Notebooks {
		dst := filepath.Clean(notebook)
		if notebook == "" || dst == active || dst == parent || isSameOrDescendant(dst, srcPath) {
			continue
		}
		items = append(items, blockui.MenuItemSpec{
			ID:    "move-dest:" + dst,
			Label: noteDisplayTitle(dst, true),
			Icon:  &icon,
			OnClick: func() {
				p.State.ClosePopups()
				if p.OnMoveItem != nil {
					p.OnMoveItem(srcPath, dst)
				}
			},
		})
	}
	if len(items) == 0 {
		items = append(items, blockui.MenuItemSpec{Label: theme.Tr("没有其他笔记本", "No other notebooks"), Disabled: true})
	}
	return items
}

// RenderSidebar renders the Ocean Paper notebook sidebar (scrollable file tree with
// inline rename, sibling reorder and a blank-area New Note / New Folder context menu,
// footer notebook switcher + settings,
// right-edge resize handle, and anchored switcher/context/cascading-move popups).
func RenderSidebar(c *ui.Context, p SidebarProps) {
	pal := theme.Ocean()
	chrome := pal.ShellChrome()
	w := SidebarDefaultW
	if p.Width != nil && *p.Width > 0 {
		w = ClampSidebarWidth(*p.Width)
	}

	c.Key("sidebar")
	root := ui.Row(c).Width(w).FillHeight().Shrink(0)
	if p.FloatingCard {
		root.Background(pal.Sidebar)
	}
	blockui.Probe(root, "sidebar")
	root.Children(func() {
		col := ui.Column(c).Grow(1).MinWidth(0).FillHeight()
		col.Children(func() {
			// 0. Top action bar: Search field + Sort/Filter + New Folder + New Page.
			renderSidebarHeaderBar(c, p)

			// 1. Top drop strip: dropping here moves an entry before the first root sibling.
			rows := p.State.treeRows[:0]
			p.State.treeRows = nil
			flattenEntries(p.Entries, 0, p.State.ExpandedDirs, &rows)
			assignRevealOrder(rows, p.State.RevealRoot)

			c.Key("tree-top-drop")
			topDrop := ui.Box(c).FillWidth().Height(4).Shrink(0)
			canDropFirst := func(item treeDragItem) bool {
				if p.State.RenamingPath != "" || len(rows) == 0 || p.ActiveNotebook == "" {
					return false
				}
				cleanNB := filepath.Clean(p.ActiveNotebook)
				first := rows[0].Entry
				return filepath.Dir(filepath.Clean(item.Path)) == cleanNB &&
					item.IsDir == first.IsDir &&
					filepath.Clean(item.Path) != filepath.Clean(first.Path)
			}
			if dragged, over := ui.DragOver[treeDragItem](topDrop); over && canDropFirst(dragged) {
				topDrop.BorderWidth(0, 0, 2, 0).BorderColor(pal.Accent)
			}
			if dropped, ok := ui.Drop[treeDragItem](topDrop); ok && canDropFirst(dropped) {
				p.State.ClearDrag()
				if p.OnReorderSibling != nil {
					p.OnReorderSibling(p.ActiveNotebook, dropped.Path, rows[0].Entry.Path, false)
				}
			}

			// 2. Scrollable File Tree + floating workspace notice.
			treeContainer := ui.Box(c).Grow(1).MinHeight(120).FillWidth()
			treeContainer.Children(func() {
				c.Key("file-tree-focus")
				treeFocus := ui.Column(c).Fill().Focusable().FocusRing(false)
				if p.State.RenamingPath == "" && !p.State.SwitcherOpen && !p.State.ContextOpen && !p.State.IconPickerOpen {
					switch blockui.MatchTreeShortcut(treeFocus) {
					case blockui.TreeKeyRename:
						if p.State.FocusDir != "" {
							p.State.StartRename(p.State.FocusDir, true)
						} else if p.ActiveNote != "" {
							p.State.StartRename(p.ActiveNote, false)
						}
					case blockui.TreeKeyTrash:
						target := p.State.FocusDir
						if target == "" {
							target = p.ActiveNote
						}
						if target != "" && p.OnTrashItem != nil {
							p.OnTrashItem(target)
						}
					}
				}
				rowMenuBefore := sidebarContextSnapshot(p.State)
				treeFocus.Children(func() {
					c.Key("file-tree-root")
					treeScroll := blockui.Scroll(c, &p.State.treeScroll).Fill().Padding(0, theme.TreeOuterPadX, 8, theme.TreeOuterPadX)
					blockui.Probe(treeScroll, "file-tree-root")
					scrollBounds := treeScroll.Bounds()
					treeScroll.Children(func() {
						if p.ActiveNotebook == "" {
							ui.Column(c).FillWidth().Padding(theme.Space8, 0, 0, 0).Children(func() {
								if blockui.GhostRow(c, "sidebar-choose-folder", theme.Tr("选择文件夹…", "Choose Folder…"), icons.FolderOpen).Clicked() && p.OnChooseNotebook != nil {
									p.OnChooseNotebook()
								}
							})
							return
						}
						if len(rows) == 0 {
							ui.Column(c).FillWidth().Padding(theme.Space8, 0, 0, 0).Children(func() {
								if blockui.GhostRow(c, "sidebar-empty-new-note", theme.Tr("新建笔记", "New Note"), icons.NotePlus).Clicked() && p.OnNewNote != nil {
									p.OnNewNote()
								}
							})
							return
						}

						anyDragOver := false
						suppressHover := blockui.IsScrollingForHover(c, treeScroll)
						rowGap := theme.TreeRowPitch - theme.HeightControl
						canVirtTree := len(rows) > 100 && !blockui.IsProbeEnabled() && p.State.DropTargetPath == "" && p.State.RenamingPath == ""
						firstIdx, lastIdx := 0, len(rows)
						if canVirtTree {
							vpH := scrollBounds.H
							if vpH <= 0 {
								vpH = 900
							}
							scrollY := max(p.State.treeScroll.Y, 0)
							firstIdx = max(int((scrollY-320)/theme.TreeRowPitch), 0)
							lastIdx = min(int((scrollY+vpH+320)/theme.TreeRowPitch)+1, len(rows))
							if firstIdx >= lastIdx {
								firstIdx = max(lastIdx-1, 0)
							}
						}
						ui.Column(c).FillWidth().Gap(rowGap).Children(func() {
							if canVirtTree {
								if firstIdx > 0 {
									topH := float32(firstIdx)*theme.TreeRowPitch - rowGap
									ui.Box(c.Key("tree-virt-top")).FillWidth().Height(max(topH, 0))
								}
								for i := firstIdx; i < lastIdx; i++ {
									if renderTreeRow(c, p, rows[i], treeFocus, suppressHover) {
										anyDragOver = true
									}
								}
								if rem := len(rows) - lastIdx; rem > 0 {
									botH := float32(rem)*theme.TreeRowPitch - rowGap
									ui.Box(c.Key("tree-virt-bot")).FillWidth().Height(max(botH, 0))
								}
							} else {
								renderTreeEntries(c, p, p.Entries, 0, rowGap, treeFocus, suppressHover, &anyDragOver)
							}
						})
						if !anyDragOver && p.State.DropTargetPath != "" {
							p.State.ClearDrag()
						}
						p.State.PendingMiddleOpenPath = ""
					})
				})
				// Right-click on blank tree space (rows consume their own right-clicks).
				if treeFocus.RightClicked() && p.ActiveNotebook != "" && sidebarContextSnapshot(p.State) == rowMenuBefore {
					b := treeFocus.Bounds()
					cx, cy := b.X+24, b.Y+24
					if px, py, ok := treeFocus.PointerPosition(); ok {
						cx, cy = b.X+px, b.Y+py
					}
					p.State.OpenBlankContextMenu(cx, cy)
				}
			})

			// Reuse storage on the next frame without retaining the file tree.
			// Release large buffers after the tree shrinks.
			if cap(rows)/2 <= max(len(rows), 128) {
				clear(rows)
				p.State.treeRows = rows[:0]
			}

			// 2.5. Plugin sidebar sections (contributes.sidebar_sections).
			if p.RenderPluginSections != nil {
				p.RenderPluginSections(c)
			}

			// 3. Footer: Notebook Switcher + Settings Button.
			footer := ui.Row(c).FillWidth().Height(48).Shrink(0).Padding(0, 8, 8, 12).Gap(4).AlignItems(ui.Center)
			if p.FloatingCard {
				footer.BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Background(pal.Sidebar)
			}
			footer.Children(func() {
				nbTitle := theme.Tr("选择笔记本", "Select Notebook")
				if p.ActiveNotebook != "" {
					nbTitle = noteDisplayTitle(p.ActiveNotebook, true)
				}
				nbIcon, hasNBIcon := sidebarNotebookIcon(p, p.ActiveNotebook)
				nbPickerEnabled := p.ActiveNotebook != "" && p.FileIconPack != nil && p.FileIconPack.Picker && p.OnSetFileIcon != nil
				nbPickerTarget := nbPickerEnabled && p.State.IconPickerOpen && p.State.IconPickerAnchor == "footer" &&
					filepath.Clean(p.State.IconPickerPath) == filepath.Clean(p.ActiveNotebook)

				swPadL := float32(8)
				swGap := float32(8)
				if nbPickerEnabled {
					swPadL = 6
					swGap = 6
				}
				c.Key("notebook-switcher")
				sw := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(32).Padding(0, 8, 0, swPadL).Gap(swGap).
					AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
					Label(nbTitle)
				swHovered := sw.Hovered()
				swPressed := sw.Pressed()
				swBG := ui.Transparent
				swBD := ui.Transparent
				if p.State.SwitcherOpen {
					swBG = pal.Selected
					swBD = pal.SelectedBorder
				} else if swPressed || swHovered {
					swBG = chrome.Hover
				}
				blockui.AnimateSurfaceColors(sw, swBG, swBD, p.State.SwitcherOpen)
				blockui.TactileCard(sw, swHovered, swPressed, p.State.SwitcherOpen, false)
				var nbIconClicked bool
				var nbIconRect ui.Rect
				drawFooterNBIcon := func(lineHot, accentHot bool) {
					if hasNBIcon {
						lineCol := pal.Text
						if !lineHot {
							lineCol = pal.TextSecondary
						}
						ic := pal.TintedDuoIconColors(theme.NormalizeIconTint(nbIcon.Tint), lineCol, false)
						pluginui.DrawFileIconAnimated(c, nbIcon.Ref, nbIcon.Spec, theme.IconRow, ic, swHovered || swPressed || p.State.SwitcherOpen || accentHot)
						return
					}
					blockui.MonogramBadge(c, nbTitle)
				}
				sw.Children(func() {
					if p.ActiveNotebook != "" {
						if nbPickerEnabled {
							iconBtn := ui.ButtonBase(c.Key("notebook-switcher-icon")).
								Size(theme.IconButtonBox, theme.IconButtonBox).Shrink(0).Center().
								Radius(theme.RadiusSM).Cursor(ui.CursorPointer).FocusRing(false).
								Label(theme.Tr("更换图标", "Change Icon"))
							iHov := iconBtn.Hovered()
							iPrs := iconBtn.Pressed()
							tileBG := ui.Transparent
							tileBD := ui.Transparent
							switch {
							case nbPickerTarget:
								tileBG = pal.Accent.Alpha(0.14)
								if pal.Dark {
									tileBG = pal.Accent.Alpha(0.24)
								}
								tileBD = pal.Accent.Alpha(0.45)
							case iHov || iPrs:
								tileBG = pal.SidebarIconTileHover()
							}
							blockui.AnimateSurfaceColors(iconBtn, tileBG, tileBD, false)
							blockui.TactileButton(iconBtn, iHov || nbPickerTarget, iPrs, false)
							if !nbPickerTarget {
								blockui.RichTooltip(c, iconBtn, blockui.TooltipSpec{
									Placement: blockui.TooltipAbove,
									Rows:      []blockui.TooltipRow{{Trigger: theme.Tr("更换图标", "Change Icon")}},
								})
							}
							iconBtn.Children(func() {
								drawFooterNBIcon(true, iHov || iPrs || nbPickerTarget)
							})
							blockui.Probe(iconBtn, "notebook-switcher-icon")
							nbIconRect = iconBtn.Bounds()
							nbIconClicked = iconBtn.Clicked()
						} else {
							drawFooterNBIcon(true, false)
						}
					} else {
						ic := pal.DuoIconColors(pal.Text)
						icons.DrawAnimated(c, icons.FolderOpen, theme.IconRow, ic, swHovered || swPressed || p.State.SwitcherOpen)
					}
					ui.Text(c, nbTitle).FontSize(theme.FontBody).FontWeight(theme.WeightRegular).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					if p.ActiveNotebook != "" && p.State.notebookAvail().Missing(p.ActiveNotebook) {
						ui.Box(c).Size(6, 6).Shrink(0).Radius(theme.RadiusPill).Background(pal.Warning).Tooltip(theme.Tr("当前文件夹已不可用", "Current folder is unavailable"))
					}
					chevTarget := float32(0)
					if p.State.SwitcherOpen {
						chevTarget = 180
					}
					chevDeg := sw.AnimateTo("nbChev", chevTarget, blockui.SpringTactile)
					chevWrap := ui.Box(c).Size(theme.IconInline, theme.IconInline).Shrink(0).Center()
					if chevDeg != 0 {
						chevWrap.Rotate(chevDeg)
					}
					chevWrap.Children(func() {
						icons.DrawAnimated(c, icons.ChevronUp, theme.IconInline, pal.DuoIconColors(pal.TextSecondary), swHovered || swPressed || p.State.SwitcherOpen)
					})
				})
				blockui.Probe(sw, "notebook-switcher")
				if b := sw.Bounds(); b.W > 0 {
					p.State.SwitcherBounds = b
					if nbPickerTarget {
						p.State.IconPickerRowBounds = b
					}
				}
				if nbPickerTarget && nbIconRect.W > 0 {
					p.State.IconPickerIconBounds = nbIconRect
				}
				if nbIconClicked && nbPickerEnabled {
					if nbPickerTarget {
						p.State.ClosePopups()
					} else {
						curTint := theme.IconTintNone
						if hasNBIcon {
							curTint = theme.NormalizeIconTint(nbIcon.Tint)
						}
						p.State.OpenNotebookIconPicker(p.ActiveNotebook, nbIconRect, sw.Bounds(), "footer", curTint)
					}
				} else if sw.Clicked() {
					next := !p.State.SwitcherOpen
					p.State.ClosePopups()
					p.State.SwitcherOpen = next
					if next {
						p.State.notebookAvail().Check(p.Notebooks, p.RequestRedraw)
					}
				}

				setBtn := blockui.IconButton(c, icons.Sliders, blockui.IconButtonOptions{
					ID:          "settings-button",
					Size:        28,
					IconSize:    16,
					Radius:      theme.RadiusSM,
					Tooltip:     theme.Tr("设置", "Settings"),
					Shortcut:    blockui.FormatShortcutDisplay("cmd-,"),
					Active:      p.SettingsOpen,
					ChromeHover: !p.FloatingCard,
				})
				if setBtn.Clicked() && p.OnOpenSettings != nil {
					p.OnOpenSettings()
				}
			})
		})

		// Right-edge resize splitter (omitted inside rounded floating edge card).
		if !p.FloatingCard && p.Width != nil {
			if blockui.SeamlessResizeHandle(c, "sidebar-resize", p.Width, SidebarMinW, SidebarMaxW, theme.SidebarDefaultWidth) {
				if p.OnWidthChanged != nil {
					p.OnWidthChanged(*p.Width)
				}
			}
		}
	})

	if p.DeferPopups != nil {
		p.DeferPopups(func() { renderSidebarPopups(c, p) })
		return
	}
	renderSidebarPopups(c, p)
}

func renderSidebarHeaderBar(c *ui.Context, p SidebarProps) {
	pal := theme.Ocean()
	chrome := pal.ShellChrome()
	scSearch := blockui.FormatShortcutDisplay(nonEmptyOr(p.ShortcutSearch, "cmd-k"))

	ui.Row(c.Key("sidebar-header-bar")).FillWidth().Shrink(0).
		Padding(4, 8, 4, 12).Gap(2).AlignItems(ui.Center).Children(func() {
		searchLabel := theme.Tr("搜索", "Search")
		c.Key("search-button")
		searchBtn := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(theme.HeightControl).
			PaddingX(8).Gap(8).AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
			Label(searchLabel)
		sHov := searchBtn.Hovered()
		sPress := searchBtn.Pressed()
		hoverBG := chrome.Hover
		if p.FloatingCard {
			hoverBG = pal.Hover
		}
		mutedFG := pal.Text.Alpha(0.55)
		sBG := ui.Transparent
		sBD := ui.Transparent
		sFG := mutedFG
		sIcon := pal.DuoIconColors(mutedFG)
		switch {
		case p.SearchOpen:
			sBG = pal.Selected
			sBD = pal.SelectedBorder
			sFG = pal.Text
			sIcon = pal.LitDuoIconColors(pal.Text)
		case sHov || sPress:
			sBG = hoverBG
			sFG = pal.Text
			sIcon = pal.DuoIconColors(pal.Text)
		}
		blockui.AnimateSurfaceColors(searchBtn, sBG, sBD, true)
		blockui.TactileCard(searchBtn, sHov, sPress, p.SearchOpen, false)
		blockui.RichTooltip(c, searchBtn, blockui.TooltipSpec{
			Placement: blockui.TooltipBelow,
			Rows: []blockui.TooltipRow{{
				Trigger:  searchLabel,
				Shortcut: scSearch,
			}},
		})
		searchBtn.Children(func() {
			icons.DrawAnimated(c, icons.FindSearch, theme.IconChrome, sIcon, sHov || sPress || p.SearchOpen)
			ui.Text(c, searchLabel).FontSize(theme.FontBody).
				TextColor(sFG).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		})
		blockui.Probe(searchBtn, "search-button")
		if searchBtn.Clicked() && p.OnOpenSearch != nil {
			p.OnOpenSearch()
		}

		sortBtn := blockui.IconButton(c, icons.SortFilter, blockui.IconButtonOptions{
			ID:          "sidebar-sort-filter",
			Size:        28,
			IconSize:    16,
			Radius:      theme.RadiusSM,
			Tooltip:     theme.Tr("排序与筛选", "Sort & Filter"),
			Active:      p.State.SortFilterOpen,
			ActiveStyle: blockui.ActiveAccent,
			ChromeHover: !p.FloatingCard,
			RestColor:   mutedFG,
		})
		if b := sortBtn.Bounds(); b.W > 0 {
			p.State.SortFilterBounds = b
		}
		if sortBtn.Clicked() {
			next := !p.State.SortFilterOpen
			p.State.ClosePopups()
			p.State.SortFilterOpen = next
		}

		folderBtn := blockui.IconButton(c, icons.FolderPlus, blockui.IconButtonOptions{
			ID:          "new-folder",
			Size:        28,
			IconSize:    16,
			Radius:      theme.RadiusSM,
			Tooltip:     theme.Tr("新建文件夹", "New Folder"),
			Shortcut:    blockui.FormatShortcutDisplay(nonEmptyOr(p.ShortcutNewFolder, "cmd-shift-n")),
			Disabled:    p.ActiveNotebook == "",
			ChromeHover: !p.FloatingCard,
			RestColor:   mutedFG,
		})
		if folderBtn.Clicked() && p.ActiveNotebook != "" && p.OnNewFolder != nil {
			p.OnNewFolder()
		}

		noteBtn := blockui.IconButton(c, icons.NotePlus, blockui.IconButtonOptions{
			ID:          "new-note",
			Size:        28,
			IconSize:    16,
			Radius:      theme.RadiusSM,
			Tooltip:     theme.Tr("新建页面", "New Page"),
			Shortcut:    blockui.FormatShortcutDisplay(nonEmptyOr(p.ShortcutNewNote, "cmd-n")),
			Disabled:    p.ActiveNotebook == "",
			ChromeHover: !p.FloatingCard,
			RestColor:   mutedFG,
		})
		if noteBtn.Clicked() && p.ActiveNotebook != "" && p.OnNewNote != nil {
			p.OnNewNote()
		}
	})
}

func renderTreeRow(c *ui.Context, p SidebarProps, r flatTreeRow, treeFocus ui.Element, suppressHover bool) bool {
	pal := theme.Ocean()
	entry := *r.Entry
	indent := theme.TreeRowPadLeft + float32(r.Depth)*theme.TreeIndentStep
	rowKey := "tree-row:" + entry.Path

	if p.State.RenamingPath == entry.Path {
		finish := func() {
			name := strings.TrimSpace(p.State.RenameBuffer)
			if (name != "" || p.State.RenamingNewItem) && p.OnRenameItem != nil {
				if err := p.OnRenameItem(entry.Path, name); err != nil {
					p.State.RenameFocus = true
					c.Invalidate()
					return
				}
			}
			p.State.CancelRename()
			c.Invalidate()
		}
		c.Key("tree-rename:" + entry.Path)
		box := ui.Row(c).FillWidth().Height(theme.HeightControl).Padding(0, 8, 0, indent).Gap(6).AlignItems(ui.Center).
			Radius(theme.RadiusMD).Background(pal.Paper).Border(1, pal.Accent)
		box.Children(func() {
			rel := sidebarRelPath(p.ActiveNotebook, entry.Path)
			if customIcon, ok := p.FileIcons[rel]; ok {
				pluginui.DrawFileIconAnimated(c, customIcon.Ref, customIcon.Spec, theme.IconRow,
					pal.TintedDuoIconColors(theme.NormalizeIconTint(customIcon.Tint), pal.Accent, false), true)
			} else {
				glyph := icons.FileIconOrDefault(filepath.Base(entry.Path))
				if entry.IsDir {
					glyph = icons.FolderIcon(filepath.Base(entry.Path), r.Expanded)
				}
				icons.DrawAnimated(c, glyph, theme.IconRow, pal.DuoIconColors(pal.Accent), true)
			}
			c.Key("tree-rename-input")
			inp := ui.TextInput(c, &p.State.RenameBuffer).Grow(1).FontSize(theme.FontBody).TextColor(pal.Text).
				BorderWidth(0).Background(ui.Transparent).Padding(0)
			if p.State.RenameFocus {
				inp.Bind(&p.State.renameInput)
				p.State.renameInput.SelectAll(c)
				inp.ScrollIntoView()
				p.State.RenameFocus = false
			}
			blockui.Probe(inp, "tree-rename-input")
			if inp.Submitted() {
				finish()
			} else if inp.Shortcut(0, ui.KeyEscape) {
				p.State.CancelRename()
				c.Invalidate()
			}
		})
		if p.State.RenamingPath == entry.Path && !p.State.RenameFocus && box.PressedOutside() {
			if p.State.RenamingNewItem {
				p.State.CancelRename()
				c.Invalidate()
			} else {
				finish()
			}
		}
		return false
	}

	selected := !entry.IsDir && entry.Path == p.ActiveNote
	focused := entry.IsDir && filepath.Clean(p.State.FocusDir) == filepath.Clean(entry.Path)
	activeFolder := focused && p.ActiveNote == ""
	highlighted := selected || activeFolder
	menuTarget := p.State.ContextOpen && p.State.ContextPath == entry.Path
	iconPickerTarget := p.State.IconPickerOpen && (p.State.IconPickerAnchor == "" || p.State.IconPickerAnchor == "tree") && p.State.IconPickerPath == entry.Path

	relPath := sidebarRelPath(p.ActiveNotebook, entry.Path)
	customIcon, hasCustomIcon := p.FileIcons[relPath]
	pickerEnabled := p.FileIconPack != nil && p.FileIconPack.Picker && p.OnSetFileIcon != nil && relPath != ""

	rowLeftPad := indent
	rowGap := theme.Space6
	if pickerEnabled {
		rowGap = theme.Space4
		if !entry.IsDir {
			rowLeftPad = max(indent-3, theme.Space4)
		}
	}

	c.Key(rowKey)
	btn := ui.ButtonBase(c).FillWidth().Height(theme.HeightControl).Padding(0, 8, 0, rowLeftPad).Gap(rowGap).
		AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
		FocusRing(false).Role(ui.RoleTreeItem).Selected(highlighted).Label(entry.Name)
	p.State.TreeDragGate.Attach(btn, entry.Path, treeDragItem{Path: entry.Path, IsDir: entry.IsDir}, blockui.DefaultDragThreshold)
	if !entry.IsDir {
		btn.ObserveInput(func(ev ui.InputEvent) {
			if ev.Kind == ui.InputPointerDown && ev.Button == 2 {
				p.State.PendingMiddleOpenPath = entry.Path
			}
		})
	}

	resolveDropMode := func(dragged treeDragItem) (ok, intoDir, after bool) {
		if dragged.Path == "" || filepath.Clean(dragged.Path) == filepath.Clean(entry.Path) {
			return false, false, false
		}
		canMoveInto := entry.IsDir &&
			filepath.Dir(filepath.Clean(dragged.Path)) != filepath.Clean(entry.Path) &&
			!(dragged.IsDir && isSameOrDescendant(entry.Path, dragged.Path))
		canReorder := filepath.Dir(filepath.Clean(dragged.Path)) == filepath.Dir(filepath.Clean(entry.Path)) &&
			dragged.IsDir == entry.IsDir
		if !canMoveInto && !canReorder {
			return false, false, false
		}
		b := btn.Bounds()
		if b.H <= 0 {
			b.H = theme.HeightControl
		}
		_, py, hasPos := btn.PointerPosition()
		if !hasPos && p.State.DropTargetPath == entry.Path {
			return true, p.State.DropIntoDir, p.State.DropAfter
		}
		if canMoveInto && (!canReorder || (py > 8 && py < b.H-8)) {
			return true, true, false
		}
		if canReorder {
			return true, false, py >= b.H*0.5
		}
		return false, false, false
	}

	dragOverHere := false
	isDropInto := false
	isDropReorder := false
	dropAfter := false
	if dragged, over := ui.DragOver[treeDragItem](btn); over {
		if ok, into, after := resolveDropMode(dragged); ok {
			dragOverHere = true
			isDropInto = into
			isDropReorder = !into
			dropAfter = after
			p.State.DropTargetPath = entry.Path
			p.State.DropIntoDir = into
			p.State.DropAfter = after
		}
	}
	if dropped, ok := ui.Drop[treeDragItem](btn); ok {
		if valid, into, after := resolveDropMode(dropped); valid {
			p.State.ClearDrag()
			if into {
				if p.OnMoveItem != nil {
					p.OnMoveItem(dropped.Path, entry.Path)
				}
			} else if p.OnReorderSibling != nil {
				p.OnReorderSibling(filepath.Dir(dropped.Path), dropped.Path, entry.Path, after)
			}
		} else {
			p.State.ClearDrag()
		}
	}

	hovered := !suppressHover && btn.Hovered()
	pressed := btn.Pressed()
	hoverBG := pal.ShellChrome().Hover
	if p.FloatingCard {
		hoverBG = pal.Hover
	}
	rowBG := ui.Transparent
	rowBD := ui.Transparent
	switch {
	case isDropInto:
		rowBG = pal.Selected
		rowBD = pal.Accent
	case isDropReorder:
		rowBG = hoverBG
	case highlighted:
		rowBG = pal.Selected
	case focused || pressed:
		rowBG = pal.Selected
	case menuTarget || iconPickerTarget || hovered:
		rowBG = hoverBG
	}
	blockui.AnimateSurfaceColors(btn, rowBG, rowBD, true)

	var iconClicked, iconRightClicked bool
	var iconRect ui.Rect

	drawLeadingIcon := func(lineHot, accentHot bool) {
		iconLit := highlighted || hovered || menuTarget || accentHot
		lineCol := pal.TextSecondary
		if lineHot {
			lineCol = pal.Text
		}
		if hasCustomIcon {
			ic := pal.TintedDuoIconColors(theme.NormalizeIconTint(customIcon.Tint), lineCol, false)
			pluginui.DrawFileIconAnimated(c, customIcon.Ref, customIcon.Spec, theme.IconRow, ic, iconLit)
			return
		}
		var glyph icons.Icon
		if entry.IsDir {
			glyph = icons.FolderIcon(filepath.Base(entry.Path), r.Expanded)
		} else {
			glyph = icons.FileIconOrDefault(filepath.Base(entry.Path))
		}
		icons.DrawAnimated(c, glyph, theme.IconRow, pal.DuoIconColors(lineCol), iconLit)
	}

	btn.Children(func() {
		if entry.IsDir {
			chevTarget := float32(0)
			if r.Expanded {
				chevTarget = 90
			}
			chevDeg := btn.AnimateTo("dirChev", chevTarget, blockui.TreeChevronAnim)
			chevBox := ui.Box(c).Size(theme.IconRow, theme.IconRow).Shrink(0).Center()
			if chevDeg != 0 {
				chevBox.Rotate(chevDeg)
			}
			chevBox.Children(func() {
				icons.DrawAnimated(c, icons.TbChevronRight, theme.IconInline, pal.DuoIconColors(pal.TextSecondary), hovered || highlighted || menuTarget)
			})
		}
		if pickerEnabled {
			iconBtn := ui.ButtonBase(c.Key("tree-icon:" + entry.Path)).
				Size(theme.IconButtonBox, theme.IconButtonBox).Shrink(0).Center().
				Radius(theme.RadiusSM).Cursor(ui.CursorPointer).FocusRing(false).
				Label(theme.Tr("更换图标", "Change Icon"))
			iHov := !suppressHover && iconBtn.Hovered()
			iPrs := iconBtn.Pressed()
			tileBG := ui.Transparent
			tileBD := ui.Transparent
			switch {
			case iconPickerTarget:
				tileBG = pal.Accent.Alpha(0.14)
				if pal.Dark {
					tileBG = pal.Accent.Alpha(0.24)
				}
				tileBD = pal.Accent.Alpha(0.45)
			case iHov || iPrs:
				tileBG = pal.SidebarIconTileHover()
			}
			blockui.AnimateSurfaceColors(iconBtn, tileBG, tileBD, false)
			blockui.TactileButton(iconBtn, iHov || iconPickerTarget, iPrs, false)
			if !iconPickerTarget {
				blockui.RichTooltip(c, iconBtn, blockui.TooltipSpec{
					Placement: blockui.TooltipAbove,
					Rows:      []blockui.TooltipRow{{Trigger: theme.Tr("更换图标", "Change Icon")}},
				})
			}
			iconBtn.Children(func() {
				drawLeadingIcon(highlighted || hovered || menuTarget || iHov || iconPickerTarget, iHov || iPrs || iconPickerTarget)
			})
			blockui.Probe(iconBtn, "tree-icon:"+entry.Path)
			iconRect = iconBtn.Bounds()
			iconClicked = iconBtn.Clicked()
			iconRightClicked = iconBtn.RightClicked()
		} else {
			drawLeadingIcon(highlighted || hovered || menuTarget, false)
		}
		rowTitle, extBadge := splitSidebarFileLabel(entry)
		ui.Text(c, rowTitle).FontSize(theme.FontBody).FontWeight(theme.WeightRegular).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if entry.IsDir && !r.Expanded && p.ActiveNote != "" && isSameOrDescendant(p.ActiveNote, entry.Path) {
			ui.Box(c).Size(6, 6).Radius(theme.RadiusPill).Background(pal.Accent).Shrink(0)
		}
		if extBadge != "" {
			blockui.FileExtBadge(c, extBadge, highlighted)
		}
		if !entry.IsDir && len(p.FileBadges) > 0 && p.ActiveNotebook != "" && relPath != "" {
			if badges := p.FileBadges[relPath]; len(badges) > 0 {
				renderTreeFileBadges(c, badges, pal)
			}
		}
	})
	if iconPickerTarget {
		if iconRect.W > 0 {
			p.State.IconPickerIconBounds = iconRect
		}
		if rb := btn.Bounds(); rb.W > 0 {
			p.State.IconPickerRowBounds = rb
		}
	}
	if isDropReorder || blockui.IsProbeEnabled() {
		btn.DrawOver(func(pt *ui.Painter, rect ui.Rect) {
			if blockui.IsProbeEnabled() {
				blockui.RecordBounds(rowKey, rect)
			}
			if isDropReorder {
				y := rect.Y
				if dropAfter {
					y = rect.Y + rect.H - 2
				}
				pt.Fill(ui.Rect{X: rect.X + indent, Y: y, W: max(rect.W-indent-6, 16), H: 2}, pal.Accent, 1)
			}
		})
	}

	openContextMenu := func(cx, cy float32) {
		p.State.ClearDrag()
		p.State.ClosePopups()
		p.State.ContextOpen = true
		p.State.ContextPath = entry.Path
		p.State.ContextIsDir = entry.IsDir
		p.State.ContextX = cx
		p.State.ContextY = cy
		p.State.ContextRowBounds = btn.Bounds()
		p.State.ContextIconBounds = iconRect
		p.State.MoveSubmenuOpen = false
		p.State.MoveRowBounds = ui.Rect{}
		p.State.MoveHoverHold = false
	}

	if p.State.PendingMiddleOpenPath == entry.Path {
		p.State.PendingMiddleOpenPath = ""
		treeFocus.Focus()
		p.State.ClearDrag()
		p.State.ClosePopups()
		if !entry.IsDir {
			p.State.FocusDir = ""
			if p.OnOpenNote != nil {
				p.OnOpenNote(entry.Path, true)
			}
		}
	} else if iconClicked && pickerEnabled {
		if iconPickerTarget {
			p.State.ClosePopups()
		} else {
			curTint := theme.IconTintNone
			if hasCustomIcon {
				curTint = theme.NormalizeIconTint(customIcon.Tint)
			}
			p.State.OpenIconPicker(entry.Path, entry.IsDir, iconRect, btn.Bounds(), curTint)
		}
	} else if btn.RightClicked() || iconRightClicked {
		b := btn.Bounds()
		cx, cy := b.X+24, b.Y+b.H
		if px, py, ok := btn.PointerPosition(); ok {
			cx, cy = b.X+px, b.Y+py
		}
		openContextMenu(cx, cy)
	} else if btn.Clicked() {
		treeFocus.Focus()
		p.State.ClearDrag()
		p.State.ClosePopups()
		if entry.IsDir {
			p.State.ToggleDir(entry.Path)
			p.State.FocusDir = entry.Path
		} else {
			p.State.FocusDir = ""
			if p.OnOpenNote != nil {
				inNewTab := (btn.ClickModifiers() & ui.Cmd) != 0
				p.OnOpenNote(entry.Path, inNewTab)
			}
		}
	}
	return dragOverHere
}

func renderSidebarPopups(c *ui.Context, p SidebarProps) {
	pal := theme.Ocean()
	vpW, vpH := c.Size()

	ui.Overlay(c, func() {
		if !p.State.SwitcherOpen && !p.State.SortFilterOpen && !p.State.ContextOpen && !p.State.IconPickerOpen {
			return
		}
		scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0)

		if p.State.IconPickerOpen && !p.State.SwitcherOpen {
			renderSidebarIconPicker(c, p, scrim, vpW, vpH)
			return
		}

		if p.State.SortFilterOpen {
			items := []blockui.MenuItemSpec{
				{
					ID:      "sort-manual",
					Label:   theme.Tr("手动排序", "Manual Order"),
					Checked: p.SortMode == model.TreeSortManual,
					OnClick: func() {
						p.State.ClosePopups()
						if p.OnSetSortMode != nil {
							p.OnSetSortMode(model.TreeSortManual)
						}
					},
				},
				{
					ID:      "sort-name",
					Label:   theme.Tr("按名称", "By Name"),
					Checked: p.SortMode == model.TreeSortName,
					OnClick: func() {
						p.State.ClosePopups()
						if p.OnSetSortMode != nil {
							p.OnSetSortMode(model.TreeSortName)
						}
					},
				},
				{
					ID:      "sort-modified",
					Label:   theme.Tr("按修改时间", "By Modified Time"),
					Checked: p.SortMode == model.TreeSortModified,
					OnClick: func() {
						p.State.ClosePopups()
						if p.OnSetSortMode != nil {
							p.OnSetSortMode(model.TreeSortModified)
						}
					},
				},
				{Separator: true},
				{
					ID:      "filter-non-md",
					Label:   theme.Tr("显示其他文件", "Show Non-Markdown Files"),
					Checked: !p.HideNonMarkdown,
					OnClick: func() {
						if p.OnToggleShowNonMD != nil {
							p.OnToggleShowNonMD()
						}
					},
				},
				{
					ID:      "filter-assets",
					Label:   theme.Tr("显示附件文件夹", "Show Assets Folder"),
					Checked: !p.HideAssets,
					OnClick: func() {
						if p.OnToggleShowAssets != nil {
							p.OnToggleShowAssets()
						}
					},
				},
			}
			menuW := min(float32(204), max(vpW-16, 1))
			estH := estimateMenuHeight(items)
			anchor := p.State.SortFilterBounds
			if anchor.W <= 0 {
				anchor = ui.Rect{X: 168, Y: 44, W: 28, H: 28}
			}
			spec := blockui.PlaceBelow(anchor)
			maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 304)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

			c.Key("sidebar-sort-filter-menu")
			card := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
			blockui.NavigateMenu(c, card, items, &p.State.MenuCursor)
			blockui.Probe(card, "sidebar-sort-filter-menu")
			card.Children(func() {
				for i, it := range items {
					blockui.MenuRow(c, it, i == p.State.MenuCursor)
				}
			})
			if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
				p.State.ClosePopups()
			}
			return
		}

		if p.State.SwitcherOpen {
			menuW := SidebarDefaultW
			if p.Width != nil && *p.Width > 0 {
				menuW = ClampSidebarWidth(*p.Width)
			}
			menuW = min(menuW, max(vpW-16, 1))
			rowCount := len(p.Notebooks) + 1
			estH := theme.MenuPadding*2 + float32(rowCount)*theme.MenuItemHeight + float32(max(rowCount-1, 0))*theme.MenuGap
			if len(p.Notebooks) > 0 {
				estH += 7 + theme.MenuGap
			}
			anchor := p.State.SwitcherBounds
			if anchor.W <= 0 {
				anchor = ui.Rect{X: 8, Y: vpH - 42, W: menuW, H: 30}
			}
			spec := blockui.PlaceAbove(anchor)
			maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 304)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

			c.Key("notebook-switcher-pop")
			card := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
			icPlus := icons.FolderPlus
			add := blockui.MenuItemSpec{
				ID:    "add-notebook",
				Label: theme.Tr("选择文件夹…", "Choose Folder…"),
				Icon:  &icPlus,
				OnClick: func() {
					p.State.ClosePopups()
					if p.OnChooseNotebook != nil {
						p.OnChooseNotebook()
					}
				},
			}
			choices := make([]blockui.MenuItemSpec, 0, len(p.Notebooks)+1)
			avail := p.State.notebookAvail()
			missingCount := 0
			for _, nb := range p.Notebooks {
				if avail.Missing(nb) {
					missingCount++
				}
			}
			if missingCount > 0 && p.OnForgetNotebook != nil {
				estH += theme.MenuItemHeight + theme.MenuGap
			}
			for _, nb := range p.Notebooks {
				choices = append(choices, blockui.MenuItemSpec{OnClick: func() {
					if avail.Missing(nb) {
						if p.OnForgetNotebook != nil {
							p.State.ConfirmForgetNotebook = nb
						}
						return
					}
					p.State.ClosePopups()
					if nb != p.ActiveNotebook && p.OnSwitchNotebook != nil {
						p.OnSwitchNotebook(nb)
					}
				}})
			}
			choices = append(choices, add)
			if !p.State.IconPickerOpen {
				blockui.NavigateMenu(c, card, choices, &p.State.MenuCursor)
			}
			blockui.Probe(card, "notebook-switcher-pop")
			card.Children(func() {
				if p.State.ConfirmForgetNotebook != "" {
					stillValid := false
					for _, nb := range p.Notebooks {
						if nb == p.State.ConfirmForgetNotebook {
							stillValid = true
							break
						}
					}
					if !stillValid {
						p.State.ConfirmForgetNotebook = ""
					}
				}
				blockui.Scroll(c).FillWidth().MaxHeight(blockui.MenuInnerMaxHeight(maxH)).Children(func() {
					ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
						for ix, nb := range p.Notebooks {
							ix := ix
							nb := nb
							checked := nb == p.ActiveNotebook
							confirming := p.State.ConfirmForgetNotebook == nb
							missing := avail.Missing(nb)
							nbIcon, hasNBIcon := sidebarNotebookIcon(p, nb)
							rowPickerEnabled := !missing && !confirming && p.FileIconPack != nil && p.FileIconPack.Picker && p.OnSetFileIcon != nil
							rowPickerTarget := rowPickerEnabled && p.State.IconPickerOpen && p.State.IconPickerAnchor == "switcher" &&
								filepath.Clean(p.State.IconPickerPath) == filepath.Clean(nb)
							rowPadL := float32(10)
							selGap := float32(8)
							if rowPickerEnabled {
								rowPadL = 7
								selGap = 6
							}
							c.Key("nb-item:" + nb)
							row := ui.Row(c).FillWidth().Height(theme.MenuItemHeight).Padding(0, 7, 0, rowPadL).Gap(4).AlignItems(ui.Center).Radius(theme.RadiusMD)
							nbDrop := false
							nbAfter := false
							if len(p.Notebooks) > 1 && p.OnReorderNotebooks != nil {
								if dragged, over := ui.DragOver[nbDragItem](row); over && dragged.Path != "" && dragged.Path != nb {
									b := row.Bounds()
									_, py, hasPos := row.PointerPosition()
									if hasPos && b.H > 0 {
										nbAfter = py >= b.H*0.5
									} else if p.State.NBDragTarget == nb {
										nbAfter = p.State.NBDragAfter
									}
									nbDrop = true
									p.State.NBDragTarget = nb
									p.State.NBDragAfter = nbAfter
									p.State.ConfirmForgetNotebook = ""
								}
								if dropped, ok := ui.Drop[nbDragItem](row); ok {
									after := nbAfter
									if !nbDrop && p.State.NBDragTarget == nb {
										after = p.State.NBDragAfter
									} else {
										b := row.Bounds()
										if _, py, hasPos := row.PointerPosition(); hasPos && b.H > 0 {
											after = py >= b.H*0.5
										}
									}
									p.State.NBDragTarget = ""
									p.State.NBDragAfter = false
									p.State.ConfirmForgetNotebook = ""
									if dropped.Path != "" && dropped.Path != nb {
										slot := ix
										if after {
											slot++
										}
										if dropped.Index < slot {
											slot--
										}
										if slot != dropped.Index {
											p.OnReorderNotebooks(dropped.Index, slot)
										}
									}
								}
								if nbDrop {
									row.DrawOver(func(pt *ui.Painter, rect ui.Rect) {
										y := rect.Y
										if nbAfter {
											y = rect.Y + rect.H - 2
										}
										pt.Fill(ui.Rect{X: rect.X + 6, Y: y, W: max(rect.W-12, 16), H: 2}, pal.Accent, 1)
									})
								}
							}
							kbActive := ix == p.State.MenuCursor
							blockui.RevealOnHighlight(row, kbActive)
							rowHover := !choices[ix].SuppressHover && row.Hovered()
							nbBG := ui.Transparent
							switch {
							case checked:
								nbBG = pal.Selected
							case confirming || rowHover || kbActive || nbDrop:
								nbBG = pal.MenuHover
							}
							blockui.AnimateSurfaceColors(row, nbBG, ui.Transparent, false)
							row.Children(func() {
								sel := ui.ButtonBase(c).Grow(1).MinWidth(0).FillHeight().Gap(selGap).AlignItems(ui.Center).
									Cursor(ui.CursorPointer).Label(noteDisplayTitle(nb, true))
								if len(p.Notebooks) > 1 && p.OnReorderNotebooks != nil && !confirming {
									p.State.NBDragGate.Attach(sel, nb, nbDragItem{Path: nb, Index: ix}, blockui.DefaultDragThreshold)
								}
								var rowIconClicked bool
								var rowIconRect ui.Rect
								drawNBRowIcon := func(lineHot, accentHot bool) {
									actIco := checked || confirming || rowHover || kbActive || accentHot
									lineCol := pal.MenuIcon
									if missing {
										lineCol = pal.TextSecondary
									} else if lineHot {
										lineCol = pal.Text
									}
									if hasNBIcon {
										ic := pal.TintedDuoIconColors(theme.NormalizeIconTint(nbIcon.Tint), lineCol, false)
										pluginui.DrawFileIconAnimated(c, nbIcon.Ref, nbIcon.Spec, theme.IconRow, ic, actIco)
										return
									}
									icons.DrawAnimated(c, icons.Folder, theme.IconRow, pal.DuoIconColors(lineCol), actIco)
								}
								sel.Children(func() {
									nameColor := pal.Text
									if missing {
										nameColor = pal.TextSecondary
									}
									if rowPickerEnabled {
										iconBtn := ui.ButtonBase(c.Key("nb-switcher-icon:"+nb)).
											Size(theme.IconButtonBox, theme.IconButtonBox).Shrink(0).Center().
											Radius(theme.RadiusSM).Cursor(ui.CursorPointer).FocusRing(false).
											Label(theme.Tr("更换图标", "Change Icon"))
										iHov := iconBtn.Hovered()
										iPrs := iconBtn.Pressed()
										tileBG := ui.Transparent
										tileBD := ui.Transparent
										switch {
										case rowPickerTarget:
											tileBG = pal.Accent.Alpha(0.14)
											if pal.Dark {
												tileBG = pal.Accent.Alpha(0.24)
											}
											tileBD = pal.Accent.Alpha(0.45)
										case iHov || iPrs:
											tileBG = pal.SidebarIconTileHover()
										}
										blockui.AnimateSurfaceColors(iconBtn, tileBG, tileBD, false)
										blockui.TactileButton(iconBtn, iHov || rowPickerTarget, iPrs, false)
										if !rowPickerTarget {
											blockui.RichTooltip(c, iconBtn, blockui.TooltipSpec{
												Placement: blockui.TooltipAbove,
												Rows:      []blockui.TooltipRow{{Trigger: theme.Tr("更换图标", "Change Icon")}},
											})
										}
										iconBtn.Children(func() {
											drawNBRowIcon(checked || rowHover || kbActive || iHov || rowPickerTarget, iHov || iPrs || rowPickerTarget)
										})
										blockui.Probe(iconBtn, "nb-switcher-icon:"+nb)
										rowIconRect = iconBtn.Bounds()
										rowIconClicked = iconBtn.Clicked()
									} else {
										drawNBRowIcon(checked || rowHover || kbActive, false)
									}
									ui.Text(c, noteDisplayTitle(nb, true)).FontSize(theme.FontBody).TextColor(nameColor).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
									if missing && !confirming && !rowHover {
										ui.Text(c, theme.Tr("已移动或删除", "Moved or deleted")).FontSize(theme.FontSmall).TextColor(pal.Warning).Shrink(0).SingleLine()
									}
									if checked && !confirming && !(rowHover && p.OnForgetNotebook != nil) {
										ui.Box(c).Size(theme.TreeMoreBtnSize, theme.TreeMoreBtnSize).Shrink(0).Center().Children(func() {
											icons.DrawAnimated(c, icons.Check, theme.IconMenuCheck, pal.MinimalIconColors(pal.Text), rowHover || kbActive)
										})
									}
								})
								if rowPickerTarget {
									if rowIconRect.W > 0 {
										p.State.IconPickerIconBounds = rowIconRect
									}
									if rb := row.Bounds(); rb.W > 0 {
										p.State.IconPickerRowBounds = rb
									}
								}
								blockui.Probe(sel, "nb-select:"+nb)
								if missing {
									sel.Tooltip(nb)
								}
								if rowIconClicked && rowPickerEnabled {
									if rowPickerTarget {
										p.State.CloseIconPicker()
									} else {
										curTint := theme.IconTintNone
										if hasNBIcon {
											curTint = theme.NormalizeIconTint(nbIcon.Tint)
										}
										p.State.OpenNotebookIconPicker(nb, rowIconRect, row.Bounds(), "switcher", curTint)
									}
								} else if sel.Clicked() {
									if confirming {
										p.State.ConfirmForgetNotebook = ""
									} else {
										choices[ix].OnClick()
									}
								}
								if p.OnForgetNotebook != nil {
									fgID := "nb-forget:" + nb
									if confirming {
										confirmed, cancelled := blockui.InlineConfirm(c, blockui.InlineConfirmOptions{
											ID:           fgID,
											ConfirmLabel: theme.Tr("移除", "Remove"),
											Danger:       true,
										})
										if cancelled {
											p.State.ConfirmForgetNotebook = ""
										} else if confirmed {
											p.State.ConfirmForgetNotebook = ""
											avail.Forget(nb)
											p.OnForgetNotebook(nb)
										}
									} else if rowHover {
										fgBtn := blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{
											ID:       fgID,
											Size:     theme.TreeMoreBtnSize,
											IconSize: theme.IconMenuCheck,
											Radius:   theme.RadiusSM,
											Tooltip:  theme.Tr("从列表移除", "Remove from list"),
											Danger:   true,
										})
										if fgBtn.Clicked() {
											p.State.ConfirmForgetNotebook = nb
										}
									}
								}
							})
						}
						if missingCount > 0 && p.OnForgetNotebook != nil {
							label := theme.Tr("移除不可用的文件夹", "Remove unavailable folders")
							if missingCount > 1 {
								label = theme.Tr("移除 %d 个不可用的文件夹", "Remove %d unavailable folders")
								label = strings.Replace(label, "%d", strconv.Itoa(missingCount), 1)
							}
							icTrash := icons.Trash
							if blockui.GhostRow(c, "nb-forget-missing", label, icTrash).Clicked() {
								for _, nb := range append([]string(nil), p.Notebooks...) {
									if avail.Missing(nb) {
										avail.Forget(nb)
										p.OnForgetNotebook(nb)
									}
								}
								p.State.ConfirmForgetNotebook = ""
							}
						}
						if len(p.Notebooks) > 0 {
							ui.Box(c).FillWidth().Height(1).MarginY(3).Background(pal.MenuSeparator)
						}
						blockui.MenuRow(c, add, p.State.MenuCursor == len(p.Notebooks))
					})
				})
			})
			var pickerCard ui.Element
			if p.State.IconPickerOpen {
				pickerCard = renderSidebarIconPickerCard(c, p, scrim, vpW, vpH)
			}
			if card.PressedOutside() && (!pickerCard.Valid() || pickerCard.PressedOutside()) {
				p.State.ClosePopups()
			} else if scrim.OverlayShortcut(0, ui.KeyEscape) {
				if p.State.IconPickerOpen {
					p.State.CloseIconPicker()
				} else if p.State.ConfirmForgetNotebook != "" {
					p.State.ConfirmForgetNotebook = ""
				} else {
					p.State.ClosePopups()
				}
			}
			return
		}

		if p.State.ContextOpen {
			menuW := min(float32(232), max(vpW-16, 1))
			targetPath := p.State.ContextPath
			targetDir := p.State.ContextIsDir
			icOpen := icons.NotePlus
			icReveal := icons.FolderReveal
			icRename := icons.Pencil
			icMove := icons.ArrowRight
			icTrash := icons.Trash

			var items []blockui.MenuItemSpec
			if targetPath == "" {
				items = blankTreeMenuItems(p)
			} else {
				if !targetDir {
					openNewTabShortcut := theme.Tr("⌘+点击", "⌘+Click")
					if !theme.IsMacOS() {
						openNewTabShortcut = theme.Tr("Ctrl+点击", "Ctrl+Click")
					}
					items = append(items,
						blockui.MenuItemSpec{
							ID:       "open-new-tab",
							Label:    theme.Tr("新标签页", "Open in New Tab"),
							Shortcut: openNewTabShortcut,
							Icon:     &icOpen,
							OnClick: func() {
								p.State.ClosePopups()
								if p.OnOpenNote != nil {
									p.OnOpenNote(targetPath, true)
								}
							},
						},
						blockui.MenuItemSpec{Separator: true},
					)
				} else if p.OnNewNoteIn != nil {
					items = append(items,
						blockui.MenuItemSpec{
							ID:    "tree-new-note-in",
							Label: theme.Tr("新建页面", "New Page"),
							Icon:  &icOpen,
							OnClick: func() {
								p.State.ClosePopups()
								p.OnNewNoteIn(targetPath)
							},
						},
						blockui.MenuItemSpec{Separator: true},
					)
				}
				renameShortcut := "↵"
				if !theme.IsMacOS() {
					renameShortcut = "F2"
				}
				items = append(items,
					blockui.MenuItemSpec{
						ID:       "rename",
						Label:    theme.Tr("重命名", "Rename"),
						Shortcut: renameShortcut,
						Icon:     &icRename,
						OnClick: func() {
							p.State.StartRename(targetPath, targetDir)
						},
					},
				)
				if p.FileIconPack != nil && p.OnSetFileIcon != nil && sidebarRelPath(p.ActiveNotebook, targetPath) != "" {
					icChangeIcon := icons.Sparkles
					rowBounds := p.State.ContextRowBounds
					iconBounds := p.State.ContextIconBounds
					items = append(items, blockui.MenuItemSpec{
						ID:    "change-icon",
						Label: theme.Tr("更换图标…", "Change Icon…"),
						Icon:  &icChangeIcon,
						OnClick: func() {
							curTint := theme.IconTintNone
							if existing, ok := p.FileIcons[sidebarRelPath(p.ActiveNotebook, targetPath)]; ok {
								curTint = theme.NormalizeIconTint(existing.Tint)
							}
							p.State.OpenIconPicker(targetPath, targetDir, iconBounds, rowBounds, curTint)
						},
					})
				}
				items = append(items,
					blockui.MenuItemSpec{
						ID:         "tree-move-row",
						Label:      theme.Tr("移动到…", "Move to…"),
						Icon:       &icMove,
						HasSubmenu: true,
						OnClick: func() {
							// Hover already opens the cascade; a click must not close it again.
							if !p.State.MoveSubmenuOpen {
								p.State.MoveSubmenuOpen = true
								p.State.MoveCursor = -1
							}
						},
					},
				)
				items = append(items, blockui.MenuItemSpec{Separator: true})
				for _, pm := range p.PluginContextMenu {
					if (targetDir && pm.OnFolder) || (!targetDir && pm.OnNote) {
						pmItem := pm
						spec := blockui.MenuItemSpec{
							ID:    "tree-plugin:" + pmItem.PluginID + "/" + pmItem.ID,
							Label: pmItem.Title,
							OnClick: func() {
								p.State.ClosePopups()
								if p.OnPluginContextMenu != nil {
									p.OnPluginContextMenu(pmItem, targetPath, targetDir)
								}
							},
						}
						if len(pmItem.Icon.StrokePaths) > 0 {
							icSpec := pmItem.Icon
							spec.CustomIcon = func(c *ui.Context, size float32, colors theme.IconColors) {
								pluginui.DrawDuoSpec(c, icSpec, size, colors)
							}
						}
						items = append(items, spec)
					}
				}
				items = append(items,
					blockui.MenuItemSpec{
						ID:    "reveal",
						Label: revealFileLabel(),
						Icon:  &icReveal,
						OnClick: func() {
							p.State.ClosePopups()
							if p.OnRevealItem != nil {
								p.OnRevealItem(targetPath)
							}
						},
					},
					blockui.MenuItemSpec{Separator: true},
					blockui.MenuItemSpec{
						ID:    "trash",
						Label: theme.Tr("移到废纸篓", "Move to Trash"),
						Shortcut: func() string {
							if theme.IsMacOS() {
								return blockui.FormatShortcutDisplay("cmd-backspace")
							}
							return blockui.FormatShortcutDisplay("delete")
						}(),
						Icon:   &icTrash,
						Danger: true,
						OnClick: func() {
							p.State.ClosePopups()
							if p.OnTrashItem != nil {
								p.OnTrashItem(targetPath)
							}
						},
					},
				)

			}

			estH := estimateMenuHeight(items)
			mainSpec := blockui.PlaceAtPoint(p.State.ContextX, p.State.ContextY)
			maxH := blockui.MaxHeightForSpec(mainSpec, vpW, vpH, 304)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(mainSpec, menuW, popH, vpW, vpH)

			c.Key("tree-menu-pop")
			mainCard := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
			if !p.State.MoveSubmenuOpen {
				blockui.NavigateMenu(c, mainCard, items, &p.State.MenuCursor)
			}
			blockui.Probe(mainCard, "tree-menu-pop")
			mainCard.Children(func() {
				blockui.Scroll(c).FillWidth().MaxHeight(blockui.MenuInnerMaxHeight(maxH)).Children(func() {
					ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
						for i, it := range items {
							el := blockui.MenuRow(c, it, i == p.State.MenuCursor || it.ID == "tree-move-row" && p.State.MoveSubmenuOpen)
							if it.ID == "tree-move-row" {
								if b := el.Bounds(); b.W > 0 {
									p.State.MoveRowBounds = b
								}
								if !el.Hovered() {
									p.State.MoveHoverHold = false
								} else if !p.State.MoveSubmenuOpen && !p.State.MoveHoverHold {
									p.State.MoveSubmenuOpen = true
									p.State.MoveCursor = -1
								}
							} else if !it.Separator && el.Hovered() && p.State.MoveSubmenuOpen {
								p.State.MoveSubmenuOpen = false
								p.State.MoveCursor = -1
							}
						}
					})
				})
			})

			var subCard ui.Element
			rowRect := p.State.MoveRowBounds
			hasSub := p.State.MoveSubmenuOpen && rowRect.W > 0
			if hasSub {
				choices := moveNotebookMenuItems(p, targetPath)
				subEstH := estimateMenuHeight(choices)
				subSpec := blockui.PlaceBelow(rowRect).WithAlignX(blockui.CascadeRight(8))
				subMaxH := min(max(vpH-16, 1), float32(300))
				subPopH := min(subEstH, subMaxH)
				sox, soy := blockui.ResolveOrigin(subSpec, menuW, subPopH, vpW, vpH)

				c.Key("tree-move-pop")
				subCard = blockui.MenuSurface(ui.Column(c).Absolute().Left(sox).Top(soy).Width(menuW).MaxHeight(subMaxH).Role(ui.RoleMenu))
				blockui.Probe(subCard, "tree-move-pop")
				if subCard.OverlayShortcut(0, ui.KeyLeft) {
					p.State.MoveSubmenuOpen = false
					p.State.MoveHoverHold = true
					p.State.MoveCursor = -1
				} else {
					blockui.NavigateMenu(c, subCard, choices, &p.State.MoveCursor)
				}

				subCard.Children(func() {
					blockui.Scroll(c).FillWidth().MaxHeight(blockui.MenuInnerMaxHeight(subMaxH)).Children(func() {
						ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
							for i, item := range choices {
								blockui.MenuRow(c, item, i == p.State.MoveCursor)
							}
						})
					})
				})
			}

			if scrim.OverlayShortcut(0, ui.KeyEscape) {
				if p.State.MoveSubmenuOpen {
					p.State.MoveSubmenuOpen = false
					p.State.MoveHoverHold = true
					p.State.MoveCursor = -1
				} else {
					p.State.ClosePopups()
				}
			} else if mainCard.PressedOutside() && (!hasSub || subCard.PressedOutside()) {
				p.State.ClosePopups()
			}
		}
	})
}

// blankTreeMenuItems lists the context menu for blank sidebar space.
func blankTreeMenuItems(p SidebarProps) []blockui.MenuItemSpec {
	icNote := icons.NotePlus
	icFolder := icons.FolderPlus
	icReveal := icons.FolderReveal
	root := p.ActiveNotebook
	return []blockui.MenuItemSpec{
		{
			ID:       "blank-new-note",
			Label:    theme.Tr("新建笔记", "New Note"),
			Shortcut: blockui.FormatShortcutDisplay(nonEmptyOr(p.ShortcutNewNote, "cmd-n")),
			Icon:     &icNote,
			OnClick: func() {
				p.State.ClosePopups()
				if p.OnNewNote != nil {
					p.OnNewNote()
				}
			},
		},
		{
			ID:       "blank-new-folder",
			Label:    theme.Tr("新建文件夹", "New Folder"),
			Shortcut: blockui.FormatShortcutDisplay("cmd-shift-n"),
			Icon:     &icFolder,
			OnClick: func() {
				p.State.ClosePopups()
				if p.OnNewFolder != nil {
					p.OnNewFolder()
				}
			},
		},
		{Separator: true},
		{
			ID:    "blank-reveal",
			Label: theme.Tr("在访达显示", "Reveal in File Manager"),
			Icon:  &icReveal,
			OnClick: func() {
				p.State.ClosePopups()
				if p.OnRevealItem != nil && root != "" {
					p.OnRevealItem(root)
				}
			},
		},
	}
}

func renderTreeFileBadges(c *ui.Context, badges []plugins.ResolvedFileBadge, pal theme.Palette) {
	ui.Row(c).Gap(theme.Space4).AlignItems(ui.Center).Shrink(0).Children(func() {
		for _, b := range badges {
			badge := b
			toneCol := pal.Accent
			switch badge.Tone {
			case "success":
				toneCol = pal.Success
			case "warning":
				toneCol = pal.Warning
			case "danger":
				toneCol = pal.Danger
			}
			ic := pal.TintedIconColors(toneCol)
			chip := ui.Row(c.Key("tree-badge-" + badge.PluginID + "-" + badge.BadgeID)).
				Gap(theme.Space2).AlignItems(ui.Center).Shrink(0)
			if badge.Tooltip != "" {
				blockui.RichTooltip(c, chip, blockui.TooltipSpec{
					Placement: blockui.TooltipAbove,
					Rows:      []blockui.TooltipRow{{Trigger: badge.Tooltip}},
				})
			}
			chip.Children(func() {
				pluginui.DrawDuoSpec(c, badge.Icon, theme.IconInline, ic)
				if badge.Text != "" {
					ui.Text(c, badge.Text).
						Font("monospace").
						FontSize(theme.FontMeta).
						FixedLineHeight(theme.LineMeta).
						FontWeight(theme.WeightMono).
						TextColor(toneCol).
						SingleLine()
				}
			})
		}
	})
}
