//! Read-only probes for the `--smoke` run; they expose state without widening field visibility.
use super::{markdown, model::Kind, view::Editor};

impl Editor {
    pub(crate) fn slash_menu_open(&self) -> bool { self.block_menu.is_some() }
    pub(crate) fn markdown_text(&self) -> String { markdown::serialize(&self.doc) }
    pub(crate) fn block_count(&self) -> usize { self.doc.blocks.len() }
    pub(crate) fn first_line_delete_armed(&self) -> bool {
        self.doc.blocks.first().is_some_and(|b| self.is_first_line_delete_armed(b.id))
    }
    pub(crate) fn first_block_text(&self) -> String {
        self.doc.blocks.first().map(|b| b.text.clone()).unwrap_or_default()
    }
    pub(crate) fn has_todo_checked(&self, expected: bool) -> bool {
        self.doc.blocks.iter().any(|b| b.kind == Kind::Todo && b.checked == expected)
    }
    pub(crate) fn find_is_open(&self) -> bool { self.find.is_some() }
    pub(crate) fn find_match_count(&self) -> usize { self.find.as_ref().map_or(0, |f| f.matches.len()) }
    pub(crate) fn find_current_index(&self) -> usize { self.find.as_ref().map_or(0, |f| f.current) }
    pub(crate) fn has_selection_toolbar(&self) -> bool { self.toolbar_anchor.is_some() && !self.caret.is_empty() }
}
