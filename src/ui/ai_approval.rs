use std::{cell::RefCell, rc::Rc};

use gpui::{App, CursorStyle, FontWeight, IntoElement, MouseButton, Window, div, prelude::*, px};
use crate::{editor::ai::backend::ApprovalResponse, theme::tr, ui::{ai_theme, smoke_overlay}};

#[derive(Clone, Copy, PartialEq, Eq)]
enum Choice { Allow, Deny }

#[derive(Default)]
pub(crate) struct ApprovalState {
    choice: Option<Choice>,
    submitted: bool,
}

fn option(id: &'static str, label: &'static str, choice: Choice, state: Rc<RefCell<ApprovalState>>) -> impl IntoElement {
    let theme = ai_theme::theme();
    let selected = state.borrow().choice == Some(choice);
    let selected_probe = match choice {
        Choice::Allow => "ai-approval-selected-allow",
        Choice::Deny => "ai-approval-selected-deny",
    };
    div().id(id).relative().children(smoke_overlay::probe(id))
        .when(selected, |row| row.children(smoke_overlay::probe(selected_probe)))
        .w_full().min_h(px(28.)).px(px(4.)).flex().items_center().gap(px(6.))
        .rounded(px(6.)).cursor(CursorStyle::PointingHand).hover(move |style| style.bg(theme.hover))
        .child(div().size(px(16.)).flex_none().rounded_full().border_1()
            .border_color(if selected { theme.text } else { theme.btn_border })
            .when(selected, |radio| radio.bg(theme.text))
            .flex().items_center().justify_center()
            .when(selected, |radio| radio.child(div().size(px(6.)).rounded_full().bg(theme.paper))))
        .child(div().text_size(px(13.)).text_color(if selected { theme.text } else { theme.text_secondary }).child(label))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            cx.stop_propagation();
            let mut state = state.borrow_mut();
            if !state.submitted { state.choice = Some(choice); window.refresh(); }
        })
}

pub(crate) fn approval_card(state: Rc<RefCell<ApprovalState>>, title: String, detail: String,
    on_response: impl Fn(ApprovalResponse, &mut Window, &mut App) -> bool + 'static) -> impl IntoElement {
    let theme = ai_theme::theme();
    let enabled = state.borrow().choice.is_some() && !state.borrow().submitted;
    let response = Rc::new(on_response);
    let skip_response = response.clone();
    let skip_state = state.clone();
    let continue_state = state.clone();
    let continue_button = div().id("ai-approval-continue").relative()
        .children(smoke_overlay::probe("ai-approval-continue"))
        .when(!enabled, |button| button.children(smoke_overlay::probe("ai-approval-continue-disabled")))
        .h(px(27.)).px(px(12.)).rounded_full().flex().items_center().justify_center()
        .text_size(px(13.)).font_weight(FontWeight(500.))
        .bg(theme.accent).text_color(gpui::rgb(0xffffff)).when(!enabled, |button| button.opacity(0.5))
        .when(enabled, |button| button.cursor(CursorStyle::PointingHand).hover(|style| style.opacity(0.8)))
        .child(tr("继续", "Continue"))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            cx.stop_propagation();
            let choice = {
                let mut state = continue_state.borrow_mut();
                if state.submitted { None } else {
                    let choice = state.choice;
                    if choice.is_some() { state.submitted = true; }
                    choice
                }
            };
            if let Some(choice) = choice {
                if !response(match choice { Choice::Allow => ApprovalResponse::AllowOnce, Choice::Deny => ApprovalResponse::Deny }, window, cx) {
                    continue_state.borrow_mut().submitted = false;
                    window.refresh();
                }
            }
        });
    let skip = div().id("ai-approval-skip").relative().children(smoke_overlay::probe("ai-approval-skip"))
        .h(px(27.)).px(px(12.)).rounded_full().flex().items_center().justify_center()
        .text_size(px(13.)).text_color(theme.text).bg(theme.selected).cursor(CursorStyle::PointingHand)
        .hover(move |style| style.bg(theme.btn_border)).child(tr("跳过", "Skip"))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            cx.stop_propagation();
            let submit = {
                let mut state = skip_state.borrow_mut();
                if state.submitted { return; }
                state.submitted = true;
                true
            };
            if submit && !skip_response(ApprovalResponse::Deny, window, cx) {
                skip_state.borrow_mut().submitted = false;
                window.refresh();
            }
        });
    let card = div().relative().children(smoke_overlay::probe("ai-approval-card"))
        .w_full().max_w(px(320.)).rounded(px(10.)).border_1().border_color(theme.hairline).bg(theme.paper)
        .text_color(theme.text).flex().flex_col()
        .child(div().p(px(12.)).flex().flex_col()
            .child(div().text_size(px(14.)).font_weight(FontWeight(500.)).child(title))
            .when(!detail.is_empty(), |body| body.child(div().mt(px(6.)).text_size(px(14.)).text_color(theme.text_secondary).child(detail)))
            .child(div().mt(px(10.)).flex().flex_col().gap(px(4.))
                .child(option("ai-approval-option-allow", tr("允许一次", "Allow once"), Choice::Allow, state.clone()))
                .child(option("ai-approval-option-deny", tr("拒绝", "Deny"), Choice::Deny, state))))
        .child(div().p(px(10.)).flex().items_center().justify_between()
            .child(div().flex().items_center().gap(px(4.)).text_size(px(12.)).text_color(theme.text_secondary)
                .child(div().opacity(0.3).child(crate::icons::icon(crate::icons::Icon::ChevronUp, px(14.), theme.mono_icon_colors(theme.text_secondary))))
                .child("1 / 1")
                .child(div().opacity(0.3).child(crate::icons::icon(crate::icons::Icon::ChevronDown, px(14.), theme.mono_icon_colors(theme.text_secondary)))))
            .child(div().flex().items_center().gap(px(6.)).child(skip).child(continue_button)));
    crate::ui::motion::fade_up(card, "ai-approval-enter")
}
