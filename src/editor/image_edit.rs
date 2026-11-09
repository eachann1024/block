//! Editing an existing image block: alignment, width, alt text, crop, copy, delete and reveal.
use std::{process::Command, time::{SystemTime, UNIX_EPOCH}};
use gpui::{Context, Entity, Window, prelude::*};
use gpui_component::input::{InputEvent, InputState};
use crate::theme::tr;
use super::{image_source::{self, ImageSpec}, input::transactions::ListChange,
    model::{Block, Kind}, view::{Editor, selection::Position}};

#[derive(Clone)]
pub(super) enum ImagePop {
    More,
    AltText(Entity<InputState>),
    Size,
    Crop,
}

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
        let editor_id = cx.entity_id();
        super::image_view::rekey(cx, editor_id, id, new_id);
        if let Some((mid, _)) = &mut self.image_menu { if *mid == id { *mid = new_id; } }
        let at = Position { block: new_id, offset: 0 };
        self.caret.anchor = at; self.caret.head = at;
        self.record_changes(before, vec![ListChange { start: index, before: 1, after: 1 }], true, cx);
        self.layouts.clear(); cx.notify();
    }
    pub(super) fn delete_image(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        self.image_menu = None;
        self.commit_composition(cx);
        let Some(index) = self.doc.index_of(id) else { return };
        let before = self.snapshot();
        let mut changes = Vec::new();
        let mut caret = None;
        self.delete_single_block(id, index, &mut changes, &mut caret);
        if let Some(at) = caret { self.caret.anchor = at; self.caret.head = at; }
        self.record_changes(before, changes, true, cx);
        self.layouts.clear();
        window.focus(&self.focus, cx);
        cx.notify();
    }
    pub(super) fn open_image_alt(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        let alt = self.doc.get(id).and_then(image_source::spec_of).map(|s| s.alt).unwrap_or_default();
        let input = cx.new(|cx| InputState::new(window, cx)
            .placeholder(tr("输入替代文本…", "Enter alt text…"))
            .default_value(alt));
        cx.subscribe_in(&input, window, move |this, _, ev: &InputEvent, window, cx| {
            if matches!(ev, InputEvent::PressEnter { secondary: false, .. }) {
                this.save_image_alt(id, window, cx);
            }
        }).detach();
        let focus_in = input.clone();
        self.image_menu = Some((id, ImagePop::AltText(input)));
        cx.defer_in(window, move |_, window, cx| focus_in.update(cx, |i, cx| i.focus(window, cx)));
        cx.notify();
    }
    pub(super) fn save_image_alt(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        let Some((_, ImagePop::AltText(input))) = self.image_menu.take() else { return };
        let val = input.read(cx).value().to_string();
        self.edit_image(id, |s| s.alt = val, cx);
        window.focus(&self.focus, cx);
    }
    pub(super) fn crop_image(&mut self, id: u64, ratio: Option<(u32, u32)>, cx: &mut Context<Self>) {
        self.image_menu = None;
        let Some(path) = self.image_file(id).filter(|p| p.exists()) else {
            self.status = tr("找不到图片文件", "Image file not found").into(); cx.notify(); return;
        };
        let Some(spec) = self.doc.get(id).and_then(image_source::spec_of) else { return };
        let out = Command::new("/usr/bin/sips").args(["-g", "pixelWidth", "-g", "pixelHeight"]).arg(&path).output();
        let Ok(out) = out else { return };
        let stdout = String::from_utf8_lossy(&out.stdout);
        let parse_dim = |key: &str| stdout.lines().find(|l| l.contains(key))
            .and_then(|l| l.split(':').nth(1)).and_then(|v| v.trim().parse::<u32>().ok());
        let (Some(w), Some(h)) = (parse_dim("pixelWidth"), parse_dim("pixelHeight")) else { return };
        if w < 8 || h < 8 { return; }
        let (cw, ch) = match ratio {
            Some((rw, rh)) => if w * rh > h * rw { ((h * rw / rh).max(1), h) } else { (w, (w * rh / rw).max(1)) },
            None => (((w as f32 * 0.85).round() as u32).max(1), ((h as f32 * 0.85).round() as u32).max(1)),
        };
        let stem = path.file_stem().and_then(|s| s.to_str()).unwrap_or("img");
        let ext = path.extension().and_then(|e| e.to_str()).unwrap_or("png");
        let stamp = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_secs() % 100_000).unwrap_or(1);
        let new_name = format!("{stem}-crop-{stamp}.{ext}");
        let (dest, new_src) = if image_source::is_remote(&spec.src) {
            if let Some(note_dir) = self.path.as_deref().and_then(std::path::Path::parent) {
                let folder = crate::theme::assets_folder();
                let assets = note_dir.join(&folder);
                let _ = std::fs::create_dir_all(&assets);
                (assets.join(&new_name), format!("./{folder}/{new_name}"))
            } else {
                let dest = path.with_file_name(&new_name);
                let src = dest.to_string_lossy().into_owned();
                (dest, src)
            }
        } else {
            let dest = path.with_file_name(&new_name);
            let src = match spec.src.rfind('/') {
                Some(pos) => format!("{}/{new_name}", &spec.src[..pos]),
                None => new_name,
            };
            (dest, src)
        };
        let ok = Command::new("/usr/bin/sips").args(["-c", &ch.to_string(), &cw.to_string()])
            .arg(&path).arg("--out").arg(&dest).status().is_ok_and(|s| s.success());
        if !ok {
            self.status = tr("无法裁剪此图片格式", "Unable to crop this image format").into();
            cx.notify(); return;
        }
        self.edit_image(id, |s| { s.src = new_src; s.width = None; }, cx);
        self.status = tr("已裁剪图片", "Image cropped").into();
        cx.notify();
    }
    fn image_file(&self, id: u64) -> Option<std::path::PathBuf> {
        image_source::image_path(self.doc.get(id)?, self.path.as_deref())
    }
    pub(super) fn copy_image(&mut self, id: u64, cx: &mut Context<Self>) -> bool {
        let Some(block) = self.doc.get(id).cloned() else { return false };
        self.copy_image_blocks(&[block], cx)
    }
    pub(super) fn copy_image_blocks(&mut self, blocks: &[Block], cx: &mut Context<Self>) -> bool {
        if blocks.is_empty() || !blocks.iter().all(|b| image_source::spec_of(b).is_some()) {
            return false;
        }
        let note = self.path.as_deref();
        let paths: Vec<std::path::PathBuf> = blocks.iter()
            .filter_map(|b| image_source::image_path(b, note))
            .filter(|p| p.exists())
            .collect();
        if paths.is_empty() {
            self.status = tr("找不到图片文件", "Image file not found").into();
            cx.notify();
            return true;
        }
        if super::clipboard_files::copy_files(&paths, cx) {
            self.status = tr("已复制图片", "Image copied").into();
        } else {
            self.status = tr("无法复制图片文件", "Failed to copy image file").into();
        }
        cx.notify();
        true
    }
    pub(super) fn copy_selected_images(&mut self, cx: &mut Context<Self>) -> bool {
        if self.caret.is_empty() { return false; }
        let (_, b, first, last) = self.ordered();
        let mut images = Vec::new();
        for i in first..=last {
            let block = &self.doc.blocks[i];
            if image_source::spec_of(block).is_some() {
                if !(i == last && first < last && b.offset == 0 && !images.is_empty()) {
                    images.push(block.clone());
                }
            } else if !self.selected_range(block.id).unwrap_or(0..0).is_empty()
                || block.meta.table.is_some()
                || super::block_menu::is_divider(block)
            {
                return false;
            }
        }
        self.copy_image_blocks(&images, cx)
    }
    pub(super) fn copy_current_file(&mut self, path: &std::path::Path, cx: &mut Context<Self>) {
        if super::clipboard_files::copy_files(&[path.to_path_buf()], cx) {
            self.status = if super::clipboard_files::is_image(path) {
                tr("已复制图片", "Image copied").into()
            } else {
                tr("已复制文件", "File copied").into()
            };
            cx.notify();
        }
    }
    #[allow(dead_code)]
    pub(super) fn reveal_image(&mut self, id: u64, cx: &mut Context<Self>) {
        match self.image_file(id).filter(|p| p.exists()) {
            Some(path) => crate::files::ops::reveal_in_finder(&path),
            None => { self.status = tr("找不到图片文件", "Image file not found").into(); cx.notify(); }
        }
    }
}
