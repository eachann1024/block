//! Session width changes of a table (fit to page, dragged columns) and the re-measuring they need.
use gpui::Context;
use super::view::Editor;

impl Editor {
    /// Re-measures one table after its session widths changed: the list caches row heights and
    /// wrapping depends on the column widths.
    pub(super) fn relayout_table(&mut self, id: u64, cx: &mut Context<Self>) {
        if let Some(index) = self.doc.index_of(id) { self.list.splice(index..index + 1, 1); }
        self.layouts.remove(&id);
        cx.notify();
    }
}
