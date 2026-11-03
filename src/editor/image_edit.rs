//! Editing an existing image block: alignment, width, copy and reveal.
use gpui::{ClipboardItem, Context};
use super::{image_source::{self, ImageSpec}, input::transactions::ListChange,
    model::{Block, Kind}, view::{Editor, selection::Position}};

impl Editor {
    /// Rewrite the image line of block `id` as one undoable edit.
    pub(super) fn edit_image(&mut self, id: u64, edit: impl FnOnce(&mut ImageSpec), cx: &mut Context<Self>) {
        let Some(index) = self.doc.index_of(id) else { return };
        let Some(mut spec) = self.doc.get(id).and_then(image_source::spec_of) else { return };
        let old = spec.clone(); edit(&mut spec);
        if spec == old { cx.notify(); return; }
        self.commit_composition(cx);
        let line = image_source::format(&spec);
        let mut block = Block::new(0, Kind::Paragraph, line.clone()); block.meta.raw_source = Some(line);
        let before = self.snapshot();
        self.doc.splice_blocks(index..index + 1, vec![block]);
        let new_id = self.doc.blocks[index].id;
        super::image_view::rekey(cx, id, new_id);
        let at = Position { block: new_id, offset: 0 };
        self.caret.anchor = at; self.caret.head = at;
        self.record_changes(before, vec![ListChange { start: index, before: 1, after: 1 }], true, cx);
        self.layouts.clear(); cx.notify();
    }
    fn image_file(&self, id: u64) -> Option<std::path::PathBuf> {
        image_source::image_path(self.doc.get(id)?, self.path.as_deref())
    }
    pub(super) fn copy_image(&mut self, id: u64, cx: &mut Context<Self>) {
        let Some(path) = self.image_file(id) else { return };
        let ext = path.extension().and_then(|e| e.to_str()).unwrap_or("").to_ascii_lowercase();
        let format = match ext.as_str() {
            "png" => gpui::ImageFormat::Png, "jpg" | "jpeg" => gpui::ImageFormat::Jpeg, "gif" => gpui::ImageFormat::Gif,
            "webp" => gpui::ImageFormat::Webp, "bmp" => gpui::ImageFormat::Bmp, "svg" => gpui::ImageFormat::Svg,
            _ => { self.status = "不支持复制这种图片格式".into(); cx.notify(); return; }
        };
        match std::fs::read(&path) {
            Ok(bytes) => {
                cx.write_to_clipboard(ClipboardItem::new_image(&gpui::Image::from_bytes(format, bytes)));
                self.status = "已复制图片".into();
            }
            Err(error) => self.status = format!("无法读取图片：{error}"),
        }
        cx.notify();
    }
    pub(super) fn reveal_image(&mut self, id: u64, cx: &mut Context<Self>) {
        match self.image_file(id).filter(|p| p.exists()) {
            // `open -R` selects the file itself in Finder; the platform reveal only opened its folder.
            Some(path) => if std::process::Command::new("open").arg("-R").arg(&path).spawn().is_err() { cx.reveal_path(&path) },
            None => { self.status = "找不到图片文件".into(); cx.notify(); }
        }
    }
}
