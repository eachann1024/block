//! Image blocks share the editor's source parser, remote cache and bounded image decoder.
use std::sync::{Arc, LazyLock};

use gpui::SvgRenderer;
use tiny_skia::{FilterQuality, Pixmap, PixmapPaint, Transform};

use super::{Cancellation, ExportError, SourceContext, layout::SCALE};
use crate::editor::{image_remote, image_source::{self, Align, ImageSpec}};

static RENDERER: LazyLock<SvgRenderer> = LazyLock::new(|| SvgRenderer::new(Arc::new(())));

pub(super) struct Picture {
    pixels: Pixmap,
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
}

pub(super) fn load(
    spec: &ImageSpec,
    source: &SourceContext,
    x: f32,
    y: f32,
    available: f32,
    cancel: &Cancellation,
) -> Result<Picture, ExportError> {
    cancel.check()?;
    let path = image_source::resolve_path(spec, source.note.as_deref());
    let path = match path {
        Some(path) => path,
        None if image_source::is_remote(&spec.src) => image_remote::download_sync(&spec.src).map_err(ExportError::Media)?,
        None => return Err(ExportError::Media(crate::theme::tr("无法解析图片位置", "Could not resolve image location").into())),
    };
    cancel.check()?;
    let decoded = crate::ui::file_preview::load::load_path(&RENDERER, &path).map_err(ExportError::Media)?;
    let size = decoded.image.size(0);
    let (w, h) = (size.width.0 as u32, size.height.0 as u32);
    let mut pixels = Pixmap::new(w, h).ok_or(ExportError::Rendering(crate::theme::tr("像素缓冲分配失败", "Failed to allocate pixel buffer")))?;
    let bytes = decoded.image.as_bytes(0).ok_or(ExportError::Rendering(crate::theme::tr("图片没有可用帧", "Image has no usable frame")))?;
    // GPUI frames are straight BGRA; tiny-skia composites premultiplied RGBA.
    for (row, (input, output)) in bytes.chunks_exact(w as usize * 4).zip(pixels.data_mut().chunks_exact_mut(w as usize * 4)).enumerate() {
        if row % 64 == 0 { cancel.check()?; }
        for (bgra, rgba) in input.chunks_exact(4).zip(output.chunks_exact_mut(4)) {
            let alpha = bgra[3] as u32;
            for (to, from) in [2, 1, 0].into_iter().enumerate() {
                rgba[to] = ((bgra[from] as u32 * alpha + 127) / 255) as u8;
            }
            rgba[3] = bgra[3];
        }
    }
    // Image sizing follows the note's content width, including enlargements beyond natural size.
    let base = source.width.map_or(available, |w| w as f32).max(1.);
    let width = spec.width.map_or(base, |w| (w as f32).clamp(base * 0.1, base)).min(available);
    let height = width * decoded.height / decoded.width;
    let offset = match spec.align { Align::Left => 0., Align::Center => (available - width) / 2., Align::Right => available - width };
    Ok(Picture { pixels, x: x + offset, y, width, height })
}

pub(super) fn paint(image: &mut Pixmap, picture: &Picture) {
    let transform = Transform::from_row(
        picture.width * SCALE / picture.pixels.width() as f32, 0., 0.,
        picture.height * SCALE / picture.pixels.height() as f32,
        picture.x * SCALE, picture.y * SCALE,
    );
    let paint = PixmapPaint { quality: FilterQuality::Bilinear, ..Default::default() };
    image.draw_pixmap(0, 0, picture.pixels.as_ref(), &paint, transform, None);
}
