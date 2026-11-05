//! Interactive Callout block controls (Ocean Paper Scheme A):
//! - Semantic 5-state palette (Note, Tip, Important, Warning, Caution) with 1px semantic border.
//! - Dedicated top header bar above block text so controls never overlap text.
//! - Clickable left icon & type badge (`提示 ▾`) opening a 5-state dropdown popover with backdrop.
//! - Top-right quick actions (`切换类型`, `转为正文`) mounted after `BlockText` for reliable hit-testing.
use std::{cell::{Cell, RefCell}, collections::HashMap};
use gpui::{
    Bounds, Context, CursorStyle, Div, ElementId, Entity, FontWeight, Hsla, MouseButton,
    MouseDownEvent, Pixels, Point, Size, Stateful, Window, anchored, deferred, div, point,
    prelude::*, px, rgb, size, transparent_black,
};
use crate::{
    dropdown::{self, RowSpec},
    editor::{model::CalloutKind, view::Editor},
    icons::{Icon, IconColors, icon},
    theme::{Theme, is_dark, tooltip, tr},
};

thread_local! {
    static OPEN_MENU: Cell<Option<(u64, Point<Pixels>)>> = const { Cell::new(None) };
    static TRIGGER_BOUNDS: RefCell<HashMap<u64, (Bounds<Pixels>, Size<Pixels>)>> = Default::default();
}

pub fn close_menu() -> bool {
    OPEN_MENU.with(|slot| slot.take().is_some())
}

pub fn is_menu_open(block_id: u64) -> bool {
    OPEN_MENU.with(|slot| slot.get().is_some_and(|(id, _)| id == block_id))
}

fn toggle_menu(block_id: u64, anchor: Point<Pixels>) {
    OPEN_MENU.with(|slot| {
        if slot.get().is_some_and(|(id, _)| id == block_id) {
            slot.set(None);
        } else {
            slot.set(Some((block_id, anchor)));
        }
    });
}

#[derive(Clone, Copy)]
pub struct CalloutStyle {
    pub bg: Hsla,
    pub border: Hsla,
    pub accent: Hsla,
    pub icon_fill: Hsla,
    pub text: Hsla,
}

pub fn style(kind: CalloutKind) -> CalloutStyle {
    let dark = is_dark();
    match (kind, dark) {
        (CalloutKind::Note, false) => CalloutStyle {
            bg: rgb(0xeef3fc).into(), border: rgb(0xc9d8f3).into(),
            accent: rgb(0x547dd0).into(), icon_fill: rgb(0xdce7fa).into(), text: rgb(0x2c4c8c).into(),
        },
        (CalloutKind::Tip, false) => CalloutStyle {
            bg: rgb(0xedf7f2).into(), border: rgb(0xbce3cf).into(),
            accent: rgb(0x1f8a5b).into(), icon_fill: rgb(0xd3efe1).into(), text: rgb(0x166541).into(),
        },
        (CalloutKind::Important, false) => CalloutStyle {
            bg: rgb(0xf3f0fb).into(), border: rgb(0xd6ccf2).into(),
            accent: rgb(0x6e56cf).into(), icon_fill: rgb(0xe4ddf8).into(), text: rgb(0x4c389e).into(),
        },
        (CalloutKind::Warning, false) => CalloutStyle {
            bg: rgb(0xfdf5e8).into(), border: rgb(0xedd4a8).into(),
            accent: rgb(0xb86b11).into(), icon_fill: rgb(0xf7e2be).into(), text: rgb(0x8a4e08).into(),
        },
        (CalloutKind::Caution, false) => CalloutStyle {
            bg: rgb(0xfceeee).into(), border: rgb(0xf1c4c4).into(),
            accent: rgb(0xc93b3b).into(), icon_fill: rgb(0xf8dada).into(), text: rgb(0x962727).into(),
        },
        (CalloutKind::Note, true) => CalloutStyle {
            bg: rgb(0x1b2434).into(), border: rgb(0x2f4368).into(),
            accent: rgb(0x7aa2f7).into(), icon_fill: rgb(0x253552).into(), text: rgb(0x9ab8ff).into(),
        },
        (CalloutKind::Tip, true) => CalloutStyle {
            bg: rgb(0x182923).into(), border: rgb(0x284d3e).into(),
            accent: rgb(0x4fd69c).into(), icon_fill: rgb(0x203d31).into(), text: rgb(0x7ce8b8).into(),
        },
        (CalloutKind::Important, true) => CalloutStyle {
            bg: rgb(0x231e34).into(), border: rgb(0x3f3366).into(),
            accent: rgb(0xa78bfa).into(), icon_fill: rgb(0x312750).into(), text: rgb(0xc4b5fd).into(),
        },
        (CalloutKind::Warning, true) => CalloutStyle {
            bg: rgb(0x2b2316).into(), border: rgb(0x544021).into(),
            accent: rgb(0xf5a623).into(), icon_fill: rgb(0x3d2f19).into(), text: rgb(0xf8c165).into(),
        },
        (CalloutKind::Caution, true) => CalloutStyle {
            bg: rgb(0x2d1b1e).into(), border: rgb(0x592e34).into(),
            accent: rgb(0xf26d6d).into(), icon_fill: rgb(0x422328).into(), text: rgb(0xff9b9b).into(),
        },
    }
}

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

pub const ALL_KINDS: [CalloutKind; 5] = [
    CalloutKind::Note,
    CalloutKind::Tip,
    CalloutKind::Important,
    CalloutKind::Warning,
    CalloutKind::Caution,
];

/// Styles the Callout card container and reserves top header space so controls never overlap text.
pub fn style_card(body: Div, kind: CalloutKind, zoom: f32) -> Div {
    let st = style(kind);
    body.rounded(px(8. * zoom))
        .bg(st.bg)
        .border_1()
        .border_color(st.border)
        .pt(px(38. * zoom))
        .pb(px(12. * zoom))
        .pr(px(16. * zoom))
        .pl(px(44. * zoom))
}

fn menu_row(
    id: impl Into<ElementId>,
    probe_name: &'static str,
    kind: CalloutKind,
    checked: bool,
    on_click: impl Fn(&MouseDownEvent, &mut Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let st = style(kind);
    dropdown::row_with(
        id,
        RowSpec {
            icon: Some(Icon::Callout),
            label: label(kind).into(),
            hint: Some(kind.tag().into()),
            checked,
            ..Default::default()
        },
    )
    .children(crate::ui::smoke_overlay::probe(probe_name))
    .child(div().size(px(8.)).rounded_full().bg(st.accent).ml(px(4.)).border_1().border_color(theme.paper))
    .on_mouse_down(MouseButton::Left, on_click)
}

/// Interactive header controls and 5-state type picker, mounted AFTER `BlockText`.
pub fn controls(
    editor: Entity<Editor>,
    id: u64,
    kind: CalloutKind,
    visible: bool,
    zoom: f32,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    dropdown::register_closer(close_menu);
    let theme = Theme::ocean();
    let st = style(kind);
    let open = is_menu_open(id);
    let icon_id = ElementId::from(("callout-icon", id as usize));
    let pill_id = ElementId::from(("callout-pill", id as usize));
    let cycle_id = ElementId::from(("callout-cycle", id as usize));
    let clear_id = ElementId::from(("callout-clear", id as usize));

    let bounds_tracker = gpui::canvas(
        move |bounds, window, _| {
            let vp = window.viewport_size();
            TRIGGER_BOUNDS.with(|b| { b.borrow_mut().insert(id, (bounds, vp)); });
        },
        |_, _, _, _| {},
    ).absolute().top_0().left_0().size_full();

    let icon_btn = div().id(icon_id).relative()
        .children(crate::ui::smoke_overlay::probe("callout-icon"))
        .size(px(26. * zoom)).rounded(px(6. * zoom)).flex().items_center().justify_center()
        .cursor(CursorStyle::PointingHand)
        .when(open, |d| d.bg(theme.paper).border_1().border_color(st.accent))
        .hover(move |s| s.bg(theme.paper).border_1().border_color(st.border))
        .tooltip(tooltip(tr("点击切换标注类型", "Click to switch callout type")))
        .child(icon(Icon::Callout, px(17. * zoom), IconColors { line: st.accent, fill: st.icon_fill, accent: st.accent }))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, event: &MouseDownEvent, window, cx| {
            cx.stop_propagation();
            toggle_menu(id, event.position);
            this.focus(window, cx);
            cx.notify();
        }));

    let pill_btn = div().id(pill_id).relative()
        .children(crate::ui::smoke_overlay::probe("callout-pill"))
        .h(px(24. * zoom)).px(px(8. * zoom)).rounded(px(6. * zoom))
        .when(open, |d| d.bg(theme.paper).border_1().border_color(st.accent))
        .hover(move |s| s.bg(theme.paper).border_1().border_color(st.border))
        .flex().items_center().gap(px(4. * zoom)).cursor(CursorStyle::PointingHand)
        .text_size(px(14. * zoom)).font_weight(FontWeight(550.)).text_color(st.text)
        .child(label(kind))
        .child(div().text_size(px(12. * zoom)).text_color(st.text).child("▾"))
        .child(bounds_tracker)
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, event: &MouseDownEvent, window, cx| {
            cx.stop_propagation();
            toggle_menu(id, event.position);
            this.focus(window, cx);
            cx.notify();
        }));

    let right_actions = div().flex().items_center().gap(px(4. * zoom))
        .when(!visible && !open, |d| d.invisible())
        .child(div().id(cycle_id).relative()
            .children(crate::ui::smoke_overlay::probe("callout-cycle"))
            .h(px(24. * zoom)).px(px(8. * zoom)).rounded(px(6. * zoom))
            .bg(theme.paper).border_1().border_color(st.border)
            .flex().items_center().cursor(CursorStyle::PointingHand)
            .text_size(px(14. * zoom)).text_color(theme.text_secondary)
            .hover(move |s| s.bg(theme.hover).text_color(theme.text).border_color(st.accent))
            .tooltip(tooltip(tr("切换下一标注类型", "Next callout type")))
            .child(tr("切换类型", "Switch"))
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                cx.stop_propagation();
                close_menu();
                this.set_callout_kind(id, Some(next_kind(kind)), cx);
                this.focus(window, cx);
            })))
        .child(div().id(clear_id).relative()
            .children(crate::ui::smoke_overlay::probe("callout-clear"))
            .h(px(24. * zoom)).px(px(8. * zoom)).rounded(px(6. * zoom))
            .bg(theme.paper).border_1().border_color(st.border)
            .flex().items_center().cursor(CursorStyle::PointingHand)
            .text_size(px(14. * zoom)).text_color(theme.text_secondary)
            .hover(move |s| s.bg(theme.hover).text_color(theme.text).border_color(st.accent))
            .tooltip(tooltip(tr("转为普通正文", "Convert to paragraph")))
            .child(tr("转为正文", "To Text"))
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                cx.stop_propagation();
                close_menu();
                this.set_callout_kind(id, None, cx);
                this.focus(window, cx);
            })));

    let popover = open.then(|| {
        let measured = TRIGGER_BOUNDS.with(|b| b.borrow().get(&id).copied());
        let fallback = OPEN_MENU.with(|slot| slot.get().map(|(_, p)| p)).unwrap_or_else(|| point(px(120.), px(120.)));
        let spec = measured
            .map(|(b, _)| dropdown::PlacementSpec::below(b).gap(px(4.)).margin(px(8.)))
            .unwrap_or_else(|| dropdown::PlacementSpec::below(Bounds::new(fallback, size(px(28.), px(24.)))).gap(px(4.)).margin(px(8.)));
        let probe_names = ["callout-opt-0", "callout-opt-1", "callout-opt-2", "callout-opt-3", "callout-opt-4"];
        let mut menu = dropdown::shell(("callout-menu", id as usize)).w(px(196.));
        for (ix, k) in ALL_KINDS.into_iter().enumerate() {
            let ed = editor.clone();
            menu = menu.child(menu_row(("callout-opt", ix), probe_names[ix], k, k == kind, move |_, window, cx| {
                cx.stop_propagation();
                close_menu();
                ed.update(cx, |this, cx| {
                    this.set_callout_kind(id, Some(k), cx);
                    this.focus(window, cx);
                });
            }));
        }
        let ed = editor.clone();
        menu = menu.child(dropdown::separator()).child(
            dropdown::row(("callout-opt-plain", id as usize), Some(Icon::Text), tr("转为普通正文", "Convert to Text"), false)
                .children(crate::ui::smoke_overlay::probe("callout-opt-plain"))
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    cx.stop_propagation();
                    close_menu();
                    ed.update(cx, |this, cx| {
                        this.set_callout_kind(id, None, cx);
                        this.focus(window, cx);
                    });
                }),
        );
        let backdrop = deferred(anchored().position(point(px(0.), px(0.))).child(
            div().id("callout-menu-backdrop").w(px(10000.)).h(px(10000.)).bg(transparent_black()).occlude()
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                    cx.stop_propagation(); close_menu(); this.focus(window, cx); cx.notify();
                }))
                .on_mouse_down(MouseButton::Right, cx.listener(|this, _, window, cx| {
                    cx.stop_propagation(); close_menu(); this.focus(window, cx); cx.notify();
                })),
        )).with_priority(2);
        let popup = dropdown::deferred_popover_for(spec, menu, 3);
        div().child(backdrop).child(popup)
    });

    div().absolute().top(px(7. * zoom)).left(px(10. * zoom)).right(px(10. * zoom)).h(px(26. * zoom))
        .flex().items_center().justify_between()
        .child(div().flex().items_center().gap(px(4. * zoom)).child(icon_btn).child(pill_btn))
        .child(right_actions)
        .children(popover)
}
