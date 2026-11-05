//! WebAssembly CJK font metrics & 2D-canvas glyph rasterizer for `cosmic-text` image export.
use std::cell::RefCell;
use cosmic_text::{Color, LayoutGlyph};
use wasm_bindgen::JsCast;
use web_sys::{CanvasRenderingContext2d, HtmlCanvasElement};

const CJK_RANGES: &[(u32, u32)] = &[
    (0x2014, 0x2026),
    (0x2190, 0x27BF),
    (0x2E80, 0x33FF),
    (0x4E00, 0x9FFF),
    (0xF900, 0xFAFF),
    (0xFE30, 0xFFEF),
];

/// Builds a minimal valid TrueType font whose `cmap` covers CJK & fullwidth punctuation
/// with 1.0em (`1000` units for `upem = 1000`) advance width and empty `glyf` outlines.
/// `cosmic-text` shapes and wraps CJK runs accurately, while `paint_glyph` rasterizes
/// the actual glyph pixels via the browser's native 2D canvas text engine.
pub(crate) fn metric_font(family: &str) -> Vec<u8> {
    let mut cmap4 = Vec::new();
    let seg_count = (CJK_RANGES.len() + 1) as u16;
    let search_range = (1u16 << (15 - seg_count.leading_zeros())) * 2;
    let entry_selector = 15 - (search_range / 2).leading_zeros() as u16;
    let range_shift = seg_count * 2 - search_range;
    push_u16(&mut cmap4, 4);
    push_u16(&mut cmap4, 16 + seg_count * 8);
    push_u16(&mut cmap4, 0);
    push_u16(&mut cmap4, seg_count * 2);
    push_u16(&mut cmap4, search_range);
    push_u16(&mut cmap4, entry_selector);
    push_u16(&mut cmap4, range_shift);
    for &(_, end) in CJK_RANGES { push_u16(&mut cmap4, end as u16); }
    push_u16(&mut cmap4, 0xFFFF);
    push_u16(&mut cmap4, 0);
    for &(start, _) in CJK_RANGES { push_u16(&mut cmap4, start as u16); }
    push_u16(&mut cmap4, 0xFFFF);
    for &(start, _) in CJK_RANGES { push_u16(&mut cmap4, (1u32.wrapping_sub(start)) as u16); }
    push_u16(&mut cmap4, 1);
    for _ in 0..=CJK_RANGES.len() { push_u16(&mut cmap4, 0); }

    let mut cmap12 = Vec::new();
    push_u16(&mut cmap12, 12);
    push_u16(&mut cmap12, 0);
    push_u32(&mut cmap12, 16 + (CJK_RANGES.len() as u32) * 12);
    push_u32(&mut cmap12, 0);
    push_u32(&mut cmap12, CJK_RANGES.len() as u32);
    for &(start, end) in CJK_RANGES {
        push_u32(&mut cmap12, start);
        push_u32(&mut cmap12, end);
        push_u32(&mut cmap12, 1);
    }

    let mut cmap = Vec::new();
    push_u16(&mut cmap, 0);
    push_u16(&mut cmap, 2);
    let off4 = 4 + 16;
    let off12 = off4 + cmap4.len() as u32;
    push_u16(&mut cmap, 0); push_u16(&mut cmap, 3); push_u32(&mut cmap, off4);
    push_u16(&mut cmap, 3); push_u16(&mut cmap, 10); push_u32(&mut cmap, off12);
    cmap.extend_from_slice(&cmap4);
    cmap.extend_from_slice(&cmap12);

    let mut head = vec![0u8; 54];
    put_u32(&mut head, 0, 0x0001_0000);
    put_u32(&mut head, 4, 0x0001_0000);
    put_u32(&mut head, 12, 0x5F0F_3CF5);
    put_u16(&mut head, 16, 0x000B);
    put_u16(&mut head, 18, 1000);
    put_u16(&mut head, 36, (-200i16) as u16);
    put_u16(&mut head, 38, 1000);
    put_u16(&mut head, 40, 880);
    put_u16(&mut head, 46, 8);

    let mut hhea = vec![0u8; 36];
    put_u32(&mut hhea, 0, 0x0001_0000);
    put_u16(&mut hhea, 4, 880);
    put_u16(&mut hhea, 6, (-120i16) as u16);
    put_u16(&mut hhea, 10, 1000);
    put_u16(&mut hhea, 34, 2);

    let mut hmtx = Vec::new();
    push_u16(&mut hmtx, 500); push_u16(&mut hmtx, 0);
    push_u16(&mut hmtx, 1000); push_u16(&mut hmtx, 0);

    let mut maxp = vec![0u8; 32];
    put_u32(&mut maxp, 0, 0x0001_0000);
    put_u16(&mut maxp, 4, 2);

    let loca = vec![0u8; 6];
    let glyf = vec![0u8; 4];

    let mut os2 = vec![0u8; 96];
    put_u16(&mut os2, 0, 4);
    put_u16(&mut os2, 2, 1000);
    put_u16(&mut os2, 4, 400);
    put_u16(&mut os2, 6, 5);
    put_u16(&mut os2, 62, 0x0040);
    put_u16(&mut os2, 64, 0x2014);
    put_u16(&mut os2, 66, 0xFFEF);
    put_u16(&mut os2, 68, 880);
    put_u16(&mut os2, 70, (-120i16) as u16);
    put_u16(&mut os2, 74, 880);
    put_u16(&mut os2, 76, 120);

    let mut post = vec![0u8; 32];
    put_u32(&mut post, 0, 0x0003_0000);

    let name = build_name_table(family);

    let tables: [(&[u8; 4], Vec<u8>); 10] = [
        (b"OS/2", os2),
        (b"cmap", cmap),
        (b"glyf", glyf),
        (b"head", head),
        (b"hhea", hhea),
        (b"hmtx", hmtx),
        (b"loca", loca),
        (b"maxp", maxp),
        (b"name", name),
        (b"post", post),
    ];
    let num_tables = tables.len() as u16;
    let mut out = Vec::new();
    push_u32(&mut out, 0x0001_0000);
    push_u16(&mut out, num_tables);
    push_u16(&mut out, 128);
    push_u16(&mut out, 3);
    push_u16(&mut out, num_tables * 16 - 128);
    let mut offset = 12 + (num_tables as usize) * 16;
    for (tag, data) in &tables {
        out.extend_from_slice(*tag);
        push_u32(&mut out, 0);
        push_u32(&mut out, offset as u32);
        push_u32(&mut out, data.len() as u32);
        offset += (data.len() + 3) & !3;
    }
    for (_, data) in &tables {
        out.extend_from_slice(data);
        while out.len() & 3 != 0 { out.push(0); }
    }
    out
}

fn build_name_table(family: &str) -> Vec<u8> {
    let utf16: Vec<u8> = family.encode_utf16().flat_map(|u| u.to_be_bytes()).collect();
    let ids = [1u16, 2, 4, 6];
    let mut name = Vec::new();
    push_u16(&mut name, 0);
    push_u16(&mut name, ids.len() as u16);
    push_u16(&mut name, 6 + (ids.len() as u16) * 12);
    for id in ids {
        push_u16(&mut name, 3);
        push_u16(&mut name, 1);
        push_u16(&mut name, 0x0409);
        push_u16(&mut name, id);
        push_u16(&mut name, utf16.len() as u16);
        push_u16(&mut name, 0);
    }
    name.extend_from_slice(&utf16);
    name
}

fn push_u16(buf: &mut Vec<u8>, v: u16) { buf.extend_from_slice(&v.to_be_bytes()); }
fn push_u32(buf: &mut Vec<u8>, v: u32) { buf.extend_from_slice(&v.to_be_bytes()); }
fn put_u16(buf: &mut [u8], i: usize, v: u16) { buf[i..i + 2].copy_from_slice(&v.to_be_bytes()); }
fn put_u32(buf: &mut [u8], i: usize, v: u32) { buf[i..i + 4].copy_from_slice(&v.to_be_bytes()); }

thread_local! {
    static RASTER_CANVAS: RefCell<Option<(HtmlCanvasElement, CanvasRenderingContext2d)>> = const { RefCell::new(None) };
}

pub(super) fn paint_glyph(
    pixels: &mut [u8],
    width: i32,
    height: i32,
    text: &str,
    glyph: &LayoutGlyph,
    base_x: f32,
    baseline_y: f32,
    scale: f32,
    color: Color,
) {
    let s = text.get(glyph.start..glyph.end).unwrap_or("").trim();
    if s.is_empty() { return; }
    let px_size = (glyph.font_size * scale).ceil().max(12.0);
    let box_w = ((px_size * 1.6).ceil() as u32).max(24);
    let box_h = ((px_size * 1.6).ceil() as u32).max(24);
    let pad_x = (px_size * 0.25).round();
    let base_off_y = (px_size * 1.1).round();

    RASTER_CANVAS.with(|slot| {
        let mut borrow = slot.borrow_mut();
        if borrow.is_none() {
            let Some(doc) = web_sys::window().and_then(|w| w.document()) else { return };
            let Ok(el) = doc.create_element("canvas") else { return };
            let Ok(canvas) = el.dyn_into::<HtmlCanvasElement>() else { return };
            let opts = js_sys::Object::new();
            let _ = js_sys::Reflect::set(&opts, &"willReadFrequently".into(), &true.into());
            let Ok(Some(ctx_obj)) = canvas.get_context_with_context_options("2d", &opts) else { return };
            let Ok(ctx) = ctx_obj.dyn_into::<CanvasRenderingContext2d>() else { return };
            *borrow = Some((canvas, ctx));
        }
        let Some((canvas, ctx)) = borrow.as_ref() else { return };
        if canvas.width() < box_w { canvas.set_width(box_w); }
        if canvas.height() < box_h { canvas.set_height(box_h); }
        ctx.clear_rect(0.0, 0.0, box_w as f64, box_h as f64);
        ctx.set_font(&format!(
            "500 {px_size}px \"PingFang SC\", \"Hiragino Sans GB\", \"Microsoft YaHei\", \"Noto Sans CJK SC\", sans-serif"
        ));
        ctx.set_text_baseline("alphabetic");
        ctx.set_fill_style_str("#ffffff");
        let _ = ctx.fill_text(s, pad_x as f64, base_off_y as f64);
        let Ok(img) = ctx.get_image_data(0.0, 0.0, box_w as f64, box_h as f64) else { return };
        let data = img.data();
        let origin_x = ((base_x + glyph.x) * scale - pad_x).round() as i32;
        let origin_y = (baseline_y * scale - base_off_y).round() as i32;
        let (cr, cg, cb, ca) = (color.r() as u32, color.g() as u32, color.b() as u32, color.a() as u32);
        for dy in 0..box_h as i32 {
            let py = origin_y + dy;
            if py < 0 || py >= height { continue; }
            for dx in 0..box_w as i32 {
                let px = origin_x + dx;
                if px < 0 || px >= width { continue; }
                let src = ((dy as u32 * box_w + dx as u32) * 4) as usize;
                let a = (data[src + 3] as u32 * ca + 127) / 255;
                if a == 0 { continue; }
                let dst = ((py * width + px) * 4) as usize;
                for (ch, c) in [cr, cg, cb].into_iter().enumerate() {
                    pixels[dst + ch] = ((c * a + pixels[dst + ch] as u32 * (255 - a) + 127) / 255) as u8;
                }
            }
        }
    });
}
