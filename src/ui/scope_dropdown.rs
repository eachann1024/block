use gpui::{
    AnyElement, CursorStyle, Div, ElementId, SharedString, Stateful, div, prelude::*, px,
};
use crate::icons::{Icon, IconColors, icon};
use crate::theme::Theme;

thread_local! {
    static TRIGGER_BOUNDS: std::rc::Rc<std::cell::Cell<Option<gpui::Bounds<gpui::Pixels>>>> = Default::default();
}

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
        .child(crate::ui::placement::track_bounds(TRIGGER_BOUNDS.with(Clone::clone), open))
        .when_some(crate::ui::smoke_overlay::probe("search-scope-trigger"), |row, probe| row.child(probe))
}

pub fn menu_popover(id: impl Into<ElementId>, rows: Vec<AnyElement>, phase: crate::ui::motion::PopupMotion, pop_gen: u64) -> Option<gpui::AnyElement> {
    let tracked = TRIGGER_BOUNDS.with(Clone::clone);
    let known = tracked.get()?;
    let menu = crate::ui::dropdown::shell_unbounded(id).w(px(236.)).children(rows);
    Some(crate::ui::dropdown::anchored_motion_with(("search-scope-popover", pop_gen), move |_, _| {
        crate::ui::dropdown::PlacementSpec::below(tracked.get().unwrap_or(known))
            .align_x(crate::ui::dropdown::AlignX::Right)
    }, menu, phase, 10))
}
