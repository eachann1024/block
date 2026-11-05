//! Stable gutters, a persistent width switch, and selection-only formatting.
use gpui::{CursorStyle, MouseButton, Pixels, div, prelude::*, px, rgb, transparent_black};
use crate::{
    icons::{Icon, IconColors, icon},
    theme::{Theme, is_dark, tr},
    ui::reveal::{fold_button, handle_button, top_insert_slot},
};
pub(super) use crate::ui::reveal::grip_button;
use super::{model::{Block, Kind}, paint::BlockText, view::Editor};

pub(super) const HANDLE_WIDTH: f32 = 56.;
/// ul/ol padding-left in the reference page; also one marker column.
const MARKER_WIDTH: f32 = 29.68;
/// Nesting is stored as marker characters; two of them make one list level.
const INDENT_PER_CHAR: f32 = MARKER_WIDTH / 2.;
pub(super) const ROW_GAP: f32 = 4.;
/// Estimated floating toolbar size, used to keep it inside the editor.
pub(super) const TOOLBAR_WIDTH: Pixels = px(460.);
pub(super) const TOOLBAR_HEIGHT: Pixels = px(42.);
pub(super) const RIGHT_INSET: f32 = 12.;
pub(super) fn standard_width(body_width: Pixels) -> Pixels {
    body_width + px(HANDLE_WIDTH + ROW_GAP + RIGHT_INSET)
}

/// CSS (margin-top, margin-bottom) of a block in the reference page.
fn margins(block: &Block) -> (f32, f32) {
    match (block.kind, block.meta.heading_level) {
        (Kind::H1, _) => (0., 8.),
        (Kind::H2, _) => (35.2, 11.),
        (Kind::H3, Some(4)) => (20.8, 4.8),
        (Kind::H3, Some(5 | 6)) => (16.8, 3.5),
        (Kind::H3, _) => (25.2, 7.2),
        (Kind::Quote, _) if block.meta.callout.is_none() => (12.8, 12.8),
        _ if super::block_menu::is_divider(block) => (16.5, 16.5),
        _ => (9.6, 9.6),
    }
}
fn is_list_kind(block: &Block) -> bool {
    matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo)
}
/// Space above `block` after margin collapsing with `prev`: list items sit 4px apart
/// (adjacent lists have no extra margin), everything else uses the larger margin.
fn collapsed_gap(prev: &Block, block: &Block) -> f32 {
    if is_list_kind(prev) && is_list_kind(block) && prev.meta.column.is_none() { return 4.; }
    let above = if prev.meta.column.is_some() { 9.6 } else { margins(prev).1 };
    let below = if prev.kind == Kind::H1
        && !matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3)
        && !super::block_menu::is_divider(block)
        && block.meta.table.is_none()
    {
        0.
    } else {
        margins(block).0
    };
    if prev.meta.table.is_some() {
        let next_mt = if block.meta.table.is_some() { 18. } else { below };
        return (10. + next_mt - super::table_layout::EXTEND_ROOM).max(0.);
    }
    if block.meta.table.is_some() {
        return above.max(2.) + 16.;
    }
    above.max(below)
}
/// (top, bottom) padding of a row; the gap between rows is carried by the lower row's top.
fn row_pads(this: &Editor, index: usize) -> (f32, f32) {
    let block = &this.doc.blocks[index];
    let zoom = super::zoom::zoom_factor(this.minimal);
    if this.minimal || block.meta.column.is_some() { return (2. * zoom, 2. * zoom); }
    let top = this.next_visible_index(index, false)
        .map_or(0., |prev| collapsed_gap(&this.doc.blocks[prev], block) * zoom);
    let bottom = if this.next_visible_index(index, true).is_none() { margins(block).1 * zoom } else { 0. };
    (top, bottom)
}

pub(super) fn block_row(editor: gpui::Entity<Editor>, block: &Block, pads: (f32, f32), handles: bool, grip_active: bool, folded: bool, drop_edge: Option<bool>, number: Option<u64>, active: bool, note: Option<std::path::PathBuf>, nowrap: bool, minimal: bool, image_pop: Option<super::image_edit::ImagePop>, has_overlay: bool, cx: &mut gpui::Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean(); let id = block.id;
    let zoom = super::zoom::zoom_factor(minimal);
    let in_col = block.meta.column.is_some();
    let handle_w = if in_col { 42. } else { HANDLE_WIDTH };
    let right_inset = if in_col { 4. } else if minimal { 8. } else { RIGHT_INSET };
    let (pt, pb) = pads;
    let is_heading = super::fold::heading_level(block).is_some();
    let is_table = block.meta.table.is_some();
    let checked = block.checked;
    let checkbox_editor = editor.clone();
    let marker = match block.kind { Kind::Bullet => "•".into(), Kind::Numbered => format!("{}.", number.unwrap_or(1)), _ => String::new() };
    let (size, factor) = super::paint::heading_metrics(block.kind, block.meta.heading_level, block.meta.code_language.is_some(), minimal, f32::from(theme.editor_font), zoom);
    let line = size * factor;
    let indent = block.meta.indent.min(12) as f32 * INDENT_PER_CHAR * zoom;
    let marker_width = if matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) { MARKER_WIDTH * zoom + indent } else { indent };
    let image = super::image_source::spec_of(block)
        .and_then(|spec| super::image_source::resolve_target(&spec, note.as_deref()).map(|target| (target, spec)));
    let rule = super::block_menu::is_divider(block);
    let nowrap = nowrap && block.meta.code_language.is_some();
    let focus_editor = editor.clone();
    let callout = block.meta.callout;
    let blank = block.kind == Kind::Paragraph && block.meta.code_language.is_none() && block.text.is_empty() && !is_table && block.meta.raw_source.is_none() && callout.is_none();
    // Images and dividers are atomic blocks and never expose their Markdown/HTML source line.
    let static_view = image.is_some() || rule;
    let code_lang = block.meta.code_language.clone();
    let preview_lang = code_lang.as_deref().is_some_and(super::code_block::is_preview_lang);
    let code_visible = handles || active;
    let first_line_h = if rule { 16. } else if code_lang.is_some() { 30. * zoom } else { line };
    let handle_top = if is_table {
        4.9 * zoom
    } else if image.is_some() {
        4. * zoom
    } else {
        ((first_line_h - 26.) / 2. + if callout.is_some() { 12. * zoom } else { 0. }).max(-pt)
    };
    let regular_handles = !minimal && !is_table && handles;
    div().id(("block", id as usize)).relative().w_full().min_w_0().pt(px(pt)).pb(px(pb))
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| {
            this.hover_block(id, *hovered, cx);
        }))
        .child(div().flex().items_start().w_full().min_w_0().pr(px(right_inset))
            .child(div().w(px(handle_w)).h(px(0.)).mr(px(ROW_GAP)).flex_none().relative()
                .child(crate::ui::motion::reveal_fade(div().absolute().right_0().top(px(handle_top)).h(px(26.)).flex().items_center().justify_end().gap(px(2.))
                    .when(regular_handles && !blank, |row| row
                        .child(grip_button(("drag", id as usize), grip_active, cx.listener(move |this, _: &gpui::MouseDownEvent, _, cx| {
                            this.begin_drag(id, cx); cx.stop_propagation();
                        })).on_mouse_up(MouseButton::Left, cx.listener(move |this, event: &gpui::MouseUpEvent, _, cx| {
                            if this.dragging == Some(id) { this.finish_drag(event.position, cx); cx.stop_propagation(); }
                        }))))
                    .when(regular_handles && blank, |row| row
                        .child(handle_button(Icon::Plus, ("add", id as usize), cx.listener(move |this, event: &gpui::MouseDownEvent, window, cx| {
                            this.add_below(id, window, cx);
                            let at = this.caret.head.block;
                            this.open_block_menu(at, event.position + gpui::point(px(-10.), px(22.)), super::block_menu::Mode::Slash, cx);
                            cx.stop_propagation();
                        }))))
                    .when(!minimal && is_table && handles, |row| row
                        .child(table_grip_button(("tbl-grip", id as usize), grip_active, cx.listener(move |this, _: &gpui::MouseDownEvent, window, cx| {
                            window.focus(&this.focus, cx);
                            this.table_menu = None;
                            this.block_menu = None;
                            this.select_whole_table(id, cx);
                            this.begin_drag(id, cx);
                            cx.stop_propagation();
                        })).on_mouse_up(MouseButton::Left, cx.listener(move |this, event: &gpui::MouseUpEvent, _, cx| {
                            if this.dragging == Some(id) { this.finish_drag(event.position, cx); cx.stop_propagation(); }
                        }))))
                    .when(!minimal && is_heading && (handles || folded), |row| row
                        .child(fold_button(folded, ("fold", id as usize), cx.listener(move |this, _: &gpui::MouseDownEvent, _, cx| {
                            this.toggle_fold(id, cx); cx.stop_propagation();
                        })))), ("handle-reveal", id as usize))))
            .when(marker_width > 0., |row| row.child(div().w(px(marker_width)).pl(px(indent)).flex_none().text_color(theme.text)
                .text_size(px(size)).line_height(px(line))
                .when(block.kind == Kind::Todo, |row| row.child(todo_box(checked, (line - 16.).max(0.) / 2., move |cx| {
                    checkbox_editor.update(cx, |this, cx| this.toggle_checked(id, cx));
                })))
                // Outside markers sit flush against the text like CSS `list-style-position: outside`.
                .when(!marker.is_empty(), |row| row.flex().justify_end().pr(px(8.)).child(marker))))
            .child(div().flex_1().min_w_0().flex().flex_col().relative()
                .when(marker_width == 0. && !in_col, |body| body.max_w(theme.content_width))
                .when(block.kind == Kind::Quote && callout.is_none(), |body| body.pl(px(16. * zoom)).py(px(1.6 * zoom)).child(
                    div().absolute().left_0().top_0().bottom_0().w(px(3. * zoom)).bg(theme.quote_bar)))
                .when_some(callout, |body, _| {
                    body.rounded(px(8.)).bg(theme.hover).border_1().border_color(theme.hairline)
                        .py(px(12.)).pr(px(16.)).pl(px(44.))
                        .child(div().absolute().left(px(16.)).top(px(12. + (line - 18.).max(0.) / 2.))
                            .child(icon(Icon::Callout, px(18.), theme.icon_colors(theme.accent))))
                })
                .when_some(block.meta.unsupported_reason.clone().filter(|_| image.is_none() && !rule), |body, reason| body.child(
                    div().text_size(px(11.)).text_color(theme.text_secondary).pb_1()
                        .child(format!("{} · {}", tr("已保留源码", "Source preserved"), unsupported_reason_label(&reason)))))
                .when_some(image, |body, (path, spec)| body.child(super::image_view::image_block(focus_editor.clone(), id, path, spec, active, image_pop, zoom, has_overlay, cx)))
                .when(static_view && rule, |body| body.child(
                    div().id(("rule", id as usize)).h(px(16.)).flex().items_center().cursor(CursorStyle::PointingHand)
                        .child(div().h(px(if active { 2. } else { 1. })).w_full().bg(if active { theme.accent } else if is_dark() { theme.hairline } else { gpui::rgba(0x0b0b0b33).into() }))
                        .on_mouse_down(MouseButton::Left, { let e = focus_editor.clone(); move |_, window, cx| {
                            e.update(cx, |this, cx| this.focus_block(id, window, cx)); cx.stop_propagation();
                        }})))
                .when(!static_view && !preview_lang && !nowrap, |body| body.child(BlockText { editor: editor.clone(), block: id }))
                // No-wrap code scrolls sideways inside its own box instead of pushing past the column.
                .when(!static_view && !preview_lang && nowrap, |body| body.child(div().id(("code-scroll", id as usize)).w_full().min_w_0().flex()
                    .overflow_x_scroll().child(BlockText { editor: editor.clone(), block: id })))
                .when(!static_view && preview_lang, |body| {
                    let source_el = if nowrap {
                        div().id(("code-scroll", id as usize)).w_full().min_w_0().flex()
                            .overflow_x_scroll().child(BlockText { editor: editor.clone(), block: id }).into_any_element()
                    } else {
                        BlockText { editor: editor.clone(), block: id }.into_any_element()
                    };
                    let lang = code_lang.as_deref().unwrap_or_default();
                    body.child(super::code_block_preview::preview_block(editor.clone(), id, lang, &block.text, active, code_visible, source_el, minimal, cx))
                })
                .when(!static_view && !preview_lang && code_lang.is_some(), |body| {
                    body.child(super::code_block::copy_button(editor, id, code_lang.unwrap_or_default(), code_visible, cx))
                })))
        .when_some(drop_edge, |row, after| row.child(div().absolute().left(px(handle_w + ROW_GAP)).right(px(right_inset))
            .h(px(2.)).bg(theme.accent)
            .when(after, |line| line.bottom_0()).when(!after, |line| line.top_0())))
}
fn todo_box(checked: bool, top: f32, toggle: impl Fn(&mut gpui::App) + 'static) -> impl IntoElement {
    let dark = is_dark();
    let border: gpui::Hsla = if checked { rgb(0x4774cf).into() } else if dark { rgb(0x858585).into() } else { gpui::rgba(0x0b0b0b80).into() };
    let hover_border: gpui::Hsla = if checked { rgb(0x3a62b8).into() } else if dark { rgb(0xa3a3a3).into() } else { gpui::rgba(0x0b0b0bcc).into() };
    div().mt(px(top)).ml(px(4.8)).size(px(16.)).relative().children(crate::ui::smoke_overlay::probe("todo-checkbox")).flex().items_center().justify_center().rounded(crate::ui::radius::MICRO_SM).cursor(CursorStyle::PointingHand)
        .when(!checked, |b| b.border_1().border_color(border)).hover(move |s| if checked { s.bg(rgb(0x3a62b8)) } else { s.border_color(hover_border) })
        .when(checked, |b| b.bg(rgb(0x4774cf)).child(icon(Icon::Check, px(12.),
            IconColors { line: rgb(0xffffff).into(), fill: transparent_black(), accent: rgb(0xffffff).into() })))
        .on_mouse_down(MouseButton::Left, move |_, _, cx| { toggle(cx); cx.stop_propagation(); })
}
fn unsupported_reason_label(reason: &str) -> &str {
    if crate::theme::is_en() {
        return reason;
    }
    match reason {
        "Unsupported table cell syntax is retained as original Markdown" => "不支持的表格单元格语法，已保留原始 Markdown",
        "Nested or indented code is retained as source" => "嵌套或缩进代码块，已保留为源码",
        "Unsupported Markdown or HTML is retained as editable source" => "不支持的 Markdown 或 HTML，已保留为可编辑源码",
        "Malformed or unsupported HTML block is retained as source" => "格式有误或不支持的 HTML 块，已保留为源码",
        "Empty HTML block source" => "HTML 块源码为空",
        "Incomplete HTML block" => "未闭合的 HTML 块",
        "Unsupported HTML wrapper is retained as original HTML" => "不支持的 HTML 包装标签，已保留原始 HTML",
        "Unsupported paragraph wrapper is retained as original HTML" => "不支持的段落包装标签，已保留原始 HTML",
        "Block contains unsupported HTML; the original HTML is retained" => "块内包含不支持的 HTML，已保留原始 HTML",
        "Column contains unsupported Markdown or HTML" => "分栏内包含不支持的 Markdown 或 HTML",
        "Text outside column" => "分栏容器外存在多余文本",
        "Columns HTML contains malformed tokens" => "分栏 HTML 包含格式错误的标记",
        "Missing columns wrapper class" => "缺少分栏容器类名",
        "Unsupported element inside columns container" => "分栏容器内包含不支持的元素",
        "Unclosed column div" => "分栏标签未闭合",
        "Columns container requires at least two columns" => "分栏容器至少需要两栏",
        "HTML contains unsupported or malformed tokens" => "HTML 包含不支持或格式错误的标记",
        "Text outside a table cell cannot be represented visually" => "表格单元格外的文本无法可视化呈现",
        "Table tags are not explicitly balanced" => "表格标签未正确闭合",
        "Nested or unbalanced cells are retained as original HTML" => "嵌套或未闭合的单元格，已保留原始 HTML",
        "A table cell has no explicit closing tag" => "表格单元格缺少闭合标签",
        "Cell contains unsupported HTML; the original table is retained" => "单元格包含不支持的 HTML，已保留原始表格",
        "Invalid table cell span" => "无效的单元格跨度",
        "Table cell span exceeds the visual editor limit" => "单元格跨度超出可视化编辑上限",
        "Table exceeds the visual editor limit; original HTML is retained" => "表格规模超出可视化编辑上限，已保留原始 HTML",
        "Unsupported table structure is retained as original HTML" => "不支持的表格结构，已保留原始 HTML",
        "Non-void table elements cannot be self-closing" => "表格元素不能自闭合",
        "Incomplete HTML table" => "未闭合的 HTML 表格",
        "Table spans extend outside the supported grid" => "单元格跨度超出表格网格范围",
        "Overlapping merged cells are retained as original HTML" => "重叠的合并单元格，已保留原始 HTML",
        "Irregular table grid is retained as original HTML" => "不规则的表格网格，已保留原始 HTML",
        "Column widths do not match the table grid" => "列宽数量与表格列数不一致",
        _ => reason,
    }
}
fn table_grip_button(id: (&'static str, usize), active: bool, listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    grip_button(id, active, listener).mr(px(4.))
}
pub(super) fn render_block_row(this: &Editor, index: usize, cx: &mut gpui::Context<Editor>) -> gpui::AnyElement {
    let block = &this.doc.blocks[index];
    let number = (block.kind == Kind::Numbered).then(|| this.doc.list_number(block.id)).flatten();
    let edge = this.drag_over.filter(|target| target.index == index).map(|target| target.after);
    let whole_table = block.meta.table.is_some() && this.table_sel == Some((block.id, super::table_actions::TableSel::Whole));
    let grip_active = this.dragging == Some(block.id) || whole_table;
    let handles = !this.minimal && (this.hover == Some(block.id) || this.caret.head.block == block.id || grip_active);
    let folded = !this.minimal && this.is_heading_folded(block.id);
    let picked = this.block_sel.is_some_and(|(a, b)| a.min(b) <= index && index <= a.max(b));
    let image_pop = this.image_menu.as_ref().filter(|(mid, _)| *mid == block.id).map(|(_, p)| p.clone());
    let in_col = block.meta.column.is_some();
    let is_col_head = in_col && (index == 0 || this.doc.blocks[index - 1].meta.column != block.meta.column);
    let show_top_slot = !this.minimal && this.path.is_some() && super::block_ops::needs_top_insert(block)
        && ((index == 0 && !in_col) || is_col_head);
    let slot_hw = if in_col { 42. } else { HANDLE_WIDTH };
    let slot_ri = if in_col { 4. } else { RIGHT_INSET };
    let bid = block.id;
    let top_slot = show_top_slot.then(|| {
        top_insert_slot(("top-insert", bid as usize), slot_hw, ROW_GAP, slot_ri, cx.listener(move |this, _, window, cx| {
            this.insert_above(bid, cx); window.focus(&this.focus, cx); cx.stop_propagation();
        }))
    });
    let has_overlay = this.block_menu.is_some() || this.table_menu.is_some() || this.format.is_some()
        || this.annotate.active_annotation.is_some() || this.annotate.hovered_annotation.is_some()
        || this.annotate.composer.is_some() || this.annotate.reactions.is_some();
    let row = block_row(cx.entity(), block, row_pads(this, index), handles, grip_active, folded, edge, number, this.caret.head.block == block.id && this.block_sel.is_none(), this.path.clone(), this.nowrap.contains(&block.id), this.minimal, image_pop, has_overlay, cx);
    div().when_some(picked.then_some(this.block_sel).flatten(), |d, (a, b)| d.bg(Theme::ocean().selection)
        .when(index == a.min(b), |d| d.rounded_t_md()).when(index == a.max(b), |d| d.rounded_b_md()))
        .when_some(top_slot, |d, s| d.child(s))
        .child(row).into_any_element()
}
pub(super) use super::toolbar::selection_toolbar;
