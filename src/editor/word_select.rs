//! Pointer-aware glyph targeting and dictionary-backed word boundaries for double-click selection.
use std::ops::Range;
use gpui::{Pixels, Point};
use unicode_segmentation::{GraphemeCursor, UnicodeSegmentation};
use super::view::{Editor, layout::TextLayout, selection::{Position, boundary}};

impl Editor {
    /// Resolve `point` to the grapheme under the pointer rather than the nearest insertion caret.
    ///
    /// `TextLayout::hit` wraps GPUI's `closest_index_for_position`, which snaps clicks on the right
    /// half of a character to the *next* character's start offset. For word/character selection on
    /// double-click, inspect only the adjacent grapheme on the same visual line.
    pub(in crate::editor) fn hit_glyph(&self, point: Point<Pixels>) -> Option<Position> {
        let at = self.hit(point)?;
        let block = self.doc.get(at.block)?;
        let layout = self.layouts.get(&at.block)?;
        let (slice_start, slice_end) = active_slice(layout, at.offset, block.text.len());
        let text = &block.text;
        let caret = boundary(text, at.offset).clamp(slice_start, slice_end);
        let prev = prev_grapheme(text, slice_start, caret);
        let next = next_grapheme(text, slice_end, caret);

        if let Some(p) = prev
            && rect_contains_x(layout, p..caret, point)
        {
            return Some(Position { block: at.block, offset: p });
        }
        if let Some(n) = next
            && rect_contains_x(layout, caret..n, point)
        {
            return Some(Position { block: at.block, offset: caret });
        }
        if let Some(p) = prev
            && same_visual_row(layout, p..caret, point.y)
        {
            let next_same_row = next.is_some_and(|n| same_visual_row(layout, caret..n, point.y));
            if !next_same_row {
                return Some(Position { block: at.block, offset: p });
            }
        }
        Some(Position { block: at.block, offset: caret })
    }

    /// Word range around `at` for double-click selection, respecting table cell slices and inline code.
    pub(in crate::editor) fn word_range_at(&self, at: Position) -> Range<usize> {
        let Some(block) = self.doc.get(at.block) else { return at.offset..at.offset };
        if let Some(s) = block.spans.iter().find(|s| {
            s.marks.contains(&crate::editor::model::MarkKind::Code)
                && s.start < s.end
                && s.start <= at.offset
                && at.offset < s.end
        }) {
            return s.start..s.end;
        }
        let text = &block.text;
        let (slice_start, slice_end) = self
            .layouts
            .get(&at.block)
            .map_or((0, text.len()), |l| active_slice(l, at.offset, text.len()));
        if slice_start >= slice_end {
            return slice_start..slice_start;
        }
        let slice = &text[slice_start..slice_end];
        let rel = boundary(text, at.offset).clamp(slice_start, slice_end) - slice_start;
        let local = word_range_in_slice(slice, rel);
        (slice_start + local.start)..(slice_start + local.end)
    }
}

fn active_slice(layout: &TextLayout, offset: usize, text_len: usize) -> (usize, usize) {
    if !layout.cells.is_empty()
        && let Some(cell) = layout.cells.iter().find(|c| c.range.start <= offset && offset <= c.range.end)
    {
        return (cell.range.start.min(text_len), cell.range.end.min(text_len));
    }
    (0, text_len)
}

fn prev_grapheme(text: &str, min: usize, offset: usize) -> Option<usize> {
    if offset <= min { return None; }
    let mut cursor = GraphemeCursor::new(offset, text.len(), true);
    cursor.prev_boundary(text, 0).ok().flatten().filter(|&p| p >= min && text[p..offset] != *"\n")
}

fn next_grapheme(text: &str, max: usize, offset: usize) -> Option<usize> {
    if offset >= max { return None; }
    let mut cursor = GraphemeCursor::new(offset, text.len(), true);
    cursor.next_boundary(text, 0).ok().flatten().filter(|&n| n <= max && text[offset..n] != *"\n")
}

fn same_visual_row(layout: &TextLayout, range: Range<usize>, y: Pixels) -> bool {
    layout.rects(range).into_iter().any(|r| y >= r.top() && y < r.bottom())
}

fn rect_contains_x(layout: &TextLayout, range: Range<usize>, point: Point<Pixels>) -> bool {
    layout.rects(range).into_iter().any(|r| {
        point.y >= r.top()
            && point.y < r.bottom()
            && point.x >= r.left()
            && point.x < r.right()
    })
}

fn word_range_in_slice(slice: &str, rel_offset: usize) -> Range<usize> {
    if slice.is_empty() {
        return 0..0;
    }
    let probe = if rel_offset >= slice.len() {
        prev_grapheme(slice, 0, slice.len()).unwrap_or(0)
    } else {
        boundary(slice, rel_offset)
    };
    let line_start = slice[..probe].rfind('\n').map_or(0, |i| i + 1);
    let line_end = slice[probe..].find('\n').map_or(slice.len(), |i| probe + i);
    if line_start >= line_end {
        return probe..probe;
    }
    let line = &slice[line_start..line_end];
    let line_probe = boundary(line, (probe - line_start).min(line.len().saturating_sub(1)));

    #[cfg(target_os = "macos")]
    if let Some(range) = macos_word_range(line, line_probe) {
        let expanded = expand_to_grapheme_boundaries(line, range);
        return (line_start + expanded.start)..(line_start + expanded.end);
    }

    let fallback = expand_to_grapheme_boundaries(line, unicode_word_range(line, line_probe));
    (line_start + fallback.start)..(line_start + fallback.end)
}

fn expand_to_grapheme_boundaries(line: &str, range: Range<usize>) -> Range<usize> {
    let mut start = range.start.min(line.len());
    let mut end = range.end.min(line.len());
    if start > 0 {
        let mut c = GraphemeCursor::new(start, line.len(), true);
        if !c.is_boundary(line, 0).unwrap_or(true) {
            start = c.prev_boundary(line, 0).ok().flatten().unwrap_or(0);
        }
    }
    if end < line.len() {
        let mut c = GraphemeCursor::new(end, line.len(), true);
        if !c.is_boundary(line, 0).unwrap_or(true) {
            end = c.next_boundary(line, 0).ok().flatten().unwrap_or(line.len());
        }
    }
    start..end
}

fn unicode_word_range(line: &str, probe: usize) -> Range<usize> {
    line.split_word_bound_indices()
        .find(|(start, word)| *start <= probe && probe < start + word.len())
        .map(|(start, word)| start..start + word.len())
        .or_else(|| {
            line.grapheme_indices(true)
                .find(|(start, g)| *start <= probe && probe < start + g.len())
                .map(|(start, g)| start..start + g.len())
        })
        .unwrap_or(0..0)
}

#[cfg(target_os = "macos")]
fn macos_word_range(line: &str, probe: usize) -> Option<Range<usize>> {
    use std::ffi::c_void;

    type CFIndex = isize;
    type CFOptionFlags = usize;
    type CFTypeRef = *const c_void;
    type CFStringRef = *const c_void;
    type CFLocaleRef = *const c_void;
    type CFStringTokenizerRef = *mut c_void;

    #[repr(C)]
    #[derive(Clone, Copy)]
    struct CFRange {
        location: CFIndex,
        length: CFIndex,
    }

    const K_CF_STRING_TOKENIZER_UNIT_WORD_BOUNDARY: CFOptionFlags = 4;
    const K_CF_STRING_TOKENIZER_TOKEN_NONE: CFOptionFlags = 0;

    #[link(name = "CoreFoundation", kind = "framework")]
    unsafe extern "C" {
        fn CFStringCreateWithBytes(
            alloc: CFTypeRef,
            bytes: *const u8,
            num_bytes: CFIndex,
            encoding: u32,
            is_external_representation: u8,
        ) -> CFStringRef;
        fn CFStringGetLength(the_string: CFStringRef) -> CFIndex;
        fn CFStringTokenizerCreate(
            alloc: CFTypeRef,
            string: CFStringRef,
            range: CFRange,
            options: CFOptionFlags,
            locale: CFLocaleRef,
        ) -> CFStringTokenizerRef;
        fn CFStringTokenizerGoToTokenAtIndex(
            tokenizer: CFStringTokenizerRef,
            index: CFIndex,
        ) -> CFOptionFlags;
        fn CFStringTokenizerGetCurrentTokenRange(tokenizer: CFStringTokenizerRef) -> CFRange;
        fn CFRelease(cf: CFTypeRef);
    }

    const K_CF_STRING_ENCODING_UTF8: u32 = 0x0800_0100;

    if line.is_empty() || probe >= line.len() {
        return None;
    }
    let ch = line[probe..].chars().next()?;
    if ch.is_whitespace() {
        return None;
    }

    let mut utf16_to_utf8: Vec<Option<usize>> = Vec::with_capacity(line.len() + 1);
    let mut probe_utf16: Option<CFIndex> = None;
    for (byte_ix, c) in line.char_indices() {
        if byte_ix == probe {
            probe_utf16 = Some(utf16_to_utf8.len() as CFIndex);
        }
        utf16_to_utf8.push(Some(byte_ix));
        for _ in 1..c.len_utf16() {
            utf16_to_utf8.push(None);
        }
    }
    utf16_to_utf8.push(Some(line.len()));
    let target_u16 = probe_utf16?;

    unsafe {
        let cf_str = CFStringCreateWithBytes(
            std::ptr::null(),
            line.as_ptr(),
            line.len() as CFIndex,
            K_CF_STRING_ENCODING_UTF8,
            0,
        );
        if cf_str.is_null() {
            return None;
        }
        let cf_len = CFStringGetLength(cf_str);
        if cf_len <= 0 || target_u16 >= cf_len {
            CFRelease(cf_str);
            return None;
        }
        let tokenizer = CFStringTokenizerCreate(
            std::ptr::null(),
            cf_str,
            CFRange { location: 0, length: cf_len },
            K_CF_STRING_TOKENIZER_UNIT_WORD_BOUNDARY,
            std::ptr::null(),
        );
        if tokenizer.is_null() {
            CFRelease(cf_str);
            return None;
        }
        let kind = CFStringTokenizerGoToTokenAtIndex(tokenizer, target_u16);
        let token = CFStringTokenizerGetCurrentTokenRange(tokenizer);
        CFRelease(tokenizer as CFTypeRef);
        CFRelease(cf_str);

        if kind == K_CF_STRING_TOKENIZER_TOKEN_NONE || token.location < 0 || token.length <= 0 {
            return None;
        }
        let start_u16 = token.location as usize;
        let end_u16 = start_u16.checked_add(token.length as usize)?;
        let start_byte = (*utf16_to_utf8.get(start_u16)?)?;
        let end_byte = (*utf16_to_utf8.get(end_u16)?)?;
        if start_byte < end_byte
            && start_byte <= probe
            && probe < end_byte
            && line.is_char_boundary(start_byte)
            && line.is_char_boundary(end_byte)
        {
            Some(start_byte..end_byte)
        } else {
            None
        }
    }
}
