//! Table controls shown while the pointer is over a table: row and column capsule handles
//! on the grid border, the bottom add-row bar, the right add-column bar, the top-right more button, drop line, and menus.
use gpui::{AnyElement, Bounds, Div, Entity, MouseButton, Pixels, Point, Stateful, Window, div, point, prelude::*, px, size};
mod resize;
use super::{table_layout, table_resize, table_actions::{TableChrome, TableCmd, TableDrag, TableMenu, TableSel, menu_items, unoccluded}, view::Editor};
use crate::{
    dropdown::{self, RowSpec},
    theme::{Theme, tr},
    ui::{reveal::table_add_bar as add_bar, table_controls::{GRIP_LONG, GRIP_PAD, GRIP_SHORT, grip, thandle}},
};

thread_local! {
    static PILL_HOVER: std::cell::Cell<Option<(u64, TableSel)>> = const { std::cell::Cell::new(None) };
}

fn floating(position: Point<Pixels>, child: impl IntoElement) -> AnyElement {
    gpui::anchored().position(position).child(child).into_any_element()
}

fn menu_item(id: (&'static str, usize), cmd: TableCmd, disabled: bool, checked: bool) -> Stateful<Div> {
    dropdown::row_with(
        id,
        RowSpec {
            icon: Some(cmd.icon()),
            label: cmd.label().into(),
            danger: cmd.danger(),
            disabled,
            checked,
            ..Default::default()
        },
    )
}

pub(super) fn overlay(editor: Entity<Editor>, chrome: &TableChrome, sel: Option<(u64, TableSel)>,
    menu: Option<TableMenu>, drag: Option<TableDrag>, window: &Window) -> Vec<AnyElement> {
    let theme = Theme::ocean();
    let zoom = super::zoom::zoom_factor(false);
    let (gl, gs, gp) = (GRIP_LONG * zoom, GRIP_SHORT * zoom, GRIP_PAD * zoom);
    let id = chrome.block;
    let locked_sel = drag.filter(|d| d.block == id).map(|d| d.sel)
        .or_else(|| menu.filter(|m| m.block == id).map(|m| m.sel));
    let (mut r, mut c) = chrome.cell;
    match locked_sel {
        Some(TableSel::Column(col)) => c = col.min(chrome.columns.len().saturating_sub(1)),
        Some(TableSel::Row(row)) => r = row.min(chrome.rows.len().saturating_sub(1)),
        _ => {}
    }
    let is_active = |target: TableSel| sel == Some((id, target))
        || menu.is_some_and(|m| m.block == id && m.sel == target)
        || drag.is_some_and(|d| d.block == id && d.sel == target);
    let is_hovered = |target: TableSel| PILL_HOVER.with(|h| h.get() == Some((id, target)));
    let mut out: Vec<AnyElement> = Vec::new();
    let Some(&(left, right)) = chrome.columns.get(c) else { return out };
    let Some(&(top, bottom)) = chrome.rows.get(r) else { return out };
    let table = chrome.bounds;
    let dragging = drag.filter(|d| d.moved && d.block == id);
    let resizing = table_resize::active().filter(|r| r.block == id);
    let view = chrome.viewport;
    // The part of the active column inside the horizontal scroll window; a column scrolled away has no grip.
    let (seen_left, seen_right) = (left.max(table.left()), right.min(table.right()));
    let col_border_adj = if c + 1 < chrome.columns.len() && seen_right == right { px(1.) } else { px(0.) };
    let col_x = (seen_left + seen_right + col_border_adj) / 2.;
    let col_sel = TableSel::Column(c);
    let col_rect = Bounds::new(point(col_x - px(gl / 2.), table.top() + px(0.5 - gs / 2.)), size(px(gl), px(gs)));
    let show_column = dragging.is_none_or(|d| matches!(d.sel, TableSel::Column(_))) && resizing.is_none()
        && seen_left < seen_right && table.top() - px(gs / 2.) >= view.top()
        && (is_active(col_sel) || unoccluded(editor.entity_id(), id, col_rect, window));
    let row_border_adj = if r + 1 < chrome.rows.len() { px(1.) } else { px(0.) };
    let row_center = (top + bottom + row_border_adj) / 2.;
    let row_sel = TableSel::Row(r);
    let row_rect = Bounds::new(point(table.left() + px(0.5 - gs / 2.), row_center - px(gl / 2.)), size(px(gs), px(gl)));
    let show_row = dragging.is_none_or(|d| matches!(d.sel, TableSel::Row(_))) && resizing.is_none()
        && row_center - px(gl / 2.) >= view.top() && row_center + px(gl / 2.) <= view.bottom()
        && (is_active(row_sel) || unoccluded(editor.entity_id(), id, row_rect, window));
    if show_column {
        let x = col_x;
        let active = is_active(col_sel);
        let expanded = active || is_hovered(col_sel);
        // The grip is centred on the 1px top border (`table.top() + 0.5px`) in both states.
        let pos = point(x - px(gl / 2. + gp), table.top() + px(0.5 - gs / 2. - gp));
        let col_editor = editor.clone();
        let hover_editor = editor.clone();
        out.push(floating(pos,
            grip(("table-col", c), false, expanded, active || menu.is_some(), drag.is_some(), zoom, tr("点击查看列操作 · 拖动移动", "Click for column options · drag to move"))
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
                        this.table_drag = Some(TableDrag { block: id, sel: col_sel, start, moved: false, position: None, target: None });
                        cx.notify();
                    });
                    cx.stop_propagation();
                })));
    }
    if show_row {
        let y = row_center;
        let active = is_active(row_sel);
        let expanded = active || is_hovered(row_sel);
        // The grip is centred on the 1px left border (`table.left() + 0.5px`) in both states.
        let pos = point(table.left() + px(0.5 - gs / 2. - gp), y - px(gl / 2. + gp));
        let row_editor = editor.clone();
        let hover_editor = editor.clone();
        out.push(floating(pos,
            grip(("table-row", r), true, expanded, active || menu.is_some(), drag.is_some(), zoom, tr("点击查看行操作 · 拖动移动", "Click for row options · drag to move"))
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
                        this.table_drag = Some(TableDrag { block: id, sel: row_sel, start, moved: false, position: None, target: None });
                        cx.notify();
                    });
                    cx.stop_propagation();
                })));
    }
    if dragging.is_none() { out.extend(resize::zones(&editor, chrome, resizing.map(|r| r.col), zoom)); }
    if dragging.is_none() && resizing.is_none() {
        let whole_selected = sel == Some((id, TableSel::Whole));
        if !whole_selected && table.bottom() + px(20. * zoom) <= view.bottom() {
            let add_row = editor.clone();
            out.push(floating(point(table.left(), table.bottom() + px(2. * zoom)),
                add_bar("table-add-row", table.size.width, px(18. * zoom), zoom, tr("添加行", "Add row"))
                    .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                        add_row.update(cx, |this, cx| { this.table_command(id, TableSel::Whole, TableCmd::AppendRow, cx); window.focus(&this.focus, cx); });
                        cx.stop_propagation();
                    })));
        }
        let col_top = table.top().max(view.top());
        let col_bottom = table.bottom().min(view.bottom());
        if !whole_selected && col_bottom - col_top >= px(24. * zoom) {
            let add_col = editor.clone();
            out.push(floating(point(table.right() + px(2. * zoom), col_top),
                add_bar("table-add-col", px(18. * zoom), col_bottom - col_top, zoom, tr("添加列", "Add column"))
                    .on_mouse_down(MouseButton::Left, move |event, window, cx| {
                        add_col.update(cx, |this, cx| { this.begin_extend(id, event.position, cx); window.focus(&this.focus, cx); });
                        cx.stop_propagation();
                    })));
        }
        let btn_pos = point(table.right() + px(22. * zoom), table.top());
        let btn_sz = px(26. * zoom);
        let btn_bounds = Bounds::new(btn_pos, size(btn_sz, btn_sz));
        let cell_open = menu.is_some_and(|m| m.block == id && matches!(m.sel, TableSel::Cell(..) | TableSel::Whole));
        if table.top() >= view.top() && table.top() + btn_sz <= view.bottom()
            && (cell_open || unoccluded(editor.entity_id(), id, btn_bounds, window))
        {
            let more_editor = editor.clone();
            let menu_spec = dropdown::PlacementSpec::below(btn_bounds)
                .align_x(dropdown::AlignX::Right)
                .viewport(view)
                .gap(px(6.));
            out.push(floating(btn_pos,
                thandle(cell_open, zoom).on_mouse_down(MouseButton::Left, move |_, _, cx| {
                    more_editor.update(cx, |this, cx| {
                        let (mr, mc) = this.cell_of_caret().filter(|x| x.0 == id).map(|x| (x.1, x.2)).unwrap_or((r, c));
                        this.open_table_menu_spec(id, TableSel::Cell(mr, mc), menu_spec, cx);
                    });
                    cx.stop_propagation();
                })));
        }
    }
    if let Some(drag) = dragging {
        if let Some(edge) = drag.target.and_then(|to| chrome.edge(drag.sel, to)) {
            let (origin, size) = match drag.sel {
                TableSel::Column(_) => (point(edge - px(1.5), table.top()), gpui::size(px(3.), table.size.height)),
                _ => (point(table.left(), edge - px(1.5)), gpui::size(table.size.width, px(3.))),
            };
            out.push(floating(origin, div().w(size.width).h(size.height).rounded(px(2.)).bg(theme.accent)));
        }
    }
    out
}

pub(super) fn menu_popup(editor: Entity<Editor>, menu: TableMenu, phase: crate::ui::motion::PopupMotion, pop_gen: u64, dims: (usize, usize)) -> AnyElement {
    let id = menu.block;
    let items = menu_items(menu.sel, dims.0, dims.1);
    let sel = menu.sel;
    let fitted = table_layout::widths_of(editor.entity_id(), id).fit;
    let width = if matches!(sel, TableSel::Row(_) | TableSel::Column(_)) { px(188.) } else { px(224.) };
    let mut list = dropdown::shell("table-menu").w(width);
    for (index, item) in items.into_iter().enumerate() {
        match item {
            None => list = list.child(dropdown::separator()),
            Some(cmd) => {
                let disabled = cmd.disabled(dims.0, dims.1);
                let checked = cmd == TableCmd::FitWidth && fitted;
                let row = menu_item(("table-menu-item", index), cmd, disabled, checked);
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
    dropdown::anchored_motion(("table-menu-pop", pop_gen), menu.spec, list, phase, 2)
}
