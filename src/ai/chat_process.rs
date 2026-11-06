use std::{cell::RefCell, collections::HashMap};
use gpui::{div, px, CursorStyle, FontWeight, IntoElement, MouseButton, ParentElement, ScrollHandle, SharedString, Styled, prelude::*};
use serde_json::Value;
use super::{agent, chat::{AiChat, Msg}};
use crate::{icons::{icon, Icon}, theme::{is_en, tr, Theme}, ui::ai_process_card::{process_card, ProcessCardSpec, ProcessStage, ProcessStep}};

fn glyph(i: Icon, size: f32, color: gpui::Hsla) -> impl IntoElement { icon(i, px(size), Theme::ocean().mono_icon_colors(color)) }

struct TurnUiState { steps_expanded: bool, notes_expanded: bool, steps_scroll: ScrollHandle, last_step_count: usize }
impl Default for TurnUiState {
    fn default() -> Self { Self { steps_expanded: true, notes_expanded: false, steps_scroll: ScrollHandle::new(), last_step_count: 0 } }
}
thread_local! {
    static TURNS: RefCell<HashMap<String, TurnUiState>> = RefCell::new(HashMap::new());
    static SMOKE_STEPS: RefCell<(Vec<ProcessStep>, Vec<String>)> = RefCell::new((Vec::new(), Vec::new()));
}

pub(crate) fn smoke_process_data() -> (Vec<ProcessStep>, Vec<String>) { SMOKE_STEPS.with(|data| data.borrow().clone()) }

fn add_note(notes: &mut Vec<String>, value: Option<&str>) {
    if let Some(value) = value.filter(|value| !value.is_empty()) {
        if !notes.iter().any(|note| note == value) { notes.push(value.to_owned()); }
    }
}

pub(super) fn record_tool(chat: &mut AiChat, request: &super::backend::ToolRequest, result: &Value, with_result: bool) {
    chat.push_msg(Msg { role: "assistant".into(), tool_calls: Some(serde_json::json!([{ "id": request.request_id, "type": "function", "function": { "name": request.name, "arguments": request.arguments.to_string() } }])), ..Default::default() });
    if with_result { chat.push_msg(Msg { role: "tool".into(), content: result.to_string(), tool_call_id: Some(request.request_id.clone()), ..Default::default() }); }
}

fn step_for(call: &Value, result: &Value, notes: &mut Vec<String>) -> (ProcessStage, ProcessStep) {
    let name = call.pointer("/function/name").and_then(Value::as_str).unwrap_or("");
    let args = call.pointer("/function/arguments").and_then(|v| v.as_str().and_then(|s| serde_json::from_str::<Value>(s).ok()).or_else(|| v.as_object().map(|_| v.clone()))).unwrap_or_default();
    let items = match name { "listPages" => result.as_array().or_else(|| result.get("pages").and_then(Value::as_array)), "searchNotes" => result.get("results").and_then(Value::as_array), "listNotebooks" => result.get("notebooks").and_then(Value::as_array), _ => None };
    if matches!(name, "listPages" | "searchNotes") {
        for item in items.into_iter().flatten() { add_note(notes, item.get("title").or_else(|| item.get("pageId")).and_then(Value::as_str)); }
    }
    if name == "executeBatchPlan" {
        for item in args.get("ops").or_else(|| args.get("operations")).and_then(Value::as_array).into_iter().flatten() {
            add_note(notes, item.get("title").or_else(|| item.get("pageId")).and_then(Value::as_str));
        }
    }
    let title = result.get("title").or_else(|| result.get("pageId")).or_else(|| args.get("pageId")).and_then(Value::as_str).unwrap_or("");
    if name == "readPage" { add_note(notes, Some(title)); }
    let (stage, label) = match name {
        "loadSkill" | "readSkillResource" => (ProcessStage::Skill, tr("加载工作区技能", "Load workspace skill").to_owned()),
        "listNotebooks" => (ProcessStage::Scan, tr("加载工作区目录", "Load workspace roots").to_owned()),
        "listPages" => (ProcessStage::Scan, tr("扫描笔记本目录", "Scan notebook pages").to_owned()),
        "searchNotes" => (ProcessStage::Scan, tr("搜索相关笔记", "Search notes").to_owned()),
        "readPage" => (ProcessStage::Read, if title.is_empty() { tr("读取笔记", "Read note").to_owned() } else { format!("{} · {title}", tr("读取笔记", "Read note")) }),
        "executeBatchPlan" => (ProcessStage::Write, tr("生成笔记变更计划", "Create note change plan").to_owned()),
        _ => (ProcessStage::Scan, tr("调用工具", "Call tool").to_owned()),
    };
    let detail = if result.get("error").is_some() { tr("失败", "Failed").to_owned() } else if let Some(items) = items {
        match name { "listNotebooks" => if is_en() { format!("{} workspaces", items.len()) } else { format!("{} 个工作区", items.len()) }, "listPages" => if is_en() { format!("{} notes", items.len()) } else { format!("{} 篇", items.len()) }, _ => if is_en() { format!("{} matches", items.len()) } else { format!("{} 条命中", items.len()) } }
    } else { tr("完成", "Done").to_owned() };
    (stage, ProcessStep { label, detail, merged_count: 1, running: false })
}

pub(super) fn render_messages(chat: &AiChat, cx: &mut gpui::Context<AiChat>) -> gpui::Div {
    let theme = Theme::ocean();
    let mut body = div().flex().flex_col().gap(px(16.)).px(px(16.)).py(px(20.));
    let Some(conv) = chat.conv() else { return body };
    let mut start = 0;
    while start < conv.messages.len() {
        let end = (start + 1..conv.messages.len()).find(|&index| conv.messages[index].role == "user").unwrap_or(conv.messages.len());
        let turn = &conv.messages[start..end];
        if turn[0].role == "user" {
            body = body.child(div().flex().justify_end().child(div().relative().children(crate::ui::smoke_overlay::probe("ai-user-message")).max_w(px(300.)).px(px(12.)).py(px(8.)).rounded(crate::ui::radius::SURFACE).bg(theme.hover)
                .text_size(px(14.)).line_height(px(22.)).child(crate::ui::selectable_message::message(format!("ai-user-{}-{start}", conv.id), &turn[0].content, false))));
        }
        let mut steps: Vec<ProcessStep> = Vec::new();
        let mut notes = Vec::new();
        let mut stage = ProcessStage::Synth;
        for message in turn {
            for call in message.tool_calls.as_ref().and_then(Value::as_array).into_iter().flatten() {
                let id = call.get("id").and_then(Value::as_str);
                let result = turn.iter().find(|m| m.role == "tool" && m.tool_call_id.as_deref() == id).and_then(|m| serde_json::from_str::<Value>(&m.content).ok()).unwrap_or_default();
                let (next_stage, step) = step_for(call, &result, &mut notes);
                stage = next_stage;
                if let Some(last) = steps.last_mut().filter(|last| last.label == step.label) { last.merged_count += 1; last.detail = step.detail; }
                else { steps.push(step); }
            }
        }
        let streaming = end == conv.messages.len() && chat.streaming.is_some();
        if streaming && (!chat.partial.is_empty() || steps.is_empty()) {
            stage = ProcessStage::Synth;
            steps.push(ProcessStep { label: if chat.partial.is_empty() { tr("分析问题与上下文", "Analyze question and context") } else { tr("生成结构化回答", "Generate answer") }.into(), detail: tr("进行中", "In progress").into(), merged_count: 1, running: true });
        } else if streaming {
            if let Some(last) = steps.last_mut() { last.running = true; last.detail = tr("进行中", "In progress").into(); }
        }
        if steps.len() > 16 { steps.drain(0..steps.len() - 16); }
        if !steps.is_empty() {
            if crate::smoke::is_requested() { SMOKE_STEPS.with(|data| *data.borrow_mut() = (steps.clone(), notes.clone())); }
            let headline = if streaming { match stage {
                ProcessStage::Skill => tr("正在加载工作区技能…", "Loading workspace skill…"), ProcessStage::Scan => tr("正在扫描笔记本目录…", "Scanning notebook pages…"), ProcessStage::Read => tr("正在读取笔记内容…", "Reading notes…"), ProcessStage::Write => tr("正在生成笔记变更计划…", "Creating note change plan…"), _ => tr("正在汇总并生成回答…", "Preparing answer…"),
            }.to_owned() } else { stage = ProcessStage::Done; if is_en() { format!("Completed {} steps", steps.len()) } else { format!("已完成 {} 步执行", steps.len()) } };
            let key = format!("{}-{start}", conv.id);
            let (steps_expanded, notes_expanded, steps_scroll) = TURNS.with(|states| {
                let mut states = states.borrow_mut(); let state = states.entry(key.clone()).or_default();
                if steps.len() > 4 && steps.len() != state.last_step_count { state.steps_scroll.scroll_to_bottom(); }
                state.last_step_count = steps.len(); (state.steps_expanded, state.notes_expanded, state.steps_scroll.clone())
            });
            let step_key = key.clone(); let notes_key = key.clone();
            body = body.child(process_card(ProcessCardSpec { key, stage, headline, steps, notes, steps_expanded, notes_expanded, steps_scroll },
                cx.listener(move |_, _, _, cx| { TURNS.with(|states| { let mut states = states.borrow_mut(); let state = states.entry(step_key.clone()).or_default(); state.steps_expanded = !state.steps_expanded; }); cx.notify(); }),
                cx.listener(move |_, _, _, cx| { TURNS.with(|states| { let mut states = states.borrow_mut(); let state = states.entry(notes_key.clone()).or_default(); state.notes_expanded = !state.notes_expanded; }); cx.notify(); })));
        }
        for (offset, message) in turn.iter().enumerate() {
            if let Some(plan) = &message.plan { body = body.child(plan_card(start + offset, plan.clone(), cx)); }
        }
        for (offset, message) in turn.iter().enumerate().filter(|(_, m)| m.role == "assistant" && !m.content.trim().is_empty()) {
            body = body.child(reply(&message.content, conv.id, start + offset, true));
        }
        if streaming && !chat.partial.trim().is_empty() { body = body.child(reply(&chat.partial, conv.id, end, false)); }
        start = end;
    }
    body
}

fn reply(text: &str, conv_id: u64, index: usize, copyable: bool) -> impl IntoElement {
    let theme = Theme::ocean(); let copy = text.to_owned();
    div().flex().flex_col().gap(px(4.))
        .child(div().relative().children(crate::ui::smoke_overlay::probe("ai-assistant-message")).child(super::md_view::render(text, &format!("{conv_id}-{index}"))))
        .when(copyable, |d| d.child(div().id(SharedString::from(format!("copy-{index}"))).w_auto().text_size(px(14.)).text_color(theme.text_secondary)
            .cursor(CursorStyle::PointingHand).hover(move |s| s.text_color(theme.text))
            .on_mouse_down(MouseButton::Left, move |_, _, cx| cx.write_to_clipboard(gpui::ClipboardItem::new_string(copy.clone())))
            .child(tr("复制", "Copy"))))
}

/// thinkly's NotebookBatchPlanCard: 笔记变更计划 with 原文 / 新片段, 应用 and 撤回.
fn plan_card(at: usize, plan: agent::Plan, cx: &mut gpui::Context<AiChat>) -> impl IntoElement {
    use agent::{OpKind, PlanState};
    let theme = Theme::ocean();
    let n = plan.ops.len();
    let (badge, note) = match plan.state {
        PlanState::Pending => (tr("等待确认", "Pending"), tr("方案已生成，尚未修改笔记。", "Plan generated; notes not modified yet.").to_owned()),
        PlanState::Applied => (tr("已应用", "Applied"), if is_en() { format!("Applied {n} operations.") } else { format!("已应用 {} 项操作。", n) }),
        PlanState::Partial => (tr("部分写入失败", "Partially failed"), tr("部分修改已写入，其余修改失败；可撤回已写入内容。", "Some changes were written, but others failed. You can undo the written changes.").to_owned()),
        PlanState::Canceled => (tr("已取消", "Canceled"), tr("未修改笔记。", "Notes not modified.").to_owned()),
        PlanState::Undone => (tr("已撤回", "Undone"), tr("本次变更已撤回。", "Changes have been undone.").to_owned()),
        PlanState::UndoConflict => (tr("撤回冲突", "Undo conflict"), tr("笔记在应用后发生变化，未覆盖后续编辑。", "Note changed after applying; later edits preserved.").to_owned()),
        PlanState::Stale => (tr("已取消", "Canceled"), tr("原文或目标位置已变化，请基于当前笔记重新生成方案。", "Source text changed; regenerate plan from current note.").to_owned()),
    };
    let snippet = |label: &'static str, text: String, del: bool| div().flex().flex_col().gap(px(2.))
        .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(label))
        .child(div().px(px(8.)).py(px(8.)).rounded(crate::ui::radius::CONTROL).text_size(px(14.)).line_height(px(20.))
            .bg(if del { gpui::hsla(0., 0.75, 0.5, 0.08) } else { gpui::hsla(0.38, 0.6, 0.4, 0.10) })
            .when(del, |d| d.line_through())
            .child(if text.is_empty() { tr("（空）", "(Empty)").to_owned() } else { text.chars().take(400).collect() }));
    let ops = plan.ops.iter().fold(div().flex().flex_col().gap(px(8.)), |d, op| {
        let (label, body) = match &op.kind {
            OpKind::Replace { old, new, all } => (if *all { tr("替换全部匹配", "Replace all matches") } else { tr("替换首处匹配", "Replace first match") },
                div().flex().flex_col().gap(px(4.)).child(snippet(tr("原文", "Original"), old.clone(), true)).child(snippet(tr("新片段", "Replacement"), new.clone(), false))),
            OpKind::Edit { markdown } => (tr("替换整篇正文", "Replace full note"), div().child(snippet(tr("新正文", "New content"), markdown.clone(), false))),
            OpKind::Create { markdown } => (tr("新建页面", "Create page"), div().child(snippet(tr("正文", "Content"), markdown.clone(), false))),
        };
        d.child(div().flex().flex_col().gap(px(4.))
            .child(div().flex().items_center().gap(px(6.)).text_size(px(14.))
                .child(div().font_weight(FontWeight(600.)).child(label))
                .child(div().text_color(theme.text_secondary).child(op.page.clone())))
            .child(body))
    });
    let actions = match plan.state {
        PlanState::Pending => Some(div().flex().gap(px(8.))
            .child(crate::ui::ai_controls::action_chip(format!("plan-apply-{at}"), None, tr("接受", "Accept"), true, false,
                cx.listener(move |this, _, _, cx| this.apply_plan(at, cx))).h(px(32.)).rounded(crate::ui::radius::CONTROL))
            .child(crate::ui::ai_controls::action_chip(format!("plan-cancel-{at}"), None, tr("拒绝", "Reject"), false, false,
                cx.listener(move |this, _, _, cx| this.set_plan_state(at, PlanState::Canceled, cx))).h(px(32.)).rounded(crate::ui::radius::CONTROL).border_1().border_color(theme.btn_border))),
        state @ (PlanState::Applied | PlanState::Partial) => Some(div().flex().justify_end()
            .child(crate::ui::ai_controls::action_chip(format!("plan-undo-{at}"), None,
                if state == PlanState::Partial { tr("撤回已写入的修改", "Undo written changes") } else { tr("撤销", "Undo") }, false, false,
                cx.listener(move |this, _, _, cx| this.undo_plan(at, cx))).h(px(32.)).rounded(crate::ui::radius::CONTROL).border_1().border_color(theme.btn_border))),
        PlanState::Stale => Some(div().flex().justify_end()
            .child(crate::ui::ai_controls::action_chip(format!("plan-regen-{at}"), None, tr("重新生成方案", "Regenerate plan"), false, false,
                cx.listener(move |this, _, w, cx| {
                    this.input.update(cx, |i, cx| i.set_value(tr("请基于当前笔记重新生成方案", "Please regenerate the plan based on the current note"), w, cx));
                    this.send(w, cx);
                })).h(px(32.)).rounded(crate::ui::radius::CONTROL).border_1().border_color(theme.btn_border))),
        _ => None,
    };
    let head = if plan.title.is_empty() { tr("笔记变更计划", "Note Change Plan").to_owned() } else if is_en() { format!("Note Change Plan · {}", plan.title) } else { format!("笔记变更计划 · {}", plan.title) };
    div().flex().flex_col().gap(px(8.)).p(px(12.)).rounded(crate::ui::radius::SURFACE).border_1().border_color(theme.btn_border).bg(theme.paper)
        .child(div().flex().items_center().gap(px(8.))
            .child(glyph(Icon::Sparkles, 14., theme.accent))
            .child(div().flex_1().text_size(px(14.)).font_weight(FontWeight(600.)).child(head))
            .child(div().px(px(6.)).py(px(1.)).rounded(crate::ui::radius::MICRO_SM).bg(theme.hover).text_size(px(14.)).text_color(theme.text_secondary).child(badge)))
        .when(!plan.summary.is_empty(), |d| d.child(div().text_size(px(14.)).text_color(theme.text_secondary).child(plan.summary.clone())))
        .child(ops)
        .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(note))
        .children(actions)
}
