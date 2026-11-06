//! Single-reaction-per-selection semantics, legacy deduplication, and picker/pill actions.
use std::{collections::HashMap, ops::Range};
use gpui::{Context, Window, point, px};
use super::{Annotation, ReactionPicker, next_id, resolve_annotation_span};
use super::super::{model::Doc, view::Editor};

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
enum SpanKey {
    Resolved(u64, Range<usize>),
    Fallback(usize, Range<usize>, String),
}

fn span_key(doc: &Doc, note: &Annotation) -> SpanKey {
    if let Some((block_id, range, _)) = resolve_annotation_span(doc, note) {
        SpanKey::Resolved(block_id, range)
    } else {
        SpanKey::Fallback(note.block_index, note.start..note.end, note.quote.clone())
    }
}

pub(super) fn matches_span(doc: &Doc, note: &Annotation, block_id: u64, range: &Range<usize>) -> bool {
    resolve_annotation_span(doc, note)
        .is_some_and(|(id, resolved, _)| id == block_id && &resolved == range)
}

impl Annotation {
    pub(super) fn has_non_reaction_data(&self) -> bool {
        !self.body.is_empty()
            || self.ai_rewritten.is_some()
            || self.ai_error.is_some()
            || self.streaming
            || self.request_id.is_some()
    }
}

impl Editor {
    /// Enforces at most one reaction per resolved selection span and at most one emoji per note,
    /// preserving comments, AI rewrites, errors, and in-flight requests.
    pub(super) fn normalize_annotations(&mut self) -> bool {
        let mut changed = false;
        for note in &mut self.annotate.annotations {
            if note.is_applied() && note.has_non_reaction_data() {
                note.body.clear();
                note.ai_rewritten = None;
                note.ai_error = None;
                note.streaming = false;
                note.request_id = None;
                changed = true;
            }
            if note.reactions.len() > 1 {
                note.reactions.truncate(1);
                changed = true;
            }
        }
        let mut chosen_owner: HashMap<SpanKey, u64> = HashMap::new();
        for note in &self.annotate.annotations {
            if note.reactions.is_empty() { continue; }
            let key = span_key(&self.doc, note);
            match chosen_owner.get(&key).copied() {
                None => { chosen_owner.insert(key, note.id); }
                Some(existing_id) => {
                    let existing_has_data = self.annotate.annotations.iter()
                        .find(|n| n.id == existing_id)
                        .is_some_and(Annotation::has_non_reaction_data);
                    if !existing_has_data && note.has_non_reaction_data() {
                        chosen_owner.insert(key, note.id);
                    }
                }
            }
        }
        for note in &mut self.annotate.annotations {
            if note.reactions.is_empty() { continue; }
            let key = span_key(&self.doc, note);
            if chosen_owner.get(&key) != Some(&note.id) {
                note.reactions.clear();
                changed = true;
            }
        }
        let before = self.annotate.annotations.len();
        self.annotate.annotations.retain(|note| note.has_non_reaction_data() || !note.reactions.is_empty());
        if self.annotate.annotations.len() != before {
            changed = true;
        }
        self.cleanup_stale_annotation_refs();
        changed
    }

    fn cleanup_stale_annotation_refs(&mut self) {
        if let Some(active) = self.annotate.active_annotation
            && !self.annotate.annotations.iter().any(|n| n.id == active)
        {
            self.annotate.active_annotation = None;
            self.annotate.followup = None;
        }
        if let Some(edit) = &self.annotate.editing
            && !self.annotate.annotations.iter().any(|n| n.id == edit.annotation_id)
        {
            self.annotate.editing = None;
        }
        if let Some(hovered) = self.annotate.hovered_annotation
            && !self.annotate.annotations.iter().any(|n| n.id == hovered)
        {
            self.annotate.hovered_annotation = None;
        }
    }

    fn find_span_reaction_owner(&self, block_id: u64, range: &Range<usize>) -> Option<u64> {
        let mut matching = self.annotate.annotations.iter()
            .filter(|note| matches_span(&self.doc, note, block_id, range));
        let first = matching.next()?;
        if !first.reactions.is_empty() {
            return Some(first.id);
        }
        matching.find(|note| !note.reactions.is_empty()).or(Some(first)).map(|note| note.id)
    }

    /// Toolbar 「添加表情回应」: open the reaction picker for the selection.
    pub(crate) fn open_reactions(&mut self, _window: &mut Window, cx: &mut Context<Self>) {
        self.sync_annotations();
        if self.normalize_annotations() {
            self.persist_annotations();
        }
        let targets = self.selected_annotation_targets();
        let Some(first) = targets.first() else { return };
        let (block_id, block_index, range) = (first.block_id, first.block_index, &first.range);
        let anchor = self.toolbar_anchor
            .or_else(|| self.list.bounds_for_item(block_index).map(|b| point(b.center().x, b.top())))
            .unwrap_or_else(|| point(px(360.), px(140.)));
        let annotation_id = self.find_span_reaction_owner(block_id, range);
        self.format = None;
        self.annotate.reactions = Some(ReactionPicker { targets, annotation_id, anchor });
        cx.notify();
    }

    pub(crate) fn pick_reaction(&mut self, emoji: &str, cx: &mut Context<Self>) {
        let Some(picker) = self.annotate.reactions.take() else { return };
        // Mixed selections converge to one emoji; only an entirely matching selection toggles off.
        let remove = picker.targets.iter().all(|target| self.annotate.annotations.iter().any(|note|
            matches_span(&self.doc, note, target.block_id, &target.range) && note.reactions.iter().any(|r| r == emoji)));
        for target in picker.targets {
            let owner = self.find_span_reaction_owner(target.block_id, &target.range);
            let already = self.annotate.annotations.iter().any(|note|
                matches_span(&self.doc, note, target.block_id, &target.range) && note.reactions.iter().any(|r| r == emoji));
            if remove || !already {
                let index = self.doc.index_of(target.block_id).unwrap_or(target.block_index);
                self.apply_single_reaction(owner, Some((target.block_id, index, target.range, target.quote)), emoji);
            }
        }
        self.normalize_annotations();
        self.persist_annotations();
        self.caret.anchor = self.caret.head;
        self.table_sel = None;
        self.toolbar_anchor = None;
        cx.notify();
    }

    pub(crate) fn toggle_reaction_on_note(&mut self, id: u64, emoji: &str, cx: &mut Context<Self>) {
        let span = self.annotate.annotations.iter().find(|n| n.id == id).and_then(|note| {
            let (block_id, range, _) = resolve_annotation_span(&self.doc, note)?;
            let block_index = self.doc.index_of(block_id).unwrap_or(note.block_index);
            Some((block_id, block_index, range, note.quote.clone()))
        });
        self.apply_single_reaction(Some(id), span, emoji);
        self.normalize_annotations();
        self.persist_annotations();
        cx.notify();
    }

    fn apply_single_reaction(
        &mut self,
        target_id: Option<u64>,
        span: Option<(u64, usize, Range<usize>, String)>,
        emoji: &str,
    ) {
        let matching_ids: Vec<u64> = self.annotate.annotations.iter()
            .filter(|note| {
                Some(note.id) == target_id
                    || span.as_ref().is_some_and(|(block_id, _, range, _)| matches_span(&self.doc, note, *block_id, range))
            })
            .map(|note| note.id)
            .collect();
        let same_emoji = self.annotate.annotations.iter().any(|note| {
            matching_ids.contains(&note.id) && note.reactions.iter().any(|r| r == emoji)
        });
        if same_emoji {
            for note in &mut self.annotate.annotations {
                if matching_ids.contains(&note.id) {
                    note.reactions.clear();
                }
            }
        } else {
            let owner_id = target_id.filter(|id| matching_ids.contains(id))
                .or_else(|| self.annotate.annotations.iter().find(|n| matching_ids.contains(&n.id) && !n.reactions.is_empty()).map(|n| n.id))
                .or_else(|| self.annotate.annotations.iter().find(|n| matching_ids.contains(&n.id) && n.has_non_reaction_data()).map(|n| n.id))
                .or_else(|| matching_ids.first().copied());
            if let Some(owner_id) = owner_id {
                for note in &mut self.annotate.annotations {
                    if note.id == owner_id {
                        note.reactions.clear();
                        note.reactions.push(emoji.to_owned());
                        if !note.has_rewrite() {
                            note.applied = Some(false);
                        }
                        if let Some((_, block_index, ref range, _)) = span {
                            note.block_index = block_index;
                            note.start = range.start;
                            note.end = range.end;
                        }
                    } else if matching_ids.contains(&note.id) {
                        note.reactions.clear();
                    }
                }
            } else if let Some((_, block_index, range, quote)) = span {
                self.annotate.annotations.push(Annotation {
                    id: next_id(),
                    block_index,
                    start: range.start,
                    end: range.end,
                    quote,
                    body: String::new(),
                    reactions: vec![emoji.to_owned()],
                    ai_rewritten: None,
                    ai_error: None,
                    applied: Some(false),
                    streaming: false,
                    request_id: None,
                });
            }
        }
    }
}
