//! Editor-facing entry: snapshot the note (and selection) and open the "生成图片" view.
//! Snapshots are bounded before cloning; rendering happens on the background executor.
use super::{table_layout, view::Editor};
use crate::export::{ExportError, ExportSlot, ExportView, Snapshot, preflight};
use gpui::{AppContext, Context, EntityId, Focusable, Window, px};
use std::sync::Arc;

pub fn export_document(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) {
    open(editor, false, window, cx);
}

pub fn export_selected(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) {
    open(editor, true, window, cx);
}

fn snapshot(editor: &Editor, selected_only: bool, editor_id: EntityId, window: &mut Window) -> Result<Snapshot, ExportError> {
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
    let mut blocks = editor.export_blocks(selected_only);
    let zoom = super::zoom::zoom_factor(editor.minimal);
    let body_width = source_width(editor);
    for block in &mut blocks {
        let Some(table) = &mut block.meta.table else { continue };
        let original = editor.doc.get(block.id).and_then(|b| b.meta.table.as_ref()).unwrap();
        // Cached bounds can belong to an earlier viewport; re-resolve all columns with the live width.
        let indent = block.meta.indent.min(12) as f32 * super::chrome::INDENT_PER_CHAR * zoom;
        let widths = table_layout::export_column_widths(original, (body_width - px(indent)).max(px(1.)),
            &table_layout::widths_of(editor_id, block.id), window, zoom);
        table.column_widths = widths.into_iter().map(|w| Some(format!("{w}px"))).collect();
    }
    Ok(Ok(Arc::new(blocks)))
}

fn source_width(editor: &Editor) -> gpui::Pixels {
    let theme = crate::theme::Theme::ocean();
    let right_inset = if editor.minimal { 8. } else { super::chrome::RIGHT_INSET };
    let scrollbar = if editor.minimal { 6. } else { crate::ui::scrollbar::GUTTER };
    let available = (editor.annotate.editor_width - px(scrollbar)).min(super::chrome::standard_width(theme.content_width));
    let body_width = (available - px(super::chrome::HANDLE_WIDTH + super::chrome::ROW_GAP + right_inset)).max(px(1.));
    if editor.annotate.editor_width > px(0.) { body_width } else { theme.content_width }
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
        Some(snapshot(editor, true, cx.entity_id(), window).unwrap_or_else(|error| Err(error.to_string())))
    } else {
        None
    };
    let whole = match snapshot(editor, false, cx.entity_id(), window) {
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
        .unwrap_or_else(|| crate::theme::tr("未命名", "Untitled").into());
    let restore = editor.focus_handle(cx);
    editor.clear_transient_overlays();
    let source = crate::export::SourceContext {
        note: editor.current_path().map(std::path::Path::to_path_buf),
        width: Some((f32::from(source_width(editor)) / super::zoom::zoom_factor(editor.minimal)).round().max(1.) as u32),
    };
    let view = cx.new(|cx| ExportView::new(title, source, whole, selection, prefer_selection, restore, cx));
    view.update(cx, |view, cx| view.request(cx));
    let focus = view.read(cx).focus_handle(cx);
    cx.set_global(ExportSlot(Some(view)));
    window.focus(&focus, cx);
}
