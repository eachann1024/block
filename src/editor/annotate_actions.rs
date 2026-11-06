//! User interactions on annotation cards: draft edit/delete, review accept/reject/regenerate,
//! follow-up submission, and editor-to-rail focus synchronization.
use gpui::{Context, Window, prelude::*};
use gpui_component::input::{InputEvent, InputState};
use crate::theme::tr;
use super::{AnnotationEdit, AnnotationFollowup, ai, resolve_annotation_span, resolve_annotation_spans};
use super::super::view::{Editor, selection::Position};

impl Editor {
    /// Returns `(draft_count, any_streaming, has_any_comment_annotation)`.
    pub(crate) fn annotation_summary(&self) -> (usize, bool, bool) {
        let drafts = self.annotate.annotations.iter().filter(|note| note.is_pending()).count();
        let generating = self.annotate.annotations.iter().any(|note| note.streaming);
        let has_any = self.annotate.annotations.iter().any(|note| !note.is_applied() && !note.body.trim().is_empty());
        (drafts, generating, has_any)
    }

    pub(crate) fn toggle_annotation_detail(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        if self.annotate.active_annotation == Some(id) {
            self.annotate.active_annotation = None;
            self.annotate.followup = None;
            self.annotate.editing = None;
            window.focus(&self.focus, cx);
            cx.notify();
            return;
        }
        let Some(note) = self.annotate.annotations.iter().find(|note| note.id == id).cloned() else { return };
        self.annotate.active_annotation = Some(id);
        self.annotate.followup = None;
        self.annotate.editing = None;
        self.toolbar_anchor = None;
        if let Some((block_id, range, _)) = resolve_annotation_span(&self.doc, &note) {
            self.ensure_block_visible(block_id);
            self.caret.anchor = Position { block: block_id, offset: range.start };
            self.caret.head = self.caret.anchor;
            self.table_sel = None;
            if !note.body.is_empty() { self.reveal_caret = true; }
        }
        cx.notify();
    }

    pub(crate) fn hover_annotation(&mut self, id: u64, hovered: bool, cx: &mut Context<Self>) {
        self.annotate.hover_seq = self.annotate.hover_seq.wrapping_add(1);
        let seq = self.annotate.hover_seq;
        if hovered {
            self.link_ui.reset_hover();
            if self.annotate.hovered_annotation != Some(id) {
                self.annotate.hovered_annotation = Some(id);
                cx.notify();
            }
            return;
        }
        if self.annotate.hovered_annotation != Some(id) {
            return;
        }
        let timer = cx.background_executor().timer(std::time::Duration::from_millis(150));
        cx.spawn(async move |this, cx| {
            timer.await;
            this.update(cx, |editor, cx| {
                if editor.annotate.hover_seq == seq && editor.annotate.hovered_annotation == Some(id) {
                    editor.annotate.hovered_annotation = None;
                    cx.notify();
                }
            })
            .ok();
        })
        .detach();
    }

    pub(in crate::editor) fn annotation_at(&self, pos: Position) -> Option<u64> {
        self.annotate.annotations.iter().rev()
            .filter(|note| !note.body.is_empty() && !note.is_applied())
            .find_map(|note| {
                let (block_id, range, _) = resolve_annotation_spans(&self.doc, note).into_iter().find(|(id, _, _)| *id == pos.block)?;
                if block_id != pos.block { return None; }
                let inside = super::is_whole_table_span(&self.doc, block_id, &range)
                    || (pos.offset >= range.start && pos.offset < range.end);
                inside.then_some(note.id)
            })
    }

    pub(in crate::editor) fn activate_target_annotation(&mut self, id: u64, _window: &mut Window, _cx: &mut Context<Self>) {
        if self.annotate.active_annotation == Some(id) {
            self.annotate.active_annotation = None;
            self.annotate.followup = None;
            self.annotate.editing = None;
            return;
        }
        if self.annotate.annotations.iter().any(|note| note.id == id) {
            self.annotate.active_annotation = Some(id);
            self.annotate.followup = None;
            self.annotate.editing = None;
        }
    }

    pub(crate) fn start_edit_annotation(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        let Some(body) = self.annotate.annotations.iter().find(|n| n.id == id).map(|n| n.body.clone()) else { return };
        let input = cx.new(|cx| InputState::new(window, cx).default_value(body));
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| match event {
            InputEvent::PressEnter { secondary: false, .. } => this.save_edit_annotation(window, cx),
            InputEvent::Change => cx.notify(),
            _ => {}
        });
        let focus_input = input.clone();
        self.annotate.active_annotation = Some(id);
        self.annotate.followup = None;
        self.annotate.editing = Some(AnnotationEdit { annotation_id: id, input, _sub: sub });
        cx.defer_in(window, move |_, window, cx| { focus_input.update(cx, |s, cx| s.focus(window, cx)); });
        cx.notify();
    }

    pub(crate) fn save_edit_annotation(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(edit) = self.annotate.editing.take() else { return };
        let text = edit.input.read(cx).value().trim().to_owned();
        if !text.is_empty()
            && let Some(note) = self.annotate.annotations.iter_mut().find(|n| n.id == edit.annotation_id)
        {
            note.body = text;
            self.persist_annotations();
        }
        window.focus(&self.focus, cx);
        cx.notify();
    }

    pub(crate) fn cancel_edit_annotation(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.annotate.editing.take().is_some() {
            window.focus(&self.focus, cx);
            cx.notify();
        }
    }

    pub(crate) fn delete_annotation(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        if let Some(note) = self.annotate.annotations.iter_mut().find(|n| n.id == id) {
            note.body.clear();
            note.ai_rewritten = None;
            note.ai_error = None;
            note.applied = Some(false);
            note.streaming = false;
            note.request_id = None;
        }
        self.normalize_annotations();
        self.persist_annotations();
        window.focus(&self.focus, cx);
        cx.notify();
    }

    pub(crate) fn open_reject_annotation(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        if !self.annotate.annotations.iter().any(|n| n.id == id) { return; }
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(tr("哪里不满意？", "What needs improvement?")));
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| match event {
            InputEvent::PressEnter { secondary: false, .. } => this.submit_annotation_followup(window, cx),
            InputEvent::Change => cx.notify(),
            _ => {}
        });
        let focus_input = input.clone();
        self.annotate.active_annotation = Some(id);
        self.annotate.editing = None;
        self.annotate.followup = Some(AnnotationFollowup { annotation_id: id, input, _sub: sub });
        cx.defer_in(window, move |_, window, cx| { focus_input.update(cx, |s, cx| s.focus(window, cx)); });
        cx.notify();
    }

    pub(crate) fn close_annotation_followup(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.annotate.followup.take().is_some() {
            window.focus(&self.focus, cx);
            cx.notify();
        }
    }

    pub(crate) fn submit_annotation_followup(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(followup) = self.annotate.followup.take() else { return };
        let id = followup.annotation_id;
        let follow_text = followup.input.read(cx).value().trim().to_owned();
        let Some(note) = self.annotate.annotations.iter_mut().find(|n| n.id == id && !n.is_applied() && !n.streaming) else {
            cx.notify();
            return;
        };
        if !follow_text.is_empty() {
            note.body = follow_text.clone();
        }
        let task = ai::RewriteTask {
            id,
            selected_text: note.quote.clone(),
            annotation: note.body.clone(),
            previous_rewrite: note.ai_rewritten.clone(),
            follow_up: (!follow_text.is_empty()).then_some(follow_text),
        };
        window.focus(&self.focus, cx);
        self.start_annotation_request(vec![task], cx);
    }

    pub(crate) fn regenerate_annotation(&mut self, id: u64, cx: &mut Context<Self>) {
        let Some(note) = self.annotate.annotations.iter().find(|n| n.id == id && !n.is_applied() && !n.streaming) else { return };
        let task = ai::RewriteTask {
            id,
            selected_text: note.quote.clone(),
            annotation: note.body.clone(),
            previous_rewrite: note.ai_rewritten.clone(),
            follow_up: None,
        };
        self.annotate.followup = None;
        self.start_annotation_request(vec![task], cx);
    }

    pub(crate) fn retry_annotation(&mut self, id: u64, cx: &mut Context<Self>) {
        let Some(note) = self.annotate.annotations.iter().find(|n| n.id == id && !n.streaming) else { return };
        let task = ai::RewriteTask {
            id,
            selected_text: note.quote.clone(),
            annotation: note.body.clone(),
            previous_rewrite: note.ai_rewritten.clone(),
            follow_up: None,
        };
        self.start_annotation_request(vec![task], cx);
    }
}
