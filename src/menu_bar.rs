//! Native menu bar. Labels carry no shortcut text: macOS draws the key
//! equivalents itself from the keymap, so writing them twice duplicates them.
use gpui::{App, KeyBinding, Menu, MenuItem, OsAction, SystemMenuType, actions};
use crate::{app, editor, quick, theme::tr};

actions!(menu_bar, [ShowAbout, HideApp, HideOthers, ShowAll, Minimize, Zoom, ToggleFullScreen]);

pub fn init(cx: &mut App) {
    #[cfg(target_os = "macos")]
    cx.bind_keys([
        KeyBinding::new("cmd-h", HideApp, None),
        KeyBinding::new("alt-cmd-h", HideOthers, None),
        KeyBinding::new("cmd-m", Minimize, None),
        KeyBinding::new("ctrl-cmd-f", ToggleFullScreen, None),
    ]);
    cx.on_action(|_: &ShowAbout, cx: &mut App| crate::about::open(cx));
    cx.on_action(|_: &HideApp, cx: &mut App| cx.hide());
    cx.on_action(|_: &HideOthers, cx: &mut App| cx.hide_other_apps());
    cx.on_action(|_: &ShowAll, cx: &mut App| cx.unhide_other_apps());
    cx.on_action(|_: &Minimize, cx: &mut App| with_window(cx, |w| w.minimize_window()));
    cx.on_action(|_: &Zoom, cx: &mut App| with_window(cx, |w| w.zoom_window()));
    cx.on_action(|_: &ToggleFullScreen, cx: &mut App| with_window(cx, |w| w.toggle_fullscreen()));
}

/// AppKit appends its own "Enter Full Screen" to a menu titled "View" (only in
/// English), duplicating ours; it reads this default at launch, so call first.
#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub fn suppress_system_full_screen_item() {
    use cocoa::base::{BOOL, NO, SEL, id, nil, selector};
    use cocoa::foundation::NSString;
    unsafe extern "C" {
        fn objc_msgSend();
        fn objc_getClass(name: *const std::ffi::c_char) -> id;
    }
    unsafe {
        let get: unsafe extern "C" fn(id, SEL) -> id = std::mem::transmute(objc_msgSend as *const ());
        let set: unsafe extern "C" fn(id, SEL, BOOL, id) = std::mem::transmute(objc_msgSend as *const ());
        let defaults = get(objc_getClass(c"NSUserDefaults".as_ptr()), selector("standardUserDefaults"));
        let key = NSString::alloc(nil).init_str("NSFullScreenMenuItemEverywhere");
        set(defaults, selector("setBool:forKey:"), NO, key);
    }
}

fn with_window(cx: &mut App, f: impl FnOnce(&mut gpui::Window) + 'static) {
    let Some(handle) = cx.active_window() else { return };
    let _ = handle.update(cx, move |_, window, _| f(window));
}

pub fn apply(cx: &mut App) {
    let sep = MenuItem::separator;
    cx.set_menus(vec![
        Menu {
            name: "Block".into(),
            items: vec![
                MenuItem::action(tr("关于 Block", "About Block"), ShowAbout),
                sep(),
                MenuItem::action(tr("偏好设置…", "Settings…"), app::OpenSettings),
                sep(),
                MenuItem::os_submenu(tr("服务", "Services"), SystemMenuType::Services),
                sep(),
                MenuItem::action(tr("隐藏 Block", "Hide Block"), HideApp),
                MenuItem::action(tr("隐藏其他", "Hide Others"), HideOthers),
                MenuItem::action(tr("全部显示", "Show All"), ShowAll),
                sep(),
                MenuItem::action(tr("退出 Block", "Quit Block"), crate::Quit),
            ],
            disabled: false,
        },
        Menu {
            name: tr("文件", "File").into(),
            items: vec![
                MenuItem::action(tr("新建笔记", "New Note"), app::NewNote),
                MenuItem::action(tr("新建文件夹", "New Folder"), app::NewFolder),
                MenuItem::action(tr("新建标签页", "New Tab"), app::NewTab),
                sep(),
                MenuItem::action(tr("速记小窗", "Quick Note"), quick::ToggleQuickNote),
                MenuItem::action(tr("搜索…", "Search…"), app::OpenSearch),
                sep(),
                MenuItem::action(tr("导出图片…", "Export Image…"), crate::ExportImage),
                sep(),
                MenuItem::action(tr("关闭标签页", "Close Tab"), app::CloseTab),
            ],
            disabled: false,
        },
        Menu {
            name: tr("编辑", "Edit").into(),
            items: vec![
                MenuItem::os_action(tr("撤销", "Undo"), editor::view::Undo, OsAction::Undo),
                MenuItem::os_action(tr("重做", "Redo"), editor::view::Redo, OsAction::Redo),
                sep(),
                MenuItem::os_action(tr("剪切", "Cut"), editor::view::Cut, OsAction::Cut),
                MenuItem::os_action(tr("拷贝", "Copy"), editor::view::Copy, OsAction::Copy),
                MenuItem::os_action(tr("粘贴", "Paste"), editor::view::Paste, OsAction::Paste),
                MenuItem::os_action(tr("全选", "Select All"), editor::view::SelectAll, OsAction::SelectAll),
                sep(),
                MenuItem::action(tr("查找…", "Find…"), editor::view::Find),
            ],
            disabled: false,
        },
        Menu {
            name: tr("显示", "View").into(),
            items: vec![
                MenuItem::action(tr("显示或隐藏侧栏", "Toggle Sidebar"), app::ToggleSidebar),
                MenuItem::action(tr("AI 面板", "AI Panel"), editor::ai::ToggleAiPanel),
                sep(),
                MenuItem::action(tr("进入或退出全屏幕", "Toggle Full Screen"), ToggleFullScreen),
            ],
            disabled: false,
        },
        Menu {
            name: tr("窗口", "Window").into(),
            items: vec![
                MenuItem::action(tr("最小化", "Minimize"), Minimize),
                MenuItem::action(tr("缩放", "Zoom"), Zoom),
                sep(),
                MenuItem::action(tr("下一个标签页", "Next Tab"), app::NextTab),
                MenuItem::action(tr("上一个标签页", "Previous Tab"), app::PrevTab),
                sep(),
                MenuItem::action(tr("后退", "Back"), app::NavigateBack),
                MenuItem::action(tr("前进", "Forward"), app::NavigateForward),
            ],
            disabled: false,
        },
    ]);
}
