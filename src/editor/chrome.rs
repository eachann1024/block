//! Constant gutter geometry and a toolbar outside the scrolling viewport.
use gpui::{CursorStyle, MouseButton, div, prelude::*, px};
use crate::{icons::{Icon, icon}, theme::Theme};
use super::{model::{Block, Kind, MarkKind}, paint::BlockText, view::Editor};

pub(super) fn block_row(editor: gpui::Entity<Editor>, block: &Block, hovered: bool, drop_target: bool, number: Option<u64>, cx: &mut gpui::Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean(); let id = block.id;
    let checked = block.checked;
    let checkbox_editor = editor.clone();
    let marker = match block.kind { Kind::Bullet => "•".into(), Kind::Numbered => format!("{}.", number.unwrap_or(1)), Kind::Quote => "▏".into(), _ => String::new() };
    div().id(("block", id as usize)).relative().w_full().py(px(2.))
        .border_t_1().border_color(if drop_target { theme.accent } else { gpui::transparent_black() })
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| { this.hover = hovered.then_some(id); cx.notify(); }))
        .on_mouse_down(MouseButton::Left, cx.listener(|this, event, window, cx| this.pointer_down(event, window, cx)))
        .child(div().flex().items_start().w_full().gap(px(6.)).pr(px(12.))
            .child(div().w(px(42.)).flex_none().flex().pt(px(4.))
                .when(hovered, |row| row
                    .child(handle_button(Icon::Plus, ("add", id as usize), cx.listener(move |this, _, window, cx| {
                        this.add_below(id, window, cx); cx.stop_propagation();
                    })))
                    .child(handle_button(Icon::Drag, ("drag", id as usize), cx.listener(move |this, _, _, cx| {
                        if this.path.is_some() { this.commit_composition(cx); this.dragging = Some(id); this.selecting = false; }
                        cx.stop_propagation();
                    })))))
            .child(div().w(px(24.+block.meta.indent.min(12) as f32*12.)).flex_none().text_color(theme.text_secondary).pt(px(3.))
                .when(block.kind == Kind::Todo, |row| row.child(div().size(px(14.)).rounded_sm().border_1().border_color(theme.accent)
                    .when(checked, |checkbox| checkbox.bg(theme.accent))
                    .on_mouse_down(MouseButton::Left, move |_, _, cx| {
                        checkbox_editor.update(cx, |this, cx| this.toggle_checked(id, cx)); cx.stop_propagation();
                    })))
                .when(!marker.is_empty(), |row| row.child(marker)))
            .child(BlockText { editor, block: id }))
}
fn handle_button(glyph: Icon, id: (&'static str, usize), listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id(id).size(px(20.)).rounded_sm().flex().items_center().justify_center()
        .hover(|s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
        .child(icon(glyph, px(16.), theme.icon_colors(theme.text_secondary)))
        .on_mouse_down(MouseButton::Left, listener)
}
pub(super) fn toolbar(editor: gpui::Entity<Editor>, enabled: bool, id: u64) -> impl IntoElement {
    let theme = Theme::ocean();
    div().h(px(38.)).flex_none().flex().items_center().justify_center().gap(px(2.))
        .border_b_1().border_color(theme.hairline).bg(theme.menu)
        .when(enabled, |bar| bar
            .child(mark_button(Icon::Bold, MarkKind::Bold, editor.clone()))
            .child(mark_button(Icon::Italic, MarkKind::Italic, editor.clone()))
            .child(mark_button(Icon::Strike, MarkKind::Strike, editor.clone()))
            .child(mark_button(Icon::Code, MarkKind::Code, editor.clone()))
            .child(heading_button(editor.clone(), id))
            .child(export_button(editor)))
}
fn mark_button(glyph: Icon, mark: MarkKind, editor: gpui::Entity<Editor>) -> gpui::Div {
    bar_button(glyph).on_mouse_down(MouseButton::Left, move |_, _, cx| {
        editor.update(cx, |this, cx| this.mark(mark, cx)); cx.stop_propagation();
    })
}
fn heading_button(editor: gpui::Entity<Editor>, id: u64) -> gpui::Div {
    bar_button(Icon::Heading).on_mouse_down(MouseButton::Left, move |_, _, cx| {
        editor.update(cx, |this, cx| this.cycle_heading(id, cx)); cx.stop_propagation();
    })
}
fn export_button(editor: gpui::Entity<Editor>) -> gpui::Div {
    bar_button(Icon::Image).ml(px(6.)).on_mouse_down(MouseButton::Left, move |_, window, cx| {
        editor.update(cx, |this, cx| super::export_selected(this, window, cx)); cx.stop_propagation();
    })
}
fn bar_button(glyph: Icon) -> gpui::Div {
    let theme = Theme::ocean();
    div().size(px(28.)).flex_none().flex().items_center().justify_center().rounded_md()
        .cursor(CursorStyle::PointingHand).hover(|s| s.bg(theme.hover))
        .child(icon(glyph, px(16.), theme.icon_colors(theme.text_secondary)))
}
