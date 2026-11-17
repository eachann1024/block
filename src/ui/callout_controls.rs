//! Interactive Callout block controls (Ocean Paper Scheme A):
//! - Semantic 5-state palette (Note, Tip, Important, Warning, Caution) with 1px semantic border.
//! - Dedicated top header bar above block text so controls never overlap text.
//! - Clickable left icon & type badge opening a 5-state dropdown popover with backdrop.
use crate::ui::tooltip::TooltipExt;
use std::{cell::{Cell, RefCell}, collections::HashMap};
use gpui::{
    Bounds, Context, CursorStyle, Div, ElementId, Entity, Focusable, Hsla,
    MouseButton, MouseDownEvent, Pixels, Point, Size, Stateful, Window, anchored, deferred, div,
    point, prelude::*, px, rgb, size, transparent_black,
};
use crate::{
    dropdown,
    editor::{model::CalloutKind, view::Editor},
    icons::{Icon, IconColors, icon},
    theme::{Theme, is_dark, tooltip, tr},
    ui::{icon_size, type_scale},
};

thread_local! {
    static OPEN_MENU: Cell<Option<(u64, Point<Pixels>)>> = const { Cell::new(None) };
    static MENU_INDEX: Cell<usize> = const { Cell::new(0) };
    static TRIGGER_BOUNDS: RefCell<HashMap<u64, (Bounds<Pixels>, Size<Pixels>)>> = Default::default();
}

pub fn close_menu() -> bool {
    OPEN_MENU.with(|slot| slot.take().is_some())
}

pub fn is_menu_open(block_id: u64) -> bool {
    OPEN_MENU.with(|slot| slot.get().is_some_and(|(id, _)| id == block_id))
}

pub fn is_any_menu_open() -> bool {
    OPEN_MENU.with(|slot| slot.get().is_some())
}

pub fn arm_menu(kind: CalloutKind) {
    let index = ALL_KINDS.iter().position(|item| *item == kind).unwrap_or(0);
    MENU_INDEX.with(|cell| cell.set(index));
}

fn menu_slots() -> [crate::ui::menu_nav::Slot; 7] {
    use crate::ui::menu_nav::Slot::{Item, Skip};
    [Item, Item, Item, Item, Item, Skip, Item]
}

pub fn step_menu(down: bool) -> bool {
    if OPEN_MENU.with(|slot| slot.get()).is_none() { return false; }
    let slots = menu_slots();
    let next = crate::ui::menu_nav::step(MENU_INDEX.with(Cell::get), &slots, down);
    MENU_INDEX.with(|cell| cell.set(next));
    true
}

pub fn activate_menu(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) -> bool {
    let Some((id, _)) = OPEN_MENU.with(|slot| slot.get()) else { return false };
    let index = crate::ui::menu_nav::clamp_index(MENU_INDEX.with(Cell::get), &menu_slots());
    close_menu();
    match index {
        0..=4 => editor.set_callout_kind(id, Some(ALL_KINDS[index]), cx),
        6 => editor.set_callout_kind(id, None, cx),
        _ => {}
    }
    editor.focus_handle(cx).focus(window, cx);
    cx.notify();
    true
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

pub(crate) fn tone(kind: CalloutKind, dark: bool) -> (u32, u32, u32, u32, u32) {
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
    highlighted: bool,
    on_click: impl Fn(&MouseDownEvent, &mut Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let st = style(kind);
    div().id(id).relative().w_full().h(px(32.)).px(px(10.)).rounded(px(6.)).children(crate::ui::smoke_overlay::probe(probe_name))
        .flex().items_center().gap(px(8.)).cursor(CursorStyle::PointingHand)
        .text_size(type_scale::BODY).line_height(px(20.)).text_color(theme.text)
        .when(checked, move |d| d.bg(theme.selected).border_1().border_color(theme.accent).font_weight(type_scale::CONTROL))
        .when(highlighted && !checked, move |d| d.bg(theme.hover))
        .when(!checked, move |d| d.hover(|s| s.bg(theme.hover)))
        .child(div().size(px(10.)).flex_none().rounded(px(5.)).bg(st.bg).border_1().border_color(st.accent))
        .child(div().flex_1().min_w_0().child(label(kind)))
        .child(div().flex_none().font_family(crate::theme::mono_font_family()).text_size(type_scale::BODY).line_height(px(20.)).text_color(theme.text_secondary).child(kind.tag()))
        .on_mouse_down(MouseButton::Left, on_click)
}

/// Interactive header controls and 5-state type picker, mounted AFTER `BlockText`.
pub fn controls(
    editor: Entity<Editor>,
    id: u64,
    kind: CalloutKind,
    zoom: f32,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    dropdown::register_closer(close_menu);
    let theme = Theme::ocean();
    let st = style(kind);
    let open = is_menu_open(id);
    let icon_id = ElementId::from(("callout-icon", id as usize));
    let pill_id = ElementId::from(("callout-pill", id as usize));

    let bounds_tracker = gpui::canvas(
        move |bounds, window, _| {
            let vp = window.viewport_size();
            TRIGGER_BOUNDS.with(|b| { b.borrow_mut().insert(id, (bounds, vp)); });
        },
        |_, _, _, _| {},
    ).absolute().top_0().left_0().size_full();

    let icon_btn = div().id(icon_id).relative().children(crate::ui::smoke_overlay::probe("callout-icon"))
        .size(px(26. * zoom)).rounded(px(6. * zoom)).border_1().border_color(transparent_black())
        .flex().items_center().justify_center().cursor(CursorStyle::PointingHand)
        .when(open, |d| d.bg(theme.paper).border_color(st.accent))
        .hover(move |s| s.bg(theme.paper).border_color(st.border))
        .tooltip_above(tooltip(tr("点击切换标注类型", "Click to switch callout type")))
        .child(icon(Icon::Callout, px(icon_size::ROW * zoom), IconColors { line: st.accent, fill: st.icon_fill, accent: st.accent }))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, event: &MouseDownEvent, window, cx| {
            cx.stop_propagation(); if !is_menu_open(id) { arm_menu(kind); } toggle_menu(id, event.position); this.focus_handle(cx).focus(window, cx); cx.notify();
        }));

    let pill_btn = div().id(pill_id).relative().children(crate::ui::smoke_overlay::probe("callout-pill"))
        .h(px(26. * zoom)).px(px(4. * zoom)).rounded(px(6. * zoom)).border_1().border_color(transparent_black())
        .when(open, |d| d.bg(theme.paper).border_color(st.accent))
        .hover(move |s| s.bg(theme.paper).border_color(st.border))
        .flex().items_center().gap(px(4. * zoom)).cursor(CursorStyle::PointingHand)
        .font_family(crate::theme::ui_font_family())
        .text_size(px(14. * zoom)).line_height(px(20. * zoom)).font_weight(type_scale::TITLE).text_color(st.text)
        .child(div().flex().items_center().child(label(kind)))
        .child(div().size(px(icon_size::MICRO * zoom)).flex_none().flex().items_center().justify_center().child(
            icon(Icon::TbChevron, px(icon_size::MICRO * zoom), IconColors { line: st.text, fill: transparent_black(), accent: st.text }),
        ))
        .child(bounds_tracker)
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, event: &MouseDownEvent, window, cx| {
            cx.stop_propagation(); if !is_menu_open(id) { arm_menu(kind); } toggle_menu(id, event.position); this.focus_handle(cx).focus(window, cx); cx.notify();
        }));

    let callout_key = format!("callout-menu-{id}");
    let motion = crate::ui::motion::popup_motion(&callout_key, open);
    if let Some((_, pop_gen)) = motion {
        crate::ui::motion::schedule_popup_finish(&callout_key, pop_gen, cx);
    }
    let popover = motion.map(|(phase, pop_gen)| {
        let probe_names = ["callout-opt-0", "callout-opt-1", "callout-opt-2", "callout-opt-3", "callout-opt-4"];
        let mut menu = dropdown::shell(("callout-menu", id as usize)).w(px(196.));
        let cursor = MENU_INDEX.with(Cell::get);
        for (ix, k) in ALL_KINDS.into_iter().enumerate() {
            let ed = editor.clone();
            menu = menu.child(menu_row(("callout-opt", ix), probe_names[ix], k, k == kind, cursor == ix, move |_, window, cx| {
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
            dropdown::row_with(("callout-opt-plain", id as usize), dropdown::RowSpec {
                icon: Some(Icon::Text), label: tr("转为普通正文", "Convert to Text").into(), highlighted: cursor == 6, ..Default::default()
            })
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
        let popup = dropdown::anchored_motion_with(
            gpui::SharedString::from(format!("callout-menu-pop-{id}-{pop_gen}")),
            move |window, _| {
                TRIGGER_BOUNDS.with(|bounds| bounds.borrow().get(&id).copied())
                    .map(|(trigger, _)| dropdown::PlacementSpec::below(trigger).gap(px(4.)).margin(px(8.)))
                    .unwrap_or_else(|| {
                        let at = OPEN_MENU.with(|slot| slot.get().map(|(_, point)| point)).unwrap_or_else(|| window.mouse_position());
                        dropdown::PlacementSpec::below(Bounds::new(at, size(px(28.), px(24.)))).gap(px(4.)).margin(px(8.))
                    })
            },
            menu,
            phase,
            3,
        );
        div().child(backdrop).child(popup)
    });

    div().absolute().top(px(7. * zoom)).left(px(10. * zoom)).h(px(26. * zoom))
        .flex().items_center().gap(px(3. * zoom))
        .child(icon_btn)
        .child(pill_btn)
        .children(popover)
}
