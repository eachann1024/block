//! Read-only probes for the `--smoke` run; they expose state without widening field visibility.
use super::{markdown, model::Kind, view::Editor};

impl Editor {
    pub(crate) fn prepare_outline_smoke(&mut self, window: &mut gpui::Window, cx: &mut gpui::Context<Self>) {
        assert!(crate::ui::smoke_overlay::is_smoke_active());
        self.reset_document(markdown::parse("# 概览总纲\n\n第一段说明\n\n## 核心架构\n\n架构正文\n\n### 渲染管线\n\n管线细节\n\n## 验收标准\n\n验收说明\n"), cx);
        self.annotate.annotations.clear(); self.persist_annotations();
        let id = self.doc.blocks.iter().find(|block| block.text == "核心架构").unwrap().id;
        self.focus_block(id, window, cx);
        self.outline.force_narrow = false; self.outline.hovered = false; cx.notify();
    }

    pub(crate) fn set_outline_force_narrow_smoke(&mut self, narrow: bool, cx: &mut gpui::Context<Self>) {
        assert!(crate::ui::smoke_overlay::is_smoke_active());
        self.outline.force_narrow = narrow; cx.notify();
    }

    pub(crate) fn outline_smoke_entries(&self) -> Vec<(u64, u8, String, bool, bool)> {
        super::outline::entries(self).into_iter().map(|entry|
            (entry.block_id, entry.level, entry.title, entry.active, entry.bar_lit)).collect()
    }

    pub(crate) fn caret_block_id(&self) -> u64 { self.caret.head.block }
}

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
"), cx);
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

impl Editor {
    pub(crate) fn menu_format_open(&self) -> bool { self.format.is_some() }
    pub(crate) fn menu_image_open(&self) -> bool { self.image_menu.is_some() }

    pub(crate) fn prepare_menu_image_smoke(&mut self, window: &mut gpui::Window, cx: &mut gpui::Context<Self>) -> u64 {
        assert!(crate::ui::smoke_overlay::is_smoke_active());
        let path = self.current_path().unwrap().parent().unwrap().join("menu-fixture.svg");
        std::fs::write(&path, "<svg xmlns='http://www.w3.org/2000/svg' width='400' height='200'><rect width='400' height='200' fill='#547dd0'/></svg>").unwrap();
        self.reset_document(markdown::parse(&format!("# Menu fixture\n\n![fixture]({})\n", path.display())), cx);
        let id = self.doc.blocks.iter().find(|block| super::image_source::spec_of(block).is_some()).unwrap().id;
        self.focus_block(id, window, cx);
        cx.notify();
        id
    }
}

impl Editor {
    pub(crate) fn annotation_smoke_spans(&self) -> Vec<(usize, usize, usize, String)> {
        self.annotate.annotations.first().map(|note| super::annotate::resolve_annotation_spans(&self.doc, note)
            .into_iter().filter_map(|(id, range, _)| {
                let index = self.doc.index_of(id)?;
                Some((index, range.start, range.end, self.doc.blocks[index].text.get(range)?.to_owned()))
            }).collect()).unwrap_or_default()
    }

    pub(crate) fn annotation_smoke_hit_ids(&self) -> Vec<Option<u64>> {
        self.doc.blocks.iter().map(|b| self.annotation_at(super::view::selection::Position { block: b.id, offset: 3 })).collect()
    }

    pub(crate) fn reload_annotation_smoke(&mut self) {
        assert!(crate::ui::smoke_overlay::is_smoke_active());
        self.annotate.loaded_path = None;
        self.sync_annotations();
    }
}

impl Editor {
    pub(crate) fn prepare_menu_edge_smoke(&mut self, window: &mut gpui::Window, cx: &mut gpui::Context<Self>) {
        assert!(crate::ui::smoke_overlay::is_smoke_active());
        self.reset_document(markdown::parse(&"M".repeat(40)), cx);
        self.focus_block(self.doc.blocks[0].id, window, cx);
        cx.notify();
    }
}

impl Editor {
    pub(crate) fn prepare_annotation_result_smoke(&mut self, cx: &mut gpui::Context<Self>) {
        assert!(crate::ui::smoke_overlay::is_smoke_active());
        if let Some(note) = self.annotate.annotations.first_mut() { note.ai_rewritten = Some("Alpha\nBeta".into()); }
        cx.notify();
    }

    pub(crate) fn verify_chaoshan_paste_smoke(&mut self, window: &mut gpui::Window, cx: &mut gpui::Context<Self>) -> Result<(), String> {
        use crate::editor::model::MarkKind;
        assert!(crate::ui::smoke_overlay::is_smoke_active());
        let icloud_path = "/Users/eachann/Library/Mobile Documents/com~apple~CloudDocs/0Markdown/生活/潮汕之旅.md";
        let source = std::fs::read_to_string(icloud_path).unwrap_or_else(|_| {
            "# 潮汕五日行程攻略（11月11日至15日）\n\n## 一、行前准备清单\n\n- [ ] 身份证\n\n### 2. 住宿分布\n\n- **11/11：**汕头龙湖区。\n\n| 餐别 | 推荐店 |\n| --- | --- |\n| 午餐 | 杏花吴记 |\n".into()
        });
        let saved_clip = cx.read_from_clipboard();
        let check_doc = |ed: &Self, label: &str| -> Result<(), String> {
            let has_h1 = ed.doc.blocks.iter().any(|b| b.kind == Kind::H1 && b.text.contains("潮汕"));
            let has_h2 = ed.doc.blocks.iter().any(|b| b.kind == Kind::H2);
            let has_h3 = ed.doc.blocks.iter().any(|b| b.kind == Kind::H3);
            let has_todo = ed.doc.blocks.iter().any(|b| b.kind == Kind::Todo);
            let has_bullet = ed.doc.blocks.iter().any(|b| b.kind == Kind::Bullet);
            let tables = ed.doc.blocks.iter().filter(|b| b.meta.table.is_some()).count();
            let has_cjk_bold = ed.doc.blocks.iter().any(|b| {
                b.text.contains("11/11：") && b.spans.iter().any(|s| s.marks.contains(&MarkKind::Bold))
            });
            if !(has_h1 && has_h2 && has_h3 && has_todo && has_bullet && tables >= 1 && has_cjk_bold) {
                return Err(format!("{label} Markdown 识别不完整: h1={has_h1} h2={has_h2} h3={has_h3} todo={has_todo} bullet={has_bullet} tables={tables} cjk_bold={has_cjk_bold}"));
            }
            Ok(())
        };
        // 1. Editor mode raw paste of 潮汕之旅.md
        self.reset_document(markdown::parse(""), cx);
        self.paste_text(&source, false, false, None, cx);
        check_doc(self, "编辑器直接粘贴潮汕之旅.md")?;
        // 2. Single Cmd+A (SelectAll) -> Cmd+C (Copy) -> paste plain text & clipboard item into Editor and QuickNote (minimal=true)
        self.caret = super::view::selection::Caret::new(self.doc.blocks[0].id);
        self.handle(&super::view::EditorAction::SelectAll, window, cx);
        let (_, _, ai, bi) = self.ordered();
        if !(ai == 0 && bi + 1 == self.doc.blocks.len()) {
            return Err(format!("单次全选未覆盖全文: ai={ai} bi={bi} len={}", self.doc.blocks.len()));
        }
        self.handle(&super::view::EditorAction::Copy, window, cx);
        let item = cx.read_from_clipboard().ok_or("全选复制后剪贴板为空")?;
        let clip_plain = item.text().ok_or("全选复制后剪贴板缺少纯文本")?;
        let clip_md = super::paste_clipboard::block_markdown(&item, &clip_plain).ok_or("全选复制后剪贴板缺少块结构")?;
        if !clip_plain.contains("潮汕") || clip_plain.contains("# 潮汕") {
            return Err("全选复制后的纯文本应仅包含可见文字".into());
        }
        // 3. QuickNote (小窗 minimal=true) paste from clipboard plain text & full source
        let prev_minimal = self.minimal;
        self.minimal = true;
        self.reset_document(markdown::parse(""), cx);
        self.handle(&super::view::EditorAction::Paste, window, cx);
        check_doc(self, "小窗粘贴复制出的潮汕之旅.md")?;
        self.reset_document(markdown::parse(""), cx);
        self.paste_text(&source, false, false, Some(&clip_md), cx);
        check_doc(self, "小窗粘贴完整潮汕之旅.md")?;
        self.minimal = prev_minimal;
        // 4. Entity-escaped Markdown paste recovery
        self.reset_document(markdown::parse(""), cx);
        let escaped = "&#35; 潮汕之旅\n\n&#45; &#42;&#42;11&#47;11：&#42;&#42;汕头龙湖区。\n\n&#124; 餐别 &#124; 店名 &#124;\n&#124; &#45;&#45;&#45; &#124; &#45;&#45;&#45; &#124;\n&#124; 午餐 &#124; 杏花吴记 &#124;";
        self.paste_text(escaped, false, false, None, cx);
        let esc_ok = self.doc.blocks.iter().any(|b| b.kind == Kind::H1)
            && self.doc.blocks.iter().any(|b| b.kind == Kind::Bullet && !b.spans.is_empty())
            && self.doc.blocks.iter().any(|b| b.meta.table.is_some());
        if !esc_ok { return Err("实体转义 Markdown 粘贴未恢复结构".into()); }
        self.reset_document(markdown::parse("# Smoke\n\n```rust\nfn main() {}\n```\n"), cx);
        if let Some(prev) = saved_clip { cx.write_to_clipboard(prev); }
        Ok(())
    }
}
