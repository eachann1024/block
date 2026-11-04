//! Table controls shown while the pointer is over a table: row and column handles outside the
//! grid, the add-row and add-column buttons, the drop line, and the option menus. All positions are window coordinates from final paint.
use gpui::{AnyElement, CursorStyle, Entity, MouseButton, Pixels, Point, div, point, prelude::*, px};
use gpui_component::tooltip::Tooltip;
use super::{menu_style, table_actions::{TableChrome, TableCmd, TableDrag, TableMenu, TableSel, menu_items}, view::Editor};
use crate::{icons::{Icon, icon}, theme::Theme};

fn floating(position: Point<Pixels>, child: impl IntoElement) -> AnyElement {
    gpui::deferred(gpui::anchored().position(position).child(child)).into_any_element()
}
/// Six-dot grip: 2 columns x 3 rows for the row handle, 3 columns x 2 rows for the column handle.
fn grip(vertical: bool, color: gpui::Hsla) -> impl IntoElement {
    let (lines, per) = if vertical { (3, 2) } else { (2, 3) };
    let mut grid = div().flex().flex_col().items_center().justify_center().gap(px(2.5));
    for _ in 0..lines {
        let mut line = div().flex().gap(px(2.5));
        for _ in 0..per { line = line.child(div().size(px(2.5)).rounded_full().bg(color)); }
        grid = grid.child(line);
    }
    grid
}
/// Reference `.goose-table-handle-btn` inside the left/top chrome: popover fill, menu border, soft shadow.
fn handle(id: (&'static str, usize), vertical: bool, open: bool, tip: &'static str) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let (w, h) = if vertical { (px(16.), px(24.)) } else { (px(28.), px(16.)) };
    div().id(id).w(w).h(h).flex_none().rounded(px(6.)).flex().items_center().justify_center()
        .bg(if open { theme.hover } else { theme.menu }).border_1().border_color(theme.menu_border)
        .shadow(vec![gpui::BoxShadow { color: gpui::hsla(0.6, 0.15, 0.13, 0.06), offset: point(px(0.), px(1.)),
            blur_radius: px(3.), spread_radius: px(0.) }])
        .hover(|s| s.bg(theme.hover)).cursor(CursorStyle::OpenHand)
        .child(grip(vertical, if open { theme.text } else { theme.text_secondary }))
        .tooltip(move |window, cx| Tooltip::new(tip).build(window, cx))
}
/// Reference `.goose-table-extend-button`: transparent strip with a 16px plus, hover fill only.
fn extend(id: &'static str, tip: &'static str) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id(id).flex().items_center().justify_center().rounded(px(6.))
        .cursor(CursorStyle::PointingHand).hover(|s| s.bg(theme.hover))
        .child(icon(Icon::Plus, px(16.), theme.icon_colors(theme.text_secondary)))
        .tooltip(move |window, cx| Tooltip::new(tip).build(window, cx))
}
fn more_button(open: bool) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id("table-more").size(px(24.)).flex_none().rounded(px(6.)).flex().items_center().justify_center()
        .bg(if open { theme.hover } else { theme.menu }).border_1().border_color(theme.menu_border)
        .shadow(vec![gpui::BoxShadow { color: gpui::hsla(0.6, 0.15, 0.13, 0.06), offset: point(px(0.), px(1.)),
            blur_radius: px(3.), spread_radius: px(0.) }])
        .hover(|s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
        .child(icon(Icon::More, px(15.), theme.icon_colors(if open { theme.text } else { theme.text_secondary })))
        .tooltip(|window, cx| Tooltip::new("右键单元格空白处或按 ⇧F10 打开行列操作").build(window, cx))
}
pub(super) fn overlay(editor: Entity<Editor>, chrome: &TableChrome, _sel: Option<(u64, TableSel)>,
    menu: Option<TableMenu>, drag: Option<TableDrag>, dims: (usize, usize)) -> Vec<AnyElement> {
    let theme = Theme::ocean(); let id = chrome.block; let (r, c) = chrome.cell;
    let open = |s: TableSel| menu.is_some_and(|m| m.block == id && m.sel == s);
    let mut out: Vec<AnyElement> = Vec::new();
    let Some(&(left, right)) = chrome.columns.get(c) else { return out };
    let Some(&(top, bottom)) = chrome.rows.get(r) else { return out };
    let table = chrome.bounds;
    let dragging = drag.filter(|d| d.moved && d.block == id);
    let follow = dragging.and_then(|d| d.position.map(|p| (d.sel, p)));
    // While a row or column is dragged only its handle stays, following the pointer.
    let view = chrome.viewport;
    // Anchored overlays flip when they overflow, so anything off-screen is hidden or clipped instead.
    let show_column = dragging.is_none_or(|d| matches!(d.sel, TableSel::Column(_))) && table.top() - px(8.) >= view.top();
    let row_mid = match follow { Some((TableSel::Row(_), p)) => p.y, _ => (top + bottom) / 2. };
    let show_row = dragging.is_none_or(|d| matches!(d.sel, TableSel::Row(_)))
        && row_mid - px(12.) >= view.top() && row_mid + px(12.) <= view.bottom();
    let strip_top = table.top().max(view.top()); let strip_bottom = table.bottom().min(view.bottom());
    if show_column {
        let x = match follow { Some((TableSel::Column(_), p)) => p.x, _ => (left + right) / 2. };
        let col_editor = editor.clone();
        // Handles straddle the table border, as in the reference, so they stay clear of the block handle and the block above.
        out.push(floating(point(x - px(14.), table.top() - px(8.)),
            handle(("table-col", c), false, open(TableSel::Column(c)), "点击打开列操作，按住拖动可移动")
                .on_mouse_down(MouseButton::Left, move |event, _, cx| {
                    let start = event.position;
                    col_editor.update(cx, |this, cx| {
                        this.table_sel = Some((id, TableSel::Column(c))); this.table_menu = None;
                        this.table_drag = Some(TableDrag { block: id, sel: TableSel::Column(c), start, moved: false, position: None });
                        cx.notify();
                    });
                    cx.stop_propagation();
                })));
    }
    if show_row {
        let y = match follow { Some((TableSel::Row(_), p)) => p.y, _ => (top + bottom) / 2. };
        let row_editor = editor.clone();
        out.push(floating(point(table.left() - px(8.), y - px(12.)),
            handle(("table-row", r), true, open(TableSel::Row(r)), "点击打开行操作，按住拖动可移动")
                .on_mouse_down(MouseButton::Left, move |event, _, cx| {
                    let start = event.position;
                    row_editor.update(cx, |this, cx| {
                        this.table_sel = Some((id, TableSel::Row(r))); this.table_menu = None;
                        this.table_drag = Some(TableDrag { block: id, sel: TableSel::Row(r), start, moved: false, position: None });
                        cx.notify();
                    });
                    cx.stop_propagation();
                })));
    }
    if dragging.is_none() {
        let add_col = editor.clone(); let add_row = editor.clone();
        if strip_bottom - strip_top >= px(24.) { out.push(floating(point(table.right() + px(4.), strip_top),
            extend("table-add-col", "添加列").w(px(24.)).h(strip_bottom - strip_top)
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    add_col.update(cx, |this, cx| { this.table_command(id, TableSel::Whole, TableCmd::AppendColumn, cx); window.focus(&this.focus); });
                    cx.stop_propagation();
                }))); }
        if table.bottom() + px(28.) <= view.bottom() { out.push(floating(point(table.left(), table.bottom() + px(4.)),
            extend("table-add-row", "添加行").w(table.size.width).h(px(24.))
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    add_row.update(cx, |this, cx| { this.table_command(id, TableSel::Whole, TableCmd::AppendRow, cx); window.focus(&this.focus); });
                    cx.stop_propagation();
                }))); }
        if table.top() + px(4.) >= view.top() && table.top() + px(28.) <= view.bottom() {
            let more_editor = editor.clone();
            let cell_open = menu.is_some_and(|m| m.block == id && matches!(m.sel, TableSel::Cell(..)));
            let btn_pos = point(table.right() - px(28.), table.top() + px(4.));
            let menu_anchor = point((table.right() - px(192.)).max(table.left()), table.top() + px(30.));
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
    // Drop line at the edge the dragged row or column will land on (`.bn-table-drop-cursor`).
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
        let items = menu_items(menu.sel, dims.0, dims.1); let sel = menu.sel;
        let mut list = menu_style::shell("table-menu", &theme).w(px(192.))
            .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation());
        for (index, item) in items.into_iter().enumerate() {
            match item {
                None => list = list.child(menu_style::group_gap()),
                Some(cmd) => {
                    let menu_editor = editor.clone();
                    list = list.child(menu_style::item(("table-menu-item", index), cmd.label(), cmd.danger(), false, false, &theme)
                        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                            menu_editor.update(cx, |this, cx| { this.table_command(id, sel, cmd, cx); window.focus(&this.focus); });
                            cx.stop_propagation();
                        }));
                }
            }
        }
        out.push(floating(menu.anchor, list));
    }
    out
}
