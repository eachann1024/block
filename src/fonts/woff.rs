//! Converts WOFF 1.0 (`wOFF`) containers from npmmirror `@fontsource/*` fallbacks into raw SFNT
//! (`\0\1\0\0` / `OTTO`) font files so CoreText, `cosmic-text` (`ttf-parser`), and `latex-rust`
//! (`skrifa`) can all load them directly.

use std::io::Read;

use flate2::read::ZlibDecoder;

use super::{is_valid_font_header, is_valid_sfnt};

fn zlib_decompress(src: &[u8], orig_len: usize) -> Option<Vec<u8>> {
    let mut out = Vec::with_capacity(orig_len);
    ZlibDecoder::new(src).read_to_end(&mut out).ok()?;
    (out.len() == orig_len).then_some(out)
}

fn read_u16(buf: &[u8], off: usize) -> Option<u16> {
    Some(u16::from_be_bytes(buf.get(off..off + 2)?.try_into().ok()?))
}

fn read_u32(buf: &[u8], off: usize) -> Option<u32> {
    Some(u32::from_be_bytes(buf.get(off..off + 4)?.try_into().ok()?))
}

/// Validates font magic bytes and converts `wOFF` into a standard SFNT (`\0\1\0\0` / `OTTO`) buffer.
pub fn ensure_sfnt(bytes: Vec<u8>) -> Option<Vec<u8>> {
    if !is_valid_font_header(&bytes) {
        return None;
    }
    if is_valid_sfnt(&bytes) {
        return Some(bytes);
    }
    if bytes.starts_with(b"wOFF") {
        return unpack_woff(&bytes);
    }
    None
}

fn unpack_woff(woff: &[u8]) -> Option<Vec<u8>> {
    if woff.len() < 44 {
        return None;
    }
    let flavor = woff.get(4..8)?;
    let num_tables = read_u16(woff, 12)? as usize;
    if num_tables == 0 || num_tables > 128 || woff.len() < 44 + num_tables * 20 {
        return None;
    }
    let mut search_range = 1u16;
    let mut entry_selector = 0u16;
    while (search_range as usize) * 2 <= num_tables {
        search_range *= 2;
        entry_selector += 1;
    }
    search_range *= 16;
    let range_shift = (num_tables as u16) * 16 - search_range;

    let mut out = Vec::with_capacity(woff.len() * 2);
    out.extend_from_slice(flavor);
    out.extend_from_slice(&(num_tables as u16).to_be_bytes());
    out.extend_from_slice(&search_range.to_be_bytes());
    out.extend_from_slice(&entry_selector.to_be_bytes());
    out.extend_from_slice(&range_shift.to_be_bytes());

    let dir_pos = out.len();
    out.resize(dir_pos + num_tables * 16, 0);

    for i in 0..num_tables {
        let entry = 44 + i * 20;
        let tag = woff.get(entry..entry + 4)?;
        let offset = read_u32(woff, entry + 4)? as usize;
        let comp_len = read_u32(woff, entry + 8)? as usize;
        let orig_len = read_u32(woff, entry + 12)? as usize;
        let orig_csum = read_u32(woff, entry + 16)?;
        if orig_len > 32 * 1024 * 1024 {
            return None;
        }
        let comp = woff.get(offset..offset.checked_add(comp_len)?)?;
        let raw = if comp_len < orig_len {
            zlib_decompress(comp, orig_len)?
        } else if comp_len == orig_len {
            comp.to_vec()
        } else {
            return None;
        };
        let t_off = u32::try_from(out.len()).ok()?;
        out.extend_from_slice(&raw);
        while out.len() % 4 != 0 {
            out.push(0);
        }
        let d = dir_pos + i * 16;
        out[d..d + 4].copy_from_slice(tag);
        out[d + 4..d + 8].copy_from_slice(&orig_csum.to_be_bytes());
        out[d + 8..d + 12].copy_from_slice(&t_off.to_be_bytes());
        out[d + 12..d + 16].copy_from_slice(&(orig_len as u32).to_be_bytes());
    }
    is_valid_sfnt(&out).then_some(out)
}
