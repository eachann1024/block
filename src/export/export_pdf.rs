//! PDF export: the rendered paper is embedded losslessly as a single-page image.
use super::layout::SCALE;
use super::{Cancellation, ExportError, RenderOptions, render_pixmap};
use crate::editor::model::Block;
use tiny_skia::Pixmap;

pub fn render_pdf(
    blocks: &[Block],
    options: RenderOptions,
    cancel: &Cancellation,
) -> Result<Vec<u8>, ExportError> {
    let (pixmap, _) = render_pixmap(blocks, options, cancel)?;
    encode_pixmap_pdf(&pixmap, cancel)
}

/// RFC 1950 zlib stream made of RFC 1951 stored blocks: lossless, dependency-free.
fn zlib_stored(data: &[u8], cancel: &Cancellation) -> Result<Vec<u8>, ExportError> {
    const MAX_BLOCK: usize = 65_535;
    let mut out = Vec::with_capacity(data.len() + data.len() / MAX_BLOCK * 5 + 16);
    out.extend_from_slice(&[0x78, 0x01]);
    if data.is_empty() {
        out.extend_from_slice(&[0x01, 0x00, 0x00, 0xff, 0xff]);
    }
    let mut chunks = data.chunks(MAX_BLOCK).peekable();
    while let Some(chunk) = chunks.next() {
        cancel.check()?;
        let len = chunk.len() as u16;
        out.push(u8::from(chunks.peek().is_none()));
        out.extend_from_slice(&len.to_le_bytes());
        out.extend_from_slice(&(!len).to_le_bytes());
        out.extend_from_slice(chunk);
    }
    out.extend_from_slice(&adler32(data).to_be_bytes());
    Ok(out)
}

fn adler32(data: &[u8]) -> u32 {
    const MOD: u32 = 65_521;
    let (mut a, mut b) = (1u32, 0u32);
    // 5552 is the largest run that cannot overflow u32 before reducing.
    for chunk in data.chunks(5552) {
        for &byte in chunk {
            a += byte as u32;
            b += a;
        }
        a %= MOD;
        b %= MOD;
    }
    (b << 16) | a
}

/// RGB bytes of the pixmap, composited over white (the pixmap is premultiplied RGBA).
fn rgb_bytes(pixmap: &Pixmap, cancel: &Cancellation) -> Result<Vec<u8>, ExportError> {
    let mut rgb = Vec::with_capacity(pixmap.pixels().len() * 3);
    for (index, pixel) in pixmap.pixels().iter().enumerate() {
        if index % (1 << 20) == 0 {
            cancel.check()?;
        }
        let gap = 255 - pixel.alpha();
        rgb.extend_from_slice(&[
            pixel.red().saturating_add(gap),
            pixel.green().saturating_add(gap),
            pixel.blue().saturating_add(gap),
        ]);
    }
    Ok(rgb)
}

pub fn encode_pixmap_pdf(pixmap: &Pixmap, cancel: &Cancellation) -> Result<Vec<u8>, ExportError> {
    let (width, height) = (pixmap.width(), pixmap.height());
    let pt_w = (width as f32 / SCALE).round().max(1.0) as u32;
    let pt_h = (height as f32 / SCALE).round().max(1.0) as u32;
    let zlib = zlib_stored(&rgb_bytes(pixmap, cancel)?, cancel)?;
    let content = format!("q {pt_w} 0 0 {pt_h} 0 0 cm /Im0 Do Q");

    let mut out: Vec<u8> = b"%PDF-1.4\n%\xe2\xe3\xcf\xd3\n".to_vec();
    let mut offsets = Vec::with_capacity(5);
    let mut object = |out: &mut Vec<u8>, number: u32, body: &[u8]| {
        offsets.push(out.len());
        out.extend_from_slice(format!("{number} 0 obj\n").as_bytes());
        out.extend_from_slice(body);
        out.extend_from_slice(b"\nendobj\n");
    };
    object(&mut out, 1, b"<< /Type /Catalog /Pages 2 0 R >>");
    object(&mut out, 2, b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>");
    let page = format!(
        "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 {pt_w} {pt_h}] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>"
    );
    object(&mut out, 3, page.as_bytes());
    let mut image = format!(
        "<< /Type /XObject /Subtype /Image /Width {width} /Height {height} /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length {} >>\nstream\n",
        zlib.len()
    )
    .into_bytes();
    image.extend_from_slice(&zlib);
    image.extend_from_slice(b"\nendstream");
    object(&mut out, 4, &image);
    let stream = format!("<< /Length {} >>\nstream\n{content}\nendstream", content.len());
    object(&mut out, 5, stream.as_bytes());

    let xref = out.len();
    out.extend_from_slice(b"xref\n0 6\n0000000000 65535 f \n");
    for offset in &offsets {
        out.extend_from_slice(format!("{offset:010} 00000 n \n").as_bytes());
    }
    out.extend_from_slice(format!("trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n").as_bytes());
    cancel.check()?;
    Ok(out)
}
