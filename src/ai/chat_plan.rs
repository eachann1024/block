use gpui::Context;
use super::{AiChat, agent};

impl AiChat {
    pub(in crate::editor::ai) fn plan_at(&self, at: usize) -> Option<&agent::Plan> {
        self.conv().and_then(|conversation| conversation.messages.get(at)).and_then(|message| message.plan.as_ref())
    }

    fn set_plan(&mut self, at: usize, plan: agent::Plan) {
        if let Some(message) = self.current.and_then(|id| self.convs.iter_mut().find(|conversation| conversation.id == id))
            .and_then(|conversation| conversation.messages.get_mut(at)) { message.plan = Some(plan); }
        self.persist();
    }

    fn write_page(&self, ctx: &agent::Ctx, page: &str, text: &str, expected: Option<&str>, cx: &mut Context<Self>) -> Result<String, String> {
        let path = ctx.checked_path(page)?;
        if self.editor.as_ref().and_then(|editor| editor.upgrade()).is_some_and(|editor| editor.read(cx).has_parked_session(&path)) {
            return Err("Note is open in another editor tab; open it before applying changes".into());
        }
        if ctx.current.as_deref() == Some(page) {
            if let Some(editor) = self.editor.as_ref().and_then(|editor| editor.upgrade()) {
                return Ok(editor.update(cx, |editor, cx| editor.ai_write_markdown(text, cx)));
            }
            return Err("Current editor is unavailable".into());
        }
        if let Some(directory) = path.parent() { std::fs::create_dir_all(directory).map_err(|error| error.to_string())?; }
        let path = ctx.checked_path(page)?;
        if ctx.read_checked(page)?.as_deref() != expected { return Err("Note changed before write".into()); }
        if expected.is_none() {
            use std::io::Write;
            let mut file = std::fs::OpenOptions::new().write(true).create_new(true).open(&path).map_err(|error| error.to_string())?;
            if let Err(error) = file.write_all(text.as_bytes()) {
                drop(file); let _ = std::fs::remove_file(&path); return Err(error.to_string());
            }
            crate::search::window::notify_saved_note(path, cx);
        } else { write_existing(ctx, page, &path, text, expected)?; }
        Ok(text.to_owned())
    }

    pub(in crate::editor::ai) fn apply_plan(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(mut plan) = self.plan_at(at).cloned() else { return };
        if plan.state != agent::PlanState::Pending { return; }
        let ctx = match plan.root.as_ref().and_then(|root| self.context_for_root(root, cx).ok()) {
            Some(ctx) => ctx, None => { plan.state = agent::PlanState::Stale; self.set_plan(at, plan); super::super::plan_link::clear_plan(self.current, at, cx); cx.notify(); return; }
        };
        let stale_content = plan.expected.as_ref().is_none_or(|expected| {
            plan.ops.iter().any(|op| !expected.iter().any(|(page, _)| page == &op.page)) ||
            expected.iter().any(|(page, original)| super::super::plan_link::plan_source(&ctx, page, original.as_deref()).is_err())
        });
        if stale_content {
            plan.state = agent::PlanState::Stale; self.set_plan(at, plan); super::super::plan_link::clear_plan(self.current, at, cx); cx.notify(); return;
        }
        let mut work: Vec<(String, Option<String>, String)> = Vec::new();
        for op in &plan.ops {
            let current = match work.iter().find(|entry| entry.0 == op.page) {
                Some(entry) => Some(entry.2.clone()),
                None => match super::super::plan_link::plan_source(&ctx, &op.page, plan.expected.as_ref().and_then(|items| items.iter().find(|(page, _)| page == &op.page)).and_then(|(_, original)| original.as_deref())) {
                    Ok(content) => content,
                    Err(error) => { self.error = Some(error); cx.notify(); return; }
                },
            };
            let Some(next) = agent::apply_op(op, current.as_deref()) else {
                plan.state = agent::PlanState::Stale; self.set_plan(at, plan); super::super::plan_link::clear_plan(self.current, at, cx); cx.notify(); return;
            };
            match work.iter_mut().find(|entry| entry.0 == op.page) {
                Some(entry) => entry.2 = next, None => work.push((op.page.clone(), current, next)),
            }
        }
        for (page, before, after) in &mut work {
            if let Some(conv) = self.current {
                match super::super::plan_link::selection::selected(conv, at, page, before.as_deref().unwrap_or(""), after) {
                    Ok(selected) => *after = selected, Err(error) => { self.error = Some(error); cx.notify(); return; }
                }
            }
        }
        if work.iter().any(|(_, before, after)| !agent::markdown_preflight::check(before.as_deref(), after)) {
            self.error = Some(crate::theme::tr("所选修改包含新增的不支持 Markdown 结构，请调整后重试。", "The selected changes introduce unsupported Markdown. Please revise them.").into());
            cx.notify(); return;
        }
        plan.receipts.clear();
        for (page, before, after) in work {
            if before.as_deref() == Some(after.as_str()) || (before.is_none() && after.is_empty()) { continue; }
            match self.write_page(&ctx, &page, &after, before.as_deref(), cx) {
                Ok(after) => plan.receipts.push(agent::Receipt { created: before.is_none(), before: before.unwrap_or_default(), after, page }),
                Err(error) => {
                    self.error = Some(format!("{}: {error}", crate::theme::tr("笔记写入失败", "Failed to write note")));
                    plan.state = if plan.receipts.is_empty() { agent::PlanState::Pending } else { agent::PlanState::Partial };
                    let partial = plan.state == agent::PlanState::Partial;
                    self.set_plan(at, plan); if partial { super::super::plan_link::clear_plan(self.current, at, cx); } cx.notify(); return;
                }
            }
        }
        if plan.receipts.is_empty() { self.error = Some("未选择任何修改".into()); cx.notify(); return; }
        let created = plan.receipts.iter().any(|receipt| receipt.created);
        plan.state = agent::PlanState::Applied; self.set_plan(at, plan); super::super::plan_link::clear_plan(self.current, at, cx);
        if created { self.open_plan_page(at, 0, cx); }
        cx.notify();
    }

    pub(in crate::editor::ai) fn undo_plan(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(mut plan) = self.plan_at(at).cloned() else { return };
        if !matches!(plan.state, agent::PlanState::Applied | agent::PlanState::Partial) { return; }
        let ctx = match plan.root.as_ref().and_then(|root| self.context_for_root(root, cx).ok()) {
            Some(ctx) => ctx, None => { plan.state = agent::PlanState::UndoConflict; self.set_plan(at, plan); cx.notify(); return; }
        };
        if plan.receipts.iter().any(|receipt| receipt.created && ctx.current.as_deref() == Some(receipt.page.as_str())) {
            self.error = Some(crate::theme::tr("撤回前请先切换到其他笔记", "Switch to another note before undoing a creation").into());
            cx.notify(); return;
        }
        if plan.receipts.iter().any(|receipt| {
            ctx.read_checked(&receipt.page).map_or(true, |current| current.as_deref() != Some(&receipt.after))
        }) {
            plan.state = agent::PlanState::UndoConflict;
        } else {
            while let Some(receipt) = plan.receipts.last() {
                let result = if receipt.created && ctx.current.as_deref() != Some(receipt.page.as_str()) {
                    ctx.checked_path(&receipt.page).and_then(|path| {
                        if self.editor.as_ref().and_then(|editor| editor.upgrade()).is_some_and(|editor| editor.read(cx).has_parked_session(&path)) {
                            return Err("Note is open in another editor tab; open it before undo".into());
                        }
                        if ctx.read_checked(&receipt.page)?.as_deref() != Some(&receipt.after) { return Err("Note changed before undo".into()); }
                        std::fs::remove_file(path).map_err(|error| error.to_string())
                    })
                } else { self.write_page(&ctx, &receipt.page, &receipt.before, Some(&receipt.after), cx).map(|_| ()) };
                if let Err(error) = result {
                    self.error = Some(format!("{}: {error}", crate::theme::tr("撤回失败", "Undo failed")));
                    self.set_plan(at, plan); cx.notify(); return;
                }
                plan.receipts.pop();
            }
            plan.state = agent::PlanState::Undone;
        }
        self.set_plan(at, plan); super::super::plan_link::clear_plan(self.current, at, cx); cx.notify();
    }

    pub(in crate::editor::ai) fn set_plan_state(&mut self, at: usize, state: agent::PlanState, cx: &mut Context<Self>) {
        if let Some(mut plan) = self.plan_at(at).cloned() { plan.state = state; self.set_plan(at, plan); }
        super::super::plan_link::clear_plan(self.current, at, cx); cx.notify();
    }

    pub(in crate::editor::ai) fn reject_plan_op(&mut self, at: usize, index: usize, cx: &mut Context<Self>) {
        let Some(mut plan) = self.plan_at(at).cloned().filter(|plan| plan.state == agent::PlanState::Pending) else { return };
        if index >= plan.ops.len() { return; }
        let page = plan.ops.remove(index).page;
        if let Some(expected) = &mut plan.expected { expected.retain(|(page, _)| plan.ops.iter().any(|op| &op.page == page)); }
        if plan.ops.is_empty() { plan.state = agent::PlanState::Canceled; }
        let canceled = plan.state == agent::PlanState::Canceled;
        self.set_plan(at, plan);
        if canceled { super::super::plan_link::clear_plan(self.current, at, cx); }
        else { super::super::plan_link::clear_plan_page(self.current, at, &page, cx); self.link_plan(at, 0, cx); }
        cx.notify();
    }

}

fn write_existing(ctx: &agent::Ctx, page: &str, path: &std::path::Path, text: &str, expected: Option<&str>) -> Result<(), String> {
    use std::{io::Write, sync::atomic::{AtomicU64, Ordering}};
    static NEXT_TEMP: AtomicU64 = AtomicU64::new(0);
    let name = format!(".block-ai-{}-{}", std::process::id(), NEXT_TEMP.fetch_add(1, Ordering::Relaxed));
    let temporary = path.with_file_name(name);
    let mut created = false;
    let result = (|| {
        let permissions = std::fs::metadata(path).map_err(|error| error.to_string())?.permissions();
        let mut file = std::fs::OpenOptions::new().write(true).create_new(true).open(&temporary).map_err(|error| error.to_string())?;
        created = true;
        file.set_permissions(permissions).map_err(|error| error.to_string())?;
        file.write_all(text.as_bytes()).map_err(|error| error.to_string())?;
        drop(file);
        if ctx.checked_path(page)? != path || ctx.read_checked(page)?.as_deref() != expected { return Err("Note changed before write".into()); }
        std::fs::rename(&temporary, path).map_err(|error| error.to_string())
    })();
    if result.is_err() && created { let _ = std::fs::remove_file(&temporary); }
    result
}
