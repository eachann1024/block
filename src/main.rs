mod app;
mod dropdown;
mod editor;
mod export;
mod files;
mod icons;
pub mod log;
mod mascot;
mod quick;
mod search;
mod smoke;
mod theme;
pub mod ui;
pub mod window_bounds;

use std::{path::PathBuf, sync::Mutex, time::Duration};
use gpui::{
    App, AppContext, KeyBinding, Menu, MenuItem, TitlebarOptions,
    WindowOptions, actions, point, px, size,
};

actions!(thinkly_fast, [Quit, ExportImage]);

pub fn sync_ime_cursor_area(_x: f32, _y: f32, _h: f32) {}

static OPEN_QUEUE: Mutex<Vec<PathBuf>> = Mutex::new(Vec::new());

fn url_or_arg_to_path(raw: &str) -> Option<PathBuf> {
    let trimmed = raw.trim();
    if trimmed.is_empty() || trimmed.starts_with('-') { return None; }
    let encoded = trimmed.strip_prefix("file://localhost").or_else(|| trimmed.strip_prefix("file://"))?;
    let mut bytes = Vec::with_capacity(encoded.len());
    let src = encoded.as_bytes();
    let mut i = 0;
    while i < src.len() {
        if src[i] == b'%' && i + 2 < src.len()
            && let Ok(hex) = std::str::from_utf8(&src[i + 1..i + 3])
            && let Ok(b) = u8::from_str_radix(hex, 16)
        {
            bytes.push(b);
            i += 3;
        } else {
            bytes.push(src[i]);
            i += 1;
        }
    }
    Some(PathBuf::from(String::from_utf8_lossy(&bytes).into_owned()))
}

fn enqueue_open_inputs<I: IntoIterator<Item = String>>(inputs: I) {
    let Ok(mut q) = OPEN_QUEUE.lock() else { return };
    for s in inputs {
        let p = url_or_arg_to_path(&s).unwrap_or_else(|| PathBuf::from(s.trim()));
        if !p.as_os_str().is_empty() && !p.to_string_lossy().starts_with('-') {
            q.push(p);
        }
    }
}

fn take_open_queue() -> Vec<PathBuf> {
    OPEN_QUEUE.lock().map(|mut q| std::mem::take(&mut *q)).unwrap_or_default()
}

struct Assets;
impl gpui::AssetSource for Assets {
    fn load(&self, path: &str) -> gpui::Result<Option<std::borrow::Cow<'static, [u8]>>> {
        if let Some(bytes) = mascot::load_asset(path) {
            return Ok(Some(std::borrow::Cow::Borrowed(bytes)));
        }
        icons::Icons.load(path)
    }
    fn list(&self, path: &str) -> gpui::Result<Vec<gpui::SharedString>> { icons::Icons.list(path) }
}

pub fn apply_app_menus(cx: &mut gpui::App) {
    cx.set_menus(vec![Menu {
        name: "Yu".into(),
        items: vec![
            MenuItem::action(theme::tr("新建标签页 ⌘T", "New Tab ⌘T"), app::NewTab),
            MenuItem::action(theme::tr("关闭标签页 ⌘W", "Close Tab ⌘W"), app::CloseTab),
            MenuItem::action(theme::tr("下一个标签页 ⌃Tab", "Next Tab ⌃Tab"), app::NextTab),
            MenuItem::action(theme::tr("上一个标签页 ⌃⇧Tab", "Previous Tab ⌃⇧Tab"), app::PrevTab),
            MenuItem::action(theme::tr("偏好设置… ⌘,", "Settings… ⌘,"), app::OpenSettings),
            MenuItem::action(theme::tr("速记小窗 ⌥N", "Quick Note ⌥N"), quick::ToggleQuickNote),
            MenuItem::action(theme::tr("导出…", "Export…"), ExportImage),
        ],
        disabled: false,
    }]);
}

fn main() {
    log::init();
    let smoke_sandbox = smoke::init_sandbox();
    enqueue_open_inputs(std::env::args().skip(1));
    let initial_state = files::load_state().ok();
    if let Some(state) = &initial_state {
        theme::apply_state(state);
    } else {
        theme::set_language_mode(files::state::LanguageMode::System);
    }
    let saved_main_bounds = initial_state.as_ref().and_then(|s| s.main_window_bounds);
    let saved_quick_bounds = initial_state.as_ref().and_then(|s| s.quick_window_bounds);
    let app = gpui_platform::application().with_assets(Assets);
    app.on_open_urls(|urls| enqueue_open_inputs(urls));
    app.on_reopen(|cx| {
        search::window::activate_main_window(cx);
    });
    app.run(move |cx: &mut App| {
        let smoke_sandbox = smoke_sandbox.clone();
        theme::init_fonts(&cx.text_system().all_font_names());
        gpui_component::init(cx);
        theme::sync_gpui_theme(cx);
        cx.bind_keys([
            KeyBinding::new("cmd-q", Quit, None),
            #[cfg(not(target_os = "macos"))]
            KeyBinding::new("ctrl-q", Quit, None),
        ]);
        editor::bind_keys(cx);
        quick::init(saved_quick_bounds, cx);
        apply_app_menus(cx);
        let min_size = size(px(760.), px(480.));
        let (window_bounds, display_id) = window_bounds::resolve_window_bounds(
            saved_main_bounds,
            size(px(1100.), px(760.)),
            min_size,
            cx,
        );
        let opened = cx.open_window(
            WindowOptions {
                window_bounds: Some(window_bounds),
                display_id,
                window_min_size: Some(min_size),
                titlebar: Some(TitlebarOptions {
                    title: Some("Yu".into()),
                    appears_transparent: true,
                    traffic_light_position: cfg!(target_os = "macos").then_some(point(px(16.), px(13.))),
                    ..Default::default()
                }),
                ..Default::default()
            },
            |window, cx| {
                let workspace = cx.new(|cx| app::Workspace::new(window, cx));
                let close_editor = workspace.read(cx).editor.clone();
                let quit_editor = close_editor.clone();
                window.on_window_should_close(cx, move |window, cx| {
                    let bounds = window_bounds::capture_window_bounds(window, cx);
                    let _ = files::update_state(|s| s.main_window_bounds = Some(bounds));
                    let ready = close_editor.update(cx, |editor, cx| {
                        let ready = editor.prepare_to_quit();
                        if !ready { cx.notify(); }
                        ready
                    });
                    if ready && search::window::hide_main_window_on_close() {
                        return false;
                    }
                    ready
                });
                cx.on_action(move |_: &Quit, cx: &mut App| {
                    let ready = quit_editor.update(cx, |editor, cx| {
                        let ready = editor.prepare_to_quit();
                        if !ready { cx.notify(); }
                        ready
                    });
                    // Both editors must checkpoint before quitting; evaluate the quick one regardless.
                    let quick_ready = quick::prepare_to_quit(cx);
                    if ready && quick_ready { cx.quit(); }
                });
                let menu_target = workspace.clone();
                let settings_target = workspace.clone();
                let tab_target = workspace.clone();
                let open_target = workspace.clone();
                let initial_open = take_open_queue();
                if !initial_open.is_empty() {
                    open_target.update(cx, |w, cx| w.open_external_paths(initial_open, cx));
                }
                cx.spawn(async move |cx| {
                    loop {
                        cx.background_executor().timer(Duration::from_millis(150)).await;
                        let queued = take_open_queue();
                        if !queued.is_empty() {
                            let target = open_target.clone();
                            cx.update(|cx| {
                                search::window::activate_main_window(cx);
                                target.update(cx, |w, cx| w.open_external_paths(queued, cx));
                            });
                        }
                    }
                }).detach();
                if smoke::is_requested() {
                    smoke::spawn(window.window_handle(), workspace.clone(), smoke_sandbox.clone(), cx);
                }
                search::window::set_main_window(window.window_handle().downcast::<gpui_component::Root>(), workspace.clone(), cx);
                cx.on_action({
                    let target = tab_target.clone();
                    move |_: &app::NewTab, cx: &mut App| {
                        let Some(handle) = cx.active_window() else { return };
                        let target = target.clone();
                        let _ = cx.update_window(handle, move |_, window, cx| {
                            target.update(cx, |this, cx| this.new_tab(window, cx));
                        });
                    }
                });
                cx.on_action({
                    let target = tab_target.clone();
                    move |_: &app::CloseTab, cx: &mut App| {
                        target.update(cx, |this, cx| this.close_active_tab(cx));
                    }
                });
                cx.on_action({
                    let target = tab_target.clone();
                    move |_: &app::NextTab, cx: &mut App| {
                        target.update(cx, |this, cx| this.step_tab(1, cx));
                    }
                });
                cx.on_action(move |_: &app::PrevTab, cx: &mut App| {
                    tab_target.update(cx, |this, cx| this.step_tab(-1, cx));
                });
                cx.on_action(move |_: &app::OpenSettings, cx: &mut App| {
                    let Some(handle) = cx.active_window() else { return };
                    let settings_target = settings_target.clone();
                    let _ = cx.update_window(handle, move |_, window, cx| {
                        settings_target.update(cx, |this, cx| {
                            this.open_settings(window, cx);
                        });
                    });
                });
                cx.on_action(move |_: &ExportImage, cx: &mut App| {
                    let Some(handle) = cx.active_window() else { return };
                    let editor = menu_target.read(cx).editor.clone();
                    let _ = cx.update_window(handle, move |_, window, cx| {
                        editor.update(cx, |editor, cx| {
                            crate::editor::export_document(editor, window, cx)
                        });
                    });
                });
                // gpui-component inputs (rename, find) require Root as the window's first layer.
                cx.new(|cx| gpui_component::Root::new(workspace, window, cx))
            },
        );
        if let Err(err) = opened {
            log::error(&format!("failed to open main window: {err:?}"));
            eprintln!("无法打开窗口");
            cx.quit();
            return;
        }
        cx.activate(true);
    });
}
