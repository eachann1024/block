package app

import (
	"runtime"

	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo"
)

// MenuActions carries callbacks invoked from the native application menu bar.
type MenuActions struct {
	OnAbout            func()
	OnCheckUpdates     func()
	OnPerformance      func()
	OnSettings         func()
	OnNewNote          func()
	OnNewFolder        func()
	OnNewTab           func()
	OnOpenFolder       func()
	OnSave             func()
	OnQuickNote        func()
	OnSearch           func()
	OnExportImage      func()
	OnCloseTab         func(*mygo.Window)
	OnReopenTab        func()
	OnFind             func()
	OnFindReplace      func()
	OnCommandPalette   func()
	OnToggleFullScreen func()
	OnToggleSidebar    func()
	OnToggleAIPanel    func()
	OnZoomReset        func()
	OnZoomIn           func()
	OnZoomOut          func()
	OnNextTab          func()
	OnPrevTab          func()
	OnNavigateBack     func()
	OnNavigateFwd      func()
}

// BuildMenuBar builds and returns the native application menu bar for the current OS.
func BuildMenuBar(shortcuts map[string]string, act MenuActions) *mygo.Menu {
	return BuildMenuBarForOS(runtime.GOOS, shortcuts, act)
}

// BuildMenuBarForOS builds a platform-idiomatic native menu bar for darwin vs Windows/Linux.
func BuildMenuBarForOS(goos string, shortcuts map[string]string, act MenuActions) *mygo.Menu {
	accel := func(actionKey, fallback string) string {
		if shortcuts != nil {
			if raw, ok := shortcuts[actionKey]; ok && raw != "" {
				if s := blockui.ToMyGoAccelerator(raw); s != "" {
					return s
				}
			}
		}
		return blockui.ToMyGoAccelerator(fallback)
	}
	call := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			if fn != nil {
				fn()
			}
		}
	}

	isMac := goos == "darwin"

	fileSubmenu := []*mygo.MenuItem{
		{ID: "menu-new-note", Label: theme.Tr("新建笔记", "New Note"), Accelerator: accel("new_note", "cmd-n"), Click: call(act.OnNewNote)},
		{ID: "menu-new-folder", Label: theme.Tr("新建文件夹", "New Folder"), Accelerator: accel("new_folder", "cmd-shift-n"), Click: call(act.OnNewFolder)},
		{ID: "menu-new-tab", Label: theme.Tr("新建标签页", "New Tab"), Accelerator: accel("new_tab", "cmd-t"), Click: call(act.OnNewTab)},
		{ID: "menu-open-folder", Label: theme.Tr("打开文件夹…", "Open Folder…"), Accelerator: accel("open_folder", "cmd-o"), Click: call(act.OnOpenFolder)},
		mygo.Separator(),
		{ID: "menu-save", Label: theme.Tr("保存", "Save"), Accelerator: blockui.ToMyGoAccelerator("cmd-s"), Click: call(act.OnSave)},
		mygo.Separator(),
		{ID: "menu-quick-note", Label: theme.Tr("速记小窗", "Quick Note"), Accelerator: accel("quick_note", "alt-n"), Click: call(act.OnQuickNote)},
		{ID: "menu-search", Label: theme.Tr("搜索…", "Search…"), Accelerator: accel("search", "cmd-k"), Click: call(act.OnSearch)},
		mygo.Separator(),
		{ID: "menu-export-image", Label: theme.Tr("导出图片…", "Export Image…"), Accelerator: accel("export_image", "cmd-shift-e"), Click: call(act.OnExportImage)},
		mygo.Separator(),
		{ID: "menu-reopen-tab", Label: theme.Tr("重新打开已关闭标签页", "Reopen Closed Tab"), Accelerator: accel("reopen_tab", "cmd-shift-t"), Click: call(act.OnReopenTab)},
		{ID: "menu-close-tab", Label: theme.Tr("关闭标签页", "Close Tab"), Accelerator: accel("close_tab", "cmd-w"), Click: func(_ *mygo.MenuItem, win *mygo.Window) {
			if act.OnCloseTab != nil {
				act.OnCloseTab(win)
			}
		}},
	}
	if !isMac {
		fileSubmenu = append(fileSubmenu,
			mygo.Separator(),
			&mygo.MenuItem{ID: "menu-settings", Label: theme.Tr("偏好设置…", "Settings…"), Accelerator: blockui.ToMyGoAccelerator(blockui.SettingsShortcut), Click: call(act.OnSettings)},
			mygo.Separator(),
			&mygo.MenuItem{Role: mygo.RoleQuit, Label: theme.Tr("退出 Block", "Exit"), Accelerator: "Ctrl+Q"},
		)
	}

	editSubmenu := []*mygo.MenuItem{
		{Role: mygo.RoleUndo, Label: theme.Tr("撤销", "Undo")},
		{Role: mygo.RoleRedo, Label: theme.Tr("重做", "Redo")},
		mygo.Separator(),
		{Role: mygo.RoleCut, Label: theme.Tr("剪切", "Cut")},
		{Role: mygo.RoleCopy, Label: theme.Tr("拷贝", "Copy")},
		{Role: mygo.RolePaste, Label: theme.Tr("粘贴", "Paste")},
	}
	if isMac {
		editSubmenu = append(editSubmenu,
			&mygo.MenuItem{Role: mygo.RolePasteAndMatchStyle, Label: theme.Tr("粘贴并匹配样式", "Paste and Match Style")},
		)
	}
	findReplaceAccel := "Ctrl+H"
	if isMac {
		findReplaceAccel = "Command+Alt+F"
	}
	editSubmenu = append(editSubmenu,
		&mygo.MenuItem{Role: mygo.RoleDelete, Label: theme.Tr("删除", "Delete")},
		&mygo.MenuItem{Role: mygo.RoleSelectAll, Label: theme.Tr("全选", "Select All")},
		mygo.Separator(),
		&mygo.MenuItem{ID: "menu-find", Label: theme.Tr("查找…", "Find…"), Accelerator: accel("find", "cmd-f"), Click: call(act.OnFind)},
		&mygo.MenuItem{ID: "menu-find-replace", Label: theme.Tr("查找并替换…", "Find and Replace…"), Accelerator: findReplaceAccel, Click: call(act.OnFindReplace)},
	)

	viewSubmenu := []*mygo.MenuItem{
		{ID: "menu-command-palette", Label: theme.Tr("命令面板…", "Command Palette…"), Accelerator: accel("command_palette", blockui.DefaultCommandPaletteShortcut), Click: call(act.OnCommandPalette)},
		mygo.Separator(),
		{ID: "menu-toggle-sidebar", Label: theme.Tr("显示或隐藏侧栏", "Toggle Sidebar"), Accelerator: accel("toggle_sidebar", "alt-b"), Click: call(act.OnToggleSidebar)},
	}
	if act.OnToggleAIPanel != nil {
		viewSubmenu = append(viewSubmenu, &mygo.MenuItem{ID: "menu-toggle-ai", Label: theme.Tr("AI Agent 面板", "AI Agent Panel"), Accelerator: accel("toggle_ai", "cmd-j"), Click: call(act.OnToggleAIPanel)})
	}
	fullScreen := &mygo.MenuItem{Label: theme.Tr("进入或退出全屏幕", "Toggle Full Screen")}
	if isMac {
		fullScreen.Role = mygo.RoleToggleFullScreen
	} else {
		// The workspace handles F11 so a held key toggles only once.
		fullScreen.Click = call(act.OnToggleFullScreen)
	}
	viewSubmenu = append(viewSubmenu,
		mygo.Separator(),
		&mygo.MenuItem{ID: "menu-zoom-reset", Label: theme.Tr("重置缩放", "Reset Zoom"), Accelerator: "CmdOrCtrl+0", Click: call(act.OnZoomReset)},
		&mygo.MenuItem{ID: "menu-zoom-in", Label: theme.Tr("放大", "Zoom In"), Accelerator: "CmdOrCtrl+=", Click: call(act.OnZoomIn)},
		&mygo.MenuItem{ID: "menu-zoom-out", Label: theme.Tr("缩小", "Zoom Out"), Accelerator: "CmdOrCtrl+-", Click: call(act.OnZoomOut)},
		mygo.Separator(),
		fullScreen,
	)

	windowSubmenu := []*mygo.MenuItem{
		{Role: mygo.RoleMinimize, Label: theme.Tr("最小化", "Minimize")},
	}
	if isMac {
		windowSubmenu = append(windowSubmenu, &mygo.MenuItem{Role: mygo.RoleZoom, Label: theme.Tr("缩放", "Zoom")})
	}
	windowSubmenu = append(windowSubmenu,
		mygo.Separator(),
		&mygo.MenuItem{ID: "menu-next-tab", Label: theme.Tr("下一个标签页", "Next Tab"), Accelerator: "Ctrl+Tab", Click: call(act.OnNextTab)},
		&mygo.MenuItem{ID: "menu-prev-tab", Label: theme.Tr("上一个标签页", "Previous Tab"), Accelerator: "Ctrl+Shift+Tab", Click: call(act.OnPrevTab)},
		mygo.Separator(),
		&mygo.MenuItem{ID: "menu-nav-back", Label: theme.Tr("后退", "Back"), Accelerator: "CmdOrCtrl+[", Click: call(act.OnNavigateBack)},
		&mygo.MenuItem{ID: "menu-nav-fwd", Label: theme.Tr("前进", "Forward"), Accelerator: "CmdOrCtrl+]", Click: call(act.OnNavigateFwd)},
	)

	if isMac {
		appSubmenu := []*mygo.MenuItem{
			{ID: "menu-about", Label: theme.Tr("关于 Block", "About Block"), Click: call(act.OnAbout)},
			{ID: "menu-check-updates", Label: theme.Tr("检查更新…", "Check for Updates…"), Click: call(act.OnCheckUpdates)},
			{ID: "menu-perf", Label: theme.Tr("性能与占用", "Performance & Usage"), Accelerator: accel("performance", "cmd-shift-m"), Click: call(act.OnPerformance)},
			mygo.Separator(),
			{ID: "menu-settings", Label: theme.Tr("偏好设置…", "Settings…"), Accelerator: blockui.ToMyGoAccelerator(blockui.SettingsShortcut), Click: call(act.OnSettings)},
			mygo.Separator(),
			{Role: mygo.RoleServices, Label: theme.Tr("服务", "Services")},
			mygo.Separator(),
			{Role: mygo.RoleHide, Label: theme.Tr("隐藏 Block", "Hide Block")},
			{Role: mygo.RoleHideOthers, Label: theme.Tr("隐藏其他", "Hide Others")},
			{Role: mygo.RoleUnhide, Label: theme.Tr("全部显示", "Show All")},
			mygo.Separator(),
			{Role: mygo.RoleQuit, Label: theme.Tr("退出 Block", "Quit Block")},
		}
		return mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Block", Submenu: appSubmenu},
			{Label: theme.Tr("文件", "File"), Submenu: fileSubmenu},
			{Label: theme.Tr("编辑", "Edit"), Submenu: editSubmenu},
			{Label: theme.Tr("显示", "View"), Submenu: viewSubmenu},
			{Label: theme.Tr("窗口", "Window"), Submenu: windowSubmenu},
		})
	}

	helpSubmenu := []*mygo.MenuItem{
		{ID: "menu-perf", Label: theme.Tr("性能与占用", "Performance & Usage"), Accelerator: accel("performance", "cmd-shift-m"), Click: call(act.OnPerformance)},
		{ID: "menu-check-updates", Label: theme.Tr("检查更新…", "Check for Updates…"), Click: call(act.OnCheckUpdates)},
		mygo.Separator(),
		{ID: "menu-about", Label: theme.Tr("关于 Block", "About Block"), Click: call(act.OnAbout)},
	}

	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: theme.Tr("文件", "File"), Submenu: fileSubmenu},
		{Label: theme.Tr("编辑", "Edit"), Submenu: editSubmenu},
		{Label: theme.Tr("显示", "View"), Submenu: viewSubmenu},
		{Label: theme.Tr("窗口", "Window"), Submenu: windowSubmenu},
		{Label: theme.Tr("帮助", "Help"), Submenu: helpSubmenu},
	})
}
