//! Shared bounded multi-line composer; actions stay outside its clipped viewport.
use gpui::{Entity, IntoElement, Styled, div, prelude::*, px};
use gpui_component::input::{Textarea, TextareaState};

pub fn field(state: &Entity<TextareaState>) -> impl IntoElement {
    div().relative().w_full().min_w_0().flex_none().overflow_hidden()
        .text_size(px(14.)).line_height(px(22.))
        .children(super::smoke_overlay::probe("ai-composer-input"))
        .child(Textarea::new(state).appearance(false).bordered(false)
            .w_full().line_height(px(22.)).px(px(2.)).py(px(0.)))
}
