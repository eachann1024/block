use std::cell::Cell;
use gpui::{
    Bounds, Context, CursorStyle, Div, ElementId, Entity, FontWeight, Hsla, MouseButton,
    MouseDownEvent, Pixels, Point, SharedString, Stateful, Window, div, point, prelude::*, px,
    rgb, size,
};
use crate::{
    dropdown,
    editor::{model::CalloutKind, view::Editor},
    icons::{Icon, IconColors, icon},
    theme::{Theme, is_dark, tooltip, tr},
};

#[derive(Clone, Copy)]
pub struct CalloutStyle {
    pub bg: Hsla,
    pub border: Hsla,
    pub icon_fill: Hsla,
    pub accent: Hsla,
    pub text: Hsla,
}

pub const ALL_KINDS: [CalloutKind; 5] = [
    CalloutKind::Note,
    CalloutKind::Tip,
    CalloutKind::Important,
    CalloutKind::Warning,
    CalloutKind::Caution,
];

pub fn label(kind: CalloutKind) -> &'static str {
    match kind {
        CalloutKind::Note => tr("提示", "Note"),
        CalloutKind::Tip => tr("技巧", "Tip"),
        CalloutKind::Important => tr("重要", "Important"),
        CalloutKind::Warning => tr("警告", "Warning"),
        CalloutKind::Caution => tr("注意", "Caution"),
    }
}

pub fn next_kind(kind: CalloutKind) -> CalloutKind {
    match kind {
        CalloutKind::Note => CalloutKind::Tip,
        CalloutKind::Tip => CalloutKind::Important,
        CalloutKind::Important => CalloutKind::Warning,
        CalloutKind::Warning => CalloutKind::Caution,
        CalloutKind::Caution => CalloutKind::Note,
    }
}

pub fn style(kind: CalloutKind) -> CalloutStyle {
    if is_dark() {
        match kind {
            CalloutKind::Note => CalloutStyle {
                bg: rgb(0x1f2633).into(), border: rgb(0x31456a).into(),
                icon_fill: rgb(0x283856).into(), accent: rgb(0x779de8).into(), text: rgb(0x9dbcf5).into(),
            },
            CalloutKind::Tip => CalloutStyle {
                bg: rgb(0x1c2b24).into(), border: rgb(0x2b5240).into(),
                icon_fill: rgb(0x234234).into(), accent: rgb(0x4cc38a).into(), text: rgb(0x8ce0b8).into(),
            },
            CalloutKind::Important => CalloutStyle {
                bg: rgb(0x252133).into(), border: rgb(0x46396e).into(),
                icon_fill: rgb(0x362c54).into(), accent: rgb(0x9e8cfc).into(), text: rgb(0xc4b8ff).into(),
            },
            CalloutKind::Warning => CalloutStyle {
                bg: rgb(0x2d251b).into(), border: rgb(0x5c4320).into(),
                icon_fill: rgb(0x473419).into(), accent: rgb(0xf0a03c).into(), text: rgb(0xf7c98b).into(),
            },
            CalloutKind::Caution => CalloutStyle {
                bg: rgb(0x2f1f20).into(), border: rgb(0x632d2f).into(),
                icon_fill: rgb(0x4d2325).into(), accent: rgb(0xf27474).into(), text: rgb(0xf9a8a8).into(),
            },
        }
    } else {
        match kind {
            CalloutKind::Note => CalloutStyle {
                bg: rgb(0xeef3fc).into(), border: rgb(0xc9d8f3).into(),
                icon_fill: rgb(0xdce7fa).into(), accent: rgb(0x547dd0).into(), text: rgb(0x2c4c8c).into(),
            },
            CalloutKind::Tip => CalloutStyle {
                bg: rgb(0xedf7f2).into(), border: rgb(0xbce3cf).into(),
                icon_fill: rgb(0xd3efe1).into(), accent: rgb(0x1f8a5b).into(), text: rgb(0x166541).into(),
            },
            CalloutKind::Important => CalloutStyle {
                bg: rgb(0xf3f0fb).into(), border: rgb(0xd6ccf2).into(),
                icon_fill: rgb(0xe4ddf8).into(), accent: rgb(0x6e56cf).into(), text: rgb(0x4c389e).into(),
            },
            CalloutKind::Warning => CalloutStyle {
                bg: rgb(0xfdf5e8).into(), border: rgb(0xedd4a8).into(),
                icon_fill: rgb(0xf7e2be).into(), accent: rgb(0xb86b11).into(), text: rgb(0x8a4e08).into(),
            },
            CalloutKind::Caution => CalloutStyle {
                bg: rgb(0xfceeee).into(), border: rgb(0xf1c4c4).into(),
                icon_fill: rgb(0xf8dada).into(), accent: rgb(0xc93b3b).into(), text: rgb(0x962727).into(),
            },
        }
    }
}

thread_local! {
    static OPEN_MENU: Cell<Option<(u64, Point<Pixels>)>> = const { Cell::new(None) };
}

pub fn close_menu() -> bool {
    OPEN_MENU.with(|slot| slot.replace(None).is_some())
}

pub fn is_menu_open(block_id: u64) -> bool {
    OPEN_MENU.with(|slot| slot.get().is_some_and(|(id, _)| id == block_id))
}

fn toggle_menu(block_id: u64, anchor: Point<Pixels>) {
    dropdown::register_closer(close_menu);
    OPEN_MENU.with(|slot| {
        if slot.get().is_some_and(|(id, _)| id == block_id) {
            slot.set(None);
        } else {
            slot.set(Some((block_id, anchor)));
        }
    });
}

fn menu_row(
    id: (&'static str, usize),
    kind: CalloutKind,
    checked: bool,
    on_pick: impl Fn(&MouseDownEvent, &mut Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let p = dropdown::palette();
    let st = style(kind);
    div().id(id).h(px(30.)).w_full().px(px(10.)).gap(px(8.)).rounded(crate::ui::radius::CONTROL)
        .flex().items_center().text_size(px(14.)).line_height(px(20.)).text_color(p.text)
        .cursor(CursorStyle::PointingHand)
        .when(checked, move |s| s.bg(p.checked_bg).font_weight(FontWeight(500.)))
        .hover(move |s| s.bg(p.hover_bg))
        .child(div().size(px(10.)).rounded(px(5.)).bg(st.icon_fill).border_1().border_color(st.accent).flex_none())
        .child(div().flex_1().child(label(kind)))
        .child(crate::ui::shortcut_label::shortcut_hint(SharedString::from(kind.tag()), p.muted))
        .on_mouse_down(MouseButton::Left, on_pick)
}

pub fn decorate_callout(
    body: Div,
    editor: Entity<Editor>,
    id: u64,
    kind: CalloutKind,
    line: f32,
    zoom: f32,
    visible: bool,
    cx: &mut Context<Editor>,
) -> Div {
    let theme = Theme::ocean();
    let st = style(kind);
    let open = is_menu_open(id);
    let icon_top = (12. * zoom + (line - 28. * zoom) / 2.).max(6. * zoom);
    let icon_id = ElementId::from(("callout-icon", id as usize));
    let pill_id = ElementId::from(("callout-pill", id as usize));
    let cycle_id = ElementId::from(("callout-cycle", id as usize));
    let clear_id = ElementId::from(("callout-clear", id as usize));

    let icon_probe = crate::ui::smoke_overlay::probe("callout-icon");
    let pill_probe = crate::ui::smoke_overlay::probe("callout-pill");
    let cycle_probe = crate::ui::smoke_overlay::probe("callout-cycle");
    let clear_probe = crate::ui::smoke_overlay::probe("callout-clear");

    let icon_btn = div().id(icon_id).relative().children(icon_probe)
        .size(px(28. * zoom)).rounded(px(6. * zoom)).flex().items_center().justify_center()
        .cursor(CursorStyle::PointingHand)
        .when(open, |d| d.bg(theme.paper).border_1().border_color(st.accent))
        .hover(move |s| s.bg(theme.paper).border_1().border_color(st.border))
        .tooltip(tooltip(tr("点击切换标注类型", "Click to switch callout type")))
        .child(icon(Icon::Callout, px(18. * zoom), IconColors { line: st.accent, fill: st.icon_fill, accent: st.accent }))
        .on_mouse_down(MouseButton::Left, cx.listener(move |_, event: &MouseDownEvent, _, cx| {
            toggle_menu(id, event.position);
            cx.notify();
            cx.stop_propagation();
        }));

    let hover_actions = crate::ui::motion::reveal_fade(
        div().flex().items_center().gap(px(4. * zoom))
            .child(div().id(pill_id).relative().children(pill_probe)
                .h(px(24. * zoom)).px(px(8. * zoom)).rounded(px(6. * zoom))
                .bg(theme.paper).border_1().border_color(st.border)
                .flex().items_center().gap(px(4. * zoom)).cursor(CursorStyle::PointingHand)
                .text_size(px(14. * zoom)).font_weight(FontWeight(500.)).text_color(st.text)
                .hover(move |s| s.border_color(st.accent))
                .child(label(kind))
                .child(div().text_color(theme.text_secondary).child("▾"))
                .on_mouse_down(MouseButton::Left, cx.listener(move |_, event: &MouseDownEvent, _, cx| {
                    toggle_menu(id, event.position);
                    cx.notify();
                    cx.stop_propagation();
                })))
            .child(div().id(cycle_id).relative().children(cycle_probe)
                .h(px(24. * zoom)).px(px(8. * zoom)).rounded(px(6. * zoom))
                .bg(theme.paper).border_1().border_color(theme.btn_border)
                .flex().items_center().cursor(CursorStyle::PointingHand)
                .text_size(px(14. * zoom)).text_color(theme.text_secondary)
                .hover(move |s| s.bg(theme.hover).text_color(theme.text).border_color(st.accent))
                .tooltip(tooltip(tr("切换下一标注类型", "Next callout type")))
                .child(tr("切换", "Switch"))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                    close_menu();
                    this.set_callout_kind(id, Some(next_kind(kind)), cx);
                    cx.stop_propagation();
                })))
            .child(div().id(clear_id).relative().children(clear_probe)
                .h(px(24. * zoom)).px(px(8. * zoom)).rounded(px(6. * zoom))
                .bg(theme.paper).border_1().border_color(theme.btn_border)
                .flex().items_center().cursor(CursorStyle::PointingHand)
                .text_size(px(14. * zoom)).text_color(theme.text_secondary)
                .hover(move |s| s.bg(theme.hover).text_color(theme.text))
                .tooltip(tooltip(tr("转为普通正文", "Convert to paragraph")))
                .child(tr("正文", "Text"))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                    close_menu();
                    this.set_callout_kind(id, None, cx);
                    cx.stop_propagation();
                }))),
        ("callout-actions", id as usize),
    );

    let popover = open.then(|| {
        let anchor_pt = OPEN_MENU.with(|slot| slot.get().map(|(_, p)| p)).unwrap_or_else(|| point(px(120.), px(120.)));
        let trigger = Bounds::new(point(anchor_pt.x - px(14.), anchor_pt.y - px(14.)), size(px(28.), px(28.)));
        let mut menu = dropdown::shell(("callout-menu", id as usize)).w(px(192.));
        for (ix, k) in ALL_KINDS.into_iter().enumerate() {
            let ed = editor.clone();
            menu = menu.child(menu_row(("callout-opt", ix), k, k == kind, move |_, _, cx| {
                close_menu();
                ed.update(cx, |this, cx| this.set_callout_kind(id, Some(k), cx));
                cx.stop_propagation();
            }));
        }
        let ed = editor.clone();
        menu = menu.child(dropdown::separator()).child(
            dropdown::row(("callout-opt-plain", id as usize), Some(Icon::Text), tr("转为普通正文", "Convert to Text"), false)
                .on_mouse_down(MouseButton::Left, move |_, _, cx| {
                    close_menu();
                    ed.update(cx, |this, cx| this.set_callout_kind(id, None, cx));
                    cx.stop_propagation();
                }),
        );
        gpui::deferred(
            dropdown::smart_anchored(dropdown::PlacementSpec::below(trigger).gap(px(4.)).margin(px(8.))).child(menu),
        ).with_priority(3)
    });

    body.rounded(px(8. * zoom)).bg(st.bg).border_1().border_color(st.border)
        .py(px(12. * zoom)).pr(px(16. * zoom)).pl(px(46. * zoom))
        .child(div().absolute().left(px(10. * zoom)).top(px(icon_top)).child(icon_btn))
        .when(visible || open, |b| b.child(div().absolute().right(px(8. * zoom)).top(px(6. * zoom)).child(hover_actions)))
        .children(popover)
}
