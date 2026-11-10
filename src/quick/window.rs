//! Quick-note window lifetime and presentation, independent of the workspace.
use gpui::{App, AppContext, TitlebarOptions, WindowKind, WindowOptions, point, px, size};
use super::{QuickState, QuickView, configure_quick_panel, mark_quick_triggered,
    is_quick_panel_visible, remember_bounds, hide_window};

pub fn toggle(cx: &mut App) {
    if !mark_quick_triggered() { return; }
    let live = cx.try_global::<QuickState>().and_then(|state| state.window.clone());
    if let Some((handle, view)) = live {
        let active = handle.update(cx, |_, window, _| {
            #[cfg(target_os = "macos")]
            { let _ = window; super::is_quick_panel_active() }
            #[cfg(not(target_os = "macos"))]
            window.is_window_active()
        });
        match active {
            Ok(is_active) if is_active && is_quick_panel_visible() => {
                let _ = handle.update(cx, |_, window, cx| view.update(cx, |view, cx| view.close(window, cx)));
                return;
            }
            Ok(_) => {
                configure_quick_panel();
                cx.global_mut::<QuickState>().visible = true;
                let _ = handle.update(cx, |_, window, cx| {
                    #[cfg(not(target_os = "macos"))]
                    if !crate::smoke::is_worker() { window.activate_window(); }
                    view.update(cx, |v, cx| v.switch(v.meta.active, window, cx));
                });
                return;
            }
            Err(_) => {
                if cx.has_global::<QuickState>() {
                    let state = cx.global_mut::<QuickState>();
                    state.window = None;
                    state.visible = false;
                }
            }
        }
    }
    open(cx);
}
fn open(cx: &mut App) {
    let saved_bounds = cx
        .try_global::<QuickState>()
        .and_then(|s| s.bounds)
        .or_else(|| crate::files::load_state().ok().and_then(|s| s.quick_window_bounds));
    let min_size = size(px(380.), px(320.));
    let (window_bounds, display_id) = crate::window_bounds::resolve_window_bounds(
        saved_bounds, size(px(440.), px(520.)), min_size, cx,
    );
    let opened = cx.open_window(
        WindowOptions {
            window_bounds: Some(window_bounds), display_id,
            window_min_size: Some(min_size), kind: WindowKind::PopUp,
            // Configure the non-activating native panel before it is ever ordered front.
            focus: !cfg!(target_os = "macos") && !crate::smoke::is_worker(),
            show: !cfg!(target_os = "macos") && !crate::smoke::is_worker(),
            is_movable: true, is_minimizable: false, is_resizable: true,
            titlebar: Some(TitlebarOptions {
                title: Some(crate::theme::tr("速记", "Quick Note").into()),
                appears_transparent: true,
                traffic_light_position: cfg!(target_os = "macos").then_some(point(px(16.), px(16.))),
                ..Default::default()
            }),
            ..Default::default()
        },
        |window, cx| {
            let view = cx.new(|cx| QuickView::new(window, cx));
            let editor = view.read(cx).editor.clone();
            cx.global_mut::<QuickState>().editor = Some(editor.clone());
            // Traffic-light close: write a recovery checkpoint synchronously and order out the panel.
            window.on_window_should_close(cx, move |window, cx| {
                let bounds = crate::window_bounds::capture_window_bounds(window, cx);
                remember_bounds(bounds, cx);
                let _ = crate::files::update_state(move |s| s.quick_window_bounds = Some(bounds));
                let ready = editor.update(cx, |editor, cx| {
                    let ready = editor.is_loading() || editor.prepare_to_quit();
                    if !ready { cx.notify(); }
                    ready
                });
                if ready {
                    hide_window(cx);
                }
                ready && !cfg!(target_os = "macos")
            });
            cx.global_mut::<QuickState>().view = Some(view.clone());
            cx.new(|cx| gpui_component::Root::new(view, window, cx))
        },
    );
    match opened {
        Ok(handle) => {
            let state = cx.global_mut::<QuickState>();
            state.window = state.view.take().map(|view| (handle, view));
            state.visible = state.window.is_some();
            configure_quick_panel();
        }
        Err(error) => crate::log::error(&format!("无法打开速记小窗: {error}")),
    }
}
