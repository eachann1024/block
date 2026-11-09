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

#[derive(Clone, Default)]
pub struct DropPosition(std::rc::Rc<std::cell::Cell<Option<gpui::Bounds<gpui::Pixels>>>>);
impl DropPosition {
    pub fn after(&self, window: &Window) -> bool {
        self.0.get().is_some_and(|bounds| window.mouse_position().y > bounds.center().y)
    }
    pub fn middle(&self, window: &Window) -> bool {
        self.0.get().is_some_and(|b| {
            let y = window.mouse_position().y;
            y > b.top() + b.size.height * 0.25 && y < b.bottom() - b.size.height * 0.25
        })
    }
}

pub fn insertion_target<T: 'static, E: StatefulInteractiveElement + ParentElement + Styled>(
    content: E, position: DropPosition, folder: bool, accepts: impl Fn(&T) -> bool + 'static,
) -> E {
    let bounds = position.0.clone();
    let edge = std::rc::Rc::new(std::cell::Cell::new(None));
    let paint_edge = edge.clone();
    content.relative().child(super::placement::track_bounds(bounds, false))
        // Paint independently of the row border: square ends, no layout changes.
        .child(gpui::canvas(|_, _, _| (), move |bounds, _, window, _| {
            if let Some(after) = paint_edge.take() {
                let y = if after { bounds.bottom() - px(2.) } else { bounds.top() };
                let line = gpui::Bounds::new(gpui::point(bounds.left(), y), gpui::size(bounds.size.width, px(2.)));
                window.paint_quad(gpui::fill(line, Theme::ocean().accent));
            }
        }).absolute().top_0().left_0().size_full())
        .on_drag_move::<T>(|_, window, _| window.refresh())
        .drag_over::<T>(move |style, item, window, _| {
            if !accepts(item) { return style; }
            let theme = Theme::ocean();
            if folder && position.middle(window) { return style.bg(theme.selected).border_color(theme.accent); }
            edge.set(Some(position.after(window)));
            style
        })
}

pub fn row<T: Clone + 'static, E: StatefulInteractiveElement + ParentElement + Styled>(
    value: T, label: String, glyph: Icon, content: E, position: DropPosition,
) -> E {
    let content = content.on_drag(DragValue { value }, move |_, _, _, cx| {
        cx.new(|_| Preview { label: label.clone().into(), glyph })
    });
    insertion_target::<DragValue<T>, _>(content, position, false, |_| true)
}

/// Insert before/after a target, correcting the index after removing the source.
pub fn move_to<T: PartialEq>(items: &mut Vec<T>, source: &T, target: &T, after: bool) -> bool {
    let Some(from) = items.iter().position(|item| item == source) else { return false };
    let Some(to) = items.iter().position(|item| item == target) else { return false };
    let slot = to + usize::from(after);
    let slot = slot - usize::from(from < slot);
    if from == slot { return false; }
    let item = items.remove(from);
    items.insert(slot, item);
    true
}
