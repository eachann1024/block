//! Getting images into a note: picker, drag-drop, pasted bitmaps and pasted files.
//! Files are stored like thinkly does: `<note dir>/assets/img_<ms>_<8 hex>.<ext>`, referenced as `./assets/...`.
use std::{collections::hash_map::RandomState, hash::{BuildHasher, Hasher}, path::{Path, PathBuf}};
use gpui::{Context, Pixels, Point, Window};
use super::{clipboard_files::is_image, image_source, input::transactions::ListChange,
    model::{Block, Kind}, view::{Editor, selection::Position}};

#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum Place { Replace, After, Before, Nested { after: bool, indent: usize } }

fn raw_block(text: &str) -> Block {
    let mut block = Block::new(0, Kind::Paragraph, text.to_owned()); block.meta.raw_source = Some(text.to_owned()); block
}
fn asset_name(ext: &str, assets: &Path) -> String {
    loop {
        let ms = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_millis()).unwrap_or(0);
        let random = RandomState::new().build_hasher().finish() as u32;
        let name = format!("img_{ms}_{random:08x}.{ext}");
        if !assets.join(&name).exists() { return name; }
    }
}

impl Editor {
    pub(super) fn pick_image(&mut self, id: u64, replace: bool, window: &mut Window, cx: &mut Context<Self>) {
        if self.note_dir().is_none() { self.status = crate::theme::tr("请先打开一篇笔记", "Open a note first").into(); cx.notify(); return; }
        let prompt = cx.prompt_for_paths(gpui::PathPromptOptions { files: true, directories: false, multiple: true, prompt: Some(crate::theme::tr("导入图片", "Import Image").into()) });
        let place = if replace { Place::Replace } else { Place::After };
        cx.spawn_in(window, async move |this, cx| {
            let Ok(Ok(Some(paths))) = prompt.await else { return };
            let _ = this.update_in(cx, |this, window, cx| { this.import_images(id, place, paths, window, cx); });
        }).detach();
    }
    pub(super) fn note_dir(&self) -> Option<PathBuf> { self.path.as_ref()?.parent().map(Path::to_path_buf) }
    fn assets_dir(&mut self, cx: &mut Context<Self>) -> Option<PathBuf> {
        let folder = crate::theme::assets_folder();
        let assets = self.note_dir()?.join(&folder);
        if let Err(error) = std::fs::create_dir_all(&assets) {
            self.status = format!("{}: {error}", crate::theme::tr("无法创建资源文件夹", "Failed to create assets folder"));
            cx.notify();
            return None;
        }
        Some(assets)
    }
    /// Copy images into `assets/` and insert one image block per file.
    pub(super) fn import_images(&mut self, id: u64, place: Place, files: Vec<PathBuf>, window: &mut Window, cx: &mut Context<Self>) {
        if self.note_dir().is_none() { self.status = crate::theme::tr("请先打开一篇笔记", "Open a note first").into(); cx.notify(); return; }
        let Some(assets) = self.assets_dir(cx) else { return };
        let mut blocks = Vec::new();
        for file in files.into_iter().filter(|f| is_image(f)) {
            let ext = file.extension().and_then(|e| e.to_str()).unwrap_or("png").to_lowercase();
            let stem = file.file_stem().and_then(|s| s.to_str()).unwrap_or("image").to_owned();
            let name = asset_name(&ext, &assets);
            if std::fs::copy(&file, assets.join(&name)).is_err() { continue; }
            blocks.push(raw_block(&Self::image_line(&stem, &name)));
        }
        self.insert_blocks(id, place, blocks, window, cx);
    }
    pub(super) fn import_image_bytes(&mut self, id: u64, bytes: Vec<u8>, ext: &str, window: &mut Window, cx: &mut Context<Self>) {
        let Some(assets) = self.assets_dir(cx) else { return };
        let name = asset_name(ext, &assets);
        if std::fs::write(assets.join(&name), bytes).is_err() { return; }
        self.insert_blocks(id, Place::After, vec![raw_block(&Self::image_line("pasted", &name))], window, cx);
    }
    fn image_line(alt: &str, name: &str) -> String {
        let folder = crate::theme::assets_folder();
        image_source::format(&image_source::ImageSpec { alt: alt.replace(['[', ']'], ""), src: format!("./{folder}/{name}"),
            title: None, width: None, align: image_source::Align::Left })
    }
    /// Block under `position` (list geometry, so static images count; nearest text block as fallback) and the side to insert on.
    pub(super) fn drop_target(&self, position: Point<Pixels>) -> Option<(u64, Place)> {
        let target = self.block_drop_target(position)?;
        Some((self.doc.blocks[target.index].id, Place::Nested { after: target.after, indent: target.indent }))
    }

    fn insert_blocks(&mut self, id: u64, place: Place, mut blocks: Vec<Block>, window: &mut Window, cx: &mut Context<Self>) {
        if blocks.is_empty() { self.status = crate::theme::tr("没有可导入的图片", "No images to import").into(); cx.notify(); return; }
        let index = self.doc.index_of(id).unwrap_or(self.doc.blocks.len().saturating_sub(1));
        let replace = place == Place::Replace && self.doc.index_of(id).is_some();
        let (start, remove) = match place { Place::Nested { after, .. } => (index + usize::from(after), 0), Place::Before => (index, 0), _ if replace => (index, 1), _ => (index + 1, 0) };
        if !matches!(place, Place::Before | Place::Nested { .. }) { blocks.push(Block::new(0, Kind::Paragraph, String::new())); }
        let indent = match place { Place::Nested { indent, .. } => indent, _ => self.doc.blocks[index].meta.indent };
        for block in &mut blocks { block.meta.indent = indent; }
        let before = self.snapshot(); let count = blocks.len();
        self.doc.splice_blocks(start..start + remove, blocks);
        let caret_index = if place == Place::Before { start + count } else { start + count - 1 };
        let at = Position { block: self.doc.blocks[caret_index.min(self.doc.blocks.len() - 1)].id, offset: 0 };
        self.caret.anchor = at; self.caret.head = at;
        self.record_changes(before, vec![ListChange { start, before: remove, after: count }], true, cx);
        self.layouts.clear();
        window.focus(&self.focus, cx); cx.notify();
    }
}
