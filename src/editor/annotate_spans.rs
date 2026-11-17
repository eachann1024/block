//! One annotation owns an ordered collection of selected block fragments.
use std::ops::Range;
use serde::{Deserialize, Serialize};
use super::{Annotation, AnnotationTarget, find_snippet, next_id, resolve_annotation_span};
use crate::editor::model::Doc;

#[derive(Clone, Debug, Serialize, Deserialize)]
pub(in crate::editor) struct AnnotationAnchor {
    pub block_index: usize,
    pub start: usize,
    pub end: usize,
    pub quote: String,
}

impl Annotation {
    pub(super) fn for_targets(targets: &[AnnotationTarget], body: String) -> Option<Self> {
        let first = targets.first()?;
        let anchors = if targets.len() > 1 { targets.iter().map(|t| AnnotationAnchor {
            block_index: t.block_index, start: t.range.start, end: t.range.end, quote: t.quote.clone(),
        }).collect() } else { Vec::new() };
        Some(Self {
            id: next_id(), block_index: first.block_index, start: first.range.start, end: first.range.end,
            quote: targets.iter().map(|t| t.quote.as_str()).collect::<Vec<_>>().join("\n"), body, anchors,
            reactions: Vec::new(), ai_rewritten: None, ai_error: None, applied: Some(false),
            streaming: false, request_id: None,
        })
    }
}

pub(super) fn resolve_anchor(doc: &Doc, anchor: &AnnotationAnchor) -> Option<(u64, Range<usize>, bool)> {
    find_snippet(doc, anchor.block_index, anchor.start, &anchor.quote)
        .map(|(id, range)| (id, range, false))
        .or_else(|| doc.blocks.get(anchor.block_index).filter(|b| crate::editor::image_source::spec_of(b).is_some())
            .map(|b| (b.id, 0..0, false)))
}

pub(in crate::editor) fn resolve_annotation_spans(doc: &Doc, note: &Annotation) -> Vec<(u64, Range<usize>, bool)> {
    if note.anchors.is_empty() { return resolve_annotation_span(doc, note).into_iter().collect(); }
    let applied = note.is_applied();
    let mut used = Vec::with_capacity(note.anchors.len());
    note.anchors.iter().filter_map(|anchor| {
        if let Some(block) = doc.blocks.get(anchor.block_index)
            && !used.contains(&block.id)
            && let Some(range) = super::find_in_text(&block.text, anchor.start, &anchor.quote)
        {
            used.push(block.id);
            return Some((block.id, range, applied));
        }
        if let Some((_, _, id, range)) = doc.blocks.iter().enumerate()
            .filter(|(_, b)| !used.contains(&b.id))
            .filter_map(|(idx, b)| {
                super::find_in_text(&b.text, anchor.start, &anchor.quote)
                    .map(|range| (idx.abs_diff(anchor.block_index), range.start.abs_diff(anchor.start), b.id, range))
            })
            .min_by_key(|(block_dist, start_dist, _, _)| (*block_dist, *start_dist))
        {
            used.push(id);
            return Some((id, range, applied));
        }
        doc.blocks.get(anchor.block_index)
            .filter(|b| !used.contains(&b.id) && crate::editor::image_source::spec_of(b).is_some())
            .map(|b| { used.push(b.id); (b.id, 0..0, applied) })
    }).collect()
}

pub(super) fn matches_targets(doc: &Doc, note: &Annotation, targets: &[AnnotationTarget]) -> bool {
    let spans = resolve_annotation_spans(doc, note);
    spans.len() == targets.len() && spans.iter().zip(targets)
        .all(|((id, range, _), target)| *id == target.block_id && *range == target.range)
}
