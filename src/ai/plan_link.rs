use std::{cell::RefCell, ops::Range, path::PathBuf, time::Duration};
use gpui::{App, Context, Entity, IntoElement, WeakEntity, div, prelude::*, px};
use crate::editor::{markdown, model::Block, view::Editor};
use super::{agent, chat::AiChat};

#[derive(Clone)]
pub(crate) struct ActivePlanLink {
    pub conv_id: u64, pub msg_at: usize, pub op_idx: usize, pub total_ops: usize,
    pub path: PathBuf, pub page_label: String, pub summary: String,
    pub block_range: Range<usize>, pub before_blocks: Vec<Block>, pub after_blocks: Vec<Block>,
    pub diff_mode: bool, pub chat: WeakEntity<AiChat>, pub editor: WeakEntity<Editor>,
}

thread_local! { static ACTIVE: RefCell<Option<ActivePlanLink>> = const { RefCell::new(None) }; }

pub(crate) fn current() -> Option<ActivePlanLink> { ACTIVE.with(|active| active.borrow().clone()) }

pub(crate) fn clear(cx: &mut App) {
    let previous = ACTIVE.with(|active| active.borrow_mut().take());
    if let Some(link) = previous {
        if let Some(editor) = link.editor.upgrade() { editor.update(cx, |editor, cx| {
            let end = link.block_range.end.min(editor.list.item_count());
            if link.block_range.start < end { editor.list.remeasure_items(link.block_range.start..end); }
            cx.notify();
        }); }
    }
}

pub(crate) fn clear_plan(conv_id: Option<u64>, at: usize, cx: &mut App) {
    if current().is_some_and(|link| Some(link.conv_id) == conv_id && link.msg_at == at) { clear(cx); }
}

fn equal(left: &Block, right: &Block) -> bool {
    left.kind == right.kind && left.text == right.text && left.spans == right.spans
        && left.checked == right.checked && left.meta == right.meta
}

pub(in crate::editor::ai) fn plan_source(context: &agent::Ctx, page: &str, expected: Option<&str>) -> Result<Option<String>, String> {
    let current = context.read_checked(page)?;
    if current.as_deref() == expected { return Ok(current); }
    if context.current.as_deref() == Some(page) {
        if let (Some(current), Some(expected)) = (current.as_deref(), expected) {
            let canonical = |text: &str| markdown::serialize(&markdown::parse(text));
            if canonical(current) == canonical(expected) { return Ok(Some(expected.to_owned())); }
        }
    }
    Err("Note changed; regenerate the plan".into())
}

impl AiChat {
    pub(in crate::editor::ai) fn link_plan(&mut self, at: usize, op_idx: usize, diff: bool, cx: &mut Context<Self>) {
        let Some(plan) = self.plan_at(at).cloned().filter(|plan| plan.state == agent::PlanState::Pending) else { return };
        let Some(op) = plan.ops.get(op_idx) else { return };
        let Some(conv_id) = self.current else { return };
        let Some(editor) = self.editor.as_ref().and_then(|editor| editor.upgrade()) else { return };
        let result = (|| -> Result<_, String> {
            let root = plan.root.as_ref().ok_or("Plan has no notebook root")?;
            let context = self.context_for_root(root, cx)?;
            context.checked_path(&op.page)?;
            let path = context.path(&op.page);
            let expected = plan.expected.as_ref().and_then(|items| items.iter().find(|(page, _)| page == &op.page)).and_then(|(_, value)| value.as_deref());
            let before = plan_source(&context, &op.page, expected)?.ok_or("Note does not exist")?;
            let after = agent::apply_op(op, Some(&before)).ok_or("Change no longer matches the note")?;
            let before_doc = markdown::parse(&before); let after_doc = markdown::parse(&after);
            let before_blocks: Vec<_> = before_doc.blocks.iter().cloned().collect();
            let after_blocks: Vec<_> = after_doc.blocks.iter().cloned().collect();
            let mut prefix = before_blocks.iter().zip(&after_blocks).take_while(|(left, right)| equal(left, right)).count();
            let suffix = before_blocks[prefix..].iter().rev().zip(after_blocks[prefix..].iter().rev()).take_while(|(left, right)| equal(left, right)).count();
            let mut before_end = before_blocks.len() - suffix;
            let mut after_end = after_blocks.len() - suffix;
            if prefix == before_end {
                if prefix > 0 { prefix -= 1; }
                else if suffix > 0 { before_end += 1; after_end += 1; }
            }
            let start = prefix; let end = before_end;
            Ok(ActivePlanLink { conv_id, msg_at: at, op_idx, total_ops: plan.ops.len(), path,
                page_label: format!("{}.md", op.page), summary: plan.summary.clone(), block_range: start..end,
                before_blocks: before_blocks[prefix..before_end].to_vec(), after_blocks: after_blocks[prefix..after_end].to_vec(),
                diff_mode: diff, chat: cx.entity().downgrade(), editor: editor.downgrade() })
        })();
        let link = match result { Ok(link) => link, Err(error) => { self.error = Some(error); cx.notify(); return; } };
        let path = link.path.clone(); let index = link.block_range.start;
        clear(cx);
        ACTIVE.with(|active| *active.borrow_mut() = Some(link));
        if editor.read(cx).current_path() != Some(path.as_path()) {
            if let Some((_, workspace)) = crate::search::window::main_window(cx) {
                workspace.update(cx, |workspace, cx| workspace.open_file_with_mode(path.clone(), false, true, cx));
            } else { editor.update(cx, |editor, cx| editor.open(path.clone(), cx)); }
        }
        reveal_when_ready(editor, path, index, cx);
        cx.notify();
    }
}

fn reveal_when_ready(editor: Entity<Editor>, path: PathBuf, index: usize, cx: &mut App) {
    cx.spawn(async move |cx| {
        for _ in 0..40 {
            cx.background_executor().timer(Duration::from_millis(50)).await;
            let ready = cx.update(|cx| editor.update(cx, |editor, cx| {
                if editor.current_path() != Some(path.as_path()) || editor.is_loading() { return false; }
                let index = index.min(editor.doc.blocks.len().saturating_sub(1));
                while let Some(heading) = editor.enclosing_folded_heading(index) {
                    let id = editor.doc.blocks[heading].id; editor.folded.remove(&id);
                }
                if editor.list.item_count() != editor.doc.blocks.len() { editor.list.reset(editor.doc.blocks.len()); }
                editor.list.remeasure_items(index..index + 1);
                crate::ui::scrollbar::reveal_item(&editor.list, index, px(80.));
                cx.notify(); true
            }));
            if ready { break; }
        }
    }).detach();
}

pub(crate) fn set_mode(diff: bool, cx: &mut App) {
    let link = ACTIVE.with(|active| { let mut active = active.borrow_mut(); let link = active.as_mut()?; link.diff_mode = diff; Some(link.clone()) });
    if let Some(link) = link {
        if let Some(chat) = link.chat.upgrade() {
            if let Some(editor) = chat.read(cx).editor.as_ref().and_then(|editor| editor.upgrade()) { editor.update(cx, |editor, cx| {
                let end = link.block_range.end.min(editor.list.item_count());
                if link.block_range.start < end { editor.list.remeasure_items(link.block_range.start..end); }
                cx.notify();
            }); }
            chat.update(cx, |_, cx| cx.notify());
        }
    }
}

pub(crate) fn render_linked_row(editor: &Editor, index: usize, _cx: &mut Context<Editor>) -> Option<gpui::AnyElement> {
    let link = current()?;
    if editor.path.as_ref() != Some(&link.path) || !link.block_range.contains(&index) { return None; }
    if link.block_range.end > editor.doc.blocks.len() || !link.before_blocks.iter().zip(link.block_range.clone())
        .all(|(before, at)| equal(before, &editor.doc.blocks[at])) { return None; }
    if index != link.block_range.start { return Some(div().id(("plan-merged-row", index)).into_any_element()); }
    Some(div().pl(px(crate::editor::chrome::HANDLE_WIDTH + crate::editor::chrome::ROW_GAP)).pr(px(crate::editor::chrome::RIGHT_INSET))
        .child(crate::ui::ai_plan_card::editor_plan_review_card(link)).into_any_element())
}

pub(crate) fn decide(link: ActivePlanLink, accept: bool, cx: &mut App) {
    let Some(chat) = link.chat.upgrade() else { clear(cx); return };
    chat.update(cx, |chat, cx| {
        if chat.current != Some(link.conv_id) { return; }
        if accept { chat.apply_plan(link.msg_at, cx); } else { chat.reject_plan_op(link.msg_at, link.op_idx, cx); }
    });
}

pub(crate) fn prepare_plan_link_smoke(root: PathBuf, window: &mut gpui::Window, cx: &mut App) -> Result<(), String> {
    if !crate::smoke::is_requested() { return Err("Plan fixture requires smoke mode".into()); }
    let (_, workspace) = crate::search::window::main_window(cx).ok_or("Smoke workspace missing")?;
    let editor = workspace.read(cx).editor.downgrade();
    let old = "| 日期 | 地点 |\n| --- | --- |\n| 1&#47;1 | 潮州&#58;老城 |\n| 1&#47;2 | 汕头 |";
    let new = "## 总路线\n\n| 日期 | 地点 |\n| --- | --- |\n| 1&#47;1 | 潮州&#58;老城 |\n| 1&#47;2 | 汕头&#58;小公园 |\n| 1&#47;3 | <span style=\"background-color:yellow\"><strong>头新溪谢肥记&#40;君&#41;</strong></span> |\n| 1&#47;4 | 南澳 |\n| 1&#47;5 | 返程 |";
    let args = serde_json::json!({"title":"行程", "summary":"更新总路线", "operations":[{"type":"search_replace","pageId":"潮州汕头行程","oldString":old,"newString":new}]}).to_string();
    super::chat::chat(window, cx).update(cx, |chat, cx| {
        chat.new_conv(window, cx); chat.open = true; chat.editor = Some(editor);
        chat.push_msg(super::chat::Msg { role: "user".into(), content: "更新行程".into(), ..Default::default() });
        let context = chat.context_for_root(&root, cx)?;
        let (result, plan) = agent::exec("executeBatchPlan", &args, &context);
        let plan = plan.ok_or_else(|| format!("Plan fixture failed: {result}"))?;
        chat.push_msg(super::chat::Msg { role: "tool".into(), content: result.to_string(), plan: Some(plan), ..Default::default() });
        chat.link_plan(1, 0, false, cx); cx.notify(); Ok(())
    })
}

pub(crate) fn smoke_applied(cx: &App) -> bool {
    super::chat::panel(cx).and_then(|chat| chat.read(cx).plan_at(1)).is_some_and(|plan| plan.state == agent::PlanState::Applied)
}
