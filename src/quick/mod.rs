//! 速记小窗: a separate 480x620 window with five Markdown slots, toggled by a global ⌥N hotkey.
mod header;
mod save;
mod store;
mod view;

use std::{path::PathBuf, sync::atomic::{AtomicU32, Ordering}, time::Duration};
use global_hotkey::{GlobalHotKeyEvent, GlobalHotKeyManager, HotKeyState, hotkey::{Code, HotKey, Modifiers}};
use gpui::{App, AppContext, Bounds, Entity, Global, KeyBinding, TitlebarOptions, WindowBounds, WindowHandle, WindowOptions, actions, point, px, size};
use crate::{app::Workspace, editor::view::Editor};
use view::QuickView;

actions!(quick_note, [ToggleQuickNote, HideQuickNote, SaveToNote, ToggleHelp, SlotOne, SlotTwo, SlotThree, SlotFour, SlotFive]);

static QUICK_HK_ID: AtomicU32 = AtomicU32::new(0);
static SEARCH_HK_ID: AtomicU32 = AtomicU32::new(0);

#[derive(Default)]
struct QuickState {
    view: Option<Entity<QuickView>>,
    window: Option<(WindowHandle<gpui_component::Root>, Entity<QuickView>)>,
    editor: Option<Entity<Editor>>,
    main: Option<(Option<WindowHandle<gpui_component::Root>>, Entity<Workspace>)>,
    /// Kept alive for as long as the app runs, otherwise the hotkey is unregistered.
    hotkey: Option<GlobalHotKeyManager>,
    registered: Vec<HotKey>,
}
impl Global for QuickState {}

pub fn set_main_window(handle: Option<WindowHandle<gpui_component::Root>>, workspace: Entity<Workspace>, cx: &mut App) {
    cx.global_mut::<QuickState>().main = Some((handle, workspace));
}

pub(super) fn notify_saved_note(path: PathBuf, cx: &mut App) {
    if let Some((_, workspace)) = cx.try_global::<QuickState>().and_then(|s| s.main.clone()) {
        workspace.update(cx, |w, cx| w.insert_external_note(path, cx));
    }
}

pub fn refresh_style(cx: &mut App) {
    let Some(state) = cx.try_global::<QuickState>() else { return };
    let (editor, window) = (state.editor.clone(), state.window.clone());
    if let Some(editor) = editor {
        editor.update(cx, |e, cx| { e.reset_layouts(); cx.notify(); });
    }
    if let Some((handle, view)) = window {
        let _ = handle.update(cx, |_, _, cx| view.update(cx, |_, cx| cx.notify()));
    }
}

fn summon_search(cx: &mut App) {
    let Some((Some(handle), workspace)) = cx.try_global::<QuickState>().and_then(|s| s.main.clone()) else { return };
    cx.activate(true);
    let _ = handle.update(cx, |_, window, cx| {
        window.activate_window();
        workspace.update(cx, |w, cx| w.summon_search(window, cx));
    });
}

/// Binds keys, the toggle action and the global hotkeys. Call once at startup.
pub fn init(cx: &mut App) {
    cx.set_global(QuickState::default());
    cx.bind_keys([
        KeyBinding::new("escape", HideQuickNote, Some("QuickNote")),
        KeyBinding::new("secondary-w", HideQuickNote, Some("QuickNote")),
        KeyBinding::new("secondary-1", SlotOne, Some("QuickNote")),
        KeyBinding::new("secondary-2", SlotTwo, Some("QuickNote")),
        KeyBinding::new("secondary-3", SlotThree, Some("QuickNote")),
        KeyBinding::new("secondary-4", SlotFour, Some("QuickNote")),
        KeyBinding::new("secondary-5", SlotFive, Some("QuickNote")),
    ]);
    cx.on_action(|_: &ToggleQuickNote, cx: &mut App| toggle(cx));
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
            Ok(true) => { let _ = handle.update(cx, |_, window, cx| view.update(cx, |view, cx| view.close(window, cx))); return; }
            Ok(false) => {
                let _ = handle.update(cx, |_, window, _| window.activate_window());
                cx.activate(true);
                return;
            }
            Err(_) => {} // window was closed: open a new one
        }
    }
    open(cx);
}

fn open(cx: &mut App) {
    let bounds = Bounds::centered(None, size(px(480.), px(620.)), cx);
    let opened = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            window_min_size: Some(size(px(420.), px(360.))),
            is_minimizable: false,
            is_resizable: false,
            titlebar: Some(TitlebarOptions {
                title: Some("速记".into()),
                appears_transparent: true,
                traffic_light_position: Some(point(px(16.), px(14.))),
                ..Default::default()
            }),
            ..Default::default()
        },
        |window, cx| {
            let view = cx.new(|cx| QuickView::new(window, cx));
            let editor = view.read(cx).editor.clone();
            cx.global_mut::<QuickState>().editor = Some(editor.clone());
            // Traffic-light close: write a recovery checkpoint synchronously, like the main window.
            window.on_window_should_close(cx, move |_, cx| {
                editor.update(cx, |editor, cx| {
                    let ready = editor.prepare_to_quit();
                    if !ready { cx.notify(); }
                    ready
                })
            });
            // gpui-component inputs (slot rename) require Root as the window's first layer.
            cx.global_mut::<QuickState>().view = Some(view.clone());
            cx.new(|cx| gpui_component::Root::new(view, window, cx))
        },
    );
    match opened {
        Ok(handle) => {
            let state = cx.global_mut::<QuickState>();
            state.window = state.view.take().map(|view| (handle, view)); cx.activate(true); }
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
                code = match s.as_bytes()[0] {
                    b'a' => Some(Code::KeyA), b'b' => Some(Code::KeyB), b'c' => Some(Code::KeyC),
                    b'd' => Some(Code::KeyD), b'e' => Some(Code::KeyE), b'f' => Some(Code::KeyF),
                    b'g' => Some(Code::KeyG), b'h' => Some(Code::KeyH), b'i' => Some(Code::KeyI),
                    b'j' => Some(Code::KeyJ), b'k' => Some(Code::KeyK), b'l' => Some(Code::KeyL),
                    b'm' => Some(Code::KeyM), b'n' => Some(Code::KeyN), b'o' => Some(Code::KeyO),
                    b'p' => Some(Code::KeyP), b'q' => Some(Code::KeyQ), b'r' => Some(Code::KeyR),
                    b's' => Some(Code::KeyS), b't' => Some(Code::KeyT), b'u' => Some(Code::KeyU),
                    b'v' => Some(Code::KeyV), b'w' => Some(Code::KeyW), b'x' => Some(Code::KeyX),
                    b'y' => Some(Code::KeyY), b'z' => Some(Code::KeyZ),
                    b'0' => Some(Code::Digit0), b'1' => Some(Code::Digit1), b'2' => Some(Code::Digit2),
                    b'3' => Some(Code::Digit3), b'4' => Some(Code::Digit4), b'5' => Some(Code::Digit5),
                    b'6' => Some(Code::Digit6), b'7' => Some(Code::Digit7), b'8' => Some(Code::Digit8),
                    b'9' => Some(Code::Digit9),
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
        cx.bind_keys([KeyBinding::new(&quick_spec, ToggleQuickNote, None)]);
    }
    if parse_hotkey(&search_spec).is_some() {
        cx.bind_keys([KeyBinding::new(&search_spec, crate::app::OpenSearch, None)]);
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
            if quick_pressed && cx.update(|cx| toggle(cx)).is_err() { break; }
            if search_pressed && cx.update(|cx| summon_search(cx)).is_err() { break; }
        }
    }).detach();
}
