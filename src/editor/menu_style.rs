//! Shared menu look delegating to `crate::dropdown` (white `#ffffff` shell, radius 12, padding 4,
//! 3-layer shadow, 28px rows with 16px leading icon column, danger hover `#d03b3b`).
#![allow(dead_code)]
use gpui::{Div, ElementId, Hsla, Stateful, div, prelude::*, px};
use crate::{dropdown::{self, RowSpec}, theme::Theme};

/// Destructive entries (delete) use `#8e2626` text (`#f28b82` in dark mode).
pub(super) fn danger_color() -> Hsla { dropdown::palette().danger_fg }

/// Menu shell: 12px radius, 4px padding, 3-layer shadow, internal scroll up to 304px.
pub(super) fn shell(id: impl Into<ElementId>, _theme: &Theme) -> Stateful<Div> {
    dropdown::shell(id)
}

/// Blank space between groups.
pub(super) fn group_gap() -> Div { div().h(px(2.)).flex_none() }

/// 1px `rgba(11,11,11,.1)` divider with `my 4`.
pub(super) fn separator(_theme: &Theme) -> Div {
    dropdown::separator()
}

/// One menu row; `active` is the keyboard highlight and `disabled` dims it and drops hover and clicks.
pub(super) fn item(
    id: impl Into<ElementId>,
    label: &'static str,
    danger: bool,
    active: bool,
    disabled: bool,
    _theme: &Theme,
) -> Stateful<Div> {
    dropdown::row_with(
        id,
        RowSpec {
            icon: None,
            label: label.into(),
            checked: false,
            highlighted: active,
            danger,
            disabled,
            hint: None,
            chevron: false,
        },
    )
}
