//! Link popover and hover tooltip (F-Inline): hover underlines the link and shows the shortcut
//! tooltip above it; placing the caret inside a link opens the action pill below it.
use std::ops::Range;
use gpui::{AnyElement, ClipboardItem, Context, CursorStyle, Entity, MouseButton, Pixels, Point, Window, div, point, prelude::*, px, rgb};
use crate::{icons::{Icon, stateful_icon}, theme::{Theme, tr}, ui::tooltip::anchored_tooltip_above};
use super::{format_cmds::open_external_url, toolbar::{ToolbarColors, pill, sep}, view::{Editor, selection::Position}};

/// Pointer-hover state for inline links.
#[derive(Default)]
pub(super) struct LinkUi {
    hover: Option<(u64, Range<usize>)>,
    /// Caret restored by opening/navigating to a note: no link pill until the caret really moves.
    pub(super) quiet: Option<Position>,
}

/// `https://host/aaaa…tail`: keeps the head and the end, which carry the host and the file name.
fn middle_ellipsis(url: &str) -> String {
    let chars: Vec<char> = url.chars().collect();
    if chars.len() <= 40 { return url.to_owned(); }
    format!("{}…{}", chars[..28].iter().collect::<String>(), chars[chars.len() - 10..].iter().collect::<String>())
}

/// Splits `https://example.com/path` into `("https://example.com", "/path")` for two-tone display.
fn split_origin(url: &str) -> (&str, &str) {
    if let Some(scheme_end) = url.find("://").map(|i| i + 3)
        && let Some(slash) = url[scheme_end..].find('/')
    {
        return url.split_at(scheme_end + slash);
    }
    (url, "")
}

impl LinkUi {
    pub(super) fn reset_hover(&mut self) { self.hover = None; }
}

impl Editor {
    /// The link under `position`, only when the pointer is really over its text rects.
    pub(super) fn link_under(&self, position: Point<Pixels>) -> Option<(u64, Range<usize>)> {
        let at = self.hit(position)?;
        let block = self.doc.get(at.block)?;
        let (range, _) = block.link_at(at.offset)
            .or_else(|| at.offset.checked_sub(1).and_then(|o| block.link_at(o)))?;
        let over = self.layouts.get(&at.block)?.rects(range.clone()).iter().any(|r| r.dilate(px(2.)).contains(&position));
        over.then_some((at.block, range))
    }
    pub(super) fn update_link_hover(&mut self, position: Point<Pixels>, cx: &mut Context<Self>) {
        let found = self.link_under(position);
        if self.link_ui.hover != found { self.link_ui.hover = found; cx.notify(); }
    }
    /// Link holding a collapsed caret when the editor is focused.
    fn caret_link(&self, window: &Window) -> Option<(u64, Range<usize>, String)> {
        if self.format.is_some() || self.path.is_none() || !self.caret.is_empty()
            || self.selecting || self.dragging.is_some() || self.table_drag.is_some()
            || self.caret.composing.is_some() || !self.focus.is_focused(window) { return None; }
        let head = self.caret.head;
        if self.link_ui.quiet == Some(head) { return None; }
        let block = self.doc.get(head.block)?;
        block.link_at(head.offset)
            .or_else(|| head.offset.checked_sub(1).and_then(|o| block.link_at(o)))
            .map(|(range, url)| (head.block, range, url))
    }
    /// Link byte ranges in `block` that should render with a 1px underline (hovered or holding caret).
    pub(super) fn active_link_ranges(&self, block: u64, window: &Window) -> Vec<Range<usize>> {
        let mut out = Vec::new();
        if let Some((b, range)) = &self.link_ui.hover && *b == block {
            out.push(range.clone());
        }
        if let Some((b, range, _)) = self.caret_link(window) && b == block && !out.contains(&range) {
            out.push(range);
        }
        out
    }
    pub(super) fn open_link_at_caret(&mut self, window: &Window, cx: &mut Context<Self>) {
        let url = self.caret_link(window).map(|(_, _, u)| u).or_else(|| {
            let (b, r) = self.link_ui.hover.as_ref()?;
            self.doc.get(*b)?.link_at(r.start).map(|(_, u)| u)
        });
        if let Some(url) = url {
            open_external_url(&url, self.path.as_deref());
        } else {
            cx.propagate();
        }
    }
    fn select_link(&mut self, block: u64, range: &Range<usize>) {
        self.caret.anchor = Position { block, offset: range.start };
        self.caret.head = Position { block, offset: range.end };
        self.table_sel = None; self.link_ui.hover = None;
    }
    fn edit_link_at(&mut self, block: u64, range: &Range<usize>, url: String, anchor: Point<Pixels>, window: &mut Window, cx: &mut Context<Self>) {
        self.select_link(block, range);
        self.show_link_popover(url, true, anchor, window, cx);
    }
    fn unlink_at(&mut self, block: u64, range: &Range<usize>, window: &mut Window, cx: &mut Context<Self>) {
        self.select_link(block, range);
        self.link_remove(window, cx);
        self.caret.anchor = self.caret.head;
        window.focus(&self.focus, cx); cx.notify();
    }
    fn link_hover_tooltip(&self, window: &Window) -> Option<AnyElement> {
        if self.path.is_none() || self.selecting || self.dragging.is_some() || self.table_drag.is_some()
            || self.caret.composing.is_some() || self.format.is_some() || self.block_menu.is_some() || self.table_menu.is_some() { return None; }
        let (block, range) = self.link_ui.hover.as_ref()?;
        if self.caret_link(window).is_some_and(|(cb, cr, _)| cb == *block && cr == *range) { return None; }
        self.doc.get(*block)?.link_at(range.start)?;
        let rect = *self.layouts.get(block)?.rects(range.clone()).first()?;
        let viewport = self.list.viewport_bounds();
        if rect.bottom() <= viewport.top() || rect.top() >= viewport.bottom() { return None; }
        let label = if cfg!(any(target_os = "macos", target_family = "wasm")) {
            tr("⌘+点击或 ⌥↩ 打开", "⌘+click or ⌥↩ to open")
        } else {
            tr("Ctrl+点击或 Alt+Enter 打开", "Ctrl+click or Alt+Enter to open")
        };
        Some(anchored_tooltip_above(rect, viewport.top(), label, ("link-hover-tip", (*block as usize) ^ (range.start << 16)), window))
    }
    fn link_pill(&self, editor: Entity<Editor>, window: &Window) -> Option<AnyElement> {
        let (block, range, url) = self.caret_link(window)?;
        let rect = *self.layouts.get(&block)?.rects(range.clone()).first()?;
        let viewport = self.list.viewport_bounds();
        if rect.bottom() <= viewport.top() || rect.top() >= viewport.bottom() { return None; }
        let anchor = point(rect.left(), rect.bottom());
        let tc = ToolbarColors::current();
        let display = middle_ellipsis(&url);
        let (origin, tail) = split_origin(&display);
        let (open_url, copy_url) = (url.clone(), url.clone());
        let base_note = self.path.clone();
        let (edit_range, unlink_range, edit_url) = (range.clone(), range, url);
        let (e_edit, e_unlink, e_copy) = (editor.clone(), editor.clone(), editor);
        let theme = Theme::ocean();
        let button = |id: &'static str, g: Icon, tip: &'static str, run: Box<dyn Fn(&mut Window, &mut gpui::App)>| div().id(id).group(id)
            .size(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(6.)).cursor(CursorStyle::PointingHand)
            .hover(move |s| s.bg(rgb(tc.hover)))
            .child(stateful_icon(id, g, px(16.), false, theme.mono_icon_colors(rgb(tc.ink).into()), theme.icon_colors(rgb(tc.ink).into())))
            .tooltip(crate::theme::tooltip(tip))
            .on_mouse_down(MouseButton::Left, move |_, window, cx| { run(window, cx); cx.stop_propagation(); });
        let url_label = div().flex().items_center().text_size(px(13.)).whitespace_nowrap()
            .child(div().text_color(rgb(tc.ink)).child(origin.to_owned()))
            .when(!tail.is_empty(), |d| d.child(div().text_color(rgb(tc.muted)).child(tail.to_owned())));
        let bar = pill().id("link-pill").pl(px(12.)).gap(px(2.))
            .child(url_label)
            .child(sep())
            .child(button("link-open", Icon::LinkOpen, tr("打开链接", "Open link"), Box::new(move |_, _| open_external_url(&open_url, base_note.as_deref()))))
            .child(button("link-copy", Icon::LinkCopy, tr("复制链接", "Copy link"), Box::new(move |_, cx| {
                cx.write_to_clipboard(ClipboardItem::new_string(copy_url.clone()));
                e_copy.update(cx, |this, cx| { this.status = tr("已复制链接", "Link copied").into(); cx.notify(); });
            })))
            .child(button("link-edit", Icon::LinkEdit, tr("编辑链接", "Edit link"), Box::new(move |window, cx| {
                e_edit.update(cx, |this, cx| this.edit_link_at(block, &edit_range, edit_url.clone(), anchor, window, cx));
            })))
            .child(button("link-unlink", Icon::LinkOff, tr("移除链接", "Remove link"), Box::new(move |window, cx| {
                e_unlink.update(cx, |this, cx| this.unlink_at(block, &unlink_range, window, cx));
            })));
        Some(gpui::deferred(gpui::anchored().position(anchor).snap_to_window_with_margin(px(8.)).child(
            div().pt(px(4.)).on_mouse_move(|_, _, cx| cx.stop_propagation()).on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .child(crate::ui::floating_bar::animate(bar, "link-pill-enter")))).into_any_element())
    }
    pub(super) fn link_overlays(&self, editor: Entity<Editor>, window: &Window) -> Vec<AnyElement> {
        self.link_pill(editor, window).into_iter().chain(self.link_hover_tooltip(window)).collect()
    }
}
