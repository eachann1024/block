//! Link popover (F-Inline): a dark pill under the link at the caret or pointer with open / copy / edit / unlink.
use std::ops::Range;
use gpui::{AnyElement, ClipboardItem, Context, CursorStyle, Entity, MouseButton, Pixels, Point, Window, div, point, prelude::*, px, rgb};
use crate::icons::Icon;
use super::{format_cmds::{FormatPop, normalize_url}, toolbar::{HOVER, INK, glyph, pill, sep}, view::{Editor, selection::Position}};

/// Pointer-hover state for the link popover.
#[derive(Default)]
pub(super) struct LinkUi { hover: Option<(u64, Range<usize>)> }

/// `https://host/aaaa…tail`: keeps the head and the end, which carry the host and the file name.
fn middle_ellipsis(url: &str) -> String {
    let chars: Vec<char> = url.chars().collect();
    if chars.len() <= 40 { return url.to_owned(); }
    format!("{}…{}", chars[..28].iter().collect::<String>(), chars[chars.len() - 10..].iter().collect::<String>())
}

impl Editor {
    /// The link under `position`, only when the pointer is really over its text.
    fn link_under(&self, position: Point<Pixels>) -> Option<(u64, Range<usize>)> {
        let at = self.hit(position)?;
        let (range, _) = self.doc.get(at.block)?.link_at(at.offset)?;
        let over = self.layouts.get(&at.block)?.rects(range.clone()).iter().any(|r| r.dilate(px(2.)).contains(&position));
        over.then_some((at.block, range))
    }
    pub(super) fn update_link_hover(&mut self, position: Point<Pixels>, cx: &mut Context<Self>) {
        let found = self.link_under(position);
        if self.link_ui.hover != found { self.link_ui.hover = found; cx.notify(); }
    }
    /// Link to show a popover for: the hovered one, else the one holding a collapsed caret.
    fn link_target(&self, window: &Window) -> Option<(u64, Range<usize>, String)> {
        if self.format.is_some() || self.path.is_none() || !self.caret.is_empty() && self.link_ui.hover.is_none()
            || self.selecting || self.dragging.is_some() || self.caret.composing.is_some() { return None; }
        let found = |block: u64, offset: usize| self.doc.get(block).and_then(|b| b.link_at(offset)).map(|(range, url)| (block, range, url));
        if let Some((block, range)) = &self.link_ui.hover { if let Some(hit) = found(*block, range.start) { return Some(hit); } }
        if self.caret.is_empty() || !self.focus.is_focused(window) { return None; }
        found(self.caret.head.block, self.caret.head.offset)
    }
    fn select_link(&mut self, block: u64, range: &Range<usize>) {
        self.caret.anchor = Position { block, offset: range.start };
        self.caret.head = Position { block, offset: range.end };
        self.table_sel = None; self.link_ui.hover = None;
    }
    fn edit_link_at(&mut self, block: u64, range: &Range<usize>, url: String, anchor: Point<Pixels>, window: &mut Window, cx: &mut Context<Self>) {
        self.select_link(block, range);
        self.format = Some(FormatPop::Link { url, existing: true, anchor });
        window.focus(&self.link_focus); cx.notify();
    }
    fn unlink_at(&mut self, block: u64, range: &Range<usize>, window: &mut Window, cx: &mut Context<Self>) {
        self.select_link(block, range);
        self.link_remove(window, cx);
        self.caret.anchor = self.caret.head;
        window.focus(&self.focus); cx.notify();
    }
    pub(super) fn link_pill(&self, editor: Entity<Editor>, window: &Window) -> Option<AnyElement> {
        let (block, range, url) = self.link_target(window)?;
        let rect = *self.layouts.get(&block)?.rects(range.clone()).first()?;
        let anchor = point(rect.left(), rect.bottom());
        let (open_url, copy_url) = (url.clone(), url.clone());
        let (edit_range, unlink_range, edit_url) = (range.clone(), range, url.clone());
        let (e_edit, e_unlink, e_copy) = (editor.clone(), editor.clone(), editor);
        let button = |id: &'static str, g: Icon, tip: &'static str, run: Box<dyn Fn(&mut Window, &mut gpui::App)>| div().id(id)
            .size(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(6.)).cursor(CursorStyle::PointingHand)
            .hover(|s| s.bg(rgb(HOVER))).child(glyph(g, 16., INK))
            .tooltip(move |window, cx| gpui_component::tooltip::Tooltip::new(tip).build(window, cx))
            .on_mouse_down(MouseButton::Left, move |_, window, cx| { run(window, cx); cx.stop_propagation(); });
        Some(gpui::deferred(gpui::anchored().position(anchor).snap_to_window_with_margin(px(8.)).child(
            // The transparent 4px top padding belongs to the popover, so the pointer can cross from text to pill.
            div().pt(px(4.)).on_mouse_move(|_, _, cx| cx.stop_propagation()).on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .child(pill().id("link-pill").pl(px(12.)).gap(px(2.))
                    .child(div().text_size(px(13.)).text_color(rgb(INK)).whitespace_nowrap().child(middle_ellipsis(&url)))
                    .child(sep())
                    .child(button("link-open", Icon::LinkOpen, "打开", Box::new(move |_, cx| cx.open_url(&normalize_url(&open_url)))))
                    .child(button("link-copy", Icon::LinkCopy, "复制", Box::new(move |_, cx| {
                        cx.write_to_clipboard(ClipboardItem::new_string(copy_url.clone()));
                        e_copy.update(cx, |this, cx| { this.status = "已复制链接".into(); cx.notify(); });
                    })))
                    .child(button("link-edit", Icon::LinkEdit, "编辑", Box::new(move |window, cx| {
                        e_edit.update(cx, |this, cx| this.edit_link_at(block, &edit_range, edit_url.clone(), anchor, window, cx));
                    })))
                    .child(button("link-unlink", Icon::LinkOff, "移除链接", Box::new(move |window, cx| {
                        e_unlink.update(cx, |this, cx| this.unlink_at(block, &unlink_range, window, cx));
                    })))))).into_any_element())
    }
}
