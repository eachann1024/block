//! Table controls shown while the pointer is over a table: row and column capsule handles
//! on the grid border, the bottom add-row bar, the right add-column bar, the top-right more button, drop line, and menus.
use gpui::{AnyElement, CursorStyle, Entity, FontWeight, MouseButton, Pixels, Point, Stateful, Div, div, point, prelude::*, px, rgb, rgba};
use super::{menu_style, table_actions::{TableChrome, TableCmd, TableDrag, TableMenu, TableSel, menu_items}, view::Editor};
use crate::{icons::{Icon, icon}, theme::{Theme, build_tooltip, is_dark, tr}};

thread_local! {
    static PILL_HOVER: std::cell::Cell<Option<(u64, TableSel)>> = const { std::cell::Cell::new(None) };
}

fn floating(position: Point<Pixels>, child: impl IntoElement) -> AnyElement {
    gpui::deferred(gpui::anchored().position(position).child(child)).into_any_element()
}

/// Row/column capsule handle: compact 24x8 / 8x24 pill when idle, 24x14 / 14x24 with 6-dot grip when hovered or active.
fn capsule(id: (&'static str, usize), vertical: bool, expanded: bool, tip: &'static str) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let (w, h) = match (vertical, expanded) {
        (false, false) => (px(24.), px(8.)),
        (false, true) => (px(24.), px(14.)),
        (true, false) => (px(8.), px(24.)),
        (true, true) => (px(14.), px(24.)),
    };
    let bg: gpui::Hsla = if dark {
        if expanded { rgb(0x262626).into() } else { rgb(0x1e1e1e).into() }
    } else {
        rgb(0xffffff).into()
    };
    let border: gpui::Hsla = if dark {
        if expanded { rgb(0x6b6b6b).into() } else { rgb(0x555555).into() }
    } else {
        rgba(0x1f1e1d52).into()
    };
    let dot_color: gpui::Hsla = if dark { theme.text } else { rgba(0x1f1e1d8c).into() };
    let radius = if expanded { px(7.) } else { px(999.) };
    div().id(id).w(w).h(h).flex_none().rounded(radius).flex().items_center().justify_center()
        .bg(bg).border_1().border_color(border).cursor(CursorStyle::OpenHand)
        .when(expanded, |el| el.child(icon(
            if vertical { Icon::Drag } else { Icon::GripHorizontal },
            px(12.),
            theme.icon_colors(dot_color),
        )))
        .tooltip(move |window, cx| build_tooltip(tip, window, cx))
}

fn add_bar(id: &'static str, w: Pixels, h: Pixels, tip: &'static str) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let bg: gpui::Hsla = if dark { rgb(0x262626).into() } else { rgba(0x1f1e1d0d).into() };
    let hover_bg: gpui::Hsla = if dark { rgb(0x303030).into() } else { rgba(0x1f1e1d1a).into() };
    let active_bg: gpui::Hsla = if dark { rgb(0x3a3a3a).into() } else { rgba(0x1f1e1d33).into() };
    let fg: gpui::Hsla = if dark { theme.text_secondary } else { rgb(0x73726c).into() };
    div().id(id).w(w).h(h).flex().items_center().justify_center()
        .rounded(px(4.)).bg(bg).cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg)).active(move |s| s.bg(active_bg))
        .child(icon(Icon::Plus, px(12.), theme.icon_colors(fg)))
        .tooltip(move |window, cx| build_tooltip(tip, window, cx))
}

fn more_button(open: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let (bg, hover_bg, border, fg, hover_fg): (gpui::Hsla, gpui::Hsla, gpui::Hsla, gpui::Hsla, gpui::Hsla) = if dark {
        let b = if open { theme.selected } else { theme.paper };
        let br = if open { theme.accent } else { theme.btn_border };
        let f = if open { theme.text } else { theme.text_secondary };
        (b, theme.hover, br, f, theme.text)
    } else {
        let b = if open { rgb(0xf7f7f7).into() } else { rgb(0xffffff).into() };
        let f = if open { rgb(0x0b0b0b).into() } else { rgb(0x52514e).into() };
        (b, rgb(0xf7f7f7).into(), rgba(0x0b0b0b1a).into(), f, rgb(0x0b0b0b).into())
    };
    div().id("table-more").group("hover-icon").w(px(28.)).h(px(28.)).flex_none().rounded(px(8.)).flex().items_center().justify_center()
        .bg(bg).border_1().border_color(border).cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .child(div().relative().flex_none().size(px(16.))
            .child(div().group_hover("hover-icon", |s| s.invisible()).child(icon(Icon::More, px(16.), theme.icon_colors(fg))))
            .child(div().absolute().top_0().left_0().invisible().group_hover("hover-icon", |s| s.visible()).child(icon(Icon::More, px(16.), theme.icon_colors(hover_fg)))))
        .tooltip(|window, cx| build_tooltip(tr("表格选项", "Table options"), window, cx))
}

fn menu_item(id: (&'static str, usize), cmd: TableCmd, disabled: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    let fg = if cmd.danger() { menu_style::danger_color() } else { theme.text };
    let hover_bg = theme.hover;
    div().id(id).w_full().min_h(theme.menu_item_height).flex_none().px(px(10.)).rounded(px(4.))
        .flex().items_center().gap(px(12.)).text_size(theme.ui_font).font_weight(FontWeight::MEDIUM).text_color(fg)
        .when(disabled, |s| s.opacity(0.4))
        .when(!disabled, move |s| s.cursor(CursorStyle::PointingHand).hover(move |s| s.bg(hover_bg)))
        .child(icon(cmd.icon(), px(18.), theme.icon_colors(fg)))
        .child(div().flex_1().min_w_0().child(cmd.label()))
}

pub(super) fn overlay(editor: Entity<Editor>, chrome: &TableChrome, sel: Option<(u64, TableSel)>,
    menu: Option<TableMenu>, drag: Option<TableDrag>, dims: (usize, usize)) -> Vec<AnyElement> {
    let theme = Theme::ocean();
    let id = chrome.block;
    let active_sel = sel.filter(|(b, _)| *b == id).map(|(_, s)| s)
        .or_else(|| menu.filter(|m| m.block == id).map(|m| m.sel));
    let (r, c) = match active_sel {
        Some(TableSel::Column(col)) => (0, col.min(chrome.columns.len().saturating_sub(1))),
        Some(TableSel::Row(row)) => (row.min(chrome.rows.len().saturating_sub(1)), 0),
        Some(TableSel::Whole) => (0, 0),
        _ => chrome.cell,
    };
    let is_active = |target: TableSel| sel == Some((id, target)) || menu.is_some_and(|m| m.block == id && m.sel == target);
    let is_hovered = |target: TableSel| PILL_HOVER.with(|h| h.get() == Some((id, target)));
    let mut out: Vec<AnyElement> = Vec::new();
    let Some(&(left, right)) = chrome.columns.get(c) else { return out };
    let Some(&(top, bottom)) = chrome.rows.get(r) else { return out };
    let table = chrome.bounds;
    let dragging = drag.filter(|d| d.moved && d.block == id);
    let follow = dragging.and_then(|d| d.position.map(|p| (d.sel, p)));
    let view = chrome.viewport;
    let show_column = dragging.is_none_or(|d| matches!(d.sel, TableSel::Column(_))) && table.top() - px(8.) >= view.top();
    let row_mid = match follow { Some((TableSel::Row(_), p)) => p.y, _ => (top + bottom) / 2. };
    let show_row = dragging.is_none_or(|d| matches!(d.sel, TableSel::Row(_)))
        && row_mid - px(13.) >= view.top() && row_mid + px(13.) <= view.bottom();
    if show_column {
        let x = match follow { Some((TableSel::Column(_), p)) => p.x, _ => (left + right) / 2. };
        let col_sel = TableSel::Column(c);
        let expanded = is_active(col_sel) || is_hovered(col_sel);
        let pos = point(x - px(12.), if expanded { table.top() - px(7.) } else { table.top() - px(4.) });
        let col_editor = editor.clone();
        let hover_editor = editor.clone();
        out.push(floating(pos,
            capsule(("table-col", c), false, expanded, tr("点击查看列操作 · 拖动移动", "Click for column options · drag to move"))
                .on_hover(move |hovered: &bool, _, cx| {
                    let cur = PILL_HOVER.with(|h| h.get());
                    let next = if *hovered { Some((id, col_sel)) } else if cur == Some((id, col_sel)) { None } else { cur };
                    if cur != next { PILL_HOVER.with(|h| h.set(next)); hover_editor.update(cx, |_, cx| cx.notify()); }
                })
                .on_mouse_down(MouseButton::Left, move |event, _, cx| {
                    let start = event.position;
                    col_editor.update(cx, |this, cx| {
                        this.table_sel = Some((id, col_sel)); this.table_menu = None;
                        this.table_drag = Some(TableDrag { block: id, sel: col_sel, start, moved: false, position: None });
                        cx.notify();
                    });
                    cx.stop_propagation();
                })));
    }
    if show_row {
        let y = match follow { Some((TableSel::Row(_), p)) => p.y, _ => (top + bottom) / 2. };
        let row_sel = TableSel::Row(r);
        let expanded = is_active(row_sel) || is_hovered(row_sel);
        let pos = point(if expanded { table.left() - px(7.) } else { table.left() - px(4.) }, y - px(12.));
        let row_editor = editor.clone();
        let hover_editor = editor.clone();
        out.push(floating(pos,
            capsule(("table-row", r), true, expanded, tr("点击查看行操作 · 拖动移动", "Click for row options · drag to move"))
                .on_hover(move |hovered: &bool, _, cx| {
                    let cur = PILL_HOVER.with(|h| h.get());
                    let next = if *hovered { Some((id, row_sel)) } else if cur == Some((id, row_sel)) { None } else { cur };
                    if cur != next { PILL_HOVER.with(|h| h.set(next)); hover_editor.update(cx, |_, cx| cx.notify()); }
                })
                .on_mouse_down(MouseButton::Left, move |event, _, cx| {
                    let start = event.position;
                    row_editor.update(cx, |this, cx| {
                        this.table_sel = Some((id, row_sel)); this.table_menu = None;
                        this.table_drag = Some(TableDrag { block: id, sel: row_sel, start, moved: false, position: None });
                        cx.notify();
                    });
                    cx.stop_propagation();
                })));
    }
    if dragging.is_none() {
        let whole_selected = sel == Some((id, TableSel::Whole));
        if !whole_selected && table.bottom() + px(18.) <= view.bottom() {
            let add_row = editor.clone();
            out.push(floating(point(table.left(), table.bottom() + px(2.)),
                add_bar("table-add-row", table.size.width, px(16.), tr("添加行", "Add row"))
                    .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                        add_row.update(cx, |this, cx| { this.table_command(id, TableSel::Whole, TableCmd::AppendRow, cx); window.focus(&this.focus); });
                        cx.stop_propagation();
                    })));
        }
        if !whole_selected && table.top() >= view.top() && table.bottom() <= view.bottom() {
            let add_col = editor.clone();
            out.push(floating(point(table.right() + px(2.), table.top()),
                add_bar("table-add-col", px(16.), table.size.height, tr("添加列", "Add column"))
                    .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                        add_col.update(cx, |this, cx| { this.table_command(id, TableSel::Whole, TableCmd::AppendColumn, cx); window.focus(&this.focus); });
                        cx.stop_propagation();
                    })));
        }
        if table.top() >= view.top() && table.top() + px(28.) <= view.bottom() {
            let more_editor = editor.clone();
            let cell_open = menu.is_some_and(|m| m.block == id && matches!(m.sel, TableSel::Cell(..) | TableSel::Whole));
            let btn_pos = point(table.right() + px(22.), table.top());
            let menu_anchor = point((table.right() + px(50.) - px(224.)).max(table.left()), table.top() + px(32.));
            out.push(floating(btn_pos,
                more_button(cell_open).on_mouse_down(MouseButton::Left, move |_, _, cx| {
                    more_editor.update(cx, |this, cx| {
                        let (mr, mc) = this.cell_of_caret().filter(|x| x.0 == id).map(|x| (x.1, x.2)).unwrap_or((r, c));
                        this.open_table_menu(id, TableSel::Cell(mr, mc), menu_anchor, cx);
                    });
                    cx.stop_propagation();
                })));
        }
    }
    if let Some(drag) = dragging {
        if let Some((_, edge)) = drag.position.and_then(|position| chrome.drop_target(drag, position)) {
            let (origin, size) = match drag.sel {
                TableSel::Column(_) => (point(edge - px(1.5), table.top()), gpui::size(px(3.), table.size.height)),
                _ => (point(table.left(), edge - px(1.5)), gpui::size(table.size.width, px(3.))),
            };
            out.push(floating(origin, div().w(size.width).h(size.height).rounded(px(2.)).bg(theme.accent)));
        }
    }
    if let Some(menu) = menu.filter(|m| m.block == id) {
        let items = menu_items(menu.sel, dims.0, dims.1);
        let sel = menu.sel;
        let mut list = div().id("table-menu").w(px(224.)).p(px(6.)).rounded(px(8.))
            .bg(theme.menu).border_1().border_color(theme.menu_border).shadow_lg()
            .flex().flex_col().gap(px(2.))
            .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation());
        for (index, item) in items.into_iter().enumerate() {
            match item {
                None => list = list.child(menu_style::separator(&theme)),
                Some(cmd) => {
                    let disabled = cmd.disabled(dims.0, dims.1);
                    let row = menu_item(("table-menu-item", index), cmd, disabled);
                    let row = if disabled {
                        row
                    } else {
                        let menu_editor = editor.clone();
                        row.on_mouse_down(MouseButton::Left, move |_, window, cx| {
                            menu_editor.update(cx, |this, cx| { this.table_command(id, sel, cmd, cx); window.focus(&this.focus); });
                            cx.stop_propagation();
                        })
                    };
                    list = list.child(row);
                }
            }
        }
        out.push(floating(menu.anchor, list));
    }
    out
}
