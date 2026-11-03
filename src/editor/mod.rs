mod chrome;
mod input;
mod keys;
pub mod markdown;
pub mod model;
mod paint;
pub mod view;

pub use keys::bind_keys;

use std::path::Path;

use gpui::{Context, Window};

use self::view::Editor;

pub fn export_document(editor: &Editor, window: &mut Window, cx: &mut Context<Editor>) {
    export(editor, false, window, cx);
}

pub fn export_selected(editor: &Editor, window: &mut Window, cx: &mut Context<Editor>) {
    export(editor, true, window, cx);
}

fn export(editor: &Editor, selected_only: bool, window: &mut Window, cx: &mut Context<Editor>) {
    let blocks = editor.export_blocks(selected_only);
    let directory = std::env::var_os("HOME").map(std::path::PathBuf::from).unwrap_or_default();
    let prompt = cx.prompt_for_new_path(Path::new(&directory), Some("note.png"));
    cx.spawn_in(window, async move |_, _cx| {
        let Ok(Ok(Some(path))) = prompt.await else { return };
        let path = path.with_extension("png");
        if let Some(bytes) = crate::export::render_png(&blocks) {
            let _ = std::fs::write(path, bytes);
        }
    })
    .detach();
}
