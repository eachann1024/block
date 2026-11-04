//! Read-only probes for the `--smoke` run; they expose state without widening field visibility.
use super::{markdown, view::Editor};

impl Editor {
    pub(crate) fn slash_menu_open(&self) -> bool { self.block_menu.is_some() }
    pub(crate) fn markdown_text(&self) -> String { markdown::serialize(&self.doc) }
}
