//! Image size pill (− / percent / + / original) and the 10% keyboard steps.
use gpui::{Context, CursorStyle, Entity, MouseButton, Window, div, prelude::*, px, rgb};
use crate::icons::Icon;
use super::{image_source::spec_of, image_view::sizes, toolbar::{HOVER, INK, glyph, pill, sep}, view::Editor};

fn step_button(editor: &Entity<Editor>, id: u64, slot: usize, g: Icon, tip: &'static str,
    run: impl Fn(&mut Editor, &mut Context<Editor>) + 'static) -> impl IntoElement {
    let editor = editor.clone();
    div().id(("image-size", id as usize * 4 + slot)).size(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(6.))
        .cursor(CursorStyle::PointingHand).hover(|s| s.bg(rgb(HOVER))).child(glyph(g, 15., INK))
        .tooltip(move |window, cx| gpui_component::tooltip::Tooltip::new(tip).build(window, cx))
        .on_mouse_down(MouseButton::Left, move |_, _: &mut Window, cx| {
            editor.update(cx, |this, cx| run(this, cx)); cx.stop_propagation();
        })
}
/// Dark pill at the image's top-right corner showing the share of the content width.
pub(super) fn size_pill(editor: &Entity<Editor>, id: u64, percent: u32) -> impl IntoElement {
    let reset_editor = editor.clone();
    div().absolute().top(px(10.)).right(px(10.)).on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .child(pill().p(px(3.)).gap(px(2.))
            .child(step_button(editor, id, 0, Icon::TbMinus, "缩小", move |this, cx| this.step_image(id, -10, cx)))
            .child(div().min_w(px(40.)).flex().justify_center().text_size(px(12.)).text_color(rgb(INK)).child(format!("{percent}%")))
            .child(step_button(editor, id, 1, Icon::TbPlus, "放大", move |this, cx| this.step_image(id, 10, cx)))
            .child(sep())
            .child(div().id(("image-size", id as usize * 4 + 2)).h(px(24.)).px(px(7.)).flex_none().flex().items_center().justify_center()
                .rounded(px(5.)).text_size(px(11.)).text_color(rgb(0xe5e7eb)).cursor(CursorStyle::PointingHand)
                .hover(|s| s.bg(rgb(HOVER))).child("原始宽度")
                .on_mouse_down(MouseButton::Left, move |_, _: &mut Window, cx| {
                    reset_editor.update(cx, |this, cx| this.edit_image(id, |s| s.width = None, cx));
                    cx.stop_propagation();
                })))
}

impl Editor {
    /// Moves the width to the next multiple of 10% of the original width, within 10%..=100%.
    pub(super) fn step_image(&mut self, id: u64, delta: i32, cx: &mut Context<Self>) {
        let (base, _) = sizes(cx, id);
        if base <= 0. { return; }
        let Some(spec) = self.doc.get(id).and_then(spec_of) else { return };
        let current_pct = spec.width.map_or(100., |w| (w as f32 / base * 10.).round() * 10.);
        let percent = (current_pct + delta as f32).clamp(10., 100.);
        let width = (base * percent / 100.).round() as u32;
        self.edit_image(id, |s| s.width = Some(width), cx);
    }
    /// Cmd+= / Cmd+-: resize the image holding the caret, if the caret block is one.
    pub(super) fn step_selected_image(&mut self, delta: i32, cx: &mut Context<Self>) {
        let id = self.caret.head.block;
        if self.doc.get(id).and_then(spec_of).is_some() { self.step_image(id, delta, cx); }
    }
}
