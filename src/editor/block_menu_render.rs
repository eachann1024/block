//! Block menu rendering: the floating list, slash separators, and cascading handle submenu.
use gpui::{AnyElement, CursorStyle, Entity, FontWeight, MouseButton, Point, Pixels, div, point, prelude::*, px, transparent_black};
use crate::{editor::{menu_style, view::Editor}, icons::{Icon, IconColors, icon}, theme::Theme};
use super::{BlockMenu, Cmd, HANDLE_MENU_WIDTH, Mode, SLASH_MENU_WIDTH, SUBMENU_WIDTH, Sub, sub_entries};

const SUBMENU_GAP: f32 = 4.;
const WINDOW_MARGIN: f32 = 8.;
const MAX_MENU_HEIGHT: f32 = 384.;

pub(in crate::editor) fn overlay(editor: Entity<Editor>, menu: BlockMenu, entries: Vec<Option<Cmd>>) -> AnyElement {
    let theme = Theme::ocean();
    let id = menu.block;
    let mode = menu.mode;
    let is_slash = mode == Mode::Slash;
    let width = if is_slash { SLASH_MENU_WIDTH } else { HANDLE_MENU_WIDTH };
    let selectable_count = entries.iter().flatten().filter(|c| !matches!(c, Cmd::Header(_))).count();
    let active_idx = menu.index.min(selectable_count.saturating_sub(1));
    let mut list = menu_style::shell("block-menu", &theme)
        .min_w(px(width)).w(px(width)).max_h(px(MAX_MENU_HEIGHT)).overflow_y_scroll()
        .when(is_slash, |s| s.rounded(px(10.)))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation());
    let mut position = 0;
    for (index, entry) in entries.iter().copied().enumerate() {
        match entry {
            None => list = list.child(if is_slash { menu_style::separator(&theme) } else { menu_style::group_gap() }),
            Some(Cmd::Header(title)) => list = list.child(div().px(px(8.)).pt(px(4.)).pb(px(2.)).text_size(px(12.))
                .text_color(theme.text_secondary).child(title)),
            Some(cmd) => {
                let row_pos = position;
                let active = (row_pos == active_idx && menu.sub == Sub::Root)
                    || matches!(cmd, Cmd::Open(sub) if sub == menu.sub);
                position += 1;
                let item_editor = editor.clone();
                let hover_editor = editor.clone();
                let mut item = row(("block-menu-item", index), cmd, active, &theme)
                    .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                        item_editor.update(cx, |this, cx| this.run_block_cmd(id, mode, cmd, window, cx));
                        cx.stop_propagation();
                    });
                if let Cmd::Open(sub) = cmd {
                    item = item.on_hover(move |hovered, window, cx| {
                        if *hovered {
                            hover_editor.update(cx, |this, cx| this.open_submenu(row_pos, sub, window, cx));
                        }
                    });
                }
                list = list.child(item);
            }
        }
    }
    let root = gpui::deferred(
        gpui::anchored().position(menu.anchor).snap_to_window_with_margin(px(WINDOW_MARGIN)).child(list)
    ).with_priority(2).into_any_element();
    if is_slash || menu.sub == Sub::Root {
        return root;
    }
    let sub_items = sub_entries(&menu);
    let sub_active = menu.sub_index.min(sub_items.len().saturating_sub(1));
    let mut sub_list = menu_style::shell(("block-submenu", menu.sub as usize), &theme)
        .min_w(px(SUBMENU_WIDTH)).w(px(SUBMENU_WIDTH)).max_h(px(MAX_MENU_HEIGHT)).overflow_y_scroll()
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation());
    for (idx, cmd) in sub_items.into_iter().enumerate() {
        let item_editor = editor.clone();
        sub_list = sub_list.child(row(("block-submenu-item", idx), cmd, idx == sub_active, &theme)
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                item_editor.update(cx, |this, cx| this.run_block_cmd(id, mode, cmd, window, cx));
                cx.stop_propagation();
            }));
    }
    let sub_anchor = submenu_anchor(&menu, &entries);
    let sub_el = gpui::deferred(
        gpui::anchored().position(sub_anchor).snap_to_window_with_margin(px(WINDOW_MARGIN)).child(sub_list)
    ).with_priority(3).into_any_element();
    div().absolute().top_0().left_0().child(root).child(sub_el).into_any_element()
}

/// Anchors the cascading submenu to the right of its trigger row, flipping left when the right side lacks room.
fn submenu_anchor(menu: &BlockMenu, entries: &[Option<Cmd>]) -> Point<Pixels> {
    let mut row_y = 0.0_f32;
    let mut total_h = 14.0_f32;
    for (idx, entry) in entries.iter().enumerate() {
        if idx > 0 { total_h += 2.; }
        let h = match entry {
            None => 2.,
            Some(Cmd::Header(_)) => 22.,
            Some(cmd) => {
                if *cmd == Cmd::Open(menu.sub) { row_y = total_h - 7.; }
                32.
            }
        };
        total_h += h;
    }
    let root_h = total_h.min(MAX_MENU_HEIGHT);
    let vw = f32::from(menu.viewport.width);
    let vh = f32::from(menu.viewport.height);
    let mut root_x = f32::from(menu.anchor.x);
    let mut root_y = f32::from(menu.anchor.y);
    if vw > 0. { root_x = root_x.min(vw - HANDLE_MENU_WIDTH - WINDOW_MARGIN).max(WINDOW_MARGIN); }
    if vh > 0. { root_y = root_y.min(vh - root_h - WINDOW_MARGIN).max(WINDOW_MARGIN); }
    let right_x = root_x + HANDLE_MENU_WIDTH + SUBMENU_GAP;
    let sub_x = if vw > 0. && right_x + SUBMENU_WIDTH + WINDOW_MARGIN > vw {
        (root_x - SUBMENU_WIDTH - SUBMENU_GAP).max(WINDOW_MARGIN)
    } else {
        right_x
    };
    point(px(sub_x), px(root_y + row_y))
}

/// One menu row: optional leading icon, label, then a check, shortcut hint, or submenu chevron.
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
        .when(matches!(cmd, Cmd::Open(_)), |s| s.child(icon(Icon::ChevronRight, px(14.), tint(theme.text_secondary))))
}
