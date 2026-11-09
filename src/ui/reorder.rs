//! Reorderable row wrapper: native drag threshold, preview and destination feedback.
use gpui::{Context, SharedString, Window, div, prelude::*, px};
use crate::{icons::{Icon, icon}, theme::Theme};

#[derive(Clone)]
pub struct DragValue<T: Clone + 'static> {
    pub value: T,
}

struct Preview { label: SharedString, glyph: Icon }
impl Render for Preview {
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        div().h(px(30.)).px(px(10.)).max_w(px(260.)).flex().items_center().gap(px(8.))
            .rounded(super::radius::CONTROL).bg(theme.menu).border_1().border_color(theme.accent)
            .text_size(theme.ui_font).text_color(theme.text)
            .child(icon(self.glyph, px(16.), theme.icon_colors(theme.text)))
            .child(div().overflow_hidden().whitespace_nowrap().text_ellipsis().child(self.label.clone()))
    }
}

pub fn row<T: Clone + 'static, E: StatefulInteractiveElement>(
    value: T, label: String, glyph: Icon, content: E,
) -> E {
    let theme = Theme::ocean();
    content.on_drag(DragValue { value }, move |_, _, _, cx| {
            cx.new(|_| Preview { label: label.clone().into(), glyph })
        })
        .drag_over::<DragValue<T>>(move |style, _, _, _| style.bg(theme.selected))
}

/// Move to the target's original index, including both ends of the list.
pub fn move_to<T: PartialEq>(items: &mut Vec<T>, source: &T, target: &T) -> bool {
    let Some(from) = items.iter().position(|item| item == source) else { return false };
    let Some(to) = items.iter().position(|item| item == target) else { return false };
    if from == to { return false; }
    let item = items.remove(from);
    items.insert(to, item);
    true
}
