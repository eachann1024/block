//! Common UI components for folder settings: affixed slash input, folder table row,
//! and folder visibility segmented switch (Design A).
use gpui::{
    App, ClickEvent, Context, FontWeight, IntoElement, MouseDownEvent,
    Window, div, prelude::*, px,
};
use gpui_base::Checkbox;
use crate::theme::{Theme, tr};
use super::settings_controls::{card_bg, mono_font, segmented, selected_bg};

pub fn folder_check_box(checked: bool) -> gpui::Div {
    let theme = Theme::ocean();
    div().size(px(15.)).rounded(px(4.)).flex_none().flex().items_center().justify_center()
        .bg(card_bg()).border_1().border_color(if checked { theme.accent } else { theme.btn_border })
        .when(checked, |b| b.child(crate::icons::icon(crate::icons::Icon::Check, px(11.), theme.icon_colors(theme.accent))))
}

pub fn folder_check_pill(
    id: impl Into<gpui::ElementId>,
    probe_id: Option<&'static str>,
    checked: bool,
    label: &'static str,
    on_toggle: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    Checkbox::new(id).checked(checked).accessibility_label(label)
        .relative().h(px(32.)).px(px(10.))
        .rounded(crate::ui::radius::CONTROL).flex().items_center().gap(px(8.))
        .cursor(gpui::CursorStyle::PointingHand)
        .when_some(probe_id, |d, pid| d.children(crate::ui::smoke_overlay::probe(pid)))
        .bg(if checked { selected_bg() } else { theme.shell })
        .border_1().border_color(if checked { theme.accent } else { theme.hairline })
        .text_size(px(14.)).line_height(px(20.)).font_weight(FontWeight(460.))
        .text_color(if checked { theme.text } else { theme.text_secondary })
        .hover(move |s| s.border_color(theme.accent).text_color(theme.text))
        .on_change(move |_state, ev, window, cx| on_toggle(ev, window, cx))
        .child(folder_check_box(checked))
        .child(div().child(label))
}

pub fn folder_check_row(
    id: impl Into<gpui::ElementId>,
    probe_id: Option<&'static str>,
    checked: bool,
    glyph: crate::icons::Icon,
    label: &'static str,
    on_toggle: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let icon_colors = if checked { theme.icon_colors(theme.accent) } else { theme.icon_colors(theme.text) };
    let row_bg = if checked { selected_bg() } else { theme.shell };
    Checkbox::new(id).checked(checked).accessibility_label(label)
        .relative().w_full().min_h(px(46.)).px(px(14.)).py(px(8.))
        .rounded(crate::ui::radius::SURFACE)
        .bg(row_bg).border_1().border_color(if checked { theme.accent } else { theme.hairline })
        .flex().items_center().justify_between().gap(px(12.))
        .cursor(gpui::CursorStyle::PointingHand)
        .when_some(probe_id, |d, pid| d.children(crate::ui::smoke_overlay::probe(pid)))
        .hover(move |s| s.border_color(theme.accent))
        .on_change(move |_state, ev, window, cx| on_toggle(ev, window, cx))
        .child(
            div().flex().items_center().gap(px(10.)).flex_1().min_w_0()
                .child(crate::icons::icon(glyph, px(16.), icon_colors))
                .child(div().text_size(px(14.)).font_weight(FontWeight(460.)).text_color(theme.text).child(label)),
        )
        .child(folder_check_box(checked))
}

pub fn folder_search_ignore_pill(
    id: impl Into<gpui::ElementId>,
    probe_id: Option<&'static str>,
    ignored: bool,
    on_toggle: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    folder_check_pill(id, probe_id, ignored, tr("不参与全局搜索", "Exclude from Global Search"), on_toggle)
}

/// Folder name field that fills the space available beside fixed-width actions.
pub fn folder_name_input(
    id: impl Into<gpui::ElementId>,
    input: &gpui::Entity<gpui_component::input::InputState>,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id(id).flex_1().min_w_0().h(px(34.)).px(px(10.))
        .rounded(crate::ui::radius::CONTROL)
        .bg(theme.shell).border_1().border_color(theme.btn_border)
        .flex().items_center().text_size(px(14.))
        .child(gpui_component::input::Input::new(input)
            .appearance(false).bordered(false).focus_bordered(false)
            .w_full().min_w_0().h(px(32.)).px(px(0.)))
}

/// Text input container with fixed non-editable `/` prefix and `/` suffix affixes.
pub fn slash_affix_input(
    id: impl Into<gpui::ElementId>,
    input: &gpui::Entity<gpui_component::input::InputState>,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .h(px(34.))
        .flex_1()
        .min_w_0()
        .flex()
        .items_center()
        .px(px(10.))
        .bg(theme.shell)
        .border_1()
        .border_color(theme.btn_border)
        .rounded(crate::ui::radius::CONTROL)
        .child(
            div()
                .font(mono_font())
                .text_size(px(14.))
                .line_height(px(20.))
                .font_weight(FontWeight::MEDIUM)
                .text_color(theme.text_secondary)
                .pr(px(4.))
                .child("/"),
        )
        .child(
            div()
                .flex_1()
                .min_w_0()
                .h_full()
                .flex()
                .items_center()
                .font(mono_font())
                .text_size(px(14.))
                .child(
                    gpui_component::input::Input::new(input)
                        .appearance(false)
                        .bordered(false)
                        .focus_bordered(false)
                        .w_full()
                        .min_w_0()
                        .h(px(32.))
                        .px(px(0.)),
                ),
        )
        .child(
            div()
                .font(mono_font())
                .text_size(px(14.))
                .line_height(px(20.))
                .font_weight(FontWeight::MEDIUM)
                .text_color(theme.text_secondary)
                .pl(px(4.))
                .child("/"),
        )
}

/// Unified folder table row with persistent or custom metadata and actions.
pub fn folder_table_row(
    id: impl Into<gpui::ElementId>,
    is_hidden: bool,
    left: impl IntoElement,
    right: impl IntoElement,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let row_bg = if is_hidden { theme.shell } else { card_bg() };
    div()
        .id(id)
        .w_full()
        .min_w_0()
        .min_h(px(46.))
        .px(px(12.))
        .py(px(4.))
        .rounded(crate::ui::radius::SURFACE)
        .bg(row_bg)
        .border_1()
        .border_color(theme.hairline)
        .flex()
        .items_center()
        .justify_between()
        .gap(px(12.))
        .child(left)
        .child(right)
}

/// Segmented switch for folder visibility: `隐藏` / `显示`.
pub fn folder_visibility_segmented<V: 'static>(
    id: impl Into<gpui::ElementId>,
    is_hidden: bool,
    on_toggle: impl Fn(bool, &mut Context<V>) -> Box<dyn Fn(&MouseDownEvent, &mut Window, &mut App) + 'static>,
) -> impl FnOnce(&mut Context<V>) -> gpui::AnyElement {
    segmented(
        id,
        [
            (tr("隐藏", "Hidden"), is_hidden, true),
            (tr("显示", "Show"), !is_hidden, false),
        ],
        on_toggle,
    )
}
