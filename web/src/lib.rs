//! WebAssembly entry point mounting the full Block GPUI workspace (`src/app.rs`,
//! `src/files`, `src/editor`, `src/search`, `src/export`) inside `#editor-stage`.
#![allow(dead_code, unused_imports)]
use std::borrow::Cow;
use std::cell::Cell;
use std::rc::Rc;

use gpui::{App, AppContext, Application, WindowOptions};
use gpui_web::{CanvasFontFallback, WebBackendPreference, WebPlatform};
use wasm_bindgen::JsCast;
use wasm_bindgen::prelude::*;

#[path = "../../src/app.rs"]
pub mod app;
#[path = "../../src/editor/mod.rs"]
pub mod editor;
#[path = "../../src/export/mod.rs"]
pub mod export;
#[path = "../../src/files/mod.rs"]
pub mod files;
#[path = "../../src/fonts/mod.rs"]
pub mod fonts;
#[path = "../../src/icons.rs"]
pub mod icons;
#[path = "../../src/mascot/mod.rs"]
pub mod mascot;
#[path = "../../src/search/mod.rs"]
pub mod search;
#[path = "../../src/theme.rs"]
pub mod theme;
#[path = "../../src/ui/mod.rs"]
pub mod ui;
#[path = "../../src/window_bounds.rs"]
pub mod window_bounds;

pub use ui::dropdown;

pub fn apply_app_menus(_cx: &mut App) {}

thread_local! {
    static LAST_IME_AREA: Cell<(f32, f32, f32)> = const { Cell::new((0.0, 0.0, 20.0)) };
}

pub fn sync_ime_cursor_area(x: f32, y: f32, h: f32) {
    LAST_IME_AREA.with(|cell| cell.set((x, y, h)));
    apply_ime_cursor_area(x, y, h);
}

fn apply_ime_cursor_area(x: f32, y: f32, h: f32) {
    let Some(win) = web_sys::window() else { return };
    let Some(doc) = win.document() else { return };
    let Ok(Some(el)) = doc.query_selector("body > textarea") else { return };
    let Ok(textarea) = el.dyn_into::<web_sys::HtmlElement>() else { return };
    let anchor = doc
        .query_selector("#editor-stage > canvas")
        .ok()
        .flatten()
        .or_else(|| doc.get_element_by_id("editor-stage"))
        .or_else(|| doc.query_selector("body > canvas").ok().flatten());
    let Some(anchor) = anchor else { return };
    let rect = anchor.get_bounding_client_rect();
    let left = rect.left() + x as f64;
    let top = rect.top() + y as f64;
    let height = h.max(16.0) as f64;
    textarea.style().set_css_text(&format!(
        "position: fixed; left: {left:.1}px; top: {top:.1}px; width: 1px; height: {height:.1}px; line-height: {height:.1}px; font-size: 16px; opacity: 0; pointer-events: none; padding: 0; border: none; margin: 0; overflow: hidden; resize: none; z-index: -1;"
    ));
}

fn install_ime_guards() {
    let Some(win) = web_sys::window() else { return };
    let opts = web_sys::AddEventListenerOptions::new();
    opts.set_capture(true);
    let composing = Rc::new(Cell::new(false));
    for (name, flag) in [("compositionstart", true), ("compositionupdate", true), ("compositionend", false), ("blur", false)] {
        let c = composing.clone();
        let cb = Closure::<dyn FnMut(web_sys::Event)>::new(move |_: web_sys::Event| {
            c.set(flag);
            let (x, y, h) = LAST_IME_AREA.with(|cell| cell.get());
            apply_ime_cursor_area(x, y, h);
        });
        let _ = win.add_event_listener_with_callback_and_add_event_listener_options(name, cb.as_ref().unchecked_ref(), &opts);
        cb.forget();
    }
    {
        let c = composing.clone();
        let cb = Closure::<dyn FnMut(web_sys::KeyboardEvent)>::new(move |event: web_sys::KeyboardEvent| {
            let key = event.key();
            if !event.meta_key()
                && !event.ctrl_key()
                && (c.get() || event.is_composing() || event.key_code() == 229 || key == "Process" || key == "Dead")
            {
                event.stop_immediate_propagation();
            }
        });
        let _ = win.add_event_listener_with_callback_and_add_event_listener_options("keydown", cb.as_ref().unchecked_ref(), &opts);
        cb.forget();
    }
    for name in ["scroll", "resize"] {
        let cb = Closure::<dyn FnMut(web_sys::Event)>::new(move |_: web_sys::Event| {
            let (x, y, h) = LAST_IME_AREA.with(|cell| cell.get());
            apply_ime_cursor_area(x, y, h);
        });
        let _ = win.add_event_listener_with_callback_and_add_event_listener_options(name, cb.as_ref().unchecked_ref(), &opts);
        cb.forget();
    }
}

pub mod quick {
    pub fn parse_hotkey(spec: &str) -> Option<()> {
        (!spec.trim().is_empty()).then_some(())
    }
    pub fn rebind_hotkeys(_cx: &mut gpui::App) {}
    pub fn refresh_style(_cx: &mut gpui::App) {}
}

pub mod log {
    pub fn set_context(_ctx: impl Into<String>) {}
    pub fn warn(msg: &str) { web_sys::console::warn_1(&msg.into()); }
    pub fn error(msg: &str) { web_sys::console::error_1(&msg.into()); }
}

struct WebAssets;
impl gpui::AssetSource for WebAssets {
    fn load(&self, path: &str) -> gpui::Result<Option<Cow<'static, [u8]>>> {
        if let Some(bytes) = mascot::load_asset(path) {
            return Ok(Some(Cow::Borrowed(bytes)));
        }
        icons::Icons.load(path)
    }
    fn list(&self, path: &str) -> gpui::Result<Vec<gpui::SharedString>> {
        icons::Icons.list(path)
    }
}

fn mount_canvas_into_stage() {
    let Some(win) = web_sys::window() else { return };
    let Some(doc) = win.document() else { return };
    let Some(stage) = doc.get_element_by_id("editor-stage") else { return };
    if let Ok(Some(canvas)) = doc.query_selector("body > canvas") {
        let _ = stage.append_child(&canvas);
    }
    let _ = doc.document_element().map(|el| el.set_attribute("data-editor-ready", "1"));
}

fn detect_page_language() -> files::state::LanguageMode {
    let is_en = web_sys::window()
        .and_then(|win| win.document())
        .and_then(|doc| doc.document_element())
        .and_then(|el| el.get_attribute("lang"))
        .is_some_and(|lang| lang.trim().to_ascii_lowercase().starts_with("en"));
    if is_en {
        files::state::LanguageMode::En
    } else {
        files::state::LanguageMode::Zh
    }
}

#[wasm_bindgen(start)]
pub fn start() {
    console_error_panic_hook::set_once();
    theme::set_language_mode(detect_page_language());
    theme::set_theme_mode(files::state::ThemeMode::Light);
    theme::set_font_choice(files::state::FontChoice::Sans);

    install_ime_guards();
    let platform = WebPlatform::new_with_backend_and_font_fallback(
        false,
        WebBackendPreference::Auto,
        CanvasFontFallback::EmojiAndCjk,
    );

    Application::with_platform(Rc::new(platform))
        .with_assets(WebAssets)
        .run(|cx: &mut App| {
            cx.spawn(async move |cx| {
                let fetched = fonts::load_ui_fonts().await;
                cx.update(|cx| {
                    let cows: Vec<Cow<'static, [u8]>> = if fetched.is_empty() {
                        vec![
                            Cow::Owned(export::metric_font("IBM Plex Sans")),
                            Cow::Owned(export::metric_font("JetBrains Mono")),
                        ]
                    } else {
                        fetched.into_iter().map(Cow::Borrowed).collect()
                    };
                    let _ = cx.text_system().add_fonts(cows);
                    theme::init_fonts(&cx.text_system().all_font_names());
                    gpui_component::init(cx);
                    gpui_component::Theme::change(gpui_component::ThemeMode::Light, None, cx);
                    editor::bind_keys(cx);

                    let _ = cx.open_window(WindowOptions::default(), |window, cx| {
                        mount_canvas_into_stage();
                        let workspace = cx.new(|cx| app::Workspace::new(window, cx));
                        search::window::set_main_window(window.window_handle().downcast::<gpui_component::Root>(), workspace.clone(), cx);
                        fonts::spawn(workspace.clone(), cx);
                        let tab_target = workspace.clone();
                        let settings_target = workspace.clone();
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
                                settings_target.update(cx, |this, cx| this.open_settings(window, cx));
                            });
                        });
                        cx.new(|cx| gpui_component::Root::new(workspace, window, cx))
                    });
                    cx.activate(true);
                });
            })
            .detach();
        });
}
