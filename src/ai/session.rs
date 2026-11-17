//! Inline AI session state and editor commands:
//! Minimalist flow: 01 Trigger (`/` or Selection `AI 改写`) -> 02 Input (default tags + saved/recent prompts)
//! -> 03 Native Block stream on paper -> 04 Review (Enter accepts, typing refines, Insert Below) -> 05 Done.
use std::{ops::Range, rc::Rc, sync::{Arc, atomic::{AtomicBool, Ordering}, mpsc}, time::Duration};
use gpui::{AppContext, Context, Entity, Pixels, Point, Subscription, Window, point, px};
use gpui_component::input::{InputEvent, InputState};
use super::provider::{self, Event};
use crate::{
    editor::{
        format_cmds::FormatPop,
        input::transactions::ListChange,
        markdown,
        model::Doc,
        view::{Editor, selection::Position},
    },
    theme::tr,
};

pub(in crate::editor) fn input_placeholder(empty_ctx: bool) -> &'static str {
    if empty_ctx {
        tr("写点什么，或直接点下面标签…", "Write something, or pick a tag below…")
    } else {
        tr("怎么改这段话，或直接点下面标签…", "How to edit this, or pick a tag below…")
    }
}

pub(in crate::editor) fn review_placeholder() -> &'static str {
    tr("不满意？直接打字继续改，或按回车完成…", "Type to refine further, or press Enter to finish…")
}

use serde::{Deserialize, Serialize};

#[derive(Clone, Copy, PartialEq, Eq, Debug, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub(in crate::editor) enum Preset {
    Continue,
    Outline,
    Tasks,
    Brainstorm,
    Polish,
    Simplify,
    Plain,
    Translate,
}

pub(in crate::editor) const EMPTY_PRESETS: &[Preset] = &[
    Preset::Continue,
    Preset::Outline,
    Preset::Tasks,
    Preset::Brainstorm,
];

pub(in crate::editor) const EDIT_PRESETS: &[Preset] = &[
    Preset::Polish,
    Preset::Simplify,
    Preset::Plain,
    Preset::Translate,
];

impl Preset {
    pub(in crate::editor) fn label(self) -> &'static str {
        let (zh, en) = self.labels();
        tr(zh, en)
    }

    fn labels(self) -> (&'static str, &'static str) {
        match self {
            Preset::Continue => ("续写", "Continue"),
            Preset::Outline => ("列大纲", "Outline"),
            Preset::Tasks => ("待办清单", "To-do list"),
            Preset::Brainstorm => ("头脑风暴", "Brainstorm"),
            Preset::Polish => ("润色", "Polish"),
            Preset::Simplify => ("精简", "Simplify"),
            Preset::Plain => ("通俗一点", "Plain language"),
            Preset::Translate => ("翻译", "Translate"),
        }
    }

    pub(in crate::editor) fn matches_input(self, raw: &str) -> bool {
        let (zh, en) = self.labels();
        raw == zh || raw.eq_ignore_ascii_case(en)
    }

    fn prompt(self) -> &'static str {
        match self {
            Preset::Continue => "顺着上文语境自然往下续写，保持语气与结构连贯，不要重复上文。",
            Preset::Outline => "结合当前主题列出结构清晰的三级标题（###）与要点大纲。",
            Preset::Tasks => "生成具体可执行的 Markdown 待办清单，每项使用独立的 `- [ ] ` 行。",
            Preset::Brainstorm => "围绕当前主题发散列出 4 到 5 个有启发性的思路要点，每项使用独立的 `- ` 行。",
            Preset::Polish => "润色给定片段，使表达更清晰流畅，保留原意与关键细节。",
            Preset::Simplify => "精简给定片段，删去冗余修饰，只保留核心信息。",
            Preset::Plain => "用通俗直白的大白话改写给定片段，去掉生僻黑话或绕口表达。",
            Preset::Translate => "智能中英互译：若给定片段主要为中文则准确自然地翻译成英文；若主要为英文或其他外语则准确自然地翻译成中文。保留专有名词、代码标识符与原有 Markdown 格式。",
        }
    }
}

#[derive(Clone)]
pub(in crate::editor) enum AiStatus {
    Input,
    Thinking {
        tick: usize,
        partial: String,
        reasoning: String,
        prev_draft: Option<String>,
    },
    Review {
        draft: String,
    },
}

#[derive(Clone)]
pub(in crate::editor) struct AiSession {
    pub anchor: Point<Pixels>,
    pub target: (Position, Position),
    pub parts: Vec<(u64, Range<usize>)>,
    pub original: String,
    pub context_before: String,
    pub input: Entity<InputState>,
    pub has_input_text: bool,
    pub status: AiStatus,
    pub error: Option<String>,
    pub prompt: String,
    pub restore_input: String,
    pub cancel: Arc<AtomicBool>,
    pub serial: u64,
    _sub: Rc<(Subscription, Subscription)>,
}

impl AiSession {
    pub(in crate::editor) fn is_empty_ctx(&self) -> bool {
        self.original.trim().is_empty()
    }
}

const SYSTEM_PROMPT: &str = "你是笔记行内写作与改写助手。根据用户指令直接输出将要插入或替换到当前位置的 Markdown 正文。\n只输出最终 Markdown 正文，不要解释、不要寒暄、不要包裹 ```markdown 代码围栏（除非正文本身是程序代码块）。\n保持干净的原生结构：小标题优先用 `### `，列表项用独立的 `- ` / `1. ` / `- [ ] ` 行（列表项之间不插空行），重要提示可用 `> [!NOTE]` 标注块，配置名或标识符用行内反引号。\n不要输出页面大标题（`# ` 一级标题），不要重复上文已有段落。\n改写已有内容时保留原有对齐、字体颜色和背景色，不要删除必要的 HTML 样式标签。";

fn user_prompt(original: &str, context_before: &str, current_draft: Option<&str>, instruction: &str) -> String {
    if let Some(draft) = current_draft {
        let mut sections = Vec::new();
        if !original.trim().is_empty() {
            sections.push(format!("【原始片段】\n{original}"));
        } else if !context_before.trim().is_empty() {
            sections.push(format!("【上文参考】\n{context_before}"));
        }
        sections.push(format!("【当前生成草稿】\n{draft}"));
        sections.push(format!("【继续修改要求】\n{}", instruction.trim()));
        sections.push("请基于【当前生成草稿】直接输出修改后的完整 Markdown 正文：".to_owned());
        return sections.join("\n\n");
    }
    if original.trim().is_empty() {
        let ctx = if context_before.trim().is_empty() { "（当前位于笔记开头，无上文）" } else { context_before.trim() };
        format!("【上文参考（仅供语境衔接，请勿重复输出）】\n{ctx}\n\n【写作要求】\n{}\n\n请直接输出在此处插入的 Markdown 正文：", instruction.trim())
    } else {
        format!("【当前片段 Markdown】\n{original}\n\n【修改要求】\n{}\n\n请直接输出改写后的 Markdown：", instruction.trim())
    }
}

fn preceding_context(doc: &Doc, before_block_id: u64) -> String {
    let Some(idx) = doc.index_of(before_block_id) else { return String::new() };
    if idx == 0 {
        return String::new();
    }
    let start = idx.saturating_sub(8);
    let blocks: Vec<_> = doc.blocks.iter().skip(start).take(idx - start).cloned().collect();
    let raw = markdown::serialize(&Doc::from_blocks(blocks));
    let trimmed = raw.trim();
    let chars: Vec<char> = trimmed.chars().collect();
    if chars.len() <= 1200 {
        trimmed.to_owned()
    } else {
        chars[chars.len() - 1200..].iter().collect()
    }
}

fn restore_input_value(input: &Entity<InputState>, value: &str, placeholder: &'static str, window: &mut Window, cx: &mut Context<Editor>) {
    let val = value.to_owned();
    input.update(cx, |i, cx| {
        i.set_placeholder(placeholder, window, cx);
        let end = val.len();
        i.set_value(val, window, cx);
        i.set_selected_range(end..end, cx);
        i.focus(window, cx);
    });
}

impl Editor {
    fn remeasure_ai_rows(&mut self) {
        let Some((first_id, last_id)) = self.ai_session().and_then(|s| Some((s.parts.first()?.0, s.parts.last()?.0))) else {
            return;
        };
        if let (Some(a), Some(b)) = (self.doc.index_of(first_id), self.doc.index_of(last_id)) {
            let start = a.min(b);
            let end = (a.max(b) + 1).min(self.list.item_count());
            if start < end {
                self.list.remeasure_items(start..end);
            }
        }
    }

    fn ai_reanchor(&mut self, entity_id: gpui::EntityId) {
        if let Some(bounds) = super::preview::live_preview_bounds(entity_id) {
            if let Some(s) = self.ai_mut() {
                s.anchor = point(bounds.left(), bounds.bottom() + px(10.));
            }
            return;
        }
        let Some(block) = self.ai_session().and_then(|s| s.parts.last().map(|p| p.0)) else { return };
        let Some(below) = self.layouts.get(&block).map(|l| point(l.bounds.left(), l.bounds.bottom() + px(8.))) else { return };
        if let Some(s) = self.ai_mut() {
            s.anchor = below;
        }
    }

    pub(in crate::editor) fn ai_session(&self) -> Option<&AiSession> {
        match self.format.as_ref() {
            Some(FormatPop::Ai(s)) => Some(s),
            _ => None,
        }
    }

    fn ai_mut(&mut self) -> Option<&mut AiSession> {
        match self.format.as_mut() {
            Some(FormatPop::Ai(s)) => Some(s),
            _ => None,
        }
    }

    fn start_ai(
        &mut self,
        anchor: Point<Pixels>,
        target: (Position, Position),
        parts: Vec<(u64, Range<usize>)>,
        original: String,
        context_before: String,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.cancel_ai();
        self.hover = None;
        super::preview::clear_preview_bounds(cx.entity_id());
        let empty_ctx = original.trim().is_empty();
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(input_placeholder(empty_ctx)));
        let sub = cx.subscribe_in(&input, window, |this, state, event: &InputEvent, window, cx| {
            match event {
                InputEvent::PressEnter { secondary: false, .. } => {
                    this.ai_submit_input(window, cx);
                }
                InputEvent::Change => {
                    let non_empty = !state.read(cx).value().trim().is_empty();
                    if let Some(s) = this.ai_mut() && s.has_input_text != non_empty {
                        s.has_input_text = non_empty;
                        cx.notify();
                    }
                }
                _ => {}
            }
        });
        input.update(cx, |s, cx| s.focus(window, cx));
        let home = window.window_handle();
        let esc = cx.observe_keystrokes(move |this, e, window, cx| {
            if e.keystroke.key == "escape" && window.window_handle() == home && this.ai_session().is_some() {
                this.ai_escape(window, cx);
            }
        });
        self.caret.anchor = target.1;
        self.caret.head = target.1;
        self.caret.composing = None;
        self.format = Some(FormatPop::Ai(Box::new(AiSession {
            anchor,
            target,
            parts,
            original,
            context_before,
            input,
            has_input_text: false,
            status: AiStatus::Input,
            error: None,
            prompt: String::new(),
            restore_input: String::new(),
            cancel: Arc::new(AtomicBool::new(false)),
            serial: 0,
            _sub: Rc::new((sub, esc)),
        })));
        self.remeasure_ai_rows();
        cx.notify();
    }

    /// Selection toolbar entry (`AI 改写`).
    pub(in crate::editor) fn open_ai_selection(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.minimal || !self.is_markdown_note() || !self.can_replace_selection() {
            return;
        }
        let parts = self.selection_ranges();
        if parts.is_empty() {
            return;
        }
        let (a, b, first, last) = self.ordered();
        let original = if first == last {
            let block = &self.doc.blocks[first];
            if a.offset == 0 && b.offset == block.text.len() {
                markdown::serialize(&Doc::from_blocks(vec![block.clone()])).trim().to_owned()
            } else {
                let mut frag = block.slice(a.offset..b.offset);
                frag.kind = crate::editor::model::Kind::Paragraph;
                frag.meta.heading_level = None;
                frag.meta.callout = None;
                frag.meta.indent = 0;
                markdown::serialize(&Doc::from_blocks(vec![frag])).trim().to_owned()
            }
        } else {
            let selected_blocks: Vec<_> = (first..=last)
                .filter_map(|i| {
                    if i == last && b.offset == 0 {
                        return None;
                    }
                    let block = &self.doc.blocks[i];
                    let r = self.selected_range(block.id)?;
                    Some(if r == (0..block.text.len()) { block.clone() } else { block.slice(r) })
                })
                .collect();
            markdown::serialize(&Doc::from_blocks(selected_blocks)).trim().to_owned()
        };
        let last_id = parts.last().map(|(id, _)| *id).unwrap_or(b.block);
        let anchor = self
            .layouts
            .get(&last_id)
            .map(|l| point(l.bounds.left(), l.bounds.bottom() + px(8.)))
            .unwrap_or_else(|| window.mouse_position() + point(px(0.), px(8.)));
        let ctx_before = preceding_context(&self.doc, parts[0].0);
        self.start_ai(anchor, (a, b), parts, original, ctx_before, window, cx);
    }

    /// Slash / space-on-empty-paragraph entry (`AI 写作`).
    pub(in crate::editor) fn open_ai_block(
        &mut self,
        id: u64,
        slash: bool,
        anchor: Option<Point<Pixels>>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.minimal || !self.is_markdown_note() {
            return;
        }
        let Some(block) = self.doc.get(id) else { return };
        if block.meta.table.is_some() {
            self.status = tr("请重新选择正文。", "Please select text first.").into();
            cx.notify();
            return;
        }
        let mut text = block.text.clone();
        if slash && text.starts_with('/') {
            let before = self.snapshot();
            self.doc.set_text(id, String::new());
            self.caret.anchor = Position { block: id, offset: 0 };
            self.caret.head = self.caret.anchor;
            self.record(before, cx);
            text.clear();
        }
        let len = text.len();
        let target = (Position { block: id, offset: 0 }, Position { block: id, offset: len });
        let anchor = anchor.unwrap_or_else(|| window.mouse_position()) + point(px(0.), px(8.));
        let ctx_before = preceding_context(&self.doc, id);
        self.start_ai(anchor, target, vec![(id, 0..len)], text, ctx_before, window, cx);
    }

    fn cancel_ai(&mut self) {
        if let Some(s) = self.ai_session() {
            s.cancel.store(true, Ordering::Relaxed);
        }
    }

    pub(in crate::editor) fn ai_escape(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        thread_local! { static GUARD: std::cell::Cell<bool> = const { std::cell::Cell::new(false) }; }
        if GUARD.with(|cell| cell.get()) {
            return;
        }
        let Some(s) = self.ai_session() else { return };
        if matches!(s.status, AiStatus::Thinking { .. }) {
            GUARD.with(|cell| cell.set(true));
            cx.on_next_frame(window, |_, _, _| GUARD.with(|cell| cell.set(false)));
            self.ai_stop(window, cx);
            return;
        }
        self.ai_close(window, cx);
    }

    pub(in crate::editor) fn ai_submit_input(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_session() else { return };
        if matches!(s.status, AiStatus::Thinking { .. }) {
            return;
        }
        let is_review = matches!(s.status, AiStatus::Review { .. });
        let scope = if is_review || !s.is_empty_ctx() {
            super::inline_prompts::PromptScope::Edit
        } else {
            super::inline_prompts::PromptScope::Empty
        };
        let text = s.input.read(cx).value().trim().to_owned();
        if text.is_empty() {
            if is_review {
                self.ai_accept(window, cx);
            }
            return;
        }
        super::inline_prompts::record(scope, &text);
        let presets = match scope {
            super::inline_prompts::PromptScope::Edit => EDIT_PRESETS,
            super::inline_prompts::PromptScope::Empty => EMPTY_PRESETS,
        };
        let instruction = presets
            .iter()
            .copied()
            .find(|p| p.matches_input(&text))
            .map(|p| p.prompt().to_owned())
            .unwrap_or_else(|| text.clone());
        self.ai_run(instruction, text, true, window, cx);
    }

    pub(in crate::editor) fn ai_run_prompt_target(
        &mut self,
        target: super::inline_prompts::PromptTarget,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        match target {
            super::inline_prompts::PromptTarget::Preset { preset } => self.ai_preset(preset, window, cx),
            super::inline_prompts::PromptTarget::Custom { text } => {
                let clean = text.trim().to_owned();
                if !clean.is_empty() {
                    let typed = self
                        .ai_session()
                        .map(|s| s.input.read(cx).value().to_string())
                        .unwrap_or_default();
                    self.ai_run(clean, typed, false, window, cx);
                }
            }
        }
    }

    pub(in crate::editor) fn ai_toggle_prompt_star(
        &mut self,
        scope: super::inline_prompts::PromptScope,
        target: &super::inline_prompts::PromptTarget,
        cx: &mut Context<Self>,
    ) {
        super::inline_prompts::toggle_star(scope, target);
        cx.notify();
    }

    pub(in crate::editor) fn ai_preset(&mut self, preset: Preset, window: &mut Window, cx: &mut Context<Self>) {
        let typed = self
            .ai_session()
            .map(|s| s.input.read(cx).value().to_string())
            .unwrap_or_default();
        self.ai_run(preset.prompt().to_owned(), typed, false, window, cx);
    }

    /// 重写: reruns the last instruction from scratch against the original target.
    pub(in crate::editor) fn ai_retry(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some((prompt, restore)) = self.ai_session().map(|s| (s.prompt.clone(), s.restore_input.clone())) else {
            return;
        };
        if prompt.is_empty() {
            return;
        }
        self.ai_run(prompt, restore, false, window, cx);
    }

    /// 停止 / 取消生成: stops the active request and restores the user's input text so nothing has to be retyped.
    pub(in crate::editor) fn ai_stop(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let entity_id = cx.entity_id();
        let Some((input, restored, placeholder, back_to_input)) = ({
            let Some(s) = self.ai_mut() else { return };
            s.cancel.store(true, Ordering::Relaxed);
            s.serial += 1;
            s.error = None;
            let prev = match &mut s.status {
                AiStatus::Thinking { prev_draft, .. } => prev_draft.take(),
                _ => None,
            };
            let empty_ctx = s.is_empty_ctx();
            let (next_status, ph, is_input) = match prev {
                Some(draft) => (AiStatus::Review { draft }, review_placeholder(), false),
                None => (AiStatus::Input, input_placeholder(empty_ctx), true),
            };
            s.status = next_status;
            s.has_input_text = !s.restore_input.trim().is_empty();
            Some((s.input.clone(), s.restore_input.clone(), ph, is_input))
        }) else {
            return;
        };
        if back_to_input {
            super::preview::clear_preview_bounds(entity_id);
        }
        restore_input_value(&input, &restored, placeholder, window, cx);
        self.remeasure_ai_rows();
        self.ai_reanchor(entity_id);
        cx.notify();
    }

    fn ai_run(
        &mut self,
        prompt: String,
        restore_input: String,
        refine_existing: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let entity_id = cx.entity_id();
        {
            let Some(s) = self.ai_mut() else { return };
            s.cancel.store(true, Ordering::Relaxed);
            let cancel = Arc::new(AtomicBool::new(false));
            s.cancel = cancel.clone();
            s.serial += 1;
            let serial = s.serial;
            s.prompt = prompt.clone();
            s.restore_input = restore_input;
            s.has_input_text = false;
            s.error = None;
            let prev_draft = match &s.status {
                AiStatus::Review { draft } if refine_existing => Some(draft.clone()),
                _ => None,
            };
            let user = user_prompt(&s.original, &s.context_before, prev_draft.as_deref(), &prompt);
            s.status = AiStatus::Thinking {
                tick: 0,
                partial: String::new(),
                reasoning: String::new(),
                prev_draft,
            };
            let input = s.input.clone();
            #[cfg(not(target_family = "wasm"))]
            let rx = super::inline_backend::stream(SYSTEM_PROMPT.to_owned(), user.clone(), cancel, cx);
            #[cfg(target_family = "wasm")]
            let rx = provider::stream(SYSTEM_PROMPT.to_owned(), user.clone(), cancel);

            input.update(cx, |i, cx| i.set_value("", window, cx));

            cx.spawn_in(window, async move |this, cx| {
                loop {
                    cx.background_executor().timer(Duration::from_millis(40)).await;
                    let mut finished = false;
                    let alive = this.update_in(cx, |this, window, cx| {
                        let Some(s) = this.ai_mut() else {
                            finished = true;
                            return;
                        };
                        if s.serial != serial {
                            finished = true;
                            return;
                        }
                        let empty_ctx = s.is_empty_ctx();
                        let restored_text = s.restore_input.clone();
                        let input_entity = s.input.clone();
                        let AiStatus::Thinking { tick, partial, reasoning, prev_draft } = &mut s.status else {
                            finished = true;
                            return;
                        };
                        *tick += 1;
                        let mut outcome: Option<Result<String, String>> = None;
                        loop {
                            match rx.try_recv() {
                                Ok(Event::Thinking(t)) => reasoning.push_str(&t),
                                Ok(Event::Delta(t)) => partial.push_str(&t),
                                Ok(Event::ToolCalls(_)) => {}
                                Ok(Event::Done) => {
                                    let draft = clean_final(partial);
                                    outcome = Some(if draft.is_empty() {
                                        Err(tr("AI 未返回可写入的内容。", "AI returned empty content.").into())
                                    } else {
                                        Ok(draft)
                                    });
                                    break;
                                }
                                Ok(Event::Failed { message, .. }) => {
                                    outcome = Some(Err(provider::format_error(&message)));
                                    break;
                                }
                                Err(mpsc::TryRecvError::Empty) => break,
                                Err(mpsc::TryRecvError::Disconnected) => {
                                    outcome = Some(Err(tr("生成中断，未对正文进行修改", "Generation interrupted; text unchanged").into()));
                                    break;
                                }
                            }
                        }
                        if let Some(res) = outcome {
                            let prev = prev_draft.take();
                            match res {
                                Ok(draft) => {
                                    s.error = None;
                                    s.status = AiStatus::Review { draft };
                                    input_entity.update(cx, |i, cx| {
                                        i.set_placeholder(review_placeholder(), window, cx);
                                        i.focus(window, cx);
                                    });
                                }
                                Err(msg) => {
                                    s.error = Some(msg);
                                    let (fallback, ph, clear_bounds) = match prev {
                                        Some(draft) => (AiStatus::Review { draft }, review_placeholder(), false),
                                        None => (AiStatus::Input, input_placeholder(empty_ctx), true),
                                    };
                                    s.status = fallback;
                                    s.has_input_text = !restored_text.trim().is_empty();
                                    if clear_bounds {
                                        super::preview::clear_preview_bounds(entity_id);
                                    }
                                    restore_input_value(&input_entity, &restored_text, ph, window, cx);
                                }
                            }
                            finished = true;
                        }
                        this.remeasure_ai_rows();
                        this.ai_reanchor(entity_id);
                        cx.notify();
                    });
                    if alive.is_err() {
                        break;
                    }
                    if finished {
                        cx.background_executor().timer(Duration::from_millis(60)).await;
                        let _ = this.update(cx, |this, cx| {
                            this.remeasure_ai_rows();
                            this.ai_reanchor(entity_id);
                            cx.notify();
                        });
                        break;
                    }
                }
            })
            .detach();
        }
        self.remeasure_ai_rows();
        cx.notify();
    }

    fn validate_ai_target(&self) -> bool {
        let Some(s) = self.ai_session() else { return false };
        let (a, b) = s.target;
        s.parts.iter().all(|(id, r)| {
            self.doc.get(*id).is_some_and(|blk| {
                blk.text.len() >= r.end && blk.text.is_char_boundary(r.start) && blk.text.is_char_boundary(r.end)
            })
        }) && self.doc.get(a.block).is_some()
            && self.doc.get(b.block).is_some()
    }

    /// 完成 / 替换 (`↵`): replaces target with generated native Blocks as one Undo step.
    pub(in crate::editor) fn ai_accept(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_session() else { return };
        let AiStatus::Review { draft } = &s.status else { return };
        let (draft, (a, b)) = (draft.clone(), s.target);
        if !self.validate_ai_target() {
            if let Some(s) = self.ai_mut() {
                s.error = Some(tr("目标块已变化，原文未修改。请重新选择后再试", "Target block changed; text unchanged.").into());
            }
            cx.notify();
            return;
        }
        self.caret.anchor = a;
        self.caret.head = b;
        self.caret.composing = None;
        if self.replace_ai_markdown(&draft, cx).is_none() {
            if let Some(s) = self.ai_mut() {
                s.error = Some(tr("无法在当前选区应用此格式，请选择完整块后重试", "Select whole blocks to apply this format").into());
            }
            cx.notify();
            return;
        }
        super::preview::clear_preview_bounds(cx.entity_id());
        self.format = None;
        window.focus(&self.focus, cx);
        cx.notify();
    }

    /// 插入下方 (selection rewrite mode): keeps original text untouched and inserts generated Blocks right below.
    pub(in crate::editor) fn ai_insert_below(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(s) = self.ai_session() else { return };
        let AiStatus::Review { draft } = &s.status else { return };
        let draft = draft.clone();
        let Some(last_id) = s.parts.last().map(|(id, _)| *id) else { return };
        let Some(last_idx) = self.doc.index_of(last_id) else { return };
        let parsed = markdown::parse(&draft);
        let mut blocks: Vec<_> = parsed.blocks.iter().cloned().collect();
        if blocks.is_empty() {
            return;
        }
        for b in &mut blocks {
            b.id = 0;
            if b.meta.code_language.as_deref() == Some("yaml-frontmatter") {
                b.meta.code_language = Some("yaml".into());
            }
        }
        let count = blocks.len();
        let before = self.snapshot();
        let insert_at = last_idx + 1;
        self.doc.splice_blocks(insert_at..insert_at, blocks);
        let end_block = &self.doc.blocks[insert_at + count - 1];
        let at = Position {
            block: end_block.id,
            offset: end_block.text.len(),
        };
        self.caret.anchor = at;
        self.caret.head = at;
        self.caret.composing = None;
        self.block_sel = None;
        self.table_sel = None;
        self.record_changes(before, vec![ListChange { start: insert_at, before: 0, after: count }], true, cx);
        super::preview::clear_preview_bounds(cx.entity_id());
        self.format = None;
        window.focus(&self.focus, cx);
        cx.notify();
    }

    /// 放弃 / 关闭: stops any request, leaves text untouched and restores selection.
    pub(in crate::editor) fn ai_close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let target = self.ai_session().map(|s| s.target);
        super::preview::clear_preview_bounds(cx.entity_id());
        self.cancel_ai();
        self.close_format(window, cx);
        if let Some((a, b)) = target {
            if self.doc.get(a.block).is_some() && self.doc.get(b.block).is_some() {
                self.caret.anchor = a;
                self.caret.head = b;
            }
            window.focus(&self.focus, cx);
        }
        self.remeasure_ai_rows();
        cx.notify();
    }
}

/// Drops a wrapping ```markdown / ```md fence the model added despite the instruction.
pub(in crate::editor) fn clean_stream(text: &str) -> String {
    let trimmed = text.trim_start();
    if let Some(after_ticks) = trimmed.strip_prefix("```") {
        let first_line_end = after_ticks.find('\n');
        let lang = match first_line_end {
            Some(pos) => after_ticks[..pos].trim().to_ascii_lowercase(),
            None => return String::new(),
        };
        if lang.is_empty() || lang == "markdown" || lang == "md" {
            let body = &after_ticks[first_line_end.unwrap() + 1..];
            let body = body.trim_end();
            let body = body.strip_suffix("```").unwrap_or(body);
            return body.trim_end_matches(['\r', '\n']).to_owned();
        }
    }
    text.trim_end_matches(['\r', '\n']).to_owned()
}

fn clean_final(text: &str) -> String {
    clean_stream(text).trim().to_owned()
}
