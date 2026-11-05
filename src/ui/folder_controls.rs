//! Common UI components for folder settings: affixed slash input, folder table row,
//! and folder visibility segmented switch (Design A).
use gpui::{
    App, Context, FontWeight, IntoElement, MouseDownEvent,
    Window, div, prelude::*, px,
};
use crate::theme::{Theme, tr};
use super::settings_controls::{card_bg, mono_font, segmented};

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
                        .focus_bordered(false),
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
        .min_h(px(46.))
        .px(px(14.))
        .py(px(8.))
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
) -> impl FnOnce(&mut Context<V>) -> gpui::Stateful<gpui::Div> {
    segmented(
        id,
        [
            (tr("隐藏", "Hidden"), is_hidden, true),
            (tr("显示", "Show"), !is_hidden, false),
        ],
        on_toggle,
    )
}
