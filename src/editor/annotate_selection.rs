//! Capture every selected block before the composer or reaction picker takes focus.
use std::ops::Range;
use gpui::{Context, Window, prelude::*};
use gpui_component::input::{InputEvent, InputState};
use crate::theme::tr;
use super::{AnnotateComposer, Annotation, next_id};
use crate::editor::view::Editor;

#[derive(Clone)]
pub(in crate::editor) struct AnnotationTarget {
    pub block_id: u64,
    pub block_index: usize,
    pub range: Range<usize>,
    pub quote: String,
}

impl Editor {
    pub(in crate::editor) fn selected_annotation_targets(&self) -> Vec<AnnotationTarget> {
        let (_, end, first, last) = self.ordered();
        (first..=last).filter_map(|index| {
            if first != last && index == last && end.offset == 0 { return None; }
            let block = self.doc.blocks.get(index)?;
            if let Some(spec) = crate::editor::image_source::spec_of(block) {
                let label = if spec.alt.trim().is_empty() { spec.src } else { spec.alt };
                return Some(AnnotationTarget { block_id: block.id, block_index: index, range: 0..0, quote: format!("[Image: {label}]") });
            }
            let range = if self.caret.is_empty() { 0..block.text.len() } else { self.selected_range(block.id)? };
            let quote = block.text.get(range.clone())?.to_owned();
            (!quote.trim().is_empty()).then_some(AnnotationTarget { block_id: block.id, block_index: index, range, quote })
        }).collect()
    }

    /// Toolbar 「添加批注」: highlight the selection and open the composer.
    pub(in crate::editor) fn start_annotation(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.is_markdown_note() { return; }
        self.sync_annotations();
        let targets = self.selected_annotation_targets();
        let Some(first) = targets.first() else { return };
        let (block_id, range) = (first.block_id, first.range.clone());
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(tr("输入批注或修改指令…", "Add an annotation or edit instruction…")));
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| match event {
            InputEvent::PressEnter { secondary: false, .. } => this.submit_annotation(window, cx),
            InputEvent::Change => cx.notify(),
            _ => {}
        });
        let focus_input = input.clone();
        self.annotate.reactions = None;
        self.format = None;
        self.annotate.composer = Some(AnnotateComposer { block_id, range, targets, input, _sub: sub });
        cx.defer_in(window, move |_, window, cx| { focus_input.update(cx, |i, cx| i.focus(window, cx)); });
        cx.notify();
    }

    pub(in crate::editor) fn close_annotation_composer(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.annotate.composer = None;
        window.focus(&self.focus, cx);
        cx.notify();
    }

    pub(in crate::editor) fn submit_annotation(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(composer) = &self.annotate.composer else { return };
        let body = composer.input.read(cx).value().trim().to_owned();
        if body.is_empty() { return; }
        let Some(composer) = self.annotate.composer.take() else { return };
        for target in composer.targets {
            self.annotate.annotations.push(Annotation {
                id: next_id(), block_index: target.block_index, start: target.range.start, end: target.range.end,
                quote: target.quote, body: body.clone(), reactions: Vec::new(), ai_rewritten: None, ai_error: None,
                applied: Some(false), streaming: false, request_id: None,
            });
        }
        self.persist_annotations();
        self.caret.anchor = self.caret.head; // collapse selection so the floating toolbar doesn't reappear
        self.table_sel = None;
        self.toolbar_anchor = None;
        window.focus(&self.focus, cx);
        cx.notify();
    }
}

impl Editor {
    pub(crate) fn smoke_annotations_persisted(&self) -> bool {
        let saved = self.path.as_deref().map(super::load_for).unwrap_or_default();
        serde_json::to_value(saved).ok() == serde_json::to_value(&self.annotate.annotations).ok()
    }
}
