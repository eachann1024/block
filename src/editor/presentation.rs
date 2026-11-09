//! Per-note presentation travels with tab sessions; measurement caches never cross documents.
use std::collections::HashMap;
use gpui::{App, EntityId};
use super::{code_block_preview, table_layout};

#[derive(Default)]
pub(super) struct State {
    tables: HashMap<u64, table_layout::Widths>,
    sources: HashMap<u64, bool>,
    scroll: HashMap<u64, table_layout::scroll::Scroll>,
}

pub(super) fn take(editor: EntityId, cx: &mut App) -> State {
    let state = State {
        tables: table_layout::width::take(editor),
        sources: code_block_preview::take_sources(editor),
        scroll: table_layout::scroll::take(editor),
    };
    clear(editor, cx);
    state
}

pub(super) fn restore(editor: EntityId, state: State, cx: &mut App) {
    clear(editor, cx);
    table_layout::width::restore(editor, state.tables);
    code_block_preview::restore_sources(editor, state.sources);
    table_layout::scroll::restore(editor, state.scroll);
}

pub(super) fn clear(editor: EntityId, cx: &mut App) {
    table_layout::width::take(editor);
    table_layout::scroll::clear_editor(editor);
    code_block_preview::take_sources(editor);
    super::diagram_preview::clear_editor(editor);
    super::math_preview::clear_editor(editor);
    super::image_view::clear_editor_measured(editor, cx);
    super::view::layout::clear_editor_keys(editor);
}
