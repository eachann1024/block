use std::cell::RefCell;
use gpui::{
    App, BoxShadow, CursorStyle, Div, ElementId, Entity, FontWeight, Hsla, IntoElement,
    MouseButton, SharedString, Stateful, div,
    point, prelude::*, px, rgb, rgba, transparent_black,
};
pub use crate::ui::placement::{
    AlignX, PlacementSpec, anchored_motion, anchored_motion_with, max_height_for_spec,
    smart_anchored_with,
};
use crate::{
    app::Workspace,
    editor::Editor,
    icons::{Icon, IconColors, icon, stateful_icon},
    theme::is_dark,
};

pub const MIN_WIDTH: f32 = 160.;
pub const MAX_WIDTH: f32 = 320.;
pub const MAX_HEIGHT: f32 = 304.;
pub const ROW_HEIGHT: f32 = 28.;

#[derive(Clone, Copy)]
pub struct DropdownPalette {
    pub bg: Hsla,
    pub text: Hsla,
    pub icon: Hsla,
    pub icon_fill: Hsla,
    pub icon_accent: Hsla,
    pub muted: Hsla,
    pub hover_bg: Hsla,
    pub checked_bg: Hsla,
    pub sep: Hsla,
    pub danger_fg: Hsla,
    pub danger_fill: Hsla,
    pub danger_hover_bg: Hsla,
    pub danger_hover_fg: Hsla,
}

pub fn palette() -> DropdownPalette {
    if is_dark() {
        DropdownPalette {
            bg: rgb(0x222222).into(), text: rgb(0xececec).into(), icon: rgb(0xb4b4b4).into(),
            icon_fill: rgb(0x31456a).into(), icon_accent: rgb(0x779de8).into(),
            muted: rgb(0x8e8d88).into(), hover_bg: rgba(0xffffff12).into(), checked_bg: rgba(0xffffff12).into(),
            sep: rgba(0xffffff1a).into(), danger_fg: rgb(0xf28b82).into(), danger_fill: rgba(0xf28b822e).into(),
            danger_hover_bg: rgb(0xd03b3b).into(), danger_hover_fg: rgb(0xffffff).into(),
        }
    } else {
        DropdownPalette {
            bg: rgb(0xffffff).into(), text: rgb(0x1f1e1d).into(), icon: rgb(0x3d3c39).into(),
            icon_fill: rgb(0xc9d8f3).into(), icon_accent: rgb(0x547dd0).into(),
            muted: rgb(0x898781).into(), hover_bg: rgba(0x0b0b0b0d).into(), checked_bg: rgba(0x0000000d).into(),
            sep: rgba(0x0b0b0b1a).into(), danger_fg: rgb(0x8e2626).into(), danger_fill: rgba(0xd03b3b24).into(),
            danger_hover_bg: rgb(0xd03b3b).into(), danger_hover_fg: rgb(0xffffff).into(),
        }
    }
}

pub fn shadows() -> Vec<BoxShadow> {
    if is_dark() {
        vec![
            BoxShadow { color: rgba(0xffffff18).into(), offset: point(px(0.), px(0.)), blur_radius: px(0.), spread_radius: px(1.), inset: false },
            BoxShadow { color: rgba(0x00000066).into(), offset: point(px(0.), px(8.)), blur_radius: px(24.), spread_radius: px(0.), inset: false },
            BoxShadow { color: rgba(0x00000040).into(), offset: point(px(0.), px(2.)), blur_radius: px(6.), spread_radius: px(0.), inset: false },
        ]
    } else {
        vec![
            BoxShadow { color: rgba(0x00000014).into(), offset: point(px(0.), px(0.)), blur_radius: px(0.), spread_radius: px(1.), inset: false },
            BoxShadow { color: rgba(0x0000001f).into(), offset: point(px(0.), px(8.)), blur_radius: px(24.), spread_radius: px(0.), inset: false },
            BoxShadow { color: rgba(0x00000014).into(), offset: point(px(0.), px(2.)), blur_radius: px(6.), spread_radius: px(0.), inset: false },
        ]
    }
}

pub fn shell_unbounded(id: impl Into<ElementId>) -> Stateful<Div> {
    let p = palette();
    let id: ElementId = id.into();
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    div().id(id).relative().children(probe).max_h(px(MAX_HEIGHT)).overflow_y_scroll().p(px(4.)).rounded(crate::ui::radius::SURFACE).bg(p.bg).shadow(shadows()).occlude()
        .flex().flex_col().gap(px(2.)).text_size(px(14.)).line_height(px(20.)).text_color(p.text)
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation())
        .on_scroll(|_, _, cx| cx.stop_propagation())
}

pub fn shell(id: impl Into<ElementId>) -> Stateful<Div> {
    shell_unbounded(id).min_w(px(MIN_WIDTH)).max_w(px(MAX_WIDTH))
}

pub fn line_icon(glyph: Icon, size: f32, color: Hsla) -> impl IntoElement {
    icon(glyph, px(size), IconColors { line: color, fill: transparent_black(), accent: color })
}

#[derive(Default)]
pub struct RowSpec {
    pub icon: Option<Icon>,
    pub label: SharedString,
    pub checked: bool,
    pub highlighted: bool,
    pub danger: bool,
    pub disabled: bool,
    pub hint: Option<SharedString>,
    pub chevron: bool,
}

pub fn row_with(id: impl Into<ElementId>, spec: RowSpec) -> gpui_base::Button {
    row_with_colors(id, spec, None)
}

pub fn row_with_colors(id: impl Into<ElementId>, spec: RowSpec, colors: Option<(IconColors, IconColors)>) -> gpui_base::Button {
    let p = palette();
    let id: ElementId = id.into();
    let group = SharedString::from(format!("dd-row-{id:?}"));
    let active = spec.checked || spec.highlighted;
    // focusable(false): menu keys stay on menu_nav, and the editor keeps its input method.
    let row = gpui_base::Button::new(id).focusable(false).role(gpui::Role::MenuItem).disabled(spec.disabled).selected(spec.checked)
        .group(group.clone()).h(px(ROW_HEIGHT)).w_full().flex_none().px(px(10.)).gap(px(8.)).rounded(crate::ui::radius::CONTROL)
        .flex().items_center().text_size(px(14.)).line_height(px(20.)).when(spec.disabled, |s| s.opacity(0.4))
        .when(spec.checked, |s| s.aria_selected(true));
    let row = if spec.danger {
        let fg = if spec.highlighted && !spec.disabled { p.danger_hover_fg } else { p.danger_fg };
        let idle_c = IconColors { line: p.danger_fg, fill: transparent_black(), accent: p.danger_fg };
        let hov_c = IconColors { line: p.danger_hover_fg, fill: rgba(0xffffff33).into(), accent: p.danger_hover_fg };
        let lead = div().relative().size(px(16.)).flex_none().flex().items_center().justify_center()
            .when_some(spec.icon, |slot, g| {
                if spec.disabled {
                    slot.child(icon(g, px(16.), idle_c))
                } else {
                    slot.child(stateful_icon(group.clone(), g, px(16.), active, idle_c, hov_c))
                }
            });
        row.text_color(fg)
            .when(spec.highlighted && !spec.disabled, move |s| s.bg(p.danger_hover_bg))
            .when(!spec.disabled, move |s| s.cursor(CursorStyle::PointingHand).hover(move |s| s.bg(p.danger_hover_bg).text_color(p.danger_hover_fg)))
            .child(lead)
    } else {
        let (idle_c, active_c) = colors.unwrap_or((
            IconColors { line: p.icon, fill: transparent_black(), accent: p.icon },
            IconColors { line: p.icon, fill: p.icon_fill, accent: p.icon_accent },
        ));
        let lead = div().size(px(16.)).flex_none().flex().items_center().justify_center()
            .when_some(spec.icon, |slot, g| {
                if spec.disabled {
                    slot.child(icon(g, px(16.), idle_c))
                } else {
                    slot.child(stateful_icon(group.clone(), g, px(16.), active, idle_c, active_c))
                }
            });
        row.text_color(p.text)
            .when(spec.checked, move |s| s.bg(p.checked_bg))
            .when(spec.highlighted && !spec.disabled, move |s| s.bg(p.hover_bg))
            .when(!spec.disabled, move |s| s.cursor(CursorStyle::PointingHand).hover(move |s| s.bg(p.hover_bg)))
            .child(lead)
    };
    let hint_group = group.clone();
    let danger_hint = spec.danger && !spec.disabled;
    let hint_active = spec.highlighted;
    row.child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(spec.label))
        .when_some(spec.hint, move |r, h| {
            r.child(if danger_hint {
                crate::ui::shortcut_label::shortcut_hint_group(h, hint_group, p.muted, p.danger_hover_fg, hint_active)
            } else {
                crate::ui::shortcut_label::shortcut_hint(h, p.muted)
            })
        })
        .when(spec.chevron, |r| r.child(div().ml_auto().pl(px(8.)).flex_none().flex().items_center().child(line_icon(Icon::TbChevronRight, 12., p.muted))))
        .when(spec.checked, |r| r.child(div().ml_auto().pl(px(12.)).flex_none().flex().items_center()
            .child(div().size(px(16.)).flex().items_center().justify_center().child(line_icon(Icon::Check, 16., p.text)))))
}

pub fn separator() -> impl IntoElement {
    gpui_component::separator::Separator::horizontal()
        .color(palette().sep)
        .relative()
        .h(px(1.))
        .my(px(4.))
        .w_full()
        .flex_none()
}

pub fn group_label(label: impl Into<SharedString>) -> Div {
    div().px(px(10.)).py(px(4.)).flex_none().text_size(px(13.)).line_height(px(17.))
        .font_weight(FontWeight::MEDIUM).text_color(palette().muted).child(label.into())
}

thread_local! {
    static EDITORS: RefCell<Vec<gpui::WeakEntity<Editor>>> = const { RefCell::new(Vec::new()) };
    static WORKSPACES: RefCell<Vec<gpui::WeakEntity<Workspace>>> = const { RefCell::new(Vec::new()) };
    static CLOSERS: RefCell<Vec<fn() -> bool>> = const { RefCell::new(Vec::new()) };
}

pub fn register_editor(editor: &Entity<Editor>) {
    let weak = editor.downgrade();
    EDITORS.with(|slot| {
        let mut list = slot.borrow_mut();
        list.retain(|w| w.upgrade().is_some());
        if !list.iter().any(|w| w.entity_id() == weak.entity_id()) {
            list.push(weak);
        }
    });
}

pub fn register_workspace(ws: &Entity<Workspace>) {
    let weak = ws.downgrade();
    WORKSPACES.with(|slot| {
        let mut list = slot.borrow_mut();
        list.retain(|w| w.upgrade().is_some());
        if !list.iter().any(|w| w.entity_id() == weak.entity_id()) {
            list.push(weak);
        }
    });
}

pub fn register_closer(closer: fn() -> bool) {
    CLOSERS.with(|slot| {
        let mut list = slot.borrow_mut();
        if !list.iter().any(|f| std::ptr::fn_addr_eq(*f, closer)) {
            list.push(closer);
        }
    });
}

pub fn dismiss_all(cx: &mut App) {
    cx.defer(move |cx| {
        let mut stateless_closed = false;
        CLOSERS.with(|slot| {
            for closer in slot.borrow().iter() {
                if closer() {
                    stateless_closed = true;
                }
            }
        });
        let editors = EDITORS.with(|slot| slot.borrow().clone());
        for weak in editors {
            if let Some(entity) = weak.upgrade() {
                entity.update(cx, |ed, cx| {
                    if ed.dismiss_dropdowns() || stateless_closed {
                        cx.notify();
                    }
                });
            }
        }
        let workspaces = WORKSPACES.with(|slot| slot.borrow().clone());
        for weak in workspaces {
            if let Some(entity) = weak.upgrade() {
                entity.update(cx, |ws, cx| {
                    if ws.dismiss_dropdowns(cx) || stateless_closed {
                        cx.notify();
                    }
                });
            }
        }
    });
}
