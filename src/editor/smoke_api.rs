//! Read-only probes for the `--smoke` run; they expose state without widening field visibility.
use super::{markdown, model::Kind, view::Editor};

impl Editor {
    pub(crate) fn slash_menu_open(&self) -> bool { self.block_menu.is_some() }
    pub(crate) fn markdown_text(&self) -> String { markdown::serialize(&self.doc) }
    pub(crate) fn block_count(&self) -> usize { self.doc.blocks.len() }
    pub(crate) fn first_line_delete_armed(&self) -> bool {
        self.doc.blocks.first().is_some_and(|b| self.is_first_line_delete_armed(b.id))
    }
    pub fn first_line_placeholder(&self) -> &'static str {
        if self.first_line_delete_armed() { crate::ui::reveal::delete_arm_placeholder_text() }
        else { crate::theme::tr("输入 / 或 、 唤出菜单，或开始输入…", "Type / to open menu, or start typing…") }
    }
    pub(crate) fn first_block_text(&self) -> String {
        self.doc.blocks.first().map(|b| b.text.clone()).unwrap_or_default()
    }
    pub(crate) fn has_todo_checked(&self, expected: bool) -> bool {
        self.doc.blocks.iter().any(|b| b.kind == Kind::Todo && b.checked == expected)
    }
    pub(crate) fn find_is_open(&self) -> bool { self.find.is_some() }
    pub(crate) fn find_match_count(&self) -> usize { self.find.as_ref().map_or(0, |f| f.matches.len()) }
    pub(crate) fn find_current_index(&self) -> usize { self.find.as_ref().map_or(0, |f| f.current) }
    pub(crate) fn has_selection_toolbar(&self) -> bool { self.toolbar_anchor.is_some() && !self.caret.is_empty() }
}

impl Editor {
    pub(crate) fn prepare_annotation_smoke(&mut self, window: &mut gpui::Window, cx: &mut gpui::Context<Self>) {
        assert!(crate::ui::smoke_overlay::is_smoke_active());
        self.reset_document(markdown::parse("- 古人一天吃什么
- 古人怎么刷牙
- 没有冰箱怎么办
- 古人晚上做什么
"));
        self.sync_annotations();
        self.annotate.annotations.clear();
        self.persist_annotations();
        window.focus(&self.focus, cx);
        cx.notify();
    }

    pub(crate) fn annotation_smoke_point(&self, index: usize, offset: usize) -> Option<gpui::Point<gpui::Pixels>> {
        let block = self.doc.blocks.get(index)?;
        let layout = self.layouts.get(&block.id)?;
        Some(layout.point(offset) + gpui::point(gpui::px(0.), layout.line_height * 0.5))
    }

    pub(crate) fn annotation_smoke_targets(&self) -> Vec<(usize, usize, usize, String)> {
        self.selected_annotation_targets().into_iter().map(|t| (t.block_index, t.range.start, t.range.end, t.quote)).collect()
    }

    pub(crate) fn annotation_smoke_notes(&self) -> Vec<(u64, usize, usize, usize, String, String, Vec<String>)> {
        self.annotate.annotations.iter().map(|n| (n.id, n.block_index, n.start, n.end, n.quote.clone(), n.body.clone(), n.reactions.clone())).collect()
    }

    pub(crate) fn annotation_smoke_pending(&self) -> usize {
        self.annotate.composer.as_ref().map(|c| c.targets.len())
            .or_else(|| self.annotate.reactions.as_ref().map(|p| p.targets.len())).unwrap_or(0)
    }
}
