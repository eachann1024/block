//! Block menu rendering: the floating list and its rows.
use gpui::{AnyElement, CursorStyle, Entity, FontWeight, MouseButton, div, prelude::*, px, transparent_black};
use crate::{editor::{menu_style, view::Editor}, icons::{Icon, IconColors, icon}, theme::Theme};
use super::{BlockMenu, Cmd, MENU_WIDTH};

pub(in crate::editor) fn overlay(editor: Entity<Editor>, menu: BlockMenu, entries: Vec<Option<Cmd>>) -> AnyElement {
    let theme = Theme::ocean(); let id = menu.block; let mode = menu.mode;
    let mut list = menu_style::shell(("block-menu", menu.sub as usize), &theme).w(px(MENU_WIDTH)).max_h(px(384.)).overflow_y_scroll()
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation());
    let mut position = 0;
    for (index, entry) in entries.into_iter().enumerate() {
        match entry {
            None => list = list.child(menu_style::group_gap()),
            Some(Cmd::Header(title)) => list = list.child(div().px(px(8.)).pt(px(4.)).pb(px(2.)).text_size(px(12.))
                .text_color(theme.text_secondary).child(title)),
            Some(cmd) => {
                let active = position == menu.index; position += 1;
                let item_editor = editor.clone();
                list = list.child(row(("block-menu-item", index), cmd, active, &theme)
                    .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                        item_editor.update(cx, |this, cx| this.run_block_cmd(id, mode, cmd, window, cx));
                        cx.stop_propagation();
                    }));
            }
        }
    }
    gpui::deferred(gpui::anchored().position(menu.anchor).snap_to_window_with_margin(px(8.)).child(list)).into_any_element()
}
/// One menu row: optional leading icon, label, then a check, a current value with a chevron, or nothing.
fn row(id: (&'static str, usize), cmd: Cmd, active: bool, theme: &Theme) -> gpui::Stateful<gpui::Div> {
    let tint = |line| IconColors { line, fill: transparent_black(), accent: theme.accent };
    let color = if cmd.danger() { menu_style::danger_color() } else { theme.text };
    let hover = theme.hover;
    div().id(id).w_full().min_h(theme.menu_item_height).flex_none().px(px(8.)).rounded(px(4.))
        .flex().items_center().gap(px(8.)).text_color(color)
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(hover))
        .when(active, |s| s.bg(theme.selected))
        .child(div().size(px(16.)).flex_none().flex().items_center().justify_center()
            .children(cmd.leading().map(|g| icon(g, px(16.), tint(if cmd.danger() { menu_style::danger_color() } else { theme.text_secondary })))))
        .child(div().flex_1().min_w_0().when(cmd.checked(), |s| s.font_weight(FontWeight(600.))).child(cmd.label()))
        .when_some(cmd.hint(), |s, hint| s.child(div().text_size(px(12.)).text_color(theme.text_secondary).child(hint)))
        .when(cmd.checked(), |s| s.child(icon(Icon::Check, px(16.), tint(theme.accent))))
        .when_some(match cmd { Cmd::Open(_, value) => Some(value), _ => None }, |s, value| s
            .child(div().text_size(px(12.)).text_color(theme.text_secondary).child(value))
            .child(icon(Icon::ChevronRight, px(14.), tint(theme.text_secondary))))
}
