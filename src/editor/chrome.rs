//! Block row chrome: the gutter marker, the hover handle and the selection bar.

use gpui::{CursorStyle, MouseButton, MouseUpEvent, StatefulInteractiveElement, div, prelude::*, px};

use crate::theme::Theme;

use super::model::{Block, Kind, MarkKind};
use super::paint::BlockText;
use super::view::Editor;

pub(super) fn block_row(
    editor: gpui::Entity<Editor>,
    block: Block,
    hovered: bool,
    selected: bool,
    hover_listener: impl Fn(&bool, &mut gpui::Window, &mut gpui::App) + 'static,
    focus_listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
    add_listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
    drag_listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
    move_listener: impl Fn(&gpui::MouseMoveEvent, &mut gpui::Window, &mut gpui::App) + 'static,
    drop_listener: impl Fn(&MouseUpEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let id = block.id;
    let marker = match block.kind {
        Kind::Bullet => "\u{2022}",
        Kind::Numbered => "1.",
        _ => "",
    };
    let text_size = match block.kind {
        Kind::H1 => px(28.),
        Kind::H2 => px(22.),
        Kind::H3 => px(19.),
        _ => theme.editor_font,
    };
    div()
        .id(("block", id as usize))
        .relative()
        .on_hover(hover_listener)
        .on_mouse_down(MouseButton::Left, focus_listener)
        .child(content_row(&block, marker, text_size, editor.clone()))
        .when(hovered, |row| row.child(handle(id, add_listener, drag_listener, move_listener, drop_listener)))
        .when(selected, |row| row.child(toolbar(editor, id)))
}

fn content_row(block: &Block, marker: &str, text_size: gpui::Pixels, editor: gpui::Entity<Editor>) -> impl IntoElement {
    let editor_for_box = editor.clone();
    let theme = Theme::ocean();
    let id = block.id;
    let checked = block.checked;
    div()
        .flex()
        .items_center()
        .gap_2()
        .pl(px(44.))
        .text_size(text_size)
        .text_color(theme.text)
        .when(block.kind == Kind::Quote, |row| {
            row.border_l_2().border_color(theme.quote_bar).pl(px(12.))
        })
        .when(block.kind == Kind::Todo, |row| {
            row.child(
                div()
                    .size(px(14.))
                    .rounded_sm()
                    .border_1()
                    .border_color(theme.accent)
                    .when(checked, |box_| box_.bg(theme.accent))
                    .on_mouse_down(MouseButton::Left, move |_, _, cx| {
                        editor_for_box.update(cx, |this, cx| {
                            this.doc.toggle_checked(id);
                            this.touch(cx);
                        });
                        cx.stop_propagation();
                    }),
            )
        })
        .when(!marker.is_empty(), |row| {
            row.child(div().w(px(20.)).text_color(theme.text_secondary).child(marker.to_string()))
        })
        .child(BlockText { editor, block: id })
}

fn handle(
    id: u64,
    add_listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
    drag_listener: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
    move_listener: impl Fn(&gpui::MouseMoveEvent, &mut gpui::Window, &mut gpui::App) + 'static,
    drop_listener: impl Fn(&MouseUpEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    div()
        .absolute()
        .left(px(0.))
        .top(px(4.))
        .flex()
        .gap(px(2.))
        .child(handle_button("+", ("add", id as usize), theme.clone_pair(), add_listener))
        .child(
            handle_button("\u{2807}", ("drag", id as usize), theme.clone_pair(), drag_listener)
                .on_mouse_move(move_listener)
                .on_mouse_up(MouseButton::Left, drop_listener),
        )
}

fn handle_button(
    label: &'static str,
    id: (&'static str, usize),
    colors: (gpui::Hsla, gpui::Hsla),
    on_down: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    div()
        .id(id)
        .size(px(18.))
        .rounded_sm()
        .flex()
        .items_center()
        .justify_center()
        .text_size(px(13.))
        .text_color(colors.1)
        .hover(|style| style.bg(colors.0))
        .cursor(CursorStyle::PointingHand)
        .child(label)
        .on_mouse_down(MouseButton::Left, on_down)
}

fn toolbar(editor: gpui::Entity<Editor>, id: u64) -> impl IntoElement {
    let theme = Theme::ocean();
    div()
        .absolute()
        .top(px(-40.))
        .left(px(44.))
        .flex()
        .gap(px(2.))
        .p_1()
        .bg(theme.menu)
        .border_1()
        .border_color(theme.hairline)
        .rounded_lg()
        .shadow_md()
        .child(mark_button("B", MarkKind::Bold, editor.clone()))
        .child(mark_button("I", MarkKind::Italic, editor.clone()))
        .child(mark_button("S", MarkKind::Strike, editor.clone()))
        .child(mark_button("</>", MarkKind::Code, editor.clone()))
        .child(heading_button(editor.clone(), id))
        .child(export_button(editor))
}

fn mark_button(label: &'static str, mark: MarkKind, editor: gpui::Entity<Editor>) -> gpui::Div {
    bar_button(label).on_mouse_down(MouseButton::Left, move |_, _, cx| {
        editor.update(cx, |this, cx| this.mark(mark, cx));
        cx.stop_propagation();
    })
}

fn heading_button(editor: gpui::Entity<Editor>, id: u64) -> gpui::Div {
    bar_button("H").on_mouse_down(MouseButton::Left, move |_, _, cx| {
        editor.update(cx, |this, cx| {
            let kind = this.doc.blocks.iter().find(|b| b.id == id).map(|b| b.kind);
            let next = match kind {
                Some(Kind::Paragraph) => Kind::H1,
                Some(Kind::H1) => Kind::H2,
                Some(Kind::H2) => Kind::H3,
                _ => Kind::Paragraph,
            };
            this.doc.set_kind(id, next);
            this.touch(cx);
        });
        cx.stop_propagation();
    })
}

fn export_button(editor: gpui::Entity<Editor>) -> gpui::Div {
    bar_button("图").on_mouse_down(MouseButton::Left, move |_, window, cx| {
        editor.update(cx, |this, cx| super::export_selected(this, window, cx));
        cx.stop_propagation();
    })
}

fn bar_button(label: &'static str) -> gpui::Div {
    let theme = Theme::ocean();
    div()
        .px_2()
        .h(theme.menu_item_height)
        .flex()
        .items_center()
        .text_size(theme.ui_font)
        .text_color(theme.text)
        .rounded_sm()
        .cursor(CursorStyle::PointingHand)
        .hover(|style| style.bg(theme.hover))
        .child(label)
}

trait ColorPair {
    fn clone_pair(&self) -> (gpui::Hsla, gpui::Hsla);
}

impl ColorPair for Theme {
    fn clone_pair(&self) -> (gpui::Hsla, gpui::Hsla) {
        (self.hover, self.text_secondary)
    }
}
