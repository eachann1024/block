use gpui::{Bounds, Context, Pixels, Point, WrappedLine, point, px, size};
use super::{Editor, selection::{Position, boundary}};

#[derive(Clone)]
pub(in crate::editor) struct TextLayout {
    pub bounds: Bounds<Pixels>,
    pub lines: Vec<(usize, WrappedLine)>,
    pub line_height: Pixels,
}
impl TextLayout {
    pub fn height(&self) -> Pixels { self.lines.iter().map(|(_, l)| l.size(self.line_height).height).fold(px(0.), |a,b| a+b) }
    pub fn point(&self, offset: usize) -> Point<Pixels> {
        let mut y = px(0.);
        for (i, (start, line)) in self.lines.iter().enumerate() {
            if offset <= start + line.len() || i + 1 == self.lines.len() {
                let p = line.position_for_index(offset.saturating_sub(*start).min(line.len()), self.line_height).unwrap_or_default();
                return self.bounds.origin + point(p.x, y + p.y);
            }
            y += line.size(self.line_height).height;
        }
        self.bounds.origin
    }
    pub fn hit(&self, point: Point<Pixels>) -> usize {
        let mut y = self.bounds.top();
        for (i, (start, line)) in self.lines.iter().enumerate() {
            let height = line.size(self.line_height).height;
            if point.y < y + height || i + 1 == self.lines.len() {
                let position = gpui::point(point.x - self.bounds.left(), (point.y-y).max(px(0.)).min(height-px(0.01)));
                return start + line.closest_index_for_position(position, self.line_height).unwrap_or_else(|i| i);
            }
            y += height;
        }
        0
    }
    pub fn rects(&self, range: std::ops::Range<usize>) -> Vec<Bounds<Pixels>> {
        let a = self.point(range.start); let b = self.point(range.end);
        if a.y == b.y { return vec![Bounds::new(a, size((b.x-a.x).max(px(1.5)), self.line_height))]; }
        let mut out = vec![Bounds::new(a, size((self.bounds.right()-a.x).max(px(1.5)), self.line_height))];
        let mut y = a.y + self.line_height;
        while y < b.y {
            out.push(Bounds::new(point(self.bounds.left(), y), size(self.bounds.size.width, self.line_height)));
            y += self.line_height;
        }
        out.push(Bounds::new(point(self.bounds.left(), b.y), size((b.x-self.bounds.left()).max(px(1.5)), self.line_height)));
        out
    }
}
impl Editor {
    pub(in crate::editor) fn hit(&self, point: Point<Pixels>) -> Option<Position> {
        let (id, layout) = self.layouts.iter().min_by(|(_,a),(_,b)| {
            let distance = |l: &TextLayout| if point.y < l.bounds.top() { l.bounds.top()-point.y } else if point.y > l.bounds.bottom() { point.y-l.bounds.bottom() } else { px(0.) };
            distance(a).partial_cmp(&distance(b)).unwrap_or(std::cmp::Ordering::Equal)
        })?;
        let text = &self.doc.get(*id)?.text;
        Some(Position { block: *id, offset: boundary(text, layout.hit(point)) })
    }
    pub(in crate::editor) fn pointer_down(&mut self, event: &gpui::MouseDownEvent, window: &mut gpui::Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        self.pending_vertical = None;
        if let Some(at) = self.hit(event.position) {
            self.set_head(at, event.modifiers.shift, cx);
            self.selecting = true; self.preferred_x = None;
            window.focus(&self.focus);
        }
    }
    pub(in crate::editor) fn pointer_move(&mut self, event: &gpui::MouseMoveEvent, cx: &mut Context<Self>) {
        if !event.dragging() { self.selecting = false; self.dragging = None; self.drag_over = None; return; }
        if self.dragging.is_some() || self.selecting {
            let viewport = self.list.viewport_bounds();
            if event.position.y < viewport.top()+px(24.) { self.list.scroll_by(px(-18.)); }
            if event.position.y > viewport.bottom()-px(24.) { self.list.scroll_by(px(18.)); }
            if let Some(at) = self.hit(event.position) {
                if self.dragging.is_some() { self.drag_over = self.doc.index_of(at.block); }
                else { self.set_head(at, true, cx); self.reveal_caret = false; }
                cx.notify();
            }
        }
    }
    pub(in crate::editor) fn vertical(&mut self, down: bool, extend: bool, cx: &mut Context<Self>) {
        let at = self.caret.head;
        let Some(index) = self.doc.index_of(at.block) else { return; };
        let mut target = None;
        if let Some(layout) = self.layouts.get(&at.block) {
            let point = layout.point(at.offset);
            let x = self.preferred_x.unwrap_or(point.x);
            self.preferred_x = Some(x);
            let y = point.y + if down { layout.line_height } else { -layout.line_height };
            if y >= layout.bounds.top() && y < layout.bounds.bottom() {
                target = Some(Position { block: at.block, offset: layout.hit(gpui::point(x, y + px(1.))) });
            }
        }
        if target.is_none() {
            let next = if down { index.checked_add(1).filter(|i| *i < self.doc.blocks.len()) } else { index.checked_sub(1) };
            if let Some(i) = next {
                let b = &self.doc.blocks[i];
                let offset = if let Some(layout) = self.layouts.get(&b.id) {
                    let y = if down { layout.bounds.top() } else { layout.bounds.bottom()-px(1.) };
                    layout.hit(point(self.preferred_x.unwrap_or(layout.bounds.left()), y))
                } else {
                    // Resolve the exact visual column after this previously unseen block is shaped.
                    if let Some(x) = self.preferred_x { self.pending_vertical = Some((b.id, x, down, extend)); }
                    let column = self.block_text()[..boundary(self.block_text(), at.offset)].chars().count();
                    b.text.char_indices().nth(column).map(|(i,_)| i).unwrap_or(b.text.len())
                };
                target = Some(Position { block: b.id, offset });
            }
        }
        if let Some(at) = target { self.set_head(at, extend, cx); }
    }
}
