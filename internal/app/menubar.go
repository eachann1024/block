package app

import (
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo"
)

// MenuActions carries callbacks invoked from the native application menu bar.
type MenuActions struct {
	OnAbout          func()
	OnPerformance    func()
	OnSettings       func()
	OnNewNote        func()
	OnNewFolder      func()
	OnNewTab         func()
	OnQuickNote      func()
	OnSearch         func()
	OnExportImage    func()
	OnCloseTab       func()
	OnReopenTab      func()
	OnFind           func()
	OnToggleSidebar  func()
	OnToggleAIPanel  func()
	OnZoomReset      func()
	OnZoomIn         func()
	OnZoomOut        func()
	OnNextTab        func()
	OnPrevTab        func()
	OnNavigateBack   func()
	OnNavigateFwd    func()
}

// BuildMenuBar builds and returns the native application menu bar matching
// thinkly-fast/src/menu_bar.rs 1:1 (Block, File, Edit, View, Window).
func BuildMenuBar(shortcuts map[string]string, act MenuActions) *mygo.Menu {
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

	appSubmenu := []*mygo.MenuItem{
		{ID: "menu-about", Label: theme.Tr("关于 Block", "About Block"), Click: call(act.OnAbout)},
		{ID: "menu-perf", Label: theme.Tr("性能与占用", "Performance & Usage"), Accelerator: accel("performance", "cmd-shift-m"), Click: call(act.OnPerformance)},
		mygo.Separator(),
		{ID: "menu-settings", Label: theme.Tr("偏好设置…", "Settings…"), Accelerator: accel("settings", "cmd-,"), Click: call(act.OnSettings)},
		mygo.Separator(),
		{Role: mygo.RoleServices, Label: theme.Tr("服务", "Services")},
		mygo.Separator(),
		{Role: mygo.RoleHide, Label: theme.Tr("隐藏 Block", "Hide Block")},
		{Role: mygo.RoleHideOthers, Label: theme.Tr("隐藏其他", "Hide Others")},
		{Role: mygo.RoleUnhide, Label: theme.Tr("全部显示", "Show All")},
		mygo.Separator(),
		{Role: mygo.RoleQuit, Label: theme.Tr("退出 Block", "Quit Block")},
	}

	fileSubmenu := []*mygo.MenuItem{
		{ID: "menu-new-note", Label: theme.Tr("新建笔记", "New Note"), Accelerator: accel("new_note", "cmd-n"), Click: call(act.OnNewNote)},
		{ID: "menu-new-folder", Label: theme.Tr("新建文件夹", "New Folder"), Accelerator: accel("new_folder", "cmd-shift-n"), Click: call(act.OnNewFolder)},
		{ID: "menu-new-tab", Label: theme.Tr("新建标签页", "New Tab"), Accelerator: accel("new_tab", "cmd-t"), Click: call(act.OnNewTab)},
		mygo.Separator(),
		{ID: "menu-quick-note", Label: theme.Tr("速记小窗", "Quick Note"), Accelerator: accel("quick_note", "alt-n"), Click: call(act.OnQuickNote)},
		{ID: "menu-search", Label: theme.Tr("搜索…", "Search…"), Accelerator: accel("search", "cmd-k"), Click: call(act.OnSearch)},
		mygo.Separator(),
		{ID: "menu-export-image", Label: theme.Tr("导出图片…", "Export Image…"), Accelerator: accel("export_image", "cmd-shift-e"), Click: call(act.OnExportImage)},
		mygo.Separator(),
		{ID: "menu-reopen-tab", Label: theme.Tr("重新打开已关闭标签页", "Reopen Closed Tab"), Accelerator: accel("reopen_tab", "cmd-shift-t"), Click: call(act.OnReopenTab)},
		{ID: "menu-close-tab", Label: theme.Tr("关闭标签页", "Close Tab"), Accelerator: accel("close_tab", "cmd-w"), Click: call(act.OnCloseTab)},
	}

	editSubmenu := []*mygo.MenuItem{
		{Role: mygo.RoleUndo, Label: theme.Tr("撤销", "Undo")},
		{Role: mygo.RoleRedo, Label: theme.Tr("重做", "Redo")},
		mygo.Separator(),
		{Role: mygo.RoleCut, Label: theme.Tr("剪切", "Cut")},
		{Role: mygo.RoleCopy, Label: theme.Tr("拷贝", "Copy")},
		{Role: mygo.RolePaste, Label: theme.Tr("粘贴", "Paste")},
		{Role: mygo.RoleSelectAll, Label: theme.Tr("全选", "Select All")},
		mygo.Separator(),
		{ID: "menu-find", Label: theme.Tr("查找…", "Find…"), Accelerator: accel("find", "cmd-f"), Click: call(act.OnFind)},
	}

	viewSubmenu := []*mygo.MenuItem{
		{ID: "menu-toggle-sidebar", Label: theme.Tr("显示或隐藏侧栏", "Toggle Sidebar"), Accelerator: accel("toggle_sidebar", "alt-b"), Click: call(act.OnToggleSidebar)},
		{ID: "menu-toggle-ai", Label: theme.Tr("AI 面板", "AI Panel"), Accelerator: accel("toggle_ai", "cmd-j"), Click: call(act.OnToggleAIPanel)},
		mygo.Separator(),
		{ID: "menu-zoom-reset", Label: theme.Tr("实际大小 (100%)", "Actual Size (100%)"), Accelerator: "CmdOrCtrl+0", Click: call(act.OnZoomReset)},
		{ID: "menu-zoom-in", Label: theme.Tr("放大", "Zoom In"), Accelerator: "CmdOrCtrl+=", Click: call(act.OnZoomIn)},
		{ID: "menu-zoom-out", Label: theme.Tr("缩小", "Zoom Out"), Accelerator: "CmdOrCtrl+-", Click: call(act.OnZoomOut)},
		mygo.Separator(),
		{Role: mygo.RoleToggleFullScreen, Label: theme.Tr("进入或退出全屏幕", "Toggle Full Screen")},
	}

	windowSubmenu := []*mygo.MenuItem{
		{Role: mygo.RoleMinimize, Label: theme.Tr("最小化", "Minimize")},
		{Role: mygo.RoleZoom, Label: theme.Tr("缩放", "Zoom")},
		mygo.Separator(),
		{ID: "menu-next-tab", Label: theme.Tr("下一个标签页", "Next Tab"), Accelerator: "Ctrl+Tab", Click: call(act.OnNextTab)},
		{ID: "menu-prev-tab", Label: theme.Tr("上一个标签页", "Previous Tab"), Accelerator: "Ctrl+Shift+Tab", Click: call(act.OnPrevTab)},
		mygo.Separator(),
		{ID: "menu-nav-back", Label: theme.Tr("后退", "Back"), Accelerator: "CmdOrCtrl+[", Click: call(act.OnNavigateBack)},
		{ID: "menu-nav-fwd", Label: theme.Tr("前进", "Forward"), Accelerator: "CmdOrCtrl+]", Click: call(act.OnNavigateFwd)},
	}

	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Block", Submenu: appSubmenu},
		{Label: theme.Tr("文件", "File"), Submenu: fileSubmenu},
		{Label: theme.Tr("编辑", "Edit"), Submenu: editSubmenu},
		{Label: theme.Tr("显示", "View"), Submenu: viewSubmenu},
		{Label: theme.Tr("窗口", "Window"), Submenu: windowSubmenu},
	})
}
