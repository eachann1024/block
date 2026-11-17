use gpui::{Bounds, Context, FontId, Pixels, Point, TextSystem, WrappedLine, point, px, size};
use super::{Editor, selection::{Position, boundary}};

thread_local! {
    /// Cached `(above_baseline, below_baseline)` ink span in em units `(cjk, regular)` per `FontId`.
    static CARET_SPANS: std::cell::RefCell<std::collections::HashMap<FontId, (Option<(f32, f32)>, (f32, f32))>> = Default::default();
}

fn glyph_span_em(ts: &TextSystem, font_id: FontId, ch: char) -> Option<(f32, f32)> {
    let b = ts.typographic_bounds(font_id, px(100.), ch).ok()?;
    let h = f32::from(b.size.height) * 0.01;
    if !(0.05..=2.5).contains(&h) { return None; }
    let y = f32::from(b.origin.y) * 0.01;
    // Font-kit typographic bounds put y upward from the baseline: top = y + h, bottom below baseline = -y.
    Some(((y + h).clamp(0.2, 1.8), (-y).clamp(-0.3, 1.2)))
}

fn caret_span_px(ts: &TextSystem, font_id: FontId, font_px: Pixels, prefer_cjk: bool) -> (Pixels, Pixels) {
    let (cjk, reg) = CARET_SPANS.with(|cache| {
        if let Some(&entry) = cache.borrow().get(&font_id) { return entry; }
        let cjk = match (glyph_span_em(ts, font_id, '国'), glyph_span_em(ts, font_id, '永')) {
            (Some(a), Some(b)) => Some(((a.0 + b.0) * 0.5 + 0.10, ((a.1 + b.1) * 0.5 + 0.10).max(0.12))),
            (Some(a), None) | (None, Some(a)) => Some((a.0 + 0.10, (a.1 + 0.10).max(0.12))),
            (None, None) => None,
        };
        let cap = glyph_span_em(ts, font_id, 'H')
            .or_else(|| glyph_span_em(ts, font_id, '0'))
            .map(|(top, bot)| (top + 0.16, bot.max(0.) + 0.16))
            .or_else(|| {
                let cap_em = (f32::from(ts.cap_height(font_id, px(100.))) * 0.01).clamp(0.45, 1.15);
                (cap_em > 0.45).then_some((cap_em + 0.16, 0.16))
            })
            .or(cjk)
            .unwrap_or_else(|| {
                let asc = (f32::from(ts.ascent(font_id, px(100.))) * 0.01).clamp(0.55, 1.20);
                let desc = (f32::from(ts.descent(font_id, px(100.))) * 0.01).clamp(0.10, 0.45);
                (asc * 0.92, desc * 0.75)
            });
        let entry = (cjk, cap);
        cache.borrow_mut().insert(font_id, entry);
        entry
    });
    let (above, below) = if prefer_cjk { cjk.unwrap_or(reg) } else { reg };
    (font_px * above, font_px * below)
}

fn is_cjk_ideograph(ch: char) -> bool {
    matches!(ch as u32, 0x2E80..=0x9FFF | 0xF900..=0xFAFF | 0xFE30..=0xFE4F | 0xFF00..=0xFFEF | 0x20000..=0x323AF)
}

fn caret_run_at(line: &WrappedLine, local_idx: usize) -> Option<(FontId, bool)> {
    let text = line.text.as_ref();
    let probe_char = if local_idx > 0 {
        text.get(..local_idx).and_then(|s| s.chars().next_back())
    } else {
        text.chars().find(|&c| c != '\u{200b}')
    };
    let prefer_cjk = probe_char.is_some_and(is_cjk_ideograph);
    let mut chosen = None;
    for run in line.runs() {
        for g in &run.glyphs {
            if local_idx > 0 && g.index < local_idx {
                chosen = Some(run.font_id);
            } else if chosen.is_none() {
                return Some((run.font_id, prefer_cjk));
            }
        }
    }
    chosen.or_else(|| line.runs().first().map(|r| r.font_id)).map(|id| (id, prefer_cjk))
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub(in crate::editor) struct LayoutKey {
    pub content: u64,
    pub width_bits: u32,
    pub nowrap: bool,
}
thread_local! {
    static KEYS: std::cell::RefCell<std::collections::HashMap<(gpui::EntityId, u64), LayoutKey>> = Default::default();
}
pub(in crate::editor) fn get_key(editor: gpui::EntityId, block: u64) -> Option<LayoutKey> {
    KEYS.with(|keys| keys.borrow().get(&(editor, block)).copied())
}
pub(in crate::editor) fn set_key(editor: gpui::EntityId, block: u64, key: LayoutKey) {
    KEYS.with(|keys| { keys.borrow_mut().insert((editor, block), key); });
}
pub(in crate::editor) fn clear_editor_keys(editor: gpui::EntityId) {
    KEYS.with(|keys| keys.borrow_mut().retain(|(ed, _), _| *ed != editor));
}
pub(in crate::editor) fn block_hash(block: &crate::editor::model::Block, minimal: bool) -> u64 {
    use std::hash::{Hash, Hasher};
    fn hash_inline(b: &crate::editor::model::Block, h: &mut impl Hasher) {
        b.text.hash(h); b.spans.len().hash(h);
        for s in &b.spans {
            (s.start, s.end, s.marks.len()).hash(h);
            for m in &s.marks { (*m as u8).hash(h); }
        }
        b.meta.inline.len().hash(h);
        for t in &b.meta.inline { (&t.start, &t.end, &t.name, &t.attrs).hash(h); }
    }
    let mut h = std::collections::hash_map::DefaultHasher::new();
    (block.kind as u8, block.meta.heading_level, block.meta.callout.map(|c| c as u8), &block.meta.code_language, block.meta.divider, block.meta.image.is_some(), block.meta.quote_depth).hash(&mut h);
    (crate::theme::font_settings_hash(), crate::theme::is_dark(), crate::editor::zoom::zoom_percent(minimal)).hash(&mut h);
    hash_inline(block, &mut h);
    if let Some(table) = &block.meta.table {
        (table.format as u8, table.columns, &table.column_widths, &table.attrs, table.rows.len()).hash(&mut h);
        for a in &table.alignments { (*a as u8).hash(&mut h); }
        for row in &table.rows {
            row.cells.len().hash(&mut h);
            for cell in &row.cells {
                (cell.column, cell.colspan, cell.rowspan, cell.header, &cell.attrs).hash(&mut h);
                hash_inline(&cell.content, &mut h);
            }
        }
    }
    h.finish()
}

pub(in crate::editor) struct TextLayout {
    pub bounds: Bounds<Pixels>,
    pub lines: Vec<(usize, WrappedLine)>,
    pub line_height: Pixels,
    pub cells: Vec<CellLayout>,
    pub alignment: gpui::TextAlign,
    pub code_spans: Vec<std::ops::Range<usize>>,
}
pub(in crate::editor) struct CellLayout {
    pub range: std::ops::Range<usize>,
    pub frame: Bounds<Pixels>,
    pub text: Box<TextLayout>,
    pub background: Option<gpui::Hsla>,
}
impl TextLayout {
    pub fn empty(line_height: Pixels) -> Self {
        Self { bounds: Bounds::default(), lines: Vec::new(), line_height, cells: Vec::new(), alignment: gpui::TextAlign::Left, code_spans: Vec::new() }
    }
    pub(in crate::editor) fn aligned_x(&self, line: &WrappedLine, visual: usize) -> Pixels {
        let edge_x = |i: usize| { let edge = &line.wrap_boundaries[i];
            line.unwrapped_layout.runs[edge.run_ix].glyphs[edge.glyph_ix].position.x };
        let rows = line.wrap_boundaries.len();
        let index = visual.min(rows);
        let start = if index == 0 { px(0.) } else { edge_x(index - 1) };
        let end = if index < rows { edge_x(index) } else { line.unwrapped_layout.width };
        let extra = (self.bounds.size.width - (end - start)).max(px(0.));
        match self.alignment { gpui::TextAlign::Center => extra / 2.,
            gpui::TextAlign::Right => extra, _ => px(0.) }
    }
    pub fn height(&self) -> Pixels {
        if !self.cells.is_empty() { return self.bounds.size.height; }
        self.lines.iter().map(|(_, l)| l.size(self.line_height).height).fold(px(0.), |a,b| a+b)
    }
    pub fn translate(&mut self, origin: Point<Pixels>) {
        self.bounds.origin += origin;
        for cell in &mut self.cells { cell.frame.origin += origin; cell.text.translate(origin); }
    }
    pub fn point(&self, offset: usize) -> Point<Pixels> {
        if !self.cells.is_empty() {
            let cell = self.cells.iter().find(|cell| cell.range.start <= offset && offset <= cell.range.end)
                .unwrap_or_else(|| if offset < self.cells[0].range.start { &self.cells[0] }
                    else { self.cells.last().unwrap() });
            return cell.text.point(offset.saturating_sub(cell.range.start));
        }
        let offset = crate::editor::inline_code::doc_to_shaped(offset, &self.code_spans);
        let mut y = px(0.);
        for (i, (start, line)) in self.lines.iter().enumerate() {
            if offset <= start + line.len() || i + 1 == self.lines.len() {
                let p = line.position_for_index(offset.saturating_sub(*start).min(line.len()), self.line_height).unwrap_or_default();
                let visual = (p.y / self.line_height) as usize;
                return self.bounds.origin + point(p.x + self.aligned_x(line, visual), y + p.y);
            }
            y += line.size(self.line_height).height;
        }
        self.bounds.origin
    }
    /// Font-adaptive caret `(origin, height)` anchored to the active run's actual glyph ink bounds
    /// relative to the painted baseline so every font family centers cleanly.
    pub fn caret_geometry(&self, offset: usize, ts: &TextSystem) -> (Point<Pixels>, Pixels) {
        if !self.cells.is_empty() {
            let cell = self.cells.iter().find(|cell| cell.range.start <= offset && offset <= cell.range.end)
                .unwrap_or_else(|| if offset < self.cells[0].range.start { &self.cells[0] }
                    else { self.cells.last().unwrap() });
            return cell.text.caret_geometry(offset.saturating_sub(cell.range.start), ts);
        }
        let shaped = crate::editor::inline_code::doc_to_shaped(offset, &self.code_spans);
        let mut y = px(0.);
        for (i, (start, line)) in self.lines.iter().enumerate() {
            if shaped <= start + line.len() || i + 1 == self.lines.len() {
                let local = shaped.saturating_sub(*start).min(line.len());
                let p = line.position_for_index(local, self.line_height).unwrap_or_default();
                let visual = (p.y / self.line_height) as usize;
                let font_px = line.font_size();
                let baseline = (self.line_height - line.ascent() - line.descent()) * 0.5 + line.ascent();
                let (caret_h, top) = if let Some((font_id, prefer_cjk)) = caret_run_at(line, local) {
                    let (above, below) = caret_span_px(ts, font_id, font_px, prefer_cjk);
                    let h = (above + below).round().clamp((font_px * 0.85).round(), self.line_height);
                    let mid = baseline - (above - below) * 0.5;
                    let top = (mid - h * 0.5).clamp(px(0.), self.line_height - h);
                    (h, top)
                } else {
                    let h = (font_px * 1.125).round().min(self.line_height);
                    (h, (baseline - font_px * 0.95).clamp(px(0.), self.line_height - h))
                };
                let dx = if offset == 0 { px(0.) } else { px(0.5) };
                return (self.bounds.origin + point(p.x + self.aligned_x(line, visual) + dx, y + p.y + top), caret_h);
            }
            y += line.size(self.line_height).height;
        }
        let caret_h = (self.line_height * 0.75).round().min(self.line_height);
        (self.bounds.origin + point(px(0.), (self.line_height - caret_h) * 0.5), caret_h)
    }
    pub fn hit(&self, point: Point<Pixels>) -> usize {
        if !self.cells.is_empty() {
            let distance = |cell: &CellLayout| {
                let dx = (cell.frame.left()-point.x).max(px(0.)).max(point.x-cell.frame.right());
                let dy = (cell.frame.top()-point.y).max(px(0.)).max(point.y-cell.frame.bottom());
                dx + dy
            };
            let cell = self.cells.iter().min_by(|a,b| distance(a).partial_cmp(&distance(b)).unwrap_or(std::cmp::Ordering::Equal)).unwrap();
            return cell.range.start + cell.text.hit(point).min(cell.range.len());
        }
        let mut y = self.bounds.top();
        for (i, (start, line)) in self.lines.iter().enumerate() {
            let height = line.size(self.line_height).height;
            if point.y < y + height || i + 1 == self.lines.len() {
                let local_y = (point.y-y).max(px(0.)).min(height-px(0.01));
                let position = gpui::point(point.x - self.bounds.left()
                    - self.aligned_x(line, (local_y / self.line_height) as usize), local_y);
                let raw = start + line.closest_index_for_position(position, self.line_height).unwrap_or_else(|i| i);
                return crate::editor::inline_code::shaped_to_doc(raw, &self.code_spans);
            }
            y += height;
        }
        0
    }
    pub fn rects(&self, range: std::ops::Range<usize>) -> Vec<Bounds<Pixels>> {
        if !self.cells.is_empty() {
            return self.cells.iter().filter(|cell| {
                if range.is_empty() { cell.range.start <= range.start && range.start <= cell.range.end }
                else { cell.range.start < range.end && range.start < cell.range.end }
            }).flat_map(|cell| {
                let start = range.start.max(cell.range.start).min(cell.range.end) - cell.range.start;
                let end = range.end.max(cell.range.start).min(cell.range.end) - cell.range.start;
                cell.text.rects(start..end)
            }).collect();
        }
        let shaped_range = crate::editor::inline_code::doc_range_to_shaped(range.clone(), &self.code_spans);
        if shaped_range.is_empty() {
            let a = self.point(range.start);
            return vec![Bounds::new(a, size(px(1.5), self.line_height))];
        }
        let mut out = Vec::new();
        let mut y = self.bounds.top();
        for (line_start, line) in &self.lines {
            let mut r_start = 0usize;
            for (v, r_end) in line.wrap_boundaries.iter()
                .map(|wb| line.unwrapped_layout.runs[wb.run_ix].glyphs[wb.glyph_ix].index)
                .chain(std::iter::once(line.len()))
                .enumerate()
            {
                let a = shaped_range.start.max(line_start + r_start);
                let b = shaped_range.end.min(line_start + r_end);
                if a < b {
                    let (i0, i1) = (a - line_start, b - line_start);
                    let row_x0 = line.unwrapped_layout.x_for_index(r_start);
                    let base_x = self.bounds.left() + self.aligned_x(line, v) - row_x0;
                    let x0 = base_x + line.unwrapped_layout.x_for_index(i0);
                    let x1 = base_x + line.unwrapped_layout.x_for_index(i1);
                    let row_y = y + self.line_height * (v as f32);
                    out.push(Bounds::new(point(x0, row_y), size((x1 - x0).max(px(1.5)), self.line_height)));
                }
                r_start = r_end;
            }
            y += line.size(self.line_height).height;
        }
        if out.is_empty() {
            let a = self.point(range.start);
            out.push(Bounds::new(a, size(px(1.5), self.line_height)));
        }
        out
    }
    pub fn visual_line_bounds(&self, offset: usize) -> (usize, usize) {
        if !self.cells.is_empty() {
            let cell = self.cells.iter().find(|cell| cell.range.start <= offset && offset <= cell.range.end)
                .unwrap_or_else(|| if offset < self.cells[0].range.start { &self.cells[0] } else { self.cells.last().unwrap() });
            let (s, e) = cell.text.visual_line_bounds(offset.saturating_sub(cell.range.start));
            return (cell.range.start + s, cell.range.start + e);
        }
        let p = self.point(offset);
        let start = self.hit(point(self.bounds.left(), p.y + px(1.)));
        let end = self.hit(point(self.bounds.right() + px(10000.), p.y + px(1.)));
        (start, end)
    }
}
impl Editor {
    pub(in crate::editor) fn hit(&self, point: Point<Pixels>) -> Option<Position> {
        // The hit area is the painted text; tables exclude the add-row/column room they reserve.
        let area = |l: &TextLayout| crate::editor::table_layout::grid_rect(l).unwrap_or(l.bounds);
        let distance = |l: &TextLayout| {
            let r = area(l);
            let dy = (r.top() - point.y).max(point.y - r.bottom()).max(px(0.));
            let dx = (r.left() - point.x).max(point.x - r.right()).max(px(0.));
            (f32::from(dy), f32::from(dx))
        };
        // Nearest area wins (vertical first, then horizontal); ties break by document order,
        // never by HashMap iteration order. Stale geometry for removed or hidden blocks is ignored.
        let (id, index, layout) = self.layouts.iter().filter_map(|(id, layout)| {
            let index = self.doc.index_of(*id)?;
            if !self.minimal && self.is_index_hidden(index) { return None; }
            Some((*id, index, layout))
        }).min_by(|(_, ia, a), (_, ib, b)| {
            let (da, db) = (distance(a), distance(b));
            da.0.partial_cmp(&db.0).unwrap_or(std::cmp::Ordering::Equal)
                .then(da.1.partial_cmp(&db.1).unwrap_or(std::cmp::Ordering::Equal))
                .then(ia.cmp(ib))
        })?;
        let text = &self.doc.blocks[index].text;
        Some(Position { block: id, offset: boundary(text, layout.hit(point)) })
    }
    pub(in crate::editor) fn vertical(&mut self, down: bool, extend: bool, cx: &mut Context<Self>) {
        let at = if !extend && !self.caret.is_empty() {
            self.preferred_x = None;
            let (a, b, _, _) = self.ordered();
            if down { b } else { a }
        } else {
            self.caret.head
        };
        let Some(index) = self.doc.index_of(at.block) else { return; };
        let mut target = None;
        if let Some(layout) = self.layouts.get(&at.block) {
            let point = layout.point(at.offset);
            let x = self.preferred_x.unwrap_or(point.x);
            self.preferred_x = Some(x);
            let y = point.y + if down { layout.line_height } else { -layout.line_height };
            if !layout.cells.is_empty() {
                // Tables: move inside the cell while it has more lines, else to the adjacent row's cell under x.
                if let Some(cell) = layout.cells.iter().find(|c| c.range.start <= at.offset && at.offset <= c.range.end) {
                    let inner = &cell.text.bounds;
                    let text_bottom = inner.top() + cell.text.height();
                    if y >= inner.top() && y < text_bottom {
                        let off = cell.range.start + cell.text.hit(gpui::point(x, y + px(1.))).min(cell.range.len());
                        let p = cell.text.point(off.saturating_sub(cell.range.start));
                        if (down && p.y > point.y + px(1.)) || (!down && p.y < point.y - px(1.)) {
                            target = Some(Position { block: at.block, offset: off });
                        }
                    }
                    if target.is_none() {
                        let probe_y = if down { cell.frame.bottom() + px(1.) } else { cell.frame.top() - px(1.) };
                        let probe_x = x.max(cell.frame.left()).min(cell.frame.right() - px(1.));
                        if let Some(next) = layout.cells.iter().find(|c| c.frame.contains(&gpui::point(probe_x, probe_y))) {
                            let ty = if down { next.text.bounds.top() } else { (next.text.bounds.top() + next.text.height() - px(1.)).max(next.text.bounds.top()) };
                            target = Some(Position { block: at.block, offset: next.range.start + next.text.hit(gpui::point(x, ty)).min(next.range.len()) });
                        }
                    }
                }
            } else {
                let text_bottom = layout.bounds.top() + layout.height();
                if y >= layout.bounds.top() && y < text_bottom {
                    let off = layout.hit(gpui::point(x, y + px(1.)));
                    let p = layout.point(off);
                    if (down && p.y > point.y + px(1.)) || (!down && p.y < point.y - px(1.)) {
                        target = Some(Position { block: at.block, offset: off });
                    }
                }
            }
        }
        if target.is_none() {
            let next = self.next_visible_index(index, down);
            if let Some(i) = next {
                let b = &self.doc.blocks[i];
                let offset = if crate::editor::image_source::spec_of(b).is_some() || crate::editor::block_menu::is_divider(b) {
                    0
                } else if let Some(layout) = self.layouts.get(&b.id) {
                    let y = if down { layout.bounds.top() } else { (layout.bounds.top() + layout.height() - px(1.)).max(layout.bounds.top()) };
                    layout.hit(point(self.preferred_x.unwrap_or(layout.bounds.left()), y))
                } else {
                    // Resolve the exact visual column after this previously unseen block is shaped.
                    if let Some(x) = self.preferred_x { self.pending_vertical = Some((b.id, x, down, extend)); }
                    let cur_text = &self.doc.blocks[index].text;
                    let column = cur_text[..boundary(cur_text, at.offset)].chars().count();
                    b.text.char_indices().nth(column).map(|(i,_)| i).unwrap_or(b.text.len())
                };
                target = Some(Position { block: b.id, offset });
            } else if down {
                let cur = &self.doc.blocks[index];
                let offset = if crate::editor::image_source::spec_of(cur).is_some() || crate::editor::block_menu::is_divider(cur) {
                    0
                } else if let Some((_, _, r)) = cur.meta.table.as_ref().and_then(|t| t.cell_ranges().into_iter().find(|(_, _, r)| r.start <= at.offset && at.offset <= r.end)) {
                    r.end
                } else {
                    cur.text.len()
                };
                target = Some(Position { block: at.block, offset });
            } else {
                let cur = &self.doc.blocks[index];
                let offset = cur.meta.table.as_ref().and_then(|t| t.cell_ranges().into_iter().find(|(_, _, r)| r.start <= at.offset && at.offset <= r.end))
                    .map_or(0, |(_, _, r)| r.start);
                target = Some(Position { block: at.block, offset });
            }
        }
        if let Some(at) = target { self.set_head(at, extend, cx); }
    }
}
