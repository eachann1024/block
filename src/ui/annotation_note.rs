use gpui::{Div, ElementId, IntoElement, Stateful, div, prelude::*, px, rgb};

/// Cardless margin note with a single semantic status rule.
pub fn rail_note(id: impl Into<ElementId>, status: u32, content: impl IntoElement) -> Stateful<Div> {
    div().id(id).w_full().border_l_2().border_color(rgb(status)).pl(px(10.)).child(content)
}
