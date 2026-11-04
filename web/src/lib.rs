//! WebAssembly entry point compiling the real Thinkly Fast GPUI editor (`src/editor`)
//! for browser embedding (`#editor-stage`).
#![allow(dead_code, unused_imports)]
use std::borrow::Cow;
use std::path::PathBuf;
use std::rc::Rc;

use gpui::{
    App, AppContext, Application, Entity, Focusable, IntoElement, ParentElement, Render, Styled,
    Window, WindowOptions, div,
};
use gpui_web::{CanvasFontFallback, WebBackendPreference, WebPlatform};
use wasm_bindgen::prelude::*;

mod demo;
#[path = "../../src/icons.rs"]
mod icons;
#[path = "../../src/theme.rs"]
mod theme;
#[path = "../../src/ui/mod.rs"]
pub mod ui;
pub use ui::dropdown;
#[path = "../../src/editor/mod.rs"]
mod editor;

pub mod app {
    pub struct Workspace;
    impl Workspace {
        pub fn dismiss_dropdowns(&mut self, _cx: &mut gpui::Context<Self>) -> bool {
            false
        }
    }
}

pub mod mascot {
    pub fn editor_empty(_cx: &mut gpui::Context<super::editor::Editor>) -> gpui::Div {
        gpui::div()
    }
}

#[path = "../../src/files"]
pub mod files {
    pub mod state;
    pub use state::app_data_dir;
    pub mod ops {
        use std::path::{Path, PathBuf};
        pub fn reveal_in_finder(_path: &Path) {}
        pub fn rebase(target: &Path, from: &Path, to: &Path) -> Option<PathBuf> {
            target.strip_prefix(from).ok().map(|rel| to.join(rel))
        }
    }
}

const FONT_PLEX_REGULAR: &[u8] = include_bytes!("../fonts/IBMPlexSans-Regular.ttf");
const FONT_PLEX_SEMIBOLD: &[u8] = include_bytes!("../fonts/IBMPlexSans-SemiBold.ttf");
const FONT_JETBRAINS_MONO: &[u8] = include_bytes!("../fonts/JetBrainsMono-Regular.ttf");

struct WebAssets;
impl gpui::AssetSource for WebAssets {
    fn load(&self, path: &str) -> gpui::Result<Option<Cow<'static, [u8]>>> {
        icons::Icons.load(path)
    }
    fn list(&self, path: &str) -> gpui::Result<Vec<gpui::SharedString>> {
        icons::Icons.list(path)
    }
}

struct WebStage {
    editor: Entity<editor::Editor>,
}

impl Render for WebStage {
    fn render(&mut self, _window: &mut Window, _cx: &mut gpui::Context<Self>) -> impl IntoElement {
        let palette = theme::Theme::ocean();
        div()
            .size_full()
            .relative()
            .flex()
            .flex_col()
            .overflow_hidden()
            .bg(palette.paper)
            .text_color(palette.text)
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .min_h_0()
                    .size_full()
                    .child(self.editor.clone()),
            )
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
                let editor = cx.new(|cx| {
                    let mut ed = editor::Editor::new(cx);
                    ed.open_web_markdown(PathBuf::from("/demo/welcome.md"), demo::DEMO_NOTE, cx);
                    ed
                });
                let focus = editor.focus_handle(cx);
                window.focus(&focus, cx);
                let stage = cx.new(|_| WebStage { editor });
                cx.new(|cx| gpui_component::Root::new(stage, window, cx))
            });
            cx.activate(true);
        });
}
