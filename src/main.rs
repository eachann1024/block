mod app;
mod editor;
mod export;
mod files;
mod icons;
mod mascot;
mod quick;
mod theme;

use gpui::{
    App, AppContext, Application, Bounds, KeyBinding, Menu, MenuItem, TitlebarOptions,
    WindowBounds, WindowOptions, actions, point, px, size,
};

actions!(thinkly_fast, [Quit, ExportImage]);

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

fn main() {
    if let Ok(state) = files::load_state() {
        theme::apply_state(&state);
    }
    Application::new().with_assets(Assets).run(|cx: &mut App| {
        theme::init_fonts(&cx.text_system().all_font_names());
        gpui_component::init(cx);
        // The app is light-only; without this, inputs follow macOS dark mode (white text, black field).
        gpui_component::Theme::change(gpui_component::ThemeMode::Light, None, cx);
        cx.bind_keys([KeyBinding::new("secondary-q", Quit, None)]);
        editor::bind_keys(cx);
        quick::init(cx);
        cx.set_menus(vec![Menu {
            name: "Thinkly".into(),
            items: vec![
                MenuItem::action("新建标签页 ⌘T", app::NewTab),
                MenuItem::action("关闭标签页 ⌘W", app::CloseTab),
                MenuItem::action("下一个标签页 ⌃Tab", app::NextTab),
                MenuItem::action("上一个标签页 ⌃⇧Tab", app::PrevTab),
                MenuItem::action("偏好设置… ⌘,", app::OpenSettings),
                MenuItem::action("速记小窗 ⌥N", quick::ToggleQuickNote),
                MenuItem::action("导出图片", ExportImage),
            ],
        }]);
        let bounds = Bounds::centered(None, size(px(1100.), px(760.)), cx);
        let opened = cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                window_min_size: Some(size(px(760.), px(480.))),
                titlebar: Some(TitlebarOptions {
                    title: Some("Thinkly".into()),
                    appears_transparent: true,
                    traffic_light_position: Some(point(px(16.), px(14.))),
                    ..Default::default()
                }),
                ..Default::default()
            },
            |window, cx| {
                let workspace = cx.new(|cx| app::Workspace::new(window, cx));
                let close_editor = workspace.read(cx).editor.clone();
                let quit_editor = close_editor.clone();
                window.on_window_should_close(cx, move |_, cx| {
                    close_editor.update(cx, |editor, cx| {
                        let ready = editor.prepare_to_quit();
                        if !ready { cx.notify(); }
                        ready
                    })
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
                quick::set_main_window(window.window_handle().downcast::<gpui_component::Root>(), workspace.clone(), cx);
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
                    let _ = menu_target.update(cx, |this, cx| {
                        let _ = cx.update_window(handle, |_, window, cx| {
                            this.editor.update(cx, |editor, cx| {
                                crate::editor::export_document(editor, window, cx)
                            });
                        });
                    });
                });
                // gpui-component inputs (rename, find) require Root as the window's first layer.
                cx.new(|cx| gpui_component::Root::new(workspace, window, cx))
            },
        );
        if opened.is_err() {
            eprintln!("无法打开窗口");
            cx.quit();
            return;
        }
        cx.activate(true);
    });
}
