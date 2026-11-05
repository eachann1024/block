//! Standalone 680x460 floating search window summoned by the global search hotkey
//! when the main window is not active.
use std::path::PathBuf;
use gpui::{
    App, AppContext, Bounds, Context, Entity, Global, IntoElement, ParentElement, Render,
    Subscription, Window, WindowBackgroundAppearance, WindowBounds, WindowControlArea,
    WindowHandle, WindowKind, WindowOptions, div, prelude::*, px, size,
};
use gpui_component::input::{InputEvent, InputState};
use crate::app::{Dismiss, OpenSearch, SearchDown, SearchUp, Workspace};
use crate::theme::{Theme, tr};
use super::{SearchState, render_body, render_footer, render_header};

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

pub fn set_main_window(
    handle: Option<WindowHandle<gpui_component::Root>>,
    workspace: Entity<Workspace>,
    cx: &mut App,
) {
    if !cx.has_global::<SearchWindowGlobal>() {
        cx.set_global(SearchWindowGlobal::default());
    }
    cx.global_mut::<SearchWindowGlobal>().main = Some((handle, workspace));
}

pub(crate) fn main_window(
    cx: &App,
) -> Option<(Option<WindowHandle<gpui_component::Root>>, Entity<Workspace>)> {
    cx.try_global::<SearchWindowGlobal>().and_then(|s| s.main.clone())
}

pub(crate) fn main_folder(cx: &App) -> Option<PathBuf> {
    cx.try_global::<SearchWindowGlobal>()
        .and_then(|s| s.main.as_ref())
        .and_then(|(_, workspace)| workspace.read(cx).folder.clone())
}

pub(crate) fn notify_saved_note(path: PathBuf, cx: &mut App) {
    if let Some((_, workspace)) = main_window(cx) {
        workspace.update(cx, |w, cx| w.insert_external_note(path, cx));
    }
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
fn app_windows() -> cocoa::base::id {
    use cocoa::{appkit::NSApplication, base::{SEL, id, nil, selector}};
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let app = NSApplication::sharedApplication(nil);
        let send: unsafe extern "C" fn(id, SEL) -> id = std::mem::transmute(objc_msgSend as *const ());
        send(app, selector("windows"))
    }
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub fn hide_main_window_on_close() -> bool {
    use cocoa::{appkit::NSWindow, base::nil, foundation::NSArray};
    unsafe {
        let windows = app_windows();
        let mut hidden = false;
        for i in 0..NSArray::count(windows) {
            let win = NSArray::objectAtIndex(windows, i);
            if NSWindow::level(win) == 0 {
                NSWindow::orderOut_(win, nil);
                hidden = true;
            }
        }
        hidden
    }
}

/// Without Cocoa window ordering, closing the main window quits instead of hiding it.
#[cfg(not(target_os = "macos"))]
pub fn hide_main_window_on_close() -> bool { false }

#[cfg(target_os = "macos")]
#[allow(deprecated)]
fn show_native_main_window() {
    use cocoa::{appkit::NSWindow, base::{BOOL, SEL, YES, id, nil, selector}, foundation::NSArray};
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let windows = app_windows();
        let is_mini: unsafe extern "C" fn(id, SEL) -> BOOL = std::mem::transmute(objc_msgSend as *const ());
        for i in 0..NSArray::count(windows) {
            let win = NSArray::objectAtIndex(windows, i);
            if NSWindow::level(win) == 0 {
                if is_mini(win, selector("isMiniaturized")) == YES {
                    NSWindow::deminiaturize_(win, nil);
                }
                NSWindow::makeKeyAndOrderFront_(win, nil);
            }
        }
    }
}

#[cfg(not(target_os = "macos"))]
fn show_native_main_window() {}

pub fn activate_main_window(cx: &mut App) {
    show_native_main_window();
    cx.activate(true);
    if let Some((Some(main_handle), _)) = main_window(cx) {
        let _ = main_handle.update(cx, |_, main_win, _| main_win.activate_window());
    }
}

pub fn summon_from_hotkey(cx: &mut App) {
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
                let _ = handle.update(cx, |_, window, cx| {
                    view.update(cx, |view, cx| view.close(window, cx));
                });
                return;
            }
            Ok(false) => {
                let _ = handle.update(cx, |_, window, _| window.remove_window());
                cx.global_mut::<SearchWindowGlobal>().window = None;
            }
            Err(_) => {
                cx.global_mut::<SearchWindowGlobal>().window = None;
            }
        }
    }
    open(cx);
}

pub fn refresh_style(cx: &mut App) {
    if let Some((_, workspace)) = main_window(cx) {
        workspace.update(cx, |w, cx| {
            w.editor.update(cx, |e, cx| { e.reset_layouts(); cx.notify(); });
            w.tree.update(cx, |_, cx| cx.notify());
            cx.notify();
        });
    }
    let Some((handle, view)) = cx.try_global::<SearchWindowGlobal>().and_then(|s| s.window.clone()) else { return };
    let _ = handle.update(cx, |_, window, cx| view.update(cx, |this, cx| {
        this.state.input.update(cx, |i, cx| i.set_placeholder(tr("搜索标题与正文", "Search titles and content"), window, cx));
        cx.notify();
    }));
}

fn open(cx: &mut App) {
    let bounds = Bounds::centered(None, size(px(680.), px(460.)), cx);
    let opened = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: None,
            focus: true,
            show: true,
            kind: WindowKind::PopUp,
            is_movable: true,
            is_resizable: false,
            is_minimizable: false,
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
                    let root = main_folder(cx);
                    this.state.run_search(root, query, cx, |this| Some(&mut this.state));
                }
                InputEvent::PressEnter { .. } => this.open_hit(window, cx),
                _ => {}
            }
        });
        let blur_sub = cx.observe_window_activation(window, |this, window, cx| {
            if !window.is_window_active() {
                this.close(window, cx);
            }
        });
        cx.observe_window_appearance(window, |_, window, cx| {
            if crate::theme::sync_system_appearance(window.appearance(), cx) {
                crate::quick::refresh_style(cx);
            }
        }).detach();
        input.update(cx, |input, cx| input.focus(window, cx));
        let focus_input = input.clone();
        cx.defer_in(window, move |_, window, cx| {
            focus_input.update(cx, |input, cx| input.focus(window, cx));
        });
        Self { state: SearchState::new(input), _input_sub: input_sub, _blur_sub: blur_sub }
    }

    pub(crate) fn close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if cx.has_global::<SearchWindowGlobal>() {
            cx.global_mut::<SearchWindowGlobal>().window = None;
        }
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
}

impl Render for SearchWindow {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let has_folder = main_folder(cx).is_some();
        div()
            .id("search-window")
            .key_context("SearchPalette")
            .size_full()
            .flex()
            .flex_col()
            .overflow_hidden()
            .rounded(px(12.))
            .bg(theme.menu)
            .border_1()
            .border_color(theme.menu_border)
            .text_color(theme.text)
            .on_action(cx.listener(|this, _: &SearchUp, _, cx| {
                this.state.move_selection(-1);
                cx.notify();
            }))
            .on_action(cx.listener(|this, _: &SearchDown, _, cx| {
                this.state.move_selection(1);
                cx.notify();
            }))
            .on_action(cx.listener(|this, _: &Dismiss, window, cx| this.close(window, cx)))
            .on_action(cx.listener(|this, _: &OpenSearch, window, cx| this.close(window, cx)))
            .child(render_header(
                &self.state.input,
                cx.listener(|this, _, window, cx| this.close(window, cx)),
            ))
            .child(render_body(&self.state, has_folder, true, cx, |this, ix, window, cx| {
                this.state.selected = ix;
                this.open_hit(window, cx);
            }))
            .child(render_footer().window_control_area(WindowControlArea::Drag))
    }
}
