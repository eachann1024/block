use crate::ui::tooltip::TooltipExt;
use std::{cell::RefCell, collections::HashMap, rc::Rc};
use gpui::{div, px, IntoElement, ParentElement, ScrollHandle, SharedString, Styled, prelude::*};
use serde_json::Value;
use super::{agent, chat::{AiChat, Msg}};
use crate::{theme::{is_en, tr}, ui::{ai_process_card::{process_card_with_options, ProcessCardOptions, ProcessCardSpec, ProcessStage, ProcessStep}, ai_process_status::{ProcessElapsed, TaskStatus}}};
#[path = "chat_process_trace.rs"] mod trace;
#[path = "chat_process_sources.rs"] mod sources;
pub(super) use trace::{record_activity, record_outcome, record_summary, start_turn};

struct TurnUiState { steps_expanded: bool, steps_toggle_seq: usize, notes_expanded: bool, sources_expanded: bool, steps_scroll: ScrollHandle, last_step_count: usize }
impl Default for TurnUiState {
    fn default() -> Self { Self { steps_expanded: false, steps_toggle_seq: 0, notes_expanded: false, sources_expanded: false, steps_scroll: ScrollHandle::new(), last_step_count: 0 } }
}
thread_local! {
    static TURNS: RefCell<HashMap<String, TurnUiState>> = RefCell::new(HashMap::new());
    static SMOKE_STEPS: RefCell<(Vec<ProcessStep>, Vec<String>)> = RefCell::new((Vec::new(), Vec::new()));
    static SMOKE_REPLIES: RefCell<Vec<String>> = const { RefCell::new(Vec::new()) };
}

pub(crate) fn smoke_process_data() -> (Vec<ProcessStep>, Vec<String>) { SMOKE_STEPS.with(|data| data.borrow().clone()) }
pub(crate) fn smoke_visible_replies() -> Vec<String> { SMOKE_REPLIES.with(|data| data.borrow().clone()) }

pub(super) fn open_created_plan(chat: &mut AiChat, cx: &mut gpui::Context<AiChat>) {
    let at = chat.conv().and_then(|conversation| conversation.messages.iter().enumerate().rev().take_while(|(_, message)| message.role != "user")
        .find(|(_, message)| message.plan.as_ref().is_some_and(|plan| plan.state == agent::PlanState::Applied && !plan.ops.is_empty()
            && plan.ops.iter().all(|op| matches!(op.kind, agent::OpKind::Create { .. })))).map(|(index, _)| index));
    if let Some(at) = at { chat.open_plan_page(at, 0, cx); }
}

fn add_note(notes: &mut Vec<String>, value: Option<&str>) {
    if let Some(value) = value.filter(|value| !value.is_empty()) {
        if !notes.iter().any(|note| note == value) { notes.push(value.to_owned()); }
    }
}

pub(super) fn record_tool(chat: &mut AiChat, request: &super::backend::ToolRequest, result: &Value, with_result: bool, cx: &gpui::App) {
    let roots = (request.name == "readPage").then(|| {
        let root = request.arguments.get("notebookRoot").and_then(Value::as_str).map(str::to_owned).unwrap_or_else(|| chat.agent_ctx(cx).root.to_string_lossy().into_owned());
        (!root.is_empty()).then(|| vec![(request.request_id.clone(), root)])
    }).flatten();
    chat.push_msg(Msg { role: "assistant".into(), source_roots: roots, tool_calls: Some(serde_json::json!([{ "id": request.request_id, "type": "function", "function": { "name": request.name, "arguments": request.arguments.to_string() } }])), ..Default::default() });
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
    if crate::smoke::is_requested() { SMOKE_REPLIES.with(|data| data.borrow_mut().clear()); }
    let theme = crate::ui::ai_theme::theme();
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
        let mut details: Vec<String> = Vec::new();
        let mut notes = Vec::new();
        let mut stage = ProcessStage::Synth;
        for message in turn {
            if message.role == "public_summary" && !message.content.trim().is_empty() {
                steps.push(ProcessStep { label: tr("推理摘要", "Reasoning summary").into(), detail: tr("状态未知", "Status unknown").into(), merged_count: 1, running: false });
                details.push(message.content.clone());
            }
            if message.role == "activity" {
                steps.push(ProcessStep { label: message.content.clone(), detail: tr("工具活动", "Tool activity").into(), merged_count: 1, running: false });
                details.push(String::new());
            }
            for call in message.tool_calls.as_ref().and_then(Value::as_array).into_iter().flatten() {
                let id = call.get("id").and_then(Value::as_str);
                let result = turn.iter().find(|m| m.role == "tool" && m.tool_call_id.as_deref() == id).and_then(|m| serde_json::from_str::<Value>(&m.content).ok());
                let (next_stage, mut step) = step_for(call, result.as_ref().unwrap_or(&Value::Null), &mut notes);
                if result.is_none() { step.detail = tr("状态未知", "Status unknown").into(); }
                let name = call.pointer("/function/name").and_then(Value::as_str).unwrap_or("");
                let args = call.pointer("/function/arguments").and_then(Value::as_str).and_then(|text| serde_json::from_str::<Value>(text).ok()).unwrap_or_default();
                let parameter = if name == "readPage" { args.get("pageId") } else if name == "searchNotes" { args.get("query") } else { None }.and_then(Value::as_str).unwrap_or("");
                let detail = format!("{} {} · {}", name.chars().take(80).collect::<String>(), parameter.chars().take(80).collect::<String>(), step.detail);
                stage = next_stage;
                if let Some(last) = steps.last_mut().filter(|last| last.label == step.label) { last.merged_count += 1; last.detail = step.detail; if let Some(last_detail) = details.last_mut() { *last_detail = detail; } }
                else { steps.push(step); details.push(detail); }
            }
        }
        let streaming = end == conv.messages.len() && chat.streaming.is_some();
        if streaming && (!chat.partial.is_empty() || steps.is_empty()) {
            stage = ProcessStage::Synth;
            steps.push(ProcessStep { label: if chat.partial.is_empty() { tr("等待模型响应", "Waiting for model response") } else { tr("正在生成回答", "Generating answer") }.into(), detail: tr("进行中", "In progress").into(), merged_count: 1, running: true });
            details.push(String::new());
        } else if streaming {
            steps.push(ProcessStep { label: tr("等待模型响应", "Waiting for model response").into(), detail: tr("进行中", "In progress").into(), merged_count: 1, running: true });
            details.push(String::new());
        }
        if steps.len() > 16 { let excess = steps.len() - 16; steps.drain(0..excess); details.drain(0..excess); }
        if !steps.is_empty() {
            if crate::smoke::is_requested() { SMOKE_STEPS.with(|data| *data.borrow_mut() = (steps.clone(), notes.clone())); }
            let headline = if streaming { match stage {
                ProcessStage::Skill => tr("正在加载工作区技能…", "Loading workspace skill…"), ProcessStage::Scan => tr("正在扫描笔记本目录…", "Scanning notebook pages…"), ProcessStage::Read => tr("正在读取笔记内容…", "Reading notes…"), ProcessStage::Write => tr("正在生成笔记变更计划…", "Creating note change plan…"), _ => tr("正在汇总并生成回答…", "Preparing answer…"),
            }.to_owned() } else {
                stage = ProcessStage::Done;
                match turn.iter().rev().find(|message| message.role == "turn_outcome").map(|message| message.content.as_str()) {
                    Some("completed") => if is_en() { format!("Completed {} steps", steps.len()) } else { format!("已完成 {} 步执行", steps.len()) },
                    Some("failed") => tr("执行失败", "Execution failed").into(),
                    Some("stopped") => tr("已停止", "Stopped").into(),
                    _ => tr("执行记录", "Activity").into(),
                }
            };
            let key = format!("{}-{start}", conv.id);
            let (steps_expanded, steps_toggle_seq, notes_expanded, steps_scroll) = TURNS.with(|states| {
                let mut states = states.borrow_mut(); let state = states.entry(key.clone()).or_default();
                if steps.len() > 4 && steps.len() != state.last_step_count { state.steps_scroll.scroll_to_bottom(); }
                state.last_step_count = steps.len(); (state.steps_expanded, state.steps_toggle_seq, state.notes_expanded, state.steps_scroll.clone())
            });
            let step_key = key.clone(); let notes_key = key.clone();
            let status = if streaming { TaskStatus::Running } else { match turn.iter().rev().find(|message| message.role == "turn_outcome").map(|message| message.content.as_str()) {
                Some("failed") => TaskStatus::Failed, Some("stopped") => TaskStatus::Stopped, Some("completed") => TaskStatus::Completed, _ => TaskStatus::Unknown,
            }};
            let elapsed = turn.first().and_then(|message| message.timing).and_then(|(start, end)| {
                let duration = std::time::Duration::from_millis(end.unwrap_or_else(trace::now_ms).saturating_sub(start));
                if streaming { std::time::Instant::now().checked_sub(duration).map(ProcessElapsed::Running) }
                else { end.map(|_| ProcessElapsed::Finished(duration)) }
            });
            let step_statuses = steps.iter().map(|step| if step.running { TaskStatus::Running } else if step.detail == tr("失败", "Failed") { TaskStatus::Failed } else if step.detail == tr("状态未知", "Status unknown") || step.detail == tr("工具活动", "Tool activity") { TaskStatus::Unknown } else { TaskStatus::Completed }).collect();
            let weak_chat = cx.weak_entity();
            let on_details_changed = Some(Rc::new(move |cx: &mut gpui::App| {
                if let Some(chat) = weak_chat.upgrade() { chat.update(cx, |_, cx| cx.notify()); }
            }) as Rc<dyn Fn(&mut gpui::App)>);
            body = body.child(process_card_with_options(ProcessCardSpec { key, stage, headline, steps, notes, steps_expanded, steps_toggle_seq, notes_expanded, steps_scroll }, ProcessCardOptions { status: Some(status), elapsed, step_statuses, step_details: details, on_details_changed, ..Default::default() },
                cx.listener(move |_, _, _, cx| { TURNS.with(|states| { let mut states = states.borrow_mut(); let state = states.entry(step_key.clone()).or_default(); state.steps_expanded = !state.steps_expanded; state.steps_toggle_seq += 1; }); cx.notify(); }),
                cx.listener(move |_, _, _, cx| { TURNS.with(|states| { let mut states = states.borrow_mut(); let state = states.entry(notes_key.clone()).or_default(); state.notes_expanded = !state.notes_expanded; }); cx.notify(); })));
        }
        for (offset, message) in turn.iter().enumerate() {
            if let Some(plan) = &message.plan { body = body.child(plan_card(start + offset, plan.clone(), cx)); }
        }
        let has_plan = turn.iter().any(|message| message.plan.is_some() || message.tool_calls.as_ref().and_then(Value::as_array)
            .is_some_and(|calls| calls.iter().any(|call| call.pointer("/function/name").and_then(Value::as_str) == Some("executeBatchPlan")
                && !turn.iter().any(|result| result.role == "tool" && result.tool_call_id.as_deref() == call.get("id").and_then(Value::as_str)))));
        if !has_plan {
            for (offset, message) in turn.iter().enumerate().filter(|(_, m)| m.role == "assistant" && !m.content.trim().is_empty()) {
                body = body.child(reply(&message.content, conv.id, start + offset, !streaming, false));
            }
            if streaming && !chat.partial.trim().is_empty() {
                body = body.child(reply(&chat.partial, conv.id, end, false, true));
            }
            if !streaming && turn.iter().any(|m| m.role == "assistant" && !m.content.trim().is_empty()) {
                body = body.child(crate::ui::ai_plan_card::followup_action(format!("ai-follow-up-{start}"), tr("请展开说明", "Explain in more detail").into(),
                    cx.listener(|this, _, window, cx| { this.input.update(cx, |input, cx| {
                        let draft = input.value().to_owned();
                        let prompt = tr("请展开说明", "Please explain in more detail");
                        let value = if draft.trim().is_empty() { prompt.to_owned() } else { format!("{draft}\n{prompt}") };
                        input.set_value(&value, window, cx); input.focus(window, cx);
                    }); })).into_any_element());
            }
        }
        let sources = sources::render(turn, start, cx);
        if !sources.is_empty() {
            let source_key = format!("{}-{start}", conv.id);
            let expanded = TURNS.with(|states| states.borrow_mut().entry(source_key.clone()).or_default().sources_expanded);
            body = body.child(crate::ui::ai_plan_card::context_card(sources, expanded, cx.listener(move |_, _, _, cx| {
                TURNS.with(|states| { let mut states = states.borrow_mut(); let state = states.entry(source_key.clone()).or_default(); state.sources_expanded = !state.sources_expanded; }); cx.notify();
            })));
        }
        start = end;
    }
    body
}

fn reply(text: &str, conv_id: u64, index: usize, copyable: bool, streaming: bool) -> impl IntoElement {
    if crate::smoke::is_requested() { SMOKE_REPLIES.with(|data| data.borrow_mut().push(text.to_owned())); }
    let copy = text.to_owned();
    let group = SharedString::from(format!("reply-{conv_id}-{index}"));
    div().group(group.clone()).w_full().flex().flex_col().gap(px(4.))
        .child(div().relative().children(crate::ui::smoke_overlay::probe("ai-assistant-message")).child(super::md_view::render(text, &format!("{conv_id}-{index}"), streaming)))
        .when(copyable, |d| d.child(div().w_full().flex().justify_end().child(
            crate::ui::icon_button::copy_button(
                SharedString::from(format!("copy-{conv_id}-{index}")), false, 1.,
                move |_, _, cx| {
                    cx.stop_propagation();
                    cx.write_to_clipboard(gpui::ClipboardItem::new_string(copy.clone()));
                },
            )
            .relative().children(crate::ui::smoke_overlay::probe("ai-reply-copy"))
            .when(crate::ui::smoke_overlay::is_smoke_active(), |b| b.child(
                gpui::canvas(|_, _, _| (), |bounds, _, _, _| {
                    crate::ui::smoke_overlay::record_bounds("ai-reply-copy-visible", bounds);
                }).absolute().size_full(),
            ))
            .invisible().group_hover(group, |s| s.visible())
            .tooltip_above(crate::ui::tooltip::tooltip(tr("复制", "Copy"))),
        )))
}

fn plan_card(at: usize, plan: agent::Plan, cx: &mut gpui::Context<AiChat>) -> impl IntoElement {
    use agent::PlanState;
    use crate::ui::ai_plan_card::{chat_plan_card, pill};
    let pending = plan.state == PlanState::Pending;
    let badge = match plan.state { PlanState::Pending => tr("编辑器待审阅", "Review in Editor"), PlanState::Applied => tr("已应用", "Applied"),
        PlanState::Partial => tr("部分写入失败", "Partially failed"), PlanState::Canceled => tr("已取消", "Canceled"), PlanState::Undone => tr("已撤回", "Undone"),
        PlanState::UndoConflict => tr("撤回冲突", "Undo conflict"), PlanState::Stale => tr("已失效", "Stale") };
    let open: Option<Box<dyn Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App)>> = if pending {
        let listener = cx.listener(move |this, event, window, cx| { let _ = (event, window); this.link_plan(at, 0, false, cx); });
        Some(Box::new(move |event, window, cx| listener(event, window, cx)))
    } else if matches!(plan.state, PlanState::Applied | PlanState::Partial) {
        let listener = cx.listener(move |this, event, window, cx| { let _ = (event, window); this.open_plan_page(at, 0, cx); });
        Some(Box::new(move |event, window, cx| listener(event, window, cx)))
    } else { None };
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
    let note: String = plan.summary.lines().next().unwrap_or("").chars().take(120).collect();
    chat_plan_card(format!("plan-op-{at}-0"), title, badge, note, pending, open, actions)
}
