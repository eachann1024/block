//! WebAssembly entry point mounting the full Thinkly Fast GPUI workspace (`src/app.rs`,
//! `src/files`, `src/editor`, `src/search`, `src/export`) inside `#editor-stage`.
#![allow(dead_code, unused_imports)]
use std::borrow::Cow;
use std::rc::Rc;

use gpui::{App, AppContext, Application, WindowOptions};
use gpui_web::{CanvasFontFallback, WebBackendPreference, WebPlatform};
use wasm_bindgen::prelude::*;

#[path = "../../src/app.rs"]
pub mod app;
#[path = "../../src/editor/mod.rs"]
pub mod editor;
#[path = "../../src/export/mod.rs"]
pub mod export;
#[path = "../../src/files/mod.rs"]
pub mod files;
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

const FONT_PLEX_REGULAR: &[u8] = include_bytes!("../fonts/IBMPlexSans-Regular.ttf");
const FONT_PLEX_SEMIBOLD: &[u8] = include_bytes!("../fonts/IBMPlexSans-SemiBold.ttf");
const FONT_JETBRAINS_MONO: &[u8] = include_bytes!("../fonts/JetBrainsMono-Regular.ttf");
/// Fonts bundled into the build; also the font database of the 生成图片 export renderer.
pub(crate) const WEB_FONTS: &[&[u8]] = &[FONT_PLEX_REGULAR, FONT_PLEX_SEMIBOLD, FONT_JETBRAINS_MONO];

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

#[wasm_bindgen(start)]
pub fn start() {
    console_error_panic_hook::set_once();
    theme::set_language_mode(files::state::LanguageMode::Zh);
    theme::set_theme_mode(files::state::ThemeMode::Light);
    theme::set_font_choice(files::state::FontChoice::Sans);

    let platform = WebPlatform::new_with_backend_and_font_fallback(
        false,
        WebBackendPreference::Auto,
        CanvasFontFallback::EmojiAndCjk,
    );

    Application::with_platform(Rc::new(platform))
        .with_assets(WebAssets)
        .run(|cx: &mut App| {
            // Must register Latin UI & mono fonts before resolving `.SystemUIFont` (upstream #3101).
            let _ = cx.text_system().add_fonts(vec![
                Cow::Borrowed(FONT_PLEX_REGULAR),
                Cow::Borrowed(FONT_PLEX_SEMIBOLD),
                Cow::Borrowed(FONT_JETBRAINS_MONO),
            ]);
            theme::init_fonts(&cx.text_system().all_font_names());
            gpui_component::init(cx);
            gpui_component::Theme::change(gpui_component::ThemeMode::Light, None, cx);
            editor::bind_keys(cx);

            let _ = cx.open_window(WindowOptions::default(), |window, cx| {
                mount_canvas_into_stage();
                let workspace = cx.new(|cx| app::Workspace::new(window, cx));
                cx.new(|cx| gpui_component::Root::new(workspace, window, cx))
            });
            cx.activate(true);
        });
}
