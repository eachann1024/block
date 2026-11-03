//! Editor-facing entry: snapshot the note (and selection) and open the "生成图片" view.
//! Snapshots are bounded before cloning; rendering happens on the background executor.
use super::view::Editor;
use crate::export::{ExportError, ExportSlot, ExportView, Snapshot, preflight};
use gpui::{AppContext, Context, Focusable, Window};
use std::sync::Arc;

pub fn export_document(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) {
    open(editor, false, window, cx);
}

pub fn export_selected(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) {
    open(editor, true, window, cx);
}

fn snapshot(editor: &Editor, selected_only: bool) -> Result<Snapshot, ExportError> {
    // Count first: a million-block document is rejected without walking or cloning it.
    let count = editor.export_block_count(selected_only);
    let (bytes, spans) = editor.export_budget(selected_only);
    preflight(count, bytes, spans)?;
    let inputs = editor.export_indices(selected_only).map(|index| {
        let block = &editor.doc.blocks[index];
        let range = if selected_only {
            editor.selected_range(block.id).unwrap_or(0..0)
        } else {
            0..block.text.len()
        };
        (block, range)
    });
    crate::export::validate_snapshot(count, inputs)?;
    Ok(Ok(Arc::new(editor.export_blocks(selected_only))))
}

fn open(
    editor: &mut Editor,
    prefer_selection: bool,
    window: &mut Window,
    cx: &mut Context<Editor>,
) {
    if crate::export::active_view(cx).is_some() {
        return;
    }
    // A selection that is empty or over budget simply disables the "选中生成" option.
    let selection = if editor.export_block_count(true) > 0 {
        Some(snapshot(editor, true).unwrap_or_else(|error| Err(error.to_string())))
    } else {
        None
    };
    let whole = match snapshot(editor, false) {
        Ok(whole) => whole,
        Err(ExportError::Empty) => {
            editor.report_status(ExportError::Empty.to_string(), cx);
            return;
        }
        Err(error) => Err(error.to_string()),
    };
    let title = editor
        .current_path()
        .and_then(|path| path.file_stem())
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| "未命名".into());
    let restore = editor.focus_handle(cx);
    let view = cx.new(|cx| ExportView::new(title, whole, selection, prefer_selection, restore, cx));
    view.update(cx, |view, cx| view.request(cx));
    let focus = view.read(cx).focus_handle(cx);
    cx.set_global(ExportSlot(Some(view)));
    window.focus(&focus);
}
