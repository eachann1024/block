//! Standalone 680x460 floating search window summoned by the global search hotkey
//! when the main window is not active.
use std::path::PathBuf;
use gpui::{
    App, AppContext, Bounds, Context, Entity, Global, IntoElement, ParentElement, Render,
    Subscription, Window, WindowBackgroundAppearance, WindowBounds, WindowControlArea,
    WindowHandle, WindowKind, WindowOptions, div, prelude::*, px, size,
};
use gpui_component::input::{InputEvent, InputState};
use crate::app::{Dismiss, OpenSearch, SearchDown, SearchNextScope, SearchPrevScope, SearchResetScopeAll, SearchUp, Workspace};
use crate::theme::{Theme, tr};
use super::{SearchState, render_body, render_footer, render_header, render_scope_menu};
#[derive(Default)]
struct SearchWindowGlobal {
    view: Option<Entity<SearchWindow>>,
    window: Option<(WindowHandle<gpui_component::Root>, Entity<SearchWindow>)>,
    main: Option<(Option<WindowHandle<gpui_component::Root>>, Entity<Workspace>)>,
}
impl Global for SearchWindowGlobal {}
pub struct SearchWindow {
    state: SearchState,
    _input_sub: Subscription,
    _blur_sub: Subscription,
}
pub fn set_main_window(handle: Option<WindowHandle<gpui_component::Root>>, workspace: Entity<Workspace>, cx: &mut App) {
    if !cx.has_global::<SearchWindowGlobal>() { cx.set_global(SearchWindowGlobal::default()); }
    cx.global_mut::<SearchWindowGlobal>().main = Some((handle, workspace));
}
pub(crate) fn main_window(cx: &App) -> Option<(Option<WindowHandle<gpui_component::Root>>, Entity<Workspace>)> {
    cx.try_global::<SearchWindowGlobal>().and_then(|s| s.main.clone())
}
pub(crate) fn main_folders(cx: &App) -> Vec<PathBuf> {
    if let Some((_, workspace)) = main_window(cx) { return workspace.read(cx).search_folders(); }
    crate::files::load_state().ok().map(|state| super::notebook_folders(state.folder, &state.notebooks)).unwrap_or_default()
}
pub(crate) fn notify_saved_note(path: PathBuf, cx: &mut App) {
    if let Some((_, workspace)) = main_window(cx) {
        workspace.update(cx, |w, cx| w.insert_external_note(path, cx));
    }
}
pub use crate::window_visibility::hide_main_window_on_close;
/// Reveal only the workspace; the quick-note lifetime is independent.
pub fn activate_main_window(cx: &mut App) {
    crate::window_visibility::show_native_main_window();
    if crate::smoke::is_worker() { return; }
    cx.activate(true);
    if let Some((Some(handle), _)) = main_window(cx) {
        let _ = handle.update(cx, |_, window, _| window.activate_window());
    }
}
pub fn reveal_main_window(window: &mut Window, cx: &mut App) {
    if !crate::window_visibility::is_main_window_visible() || !window.is_window_active() {
        crate::window_visibility::show_native_main_window();
        if !crate::smoke::is_worker() {
            cx.activate(true);
            window.activate_window();
        }
    }
}
pub fn ensure_main_window(cx: &mut App) -> Option<gpui::AnyWindowHandle> {
    let handle = main_window(cx)?.0?;
    let active = handle.update(cx, |_, window, _| window.is_window_active()).unwrap_or(false);
    if !crate::window_visibility::is_main_window_visible() || !active {
        activate_main_window(cx);
    }
    Some(handle.into())
}
pub fn summon_main(cx: &mut App) {
    if !crate::quick::mark_search_triggered() { return; }
    activate_main_window(cx);
    if let Some((Some(handle), workspace)) = main_window(cx) {
        let _ = handle.update(cx, |_, window, cx| {
            workspace.update(cx, |w, cx| w.summon_search(window, cx));
        });
    }
}
pub fn summon_from_hotkey(cx: &mut App) {
    if !crate::quick::mark_search_triggered() { return; }
    if !crate::window_visibility::is_main_window_visible() {
        activate_main_window(cx);
        if let Some((Some(handle), workspace)) = main_window(cx) {
            let _ = handle.update(cx, |_, window, cx| {
                workspace.update(cx, |w, cx| w.summon_search(window, cx));
            });
        }
        return;
    }
    if let Some((Some(handle), workspace)) = main_window(cx) {
        if handle.update(cx, |_, window, _| window.is_window_active()).unwrap_or(false) {
            let _ = handle.update(cx, |_, window, cx| {
                workspace.update(cx, |w, cx| w.summon_search(window, cx));
            });
            return;
        }
    }
    toggle(cx);
}
pub fn toggle(cx: &mut App) {
    if !cx.has_global::<SearchWindowGlobal>() {
        cx.set_global(SearchWindowGlobal::default());
    }
    let live = cx.global::<SearchWindowGlobal>().window.clone();
    if let Some((handle, view)) = live {
        match handle.update(cx, |_, window, _| window.is_window_active()) {
            Ok(true) => {
                let _ = handle.update(cx, |_, window, cx| view.update(cx, |v, cx| v.close(window, cx)));
                return;
            }
            Ok(false) => {
                let _ = handle.update(cx, |_, window, _| window.remove_window());
                cx.global_mut::<SearchWindowGlobal>().window = None;
            }
            Err(_) => cx.global_mut::<SearchWindowGlobal>().window = None,
        }
    }
    open(cx);
}
pub fn refresh_style(cx: &mut App) {
    let Some((handle, view)) = cx.try_global::<SearchWindowGlobal>().and_then(|s| s.window.clone()) else { return };
    if handle.update(cx, |_, window, cx| view.update(cx, |this, cx| {
        this.state.input.update(cx, |i, cx| i.set_placeholder(tr("搜索标题与正文", "Search titles and content"), window, cx));
        cx.notify();
    })).is_err() {
        view.update(cx, |_, cx| cx.notify());
    }
}
fn open(cx: &mut App) {
    let bounds = Bounds::centered(None, size(px(680.), px(460.)), cx);
    let opened = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: None, focus: true, show: true,
            kind: WindowKind::PopUp, is_movable: true, is_resizable: false, is_minimizable: false,
            window_background: WindowBackgroundAppearance::Transparent,
            ..Default::default()
        },
        |window, cx| {
            let view = cx.new(|cx| SearchWindow::new(window, cx));
            cx.global_mut::<SearchWindowGlobal>().view = Some(view.clone());
            cx.new(|cx| gpui_component::Root::new(view, window, cx).bg(gpui::transparent_black()))
        },
    );
    match opened {
        Ok(handle) => {
            let state = cx.global_mut::<SearchWindowGlobal>();
            state.window = state.view.take().map(|view| (handle, view));
        }
        Err(error) => eprintln!("无法打开全局搜索窗口: {error}"),
    }
}
impl SearchWindow {
    fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let input = cx.new(|cx| {
            InputState::new(window, cx)
                .placeholder(tr("搜索标题与正文", "Search titles and content"))
        });
        let input_sub = cx.subscribe_in(&input, window, |this, input, event: &InputEvent, window, cx| {
            match event {
                InputEvent::Change => {
                    let query = input.read(cx).value().to_string();
                    this.state.sync_folders(main_folders(cx));
                    this.state.run_search(query, cx, |this| Some(&mut this.state));
                }
                InputEvent::PressEnter { .. } => this.open_hit(window, cx),
                _ => {}
            }
        });
        let blur_sub = cx.observe_window_activation(window, |this, window, cx| {
            if !window.is_window_active() { this.close(window, cx); }
        });
        window.observe_window_appearance(|window, cx| {
            if crate::theme::sync_system_appearance(window.appearance(), cx) {
                cx.defer(|cx| {
                    if let Some((_, ws)) = main_window(cx) {
                        ws.update(cx, |w, cx| {
                            w.editor.update(cx, |e, cx| { e.reset_layouts(); cx.notify(); });
                            w.tree.update(cx, |_, cx| cx.notify());
                            cx.notify();
                        });
                    }
                    crate::quick::refresh_style(cx);
                });
            }
        }).detach();
        input.update(cx, |input, cx| input.focus(window, cx));
        let focus_input = input.clone();
        cx.defer_in(window, move |_, window, cx| {
            focus_input.update(cx, |input, cx| input.focus(window, cx));
        });
        Self { state: SearchState::new(input, main_folders(cx)), _input_sub: input_sub, _blur_sub: blur_sub }
    }
    pub(crate) fn close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if cx.has_global::<SearchWindowGlobal>() {
            cx.global_mut::<SearchWindowGlobal>().window = None;
        }
        crate::ui::motion::popup_reset("quick-search-scope");
        window.remove_window();
    }
    fn open_hit(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(path) = self.state.selected_path() else { return };
        self.close(window, cx);
        let Some((Some(main_handle), workspace)) = main_window(cx) else { return };
        activate_main_window(cx);
        let _ = main_handle.update(cx, |_, main_win, cx| {
            workspace.update(cx, |w, cx| w.open_from_search(path, main_win, cx));
        });
    }
    fn step_scope(&mut self, delta: isize, cx: &mut Context<Self>) {
        let list = self.state.sync_folders(main_folders(cx));
        if self.state.cycle_scope(delta) || (list && self.state.asked) { self.state.research(cx, |this| Some(&mut this.state)); }
        cx.notify();
    }
}
impl Render for SearchWindow {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let folders = main_folders(cx);
        if self.state.sync_folders(folders) && self.state.asked {
            let task = cx.entity().downgrade();
            cx.defer(move |cx| { task.update(cx, |this, cx| this.state.research(cx, |this| Some(&mut this.state))).ok(); });
        }
        div()
            .id("search-window").key_context("SearchPalette").size_full().relative()
            .flex().flex_col().overflow_hidden().rounded(px(12.))
            .bg(theme.menu).border_1().border_color(theme.menu_border).text_color(theme.text)
            .font_family(crate::theme::ui_font_family())
            .on_mouse_down(gpui::MouseButton::Left, cx.listener(|this, _, _, cx| {
                if this.state.scope_menu_open { this.state.scope_menu_open = false; cx.notify(); }
            }))
            .on_action(cx.listener(|this, _: &SearchUp, _, cx| { this.state.move_selection(-1); cx.notify(); }))
            .on_action(cx.listener(|this, _: &SearchDown, _, cx| { this.state.move_selection(1); cx.notify(); }))
            .on_action(cx.listener(|this, _: &SearchNextScope, _, cx| this.step_scope(1, cx)))
            .on_action(cx.listener(|this, _: &SearchPrevScope, _, cx| this.step_scope(-1, cx)))
            .on_action(cx.listener(|this, _: &SearchResetScopeAll, _, cx| {
                if this.state.reset_scope_all() { this.state.research(cx, |this| Some(&mut this.state)); }
                cx.notify();
            }))
            .capture_action(cx.listener(|this, _: &gpui_component::input::Escape, window, cx| {
                cx.stop_propagation();
                if this.state.scope_menu_open { this.state.scope_menu_open = false; cx.notify(); return; }
                this.close(window, cx);
            }))
            .on_action(cx.listener(|this, _: &Dismiss, window, cx| {
                if this.state.scope_menu_open { this.state.scope_menu_open = false; cx.notify(); return; }
                this.close(window, cx);
            }))
            .on_action(cx.listener(|this, _: &OpenSearch, window, cx| this.close(window, cx)))
            .child(render_header(
                &self.state,
                cx.listener(|this, _, _, cx| { this.state.toggle_scope_menu(); cx.notify(); }),
            ))
            .children(render_scope_menu("quick-search-scope", &self.state, cx, |this, index, _, cx| {
                if this.state.select_scope_index(index) { this.state.research(cx, |this| Some(&mut this.state)); }
                cx.notify();
            }))
            .child(render_body(&self.state, true, cx, |this, ix, window, cx| {
                this.state.selected = ix;
                this.open_hit(window, cx);
            }))
            .child(render_footer().window_control_area(WindowControlArea::Drag))
    }
}
