//! Inline code spacing, hover tracking and ⌘+Click copy (chip painting: `inline_code_chip`).
use std::ops::Range;
use std::cell::RefCell;
use gpui::{Context, Hsla, Pixels, Point, SharedString, TextRun, px, rgb};
use crate::theme::{Theme, is_dark, tr};
use super::{model::{Block, MarkKind}, view::Editor};

type HoverTarget = (gpui::EntityId, u64, Range<usize>);

thread_local! {
    /// Code chip under the pointer: (editor entity, block, span) — view state only.
    static HOVER: RefCell<Option<HoverTarget>> = const { RefCell::new(None) };
}

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
    if is_dark() { rgb(0xff7369).into() } else { rgb(0xeb5757).into() }
}

/// Chip span under the pointer in `block` of editor `id`, for the hover fill.
pub(in crate::editor) fn hovered(id: gpui::EntityId, block: u64) -> Option<Range<usize>> {
    HOVER.with(|h| h.borrow().as_ref().filter(|(e, b, _)| *e == id && *b == block).map(|(_, _, r)| r.clone()))
}

pub(in crate::editor) fn clear_hover() {
    HOVER.with(|h| *h.borrow_mut() = None);
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
        let token = block.text[range.clone()].to_owned();
        cx.write_to_clipboard(super::paste_clipboard::inline_item(&Block::new(0, super::model::Kind::Paragraph, token.clone())));
        self.status = format!("{}: {}", tr("已复制代码", "Copied code"), token);
        let target: HoverTarget = (cx.entity_id(), block_id, range);
        HOVER.with(|h| *h.borrow_mut() = Some(target));
        cx.notify();
        true
    }

    pub(super) fn update_code_hover(&mut self, position: Point<Pixels>, cx: &mut Context<Self>) {
        let found = self.code_under(position).map(|(b, r)| (cx.entity_id(), b, r));
        let hover_changed = HOVER.with(|h| {
            let mut h = h.borrow_mut();
            let c = *h != found;
            if c { *h = found; }
            c
        });
        if hover_changed { cx.notify(); }
    }
}
