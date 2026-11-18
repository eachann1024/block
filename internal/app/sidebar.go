package app

import (
	"path/filepath"
	"strings"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/model"
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

	RenamingPath string
	RenamingDir  bool
	RenameBuffer string
	RenameFocus  bool

	DropTargetPath string
	DropAfter      bool
	DropIntoDir    bool

	PendingMiddleOpenPath string
	NBDragTarget          string
	NBDragAfter           bool
	ConfirmForgetNotebook string

	SwitcherOpen   bool
	SwitcherBounds ui.Rect

	MenuCursor      int
	MoveCursor      int
	ContextOpen     bool
	ContextPath     string
	ContextIsDir    bool
	ContextX        float32
	ContextY        float32
	MoveSubmenuOpen bool
	MoveRowBounds   ui.Rect
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
	s.MenuCursor, s.MoveCursor = -1, -1
	s.ContextOpen = true
	s.ContextPath = ""
	s.ContextIsDir = false
	s.ContextX, s.ContextY = x, y
	s.MoveSubmenuOpen = false
}

type sidebarContextKey struct {
	open bool
	path string
	x, y float32
}

func sidebarContextSnapshot(s *SidebarUIState) sidebarContextKey {
	return sidebarContextKey{s.ContextOpen, s.ContextPath, s.ContextX, s.ContextY}
}

// ClosePopups closes switcher and tree context menus.
func (s *SidebarUIState) ClosePopups() bool {
	had := s.SwitcherOpen || s.ContextOpen
	s.SwitcherOpen = false
	s.ConfirmForgetNotebook = ""
	s.ContextOpen = false
	s.MoveSubmenuOpen = false
	s.MenuCursor, s.MoveCursor = -1, -1
	return had
}

// StartRename enters inline rename mode for path.
func (s *SidebarUIState) StartRename(path string, isDir bool) {
	s.ClosePopups()
	s.RenamingPath = path
	s.RenamingDir = isDir
	s.RenameBuffer = noteDisplayTitle(path, isDir)
	s.RenameFocus = true
}

// CancelRename exits inline rename mode without modifying disk.
func (s *SidebarUIState) CancelRename() {
	s.RenamingPath = ""
	s.RenameBuffer = ""
	s.RenameFocus = false
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
	State          *SidebarUIState
	Width          *float32
	OnWidthChanged func(w float32)

	ActiveNotebook string
	Notebooks      []string
	Entries        []model.FileEntry
	ActiveNote     string
	Notice         string

	SettingsOpen bool
	HasUpdate    bool

	ShortcutNewNote string

	OnNewFolder        func()
	OnNewNote          func()
	OnOpenNote         func(path string, inNewTab bool)
	OnRenameItem       func(oldPath, newName string)
	OnMoveItem         func(srcPath, dstDir string)
	OnReorderSibling   func(parentDir, srcPath, targetPath string, after bool)
	OnReorderNotebooks func(fromIdx, toIdx int)
	OnTrashItem        func(path string)
	OnRevealItem       func(path string)
	OnSwitchNotebook   func(root string)
	OnForgetNotebook   func(root string)
	OnChooseNotebook   func()
	OnOpenSettings     func()
	OnOpenUpdater      func()
}

type flatTreeRow struct {
	Entry       model.FileEntry
	Depth       int
	ChildIdx    int
	Expanded    bool
	RevealOrder int
}

func flattenEntries(entries []model.FileEntry, depth int, expanded map[string]bool, out *[]flatTreeRow) {
	for idx, e := range entries {
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

func fileEntryIcon(name string) icons.Icon {
	if files.IsMarkdown(name) {
		return icons.Note
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".svg":
		return icons.Image
	case ".html", ".htm":
		return icons.LangHtml
	case ".json":
		return icons.LangJson
	case ".yaml", ".yml":
		return icons.LangYaml
	case ".js", ".mjs", ".cjs":
		return icons.LangJs
	case ".ts", ".tsx":
		return icons.LangTs
	case ".py":
		return icons.LangPython
	case ".rs":
		return icons.LangRust
	case ".css":
		return icons.LangCss
	case ".sh", ".zsh", ".bash":
		return icons.LangBash
	case ".sql":
		return icons.LangSql
	case ".xml":
		return icons.LangXml
	default:
		return icons.Code
	}
}

type moveDest struct {
	Dir    string
	Depth  int
	Label  string
	IsRoot bool
}

func collectMoveDestinations(root string, entries []model.FileEntry, srcPath string, srcIsDir bool) []moveDest {
	if root == "" {
		return nil
	}
	cleanRoot := filepath.Clean(root)
	cleanSrc := filepath.Clean(srcPath)
	parent := filepath.Dir(cleanSrc)

	var out []moveDest
	if parent != cleanRoot {
		out = append(out, moveDest{
			Dir:    cleanRoot,
			Depth:  0,
			Label:  noteDisplayTitle(cleanRoot, true),
			IsRoot: true,
		})
	}
	var walk func(items []model.FileEntry, depth int)
	walk = func(items []model.FileEntry, depth int) {
		for _, it := range items {
			if !it.IsDir {
				continue
			}
			cleanDir := filepath.Clean(it.Path)
			if srcIsDir && (cleanDir == cleanSrc || strings.HasPrefix(cleanDir, cleanSrc+string(filepath.Separator))) {
				continue
			}
			if cleanDir != parent {
				out = append(out, moveDest{
					Dir:   cleanDir,
					Depth: depth,
					Label: it.Name,
				})
			}
			walk(it.Children, depth+1)
		}
	}
	walk(entries, 1)
	return out
}

// RenderSidebar renders the Ocean Paper notebook sidebar (scrollable file tree with
// inline rename, sibling reorder and a blank-area New Note / New Folder context menu,
// footer notebook switcher + settings,
// right-edge resize handle, and anchored switcher/context/cascading-move popups).
func RenderSidebar(c *ui.Context, p SidebarProps) {
	pal := theme.Ocean()
	w := SidebarDefaultW
	if p.Width != nil && *p.Width > 0 {
		w = ClampSidebarWidth(*p.Width)
	}

	c.Key("sidebar")
	root := ui.Row(c).Width(w).FillHeight().Shrink(0).Background(pal.Sidebar)
	blockui.Probe(root, "sidebar")
	root.Children(func() {
		col := ui.Column(c).Grow(1).MinWidth(0).FillHeight()
		col.Children(func() {
			// 1. Top drop strip: dropping here moves an entry before the first root sibling.
			var rows []flatTreeRow
			flattenEntries(p.Entries, 0, p.State.ExpandedDirs, &rows)
			assignRevealOrder(rows, p.State.RevealRoot)

			c.Key("tree-top-drop")
			topDrop := ui.Box(c).FillWidth().Height(8).Shrink(0)
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
			treeContainer := ui.Box(c).Grow(1).MinHeight(0).FillWidth()
			treeContainer.Children(func() {
				c.Key("file-tree-focus")
				treeFocus := ui.Column(c).Fill().Focusable().FocusRing(false)
				if p.State.RenamingPath == "" && !p.State.SwitcherOpen && !p.State.ContextOpen {
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
					treeScroll := blockui.Scroll(c).Fill().Padding(0, theme.TreeOuterPadX, 8, theme.TreeOuterPadX)
					blockui.Probe(treeScroll, "file-tree-root")
					treeScroll.Children(func() {
						if p.ActiveNotebook == "" {
							ui.Column(c).FillWidth().Padding(28, 12, 16, 12).Gap(12).AlignItems(ui.Center).Children(func() {
								icons.Mascot(c, icons.MascotNoFolder, 112)
								ui.Text(c, theme.Tr("尚未打开笔记本", "No notebook open")).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary)
								chooseBtn := blockui.ActionButton(c, "sidebar-choose-folder", theme.Tr("选择文件夹…", "Choose Folder…"), nil, blockui.ButtonPrimary, false)
								if chooseBtn.Clicked() && p.OnChooseNotebook != nil {
									p.OnChooseNotebook()
								}
							})
							return
						}
						if len(rows) == 0 {
							ui.Column(c).FillWidth().Padding(28, 12, 16, 12).Gap(10).AlignItems(ui.Center).Children(func() {
								icons.Mascot(c, icons.MascotEmptyFolder, 104)
								ui.Text(c, theme.Tr("文件夹为空，右键新建笔记", "Folder is empty. Right-click to create a note")).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
							})
							return
						}

						anyDragOver := false
						ui.Column(c).FillWidth().Gap(theme.TreeRowPitch - theme.HeightControl).Children(func() {
							for _, row := range rows {
								if renderTreeRow(c, p, row, treeFocus) {
									anyDragOver = true
								}
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

				if p.Notice != "" {
					noticeBar := ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).MaxHeight(48).
						Padding(4, 20, 4, 20).Background(pal.Sidebar)
					noticeBar.Children(func() {
						blockui.Scroll(c).MaxHeight(40).Children(func() {
							ui.Text(c, p.Notice).FontSize(theme.FontMeta).TextColor(pal.TextSecondary)
						})
					})
				}
			})

			// 3. Footer: Notebook Switcher + Updater Badge + Settings Button.
			ui.Row(c).FillWidth().Padding(6, 8, 6, 8).Gap(4).AlignItems(ui.Center).
				BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Background(pal.Sidebar).Children(func() {
				nbTitle := theme.Tr("选择笔记本", "Select Notebook")
				if p.ActiveNotebook != "" {
					nbTitle = noteDisplayTitle(p.ActiveNotebook, true)
				}
				c.Key("notebook-switcher")
				sw := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(30).PaddingX(8).Gap(7).
					AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
					Label(nbTitle)
				swHovered := sw.Hovered()
				swPressed := sw.Pressed()
				swBG := ui.Transparent
				swBD := ui.Transparent
				if p.State.SwitcherOpen {
					swBG = pal.Selected
					swBD = pal.Accent
				} else if swPressed {
					swBG = pal.Selected
				} else if swHovered {
					swBG = pal.Hover
				}
				blockui.AnimateSurfaceColors(sw, swBG, swBD, true)
				blockui.TactileCard(sw, swHovered, swPressed, p.State.SwitcherOpen, false)
				sw.Children(func() {
					ic := pal.MonoIconColors(pal.Text)
					if p.State.SwitcherOpen {
						ic = pal.DuoIconColors(pal.Accent)
					} else if swHovered || swPressed {
						ic = pal.DuoIconColors(pal.Text)
					}
					icons.DrawAnimated(c, icons.FolderOpen, theme.IconRow, ic, swHovered || swPressed || p.State.SwitcherOpen)
					ui.Text(c, nbTitle).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
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
						icons.Draw(c, icons.ChevronUp, theme.IconInline, pal.MonoIconColors(pal.TextSecondary))
					})
				})
				blockui.Probe(sw, "notebook-switcher")
				if b := sw.Bounds(); b.W > 0 {
					p.State.SwitcherBounds = b
				}
				if sw.Clicked() {
					next := !p.State.SwitcherOpen
					p.State.ClosePopups()
					p.State.SwitcherOpen = next
				}

				if p.HasUpdate {
					c.Key("updater-badge-button")
					upBtn := ui.ButtonBase(c).Size(30, 30).Shrink(0).Center().
						Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
						Label(theme.Tr("有可用更新", "Update available"))
					upHover := upBtn.Hovered()
					upPress := upBtn.Pressed()
					upBG := pal.Paper
					if upPress {
						upBG = pal.Selected
					} else if upHover {
						upBG = pal.Hover
					}
					blockui.AnimateSurfaceColors(upBtn, upBG, pal.BtnBorder, true)
					blockui.TactileButton(upBtn, upHover, upPress, false)
					blockui.Tooltip(c, upBtn, theme.Tr("有可用更新", "Update available"))
					upBtn.Children(func() {
						ic := pal.MonoIconColors(pal.TextSecondary)
						if upHover || upPress {
							ic = pal.DuoIconColors(pal.Text)
						}
						icons.DrawAnimated(c, icons.Download, theme.IconChrome, ic, upHover || upPress)
						ui.Box(c).Absolute().Top(3).Right(3).Size(5, 5).Radius(theme.RadiusPill).Background(pal.Accent)
					})
					blockui.Probe(upBtn, "updater-badge-button")
					if upBtn.Clicked() && p.OnOpenUpdater != nil {
						p.OnOpenUpdater()
					}
				}

				ui.Box(c).Size(1, 16).Shrink(0).Background(pal.SidebarBorder)

				setBtn := blockui.IconButton(c, icons.Sliders, blockui.IconButtonOptions{
					ID:       "settings-button",
					Size:     30,
					Tooltip:  theme.Tr("设置", "Settings"),
					Shortcut: blockui.FormatShortcutDisplay("cmd-,"),
					Active:   p.SettingsOpen,
				})
				if setBtn.Clicked() && p.OnOpenSettings != nil {
					p.OnOpenSettings()
				}
			})
		})

		// Right-edge resize splitter.
		if p.Width != nil {
			if blockui.ResizeHandle(c, "sidebar-resize", p.Width, SidebarMinW, SidebarMaxW, false, theme.SidebarDefaultWidth) {
				if p.OnWidthChanged != nil {
					p.OnWidthChanged(*p.Width)
				}
			}
		} else {
			ui.Box(c).Width(1).FillHeight().Background(pal.SidebarBorder)
		}
	})

	renderSidebarPopups(c, p)
}

func renderTreeRow(c *ui.Context, p SidebarProps, r flatTreeRow, treeFocus ui.Element) bool {
	pal := theme.Ocean()
	entry := r.Entry
	indent := theme.TreeRowPadLeft + float32(r.Depth)*theme.TreeIndentStep
	rowKey := "tree-row:" + entry.Path

	if p.State.RenamingPath == entry.Path {
		c.Key("tree-rename:" + entry.Path)
		box := ui.Row(c).FillWidth().Height(theme.HeightControl).Padding(0, 8, 0, indent).Gap(6).AlignItems(ui.Center).
			Radius(theme.RadiusMD).Background(pal.Paper).Border(1, pal.Accent)
		box.Children(func() {
			glyph := fileEntryIcon(filepath.Base(entry.Path))
			if entry.IsDir {
				glyph = icons.Folder
			}
			icons.Draw(c, glyph, theme.IconRow, pal.DuoIconColors(pal.Accent))
			c.Key("tree-rename-input")
			inp := ui.TextInput(c, &p.State.RenameBuffer).Grow(1).FontSize(theme.FontBody).TextColor(pal.Text).
				BorderWidth(0).Background(ui.Transparent).Padding(0)
			if p.State.RenameFocus {
				inp.Focus()
				p.State.RenameFocus = false
			}
			blockui.Probe(inp, "tree-rename-input")
			if inp.Submitted() {
				trimmed := strings.TrimSpace(p.State.RenameBuffer)
				targetPath := entry.Path
				p.State.CancelRename()
				if trimmed != "" && p.OnRenameItem != nil {
					p.OnRenameItem(targetPath, trimmed)
				}
			} else if inp.Shortcut(0, ui.KeyEscape) {
				p.State.CancelRename()
			}
		})
		if box.PressedOutside() {
			trimmed := strings.TrimSpace(p.State.RenameBuffer)
			targetPath := entry.Path
			p.State.CancelRename()
			if trimmed != "" && p.OnRenameItem != nil {
				p.OnRenameItem(targetPath, trimmed)
			}
		}
		return false
	}

	selected := !entry.IsDir && entry.Path == p.ActiveNote
	focused := entry.IsDir && filepath.Clean(p.State.FocusDir) == filepath.Clean(entry.Path)
	activeFolder := focused && p.ActiveNote == ""
	highlighted := selected || activeFolder
	menuTarget := p.State.ContextOpen && p.State.ContextPath == entry.Path

	c.Key(rowKey)
	btn := ui.ButtonBase(c).FillWidth().Height(theme.HeightControl).Padding(0, 8, 0, indent).Gap(6).
		AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
		FocusRing(false).Role(ui.RoleTreeItem).Selected(highlighted).Label(entry.Name)
	if r.Depth > 0 {
		btn.Transition(blockui.TreeRevealTransition(r.RevealOrder))
	}
	btn.Drag(treeDragItem{Path: entry.Path, IsDir: entry.IsDir})
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

	hovered := btn.Hovered()
	pressed := btn.Pressed()
	rowBG := ui.Transparent
	rowBD := ui.Transparent
	switch {
	case isDropInto:
		rowBG = pal.Selected
		rowBD = pal.Accent
	case isDropReorder:
		rowBG = pal.Hover
	case highlighted:
		rowBG = pal.Selected
		rowBD = pal.Accent
	case focused || pressed:
		rowBG = pal.Selected
	case menuTarget || hovered:
		rowBG = pal.Hover
	}
	blockui.AnimateSurfaceColors(btn, rowBG, rowBD, true)
	if pressed {
		sc := btn.AnimateTo("trSc", 0.98, blockui.SpringTactile)
		if sc != 1 {
			btn.Scale(sc, sc)
		}
	} else {
		sc := btn.AnimateTo("trSc", 1.0, blockui.SpringTactile)
		if sc != 1 {
			btn.Scale(sc, sc)
		}
	}

	var moreClicked bool
	var moreBounds ui.Rect
	btn.Children(func() {
		if entry.IsDir {
			chevTarget := float32(0)
			if r.Expanded {
				chevTarget = 90
			}
			chevDeg := btn.AnimateTo("dirChev", chevTarget, blockui.SpringTactile)
			chevBox := ui.Box(c).Size(theme.IconRow, theme.IconRow).Shrink(0).Center()
			if chevDeg != 0 {
				chevBox.Rotate(chevDeg)
			}
			chevBox.Children(func() {
				icons.Draw(c, icons.TbChevronRight, theme.IconInline, pal.MonoIconColors(pal.TextSecondary))
			})
			folderGlyph := icons.Folder
			if r.Expanded {
				folderGlyph = icons.FolderOpen
			}
			ic := pal.MonoIconColors(pal.TextSecondary)
			if highlighted {
				ic = pal.DuoIconColors(pal.Accent)
			} else if hovered || menuTarget {
				ic = pal.DuoIconColors(pal.Text)
			}
			icons.DrawAnimated(c, folderGlyph, theme.IconRow, ic, hovered || menuTarget)
		} else {
			ic := pal.MonoIconColors(pal.TextSecondary)
			if highlighted {
				ic = pal.DuoIconColors(pal.Accent)
			} else if hovered || menuTarget {
				ic = pal.DuoIconColors(pal.Text)
			}
			icons.DrawAnimated(c, fileEntryIcon(filepath.Base(entry.Path)), theme.IconRow, ic, hovered || menuTarget)
		}
		weight := theme.WeightRegular
		if highlighted {
			weight = theme.WeightControl
		}
		ui.Text(c, entry.Name).FontSize(theme.FontBody).FontWeight(weight).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")

		if hovered || menuTarget {
			moreID := "row-more:" + entry.Path
			c.Key(moreID)
			moreBtn := ui.ButtonBase(c).Size(theme.TreeMoreBtnSize, theme.TreeMoreBtnSize).Shrink(0).Center().
				Radius(theme.RadiusSM).Cursor(ui.CursorPointer).FocusRing(false).
				Label(theme.Tr("更多操作", "More actions"))
			moreHover := moreBtn.Hovered()
			morePress := moreBtn.Pressed()
			moreBG := ui.Transparent
			if menuTarget || moreHover || morePress {
				moreBG = pal.MenuHover
			}
			blockui.AnimateSurfaceColors(moreBtn, moreBG, ui.Transparent, false)
			blockui.TactileButton(moreBtn, moreHover, morePress, false)
			moreBtn.Children(func() {
				ic := pal.MonoIconColors(pal.TextSecondary)
				if menuTarget || moreHover || morePress {
					ic = pal.DuoIconColors(pal.Text)
				}
				icons.DrawAnimated(c, icons.More, theme.IconInline, ic, menuTarget || moreHover || morePress)
			})
			blockui.Probe(moreBtn, moreID)
			moreBounds = moreBtn.Bounds()
			if moreBtn.Clicked() {
				moreClicked = true
			}
		} else {
			ui.Box(c).Size(theme.TreeMoreBtnSize, theme.TreeMoreBtnSize).Shrink(0)
		}
	})
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
		p.State.SwitcherOpen = false
		p.State.MenuCursor, p.State.MoveCursor = -1, -1
		p.State.ContextOpen = true
		p.State.ContextPath = entry.Path
		p.State.ContextIsDir = entry.IsDir
		p.State.ContextX = cx
		p.State.ContextY = cy
		p.State.MoveSubmenuOpen = false
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
	} else if moreClicked {
		cx, cy := moreBounds.X, moreBounds.Y+moreBounds.H+2
		if moreBounds.W <= 0 {
			b := btn.Bounds()
			cx, cy = b.X+b.W-28, b.Y+b.H
		}
		openContextMenu(cx, cy)
	} else if btn.RightClicked() {
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
		if !p.State.SwitcherOpen && !p.State.ContextOpen {
			return
		}
		scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0)

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
			for _, nb := range p.Notebooks {
				choices = append(choices, blockui.MenuItemSpec{OnClick: func() {
					p.State.ClosePopups()
					if nb != p.ActiveNotebook && p.OnSwitchNotebook != nil {
						p.OnSwitchNotebook(nb)
					}
				}})
			}
			choices = append(choices, add)
			blockui.NavigateMenu(card, choices, &p.State.MenuCursor)
			blockui.Probe(card, "notebook-switcher-pop")
			card.Children(func() {
				if p.State.ConfirmForgetNotebook != "" {
					stillValid := false
					for _, nb := range p.Notebooks {
						if nb == p.State.ConfirmForgetNotebook && nb != p.ActiveNotebook {
							stillValid = true
							break
						}
					}
					if !stillValid {
						p.State.ConfirmForgetNotebook = ""
					}
				}
				blockui.Scroll(c).MaxHeight(maxH - theme.MenuPadding*2).Children(func() {
					ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
						for ix, nb := range p.Notebooks {
							ix := ix
							nb := nb
							checked := nb == p.ActiveNotebook
							confirming := !checked && p.State.ConfirmForgetNotebook == nb
							c.Key("nb-item:" + nb)
							row := ui.Row(c).FillWidth().Height(theme.MenuItemHeight).Padding(0, 7, 0, 10).Gap(4).AlignItems(ui.Center).Radius(theme.RadiusMD)
							nbDrop := false
							nbAfter := false
							if len(p.Notebooks) > 1 && p.OnReorderNotebooks != nil {
								if !confirming {
									row.Drag(nbDragItem{Path: nb, Index: ix})
								}
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
							rowHover := row.Hovered()
							kbActive := ix == p.State.MenuCursor
							nbBG := ui.Transparent
							switch {
							case checked:
								nbBG = pal.Selected
							case confirming || rowHover || kbActive || nbDrop:
								nbBG = pal.MenuHover
							}
							blockui.AnimateSurfaceColors(row, nbBG, ui.Transparent, false)
							row.Transition(blockui.CascadeRowTransition(ix))
							row.Children(func() {
								sel := ui.ButtonBase(c).Grow(1).MinWidth(0).FillHeight().Gap(8).AlignItems(ui.Center).
									Cursor(ui.CursorPointer).Label(noteDisplayTitle(nb, true))
								sel.Children(func() {
									ic := pal.MonoIconColors(pal.MenuIcon)
									actIco := checked || confirming || rowHover || kbActive
									if actIco {
										ic = pal.DuoIconColors(pal.MenuIcon)
									}
									icons.DrawAnimated(c, icons.Folder, theme.IconRow, ic, rowHover || kbActive)
									ui.Text(c, noteDisplayTitle(nb, true)).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
									if checked {
										ui.Box(c).Size(theme.TreeMoreBtnSize, theme.TreeMoreBtnSize).Shrink(0).Center().Children(func() {
											icons.Draw(c, icons.Check, theme.IconMenuCheck, pal.MonoIconColors(pal.Text))
										})
									}
								})
								blockui.Probe(sel, "nb-select:"+nb)
								if sel.Clicked() {
									if confirming {
										p.State.ConfirmForgetNotebook = ""
									} else {
										choices[ix].OnClick()
									}
								}
								if !checked && p.OnForgetNotebook != nil {
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
						if len(p.Notebooks) > 0 {
							ui.Box(c).FillWidth().Height(1).MarginY(3).Background(pal.MenuSeparator)
						}
						blockui.MenuRow(c, add, p.State.MenuCursor == len(p.Notebooks))
					})
				})
			})
			if card.PressedOutside() {
				p.State.ClosePopups()
			} else if scrim.OverlayShortcut(0, ui.KeyEscape) {
				if p.State.ConfirmForgetNotebook != "" {
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
			icUp := icons.ArrowUp
			icDown := icons.ArrowDown
			icTrash := icons.Trash

			sibs := findSameKindSiblings(p.Entries, targetPath)
			sibIdx := -1
			for i, s := range sibs {
				if filepath.Clean(s.Path) == filepath.Clean(targetPath) {
					sibIdx = i
					break
				}
			}

			var items []blockui.MenuItemSpec
			if targetPath == "" {
				items = blankTreeMenuItems(p)
			} else {
				if !targetDir {
					items = append(items,
						blockui.MenuItemSpec{
							ID:       "open-new-tab",
							Label:    theme.Tr("在新标签页打开", "Open in New Tab"),
							Shortcut: theme.Tr("⌘+点击", "⌘+Click"),
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
				}
				items = append(items,
					blockui.MenuItemSpec{
						ID:    "reveal",
						Label: theme.Tr("在访达中显示", "Reveal in File Manager"),
						Icon:  &icReveal,
						OnClick: func() {
							p.State.ClosePopups()
							if p.OnRevealItem != nil {
								p.OnRevealItem(targetPath)
							}
						},
					},
					blockui.MenuItemSpec{
						ID:       "rename",
						Label:    theme.Tr("重命名", "Rename"),
						Shortcut: "↵",
						Icon:     &icRename,
						OnClick: func() {
							p.State.StartRename(targetPath, targetDir)
						},
					},
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
				if len(sibs) > 1 && p.OnReorderSibling != nil {
					canUp := sibIdx > 0
					canDown := sibIdx >= 0 && sibIdx+1 < len(sibs)
					items = append(items,
						blockui.MenuItemSpec{
							ID:       "tree-reorder-up",
							Label:    theme.Tr("上移", "Move Up"),
							Icon:     &icUp,
							Disabled: !canUp,
							OnClick: func() {
								p.State.ClosePopups()
								if canUp {
									p.OnReorderSibling(filepath.Dir(targetPath), targetPath, sibs[sibIdx-1].Path, false)
								}
							},
						},
						blockui.MenuItemSpec{
							ID:       "tree-reorder-down",
							Label:    theme.Tr("下移", "Move Down"),
							Icon:     &icDown,
							Disabled: !canDown,
							OnClick: func() {
								p.State.ClosePopups()
								if canDown {
									p.OnReorderSibling(filepath.Dir(targetPath), targetPath, sibs[sibIdx+1].Path, true)
								}
							},
						},
					)
				}
				items = append(items,
					blockui.MenuItemSpec{Separator: true},
					blockui.MenuItemSpec{
						ID:       "trash",
						Label:    theme.Tr("移到废纸篓", "Move to Trash"),
						Shortcut: blockui.FormatShortcutDisplay("cmd-backspace"),
						Icon:     &icTrash,
						Danger:   true,
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
			mainCard := blockui.MenuSurface(blockui.Scroll(c).Column().Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
			if !p.State.MoveSubmenuOpen {
				blockui.NavigateMenu(mainCard, items, &p.State.MenuCursor)
			}
			blockui.Probe(mainCard, "tree-menu-pop")
			mainCard.Children(func() {
				for i, it := range items {
					el := blockui.MenuRow(c, it, i == p.State.MenuCursor || it.ID == "tree-move-row" && p.State.MoveSubmenuOpen)
					if it.ID == "tree-move-row" {
						if b := el.Bounds(); b.W > 0 {
							p.State.MoveRowBounds = b
						}
						if el.Hovered() && !p.State.MoveSubmenuOpen {
							p.State.MoveSubmenuOpen = true
							p.State.MoveCursor = -1
						}
					} else if !it.Separator && el.Hovered() && p.State.MoveSubmenuOpen {
						p.State.MoveSubmenuOpen = false
						p.State.MoveCursor = -1
					}
				}
			})

			var subCard ui.Element
			hasSub := p.State.MoveSubmenuOpen
			if hasSub {
				dests := collectMoveDestinations(p.ActiveNotebook, p.Entries, targetPath, targetDir)
				subW := menuW
				subCount := max(len(dests), 1)
				subEstH := theme.MenuPadding*2 + float32(subCount)*theme.MenuItemHeight + float32(max(subCount-1, 0))*theme.MenuGap
				rowRect := p.State.MoveRowBounds
				if rowRect.W <= 0 {
					rowRect = ui.Rect{X: ox, Y: oy + 68, W: menuW, H: theme.MenuItemHeight}
				}
				subSpec := blockui.PlaceBelow(rowRect).WithAlignX(blockui.CascadeRight(8))
				subMaxH := min(max(vpH-16, 1), float32(300))
				subPopH := min(subEstH, subMaxH)
				sox, soy := blockui.ResolveOrigin(subSpec, subW, subPopH, vpW, vpH)

				c.Key("tree-move-pop")
				subCard = blockui.MenuSurface(ui.Column(c).Absolute().Left(sox).Top(soy).Width(subW).MaxHeight(subMaxH).Role(ui.RoleMenu))
				blockui.Probe(subCard, "tree-move-pop")
				choices := make([]blockui.MenuItemSpec, len(dests))
				for i, d := range dests {
					choices[i].OnClick = func() {
						p.State.ClosePopups()
						if p.OnMoveItem != nil {
							p.OnMoveItem(targetPath, d.Dir)
						}
					}
				}
				if subCard.OverlayShortcut(0, ui.KeyLeft) {
					p.State.MoveSubmenuOpen = false
					p.State.MoveCursor = -1
				} else {
					blockui.NavigateMenu(subCard, choices, &p.State.MoveCursor)
				}

				subCard.Children(func() {
					blockui.Scroll(c).MaxHeight(subMaxH - theme.MenuPadding*2).Children(func() {
						ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
							if len(dests) == 0 {
								ui.Row(c).FillWidth().Height(theme.MenuItemHeight).PaddingX(10).AlignItems(ui.Center).Children(func() {
									ui.Text(c, theme.Tr("没有可移动到的位置", "No destination available")).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
								})
								return
							}
							for i, d := range dests {
								d := d
								glyph := icons.Folder
								if d.IsRoot {
									glyph = icons.FolderOpen
								}
								padL := float32(10 + min(d.Depth, 6)*12)
								c.Key("move-dest:" + d.Dir)
								row := ui.ButtonBase(c).FillWidth().Height(theme.MenuItemHeight).Padding(0, 10, 0, padL).Gap(8).
									AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(d.Label)
								rowHover := row.Hovered() || i == p.State.MoveCursor
								if rowHover {
									row.Background(pal.MenuHover)
								}
								row.Children(func() {
									ic := pal.MonoIconColors(pal.MenuIcon)
									if rowHover {
										ic = pal.DuoIconColors(pal.MenuIcon)
									}
									icons.Draw(c, glyph, theme.IconRow, ic)
									ui.Text(c, d.Label).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
								})
								blockui.Probe(row, "move-dest:"+d.Dir)
								if row.Clicked() {
									p.State.ClosePopups()
									if p.OnMoveItem != nil {
										p.OnMoveItem(targetPath, d.Dir)
									}
								}
							}
						})
					})
				})
			}

			if scrim.OverlayShortcut(0, ui.KeyEscape) {
				if p.State.MoveSubmenuOpen {
					p.State.MoveSubmenuOpen = false
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
			Label: theme.Tr("在访达中显示", "Reveal in File Manager"),
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
