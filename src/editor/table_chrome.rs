//! Table controls shown while the pointer is over a table: row and column capsule handles
//! on the grid border, the bottom add-row bar, the right add-column bar, the top-right more button, drop line, and menus.
use gpui::{AnyElement, Div, Entity, MouseButton, Pixels, Point, Stateful, div, point, prelude::*, px};
use super::{table_actions::{TableChrome, TableCmd, TableDrag, TableMenu, TableSel, menu_items}, view::Editor};
use crate::{
    dropdown::{self, RowSpec},
    theme::{Theme, tr},
    ui::reveal::{table_add_bar as add_bar, table_capsule as capsule, table_more_button as more_button},
};

thread_local! {
    static PILL_HOVER: std::cell::Cell<Option<(u64, TableSel)>> = const { std::cell::Cell::new(None) };
}

fn floating(position: Point<Pixels>, child: impl IntoElement) -> AnyElement {
    gpui::deferred(gpui::anchored().position(position).child(child)).into_any_element()
}

fn menu_item(id: (&'static str, usize), cmd: TableCmd, disabled: bool) -> Stateful<Div> {
    dropdown::row_with(
        id,
        RowSpec {
            icon: Some(cmd.icon()),
            label: cmd.label().into(),
            danger: cmd.danger(),
            disabled,
            ..Default::default()
        },
    )
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
    let follow = dragging.and_then(|d| d.position.map(|p| (d.sel, d.start, p)));
    let view = chrome.viewport;
    let show_column = dragging.is_none_or(|d| matches!(d.sel, TableSel::Column(_))) && table.top() - px(8.) >= view.top();
    let row_mid = match follow {
        Some((TableSel::Row(_), s, p)) => ((top + bottom) / 2. + p.y - s.y).clamp(table.top() + px(12.), (table.bottom() - px(12.)).max(table.top() + px(12.))),
        _ => (top + bottom) / 2.,
    };
    let show_row = dragging.is_none_or(|d| matches!(d.sel, TableSel::Row(_)))
        && row_mid - px(13.) >= view.top() && row_mid + px(13.) <= view.bottom();
    if show_column {
        let x = match follow {
            Some((TableSel::Column(_), s, p)) => ((left + right) / 2. + p.x - s.x).clamp(table.left() + px(12.), (table.right() - px(12.)).max(table.left() + px(12.))),
            _ => (left + right) / 2.,
        };
        let col_sel = TableSel::Column(c);
        let expanded = is_active(col_sel) || is_hovered(col_sel);
        let pos = point(x - px(12.), if expanded { table.top() - px(7.) } else { table.top() - px(4.) });
        let col_editor = editor.clone();
        let hover_editor = editor.clone();
        out.push(floating(pos,
            capsule(("table-col", c), false, expanded, drag.is_some(), tr("点击查看列操作 · 拖动移动", "Click for column options · drag to move"))
                .on_hover(move |hovered: &bool, _, cx| {
                    if drag.is_some() || hover_editor.read(cx).dragging.is_some() { return; }
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
        let y = row_mid;
        let row_sel = TableSel::Row(r);
        let expanded = is_active(row_sel) || is_hovered(row_sel);
        let pos = point(if expanded { table.left() - px(7.) } else { table.left() - px(4.) }, y - px(12.));
        let row_editor = editor.clone();
        let hover_editor = editor.clone();
        out.push(floating(pos,
            capsule(("table-row", r), true, expanded, drag.is_some(), tr("点击查看行操作 · 拖动移动", "Click for row options · drag to move"))
                .on_hover(move |hovered: &bool, _, cx| {
                    if drag.is_some() || hover_editor.read(cx).dragging.is_some() { return; }
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
                        add_row.update(cx, |this, cx| { this.table_command(id, TableSel::Whole, TableCmd::AppendRow, cx); window.focus(&this.focus, cx); });
                        cx.stop_propagation();
                    })));
        }
        if !whole_selected && table.top() >= view.top() && table.bottom() <= view.bottom() {
            let add_col = editor.clone();
            out.push(floating(point(table.right() + px(2.), table.top()),
                add_bar("table-add-col", px(16.), table.size.height, tr("添加列", "Add column"))
                    .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                        add_col.update(cx, |this, cx| { this.table_command(id, TableSel::Whole, TableCmd::AppendColumn, cx); window.focus(&this.focus, cx); });
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
        let mut list = dropdown::shell("table-menu").w(px(224.));
        for (index, item) in items.into_iter().enumerate() {
            match item {
                None => list = list.child(dropdown::separator()),
                Some(cmd) => {
                    let disabled = cmd.disabled(dims.0, dims.1);
                    let row = menu_item(("table-menu-item", index), cmd, disabled);
                    let row = if disabled {
                        row
                    } else {
                        let menu_editor = editor.clone();
                        row.on_mouse_down(MouseButton::Left, move |_, window, cx| {
                            menu_editor.update(cx, |this, cx| { this.table_command(id, sel, cmd, cx); window.focus(&this.focus, cx); });
                            cx.stop_propagation();
                        })
                    };
                    list = list.child(row);
                }
            }
        }
        out.push(dropdown::deferred_popover(menu.anchor, list, 2).into_any_element());
    }
    out
}
