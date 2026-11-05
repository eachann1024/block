//! Read-only probes for the `--smoke` run; they expose state without widening field visibility.
use super::{markdown, model::Kind, view::Editor};

impl Editor {
    pub(crate) fn slash_menu_open(&self) -> bool { self.block_menu.is_some() }
    pub(crate) fn ai_is_open(&self) -> bool { self.ai_session().is_some() }
    pub(crate) fn current_callout(&self) -> Option<super::model::CalloutKind> {
        self.doc.get(self.caret.head.block).and_then(|b| b.meta.callout)
    }
    pub(crate) fn markdown_text(&self) -> String { markdown::serialize(&self.doc) }
    pub(crate) fn block_count(&self) -> usize { self.doc.blocks.len() }
    pub(crate) fn has_todo_checked(&self, expected: bool) -> bool {
        self.doc.blocks.iter().any(|b| b.kind == Kind::Todo && b.checked == expected)
    }
    pub(crate) fn find_is_open(&self) -> bool { self.find.is_some() }
    pub(crate) fn find_match_count(&self) -> usize { self.find.as_ref().map_or(0, |f| f.matches.len()) }
    pub(crate) fn find_current_index(&self) -> usize { self.find.as_ref().map_or(0, |f| f.current) }
    pub(crate) fn has_selection_toolbar(&self) -> bool { self.toolbar_anchor.is_some() && !self.caret.is_empty() }
}
