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

fn snapshot(editor: &Editor, selected_only: bool, source: &crate::export::SourceContext) -> Result<Snapshot, ExportError> {
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
    for block in &mut blocks {
        if let Some(table) = &mut block.meta.table
            && let Some(geometry) = source.block(block.id).and_then(|b| b.table.as_ref()) {
            table.column_widths = geometry.columns.iter().map(|w| Some(format!("{w}px"))).collect();
        }
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

fn capture_source(editor: &Editor, editor_id: EntityId, window: &mut Window) -> crate::export::SourceContext {
    let zoom = super::zoom::zoom_factor(editor.minimal);
    let base = source_width(editor);
    let blocks = editor.doc.blocks.iter().map(|block| {
        let width = (base - px(super::chrome::block_gutter(block, zoom) + super::chrome::body_padding(block, zoom))).max(px(1.));
        let logical_width = f32::from(width) / zoom;
        let table = block.meta.table.as_ref().map(|table| table_layout::export_geometry(table, width,
            &table_layout::widths_of(editor_id, block.id), window, zoom));
        let image_width = super::image_source::spec_of(block).map(|spec|
            spec.width.map_or(logical_width, |w| (w as f32).clamp(logical_width * 0.1, logical_width)));
        (block.id, crate::export::BlockLayout {
            width: logical_width, image_width, table,
            nowrap: editor.nowrap.contains(&block.id),
            show_source: super::code_block_preview::is_source_open(editor_id, block.id, &block.text),
        })
    }).collect();
    crate::export::SourceContext { note: editor.current_path().map(std::path::Path::to_path_buf), font_family: crate::theme::editor_font().0.to_string(), blocks }
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
    let source = capture_source(editor, cx.entity_id(), window);
    // A selection that is empty or over budget simply disables the "选中生成" option.
    let selection = if editor.export_block_count(true) > 0 {
        Some(snapshot(editor, true, &source).unwrap_or_else(|error| Err(error.to_string())))
    } else {
        None
    };
    let whole = match snapshot(editor, false, &source) {
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
    let view = cx.new(|cx| ExportView::new(title, source, whole, selection, prefer_selection, restore, cx));
    view.update(cx, |view, cx| view.request(cx));
    let focus = view.read(cx).focus_handle(cx);
    cx.set_global(ExportSlot(Some(view)));
    window.focus(&focus, cx);
}
