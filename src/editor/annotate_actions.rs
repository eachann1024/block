use std::{collections::{BTreeSet, HashMap, HashSet}, sync::mpsc, time::Duration};
use gpui::{Context, Window, prelude::*};
use gpui_component::input::{InputEvent, InputState};
use crate::{editor::{ai::provider::{self, Event}, input::transactions::ListChange, markdown}, theme::tr};
use super::{AnnotationFollowup, ai, resolve_annotation_span};
use super::super::view::{Editor, selection::Position};

struct ApplyTarget {
    id: u64,
    block_id: u64,
    block_index: usize,
    range: std::ops::Range<usize>,
    replacement: String,
}

impl Editor {
    pub(crate) fn annotation_summary(&self) -> (usize, usize, usize, bool) {
        let pending = self.annotate.annotations.iter().filter(|note| note.is_pending()).count();
        let generated = self.annotate.annotations.iter().filter(|note| note.is_generated()).count();
        let restorable = self.annotate.annotations.iter().filter(|note| note.has_rewrite()).count();
        let generating = self.annotate.annotations.iter().any(|note| note.streaming);
        (pending, generated, restorable, generating)
    }

    pub(crate) fn generate_pending_annotations(&mut self, cx: &mut Context<Self>) {
        let tasks = self.annotate.annotations.iter().filter(|note| note.is_pending() && !note.streaming)
            .map(|note| ai::RewriteTask {
                id: note.id, selected_text: note.quote.clone(), annotation: note.body.clone(),
                previous_rewrite: None, follow_up: None,
            }).collect::<Vec<_>>();
        self.start_annotation_request(tasks, cx);
    }

    pub(crate) fn apply_all_annotations(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let ids = self.annotate.annotations.iter().filter(|note| note.is_generated())
            .map(|note| note.id).collect::<Vec<_>>();
        self.apply_annotations(&ids, window, cx);
    }

    pub(crate) fn toggle_annotation_detail(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        if self.annotate.active_annotation == Some(id) {
            self.annotate.active_annotation = None;
            self.annotate.followup = None;
            window.focus(&self.focus, cx);
            cx.notify();
            return;
        }
        let Some(note) = self.annotate.annotations.iter().find(|note| note.id == id).cloned() else { return };
        if note.is_applied() || note.body.is_empty() {
            self.annotate.active_annotation = Some(id);
            self.annotate.followup = None;
            self.toolbar_anchor = None;
            if let Some((block_id, range, _)) = resolve_annotation_span(&self.doc, &note) {
                self.ensure_block_visible(block_id);
                self.caret.anchor = Position { block: block_id, offset: range.start };
                self.caret.head = self.caret.anchor;
                self.table_sel = None;
            }
            cx.notify();
            return;
        }
        let input = cx.new(|cx| InputState::new(window, cx)
            .placeholder(tr("继续对话微调… ↵", "Continue refining… ↵")));
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| {
            if matches!(event, InputEvent::PressEnter { secondary: false, .. }) {
                this.submit_annotation_followup(window, cx);
            } else if matches!(event, InputEvent::Change) { cx.notify(); }
        });
        self.annotate.active_annotation = Some(id);
        self.annotate.followup = Some(AnnotationFollowup { annotation_id: id, input: input.clone(), _sub: sub });
        self.toolbar_anchor = None;
        if let Some((block_id, range, _)) = resolve_annotation_span(&self.doc, &note) {
            self.ensure_block_visible(block_id);
            self.caret.anchor = Position { block: block_id, offset: range.start };
            self.caret.head = self.caret.anchor;
            self.table_sel = None;
            self.reveal_caret = true;
        }
        cx.defer_in(window, move |_, window, cx| { input.update(cx, |state, cx| state.focus(window, cx)); });
        cx.notify();
    }

    pub(crate) fn apply_annotation(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        self.apply_annotations(&[id], window, cx);
    }

    fn submit_annotation_followup(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some((id, input, follow_up)) = self.annotate.followup.as_ref()
            .filter(|followup| self.annotate.active_annotation == Some(followup.annotation_id))
            .map(|followup| (followup.annotation_id, followup.input.clone(), followup.input.read(cx).value().trim().to_owned()))
        else { return };
        if follow_up.is_empty() { return; }
        let Some(note) = self.annotate.annotations.iter().find(|note| note.id == id && !note.is_applied() && !note.streaming) else { return };
        let task = ai::RewriteTask {
            id, selected_text: note.quote.clone(), annotation: note.body.clone(),
            previous_rewrite: note.ai_rewritten.clone(), follow_up: Some(follow_up),
        };
        input.update(cx, |state, cx| { state.set_value("", window, cx); state.focus(window, cx); });
        self.start_annotation_request(vec![task], cx);
    }

    fn start_annotation_request(&mut self, tasks: Vec<ai::RewriteTask>, cx: &mut Context<Self>) {
        if tasks.is_empty() { return; }
        self.annotate.request_seq = self.annotate.request_seq.wrapping_add(1).max(1);
        let request_id = self.annotate.request_seq;
        let ids = tasks.iter().map(|task| task.id).collect::<Vec<_>>();
        for note in &mut self.annotate.annotations {
            if ids.contains(&note.id) {
                note.streaming = true;
                note.request_id = Some(request_id);
                note.ai_error = None;
            }
        }
        let document = markdown::serialize(&self.doc);
        let receiver = ai::stream(document, tasks);
        self.persist_annotations();
        cx.notify();
        cx.spawn(async move |this, cx| {
            let mut output = String::new();
            loop {
                cx.background_executor().timer(Duration::from_millis(40)).await;
                let mut done = false;
                let updated = this.update(cx, |this, cx| {
                    loop {
                        match receiver.try_recv() {
                            Ok(Event::Thinking(_)) | Ok(Event::ToolCalls(_)) => {}
                            Ok(Event::Delta(text)) => output.push_str(&text),
                            Ok(Event::Done) => {
                                done = true;
                                let result = ai::parse_response(&output);
                                this.finish_annotation_request(request_id, &ids, result, cx);
                                break;
                            }
                            Ok(Event::Failed { message, .. }) => {
                                done = true;
                                this.finish_annotation_error(request_id, &ids, provider::format_error(&message), cx);
                                break;
                            }
                            Err(mpsc::TryRecvError::Empty) => break,
                            Err(mpsc::TryRecvError::Disconnected) => {
                                done = true;
                                this.finish_annotation_error(request_id, &ids, tr("AI 连接已中断", "AI connection was interrupted").into(), cx);
                                break;
                            }
                        }
                    }
                });
                if updated.is_err() || done { break; }
            }
        }).detach();
    }

    fn finish_annotation_request(
        &mut self, request_id: u64, ids: &[u64], result: Result<HashMap<u64, String>, String>, cx: &mut Context<Self>,
    ) {
        let parsed = result.map_err(|message| provider::format_error(&message));
        for id in ids {
            let Some(note) = self.annotate.annotations.iter_mut().find(|note| note.id == *id && note.request_id == Some(request_id)) else { continue };
            note.streaming = false;
            note.request_id = None;
            match parsed.as_ref() {
                Ok(results) if results.contains_key(id) => {
                    note.ai_rewritten = results.get(id).cloned();
                    note.ai_error = None;
                    note.applied = Some(false);
                }
                Ok(_) => note.ai_error = Some(tr("模型未返回这条批注的改写结果", "No rewrite was returned for this annotation").into()),
                Err(message) => note.ai_error = Some(message.clone()),
            }
        }
        self.persist_annotations();
        cx.notify();
    }

    fn finish_annotation_error(&mut self, request_id: u64, ids: &[u64], message: String, cx: &mut Context<Self>) {
        for id in ids {
            if let Some(note) = self.annotate.annotations.iter_mut().find(|note| note.id == *id && note.request_id == Some(request_id)) {
                note.streaming = false;
                note.request_id = None;
                note.ai_error = Some(message.clone());
            }
        }
        self.persist_annotations();
        cx.notify();
    }

    pub(super) fn shift_annotation_offsets(&mut self, block_index: usize, id: u64, range: std::ops::Range<usize>, new_len: usize) {
        let shift = new_len as isize - range.len() as isize;
        for note in self.annotate.annotations.iter_mut().filter(|note| note.block_index == block_index) {
            if note.id == id {
                note.block_index = block_index;
                note.start = range.start;
                note.end = range.start + new_len;
            } else if note.start >= range.end {
                if shift >= 0 {
                    note.start += shift as usize;
                    note.end += shift as usize;
                } else {
                    note.start = note.start.saturating_sub((-shift) as usize);
                    note.end = note.end.saturating_sub((-shift) as usize);
                }
            }
        }
    }

    fn apply_annotations(&mut self, ids: &[u64], window: &mut Window, cx: &mut Context<Self>) {
        let mut invalid = Vec::new();
        let mut targets = Vec::new();
        for id in ids {
            let Some(note) = self.annotate.annotations.iter().find(|note| note.id == *id && note.is_generated()) else { continue };
            let Some(replacement) = note.ai_rewritten.clone() else { continue };
            let Some((block_id, range, _)) = resolve_annotation_span(&self.doc, note) else { invalid.push(*id); continue };
            let Some(block) = self.doc.get(block_id).filter(|block| block.text.get(range.clone()).is_some()) else { invalid.push(*id); continue };
            let _ = block;
            let Some(block_index) = self.doc.index_of(block_id) else { invalid.push(*id); continue };
            targets.push(ApplyTarget { id: *id, block_id, block_index, range, replacement });
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
        for id in &invalid {
            if let Some(note) = self.annotate.annotations.iter_mut().find(|note| note.id == *id) {
                note.ai_error = Some(tr("原文已变化，无法应用这条批注", "The selected text changed and this annotation cannot be applied").into());
            }
        }
        for id in &conflicts {
            if let Some(note) = self.annotate.annotations.iter_mut().find(|note| note.id == *id) {
                note.ai_error = Some(tr("批注选区重叠，请逐条应用", "Annotations overlap; apply them one at a time").into());
            }
        }
        if !targets.is_empty() {
            targets.sort_by_key(|target| (std::cmp::Reverse(target.block_index), std::cmp::Reverse(target.range.start)));
            let before = self.snapshot();
            let mut changed_blocks = BTreeSet::new();
            let mut applied_ids = Vec::new();
            let mut caret = None;
            for target in &targets {
                self.doc.replace_range(target.block_id, target.range.clone(), &target.replacement);
                self.shift_annotation_offsets(target.block_index, target.id, target.range.clone(), target.replacement.len());
                changed_blocks.insert(target.block_index);
                applied_ids.push(target.id);
                caret = Some(Position { block: target.block_id, offset: target.range.start + target.replacement.len() });
            }
            if let Some(pos) = caret { self.caret.anchor = pos; self.caret.head = pos; }
            let changes = changed_blocks.into_iter().map(|start| ListChange { start, before: 1, after: 1 }).collect();
            self.record_changes(before, changes, true, cx);
            for note in &mut self.annotate.annotations {
                if applied_ids.contains(&note.id) {
                    note.applied = Some(true);
                    note.streaming = false;
                    note.request_id = None;
                    note.ai_error = None;
                }
            }
            if self.annotate.active_annotation.is_some_and(|id| applied_ids.contains(&id)) {
                self.annotate.active_annotation = None;
                self.annotate.followup = None;
                window.focus(&self.focus, cx);
            }
        }
        self.persist_annotations();
        cx.notify();
    }
}
