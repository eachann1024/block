//! Rendering and batch-plan card UI for `AiChat`.
use std::cell::Cell;
use std::rc::Rc;
use crate::ui::tooltip::TooltipExt;
use gpui::{Bounds, CursorStyle, Focusable, FontWeight, IntoElement, MouseButton, ParentElement, Pixels, Render, SharedString, Styled, Window, WindowControlArea, div, prelude::*, px};

thread_local! {
    static HISTORY_TRIGGER: Rc<Cell<Option<Bounds<Pixels>>>> = Rc::new(Cell::new(None));
}


use super::chat::{AiChat, first_placeholder, follow_placeholder};
use super::backend_ui;
use crate::icons::{Icon, icon, stateful_icon};
use crate::theme::{is_en, tr};

fn duo_glyph(i: Icon, size: f32, color: gpui::Hsla) -> impl IntoElement {
    icon(i, px(size), crate::ui::ai_theme::theme().icon_colors(color))
}

fn title_drag(id: &'static str, drag: bool) -> gpui::Stateful<gpui::Div> {
    div().id(id).when(drag, |row| row
        .window_control_area(WindowControlArea::Drag)
        .on_click(|event, window, _| if event.click_count() == 2 { window.titlebar_double_click(); }))
}

#[cfg(target_family = "wasm")]
fn wasm_traffic_lights() -> gpui::Div {
    div().absolute().left(px(14.)).top_0().bottom_0().flex().items_center().gap(px(8.))
        .child(div().size(px(12.)).rounded_full().bg(gpui::rgb(0xff5f57)))
        .child(div().size(px(12.)).rounded_full().bg(gpui::rgb(0xfebc2e)))
        .child(div().size(px(12.)).rounded_full().bg(gpui::rgb(0x28c840)))
}
fn size_panel(panel: gpui::Stateful<gpui::Div>, fullscreen: bool, split: crate::ui::shell_layout::ShellSplit) -> gpui::Stateful<gpui::Div> {
    let theme = crate::ui::ai_theme::theme();
    let width = px(split.chat_width);
    let overlay = !fullscreen && split.chat_overlay;
    panel
        .when(fullscreen, |p| p.flex_1().w_full().min_w_0())
        .when(overlay, |p| p.size_full())
        .when(!fullscreen && !overlay, |p| p.w(width).flex_none())
        .when(!fullscreen, |p| p.border_l_1().border_color(theme.hairline))
}

impl Render for AiChat {
    fn render(&mut self, window: &mut Window, cx: &mut gpui::Context<Self>) -> impl IntoElement {
        let split = self.displayed_split(window);
        if self.last_en != is_en() {
            self.last_en = is_en();
            let ph = if self.conv().is_some() { follow_placeholder() } else { first_placeholder() };
            self.input.update(cx, |i, cx| i.set_placeholder(ph, window, cx));
        }
        let theme = crate::ui::ai_theme::theme();
        if super::chat_empty::builtin_blocked(self) {
            let focus = self.input.read(cx).focus_handle(cx);
            let macos_chrome = crate::ui::window_controls::is_macos();
            return size_panel(div().id("ai-chat-panel").track_focus(&focus), self.fullscreen, split)
                .h_full().flex().flex_col().relative().bg(theme.paper).text_color(theme.text)
                .capture_action(cx.listener(|this, _: &gpui_component::input::Escape, window, cx| { cx.stop_propagation(); this.open = false; this.history = false; crate::ui::motion::popup_reset("ai-chat-history"); crate::ui::motion::popup_reset("ai-agent-deck"); window.refresh(); cx.notify(); }))
                .when(!self.fullscreen, |p| p.child(self.resize_handle(cx)))
                .when(self.fullscreen, |panel| {
                    let gutter = title_drag("ai-blocked-traffic-drag", true).w(px(crate::ui::window_controls::TRAFFIC_LEADING)).h_full().flex_none();
                    #[cfg(target_family = "wasm")]
                    let gutter = gutter.relative().child(wasm_traffic_lights());
                    panel.child(div().h(px(52.)).flex_none().flex().items_center().gap(px(4.)).border_b_1().border_color(theme.hairline).bg(theme.paper)
                        .pr(px(if macos_chrome { 16. } else { 4. }))
                        .child(gutter)
                        .child(title_drag("ai-blocked-title-drag", true).flex_1().h_full())
                        .child(crate::ui::icon_button::header_action_button("ai-layout-toggle", Icon::WindowRestore, true, Some(tr("切回侧栏", "Switch to sidebar")),
                            cx.listener(|this, _, window, cx| { this.set_fullscreen(false, cx); window.refresh(); cx.notify(); })))
                        .child(crate::ui::icon_button::header_action_button("ai-chat-close", Icon::Close, false, None, cx.listener(|this, _, window, cx| {
                            this.open = false; this.history = false;
                            crate::ui::motion::popup_reset("ai-chat-history"); crate::ui::motion::popup_reset("ai-agent-deck");
                            backend_ui::close_menu(); window.refresh(); cx.notify();
                        })))
                        .when(!macos_chrome, |bar| bar.child(crate::ui::window_controls::window_controls("main-win", true, true))))
                })
                .child(div().id("ai-chat-scroll").flex_1().min_h_0().size_full().flex().flex_col().items_center().justify_center().child(super::chat_empty::enable_cover(self.fullscreen, cx)));
        }
        let title = self.conv().map(|conversation| conversation.title.clone()).unwrap_or_else(|| tr("新会话", "New chat").to_owned());
        let macos_chrome = crate::ui::window_controls::is_macos();
        let header = div().h(px(52.)).flex_none().flex().items_center().gap(px(4.)).border_b_1().border_color(theme.hairline)
            .when(self.fullscreen, |bar| bar.bg(theme.paper).pr(px(if macos_chrome { 16. } else { 4. })))
            .when(!self.fullscreen, |bar| bar.px(px(16.)))
            .when(self.fullscreen, |bar| {
                let gutter = title_drag("ai-chat-traffic-drag", true).w(px(crate::ui::window_controls::TRAFFIC_LEADING)).h_full().flex_none();
                #[cfg(target_family = "wasm")]
                let gutter = gutter.relative().child(wasm_traffic_lights());
                bar.child(gutter)
            })
            .child(title_drag("ai-chat-title-drag", self.fullscreen).flex_1().min_w_0().h_full().flex().items_center().overflow_hidden()
                .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(15.)).font_weight(FontWeight(600.)).child(title)))
            .child(crate::ui::icon_button::header_action_button("ai-chat-new", Icon::AiPlus, false, Some(tr("新建会话", "New chat")), cx.listener(|this, _, w, cx| {
                cx.stop_propagation();
                let backend = this.selected_backend(); this.save_tab_draft(cx); this.tab_drafts.remove(&None); this.new_conv(w, cx); this.select_backend(backend, w, cx);
                // Restore focus after mouse-down blur handlers have finished.
                cx.defer_in(w, |this, window, cx| this.input.update(cx, |input, cx| input.focus(window, cx)));
            })))
            .child(crate::ui::icon_button::header_action_button("ai-layout-toggle", if self.fullscreen { Icon::WindowRestore } else { Icon::WindowMax }, self.fullscreen, Some(if self.fullscreen { tr("切回侧栏", "Switch to sidebar") } else { tr("全屏", "Full screen") }),
                cx.listener(|this, _, window, cx| { this.set_fullscreen(!this.fullscreen, cx); window.refresh(); cx.notify(); })))
            .child(crate::ui::icon_button::header_action_button("ai-chat-history", Icon::History, self.history, Some(tr("历史会话", "Chat history")), cx.listener(|this, _, _, cx| {
                backend_ui::close_menu(); this.history = !this.history; cx.notify();
            })).child(crate::ui::placement::track_bounds(HISTORY_TRIGGER.with(Clone::clone), self.history)))
            .child(crate::ui::icon_button::header_action_button("ai-chat-close", Icon::Close, false, None, cx.listener(|this, _, window, cx| { this.open = false; this.history = false; crate::ui::motion::popup_reset("ai-chat-history"); crate::ui::motion::popup_reset("ai-agent-deck"); backend_ui::close_menu(); window.refresh(); cx.notify(); })))
            .when(self.fullscreen && !macos_chrome, |bar| bar.child(crate::ui::window_controls::window_controls("main-win", true, true)));
        let fullscreen = self.fullscreen;
        let menu_room = if fullscreen { f32::from(window.viewport_size().width) } else { split.chat_width };
        let menu_w = (menu_room - 32.).clamp(220., 320.);
        let history_max_h = (f32::from(window.viewport_size().height) - 140.).clamp(160., if fullscreen { 420. } else { 304. });
        let history_motion = crate::ui::motion::popup_motion("ai-chat-history", self.history);
        if let Some((_, pop_gen)) = history_motion {
            crate::ui::motion::schedule_popup_finish("ai-chat-history", pop_gen, cx);
        }
        let history = history_motion.is_some().then(|| {
            let list = if self.convs.iter().all(|c| c.messages.is_empty()) {
                div().p(px(12.)).text_size(px(14.)).text_color(theme.text_secondary).child(tr("暂无历史会话", "No chat history"))
            } else {
                self.convs.iter().filter(|c| !c.messages.is_empty()).fold(div().flex().flex_col().p(px(4.)), |d, c| {
                    let (id, active) = (c.id, Some(c.id) == self.current);
                    let del_id = SharedString::from(format!("conv-del-{id}"));
                    d.child(div().id(SharedString::from(format!("conv-{id}"))).relative().children(crate::ui::smoke_overlay::probe(format!("conv-{id}"))).h(px(32.)).px(px(8.)).flex().items_center().gap(px(8.)).rounded(crate::ui::radius::CONTROL)
                        .when(active, |r| r.bg(theme.hover)).hover(move |s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                            cx.stop_propagation();
                            this.switch_tab(Some(id), w, cx);
                        }))
                        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(14.)).child(c.title.clone()))
                        .child(div().flex_none().text_size(px(13.)).text_color(theme.text_secondary).child(backend_ui::backend_name(c.backend)))
                        .child(div().id(del_id.clone()).group(del_id.clone()).size(px(22.)).flex_none().flex().items_center().justify_center().rounded(crate::ui::radius::MICRO_SM)
                            .hover(move |s| s.bg(theme.shell))
                            .tooltip_above(crate::theme::tooltip(tr("删除会话", "Delete chat")))
                            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                                cx.stop_propagation();
                                if this.current == Some(id) { super::plan_link::clear(cx); this.stop(); this.current = None; }
                                this.tab_drafts.remove(&Some(id)); this.convs.retain(|c| c.id != id); this.persist(); this.persist_active_draft(cx); cx.notify();
                            }))
                            .child(stateful_icon(del_id, Icon::Trash, px(13.), false, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text)))))
                })
            };
            // Hung from the history button; the panel positions it with the shared anchor.
            div().id("ai-chat-history-list").w(px(menu_w)).max_h(px(history_max_h)).overflow_y_scroll().rounded(crate::ui::radius::SURFACE).border_1()
                .border_color(theme.menu_border).bg(theme.menu).shadow_md().occlude()
                .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .child(list)
        });
        let empty = self.conv().is_none_or(|c| c.messages.is_empty()) && self.streaming.is_none();
        let body = super::chat_process::render_messages(self, cx)
            .when(empty, |body| body.child(super::chat_empty::view(self, cx)));
        let error = self.error.clone().map(|e| div().mx(px(12.)).mb(px(8.)).p(px(10.)).flex().items_start().gap(px(8.)).rounded(crate::ui::radius::CONTROL)
            .bg(gpui::hsla(0., 0.75, 0.5, 0.08)).text_size(px(14.))
            .child(div().flex_1().flex().flex_col().gap(px(2.))
                .child(div().font_weight(FontWeight(600.)).text_color(gpui::rgb(0xb42318)).child(tr("本轮失败原因", "Request failed")))
                .child(div().text_color(theme.text).child(e))
                .when(self.error_settings, |d| d.child(crate::ui::ai_controls::action_chip(
                    "ai-chat-settings", None, tr("打开 AI 设置", "Open AI settings"), false, false,
                    |_, window, cx| window.defer(cx, |window, cx| window.dispatch_action(Box::new(super::OpenAiSettings), cx))))))
            .child(crate::ui::icon_button::header_action_button("ai-chat-error-close", Icon::Close, false, Some(tr("关闭错误提示", "Dismiss error")), cx.listener(|this, _, _, cx| { this.error = None; cx.notify(); }))));

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
        self.refresh_picker(cx);
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
                    .child(if self.picker_cache.loading { tr("正在加载…", "Loading…") } else if token.starts_with('@') { tr("没有匹配的文件", "No matching files") } else { tr("没有匹配的指令或技能", "No matching commands or skills") })
                    .into_any_element()
            } else {
                let scroll = super::chat_composer::PICKER_SCROLL.with(Clone::clone);
                let height = (picks.len() as f32 * 36.).min(picker_height - 32.);
                gpui::uniform_list("ai-reference-list", picks.len(), cx.processor(move |_, range: std::ops::Range<usize>, _, cx| {
                    range.map(|i| {
                    let (pick, name, desc) = picks[i].clone();
                    let row = crate::ui::dropdown::row_with_colors(SharedString::from(format!("pick-{i}")), crate::ui::dropdown::RowSpec {
                        icon: Some(match &pick { super::chat::Pick::Folder(_) => Icon::Folder, super::chat::Pick::Page(_) => Icon::Note, _ => Icon::Sparkles }),
                        highlighted: i == selected,
                        chevron: matches!(&pick, super::chat::Pick::Folder(_)),
                        label: name.into(), hint: Some(desc.into()), ..Default::default()
                    }, Some((theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(if i == selected { theme.accent } else { theme.text })))).relative().children(crate::ui::smoke_overlay::probe(format!("ai-pick-{i}"))).h(px(36.)).when(i == selected, |row| row.bg(theme.selected).border_1().border_color(theme.accent))
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                        cx.stop_propagation(); this.accept_pick(pick.clone(), w, cx);
                    }));
                    row
                    }).collect::<Vec<_>>()
                })).track_scroll(&scroll).h(px(height)).w_full().into_any_element()
            };
            div().mx(px(16.)).mb(px(4.)).flex_none().max_h(px(picker_height)).rounded(crate::ui::radius::SURFACE).border_1().border_color(theme.menu_border).bg(theme.menu)
                .child(div().px(px(10.)).pt(px(6.)).text_size(px(14.)).text_color(theme.text_secondary).child(if token.starts_with('@') { tr("引用文件", "Reference File") } else { tr("指令与技能", "Commands & Skills") }))
                .child(list)
        });
        let busy = self.streaming.is_some() || self.pending_question.is_some();
        let action = if busy {
            crate::ui::ai_controls::action_chip("ai-chat-stop", None, tr("停止", "Stop"), false, false, cx.listener(|this, _, _, cx| { this.stop(); cx.notify(); }))
                .h(px(28.)).bg(theme.text).text_color(theme.paper).font_weight(FontWeight(500.)).hover(move |s| s.bg(theme.text_secondary))
        } else {
            crate::ui::ai_controls::send_button_enabled("ai-chat-send", !self.input.read(cx).value().trim().is_empty(), cx.listener(move |this, _, w, cx| { cx.stop_propagation(); this.send(w, cx); })).size(px(28.))
        };
        if let Some((id, _, _)) = &self.pending_approval {
            if self.approval_state.as_ref().is_none_or(|(current, _)| current != id) {
                self.approval_state = Some((id.clone(), Default::default()));
            }
        } else { self.approval_state = None; }
        let approval = self.pending_approval.clone().map(|(request_id, title, detail)| {
            let entity = cx.entity().downgrade();
            let expected = request_id.clone();
            let state = self.approval_state.as_ref().unwrap().1.clone();
            backend_ui::approval_card(state, title, detail, move |response, _, cx| {
                entity.update(cx, |this, cx| {
                    if this.pending_approval.as_ref().is_none_or(|(id, _, _)| id != &expected) { return false; }
                    this.respond_approval(response, cx);
                    this.pending_approval.is_none()
                }).unwrap_or(false)
            }).into_any_element()
        });
        let question = self.pending_question.as_ref().map(|pq| (pq.request_id.clone(), pq.questions.clone(), pq.state.clone())).map(|(expected, questions, state)| {
            let entity = cx.entity().downgrade();
            backend_ui::question_card(state, questions, window, cx, move |value, window, cx| {
                entity.update(cx, |this, cx| {
                    if this.pending_question.as_ref().is_none_or(|pq| pq.request_id != expected) { return false; }
                    this.respond_question(value, window, cx)
                }).unwrap_or(false)
            })
        }).or_else(|| {
            if self.streaming.is_some() || self.pending_approval.is_some() { return None; }
            let conv = self.conv()?;
            let (idx, last) = conv.messages.iter().enumerate().rev().find(|(_, m)| m.role == "assistant" || m.role == "user")?;
            if last.role != "assistant" { return None; }
            let questions = crate::ui::ai_approval::extract_markdown_questions(&last.content);
            if questions.is_empty() { return None; }
            let key = format!("md-q-{}-{idx}", conv.id);
            if self.approval_state.as_ref().is_none_or(|(id, _)| id != &key) {
                self.approval_state = Some((key.clone(), Default::default()));
            }
            let state = self.approval_state.as_ref().unwrap().1.clone();
            let entity = cx.entity().downgrade();
            Some(backend_ui::question_card(state, questions, window, cx, move |value, window, cx| {
                entity.update(cx, |this, cx| {
                    if value.get("status").and_then(|v| v.as_str()) == Some("skipped") {
                        if let Some((_, st)) = &this.approval_state { st.borrow_mut().dismissed = true; }
                        cx.notify();
                        return true;
                    }
                    this.approval_state = None;
                    let summary = value.get("items").and_then(|v| v.as_array()).into_iter().flatten().enumerate().filter_map(|(i, item)| {
                        let ans = item.get("answer").and_then(|v| v.as_str()).unwrap_or("").trim();
                        (!ans.is_empty()).then(|| format!("{}. {ans}", i + 1))
                    }).collect::<Vec<_>>().join("\n");
                    if !summary.is_empty() {
                        this.input.update(cx, |input, cx| input.set_value(&summary, window, cx));
                        this.send(window, cx);
                    }
                    true
                }).unwrap_or(false)
            }))
        });
        let interactive_card = question.or(approval);
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
        let composer = div().pt(px(2.)).pb(px(10.)).flex().flex_col().gap(px(4.)).rounded(px(8.)).border_1().border_color(if self.input.read(cx).focus_handle(cx).is_focused(window) { theme.btn_border } else { theme.hairline }).bg(theme.hover)
            .when(!self.refs.is_empty() || !self.skills.is_empty(), |d| d.child(chips.px(px(10.)).pt(px(8.))))
            .child(div().on_mouse_down(MouseButton::Left, cx.listener(|_, _, _, cx| {
                if backend_ui::close_menu() { cx.notify(); }
                cx.stop_propagation();
            })).child(crate::ui::chat_input::field(&self.input)))
            .child(div().relative().children(crate::ui::smoke_overlay::probe("ai-composer-actions")).px(px(10.)).flex_none().flex().items_center().gap(px(6.))
                .child(div().flex_1().min_w_0().flex().items_center().gap(px(6.))
                    .child(super::chat_deck::trigger(self, cx)))
                .child(action));

        size_panel(div().id("ai-chat-panel").key_context("AiChat").track_focus(&self.panel_focus), self.fullscreen, split).h_full().flex().flex_col().relative().bg(theme.paper).font_family(crate::theme::ui_font_family())
            .text_color(theme.text)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                if backend_ui::close_menu() { cx.notify(); }
                window.focus(&this.panel_focus, cx);
            }))
            .on_action(cx.listener(|this, _: &super::CloseAiPanel, window, cx| {
                cx.stop_propagation();
                if this.picker_token(cx).is_some() { this.picker_dismissed = true; }
                else if backend_ui::close_menu() {}
                else if this.history { this.history = false; }
                else {
                    this.open = false;
                    this.history = false;
                    crate::ui::motion::popup_reset("ai-chat-history");
                    crate::ui::motion::popup_reset("ai-agent-deck");
                    if let Some(editor) = this.editor.as_ref().and_then(|editor| editor.upgrade()) {
                        let focus = editor.read(cx).focus_handle(cx);
                        window.focus(&focus, cx);
                    }
                    window.refresh();
                }
                cx.notify();
            }))
            .capture_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, window, cx| {
                if event.keystroke.key == "escape" && this.picker_token(cx).is_some() {
                    this.picker_dismissed = true; cx.stop_propagation(); cx.notify();
                } else if event.keystroke.key == "tab" && !event.keystroke.modifiers.modified()
                    && this.input.read(cx).focus_handle(cx).is_focused(window) && !this.composing(window, cx)
                    && this.picker_token(cx).is_some_and(|token| !this.picker_items(&token, cx).is_empty()) {
                    cx.stop_propagation(); this.send(window, cx);
                }
            }))
            .capture_action(cx.listener(|this, action: &gpui_component::input::Enter, window, cx| {
                if this.composing(window, cx) { cx.stop_propagation(); return; }
                if !action.shift && !action.secondary && this.picker_token(cx).is_some() {
                    cx.stop_propagation(); this.send(window, cx);
                }
            }))
            .capture_action(cx.listener(|this, _: &gpui_component::input::IndentInline, window, cx| {
                if !this.composing(window, cx)
                    && this.picker_token(cx).is_some_and(|token| !this.picker_items(&token, cx).is_empty()) {
                    cx.stop_propagation(); this.send(window, cx);
                }
            }))
            .capture_action(cx.listener(|this, _: &gpui_component::input::MoveUp, _, cx| {
                if this.move_pick(-1, cx) { cx.stop_propagation(); }
            }))
            .capture_action(cx.listener(|this, _: &gpui_component::input::MoveDown, _, cx| {
                if this.move_pick(1, cx) { cx.stop_propagation(); }
            }))
            .capture_action(cx.listener(|this, _: &gpui_component::input::Escape, _, cx| {
                if this.picker_token(cx).is_some() { this.picker_dismissed = true; }
                else if backend_ui::close_menu() {} else if this.history { this.history = false; }
                else { return; }
                cx.stop_propagation();
                cx.notify();
            }))
            .when(!self.fullscreen, |panel| panel.child(self.resize_handle(cx)))
            .child(header)
            .child(div().id("ai-chat-scroll").flex_1().min_h_0().overflow_y_scroll().track_scroll(&self.scroll)
                .on_scroll(|_, _, cx| crate::dropdown::dismiss_all(cx))
                .child(div().w_full().when(self.fullscreen, |inner| inner.max_w(px(720.)).mx_auto()).child(body)
                    .children(error)))
            .children(interactive_card.map(|card| div().flex_none().px(px(16.)).pt(px(8.)).pb(px(4.))
                .child(div().w_full().when(self.fullscreen, |inner| inner.max_w(px(720.)).mx_auto()).child(card))))
            .children(draft_recovery)
            .children(picker)
            .child(div().flex_none().relative().px(px(16.)).pt(px(if picker_open { 0. } else { 12. })).pb(px(16.)).border_t_1().border_color(theme.hairline)
                .child(div().w_full().when(self.fullscreen, |inner| inner.max_w(px(720.)).mx_auto()).relative().child(composer)
                    .children(super::chat_deck::view(self, window, cx))))
            .when(history_motion.is_some(), |panel| {
                let entering = history_motion.is_some_and(|(phase, _)| phase == crate::ui::motion::PopupMotion::Enter);
                let pop_gen = history_motion.map(|(_, pop_gen)| pop_gen).unwrap_or(0);
                let scrim = div().id("ai-chat-history-dismiss").absolute().top(px(52.)).left_0().right_0().bottom_0().occlude()
                    .bg(gpui::hsla(0., 0., 0., if fullscreen { if crate::theme::is_dark() { 0.46 } else { 0.28 } } else if crate::theme::is_dark() { 0.28 } else { 0.08 }))
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                        cx.stop_propagation();
                        this.history = false;
                        cx.notify();
                    }));
                panel.child(crate::ui::motion::layer_fade(scrim, ("ai-chat-history-scrim", pop_gen), entering))
            })
            .children(history_motion.zip(history).map(|((phase, pop_gen), card)| {
                crate::dropdown::anchored_motion_with(("ai-chat-history-pop", pop_gen), move |window, _| {
                    HISTORY_TRIGGER.with(|cell| cell.get()).map(|trigger| {
                        crate::dropdown::PlacementSpec::below(trigger)
                            .align_x(crate::dropdown::AlignX::Right)
                            .gap(px(6.))
                            .margin(px(8.))
                    }).unwrap_or_else(|| crate::dropdown::PlacementSpec::point(window.mouse_position()))
                }, card, phase, 24)
            }))
    }
}
