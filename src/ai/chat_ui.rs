//! Rendering and batch-plan card UI for `AiChat`.
use gpui::{CursorStyle, FontWeight, IntoElement, MouseButton, ParentElement, Render, SharedString, Styled, Window, div, prelude::*, px, transparent_black};
use gpui_component::input::Input;

use super::agent;
use super::chat::{AiChat, FOLLOW_PLACEHOLDER, Msg, Pick};
use super::provider;
use crate::icons::{Icon, IconColors, icon};
use crate::theme::Theme;

fn glyph(i: Icon, size: f32, color: gpui::Hsla) -> impl IntoElement {
    icon(i, px(size), IconColors { line: color, fill: transparent_black(), accent: color })
}

fn icon_btn(id: impl Into<SharedString>, glyph_icon: Icon, tip: &'static str) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id(id.into()).size(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(6.))
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
        .tooltip(move |w, cx| gpui_component::tooltip::Tooltip::new(tip).build(w, cx))
        .child(glyph(glyph_icon, 16., theme.text_secondary))
}

impl Render for AiChat {
    fn render(&mut self, _: &mut Window, cx: &mut gpui::Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let model = provider::config().model;
        let header = div().h(px(44.)).flex_none().px(px(12.)).flex().items_center().gap(px(4.)).border_b_1().border_color(theme.hairline)
            .child(glyph(Icon::Sparkles, 16., theme.accent))
            .child(div().flex_1().pl(px(4.)).text_size(px(14.)).font_weight(FontWeight(600.)).child("AI 助手"))
            .child(icon_btn("ai-chat-new", Icon::AiPlus, "新建会话").on_mouse_down(MouseButton::Left, cx.listener(|this, _, w, cx| this.new_conv(w, cx))))
            .child(icon_btn("ai-chat-history", Icon::History, "历史会话").on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.history = !this.history; cx.notify(); })))
            .child(icon_btn("ai-chat-close", Icon::Close, "关闭 AI").on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.open = false; cx.notify(); })));

        let history = self.history.then(|| {
            let list = if self.convs.iter().all(|c| c.messages.is_empty()) {
                div().p(px(12.)).text_size(px(13.)).text_color(theme.text_secondary).child("暂无历史会话")
            } else {
                self.convs.iter().filter(|c| !c.messages.is_empty()).fold(div().flex().flex_col().p(px(4.)), |d, c| {
                    let (id, active) = (c.id, Some(c.id) == self.current);
                    d.child(div().id(SharedString::from(format!("conv-{id}"))).h(px(32.)).px(px(8.)).flex().items_center().gap(px(6.)).rounded(px(6.))
                        .when(active, |r| r.bg(theme.hover)).hover(move |s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                            this.stop(); this.current = Some(id); this.history = false; this.error = None;
                            this.input.update(cx, |i, cx| i.set_placeholder(FOLLOW_PLACEHOLDER, w, cx));
                            cx.notify();
                        }))
                        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(13.)).child(c.title.clone()))
                        .child(div().id(SharedString::from(format!("conv-del-{id}"))).size(px(22.)).flex().items_center().justify_center().rounded(px(4.))
                            .hover(move |s| s.bg(theme.shell))
                            .tooltip(|w, cx| gpui_component::tooltip::Tooltip::new("删除会话").build(w, cx))
                            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                                cx.stop_propagation();
                                if this.current == Some(id) { this.stop(); this.current = None; }
                                this.convs.retain(|c| c.id != id); this.persist(); cx.notify();
                            }))
                            .child(glyph(Icon::Trash, 13., theme.text_secondary))))
                })
            };
            div().id("ai-chat-history-list").mx(px(8.)).mt(px(6.)).max_h(px(260.)).overflow_y_scroll().rounded(px(8.)).border_1()
                .border_color(theme.menu_border).bg(theme.menu).shadow_md().child(list)
        });

        let mut msgs: Vec<Msg> = self.conv().map(|c| c.messages.clone()).unwrap_or_default();
        if self.streaming.is_some() { msgs.push(Msg { role: "assistant".into(), content: if self.partial.is_empty() { "思考中…".into() } else { self.partial.clone() }, ..Default::default() }); }
        let empty = msgs.is_empty();
        let body = msgs.into_iter().enumerate().fold(div().flex().flex_col().gap(px(14.)).p(px(14.)), |d, (i, m)| {
            let text = m.content.clone();
            if m.role == "user" {
                d.child(div().flex().justify_end().child(div().max_w(px(300.)).px(px(12.)).py(px(8.)).rounded(px(12.)).bg(theme.hover)
                    .text_size(px(14.)).line_height(px(22.)).child(text)))
            } else if m.role == "tool" {
                match m.plan { Some(plan) => d.child(plan_card(i, plan, cx)), None => d }
            } else {
                let tools = m.tool_calls.as_ref().and_then(|t| t.as_array()).map(|a| a.iter()
                    .filter_map(|c| c.pointer("/function/name").and_then(|n| n.as_str()).map(tool_label)).collect::<Vec<_>>().join(" · "));
                let copy = text.clone();
                d.child(div().flex().flex_col().gap(px(4.))
                    .when(!text.trim().is_empty(), |d| d.child(super::md_view::render(&text, &format!("{i}"))))
                    .when_some(tools, |d, t| d.child(div().flex().items_center().gap(px(6.)).text_size(px(12.)).text_color(theme.text_secondary)
                        .child(glyph(Icon::Sparkles, 12., theme.text_secondary)).child(t)))
                    .when(m.tool_calls.is_none(), |d| d.child(div().id(SharedString::from(format!("copy-{i}"))).w_auto().text_size(px(12.)).text_color(theme.text_secondary)
                        .cursor(CursorStyle::PointingHand).hover(move |s| s.text_color(theme.text))
                        .on_mouse_down(MouseButton::Left, move |_, _, cx| cx.write_to_clipboard(gpui::ClipboardItem::new_string(copy.clone())))
                        .child("复制"))))
            }
        });
        let body = if empty { body.child(div().pt(px(40.)).flex().flex_col().items_center().gap(px(8.)).text_color(theme.text_secondary)
            .child(glyph(Icon::Sparkles, 28., theme.accent)).child(div().text_size(px(14.)).child("有什么可以帮你？"))) } else { body };

        let error = self.error.clone().map(|e| div().mx(px(12.)).mb(px(8.)).p(px(10.)).flex().items_start().gap(px(8.)).rounded(px(8.))
            .bg(gpui::hsla(0., 0.75, 0.5, 0.08)).text_size(px(12.))
            .child(div().flex_1().flex().flex_col().gap(px(2.))
                .child(div().font_weight(FontWeight(600.)).text_color(gpui::rgb(0xb42318)).child("本轮失败原因"))
                .child(div().text_color(theme.text).child(e)))
            .child(icon_btn("ai-chat-error-close", Icon::Close, "关闭错误提示").on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.error = None; cx.notify(); }))));

        let chip_list: Vec<(bool, String)> = self.refs.iter().map(|r| (true, r.clone())).chain(self.skills.iter().map(|s| (false, s.clone()))).collect();
        let chips = chip_list.into_iter().enumerate().fold(div().flex().flex_wrap().gap(px(4.)), |d, (i, (is_ref, name))| {
            d.child(div().id(SharedString::from(format!("chip-{i}"))).flex().items_center().gap(px(4.)).px(px(6.)).py(px(2.)).rounded(px(6.))
                .bg(theme.hover).text_size(px(12.))
                .child(glyph(if is_ref { Icon::Note } else { Icon::Sparkles }, 12., theme.accent))
                .child(if is_ref { name.clone() } else { format!("/{name}") })
                .child(div().id(SharedString::from(format!("chip-x-{i}"))).cursor(CursorStyle::PointingHand)
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                        if is_ref { this.refs.retain(|r| r != &name); } else { this.skills.retain(|s| s != &name); }
                        cx.notify();
                    }))
                    .child(glyph(Icon::Close, 10., theme.text_secondary))))
        });
        let value = self.input.read(cx).value().to_string();
        let token = value.rsplit(char::is_whitespace).next().unwrap_or("").to_owned();
        let picks: Vec<(Pick, String, String)> = if let Some(q) = token.strip_prefix('@') {
            let q = q.to_lowercase();
            self.agent_ctx(cx).pages().into_iter().filter(|p| p.to_lowercase().contains(&q) && !self.refs.contains(p))
                .take(8).map(|p| (Pick::Page(p.clone()), p, "笔记".into())).collect()
        } else if let Some(q) = token.strip_prefix('/').filter(|_| value.trim_start().starts_with('/') || value.contains(" /")) {
            let q = q.to_lowercase();
            let mut v: Vec<(Pick, String, String)> = vec![(Pick::NewConv, "/新会话".into(), "开启空白对话，当前会话会留在历史里".into())];
            v.extend(self.agent_ctx(cx).local_skills().into_iter().map(|(n, d, _)| (Pick::Skill(n.clone()), format!("/{n}"), d)));
            v.into_iter().filter(|(_, n, d)| q.is_empty() || n.to_lowercase().contains(&q) || d.to_lowercase().contains(&q) || (q == "new" && n == "/新会话")).collect()
        } else { Vec::new() };
        let picker_open = token.starts_with('@') || (token.starts_with('/') && (value.trim_start().starts_with('/') || value.contains(" /")));
        let picker = picker_open.then(|| {
            let list = if picks.is_empty() {
                div().p(px(10.)).text_size(px(12.)).text_color(theme.text_secondary)
                    .child(if token.starts_with('@') { "没有匹配的笔记" } else { "没有匹配的指令或 Skill" })
            } else {
                picks.into_iter().enumerate().fold(div().flex().flex_col().p(px(4.)), |d, (i, (pick, name, desc))| {
                    let tlen = token.len();
                    d.child(div().id(SharedString::from(format!("pick-{i}"))).px(px(8.)).py(px(6.)).flex().items_center().gap(px(8.)).rounded(px(6.))
                        .hover(move |s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                            let v = this.input.read(cx).value().to_string();
                            let rest = v[..v.len().saturating_sub(tlen)].to_owned();
                            this.input.update(cx, |i, cx| { i.set_value(rest, w, cx); i.focus(w, cx); });
                            match &pick {
                                Pick::Page(p) => this.refs.push(p.clone()),
                                Pick::Skill(s) => this.skills.push(s.clone()),
                                Pick::NewConv => this.new_conv(w, cx),
                            }
                            cx.notify();
                        }))
                        .child(glyph(if name.starts_with('/') { Icon::Sparkles } else { Icon::Note }, 14., theme.accent))
                        .child(div().text_size(px(13.)).child(name))
                        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(12.)).text_color(theme.text_secondary).child(desc)))
                })
            };
            div().mx(px(12.)).max_h(px(240.)).rounded(px(8.)).border_1().border_color(theme.menu_border).bg(theme.menu).shadow_md()
                .child(div().px(px(10.)).pt(px(6.)).text_size(px(11.)).text_color(theme.text_secondary).child(if token.starts_with('@') { "引用笔记" } else { "指令与 Skill" }))
                .child(list)
        });
        let busy = self.streaming.is_some();
        let action = if busy {
            icon_btn("ai-chat-stop", Icon::Stop, "停止生成").on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.stop(); cx.notify(); }))
        } else {
            div().id("ai-chat-send").size(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(8.)).bg(theme.primary)
                .cursor(CursorStyle::PointingHand).tooltip(|w, cx| gpui_component::tooltip::Tooltip::new("发送消息").build(w, cx))
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, w, cx| this.send(w, cx)))
                .child(glyph(Icon::ArrowUp, 16., gpui::white()))
        };
        let composer = div().flex_none().m(px(12.)).p(px(10.)).flex().flex_col().gap(px(8.)).rounded(px(12.)).border_1().border_color(theme.btn_border).bg(theme.paper)
            .when(!self.refs.is_empty() || !self.skills.is_empty(), |d| d.child(chips))
            .child(div().h(px(28.)).text_size(px(14.)).child(Input::new(&self.input).appearance(false).bordered(false).focus_bordered(false).h(px(28.))))
            .child(div().flex().items_center().gap(px(6.))
                .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(12.)).text_color(theme.text_secondary)
                    .child(SharedString::from(if model.is_empty() { "当前模型：默认".to_owned() } else { format!("当前模型：{model}") })))
                .child(action));

        div().id("ai-chat-panel").w(px(380.)).flex_none().h_full().flex().flex_col().relative().bg(theme.paper).border_l_1().border_color(theme.hairline)
            .text_color(theme.text)
            .child(header)
            .child(div().id("ai-chat-scroll").flex_1().min_h_0().overflow_y_scroll().track_scroll(&self.scroll).child(body))
            .children(error)
            .children(picker)
            .child(composer)
            .children(history.map(|h| div().absolute().top(px(44.)).left_0().right_0().child(h)))
    }
}

fn tool_label(name: &str) -> &'static str {
    match name {
        "loadSkill" => "加载 Skill", "listPages" => "列出页面", "searchNotes" => "搜索笔记", "readPage" => "读取页面",
        "executeBatchPlan" => "生成变更计划", _ => "调用工具",
    }
}

/// thinkly's NotebookBatchPlanCard: 笔记变更计划 with 原文 / 新片段, 应用 and 撤回.
fn plan_card(at: usize, plan: agent::Plan, cx: &mut gpui::Context<AiChat>) -> impl IntoElement {
    use agent::{OpKind, PlanState};
    let theme = Theme::ocean();
    let n = plan.ops.len();
    let (badge, note) = match plan.state {
        PlanState::Pending => ("等待确认", "方案已生成，尚未修改笔记。".to_owned()),
        PlanState::Applied => ("已应用", format!("已应用 {} 项操作。", n)),
        PlanState::Canceled => ("已取消", "未修改笔记。".to_owned()),
        PlanState::Undone => ("已撤回", "本次变更已撤回。".to_owned()),
        PlanState::UndoConflict => ("撤回冲突", "笔记在应用后发生变化，未覆盖后续编辑。".to_owned()),
        PlanState::Stale => ("已取消", "原文或目标位置已变化，请基于当前笔记重新生成方案。".to_owned()),
    };
    let snippet = |label: &'static str, text: String, del: bool| div().flex().flex_col().gap(px(2.))
        .child(div().text_size(px(11.)).text_color(theme.text_secondary).child(label))
        .child(div().px(px(8.)).py(px(4.)).rounded(px(6.)).text_size(px(12.)).line_height(px(18.))
            .bg(if del { gpui::hsla(0., 0.75, 0.5, 0.08) } else { gpui::hsla(0.38, 0.6, 0.4, 0.10) })
            .when(del, |d| d.line_through())
            .child(if text.is_empty() { "（空）".to_owned() } else { text.chars().take(400).collect() }));
    let ops = plan.ops.iter().fold(div().flex().flex_col().gap(px(8.)), |d, op| {
        let (label, body) = match &op.kind {
            OpKind::Replace { old, new, all } => (if *all { "替换全部匹配" } else { "替换首处匹配" },
                div().flex().flex_col().gap(px(4.)).child(snippet("原文", old.clone(), true)).child(snippet("新片段", new.clone(), false))),
            OpKind::Edit { markdown } => ("替换整篇正文", div().child(snippet("新正文", markdown.clone(), false))),
            OpKind::Create { markdown } => ("新建页面", div().child(snippet("正文", markdown.clone(), false))),
        };
        d.child(div().flex().flex_col().gap(px(4.))
            .child(div().flex().items_center().gap(px(6.)).text_size(px(12.))
                .child(div().font_weight(FontWeight(600.)).child(label))
                .child(div().text_color(theme.text_secondary).child(op.page.clone())))
            .child(body))
    });
    let btn = |id: String, _label: String, primary: bool| div().id(SharedString::from(id)).h(px(28.)).px(px(12.)).flex().items_center().rounded(px(6.))
        .text_size(px(12.)).cursor(CursorStyle::PointingHand)
        .when(primary, |d| d.bg(theme.primary).text_color(gpui::white()))
        .when(!primary, |d| d.border_1().border_color(theme.btn_border).hover(move |s| s.bg(theme.hover)));
    let actions = match plan.state {
        PlanState::Pending => Some(div().flex().justify_end().gap(px(8.))
            .child(btn(format!("plan-cancel-{at}"), "取消".into(), false).child("取消")
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| this.set_plan_state(at, PlanState::Canceled, cx))))
            .child(btn(format!("plan-apply-{at}"), String::new(), true).child(if n > 1 { format!("应用 {n} 项") } else { "应用".into() })
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| this.apply_plan(at, cx))))),
        PlanState::Applied => Some(div().flex().justify_end()
            .child(btn(format!("plan-undo-{at}"), String::new(), false).child("撤回本次变更")
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| this.undo_plan(at, cx))))),
        PlanState::Stale => Some(div().flex().justify_end()
            .child(btn(format!("plan-regen-{at}"), String::new(), false).child("重新生成方案")
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                    this.input.update(cx, |i, cx| i.set_value("请基于当前笔记重新生成方案", w, cx));
                    this.send(w, cx);
                })))),
        _ => None,
    };
    div().flex().flex_col().gap(px(8.)).p(px(12.)).rounded(px(10.)).border_1().border_color(theme.btn_border).bg(theme.paper)
        .child(div().flex().items_center().gap(px(8.))
            .child(glyph(Icon::Sparkles, 14., theme.accent))
            .child(div().flex_1().text_size(px(13.)).font_weight(FontWeight(600.)).child(if plan.title.is_empty() { "笔记变更计划".to_owned() } else { format!("笔记变更计划 · {}", plan.title) }))
            .child(div().px(px(6.)).py(px(1.)).rounded(px(4.)).bg(theme.hover).text_size(px(11.)).text_color(theme.text_secondary).child(badge)))
        .when(!plan.summary.is_empty(), |d| d.child(div().text_size(px(12.)).text_color(theme.text_secondary).child(plan.summary.clone())))
        .child(ops)
        .child(div().text_size(px(12.)).text_color(theme.text_secondary).child(note))
        .children(actions)
}
