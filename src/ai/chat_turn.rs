//! Built-in provider turn orchestration and bounded tool continuation.
use std::{sync::{Arc, atomic::AtomicBool, mpsc}, time::Duration};
use gpui::{Context, Window};
use super::{AiChat, Msg, agent};
use super::super::provider::{self, Event};
const MAX_STEPS: usize = 8;

impl AiChat {
    pub(in crate::editor::ai) fn run_turn(&mut self, step: usize, window: &mut Window, cx: &mut Context<Self>) {
        if step == 0 { super::super::chat_process::start_turn(self); }
        let ctx = self.agent_ctx(cx);
        let notebook = ctx.root.file_name().map(|s| s.to_string_lossy().into_owned()).unwrap_or_default();
        let live = self.note_context(cx);
        let system = format!("{}\n\n# 当前上下文\n\n- 当前笔记本：{}\n- 当前打开页面：{}\n- 当前内容范围：{}\n\n{}\n\n# 当前选区\n\n{}", agent::AGENTS,
            if notebook.is_empty() { "（无）" } else { &notebook },
            ctx.current.as_ref().map(|p| format!("[{p}] {p}（pageId: {p}）")).unwrap_or("（无当前打开页面）".into()),
            if self.selection_scope { "选区" } else { "当前笔记" }, live.markdown.unwrap_or_default(), live.selection.unwrap_or_default());
        let mut messages = vec![serde_json::json!({ "role": "system", "content": system })];
        if let Some(c) = self.conv() {
            for m in &c.messages {
                if matches!(m.role.as_str(), "activity" | "public_summary" | "turn_outcome") { continue; }
                messages.push(match m.role.as_str() {
                    "tool" => serde_json::json!({ "role": "tool", "tool_call_id": m.tool_call_id, "content": m.content }),
                    "assistant" if m.tool_calls.is_some() => serde_json::json!({ "role": "assistant", "content": m.content, "tool_calls": m.tool_calls }),
                    role => serde_json::json!({ "role": role, "content": format!("{}{}", m.content, m.context.as_deref().unwrap_or("")) }),
                });
            }
        }
        if step == MAX_STEPS {
            messages.push(serde_json::json!({"role":"system", "content":"本轮工具调用已达上限。请根据已有工具结果回答，明确未完成或未覆盖的范围，不要声称全部完成。"}));
        }
        let cancel = Arc::new(AtomicBool::new(false));
        self.serial += 1;
        let serial = self.serial;
        self.streaming = Some((serial, cancel.clone()));
        self.partial.clear();
        let rx = provider::stream_agent(messages, if step < MAX_STEPS { Some(agent::tools()) } else { None }, cancel);
        cx.spawn_in(window, async move |this, cx| {
            let mut calls = Vec::new();
            loop {
                cx.background_executor().timer(Duration::from_millis(40)).await;
                let mut finished = false;
                let alive = this.update_in(cx, |this, window, cx| {
                    if this.serial != serial { finished = true; return; }
                    let mut failed = false;
                    let mut changed = false;
                    loop {
                        let event = match rx.try_recv() {
                            Err(mpsc::TryRecvError::Empty) => break,
                            event => event,
                        };
                        changed = true;
                        match event {
                            Ok(Event::Thinking(_)) => {}
                            Ok(Event::Delta(t)) => this.partial.push_str(&t),
                            Ok(Event::ToolCalls(c)) => calls = c,
                            Ok(Event::Done) => { finished = true; break; }
                            Ok(Event::Failed { message, settings }) => { this.error = Some(provider::format_error(&message)); this.error_settings = settings; finished = true; failed = true; break; }
                            Err(mpsc::TryRecvError::Empty) => break,
                            Err(mpsc::TryRecvError::Disconnected) => { this.error = Some("AI backend disconnected".into()); finished = true; failed = true; break; }
                        }
                    }
                    if finished {
                        this.streaming = None;
                        if !calls.is_empty() && step >= MAX_STEPS {
                            this.error = Some(crate::theme::tr("已达到工具调用上限，请缩小范围后继续。", "Tool limit reached. Narrow the scope and continue.").into());
                            failed = true;
                        }
                        if failed { calls.clear(); }
                        let text = std::mem::take(&mut this.partial);
                        if calls.is_empty() {
                            if !text.trim().is_empty() { this.push("assistant", text); }
                        } else {
                            let roots = calls.iter().filter(|(_, name, _)| name == "readPage").filter_map(|(id, _, args)| {
                                let root = serde_json::from_str::<serde_json::Value>(args).ok().and_then(|value| value.get("notebookRoot").and_then(|root| root.as_str()).map(str::to_owned)).unwrap_or_else(|| this.agent_ctx(cx).root.to_string_lossy().into_owned());
                                (!root.is_empty()).then(|| (id.clone(), root))
                            }).collect();
                            let tc: Vec<serde_json::Value> = calls.iter().map(|(id, name, args)| serde_json::json!({ "id": id, "type": "function", "function": { "name": name, "arguments": args } })).collect();
                            this.push_msg(Msg { role: "assistant".into(), content: text, source_roots: Some(roots), tool_calls: Some(tc.into()), ..Default::default() });
                            for (id, name, args) in std::mem::take(&mut calls) {
                                let root = serde_json::from_str::<serde_json::Value>(&args).ok().and_then(|value| value.get("notebookRoot").and_then(|root| root.as_str()).map(str::to_owned));
                                let ctx = root.as_ref().map(|root| this.context_for_root(std::path::Path::new(root), cx)).unwrap_or_else(|| Ok(this.agent_ctx(cx)));
                                let (mut result, plan) = match ctx {
                                    Ok(ctx) => agent::exec(&name, &args, &ctx),
                                    Err(error) => (serde_json::json!({"error":error}), None),
                                };
                                let auto = plan.as_ref().is_some_and(|p| p.ops.iter().all(|o| matches!(o.kind, agent::OpKind::Create { .. })));
                                this.push_msg(Msg { role: "tool".into(), content: String::new(), tool_call_id: Some(id), plan, ..Default::default() });
                                if !auto && this.plan_at(this.conv().map_or(0, |c| c.messages.len() - 1)).is_some() {
                                    let at = this.conv().map_or(0, |c| c.messages.len() - 1); this.link_plan(at, 0, false, cx);
                                }
                                if auto {
                                    let at = this.conv().map(|c| c.messages.len() - 1).unwrap_or(0);
                                    this.apply_plan(at, cx);
                                    result = if this.plan_at(at).map(|p| p.state.clone()) == Some(agent::PlanState::Applied) {
                                        this.open_plan_page(at, 0, cx);
                                        serde_json::json!({ "status": "applied", "message": "已新建页面。" })
                                    } else { serde_json::json!({ "error": this.error.clone().unwrap_or_else(|| "新建失败：目标页面已变化或不可写。".into()) }) };
                                }
                                if let Some(m) = this.current.and_then(|cid| this.convs.iter_mut().find(|c| c.id == cid)).and_then(|c| c.messages.last_mut()) {
                                    m.content = result.to_string();
                                }
                            }
                            this.persist();
                            if step < MAX_STEPS { this.run_turn(step + 1, window, cx); }

                        }
                        if failed { crate::log::warn("AI chat turn failed; see the in-app error for details"); this.restore_failed_draft(window, cx); }
                        if failed { super::super::chat_process::record_outcome(this, "failed"); }
                        else if this.streaming.is_none() { super::super::chat_process::record_outcome(this, "completed"); }
                        if !failed && calls.is_empty() && this.streaming.is_none() { this.sent_draft = None; }
                    }
                    if changed { this.scroll.scroll_to_bottom(); cx.notify(); }
                });
                if alive.is_err() || finished { break; }
            }
        }).detach();
        self.scroll.scroll_to_bottom();
        cx.notify();
    }

}
