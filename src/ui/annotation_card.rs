//! Unified annotation card component shared by the wide right rail and the narrow-window popover,
//! plus the titlebar batch send button (`统一发送（n）`).
use std::{rc::Rc, time::Duration};
use gpui::{
    Animation, AnimationExt, App, BoxShadow, CursorStyle, Div, ElementId, Entity, FontWeight, Hsla,
    IntoElement, MouseButton, MouseDownEvent, ParentElement, SharedString, Stateful, Styled,
    Transformation, Window, div, linear_color_stop, linear_gradient, percentage, point, prelude::*,
    px, relative, rgb, rgba, svg,
};
use gpui_component::input::{Input, InputState};
use crate::{
    icons::{Icon, icon, stateful_icon},
    theme::{Theme, is_dark, tooltip, tr},
    ui::{annotation_actions::{BatchTone, batch_button}, fade, motion},
};

pub type PressFn = Rc<dyn Fn(&MouseDownEvent, &mut Window, &mut App)>;

pub fn on_press(f: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static) -> PressFn {
    Rc::new(f)
}

fn accent_hover() -> Hsla {
    if is_dark() { rgb(0x6389d6).into() } else { rgb(0x3d63b0).into() }
}

/// Spinning 14×14 arc icon (`0.9s linear infinite`).
pub fn spinner(id: impl Into<ElementId>, color: Hsla) -> impl IntoElement {
    svg().path("icons/spinner.line.svg").size(px(14.)).flex_none().text_color(color).with_animation(
        id,
        Animation::new(Duration::from_millis(900)).repeat(),
        |el, delta| el.with_transformation(Transformation::rotate(percentage(delta))),
    )
}

/// Titlebar split-pill left segment: `统一发送 n` / `生成中…`, styled for the combined action pill.
pub fn send_all_button(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    count: Option<usize>,
    generating: bool,
    enabled: bool,
    on_click: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let active = enabled || generating;
    let fg: Hsla = if active {
        if is_dark() { rgb(0xe6eefc).into() } else { rgb(0x184f95).into() }
    } else {
        theme.text_secondary
    };
    let mut btn = div()
        .id(id)
        .h(px(24.))
        .px(px(9.))
        .gap(px(6.))
        .flex_none()
        .rounded(px(6.))
        .flex()
        .items_center()
        .justify_center()
        .when(active, |s| s.bg(theme.selected))
        .when(!active, |s| s.bg(gpui::transparent_black()).opacity(0.55))
        .text_color(fg)
        .text_size(px(14.))
        .line_height(px(20.))
        .font_weight(FontWeight(460.));

    if generating {
        btn = btn.child(spinner("titlebar-annotate-spin", theme.accent));
    } else {
        btn = btn.child(div().size(px(7.)).flex_none().rounded_full().bg(rgb(0xf5c84c)));
    }

    btn = btn.child(label.into());

    if let Some(n) = count.filter(|&n| n > 0 && !generating) {
        btn = btn.child(
            div()
                .text_color(theme.accent)
                .font_weight(FontWeight(600.))
                .child(SharedString::from(n.to_string())),
        );
    }

    if enabled {
        btn.cursor(CursorStyle::PointingHand)
            .hover(move |s| s.bg(theme.hover))
            .on_mouse_down(MouseButton::Left, on_click)
    } else {
        btn.cursor(CursorStyle::Arrow)
    }
}

/// Unified split pill container for the titlebar right rail (`send_segment` | `more_button`).
pub fn titlebar_split_pill(send_segment: Option<Stateful<Div>>, more_button: Stateful<Div>) -> Stateful<Div> {
    let theme = Theme::ocean();
    if let Some(send_btn) = send_segment {
        div()
            .id("titlebar-action-pill")
            .h(px(28.))
            .p(px(2.))
            .gap(px(2.))
            .flex_none()
            .flex()
            .items_center()
            .rounded(crate::ui::radius::CONTROL)
            .border_1()
            .border_color(theme.btn_border)
            .bg(theme.menu)
            .child(send_btn)
            .child(div().w(px(1.)).h(px(14.)).flex_none().bg(theme.btn_border))
            .child(more_button)
    } else {
        div().id("titlebar-action-pill").flex_none().flex().items_center().child(more_button)
    }
}

/// Subtle 2-layer tight shadow for floating annotation popovers (inline cards stay flat).
pub fn floating_shadows() -> Vec<BoxShadow> {
    let shadow = |color, y, blur| BoxShadow {
        color, offset: point(px(0.), px(y)), blur_radius: px(blur), spread_radius: px(0.), inset: false,
    };
    vec![shadow(rgba(0x0000001f).into(), 1., 2.), shadow(rgba(0x00000014).into(), 2., 8.)]
}

/// Outer shell for an annotation card (`border-radius: 10px`, `padding: 10px 12px`, `gap: 10px`).
pub fn card_shell(id: impl Into<ElementId>, accent_border: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id(id).w_full().px(px(12.)).py(px(10.)).rounded(px(10.)).bg(theme.menu).border_1()
        .border_color(if accent_border { theme.accent } else { theme.hairline })
        .flex().flex_col().gap(px(10.)).text_size(px(14.)).line_height(px(20.)).text_color(theme.text)
}

/// 30×30 bordered icon button (`.ib` / `.ib-ok` in `AnnotateFlow.dc.html`).
fn icon_box_button(
    id: impl Into<ElementId>, glyph: Icon, filled_accent: bool, tip: &'static str, handler: PressFn,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let white: Hsla = rgb(0xffffff).into();
    let fg = if filled_accent { white } else { theme.text_secondary };
    let id: ElementId = id.into();
    let group = SharedString::from(format!("ann-ib-{id:?}"));
    let glyph_el = if filled_accent {
        icon(glyph, px(14.), theme.mono_icon_colors(fg))
    } else {
        stateful_icon(group.clone(), glyph, px(14.), false, theme.mono_icon_colors(fg), theme.icon_colors(theme.text))
    };
    div().id(id).group(group).size(px(30.)).flex_none().rounded(px(6.)).border_1()
        .border_color(if filled_accent { theme.accent } else { theme.btn_border })
        .bg(if filled_accent { theme.accent } else { theme.paper })
        .flex().items_center().justify_center().cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(if filled_accent { accent_hover() } else { theme.hover }))
        .tooltip(tooltip(tip))
        .on_mouse_down(MouseButton::Left, move |ev, window, cx| handler(ev, window, cx))
        .child(glyph_el)
}

/// 26×26 borderless ghost icon button (`.gi` in `AnnotateFlow.dc.html`).
fn ghost_icon_button(id: impl Into<ElementId>, glyph: Icon, tip: &'static str, handler: PressFn) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let group = SharedString::from(format!("ann-gi-{id:?}"));
    div().id(id).group(group.clone()).size(px(26.)).flex_none().rounded(px(6.)).flex().items_center().justify_center()
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover)).tooltip(tooltip(tip))
        .on_mouse_down(MouseButton::Left, move |ev, window, cx| handler(ev, window, cx))
        .child(stateful_icon(group, glyph, px(14.), false, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text)))
}

fn skeleton_bar(id: impl Into<ElementId>, width_ratio: f32) -> impl IntoElement {
    let (base, peak) = if is_dark() { (0x2a2a2a, 0x3b4659) } else { (0xeef0f3, 0xdfe6f3) };
    div().w(relative(width_ratio)).h(px(12.)).rounded(px(4.)).with_animation(
        id,
        Animation::new(Duration::from_millis(1200)).repeat(),
        move |el, delta| {
            let phase = delta * std::f32::consts::TAU;
            let c1 = fade::mix(base, peak, 0.5 - 0.5 * phase.cos());
            let c2 = fade::mix(base, peak, 0.5 - 0.5 * (phase - 1.4).cos());
            el.bg(linear_gradient(90., linear_color_stop(c1, 0.), linear_color_stop(c2, 1.)))
        },
    )
}

fn result_box(id: u64, rewritten: SharedString) -> impl IntoElement {
    let theme = Theme::ocean();
    motion::fade_up(
        div().w_full().p(px(10.)).rounded(px(6.)).bg(theme.selected).text_color(theme.selection_text)
            .text_size(px(14.)).line_height(px(22.)).child(rewritten),
        ("annotation-result-fade", id),
    )
}

pub enum CardState {
    Draft { body: SharedString, on_edit: PressFn, on_delete: PressFn },
    Editing { input: Entity<InputState>, on_cancel: PressFn, on_save: PressFn },
    Generating { body: SharedString },
    Review { rewritten: SharedString, on_accept: PressFn, on_reject: PressFn, on_regen: PressFn, on_delete: PressFn },
    Reject { rewritten: SharedString, input: Entity<InputState>, on_cancel: PressFn, on_submit: PressFn },
    Accepted { on_undo: PressFn },
    Error { message: SharedString, on_retry: PressFn, on_delete: PressFn },
}

pub fn render_card_state(id: u64, state: CardState) -> Div {
    let theme = Theme::ocean();
    let root = div().w_full().flex().flex_col().gap(px(10.));
    match state {
        CardState::Draft { body, on_edit, on_delete } => root.child(
            div().w_full().flex().items_start().gap(px(8.))
                // Text row is padded to the 26px ghost-button height so a single line sits centered.
                .child(div().size(px(8.)).mt(px(9.)).flex_none().rounded_full().bg(rgb(0xf5c84c)))
                .child(div().flex_1().min_w_0().py(px(3.)).text_size(px(14.)).line_height(px(20.)).text_color(theme.text).child(body))
                .child(ghost_icon_button(("ann-edit", id), Icon::Pencil, tr("编辑批注", "Edit annotation"), on_edit).children(crate::ui::smoke_overlay::probe(format!("annotation-edit-{id}"))))
                .child(ghost_icon_button(("ann-del", id), Icon::Trash, tr("删除批注", "Delete annotation"), on_delete).children(crate::ui::smoke_overlay::probe(format!("annotation-delete-{id}")))),
        ),
        CardState::Editing { input, on_cancel, on_save } => root
            .child(
                div().w_full().h(px(32.)).px(px(8.)).rounded(px(6.)).bg(theme.shell).border_1().border_color(theme.accent)
                    .flex().items_center().on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                    .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(30.)).px(px(0.))),
            )
            .child(
                div().w_full().flex().justify_end().gap(px(6.))
                    .child(batch_button(("ann-edit-cancel", id), tr("取消", "Cancel"), BatchTone::Neutral, true, move |ev, w, cx| on_cancel(ev, w, cx)))
                    .child(batch_button(("ann-edit-save", id), tr("保存", "Save"), BatchTone::Accent, true, move |ev, w, cx| on_save(ev, w, cx))),
            ),
        CardState::Generating { body } => {
            let sk_bg = if is_dark() { theme.paper } else { theme.settings_bg };
            root.child(
                div().w_full().flex().items_center().gap(px(8.)).text_color(theme.text_secondary)
                    .child(spinner(("ann-card-spin", id), theme.accent))
                    .child(div().flex_1().min_w_0().text_size(px(14.)).line_height(px(20.)).child(body)),
            )
            .child(
                div().w_full().p(px(10.)).rounded(px(6.)).bg(sk_bg).flex().flex_col().gap(px(8.))
                    .child(skeleton_bar(("ann-sk-1", id), 1.0))
                    .child(skeleton_bar(("ann-sk-2", id), 0.72)),
            )
        }
        CardState::Review { rewritten, on_accept, on_reject, on_regen, on_delete } => root
            .child(result_box(id, rewritten))
            .child(
                div().w_full().flex().items_center().gap(px(6.))
                    .child(icon_box_button(("ann-accept", id), Icon::Check, true, tr("同意，替换原文", "Accept and replace"), on_accept).children(crate::ui::smoke_overlay::probe(format!("annotation-accept-{id}"))))
                    .child(icon_box_button(("ann-reject", id), Icon::Close, false, tr("不满意", "Not satisfied"), on_reject))
                    .child(icon_box_button(("ann-regen", id), Icon::Refresh, false, tr("重新生成", "Regenerate"), on_regen))
                    .child(div().flex_1())
                    .child(ghost_icon_button(("ann-discard", id), Icon::Trash, tr("不改了，删除这条批注", "Discard annotation"), on_delete)),
            ),
        CardState::Reject { rewritten, input, on_cancel, on_submit } => root
            .child(result_box(id, rewritten))
            .child(motion::slide_in_right(
                div().w_full().flex().items_center().gap(px(6.))
                    .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                    .child(icon_box_button(("ann-reject-close", id), Icon::Close, false, tr("收起", "Collapse"), on_cancel))
                    .child(
                        div().flex_1().min_w_0().h(px(30.)).px(px(8.)).rounded(px(6.)).bg(theme.shell).border_1().border_color(theme.accent)
                            .flex().items_center()
                            .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).px(px(0.))),
                    )
                    .child(icon_box_button(("ann-reject-ok", id), Icon::Check, true, tr("按意见重新生成", "Regenerate with feedback"), on_submit)),
                ("ann-reject-slide", id),
            )),
        CardState::Accepted { on_undo } => root.child(
            div().w_full().flex().items_center().gap(px(8.)).text_color(theme.text_secondary)
                .child(icon(Icon::Check, px(14.), theme.mono_icon_colors(theme.accent)))
                .child(div().flex_1().min_w_0().text_size(px(14.)).line_height(px(20.)).child(tr("已替换", "Replaced")))
                .child(
                    div().id(("ann-undo", id)).h(px(26.)).px(px(8.)).rounded(px(6.)).flex_none()
                        .flex().items_center().justify_center().text_size(px(14.)).line_height(px(20.)).text_color(theme.accent)
                        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
                        .on_mouse_down(MouseButton::Left, move |ev, w, cx| on_undo(ev, w, cx))
                        .child(tr("撤销", "Undo")),
                ),
        ),
        CardState::Error { message, on_retry, on_delete } => {
            let err_fg: Hsla = if is_dark() { rgb(0xf97066).into() } else { rgb(0xb42318).into() };
            root.child(
                div().w_full().flex().items_center().gap(px(6.))
                    .child(div().flex_1().min_w_0().text_size(px(14.)).line_height(px(20.)).text_color(err_fg).child(message))
                    .child(icon_box_button(("ann-retry", id), Icon::Refresh, false, tr("重试", "Retry"), on_retry))
                    .child(ghost_icon_button(("ann-err-del", id), Icon::Trash, tr("删除批注", "Delete annotation"), on_delete).children(crate::ui::smoke_overlay::probe(format!("annotation-delete-{id}")))),
            )
        }
    }
}
