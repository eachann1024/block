//! Disk and unsaved-SVG decode. Header limits run before GPUI rasterizes.

use std::{path::Path, sync::Arc};
use gpui::{DevicePixels, Image, ImageFormat, RenderImage, SvgRenderer};

use super::raster::{self, RasterKind};
use super::svg_scan::{self, MAX_SVG_EDGE};

/// Output cap for one SVG frame. Matches the scan's 4_000_000 logical-pixel budget.
const MAX_DEVICE_PIXELS: u64 = 4_000_000;

pub(crate) struct Decoded {
    pub image: Arc<RenderImage>,
    pub width: f32,
    pub height: f32,
}

pub(crate) fn load_path(renderer: &SvgRenderer, path: &Path) -> Result<Decoded, String> {
    if super::is_svg_path(path) {
        let limit = svg_scan::MAX_SVG_BYTES as u64;
        let bytes = raster::read_capped(path, limit)?;
        let text = String::from_utf8(bytes).map_err(|_| super::copy("SVG 不是 UTF-8", "SVG is not UTF-8"))?;
        return render_svg(renderer, &text);
    }
    decode_raster(renderer, raster::read_bounded(path)?)
}

/// Logical size comes from the scanner. `SvgSize::ScaleFactor` would double it (`SMOOTH_SVG_SCALE_FACTOR`).
pub(crate) fn render_svg(renderer: &SvgRenderer, source: &str) -> Result<Decoded, String> {
    let size = svg_scan::sanitize_svg(source)?;
    let edge = size.width.max(size.height);
    if !(edge.is_finite() && edge > 0.0) || edge > MAX_SVG_EDGE || size.width * size.height > MAX_DEVICE_PIXELS as f32 {
        return Err(super::copy("SVG 渲染尺寸超出限制", "SVG render size exceeds the limit"));
    }
    let parsed = renderer.parse_svg(source.as_bytes()).map_err(svg_err)?;
    let pixels = gpui::size(
        DevicePixels(size.width.round().clamp(1.0, MAX_SVG_EDGE) as i32),
        DevicePixels(size.height.round().clamp(1.0, MAX_SVG_EDGE) as i32),
    );
    let image = renderer.render_parsed(&parsed, gpui::SvgSize::ExactSize(pixels)).map_err(svg_err)?;
    let frame = image.size(0);
    let (width, height) = (frame.width.0.max(0) as u64, frame.height.0.max(0) as u64);
    if width == 0 || height == 0 || width > MAX_SVG_EDGE as u64 || height > MAX_SVG_EDGE as u64 || width.saturating_mul(height) > MAX_DEVICE_PIXELS {
        return Err(super::copy("SVG 渲染尺寸超出限制", "SVG render size exceeds the limit"));
    }
    Ok(Decoded { image, width: size.width, height: size.height })
}

fn svg_err(err: impl std::fmt::Display) -> String {
    let detail: String = err.to_string().chars().take(160).collect();
    if crate::theme::is_en() { format!("SVG render failed: {detail}") } else { format!("SVG 渲染失败：{detail}") }
}

fn decode_raster(renderer: &SvgRenderer, bytes: Vec<u8>) -> Result<Decoded, String> {
    let info = raster::probe_raster(&bytes)?;
    let format = match info.kind {
        RasterKind::Png => ImageFormat::Png,
        RasterKind::Jpeg => ImageFormat::Jpeg,
        RasterKind::Gif => ImageFormat::Gif,
        RasterKind::Webp => ImageFormat::Webp,
        RasterKind::Bmp => ImageFormat::Bmp,
    };
    let image = Image::from_bytes(format, bytes).to_image_data(renderer.clone()).map_err(|err| {
        let detail: String = err.to_string().chars().take(160).collect();
        if crate::theme::is_en() { format!("Could not decode the image: {detail}") } else { format!("无法解码图片：{detail}") }
    })?;
    let frame = image.size(0);
    let width = frame.width.0.max(0) as u32;
    let height = frame.height.0.max(0) as u32;
    raster::check_pixels(width, height, image.frame_count().max(1) as u32)?;
    Ok(Decoded { image, width: width as f32, height: height as f32 })
}
