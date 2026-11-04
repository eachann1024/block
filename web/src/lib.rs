//! WebAssembly entry point compiling the real Thinkly Fast GPUI editor (`src/editor`)
//! for browser embedding (`#editor-stage`).
#![allow(dead_code, unused_imports)]
use std::borrow::Cow;
use std::path::PathBuf;
use std::rc::Rc;

use gpui::{
    App, AppContext, Application, Entity, Focusable, IntoElement, ParentElement, Render, Styled,
    Window, WindowOptions, div, px, InteractiveElement, StatefulInteractiveElement,
};
use icons::icon;
use gpui_web::{CanvasFontFallback, WebBackendPreference, WebPlatform};
use wasm_bindgen::prelude::*;

mod demo;
#[path = "../../src/export/mod.rs"]
mod export;
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

pub mod log {
    pub fn set_context(_ctx: impl Into<String>) {}
    pub fn warn(msg: &str) { web_sys::console::warn_1(&msg.into()); }
    pub fn error(msg: &str) { web_sys::console::error_1(&msg.into()); }
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
        pub fn reveal_label() -> &'static str { "" }
        pub fn rebase(target: &Path, from: &Path, to: &Path) -> Option<PathBuf> {
            target.strip_prefix(from).ok().map(|rel| to.join(rel))
        }
    }
}

const FONT_PLEX_REGULAR: &[u8] = include_bytes!("../fonts/IBMPlexSans-Regular.ttf");
const FONT_PLEX_SEMIBOLD: &[u8] = include_bytes!("../fonts/IBMPlexSans-SemiBold.ttf");
const FONT_JETBRAINS_MONO: &[u8] = include_bytes!("../fonts/JetBrainsMono-Regular.ttf");
/// Fonts bundled into the build; also the font database of the 生成图片 export renderer.
pub(crate) const WEB_FONTS: &[&[u8]] = &[FONT_PLEX_REGULAR, FONT_PLEX_SEMIBOLD, FONT_JETBRAINS_MONO];

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

impl WebStage {
    fn new(editor: Entity<editor::Editor>, cx: &mut gpui::Context<Self>) -> Self {
        // Opening or closing the 生成图片 view swaps the overlay below.
        cx.observe_global::<export::ExportSlot>(|_, cx| cx.notify()).detach();
        Self { editor }
    }

    fn titlebar(&self) -> impl IntoElement {
        let theme = theme::Theme::ocean();
        let (find, export) = (self.editor.clone(), self.editor.clone());
        let tint = theme.icon_colors(theme.text_secondary);
        let action = move |id: &'static str| div().id(id).h(px(28.)).px(px(8.)).flex().items_center().gap(px(6.))
            .rounded(px(6.)).cursor(gpui::CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
            .text_size(px(14.)).text_color(theme.text_secondary);
        div().h(px(40.)).flex_none().flex().items_center().justify_between().px(px(12.))
            .bg(theme.shell).border_b_1().border_color(theme.sidebar_border)
            .child(div().text_size(px(14.)).font_weight(gpui::FontWeight(560.)).text_color(theme.text).child("Thinkly Fast"))
            .child(div().flex().items_center().gap(px(4.))
                .child(action("web-find").child(icon(icons::Icon::FindSearch, px(15.), tint)).child("⌘F")
                    .on_click(move |_, window, cx| find.update(cx, |e, cx| e.open_find(window, cx))))
                .child(action("web-export").child(icon(icons::Icon::Image, px(15.), tint)).child("生成图片 ⇧⌘E")
                    .on_click(move |_, window, cx| export.update(cx, |e, cx| editor::export_document(e, window, cx)))))
    }
}

impl Render for WebStage {
    fn render(&mut self, _window: &mut Window, cx: &mut gpui::Context<Self>) -> impl IntoElement {
        let theme = theme::Theme::ocean();
        div()
            .size_full()
            .relative()
            .flex()
            .flex_col()
            .overflow_hidden()
            .bg(theme.paper)
            .text_color(theme.text)
            .on_action(cx.listener(|this, _: &editor::view::Find, window, cx| {
                this.editor.update(cx, |e, cx| e.open_find(window, cx))
            }))
            .child(self.titlebar())
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .w_full()
                    .flex()
                    .overflow_hidden()
                    .child(div().flex_1().min_w_0().h_full().child(self.editor.clone())),
            )
            .children(export::active_view(cx).map(|view| {
                div().absolute().top_0().left_0().size_full().occlude().bg(theme.shell).child(view)
            }))
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
                let stage = cx.new(|cx| WebStage::new(editor, cx));
                cx.new(|cx| gpui_component::Root::new(stage, window, cx))
            });
            cx.activate(true);
        });
}
