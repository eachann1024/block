//! Block menu rendering: the floating slash/insert list and separators.
use gpui::{AnyElement, CursorStyle, Entity, FontWeight, MouseButton, div, prelude::*, px, transparent_black};
use crate::{editor::{menu_style, view::Editor}, icons::{Icon, IconColors, icon}, theme::Theme};
use super::{BlockMenu, Cmd, SLASH_MENU_WIDTH};

const WINDOW_MARGIN: f32 = 8.;
const MAX_MENU_HEIGHT: f32 = 384.;

pub(in crate::editor) fn overlay(editor: Entity<Editor>, menu: BlockMenu, entries: Vec<Option<Cmd>>) -> AnyElement {
    let theme = Theme::ocean();
    let id = menu.block;
    let mode = menu.mode;
    let selectable_count = entries.iter().flatten().count();
    let active_idx = menu.index.min(selectable_count.saturating_sub(1));
    let mut list = menu_style::shell("block-menu", &theme)
        .min_w(px(SLASH_MENU_WIDTH)).w(px(SLASH_MENU_WIDTH)).max_h(px(MAX_MENU_HEIGHT)).overflow_y_scroll()
        .rounded(px(10.))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation());
    let mut position = 0;
    for (index, entry) in entries.iter().copied().enumerate() {
        match entry {
            None => list = list.child(menu_style::separator(&theme)),
            Some(cmd) => {
                let active = position == active_idx;
                position += 1;
                let item_editor = editor.clone();
                let item = row(("block-menu-item", index), cmd, active, &theme)
                    .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                        item_editor.update(cx, |this, cx| this.run_block_cmd(id, mode, cmd, window, cx));
                        cx.stop_propagation();
                    });
                list = list.child(item);
            }
        }
    }
    gpui::deferred(
        gpui::anchored().position(menu.anchor).snap_to_window_with_margin(px(WINDOW_MARGIN)).child(list)
    ).with_priority(2).into_any_element()
}

/// One menu row: optional leading icon, label, then a check or shortcut hint.
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
}
