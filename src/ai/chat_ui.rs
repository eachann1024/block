//! Rendering and batch-plan card UI for `AiChat`.
use crate::ui::tooltip::TooltipExt;
use gpui::{CursorStyle, Focusable, FontWeight, IntoElement, MouseButton, ParentElement, Render, SharedString, Styled, Window, div, prelude::*, px};


use super::chat::{AiChat, first_placeholder, follow_placeholder};
use super::backend_ui;
use crate::icons::{Icon, icon, stateful_icon};
use crate::theme::{is_en, tr};

fn duo_glyph(i: Icon, size: f32, color: gpui::Hsla) -> impl IntoElement {
    icon(i, px(size), crate::ui::ai_theme::theme().icon_colors(color))
}
impl Render for AiChat {
    fn render(&mut self, window: &mut Window, cx: &mut gpui::Context<Self>) -> impl IntoElement {
        if self.last_en != is_en() {
            self.last_en = is_en();
            let ph = if self.conv().is_some() { follow_placeholder() } else { first_placeholder() };
            self.input.update(cx, |i, cx| i.set_placeholder(ph, window, cx));
        }
        let theme = crate::ui::ai_theme::theme();
        if super::chat_empty::builtin_blocked(self) {
            let focus = self.input.read(cx).focus_handle(cx);
            return div().id("ai-chat-panel").track_focus(&focus).when(self.fullscreen, |p| p.flex_1().w_full().min_w_0())
                .when(!self.fullscreen, |p| p.w(self.panel_width).flex_none().border_l_1().border_color(theme.hairline))
                .h_full().flex().flex_col().relative().bg(theme.paper).text_color(theme.text)
                .capture_action(cx.listener(|this, _: &gpui_component::input::Escape, window, cx| { cx.stop_propagation(); super::chat::chat_dictation::stop_for_input(&this.input); this.open = false; this.fullscreen = false; window.refresh(); cx.notify(); }))
                .when(!self.fullscreen, |p| p.child(self.resize_handle(cx)))
                .child(div().id("ai-chat-scroll").flex_1().min_h_0().size_full().flex().flex_col().items_center().justify_center().child(super::chat_empty::enable_cover(cx)));
        }
        let title = self.conv().map(|conversation| conversation.title.clone()).unwrap_or_else(|| tr("新会话", "New chat").to_owned());
        let header = div().h(px(52.)).flex_none().px(px(16.)).flex().items_center().gap(px(4.)).border_b_1().border_color(theme.hairline)
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(15.)).font_weight(FontWeight(600.)).child(title))
            .child(crate::ui::icon_button::header_action_button("ai-layout-toggle", if self.fullscreen { Icon::WindowRestore } else { Icon::WindowMax }, self.fullscreen, if self.fullscreen { tr("切回侧栏", "Switch to sidebar") } else { tr("全屏", "Full screen") },
                cx.listener(|this, _, window, cx| { this.fullscreen = !this.fullscreen; window.refresh(); cx.notify(); })))
            .child(crate::ui::icon_button::header_action_button("ai-chat-new", Icon::AiPlus, false, tr("新建会话", "New chat"), cx.listener(|this, _, w, cx| {
                cx.stop_propagation();
                let backend = this.selected_backend(); this.save_tab_draft(cx); this.tab_drafts.remove(&None); this.new_conv(w, cx); this.select_backend(backend, w, cx);
                // Restore focus after mouse-down blur handlers have finished.
                cx.defer_in(w, |this, window, cx| this.input.update(cx, |input, cx| input.focus(window, cx)));
            })))
            .child(crate::ui::icon_button::header_action_button("ai-chat-history", Icon::History, self.history, tr("历史会话", "Chat history"), cx.listener(|this, _, _, cx| {
                backend_ui::close_menu(); this.history = !this.history; cx.notify();
            })))
            .child(crate::ui::icon_button::header_action_button("ai-chat-close", Icon::Close, false, tr("关闭 AI", "Close AI"), cx.listener(|this, _, window, cx| { super::chat::chat_dictation::stop_for_input(&this.input); this.open = false; this.fullscreen = false; backend_ui::close_menu(); window.refresh(); cx.notify(); })));
        let history = self.history.then(|| {
            let list = if self.convs.iter().all(|c| c.messages.is_empty()) {
                div().p(px(12.)).text_size(px(14.)).text_color(theme.text_secondary).child(tr("暂无历史会话", "No chat history"))
            } else {
                self.convs.iter().filter(|c| !c.messages.is_empty()).fold(div().flex().flex_col().p(px(4.)), |d, c| {
                    let (id, active) = (c.id, Some(c.id) == self.current);
                    let del_id = SharedString::from(format!("conv-del-{id}"));
                    d.child(div().id(SharedString::from(format!("conv-{id}"))).relative().children(crate::ui::smoke_overlay::probe(format!("conv-{id}"))).h(px(32.)).px(px(8.)).flex().items_center().gap(px(6.)).rounded(crate::ui::radius::CONTROL)
                        .when(active, |r| r.bg(theme.hover)).hover(move |s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                            this.switch_tab(Some(id), w, cx);
                        }))
                        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(14.)).child(c.title.clone()))
                        .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(backend_ui::backend_name(c.backend)))
                        .child(div().id(del_id.clone()).group(del_id.clone()).size(px(22.)).flex().items_center().justify_center().rounded(crate::ui::radius::MICRO_SM)
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
            div().id("ai-chat-history-list").mx(px(8.)).mt(px(6.)).max_h(px(260.)).overflow_y_scroll().rounded(crate::ui::radius::SURFACE).border_1()
                .border_color(theme.menu_border).bg(theme.menu).shadow_md().child(list)
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
        let busy = self.streaming.is_some();
        let action = if busy {
            crate::ui::ai_controls::action_chip("ai-chat-stop", None, tr("停止", "Stop"), false, false, cx.listener(|this, _, _, cx| { this.stop(); cx.notify(); })).h(px(28.)).bg(theme.text).text_color(theme.paper)
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
                        super::chat::chat_dictation::stop_for_input(&this.input);
                        this.restore_deferred_draft(window, cx); cx.notify();
                    })))
        });
        let composer = div().pt(px(2.)).pb(px(10.)).flex().flex_col().gap(px(4.)).rounded(px(8.)).border_1().border_color(if self.input.read(cx).focus_handle(cx).is_focused(window) { theme.btn_border } else { theme.hairline }).bg(theme.hover)
            .when(!self.refs.is_empty() || !self.skills.is_empty(), |d| d.child(chips.px(px(10.)).pt(px(8.))))
            .child(crate::ui::chat_input::field(&self.input))
            .child(div().relative().children(crate::ui::smoke_overlay::probe("ai-composer-actions")).px(px(10.)).flex_none().flex().items_center().gap(px(6.))
                .child(div().flex_1().min_w_0().flex().items_center().gap(px(6.))
                    .child(super::chat_deck::trigger(self, cx)))
                .child(crate::ui::ai_controls::dictation_button(
                    super::chat::chat_dictation::is_recording(self.input.entity_id()),
                    cx.listener(|this, _, window, cx| { cx.stop_propagation(); super::chat::chat_dictation::toggle(this, window, cx); })))
                .child(action));

        div().id("ai-chat-panel").when(self.fullscreen, |panel| panel.flex_1().w_full().min_w_0()).when(!self.fullscreen, |panel| panel.w(self.panel_width).flex_none().border_l_1().border_color(theme.hairline)).h_full().flex().flex_col().relative().bg(theme.paper).font_family("Inter")
            .text_color(theme.text)
            .on_mouse_down(MouseButton::Left, cx.listener(|_, _, _, cx| { if backend_ui::close_menu() { cx.notify(); } }))
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
            .capture_action(cx.listener(|this, _: &gpui_component::input::Escape, window, cx| {
                cx.stop_propagation();
                if this.picker_token(cx).is_some() { this.picker_dismissed = true; }
                else if backend_ui::close_menu() {} else if this.history { this.history = false; } else { super::chat::chat_dictation::stop_for_input(&this.input); this.open = false; this.fullscreen = false; window.refresh(); }
                cx.notify();
            }))
            .when(!self.fullscreen, |panel| panel.child(self.resize_handle(cx)))
            .child(header)
            .child(div().id("ai-chat-scroll").flex_1().min_h_0().overflow_y_scroll().track_scroll(&self.scroll)
                .on_scroll(|_, _, cx| crate::dropdown::dismiss_all(cx))
                .child(div().w_full().when(self.fullscreen, |inner| inner.max_w(px(720.)).mx_auto()).child(body)
                    .child(div().px(px(16.)).pb(px(16.)).children(approval)).children(error)))
            .children(draft_recovery)
            .children(picker)
            .child(div().flex_none().relative().px(px(16.)).pt(px(if picker_open { 0. } else { 12. })).pb(px(16.)).border_t_1().border_color(theme.hairline)
                .child(div().w_full().when(self.fullscreen, |inner| inner.max_w(px(720.)).mx_auto()).relative().child(composer)
                    .children(super::chat_deck::view(self, window, cx))))
            .children(history.map(|h| div().absolute().top(px(52.)).left_0().right_0().child(h)))
    }
}
