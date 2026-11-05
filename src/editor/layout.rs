use gpui::{Bounds, Context, Pixels, Point, WrappedLine, point, px, size};
use super::{Editor, selection::{Position, boundary}};

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
pub(in crate::editor) fn clear_keys() {
    KEYS.with(|keys| keys.borrow_mut().clear());
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
    (block.kind as u8, block.meta.heading_level, block.meta.callout.map(|c| c as u8), &block.meta.code_language, block.meta.raw_source.is_some()).hash(&mut h);
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
        let mut starts = vec![px(0.)];
        starts.extend(line.wrap_boundaries.iter().map(|edge|
            line.unwrapped_layout.runs[edge.run_ix].glyphs[edge.glyph_ix].position.x));
        starts.push(line.unwrapped_layout.width);
        let index = visual.min(starts.len()-2);
        let extra = (self.bounds.size.width - (starts[index+1]-starts[index])).max(px(0.));
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
