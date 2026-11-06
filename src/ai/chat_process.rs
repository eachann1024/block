use std::{cell::RefCell, collections::HashMap};
use gpui::{div, px, CursorStyle, IntoElement, MouseButton, ParentElement, ScrollHandle, SharedString, Styled, prelude::*};
use serde_json::Value;
use super::{agent, chat::{AiChat, Msg}};
use crate::{theme::{is_en, tr, Theme}, ui::ai_process_card::{process_card, ProcessCardSpec, ProcessStage, ProcessStep}};

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

fn plan_card(at: usize, plan: agent::Plan, cx: &mut gpui::Context<AiChat>) -> impl IntoElement {
    use agent::{OpKind, PlanState};
    use crate::ui::ai_plan_card::{chat_plan_card, pill};
    let pending = plan.state == PlanState::Pending;
    let badge = match plan.state { PlanState::Pending => tr("编辑器待审阅", "Review in Editor"), PlanState::Applied => tr("已应用", "Applied"),
        PlanState::Partial => tr("部分写入失败", "Partially failed"), PlanState::Canceled => tr("已取消", "Canceled"), PlanState::Undone => tr("已撤回", "Undone"),
        PlanState::UndoConflict => tr("撤回冲突", "Undo conflict"), PlanState::Stale => tr("已失效", "Stale") };
    let active = super::plan_link::current();
    let mut ops = Vec::new();
    for (index, op) in plan.ops.iter().enumerate() {
        let source = match &op.kind { OpKind::Replace { new, old, .. } => if new.is_empty() { old } else { new }, OpKind::Edit { markdown } | OpKind::Create { markdown } => markdown };
        let label = source.lines().find(|line| line.trim_start().starts_with('#')).or_else(|| source.lines().find(|line| !line.trim().is_empty())).unwrap_or(&op.page).trim().trim_start_matches('#').trim();
        let label: String = label.chars().take(30).collect();
        let number = ["①", "②", "③", "④", "⑤", "⑥", "⑦", "⑧", "⑨", "⑩"].get(index).map_or_else(|| (index + 1).to_string(), |number| (*number).to_owned());
        let label = format!("{number} {label} {}", tr("（在编辑器审阅）", "(Review in editor)"));
        let selected = active.as_ref().is_some_and(|link| link.msg_at == at && link.op_idx == index);
        if pending { ops.push(pill(format!("plan-op-{at}-{index}"), label, selected, cx.listener(move |this, _, _, cx| this.link_plan(at, index, false, cx))).into_any_element()); }
    }
    if pending && !plan.ops.is_empty() {
        let index = active.as_ref().filter(|link| link.msg_at == at).map_or(0, |link| link.op_idx);
        let diff = !active.as_ref().is_some_and(|link| link.msg_at == at && link.diff_mode);
        ops.push(pill(format!("plan-toggle-diff-{at}"), tr("② 切换对比明细", "② Toggle comparison").into(), false,
            cx.listener(move |this, _, _, cx| this.link_plan(at, index, diff, cx))).into_any_element());
    }
    let mut actions = Vec::new();
    if pending {
        actions.push(pill(format!("plan-apply-{at}"), tr("接受", "Accept").into(), true, cx.listener(move |this, _, _, cx| this.apply_plan(at, cx))).into_any_element());
        actions.push(pill(format!("plan-cancel-{at}"), tr("拒绝", "Reject").into(), false, cx.listener(move |this, _, _, cx| this.set_plan_state(at, PlanState::Canceled, cx))).into_any_element());
    } else if matches!(plan.state, PlanState::Applied | PlanState::Partial) {
        actions.push(pill(format!("plan-undo-{at}"), tr("撤销", "Undo").into(), false, cx.listener(move |this, _, _, cx| this.undo_plan(at, cx))).into_any_element());
    } else if plan.state == PlanState::Stale {
        actions.push(pill(format!("plan-regen-{at}"), tr("重新生成方案", "Regenerate plan").into(), false, cx.listener(move |this, _, window, cx| {
            this.input.update(cx, |input, cx| input.set_value(tr("请基于当前笔记重新生成方案", "Please regenerate the plan based on the current note"), window, cx)); this.send(window, cx);
        })).into_any_element());
    }
    let title = plan.ops.first().map(|op| format!("{}.md", op.page)).unwrap_or(plan.title);
    chat_plan_card(title, badge, plan.summary, pending, ops, actions)
}
