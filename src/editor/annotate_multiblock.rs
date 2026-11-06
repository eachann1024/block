//! Apply a grouped annotation to its whole selection, never just its first block.
use gpui::{Context, Window};
use crate::{editor::view::{Editor, selection::Position}, theme::tr};
use super::{Annotation, resolve_annotation_spans};

impl Editor {
    pub(super) fn apply_multiblock_annotation(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        let Some(note) = self.annotate.annotations.iter().find(|n| n.id == id && n.is_generated()).cloned() else { return };
        let spans = resolve_annotation_spans(&self.doc, &note);
        let saved_caret = self.caret.clone();
        let valid = spans.len() == note.anchors.len() && spans.iter().zip(&note.anchors).all(|((block, range, _), anchor)|
            self.doc.get(*block).is_some_and(|b| b.text.get(range.clone()) == Some(anchor.quote.as_str())));
        if valid && let (Some(first), Some(last)) = (spans.first(), spans.last()) {
            self.caret.anchor = Position { block: first.0, offset: first.1.start };
            self.caret.head = Position { block: last.0, offset: last.1.end };
            let targets = self.selected_annotation_targets();
            if super::spans::matches_targets(&self.doc, &note, &targets) && self.can_replace_selection() {
                let start = self.caret.anchor;
                let replacement = note.ai_rewritten.as_deref().unwrap_or("");
                self.replace_selection(replacement, false, cx);
                let end = self.caret.head;
                self.caret.anchor = start;
                let rewritten = Annotation::for_targets(&self.selected_annotation_targets(), String::new());
                self.caret.anchor = end;
                if let Some(current) = self.annotate.annotations.iter_mut().find(|n| n.id == id) {
                    if let Some(rewritten) = rewritten {
                        current.block_index = rewritten.block_index;
                        current.start = rewritten.start;
                        current.end = rewritten.end;
                        current.quote = rewritten.quote;
                        current.anchors = rewritten.anchors;
                    }
                    current.applied = Some(true);
                    current.streaming = false;
                    current.request_id = None;
                    current.ai_error = None;
                }
                self.normalize_annotations();
                self.persist_annotations();
                window.focus(&self.focus, cx);
                cx.notify();
                return;
            }
        }
        self.caret = saved_caret;
        if let Some(current) = self.annotate.annotations.iter_mut().find(|n| n.id == id) {
            current.ai_error = Some(tr("原文已变化，无法应用这条批注", "The selected text changed and this annotation cannot be applied").into());
        }
        self.persist_annotations();
        cx.notify();
    }
}
