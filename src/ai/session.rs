//! Inline AI session state and the editor commands that drive it.
use std::rc::Rc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, mpsc};
use std::time::Duration;

use gpui::{AppContext, Context, Entity, Pixels, Point, Subscription, Window, point, px};
use gpui_component::input::{InputEvent, InputState};

use super::provider::{self, Event};
use crate::editor::format_cmds::FormatPop;
use crate::editor::view::selection::Position;
use crate::editor::view::Editor;

pub(in crate::editor) const INPUT_PLACEHOLDER: &str = "告诉 AI 如何处理这段内容（如：润色、精简、翻译）…";
pub(in crate::editor) const WRITE_PLACEHOLDER: &str = "告诉 AI 要写什么（如：写一段会议纪要开头）…";
pub(in crate::editor) const REVIEW_PLACEHOLDER: &str = "输入进一步修改要求（如：再简短一些）…";

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub(in crate::editor) enum Preset { Polish, Simplify, English, Chinese, Jargon, Colloquial, List, Tasks }
impl Preset {
    pub(in crate::editor) fn label(self) -> &'static str {
        match self {
            Preset::Polish => "润色", Preset::Simplify => "精简", Preset::English => "翻译成英文", Preset::Chinese => "翻译成中文",
            Preset::Jargon => "工作黑话", Preset::Colloquial => "口语化", Preset::List => "整理为列表", Preset::Tasks => "待办事项",
        }
    }
    fn prompt(self) -> &'static str {
        match self {
            Preset::Polish => "润色给定片段，保留原意。",
            Preset::Jargon => "把给定片段改写成工作黑话，保留原意。",
            Preset::Simplify => "精简给定片段，保留关键信息。",
            Preset::English => "将给定片段翻译成英文。",
            Preset::Chinese => "将给定片段翻译成中文。",
            Preset::Colloquial => "把给定片段改得更口语、自然。",
            Preset::List => "将给定片段整理成无序列表，每个要点独立一行。",
            Preset::Tasks => "将给定片段整理成 Markdown 待办列表，每项使用 - [ ] 独立一行。",
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub(in crate::editor) enum AiMenu { None, Translate, More }

#[derive(Clone)]
pub(in crate::editor) enum AiStatus {
    Input,
    Thinking { tick: usize, partial: String, reasoning: bool },
    Review { draft: String },
    Error { message: String, settings: bool },
}

#[derive(Clone)]
pub(in crate::editor) struct AiSession {
    pub anchor: Point<Pixels>,
    /// Selection the result replaces; equal ends for a slash-opened writing session.
    pub target: (Position, Position),
    pub original: String,
    pub input: Entity<InputState>,
    pub status: AiStatus,
    /// The last instruction sent, kept for 重新生成 and follow-ups.
    pub prompt: String,
    /// The previous draft when a follow-up refines a reviewed result.
    pub base: Option<String>,
    pub menu: AiMenu,
    pub cancel: Arc<AtomicBool>,
    pub serial: u64,
    _sub: Rc<Subscription>,
}

const SYSTEM: &str = "你是笔记编辑器里的写作助手。只输出处理后的结果正文，可以使用 Markdown；不要解释，不要加引号，不要用代码围栏包裹，不要输出“好的”之类的前言。";

impl Editor {
    pub(in crate::editor) fn ai_session(&self) -> Option<&AiSession> {
        match self.format.as_ref() { Some(FormatPop::Ai(s)) => Some(s), _ => None }
    }
    fn ai_mut(&mut self) -> Option<&mut AiSession> {
        match self.format.as_mut() { Some(FormatPop::Ai(s)) => Some(s), _ => None }
    }
    fn selected_plain(&self) -> String {
        self.selection_ranges().into_iter()
            .filter_map(|(id, r)| self.doc.get(id).map(|b| b.text[r].to_owned()))
            .collect::<Vec<_>>().join("\n")
    }
    fn start_ai(&mut self, anchor: Point<Pixels>, target: (Position, Position), original: String, window: &mut Window, cx: &mut Context<Self>) {
        self.cancel_ai();
        let placeholder = if original.is_empty() { WRITE_PLACEHOLDER } else { INPUT_PLACEHOLDER };
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(placeholder));
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| {
            if let InputEvent::PressEnter { secondary: false } = event { this.ai_submit_input(window, cx); }
        });
        input.update(cx, |s, cx| s.focus(window, cx));
        self.format = Some(FormatPop::Ai(Box::new(AiSession {
            anchor, target, original, input, status: AiStatus::Input, prompt: String::new(), base: None,
            menu: AiMenu::None, cancel: Arc::new(AtomicBool::new(false)), serial: 0, _sub: Rc::new(sub),
        })));
        cx.notify();
    }
    /// Selection toolbar entry (AI 润色).
    pub(in crate::editor) fn open_ai(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        let original = self.selected_plain();
        if original.trim().is_empty() { self.status = "请先选择要处理的文字".into(); cx.notify(); return; }
        if !self.can_replace_selection() { self.status = "AI 暂不支持跨表格单元格的选区".into(); cx.notify(); return; }
        let (a, b, _, _) = self.ordered();
        let anchor = self.toolbar_anchor.map(|p| p + point(px(0.), px(80.))).unwrap_or_else(|| window.mouse_position() + point(px(0.), px(16.)));
        self.start_ai(anchor, (a, b), original, window, cx);
    }
    /// Slash / block-handle entry (AI 写作): rewrite the block, or write into an empty one.
    pub(in crate::editor) fn open_ai_block(&mut self, id: u64, slash: bool, anchor: Option<Point<Pixels>>, window: &mut Window, cx: &mut Context<Self>) {
        let Some(block) = self.doc.get(id) else { return };
        if block.meta.table.is_some() { self.status = "AI 暂不支持表格".into(); cx.notify(); return; }
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
        self.start_ai(anchor, target, text, window, cx);
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
        let refine = match &s.status { AiStatus::Review { draft } => Some(draft.clone()), _ => None };
        self.ai_run(text, refine, window, cx);
    }
    pub(in crate::editor) fn ai_preset(&mut self, preset: Preset, window: &mut Window, cx: &mut Context<Self>) {
        self.ai_run(preset.prompt().to_owned(), None, window, cx);
    }
    pub(in crate::editor) fn ai_retry(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_session() else { return };
        let (prompt, base) = (s.prompt.clone(), s.base.clone());
        if prompt.is_empty() { return; }
        self.ai_run(prompt, base, window, cx);
    }
    pub(in crate::editor) fn ai_stop(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_mut() else { return };
        s.cancel.store(true, Ordering::Relaxed);
        s.serial += 1;
        s.status = if let Some(draft) = s.base.clone() { AiStatus::Review { draft } } else { AiStatus::Input };
        let input = s.input.clone();
        input.update(cx, |i, cx| i.focus(window, cx));
        cx.notify();
    }
    fn ai_run(&mut self, prompt: String, base: Option<String>, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_mut() else { return };
        s.cancel.store(true, Ordering::Relaxed);
        let cancel = Arc::new(AtomicBool::new(false));
        s.cancel = cancel.clone();
        s.serial += 1;
        let serial = s.serial;
        s.prompt = prompt.clone();
        s.base = base.clone();
        s.menu = AiMenu::None;
        s.status = AiStatus::Thinking { tick: 0, partial: String::new(), reasoning: false };
        let user = match (&base, s.original.is_empty()) {
            (Some(draft), _) => format!("原文：\n{}\n\n上一版结果：\n{draft}\n\n请在上一版结果的基础上按要求修改：{prompt}", s.original),
            (None, true) => format!("请直接写出内容：{prompt}"),
            (None, false) => format!("要求：{prompt}\n\n给定片段：\n{}", s.original),
        };
        let input = s.input.clone();
        input.update(cx, |i, cx| { i.set_value("", window, cx); i.set_placeholder(REVIEW_PLACEHOLDER, window, cx); });
        let rx = provider::stream(SYSTEM.to_owned(), user, cancel);
        cx.spawn(async move |this, cx| {
            loop {
                cx.background_executor().timer(Duration::from_millis(40)).await;
                let mut finished = false;
                let alive = this.update(cx, |this, cx| {
                    let Some(s) = this.ai_mut() else { finished = true; return };
                    if s.serial != serial { finished = true; return; }
                    let AiStatus::Thinking { tick, partial, reasoning } = &mut s.status else { finished = true; return };
                    *tick += 1;
                    loop {
                        match rx.try_recv() {
                            Ok(Event::Thinking(_)) => *reasoning = true,
                            Ok(Event::Delta(t)) => partial.push_str(&t),
                            Ok(Event::Done) => {
                                let draft = clean(partial);
                                s.status = AiStatus::Review { draft }; finished = true; break;
                            }
                            Ok(Event::Failed { message, settings }) => { s.status = AiStatus::Error { message, settings }; finished = true; break; }
                            Err(mpsc::TryRecvError::Empty) => break,
                            Err(mpsc::TryRecvError::Disconnected) => {
                                s.status = AiStatus::Error { message: "生成中断，未对正文进行修改".into(), settings: false }; finished = true; break;
                            }
                        }
                    }
                    cx.notify();
                });
                if alive.is_err() || finished { break; }
            }
        }).detach();
        cx.notify();
    }
    /// 应用修改: replaces the target with the reviewed draft as one undo step.
    pub(in crate::editor) fn ai_accept(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_session() else { return };
        let AiStatus::Review { draft } = &s.status else { return };
        let (draft, (a, b)) = (draft.clone(), s.target);
        self.format = None;
        if self.doc.get(a.block).is_none() || self.doc.get(b.block).is_none() { self.status = "原文已变化，未应用修改".into(); window.focus(&self.focus); cx.notify(); return; }
        self.caret.anchor = a; self.caret.head = b; self.caret.composing = None;
        let markdownish = draft.contains('\n') || draft.starts_with("- ") || draft.starts_with("* ") || draft.starts_with('#') || draft.starts_with("> ") || draft.chars().next().is_some_and(|c| c.is_ascii_digit()) && draft.contains(". ");
        let pasted = markdownish && {
            self.block_clip = Some((draft.clone(), draft.clone()));
            self.paste_blocks(&draft, cx)
        };
        if !pasted { self.replace_selection(&draft, false, cx); }
        self.status = "已应用 AI 修改".into();
        window.focus(&self.focus);
        cx.notify();
    }
    /// 放弃 / 关闭: stops any request and leaves the text untouched.
    pub(in crate::editor) fn ai_close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.cancel_ai();
        self.close_format(window, cx);
    }
}

/// Drops a wrapping code fence or quotes the model added despite the instruction.
fn clean(text: &str) -> String {
    let mut t = text.trim();
    if t.starts_with("```") && t.ends_with("```") && t.len() > 6 {
        t = t[3..t.len() - 3].trim();
        if let Some(nl) = t.find('\n') { if !t[..nl].contains(' ') { t = t[nl + 1..].trim(); } }
    }
    t.to_owned()
}
