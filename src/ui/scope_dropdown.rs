use gpui::{
    AnyElement, CursorStyle, Div, ElementId, SharedString, Stateful, div, prelude::*, px,
};
use crate::icons::{Icon, IconColors, icon};
use crate::theme::Theme;

pub fn trigger_capsule(id: impl Into<ElementId>, label: impl Into<SharedString>, open: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    let line = if open { theme.accent } else { theme.text_secondary };
    div().id(id).relative().h(px(30.)).px(px(10.)).rounded(crate::ui::radius::CONTROL)
        .flex().items_center().gap(px(6.)).cursor(CursorStyle::PointingHand)
        .when(open, |row| row.bg(theme.selected).border_1().border_color(theme.accent))
        .when(!open, |row| row.bg(theme.sidebar).border_1().border_color(theme.btn_border)
            .hover(move |row| row.bg(theme.hover).border_color(theme.accent)))
        .child(icon(Icon::Folder, px(15.), IconColors { line, fill: theme.icon_fill, accent: theme.accent }))
        .child(div().max_w(px(136.)).overflow_hidden().whitespace_nowrap().text_ellipsis()
            .text_size(px(14.)).line_height(px(20.)).text_color(theme.text).child(label.into()))
        .child(icon(Icon::ChevronDown, px(13.), theme.mono_icon_colors(line)))
        .when_some(crate::ui::smoke_overlay::probe("search-scope-trigger"), |row, probe| row.child(probe))
}

pub fn menu_popover(id: impl Into<ElementId>, rows: Vec<AnyElement>) -> impl gpui::IntoElement {
    let menu = crate::ui::dropdown::shell_unbounded(id).w(px(236.)).children(rows);
    gpui::deferred(
        crate::ui::motion::popup_enter(
            div().absolute().top(px(48.)).right(px(16.)).w(px(236.)).child(menu),
            "search-scope-popover",
        ),
    )
    .with_priority(10)
}
