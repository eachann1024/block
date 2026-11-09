//! Stable gutters, a persistent width switch, and selection-only formatting.
use gpui::{CursorStyle, MouseButton, Pixels, div, prelude::*, px, rgb, transparent_black};
use crate::{
    icons::{Icon, IconColors, icon},
    theme::{Theme, is_dark, tr},
    ui::reveal::{delete_arm_button, fold_button, handle_button},
};
pub(super) use crate::ui::reveal::grip_button;
use super::{model::{Block, Kind}, paint::BlockText, view::Editor};

pub(super) const HANDLE_WIDTH: f32 = 56.;
/// ul/ol padding-left in the reference page; also one marker column.
const MARKER_WIDTH: f32 = 29.68;
/// Nesting is stored as marker characters; two of them make one list level.
pub(super) const INDENT_PER_CHAR: f32 = MARKER_WIDTH / 2.;
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
    if is_list_kind(prev) && is_list_kind(block) { return 4.; }
    let above = margins(prev).1;
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
    if this.minimal { return (2. * zoom, 2. * zoom); }
    let top = this.next_visible_index(index, false)
        .map_or(0., |prev| collapsed_gap(&this.doc.blocks[prev], block) * zoom);
    let bottom = if this.next_visible_index(index, true).is_none() { margins(block).1 * zoom } else { 0. };
    (top, bottom)
}

pub(super) fn block_row(editor: gpui::Entity<Editor>, block: &Block, pads: (f32, f32), handles: bool, grip_active: bool, folded: bool, drop_edge: Option<(bool, usize)>, number: Option<u64>, active: bool, note: Option<std::path::PathBuf>, nowrap: bool, minimal: bool, image_pop: Option<super::image_edit::ImagePop>, has_overlay: bool, delete_armed: bool, cx: &mut gpui::Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean(); let id = block.id;
    let zoom = super::zoom::zoom_factor(minimal);
    let handle_w = HANDLE_WIDTH;
    let right_inset = if minimal { 8. } else { RIGHT_INSET };
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
    let first_line_h = if rule { 16. * zoom } else if code_lang.is_some() { 30. * zoom } else { line };
    let handle_top = if is_table { 4.9 * zoom } else if image.is_some() { 4. * zoom } else { ((first_line_h - 26. * zoom) / 2. + if callout.is_some() { 12. * zoom } else { 0. }).max(-pt) };
    let regular_handles = !minimal && !is_table && handles;
    div().id(("block", id as usize)).relative().w_full().min_w_0().pt(px(pt)).pb(px(pb))
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| {
            this.hover_block(id, *hovered, cx);
        }))
        .child(div().flex().items_start().w_full().min_w_0().pr(px(right_inset))
            .child(div().w(px(handle_w)).h(px(0.)).mr(px(ROW_GAP)).flex_none().relative()
                .child(crate::ui::motion::reveal_fade(div().absolute().right_0().top(px(handle_top)).h(px(26. * zoom)).flex().items_center().justify_end().gap(px(2. * zoom))
                    .when(regular_handles && !blank, |row| row
                        .child(grip_button(("drag", id as usize), grip_active, zoom, cx.listener(move |this, _: &gpui::MouseDownEvent, window, cx| {
                            window.focus(&this.focus, cx);
                            this.begin_drag(id, cx); cx.stop_propagation();
                        })).on_mouse_up(MouseButton::Left, cx.listener(move |this, event: &gpui::MouseUpEvent, _, cx| {
                            if this.dragging == Some(id) { this.finish_drag(event.position, cx); cx.stop_propagation(); }
                        }))))
                    .when(regular_handles && blank && delete_armed, |row| row
                        .child(delete_arm_button(("del-arm", id as usize), zoom, cx.listener(move |this, _: &gpui::MouseDownEvent, window, cx| {
                            window.focus(&this.focus, cx); this.confirm_first_line_delete(id, cx); cx.stop_propagation();
                        }))))
                    .when(regular_handles && blank && !delete_armed, |row| row
                        .child(handle_button(Icon::Plus, ("add", id as usize), zoom, cx.listener(move |this, event: &gpui::MouseDownEvent, window, cx| {
                            this.focus_block(id, window, cx);
                            let trigger = this.block_trigger_bounds(cx.entity_id(), id)
                                .unwrap_or_else(|| gpui::Bounds::new(event.position + gpui::point(px(16.), px(-10.)), gpui::size(px(224.), gpui::px(24.))));
                            this.open_block_menu_at(id, trigger, super::block_menu::Mode::Slash, cx);
                            cx.stop_propagation();
                        }))))
                    .when(!minimal && is_table && handles, |row| row
                        .child(table_grip_button(("tbl-grip", id as usize), grip_active, zoom, cx.listener(move |this, _: &gpui::MouseDownEvent, window, cx| {
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
                        .child(fold_button(folded, ("fold", id as usize), zoom, cx.listener(move |this, _: &gpui::MouseDownEvent, _, cx| {
                            this.toggle_fold(id, cx); cx.stop_propagation();
                        })))), ("handle-reveal", id as usize))))
            .when(marker_width > 0., |row| row.child(div().w(px(marker_width)).pl(px(indent)).flex_none().text_color(theme.text)
                .text_size(px(size)).line_height(px(line))
                .when(block.kind == Kind::Todo, |row| row.child(todo_box(checked, (line - 16. * zoom).max(0.) / 2., zoom, move |cx| {
                    checkbox_editor.update(cx, |this, cx| this.toggle_checked(id, cx));
                })))
                // Outside markers sit flush against the text like CSS `list-style-position: outside`.
                .when(!marker.is_empty(), |row| row.flex().justify_end().pr(px(8. * zoom)).child(marker))))
            .child(div().flex_1().min_w_0().flex().flex_col().relative()
                .when(marker_width == 0., |body| body.max_w(theme.content_width))
                .when(block.kind == Kind::Quote && callout.is_none(), |body| body.pl(px(16. * zoom)).py(px(1.6 * zoom)).child(
                    div().absolute().left_0().top_0().bottom_0().w(px(3. * zoom)).bg(theme.quote_bar)))
                .when_some(callout, |body, kind| {
                    crate::ui::callout_controls::style_card(body, kind, zoom)
                })
                .when_some(block.meta.unsupported_reason.clone().filter(|_| image.is_none() && !rule), |body, reason| body.child(
                    div().text_size(px(11. * zoom)).text_color(theme.text_secondary).pb(px(4. * zoom))
                        .child(format!("{} · {}", tr("已保留源码", "Source preserved"), unsupported_reason_label(&reason)))))
                .when_some(image, |body, (path, spec)| body.child(super::image_view::image_block(focus_editor.clone(), id, path, spec, active, image_pop, zoom, has_overlay, cx)))
                .when(static_view && rule, |body| body.child(
                    div().id(("rule", id as usize)).h(px(16. * zoom)).flex().items_center().cursor(CursorStyle::PointingHand)
                        .child(div().h(px((if active { 2. } else { 1. }) * zoom)).w_full().bg(if active { theme.accent } else if is_dark() { theme.hairline } else { gpui::rgba(0x0b0b0b33).into() }))
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
                    body.child(super::code_block::copy_button(editor.clone(), id, code_lang.unwrap_or_default(), code_visible, minimal, cx))
                })
                .when_some(callout.filter(|_| !minimal), |body, kind| {
                    body.child(crate::ui::callout_controls::controls(editor, id, kind, code_visible, zoom, cx))
                })))
        .when_some(drop_edge, |row, (after, indent)| row.child(div().absolute().left(px(handle_w + ROW_GAP + indent.min(12) as f32 * INDENT_PER_CHAR * zoom)).right(px(right_inset))
            .h(px(2. * zoom)).bg(theme.accent)
            .when(after, |line| line.bottom_0()).when(!after, |line| line.top_0())))
}
fn todo_box(checked: bool, top: f32, zoom: f32, toggle: impl Fn(&mut gpui::App) + 'static) -> impl IntoElement {
    let dark = is_dark();
    let border: gpui::Hsla = if checked { rgb(0x4774cf).into() } else if dark { rgb(0x858585).into() } else { gpui::rgba(0x0b0b0b80).into() };
    let hover_border: gpui::Hsla = if checked { rgb(0x3a62b8).into() } else if dark { rgb(0xa3a3a3).into() } else { gpui::rgba(0x0b0b0bcc).into() };
    div().mt(px(top)).ml(px(4.8 * zoom)).size(px(16. * zoom)).relative().children(crate::ui::smoke_overlay::probe("todo-checkbox")).flex().items_center().justify_center().rounded(px(4. * zoom)).cursor(CursorStyle::PointingHand)
        .when(!checked, |b| b.border_1().border_color(border)).hover(move |s| if checked { s.bg(rgb(0x3a62b8)) } else { s.border_color(hover_border) })
        .when(checked, |b| b.bg(rgb(0x4774cf)).child(icon(Icon::Check, px(12. * zoom),
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
fn table_grip_button(id: (&'static str, usize), active: bool, zoom: f32, listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    grip_button(id, active, zoom, listener).mr(px(4. * zoom))
}
pub(super) fn render_block_row(this: &Editor, index: usize, cx: &mut gpui::Context<Editor>) -> gpui::AnyElement {
    #[cfg(not(target_family = "wasm"))]
    if let Some(row) = super::ai::plan_link::render_linked_row(this, index, cx) { return row; }
    let block = &this.doc.blocks[index];
    let number = (block.kind == Kind::Numbered).then(|| this.doc.list_number(block.id)).flatten();
    let edge = this.drag_over.filter(|target| if target.after { target.slot.saturating_sub(1) == index } else { target.slot == index }).map(|target| (target.after, target.indent));
    let whole_table = block.meta.table.is_some() && this.table_sel == Some((block.id, super::table_actions::TableSel::Whole));
    let grip_active = this.dragging == Some(block.id) || whole_table;
    let handles = !this.minimal && (this.hover == Some(block.id) || this.caret.head.block == block.id || grip_active);
    let folded = !this.minimal && this.is_heading_folded(block.id);
    let picked = this.block_sel.is_some_and(|(a, b)| a.min(b) <= index && index <= a.max(b));
    let image_pop = this.image_menu.as_ref().filter(|(mid, _)| *mid == block.id).map(|(_, p)| p.clone());
    let has_overlay = this.block_menu.is_some() || this.table_menu.is_some() || this.format.is_some()
        || this.annotate.active_annotation.is_some() || this.annotate.hovered_annotation.is_some()
        || this.annotate.composer.is_some() || this.annotate.reactions.is_some();
    let row = block_row(cx.entity(), block, row_pads(this, index), handles, grip_active, folded, edge, number, this.caret.head.block == block.id && this.block_sel.is_none(), this.path.clone(), this.nowrap.contains(&block.id), this.minimal, image_pop, has_overlay, this.is_first_line_delete_armed(block.id), cx);
    div().when_some(picked.then_some(this.block_sel).flatten(), |d, (a, b)| d.bg(Theme::ocean().selection)
        .when(index == a.min(b), |d| d.rounded_t_md()).when(index == a.max(b), |d| d.rounded_b_md()))
        .child(row).into_any_element()
}
pub(super) use super::toolbar::selection_toolbar;
