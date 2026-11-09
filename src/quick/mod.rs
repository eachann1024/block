//! 速记小窗: a standalone 440x520 non-activating panel with three Markdown slots, toggled by a global ⌥N hotkey.
mod header;
#[cfg(target_os = "macos")]
mod ime_bridge;
mod panel;
mod save;
mod store;
mod view;
mod window;
#[cfg(target_os = "macos")]
mod panel_class;
pub use window::toggle;
pub(crate) mod window_smoke;

use std::{sync::atomic::{AtomicBool, AtomicU32, Ordering}, time::Duration};
use global_hotkey::{GlobalHotKeyEvent, GlobalHotKeyManager, HotKeyState, hotkey::HotKey};
use gpui::{
    App, Entity, Focusable, Global, KeyBinding,
    WindowHandle, actions,
};
use crate::{editor::view::{Editor, EditorAction}, files::state::SavedWindowBounds};
use panel::configure_quick_panel;
pub(crate) use panel::{
    is_quick_panel_active, is_quick_panel_visible,
    mark_quick_triggered, mark_search_triggered, order_out_quick_panel,
    record_search_triggered,
};
pub use panel::parse_hotkey;
use view::QuickView;

actions!(quick_note, [ToggleQuickNote, HideQuickNote, SaveToNote, SlotOne, SlotTwo, SlotThree]);

static QUICK_HK_ID: AtomicU32 = AtomicU32::new(0);
static SEARCH_HK_ID: AtomicU32 = AtomicU32::new(0);
/// Recording must receive the chord; Carbon would otherwise swallow it and open a panel.
static HOTKEYS_SUSPENDED: AtomicBool = AtomicBool::new(false);

#[derive(Default)]
struct QuickState {
    view: Option<Entity<QuickView>>,
    window: Option<(WindowHandle<gpui_component::Root>, Entity<QuickView>)>,
    editor: Option<Entity<Editor>>,
    visible: bool,
    bounds: Option<SavedWindowBounds>,
    /// Kept alive for as long as the app runs, otherwise the hotkey is unregistered.
    hotkey: Option<GlobalHotKeyManager>,
    registered: Vec<HotKey>,
}
impl Global for QuickState {}

pub(super) fn remember_bounds(bounds: SavedWindowBounds, cx: &mut App) {
    if cx.has_global::<QuickState>() { cx.global_mut::<QuickState>().bounds = Some(bounds); }
}

pub(super) fn hide_window(cx: &mut App) {
    order_out_quick_panel();
    if cx.has_global::<QuickState>() {
        let state = cx.global_mut::<QuickState>();
        state.visible = false;
        // macOS retains the panel, editor, selection and undo history across toggles.
        #[cfg(not(target_os = "macos"))]
        { state.window = None; }
    }
}

pub fn is_open(cx: &App) -> bool {
    cx.try_global::<QuickState>().is_some_and(|s| s.visible && s.window.is_some())
}

pub fn close_if_open(cx: &mut App) {
    flush_editor(cx);
    #[cfg(not(target_os = "macos"))]
    let live = cx.try_global::<QuickState>().and_then(|s| s.window.clone());
    hide_window(cx);
    #[cfg(not(target_os = "macos"))]
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
    if !is_open(cx) { return false; }
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
        crate::ui::app_shortcuts::bind(cx, [
            KeyBinding::new(key, crate::app::OpenSearch, Some("QuickNote > Editor")),
            KeyBinding::new(key, crate::app::OpenSearch, Some("QuickNote")),
            KeyBinding::new(key, crate::app::OpenSearch, None),
        ]);
    }
    crate::ui::app_shortcuts::bind(cx, [
        KeyBinding::new("escape", HideQuickNote, Some("QuickNote")),
        KeyBinding::new("cmd-w", HideQuickNote, Some("QuickNote")),
        KeyBinding::new("cmd-shift-s", SaveToNote, Some("QuickNote")),
        KeyBinding::new("cmd-1", SlotOne, Some("QuickNote")),
        KeyBinding::new("cmd-2", SlotTwo, Some("QuickNote")),
        KeyBinding::new("cmd-3", SlotThree, Some("QuickNote")),
    ]);
    #[cfg(not(target_os = "macos"))]
    crate::ui::app_shortcuts::bind(cx, [
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
    if crate::smoke::is_worker() { rebind_hotkeys(cx); } else { start_hotkey(cx); }
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

/// Unregisters the system hotkeys so a settings recorder can see the same chord.
pub fn suspend_global_hotkeys(cx: &mut App) {
    if HOTKEYS_SUSPENDED.swap(true, Ordering::Relaxed) { return; }
    if !cx.has_global::<QuickState>() {
        HOTKEYS_SUSPENDED.store(false, Ordering::Relaxed);
        return;
    }
    let state = cx.global_mut::<QuickState>();
    let Some(manager) = &state.hotkey else { return; };
    for hk in state.registered.drain(..) { let _ = manager.unregister(hk); }
    QUICK_HK_ID.store(0, Ordering::Relaxed);
    SEARCH_HK_ID.store(0, Ordering::Relaxed);
}

pub fn rebind_hotkeys(cx: &mut App) {
    let was_suspended = HOTKEYS_SUSPENDED.swap(false, Ordering::Relaxed);
    let quick_spec = crate::theme::quick_shortcut();
    let search_spec = crate::theme::search_shortcut();
    if parse_hotkey(&quick_spec).is_some() {
        if let Ok(b) = KeyBinding::load(&quick_spec, Box::new(ToggleQuickNote), None, false, None, cx) {
            crate::ui::app_shortcuts::bind(cx, [b]);
        }
    }
    if parse_hotkey(&search_spec).is_some() {
        if let Ok(b) = KeyBinding::load(&search_spec, Box::new(crate::app::OpenSearch), None, false, None, cx) {
            crate::ui::app_shortcuts::bind(cx, [b]);
        }
        for ctx in ["QuickNote", "QuickNote > Editor"] {
            let pred = gpui::KeyBindingContextPredicate::parse(ctx).ok().map(std::rc::Rc::new);
            if let Ok(b) = KeyBinding::load(&search_spec, Box::new(crate::app::OpenSearch), pred, false, None, cx) {
                crate::ui::app_shortcuts::bind(cx, [b]);
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
    // The chord that ended recording may still be held; ignore the matching hotkey echo.
    if was_suspended {
        let _ = mark_quick_triggered();
        let _ = mark_search_triggered();
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
            let suspended = HOTKEYS_SUSPENDED.load(Ordering::Relaxed);
            let (quick_id, search_id) = (QUICK_HK_ID.load(Ordering::Relaxed), SEARCH_HK_ID.load(Ordering::Relaxed));
            let (mut quick_pressed, mut search_pressed) = (false, false);
            while let Ok(event) = GlobalHotKeyEvent::receiver().try_recv() {
                if suspended { continue; }
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
