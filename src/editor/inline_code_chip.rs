//! Scheme F (Outline) inline code chips: transparent fill with a hairline border.
//! Fills paint under the selection, borders paint over it so a selected chip keeps its outline.
use std::ops::Range;
use gpui::{BorderStyle, Bounds, Corners, Hsla, Pixels, Window, point, px, quad, rgb, size};
use crate::theme::{Theme, is_dark};
use super::view::layout::TextLayout;

/// What the chip painter needs to know about the block, in block byte offsets.
#[derive(Default, Clone)]
pub(in crate::editor) struct ChipState {
    pub caret: Option<usize>,
    pub selection: Option<Range<usize>>,
    pub hover: Option<Range<usize>>,
}

impl ChipState {
    fn shifted(&self, by: usize) -> Self {
        let sh = |r: &Range<usize>| r.start.saturating_sub(by)..r.end.saturating_sub(by);
        Self {
            caret: self.caret.and_then(|c| c.checked_sub(by)),
            selection: self.selection.as_ref().filter(|r| r.end > by).map(sh),
            hover: self.hover.as_ref().filter(|r| r.start >= by).map(sh),
        }
    }
}

#[derive(Clone, Copy, PartialEq)]
enum Look { Rest, Hover, Edit, Selected }

struct Palette { border: Hsla, hover: Hsla, edit_bg: Hsla, edit_border: Hsla, sel_border: Hsla }

fn palette() -> Palette {
    let accent = Theme::ocean().accent;
    if is_dark() {
        Palette { border: rgb(0x454545).into(), hover: rgb(0x2c2c2c).into(), edit_bg: rgb(0x232c3d).into(), edit_border: accent, sel_border: rgb(0x5875ad).into() }
    } else {
        Palette { border: rgb(0xd3d7de).into(), hover: rgb(0xeef3fc).into(), edit_bg: rgb(0xe5edfc).into(), edit_border: accent, sel_border: rgb(0x8eabe0).into() }
    }
}

/// Chip geometry scales with the line font: 16px text → 21px tall, 5.5px padding, 6px radius.
pub(in crate::editor) struct Metrics { pub h: Pixels, pub pad: Pixels, pub radius: Pixels }

pub(in crate::editor) fn metrics(layout: &TextLayout) -> Metrics {
    let font = layout.lines.first().map(|(_, l)| l.font_size()).unwrap_or(px(16.));
    let f = f32::from(font);
    let s = (f / 16.).min(1.);
    let h = (font + px(5. * s)).round().min(layout.line_height - px(2. * s)).max(px(18. * s).min(layout.line_height));
    Metrics { h, pad: px((f * 0.25 + 1.5 * s).min(9.)), radius: px((f / 6. + 3.3 * s).round().min(8.)) }
}

fn look(span: &Range<usize>, st: &ChipState) -> Look {
    if let Some(sel) = st.selection.as_ref().filter(|r| !r.is_empty()) {
        if sel.start <= span.start && sel.end >= span.end { return Look::Selected; }
        if sel.start < span.end && span.start < sel.end { return Look::Edit; }
    }
    if st.selection.is_none() && st.caret.is_some_and(|c| span.start <= c && c <= span.end) { return Look::Edit; }
    if st.hover.as_ref() == Some(span) { return Look::Hover; }
    Look::Rest
}

/// Chip boxes of one span; a wrapped span gets one box per line, squared off at the break.
fn boxes(layout: &TextLayout, span: &Range<usize>, m: &Metrics) -> Vec<(Bounds<Pixels>, Corners<Pixels>)> {
    let rects = layout.rects(span.clone());
    let n = rects.len();
    rects.into_iter().enumerate().map(|(i, r)| {
        let top = r.top() + ((r.size.height - m.h) * 0.5).round();
        let b = Bounds::new(point(r.left() - m.pad, top), size(r.size.width + m.pad * 2., m.h));
        let (l, rr) = (if i == 0 { m.radius } else { px(2.) }, if i + 1 == n { m.radius } else { px(2.) });
        (b, Corners { top_left: l, bottom_left: l, top_right: rr, bottom_right: rr })
    }).collect()
}

fn each_layout(layout: &TextLayout, st: &ChipState, mut f: impl FnMut(&TextLayout, &ChipState)) {
    if layout.cells.is_empty() { f(layout, st); return; }
    for cell in &layout.cells { f(&cell.text, &st.shifted(cell.range.start)); }
}

/// Chip backgrounds (hover, editing, fully selected); runs before the selection band.
pub(in crate::editor) fn paint_fills(layout: &TextLayout, st: &ChipState, window: &mut Window) {
    let p = palette();
    let sel = Theme::ocean().selection;
    each_layout(layout, st, |l, st| {
        let m = metrics(l);
        for span in &l.code_spans {
            let bg = match look(span, st) { Look::Rest => continue, Look::Hover => p.hover, Look::Edit => p.edit_bg, Look::Selected => sel };
            for (b, c) in boxes(l, span, &m) {
                window.paint_quad(quad(b, c, bg, 0., gpui::transparent_black(), BorderStyle::default()));
            }
        }
    });
}

/// Chip outlines; runs after the selection band so the border floats above it.
/// `Hsla::is_transparent` is `a == 0`, and GPUI then splits the quad into four
/// strips that redraw the joins. A 1/10000 border tint stays one quad and does
/// not move an 8-bit pixel, while the shader fades the border by alpha.
pub(in crate::editor) fn paint_borders(layout: &TextLayout, st: &ChipState, window: &mut Window) {
    let p = palette();
    each_layout(layout, st, |l, st| {
        let m = metrics(l);
        for span in &l.code_spans {
            let border = match look(span, st) { Look::Edit => p.edit_border, Look::Selected => p.sel_border, _ => p.border };
            for (b, c) in boxes(l, span, &m) {
                window.paint_quad(quad(b, c, border.opacity(0.0001), px(1.), border, BorderStyle::Solid));
            }
        }
    });
}

struct Piece { b: Bounds<Pixels>, l: Pixels, r: Pixels, chip: bool }

/// Selection pieces of one text layout. Plain text gets a band as tall as a chip; inside a chip
/// the band sits 1px inside the border, so nothing ever pokes past the chip or the line.
fn pieces(layout: &TextLayout, range: Range<usize>) -> Vec<Piece> {
    let m = metrics(layout);
    let mut out: Vec<Piece> = Vec::new();
    let plain = |out: &mut Vec<Piece>, a: usize, b: usize| for r in layout.rects(a..b) {
        let top = r.top() + ((r.size.height - m.h) * 0.5).round();
        out.push(Piece { b: Bounds::new(point(r.left(), top), size(r.size.width.max(px(2.)), m.h)), l: px(2.), r: px(2.), chip: false });
    };
    let mut cursor = range.start;
    for span in &layout.code_spans {
        if span.end <= cursor { continue; }
        if span.start >= range.end { break; }
        if cursor < span.start { let e = span.start.min(range.end); plain(&mut out, cursor, e); cursor = e; }
        let (s, e) = (cursor.max(span.start), range.end.min(span.end));
        if s < e {
            let rects = layout.rects(s..e);
            let n = rects.len();
            for (i, r) in rects.iter().enumerate() {
                let (ext_l, ext_r) = (s == span.start || i > 0, e == span.end || i + 1 < n);
                let inner = m.pad - px(1.);
                let left = r.left() - if ext_l { inner } else { px(0.) };
                let right = r.right() + if ext_r { inner } else { px(0.) };
                let top = r.top() + ((r.size.height - m.h) * 0.5).round() + px(1.);
                let rad = |ext: bool| if ext { m.radius - px(1.) } else { px(2.) };
                out.push(Piece { b: Bounds::new(point(left, top), size((right - left).max(px(2.)), m.h - px(2.))), l: rad(ext_l), r: rad(ext_r), chip: true });
            }
            cursor = e;
        }
    }
    if cursor < range.end { plain(&mut out, cursor, range.end); }
    // A plain band beside a chip reaches into its border, so a selection crossing the edge reads as one.
    for i in 1..out.len() {
        let (prev, next) = (out[i - 1].b, out[i].b);
        let gap = next.left() - prev.right();
        if (prev.center().y - next.center().y).abs() >= px(1.) || gap < px(0.) || gap >= px(14.) { continue; }
        if !out[i - 1].chip { out[i - 1].b.size.width = next.left() - prev.left(); out[i - 1].r = px(0.); }
        if !out[i].chip { out[i].b.origin.x = prev.right(); out[i].b.size.width = next.right() - prev.right(); out[i].l = px(0.); }
    }
    out
}

pub(in crate::editor) fn paint_selection(layout: &TextLayout, range: Range<usize>, window: &mut Window) -> Vec<Bounds<Pixels>> {
    let sel_color = Theme::ocean().selection;
    if range.is_empty() {
        let rects = layout.rects(range);
        for r in &rects { window.paint_quad(quad(*r, px(2.), sel_color, 0., gpui::transparent_black(), BorderStyle::default())); }
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
