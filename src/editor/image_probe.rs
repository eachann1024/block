//! Image header dimension probing without external image decoding libraries.
use std::{fs::File, io::Read, path::Path};

pub(super) fn probe_dims(path: &Path) -> Option<(f32, f32)> {
    let mut f = File::open(path).ok()?;
    let mut buf = vec![0u8; 65536];
    let mut n = 0;
    while n < buf.len() {
        match f.read(&mut buf[n..]) { Ok(0) | Err(_) => break, Ok(k) => n += k }
    }
    let b = &buf[..n];
    let (w, h) = if b.len() >= 24 && &b[..8] == b"\x89PNG\r\n\x1a\n" {
        (u32::from_be_bytes([b[16], b[17], b[18], b[19]]), u32::from_be_bytes([b[20], b[21], b[22], b[23]]))
    } else if b.len() >= 10 && (&b[..6] == b"GIF87a" || &b[..6] == b"GIF89a") {
        (u16::from_le_bytes([b[6], b[7]]) as u32, u16::from_le_bytes([b[8], b[9]]) as u32)
    } else if b.len() >= 26 && &b[..2] == b"BM" {
        (i32::from_le_bytes([b[18], b[19], b[20], b[21]]).unsigned_abs(), i32::from_le_bytes([b[22], b[23], b[24], b[25]]).unsigned_abs())
    } else if b.len() >= 30 && &b[..4] == b"RIFF" && &b[8..12] == b"WEBP" {
        match &b[12..16] {
            b"VP8 " if &b[23..26] == &[0x9d, 0x01, 0x2a] =>
                ((u16::from_le_bytes([b[26], b[27]]) & 0x3fff) as u32, (u16::from_le_bytes([b[28], b[29]]) & 0x3fff) as u32),
            b"VP8L" if b[20] == 0x2f => {
                let bits = u32::from_le_bytes([b[21], b[22], b[23], b[24]]);
                ((bits & 0x3fff) + 1, ((bits >> 14) & 0x3fff) + 1)
            }
            b"VP8X" => (u32::from_le_bytes([b[24], b[25], b[26], 0]) + 1, u32::from_le_bytes([b[27], b[28], b[29], 0]) + 1),
            _ => return None,
        }
    } else if b.len() >= 4 && b[0] == 0xFF && b[1] == 0xD8 {
        let mut i = 2;
        let mut found = None;
        while i + 4 <= b.len() && b[i] == 0xFF {
            while i < b.len() && b[i] == 0xFF { i += 1; }
            if i >= b.len() { break; }
            let marker = b[i];
            i += 1;
            if marker == 0xD9 || marker == 0xDA { break; }
            if marker == 0x01 || (0xD0..=0xD8).contains(&marker) { continue; }
            if i + 2 > b.len() { break; }
            let seg_len = u16::from_be_bytes([b[i], b[i + 1]]) as usize;
            if seg_len < 2 { break; }
            if matches!(marker, 0xC0..=0xC3 | 0xC5..=0xC7 | 0xC9..=0xCB | 0xCD..=0xCF) {
                if i + 7 <= b.len() {
                    let h = u16::from_be_bytes([b[i + 3], b[i + 4]]) as u32;
                    let w = u16::from_be_bytes([b[i + 5], b[i + 6]]) as u32;
                    found = Some((w, h));
                }
                break;
            }
            i += seg_len;
        }
        found?
    } else {
        return None;
    };
    (w > 0 && h > 0).then_some((w as f32, h as f32))
}
