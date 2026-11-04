//! Editing an existing image block: alignment, width, alt text, crop, copy, delete and reveal.
use std::{process::Command, time::{SystemTime, UNIX_EPOCH}};
use gpui::{ClipboardItem, Context, Entity, Window, prelude::*};
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
        super::image_view::rekey(cx, id, new_id);
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
        window.focus(&self.focus);
        cx.notify();
    }
    pub(super) fn open_image_alt(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        let alt = self.doc.get(id).and_then(image_source::spec_of).map(|s| s.alt).unwrap_or_default();
        let input = cx.new(|cx| InputState::new(window, cx)
            .placeholder(tr("输入替代文本…", "Enter alt text…"))
            .default_value(alt));
        cx.subscribe_in(&input, window, move |this, _, ev: &InputEvent, window, cx| {
            if matches!(ev, InputEvent::PressEnter { secondary: false }) {
                this.save_image_alt(id, window, cx);
            }
        }).detach();
        let focus_in = input.clone();
        self.image_menu = Some((id, ImagePop::AltText(input)));
        focus_in.update(cx, |i, cx| i.focus(window, cx));
        cx.notify();
    }
    pub(super) fn save_image_alt(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        let Some((_, ImagePop::AltText(input))) = self.image_menu.take() else { return };
        let val = input.read(cx).value().to_string();
        self.edit_image(id, |s| s.alt = val, cx);
        window.focus(&self.focus);
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
        let dest = path.with_file_name(&new_name);
        let ok = Command::new("/usr/bin/sips").args(["-c", &ch.to_string(), &cw.to_string()])
            .arg(&path).arg("--out").arg(&dest).status().is_ok_and(|s| s.success());
        if !ok {
            self.status = tr("无法裁剪此图片格式", "Unable to crop this image format").into();
            cx.notify(); return;
        }
        let new_src = match spec.src.rfind('/') {
            Some(pos) => format!("{}/{new_name}", &spec.src[..pos]),
            None => new_name,
        };
        self.edit_image(id, |s| { s.src = new_src; s.width = None; }, cx);
        self.status = tr("已裁剪图片", "Image cropped").into();
        cx.notify();
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
            _ => { self.status = tr("不支持复制这种图片格式", "Unsupported image format for copy").into(); cx.notify(); return; }
        };
        match std::fs::read(&path) {
            Ok(bytes) => {
                cx.write_to_clipboard(ClipboardItem::new_image(&gpui::Image::from_bytes(format, bytes)));
                self.status = tr("已复制图片", "Image copied").into();
            }
            Err(error) => self.status = format!("{}{error}", tr("无法读取图片：", "Failed to read image: ")),
        }
        cx.notify();
    }
    pub(super) fn reveal_image(&mut self, id: u64, cx: &mut Context<Self>) {
        match self.image_file(id).filter(|p| p.exists()) {
            Some(path) => if Command::new("open").arg("-R").arg(&path).spawn().is_err() { cx.reveal_path(&path) },
            None => { self.status = tr("找不到图片文件", "Image file not found").into(); cx.notify(); }
        }
    }
}
