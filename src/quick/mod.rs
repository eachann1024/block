//! 速记小窗: a standalone 440x520 non-activating panel with three Markdown slots, toggled by a global ⌥N hotkey.
mod header;
#[cfg(target_os = "macos")]
mod ime_bridge;
mod panel;
mod save;
mod store;
mod view;

use std::{sync::atomic::{AtomicU32, Ordering}, time::Duration};
use global_hotkey::{GlobalHotKeyEvent, GlobalHotKeyManager, HotKeyState, hotkey::{Code, HotKey, Modifiers}};
use gpui::{
    App, AppContext, Entity, Global, KeyBinding, TitlebarOptions,
    WindowHandle, WindowKind, WindowOptions, actions, point, px, size,
};
use crate::{editor::view::Editor, files::state::SavedWindowBounds};
use panel::{configure_quick_panel, order_out_quick_panel, prepare_quick_summon};
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
    if cx.has_global::<QuickState>() {
        cx.global_mut::<QuickState>().bounds = Some(bounds);
    }
}

pub(super) fn clear_window_state(cx: &mut App) {
    order_out_quick_panel();
    if cx.has_global::<QuickState>() {
        cx.global_mut::<QuickState>().window = None;
    }
}

pub fn is_open(cx: &App) -> bool {
    cx.try_global::<QuickState>().map_or(false, |s| s.window.is_some())
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
    cx.set_global(QuickState { bounds: saved_bounds, ..Default::default() });
    cx.bind_keys([
        KeyBinding::new("cmd-shift-k", crate::app::OpenSearch, Some("QuickNote > Editor")),
        KeyBinding::new("cmd-shift-k", crate::app::OpenSearch, Some("QuickNote")),
        KeyBinding::new("cmd-shift-k", crate::app::OpenSearch, None),
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
        KeyBinding::new("ctrl-shift-k", crate::app::OpenSearch, Some("QuickNote")),
        KeyBinding::new("ctrl-shift-k", crate::app::OpenSearch, None),
        KeyBinding::new("ctrl-w", HideQuickNote, Some("QuickNote")),
        KeyBinding::new("ctrl-shift-s", SaveToNote, Some("QuickNote")),
        KeyBinding::new("ctrl-1", SlotOne, Some("QuickNote")),
        KeyBinding::new("ctrl-2", SlotTwo, Some("QuickNote")),
        KeyBinding::new("ctrl-3", SlotThree, Some("QuickNote")),
    ]);
    cx.on_action(|_: &ToggleQuickNote, cx: &mut App| toggle(cx));
    cx.on_action(|_: &crate::app::OpenSearch, cx: &mut App| crate::search::window::summon_main(cx));
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
    let live = cx.try_global::<QuickState>().and_then(|state| state.window.clone());
    if let Some((handle, view)) = live {
        let active = handle.update(cx, |_, window, _| window.is_window_active());
        match active {
            Ok(true) => {
                let _ = handle.update(cx, |_, window, cx| view.update(cx, |view, cx| view.close(window, cx)));
                return;
            }
            Ok(false) => {
                prepare_quick_summon();
                let _ = handle.update(cx, |_, window, _| window.activate_window());
                configure_quick_panel();
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

pub fn parse_hotkey(spec: &str) -> Option<HotKey> {
    let mut mods = Modifiers::empty();
    let mut code = None;
    for part in spec.split('-').filter(|s| !s.is_empty()) {
        match part.to_ascii_lowercase().as_str() {
            "ctrl" | "control" => mods |= Modifiers::CONTROL,
            "alt" | "opt" | "option" => mods |= Modifiers::ALT,
            "shift" => mods |= Modifiers::SHIFT,
            "cmd" | "super" | "meta" => mods |= Modifiers::META,
            "space" => code = Some(Code::Space),
            "comma" | "," => code = Some(Code::Comma),
            "period" | "." => code = Some(Code::Period),
            "slash" | "/" => code = Some(Code::Slash),
            "semicolon" | ";" => code = Some(Code::Semicolon),
            "quote" | "'" => code = Some(Code::Quote),
            "minus" => code = Some(Code::Minus),
            "equal" | "=" => code = Some(Code::Equal),
            "backquote" | "`" => code = Some(Code::Backquote),
            s if s.len() == 1 => {
                let b = s.as_bytes()[0];
                code = match b {
                    b'a'..=b'z' => Some(match b {
                        b'a' => Code::KeyA, b'b' => Code::KeyB, b'c' => Code::KeyC, b'd' => Code::KeyD,
                        b'e' => Code::KeyE, b'f' => Code::KeyF, b'g' => Code::KeyG, b'h' => Code::KeyH,
                        b'i' => Code::KeyI, b'j' => Code::KeyJ, b'k' => Code::KeyK, b'l' => Code::KeyL,
                        b'm' => Code::KeyM, b'n' => Code::KeyN, b'o' => Code::KeyO, b'p' => Code::KeyP,
                        b'q' => Code::KeyQ, b'r' => Code::KeyR, b's' => Code::KeyS, b't' => Code::KeyT,
                        b'u' => Code::KeyU, b'v' => Code::KeyV, b'w' => Code::KeyW, b'x' => Code::KeyX,
                        b'y' => Code::KeyY, _ => Code::KeyZ,
                    }),
                    b'0'..=b'9' => Some(match b {
                        b'0' => Code::Digit0, b'1' => Code::Digit1, b'2' => Code::Digit2, b'3' => Code::Digit3,
                        b'4' => Code::Digit4, b'5' => Code::Digit5, b'6' => Code::Digit6, b'7' => Code::Digit7,
                        b'8' => Code::Digit8, _ => Code::Digit9,
                    }),
                    _ => None,
                };
            }
            _ => return None,
        }
    }
    if spec.ends_with("--") { code = Some(Code::Minus); }
    if mods.is_empty() { return None; }
    Some(HotKey::new(Some(mods), code?))
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
