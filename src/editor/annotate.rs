//! Annotation mode (F-Annotate): selection comments, emoji reactions, inline
//! highlights (`#fbeed5` selected / `#fdf3c4` AI-modified), and `@Claude` rewrites.
use std::{
    collections::BTreeMap, fs, ops::Range, path::{Path, PathBuf}, process::Command,
    sync::{Arc, atomic::AtomicBool, mpsc}, time::{Duration, SystemTime, UNIX_EPOCH},
};
use gpui::{BorderStyle, Bounds, Context, Entity, Pixels, Point, Subscription, Window, point, prelude::*, px, quad, rgb, transparent_black};
use gpui_component::input::{InputEvent, InputState};
use serde::{Deserialize, Serialize};
use super::{
    ai::provider::{self, Event}, input::transactions::ListChange, model::Doc,
    view::{Editor, layout::TextLayout, selection::Position},
};

pub(super) const HL_SELECT_BG: u32 = 0xfbeed5;
pub(super) const HL_SELECT_BORDER: u32 = 0xecd9b4;
pub(super) const HL_AI_BG: u32 = 0xfdf3c4;
pub(super) const HL_AI_BORDER: u32 = 0xefd77a;

#[derive(Clone, Debug, Serialize, Deserialize)]
pub(super) struct Comment {
    pub id: u64,
    pub block_index: usize,
    pub start: usize,
    pub end: usize,
    pub quote: String,
    pub author: String,
    pub avatar: String,
    pub time: String,
    pub body: String,
    pub sent_to_claude: bool,
    #[serde(default)] pub reactions: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")] pub ai_rewritten: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")] pub ai_error: Option<String>,
    #[serde(skip)] pub streaming: bool,
}

pub(super) struct AnnotateComposer {
    pub block_id: u64, pub block_index: usize, pub range: Range<usize>, pub quote: String,
    pub send_to_claude: bool, pub input: Entity<InputState>, _sub: Subscription,
}

#[derive(Clone)]
pub(super) struct ReactionPicker {
    pub block_id: u64, pub block_index: usize, pub range: Range<usize>,
    pub quote: String, pub comment_id: Option<u64>, pub anchor: Point<Pixels>,
}

#[derive(Default)]
pub(super) struct AnnotateState {
    pub loaded_path: Option<PathBuf>, pub comments: Vec<Comment>, pub composer: Option<AnnotateComposer>,
    pub reactions: Option<ReactionPicker>, pub active_comment: Option<u64>, pub editor_top: Pixels,
}

impl AnnotateState {
    pub(super) fn has_visible_rail(&self) -> bool { !self.comments.is_empty() || self.composer.is_some() }
}

#[derive(Default, Serialize, Deserialize)]
struct AnnotationStore { notes: BTreeMap<String, Vec<Comment>> }

fn store_path() -> Option<PathBuf> {
    std::env::var_os("HOME").map(|h| PathBuf::from(h).join("Library/Application Support/Thinkly Fast/annotations.json"))
}

fn load_for(path: &Path) -> Vec<Comment> {
    let store: AnnotationStore = store_path().and_then(|p| fs::read_to_string(p).ok())
        .and_then(|raw| serde_json::from_str(&raw).ok()).unwrap_or_default();
    store.notes.get(path.to_string_lossy().as_ref()).cloned().unwrap_or_default()
}

fn save_for(path: &Path, comments: &[Comment]) {
    let Some(file) = store_path() else { return };
    if let Some(parent) = file.parent() { let _ = fs::create_dir_all(parent); }
    let mut store: AnnotationStore = fs::read_to_string(&file).ok().and_then(|raw| serde_json::from_str(&raw).ok()).unwrap_or_default();
    let key = path.to_string_lossy().into_owned();
    if comments.is_empty() { store.notes.remove(&key); } else { store.notes.insert(key, comments.to_vec()); }
    if let Ok(json) = serde_json::to_string_pretty(&store) { let _ = fs::write(file, json); }
}

fn next_id() -> u64 { SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_millis() as u64).unwrap_or(1) }

fn now_hm() -> String {
    Command::new("date").arg("+%H:%M").output().ok().and_then(|o| String::from_utf8(o.stdout).ok())
        .map(|s| s.trim().to_owned()).filter(|s| !s.is_empty()).unwrap_or_else(|| "刚刚".into())
}

/// Resolves a comment's current `(block_id, byte_range, is_ai_modified)` in `doc`.
pub(super) fn resolve_comment_span(doc: &Doc, comment: &Comment) -> Option<(u64, Range<usize>, bool)> {
    if let Some(rewritten) = comment.ai_rewritten.as_deref().filter(|s| !s.is_empty()) {
        if let Some((id, range)) = find_snippet(doc, comment.block_index, comment.start, rewritten) { return Some((id, range, true)); }
    }
    if !comment.quote.is_empty() {
        if let Some((id, range)) = find_snippet(doc, comment.block_index, comment.start, &comment.quote) { return Some((id, range, false)); }
    }
    doc.blocks.get(comment.block_index).map(|b| {
        let end = comment.end.min(b.text.len());
        (b.id, comment.start.min(end)..end, comment.ai_rewritten.is_some())
    })
}

fn find_snippet(doc: &Doc, preferred_index: usize, preferred_start: usize, needle: &str) -> Option<(u64, Range<usize>)> {
    if let Some(block) = doc.blocks.get(preferred_index) {
        let end = preferred_start.saturating_add(needle.len());
        if end <= block.text.len() && block.text.get(preferred_start..end) == Some(needle) { return Some((block.id, preferred_start..end)); }
        if let Some(pos) = block.text.find(needle) { return Some((block.id, pos..(pos + needle.len()))); }
    }
    doc.blocks.iter().enumerate()
        .filter_map(|(idx, b)| b.text.find(needle).map(|pos| (idx.abs_diff(preferred_index), b.id, pos..(pos + needle.len()))))
        .min_by_key(|(dist, _, _)| *dist).map(|(_, id, range)| (id, range))
}

/// Paints `#fbeed5` (annotated selection) and `#fdf3c4` (AI-modified) spans under block text.
pub(super) fn paint_highlights(editor: &Editor, block: u64, layout: &TextLayout, window: &mut Window) {
    for comment in &editor.annotate.comments {
        let Some((id, range, is_ai)) = resolve_comment_span(&editor.doc, comment) else { continue };
        if id != block || range.is_empty() { continue; }
        let (bg, border_col) = if is_ai { (HL_AI_BG, HL_AI_BORDER) } else { (HL_SELECT_BG, HL_SELECT_BORDER) };
        let active = editor.annotate.active_comment == Some(comment.id);
        for rect in layout.rects(range) {
            let rect: Bounds<Pixels> = rect;
            let (bw, bc, bs) = if active { (px(1.), gpui::Hsla::from(rgb(border_col)), BorderStyle::Solid) } else { (px(0.), transparent_black(), BorderStyle::default()) };
            window.paint_quad(quad(rect, px(2.), rgb(bg), bw, bc, bs));
        }
    }
    if let Some(c) = editor.annotate.composer.as_ref().filter(|c| c.block_id == block && !c.range.is_empty()) {
        for rect in layout.rects(c.range.clone()) {
            window.paint_quad(quad(rect, px(2.), rgb(HL_SELECT_BG), px(1.), rgb(HL_SELECT_BORDER), BorderStyle::Solid));
        }
    }
    if let Some(p) = editor.annotate.reactions.as_ref().filter(|p| p.block_id == block && !p.range.is_empty()) {
        for rect in layout.rects(p.range.clone()) {
            window.paint_quad(quad(rect, px(2.), rgb(HL_SELECT_BG), px(0.), transparent_black(), BorderStyle::default()));
        }
    }
}

impl Editor {
    pub(super) fn sync_annotations(&mut self) {
        if self.annotate.loaded_path != self.path {
            self.annotate.loaded_path = self.path.clone();
            self.annotate.comments = self.path.as_deref().map(load_for).unwrap_or_default();
            self.annotate.composer = None;
            self.annotate.reactions = None;
            self.annotate.active_comment = None;
        }
    }

    pub(super) fn persist_annotations(&self) {
        if let Some(path) = self.path.as_deref() { save_for(path, &self.annotate.comments); }
    }

    fn selected_annotation_target(&self) -> Option<(u64, usize, Range<usize>, String)> {
        let (a, _b, ai, _bi) = self.ordered();
        let block = self.doc.blocks.get(ai)?;
        let range = self.selected_range(block.id).filter(|r| !r.is_empty()).unwrap_or(0..block.text.len());
        let quote = block.text.get(range.clone()).unwrap_or("").to_owned();
        (!quote.trim().is_empty()).then_some((a.block, ai, range, quote))
    }

    /// Toolbar 「添加批注」: highlight the selection and open the composer card.
    pub(super) fn start_annotation(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.sync_annotations();
        let Some((block_id, block_index, range, quote)) = self.selected_annotation_target() else { return };
        let input = cx.new(|cx| InputState::new(window, cx).placeholder("输入批注或修改指令…"));
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| match event {
            InputEvent::PressEnter { secondary: false } => this.submit_annotation(window, cx),
            InputEvent::Change => cx.notify(),
            _ => {}
        });
        let focus_input = input.clone();
        self.annotate.reactions = None;
        self.format = None;
        self.annotate.composer = Some(AnnotateComposer { block_id, block_index, range, quote, send_to_claude: true, input, _sub: sub });
        focus_input.update(cx, |i, cx| i.focus(window, cx));
        cx.defer_in(window, move |_, window, cx| { focus_input.update(cx, |i, cx| i.focus(window, cx)); });
        cx.notify();
    }

    /// Toolbar 「添加表情回应」: open the reaction picker for the selection.
    pub(super) fn open_reactions(&mut self, _window: &mut Window, cx: &mut Context<Self>) {
        self.sync_annotations();
        let Some((block_id, block_index, range, quote)) = self.selected_annotation_target() else { return };
        let anchor = self.toolbar_anchor.unwrap_or_else(|| point(px(360.), px(140.)));
        let comment_id = self.annotate.comments.iter()
            .find(|c| c.block_index == block_index && (c.quote == quote || c.ai_rewritten.as_deref() == Some(quote.as_str())))
            .map(|c| c.id);
        self.format = None;
        self.annotate.reactions = Some(ReactionPicker { block_id, block_index, range, quote, comment_id, anchor });
        cx.notify();
    }

    pub(super) fn close_annotation_composer(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.annotate.composer = None;
        window.focus(&self.focus);
        cx.notify();
    }

    pub(super) fn pick_reaction(&mut self, emoji: &str, cx: &mut Context<Self>) {
        let Some(picker) = self.annotate.reactions.take() else { return };
        let existing = picker.comment_id.and_then(|id| self.annotate.comments.iter_mut().find(|c| c.id == id));
        if let Some(comment) = existing {
            if let Some(idx) = comment.reactions.iter().position(|r| r == emoji) { comment.reactions.remove(idx); }
            else { comment.reactions.push(emoji.to_owned()); }
            let (id, empty) = (comment.id, comment.body.is_empty() && comment.reactions.is_empty());
            if empty { self.annotate.comments.retain(|c| c.id != id); }
        } else {
            self.annotate.comments.push(Comment {
                id: next_id(), block_index: picker.block_index, start: picker.range.start, end: picker.range.end,
                quote: picker.quote, author: "安静的写作者".into(), avatar: "安".into(), time: now_hm(),
                body: String::new(), sent_to_claude: false, reactions: vec![emoji.to_owned()],
                ai_rewritten: None, ai_error: None, streaming: false,
            });
        }
        self.persist_annotations();
        cx.notify();
    }

    pub(super) fn delete_comment(&mut self, id: u64, cx: &mut Context<Self>) {
        self.annotate.comments.retain(|c| c.id != id);
        if self.annotate.active_comment == Some(id) { self.annotate.active_comment = None; }
        self.persist_annotations();
        cx.notify();
    }

    pub(super) fn submit_annotation(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(composer) = &self.annotate.composer else { return };
        let body = composer.input.read(cx).value().trim().to_owned();
        if body.is_empty() { return; }
        let composer = self.annotate.composer.take().unwrap();
        let (id, sent_to_claude, quote) = (next_id(), composer.send_to_claude, composer.quote.clone());
        self.annotate.comments.push(Comment {
            id, block_index: composer.block_index, start: composer.range.start, end: composer.range.end,
            quote: quote.clone(), author: "安静的写作者".into(), avatar: "安".into(), time: now_hm(),
            body: body.clone(), sent_to_claude, reactions: Vec::new(),
            ai_rewritten: None, ai_error: None, streaming: sent_to_claude,
        });
        self.persist_annotations();
        window.focus(&self.focus);
        if sent_to_claude { self.run_claude_annotation(id, quote, body, cx); }
        cx.notify();
    }

    fn run_claude_annotation(&mut self, comment_id: u64, quote: String, instruction: String, cx: &mut Context<Self>) {
        let system = "你是 Thinkly 笔记的批注修订助手（@Claude）。用户在正文中选中了一段文字并附上批注指令。\
            请严格按照批注指令改写「选中原文」，只直接输出替换后的新文本本身，不要输出任何解释、引号或多余前后缀。".to_owned();
        let user = format!("选中原文：\n{quote}\n\n批注指令：\n{instruction}\n\n请直接输出替换「选中原文」的新文本：");
        let rx = provider::stream(system, user, Arc::new(AtomicBool::new(false)));
        cx.spawn(async move |this, cx| {
            let mut buf = String::new();
            loop {
                cx.background_executor().timer(Duration::from_millis(40)).await;
                let mut done = false;
                let ok = this.update(cx, |this, cx| {
                    loop {
                        match rx.try_recv() {
                            Ok(Event::Thinking(_)) | Ok(Event::ToolCalls(_)) => {}
                            Ok(Event::Delta(t)) => buf.push_str(&t),
                            Ok(Event::Done) => { done = true; this.finish_claude_annotation(comment_id, buf.trim(), cx); break; }
                            Ok(Event::Failed { message, .. }) => {
                                done = true;
                                if let Some(c) = this.annotate.comments.iter_mut().find(|c| c.id == comment_id) {
                                    c.streaming = false;
                                    c.ai_error = Some(provider::format_error(&message));
                                }
                                this.persist_annotations();
                                break;
                            }
                            Err(mpsc::TryRecvError::Empty) => break,
                            Err(mpsc::TryRecvError::Disconnected) => { done = true; break; }
                        }
                    }
                    cx.notify();
                });
                if ok.is_err() || done { break; }
            }
        }).detach();
    }

    fn finish_claude_annotation(&mut self, comment_id: u64, clean: &str, cx: &mut Context<Self>) {
        if let Some(c) = (!clean.is_empty()).then(|| self.annotate.comments.iter().find(|c| c.id == comment_id).cloned()).flatten() {
            if let Some((block_id, range, _)) = resolve_comment_span(&self.doc, &c) {
                if let Some(idx) = self.doc.index_of(block_id) {
                    let before = self.snapshot();
                    self.doc.replace_range(block_id, range.clone(), clean);
                    let pos = Position { block: block_id, offset: range.start + clean.len() };
                    self.caret.anchor = pos; self.caret.head = pos;
                    self.record_changes(before, vec![ListChange { start: idx, before: 1, after: 1 }], true, cx);
                }
            }
        }
        if let Some(c) = self.annotate.comments.iter_mut().find(|c| c.id == comment_id) {
            c.streaming = false;
            if !clean.is_empty() { c.ai_rewritten = Some(clean.to_owned()); }
        }
        self.persist_annotations();
    }
}
