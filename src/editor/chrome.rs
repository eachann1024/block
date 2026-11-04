//! Stable gutters, a persistent width switch, and selection-only formatting.
use gpui::{CursorStyle, MouseButton, Pixels, div, prelude::*, px, rgb, transparent_black};
use crate::{
    icons::{Icon, IconColors, icon},
    theme::{Theme, is_dark, tr},
    ui::reveal::{fold_button, handle_button},
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
        (Kind::H3, Some(4)) => (20., 6.),
        (Kind::H3, Some(5 | 6)) => (16., 5.),
        (Kind::H3, _) => (25.2, 7.2),
        (Kind::Quote, _) if block.meta.callout.is_none() => (12.8, 12.8),
        _ if block.meta.code_language.is_some() => (8.4, 8.4),
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
    above.max(margins(block).0)
}
/// (top, bottom) padding of a row; the gap between rows is carried by the lower row's top.
fn row_pads(this: &Editor, index: usize) -> (f32, f32) {
    let block = &this.doc.blocks[index];
    if this.minimal || block.meta.column.is_some() { return (2., 2.); }
    let top = this.next_visible_index(index, false)
        .map_or(0., |prev| collapsed_gap(&this.doc.blocks[prev], block));
    let bottom = if this.next_visible_index(index, true).is_none() { margins(block).1 } else { 0. };
    (top, bottom)
}

pub(super) fn block_row(editor: gpui::Entity<Editor>, block: &Block, pads: (f32, f32), handles: bool, grip_active: bool, folded: bool, drop_edge: Option<bool>, number: Option<u64>, active: bool, note: Option<std::path::PathBuf>, nowrap: bool, minimal: bool, image_pop: Option<super::image_edit::ImagePop>, cx: &mut gpui::Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean(); let id = block.id;
    let in_col = block.meta.column.is_some();
    let handle_w = if in_col { 42. } else { HANDLE_WIDTH };
    let right_inset = if in_col { 4. } else { RIGHT_INSET };
    let (pt, pb) = pads;
    let is_heading = super::fold::heading_level(block).is_some();
    let is_table = block.meta.table.is_some();
    let checked = block.checked;
    let checkbox_editor = editor.clone();
    let marker = match block.kind { Kind::Bullet => "•".into(), Kind::Numbered => format!("{}.", number.unwrap_or(1)), _ => String::new() };
    let (size, factor) = super::paint::heading_metrics(block.kind, block.meta.heading_level, block.meta.code_language.is_some(), minimal, f32::from(theme.editor_font));
    let line = size * factor;
    let indent = block.meta.indent.min(12) as f32 * INDENT_PER_CHAR;
    let marker_width = if matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) { MARKER_WIDTH + indent } else { indent };
    let image = super::image_source::spec_of(block)
        .and_then(|spec| super::image_source::resolve_path(&spec, note.as_deref()).map(|path| (path, spec)));
    let rule = super::block_menu::is_divider(block);
    let nowrap = nowrap && block.meta.code_language.is_some();
    let focus_editor = editor.clone();
    let callout = block.meta.callout;
    let blank = block.text.is_empty() && !is_table && block.meta.raw_source.is_none() && callout.is_none();
    // Images and dividers are atomic blocks and never expose their Markdown/HTML source line.
    let static_view = image.is_some() || rule;
    let code_lang = block.meta.code_language.clone();
    let preview_lang = code_lang.as_deref().is_some_and(super::code_block::is_preview_lang);
    let code_visible = handles || active;
    let first_line_h = if rule { 16. } else if code_lang.is_some() { 30. } else { line };
    let handle_top = if is_table || image.is_some() {
        4.
    } else {
        ((first_line_h - 26.) / 2. + if callout.is_some() { 12. } else { 0. }).max(-pt)
    };
    let regular_handles = !minimal && !is_table && handles;
    div().id(("block", id as usize)).relative().w_full().min_w_0().pt(px(pt)).pb(px(pb))
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| {
            this.hover_block(id, *hovered, cx);
        }))
        .child(div().flex().items_start().w_full().min_w_0().gap(px(ROW_GAP)).pr(px(right_inset))
            .child(div().w(px(handle_w)).h(px(0.)).flex_none().relative()
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
                .when(block.kind == Kind::Quote && callout.is_none(), |body| body.pl(px(16.)).py(px(1.6)).child(
                    div().absolute().left_0().top_0().bottom_0().w(px(3.)).bg(theme.quote_bar)))
                .when_some(callout, |body, _| {
                    body.rounded(px(8.)).bg(theme.hover).border_1().border_color(theme.hairline)
                        .py(px(12.)).pr(px(16.)).pl(px(44.))
                        .child(div().absolute().left(px(16.)).top(px(12. + (line - 18.).max(0.) / 2.))
                            .child(icon(Icon::Callout, px(18.), theme.icon_colors(theme.accent))))
                })
                .when_some(block.meta.unsupported_reason.clone().filter(|_| image.is_none() && !rule), |body, reason| body.child(
                    div().text_size(px(11.)).text_color(theme.text_secondary).pb_1()
                        .child(format!("{} · {reason}", tr("已保留源码", "Source preserved")))))
                .when_some(image, |body, (path, spec)| body.child(super::image_view::image_block(focus_editor.clone(), id, path, spec, active, image_pop, cx)))
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
                    body.child(super::code_block_preview::preview_block(editor.clone(), id, lang, &block.text, active, code_visible, source_el, cx))
                })
                .when(!static_view && !preview_lang && code_lang.is_some(), |body| {
                    body.child(super::code_block::copy_button(editor, id, code_lang.unwrap_or_default(), code_visible, cx))
                })))
        .when_some(drop_edge, |row, after| row.child(div().absolute().left(px(handle_w)).right(px(right_inset))
            .h(px(2.)).bg(theme.accent)
            .when(after, |line| line.bottom_0()).when(!after, |line| line.top_0())))
}
fn todo_box(checked: bool, top: f32, toggle: impl Fn(&mut gpui::App) + 'static) -> impl IntoElement {
    let dark = is_dark();
    let border: gpui::Hsla = if checked { rgb(0x2f6fe4).into() } else if dark { rgb(0x787772).into() } else { gpui::rgba(0x0b0b0b80).into() };
    let hover_border: gpui::Hsla = if checked { rgb(0x1f57c0).into() } else if dark { rgb(0x9b9a95).into() } else { gpui::rgba(0x0b0b0bcc).into() };
    div().mt(px(top)).ml(px(4.)).size(px(16.)).flex().items_center().justify_center().rounded(px(4.)).cursor(CursorStyle::PointingHand)
        .border_1().border_color(border).hover(move |s| s.border_color(hover_border))
        .when(checked, |b| b.bg(rgb(0x2f6fe4)).hover(|s| s.bg(rgb(0x1f57c0))).child(icon(Icon::Check, px(12.),
            IconColors { line: rgb(0xffffff).into(), fill: transparent_black(), accent: transparent_black() })))
        .on_mouse_down(MouseButton::Left, move |_, _, cx| { toggle(cx); cx.stop_propagation(); })
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
    let row = block_row(cx.entity(), block, row_pads(this, index), handles, grip_active, folded, edge, number, this.caret.head.block == block.id && this.block_sel.is_none(), this.path.clone(), this.nowrap.contains(&block.id), this.minimal, image_pop, cx);
    div().when_some(picked.then_some(this.block_sel).flatten(), |d, (a, b)| d.bg(Theme::ocean().selection)
        .when(index == a.min(b), |d| d.rounded_t_md()).when(index == a.max(b), |d| d.rounded_b_md())).child(row).into_any_element()
}
pub(super) use super::toolbar::selection_toolbar;
