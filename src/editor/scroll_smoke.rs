//! Isolated scroll-boundary probes for the built-in smoke worker.
use gpui::{Context, Window, Pixels, Bounds};
use super::{view::Editor, markdown};
impl Editor {
    pub(crate) fn prepare_scroll_smoke(&mut self, path: std::path::PathBuf, window: &mut Window, cx: &mut Context<Self>) {
        assert!(crate::ui::smoke_overlay::is_smoke_active());
        self.minimal = true;
        self.path = Some(path);
        let mut text = (0..60).map(|i| format!("## 标题 {i}\n\n滚动到底后持续触摸板滚动，内容应该保持稳定。\n\n")).collect::<String>();
        text.push_str("/");
        self.reset_document(markdown::parse(&text));
        let id = self.doc.blocks.last().unwrap().id;
        self.focus_block(id, window, cx);
        cx.notify();
    }
    pub(crate) fn scroll_smoke_geometry(&self) -> (Bounds<Pixels>, Option<Bounds<Pixels>>) {
        (self.list.viewport_bounds(), self.doc.blocks.last().and_then(|b| self.layouts.get(&b.id)).map(|l| l.bounds))
    }
    pub(crate) fn menu_scroll_smoke_offset(&self) -> (gpui::Point<Pixels>, gpui::Point<Pixels>) {
        (self.block_menu_scroll.offset(), self.block_menu_scroll.max_offset())
    }
    pub(crate) fn scroll_smoke_menu(&mut self, cx: &mut Context<Self>) {
        let Some(last) = self.doc.blocks.last() else { return };
        let id = last.id;
        let bounds = self.layouts.get(&id).map(|l| l.bounds).unwrap_or_else(|| self.list.viewport_bounds());
        self.open_block_menu_at(id, bounds, super::block_menu::Mode::Slash, cx);
    }
}
