//! Rendering and batch-plan card UI for `AiChat`.
use gpui::{CursorStyle, Focusable, FontWeight, IntoElement, MouseButton, ParentElement, Render, SharedString, Styled, Window, div, prelude::*, px};
use gpui_component::input::Input;

use super::agent;
use super::chat::{AiChat, Msg, first_placeholder, follow_placeholder};
use super::backend_ui;
use crate::icons::{Icon, icon, stateful_icon};
use crate::theme::{Theme, is_en, tr};

fn glyph(i: Icon, size: f32, color: gpui::Hsla) -> impl IntoElement {
    icon(i, px(size), Theme::ocean().mono_icon_colors(color))
}

fn duo_glyph(i: Icon, size: f32, color: gpui::Hsla) -> impl IntoElement {
    icon(i, px(size), Theme::ocean().icon_colors(color))
}
impl Render for AiChat {
    fn render(&mut self, window: &mut Window, cx: &mut gpui::Context<Self>) -> impl IntoElement {
        if self.last_en != is_en() {
            self.last_en = is_en();
            let ph = if self.conv().is_some() { follow_placeholder() } else { first_placeholder() };
            self.input.update(cx, |i, cx| i.set_placeholder(ph, window, cx));
        }
        let theme = Theme::ocean();
        let note = self.agent_ctx(cx).current;
        let title = self.conv().map(|conversation| conversation.title.clone()).unwrap_or_else(|| note.as_ref().map(|note| format!("{} · {note}", tr("新会话", "New chat"))).unwrap_or_else(|| tr("新会话", "New chat").to_owned()));
        let header = div().h(px(52.)).flex_none().px(px(16.)).flex().items_center().gap(px(8.)).border_b_1().border_color(theme.hairline)
            .child(div().flex_1().min_w_0().child(crate::ui::ai_controls::action_chip("ai-session-switcher", None, title, false, self.history, cx.listener(|this, _, _, cx| {
                cx.stop_propagation(); backend_ui::close_menu(); this.history = !this.history; cx.notify();
            })).h(px(30.)).max_w(px(240.)).child(glyph(Icon::ChevronDown, 13., theme.text_secondary))))
            .child(crate::ui::ai_controls::action_chip("ai-layout-toggle", Some(if self.fullscreen { Icon::WindowRestore } else { Icon::WindowMax }), if self.fullscreen { tr("侧栏", "Sidebar") } else { tr("全屏", "Full screen") }, false, false,
                cx.listener(|this, _, window, cx| { cx.stop_propagation(); this.fullscreen = !this.fullscreen; window.refresh(); cx.notify(); })).h(px(28.)).px(px(10.)).bg(theme.shell).rounded(crate::ui::radius::CONTROL))
            .child(crate::ui::icon_button::header_action_button("ai-chat-new", Icon::AiPlus, false, tr("新建会话", "New chat"), cx.listener(|this, _, w, cx| {
                let backend = this.selected_backend(); this.new_conv(w, cx); this.select_backend(backend, w, cx);
            })))
            .child(crate::ui::icon_button::header_action_button("ai-chat-close", Icon::Close, false, tr("关闭 AI", "Close AI"), cx.listener(|this, _, window, cx| { this.open = false; this.fullscreen = false; backend_ui::close_menu(); window.refresh(); cx.notify(); })));
        let history = self.history.then(|| {
            let list = if self.convs.iter().all(|c| c.messages.is_empty()) {
                div().p(px(12.)).text_size(px(14.)).text_color(theme.text_secondary).child(tr("暂无历史会话", "No chat history"))
            } else {
                self.convs.iter().filter(|c| !c.messages.is_empty()).fold(div().flex().flex_col().p(px(4.)), |d, c| {
                    let (id, active) = (c.id, Some(c.id) == self.current);
                    let del_id = SharedString::from(format!("conv-del-{id}"));
                    d.child(div().id(SharedString::from(format!("conv-{id}"))).h(px(32.)).px(px(8.)).flex().items_center().gap(px(6.)).rounded(crate::ui::radius::CONTROL)
                        .when(active, |r| r.bg(theme.hover)).hover(move |s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                            this.stop(); this.current = Some(id); this.history = false; this.error = None; this.persist_active_draft(cx);
                            this.input.update(cx, |i, cx| i.set_placeholder(follow_placeholder(), w, cx));
                            cx.notify();
                        }))
                        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(14.)).child(c.title.clone()))
                        .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(backend_ui::backend_name(c.backend)))
                        .child(div().id(del_id.clone()).group(del_id.clone()).size(px(22.)).flex().items_center().justify_center().rounded(crate::ui::radius::MICRO_SM)
                            .hover(move |s| s.bg(theme.shell))
                            .tooltip(crate::theme::tooltip(tr("删除会话", "Delete chat")))
                            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                                cx.stop_propagation();
                                if this.current == Some(id) { this.stop(); this.current = None; }
                                this.convs.retain(|c| c.id != id); this.persist(); this.persist_active_draft(cx); cx.notify();
                            }))
                            .child(stateful_icon(del_id, Icon::Trash, px(13.), false, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text)))))
                })
            };
            div().id("ai-chat-history-list").mx(px(8.)).mt(px(6.)).max_h(px(260.)).overflow_y_scroll().rounded(crate::ui::radius::SURFACE).border_1()
                .border_color(theme.menu_border).bg(theme.menu).shadow_md().child(list)
        });
        let mut msgs: Vec<Msg> = self.conv().map(|c| c.messages.clone()).unwrap_or_default();
        if self.streaming.is_some() { msgs.push(Msg { role: "assistant".into(), content: if self.partial.is_empty() { tr("思考中…", "Thinking…").into() } else { self.partial.clone() }, ..Default::default() }); }
        let empty = msgs.is_empty();
        let body = msgs.into_iter().enumerate().fold(div().flex().flex_col().gap(px(16.)).px(px(16.)).py(px(20.)), |d, (i, m)| {
            let text = m.content.clone();
            if m.role == "user" {
                d.child(div().flex().justify_end().child(div().max_w(px(300.)).px(px(12.)).py(px(8.)).rounded(crate::ui::radius::SURFACE).bg(theme.hover)
                    .text_size(px(14.)).line_height(px(22.)).child(text)))
            } else if m.role == "tool" {
                match m.plan { Some(plan) => d.child(plan_card(i, plan, cx)), None => d }
            } else {
                let tools = m.tool_calls.as_ref().and_then(|t| t.as_array()).map(|a| a.iter()
                    .filter_map(|c| c.pointer("/function/name").and_then(|n| n.as_str()).map(tool_label)).collect::<Vec<_>>().join(" · "));
                let copy = text.clone();
                d.child(div().flex().flex_col().gap(px(4.))
                    .when(!text.trim().is_empty(), |d| d.child(super::md_view::render(&text, &format!("{i}"))))
                    .when_some(tools, |d, t| d.child(div().flex().items_center().gap(px(6.)).text_size(px(14.)).text_color(theme.text_secondary)
                        .child(glyph(Icon::Sparkles, 12., theme.text_secondary)).child(t)))
                    .when(m.tool_calls.is_none(), |d| d.child(div().id(SharedString::from(format!("copy-{i}"))).w_auto().text_size(px(14.)).text_color(theme.text_secondary)
                        .cursor(CursorStyle::PointingHand).hover(move |s| s.text_color(theme.text))
                        .on_mouse_down(MouseButton::Left, move |_, _, cx| cx.write_to_clipboard(gpui::ClipboardItem::new_string(copy.clone())))
                        .child(tr("复制", "Copy")))))
            }
        });
        let body = body.when(empty, |body| body.child(super::chat_empty::view(self, cx)));

        let error = self.error.clone().map(|e| div().mx(px(12.)).mb(px(8.)).p(px(10.)).flex().items_start().gap(px(8.)).rounded(crate::ui::radius::CONTROL)
            .bg(gpui::hsla(0., 0.75, 0.5, 0.08)).text_size(px(14.))
            .child(div().flex_1().flex().flex_col().gap(px(2.))
                .child(div().font_weight(FontWeight(600.)).text_color(gpui::rgb(0xb42318)).child(tr("本轮失败原因", "Request failed")))
                .child(div().text_color(theme.text).child(e))
                .when(self.error_settings, |d| d.child(crate::ui::ai_controls::action_chip(
                    "ai-chat-settings", None, tr("打开 AI 设置", "Open AI settings"), false, false,
                    |_, window, cx| window.dispatch_action(Box::new(super::OpenAiSettings), cx)))))
            .child(crate::ui::icon_button::header_action_button("ai-chat-error-close", Icon::Close, false, tr("关闭错误提示", "Dismiss error"), cx.listener(|this, _, _, cx| { this.error = None; cx.notify(); }))));

        let chip_list: Vec<(bool, String)> = self.refs.iter().map(|r| (true, r.clone())).chain(self.skills.iter().map(|s| (false, s.clone()))).collect();
        let chips = chip_list.into_iter().enumerate().fold(div().flex().flex_wrap().gap(px(4.)), |d, (i, (is_ref, name))| {
            let x_id = SharedString::from(format!("chip-x-{i}"));
            d.child(div().id(SharedString::from(format!("chip-{i}"))).flex().items_center().gap(px(4.)).px(px(6.)).py(px(2.)).rounded(crate::ui::radius::CONTROL)
                .bg(theme.hover).text_size(px(14.))
                .child(duo_glyph(if is_ref { Icon::Note } else { Icon::Sparkles }, 12., theme.accent))
                .child(if is_ref { name.clone() } else { format!("/{name}") })
                .child(div().id(x_id.clone()).group(x_id.clone()).cursor(CursorStyle::PointingHand)
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                        if is_ref { this.refs.retain(|r| r != &name); } else { this.skills.retain(|s| s != &name); } this.persist_active_draft(cx);
                        cx.notify();
                    }))
                    .child(stateful_icon(x_id, Icon::Close, px(10.), false, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text)))))
        });
        let token = self.picker_token(cx);
        let picks = token.as_deref().map(|token| self.picker_items(token, cx)).unwrap_or_default();
        let selected = self.pick_index.min(picks.len().saturating_sub(1));
        let picker_open = token.is_some();
        if picker_open { backend_ui::close_menu(); }
        let token = token.unwrap_or_default();
        let picker_height = (f32::from(window.viewport_size().height) - 260.).clamp(80., 200.);
        let picker = picker_open.then(|| {
            let list = if picks.is_empty() {
                div().p(px(10.)).text_size(px(14.)).text_color(theme.text_secondary)
                    .child(if token.starts_with('@') { tr("没有匹配的笔记", "No matching notes") } else { tr("没有匹配的指令或技能", "No matching commands or skills") })
                    .into_any_element()
            } else {
                let scroll = super::chat_composer::PICKER_SCROLL.with(Clone::clone);
                picks.into_iter().enumerate().fold(div().id("ai-reference-list").max_h(px(picker_height - 32.)).overflow_y_scroll().track_scroll(&scroll).p(px(4.)).flex().flex_col(), |list, (i, (pick, name, desc))| {
                    let row = crate::ui::dropdown::row_with(SharedString::from(format!("pick-{i}")), crate::ui::dropdown::RowSpec {
                        icon: Some(if name.starts_with('/') { Icon::Sparkles } else { Icon::Note }),
                        label: name.into(), hint: Some(desc.into()), ..Default::default()
                    }).h(px(36.)).when(i == selected, |row| row.bg(theme.selected).border_1().border_color(theme.accent))
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                        cx.stop_propagation(); this.accept_pick(pick.clone(), w, cx);
                    }));
                    list.child(row)
                }).into_any_element()
            };
            div().mx(px(16.)).mb(px(4.)).flex_none().max_h(px(picker_height)).rounded(crate::ui::radius::SURFACE).border_1().border_color(theme.menu_border).bg(theme.menu)
                .child(div().px(px(10.)).pt(px(6.)).text_size(px(14.)).text_color(theme.text_secondary).child(if token.starts_with('@') { tr("引用笔记", "Reference Note") } else { tr("指令与技能", "Commands & Skills") }))
                .child(list)
        });
        let busy = self.streaming.is_some();
        let action = if busy {
            crate::ui::ai_controls::action_chip("ai-chat-stop", None, tr("停止", "Stop"), false, false, cx.listener(|this, _, _, cx| { this.stop(); cx.notify(); })).h(px(32.)).border_1().border_color(theme.btn_border)
        } else {
            crate::ui::ai_controls::action_chip("ai-chat-send", None, tr("发送", "Send"), true, false, cx.listener(move |this, _, w, cx| { cx.stop_propagation(); this.send(w, cx); })).h(px(32.))
        };
        let approval = self.pending_approval.as_ref().map(|(_, title, detail)| {
            let entity = cx.entity().downgrade();
            backend_ui::approval_card(title.clone(), detail.clone(), move |response, _, cx| {
                let _ = entity.update(cx, |this, cx| this.respond_approval(response, cx));
            })
        });
        let draft_recovery = self.has_deferred_draft().then(|| {
            let has_current_draft = !self.input.read(cx).value().is_empty() || !self.refs.is_empty() || !self.skills.is_empty();
            div().px(px(16.)).pb(px(8.)).flex().flex_col().items_start().gap(px(6.)).text_size(px(14.))
                .child(div().text_color(theme.text_secondary).child(if has_current_draft {
                    tr("切换时会保存当前草稿。", "Your current draft will be saved when you switch.")
                } else {
                    tr("上一条未完成的草稿可恢复。", "The previous unfinished draft can be restored.")
                }))
                .child(crate::ui::settings_controls::action_button("ai-restore-draft", if has_current_draft { tr("切换到失败草稿", "Switch to failed draft") } else { tr("恢复草稿", "Restore draft") }, false, true)
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                        this.restore_deferred_draft(window, cx); cx.notify();
                    })))
        });
        let composer = div().p(px(10.)).flex().flex_col().gap(px(10.)).rounded(crate::ui::radius::SURFACE).border_1().border_color(if self.input.read(cx).focus_handle(cx).is_focused(window) { theme.accent } else { theme.btn_border }).bg(theme.paper)
            .when(!self.refs.is_empty() || !self.skills.is_empty(), |d| d.child(chips))
            .child(div().h(px(54.)).text_size(px(14.)).child(Input::new(&self.input).appearance(false).bordered(false).focus_bordered(false).h(px(54.))))
            .child(div().flex().items_center().gap(px(6.))
                .child(div().flex_1().min_w_0().flex().items_center().gap(px(6.))
                    .child(super::chat_deck::trigger(self, cx))
                    .child(crate::ui::ai_controls::action_chip("ai-reference-trigger", None, note.map(|note| format!("@{note}")).unwrap_or_else(|| "@".into()), false, false, cx.listener(|this, _, window, cx| {
                        backend_ui::close_menu();
                        let mut text = this.input.read(cx).value().to_string();
                        if !text.rsplit(char::is_whitespace).next().unwrap_or("").starts_with('@') {
                            if !text.is_empty() && !text.ends_with(char::is_whitespace) { text.push(' '); }
                            text.push('@');
                        }
                        this.picker_dismissed = false; this.pick_index = 0;
                        this.input.update(cx, |input, cx| { input.set_value(text, window, cx); input.focus(window, cx); });
                        cx.notify();
                    })).h(px(30.)).max_w(px(100.)).overflow_hidden().border_1().border_color(theme.btn_border).tooltip(crate::theme::tooltip(tr("引用笔记", "Reference note")))))
                .child(action));

        div().id("ai-chat-panel").when(self.fullscreen, |panel| panel.flex_1().w_full().min_w_0()).when(!self.fullscreen, |panel| panel.w(px(396.)).flex_none().border_l_1().border_color(theme.hairline)).h_full().flex().flex_col().relative().bg(theme.paper)
            .text_color(theme.text)
            .on_mouse_down(MouseButton::Left, cx.listener(|_, _, _, cx| { if backend_ui::close_menu() { cx.notify(); } }))
            .capture_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, _, cx| {
                if event.keystroke.key == "escape" && this.picker_token(cx).is_some() {
                    this.picker_dismissed = true; cx.stop_propagation(); cx.notify();
                }
            }))
            .capture_action(cx.listener(|this, _: &gpui_component::input::MoveUp, _, cx| {
                if this.move_pick(-1, cx) { cx.stop_propagation(); }
            }))
            .capture_action(cx.listener(|this, _: &gpui_component::input::MoveDown, _, cx| {
                if this.move_pick(1, cx) { cx.stop_propagation(); }
            }))
            .capture_action(cx.listener(|this, _: &gpui_component::input::Escape, window, cx| {
                cx.stop_propagation();
                if this.picker_token(cx).is_some() { this.picker_dismissed = true; }
                else if backend_ui::close_menu() || super::dsh_ui::close() {} else if this.history { this.history = false; } else { this.open = false; this.fullscreen = false; window.refresh(); }
                cx.notify();
            }))
            .child(header)
            .child(div().id("ai-chat-scroll").flex_1().min_h_0().overflow_y_scroll().track_scroll(&self.scroll)
                .on_scroll(|_, _, cx| crate::dropdown::dismiss_all(cx))
                .child(div().w_full().when(self.fullscreen, |inner| inner.max_w(px(720.)).mx_auto()).child(body)
                    .child(div().px(px(16.)).pb(px(16.)).children(approval)).children(error)))
            .children(draft_recovery)
            .children(picker)
            .child(div().flex_none().relative().px(px(16.)).pt(px(if picker_open { 0. } else { 12. })).pb(px(16.)).border_t_1().border_color(theme.hairline)
                .child(div().w_full().when(self.fullscreen, |inner| inner.max_w(px(720.)).mx_auto()).relative().child(composer)
                    .children(super::chat_deck::view(self, cx).map(|deck| gpui::deferred(div().absolute().bottom(px(112.)).left_0().child(deck)).with_priority(20)))))
            .children(history.map(|h| div().absolute().top(px(52.)).left_0().right_0().child(h)))
    }
}

fn tool_label(name: &str) -> &'static str {
    match name {
        "loadSkill" => tr("加载技能", "Load skill"),
        "listPages" => tr("列出页面", "List pages"),
        "searchNotes" => tr("搜索笔记", "Search notes"),
        "readPage" => tr("读取页面", "Read page"),
        "executeBatchPlan" => tr("生成变更计划", "Create change plan"),
        _ => tr("调用工具", "Call tool"),
    }
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
