use gpui::{AnyElement, CursorStyle, ElementId, FontWeight, MouseButton, SharedString, div, prelude::*, px};
use crate::{icons::{icon, Icon}, theme::tr, ui::{radius, smoke_overlay}};

pub(crate) fn pill(id: impl Into<SharedString>, label: String, selected: bool,
    click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = crate::ui::ai_theme::theme(); let id = ElementId::from(id.into());
    div().id(id.clone()).relative().children(smoke_overlay::probe_for(&id)).px(px(10.)).py(px(5.))
        .rounded(radius::CONTROL).border_1().border_color(if selected { theme.accent } else { theme.btn_border })
        .bg(if selected { theme.selected } else { theme.paper }).cursor(CursorStyle::PointingHand)
        .text_size(px(14.)).child(label).on_mouse_down(MouseButton::Left, click)
}

pub(crate) fn followup_action(id: impl Into<SharedString>, label: String,
    click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = crate::ui::ai_theme::theme(); let id = ElementId::from(id.into());
    div().id(id.clone()).relative().children(smoke_overlay::probe_for(&id))
        .flex().items_center().gap(px(8.)).px(px(6.)).py(px(6.)).rounded(px(7.))
        .border_b_1().border_color(theme.hairline).hover(|style| style.bg(theme.hover))
        .cursor(CursorStyle::PointingHand).text_size(px(12.5)).text_color(theme.text)
        .child(icon(Icon::KeyReturn, px(11.), theme.mono_icon_colors(theme.text_secondary)))
        .child(label).on_mouse_down(MouseButton::Left, click)
}

pub(crate) fn chat_plan_card(id: impl Into<SharedString>, title: String, badge: &'static str, note: String, pending: bool,
    open: Option<Box<dyn Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App)>>, actions: Vec<AnyElement>) -> impl IntoElement {
    let theme = crate::ui::ai_theme::theme();
    let id = ElementId::from(id.into());
    div().relative().children(smoke_overlay::probe("ai-plan-card")).flex().flex_col()
        .rounded(px(10.)).border_1().border_color(theme.btn_border).bg(theme.shell)
        .child(div().id(id.clone()).relative().children(smoke_overlay::probe_for(&id)).flex().flex_col().gap(px(8.)).p(px(12.))
          .when(open.is_some(), |header| header.cursor(CursorStyle::PointingHand))
          .when_some(open, |header, click| header.on_mouse_down(MouseButton::Left, move |event, window, cx| click(event, window, cx)))
          .child(div().flex().items_center().gap(px(8.))
            .child(icon(Icon::Sparkles, px(14.), theme.mono_icon_colors(theme.accent)))
            .child(div().flex_1().min_w_0().text_size(px(14.)).font_weight(FontWeight(500.)).child(title)))
          .when(!note.is_empty(), |card| card.child(div().text_size(px(13.)).line_height(px(20.)).text_color(theme.text_secondary).child(note))))
        .child(div().flex().flex_wrap().items_center().justify_between().gap(px(8.)).p(px(10.)).border_t_1().border_color(theme.hairline)
            .child(div().text_size(px(12.5)).text_color(if pending { theme.accent } else { theme.text_secondary }).child(badge))
            .child(div().flex().flex_wrap().gap(px(8.)).children(actions)))
}

pub(crate) fn context_card(sources: Vec<AnyElement>, expanded: bool, toggle: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> impl IntoElement {
    let theme = crate::ui::ai_theme::theme();
    let count = sources.len();
    div().relative().children(smoke_overlay::probe("ai-context-card")).flex().flex_col().gap(px(8.))
        .child(div().id("ai-sources-toggle").relative().children(smoke_overlay::probe("ai-sources-toggle")).cursor(CursorStyle::PointingHand)
            .flex().items_center().gap(px(8.)).text_size(px(13.)).font_weight(FontWeight(600.)).on_mouse_down(MouseButton::Left, toggle)
            .child(tr("参考笔记", "Sources")).child(div().h(px(20.)).px(px(6.)).rounded(px(5.)).bg(theme.hover)
                .flex().items_center().text_size(px(11.5)).text_color(theme.text_secondary).child(count.to_string())))
        .when(expanded, |card| card.children(sources)).text_color(theme.text)
}

pub(crate) fn source_card(title: String, count: usize, excerpt: String, link: AnyElement) -> impl IntoElement {
    let theme = crate::ui::ai_theme::theme();
    div().flex().flex_col().rounded(px(10.)).border_1().border_color(theme.btn_border).bg(theme.shell)
        .child(div().flex().justify_between().gap(px(8.)).p(px(10.)).border_b_1().border_color(theme.hairline)
            .child(div().text_size(px(13.)).font_weight(FontWeight(500.)).child(title))
            .child(div().text_size(px(12.)).text_color(theme.text_secondary).child(format!("{count} {}", tr("字", "chars")))))
        .child(div().flex().flex_col().items_start().gap(px(8.)).p(px(10.))
            .when(!excerpt.is_empty(), |card| card.child(div().text_size(px(12.5)).line_height(px(19.)).text_color(theme.text_secondary).child(excerpt))).child(link))
}

pub(crate) use super::ai_editor_diff::editor_plan_review_card;
