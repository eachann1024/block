//! Single-reaction-per-selection semantics, legacy deduplication, and picker/pill actions.
use std::{collections::HashMap, ops::Range};
use gpui::{Context, Window, point, px};
use super::{Annotation, AnnotationTarget, ReactionPicker, resolve_annotation_spans, spans::matches_targets};
use super::super::{model::Doc, view::Editor};

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
enum SpanKey {
    Resolved(Vec<(u64, Range<usize>)>),
    Fallback(String),
}

fn span_key(doc: &Doc, note: &Annotation) -> SpanKey {
    let spans = resolve_annotation_spans(doc, note);
    if spans.len() == note.anchors.len().max(1) {
        SpanKey::Resolved(spans.into_iter().map(|(id, range, _)| (id, range)).collect())
    } else {
        SpanKey::Fallback(serde_json::to_string(&(note.block_index, note.start, note.end, &note.quote, &note.anchors)).unwrap_or_default())
    }
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

    fn find_span_reaction_owner(&self, targets: &[AnnotationTarget]) -> Option<u64> {
        let mut matching = self.annotate.annotations.iter()
            .filter(|note| matches_targets(&self.doc, note, targets));
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
        let block_index = first.block_index;
        let anchor = self.toolbar_anchor
            .or_else(|| self.list.bounds_for_item(block_index).map(|b| point(b.center().x, b.top())))
            .unwrap_or_else(|| {
                let viewport = self.list.viewport_bounds();
                point(viewport.center().x, viewport.top() + px(48.))
            });
        let annotation_id = self.find_span_reaction_owner(&targets);
        self.format = None;
        self.annotate.reactions = Some(ReactionPicker { targets, annotation_id, anchor });
        cx.notify();
    }

    pub(crate) fn pick_reaction(&mut self, emoji: &str, cx: &mut Context<Self>) {
        let Some(picker) = self.annotate.reactions.take() else { return };
        self.apply_single_reaction(picker.annotation_id, Some(&picker.targets), emoji);
        self.normalize_annotations();
        self.persist_annotations();
        self.caret.anchor = self.caret.head;
        self.table_sel = None;
        self.toolbar_anchor = None;
        cx.notify();
    }

    pub(crate) fn toggle_reaction_on_note(&mut self, id: u64, emoji: &str, cx: &mut Context<Self>) {
        self.apply_single_reaction(Some(id), None, emoji);
        self.normalize_annotations();
        self.persist_annotations();
        cx.notify();
    }

    fn apply_single_reaction(
        &mut self,
        target_id: Option<u64>,
        targets: Option<&[AnnotationTarget]>,
        emoji: &str,
    ) {
        let matching_ids: Vec<u64> = self.annotate.annotations.iter()
            .filter(|note| {
                Some(note.id) == target_id
                    || targets.is_some_and(|targets| matches_targets(&self.doc, note, targets))
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
                    } else if matching_ids.contains(&note.id) {
                        note.reactions.clear();
                    }
                }
            } else if let Some(targets) = targets
                && let Some(mut note) = Annotation::for_targets(targets, String::new())
            {
                note.reactions.push(emoji.to_owned());
                self.annotate.annotations.push(note);
            }
        }
    }
}
