//! Standalone 680x460 floating search window summoned by the global search hotkey
//! when the main window is not active.
use gpui::{
    App, AppContext, Bounds, Context, Entity, Global, IntoElement, ParentElement, Render,
    Subscription, Window, WindowBackgroundAppearance, WindowBounds, WindowControlArea,
    WindowHandle, WindowKind, WindowOptions, div, prelude::*, px, size,
};
use gpui_component::input::{InputEvent, InputState};
use crate::app::{Dismiss, OpenSearch, SearchDown, SearchUp};
use crate::theme::{Theme, tr};
use super::{SearchState, render_body, render_footer, render_header};

#[derive(Default)]
struct SearchWindowGlobal {
    view: Option<Entity<SearchWindow>>,
    window: Option<(WindowHandle<gpui_component::Root>, Entity<SearchWindow>)>,
}
impl Global for SearchWindowGlobal {}

pub struct SearchWindow {
    state: SearchState,
    _input_sub: Subscription,
    _blur_sub: Subscription,
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
    let Some((handle, view)) = cx
        .try_global::<SearchWindowGlobal>()
        .and_then(|state| state.window.clone())
    else {
        return;
    };
    let _ = handle.update(cx, |_, _, cx| view.update(cx, |_, cx| cx.notify()));
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
            cx.new(|cx| gpui_component::Root::new(view, window, cx))
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
                    let root = crate::quick::main_folder(cx);
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
        input.update(cx, |input, cx| input.focus(window, cx));
        let focus_input = input.clone();
        cx.defer_in(window, move |_, window, cx| {
            focus_input.update(cx, |input, cx| input.focus(window, cx));
        });
        Self {
            state: SearchState::new(input),
            _input_sub: input_sub,
            _blur_sub: blur_sub,
        }
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
        let Some((Some(main_handle), workspace)) = crate::quick::main_window(cx) else { return };
        cx.activate(true);
        let _ = main_handle.update(cx, |_, main_win, cx| {
            main_win.activate_window();
            workspace.update(cx, |w, cx| w.open_from_search(path, main_win, cx));
        });
    }
}

impl Render for SearchWindow {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let has_folder = crate::quick::main_folder(cx).is_some();
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
