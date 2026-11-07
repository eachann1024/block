//! Ocean Paper view for the Feedback modal (`#feedback-overlay` / `#feedback-dialog`).
use gpui::{
    AnyElement, App, Context, Focusable, FontWeight, InteractiveElement, IntoElement, MouseButton,
    ParentElement, Render, SharedString, Styled, Window, div, px, rgb,
};
use gpui_component::input::{Escape as InputEscape, Input, Textarea};
use crate::feedback::{
    FeedbackCategory, FeedbackModal, close, ensure_modal, is_open, open_seq,
};
use crate::theme::{Theme, is_en, tr};
use crate::ui::feedback_controls::{feedback_chip, feedback_done_badge, feedback_send_button};
use crate::ui::modal::{modal_close_button, modal_secondary_button};
use crate::ui::motion::modal_enter;
use crate::ui::radius;

impl Render for FeedbackModal {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if self.last_en != is_en() { self.sync_placeholders(window, cx); }
        let theme = Theme::ocean();
        let done = self.done;
        let body = if self.done {
            self.render_done(&theme).into_any_element()
        } else {
            self.render_form(&theme, window, cx).into_any_element()
        };
        let header = (!done).then(|| {
            let close_btn = modal_close_button("feedback-close", tr("关闭", "Close"), |_, window, cx| {
                let _ = close(window, cx);
            });
            div().id("feedback-header").relative().children(crate::ui::smoke_overlay::probe("feedback-header")).flex().items_start().justify_between().gap(px(12.)).child(
                div().flex_1().min_w_0().flex().flex_col().gap(px(4.))
                    .child(div().text_size(px(20.)).line_height(px(26.)).font_weight(FontWeight(650.)).text_color(theme.text).child(tr("给我们带句话", "Send us a note")))
                    .child(div().w_full().whitespace_normal().text_size(px(14.)).line_height(px(21.)).text_color(theme.text_secondary).child(tr(
                        "用得顺不顺手、哪里可以做得更好，或者遇到了问题、有什么想法，都可以跟我们聊聊。",
                        "Tell us what's working well, what could be better, or anything that got in your way.",
                    ))),
            ).child(close_btn.flex_shrink_0())
        });

        let card = div().id("feedback-dialog").key_context("Workspace").track_focus(&self.focus)
            .w(if done { px(400.) } else { px(500.) }).max_w(px(500.)).rounded(radius::SURFACE).bg(theme.paper)
            .border_1().border_color(theme.menu_border).shadow_lg().p(px(24.)).flex().flex_col().gap(px(18.))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, w, cx| {
                cx.stop_propagation();
                if !this.message.read(cx).focus_handle(cx).is_focused(w) && !this.contact.read(cx).focus_handle(cx).is_focused(w) {
                    w.focus(&this.focus, cx);
                }
            }))
            .capture_action(cx.listener(|_, _: &InputEscape, w, cx| if close(w, cx) { cx.stop_propagation(); }))
            .capture_action(cx.listener(|_, _: &crate::editor::view::Cancel, w, cx| if close(w, cx) { cx.stop_propagation(); }))
            .capture_action(cx.listener(|_, _: &crate::app::Dismiss, w, cx| if close(w, cx) { cx.stop_propagation(); }))
            .children(header)
            .child(body);
        modal_enter(card, SharedString::from(format!("feedback-card-{}", open_seq())))
    }
}

impl FeedbackModal {
    fn render_form(&mut self, theme: &Theme, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let active_cat = self.category;
        let seq = self.chip_seq;
        let entity = cx.entity();

        let cat_row = FeedbackCategory::ALL.into_iter().fold(div().flex().flex_wrap().gap(px(8.)), |row, cat| {
            let ent = entity.clone();
            let id = SharedString::from(format!("feedback-cat-{}", cat.code()));
            row.child(feedback_chip(id, cat.label(), cat == active_cat, seq, move |w, cx| {
                ent.update(cx, |m, cx| m.select_category(cat, w, cx));
            }))
        });

        let msg_focused = self.message.read(cx).focus_handle(cx).is_focused(window);
        let contact_focused = self.contact.read(cx).focus_handle(cx).is_focused(window);
        let has_draft_text = !self.message.read(cx).value().trim().is_empty()
            || !self.contact.read(cx).value().trim().is_empty();

        let msg_box = div().id("feedback-message-box").relative().children(crate::ui::smoke_overlay::probe("feedback-message-box"))
            .min_h(px(116.)).px(px(10.)).py(px(8.)).rounded(px(8.))
            .border_1().border_color(if msg_focused { theme.accent } else { theme.btn_border })
            .bg(theme.sidebar).text_size(px(14.)).line_height(px(22.)).text_color(theme.text)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, w, cx| this.message.update(cx, |m, cx| m.focus(w, cx))))
            .child(Textarea::new(&self.message).appearance(false).bordered(false));

        let contact_box = div().id("feedback-contact-box").relative().children(crate::ui::smoke_overlay::probe("feedback-contact-box"))
            .h(px(38.)).px(px(10.)).rounded(px(8.))
            .border_1().border_color(if contact_focused { theme.accent } else { theme.btn_border })
            .bg(theme.sidebar).flex().items_center().text_size(px(14.)).text_color(theme.text)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, w, cx| this.contact.update(cx, |c, cx| c.focus(w, cx))))
            .child(div().flex_1().child(Input::new(&self.contact).appearance(false).bordered(false).focus_bordered(false).h(px(32.))));

        let status = if let Some(err) = self.error.clone() {
            div().id("feedback-status").text_size(px(14.)).text_color(rgb(0xc4514b)).child(err)
        } else if has_draft_text {
            div().id("feedback-status").text_size(px(14.)).text_color(theme.text_secondary)
                .child(tr("草稿已自动保存，误关也不会丢失", "Draft auto-saved"))
        } else {
            div().id("feedback-status")
        };

        let send_ent = entity;
        let send_label = if self.sending { tr("发送中…", "Sending…") } else { tr("发送", "Send") };
        let send_btn = feedback_send_button("feedback-send", send_label, self.sending, move |w, cx| {
            send_ent.update(cx, |m, cx| m.submit(w, cx));
        });

        let email_head = div().flex().items_center().gap(px(6.)).text_size(px(14.))
            .child(div().font_weight(FontWeight(600.)).text_color(theme.text).child(tr("回复邮箱", "Reply email")))
            .child(div().text_color(theme.text_secondary).child(tr("（选填）", "(optional)")));

        div().flex().flex_col().gap(px(16.))
            .child(field_section(theme, tr("属于哪一类？", "What is this about?"), cat_row))
            .child(field_section(theme, tr("具体说说", "Tell us more"), msg_box))
            .child(div().flex().flex_col().gap(px(8.)).child(email_head).child(contact_box))
            .child(div().pt(px(2.)).flex().items_center().justify_between().gap(px(12.))
                .child(div().flex_1().min_w_0().child(status))
                .child(send_btn))
    }

    fn render_done(&self, theme: &Theme) -> impl IntoElement {
        let note = if self.sent_contact.is_empty() {
            tr("我们会认真看每一条反馈。", "We read every piece of feedback carefully.").to_string()
        } else if is_en() {
            format!("We read every note and will reply to {}.", self.sent_contact)
        } else {
            format!("我们会认真看，有进展会发到 {}。", self.sent_contact)
        };
        div().id("feedback-done").relative().children(crate::ui::smoke_overlay::probe("feedback-done")).py(px(8.)).flex().flex_col().items_center().gap(px(12.))
            .child(feedback_done_badge(self.done_seq))
            .child(div().text_size(px(18.)).font_weight(FontWeight(650.)).text_color(theme.text).child(tr("收到了，谢谢你", "Got it, thank you")))
            .child(div().text_size(px(14.)).line_height(px(22.)).text_color(theme.text_secondary).child(note))
            .child(div().pt(px(6.)).w(px(96.)).child(modal_secondary_button("feedback-done-close", tr("好的", "Done"), |_, w, cx| { let _ = close(w, cx); })))
    }
}

fn field_section(theme: &Theme, title: &'static str, body: impl IntoElement) -> impl IntoElement {
    div().flex().flex_col().gap(px(8.))
        .child(div().text_size(px(14.)).font_weight(FontWeight(600.)).text_color(theme.text).child(title))
        .child(body)
}

pub fn modal_overlay(window: &mut Window, cx: &mut App) -> Option<AnyElement> {
    if !is_open() { return None; }
    let entity = ensure_modal(window, cx);
    Some(
        div().id("feedback-overlay").absolute().inset_0().bg(gpui::rgba(0x20242c2e)).flex().items_center().justify_center()
            .on_mouse_down(MouseButton::Left, |_, w, cx| { cx.stop_propagation(); let _ = close(w, cx); })
            .child(entity)
            .into_any_element(),
    )
}
