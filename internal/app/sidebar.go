package app

import (
	"fmt"
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

	RenamingPath string
	RenamingDir  bool
	RenameBuffer string
	RenameFocus  bool

	DraggingPath   string
	DraggingIsDir  bool
	DragStartY     float32
	DragActive     bool
	DropTargetPath string
	DropAfter      bool
	DropIntoDir    bool

	SwitcherOpen   bool
	SwitcherBounds ui.Rect

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
	}
}

// ClosePopups closes switcher and tree context menus.
func (s *SidebarUIState) ClosePopups() bool {
	had := s.SwitcherOpen || s.ContextOpen
	s.SwitcherOpen = false
	s.ContextOpen = false
	s.MoveSubmenuOpen = false
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
	s.DraggingPath = ""
	s.DraggingIsDir = false
	s.DragActive = false
	s.DropTargetPath = ""
	s.DropAfter = false
	s.DropIntoDir = false
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

	SearchOpen   bool
	SettingsOpen bool
	HasUpdate    bool

	ShortcutSearch  string
	ShortcutNewNote string

	OnOpenSearch       func()
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
	Entry    model.FileEntry
	Depth    int
	Expanded bool
}

func flattenEntries(entries []model.FileEntry, depth int, expanded map[string]bool, out *[]flatTreeRow) {
	for _, e := range entries {
		exp := e.IsDir && expanded[e.Path]
		*out = append(*out, flatTreeRow{
			Entry:    e,
			Depth:    depth,
			Expanded: exp,
		})
		if exp && len(e.Children) > 0 {
			flattenEntries(e.Children, depth+1, expanded, out)
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

// RenderSidebar renders the Ocean Paper notebook sidebar (header search + new buttons,
// scrollable file tree with inline rename & sibling reorder, footer notebook switcher + settings,
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
			// 1. Header: Search button + New Folder + New Note.
			ui.Row(c).FillWidth().Padding(10, 10, 8, 10).Gap(6).AlignItems(ui.Center).Children(func() {
				c.Key("search-button")
				searchBtn := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(32).PaddingX(10).Gap(6).
					AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
					Background(pal.Paper).Label(theme.Tr("搜索", "Search"))
				borderCol := pal.BtnBorder
				if p.SearchOpen {
					borderCol = pal.Accent
				}
				searchBtn.Border(1, borderCol)
				if searchBtn.Hovered() {
					searchBtn.Background(pal.Hover)
				}
				searchBtn.Children(func() {
					ic := pal.MonoIconColors(pal.TextSecondary)
					if p.SearchOpen || searchBtn.Hovered() {
						ic = pal.DuoIconColors(pal.Text)
					}
					icons.Draw(c, icons.FindSearch, 16, ic)
					ui.Text(c, theme.Tr("搜索", "Search")).FontSize(14).TextColor(pal.TextSecondary).Grow(1).SingleLine().Ellipsis("…")
					ui.Text(c, blockui.FormatShortcutDisplay(nonEmptyOr(p.ShortcutSearch, "cmd-k"))).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
				})
				blockui.Probe(searchBtn, "search-button")
				if searchBtn.Clicked() && p.OnOpenSearch != nil {
					p.OnOpenSearch()
				}

				nfBtn := blockui.IconButton(c, icons.FolderPlus, blockui.IconButtonOptions{
					ID:      "new-folder",
					Size:    32,
					Tooltip: theme.Tr("新建文件夹", "New Folder"),
				})
				if nfBtn.Clicked() && p.OnNewFolder != nil {
					p.OnNewFolder()
				}

				nnBtn := blockui.IconButton(c, icons.NotePlus, blockui.IconButtonOptions{
					ID:      "new-note",
					Size:    32,
					Tooltip: fmt.Sprintf("%s %s", theme.Tr("新建笔记", "New Note"), blockui.FormatShortcutDisplay(nonEmptyOr(p.ShortcutNewNote, "cmd-n"))),
				})
				if nnBtn.Clicked() && p.OnNewNote != nil {
					p.OnNewNote()
				}
			})

			// 2. Scrollable File Tree.
			var rows []flatTreeRow
			flattenEntries(p.Entries, 0, p.State.ExpandedDirs, &rows)

			c.Key("file-tree-root")
			treeScroll := ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Padding(0, 8, 8, 8)
			blockui.Probe(treeScroll, "file-tree-root")
			treeScroll.Children(func() {
				if p.ActiveNotebook == "" {
					ui.Column(c).FillWidth().Padding(28, 12, 16, 12).Gap(12).AlignItems(ui.Center).Children(func() {
						icons.Mascot(c, icons.MascotNoFolder, 112)
						ui.Text(c, theme.Tr("尚未打开笔记本", "No notebook open")).FontSize(14).FontWeight(500).TextColor(pal.TextSecondary)
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
						ui.Text(c, theme.Tr("文件夹为空，点击右上角新建笔记", "Folder is empty")).FontSize(14).TextColor(pal.TextSecondary)
					})
					return
				}

				anyPressed := false
				for _, row := range rows {
					if renderTreeRow(c, p, row) {
						anyPressed = true
					}
				}
				// Commit drag drop on pointer release.
				if p.State.DraggingPath != "" && !anyPressed {
					if p.State.DragActive && p.State.DropTargetPath != "" && p.State.DropTargetPath != p.State.DraggingPath {
						src := p.State.DraggingPath
						dst := p.State.DropTargetPath
						intoDir := p.State.DropIntoDir
						after := p.State.DropAfter
						p.State.ClearDrag()
						if intoDir {
							if p.OnMoveItem != nil {
								p.OnMoveItem(src, dst)
							}
						} else if p.OnReorderSibling != nil && filepath.Dir(src) == filepath.Dir(dst) {
							p.OnReorderSibling(filepath.Dir(src), src, dst, after)
						}
					} else {
						p.State.ClearDrag()
					}
				}
			})

			if p.Notice != "" {
				ui.Row(c).FillWidth().Padding(4, 12, 4, 12).Background(pal.Sidebar).Children(func() {
					ui.Text(c, p.Notice).FontSize(14).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
				})
			}

			// 3. Footer: Notebook Switcher + Updater Badge + Settings Button.
			ui.Row(c).FillWidth().Padding(6, 8, 6, 8).Gap(4).AlignItems(ui.Center).
				BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Background(pal.Sidebar).Children(func() {
				nbTitle := theme.Tr("选择笔记本", "Select Notebook")
				if p.ActiveNotebook != "" {
					nbTitle = noteDisplayTitle(p.ActiveNotebook, true)
				}
				c.Key("notebook-switcher")
				sw := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(30).PaddingX(8).Gap(7).
					AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
					Label(nbTitle)
				if p.State.SwitcherOpen {
					sw.Background(pal.Selected).Border(1, pal.Accent)
				} else if sw.Hovered() {
					sw.Background(pal.Hover).Border(1, ui.Transparent)
				} else {
					sw.Border(1, ui.Transparent)
				}
				sw.Children(func() {
					ic := pal.MonoIconColors(pal.Text)
					if p.State.SwitcherOpen || sw.Hovered() {
						ic = pal.DuoIconColors(pal.Text)
					}
					icons.Draw(c, icons.FolderOpen, 16, ic)
					ui.Text(c, nbTitle).FontSize(14).FontWeight(500).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					icons.Draw(c, icons.ChevronUp, 14, pal.MonoIconColors(pal.TextSecondary))
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
					upBtn := blockui.IconButton(c, icons.Download, blockui.IconButtonOptions{
						ID:      "updater-badge-button",
						Size:    30,
						Tooltip: theme.Tr("有可用更新", "Update available"),
						Active:  true,
					})
					if upBtn.Clicked() && p.OnOpenUpdater != nil {
						p.OnOpenUpdater()
					}
				}

				ui.Box(c).Size(1, 16).Shrink(0).Background(pal.SidebarBorder)

				setBtn := blockui.IconButton(c, icons.Sliders, blockui.IconButtonOptions{
					ID:      "settings-button",
					Size:    30,
					Tooltip: fmt.Sprintf("%s %s", theme.Tr("设置", "Settings"), blockui.FormatShortcutDisplay("cmd-,")),
					Active:  p.SettingsOpen,
				})
				if setBtn.Clicked() && p.OnOpenSettings != nil {
					p.OnOpenSettings()
				}
			})
		})

		// Right-edge resize splitter.
		if p.Width != nil {
			if blockui.VerticalResizeHandle(c, "sidebar-resize", p.Width, SidebarMinW, SidebarMaxW, false) {
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

func renderTreeRow(c *ui.Context, p SidebarProps, r flatTreeRow) bool {
	pal := theme.Ocean()
	entry := r.Entry
	indent := float32(8 + min(r.Depth, 8)*14)
	rowKey := "tree-row:" + entry.Path

	if p.State.RenamingPath == entry.Path {
		c.Key("tree-rename:" + entry.Path)
		box := ui.Row(c).FillWidth().Height(32).Padding(0, 8, 0, indent).Gap(6).AlignItems(ui.Center).
			Radius(theme.RadiusControl).Background(pal.Paper).Border(1, pal.Accent)
		box.Children(func() {
			glyph := fileEntryIcon(filepath.Base(entry.Path))
			if entry.IsDir {
				glyph = icons.Folder
			}
			icons.Draw(c, glyph, 16, pal.DuoIconColors(pal.Accent))
			c.Key("tree-rename-input")
			inp := ui.TextInput(c, &p.State.RenameBuffer).Grow(1).FontSize(14).TextColor(pal.Text).
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
	menuTarget := p.State.ContextOpen && p.State.ContextPath == entry.Path
	isDropTarget := p.State.DragActive && p.State.DropTargetPath == entry.Path

	c.Key(rowKey)
	btn := ui.ButtonBase(c).FillWidth().Height(32).Padding(0, 8, 0, indent).Gap(6).
		AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
		Role(ui.RoleTreeItem).Selected(selected).Label(entry.Name)
	hovered := btn.Hovered()
	pressed := btn.Pressed()

	switch {
	case isDropTarget && p.State.DropIntoDir:
		btn.Background(pal.Selected).Border(1, pal.Accent)
	case isDropTarget:
		btn.Background(pal.Hover).Border(1, ui.Transparent)
	case selected:
		btn.Background(pal.Selected).Border(1, ui.Transparent)
	case menuTarget:
		btn.Background(pal.Hover).Border(1, pal.Accent)
	case hovered:
		btn.Background(pal.Hover).Border(1, ui.Transparent)
	default:
		btn.Border(1, ui.Transparent)
	}

	// Track drag-and-drop sibling reorder & folder move.
	if pressed {
		b := btn.Bounds()
		_, py, hasPos := btn.PointerPosition()
		absY := b.Y + py
		if p.State.DraggingPath == "" {
			p.State.DraggingPath = entry.Path
			p.State.DraggingIsDir = entry.IsDir
			p.State.DragStartY = absY
		} else if hasPos && (absY-p.State.DragStartY > 6 || p.State.DragStartY-absY > 6) {
			p.State.DragActive = true
		}
	}
	if p.State.DragActive && hovered && entry.Path != p.State.DraggingPath {
		b := btn.Bounds()
		_, py, _ := btn.PointerPosition()
		if entry.IsDir && (filepath.Dir(p.State.DraggingPath) != filepath.Dir(entry.Path) || (py > 8 && py < b.H-8)) {
			p.State.DropTargetPath = entry.Path
			p.State.DropIntoDir = true
			p.State.DropAfter = false
		} else if filepath.Dir(p.State.DraggingPath) == filepath.Dir(entry.Path) && p.State.DraggingIsDir == entry.IsDir {
			p.State.DropTargetPath = entry.Path
			p.State.DropIntoDir = false
			p.State.DropAfter = py >= b.H*0.5
		}
	}

	btn.Children(func() {
		if entry.IsDir {
			chev := icons.ChevronRight
			if r.Expanded {
				chev = icons.ChevronDown
			}
			icons.Draw(c, chev, 13, pal.MonoIconColors(pal.TextSecondary))
			folderGlyph := icons.Folder
			if r.Expanded {
				folderGlyph = icons.FolderOpen
			}
			ic := pal.MonoIconColors(pal.TextSecondary)
			if hovered || r.Expanded {
				ic = pal.DuoIconColors(pal.Text)
			}
			icons.Draw(c, folderGlyph, 16, ic)
		} else {
			ic := pal.MonoIconColors(pal.TextSecondary)
			if selected || hovered {
				ic = pal.DuoIconColors(pal.Text)
			}
			icons.Draw(c, fileEntryIcon(filepath.Base(entry.Path)), 16, ic)
		}
		fg := pal.Text
		weight := 400
		if selected {
			weight = 500
		}
		ui.Text(c, entry.Name).FontSize(14).FontWeight(weight).TextColor(fg).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
	})
	dropLine := isDropTarget && !p.State.DropIntoDir
	dropAfter := p.State.DropAfter
	btn.DrawOver(func(pt *ui.Painter, rect ui.Rect) {
		blockui.RecordBounds(rowKey, rect)
		if dropLine {
			y := rect.Y
			if dropAfter {
				y = rect.Y + rect.H - 2
			}
			pt.Fill(ui.Rect{X: rect.X + indent, Y: y, W: max(rect.W-indent-6, 16), H: 2}, pal.Accent, 1)
		}
	})

	if btn.RightClicked() {
		b := btn.Bounds()
		cx, cy := b.X+24, b.Y+b.H
		if px, py, ok := btn.PointerPosition(); ok {
			cx, cy = b.X+px, b.Y+py
		}
		p.State.ClearDrag()
		p.State.SwitcherOpen = false
		p.State.ContextOpen = true
		p.State.ContextPath = entry.Path
		p.State.ContextIsDir = entry.IsDir
		p.State.ContextX = cx
		p.State.ContextY = cy
		p.State.MoveSubmenuOpen = false
	} else if btn.Clicked() && !p.State.DragActive {
		p.State.ClearDrag()
		p.State.ClosePopups()
		if entry.IsDir {
			p.State.ExpandedDirs[entry.Path] = !p.State.ExpandedDirs[entry.Path]
		} else if p.OnOpenNote != nil {
			inNewTab := (btn.ClickModifiers() & ui.Cmd) != 0
			p.OnOpenNote(entry.Path, inNewTab)
		}
	}
	return pressed
}

func renderSidebarPopups(c *ui.Context, p SidebarProps) {
	if !p.State.SwitcherOpen && !p.State.ContextOpen {
		return
	}
	pal := theme.Ocean()
	vpW, vpH := c.Size()

	ui.Overlay(c, func() {
		scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0)

		if p.State.SwitcherOpen {
			menuW := SidebarDefaultW
			if p.Width != nil && *p.Width > 0 {
				menuW = ClampSidebarWidth(*p.Width) - 16
				if menuW < 220 {
					menuW = 220
				}
			}
			rowCount := len(p.Notebooks) + 1
			estH := float32(12 + rowCount*32)
			if len(p.Notebooks) > 0 {
				estH += 9
			}
			anchor := p.State.SwitcherBounds
			if anchor.W <= 0 {
				anchor = ui.Rect{X: 8, Y: vpH - 42, W: menuW, H: 30}
			}
			spec := blockui.PlaceAbove(anchor)
			maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 340)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

			c.Key("notebook-switcher-pop")
			card := ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).
				Padding(6).Radius(theme.RadiusSurface).Background(pal.Paper).Border(1, pal.Hairline).
				Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.18)).Role(ui.RoleMenu)
			blockui.Probe(card, "notebook-switcher-pop")
			card.Children(func() {
				ui.Scroll(c).MaxHeight(maxH - 12).Children(func() {
					for ix, nb := range p.Notebooks {
						ix := ix
						nb := nb
						checked := nb == p.ActiveNotebook
						c.Key("nb-item:" + nb)
						row := ui.Row(c).FillWidth().Height(32).PaddingX(8).Gap(4).AlignItems(ui.Center).Radius(6)
						if row.Hovered() {
							row.Background(pal.Hover)
						}
						row.Children(func() {
							sel := ui.ButtonBase(c).Grow(1).MinWidth(0).FillHeight().Gap(8).AlignItems(ui.Center).
								Cursor(ui.CursorPointer).Label(noteDisplayTitle(nb, true))
							sel.Children(func() {
								icons.Draw(c, icons.Folder, 16, pal.DuoIconColors(pal.Text))
								ui.Text(c, noteDisplayTitle(nb, true)).FontSize(14).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
								if checked {
									icons.Draw(c, icons.Check, 15, pal.MonoIconColors(pal.Accent))
								}
							})
							blockui.Probe(sel, "nb-select:"+nb)
							if sel.Clicked() {
								p.State.ClosePopups()
								if nb != p.ActiveNotebook && p.OnSwitchNotebook != nil {
									p.OnSwitchNotebook(nb)
								}
							}
							if len(p.Notebooks) > 1 && p.OnReorderNotebooks != nil {
								if ix > 0 {
									upBtn := blockui.IconButton(c, icons.ArrowUp, blockui.IconButtonOptions{
										ID:       "nb-up:" + nb,
										Size:     20,
										IconSize: 12,
										Tooltip:  theme.Tr("上移", "Move Up"),
									})
									if upBtn.Clicked() {
										p.OnReorderNotebooks(ix, ix-1)
									}
								}
								if ix+1 < len(p.Notebooks) {
									dnBtn := blockui.IconButton(c, icons.ArrowDown, blockui.IconButtonOptions{
										ID:       "nb-down:" + nb,
										Size:     20,
										IconSize: 12,
										Tooltip:  theme.Tr("下移", "Move Down"),
									})
									if dnBtn.Clicked() {
										p.OnReorderNotebooks(ix, ix+1)
									}
								}
							}
							if !checked && p.OnForgetNotebook != nil {
								fgBtn := blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{
									ID:       "nb-forget:" + nb,
									Size:     22,
									IconSize: 13,
									Tooltip:  theme.Tr("从列表移除", "Remove from list"),
								})
								if fgBtn.Clicked() {
									p.OnForgetNotebook(nb)
								}
							}
						})
					}
					if len(p.Notebooks) > 0 {
						ui.Box(c).FillWidth().Height(1).MarginY(4).Background(pal.Hairline)
					}
					icPlus := icons.FolderPlus
					blockui.MenuRow(c, blockui.MenuItemSpec{
						ID:    "add-notebook",
						Label: theme.Tr("选择文件夹…", "Choose Folder…"),
						Icon:  &icPlus,
						OnClick: func() {
							p.State.ClosePopups()
							if p.OnChooseNotebook != nil {
								p.OnChooseNotebook()
							}
						},
					}, false)
				})
			})
			if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
				p.State.ClosePopups()
			}
			return
		}

		if p.State.ContextOpen {
			menuW := float32(232)
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
						p.State.MoveSubmenuOpen = !p.State.MoveSubmenuOpen
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

			estH := estimateMenuHeight(items)
			mainSpec := blockui.PlaceAtPoint(p.State.ContextX, p.State.ContextY)
			maxH := blockui.MaxHeightForSpec(mainSpec, vpW, vpH, 380)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(mainSpec, menuW, popH, vpW, vpH)

			c.Key("tree-menu-pop")
			mainCard := ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).
				Padding(6).Radius(theme.RadiusSurface).Background(pal.Paper).Border(1, pal.Hairline).
				Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.18)).Role(ui.RoleMenu)
			blockui.Probe(mainCard, "tree-menu-pop")
			mainCard.Children(func() {
				for _, it := range items {
					el := blockui.MenuRow(c, it, it.ID == "tree-move-row" && p.State.MoveSubmenuOpen)
					if it.ID == "tree-move-row" {
						if b := el.Bounds(); b.W > 0 {
							p.State.MoveRowBounds = b
						}
						if el.Hovered() {
							p.State.MoveSubmenuOpen = true
						}
					} else if !it.Separator && el.Hovered() {
						p.State.MoveSubmenuOpen = false
					}
				}
			})

			var subCard ui.Element
			hasSub := p.State.MoveSubmenuOpen
			if hasSub {
				dests := collectMoveDestinations(p.ActiveNotebook, p.Entries, targetPath, targetDir)
				subW := float32(232)
				subCount := max(len(dests), 1)
				subEstH := float32(12 + subCount*32)
				rowRect := p.State.MoveRowBounds
				if rowRect.W <= 0 {
					rowRect = ui.Rect{X: ox, Y: oy + 68, W: menuW, H: 32}
				}
				subSpec := blockui.PlaceBelow(rowRect).WithAlignX(blockui.CascadeRight(8))
				subMaxH := min(blockui.MaxHeightForSpec(subSpec, vpW, vpH, 300), float32(300))
				subPopH := min(subEstH, subMaxH)
				sox, soy := blockui.ResolveOrigin(subSpec, subW, subPopH, vpW, vpH)

				c.Key("tree-move-pop")
				subCard = ui.Column(c).Absolute().Left(sox).Top(soy).Width(subW).MaxHeight(subMaxH).
					Padding(6).Radius(theme.RadiusSurface).Background(pal.Paper).Border(1, pal.Hairline).
					Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.18)).Role(ui.RoleMenu)
				blockui.Probe(subCard, "tree-move-pop")
				subCard.Children(func() {
					ui.Scroll(c).MaxHeight(subMaxH - 12).Children(func() {
						if len(dests) == 0 {
							ui.Row(c).FillWidth().Height(32).PaddingX(10).AlignItems(ui.Center).Children(func() {
								ui.Text(c, theme.Tr("没有可移动到的位置", "No destination available")).FontSize(14).TextColor(pal.TextSecondary)
							})
							return
						}
						for _, d := range dests {
							d := d
							glyph := icons.Folder
							if d.IsRoot {
								glyph = icons.FolderOpen
							}
							padL := float32(10 + min(d.Depth, 6)*12)
							c.Key("move-dest:" + d.Dir)
							row := ui.ButtonBase(c).FillWidth().Height(32).Padding(0, 10, 0, padL).Gap(8).
								AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer).Label(d.Label)
							if row.Hovered() {
								row.Background(pal.Hover)
							}
							row.Children(func() {
								icons.Draw(c, glyph, 16, pal.DuoIconColors(pal.Text))
								ui.Text(c, d.Label).FontSize(14).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
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
			}

			if scrim.OverlayShortcut(0, ui.KeyEscape) {
				if p.State.MoveSubmenuOpen {
					p.State.MoveSubmenuOpen = false
				} else {
					p.State.ClosePopups()
				}
			} else if mainCard.PressedOutside() && (!hasSub || subCard.PressedOutside()) {
				p.State.ClosePopups()
			}
		}
	})
}
