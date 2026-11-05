//! Universal font picker dropdown: trigger + search + virtualized fonts list.
use std::cell::RefCell;
use std::collections::HashMap;
use std::rc::Rc;
use gpui::{
    Bounds, CursorStyle, ElementId, IntoElement, MouseButton, Pixels,
    SharedString, Size, Stateful, anchored, deferred, div, point, prelude::*, px,
    uniform_list,
};
use gpui_component::input::Input;
use crate::app::Workspace;
use crate::icons::{Icon, IconColors, icon};
use crate::theme::{Theme, tr};
use crate::ui::dropdown::{self, palette};
use crate::ui::font_picker_cache::FlatFontItem;

pub const FONT_MENU_WIDTH: f32 = 300.;
const ITEM_HEIGHT: f32 = 34.;
const MAX_VISIBLE_ROWS: usize = 7;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum FontPickerRole {
    Body,
    Code,
}

thread_local! {
    static TRIGGER_BOUNDS: RefCell<HashMap<FontPickerRole, (Bounds<Pixels>, Size<Pixels>)>> = RefCell::new(HashMap::new());
}

pub fn record_trigger_bounds(role: FontPickerRole, bounds: Bounds<Pixels>, vp: Size<Pixels>) {
    TRIGGER_BOUNDS.with(|slot| { slot.borrow_mut().insert(role, (bounds, vp)); });
}

pub fn get_trigger_bounds(role: FontPickerRole) -> Option<(Bounds<Pixels>, Size<Pixels>)> {
    TRIGGER_BOUNDS.with(|slot| slot.borrow().get(&role).copied())
}

pub fn font_picker_trigger(
    id: impl Into<ElementId>,
    role: FontPickerRole,
    open: bool,
    display_name: SharedString,
    display_family: SharedString,
    cx: &mut gpui::Context<Workspace>,
) -> Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let border_color = if open { theme.accent } else { theme.btn_border };
    let chevron_icon = if open { Icon::ChevronUp } else { Icon::ChevronDown };
    div()
        .id(id).w(px(248.)).h(px(36.)).px(px(10.)).pl(px(12.))
        .rounded(crate::ui::radius::CONTROL).bg(theme.shell)
        .border_1().border_color(border_color)
        .flex().items_center().justify_between().cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(theme.hover))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
            cx.stop_propagation();
            this.toggle_font_picker(role, window, cx);
        }))
        .child(
            div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
                .text_size(px(15.)).font_family(display_family).text_color(theme.text).child(display_name),
        )
        .child(div().flex_none().ml(px(8.)).child(icon(chevron_icon, px(14.), theme.mono_icon_colors(theme.text_secondary))))
        .child(
            gpui::canvas(
                move |bounds, window, _| record_trigger_bounds(role, bounds, window.viewport_size()),
                |_, _, _, _| {},
            )
            .absolute().top_0().left_0().size_full(),
        )
}

pub fn font_picker_popover(
    role: FontPickerRole,
    search_input: &gpui::Entity<gpui_component::input::InputState>,
    scroll_handle: &gpui::UniformListScrollHandle,
    items: Rc<[FlatFontItem]>,
    cx: &mut gpui::Context<Workspace>,
) -> gpui::Div {
    let theme = Theme::ocean();
    let pal = palette();
    let measured = get_trigger_bounds(role);
    let max_h = measured.map(|(_, vp)| (f32::from(vp.height) - 16.).clamp(80., dropdown::MAX_HEIGHT)).unwrap_or(dropdown::MAX_HEIGHT);
    let anchor = measured.map(|(b, vp)| dropdown::anchor_below_right(b, FONT_MENU_WIDTH, max_h, Some(vp))).unwrap_or(point(px(100.), px(100.)));

    let search_bar = div()
        .flex_none().flex().items_center().gap(px(8.)).px(px(8.)).pb(px(4.)).mb(px(4.))
        .border_b_1().border_color(pal.sep)
        .child(icon(Icon::FindSearch, px(14.), theme.mono_icon_colors(theme.text_secondary)))
        .child(div().flex_1().min_w_0().child(Input::new(search_input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).text_size(px(14.))));

    let list_h = if items.is_empty() {
        px(48.)
    } else {
        px((items.len().min(MAX_VISIBLE_ROWS) as f32) * ITEM_HEIGHT)
    };

    let list_element = if items.is_empty() {
        div().w_full().h(list_h).flex().items_center().px(px(10.))
            .text_size(px(14.)).text_color(pal.muted)
            .child(tr("没有匹配的字体", "No matching fonts"))
            .into_any_element()
    } else {
        let items_clone = items.clone();
        uniform_list(
            ("font-picker-uniform-list", role as usize),
            items.len(),
            cx.processor(move |_this, range: std::ops::Range<usize>, _window, cx| {
                let theme = Theme::ocean();
                let pal = palette();
                range.map(|ix| {
                    match &items_clone[ix] {
                        FlatFontItem::Header(title) => {
                            let title = title.clone();
                            div()
                                .id(ElementId::NamedInteger("font-hdr".into(), ix as u64))
                                .w_full().h(px(ITEM_HEIGHT)).px(px(10.))
                                .flex().items_center()
                                .text_size(px(13.)).font_weight(gpui::FontWeight::MEDIUM)
                                .text_color(pal.muted)
                                .child(title)
                                .into_any_element()
                        }
                        FlatFontItem::Font { id, name, selected } => {
                            let active = *selected;
                            let id = id.clone();
                            let name = name.clone();
                            let check_colors = IconColors {
                                line: theme.accent,
                                fill: gpui::transparent_black(),
                                accent: theme.accent,
                            };
                            div()
                                .id(ElementId::NamedInteger("font-row".into(), ix as u64))
                                .w_full().h(px(ITEM_HEIGHT)).px(px(10.)).rounded(crate::ui::radius::CONTROL)
                                .flex().items_center().justify_between().cursor(CursorStyle::PointingHand)
                                .hover(move |s| s.bg(pal.hover_bg))
                                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                                    cx.stop_propagation();
                                    match role {
                                        FontPickerRole::Body => this.select_editor_font(id.clone(), window, cx),
                                        FontPickerRole::Code => this.select_code_font(id.clone(), window, cx),
                                    }
                                }))
                                .child(
                                    div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
                                        .text_size(px(15.)).text_color(pal.text).child(name),
                                )
                                .when(active, |r| r.child(div().ml(px(8.)).flex_none().child(icon(Icon::Check, px(16.), check_colors))))
                                .into_any_element()
                        }
                    }
                }).collect::<Vec<_>>()
            }),
        )
        .track_scroll(scroll_handle)
        .w_full()
        .h(list_h)
        .into_any_element()
    };

    let menu = div()
        .id(("font-picker-menu", role as usize))
        .w(px(FONT_MENU_WIDTH)).max_h(px(max_h)).p(px(6.))
        .rounded(crate::ui::radius::SURFACE).bg(pal.bg).shadow(dropdown::shadows())
        .occlude().flex().flex_col()
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation())
        .on_scroll(|_, _, cx| cx.stop_propagation())
        .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, _, cx| {
            if event.keystroke.key == "escape" {
                cx.stop_propagation();
                this.close_font_picker(cx);
            }
        }))
        .child(search_bar).child(list_element);

    let backdrop = deferred(
        anchored().position(point(px(0.), px(0.))).child(
            div().id("font-picker-backdrop").w(px(10000.)).h(px(10000.)).bg(gpui::transparent_black()).occlude()
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.close_font_picker(cx); }))
                .on_mouse_down(MouseButton::Right, cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.close_font_picker(cx); })),
        )
    ).with_priority(2);

    div().child(backdrop).child(dropdown::deferred_popover(anchor, menu, 3))
}
