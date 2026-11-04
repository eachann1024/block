//! Stable gutters, a persistent width switch, and selection-only formatting.
use gpui::{CursorStyle, MouseButton, Pixels, div, prelude::*, px, rgb, rgba, transparent_black};
use crate::{icons::{Icon, IconColors, icon}, theme::{Theme, is_dark, tr}};
use super::{model::{Block, Kind}, paint::BlockText, view::Editor};

pub(super) const HANDLE_WIDTH: f32 = 56.;
const MARKER_WIDTH: f32 = 24.;
pub(super) const ROW_GAP: f32 = 4.;
/// Estimated floating toolbar size, used to keep it inside the editor.
pub(super) const TOOLBAR_WIDTH: Pixels = px(460.);
pub(super) const TOOLBAR_HEIGHT: Pixels = px(42.);
pub(super) const RIGHT_INSET: f32 = 12.;
pub(super) fn standard_width(body_width: Pixels) -> Pixels {
    body_width + px(HANDLE_WIDTH + ROW_GAP + RIGHT_INSET)
}

fn block_v_pad(block: &Block, minimal: bool, in_col: bool) -> (f32, f32) {
    if minimal || in_col { return (2., 2.); }
    match (block.kind, block.meta.heading_level) {
        (Kind::H1, _) => (8., 4.),
        (Kind::H2, _) => (22., 6.),
        (Kind::H3, Some(4)) => (14., 4.),
        (Kind::H3, Some(5 | 6)) => (12., 3.),
        (Kind::H3, _) => (18., 5.),
        (Kind::Bullet | Kind::Numbered | Kind::Todo, _) => (2., 2.),
        (Kind::Quote, _) if block.meta.callout.is_none() => (6., 6.),
        _ => (5., 5.),
    }
}

pub(super) fn block_row(editor: gpui::Entity<Editor>, block: &Block, handles: bool, grip_active: bool, folded: bool, drop_edge: Option<bool>, number: Option<u64>, active: bool, note: Option<std::path::PathBuf>, nowrap: bool, minimal: bool, image_pop: Option<super::image_edit::ImagePop>, cx: &mut gpui::Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean(); let id = block.id;
    let in_col = block.meta.column.is_some();
    let handle_w = if in_col { 42. } else { HANDLE_WIDTH };
    let right_inset = if in_col { 4. } else { RIGHT_INSET };
    let (pt, pb) = block_v_pad(block, minimal, in_col);
    let is_heading = super::fold::heading_level(block).is_some();
    let is_table = block.meta.table.is_some();
    let checked = block.checked;
    let checkbox_editor = editor.clone();
    let marker = match block.kind { Kind::Bullet => "•".into(), Kind::Numbered => format!("{}.", number.unwrap_or(1)), _ => String::new() };
    let (size, factor) = super::paint::heading_metrics(block.kind, block.meta.heading_level, block.meta.code_language.is_some(), minimal, f32::from(theme.editor_font));
    let line = size * factor;
    let indent = block.meta.indent.min(12) as f32 * 12.;
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
    let in_edit_mode = active;
    let preview_only = preview_lang && !in_edit_mode;
    let first_line_h = if rule { 22. } else if code_lang.is_some() { 30. } else { line };
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
                .child(div().absolute().right_0().top(px(handle_top)).h(px(26.)).flex().items_center().justify_end().gap(px(2.))
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
                            window.focus(&this.focus);
                            this.table_menu = None;
                            this.block_menu = None;
                            this.select_whole_table(id, cx);
                            cx.stop_propagation();
                        }))))
                    .when(!minimal && is_heading && (handles || folded), |row| row
                        .child(fold_button(folded, ("fold", id as usize), cx.listener(move |this, _: &gpui::MouseDownEvent, _, cx| {
                            this.toggle_fold(id, cx); cx.stop_propagation();
                        }))))))
            .when(marker_width > 0., |row| row.child(div().w(px(marker_width)).pl(px(indent)).flex_none().text_color(theme.text)
                .text_size(px(size)).line_height(px(line))
                .when(block.kind == Kind::Todo, |row| row.child(todo_box(checked, (line - 17.).max(0.) / 2., move |cx| {
                    checkbox_editor.update(cx, |this, cx| this.toggle_checked(id, cx));
                })))
                .when(!marker.is_empty(), |row| row.child(marker))))
            .child(div().flex_1().min_w_0().flex().flex_col().relative()
                .when(marker_width == 0. && !in_col, |body| body.max_w(theme.content_width))
                .when(block.kind == Kind::Quote && callout.is_none(), |body| body.pl(px(16.)).child(
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
                    div().id(("rule", id as usize)).h(px(22.)).flex().items_center().cursor(CursorStyle::PointingHand)
                        .child(div().h(px(if active { 2. } else { 1. })).w_full().bg(if active { theme.accent } else { theme.hairline }))
                        .on_mouse_down(MouseButton::Left, { let e = focus_editor.clone(); move |_, window, cx| {
                            e.update(cx, |this, cx| this.focus_block(id, window, cx)); cx.stop_propagation();
                        }})))
                .when(!static_view && !preview_only && !nowrap, |body| body.child(BlockText { editor: editor.clone(), block: id }))
                // No-wrap code scrolls sideways inside its own box instead of pushing past the column.
                .when(!static_view && !preview_only && nowrap, |body| body.child(div().id(("code-scroll", id as usize)).w_full().min_w_0().flex()
                    .overflow_x_scroll().child(BlockText { editor: editor.clone(), block: id })))
                .when(!static_view && preview_lang, |body| {
                    let lang = code_lang.as_deref().unwrap_or_default();
                    body.child(super::code_block::preview_card(editor.clone(), id, lang, &block.text, in_edit_mode, cx))
                })
                .when(!static_view && code_lang.is_some(), |body| {
                    body.child(super::code_block::copy_button(editor, id, code_lang.unwrap_or_default(), cx))
                })))
        .when_some(drop_edge, |row, after| row.child(div().absolute().left(px(handle_w)).right(px(right_inset))
            .h(px(2.)).bg(theme.accent)
            .when(after, |line| line.bottom_0()).when(!after, |line| line.top_0())))
}
/// Icon that switches from `base` to `hover` colour while its `group` is hovered.
fn hover_icon(glyph: Icon, size: f32, base: gpui::Hsla, hover: gpui::Hsla) -> impl IntoElement {
    let theme = Theme::ocean();
    let tint = |line| IconColors { line, fill: transparent_black(), accent: theme.accent };
    div().relative().flex_none().size(px(size))
        .child(div().group_hover("hover-icon", |s| s.invisible()).child(icon(glyph, px(size), tint(base))))
        .child(div().absolute().top_0().left_0().invisible().group_hover("hover-icon", |s| s.visible()).child(icon(glyph, px(size), tint(hover))))
}
fn todo_box(checked: bool, top: f32, toggle: impl Fn(&mut gpui::App) + 'static) -> impl IntoElement {
    let dark = is_dark();
    let border: gpui::Hsla = if checked { rgb(0x2f6fe4).into() } else if dark { rgb(0x787772).into() } else { rgb(0x8c8b85).into() };
    let hover_border: gpui::Hsla = if checked { rgb(0x1f57c0).into() } else if dark { rgb(0x9b9a95).into() } else { rgb(0x5e5d59).into() };
    div().mt(px(top)).size(px(17.)).flex().items_center().justify_center().rounded(px(4.)).cursor(CursorStyle::PointingHand)
        .border(px(1.5)).border_color(border).hover(move |s| s.border_color(hover_border))
        .when(checked, |b| b.bg(rgb(0x2f6fe4)).hover(|s| s.bg(rgb(0x1f57c0))).child(icon(Icon::Check, px(12.),
            IconColors { line: rgb(0xffffff).into(), fill: transparent_black(), accent: transparent_black() })))
        .on_mouse_down(MouseButton::Left, move |_, _, cx| { toggle(cx); cx.stop_propagation(); })
}
/// The 6-dot drag grip handle of a block with content.
pub(super) fn grip_button(id: (&'static str, usize), active: bool, listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let bg_hover: gpui::Hsla = if dark { rgb(0x2c2c2a).into() } else { rgb(0xf5f4ed).into() };
    let base_fg: gpui::Hsla = if dark { theme.text_secondary } else { rgba(0x1f1e1d8c).into() };
    let hover_fg: gpui::Hsla = if dark { theme.text } else { rgb(0x1f1e1d).into() };
    div().id(id).group("hover-icon").size(px(26.)).flex_none().rounded(px(8.)).flex().items_center().justify_center()
        .when(active, move |s| s.bg(bg_hover)).hover(move |s| s.bg(bg_hover)).cursor(CursorStyle::PointingHand)
        .child(hover_icon(Icon::Drag, 14., if active { hover_fg } else { base_fg }, hover_fg))
        .on_mouse_down(MouseButton::Left, listener)
}
fn handle_button(glyph: Icon, id: (&'static str, usize), listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let bg_hover: gpui::Hsla = if is_dark() { rgb(0x2c2c2a).into() } else { rgba(0x0b0b0b0d).into() };
    div().id(id).group("hover-icon").size(px(24.)).flex_none().rounded(px(6.)).flex().items_center().justify_center()
        .hover(move |s| s.bg(bg_hover)).cursor(CursorStyle::PointingHand)
        .child(hover_icon(glyph, 14., theme.text_secondary, theme.text))
        .on_mouse_down(MouseButton::Left, listener)
}
fn table_grip_button(id: (&'static str, usize), active: bool, listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    grip_button(id, active, listener)
}
fn fold_button(folded: bool, id: (&'static str, usize), listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let bg_hover: gpui::Hsla = if dark { rgb(0x2c2c2a).into() } else { rgba(0x0b0b0b0d).into() };
    let base_fg: gpui::Hsla = if dark { theme.text_secondary } else { rgb(0x52514e).into() };
    let hover_fg: gpui::Hsla = if dark { theme.text } else { rgb(0x1f1e1d).into() };
    let glyph = if folded { Icon::TbChevronRight } else { Icon::TbChevron };
    let tip = if folded { tr("展开章节", "Expand section") } else { tr("折叠章节", "Collapse section") };
    div().id(id).group("hover-icon").size(px(24.)).flex_none().rounded(px(6.))
        .flex().items_center().justify_center().hover(move |s| s.bg(bg_hover)).cursor(CursorStyle::PointingHand)
        .tooltip(move |window, cx| crate::theme::build_tooltip(tip, window, cx))
        .child(hover_icon(glyph, 14., base_fg, hover_fg))
        .on_mouse_down(MouseButton::Left, listener)
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
    let row = block_row(cx.entity(), block, handles, grip_active, folded, edge, number, this.caret.head.block == block.id && this.block_sel.is_none(), this.path.clone(), this.nowrap.contains(&block.id), this.minimal, image_pop, cx);
    div().when_some(picked.then_some(this.block_sel).flatten(), |d, (a, b)| d.bg(Theme::ocean().selection)
        .when(index == a.min(b), |d| d.rounded_t_md()).when(index == a.max(b), |d| d.rounded_b_md())).child(row).into_any_element()
}
pub(super) use super::toolbar::selection_toolbar;
