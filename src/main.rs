mod app;
mod editor;
mod export;
mod files;
mod icons;
mod theme;

use gpui::{
    App, AppContext, Application, Bounds, KeyBinding, Menu, MenuItem, TitlebarOptions,
    WindowBounds, WindowOptions, actions, point, px, size,
};

actions!(thinkly_fast, [Quit, ExportImage]);

fn main() {
    Application::new().with_assets(icons::Icons).run(|cx: &mut App| {
        cx.on_action(|_: &Quit, cx: &mut App| cx.quit());
        cx.bind_keys([KeyBinding::new("cmd-q", Quit, None)]);
        editor::bind_keys(cx);
        cx.set_menus(vec![Menu {
            name: "Thinkly".into(),
            items: vec![MenuItem::action("导出图片", ExportImage)],
        }]);
        let bounds = Bounds::centered(None, size(px(1100.), px(760.)), cx);
        let opened = cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                window_min_size: Some(size(px(760.), px(480.))),
                titlebar: Some(TitlebarOptions {
                    title: Some("Thinkly".into()),
                    appears_transparent: true,
                    traffic_light_position: Some(point(px(12.), px(16.))),
                    ..Default::default()
                }),
                ..Default::default()
            },
            |window, cx| {
                let workspace = cx.new(|cx| app::Workspace::new(window, cx));
                let menu_target = workspace.clone();
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
                workspace
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
