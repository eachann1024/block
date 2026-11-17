use gpui::{CursorStyle, Div, ElementId, Hsla, IntoElement, MouseButton, SharedString, Stateful, div, prelude::*, px, rgb, rgba};
use crate::{
    icons::{Icon, icon, stateful_icon},
    theme::{Theme, is_dark, tooltip, tr},
    ui::{
        annotation_card::{PressFn, spinner},
        icon_size, radius, smoke_overlay,
        tooltip::TooltipExt,
        type_scale,
    },
};

/// Card container with semantic status indicator and optional floating shadow.
pub fn rail_note(id: impl Into<ElementId>, status: u32, floating: bool, content: impl IntoElement) -> Stateful<Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .w_full()
        .px(px(10.))
        .py(px(6.))
        .rounded(px(10.))
        .bg(theme.menu)
        .border_1()
        .border_color(theme.accent)
        .when(floating, |d| d.shadow(crate::ui::annotation_card::floating_shadows()))
        .flex()
        .items_center()
        .gap(px(8.))
        .child(div().size(px(8.)).flex_none().rounded_full().bg(rgb(status)))
        .child(div().flex_1().min_w_0().child(content))
}

/// Emoji reaction pill (`🤔 1`, `❤️ 1`) shared by the wide right rail and narrow block-foot strip.
pub fn reaction_pill(id: impl Into<ElementId>, emoji: &str, active: bool) -> Stateful<Div> {
    let dark = is_dark();
    let (fg, bg, hover_bg, border): (Hsla, Hsla, Hsla, Hsla) = if dark {
        (
            rgb(0x9ec3ff).into(),
            if active { rgb(0x283d63).into() } else { rgb(0x212e47).into() },
            rgb(0x2d4673).into(),
            if active { rgb(0x6f97e6).into() } else { rgb(0x33486e).into() },
        )
    } else {
        (
            rgb(0x1757ac).into(),
            if active { rgb(0xdceafb).into() } else { rgb(0xeaf2fd).into() },
            rgb(0xd4e5fb).into(),
            if active { rgb(0x8cb3ec).into() } else { rgb(0xcfe0f8).into() },
        )
    };
    div()
        .id(id)
        .h(px(24.))
        .px(px(9.))
        .gap(px(4.))
        .rounded_full()
        .bg(bg)
        .border_1()
        .border_color(border)
        .text_color(fg)
        .hover(move |s| s.bg(hover_bg))
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .child(div().text_size(type_scale::BODY).line_height(px(16.)).child(emoji.to_owned()))
        .child(div().text_size(type_scale::BODY).line_height(px(16.)).font_weight(type_scale::CONTROL).child("1"))
}

/// `+` quick-reaction trigger pill placed beside inline reaction pills in narrow mode.
pub fn reaction_add_button(id: impl Into<ElementId>, active: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .h(px(24.))
        .px(px(8.))
        .rounded_full()
        .border_1()
        .border_color(if active { theme.accent } else { theme.btn_border })
        .bg(if active { theme.selected } else { theme.sidebar })
        .text_color(theme.text_secondary)
        .hover(move |s| s.bg(theme.hover).text_color(theme.text))
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .text_size(type_scale::BODY)
        .line_height(px(16.))
        .tooltip_above(tooltip(tr("切换表情回应", "Switch reaction")))
        .child("+")
}

/// Inline horizontal container for switching reactions inside a narrow block-foot strip.
pub fn inline_reaction_bar(id: impl Into<ElementId>) -> Stateful<Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .px(px(6.))
        .py(px(2.))
        .rounded(radius::CONTROL)
        .bg(theme.menu)
        .border_1()
        .border_color(theme.hairline)
        .flex()
        .items_center()
        .gap(px(2.))
}

/// Emoji picker button inside the reaction popover or inline strip, highlighting the active reaction.
pub fn reaction_picker_item(id: impl Into<ElementId>, emoji: &str, selected: bool) -> Stateful<Div> {
    let dark = is_dark();
    let (sel_bg, sel_border, sel_fg) = if dark {
        (rgba(0x4c8bf526), rgba(0x8ab4f84d), rgb(0x8ab4f8))
    } else {
        (rgba(0x257de01a), rgba(0x1757ac4d), rgb(0x1757ac))
    };
    let hover_bg = if selected { Hsla::from(sel_bg) } else { Theme::ocean().hover };
    div()
        .id(id)
        .size(px(28.))
        .rounded(radius::CONTROL)
        .flex()
        .items_center()
        .justify_center()
        .text_size(px(15.))
        .cursor(CursorStyle::PointingHand)
        .when(selected, |s| s.bg(sel_bg).border_1().border_color(sel_border).text_color(sel_fg))
        .hover(move |s| s.bg(hover_bg))
        .child(emoji.to_owned())
}

fn single_line_preview(text: &str) -> SharedString {
    let joined = text
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty())
        .collect::<Vec<_>>()
        .join(" ");
    SharedString::from(if joined.is_empty() { text.trim().to_owned() } else { joined })
}

fn chip_ghost_icon(id: impl Into<ElementId>, glyph: Icon, tip: &'static str, handler: PressFn) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let group = SharedString::from(format!("foot-gi-{id:?}"));
    div()
        .id(id)
        .group(group.clone())
        .size(px(22.))
        .flex_none()
        .rounded(radius::MICRO_MD)
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(theme.hover))
        .tooltip_above(tooltip(tip))
        .on_mouse_down(MouseButton::Left, move |ev, window, cx| handler(ev, window, cx))
        .child(stateful_icon(
            group,
            glyph,
            px(icon_size::INLINE),
            false,
            theme.mono_icon_colors(theme.text_secondary),
            theme.icon_colors(theme.text),
        ))
}

fn chip_accept_icon(id: impl Into<ElementId>, tip: &'static str, handler: PressFn) -> Stateful<Div> {
    let theme = Theme::ocean();
    let hover_bg: Hsla = if is_dark() { rgb(0x6389d6).into() } else { rgb(0x3d63b0).into() };
    div()
        .id(id)
        .size(px(22.))
        .flex_none()
        .rounded(radius::MICRO_MD)
        .bg(theme.accent)
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .tooltip_above(tooltip(tip))
        .on_mouse_down(MouseButton::Left, move |ev, window, cx| handler(ev, window, cx))
        .child(icon(Icon::Check, px(13.), theme.mono_icon_colors(rgb(0xffffff).into())))
}

pub enum FootChipKind {
    Draft {
        body: SharedString,
        on_toggle: PressFn,
        on_edit: PressFn,
        on_delete: PressFn,
    },
    Generating {
        body: SharedString,
        on_toggle: PressFn,
    },
    Review {
        rewritten: SharedString,
        expanded: bool,
        on_toggle: PressFn,
        on_accept: PressFn,
    },
}

/// Compact 28px Ocean Paper inline annotation capsule anchored at the block foot in narrow mode (Scheme B).
pub fn foot_note_chip(
    id: impl Into<ElementId>,
    raw_id: u64,
    highlighted: bool,
    kind: FootChipKind,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    match kind {
        FootChipKind::Draft { body, on_toggle, on_edit, on_delete } => div()
            .id(id)
            .min_h(px(28.))
            .max_w(px(460.))
            .pl(px(10.))
            .pr(px(4.))
            .py(px(2.))
            .rounded(radius::CONTROL)
            .bg(theme.sidebar)
            .border_1()
            .border_color(if highlighted { theme.accent } else { theme.hairline })
            .flex()
            .items_center()
            .gap(px(6.))
            .child(
                div()
                    .id(("foot-draft-toggle", raw_id))
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .items_center()
                    .gap(px(7.))
                    .cursor(CursorStyle::PointingHand)
                    .on_mouse_down(MouseButton::Left, move |ev, w, cx| on_toggle(ev, w, cx))
                    .child(div().size(px(7.)).flex_none().rounded_full().bg(rgb(0xf5c84c)))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .truncate()
                            .text_size(type_scale::BODY)
                            .line_height(px(20.))
                            .text_color(theme.text)
                            .child(single_line_preview(&body)),
                    ),
            )
            .child(
                div()
                    .flex_none()
                    .flex()
                    .items_center()
                    .gap(px(2.))
                    .child(
                        chip_ghost_icon(("foot-edit", raw_id), Icon::Pencil, tr("编辑批注", "Edit annotation"), on_edit)
                            .children(smoke_overlay::probe(format!("annotation-edit-{raw_id}"))),
                    )
                    .child(
                        chip_ghost_icon(("foot-del", raw_id), Icon::Trash, tr("删除批注", "Delete annotation"), on_delete)
                            .children(smoke_overlay::probe(format!("annotation-delete-{raw_id}"))),
                    ),
            ),
        FootChipKind::Generating { body, on_toggle } => div()
            .id(id)
            .min_h(px(28.))
            .max_w(px(460.))
            .px(px(10.))
            .py(px(3.))
            .rounded(radius::CONTROL)
            .bg(theme.sidebar)
            .border_1()
            .border_color(theme.accent)
            .flex()
            .items_center()
            .gap(px(7.))
            .cursor(CursorStyle::PointingHand)
            .on_mouse_down(MouseButton::Left, move |ev, w, cx| on_toggle(ev, w, cx))
            .child(spinner(("foot-spin", raw_id), theme.accent))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .text_size(type_scale::BODY)
                    .line_height(px(20.))
                    .text_color(theme.text_secondary)
                    .child(single_line_preview(&body)),
            ),
        FootChipKind::Review { rewritten, expanded, on_toggle, on_accept } => {
            let soft_border: Hsla = if is_dark() { rgb(0x4a6ba3).into() } else { rgb(0xb7cef2).into() };
            let summary = format!("{}{}", tr("建议改写：", "Suggested: "), single_line_preview(&rewritten));
            div()
                .id(id)
                .min_h(px(28.))
                .max_w(px(460.))
                .pl(px(10.))
                .pr(px(if expanded { 10. } else { 4. }))
                .py(px(2.))
                .rounded(radius::CONTROL)
                .bg(theme.selected)
                .border_1()
                .border_color(if highlighted || expanded { theme.accent } else { soft_border })
                .flex()
                .items_center()
                .gap(px(6.))
                .child(
                    div()
                        .id(("foot-review-toggle", raw_id))
                        .flex_1()
                        .min_w_0()
                        .flex()
                        .items_center()
                        .gap(px(6.))
                        .cursor(CursorStyle::PointingHand)
                        .on_mouse_down(MouseButton::Left, move |ev, w, cx| on_toggle(ev, w, cx))
                        .child(icon(Icon::TbComment, px(icon_size::INLINE), theme.icon_colors(theme.accent)))
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .truncate()
                                .text_size(type_scale::BODY)
                                .line_height(px(20.))
                                .font_weight(type_scale::CONTROL)
                                .text_color(theme.text)
                                .child(summary),
                        ),
                )
                .when(!expanded, |row| {
                    row.child(
                        chip_accept_icon(("foot-accept", raw_id), tr("同意，替换原文", "Accept and replace"), on_accept)
                            .children(smoke_overlay::probe(format!("annotation-accept-{raw_id}"))),
                    )
                })
        }
    }
}
