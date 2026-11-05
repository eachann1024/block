//! Scheme B (TokenChip) inline code spacing, chip painting and ⌘+Click copy.
use std::ops::Range;
use gpui::{BorderStyle, Bounds, ClipboardItem, Context, Corners, Hsla, Pixels, Point,
    SharedString, TextRun, Window, fill, point, px, quad, rgb, rgba, size};
use crate::theme::{Theme, is_dark, tr};
use super::{model::{Block, MarkKind}, view::{Editor, layout::TextLayout}};

pub(in crate::editor) const PAD_BYTES: usize = 2;

pub(in crate::editor) fn code_spans(block: &Block) -> Vec<Range<usize>> {
    if block.meta.code_language.is_some() || block.meta.raw_source.is_some()
        || block.meta.table.is_some() || block.text.is_empty() { return Vec::new(); }
    let mut out: Vec<Range<usize>> = Vec::new();
    for s in &block.spans {
        if s.marks.contains(&MarkKind::Code) && s.start < s.end && s.end <= block.text.len() {
            if let Some(last) = out.last_mut() && last.end == s.start { last.end = s.end; }
            else { out.push(s.start..s.end); }
        }
    }
    out
}

fn slice_runs(runs: &[TextRun], range: Range<usize>) -> Vec<TextRun> {
    if range.is_empty() { return Vec::new(); }
    let (mut out, mut at) = (Vec::new(), 0usize);
    for run in runs {
        let end = at + run.len;
        if end > range.start && at < range.end {
            let (a, b) = (at.max(range.start), end.min(range.end));
            if a < b { let mut r = run.clone(); r.len = b - a; out.push(r); }
        }
        at = end;
    }
    out
}

pub(in crate::editor) fn prepare_shaping(
    text: &str,
    runs: Vec<TextRun>,
    code_spans: &[Range<usize>],
) -> (SharedString, Vec<TextRun>) {
    if code_spans.is_empty() { return (SharedString::from(text.to_owned()), runs); }
    let plain_col = Theme::ocean().text;
    let mut out_text = String::with_capacity(text.len() + code_spans.len() * PAD_BYTES * 2);
    let mut out_runs = Vec::with_capacity(runs.len() + code_spans.len() * 3);
    let mut cursor = 0usize;
    for span in code_spans {
        if cursor < span.start {
            out_text.push_str(&text[cursor..span.start]);
            out_runs.extend(slice_runs(&runs, cursor..span.start));
        }
        out_text.push_str("  ");
        out_runs.push(super::text_runs::plain(PAD_BYTES, plain_col));
        out_text.push_str(&text[span.start..span.end]);
        out_runs.extend(slice_runs(&runs, span.start..span.end));
        out_text.push_str("  ");
        out_runs.push(super::text_runs::plain(PAD_BYTES, plain_col));
        cursor = span.end;
    }
    if cursor < text.len() {
        out_text.push_str(&text[cursor..]);
        out_runs.extend(slice_runs(&runs, cursor..text.len()));
    }
    (SharedString::from(out_text), out_runs)
}

pub(in crate::editor) fn doc_to_shaped(doc_offset: usize, code_spans: &[Range<usize>]) -> usize {
    let mut shift = 0usize;
    for span in code_spans {
        if doc_offset < span.start { break; }
        shift += PAD_BYTES;
        if doc_offset <= span.end { break; }
        shift += PAD_BYTES;
    }
    doc_offset + shift
}

pub(in crate::editor) fn doc_range_to_shaped(range: Range<usize>, code_spans: &[Range<usize>]) -> Range<usize> {
    let mut start_shift = 0usize;
    for span in code_spans {
        if range.start < span.start { break; }
        if range.start == span.start { start_shift += PAD_BYTES; break; }
        start_shift += PAD_BYTES;
        if range.start < span.end { break; }
        if range.start == span.end {
            if range.end > span.end { start_shift += PAD_BYTES; }
            break;
        }
        start_shift += PAD_BYTES;
    }
    let mut end_shift = 0usize;
    for span in code_spans {
        if range.end < span.start { break; }
        if range.end == span.start {
            if range.start >= span.start { end_shift += PAD_BYTES; }
            break;
        }
        end_shift += PAD_BYTES;
        if range.end <= span.end { break; }
        end_shift += PAD_BYTES;
    }
    (range.start + start_shift)..(range.end + end_shift)
}

pub(in crate::editor) fn shaped_to_doc(shaped_offset: usize, code_spans: &[Range<usize>]) -> usize {
    let mut shift = 0usize;
    for span in code_spans {
        let left_pad_start = span.start + shift;
        let code_start = left_pad_start + PAD_BYTES;
        let code_end = span.end + shift + PAD_BYTES;
        let right_pad_end = code_end + PAD_BYTES;
        if shaped_offset < left_pad_start { return shaped_offset - shift; }
        if shaped_offset <= code_start { return span.start; }
        if shaped_offset <= code_end { return shaped_offset - shift - PAD_BYTES; }
        if shaped_offset <= right_pad_end { return span.end; }
        shift += PAD_BYTES * 2;
    }
    shaped_offset.saturating_sub(shift)
}

pub(in crate::editor) fn code_text_color() -> Hsla {
    if is_dark() { rgb(0xadc6ff).into() } else { rgb(0x1e3a6e).into() }
}

fn paint_layout_chips(layout: &TextLayout, window: &mut Window) {
    if layout.code_spans.is_empty() { return; }
    let (bg, border, top_hi): (Hsla, Hsla, Hsla) = if is_dark() {
        (rgb(0x252a34).into(), rgb(0x3a4354).into(), rgba(0xffffff0d).into())
    } else {
        (rgb(0xf1f3f7).into(), rgb(0xd5dce6).into(), rgba(0xffffffe0).into())
    };
    let chip_h = band_height(layout);
    let pad_x = px(5.5);
    for span in &layout.code_spans {
        for r in layout.rects(span.clone()) {
            let top = r.top() + ((r.size.height - chip_h) * 0.5).round();
            let box_bounds = Bounds::new(point(r.left() - pad_x, top), size(r.size.width + pad_x * 2., chip_h));
            window.paint_quad(quad(box_bounds, Corners::all(px(5.5)), bg, px(1.), border, BorderStyle::Solid));
            let hi_bounds = Bounds::new(point(box_bounds.left() + px(3.), box_bounds.top() + px(1.)), size((box_bounds.size.width - px(6.)).max(px(0.)), px(1.)));
            window.paint_quad(fill(hi_bounds, top_hi));
        }
    }
}

pub(in crate::editor) fn paint_chips(layout: &TextLayout, window: &mut Window) {
    if !layout.cells.is_empty() {
        for cell in &layout.cells {
            if !cell.text.code_spans.is_empty() {
                paint_layout_chips(&cell.text, window);
            }
        }
        return;
    }
    paint_layout_chips(layout, window);
}

/// Selection band height: equal to the chip height, so a selection never pokes above or below a chip.
fn band_height(layout: &TextLayout) -> Pixels {
    let font_px = layout.lines.first().map(|(_, l)| l.font_size()).unwrap_or(px(16.));
    (font_px + px(5.)).round().min(layout.line_height - px(2.)).max(px(18.).min(layout.line_height))
}

struct Piece { b: Bounds<Pixels>, l: Pixels, r: Pixels }

fn band(r: Bounds<Pixels>, h: Pixels, left: Pixels, right: Pixels, l: Pixels, rr: Pixels) -> Piece {
    let top = r.top() + ((r.size.height - h) * 0.5).round();
    Piece { b: Bounds::new(point(left, top), size((right - left).max(px(2.)), h)), l, r: rr }
}

/// Pieces of one text layout's selection: plain runs and chip parts share one band height.
fn pieces(layout: &TextLayout, range: Range<usize>) -> Vec<Piece> {
    let h = band_height(layout);
    let plain = |out: &mut Vec<Piece>, a: usize, b: usize| for r in layout.rects(a..b) {
        out.push(band(r, h, r.left(), r.right(), px(2.), px(2.)));
    };
    let (pad_x, chip_r) = (px(5.5), px(5.5));
    let mut out = Vec::new();
    let mut cursor = range.start;
    for span in &layout.code_spans {
        if span.end <= cursor { continue; }
        if span.start >= range.end { break; }
        if cursor < span.start {
            let plain_end = span.start.min(range.end);
            plain(&mut out, cursor, plain_end);
            cursor = plain_end;
        }
        let (s, e) = (cursor.max(span.start), range.end.min(span.end));
        if s < e {
            let rects = layout.rects(s..e);
            for (idx, r) in rects.iter().enumerate() {
                let ext_l = s == span.start || idx > 0;
                let ext_r = e == span.end || idx + 1 < rects.len();
                let left = r.left() - if ext_l { pad_x } else { px(0.) };
                let right = r.right() + if ext_r { pad_x } else { px(0.) };
                out.push(band(*r, h, left, right, if ext_l { chip_r } else { px(2.) }, if ext_r { chip_r } else { px(2.) }));
            }
            cursor = e;
        }
    }
    if cursor < range.end { plain(&mut out, cursor, range.end); }
    // Bridge the breathing gap beside a chip so a selection crossing its edge reads as one band.
    for i in 1..out.len() {
        let (prev, next) = (out[i - 1].b, out[i].b);
        let gap = next.left() - prev.right();
        if (prev.top() - next.top()).abs() < px(1.) && gap >= px(0.) && gap < px(12.) {
            out[i - 1].b.size.width = next.left() - prev.left();
            out[i - 1].r = px(0.);
            out[i].l = px(0.);
        }
    }
    out
}

pub(in crate::editor) fn paint_selection(layout: &TextLayout, range: Range<usize>, window: &mut Window) -> Vec<Bounds<Pixels>> {
    let sel_color = Theme::ocean().selection;
    if range.is_empty() {
        let rects = layout.rects(range);
        for r in &rects {
            window.paint_quad(quad(*r, px(2.), sel_color, 0., gpui::transparent_black(), BorderStyle::default()));
        }
        return rects;
    }
    let all: Vec<Piece> = if layout.cells.is_empty() { pieces(layout, range) } else {
        layout.cells.iter().filter(|c| c.range.start < range.end && range.start < c.range.end).flat_map(|c| {
            let a = range.start.max(c.range.start) - c.range.start;
            let b = range.end.min(c.range.end) - c.range.start;
            pieces(&c.text, a..b)
        }).collect()
    };
    for p in &all {
        let corners = Corners { top_left: p.l, bottom_left: p.l, top_right: p.r, bottom_right: p.r };
        window.paint_quad(quad(p.b, corners, sel_color, 0., gpui::transparent_black(), BorderStyle::default()));
    }
    all.into_iter().map(|p| p.b).collect()
}

impl Editor {
    pub(super) fn code_under(&self, position: Point<Pixels>) -> Option<(u64, Range<usize>)> {
        let at = self.hit(position)?;
        let layout = self.layouts.get(&at.block)?;
        if !layout.cells.is_empty() {
            for cell in &layout.cells {
                for span in &cell.text.code_spans {
                    if cell.text.rects(span.clone()).iter().any(|r| r.dilate(px(5.)).contains(&position)) {
                        return Some((at.block, (cell.range.start + span.start)..(cell.range.start + span.end)));
                    }
                }
            }
            return None;
        }
        layout.code_spans.iter().find(|s| layout.rects((*s).clone()).iter().any(|r| r.dilate(px(5.)).contains(&position)))
            .map(|s| (at.block, s.clone()))
    }

    pub(super) fn copy_code_at(&mut self, position: Point<Pixels>, cx: &mut Context<Self>) -> bool {
        let Some((block_id, range)) = self.code_under(position) else { return false };
        let Some(block) = self.doc.get(block_id) else { return false };
        if range.end > block.text.len() || !block.text.is_char_boundary(range.start) || !block.text.is_char_boundary(range.end) {
            return false;
        }
        let token = block.text[range].to_owned();
        cx.write_to_clipboard(ClipboardItem::new_string(token.clone()));
        self.status = format!("{}: {}", tr("已复制代码", "Copied code"), token);
        cx.notify();
        true
    }
}
