//! Inline AI session state and the editor commands that drive it, after thinkly's
//! `GooseAIMenu` / `useInlineAiEdit`: user-input → thinking → user-reviewing | error.
use std::{ops::Range, rc::Rc, sync::{Arc, atomic::{AtomicBool, Ordering}, mpsc}, time::Duration};
use gpui::{AppContext, Context, Entity, Pixels, Point, Subscription, Window, point, px};
use gpui_component::input::{InputEvent, InputState};
use super::provider::{self, Event};
use crate::{editor::{format_cmds::FormatPop, view::{Editor, selection::Position}}, theme::tr};

pub(in crate::editor) fn input_placeholder() -> &'static str { tr("告诉 AI 如何处理这段内容（如：润色、精简、翻译）…", "Tell AI how to edit this (e.g., polish, simplify, translate)…") }
pub(in crate::editor) fn review_placeholder() -> &'static str { tr("输入进一步修改要求（如：再简短一些）…", "Enter follow-up instructions (e.g., make it shorter)…") }

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub(in crate::editor) enum Preset { Polish, Simplify, English, Chinese, Jargon, Colloquial, List, Tasks }
impl Preset {
    pub(in crate::editor) fn label(self) -> &'static str {
        match self {
            Preset::Polish => tr("润色", "Polish"), Preset::Simplify => tr("精简", "Simplify"), Preset::English => tr("翻译成英文", "Translate to English"), Preset::Chinese => tr("翻译成中文", "Translate to Chinese"),
            Preset::Jargon => tr("工作黑话", "Corporate Tone"), Preset::Colloquial => tr("口语化", "Conversational"), Preset::List => tr("整理为列表", "Bullet List"), Preset::Tasks => tr("待办事项", "To-Do List"),
        }
    }
    fn prompt(self) -> &'static str {
        match self {
            Preset::Polish => "润色给定片段，保留原意。", Preset::Jargon => "把给定片段改写成工作黑话，保留原意。",
            Preset::Simplify => "精简给定片段，保留关键信息。", Preset::English => "将给定片段翻译成英文。",
            Preset::Chinese => "将给定片段翻译成中文。", Preset::Colloquial => "把给定片段改得更口语、自然。",
            Preset::List => "将给定片段整理成无序列表，每个要点独立一行。", Preset::Tasks => "将给定片段整理成 Markdown 待办列表，每项使用 - [ ] 独立一行。",
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub(in crate::editor) enum AiMenu { None, Translate, More }
#[derive(Clone)]
pub(in crate::editor) enum AiStatus { Input, Thinking { tick: usize, partial: String, reasoning: String }, Review { draft: String }, Error { message: String } }
#[derive(Clone)]
pub(in crate::editor) struct AiSession {
    pub anchor: Point<Pixels>, pub target: (Position, Position), pub parts: Vec<(u64, Range<usize>)>,
    pub original: String, pub input: Entity<InputState>, pub status: AiStatus, pub prompt: String,
    pub menu: AiMenu, pub cancel: Arc<AtomicBool>, pub serial: u64, _sub: Rc<(Subscription, Subscription)>,
}

const SYSTEM_PROMPT: &str = "你是笔记行内改写助手。根据用户指令改写给定的 Markdown 片段。\n只输出改写后的 Markdown 正文，不要解释、不要包代码围栏（除非正文本身是代码块）。\n保持合理的 Markdown 结构：列表项用独立的 `- ` / `1. ` / `- [ ] ` 行；列表项之间不要插空行。\n若用户要求生成 N 个列表项，输出 N 行列表语法，不要挤在一个段落里。\n不要输出页面标题（# 一级标题），只改写当前片段。\n保留原有对齐、字体颜色和背景色：不要删除 HTML 注释、span style 或 text-align。";
fn user_prompt(old: &str, instruction: &str) -> String {
    ["【当前片段 Markdown】", if old.is_empty() { "（空）" } else { old }, "", "【用户指令】", instruction.trim(), "", "请直接输出改写后的 Markdown："].join("\n")
}

impl Editor {
    /// Keep the card under the target as the in-document diff grows.
    fn ai_reanchor(&mut self) {
        let Some(block) = self.ai_session().and_then(|s| s.parts.last().map(|p| p.0)) else { return };
        let Some(below) = self.layouts.get(&block).map(|l| point(l.bounds.left(), l.bounds.bottom() + px(8.))) else { return };
        if let Some(s) = self.ai_mut() { s.anchor = below; }
    }
    pub(in crate::editor) fn ai_session(&self) -> Option<&AiSession> {
        match self.format.as_ref() { Some(FormatPop::Ai(s)) => Some(s), _ => None }
    }
    fn ai_mut(&mut self) -> Option<&mut AiSession> {
        match self.format.as_mut() { Some(FormatPop::Ai(s)) => Some(s), _ => None }
    }
    fn start_ai(&mut self, anchor: Point<Pixels>, target: (Position, Position), parts: Vec<(u64, Range<usize>)>, original: String, window: &mut Window, cx: &mut Context<Self>) {
        self.cancel_ai();
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(input_placeholder()));
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| {
            if let InputEvent::PressEnter { secondary: false, .. } = event { this.ai_submit_input(window, cx); }
        });
        input.update(cx, |s, cx| s.focus(window, cx));
        // Esc closes the card wherever focus is (chat input, sidebar…), not only inside the card.
        let home = window.window_handle();
        let esc = cx.observe_keystrokes(move |this, e, window, cx| {
            if e.keystroke.key == "escape" && window.window_handle() == home && this.ai_session().is_some() { this.ai_close(window, cx); }
        });
        // The selection highlight would sit on top of the inline diff; the target is
        // outlined instead and the selection comes back on 放弃 / 关闭.
        self.caret.anchor = target.1; self.caret.head = target.1; self.caret.composing = None;
        self.format = Some(FormatPop::Ai(Box::new(AiSession {
            anchor, target, parts, original, input, status: AiStatus::Input, prompt: String::new(),
            menu: AiMenu::None, cancel: Arc::new(AtomicBool::new(false)), serial: 0, _sub: Rc::new((sub, esc)),
        })));
        cx.notify();
    }
    /// Selection toolbar entry (AI 润色).
    #[allow(dead_code)]
    pub(in crate::editor) fn open_ai(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.is_markdown_note() { self.status = tr("当前页面不可编辑或 AI 未开启。", "Page is not editable or AI is disabled.").into(); cx.notify(); return; }
        let parts = self.selection_ranges();
        let original = parts.iter().filter_map(|(id, r)| self.doc.get(*id).map(|b| {
            // Paragraphs go to the model as inline Markdown so links / bold / code survive 应用修改.
            if b.kind != crate::editor::model::Kind::Paragraph || b.meta.table.is_some() || b.meta.raw_source.is_some() { return b.text[r.clone()].to_owned(); }
            let mut piece = b.slice(r.clone()); piece.meta.indent = 0; piece.meta.list_start = None;
            let md = crate::editor::markdown::serialize(&crate::editor::model::Doc::from_blocks(vec![piece]));
            let md = md.trim().to_owned();
            if md.is_empty() { b.text[r.clone()].to_owned() } else { md }
        })).collect::<Vec<_>>().join("\n");
        if original.trim().is_empty() { self.status = tr("请重新选择正文。", "Please select text first.").into(); cx.notify(); return; }
        if !self.can_replace_selection() { self.status = tr("请重新选择正文。", "Please select text first.").into(); cx.notify(); return; }
        let (a, b, _, _) = self.ordered();
        // Card sits under the last target block, like thinkly's inline AI menu, so the diff stays visible.
        let below = self.layouts.get(&b.block).map(|l| point(l.bounds.left(), l.bounds.bottom() + px(8.)));
        let anchor = below.or_else(|| self.toolbar_anchor.map(|p| p + point(px(0.), px(44.)))).unwrap_or_else(|| window.mouse_position() + point(px(0.), px(16.)));
        self.start_ai(anchor, (a, b), parts, original, window, cx);
    }
    /// Slash / block-handle / space-on-empty-paragraph entry (AI 写作).
    pub(in crate::editor) fn open_ai_block(&mut self, id: u64, slash: bool, anchor: Option<Point<Pixels>>, window: &mut Window, cx: &mut Context<Self>) {
        if self.minimal || !self.is_markdown_note() { return; }
        let Some(block) = self.doc.get(id) else { return };
        if block.meta.table.is_some() { self.status = tr("请重新选择正文。", "Please select text first.").into(); cx.notify(); return; }
        let mut text = block.text.clone();
        if slash && text.starts_with('/') {
            let before = self.snapshot();
            self.doc.set_text(id, String::new());
            self.caret.anchor = Position { block: id, offset: 0 }; self.caret.head = self.caret.anchor;
            self.record(before, cx);
            text.clear();
        }
        let len = text.len();
        let target = (Position { block: id, offset: 0 }, Position { block: id, offset: len });
        let anchor = anchor.unwrap_or_else(|| window.mouse_position()) + point(px(0.), px(8.));
        self.start_ai(anchor, target, vec![(id, 0..len)], text, window, cx);
    }
    fn cancel_ai(&mut self) {
        if let Some(s) = self.ai_session() { s.cancel.store(true, Ordering::Relaxed); }
    }
    pub(in crate::editor) fn ai_toggle_menu(&mut self, menu: AiMenu, cx: &mut Context<Self>) {
        if let Some(s) = self.ai_mut() { s.menu = if s.menu == menu { AiMenu::None } else { menu }; }
        cx.notify();
    }
    pub(in crate::editor) fn ai_submit_input(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_session() else { return };
        if matches!(s.status, AiStatus::Thinking { .. }) { return; }
        let text = s.input.read(cx).value().trim().to_owned();
        if text.is_empty() { return; }
        self.ai_run(text, window, cx);
    }
    pub(in crate::editor) fn ai_preset(&mut self, preset: Preset, window: &mut Window, cx: &mut Context<Self>) {
        self.ai_run(preset.prompt().to_owned(), window, cx);
    }
    /// 重新生成 / 重试: resubmits the last instruction against the original.
    pub(in crate::editor) fn ai_retry(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_session() else { return };
        let prompt = s.prompt.clone();
        if prompt.is_empty() { return; }
        self.ai_run(prompt, window, cx);
    }
    pub(in crate::editor) fn ai_open_settings(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.ai_close(window, cx);
        window.dispatch_action(Box::new(super::OpenAiSettings), cx);
    }
    pub(in crate::editor) fn ai_stop(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_mut() else { return };
        s.cancel.store(true, Ordering::Relaxed);
        s.serial += 1; s.status = AiStatus::Input;
        let input = s.input.clone();
        input.update(cx, |i, cx| { i.set_placeholder(input_placeholder(), window, cx); i.focus(window, cx); });
        cx.notify();
    }
    fn ai_run(&mut self, prompt: String, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_mut() else { return };
        s.cancel.store(true, Ordering::Relaxed);
        let cancel = Arc::new(AtomicBool::new(false));
        s.cancel = cancel.clone(); s.serial += 1;
        let serial = s.serial;
        s.prompt = prompt.clone(); s.menu = AiMenu::None;
        s.status = AiStatus::Thinking { tick: 0, partial: String::new(), reasoning: String::new() };
        let (user, input) = (user_prompt(&s.original, &prompt), s.input.clone());
        input.update(cx, |i, cx| i.set_value("", window, cx));
        #[cfg(not(target_family = "wasm"))]
        let rx = super::inline_backend::stream(SYSTEM_PROMPT.to_owned(), user, cancel, cx);
        #[cfg(target_family = "wasm")]
        let rx = provider::stream(SYSTEM_PROMPT.to_owned(), user, cancel);
        cx.spawn_in(window, async move |this, cx| {
            loop {
                cx.background_executor().timer(Duration::from_millis(40)).await;
                let mut finished = false;
                let alive = this.update_in(cx, |this, window, cx| {
                    let Some(s) = this.ai_mut() else { finished = true; return };
                    if s.serial != serial { finished = true; return; }
                    let AiStatus::Thinking { tick, partial, reasoning } = &mut s.status else { finished = true; return };
                    *tick += 1;
                    let mut next: Option<AiStatus> = None;
                    loop {
                        match rx.try_recv() {
                            Ok(Event::Thinking(t)) => reasoning.push_str(&t),
                            Ok(Event::Delta(t)) => partial.push_str(&t),
                            Ok(Event::ToolCalls(_)) => {}
                            Ok(Event::Done) => {
                                let draft = clean(partial);
                                next = Some(if draft.is_empty() { AiStatus::Error { message: tr("AI 未返回可写入的内容。", "AI returned empty content.").into() } } else { AiStatus::Review { draft } });
                                break;
                            }
                            Ok(Event::Failed { message, .. }) => { next = Some(AiStatus::Error { message: provider::format_error(&message) }); break; }
                            Err(mpsc::TryRecvError::Empty) => break,
                            Err(mpsc::TryRecvError::Disconnected) => { next = Some(AiStatus::Error { message: tr("生成中断，未对正文进行修改", "Generation interrupted; text unchanged").into() }); break; }
                        }
                    }
                    if let Some(status) = next {
                        let placeholder = if matches!(status, AiStatus::Review { .. }) { review_placeholder() } else { input_placeholder() };
                        s.status = status;
                        let input = s.input.clone();
                        input.update(cx, |i, cx| { i.set_placeholder(placeholder, window, cx); i.focus(window, cx); });
                        finished = true;
                    }
                    this.ai_reanchor();
                    cx.notify();
                });
                if alive.is_err() { break; }
                if finished {
                    // The diff's final height lands one paint later; settle the card under it.
                    cx.background_executor().timer(Duration::from_millis(60)).await;
                    let _ = this.update(cx, |this, cx| { this.ai_reanchor(); cx.notify(); });
                    break;
                }
            }
        }).detach();
        cx.notify();
    }
    /// 应用修改: validates the target, then replaces it with the draft as one undo step.
    pub(in crate::editor) fn ai_accept(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_session() else { return };
        let AiStatus::Review { draft } = &s.status else { return };
        let (draft, (a, b)) = (draft.clone(), s.target);
        let intact = s.parts.iter().all(|(id, r)| self.doc.get(*id).is_some_and(|blk| blk.text.len() >= r.end && blk.text.is_char_boundary(r.start) && blk.text.is_char_boundary(r.end)))
            && self.doc.get(a.block).is_some() && self.doc.get(b.block).is_some();
        if !intact {
            if let Some(s) = self.ai_mut() { s.status = AiStatus::Error { message: tr("目标块已变化，原文未修改。请重新选择后再试", "Target block changed; text unchanged. Please reselect and try again").into() }; }
            cx.notify(); return;
        }
        self.format = None;
        self.caret.anchor = a; self.caret.head = b; self.caret.composing = None;
        let markdownish = draft.contains('\n') || draft.starts_with("- ") || draft.starts_with("* ") || draft.starts_with('#') || draft.starts_with("> ") || draft.chars().next().is_some_and(|c| c.is_ascii_digit()) && draft.contains(". ");
        let pasted = markdownish && {
            self.block_clip = Some((draft.clone(), draft.clone()));
            self.paste_blocks(&draft, cx)
        };
        if !pasted {
            // One-line drafts still carry inline Markdown (**粗体**, `代码`, [链接](…)): insert the
            // rendered text, then lay the parsed marks and inline tags over the inserted range.
            let rich = (a.block == b.block).then(|| crate::editor::markdown::parse(&draft)).and_then(|doc| {
                let mut it = doc.blocks.iter().filter(|blk| !blk.text.is_empty());
                let first = it.next().cloned();
                first.filter(|blk| it.next().is_none() && blk.kind == crate::editor::model::Kind::Paragraph && blk.meta.raw_source.is_none()
                    && (!blk.spans.is_empty() || !blk.meta.inline.is_empty()))
            });
            match rich {
                Some(rich) => {
                    self.replace_selection(&rich.text, false, cx);
                    let at = a.offset;
                    if let Some(blk) = self.doc.get_mut(a.block) && blk.text.get(at..at + rich.text.len()) == Some(rich.text.as_str()) {
                        blk.spans.extend(rich.spans.iter().map(|sp| crate::editor::model::Span { start: sp.start + at, end: sp.end + at, marks: sp.marks.clone() }));
                        blk.normalize_spans();
                        blk.meta.inline.extend(rich.meta.inline.iter().map(|t| { let mut t = t.clone(); t.start += at; t.end += at; t }));
                        blk.meta.inline.sort_by_key(|t| (t.start, t.end));
                        self.layouts.clear();
                    }
                }
                None => self.replace_selection(&draft, false, cx),
            }
        }
        window.focus(&self.focus, cx);
        cx.notify();
    }
    /// 放弃 / 关闭: stops any request, leaves the text untouched and restores the selection.
    pub(in crate::editor) fn ai_close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let target = self.ai_session().map(|s| s.target);
        self.cancel_ai();
        self.close_format(window, cx);
        if let Some((a, b)) = target {
            if self.doc.get(a.block).is_some() && self.doc.get(b.block).is_some() { self.caret.anchor = a; self.caret.head = b; }
            window.focus(&self.focus, cx);
        }
        cx.notify();
    }
}

/// thinkly `visibleBusyTickerLine`: the last sentence of what has streamed so far.
pub(in crate::editor) fn ticker_line(text: &str) -> String {
    let t = text.trim_end();
    if t.is_empty() { return String::new(); }
    let ends = ['。', '！', '？', '.', '!', '?', '\n', '；', ';'];
    let body = t.trim_end_matches(ends);
    let start = body.rfind(ends).map(|i| i + body[i..].chars().next().map_or(1, char::len_utf8)).unwrap_or(0);
    body[start..].trim().to_owned()
}

/// Drops a wrapping ```markdown / ```md fence the model added despite the instruction.
fn clean(text: &str) -> String {
    let mut t = text.trim();
    if t.starts_with("```") && t.ends_with("```") && t.len() > 6 {
        let inner = t[3..t.len() - 3].trim_start_matches(|c: char| c != '\n');
        let lang = t[3..].split('\n').next().unwrap_or("").trim().to_lowercase();
        if lang.is_empty() || lang == "markdown" || lang == "md" { t = inner.trim(); }
    }
    t.to_owned()
}
