//! Renders blocks to a 720px-wide PNG at 2x. Layout is a single column: a
//! marker gutter, then the text shaped by cosmic-text.

use cosmic_text::{Attrs, Family, Metrics, Style as FontStyle, Weight};
use tiny_skia::{Color, Paint, PathBuilder, Pixmap, Rect, Stroke, Transform};

use crate::editor::model::{Block, Kind, MarkKind};

const CONTENT: f32 = 720.0;
const MARGIN: f32 = 48.0;
const SCALE: f32 = 2.0;
const FONT_SIZE: f32 = 17.0;
const LINE: f32 = 25.5;

pub fn render_png(blocks: &[Block]) -> Option<Vec<u8>> {
    let width = CONTENT + MARGIN * 2.0;
    let mut rows: Vec<Row> = Vec::new();
    let mut y = MARGIN;
    for block in blocks {
        let (size, weight) = metrics(block.kind);
        let text = if block.text.is_empty() { " " } else { block.text.as_str() };
        let wrapped = wrap(text, CONTENT - gutter(block.kind), size);
        let height = (wrapped.len() as f32 * LINE).max(LINE);
        rows.push(Row { block, lines: wrapped, y, height, size, weight });
        y += height + gap(block.kind);
    }
    let height = y + MARGIN;
    let mut pixmap = Pixmap::new((width * SCALE) as u32, (height * SCALE) as u32)?;
    pixmap.fill(Color::WHITE);
    let mut paint = Paint::default();
    for row in &rows {
        paint.set_color_rgba8(0x20, 0x24, 0x2c, 0xff);
        let gx = MARGIN;
        draw_marker(&mut pixmap, row, gx);
        let mut line_y = row.y;
        for line in &row.lines {
            draw_text(&mut pixmap, line, MARGIN + gutter(row.block.kind), line_y, row.size, row.weight, row.block);
            line_y += LINE;
        }
    }
    Some(pixmap.encode_png().ok()?)
}

struct Row<'a> {
    block: &'a Block,
    lines: Vec<String>,
    y: f32,
    height: f32,
    size: f32,
    weight: Weight,
}

fn metrics(kind: Kind) -> (f32, Weight) {
    match kind {
        Kind::H1 => (28.0, Weight::BOLD),
        Kind::H2 => (22.0, Weight::BOLD),
        Kind::H3 => (19.0, Weight::BOLD),
        _ => (FONT_SIZE, Weight::NORMAL),
    }
}

fn gutter(kind: Kind) -> f32 {
    match kind {
        Kind::Bullet | Kind::Numbered | Kind::Todo => 28.0,
        Kind::Quote => 16.0,
        _ => 0.0,
    }
}

fn gap(kind: Kind) -> f32 {
    match kind {
        Kind::H1 | Kind::H2 | Kind::H3 => 12.0,
        _ => 6.0,
    }
}

fn wrap(text: &str, width: f32, size: f32) -> Vec<String> {
    let max_chars = ((width / size) * 2.0).max(8.0) as usize;
    let mut lines = Vec::new();
    let mut current = String::new();
    let mut count = 0usize;
    for ch in text.chars() {
        let w = if ch.is_ascii() { 1 } else { 2 };
        if count + w > max_chars && !current.is_empty() {
            lines.push(std::mem::take(&mut current));
            count = 0;
        }
        current.push(ch);
        count += w;
    }
    if !current.is_empty() {
        lines.push(current);
    }
    if lines.is_empty() { lines.push(String::new()); }
    lines
}

fn draw_marker(pixmap: &mut Pixmap, row: &Row<'_>, x: f32) {
    let y = row.y + LINE / 2.0;
    match row.block.kind {
        Kind::Bullet => dot(pixmap, x + 8.0, y, 3.0, false),
        Kind::Todo => box_mark(pixmap, x + 2.0, y - 7.0, row.block.checked),
        Kind::Quote => bar(pixmap, x, row.y, row.height),
        Kind::Numbered => {}
        _ => {}
    }
}

fn dot(pixmap: &mut Pixmap, x: f32, y: f32, r: f32, _fill: bool) {
    let mut paint = Paint::default();
    paint.set_color_rgba8(0x52, 0x60, 0x71, 0xff);
    let rect = Rect::from_xywh((x - r) * SCALE, (y - r) * SCALE, r * 2.0 * SCALE, r * 2.0 * SCALE);
    if let Some(rect) = rect {
        pixmap.fill_rect(rect, &paint, Transform::identity(), None);
    }
}

fn box_mark(pixmap: &mut Pixmap, x: f32, y: f32, checked: bool) {
    let mut paint = Paint::default();
    paint.set_color_rgba8(0x54, 0x7d, 0xd0, 0xff);
    let rect = Rect::from_xywh(x * SCALE, y * SCALE, 14.0 * SCALE, 14.0 * SCALE);
    let Some(rect) = rect else { return };
    if checked {
        pixmap.fill_rect(rect, &paint, Transform::identity(), None);
    } else {
        let path = PathBuilder::from_rect(rect);
        let mut stroke = Stroke::default();
        stroke.width = SCALE;
        pixmap.stroke_path(&path, &paint, &stroke, Transform::identity(), None);
    }
}

fn bar(pixmap: &mut Pixmap, x: f32, y: f32, height: f32) {
    let mut paint = Paint::default();
    paint.set_color_rgba8(0xc5, 0xd0, 0xe6, 0xff);
    if let Some(rect) = Rect::from_xywh(x * SCALE, y * SCALE, 3.0 * SCALE, height * SCALE) {
        pixmap.fill_rect(rect, &paint, Transform::identity(), None);
    }
}

fn draw_text(pixmap: &mut Pixmap, text: &str, x: f32, y: f32, size: f32, weight: Weight, block: &Block) {
    let italic = block.spans.iter().any(|s| s.marks.contains(&MarkKind::Italic));
    let mut font_system = cosmic_text::FontSystem::new();
    let metrics = Metrics::new(size, LINE);
    let mut buffer = cosmic_text::Buffer::new(&mut font_system, metrics);
    let attrs = Attrs::new()
        .family(Family::Name("PingFang SC"))
        .weight(weight)
        .style(if italic { FontStyle::Italic } else { FontStyle::Normal });
    buffer.set_text(&mut font_system, text, &attrs, cosmic_text::Shaping::Advanced);
    buffer.set_size(&mut font_system, Some(CONTENT), None);
    buffer.shape_until_scroll(&mut font_system, true);
    let mut paint = Paint::default();
    paint.set_color_rgba8(0x20, 0x24, 0x2c, 0xff);
    buffer.draw(&mut font_system, &mut cosmic_text::SwashCache::new(), cosmic_text::Color::rgb(0x20, 0x24, 0x2c), |dx, dy, w, h, color| {
        paint.set_color_rgba8(color.r(), color.g(), color.b(), color.a());
        let px = (x + dx as f32) * SCALE;
        let py = (y + dy as f32) * SCALE;
        if let Some(rect) = Rect::from_xywh(px, py, w as f32 * SCALE, h as f32 * SCALE) {
            pixmap.fill_rect(rect, &paint, Transform::identity(), None);
        }
    });
}
