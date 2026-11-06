//! 速记小窗: a standalone 440x520 non-activating panel with three Markdown slots, toggled by a global ⌥N hotkey.
mod header;
#[cfg(target_os = "macos")]
mod ime_bridge;
mod panel;
mod save;
mod store;
mod view;

use std::{sync::atomic::{AtomicU32, Ordering}, time::Duration};
use global_hotkey::{GlobalHotKeyEvent, GlobalHotKeyManager, HotKeyState, hotkey::HotKey};
use gpui::{
    App, AppContext, Entity, Focusable, Global, KeyBinding, TitlebarOptions,
    WindowHandle, WindowKind, WindowOptions, actions, point, px, size,
};
use crate::{editor::view::{Editor, EditorAction}, files::state::SavedWindowBounds};
use panel::{configure_quick_panel, prepare_quick_summon};
pub(crate) use panel::{
    is_main_window, is_main_window_visible, is_quick_panel_active, is_quick_panel_visible,
    mark_quick_triggered, mark_search_triggered, order_out_quick_panel,
    record_search_triggered, show_native_main_window,
};
pub use panel::parse_hotkey;
use view::QuickView;

actions!(quick_note, [ToggleQuickNote, HideQuickNote, SaveToNote, SlotOne, SlotTwo, SlotThree]);

static QUICK_HK_ID: AtomicU32 = AtomicU32::new(0);
static SEARCH_HK_ID: AtomicU32 = AtomicU32::new(0);

#[derive(Default)]
struct QuickState {
    view: Option<Entity<QuickView>>,
    window: Option<(WindowHandle<gpui_component::Root>, Entity<QuickView>)>,
    editor: Option<Entity<Editor>>,
    bounds: Option<SavedWindowBounds>,
    /// Kept alive for as long as the app runs, otherwise the hotkey is unregistered.
    hotkey: Option<GlobalHotKeyManager>,
    registered: Vec<HotKey>,
}
impl Global for QuickState {}

pub(super) fn remember_bounds(bounds: SavedWindowBounds, cx: &mut App) {
    if cx.has_global::<QuickState>() { cx.global_mut::<QuickState>().bounds = Some(bounds); }
}

pub(super) fn clear_window_state(cx: &mut App) {
    order_out_quick_panel();
    if cx.has_global::<QuickState>() { cx.global_mut::<QuickState>().window = None; }
}

pub fn is_open(cx: &App) -> bool {
    cx.try_global::<QuickState>().map_or(false, |s| s.window.is_some())
}

pub fn close_if_open(cx: &mut App) {
    flush_editor(cx);
    let live = cx.try_global::<QuickState>().and_then(|s| s.window.clone());
    clear_window_state(cx);
    if let Some((handle, _)) = live {
        if handle.update(cx, |_, window, _| window.remove_window()).is_err() {
            cx.defer(move |cx| { let _ = handle.update(cx, |_, window, _| window.remove_window()); });
        }
    }
}

pub fn dispatch_editor_action(action: &EditorAction, cx: &mut App) -> bool {
    if !is_quick_panel_active() && !crate::smoke::is_worker() {
        if let Some(active) = cx.active_window() {
            let _ = active.update(cx, |_, w, cx| { let _ = crate::editor::paste_clipboard::forward_focused_input_action(action, w, cx); });
        }
        return false;
    }
    let Some((handle, view)) = cx.try_global::<QuickState>().and_then(|s| s.window.clone()) else { return false };
    handle.update(cx, |_, window, cx| {
        if crate::editor::paste_clipboard::forward_focused_input_action(action, window, cx) { return; }
        let editor = view.read(cx).editor.clone();
        window.focus(&editor.focus_handle(cx), cx);
        editor.update(cx, |e, cx| e.handle(action, window, cx));
    }).is_ok()
}

pub fn flush_editor(cx: &mut App) {
    if let Some(editor) = cx.try_global::<QuickState>().and_then(|s| s.editor.clone()) {
        editor.update(cx, |editor, cx| editor.save(cx));
    }
}

pub fn refresh_style(cx: &mut App) {
    crate::search::window::refresh_style(cx);
    let (editor, window) = match cx.try_global::<QuickState>() {
        Some(s) => (s.editor.clone(), s.window.clone()),
        None => return,
    };
    if let Some(editor) = editor { editor.update(cx, |e, cx| { e.reset_layouts(); cx.notify(); }); }
    if let Some((_, view)) = window { view.update(cx, |_, cx| cx.notify()); }
}

/// Binds keys, the toggle action and the global hotkeys. Call once at startup.
pub fn init(saved_bounds: Option<SavedWindowBounds>, cx: &mut App) {
    use crate::editor::view as ev;
    cx.set_global(QuickState { bounds: saved_bounds, ..Default::default() });
    for key in ["cmd-shift-k", "cmd-k", "cmd-p", "alt-cmd-k"] {
        cx.bind_keys([
            KeyBinding::new(key, crate::app::OpenSearch, Some("QuickNote > Editor")),
            KeyBinding::new(key, crate::app::OpenSearch, Some("QuickNote")),
            KeyBinding::new(key, crate::app::OpenSearch, None),
        ]);
    }
    cx.bind_keys([
        KeyBinding::new("escape", HideQuickNote, Some("QuickNote")),
        KeyBinding::new("cmd-w", HideQuickNote, Some("QuickNote")),
        KeyBinding::new("cmd-shift-s", SaveToNote, Some("QuickNote")),
        KeyBinding::new("cmd-1", SlotOne, Some("QuickNote")),
        KeyBinding::new("cmd-2", SlotTwo, Some("QuickNote")),
        KeyBinding::new("cmd-3", SlotThree, Some("QuickNote")),
    ]);
    #[cfg(not(target_os = "macos"))]
    cx.bind_keys([
        KeyBinding::new("ctrl-shift-k", crate::app::OpenSearch, Some("QuickNote > Editor")),
        KeyBinding::new("ctrl-k", crate::app::OpenSearch, Some("QuickNote > Editor")),
        KeyBinding::new("ctrl-p", crate::app::OpenSearch, Some("QuickNote > Editor")),
        KeyBinding::new("ctrl-w", HideQuickNote, Some("QuickNote")),
        KeyBinding::new("ctrl-shift-s", SaveToNote, Some("QuickNote")),
        KeyBinding::new("ctrl-1", SlotOne, Some("QuickNote")),
        KeyBinding::new("ctrl-2", SlotTwo, Some("QuickNote")),
        KeyBinding::new("ctrl-3", SlotThree, Some("QuickNote")),
    ]);
    cx.on_action(|_: &ToggleQuickNote, cx: &mut App| toggle(cx));
    cx.on_action(|_: &crate::app::OpenSearch, cx: &mut App| crate::search::window::summon_main(cx));
    cx.on_action(|_: &ev::Paste, cx: &mut App| { let _ = dispatch_editor_action(&EditorAction::Paste, cx); });
    cx.on_action(|_: &ev::Copy, cx: &mut App| { let _ = dispatch_editor_action(&EditorAction::Copy, cx); });
    cx.on_action(|_: &ev::Cut, cx: &mut App| { let _ = dispatch_editor_action(&EditorAction::Cut, cx); });
    cx.on_action(|_: &ev::SelectAll, cx: &mut App| { let _ = dispatch_editor_action(&EditorAction::SelectAll, cx); });
    cx.on_action(|_: &ev::Undo, cx: &mut App| { let _ = dispatch_editor_action(&EditorAction::Undo, cx); });
    cx.on_action(|_: &ev::Redo, cx: &mut App| { let _ = dispatch_editor_action(&EditorAction::Redo, cx); });
    start_hotkey(cx);
}

/// Flush the quick-note editor on quit; false when the final checkpoint could not be written.
pub fn prepare_to_quit(cx: &mut App) -> bool {
    let Some(editor) = cx.try_global::<QuickState>().and_then(|state| state.editor.clone()) else { return true };
    editor.update(cx, |editor, cx| {
        let ready = editor.prepare_to_quit();
        if !ready { cx.notify(); }
        ready
    })
}

pub fn toggle(cx: &mut App) {
    if !mark_quick_triggered() { return; }
    let live = cx.try_global::<QuickState>().and_then(|state| state.window.clone());
    if let Some((handle, view)) = live {
        let active = handle.update(cx, |_, window, _| window.is_window_active());
        match active {
            Ok(is_active) if is_active && is_quick_panel_visible() => {
                let _ = handle.update(cx, |_, window, cx| view.update(cx, |view, cx| view.close(window, cx)));
                return;
            }
            Ok(_) => {
                prepare_quick_summon();
                configure_quick_panel();
                let _ = handle.update(cx, |_, window, cx| {
                    window.activate_window();
                    view.update(cx, |v, cx| v.switch(v.meta.active, window, cx));
                });
                return;
            }
            Err(_) => {
                if cx.has_global::<QuickState>() {
                    cx.global_mut::<QuickState>().window = None;
                }
            }
        }
    }
    open(cx);
}

fn open(cx: &mut App) {
    prepare_quick_summon();
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
            focus: true, show: true, is_movable: true, is_minimizable: false, is_resizable: true,
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
                    let ready = editor.prepare_to_quit();
                    if !ready { cx.notify(); }
                    ready
                });
                if ready {
                    clear_window_state(cx);
                }
                ready
            });
            cx.global_mut::<QuickState>().view = Some(view.clone());
            cx.new(|cx| gpui_component::Root::new(view, window, cx))
        },
    );
    match opened {
        Ok(handle) => {
            let state = cx.global_mut::<QuickState>();
            state.window = state.view.take().map(|view| (handle, view));
            configure_quick_panel();
        }
        Err(error) => eprintln!("无法打开速记小窗: {error}"),
    }
}

pub fn rebind_hotkeys(cx: &mut App) {
    let quick_spec = crate::theme::quick_shortcut();
    let search_spec = crate::theme::search_shortcut();
    if parse_hotkey(&quick_spec).is_some() {
        if let Ok(b) = KeyBinding::load(&quick_spec, Box::new(ToggleQuickNote), None, false, None, cx) {
            cx.bind_keys([b]);
        }
    }
    if parse_hotkey(&search_spec).is_some() {
        if let Ok(b) = KeyBinding::load(&search_spec, Box::new(crate::app::OpenSearch), None, false, None, cx) {
            cx.bind_keys([b]);
        }
        for ctx in ["QuickNote", "QuickNote > Editor"] {
            let pred = gpui::KeyBindingContextPredicate::parse(ctx).ok().map(std::rc::Rc::new);
            if let Ok(b) = KeyBinding::load(&search_spec, Box::new(crate::app::OpenSearch), pred, false, None, cx) {
                cx.bind_keys([b]);
            }
        }
    }
    if !cx.has_global::<QuickState>() { return; }
    let state = cx.global_mut::<QuickState>();
    let Some(manager) = &state.hotkey else { return };
    for hk in state.registered.drain(..) { let _ = manager.unregister(hk); }
    QUICK_HK_ID.store(0, Ordering::Relaxed);
    SEARCH_HK_ID.store(0, Ordering::Relaxed);
    if let Some(hk) = parse_hotkey(&quick_spec) {
        if manager.register(hk).is_ok() {
            QUICK_HK_ID.store(hk.id(), Ordering::Relaxed);
            state.registered.push(hk);
        }
    }
    if let Some(hk) = parse_hotkey(&search_spec) {
        if manager.register(hk).is_ok() {
            SEARCH_HK_ID.store(hk.id(), Ordering::Relaxed);
            state.registered.push(hk);
        }
    }
}

/// Registers global hotkeys (Carbon on macOS) and polls events on the main thread.
fn start_hotkey(cx: &mut App) {
    match GlobalHotKeyManager::new() {
        Ok(manager) => cx.global_mut::<QuickState>().hotkey = Some(manager),
        Err(error) => { eprintln!("全局快捷键不可用: {error}"); return; }
    }
    rebind_hotkeys(cx);
    cx.spawn(async move |cx| {
        loop {
            cx.background_executor().timer(Duration::from_millis(100)).await;
            let (quick_id, search_id) = (QUICK_HK_ID.load(Ordering::Relaxed), SEARCH_HK_ID.load(Ordering::Relaxed));
            let (mut quick_pressed, mut search_pressed) = (false, false);
            while let Ok(event) = GlobalHotKeyEvent::receiver().try_recv() {
                if event.state == HotKeyState::Pressed {
                    quick_pressed |= quick_id != 0 && event.id == quick_id;
                    search_pressed |= search_id != 0 && event.id == search_id;
                }
            }
            if quick_pressed { cx.update(|cx| toggle(cx)); }
            if search_pressed { cx.update(|cx| crate::search::window::summon_from_hotkey(cx)); }
        }
    }).detach();
}
