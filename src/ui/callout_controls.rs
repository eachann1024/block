//! Interactive Callout block controls (Ocean Paper Scheme A):
//! - Semantic 5-state palette (Note, Tip, Important, Warning, Caution) with 1px semantic border.
//! - Dedicated top header bar above block text so controls never overlap text.
//! - Clickable left icon & type badge (`提示 ▾`) opening a 5-state dropdown popover with backdrop.
//! - Top-right quick actions (`切换类型`, `转为正文`) mounted after `BlockText` for reliable hit-testing.
use crate::ui::tooltip::TooltipExt;
use std::{cell::{Cell, RefCell}, collections::{HashMap, HashSet}, time::Duration};
use gpui::{
    Bounds, ClipboardItem, Context, CursorStyle, Div, ElementId, Entity, Focusable, FontWeight, Hsla,
    MouseButton, MouseDownEvent, Pixels, Point, Size, Stateful, Window, anchored, deferred, div,
    point, prelude::*, px, rgb, size, transparent_black,
};
use crate::{
    dropdown,
    editor::{model::CalloutKind, view::Editor},
    icons::{Icon, IconColors, icon},
    theme::{Theme, is_dark, tooltip, tr},
};

thread_local! {
    static OPEN_MENU: Cell<Option<(u64, Point<Pixels>)>> = const { Cell::new(None) };
    static TRIGGER_BOUNDS: RefCell<HashMap<u64, (Bounds<Pixels>, Size<Pixels>)>> = Default::default();
    static COPIED: RefCell<HashSet<u64>> = Default::default();
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

fn tone(kind: CalloutKind, dark: bool) -> (u32, u32, u32, u32, u32) {
    match (kind, dark) {
        (CalloutKind::Note, false) => (0xeef3fc, 0xc9d8f3, 0x547dd0, 0xdce7fa, 0x2c4c8c),
        (CalloutKind::Tip, false) => (0xedf7f2, 0xbce3cf, 0x1f8a5b, 0xd3efe1, 0x166541),
        (CalloutKind::Important, false) => (0xf3f0fb, 0xd6ccf2, 0x6e56cf, 0xe4ddf8, 0x4c389e),
        (CalloutKind::Warning, false) => (0xfdf5e8, 0xedd4a8, 0xb86b11, 0xf7e2be, 0x8a4e08),
        (CalloutKind::Caution, false) => (0xfceeee, 0xf1c4c4, 0xc93b3b, 0xf8dada, 0x962727),
        (CalloutKind::Note, true) => (0x1b2434, 0x2f4368, 0x7aa2f7, 0x253552, 0x9ab8ff),
        (CalloutKind::Tip, true) => (0x182923, 0x284d3e, 0x4fd69c, 0x203d31, 0x7ce8b8),
        (CalloutKind::Important, true) => (0x231e34, 0x3f3366, 0xa78bfa, 0x312750, 0xc4b5fd),
        (CalloutKind::Warning, true) => (0x2b2316, 0x544021, 0xf5a623, 0x3d2f19, 0xf8c165),
        (CalloutKind::Caution, true) => (0x2d1b1e, 0x592e34, 0xf26d6d, 0x422328, 0xff9b9b),
    }
}

pub fn style(kind: CalloutKind) -> CalloutStyle {
    let (bg, border, accent, icon_fill, text) = tone(kind, is_dark());
    CalloutStyle { bg: rgb(bg).into(), border: rgb(border).into(), accent: rgb(accent).into(), icon_fill: rgb(icon_fill).into(), text: rgb(text).into() }
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
    body.rounded(px(8. * zoom)).bg(st.bg).border_1().border_color(st.border)
        .pt(px(38. * zoom)).pb(px(12. * zoom)).pr(px(16. * zoom)).pl(px(44. * zoom))
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
    div().id(id).relative().w_full().h(px(32.)).px(px(10.)).rounded(px(6.)).children(crate::ui::smoke_overlay::probe(probe_name))
        .flex().items_center().gap(px(8.)).cursor(CursorStyle::PointingHand).text_size(px(14.)).text_color(theme.text)
        .when(checked, move |d| d.bg(theme.selected).border_1().border_color(theme.accent).font_weight(FontWeight(500.)))
        .when(!checked, move |d| d.hover(|s| s.bg(theme.hover)))
        .child(div().size(px(10.)).flex_none().rounded(px(5.)).bg(st.bg).border_1().border_color(st.accent))
        .child(div().flex_1().min_w_0().child(label(kind)))
        .child(div().flex_none().font_family(crate::theme::mono_font_family()).text_size(px(14.)).text_color(theme.text_secondary).child(kind.tag()))
        .on_mouse_down(MouseButton::Left, on_click)
}

fn chip(id: ElementId, probe: &'static str, zoom: f32, st: CalloutStyle, label: &'static str, tip: &'static str) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id(id).relative().children(crate::ui::smoke_overlay::probe(probe))
        .h(px(24. * zoom)).px(px(8. * zoom)).rounded(px(6. * zoom))
        .bg(theme.paper).border_1().border_color(st.border)
        .flex().items_center().cursor(CursorStyle::PointingHand)
        .text_size(px(14. * zoom)).text_color(theme.text_secondary)
        .hover(move |s| s.bg(theme.hover).text_color(theme.text).border_color(st.accent))
        .tooltip_above(tooltip(tip)).child(label)
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
    let copy_id = ElementId::from(("callout-copy", id as usize));
    let cycle_id = ElementId::from(("callout-cycle", id as usize));
    let clear_id = ElementId::from(("callout-clear", id as usize));
    let copied = COPIED.with(|set| set.borrow().contains(&id));

    let bounds_tracker = gpui::canvas(
        move |bounds, window, _| {
            let vp = window.viewport_size();
            TRIGGER_BOUNDS.with(|b| { b.borrow_mut().insert(id, (bounds, vp)); });
        },
        |_, _, _, _| {},
    ).absolute().top_0().left_0().size_full();

    let icon_btn = div().id(icon_id).relative().children(crate::ui::smoke_overlay::probe("callout-icon"))
        .size(px(26. * zoom)).rounded(px(6. * zoom)).flex().items_center().justify_center().cursor(CursorStyle::PointingHand)
        .when(open, |d| d.bg(theme.paper).border_1().border_color(st.accent))
        .hover(move |s| s.bg(theme.paper).border_1().border_color(st.border))
        .tooltip_above(tooltip(tr("点击切换标注类型", "Click to switch callout type")))
        .child(icon(Icon::Callout, px(17. * zoom), IconColors { line: st.accent, fill: st.icon_fill, accent: st.accent }))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, event: &MouseDownEvent, window, cx| {
            cx.stop_propagation(); toggle_menu(id, event.position); this.focus_handle(cx).focus(window, cx); cx.notify();
        }));

    let pill_btn = div().id(pill_id).relative().children(crate::ui::smoke_overlay::probe("callout-pill"))
        .h(px(24. * zoom)).px(px(8. * zoom)).rounded(px(6. * zoom))
        .when(open, |d| d.bg(theme.paper).border_1().border_color(st.accent))
        .hover(move |s| s.bg(theme.paper).border_1().border_color(st.border))
        .flex().items_center().gap(px(4. * zoom)).cursor(CursorStyle::PointingHand)
        .text_size(px(14. * zoom)).font_weight(FontWeight(550.)).text_color(st.text)
        .child(label(kind)).child(div().text_size(px(12. * zoom)).text_color(st.text).child("▾")).child(bounds_tracker)
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, event: &MouseDownEvent, window, cx| {
            cx.stop_propagation(); toggle_menu(id, event.position); this.focus_handle(cx).focus(window, cx); cx.notify();
        }));

    let copy_editor = editor.clone();
    let right_actions = div().flex().items_center().gap(px(4. * zoom))
        .when(!visible && !open, |d| d.invisible())
        .child(chip(copy_id, "callout-copy", zoom, st, if copied { tr("已复制", "Copied") } else { tr("复制", "Copy") }, tr("复制标注文本", "Copy callout text"))
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                cx.stop_propagation();
                let text = this.block_plain_text(id);
                cx.write_to_clipboard(ClipboardItem::new_string(text));
                COPIED.with(|set| { set.borrow_mut().insert(id); });
                cx.notify();
                let entity = copy_editor.downgrade();
                cx.spawn(async move |_, cx| {
                    cx.background_executor().timer(Duration::from_millis(1500)).await;
                    let _ = entity.update(cx, |_, cx| { COPIED.with(|set| { set.borrow_mut().remove(&id); }); cx.notify(); });
                }).detach();
            })))
        .child(chip(cycle_id, "callout-cycle", zoom, st, tr("切换类型", "Switch"), tr("切换下一标注类型", "Next callout type"))
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                cx.stop_propagation(); close_menu(); this.set_callout_kind(id, Some(next_kind(kind)), cx); this.focus_handle(cx).focus(window, cx);
            })))
        .child(chip(clear_id, "callout-clear", zoom, st, tr("转为正文", "To Text"), tr("转为普通正文", "Convert to paragraph"))
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                cx.stop_propagation(); close_menu(); this.set_callout_kind(id, None, cx); this.focus_handle(cx).focus(window, cx);
            })));

    let callout_key = format!("callout-menu-{id}");
    let motion = crate::ui::motion::popup_motion(&callout_key, open);
    if let Some((_, pop_gen)) = motion {
        crate::ui::motion::schedule_popup_finish(&callout_key, pop_gen, cx);
    }
    let popover = motion.map(|(phase, pop_gen)| {
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
                    this.focus_handle(cx).focus(window, cx);
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
                        this.focus_handle(cx).focus(window, cx);
                    });
                }),
        );
        let dismiss = cx.listener(|this, _: &MouseDownEvent, window, cx| {
            cx.stop_propagation(); close_menu(); this.focus_handle(cx).focus(window, cx); cx.notify();
        });
        let entering = phase == crate::ui::motion::PopupMotion::Enter;
        let backdrop = deferred(anchored().position(point(px(0.), px(0.))).child(
            crate::ui::motion::layer_fade(
                div().id("callout-menu-backdrop").w(px(10000.)).h(px(10000.)).bg(transparent_black()).occlude()
                    .on_mouse_down(MouseButton::Left, dismiss)
                    .on_mouse_down(MouseButton::Right, cx.listener(|this, _, window, cx| {
                        cx.stop_propagation(); close_menu(); this.focus_handle(cx).focus(window, cx); cx.notify();
                    })),
                gpui::SharedString::from(format!("callout-menu-scrim-{id}-{pop_gen}")),
                entering,
            ),
        )).with_priority(2);
        let popup = dropdown::anchored_motion(gpui::SharedString::from(format!("callout-menu-pop-{id}-{pop_gen}")), spec, menu, phase, 3);
        div().child(backdrop).child(popup)
    });

    div().absolute().top(px(7. * zoom)).left(px(10. * zoom)).right(px(10. * zoom)).h(px(26. * zoom))
        .flex().items_center().justify_between()
        .child(div().flex().items_center().gap(px(4. * zoom)).child(icon_btn).child(pill_btn))
        .child(right_actions)
        .children(popover)
}
