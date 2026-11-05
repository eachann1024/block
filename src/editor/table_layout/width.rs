//! Session-only table widths: the "fit to page width" switch and columns the user dragged to a width.
//! Nothing here reaches the document or the saved Markdown; a reopened file measures by content again.
use std::{cell::RefCell, collections::HashMap, hash::{Hash, Hasher}};
use gpui::EntityId;
use super::MIN_COL;

/// Upper bound for a hand-set column.
pub(in crate::editor) const MAX_MANUAL: f32 = 1200.;

#[derive(Clone, Default, PartialEq)]
pub(in crate::editor) struct Widths {
    /// Stretch the free columns so the grid exactly fills the visible width.
    pub fit: bool,
    /// Column count the entries in `manual` were set for; they are ignored once the table changes shape.
    columns: usize,
    manual: Vec<Option<f32>>,
}
impl Widths {
    /// Width the user gave column `index` of a table that has `columns` columns.
    pub fn manual_at(&self, index: usize, columns: usize) -> Option<f32> {
        if self.columns != columns { return None; }
        self.manual.get(index).copied().flatten()
    }
    fn signature(&self) -> u64 {
        let mut h = std::collections::hash_map::DefaultHasher::new();
        (self.fit, self.columns).hash(&mut h);
        for value in &self.manual { value.map(f32::to_bits).hash(&mut h); }
        h.finish()
    }
}
thread_local! {
    static STATE: RefCell<HashMap<(EntityId, u64), Widths>> = RefCell::default();
}

pub(in crate::editor) fn get(editor: EntityId, block: u64) -> Widths {
    STATE.with(|s| s.borrow().get(&(editor, block)).cloned().unwrap_or_default())
}
/// Folded into the layout cache key, so any change here re-measures the table.
pub(in crate::editor) fn signature(editor: EntityId, block: u64) -> u64 {
    STATE.with(|s| s.borrow().get(&(editor, block)).map_or(0, Widths::signature))
}
fn edit(editor: EntityId, block: u64, change: impl FnOnce(&mut Widths)) {
    STATE.with(|s| change(s.borrow_mut().entry((editor, block)).or_default()));
}
pub(in crate::editor) fn toggle_fit(editor: EntityId, block: u64) {
    edit(editor, block, |w| w.fit = !w.fit);
}
/// Sets (or with `None` clears) the width of one column of a table with `columns` columns.
pub(in crate::editor) fn set_manual(editor: EntityId, block: u64, index: usize, columns: usize, width: Option<f32>) {
    edit(editor, block, |w| {
        if w.columns != columns { w.manual.clear(); w.columns = columns; }
        if w.manual.len() < columns { w.manual.resize(columns, None); }
        if let Some(slot) = w.manual.get_mut(index) { *slot = width.map(|v| v.clamp(MIN_COL, MAX_MANUAL)); }
    });
}
/// Structure edits (insert, delete, move columns) drop the hand-set widths so none can end up on the wrong column.
pub(in crate::editor) fn reset_manual(editor: EntityId, block: u64) {
    edit(editor, block, |w| { w.manual.clear(); w.columns = 0; });
}

/// Spreads `target` over the columns that are not `fixed`, in proportion to their content widths,
/// never going below `min_col`; fixed columns keep their width.
pub(super) fn fit(content: &[f32], fixed: &[bool], target: f32, min_col: f32) -> Vec<f32> {
    let mut out = content.to_vec();
    let mut pool: Vec<usize> = (0..content.len()).filter(|i| !fixed[*i]).collect();
    let mut room = target - (0..content.len()).filter(|i| fixed[*i]).map(|i| content[i]).sum::<f32>();
    while !pool.is_empty() {
        let scale = room / pool.iter().map(|i| content[*i]).sum::<f32>();
        let (small, rest): (Vec<usize>, Vec<usize>) = pool.iter().partition(|i| content[**i] * scale < min_col);
        if small.is_empty() {
            for i in pool { out[i] = content[i] * scale; }
            break;
        }
        for i in small { out[i] = min_col; room -= min_col; }
        pool = rest;
    }
    out
}
