//! Header probes for PNG, JPEG, GIF, WebP and BMP. Dimensions and frame counts
//! are known before any pixel buffer is allocated.

use std::{fs::File, io::Read, path::Path};

pub const MAX_FILE_BYTES: u64 = 64 * 1024 * 1024;
pub const MAX_PIXELS: u64 = 40_000_000;
/// GPUI SVG pixmap cap and the usual WebGPU 2D texture edge (`gpui-pre` 0.3.11 `MAX_SIZE`).
pub const MAX_EDGE: u32 = 8192;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum RasterKind {
    Png,
    Jpeg,
    Gif,
    Webp,
    Bmp,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RasterInfo {
    pub kind: RasterKind,
    pub width: u32,
    pub height: u32,
    pub frames: u32,
}

pub fn read_bounded(path: &Path) -> Result<Vec<u8>, String> {
    read_capped(path, MAX_FILE_BYTES)
}

/// Metadata is only a hint. The read itself stops at `limit + 1` bytes.
pub fn read_capped(path: &Path, limit: u64) -> Result<Vec<u8>, String> {
    let meta = std::fs::metadata(path).map_err(|_| super::copy("无法读取文件", "Could not read the file"))?;
    if !meta.is_file() {
        return Err(super::copy("无法读取文件", "Could not read the file"));
    }
    if meta.len() > limit {
        return Err(over_limit(limit));
    }
    let file = File::open(path).map_err(|_| super::copy("无法读取文件", "Could not read the file"))?;
    let mut buf = Vec::new();
    let reserve = meta.len().min(limit.saturating_add(1)) as usize;
    buf.try_reserve(reserve).map_err(|_| super::copy("内存不足，已取消预览", "Not enough memory to preview"))?;
    file.take(limit.saturating_add(1)).read_to_end(&mut buf).map_err(|_| super::copy("无法读取文件", "Could not read the file"))?;
    if buf.is_empty() {
        return Err(super::copy("文件是空的", "The file is empty"));
    }
    if buf.len() as u64 > limit {
        return Err(over_limit(limit));
    }
    Ok(buf)
}

fn over_limit(limit: u64) -> String {
    if limit >= MAX_FILE_BYTES {
        super::copy("文件超过 64MB，已取消预览", "File is over 64MB, so preview was skipped")
    } else {
        super::copy("SVG 超过大小或编码限制", "SVG exceeds the size or encoding limit")
    }
}

pub fn probe_raster(bytes: &[u8]) -> Result<RasterInfo, String> {
    let info = if bytes.len() >= 24 && bytes.starts_with(b"\x89PNG\r\n\x1a\n") && &bytes[12..16] == b"IHDR" {
        RasterInfo { kind: RasterKind::Png, width: u32_be(bytes, 16), height: u32_be(bytes, 20), frames: 1 }
    } else if bytes.len() >= 4 && bytes[0] == 0xFF && bytes[1] == 0xD8 {
        let (width, height) = jpeg_size(bytes)?;
        RasterInfo { kind: RasterKind::Jpeg, width, height, frames: 1 }
    } else if bytes.len() >= 10 && (bytes.starts_with(b"GIF87a") || bytes.starts_with(b"GIF89a")) {
        let (width, height, frames) = gif_size(bytes)?;
        RasterInfo { kind: RasterKind::Gif, width, height, frames }
    } else if bytes.len() >= 16 && bytes.starts_with(b"RIFF") && &bytes[8..12] == b"WEBP" {
        let (width, height, frames) = webp_size(bytes)?;
        RasterInfo { kind: RasterKind::Webp, width, height, frames }
    } else if bytes.len() >= 26 && bytes.starts_with(b"BM") {
        let (width, height) = bmp_size(bytes)?;
        RasterInfo { kind: RasterKind::Bmp, width, height, frames: 1 }
    } else {
        return Err(super::copy("无法识别的图片数据", "Unrecognized image data"));
    };
    check_pixels(info.width, info.height, info.frames)?;
    Ok(info)
}

pub fn check_pixels(width: u32, height: u32, frames: u32) -> Result<(), String> {
    let pixels = (width as u64).saturating_mul(height as u64).saturating_mul(frames.max(1) as u64);
    if width == 0 || height == 0 || frames == 0 || width > MAX_EDGE || height > MAX_EDGE || pixels > MAX_PIXELS {
        Err(super::copy("图片像素过多，已取消预览", "Image has too many pixels, so preview was skipped"))
    } else {
        Ok(())
    }
}

fn u32_be(bytes: &[u8], at: usize) -> u32 {
    u32::from_be_bytes([bytes[at], bytes[at + 1], bytes[at + 2], bytes[at + 3]])
}

fn jpeg_size(bytes: &[u8]) -> Result<(u32, u32), String> {
    let mut i = 2;
    let mut orientation = 1u16;
    while i + 3 < bytes.len() {
        if bytes[i] != 0xFF {
            return Err(super::copy("无法读取图片尺寸", "Could not read the image size"));
        }
        while i < bytes.len() && bytes[i] == 0xFF { i += 1; }
        if i >= bytes.len() { break; }
        let marker = bytes[i];
        i += 1;
        if marker == 0xD9 || marker == 0xDA { break; }
        if marker == 0x01 || (0xD0..=0xD8).contains(&marker) { continue; }
        if i + 2 > bytes.len() { break; }
        let len = u16::from_be_bytes([bytes[i], bytes[i + 1]]) as usize;
        if len < 2 || i + len > bytes.len() {
            return Err(super::copy("无法读取图片尺寸", "Could not read the image size"));
        }
        if marker == 0xE1 {
            orientation = exif_orientation(&bytes[i + 2..i + len]).unwrap_or(orientation);
        }
        if matches!(marker, 0xC0..=0xC3 | 0xC5..=0xC7 | 0xC9..=0xCB | 0xCD..=0xCF) && len >= 7 {
            let height = u16::from_be_bytes([bytes[i + 3], bytes[i + 4]]) as u32;
            let width = u16::from_be_bytes([bytes[i + 5], bytes[i + 6]]) as u32;
            return if matches!(orientation, 5..=8) { Ok((height, width)) } else { Ok((width, height)) };
        }
        i += len;
    }
    Err(super::copy("无法读取图片尺寸", "Could not read the image size"))
}

fn exif_orientation(data: &[u8]) -> Option<u16> {
    if data.len() < 16 || &data[..6] != b"Exif\0\0" { return None; }
    let tiff = &data[6..];
    let be = match &tiff[..2] {
        b"MM" => true,
        b"II" => false,
        _ => return None,
    };
    let u16_at = |off: usize| -> Option<u16> {
        let b = tiff.get(off..off + 2)?;
        Some(if be { u16::from_be_bytes([b[0], b[1]]) } else { u16::from_le_bytes([b[0], b[1]]) })
    };
    let u32_at = |off: usize| -> Option<u32> {
        let b = tiff.get(off..off + 4)?;
        Some(if be { u32::from_be_bytes([b[0], b[1], b[2], b[3]]) } else { u32::from_le_bytes([b[0], b[1], b[2], b[3]]) })
    };
    if u16_at(2)? != 42 { return None; }
    let ifd = u32_at(4)? as usize;
    let count = u16_at(ifd)? as usize;
    for entry in 0..count {
        let at = ifd + 2 + entry * 12;
        if u16_at(at)? == 0x0112 {
            return Some(if u16_at(at + 2)? == 3 { u16_at(at + 8)? } else { u32_at(at + 8)? as u16 });
        }
    }
    None
}

fn gif_size(bytes: &[u8]) -> Result<(u32, u32, u32), String> {
    if bytes.len() < 13 {
        return Err(super::copy("无法读取图片尺寸", "Could not read the image size"));
    }
    let width = u16::from_le_bytes([bytes[6], bytes[7]]) as u32;
    let height = u16::from_le_bytes([bytes[8], bytes[9]]) as u32;
    let mut i = 13;
    if bytes[10] & 0x80 != 0 {
        i += 3 * (1usize << ((bytes[10] & 0x07) + 1));
    }
    let mut frames = 0u32;
    while i < bytes.len() {
        match bytes[i] {
            0x3B => {
                if frames == 0 {
                    return Err(super::copy("无法读取图片尺寸", "Could not read the image size"));
                }
                return Ok((width, height, frames));
            }
            0x21 => {
                i += 2;
                i = gif_subblocks(bytes, i)?;
            }
            0x2C => {
                if i + 10 > bytes.len() {
                    return Err(super::copy("无法读取图片尺寸", "Could not read the image size"));
                }
                frames = frames.saturating_add(1);
                let packed = bytes[i + 9];
                i += 10;
                if packed & 0x80 != 0 {
                    i += 3 * (1usize << ((packed & 0x07) + 1));
                }
                if i >= bytes.len() {
                    return Err(super::copy("无法读取图片尺寸", "Could not read the image size"));
                }
                i += 1;
                i = gif_subblocks(bytes, i)?;
            }
            _ => return Err(super::copy("无法读取图片尺寸", "Could not read the image size")),
        }
    }
    Err(super::copy("无法读取图片尺寸", "Could not read the image size"))
}

fn gif_subblocks(bytes: &[u8], mut i: usize) -> Result<usize, String> {
    while i < bytes.len() {
        let size = bytes[i] as usize;
        i += 1;
        if size == 0 { return Ok(i); }
        if i + size > bytes.len() {
            return Err(super::copy("无法读取图片尺寸", "Could not read the image size"));
        }
        i += size;
    }
    Err(super::copy("无法读取图片尺寸", "Could not read the image size"))
}

fn webp_size(bytes: &[u8]) -> Result<(u32, u32, u32), String> {
    let mut i = 12;
    let mut size = None;
    let mut frames = 0u32;
    let mut animated = false;
    while i + 8 <= bytes.len() {
        let tag = &bytes[i..i + 4];
        let chunk = u32::from_le_bytes([bytes[i + 4], bytes[i + 5], bytes[i + 6], bytes[i + 7]]) as usize;
        let data = i + 8;
        if data + chunk > bytes.len() {
            return Err(super::copy("无法读取图片尺寸", "Could not read the image size"));
        }
        match tag {
            b"VP8X" if chunk >= 10 => {
                animated = bytes[data] & 0x02 != 0;
                let width = 1 + u32::from_le_bytes([bytes[data + 4], bytes[data + 5], bytes[data + 6], 0]);
                let height = 1 + u32::from_le_bytes([bytes[data + 7], bytes[data + 8], bytes[data + 9], 0]);
                size = Some((width, height));
            }
            b"VP8 " if size.is_none() && chunk >= 10 && bytes.get(data + 3..data + 6) == Some(&[0x9D, 0x01, 0x2A]) => {
                let width = u16::from_le_bytes([bytes[data + 6], bytes[data + 7]]) as u32 & 0x3FFF;
                let height = u16::from_le_bytes([bytes[data + 8], bytes[data + 9]]) as u32 & 0x3FFF;
                size = Some((width, height));
            }
            b"VP8L" if size.is_none() && chunk >= 5 && bytes[data] == 0x2F => {
                let bits = u32::from_le_bytes([bytes[data + 1], bytes[data + 2], bytes[data + 3], bytes[data + 4]]);
                size = Some(((bits & 0x3FFF) + 1, ((bits >> 14) & 0x3FFF) + 1));
            }
            b"ANMF" => frames = frames.saturating_add(1),
            _ => {}
        }
        i = data + chunk + (chunk & 1);
    }
    let (width, height) = size.ok_or_else(|| super::copy("无法读取图片尺寸", "Could not read the image size"))?;
    if animated && frames == 0 {
        return Err(super::copy("无法读取图片尺寸", "Could not read the image size"));
    }
    Ok((width, height, if animated { frames } else { 1 }))
}

fn bmp_size(bytes: &[u8]) -> Result<(u32, u32), String> {
    let header = u32::from_le_bytes([bytes[14], bytes[15], bytes[16], bytes[17]]);
    let (width, height) = if header == 12 && bytes.len() >= 22 {
        (u16::from_le_bytes([bytes[18], bytes[19]]) as u32, u16::from_le_bytes([bytes[20], bytes[21]]) as u32)
    } else if bytes.len() >= 26 {
        let width = i32::from_le_bytes([bytes[18], bytes[19], bytes[20], bytes[21]]).unsigned_abs();
        let height = i32::from_le_bytes([bytes[22], bytes[23], bytes[24], bytes[25]]).unsigned_abs();
        (width, height)
    } else {
        return Err(super::copy("无法读取图片尺寸", "Could not read the image size"));
    };
    Ok((width, height))
}
