use std::collections::{BTreeSet, HashSet};
use gpui::{Context, Window};
use crate::{editor::input::transactions::ListChange, theme::tr};
use super::resolve_annotation_span;
use super::super::view::{Editor, selection::Position};

struct RestoreTarget {
    id: u64,
    block_id: u64,
    block_index: usize,
    range: std::ops::Range<usize>,
    original: String,
}

impl Editor {
    pub(crate) fn restore_annotation(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        self.restore_annotations(&[id], window, cx);
    }

    fn restore_annotations(&mut self, ids: &[u64], window: &mut Window, cx: &mut Context<Self>) {
        let _ = window;
        let mut targets = Vec::new();
        let mut invalid = Vec::new();
        let mut changed = false;
        for id in ids {
            let Some(note) = self.annotate.annotations.iter().find(|note| note.id == *id).cloned() else { continue };
            if !note.has_rewrite() || !note.is_applied() { continue; }
            let Some((block_id, range, _)) = resolve_annotation_span(&self.doc, &note) else {
                invalid.push(*id);
                continue;
            };
            let Some(block_index) = self.doc.index_of(block_id) else {
                invalid.push(*id);
                continue;
            };
            let Some(rewritten) = note.ai_rewritten.as_deref() else { continue };
            let valid = self.doc.get(block_id).is_some_and(|block| block.text.get(range.clone()) == Some(rewritten));
            if !valid {
                invalid.push(*id);
                continue;
            }
            targets.push(RestoreTarget { id: *id, block_id, block_index, range, original: note.quote });
        }

        let mut conflicts = HashSet::new();
        targets.sort_by_key(|target| (target.block_index, target.range.start, target.range.end));
        for pair in targets.windows(2) {
            if pair[0].block_id == pair[1].block_id && pair[0].range.end > pair[1].range.start {
                conflicts.insert(pair[0].id);
                conflicts.insert(pair[1].id);
            }
        }
        targets.retain(|target| !conflicts.contains(&target.id));
        for id in invalid {
            if let Some(note) = self.annotate.annotations.iter_mut().find(|note| note.id == id) {
                note.ai_error = Some(tr("正文已变化，无法还原这条批注", "The document changed and this annotation cannot be restored").into());
                changed = true;
            }
        }
        for id in conflicts {
            if let Some(note) = self.annotate.annotations.iter_mut().find(|note| note.id == id) {
                note.ai_error = Some(tr("批注选区重叠，请逐条还原", "Annotations overlap; restore them one at a time").into());
                changed = true;
            }
        }

        if !targets.is_empty() {
            targets.sort_by_key(|target| (std::cmp::Reverse(target.block_index), std::cmp::Reverse(target.range.start)));
            let before = self.snapshot();
            let mut changed_blocks = BTreeSet::new();
            let mut restored_ids = Vec::new();
            let mut caret = None;
            for target in &targets {
                self.doc.replace_range(target.block_id, target.range.clone(), &target.original);
                self.shift_annotation_offsets(target.block_index, target.id, target.range.clone(), target.original.len());
                changed_blocks.insert(target.block_index);
                restored_ids.push(target.id);
                caret = Some(Position { block: target.block_id, offset: target.range.start + target.original.len() });
            }
            if let Some(pos) = caret { self.caret.anchor = pos; self.caret.head = pos; }
            let changes = changed_blocks.into_iter().map(|start| ListChange { start, before: 1, after: 1 }).collect();
            self.record_changes(before, changes, true, cx);
            for note in &mut self.annotate.annotations {
                if restored_ids.contains(&note.id) {
                    note.applied = Some(false);
                    note.streaming = false;
                    note.request_id = None;
                    note.ai_error = None;
                }
            }
            changed = true;
        }
        if changed { self.persist_annotations(); cx.notify(); }
    }
}
