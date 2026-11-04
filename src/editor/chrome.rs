//! Stable gutters, a persistent width switch, and selection-only formatting.
use gpui::{CursorStyle, MouseButton, Pixels, div, prelude::*, px, rgb, transparent_black};
use crate::{icons::{Icon, IconColors, icon}, theme::Theme};
use super::{model::{Block, Kind}, paint::BlockText, view::Editor};

const HANDLE_WIDTH: f32 = 44.;
const MARKER_WIDTH: f32 = 24.;
const ROW_GAP: f32 = 8.;
const BLOCK_PAD: f32 = 7.;
/// Estimated floating toolbar size, used to keep it inside the editor.
pub(super) const TOOLBAR_WIDTH: Pixels = px(440.);
pub(super) const TOOLBAR_HEIGHT: Pixels = px(42.);
const RIGHT_INSET: f32 = 12.;
pub(super) fn standard_width(body_width: Pixels) -> Pixels {
    body_width + px(HANDLE_WIDTH + ROW_GAP + RIGHT_INSET)
}

pub(super) fn block_row(editor: gpui::Entity<Editor>, block: &Block, handles: bool, drop_edge: Option<bool>, number: Option<u64>, active: bool, note: Option<std::path::PathBuf>, nowrap: bool, minimal: bool, cx: &mut gpui::Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean(); let id = block.id;
    let checked = block.checked;
    let checkbox_editor = editor.clone();
    let marker = match block.kind { Kind::Bullet => "•".into(), Kind::Numbered => format!("{}.", number.unwrap_or(1)), _ => String::new() };
    let (size, factor) = super::paint::block_metrics(block.kind, block.meta.code_language.is_some(), minimal, f32::from(theme.editor_font)); let line = size * factor;
    let indent = block.meta.indent.min(12) as f32 * 12.;
    let marker_width = if matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) { MARKER_WIDTH + indent } else { indent };
    let image = super::image_source::spec_of(block)
        .and_then(|spec| super::image_source::resolve_path(&spec, note.as_deref()).map(|path| (path, spec)));
    let rule = super::block_menu::is_divider(block);
    let nowrap = nowrap && block.meta.code_language.is_some();
    let focus_editor = editor.clone();
    let blank = block.text.is_empty() && block.meta.table.is_none() && block.meta.raw_source.is_none();
    // Images never expose their Markdown/HTML line, as in the reference; dividers show source only while focused.
    let static_view = image.is_some() || (!active && rule);
    div().id(("block", id as usize)).relative().w_full().min_w_0().py(px(if minimal { 5. } else { BLOCK_PAD }))
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| {
            if this.minimal { return; }
            let next = if *hovered { Some(id) } else if this.hover == Some(id) { None } else { this.hover };
            if this.hover != next { this.hover = next; cx.notify(); }
        }))
        .child(div().flex().items_start().w_full().min_w_0().gap(px(ROW_GAP)).pr(px(RIGHT_INSET))
            .child(div().w(px(HANDLE_WIDTH)).flex_none().flex().justify_end().gap(px(2.)).pt(px((line - 22.).max(0.) / 2.))
                .when(handles && !blank, |row| row
                    .child(grip_button(type_mark(block), id, ("drag", id as usize), cx.listener(move |this, event: &gpui::MouseDownEvent, _, cx| {
                        this.begin_drag(id, cx); this.grip_origin = Some(event.position); cx.stop_propagation();
                    })).on_mouse_up(MouseButton::Left, cx.listener(move |this, event: &gpui::MouseUpEvent, _, cx| {
                        // A press-release on the handle itself is a click, whatever the root saw.
                        if this.dragging == Some(id) { this.finish_drag(event.position, cx); cx.stop_propagation(); }
                    }))))
                .when(handles && blank, |row| row
                    .child(handle_button(Icon::Plus, ("add", id as usize), cx.listener(move |this, event: &gpui::MouseDownEvent, window, cx| {
                        this.add_below(id, window, cx);
                        let at = this.caret.head.block;
                        this.open_block_menu(at, event.position + gpui::point(px(-10.), px(22.)), super::block_menu::Mode::Slash, cx);
                        cx.stop_propagation();
                    })))))
            .when(marker_width > 0., |row| row.child(div().w(px(marker_width)).pl(px(indent)).flex_none().text_color(theme.text_secondary)
                .text_size(px(size)).line_height(px(line))
                .when(block.kind == Kind::Todo, |row| row.child(todo_box(checked, (line - 16.).max(0.) / 2., move |cx| {
                    checkbox_editor.update(cx, |this, cx| this.toggle_checked(id, cx));
                })))
                .when(!marker.is_empty(), |row| row.child(marker))))
            .child(div().flex_1().min_w_0().flex().flex_col().relative()
                .when(marker_width == 0., |body| body.max_w(theme.content_width))
                .when(block.kind == Kind::Quote, |body| body.pl(px(14.)).child(
                    div().absolute().left_0().top_0().bottom_0().w(px(2.)).bg(theme.quote_bar)))
                .when_some(block.meta.unsupported_reason.clone().filter(|_| image.is_none() && !rule), |body, reason| body.child(
                    div().text_size(px(11.)).text_color(theme.text_secondary).pb_1()
                        .child(format!("已保留源码 · {reason}"))))
                .when_some(image, |body, (path, spec)| body.child(super::image_view::image_block(focus_editor.clone(), id, path, spec, active, cx)))
                .when(static_view && rule, |body| body.child(
                    div().id(("rule", id as usize)).h(px(22.)).flex().items_center().cursor(CursorStyle::PointingHand)
                        .child(div().h(px(1.)).w_full().bg(theme.hairline))
                        .on_mouse_down(MouseButton::Left, { let e = focus_editor.clone(); move |_, window, cx| {
                            e.update(cx, |this, cx| this.focus_block(id, window, cx)); cx.stop_propagation();
                        }})))
                .when(!static_view && !nowrap, |body| body.child(BlockText { editor: editor.clone(), block: id }))
                // No-wrap code scrolls sideways inside its own box instead of pushing past the column.
                .when(!static_view && nowrap, |body| body.child(div().id(("code-scroll", id as usize)).w_full().min_w_0().flex()
                    .overflow_x_scroll().child(BlockText { editor: editor.clone(), block: id })))
                .when(!static_view && block.meta.code_language.is_some(), |body| {
                    body.child(super::code_block::copy_button(editor, id, cx))
                })))
        .when_some(drop_edge, |row, after| row.child(div().absolute().left(px(HANDLE_WIDTH)).right(px(RIGHT_INSET))
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
    let theme = Theme::ocean();
    div().mt(px(top)).size(px(16.)).flex().items_center().justify_center().rounded(px(4.))
        .border(px(1.5)).border_color(if checked { theme.accent } else { rgb(0xc3cad4).into() })
        .when(checked, |b| b.bg(theme.accent).child(icon(Icon::Check, px(12.),
            IconColors { line: rgb(0xffffff).into(), fill: transparent_black(), accent: transparent_black() })))
        .on_mouse_down(MouseButton::Left, move |_, _, cx| { toggle(cx); cx.stop_propagation(); })
}
/// Compact block-type mark drawn before the six-dot grip: an icon where the set has one, else a short glyph.
enum TypeMark { Icon(Icon) }
fn type_mark(block: &Block) -> TypeMark {
    use super::block_menu::BlockType;
    match super::block_menu::block_type(block) {
        BlockType::Code => TypeMark::Icon(Icon::Code), BlockType::Image => TypeMark::Icon(Icon::Image),
        BlockType::Table => TypeMark::Icon(Icon::Table), BlockType::Divider => TypeMark::Icon(Icon::Divider),
        BlockType::Source => TypeMark::Icon(Icon::Source),
        BlockType::Text(kind) => TypeMark::Icon(super::block_menu::kind_icon(kind)),
    }
}
/// The handle of a block with content: type mark plus grip in one 22px-high target, as in the reference.
fn grip_button(mark: TypeMark, block: u64, id: (&'static str, usize), listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let (base, hover) = (theme.text_secondary, theme.text);
    let TypeMark::Icon(glyph) = mark;
    let mark = hover_icon(glyph, 14., base, hover);
    div().id(id).relative().group("hover-icon").h(px(22.)).flex_none().px(px(3.)).gap(px(1.)).rounded(px(4.)).flex().items_center()
        .hover(|s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
        .child(mark)
        .child(hover_icon(Icon::Drag, 14., base, hover))
        // Remember where the handle sits so its menu opens beside it, not at the pointer.
        .child(gpui::canvas(move |bounds, _, _| { GRIP_BOUNDS.with(|b| { b.borrow_mut().insert(block, bounds); }); }, |_, _, _, _| {})
            .absolute().top_0().left_0().size_full())
        .on_mouse_down(MouseButton::Left, listener)
}
thread_local! {
    static GRIP_BOUNDS: std::cell::RefCell<std::collections::HashMap<u64, gpui::Bounds<gpui::Pixels>>> = Default::default();
}
/// Last painted bounds of a block's handle, in window coordinates.
pub(super) fn grip_bounds(block: u64) -> Option<gpui::Bounds<gpui::Pixels>> { GRIP_BOUNDS.with(|b| b.borrow().get(&block).copied()) }
fn handle_button(glyph: Icon, id: (&'static str, usize), listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id(id).group("hover-icon").size(px(22.)).flex_none().rounded(px(4.)).flex().items_center().justify_center()
        .hover(|s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
        .child(hover_icon(glyph, 14., theme.text_secondary, theme.text))
        .on_mouse_down(MouseButton::Left, listener)
}
pub(super) use super::toolbar::selection_toolbar;
